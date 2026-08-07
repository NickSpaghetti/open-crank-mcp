package tools

import (
	"bytes"
	"encoding/json"
	"github.com/NickSpaghetti/open-crank-mcp/internal/sdk"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NickSpaghetti/open-crank-mcp/internal/simulator"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newTestServer builds a Server backed by a trivial stand-in process (not a
// real PlaydateSimulator), the same approach internal/simulator's own tests
// use. sdkPath points at a scratch directory whose bin/PlaydateSimulator is
// actually `sh` (via a symlink), so code paths that relaunch through
// s.sdkPath (restart_simulator) exercise a real, working Launch call instead
// of just an error path.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	sim, err := simulator.Launch("sh", "-c", "sleep 30")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	t.Cleanup(func() {
		_ = sim.Stop()
		_ = sim.Wait()
	})

	sdkPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sdkPath, "bin"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	shPath, err := exec.LookPath("sh")
	if err != nil {
		t.Fatalf("LookPath(sh): %v", err)
	}
	if err := os.Symlink(shPath, filepath.Join(sdkPath, "bin", "PlaydateSimulator")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	dataDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataDir, "mcp"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	return &Server{
		// The symlinked stand-in is named directly rather than derived from a
		// layout: this fixture is about the Server's lifecycle handling, not about
		// where a real SDK keeps its Simulator.
		sdk: sdk.Paths{
			Root:         sdkPath,
			SimulatorBin: filepath.Join(sdkPath, "bin", "PlaydateSimulator"),
			PDC:          filepath.Join(sdkPath, "bin", "pdc"),
		},
		sim: sim,
		// Empty pdxPath means "launch with no game" (a supported mode -
		// see internal/simulator.Launch) - sh then just gets dataDir as
		// its lone arg, which it errors on internally trying to read as a
		// script, but Launch() itself still succeeds since the process
		// starts fine. That's all restart_simulator's success path needs.
		pdxPath:  "",
		dataDir:  dataDir,
		bundleID: "com.example.test",
	}
}

// startFakeHarness watches dataDir/mcp/command.json and, once it appears,
// deletes it and writes resp as response.json - mimicking what a real C/Lua
// harness's own update loop does, without needing either.
func startFakeHarness(t *testing.T, dataDir string, resp map[string]any) {
	t.Helper()
	mcpDir := filepath.Join(dataDir, "mcp")
	cmdPath := filepath.Join(mcpDir, "command.json")
	respPath := filepath.Join(mcpDir, "response.json")

	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(cmdPath); err == nil {
				_ = os.Remove(cmdPath)
				b, _ := json.Marshal(resp)
				_ = os.WriteFile(respPath, b, 0o644)
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
}

// startEchoingFakeHarness continuously watches dataDir/mcp/command.json and,
// for each one seen, echoes its "id" field back as response.json before
// deleting the command file - unlike startFakeHarness's single canned
// response, this handles many command/response cycles, needed to exercise
// concurrent roundTrip calls against the same fixed filenames. A short sleep
// before writing the response widens the window a missing lock would need to
// actually race in. Runs until the test finishes.
func startEchoingFakeHarness(t *testing.T, dataDir string) {
	t.Helper()
	mcpDir := filepath.Join(dataDir, "mcp")
	cmdPath := filepath.Join(mcpDir, "command.json")
	respPath := filepath.Join(mcpDir, "response.json")

	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })

	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			b, err := os.ReadFile(cmdPath)
			if err != nil || len(b) == 0 {
				time.Sleep(time.Millisecond)
				continue
			}
			var cmd map[string]any
			if err := json.Unmarshal(b, &cmd); err != nil {
				time.Sleep(time.Millisecond)
				continue
			}
			_ = os.Remove(cmdPath)
			time.Sleep(time.Millisecond)
			respBytes, _ := json.Marshal(map[string]any{"status": "ok", "id": cmd["id"]})
			_ = os.WriteFile(respPath, respBytes, 0o644)
		}
	}()
}

// renderContent flattens a tool result's text blocks, for asserting on the
// message a recoverable failure hands back. Those messages are load-bearing here
// rather than decoration - several of them are the only place a caller learns
// what to do about the condition - so tests assert on their content, not just on
// IsError.
func renderContent(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	var b strings.Builder
	for _, c := range result.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// recordedCommands is what startRecordingFakeHarness collects.
type recordedCommands struct {
	mu  sync.Mutex
	raw [][]byte
}

func (r *recordedCommands) add(b []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.raw = append(r.raw, bytes.Clone(b))
}

func (r *recordedCommands) all() [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]byte(nil), r.raw...)
}

// startRecordingFakeHarness answers commands like startEchoingFakeHarness does and
// also keeps every command JSON it saw.
//
// Needed because the interesting property for some tests is what the server put on
// the wire, not what came back - and the handler has no return value that reveals
// it. Asserting on the real bytes is the only way to catch a field that was meant
// to be sent and was not.
func startRecordingFakeHarness(t *testing.T, dataDir string) *recordedCommands {
	t.Helper()
	mcpDir := filepath.Join(dataDir, "mcp")
	cmdPath := filepath.Join(mcpDir, "command.json")
	respPath := filepath.Join(mcpDir, "response.json")

	rec := &recordedCommands{}
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })

	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			b, err := os.ReadFile(cmdPath)
			if err != nil || len(b) == 0 {
				time.Sleep(time.Millisecond)
				continue
			}
			var cmd map[string]any
			if err := json.Unmarshal(b, &cmd); err != nil {
				time.Sleep(time.Millisecond)
				continue
			}
			rec.add(b)
			_ = os.Remove(cmdPath)
			respBytes, _ := json.Marshal(map[string]any{"status": "ok", "id": cmd["id"]})
			_ = os.WriteFile(respPath, respBytes, 0o644)
		}
	}()
	return rec
}
