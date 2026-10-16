package setup

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// setupLua covers both Lua and Hybrid: in a hybrid project the Lua VM still
// drives the update loop, so only the Lua harness is needed.
func setupLua(sourceDir string, harnessFS fs.FS, language Language) (SetupResult, error) {
	result := SetupResult{Language: language}

	harnessDst := filepath.Join(sourceDir, "Source", "mcp_harness.lua")
	if err := CopyHarnessFile(harnessFS, path.Join("lua", "mcp_harness.lua"), harnessDst); err != nil {
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

// patchLuaMain appends a marker block importing mcp_harness unless the import
// line is already present (hand-wired projects have no markers). Placement
// doesn't matter: the harness hooks playdate.update via __newindex.
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

	// Keep the harness if an unmarked import still references it, or the next
	// build breaks.
	harnessPath := filepath.Join(sourceDir, "Source", "mcp_harness.lua")
	if !stillReferenced && fileExists(harnessPath) {
		if err := os.Remove(harnessPath); err != nil {
			return result, fmt.Errorf("removing mcp_harness.lua: %w", err)
		}
		result.FilesRemoved = append(result.FilesRemoved, harnessPath)
	}

	return result, nil
}
