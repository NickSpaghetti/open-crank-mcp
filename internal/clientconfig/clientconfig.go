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

// Invocation is how a client should start this server.
type Invocation struct {
	// Argv is the command, already absolute.
	Argv []string

	// Resolves is true when Argv names the plugin launcher rather than a server
	// binary - meaning the first run for a version downloads before it serves.
	//
	// It exists to decide one field, and only OpenCode has that field. See
	// openCodeTimeoutMS.
	Resolves bool
}

// openCodeTimeoutMS is what OpenCode's `timeout` is set to when the command
// resolves a binary before serving.
//
// OpenCode defaults this to 5000 ms. A cold first run downloads about 10 MB, so
// the default does not merely risk a timeout - it guarantees one, on every
// machine, on the first run after install or after a version bump. This is the
// same first-run problem Claude Code has; OpenCode's answer is a number in a
// config file rather than a hook, and this is the only place that number can be
// got right on the user's behalf.
//
// Not emitted when the command is a plain binary: there is nothing to resolve,
// the default is fine, and a raised timeout there would be a number nobody can
// explain.
const openCodeTimeoutMS = 120000

// Clients returns the client names Render accepts, for a usage message.
func Clients() []string {
	out := make([]string, 0, len(renderers))
	for name := range renderers {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

var renderers = map[string]func(Invocation) (string, error){
	"opencode": renderOpenCode,
	"claude":   renderClaude,
	"cursor":   renderCursor,
}

// Render produces the block for one client.
func Render(client string, inv Invocation) (string, error) {
	render, ok := renderers[client]
	if !ok {
		return "", fmt.Errorf("unknown client %q; known clients are %s",
			client, strings.Join(Clients(), ", "))
	}
	if len(inv.Argv) == 0 {
		return "", fmt.Errorf("no command to render for %q", client)
	}
	return render(inv)
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
func renderOpenCode(inv Invocation) (string, error) {
	server := map[string]any{
		"type":        "local",
		"command":     inv.Argv,
		"environment": map[string]any{},
	}
	if inv.Resolves {
		server["timeout"] = openCodeTimeoutMS
	}
	return encode(map[string]any{
		"$schema": "https://opencode.ai/config.json",
		"mcp":     map[string]any{"open-crank-mcp": server},
	})
}

// Claude Code and Cursor share a shape: a command string plus an args array, and
// no type discriminator - stdio is inferred from the presence of `command`.
func renderCommandAndArgs(inv Invocation) map[string]any {
	server := map[string]any{"command": inv.Argv[0]}
	if len(inv.Argv) > 1 {
		server["args"] = inv.Argv[1:]
	}
	return map[string]any{"mcpServers": map[string]any{"open-crank-mcp": server}}
}

func renderClaude(inv Invocation) (string, error) { return encode(renderCommandAndArgs(inv)) }
func renderCursor(inv Invocation) (string, error) { return encode(renderCommandAndArgs(inv)) }
