// Command sdk-path prints the Playdate SDK that internal/sdk resolves, and which
// of its sources found it, for debugging SDK detection without a server.
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
