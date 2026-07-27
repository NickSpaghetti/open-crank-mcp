package tools

import (
	"context"
	"testing"
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
	state, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("getGameState() = %v (%T), want a map", out, out)
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
	want := Entity{Tag: 2, ClassName: "Enemy", X: 10, Y: 20, Width: 16, Height: 16, ZIndex: 500, Visible: true}
	if out.Entities[0] != want {
		t.Fatalf("listEntities().Entities[0] = %+v, want %+v", out.Entities[0], want)
	}
}

func TestListEntitiesSkipsMalformedEntries(t *testing.T) {
	s := newTestServer(t)
	startFakeHarness(t, s.dataDir, map[string]any{
		"status":            "ok",
		"entities_complete": false,
		"entities":          []any{"not an object", 42, nil},
	})

	_, out, err := s.listEntities(context.Background(), nil, ListEntitiesInput{})
	if err != nil {
		t.Fatalf("listEntities: %v", err)
	}
	if len(out.Entities) != 0 {
		t.Fatalf("listEntities() returned %d entities for malformed input, want 0", len(out.Entities))
	}
}
