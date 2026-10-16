// Package doctor holds the environment checks behind `open-crank-mcp -doctor` and
// cmd/smoke-check. Gather does the I/O; Findings.String renders it purely.
package doctor

import (
	"github.com/NickSpaghetti/open-crank-mcp/internal/sdk"
)

// Options controls how much Gather does.
type Options struct {
	// Launch starts the Simulator and reads its output. Off by default: it puts a
	// window on the user's desktop.
	Launch bool
}

// Findings is everything the checks learned.
type Findings struct {
	Version string // stamped into package main at build time
	GOOS    string

	SDK    sdk.Paths
	SDKErr error

	LibsErr error // only meaningful when the SDK resolved
	PDCOut  string
	PDCErr  error

	DataDirs []string // rendered with placeholderBundleID

	Launched  bool // Options.Launch was set
	LaunchErr error

	SimulatorRan      bool
	SimulatorRanKnown bool
}

// placeholderBundleID stands in for a real game's bundle ID; no game is running.
const placeholderBundleID = "<bundleID>"
