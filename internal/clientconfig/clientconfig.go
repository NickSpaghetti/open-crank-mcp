// Package clientconfig renders the MCP configuration block to paste into a client.
// OpenCode's v1 plugin API has no MCP hook, so its config must be written by hand.
package clientconfig

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Clients returns the client names Render accepts, for a usage message.
func Clients() []string {
	out := make([]string, 0, len(renderers))
	for name := range renderers {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

var renderers = map[string]func([]string) (string, error){
	"opencode": renderOpenCode,
	"claude":   renderClaude,
	"cursor":   renderCursor,
}

// Render produces the block for one client. argv is the command a client should
// run, already absolute.
func Render(client string, argv []string) (string, error) {
	render, ok := renderers[client]
	if !ok {
		return "", fmt.Errorf("unknown client %q; known clients are %s",
			client, strings.Join(Clients(), ", "))
	}
	if len(argv) == 0 {
		return "", fmt.Errorf("no command to render for %q", client)
	}
	return render(argv)
}

// encode renders a config as paste-ready indented JSON with a trailing newline.
func encode(v any) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}

// OpenCode wants a single argv array, `environment` rather than `env`, and
// `type: "local"`. Its default 5000ms timeout suffices for a binary that starts immediately.
func renderOpenCode(argv []string) (string, error) {
	server := map[string]any{
		"type":        "local",
		"command":     argv,
		"environment": map[string]any{},
	}
	return encode(map[string]any{
		"$schema": "https://opencode.ai/config.json",
		"mcp":     map[string]any{"open-crank-mcp": server},
	})
}

// Claude Code and Cursor share a shape: a command string plus an args array, and
// no type discriminator - stdio is inferred from the presence of `command`.
func renderCommandAndArgs(argv []string) map[string]any {
	server := map[string]any{"command": argv[0]}
	if len(argv) > 1 {
		server["args"] = argv[1:]
	}
	return map[string]any{"mcpServers": map[string]any{"open-crank-mcp": server}}
}

func renderClaude(argv []string) (string, error) { return encode(renderCommandAndArgs(argv)) }
func renderCursor(argv []string) (string, error) { return encode(renderCommandAndArgs(argv)) }
