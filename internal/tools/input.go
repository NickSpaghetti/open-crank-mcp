package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/NickSpaghetti/open-crank-mcp/internal/harness"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The four input tools and how they divide up one wire command each.
//
// Both harnesses implement a single rule - a non-positive duration_ms means no
// expiry - for presses, releases and crank commands alike. Which tools actually send
// a non-positive value is policy, and it lives here, because this is the layer that
// knows what an agent meant. The split matters enough to state: the harnesses are the
// mechanism, this file is the policy, and neither should be read as describing the
// other.
//
//	press_button    a tap; substitutes defaultPressMs when the caller omits one
//	hold_button     held until released; sends the sentinel, and takes no duration
//	release_button  lets go; substitutes defaultPressMs, then expires to passthrough
//	reset_input     drops every override at once, buttons and crank
//
// press_button keeps its tap default rather than becoming a hold. Omitting the
// duration is the common case for an agent, and a tap is the safe reading of it - the
// alternative leaves a button stuck down because a caller left a field out. Holding
// is the deliberate act, so it gets its own verb.

type PressButtonInput struct {
	Button string `json:"button" jsonschema:"one of a, b, up, down, left, right"`
	// DurationMs is how long to hold the button. Omit it for a tap; see
	// defaultPressMs.
	DurationMs int `json:"duration_ms,omitempty" jsonschema:"how long to hold the button, in ms. Omit for a short tap."`
}

// defaultPressMs is how long an omitted duration holds a button for, in press_button
// and release_button alike.
//
// It cannot be zero, and that is not the same statement it was: a zero now means "no
// expiry" rather than "expire immediately", so sending one would hold the button
// forever instead of doing nothing. Either way it is not a tap.
//
// The lower bound is a frame. The harnesses compute edges once per frame and
// deliberately defer a fresh command's edge to the *next* frame, so an override that
// expires before that frame arrives produces no press at all - the tool would report
// success and the game would never see it. Games run at around 30fps, so a frame is
// about 33ms; 100ms is three of them, enough that jitter in the round trip cannot
// swallow the press.
const defaultPressMs = 100

type PressButtonOutput struct{}

// buttonCommand validates a button name and sends cmdType for it. Returns a
// model-visible result, or nil when the round trip succeeded.
//
// The name is validated here rather than by trusting the harness to complain, because
// neither harness does: an unrecognised name maps to no button and is still answered
// with status "ok", so press_button("A") reported success and did nothing. Confirmed
// against a real game for "x", "", "A" and "start". This is the only one of the three
// layers that can hand a useful message back to whoever asked - which is also why the
// three button tools share this rather than each writing their own message.
func (s *Server) buttonCommand(cmdType, button string, durationMs int) (*mcp.CallToolResult, error) {
	if !harness.ValidButton(button) {
		return errorResult(fmt.Sprintf("unknown button %q, want one of %s",
			button, strings.Join(harness.ButtonNames, ", "))), nil
	}

	_, err := s.roundTrip(harness.Command{
		Type:       cmdType,
		Button:     button,
		DurationMs: durationMs,
	})
	if err != nil {
		return handleRoundTripErr(err)
	}
	return nil, nil
}

func (s *Server) pressButton(_ context.Context, _ *mcp.CallToolRequest, in PressButtonInput) (*mcp.CallToolResult, PressButtonOutput, error) {
	duration := in.DurationMs
	if duration <= 0 {
		duration = defaultPressMs
	}
	result, err := s.buttonCommand(harness.CmdPress, in.Button, duration)
	return result, PressButtonOutput{}, err
}

// HoldButtonInput has no duration field at all, rather than an ignored one. The whole
// tool is "hold this until I say otherwise", and a duration on it would mean
// press_button - offering one would be inviting a caller to write a hold that is
// secretly a tap.
type HoldButtonInput struct {
	Button string `json:"button" jsonschema:"one of a, b, up, down, left, right"`
}

type HoldButtonOutput struct{}

// holdButton sends a press with duration 0, which both harnesses read as no expiry.
// The zero has to reach them intact, so nothing here substitutes a default - the same
// reason set_crank does not, and the reason harness.Command has no omitempty.
func (s *Server) holdButton(_ context.Context, _ *mcp.CallToolRequest, in HoldButtonInput) (*mcp.CallToolResult, HoldButtonOutput, error) {
	result, err := s.buttonCommand(harness.CmdPress, in.Button, 0)
	return result, HoldButtonOutput{}, err
}

type ReleaseButtonInput struct {
	Button string `json:"button" jsonschema:"one of a, b, up, down, left, right"`
	// DurationMs is how long to force the button up before handing it back to the
	// player. Omit it; the default is right in almost every case.
	DurationMs int `json:"duration_ms,omitempty" jsonschema:"how long to force the button up, in ms. Omit for the default."`
}

type ReleaseButtonOutput struct{}

// releaseButton substitutes a real duration when the caller omits one, rather than
// sending the no-expiry sentinel like holdButton does. The harnesses would accept the
// sentinel - a release forces not-pressed rather than merely clearing the override, so
// that a human driving real input at the same time cannot leak a press through - and
// forcing it for ever would leave the button permanently deaf to the player, with no
// way back except another press. Forcing it up for three frames and then expiring to
// passthrough is what "let go" means.
//
// reset_input is the tool for handing everything back at once, and the only one that
// can do it for a held crank.
func (s *Server) releaseButton(_ context.Context, _ *mcp.CallToolRequest, in ReleaseButtonInput) (*mcp.CallToolResult, ReleaseButtonOutput, error) {
	duration := in.DurationMs
	if duration <= 0 {
		duration = defaultPressMs
	}
	result, err := s.buttonCommand(harness.CmdRelease, in.Button, duration)
	return result, ReleaseButtonOutput{}, err
}

type ResetInputInput struct{}

type ResetInputOutput struct{}

// resetInput drops every override in one round trip.
//
// It exists for the crank, which has no release: set_crank always activates the
// override and a duration-less one never expires, so before this there was no call
// that could give a game back its real crank reading. Buttons come along because the
// same command clears them and "hand input back to the player" is one action, not
// seven.
func (s *Server) resetInput(_ context.Context, _ *mcp.CallToolRequest, _ ResetInputInput) (*mcp.CallToolResult, ResetInputOutput, error) {
	_, err := s.roundTrip(harness.Command{Type: harness.CmdReset})
	if err != nil {
		result, wrapErr := handleRoundTripErr(err)
		return result, ResetInputOutput{}, wrapErr
	}
	return nil, ResetInputOutput{}, nil
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
