package scan

import "strings"

// Code records, byte by byte, whether a source file's bytes are code or are
// comment and literal text that only looks like code. A nil Code treats every
// byte as code.
type Code []bool

// IsCode reports whether the byte at i is code. Out-of-range offsets are not.
func (c Code) IsCode(i int) bool {
	if c == nil {
		return true
	}
	return i >= 0 && i < len(c) && c[i]
}

// Searches test only a match's first byte: `#include "mcp_harness.h"` is code
// containing a literal, so requiring every byte be code would reject it.

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
// before whose start is code, or -1.
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
func (c Code) Contains(content, sub string) bool {
	return c.Index(content, sub, 0) >= 0
}

// TokenIndex finds the next occurrence of token at or after from that is a whole
// identifier in code, or -1, so "set" does not match inside "offset".
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

// CCode classifies a C translation unit. Comments and string/char literals,
// delimiters included, are not code. Trigraphs are not handled.
func CCode(content string) Code {
	code := make(Code, len(content))
	for i := 0; i < len(content); {
		switch {
		case strings.HasPrefix(content[i:], "//"):
			// i+2 guarantees progress past the "//".
			i = cLineCommentEnd(content, i+2)
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
// i, or the end of the file. A backslash before the line break splices the next
// line into the comment, per C.
func cLineCommentEnd(content string, i int) int {
	for ; i < len(content); i++ {
		switch content[i] {
		case '\\':
			j := i + 1
			if j < len(content) && content[j] == '\r' {
				j++
			}
			if j < len(content) && content[j] == '\n' {
				i = j // the loop's i++ steps past the spliced line break
			}
		case '\n':
			return i
		}
	}
	return i
}

// cBlockCommentEnd returns the offset just past the "*/" closing the comment at
// i, or the end of the file if unterminated. Block comments do not nest.
func cBlockCommentEnd(content string, i int) int {
	end := strings.Index(content[i+2:], "*/")
	if end < 0 {
		return len(content)
	}
	return i + 2 + end + 2
}

// cLiteralEnd returns the offset just past the string or character literal at i.
// An unterminated literal ends at the line break rather than swallowing the file.
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

// CMakeCode classifies a CMake listfile. Only comments are excluded: unlike C,
// a quoted argument is build input and stays code, though "#" inside one is not
// a comment.
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
// any number of "=", then "[". It returns the offset of the first "[".
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
// opened at open. The number of "=" has to match.
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
// Backslash escapes apply, and an unterminated quote ends at the line break.
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
