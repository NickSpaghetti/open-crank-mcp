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
		// The fixture creates one sprite with a collide rect and one
		// without. querySpritesInRect (the C API's only bulk sprite query)
		// only matches sprites with a collide rect set, so only the
		// collidable one should show up here, and entities_complete must
		// be false - proving the approximation's documented limitation is
		// real, not just a design note.
		runContractCheck(t, paths, cPdx, "dev.open-crank-mcp.contractcheck", 1, false, false)
	})
	t.Run("Lua harness", func(t *testing.T) {
		// getAllSprites() is a true, complete enumeration - both the
		// fixture's sprites should show up regardless of collide rects.
		runContractCheck(t, paths, luaPdx, "dev.open-crank-mcp.contractchecklua", 2, true, true)
	})
}

// paths rather than a bare SDK directory, because both values this needs are
// per-platform and only internal/sdk knows them. Building them here as
// <sdk>/bin/PlaydateSimulator was correct on Linux and silently wrong on macOS,
// where the executable lives inside a .app bundle - the macOS CI leg failed on
// exactly that, with `stdbuf: .../bin/PlaydateSimulator: No such file or
// directory`, on its first ever run.
func runContractCheck(t *testing.T, paths sdk.Paths, pdxPath, bundleID string, wantEntityCount int, wantEntitiesComplete, checkGameLogs bool) {
	t.Helper()
	// The first candidate is where the Simulator will put it, on every platform
	// this supports. Predicted rather than probed because this waits for the
	// directory to appear, so it cannot look for one that already exists.
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

	// The stamped fingerprint has to survive the whole path: substituted by
	// setup.CopyHarnessFile, compiled or loaded into the game, and reported back
	// over the file protocol by a real Simulator. Unit tests cover each step; only
	// this covers the join, and a silent break here is what would let a stale
	// harness stop being detectable. Either harness's fingerprint is accepted,
	// which is the same rule the server itself applies.
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
	resp = mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{ID: "4b", Type: harness.CmdState}))
	assertStateField(t, resp, "a_down_count", float64(1))
	assertStateField(t, resp, "a_up_count", float64(1))

	// What the game's own dock reading is before anything overrides it.
	//
	// Asserted to be true rather than merely recorded, because the value is what
	// gives the "leave the dock alone" check below its teeth: the old protocol
	// forced *undocked* on every crank command, so against a Simulator that
	// reported undocked anyway, a passthrough and a wrongly-forced override look
	// identical. If a future SDK starts reporting the crank undocked at rest, this
	// fails and says why - which is the outcome to want, because the alternative is
	// a test that quietly stops proving anything.
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

	// A crank command that does not ask about the dock must move the angle and
	// leave the dock reading exactly as the game would see it. Asserted against the
	// baseline captured above rather than against a hardcoded false, so this cannot
	// pass by agreeing with a default.
	mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{
		ID: "6b", Type: harness.CmdCrank,
		CrankAngle: 200.0, CrankDock: harness.DockUnchanged, DurationMs: 10000}))
	resp = mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{ID: "6c", Type: harness.CmdState}))
	assertStateField(t, resp, "crank_angle", 200.0)
	if got := stateMap(t, resp)["crank_docked"]; got != realDocked {
		t.Errorf("crank_dock=unchanged changed the dock reading from %v to %v", realDocked, got)
	}

	// And forcing undocked still overrides, so the passthrough above is a decision
	// rather than an inability to set it.
	mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{
		ID: "6d", Type: harness.CmdCrank,
		CrankAngle: 200.0, CrankDock: harness.DockUndocked, DurationMs: 10000}))
	resp = mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{ID: "6e", Type: harness.CmdState}))
	assertStateField(t, resp, "crank_docked", false)

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

	// Every response above arrived, so every one of them was published by rename.
	// A leftover temp file would mean the rename failed and the fallback wrote the
	// response directly - still working, but without the guarantee that a response
	// on disk is complete, which is the whole point of publishing that way.
	if _, err := os.Stat(filepath.Join(dataDir, "mcp", "response.tmp.json")); err == nil {
		t.Errorf("mcp/response.tmp.json was left behind; responses are not being published by rename")
	}

	if checkGameLogs {
		checkGameLogsContract(t, dataDir)
		checkStdoutIsLineBuffered(t, sim)
	}
}

// checkGameLogsContract exercises get_game_logs against the real Lua fixture
// (lua/test-fixture/Source/main.lua): a print() call at init should already
// be captured, and triggering the fixture's deliberate error (via the
// "crank" command with its magic sentinel angle) should both surface a
// traceback AND prove mcp.run() kept the harness alive - the whole point of
// this fix, not just the log text. Reads mcp/game_logs.jsonl directly, the
// same direct-file-access path get_game_logs itself uses (see
// internal/tools/gamelogs.go), rather than going through a command/response
// round trip.
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

	// The real assertion: the harness's own polling loop must still be
	// alive after that uncaught error, not frozen along with the game's
	// own broken frame logic.
	mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{ID: "10", Type: harness.CmdPing}))
}

// checkCallbackErrorContract covers an error raised inside a callback the *harness*
// invokes, as opposed to inside the game's frame logic.
//
// wrapUpdate has always protected the frame logic. It did not protect mcp.update(),
// which is where the harness calls the game's own AButtonDown/AButtonUp - so an
// error there escaped, went unrecorded by the very channel that advertises
// "uncaught-error tracebacks", and stopped the polling loop permanently. Measured
// before the fix: the traceback reached stdout only, game_logs held one unrelated
// entry, and a ping issued afterwards was never answered.
//
// Both halves are asserted, because either alone would be satisfied by a broken
// implementation: the traceback has to be captured, and the harness has to still be
// answering afterwards.
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

	// The polling loop has to have survived it. A stale response would fail the id
	// check inside WaitForResponse rather than being mistaken for this one's answer.
	mustSucceed(t, mustRoundTrip(t, dataDir, harness.Command{ID: "8c3", Type: harness.CmdPing}))
}

// checkLogRotationContract proves the harness rotates its log by renaming the full
// generation aside, not by discarding it.
//
// This is the one assertion that could not be made anywhere else. The first
// implementation truncated at the size cap, so a rotation left zero history - worst
// at exactly the moment the log is read, since a traceback is always appended after
// whatever caused it. Unit tests can only check the reader's half; the writer's half
// is Lua running in a real Simulator, and the size cap makes it reachable only by
// actually filling a generation.
//
// The fixture floods past the cap on a sentinel crank angle, printing a marker
// before and after. Both markers being readable afterwards is the property: the old
// one can only still exist if it was moved rather than dropped.
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

	// Read the way get_game_logs does - both generations, oldest first - and require
	// the pre-rotation marker to have survived alongside the post-rotation one.
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

// checkStdoutIsLineBuffered proves the Simulator's own startup diagnostics reach us
// without waiting for a block buffer to fill.
//
// `Loading: <pdx>` is the assertion rather than the fixture's print() because it is
// what was actually measured to change: through this same code path, captured output
// went from 223 bytes (the GTK warning alone) to 288 bytes including this line, with
// stdbuf the only difference. A Lua game's print() still does not arrive here, which
// is exactly why get_game_logs is not retired - see lineBufferedCommand and
// docs/GOTCHAS.md.
//
// Worth having because the line is small: without line buffering it sits unflushed,
// and Stop() is a hard kill that discards it. That is the same mechanism that hides
// the one message explaining a Simulator which quits during startup.
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
			// The tool's own decoder, not a second one written for the test.
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

// installHarness puts a harness source into a fixture the same way the setup tool
// puts it into a real game - from the embedded copy, through the same stamping
// path - rather than copying the file off disk.
//
// The difference matters. A hand-copied fixture carries an unsubstituted version
// placeholder, so it would be the one harness copy in this project whose drift
// check cannot work, in the very suite whose job is to catch protocol drift
// against a real Simulator. Going through setup.CopyHarnessFile also means these
// tests exercise the stamping itself end to end.
func installHarness(t *testing.T, canonicalPath, dst string) {
	t.Helper()
	if err := setup.CopyHarnessFile(opencrank.HarnessFS, canonicalPath, dst); err != nil {
		t.Fatalf("installing %s: %v", canonicalPath, err)
	}
}

// mustRoundTrip sends cmd and waits for the response to that exact command.
// Every call site was already a send immediately followed by a receive, and the
// id now has to be threaded from one to the other, so they are one helper.
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

// mustSucceed is the check no tool used to make: the harness reporting a failure
// through status/error, which both harnesses genuinely do.
func mustSucceed(t *testing.T, resp harness.Response) harness.Response {
	t.Helper()
	if resp.Failed() {
		t.Fatalf("harness reported failure: %s (full response: %+v)", resp.ErrorMessage(), resp)
	}
	return resp
}

// stateMap decodes the game-defined state payload. It stays raw on the wire
// because its shape belongs to the game, so a test that wants to look inside is
// the thing that decodes it.
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
