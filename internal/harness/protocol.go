package harness

import "encoding/json"

// StatusOK is the value both harnesses put in Response.Status on success.
const StatusOK = "ok"

// Screenshot formats, from Response.Format. Lua has no raw framebuffer
// accessor and C has no PNG encoder, so the harnesses differ.
const (
	FormatPNG  = "png"
	FormatRaw  = "raw"
	FormatNone = "none"
)

// Command is every field any command type can carry. No field is omitempty:
// an explicit zero (e.g. crank_angle 0) must be distinguishable on the wire.
type Command struct {
	ID   string `json:"id"`
	Type string `json:"type"`

	// Button is one of ButtonNames for press and release, "" otherwise.
	Button string `json:"button"`
	// DurationMs is how long the override lasts; non-positive means no expiry.
	DurationMs int `json:"duration_ms"`

	CrankAngle float64 `json:"crank_angle"`
	CrankDelta float64 `json:"crank_delta"`
	// CrankDock is one of the DockMode values; a bool cannot express "leave the
	// dock alone".
	CrankDock string `json:"crank_dock"`
}

// Dock modes for Command.CrankDock. Both harnesses treat an empty or
// unrecognised value as unchanged, so a zeroed Command is safe.
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

// Command type values. Both harnesses dispatch on these exact strings.
const (
	CmdPing       = "ping"
	CmdPress      = "press"
	CmdRelease    = "release"
	CmdCrank      = "crank"
	CmdState      = "state"
	CmdScreenshot = "screenshot"
	CmdEntities   = "entities"
	// CmdReset drops every override (buttons and crank). It is the only way to
	// release a crank override with no expiry.
	CmdReset = "reset"
)

// ButtonNames are the only button names either harness recognises. Both
// answer an unknown name with status "ok", so it is validated here.
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

// Entity is one sprite from an entities response. ClassName is "Sprite" for a
// plain Lua sprite (the SDK base class) and always "" for C.
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

	// State is the game inspector's output, kept raw because its shape is
	// game-defined.
	State json.RawMessage `json:"state"`

	Entities []Entity `json:"entities"`
	// EntitiesComplete is true for Lua (getAllSprites is a real enumeration) and
	// false for C (querySpritesInRect only matches sprites with a collide rect).
	EntitiesComplete bool `json:"entities_complete"`

	// HarnessVersion is the content fingerprint of the answering harness
	// (see version.go). Empty means a harness older than the marker.
	HarnessVersion string `json:"harness_version"`
}

// Failed reports whether the harness said this command did not succeed. An
// empty Status is treated as OK, not as an unreported error.
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
