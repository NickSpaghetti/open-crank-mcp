package doctor

import (
	"path/filepath"

	"github.com/NickSpaghetti/open-crank-mcp/internal/sdk"
)

// simulatorPrefs is rewritten by the macOS Simulator on every launch, so its
// absence means it has probably never run (docs/GOTCHAS.md).
const simulatorPrefs = "Library/Preferences/date.play.simulator.plist"

// SimulatorEverRan reports whether the macOS Simulator appears to have run here
// before, and whether that could be answered at all. It is an inference from the
// plist; a first run hides a dialog behind the Simulator window.
func SimulatorEverRan(env sdk.Env, goos string) (ran, known bool) {
	if goos != "darwin" {
		return false, false
	}
	home, err := env.HomeDir()
	if err != nil {
		return false, false
	}
	return sdk.Exists(env, filepath.Join(home, simulatorPrefs)), true
}
