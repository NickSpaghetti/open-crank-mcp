//go:build linux

package main

import (
	"fmt"
	"os"
	"time"

	"github.com/NickSpaghetti/open-crank-mcp/internal/simulator"
)

// startDisplay brings up an Xvfb display and points DISPLAY at it, returning a
// stop function.
//
// Unconditional here, even when a real display is already present. This check
// is about whether the Simulator's libraries resolve and it survives startup,
// and a headless Xvfb is the environment where that answer means the most:
// it is what CI and the container both use. The cost is that on a Linux desktop
// this hides the window it is checking, which is worth knowing but not worth
// branching on.
func startDisplay() (func(), error) {
	xvfb, err := simulator.Launch("Xvfb", ":99", "-screen", "0", "1280x800x24")
	if err != nil {
		return nil, fmt.Errorf("launching Xvfb: %w", err)
	}
	stop := func() {
		_ = xvfb.Stop()
		_ = xvfb.Wait()
	}
	time.Sleep(1 * time.Second)

	if err := os.Setenv("DISPLAY", ":99"); err != nil {
		stop()
		return nil, fmt.Errorf("setting DISPLAY: %w", err)
	}
	return stop, nil
}
