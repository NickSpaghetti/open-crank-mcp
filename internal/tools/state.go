package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetGameStateInput struct{}

func (s *Server) getGameState(_ context.Context, _ *mcp.CallToolRequest, _ GetGameStateInput) (*mcp.CallToolResult, any, error) {
	resp, err := s.roundTrip(map[string]any{"type": "state"})
	if err != nil {
		result, wrapErr := handleRoundTripErr(err)
		return result, nil, wrapErr
	}
	return nil, resp["state"], nil
}

type ListEntitiesInput struct{}

// Entity is one flat struct covering both harnesses' fields. ClassName is
// "Sprite" (not empty) for a plain, non-subclassed Lua sprite - that's the
// SDK's own base class name, not a missing-value marker - and always ""
// for C, which has no class system at all.
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

type ListEntitiesOutput struct {
	Entities []Entity `json:"entities"`
	// Complete is true for Lua games (getAllSprites is a real, complete
	// enumeration) and false for C games (querySpritesInRect only matches
	// sprites with a collision rect set - decorative/visual sprites are
	// commonly missed).
	Complete bool `json:"complete"`
}

func (s *Server) listEntities(_ context.Context, _ *mcp.CallToolRequest, _ ListEntitiesInput) (*mcp.CallToolResult, ListEntitiesOutput, error) {
	resp, err := s.roundTrip(map[string]any{"type": "entities"})
	if err != nil {
		result, wrapErr := handleRoundTripErr(err)
		return result, ListEntitiesOutput{}, wrapErr
	}

	rawEntities, _ := resp["entities"].([]any)
	entities := make([]Entity, 0, len(rawEntities))
	for _, re := range rawEntities {
		m, ok := re.(map[string]any)
		if !ok {
			continue
		}
		entities = append(entities, Entity{
			Tag:       int(asFloat(m["tag"])),
			ClassName: asString(m["class_name"]),
			X:         asFloat(m["x"]),
			Y:         asFloat(m["y"]),
			Width:     asFloat(m["width"]),
			Height:    asFloat(m["height"]),
			ZIndex:    int(asFloat(m["z_index"])),
			Visible:   asBool(m["visible"]),
		})
	}

	return nil, ListEntitiesOutput{Entities: entities, Complete: asBool(resp["entities_complete"])}, nil
}
