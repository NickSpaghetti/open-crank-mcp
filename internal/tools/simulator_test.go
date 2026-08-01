package tools

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/NickSpaghetti/open-crank-mcp/internal/harness"
)

func TestStopSimulatorWhenNotRunning(t *testing.T) {
	s := &Server{}
	result, _, err := s.stopSimulator(context.Background(), nil, StopSimulatorInput{})
	if err != nil {
		t.Fatalf("stopSimulator: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("stopSimulator() result = %v, want an IsError result", result)
	}
}

func TestStopSimulatorWhenRunning(t *testing.T) {
	s := newTestServer(t)
	result, _, err := s.stopSimulator(context.Background(), nil, StopSimulatorInput{})
	if err != nil {
		t.Fatalf("stopSimulator: %v", err)
	}
	if result != nil {
		t.Fatalf("stopSimulator() result = %v, want nil (success)", result)
	}
	if s.sim != nil {
		t.Fatal("stopSimulator: s.sim is still set after stopping")
	}
}

func TestRestartSimulatorWhenNotRunning(t *testing.T) {
	s := &Server{}
	result, _, err := s.restartSimulator(context.Background(), nil, RestartSimulatorInput{})
	if err != nil {
		t.Fatalf("restartSimulator: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("restartSimulator() result = %v, want an IsError result", result)
	}
}

func TestRestartSimulatorWhenRunning(t *testing.T) {
	s := newTestServer(t)
	oldSim := s.sim

	result, out, err := s.restartSimulator(context.Background(), nil, RestartSimulatorInput{})
	if err != nil {
		t.Fatalf("restartSimulator: %v", err)
	}
	if result != nil {
		t.Fatalf("restartSimulator() result = %v, want nil (success)", result)
	}
	if out.BundleID != "com.example.test" {
		t.Fatalf("restartSimulator() BundleID = %q, want %q", out.BundleID, "com.example.test")
	}
	if s.sim == nil || s.sim == oldSim {
		t.Fatal("restartSimulator: s.sim was not replaced with a new instance")
	}
}

func TestGetStatusWhenNotRunning(t *testing.T) {
	s := &Server{}
	_, out, err := s.getStatus(context.Background(), nil, GetStatusInput{})
	if err != nil {
		t.Fatalf("getStatus: %v", err)
	}
	if out.Running {
		t.Fatal("getStatus().Running = true, want false")
	}
	if out.HarnessReachable {
		t.Fatal("getStatus().HarnessReachable = true, want false")
	}
}

func TestGetStatusWhenRunning(t *testing.T) {
	s := newTestServer(t)
	_, out, err := s.getStatus(context.Background(), nil, GetStatusInput{})
	if err != nil {
		t.Fatalf("getStatus: %v", err)
	}
	if !out.Running {
		t.Fatal("getStatus().Running = false, want true")
	}
	if out.BundleID != "com.example.test" {
		t.Fatalf("getStatus().BundleID = %q, want %q", out.BundleID, "com.example.test")
	}
	if !out.HarnessReachable {
		t.Fatal("getStatus().HarnessReachable = false, want true (newTestServer creates dataDir/mcp)")
	}
}

// harnessFS whose fingerprints the tests below compare against. The same
// stand-in shape setup_test uses, so a fingerprint computed here is stable and
// does not depend on the real harness content.
func statusTestServer(t *testing.T) *Server {
	t.Helper()
	s := newTestServer(t)
	s.harnessFS = testHarnessFS()
	return s
}

// Nothing observed yet must produce no warning. get_status is called before
// anything else touches the harness, and inventing a verdict from no evidence
// would be a false alarm on every fresh server.
func TestGetStatusNoHarnessWarningBeforeAnyRoundTrip(t *testing.T) {
	s := statusTestServer(t)
	_, out, err := s.getStatus(context.Background(), nil, GetStatusInput{})
	if err != nil {
		t.Fatalf("getStatus: %v", err)
	}
	if out.HarnessWarning != "" {
		t.Fatalf("HarnessWarning = %q, want empty when no round trip has happened", out.HarnessWarning)
	}
}

// A harness reporting the fingerprint this server ships is current, and must not
// be warned about.
func TestGetStatusNoHarnessWarningForCurrentHarness(t *testing.T) {
	s := statusTestServer(t)
	current, err := harness.LuaFingerprint(s.harnessFS)
	if err != nil {
		t.Fatalf("LuaFingerprint: %v", err)
	}
	s.harnessVersion, s.harnessVersionSeen = current, true

	_, out, err := s.getStatus(context.Background(), nil, GetStatusInput{})
	if err != nil {
		t.Fatalf("getStatus: %v", err)
	}
	if out.HarnessWarning != "" {
		t.Fatalf("HarnessWarning = %q, want empty for the current harness", out.HarnessWarning)
	}
}

// The case that would have caught this entire class of bug: a game carrying a
// harness copy from before version reporting existed. Both real vendored games
// were in exactly this state.
func TestGetStatusWarnsAboutPreVersioningHarness(t *testing.T) {
	s := statusTestServer(t)
	s.harnessVersion, s.harnessVersionSeen = "", true

	_, out, err := s.getStatus(context.Background(), nil, GetStatusInput{})
	if err != nil {
		t.Fatalf("getStatus: %v", err)
	}
	if out.HarnessWarning == "" {
		t.Fatal("HarnessWarning is empty for a harness that reports no version, want a warning")
	}
	if !strings.Contains(out.HarnessWarning, "setup") {
		t.Fatalf("HarnessWarning = %q, want it to name setup as the remedy", out.HarnessWarning)
	}
}

// A stale-but-versioned harness: the fingerprint is real, just not ours.
func TestGetStatusWarnsAboutForeignFingerprint(t *testing.T) {
	s := statusTestServer(t)
	s.harnessVersion, s.harnessVersionSeen = "deadbeef1234", true

	_, out, err := s.getStatus(context.Background(), nil, GetStatusInput{})
	if err != nil {
		t.Fatalf("getStatus: %v", err)
	}
	if !strings.Contains(out.HarnessWarning, "deadbeef1234") {
		t.Fatalf("HarnessWarning = %q, want it to quote the fingerprint the game reported", out.HarnessWarning)
	}
	if !strings.Contains(out.HarnessWarning, "setup") {
		t.Fatalf("HarnessWarning = %q, want it to name setup as the remedy", out.HarnessWarning)
	}
}

// A copy that was never stamped at all - hand-copied rather than installed by
// setup. Distinguished from the stale cases because the remedy reads differently:
// the file was never processed, rather than processed by an older version.
func TestGetStatusWarnsAboutUnstampedHarness(t *testing.T) {
	s := statusTestServer(t)
	s.harnessVersion, s.harnessVersionSeen = harness.VersionPlaceholder, true

	_, out, err := s.getStatus(context.Background(), nil, GetStatusInput{})
	if err != nil {
		t.Fatalf("getStatus: %v", err)
	}
	if !strings.Contains(out.HarnessWarning, "by hand") {
		t.Fatalf("HarnessWarning = %q, want it to identify an unstamped, hand-copied harness", out.HarnessWarning)
	}
}

// A broken binary must not be reported as a stale game. If the embedded harness
// cannot be read, the fault is here, and the message has to say so.
func TestGetStatusReportsAnUnreadableEmbeddedHarness(t *testing.T) {
	s := newTestServer(t)
	s.harnessFS = fstest.MapFS{} // no harness sources at all
	s.harnessVersion, s.harnessVersionSeen = "whatever", true

	_, out, err := s.getStatus(context.Background(), nil, GetStatusInput{})
	if err != nil {
		t.Fatalf("getStatus: %v", err)
	}
	if !strings.Contains(out.HarnessWarning, "this server's own embedded harness") {
		t.Fatalf("HarnessWarning = %q, want it to blame the server rather than the game", out.HarnessWarning)
	}
}

// Every successful round trip records what answered, so get_status can report it
// without a round trip of its own.
func TestRoundTripRecordsTheHarnessVersion(t *testing.T) {
	s := statusTestServer(t)
	startFakeHarness(t, s.dataDir, map[string]any{"status": "ok", "harness_version": "abc123abc123"})

	if _, err := s.roundTrip(harness.Command{Type: harness.CmdPing}); err != nil {
		t.Fatalf("roundTrip: %v", err)
	}
	if !s.harnessVersionSeen {
		t.Fatal("harnessVersionSeen = false after a successful round trip")
	}
	if s.harnessVersion != "abc123abc123" {
		t.Fatalf("harnessVersion = %q, want the value the harness reported", s.harnessVersion)
	}
}

// Recorded even when the harness reported a failure: which harness answered is
// true regardless of what it said, and a stale one is a plausible cause.
func TestRoundTripRecordsTheHarnessVersionOnFailure(t *testing.T) {
	s := statusTestServer(t)
	startFakeHarness(t, s.dataDir, map[string]any{
		"status": "error", "error": "nope", "harness_version": "abc123abc123",
	})

	if _, err := s.roundTrip(harness.Command{Type: harness.CmdPing}); err == nil {
		t.Fatal("roundTrip succeeded, want the harness's reported failure")
	}
	if s.harnessVersion != "abc123abc123" {
		t.Fatalf("harnessVersion = %q, want it recorded even though the harness reported an error", s.harnessVersion)
	}
}
