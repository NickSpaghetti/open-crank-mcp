package tools

import (
	"github.com/NickSpaghetti/open-crank-mcp/internal/sdk"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"testing/fstest"
)

// mcp.AddTool panics if a tool's input/output struct can't be turned into a
// JSON schema (e.g. a bad struct tag). This is the regression test for
// that - it doesn't need a real simulator, just confirms every tool this
// package registers has valid, inferable schemas.
func TestRegisterAllDoesNotPanic(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test"}, nil)
	RegisterAll(server, NewServer(sdk.Paths{Root: "/fake/sdk"}, nil, fstest.MapFS{}))
}
