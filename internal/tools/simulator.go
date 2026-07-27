package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/NickSpaghetti/open-crank-mcp/internal/simulator"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type StopSimulatorInput struct{}
type StopSimulatorOutput struct{}

func (s *Server) stopSimulator(_ context.Context, _ *mcp.CallToolRequest, _ StopSimulatorInput) (*mcp.CallToolResult, StopSimulatorOutput, error) {
	s.mu.Lock()
	sim := s.sim
	s.mu.Unlock()
	if sim == nil {
		return notRunningResult(), StopSimulatorOutput{}, nil
	}

	_ = sim.Stop()
	_ = sim.Wait()

	s.mu.Lock()
	s.sim = nil
	s.mu.Unlock()

	return nil, StopSimulatorOutput{}, nil
}

type RestartSimulatorInput struct{}

type RestartSimulatorOutput struct {
	BundleID string `json:"bundle_id"`
	DataDir  string `json:"data_dir"`
}

func (s *Server) restartSimulator(_ context.Context, _ *mcp.CallToolRequest, _ RestartSimulatorInput) (*mcp.CallToolResult, RestartSimulatorOutput, error) {
	s.mu.Lock()
	sim := s.sim
	pdxPath := s.pdxPath
	dataDir := s.dataDir
	bundleID := s.bundleID
	simBin := filepath.Join(s.sdkPath, "bin", "PlaydateSimulator")
	s.mu.Unlock()

	if sim == nil {
		return notRunningResult(), RestartSimulatorOutput{}, nil
	}
	_ = sim.Stop()
	_ = sim.Wait()

	newSim, err := simulator.Launch(simBin, pdxPath, dataDir)
	if err != nil {
		s.mu.Lock()
		s.sim = nil
		s.mu.Unlock()
		return nil, RestartSimulatorOutput{}, fmt.Errorf("relaunching simulator: %w", err)
	}

	s.mu.Lock()
	s.sim = newSim
	s.mu.Unlock()

	return nil, RestartSimulatorOutput{BundleID: bundleID, DataDir: dataDir}, nil
}

type GetStatusInput struct{}

type GetStatusOutput struct {
	Running          bool   `json:"running"`
	BundleID         string `json:"bundle_id,omitempty"`
	HarnessReachable bool   `json:"harness_reachable"`
}

func (s *Server) getStatus(_ context.Context, _ *mcp.CallToolRequest, _ GetStatusInput) (*mcp.CallToolResult, GetStatusOutput, error) {
	s.mu.Lock()
	out := GetStatusOutput{Running: s.sim != nil, BundleID: s.bundleID}
	dataDir := s.dataDir
	s.mu.Unlock()

	if dataDir != "" {
		if info, err := os.Stat(filepath.Join(dataDir, "mcp")); err == nil && info.IsDir() {
			out.HarnessReachable = true
		}
	}
	return nil, out, nil
}
