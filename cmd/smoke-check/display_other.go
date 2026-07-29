//go:build !linux

package main

// startDisplay is a no-op off Linux. There is no Xvfb on macOS or Windows, and
// no need for one: the Simulator opens on the desktop that is already there.
// Keeping the signature identical means run() has no platform branch in it.
func startDisplay() (func(), error) {
	return func() {}, nil
}
