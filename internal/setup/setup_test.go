package setup

import (
	"os"
	"path/filepath"
	"testing"
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
