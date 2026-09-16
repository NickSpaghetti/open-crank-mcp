package opencrank

import (
	"io/fs"
	"slices"
	"strings"
	"testing"
)

// Asserts the embed patterns actually matched, and matched the real harnesses
// rather than something empty or truncated. Without this a broken pattern is a
// clean build that fails at a user's first `setup` call, which is a long way
// from the mistake.
//
// The markers are each harness's own entry point, the one symbol a game is
// wired to call. A file that does not contain it is not a harness.
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

// embeddedFiles is every path the binary is allowed to carry. Exhaustive, and
// the exhaustiveness is the point - see TestHarnessFSCarriesNothingExtra. Sorted
// by the test rather than by hand, so an entry added in the wrong place fails
// with a useful diff instead of two identical-looking lists in a different order.
var embeddedFiles = []string{
	"c-harness/mcp_harness.c",
	"c-harness/mcp_harness.h",
	"lua/mcp_harness.lua",
}

// The embed patterns name three files individually rather than globbing
// c-harness/*, so that the C test suite and the fixture game stay out of the
// binary. This is what notices if someone widens them to a glob.
//
// It compares the exact path set rather than counting, and that distinction is
// load-bearing rather than fastidious. This test is half of the licence guard:
// the Playdate SDK License bans redistributing the SDK, and published release
// binaries are licence-clean only because everything go:embed carries is this
// repo's own MIT code. A count of three passes just as happily if one harness
// source is swapped for an SDK header - same number of files, different
// contents, and a published artifact that redistributes Panic's work. The other
// half of the guard is in .github/workflows/release.yml, which asserts the
// uploaded artifact list; this half asserts what is inside the artifact.
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
