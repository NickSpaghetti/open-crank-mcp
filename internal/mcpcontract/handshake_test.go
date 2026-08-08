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

// buildServer compiles cmd/open-crank-mcp into a temp directory and returns its path.
//
// Building rather than reusing whatever `make go-build` left in the tree, so this cannot
// pass against a stale binary from before the change under test - which is the failure
// mode that would make the whole test worthless.
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

// TestHandshakeSucceedsWithNoSDK is main.go's central claim, tested where it is made.
//
// SDK resolution failing is deliberately not fatal. It used to exit(1), which was
// harmless while the only way to run this was a container that always set
// PLAYDATE_SDK_PATH - and wrong the moment it ran natively, where an unset variable is
// the ordinary first-time case. A process that exits before the handshake is reported by
// a client as "server failed to start" and nothing else, with the message explaining
// what to do going to a stderr nobody reads.
//
// So: a real subprocess, over the real stdio transport a client uses, with the
// environment stripped of every way to find an SDK and HOME pointed at an empty
// directory so ~/.Playdate/config and the per-OS default cannot resolve either. The
// handshake has to complete and tools/list has to answer.
//
// Nothing else covers this. internal/mcpcontract's other tests build the server
// in-process and never run main; internal/tools tests handlers directly. This is the
// only test in the repo that starts the actual binary.
func TestHandshakeSucceedsWithNoSDK(t *testing.T) {
	bin := buildServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.Command(bin)
	// A deliberately empty environment except for HOME and PATH. Inheriting the
	// developer's environment would let a real installed SDK resolve, and the test
	// would then prove the opposite of what it says - that startup works *with* an
	// SDK, which every other path already shows.
	emptyHome := t.TempDir()
	cmd.Env = []string{
		"HOME=" + emptyHome,
		"USERPROFILE=" + emptyHome, // windows' equivalent, for the cross-compiled case
		"PATH=" + os.Getenv("PATH"),
	}
	// Not silenced: main writes the resolution failure to stderr, and a test that
	// discarded it would hide a crash loop behind a timeout.
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

// TestSDKlessToolsExplainThemselves - the point of staying up without an SDK is that a
// tool can say what is wrong, so this checks the message is actually actionable rather
// than just non-fatal. It has to name the environment variable, because on a machine
// where detection found nothing that is the one thing the user can do about it.
//
// In-process rather than through the subprocess: the message comes from
// tools.Server.requireSDK, and injecting the failure is both faster and not dependent on
// the test machine genuinely lacking an SDK.
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
