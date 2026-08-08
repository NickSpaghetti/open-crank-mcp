package harness

import "encoding/json"

// The wire protocol between the Go server and either harness, as one flat
// struct per direction.
//
// This is the third leg of the fat-struct decision in docs/ROADMAP.md: the C
// harness has had McpCommand/McpResponse since Checkpoint 2 and the Lua harness
// has had emptyResponse(), but the Go side talked in map[string]any until now,
// so "all three agree on one schema" was two thirds true. Everything a command
// or response can carry is a field here, a ping leaves most of them zero, and
// that is the accepted trade - same reasoning, same shape, in the third
// language.
//
// It is not only about tidiness. Three defects were live in the gap and all
// three were confirmed against a real game: status and error were produced by
// both harnesses and read by nothing, the id was written and never compared, and
// an unknown button name was accepted at every layer. With the fields named in a
// struct, forgetting one is a compile error rather than a review catch.
//
// The field names and their JSON tags are not from the docs - they were captured
// off the wire from both harnesses. Keep them in sync with
// lua/mcp_harness.lua's emptyResponse() and mcp_format_response() in
// c-harness/mcp_harness.c.

// StatusOK is the value both harnesses put in Response.Status on success.
// Anything else is a failure the harness is reporting deliberately.
const StatusOK = "ok"

// Screenshot formats, from Response.Format. The two harnesses genuinely cannot
// share one: Lua has no raw framebuffer accessor and C has no PNG encoder. See
// docs/ROADMAP.md.
const (
	FormatPNG  = "png"
	FormatRaw  = "raw"
	FormatNone = "none"
)

// Command is every field any command type can carry.
//
// No field is omitempty, deliberately: every command puts every field on the
// wire, and a ping simply sends them all zero. That is the fat-struct decision
// applied to the wire and not only to the three structs - McpCommand starts from
// a memset and Lua's command table is read field by field, so "present and zero"
// is the shape both harnesses are built around.
//
// Two separate defects came out of this, and they are worth keeping apart.
//
// The first was omitempty itself, and it was a latent trap rather than a bug:
// set_crank(crank_angle: 0) sent no crank_angle at all, making "explicitly zero"
// indistinguishable on the wire from "not specified". It still did the right thing,
// because both harnesses independently default a missing field to zero - C memsets
// in mcp_parse_command before reading anything, Lua does `cmd.crank_angle or 0` -
// so the correctness of a Go marshaling decision rested on an invariant implemented
// twice, in two other languages, and stated in neither this file nor either of them.
//
// The second was the dock state, which is a different problem that removing
// omitempty would not have fixed: it was a bool, and a bool cannot say "leave it
// alone", so *always* sending it forces a value on every call. It is now a
// three-valued CrankDock; see its values below.
//
// That second one was a real bug, not a latent one, which only measuring
// established: the
// Simulator reports the crank *docked* when nobody is turning it (asserted in
// internal/contracttest, so a future SDK changing that is a loud failure). The old
// shape therefore sent crank_docked=false on every set_crank call and the override
// forced the game to read the crank as undocked - silently, on a call that only
// asked to change the angle. A game gating on playdate.isCrankDocked() saw a state
// nobody requested.
//
// The harness-side defaults stay where they are, as tolerance for anything that
// writes mcp/command.json by hand (this project's own probes have), but nothing
// here relies on them any more.
type Command struct {
	ID   string `json:"id"`
	Type string `json:"type"`

	// Button is one of the six names in ButtonNames, for press and release, and
	// "" for every other command type - which both harnesses map to no button.
	Button string `json:"button"`
	// DurationMs is how long the override lasts. Non-positive means no expiry: hold
	// it until something replaces it. That rule is uniform across presses, releases
	// and crank commands in both harnesses; which tools actually send a non-positive
	// value is a separate decision that lives in internal/tools, and they differ.
	DurationMs int `json:"duration_ms"`

	CrankAngle float64 `json:"crank_angle"`
	CrankDelta float64 `json:"crank_delta"`
	// CrankDock is one of the DockMode values. A string rather than the obvious
	// pair of booleans (a value plus a "did they set it" flag) for two reasons:
	// that pair has four states for three meanings, so one combination is
	// nonsense every reader has to know to ignore; and the C harness finds keys
	// with strstr, where a `crank_docked` / `crank_docked_set` pair works only
	// because the pattern includes the closing quote. One self-describing field
	// removes both problems, and it is the same vocabulary the set_crank tool
	// takes, so the wire reads the way the caller wrote it.
	CrankDock string `json:"crank_dock"`
}

// Dock modes for Command.CrankDock.
//
// DockUnchanged is the zero-ish value on purpose: every field of a Command has to
// be safe when zero, since a ping sends the whole struct zeroed. Both harnesses
// treat an unrecognised or empty value as unchanged for that reason, while
// set_crank always writes one of these three explicitly so real traffic is
// readable rather than merely valid.
const (
	DockUnchanged = "unchanged"
	DockDocked    = "docked"
	DockUndocked  = "undocked"
)

// DockModes are the values set_crank accepts, in the order they are offered to a
// caller.
var DockModes = []string{DockUnchanged, DockDocked, DockUndocked}

// ValidDockMode reports whether mode is one a caller may send. The empty string
// is accepted as the zero value's synonym for DockUnchanged.
func ValidDockMode(mode string) bool {
	if mode == "" {
		return true
	}
	for _, m := range DockModes {
		if m == mode {
			return true
		}
	}
	return false
}

// There is deliberately no Go helper resolving a mode into "override the dock?"
// and "force it to what?". Go only ever sends the mode; the resolution belongs to
// whichever harness receives it, and both do it in four lines
// (dockOverrideFromMode in lua/mcp_harness.lua, mcp_parse_command in
// c-harness/mcp_harness.c). A Go copy would have no caller and would be a third
// place for the same three cases to drift.

// Command type values. Both harnesses dispatch on these exact strings.
const (
	CmdPing       = "ping"
	CmdPress      = "press"
	CmdRelease    = "release"
	CmdCrank      = "crank"
	CmdState      = "state"
	CmdScreenshot = "screenshot"
	CmdEntities   = "entities"
	// CmdReset drops every override at once, buttons and crank, handing input back
	// to the player. It carries no fields.
	//
	// It exists because a crank override has no release. CmdRelease answers a held
	// button, but set_crank always activates the crank override and a duration-less
	// one never expires, so before this there was no call in the protocol that could
	// return the real crank reading to a game.
	CmdReset = "reset"
)

// ButtonNames are the only button names either harness recognises.
//
// Worth stating plainly because nothing used to enforce it: an unrecognised name
// reaches the harness, maps to no button, and is answered with status "ok", so
// press_button("A") reported success and did nothing. Validated in the Go layer
// now, since it is the only one of the three that can return a useful message to
// whoever asked.
var ButtonNames = []string{"a", "b", "up", "down", "left", "right"}

// ValidButton reports whether name is one the harnesses know.
func ValidButton(name string) bool {
	for _, b := range ButtonNames {
		if b == name {
			return true
		}
	}
	return false
}

// Entity is one sprite from an entities response, flat across both harnesses.
//
// ClassName is "Sprite" (not empty) for a plain, non-subclassed Lua sprite -
// that is the SDK's own base class name, not a missing-value marker - and always
// "" for C, which has no class system at all.
type Entity struct {
	Tag       int     `json:"tag"`
	ClassName string  `json:"class_name"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Width     float64 `json:"width"`
	Height    float64 `json:"height"`
	ZIndex    int     `json:"z_index"`
	Visible   bool    `json:"visible"`
}

// Response is every field any response can carry.
type Response struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Error  string `json:"error"`

	Format   string `json:"format"`
	Path     string `json:"path"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	RowBytes int    `json:"row_bytes"`

	// State is whatever the game's registered inspector returned. Kept raw
	// because its shape is game-defined by design - decoding it here would mean
	// inventing a schema the game never agreed to.
	State json.RawMessage `json:"state"`

	Entities []Entity `json:"entities"`
	// EntitiesComplete is true for Lua (getAllSprites is a real enumeration) and
	// false for C (querySpritesInRect only matches sprites with a collide rect).
	EntitiesComplete bool `json:"entities_complete"`

	// HarnessVersion identifies which canonical harness the answering game is
	// carrying. Empty means a harness older than the marker itself. See
	// version.go for why this is a content fingerprint rather than a number.
	HarnessVersion string `json:"harness_version"`
}

// Failed reports whether the harness said this command did not succeed.
//
// Treats an empty Status as OK rather than as a failure: every current harness
// sets it, but a response that reached us and parsed, with no status at all, is
// better read as "said nothing" than as "reported an error it did not report".
func (r Response) Failed() bool {
	return r.Status != "" && r.Status != StatusOK
}

// ErrorMessage is what to tell the caller when Failed is true, never empty.
func (r Response) ErrorMessage() string {
	if r.Error != "" {
		return r.Error
	}
	return "harness reported status " + r.Status + " with no error message"
}
