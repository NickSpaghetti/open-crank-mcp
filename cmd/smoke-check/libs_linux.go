//go:build linux

package main

import (
	"fmt"
	"os/exec"
	"regexp"
)

var notFoundPattern = regexp.MustCompile(`not found`)

// checkSharedLibraries runs ldd and fails if anything is unresolved.
//
// This is the check the whole command was written for: PlaydateSimulator needs
// libwebkit2gtk-4.1 and libjavascriptcoregtk-4.1, and a missing one produces a
// dynamic-link failure rather than an error the Simulator itself reports. See
// docs/ROADMAP.md for why that particular dependency drove the container.
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
