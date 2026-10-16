// Package httpserver exposes the MCP server over Streamable HTTP, for contract
// testers like Specmatic that cannot speak stdio.
package httpserver

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
// cancelled.
const shutdownGrace = 2 * time.Second

// CheckLoopback rejects any address that is not loopback, since the server has no
// authentication. A missing host is rejected because ":8237" means every interface.
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

// Handler is the MCP HTTP handler for server. Stateless with plain JSON responses,
// so testers that never send Mcp-Session-Id or parse event streams still work. The
// SDK's DNS-rebinding protection stays at its default (on).
func Handler(server *mcp.Server) http.Handler {
	return mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true},
	)
}

// Serve listens on addr and serves server until ctx is cancelled. A bind failure is
// returned so a port already in use fails the run.
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

// ServeListener serves server on an already-bound listener until ctx is cancelled.
func ServeListener(ctx context.Context, server *mcp.Server, listener net.Listener) error {
	httpServer := &http.Server{
		Handler: Handler(server),
		// Non-zero so an idle client can't hold a connection forever.
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
		// Serve's deferred close is what releases the listener, so wait for it or the
		// next bind on the port fails. After Shutdown it only returns ErrServerClosed.
		<-done
		return err
	}
}
