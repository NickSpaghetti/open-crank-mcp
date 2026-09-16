package clientconfig

import (
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/NickSpaghetti/open-crank-mcp/internal/sdk"
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
	out, err := Render("opencode", Invocation{Argv: []string{"/p/bin/launcher"}, Resolves: true})
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

// The timeout is the field this renderer exists to get right. OpenCode defaults
// it to 5000 ms and a cold first run downloads ~10 MB, so a launcher command
// without it times out on every machine's first run.
func TestOpenCodeRaisesTheTimeoutOnlyWhenSomethingResolves(t *testing.T) {
	viaLauncher, err := Render("opencode", Invocation{Argv: []string{"/p/bin/launcher"}, Resolves: true})
	if err != nil {
		t.Fatal(err)
	}
	server := decode(t, viaLauncher)["mcp"].(map[string]any)["open-crank-mcp"].(map[string]any)
	timeout, ok := server["timeout"].(float64)
	if !ok {
		t.Fatal("a launcher command rendered no timeout; the 5000ms default guarantees a " +
			"first-run failure")
	}
	if timeout <= 5000 {
		t.Errorf("timeout is %v, which is not above OpenCode's 5000ms default", timeout)
	}

	direct, err := Render("opencode", Invocation{Argv: []string{"/usr/local/bin/open-crank-mcp"}})
	if err != nil {
		t.Fatal(err)
	}
	server = decode(t, direct)["mcp"].(map[string]any)["open-crank-mcp"].(map[string]any)
	if _, ok := server["timeout"]; ok {
		t.Error("a plain binary rendered a raised timeout; nothing resolves on that path, " +
			"so the number would be unexplainable")
	}
}

// Claude Code and Cursor split the command from its arguments and have no type
// discriminator.
func TestCommandAndArgsClients(t *testing.T) {
	for _, client := range []string{"claude", "cursor"} {
		t.Run(client, func(t *testing.T) {
			out, err := Render(client, Invocation{Argv: []string{"/p/bin/launcher", "-x"}})
			if err != nil {
				t.Fatal(err)
			}
			server := decode(t, out)["mcpServers"].(map[string]any)["open-crank-mcp"].(map[string]any)
			if server["command"] != "/p/bin/launcher" {
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
			out, err := Render(client, Invocation{Argv: []string{"/p/bin/launcher"}})
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
	_, err := Render("emacs", Invocation{Argv: []string{"/x"}})
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

func envWith(files fstest.MapFS) sdk.Env {
	return sdk.Env{
		FS:      files,
		Getenv:  func(string) string { return "" },
		HomeDir: func() (string, error) { return "/home/u", nil },
	}
}

func TestDetectFindsTheLauncherFromInsideACheckout(t *testing.T) {
	files := fstest.MapFS{
		"repo/plugin/bin/open-crank-mcp-launcher": {Data: []byte("#!/bin/sh\n")},
		"repo/plugin/plugin.json":                 {Data: []byte("{}")},
	}
	got := Detect(envWith(files), "/repo/open-crank-mcp")
	if !got.Resolves {
		t.Error("a checkout was not reported as resolving, so OpenCode would get no timeout")
	}
	if got.Argv[0] != "/repo/plugin/bin/open-crank-mcp-launcher" {
		t.Errorf("Argv[0] = %q, want the launcher", got.Argv[0])
	}
}

// The binary may sit well below the repository root - cmd output, a bin/
// directory - so detection walks up rather than looking only beside itself.
func TestDetectWalksUpToTheCheckout(t *testing.T) {
	files := fstest.MapFS{
		"repo/plugin/bin/open-crank-mcp-launcher": {Data: []byte("#!/bin/sh\n")},
		"repo/plugin/plugin.json":                 {Data: []byte("{}")},
	}
	got := Detect(envWith(files), "/repo/some/deep/dir/open-crank-mcp")
	if !got.Resolves || got.Argv[0] != "/repo/plugin/bin/open-crank-mcp-launcher" {
		t.Errorf("Detect from a nested path = %+v, want the checkout's launcher", got)
	}
}

// The release-binary case, and the one that must not be reached by accident: a
// plugin directory that is missing its manifest is not a checkout, because the
// launcher reads the version out of that manifest.
func TestDetectFallsBackToTheBinaryItself(t *testing.T) {
	for name, files := range map[string]fstest.MapFS{
		"nothing above it": {},
		"a launcher with no manifest beside it": {
			"opt/plugin/bin/open-crank-mcp-launcher": {Data: []byte("#!/bin/sh\n")},
		},
	} {
		t.Run(name, func(t *testing.T) {
			const exe = "/opt/cache/open-crank-mcp-0.1.0"
			got := Detect(envWith(files), exe)
			if got.Resolves {
				t.Error("reported as resolving, but there is no launcher to resolve through")
			}
			if got.Argv[0] != exe {
				t.Errorf("Argv[0] = %q, want the binary itself (%q)", got.Argv[0], exe)
			}
		})
	}
}
