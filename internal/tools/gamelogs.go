package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetGameLogsInput struct {
	TailN int `json:"tail_n,omitempty" jsonschema:"number of trailing entries to return; 0 or omitted returns everything still retained (the harness keeps two 256KB generations)"`
}

type GameLogEntry struct {
	Type    string `json:"type"` // "print" or "error"
	Message string `json:"message"`
	Ms      int64  `json:"ms"`
}

type GetGameLogsOutput struct {
	Entries []GameLogEntry `json:"entries"`
}

// gameLogsFile is one JSON object per line, appended by the Lua harness.
const gameLogsFile = "game_logs.jsonl"

// gameLogsPrevFile is the generation the harness rotated aside at its size
// cap. Read first, since it is older.
const gameLogsPrevFile = "game_logs.1.jsonl"

// legacyGameLogsFile is what pre-JSONL harnesses wrote. It is not read, only
// detected, so a stale vendored harness is reported instead of looking quiet.
const legacyGameLogsFile = "game_logs.json"

// getGameLogs reads the log files directly rather than via a round trip, so it
// still works when the harness is unresponsive after a game error.
func (s *Server) getGameLogs(_ context.Context, _ *mcp.CallToolRequest, in GetGameLogsInput) (*mcp.CallToolResult, GetGameLogsOutput, error) {
	dataDir, err := s.requireDataDir()
	if err != nil {
		result, wrapErr := handleRoundTripErr(err)
		return result, GetGameLogsOutput{}, wrapErr
	}

	mcpDir := filepath.Join(dataDir, "mcp")

	// Each generation is parsed separately so torn-final-line tolerance applies
	// to the end of each file. Older first, so tail_n is chronological.
	var entries []GameLogEntry
	if prev, err := os.ReadFile(filepath.Join(mcpDir, gameLogsPrevFile)); err == nil {
		parsed, parseErr := ParseGameLogs(prev)
		if parseErr != nil {
			return nil, GetGameLogsOutput{}, fmt.Errorf("parsing %s: %w", gameLogsPrevFile, parseErr)
		}
		entries = parsed
	} else if !os.IsNotExist(err) {
		return nil, GetGameLogsOutput{}, fmt.Errorf("reading %s: %w", gameLogsPrevFile, err)
	}

	path := filepath.Join(mcpDir, gameLogsFile)
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		// A reader can land between rotation's rename and the next append.
		if len(entries) > 0 {
			return nil, GetGameLogsOutput{Entries: tailEntries(entries, in.TailN)}, nil
		}

		legacy := filepath.Join(dataDir, "mcp", legacyGameLogsFile)
		if _, statErr := os.Stat(legacy); statErr == nil {
			return errorResult(fmt.Sprintf(
				"found %s but no %s: this game's vendored mcp_harness.lua predates this server, "+
					"so its logs are being written in a format that is no longer read. "+
					"Re-run the setup tool on the game's source directory to update its harness copy.",
				legacy, gameLogsFile)), GetGameLogsOutput{}, nil
		}
		// Nothing logged yet, or the game does not use the harness capture.
		return nil, GetGameLogsOutput{}, nil
	}
	if err != nil {
		return nil, GetGameLogsOutput{}, fmt.Errorf("reading %s: %w", path, err)
	}

	current, err := ParseGameLogs(b)
	if err != nil {
		return nil, GetGameLogsOutput{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	entries = append(entries, current...)

	return nil, GetGameLogsOutput{Entries: tailEntries(entries, in.TailN)}, nil
}

// tailEntries returns the last n entries, or all of them when n is not positive.
func tailEntries(entries []GameLogEntry, n int) []GameLogEntry {
	if n > 0 && n < len(entries) {
		return entries[len(entries)-n:]
	}
	return entries
}

// ParseGameLogs decodes one entry per line, skipping a torn final line since
// the file is read unlocked while the game appends. Exported for contracttest.
func ParseGameLogs(b []byte) ([]GameLogEntry, error) {
	var lines [][]byte
	scanner := bufio.NewScanner(bytes.NewReader(b))
	// A traceback can exceed the default 64KB line limit.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		if line := bytes.TrimSpace(scanner.Bytes()); len(line) > 0 {
			lines = append(lines, bytes.Clone(line))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	entries := make([]GameLogEntry, 0, len(lines))
	for i, line := range lines {
		var entry GameLogEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			if i == len(lines)-1 {
				break // Torn final line, see above.
			}
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}
