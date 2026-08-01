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

	dataDir, err := s.requireDataDir()
	if err != nil {
		result, wrapErr := handleRoundTripErr(err)
		return result, nil, wrapErr
	}
	fullPath := filepath.Join(dataDir, resp.Path)

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
