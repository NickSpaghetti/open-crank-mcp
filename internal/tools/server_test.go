package tools

import (
	"errors"
	"testing"
)

func TestAsFloat(t *testing.T) {
	if got := asFloat(float64(3.5)); got != 3.5 {
		t.Fatalf("asFloat(3.5) = %v, want 3.5", got)
	}
	if got := asFloat("not a number"); got != 0 {
		t.Fatalf("asFloat(non-number) = %v, want 0", got)
	}
	if got := asFloat(nil); got != 0 {
		t.Fatalf("asFloat(nil) = %v, want 0", got)
	}
}

func TestAsString(t *testing.T) {
	if got := asString("hello"); got != "hello" {
		t.Fatalf("asString(\"hello\") = %q, want %q", got, "hello")
	}
	if got := asString(42); got != "" {
		t.Fatalf("asString(non-string) = %q, want empty", got)
	}
}

func TestAsBool(t *testing.T) {
	if got := asBool(true); got != true {
		t.Fatalf("asBool(true) = %v, want true", got)
	}
	if got := asBool("not a bool"); got != false {
		t.Fatalf("asBool(non-bool) = %v, want false", got)
	}
}

func TestHandleRoundTripErrNotRunning(t *testing.T) {
	result, err := handleRoundTripErr(errNotRunning)
	if err != nil {
		t.Fatalf("handleRoundTripErr(errNotRunning) error = %v, want nil", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("handleRoundTripErr(errNotRunning) result = %v, want an IsError result", result)
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
	if _, err := s.roundTrip(map[string]any{"type": "ping"}); !errors.Is(err, errNotRunning) {
		t.Fatalf("roundTrip() error = %v, want errNotRunning", err)
	}
}

func TestRoundTripAssignsIncrementingIDs(t *testing.T) {
	s := newTestServer(t)
	startFakeHarness(t, s.dataDir, map[string]any{"status": "ok"})
	if _, err := s.roundTrip(map[string]any{"type": "ping"}); err != nil {
		t.Fatalf("roundTrip: %v", err)
	}
	if s.nextID != 1 {
		t.Fatalf("nextID = %d, want 1 after one roundTrip call", s.nextID)
	}
}
