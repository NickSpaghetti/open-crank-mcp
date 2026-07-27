// Package setup automates wiring open-crank-mcp's harness into a game
// project - copying the harness file(s) in and patching main.lua/
// CMakeLists.txt/the eventHandler file - instead of requiring a human to
// do it by hand.
package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Language string

const (
	Lua    Language = "lua"
	C      Language = "c"
	Hybrid Language = "hybrid"
)

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
// C+Lua project (only main.lua needs anything - Lua drives the update
// loop even when C extensions are present, see the README). Deliberately
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

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("reading %s: %w", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(dst), err)
	}
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", dst, err)
	}
	return nil
}

// Setup wires the harness into sourceDir for the given language.
func Setup(sourceDir string, language Language) (SetupResult, error) {
	root, err := repoRoot()
	if err != nil {
		return SetupResult{}, err
	}
	switch language {
	case Lua, Hybrid:
		return setupLua(sourceDir, root, language)
	case C:
		return setupC(sourceDir, root)
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
