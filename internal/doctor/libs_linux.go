//go:build linux

package doctor

import (
	"bytes"
	"fmt"
	"os/exec"
)

// SharedLibraries runs ldd and fails if anything is unresolved. A missing
// webkit2gtk-4.1 is a dynamic-link failure, not an error the Simulator reports.
func SharedLibraries(binPath string) error {
	out, err := exec.Command("ldd", binPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ldd %s: %w\n%s", binPath, err, out)
	}
	if bytes.Contains(out, []byte("not found")) {
		return fmt.Errorf("missing shared libraries:\n%s", out)
	}
	return nil
}
