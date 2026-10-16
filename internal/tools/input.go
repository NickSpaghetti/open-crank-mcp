package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/NickSpaghetti/open-crank-mcp/internal/harness"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Both harnesses read a non-positive duration_ms as no expiry. Which tools
// send one is policy and lives here: press and release default to a tap,
// hold sends 0, so omitting a field never leaves a button stuck down.

type PressButtonInput struct {
	Button     string `json:"button" jsonschema:"one of a, b, up, down, left, right"`
	DurationMs int    `json:"duration_ms,omitempty" jsonschema:"how long to hold the button, in ms. Omit for a short tap."`
}

// defaultPressMs must span more than a frame (~33ms at 30fps): the harnesses
// defer a new command's edge to the next frame, so a shorter override is never
// seen by the game.
const defaultPressMs = 100

type PressButtonOutput struct{}

// buttonCommand validates a button name and sends cmdType for it. Returns a
// model-visible result, or nil when the round trip succeeded.
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

// HoldButtonInput has no duration: a hold with a duration is a press.
type HoldButtonInput struct {
	Button string `json:"button" jsonschema:"one of a, b, up, down, left, right"`
}

type HoldButtonOutput struct{}

// holdButton sends duration 0, which both harnesses read as no expiry.
func (s *Server) holdButton(_ context.Context, _ *mcp.CallToolRequest, in HoldButtonInput) (*mcp.CallToolResult, HoldButtonOutput, error) {
	result, err := s.buttonCommand(harness.CmdPress, in.Button, 0)
	return result, HoldButtonOutput{}, err
}

type ReleaseButtonInput struct {
	Button     string `json:"button" jsonschema:"one of a, b, up, down, left, right"`
	DurationMs int    `json:"duration_ms,omitempty" jsonschema:"how long to force the button up, in ms. Omit for the default."`
}

type ReleaseButtonOutput struct{}

// releaseButton defaults the duration rather than sending no-expiry: a release
// forces the button up, so forcing it forever would leave it deaf to the player.
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

// resetInput drops every override, buttons and crank, in one round trip.
func (s *Server) resetInput(_ context.Context, _ *mcp.CallToolRequest, _ ResetInputInput) (*mcp.CallToolResult, ResetInputOutput, error) {
	_, err := s.roundTrip(harness.Command{Type: harness.CmdReset})
	if err != nil {
		result, wrapErr := handleRoundTripErr(err)
		return result, ResetInputOutput{}, wrapErr
	}
	return nil, ResetInputOutput{}, nil
}

// SetCrankInput's omitempty only makes the properties optional in the tool
// schema. CrankDock is a string because a *bool infers a nullable union, the
// schema shape that broke a real client (see readSaveDataOutputSchema).
type SetCrankInput struct {
	CrankAngle float64 `json:"crank_angle,omitempty"`
	CrankDelta float64 `json:"crank_delta,omitempty"`
	CrankDock  string  `json:"crank_dock,omitempty" jsonschema:"one of unchanged, docked, undocked; omit to leave the dock state as the game sees it"`
	// DurationMs is not defaulted: a non-positive value must reach the harness
	// to mean "no expiry".
	DurationMs int `json:"duration_ms,omitempty" jsonschema:"how long to hold the crank, in ms. Omit to leave it there."`
}

type SetCrankOutput struct{}

func (s *Server) setCrank(_ context.Context, _ *mcp.CallToolRequest, in SetCrankInput) (*mcp.CallToolResult, SetCrankOutput, error) {
	if !harness.ValidDockMode(in.CrankDock) {
		return errorResult(fmt.Sprintf("unknown crank_dock %q, want one of %s (or omit it to leave the dock alone)",
			in.CrankDock, strings.Join(harness.DockModes, ", "))), SetCrankOutput{}, nil
	}
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
