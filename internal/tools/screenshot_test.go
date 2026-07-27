package tools

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	screenshotpkg "github.com/NickSpaghetti/open-crank-mcp/internal/screenshot"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestGetScreenshotWhenNotRunning(t *testing.T) {
	s := &Server{}
	result, _, err := s.getScreenshot(context.Background(), nil, GetScreenshotInput{})
	if err != nil {
		t.Fatalf("getScreenshot: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("getScreenshot() result = %v, want an IsError result", result)
	}
}

func TestGetScreenshotRawFormat(t *testing.T) {
	s := newTestServer(t)
	raw := make([]byte, screenshotpkg.Height*screenshotpkg.RowBytes)
	if err := os.MkdirAll(filepath.Join(s.dataDir, "mcp"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(s.dataDir, "mcp", "screenshot.raw"), raw, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	startFakeHarness(t, s.dataDir, map[string]any{
		"status": "ok", "format": "raw", "path": "mcp/screenshot.raw",
	})

	result, _, err := s.getScreenshot(context.Background(), nil, GetScreenshotInput{})
	if err != nil {
		t.Fatalf("getScreenshot: %v", err)
	}
	assertPNGImageContent(t, result)
}

func TestGetScreenshotPNGFormat(t *testing.T) {
	s := newTestServer(t)
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(s.dataDir, "mcp"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(s.dataDir, "mcp", "screenshot.png"), buf.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	startFakeHarness(t, s.dataDir, map[string]any{
		"status": "ok", "format": "png", "path": "mcp/screenshot.png",
	})

	result, _, err := s.getScreenshot(context.Background(), nil, GetScreenshotInput{})
	if err != nil {
		t.Fatalf("getScreenshot: %v", err)
	}
	assertPNGImageContent(t, result)
}

func TestGetScreenshotUnknownFormat(t *testing.T) {
	s := newTestServer(t)
	startFakeHarness(t, s.dataDir, map[string]any{"status": "ok", "format": "none"})

	result, _, err := s.getScreenshot(context.Background(), nil, GetScreenshotInput{})
	if err != nil {
		t.Fatalf("getScreenshot: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("getScreenshot() result = %v, want an IsError result for an unknown format", result)
	}
}

func assertPNGImageContent(t *testing.T, result *mcp.CallToolResult) {
	t.Helper()
	if result == nil || len(result.Content) != 1 {
		t.Fatalf("result = %v, want exactly one content block", result)
	}
	img, ok := result.Content[0].(*mcp.ImageContent)
	if !ok {
		t.Fatalf("content[0] = %T, want *mcp.ImageContent", result.Content[0])
	}
	if img.MIMEType != "image/png" {
		t.Fatalf("MIMEType = %q, want image/png", img.MIMEType)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("image content is not a valid PNG: %v", err)
	}
}
