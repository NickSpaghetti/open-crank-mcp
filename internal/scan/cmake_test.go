package scan

import (
	"strings"
	"testing"
)

func TestFindCMakeCall(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string // the argument list, or "" when no call should be found
	}{
		{name: "plain", content: "set(FOO bar)\n", want: "FOO bar"},
		{
			// A CMake command name may be followed by whitespace.
			name:    "space_before_paren",
			content: "set (FOO bar)\n",
			want:    "FOO bar",
		},
		{name: "uppercase", content: "SET(FOO bar)\n", want: "FOO bar"},
		{
			// The name has to be a whole token: "offset" is not "set". This is
			// the substring trap a scanner has to handle that a regex \b
			// handled for free.
			name:    "name_inside_a_longer_word_is_not_a_call",
			content: "offset(FOO bar)\nset(REAL yes)\n",
			want:    "REAL yes",
		},
		{
			name:    "name_with_a_suffix_is_not_a_call",
			content: "settings(FOO bar)\nset(REAL yes)\n",
			want:    "REAL yes",
		},
		{
			// Parens are counted, so a nested call doesn't end the outer one
			// early - the failure mode of the [^()]* class this replaces.
			name:    "nested_parens_are_counted",
			content: "set(FOO $<IF:$<BOOL:${X}>,a,b> \"(literal)\")\n",
			want:    "FOO $<IF:$<BOOL:${X}>,a,b> \"(literal)\"",
		},
		{
			name:    "unterminated_call_is_not_a_match",
			content: "set(FOO bar\n",
			want:    "",
		},
		{
			name:    "bare_name_with_no_call_is_not_a_match",
			content: "# nothing to set here\n",
			want:    "",
		},
		{
			// The name is the last thing in the file, so the check for a
			// following paren has to not read past the end.
			content: "# nothing to set",
			name:    "name_at_the_end_of_the_content",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			openAt, closeAt, ok := FindCMakeCall(tt.content, "set", 0)
			if tt.want == "" {
				if ok {
					t.Fatalf("FindCMakeCall() found %q, want no match", tt.content[openAt+1:closeAt])
				}
				return
			}
			if !ok {
				t.Fatal("FindCMakeCall() found nothing, want a match")
			}
			if got := tt.content[openAt+1 : closeAt]; got != tt.want {
				t.Errorf("args = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCMakeSetBodyAndArgTokens(t *testing.T) {
	const content = "set(CMAKE_C_STANDARD 11)\n" +
		"set(GAME_SOURCES\n\tsrc/main.c\n\t\"src/my file.c\"\n)\n" +
		"set(GAME_SOURCES src/ignored.c)\n"

	body, ok := CMakeSetBody(content, "GAME_SOURCES")
	if !ok {
		t.Fatal("CMakeSetBody() found nothing for GAME_SOURCES")
	}
	got := CMakeArgTokens(body)
	want := []string{"src/main.c", "src/my file.c"}
	if len(got) != len(want) {
		t.Fatalf("tokens = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token %d = %q, want %q", i, got[i], want[i])
		}
	}

	if _, ok := CMakeSetBody(content, "GAME"); ok {
		t.Error("CMakeSetBody() matched GAME against GAME_SOURCES")
	}
	if _, ok := CMakeSetBody(content, "MISSING"); ok {
		t.Error("CMakeSetBody() found a variable that is not set")
	}

	// An empty quoted argument is a real (if pointless) argument, not an
	// unterminated quote - the two are one byte apart in the scan.
	if got := CMakeArgTokens(`a "" b`); len(got) != 3 || got[1] != "" {
		t.Errorf("CMakeArgTokens() = %q, want three tokens with an empty middle one", got)
	}
}

func TestCMakeVarReference(t *testing.T) {
	tests := []struct {
		token string
		want  string
	}{
		{token: "${SRC}", want: "SRC"},
		{token: "${GAME_SOURCES}", want: "GAME_SOURCES"},
		// A path that merely contains a reference is a path, not a reference.
		{token: "${CMAKE_CURRENT_SOURCE_DIR}/src/main.c"},
		{token: "src/main.c"},
		{token: "${}"},
		{token: "${A}${B}"},
		{token: "SHARED"},
	}

	for _, tt := range tests {
		t.Run(tt.token, func(t *testing.T) {
			got, ok := CMakeVarReference(tt.token)
			if ok != (tt.want != "") || got != tt.want {
				t.Errorf("CMakeVarReference(%q) = %q, %v, want %q, %v", tt.token, got, ok, tt.want, tt.want != "")
			}
		})
	}
}

func TestFilterCMakeArgs(t *testing.T) {
	dropMain := func(arg string) bool { return arg == "src/main.c" }

	tests := []struct {
		name        string
		content     string
		drop        func(string) bool
		want        string
		wantRemoved bool
	}{
		{
			// The ")" closing the call survives, because parens are their own
			// tokens rather than part of the argument before them.
			name:        "closing_paren_survives",
			content:     "add_library(a SHARED src/entity.c src/main.c)\n",
			drop:        dropMain,
			want:        "add_library(a SHARED src/entity.c)\n",
			wantRemoved: true,
		},
		{
			// Leading whitespace goes with the argument, so a one-per-line
			// list doesn't leave a blank line behind.
			name:        "whole_line_goes_with_the_argument",
			content:     "set(SRC\n\tsrc/entity.c\n\tsrc/main.c\n)\n",
			drop:        dropMain,
			want:        "set(SRC\n\tsrc/entity.c\n)\n",
			wantRemoved: true,
		},
		{
			// CRLF leaves no stray carriage return where the line was.
			name:        "crlf_line_is_removed_cleanly",
			content:     "set(SRC\r\n\tsrc/entity.c\r\n\tsrc/main.c\r\n)\r\n",
			drop:        dropMain,
			want:        "set(SRC\r\n\tsrc/entity.c\r\n)\r\n",
			wantRemoved: true,
		},
		{
			// Quotes are off by the time drop sees the argument, and the
			// quoted argument is removed with them.
			name:        "quoted_argument_is_removed_with_its_quotes",
			content:     "add_library(a SHARED \"src/main.c\" src/entity.c)\n",
			drop:        dropMain,
			want:        "add_library(a SHARED src/entity.c)\n",
			wantRemoved: true,
		},
		{
			// Nothing to drop means the input comes back byte for byte, which
			// is what makes this safe to run over a whole file.
			name:        "no_match_returns_the_input_unchanged",
			content:     "add_library(a SHARED src/entity.c)\n# a comment (with parens)\n",
			drop:        dropMain,
			want:        "add_library(a SHARED src/entity.c)\n# a comment (with parens)\n",
			wantRemoved: false,
		},
		{
			// A file that ends mid-token, with no trailing newline: the token
			// scan has to stop at the end of the content.
			name:        "no_trailing_newline",
			content:     "add_library(a SHARED src/entity.c src/main.c",
			drop:        dropMain,
			want:        "add_library(a SHARED src/entity.c",
			wantRemoved: true,
		},
		{
			// An unterminated quote runs to the end of the file rather than
			// looping or panicking.
			name:        "unterminated_quote_terminates",
			content:     "add_library(a SHARED \"src/main.c\n",
			drop:        dropMain,
			want:        "add_library(a SHARED \"src/main.c\n",
			wantRemoved: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, removed := FilterCMakeArgs(tt.content, tt.drop)
			if got != tt.want {
				t.Errorf("FilterCMakeArgs() = %q, want %q", got, tt.want)
			}
			if removed != tt.wantRemoved {
				t.Errorf("removed = %v, want %v", removed, tt.wantRemoved)
			}
		})
	}
}

func TestCMakeCalls(t *testing.T) {
	const content = "add_library(vendor INTERFACE)\n" +
		"if(BUILD_TOOLS)\n\tADD_EXECUTABLE(gen src/gen.c)\nendif()\n" +
		"add_library(game SHARED src/main.c  # (entry point)\n)\n"

	calls := CMakeCalls(content, "add_library", "add_executable")
	if len(calls) != 3 {
		t.Fatalf("found %d calls, want 3: %+v", len(calls), calls)
	}
	// Source order, not per-name order, so a caller rewriting from the last one
	// backward keeps its offsets valid.
	wantArgs := []string{
		"vendor INTERFACE",
		"gen src/gen.c",
		"game SHARED src/main.c  # (entry point)\n",
	}
	for i, want := range wantArgs {
		if got := content[calls[i].ArgsStart:calls[i].ArgsEnd]; got != want {
			t.Errorf("call %d args = %q, want %q", i, got, want)
		}
	}
	if calls := CMakeCalls("project(Game C)\n", "add_library"); len(calls) != 0 {
		t.Errorf("found %d calls in a file with none", len(calls))
	}

	// The names are searched one at a time, so a file whose first call is the
	// second name has to be sorted back into source order - including moving an
	// entry all the way to the front.
	const executableFirst = "add_executable(gen src/gen.c)\nadd_library(game SHARED src/main.c)\n"
	ordered := CMakeCalls(executableFirst, "add_library", "add_executable")
	if len(ordered) != 2 {
		t.Fatalf("found %d calls, want 2", len(ordered))
	}
	if ordered[0].Name != "add_executable" || ordered[1].Name != "add_library" {
		t.Errorf("calls = %s, %s, want add_executable then add_library", ordered[0].Name, ordered[1].Name)
	}
}

func TestCMakeCallOnOwnLine(t *testing.T) {
	tests := []struct {
		name string
		src  string
		// want is the text the returned offset should sit at the end of.
		want string
	}{
		{name: "plain", src: "cmake_minimum_required(VERSION 3.14)\nproject(Game C)\nadd_library(a SHARED x.c)\n", want: "project(Game C)"},
		{name: "space_before_paren", src: "project (Game C)\n", want: "project (Game C)"},
		{name: "indented", src: "  project(Game C)\n", want: "  project(Game C)"},
		// The comment stays on the line it was written on.
		{name: "trailing_comment", src: "project(Game C) # the game\n", want: "project(Game C) # the game"},
		{name: "multiline_call", src: "project(Game\n\tC\n)\nadd_library(a SHARED x.c)\n", want: "project(Game\n\tC\n)"},
		// The offset lands after the carriage return, so inserting there leaves
		// the project line's own CRLF ending intact.
		{name: "crlf", src: "project(Game C)\r\nadd_library(a SHARED x.c)\r\n", want: "project(Game C)\r"},
		{name: "uppercase", src: "PROJECT(Game C)\n", want: "PROJECT(Game C)"},
		{
			// No trailing newline: the end-of-line check has to stop at the end
			// of the content.
			name: "no_trailing_newline",
			src:  "project(Game C)",
			want: "project(Game C)",
		},
		{name: "not_on_its_own_line", src: "set(X y) project(Game C)\n"},
		// Anything other than a comment after the call means the line is not
		// the call's own.
		{name: "trailing_argument_like_text", src: "project(Game C) junk\n"},
		{name: "absent", src: "add_library(a SHARED x.c)\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			at, ok := CMakeCallOnOwnLine(tt.src, "project")
			if tt.want == "" {
				if ok {
					t.Fatalf("CMakeCallOnOwnLine() = %d, want no match (%q)", at, tt.src[:at])
				}
				return
			}
			if !ok {
				t.Fatal("CMakeCallOnOwnLine() found nothing, want a match")
			}
			if got := tt.src[:at]; !strings.HasSuffix(got, tt.want) {
				t.Errorf("offset sits after %q, want it to sit right after %q", got, tt.want)
			}
		})
	}
}

func TestCMakeSetBlocks(t *testing.T) {
	t.Run("fields", func(t *testing.T) {
		const src = "set(GAME_SOURCES\n    src/main.c\n  )\n"
		blocks := CMakeSetBlocks(src)
		if len(blocks) != 1 {
			t.Fatalf("found %d blocks, want 1", len(blocks))
		}
		b := blocks[0]
		if b.Name != "GAME_SOURCES" {
			t.Errorf("Name = %q, want GAME_SOURCES", b.Name)
		}
		if got := b.Body(src); got != "    src/main.c" {
			t.Errorf("Body() = %q, want %q", got, "    src/main.c")
		}
		// The indent of the closing paren's line is what an inserted entry
		// copies.
		if got := b.Indent(src); got != "  " {
			t.Errorf("Indent() = %q, want two spaces", got)
		}
	})

	t.Run("shapes", func(t *testing.T) {
		tests := []struct {
			name  string
			src   string
			want  int
			names []string
		}{
			{name: "multiline", src: "set(SRC\n\ta.c\n)\n", want: 1, names: []string{"SRC"}},
			// A single-line set() has no line-shaped body to add an entry to.
			{name: "single_line", src: "set(SRC a.c)\n"},
			// Nor does one whose closing paren shares the last source's line.
			{name: "closing_paren_on_source_line", src: "set(SRC\n\ta.c)\n"},
			{name: "two_blocks", src: "set(FLAGS\n\t-Wall\n)\nset(SRC\n\ta.c\n)\n", want: 2, names: []string{"FLAGS", "SRC"}},
			// "offset(" is not "set(".
			{name: "not_a_set_call", src: "offset(SRC\n\ta.c\n)\n"},
			// CMake variable names cannot start with a digit.
			{name: "digit_leading_name", src: "set(2SRC\n\ta.c\n)\n"},
			// Both ends of the digit range, since the check is a comparison.
			{name: "zero_leading_name", src: "set(0SRC\n\ta.c\n)\n"},
			{name: "nine_leading_name", src: "set(9SRC\n\ta.c\n)\n"},
			{name: "empty_call", src: "set()\n"},
			{name: "uppercase", src: "SET(SRC\n\ta.c\n)\n", want: 1, names: []string{"SRC"}},
			{name: "blank_line_before_paren", src: "set(SRC\n\ta.c\n\n)\n", want: 1, names: []string{"SRC"}},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				blocks := CMakeSetBlocks(tt.src)
				if len(blocks) != tt.want {
					t.Fatalf("found %d blocks, want %d: %+v", len(blocks), tt.want, blocks)
				}
				for i, name := range tt.names {
					if blocks[i].Name != name {
						t.Errorf("block %d name = %q, want %q", i, blocks[i].Name, name)
					}
				}
			})
		}
	})
}
