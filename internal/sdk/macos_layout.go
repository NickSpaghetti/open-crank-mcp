package sdk

import "path/filepath"

// appBundleNames are the plausible Simulator bundle names, most likely first.
var appBundleNames = []string{
	"Playdate Simulator.app",
	"PlaydateSimulator.app",
}

// darwinLayout is the macOS layout, checked against SDK 3.1.1 (docs/NATIVE-PROBE.md).
// Contents/MacOS also holds crashpad_handler, so name the executable; never pick by listing.
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

		// The Mach-O inside the bundle, never the .app or `open`: LaunchServices returns
		// immediately, so our pipes would belong to an exited process and get_logs goes empty.
		simulatorBin: func(env Env, root string) (string, bool) {
			for _, app := range appBundleNames {
				stem := app[:len(app)-len(".app")]
				exe := filepath.Join(root, "bin", app, "Contents", "MacOS", stem)
				if exists(env, exe) {
					return exe, true
				}
			}
			plain := filepath.Join(root, "bin", "PlaydateSimulator")
			return plain, exists(env, plain)
		},

		pdcBin: func(root string) string {
			return filepath.Join(root, "bin", "pdc")
		},

		// The Simulator keeps game data inside the SDK directory, as on Linux, not under
		// Application Support; those paths are cheap fallbacks.
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
