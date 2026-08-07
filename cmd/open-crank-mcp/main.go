package main

import (
	"context"
	"fmt"
	"log"
	"os"

	opencrank "github.com/NickSpaghetti/open-crank-mcp"
	"github.com/NickSpaghetti/open-crank-mcp/internal/sdk"
	"github.com/NickSpaghetti/open-crank-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
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

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Printf("server failed: %v", err)
		os.Exit(1)
	}
}
