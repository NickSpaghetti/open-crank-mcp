package tools

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NickSpaghetti/open-crank-mcp/internal/harness"

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

// The Lua harness writes PNGs into the scratch directory, not the sandboxed data
// directory, because playdate.simulator.writeToFile takes a host path. This
// fixture therefore puts the file where Lua would, which on Linux is a different
// directory from the raw case for the first time.
func TestGetScreenshotPNGFormat(t *testing.T) {
	s := newTestServer(t)
	s.scratchDir = t.TempDir()

	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(s.scratchDir, "mcp"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(s.scratchDir, "mcp", "screenshot.png"), buf.Bytes(), 0o644); err != nil {
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

// A PNG in the data directory must NOT be found, because that is where the C
// harness writes and Lua does not. Without this, the two bases could quietly
// collapse back into one - which is exactly what they were on Linux before, and
// why the difference went unnoticed.
func TestGetScreenshotPNGIgnoresDataDir(t *testing.T) {
	s := newTestServer(t)
	s.scratchDir = t.TempDir()

	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	// Written to the data directory, where a PNG never belongs.
	if err := os.MkdirAll(filepath.Join(s.dataDir, "mcp"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(s.dataDir, "mcp", "screenshot.png"), buf.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	startFakeHarness(t, s.dataDir, map[string]any{
		"status": "ok", "format": "png", "path": "mcp/screenshot.png",
	})

	if _, _, err := s.getScreenshot(context.Background(), nil, GetScreenshotInput{}); err == nil {
		t.Fatal("getScreenshot read a PNG out of the data directory; the two bases have collapsed into one")
	}
}

// The regression this pins: restart cleared the scratch directory, never made a
// new one, and handed the data directory to the game instead. The harness then
// reported writing the PNG successfully while the server joined an empty base
// into the bare relative path "mcp/screenshot.png" and failed to open it.
func TestGetScreenshotAfterRestart(t *testing.T) {
	s := newTestServer(t)
	s.scratchDir = t.TempDir()

	if _, _, err := s.restartSimulator(context.Background(), nil, RestartSimulatorInput{}); err != nil {
		t.Fatalf("restartSimulator: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(s.scratchDir) })

	// Written where the restarted game was told to write, not where the first
	// launch was: that difference is the whole bug.
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	shot := filepath.Join(s.scratchDir, "mcp", "screenshot.png")
	if err := os.WriteFile(shot, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", shot, err)
	}
	startFakeHarness(t, s.dataDir, map[string]any{
		"status": "ok", "format": "png", "path": "mcp/screenshot.png",
	})

	result, _, err := s.getScreenshot(context.Background(), nil, GetScreenshotInput{})
	if err != nil {
		t.Fatalf("getScreenshot after restart: %v", err)
	}
	assertPNGImageContent(t, result)
}

// An empty scratch directory used to join into a relative path, so the failure
// read as a missing file rather than as missing state.
func TestScreenshotBaseRejectsAnEmptyScratchDir(t *testing.T) {
	s := newTestServer(t)
	s.scratchDir = ""

	if _, err := s.screenshotBase(harness.FormatPNG); !errors.Is(err, errNoScratch) {
		t.Fatalf("screenshotBase(png) err = %v, want errNoScratch", err)
	}
}

// The three states that separate "never got the command" from "could not write
// the file", which is the distinction a bare round-trip timeout cannot make.
func TestScratchReportDescribesWhatTheGameWasGiven(t *testing.T) {
	for _, tc := range []struct {
		name    string
		setup   func(t *testing.T, s *Server)
		wantAny []string
	}{
		{
			name:    "no scratch directory",
			setup:   func(t *testing.T, s *Server) { s.scratchDir = "" },
			wantAny: []string{"no scratch directory recorded"},
		},
		{
			name: "scratch exists but the game wrote nothing",
			setup: func(t *testing.T, s *Server) {
				s.scratchDir = t.TempDir()
				if err := os.MkdirAll(filepath.Join(s.scratchDir, luaScreenshotRelDir), 0o755); err != nil {
					t.Fatalf("MkdirAll: %v", err)
				}
			},
			wantAny: []string{"exists and is empty"},
		},
		{
			name: "the game wrote a screenshot",
			setup: func(t *testing.T, s *Server) {
				s.scratchDir = t.TempDir()
				dir := filepath.Join(s.scratchDir, luaScreenshotRelDir)
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatalf("MkdirAll: %v", err)
				}
				if err := os.WriteFile(filepath.Join(dir, "screenshot.png"), []byte("x"), 0o644); err != nil {
					t.Fatalf("WriteFile: %v", err)
				}
			},
			wantAny: []string{"screenshot.png"},
		},
		{
			name:    "scratch recorded but missing on disk",
			setup:   func(t *testing.T, s *Server) { s.scratchDir = filepath.Join(t.TempDir(), "gone") },
			wantAny: []string{"unreadable"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{}
			tc.setup(t, s)
			got := s.scratchReport()
			for _, want := range tc.wantAny {
				if !strings.Contains(got, want) {
					t.Errorf("scratchReport() = %q, want it to mention %q", got, want)
				}
			}
			if s.scratchDir != "" && !strings.Contains(got, s.scratchDir) {
				t.Errorf("scratchReport() = %q, want it to name the scratch path %q", got, s.scratchDir)
			}
		})
	}
}
