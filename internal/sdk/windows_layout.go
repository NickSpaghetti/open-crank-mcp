package sdk

import "path/filepath"

// windowsLayout is the Windows layout, checked against SDK 3.1.2. The installer
// writes no ~/.Playdate/config, so resolution relies on default roots.
func windowsLayout() layout {
	return layout{
		name: "windows",

		// The installer puts the SDK under Documents.
		defaultRoots: func(env Env) []string {
			var out []string
			if profile := env.Getenv("USERPROFILE"); profile != "" {
				out = append(out, filepath.Join(profile, "Documents", "PlaydateSDK"))
			}
			if local := env.Getenv("LOCALAPPDATA"); local != "" {
				out = append(out, filepath.Join(local, "Programs", "PlaydateSDK"))
			}
			if home, err := env.HomeDir(); err == nil {
				out = append(out, filepath.Join(home, "PlaydateSDK"))
			}
			return out
		},

		simulatorBin: func(env Env, root string) (string, bool) {
			bin := filepath.Join(root, "bin", "PlaydateSimulator.exe")
			return bin, exists(env, bin)
		},

		pdcBin: func(root string) string {
			return filepath.Join(root, "bin", "pdc.exe")
		},

		// The Simulator writes game data inside the SDK. %LOCALAPPDATA%\Playdate Simulator
		// exists but its contents are unknown, so it is only a fallback.
		dataDirCandidates: func(env Env, root, bundleID string) []string {
			out := []string{filepath.Join(root, "Disk", "Data", bundleID)}
			if local := env.Getenv("LOCALAPPDATA"); local != "" {
				sim := filepath.Join(local, "Playdate Simulator")
				out = append(out,
					filepath.Join(sim, "Data", bundleID),
					filepath.Join(sim, bundleID),
				)
			}
			return out
		},

		searchRoots: func(env Env, root string) []string {
			out := []string{filepath.Join(root, "Disk", "Data")}
			if local := env.Getenv("LOCALAPPDATA"); local != "" {
				out = append(out, filepath.Join(local, "Playdate Simulator"))
			}
			return out
		},
	}
}
