package tools

import (
	"sync"
	"testing"

	"github.com/NickSpaghetti/open-crank-mcp/internal/harness"
)

// The go-sdk dispatches tool calls concurrently, but the harness IPC is one
// fixed command/response pair, so concurrent roundTrips must not cross-talk.
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

	// A swapped answer would be discarded by the id check and surface as a
	// timeout, so assert every caller got a distinct response.
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
