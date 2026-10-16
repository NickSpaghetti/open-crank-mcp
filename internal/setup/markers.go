package setup

import "strings"

// Everything setup inserts sits between begin/end markers so idempotency and
// teardown are plain string operations. CMake insertions inside an argument list
// can't be marked: a "#" comment there would swallow the rest of the call.
const (
	luaMarkerBegin   = "-- BEGIN MCP HARNESS"
	luaMarkerEnd     = "-- END MCP HARNESS"
	cMarkerBegin     = "/* BEGIN MCP HARNESS */"
	cMarkerEnd       = "/* END MCP HARNESS */"
	cmakeMarkerBegin = "# BEGIN MCP HARNESS"
	cmakeMarkerEnd   = "# END MCP HARNESS"
)

func markerBlock(beginMarker, endMarker, content string) string {
	return beginMarker + "\n" + content + "\n" + endMarker + "\n"
}

func hasMarkerBlock(content, beginMarker string) bool {
	return strings.Contains(content, beginMarker)
}

// stripMarkerBlocks removes every marker block and reports whether it removed
// any. A begin marker with no matching end marker is left alone.
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
