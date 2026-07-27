package tools

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/NickSpaghetti/open-crank-mcp/internal/simulator"
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
		sdkPath: sdkPath,
		sim:     sim,
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
