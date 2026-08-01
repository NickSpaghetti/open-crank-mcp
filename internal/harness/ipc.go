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
//
// Any response.json already sitting there is removed first. Without that, a
// leftover from a round trip that already timed out is the first thing the next
// WaitForResponse sees, and it returns it as this command's answer before the
// harness has even read the command - measured at 2ms against a real game, with
// the previous call's payload. Clearing it here is half the fix; the id check in
// WaitForResponse is the other half, for the response that lands mid-wait.
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
//
// Its check function requires content that parses, not merely a non-empty file.
// Both harnesses now publish by writing a temp file and renaming it into place, so
// a response that exists is complete - but a game carrying an older vendored
// harness still writes in place, where a reader can see a zero-length file (after
// the truncating open) or a short one (mid-write). Neither is an answer, and
// neither ends the wait.
//
// Whether a short read actually happens was measured rather than assumed, and it
// did not reproduce: 240 calls against a real game returning a 512KB response, on
// Linux/overlayfs with SDK 3.1.1, produced no partial reads. So this is latent
// rather than live on that platform. It is handled anyway because the cost is a
// branch, the previous handling was wrong in two independent ways (it ended the
// wait early *and* deleted the evidence), and the platforms native mode will add
// have not been measured at all.
//
// A response that does not stay removed would be worse than one that never
// arrives, so removal happens for both outcomes that end interest in a file: the
// answer we wanted, and a complete answer belonging to someone else.
//
// A response carrying some other id is discarded and the wait continues. That is
// what the id was always for (docs/ROADMAP.md: "a stale leftover response from a
// previous run is easy to detect and ignore") and it had never been implemented:
// one round trip that outlived its timeout left its answer on disk, and the next
// call returned it, silently, as its own. Reproduced against a real game -
// set_crank timed out at 5.005s and the following press_button consumed its
// answer in 3ms.
//
// An empty id is accepted rather than treated as a mismatch, and that is
// load-bearing, not defensive: when the C harness fails to parse a command it
// answers with an empty id, because it never gets as far as copying one
// (mcp_parse_command bails before the id is read). Rejecting those would turn
// every C-side parse failure into a five-second timeout instead of the error
// message it is trying to hand back. Verified on the wire, not assumed.
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
			// Content that does not parse is treated as not-ready-yet, not as an
			// answer. The file is deliberately left alone and the wait continues:
			// both harnesses publish by rename now, but a game carrying an older
			// vendored harness still writes in place, and there a short read is
			// content that is about to be finished. Deleting it would unlink a file
			// the writer still has open, sending the completing write to an orphaned
			// inode - so the answer would never arrive under its own name, and the
			// harness has already consumed command.json and will not resend.
			//
			// The error is kept so a genuinely malformed response is still
			// diagnosable when the deadline expires, rather than surfacing as a bare
			// timeout with no clue what was on disk.
			parseErr = fmt.Errorf("parsing response %q: %w", string(b), err)
			return false
		}
		parseErr = nil

		if got.ID != "" && wantID != "" && got.ID != wantID {
			// Someone else's answer, and complete - so unlike the parse failure
			// above it will never become ours, and leaving it would mean re-reading
			// the same stale response until the deadline. Removed, and the wait
			// continues.
			_ = os.Remove(path)
			return false
		}

		_ = os.Remove(path)
		resp = got
		return true
	}

	if err := awaitPath(path, timeout, check); err != nil {
		// A recorded parse failure explains the timeout better than the timeout
		// does: it means something was there and could not be read.
		if parseErr != nil {
			return Response{}, parseErr
		}
		return Response{}, err
	}
	return resp, nil
}
