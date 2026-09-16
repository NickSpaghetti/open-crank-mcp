// The checks that can be tested without a Simulator, kept apart from the one
// that cannot.
//
// That separation is the whole reason this file exists. gremlins excludes by
// file path, so a single launch.go holding both Launch and these would have had
// its exclusion swallow code that is perfectly testable - which is the mistake
// docs/ROADMAP.md records at Checkpoint 7, where a basename-matched exclusion
// silently hid 48 mutants. internal/build already splits detect.go from exec.go
// on the same line.
package doctor

import (
	"fmt"
	"os/exec"
	"strings"
)

// PDCVersion runs `pdc --version` and returns what it printed.
//
// The output is returned rather than logged because two callers want it
// differently: cmd/smoke-check prints it verbatim to keep its own output
// unchanged, and the doctor report folds it into a line.
func PDCVersion(pdcBin string) (string, error) {
	out, err := exec.Command(pdcBin, "--version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("pdc --version: %w\n%s", err, out)
	}
	return string(out), nil
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

// checkForErrors reports whether the Simulator's output complains about anything.
//
// Line by line, and lines that announce themselves as warnings are skipped. That
// exclusion is doing real work: `error` is in the marker list, and a healthy
// containerised run prints
//
//	libEGL warning: DRI3 error: Could not get DRI3 device
//
// which failed `make smoke-check` on a machine where the Simulator had started
// perfectly well. Matching the whole output as one string cannot tell that apart
// from a real failure, because the word it keys on is inside a line that already
// said it was a warning.
//
// Broad markers over precise ones is still the right trade - the Simulator's
// failures have no stable format and a missed one is a green check on a broken
// environment - but "a warning is not an error" costs nothing and removes the
// false positive that was actually happening.
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
