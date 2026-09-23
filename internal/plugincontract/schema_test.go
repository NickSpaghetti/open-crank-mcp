package plugincontract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

// Validates the portable pair against the published 1.0.0 schemas.
//
// Both files, not just the manifest. The manifest's schema is closed
// (additionalProperties: false), which is what catches a Claude Code field
// leaking into it; the MCP schema is a oneOf over three transport variants,
// which is what catches a missing `type`. Neither failure is visible by reading
// the file.
//
// Against vendored copies rather than a live fetch - see plugin/schemas/README.md
// for why, and `make plugin-upstream-check` for the job that notices upstream
// moving. No new dependency: jsonschema-go already drives the tool schemas in
// internal/tools.
func validateAgainstSchema(t *testing.T, schemaPath, docPath string) {
	t.Helper()

	schemaBytes, err := os.ReadFile(filepath.Join(repoRoot, schemaPath))
	if err != nil {
		t.Fatalf("reading %s: %v", schemaPath, err)
	}
	schemaBytes = stripLookaheadPattern(t, schemaBytes)

	var schema jsonschema.Schema
	if err := json.Unmarshal(schemaBytes, &schema); err != nil {
		t.Fatalf("%s is not a valid schema document: %v", schemaPath, err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatalf("resolving %s: %v", schemaPath, err)
	}

	docBytes, err := os.ReadFile(filepath.Join(repoRoot, docPath))
	if err != nil {
		t.Fatalf("reading %s: %v", docPath, err)
	}
	var doc any
	if err := json.Unmarshal(docBytes, &doc); err != nil {
		t.Fatalf("%s is not valid JSON: %v", docPath, err)
	}

	if err := resolved.Validate(doc); err != nil {
		t.Errorf("%s does not satisfy %s:\n%v", docPath, schemaPath, err)
	}
}

func TestPortableManifestMatchesTheSchema(t *testing.T) {
	validateAgainstSchema(t, "plugin/schemas/1.0.0/plugin.schema.json", "plugin/plugin.json")
}

func TestPortableMCPConfigMatchesTheSchema(t *testing.T) {
	validateAgainstSchema(t, "plugin/schemas/1.0.0/mcp.schema.json", "plugin/mcp.json")
}

// stripLookaheadPattern removes the one pattern in the 1.0.0 schemas that Go
// cannot compile, so the other ninety-odd percent of the schema still validates.
//
// plugin.schema.json constrains `name` with
// ^(?!.*(?:--|\.\.))[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$ - a negative lookahead,
// which RE2 does not implement, so jsonschema-go refuses to resolve the whole
// document and nothing at all gets checked. Dropping just that key is strictly
// better than skipping the file.
//
// The rule it expresses is not lost: TestPortableNameSatisfiesTheSpecPattern
// checks it directly below, by hand rather than with a pattern, which is what
// `make no-regex` requires of Go in this repo anyway.
//
// Narrow on purpose. It removes one named key rather than every pattern it
// cannot compile, so a future schema revision that adds another unsupported one
// fails loudly here instead of being silently skipped.
func stripLookaheadPattern(t *testing.T, schemaBytes []byte) []byte {
	t.Helper()

	var doc map[string]any
	if err := json.Unmarshal(schemaBytes, &doc); err != nil {
		t.Fatalf("parsing the schema: %v", err)
	}
	properties, ok := doc["properties"].(map[string]any)
	if !ok {
		return schemaBytes
	}
	name, ok := properties["name"].(map[string]any)
	if !ok {
		return schemaBytes
	}
	if _, present := name["pattern"]; !present {
		return schemaBytes
	}
	delete(name, "pattern")

	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("re-encoding the schema: %v", err)
	}
	return out
}

// The `name` rule from plugin.schema.json, checked by hand because the schema
// expresses it with a lookahead Go cannot compile.
//
// It matters beyond conformance: `name` is the skill namespace, so a name a
// client rejects is a plugin whose skills cannot be invoked.
func TestPortableNameSatisfiesTheSpecPattern(t *testing.T) {
	name, _ := readJSON(t, "plugin/plugin.json")["name"].(string)
	if name == "" {
		t.Fatal("plugin/plugin.json has no name")
	}

	isLowerAlnum := func(c byte) bool {
		return (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
	}

	if !isLowerAlnum(name[0]) {
		t.Errorf("name %q must start with a lowercase letter or digit", name)
	}
	if !isLowerAlnum(name[len(name)-1]) {
		t.Errorf("name %q must end with a lowercase letter or digit", name)
	}
	for i := 0; i < len(name); i++ {
		if c := name[i]; !isLowerAlnum(c) && c != '.' && c != '-' {
			t.Errorf("name %q contains %q, which is not lowercase alphanumeric, a dot, or a hyphen",
				name, string(c))
		}
	}
	for _, forbidden := range []string{"--", ".."} {
		if strings.Contains(name, forbidden) {
			t.Errorf("name %q contains %q, which the spec forbids", name, forbidden)
		}
	}
	if len(name) > 64 {
		t.Errorf("name %q is %d characters, over the 64 the schema allows", name, len(name))
	}
}
