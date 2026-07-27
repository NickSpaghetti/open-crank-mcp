package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestSetupHarnessLua(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Source", "main.lua"), "function playdate.update()\nend\n")

	s := &Server{}
	result, out, err := s.setupHarness(context.Background(), nil, SetupInput{SourceDir: dir})
	if err != nil {
		t.Fatalf("setupHarness: %v", err)
	}
	if result != nil {
		t.Fatalf("setupHarness() result = %v, want nil (success)", result)
	}
	if out.Language != "lua" {
		t.Fatalf("setupHarness().Language = %q, want %q", out.Language, "lua")
	}
	if len(out.FilesCopied) != 1 {
		t.Fatalf("setupHarness().FilesCopied = %v, want exactly 1", out.FilesCopied)
	}
}

func TestSetupHarnessUnknownLanguageIsAToolError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Source", "main.lua"), "")

	s := &Server{}
	result, _, err := s.setupHarness(context.Background(), nil, SetupInput{SourceDir: dir, Language: "rust"})
	if err != nil {
		t.Fatalf("setupHarness: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("setupHarness() result = %v, want an IsError result for an unknown language", result)
	}
}

func TestSetupHarnessNoProjectFoundIsAToolError(t *testing.T) {
	dir := t.TempDir()

	s := &Server{}
	result, _, err := s.setupHarness(context.Background(), nil, SetupInput{SourceDir: dir})
	if err != nil {
		t.Fatalf("setupHarness: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("setupHarness() result = %v, want an IsError result when no project is detected", result)
	}
}

func TestSetupThenTeardownHarnessLua(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "Source", "main.lua")
	writeFile(t, mainPath, "function playdate.update()\nend\n")

	s := &Server{}
	if result, _, err := s.setupHarness(context.Background(), nil, SetupInput{SourceDir: dir}); err != nil || result != nil {
		t.Fatalf("setupHarness: result=%v err=%v", result, err)
	}

	result, out, err := s.teardownHarness(context.Background(), nil, TeardownInput{SourceDir: dir})
	if err != nil {
		t.Fatalf("teardownHarness: %v", err)
	}
	if result != nil {
		t.Fatalf("teardownHarness() result = %v, want nil (success)", result)
	}
	if len(out.FilesRemoved) != 1 {
		t.Fatalf("teardownHarness().FilesRemoved = %v, want exactly 1", out.FilesRemoved)
	}
	if _, err := os.ReadFile(mainPath); err != nil {
		t.Fatalf("main.lua should still exist after teardown: %v", err)
	}
}

func TestResolveLanguageExplicitOverride(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "CMakeLists.txt"), "")
	writeFile(t, filepath.Join(dir, "Source", "main.lua"), "")

	got, err := resolveLanguage(dir, "c")
	if err != nil {
		t.Fatalf("resolveLanguage: %v", err)
	}
	if got != "c" {
		t.Fatalf("resolveLanguage() = %q, want %q (override should win over auto-detected hybrid)", got, "c")
	}
}
