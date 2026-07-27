package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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

func writeGameLogs(t *testing.T, dataDir string, entries []GameLogEntry) {
	t.Helper()
	b, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("marshaling entries: %v", err)
	}
	path := filepath.Join(dataDir, "mcp", "game_logs.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
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
