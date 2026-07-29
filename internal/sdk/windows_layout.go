package sdk

import "path/filepath"

// The Windows layout. Its logic is exercised by the fstest suite and it compiles
// under `make go-build-cross`, but Windows-native is NOT a supported runtime -
// see docs/ROADMAP.md. WSL2 already serves Windows users through the container.
//
// This exists so the package builds and behaves coherently for windows/amd64,
// which keeps promoting Windows later additive rather than a rewrite. Treat the
// values as unverified placeholders, not a supported configuration.
//
// Not build-tagged, on purpose: see layout.go.
func windowsLayout() layout {
	return layout{
		name: "windows",

		defaultRoots: func(env Env) []string {
			var out []string
			if local := env.Getenv("LOCALAPPDATA"); local != "" {
				out = append(out, filepath.Join(local, "Programs", "PlaydateSDK"))
			}
			if profile := env.Getenv("USERPROFILE"); profile != "" {
				out = append(out, filepath.Join(profile, "Documents", "PlaydateSDK"))
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

		dataDirCandidates: func(env Env, root, bundleID string) []string {
			var out []string
			if appData := env.Getenv("APPDATA"); appData != "" {
				out = append(out, filepath.Join(appData, "Playdate Simulator", "Data", bundleID))
			}
			return append(out, filepath.Join(root, "Disk", "Data", bundleID))
		},

		searchRoots: func(env Env, root string) []string {
			var out []string
			if appData := env.Getenv("APPDATA"); appData != "" {
				out = append(out, filepath.Join(appData, "Playdate Simulator"))
			}
			return append(out, filepath.Join(root, "Disk", "Data"))
		},
	}
}
