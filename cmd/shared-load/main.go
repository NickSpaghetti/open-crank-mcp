// Command shared-load builds and launches a game in an already-running shared
// container, by driving the MCP server the same way a client would.
//
// It exists because starting the container and loading a game are two separate
// things, and only the first one had a command. `make up-shared` gets you a
// container with a display; something still has to call setup, build_game and
// launch_simulator, and without this that meant either connecting a real MCP
// client or hand-rolling JSON-RPC.
//
// The transport is the same one a client uses: stdio, over
// `docker compose exec -T simulator-shared open-crank-mcp`. No ports, no HTTP.
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

// Where docker-compose.yml mounts GAME_DIR. Fixed by the shared service, not
// something the caller chooses.
const gameDir = "/your-game"

// The compose service and profile this drives. Named once rather than repeated
// at each `docker compose` call site: they appeared eight times as bare
// literals across three functions before, which is how a rename leaves a
// half-finished one behind.
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

// The subset of each tool's output this needs. Everything else is passed
// through to the caller as the raw JSON it came in as.
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

	// Skipping anything that isn't the reply we're waiting for: the server is
	// free to emit notifications, and a mismatched id is not an error.
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
// launch_simulator has no already-running guard, and it can't have a useful one:
// server state lives in the connection, so a fresh one has no idea a previous
// session left a Simulator behind. Launching anyway gives two Simulators on one
// display, both running the same .pdx, so both harnesses poll the same
// mcp/command.json and a tool response can come back from either.
//
// SIGKILL because the Simulator ignores SIGTERM, which is also why the server's
// own stop_simulator uses it. Server processes are deliberately left alone:
// killing every open-crank-mcp would disconnect an agent that is connected right
// now, and an idle server owns nothing once its Simulator is gone.
func clearRunningSimulators(composeFile string) {
	// pkill exits 0 when it killed something, 1 when there was nothing to kill.
	cmd := exec.Command("docker", "compose", "-f", composeFile,
		"exec", "-T", service, "pkill", "-9", "-f", "bin/PlaydateSimulator")
	if err := cmd.Run(); err == nil {
		fmt.Println("stopped: a Simulator was already running")
	}
}

// recreateContainer tears the shared container down and brings it back up against
// the current GAME_DIR.
//
// Replacing it rather than reusing it, because GAME_DIR is fixed when the
// container starts: a container left over from a different game keeps serving
// that game, and every tool then reports confidently about the wrong thing.
// The failure is quiet and confusing - either "no Playdate project in
// /your-game" or, worse, a successful build of a game you weren't asking about.
//
// The image is rebuilt too. Plain `docker compose build` skips services that
// declare a profile, so an edit to run-vnc.sh or the Dockerfile would otherwise
// be silently absent from the container this just started.
func recreateContainer(composeFile, gameDir string) error {
	sdkVersion := os.Getenv("PLAYDATE_SDK_VERSION")
	if sdkVersion == "" {
		sdkVersion = "3.1.1"
	}
	env := append(os.Environ(),
		"GAME_DIR="+gameDir,
		"PLAYDATE_SDK_VERSION="+sdkVersion,
	)

	// --remove-orphans also clears containers left behind by `docker compose
	// run`, which hold the project's network open and make `down` complain.
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
// inside it is accepting connections.
//
// Both halves matter. A container reports as started before it can be exec'd,
// and it can be exec'd well before run-vnc.sh has PulseAudio up. The Simulator
// runs with SDL_AUDIODRIVER=pulseaudio and refuses to start at all without it:
//
//	SDL2 could not be initalized (-1 - Could not setup connection to PulseAudio).
//	SDL2 is required for the Playdate Simulator to run and it will now quit.
//
// That is a game that never appears on a freshly started container and works
// perfectly on a warm one, which reads as flakiness rather than as a missing
// dependency.
func waitForServer(composeFile string) error {
	deadline := time.Now().Add(90 * time.Second)
	warned := false
	for {
		// pactl talks to the same socket SDL will, so this is the real check
		// rather than a proxy for it.
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

// harnessTimeout is generous on purpose. A cold container has Xvfb, openbox and
// pulseaudio starting, a first-time build, and a game loading its assets, all at
// once. 20 seconds was enough on a warm container and not on a cold one, which
// is the difference between "your game works" and a warning that reads like a
// broken harness.
const harnessTimeout = 45 * time.Second

// waitForHarness polls get_status until the game's harness answers. Returns
// whether it answered; a game with no harness wired in is not an error.
func waitForHarness(c *client) (bool, error) {
	// A floor as well as a poll. get_status can report the harness reachable
	// immediately, because the IPC it checks is a file in the data directory and
	// a previous run's response can still be sitting there.
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
	// Closed, then waited for. Both halves matter, and it took measuring to
	// find out: killing the docker client instead tears down the exec session
	// and takes the Simulator with it, so the game dies about a second after
	// this command reports it running. Letting the client exit on its own
	// leaves the Simulator parented to the container's init, which is what
	// keeps it alive and on screen afterwards.
	//
	// The server itself has no shutdown hook, so an EOF on stdin just ends it.
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
		// A container that isn't running fails here, and the docker error is
		// more useful than the read error that surfaces it.
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("%w\n%s", err, msg)
		}
		return err
	}
	if _, err := c.send("notifications/initialized", nil, false); err != nil {
		return err
	}

	// Skippable because it rewrites the harness file in your game's source
	// tree. Worth doing once per game, less welcome on every load.
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

	// One relaunch if it never answered. On a cold container the first launch
	// competes with everything else starting up, and a second attempt usually
	// answers immediately.
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

	// Never report a game as running when its process is gone. That is the
	// failure that sent someone looking at harness wiring for a game whose
	// harness was fine.
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
