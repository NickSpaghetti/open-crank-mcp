// Package contracttest builds the C and Lua fixture games and drives them
// through a real PlaydateSimulator, verifying the harness protocol against
// the actual SDK rather than a fake. Skipped unless PLAYDATE_SDK_PATH is
// set (i.e. unless run inside the full simulator Docker image) - the
// plain `go test ./...` job never has that environment.
package contracttest

import (
	"bytes"
	"encoding/json"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NickSpaghetti/open-crank-mcp/internal/build"
	"github.com/NickSpaghetti/open-crank-mcp/internal/harness"
	"github.com/NickSpaghetti/open-crank-mcp/internal/screenshot"
	"github.com/NickSpaghetti/open-crank-mcp/internal/simulator"
	"github.com/NickSpaghetti/open-crank-mcp/internal/tools"
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
	luaPdx := buildLuaFixture(t, repoRoot)

	t.Run("C harness", func(t *testing.T) {
		// The fixture creates one sprite with a collide rect and one
		// without. querySpritesInRect (the C API's only bulk sprite query)
		// only matches sprites with a collide rect set, so only the
		// collidable one should show up here, and entities_complete must
		// be false - proving the approximation's documented limitation is
		// real, not just a design note.
		runContractCheck(t, sdkPath, cPdx, "dev.open-crank-mcp.contractcheck", 1, false, false)
	})
	t.Run("Lua harness", func(t *testing.T) {
		// getAllSprites() is a true, complete enumeration - both the
		// fixture's sprites should show up regardless of collide rects.
		runContractCheck(t, sdkPath, luaPdx, "dev.open-crank-mcp.contractchecklua", 2, true, true)
	})
}

func runContractCheck(t *testing.T, sdkPath, pdxPath, bundleID string, wantEntityCount int, wantEntitiesComplete, checkGameLogs bool) {
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

	// a_down_count/a_up_count are persistent counters, not the raw
	// pushed/released bits (which are one-frame-only and this query is a
	// separate round trip from the press/release that caused them, so it
	// usually lands several frames later) - proving press_button now
	// synthesizes a real edge, not just "currently held", in both
	// harnesses. See mcp_override_update_edges (C) / updateButtonEdges
	// (Lua). A short sleep first guarantees this query itself doesn't
	// land on the exact same frame the release's edge first appears -
	// the C fixture's own counter increments after mcp_harness_update
	// returns, so report_state (invoked from inside that same call) would
	// otherwise sometimes read the counter one increment too early.
	time.Sleep(300 * time.Millisecond)
	mustSend(t, dataDir, map[string]any{"id": "4b", "type": "state"})
	resp = mustReceive(t, dataDir)
	assertStateField(t, resp, "a_down_count", float64(1))
	assertStateField(t, resp, "a_up_count", float64(1))

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

	mustSend(t, dataDir, map[string]any{"id": "8", "type": "entities"})
	resp = mustReceive(t, dataDir)
	assertEqual(t, resp, "entities_complete", wantEntitiesComplete)
	entities, ok := resp["entities"].([]any)
	if !ok {
		t.Fatalf(`response["entities"] is not an array: %v (full response: %v)`, resp["entities"], resp)
	}
	if len(entities) != wantEntityCount {
		t.Fatalf("entities has %d entries, want %d: %v", len(entities), wantEntityCount, entities)
	}

	if checkGameLogs {
		checkGameLogsContract(t, dataDir)
	}
}

// checkGameLogsContract exercises get_game_logs against the real Lua fixture
// (lua/test-fixture/Source/main.lua): a print() call at init should already
// be captured, and triggering the fixture's deliberate error (via the
// "crank" command with its magic sentinel angle) should both surface a
// traceback AND prove mcp.run() kept the harness alive - the whole point of
// this fix, not just the log text. Reads mcp/game_logs.json directly, the
// same direct-file-access path get_game_logs itself uses (see
// internal/tools/gamelogs.go), rather than going through a command/response
// round trip.
func checkGameLogsContract(t *testing.T, dataDir string) {
	t.Helper()
	logsPath := filepath.Join(dataDir, "mcp", "game_logs.json")

	entries := waitForGameLogEntry(t, logsPath, "print", "fixture print line")
	if entries == nil {
		t.Fatalf("game_logs.json never contained the fixture's print() line")
	}

	mustSend(t, dataDir, map[string]any{
		"id": "9", "type": "crank",
		"crank_angle": 999999.0, "crank_delta": 0.0, "crank_docked": false, "duration_ms": 10000,
	})
	mustReceive(t, dataDir)

	if waitForGameLogEntry(t, logsPath, "error", "deliberate fixture error") == nil {
		t.Fatalf("game_logs.json never contained the fixture's deliberate error traceback")
	}

	// The real assertion: the harness's own polling loop must still be
	// alive after that uncaught error, not frozen along with the game's
	// own broken frame logic.
	mustSend(t, dataDir, map[string]any{"id": "10", "type": "ping"})
	resp := mustReceive(t, dataDir)
	assertEqual(t, resp, "status", "ok")
}

func waitForGameLogEntry(t *testing.T, path, wantType, wantMessageContains string) []tools.GameLogEntry {
	t.Helper()
	deadline := time.Now().Add(mcpDirTimeout)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(path)
		if err == nil {
			var entries []tools.GameLogEntry
			if err := json.Unmarshal(b, &entries); err == nil {
				for _, e := range entries {
					if e.Type == wantType && bytes.Contains([]byte(e.Message), []byte(wantMessageContains)) {
						return entries
					}
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil
}

func buildCFixture(t *testing.T, repoRoot string) string {
	t.Helper()
	fixtureDir := filepath.Join(repoRoot, "c-harness", "test", "fixture-game")
	srcDir := filepath.Join(fixtureDir, "src")

	copyFile(t, filepath.Join(repoRoot, "c-harness", "mcp_harness.h"), filepath.Join(srcDir, "mcp_harness.h"))
	copyFile(t, filepath.Join(repoRoot, "c-harness", "mcp_harness.c"), filepath.Join(srcDir, "mcp_harness.c"))

	result, err := build.Build(fixtureDir)
	if err != nil {
		t.Fatalf("build.Build: %v\n%s", err, result.Output)
	}
	return result.PdxPath
}

func buildLuaFixture(t *testing.T, repoRoot string) string {
	t.Helper()
	fixtureDir := filepath.Join(repoRoot, "lua", "test-fixture")
	sourceDir := filepath.Join(fixtureDir, "Source")

	copyFile(t, filepath.Join(repoRoot, "lua", "mcp_harness.lua"), filepath.Join(sourceDir, "mcp_harness.lua"))

	result, err := build.Build(fixtureDir)
	if err != nil {
		t.Fatalf("build.Build: %v\n%s", err, result.Output)
	}
	return result.PdxPath
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

// assertRawScreenshot checks both the raw dump's size and, decoded, its
// content. The fixture clears its display to kColorBlack at init and never
// draws anything else, so the decoded image should be entirely black. This
// is what pins down internal/screenshot's undocumented bit polarity against
// the real Simulator, not just self-consistent synthetic input.
func assertRawScreenshot(t *testing.T, dataDir string) {
	t.Helper()
	path := filepath.Join(dataDir, "mcp", "screenshot.raw")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("expected raw screenshot at %s: %v", path, err)
		return
	}
	if int64(len(raw)) != rawScreenshotSize {
		t.Errorf("raw screenshot is %d bytes, want %d (LCD_ROWS*LCD_ROWSIZE)", len(raw), rawScreenshotSize)
		return
	}

	pngBytes, err := screenshot.DecodeRawToPNG(raw)
	if err != nil {
		t.Errorf("DecodeRawToPNG: %v", err)
		return
	}
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("png.Decode: %v", err)
	}
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := color.GrayModel.Convert(img.At(x, y)).(color.Gray)
			if c.Y != 0 {
				t.Fatalf("pixel (%d,%d) = %d, want 0 (black). The fixture clears to kColorBlack and never draws anything else.", x, y, c.Y)
			}
		}
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
