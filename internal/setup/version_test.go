package setup

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	opencrank "github.com/NickSpaghetti/open-crank-mcp"
	"github.com/NickSpaghetti/open-crank-mcp/internal/harness"
)

// The guard that keeps the whole drift-detection mechanism honest.
//
// If a harness source ever loses its version placeholder - renamed, refactored
// away, lost in a merge - then every copy setup writes is unidentifiable, the
// drift check silently answers "fine" forever, and the next breaking harness
// change lands exactly as quietly as the one that prompted building this. That is
// worth a test against the *real* embedded sources rather than a stand-in, because
// the stand-ins are the one thing that cannot regress here.
func TestEmbeddedHarnessSourcesCarryExactlyOnePlaceholder(t *testing.T) {
	for _, name := range []string{harness.LuaSourcePath, harness.CHeaderPath} {
		b, err := readEmbedded(name)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		if got := bytes.Count(b, []byte(harness.VersionPlaceholder)); got != 1 {
			t.Errorf("%s contains %d copies of %s, want exactly 1",
				name, got, harness.VersionPlaceholder)
		}
	}
}

// The C source deliberately has no placeholder - the pair's stamp lives in the
// header. Asserted so that adding one there (and thereby stamping the same
// fingerprint into two files that must agree) is a deliberate act.
func TestEmbeddedCSourceCarriesNoPlaceholder(t *testing.T) {
	b, err := readEmbedded(harness.CSourcePath)
	if err != nil {
		t.Fatalf("reading %s: %v", harness.CSourcePath, err)
	}
	if bytes.Contains(b, []byte(harness.VersionPlaceholder)) {
		t.Errorf("%s carries a version placeholder; only the header should", harness.CSourcePath)
	}
}

func readEmbedded(name string) ([]byte, error) {
	f, err := opencrank.HarnessFS.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(f); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// A stamped copy must carry a real fingerprint and no leftover placeholder -
// otherwise the server would compare against a literal and report every game as
// drifted.
func TestCopyHarnessFileStampsTheFingerprint(t *testing.T) {
	dst := t.TempDir() + "/mcp_harness.lua"
	if err := CopyHarnessFile(opencrank.HarnessFS, harness.LuaSourcePath, dst); err != nil {
		t.Fatalf("CopyHarnessFile: %v", err)
	}

	written := readFileString(t, dst)
	if strings.Contains(written, harness.VersionPlaceholder) {
		t.Fatal("the written copy still contains the placeholder, so it was never stamped")
	}
	want, err := harness.LuaFingerprint(opencrank.HarnessFS)
	if err != nil {
		t.Fatalf("LuaFingerprint: %v", err)
	}
	if !strings.Contains(written, want) {
		t.Fatalf("the written copy does not contain the fingerprint %q", want)
	}
}

// The C header is stamped with the fingerprint of the header *and* source
// together, so editing either invalidates a game's copy.
func TestCopyHarnessFileStampsCHeaderWithThePairFingerprint(t *testing.T) {
	dir := t.TempDir()
	if err := CopyHarnessFile(opencrank.HarnessFS, harness.CHeaderPath, dir+"/mcp_harness.h"); err != nil {
		t.Fatalf("CopyHarnessFile(header): %v", err)
	}
	if err := CopyHarnessFile(opencrank.HarnessFS, harness.CSourcePath, dir+"/mcp_harness.c"); err != nil {
		t.Fatalf("CopyHarnessFile(source): %v", err)
	}

	want, err := harness.CFingerprint(opencrank.HarnessFS)
	if err != nil {
		t.Fatalf("CFingerprint: %v", err)
	}
	if header := readFileString(t, dir+"/mcp_harness.h"); !strings.Contains(header, want) {
		t.Fatalf("the written header does not contain the pair fingerprint %q", want)
	}
	// The .c file is copied through unchanged; it has nothing to stamp.
	if source := readFileString(t, dir+"/mcp_harness.c"); strings.Contains(source, harness.VersionPlaceholder) {
		t.Fatal("mcp_harness.c came out containing a placeholder")
	}
}

// A source that should carry a placeholder but does not must fail loudly rather
// than write an unidentifiable copy. This is the failure mode the mechanism has,
// so it gets its own test rather than relying on the embedded-source guard above.
func TestCopyHarnessFileRefusesASourceWithNoPlaceholder(t *testing.T) {
	fsys := fstest.MapFS{
		harness.LuaSourcePath: {Data: []byte("-- no placeholder here\n")},
	}
	err := CopyHarnessFile(fsys, harness.LuaSourcePath, t.TempDir()+"/mcp_harness.lua")
	if err == nil {
		t.Fatal("CopyHarnessFile succeeded on a source with no placeholder, want an error")
	}
	if !strings.Contains(err.Error(), harness.VersionPlaceholder) {
		t.Fatalf("error = %q, want it to name the missing placeholder", err)
	}
}

// Two placeholders is equally wrong: one of them would survive into the copy, and
// a game reporting a half-substituted version is worse than one reporting none.
func TestCopyHarnessFileRefusesTwoPlaceholders(t *testing.T) {
	fsys := fstest.MapFS{
		harness.LuaSourcePath: {Data: []byte(
			"local A = \"" + harness.VersionPlaceholder + "\"\nlocal B = \"" + harness.VersionPlaceholder + "\"\n")},
	}
	if err := CopyHarnessFile(fsys, harness.LuaSourcePath, t.TempDir()+"/mcp_harness.lua"); err == nil {
		t.Fatal("CopyHarnessFile succeeded with two placeholders, want an error")
	}
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(b)
}
