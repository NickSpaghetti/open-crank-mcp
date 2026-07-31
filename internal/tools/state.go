package tools

import (
	"context"
	"encoding/json"

	"github.com/NickSpaghetti/open-crank-mcp/internal/harness"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetGameStateInput struct{}

func (s *Server) getGameState(_ context.Context, _ *mcp.CallToolRequest, _ GetGameStateInput) (*mcp.CallToolResult, any, error) {
	resp, err := s.roundTrip(harness.Command{Type: harness.CmdState})
	if err != nil {
		result, wrapErr := handleRoundTripErr(err)
		return result, nil, wrapErr
	}
	if len(resp.State) == 0 {
		return nil, nil, nil
	}
	// Passed through as the raw JSON the game produced, so the shape stays the
	// game's own. json.RawMessage marshals back out verbatim.
	return nil, json.RawMessage(resp.State), nil
}

type ListEntitiesInput struct{}

type ListEntitiesOutput struct {
	Entities []harness.Entity `json:"entities"`
	// Complete is true for Lua games (getAllSprites is a real, complete
	// enumeration) and false for C games (querySpritesInRect only matches
	// sprites with a collision rect set - decorative/visual sprites are
	// commonly missed).
	Complete bool `json:"complete"`
}

func (s *Server) listEntities(_ context.Context, _ *mcp.CallToolRequest, _ ListEntitiesInput) (*mcp.CallToolResult, ListEntitiesOutput, error) {
	resp, err := s.roundTrip(harness.Command{Type: harness.CmdEntities})
	if err != nil {
		result, wrapErr := handleRoundTripErr(err)
		return result, ListEntitiesOutput{}, wrapErr
	}

	// Never nil, so the output is an empty array rather than null: a caller
	// distinguishing "no sprites" from "field missing" should not have to.
	entities := resp.Entities
	if entities == nil {
		entities = []harness.Entity{}
	}
	return nil, ListEntitiesOutput{Entities: entities, Complete: resp.EntitiesComplete}, nil
}
