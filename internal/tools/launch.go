package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NickSpaghetti/open-crank-mcp/internal/build"
	"github.com/NickSpaghetti/open-crank-mcp/internal/sdk"
	"github.com/NickSpaghetti/open-crank-mcp/internal/simulator"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Long enough for the Simulator to fail on startup and short enough not to be
// felt. Its SDL/audio initialisation failures land well inside this.
const startupGrace = 750 * time.Millisecond

// How long to wait for the running game to reveal its data directory by creating
// mcp/ in it. Generous, because it is only ever paid in full when the answer is
// "this game has no harness", and cheap otherwise: the probe returns the moment
// the directory appears, which for a harnessed game is usually already true by
// the time the startup grace above has elapsed.
const dataDirGrace = 3 * time.Second

type LaunchSimulatorInput struct {
	PdxPath string `json:"pdx_path" jsonschema:"path to the built .pdx"`
}

type LaunchSimulatorOutput struct {
	BundleID string `json:"bundle_id"`
	DataDir  string `json:"data_dir"`
	// DataDirSource says how DataDir was arrived at: observed by finding the
	// harness's own directory, taken from an override, or merely assumed. A
	// caller that sees "assumed" and then hits timeouts has its explanation.
	DataDirSource string `json:"data_dir_source"`
}

// Where the Lua harness writes its screenshot, relative to the scratch directory
// below. The C harness does not use this: it writes through pd->file->*, which is
// already sandbox-relative.
const luaScreenshotRelDir = "mcp"

func (s *Server) launchSimulator(_ context.Context, _ *mcp.CallToolRequest, in LaunchSimulatorInput) (*mcp.CallToolResult, LaunchSimulatorOutput, error) {
	paths, errResult := s.requireSDK()
	if errResult != nil {
		return errResult, LaunchSimulatorOutput{}, nil
	}

	bundleID, err := build.ReadBundleID(in.PdxPath)
	if err != nil {
		return nil, LaunchSimulatorOutput{}, fmt.Errorf("reading bundle ID: %w", err)
	}

	// A directory this process owns, passed to the game as playdate.argv[2].
	//
	// It used to be the sandboxed data directory, which meant predicting that
	// directory before the game had run. It never had to be: the Lua harness uses
	// this only as a base for playdate.simulator.writeToFile, and that call takes
	// a path on the dev machine rather than a sandbox-relative one. So the guess
	// was load-bearing for no reason. A scratch directory removes it entirely.
	//
	// mcp/ is created here because writeToFile will not create directories.
	scratch, err := os.MkdirTemp("", "open-crank-"+bundleID+"-")
	if err != nil {
		return nil, LaunchSimulatorOutput{}, fmt.Errorf("creating screenshot scratch directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(scratch, luaScreenshotRelDir), 0o755); err != nil {
		return nil, LaunchSimulatorOutput{}, fmt.Errorf("creating %s in scratch directory: %w", luaScreenshotRelDir, err)
	}

	// ToSlash because the Lua harness joins this with a hardcoded "/". Windows
	// file APIs accept forward slashes, so normalising here is enough, and it is
	// the only place it can be done: `setup` vendors a *copy* of the harness into
	// each game, so a fix on the Lua side would never reach a project that was set
	// up before it.
	sim, err := simulator.Launch(s.simulatorBin(), in.PdxPath, filepath.ToSlash(scratch))
	if err != nil {
		os.RemoveAll(scratch)
		return nil, LaunchSimulatorOutput{}, fmt.Errorf("launching simulator: %w", err)
	}

	// Starting the process says nothing about whether it stayed. The Simulator
	// quits during startup for reasons that have nothing to do with the game -
	// notably a missing PulseAudio, since it runs with SDL_AUDIODRIVER=pulseaudio
	// and refuses to run without it. Reporting success for a Simulator that has
	// already gone sends whoever asked looking at their game instead of reading
	// the one message that explains it, which never reaches any log this server
	// exposes.
	time.Sleep(startupGrace)
	if sim.Exited() {
		os.RemoveAll(scratch)
		return nil, LaunchSimulatorOutput{}, fmt.Errorf(
			"the simulator quit during startup:\n%s", strings.TrimSpace(sim.Output()))
	}

	// Now that the game is running, ask where its data actually went rather than
	// predicting it. See sdk.FindDataDir: the harness creates mcp/ inside the
	// sandboxed directory, which makes the right answer observable.
	// Polled rather than probed once: the harness creates mcp/ during its own
	// init, which may not have happened yet. The loop exits the moment it
	// appears, so a harnessed game normally pays one iteration and an unharnessed
	// one pays the full grace period exactly once per launch.
	env := sdk.OSEnv()
	var (
		dataDir string
		found   bool
		tried   []string
	)
	for deadline := time.Now().Add(dataDirGrace); ; {
		dataDir, found, tried = paths.FindDataDir(env, bundleID)
		if found || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	source := "observed"
	if !found {
		source = "assumed"
	}

	s.mu.Lock()
	s.sim = sim
	s.pdxPath = in.PdxPath
	s.dataDir = dataDir
	s.bundleID = bundleID
	s.scratchDir = scratch
	s.mu.Unlock()

	out := LaunchSimulatorOutput{BundleID: bundleID, DataDir: dataDir, DataDirSource: source}
	if found {
		return nil, out, nil
	}

	// Deliberately not an error, and deliberately loud. Launching a game whose
	// harness is not installed yet is a normal step - the flow is build, launch,
	// setup, relaunch - so failing here would break it. But if the directory is
	// genuinely wrong, every harness-dependent tool after this times out with
	// nothing naming the cause, so the diagnosis goes out now, on the first call,
	// while it is still attached to the thing that caused it.
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: sdk.DataDirDiagnostic(bundleID, tried)}},
	}, out, nil
}
