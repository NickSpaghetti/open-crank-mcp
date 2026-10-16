// Package contracttest drives the C and Lua fixture games through a real
// PlaydateSimulator. Skipped unless OPEN_CRANK_SDK_CONTRACT is set.
package contracttest

import (
	"bytes"
	"encoding/json"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	opencrank "github.com/NickSpaghetti/open-crank-mcp"
	"github.com/NickSpaghetti/open-crank-mcp/internal/build"
	"github.com/NickSpaghetti/open-crank-mcp/internal/harness"
	"github.com/NickSpaghetti/open-crank-mcp/internal/screenshot"
	"github.com/NickSpaghetti/open-crank-mcp/internal/sdk"
	"github.com/NickSpaghetti/open-crank-mcp/internal/setup"
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
	if os.Getenv("OPEN_CRANK_SDK_CONTRACT") == "" {
		t.Skip("OPEN_CRANK_SDK_CONTRACT not set - run `make sdk-contract-check` (container) or `make sdk-contract-check-native` (host SDK)")
	}
	paths := contractSDK(t)

	repoRoot := findRepoRoot(t)

	startDisplay(t, ":99")

	cPdx := buildCFixture(t, repoRoot)
	luaPdx := buildLuaFixture(t, repoRoot)

	t.Run("C harness", func(t *testing.T) {
		// querySpritesInRect, the C API's only bulk sprite query, skips sprites
		// without a collide rect, so only one of the fixture's two shows up.
		runContractCheck(t, paths, cPdx, "dev.open-crank-mcp.contractcheck", 1, false, false)
	})
	t.Run("Lua harness", func(t *testing.T) {
		// getAllSprites() enumerates every sprite.
		runContractCheck(t, paths, luaPdx, "dev.open-crank-mcp.contractchecklua", 2, true, true)
	})
}

// Takes sdk.Paths because the Simulator binary and data dir locations are
// per-platform (inside a .app bundle on macOS).
func runContractCheck(t *testing.T, paths sdk.Paths, pdxPath, bundleID string, wantEntityCount int, wantEntitiesComplete, checkGameLogs bool) {
	t.Helper()
	// Predicted rather than probed: the directory doesn't exist until launch.
	dataDir := paths.DataDirCandidates(sdk.OSEnv(), bundleID)[0]
	defer os.RemoveAll(dataDir)

	sim, err := simulator.Launch(paths.SimulatorBin, pdxPath, dataDir)
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

	resp := mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{ID: "1", Type: harness.CmdPing}))

	// The stamped fingerprint must survive stamping, compiling and the file
	// protocol end to end. Either harness's fingerprint is accepted, as in the server.
	current, err := harness.IsCurrentFingerprint(opencrank.HarnessFS, resp.HarnessVersion)
	if err != nil {
		t.Fatalf("fingerprinting the embedded harness: %v", err)
	}
	if !current {
		t.Errorf("harness reported version %q, which is not one this build ships - "+
			"the fixture was not stamped, or stamping is broken", resp.HarnessVersion)
	}

	mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{
		ID: "2", Type: harness.CmdPress, Button: "a", DurationMs: 10000}))

	resp = mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{ID: "3", Type: harness.CmdState}))
	assertStateField(t, resp, "current", float64(32)) // kButtonA bit

	mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{
		ID: "4", Type: harness.CmdRelease, Button: "a", DurationMs: 10000}))

	// Persistent counters, since pushed/released bits last one frame. The sleep
	// avoids reading the C fixture's counter on the frame before it increments.
	time.Sleep(300 * time.Millisecond)
	resp = mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{ID: "4b", Type: harness.CmdState}))
	assertStateField(t, resp, "a_down_count", float64(1))
	assertStateField(t, resp, "a_up_count", float64(1))

	// A press with no duration holds across frames. The sleep spans several frames
	// so an override expiring at frame start would be caught.
	mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{
		ID: "4d", Type: harness.CmdPress, Button: "a"}))
	time.Sleep(300 * time.Millisecond)
	resp = mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{ID: "4e", Type: harness.CmdState}))
	assertStateField(t, resp, "current", float64(32)) // kButtonA bit, still held

	// A release ends it. Asserted on a_up_count since the released bit is one frame wide.
	before := stateMap(t, resp)["a_up_count"]
	mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{
		ID: "4f", Type: harness.CmdRelease, Button: "a", DurationMs: 200}))
	time.Sleep(300 * time.Millisecond)
	resp = mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{ID: "4g", Type: harness.CmdState}))
	assertStateField(t, resp, "current", float64(0))
	if got := stateMap(t, resp)["a_up_count"]; got == before {
		t.Errorf("a_up_count stayed at %v after releasing a held button; the game saw the "+
			"button stop being pressed with no ButtonUp callback", got)
	}

	// The real dock reading must be docked at rest, or the passthrough check below
	// can't tell a passthrough from a forced undock.
	resp = mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{ID: "4c", Type: harness.CmdState}))
	realDocked := stateMap(t, resp)["crank_docked"]
	if realDocked != true {
		t.Fatalf("the Simulator reports the crank docked=%v at rest, want true; the dock "+
			"passthrough assertions below cannot distinguish a fix from the old bug without it", realDocked)
	}

	mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{
		ID: "5", Type: harness.CmdCrank,
		CrankAngle: 123.0, CrankDelta: 5.0, CrankDock: harness.DockDocked, DurationMs: 10000}))

	resp = mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{ID: "6", Type: harness.CmdState}))
	assertStateField(t, resp, "crank_angle", 123.0)
	assertStateField(t, resp, "crank_docked", true)

	// A crank command that leaves the dock unchanged must pass the real reading through.
	mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{
		ID: "6b", Type: harness.CmdCrank,
		CrankAngle: 200.0, CrankDock: harness.DockUnchanged, DurationMs: 10000}))
	resp = mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{ID: "6c", Type: harness.CmdState}))
	assertStateField(t, resp, "crank_angle", 200.0)
	if got := stateMap(t, resp)["crank_docked"]; got != realDocked {
		t.Errorf("crank_dock=unchanged changed the dock reading from %v to %v", realDocked, got)
	}

	// Forcing undocked still overrides.
	mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{
		ID: "6d", Type: harness.CmdCrank,
		CrankAngle: 200.0, CrankDock: harness.DockUndocked, DurationMs: 10000}))
	resp = mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{ID: "6e", Type: harness.CmdState}))
	assertStateField(t, resp, "crank_docked", false)

	// A crank with no duration holds across frames; the sleep spans several so an
	// override expiring at frame start would be caught.
	mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{
		ID: "6f", Type: harness.CmdCrank, CrankAngle: 42.0, CrankDock: harness.DockUnchanged}))
	time.Sleep(300 * time.Millisecond)
	resp = mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{ID: "6g", Type: harness.CmdState}))
	assertStateField(t, resp, "crank_angle", 42.0)

	// A later crank replaces a held one and expires normally, restoring passthrough.
	mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{
		ID: "6h", Type: harness.CmdCrank,
		CrankAngle: 300.0, CrankDock: harness.DockUnchanged, DurationMs: 200}))
	time.Sleep(600 * time.Millisecond)
	resp = mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{ID: "6i", Type: harness.CmdState}))
	if got := stateMap(t, resp)["crank_angle"]; got == 300.0 || got == 42.0 {
		t.Errorf("crank_angle=%v after the replacing override should have expired; "+
			"a duration-less crank is meant to be replaceable, not permanent", got)
	}

	// reset is the only way to release a held crank (and held buttons). Compared
	// against a captured baseline, not a hardcoded value.
	resp = mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{ID: "6j", Type: harness.CmdState}))
	realAngle := stateMap(t, resp)["crank_angle"]

	mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{
		ID: "6k", Type: harness.CmdCrank, CrankAngle: 271.0, CrankDock: harness.DockUndocked}))
	mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{
		ID: "6l", Type: harness.CmdPress, Button: "a"}))
	time.Sleep(300 * time.Millisecond)
	resp = mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{ID: "6m", Type: harness.CmdState}))
	assertStateField(t, resp, "crank_angle", 271.0)
	assertStateField(t, resp, "current", float64(32))

	mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{ID: "6n", Type: harness.CmdReset}))
	time.Sleep(300 * time.Millisecond)
	resp = mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{ID: "6o", Type: harness.CmdState}))
	if got := stateMap(t, resp)["crank_angle"]; got != realAngle {
		t.Errorf("crank_angle=%v after reset, want the real reading %v; a held crank was not "+
			"handed back, which is the one thing reset exists for", got, realAngle)
	}
	assertStateField(t, resp, "current", float64(0))
	assertStateField(t, resp, "crank_docked", true)

	resp = mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{ID: "7", Type: harness.CmdScreenshot}))
	if resp.Width != screenshot.Width || resp.Height != screenshot.Height {
		t.Errorf("screenshot reported %dx%d, want %dx%d",
			resp.Width, resp.Height, screenshot.Width, screenshot.Height)
	}

	switch resp.Format {
	case harness.FormatRaw:
		if resp.RowBytes != screenshot.RowBytes {
			t.Errorf("raw screenshot reported row_bytes %d, want %d", resp.RowBytes, screenshot.RowBytes)
		}
		assertRawScreenshot(t, dataDir)
	case harness.FormatPNG:
		assertPNGScreenshot(t, dataDir)
	default:
		t.Errorf("response had format %q, want %q or %q: %+v",
			resp.Format, harness.FormatRaw, harness.FormatPNG, resp)
	}

	resp = mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{ID: "8", Type: harness.CmdEntities}))
	if resp.EntitiesComplete != wantEntitiesComplete {
		t.Errorf("entities_complete = %v, want %v", resp.EntitiesComplete, wantEntitiesComplete)
	}
	if len(resp.Entities) != wantEntityCount {
		t.Fatalf("entities has %d entries, want %d: %+v", len(resp.Entities), wantEntityCount, resp.Entities)
	}

	// A leftover temp file means the rename failed and the non-atomic fallback ran.
	if _, err := os.Stat(filepath.Join(dataDir, "mcp", "response.tmp.json")); err == nil {
		t.Errorf("mcp/response.tmp.json was left behind; responses are not being published by rename")
	}

	if checkGameLogs {
		checkGameLogsContract(t, dataDir)
		checkStdoutIsLineBuffered(t, sim)
	}
}

// checkGameLogsContract checks the Lua fixture's print() and deliberate error
// (triggered by a sentinel crank angle) reach mcp/game_logs.jsonl, and that the
// harness survives the error.
func checkGameLogsContract(t *testing.T, dataDir string) {
	t.Helper()
	logsPath := filepath.Join(dataDir, "mcp", "game_logs.jsonl")

	entries := waitForGameLogEntry(t, logsPath, "print", "fixture print line")
	if entries == nil {
		t.Fatalf("game_logs.jsonl never contained the fixture's print() line")
	}

	mustRoundTrip(t, dataDir, harness.Command{
		ID: "9", Type: harness.CmdCrank,
		CrankAngle: 999999.0, CrankDelta: 0.0, CrankDock: harness.DockUnchanged, DurationMs: 10000})

	if waitForGameLogEntry(t, logsPath, "error", "deliberate fixture error") == nil {
		t.Fatalf("game_logs.jsonl never contained the fixture's deliberate error traceback")
	}

	checkCallbackErrorContract(t, dataDir)
	checkLogRotationContract(t, dataDir)

	// The polling loop must survive the uncaught error.
	mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{ID: "10", Type: harness.CmdPing}))
}

// checkCallbackErrorContract covers an error raised inside a callback the *harness*
// invokes (e.g. the game's AButtonDown from mcp.update()): the traceback must be
// logged and the harness must keep answering.
func checkCallbackErrorContract(t *testing.T, dataDir string) {
	t.Helper()
	logsPath := filepath.Join(dataDir, "mcp", "game_logs.jsonl")

	// Arm the fixture's throwing callback, then cause the edge that invokes it.
	mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{
		ID: "8c1", Type: harness.CmdCrank,
		CrankAngle: 777777.0, CrankDock: harness.DockUnchanged, DurationMs: 4000}))
	mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{
		ID: "8c2", Type: harness.CmdPress, Button: "a", DurationMs: 1000}))

	if waitForGameLogEntry(t, logsPath, "error", "deliberate callback error") == nil {
		t.Fatalf("an error thrown from a harness-invoked button callback never reached %s; "+
			"it is escaping the harness the way it did before callGameCallback existed", logsPath)
	}

	// The polling loop must survive it.
	mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{ID: "8c3", Type: harness.CmdPing}))
}

// checkLogRotationContract proves the harness rotates its log by renaming the full
// generation aside, not by discarding it. The fixture floods past the cap on a
// sentinel crank angle with a marker before and after; both must survive.
func checkLogRotationContract(t *testing.T, dataDir string) {
	t.Helper()

	mustRoundTrip(t, dataDir, harness.Command{
		ID: "8r", Type: harness.CmdCrank,
		CrankAngle: 888888.0, CrankDock: harness.DockUnchanged, DurationMs: 4000})

	prevPath := filepath.Join(dataDir, "mcp", "game_logs.1.jsonl")
	deadline := time.Now().Add(mcpDirTimeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(prevPath); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(prevPath); err != nil {
		t.Fatalf("no rotated generation at %s after flooding past the size cap: %v\n"+
			"a rotation that leaves nothing behind is the bug this guards", prevPath, err)
	}

	// Both generations, oldest first, as get_game_logs reads them.
	older := readGameLogEntries(t, prevPath)
	newer := readGameLogEntries(t, filepath.Join(dataDir, "mcp", "game_logs.jsonl"))

	t.Logf("rotation: %d entries in the rotated generation, %d in the current one", len(older), len(newer))
	if !containsMessage(older, "ROTATION-MARKER-OLD") {
		t.Errorf("the rotated generation does not contain the pre-rotation marker; "+
			"it holds %d entries", len(older))
	}
	if !containsMessage(newer, "ROTATION-MARKER-NEW") {
		t.Errorf("the current generation does not contain the post-rotation marker; "+
			"it holds %d entries", len(newer))
	}
}

// checkStdoutIsLineBuffered proves the Simulator's startup line `Loading: <pdx>`
// arrives without a block buffer filling. A Lua print() still doesn't reach stdout.
func checkStdoutIsLineBuffered(t *testing.T, sim *simulator.Simulator) {
	t.Helper()
	const want = "Loading:"
	deadline := time.Now().Add(mcpDirTimeout)
	for time.Now().Before(deadline) {
		if strings.Contains(sim.Output(), want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("the Simulator's %q line never reached its captured stdout, so stdout is still "+
		"block-buffered. Is stdbuf present on PATH?\ncaptured output:\n%s", want, sim.Output())
}

func readGameLogEntries(t *testing.T, path string) []tools.GameLogEntry {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	entries, err := tools.ParseGameLogs(b)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	return entries
}

func containsMessage(entries []tools.GameLogEntry, want string) bool {
	for _, e := range entries {
		if bytes.Contains([]byte(e.Message), []byte(want)) {
			return true
		}
	}
	return false
}

func waitForGameLogEntry(t *testing.T, path, wantType, wantMessageContains string) []tools.GameLogEntry {
	t.Helper()
	deadline := time.Now().Add(mcpDirTimeout)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(path)
		if err == nil {
			if entries, err := tools.ParseGameLogs(b); err == nil {
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

	installHarness(t, harness.CHeaderPath, filepath.Join(srcDir, "mcp_harness.h"))
	installHarness(t, harness.CSourcePath, filepath.Join(srcDir, "mcp_harness.c"))

	result, err := build.Build(fixtureDir, contractSDK(t))
	if err != nil {
		t.Fatalf("build.Build: %v\n%s", err, result.Output)
	}
	return result.PdxPath
}

func buildLuaFixture(t *testing.T, repoRoot string) string {
	t.Helper()
	fixtureDir := filepath.Join(repoRoot, "lua", "test-fixture")
	sourceDir := filepath.Join(fixtureDir, "Source")

	installHarness(t, harness.LuaSourcePath, filepath.Join(sourceDir, "mcp_harness.lua"))

	result, err := build.Build(fixtureDir, contractSDK(t))
	if err != nil {
		t.Fatalf("build.Build: %v\n%s", err, result.Output)
	}
	return result.PdxPath
}

// installHarness copies a harness into a fixture through setup.CopyHarnessFile,
// as setup does, so the version placeholder gets stamped.
func installHarness(t *testing.T, canonicalPath, dst string) {
	t.Helper()
	if err := setup.CopyHarnessFile(opencrank.HarnessFS, canonicalPath, dst); err != nil {
		t.Fatalf("installing %s: %v", canonicalPath, err)
	}
}

// mustRoundTrip sends cmd and waits for the response to that exact command.
func mustRoundTrip(t *testing.T, dataDir string, cmd harness.Command) harness.Response {
	t.Helper()
	if err := harness.SendCommand(dataDir, cmd); err != nil {
		t.Fatalf("SendCommand: %v", err)
	}
	resp, err := harness.WaitForResponse(dataDir, cmd.ID, responseTimeout)
	if err != nil {
		t.Fatalf("WaitForResponse for command %q: %v", cmd.ID, err)
	}
	return resp
}

// mustSucceed fails the test if the harness reported a failure via status/error.
func mustSucceed(t *testing.T, resp harness.Response) harness.Response {
	t.Helper()
	if resp.Failed() {
		t.Fatalf("harness reported failure: %s (full response: %+v)", resp.ErrorMessage(), resp)
	}
	return resp
}

// stateMap decodes the game-defined state payload, which stays raw on the wire.
func stateMap(t *testing.T, resp harness.Response) map[string]any {
	t.Helper()
	if len(resp.State) == 0 {
		t.Fatalf("response carried no state (full response: %+v)", resp)
	}
	var state map[string]any
	if err := json.Unmarshal(resp.State, &state); err != nil {
		t.Fatalf("state is not a JSON object: %s (%v)", resp.State, err)
	}
	return state
}

func assertStateField(t *testing.T, resp harness.Response, field string, want any) {
	t.Helper()
	state := stateMap(t, resp)
	got := state[field]
	if got != want {
		t.Errorf("state[%q] = %v (%T), want %v (%T)\nfull state: %v", field, got, got, want, want, state)
	}
}

// assertRawScreenshot checks the raw dump's size and that it decodes all black,
// as the fixture draws, pinning the SDK's undocumented bit polarity.
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
