// Package harness is the file-based IPC client for the Lua or C harness
// (mcp/command.json in, mcp/response.json out).
package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// bootstrapPollInterval is used only while the directory to watch does not
// exist yet (e.g. before the Simulator creates its data directory).
const bootstrapPollInterval = 1 * time.Millisecond

const backstopInterval = 10 * time.Millisecond

// awaitPath blocks until check() returns true, waking on fsnotify events for
// path's directory, with a periodic backstop re-check.
func awaitPath(path string, timeout time.Duration, check func() bool) error {
	deadline := time.Now().Add(timeout)
	if check() {
		return nil
	}

	cleanPath := filepath.Clean(path)

	watcher, err := newWatcherForExistingDir(filepath.Dir(path), deadline)
	if err != nil {
		return err
	}
	defer watcher.Close()

	ticker := time.NewTicker(backstopInterval)
	defer ticker.Stop()

	// Closes the race between the first check and the watch being armed.
	if check() {
		return nil
	}

	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("timed out after %s waiting for %s", timeout, path)
		}
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return fmt.Errorf("watcher closed while waiting for %s", path)
			}
			// fsnotify's event path and the caller's only agree by convention.
			if filepath.Clean(event.Name) == cleanPath && check() {
				return nil
			}
		case <-ticker.C:
			// macOS kqueue only sees writes after its per-file watch is armed
			// (just after Create), so a file created and filled in that window
			// produces no further event.
			if check() {
				return nil
			}
		case werr, ok := <-watcher.Errors:
			if !ok {
				return fmt.Errorf("watcher closed while waiting for %s", path)
			}
			return fmt.Errorf("watching %s: %w", path, werr)
		case <-time.After(remaining):
			return fmt.Errorf("timed out after %s waiting for %s", timeout, path)
		}
	}
}

// newWatcherForExistingDir arms an fsnotify watch on dir, polling until dir
// exists or deadline passes.
func newWatcherForExistingDir(dir string, deadline time.Time) (*fsnotify.Watcher, error) {
	for {
		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			return nil, fmt.Errorf("creating watcher for %s: %w", dir, err)
		}
		if err := watcher.Add(dir); err == nil {
			return watcher, nil
		}
		_ = watcher.Close()
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for directory %s to exist", dir)
		}
		time.Sleep(bootstrapPollInterval)
	}
}

// WaitForFile blocks until path exists as a regular file, or returns an
// error once timeout elapses.
func WaitForFile(path string, timeout time.Duration) error {
	return awaitPath(path, timeout, func() bool {
		info, err := os.Stat(path)
		return err == nil && !info.IsDir()
	})
}

// WaitForDir blocks until path exists as a directory, or returns an error
// once timeout elapses.
func WaitForDir(path string, timeout time.Duration) error {
	return awaitPath(path, timeout, func() bool {
		info, err := os.Stat(path)
		return err == nil && info.IsDir()
	})
}

// SendCommand writes cmd as mcp/command.json under dataDir, first removing
// any leftover response.json from a timed-out round trip so it cannot be
// read as this command's answer.
func SendCommand(dataDir string, cmd Command) error {
	b, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshaling command: %w", err)
	}
	mcpDir := filepath.Join(dataDir, "mcp")
	if err := os.MkdirAll(mcpDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", mcpDir, err)
	}
	if err := os.Remove(filepath.Join(mcpDir, "response.json")); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clearing stale response: %w", err)
	}
	path := filepath.Join(mcpDir, "command.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// WaitForResponse waits for the response to wantID to appear as
// mcp/response.json under dataDir, reads and removes it, and returns it decoded.
// Responses with another id are discarded. An empty id is accepted: the C
// harness answers parse failures with no id because it never read one.
func WaitForResponse(dataDir string, wantID string, timeout time.Duration) (Response, error) {
	path := filepath.Join(dataDir, "mcp", "response.json")

	var (
		resp     Response
		parseErr error
	)
	check := func() bool {
		b, err := os.ReadFile(path)
		if err != nil || len(b) == 0 {
			return false
		}

		var got Response
		if err := json.Unmarshal(b, &got); err != nil {
			// Not ready yet: older vendored harnesses write in place, so this may
			// be a short read. Deleting it would orphan the writer's open inode.
			parseErr = fmt.Errorf("parsing response %q: %w", string(b), err)
			return false
		}
		parseErr = nil

		if got.ID != "" && wantID != "" && got.ID != wantID {
			_ = os.Remove(path)
			return false
		}

		_ = os.Remove(path)
		resp = got
		return true
	}

	if err := awaitPath(path, timeout, check); err != nil {
		if parseErr != nil {
			return Response{}, parseErr
		}
		return Response{}, err
	}
	return resp, nil
}
