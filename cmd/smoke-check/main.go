// Command smoke-check confirms the Playdate SDK's shared libraries resolve, pdc
// runs, and PlaydateSimulator launches cleanly, wrapping internal/doctor's checks.
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
