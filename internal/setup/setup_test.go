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

// The SDK's "Sprite Game" is pure C but ships an empty Source/main.lua stub.
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

// "" is rejected here: it means auto-detect, which callers handle separately.
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

// The point is the second assertion: nothing may be created on the way to the error.
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

// testHarnessFS stands in for opencrank.HarnessFS. Stampable sources carry the
// placeholder because CopyHarnessFile refuses one without it.
func testHarnessFS() fs.FS {
	return fstest.MapFS{
		"lua/mcp_harness.lua":     {Data: []byte("-- test harness stand-in\nlocal V = \"" + harness.VersionPlaceholder + "\"\n")},
		"c-harness/mcp_harness.h": {Data: []byte("/* test harness stand-in */\n#define V \"" + harness.VersionPlaceholder + "\"\n")},
		"c-harness/mcp_harness.c": {Data: []byte("/* test harness stand-in */\n")},
	}
}
