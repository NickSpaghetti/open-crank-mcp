package contracttest

import (
	"testing"

	"github.com/NickSpaghetti/open-crank-mcp/internal/sdk"
)

// contractSDK resolves the SDK the way the real server does, rather than each
// test rebuilding paths from PLAYDATE_SDK_PATH by hand.
//
// Worth doing here specifically: these are the only tests that run against a real
// SDK, so they are the only place resolution gets exercised end to end rather
// than against a synthetic filesystem. If internal/sdk ever stops finding a real
// install, this is where it should show up.
func contractSDK(t *testing.T) sdk.Paths {
	t.Helper()
	paths, err := sdk.Resolve(sdk.OSEnv())
	if err != nil {
		t.Fatalf("resolving SDK: %v", err)
	}
	return paths
}
