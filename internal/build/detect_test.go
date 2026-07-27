package build

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectProjectTypeC(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "CMakeLists.txt"), []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := DetectProjectType(dir)
	if err != nil {
		t.Fatalf("DetectProjectType: %v", err)
	}
	if got != ProjectTypeC {
		t.Fatalf("DetectProjectType() = %v, want ProjectTypeC", got)
	}
}

func TestDetectProjectTypeLua(t *testing.T) {
	dir := t.TempDir()
	sourceDir := filepath.Join(dir, "Source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "main.lua"), []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := DetectProjectType(dir)
	if err != nil {
		t.Fatalf("DetectProjectType: %v", err)
	}
	if got != ProjectTypeLua {
		t.Fatalf("DetectProjectType() = %v, want ProjectTypeLua", got)
	}
}

func TestDetectProjectTypeNeitherSignalIsAnError(t *testing.T) {
	dir := t.TempDir()

	if _, err := DetectProjectType(dir); err == nil {
		t.Fatal("DetectProjectType: expected an error for an empty directory, got nil")
	}
}

func TestDetectProjectTypePrefersCWhenBothSignalsPresent(t *testing.T) {
	dir := t.TempDir()
	sourceDir := filepath.Join(dir, "Source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "main.lua"), []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "CMakeLists.txt"), []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := DetectProjectType(dir)
	if err != nil {
		t.Fatalf("DetectProjectType: %v", err)
	}
	if got != ProjectTypeC {
		t.Fatalf("DetectProjectType() = %v, want ProjectTypeC", got)
	}
}

func TestLocatePDXFindsSingleFile(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, "game.pdx")
	if err := os.WriteFile(want, []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := locatePDX(dir)
	if err != nil {
		t.Fatalf("locatePDX: %v", err)
	}
	if got != want {
		t.Fatalf("locatePDX() = %q, want %q", got, want)
	}
}

func TestLocatePDXErrorsOnNoFiles(t *testing.T) {
	dir := t.TempDir()

	if _, err := locatePDX(dir); err == nil {
		t.Fatal("locatePDX: expected an error for a directory with no .pdx, got nil")
	}
}

func TestLocatePDXErrorsOnMultipleFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.pdx"), []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.pdx"), []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := locatePDX(dir); err == nil {
		t.Fatal("locatePDX: expected an error for a directory with multiple .pdx files, got nil")
	}
}
