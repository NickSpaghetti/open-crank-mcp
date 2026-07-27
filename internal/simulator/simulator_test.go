package simulator

import (
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
