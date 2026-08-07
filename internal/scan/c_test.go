package scan

import (
	"strings"
	"testing"
)

func TestSkipSpace(t *testing.T) {
	const content = "a \t\n b"
	if got := SkipSpace(content, 1); got != 5 {
		t.Errorf("SkipSpace() = %d, want 5", got)
	}
	if got := SkipSpace(content, 0); got != 0 {
		t.Errorf("SkipSpace() on a non-space byte = %d, want 0", got)
	}
	if got := SkipSpace(content, len(content)); got != len(content) {
		t.Errorf("SkipSpace() at the end = %d, want %d", got, len(content))
	}
}

func TestSkipSpaceBefore(t *testing.T) {
	const content = "a \t\n b"
	if got := SkipSpaceBefore(content, 5); got != 1 {
		t.Errorf("SkipSpaceBefore() = %d, want 1", got)
	}
	if got := SkipSpaceBefore(content, 1); got != 1 {
		t.Errorf("SkipSpaceBefore() with no run = %d, want 1", got)
	}
	if got := SkipSpaceBefore(" ", 1); got != 0 {
		t.Errorf("SkipSpaceBefore() at the start = %d, want 0", got)
	}
}

func TestIdentifier(t *testing.T) {
	tests := []struct {
		content  string
		at       int
		wantName string
		wantEnd  int
	}{
		{content: "pd->system", at: 0, wantName: "pd", wantEnd: 2},
		{content: "g_pd2 = 0", at: 0, wantName: "g_pd2", wantEnd: 5},
		// Identifiers are read as the patterns did, so a leading digit is not
		// rejected here - callers that care check it themselves.
		{content: "2fast", at: 0, wantName: "2fast", wantEnd: 5},
		{content: "->pd", at: 0, wantName: "", wantEnd: 0},
		{content: "pd", at: 2, wantName: "", wantEnd: 2},
	}

	for _, tt := range tests {
		name, end := Identifier(tt.content, tt.at)
		if name != tt.wantName || end != tt.wantEnd {
			t.Errorf("Identifier(%q, %d) = %q, %d, want %q, %d", tt.content, tt.at, name, end, tt.wantName, tt.wantEnd)
		}
	}
}

func TestIdentifierBefore(t *testing.T) {
	const content = "game->pd->system"
	name, start := IdentifierBefore(content, strings.Index(content, "->system"))
	if name != "pd" || start != 6 {
		t.Errorf("IdentifierBefore() = %q, %d, want \"pd\", 6", name, start)
	}
	if name, start := IdentifierBefore("->pd", 2); name != "" || start != 2 {
		t.Errorf("IdentifierBefore() on an arrow = %q, %d, want \"\", 2", name, start)
	}
	if name, start := IdentifierBefore("pd", 0); name != "" || start != 0 {
		t.Errorf("IdentifierBefore() at the start = %q, %d, want \"\", 0", name, start)
	}
}

func TestLiteral(t *testing.T) {
	const content = "pd->system"
	if end, ok := Literal(content, 2, "->"); !ok || end != 4 {
		t.Errorf("Literal() = %d, %v, want 4, true", end, ok)
	}
	// Offset zero is a valid place to match, and so is a literal that ends
	// exactly at the end of the content.
	if end, ok := Literal(content, 0, "pd"); !ok || end != 2 {
		t.Errorf("Literal() at offset 0 = %d, %v, want 2, true", end, ok)
	}
	if end, ok := Literal("pd", 0, "pd"); !ok || end != 2 {
		t.Errorf("Literal() ending at EOF = %d, %v, want 2, true", end, ok)
	}
	if _, ok := Literal(content, 2, "=="); ok {
		t.Error("Literal() matched the wrong bytes")
	}
	// Past the end and before the start both report no match rather than
	// panicking - callers chain these without checking bounds first.
	if _, ok := Literal(content, len(content)-1, "->"); ok {
		t.Error("Literal() matched past the end of the content")
	}
	if _, ok := Literal(content, -1, "p"); ok {
		t.Error("Literal() matched at a negative offset")
	}
}

func TestTokenIndex(t *testing.T) {
	tests := []struct {
		name    string
		content string
		token   string
		from    int
		want    int
	}{
		{name: "plain", content: "if (event == kEventInit)", token: "kEventInit", want: 13},
		{
			// The trap a hand-written search gets wrong first, in both
			// directions.
			name:    "inside_a_longer_identifier_before",
			content: "kEventInitDone; kEventInit;",
			token:   "kEventInit",
			want:    16,
		},
		{
			name:    "inside_a_longer_identifier_after",
			content: "my_kEventInit; kEventInit;",
			token:   "kEventInit",
			want:    15,
		},
		{name: "from_skips_earlier_matches", content: "kEventInit kEventInit", token: "kEventInit", from: 1, want: 11},
		{
			// The token is the entire content, so the search has to consider
			// the offset where content length and token length are equal.
			name:    "token_is_the_whole_content",
			content: "kEventInit",
			token:   "kEventInit",
			want:    0,
		},
		{
			name:    "token_at_the_very_end",
			content: "x kEventInit",
			token:   "kEventInit",
			want:    2,
		},
		{name: "absent", content: "kEventInitDone", token: "kEventInit", want: -1},
		{name: "empty_token", content: "anything", token: "", want: -1},
		{name: "negative_from", content: "kEventInit", token: "kEventInit", from: -1, want: -1},
		{name: "token_longer_than_content", content: "k", token: "kEventInit", want: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TokenIndex(tt.content, tt.token, tt.from); got != tt.want {
				t.Errorf("TokenIndex() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestBalancedParens(t *testing.T) {
	tests := []struct {
		name    string
		content string
		at      int
		want    int
		wantOK  bool
	}{
		{name: "simple", content: "(void)", at: 0, want: 5, wantOK: true},
		{
			// The case the "no parens inside" character class could not do.
			name:    "nested",
			content: "(int (*tick)(void), void *ud)",
			at:      0,
			want:    28,
			wantOK:  true,
		},
		{name: "not_at_a_paren", content: "f(void)", at: 0},
		{name: "unterminated", content: "(void", at: 0},
		// Exactly at the end, and past it: neither may read a byte that is not
		// there.
		{name: "at_the_end", content: "()", at: 2},
		{name: "past_the_end", content: "()", at: 5},
		{name: "negative", content: "()", at: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := BalancedParens(tt.content, tt.at)
			if ok != tt.wantOK || (ok && got != tt.want) {
				t.Errorf("BalancedParens() = %d, %v, want %d, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestCallParens(t *testing.T) {
	const content = "update (void *ud) {"
	openAt, closeAt, ok := CallParens(content, len("update"))
	if !ok || openAt != 7 || closeAt != 16 {
		t.Errorf("CallParens() = %d, %d, %v, want 7, 16, true", openAt, closeAt, ok)
	}
	if _, _, ok := CallParens("update;", len("update")); ok {
		t.Error("CallParens() found a call where there is none")
	}
}

func TestBlockOpenAfter(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    int
		wantOK  bool
	}{
		{name: "if_branch", content: " == kEventInit) {\n\tsetup();", want: 16, wantOK: true},
		{name: "compound_condition", content: " == kEventInit && !inited) {", want: 27, wantOK: true},
		{name: "brace_on_the_next_line", content: ")\n{\n", want: 2, wantOK: true},
		{
			// A braceless branch, and an expression using the token: both end
			// at a ";" with no block to insert into.
			name:    "semicolon_first",
			content: ")\n\t\tsetup();\n",
			wantOK:  false,
		},
		{name: "expression", content: ";\n\tif (wantInit) {\n", wantOK: false},
		{name: "nothing_left", content: " == kEventInit)", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := BlockOpenAfter(tt.content, 0)
			if ok != tt.wantOK || (ok && got != tt.want) {
				t.Errorf("BlockOpenAfter() = %d, %v, want %d, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
