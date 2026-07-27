package tools

import (
	"context"
	"testing"
)

func TestPressButtonWhenNotRunning(t *testing.T) {
	s := &Server{}
	result, _, err := s.pressButton(context.Background(), nil, PressButtonInput{Button: "a", DurationMs: 100})
	if err != nil {
		t.Fatalf("pressButton: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("pressButton() result = %v, want an IsError result", result)
	}
}

func TestPressButtonWhenRunning(t *testing.T) {
	s := newTestServer(t)
	startFakeHarness(t, s.dataDir, map[string]any{"status": "ok"})

	result, _, err := s.pressButton(context.Background(), nil, PressButtonInput{Button: "a", DurationMs: 100})
	if err != nil {
		t.Fatalf("pressButton: %v", err)
	}
	if result != nil {
		t.Fatalf("pressButton() result = %v, want nil (success)", result)
	}
}

func TestSetCrankWhenNotRunning(t *testing.T) {
	s := &Server{}
	result, _, err := s.setCrank(context.Background(), nil, SetCrankInput{CrankAngle: 90})
	if err != nil {
		t.Fatalf("setCrank: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("setCrank() result = %v, want an IsError result", result)
	}
}

func TestSetCrankWhenRunning(t *testing.T) {
	s := newTestServer(t)
	startFakeHarness(t, s.dataDir, map[string]any{"status": "ok"})

	result, _, err := s.setCrank(context.Background(), nil, SetCrankInput{CrankAngle: 90, CrankDocked: true})
	if err != nil {
		t.Fatalf("setCrank: %v", err)
	}
	if result != nil {
		t.Fatalf("setCrank() result = %v, want nil (success)", result)
	}
}
