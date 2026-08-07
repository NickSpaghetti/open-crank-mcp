package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/NickSpaghetti/open-crank-mcp/internal/harness"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type PressButtonInput struct {
	Button string `json:"button" jsonschema:"one of a, b, up, down, left, right"`
	// DurationMs is how long to hold the button. Omit it for a tap; see
	// defaultPressMs.
	DurationMs int `json:"duration_ms,omitempty" jsonschema:"how long to hold the button, in ms. Omit for a short tap."`
}

// defaultPressMs is how long an omitted duration holds a button for.
//
// It cannot be zero. The harness computes edges once per frame and deliberately
// defers a fresh command's edge to the *next* frame, so an override that expires
// before that frame arrives produces no press at all - press_button would report
// success and the game would never see it. Games run at around 30fps, so a frame
// is about 33ms; 100ms is three of them, enough that jitter in the round trip
// cannot swallow the press.
//
// Buttons get a default rather than the crank's hold-forever treatment because
// nothing exposes a release: a button held indefinitely could never be let go.
const defaultPressMs = 100

type PressButtonOutput struct{}

// pressButton validates the button name here rather than trusting the harness to
// complain, because neither harness does: an unrecognised name maps to no button
// and is still answered with status "ok", so press_button("A") reported success
// and did nothing. Confirmed against a real game for "x", "", "A" and "start".
// This is the only one of the three layers that can hand a useful message back to
// whoever asked.
func (s *Server) pressButton(_ context.Context, _ *mcp.CallToolRequest, in PressButtonInput) (*mcp.CallToolResult, PressButtonOutput, error) {
	if !harness.ValidButton(in.Button) {
		return errorResult(fmt.Sprintf("unknown button %q, want one of %s",
			in.Button, strings.Join(harness.ButtonNames, ", "))), PressButtonOutput{}, nil
	}

	duration := in.DurationMs
	if duration <= 0 {
		duration = defaultPressMs
	}

	_, err := s.roundTrip(harness.Command{
		Type:       harness.CmdPress,
		Button:     in.Button,
		DurationMs: duration,
	})
	if err != nil {
		result, wrapErr := handleRoundTripErr(err)
		return result, PressButtonOutput{}, wrapErr
	}
	return nil, PressButtonOutput{}, nil
}

// SetCrankInput keeps omitempty where harness.Command deliberately dropped it,
// because these are two different contracts and the tag does two different jobs.
//
// On harness.Command, omitempty decided what reached the wire, and dropping a
// zero there made "explicitly 0" indistinguishable from "unset" for two harnesses
// that had to guess. Here it only decides whether jsonschema-go marks the
// property `required` in the tool schema, and optional is right: an agent asking
// to turn the crank to 90 degrees should not also have to name a delta, a dock
// state and a duration.
//
// CrankDock is a string, not a bool, and not a *bool. As a bool, omitting it and
// passing false were the same request, so there was no way to move the angle and
// leave the dock alone. As a *bool it would infer a `["null","boolean"]` union
// into the tool schema - checked, not assumed - and a nullable union in a tool
// schema is the shape that already broke a real client here once (see
// readSaveDataOutputSchema). A named value says what it means in one type.
type SetCrankInput struct {
	CrankAngle float64 `json:"crank_angle,omitempty"`
	CrankDelta float64 `json:"crank_delta,omitempty"`
	CrankDock  string  `json:"crank_dock,omitempty" jsonschema:"one of unchanged, docked, undocked; omit to leave the dock state as the game sees it"`
	// DurationMs is how long to hold the crank there. Omit it to leave the crank
	// where it is put, which is what a real crank does; see
	// mcp_override_apply_crank. Not defaulted here, unlike press_button's: the
	// harnesses read a non-positive duration as "no expiry", so the sentinel has
	// to reach them intact.
	DurationMs int `json:"duration_ms,omitempty" jsonschema:"how long to hold the crank, in ms. Omit to leave it there."`
}

type SetCrankOutput struct{}

// setCrank validates the dock mode here for the same reason pressButton validates
// its button name: this is the only layer that can tell the caller what it should
// have said. An unrecognised value would otherwise reach a harness, resolve to
// "unchanged", and be answered with success.
func (s *Server) setCrank(_ context.Context, _ *mcp.CallToolRequest, in SetCrankInput) (*mcp.CallToolResult, SetCrankOutput, error) {
	if !harness.ValidDockMode(in.CrankDock) {
		return errorResult(fmt.Sprintf("unknown crank_dock %q, want one of %s (or omit it to leave the dock alone)",
			in.CrankDock, strings.Join(harness.DockModes, ", "))), SetCrankOutput{}, nil
	}
	// Normalised so the command on disk always names its mode, rather than
	// carrying an empty string that a reader has to know means "unchanged".
	dock := in.CrankDock
	if dock == "" {
		dock = harness.DockUnchanged
	}

	_, err := s.roundTrip(harness.Command{
		Type:       harness.CmdCrank,
		CrankAngle: in.CrankAngle,
		CrankDelta: in.CrankDelta,
		CrankDock:  dock,
		DurationMs: in.DurationMs,
	})
	if err != nil {
		result, wrapErr := handleRoundTripErr(err)
		return result, SetCrankOutput{}, wrapErr
	}
	return nil, SetCrankOutput{}, nil
}
