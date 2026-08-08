// Package httpserve exposes an MCP server over Streamable HTTP instead of stdio.
//
// stdio is what MCP clients use and stays the default. This exists because contract
// testing tools speak HTTP and not stdio - Specmatic's MCP auto-test names
// STREAMABLE_HTTP as the only value its --transport-kind accepts - so without it the
// tool surface cannot be driven by anything but a client. The same server registration
// backs both transports, which is what makes the HTTP endpoint a faithful stand-in
// rather than a second surface that can drift from the real one.
package httpserve

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// shutdownGrace is how long in-flight requests get to finish once the context is
// cancelled. Short: the only requests this serves are local tool calls, and a contract
// test run that has been interrupted is not worth waiting on.
const shutdownGrace = 2 * time.Second

// CheckLoopback rejects any address that is not loopback.
//
// This server builds code, launches processes and reads and writes files anywhere the
// caller names. On stdio that authority is bounded by whoever spawned the process; on a
// listening socket it is bounded by whoever can reach the port, which on 0.0.0.0 is the
// network. There is no authentication here and adding a listener is not a reason to
// invent one, so the listener refuses to be reachable instead.
//
// A missing host is rejected rather than defaulted, which is the case worth being
// explicit about: ":8237" is the most natural thing to type and means every interface.
func CheckLoopback(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%q is not a host:port address: %w", addr, err)
	}
	if port == "" {
		return fmt.Errorf("%q names no port", addr)
	}
	if host == "" {
		return fmt.Errorf("%q has no host, which means every interface - "+
			"write 127.0.0.1:%s to bind loopback only", addr, port)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("%q is not an IP address or localhost; this listener is "+
			"loopback-only, so a hostname it cannot verify is refused", host)
	}
	if !ip.IsLoopback() {
		return fmt.Errorf("%q is not a loopback address. This server builds code and "+
			"launches processes on request and has no authentication, so it will not "+
			"listen anywhere reachable from outside this machine", host)
	}
	return nil
}

// Handler is the MCP HTTP handler for server.
//
// Stateless because every consumer of this is a one-shot run against a fresh process:
// there is no session to resume, and a stateless handler answers a client that never
// sends Mcp-Session-Id, which is the shape a testing tool is most likely to have.
// JSONResponse for the same reason - a plain JSON body is what a non-streaming client
// reads, rather than an event stream it has to parse.
//
// DNS-rebinding protection is left at the SDK's default (on), which rejects a request
// arriving on loopback with a non-loopback Host header. Combined with CheckLoopback
// that is both halves of the same guarantee: nothing off this machine can reach it, and
// nothing can trick a browser on this machine into reaching it either.
func Handler(server *mcp.Server) http.Handler {
	return mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true},
	)
}

// Serve listens on addr and serves server until ctx is cancelled.
//
// The listener is created before returning, so a caller that starts this in a goroutine
// and then connects cannot race the bind - which is exactly what `make mcp-auto-test`
// does. Errors from the bind are returned rather than logged for the same reason: a
// port already in use has to fail the run, not produce a test that connects to whatever
// else is on that port.
func Serve(ctx context.Context, server *mcp.Server, addr string) error {
	if err := CheckLoopback(addr); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", addr, err)
	}
	return ServeListener(ctx, server, listener)
}

// ServeListener serves server on an already-bound listener until ctx is cancelled. Split
// out so a test can bind port 0 and learn the port it got, which is the only way to run
// concurrently with anything else on the machine.
func ServeListener(ctx context.Context, server *mcp.Server, listener net.Listener) error {
	httpServer := &http.Server{
		Handler: Handler(server),
		// Not zero, because a client that connects and never sends anything would
		// otherwise hold the connection forever. Generous, because a tool call here
		// can legitimately take a while: build_game runs cmake.
		ReadHeaderTimeout: 30 * time.Second,
	}

	done := make(chan error, 1)
	go func() { done <- httpServer.Serve(listener) }()

	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		err := httpServer.Shutdown(shutdownCtx)
		// Waiting for Serve to return, not just for Shutdown to. Shutdown finishes once
		// in-flight requests are done, but it is Serve's own deferred close that releases
		// the listener - so returning without this leaves the port held for a moment
		// after the caller believes the server is stopped, and the next bind on it fails.
		// Asserted by a test that reopens the port.
		//
		// Its value is deliberately discarded. Once Shutdown has been called, Serve
		// returns http.ErrServerClosed and nothing else: its accept loop checks
		// shuttingDown() before it ever returns an Accept error (net/http/server.go).
		// This first inspected serveErr and preferred it over Shutdown's error, which
		// read as careful and was unreachable - mutation testing found two surviving
		// mutants in that condition, which is what unreachable code looks like from the
		// outside. Draining is the part that matters.
		<-done
		return err
	}
}
