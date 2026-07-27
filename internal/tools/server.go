// Package tools registers MCP tools that wire internal/simulator,
// internal/harness, internal/build, and internal/screenshot together.
package tools

import (
	"errors"
	"fmt"
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

// Server holds everything a tool handler might need across separate tool
// calls - which simulator (if any) is running, its data directory, and a
// counter for correlating harness command IDs. Guarded by mu since it
// outlives any single tool call.
type Server struct {
	mu       sync.Mutex
	sdkPath  string
	sim      *simulator.Simulator
	pdxPath  string
	dataDir  string
	bundleID string
	nextID   int
}

// NewServer reads PLAYDATE_SDK_PATH from the environment, matching every
// other entry point in this project (cmd/smoke-check, internal/contracttest).
func NewServer(sdkPath string) *Server {
	return &Server{sdkPath: sdkPath}
}

// notRunningResult is a recoverable, model-visible error - the caller can
// see it and react (e.g. call launch_simulator), rather than the whole
// tool call failing as an opaque protocol error.
func notRunningResult() *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: errNotRunning.Error()}},
	}
}

// handleRoundTripErr turns errNotRunning into a recoverable tool result;
// any other error is a genuine unexpected failure and stays a Go error.
func handleRoundTripErr(err error) (*mcp.CallToolResult, error) {
	if errors.Is(err, errNotRunning) {
		return notRunningResult(), nil
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
func (s *Server) roundTrip(cmd map[string]any) (map[string]any, error) {
	s.mu.Lock()
	if s.sim == nil {
		s.mu.Unlock()
		return nil, errNotRunning
	}
	dataDir := s.dataDir
	s.nextID++
	cmd["id"] = strconv.Itoa(s.nextID)
	s.mu.Unlock()

	if err := harness.SendCommand(dataDir, cmd); err != nil {
		return nil, fmt.Errorf("sending command: %w", err)
	}
	return harness.WaitForResponse(dataDir, responseTimeout)
}

func asFloat(v any) float64 {
	f, _ := v.(float64)
	return f
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func asBool(v any) bool {
	b, _ := v.(bool)
	return b
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
		Description: "Returns the Simulator process's own buffered stdout/stderr (GTK warnings, startup messages, and the like). Does NOT include a Lua game's print() output or tracebacks - use get_game_logs for those.",
	}, s.getLogs)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_game_logs",
		Description: "Returns a Lua game's own print() output and unhandled-error tracebacks, captured by the harness (lua/mcp_harness.lua) since PlaydateSimulator's Lua console never reaches real stdout/stderr. Requires the game to use the harness's print()/mcp.run() capture - see the Lua harness README section. Not applicable to C games.",
	}, s.getGameLogs)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "press_button",
		Description: "Presses a button (a, b, up, down, left, right) for duration_ms.",
	}, s.pressButton)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "set_crank",
		Description: "Overrides the crank's angle, delta, and dock state for duration_ms.",
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
