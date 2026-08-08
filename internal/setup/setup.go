// Package setup automates wiring open-crank-mcp's harness into a game
// project - copying the harness file(s) in and patching main.lua/
// CMakeLists.txt/the eventHandler file - instead of requiring a human to
// do it by hand.
package setup

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/NickSpaghetti/open-crank-mcp/internal/harness"
)

type Language string

const (
	Lua    Language = "lua"
	C      Language = "c"
	Hybrid Language = "hybrid"
)

// Languages are the values a caller may name, in the order they are offered.
//
// One list rather than three. This was a switch in internal/tools.resolveLanguage, a
// sentence in the error message next to it, and - once the tool schemas started
// declaring their closed sets as JSON Schema enums - about to become a third copy. Same
// shape as harness.ButtonNames and harness.DockModes, for the same reason.
var Languages = []Language{Lua, C, Hybrid}

// ValidLanguage reports whether l is one a caller may name. The empty string is not a
// language: it is how a caller asks for auto-detection, which is a different question.
func ValidLanguage(l Language) bool {
	for _, known := range Languages {
		if known == l {
			return true
		}
	}
	return false
}

// LanguageNames is Languages as plain strings, for error messages and schema enums.
func LanguageNames() []string {
	out := make([]string, len(Languages))
	for i, l := range Languages {
		out[i] = string(l)
	}
	return out
}

// FileChange describes one file setup or teardown touched.
type FileChange struct {
	Path    string `json:"path"`
	Changed bool   `json:"changed"` // false: already up to date, no-op
}

type SetupResult struct {
	Language     Language     `json:"language"`
	FilesCopied  []string     `json:"files_copied,omitempty"`
	FilesPatched []FileChange `json:"files_patched,omitempty"`
	// Steps setup couldn't safely automate - never a guess, always
	// something it confidently determined and stopped short of doing.
	ManualSteps []string `json:"manual_steps,omitempty"`
}

type TeardownResult struct {
	FilesRemoved []string     `json:"files_removed,omitempty"`
	FilesPatched []FileChange `json:"files_patched,omitempty"`
}

// DetectLanguage inspects sourceDir for the harness install pattern it
// needs: Source/main.lua + CMakeLists.txt both present means a hybrid
// C+Lua project (only main.lua needs anything, since Lua drives the update
// loop even when C extensions are present, see guides/harness-wiring.md).
// Deliberately
// separate from internal/build.DetectProjectType, whose "prefers C"
// tie-break answers a different question (which toolchain builds this),
// not "which harness(es) does it need."
//
// main.lua must be non-empty to count as a real Lua signal - some pure C
// SDK examples (e.g. the bundled "Sprite Game") ship a required-but-blank
// Source/main.lua stub alongside a real CMakeLists.txt. Treating its mere
// existence as "this project needs the Lua harness" would misdetect a
// pure C project as hybrid and skip installing the C harness entirely.
func DetectLanguage(sourceDir string) (Language, error) {
	hasLua := fileHasContent(filepath.Join(sourceDir, "Source", "main.lua"))
	hasC := fileExists(filepath.Join(sourceDir, "CMakeLists.txt"))
	switch {
	case hasLua && hasC:
		return Hybrid, nil
	case hasLua:
		return Lua, nil
	case hasC:
		return C, nil
	default:
		return "", fmt.Errorf("could not detect a Playdate project in %s: no CMakeLists.txt (C) or a non-empty Source/main.lua (Lua) found", sourceDir)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func fileHasContent(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(b))) > 0
}

// CopyHarnessFile writes one canonical harness source into a game, stamped with
// the fingerprint identifying which version it came from.
//
// name is a path inside harnessFS, so it is built with path.Join and never
// filepath.Join: fs.FS paths are slash-separated on every platform, and
// filepath.Join would produce backslashes on Windows that match nothing.
// dst is a real filesystem path and does use filepath.
//
// Exported because internal/contracttest builds its fixtures without going
// through Setup, and a fixture that skipped the stamp would be the one harness
// copy in the project that the drift check cannot see - so it uses this instead
// of copying the file itself.
func CopyHarnessFile(harnessFS fs.FS, name, dst string) error {
	b, err := fs.ReadFile(harnessFS, name)
	if err != nil {
		return fmt.Errorf("reading embedded %s: %w", name, err)
	}
	stamped, err := stampVersion(harnessFS, name, b)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(dst), err)
	}
	if err := os.WriteFile(dst, stamped, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", dst, err)
	}
	return nil
}

// stampVersion substitutes the version placeholder for this harness's
// fingerprint. Files that carry no placeholder are returned unchanged.
//
// A missing placeholder in a file that should have one is an error rather than a
// silent pass-through, and that is the whole reason this can be trusted: an
// unstamped copy would report nothing recognisable, the drift check would go
// quiet, and the next breaking harness change would land as silently as the one
// that prompted all this. Failing here turns that into a loud failure at setup
// time. The paired test asserting every embedded source still contains its
// placeholder is what catches it even earlier.
func stampVersion(harnessFS fs.FS, name string, content []byte) ([]byte, error) {
	fingerprint, err := harness.FingerprintFor(harnessFS, name)
	if err != nil {
		return nil, err
	}
	if fingerprint == "" {
		return content, nil
	}

	placeholder := []byte(harness.VersionPlaceholder)
	if n := bytes.Count(content, placeholder); n != 1 {
		return nil, fmt.Errorf(
			"embedded %s contains %d copies of the version placeholder %s, want exactly 1 - "+
				"without it, a game's harness copy cannot be identified and drift goes undetected",
			name, n, harness.VersionPlaceholder)
	}
	return bytes.Replace(content, placeholder, []byte(fingerprint), 1), nil
}

// Setup wires the harness into sourceDir for the given language.
//
// harnessFS carries the canonical harness sources, normally
// opencrank.HarnessFS. It is a parameter rather than a package-level default so
// tests can supply an fstest.MapFS, and so this package does not import the repo
// root (which would be an import cycle in waiting).
func Setup(sourceDir string, language Language, harnessFS fs.FS) (SetupResult, error) {
	// Checked rather than left to panic inside fs.ReadFile. A nil here is always
	// a wiring mistake (NewServer always supplies one), but `setup` is a
	// model-facing tool, and a named error is diagnosable where a nil-pointer
	// panic in a goroutine is not.
	if harnessFS == nil {
		return SetupResult{}, fmt.Errorf("no harness sources available: server was built without them")
	}

	// The directory has to be a Playdate project before anything is written into it.
	//
	// Only the auto-detect path used to check, because that path calls DetectLanguage to
	// answer a different question and got the check for free. With an explicit language,
	// nothing looked: setupLua creates Source/ on its way to writing the harness, so
	// `setup(source_dir: "QBSRK", language: "lua")` created QBSRK/Source/mcp_harness.lua
	// in the working directory, failed afterwards on the main.lua it could not patch, and
	// left the tree behind. Found by Specmatic's auto-test, which generates a random
	// string for a path and produced four such directories in the repo root before
	// anyone noticed.
	//
	// DetectLanguage rather than a bare os.Stat, so an unrelated directory is refused
	// too, not just a nonexistent one. Its *answer* is deliberately discarded: an
	// explicit language is allowed to disagree with detection - that is what it is for,
	// e.g. treating a hybrid project as Lua-only. All this requires is that there is a
	// project here at all.
	if _, err := DetectLanguage(sourceDir); err != nil {
		return SetupResult{}, err
	}

	switch language {
	case Lua, Hybrid:
		return setupLua(sourceDir, harnessFS, language)
	case C:
		return setupC(sourceDir, harnessFS)
	default:
		return SetupResult{}, fmt.Errorf("unknown language %q", language)
	}
}

// Teardown removes whatever Setup would have added for the given language.
func Teardown(sourceDir string, language Language) (TeardownResult, error) {
	switch language {
	case Lua, Hybrid:
		return teardownLua(sourceDir)
	case C:
		return teardownC(sourceDir)
	default:
		return TeardownResult{}, fmt.Errorf("unknown language %q", language)
	}
}
