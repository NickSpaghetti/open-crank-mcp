package sdk

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// errNoHome stands in for os.UserHomeDir failing, as it does with no HOME set.
var errNoHome = errors.New("no home directory")

// dataDirEnv builds an env with a harnessed data directory at each of the given
// paths, plus a valid SDK at root.
func dataDirEnv(goos, home, root string, harnessed []string, environ map[string]string) Env {
	files := sdkFiles(goos, root)
	for _, dir := range harnessed {
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
			if filepath.ToSlash(dir) != tc.harnessed {
				t.Errorf("dir = %q, want %q", dir, tc.harnessed)
			}
		})
	}
}

// A candidate that exists but has no mcp/ in it is not the answer.
func TestFindDataDirSkipsCandidatesWithoutHarnessDir(t *testing.T) {
	const bundle = "com.example.game"
	root := "/Users/u/Developer/PlaydateSDK"
	support := "/Users/u/Library/Application Support/Playdate Simulator"

	files := sdkFiles("darwin", root)
	files[root+"/Disk/Data/"+bundle+"/.keep"] = ""
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
	if want := support + "/Data/" + bundle; filepath.ToSlash(dir) != want {
		t.Errorf("dir = %q, want %q (the harnessed one, not the empty first candidate)", dir, want)
	}
}

func TestFindDataDirHonoursDataRootOverride(t *testing.T) {
	const bundle = "com.example.game"
	root := "/opt/sdk"
	override := "/somewhere/unexpected"

	files := sdkFiles("linux", root)
	files[override+"/"+bundle+"/mcp/game_logs.json"] = "[]"
	// A valid default also exists, to prove the override wins.
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
	if want := override + "/" + bundle; filepath.ToSlash(dir) != want {
		t.Errorf("dir = %q, want %q", dir, want)
	}
}

// The harnessed directory is under a search root at a path no candidate names.
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
	if filepath.ToSlash(dir) != unexpected {
		t.Errorf("dir = %q, want %q", dir, unexpected)
	}
}

// Not finding it is not an error: a game with no harness yet is legitimately
// launched before setup. The contract is a usable fallback plus everything tried.
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

// A struct-literal Paths has no goos and must fall back to the host layout.
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

func TestFindDataDirSearchRespectsDepthLimit(t *testing.T) {
	const bundle = "com.example.game"
	root := "/opt/sdk"
	// Five levels below the <root>/Disk/Data search root, past the limit.
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

func TestFindDataDirSearchRespectsNodeBudget(t *testing.T) {
	const bundle = "com.example.game"
	root := "/opt/sdk"
	target := root + "/Disk/Data/nested/" + bundle

	files := sdkFiles("linux", root)
	files[target+"/mcp/game_logs.json"] = "[]"
	// Siblings that sort ahead of "nested", so a tiny budget is spent first.
	for _, name := range []string{"aaa", "aab", "aac", "aad", "aae"} {
		files[root+"/Disk/Data/"+name+"/filler"] = ""
	}

	env := testEnv("/home/u", files, map[string]string{EnvVarSDKPath: root})
	p, err := resolveWith(env, linuxLayout())
	if err != nil {
		t.Fatalf("resolveWith: %v", err)
	}

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

// Missing environment must drop candidates, not produce paths with empty segments.
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

			if roots := lay.defaultRoots(noHome); len(roots) != 0 {
				t.Errorf("defaultRoots with no home returned %v, want none", roots)
			}

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

// Windows derives its locations from LOCALAPPDATA and USERPROFILE. A missing one
// must drop its candidate rather than contribute a malformed path.
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

	if want := "/c/users/u/Documents/PlaydateSDK"; filepath.ToSlash(roots[0]) != want {
		t.Errorf("first default root = %q, want %q (where the installer puts it)", roots[0], want)
	}

	partial := testEnv("/c/users/u", nil, map[string]string{
		"USERPROFILE": "/c/users/u",
	})
	if got := lay.defaultRoots(partial); len(got) != 2 {
		t.Errorf("defaultRoots without LOCALAPPDATA returned %d entries, want 2: %v", len(got), got)
	}

	noLocal := testEnv("/c/users/u", nil, nil)
	if got := lay.dataDirCandidates(noLocal, "/c/sdk", bundle); len(got) != 1 {
		t.Errorf("dataDirCandidates without LOCALAPPDATA returned %d entries, want 1: %v", len(got), got)
	}
	if got := lay.searchRoots(noLocal, "/c/sdk"); len(got) != 1 {
		t.Errorf("searchRoots without LOCALAPPDATA returned %d entries, want 1: %v", len(got), got)
	}
}

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
	if !strings.HasPrefix(filepath.ToSlash(got[0]), "/opt/sdk") {
		t.Errorf("remaining candidate %q is not the in-SDK path", got[0])
	}
}

// The depth limit is inclusive: a bundle exactly searchDepth below a search root is in reach.
func TestFindDataDirSearchIncludesExactDepthLimit(t *testing.T) {
	const bundle = "com.example.game"
	root := "/opt/sdk"
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
	if filepath.ToSlash(dir) != atLimit {
		t.Errorf("dir = %q, want %q", dir, atLimit)
	}
}

// A path built from an empty home would be relative, so assert every root is absolute.
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
		if !strings.HasPrefix(filepath.ToSlash(name), "/") {
			t.Errorf("search root %q is not absolute, so it would resolve against the process cwd", name)
		}
	}
}

// A real macOS Simulator keeps game data in the SDK, not Application Support,
// despite macOS convention.
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
	if want := root + "/Disk/Data/" + bundle; filepath.ToSlash(cands[0]) != want {
		t.Errorf("first candidate = %q, want %q (confirmed against a real install)", cands[0], want)
	}
	if len(cands) < 2 {
		t.Error("the Application Support fallbacks were dropped; keep them behind the confirmed path")
	}
}

// A "C:/..." root has no leading separator; the hit must not come back as "/C:/...".
func TestSearchForBundleKeepsAWindowsStyleRoot(t *testing.T) {
	const bundle = "dev.example.game"
	const root = "C:/Users/u/Documents/PlaydateSDK/Disk/Data"

	env := testEnv("C:/Users/u", map[string]string{
		root + "/" + bundle + "/" + harnessDir + "/keep": "",
	}, nil)

	got, found := searchForBundle(env, root, bundle)
	if !found {
		t.Fatalf("searchForBundle did not find %s under %s", bundle, root)
	}
	if want := root + "/" + bundle; filepath.ToSlash(got) != want {
		t.Errorf("searchForBundle = %q, want %q", filepath.ToSlash(got), want)
	}
}
