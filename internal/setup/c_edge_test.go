package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Edge-case tests for the regex-driven C patcher, written ahead of the
// planned port to a hand-written scanner (see docs/ROADMAP.md - "no regex in
// source"). They serve two purposes, and each case says which one it is:
//
//   - LOCK: characterization. The current behavior is correct (or at least
//     acceptable), and the port has to reproduce it exactly. A failure after
//     the port is a regression.
//   - BUG: the current behavior is wrong, and the test asserts the wrong
//     behavior on purpose so the port stays byte-identical through steps 1-5
//     of the plan. The comment names what the right answer is. Step 6
//     (comment/string awareness) flips these deliberately, one at a time.
//
// The BUG cases all come from the same root cause: a regex sees a .c file as
// a flat byte string, so a match inside a comment or a string literal is
// indistinguishable from real code, and any real code whose spacing differs
// from the pattern is invisible. Both directions do damage - a false match
// rewrites something that was never code, and a missed match means
// press_button/set_crank silently do nothing at runtime with no diagnostic.

// ---------------------------------------------------------------------------
// setupC end to end - the shapes step 7 changed the outcome for
// ---------------------------------------------------------------------------

// TestSetupCSwitchBasedHandler covers the worst of the old failures at the
// level the user actually sees it. A handler written as a switch used to fail
// the whole setup call, after the harness files had been copied and
// CMakeLists.txt patched - a half-applied state the user then had to undo by
// hand. It is an ordinary way to write the handler, so it has to work.
func TestSetupCSwitchBasedHandler(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "CMakeLists.txt"),
		"cmake_minimum_required(VERSION 3.14)\nproject(Game C)\nadd_library(game SHARED src/main.c)\n")
	mustWrite(t, filepath.Join(dir, "src", "main.c"),
		"#include \"pd_api.h\"\n\n"+
			"static PlaydateAPI *pd = NULL;\n\n"+
			"static int update(void *ud) {\n\treturn 1;\n}\n\n"+
			"int eventHandler(PlaydateAPI *playdate, PDSystemEvent event, uint32_t arg) {\n"+
			"\tswitch (event) {\n"+
			"\tcase kEventInit:\n"+
			"\t\tpd = playdate;\n"+
			"\t\tpd->system->setUpdateCallback(update, NULL);\n"+
			"\t\tbreak;\n"+
			"\tdefault:\n\t\tbreak;\n\t}\n\treturn 0;\n}\n")

	result, err := setupC(dir, testHarnessFS())
	if err != nil {
		t.Fatalf("setupC: %v", err)
	}
	if len(result.ManualSteps) != 0 {
		t.Errorf("ManualSteps = %q, want none", result.ManualSteps)
	}

	// Every patched file is reported, and every reported entry names a real
	// file - an empty path used to be possible when the update callback could
	// not be located.
	patched := map[string]bool{}
	for _, change := range result.FilesPatched {
		if change.Path == "" {
			t.Errorf("FilesPatched contains an entry with no path: %+v", result.FilesPatched)
		}
		patched[change.Path] = true
	}
	for _, want := range []string{
		filepath.Join(dir, "CMakeLists.txt"),
		filepath.Join(dir, "src", "main.c"),
	} {
		if !patched[want] {
			t.Errorf("FilesPatched does not mention %s: %+v", want, result.FilesPatched)
		}
	}

	main := mustRead(t, filepath.Join(dir, "src", "main.c"))
	for _, want := range []string{
		`#include "mcp_harness.h"`,
		"mcp_harness_init(playdate);",
		"mcp_harness_update(pd);",
	} {
		if !strings.Contains(main, want) {
			t.Errorf("main.c missing %q:\n%s", want, main)
		}
	}
	// The init call belongs to the case body, after its label.
	initAt := strings.Index(main, "mcp_harness_init(")
	labelAt := strings.Index(main, "case kEventInit:")
	assignAt := strings.Index(main, "pd = playdate;")
	if !(labelAt < initAt && initAt < assignAt) {
		t.Errorf("want the init call between the case label and the case body:\n%s", main)
	}
	if !strings.Contains(mustRead(t, filepath.Join(dir, "CMakeLists.txt")), "src/mcp_harness.c") {
		t.Error("CMakeLists.txt was not given the harness source")
	}
}

// TestSetupCOnTheSDKTemplateShape runs setup against this repo's own contract
// fixture, which is the SDK's project template: two targets (a device
// executable and a simulator library) built from the same sources, wrapped in
// if/else, alongside an execute_process(...) call whose argument list contains
// nested parens and quoted strings. It is the shape every Playdate C project
// starts from, and the one the target rule and the paren counting have to get
// right.
func TestSetupCOnTheSDKTemplateShape(t *testing.T) {
	template, err := os.ReadFile(filepath.Join("..", "..", "c-harness", "test", "fixture-game", "CMakeLists.txt"))
	if err != nil {
		t.Fatalf("reading the contract fixture's CMakeLists.txt: %v", err)
	}
	// The committed fixture is already wired. Un-wire it, so this starts where
	// a user's own project would.
	unwired := strings.ReplaceAll(string(template), " src/mcp_harness.c", "")
	if strings.Contains(unwired, "mcp_harness.c") {
		t.Fatalf("could not un-wire the fixture, it still references the harness:\n%s", unwired)
	}

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "CMakeLists.txt"), unwired)
	mustWrite(t, filepath.Join(dir, "src", "main.c"),
		"static PlaydateAPI *pd = NULL;\n"+
			"static int update(void *ud) {\n\treturn 1;\n}\n"+
			"int eventHandler(PlaydateAPI *playdate, PDSystemEvent event, uint32_t arg) {\n"+
			"\tif (event == kEventInit) {\n\t\tpd = playdate;\n"+
			"\t\tpd->system->setUpdateCallback(update, NULL);\n\t}\n\treturn 0;\n}\n")

	result, err := setupC(dir, testHarnessFS())
	if err != nil {
		t.Fatalf("setupC: %v", err)
	}
	if len(result.ManualSteps) != 0 {
		t.Errorf("ManualSteps = %q, want none", result.ManualSteps)
	}

	cmake := mustRead(t, filepath.Join(dir, "CMakeLists.txt"))
	// Both targets build the game from the same sources, so both need it.
	if n := strings.Count(cmake, "src/mcp_harness.c"); n != 2 {
		t.Errorf("src/mcp_harness.c appears %d times, want 2 (device and simulator targets):\n%s", n, cmake)
	}
	if !strings.Contains(cmake, "include_directories(src)") {
		t.Errorf("include_directories(src) missing:\n%s", cmake)
	}
	// The SDK's own execute_process(...) block has nested parens and quoted
	// arguments; nothing in it should have been touched.
	if !strings.Contains(cmake, "COMMAND head -n 1") {
		t.Errorf("the execute_process block was damaged:\n%s", cmake)
	}
}

// TestSetupCSkippedUpdatePatchReportsNoFile covers the reporting side of an
// update callback setup declines to patch: the ManualStep is what the user acts
// on, and FilesPatched must not gain an entry naming nothing.
func TestSetupCSkippedUpdatePatchReportsNoFile(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "CMakeLists.txt"),
		"project(Game C)\nadd_library(game SHARED src/main.c)\n")
	// The cast in front of the callback is a form this tool does not read, so
	// there is no update function to patch and no file to report.
	mustWrite(t, filepath.Join(dir, "src", "main.c"),
		"static PlaydateAPI *pd = NULL;\n"+
			"static int update(void *ud) {\n\treturn 1;\n}\n"+
			"int eventHandler(PlaydateAPI *playdate, PDSystemEvent event, uint32_t arg) {\n"+
			"\tif (event == kEventInit) {\n\t\tpd = playdate;\n"+
			"\t\tpd->system->setUpdateCallback((PDCallbackFunction *)update, NULL);\n\t}\n\treturn 0;\n}\n")

	result, err := setupC(dir, testHarnessFS())
	if err != nil {
		t.Fatalf("setupC: %v", err)
	}

	var reported bool
	for _, step := range result.ManualSteps {
		if strings.Contains(step, "setUpdateCallback") {
			reported = true
		}
	}
	if !reported {
		t.Errorf("ManualSteps = %q, want one about setUpdateCallback", result.ManualSteps)
	}
	for _, change := range result.FilesPatched {
		if change.Path == "" {
			t.Errorf("FilesPatched gained an entry with no path: %+v", result.FilesPatched)
		}
	}
	if strings.Contains(mustRead(t, filepath.Join(dir, "src", "main.c")), "mcp_harness_update(") {
		t.Error("an update call was inserted despite reporting a manual step")
	}
}

// TestSetupCPicksTheGameTargetOverATool checks the target rule end to end: the
// harness goes into the target that builds the game and nowhere else. It used
// to go into every add_library/add_executable in the file, which meant a
// host-side tool target got a source that needs pd_api.h on an include path it
// does not have, and an INTERFACE library got a source argument that CMake
// rejects outright.
func TestSetupCPicksTheGameTargetOverATool(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "CMakeLists.txt"),
		"cmake_minimum_required(VERSION 3.14)\n"+
			"project(Game C)\n"+
			"add_library(vendor_iface INTERFACE)\n"+
			"add_executable(atlasgen tools/atlasgen.c)\n"+
			"add_library(game SHARED src/main.c src/entity.c)\n")
	mustWrite(t, filepath.Join(dir, "tools", "atlasgen.c"), "int main(void) { return 0; }\n")
	mustWrite(t, filepath.Join(dir, "src", "entity.c"), "void entity_tick(void) {}\n")
	mustWrite(t, filepath.Join(dir, "src", "main.c"),
		"#include \"pd_api.h\"\n\n"+
			"static PlaydateAPI *pd = NULL;\n\n"+
			"static int update(void *ud) {\n\treturn 1;\n}\n\n"+
			"int eventHandler(PlaydateAPI *playdate, PDSystemEvent event, uint32_t arg) {\n"+
			"\tif (event == kEventInit) {\n\t\tpd = playdate;\n"+
			"\t\tpd->system->setUpdateCallback(update, NULL);\n\t}\n\treturn 0;\n}\n")

	if _, err := setupC(dir, testHarnessFS()); err != nil {
		t.Fatalf("setupC: %v", err)
	}

	cmake := mustRead(t, filepath.Join(dir, "CMakeLists.txt"))
	if n := strings.Count(cmake, "src/mcp_harness.c"); n != 1 {
		t.Fatalf("src/mcp_harness.c appears %d times, want 1:\n%s", n, cmake)
	}
	if !strings.Contains(cmake, "add_library(game SHARED src/main.c src/entity.c src/mcp_harness.c)") {
		t.Errorf("the game target did not get the harness source:\n%s", cmake)
	}
	if !strings.Contains(cmake, "add_library(vendor_iface INTERFACE)") {
		t.Errorf("the INTERFACE library was modified:\n%s", cmake)
	}
	if !strings.Contains(cmake, "add_executable(atlasgen tools/atlasgen.c)") {
		t.Errorf("the tool target was modified:\n%s", cmake)
	}
}

// TestSetupCReportsInputCallsItCannotRewrite covers the other half of step 7:
// an input call the patterns can't reach is reported with its file and line
// instead of being left silent. Silence there is the tool's worst failure mode
// - press_button and set_crank do nothing, setup says it succeeded, and
// nothing in the game's own output points at the cause.
func TestSetupCReportsInputCallsItCannotRewrite(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "CMakeLists.txt"),
		"project(Game C)\nadd_library(game SHARED src/main.c src/input.c)\n")
	mustWrite(t, filepath.Join(dir, "src", "main.c"),
		"static PlaydateAPI *pd = NULL;\n"+
			"static int update(void *ud) {\n\treturn 1;\n}\n"+
			"int eventHandler(PlaydateAPI *playdate, PDSystemEvent event, uint32_t arg) {\n"+
			"\tif (event == kEventInit) {\n\t\tpd = playdate;\n"+
			"\t\tpd->system->setUpdateCallback(update, NULL);\n\t}\n\treturn 0;\n}\n")
	mustWrite(t, filepath.Join(dir, "src", "input.c"),
		"#include \"pd_api.h\"\n"+
			"void read_input(Game *game) {\n"+
			"\tPDButtons c;\n"+
			"\tgame->pd->system->getButtonState(&c, NULL, NULL);\n"+
			"\tfloat a = api()->pd->system->getCrankAngle();\n"+
			"}\n")

	result, err := setupC(dir, testHarnessFS())
	if err != nil {
		t.Fatalf("setupC: %v", err)
	}

	input := mustRead(t, filepath.Join(dir, "src", "input.c"))
	if !strings.Contains(input, "mcp_get_button_state(game->pd, &c, NULL, NULL)") {
		t.Errorf("the struct-held receiver was not rewritten whole:\n%s", input)
	}

	var reported string
	for _, step := range result.ManualSteps {
		if strings.Contains(step, "getCrankAngle") {
			reported = step
		}
	}
	if reported == "" {
		t.Fatalf("no ManualStep for the call that could not be rewritten: %q", result.ManualSteps)
	}
	if !strings.Contains(reported, "input.c:5") {
		t.Errorf("ManualStep = %q, want it to name input.c line 5", reported)
	}
}

// ---------------------------------------------------------------------------
// patchInputCalls: the receiver chain and the three crank calls
// ---------------------------------------------------------------------------

// patchInputCallsOn runs patchInputCalls over a source tree holding a single
// file and returns its resulting content. name is relative to the tree root
// so a case can exercise walkCSources' extension filter.
func patchInputCallsOn(t *testing.T, name, src string) (content string, changed bool, steps []string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, filepath.FromSlash(name))
	mustWrite(t, path, src)
	changes, steps, err := patchInputCalls(dir)
	if err != nil {
		t.Fatalf("patchInputCalls: %v", err)
	}
	return mustRead(t, path), len(changes) == 1 && changes[0].Changed, steps
}

func TestPatchInputCallsEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		file        string
		src         string
		wantChanged bool
		wantHas     []string
		wantLacks   []string
		// wantSteps is the number of ManualSteps the file should produce,
		// i.e. input calls left unrewritten and reported rather than left
		// silent.
		wantSteps int
	}{
		{
			// LOCK (fixed in step 6): a call named in a line comment is not
			// code, so nothing is rewritten and nothing is reported - a
			// comment describing the raw call is not a call this tool failed
			// to reach.
			name:        "call_in_line_comment_is_left_alone",
			src:         "void f(void) {\n\t// pd->system->getCrankAngle() is the raw call\n}\n",
			wantChanged: false,
			wantHas:     []string{"// pd->system->getCrankAngle() is the raw call"},
		},
		{
			// LOCK (fixed in step 6): same, inside a block comment.
			name:        "call_in_block_comment_is_left_alone",
			src:         "/*\n * Reads input via pd->system->getCrankChange().\n */\nvoid f(void) {}\n",
			wantChanged: false,
			wantHas:     []string{"* Reads input via pd->system->getCrankChange()."},
		},
		{
			// LOCK (fixed in step 6): the literal is data the game prints, not
			// code. Rewriting it left the code correct and the log message a
			// lie, which is the case that made comment/string awareness worth
			// doing rather than shrugging at.
			name:        "call_in_string_literal_is_left_alone",
			src:         "void f(void) {\n\tpd->system->logToConsole(\"pd->system->getCrankAngle() returned %f\", a);\n}\n",
			wantChanged: false,
			wantHas:     []string{`"pd->system->getCrankAngle() returned %f"`},
		},
		{
			// BUG (miss), now reported: C allows whitespace around ->, and
			// the patterns have no \s* between their tokens. The call is
			// still not rewritten - that waits for the port, which gets
			// whitespace tolerance for free - but it is no longer silent.
			name:        "spaces_around_arrows_are_missed_but_reported",
			src:         "void f(void) {\n\tfloat a = pd -> system -> getCrankAngle();\n}\n",
			wantChanged: false,
			wantSteps:   1,
		},
		{
			// BUG (miss), now reported: same defect, split across lines -
			// the shape clang-format produces on a long line.
			name:        "arrow_split_across_lines_is_missed_but_reported",
			src:         "void f(void) {\n\tPDButtons c;\n\tpd->system->\n\t\tgetButtonState(&c, NULL, NULL);\n}\n",
			wantChanged: false,
			wantSteps:   1,
		},
		{
			// BUG (miss), now reported: the three crank patterns hardcode
			// "()" with no room for whitespace.
			name:        "space_inside_empty_parens_is_missed_but_reported",
			src:         "void f(void) {\n\tfloat a = pd->system->getCrankAngle( );\n}\n",
			wantChanged: false,
			wantSteps:   1,
		},
		{
			// LOCK (fixed in step 7): the receiver is a whole member-access
			// chain now, so a game that reaches the API through a struct gets
			// mcp_get_button_state(game->pd, ...). It used to capture only
			// "pd" and leave game->mcp_get_button_state(pd, ...) behind,
			// which does not compile.
			name:        "member_chain_receiver_is_rewritten_whole",
			src:         "void f(Game *game) {\n\tPDButtons c;\n\tgame->pd->system->getButtonState(&c, NULL, NULL);\n}\n",
			wantChanged: true,
			wantHas:     []string{"mcp_get_button_state(game->pd, &c, NULL, NULL)"},
			wantLacks:   []string{"game->mcp_get_button_state"},
		},
		{
			// LOCK: a dotted chain works the same way.
			name:        "dotted_chain_receiver_is_rewritten_whole",
			src:         "void f(Game *game) {\n\tfloat a = game->state.pd->system->getCrankAngle();\n}\n",
			wantChanged: true,
			wantHas:     []string{"mcp_get_crank_angle(game->state.pd)"},
		},
		{
			// LOCK: a receiver this tool cannot rewrite (a call in the chain)
			// is left alone and reported, rather than half-rewritten into
			// something that does not compile.
			name:        "call_in_the_receiver_chain_is_left_alone_and_reported",
			src:         "void f(void) {\n\tPDButtons c;\n\tapi()->pd->system->getButtonState(&c, NULL, NULL);\n}\n",
			wantChanged: false,
			wantSteps:   1,
		},
		{
			// LOCK: a greater-than immediately before the receiver is not
			// mistaken for the "->" of a longer chain.
			name:        "greater_than_before_the_receiver_is_not_a_chain",
			src:         "void f(void) {\n\tif (a>pd->system->getCrankAngle()) { return; }\n}\n",
			wantChanged: true,
			wantHas:     []string{"if (a>mcp_get_crank_angle(pd))"},
		},
		{
			// LOCK: an alias macro is genuinely out of scope - resolving it
			// needs a preprocessor, not a better scanner. Left unpatched,
			// same as today.
			name:        "macro_alias_for_system_is_out_of_scope",
			src:         "#define SYS pd->system\nvoid f(void) {\n\tfloat a = SYS->getCrankAngle();\n}\n",
			wantChanged: false,
		},
		{
			// LOCK: walkCSources filters on ".c", so a C++ translation unit
			// is skipped. The Playdate C API is C, and this project has no
			// C++ example, so this is deliberate scope, not a defect - but
			// the port must keep the same extension filter.
			name:        "cpp_file_is_not_walked",
			file:        "src/game.cpp",
			src:         "void f(void) {\n\tfloat a = pd->system->getCrankAngle();\n}\n",
			wantChanged: false,
		},
		{
			// LOCK: headers are skipped too, so an input call in a static
			// inline function in a .h stays unpatched. Worth knowing about
			// (it is a real silent-miss shape) but changing it is a scope
			// decision, not part of the port.
			name:        "header_file_is_not_walked",
			file:        "src/input.h",
			src:         "static inline float angle(void) {\n\treturn pd->system->getCrankAngle();\n}\n",
			wantChanged: false,
		},
		{
			// LOCK: CRLF sources (a game authored on Windows) patch fine
			// today because the pattern never touches line endings. A
			// line-oriented scanner is where this breaks, so it is pinned
			// here: the patched line keeps its \r and no \n is introduced
			// mid-line.
			name:        "crlf_source_is_patched",
			src:         "#include <stdio.h>\r\nvoid f(void) {\r\n\tfloat a = pd->system->getCrankAngle();\r\n}\r\n",
			wantChanged: true,
			wantHas:     []string{"float a = mcp_get_crank_angle(pd);\r\n"},
		},
		{
			// LOCK: every occurrence is replaced, not just the first.
			name:        "all_occurrences_in_one_file_are_replaced",
			src:         "void a(void) { float x = pd->system->getCrankAngle(); }\nvoid b(void) { float y = pd->system->getCrankAngle(); }\n",
			wantChanged: true,
			wantLacks:   []string{"->system->"},
		},
		{
			// LOCK: two different API pointer names in one file each keep
			// their own receiver.
			name:        "distinct_receivers_are_kept_per_call",
			src:         "void a(void) { float x = pd->system->getCrankAngle(); }\nvoid b(void) { float y = playdate->system->getCrankAngle(); }\n",
			wantChanged: true,
			wantHas:     []string{"mcp_get_crank_angle(pd)", "mcp_get_crank_angle(playdate)"},
		},
		{
			// LOCK: a patched file that had no mcp_harness.h include gets
			// one, so the mcp_get_* calls are declared. Regression guard for
			// the implicit-declaration bug noted in patchInputCalls.
			name:        "patched_file_gains_the_harness_include",
			src:         "#include <stdio.h>\nvoid f(void) { float a = pd->system->getCrankAngle(); }\n",
			wantChanged: true,
			wantHas:     []string{`#include "mcp_harness.h"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := tt.file
			if file == "" {
				file = "src/game.c"
			}
			got, changed, steps := patchInputCallsOn(t, file, tt.src)
			if changed != tt.wantChanged {
				t.Errorf("changed = %v, want %v\ngot content:\n%s", changed, tt.wantChanged, got)
			}
			if len(steps) != tt.wantSteps {
				t.Errorf("got %d manual steps, want %d: %q", len(steps), tt.wantSteps, steps)
			}
			if !tt.wantChanged && got != tt.src {
				t.Errorf("content changed but should not have:\n%s", got)
			}
			for _, want := range tt.wantHas {
				if !strings.Contains(got, want) {
					t.Errorf("content missing %q:\n%s", want, got)
				}
			}
			for _, unwanted := range tt.wantLacks {
				if strings.Contains(got, unwanted) {
					t.Errorf("content still has %q:\n%s", unwanted, got)
				}
			}
		})
	}
}

// TestPatchInputCallsPortHazards pins the details of Go's regexp semantics
// that a hand-written scanner is most likely to get subtly different. None of
// these is a bug in the current code - they exist so the port has to make the
// same choices deliberately instead of discovering them from a bug report.
func TestPatchInputCallsPortHazards(t *testing.T) {
	tests := []struct {
		name        string
		src         string
		wantChanged bool
		wantHas     []string
		wantSteps   int
	}{
		{
			// Go's \w is ASCII-only: [0-9A-Za-z_]. C23 and GCC's extensions
			// allow non-ASCII identifiers, and this call is not patched
			// today (it is reported instead). A scanner reading identifiers
			// with unicode.IsLetter would start matching it - a change in
			// behavior, not a fix, and not one to make by accident.
			name:        "non_ascii_identifier_is_not_matched",
			src:         "void f(void) {\n\tfloat a = pdé->system->getCrankAngle();\n}\n",
			wantChanged: false,
			wantSteps:   1,
		},
		{
			// Matching is case-sensitive. A scanner doing a
			// case-insensitive compare on the member names would match
			// this. C is case-sensitive, so staying strict is correct.
			name:        "member_names_are_case_sensitive",
			src:         "void f(void) {\n\tfloat a = pd->System->GetCrankAngle();\n}\n",
			wantChanged: false,
		},
		{
			// Offsets are byte offsets. A port that indexes by rune, or
			// rebuilds the file from decoded runes, corrupts the multi-byte
			// text before the match - so the comment is asserted intact
			// alongside the patch.
			name:        "multibyte_text_before_a_call_survives_intact",
			src:         "void f(void) {\n\t// ángulo del cigüeñal · ✓\n\tfloat a = pd->system->getCrankAngle();\n}\n",
			wantChanged: true,
			wantHas: []string{
				"// ángulo del cigüeñal · ✓",
				"float a = mcp_get_crank_angle(pd);",
			},
		},
		{
			// Two matches in one expression, replaced left to right and
			// non-overlapping. A scanner that restarts from the start of the
			// rewritten text after each edit either loops or double-patches.
			name:        "two_calls_in_one_expression_are_both_replaced",
			src:         "void f(void) {\n\tfloat d = pd->system->getCrankAngle() - pd->system->getCrankChange();\n}\n",
			wantChanged: true,
			wantHas:     []string{"float d = mcp_get_crank_angle(pd) - mcp_get_crank_change(pd);"},
		},
		{
			// The replacement text itself contains no ->system->, so a
			// scanner that rescans its own output still terminates with the
			// same result. Pinned as the idempotency property the port needs.
			name:        "replacement_output_is_not_itself_a_match",
			src:         "void f(void) {\n\tfloat a = mcp_get_crank_angle(pd);\n}\n",
			wantChanged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed, steps := patchInputCallsOn(t, "src/game.c", tt.src)
			if changed != tt.wantChanged {
				t.Errorf("changed = %v, want %v\ngot:\n%s", changed, tt.wantChanged, got)
			}
			if len(steps) != tt.wantSteps {
				t.Errorf("got %d manual steps, want %d: %q", len(steps), tt.wantSteps, steps)
			}
			if !tt.wantChanged && got != tt.src {
				t.Errorf("content changed but should not have:\n%s", got)
			}
			for _, want := range tt.wantHas {
				if !strings.Contains(got, want) {
					t.Errorf("content missing %q:\n%s", want, got)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// insertIncludeIfMissing: (?m)^#include\s*[<"][^>"]+[>"]\s*$
// ---------------------------------------------------------------------------

func TestInsertIncludeIfMissingEdgeCases(t *testing.T) {
	const harnessInclude = `#include "mcp_harness.h"`

	tests := []struct {
		name        string
		src         string
		wantChanged bool
		// wantAfter, when set, is the substring the harness include must
		// come after. Empty means it must be at the very start.
		wantAfter string
		// wantStray asserts the insertion left the outer block comment's
		// own "*/" stranded outside any comment.
		wantStray bool
	}{
		{
			// BUG (miss): a trailing comment on the last include defeats
			// \s*$, so that line is not seen as an include and the harness
			// include is prepended to the top of the file instead of joining
			// the include block. Cosmetic here, but the same \s*$ strictness
			// is what breaks projectLineRe below, where it is not cosmetic.
			name:        "include_with_trailing_comment_is_not_seen",
			src:         "#include <stdio.h> // c stdlib\nvoid f(void) {}\n",
			wantChanged: true,
			wantAfter:   "",
		},
		{
			// BUG (miss): ^ anchors at column 0, so an indented include
			// (inside an #ifdef block, commonly) is not seen.
			name:        "indented_include_is_not_seen",
			src:         "#ifdef _WIN32\n\t#include <windows.h>\n#endif\nvoid f(void) {}\n",
			wantChanged: true,
			wantAfter:   "",
		},
		{
			// BUG (miss): "# include" with a space is legal C and not seen.
			name:        "hash_space_include_is_not_seen",
			src:         "#  include <stdio.h>\nvoid f(void) {}\n",
			wantChanged: true,
			wantAfter:   "",
		},
		{
			// LOCK: a commented-out include earlier in the file is harmless,
			// because the LAST match wins and that is the real one. Pinned
			// so the port keeps last-match-wins rather than first.
			name:        "commented_include_before_a_real_one_is_harmless",
			src:         "/* legacy:\n#include <old.h>\n*/\n#include <stdio.h>\nvoid f(void) {}\n",
			wantChanged: true,
			wantAfter:   "#include <stdio.h>",
		},
		{
			// LOCK (fixed in step 6): an include inside a block comment is
			// skipped, so the insertion joins the real include block instead
			// of landing in the comment. It used to land inside it, where the
			// marker block's own "*/" closed the outer comment early and left
			// the original "*/" stranded as a stray token - a hard compile
			// error pointing at a line the user did not write.
			name:        "commented_include_as_last_match_is_skipped",
			src:         "#include <stdio.h>\n/* legacy:\n#include <old.h>\n*/\nvoid f(void) {}\n",
			wantChanged: true,
			wantAfter:   "#include <stdio.h>",
		},
		{
			// LOCK: CRLF include block is matched (\r is absorbed by \s*)
			// and the insertion lands after the last include. A line-based
			// port must not regress this.
			name:        "crlf_include_block_is_matched",
			src:         "#include <stdio.h>\r\n#include \"pd_api.h\"\r\n\r\nvoid f(void) {}\r\n",
			wantChanged: true,
			wantAfter:   `#include "pd_api.h"`,
		},
		{
			// LOCK: the last include wins, not the first.
			name:        "insertion_follows_the_last_include",
			src:         "#include <stdio.h>\n#include <stdlib.h>\n#include \"pd_api.h\"\nvoid f(void) {}\n",
			wantChanged: true,
			wantAfter:   `#include "pd_api.h"`,
		},
		{
			// LOCK: no includes at all means prepend.
			name:        "no_includes_means_prepend",
			src:         "void f(void) {}\n",
			wantChanged: true,
			wantAfter:   "",
		},
		{
			// LOCK: idempotent against the literal text, markers or not.
			name:        "already_present_is_a_no_op",
			src:         "#include \"mcp_harness.h\"\nvoid f(void) {}\n",
			wantChanged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := insertIncludeIfMissing(tt.src)
			if changed != tt.wantChanged {
				t.Fatalf("changed = %v, want %v\ngot:\n%s", changed, tt.wantChanged, got)
			}
			if !tt.wantChanged {
				if got != tt.src {
					t.Fatalf("content changed on a no-op:\n%s", got)
				}
				return
			}
			at := strings.Index(got, harnessInclude)
			if at < 0 {
				t.Fatalf("harness include missing:\n%s", got)
			}
			if tt.wantStray && !strings.Contains(got, cMarkerEnd+"\n\n*/") {
				t.Errorf("want a stray */ left after the marker block:\n%s", got)
			}
			if tt.wantAfter == "" {
				if at != 0 && !strings.HasPrefix(got, cMarkerBegin) {
					t.Errorf("want harness include at the top, got index %d:\n%s", at, got)
				}
				return
			}
			anchor := strings.Index(got, tt.wantAfter)
			if anchor < 0 {
				t.Fatalf("anchor %q missing:\n%s", tt.wantAfter, got)
			}
			if at < anchor {
				t.Errorf("want harness include after %q, got it before:\n%s", tt.wantAfter, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// patchCMakeSourceList: setBlockRe and addCallRe
// ---------------------------------------------------------------------------

func TestPatchCMakeSourceListEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		src  string
		// gameSource is the project-relative path of the file defining
		// eventHandler, which is how the game's own target is told apart from
		// anything else in the file. Empty exercises the fallback for a
		// project where it could not be found.
		gameSource  string
		wantChanged bool
		wantHas     []string
		wantLacks   []string
		wantRefs    int // occurrences of src/mcp_harness.c, when it matters
	}{
		{
			// LOCK: a single-line set() does not match setBlockRe (which
			// requires a newline after the variable name), so the harness
			// lands in the add_library argument list instead. Different
			// placement from the multi-line case, same working build. The
			// port must make the same choice, including leaving the set()
			// line alone.
			name:        "single_line_set_falls_through_to_add_library",
			src:         "set(GAME_SOURCES src/main.c)\nadd_library(${NAME} SHARED ${GAME_SOURCES})\n",
			wantChanged: true,
			wantHas:     []string{"set(GAME_SOURCES src/main.c)", "${GAME_SOURCES} src/mcp_harness.c)"},
			wantRefs:    1,
		},
		{
			// LOCK: same fall-through when the closing paren shares the last
			// source's line - setBlockRe needs \n before it.
			name:        "closing_paren_on_source_line_falls_through",
			src:         "set(GAME_SOURCES\n\tsrc/main.c)\nadd_library(${NAME} SHARED ${GAME_SOURCES})\n",
			wantChanged: true,
			wantHas:     []string{"${GAME_SOURCES} src/mcp_harness.c)"},
			wantRefs:    1,
		},
		{
			// LOCK (fixed in step 7): every multi-line set() block is
			// considered, and the one holding the game's sources wins. Only
			// the first block used to be looked at, so a project listing
			// something else multi-line first (compiler flags here) abandoned
			// the set() path entirely.
			name: "source_set_block_is_found_past_a_non_source_one",
			src: "set(CMAKE_C_FLAGS\n\t-Wall\n)\n" +
				"set(GAME_SOURCES\n\tsrc/main.c\n)\n" +
				"add_library(${NAME} SHARED ${GAME_SOURCES})\n",
			wantChanged: true,
			wantHas:     []string{"src/main.c\n\tsrc/mcp_harness.c\n)"},
			wantLacks:   []string{"-Wall\n\tsrc/mcp_harness.c"},
			wantRefs:    1,
		},
		{
			// LOCK (fixed by the port): a comment containing parentheses
			// inside the argument list used to defeat the [^()]* character
			// class, so nothing matched and setup reported a ManualStep for a
			// perfectly ordinary CMakeLists. Counting parens handles it.
			name:        "parens_in_an_arg_list_comment_are_handled",
			src:         "add_library(${NAME} SHARED\n\tsrc/main.c  # (entry point)\n)\n",
			wantChanged: true,
			wantHas:     []string{"# (entry point)\n src/mcp_harness.c)"},
			wantRefs:    1,
		},
		{
			// LOCK (fixed by the port): same class - a quoted path containing
			// parens.
			name:        "parens_inside_a_quoted_path_are_handled",
			src:         "add_library(${NAME} SHARED src/main.c \"src/my (old) file.c\")\n",
			wantChanged: true,
			wantHas:     []string{"\"src/my (old) file.c\" src/mcp_harness.c)"},
			wantRefs:    1,
		},
		{
			// LOCK: a generator expression is matched, because $<...> uses
			// angle brackets - no parens for [^()]* to trip over. Worth
			// pinning so a port's balanced-paren scanner does not start
			// treating $< > as nesting.
			name:        "generator_expression_in_arg_list_is_matched",
			src:         "add_library(${NAME} SHARED src/main.c $<$<BOOL:${DEBUG}>:src/debug.c>)\n",
			wantChanged: true,
			wantHas:     []string{"$<$<BOOL:${DEBUG}>:src/debug.c> src/mcp_harness.c)"},
			wantRefs:    1,
		},
		{
			// LOCK: every add_library/add_executable gets the entry, which
			// is what the SDK template needs (a simulator .so and a device
			// executable built from the same sources). Reverse iteration
			// keeps the offsets valid - assert both, in order.
			name: "all_targets_get_the_entry",
			src: "add_library(${NAME} SHARED src/main.c)\n" +
				"add_executable(${NAME}_device src/main.c)\n",
			wantChanged: true,
			wantHas: []string{
				"add_library(${NAME} SHARED src/main.c src/mcp_harness.c)",
				"add_executable(${NAME}_device src/main.c src/mcp_harness.c)",
			},
			wantRefs: 2,
		},
		{
			// LOCK (fixed in step 7): only the target that builds the game
			// gets the entry. An INTERFACE library used to get a source
			// argument, which CMake rejects outright, and an unrelated tool
			// target used to get a harness source that needs pd_api.h on an
			// include path it does not have.
			name: "unrelated_and_interface_targets_are_skipped",
			src: "add_library(vendor_iface INTERFACE)\n" +
				"if(BUILD_TOOLS)\n\tadd_executable(gen src/gen.c)\nendif()\n" +
				"add_library(game SHARED src/main.c)\n",
			wantChanged: true,
			wantHas: []string{
				"add_library(vendor_iface INTERFACE)",
				"add_executable(gen src/gen.c)",
				"add_library(game SHARED src/main.c src/mcp_harness.c)",
			},
			wantRefs: 1,
		},
		{
			// LOCK: with no gameSource to go on, the looser rule applies -
			// any target that already holds sources gets the entry - but a
			// target with no sources at all is still skipped, because a
			// source on an INTERFACE library is a hard CMake error rather
			// than a guess that might pay off.
			name: "without_a_game_source_every_source_bearing_target_is_patched",
			src: "add_library(vendor_iface INTERFACE)\n" +
				"add_executable(gen src/gen.c)\n" +
				"add_library(game SHARED src/main.c)\n",
			gameSource:  "-",
			wantChanged: true,
			wantHas:     []string{"add_library(vendor_iface INTERFACE)"},
			wantRefs:    2,
		},
		{
			// LOCK: a source list this tool cannot resolve - file(GLOB ...),
			// or a variable set in a parent scope - counts as the game's.
			// Refusing to patch there would break projects that build fine
			// today, and the cost of being wrong is one extra source in a
			// target rather than a game that never answers the harness.
			name:        "unresolvable_source_variable_is_assumed_to_be_the_game",
			src:         "file(GLOB GAME_SRC src/*.c)\nadd_library(game SHARED ${GAME_SRC})\n",
			wantChanged: true,
			wantRefs:    1,
		},
		{
			// LOCK: a variable that IS resolvable and holds someone else's
			// sources does not count.
			name:        "resolvable_variable_holding_other_sources_is_skipped",
			src:         "set(TOOL_SRC\n\tsrc/gen.c\n)\nadd_executable(gen ${TOOL_SRC})\n",
			wantChanged: false,
		},
		{
			// LOCK: a source list one level further indirect is followed once
			// and then given the benefit of the doubt, so the target still
			// gets the entry. The set() block itself is left alone - appending
			// a source to a list that might be compiler flags is the one
			// mistake in this area that is not recoverable.
			name: "doubly_indirect_source_list_still_patches_the_target",
			src: "set(GAME_SOURCES\n\t${COMMON_SRC}\n)\n" +
				"add_library(game SHARED ${GAME_SOURCES})\n",
			wantChanged: true,
			wantHas:     []string{"${GAME_SOURCES} src/mcp_harness.c)"},
			wantRefs:    1,
		},
		{
			// LOCK: idempotent - a target already listing the harness is
			// skipped while its sibling still gets patched.
			name: "already_listed_target_is_skipped_per_target",
			src: "add_library(${NAME} SHARED src/main.c src/mcp_harness.c)\n" +
				"add_executable(${NAME}_device src/main.c)\n",
			wantChanged: true,
			wantRefs:    2,
		},
		{
			// LOCK (fixed in step 7): CMake command names are
			// case-insensitive and older projects write them in caps. Only
			// the command name is matched loosely - paths stay
			// case-sensitive, because the filesystem is.
			name:        "uppercase_cmake_commands_are_patched",
			src:         "SET(GAME_SOURCES\n\tsrc/main.c\n)\nADD_LIBRARY(a SHARED ${GAME_SOURCES})\n",
			wantChanged: true,
			wantHas:     []string{"src/main.c\n\tsrc/mcp_harness.c\n)"},
			wantRefs:    1,
		},
		{
			// LOCK: nothing to patch means no change, which is what drives
			// setupC's ManualStep.
			name:        "no_target_and_no_source_set_is_no_change",
			src:         "cmake_minimum_required(VERSION 3.14)\nproject(Game C)\n",
			wantChanged: false,
		},
		{
			// LOCK: the multi-line set() path takes the indent of the
			// closing paren's line and adds one tab. Pinning the exact
			// output because the port reimplements this offset arithmetic.
			name:        "multiline_set_indent_is_derived_from_closing_paren",
			src:         "set(GAME_SOURCES\n    src/main.c\n  )\nadd_library(${NAME} SHARED ${GAME_SOURCES})\n",
			wantChanged: true,
			wantHas:     []string{"    src/main.c\n  \tsrc/mcp_harness.c\n  )"},
			wantRefs:    1,
		},
		{
			// LOCK: a blank line before the closing paren is absorbed into
			// the captured indent, so the inserted entry ends up separated
			// by a blank line. Odd-looking and harmless; pinned because it
			// is easy for a port to normalize by accident.
			name:        "blank_line_before_closing_paren_is_absorbed_into_indent",
			src:         "set(GAME_SOURCES\n\tsrc/main.c\n\n)\nadd_library(${NAME} SHARED ${GAME_SOURCES})\n",
			wantChanged: true,
			wantHas:     []string{"src/main.c\n\n\tsrc/mcp_harness.c\n\n)"},
			wantRefs:    1,
		},
		{
			// LOCK: CRLF CMakeLists. The set() body keeps its \r and the
			// inserted line is bare \n, so the file ends up with mixed line
			// endings - which CMake accepts. Pinned as-is: normalizing is a
			// behavior change, not a port.
			name:        "crlf_set_block_inserts_a_bare_lf_line",
			src:         "set(GAME_SOURCES\r\n\tsrc/main.c\r\n)\r\nadd_library(${NAME} SHARED ${GAME_SOURCES})\r\n",
			wantChanged: true,
			wantHas:     []string{"src/mcp_harness.c"},
			wantRefs:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gameSource := tt.gameSource
			switch gameSource {
			case "":
				gameSource = "src/main.c"
			case "-":
				gameSource = ""
			}
			got, changed := patchCMakeSourceList(tt.src, gameSource)
			if changed != tt.wantChanged {
				t.Errorf("changed = %v, want %v\ngot:\n%s", changed, tt.wantChanged, got)
			}
			if !tt.wantChanged && got != tt.src {
				t.Errorf("content changed but should not have:\n%s", got)
			}
			for _, want := range tt.wantHas {
				if !strings.Contains(got, want) {
					t.Errorf("content missing %q:\n%s", want, got)
				}
			}
			for _, unwanted := range tt.wantLacks {
				if strings.Contains(got, unwanted) {
					t.Errorf("content still has %q:\n%s", unwanted, got)
				}
			}
			if tt.wantRefs > 0 {
				if n := strings.Count(got, "src/mcp_harness.c"); n != tt.wantRefs {
					t.Errorf("src/mcp_harness.c appears %d times, want %d:\n%s", n, tt.wantRefs, got)
				}
			}
		})
	}
}

// The scanners the patching is built from, at their boundaries. A hand-written
// scanner's characteristic bug is an offset that is one byte off - the CRLF
// insertion bug below was exactly that - and the cases that catch it are the
// ones where a token sits at the very start or the very end of the content.
func TestScannerBoundaries(t *testing.T) {
	t.Run("receiverIsWhole", func(t *testing.T) {
		tests := []struct {
			name    string
			content string
			at      int
			want    bool
		}{
			{name: "start_of_content", content: "pd->system->getCrankAngle()", at: 0, want: true},
			// One byte in, preceded by a member access: the tail of an
			// expression this cannot rewrite.
			{name: "preceded_by_a_dot", content: ".pd->system", at: 1},
			// Two bytes in, preceded by an arrow: the shortest possible chain
			// that has to be declined.
			{name: "preceded_by_an_arrow", content: "->pd->system", at: 2},
			{name: "preceded_by_a_close_paren", content: ")pd->system", at: 1},
			{name: "preceded_by_a_subscript", content: "]pd->system", at: 1},
			// A greater-than is not the ">" of an arrow.
			{name: "preceded_by_greater_than", content: "a>pd->system", at: 2, want: true},
			{name: "preceded_by_a_space", content: "= pd->system", at: 2, want: true},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if got := receiverIsWhole(tt.content, tt.at); got != tt.want {
					t.Errorf("receiverIsWhole(%q, %d) = %v, want %v", tt.content, tt.at, got, tt.want)
				}
			})
		}
	})

	// Each of these reads a token that can legitimately sit at offset 0, where a
	// "not found" sentinel of -1 and a real match are one comparison apart.
	t.Run("token_at_offset_zero", func(t *testing.T) {
		if v, ok := eventHandlerParam("eventHandler(PlaydateAPI *pd, PDSystemEvent e, uint32_t a) {\n}\n"); !ok || v != "pd" {
			t.Errorf("eventHandlerParam() = %q, %v, want \"pd\", true", v, ok)
		}
		if name, ok := registeredUpdateCallback("setUpdateCallback(update, NULL);\n"); !ok || name != "update" {
			t.Errorf("registeredUpdateCallback() = %q, %v, want \"update\", true", name, ok)
		}
		if at, ok := functionBodyStart("update(void *ud) {\n\treturn 1;\n}\n", "update"); !ok || at != 18 {
			t.Errorf("functionBodyStart() = %d, %v, want 18, true", at, ok)
		}
		if at, found := findInitInsertionPoint("kEventInit == event) {\n\tsetup();\n}\n"); !found || at != 22 {
			t.Errorf("findInitInsertionPoint() = %d, %v, want 22, true", at, found)
		}
		steps := leftoverInputCallSteps("src/input.c", "system -> getCrankAngle();\n")
		if len(steps) != 1 {
			t.Fatalf("leftoverInputCallSteps() = %q, want one step", steps)
		}
		if !strings.Contains(steps[0], "src/input.c:1") {
			t.Errorf("step = %q, want it to name line 1", steps[0])
		}
	})

	t.Run("declaration_at_end_of_content", func(t *testing.T) {
		// No terminating ";" because the file ends: the check for one must not
		// read past the end, and must not accept the declaration either.
		if v, ok := findAccessiblePlaydateVar("static PlaydateAPI *pd"); ok {
			t.Errorf("findAccessiblePlaydateVar() = %q, true, want no match on an unterminated declaration", v)
		}
		if v, ok := findAccessiblePlaydateVar("static PlaydateAPI *pd;"); !ok || v != "pd" {
			t.Errorf("findAccessiblePlaydateVar() = %q, %v, want \"pd\", true", v, ok)
		}
	})

	t.Run("include_lines", func(t *testing.T) {
		tests := []struct {
			name string
			line string
			want bool
		}{
			{name: "angle", line: "#include <stdio.h>", want: true},
			{name: "quoted", line: `#include "pd_api.h"`, want: true},
			{name: "trailing_space", line: "#include <stdio.h> ", want: true},
			// A directive with nothing after it, so the delimiter check runs at
			// the end of the line.
			{name: "bare_directive", line: "#include"},
			// An unterminated delimiter, so the name scan runs to the end.
			{name: "unterminated", line: "#include <stdio.h"},
			// An empty name, which is the other end of the same check.
			{name: "empty_name", line: "#include <>"},
			{name: "trailing_comment", line: "#include <stdio.h> // c stdlib"},
			{name: "not_an_include", line: "#define X 1"},
			{name: "empty", line: ""},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if got := isIncludeLine(tt.line); got != tt.want {
					t.Errorf("isIncludeLine(%q) = %v, want %v", tt.line, got, tt.want)
				}
			})
		}

		// A blank first line, so the line walk starts on an empty line rather
		// than on content.
		got, changed := insertIncludeIfMissing("\n#include <stdio.h>\nvoid f(void) {}\n")
		if !changed {
			t.Fatal("insertIncludeIfMissing() changed = false, want true")
		}
		if !strings.HasPrefix(got, "\n#include <stdio.h>\n"+cMarkerBegin) {
			t.Errorf("want the block inserted after the include, not at the top:\n%q", got)
		}
	})
}

// TestInsertionsKeepExistingLineEndings pins the CRLF property of the insertion
// points: text is added on its own new line, and the line it follows keeps the
// ending it had. Getting this backwards is easy - stopping an offset just before
// the "\r" of a CRLF pair silently rewrites a line the patch was never meant to
// touch - and nothing caught it until this test.
func TestInsertionsKeepExistingLineEndings(t *testing.T) {
	t.Run("include", func(t *testing.T) {
		got, changed := insertIncludeIfMissing("#include <stdio.h>\r\n#include \"pd_api.h\"\r\n\r\nvoid f(void) {}\r\n")
		if !changed {
			t.Fatal("insertIncludeIfMissing() changed = false, want true")
		}
		if !strings.Contains(got, "#include \"pd_api.h\"\r\n") {
			t.Errorf("the existing include lost its CRLF ending:\n%q", got)
		}
	})

	t.Run("include_directories", func(t *testing.T) {
		got, changed := patchCMakeIncludeDirectories("project(Game C)\r\nadd_library(a SHARED src/main.c)\r\n")
		if !changed {
			t.Fatal("patchCMakeIncludeDirectories() changed = false, want true")
		}
		if !strings.Contains(got, "project(Game C)\r\n") {
			t.Errorf("the project line lost its CRLF ending:\n%q", got)
		}
	})
}

// ---------------------------------------------------------------------------
// patchCMakeIncludeDirectories and the project() line
// ---------------------------------------------------------------------------

func TestPatchCMakeIncludeDirectoriesEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		src         string
		wantChanged bool
		// wantAfter, when set, is the text the inserted block must follow.
		wantAfter string
	}{
		{
			// LOCK (fixed by the port): "project (Game C)" with a space is
			// valid CMake. It used to go unmatched, so include_directories(src)
			// was never added, and a project keeping its sources outside src/
			// then failed to find mcp_harness.h - surfacing as a compile error
			// in the user's own file.
			name:        "space_before_paren_is_handled",
			src:         "cmake_minimum_required(VERSION 3.14)\nproject (Game C)\nadd_library(a SHARED src/main.c)\n",
			wantChanged: true,
		},
		{
			// LOCK (fixed by the port): a trailing comment used to defeat the
			// end-of-line anchor. The comment stays on the project line and the
			// insertion goes after it.
			name:        "trailing_comment_on_project_line_is_handled",
			src:         "project(Game C) # the game\nadd_library(a SHARED src/main.c)\n",
			wantChanged: true,
			wantAfter:   "project(Game C) # the game\n",
		},
		{
			// LOCK (fixed by the port): the column-0 anchor was a pattern
			// artifact, not a rule about CMake.
			name:        "indented_project_line_is_handled",
			src:         "  project(Game C)\nadd_library(a SHARED src/main.c)\n",
			wantChanged: true,
		},
		{
			// LOCK (fixed in step 6): a commented-out attempt is a note about
			// something that is not there. Reading it as "already present"
			// suppressed the real insertion entirely.
			name:        "commented_include_directories_does_not_suppress_the_patch",
			src:         "project(Game C)\n# include_directories(src)  # tried this, did not help\n",
			wantChanged: true,
		},
		{
			// LOCK: a multi-line project() call is matched, because [^)]*
			// crosses newlines.
			name:        "multiline_project_call_is_matched",
			src:         "project(Game\n\tC\n)\nadd_library(a SHARED src/main.c)\n",
			wantChanged: true,
		},
		{
			// LOCK: plain form.
			name:        "plain_project_line_is_matched",
			src:         "cmake_minimum_required(VERSION 3.14)\nproject(Game C)\nadd_library(a SHARED src/main.c)\n",
			wantChanged: true,
		},
		{
			// LOCK: CRLF project line is matched (\r absorbed by \s*).
			name:        "crlf_project_line_is_matched",
			src:         "project(Game C)\r\nadd_library(a SHARED src/main.c)\r\n",
			wantChanged: true,
		},
		{
			// LOCK: idempotent against a hand-written line.
			name:        "existing_include_directories_is_a_no_op",
			src:         "project(Game C)\ninclude_directories(src)\n",
			wantChanged: false,
		},
		{
			// LOCK (fixed in step 7): PROJECT(...) in caps is valid CMake.
			name:        "uppercase_project_command_is_matched",
			src:         "PROJECT(Game C)\nadd_library(a SHARED src/main.c)\n",
			wantChanged: true,
		},
		{
			// LOCK (fixed in step 7): an existing INCLUDE_DIRECTORIES(src) in
			// caps is recognized, so the line is not added a second time.
			name:        "uppercase_existing_include_directories_is_a_no_op",
			src:         "project(Game C)\nINCLUDE_DIRECTORIES(src)\n",
			wantChanged: false,
		},
		{
			// LOCK: no project() line means no insertion, and no guess.
			name:        "no_project_line_is_a_no_op",
			src:         "add_library(a SHARED src/main.c)\n",
			wantChanged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := patchCMakeIncludeDirectories(tt.src)
			if changed != tt.wantChanged {
				t.Fatalf("changed = %v, want %v\ngot:\n%s", changed, tt.wantChanged, got)
			}
			if !tt.wantChanged {
				if got != tt.src {
					t.Errorf("content changed on a no-op:\n%s", got)
				}
				return
			}
			if !strings.Contains(got, "include_directories(src)") {
				t.Errorf("include_directories(src) missing:\n%s", got)
			}
			if !strings.Contains(got, cmakeMarkerBegin) {
				t.Errorf("insertion is not marker-wrapped, teardown cannot reverse it:\n%s", got)
			}
			if tt.wantAfter != "" && !strings.Contains(got, tt.wantAfter+cmakeMarkerBegin) {
				t.Errorf("want the block inserted directly after %q:\n%s", tt.wantAfter, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// findEventHandler: \beventHandler\s*\(\s*PlaydateAPI\s*\*\s*(\w+)
// ---------------------------------------------------------------------------

func TestFindEventHandlerEdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		src        string
		wantVar    string
		wantNoFind bool
	}{
		{
			// LOCK: the SDK's own DllExport form, attribute and all.
			name:    "dllexport_prototype_form",
			src:     "#ifdef _WINDLL\n__declspec(dllexport)\n#endif\nint eventHandler(PlaydateAPI* pd, PDSystemEvent event, uint32_t arg) {\n\treturn 0;\n}\n",
			wantVar: "pd",
		},
		{
			// LOCK: whitespace around the pointer star and the parameter
			// name is tolerated, and the name is captured rather than
			// assumed.
			name:    "spaced_pointer_and_custom_var_name",
			src:     "int eventHandler ( PlaydateAPI * playdate , PDSystemEvent event, uint32_t arg) {\n\treturn 0;\n}\n",
			wantVar: "playdate",
		},
		{
			// LOCK: \b keeps a differently-named function from matching.
			name:       "similarly_named_function_does_not_match",
			src:        "int myEventHandler(PlaydateAPI *pd, PDSystemEvent event, uint32_t arg) {\n\treturn 0;\n}\n",
			wantNoFind: true,
		},
		{
			// LOCK (fixed in step 6): a commented-out eventHandler is not one.
			// Accepting it designated this file as the handler's home, where
			// no kEventInit branch could then be found - and setup failed the
			// whole call after having already copied the harness files in and
			// patched CMakeLists.
			name:       "commented_out_event_handler_is_skipped",
			src:        "// int eventHandler(PlaydateAPI *pd, PDSystemEvent event, uint32_t arg);\nvoid other(void) {}\n",
			wantNoFind: true,
		},
		{
			// LOCK: a prototype before the definition matches first. Same
			// parameter name, same file, so the outcome is identical - but
			// pinned, because a port that looks for a definition (a "{"
			// after the parameter list) would report the other file in a
			// project that declares eventHandler in one .c and defines it in
			// another, changing which file gets patched.
			name:    "prototype_matches_before_definition",
			src:     "int eventHandler(PlaydateAPI *pd, PDSystemEvent event, uint32_t arg);\n\nint eventHandler(PlaydateAPI *pd, PDSystemEvent event, uint32_t arg) {\n\treturn 0;\n}\n",
			wantVar: "pd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "src", "main.c")
			mustWrite(t, path, tt.src)

			gotPath, gotVar, err := findEventHandler(dir)
			if tt.wantNoFind {
				if err == nil {
					t.Fatalf("findEventHandler() = %q, %q, want an error", gotPath, gotVar)
				}
				return
			}
			if err != nil {
				t.Fatalf("findEventHandler: %v", err)
			}
			if gotPath != path {
				t.Errorf("path = %q, want %q", gotPath, path)
			}
			if gotVar != tt.wantVar {
				t.Errorf("pdVar = %q, want %q", gotVar, tt.wantVar)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// patchEventHandlerInit and findInitInsertionPoint
// ---------------------------------------------------------------------------

// This used to be the most brittle pattern in the file, and the only one whose
// failure was fatal rather than a ManualStep: an unrecognized branch shape
// failed the whole setup call, leaving the harness files copied and CMakeLists
// patched but nothing wired. Step 7 widened the shapes it accepts and
// downgraded the rest to ManualSteps.
func TestPatchEventHandlerInitEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		src  string
		// wantStep, when set, is a substring of the ManualStep this file
		// should produce instead of an inserted init call.
		wantStep string
		// wantInitBefore, when set, is a substring the inserted
		// mcp_harness_init call must precede.
		wantInitBefore string
	}{
		{
			// LOCK: the canonical shape from the SDK examples.
			name: "canonical_if_branch",
			src:  "int eventHandler(PlaydateAPI *pd, PDSystemEvent event, uint32_t arg) {\n\tif (event == kEventInit) {\n\t\tsetup();\n\t}\n\treturn 0;\n}\n",
		},
		{
			// LOCK (fixed in step 7): a switch over the event is idiomatic C
			// and arguably the more natural way to write this handler. The
			// call goes right after the case label's colon, which is a
			// statement position whether or not the body has braces.
			name:           "switch_case_with_braces",
			src:            "int eventHandler(PlaydateAPI *pd, PDSystemEvent event, uint32_t arg) {\n\tswitch (event) {\n\tcase kEventInit: {\n\t\tsetup();\n\t\tbreak;\n\t}\n\t}\n\treturn 0;\n}\n",
			wantInitBefore: "setup();",
		},
		{
			// LOCK (fixed in step 7): the same, with no braces on the case
			// body - the shape a switch usually has.
			name:           "switch_case_without_braces",
			src:            "int eventHandler(PlaydateAPI *pd, PDSystemEvent event, uint32_t arg) {\n\tswitch (event) {\n\tcase kEventInit:\n\t\tsetup();\n\t\tbreak;\n\t}\n\treturn 0;\n}\n",
			wantInitBefore: "setup();",
		},
		{
			// LOCK (fixed in step 7): a compound condition. Most real
			// handlers guard against double init this way.
			name:           "compound_condition",
			src:            "int eventHandler(PlaydateAPI *pd, PDSystemEvent event, uint32_t arg) {\n\tif (event == kEventInit && !inited) {\n\t\tsetup();\n\t}\n\treturn 0;\n}\n",
			wantInitBefore: "setup();",
		},
		{
			// LOCK (fixed in step 7): Yoda comparison.
			name:           "reversed_comparison",
			src:            "int eventHandler(PlaydateAPI *pd, PDSystemEvent event, uint32_t arg) {\n\tif (kEventInit == event) {\n\t\tsetup();\n\t}\n\treturn 0;\n}\n",
			wantInitBefore: "setup();",
		},
		{
			// LOCK (fixed in step 7): redundant parentheses.
			name:           "double_parens",
			src:            "int eventHandler(PlaydateAPI *pd, PDSystemEvent event, uint32_t arg) {\n\tif ((event == kEventInit)) {\n\t\tsetup();\n\t}\n\treturn 0;\n}\n",
			wantInitBefore: "setup();",
		},
		{
			// LOCK: a braceless branch still can't be patched - there is no
			// block to insert into without rewriting the branch - but it is a
			// ManualStep naming the file now, not a failed setup call.
			name:     "braceless_branch_reports_a_manual_step",
			src:      "int eventHandler(PlaydateAPI *pd, PDSystemEvent event, uint32_t arg) {\n\tif (event == kEventInit)\n\t\tsetup();\n\treturn 0;\n}\n",
			wantStep: "no braces to insert into",
		},
		{
			// LOCK: kEventInit used in an expression rather than a branch. The
			// ";" reached before any "{" is what rules it out, and guessing at
			// the if (wantInit) below it is not something this tool should do.
			name:     "kEventInit_in_an_expression_reports_a_manual_step",
			src:      "int eventHandler(PlaydateAPI *pd, PDSystemEvent event, uint32_t arg) {\n\tint wantInit = event == kEventInit;\n\tif (wantInit) {\n\t\tsetup();\n\t}\n\treturn 0;\n}\n",
			wantStep: "no braces to insert into",
		},
		{
			// LOCK: no kEventInit anywhere. Reported, not fatal.
			name:     "no_init_branch_reports_a_manual_step",
			src:      "int eventHandler(PlaydateAPI *pd, PDSystemEvent event, uint32_t arg) {\n\treturn 0;\n}\n",
			wantStep: "could not find where",
		},
		{
			// LOCK (fixed in step 6): the first match in CODE wins, so the
			// live branch gets the call and the commented-out one above it is
			// stepped over. It used to take the comment, putting the init call
			// where it never compiles while setup still reported success - the
			// worst outcome in this file, because the symptom (a harness that
			// never answers) reads like a transport bug.
			name: "commented_branch_does_not_swallow_the_init_call",
			src: "int eventHandler(PlaydateAPI *pd, PDSystemEvent event, uint32_t arg) {\n" +
				"\t// was: if (event == kEventInit) { legacySetup(); }\n" +
				"\tif (event == kEventInit) {\n\t\tsetup();\n\t}\n\treturn 0;\n}\n",
			// Same anchor as every other case here: the call goes in front of
			// the live branch's first statement. It used to land above this
			// line entirely, inside the comment.
			wantInitBefore: "setup();",
		},
		{
			// LOCK: whitespace around == is tolerated.
			name: "spaced_comparison_is_matched",
			src:  "int eventHandler(PlaydateAPI *pd, PDSystemEvent event, uint32_t arg) {\n\tif ( event  ==  kEventInit )\n\t{\n\t\tsetup();\n\t}\n\treturn 0;\n}\n",
		},
		{
			// LOCK: an existing hand-written init call is not duplicated,
			// and the file is still patched for the include only.
			name: "existing_hand_written_init_is_not_duplicated",
			src:  "int eventHandler(PlaydateAPI *pd, PDSystemEvent event, uint32_t arg) {\n\tif (event == kEventInit) {\n\t\tmcp_harness_init(pd);\n\t}\n\treturn 0;\n}\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "src", "main.c")
			mustWrite(t, path, tt.src)

			_, step, err := patchEventHandlerInit(path, "pd")
			if err != nil {
				t.Fatalf("patchEventHandlerInit: %v", err)
			}
			got := mustRead(t, path)
			if tt.wantStep != "" {
				if !strings.Contains(step, tt.wantStep) {
					t.Fatalf("manualStep = %q, want it to mention %q", step, tt.wantStep)
				}
				if strings.Contains(got, "mcp_harness_init(") {
					t.Errorf("init call inserted despite reporting a manual step:\n%s", got)
				}
				// The include is still inserted, so the hand-written call the
				// manual step asks for has a declaration to use.
				if !strings.Contains(got, `#include "mcp_harness.h"`) {
					t.Errorf("harness include missing, the manual step's own advice would not compile:\n%s", got)
				}
				return
			}
			if step != "" {
				t.Errorf("unexpected manual step: %q", step)
			}
			if n := strings.Count(got, "mcp_harness_init("); n != 1 {
				t.Errorf("mcp_harness_init( appears %d times, want 1:\n%s", n, got)
			}
			if tt.wantInitBefore != "" {
				initAt := strings.Index(got, "mcp_harness_init(")
				anchor := strings.Index(got, tt.wantInitBefore)
				if anchor < 0 {
					t.Fatalf("anchor %q missing:\n%s", tt.wantInitBefore, got)
				}
				if initAt > anchor {
					t.Errorf("want the init call before %q:\n%s", tt.wantInitBefore, got)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// patchUpdateCallback: setUpdateCallbackRe and the dynamic funcRe
// ---------------------------------------------------------------------------

func TestPatchUpdateCallbackEdgeCases(t *testing.T) {
	tests := []struct {
		name           string
		src            string
		wantChanged    bool
		wantManualStep string // substring
		wantHas        []string
	}{
		{
			// LOCK: the canonical shape.
			name: "canonical_registration_and_definition",
			src: "static PlaydateAPI *pd = NULL;\n" +
				"static int update(void *ud) { return 1; }\n" +
				"int eventHandler(PlaydateAPI *p, PDSystemEvent event, uint32_t arg) {\n" +
				"\tif (event == kEventInit) { pd = p; pd->system->setUpdateCallback(update, NULL); }\n\treturn 0;\n}\n",
			wantChanged: true,
			wantHas:     []string{"mcp_harness_update(pd);"},
		},
		{
			// BUG (miss): a cast on the callback argument - the form the SDK
			// docs themselves show for C++ callers - defeats
			// (\w+) right after the paren, so the update hook is never
			// wired. Degrades to a ManualStep, so at least it is reported.
			name: "cast_in_registration_is_missed",
			src: "static PlaydateAPI *pd = NULL;\n" +
				"static int update(void *ud) { return 1; }\n" +
				"int eventHandler(PlaydateAPI *p, PDSystemEvent event, uint32_t arg) {\n" +
				"\tpd->system->setUpdateCallback((PDCallbackFunction *)update, NULL);\n\treturn 0;\n}\n",
			wantManualStep: "setUpdateCallback",
		},
		{
			// LOCK (fixed by the port): a function-pointer parameter in the
			// callback's own signature used to defeat the "no parens inside"
			// character class, so the definition went unfound even though it
			// was right there, and the harness never got its per-frame call.
			// Counting the parameter list's parens handles it.
			name: "function_pointer_param_in_the_callback_signature",
			src: "static PlaydateAPI *pd = NULL;\n" +
				"static int update(int (*tick)(void), void *ud) { return 1; }\n" +
				"int eventHandler(PlaydateAPI *p, PDSystemEvent event, uint32_t arg) {\n" +
				"\tpd->system->setUpdateCallback(update, NULL);\n\treturn 0;\n}\n",
			wantChanged: true,
			wantHas:     []string{"mcp_harness_update(pd);"},
		},
		{
			// LOCK (fixed in step 6): the live registration wins. The
			// commented-out one used to, sending the tool after a callback the
			// user had already deleted and reporting a ManualStep naming it.
			name: "commented_registration_does_not_win_the_match",
			src: "static PlaydateAPI *pd = NULL;\n" +
				"static int update(void *ud) { return 1; }\n" +
				"int eventHandler(PlaydateAPI *p, PDSystemEvent event, uint32_t arg) {\n" +
				"\t// pd->system->setUpdateCallback(oldUpdate, NULL);\n" +
				"\tpd->system->setUpdateCallback(update, NULL);\n\treturn 0;\n}\n",
			wantChanged: true,
			wantHas:     []string{"mcp_harness_update(pd);"},
		},
		{
			// LOCK: registration split across lines is matched, because
			// \s* crosses newlines.
			name: "registration_split_across_lines_is_matched",
			src: "static PlaydateAPI *pd = NULL;\n" +
				"static int update(void *ud) { return 1; }\n" +
				"int eventHandler(PlaydateAPI *p, PDSystemEvent event, uint32_t arg) {\n" +
				"\tpd->system->setUpdateCallback(\n\t\tupdate,\n\t\tNULL);\n\treturn 0;\n}\n",
			wantChanged: true,
			wantHas:     []string{"mcp_harness_update(pd);"},
		},
		{
			// LOCK: an existing hand-written update call is left alone even
			// when the API pointer is only declared in a header this file
			// includes (missile-command's shape) - the mcp_harness_update
			// check runs before the variable heuristic on purpose.
			name: "existing_hand_written_update_is_a_no_op",
			src: "#include \"entity.h\"\n" +
				"static int update(void *ud) { mcp_harness_update(mc_pd); return 1; }\n" +
				"int eventHandler(PlaydateAPI *p, PDSystemEvent event, uint32_t arg) {\n" +
				"\tp->system->setUpdateCallback(update, NULL);\n\treturn 0;\n}\n",
			wantChanged: false,
		},
		{
			// LOCK: no visible API pointer means a ManualStep, not a guess.
			name: "no_visible_playdate_var_reports_a_manual_step",
			src: "static int update(void *ud) { return 1; }\n" +
				"int eventHandler(PlaydateAPI *p, PDSystemEvent event, uint32_t arg) {\n" +
				"\tp->system->setUpdateCallback(update, NULL);\n\treturn 0;\n}\n",
			wantManualStep: "no PlaydateAPI pointer variable visible",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "src", "main.c")
			mustWrite(t, path, tt.src)

			changed, _, manualStep, err := patchUpdateCallback(dir, path)
			if err != nil {
				t.Fatalf("patchUpdateCallback: %v", err)
			}
			if changed != tt.wantChanged {
				t.Errorf("changed = %v, want %v (manualStep=%q)", changed, tt.wantChanged, manualStep)
			}
			if tt.wantManualStep == "" {
				if manualStep != "" {
					t.Errorf("unexpected manual step: %q", manualStep)
				}
			} else if !strings.Contains(manualStep, tt.wantManualStep) {
				t.Errorf("manualStep = %q, want it to mention %q", manualStep, tt.wantManualStep)
			}
			got := mustRead(t, path)
			for _, want := range tt.wantHas {
				if !strings.Contains(got, want) {
					t.Errorf("content missing %q:\n%s", want, got)
				}
			}
			if !tt.wantChanged && got != tt.src {
				t.Errorf("file was modified when it should not have been:\n%s", got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// findAccessiblePlaydateVar: (?:static\s+)?PlaydateAPI\s*\*\s*(\w+)\s*(?:=|;)
// ---------------------------------------------------------------------------

func TestFindAccessiblePlaydateVarEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		wantVar string
		wantOK  bool
	}{
		// LOCK: the shapes this project's own examples use.
		{name: "static_initialized", src: "static PlaydateAPI *pd = NULL;\n", wantVar: "pd", wantOK: true},
		{name: "static_uninitialized", src: "static PlaydateAPI *g_pd;\n", wantVar: "g_pd", wantOK: true},
		{name: "star_hugs_type", src: "PlaydateAPI* mc_pd = NULL;\n", wantVar: "mc_pd", wantOK: true},
		{name: "extern_declaration", src: "extern PlaydateAPI *mc_pd;\n", wantVar: "mc_pd", wantOK: true},
		{
			// LOCK: a parameter is correctly not mistaken for an accessible
			// variable - the trailing (?:=|;) is what rules it out.
			name: "function_parameter_is_not_a_variable",
			src:  "void render(PlaydateAPI *pd) {\n\t(void)pd;\n}\n",
		},
		{
			// LOCK: an array of pointers is not a usable pointer, and is
			// correctly rejected.
			name: "pointer_array_is_rejected",
			src:  "PlaydateAPI *pds[4];\n",
		},
		{
			// LOCK: a pointer-to-pointer is rejected.
			name: "double_pointer_is_rejected",
			src:  "PlaydateAPI **pdp;\n",
		},
		{
			// LOCK (fixed in step 7): a typedef is not a variable. It used to
			// be inserted into generated code as mcp_harness_update(PDRef),
			// which does not compile, with the error landing in the user's own
			// file. The declaration that follows it is not a match either -
			// "PDRef pd" is not the PlaydateAPI spelling this looks for - so
			// the outcome is a ManualStep.
			name: "typedef_is_not_a_variable",
			src:  "typedef PlaydateAPI *PDRef;\nstatic PDRef pd = NULL;\n",
		},
		{
			// LOCK: a typedef followed by a real declaration finds the real
			// one, rather than stopping at the first textual match.
			name:    "real_declaration_after_a_typedef_is_found",
			src:     "typedef PlaydateAPI *PDRef;\nstatic PlaydateAPI *pd = NULL;\n",
			wantVar: "pd",
			wantOK:  true,
		},
		{
			// LOCK (fixed in step 6): a declaration in a comment declares
			// nothing, and passing its name to mcp_harness_update() produced a
			// file that does not compile.
			name:    "commented_declaration_is_skipped",
			src:     "// static PlaydateAPI *pd = NULL;  // removed, use the param\nstatic int update(void *ud) { return 1; }\n",
			wantVar: "",
			wantOK:  false,
		},
		{
			// LOCK (fixed in step 6): a commented-out old name no longer
			// shadows the live declaration below it.
			name:    "commented_declaration_does_not_shadow_the_real_one",
			src:     "// static PlaydateAPI *oldPd = NULL;\nstatic PlaydateAPI *pd = NULL;\n",
			wantVar: "pd",
			wantOK:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := findAccessiblePlaydateVar(tt.src)
			if ok != tt.wantOK || got != tt.wantVar {
				t.Errorf("findAccessiblePlaydateVar() = %q, %v, want %q, %v", got, ok, tt.wantVar, tt.wantOK)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// teardownCMakeLists: [ \t\n]*src/mcp_harness\.c
// ---------------------------------------------------------------------------

func TestTeardownCMakeListsEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		src         string
		wantChanged bool
		wantHas     []string
		wantLacks   []string
	}{
		{
			// LOCK: the round trip this tool actually produces - inline
			// argument list.
			name:        "inline_entry_is_removed_cleanly",
			src:         "add_library(${NAME} SHARED src/main.c src/mcp_harness.c)\n",
			wantChanged: true,
			wantHas:     []string{"add_library(${NAME} SHARED src/main.c)"},
		},
		{
			// LOCK: multi-line set() entry, including the newline and indent
			// the patch added.
			name:        "set_block_entry_is_removed_with_its_line",
			src:         "set(GAME_SOURCES\n\tsrc/main.c\n\tsrc/mcp_harness.c\n)\n",
			wantChanged: true,
			wantHas:     []string{"set(GAME_SOURCES\n\tsrc/main.c\n)"},
		},
		{
			// LOCK: every occurrence is removed, not just the first.
			name:        "all_occurrences_are_removed",
			src:         "add_library(a SHARED src/main.c src/mcp_harness.c)\nadd_executable(b src/main.c src/mcp_harness.c)\n",
			wantChanged: true,
			wantLacks:   []string{"mcp_harness.c"},
		},
		{
			// LOCK (fixed in step 7): a prefixed entry is removed whole.
			// Removal used to match the bare path anywhere, so this lost only
			// its tail and left a dangling ${CMAKE_CURRENT_SOURCE_DIR}/
			// argument behind - a CMakeLists that no longer configures, from
			// the operation that is supposed to be the safe direction.
			name:        "prefixed_path_is_removed_whole",
			src:         "add_library(a SHARED src/main.c ${CMAKE_CURRENT_SOURCE_DIR}/src/mcp_harness.c)\n",
			wantChanged: true,
			wantHas:     []string{"add_library(a SHARED src/main.c)"},
			wantLacks:   []string{"CMAKE_CURRENT_SOURCE_DIR"},
		},
		{
			// LOCK: a quoted entry is removed whole, quotes included.
			name:        "quoted_entry_is_removed_whole",
			src:         "add_library(a SHARED src/main.c \"src/mcp_harness.c\")\n",
			wantChanged: true,
			wantHas:     []string{"add_library(a SHARED src/main.c)"},
			wantLacks:   []string{`""`},
		},
		{
			// LOCK: CRLF CMakeLists teardown leaves no stray carriage return
			// where the entry's line used to be.
			name:        "crlf_set_block_entry_is_removed_cleanly",
			src:         "set(GAME_SOURCES\r\n\tsrc/main.c\r\n\tsrc/mcp_harness.c\r\n)\r\n",
			wantChanged: true,
			wantHas:     []string{"set(GAME_SOURCES\r\n\tsrc/main.c\r\n)\r\n"},
		},
		{
			// LOCK (fixed in step 6): a mention inside a comment is prose about
			// the build, not part of it. Editing one left a mangled
			// half-sentence behind - harmless to CMake, and exactly the kind of
			// unexplained edit that makes a user distrust every other change.
			name:        "mention_in_a_comment_is_left_alone",
			src:         "# src/mcp_harness.c is added by open-crank setup\nadd_library(a SHARED src/main.c)\n",
			wantChanged: false,
			wantHas:     []string{"# src/mcp_harness.c is added by open-crank setup"},
		},
		{
			// LOCK: the marker-wrapped include_directories block is removed
			// by marker, independently of the source entry.
			name:        "marker_wrapped_include_directories_is_stripped",
			src:         "project(Game C)\n" + markerBlock(cmakeMarkerBegin, cmakeMarkerEnd, "include_directories(src)") + "\nadd_library(a SHARED src/main.c)\n",
			wantChanged: true,
			wantLacks:   []string{"include_directories(src)", cmakeMarkerBegin},
		},
		{
			// LOCK: a hand-written include_directories(src) with no markers
			// is left alone - teardown never removes what setup did not add.
			name:        "unmarked_include_directories_is_left_alone",
			src:         "project(Game C)\ninclude_directories(src)\nadd_library(a SHARED src/main.c)\n",
			wantChanged: false,
		},
		{
			// LOCK: nothing to do means no write.
			name:        "clean_file_is_untouched",
			src:         "project(Game C)\nadd_library(a SHARED src/main.c)\n",
			wantChanged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "CMakeLists.txt")
			mustWrite(t, path, tt.src)

			changed, err := teardownCMakeLists(path)
			if err != nil {
				t.Fatalf("teardownCMakeLists: %v", err)
			}
			if changed != tt.wantChanged {
				t.Errorf("changed = %v, want %v", changed, tt.wantChanged)
			}
			got := mustRead(t, path)
			if !tt.wantChanged && got != tt.src {
				t.Errorf("file was modified when it should not have been:\n%s", got)
			}
			for _, want := range tt.wantHas {
				if !strings.Contains(got, want) {
					t.Errorf("content missing %q:\n%s", want, got)
				}
			}
			for _, unwanted := range tt.wantLacks {
				if strings.Contains(got, unwanted) {
					t.Errorf("content still has %q:\n%s", unwanted, got)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// teardownC's hand-wiring check
// ---------------------------------------------------------------------------

// TestTeardownCIgnoresHarnessMentionsInComments is the counterpart to
// TestTeardownCPreservesHandWrittenReferences: that one pins teardown as a full
// no-op when the project really does reference the harness by hand, this one
// pins that a comment about the harness is not such a reference.
//
// The check is deliberately conservative, so it has to be accurate. Reading a
// leftover comment as hand-wiring made teardown a silent no-op - it reported
// nothing removed and nothing patched, on a project it could have cleaned up
// completely, and gave the user no way to tell that apart from "there was
// nothing to do".
func TestTeardownCIgnoresHarnessMentionsInComments(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "CMakeLists.txt"),
		"project(Game C)\nadd_library(a SHARED src/main.c src/mcp_harness.c)\n")
	mustWrite(t, filepath.Join(dir, "src", "mcp_harness.h"), "// pretend header\n")
	mustWrite(t, filepath.Join(dir, "src", "mcp_harness.c"), "// pretend impl\n")

	// Every reference to the harness here is prose. The marker block is the
	// only real wiring, and teardown owns that.
	mainPath := filepath.Join(dir, "src", "main.c")
	mustWrite(t, mainPath, "#include \"pd_api.h\"\n"+
		markerBlock(cMarkerBegin, cMarkerEnd, `#include "mcp_harness.h"`)+"\n"+
		"// we used to call mcp_harness_init(pd) by hand here\n"+
		"/* and mcp_harness_update(pd) from the update callback */\n"+
		"int eventHandler(PlaydateAPI *pd, PDSystemEvent event, uint32_t arg) {\n"+
		"\treturn 0;\n}\n")

	result, err := Teardown(dir, C)
	if err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	if len(result.FilesRemoved) != 2 {
		t.Errorf("Teardown().FilesRemoved = %v, want both harness files - the only mentions "+
			"left are comments, which wire nothing", result.FilesRemoved)
	}
	got := mustRead(t, mainPath)
	if strings.Contains(got, cMarkerBegin) {
		t.Errorf("the marker block survived teardown:\n%s", got)
	}
	for _, want := range []string{
		"// we used to call mcp_harness_init(pd) by hand here",
		"/* and mcp_harness_update(pd) from the update callback */",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("teardown edited a comment it should have left alone, want %q:\n%s", want, got)
		}
	}
}
