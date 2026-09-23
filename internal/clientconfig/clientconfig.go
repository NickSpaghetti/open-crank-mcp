// Package clientconfig renders the MCP configuration block a client needs, for
// clients that cannot install this server themselves.
//
// It exists for OpenCode. OpenCode has plugins - TS/JS modules from
// @opencode-ai/plugin - but its v1 plugin API has no config or MCP hook, so a
// plugin cannot register a server there. Its config is written by hand, and a
// binary that prints the exact block to paste is the thing that helps. Claude
// Code and Cursor are rendered too, because once the shapes are data the extra
// two cost nothing and the guides then have one source for all three.
//
// Rendering is separated from detecting for the reason cmd/sdk-path/main.go
// gives about sdk.Paths.Describe: the rendering is the part worth testing, and
// it is pure, so every client's shape is a table entry rather than something
// only reproducible on a machine in the right state.
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

// encode renders a config as indented JSON with a trailing newline, so the
// output is paste-ready rather than something to reformat.
func encode(v any) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}

// OpenCode's shape differs from the other two in four ways at once: the command
// is a single argv array rather than a command plus args, the environment map is
// called `environment` rather than `env`, it is declared `type: "local"`, and it
// has a timeout that has to be raised. Each one is an easy thing to get subtly
// right-looking and wrong.
func renderOpenCode(argv []string) (string, error) {
	// No `timeout`. OpenCode defaults it to 5000ms, and the command here is a
	// server binary that starts immediately - the ocm-install-server skill has already
	// put it in place. An earlier design had the MCP command resolve and download
	// a binary on first use, which did not fit in that default and so needed the
	// timeout raised; nothing resolves at start-up now, and emitting a number whose
	// reason no longer applies would be cargo cult.
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
