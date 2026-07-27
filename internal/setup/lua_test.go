package setup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPatchLuaMainAddsMarkerBlock(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.lua")
	mustWrite(t, mainPath, "function playdate.update()\nend\n")

	changed, err := patchLuaMain(mainPath)
	if err != nil {
		t.Fatalf("patchLuaMain: %v", err)
	}
	if !changed {
		t.Fatal("patchLuaMain() changed = false, want true")
	}
	content := mustRead(t, mainPath)
	if !hasMarkerBlock(content, luaMarkerBegin) {
		t.Fatalf("main.lua doesn't contain the marker block:\n%s", content)
	}
}

func TestPatchLuaMainIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.lua")
	mustWrite(t, mainPath, "function playdate.update()\nend\n")

	if _, err := patchLuaMain(mainPath); err != nil {
		t.Fatalf("first patchLuaMain: %v", err)
	}
	firstContent := mustRead(t, mainPath)

	changed, err := patchLuaMain(mainPath)
	if err != nil {
		t.Fatalf("second patchLuaMain: %v", err)
	}
	if changed {
		t.Fatal("patchLuaMain() second call changed = true, want false (already patched)")
	}
	if mustRead(t, mainPath) != firstContent {
		t.Fatal("second patchLuaMain() modified an already-patched file")
	}
}

func TestSetupAndTeardownLuaRoundTrip(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "Source", "main.lua")
	mustWrite(t, mainPath, "function playdate.update()\nend\n")

	result, err := Setup(dir, Lua)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if len(result.FilesCopied) != 1 {
		t.Fatalf("Setup().FilesCopied = %v, want exactly 1 (mcp_harness.lua)", result.FilesCopied)
	}
	harnessPath := filepath.Join(dir, "Source", "mcp_harness.lua")
	if !fileExists(harnessPath) {
		t.Fatalf("mcp_harness.lua was not copied to %s", harnessPath)
	}
	if !hasMarkerBlock(mustRead(t, mainPath), luaMarkerBegin) {
		t.Fatal("main.lua wasn't patched with the marker block")
	}

	teardownResult, err := Teardown(dir, Lua)
	if err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	if len(teardownResult.FilesRemoved) != 1 || teardownResult.FilesRemoved[0] != harnessPath {
		t.Fatalf("Teardown().FilesRemoved = %v, want [%s]", teardownResult.FilesRemoved, harnessPath)
	}
	if fileExists(harnessPath) {
		t.Fatal("mcp_harness.lua still exists after teardown")
	}
	if hasMarkerBlock(mustRead(t, mainPath), luaMarkerBegin) {
		t.Fatal("main.lua still has the marker block after teardown")
	}
}

// TestTeardownLuaPreservesHandWrittenImport reproduces a real bug found
// testing against missile-command (a project hand-wired before this tool
// existed): its main.lua has a plain, unmarked `import "mcp_harness"`.
// Teardown must not delete the vendored harness file in that case - doing
// so would leave the import pointing at a file that no longer exists,
// breaking the next build.
func TestTeardownLuaPreservesHandWrittenImport(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "Source", "main.lua")
	mustWrite(t, mainPath, "import \"mcp_harness\"\n\nfunction playdate.update()\nend\n")
	harnessPath := filepath.Join(dir, "Source", "mcp_harness.lua")
	mustWrite(t, harnessPath, "-- pretend harness content\n")

	result, err := Teardown(dir, Lua)
	if err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	if len(result.FilesRemoved) != 0 {
		t.Fatalf("Teardown().FilesRemoved = %v, want empty - main.lua still imports it", result.FilesRemoved)
	}
	if !fileExists(harnessPath) {
		t.Fatal("Teardown removed mcp_harness.lua even though main.lua still imports it")
	}
}

func TestTeardownLuaIsNoOpWhenNeverSetUp(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "Source", "main.lua")
	mustWrite(t, mainPath, "function playdate.update()\nend\n")

	result, err := Teardown(dir, Lua)
	if err != nil {
		t.Fatalf("Teardown on a never-set-up project: %v", err)
	}
	if len(result.FilesRemoved) != 0 {
		t.Fatalf("Teardown().FilesRemoved = %v, want empty", result.FilesRemoved)
	}
	for _, fc := range result.FilesPatched {
		if fc.Changed {
			t.Fatalf("Teardown() reported a change to %s on a project that was never set up", fc.Path)
		}
	}
}

func TestSetupHybridOnlyTouchesLuaFiles(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "Source", "main.lua")
	mustWrite(t, mainPath, "function playdate.update()\nend\n")
	cmakePath := filepath.Join(dir, "CMakeLists.txt")
	cmakeContent := "add_library(NAME SHARED src/main.c)\n"
	mustWrite(t, cmakePath, cmakeContent)

	result, err := Setup(dir, Hybrid)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if result.Language != Hybrid {
		t.Fatalf("Setup(Hybrid).Language = %v, want Hybrid (dispatches to the Lua-only steps, but should still report what it was actually invoked for)", result.Language)
	}
	if mustRead(t, cmakePath) != cmakeContent {
		t.Fatal("Setup(Hybrid) modified CMakeLists.txt - it should only touch the Lua side")
	}
	if fileExists(filepath.Join(dir, "src", "mcp_harness.c")) {
		t.Fatal("Setup(Hybrid) copied C harness files - it should only touch the Lua side")
	}
}

func TestSetupLuaMissingMainLuaIsAnError(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "Source"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if _, err := Setup(dir, Lua); err == nil {
		t.Fatal("Setup: expected an error when Source/main.lua doesn't exist, got nil")
	}
}
