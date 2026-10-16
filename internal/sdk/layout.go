package sdk

import "runtime"

// layout is one platform's answers about where things are inside an SDK.
// A runtime value rather than build-tagged files so every layout is tested on every OS.
// filepath.Join uses the host separator, so the windows layout has forward slashes on Linux.
type layout struct {
	name              string // GOOS, for error messages
	defaultRoots      func(Env) []string
	simulatorBin      func(Env, string) (string, bool)
	pdcBin            func(string) string
	dataDirCandidates func(Env, string, string) []string // most likely first
	searchRoots       func(Env, string) []string         // walked as a last resort by FindDataDir
}

// layoutFor returns the layout for a GOOS value. Unknown platforms get the Linux
// layout as a best guess rather than refusing to start.
func layoutFor(goos string) layout {
	switch goos {
	case "darwin":
		return darwinLayout()
	case "windows":
		return windowsLayout()
	default:
		return linuxLayout()
	}
}

func hostLayout() layout {
	return layoutFor(runtime.GOOS)
}
