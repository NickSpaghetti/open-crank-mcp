package simulator

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

// These use /bin/sh as a trivial stand-in process, not the real (huge)
// PlaydateSimulator binary. Only the generic launch/stop/wait/output
// lifecycle is under test here.

func TestLaunchCapturesOutput(t *testing.T) {
	sim, err := Launch("sh", "-c", "echo hello; echo world")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if err := sim.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	out := sim.Output()
	if !strings.Contains(out, "hello") || !strings.Contains(out, "world") {
		t.Fatalf("Output() = %q, want it to contain both hello and world", out)
	}
}

func TestLaunchForwardsExtraArgsAsPlaydateArgv(t *testing.T) {
	// pdxPath doesn't have to be a real pdx here. Launch just appends it
	// plus extraArgs as positional args to the child process, mirroring
	// how PlaydateSimulator forwards argv[1]=pdx path, argv[2:]=extras
	// into playdate.argv.
	sim, err := Launch("sh", "-c", `echo "$0:$1"`, "argA", "argB")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if err := sim.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	out := strings.TrimSpace(sim.Output())
	if out != "argA:argB" {
		t.Fatalf("Output() = %q, want %q", out, "argA:argB")
	}
}

func TestLaunchWithEmptyPdxPathOmitsIt(t *testing.T) {
	// An empty pdxPath means "launch with no game". extraArgs should be
	// passed as-is, not preceded by an empty positional argument.
	sim, err := Launch("sh", "", "-c", `echo "$0:$1"`, "onlyArg")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if err := sim.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	out := strings.TrimSpace(sim.Output())
	if out != "onlyArg:" {
		t.Fatalf("Output() = %q, want %q", out, "onlyArg:")
	}
}

func TestStopKillsRunningProcess(t *testing.T) {
	sim, err := Launch("sh", "-c", "sleep 5")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}

	if err := sim.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- sim.Wait() }()

	select {
	case <-done:
		// Killed processes report a non-nil error from Wait(); that's
		// expected and fine, the point is it returned promptly.
	case <-time.After(1 * time.Second):
		t.Fatal("Wait() did not return within 1s of Stop(). Process wasn't actually killed.")
	}
}

func TestOutputIsSafeToReadWhileProcessIsRunning(t *testing.T) {
	sim, err := Launch("sh", "-c", "while true; do echo tick; done")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer func() {
		_ = sim.Stop()
		_ = sim.Wait()
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			_ = sim.Output()
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("reading Output() concurrently with a running process did not complete within 5s")
	}
}

func TestStopAfterProcessAlreadyExitedDoesNotError(t *testing.T) {
	sim, err := Launch("sh", "-c", "true")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if err := sim.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	// The process is already reaped; Stop() must not panic even though
	// Signal() on an already-exited process typically errors.
	_ = sim.Stop()
}

// stdbuf is an exec wrapper: it sets LD_PRELOAD and _STDBUF_O, then execs the
// target, so the pid stays the Simulator's. That is what keeps Stop(), Exited() and
// the `pkill -f PlaydateSimulator` in cmd/shared-load working unchanged, and it is
// worth pinning because a wrapper that *forked* instead would silently break all
// three.
func TestLineBufferedCommandWrapsWhenStdbufExists(t *testing.T) {
	original := lookPath
	t.Cleanup(func() { lookPath = original })
	lookPath = func(string) (string, error) { return "/usr/bin/stdbuf", nil }

	name, args := lineBufferedCommand("/opt/sdk/bin/PlaydateSimulator", []string{"game.pdx", "/data"})
	if name != "/usr/bin/stdbuf" {
		t.Fatalf("command = %q, want the stdbuf path", name)
	}
	want := []string{"-oL", "/opt/sdk/bin/PlaydateSimulator", "game.pdx", "/data"}
	if !slices.Equal(args, want) {
		t.Fatalf("args = %v, want %v", args, want)
	}
}

// A stock macOS has no stdbuf, and native mode targets macOS. Falling back matters
// more than the buffering does: worse logs beat a Simulator that will not start.
func TestLineBufferedCommandFallsBackWithoutStdbuf(t *testing.T) {
	original := lookPath
	t.Cleanup(func() { lookPath = original })
	lookPath = func(string) (string, error) { return "", errors.New("not found") }

	name, args := lineBufferedCommand("/opt/sdk/bin/PlaydateSimulator", []string{"game.pdx"})
	if name != "/opt/sdk/bin/PlaydateSimulator" {
		t.Fatalf("command = %q, want the binary itself", name)
	}
	if !slices.Equal(args, []string{"game.pdx"}) {
		t.Fatalf("args = %v, want the arguments unchanged", args)
	}
}

// And Launch itself still works on a machine with no stdbuf, which is the part a
// wrong fallback would break.
func TestLaunchSucceedsWithoutStdbuf(t *testing.T) {
	original := lookPath
	t.Cleanup(func() { lookPath = original })
	lookPath = func(string) (string, error) { return "", errors.New("not found") }

	sim, err := Launch("sh", "", "-c", "sleep 30")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	t.Cleanup(func() {
		_ = sim.Stop()
		_ = sim.Wait()
	})
	if sim.Exited() {
		t.Fatal("Exited() = true immediately after Launch")
	}
}
