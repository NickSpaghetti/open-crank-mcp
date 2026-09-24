package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NickSpaghetti/open-crank-mcp/internal/harness"
	"github.com/NickSpaghetti/open-crank-mcp/internal/screenshot"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetScreenshotInput struct{}

func (s *Server) getScreenshot(_ context.Context, _ *mcp.CallToolRequest, _ GetScreenshotInput) (*mcp.CallToolResult, any, error) {
	// Held across the round trip AND the file read below: the screenshot
	// path (mcp/screenshot.png|raw) is fixed, not per-request, so a second
	// concurrent get_screenshot call could otherwise overwrite it between
	// this call's round trip returning and its own os.ReadFile.
	s.harnessMu.Lock()
	defer s.harnessMu.Unlock()

	resp, err := s.roundTripLocked(harness.Command{Type: harness.CmdScreenshot})
	if err != nil {
		// The round-trip error names response.json in the data directory, which
		// says nothing about the scratch directory the PNG is written to. A
		// screenshot that never comes back is usually about the latter.
		result, wrapErr := handleRoundTripErr(fmt.Errorf("%w%s", err, s.scratchReport()))
		return result, nil, wrapErr
	}

	// The two harnesses write to different places, so the base differs by format.
	//
	// C writes through pd->file->*, which is sandboxed: its path is relative to
	// the Simulator's data directory. Lua writes through
	// playdate.simulator.writeToFile, which takes a path on the dev machine, so
	// it writes into the scratch directory this process created and handed over
	// as playdate.argv[2].
	//
	// On Linux these used to be the same directory, which is why one base worked
	// for both and why the difference stayed invisible.
	base, err := s.screenshotBase(resp.Format)
	if err != nil {
		result, wrapErr := handleRoundTripErr(err)
		return result, nil, wrapErr
	}
	fullPath := filepath.Join(base, resp.Path)

	var pngBytes []byte
	switch resp.Format {
	case harness.FormatRaw:
		raw, err := os.ReadFile(fullPath)
		if err != nil {
			return nil, nil, fmt.Errorf("reading raw screenshot: %w", err)
		}
		pngBytes, err = screenshot.DecodeRawToPNG(raw)
		if err != nil {
			return nil, nil, fmt.Errorf("decoding screenshot: %w", err)
		}
	case harness.FormatPNG:
		pngBytes, err = os.ReadFile(fullPath)
		if err != nil {
			return nil, nil, fmt.Errorf("reading png screenshot: %w", err)
		}
	default:
		return errorResult(fmt.Sprintf(
			"screenshot response had format %q, want %q or %q",
			resp.Format, harness.FormatRaw, harness.FormatPNG)), nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.ImageContent{Data: pngBytes, MIMEType: "image/png"}},
	}, nil, nil
}

// screenshotBase is the directory a screenshot of the given format is written
// into. See the comment at its call site for why they differ.
func (s *Server) screenshotBase(format string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sim == nil {
		return "", errNotRunning
	}
	if format == harness.FormatPNG {
		// Without this an empty scratchDir joins into a bare relative path, and the
		// failure reads as a missing file rather than as missing state.
		if s.scratchDir == "" {
			return "", errNoScratch
		}
		return s.scratchDir, nil
	}
	return s.dataDir, nil
}

// scratchReport describes the directory handed to the game as playdate.argv[2]
// and what is currently in it.
//
// Written for the case where get_screenshot times out after a restart: the
// scratch directory is the only thing restart changes, and whether the game
// wrote anything into it separates "never got the command" from "could not
// write the file".
func (s *Server) scratchReport() string {
	s.mu.Lock()
	scratch := s.scratchDir
	s.mu.Unlock()

	if scratch == "" {
		return " (no scratch directory recorded, so the PNG had nowhere to go)"
	}
	dir := filepath.Join(scratch, luaScreenshotRelDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Sprintf(" (scratch %s: %s is unreadable: %v)", scratch, luaScreenshotRelDir, err)
	}
	if len(entries) == 0 {
		return fmt.Sprintf(" (scratch %s: %s exists and is empty)", scratch, luaScreenshotRelDir)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return fmt.Sprintf(" (scratch %s: %s holds %s)", scratch, luaScreenshotRelDir, strings.Join(names, ", "))
}
