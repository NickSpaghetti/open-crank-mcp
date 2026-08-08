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

// closedSetSchema infers In's input schema and pins the named properties to the exact
// values they accept, as JSON Schema `enum`.
//
// The values were only ever named in prose before - "one of a, b, up, down, left,
// right" in a description - and a description is not a constraint. Two things follow
// from making it one. A client can reject press_button("A") before it is sent, rather
// than the server being the first thing that notices; and anything generating inputs
// from the schema produces valid ones. That second one is not hypothetical: Specmatic's
// auto-test called teardown with language "MIRMU" and setup with a random source_dir,
// because a random string is what a schema saying `type: string` asks for. See
// docs/GOTCHAS.md.
//
// The handler-side validation stays. It is what produces the message that names the
// alternatives, it covers the values a schema cannot express (crank_dock's empty string
// meaning "unchanged"), and a constraint worth declaring is worth enforcing where the
// work happens too.
func closedSetSchema[In any](enums map[string][]any) *jsonschema.Schema {
	s, err := jsonschema.For[In](nil)
	if err != nil {
		panic(fmt.Sprintf("inferring %T schema: %v", *new(In), err))
	}
	for property, values := range enums {
		prop, ok := s.Properties[property]
		if !ok {
			// A panic rather than a silent skip: this runs at registration, so it
			// fails on the first connection rather than shipping a schema whose
			// constraint quietly went missing when a field was renamed.
			panic(fmt.Sprintf("closedSetSchema: %T has no property %q to constrain", *new(In), property))
		}
		prop.Enum = values
	}
	return s
}

// asAny widens a list of allowed values for jsonschema's Enum field, which is []any
// because JSON Schema allows any type in an enum. Takes ~string so a named string type
// (setup.Language) needs no conversion at the call site.
func asAny[T ~string](values []T) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = string(v)
	}
	return out
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
	// the SDK path was read under mu at two call sites, which reads as a claim
	// that something mutates it.
	sdk       sdk.Paths
	harnessFS fs.FS

	// sdkErr is why resolution failed, or nil. Held rather than fatal, so the
	// server still completes the MCP handshake and can say what went wrong
	// through a tool result. A process that exits before the handshake surfaces
	// in a client as "server failed to start", which names nothing.
	sdkErr error

	mu        sync.Mutex
	harnessMu sync.Mutex

	// Guarded by mu.
	sim      *simulator.Simulator
	pdxPath  string
	dataDir  string
	bundleID string
	nextID   int

	// scratchDir is a directory this process owns, handed to the game as
	// playdate.argv[2] for the Lua harness to write screenshots into. Separate
	// from dataDir because it does not have to be the sandbox: writeToFile takes
	// a host path. Removed when the Simulator stops.
	scratchDir string

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
//
// It no longer builds the path: internal/sdk resolved it, which is what makes
// the macOS .app bundle and the Windows .exe work without either call site
// knowing they exist.
func (s *Server) simulatorBin() string {
	return s.sdk.SimulatorBin
}

// NewServer takes an SDK already resolved by the caller, the reason resolution
// failed if it did, and the harness sources the `setup` tool writes into a game
// (normally opencrank.HarnessFS).
//
// All injected rather than read here, so a test can supply a fake SDK layout and
// an fstest.MapFS without touching the environment or the filesystem.
func NewServer(paths sdk.Paths, sdkErr error, harnessFS fs.FS) *Server {
	return &Server{sdk: paths, sdkErr: sdkErr, harnessFS: harnessFS}
}

// requireSDK reports the SDK, or a model-visible result explaining why there
// isn't one. Same shape as notRunningResult: a tool the agent can react to,
// rather than an opaque protocol failure.
//
// Every path internal/sdk tried is included, because "it looked here and here"
// is the only actionable thing to say when detection fails on a machine nobody
// here can see.
func (s *Server) requireSDK() (sdk.Paths, *mcp.CallToolResult) {
	if s.sdkErr == nil {
		return s.sdk, nil
	}
	return sdk.Paths{}, errorResult(fmt.Sprintf(
		"no Playdate SDK available: %v\n\nSet %s to the SDK directory, then restart this server.",
		s.sdkErr, sdk.EnvVarSDKPath))
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

// clearScratchLocked removes the screenshot scratch directory, if any. Caller
// holds mu.
//
// Best-effort: a leftover directory under the OS temp dir is untidy, not broken,
// and failing a stop_simulator call over it would be worse than the leak.
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
	// The closed sets, declared once each and shared by the tools that take them, so a
	// new button or language cannot reach one tool's schema and miss another's.
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
		// The enum lists the three modes but cannot say "or leave it out": that is what
		// the field being optional means, and it is the ordinary case.
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
