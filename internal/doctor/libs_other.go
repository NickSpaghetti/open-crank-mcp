//go:build !linux

package doctor

import (
	"fmt"
	"os"
)

// SharedLibraries only confirms the binary is there, off Linux. The loaders there
// report missing libraries at launch, which Launch observes.
func SharedLibraries(binPath string) error {
	if _, err := os.Stat(binPath); err != nil {
		return fmt.Errorf("simulator binary: %w", err)
	}
	return nil
}
