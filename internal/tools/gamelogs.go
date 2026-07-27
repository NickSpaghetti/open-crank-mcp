package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetGameLogsInput struct {
	TailN int `json:"tail_n,omitempty" jsonschema:"number of trailing entries to return; 0 or omitted returns everything"`
}

type GameLogEntry struct {
	Type    string `json:"type"` // "print" or "error"
	Message string `json:"message"`
	Ms      int64  `json:"ms"`
}

type GetGameLogsOutput struct {
	Entries []GameLogEntry `json:"entries"`
}

// getGameLogs reads mcp/game_logs.json directly, the same direct-file-access
// pattern readSaveData uses (see savedata.go) rather than a harness
// round-trip - deliberately, so this keeps working in the exact scenario it
// exists to diagnose: an unresponsive harness after the game's own code threw
// an uncaught error (see mcp_harness.lua's mcp.run()).
func (s *Server) getGameLogs(_ context.Context, _ *mcp.CallToolRequest, in GetGameLogsInput) (*mcp.CallToolResult, GetGameLogsOutput, error) {
	dataDir, err := s.requireDataDir()
	if err != nil {
		result, wrapErr := handleRoundTripErr(err)
		return result, GetGameLogsOutput{}, wrapErr
	}

	path := filepath.Join(dataDir, "mcp", "game_logs.json")
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		// The game hasn't printed/errored yet, or isn't using the harness's
		// print()/mcp.run() capture at all - not an error, just nothing to
		// report, matching get_logs' own empty-output behavior.
		return nil, GetGameLogsOutput{}, nil
	}
	if err != nil {
		return nil, GetGameLogsOutput{}, fmt.Errorf("reading %s: %w", path, err)
	}

	var entries []GameLogEntry
	if err := json.Unmarshal(b, &entries); err != nil {
		return nil, GetGameLogsOutput{}, fmt.Errorf("parsing %s as JSON: %w", path, err)
	}
	if in.TailN > 0 && in.TailN < len(entries) {
		entries = entries[len(entries)-in.TailN:]
	}
	return nil, GetGameLogsOutput{Entries: entries}, nil
}
