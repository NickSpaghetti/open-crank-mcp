package doctor

import (
	"fmt"
	"time"

	"github.com/NickSpaghetti/open-crank-mcp/internal/simulator"
)

// launchSettle is how long the Simulator gets to prove it is going to stay up.
const launchSettle = 5 * time.Second

// reapGiveUp caps how long a failed Stop waits for the process to die.
const reapGiveUp = 2 * time.Second

// Launch starts the Simulator, gives it launchSettle to stay up, stops it, and
// reports anything its output complained about. Callers provide the display.
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
		if err := sim.Stop(); err != nil {
			// The kill failed, so Wait may never return; don't hang the diagnostic.
			select {
			case <-done:
			case <-time.After(reapGiveUp):
			}
			return fmt.Errorf("stopping simulator: %w", err)
		}
		// Drain before reading Output so the process has finished writing.
		<-done
	}

	return checkForErrors(sim.Output())
}
