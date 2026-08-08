package httpserve

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCheckLoopbackAccepts(t *testing.T) {
	for _, addr := range []string{
		"127.0.0.1:8237",
		"127.0.0.53:1",
		"localhost:8237",
		"[::1]:8237",
	} {
		if err := CheckLoopback(addr); err != nil {
			t.Errorf("CheckLoopback(%q) = %v, want nil", addr, err)
		}
	}
}

// The rejections, each of which is a way to accidentally expose a server that builds
// code and launches processes with no authentication in front of it.
func TestCheckLoopbackRejects(t *testing.T) {
	for _, tc := range []struct {
		addr string
		// A fragment the message must contain, because the message is the whole
		// value of the check: someone who typed ":8237" needs to be told what to
		// type instead, not just told no.
		wantInMessage string
	}{
		// The one most likely to be typed, and the one that means every interface.
		{":8237", "127.0.0.1:8237"},
		{"0.0.0.0:8237", "loopback"},
		{"192.168.1.10:8237", "loopback"},
		{"[::]:8237", "loopback"},
		{"example.com:8237", "loopback-only"},
		// Not an address at all.
		{"8237", "host:port"},
		{"", "host:port"},
		{"127.0.0.1", "host:port"},
	} {
		err := CheckLoopback(tc.addr)
		if err == nil {
			t.Errorf("CheckLoopback(%q) = nil, want an error", tc.addr)
			continue
		}
		if !strings.Contains(err.Error(), tc.wantInMessage) {
			t.Errorf("CheckLoopback(%q) said %q, want %q in it", tc.addr, err, tc.wantInMessage)
		}
	}
}

// Serve must refuse a non-loopback address before it binds anything, not after. A
// listener that opens and then errors has already been reachable.
//
// The context is bounded rather than context.Background(), which matters for a reason
// that is not obvious: if the loopback guard is ever removed or inverted, Serve proceeds
// to bind 0.0.0.0 and serve forever, and this test hangs instead of failing. Mutation
// testing found exactly that - the mutant timed out rather than being killed, and a
// timed-out mutant teaches nothing. With a deadline the wrong behaviour returns and the
// assertion below fails, which is the outcome to want.
func TestServeRefusesNonLoopbackWithoutBinding(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := Serve(ctx, mcp.NewServer(&mcp.Implementation{Name: "test"}, nil), "0.0.0.0:0")
	if err == nil {
		t.Fatal("Serve on 0.0.0.0 succeeded, want a refusal")
	}
	if !strings.Contains(err.Error(), "loopback") {
		t.Errorf("Serve said %q, want the loopback reason", err)
	}
}

// A port that cannot be bound has to come back as an error naming the address, not as a
// server that silently is not listening. `make mcp-auto-test` starts this in the
// background and then connects, so a swallowed bind failure would show up as a contract
// run against whatever else holds the port.
func TestServeReportsABindFailure(t *testing.T) {
	// Hold a port, then ask Serve for the same one. Port 0 first so this cannot collide
	// with anything else on the machine.
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = held.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = Serve(ctx, mcp.NewServer(&mcp.Implementation{Name: "test"}, nil), held.Addr().String())
	if err == nil {
		t.Fatal("Serve on an already-held port succeeded, want a bind error")
	}
	// The address, because "listen tcp: bind: address already in use" with no port in it
	// is the least useful version of this message.
	if !strings.Contains(err.Error(), held.Addr().String()) {
		t.Errorf("Serve said %q, want the address %q in it", err, held.Addr().String())
	}
}

// The transport actually works: a real MCP client completes a handshake and lists tools
// over HTTP. Worth asserting rather than assuming, because the Stateless/JSONResponse
// pair is a choice about what a client has to do, and a client that has to send a
// session id it does not have would fail here and nowhere else.
func TestServeAnswersAnMCPClient(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "open-crank-mcp"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "ping_test", Description: "a tool"},
		func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, struct{}, error) {
			return nil, struct{}{}, nil
		})

	// Port 0 so this cannot collide with anything else on the machine, including
	// another copy of this test.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	endpoint := "http://" + listener.Addr().String()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	served := make(chan error, 1)
	go func() { served <- ServeListener(ctx, server, listener) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint}, nil)
	if err != nil {
		t.Fatalf("connecting over streamable HTTP: %v", err)
	}
	res, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list over streamable HTTP: %v", err)
	}
	if len(res.Tools) != 1 || res.Tools[0].Name != "ping_test" {
		t.Errorf("tools/list returned %d tools, want the one registered", len(res.Tools))
	}
	_ = session.Close()

	cancel()
	if err := <-served; err != nil {
		t.Errorf("ServeListener returned %v, want nil after cancellation", err)
	}
}

// Cancelling the context has to stop the server, or `make mcp-auto-test` leaves a
// listener behind on every run.
func TestServeStopsOnContextCancel(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() {
		served <- ServeListener(ctx, mcp.NewServer(&mcp.Implementation{Name: "test"}, nil), listener)
	}()

	cancel()
	select {
	case err := <-served:
		if err != nil {
			t.Errorf("ServeListener returned %v, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ServeListener did not return after its context was cancelled")
	}

	// And the port is free again, which is the property that actually matters.
	reopened, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("port %s still held after shutdown: %v", addr, err)
	}
	_ = reopened.Close()
}
