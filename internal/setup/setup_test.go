package setup

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/NickSpaghetti/open-crank-mcp/internal/harness"
)

func TestDetectLanguageLua(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "Source", "main.lua"), "function playdate.update()\nend\n")

	got, err := DetectLanguage(dir)
	if err != nil {
		t.Fatalf("DetectLanguage: %v", err)
	}
	if got != Lua {
		t.Fatalf("DetectLanguage() = %v, want Lua", got)
	}
}

// TestDetectLanguageEmptyMainLuaDoesNotCountAsLua reproduces a real bug
// found testing against the Playdate SDK's own bundled "Sprite Game"
// example: a pure C project that ships a required-but-blank
// Source/main.lua stub (0 bytes) alongside a real CMakeLists.txt.
// Treating the stub's mere existence as a Lua signal misdetected it as
// hybrid and skipped installing the C harness entirely.
func TestDetectLanguageEmptyMainLuaDoesNotCountAsLua(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "Source", "main.lua"), "")
	mustWrite(t, filepath.Join(dir, "CMakeLists.txt"), "")

	got, err := DetectLanguage(dir)
	if err != nil {
		t.Fatalf("DetectLanguage: %v", err)
	}
	if got != C {
		t.Fatalf("DetectLanguage() = %v, want C (an empty main.lua stub shouldn't count as a real Lua signal)", got)
	}
}

func TestDetectLanguageC(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "CMakeLists.txt"), "")

	got, err := DetectLanguage(dir)
	if err != nil {
		t.Fatalf("DetectLanguage: %v", err)
	}
	if got != C {
		t.Fatalf("DetectLanguage() = %v, want C", got)
	}
}

func TestDetectLanguageHybrid(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "Source", "main.lua"), "function playdate.update()\nend\n")
	mustWrite(t, filepath.Join(dir, "CMakeLists.txt"), "")

	got, err := DetectLanguage(dir)
	if err != nil {
		t.Fatalf("DetectLanguage: %v", err)
	}
	if got != Hybrid {
		t.Fatalf("DetectLanguage() = %v, want Hybrid", got)
	}
}

func TestDetectLanguageNeitherIsAnError(t *testing.T) {
	dir := t.TempDir()
	if _, err := DetectLanguage(dir); err == nil {
		t.Fatal("DetectLanguage: expected an error for an empty directory, got nil")
	}
}

// Same reason TestValidButton and TestValidDockMode exist in internal/harness: this is
// called from internal/tools, so without a test in its own package mutation testing
// reports its branches as uncovered. The empty string is real behaviour rather than an
// oversight - it is how a caller asks for auto-detection, which is a different question
// from naming an invalid language.
func TestValidLanguage(t *testing.T) {
	for _, l := range Languages {
		if !ValidLanguage(l) {
			t.Errorf("ValidLanguage(%q) = false, want true", l)
		}
	}
	for _, l := range []Language{"", "LUA", "Lua", "python", "c++"} {
		if ValidLanguage(l) {
			t.Errorf("ValidLanguage(%q) = true, want false", l)
		}
	}
}

// LanguageNames feeds both an error message and the schema enums the tools declare, so a
// mismatch with Languages would publish a closed set that disagrees with the one actually
// enforced.
func TestLanguageNamesMatchLanguages(t *testing.T) {
	names := LanguageNames()
	if len(names) != len(Languages) {
		t.Fatalf("LanguageNames() has %d entries, Languages has %d", len(names), len(Languages))
	}
	for i, l := range Languages {
		if names[i] != string(l) {
			t.Errorf("LanguageNames()[%d] = %q, want %q", i, names[i], l)
		}
	}
}

// TestSetupRefusesADirectoryThatIsNotAProject is the regression test for setup writing
// into a path that was never a game.
//
// Only the auto-detect path checked, because it calls DetectLanguage anyway. With an
// explicit language nothing looked, and setupLua creates Source/ on its way to writing
// the harness - so setup(source_dir: "QBSRK", language: "lua") created
// QBSRK/Source/mcp_harness.lua relative to the process's working directory, then failed
// on the main.lua it could not patch and left the tree behind.
//
// Found by Specmatic's MCP auto-test, which generates a random string when a schema says
// `type: string`: four such directories appeared in the repo root before anyone noticed
// them. That is what an unchecked filesystem write looks like from the outside.
//
// The assertion that matters is the second one. An error alone was always returned; what
// was missing is that nothing should be created on the way to it.
func TestSetupRefusesADirectoryThatIsNotAProject(t *testing.T) {
	for _, language := range Languages {
		parent := t.TempDir()
		target := filepath.Join(parent, "NOTAPROJECT")

		if _, err := Setup(target, language, testHarnessFS()); err == nil {
			t.Errorf("Setup(%q, %v) succeeded on a path that does not exist", target, language)
		}

		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Errorf("Setup(%q, %v) created the directory anyway (stat err: %v); a failed setup "+
				"must not leave a harness tree behind in a path that was never a game",
				target, language, err)
		}
	}
}

// An existing directory with nothing project-shaped in it is refused too, not just a
// nonexistent one. This is the case that matters on a real machine: a mistyped but real
// path - a home directory, a repo root - would otherwise get a Source/mcp_harness.lua
// written into it.
func TestSetupRefusesAnUnrelatedDirectory(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "notes.txt"), "not a game\n")

	if _, err := Setup(dir, Lua, testHarnessFS()); err == nil {
		t.Fatalf("Setup(%q, Lua) succeeded on a directory with no project in it", dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "Source")); !os.IsNotExist(err) {
		t.Error("Setup created Source/ in a directory that is not a game")
	}
}

// An explicit language may still disagree with what detection would have answered - that
// is the whole point of being able to pass one. A hybrid project asked to set up as Lua
// installs the Lua harness, and the new project check must not turn that into an error.
func TestSetupExplicitLanguageMayDisagreeWithDetection(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "Source", "main.lua"), "function playdate.update()\nend\n")
	mustWrite(t, filepath.Join(dir, "CMakeLists.txt"), "")

	detected, err := DetectLanguage(dir)
	if err != nil {
		t.Fatalf("DetectLanguage: %v", err)
	}
	if detected != Hybrid {
		t.Fatalf("fixture detected as %v, want Hybrid; this test is about overriding detection", detected)
	}

	result, err := Setup(dir, Lua, testHarnessFS())
	if err != nil {
		t.Fatalf("Setup(dir, Lua) on a hybrid project: %v", err)
	}
	if result.Language != Lua {
		t.Errorf("Setup reported language %v, want the requested Lua", result.Language)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	return string(b)
}

// testHarnessFS stands in for opencrank.HarnessFS.
//
// A MapFS rather than the real embedded sources, so these tests exercise the
// same code path without depending on the harness content. Nothing here asserts
// what is inside a copied harness, only that the right path was written and that
// the *game's* own files were patched correctly, which is the part with logic in
// it. The keys must match the paths internal/setup asks for.
//
// The two stampable sources carry the version placeholder, because a real one
// always does and CopyHarnessFile refuses a source without it. mcp_harness.c has
// none on purpose - the C pair's stamp lives in the header.
func testHarnessFS() fs.FS {
	return fstest.MapFS{
		"lua/mcp_harness.lua":     {Data: []byte("-- test harness stand-in\nlocal V = \"" + harness.VersionPlaceholder + "\"\n")},
		"c-harness/mcp_harness.h": {Data: []byte("/* test harness stand-in */\n#define V \"" + harness.VersionPlaceholder + "\"\n")},
		"c-harness/mcp_harness.c": {Data: []byte("/* test harness stand-in */\n")},
	}
}
