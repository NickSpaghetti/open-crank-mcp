//go:build !linux

package contracttest

import "testing"

// startDisplay is a no-op off Linux. There is no Xvfb on macOS or Windows, and
// no need for one: a host running these has a real display already.
//
// Build-tagged rather than selected at runtime, unlike internal/sdk's layouts,
// because here the dependency genuinely does not exist off-platform. There is
// nothing portable to test.
func startDisplay(t *testing.T, display string) {}
