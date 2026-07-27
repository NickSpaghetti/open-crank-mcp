package tools

import (
	"context"
	"testing"
)

func TestStopSimulatorWhenNotRunning(t *testing.T) {
	s := &Server{}
	result, _, err := s.stopSimulator(context.Background(), nil, StopSimulatorInput{})
	if err != nil {
		t.Fatalf("stopSimulator: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("stopSimulator() result = %v, want an IsError result", result)
	}
}

func TestStopSimulatorWhenRunning(t *testing.T) {
	s := newTestServer(t)
	result, _, err := s.stopSimulator(context.Background(), nil, StopSimulatorInput{})
	if err != nil {
		t.Fatalf("stopSimulator: %v", err)
	}
	if result != nil {
		t.Fatalf("stopSimulator() result = %v, want nil (success)", result)
	}
	if s.sim != nil {
		t.Fatal("stopSimulator: s.sim is still set after stopping")
	}
}

func TestRestartSimulatorWhenNotRunning(t *testing.T) {
	s := &Server{}
	result, _, err := s.restartSimulator(context.Background(), nil, RestartSimulatorInput{})
	if err != nil {
		t.Fatalf("restartSimulator: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("restartSimulator() result = %v, want an IsError result", result)
	}
}

func TestRestartSimulatorWhenRunning(t *testing.T) {
	s := newTestServer(t)
	oldSim := s.sim

	result, out, err := s.restartSimulator(context.Background(), nil, RestartSimulatorInput{})
	if err != nil {
		t.Fatalf("restartSimulator: %v", err)
	}
	if result != nil {
		t.Fatalf("restartSimulator() result = %v, want nil (success)", result)
	}
	if out.BundleID != "com.example.test" {
		t.Fatalf("restartSimulator() BundleID = %q, want %q", out.BundleID, "com.example.test")
	}
	if s.sim == nil || s.sim == oldSim {
		t.Fatal("restartSimulator: s.sim was not replaced with a new instance")
	}
}

func TestGetStatusWhenNotRunning(t *testing.T) {
	s := &Server{}
	_, out, err := s.getStatus(context.Background(), nil, GetStatusInput{})
	if err != nil {
		t.Fatalf("getStatus: %v", err)
	}
	if out.Running {
		t.Fatal("getStatus().Running = true, want false")
	}
	if out.HarnessReachable {
		t.Fatal("getStatus().HarnessReachable = true, want false")
	}
}

func TestGetStatusWhenRunning(t *testing.T) {
	s := newTestServer(t)
	_, out, err := s.getStatus(context.Background(), nil, GetStatusInput{})
	if err != nil {
		t.Fatalf("getStatus: %v", err)
	}
	if !out.Running {
		t.Fatal("getStatus().Running = false, want true")
	}
	if out.BundleID != "com.example.test" {
		t.Fatalf("getStatus().BundleID = %q, want %q", out.BundleID, "com.example.test")
	}
	if !out.HarnessReachable {
		t.Fatal("getStatus().HarnessReachable = false, want true (newTestServer creates dataDir/mcp)")
	}
}
