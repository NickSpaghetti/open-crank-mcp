package doctor

import (
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/NickSpaghetti/open-crank-mcp/internal/sdk"
)

// simulatorPrefs is the file the macOS Simulator keeps its settings in.
//
// docs/GOTCHAS.md establishes the property this check rests on, and establishes
// it by observation rather than assumption: the Simulator "rewrites
// ~/Library/Preferences/date.play.simulator.plist on every launch, and it
// preserves the keys written there". Rewritten on *every* launch is what makes
// the absence of the file meaningful.
const simulatorPrefs = "Library/Preferences/date.play.simulator.plist"

// SimulatorEverRan reports whether the macOS Simulator appears to have run on
// this machine before, and whether that question could be answered at all.
//
// This exists for one specific failure, and it is the most likely thing a new
// plugin install hits on a Mac: a first-run dialog opens behind the Simulator
// window and the game waits on a click that never comes. `Loading: <game>.pdx/`
// appears on stdout, so the launch looks fine, which is what makes it so easy to
// misdiagnose. docs/GOTCHAS.md has the full account, including the settings that
// were tried and do not suppress it.
//
// It is an **inference**, and callers must present it as one. "The plist is
// absent" is the measurement; "the Simulator has never run" is what that
// probably means. A machine where someone deleted the file, or where preferences
// live somewhere unusual, would read as never-run and is simply wrong rather
// than detectably wrong. That is an acceptable trade for a warning, and would
// not be for anything that changed behaviour.
//
// The goos is a parameter rather than a build tag, for the reason
// internal/sdk/layout.go already gives about layouts: a build-tagged darwin file
// can only be compiled on darwin, so its logic could only be tested on a Mac.
// This way the darwin branch is exercised on every platform on every PR.
func SimulatorEverRan(env sdk.Env, goos string) (ran, known bool) {
	if goos != "darwin" {
		return false, false
	}
	home, err := env.HomeDir()
	if err != nil {
		return false, false
	}
	// fs.FS keys are slash-separated and never rooted, so an absolute OS path
	// has its leading separator trimmed. internal/sdk does the same thing for
	// the same reason.
	key := strings.TrimPrefix(filepath.ToSlash(filepath.Join(home, simulatorPrefs)), "/")
	if _, err := fs.Stat(env.FS, key); err != nil {
		return false, true
	}
	return true, true
}
