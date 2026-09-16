package doctor

import (
	"fmt"
	"time"

	"github.com/NickSpaghetti/open-crank-mcp/internal/simulator"
)

// launchSettle is how long the Simulator gets to prove it is going to stay up.
//
// Five seconds because that is what cmd/smoke-check has always waited, and this
// is an extraction rather than a retuning. It is a GUI app: exiting inside that
// window means it failed, staying up means nothing is obviously wrong.
const launchSettle = 5 * time.Second

// reapGiveUp caps how long a failed Stop waits for the process to die before
// giving up on reaping the Wait goroutine. Only reached when the kill itself
// failed, which is already an error path.
const reapGiveUp = 2 * time.Second

// Launch starts the Simulator, gives it launchSettle to stay up, stops it, and
// reports anything its output complained about.
//
// The Xvfb setup around it deliberately
// did not come with it: starting a display is about the environment a check runs
// in, not about the check, and the two callers differ there. smoke-check runs
// headless in a container and brings its own; `-doctor` runs on a desktop that
// already has one.
func Launch(simBin string) error {
	sim, err := simulator.Launch(simBin, "")
	if err != nil {
		return fmt.Errorf("launching simulator: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- sim.Wait() }()

	select {
	case err := <-done:
		return fmt.Errorf("simulator exited early (%v), expected it to keep running:\n%s", err, sim.Output())
	case <-time.After(launchSettle):
		// Still running - expected for a GUI app. Force-kill it:
		// PlaydateSimulator doesn't exit on SIGTERM.
		if err := sim.Stop(); err != nil {
			// Bounded rather than an unconditional wait. Stop failing means the
			// kill failed, so the process may still be alive and Wait may never
			// return; blocking here would hang a diagnostic whose output someone
			// is waiting on. This reaps the goroutine when the process did die
			// and gives up when it did not.
			select {
			case <-done:
			case <-time.After(reapGiveUp):
			}
			return fmt.Errorf("stopping simulator: %w", err)
		}
		// Kill succeeded, so Wait returns. Drained before Output is read below,
		// so the process has finished writing.
		<-done
	}

	return checkForErrors(sim.Output())
}
