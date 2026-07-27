package setup

import "strings"

// Every line setup inserts into a game's own source file sits between a
// begin/end marker comment pair, each on its own line - never bare. That's
// what makes both idempotency ("is a marker block already present") and
// teardown ("strip everything between these markers") simple, reliable
// string operations instead of fragile content-diffing. CMakeLists.txt is
// the one exception - see patchCMakeLists, a CMake comment mid-argument-list
// would comment out the rest of that line, so it can't use this scheme.
const (
	luaMarkerBegin = "-- BEGIN MCP HARNESS"
	luaMarkerEnd   = "-- END MCP HARNESS"
	cMarkerBegin   = "/* BEGIN MCP HARNESS */"
	cMarkerEnd     = "/* END MCP HARNESS */"
)

// markerBlock wraps content between beginMarker/endMarker, each on its own
// line, with a trailing newline - ready to append or insert as-is.
func markerBlock(beginMarker, endMarker, content string) string {
	return beginMarker + "\n" + content + "\n" + endMarker + "\n"
}

func hasMarkerBlock(content, beginMarker string) bool {
	return strings.Contains(content, beginMarker)
}

// stripMarkerBlocks removes every marker block (begin marker through end
// marker inclusive, plus one trailing newline if present) from content.
// Reports whether anything was actually removed. A begin marker with no
// matching end marker is left alone rather than guessed at.
func stripMarkerBlocks(content, beginMarker, endMarker string) (string, bool) {
	changed := false
	for {
		start := strings.Index(content, beginMarker)
		if start < 0 {
			break
		}
		relEnd := strings.Index(content[start:], endMarker)
		if relEnd < 0 {
			break
		}
		end := start + relEnd + len(endMarker)
		if end < len(content) && content[end] == '\n' {
			end++
		}
		content = content[:start] + content[end:]
		changed = true
	}
	return content, changed
}
