package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	opencrank "github.com/NickSpaghetti/open-crank-mcp"
	"github.com/NickSpaghetti/open-crank-mcp/internal/httpserve"
	"github.com/NickSpaghetti/open-crank-mcp/internal/sdk"
	"github.com/NickSpaghetti/open-crank-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	// stdio is the default and what every MCP client uses. -http exists for contract
	// testing tools, which speak HTTP and not stdio: Specmatic's MCP auto-test accepts
	// only STREAMABLE_HTTP for --transport-kind. Both transports serve the same
	// RegisterAll, so the HTTP endpoint cannot present a different tool surface than
	// the one a client gets.
	//
	// Loopback addresses only, enforced in internal/httpserve. This server builds code
	// and launches processes on request and has no authentication.
	httpAddr := flag.String("http", "", "serve over Streamable HTTP on this loopback address "+
		"(e.g. 127.0.0.1:8237) instead of stdio. For contract testing; MCP clients use stdio.")
	flag.Parse()

	// Resolution failing is deliberately not fatal.
	//
	// This used to exit(1) when PLAYDATE_SDK_PATH was unset, which was harmless
	// when the only way to run this was a container that always set it. Run
	// natively, an unset variable is the ordinary first-time case, and exiting
	// before the MCP handshake gets reported by the client as "server failed to
	// start" and nothing else: the message explaining what to do goes to a stderr
	// nobody is reading.
	//
	// So serve regardless, and let the tools that actually need an SDK say so in
	// a result the agent can read and act on. See tools.Server.requireSDK.
	paths, sdkErr := sdk.Resolve(sdk.OSEnv())
	if sdkErr != nil {
		fmt.Fprintf(os.Stderr, "open-crank-mcp: %v\n", sdkErr)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "open-crank-mcp"}, nil)
	tools.RegisterAll(server, tools.NewServer(paths, sdkErr, opencrank.HarnessFS))

	if *httpAddr != "" {
		// Checked before announcing anything. Serve checks too - it has to, being
		// exported - but printing "serving on http://0.0.0.0:9999" and then refusing
		// to serve it reads like a crash rather than a deliberate refusal.
		if err := httpserve.CheckLoopback(*httpAddr); err != nil {
			fmt.Fprintf(os.Stderr, "open-crank-mcp: -http %v\n", err)
			os.Exit(1)
		}
		// Logged rather than silent: unlike stdio, nothing else tells you the server
		// came up, and a contract run that connected to the wrong port is confusing to
		// diagnose. stderr, because stdout is the MCP channel on the other path and
		// keeping the two consistent is worth more than the convenience.
		fmt.Fprintf(os.Stderr, "open-crank-mcp: serving MCP over Streamable HTTP on http://%s\n", *httpAddr)
		if err := httpserve.Serve(context.Background(), server, *httpAddr); err != nil {
			log.Printf("http server failed: %v", err)
			os.Exit(1)
		}
		return
	}

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Printf("server failed: %v", err)
		os.Exit(1)
	}
}
