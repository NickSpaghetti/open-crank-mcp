// Package scan is byte-level source scanning used instead of regular
// expressions, which can't tell code from comments or string literals.
package scan

import "strings"

// IsSpace reports whether c is a byte C and CMake both treat as whitespace,
// including \r so CRLF files scan the same.
func IsSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// IsWord reports whether c is an ASCII identifier byte.
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

// LineNumber returns the 1-based line the given byte offset falls on.
func LineNumber(content string, offset int) int {
	if offset > len(content) {
		offset = len(content)
	}
	return 1 + strings.Count(content[:offset], "\n")
}

var cSourceExtensions = []string{".c", ".cc", ".cpp", ".cxx", ".s", ".asm", ".m"}

// IsCSourceFile reports whether a path names something a C toolchain compiles,
// matching the extension case-insensitively.
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
