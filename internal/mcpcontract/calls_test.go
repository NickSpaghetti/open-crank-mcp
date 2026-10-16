package mcpcontract

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// callTool sends arguments as JSON, exercising the SDK's marshal/validate layer that
// internal/tools's direct handler tests skip.
func callTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("tools/call %s: %v", name, err)
	}
	return res
}

func resultText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// TestRecoverableFailuresAreToolResults - calling a tool with no Simulator must return
// an IsError result the model can act on, not a JSON-RPC error.
func TestRecoverableFailuresAreToolResults(t *testing.T) {
	session := connect(t)

	// Every tool that needs a running Simulator, plus the two that need a resolved SDK.
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"press_button", map[string]any{"button": "a"}},
		{"hold_button", map[string]any{"button": "a"}},
		{"release_button", map[string]any{"button": "a"}},
		{"reset_input", map[string]any{}},
		{"set_crank", map[string]any{"crank_angle": 90}},
		{"get_screenshot", map[string]any{}},
		{"get_game_state", map[string]any{}},
		{"list_entities", map[string]any{}},
		{"get_logs", map[string]any{}},
		{"get_game_logs", map[string]any{}},
		{"stop_simulator", map[string]any{}},
		{"restart_simulator", map[string]any{}},
		{"read_save_data", map[string]any{}},
		{"get_status", map[string]any{}},
	} {
		res := callTool(t, session, tc.tool, tc.args)
		// get_status answering "nothing running" is a success.
		if tc.tool == "get_status" {
			if res.IsError {
				t.Errorf("get_status reported an error with nothing running; it is the one "+
					"tool whose job is to answer that question: %s", resultText(res))
			}
			continue
		}
		if !res.IsError {
			t.Errorf("%s succeeded with no Simulator running, want a model-visible error", tc.tool)
			continue
		}
		if resultText(res) == "" {
			t.Errorf("%s returned an error result with no text; the message is the only thing "+
				"telling the agent what to do about it", tc.tool)
		}
	}
}

// TestUnknownButtonIsRejectedOverTheWire - neither harness rejects a bad button name,
// so internal/tools must, for all three button tools.
func TestUnknownButtonIsRejectedOverTheWire(t *testing.T) {
	session := connect(t)
	for _, tool := range []string{"press_button", "hold_button", "release_button"} {
		res := callTool(t, session, tool, map[string]any{"button": "A"})
		if !res.IsError {
			t.Errorf("%s(button=A) succeeded, want an error naming the valid buttons", tool)
			continue
		}
		// The message must list the valid names so the agent has something to try.
		if msg := resultText(res); !strings.Contains(msg, "up") || !strings.Contains(msg, "right") {
			t.Errorf("%s(button=A) said %q, want the valid button names in it", tool, msg)
		}
	}
}

// TestUnknownDockModeIsRejectedOverTheWire - same, for set_crank's dock field, which
// both harnesses treat a typo in as "unchanged".
func TestUnknownDockModeIsRejectedOverTheWire(t *testing.T) {
	res := callTool(t, connect(t), "set_crank", map[string]any{"crank_dock": "DOCKED"})
	if !res.IsError {
		t.Fatal("set_crank(crank_dock=DOCKED) succeeded, want an error naming the valid modes")
	}
	if msg := resultText(res); !strings.Contains(msg, "docked") || !strings.Contains(msg, "undocked") {
		t.Errorf("set_crank(crank_dock=DOCKED) said %q, want the valid modes in it", msg)
	}
}

// TestSchemaViolationsAreRejected - the SDK validates arguments against the input
// schema before a handler runs, so handlers needn't.
func TestSchemaViolationsAreRejected(t *testing.T) {
	session := connect(t)

	// A string where the schema says integer.
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "press_button",
		Arguments: map[string]any{"button": "a", "duration_ms": "quite a while"},
	})
	if err == nil && (res == nil || !res.IsError) {
		t.Error("press_button accepted a string duration_ms; the declared schema says integer")
	}

	// A tool that does not exist.
	if _, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "press_buton",
		Arguments: map[string]any{"button": "a"},
	}); err == nil {
		t.Error("calling a tool that does not exist succeeded")
	}
}

// TestHoldButtonTakesNoDuration - a duration_ms on hold_button would let a caller
// write a hold that is secretly a tap.
func TestHoldButtonTakesNoDuration(t *testing.T) {
	for _, tool := range listTools(t) {
		if tool.Name != "hold_button" {
			continue
		}
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("hold_button inputSchema is %T, want an object", tool.InputSchema)
		}
		props, _ := schema["properties"].(map[string]any)
		if _, present := props["duration_ms"]; present {
			t.Error("hold_button declares duration_ms; a hold has no duration, and offering " +
				"one invites a caller to write a hold that silently expires. Use press_button.")
		}
		if _, present := props["button"]; !present {
			t.Error("hold_button does not declare button")
		}
		return
	}
	t.Fatal("hold_button is not registered")
}
