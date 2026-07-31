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
//
// It used to be a single JSON array that the harness rewrote in full on every
// print(), which cost 0.855ms per call at its 200-entry steady state against
// 0.0117ms for an append - 2.6% of a 33ms frame, per logged line, paid by the
// game. Reading a line-delimited file here is what let that go away. See
// appendGameLog in lua/mcp_harness.lua.
const gameLogsFile = "game_logs.jsonl"

// gameLogsPrevFile is the generation the harness renamed aside when the current
// file hit its size cap. Read first, since it is the older half of the history.
//
// Two generations exist so that a rotation never leaves the log empty. It used to
// truncate, which meant a crash shortly after a rotation showed its traceback with
// none of the run-up - the one thing this channel is read for. See rotateGameLog in
// lua/mcp_harness.lua.
const gameLogsPrevFile = "game_logs.1.jsonl"

// legacyGameLogsFile is what the pre-JSONL harness wrote: one JSON array,
// rewritten in full on every print().
//
// It is not read. Backward compatibility is deliberately not offered - the log is
// a debug buffer, regenerated on the next launch, and there is one developer. What
// is not acceptable is going quiet: a game whose vendored harness predates this
// server would otherwise report zero entries with no error, because the harness is
// a *copy* in the game's own source tree and nothing else would notice it had
// drifted. So its presence is detected purely to say so.
const legacyGameLogsFile = "game_logs.json"

// getGameLogs reads mcp/game_logs.jsonl directly, the same direct-file-access
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

	mcpDir := filepath.Join(dataDir, "mcp")

	// The rotated generation, if there is one. Parsed separately from the current
	// file rather than concatenating the bytes, which keeps the torn-final-line
	// tolerance scoped to one file each: glued together, a partial last line in the
	// older file would fuse with the first line of the newer one into a single
	// corrupt entry, and the tolerance would no longer be looking at the end of the
	// input where a real tear can only be.
	//
	// Order is load-bearing - older first. A caller asking for tail_n expects the
	// last N entries chronologically.
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
		// A rotation is a rename followed by the append that triggered it, so a
		// reader can land in between and see only the rotated generation. Its
		// entries are real history and are returned rather than discarded.
		if len(entries) > 0 {
			return nil, GetGameLogsOutput{Entries: tailEntries(entries, in.TailN)}, nil
		}

		// Before concluding "nothing logged yet", check whether the game is
		// writing the old file instead. If it is, this tool cannot do its job and
		// says so, rather than returning an empty list that looks like a quiet
		// game.
		legacy := filepath.Join(dataDir, "mcp", legacyGameLogsFile)
		if _, statErr := os.Stat(legacy); statErr == nil {
			return errorResult(fmt.Sprintf(
				"found %s but no %s: this game's vendored mcp_harness.lua predates this server, "+
					"so its logs are being written in a format that is no longer read. "+
					"Re-run the setup tool on the game's source directory to update its harness copy.",
				legacy, gameLogsFile)), GetGameLogsOutput{}, nil
		}
		// The game hasn't printed/errored yet, or isn't using the harness's
		// print()/mcp.run() capture at all - not an error, just nothing to
		// report, matching get_logs' own empty-output behavior.
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
//
// Its own function because the tail has to be taken after both generations are
// joined, and there are two return paths that need it. Taking it per file would
// return up to 2n entries, or n from the wrong generation.
func tailEntries(entries []GameLogEntry, n int) []GameLogEntry {
	if n > 0 && n < len(entries) {
		return entries[len(entries)-n:]
	}
	return entries
}

// ParseGameLogs decodes one entry per line.
//
// Exported so internal/contracttest can assert against the real Lua harness
// using this exact decoder rather than a second implementation of it - a
// duplicate is free to disagree with this one, which would make the contract
// test pass while get_game_logs is broken.
//
// A torn final line is skipped rather than treated as corruption: the harness
// appends and closes per call, but this file is read without any locking while
// the game is still running, so catching it mid-append is expected rather than
// exceptional. Skipping only the last line is what makes that safe - anything
// earlier in the file is already complete, so a malformed line there is a real
// error and still reported as one.
func ParseGameLogs(b []byte) ([]GameLogEntry, error) {
	var lines [][]byte
	scanner := bufio.NewScanner(bytes.NewReader(b))
	// A traceback is a legitimate multi-KB entry, so the default 64KB line
	// limit is raised rather than risking a real error being unreadable.
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
