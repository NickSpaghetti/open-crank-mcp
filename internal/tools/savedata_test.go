package tools

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestReadSaveDataWhenNotRunning(t *testing.T) {
	s := &Server{}
	result, _, err := s.readSaveData(context.Background(), nil, ReadSaveDataInput{})
	if err != nil {
		t.Fatalf("readSaveData: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("readSaveData() result = %v, want an IsError result", result)
	}
}

func TestReadSaveDataListsFilesExcludingMcp(t *testing.T) {
	s := newTestServer(t)
	if err := os.WriteFile(filepath.Join(s.dataDir, "save.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(s.dataDir, "highscores.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, out, err := s.readSaveData(context.Background(), nil, ReadSaveDataInput{})
	if err != nil {
		t.Fatalf("readSaveData: %v", err)
	}
	got := append([]string(nil), out.Files...)
	sort.Strings(got)
	want := []string{"highscores.json", "save.json"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("readSaveData().Files = %v, want %v (mcp/ excluded)", got, want)
	}
}

func TestReadSaveDataReadsAndParsesAFile(t *testing.T) {
	s := newTestServer(t)
	if err := os.WriteFile(filepath.Join(s.dataDir, "save.json"), []byte(`{"score":42}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, out, err := s.readSaveData(context.Background(), nil, ReadSaveDataInput{Filename: "save.json"})
	if err != nil {
		t.Fatalf("readSaveData: %v", err)
	}
	data, ok := out.Data.(map[string]any)
	if !ok {
		t.Fatalf("readSaveData().Data = %v (%T), want a map", out.Data, out.Data)
	}
	if data["score"] != float64(42) {
		t.Fatalf(`readSaveData().Data["score"] = %v, want 42`, data["score"])
	}
}

func TestReadSaveDataMissingFileIsAnError(t *testing.T) {
	s := newTestServer(t)
	if _, _, err := s.readSaveData(context.Background(), nil, ReadSaveDataInput{Filename: "missing.json"}); err == nil {
		t.Fatal("readSaveData: expected an error for a missing file, got nil")
	}
}

func TestWriteSaveDataWhenNotRunning(t *testing.T) {
	s := &Server{}
	result, _, err := s.writeSaveData(context.Background(), nil, WriteSaveDataInput{Filename: "save.json", Data: map[string]any{}})
	if err != nil {
		t.Fatalf("writeSaveData: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("writeSaveData() result = %v, want an IsError result", result)
	}
}

func TestWriteSaveDataWritesJSON(t *testing.T) {
	s := newTestServer(t)
	_, _, err := s.writeSaveData(context.Background(), nil, WriteSaveDataInput{
		Filename: "save.json",
		Data:     map[string]any{"score": 42.0},
	})
	if err != nil {
		t.Fatalf("writeSaveData: %v", err)
	}

	b, err := os.ReadFile(filepath.Join(s.dataDir, "save.json"))
	if err != nil {
		t.Fatalf("reading written file: %v", err)
	}
	if string(b) != `{"score":42}` {
		t.Fatalf("written file = %q, want %q", string(b), `{"score":42}`)
	}
}
