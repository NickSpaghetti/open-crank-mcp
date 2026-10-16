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
	"github.com/NickSpaghetti/open-crank-mcp/internal/httpserver"
	"github.com/NickSpaghetti/open-crank-mcp/internal/sdk"
	"github.com/NickSpaghetti/open-crank-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")

	runDoctor := flag.Bool("doctor", false,
		"check the environment and print a report, then exit")
	// OpenCode's v1 plugin API has no config or MCP hook, so users paste this block.
	printConfig := flag.String("print-config", "",
		"print the MCP config block for a client ("+strings.Join(clientconfig.Clients(), ", ")+
			") and exit")
	doctorLaunch := flag.Bool("doctor-launch", false,
		"with -doctor, also start the Simulator to see whether it stays up. "+
			"Off by default because it puts a window on your desktop.")

	httpAddr := flag.String("http", "", "serve over Streamable HTTP on this loopback address "+
		"(e.g. 127.0.0.1:8237) instead of stdio. For contract testing; MCP clients use stdio.")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	if *doctorLaunch && !*runDoctor {
		fmt.Fprintln(os.Stderr, "open-crank-mcp: -doctor-launch does nothing without -doctor")
		os.Exit(1)
	}

	if *printConfig != "" {
		if err := emitClientConfig(*printConfig); err != nil {
			fmt.Fprintf(os.Stderr, "open-crank-mcp: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if *runDoctor {
		// Exits 0 even on problems: no SDK is the normal state right after install.
		fmt.Print(doctor.Gather(
			sdk.OSEnv(), runtime.GOOS, version,
			doctor.Options{Launch: *doctorLaunch},
		))
		return
	}

	// A missing SDK is not fatal: clients show an early exit only as "server failed
	// to start". Tools that need the SDK report it themselves.
	paths, sdkErr := sdk.Resolve(sdk.OSEnv())
	if sdkErr != nil {
		fmt.Fprintf(os.Stderr, "open-crank-mcp: %v\n", sdkErr)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "open-crank-mcp", Version: version}, nil)
	tools.RegisterAll(server, tools.NewServer(paths, sdkErr, opencrank.HarnessFS))

	if *httpAddr != "" {
		if err := httpserver.CheckLoopback(*httpAddr); err != nil {
			fmt.Fprintf(os.Stderr, "open-crank-mcp: -http %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "open-crank-mcp: serving MCP over Streamable HTTP on http://%s\n", *httpAddr)
		if err := httpserver.Serve(context.Background(), server, *httpAddr); err != nil {
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

// emitClientConfig writes one client's MCP config block to stdout, naming this
// binary by its symlink-resolved absolute path so it needn't be on PATH.
func emitClientConfig(client string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating this binary: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	out, err := clientconfig.Render(client, []string{exe})
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
}
