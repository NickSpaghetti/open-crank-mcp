package tools

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/NickSpaghetti/open-crank-mcp/internal/harness"
)

// testHarnessFS stands in for opencrank.HarnessFS. The keys are the paths
// internal/setup looks up; the contents are irrelevant to what these tests
// assert, which is the tool's own wiring and reporting - except that the two
// stampable sources must carry the version placeholder, since CopyHarnessFile
// refuses a source without one.
func testHarnessFS() fs.FS {
	return fstest.MapFS{
		"lua/mcp_harness.lua":     {Data: []byte("-- test harness stand-in\nlocal V = \"" + harness.VersionPlaceholder + "\"\n")},
		"c-harness/mcp_harness.h": {Data: []byte("/* test harness stand-in */\n#define V \"" + harness.VersionPlaceholder + "\"\n")},
		"c-harness/mcp_harness.c": {Data: []byte("/* test harness stand-in */\n")},
	}
}

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

	s := &Server{harnessFS: testHarnessFS()}
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

	s := &Server{harnessFS: testHarnessFS()}
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

	s := &Server{harnessFS: testHarnessFS()}
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

	s := &Server{harnessFS: testHarnessFS()}
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
