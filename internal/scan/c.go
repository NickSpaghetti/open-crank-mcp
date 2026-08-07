package scan

// The byte-level readers the C patching in internal/setup is built from. They
// are deliberately small and offset-based rather than producing a token stream:
// every caller needs to splice text back into the file at an exact position, so
// what they want from a scan is offsets into the original bytes, not a
// re-rendered form of it.

// SkipSpace returns the offset of the first non-whitespace byte at or after i.
func SkipSpace(content string, i int) int {
	for i < len(content) && IsSpace(content[i]) {
		i++
	}
	return i
}

// SkipSpaceBefore returns the start of the whitespace run ending at i, i.e. the
// offset just past the last non-whitespace byte before it.
func SkipSpaceBefore(content string, i int) int {
	for i > 0 && IsSpace(content[i-1]) {
		i--
	}
	return i
}

// Identifier reads the identifier starting at i, returning it and the offset
// just past it. The name is empty when there is no identifier there.
func Identifier(content string, i int) (name string, end int) {
	end = i
	for end < len(content) && IsWord(content[end]) {
		end++
	}
	return content[i:end], end
}

// IdentifierBefore reads the identifier ending just before i, returning it and
// the offset it starts at. Used to walk a member-access chain leftward from the
// arrow that anchors it.
func IdentifierBefore(content string, i int) (name string, start int) {
	start = i
	for start > 0 && IsWord(content[start-1]) {
		start--
	}
	return content[start:i], start
}

// Literal reports whether the exact bytes of lit appear at i, returning the
// offset just past them.
func Literal(content string, i int, lit string) (end int, ok bool) {
	if i < 0 || i+len(lit) > len(content) || content[i:i+len(lit)] != lit {
		return i, false
	}
	return i + len(lit), true
}

// TokenIndex finds the next occurrence of token at or after from that is a whole
// identifier rather than part of a longer one, or -1.
//
// This searches the file as flat text. Code.TokenIndex is the same search
// restricted to code, which is what a caller that must not match inside a
// comment wants.
func TokenIndex(content, token string, from int) int {
	return Code(nil).TokenIndex(content, token, from)
}

// BalancedParens takes an offset pointing at "(" and returns the offset of its
// matching ")". Counting depth is the whole point: the patterns this replaced
// used a "no parens allowed inside" character class, which silently skipped any
// argument list containing a nested call, a parenthesized comment or a quoted
// path with a paren in it.
func BalancedParens(content string, i int) (closeAt int, ok bool) {
	if i < 0 || i >= len(content) || content[i] != '(' {
		return 0, false
	}
	depth := 0
	for k := i; k < len(content); k++ {
		switch content[k] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return k, true
			}
		}
	}
	return 0, false
}

// CallParens reads a call's parentheses starting from the offset just past its
// name: optional whitespace, "(", then the matching ")".
func CallParens(content string, afterName int) (openAt, closeAt int, ok bool) {
	openAt = SkipSpace(content, afterName)
	closeAt, ok = BalancedParens(content, openAt)
	return openAt, closeAt, ok
}

// BlockOpenAfter finds the "{" that opens the block following i, and reports
// whether one was reached before the statement ended at a ";".
//
// This is how the kEventInit branch is located. Looking for the brace rather
// than for a particular condition shape is what lets a switch/case, a compound
// condition, a reversed comparison and redundant parens all work, none of which
// the pattern it replaced accepted.
func BlockOpenAfter(content string, i int) (offset int, ok bool) {
	return Code(nil).BlockOpenAfter(content, i)
}

// BlockOpenAfter is the same search restricted to code, so a "{" or a ";" inside
// a comment between the token and the real brace is stepped over rather than
// mistaken for the end of the search. A commented-out branch sitting above a
// live one puts both there.
func (c Code) BlockOpenAfter(content string, i int) (offset int, ok bool) {
	for ; i < len(content); i++ {
		if !c.IsCode(i) {
			continue
		}
		switch content[i] {
		case '{':
			return i, true
		case ';':
			return 0, false
		}
	}
	return 0, false
}
