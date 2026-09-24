//go:build linux

package contracttest

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	opencrank "github.com/NickSpaghetti/open-crank-mcp"
	"github.com/NickSpaghetti/open-crank-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestRestartThenScreenshot answers one question: is the post-restart
// screenshot failure seen on Windows CI actually specific to Windows.
//
// Nothing else in this repo drives restart followed by a real screenshot.
// TestSDKContract screenshots but never restarts, and it hands the Simulator
// the data directory as argv[2] rather than a scratch directory, so it misses
// the mechanism entirely. mcp-auto-test skips both tools by name. The Windows
// contract test is the first thing to try the sequence, which makes "it only
// fails on Windows" an untested claim rather than a finding.
//
// Driven through the MCP tools rather than through raw harness round trips,
// because restart_simulator is where the scratch directory is replaced and that
// only happens inside the server.
func TestRestartThenScreenshot(t *testing.T) {
	if os.Getenv("OPEN_CRANK_SDK_CONTRACT") == "" {
		t.Skip("OPEN_CRANK_SDK_CONTRACT not set - run `make sdk-contract-check`")
	}
	paths := contractSDK(t)
	repoRoot := findRepoRoot(t)

	// Its own display number, matching the convention in the other contract
	// tests: each is self-contained rather than relying on execution order.
	startDisplay(t, ":97")
	pdxPath := buildLuaFixture(t, repoRoot)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	server := mcp.NewServer(&mcp.Implementation{Name: "open-crank-mcp-restart-contract", Version: "test"}, nil)
	tools.RegisterAll(server, tools.NewServer(paths, nil, opencrank.HarnessFS))
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connecting MCP server: %v", err)
	}
	client, err := mcp.NewClient(&mcp.Implementation{Name: "restart-contract", Version: "test"}, nil).
		Connect(ctx, clientTransport, nil)
	if err != nil {
		_ = serverSession.Close()
		t.Fatalf("connecting MCP client: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Close()
		_ = serverSession.Wait()
	})

	call := func(name string, args map[string]any) *mcp.CallToolResult {
		t.Helper()
		result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("MCP call %s: %v", name, err)
		}
		if result.IsError {
			t.Fatalf("MCP tool %s failed: %v", name, result.Content)
		}
		return result
	}

	call("launch_simulator", map[string]any{"pdx_path": pdxPath})
	t.Cleanup(func() {
		_, _ = client.CallTool(context.Background(), &mcp.CallToolParams{Name: "stop_simulator"})
	})

	// A real round trip, not get_status. harness_reachable is a stat on the data
	// directory, so after a restart it is true before the new game is up, and
	// gating on it would make this test agree with a broken one.
	waitForHarness := func(stage string) {
		t.Helper()
		deadline := time.Now().Add(60 * time.Second)
		var last string
		for time.Now().Before(deadline) {
			result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "get_game_state", Arguments: map[string]any{}})
			switch {
			case err != nil:
				last = err.Error()
			case result.IsError:
				last = fmt.Sprint(result.Content)
			default:
				return
			}
			time.Sleep(250 * time.Millisecond)
		}
		t.Fatalf("harness never answered a round trip %s, last error: %s", stage, last)
	}

	hasPNG := func(result *mcp.CallToolResult) bool {
		for _, content := range result.Content {
			if image, ok := content.(*mcp.ImageContent); ok && image.MIMEType == "image/png" && len(image.Data) != 0 {
				return true
			}
		}
		return false
	}

	waitForHarness("after launch")
	if !hasPNG(call("get_screenshot", map[string]any{})) {
		t.Fatal("get_screenshot returned no PNG before any restart")
	}

	call("restart_simulator", map[string]any{})
	waitForHarness("after restart")
	if !hasPNG(call("get_screenshot", map[string]any{})) {
		t.Fatal("get_screenshot returned no PNG after restart")
	}
}
