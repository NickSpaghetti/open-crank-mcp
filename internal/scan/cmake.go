package scan

import "strings"

// FindCMakeCall locates the next call to the named command at or after from,
// returning the offsets of its opening and closing parens.
//
// The command name is matched case-insensitively, because CMake command names
// are, and at a token boundary, so "offset(" is not a "set(" call. Parens are
// counted rather than scanned for the first ")", so a nested call or a
// parenthesized comment inside the argument list doesn't end it early - the
// failure that made the old pattern skip an entire ordinary CMakeLists.
func FindCMakeCall(content, name string, from int) (openAt, closeAt int, ok bool) {
	lower := strings.ToLower(content)
	for i := from; ; {
		at := strings.Index(lower[i:], name)
		if at < 0 {
			return 0, 0, false
		}
		at += i
		i = at + len(name)
		if at > 0 && IsWord(content[at-1]) {
			continue
		}
		openAt, closeAt, ok := CallParens(content, i)
		if !ok {
			if openAt < len(content) && content[openAt] == '(' {
				// An unterminated call: there is no later one either.
				return 0, 0, false
			}
			continue
		}
		return openAt, closeAt, true
	}
}

// CMakeCall is one call to a command, with the offsets of its argument list
// (between the parens, exclusive).
type CMakeCall struct {
	Name                  string
	ArgsStart, ArgsEnd    int
	OpenParen, CloseParen int
}

// CMakeCalls returns every call to any of the named commands, in the order they
// appear.
func CMakeCalls(content string, names ...string) []CMakeCall {
	var calls []CMakeCall
	for _, name := range names {
		for i := 0; i < len(content); {
			openAt, closeAt, ok := FindCMakeCall(content, name, i)
			if !ok {
				break
			}
			calls = append(calls, CMakeCall{
				Name:       name,
				ArgsStart:  openAt + 1,
				ArgsEnd:    closeAt,
				OpenParen:  openAt,
				CloseParen: closeAt,
			})
			i = closeAt + 1
		}
	}
	sortCallsByPosition(calls)
	return calls
}

// sortCallsByPosition puts calls back in source order after the per-name
// passes above. Insertion sort: a CMakeLists has a handful of targets, and the
// list is already almost sorted.
func sortCallsByPosition(calls []CMakeCall) {
	for i := 1; i < len(calls); i++ {
		for j := i; j > 0 && calls[j-1].OpenParen > calls[j].OpenParen; j-- {
			calls[j-1], calls[j] = calls[j], calls[j-1]
		}
	}
}

// CMakeCallOnOwnLine finds a call to name that takes up a whole line of its own,
// and returns the offset at the end of that line - where a following statement
// can be inserted.
//
// "Its own line" allows leading whitespace and a trailing comment, neither of
// which the line-anchored pattern this replaced accepted - so a project() line
// with a "# the game" comment after it went unrecognized, and the
// include_directories(src) that depends on finding it was never added. The
// offset is the line's end rather than the closing paren's, so such a comment
// stays on the line it was written on.
func CMakeCallOnOwnLine(content, name string) (endOfLine int, ok bool) {
	for i := 0; i < len(content); {
		openAt, closeAt, found := FindCMakeCall(content, name, i)
		if !found {
			return 0, false
		}
		i = closeAt + 1

		if !atLineStart(content, openAt, name) {
			continue
		}
		if !blankToEndOfLine(content, closeAt+1) {
			continue
		}
		return endOfLineFrom(content, closeAt+1), true
	}
	return 0, false
}

// endOfLineFrom returns the offset of the newline that ends the line at or after
// i, or the end of the content.
//
// On a CRLF file this deliberately lands after the carriage return rather than
// before it. Text inserted here begins with its own newline, so stopping short
// of the "\r" would leave the existing line ending in a bare "\n" - rewriting a
// line the caller never meant to touch.
func endOfLineFrom(content string, i int) int {
	nl := strings.IndexByte(content[i:], '\n')
	if nl < 0 {
		return len(content)
	}
	return i + nl
}

// atLineStart reports whether the command name ending at the given open paren
// is the first thing on its line.
func atLineStart(content string, openAt int, name string) bool {
	i := SkipSpaceBefore(content, openAt)
	if i < len(name) {
		return false
	}
	i -= len(name)
	for i > 0 {
		c := content[i-1]
		if c == '\n' {
			return true
		}
		if c != ' ' && c != '\t' && c != '\r' {
			return false
		}
		i--
	}
	return true
}

// blankToEndOfLine reports whether the rest of the line from i is whitespace, or
// whitespace followed by a comment.
func blankToEndOfLine(content string, i int) bool {
	for ; i < len(content); i++ {
		switch c := content[i]; {
		case c == '\n':
			return true
		case c == '#':
			return true
		case c == ' ' || c == '\t' || c == '\r':
		default:
			return false
		}
	}
	return true
}

// CMakeSetBlock is a multi-line set(<name> ...) call: the offsets of its source
// list and of the indentation in front of its closing paren, which is what an
// inserted entry copies so it lines up with the entries already there.
//
// Only multi-line calls are reported. A single-line set() has no line-shaped
// body to add an entry to, and callers handle that shape by patching the target
// that references the variable instead.
type CMakeSetBlock struct {
	Name                   string
	BodyStart, BodyEnd     int
	IndentStart, IndentEnd int
}

// Body returns the block's source list.
func (b CMakeSetBlock) Body(content string) string {
	return content[b.BodyStart:b.BodyEnd]
}

// Indent returns the whitespace in front of the block's closing paren.
func (b CMakeSetBlock) Indent(content string) string {
	return content[b.IndentStart:b.IndentEnd]
}

// CMakeSetBlocks returns every multi-line set() call in content, in order.
func CMakeSetBlocks(content string) []CMakeSetBlock {
	var blocks []CMakeSetBlock
	for i := 0; i < len(content); {
		openAt, closeAt, ok := FindCMakeCall(content, "set", i)
		if !ok {
			return blocks
		}
		i = closeAt + 1

		nameStart := SkipSpace(content, openAt+1)
		name, nameEnd := Identifier(content, nameStart)
		if name == "" || (name[0] >= '0' && name[0] <= '9') {
			continue
		}

		// The body starts on the line after the variable name, so the run of
		// whitespace after it has to hold a newline.
		bodyStart, ok := afterLastNewline(content, nameEnd, SkipSpace(content, nameEnd))
		if !ok {
			continue
		}
		// The closing paren has to be on a line of its own too, and the
		// whitespace in front of it is the indentation an inserted entry
		// copies.
		indentRunStart := SkipSpaceBefore(content, closeAt)
		newline := strings.IndexByte(content[indentRunStart:closeAt], '\n')
		if newline < 0 {
			continue
		}
		bodyEnd := indentRunStart + newline
		if bodyEnd < bodyStart {
			continue
		}
		blocks = append(blocks, CMakeSetBlock{
			Name:        name,
			BodyStart:   bodyStart,
			BodyEnd:     bodyEnd,
			IndentStart: bodyEnd + 1,
			IndentEnd:   closeAt,
		})
	}
	return blocks
}

// afterLastNewline returns the offset just past the last newline in
// content[from:to], which is where the next line's content begins.
func afterLastNewline(content string, from, to int) (int, bool) {
	last := strings.LastIndexByte(content[from:to], '\n')
	if last < 0 {
		return 0, false
	}
	return from + last + 1, true
}

// CMakeArgTokens splits a CMake argument list into individual arguments,
// dropping the quotes around quoted ones. Parentheses are treated as
// delimiters rather than kept: callers only ask whether a token is a source
// path or a variable reference, and neither contains one.
func CMakeArgTokens(args string) []string {
	var tokens []string
	for i := 0; i < len(args); {
		if IsSpace(args[i]) || args[i] == '(' || args[i] == ')' {
			i++
			continue
		}
		if args[i] == '"' {
			end := strings.IndexByte(args[i+1:], '"')
			if end < 0 {
				tokens = append(tokens, args[i+1:])
				break
			}
			tokens = append(tokens, args[i+1:i+1+end])
			i += end + 2
			continue
		}
		// The byte at i is known to start a token: space, "(", ")" and quote are
		// all handled above. Consuming it before the loop rather than leaving the
		// loop to discover it makes forward progress structural, so this cannot
		// append a zero-length token and spin - which is not a theoretical
		// concern, it is what took a CI runner down by growing this slice until
		// the machine ran out of memory.
		start := i
		i++
		for i < len(args) && !IsSpace(args[i]) && args[i] != '(' && args[i] != ')' && args[i] != '"' {
			i++
		}
		tokens = append(tokens, args[start:i])
	}
	return tokens
}

// CMakeVarReference returns the variable name in a token that is exactly a
// ${...} reference. A token that merely contains one
// (${CMAKE_CURRENT_SOURCE_DIR}/src/main.c) is a path, and is compared as one.
func CMakeVarReference(token string) (string, bool) {
	if !strings.HasPrefix(token, "${") || !strings.HasSuffix(token, "}") {
		return "", false
	}
	name := token[2 : len(token)-1]
	if name == "" || strings.ContainsAny(name, "${}/\\") {
		return "", false
	}
	return name, true
}

// CMakeSetBody returns the argument list of the first set(<name> ...) call in
// content, minus the name itself. The name is compared as a whole token, so
// asking for GAME does not find GAME_SOURCES.
func CMakeSetBody(content, name string) (string, bool) {
	for i := 0; i < len(content); {
		openAt, closeAt, ok := FindCMakeCall(content, "set", i)
		if !ok {
			return "", false
		}
		i = closeAt + 1
		args := content[openAt+1 : closeAt]
		tokens := CMakeArgTokens(args)
		if len(tokens) == 0 || tokens[0] != name {
			continue
		}
		return strings.TrimPrefix(strings.TrimSpace(args), name), true
	}
	return "", false
}

// FilterCMakeArgs rewrites content with every argument drop reports true for
// removed, along with the whitespace in front of it, and reports whether
// anything was removed.
//
// Whole arguments, not substrings: removing the text "src/mcp_harness.c" on
// its own truncated a ${CMAKE_CURRENT_SOURCE_DIR}/src/mcp_harness.c entry and
// left a dangling prefix behind, i.e. a CMakeLists that no longer configures.
// Parens are their own tokens, so the ")" closing a call whose last argument
// was removed survives. Every byte of content ends up either in a whitespace
// run or a token, so a call that removes nothing returns the input unchanged.
func FilterCMakeArgs(content string, drop func(arg string) bool) (string, bool) {
	var out strings.Builder
	removed := false
	// A comment naming the harness source is prose about the build, not part of
	// it. Editing one used to leave a mangled half-sentence behind - harmless to
	// CMake, and exactly the kind of unexplained edit that makes a user distrust
	// everything else the tool did to their file.
	code := CMakeCode(content)
	for i := 0; i < len(content); {
		spaceStart := i
		for i < len(content) && IsSpace(content[i]) {
			i++
		}
		space := content[spaceStart:i]

		tokenStart := i
		switch {
		case i >= len(content):
		case content[i] == '"':
			i++
			for i < len(content) && content[i] != '"' {
				i++
			}
			if i < len(content) {
				i++
			}
		case content[i] == '(' || content[i] == ')':
			i++
		default:
			// Same as CMakeArgTokens: the byte at i is known to start a token, so
			// consume it up front and the loop can only move forward.
			i++
			for i < len(content) && !IsSpace(content[i]) && !strings.ContainsRune(`"()`, rune(content[i])) {
				i++
			}
		}
		token := content[tokenStart:i]

		if code.IsCode(tokenStart) && drop(strings.Trim(token, `"`)) {
			removed = true
			continue
		}
		out.WriteString(space)
		out.WriteString(token)
	}
	return out.String(), removed
}
