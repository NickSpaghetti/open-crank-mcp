// Package setup wires the harness into a game project: it copies the harness
// in and patches main.lua, CMakeLists.txt and the eventHandler file.
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
	// Steps setup determined were needed but couldn't safely automate.
	ManualSteps []string `json:"manual_steps,omitempty"`
}

type TeardownResult struct {
	FilesRemoved []string     `json:"files_removed,omitempty"`
	FilesPatched []FileChange `json:"files_patched,omitempty"`
}

// DetectLanguage reports which harness sourceDir needs. main.lua must be
// non-empty to count: some pure C SDK examples (e.g. "Sprite Game") ship a
// blank Source/main.lua stub that would otherwise read as hybrid.
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

// CopyHarnessFile writes one harness source into a game, stamped with its
// version fingerprint. name is an fs.FS path (path.Join, never filepath.Join:
// backslashes match nothing in an fs.FS on Windows).
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
// fingerprint. A fingerprinted file without exactly one placeholder is an
// error, since an unstamped copy would make drift undetectable.
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

// Setup wires the harness into sourceDir for the given language. harnessFS is
// normally opencrank.HarnessFS.
func Setup(sourceDir string, language Language, harnessFS fs.FS) (SetupResult, error) {
	if harnessFS == nil {
		return SetupResult{}, fmt.Errorf("no harness sources available: server was built without them")
	}

	// Refuse a non-project before writing anything. The detected language is
	// discarded: an explicit one may disagree (e.g. a hybrid treated as Lua-only).
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
