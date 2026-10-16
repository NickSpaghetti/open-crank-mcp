package sdk

import "path/filepath"

// linuxLayout is the layout the container uses.
func linuxLayout() layout {
	return layout{
		name: "linux",

		// The Linux SDK is a user-extracted tarball; ~/PlaydateSDK is what Panic's instructions use.
		defaultRoots: func(env Env) []string {
			home, err := env.HomeDir()
			if err != nil {
				return nil
			}
			return []string{filepath.Join(home, "PlaydateSDK")}
		},

		simulatorBin: func(env Env, root string) (string, bool) {
			bin := filepath.Join(root, "bin", "PlaydateSimulator")
			return bin, exists(env, bin)
		},

		pdcBin: func(root string) string {
			return filepath.Join(root, "bin", "pdc")
		},

		// The Simulator sandboxes game data inside the SDK directory.
		dataDirCandidates: func(env Env, root, bundleID string) []string {
			return []string{filepath.Join(root, "Disk", "Data", bundleID)}
		},

		searchRoots: func(env Env, root string) []string {
			return []string{filepath.Join(root, "Disk", "Data")}
		},
	}
}
