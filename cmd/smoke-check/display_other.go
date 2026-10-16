//go:build !linux

package main

// startDisplay is a no-op off Linux, where the Simulator opens on the desktop.
func startDisplay() (func(), error) {
	return func() {}, nil
}
