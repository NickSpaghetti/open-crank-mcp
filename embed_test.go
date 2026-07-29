package opencrank

import (
	"io/fs"
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

// The embed patterns name three files individually rather than globbing
// c-harness/*, so that the C test suite and the fixture game stay out of the
// binary. This is what notices if someone widens them to a glob.
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
	if len(got) != 3 {
		t.Errorf("HarnessFS carries %d files, want exactly 3: %v", len(got), got)
	}
}
