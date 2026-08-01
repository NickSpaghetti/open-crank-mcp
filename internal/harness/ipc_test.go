package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
	cmd := Command{ID: "1", Type: CmdPing}

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

// A response.json left over from an earlier round trip is what made one slow
// call desync every later one, so SendCommand clears it before writing the
// command rather than leaving it for the next WaitForResponse to find.
func TestSendCommandClearsStaleResponse(t *testing.T) {
	dir := t.TempDir()
	mcpDir := filepath.Join(dir, "mcp")
	if err := os.MkdirAll(mcpDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	stalePath := filepath.Join(mcpDir, "response.json")
	if err := os.WriteFile(stalePath, []byte(`{"id":"old","status":"ok"}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := SendCommand(dir, Command{ID: "2", Type: CmdPing}); err != nil {
		t.Fatalf("SendCommand: %v", err)
	}
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Fatal("response.json survived SendCommand, so the next wait would return it as this command's answer")
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

	resp, err := WaitForResponse(dir, "1", 2*time.Second)
	if err != nil {
		t.Fatalf("WaitForResponse: %v", err)
	}
	if resp.Status != StatusOK {
		t.Fatalf("resp = %+v, want status=ok", resp)
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

	resp, err := WaitForResponse(dir, "1", 2*time.Second)
	if err != nil {
		t.Fatalf("WaitForResponse: %v", err)
	}
	if resp.Status != StatusOK {
		t.Fatalf("resp = %+v, want status=ok", resp)
	}
}

// Content that never becomes valid JSON has to be reported, and reported with the
// content - a bare timeout would say only that nothing arrived, when in fact
// something did and could not be read.
//
// This test replaced one asserting the opposite shape: that a parse failure returns
// immediately and deletes the file. Both halves of that were wrong. Returning
// immediately spent none of the deadline the function already had, and deleting
// unlinked a file a writer might still have open, so a completing write could never
// land under its own name. The deletion's stated reason - stopping a leftover from
// wedging the next call - is handled by SendCommand, which clears response.json
// before every command.
func TestWaitForResponseReportsUnparseableContentAfterTheDeadline(t *testing.T) {
	dir := t.TempDir()
	mcpDir := filepath.Join(dir, "mcp")
	if err := os.MkdirAll(mcpDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	responsePath := filepath.Join(mcpDir, "response.json")
	if err := os.WriteFile(responsePath, []byte("not json"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Short, because this path now waits the deadline out on purpose.
	_, err := WaitForResponse(dir, "1", 200*time.Millisecond)
	if err == nil {
		t.Fatal("WaitForResponse: expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "not json") {
		t.Fatalf("error = %q, want it to quote the content it could not parse", err)
	}
}

// A short read is content that is about to be finished, so the wait has to survive
// it. Same shape as TestWaitForResponseToleratesEmptyFileMidWrite, one line
// different - that test proves the zero-length case was thought about, and this is
// the case it missed.
func TestWaitForResponseToleratesPartialFileMidWrite(t *testing.T) {
	dir := t.TempDir()
	mcpDir := filepath.Join(dir, "mcp")
	if err := os.MkdirAll(mcpDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	responsePath := filepath.Join(mcpDir, "response.json")

	// Non-empty but truncated: valid JSON is coming, it just is not all here.
	if err := os.WriteFile(responsePath, []byte(`{"id":"1","stat`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = os.WriteFile(responsePath, []byte(`{"id":"1","status":"ok"}`), 0o644)
	}()

	resp, err := WaitForResponse(dir, "1", 2*time.Second)
	if err != nil {
		t.Fatalf("WaitForResponse: %v", err)
	}
	if resp.Status != StatusOK {
		t.Fatalf("resp = %+v, want status=ok", resp)
	}
}

// The destructive half, asserted on its own: a partial read must leave the file
// alone. Deleting it unlinks a file the writer still has open, so the completing
// write goes to an orphaned inode and the response never appears - unrecoverable,
// since the harness has already consumed command.json.
func TestWaitForResponseDoesNotDeleteAPartialFile(t *testing.T) {
	dir := t.TempDir()
	mcpDir := filepath.Join(dir, "mcp")
	if err := os.MkdirAll(mcpDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	responsePath := filepath.Join(mcpDir, "response.json")
	if err := os.WriteFile(responsePath, []byte(`{"id":"1","stat`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := WaitForResponse(dir, "1", 150*time.Millisecond); err == nil {
		t.Fatal("WaitForResponse succeeded on content that never completed")
	}
	if _, statErr := os.Stat(responsePath); statErr != nil {
		t.Fatalf("the partial file was removed (%v); a writer still holding it open "+
			"would have nowhere to land its completing write", statErr)
	}
}

// A complete response belonging to someone else is removed, because it will never
// become ours and leaving it would mean re-reading it until the deadline. This is
// the case that genuinely needs the removal the parse-failure path gave up.
func TestWaitForResponseRemovesAMismatchedResponse(t *testing.T) {
	dir := t.TempDir()
	mcpDir := filepath.Join(dir, "mcp")
	if err := os.MkdirAll(mcpDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	responsePath := filepath.Join(mcpDir, "response.json")
	if err := os.WriteFile(responsePath, []byte(`{"id":"99","status":"ok"}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := WaitForResponse(dir, "1", 150*time.Millisecond); err == nil {
		t.Fatal("WaitForResponse returned a response for the wrong id")
	}
	if _, statErr := os.Stat(responsePath); !os.IsNotExist(statErr) {
		t.Fatal("a mismatched response was left on disk; the next wait would read it again")
	}
}

// Unparseable content followed by a valid response for a *different* id: the parse
// error must not survive as the reported failure once real content has been read
// and rejected on its merits.
func TestWaitForResponseClearsAStaleParseErrorOnceContentParses(t *testing.T) {
	dir := t.TempDir()
	mcpDir := filepath.Join(dir, "mcp")
	if err := os.MkdirAll(mcpDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	responsePath := filepath.Join(mcpDir, "response.json")
	if err := os.WriteFile(responsePath, []byte("not json"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	go func() {
		time.Sleep(40 * time.Millisecond)
		_ = os.WriteFile(responsePath, []byte(`{"id":"99","status":"ok"}`), 0o644)
	}()

	_, err := WaitForResponse(dir, "1", 400*time.Millisecond)
	if err == nil {
		t.Fatal("WaitForResponse: expected a timeout, got nil")
	}
	if strings.Contains(err.Error(), "not json") {
		t.Fatalf("error = %q, want the timeout rather than a parse error that was "+
			"superseded by content this call read and rejected", err)
	}
}

// A response carrying another call's id must be discarded and waited past, not
// returned. This is the correlation docs/ROADMAP.md always described and that had
// never been implemented: against a real game, set_crank timed out at 5.005s and
// the following press_button consumed its answer in 3ms.
func TestWaitForResponseSkipsMismatchedID(t *testing.T) {
	dir := t.TempDir()
	mcpDir := filepath.Join(dir, "mcp")
	if err := os.MkdirAll(mcpDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	responsePath := filepath.Join(mcpDir, "response.json")

	// Someone else's answer is already sitting there when the wait starts.
	if err := os.WriteFile(responsePath, []byte(`{"id":"7","status":"ok","error":"wrong one"}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	go func() {
		time.Sleep(80 * time.Millisecond)
		_ = os.WriteFile(responsePath, []byte(`{"id":"8","status":"ok"}`), 0o644)
	}()

	resp, err := WaitForResponse(dir, "8", 2*time.Second)
	if err != nil {
		t.Fatalf("WaitForResponse: %v", err)
	}
	if resp.ID != "8" {
		t.Fatalf("WaitForResponse returned the response for id %q, want %q", resp.ID, "8")
	}
}

// An empty id is accepted rather than rejected, and this is load-bearing: when
// the C harness fails to parse a command it answers with an empty id, because
// mcp_parse_command bails before the id is copied. Treating that as a mismatch
// would turn every C-side parse failure into a five-second timeout instead of the
// error message it is trying to return.
func TestWaitForResponseAcceptsEmptyID(t *testing.T) {
	dir := t.TempDir()
	mcpDir := filepath.Join(dir, "mcp")
	if err := os.MkdirAll(mcpDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	responsePath := filepath.Join(mcpDir, "response.json")
	if err := os.WriteFile(responsePath, []byte(`{"id":"","status":"error","error":"failed to parse command"}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	resp, err := WaitForResponse(dir, "3", 2*time.Second)
	if err != nil {
		t.Fatalf("WaitForResponse: %v", err)
	}
	if resp.Error != "failed to parse command" {
		t.Fatalf("resp.Error = %q, want the harness's parse-failure message", resp.Error)
	}
}

// A response whose id never matches must time out rather than return the wrong
// answer. This is the case the old code got wrong in the most damaging way: it
// returned it, immediately, as if it were the right one.
func TestWaitForResponseTimesOutRatherThanReturnWrongID(t *testing.T) {
	dir := t.TempDir()
	mcpDir := filepath.Join(dir, "mcp")
	if err := os.MkdirAll(mcpDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mcpDir, "response.json"),
		[]byte(`{"id":"99","status":"ok"}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := WaitForResponse(dir, "1", 200*time.Millisecond); err == nil {
		t.Fatal("WaitForResponse returned a response for the wrong id, want a timeout")
	}
}
