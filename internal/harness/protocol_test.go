package harness

import (
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"
)

func TestResponseFailed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status string
		want   bool
	}{
		{"ok is not a failure", StatusOK, false},
		{"error is a failure", "error", true},
		{"anything unrecognised is a failure", "weird", true},
		// A parsed response with no status reads as "said nothing", not failure.
		{"empty status is not a failure", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := (Response{Status: tc.status}).Failed(); got != tc.want {
				t.Fatalf("Response{Status:%q}.Failed() = %v, want %v", tc.status, got, tc.want)
			}
		})
	}
}

func TestResponseErrorMessage(t *testing.T) {
	withError := Response{Status: "error", Error: "getDisplayFrame returned NULL"}
	if got := withError.ErrorMessage(); got != "getDisplayFrame returned NULL" {
		t.Fatalf("ErrorMessage() = %q, want the harness's own message", got)
	}

	bare := Response{Status: "error"}
	got := bare.ErrorMessage()
	if got == "" {
		t.Fatal("ErrorMessage() = \"\", want a fallback message")
	}
	if !strings.Contains(got, "error") {
		t.Fatalf("ErrorMessage() = %q, want it to name the status it saw", got)
	}
}

func TestValidButton(t *testing.T) {
	for _, name := range ButtonNames {
		if !ValidButton(name) {
			t.Fatalf("ValidButton(%q) = false, want true for a name both harnesses know", name)
		}
	}
	for _, name := range []string{"", "A", "x", "start", "a ", "Up"} {
		if ValidButton(name) {
			t.Fatalf("ValidButton(%q) = true, want false", name)
		}
	}
}

// The wire tags are the contract with both harnesses; a rename is a protocol break.
func TestResponseJSONTagsMatchTheWire(t *testing.T) {
	onTheWire := `{"id":"7","status":"ok","error":"","format":"raw","path":"mcp/screenshot.raw",` +
		`"width":400,"height":240,"row_bytes":52,"state":{"score":1},` +
		`"entities":[{"tag":2,"class_name":"","x":1.5,"y":2.5,"width":6,"height":6,"z_index":150,"visible":true}],` +
		`"entities_complete":false,"harness_version":"abc123abc123"}`

	var resp Response
	if err := json.Unmarshal([]byte(onTheWire), &resp); err != nil {
		t.Fatalf("unmarshaling a real response: %v", err)
	}
	if resp.ID != "7" || resp.Status != StatusOK || resp.Format != FormatRaw {
		t.Fatalf("id/status/format decoded as %q/%q/%q", resp.ID, resp.Status, resp.Format)
	}
	if resp.Width != 400 || resp.Height != 240 || resp.RowBytes != 52 {
		t.Fatalf("geometry decoded as %dx%d row_bytes=%d", resp.Width, resp.Height, resp.RowBytes)
	}
	if string(resp.State) != `{"score":1}` {
		t.Fatalf("state = %s, want the game's own JSON passed through verbatim", resp.State)
	}
	if len(resp.Entities) != 1 {
		t.Fatalf("entities decoded as %d entries, want 1", len(resp.Entities))
	}
	if e := resp.Entities[0]; e.Tag != 2 || e.X != 1.5 || e.ZIndex != 150 || !e.Visible {
		t.Fatalf("entity decoded as %+v", e)
	}
	if resp.HarnessVersion != "abc123abc123" {
		t.Fatalf("harness_version = %q, want the fingerprint the harness reported", resp.HarnessVersion)
	}
}

// A harness older than the version marker sends no such field.
func TestResponseWithoutHarnessVersionDecodesToEmpty(t *testing.T) {
	var resp Response
	if err := json.Unmarshal([]byte(`{"id":"1","status":"ok"}`), &resp); err != nil {
		t.Fatalf("unmarshaling a pre-versioning response: %v", err)
	}
	if resp.HarnessVersion != "" {
		t.Fatalf("harness_version = %q, want empty", resp.HarnessVersion)
	}
}

func TestFingerprintChangesWithContent(t *testing.T) {
	base := fstest.MapFS{
		LuaSourcePath: {Data: []byte("-- one\n")},
		CHeaderPath:   {Data: []byte("/* h */\n")},
		CSourcePath:   {Data: []byte("/* c */\n")},
	}
	edited := fstest.MapFS{
		LuaSourcePath: {Data: []byte("-- two\n")},
		CHeaderPath:   {Data: []byte("/* h */\n")},
		CSourcePath:   {Data: []byte("/* c */\n")},
	}

	first := mustLuaFingerprint(t, base)
	if again := mustLuaFingerprint(t, base); first != again {
		t.Fatalf("fingerprint is not stable: %q then %q", first, again)
	}
	if changed := mustLuaFingerprint(t, edited); changed == first {
		t.Fatal("editing the Lua harness did not change its fingerprint")
	}
}

// The stamp lives in the header, but a change to the C source must move it too.
func TestCFingerprintCoversBothFiles(t *testing.T) {
	base := fstest.MapFS{
		CHeaderPath: {Data: []byte("/* h */\n")},
		CSourcePath: {Data: []byte("/* c */\n")},
	}
	editedSource := fstest.MapFS{
		CHeaderPath: {Data: []byte("/* h */\n")},
		CSourcePath: {Data: []byte("/* c changed */\n")},
	}

	before, err := CFingerprint(base)
	if err != nil {
		t.Fatalf("CFingerprint: %v", err)
	}
	after, err := CFingerprint(editedSource)
	if err != nil {
		t.Fatalf("CFingerprint: %v", err)
	}
	if before == after {
		t.Fatal("editing mcp_harness.c did not change the C fingerprint")
	}
}

func TestCFingerprintDistinguishesMovedContent(t *testing.T) {
	a, err := CFingerprint(fstest.MapFS{
		CHeaderPath: {Data: []byte("AB")},
		CSourcePath: {Data: []byte("C")},
	})
	if err != nil {
		t.Fatalf("CFingerprint: %v", err)
	}
	b, err := CFingerprint(fstest.MapFS{
		CHeaderPath: {Data: []byte("A")},
		CSourcePath: {Data: []byte("BC")},
	})
	if err != nil {
		t.Fatalf("CFingerprint: %v", err)
	}
	if a == b {
		t.Fatal("moving a byte between the header and the source did not change the fingerprint")
	}
}

func TestIsCurrentFingerprint(t *testing.T) {
	fsys := fstest.MapFS{
		LuaSourcePath: {Data: []byte("-- lua\n")},
		CHeaderPath:   {Data: []byte("/* h */\n")},
		CSourcePath:   {Data: []byte("/* c */\n")},
	}
	lua := mustLuaFingerprint(t, fsys)

	for _, reported := range []string{lua, mustCFingerprint(t, fsys)} {
		ok, err := IsCurrentFingerprint(fsys, reported)
		if err != nil {
			t.Fatalf("IsCurrentFingerprint(%q): %v", reported, err)
		}
		if !ok {
			t.Fatalf("IsCurrentFingerprint(%q) = false, want true", reported)
		}
	}
	for _, reported := range []string{"", VersionPlaceholder, "deadbeef1234"} {
		ok, err := IsCurrentFingerprint(fsys, reported)
		if err != nil {
			t.Fatalf("IsCurrentFingerprint(%q): %v", reported, err)
		}
		if ok {
			t.Fatalf("IsCurrentFingerprint(%q) = true, want false", reported)
		}
	}
}

func mustLuaFingerprint(t *testing.T, fsys fstest.MapFS) string {
	t.Helper()
	got, err := LuaFingerprint(fsys)
	if err != nil {
		t.Fatalf("LuaFingerprint: %v", err)
	}
	return got
}

func mustCFingerprint(t *testing.T, fsys fstest.MapFS) string {
	t.Helper()
	got, err := CFingerprint(fsys)
	if err != nil {
		t.Fatalf("CFingerprint: %v", err)
	}
	return got
}

func TestCommandJSONTagsMatchTheWire(t *testing.T) {
	b, err := json.Marshal(Command{
		ID: "3", Type: CmdPress, Button: "a", DurationMs: 200,
	})
	if err != nil {
		t.Fatalf("marshaling: %v", err)
	}
	got := string(b)
	for _, want := range []string{`"id":"3"`, `"type":"press"`, `"button":"a"`, `"duration_ms":200`} {
		if !strings.Contains(got, want) {
			t.Fatalf("marshaled command %s, want it to contain %s", got, want)
		}
	}
	// Every field is on the wire, including ones this command type does not use.
	for _, want := range []string{`"crank_angle":0`, `"crank_delta":0`, `"crank_dock":""`} {
		if !strings.Contains(got, want) {
			t.Fatalf("marshaled press command %s, want it to still carry %s", got, want)
		}
	}
}

// "Set the crank to 0 degrees, undocked" must be distinguishable from saying nothing.
func TestCommandKeepsMeaningfulZeros(t *testing.T) {
	b, err := json.Marshal(Command{
		ID: "1", Type: CmdCrank,
		CrankAngle: 0, CrankDelta: 0, CrankDock: DockUndocked, DurationMs: 0,
	})
	if err != nil {
		t.Fatalf("marshaling: %v", err)
	}
	got := string(b)
	for _, want := range []string{
		`"crank_angle":0`, `"crank_delta":0`, `"crank_dock":"undocked"`,
		`"duration_ms":0`, `"button":""`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("marshaled zero-valued crank command %s, want it to contain %s", got, want)
		}
	}
}

func TestPingCarriesEveryField(t *testing.T) {
	b, err := json.Marshal(Command{ID: "1", Type: CmdPing})
	if err != nil {
		t.Fatalf("marshaling: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshaling: %v", err)
	}
	for _, key := range []string{
		"id", "type", "button", "duration_ms", "crank_angle", "crank_delta", "crank_dock",
	} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("a ping is missing %q; every command carries every field", key)
		}
	}
	if len(decoded) != 7 {
		t.Errorf("a ping marshaled %d fields, want 7 - a new field needs adding to this list "+
			"and to both harnesses", len(decoded))
	}
}

// Empty is valid: omitting the field means "leave the dock alone".
func TestValidDockMode(t *testing.T) {
	for _, mode := range append([]string{""}, DockModes...) {
		if !ValidDockMode(mode) {
			t.Errorf("ValidDockMode(%q) = false, want true", mode)
		}
	}
	// "true"/"false" are what someone remembering the old bool would try.
	for _, mode := range []string{"true", "false", "dock", "DOCKED", "Undocked", "0"} {
		if ValidDockMode(mode) {
			t.Errorf("ValidDockMode(%q) = true, want false", mode)
		}
	}
}

func TestDockModesAreDistinct(t *testing.T) {
	seen := map[string]bool{}
	for _, mode := range DockModes {
		if mode == "" {
			t.Error("a dock mode is the empty string, which is reserved for the zero value")
		}
		if seen[mode] {
			t.Errorf("duplicate dock mode %q", mode)
		}
		seen[mode] = true
	}
	if len(DockModes) != 3 {
		t.Errorf("DockModes has %d entries, want 3 (unchanged, docked, undocked)", len(DockModes))
	}
}
