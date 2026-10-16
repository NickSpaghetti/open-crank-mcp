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

	// Found before patching CMakeLists, since the target that lists this file
	// is the one that gets the harness source.
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
	// Not cmakeChanged: the include_directories patch alone can set it.
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

// The SDK input calls and the wrappers that replace them.
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

// patchInputCalls rewrites SDK input calls project-wide to their mcp_get_*
// wrappers. pd->system is read-only in the Simulator, so injected input only
// reaches the game through the wrappers. Teardown does not reverse this.
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
		// Without a declaration this is an implicit-declaration error under C23.
		patched, _ = insertIncludeIfMissing(patched)
		if writeErr := os.WriteFile(p, []byte(patched), 0o644); writeErr != nil {
			return false, writeErr
		}
		changes = append(changes, FileChange{Path: p, Changed: true})
		return false, nil
	})
	return changes, manualSteps, err
}

// patchInputCallsInContent rewrites every input call it can reach. A receiver
// that is part of a larger expression is skipped; leftoverInputCallSteps reports it.
func patchInputCallsInContent(content string) string {
	for _, pat := range inputCallPatterns {
		// Reclassified per pattern: earlier rewrites moved bytes. Within a pattern
		// it stays valid because rewrites run last-to-first.
		code := scan.CCode(content)
		anchor := "->system->" + pat.sdkCall + "("
		for limit := len(content); ; {
			at := code.LastIndex(content, anchor, limit)
			if at < 0 {
				break
			}
			limit = at
			callEnd := at + len(anchor)
			if !pat.takesArgs {
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
			limit = recvStart
		}
	}
	return content
}

// receiverChainStart walks left from the "->system->" anchor to the start of the
// member-access chain, so game->pd->system->... yields "game->pd".
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

// receiverIsWhole reports whether at begins the whole receiver expression, not
// the tail of foo(x)->pd. "->" is matched whole so "a > pd->..." still counts.
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

// leftoverInputCallSteps reports every input call in code that survived
// patching. Deliberately loose: a false report costs a line of advice, a miss
// leaves press_button/set_crank silently doing nothing.
func leftoverInputCallSteps(path, content string) []string {
	var steps []string
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

// patchCMakeLists adds src/mcp_harness.c to the game's source list and an
// include_directories(src) line.
func patchCMakeLists(path, gameSource string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	content := string(b)
	changed := false

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

// patchCMakeSourceList adds src/mcp_harness.c (unmarked; the filename itself
// is the idempotency key) to the list that compiles gameSource, the eventHandler
// file. An empty gameSource falls back to any list that holds sources.
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
// builds the game. An unresolvable ${VAR} (a GLOB or parent-scope variable)
// counts as yes: a stray harness source beats a game that never answers.
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
			// Nested variables are followed one level, then given the benefit of the doubt.
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

// patchCMakeIncludeDirectories inserts a marker-wrapped include_directories(src)
// after project(...), so sources outside src/ (e.g. the SDK's "Sprite Game")
// can resolve #include "mcp_harness.h".
func patchCMakeIncludeDirectories(content string) (string, bool) {
	// Lowercased: CMake command names are case-insensitive.
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
// PlaydateAPI pointer parameter, which varies between projects.
func eventHandlerParam(content string) (string, bool) {
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

// findEventHandler returns the .c file defining eventHandler and its
// PlaydateAPI parameter name.
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
// after the last #include (or at the top) unless it is already present.
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

// patchEventHandlerInit inserts the #include and mcp_harness_init() call, each
// checked against existing code rather than markers so hand-wired projects
// aren't duplicated. An unfindable init branch is a ManualStep, not an error.
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

// findInitInsertionPoint returns where mcp_harness_init() goes in the kEventInit
// branch (after "case kEventInit:" or the condition's "{"), and whether the
// token exists. A negative offset means there is no block to insert into.
func findInitInsertionPoint(content string) (offset int, found bool) {
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

// registeredUpdateCallback returns the first argument to setUpdateCallback. A
// cast in front of it reports not-found, which becomes a ManualStep.
func registeredUpdateCallback(content string) (string, bool) {
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

// functionBodyStart returns the offset just past the "{" opening the named
// function's definition. Declarations are skipped.
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

// lastIncludeLineEnd returns the end of the last #include line in code. One
// inside a block comment must not count: our /* */ marker would close it early.
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
			// At the "\n", so a CRLF line keeps its "\r".
			end = lineEnd
		}
		i = lineEnd + 1
	}
	return end, found
}

// isIncludeLine reports whether a whole line is nothing but an #include
// directive. Strictness is harmless: a miss just puts our include at the top.
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

// patchUpdateCallback inserts mcp_harness_update() as the first statement of
// the registered update callback. The SDK callback gets no PlaydateAPI pointer,
// so a file-level one must be found, or a ManualStep is reported instead.
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

	// Before the variable search: a pointer declared only in a header would
	// otherwise report a false ManualStep. Not hasMarkerBlock, since the
	// include/init markers may already be in this file.
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

// findAccessiblePlaydateVar returns the name of a file-level PlaydateAPI
// pointer declared in code, skipping typedefs.
func findAccessiblePlaydateVar(content string) (string, bool) {
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
		// Only "=" or ";" ends a declaration; anything else is a parameter, array or cast.
		j = scan.SkipSpace(content, j)
		if j >= len(content) || (content[j] != '=' && content[j] != ';') {
			continue
		}
		return name, true
	}
}

// teardownC is a full no-op if any .c file references the harness outside a
// marker block: the unmarked CMake source entry can't be told apart from a
// hand-written one, and shared libraries link with undefined symbols, so a
// partial teardown builds fine and crashes in the Simulator.
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

// cHasUnmarkedHarnessReference reports whether any .c file references the
// harness in code outside a marker block.
func cHasUnmarkedHarnessReference(sourceDir string) (bool, error) {
	found := false
	err := walkCSources(sourceDir, func(p string, b []byte) (bool, error) {
		withoutMarkers, _ := stripMarkerBlocks(string(b), cMarkerBegin, cMarkerEnd)
		code := scan.CCode(withoutMarkers)
		if code.Contains(withoutMarkers, `#include "mcp_harness.h"`) ||
			code.Contains(withoutMarkers, "mcp_harness_init(") ||
			code.Contains(withoutMarkers, "mcp_harness_update(") ||
			// Input wrappers are inline and unmarked, so they always need the harness.
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

// namesHarnessSource reports whether an unquoted CMake argument names the
// harness source, with or without a directory prefix.
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

	if newContent, stripped := stripMarkerBlocks(content, cmakeMarkerBegin, cmakeMarkerEnd); stripped {
		content = newContent
		changed = true
	}

	if !changed {
		return false, nil
	}
	return true, os.WriteFile(path, []byte(content), 0o644)
}

// walkCSources visits every .c file under sourceDir except the harness itself.
// Returning true stops the walk; unreadable files are skipped.
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
