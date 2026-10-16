// Package opencrank embeds the harness sources into the server binary. It lives
// at the repo root because go:embed cannot reach a parent directory.
package opencrank

import (
	"embed"
	"io/fs"
)

// Named individually so the C test suite and fixture game stay out of the binary.
//
//go:embed lua/mcp_harness.lua
//go:embed c-harness/mcp_harness.h
//go:embed c-harness/mcp_harness.c
var harnessFiles embed.FS

// HarnessFS holds the harness sources keyed by repo-relative path. fs.FS paths
// are slash-separated on every platform, so use path.Join, not filepath.Join.
var HarnessFS fs.FS = harnessFiles
