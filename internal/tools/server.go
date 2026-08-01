// Package tools registers MCP tools that wire internal/simulator,
// internal/harness, internal/build, and internal/screenshot together.
package tools

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"time"

	"github.com/NickSpaghetti/open-crank-mcp/internal/harness"
	"github.com/NickSpaghetti/open-crank-mcp/internal/simulator"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// readSaveDataOutputSchema overrides the auto-inferred schema for
// ReadSaveDataOutput. Its Data field is `any` (a save file's shape isn't
// known ahead of time), which the default inference renders as the JSON
// Schema value `true` (spec-legal - "any value is valid" - but rejected
// by at least one real MCP client's stricter schema validator, which
// failed its entire tools/list fetch over this single property).
// jsonschema-go collapses any all-empty schema to `true` as a shorthand
// (see its Schema.MarshalJSON), so an empty override doesn't avoid this -
// the override needs some real content, hence the description, to stay
// a schema object.
func readSaveDataOutputSchema() *jsonschema.Schema {
	s, err := jsonschema.For[ReadSaveDataOutput](&jsonschema.ForOptions{
		TypeSchemas: map[reflect.Type]*jsonschema.Schema{
			reflect.TypeFor[any](): {Description: "arbitrary JSON value read from the save file"},
		},
	})
	if err != nil {
		panic(fmt.Sprintf("inferring ReadSaveDataOutput schema: %v", err))
	}
	return s
}

const responseTimeout = 5 * time.Second

var errNotRunning = errors.New("simulator not running - call launch_simulator first")

// errHarness marks a failure the harness itself reported, as opposed to one this
// server ran into. Like errNotRunning it is recoverable and model-visible: the
// caller can read it and react, so it must not surface as an opaque protocol
// error.
var errHarness = errors.New("harness reported an error")

// Server holds everything a tool handler might need across separate tool
// calls - which simulator (if any) is running, its data directory, and a
// counter for correlating harness command IDs. Guarded by mu since it
// outlives any single tool call.
//
// harnessMu is a separate lock serializing any interaction with the
// harness's shared, fixed-filename IPC channel (mcp/command.json,
// mcp/response.json, and any fixed path a response references, e.g.
// mcp/screenshot.png|raw). The MCP go-sdk dispatches tool calls
// concurrently by default (jsonrpc2.Async, called for every request but
// "initialize"), but this protocol supports exactly one outstanding
// request at a time - without this lock, two concurrent calls can read
// back each other's responses, or one get_screenshot call can overwrite
// the fixed screenshot file before another has read it. Always acquired
// before mu, never after, so there's no lock-ordering risk between them.
type Server struct {
	// Set once by NewServer and never written again, so they are read without
	// the lock. Grouped and said out loud because the opposite was happening:
	// sdkPath was read under mu at two call sites, which reads as a claim that
	// something mutates it.
	sdkPath   string
	harnessFS fs.FS

	mu        sync.Mutex
	harnessMu sync.Mutex

	// Guarded by mu.
	sim      *simulator.Simulator
	pdxPath  string
	dataDir  string
	bundleID string
	nextID   int

	// harnessVersion is what the last successful round trip reported, and
	// harnessVersionSeen is whether there has been one. Two fields rather than
	// treating "" as unknown, because "" is a real answer: it is what a harness
	// older than the version marker reports. Conflating "nothing has asked yet"
	// with "the game answered and it is stale" would be the same silent-conflation
	// mistake this change set exists to remove, in miniature.
	harnessVersion     string
	harnessVersionSeen bool
}

// simulatorBin is the Simulator executable inside the resolved SDK. A method
// rather than a field so the two call sites that used to build this path under
// mu (launch and restart) have one place to get it from.
func (s *Server) simulatorBin() string {
	return filepath.Join(s.sdkPath, "bin", "PlaydateSimulator")
}

// NewServer takes the SDK path already resolved by the caller, and the harness
// sources the `setup` tool writes into a game (normally opencrank.HarnessFS).
//
// Both are injected rather than read here, so a test can supply a fake SDK
// layout and an fstest.MapFS without touching the environment or the filesystem.
func NewServer(sdkPath string, harnessFS fs.FS) *Server {
	return &Server{sdkPath: sdkPath, harnessFS: harnessFS}
}

// errorResult is a recoverable, model-visible error - the caller can see it and
// react (e.g. call launch_simulator), rather than the whole tool call failing as
// an opaque protocol error.
func errorResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}

func notRunningResult() *mcp.CallToolResult {
	return errorResult(errNotRunning.Error())
}

// handleRoundTripErr turns the two recoverable failures - no simulator, and a
// harness that reported an error - into model-visible tool results. Any other
// error is a genuine unexpected failure and stays a Go error.
func handleRoundTripErr(err error) (*mcp.CallToolResult, error) {
	if errors.Is(err, errNotRunning) || errors.Is(err, errHarness) {
		return errorResult(err.Error()), nil
	}
	return nil, err
}

// requireDataDir returns the current data directory, or errNotRunning if
// no simulator has been launched yet.
func (s *Server) requireDataDir() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sim == nil {
		return "", errNotRunning
	}
	return s.dataDir, nil
}

// roundTrip sends cmd through the harness and waits for its response.
// Adds a unique id to cmd, overwriting any caller-supplied one.
func (s *Server) roundTrip(cmd harness.Command) (harness.Response, error) {
	s.harnessMu.Lock()
	defer s.harnessMu.Unlock()
	return s.roundTripLocked(cmd)
}

// roundTripLocked is roundTrip's body, assuming the caller already holds
// harnessMu. Used directly by handlers (getScreenshot) that need to
// extend the critical section past the round trip itself, to also cover
// a subsequent read of a fixed path the response references.
//
// A harness that answers with a failure status is returned as errHarness, not as
// a successful response the caller has to remember to inspect. Both harnesses
// have always reported real failures this way - "failed to parse command",
// "getDisplayFrame returned NULL", "entities list did not fit" - and every tool
// used to discard them and report success.
func (s *Server) roundTripLocked(cmd harness.Command) (harness.Response, error) {
	s.mu.Lock()
	if s.sim == nil {
		s.mu.Unlock()
		return harness.Response{}, errNotRunning
	}
	dataDir := s.dataDir
	s.nextID++
	cmd.ID = strconv.Itoa(s.nextID)
	s.mu.Unlock()

	if err := harness.SendCommand(dataDir, cmd); err != nil {
		return harness.Response{}, fmt.Errorf("sending command: %w", err)
	}
	resp, err := harness.WaitForResponse(dataDir, cmd.ID, responseTimeout)
	if err != nil {
		return harness.Response{}, err
	}

	// Recorded even when the harness reported a failure below: which harness
	// answered is true regardless of what it said, and a stale harness is a
	// plausible reason for the failure.
	s.mu.Lock()
	s.harnessVersion = resp.HarnessVersion
	s.harnessVersionSeen = true
	s.mu.Unlock()

	if resp.Failed() {
		return resp, fmt.Errorf("%w: %s", errHarness, resp.ErrorMessage())
	}
	return resp, nil
}

// RegisterAll wires every tool onto server.
func RegisterAll(server *mcp.Server, s *Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "setup",
		Description: "Wires the MCP harness into a game project: copies mcp_harness.lua (Lua) or mcp_harness.{h,c} " +
			"(C) in and patches main.lua/CMakeLists.txt/the eventHandler file to call it. Auto-detects Lua, C, or " +
			"hybrid C+Lua (hybrid only needs the Lua harness - a real Lua VM still drives the update loop) unless " +
			"language is given explicitly. Safe to re-run. For C, some steps may not be confidently automatable " +
			"(e.g. finding the right PlaydateAPI pointer inside your update callback) - check manual_steps in the " +
			"response for anything left to do by hand.",
	}, s.setupHarness)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "teardown",
		Description: "Reverses setup: removes the copied harness file(s) and strips everything setup patched into main.lua/CMakeLists.txt/the eventHandler file.",
	}, s.teardownHarness)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "build_game",
		Description: "Detects a game's project type (C or Lua) and builds it into a .pdx. Returns compile errors and warnings either way.",
	}, s.buildGame)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "launch_simulator",
		Description: "Launches PlaydateSimulator with the given .pdx.",
	}, s.launchSimulator)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "stop_simulator",
		Description: "Stops the running simulator.",
	}, s.stopSimulator)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "restart_simulator",
		Description: "Stops and relaunches the simulator with the same .pdx it was last launched with.",
	}, s.restartSimulator)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_status",
		Description: "Reports whether the simulator is running, its bundle ID, and whether the harness is reachable.",
	}, s.getStatus)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_logs",
		Description: "Returns the Simulator process's own stdout/stderr: GTK warnings, startup messages, and - unreliably - a Lua game's print() output and tracebacks. Unreliably because the simulator is launched line-buffered so its own startup diagnostics arrive immediately, but a Lua game's print() still does not reliably appear here. Use get_game_logs for a game's own output; it is written to disk per entry.",
	}, s.getLogs)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_game_logs",
		Description: "Returns a Lua game's own print() output, plus tracebacks from errors raised inside the game's update function, captured to disk by the harness (lua/mcp_harness.lua) so they are complete the moment you ask. Prefer this over get_logs for a game's own output, which stdout buffering makes unreliable. Errors from the button callbacks the harness itself invokes are captured here too. Requires the game to use the harness's print()/mcp.run() capture. Not applicable to C games.",
	}, s.getGameLogs)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "press_button",
		Description: "Presses a button (a, b, up, down, left, right) for duration_ms.",
	}, s.pressButton)
	mcp.AddTool(server, &mcp.Tool{
		Name: "set_crank",
		Description: "Overrides the crank's angle and delta for duration_ms. Optionally overrides the docked state " +
			"too: crank_dock takes \"docked\" or \"undocked\", and omitting it leaves the dock reading whatever the " +
			"game would really see.",
	}, s.setCrank)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_screenshot",
		Description: "Returns the current screen as a PNG image.",
	}, s.getScreenshot)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_game_state",
		Description: "Returns the game's registered state inspector output as JSON. The shape is entirely game-defined.",
	}, s.getGameState)
	mcp.AddTool(server, &mcp.Tool{
		Name: "list_entities",
		Description: "Lists sprites currently in the display list (position, bounds, tag, z-index, visibility, " +
			"and class name for Lua games). For Lua games this is a complete list. For C-built games, only " +
			"sprites with a collision rect set are included - purely decorative/visual sprites are commonly " +
			"missed, since the C API has no true 'list all sprites' query. Check the complete field.",
	}, s.listEntities)

	mcp.AddTool(server, &mcp.Tool{
		Name:         "read_save_data",
		Description:  "Reads a JSON save file from the game's data directory. Omit filename to list available files instead.",
		OutputSchema: readSaveDataOutputSchema(),
	}, s.readSaveData)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "write_save_data",
		Description: "Writes a JSON value to a save file in the game's data directory.",
	}, s.writeSaveData)
}
