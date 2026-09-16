// Command smoke-check confirms the Playdate SDK's shared libraries resolve, pdc
// runs, and PlaydateSimulator launches without crashing or logging an error.
// The environment-health check: run it when the SDK moves, the image changes, or
// a host install is new.
//
// The checks themselves live in internal/doctor, because `open-crank-mcp -doctor`
// answers the same question for someone who installed this as an editor plugin
// and has neither this binary nor a Makefile. This is the shell around them, and
// it keeps the one thing that is genuinely its own: the throwaway Xvfb, which is
// about the environment a check runs in rather than the check. A desktop already
// has a display; a container does not.
//
// display_*.go is what remains platform-specific here. The ldd-versus-stat split
// moved with the check it belongs to.
package main

import (
	"fmt"
	"os"

	"github.com/NickSpaghetti/open-crank-mcp/internal/doctor"
	"github.com/NickSpaghetti/open-crank-mcp/internal/sdk"
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

	if err := doctor.SharedLibraries(simBin); err != nil {
		return err
	}

	out, err := doctor.PDCVersion(pdcBin)
	if err != nil {
		return err
	}
	fmt.Print(out)

	stopDisplay, err := startDisplay()
	if err != nil {
		return err
	}
	defer stopDisplay()

	return doctor.Launch(simBin)
}
