package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/NickSpaghetti/open-crank-mcp/internal/build"
	"github.com/NickSpaghetti/open-crank-mcp/internal/simulator"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Long enough for the Simulator to fail on startup and short enough not to be
// felt. Its SDL/audio initialisation failures land well inside this.
const startupGrace = 750 * time.Millisecond

type LaunchSimulatorInput struct {
	PdxPath string `json:"pdx_path" jsonschema:"path to the built .pdx"`
}

type LaunchSimulatorOutput struct {
	BundleID string `json:"bundle_id"`
	DataDir  string `json:"data_dir"`
}

func (s *Server) launchSimulator(_ context.Context, _ *mcp.CallToolRequest, in LaunchSimulatorInput) (*mcp.CallToolResult, LaunchSimulatorOutput, error) {
	bundleID, err := build.ReadBundleID(in.PdxPath)
	if err != nil {
		return nil, LaunchSimulatorOutput{}, fmt.Errorf("reading bundle ID: %w", err)
	}

	s.mu.Lock()
	dataDir := filepath.Join(s.sdkPath, "Disk", "Data", bundleID)
	simBin := filepath.Join(s.sdkPath, "bin", "PlaydateSimulator")
	s.mu.Unlock()

	// The Data directory is the extra CLI arg the Lua harness's screenshot
	// path needs (simulator.writeToFile resolves relative to the process's
	// own cwd otherwise, not the sandboxed Data directory).
	sim, err := simulator.Launch(simBin, in.PdxPath, dataDir)
	if err != nil {
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
		return nil, LaunchSimulatorOutput{}, fmt.Errorf(
			"the simulator quit during startup:\n%s", strings.TrimSpace(sim.Output()))
	}

	s.mu.Lock()
	s.sim = sim
	s.pdxPath = in.PdxPath
	s.dataDir = dataDir
	s.bundleID = bundleID
	s.mu.Unlock()

	return nil, LaunchSimulatorOutput{BundleID: bundleID, DataDir: dataDir}, nil
}
