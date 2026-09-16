package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	opencrank "github.com/NickSpaghetti/open-crank-mcp"
	"github.com/NickSpaghetti/open-crank-mcp/internal/clientconfig"
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
	// OpenCode's v1 plugin API has no config or MCP hook, so its config is
	// written by hand. Printing the exact block is the next best thing to
	// installing it. Claude Code and Cursor are rendered too, since the shapes
	// are data and the guides then have one source for all three.
	printConfig := flag.String("print-config", "",
		"print the MCP config block for a client ("+strings.Join(clientconfig.Clients(), ", ")+
			") and exit")
	// A wrong guess about how the user got this binary would be silent, and the
	// override costs one flag.
	configLauncher := flag.Bool("print-config-launcher", false,
		"with -print-config, always emit the plugin launcher's path")
	configBinary := flag.Bool("print-config-binary", false,
		"with -print-config, always emit this binary's own path")

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

	if *printConfig != "" {
		if err := emitClientConfig(*printConfig, *configLauncher, *configBinary); err != nil {
			fmt.Fprintf(os.Stderr, "open-crank-mcp: %v\n", err)
			os.Exit(1)
		}
		return
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

// emitClientConfig writes one client's MCP config block to stdout.
//
// The shell around internal/clientconfig, which holds the rendering and the
// detection so both can be tested. os.Executable is resolved through
// EvalSymlinks because a cached binary may be reached through a link, and a
// config naming the link would break the moment it was replaced.
func emitClientConfig(client string, forceLauncher, forceBinary bool) error {
	if forceLauncher && forceBinary {
		return fmt.Errorf("-print-config-launcher and -print-config-binary contradict each other")
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating this binary: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	inv := clientconfig.Detect(sdk.OSEnv(), exe)
	switch {
	case forceBinary:
		inv = clientconfig.Invocation{Argv: []string{exe}}
	case forceLauncher && !inv.Resolves:
		return fmt.Errorf("-print-config-launcher was given, but no plugin launcher was "+
			"found above %s", exe)
	}

	out, err := clientconfig.Render(client, inv)
	if err != nil {
		return err
	}
	// stdout: this is the output someone asked for, and nothing is serving the
	// MCP channel on this path.
	fmt.Print(out)
	return nil
}
