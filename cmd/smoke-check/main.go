// Command smoke-check confirms the Playdate SDK's shared libraries resolve, pdc
// runs, and PlaydateSimulator launches without crashing or logging an error.
// The environment-health check: run it when the SDK moves, the image changes, or
// a host install is new.
//
// The two platform-specific parts are in display_*.go and libs_*.go. On Linux it
// runs under a throwaway Xvfb and greps ldd; elsewhere the desktop is already
// there and there is no ldd to grep. run() itself has no platform branch.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/NickSpaghetti/open-crank-mcp/internal/sdk"
	"github.com/NickSpaghetti/open-crank-mcp/internal/simulator"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
	fmt.Println("smoke check passed")
}

func run() error {
	// Resolved rather than read from the environment. This used to require
	// PLAYDATE_SDK_PATH and build its paths by string concatenation, which was
	// invisible while the only caller was the container that always sets it - and
	// which made `make smoke-check-native` impossible to use for the exact thing
	// it exists to check.
	paths, err := sdk.Resolve(sdk.OSEnv())
	if err != nil {
		return err
	}
	fmt.Printf("SDK: %s (via %s)\n", paths.Root, paths.RootSource)
	simBin := paths.SimulatorBin
	pdcBin := paths.PDC

	if err := checkSharedLibraries(simBin); err != nil {
		return err
	}

	out, err := exec.Command(pdcBin, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("pdc --version: %w\n%s", err, out)
	}
	fmt.Print(string(out))

	stopDisplay, err := startDisplay()
	if err != nil {
		return err
	}
	defer stopDisplay()

	sim, err := simulator.Launch(simBin, "")
	if err != nil {
		return fmt.Errorf("launching simulator: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- sim.Wait() }()

	select {
	case err := <-done:
		return fmt.Errorf("simulator exited early (%v), expected it to keep running:\n%s", err, sim.Output())
	case <-time.After(5 * time.Second):
		// Still running after 5s - expected for a GUI app. Force-kill it:
		// PlaydateSimulator doesn't exit on SIGTERM.
		if err := sim.Stop(); err != nil {
			return fmt.Errorf("stopping simulator: %w", err)
		}
		<-done
	}

	if err := checkForErrors(sim.Output()); err != nil {
		return err
	}

	return nil
}

// Both the correctly-spelled and the typo'd form SDL2 itself uses ("could not
// be initalized") are listed - the typo was seen directly in this project's own
// SDL2 audio-driver debugging, not a hypothetical.
var errorMarkers = []string{
	"could not be initalized",
	"could not be initialized",
	"error",
	"not found",
}

func checkForErrors(output string) error {
	lower := strings.ToLower(output)
	for _, marker := range errorMarkers {
		if strings.Contains(lower, marker) {
			return fmt.Errorf("simulator reported an error:\n%s", output)
		}
	}
	return nil
}
