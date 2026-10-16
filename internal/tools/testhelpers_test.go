package tools

import (
	"bytes"
	"encoding/json"
	"github.com/NickSpaghetti/open-crank-mcp/internal/sdk"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NickSpaghetti/open-crank-mcp/internal/simulator"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func testShell(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-backed tool fixtures run only on Unix; Windows is covered by the native contract test")
	}
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("these tool tests use sh as a fake Simulator, which is not available on Windows")
	}
	return shell
}

// newTestServer builds a Server backed by a stand-in `sh` process. The SDK's
// Simulator is a symlink to sh, so restart_simulator exercises a real Launch.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	shPath := testShell(t)
	sim, err := simulator.Launch(shPath, "-c", "sleep 30")
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
	if err := os.Symlink(shPath, filepath.Join(sdkPath, "bin", "PlaydateSimulator")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	dataDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataDir, "mcp"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	return &Server{
		sdk: sdk.Paths{
			Root:         sdkPath,
			SimulatorBin: filepath.Join(sdkPath, "bin", "PlaydateSimulator"),
			PDC:          filepath.Join(sdkPath, "bin", "pdc"),
		},
		sim: sim,
		// Empty pdxPath launches with no game: sh gets the scratch dir as a script
		// path and fails, but the process starts, which is all restart needs.
		pdxPath:  "",
		dataDir:  dataDir,
		bundleID: "com.example.test",
	}
}

// startFakeHarness answers one command.json with resp, as a real harness would.
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

// startCapturingFakeHarness answers one command like startFakeHarness and
// returns what it was sent.
func startCapturingFakeHarness(t *testing.T, dataDir string, resp map[string]any) <-chan map[string]any {
	t.Helper()
	mcpDir := filepath.Join(dataDir, "mcp")
	cmdPath := filepath.Join(mcpDir, "command.json")
	respPath := filepath.Join(mcpDir, "response.json")

	got := make(chan map[string]any, 1)
	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			b, err := os.ReadFile(cmdPath)
			if err != nil || len(b) == 0 {
				time.Sleep(5 * time.Millisecond)
				continue
			}
			var cmd map[string]any
			if err := json.Unmarshal(b, &cmd); err != nil {
				time.Sleep(5 * time.Millisecond)
				continue
			}
			_ = os.Remove(cmdPath)
			got <- cmd
			respBytes, _ := json.Marshal(resp)
			_ = os.WriteFile(respPath, respBytes, 0o644)
			return
		}
		close(got)
	}()
	return got
}

// startEchoingFakeHarness answers every command by echoing its id, for
// concurrent roundTrip tests. The sleep widens the window a missing lock would race in.
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
// message a recoverable failure hands back.
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

// startRecordingFakeHarness answers commands like startEchoingFakeHarness and
// keeps every command JSON it saw, for asserting on what went on the wire.
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
