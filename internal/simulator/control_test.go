package simulator

import (
	"errors"
	"testing"
)

// The contract from issue #35, asserted here rather than only on Windows
// because processControl is shared code and none of this is platform specific.
//
// TestStopAfterProcessAlreadyExitedDoesNotError reaches the same sequence
// through Wait and Stop, but it discards Stop's error and only proves nothing
// panics, which the buggy version also managed. This counts the calls.
func TestProcessControlStopAfterReleaseIsANoOp(t *testing.T) {
	var stops, releases int
	c := &processControl{
		stopFunc:    func() error { stops++; return nil },
		releaseFunc: func() error { releases++; return nil },
	}

	if err := c.release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	if err := c.release(); err != nil {
		t.Fatalf("second release: %v", err)
	}
	if err := c.stop(); err != nil {
		t.Fatalf("stop after release: %v", err)
	}

	if releases != 1 {
		t.Errorf("releaseFunc ran %d times, want 1. A second CloseHandle targets whatever "+
			"has since been given that handle value.", releases)
	}
	if stops != 0 {
		t.Errorf("stopFunc ran %d times after release, want 0. TerminateJobObject on a "+
			"closed handle can kill an unrelated process tree.", stops)
	}
}

// A failed stop must stay retryable. Marking it stopped regardless would turn
// one transient failure into a Simulator nothing can ever kill.
func TestProcessControlFailedStopIsRetryable(t *testing.T) {
	boom := errors.New("terminate failed")
	var stops int
	c := &processControl{
		stopFunc: func() error {
			stops++
			if stops == 1 {
				return boom
			}
			return nil
		},
	}

	if err := c.stop(); !errors.Is(err, boom) {
		t.Fatalf("first stop err = %v, want %v", err, boom)
	}
	if err := c.stop(); err != nil {
		t.Fatalf("retried stop: %v", err)
	}
	if stops != 2 {
		t.Errorf("stopFunc ran %d times, want 2: a failed stop must be retryable", stops)
	}
}

// Stop falls back to killProcess when there is no platform control. On Unix the
// two paths have the same effect, so only a control that records being used can
// tell them apart.
func TestStopPrefersTheControlOverKillProcess(t *testing.T) {
	sim, err := Launch(testShell(t), "-c", "sleep 30")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	t.Cleanup(func() { _ = killProcess(sim.cmd); _ = sim.Wait() })

	var stops int
	sim.control = &processControl{stopFunc: func() error { stops++; return nil }}
	if err := sim.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if stops != 1 {
		t.Errorf("control.stop ran %d times, want 1; Stop took the killProcess fallback "+
			"even though a control was set", stops)
	}
}

// Wait releases the platform control. On Windows that is the CloseHandle that
// frees the job object, so skipping it leaks a kernel handle per launch.
func TestWaitReleasesTheControl(t *testing.T) {
	sim, err := Launch(testShell(t), "-c", "true")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	var releases int
	sim.control = &processControl{releaseFunc: func() error { releases++; return nil }}
	if err := sim.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if releases != 1 {
		t.Errorf("control.release ran %d times, want 1", releases)
	}
}

// A release failure surfaces when the process itself exited cleanly. Otherwise
// the process error is the more useful one and wins.
func TestWaitReportsAReleaseFailure(t *testing.T) {
	boom := errors.New("closing the job object failed")
	sim, err := Launch(testShell(t), "-c", "true")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	sim.control = &processControl{releaseFunc: func() error { return boom }}
	if err := sim.Wait(); !errors.Is(err, boom) {
		t.Fatalf("Wait err = %v, want %v", err, boom)
	}
}
