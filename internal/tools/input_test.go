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

// Asserted on the wire: a zero-length press expires before any frame turns it
// into an edge. The exact value is not pinned, only that it outlives a frame.
func TestPressButtonSuppliesADefaultDuration(t *testing.T) {
	s := newTestServer(t)
	got := startCapturingFakeHarness(t, s.dataDir, map[string]any{"status": "ok"})

	if _, _, err := s.pressButton(context.Background(), nil, PressButtonInput{Button: "a"}); err != nil {
		t.Fatalf("pressButton: %v", err)
	}

	cmd, ok := <-got
	if !ok {
		t.Fatal("the fake harness never saw a command")
	}
	if d, _ := cmd["duration_ms"].(float64); d < 34 {
		t.Errorf("press with no duration sent duration_ms=%v, want at least one frame's "+
			"worth (~33ms); anything shorter expires before the harness can synthesize the press", cmd["duration_ms"])
	}
}

func TestPressButtonKeepsAnExplicitDuration(t *testing.T) {
	s := newTestServer(t)
	got := startCapturingFakeHarness(t, s.dataDir, map[string]any{"status": "ok"})

	if _, _, err := s.pressButton(context.Background(), nil,
		PressButtonInput{Button: "a", DurationMs: 5000}); err != nil {
		t.Fatalf("pressButton: %v", err)
	}

	cmd, ok := <-got
	if !ok {
		t.Fatal("the fake harness never saw a command")
	}
	if d, _ := cmd["duration_ms"].(float64); d != 5000 {
		t.Errorf("duration_ms = %v, want the 5000 that was asked for", cmd["duration_ms"])
	}
}

// hold_button must send duration 0 intact, which the harnesses read as no expiry.
func TestHoldButtonSendsTheNoExpirySentinel(t *testing.T) {
	s := newTestServer(t)
	rec := startRecordingFakeHarness(t, s.dataDir)

	if _, _, err := s.holdButton(context.Background(), nil, HoldButtonInput{Button: "right"}); err != nil {
		t.Fatalf("holdButton: %v", err)
	}

	sent := rec.all()
	if len(sent) != 1 {
		t.Fatalf("harness saw %d commands, want 1", len(sent))
	}
	got := string(sent[0])
	if !strings.Contains(got, `"duration_ms":0`) {
		t.Errorf("command on the wire was %s, want duration_ms 0 so the harness holds the button", got)
	}
	if !strings.Contains(got, `"type":"`+harness.CmdPress+`"`) {
		t.Errorf("command on the wire was %s, want type %q", got, harness.CmdPress)
	}
	if !strings.Contains(got, `"button":"right"`) {
		t.Errorf("command on the wire was %s, want button right", got)
	}
}

// release_button substitutes a duration: forcing the button up with no expiry
// would leave it deaf to the player.
func TestReleaseButtonSuppliesADefaultDuration(t *testing.T) {
	s := newTestServer(t)
	got := startCapturingFakeHarness(t, s.dataDir, map[string]any{"status": "ok"})

	if _, _, err := s.releaseButton(context.Background(), nil,
		ReleaseButtonInput{Button: "a"}); err != nil {
		t.Fatalf("releaseButton: %v", err)
	}

	cmd, ok := <-got
	if !ok {
		t.Fatal("the fake harness never saw a command")
	}
	if cmd["type"] != harness.CmdRelease {
		t.Errorf("type = %v, want %q", cmd["type"], harness.CmdRelease)
	}
	if d, _ := cmd["duration_ms"].(float64); d < 34 {
		t.Errorf("release with no duration sent duration_ms=%v, want at least one frame's worth "+
			"(~33ms); anything shorter expires before the harness can synthesize the release", cmd["duration_ms"])
	}
}

func TestReleaseButtonKeepsAnExplicitDuration(t *testing.T) {
	s := newTestServer(t)
	got := startCapturingFakeHarness(t, s.dataDir, map[string]any{"status": "ok"})

	if _, _, err := s.releaseButton(context.Background(), nil,
		ReleaseButtonInput{Button: "a", DurationMs: 5000}); err != nil {
		t.Fatalf("releaseButton: %v", err)
	}

	cmd, ok := <-got
	if !ok {
		t.Fatal("the fake harness never saw a command")
	}
	if d, _ := cmd["duration_ms"].(float64); d != 5000 {
		t.Errorf("duration_ms = %v, want the 5000 that was asked for", cmd["duration_ms"])
	}
}

// hold_button and release_button must also route through buttonCommand's
// validation.
func TestHoldAndReleaseRejectUnknownButtons(t *testing.T) {
	for _, name := range []string{"x", "", "A", "start"} {
		s := newTestServer(t)

		result, _, err := s.holdButton(context.Background(), nil, HoldButtonInput{Button: name})
		if err != nil {
			t.Fatalf("holdButton(%q): %v", name, err)
		}
		if result == nil || !result.IsError {
			t.Errorf("holdButton(%q) result = %v, want an IsError result", name, result)
		}

		result, _, err = s.releaseButton(context.Background(), nil, ReleaseButtonInput{Button: name})
		if err != nil {
			t.Fatalf("releaseButton(%q): %v", name, err)
		}
		if result == nil || !result.IsError {
			t.Errorf("releaseButton(%q) result = %v, want an IsError result", name, result)
		}
	}
}

func TestHoldAndReleaseAcceptEveryKnownButton(t *testing.T) {
	for _, name := range harness.ButtonNames {
		s := newTestServer(t)
		startEchoingFakeHarness(t, s.dataDir)

		result, _, err := s.holdButton(context.Background(), nil, HoldButtonInput{Button: name})
		if err != nil {
			t.Fatalf("holdButton(%q): %v", name, err)
		}
		if result != nil && result.IsError {
			t.Errorf("holdButton(%q) reported an error: %v", name, result)
		}

		result, _, err = s.releaseButton(context.Background(), nil, ReleaseButtonInput{Button: name})
		if err != nil {
			t.Fatalf("releaseButton(%q): %v", name, err)
		}
		if result != nil && result.IsError {
			t.Errorf("releaseButton(%q) reported an error: %v", name, result)
		}
	}
}

func TestResetInputSendsTheResetCommand(t *testing.T) {
	s := newTestServer(t)
	rec := startRecordingFakeHarness(t, s.dataDir)

	if _, _, err := s.resetInput(context.Background(), nil, ResetInputInput{}); err != nil {
		t.Fatalf("resetInput: %v", err)
	}

	sent := rec.all()
	if len(sent) != 1 {
		t.Fatalf("harness saw %d commands, want 1", len(sent))
	}
	if got := string(sent[0]); !strings.Contains(got, `"type":"`+harness.CmdReset+`"`) {
		t.Fatalf("command on the wire was %s, want type %q", got, harness.CmdReset)
	}
}

func TestResetInputWhenNotRunning(t *testing.T) {
	s := &Server{}
	result, _, err := s.resetInput(context.Background(), nil, ResetInputInput{})
	if err != nil {
		t.Fatalf("resetInput: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("resetInput() result = %v, want an IsError result", result)
	}
}

func TestHoldButtonWhenNotRunning(t *testing.T) {
	s := &Server{}
	result, _, err := s.holdButton(context.Background(), nil, HoldButtonInput{Button: "a"})
	if err != nil {
		t.Fatalf("holdButton: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("holdButton() result = %v, want an IsError result", result)
	}
}

func TestReleaseButtonWhenNotRunning(t *testing.T) {
	s := &Server{}
	result, _, err := s.releaseButton(context.Background(), nil, ReleaseButtonInput{Button: "a"})
	if err != nil {
		t.Fatalf("releaseButton: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("releaseButton() result = %v, want an IsError result", result)
	}
}

func TestNewInputToolsSurfaceAHarnessError(t *testing.T) {
	s := newTestServer(t)
	startFakeHarness(t, s.dataDir, map[string]any{
		"status": "error", "error": "harness said no", "id": "1"})

	result, _, err := s.holdButton(context.Background(), nil, HoldButtonInput{Button: "a"})
	if err != nil {
		t.Fatalf("holdButton: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("holdButton() result = %v, want an IsError result", result)
	}
	if msg := renderContent(t, result); !strings.Contains(msg, "harness said no") {
		t.Errorf("result said %q, want the harness's own message in it", msg)
	}
}

// set_crank must not substitute a default: a non-positive duration means "hold
// it there", and a default would silently revert the angle.
func TestSetCrankSendsNoDurationWhenNoneGiven(t *testing.T) {
	s := newTestServer(t)
	got := startCapturingFakeHarness(t, s.dataDir, map[string]any{"status": "ok"})

	if _, _, err := s.setCrank(context.Background(), nil, SetCrankInput{CrankAngle: 90}); err != nil {
		t.Fatalf("setCrank: %v", err)
	}

	cmd, ok := <-got
	if !ok {
		t.Fatal("the fake harness never saw a command")
	}
	if d, present := cmd["duration_ms"]; present {
		if f, _ := d.(float64); f > 0 {
			t.Errorf("duration_ms = %v, want absent or non-positive so the harnesses hold the crank", d)
		}
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

// Both harnesses answer an unknown button with "ok", so it is rejected here.
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

// The harnesses treat an unknown dock mode as "unchanged" and answer success.
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

// An omitted mode reaches the wire as an explicit "unchanged".
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
