package opencrank

import (
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/NickSpaghetti/open-crank-mcp/internal/harness"
)

// A broken embed pattern still builds cleanly; this catches it before a user's
// first `setup` call. Each marker is the harness entry point a game calls.
func TestHarnessFSContainsRealHarnesses(t *testing.T) {
	for _, tc := range []struct {
		path   string
		marker string
	}{
		{"lua/mcp_harness.lua", "function mcp.update"},
		{"c-harness/mcp_harness.h", "void mcp_harness_update("},
		{"c-harness/mcp_harness.c", "void mcp_harness_update("},
	} {
		b, err := fs.ReadFile(HarnessFS, tc.path)
		if err != nil {
			t.Errorf("reading %s: %v", tc.path, err)
			continue
		}
		if !strings.Contains(string(b), tc.marker) {
			t.Errorf("%s does not contain %q, so the embed matched the wrong file or a stale one",
				tc.path, tc.marker)
		}
	}
}

// embeddedFiles is every path the binary is allowed to carry, exhaustively.
var embeddedFiles = []string{
	"c-harness/mcp_harness.c",
	"c-harness/mcp_harness.h",
	"lua/mcp_harness.lua",
}

// Licence guard: the Playdate SDK License bans redistribution, so the binary may
// embed only this repo's own code. Compares exact paths, since a count would pass
// if a harness were swapped for an SDK header.
func TestHarnessFSCarriesNothingExtra(t *testing.T) {
	var got []string
	err := fs.WalkDir(HarnessFS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			got = append(got, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking HarnessFS: %v", err)
	}

	want := slices.Clone(embeddedFiles)
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("HarnessFS carries:\n  %s\nwant exactly:\n  %s\n\n"+
			"Every embedded file ships inside published release binaries. Anything here "+
			"that is not this repo's own code would redistribute it - see the licence "+
			"guard in .github/workflows/release.yml and the SDK-redistribution reasoning "+
			"in README.md.",
			strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// internal/harness names these paths for fingerprinting; they must be the embedded ones.
func TestHarnessPathConstantsAreEmbedded(t *testing.T) {
	for _, p := range []string{harness.LuaSourcePath, harness.CHeaderPath, harness.CSourcePath} {
		if _, err := fs.Stat(HarnessFS, p); err != nil {
			t.Errorf("harness path %s is not in HarnessFS: %v", p, err)
		}
	}
}

// The Lua and C harnesses must agree on the no-expiry sentinel.
func TestNoExpirySentinelsMatch(t *testing.T) {
	for _, tc := range []struct{ path, decl string }{
		{harness.LuaSourcePath, "local NO_EXPIRY = -1\n"},
		{harness.CHeaderPath, "#define MCP_NO_EXPIRY (-1L)\n"},
	} {
		b, err := fs.ReadFile(HarnessFS, tc.path)
		if err != nil {
			t.Fatalf("reading %s: %v", tc.path, err)
		}
		if !strings.Contains(strings.ReplaceAll(string(b), "\r\n", "\n"), tc.decl) {
			t.Errorf("%s does not declare %q", tc.path, tc.decl)
		}
	}
}
