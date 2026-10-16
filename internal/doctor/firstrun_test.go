package doctor

import (
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/NickSpaghetti/open-crank-mcp/internal/sdk"
)

func envWith(home string, files fstest.MapFS) sdk.Env {
	return sdk.Env{
		FS:      files,
		Getenv:  func(string) string { return "" },
		HomeDir: func() (string, error) { return home, nil },
	}
}

func TestSimulatorEverRan(t *testing.T) {
	const home = "/Users/someone"
	prefs := home + "/" + simulatorPrefs

	for _, tc := range []struct {
		name      string
		goos      string
		files     fstest.MapFS
		wantRan   bool
		wantKnown bool
	}{
		{
			name:      "darwin, plist present, so it has run",
			goos:      "darwin",
			files:     fstest.MapFS{prefs[1:]: {Data: []byte("bplist00")}},
			wantRan:   true,
			wantKnown: true,
		},
		{
			name:      "darwin, plist absent, so it probably never has",
			goos:      "darwin",
			files:     fstest.MapFS{},
			wantRan:   false,
			wantKnown: true,
		},
		{
			// Unknowable off macOS, not "never ran".
			name:      "linux, not applicable",
			goos:      "linux",
			files:     fstest.MapFS{},
			wantRan:   false,
			wantKnown: false,
		},
		{
			name:      "windows, not applicable",
			goos:      "windows",
			files:     fstest.MapFS{},
			wantRan:   false,
			wantKnown: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ran, known := SimulatorEverRan(envWith(home, tc.files), tc.goos)
			if ran != tc.wantRan || known != tc.wantKnown {
				t.Errorf("SimulatorEverRan = (ran=%v, known=%v), want (ran=%v, known=%v)",
					ran, known, tc.wantRan, tc.wantKnown)
			}
		})
	}
}

// A home directory that cannot be determined is "unknown", never "never ran".
func TestSimulatorEverRanUnknownWithoutHome(t *testing.T) {
	env := sdk.Env{
		FS:      fstest.MapFS{},
		Getenv:  func(string) string { return "" },
		HomeDir: func() (string, error) { return "", errors.New("no home") },
	}
	if ran, known := SimulatorEverRan(env, "darwin"); ran || known {
		t.Errorf("SimulatorEverRan with no home = (%v, %v), want (false, false)", ran, known)
	}
}

// A rooted fs.FS key fails fs.Stat, which would read as "never ran" on every Mac.
func TestSimulatorPrefsKeyIsRootless(t *testing.T) {
	const home = "/Users/someone"
	key := home[1:] + "/" + simulatorPrefs
	if !fs.ValidPath(key) {
		t.Fatalf("%q is not a valid fs.FS path", key)
	}
	files := fstest.MapFS{key: {Data: []byte("bplist00")}}
	if ran, _ := SimulatorEverRan(envWith(home, files), "darwin"); !ran {
		t.Error("a plist present at the rootless key was not found, so the key conversion is wrong")
	}
}
