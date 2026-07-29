package sdk

import "path/filepath"

// The Linux layout. The only one verified against a real install, and the one the
// container uses.
//
// Not build-tagged, on purpose: see layout.go.
func linuxLayout() layout {
	return layout{
		name: "linux",

		// The Linux SDK is a tarball the user extracts themselves, so there is no
		// installer-blessed location. ~/PlaydateSDK is what Panic's instructions
		// use. The container never reaches this, because it sets
		// PLAYDATE_SDK_PATH.
		defaultRoots: func(env Env) []string {
			home, err := env.HomeDir()
			if err != nil {
				return nil
			}
			return []string{filepath.Join(home, "PlaydateSDK")}
		},

		// A plain ELF executable under bin/.
		simulatorBin: func(env Env, root string) (string, bool) {
			bin := filepath.Join(root, "bin", "PlaydateSimulator")
			return bin, exists(env, bin)
		},

		pdcBin: func(root string) string {
			return filepath.Join(root, "bin", "pdc")
		},

		// Verified: the sandboxed data directory is inside the SDK directory,
		// which is why the shared profile can bind-mount
		// $PLAYDATE_SDK_PATH/Disk/Data and see a running game's saves and logs
		// from the host.
		dataDirCandidates: func(env Env, root, bundleID string) []string {
			return []string{filepath.Join(root, "Disk", "Data", bundleID)}
		},

		searchRoots: func(env Env, root string) []string {
			return []string{filepath.Join(root, "Disk", "Data")}
		},
	}
}
