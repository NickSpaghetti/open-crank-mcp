package sdk

import "path/filepath"

// The Windows layout. Windows-native is NOT a supported runtime - WSL2 serves
// Windows users through the container instead. See docs/ROADMAP.md.
//
// The values below are nonetheless real, taken from an SDK 3.1.1 install on
// Windows (see docs/NATIVE-PROBE.md), because getting them right costs nothing
// and keeps promoting Windows later additive rather than a rewrite.
//
// Three things that probe established, each of which contradicted the first
// guess here:
//
//   - There is no ~/.Playdate/config at all. `Get-Content` on it returned
//     nothing. Resolution has to fall through to a default location, which it
//     does, but it means the config step is dead weight on this platform rather
//     than the primary signal it is on macOS.
//   - The SDK installs to %USERPROFILE%\Documents\PlaydateSDK, not under
//     %LOCALAPPDATA%\Programs.
//   - The Simulator's own directory is under %LOCALAPPDATA%, not %APPDATA%.
//
// Not build-tagged, on purpose: see layout.go.
func windowsLayout() layout {
	return layout{
		name: "windows",

		// Documents first: that is where the installer actually put it. The
		// %LOCALAPPDATA%\Programs path was the original guess and did not exist
		// on the machine checked, so it drops to second rather than being removed.
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

		// In-SDK first, matching the two platforms where a running game has
		// actually been observed writing there. No game was run on Windows, so
		// this is inference rather than measurement, but it is inference from two
		// confirmed data points about one codebase rather than from convention.
		//
		// %LOCALAPPDATA%\Playdate Simulator does exist on Windows and is kept as
		// a fallback. What is in it is unknown: it may be settings rather than
		// game data. %APPDATA% was the original guess and holds nothing at all.
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
