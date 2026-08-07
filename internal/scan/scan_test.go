package scan

import (
	"strings"
	"testing"
)

func TestIsSpaceAndIsWord(t *testing.T) {
	for _, c := range []byte{' ', '\t', '\n', '\r'} {
		if !IsSpace(c) {
			t.Errorf("IsSpace(%q) = false, want true", c)
		}
	}
	for _, c := range []byte{'a', '0', '_', '-', '(', 0} {
		if IsSpace(c) {
			t.Errorf("IsSpace(%q) = true, want false", c)
		}
	}
	for _, c := range []byte{'a', 'z', 'A', 'Z', '0', '9', '_'} {
		if !IsWord(c) {
			t.Errorf("IsWord(%q) = false, want true", c)
		}
	}
	// ASCII only, on purpose - see the doc comment. A UTF-8 identifier byte
	// must not count, or the port would start matching identifiers the
	// patterns it replaced never did.
	for _, c := range []byte{'-', '.', ' ', '>', 0xC3, 0xA9} {
		if IsWord(c) {
			t.Errorf("IsWord(%#x) = true, want false", c)
		}
	}
}

func TestPrecededByKeyword(t *testing.T) {
	tests := []struct {
		name    string
		content string
		token   string
		keyword string
		want    bool
	}{
		{name: "directly_before", content: "case kEventInit:", token: "kEventInit", keyword: "case", want: true},
		{name: "extra_spaces", content: "case   kEventInit:", token: "kEventInit", keyword: "case", want: true},
		{name: "across_a_newline", content: "case\n\tkEventInit:", token: "kEventInit", keyword: "case", want: true},
		{name: "at_the_start_of_content", content: "typedef PlaydateAPI *R;", token: "PlaydateAPI", keyword: "typedef", want: true},
		{name: "absent", content: "if (event == kEventInit)", token: "kEventInit", keyword: "case"},
		{
			// Nothing but whitespace in front of the token, which a file
			// starting with a blank line produces. The backward walk has to
			// stop at offset 0 rather than read the byte before it.
			name:    "only_whitespace_before_the_token",
			content: "\n\tPlaydateAPI *pd;",
			token:   "PlaydateAPI",
			keyword: "typedef",
		},
		{
			// Preceded by a non-word byte partway into the content, which is
			// the ordinary case inside a switch.
			name:    "after_a_newline",
			content: "switch (event) {\n\tcase kEventInit:",
			token:   "kEventInit",
			keyword: "case",
			want:    true,
		},
		{
			// The keyword has to be a whole token itself, or "lowercase x"
			// would read as a case label.
			name:    "keyword_is_the_tail_of_a_longer_word",
			content: "lowercase kEventInit:",
			token:   "kEventInit",
			keyword: "case",
		},
		{
			// A different identifier ending in the keyword's letters.
			name:    "identifier_ending_in_the_keyword",
			content: "mytypedef PlaydateAPI *R;",
			token:   "PlaydateAPI",
			keyword: "typedef",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			at := strings.Index(tt.content, tt.token)
			if at < 0 {
				t.Fatalf("test setup: %q not found in %q", tt.token, tt.content)
			}
			if got := PrecededByKeyword(tt.content, at, tt.keyword); got != tt.want {
				t.Errorf("PrecededByKeyword() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLineNumber(t *testing.T) {
	const content = "one\ntwo\nthree\n"
	tests := []struct {
		offset int
		want   int
	}{
		{offset: 0, want: 1},
		{offset: 3, want: 1},
		{offset: 4, want: 2},
		{offset: 8, want: 3},
		{offset: len(content), want: 4},
		// Past the end rather than panicking: callers pass offsets from
		// content they have already rewritten.
		{offset: len(content) + 10, want: 4},
	}

	for _, tt := range tests {
		if got := LineNumber(content, tt.offset); got != tt.want {
			t.Errorf("LineNumber(_, %d) = %d, want %d", tt.offset, got, tt.want)
		}
	}

	// CRLF counts lines the same way.
	if got := LineNumber("one\r\ntwo\r\n", 5); got != 2 {
		t.Errorf("LineNumber() = %d on CRLF content, want 2", got)
	}
}

func TestIsCSourceFile(t *testing.T) {
	for _, path := range []string{"src/main.c", "main.C", "a.cc", "a.cpp", "a.cxx", "boot.s", "boot.S", "a.asm", "a.m"} {
		if !IsCSourceFile(path) {
			t.Errorf("IsCSourceFile(%q) = false, want true", path)
		}
	}
	for _, path := range []string{"src/main.h", "CMakeLists.txt", "main.cs", "README", "a.lua", ""} {
		if IsCSourceFile(path) {
			t.Errorf("IsCSourceFile(%q) = true, want false", path)
		}
	}
}

func TestSamePath(t *testing.T) {
	tests := []struct {
		path string
		rel  string
		want bool
	}{
		{path: "src/main.c", rel: "src/main.c", want: true},
		{path: "${CMAKE_CURRENT_SOURCE_DIR}/src/main.c", rel: "src/main.c", want: true},
		{path: "../shared/src/main.c", rel: "src/main.c", want: true},
		// A directory in front of rel is fine, that is the whole point.
		{path: "src/main.c", rel: "main.c", want: true},
		// Anchored on a separator, so a longer filename is not a match.
		{path: "src/domain.c", rel: "main.c"},
		{path: "src/mymain.c", rel: "main.c"},
	}

	for _, tt := range tests {
		if got := SamePath(tt.path, tt.rel); got != tt.want {
			t.Errorf("SamePath(%q, %q) = %v, want %v", tt.path, tt.rel, got, tt.want)
		}
	}
}
