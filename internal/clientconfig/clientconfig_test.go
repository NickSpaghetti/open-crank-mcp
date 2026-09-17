package clientconfig

import (
	"encoding/json"
	"strings"
	"testing"
)

func decode(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("rendered output is not valid JSON: %v\n%s", err, s)
	}
	return m
}

// OpenCode's four differences, each asserted, because each is a plausible thing
// to render in the other clients' shape and have look right.
func TestOpenCodeShape(t *testing.T) {
	out, err := Render("opencode", []string{"/opt/bin/open-crank-mcp"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	server := decode(t, out)["mcp"].(map[string]any)["open-crank-mcp"].(map[string]any)

	if server["type"] != "local" {
		t.Errorf(`type is %v, want "local"`, server["type"])
	}
	if _, ok := server["command"].([]any); !ok {
		t.Errorf("command is %T, want an argv array - OpenCode has no separate args field", server["command"])
	}
	if _, ok := server["environment"]; !ok {
		t.Error("no environment key; OpenCode calls it `environment`, not `env`")
	}
	if _, ok := server["env"]; ok {
		t.Error("rendered an `env` key, which is the other clients' spelling")
	}
}

// OpenCode defaults `timeout` to 5000ms and nothing here resolves at start-up,
// so no timeout should be emitted at all. Asserted rather than left implicit
// because an earlier design did emit one - the MCP command used to download a
// binary on first use - and reinstating it without that reason would be a number
// nobody could explain.
func TestOpenCodeEmitsNoTimeout(t *testing.T) {
	out, err := Render("opencode", []string{"/opt/bin/open-crank-mcp"})
	if err != nil {
		t.Fatal(err)
	}
	server := decode(t, out)["mcp"].(map[string]any)["open-crank-mcp"].(map[string]any)
	if v, present := server["timeout"]; present {
		t.Errorf("rendered timeout %v; the command is a server binary that starts "+
			"immediately, so OpenCode's default is correct", v)
	}
}

// Claude Code and Cursor split the command from its arguments and have no type
// discriminator.
func TestCommandAndArgsClients(t *testing.T) {
	for _, client := range []string{"claude", "cursor"} {
		t.Run(client, func(t *testing.T) {
			out, err := Render(client, []string{"/opt/bin/open-crank-mcp", "-x"})
			if err != nil {
				t.Fatal(err)
			}
			server := decode(t, out)["mcpServers"].(map[string]any)["open-crank-mcp"].(map[string]any)
			if server["command"] != "/opt/bin/open-crank-mcp" {
				t.Errorf("command is %v, want the bare path", server["command"])
			}
			args, ok := server["args"].([]any)
			if !ok || len(args) != 1 || args[0] != "-x" {
				t.Errorf("args is %v, want the remaining argv", server["args"])
			}
			if _, ok := server["type"]; ok {
				t.Error("rendered a type discriminator; these clients infer stdio from command")
			}
		})
	}
}

// The common case: one command, no arguments. An empty "args": [] would be
// accepted by both clients and is untidy rather than broken - but it is also the
// difference between the boundary being tested and not, and the same helper
// renders the argv-bearing case above.
func TestCommandAndArgsClientsOmitArgsWhenThereAreNone(t *testing.T) {
	for _, client := range []string{"claude", "cursor"} {
		t.Run(client, func(t *testing.T) {
			out, err := Render(client, []string{"/opt/bin/open-crank-mcp"})
			if err != nil {
				t.Fatal(err)
			}
			server := decode(t, out)["mcpServers"].(map[string]any)["open-crank-mcp"].(map[string]any)
			if v, present := server["args"]; present {
				t.Errorf("rendered args %v for a command with none", v)
			}
		})
	}
}

func TestRenderRejectsAnUnknownClient(t *testing.T) {
	_, err := Render("emacs", []string{"/x"})
	if err == nil {
		t.Fatal("an unknown client was accepted")
	}
	// The message has to list what is known, since the alternative is guessing.
	for _, client := range Clients() {
		if !strings.Contains(err.Error(), client) {
			t.Errorf("error %q does not mention the known client %q", err, client)
		}
	}
}
