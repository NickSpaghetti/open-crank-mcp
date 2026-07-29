package sdk

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

// EnvVarDataRoot overrides the *parent* of the per-bundle data directory, so a
// user whose Simulator sandboxes somewhere none of the candidates name can point
// at it directly rather than wait for a fix.
const EnvVarDataRoot = "OPEN_CRANK_DATA_ROOT"

// harnessDir is the directory the harness creates inside the sandboxed data
// directory. Its existence is what makes the right data directory recognisable
// rather than merely predicted, which is the whole idea below.
const harnessDir = "mcp"

// searchDepth bounds the last-resort walk. Two levels below a search root is
// enough to find <root>/<bundleID>/mcp or <root>/Data/<bundleID>/mcp without
// walking a home directory.
const searchDepth = 3

// searchNodeBudget bounds it again, by node count, so a search root that turns
// out to be enormous degrades into "gave up" rather than "hung".
const searchNodeBudget = 4000

// DataDirCandidates lists the plausible per-bundle data directories for this
// platform, most likely first. Exported so a caller can report what was
// considered.
func (p Paths) DataDirCandidates(env Env, bundleID string) []string {
	if root := env.Getenv(EnvVarDataRoot); root != "" {
		return []string{filepath.Join(root, bundleID)}
	}
	return p.layout().dataDirCandidates(env, p.Root, bundleID)
}

// FindDataDir returns the sandboxed data directory for bundleID.
//
// This is deliberately not a prediction. The Simulator's data directory is
// *observable* once a harnessed game has run, because the harness creates an
// mcp/ directory inside it, so this looks for that instead of trusting a
// hardcoded layout. That matters because the failure mode of guessing wrong is
// the worst kind: launching succeeds, the Simulator runs, and then every tool
// that talks to the harness times out five seconds at a time with nothing
// pointing at the cause.
//
// Three strategies, cheapest first:
//
//  1. OPEN_CRANK_DATA_ROOT, if set. No probing, no argument.
//  2. Each platform candidate that already contains mcp/.
//  3. A bounded walk of the platform's search roots. This is what makes a wrong
//     candidate list degrade to "slower but correct" instead of "broken", which
//     is the difference that lets the macOS paths ship unverified at all.
//
// found reports whether the harness directory was actually seen. When it is
// false, dir is the first candidate as a fallback and every path considered is
// in tried: the caller is expected to say so rather than fail, because launching
// a game that has no harness installed yet is legitimate.
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
// contains mcp/. Bounded by both depth and node count; see the consts above.
func searchForBundle(env Env, root, bundleID string) (string, bool) {
	rootKey := fsKey(root)
	if !isDir(env, root) {
		return "", false
	}

	budget := searchNodeBudget
	var hit string
	// The error from WalkDir is deliberately ignored: an unreadable subtree is a
	// reason to keep looking elsewhere, not to fail the whole lookup.
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
				hit = "/" + p
				return fs.SkipAll
			}
		}
		return nil
	})
	if hit == "" {
		return "", false
	}
	return filepath.FromSlash(hit), true
}

// DataDirDiagnostic is the message to hand back when the data directory could
// not be confirmed. Written as a tool-visible warning rather than an error: the
// agent gets the diagnosis on its first call instead of discovering it through
// a series of five-second timeouts.
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
