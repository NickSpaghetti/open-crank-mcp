package setup

import "testing"

func TestMarkerBlockRoundTrip(t *testing.T) {
	block := markerBlock("-- BEGIN", "-- END", `import "mcp_harness"`)
	if !hasMarkerBlock(block, "-- BEGIN") {
		t.Fatalf("hasMarkerBlock() = false for a block that was just built, want true")
	}
}

func TestStripMarkerBlocksRemovesExactlyTheBlock(t *testing.T) {
	before := "local x = 1\n"
	content := before + markerBlock("-- BEGIN", "-- END", `import "mcp_harness"`)

	got, changed := stripMarkerBlocks(content, "-- BEGIN", "-- END")
	if !changed {
		t.Fatal("stripMarkerBlocks() changed = false, want true")
	}
	if got != before {
		t.Fatalf("stripMarkerBlocks() = %q, want %q", got, before)
	}
}

func TestStripMarkerBlocksNoOpWhenAbsent(t *testing.T) {
	content := "local x = 1\n"
	got, changed := stripMarkerBlocks(content, "-- BEGIN", "-- END")
	if changed {
		t.Fatal("stripMarkerBlocks() changed = true, want false (no marker present)")
	}
	if got != content {
		t.Fatalf("stripMarkerBlocks() = %q, want unchanged %q", got, content)
	}
}

func TestStripMarkerBlocksHandlesMultipleBlocks(t *testing.T) {
	content := markerBlock("-- BEGIN", "-- END", "one") +
		"local kept = true\n" +
		markerBlock("-- BEGIN", "-- END", "two")

	got, changed := stripMarkerBlocks(content, "-- BEGIN", "-- END")
	if !changed {
		t.Fatal("stripMarkerBlocks() changed = false, want true")
	}
	want := "local kept = true\n"
	if got != want {
		t.Fatalf("stripMarkerBlocks() = %q, want %q", got, want)
	}
}

func TestStripMarkerBlocksLeavesUnmatchedBeginAlone(t *testing.T) {
	content := "-- BEGIN\nno matching end here\n"
	got, changed := stripMarkerBlocks(content, "-- BEGIN", "-- END")
	if changed {
		t.Fatal("stripMarkerBlocks() changed = true, want false (no matching end marker - shouldn't guess)")
	}
	if got != content {
		t.Fatalf("stripMarkerBlocks() = %q, want unchanged %q", got, content)
	}
}
