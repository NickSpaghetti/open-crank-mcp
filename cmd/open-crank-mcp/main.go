package main

import (
	"context"
	"fmt"
	"log"
	"os"

	opencrank "github.com/NickSpaghetti/open-crank-mcp"
	"github.com/NickSpaghetti/open-crank-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	sdkPath := os.Getenv("PLAYDATE_SDK_PATH")
	if sdkPath == "" {
		fmt.Fprintln(os.Stderr, "PLAYDATE_SDK_PATH is not set")
		os.Exit(1)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "open-crank-mcp"}, nil)
	tools.RegisterAll(server, tools.NewServer(sdkPath, opencrank.HarnessFS))

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Printf("server failed: %v", err)
		os.Exit(1)
	}
}
