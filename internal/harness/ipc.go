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

	"github.com/fsnotify/fsnotify"
)

// bootstrapPollInterval only governs the rare, one-time-per-launch case
// where the directory a wait needs to watch doesn't exist yet (e.g.
// WaitForDir's own parent, before the Simulator has created its data
// directory). It never gates the per-tool-call hot path - SendCommand
// always creates mcp/ before WaitForResponse is ever called, so that wait
// never falls back to this.
const bootstrapPollInterval = 1 * time.Millisecond

// awaitPath blocks until check() returns true, waking up only when path's
// directory actually changes (via inotify, through fsnotify) rather than
// waking up on a timer to ask "yet?" every interval. Real stress testing
// against three real games (see docs/GOTCHAS.md) found a polling loop's
// interval wasn't the round-trip latency bottleneck - the Simulator's own
// ~30fps frame period was - but blocking on a real notification is still
// strictly better than polling: zero wasted wakeups while waiting, and no
// polling-interval-shaped detection delay stacked on top of whatever the
// frame period already costs.
func awaitPath(path string, timeout time.Duration, check func() bool) error {
	deadline := time.Now().Add(timeout)
	if check() {
		return nil
	}

	watcher, err := newWatcherForExistingDir(filepath.Dir(path), deadline)
	if err != nil {
		return err
	}
	defer watcher.Close()

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
			if event.Name == path && check() {
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

// newWatcherForExistingDir arms an fsnotify watch on dir, falling back to
// a short bootstrap poll only if dir itself doesn't exist yet - a rare,
// one-time race (e.g. the Simulator hasn't created its data directory
// yet), not the repeated per-tool-call wait this change targets.
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
// response doesn't wedge the next wait. Its check function requires
// non-empty content, not just existence - every harness (Lua, C, and the
// Go test fake) writes this file non-atomically (create/truncate, then
// write the content), and a Create event can fire before that write
// lands, so waiting on mere existence would busy-spin on an empty file
// instead of genuinely waiting for the following Write event.
func WaitForResponse(dataDir string, timeout time.Duration) (map[string]any, error) {
	path := filepath.Join(dataDir, "mcp", "response.json")

	var content []byte
	check := func() bool {
		b, err := os.ReadFile(path)
		if err != nil || len(b) == 0 {
			return false
		}
		content = b
		return true
	}

	if err := awaitPath(path, timeout, check); err != nil {
		return nil, err
	}
	_ = os.Remove(path)

	var resp map[string]any
	if err := json.Unmarshal(content, &resp); err != nil {
		return nil, fmt.Errorf("parsing response %q: %w", string(content), err)
	}
	return resp, nil
}
