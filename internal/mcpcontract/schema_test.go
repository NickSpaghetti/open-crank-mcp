// Package mcpcontract drives this server the way an MCP client does - over a real
// JSON-RPC session, through the real go-sdk client - and asserts what that client
// sees. It is the counterpart to internal/contracttest: that one is this project's
// contract with Panic's SDK, this one is its contract with MCP clients.
//
// It needs no Simulator, no SDK and no Docker, so it runs in the ordinary `go test
// ./...` job on every platform. That matters, because the failure it exists for is
// invisible to every other test here: a tool's schema can be spec-legal, inferred
// without error, and still be rejected by a client's own validator. When that happened
// (docs/GOTCHAS.md, read_save_data's `any` field rendering as `"data": true`) the
// rejection aborted the entire tools/list fetch, so every tool became unusable from
// Claude Code at once - and nothing in this repo noticed, because nothing in this repo
// looked at the surface a client actually reads.
package mcpcontract

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/NickSpaghetti/open-crank-mcp/internal/harness"
	"github.com/NickSpaghetti/open-crank-mcp/internal/sdk"
	crankSetup "github.com/NickSpaghetti/open-crank-mcp/internal/setup"
	"github.com/NickSpaghetti/open-crank-mcp/internal/tools"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// updateGolden rewrites goldenPath instead of comparing against it. `make mcp-schema`
// is this flag; there is deliberately no separate dumper binary, because two programs
// that both claim to produce the golden file can disagree about it, and then the check
// passes while describing something no client is served.
var updateGolden = flag.Bool("update", false, "rewrite the golden tool schema instead of comparing against it")

// goldenPath is under docs/ rather than in testdata/, which is the less idiomatic of
// the two on purpose: it is a published description of the tool surface, useful to
// anyone wiring up a client, and a schema change belongs somewhere a reader would
// think to look. The cost is a test reaching two directories up.
var goldenPath = filepath.Join("..", "..", "docs", "mcp-schema.json")

// connect returns a client session talking to a fully registered server over an
// in-memory transport.
//
// The SDK path and harness FS are stand-ins: every schema here is derived from Go
// types, so none of it depends on a resolved SDK. Passing an error alongside them is
// the honest reproduction of a first run on a machine with no SDK installed, which is
// the state a client's very first tools/list arrives in.
func connect(t *testing.T) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()

	server := mcp.NewServer(&mcp.Implementation{Name: "open-crank-mcp"}, nil)
	tools.RegisterAll(server, tools.NewServer(
		sdk.Paths{Root: "/fake/sdk"},
		fmt.Errorf("no SDK for this test"),
		fstest.MapFS{},
	))

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connecting the server: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "mcpcontract", Version: "v0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connecting the client: %v", err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		_ = serverSession.Wait()
	})
	return session
}

// listTools is tools/list as a client receives it. Every schema in the result is a
// map[string]any - the client-side shape, decoded from the wire - rather than the
// jsonschema.Schema the server built. That distinction is the whole point: the
// incident this package exists for happened in the gap between the two.
func listTools(t *testing.T) []*mcp.Tool {
	t.Helper()
	res, err := connect(t).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if len(res.Tools) == 0 {
		t.Fatal("tools/list returned no tools")
	}
	return res.Tools
}

// TestToolSchemasMatchGolden is the drift gate. Any change to a tool's name,
// description or schema shows up as a diff in docs/mcp-schema.json, in the pull
// request, where a person sees it - which is exactly the review surface that was
// missing when read_save_data's schema changed shape and took out every tool.
//
// Stable without any sorting here: the SDK keeps its tool set ordered by name, and
// encoding/json sorts map keys.
func TestToolSchemasMatchGolden(t *testing.T) {
	got, err := json.MarshalIndent(listTools(t), "", "  ")
	if err != nil {
		t.Fatalf("marshaling the tool list: %v", err)
	}
	got = append(got, '\n')

	if *updateGolden {
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("writing %s: %v", goldenPath, err)
		}
		t.Logf("wrote %s", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("reading %s: %v\n\nrun `make mcp-schema` to create it", goldenPath, err)
	}
	if string(got) != string(want) {
		t.Errorf("the tool surface a client sees no longer matches %s.\n\n"+
			"If the change is intended, run `make mcp-schema` and commit the result - the diff is "+
			"the point, since a schema change is a change to what every client is served.\n\n"+
			"Do not hand-edit the file; it is generated by this test.\n\ngot:\n%s\n\nwant:\n%s",
			goldenPath, got, want)
	}
}

// TestEveryToolHasADescription - a tool with no description is one the model has to
// guess at from its name, which is the difference between an agent using a tool
// correctly and never reaching for it.
func TestEveryToolHasADescription(t *testing.T) {
	for _, tool := range listTools(t) {
		if tool.Description == "" {
			t.Errorf("tool %q has no description", tool.Name)
		}
	}
}

// TestInputSchemasAreObjects - the spec says a tool's inputSchema is an object schema,
// and a client that trusts that will index into properties without checking. A tool
// taking no arguments still has to say `type: object`, not omit the type.
func TestInputSchemasAreObjects(t *testing.T) {
	for _, tool := range listTools(t) {
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Errorf("tool %q has an inputSchema of type %T, want a JSON object", tool.Name, tool.InputSchema)
			continue
		}
		if schema["type"] != "object" {
			t.Errorf("tool %q has inputSchema type %v, want \"object\"", tool.Name, schema["type"])
		}
	}
}

// TestNoBareBooleanSubschemas is the regression test for the outage.
//
// `true` is a legal JSON Schema meaning "anything validates", and jsonschema-go emits
// it for an all-empty schema - which is what an `any`-typed Go field infers to. It is
// spec-compliant and at least one real client's validator rejects it outright, and
// because a client validates the whole tools/list response as a unit, one such
// property made every tool in this server unusable.
//
// So the rule is stricter than the spec on purpose: no schema slot anywhere in any
// tool's input or output schema may be a bare boolean. The fix at the time was an
// explicit OutputSchema with a real Description, and this is what keeps it from
// regressing - including in a tool nobody has written yet.
func TestNoBareBooleanSubschemas(t *testing.T) {
	for _, tool := range listTools(t) {
		for _, named := range namedSchemas(tool) {
			for _, where := range findBareBooleans(named.schema, named.label) {
				t.Errorf("tool %q has a bare boolean schema at %s.\n"+
					"It is spec-legal and a real client rejected it, failing the entire tools/list "+
					"fetch and making every tool unusable. Give the field an explicit schema with "+
					"real content - see readSaveDataOutputSchema in internal/tools/server.go.",
					tool.Name, where)
			}
		}
	}
}

// TestNoNullableInputTypeUnions - a `["null", "boolean"]` type is what a *bool field
// infers to, and internal/tools already turned one down for that reason: crank_dock is
// a three-valued string rather than a *bool, partly because of the union a pointer
// would have put in the schema. This makes that a rule instead of a habit someone has
// to remember, for every input field added later.
//
// Input schemas only, and the exemption is measured rather than assumed. Every Go slice
// infers to ["null", "array"], because a nil slice marshals to null - so nine of this
// server's output schemas carry a nullable union today, and those same schemas have
// been served to real clients through many sessions without trouble. A rule that
// flagged them would be asserting something known to be false. What is worth pinning
// is the input side: an argument schema is what a model reads to decide how to call a
// tool, and a union there is ambiguity in the one place ambiguity costs most.
func TestNoNullableInputTypeUnions(t *testing.T) {
	for _, tool := range listTools(t) {
		for _, where := range findNullableUnions(tool.InputSchema, "inputSchema") {
			t.Errorf("tool %q has a nullable type union at %s.\n"+
				"A pointer field infers to [\"null\", T]. Use a named value or a plain type "+
				"instead - see SetCrankInput.CrankDock in internal/tools/input.go.",
				tool.Name, where)
		}
	}
}

// TestClosedSetsDeclareEnums - a field that accepts a fixed set of values has to say so
// as a JSON Schema `enum`, not only in its description.
//
// Found by running Specmatic's MCP auto-test against this server, which is the whole
// reason that layer exists: it generated `language: "MIRMU"` for teardown and a random
// string for every button, because a random string is exactly what a schema saying
// `type: string` asks for. A description is prose - a model reads it, a validator cannot.
//
// With the enum declared, a client rejects press_button("A") before it is sent, and the
// message it produces names the alternatives itself. The handler-side checks in
// internal/tools stay regardless; this is about what the schema promises.
//
// The lists come from the same variables the server registers from, so this cannot pass
// by agreeing with a stale copy - it fails if an enum goes missing, not if a value is
// added.
func TestClosedSetsDeclareEnums(t *testing.T) {
	want := map[string]map[string][]string{
		"press_button":   {"button": harness.ButtonNames},
		"hold_button":    {"button": harness.ButtonNames},
		"release_button": {"button": harness.ButtonNames},
		"set_crank":      {"crank_dock": harness.DockModes},
		"setup":          {"language": crankSetup.LanguageNames()},
		"teardown":       {"language": crankSetup.LanguageNames()},
	}

	byName := map[string]*mcp.Tool{}
	for _, tool := range listTools(t) {
		byName[tool.Name] = tool
	}

	for toolName, fields := range want {
		tool, ok := byName[toolName]
		if !ok {
			t.Errorf("tool %q is not registered", toolName)
			continue
		}
		schema, _ := tool.InputSchema.(map[string]any)
		props, _ := schema["properties"].(map[string]any)
		for field, values := range fields {
			prop, ok := props[field].(map[string]any)
			if !ok {
				t.Errorf("tool %q has no %q property", toolName, field)
				continue
			}
			declared, ok := prop["enum"].([]any)
			if !ok {
				t.Errorf("tool %q's %q accepts a fixed set of values but declares no enum.\n"+
					"Naming them in the description only tells a model; an enum also tells "+
					"every client and every input generator. See closedSetSchema in "+
					"internal/tools/server.go.", toolName, field)
				continue
			}
			got := make([]string, 0, len(declared))
			for _, v := range declared {
				s, _ := v.(string)
				got = append(got, s)
			}
			if !slices.Equal(got, values) {
				t.Errorf("tool %q's %q declares enum %v, want %v", toolName, field, got, values)
			}
		}
	}
}

// TestSchemasResolve - every schema a client is handed has to be one the SDK's own
// validator can load. Inference through AddTool would panic on a schema it could not
// build, but a hand-supplied OutputSchema bypasses that, and readSaveDataOutputSchema
// is exactly such a schema. This round-trips each one through the wire shape a client
// receives and back into jsonschema, which is the only way to check the thing the
// client got rather than the thing the server meant.
func TestSchemasResolve(t *testing.T) {
	for _, tool := range listTools(t) {
		for _, named := range namedSchemas(tool) {
			raw, err := json.Marshal(named.schema)
			if err != nil {
				t.Errorf("tool %q: marshaling %s: %v", tool.Name, named.label, err)
				continue
			}
			var schema jsonschema.Schema
			if err := json.Unmarshal(raw, &schema); err != nil {
				t.Errorf("tool %q: %s is not a schema the SDK can parse: %v", tool.Name, named.label, err)
				continue
			}
			if _, err := schema.Resolve(nil); err != nil {
				t.Errorf("tool %q: %s does not resolve: %v", tool.Name, named.label, err)
			}
		}
	}
}

// namedSchema pairs a schema with a human-readable path into it, so a failure names
// the property rather than making someone diff the whole tool.
type namedSchema struct {
	label  string
	schema any
}

func namedSchemas(tool *mcp.Tool) []namedSchema {
	out := []namedSchema{{label: "inputSchema", schema: tool.InputSchema}}
	if tool.OutputSchema != nil {
		out = append(out, namedSchema{label: "outputSchema", schema: tool.OutputSchema})
	}
	return out
}

// The keyword groups below are how a schema slot is recognised. Walking every boolean
// in the tree would flag `"visible": {"type": "boolean"}`, which is a value and not a
// schema at all, so the walk has to know which positions hold a schema.
var (
	// Keywords whose value is one schema.
	singleSchemaKeywords = []string{
		"items", "not", "if", "then", "else", "propertyNames", "contains",
		"unevaluatedItems", "unevaluatedProperties", "additionalProperties",
	}
	// Keywords whose value is a map of name to schema.
	schemaMapKeywords = []string{
		"properties", "patternProperties", "$defs", "definitions", "dependentSchemas",
	}
	// Keywords whose value is a list of schemas.
	schemaListKeywords = []string{"allOf", "anyOf", "oneOf", "prefixItems"}

	// Slots where a bare boolean is the idiomatic spelling rather than a smell.
	// `additionalProperties: false` is how every struct schema jsonschema-go emits
	// says "no extra properties", and every client understands it - measured, not
	// assumed: this server serves it on all nineteen tools today. The two
	// unevaluated* keywords are the same idiom. Flagging them would have made this
	// rule fire 20-odd times on known-good schemas, which is how a rule gets
	// deleted instead of obeyed.
	booleanIdiomaticKeywords = []string{
		"additionalProperties", "unevaluatedProperties", "unevaluatedItems",
	}
)

// walkSchemas calls visit for the root schema and every subschema below it, with a path
// describing where it was found and whether a bare boolean is conventional there.
func walkSchemas(schema any, path string, booleanOK bool, visit func(schema any, path string, booleanOK bool)) {
	visit(schema, path, booleanOK)

	obj, ok := schema.(map[string]any)
	if !ok {
		return
	}
	for _, kw := range singleSchemaKeywords {
		if sub, present := obj[kw]; present {
			walkSchemas(sub, path+"."+kw, slices.Contains(booleanIdiomaticKeywords, kw), visit)
		}
	}
	for _, kw := range schemaMapKeywords {
		subs, present := obj[kw].(map[string]any)
		if !present {
			continue
		}
		// Sorted so a tool with several offending properties reports them in a
		// stable order rather than whatever the map yields this run.
		names := make([]string, 0, len(subs))
		for name := range subs {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			walkSchemas(subs[name], fmt.Sprintf("%s.%s.%s", path, kw, name), false, visit)
		}
	}
	for _, kw := range schemaListKeywords {
		subs, present := obj[kw].([]any)
		if !present {
			continue
		}
		for i, sub := range subs {
			walkSchemas(sub, fmt.Sprintf("%s.%s[%d]", path, kw, i), false, visit)
		}
	}
}

func findBareBooleans(schema any, path string) []string {
	var found []string
	walkSchemas(schema, path, false, func(sub any, at string, booleanOK bool) {
		if _, isBool := sub.(bool); isBool && !booleanOK {
			found = append(found, at)
		}
	})
	return found
}

func findNullableUnions(schema any, path string) []string {
	var found []string
	walkSchemas(schema, path, false, func(sub any, at string, _ bool) {
		obj, ok := sub.(map[string]any)
		if !ok {
			return
		}
		types, ok := obj["type"].([]any)
		if !ok {
			return
		}
		if slices.Contains(types, any("null")) {
			found = append(found, at)
		}
	})
	return found
}
