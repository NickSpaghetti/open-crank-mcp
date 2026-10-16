// Package tools registers MCP tools that wire internal/simulator,
// internal/harness, internal/build, and internal/screenshot together.
package tools

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"reflect"
	"strconv"
	"sync"
	"time"

	"github.com/NickSpaghetti/open-crank-mcp/internal/harness"
	"github.com/NickSpaghetti/open-crank-mcp/internal/sdk"
	crankSetup "github.com/NickSpaghetti/open-crank-mcp/internal/setup"
	"github.com/NickSpaghetti/open-crank-mcp/internal/simulator"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// readSaveDataOutputSchema avoids the inferred `true` schema for the `any`
// Data field, which a real MCP client's validator rejected. jsonschema-go
// collapses an empty schema to `true`, so the override needs a description.
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

// closedSetSchema infers In's input schema and pins the named properties to
// the values they accept, as JSON Schema `enum`, so schema-driven clients
// cannot send arbitrary strings.
func closedSetSchema[In any](enums map[string][]any) *jsonschema.Schema {
	s, err := jsonschema.For[In](nil)
	if err != nil {
		panic(fmt.Sprintf("inferring %T schema: %v", *new(In), err))
	}
	for property, values := range enums {
		prop, ok := s.Properties[property]
		if !ok {
			// Panic so a renamed field fails at registration, not silently.
			panic(fmt.Sprintf("closedSetSchema: %T has no property %q to constrain", *new(In), property))
		}
		prop.Enum = values
	}
	return s
}

// asAny widens values for jsonschema's []any Enum field.
func asAny[T ~string](values []T) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = string(v)
	}
	return out
}

const responseTimeout = 5 * time.Second

var errNotRunning = errors.New("simulator not running - call launch_simulator first")

// errHarness marks a failure the harness itself reported. Like errNotRunning
// it is surfaced as a model-visible result, not a protocol error.
var errHarness = errors.New("harness reported an error")

// Only reachable as a bug: every path that sets s.sim also sets s.scratchDir.
var errNoScratch = errors.New("simulator is running with no screenshot scratch directory")

// Server holds state shared across tool calls, guarded by mu.
//
// harnessMu serializes use of the harness's fixed-filename IPC files: the MCP
// go-sdk dispatches tool calls concurrently, but the protocol allows one
// outstanding request. Always acquired before mu, never after.
type Server struct {
	// Set once by NewServer and never written again, so read without the lock.
	sdk       sdk.Paths
	harnessFS fs.FS

	// sdkErr is held rather than fatal so the server still completes the MCP
	// handshake and can report the problem through a tool result.
	sdkErr error

	mu        sync.Mutex
	harnessMu sync.Mutex

	// Guarded by mu.
	sim      *simulator.Simulator
	pdxPath  string
	dataDir  string
	bundleID string
	nextID   int

	// scratchDir is a directory this process owns, passed to the game as
	// playdate.argv[2] for Lua screenshots. Removed when the Simulator stops.
	scratchDir string

	// harnessVersionSeen distinguishes "not asked yet" from "", which is what a
	// harness older than the version marker reports.
	harnessVersion     string
	harnessVersionSeen bool
}

func (s *Server) simulatorBin() string {
	return s.sdk.SimulatorBin
}

// NewServer takes an already-resolved SDK, the resolution error if any, and
// the harness sources `setup` writes into a game (normally opencrank.HarnessFS).
func NewServer(paths sdk.Paths, sdkErr error, harnessFS fs.FS) *Server {
	return &Server{sdk: paths, sdkErr: sdkErr, harnessFS: harnessFS}
}

// requireSDK reports the SDK, or a model-visible result explaining why there
// isn't one.
func (s *Server) requireSDK() (sdk.Paths, *mcp.CallToolResult) {
	if s.sdkErr == nil {
		return s.sdk, nil
	}
	return sdk.Paths{}, errorResult(fmt.Sprintf(
		"no Playdate SDK available: %v\n\nSet %s to the SDK directory, then restart this server.",
		s.sdkErr, sdk.EnvVarSDKPath))
}

// errorResult is a recoverable, model-visible error rather than a protocol error.
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

// clearScratchLocked removes the screenshot scratch directory, if any. Caller
// holds mu. Best-effort: a leftover temp dir is not worth failing a stop over.
func (s *Server) clearScratchLocked() {
	if s.scratchDir == "" {
		return
	}
	_ = os.RemoveAll(s.scratchDir)
	s.scratchDir = ""
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

// roundTripLocked is roundTrip for callers already holding harnessMu, such as
// getScreenshot, which must also read the fixed path the response references.
// A harness failure status is returned as errHarness.
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

	// Recorded even on failure: a stale harness is a plausible cause.
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
	languageEnum := map[string][]any{"language": asAny(crankSetup.Languages)}

	mcp.AddTool(server, &mcp.Tool{
		Name: "setup",
		Description: "Wires the MCP harness into a game project: copies mcp_harness.lua (Lua) or mcp_harness.{h,c} " +
			"(C) in and patches main.lua/CMakeLists.txt/the eventHandler file to call it. Auto-detects Lua, C, or " +
			"hybrid C+Lua (hybrid only needs the Lua harness - a real Lua VM still drives the update loop) unless " +
			"language is given explicitly. Safe to re-run. For C, some steps may not be confidently automatable " +
			"(e.g. finding the right PlaydateAPI pointer inside your update callback) - check manual_steps in the " +
			"response for anything left to do by hand.",
		InputSchema: closedSetSchema[SetupInput](languageEnum),
	}, s.setupHarness)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "teardown",
		Description: "Reverses setup: removes the copied harness file(s) and strips everything setup patched into main.lua/CMakeLists.txt/the eventHandler file.",
		InputSchema: closedSetSchema[TeardownInput](languageEnum),
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

	buttonEnum := map[string][]any{"button": asAny(harness.ButtonNames)}
	mcp.AddTool(server, &mcp.Tool{
		Name: "press_button",
		Description: "Taps a button (a, b, up, down, left, right). Omit duration_ms for a short press the " +
			"game is guaranteed to see; pass one to hold for that long. Either way it releases on its own - " +
			"use hold_button for a press that stays down.",
		InputSchema: closedSetSchema[PressButtonInput](buttonEnum),
	}, s.pressButton)
	mcp.AddTool(server, &mcp.Tool{
		Name: "hold_button",
		Description: "Holds a button (a, b, up, down, left, right) down until release_button or reset_input " +
			"lets it go, or another command replaces it. For anything a player would hold rather than tap: " +
			"walking, steering, charging a shot.",
		InputSchema: closedSetSchema[HoldButtonInput](buttonEnum),
	}, s.holdButton)
	mcp.AddTool(server, &mcp.Tool{
		Name: "release_button",
		Description: "Releases a button, ending a hold. Forces it up briefly - so a human driving the same " +
			"Simulator cannot leak a press through - and then hands it back to real input.",
		InputSchema: closedSetSchema[ReleaseButtonInput](buttonEnum),
	}, s.releaseButton)
	mcp.AddTool(server, &mcp.Tool{
		Name: "reset_input",
		Description: "Drops every input override at once, buttons and crank, so the game reads real input " +
			"again. The only way to release a crank set with no duration_ms, since set_crank holds it " +
			"indefinitely by design.",
	}, s.resetInput)
	mcp.AddTool(server, &mcp.Tool{
		Name: "set_crank",
		Description: "Overrides the crank's angle and delta. Omit duration_ms and it stays where you put it, " +
			"the way a real crank does - reset_input is what hands it back. Optionally overrides the docked " +
			"state too: crank_dock takes \"docked\" or \"undocked\", and omitting it leaves the dock reading " +
			"whatever the game would really see.",
		InputSchema: closedSetSchema[SetCrankInput](map[string][]any{
			"crank_dock": asAny(harness.DockModes),
		}),
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
