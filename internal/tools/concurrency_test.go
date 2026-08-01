package tools

import (
	"sync"
	"testing"

	"github.com/NickSpaghetti/open-crank-mcp/internal/harness"
)

// TestRoundTripConcurrentCallsDoNotCrossTalk exercises the race the MCP
// go-sdk's default concurrent tool-call dispatch (jsonrpc2.Async in
// mcp/server.go, called for every request but "initialize") exposes: the
// harness protocol is a single fixed mcp/command.json/response.json pair,
// not per-request files, so two roundTrip calls in flight at once can read
// back the wrong response. roundTrip fills in cmd.ID, so each goroutine can
// compare what it sent against what it got back.
func TestRoundTripConcurrentCallsDoNotCrossTalk(t *testing.T) {
	s := newTestServer(t)
	startEchoingFakeHarness(t, s.dataDir)

	const n = 20
	type result struct {
		gotID string
		err   error
	}
	results := make([]result, n)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp, err := s.roundTrip(harness.Command{Type: harness.CmdState})
			results[i] = result{gotID: resp.ID, err: err}
		}(i)
	}
	wg.Wait()

	// Two things are asserted, and the id check in WaitForResponse is why the
	// first one is now worth stating this way rather than comparing each call's
	// sent id to its received one: a response carrying someone else's id is
	// discarded and waited past, so a swap (A reading B's answer while B reads
	// A's) can no longer be represented - it would surface as a timeout, i.e. as
	// r.err, not as a mismatch. What remains observable is that no two callers
	// came back with the same response, and that every caller got one at all.
	// See TestWaitForResponseSkipsMismatchedID for the correlation itself.
	seen := make(map[string]bool, n)
	for _, r := range results {
		if r.err != nil {
			t.Fatalf("roundTrip: %v", r.err)
		}
		if r.gotID == "" {
			t.Fatalf("response carried no id, so nothing correlated the call to it")
		}
		if seen[r.gotID] {
			t.Fatalf("cross-talk: two concurrent calls both received the response for id %q", r.gotID)
		}
		seen[r.gotID] = true
	}
}
