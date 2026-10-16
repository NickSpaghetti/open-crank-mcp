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

// startupGrace catches the Simulator quitting during startup (e.g. SDL/audio
// init failures).
const startupGrace = 750 * time.Millisecond

// dataDirGrace is only paid in full for a game with no harness; a harnessed
// game creates mcp/ almost immediately.
const dataDirGrace = 3 * time.Second

type LaunchSimulatorInput struct {
	PdxPath string `json:"pdx_path" jsonschema:"path to the built .pdx"`
}

type LaunchSimulatorOutput struct {
	BundleID string `json:"bundle_id"`
	DataDir  string `json:"data_dir"`
	// DataDirSource is "observed" or "assumed"; "assumed" explains later timeouts.
	DataDirSource string `json:"data_dir_source"`
}

// Where the Lua harness writes its screenshot, relative to the scratch
// directory. The C harness writes sandbox-relative instead.
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

	// Passed to the game as playdate.argv[2]. writeToFile takes a host path, so
	// this need not be the sandboxed data directory.
	scratch, err := newScratchDir(bundleID)
	if err != nil {
		return nil, LaunchSimulatorOutput{}, err
	}

	// ToSlash because the Lua harness joins this with "/", and already-vendored
	// harness copies cannot be fixed. Windows accepts forward slashes.
	sim, err := simulator.Launch(s.simulatorBin(), filepath.ToSlash(in.PdxPath), filepath.ToSlash(scratch))
	if err != nil {
		os.RemoveAll(scratch)
		return nil, LaunchSimulatorOutput{}, fmt.Errorf("launching simulator: %w", err)
	}

	// The Simulator can quit during startup for reasons unrelated to the game
	// (e.g. SDL_AUDIODRIVER names an audio backend that is not available).
	time.Sleep(startupGrace)
	if sim.Exited() {
		_ = sim.Wait()
		err := os.RemoveAll(scratch)
		if err != nil {
			return nil, LaunchSimulatorOutput{}, err
		}
		return nil, LaunchSimulatorOutput{}, fmt.Errorf(
			"the simulator quit during startup:\n%s", strings.TrimSpace(sim.Output()))
	}

	// Observe the data directory via the mcp/ dir the harness creates in its
	// sandbox (see sdk.FindDataDir), polling since harness init may lag.
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

	// Not an error: launch-before-setup is a normal flow. But a wrong data dir
	// makes every later tool time out, so diagnose it now.
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: sdk.DataDirDiagnostic(bundleID, tried)}},
	}, out, nil
}

// newScratchDir creates the directory handed to the game as playdate.argv[2].
func newScratchDir(bundleID string) (string, error) {
	scratch, err := os.MkdirTemp("", "open-crank-"+bundleID+"-")
	if err != nil {
		return "", fmt.Errorf("creating screenshot scratch directory: %w", err)
	}
	// mcp/ is created here because writeToFile will not create directories.
	if err := os.MkdirAll(filepath.Join(scratch, luaScreenshotRelDir), 0o755); err != nil {
		_ = os.RemoveAll(scratch)
		return "", fmt.Errorf("creating %s in scratch directory: %w", luaScreenshotRelDir, err)
	}
	return scratch, nil
}
