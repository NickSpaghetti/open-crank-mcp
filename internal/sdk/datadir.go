package sdk

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

// EnvVarDataRoot overrides the *parent* of the per-bundle data directory.
const EnvVarDataRoot = "OPEN_CRANK_DATA_ROOT"

// harnessDir is created by the harness inside the sandboxed data directory, which
// makes the right directory recognisable rather than predicted.
const harnessDir = "mcp"

// searchDepth bounds the last-resort walk; enough for <root>/Data/<bundleID>.
const searchDepth = 3

// searchNodeBudget bounds the walk by node count so a huge root gives up rather
// than hangs. A var so tests can shrink it.
var searchNodeBudget = 4000

// DataDirCandidates lists the plausible per-bundle data directories for this
// platform, most likely first.
func (p Paths) DataDirCandidates(env Env, bundleID string) []string {
	if root := env.Getenv(EnvVarDataRoot); root != "" {
		return []string{filepath.Join(root, bundleID)}
	}
	return p.layout().dataDirCandidates(env, p.Root, bundleID)
}

// FindDataDir returns the sandboxed data directory for bundleID: the first
// candidate containing mcp/, else a bounded walk of the search roots. When found
// is false, dir is the first candidate; a game with no harness yet is legitimate.
func (p Paths) FindDataDir(env Env, bundleID string) (dir string, found bool, tried []string) {
	candidates := p.DataDirCandidates(env, bundleID)
	tried = append(tried, candidates...)

	for _, cand := range candidates {
		if isDir(env, filepath.Join(cand, harnessDir)) {
			return cand, true, tried
		}
	}

	for _, root := range p.layout().searchRoots(env, p.Root) {
		tried = append(tried, root+" (searched)")
		if hit, ok := searchForBundle(env, root, bundleID); ok {
			return hit, true, tried
		}
	}

	if len(candidates) == 0 {
		return "", false, tried
	}
	return candidates[0], false, tried
}

// searchForBundle walks root looking for a directory named bundleID that
// contains mcp/.
func searchForBundle(env Env, root, bundleID string) (string, bool) {
	rootKey := fsKey(root)
	if !isDir(env, root) {
		return "", false
	}

	budget := searchNodeBudget
	var hit string
	// An unreadable subtree means keep looking, not fail.
	_ = fs.WalkDir(env.FS, rootKey, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return fs.SkipDir
		}
		if budget <= 0 {
			return fs.SkipAll
		}
		budget--
		if !d.IsDir() {
			return nil
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(p, rootKey), "/")
		if rel != "" && strings.Count(rel, "/")+1 > searchDepth {
			return fs.SkipDir
		}
		if d.Name() == bundleID {
			if _, err := fs.Stat(env.FS, p+"/"+harnessDir); err == nil {
				hit = filepath.Join(root, filepath.FromSlash(rel))
				return fs.SkipAll
			}
		}
		return nil
	})
	if hit == "" {
		return "", false
	}
	return hit, true
}

// DataDirDiagnostic is the warning returned when the data directory could not
// be confirmed, so the agent learns it before hitting harness timeouts.
func DataDirDiagnostic(bundleID string, tried []string) string {
	return fmt.Sprintf(
		"warning: could not confirm the Simulator's data directory for %s. "+
			"Every harness-dependent tool will time out if this is wrong.\n"+
			"Looked at:\n  %s\n"+
			"If the Simulator keeps its data somewhere else, set %s to the directory "+
			"that contains the per-game folders.\n"+
			"This is expected if the game has no harness installed yet: run setup, then relaunch.",
		bundleID, strings.Join(tried, "\n  "), EnvVarDataRoot)
}
