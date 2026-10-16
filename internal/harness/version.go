package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
)

// The harness version is a content fingerprint of the embedded canonical
// sources, stamped into each copy by `setup`, so no number needs bumping.

// VersionPlaceholder marks where the fingerprint is stamped. The stamping
// guard counts occurrences, so it must not appear in prose.
const VersionPlaceholder = "@HARNESS_VERSION@"

// Canonical paths within the embedded harness FS (opencrank.HarnessFS).
const (
	LuaSourcePath = "lua/mcp_harness.lua"
	CHeaderPath   = "c-harness/mcp_harness.h"
	CSourcePath   = "c-harness/mcp_harness.c"
)

// fingerprintLen keeps the stamp readable; this is not collision-resistant.
const fingerprintLen = 12

// fingerprintOf hashes the given sources together, folding in each file's
// name and length so moving text between files changes the result.
func fingerprintOf(fsys fs.FS, paths ...string) (string, error) {
	h := sha256.New()
	for _, p := range paths {
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return "", fmt.Errorf("fingerprinting %s: %w", p, err)
		}
		fmt.Fprintf(h, "%s\x00%d\x00", p, len(b))
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))[:fingerprintLen], nil
}

// LuaFingerprint identifies the Lua harness.
func LuaFingerprint(fsys fs.FS) (string, error) {
	return fingerprintOf(fsys, LuaSourcePath)
}

// CFingerprint identifies the C harness, over the header and the source
// together: the stamp only lives in the header, but a change to either matters.
func CFingerprint(fsys fs.FS) (string, error) {
	return fingerprintOf(fsys, CHeaderPath, CSourcePath)
}

// FingerprintFor returns the fingerprint to stamp into the named canonical
// source, or "" for a file that carries no stamp (the C pair's lives in the header).
func FingerprintFor(fsys fs.FS, sourcePath string) (string, error) {
	switch sourcePath {
	case LuaSourcePath:
		return LuaFingerprint(fsys)
	case CHeaderPath:
		return CFingerprint(fsys)
	default:
		return "", nil
	}
}

// Fingerprints returns every fingerprint this binary considers current. Both
// are returned since the server does not know whether the game is C or Lua.
func Fingerprints(fsys fs.FS) ([]string, error) {
	lua, err := LuaFingerprint(fsys)
	if err != nil {
		return nil, err
	}
	c, err := CFingerprint(fsys)
	if err != nil {
		return nil, err
	}
	return []string{lua, c}, nil
}

// IsCurrentFingerprint reports whether reported matches a fingerprint this binary
// ships.
func IsCurrentFingerprint(fsys fs.FS, reported string) (bool, error) {
	known, err := Fingerprints(fsys)
	if err != nil {
		return false, err
	}
	for _, k := range known {
		if k == reported {
			return true, nil
		}
	}
	return false, nil
}
