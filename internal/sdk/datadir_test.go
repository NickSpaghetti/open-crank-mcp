package sdk

import (
	"strings"
	"testing"
)

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
		{"windows", "/c/users/u", "/c/PlaydateSDK",
			"/c/users/u/AppData/Roaming/Playdate Simulator/Data/" + bundle},
	} {
		t.Run(tc.goos, func(t *testing.T) {
			environ := map[string]string{EnvVarSDKPath: tc.root}
			if tc.goos == "windows" {
				environ["APPDATA"] = "/c/users/u/AppData/Roaming"
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
	// An empty directory at the first candidate...
	files[support+"/Data/"+bundle+"/.keep"] = ""
	// ...and the real, harnessed one at the last.
	files[root+"/Disk/Data/"+bundle+"/mcp/game_logs.json"] = "[]"

	env := testEnv("/Users/u", files, map[string]string{EnvVarSDKPath: root})
	p, err := resolveWith(env, darwinLayout())
	if err != nil {
		t.Fatalf("resolveWith: %v", err)
	}

	dir, found, _ := p.FindDataDir(env, bundle)
	if !found {
		t.Fatal("FindDataDir found nothing, though a harnessed directory exists")
	}
	if want := root + "/Disk/Data/" + bundle; dir != want {
		t.Errorf("dir = %q, want %q (the harnessed one, not the empty earlier candidate)", dir, want)
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
