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

// restartSimulator holds mu across the whole stop-then-relaunch, rather than
// taking it three times around the outside of each step.
//
// The lock used to be released between Stop and Launch, which left a window
// where s.sim pointed at a process that had already been killed: two concurrent
// restarts could both get past the nil check and both launch, leaving one
// Simulator running that nothing holds a handle to and therefore nothing can
// ever stop. The MCP go-sdk dispatches tool calls concurrently by default, so
// "two at once" is the normal case here rather than a hypothetical.
//
// Holding it across a Stop/Wait/Launch means other tools block for as long as
// that takes, which is the intended behaviour: there is no useful answer any of
// them could give about a simulator that is mid-restart.
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

	newSim, err := simulator.Launch(s.simulatorBin(), s.pdxPath, s.dataDir)
	if err != nil {
		return nil, RestartSimulatorOutput{}, fmt.Errorf("relaunching simulator: %w", err)
	}
	s.sim = newSim

	return nil, RestartSimulatorOutput{BundleID: s.bundleID, DataDir: s.dataDir}, nil
}

type GetStatusInput struct{}

type GetStatusOutput struct {
	Running          bool   `json:"running"`
	BundleID         string `json:"bundle_id,omitempty"`
	HarnessReachable bool   `json:"harness_reachable"`
	// HarnessWarning is set when the game's vendored harness copy does not match
	// what this binary ships. Only the warning is exposed, not the raw
	// fingerprint and not a nullable "unknown": a nullable field in a tool schema
	// is what broke a real client once (see readSaveDataOutputSchema), and the
	// tri-state belongs inside the server rather than in the caller's lap.
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

// harnessDriftWarning describes a game whose vendored harness copy is not the one
// this binary ships, or returns "" when it is.
//
// Deliberately not a harness round trip of its own: get_status has to keep
// answering while a game's frame loop is wedged, which is most of what it is for,
// so it reports what some earlier call already observed.
//
// The fingerprints are recomputed per call rather than cached. This is a SHA-256
// over about 20KB of embedded bytes, on a tool a human or agent invokes
// occasionally, so caching it would be machinery in exchange for nothing
// measurable.
func (s *Server) harnessDriftWarning(reported string) string {
	current, err := harness.IsCurrentFingerprint(s.harnessFS, reported)
	if err != nil {
		// The embedded harness could not be read, which is a broken binary rather
		// than a stale game. Say that instead of blaming the game.
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
