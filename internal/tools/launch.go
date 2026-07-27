package tools

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/NickSpaghetti/open-crank-mcp/internal/build"
	"github.com/NickSpaghetti/open-crank-mcp/internal/simulator"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

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

	s.mu.Lock()
	s.sim = sim
	s.pdxPath = in.PdxPath
	s.dataDir = dataDir
	s.bundleID = bundleID
	s.mu.Unlock()

	return nil, LaunchSimulatorOutput{BundleID: bundleID, DataDir: dataDir}, nil
}
