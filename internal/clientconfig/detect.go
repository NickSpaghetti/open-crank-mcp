package clientconfig

import (
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/NickSpaghetti/open-crank-mcp/internal/sdk"
)

// LauncherRelPath is where the plugin launcher sits inside a plugin root.
const LauncherRelPath = "bin/open-crank-mcp-launcher"

// Detect works out which command a client should be told to run.
//
// There are exactly two ways someone has a binary to run this from, and they
// need different answers:
//
//   - A release binary, downloaded or cached by the launcher. There is no plugin
//     directory to point at, so the command is the binary itself and nothing
//     resolves at startup.
//   - A checkout, where `make go-build` produced a binary inside the repository
//     and plugin/bin/open-crank-mcp-launcher exists beside it. The launcher is
//     the better command - it keeps working across versions - but it may
//     download on first use, which is what Invocation.Resolves records.
//
// Detected rather than asked about, because the caller already has the binary
// and the answer is knowable from where it sits. -print-config takes overrides
// anyway, since a wrong guess here would otherwise be silent.
//
// Takes its filesystem as a parameter, the way internal/sdk does, so every
// branch is testable with fstest.MapFS rather than only on a machine that
// happens to be laid out the right way.
func Detect(env sdk.Env, executable string) Invocation {
	dir := filepath.Dir(executable)
	for {
		launcher := filepath.Join(dir, "plugin", LauncherRelPath)
		manifest := filepath.Join(dir, "plugin", "plugin.json")
		if exists(env, launcher) && exists(env, manifest) {
			return Invocation{Argv: []string{launcher}, Resolves: true}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached the filesystem root without finding a plugin directory.
			return Invocation{Argv: []string{executable}}
		}
		dir = parent
	}
}

// exists reports whether a path is present in env.FS.
//
// fs.FS keys are slash-separated and never rooted, so the leading separator is
// trimmed - the same conversion internal/sdk does, and the same trap: getting it
// wrong fails open, reporting "not found" for everything and quietly turning
// every checkout into the release-binary answer.
func exists(env sdk.Env, path string) bool {
	key := strings.TrimPrefix(filepath.ToSlash(path), "/")
	if key == "" {
		return false
	}
	_, err := fs.Stat(env.FS, key)
	return err == nil
}
