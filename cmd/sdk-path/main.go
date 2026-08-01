// Command sdk-path prints the Playdate SDK that internal/sdk resolves, and which
// of its sources found it.
//
// SDK detection is otherwise invisible: it happens once at startup and only
// shows up as tool behaviour. When it picks the wrong SDK, or none, this is the
// shortest path to seeing why, without needing to start a server.
//
// The rendering lives in sdk.Paths.Describe so it can be tested; this is the
// shell around it.
package main

import (
	"fmt"
	"os"

	"github.com/NickSpaghetti/open-crank-mcp/internal/sdk"
)

func main() {
	paths, err := sdk.Resolve(sdk.OSEnv())
	if err != nil {
		fmt.Fprintf(os.Stderr, "no SDK found: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(paths.Describe())
}
