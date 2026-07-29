package sdk

import "path/filepath"

// appBundleNames are the plausible Simulator bundle names, most likely first. The
// SDK ships the Simulator as an application bundle, and the name appears in both
// spaced and unspaced forms depending on where you read.
var appBundleNames = []string{
	"Playdate Simulator.app",
	"PlaydateSimulator.app",
}

// The macOS layout. UNVERIFIED: written from Panic's documented install location
// and standard macOS conventions, never run against a real install. The repo
// already carries this kind of caveat for its WSL profile.
//
// Every guess here is recoverable without a code change, which is what makes
// shipping it defensible:
//   - a wrong bundle name is probed for, then overridable with
//     OPEN_CRANK_SIMULATOR_BIN
//   - a wrong data directory is probed for, then searched for, then reported in a
//     warning naming OPEN_CRANK_DATA_ROOT
//
// The fstest suite exercises this logic on every platform, so what is unverified
// is the *values*, not the code that consumes them.
//
// Not build-tagged, on purpose: see layout.go.
func darwinLayout() layout {
	return layout{
		name: "darwin",

		defaultRoots: func(env Env) []string {
			home, err := env.HomeDir()
			if err != nil {
				return nil
			}
			return []string{
				filepath.Join(home, "Developer", "PlaydateSDK"),
				filepath.Join(home, "PlaydateSDK"),
			}
		},

		// Returns the Mach-O executable *inside* the .app bundle.
		//
		// Never the bundle directory, and never `open`. This is the load-bearing
		// detail on macOS: `open` hands the launch to LaunchServices and returns
		// immediately, so the process this server started is not the Simulator,
		// and the pipes it set up belong to something that has already exited.
		// Simulator.Output() would be permanently empty, which silently breaks
		// get_logs and the startup check that reads the Simulator's own SDL
		// error message. Exec the inner binary directly.
		//
		// The trade is that this bypasses LaunchServices, so window activation
		// and any Gatekeeper prompt behave differently from a double-click.
		// get_logs being correct is worth more than that.
		simulatorBin: func(env Env, root string) (string, bool) {
			for _, app := range appBundleNames {
				// The executable inside a bundle conventionally drops .app.
				stem := app[:len(app)-len(".app")]
				exe := filepath.Join(root, "bin", app, "Contents", "MacOS", stem)
				if exists(env, exe) {
					return exe, true
				}
			}
			// Some layouts may keep a plain executable alongside the bundle.
			plain := filepath.Join(root, "bin", "PlaydateSimulator")
			return plain, exists(env, plain)
		},

		pdcBin: func(root string) string {
			return filepath.Join(root, "bin", "pdc")
		},

		// Application Support is where a macOS app is supposed to keep this, so it
		// goes first. The in-SDK location is listed too, because that is
		// demonstrably where it is on Linux and the Simulator is one codebase.
		dataDirCandidates: func(env Env, root, bundleID string) []string {
			var out []string
			if home, err := env.HomeDir(); err == nil {
				support := filepath.Join(home, "Library", "Application Support", "Playdate Simulator")
				out = append(out,
					filepath.Join(support, "Data", bundleID),
					filepath.Join(support, bundleID),
				)
			}
			return append(out, filepath.Join(root, "Disk", "Data", bundleID))
		},

		searchRoots: func(env Env, root string) []string {
			var out []string
			if home, err := env.HomeDir(); err == nil {
				out = append(out, filepath.Join(home, "Library", "Application Support", "Playdate Simulator"))
			}
			return append(out, filepath.Join(root, "Disk", "Data"))
		},
	}
}
