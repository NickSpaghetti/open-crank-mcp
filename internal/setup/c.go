package setup

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/NickSpaghetti/open-crank-mcp/internal/scan"
)

func setupC(sourceDir string, harnessFS fs.FS) (SetupResult, error) {
	result := SetupResult{Language: C}

	for _, name := range []string{"mcp_harness.h", "mcp_harness.c"} {
		dst := filepath.Join(sourceDir, "src", name)
		if err := CopyHarnessFile(harnessFS, path.Join("c-harness", name), dst); err != nil {
			return result, fmt.Errorf("copying %s: %w", name, err)
		}
		result.FilesCopied = append(result.FilesCopied, dst)
	}

	// Located before CMakeLists.txt is touched, not after, because which
	// build target counts as "the game" is decided by whether it lists this
	// file - see patchCMakeSourceList. A project with no eventHandler at all
	// still gets its CMakeLists patched (with the looser rule) and the
	// ManualStep below: the harness has to be compiled in either way for a
	// hand-written init/update call to link.
	eventHandlerPath, pdVar, ehErr := findEventHandler(sourceDir)
	gameSource := ""
	if ehErr == nil {
		if rel, relErr := filepath.Rel(sourceDir, eventHandlerPath); relErr == nil {
			gameSource = filepath.ToSlash(rel)
		}
	}

	cmakePath := filepath.Join(sourceDir, "CMakeLists.txt")
	cmakeChanged, err := patchCMakeLists(cmakePath, gameSource)
	if err != nil {
		return result, fmt.Errorf("patching CMakeLists.txt: %w", err)
	}
	result.FilesPatched = append(result.FilesPatched, FileChange{Path: cmakePath, Changed: cmakeChanged})
	// Checked against the file's actual end state, not cmakeChanged - that
	// flag can be true purely from the include_directories(src) patch (see
	// patchCMakeLists) even when the source-list patch itself found nothing
	// to touch, which would otherwise wrongly suppress this ManualStep.
	if !cmakeListsContains(cmakePath, "src/mcp_harness.c") {
		result.ManualSteps = append(result.ManualSteps, fmt.Sprintf(
			"could not find a source list to patch in %s - add src/mcp_harness.c to your add_library/add_executable call yourself",
			cmakePath))
	}

	if ehErr != nil {
		result.ManualSteps = append(result.ManualSteps, fmt.Sprintf(
			"could not find a file defining eventHandler(PlaydateAPI*, ...) under %s - add #include \"mcp_harness.h\", "+
				"call mcp_harness_init(pd) on kEventInit, and call mcp_harness_update(pd) once per frame from your update "+
				"callback yourself (using whatever your own PlaydateAPI pointer is named)", sourceDir))
		return result, nil
	}

	initChanged, initStep, err := patchEventHandlerInit(eventHandlerPath, pdVar)
	if err != nil {
		return result, fmt.Errorf("patching %s: %w", eventHandlerPath, err)
	}
	result.FilesPatched = append(result.FilesPatched, FileChange{Path: eventHandlerPath, Changed: initChanged})
	if initStep != "" {
		result.ManualSteps = append(result.ManualSteps, initStep)
	}

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

	inputChanges, inputSteps, err := patchInputCalls(sourceDir)
	if err != nil {
		return result, fmt.Errorf("patching input calls: %w", err)
	}
	result.FilesPatched = append(result.FilesPatched, inputChanges...)
	result.ManualSteps = append(result.ManualSteps, inputSteps...)

	return result, nil
}

// The four SDK input calls, each with the wrapper that replaces it. Only
// getButtonState takes arguments of its own, so only its replacement leaves the
// argument list open for them.
var inputCallPatterns = []struct {
	sdkCall   string
	wrapper   string
	takesArgs bool
}{
	{sdkCall: "getButtonState", wrapper: "mcp_get_button_state", takesArgs: true},
	{sdkCall: "getCrankAngle", wrapper: "mcp_get_crank_angle"},
	{sdkCall: "getCrankChange", wrapper: "mcp_get_crank_change"},
	{sdkCall: "isCrankDocked", wrapper: "mcp_get_crank_docked"},
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
// Anything it could not rewrite comes back as a ManualStep rather than being
// left silent - see leftoverInputCallSteps.
//
// Not reversed by teardown - see cHasUnmarkedHarnessReference, which
// treats any mcp_get_* call as a reason to leave the harness files (and
// everything else) in place, the same conservative "don't guess" choice
// applied to hand-written init/update calls.
func patchInputCalls(sourceDir string) ([]FileChange, []string, error) {
	var changes []FileChange
	var manualSteps []string
	err := walkCSources(sourceDir, func(p string, b []byte) (bool, error) {
		content := string(b)
		patched := patchInputCallsInContent(content)
		manualSteps = append(manualSteps, leftoverInputCallSteps(p, patched)...)
		if patched == content {
			return false, nil
		}
		// A file other than the eventHandler one (which already got its
		// own #include from patchEventHandlerInit) may not have any
		// declaration of the mcp_get_* functions now used in it - without
		// this, it "compiles" only as an implicit-declaration warning
		// (and only links because mcp_harness.c happens to be in the same
		// build), which breaks under a stricter toolchain or C23.
		patched, _ = insertIncludeIfMissing(patched)
		if writeErr := os.WriteFile(p, []byte(patched), 0o644); writeErr != nil {
			return false, writeErr
		}
		changes = append(changes, FileChange{Path: p, Changed: true})
		return false, nil
	})
	return changes, manualSteps, err
}

// patchInputCallsInContent rewrites every input call it can reach. Matches
// are applied last-to-first so each edit leaves the offsets of the ones still
// to come untouched, and a receiver that turns out to be part of a larger
// expression is skipped rather than mangled - leftoverInputCallSteps then
// reports it.
func patchInputCallsInContent(content string) string {
	for _, pat := range inputCallPatterns {
		// Reclassified per pattern rather than once up front: an earlier
		// pattern's rewrites have already changed the file, and a stale map
		// would be reading the wrong bytes. Within one pattern it stays valid,
		// because the rewrites run last-to-first and so only ever touch text
		// after the part still being searched.
		code := scan.CCode(content)
		// Last occurrence first, so each rewrite leaves the offsets of the ones
		// still to come untouched.
		anchor := "->system->" + pat.sdkCall + "("
		for limit := len(content); ; {
			at := code.LastIndex(content, anchor, limit)
			if at < 0 {
				break
			}
			limit = at
			callEnd := at + len(anchor)
			if !pat.takesArgs {
				// The crank calls take none, so the "()" is part of what gets
				// replaced. Anything between the parens means this is not the
				// call it looks like.
				end, ok := scan.Literal(content, callEnd, ")")
				if !ok {
					continue
				}
				callEnd = end
			}
			recvStart, ok := receiverChainStart(content, at)
			if !ok || !receiverIsWhole(content, recvStart) {
				continue
			}
			call := pat.wrapper + "(" + content[recvStart:at]
			if pat.takesArgs {
				call += ", "
			} else {
				call += ")"
			}
			content = content[:recvStart] + call + content[callEnd:]
			// Everything from here on has moved; only the text in front of the
			// receiver is still at the offsets it was.
			limit = recvStart
		}
	}
	return content
}

// receiverChainStart walks leftward from the "->system->" anchor at to the start
// of the member-access chain in front of it, so game->pd->system->... yields
// "game->pd" rather than just "pd".
//
// Whitespace is allowed between the links of the chain but not before the anchor
// itself, which is where the calls this tool has actually seen put it. A chain
// that stops at something other than an identifier (a call or a subscript, as in
// api()->pd->system->...) reports the last identifier as the start; receiverIsWhole
// is what then declines to rewrite it.
func receiverChainStart(content string, at int) (int, bool) {
	name, start := scan.IdentifierBefore(content, at)
	if name == "" {
		return 0, false
	}
	for {
		linkEnd := scan.SkipSpaceBefore(content, start)
		arrowStart := linkEnd - 2
		if linkEnd >= 1 && content[linkEnd-1] == '.' {
			arrowStart = linkEnd - 1
		} else if arrowStart < 0 || content[arrowStart:linkEnd] != "->" {
			return start, true
		}
		prev, prevStart := scan.IdentifierBefore(content, scan.SkipSpaceBefore(content, arrowStart))
		if prev == "" {
			return start, true
		}
		start = prevStart
	}
}

// receiverIsWhole reports whether the match starting at at begins the whole
// receiver expression. A match that starts immediately after "->", ".", ")"
// or "]" is the tail of something bigger - foo(x)->pd->system->getButtonState
// captures only "pd", and rewriting that much would leave foo(x)-> attached
// to a function call. Two bytes are compared for "->" rather than one for ">"
// so a greater-than (a > pd->system->getCrankAngle()) still counts as whole.
func receiverIsWhole(content string, at int) bool {
	if at >= 2 && content[at-2:at] == "->" {
		return false
	}
	if at >= 1 {
		switch content[at-1] {
		case '.', ')', ']':
			return false
		}
	}
	return true
}

// leftoverInputCallSteps reports every input call that survived patching,
// whatever shape it is in - spacing the rewriter doesn't accept, a receiver it
// can't rewrite ((*pdp)->system->..., api()->pd->system->...), or a line split.
//
// It only ever reports, never rewrites: a silently unpatched input call means
// press_button/set_crank do nothing at runtime while setup reports success,
// which is the hardest failure in this tool to diagnose from the outside. Being
// loose is the point - a false report costs the user one line of advisory text,
// a miss costs them an afternoon.
func leftoverInputCallSteps(path, content string) []string {
	var steps []string
	// Loose about shape, strict about being code. A call named in a comment is
	// not one this tool failed to rewrite - it is not a call at all, and
	// reporting it would send the user looking for something to fix in a line
	// that is already fine.
	code := scan.CCode(content)
	for i := 0; ; {
		at := code.Index(content, "system", i)
		if at < 0 {
			return steps
		}
		i = at + len("system")

		j := scan.SkipSpace(content, i)
		j, ok := scan.Literal(content, j, "->")
		if !ok {
			continue
		}
		j = scan.SkipSpace(content, j)
		name, j := scan.Identifier(content, j)
		wrapper, isInputCall := wrapperFor(name)
		if !isInputCall {
			continue
		}
		if _, ok := scan.Literal(content, scan.SkipSpace(content, j), "("); !ok {
			continue
		}
		steps = append(steps, fmt.Sprintf(
			"%s:%d reads input directly through ->system->%s(...) in a form this tool could not rewrite - "+
				"replace it with %s(<your PlaydateAPI pointer>, ...) yourself, or press_button and set_crank "+
				"will have no effect on it",
			path, scan.LineNumber(content, at), name, wrapper))
	}
}

func wrapperFor(sdkCall string) (string, bool) {
	for _, pat := range inputCallPatterns {
		if pat.sdkCall == sdkCall {
			return pat.wrapper, true
		}
	}
	return "", false
}

// cmakeListsContains reports whether a CMakeLists names substr as part of the
// build rather than in a comment about it.
func cmakeListsContains(path, substr string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	content := string(b)
	return scan.CMakeCode(content).Contains(content, substr)
}

// patchCMakeLists makes two independent edits, in one read/write pass:
// adding "src/mcp_harness.c" to the project's source list, and adding an
// include_directories(src) line so #include "mcp_harness.h" resolves from
// any .c file in the project - not just ones that happen to already live
// in src/ alongside it (see patchCMakeIncludeDirectories).
func patchCMakeLists(path, gameSource string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	content := string(b)
	changed := false

	// Code only, for the same reason as the include_directories guard: a comment
	// naming the harness source is not an entry in a source list.
	if !scan.CMakeCode(content).Contains(content, "src/mcp_harness.c") {
		if newContent, ok := patchCMakeSourceList(content, gameSource); ok {
			content = newContent
			changed = true
		}
	}

	if newContent, ok := patchCMakeIncludeDirectories(content); ok {
		content = newContent
		changed = true
	}

	if !changed {
		return false, nil
	}
	return true, os.WriteFile(path, []byte(content), 0o644)
}

// patchCMakeSourceList can't use marker comments the way Lua/C source
// files do - a CMake comment mid-argument-list would comment out the rest
// of that line, breaking the call. "src/mcp_harness.c" is unique and
// unambiguous enough (a user's own project wouldn't organically have a
// file with this exact name) to use directly for both idempotency and
// teardown instead.
//
// Handles the two source-list shapes seen across this project's own
// examples: an inline list (add_library(NAME SHARED src/main.c ...)) and
// a set(GAME_SOURCES ...) variable referenced by ${GAME_SOURCES}. Doesn't
// guess at anything else - see setupC's ManualSteps fallback.
// patchCMakeSourceList adds src/mcp_harness.c to the source list that builds
// the game. gameSource is the project-relative path of the file defining
// eventHandler ("src/main.c" in every shape this tool has seen), which is what
// makes "the game's source list" identifiable at all - see targetBuildsGame.
// An empty gameSource means it could not be determined, and the looser rule
// applies: any list that already holds sources.
func patchCMakeSourceList(content, gameSource string) (string, bool) {
	for _, block := range scan.CMakeSetBlocks(content) {
		if !sourceListBuildsGame(block.Body(content), gameSource) {
			continue
		}
		insertion := "\n" + block.Indent(content) + "\tsrc/mcp_harness.c"
		return content[:block.BodyEnd] + insertion + content[block.BodyEnd:], true
	}

	calls := scan.CMakeCalls(content, "add_library", "add_executable")
	if len(calls) == 0 {
		return content, false
	}
	changed := false
	// Last to first, so each insertion leaves the offsets of the calls still to
	// come untouched.
	for i := len(calls) - 1; i >= 0; i-- {
		args := content[calls[i].ArgsStart:calls[i].ArgsEnd]
		if strings.Contains(args, "mcp_harness.c") {
			continue
		}
		if !targetBuildsGame(content, args, gameSource) {
			continue
		}
		at := calls[i].ArgsEnd
		content = content[:at] + " src/mcp_harness.c" + content[at:]
		changed = true
	}
	return content, changed
}

// sourceListBuildsGame reports whether a set() body is the game's source list.
// Every multi-line set() in the file is now considered rather than only the
// first: a project that lists something else multi-line before its sources
// (compiler flags, a tool's inputs) used to abandon the set() path entirely on
// that first non-matching block.
func sourceListBuildsGame(body, gameSource string) bool {
	sawSource := false
	for _, tok := range scan.CMakeArgTokens(body) {
		if !scan.IsCSourceFile(tok) {
			continue
		}
		if gameSource != "" && scan.SamePath(tok, gameSource) {
			return true
		}
		sawSource = true
	}
	return gameSource == "" && sawSource
}

// targetBuildsGame reports whether an add_library/add_executable argument list
// builds the game, i.e. whether the harness belongs in it.
//
// Every target used to get the entry, which broke two ordinary layouts: an
// INTERFACE library (CMake rejects a source on one outright) and an unrelated
// tool target in the same file (mcp_harness.c needs pd_api.h on its include
// path, which a host-side tool target has no reason to have). Both are now
// skipped, and a project where no target can be identified gets the
// "add it yourself" ManualStep instead of a build that fails on the first
// compile.
//
// A ${VAR} that isn't set in this file counts as "yes". It could be a
// file(GLOB ...) result or a parent-scope variable, neither of which is
// resolvable here, and refusing to patch would break projects that work
// today. The cost of guessing wrong in that direction is a harness source
// compiled into a target that didn't need it; the cost in the other direction
// is a game that never answers the harness.
func targetBuildsGame(content, args, gameSource string) bool {
	sawSource := false
	for _, tok := range scan.CMakeArgTokens(args) {
		values := []string{tok}
		if name, ok := scan.CMakeVarReference(tok); ok {
			body, found := scan.CMakeSetBody(content, name)
			if !found {
				return true
			}
			values = scan.CMakeArgTokens(body)
		}
		for _, v := range values {
			// A variable inside a resolved variable is only followed one level
			// deep, then given the same benefit of the doubt as an
			// unresolvable one - a project that keeps its sources in
			// set(GAME_SOURCES ${COMMON_SRC}) builds today and has to keep
			// building.
			if _, nested := scan.CMakeVarReference(v); nested {
				return true
			}
			if !scan.IsCSourceFile(v) {
				continue
			}
			if gameSource != "" && scan.SamePath(v, gameSource) {
				return true
			}
			sawSource = true
		}
	}
	return gameSource == "" && sawSource
}

// patchCMakeIncludeDirectories inserts a marker-wrapped
// include_directories(src) line right after the project(...) line -
// present in every CMakeLists style this tool handles. Unlike the
// source-list entry above, this is its own standalone statement, not an
// insertion inside an existing call's argument list, so it CAN safely use
// marker comments (a "#" here doesn't truncate anything else) and be
// cleanly reversed by teardownCMakeLists.
//
// Needed because a bare #include "mcp_harness.h" only resolves against
// the including file's own directory before falling back to -I search
// paths - every project this tool was tested against originally
// (missile-command, c-harness/test/fixture-game) keeps all its sources
// under src/ alongside mcp_harness.h, so this always happened to resolve.
// The SDK's own "Sprite Game" example keeps main.c/game.c at the project
// root instead, where a bare include has no way to find a header that
// only exists in src/ - this is what makes it reachable regardless of
// where a project's own files live. Checked against the literal text, not
// hasMarkerBlock, so a project that already has this line by hand isn't
// duplicated.
func patchCMakeIncludeDirectories(content string) (string, bool) {
	// Lowercased for the same reason the command patterns are
	// case-insensitive: INCLUDE_DIRECTORIES(src) is the same statement, and
	// adding a second copy of it is pointless noise in the user's file.
	// Code only: a commented-out attempt ("# include_directories(src)  # tried
	// this, did not help") is a note about something that is not there, and
	// reading it as "already present" suppressed the real insertion entirely.
	if scan.CMakeCode(content).Contains(strings.ToLower(content), "include_directories(src)") {
		return content, false
	}
	afterProject, ok := scan.CMakeCallOnOwnLine(content, "project")
	if !ok {
		return content, false
	}
	block := markerBlock(cmakeMarkerBegin, cmakeMarkerEnd, "include_directories(src)")
	return content[:afterProject] + "\n" + block + content[afterProject:], true
}

// eventHandlerParam returns the name the file's eventHandler gives its
// PlaydateAPI pointer parameter. Real projects differ ("pd" and "playdate" both
// appear in this project's own examples), so it is read rather than assumed.
func eventHandlerParam(content string) (string, bool) {
	// A commented-out prototype used to win, and the damage landed downstream:
	// this file was then designated "the" eventHandler file, no kEventInit
	// branch could be found in it, and setup failed the whole call after having
	// already copied the harness in and patched CMakeLists.
	code := scan.CCode(content)
	for i := 0; ; {
		at := code.TokenIndex(content, "eventHandler", i)
		if at < 0 {
			return "", false
		}
		i = at + len("eventHandler")

		j := scan.SkipSpace(content, i)
		j, ok := scan.Literal(content, j, "(")
		if !ok {
			continue
		}
		j, ok = scan.Literal(content, scan.SkipSpace(content, j), "PlaydateAPI")
		if !ok {
			continue
		}
		j, ok = scan.Literal(content, scan.SkipSpace(content, j), "*")
		if !ok {
			continue
		}
		name, _ := scan.Identifier(content, scan.SkipSpace(content, j))
		if name == "" {
			continue
		}
		return name, true
	}
}

// findEventHandler walks sourceDir for the .c file defining eventHandler,
// returning its path and the actual parameter name it uses for the
// PlaydateAPI pointer (varies across real projects - "pd" and "playdate"
// both appear in this project's own examples - so it's captured rather
// than assumed).
func findEventHandler(sourceDir string) (path, pdVar string, err error) {
	walkErr := walkCSources(sourceDir, func(p string, b []byte) (bool, error) {
		if v, ok := eventHandlerParam(string(b)); ok {
			path, pdVar = p, v
			return true, nil
		}
		return false, nil
	})
	if walkErr != nil {
		return "", "", walkErr
	}
	if path == "" {
		return "", "", fmt.Errorf("no file defining eventHandler(PlaydateAPI*, ...) found under %s", sourceDir)
	}
	return path, pdVar, nil
}

// insertIncludeIfMissing inserts a marker-wrapped #include "mcp_harness.h"
// into content - after the last existing #include line if any, otherwise
// prepended - unless the literal include is already present. Shared by
// patchEventHandlerInit and patchInputCalls: both need to guarantee
// mcp_harness.h is reachable from whatever file they're touching, not just
// the one file setup happens to designate as "the" eventHandler file.
func insertIncludeIfMissing(content string) (string, bool) {
	code := scan.CCode(content)
	if code.Contains(content, `#include "mcp_harness.h"`) {
		return content, false
	}
	includeBlock := markerBlock(cMarkerBegin, cMarkerEnd, `#include "mcp_harness.h"`)
	if at, ok := lastIncludeLineEnd(content, code); ok {
		return content[:at] + "\n" + includeBlock + content[at:], true
	}
	return includeBlock + "\n" + content, true
}

// patchEventHandlerInit inserts the #include and the mcp_harness_init()
// call independently, each checked against the file's literal existing
// content (not hasMarkerBlock) - a project already hand-wired before this
// tool existed (no markers) would otherwise get a duplicate of either.
//
// A file whose init branch can't be found is reported as a ManualStep, never
// as an error. It used to fail the whole setup call, which left the harness
// files copied and CMakeLists patched but nothing wired - a worse state than
// "here is the one line to add yourself", and reachable from several ordinary
// ways of writing the handler (see findInitInsertionPoint). The #include is
// still inserted in that case, so the hand-written call has a declaration to
// use, and it is marker-wrapped so teardown reverses it.
func patchEventHandlerInit(path, pdVar string) (changed bool, manualStep string, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, "", err
	}
	content := string(b)

	if newContent, ok := insertIncludeIfMissing(content); ok {
		content = newContent
		changed = true
	}

	// Code only: a comment saying the call was removed is not the call.
	if !scan.CCode(content).Contains(content, "mcp_harness_init(") {
		insertAt, found := findInitInsertionPoint(content)
		switch {
		case !found:
			manualStep = fmt.Sprintf(
				"could not find where %s handles kEventInit - call mcp_harness_init(%s) yourself when your game "+
					"initializes, before its first frame", path, pdVar)
		case insertAt < 0:
			manualStep = fmt.Sprintf(
				"found the kEventInit branch in %s but it has no braces to insert into - give it a { } body and "+
					"call mcp_harness_init(%s) as its first statement", path, pdVar)
		default:
			initBlock := markerBlock(cMarkerBegin, cMarkerEnd, fmt.Sprintf("mcp_harness_init(%s);", pdVar))
			content = content[:insertAt] + "\n" + initBlock + content[insertAt:]
			changed = true
		}
	}

	if !changed {
		return false, manualStep, nil
	}
	return true, manualStep, os.WriteFile(path, []byte(content), 0o644)
}

// findInitInsertionPoint returns the offset at which mcp_harness_init() can be
// inserted as the first statement of the branch handling kEventInit, and
// whether the kEventInit token was there to begin with. The two are reported
// separately because "this file never mentions kEventInit" and "it does, but
// not in a form with a body to insert into" need different advice.
//
// It works from the kEventInit token outward rather than matching a whole
// branch shape, because the shapes are not enumerable in practice. The old
// pattern (event == kEventInit) followed by ") {" refused a switch/case, a
// compound condition (event == kEventInit && !inited), a reversed comparison
// (kEventInit == event) and redundant parens - all ordinary C, all of which
// made setup fail outright.
//
// Two positions are valid, and which one applies is decided by whether the
// token sits in a case label:
//
//	case kEventInit:      insert straight after the colon. A label may be
//	                      followed by any statement, so this works whether or
//	                      not the case body is wrapped in braces.
//	if (... kEventInit ...) {   insert after the opening brace.
//
// For the condition form, a ";" reached before any "{" means there is no
// block: either a braceless branch, or kEventInit used in an expression this
// function has no business rewriting (int wantInit = event == kEventInit;).
// Both return a negative offset rather than guessing.
func findInitInsertionPoint(content string) (offset int, found bool) {
	// The first match in *code*, not the first in the file. A commented-out
	// branch above the live one used to win, putting the init call inside the
	// comment where it never compiles - and setup still reported success, so
	// the symptom was a harness that never answers, which reads like a
	// transport bug rather than a patching one. The worst outcome in this file.
	code := scan.CCode(content)
	at := code.TokenIndex(content, "kEventInit", 0)
	if at < 0 {
		return -1, false
	}
	afterToken := at + len("kEventInit")

	if scan.PrecededByKeyword(content, at, "case") {
		if end, ok := scan.Literal(content, scan.SkipSpace(content, afterToken), ":"); ok {
			return end, true
		}
		return -1, true
	}

	brace, ok := code.BlockOpenAfter(content, afterToken)
	if !ok {
		return -1, true
	}
	return brace + 1, true
}

// registeredUpdateCallback returns the name passed as the first argument to
// pd->system->setUpdateCallback. A cast in front of it (setUpdateCallback(
// (PDCallbackFunction *)update, NULL)) is not read, and reports not-found -
// which becomes a ManualStep rather than a wrong guess.
func registeredUpdateCallback(content string) (string, bool) {
	// A commented-out registration used to win the match, sending the tool
	// looking for a callback the user had already deleted and reporting a
	// ManualStep that named it.
	code := scan.CCode(content)
	for i := 0; ; {
		at := code.TokenIndex(content, "setUpdateCallback", i)
		if at < 0 {
			return "", false
		}
		i = at + len("setUpdateCallback")

		j, ok := scan.Literal(content, scan.SkipSpace(content, i), "(")
		if !ok {
			continue
		}
		name, j := scan.Identifier(content, scan.SkipSpace(content, j))
		if name == "" {
			continue
		}
		if _, ok := scan.Literal(content, scan.SkipSpace(content, j), ","); !ok {
			continue
		}
		return name, true
	}
}

// functionBodyStart finds the definition of the named function and returns the
// offset just past the "{" that opens its body, which is where a first statement
// goes.
//
// A declaration is skipped, since it has no "{". Counting the parameter list's
// parens rather than requiring it to contain none is what makes a
// function-pointer parameter work - int update(int (*tick)(void), void *ud) was
// invisible to the pattern this replaced, so the harness never got wired into
// the games that have one.
func functionBodyStart(content, name string) (offset int, ok bool) {
	code := scan.CCode(content)
	for i := 0; ; {
		at := code.TokenIndex(content, name, i)
		if at < 0 {
			return 0, false
		}
		i = at + len(name)

		_, closeAt, found := scan.CallParens(content, i)
		if !found {
			continue
		}
		brace, found := scan.Literal(content, scan.SkipSpace(content, closeAt+1), "{")
		if !found {
			continue
		}
		return brace, true
	}
}

// lastIncludeLineEnd returns the offset at the end of the file's last #include
// line, where another include can be added.
//
// An include inside a comment does not count, and the reason is worse than
// tidiness. The block this tool inserts is itself a /* */ pair, so landing it
// inside an existing block comment ends that comment early and strands the
// original "*/" as a stray token: a hard compile error pointing at a line the
// user never wrote. A commented-out include is a completely ordinary thing to
// find at the top of a game's main.c.
func lastIncludeLineEnd(content string, code scan.Code) (int, bool) {
	found := false
	end := 0
	for i := 0; i < len(content); {
		lineEnd := len(content)
		if nl := strings.IndexByte(content[i:], '\n'); nl >= 0 {
			lineEnd = i + nl
		}
		if code.IsCode(i) && isIncludeLine(content[i:lineEnd]) {
			found = true
			// The newline itself, not the byte before it: on a CRLF file,
			// stopping short of the "\r" would rewrite the include's own line
			// ending as a bare "\n".
			end = lineEnd
		}
		i = lineEnd + 1
	}
	return end, found
}

// isIncludeLine reports whether a whole line is nothing but an #include
// directive. Deliberately as strict as the pattern it replaces: this only
// decides where the harness include is placed, and a line it declines to
// recognize just means the include goes at the top of the file instead, which
// compiles the same.
func isIncludeLine(line string) bool {
	rest, ok := scan.Literal(line, 0, "#include")
	if !ok {
		return false
	}
	rest = scan.SkipSpace(line, rest)
	if rest >= len(line) || (line[rest] != '<' && line[rest] != '"') {
		return false
	}
	rest++
	start := rest
	for rest < len(line) && line[rest] != '>' && line[rest] != '"' {
		rest++
	}
	if rest == start || rest >= len(line) {
		return false
	}
	return scan.SkipSpace(line, rest+1) == len(line)
}

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

	callbackName, ok := registeredUpdateCallback(content)
	if !ok {
		return false, "", fmt.Sprintf(
			"could not find a pd->system->setUpdateCallback(...) call to identify your update function - " +
				"call mcp_harness_update(pd) yourself as the first line of whatever function you register"), nil
	}

	funcEnd, found := functionBodyStart(content, callbackName)
	funcFile := eventHandlerPath
	if !found {
		// The callback might be defined in a different .c file.
		found, foundContent, ferr := findFunctionInSourceDir(sourceDir, callbackName)
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
		funcEnd, _ = functionBodyStart(content, callbackName)
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
	if scan.CCode(content).Contains(content, "mcp_harness_update(") {
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
	insertAt := funcEnd
	content = content[:insertAt] + "\n" + block + content[insertAt:]

	if err := os.WriteFile(funcFile, []byte(content), 0o644); err != nil {
		return false, "", "", err
	}
	return true, funcFile, "", nil
}

func findFunctionInSourceDir(sourceDir, name string) (path, content string, err error) {
	walkErr := walkCSources(sourceDir, func(p string, b []byte) (bool, error) {
		if _, ok := functionBodyStart(string(b), name); ok {
			path, content = p, string(b)
			return true, nil
		}
		return false, nil
	})
	return path, content, walkErr
}

// findAccessiblePlaydateVar looks for a global/static PlaydateAPI pointer
// declaration in content - the pattern this project's own examples all
// use (g_pd, mc_pd, a file-static "pd", etc.) to reach the API from
// inside a callback that doesn't receive it as a parameter.
//
// A typedef is skipped rather than taken for a declaration: typedef
// PlaydateAPI *PDRef; reads identically to the pattern, and using "PDRef" as
// the argument to mcp_harness_update() produced a file that does not compile,
// with the error landing in the user's own source and nothing pointing back at
// setup as the cause.
func findAccessiblePlaydateVar(content string) (string, bool) {
	// Skipping comments matters in both directions here: a commented-out
	// declaration is not a variable that exists (so passing its name to
	// mcp_harness_update produces a file that does not compile), and a
	// commented-out old name sitting above the live one used to shadow it.
	code := scan.CCode(content)
	for i := 0; ; {
		at := code.TokenIndex(content, "PlaydateAPI", i)
		if at < 0 {
			return "", false
		}
		i = at + len("PlaydateAPI")

		if scan.PrecededByKeyword(content, at, "typedef") {
			continue
		}
		j, ok := scan.Literal(content, scan.SkipSpace(content, i), "*")
		if !ok {
			continue
		}
		name, j := scan.Identifier(content, scan.SkipSpace(content, j))
		if name == "" {
			continue
		}
		// A declaration ends in "=" (initialized) or ";" (not). Anything else
		// is a parameter, an array, a pointer-to-pointer or a cast, none of
		// which is a variable this can pass to mcp_harness_update().
		j = scan.SkipSpace(content, j)
		if j >= len(content) || (content[j] != '=' && content[j] != ';') {
			continue
		}
		return name, true
	}
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
	err = walkCSources(sourceDir, func(p string, b []byte) (bool, error) {
		content, changed := stripMarkerBlocks(string(b), cMarkerBegin, cMarkerEnd)
		if !changed {
			return false, nil
		}
		if writeErr := os.WriteFile(p, []byte(content), 0o644); writeErr != nil {
			return false, writeErr
		}
		result.FilesPatched = append(result.FilesPatched, FileChange{Path: p, Changed: true})
		return false, nil
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
	err := walkCSources(sourceDir, func(p string, b []byte) (bool, error) {
		withoutMarkers, _ := stripMarkerBlocks(string(b), cMarkerBegin, cMarkerEnd)
		// Code only. A comment mentioning the harness is not wiring this tool
		// has to preserve, and reading one as hand-wiring made teardown a
		// silent no-op on a project it could have cleaned up completely.
		code := scan.CCode(withoutMarkers)
		if code.Contains(withoutMarkers, `#include "mcp_harness.h"`) ||
			code.Contains(withoutMarkers, "mcp_harness_init(") ||
			code.Contains(withoutMarkers, "mcp_harness_update(") ||
			// patchInputCalls' replacements are never marker-wrapped (same
			// reasoning as CMakeLists.txt: inline, no clean way to mark a
			// mid-expression substitution) and never reversed - their
			// presence means mcp_harness.c/.h are still needed regardless
			// of whether setup or a human put them there.
			code.Contains(withoutMarkers, "mcp_get_button_state(") ||
			code.Contains(withoutMarkers, "mcp_get_crank_angle(") ||
			code.Contains(withoutMarkers, "mcp_get_crank_change(") ||
			code.Contains(withoutMarkers, "mcp_get_crank_docked(") {
			found = true
			return true, nil
		}
		return false, nil
	})
	return found, err
}

// namesHarnessSource reports whether a CMake argument names the harness source
// file, with or without a directory prefix. Quotes are already off by the time
// scan.FilterCMakeArgs calls this.
//
// Teardown removes the whole argument, prefix included: the harness files are
// about to be deleted, so an argument still naming one is a broken build either
// way, and taking out only the part that reads "src/mcp_harness.c" leaves a
// dangling prefix that CMake refuses outright.
func namesHarnessSource(arg string) bool {
	const name = "mcp_harness.c"
	return arg == name || strings.HasSuffix(arg, "/"+name)
}

func teardownCMakeLists(path string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	content := string(b)
	changed := false

	if strings.Contains(content, "mcp_harness.c") {
		if newContent, removed := scan.FilterCMakeArgs(content, namesHarnessSource); removed {
			content = newContent
			changed = true
		}
	}

	// The include_directories(src) block (see patchCMakeIncludeDirectories)
	// is always marker-wrapped, unlike the source-list entry above, so
	// stripMarkerBlocks alone correctly removes only what setup added -
	// never a hand-written line that happens to read the same way.
	if newContent, stripped := stripMarkerBlocks(content, cmakeMarkerBegin, cmakeMarkerEnd); stripped {
		content = newContent
		changed = true
	}

	if !changed {
		return false, nil
	}
	return true, os.WriteFile(path, []byte(content), 0o644)
}

// walkCSources visits every .c file under sourceDir, handing each one's path and
// contents to fn. Returning true stops the walk.
//
// Five call sites had the same four-clause skip condition written out
// (patchInputCalls, findEventHandler, findFunctionInSourceDir, teardownC,
// cHasUnmarkedHarnessReference), each with its own os.ReadFile and its own
// decision about ignoring a read error. The condition is easy to get subtly
// wrong in one copy - mcp_harness.c has to be excluded or the tool patches its
// own harness - so it lives once, here.
//
// An unreadable file is skipped rather than fatal, matching what every one of
// those call sites already did: a source tree this tool cannot fully read is
// still one it should do its best with, and the ManualSteps in the result are
// how anything it could not handle gets reported.
func walkCSources(sourceDir string, fn func(path string, content []byte) (stop bool, err error)) error {
	return filepath.WalkDir(sourceDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Base(p) == "mcp_harness.c" || !strings.HasSuffix(p, ".c") {
			return err
		}
		b, readErr := os.ReadFile(p)
		if readErr != nil {
			return nil
		}
		stop, fnErr := fn(p, b)
		if fnErr != nil {
			return fnErr
		}
		if stop {
			return fs.SkipAll
		}
		return nil
	})
}
