//go:build windows && native

package contracttest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	opencrank "github.com/NickSpaghetti/open-crank-mcp"
	"github.com/NickSpaghetti/open-crank-mcp/internal/build"
	"github.com/NickSpaghetti/open-crank-mcp/internal/sdk"
	"github.com/NickSpaghetti/open-crank-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestWindowsNativeMCP drives both fixture languages through the actual MCP
// registrations, Windows SDK binaries, CMake, Simulator, and file-based harness.
// Run explicitly with `go test -tags=native ./internal/contracttest` on Windows.
func TestWindowsNativeMCP(t *testing.T) {
	paths, err := sdk.Resolve(sdk.OSEnv())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	server := mcp.NewServer(&mcp.Implementation{Name: "open-crank-mcp-windows-contract", Version: "test"}, nil)
	tools.RegisterAll(server, tools.NewServer(paths, nil, opencrank.HarnessFS))
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connecting MCP server: %v", err)
	}
	client, err := mcp.NewClient(&mcp.Implementation{Name: "windows-native-contract", Version: "test"}, nil).
		Connect(ctx, clientTransport, nil)
	if err != nil {
		_ = serverSession.Close()
		t.Fatalf("connecting MCP client: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Close()
		_ = serverSession.Wait()
	})

	repoRoot := findRepoRoot(t)
	t.Run("Lua", func(t *testing.T) {
		project := copyNativeFixture(t, filepath.Join(repoRoot, "lua", "test-fixture"), "lua")
		copyNativeFile(t, filepath.Join(repoRoot, "lua", "mcp_harness.lua"), filepath.Join(project, "Source", "mcp_harness.lua"))
		runWindowsNativeGame(t, ctx, client, paths, project, "Lua")
	})
	t.Run("C", func(t *testing.T) {
		project := copyNativeFixture(t, filepath.Join(repoRoot, "c-harness", "test", "fixture-game"), "c")
		copyNativeFile(t, filepath.Join(repoRoot, "c-harness", "mcp_harness.c"), filepath.Join(project, "src", "mcp_harness.c"))
		copyNativeFile(t, filepath.Join(repoRoot, "c-harness", "mcp_harness.h"), filepath.Join(project, "src", "mcp_harness.h"))
		runWindowsNativeGame(t, ctx, client, paths, project, "C")
	})
}

// Deliberately not a t.Helper: this drives a dozen tool calls, and the line
// number of the one that failed is most of the diagnosis.
func runWindowsNativeGame(t *testing.T, ctx context.Context, client *mcp.ClientSession, paths sdk.Paths, project, wantType string) {
	buildResult := callWindowsTool(t, ctx, client, "build_game", map[string]any{"source_dir": project})
	buildOutput := windowsStructured(t, buildResult)
	if got := buildOutput["project_type"]; got != wantType {
		t.Fatalf("build_game project_type = %v, want %q", got, wantType)
	}
	pdxPath, ok := buildOutput["pdx_path"].(string)
	if !ok || pdxPath == "" {
		t.Fatalf("build_game returned invalid pdx_path: %v", buildOutput["pdx_path"])
	}
	bundleID, err := build.ReadBundleID(pdxPath)
	if err != nil {
		t.Fatalf("reading fixture bundle ID: %v", err)
	}
	dataDirs := paths.DataDirCandidates(sdk.OSEnv(), bundleID)
	if len(dataDirs) == 0 {
		t.Fatal("SDK layout produced no data-directory candidates")
	}
	dataDir := dataDirs[0]
	if err := os.RemoveAll(dataDir); err != nil {
		t.Fatalf("clearing fixture data dir: %v", err)
	}

	launch := callWindowsTool(t, ctx, client, "launch_simulator", map[string]any{"pdx_path": pdxPath})
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			_, _ = client.CallTool(context.Background(), &mcp.CallToolParams{Name: "stop_simulator"})
		}
		_ = os.RemoveAll(dataDir)
	})
	if got := windowsStructured(t, launch)["bundle_id"]; got != bundleID {
		t.Fatalf("launch_simulator bundle_id = %v, want %q", got, bundleID)
	}

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		status := windowsStructured(t, callWindowsTool(t, ctx, client, "get_status", map[string]any{}))
		if status["harness_reachable"] == true {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	status := windowsStructured(t, callWindowsTool(t, ctx, client, "get_status", map[string]any{}))
	if status["harness_reachable"] != true {
		t.Fatalf("harness did not become reachable: %v", status)
	}

	callWindowsTool(t, ctx, client, "set_crank", map[string]any{
		"crank_angle": 123.0, "crank_delta": 5.0, "crank_dock": "undocked", "duration_ms": 10000,
	})
	state := windowsStructured(t, callWindowsTool(t, ctx, client, "get_game_state", map[string]any{}))
	if angle, ok := state["crank_angle"].(float64); !ok || angle != 123 {
		t.Fatalf("set_crank did not reach the game: state crank_angle = %v", state["crank_angle"])
	}

	callWindowsTool(t, ctx, client, "press_button", map[string]any{"button": "a", "duration_ms": 100})
	time.Sleep(300 * time.Millisecond)
	state = windowsStructured(t, callWindowsTool(t, ctx, client, "get_game_state", map[string]any{}))
	if count, ok := state["a_down_count"].(float64); !ok || count < 1 {
		t.Fatalf("press_button did not reach the game: state a_down_count = %v", state["a_down_count"])
	}

	screenshot := callWindowsTool(t, ctx, client, "get_screenshot", map[string]any{})
	foundPNG := false
	for _, content := range screenshot.Content {
		if image, ok := content.(*mcp.ImageContent); ok && image.MIMEType == "image/png" && len(image.Data) != 0 {
			foundPNG = true
		}
	}
	if !foundPNG {
		t.Fatal("get_screenshot returned no PNG image content")
	}

	callWindowsTool(t, ctx, client, "restart_simulator", map[string]any{})
	waitForLiveHarness(t, ctx, client, "after restart")
	screenshot = callWindowsTool(t, ctx, client, "get_screenshot", map[string]any{})
	foundPNG = false
	for _, content := range screenshot.Content {
		if image, ok := content.(*mcp.ImageContent); ok && image.MIMEType == "image/png" && len(image.Data) != 0 {
			foundPNG = true
		}
	}
	if !foundPNG {
		t.Fatal("get_screenshot returned no PNG after restart")
	}

	callWindowsTool(t, ctx, client, "stop_simulator", map[string]any{})
	stopped = true
	status = windowsStructured(t, callWindowsTool(t, ctx, client, "get_status", map[string]any{}))
	if status["running"] != false {
		t.Fatalf("Simulator still reports running after stop: %v", status)
	}
}

func callWindowsTool(t *testing.T, ctx context.Context, client *mcp.ClientSession, name string, args any) *mcp.CallToolResult {
	t.Helper()
	result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("MCP call %s: %v\nstate at failure:%s", name, err, windowsDiagnostics(ctx, client))
	}
	if result.IsError {
		var messages []string
		for _, content := range result.Content {
			if text, ok := content.(*mcp.TextContent); ok {
				messages = append(messages, text.Text)
			}
		}
		t.Fatalf("MCP tool %s failed: %s\nstate at failure:%s",
			name, strings.Join(messages, "\n"), windowsDiagnostics(ctx, client))
	}
	return result
}

// windowsDiagnostics reports what a failing tool call cannot say for itself:
// whether the Simulator is still running, and what it printed on the way down.
//
// A harness timeout is the motivating case. "timed out waiting for
// response.json" is true of a Simulator that crashed, one that is wedged, and
// one that never got the command, and those want different fixes.
//
// get_logs comes before get_game_logs on purpose. It reads the Simulator's own
// captured output from this process, so it still answers when the harness is
// gone, which is exactly when this runs. get_game_logs needs a round trip and
// will spend its timeout if the harness is dead, so it goes last.
//
// Best-effort throughout: this runs on a path where something already failed,
// and a diagnostic that fails the test itself would bury the real error.
func windowsDiagnostics(ctx context.Context, client *mcp.ClientSession) string {
	var b strings.Builder
	report := func(name string, args map[string]any) {
		short, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		result, err := client.CallTool(short, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			fmt.Fprintf(&b, "\n  %s: unavailable: %v", name, err)
			return
		}
		if result.StructuredContent != nil {
			fmt.Fprintf(&b, "\n  %s: %s", name, clampDiagnostic(fmt.Sprint(result.StructuredContent)))
			return
		}
		var messages []string
		for _, content := range result.Content {
			if text, ok := content.(*mcp.TextContent); ok {
				messages = append(messages, text.Text)
			}
		}
		fmt.Fprintf(&b, "\n  %s: %s", name, clampDiagnostic(strings.Join(messages, " ")))
	}
	report("get_status", map[string]any{})
	report("get_logs", map[string]any{"tail_n": 40})
	report("get_game_logs", map[string]any{"tail_n": 20})
	return b.String()
}

func windowsStructured(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()
	value, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("tool result structured content has type %T: %v", result.StructuredContent, result.StructuredContent)
	}
	return value
}

func copyNativeFixture(t *testing.T, source, language string) string {
	t.Helper()
	destination := filepath.Join(t.TempDir(), language+"-fixture")
	if err := copyNativeTree(source, destination); err != nil {
		t.Fatalf("copying %s fixture: %v", language, err)
	}
	pdxinfo := filepath.Join(destination, "Source", "pdxinfo")
	b, err := os.ReadFile(pdxinfo)
	if err != nil {
		t.Fatalf("reading %s pdxinfo: %v", language, err)
	}
	contents := strings.Replace(string(b), "bundleID=dev.open-crank-mcp.contractcheck", "bundleID=dev.open-crank-mcp.native."+language+fmt.Sprint(time.Now().UnixNano()), 1)
	if err := os.WriteFile(pdxinfo, []byte(contents), 0o644); err != nil {
		t.Fatalf("writing %s pdxinfo: %v", language, err)
	}
	return destination
}

func copyNativeTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		out := filepath.Join(destination, rel)
		if entry.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		return os.WriteFile(out, b, 0o644)
	})
}

func copyNativeFile(t *testing.T, source, destination string) {
	t.Helper()
	b, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("reading %s: %v", source, err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatalf("creating directory for %s: %v", destination, err)
	}
	if err := os.WriteFile(destination, b, 0o644); err != nil {
		t.Fatalf("writing %s: %v", destination, err)
	}
}

// clampDiagnostic bounds one diagnostic line. A wedged game can hold thousands
// of log entries, and burying the real failure under them defeats the purpose.
func clampDiagnostic(s string) string {
	const max = 2000
	if len(s) <= max {
		return s
	}
	return s[:max] + " ... (truncated)"
}

// waitForLiveHarness blocks until the game answers a real round trip.
//
// get_status cannot do this job. harness_reachable is a stat on the data
// directory, and the run before the restart already created it, so after a
// restart it reports true before the new game has started its update loop. It
// gates correctly after a launch only because the test deletes that directory
// first.
//
// Before this existed, the post-restart get_screenshot was the first call that
// needed a live game, so it absorbed the whole relaunch and timed out at five
// seconds. That reads as a broken screenshot rather than a test that did not
// wait, which is what it was.
func waitForLiveHarness(t *testing.T, ctx context.Context, client *mcp.ClientSession, stage string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "get_game_state", Arguments: map[string]any{}})
		switch {
		case err != nil:
			last = err.Error()
		case result.IsError:
			last = fmt.Sprint(result.Content)
		default:
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("harness never answered a round trip %s, last error: %s\nstate at failure:%s",
		stage, last, windowsDiagnostics(ctx, client))
}
