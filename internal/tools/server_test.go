package tools

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NickSpaghetti/open-crank-mcp/internal/harness"
)

func TestHandleRoundTripErrNotRunning(t *testing.T) {
	result, err := handleRoundTripErr(errNotRunning)
	if err != nil {
		t.Fatalf("handleRoundTripErr(errNotRunning) error = %v, want nil", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("handleRoundTripErr(errNotRunning) result = %v, want an IsError result", result)
	}
}

// A harness-reported failure has to come back as a model-visible tool error,
// not as a transport error and not as a success. Before this it came back as a
// success: every tool discarded the status and error fields the harnesses were
// setting.
func TestHandleRoundTripErrHarnessError(t *testing.T) {
	result, err := handleRoundTripErr(errHarness)
	if err != nil {
		t.Fatalf("handleRoundTripErr(errHarness) error = %v, want nil", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("handleRoundTripErr(errHarness) result = %v, want an IsError result", result)
	}
}

func TestHandleRoundTripErrOtherError(t *testing.T) {
	other := errors.New("boom")
	result, err := handleRoundTripErr(other)
	if result != nil {
		t.Fatalf("handleRoundTripErr(other) result = %v, want nil", result)
	}
	if !errors.Is(err, other) {
		t.Fatalf("handleRoundTripErr(other) error = %v, want %v", err, other)
	}
}

func TestRequireDataDirWhenNotRunning(t *testing.T) {
	s := &Server{}
	if _, err := s.requireDataDir(); !errors.Is(err, errNotRunning) {
		t.Fatalf("requireDataDir() error = %v, want errNotRunning", err)
	}
}

func TestRequireDataDirWhenRunning(t *testing.T) {
	s := newTestServer(t)
	got, err := s.requireDataDir()
	if err != nil {
		t.Fatalf("requireDataDir: %v", err)
	}
	if got != s.dataDir {
		t.Fatalf("requireDataDir() = %q, want %q", got, s.dataDir)
	}
}

func TestRoundTripWhenNotRunning(t *testing.T) {
	s := &Server{}
	if _, err := s.roundTrip(harness.Command{Type: harness.CmdPing}); !errors.Is(err, errNotRunning) {
		t.Fatalf("roundTrip() error = %v, want errNotRunning", err)
	}
}

func TestRoundTripAssignsIncrementingIDs(t *testing.T) {
	s := newTestServer(t)
	startFakeHarness(t, s.dataDir, map[string]any{"status": "ok"})
	if _, err := s.roundTrip(harness.Command{Type: harness.CmdPing}); err != nil {
		t.Fatalf("roundTrip: %v", err)
	}
	if s.nextID != 1 {
		t.Fatalf("nextID = %d, want 1 after one roundTrip call", s.nextID)
	}
}

// The harness reporting a failure must reach the caller as errHarness, carrying
// the harness's own message. Both harnesses really do produce this shape - a
// bogus command type comes back as status "error" with "failed to parse command"
// (C) or "unknown command type" (Lua), verified on the wire.
func TestRoundTripSurfacesHarnessError(t *testing.T) {
	s := newTestServer(t)
	startFakeHarness(t, s.dataDir, map[string]any{
		"status": "error",
		"error":  "getDisplayFrame returned NULL",
	})
	_, err := s.roundTrip(harness.Command{Type: harness.CmdScreenshot})
	if !errors.Is(err, errHarness) {
		t.Fatalf("roundTrip() error = %v, want errHarness", err)
	}
	if !strings.Contains(err.Error(), "getDisplayFrame returned NULL") {
		t.Fatalf("roundTrip() error = %q, want it to carry the harness's own message", err)
	}
}

// A response already on disk when the call starts is the stale-response bug:
// against a real game a planted file came back as get_game_state's answer in
// 2ms, with the previous caller's payload. SendCommand clears it, so this call
// waits for its own answer instead.
func TestRoundTripIgnoresStaleResponseAlreadyOnDisk(t *testing.T) {
	s := newTestServer(t)
	respPath := filepath.Join(s.dataDir, "mcp", "response.json")
	stale := `{"id":"99999","status":"ok","state":{"marker":"stale"}}`
	if err := os.WriteFile(respPath, []byte(stale), 0o644); err != nil {
		t.Fatalf("planting stale response: %v", err)
	}
	startFakeHarness(t, s.dataDir, map[string]any{
		"status": "ok",
		"id":     "1",
		"state":  map[string]any{"marker": "fresh"},
	})

	resp, err := s.roundTrip(harness.Command{Type: harness.CmdState})
	if err != nil {
		t.Fatalf("roundTrip: %v", err)
	}
	if got := string(resp.State); !strings.Contains(got, "fresh") {
		t.Fatalf("roundTrip returned state %s, want the fresh response, not the stale one", got)
	}
	if resp.ID != "1" {
		t.Fatalf("roundTrip returned response id %q, want %q", resp.ID, "1")
	}
}
