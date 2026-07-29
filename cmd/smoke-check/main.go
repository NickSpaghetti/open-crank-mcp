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
	"regexp"
	"time"

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
	sdkPath := os.Getenv("PLAYDATE_SDK_PATH")
	if sdkPath == "" {
		return fmt.Errorf("PLAYDATE_SDK_PATH is not set")
	}
	simBin := sdkPath + "/bin/PlaydateSimulator"
	pdcBin := sdkPath + "/bin/pdc"

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

// Matches both the correctly-spelled and the typo'd form SDL2 itself uses
// ("could not be initalized") - seen directly in this project's own SDL2
// audio-driver debugging, not a hypothetical.
var errorPattern = regexp.MustCompile(`(?i)could not be initi?alized|error|not found`)

func checkForErrors(output string) error {
	if errorPattern.MatchString(output) {
		return fmt.Errorf("simulator reported an error:\n%s", output)
	}
	return nil
}
