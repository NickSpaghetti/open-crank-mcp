package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGetGameLogsWhenNotRunning(t *testing.T) {
	s := &Server{}
	result, _, err := s.getGameLogs(context.Background(), nil, GetGameLogsInput{})
	if err != nil {
		t.Fatalf("getGameLogs: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("getGameLogs() result = %v, want an IsError result", result)
	}
}

func TestGetGameLogsWhenFileMissingReturnsEmpty(t *testing.T) {
	s := newTestServer(t)

	_, out, err := s.getGameLogs(context.Background(), nil, GetGameLogsInput{})
	if err != nil {
		t.Fatalf("getGameLogs: %v", err)
	}
	if len(out.Entries) != 0 {
		t.Fatalf("getGameLogs().Entries = %v, want empty", out.Entries)
	}
}

// writeGameLogs writes entries the way the Lua harness now does: one JSON
// object per line, appended.
func writeGameLogs(t *testing.T, dataDir string, entries []GameLogEntry) {
	t.Helper()
	var buf bytes.Buffer
	for _, e := range entries {
		b, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("marshaling entry: %v", err)
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}
	path := filepath.Join(dataDir, "mcp", gameLogsFile)
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestGetGameLogsReturnsAllEntriesByDefault(t *testing.T) {
	s := newTestServer(t)
	want := []GameLogEntry{
		{Type: "print", Message: "one", Ms: 1},
		{Type: "print", Message: "two", Ms: 2},
		{Type: "error", Message: "boom", Ms: 3},
	}
	writeGameLogs(t, s.dataDir, want)

	_, out, err := s.getGameLogs(context.Background(), nil, GetGameLogsInput{})
	if err != nil {
		t.Fatalf("getGameLogs: %v", err)
	}
	if !reflect.DeepEqual(out.Entries, want) {
		t.Fatalf("getGameLogs().Entries = %v, want %v", out.Entries, want)
	}
}

func TestGetGameLogsTailN(t *testing.T) {
	s := newTestServer(t)
	all := []GameLogEntry{
		{Type: "print", Message: "one", Ms: 1},
		{Type: "print", Message: "two", Ms: 2},
		{Type: "error", Message: "boom", Ms: 3},
	}
	writeGameLogs(t, s.dataDir, all)

	_, out, err := s.getGameLogs(context.Background(), nil, GetGameLogsInput{TailN: 2})
	if err != nil {
		t.Fatalf("getGameLogs: %v", err)
	}
	want := all[1:]
	if !reflect.DeepEqual(out.Entries, want) {
		t.Fatalf("getGameLogs(tail_n=2).Entries = %v, want %v", out.Entries, want)
	}
}

func TestGetGameLogsTailNLargerThanAvailableReturnsEverything(t *testing.T) {
	s := newTestServer(t)
	all := []GameLogEntry{{Type: "print", Message: "one", Ms: 1}}
	writeGameLogs(t, s.dataDir, all)

	_, out, err := s.getGameLogs(context.Background(), nil, GetGameLogsInput{TailN: 100})
	if err != nil {
		t.Fatalf("getGameLogs: %v", err)
	}
	if !reflect.DeepEqual(out.Entries, all) {
		t.Fatalf("getGameLogs(tail_n=100).Entries = %v, want %v", out.Entries, all)
	}
}

// A file caught mid-append ends in a partial line. That is normal here - nothing
// locks this file and the game keeps writing to it - so the complete entries
// before it must still be returned.
func TestGetGameLogsSkipsTornFinalLine(t *testing.T) {
	s := newTestServer(t)
	content := `{"type":"print","message":"one","ms":1}` + "\n" +
		`{"type":"print","message":"tw`
	path := filepath.Join(s.dataDir, "mcp", gameLogsFile)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, out, err := s.getGameLogs(context.Background(), nil, GetGameLogsInput{})
	if err != nil {
		t.Fatalf("getGameLogs: %v", err)
	}
	want := []GameLogEntry{{Type: "print", Message: "one", Ms: 1}}
	if !reflect.DeepEqual(out.Entries, want) {
		t.Fatalf("getGameLogs().Entries = %v, want %v", out.Entries, want)
	}
}

// Corruption anywhere but the last line is a real error, not something to skip:
// only a trailing partial write is explainable by the read racing the harness.
func TestGetGameLogsRejectsMalformedInteriorLine(t *testing.T) {
	s := newTestServer(t)
	content := "not json\n" + `{"type":"print","message":"one","ms":1}` + "\n"
	path := filepath.Join(s.dataDir, "mcp", gameLogsFile)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, _, err := s.getGameLogs(context.Background(), nil, GetGameLogsInput{}); err == nil {
		t.Fatal("getGameLogs() succeeded on a malformed interior line, want an error")
	}
}

// A traceback is a legitimate multi-KB entry, and bufio.Scanner's default line
// limit is 64KB - low enough to be worth proving we raised it, since the entry
// that would hit it is exactly the one you need most.
func TestGetGameLogsHandlesVeryLongTraceback(t *testing.T) {
	s := newTestServer(t)
	long := strings.Repeat("stack frame line\t", 8000)
	writeGameLogs(t, s.dataDir, []GameLogEntry{{Type: "error", Message: long, Ms: 7}})

	_, out, err := s.getGameLogs(context.Background(), nil, GetGameLogsInput{})
	if err != nil {
		t.Fatalf("getGameLogs: %v", err)
	}
	if len(out.Entries) != 1 || out.Entries[0].Message != long {
		t.Fatalf("getGameLogs() did not round-trip a %d-byte traceback entry (got %d entries)",
			len(long), len(out.Entries))
	}
}

// Blank lines are skipped rather than parsed. They are reachable: the harness
// truncates this file when it passes its size cap, and a reader can catch the
// moment between truncation and the next append.
func TestGetGameLogsSkipsBlankLines(t *testing.T) {
	s := newTestServer(t)
	content := "\n" + `{"type":"print","message":"one","ms":1}` + "\n\n   \n" +
		`{"type":"error","message":"boom","ms":2}` + "\n"
	path := filepath.Join(s.dataDir, "mcp", gameLogsFile)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, out, err := s.getGameLogs(context.Background(), nil, GetGameLogsInput{})
	if err != nil {
		t.Fatalf("getGameLogs: %v", err)
	}
	want := []GameLogEntry{
		{Type: "print", Message: "one", Ms: 1},
		{Type: "error", Message: "boom", Ms: 2},
	}
	if !reflect.DeepEqual(out.Entries, want) {
		t.Fatalf("getGameLogs().Entries = %v, want %v", out.Entries, want)
	}
}

// A game whose vendored harness predates this server writes the old file and
// nothing else. Returning an empty list there is the silent failure this whole
// change set exists to remove, so it is an error naming the remedy instead.
func TestGetGameLogsErrorsWhenOnlyTheLegacyFileExists(t *testing.T) {
	s := newTestServer(t)
	legacy := filepath.Join(s.dataDir, "mcp", legacyGameLogsFile)
	if err := os.WriteFile(legacy, []byte(`[{"type":"print","message":"old","ms":1}]`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	result, out, err := s.getGameLogs(context.Background(), nil, GetGameLogsInput{})
	if err != nil {
		t.Fatalf("getGameLogs: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("getGameLogs() result = %v, want an IsError result", result)
	}
	if len(out.Entries) != 0 {
		t.Fatalf("getGameLogs().Entries = %v, want none - the old format is deliberately not read", out.Entries)
	}
	// The message has to name the fix, not just the symptom: this is the only
	// place the caller learns that re-running setup is what resolves it.
	text := renderContent(t, result)
	if !strings.Contains(text, "setup") {
		t.Fatalf("error text = %q, want it to name the setup tool as the remedy", text)
	}
}

// A current harness deletes the legacy file, but a game that has just been
// re-setup and not yet relaunched can briefly have both. The current file wins,
// with no error - otherwise the tool would refuse to work for a game that is
// already fixed.
func TestGetGameLogsPrefersCurrentFileOverLegacy(t *testing.T) {
	s := newTestServer(t)
	legacy := filepath.Join(s.dataDir, "mcp", legacyGameLogsFile)
	if err := os.WriteFile(legacy, []byte(`[{"type":"print","message":"old","ms":1}]`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	want := []GameLogEntry{{Type: "print", Message: "new", Ms: 2}}
	writeGameLogs(t, s.dataDir, want)

	result, out, err := s.getGameLogs(context.Background(), nil, GetGameLogsInput{})
	if err != nil {
		t.Fatalf("getGameLogs: %v", err)
	}
	if result != nil && result.IsError {
		t.Fatalf("getGameLogs() reported an error despite a current log being present: %v", result)
	}
	if !reflect.DeepEqual(out.Entries, want) {
		t.Fatalf("getGameLogs().Entries = %v, want %v", out.Entries, want)
	}
}

// writePrevGameLogs writes the rotated generation, the way the harness's rename
// leaves it.
func writePrevGameLogs(t *testing.T, dataDir string, entries []GameLogEntry) {
	t.Helper()
	var buf bytes.Buffer
	for _, e := range entries {
		b, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("marshaling entry: %v", err)
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}
	path := filepath.Join(dataDir, "mcp", gameLogsPrevFile)
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// Both generations are returned, oldest first. Chronological order is the whole
// point: tail_n means "the most recent N", which is wrong if the halves are
// swapped.
func TestGetGameLogsReadsBothGenerationsInOrder(t *testing.T) {
	s := newTestServer(t)
	writePrevGameLogs(t, s.dataDir, []GameLogEntry{
		{Type: "print", Message: "older-1", Ms: 1},
		{Type: "print", Message: "older-2", Ms: 2},
	})
	writeGameLogs(t, s.dataDir, []GameLogEntry{
		{Type: "print", Message: "newer-1", Ms: 3},
		{Type: "print", Message: "newer-2", Ms: 4},
	})

	_, out, err := s.getGameLogs(context.Background(), nil, GetGameLogsInput{})
	if err != nil {
		t.Fatalf("getGameLogs: %v", err)
	}
	want := []GameLogEntry{
		{Type: "print", Message: "older-1", Ms: 1},
		{Type: "print", Message: "older-2", Ms: 2},
		{Type: "print", Message: "newer-1", Ms: 3},
		{Type: "print", Message: "newer-2", Ms: 4},
	}
	if !reflect.DeepEqual(out.Entries, want) {
		t.Fatalf("getGameLogs().Entries = %v, want %v", out.Entries, want)
	}
}

// The scenario that made truncation unacceptable, in miniature: the game crashed
// shortly after a rotation, so the traceback is alone in the current generation and
// everything leading up to it is in the rotated one. Both have to come back.
func TestGetGameLogsKeepsHistoryAcrossARotation(t *testing.T) {
	s := newTestServer(t)
	writePrevGameLogs(t, s.dataDir, []GameLogEntry{
		{Type: "print", Message: "the run-up to the failure", Ms: 1},
	})
	writeGameLogs(t, s.dataDir, []GameLogEntry{
		{Type: "error", Message: "stack traceback: ...", Ms: 2},
	})

	_, out, err := s.getGameLogs(context.Background(), nil, GetGameLogsInput{})
	if err != nil {
		t.Fatalf("getGameLogs: %v", err)
	}
	if len(out.Entries) != 2 {
		t.Fatalf("getGameLogs() returned %d entries, want 2 - the pre-rotation history is the "+
			"context a traceback is useless without: %v", len(out.Entries), out.Entries)
	}
	if out.Entries[0].Message != "the run-up to the failure" {
		t.Fatalf("first entry = %q, want the pre-rotation one", out.Entries[0].Message)
	}
}

// tail_n counts across the boundary, not per file. Asking for 3 of 4 must not
// return 3 from each generation, nor 3 from the wrong one.
func TestGetGameLogsTailNSpansGenerations(t *testing.T) {
	s := newTestServer(t)
	writePrevGameLogs(t, s.dataDir, []GameLogEntry{
		{Type: "print", Message: "older-1", Ms: 1},
		{Type: "print", Message: "older-2", Ms: 2},
	})
	writeGameLogs(t, s.dataDir, []GameLogEntry{
		{Type: "print", Message: "newer-1", Ms: 3},
		{Type: "print", Message: "newer-2", Ms: 4},
	})

	_, out, err := s.getGameLogs(context.Background(), nil, GetGameLogsInput{TailN: 3})
	if err != nil {
		t.Fatalf("getGameLogs: %v", err)
	}
	want := []GameLogEntry{
		{Type: "print", Message: "older-2", Ms: 2},
		{Type: "print", Message: "newer-1", Ms: 3},
		{Type: "print", Message: "newer-2", Ms: 4},
	}
	if !reflect.DeepEqual(out.Entries, want) {
		t.Fatalf("getGameLogs(tail_n=3).Entries = %v, want %v", out.Entries, want)
	}
}

// A rotation is a rename followed by the append that triggered it, so a reader can
// land in the gap and find only the rotated generation. That is real history, not
// an empty log.
func TestGetGameLogsReadsPrevGenerationAlone(t *testing.T) {
	s := newTestServer(t)
	writePrevGameLogs(t, s.dataDir, []GameLogEntry{{Type: "print", Message: "only-older", Ms: 1}})

	result, out, err := s.getGameLogs(context.Background(), nil, GetGameLogsInput{})
	if err != nil {
		t.Fatalf("getGameLogs: %v", err)
	}
	if result != nil && result.IsError {
		t.Fatalf("getGameLogs() reported an error with a rotated log present: %v", result)
	}
	if len(out.Entries) != 1 || out.Entries[0].Message != "only-older" {
		t.Fatalf("getGameLogs().Entries = %v, want the rotated generation's entry", out.Entries)
	}
}

// A torn line can only be at the very end of the *current* file. One in the
// rotated generation means real corruption - rotation happens between appends, so
// that file is always line-complete - and must be reported rather than skipped.
func TestGetGameLogsRejectsMalformedPrevGeneration(t *testing.T) {
	s := newTestServer(t)
	path := filepath.Join(s.dataDir, "mcp", gameLogsPrevFile)
	if err := os.WriteFile(path, []byte("not json\n{\"type\":\"print\",\"message\":\"x\",\"ms\":1}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	writeGameLogs(t, s.dataDir, []GameLogEntry{{Type: "print", Message: "current", Ms: 2}})

	if _, _, err := s.getGameLogs(context.Background(), nil, GetGameLogsInput{}); err == nil {
		t.Fatal("getGameLogs() succeeded with a corrupt rotated generation, want an error")
	}
}

// The stale-harness detection has to keep working when a rotated file is absent
// and only the pre-JSONL format exists.
func TestGetGameLogsStillDetectsLegacyWithNoGenerations(t *testing.T) {
	s := newTestServer(t)
	legacy := filepath.Join(s.dataDir, "mcp", legacyGameLogsFile)
	if err := os.WriteFile(legacy, []byte(`[{"type":"print","message":"old","ms":1}]`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	result, _, err := s.getGameLogs(context.Background(), nil, GetGameLogsInput{})
	if err != nil {
		t.Fatalf("getGameLogs: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("getGameLogs() result = %v, want an IsError result naming setup", result)
	}
}
