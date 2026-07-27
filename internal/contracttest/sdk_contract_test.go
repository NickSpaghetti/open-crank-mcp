// Package contracttest builds the C and Lua fixture games and drives them
// through a real PlaydateSimulator, verifying the harness protocol against
// the actual SDK rather than a fake. Skipped unless PLAYDATE_SDK_PATH is
// set (i.e. unless run inside the full simulator Docker image) - the
// plain `go test ./...` job never has that environment.
package contracttest

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/NickSpaghetti/open-crank-mcp/internal/harness"
	"github.com/NickSpaghetti/open-crank-mcp/internal/simulator"
)

const (
	responseTimeout   = 5 * time.Second
	mcpDirTimeout     = 10 * time.Second
	rawScreenshotSize = 240 * 52 // LCD_ROWS * LCD_ROWSIZE
)

var pngMagic = []byte{0x89, 0x50, 0x4E, 0x47}

func TestSDKContract(t *testing.T) {
	sdkPath := os.Getenv("PLAYDATE_SDK_PATH")
	if sdkPath == "" {
		t.Skip("PLAYDATE_SDK_PATH not set - run inside the simulator image (make sdk-contract-check)")
	}

	repoRoot := findRepoRoot(t)

	xvfb, err := simulator.Launch("Xvfb", ":99", "-screen", "0", "1280x800x24")
	if err != nil {
		t.Fatalf("launching Xvfb: %v", err)
	}
	defer func() {
		_ = xvfb.Stop()
		_ = xvfb.Wait()
	}()
	time.Sleep(1 * time.Second)
	if err := os.Setenv("DISPLAY", ":99"); err != nil {
		t.Fatalf("setting DISPLAY: %v", err)
	}

	cPdx := buildCFixture(t, repoRoot)
	luaPdx := buildLuaFixture(t, repoRoot, sdkPath)

	t.Run("C harness", func(t *testing.T) {
		runContractCheck(t, sdkPath, cPdx, "dev.open-crank-mcp.contractcheck")
	})
	t.Run("Lua harness", func(t *testing.T) {
		runContractCheck(t, sdkPath, luaPdx, "dev.open-crank-mcp.contractchecklua")
	})
}

func runContractCheck(t *testing.T, sdkPath, pdxPath, bundleID string) {
	t.Helper()
	dataDir := filepath.Join(sdkPath, "Disk", "Data", bundleID)
	defer os.RemoveAll(dataDir)

	simBin := filepath.Join(sdkPath, "bin", "PlaydateSimulator")
	sim, err := simulator.Launch(simBin, pdxPath, dataDir)
	if err != nil {
		t.Fatalf("launching simulator: %v", err)
	}
	defer func() {
		_ = sim.Stop()
		_ = sim.Wait()
	}()

	if err := harness.WaitForDir(filepath.Join(dataDir, "mcp"), mcpDirTimeout); err != nil {
		t.Fatalf("waiting for mcp/ directory: %v\nsimulator output:\n%s", err, sim.Output())
	}

	mustSend(t, dataDir, map[string]any{"id": "1", "type": "ping"})
	resp := mustReceive(t, dataDir)
	assertEqual(t, resp, "status", "ok")

	mustSend(t, dataDir, map[string]any{"id": "2", "type": "press", "button": "a", "duration_ms": 10000})
	mustReceive(t, dataDir)

	mustSend(t, dataDir, map[string]any{"id": "3", "type": "state"})
	resp = mustReceive(t, dataDir)
	assertStateField(t, resp, "current", float64(32)) // kButtonA bit

	mustSend(t, dataDir, map[string]any{"id": "4", "type": "release", "button": "a", "duration_ms": 10000})
	mustReceive(t, dataDir)

	mustSend(t, dataDir, map[string]any{
		"id": "5", "type": "crank",
		"crank_angle": 123.0, "crank_delta": 5.0, "crank_docked": true, "duration_ms": 10000,
	})
	mustReceive(t, dataDir)

	mustSend(t, dataDir, map[string]any{"id": "6", "type": "state"})
	resp = mustReceive(t, dataDir)
	assertStateField(t, resp, "crank_angle", 123.0)
	assertStateField(t, resp, "crank_docked", true)

	mustSend(t, dataDir, map[string]any{"id": "7", "type": "screenshot"})
	resp = mustReceive(t, dataDir)
	assertEqual(t, resp, "width", float64(400))
	assertEqual(t, resp, "height", float64(240))

	switch resp["format"] {
	case "raw":
		assertRawScreenshot(t, dataDir)
	case "png":
		assertPNGScreenshot(t, dataDir)
	default:
		t.Errorf("response had neither raw nor png format: %v", resp)
	}
}

func buildCFixture(t *testing.T, repoRoot string) string {
	t.Helper()
	fixtureDir := filepath.Join(repoRoot, "c-harness", "test", "fixture-game")
	srcDir := filepath.Join(fixtureDir, "src")

	copyFile(t, filepath.Join(repoRoot, "c-harness", "mcp_harness.h"), filepath.Join(srcDir, "mcp_harness.h"))
	copyFile(t, filepath.Join(repoRoot, "c-harness", "mcp_harness.c"), filepath.Join(srcDir, "mcp_harness.c"))

	runCommand(t, fixtureDir, "cmake", "-S", ".", "-B", "build")
	runCommand(t, fixtureDir, "cmake", "--build", "build")

	return filepath.Join(fixtureDir, "mcp_contract_check.pdx")
}

func buildLuaFixture(t *testing.T, repoRoot, sdkPath string) string {
	t.Helper()
	fixtureDir := filepath.Join(repoRoot, "lua", "test-fixture")
	sourceDir := filepath.Join(fixtureDir, "Source")

	copyFile(t, filepath.Join(repoRoot, "lua", "mcp_harness.lua"), filepath.Join(sourceDir, "mcp_harness.lua"))

	pdxPath := filepath.Join(fixtureDir, "mcp_contract_check_lua.pdx")
	pdcBin := filepath.Join(sdkPath, "bin", "pdc")
	runCommand(t, repoRoot, pdcBin, "-sdkpath", sdkPath, sourceDir, pdxPath)

	return pdxPath
}

func mustSend(t *testing.T, dataDir string, cmd map[string]any) {
	t.Helper()
	if err := harness.SendCommand(dataDir, cmd); err != nil {
		t.Fatalf("SendCommand: %v", err)
	}
}

func mustReceive(t *testing.T, dataDir string) map[string]any {
	t.Helper()
	resp, err := harness.WaitForResponse(dataDir, responseTimeout)
	if err != nil {
		t.Fatalf("WaitForResponse: %v", err)
	}
	return resp
}

func assertEqual(t *testing.T, resp map[string]any, field string, want any) {
	t.Helper()
	got := resp[field]
	if got != want {
		t.Errorf("response[%q] = %v (%T), want %v (%T)\nfull response: %v", field, got, got, want, want, resp)
	}
}

func assertStateField(t *testing.T, resp map[string]any, field string, want any) {
	t.Helper()
	state, ok := resp["state"].(map[string]any)
	if !ok {
		t.Fatalf(`response["state"] is not an object: %v (full response: %v)`, resp["state"], resp)
	}
	got := state[field]
	if got != want {
		t.Errorf("state[%q] = %v (%T), want %v (%T)\nfull state: %v", field, got, got, want, want, state)
	}
}

func assertRawScreenshot(t *testing.T, dataDir string) {
	t.Helper()
	path := filepath.Join(dataDir, "mcp", "screenshot.raw")
	info, err := os.Stat(path)
	if err != nil {
		t.Errorf("expected raw screenshot at %s: %v", path, err)
		return
	}
	if info.Size() != rawScreenshotSize {
		t.Errorf("raw screenshot is %d bytes, want %d (LCD_ROWS*LCD_ROWSIZE)", info.Size(), rawScreenshotSize)
	}
}

func assertPNGScreenshot(t *testing.T, dataDir string) {
	t.Helper()
	path := filepath.Join(dataDir, "mcp", "screenshot.png")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("expected png screenshot at %s: %v", path, err)
		return
	}
	if len(b) < len(pngMagic) || !bytes.Equal(b[:len(pngMagic)], pngMagic) {
		got := b
		if len(got) > len(pngMagic) {
			got = got[:len(pngMagic)]
		}
		t.Errorf("png screenshot at %s does not start with the PNG magic number, got %x", path, got)
	}
}

func runCommand(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("reading %s: %v", src, err)
	}
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		t.Fatalf("writing %s: %v", dst, err)
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (no go.mod in any parent directory)")
		}
		dir = parent
	}
}
