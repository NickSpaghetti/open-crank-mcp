package setup

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// setupLua covers both Lua and Hybrid: a hybrid C+Lua project only needs
// the Lua harness, since a real Lua VM still drives the update loop (see
// README's "Hybrid C+Lua games" note) - so this is deliberately the same
// steps regardless of which of those two languages was detected, and
// never touches CMakeLists.txt or any .c file.
func setupLua(sourceDir string, harnessFS fs.FS, language Language) (SetupResult, error) {
	result := SetupResult{Language: language}

	harnessDst := filepath.Join(sourceDir, "Source", "mcp_harness.lua")
	if err := copyHarnessFile(harnessFS, path.Join("lua", "mcp_harness.lua"), harnessDst); err != nil {
		return result, fmt.Errorf("copying mcp_harness.lua: %w", err)
	}
	result.FilesCopied = append(result.FilesCopied, harnessDst)

	mainPath := filepath.Join(sourceDir, "Source", "main.lua")
	changed, err := patchLuaMain(mainPath)
	if err != nil {
		return result, fmt.Errorf("patching main.lua: %w", err)
	}
	result.FilesPatched = append(result.FilesPatched, FileChange{Path: mainPath, Changed: changed})

	return result, nil
}

const luaImportLine = `import "mcp_harness"`

// patchLuaMain appends a marker block importing mcp_harness, if the
// import isn't already present in some form - checked against the
// literal import line, not just hasMarkerBlock, so a project hand-wired
// before this tool existed (no markers) doesn't get a duplicate import.
// Harmless either way per the SDK docs ("a second import call from
// anywhere in the pdz will do nothing"), but there's no reason to add
// one. Where in the file the import goes doesn't matter - the harness's
// own __newindex hook on the playdate table intercepts a playdate.update
// assignment whenever and wherever it happens, so this works whether the
// import lands first (letting mcp.registerState() etc. be called
// anywhere afterward, the natural place for it) or last.
func patchLuaMain(mainPath string) (bool, error) {
	b, err := os.ReadFile(mainPath)
	if err != nil {
		return false, err
	}
	content := string(b)
	if strings.Contains(content, luaImportLine) {
		return false, nil
	}

	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += "\n" + markerBlock(luaMarkerBegin, luaMarkerEnd, luaImportLine)

	return true, os.WriteFile(mainPath, []byte(content), 0o644)
}

func teardownLua(sourceDir string) (TeardownResult, error) {
	result := TeardownResult{}

	mainPath := filepath.Join(sourceDir, "Source", "main.lua")
	stillReferenced := false
	b, err := os.ReadFile(mainPath)
	if err != nil && !os.IsNotExist(err) {
		return result, fmt.Errorf("reading main.lua: %w", err)
	}
	if err == nil {
		content, changed := stripMarkerBlocks(string(b), luaMarkerBegin, luaMarkerEnd)
		if changed {
			if err := os.WriteFile(mainPath, []byte(content), 0o644); err != nil {
				return result, fmt.Errorf("patching main.lua: %w", err)
			}
		}
		result.FilesPatched = append(result.FilesPatched, FileChange{Path: mainPath, Changed: changed})
		stillReferenced = strings.Contains(content, luaImportLine)
	}

	// Only remove the vendored harness copy if nothing still imports it -
	// a hand-written (unmarked) import this tool doesn't touch would
	// otherwise be left pointing at a file that no longer exists, break-
	// ing the next build.
	harnessPath := filepath.Join(sourceDir, "Source", "mcp_harness.lua")
	if !stillReferenced && fileExists(harnessPath) {
		if err := os.Remove(harnessPath); err != nil {
			return result, fmt.Errorf("removing mcp_harness.lua: %w", err)
		}
		result.FilesRemoved = append(result.FilesRemoved, harnessPath)
	}

	return result, nil
}
