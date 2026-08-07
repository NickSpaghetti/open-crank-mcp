// Package scan holds the byte-level source scanning this project uses in place
// of regular expressions.
//
// The reason is in https://regexlicensing.org/: a pattern that reads a source
// file as a flat byte string can't tell code from a comment or a string
// literal, and can't be widened to accept ordinary spacing without becoming
// unreadable. internal/setup hit both walls patching real games, in ways that
// either damaged files or silently failed to patch them - see the edge-case
// tests there for the specific ones.
//
// Everything here is mechanism only. Whether a given argument or identifier
// means anything is the caller's decision, which keeps the policy in
// internal/setup where it can be read next to the thing it patches.
package scan

import "strings"

// IsSpace reports whether c is a byte C and CMake both treat as whitespace.
// Carriage return is included, so a file written on Windows scans the same as
// one written anywhere else.
func IsSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// IsWord reports whether c can appear in an identifier. ASCII only, matching
// what Go's regexp \w accepted before this package replaced it - a scanner
// using unicode.IsLetter here would start matching identifiers the old
// patterns never did, which is a behavior change rather than a port.
func IsWord(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// PrecededByKeyword reports whether the token starting at at is preceded by the
// given keyword, ignoring whitespace between them. The keyword has to be a
// whole token itself, so "case" doesn't match the tail of "lowercase".
func PrecededByKeyword(content string, at int, keyword string) bool {
	i := at
	for i > 0 && IsSpace(content[i-1]) {
		i--
	}
	if i < len(keyword) || content[i-len(keyword):i] != keyword {
		return false
	}
	return i-len(keyword) == 0 || !IsWord(content[i-len(keyword)-1])
}

// LineNumber returns the 1-based line the given byte offset falls on, for
// pointing a user at something in their own file.
func LineNumber(content string, offset int) int {
	if offset > len(content) {
		offset = len(content)
	}
	return 1 + strings.Count(content[:offset], "\n")
}

var cSourceExtensions = []string{".c", ".cc", ".cpp", ".cxx", ".s", ".asm", ".m"}

// IsCSourceFile reports whether a path names something a C toolchain compiles.
// Case-insensitive on the extension only: ".S" and ".s" are both assembly, but
// the rest of the path belongs to a filesystem that may not be.
func IsCSourceFile(path string) bool {
	lower := strings.ToLower(path)
	for _, ext := range cSourceExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// SamePath reports whether path names rel, allowing for any prefix in front of
// it - ${CMAKE_CURRENT_SOURCE_DIR}/src/main.c and src/main.c are the same file.
// The comparison is anchored on a separator, so "domain.c" is not "main.c".
func SamePath(path, rel string) bool {
	return path == rel || strings.HasSuffix(path, "/"+rel)
}
