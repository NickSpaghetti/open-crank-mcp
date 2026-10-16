package simulator

import (
	"errors"
	"testing"
)

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

// A failed stop must stay retryable.
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

// Wait releases the platform control; on Windows skipping it leaks a job handle per launch.
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

// A release failure surfaces only when the process itself exited cleanly.
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
