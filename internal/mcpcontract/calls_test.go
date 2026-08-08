package mcpcontract

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// callTool is tools/call as a client makes it, with the arguments going out as JSON
// rather than as a Go struct. That is the difference this file is for: internal/tools's
// own tests call each handler directly with a typed input, so they cannot see anything
// that happens in the marshal/validate layer between a client and a handler.
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

// TestRecoverableFailuresAreToolResults - every tool here can be called before a
// Simulator exists, and that has to come back as an IsError *result* the model can read
// and react to, not as a JSON-RPC error.
//
// The distinction is invisible from inside internal/tools, because a handler returning
// (nil, err) and one returning an IsError result look similar in a Go test and land
// completely differently in a client: one is a message the agent can act on, the other
// is a protocol failure it usually cannot. Asserting it from the client side is the only
// way to be sure which one a caller gets.
func TestRecoverableFailuresAreToolResults(t *testing.T) {
	session := connect(t)

	// Every tool that needs a running Simulator, plus the two that need a resolved
	// SDK. This server has neither in this test, which is a real first-run state.
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
		// get_status is the exception and has to be: its whole job is answering
		// "is anything running?", so "no" is a successful answer.
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

// TestUnknownButtonIsRejectedOverTheWire - the validation internal/tools does exists
// because neither harness rejects a bad name, and this checks it survives the trip
// through a client, for all three button tools.
//
// "A" is the realistic mistake: the names are lower-case and nothing in the schema says
// so, which is how press_button("A") came to report success and do nothing.
func TestUnknownButtonIsRejectedOverTheWire(t *testing.T) {
	session := connect(t)
	for _, tool := range []string{"press_button", "hold_button", "release_button"} {
		res := callTool(t, session, tool, map[string]any{"button": "A"})
		if !res.IsError {
			t.Errorf("%s(button=A) succeeded, want an error naming the valid buttons", tool)
			continue
		}
		// The message has to list the real names. An agent that gets "unknown button"
		// and no alternatives has nothing to try next.
		if msg := resultText(res); !strings.Contains(msg, "up") || !strings.Contains(msg, "right") {
			t.Errorf("%s(button=A) said %q, want the valid button names in it", tool, msg)
		}
	}
}

// TestUnknownDockModeIsRejectedOverTheWire - same, for set_crank's three-valued dock
// field. A typo resolves to "unchanged" in both harnesses and is answered with success,
// so this layer is the only one that can say anything useful.
func TestUnknownDockModeIsRejectedOverTheWire(t *testing.T) {
	res := callTool(t, connect(t), "set_crank", map[string]any{"crank_dock": "DOCKED"})
	if !res.IsError {
		t.Fatal("set_crank(crank_dock=DOCKED) succeeded, want an error naming the valid modes")
	}
	if msg := resultText(res); !strings.Contains(msg, "docked") || !strings.Contains(msg, "undocked") {
		t.Errorf("set_crank(crank_dock=DOCKED) said %q, want the valid modes in it", msg)
	}
}

// TestSchemaViolationsAreRejected - the SDK validates arguments against the declared
// input schema before a handler runs, and that is worth one assertion rather than
// assuming: it is what makes a wrong type a protocol-level complaint instead of
// something each handler has to defend against by hand.
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

	// A tool that does not exist. Included because a client typo and a renamed tool
	// look the same from the outside, and both should be a clear failure rather than
	// a silent no-op.
	if _, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "press_buton",
		Arguments: map[string]any{"button": "a"},
	}); err == nil {
		t.Error("calling a tool that does not exist succeeded")
	}
}

// TestHoldButtonTakesNoDuration is the schema half of the hold/press split. The two
// tools differ in exactly one way a caller can see - hold_button has no duration_ms -
// and if that field ever appears on it, a caller can write a hold that is secretly a
// tap, which is the confusion the separate verb exists to prevent.
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
