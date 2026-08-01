package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/NickSpaghetti/open-crank-mcp/internal/harness"
)

func TestPressButtonWhenNotRunning(t *testing.T) {
	s := &Server{}
	result, _, err := s.pressButton(context.Background(), nil, PressButtonInput{Button: "a", DurationMs: 100})
	if err != nil {
		t.Fatalf("pressButton: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("pressButton() result = %v, want an IsError result", result)
	}
}

func TestPressButtonWhenRunning(t *testing.T) {
	s := newTestServer(t)
	startFakeHarness(t, s.dataDir, map[string]any{"status": "ok"})

	result, _, err := s.pressButton(context.Background(), nil, PressButtonInput{Button: "a", DurationMs: 100})
	if err != nil {
		t.Fatalf("pressButton: %v", err)
	}
	if result != nil {
		t.Fatalf("pressButton() result = %v, want nil (success)", result)
	}
}

func TestSetCrankWhenNotRunning(t *testing.T) {
	s := &Server{}
	result, _, err := s.setCrank(context.Background(), nil, SetCrankInput{CrankAngle: 90})
	if err != nil {
		t.Fatalf("setCrank: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("setCrank() result = %v, want an IsError result", result)
	}
}

func TestSetCrankWhenRunning(t *testing.T) {
	s := newTestServer(t)
	startFakeHarness(t, s.dataDir, map[string]any{"status": "ok"})

	result, _, err := s.setCrank(context.Background(), nil,
		SetCrankInput{CrankAngle: 90, CrankDock: harness.DockDocked})
	if err != nil {
		t.Fatalf("setCrank: %v", err)
	}
	if result != nil {
		t.Fatalf("setCrank() result = %v, want nil (success)", result)
	}
}

// An unknown button name has to be rejected here, because no layer below does:
// both harnesses map an unrecognised name to no button and still answer "ok", so
// this used to report success and do nothing. "A" is the realistic case - the
// names are lower-case and nothing said so.
func TestPressButtonRejectsUnknownButton(t *testing.T) {
	s := newTestServer(t)
	for _, name := range []string{"x", "", "A", "start"} {
		result, _, err := s.pressButton(context.Background(), nil,
			PressButtonInput{Button: name, DurationMs: 40})
		if err != nil {
			t.Fatalf("pressButton(%q): %v", name, err)
		}
		if result == nil || !result.IsError {
			t.Fatalf("pressButton(%q) result = %v, want an IsError result", name, result)
		}
	}
}

// The six real names must still pass validation. Guards against a typo in
// ButtonNames turning every press into an error.
func TestPressButtonAcceptsEveryKnownButton(t *testing.T) {
	for _, name := range harness.ButtonNames {
		s := newTestServer(t)
		startFakeHarness(t, s.dataDir, map[string]any{"status": "ok"})
		result, _, err := s.pressButton(context.Background(), nil,
			PressButtonInput{Button: name, DurationMs: 10})
		if err != nil {
			t.Fatalf("pressButton(%q): %v", name, err)
		}
		if result != nil && result.IsError {
			t.Fatalf("pressButton(%q) reported an error: %v", name, result)
		}
	}
}

// An unrecognised dock mode is rejected here for the same reason an unrecognised
// button is: the harnesses resolve anything they do not know to "leave the dock
// alone" and answer success, so a typo would silently do nothing.
func TestSetCrankRejectsUnknownDockMode(t *testing.T) {
	s := newTestServer(t)
	for _, mode := range []string{"dock", "DOCKED", "true", "yes"} {
		result, _, err := s.setCrank(context.Background(), nil, SetCrankInput{CrankDock: mode})
		if err != nil {
			t.Fatalf("setCrank(%q): %v", mode, err)
		}
		if result == nil || !result.IsError {
			t.Fatalf("setCrank(crank_dock=%q) result = %v, want an IsError result", mode, result)
		}
	}
}

// Every mode a caller may send has to pass, including the empty string: omitting
// the field is the ordinary way to say "leave the dock alone".
func TestSetCrankAcceptsEveryDockMode(t *testing.T) {
	for _, mode := range append([]string{""}, harness.DockModes...) {
		s := newTestServer(t)
		startFakeHarness(t, s.dataDir, map[string]any{"status": "ok"})
		result, _, err := s.setCrank(context.Background(), nil, SetCrankInput{CrankDock: mode})
		if err != nil {
			t.Fatalf("setCrank(%q): %v", mode, err)
		}
		if result != nil && result.IsError {
			t.Fatalf("setCrank(crank_dock=%q) reported an error: %v", mode, result)
		}
	}
}

// An omitted mode must reach the harness as an explicit "unchanged" rather than an
// empty string, so a command on disk always names what it is doing. Asserted
// against the bytes the server actually wrote - the handler returns nothing that
// would reveal this, and a test that only checked the constant would prove nothing.
func TestSetCrankNormalisesOmittedDockMode(t *testing.T) {
	s := newTestServer(t)
	rec := startRecordingFakeHarness(t, s.dataDir)

	if _, _, err := s.setCrank(context.Background(), nil, SetCrankInput{CrankAngle: 45}); err != nil {
		t.Fatalf("setCrank: %v", err)
	}

	sent := rec.all()
	if len(sent) != 1 {
		t.Fatalf("harness saw %d commands, want 1", len(sent))
	}
	got := string(sent[0])
	if !strings.Contains(got, `"crank_dock":"`+harness.DockUnchanged+`"`) {
		t.Fatalf("command on the wire was %s, want an explicit crank_dock of %q", got, harness.DockUnchanged)
	}
}

// An explicit mode is passed through as given.
func TestSetCrankSendsTheModeItWasGiven(t *testing.T) {
	for _, mode := range []string{harness.DockDocked, harness.DockUndocked} {
		s := newTestServer(t)
		rec := startRecordingFakeHarness(t, s.dataDir)

		if _, _, err := s.setCrank(context.Background(), nil, SetCrankInput{CrankDock: mode}); err != nil {
			t.Fatalf("setCrank(%q): %v", mode, err)
		}
		sent := rec.all()
		if len(sent) != 1 {
			t.Fatalf("harness saw %d commands, want 1", len(sent))
		}
		if got := string(sent[0]); !strings.Contains(got, `"crank_dock":"`+mode+`"`) {
			t.Fatalf("command on the wire was %s, want crank_dock %q", got, mode)
		}
	}
}
