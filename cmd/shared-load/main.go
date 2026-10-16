// Command shared-load builds and launches a game in an already-running shared
// container, by driving the MCP server over stdio via `docker compose exec`.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Where docker-compose.yml mounts GAME_DIR.
const gameDir = "/your-game"

// The compose service and profile this drives.
const (
	service = "simulator-shared"
	profile = "shared"
)

type request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type response struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// The subset of each tool's output this needs.
type buildOutput struct {
	PdxPath     string `json:"pdx_path"`
	ProjectType string `json:"project_type"`
}

type launchOutput struct {
	BundleID string `json:"bundle_id"`
	DataDir  string `json:"data_dir"`
}

type client struct {
	stdin  io.WriteCloser
	stdout *bufio.Reader
	nextID int
}

func (c *client) send(method string, params any, wantReply bool) (json.RawMessage, error) {
	req := request{JSONRPC: "2.0", Method: method, Params: params}
	if wantReply {
		c.nextID++
		req.ID = c.nextID
	}
	line, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := c.stdin.Write(append(line, '\n')); err != nil {
		return nil, fmt.Errorf("writing %s: %w", method, err)
	}
	if !wantReply {
		return nil, nil
	}

	// Skip notifications and other ids.
	for {
		raw, err := c.stdout.ReadBytes('\n')
		if err != nil {
			return nil, fmt.Errorf("reading reply to %s: %w", method, err)
		}
		var resp response
		if err := json.Unmarshal(raw, &resp); err != nil {
			continue
		}
		if resp.ID != c.nextID {
			continue
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("%s: %s", method, resp.Error.Message)
		}
		return resp.Result, nil
	}
}

// callTool unwraps the two layers MCP puts around a tool's output: the
// JSON-RPC result, then the tool result's structuredContent. isError is
// reported as a Go error so a failed build doesn't look like a successful one.
func (c *client) callTool(name string, args map[string]any) (json.RawMessage, error) {
	result, err := c.send("tools/call", map[string]any{"name": name, "arguments": args}, true)
	if err != nil {
		return nil, err
	}
	var wrapper struct {
		StructuredContent json.RawMessage `json:"structuredContent"`
		Content           json.RawMessage `json:"content"`
		IsError           bool            `json:"isError"`
	}
	if err := json.Unmarshal(result, &wrapper); err != nil {
		return nil, fmt.Errorf("%s: parsing result: %w", name, err)
	}
	if wrapper.IsError {
		return nil, fmt.Errorf("%s failed: %s", name, strings.TrimSpace(string(wrapper.Content)))
	}
	if len(wrapper.StructuredContent) > 0 {
		return wrapper.StructuredContent, nil
	}
	return wrapper.Content, nil
}

// clearRunningSimulators kills any Simulator already running in the container,
// for the -keep-container path where the container is not being replaced.
//
// A fresh server session can't see a previous session's Simulator, and two would
// poll the same mcp/command.json. SIGKILL because the Simulator ignores SIGTERM.
func clearRunningSimulators(composeFile string) {
	// pkill exits 0 when it killed something, 1 when there was nothing to kill.
	cmd := exec.Command("docker", "compose", "-f", composeFile,
		"exec", "-T", service, "pkill", "-9", "-f", "bin/PlaydateSimulator")
	if err := cmd.Run(); err == nil {
		fmt.Println("stopped: a Simulator was already running")
	}
}

// recreateContainer tears the shared container down and brings it back up against
// the current GAME_DIR, which is fixed at container start. The service is built by
// name because plain `docker compose build` skips services with a profile.
func recreateContainer(composeFile, gameDir string) error {
	sdkVersion := os.Getenv("PLAYDATE_SDK_VERSION")
	if sdkVersion == "" {
		sdkVersion = "3.1.1"
	}
	env := append(os.Environ(),
		"GAME_DIR="+gameDir,
		"PLAYDATE_SDK_VERSION="+sdkVersion,
	)

	// --remove-orphans clears `docker compose run` leftovers that hold the network open.
	steps := [][]string{
		{"compose", "-f", composeFile, "--profile", profile, "down", "--remove-orphans"},
		{"compose", "-f", composeFile, "--profile", profile, "build", service},
		{"compose", "-f", composeFile, "--profile", profile, "up", "-d", service},
	}
	for _, args := range steps {
		cmd := exec.Command("docker", args...)
		cmd.Env = env
		var out strings.Builder
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("docker %s: %w\n%s", args[3], err, strings.TrimSpace(out.String()))
		}
	}
	fmt.Printf("container: recreated against %s\n", gameDir)
	return nil
}

// waitForServer retries until the container is accepting execs *and* PulseAudio
// inside it is accepting connections. The Simulator, with
// SDL_AUDIODRIVER=pulseaudio, quits at startup without it.
func waitForServer(composeFile string) error {
	deadline := time.Now().Add(90 * time.Second)
	warned := false
	for {
		// pactl talks to the same socket SDL will.
		cmd := exec.Command("docker", "compose", "-f", composeFile,
			"exec", "-T", service, "pactl", "info")
		if err := cmd.Run(); err == nil {
			return nil
		}
		if !warned && time.Since(deadline.Add(-90*time.Second)) > 10*time.Second {
			fmt.Println("waiting for audio to come up in the container")
			warned = true
		}
		if time.Now().After(deadline) {
			return errors.New("PulseAudio never came up in the container, and the " +
				"Simulator will not start without it (SDL_AUDIODRIVER=pulseaudio)")
		}
		time.Sleep(time.Second)
	}
}

// harnessTimeout is generous because a cold container starts everything at once.
const harnessTimeout = 45 * time.Second

// waitForHarness polls get_status until it reports the harness reachable. A game
// with no harness wired in is not an error.
func waitForHarness(c *client) (bool, error) {
	start := time.Now()
	const minimum = 5 * time.Second
	nextNote := 15 * time.Second

	for {
		status, err := readStatus(c)
		if err != nil {
			return false, err
		}
		if status.HarnessReachable && time.Since(start) >= minimum {
			return true, nil
		}
		if !status.Running {
			return false, errors.New("the simulator is not running.\n" +
				"  Its own console has the reason, and Lua output never reaches stdout:\n" +
				"  open http://localhost:6080/ and look at the Simulator window.")
		}
		if elapsed := time.Since(start); elapsed >= nextNote {
			fmt.Printf("waiting for the harness (%ds)\n", int(elapsed.Seconds()))
			nextNote += 15 * time.Second
		}
		if time.Since(start) > harnessTimeout {
			return false, nil
		}
		time.Sleep(500 * time.Millisecond)
	}
}

type simulatorStatus struct {
	Running          bool `json:"running"`
	HarnessReachable bool `json:"harness_reachable"`
}

func readStatus(c *client) (simulatorStatus, error) {
	out, err := c.callTool("get_status", map[string]any{})
	if err != nil {
		return simulatorStatus{}, err
	}
	var status simulatorStatus
	if err := json.Unmarshal(out, &status); err != nil {
		return simulatorStatus{}, fmt.Errorf("parsing get_status output: %w", err)
	}
	return status, nil
}

func run(composeFile string, skipSetup, keepContainer bool) error {
	if keepContainer {
		clearRunningSimulators(composeFile)
	} else {
		gameDir := os.Getenv("GAME_DIR")
		if gameDir == "" {
			return errors.New("GAME_DIR is not set.\n" +
				"  GAME_DIR=/absolute/path/to/your-game make shared-load\n" +
				"  It must be the directory that contains Source/, not Source itself.\n" +
				"  Pass -keep-container to reuse a running container instead.")
		}
		if !filepath.IsAbs(gameDir) {
			return fmt.Errorf("GAME_DIR must be an absolute path, got %q", gameDir)
		}
		if err := recreateContainer(composeFile, gameDir); err != nil {
			return err
		}
		if err := waitForServer(composeFile); err != nil {
			return err
		}
	}

	cmd := exec.Command("docker", "compose", "-f", composeFile,
		"exec", "-T", service, "open-crank-mcp")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting the server: %w", err)
	}
	// Close stdin and wait rather than kill: killing the docker client tears down
	// the exec session and takes the Simulator with it.
	defer func() {
		_ = stdin.Close()
		_, _ = cmd.Process.Wait()
	}()

	c := &client{stdin: stdin, stdout: bufio.NewReader(stdout), nextID: 0}

	if _, err := c.send("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "shared-load", "version": "1"},
	}, true); err != nil {
		// Prefer docker's error, e.g. for a container that isn't running.
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("%w\n%s", err, msg)
		}
		return err
	}
	if _, err := c.send("notifications/initialized", nil, false); err != nil {
		return err
	}

	if !skipSetup {
		out, err := c.callTool("setup", map[string]any{"source_dir": gameDir})
		if err != nil {
			return err
		}
		fmt.Printf("setup:  %s\n", out)
	}

	out, err := c.callTool("build_game", map[string]any{"source_dir": gameDir})
	if err != nil {
		return err
	}
	var built buildOutput
	if err := json.Unmarshal(out, &built); err != nil {
		return fmt.Errorf("parsing build_game output: %w", err)
	}
	if built.PdxPath == "" {
		return errors.New("build_game reported no pdx_path")
	}
	fmt.Printf("built:  %s (%s)\n", built.PdxPath, built.ProjectType)

	out, err = c.callTool("launch_simulator", map[string]any{"pdx_path": built.PdxPath})
	if err != nil {
		return err
	}
	var launched launchOutput
	if err := json.Unmarshal(out, &launched); err != nil {
		return fmt.Errorf("parsing launch_simulator output: %w", err)
	}

	reachable, err := waitForHarness(c)
	if err != nil {
		return err
	}

	// One relaunch: a cold container's first launch often loses the startup race.
	if !reachable {
		fmt.Println("harness did not answer, relaunching once")
		if _, err := c.callTool("restart_simulator", map[string]any{}); err != nil {
			return err
		}
		reachable, err = waitForHarness(c)
		if err != nil {
			return err
		}
	}

	status, err := readStatus(c)
	if err != nil {
		return err
	}
	if !status.Running {
		return errors.New("the simulator is no longer running.\n" +
			"  Its own console has the reason, and Lua output never reaches stdout:\n" +
			"  open http://localhost:6080/ and look at the Simulator window.")
	}

	fmt.Printf("running: %s\n", launched.BundleID)
	if !reachable {
		fmt.Println("harness:  not answering. The game runs, but screenshots, state")
		fmt.Println("          and input tools will time out. See the README's")
		fmt.Println("          \"Setting up a game\" section.")
	} else {
		fmt.Println("harness:  reachable")
	}
	fmt.Printf("logs:    .shared-data/%s/mcp/game_logs.jsonl\n", launched.BundleID)
	fmt.Println("view:    http://localhost:6080/")
	return nil
}

func main() {
	composeFile := flag.String("compose-file", "docker-compose.yml", "path to this repo's docker-compose.yml")
	skipSetup := flag.String("skip-setup", "", "set to any value to skip the setup tool, which rewrites the harness in your game's source")
	keepContainer := flag.Bool("keep-container", false, "reuse the running container instead of replacing it; faster, and keeps the volume slider and VNC connection, but keeps whatever GAME_DIR it was started with")
	flag.Parse()

	if err := run(*composeFile, *skipSetup != "", *keepContainer); err != nil {
		fmt.Fprintf(os.Stderr, "shared-load: %v\n", err)
		os.Exit(1)
	}
}
