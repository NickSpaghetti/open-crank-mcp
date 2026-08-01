// Package sdk answers two questions that used to be answered by string
// concatenation in four unconnected places: where is the Playdate SDK, and where
// are the things inside it on this platform.
//
// It exists because the server now runs two ways. In the container the SDK is at
// a known path in a known layout, and PLAYDATE_SDK_PATH is set for it. On a
// developer's own machine none of that holds: the SDK is wherever they installed
// it, the Simulator is an .app bundle on macOS and an .exe on Windows, and
// nothing sets an environment variable.
//
// Everything here takes its filesystem and environment as parameters rather than
// reaching for the real ones, and the per-platform layouts are values rather than
// build-tagged files. That combination is what lets the darwin and windows path
// logic be tested on any machine, which matters more than usual here: those
// layouts could not be verified by running them, so exercising them against a
// synthetic filesystem in CI is the next best thing. See sdk_test.go.
package sdk

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Env is the outside world, injected. Production callers use OSEnv.
type Env struct {
	// FS is rooted at the filesystem root, so an absolute OS path maps to a key
	// in it via fsKey. os.DirFS("/") in production, fstest.MapFS in tests.
	FS fs.FS
	// Getenv looks up an environment variable. os.Getenv in production.
	Getenv func(string) string
	// HomeDir returns the current user's home directory. os.UserHomeDir in
	// production.
	HomeDir func() (string, error)
}

// OSEnv is the real environment.
func OSEnv() Env {
	return Env{
		FS:      os.DirFS("/"),
		Getenv:  os.Getenv,
		HomeDir: os.UserHomeDir,
	}
}

// Paths is a resolved SDK: where it is, how that was decided, and the absolute
// paths of the two binaries this project runs out of it.
type Paths struct {
	// Root is the SDK directory.
	Root string
	// RootSource names what found Root, for error messages and `make sdk-path`.
	// Detection failing visibly is most of the value here.
	RootSource string
	// SimulatorBin is the executable itself, never an .app directory. On macOS
	// that means the Mach-O inside the bundle; see paths_darwin.go for why that
	// distinction is load-bearing.
	SimulatorBin string
	// PDC is the compiler.
	PDC string
	// Tried lists every candidate considered, in order, whether or not
	// resolution succeeded. "It looked here and here" is the only actionable
	// thing to say when it fails.
	Tried []string

	// goos records which platform layout produced these paths, so the data
	// directory methods can use the same one. Unexported and tolerant of being
	// unset: a Paths built as a struct literal in a test falls back to the host
	// layout rather than panicking on nil functions.
	goos string
}

// Sources for RootSource. Strings rather than an enum because they are only ever
// shown to a human.
const (
	SourceEnv     = "PLAYDATE_SDK_PATH"
	SourceConfig  = "SDKRoot in ~/.Playdate/config"
	SourceDefault = "default install location"
)

// EnvVarSDKPath is what the container sets, and what a user with a non-standard
// install can set. Checked first, so container behaviour is unchanged by any of
// this.
const EnvVarSDKPath = "PLAYDATE_SDK_PATH"

// EnvVarSimulatorBin overrides the resolved Simulator executable outright.
//
// The escape hatch for the thing here most likely to be wrong: the name of the
// .app bundle on macOS, which could not be verified without a real install. It
// turns "this doesn't work on my Mac" into a one-line fix rather than a blocker.
const EnvVarSimulatorBin = "OPEN_CRANK_SIMULATOR_BIN"

// Resolve locates the SDK and the binaries inside it, using the host's platform
// layout.
func Resolve(env Env) (Paths, error) {
	return resolveWith(env, hostLayout())
}

// resolveWith is Resolve against an explicit layout, so tests can ask for
// darwin's or windows' answers on any machine.
//
// Order: PLAYDATE_SDK_PATH, then SDKRoot in ~/.Playdate/config, then the per-OS
// default install locations. The environment variable winning outright is what
// makes this a no-op inside the container.
//
// ~/.Playdate/config is not a guess. It is the SDK's own file, written by its
// installer, and Panic's CMake support reads the same key out of it - see
// c-harness/test/fixture-game/CMakeLists.txt, which greps for exactly this.
//
// A returned error still carries a populated Paths.Tried.
func resolveWith(env Env, lay layout) (Paths, error) {
	var tried []string

	for _, cand := range candidateRoots(env, lay, &tried) {
		if p, ok := validate(env, lay, cand.root, cand.source, &tried); ok {
			return p, nil
		}
	}

	return Paths{Tried: tried, goos: lay.name}, fmt.Errorf(
		"could not find a Playdate SDK. Looked at:\n  %s\nSet %s to the SDK directory if it is somewhere else",
		strings.Join(tried, "\n  "), EnvVarSDKPath)
}

type rootCandidate struct {
	root   string
	source string
}

// candidateRoots is the resolution order, as data. Empty entries are skipped so
// callers do not have to check.
func candidateRoots(env Env, lay layout, tried *[]string) []rootCandidate {
	var out []rootCandidate
	add := func(root, source string) {
		if root != "" {
			out = append(out, rootCandidate{root: root, source: source})
		}
	}

	add(env.Getenv(EnvVarSDKPath), SourceEnv)
	add(configSDKRoot(env, tried), SourceConfig)
	for _, root := range lay.defaultRoots(env) {
		add(root, SourceDefault)
	}
	return out
}

// configSDKRoot reads the SDKRoot key out of ~/.Playdate/config.
//
// The file is `key<whitespace>value` per line. Parsed by splitting on whitespace
// rather than by column offset: the SDK's own CMake support does `cut -c9-`,
// which works only because "SDKRoot" happens to be seven characters plus one
// separator, and which breaks on any other key or a second space.
func configSDKRoot(env Env, tried *[]string) string {
	home, err := env.HomeDir()
	if err != nil {
		return ""
	}
	configPath := filepath.Join(home, ".Playdate", "config")
	*tried = append(*tried, configPath+" (SDKRoot key)")

	b, err := fs.ReadFile(env.FS, fsKey(configPath))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "SDKRoot" {
			return fields[1]
		}
	}
	return ""
}

// validate confirms a candidate root actually holds the binaries, and fills in
// their paths. A directory that exists but has no Simulator in it is not an SDK,
// and saying so here beats a confusing failure at launch time.
func validate(env Env, lay layout, root, source string, tried *[]string) (Paths, bool) {
	simBin, ok := lay.simulatorBin(env, root)
	if !ok {
		*tried = append(*tried, fmt.Sprintf("%s (no Simulator executable)", root))
		return Paths{}, false
	}
	pdc := lay.pdcBin(root)
	if !exists(env, pdc) {
		*tried = append(*tried, fmt.Sprintf("%s (no %s)", root, filepath.Base(pdc)))
		return Paths{}, false
	}

	// Applied after validation, deliberately. An override should not have to name
	// a whole valid SDK to correct one path inside it, and it has to still work
	// when the bundle-name probing has guessed wrong.
	if override := env.Getenv(EnvVarSimulatorBin); override != "" {
		simBin = override
	}

	*tried = append(*tried, root+" (found)")
	return Paths{
		Root:         root,
		RootSource:   source,
		SimulatorBin: simBin,
		PDC:          pdc,
		Tried:        *tried,
		goos:         lay.name,
	}, true
}

// layout returns the platform layout these paths came from, falling back to the
// host's. The fallback is what keeps a struct-literal Paths usable in tests.
func (p Paths) layout() layout {
	if p.goos == "" {
		return hostLayout()
	}
	return layoutFor(p.goos)
}

// BuildEnv is os.Environ() with PLAYDATE_SDK_PATH set to the resolved root.
//
// Needed because a C game's own CMakeLists.txt reads $ENV{PLAYDATE_SDK_PATH}.
// When the SDK came from the config file or a default location rather than the
// environment, that variable is unset in this process, and the SDK's CMake
// template falls through to a branch that shells out to bash, egrep, head and
// cut. Setting it here means the first branch always wins, on every platform.
//
// The existing value is replaced rather than appended to, since a stale one in
// the inherited environment would otherwise win.
func (p Paths) BuildEnv() []string {
	environ := os.Environ()
	out := make([]string, 0, len(environ)+1)
	for _, kv := range environ {
		if !strings.HasPrefix(kv, EnvVarSDKPath+"=") {
			out = append(out, kv)
		}
	}
	return append(out, EnvVarSDKPath+"="+p.Root)
}

// fsKey converts an absolute OS path into a key for an fs.FS rooted at the
// filesystem root.
//
// fs.FS paths are always slash-separated and never rooted, so a leading
// separator has to go. A Windows drive letter survives as a "C:/..." prefix,
// which is not a meaningful fs.FS path but is a consistent key, and is only ever
// used against a synthetic filesystem: Windows-native is unsupported at runtime.
func fsKey(path string) string {
	return strings.TrimPrefix(filepath.ToSlash(path), "/")
}

// exists reports whether path is present in env.FS.
func exists(env Env, path string) bool {
	_, err := fs.Stat(env.FS, fsKey(path))
	return err == nil
}

// isDir reports whether path is present in env.FS and is a directory.
func isDir(env Env, path string) bool {
	info, err := fs.Stat(env.FS, fsKey(path))
	return err == nil && info.IsDir()
}

// Describe renders a resolved SDK for a human: where it is, which of the three
// sources found it, and what was considered along the way.
//
// It lives here rather than in cmd/sdk-path because that makes it testable. The
// command around it is a shell that prints this and exits, which is the same
// shape as the other commands in this repo and excluded from mutation testing
// for the same reason. The formatting is not incidental though: "which source
// won" is the one thing a person debugging detection needs, and it is invisible
// everywhere else.
func (p Paths) Describe() string {
	var b strings.Builder
	fmt.Fprintf(&b, "root:       %s\n", p.Root)
	fmt.Fprintf(&b, "found via:  %s\n", p.RootSource)
	fmt.Fprintf(&b, "simulator:  %s\n", p.SimulatorBin)
	fmt.Fprintf(&b, "pdc:        %s\n", p.PDC)

	// Only worth printing when something was ruled out. A single entry means the
	// first candidate won, and listing it again under "considered" says nothing.
	if len(p.Tried) > 1 {
		b.WriteString("\nconsidered, in order:\n")
		for _, t := range p.Tried {
			fmt.Fprintf(&b, "  %s\n", t)
		}
	}
	return b.String()
}
