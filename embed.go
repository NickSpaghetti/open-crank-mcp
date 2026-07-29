// Package opencrank exists to embed the canonical harness sources into the
// server binary. It holds nothing else.
//
// The harness files have to travel with the binary, because `setup` writes a
// copy of them into whatever game it is wiring up. Reading them off disk only
// worked while the server ran exclusively from the Docker image, where WORKDIR
// is this repo root and the whole tree is present. A binary installed anywhere
// else has no repo to read from, and an MCP client launches its server with an
// arbitrary working directory.
//
// Why this package is here, at the repo root, rather than inside
// internal/setup where it is used: `go:embed` cannot reference a parent
// directory, so the embedding package has to sit at or above lua/ and
// c-harness/. The repo root can hold exactly one package, and internal/setup
// cannot import `package main`. So the root became a library package, and
// main.go moved to cmd/open-crank-mcp/.
//
// The patterns name the three files exactly rather than using c-harness/*,
// which would also pull the C test suite and the fixture game into every
// binary.
package opencrank

import (
	"embed"
	"io/fs"
)

//go:embed lua/mcp_harness.lua
//go:embed c-harness/mcp_harness.h
//go:embed c-harness/mcp_harness.c
var harnessFiles embed.FS

// HarnessFS holds the harness sources, keyed by their repo-relative paths
// (`lua/mcp_harness.lua`, `c-harness/mcp_harness.h`, `c-harness/mcp_harness.c`).
// Those names are what internal/setup looks up, so they are part of this
// package's contract, not an implementation detail.
//
// Paths in an fs.FS are always slash-separated, on every platform. Anything
// reading from here must use path.Join, never filepath.Join.
var HarnessFS fs.FS = harnessFiles
