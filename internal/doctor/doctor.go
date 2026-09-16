// Package doctor holds the environment checks that answer "why is this not
// working", and the rendering of their results.
//
// It exists because the same question had two answers with different reach.
// cmd/smoke-check already resolved the SDK, checked shared libraries, ran pdc
// and launched the Simulator - but it is a second binary behind `make`, and
// someone who installed this as an editor plugin has neither a checkout nor a
// Makefile. Rather than write those checks twice, they live here and both
// callers are shells around them: `open-crank-mcp -doctor` for a plugin user,
// cmd/smoke-check for CI and the container.
//
// That split is this repo's existing pattern rather than a new idea.
// cmd/sdk-path/main.go states it: "the rendering lives in sdk.Paths.Describe so
// it can be tested; this is the shell around it."
//
// Gathering and rendering are separate for the same reason. Gather does I/O -
// exec, stat, process launch - and cannot be table-tested. Findings.String is
// pure, so every line it can produce is asserted somewhere, including the
// combinations a developer's own machine will never be in (no SDK, a Mac that
// has never run the Simulator, a failed ldd).
package doctor

import (
	"github.com/NickSpaghetti/open-crank-mcp/internal/sdk"
)

// Options controls how much Gather does.
type Options struct {
	// Launch actually starts the Simulator, waits to see whether it stays up,
	// and reads its output.
	//
	// Off by default, and that default is the decision rather than an oversight.
	// Launching is a side effect: it puts a window on the user's desktop, and
	// "nothing supervises the Simulator an agent launched" is already a known
	// rough edge in this project. A diagnostic should not add to it unless asked.
	// The cheap check catches the common case anyway - on Linux a missing
	// webkit2gtk is an unresolved symbol ldd sees without starting anything.
	Launch bool
}

// Findings is everything the checks learned. Every field is data, so the
// rendering below is a pure function of it.
type Findings struct {
	// Version of the server binary reporting this, passed in rather than read,
	// because it is stamped into package main at build time.
	Version string
	GOOS    string

	SDK    sdk.Paths
	SDKErr error

	// LibsErr is the shared-library check. Nil means it passed; it is only
	// meaningful when the SDK resolved.
	LibsErr error
	// PDCOut is what `pdc --version` printed.
	PDCOut string
	PDCErr error

	// DataDirs is where a running game's sandboxed data directory would be
	// looked for. Rendered with a placeholder bundle ID, since no game is
	// running when a doctor runs.
	DataDirs []string

	// Launched records whether Options.Launch was set, so the report can tell
	// "the launch passed" apart from "no launch was attempted".
	Launched  bool
	LaunchErr error

	// SimulatorRan and SimulatorRanKnown carry SimulatorEverRan's two results.
	// Two fields rather than a nullable one, on the same reasoning
	// GetStatusOutput.HarnessWarning records: a tri-state belongs inside the
	// producer, not in the reader's lap.
	SimulatorRan      bool
	SimulatorRanKnown bool
}

// placeholderBundleID stands in for a real game's bundle ID in the data
// directory listing. No game is running, so the useful thing to show is the
// shape and the roots, not a path that exists.
const placeholderBundleID = "<bundleID>"
