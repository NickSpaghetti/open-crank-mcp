package scan

import "strings"

// Code records, byte by byte, whether a source file's bytes are code or are
// comment and literal text that only looks like code.
//
// This is the piece a regular expression fundamentally cannot supply. Deciding
// whether an offset is inside a comment needs state carried from the start of
// the file, and a pattern has none - which is why a call named in a comment, or
// spelled out inside a string a game logs, used to be rewritten as if it were
// the real thing.
//
// A nil Code means "unclassified": every byte counts as code. That is what lets
// the plain search helpers below share one implementation with the code-aware
// ones instead of keeping two copies of the tricky part.
type Code []bool

// IsCode reports whether the byte at i is code. An offset outside the file is
// not code, so a caller doesn't have to bounds-check before asking.
func (c Code) IsCode(i int) bool {
	if c == nil {
		return true
	}
	return i >= 0 && i < len(c) && c[i]
}

// Every search below tests only the FIRST byte of a match.
//
// That is sufficient, not a shortcut: a comment or a literal cannot begin in the
// middle of a token, so if a match starts in code, none of it is prose. Testing
// the whole span would be actively wrong - `#include "mcp_harness.h"` is code
// that contains a literal, and requiring every byte to be code would make it
// indistinguishable from the same line inside a comment.

// Index returns the offset of the first occurrence of sub at or after from whose
// start is code, or -1.
func (c Code) Index(content, sub string, from int) int {
	if from < 0 {
		from = 0
	}
	for i := from; i <= len(content); {
		at := strings.Index(content[i:], sub)
		if at < 0 {
			return -1
		}
		at += i
		if c.IsCode(at) {
			return at
		}
		i = at + 1
	}
	return -1
}

// LastIndex returns the offset of the last occurrence of sub starting before
// before whose start is code, or -1. Searching backwards is what lets a caller
// rewrite matches last-to-first, so each edit leaves the offsets of the ones
// still to come untouched.
func (c Code) LastIndex(content, sub string, before int) int {
	if before > len(content) {
		before = len(content)
	}
	for before >= 0 {
		at := strings.LastIndex(content[:before], sub)
		if at < 0 {
			return -1
		}
		if c.IsCode(at) {
			return at
		}
		before = at
	}
	return -1
}

// Contains reports whether sub appears anywhere in content as code.
//
// The guards that keep this tool idempotent are the main users: a project whose
// only mention of mcp_harness_init is in a comment explaining that it was
// removed has not been set up, and treating that mention as "already wired"
// left the harness copied in but never called.
func (c Code) Contains(content, sub string) bool {
	return c.Index(content, sub, 0) >= 0
}

// TokenIndex finds the next occurrence of token at or after from that is a whole
// identifier in code, or -1. This is what the regex \b assertions did, and the
// case a hand-written search gets wrong first: searching for "set" finds one
// inside "offset", and searching for "kEventInit" finds one inside
// "kEventInitDone".
func (c Code) TokenIndex(content, token string, from int) int {
	if token == "" || from < 0 {
		return -1
	}
	for i := from; i <= len(content)-len(token); {
		at := strings.Index(content[i:], token)
		if at < 0 {
			return -1
		}
		at += i
		i = at + len(token)
		if at > 0 && IsWord(content[at-1]) {
			continue
		}
		if i < len(content) && IsWord(content[i]) {
			continue
		}
		if !c.IsCode(at) {
			continue
		}
		return at
	}
	return -1
}

// CCode classifies a C translation unit.
//
// Not code: a // or /* */ comment including its delimiters, and a "..." or
// '...' literal including its quotes. Everything else is, preprocessor
// directives included.
//
// The cases that make this more than a one-liner, each of which appears in real
// game source: an escaped quote inside a string ("say \"hi\""), a character
// literal holding a quote ('"'), a // inside a string that is not a comment, a
// " inside a comment that is not a string, /* */ not nesting, and a backslash at
// end of line splicing the next line into a // comment.
//
// Trigraphs (??/ for a backslash) are not handled. They were removed in C23 and
// no Playdate game has one.
func CCode(content string) Code {
	code := make(Code, len(content))
	for i := 0; i < len(content); {
		switch {
		case strings.HasPrefix(content[i:], "//"):
			i = cLineCommentEnd(content, i)
		case strings.HasPrefix(content[i:], "/*"):
			i = cBlockCommentEnd(content, i)
		case content[i] == '"' || content[i] == '\'':
			i = cLiteralEnd(content, i)
		default:
			code[i] = true
			i++
		}
	}
	return code
}

// cLineCommentEnd returns the offset of the newline that ends the // comment at
// i, or the end of the file. The newline itself is left for the caller to mark
// as code: it separates lines whatever came before it.
//
// A backslash immediately before the line break splices the next line into the
// comment. That is legal C and does happen, usually by accident at the end of a
// commented-out macro, and getting it wrong means treating the spliced line as
// code.
func cLineCommentEnd(content string, i int) int {
	for i < len(content) {
		if content[i] == '\\' {
			j := i + 1
			if j < len(content) && content[j] == '\r' {
				j++
			}
			if j < len(content) && content[j] == '\n' {
				i = j + 1
				continue
			}
			i++
			continue
		}
		if content[i] == '\n' {
			return i
		}
		i++
	}
	return i
}

// cBlockCommentEnd returns the offset just past the "*/" closing the comment at
// i. Block comments do not nest, so the first "*/" ends it however many "/*"
// appear in between. An unterminated one runs to the end of the file, which is
// what a compiler does too.
func cBlockCommentEnd(content string, i int) int {
	end := strings.Index(content[i+2:], "*/")
	if end < 0 {
		return len(content)
	}
	return i + 2 + end + 2
}

// cLiteralEnd returns the offset just past the string or character literal at i.
//
// An unterminated literal ends at the line break rather than swallowing the rest
// of the file. C requires a backslash to continue one across lines, which the
// escape branch already handles, so a raw newline here means the source is
// malformed - and a classifier that ran to EOF on it would mark an entire
// working file as non-code and silently patch nothing.
func cLiteralEnd(content string, i int) int {
	quote := content[i]
	for j := i + 1; j < len(content); j++ {
		switch content[j] {
		case '\\':
			j++
		case quote:
			return j + 1
		case '\n':
			return j
		}
	}
	return len(content)
}

// CMakeCode classifies a CMake listfile.
//
// Only comments are excluded, and unlike C a quoted argument stays code. The
// difference is what the quoted text is for: a C string literal is data the game
// prints at runtime and must never be rewritten, while a quoted CMake argument
// is a value the build uses - "src/mcp_harness.c" is exactly the argument
// teardown has to be able to remove.
//
// Quoting is still tracked, because a "#" inside a quoted argument does not
// start a comment.
func CMakeCode(content string) Code {
	code := make(Code, len(content))
	for i := 0; i < len(content); {
		switch {
		case content[i] == '#':
			if open, ok := cmakeBracketOpen(content, i+1); ok {
				i = cmakeBracketEnd(content, open)
				continue
			}
			i = cmakeLineCommentEnd(content, i)
		case content[i] == '"':
			end := cmakeQuotedEnd(content, i)
			for ; i < end; i++ {
				code[i] = true
			}
		default:
			code[i] = true
			i++
		}
	}
	return code
}

func cmakeLineCommentEnd(content string, i int) int {
	if nl := strings.IndexByte(content[i:], '\n'); nl >= 0 {
		return i + nl
	}
	return len(content)
}

// cmakeBracketOpen reports whether a bracket argument opens at i, i.e. "[",
// any number of "=", then "[". It returns the offset of the second "[".
func cmakeBracketOpen(content string, i int) (open int, ok bool) {
	if i >= len(content) || content[i] != '[' {
		return 0, false
	}
	j := i + 1
	for j < len(content) && content[j] == '=' {
		j++
	}
	if j >= len(content) || content[j] != '[' {
		return 0, false
	}
	return i, true
}

// cmakeBracketEnd returns the offset just past the "]=*]" matching the bracket
// opened at open. The number of "=" has to match, which is the whole reason
// CMake has the form: it lets the content hold "]]".
func cmakeBracketEnd(content string, open int) int {
	equals := 0
	for open+1+equals < len(content) && content[open+1+equals] == '=' {
		equals++
	}
	closer := "]" + strings.Repeat("=", equals) + "]"
	start := open + equals + 2
	if start > len(content) {
		return len(content)
	}
	at := strings.Index(content[start:], closer)
	if at < 0 {
		return len(content)
	}
	return start + at + len(closer)
}

// cmakeQuotedEnd returns the offset just past the quoted argument at i.
// Backslash escapes apply, and an unterminated quote ends at the line break for
// the same reason it does in C.
func cmakeQuotedEnd(content string, i int) int {
	for j := i + 1; j < len(content); j++ {
		switch content[j] {
		case '\\':
			j++
		case '"':
			return j + 1
		case '\n':
			return j
		}
	}
	return len(content)
}
