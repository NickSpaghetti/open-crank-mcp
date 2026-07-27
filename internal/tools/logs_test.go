package tools

import (
	"context"
	"reflect"
	"testing"

	"github.com/NickSpaghetti/open-crank-mcp/internal/simulator"
)

func TestGetLogsWhenNotRunning(t *testing.T) {
	s := &Server{}
	result, _, err := s.getLogs(context.Background(), nil, GetLogsInput{})
	if err != nil {
		t.Fatalf("getLogs: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("getLogs() result = %v, want an IsError result", result)
	}
}

func TestGetLogsReturnsAllLinesByDefault(t *testing.T) {
	sim, err := simulator.Launch("sh", "-c", "echo one; echo two; echo three")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if err := sim.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	s := &Server{sim: sim}

	_, out, err := s.getLogs(context.Background(), nil, GetLogsInput{})
	if err != nil {
		t.Fatalf("getLogs: %v", err)
	}
	want := []string{"one", "two", "three"}
	if !reflect.DeepEqual(out.Lines, want) {
		t.Fatalf("getLogs().Lines = %v, want %v", out.Lines, want)
	}
}

func TestGetLogsTailN(t *testing.T) {
	sim, err := simulator.Launch("sh", "-c", "echo one; echo two; echo three")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if err := sim.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	s := &Server{sim: sim}

	_, out, err := s.getLogs(context.Background(), nil, GetLogsInput{TailN: 2})
	if err != nil {
		t.Fatalf("getLogs: %v", err)
	}
	want := []string{"two", "three"}
	if !reflect.DeepEqual(out.Lines, want) {
		t.Fatalf("getLogs(tail_n=2).Lines = %v, want %v", out.Lines, want)
	}
}

func TestGetLogsTailNLargerThanAvailableReturnsEverything(t *testing.T) {
	sim, err := simulator.Launch("sh", "-c", "echo one")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if err := sim.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	s := &Server{sim: sim}

	_, out, err := s.getLogs(context.Background(), nil, GetLogsInput{TailN: 100})
	if err != nil {
		t.Fatalf("getLogs: %v", err)
	}
	want := []string{"one"}
	if !reflect.DeepEqual(out.Lines, want) {
		t.Fatalf("getLogs(tail_n=100).Lines = %v, want %v", out.Lines, want)
	}
}

func TestGetLogsWithNoOutputReturnsEmptyLines(t *testing.T) {
	sim, err := simulator.Launch("sh", "-c", "true")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if err := sim.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	s := &Server{sim: sim}

	_, out, err := s.getLogs(context.Background(), nil, GetLogsInput{})
	if err != nil {
		t.Fatalf("getLogs: %v", err)
	}
	if len(out.Lines) != 0 {
		t.Fatalf("getLogs().Lines = %v, want empty", out.Lines)
	}
}
