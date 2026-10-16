// Package build detects a Playdate game's project type and builds it into
// a .pdx.
package build

import (
	"fmt"
	"os"
	"path/filepath"
)

// ProjectType identifies which toolchain a game's source directory needs.
type ProjectType int

const (
	ProjectTypeC ProjectType = iota
	ProjectTypeLua
)

func (t ProjectType) String() string {
	switch t {
	case ProjectTypeC:
		return "C"
	case ProjectTypeLua:
		return "Lua"
	default:
		return fmt.Sprintf("ProjectType(%d)", int(t))
	}
}

// DetectProjectType inspects sourceDir and reports whether it's a C or Lua
// game. A CMakeLists.txt at the root means C; a Source/main.lua means Lua.
func DetectProjectType(sourceDir string) (ProjectType, error) {
	if _, err := os.Stat(filepath.Join(sourceDir, "CMakeLists.txt")); err == nil {
		return ProjectTypeC, nil
	}
	if _, err := os.Stat(filepath.Join(sourceDir, "Source", "main.lua")); err == nil {
		return ProjectTypeLua, nil
	}
	return 0, fmt.Errorf("could not detect project type in %s: no CMakeLists.txt (C) or Source/main.lua (Lua) found", sourceDir)
}

// locatePDX finds the single .pdx produced by a C build. The SDK's CMake support
// names it after the game's PLAYDATE_GAME_NAME, so it can't be predicted.
func locatePDX(dir string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.pdx"))
	if err != nil {
		return "", fmt.Errorf("globbing for .pdx in %s: %w", dir, err)
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no .pdx found in %s after build", dir)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("ambiguous: found %d .pdx files in %s (%v), remove stale build output and retry", len(matches), dir, matches)
	}
}
