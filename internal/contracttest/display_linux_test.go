//go:build linux

package contracttest

import (
	"os"
	"testing"
	"time"

	"github.com/NickSpaghetti/open-crank-mcp/internal/simulator"
)

// startDisplay brings up an Xvfb display on the given number and points DISPLAY
// at it, cleaning up when the test ends.
//
// The display number is a parameter because each contract test function is
// self-contained and uses its own, rather than relying on execution order for
// shared setup.
func startDisplay(t *testing.T, display string) {
	t.Helper()
	xvfb, err := simulator.Launch("Xvfb", display, "-screen", "0", "1280x800x24")
	if err != nil {
		t.Fatalf("launching Xvfb: %v", err)
	}
	t.Cleanup(func() {
		_ = xvfb.Stop()
		_ = xvfb.Wait()
	})
	time.Sleep(1 * time.Second)
	if err := os.Setenv("DISPLAY", display); err != nil {
		t.Fatalf("setting DISPLAY: %v", err)
	}
}
