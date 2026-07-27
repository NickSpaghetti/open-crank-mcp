// Command smoke-check confirms the Playdate SDK's shared libraries resolve,
// pdc runs, and PlaydateSimulator launches cleanly under Xvfb without
// crashing or logging an error - the environment-health check for the full
// simulator Docker image.
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

	xvfb, err := simulator.Launch("Xvfb", ":99", "-screen", "0", "1280x800x24")
	if err != nil {
		return fmt.Errorf("launching Xvfb: %w", err)
	}
	defer func() {
		_ = xvfb.Stop()
		_ = xvfb.Wait()
	}()
	time.Sleep(1 * time.Second)

	if err := os.Setenv("DISPLAY", ":99"); err != nil {
		return fmt.Errorf("setting DISPLAY: %w", err)
	}

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

func checkSharedLibraries(binPath string) error {
	out, err := exec.Command("ldd", binPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ldd %s: %w\n%s", binPath, err, out)
	}
	if notFoundPattern.Match(out) {
		return fmt.Errorf("missing shared libraries:\n%s", out)
	}
	return nil
}

var notFoundPattern = regexp.MustCompile(`not found`)

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
