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

// The duration press_button sends when the caller omitted one, asserted on the
// wire rather than on the constant, because the whole failure this prevents is a
// zero reaching a harness. Both harnesses expire overrides at the top of every
// frame and defer a fresh command's edge to the next one, so an override with a
// zero-length duration is gone before any frame can turn it into a press: the
// tool reports success and the game sees nothing.
//
// Deliberately not asserting the exact value. 100ms is a judgement about frame
// timing, not a contract, and a test pinning it would just have to be edited
// alongside it. What matters is that it is long enough to outlive a frame.
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

// An explicit duration is passed through untouched. Without this, a default
// applied unconditionally would look identical to one applied only when asked.
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

// hold_button must NOT get press_button's treatment, and this is the assertion that
// separates the two tools. The harnesses read a non-positive duration as "no expiry",
// so the zero has to arrive intact - substituting a default here would turn every hold
// into a tap that looked like it worked, which is the same class of silent failure
// set_crank had before its sentinel existed.
//
// Asserted on the bytes rather than through the input struct, because HoldButtonInput
// has no duration field to inspect: what is being tested is what Go put on the wire in
// its absence.
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
	// A hold is a press with no expiry, not its own command type. Worth asserting so
	// nobody "tidies" it into a new type the harnesses do not dispatch.
	if !strings.Contains(got, `"type":"`+harness.CmdPress+`"`) {
		t.Errorf("command on the wire was %s, want type %q", got, harness.CmdPress)
	}
	if !strings.Contains(got, `"button":"right"`) {
		t.Errorf("command on the wire was %s, want button right", got)
	}
}

// release_button, unlike hold_button, does substitute a duration. Sending the sentinel
// would force the button up for ever - the harnesses force not-pressed rather than
// merely clearing the override - leaving it deaf to the player with no way back.
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

// An explicit release duration is passed through, same as press_button's.
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

// hold_button and release_button validate button names too. They share
// buttonCommand with press_button, so this is guarding the wiring rather than the
// check: a handler that forgot to route through it would accept "A" and do nothing,
// which is exactly the bug press_button had.
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

// reset_input's whole job is the command type, since it carries nothing else. A
// mistyped type reaches a harness, is answered with "unknown command type", and the
// held crank it was meant to release stays held.
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

// A harness that reports a failure has to surface as a model-visible error rather than
// a silent success, for the new tools as much as the old ones. This is the defect the
// flat-protocol work fixed - status and error were produced by both harnesses and read
// by nothing - and a new tool is exactly where it would come back.
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

// set_crank must NOT get press_button's treatment. A crank is a position, and
// the harnesses read a non-positive duration as "hold it there", so substituting
// a default here would put the crank back on a timer and reintroduce the bug in
// a slower form: the angle would take, then silently revert a moment later.
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
