package sdk

import "path/filepath"

// appBundleNames are the plausible Simulator bundle names, most likely first. The
// SDK ships the Simulator as an application bundle, and the name appears in both
// spaced and unspaced forms depending on where you read.
var appBundleNames = []string{
	"Playdate Simulator.app",
	"PlaydateSimulator.app",
}

// The macOS layout. Confirmed against a real SDK 3.1.1 install on macOS, except
// for the data directory. See docs/NATIVE-PROBE.md for the raw output.
//
// Confirmed:
//   - the SDK installs to ~/Developer/PlaydateSDK
//   - ~/.Playdate/config holds "SDKRoot\t<path>\n", tab-separated
//   - the bundle is "Playdate Simulator.app" under bin/, and the executable is
//     Contents/MacOS/Playdate Simulator
//   - pdc is bin/pdc, with no extension
//
// Two things that install does NOT have, both worth knowing:
//   - there is no plain bin/PlaydateSimulator alongside the bundle. The fallback
//     below is kept anyway, since it costs one stat and covers a layout that may
//     exist elsewhere.
//   - Contents/MacOS also contains crashpad_handler, which sorts before the
//     Simulator alphabetically. Anything that picks an executable out of that
//     directory by listing it will pick the wrong one. This code names the file
//     it wants, so it is immune, but a probe script written the obvious way is
//     not: that mistake is recorded in docs/NATIVE-PROBE.md because it happened.
//
// The data directory is confirmed too: <root>/Disk/Data/<bundleID>, the same as
// Linux. See the comment on dataDirCandidates, and docs/NATIVE-PROBE.md for the
// run that established it.
//
// Nothing in this layout is a guess any more. The probing and the
// OPEN_CRANK_DATA_ROOT override stay regardless, because the evidence is one
// machine on one SDK version, and the cost of being wrong is a silent hang
// rather than an error.
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

		// Confirmed: the Simulator sandboxes per-game data inside the SDK
		// directory, exactly as it does on Linux. A game run on macOS 3.1.1 left
		// its mcp/game_logs.json at <root>/Disk/Data/<bundleID>/, and nothing at
		// all appeared under Application Support or Containers.
		//
		// So this is first, not last. It was originally third, behind two
		// Application Support guesses made from macOS convention: an app is
		// *supposed* to keep this kind of state there. The Simulator does not.
		// Guessing from convention put the real answer last, which would have
		// meant every lookup walking two dead candidates first.
		//
		// The Application Support paths are kept behind it rather than deleted.
		// They cost one stat each when the first candidate misses, and the
		// evidence is a single machine and a single SDK version.
		dataDirCandidates: func(env Env, root, bundleID string) []string {
			out := []string{filepath.Join(root, "Disk", "Data", bundleID)}
			if home, err := env.HomeDir(); err == nil {
				support := filepath.Join(home, "Library", "Application Support", "Playdate Simulator")
				out = append(out,
					filepath.Join(support, "Data", bundleID),
					filepath.Join(support, bundleID),
				)
			}
			return out
		},

		searchRoots: func(env Env, root string) []string {
			out := []string{filepath.Join(root, "Disk", "Data")}
			if home, err := env.HomeDir(); err == nil {
				out = append(out, filepath.Join(home, "Library", "Application Support", "Playdate Simulator"))
			}
			return out
		},
	}
}
