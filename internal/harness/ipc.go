// Package harness is the file-based IPC client for talking to either the
// Lua or C harness (mcp/command.json in, mcp/response.json out). The
// protocol is language-agnostic, so this client works with either.
package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// WaitForFile polls until path exists as a regular file, or returns an
// error once timeout elapses.
func WaitForFile(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s waiting for file %s", timeout, path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// WaitForDir polls until path exists as a directory, or returns an error
// once timeout elapses.
func WaitForDir(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s waiting for directory %s", timeout, path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// SendCommand writes cmd as mcp/command.json under dataDir. The mcp
// directory is normally already created by the harness's own init, but
// this creates it if missing rather than depend on that ordering.
func SendCommand(dataDir string, cmd map[string]any) error {
	b, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshaling command: %w", err)
	}
	mcpDir := filepath.Join(dataDir, "mcp")
	if err := os.MkdirAll(mcpDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", mcpDir, err)
	}
	path := filepath.Join(mcpDir, "command.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// WaitForResponse waits for mcp/response.json to appear under dataDir,
// reads and deletes it, and returns the decoded JSON. The response file is
// always removed once read, even if it fails to parse, so a malformed
// response doesn't wedge the next poll cycle.
func WaitForResponse(dataDir string, timeout time.Duration) (map[string]any, error) {
	path := filepath.Join(dataDir, "mcp", "response.json")
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, fmt.Errorf("timed out after %s waiting for file %s", timeout, path)
		}
		if err := WaitForFile(path, remaining); err != nil {
			return nil, err
		}

		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		if len(b) == 0 {
			// Every harness (Lua, C, and the Go test fake) writes this file
			// non-atomically - create/truncate, then write the content - so
			// a poll can land in that gap and see it empty. Keep waiting
			// instead of treating that as a parse failure.
			time.Sleep(10 * time.Millisecond)
			continue
		}
		_ = os.Remove(path)

		var resp map[string]any
		if err := json.Unmarshal(b, &resp); err != nil {
			return nil, fmt.Errorf("parsing response %q: %w", string(b), err)
		}
		return resp, nil
	}
}
