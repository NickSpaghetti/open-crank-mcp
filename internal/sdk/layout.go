package sdk

import "runtime"

// layout is one platform's answers about where things are inside an SDK.
//
// Deliberately a value selected at runtime rather than three build-tagged files.
// Build tags were the first attempt and they are wrong here: a tagged
// paths_darwin.go can only be compiled on darwin, so its logic can only be
// tested on a Mac, and the macOS layout is precisely the part nobody here can
// verify by running it. Keeping every layout compiled on every platform is what
// lets the fstest suite exercise all three in CI on every commit. The per-OS
// files stay separate for readability; they just are not conditional.
//
// One consequence worth knowing: filepath.Join uses the host separator, so the
// windows layout produces forward slashes when it is built on Linux. That is
// fine for the tests, which only need keys to be consistent, and irrelevant in
// production, where the windows layout only ever runs on Windows.
type layout struct {
	// name is the GOOS this describes, for error messages.
	name string
	// defaultRoots are install locations to try when nothing else says where.
	defaultRoots func(Env) []string
	// simulatorBin returns the Simulator executable, and whether it was found.
	// Returning a bool rather than just a path because macOS has to probe a list
	// of possible bundle names.
	simulatorBin func(Env, string) (string, bool)
	// pdcBin returns the compiler path. No probing: it is bin/pdc everywhere,
	// give or take an .exe.
	pdcBin func(string) string
	// dataDirCandidates are the plausible sandboxed data directories for a
	// bundle ID, most likely first.
	dataDirCandidates func(Env, string, string) []string
	// searchRoots are walked as a last resort. See FindDataDir.
	searchRoots func(Env, string) []string
}

// layoutFor returns the layout for a GOOS value, defaulting to the Linux one.
//
// Linux is the fallback rather than an error because it is the layout that has
// actually been verified, and because an unrecognised GOOS here means some
// platform nobody has considered - in which case the verified layout is a better
// guess than refusing to start.
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

// hostLayout is the layout for the machine this is running on.
func hostLayout() layout {
	return layoutFor(runtime.GOOS)
}
