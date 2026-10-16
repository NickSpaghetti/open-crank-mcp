package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/NickSpaghetti/open-crank-mcp/internal/harness"
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
	s.clearScratchLocked()
	s.mu.Unlock()

	return nil, StopSimulatorOutput{}, nil
}

type RestartSimulatorInput struct{}

type RestartSimulatorOutput struct {
	BundleID string `json:"bundle_id"`
	DataDir  string `json:"data_dir"`
}

// restartSimulator holds mu across the whole stop-then-relaunch so concurrent
// tool calls (the go-sdk dispatches them in parallel) cannot both launch.
func (s *Server) restartSimulator(_ context.Context, _ *mcp.CallToolRequest, _ RestartSimulatorInput) (*mcp.CallToolResult, RestartSimulatorOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sim == nil {
		return notRunningResult(), RestartSimulatorOutput{}, nil
	}
	_ = s.sim.Stop()
	_ = s.sim.Wait()
	s.sim = nil
	s.clearScratchLocked()

	scratch, err := newScratchDir(s.bundleID)
	if err != nil {
		return nil, RestartSimulatorOutput{}, err
	}
	newSim, err := simulator.Launch(s.simulatorBin(), filepath.ToSlash(s.pdxPath), filepath.ToSlash(scratch))
	if err != nil {
		_ = os.RemoveAll(scratch)
		return nil, RestartSimulatorOutput{}, fmt.Errorf("relaunching simulator: %w", err)
	}
	s.sim = newSim
	s.scratchDir = scratch

	return nil, RestartSimulatorOutput{BundleID: s.bundleID, DataDir: s.dataDir}, nil
}

type GetStatusInput struct{}

type GetStatusOutput struct {
	Running          bool   `json:"running"`
	BundleID         string `json:"bundle_id,omitempty"`
	HarnessReachable bool   `json:"harness_reachable"`
	// HarnessWarning is set when the game's harness copy does not match this
	// binary's. Not a nullable field: that schema shape broke a real client.
	HarnessWarning string `json:"harness_warning,omitempty"`
}

func (s *Server) getStatus(_ context.Context, _ *mcp.CallToolRequest, _ GetStatusInput) (*mcp.CallToolResult, GetStatusOutput, error) {
	s.mu.Lock()
	out := GetStatusOutput{Running: s.sim != nil, BundleID: s.bundleID}
	dataDir := s.dataDir
	version, versionSeen := s.harnessVersion, s.harnessVersionSeen
	s.mu.Unlock()

	if dataDir != "" {
		if info, err := os.Stat(filepath.Join(dataDir, "mcp")); err == nil && info.IsDir() {
			out.HarnessReachable = true
		}
	}
	if versionSeen {
		out.HarnessWarning = s.harnessDriftWarning(version)
	}
	return nil, out, nil
}

// harnessDriftWarning describes a game whose vendored harness copy is not the
// one this binary ships, or returns "". It uses the last observed version rather
// than a round trip, so get_status still answers when the game is wedged.
func (s *Server) harnessDriftWarning(reported string) string {
	current, err := harness.IsCurrentFingerprint(s.harnessFS, reported)
	if err != nil {
		return fmt.Sprintf("could not fingerprint this server's own embedded harness: %v", err)
	}
	if current {
		return ""
	}
	if reported == "" {
		return "this game's mcp_harness copy predates harness version reporting, so it is older than " +
			"this server. Re-run the setup tool on the game's source directory to update it."
	}
	if reported == harness.VersionPlaceholder {
		return "this game's mcp_harness copy carries an unsubstituted version placeholder, so it was " +
			"copied in by hand rather than by the setup tool. Re-run setup on the game's source " +
			"directory so it matches this server."
	}
	return fmt.Sprintf(
		"this game's mcp_harness copy (%s) differs from the harness this server ships. It is either "+
			"from an older version or locally modified. Re-run the setup tool on the game's source "+
			"directory to sync it.", reported)
}
