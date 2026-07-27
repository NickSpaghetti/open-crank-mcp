package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type PressButtonInput struct {
	Button     string `json:"button" jsonschema:"one of a, b, up, down, left, right"`
	DurationMs int    `json:"duration_ms"`
}

type PressButtonOutput struct{}

func (s *Server) pressButton(_ context.Context, _ *mcp.CallToolRequest, in PressButtonInput) (*mcp.CallToolResult, PressButtonOutput, error) {
	_, err := s.roundTrip(map[string]any{
		"type":        "press",
		"button":      in.Button,
		"duration_ms": in.DurationMs,
	})
	if err != nil {
		result, wrapErr := handleRoundTripErr(err)
		return result, PressButtonOutput{}, wrapErr
	}
	return nil, PressButtonOutput{}, nil
}

type SetCrankInput struct {
	CrankAngle  float64 `json:"crank_angle,omitempty"`
	CrankDelta  float64 `json:"crank_delta,omitempty"`
	CrankDocked bool    `json:"crank_docked,omitempty"`
	DurationMs  int     `json:"duration_ms,omitempty"`
}

type SetCrankOutput struct{}

func (s *Server) setCrank(_ context.Context, _ *mcp.CallToolRequest, in SetCrankInput) (*mcp.CallToolResult, SetCrankOutput, error) {
	_, err := s.roundTrip(map[string]any{
		"type":         "crank",
		"crank_angle":  in.CrankAngle,
		"crank_delta":  in.CrankDelta,
		"crank_docked": in.CrankDocked,
		"duration_ms":  in.DurationMs,
	})
	if err != nil {
		result, wrapErr := handleRoundTripErr(err)
		return result, SetCrankOutput{}, wrapErr
	}
	return nil, SetCrankOutput{}, nil
}
