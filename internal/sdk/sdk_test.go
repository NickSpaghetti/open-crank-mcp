package sdk

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

// These tests are the reason this package takes its filesystem and its platform
// layout as parameters. The darwin and windows layouts cannot be verified by
// running them on the machine this was written on, so instead every one of them
// is exercised here against a synthetic filesystem, on whatever OS the tests
// happen to run on. What stays unverified is the path *values* against a real
// install, not the code that consumes them.

// testEnv builds an Env over a MapFS. Keys are absolute-looking paths with the
// leading slash stripped, matching fsKey.
func testEnv(home string, files map[string]string, environ map[string]string) Env {
	mapfs := fstest.MapFS{}
	for path, content := range files {
		mapfs[strings.TrimPrefix(path, "/")] = &fstest.MapFile{Data: []byte(content)}
	}
	return Env{
		FS:      mapfs,
		Getenv:  func(k string) string { return environ[k] },
		HomeDir: func() (string, error) { return home, nil },
	}
}

// sdkFiles returns the files that make a directory look like a real SDK for a
// given layout, so each test does not have to know each platform's layout.
func sdkFiles(goos, root string) map[string]string {
	switch goos {
	case "darwin":
		return map[string]string{
			root + "/bin/Playdate Simulator.app/Contents/MacOS/Playdate Simulator": "mach-o",
			root + "/bin/pdc": "pdc",
		}
	case "windows":
		return map[string]string{
			root + "/bin/PlaydateSimulator.exe": "exe",
			root + "/bin/pdc.exe":               "pdc",
		}
	default:
		return map[string]string{
			root + "/bin/PlaydateSimulator": "elf",
			root + "/bin/pdc":               "pdc",
		}
	}
}

func TestResolveOrder(t *testing.T) {
	// Every platform, so a change to one layout's precedence cannot pass here.
	for _, goos := range []string{"linux", "darwin", "windows"} {
		t.Run(goos+"/env wins over everything", func(t *testing.T) {
			files := map[string]string{}
			for p, c := range sdkFiles(goos, "/opt/from-env") {
				files[p] = c
			}
			for p, c := range sdkFiles(goos, "/opt/from-config") {
				files[p] = c
			}
			files["/home/u/.Playdate/config"] = "SDKRoot\t/opt/from-config\n"

			p, err := resolveWith(testEnv("/home/u", files,
				map[string]string{EnvVarSDKPath: "/opt/from-env"}), layoutFor(goos))
			if err != nil {
				t.Fatalf("resolveWith: %v", err)
			}
			if p.Root != "/opt/from-env" {
				t.Errorf("Root = %q, want /opt/from-env", p.Root)
			}
			if p.RootSource != SourceEnv {
				t.Errorf("RootSource = %q, want %q", p.RootSource, SourceEnv)
			}
		})

		t.Run(goos+"/config wins when env is unset", func(t *testing.T) {
			files := sdkFiles(goos, "/opt/from-config")
			files["/home/u/.Playdate/config"] = "SDKRoot\t/opt/from-config\n"

			p, err := resolveWith(testEnv("/home/u", files, nil), layoutFor(goos))
			if err != nil {
				t.Fatalf("resolveWith: %v", err)
			}
			if p.Root != "/opt/from-config" {
				t.Errorf("Root = %q, want /opt/from-config", p.Root)
			}
			if p.RootSource != SourceConfig {
				t.Errorf("RootSource = %q, want %q", p.RootSource, SourceConfig)
			}
		})
	}
}

// The default install location is per-platform, so this asserts each one is
// actually reachable rather than dead code.
func TestResolveFallsBackToDefaultLocation(t *testing.T) {
	for _, tc := range []struct {
		goos    string
		root    string
		environ map[string]string
	}{
		{"linux", "/home/u/PlaydateSDK", nil},
		{"darwin", "/home/u/Developer/PlaydateSDK", nil},
		// Confirmed install location. Windows also has no ~/.Playdate/config at
		// all, so this default is the only thing resolution has to go on there.
		{"windows", "/users/u/Documents/PlaydateSDK", map[string]string{"USERPROFILE": "/users/u"}},
	} {
		t.Run(tc.goos, func(t *testing.T) {
			p, err := resolveWith(testEnv("/home/u", sdkFiles(tc.goos, tc.root), tc.environ), layoutFor(tc.goos))
			if err != nil {
				t.Fatalf("resolveWith: %v", err)
			}
			if p.Root != tc.root {
				t.Errorf("Root = %q, want %q", p.Root, tc.root)
			}
			if p.RootSource != SourceDefault {
				t.Errorf("RootSource = %q, want %q", p.RootSource, SourceDefault)
			}
		})
	}
}

// A directory that exists but holds no Simulator is not an SDK. Getting this
// wrong means failing later, at launch, with something less clear.
func TestResolveRejectsIncompleteSDK(t *testing.T) {
	files := map[string]string{"/opt/not-an-sdk/bin/pdc": "pdc"} // no Simulator
	_, err := resolveWith(testEnv("/home/u", files,
		map[string]string{EnvVarSDKPath: "/opt/not-an-sdk"}), linuxLayout())
	if err == nil {
		t.Fatal("resolveWith succeeded on a directory with no Simulator executable")
	}
	if !strings.Contains(err.Error(), "no Simulator executable") {
		t.Errorf("error does not say what was missing: %v", err)
	}
}

// Failure has to be actionable: it must list what was looked at and name the
// variable that fixes it.
func TestResolveFailureIsDiagnosable(t *testing.T) {
	p, err := resolveWith(testEnv("/home/u", nil, nil), linuxLayout())
	if err == nil {
		t.Fatal("resolveWith succeeded with an empty filesystem")
	}
	if len(p.Tried) == 0 {
		t.Error("Paths.Tried is empty on failure, so a caller cannot report what was checked")
	}
	if !strings.Contains(err.Error(), EnvVarSDKPath) {
		t.Errorf("error does not mention %s: %v", EnvVarSDKPath, err)
	}
	for _, want := range []string{".Playdate/config", "PlaydateSDK"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q, so the user cannot tell where it looked: %v", want, err)
		}
	}
}

func TestConfigParsing(t *testing.T) {
	for _, tc := range []struct {
		name     string
		content  string
		wantRoot string
	}{
		{"tab separated, as the SDK writes it", "SDKRoot\t/opt/sdk\n", "/opt/sdk"},
		{"space separated", "SDKRoot /opt/sdk\n", "/opt/sdk"},
		{"leading whitespace", "  SDKRoot\t/opt/sdk\n", "/opt/sdk"},
		{"other keys present", "Foo\tbar\nSDKRoot\t/opt/sdk\nBaz\tqux\n", "/opt/sdk"},
		{"first SDKRoot wins", "SDKRoot\t/opt/sdk\nSDKRoot\t/opt/other\n", "/opt/sdk"},
		{"no trailing newline", "SDKRoot\t/opt/sdk", "/opt/sdk"},
		// Each of these must fall through to the default location rather than
		// resolving to something wrong.
		{"empty file", "", ""},
		{"key with no value", "SDKRoot\n", ""},
		{"unrelated content", "nonsense\n", ""},
		{"key is a prefix of another", "SDKRootExtra\t/opt/wrong\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{"/home/u/.Playdate/config": tc.content}
			for p, c := range sdkFiles("linux", "/opt/sdk") {
				files[p] = c
			}
			// A valid SDK also sits at the default location, so a failed parse
			// resolves there and the two cases are distinguishable.
			for p, c := range sdkFiles("linux", "/home/u/PlaydateSDK") {
				files[p] = c
			}

			p, err := resolveWith(testEnv("/home/u", files, nil), linuxLayout())
			if err != nil {
				t.Fatalf("resolveWith: %v", err)
			}
			if tc.wantRoot == "" {
				if p.RootSource != SourceDefault {
					t.Errorf("RootSource = %q, want the default location (config should not have parsed)", p.RootSource)
				}
				return
			}
			if p.Root != tc.wantRoot {
				t.Errorf("Root = %q, want %q", p.Root, tc.wantRoot)
			}
		})
	}
}

// A missing config file is the common case on a fresh machine, and must not be
// an error.
func TestResolveWithNoConfigFile(t *testing.T) {
	p, err := resolveWith(testEnv("/home/u", sdkFiles("linux", "/home/u/PlaydateSDK"), nil), linuxLayout())
	if err != nil {
		t.Fatalf("resolveWith: %v", err)
	}
	if p.RootSource != SourceDefault {
		t.Errorf("RootSource = %q, want %q", p.RootSource, SourceDefault)
	}
}

// macOS ships the Simulator as an application bundle, and the executable is
// nested inside it. Resolving to the bundle directory, or to `open`, would break
// get_logs silently - see paths_darwin.go.
func TestDarwinResolvesInnerExecutable(t *testing.T) {
	root := "/Users/u/Developer/PlaydateSDK"
	p, err := resolveWith(testEnv("/Users/u", sdkFiles("darwin", root), nil), darwinLayout())
	if err != nil {
		t.Fatalf("resolveWith: %v", err)
	}
	want := root + "/bin/Playdate Simulator.app/Contents/MacOS/Playdate Simulator"
	if p.SimulatorBin != want {
		t.Errorf("SimulatorBin = %q, want the inner Mach-O %q", p.SimulatorBin, want)
	}
	if strings.HasSuffix(p.SimulatorBin, ".app") {
		t.Error("SimulatorBin is the bundle directory, which is not executable")
	}
}

// The bundle name is the least certain value in this package, so the fallback
// name has to work too.
func TestDarwinProbesAlternateBundleName(t *testing.T) {
	root := "/Users/u/Developer/PlaydateSDK"
	files := map[string]string{
		root + "/bin/PlaydateSimulator.app/Contents/MacOS/PlaydateSimulator": "mach-o",
		root + "/bin/pdc": "pdc",
	}
	p, err := resolveWith(testEnv("/Users/u", files, nil), darwinLayout())
	if err != nil {
		t.Fatalf("resolveWith: %v", err)
	}
	if !strings.Contains(p.SimulatorBin, "PlaydateSimulator.app") {
		t.Errorf("SimulatorBin = %q, want the unspaced bundle name", p.SimulatorBin)
	}
}

// The escape hatch has to work even when probing found something, since its whole
// purpose is correcting a wrong guess.
func TestSimulatorBinOverride(t *testing.T) {
	root := "/opt/sdk"
	p, err := resolveWith(testEnv("/home/u", sdkFiles("linux", root), map[string]string{
		EnvVarSDKPath:      root,
		EnvVarSimulatorBin: "/somewhere/else/MySimulator",
	}), linuxLayout())
	if err != nil {
		t.Fatalf("resolveWith: %v", err)
	}
	if p.SimulatorBin != "/somewhere/else/MySimulator" {
		t.Errorf("SimulatorBin = %q, want the override", p.SimulatorBin)
	}
}

func TestWindowsUsesExeSuffixes(t *testing.T) {
	root := "/c/PlaydateSDK"
	p, err := resolveWith(testEnv("/c/users/u", sdkFiles("windows", root),
		map[string]string{EnvVarSDKPath: root}), windowsLayout())
	if err != nil {
		t.Fatalf("resolveWith: %v", err)
	}
	for _, got := range []string{p.SimulatorBin, p.PDC} {
		if !strings.HasSuffix(got, ".exe") {
			t.Errorf("%q does not end in .exe", got)
		}
	}
}

func TestBuildEnvSetsSDKPath(t *testing.T) {
	p := Paths{Root: "/opt/sdk"}
	var found int
	for _, kv := range p.BuildEnv() {
		if strings.HasPrefix(kv, EnvVarSDKPath+"=") {
			found++
			if kv != EnvVarSDKPath+"=/opt/sdk" {
				t.Errorf("got %q, want %s=/opt/sdk", kv, EnvVarSDKPath)
			}
		}
	}
	// Exactly one: a stale inherited value must be replaced, not shadowed, since
	// which one a child process sees would otherwise be unspecified.
	if found != 1 {
		t.Errorf("%s appears %d times in BuildEnv(), want exactly 1", EnvVarSDKPath, found)
	}
}

func TestFsKey(t *testing.T) {
	for in, want := range map[string]string{
		"/opt/sdk":       "opt/sdk",
		"/":              "",
		"opt/sdk":        "opt/sdk",
		"/a/b/c/file.so": "a/b/c/file.so",
	} {
		if got := fsKey(in); got != want {
			t.Errorf("fsKey(%q) = %q, want %q", in, got, want)
		}
	}
}

// Sanity check that MapFS keys and fsKey agree, since every other test depends
// on it.
func TestExistsAgreesWithMapFS(t *testing.T) {
	env := testEnv("/home/u", map[string]string{"/opt/sdk/bin/pdc": "pdc"}, nil)
	if !exists(env, "/opt/sdk/bin/pdc") {
		t.Error("exists() cannot see a file that is in the MapFS")
	}
	if exists(env, "/opt/sdk/bin/missing") {
		t.Error("exists() found a file that is not in the MapFS")
	}
	if !isDir(env, "/opt/sdk/bin") {
		t.Error("isDir() cannot see an implied parent directory")
	}
	if isDir(env, "/opt/sdk/bin/pdc") {
		t.Error("isDir() reported a regular file as a directory")
	}
}

var _ fs.FS = fstest.MapFS{}
