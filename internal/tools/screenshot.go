package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

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
		result, wrapErr := handleRoundTripErr(err)
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
		return s.scratchDir, nil
	}
	return s.dataDir, nil
}
