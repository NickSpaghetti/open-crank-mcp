package tools

import (
	"github.com/NickSpaghetti/open-crank-mcp/internal/sdk"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"testing/fstest"
)

// mcp.AddTool panics if a tool's input/output struct cannot be turned into a
// JSON schema.
func TestRegisterAllDoesNotPanic(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test"}, nil)
	RegisterAll(server, NewServer(sdk.Paths{Root: "/fake/sdk"}, nil, fstest.MapFS{}))
}
