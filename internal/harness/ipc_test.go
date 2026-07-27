package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWaitForFileSucceedsOnceCreated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ready.txt")

	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = os.WriteFile(path, []byte("x"), 0o644)
	}()

	if err := WaitForFile(path, 2*time.Second); err != nil {
		t.Fatalf("WaitForFile: %v", err)
	}
}

func TestWaitForFileTimesOut(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "never-created.txt")

	start := time.Now()
	err := WaitForFile(path, 200*time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("WaitForFile: expected a timeout error, got nil")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("WaitForFile took %s, expected to return promptly after its own timeout", elapsed)
	}
}

func TestWaitForFileIgnoresADirectoryOfTheSameName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "actually-a-dir")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	err := WaitForFile(path, 200*time.Millisecond)
	if err == nil {
		t.Fatal("WaitForFile: expected a timeout error for a directory, got nil")
	}
}

func TestWaitForDirSucceedsOnceCreated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ready-dir")

	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = os.Mkdir(path, 0o755)
	}()

	if err := WaitForDir(path, 2*time.Second); err != nil {
		t.Fatalf("WaitForDir: %v", err)
	}
}

func TestWaitForDirIgnoresARegularFileOfTheSameName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "actually-a-file")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	err := WaitForDir(path, 200*time.Millisecond)
	if err == nil {
		t.Fatal("WaitForDir: expected a timeout error for a regular file, got nil")
	}
}

func TestSendCommandWritesValidJSON(t *testing.T) {
	dir := t.TempDir()
	cmd := map[string]any{"id": "1", "type": "ping"}

	if err := SendCommand(dir, cmd); err != nil {
		t.Fatalf("SendCommand: %v", err)
	}

	b, err := os.ReadFile(filepath.Join(dir, "mcp", "command.json"))
	if err != nil {
		t.Fatalf("reading back command.json: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("command.json is not valid JSON: %v", err)
	}
	if got["id"] != "1" || got["type"] != "ping" {
		t.Fatalf("command.json round-tripped as %v, want %v", got, cmd)
	}
}

func TestWaitForResponseReadsAndDeletes(t *testing.T) {
	dir := t.TempDir()
	mcpDir := filepath.Join(dir, "mcp")
	if err := os.MkdirAll(mcpDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	responsePath := filepath.Join(mcpDir, "response.json")
	if err := os.WriteFile(responsePath, []byte(`{"id":"1","status":"ok"}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	resp, err := WaitForResponse(dir, 2*time.Second)
	if err != nil {
		t.Fatalf("WaitForResponse: %v", err)
	}
	if resp["status"] != "ok" {
		t.Fatalf("resp = %v, want status=ok", resp)
	}
	if _, err := os.Stat(responsePath); !os.IsNotExist(err) {
		t.Fatalf("response.json still exists after WaitForResponse, want it deleted")
	}
}

func TestWaitForResponseToleratesEmptyFileMidWrite(t *testing.T) {
	dir := t.TempDir()
	mcpDir := filepath.Join(dir, "mcp")
	if err := os.MkdirAll(mcpDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	responsePath := filepath.Join(mcpDir, "response.json")

	// Simulate a non-atomic writer (as every real harness is): the file
	// exists but is empty for a moment before its content lands.
	if err := os.WriteFile(responsePath, nil, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = os.WriteFile(responsePath, []byte(`{"id":"1","status":"ok"}`), 0o644)
	}()

	resp, err := WaitForResponse(dir, 2*time.Second)
	if err != nil {
		t.Fatalf("WaitForResponse: %v", err)
	}
	if resp["status"] != "ok" {
		t.Fatalf("resp = %v, want status=ok", resp)
	}
}

func TestWaitForResponseMalformedJSONStillDeletesFile(t *testing.T) {
	dir := t.TempDir()
	mcpDir := filepath.Join(dir, "mcp")
	if err := os.MkdirAll(mcpDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	responsePath := filepath.Join(mcpDir, "response.json")
	if err := os.WriteFile(responsePath, []byte("not json"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := WaitForResponse(dir, 2*time.Second)
	if err == nil {
		t.Fatal("WaitForResponse: expected a parse error, got nil")
	}
	if _, statErr := os.Stat(responsePath); !os.IsNotExist(statErr) {
		t.Fatal("response.json still exists after a parse failure, want it deleted so it can't wedge the next poll")
	}
}
