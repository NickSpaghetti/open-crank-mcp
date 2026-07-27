package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/NickSpaghetti/open-crank-mcp/internal/screenshot"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetScreenshotInput struct{}

func (s *Server) getScreenshot(_ context.Context, _ *mcp.CallToolRequest, _ GetScreenshotInput) (*mcp.CallToolResult, any, error) {
	resp, err := s.roundTrip(map[string]any{"type": "screenshot"})
	if err != nil {
		result, wrapErr := handleRoundTripErr(err)
		return result, nil, wrapErr
	}

	format := asString(resp["format"])
	relPath := asString(resp["path"])

	dataDir, err := s.requireDataDir()
	if err != nil {
		result, wrapErr := handleRoundTripErr(err)
		return result, nil, wrapErr
	}
	fullPath := filepath.Join(dataDir, relPath)

	var pngBytes []byte
	switch format {
	case "raw":
		raw, err := os.ReadFile(fullPath)
		if err != nil {
			return nil, nil, fmt.Errorf("reading raw screenshot: %w", err)
		}
		pngBytes, err = screenshot.DecodeRawToPNG(raw)
		if err != nil {
			return nil, nil, fmt.Errorf("decoding screenshot: %w", err)
		}
	case "png":
		pngBytes, err = os.ReadFile(fullPath)
		if err != nil {
			return nil, nil, fmt.Errorf("reading png screenshot: %w", err)
		}
	default:
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "screenshot response had neither raw nor png format"}},
		}, nil, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.ImageContent{Data: pngBytes, MIMEType: "image/png"}},
	}, nil, nil
}
