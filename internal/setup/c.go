package setup

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func setupC(sourceDir, repoRoot string) (SetupResult, error) {
	result := SetupResult{Language: C}

	for _, name := range []string{"mcp_harness.h", "mcp_harness.c"} {
		src := filepath.Join(repoRoot, "c-harness", name)
		dst := filepath.Join(sourceDir, "src", name)
		if err := copyFile(src, dst); err != nil {
			return result, fmt.Errorf("copying %s: %w", name, err)
		}
		result.FilesCopied = append(result.FilesCopied, dst)
	}

	cmakePath := filepath.Join(sourceDir, "CMakeLists.txt")
	cmakeChanged, err := patchCMakeLists(cmakePath)
	if err != nil {
		return result, fmt.Errorf("patching CMakeLists.txt: %w", err)
	}
	result.FilesPatched = append(result.FilesPatched, FileChange{Path: cmakePath, Changed: cmakeChanged})
	if !cmakeChanged && !fileContains(cmakePath, "src/mcp_harness.c") {
		result.ManualSteps = append(result.ManualSteps, fmt.Sprintf(
			"could not find a source list to patch in %s - add src/mcp_harness.c to your add_library/add_executable call yourself",
			cmakePath))
	}

	eventHandlerPath, pdVar, err := findEventHandler(sourceDir)
	if err != nil {
		result.ManualSteps = append(result.ManualSteps, fmt.Sprintf(
			"could not find a file defining eventHandler(PlaydateAPI*, ...) under %s - add #include \"mcp_harness.h\", "+
				"call mcp_harness_init(pd) on kEventInit, and call mcp_harness_update(pd) once per frame from your update "+
				"callback yourself (using whatever your own PlaydateAPI pointer is named)", sourceDir))
		return result, nil
	}

	initChanged, err := patchEventHandlerInit(eventHandlerPath, pdVar)
	if err != nil {
		return result, fmt.Errorf("patching %s: %w", eventHandlerPath, err)
	}
	result.FilesPatched = append(result.FilesPatched, FileChange{Path: eventHandlerPath, Changed: initChanged})

	updateChanged, updatePath, manualStep, err := patchUpdateCallback(sourceDir, eventHandlerPath)
	if err != nil {
		return result, fmt.Errorf("patching update callback: %w", err)
	}
	if updatePath != "" {
		result.FilesPatched = append(result.FilesPatched, FileChange{Path: updatePath, Changed: updateChanged})
	}
	if manualStep != "" {
		result.ManualSteps = append(result.ManualSteps, manualStep)
	}

	inputChanges, err := patchInputCalls(sourceDir)
	if err != nil {
		return result, fmt.Errorf("patching input calls: %w", err)
	}
	result.FilesPatched = append(result.FilesPatched, inputChanges...)

	return result, nil
}

// patchInputCalls replaces direct pd->system->getButtonState/getCrankAngle/
// getCrankChange/isCrankDocked calls with their mcp_get_* wrapper
// equivalents, wherever they appear in the project - not just in the
// eventHandler/update-callback files, since input-reading code can live
// anywhere. Without this, press_button/set_crank silently do nothing for
// any game that wasn't already written with the harness in mind (i.e.
// virtually every real, pre-existing C game) - pd->system is write-
// protected in memory in the real Simulator, so the override can only
// take effect through these wrapper functions, never by patching
// pd->system itself. See the design note in mcp_harness.h.
//
// Not reversed by teardown - see cHasUnmarkedHarnessReference, which
// treats any mcp_get_* call as a reason to leave the harness files (and
// everything else) in place, the same conservative "don't guess" choice
// applied to hand-written init/update calls.
var (
	getButtonStateCallRe = regexp.MustCompile(`(\w+)->system->getButtonState\(`)
	getCrankAngleCallRe  = regexp.MustCompile(`(\w+)->system->getCrankAngle\(\)`)
	getCrankChangeCallRe = regexp.MustCompile(`(\w+)->system->getCrankChange\(\)`)
	isCrankDockedCallRe  = regexp.MustCompile(`(\w+)->system->isCrankDocked\(\)`)
)

func patchInputCalls(sourceDir string) ([]FileChange, error) {
	var changes []FileChange
	err := filepath.WalkDir(sourceDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Base(p) == "mcp_harness.c" || !strings.HasSuffix(p, ".c") {
			return err
		}
		b, readErr := os.ReadFile(p)
		if readErr != nil {
			return nil
		}
		content := string(b)
		patched := getButtonStateCallRe.ReplaceAllString(content, "mcp_get_button_state($1, ")
		patched = getCrankAngleCallRe.ReplaceAllString(patched, "mcp_get_crank_angle($1)")
		patched = getCrankChangeCallRe.ReplaceAllString(patched, "mcp_get_crank_change($1)")
		patched = isCrankDockedCallRe.ReplaceAllString(patched, "mcp_get_crank_docked($1)")
		if patched == content {
			return nil
		}
		if writeErr := os.WriteFile(p, []byte(patched), 0o644); writeErr != nil {
			return writeErr
		}
		changes = append(changes, FileChange{Path: p, Changed: true})
		return nil
	})
	return changes, err
}

func fileContains(path, substr string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(string(b), substr)
}

// patchCMakeLists can't use marker comments the way Lua/C source files do -
// a CMake comment mid-argument-list would comment out the rest of that
// line, breaking the call. "src/mcp_harness.c" is unique and unambiguous
// enough (a user's own project wouldn't organically have a file with this
// exact name) to use directly for both idempotency and teardown instead.
//
// Handles the two source-list shapes seen across this project's own
// examples: an inline list (add_library(NAME SHARED src/main.c ...)) and
// a set(GAME_SOURCES ...) variable referenced by ${GAME_SOURCES}. Doesn't
// guess at anything else - see setupC's ManualSteps fallback.
var (
	setBlockRe = regexp.MustCompile(`(?s)set\(\s*[A-Za-z_][A-Za-z0-9_]*\s*\n(.*?)\n(\s*)\)`)
	addCallRe  = regexp.MustCompile(`(?s)(?:add_library|add_executable)\(([^()]*)\)`)
)

func patchCMakeLists(path string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	content := string(b)
	if strings.Contains(content, "src/mcp_harness.c") {
		return false, nil
	}

	if loc := setBlockRe.FindStringSubmatchIndex(content); loc != nil {
		body := content[loc[2]:loc[3]]
		if strings.Contains(body, ".c") {
			indent := content[loc[4]:loc[5]]
			insertion := "\n" + indent + "\tsrc/mcp_harness.c"
			bodyEnd := loc[3]
			content = content[:bodyEnd] + insertion + content[bodyEnd:]
			return true, os.WriteFile(path, []byte(content), 0o644)
		}
	}

	matches := addCallRe.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return false, nil
	}
	changed := false
	for i := len(matches) - 1; i >= 0; i-- {
		m := matches[i]
		argsStart, argsEnd := m[2], m[3]
		if strings.Contains(content[argsStart:argsEnd], "mcp_harness.c") {
			continue
		}
		content = content[:argsEnd] + " src/mcp_harness.c" + content[argsEnd:]
		changed = true
	}
	if !changed {
		return false, nil
	}
	return true, os.WriteFile(path, []byte(content), 0o644)
}

var eventHandlerRe = regexp.MustCompile(`\beventHandler\s*\(\s*PlaydateAPI\s*\*\s*(\w+)`)

// findEventHandler walks sourceDir for the .c file defining eventHandler,
// returning its path and the actual parameter name it uses for the
// PlaydateAPI pointer (varies across real projects - "pd" and "playdate"
// both appear in this project's own examples - so it's captured rather
// than assumed).
func findEventHandler(sourceDir string) (path, pdVar string, err error) {
	walkErr := filepath.WalkDir(sourceDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Base(p) == "mcp_harness.c" || !strings.HasSuffix(p, ".c") {
			return err
		}
		b, readErr := os.ReadFile(p)
		if readErr != nil {
			return nil
		}
		if m := eventHandlerRe.FindSubmatch(b); m != nil {
			path, pdVar = p, string(m[1])
			return fs.SkipAll
		}
		return nil
	})
	if walkErr != nil {
		return "", "", walkErr
	}
	if path == "" {
		return "", "", fmt.Errorf("no file defining eventHandler(PlaydateAPI*, ...) found under %s", sourceDir)
	}
	return path, pdVar, nil
}

var includeRe = regexp.MustCompile(`(?m)^#include\s*[<"][^>"]+[>"]\s*$`)

var kEventInitRe = regexp.MustCompile(`event\s*==\s*kEventInit\s*\)\s*\{`)

// patchEventHandlerInit inserts the #include and the mcp_harness_init()
// call independently, each checked against the file's literal existing
// content (not hasMarkerBlock) - a project already hand-wired before this
// tool existed (no markers) would otherwise get a duplicate of either.
func patchEventHandlerInit(path, pdVar string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	content := string(b)
	changed := false

	if !strings.Contains(content, `#include "mcp_harness.h"`) {
		includeBlock := markerBlock(cMarkerBegin, cMarkerEnd, `#include "mcp_harness.h"`)
		if allIncludes := includeRe.FindAllStringIndex(content, -1); len(allIncludes) > 0 {
			last := allIncludes[len(allIncludes)-1]
			content = content[:last[1]] + "\n" + includeBlock + content[last[1]:]
		} else {
			content = includeBlock + "\n" + content
		}
		changed = true
	}

	if !strings.Contains(content, "mcp_harness_init(") {
		loc := kEventInitRe.FindStringIndex(content)
		if loc == nil {
			return false, fmt.Errorf("found eventHandler but not its kEventInit branch in %s", path)
		}
		initBlock := markerBlock(cMarkerBegin, cMarkerEnd, fmt.Sprintf("mcp_harness_init(%s);", pdVar))
		insertAt := loc[1]
		content = content[:insertAt] + "\n" + initBlock + content[insertAt:]
		changed = true
	}

	if !changed {
		return false, nil
	}
	return true, os.WriteFile(path, []byte(content), 0o644)
}

var setUpdateCallbackRe = regexp.MustCompile(`setUpdateCallback\s*\(\s*(\w+)\s*,`)

// patchUpdateCallback finds the function registered via
// pd->system->setUpdateCallback and, if a PlaydateAPI pointer is visible
// inside it (a global/static variable declared in the same file - the
// pattern every example in this project already uses), inserts
// mcp_harness_update() as its first statement. The update callback's own
// signature (int (*)(void *userdata), per the SDK) never receives a
// PlaydateAPI pointer directly, so this can't be assumed the way the
// eventHandler's own parameter can - if no such variable is confidently
// found, this reports a ManualStep instead of guessing.
func patchUpdateCallback(sourceDir, eventHandlerPath string) (changed bool, path, manualStep string, err error) {
	b, err := os.ReadFile(eventHandlerPath)
	if err != nil {
		return false, "", "", err
	}
	content := string(b)

	m := setUpdateCallbackRe.FindStringSubmatch(content)
	if m == nil {
		return false, "", fmt.Sprintf(
			"could not find a pd->system->setUpdateCallback(...) call to identify your update function - " +
				"call mcp_harness_update(pd) yourself as the first line of whatever function you register"), nil
	}
	callbackName := m[1]

	funcRe := regexp.MustCompile(`(?s)\b` + regexp.QuoteMeta(callbackName) + `\s*\([^)]*\)\s*\{`)
	funcLoc := funcRe.FindStringIndex(content)
	funcFile := eventHandlerPath
	if funcLoc == nil {
		// The callback might be defined in a different .c file.
		found, foundContent, ferr := findFunctionInSourceDir(sourceDir, funcRe)
		if ferr != nil {
			return false, "", "", ferr
		}
		if found == "" {
			return false, "", fmt.Sprintf(
				"found setUpdateCallback(%s, ...) but not %s's own definition - "+
					"call mcp_harness_update(pd) yourself as its first line (using whatever your own PlaydateAPI pointer is named)",
				callbackName, callbackName), nil
		}
		funcFile = found
		content = foundContent
		funcLoc = funcRe.FindStringIndex(content)
	}

	// Checked before the PlaydateAPI-variable heuristic below, not after -
	// a project whose variable is declared in a header the update function
	// only #includes (rather than textually containing the declaration
	// itself, e.g. missile-command's `extern PlaydateAPI *mc_pd;` in
	// entity.h) would otherwise get a false "can't find a variable" report
	// even though mcp_harness_update() is already correctly called. Checks
	// specifically for this call, not hasMarkerBlock(content, cMarkerBegin)
	// - patchEventHandlerInit may have already added its own, unrelated
	// marker blocks (include/init) to this same file when the update
	// callback and eventHandler share a file, which would otherwise look
	// identical to "already patched" here.
	if strings.Contains(content, "mcp_harness_update(") {
		return false, "", "", nil
	}

	pdVar, ok := findAccessiblePlaydateVar(content)
	if !ok {
		return false, "", fmt.Sprintf(
			"found %s (your update function) but no PlaydateAPI pointer variable visible in %s to call "+
				"mcp_harness_update() with - add one (e.g. a static PlaydateAPI *pd set on kEventInit) and call "+
				"mcp_harness_update(pd) yourself as %s's first line", callbackName, funcFile, callbackName), nil
	}

	block := markerBlock(cMarkerBegin, cMarkerEnd, fmt.Sprintf("mcp_harness_update(%s);", pdVar))
	insertAt := funcLoc[1]
	content = content[:insertAt] + "\n" + block + content[insertAt:]

	if err := os.WriteFile(funcFile, []byte(content), 0o644); err != nil {
		return false, "", "", err
	}
	return true, funcFile, "", nil
}

func findFunctionInSourceDir(sourceDir string, funcRe *regexp.Regexp) (path, content string, err error) {
	walkErr := filepath.WalkDir(sourceDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Base(p) == "mcp_harness.c" || !strings.HasSuffix(p, ".c") {
			return err
		}
		b, readErr := os.ReadFile(p)
		if readErr != nil {
			return nil
		}
		if funcRe.Match(b) {
			path, content = p, string(b)
			return fs.SkipAll
		}
		return nil
	})
	return path, content, walkErr
}

var playdateVarRe = regexp.MustCompile(`(?:static\s+)?PlaydateAPI\s*\*\s*(\w+)\s*(?:=|;)`)

// findAccessiblePlaydateVar looks for a global/static PlaydateAPI pointer
// declaration in content - the pattern this project's own examples all
// use (g_pd, mc_pd, a file-static "pd", etc.) to reach the API from
// inside a callback that doesn't receive it as a parameter.
func findAccessiblePlaydateVar(content string) (string, bool) {
	m := playdateVarRe.FindStringSubmatch(content)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// teardownC is a full no-op - CMakeLists.txt untouched, no files stripped
// or removed - if any .c file has a harness reference outside a marker
// block. CMakeLists.txt can't use markers at all (a CMake comment
// mid-argument-list would comment out the rest of that line - see
// patchCMakeLists), so it's fundamentally unable to tell whether its own
// "src/mcp_harness.c" entry is hand-written or something this tool added.
// Partially tearing down - e.g. stripping that entry while leaving
// mcp_harness.h/.c in place because a hand-written #include/call elsewhere
// still needs them - was tried and found to compile "successfully" but
// crash at runtime: shared libraries link with undefined symbols by
// default (no -Wl,-z,defs), so a missing mcp_harness_init/update reference
// doesn't fail until the Simulator actually calls it. An unmarked
// reference anywhere means at least part of this project's wiring predates
// this tool (or was done by hand) in a way CMakeLists.txt can't be safely
// disentangled from - so nothing is touched at all in that case, matching
// how missile-command's own C port (hand-wired before this tool existed)
// needs to behave.
func teardownC(sourceDir string) (TeardownResult, error) {
	result := TeardownResult{}

	handWired, err := cHasUnmarkedHarnessReference(sourceDir)
	if err != nil {
		return result, err
	}
	if handWired {
		return result, nil
	}

	if cmakeChanged, err := teardownCMakeLists(filepath.Join(sourceDir, "CMakeLists.txt")); err != nil {
		return result, fmt.Errorf("patching CMakeLists.txt: %w", err)
	} else if cmakeChanged {
		result.FilesPatched = append(result.FilesPatched, FileChange{Path: filepath.Join(sourceDir, "CMakeLists.txt"), Changed: true})
	}

	// Marker blocks may be spread across the eventHandler file and,
	// separately, whatever file defines the update callback - strip them
	// from every .c file under sourceDir rather than re-deriving which
	// files setup touched. cHasUnmarkedHarnessReference already confirmed
	// nothing remains outside these blocks, so it's safe to remove the
	// harness files unconditionally afterward.
	err = filepath.WalkDir(sourceDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Base(p) == "mcp_harness.c" || !strings.HasSuffix(p, ".c") {
			return err
		}
		b, readErr := os.ReadFile(p)
		if readErr != nil {
			return nil
		}
		content, changed := stripMarkerBlocks(string(b), cMarkerBegin, cMarkerEnd)
		if !changed {
			return nil
		}
		if writeErr := os.WriteFile(p, []byte(content), 0o644); writeErr != nil {
			return writeErr
		}
		result.FilesPatched = append(result.FilesPatched, FileChange{Path: p, Changed: true})
		return nil
	})
	if err != nil {
		return result, fmt.Errorf("stripping marker blocks: %w", err)
	}

	for _, name := range []string{"mcp_harness.h", "mcp_harness.c"} {
		p := filepath.Join(sourceDir, "src", name)
		if fileExists(p) {
			if err := os.Remove(p); err != nil {
				return result, fmt.Errorf("removing %s: %w", name, err)
			}
			result.FilesRemoved = append(result.FilesRemoved, p)
		}
	}

	return result, nil
}

// cHasUnmarkedHarnessReference reports whether any .c file under
// sourceDir references the harness (#include, mcp_harness_init, or
// mcp_harness_update) outside of a marker block - i.e. in a way this
// tool didn't add and can't safely remove.
func cHasUnmarkedHarnessReference(sourceDir string) (bool, error) {
	found := false
	err := filepath.WalkDir(sourceDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Base(p) == "mcp_harness.c" || !strings.HasSuffix(p, ".c") {
			return err
		}
		b, readErr := os.ReadFile(p)
		if readErr != nil {
			return nil
		}
		withoutMarkers, _ := stripMarkerBlocks(string(b), cMarkerBegin, cMarkerEnd)
		if strings.Contains(withoutMarkers, `#include "mcp_harness.h"`) ||
			strings.Contains(withoutMarkers, "mcp_harness_init(") ||
			strings.Contains(withoutMarkers, "mcp_harness_update(") ||
			// patchInputCalls' replacements are never marker-wrapped (same
			// reasoning as CMakeLists.txt: inline, no clean way to mark a
			// mid-expression substitution) and never reversed - their
			// presence means mcp_harness.c/.h are still needed regardless
			// of whether setup or a human put them there.
			strings.Contains(withoutMarkers, "mcp_get_button_state(") ||
			strings.Contains(withoutMarkers, "mcp_get_crank_angle(") ||
			strings.Contains(withoutMarkers, "mcp_get_crank_change(") ||
			strings.Contains(withoutMarkers, "mcp_get_crank_docked(") {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found, err
}

var mcpHarnessCRefRe = regexp.MustCompile(`[ \t\n]*src/mcp_harness\.c`)

func teardownCMakeLists(path string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	content := string(b)
	if !strings.Contains(content, "src/mcp_harness.c") {
		return false, nil
	}
	newContent := mcpHarnessCRefRe.ReplaceAllString(content, "")
	return true, os.WriteFile(path, []byte(newContent), 0o644)
}
