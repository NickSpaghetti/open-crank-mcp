package tools

import (
	"sync"
	"testing"
)

// TestRoundTripConcurrentCallsDoNotCrossTalk exercises the race the MCP
// go-sdk's default concurrent tool-call dispatch (jsonrpc2.Async in
// mcp/server.go, called for every request but "initialize") exposes: the
// harness protocol is a single fixed mcp/command.json/response.json pair,
// not per-request files, so two roundTrip calls in flight at once can read
// back the wrong response. roundTrip mutates cmd["id"] in place, so each
// goroutine can compare what it sent against what it got back.
func TestRoundTripConcurrentCallsDoNotCrossTalk(t *testing.T) {
	s := newTestServer(t)
	startEchoingFakeHarness(t, s.dataDir)

	const n = 20
	type result struct {
		sentID string
		gotID  string
		err    error
	}
	results := make([]result, n)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cmd := map[string]any{"type": "state"}
			resp, err := s.roundTrip(cmd)
			results[i] = result{sentID: asString(cmd["id"]), err: err}
			if resp != nil {
				results[i].gotID = asString(resp["id"])
			}
		}(i)
	}
	wg.Wait()

	seen := make(map[string]bool, n)
	for _, r := range results {
		if r.err != nil {
			t.Fatalf("roundTrip: %v", r.err)
		}
		if r.sentID == "" {
			t.Fatalf("roundTrip did not assign an id to the outgoing command")
		}
		if r.sentID != r.gotID {
			t.Fatalf("cross-talk: sent command id %q, got response for id %q", r.sentID, r.gotID)
		}
		if seen[r.sentID] {
			t.Fatalf("duplicate id assigned to two concurrent calls: %q", r.sentID)
		}
		seen[r.sentID] = true
	}
}
