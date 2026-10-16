// Package sdk locates the Playdate SDK and the per-platform paths inside it.
// Filesystem and environment are injected so every layout is testable on every OS.
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
	FS      fs.FS // keyed by fsKey(absolute path)
	Getenv  func(string) string
	HomeDir func() (string, error)
}

// OSEnv is the real environment.
func OSEnv() Env {
	return Env{
		FS:      osPathFS{},
		Getenv:  os.Getenv,
		HomeDir: os.UserHomeDir,
	}
}

// osPathFS adapts absolute OS paths to fs.FS. Unlike os.DirFS("/"), it handles
// Windows drive-letter paths.
type osPathFS struct{}

func (osPathFS) Open(name string) (fs.File, error) {
	path := filepath.FromSlash(name)
	if !filepath.IsAbs(path) {
		path = string(os.PathSeparator) + path
	}
	return os.Open(path)
}

// Paths is a resolved SDK: where it is, how that was decided, and its binaries.
type Paths struct {
	Root       string
	RootSource string // what found Root, for error messages and `make sdk-path`
	// SimulatorBin is the executable itself, never an .app directory; see darwinLayout.
	SimulatorBin string
	PDC          string
	Tried        []string // every candidate considered, in order, even on success

	goos string // layout that produced these paths; empty means host
}

// Sources for RootSource.
const (
	SourceEnv     = "PLAYDATE_SDK_PATH"
	SourceConfig  = "SDKRoot in ~/.Playdate/config"
	SourceDefault = "default install location"
)

// EnvVarSDKPath is set by the container or a user with a non-standard install. Checked first.
const EnvVarSDKPath = "PLAYDATE_SDK_PATH"

// EnvVarSimulatorBin replaces the Simulator path, so a root validates on pdc alone
// when the bundle-name probe is wrong.
const EnvVarSimulatorBin = "OPEN_CRANK_SIMULATOR_BIN"

// Resolve locates the SDK and the binaries inside it, using the host's platform
// layout.
func Resolve(env Env) (Paths, error) {
	return resolveWith(env, hostLayout())
}

// resolveWith is Resolve against an explicit layout. Order: PLAYDATE_SDK_PATH,
// SDKRoot in the SDK installer's ~/.Playdate/config, then default roots.
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

// candidateRoots is the resolution order, as data. Empty entries are skipped.
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

// configSDKRoot reads the SDKRoot key out of ~/.Playdate/config, whose lines are
// `key<whitespace>value`. Split on whitespace, not column offset like the SDK's `cut -c9-`.
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
// their paths. "Missing" and "not an SDK" are reported differently.
func validate(env Env, lay layout, root, source string, tried *[]string) (Paths, bool) {
	if !isDir(env, root) {
		*tried = append(*tried, fmt.Sprintf("%s (does not exist)", root))
		return Paths{}, false
	}

	simBin := env.Getenv(EnvVarSimulatorBin)
	if simBin == "" {
		var ok bool
		if simBin, ok = lay.simulatorBin(env, root); !ok {
			*tried = append(*tried, fmt.Sprintf("%s (no Simulator executable)", root))
			return Paths{}, false
		}
	}
	pdc := lay.pdcBin(root)
	if !exists(env, pdc) {
		*tried = append(*tried, fmt.Sprintf("%s (no %s)", root, filepath.Base(pdc)))
		return Paths{}, false
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

// layout returns the platform layout these paths came from, falling back to the host's.
func (p Paths) layout() layout {
	if p.goos == "" {
		return hostLayout()
	}
	return layoutFor(p.goos)
}

// BuildEnv is os.Environ() with PLAYDATE_SDK_PATH replaced by the resolved root.
// Without it the SDK's CMake template falls back to shelling out to bash, egrep and cut.
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

// fsKey converts an absolute OS path into an fs.FS key: slash-separated with no
// leading slash. Windows drive letters stay as "C:/...", which osPathFS understands.
func fsKey(path string) string {
	return strings.TrimPrefix(filepath.ToSlash(path), "/")
}

// Exists reports whether an absolute OS path exists in env.FS.
func Exists(env Env, path string) bool { return exists(env, path) }

func exists(env Env, path string) bool {
	_, err := fs.Stat(env.FS, fsKey(path))
	return err == nil
}

func isDir(env Env, path string) bool {
	info, err := fs.Stat(env.FS, fsKey(path))
	return err == nil && info.IsDir()
}

// Describe renders a resolved SDK for a human: where it is, which source found
// it, and what was considered along the way.
func (p Paths) Describe() string {
	var b strings.Builder
	fmt.Fprintf(&b, "root:       %s\n", p.Root)
	fmt.Fprintf(&b, "found via:  %s\n", p.RootSource)
	fmt.Fprintf(&b, "simulator:  %s\n", p.SimulatorBin)
	fmt.Fprintf(&b, "pdc:        %s\n", p.PDC)

	// A single entry means the first candidate won; nothing worth listing.
	if len(p.Tried) > 1 {
		b.WriteString("\nconsidered, in order:\n")
		for _, t := range p.Tried {
			fmt.Fprintf(&b, "  %s\n", t)
		}
	}
	return b.String()
}
