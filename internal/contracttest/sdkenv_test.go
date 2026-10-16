package contracttest

import (
	"testing"

	"github.com/NickSpaghetti/open-crank-mcp/internal/sdk"
)

// contractSDK resolves the SDK the way the real server does, which also exercises
// internal/sdk against a real install.
func contractSDK(t *testing.T) sdk.Paths {
	t.Helper()
	paths, err := sdk.Resolve(sdk.OSEnv())
	if err != nil {
		t.Fatalf("resolving SDK: %v", err)
	}
	return paths
}
