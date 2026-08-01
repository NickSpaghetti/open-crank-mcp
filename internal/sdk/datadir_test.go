package sdk

import (
	"errors"
	"strings"
	"testing"
)

// errNoHome stands in for os.UserHomeDir failing, which happens on a machine
// with no HOME set. Several layouts have to cope with it.
var errNoHome = errors.New("no home directory")

// The data directory is the single riskiest value in native mode: guess it wrong
// and launching succeeds, the game runs, and then every harness-dependent tool
// times out five seconds at a time with nothing naming the cause. So these tests
// cover not just the happy path but each fallback, and the diagnostic.

// dataDirEnv builds an env whose filesystem contains a harnessed data directory
// at each of the given paths, plus a valid SDK at root.
func dataDirEnv(goos, home, root string, harnessed []string, environ map[string]string) Env {
	files := sdkFiles(goos, root)
	for _, dir := range harnessed {
		// The harness creates mcp/ inside its data directory. That file is what
		// makes the directory recognisable rather than merely predicted.
		files[dir+"/mcp/game_logs.json"] = "[]"
	}
	return testEnv(home, files, environ)
}

func TestFindDataDirPicksTheCandidateWithHarnessDir(t *testing.T) {
	const bundle = "com.example.game"
	for _, tc := range []struct {
		goos      string
		home      string
		root      string
		harnessed string
	}{
		{"linux", "/home/u", "/opt/sdk", "/opt/sdk/Disk/Data/" + bundle},
		{"darwin", "/Users/u", "/Users/u/Developer/PlaydateSDK",
			"/Users/u/Library/Application Support/Playdate Simulator/Data/" + bundle},
		// LOCALAPPDATA, not APPDATA: the Simulator's directory is under Local on
		// a real install, and Roaming holds nothing.
		{"windows", "/c/users/u", "/c/PlaydateSDK",
			"/c/users/u/AppData/Local/Playdate Simulator/Data/" + bundle},
	} {
		t.Run(tc.goos, func(t *testing.T) {
			environ := map[string]string{EnvVarSDKPath: tc.root}
			if tc.goos == "windows" {
				environ["LOCALAPPDATA"] = "/c/users/u/AppData/Local"
			}
			env := dataDirEnv(tc.goos, tc.home, tc.root, []string{tc.harnessed}, environ)
			p, err := resolveWith(env, layoutFor(tc.goos))
			if err != nil {
				t.Fatalf("resolveWith: %v", err)
			}

			dir, found, tried := p.FindDataDir(env, bundle)
			if !found {
				t.Fatalf("FindDataDir did not find %s; tried:\n  %s", tc.harnessed, strings.Join(tried, "\n  "))
			}
			if dir != tc.harnessed {
				t.Errorf("dir = %q, want %q", dir, tc.harnessed)
			}
		})
	}
}

// A candidate that exists but has no mcp/ in it is not the answer: on macOS the
// in-SDK path is a *later* candidate than Application Support, and picking a bare
// directory over the harnessed one is exactly the silent failure this avoids.
func TestFindDataDirSkipsCandidatesWithoutHarnessDir(t *testing.T) {
	const bundle = "com.example.game"
	root := "/Users/u/Developer/PlaydateSDK"
	support := "/Users/u/Library/Application Support/Playdate Simulator"

	files := sdkFiles("darwin", root)
	// An empty directory at the FIRST candidate, which since the macOS layout was
	// corrected is the in-SDK one...
	files[root+"/Disk/Data/"+bundle+"/.keep"] = ""
	// ...and the real, harnessed one at a later candidate.
	files[support+"/Data/"+bundle+"/mcp/game_logs.json"] = "[]"

	env := testEnv("/Users/u", files, map[string]string{EnvVarSDKPath: root})
	p, err := resolveWith(env, darwinLayout())
	if err != nil {
		t.Fatalf("resolveWith: %v", err)
	}

	dir, found, _ := p.FindDataDir(env, bundle)
	if !found {
		t.Fatal("FindDataDir found nothing, though a harnessed directory exists")
	}
	if want := support + "/Data/" + bundle; dir != want {
		t.Errorf("dir = %q, want %q (the harnessed one, not the empty first candidate)", dir, want)
	}
}

// The override exists so a layout nobody could verify is never a dead end.
func TestFindDataDirHonoursDataRootOverride(t *testing.T) {
	const bundle = "com.example.game"
	root := "/opt/sdk"
	override := "/somewhere/unexpected"

	files := sdkFiles("linux", root)
	files[override+"/"+bundle+"/mcp/game_logs.json"] = "[]"
	// A perfectly good default location also exists, to prove the override wins.
	files[root+"/Disk/Data/"+bundle+"/mcp/game_logs.json"] = "[]"

	env := testEnv("/home/u", files, map[string]string{
		EnvVarSDKPath:  root,
		EnvVarDataRoot: override,
	})
	p, err := resolveWith(env, linuxLayout())
	if err != nil {
		t.Fatalf("resolveWith: %v", err)
	}

	dir, found, _ := p.FindDataDir(env, bundle)
	if !found {
		t.Fatal("FindDataDir ignored the override")
	}
	if want := override + "/" + bundle; dir != want {
		t.Errorf("dir = %q, want %q", dir, want)
	}
}

// The bounded search is what turns a wrong candidate list into "slower but
// correct" rather than "broken", and it is the reason the macOS values can ship
// unverified at all. Here the harnessed directory is under a search root but at
// a path no candidate names.
func TestFindDataDirFallsBackToBoundedSearch(t *testing.T) {
	const bundle = "com.example.game"
	root := "/opt/sdk"
	unexpected := root + "/Disk/Data/Sandbox/" + bundle

	files := sdkFiles("linux", root)
	files[unexpected+"/mcp/game_logs.json"] = "[]"

	env := testEnv("/home/u", files, map[string]string{EnvVarSDKPath: root})
	p, err := resolveWith(env, linuxLayout())
	if err != nil {
		t.Fatalf("resolveWith: %v", err)
	}

	dir, found, tried := p.FindDataDir(env, bundle)
	if !found {
		t.Fatalf("bounded search missed %s; tried:\n  %s", unexpected, strings.Join(tried, "\n  "))
	}
	if dir != unexpected {
		t.Errorf("dir = %q, want %q", dir, unexpected)
	}
}

// Not finding it must not be an error: launching a game whose harness is not
// installed yet is a legitimate thing to do, and the flow is build, launch,
// setup, relaunch. The contract is a usable fallback plus everything tried.
func TestFindDataDirReportsRatherThanFailing(t *testing.T) {
	const bundle = "com.example.game"
	root := "/opt/sdk"
	env := testEnv("/home/u", sdkFiles("linux", root), map[string]string{EnvVarSDKPath: root})
	p, err := resolveWith(env, linuxLayout())
	if err != nil {
		t.Fatalf("resolveWith: %v", err)
	}

	dir, found, tried := p.FindDataDir(env, bundle)
	if found {
		t.Fatal("FindDataDir claimed to find a directory that does not exist")
	}
	if dir == "" {
		t.Error("dir is empty, but callers need a usable fallback to keep working with")
	}
	if len(tried) == 0 {
		t.Fatal("tried is empty, so the warning would name nothing")
	}

	msg := DataDirDiagnostic(bundle, tried)
	for _, want := range []string{bundle, EnvVarDataRoot, "setup"} {
		if !strings.Contains(msg, want) {
			t.Errorf("diagnostic does not mention %q, which is what makes it actionable:\n%s", want, msg)
		}
	}
	for _, path := range tried {
		if !strings.Contains(msg, path) {
			t.Errorf("diagnostic omits a path that was tried (%q)", path)
		}
	}
}

// A Paths built as a struct literal has no goos, and must fall back to the host
// layout instead of panicking on nil layout functions. Several tests elsewhere in
// the tree construct Paths this way.
func TestZeroValuePathsDoesNotPanic(t *testing.T) {
	p := Paths{Root: "/opt/sdk"}
	env := testEnv("/home/u", nil, nil)
	if got := p.DataDirCandidates(env, "com.example.game"); len(got) == 0 {
		t.Error("DataDirCandidates returned nothing for a struct-literal Paths")
	}
	if _, _, tried := p.FindDataDir(env, "com.example.game"); len(tried) == 0 {
		t.Error("FindDataDir reported nothing tried for a struct-literal Paths")
	}
}

// The bounded search has to stay bounded. Both bounds exist so that a wrong
// candidate list degrades into "slower" rather than "hangs on a home directory",
// and neither was pinned by anything until mutation testing pointed out that
// changing the limits broke no test.

// Anything deeper than searchDepth is out of reach on purpose. Without this the
// depth limit could be raised or removed silently, and the search would start
// walking arbitrarily far below a search root.
func TestFindDataDirSearchRespectsDepthLimit(t *testing.T) {
	const bundle = "com.example.game"
	root := "/opt/sdk"
	// Search root is <root>/Disk/Data, so this sits four levels below it, past
	// the limit.
	tooDeep := root + "/Disk/Data/a/b/c/d/" + bundle

	files := sdkFiles("linux", root)
	files[tooDeep+"/mcp/game_logs.json"] = "[]"

	env := testEnv("/home/u", files, map[string]string{EnvVarSDKPath: root})
	p, err := resolveWith(env, linuxLayout())
	if err != nil {
		t.Fatalf("resolveWith: %v", err)
	}

	dir, found, _ := p.FindDataDir(env, bundle)
	if found {
		t.Errorf("found %q past the depth limit; the walk is unbounded", dir)
	}
}

// The node budget is the backstop for a search root that is enormous rather than
// deep. Shrinking it here proves it is load-bearing.
func TestFindDataDirSearchRespectsNodeBudget(t *testing.T) {
	const bundle = "com.example.game"
	root := "/opt/sdk"
	target := root + "/Disk/Data/nested/" + bundle

	files := sdkFiles("linux", root)
	files[target+"/mcp/game_logs.json"] = "[]"
	// Enough siblings that a tiny budget is spent before reaching the target.
	// Named to sort ahead of "nested" so the walk meets them first.
	for _, name := range []string{"aaa", "aab", "aac", "aad", "aae"} {
		files[root+"/Disk/Data/"+name+"/filler"] = ""
	}

	env := testEnv("/home/u", files, map[string]string{EnvVarSDKPath: root})
	p, err := resolveWith(env, linuxLayout())
	if err != nil {
		t.Fatalf("resolveWith: %v", err)
	}

	// Confirm it is reachable with the real budget, so the next assertion is
	// about the budget and not about the fixture being wrong.
	if _, found, _ := p.FindDataDir(env, bundle); !found {
		t.Fatal("fixture is wrong: the target is not findable even with the full budget")
	}

	original := searchNodeBudget
	searchNodeBudget = 2
	defer func() { searchNodeBudget = original }()

	if _, found, _ := p.FindDataDir(env, bundle); found {
		t.Error("the walk found the target with a budget of 2 nodes, so the budget does nothing")
	}
}

// Every layout builds its paths from the environment, and on an unfamiliar
// machine parts of that environment are missing. The candidate list must degrade
// to whatever it can still derive rather than producing paths with empty
// segments in them.
func TestLayoutsToleratePartialEnvironment(t *testing.T) {
	const bundle = "com.example.game"
	noHome := Env{
		FS:      testEnv("", nil, nil).FS,
		Getenv:  func(string) string { return "" },
		HomeDir: func() (string, error) { return "", errNoHome },
	}

	for _, goos := range []string{"linux", "darwin", "windows"} {
		t.Run(goos, func(t *testing.T) {
			lay := layoutFor(goos)

			// No home and no environment variables: nothing to derive a default
			// install location from, and an empty list is the honest answer.
			if roots := lay.defaultRoots(noHome); len(roots) != 0 {
				t.Errorf("defaultRoots with no home returned %v, want none", roots)
			}

			// Data candidates can still be derived from the SDK root, which is
			// known by then. Every one of them must be absolute and must not
			// contain an empty path segment from a missing variable.
			cands := lay.dataDirCandidates(noHome, "/opt/sdk", bundle)
			if len(cands) == 0 {
				t.Fatal("dataDirCandidates returned nothing even though the SDK root is known")
			}
			for _, c := range cands {
				if strings.Contains(c, "//") {
					t.Errorf("candidate %q has an empty path segment", c)
				}
				if !strings.HasSuffix(c, bundle) {
					t.Errorf("candidate %q does not end in the bundle ID", c)
				}
			}

			if roots := lay.searchRoots(noHome, "/opt/sdk"); len(roots) == 0 {
				t.Error("searchRoots returned nothing even though the SDK root is known")
			}
		})
	}
}

// Windows derives its locations from LOCALAPPDATA, USERPROFILE and APPDATA. Each
// is optional, and a missing one must drop its candidate rather than contribute a
// malformed path.
func TestWindowsLayoutUsesEachEnvironmentVariable(t *testing.T) {
	const bundle = "com.example.game"
	lay := windowsLayout()

	full := testEnv("/c/users/u", nil, map[string]string{
		"LOCALAPPDATA": "/c/users/u/AppData/Local",
		"USERPROFILE":  "/c/users/u",
	})
	roots := lay.defaultRoots(full)
	if len(roots) != 3 {
		t.Errorf("defaultRoots with every variable set returned %d entries, want 3: %v", len(roots), roots)
	}

	// The confirmed install location goes first.
	if want := "/c/users/u/Documents/PlaydateSDK"; roots[0] != want {
		t.Errorf("first default root = %q, want %q (where the installer puts it)", roots[0], want)
	}

	// Dropping LOCALAPPDATA has to drop exactly one candidate.
	partial := testEnv("/c/users/u", nil, map[string]string{
		"USERPROFILE": "/c/users/u",
	})
	if got := lay.defaultRoots(partial); len(got) != 2 {
		t.Errorf("defaultRoots without LOCALAPPDATA returned %d entries, want 2: %v", len(got), got)
	}

	// Without LOCALAPPDATA the only data candidate left is the in-SDK one.
	noLocal := testEnv("/c/users/u", nil, nil)
	if got := lay.dataDirCandidates(noLocal, "/c/sdk", bundle); len(got) != 1 {
		t.Errorf("dataDirCandidates without LOCALAPPDATA returned %d entries, want 1: %v", len(got), got)
	}
	if got := lay.searchRoots(noLocal, "/c/sdk"); len(got) != 1 {
		t.Errorf("searchRoots without LOCALAPPDATA returned %d entries, want 1: %v", len(got), got)
	}
}

// macOS derives its data candidates from the home directory, with the in-SDK
// location as the last resort.
func TestMacOSLayoutWithoutHomeKeepsInSDKCandidate(t *testing.T) {
	const bundle = "com.example.game"
	lay := darwinLayout()

	withHome := lay.dataDirCandidates(testEnv("/Users/u", nil, nil), "/opt/sdk", bundle)
	if len(withHome) != 3 {
		t.Errorf("with a home directory: %d candidates, want 3: %v", len(withHome), withHome)
	}

	noHome := Env{
		FS:      testEnv("", nil, nil).FS,
		Getenv:  func(string) string { return "" },
		HomeDir: func() (string, error) { return "", errNoHome },
	}
	got := lay.dataDirCandidates(noHome, "/opt/sdk", bundle)
	if len(got) != 1 {
		t.Fatalf("without a home directory: %d candidates, want just the in-SDK one: %v", len(got), got)
	}
	if !strings.HasPrefix(got[0], "/opt/sdk") {
		t.Errorf("remaining candidate %q is not the in-SDK path", got[0])
	}
}

// The depth limit is inclusive: a bundle sitting at exactly searchDepth below a
// search root is still in reach. Pinning the boundary rather than just "too deep
// is rejected", because an off-by-one here silently shrinks how far the fallback
// can rescue a wrong candidate list, which is the whole reason it exists.
func TestFindDataDirSearchIncludesExactDepthLimit(t *testing.T) {
	const bundle = "com.example.game"
	root := "/opt/sdk"
	// Search root is <root>/Disk/Data, so this is three segments below it:
	// "a", "b", then the bundle. Exactly searchDepth.
	atLimit := root + "/Disk/Data/a/b/" + bundle

	files := sdkFiles("linux", root)
	files[atLimit+"/mcp/game_logs.json"] = "[]"

	env := testEnv("/home/u", files, map[string]string{EnvVarSDKPath: root})
	p, err := resolveWith(env, linuxLayout())
	if err != nil {
		t.Fatalf("resolveWith: %v", err)
	}

	dir, found, tried := p.FindDataDir(env, bundle)
	if !found {
		t.Fatalf("a bundle at exactly the depth limit was not found; tried:\n  %s", strings.Join(tried, "\n  "))
	}
	if dir != atLimit {
		t.Errorf("dir = %q, want %q", dir, atLimit)
	}
}

// macOS search roots come from the home directory plus the SDK. Asserting the
// count both ways, and that every entry is absolute: a path built from an empty
// home would still be non-empty, just relative and useless.
func TestMacOSSearchRootsDependOnHome(t *testing.T) {
	lay := darwinLayout()

	withHome := lay.searchRoots(testEnv("/Users/u", nil, nil), "/opt/sdk")
	if len(withHome) != 2 {
		t.Errorf("with a home directory: %d search roots, want 2: %v", len(withHome), withHome)
	}

	noHome := Env{
		FS:      testEnv("", nil, nil).FS,
		Getenv:  func(string) string { return "" },
		HomeDir: func() (string, error) { return "", errNoHome },
	}
	got := lay.searchRoots(noHome, "/opt/sdk")
	if len(got) != 1 {
		t.Errorf("without a home directory: %d search roots, want just the in-SDK one: %v", len(got), got)
	}

	for _, name := range append(withHome, got...) {
		if !strings.HasPrefix(name, "/") {
			t.Errorf("search root %q is not absolute, so it would resolve against the process cwd", name)
		}
	}
}

// The order of the macOS candidates is a finding, not a preference, so it gets
// pinned. A game run on a real macOS install left its data at
// <sdk>/Disk/Data/<bundleID>, and nothing appeared under Application Support.
// That is the opposite of what macOS convention suggests, which is exactly why
// the original guess had it last, so this exists to stop a future tidy-up from
// reasoning its way back to the convention.
func TestMacOSPrefersInSDKDataDir(t *testing.T) {
	const bundle = "com.example.game"
	root := "/Users/u/Developer/PlaydateSDK"
	env := testEnv("/Users/u", sdkFiles("darwin", root), map[string]string{EnvVarSDKPath: root})
	p, err := resolveWith(env, darwinLayout())
	if err != nil {
		t.Fatalf("resolveWith: %v", err)
	}

	cands := p.DataDirCandidates(env, bundle)
	if len(cands) == 0 {
		t.Fatal("no candidates")
	}
	if want := root + "/Disk/Data/" + bundle; cands[0] != want {
		t.Errorf("first candidate = %q, want %q (confirmed against a real install)", cands[0], want)
	}
	// The Application Support paths stay as later candidates: the evidence is one
	// machine and one SDK version, and they cost a stat each only on a miss.
	if len(cands) < 2 {
		t.Error("the Application Support fallbacks were dropped; keep them behind the confirmed path")
	}
}
