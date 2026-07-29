// Command byos-load builds and launches a game in an already-running byos
// container, by driving the MCP server the same way a client would.
//
// It exists because starting the container and loading a game are two separate
// things, and only the first one had a command. `make up-byos` gets you a
// container with a display; something still has to call setup, build_game and
// launch_simulator, and without this that meant either connecting a real MCP
// client or hand-rolling JSON-RPC.
//
// The transport is the same one a client uses: stdio, over
// `docker compose exec -T simulator-byos open-crank-mcp`. No ports, no HTTP.
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
	"strings"
	"time"
)

// Where docker-compose.yml mounts GAME_DIR. Fixed by the byos service, not
// something the caller chooses.
const gameDir = "/your-game"

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

// clearRunningSimulators kills any Simulator already running in the container
// before a new one is launched.
//
// launch_simulator has no already-running guard, and it can't have a useful one
// here: server state lives in the connection, so a fresh one has no idea a
// previous session left a Simulator behind. Launching anyway gives two
// Simulators on one display, both running the same .pdx, so both harnesses poll
// the same mcp/command.json and a tool response can come back from either.
//
// SIGKILL because the Simulator ignores SIGTERM, which is also why the server's
// own stop_simulator uses it.
func clearRunningSimulators(composeFile string) {
	// pkill exits 0 when it killed something, 1 when there was nothing to kill.
	cmd := exec.Command("docker", "compose", "-f", composeFile,
		"exec", "-T", "simulator-byos", "pkill", "-9", "-f", "bin/PlaydateSimulator")
	if err := cmd.Run(); err == nil {
		fmt.Println("stopped: a Simulator was already running")
	}

	// Server processes are deliberately left alone. Killing every
	// open-crank-mcp in the container would also disconnect an agent that is
	// connected right now, and an idle server owns nothing once its Simulator
	// is gone. Only the Simulator is worth clearing, because two of those on
	// one display is a genuine conflict.
}

// waitForHarness polls get_status until the game's harness answers, which is
// the point at which the Simulator is fully up.
func waitForHarness(c *client) error {
	// A floor as well as a poll. get_status can report the harness reachable
	// immediately, because the IPC it checks is a file in the data directory and
	// a previous run's response can still be sitting there. Exiting on that
	// answer kills a Simulator that is half a second old, so hold on regardless
	// of what the first few polls claim.
	start := time.Now()
	const minimum = 5 * time.Second

	deadline := time.Now().Add(20 * time.Second)
	for {
		out, err := c.callTool("get_status", map[string]any{})
		if err != nil {
			return err
		}
		var status struct {
			Running          bool `json:"running"`
			HarnessReachable bool `json:"harness_reachable"`
		}
		if err := json.Unmarshal(out, &status); err != nil {
			return fmt.Errorf("parsing get_status output: %w", err)
		}
		if status.HarnessReachable && time.Since(start) >= minimum {
			return nil
		}
		if !status.Running {
			return errors.New("the simulator exited during startup")
		}
		if time.Now().After(deadline) {
			// Running but silent means the game is up and the harness isn't
			// wired in, which is a game problem rather than a launch failure.
			fmt.Println("warning: the simulator is running but its harness never answered")
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func run(composeFile string, skipSetup bool) error {
	clearRunningSimulators(composeFile)

	cmd := exec.Command("docker", "compose", "-f", composeFile,
		"exec", "-T", "simulator-byos", "open-crank-mcp")
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
		"clientInfo":      map[string]any{"name": "byos-load", "version": "1"},
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

	// Waiting for the harness before exiting, and this is load-bearing rather
	// than cosmetic. The Simulator's stdout is a pipe held by this server, so
	// exiting the moment launch_simulator returns closes that pipe while the
	// Simulator is still writing its startup output, and it dies about a second
	// later - a game that launches and immediately disappears. Staying until
	// the harness answers gets it past startup, after which it survives on its
	// own.
	if err := waitForHarness(c); err != nil {
		return err
	}

	fmt.Printf("running: %s\n", launched.BundleID)
	fmt.Printf("logs:    .byos-data/%s/mcp/game_logs.json\n", launched.BundleID)
	fmt.Println("view:    http://localhost:6080/")
	return nil
}

func main() {
	composeFile := flag.String("compose-file", "docker-compose.yml", "path to this repo's docker-compose.yml")
	skipSetup := flag.String("skip-setup", "", "set to any value to skip the setup tool, which rewrites the harness in your game's source")
	flag.Parse()

	if err := run(*composeFile, *skipSetup != ""); err != nil {
		fmt.Fprintf(os.Stderr, "byos-load: %v\n", err)
		os.Exit(1)
	}
}
