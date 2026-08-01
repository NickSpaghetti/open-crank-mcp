package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/NickSpaghetti/open-crank-mcp/internal/harness"
)

func TestGetGameStateWhenNotRunning(t *testing.T) {
	s := &Server{}
	result, _, err := s.getGameState(context.Background(), nil, GetGameStateInput{})
	if err != nil {
		t.Fatalf("getGameState: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("getGameState() result = %v, want an IsError result", result)
	}
}

func TestGetGameStateWhenRunning(t *testing.T) {
	s := newTestServer(t)
	startFakeHarness(t, s.dataDir, map[string]any{
		"status": "ok",
		"state":  map[string]any{"score": float64(42)},
	})

	_, out, err := s.getGameState(context.Background(), nil, GetGameStateInput{})
	if err != nil {
		t.Fatalf("getGameState: %v", err)
	}
	// The game's own state is passed through as the raw JSON it produced, so
	// what comes back out is bytes rather than a decoded map. That is the point:
	// its shape is game-defined and this server never had a schema for it.
	raw, ok := out.(json.RawMessage)
	if !ok {
		t.Fatalf("getGameState() = %v (%T), want json.RawMessage", out, out)
	}
	var state map[string]any
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("getGameState() returned invalid JSON %s: %v", raw, err)
	}
	if state["score"] != float64(42) {
		t.Fatalf(`getGameState()["score"] = %v, want 42`, state["score"])
	}
}

func TestListEntitiesWhenNotRunning(t *testing.T) {
	s := &Server{}
	result, _, err := s.listEntities(context.Background(), nil, ListEntitiesInput{})
	if err != nil {
		t.Fatalf("listEntities: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("listEntities() result = %v, want an IsError result", result)
	}
}

func TestListEntitiesWhenRunning(t *testing.T) {
	s := newTestServer(t)
	startFakeHarness(t, s.dataDir, map[string]any{
		"status":            "ok",
		"entities_complete": true,
		"entities": []any{
			map[string]any{
				"tag": float64(2), "class_name": "Enemy",
				"x": float64(10), "y": float64(20),
				"width": float64(16), "height": float64(16),
				"z_index": float64(500), "visible": true,
			},
		},
	})

	_, out, err := s.listEntities(context.Background(), nil, ListEntitiesInput{})
	if err != nil {
		t.Fatalf("listEntities: %v", err)
	}
	if !out.Complete {
		t.Fatal("listEntities().Complete = false, want true")
	}
	if len(out.Entities) != 1 {
		t.Fatalf("listEntities() returned %d entities, want 1", len(out.Entities))
	}
	want := harness.Entity{Tag: 2, ClassName: "Enemy", X: 10, Y: 20, Width: 16, Height: 16, ZIndex: 500, Visible: true}
	if out.Entities[0] != want {
		t.Fatalf("listEntities().Entities[0] = %+v, want %+v", out.Entities[0], want)
	}
}

// A malformed entities array is now an error rather than a silently shortened
// list, and that is a deliberate change of behaviour. The old hand-rolled decode
// skipped anything that was not a map, so a harness whose entity shape drifted
// would quietly return fewer sprites than the game has - the same class of silent
// wrongness this whole change set exists to remove, and the one thing
// list_entities must never do, since a caller cannot tell a missing sprite from
// an absent one. Note that only a *type* change trips this: encoding/json ignores
// fields it does not know, so a harness adding a field stays compatible.
func TestListEntitiesRejectsMalformedEntries(t *testing.T) {
	s := newTestServer(t)
	startFakeHarness(t, s.dataDir, map[string]any{
		"status":            "ok",
		"entities_complete": false,
		"entities":          []any{"not an object", 42, nil},
	})

	_, _, err := s.listEntities(context.Background(), nil, ListEntitiesInput{})
	if err == nil {
		t.Fatal("listEntities() succeeded on a malformed entities array, want an error")
	}
	// Not a harness-reported failure and not a missing simulator: a response this
	// server could not decode is a genuine unexpected condition.
	if errors.Is(err, errHarness) || errors.Is(err, errNotRunning) {
		t.Fatalf("listEntities() error = %v, want a decode error", err)
	}
}
