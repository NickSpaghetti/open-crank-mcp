package setup

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestPatchInputCallsReplacesDirectSDKCalls reproduces a real gap found
// testing against the Playdate SDK's own bundled "Sprite Game" example:
// setupC wired in mcp_harness_init/update correctly, but the game's own
// input-reading code called pd->system->getButtonState directly (as any
// real, pre-existing C game does) - meaning press_button silently did
// nothing, since pd->system is write-protected in memory and only the
// mcp_get_* wrapper functions see the override.
func TestPatchInputCallsReplacesDirectSDKCalls(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "src", "game.c")
	mustWrite(t, path, `static void updatePlayer(void) {
	PDButtons current;
	pd->system->getButtonState(&current, NULL, NULL);
}

void checkButtons(void) {
	PDButtons pushed;
	pd->system->getButtonState(NULL, &pushed, NULL);
}

void checkCrank(void) {
	float change = pd->system->getCrankChange();
	float angle = pd->system->getCrankAngle();
	int docked = pd->system->isCrankDocked();
}
`)

	changes, err := patchInputCalls(dir)
	if err != nil {
		t.Fatalf("patchInputCalls: %v", err)
	}
	if len(changes) != 1 || !changes[0].Changed {
		t.Fatalf("patchInputCalls() = %v, want exactly one Changed=true entry", changes)
	}
	content := mustRead(t, path)
	for _, want := range []string{
		"mcp_get_button_state(pd, &current, NULL, NULL)",
		"mcp_get_button_state(pd, NULL, &pushed, NULL)",
		"mcp_get_crank_change(pd)",
		"mcp_get_crank_angle(pd)",
		"mcp_get_crank_docked(pd)",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("patched content missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "->system->") {
		t.Errorf("patched content still has a direct ->system-> call:\n%s", content)
	}
}

func TestPatchInputCallsIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "src", "game.c")
	mustWrite(t, path, "void f(void) { pd->system->getButtonState(&c, NULL, NULL); }\n")

	if _, err := patchInputCalls(dir); err != nil {
		t.Fatalf("first patchInputCalls: %v", err)
	}
	firstContent := mustRead(t, path)

	changes, err := patchInputCalls(dir)
	if err != nil {
		t.Fatalf("second patchInputCalls: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("patchInputCalls() second call = %v, want no changes (already converted)", changes)
	}
	if mustRead(t, path) != firstContent {
		t.Fatal("second patchInputCalls() modified an already-converted file")
	}
}

// TestPatchInputCallsAddsMissingInclude reproduces the second half of the
// real gap found testing against the SDK's own "Sprite Game" example:
// game.c (a file other than the one setupC designates as "the"
// eventHandler file) got its direct SDK calls rewritten to mcp_get_*, but
// never got #include "mcp_harness.h" added - it only "compiled" as an
// implicit-declaration warning, since nothing else in the project already
// included it.
func TestPatchInputCallsAddsMissingInclude(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "game.c")
	mustWrite(t, path, `#include "game.h"

void checkButtons(void) {
	PDButtons pushed;
	pd->system->getButtonState(NULL, &pushed, NULL);
}
`)

	changes, err := patchInputCalls(dir)
	if err != nil {
		t.Fatalf("patchInputCalls: %v", err)
	}
	if len(changes) != 1 || !changes[0].Changed {
		t.Fatalf("patchInputCalls() = %v, want exactly one Changed=true entry", changes)
	}
	content := mustRead(t, path)
	if !strings.Contains(content, `#include "mcp_harness.h"`) {
		t.Fatalf("patched content missing #include \"mcp_harness.h\":\n%s", content)
	}
	if !strings.Contains(content, "mcp_get_button_state(pd, NULL, &pushed, NULL)") {
		t.Fatalf("patched content missing the rewritten call:\n%s", content)
	}
}

// TestPatchInputCallsDoesNotDuplicateExistingInclude covers a file that
// already has the literal include (missile-command's aim.c, hand-wired
// before this tool existed) - patchInputCalls shouldn't add a second one.
func TestPatchInputCallsDoesNotDuplicateExistingInclude(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aim.c")
	mustWrite(t, path, `#include "mcp_harness.h"

void checkButtons(void) {
	PDButtons pushed;
	pd->system->getButtonState(NULL, &pushed, NULL);
}
`)

	if _, err := patchInputCalls(dir); err != nil {
		t.Fatalf("patchInputCalls: %v", err)
	}
	content := mustRead(t, path)
	if strings.Count(content, `#include "mcp_harness.h"`) != 1 {
		t.Fatalf("expected exactly one #include \"mcp_harness.h\", got:\n%s", content)
	}
}

func TestPatchCMakeListsAddsIncludeDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CMakeLists.txt")
	mustWrite(t, path, "cmake_minimum_required(VERSION 3.14)\n"+
		"project(Game C ASM)\n\n"+
		"add_library(${NAME} SHARED main.c)\n")

	changed, err := patchCMakeLists(path)
	if err != nil {
		t.Fatalf("patchCMakeLists: %v", err)
	}
	if !changed {
		t.Fatal("patchCMakeLists() changed = false, want true")
	}
	content := mustRead(t, path)
	if !strings.Contains(content, "include_directories(src)") {
		t.Fatalf("expected include_directories(src) to be inserted:\n%s", content)
	}
	if !strings.Contains(content, cmakeMarkerBegin) || !strings.Contains(content, cmakeMarkerEnd) {
		t.Fatalf("expected the include_directories(src) insertion to be marker-wrapped:\n%s", content)
	}
	if strings.Contains(content, cMarkerBegin) {
		t.Fatalf("include_directories(src) was wrapped in C-style markers, not valid CMake syntax:\n%s", content)
	}
	// Inserted after project(...), before the add_library call.
	projectIdx := strings.Index(content, "project(")
	includeIdx := strings.Index(content, "include_directories(src)")
	addLibIdx := strings.Index(content, "add_library(")
	if !(projectIdx < includeIdx && includeIdx < addLibIdx) {
		t.Fatalf("expected project(...) < include_directories(src) < add_library(...):\n%s", content)
	}
}

func TestPatchCMakeListsIncludeDirectoriesIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CMakeLists.txt")
	mustWrite(t, path, "project(Game C ASM)\n\nadd_library(${NAME} SHARED main.c)\n")

	if _, err := patchCMakeLists(path); err != nil {
		t.Fatalf("first patchCMakeLists: %v", err)
	}
	firstContent := mustRead(t, path)

	changed, err := patchCMakeLists(path)
	if err != nil {
		t.Fatalf("second patchCMakeLists: %v", err)
	}
	if changed {
		t.Fatal("patchCMakeLists() second call changed = true, want false (already patched)")
	}
	if mustRead(t, path) != firstContent {
		t.Fatal("second patchCMakeLists() modified an already-patched file")
	}
}

func TestTeardownCStripsIncludeDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CMakeLists.txt")
	mustWrite(t, path, "project(Game C ASM)\n\nadd_library(${NAME} SHARED main.c)\n")

	changed, err := teardownCMakeLists(path)
	if err != nil {
		t.Fatalf("teardownCMakeLists: %v", err)
	}
	if changed {
		t.Fatal("teardownCMakeLists() changed = true on a file with neither src/mcp_harness.c nor " +
			"an include_directories(src) block, want false")
	}

	// Now with the block actually present, as patchCMakeLists would leave it.
	if _, err := patchCMakeLists(path); err != nil {
		t.Fatalf("patchCMakeLists: %v", err)
	}
	if !strings.Contains(mustRead(t, path), "include_directories(src)") {
		t.Fatal("expected patchCMakeLists to have inserted include_directories(src)")
	}

	changed, err = teardownCMakeLists(path)
	if err != nil {
		t.Fatalf("teardownCMakeLists: %v", err)
	}
	if !changed {
		t.Fatal("teardownCMakeLists() changed = false, want true")
	}
	content := mustRead(t, path)
	if strings.Contains(content, "include_directories(src)") || strings.Contains(content, cmakeMarkerBegin) {
		t.Fatalf("teardownCMakeLists left the include_directories(src) block in place:\n%s", content)
	}
}

// TestSetupAndTeardownCFlatLayoutRoundTrip reproduces the SDK's own
// "Sprite Game" example's actual shape: no src/ directory at all - the
// eventHandler file and a second file with direct input calls both live
// at the project root. Without patchCMakeIncludeDirectories and
// patchInputCalls' own #include insertion, a bare #include
// "mcp_harness.h" (always copied into sourceDir/src/) can't resolve from
// either file, and game.c's rewritten calls have no declaration in scope
// at all.
func TestSetupAndTeardownCFlatLayoutRoundTrip(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "CMakeLists.txt"),
		"cmake_minimum_required(VERSION 3.14)\n"+
			"project(SpriteGame C ASM)\n\n"+
			"add_library(${NAME} SHARED main.c game.c)\n")
	mustWrite(t, filepath.Join(dir, "main.c"), fixtureStyleMain)
	mustWrite(t, filepath.Join(dir, "game.c"), `#include "game.h"

void checkButtons(void) {
	PDButtons pushed;
	pd->system->getButtonState(NULL, &pushed, NULL);
}
`)

	result, err := Setup(dir, C)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("Setup().ManualSteps = %v, want empty for this fixture shape", result.ManualSteps)
	}
	if !fileExists(filepath.Join(dir, "src", "mcp_harness.h")) || !fileExists(filepath.Join(dir, "src", "mcp_harness.c")) {
		t.Fatal("mcp_harness.h/.c were not copied to src/")
	}

	cmakeContent := mustRead(t, filepath.Join(dir, "CMakeLists.txt"))
	if !strings.Contains(cmakeContent, "src/mcp_harness.c") {
		t.Fatalf("CMakeLists.txt source list wasn't patched:\n%s", cmakeContent)
	}
	if !strings.Contains(cmakeContent, "include_directories(src)") {
		t.Fatalf("CMakeLists.txt missing include_directories(src) - main.c/game.c at the project root "+
			"can't otherwise reach mcp_harness.h in src/:\n%s", cmakeContent)
	}

	gameContent := mustRead(t, filepath.Join(dir, "game.c"))
	if !strings.Contains(gameContent, `#include "mcp_harness.h"`) {
		t.Fatalf("game.c missing #include \"mcp_harness.h\" after its calls were rewritten:\n%s", gameContent)
	}
	if !strings.Contains(gameContent, "mcp_get_button_state(pd, NULL, &pushed, NULL)") {
		t.Fatalf("game.c's direct call wasn't rewritten:\n%s", gameContent)
	}

	teardownResult, err := Teardown(dir, C)
	if err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	// patchInputCalls' rewrite in game.c is never reversed (same
	// documented trade-off as TestSetupThenTeardownCWithInputCallsIsANoOp),
	// so teardown here is correctly a full no-op.
	if len(teardownResult.FilesRemoved) != 0 || len(teardownResult.FilesPatched) != 0 {
		t.Fatalf("Teardown() = %+v, want a full no-op - mcp_get_button_state() is still called in game.c", teardownResult)
	}
}

func TestPatchCMakeListsInlineStyle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CMakeLists.txt")
	mustWrite(t, path, "if (TOOLCHAIN STREQUAL \"armgcc\")\n"+
		"\tadd_executable(${DEVICE} src/main.c)\n"+
		"else()\n"+
		"\tadd_library(${NAME} SHARED src/main.c)\n"+
		"endif()\n")

	changed, err := patchCMakeLists(path)
	if err != nil {
		t.Fatalf("patchCMakeLists: %v", err)
	}
	if !changed {
		t.Fatal("patchCMakeLists() changed = false, want true")
	}
	content := mustRead(t, path)
	if strings.Count(content, "src/mcp_harness.c") != 2 {
		t.Fatalf("expected src/mcp_harness.c inserted into both add_executable and add_library calls:\n%s", content)
	}
}

func TestPatchCMakeListsVariableStyle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CMakeLists.txt")
	mustWrite(t, path, "set(GAME_SOURCES\n"+
		"\tsrc/main.c\n"+
		"\tsrc/entity.c\n"+
		")\n\n"+
		"add_library(${NAME} SHARED ${GAME_SOURCES})\n")

	changed, err := patchCMakeLists(path)
	if err != nil {
		t.Fatalf("patchCMakeLists: %v", err)
	}
	if !changed {
		t.Fatal("patchCMakeLists() changed = false, want true")
	}
	content := mustRead(t, path)
	if !strings.Contains(content, "src/mcp_harness.c") {
		t.Fatalf("expected src/mcp_harness.c inserted into the GAME_SOURCES block:\n%s", content)
	}
	// Inserted inside the set() block, not duplicated into the add_library call.
	if strings.Count(content, "src/mcp_harness.c") != 1 {
		t.Fatalf("expected exactly one src/mcp_harness.c reference:\n%s", content)
	}
}

func TestPatchCMakeListsIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CMakeLists.txt")
	mustWrite(t, path, "add_library(${NAME} SHARED src/main.c src/mcp_harness.c)\n")

	changed, err := patchCMakeLists(path)
	if err != nil {
		t.Fatalf("patchCMakeLists: %v", err)
	}
	if changed {
		t.Fatal("patchCMakeLists() changed = true, want false (already present)")
	}
}

func TestPatchCMakeListsNoMatchDoesNotGuess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CMakeLists.txt")
	original := "# a totally custom build file with no add_library/add_executable at all\n"
	mustWrite(t, path, original)

	changed, err := patchCMakeLists(path)
	if err != nil {
		t.Fatalf("patchCMakeLists: %v", err)
	}
	if changed {
		t.Fatal("patchCMakeLists() changed = true, want false (nothing safe to patch)")
	}
	if mustRead(t, path) != original {
		t.Fatal("patchCMakeLists() modified a file it couldn't confidently patch")
	}
}

// TestTeardownCPreservesHandWrittenReferences reproduces a real bug found
// testing against missile-command's C port (hand-wired before this tool
// existed): CMakeLists.txt and main.c reference mcp_harness.c/.h in plain,
// unmarked form. An earlier version of this fix preserved the files but
// still stripped CMakeLists.txt's reference (the only signal it can act
// on, since it has no markers) - that "compiled" but crashed at runtime,
// since shared libraries link with undefined symbols by default. Teardown
// must be a full no-op here: nothing removed, CMakeLists.txt untouched,
// main.c untouched.
func TestTeardownCPreservesHandWrittenReferences(t *testing.T) {
	dir := t.TempDir()
	cmakePath := filepath.Join(dir, "CMakeLists.txt")
	cmakeContent := "add_library(${NAME} SHARED src/main.c src/mcp_harness.c)\n"
	mustWrite(t, cmakePath, cmakeContent)
	headerPath := filepath.Join(dir, "src", "mcp_harness.h")
	cPath := filepath.Join(dir, "src", "mcp_harness.c")
	mustWrite(t, headerPath, "// pretend header\n")
	mustWrite(t, cPath, "// pretend impl\n")
	mainPath := filepath.Join(dir, "src", "main.c")
	mainContent := `#include "pd_api.h"
#include "mcp_harness.h"

static PlaydateAPI *pd;

int eventHandler(PlaydateAPI *playdate, PDSystemEvent event, uint32_t arg) {
    if (event == kEventInit) {
        pd = playdate;
    }
    return 0;
}
`
	mustWrite(t, mainPath, mainContent)

	result, err := Teardown(dir, C)
	if err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	if len(result.FilesRemoved) != 0 {
		t.Fatalf("Teardown().FilesRemoved = %v, want empty - both files are still referenced", result.FilesRemoved)
	}
	if len(result.FilesPatched) != 0 {
		t.Fatalf("Teardown().FilesPatched = %v, want empty - a hand-wired reference means a full no-op, "+
			"not a partial teardown (stripping CMakeLists.txt while keeping the files compiles but crashes "+
			"at runtime on the undefined symbols)", result.FilesPatched)
	}
	if !fileExists(headerPath) || !fileExists(cPath) {
		t.Fatal("Teardown removed a harness file that's still referenced")
	}
	if mustRead(t, cmakePath) != cmakeContent {
		t.Fatal("Teardown modified CMakeLists.txt even though it should be a full no-op")
	}
	if mustRead(t, mainPath) != mainContent {
		t.Fatal("Teardown modified main.c even though it should be a full no-op")
	}
}

func TestTeardownCMakeListsBothStyles(t *testing.T) {
	dir := t.TempDir()

	inlinePath := filepath.Join(dir, "inline.txt")
	mustWrite(t, inlinePath, "add_library(${NAME} SHARED src/main.c src/mcp_harness.c)\n")
	changed, err := teardownCMakeLists(inlinePath)
	if err != nil || !changed {
		t.Fatalf("teardownCMakeLists(inline) = (%v, %v), want (true, nil)", changed, err)
	}
	if strings.Contains(mustRead(t, inlinePath), "mcp_harness.c") {
		t.Fatal("teardownCMakeLists left mcp_harness.c in the inline-style file")
	}

	varPath := filepath.Join(dir, "var.txt")
	mustWrite(t, varPath, "set(GAME_SOURCES\n\tsrc/main.c\n\tsrc/mcp_harness.c\n)\n")
	changed, err = teardownCMakeLists(varPath)
	if err != nil || !changed {
		t.Fatalf("teardownCMakeLists(var) = (%v, %v), want (true, nil)", changed, err)
	}
	if strings.Contains(mustRead(t, varPath), "mcp_harness.c") {
		t.Fatal("teardownCMakeLists left mcp_harness.c in the variable-style file")
	}
}

const fixtureStyleMain = `#include "pd_api.h"

static PlaydateAPI *pd;

static int update(void *userdata) {
    return 1;
}

int eventHandler(PlaydateAPI *playdate, PDSystemEvent event, uint32_t arg) {
    if (event == kEventInit) {
        pd = playdate;
        pd->system->setUpdateCallback(update, NULL);
    }
    return 0;
}
`

func TestFindEventHandlerCapturesParamName(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "src", "main.c"), fixtureStyleMain)

	path, pdVar, err := findEventHandler(dir)
	if err != nil {
		t.Fatalf("findEventHandler: %v", err)
	}
	if pdVar != "playdate" {
		t.Fatalf("findEventHandler() pdVar = %q, want %q", pdVar, "playdate")
	}
	if filepath.Base(path) != "main.c" {
		t.Fatalf("findEventHandler() path = %q, want main.c", path)
	}
}

func TestFindEventHandlerNotFound(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "src", "main.c"), "int main(void) { return 0; }\n")

	if _, _, err := findEventHandler(dir); err == nil {
		t.Fatal("findEventHandler: expected an error when no eventHandler is defined, got nil")
	}
}

func TestPatchEventHandlerInitInsertsIncludeAndInit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.c")
	mustWrite(t, path, `#include "pd_api.h"

int eventHandler(PlaydateAPI *pd, PDSystemEvent event, uint32_t arg) {
    if (event == kEventInit) {
        doStuff();
    }
    return 0;
}
`)

	changed, err := patchEventHandlerInit(path, "pd")
	if err != nil {
		t.Fatalf("patchEventHandlerInit: %v", err)
	}
	if !changed {
		t.Fatal("patchEventHandlerInit() changed = false, want true")
	}
	content := mustRead(t, path)
	if !strings.Contains(content, `#include "mcp_harness.h"`) {
		t.Fatalf("missing #include:\n%s", content)
	}
	if !strings.Contains(content, "mcp_harness_init(pd);") {
		t.Fatalf("missing mcp_harness_init(pd) call:\n%s", content)
	}
}

func TestPatchEventHandlerInitIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.c")
	mustWrite(t, path, `#include "pd_api.h"

int eventHandler(PlaydateAPI *pd, PDSystemEvent event, uint32_t arg) {
    if (event == kEventInit) {
        doStuff();
    }
    return 0;
}
`)

	if _, err := patchEventHandlerInit(path, "pd"); err != nil {
		t.Fatalf("first patchEventHandlerInit: %v", err)
	}
	firstContent := mustRead(t, path)

	changed, err := patchEventHandlerInit(path, "pd")
	if err != nil {
		t.Fatalf("second patchEventHandlerInit: %v", err)
	}
	if changed {
		t.Fatal("patchEventHandlerInit() second call changed = true, want false")
	}
	if mustRead(t, path) != firstContent {
		t.Fatal("second patchEventHandlerInit() modified an already-patched file")
	}
}

// TestPatchEventHandlerInitRecognizesPreExistingHandWiring covers a
// project wired by hand before this tool existed (no marker comments,
// e.g. missile-command's own C port at the time this was built) - the
// include and init call are already there, just unmarked, and shouldn't
// be duplicated.
func TestPatchEventHandlerInitRecognizesPreExistingHandWiring(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.c")
	original := `#include "pd_api.h"
#include "mcp_harness.h"

int eventHandler(PlaydateAPI *pd, PDSystemEvent event, uint32_t arg) {
    if (event == kEventInit) {
        mcp_harness_init(pd);
        doStuff();
    }
    return 0;
}
`
	mustWrite(t, path, original)

	changed, err := patchEventHandlerInit(path, "pd")
	if err != nil {
		t.Fatalf("patchEventHandlerInit: %v", err)
	}
	if changed {
		t.Fatal("patchEventHandlerInit() changed = true, want false (already hand-wired, just unmarked)")
	}
	if mustRead(t, path) != original {
		t.Fatal("patchEventHandlerInit() modified a file that was already correctly wired")
	}
}

func TestPatchUpdateCallbackRecognizesPreExistingHandWiringAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	// Mirrors missile-command's actual C structure: the PlaydateAPI
	// pointer is declared (non-static, extern'd via a header) in a
	// different file than the update callback that uses it - so it's
	// never textually present in mainPath's own content, only used by
	// name. patchUpdateCallback must not report a false manual step when
	// the call is already there, regardless of whether its heuristic can
	// find the variable's declaration.
	mainPath := filepath.Join(dir, "src", "main.c")
	mustWrite(t, mainPath, `#include "pd_api.h"
#include "entity.h"

static int update(void *userdata) {
    mcp_harness_update(mc_pd);
    return 1;
}

int eventHandler(PlaydateAPI *pd, PDSystemEvent event, uint32_t arg) {
    if (event == kEventInit) {
        mc_pd = pd;
        pd->system->setUpdateCallback(update, NULL);
    }
    return 0;
}
`)

	changed, _, manualStep, err := patchUpdateCallback(dir, mainPath)
	if err != nil {
		t.Fatalf("patchUpdateCallback: %v", err)
	}
	if changed {
		t.Fatal("patchUpdateCallback() changed = true, want false (mcp_harness_update() is already there)")
	}
	if manualStep != "" {
		t.Fatalf("patchUpdateCallback() manualStep = %q, want empty - the call already exists, "+
			"regardless of whether the variable's declaration is visible in this file", manualStep)
	}
}

func TestPatchUpdateCallbackFindsStaticPlaydateVar(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "src", "main.c")
	mustWrite(t, mainPath, fixtureStyleMain)

	changed, path, manualStep, err := patchUpdateCallback(dir, mainPath)
	if err != nil {
		t.Fatalf("patchUpdateCallback: %v", err)
	}
	if manualStep != "" {
		t.Fatalf("patchUpdateCallback() manualStep = %q, want empty (should have confidently patched)", manualStep)
	}
	if !changed {
		t.Fatal("patchUpdateCallback() changed = false, want true")
	}
	content := mustRead(t, path)
	if !strings.Contains(content, "mcp_harness_update(pd);") {
		t.Fatalf("missing mcp_harness_update(pd) call:\n%s", content)
	}
}

func TestPatchUpdateCallbackFallsBackWhenNoPlaydateVarVisible(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "src", "main.c")
	mustWrite(t, mainPath, `#include "pd_api.h"

static int update(void *userdata) {
    return 1;
}

int eventHandler(PlaydateAPI *pd, PDSystemEvent event, uint32_t arg) {
    if (event == kEventInit) {
        pd->system->setUpdateCallback(update, NULL);
    }
    return 0;
}
`)

	changed, _, manualStep, err := patchUpdateCallback(dir, mainPath)
	if err != nil {
		t.Fatalf("patchUpdateCallback: %v", err)
	}
	if changed {
		t.Fatal("patchUpdateCallback() changed = true, want false (no PlaydateAPI variable visible in update())")
	}
	if manualStep == "" {
		t.Fatal("patchUpdateCallback() manualStep is empty, want a fallback message")
	}
}

func TestSetupAndTeardownCRoundTrip(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "CMakeLists.txt"),
		"add_library(${NAME} SHARED src/main.c)\n")
	mustWrite(t, filepath.Join(dir, "src", "main.c"), fixtureStyleMain)

	result, err := Setup(dir, C)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("Setup().ManualSteps = %v, want empty for this fixture shape", result.ManualSteps)
	}
	if !fileExists(filepath.Join(dir, "src", "mcp_harness.h")) || !fileExists(filepath.Join(dir, "src", "mcp_harness.c")) {
		t.Fatal("mcp_harness.h/.c were not copied")
	}
	cmakeContent := mustRead(t, filepath.Join(dir, "CMakeLists.txt"))
	if !strings.Contains(cmakeContent, "src/mcp_harness.c") {
		t.Fatalf("CMakeLists.txt wasn't patched:\n%s", cmakeContent)
	}
	mainContent := mustRead(t, filepath.Join(dir, "src", "main.c"))
	if !strings.Contains(mainContent, "mcp_harness_init(playdate);") {
		t.Fatalf("main.c missing mcp_harness_init call:\n%s", mainContent)
	}
	if !strings.Contains(mainContent, "mcp_harness_update(pd);") {
		t.Fatalf("main.c missing mcp_harness_update call:\n%s", mainContent)
	}

	teardownResult, err := Teardown(dir, C)
	if err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	if len(teardownResult.FilesRemoved) != 2 {
		t.Fatalf("Teardown().FilesRemoved = %v, want 2 (mcp_harness.h and .c)", teardownResult.FilesRemoved)
	}
	if fileExists(filepath.Join(dir, "src", "mcp_harness.h")) || fileExists(filepath.Join(dir, "src", "mcp_harness.c")) {
		t.Fatal("mcp_harness.h/.c still exist after teardown")
	}
	if strings.Contains(mustRead(t, filepath.Join(dir, "CMakeLists.txt")), "mcp_harness.c") {
		t.Fatal("CMakeLists.txt still references mcp_harness.c after teardown")
	}
	finalMain := mustRead(t, filepath.Join(dir, "src", "main.c"))
	if strings.Contains(finalMain, "mcp_harness") {
		t.Fatalf("main.c still references mcp_harness after teardown:\n%s", finalMain)
	}
}

// TestSetupThenTeardownCWithInputCallsIsANoOp documents a deliberate
// trade-off: patchInputCalls' pd->system->getButtonState replacements are
// never reversed by teardown (no clean way to mark a mid-expression
// substitution the way whole-line inserts use comment markers), so once
// setup has converted any input call, teardown becomes a full no-op for
// that project going forward - the same conservative choice already
// applied to hand-written init/update calls, not a bug.
func TestSetupThenTeardownCWithInputCallsIsANoOp(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "CMakeLists.txt"),
		"add_library(${NAME} SHARED src/main.c)\n")
	mustWrite(t, filepath.Join(dir, "src", "main.c"), `#include "pd_api.h"

static PlaydateAPI *pd;

static int update(void *userdata) {
    PDButtons current;
    pd->system->getButtonState(&current, NULL, NULL);
    return 1;
}

int eventHandler(PlaydateAPI *playdate, PDSystemEvent event, uint32_t arg) {
    if (event == kEventInit) {
        pd = playdate;
        pd->system->setUpdateCallback(update, NULL);
    }
    return 0;
}
`)

	if _, err := Setup(dir, C); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if !strings.Contains(mustRead(t, filepath.Join(dir, "src", "main.c")), "mcp_get_button_state(pd,") {
		t.Fatal("Setup didn't convert the direct getButtonState call")
	}

	teardownResult, err := Teardown(dir, C)
	if err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	if len(teardownResult.FilesRemoved) != 0 || len(teardownResult.FilesPatched) != 0 {
		t.Fatalf("Teardown() = %+v, want a full no-op - mcp_get_button_state() is still called", teardownResult)
	}
	if !fileExists(filepath.Join(dir, "src", "mcp_harness.c")) {
		t.Fatal("Teardown removed mcp_harness.c even though mcp_get_button_state() still calls into it")
	}
}
