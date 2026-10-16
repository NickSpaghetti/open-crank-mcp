package httpserver

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

func TestCheckLoopbackRejects(t *testing.T) {
	for _, tc := range []struct {
		addr string
		// The message must tell the user what to type instead.
		wantInMessage string
	}{
		{":8237", "127.0.0.1:8237"},
		{"0.0.0.0:8237", "loopback"},
		{"192.168.1.10:8237", "loopback"},
		{"[::]:8237", "loopback"},
		{"example.com:8237", "loopback-only"},
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

// Serve must refuse a non-loopback address before binding. The deadline makes a
// broken guard fail the test instead of serving forever.
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

func TestServeReportsABindFailure(t *testing.T) {
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
	if !strings.Contains(err.Error(), held.Addr().String()) {
		t.Errorf("Serve said %q, want the address %q in it", err, held.Addr().String())
	}
}

// A real MCP client can handshake and list tools without a session id.
func TestServeAnswersAnMCPClient(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "open-crank-mcp"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "ping_test", Description: "a tool"},
		func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, struct{}, error) {
			return nil, struct{}{}, nil
		})

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

	reopened, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("port %s still held after shutdown: %v", addr, err)
	}
	_ = reopened.Close()
}
