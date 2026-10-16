package mcpcontract

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/NickSpaghetti/open-crank-mcp/internal/sdk"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// buildServer compiles cmd/open-crank-mcp into a temp directory and returns its path,
// so the test never runs a stale binary.
func buildServer(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("no go toolchain on PATH: %v", err)
	}

	bin := filepath.Join(t.TempDir(), "open-crank-mcp")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, "../../cmd/open-crank-mcp")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the server: %v\n%s", err, out)
	}
	return bin
}

// TestHandshakeSucceedsWithNoSDK runs the real binary over stdio with no way to find
// an SDK. Clients report an early exit only as "server failed to start", so the
// handshake must still complete.
func TestHandshakeSucceedsWithNoSDK(t *testing.T) {
	bin := buildServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.Command(bin)
	// Empty except HOME and PATH, so an installed SDK can't resolve.
	emptyHome := t.TempDir()
	cmd.Env = []string{
		"HOME=" + emptyHome,
		"USERPROFILE=" + emptyHome, // windows' equivalent, for the cross-compiled case
		"PATH=" + os.Getenv("PATH"),
	}
	if runtime.GOOS == "windows" {
		// Windows child processes need their system and temp roots to start.
		for _, key := range []string{"SYSTEMROOT", "WINDIR", "TEMP", "TMP", "COMSPEC"} {
			if value := os.Getenv(key); value != "" {
				cmd.Env = append(cmd.Env, key+"="+value)
			}
		}
	}
	cmd.Stderr = os.Stderr

	client := mcp.NewClient(&mcp.Implementation{Name: "mcpcontract", Version: "v0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("the server did not complete the MCP handshake with no SDK available: %v\n\n"+
			"Resolution failing must not be fatal - a client reports a process that exits "+
			"before the handshake as \"server failed to start\", which names nothing. See "+
			"cmd/open-crank-mcp/main.go.", err)
	}
	defer func() { _ = session.Close() }()

	res, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list against the real binary: %v", err)
	}
	if len(res.Tools) == 0 {
		t.Fatal("the real binary served an empty tool list")
	}
}

// TestSDKlessToolsExplainThemselves - without an SDK, a tool's error must name the
// environment variable the user can set.
func TestSDKlessToolsExplainThemselves(t *testing.T) {
	res := callTool(t, connect(t), "build_game", map[string]any{"source_dir": "/tmp/nonexistent"})
	if !res.IsError {
		t.Fatal("build_game succeeded with no SDK resolved")
	}
	if msg := resultText(res); !strings.Contains(msg, sdk.EnvVarSDKPath) {
		t.Errorf("build_game with no SDK said %q, want %s named as the remedy",
			msg, sdk.EnvVarSDKPath)
	}
}
