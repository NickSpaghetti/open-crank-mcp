package setup

import (
	"fmt"
	"os"
	"path/filepath"
)

// repoRoot locates this project's own repo root (where the canonical
// lua/mcp_harness.lua and c-harness/mcp_harness.{h,c} live) by walking up
// from the current working directory looking for go.mod - the same
// pattern internal/contracttest's own findRepoRoot uses. The Docker image
// this project runs from sets WORKDIR to the repo root and copies the
// whole tree there, so this is normally a zero-hop lookup in practice;
// walking up is just robustness against being invoked from a subdirectory.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("Getwd: %w", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find repo root (no go.mod in any parent directory of %s)", dir)
		}
		dir = parent
	}
}
