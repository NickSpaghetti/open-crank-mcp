package scan

import "testing"

// codeString renders the source with every non-code byte replaced by ".",
// leaving whitespace alone so diffs stay readable.
func codeString(content string, c Code) string {
	out := []byte(content)
	for i := range out {
		if c.IsCode(i) || out[i] == '\n' || out[i] == '\r' || out[i] == '\t' {
			continue
		}
		out[i] = '.'
	}
	return string(out)
}

func TestCCode(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "plain code is all code",
			src:  "int x = 1;",
			want: "int x = 1;",
		},
		{
			name: "line comment",
			src:  "int x; // note\nint y;",
			want: "int x; .......\nint y;",
		},
		{
			name: "block comment",
			src:  "int x; /* a\nb */ int y;",
			want: "int x; ....\n.... int y;",
		},
		{
			name: "string literal including its quotes",
			src:  `puts("hi");`,
			want: `puts(....);`,
		},
		{
			name: "escaped quote does not end the string",
			src:  `puts("say \"hi\"") ; int x;`,
			want: `puts(............) ; int x;`,
		},
		{
			name: "character literal holding a quote",
			src:  `char q = '"'; int x;`,
			want: `char q = ...; int x;`,
		},
		{
			name: "escaped backslash still ends the string",
			src:  `puts("\\"); int x;`,
			want: `puts(....); int x;`,
		},
		{
			name: "a // inside a string is not a comment",
			src:  `char *u = "http://x"; int y;`,
			want: `char *u = ..........; int y;`,
		},
		{
			name: "a quote inside a comment is not a string",
			src:  "// it's fine\nint x = 1;",
			want: "............\nint x = 1;",
		},
		{
			name: "a /* inside a line comment opens nothing",
			src:  "// /* not a block\nint x;",
			want: ".................\nint x;",
		},
		{
			name: "a // inside a block comment closes nothing",
			src:  "/* // still open\n*/ int x;",
			want: "................\n.. int x;",
		},
		{
			name: "block comments do not nest",
			src:  "/* a /* b */ int x;",
			want: "............ int x;",
		},
		{
			name: "backslash splices the next line into a line comment",
			src:  "// first \\\nstill comment\nint x;",
			want: "..........\n.............\nint x;",
		},
		{
			name: "backslash splice survives CRLF",
			src:  "// first \\\r\nstill comment\r\nint x;",
			want: "..........\r\n.............\r\nint x;",
		},
		{
			name: "unterminated string ends at the line break",
			src:  "puts(\"oops);\nint x = 1;",
			want: "puts(.......\nint x = 1;",
		},
		{
			name: "unterminated block comment runs to the end",
			src:  "int x;\n/* forever\nint y;",
			want: "int x;\n..........\n......",
		},
		{
			// A first-byte guard must still find the directive.
			name: "include directive is code holding a literal",
			src:  "#include \"mcp_harness.h\"\n",
			want: "#include ...............\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := codeString(tt.src, CCode(tt.src)); got != tt.want {
				t.Errorf("CCode()\n got: %q\nwant: %q", got, tt.want)
			}
		})
	}
}

func TestCMakeCode(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "line comment",
			src:  "add_library(a) # note\nset(X 1)",
			want: "add_library(a) ......\nset(X 1)",
		},
		{
			name: "quoted argument stays code",
			src:  `add_library(a "src/main.c")`,
			want: `add_library(a "src/main.c")`,
		},
		{
			name: "a hash inside a quoted argument is not a comment",
			src:  `set(X "a # b") set(Y 1)`,
			want: `set(X "a # b") set(Y 1)`,
		},
		{
			name: "bracket comment",
			src:  "#[[ a\nb ]] set(X 1)",
			want: ".....\n.... set(X 1)",
		},
		{
			name: "bracket comment with equals signs",
			src:  "#[==[ ]] still ]==] set(X 1)",
			want: "................... set(X 1)",
		},
		{
			name: "unterminated quote ends at the line break",
			src:  "set(X \"oops)\nset(Y 1)",
			want: "set(X \"oops)\nset(Y 1)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := codeString(tt.src, CMakeCode(tt.src)); got != tt.want {
				t.Errorf("CMakeCode()\n got: %q\nwant: %q", got, tt.want)
			}
		})
	}
}

// The package's free search functions rely on a nil Code treating all bytes as code.
func TestNilCodeTreatsEverythingAsCode(t *testing.T) {
	var c Code
	if !c.IsCode(0) || !c.IsCode(1<<20) {
		t.Error("a nil Code should treat every offset as code")
	}
	if got := c.Index("// pd->system->getCrankAngle()", "getCrankAngle", 0); got < 0 {
		t.Error("a nil Code should find a match inside a comment")
	}
}

func TestCodeSearches(t *testing.T) {
	src := "// kEventInit here\nkEventInit;\n/* kEventInit */\n"
	c := CCode(src)

	first := c.TokenIndex(src, "kEventInit", 0)
	if first < 0 || src[first-1] != '\n' {
		t.Errorf("TokenIndex() = %d, want the live occurrence at the start of line 2", first)
	}
	if last := c.LastIndex(src, "kEventInit", len(src)); last != first {
		t.Errorf("LastIndex() = %d, want the same live occurrence at %d", last, first)
	}
	if !c.Contains(src, "kEventInit") {
		t.Error("Contains() = false, want true for the live occurrence")
	}
	if c.Contains(src, "here") {
		t.Error("Contains() = true for a word that only appears in a comment")
	}

	only := "kEventInitDone;\n"
	if got := CCode(only).TokenIndex(only, "kEventInit", 0); got != -1 {
		t.Errorf("TokenIndex() = %d, want -1: kEventInitDone is not kEventInit", got)
	}
}

// Callers feed back offsets computed from rewritten content, so out-of-range
// offsets are ordinary inputs here.
func TestCodeSearchBoundaries(t *testing.T) {
	src := "kEventInit;\n"
	c := CCode(src)

	t.Run("empty content", func(t *testing.T) {
		empty := CCode("")
		if empty.Index("", "x", 0) != -1 || empty.LastIndex("", "x", 0) != -1 {
			t.Error("searching an empty file should find nothing")
		}
		if empty.TokenIndex("", "x", 0) != -1 {
			t.Error("TokenIndex on an empty file should find nothing")
		}
		if empty.IsCode(0) {
			t.Error("offset 0 of an empty file is not code")
		}
	})

	t.Run("absent needle", func(t *testing.T) {
		if got := c.Index(src, "nope", 0); got != -1 {
			t.Errorf("Index() = %d, want -1", got)
		}
		if got := c.LastIndex(src, "nope", len(src)); got != -1 {
			t.Errorf("LastIndex() = %d, want -1", got)
		}
		if got := c.TokenIndex(src, "nope", 0); got != -1 {
			t.Errorf("TokenIndex() = %d, want -1", got)
		}
	})

	t.Run("negative from is treated as the start", func(t *testing.T) {
		if got := c.Index(src, "kEventInit", -5); got != 0 {
			t.Errorf("Index() = %d, want 0", got)
		}
		// TokenIndex treats a negative from as a caller bug and refuses.
		if got := c.TokenIndex(src, "kEventInit", -5); got != -1 {
			t.Errorf("TokenIndex() = %d, want -1 for a negative from", got)
		}
	})

	t.Run("before past the end is clamped", func(t *testing.T) {
		if got := c.LastIndex(src, "kEventInit", len(src)+100); got != 0 {
			t.Errorf("LastIndex() = %d, want 0", got)
		}
	})

	t.Run("from at or past the end finds nothing", func(t *testing.T) {
		if got := c.Index(src, "kEventInit", len(src)); got != -1 {
			t.Errorf("Index() = %d, want -1", got)
		}
		if got := c.LastIndex(src, "kEventInit", 0); got != -1 {
			t.Errorf("LastIndex() = %d, want -1", got)
		}
	})

	t.Run("empty token", func(t *testing.T) {
		if got := c.TokenIndex(src, "", 0); got != -1 {
			t.Errorf("TokenIndex() = %d, want -1 for an empty token", got)
		}
	})

	t.Run("token longer than the file", func(t *testing.T) {
		if got := c.TokenIndex(src, src+"more", 0); got != -1 {
			t.Errorf("TokenIndex() = %d, want -1", got)
		}
	})

	t.Run("negative offset is not code", func(t *testing.T) {
		if c.IsCode(-1) {
			t.Error("a negative offset should not be code")
		}
	})
}

// Every prefix of each input must classify without panicking or overrunning.
func TestCodeHandlesTruncatedInput(t *testing.T) {
	for _, full := range []string{
		`x = "abc\";`,
		"x; /* c */ y;",
		"x; // c\ny;",
		`c = '\'';`,
		"// splice \\\nmore\n",
		"set(X \"a\") # c\n",
		"#[==[ b ]==] set(X 1)\n",
	} {
		for i := 0; i <= len(full); i++ {
			prefix := full[:i]
			if got := len(CCode(prefix)); got != len(prefix) {
				t.Fatalf("CCode(%q) length = %d, want %d", prefix, got, len(prefix))
			}
			if got := len(CMakeCode(prefix)); got != len(prefix) {
				t.Fatalf("CMakeCode(%q) length = %d, want %d", prefix, got, len(prefix))
			}
		}
	}
}

// A trailing backslash in a literal or comment must not walk past the end.
func TestCodeTrailingBackslash(t *testing.T) {
	for _, src := range []string{`x = "a\`, "// c \\", `c = '\`} {
		got := codeString(src, CCode(src))
		if len(got) != len(src) {
			t.Fatalf("CCode(%q) produced %d bytes, want %d", src, len(got), len(src))
		}
	}
}

func TestCMakeBracketCommentTermination(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "unterminated runs to the end",
			src:  "#[[ forever\nset(X 1)",
			want: "...........\n........",
		},
		{
			name: "a closer with the wrong equals count does not close it",
			src:  "#[=[ ]] still ]=] set(X 1)",
			want: "................. set(X 1)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := codeString(tt.src, CMakeCode(tt.src)); got != tt.want {
				t.Errorf("CMakeCode()\n got: %q\nwant: %q", got, tt.want)
			}
		})
	}
}
