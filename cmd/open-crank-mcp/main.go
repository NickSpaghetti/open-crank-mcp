package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"

	opencrank "github.com/NickSpaghetti/open-crank-mcp"
	"github.com/NickSpaghetti/open-crank-mcp/internal/doctor"
	"github.com/NickSpaghetti/open-crank-mcp/internal/httpserve"
	"github.com/NickSpaghetti/open-crank-mcp/internal/sdk"
	"github.com/NickSpaghetti/open-crank-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// version is set at build time with -ldflags "-X main.version=...".
// A plain `go build` leaves it as "dev".
var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")

	// -doctor is the plugin user's equivalent of `make sdk-path` and
	// `make smoke-check-native`, neither of which they have: an editor plugin
	// ships a binary, not a checkout. The checks are internal/doctor's, shared
	// with cmd/smoke-check so there is one answer to "why is this not working".
	runDoctor := flag.Bool("doctor", false,
		"check the environment and print a report, then exit")
	doctorLaunch := flag.Bool("doctor-launch", false,
		"with -doctor, also start the Simulator to see whether it stays up. "+
			"Off by default because it puts a window on your desktop.")

	// -http is for contract testing. Specmatic's MCP auto-test only speaks
	// STREAMABLE_HTTP; real clients use stdio. Loopback addresses only, enforced in
	// internal/httpserve, since this server builds code and runs processes with no
	// authentication.
	httpAddr := flag.String("http", "", "serve over Streamable HTTP on this loopback address "+
		"(e.g. 127.0.0.1:8237) instead of stdio. For contract testing; MCP clients use stdio.")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	// Before SDK resolution, because resolution writes its failure to stderr on
	// the way past and the report is going to say the same thing properly.
	//
	// Exits 0 even when it finds problems. "No SDK found" is the ordinary state
	// on a machine that has just installed the plugin, not a failure, so severity
	// belongs in the text rather than in the exit status. A non-zero exit here
	// would also make the obvious `-doctor || echo broken` wrapper lie.
	if *doctorLaunch && !*runDoctor {
		fmt.Fprintln(os.Stderr, "open-crank-mcp: -doctor-launch does nothing without -doctor")
		os.Exit(1)
	}

	if *runDoctor {
		// stdout: this is the output someone asked for, not a diagnostic, and
		// nothing is serving the MCP channel on this path.
		fmt.Print(doctor.Gather(
			sdk.OSEnv(), runtime.GOOS, version,
			doctor.Options{Launch: *doctorLaunch},
		))
		return
	}

	// A missing SDK is not fatal. It is the normal first run, and exiting here would
	// reach the client as "server failed to start" with the real reason stuck on a
	// stderr nobody reads. Tools that need the SDK report it themselves, in a result
	// the agent can act on. See tools.Server.requireSDK.
	paths, sdkErr := sdk.Resolve(sdk.OSEnv())
	if sdkErr != nil {
		fmt.Fprintf(os.Stderr, "open-crank-mcp: %v\n", sdkErr)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "open-crank-mcp", Version: version}, nil)
	tools.RegisterAll(server, tools.NewServer(paths, sdkErr, opencrank.HarnessFS))

	if *httpAddr != "" {
		// Serve checks this too, but checking first avoids announcing an address we
		// are about to refuse.
		if err := httpserve.CheckLoopback(*httpAddr); err != nil {
			fmt.Fprintf(os.Stderr, "open-crank-mcp: -http %v\n", err)
			os.Exit(1)
		}
		// Unlike stdio, nothing else tells you the server came up. stderr to match the
		// stdio path, where stdout carries the protocol.
		fmt.Fprintf(os.Stderr, "open-crank-mcp: serving MCP over Streamable HTTP on http://%s\n", *httpAddr)
		if err := httpserve.Serve(context.Background(), server, *httpAddr); err != nil {
			log.Printf("http server failed: %v", err)
			os.Exit(1)
		}
		return
	}

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Printf("server failed: %v", err)
		os.Exit(1)
	}
}
