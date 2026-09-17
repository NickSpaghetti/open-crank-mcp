// Package plugincontract asserts the things about the plugin's own files that
// nothing else can: the shape a client parses before it will run anything, and
// the agreement between files that each carry a copy of the same fact.
//
// Test-only, with no production files, deliberately following
// internal/mcpcontract. There is no runtime behaviour here to export - these
// files have real readers elsewhere (a client, the launcher), and a second
// parser living in the binary would be a second opinion about them.
//
// The division of labour with schema validation is the one docs/GOTCHAS.md
// already draws for tool inputs: declare what a schema can express in the
// schema, and keep here only what it cannot - agreement between files, the
// absence of a key, whether a path on disk exists.
package plugincontract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const repoRoot = "../.."

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot, path))
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%s is not valid JSON: %v", path, err)
	}
	return m
}

// Every manifest that carries a version, and there is more than one because each
// client insists on its own file.
var versionedManifests = []string{
	"plugin/plugin.json",
	"plugin/.claude-plugin/plugin.json",
	"plugin/.cursor-plugin/plugin.json",
}

// Three files carrying one fact is three chances to drift, and the launcher
// reads the first of them to decide which release asset to fetch - so a stale
// version in it is an install that downloads the wrong thing, or nothing.
//
// Written over the list rather than comparing two named files, so adding a
// fourth client's manifest to the slice is the whole change.
func TestEveryManifestDeclaresTheSameVersion(t *testing.T) {
	want, _ := readJSON(t, versionedManifests[0])["version"].(string)
	if want == "" {
		t.Fatalf("%s has no version, and it is the source every other one copies",
			versionedManifests[0])
	}
	for _, path := range versionedManifests[1:] {
		got, _ := readJSON(t, path)["version"].(string)
		if got != want {
			t.Errorf("%s says version %q, %s says %q", path, got, versionedManifests[0], want)
		}
	}
}

// The portable manifest's schema is closed (additionalProperties: false), so a
// Claude Code field landing in it is a validation failure rather than a harmless
// extra. Asserted here as well as by the schema because the failure mode is
// silent at authoring time: copying userConfig or mcpServers across is an easy
// and plausible edit.
func TestPortableManifestCarriesNoClientSpecificFields(t *testing.T) {
	allowed := map[string]bool{
		"$schema": true, "name": true, "version": true, "description": true,
		"author": true, "homepage": true, "repository": true, "license": true,
		"keywords": true, "extensions": true,
	}
	for key := range readJSON(t, "plugin/plugin.json") {
		if !allowed[key] {
			t.Errorf("plugin/plugin.json has %q, which the 1.0.0 schema does not permit. "+
				"Client-specific settings belong under \"extensions\", or in that client's "+
				"own manifest.", key)
		}
	}
}

// mcpCommand digs the single server's command out of an MCP config.
func mcpCommand(t *testing.T, path string) map[string]any {
	t.Helper()
	servers, ok := readJSON(t, path)["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no mcpServers object", path)
	}
	entry, ok := servers["open-crank-mcp"].(map[string]any)
	if !ok {
		t.Fatalf("%s declares no open-crank-mcp server", path)
	}
	return entry
}

// The single most expensive mistake available in these files, and the one an
// earlier draft of the plan actually made: writing ${CLAUDE_PLUGIN_ROOT} into
// the portable config. The 1.0.0 spec does not interpolate `command` at all, so
// it would reach a conforming client as a literal path containing a dollar sign
// and fail with something unrecognisable.
//
// Also guards the other direction, because Cursor's own documented example adds
// "cwd": "${CURSOR_PLUGIN_ROOT}" - which fails the spec's cwd pattern outright.
// Someone following those docs to "fix" this file is the expected way it breaks,
// so the failure message says why rather than only what.
func TestPortableMCPConfigIsSpecShaped(t *testing.T) {
	entry := mcpCommand(t, "plugin/mcp.json")

	if entry["type"] != "stdio" {
		t.Errorf("plugin/mcp.json must declare \"type\": \"stdio\"; it is required for "+
			"stdio servers in the 1.0.0 schema, got %v", entry["type"])
	}

	// A bare executable name, resolved on PATH. The spec allows that or a
	// plugin-relative "./" path; there is no file inside the plugin to point at
	// since the server is downloaded by /open-crank-mcp:install-server, so the
	// bare form is the only one that can work here.
	command, _ := entry["command"].(string)
	if strings.ContainsAny(command, "/\\") {
		t.Errorf("plugin/mcp.json command is %q; it must be a bare executable name "+
			"resolved on PATH, because the plugin ships no binary to point at", command)
	}
	if strings.Contains(command, "${") {
		t.Errorf("plugin/mcp.json command is %q, but interpolation does not apply to "+
			"`command` in the 1.0.0 spec - it would arrive at a conforming client as a "+
			"literal. The absolute form belongs in .claude-plugin/mcp.json.", command)
	}
	if _, present := entry["cwd"]; present {
		t.Error("plugin/mcp.json must not set cwd. Cursor's documented example uses " +
			"${CURSOR_PLUGIN_ROOT}, which fails the spec's cwd pattern, and ${PLUGIN_ROOT} " +
			"is not expanded by Cursor. Omitting it is the only form both accept, because " +
			"the spec already requires the plugin root as the default working directory.")
	}
}

// Claude Code does interpolate `command`, and the spike measured the server
// being started with cwd set to the user's working directory - so a ./-relative
// command here would resolve against the wrong place.
func TestClaudeMCPConfigUsesAnAbsoluteInterpolatedCommand(t *testing.T) {
	command, _ := mcpCommand(t, "plugin/.claude-plugin/mcp.json")["command"].(string)
	if !strings.HasPrefix(command, "${CLAUDE_PLUGIN_DATA}/") {
		t.Errorf("Claude Code's command is %q; it must be rooted at ${CLAUDE_PLUGIN_DATA}, "+
			"which is where /open-crank-mcp:install-server writes the binary and which "+
			"survives plugin updates", command)
	}
}

// Every config must name the same binary, spelled for its client. A release that
// updated one and not the other would leave a client starting something the
// install skill never put there.
func TestEveryMCPConfigNamesTheSameBinary(t *testing.T) {
	const want = "open-crank-mcp"
	for _, path := range []string{
		"plugin/mcp.json",
		"plugin/.claude-plugin/mcp.json",
		"plugin/.cursor-plugin/mcp.json",
	} {
		command, _ := mcpCommand(t, path)["command"].(string)
		if filepath.Base(command) != want {
			t.Errorf("%s names %q, whose basename is %q; every config must end in %q",
				path, command, filepath.Base(command), want)
		}
	}
}

// The spike installed a plugin whose manifest declared "hooks":
// "./hooks/hooks.json", and it installed fine and then failed to load with
// "Duplicate hooks file detected ... The standard hooks/hooks.json is loaded
// automatically". `claude plugin validate` passed it, --strict included, so only
// a real install caught it - which is why it is asserted here.
func TestClaudeManifestDeclaresNoDefaultComponentPaths(t *testing.T) {
	manifest := readJSON(t, "plugin/.claude-plugin/plugin.json")
	for _, field := range []string{"hooks", "skills", "commands", "agents"} {
		if v, present := manifest[field]; present {
			t.Errorf("the Claude Code manifest declares %q: %v. Components at their default "+
				"locations are discovered automatically, and declaring one that is already "+
				"found there makes the plugin fail to load after installing cleanly.",
				field, v)
		}
	}
}

// userConfig entries need a title. --strict rejected the first attempt at this
// with "userConfig.playdate_sdk_path.title: Invalid input", which is not a thing
// the documentation says in so many words.
func TestUserConfigEntriesAreComplete(t *testing.T) {
	manifest := readJSON(t, "plugin/.claude-plugin/plugin.json")
	userConfig, ok := manifest["userConfig"].(map[string]any)
	if !ok {
		return // none declared is fine
	}
	for key, raw := range userConfig {
		entry, ok := raw.(map[string]any)
		if !ok {
			t.Errorf("userConfig.%s is not an object", key)
			continue
		}
		for _, required := range []string{"type", "title", "description"} {
			if v, _ := entry[required].(string); v == "" {
				t.Errorf("userConfig.%s has no %q; `claude plugin validate --strict` "+
					"rejects the manifest without it", key, required)
			}
		}
	}
}
func TestMarketplacePointsAtTheRealPlugin(t *testing.T) {
	plugins, ok := readJSON(t, ".claude-plugin/marketplace.json")["plugins"].([]any)
	if !ok || len(plugins) == 0 {
		t.Fatal(".claude-plugin/marketplace.json lists no plugins")
	}
	for _, raw := range plugins {
		entry := raw.(map[string]any)
		source, _ := entry["source"].(string)
		if !strings.HasPrefix(source, "./") {
			t.Errorf("marketplace source %q must be a relative path beginning with \"./\"", source)
		}
		manifest := filepath.Join(repoRoot, source, ".claude-plugin", "plugin.json")
		if _, err := os.Stat(manifest); err != nil {
			t.Errorf("marketplace source %q has no .claude-plugin/plugin.json under it: %v",
				source, err)
		}
		// Declaring a version in both places lets plugin.json silently win.
		if _, present := entry["version"]; present {
			t.Error("the marketplace entry declares a version; plugin.json owns it, and " +
				"declaring it twice means one of them is quietly ignored")
		}
	}
}

// A declared logo must exist, and it must resolve from the *plugin* root.
//
// Cursor turns a relative logo path into a raw.githubusercontent.com URL built
// from the repository and commit SHA, so a path that is wrong or missing does not
// fail at install - it produces a listing with a broken image, discovered by
// whoever looks at the marketplace. Its submission checklist requires "all paths
// in manifest are relative and valid (no `..`, no absolute paths)".
//
// The base is the plugin directory, not the repository root, which is the detail
// worth pinning: this plugin lives in a subdirectory, so `assets/logo.png` means
// plugin/assets/logo.png. An earlier plan put it at the repository root, where it
// would have resolved to a URL with nothing behind it.
func TestDeclaredLogoExistsUnderThePluginRoot(t *testing.T) {
	for _, manifest := range []string{
		"plugin/.cursor-plugin/plugin.json",
		"plugin/plugin.json",
	} {
		logo, _ := readJSON(t, manifest)["logo"].(string)
		if logo == "" {
			continue // optional; the checklist says "if provided"
		}
		if strings.HasPrefix(logo, "http://") || strings.HasPrefix(logo, "https://") {
			continue // absolute URLs are explicitly accepted, and not ours to check
		}
		if strings.HasPrefix(logo, "/") || strings.Contains(logo, "..") {
			t.Errorf("%s declares logo %q; the path must be relative with no \"..\"",
				manifest, logo)
			continue
		}
		if _, err := os.Stat(filepath.Join(repoRoot, "plugin", logo)); err != nil {
			t.Errorf("%s declares logo %q, which does not exist at plugin/%s. "+
				"Cursor resolves it against the plugin root and serves it from "+
				"raw.githubusercontent.com, so a missing file is a broken image in the "+
				"marketplace listing rather than a failed install.", manifest, logo, logo)
		}
	}
}
