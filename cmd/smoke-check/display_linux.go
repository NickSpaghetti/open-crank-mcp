//go:build linux

package main

import (
	"fmt"
	"os"
	"time"

	"github.com/NickSpaghetti/open-crank-mcp/internal/simulator"
)

// startDisplay brings up an Xvfb display and points DISPLAY at it, returning a
// stop function. Unconditional, even on a desktop, since headless is what CI and
// the container use.
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
