//go:build !linux

package contracttest

import "testing"

// startDisplay is a no-op off Linux, where the host already has a real display.
func startDisplay(t *testing.T, display string) {}
