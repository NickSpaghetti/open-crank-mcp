package doctor

import (
	"fmt"
	"os/exec"
	"strings"
)

// PDCVersion runs `pdc --version` and returns what it printed.
func PDCVersion(pdcBin string) (string, error) {
	out, err := exec.Command(pdcBin, "--version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("pdc --version: %w\n%s", err, out)
	}
	return string(out), nil
}

// SDL2 itself misspells "could not be initalized", so both spellings are listed.
var errorMarkers = []string{
	"could not be initalized",
	"could not be initialized",
	"error",
	"not found",
}

// checkForErrors reports whether the Simulator's output complains about anything.
// Warning lines are skipped: a healthy container prints "libEGL warning: DRI3 error: ...".
// Markers are broad because the Simulator's failures have no stable format.
func checkForErrors(output string) error {
	for _, line := range strings.Split(output, "\n") {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "warning") {
			continue
		}
		for _, marker := range errorMarkers {
			if strings.Contains(lower, marker) {
				return fmt.Errorf("simulator reported an error:\n%s", output)
			}
		}
	}
	return nil
}
