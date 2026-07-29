//go:build !linux

package main

import (
	"fmt"
	"os"
)

// checkSharedLibraries only confirms the binary is there, off Linux.
//
// Deliberately not an otool -L or dumpbin equivalent. There is no `not found`
// marker to grep for on either platform: the dynamic loader resolves lazily and
// reports at launch, so a missing library shows up as the Simulator failing to
// start, which the caller already checks by launching it and reading the output.
// Parsing Mach-O load commands to say the same thing earlier is not worth the
// code.
func checkSharedLibraries(binPath string) error {
	if _, err := os.Stat(binPath); err != nil {
		return fmt.Errorf("simulator binary: %w", err)
	}
	return nil
}
