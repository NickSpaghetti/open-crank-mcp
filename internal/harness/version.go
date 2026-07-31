package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
)

// Identifying which harness a game is carrying, without anyone having to
// remember to bump a number.
//
// The harness is a *copy*. `setup` writes it into a game's own source tree, and
// from that moment the game's copy and this repo's canonical version can drift
// with nothing to notice. That is not hypothetical: renaming the game-log file
// during the performance review made every already-set-up game report an empty
// log, silently, because a copy from before the change kept writing the old one.
//
// A hand-maintained version constant would have caught it, and would also be one
// more thing to forget - and forgetting it reproduces exactly the bug it exists to
// prevent. So the version is not written by hand at all: it is a content
// fingerprint of the canonical harness source, computed from the bytes embedded in
// this binary, and stamped into each copy as `setup` writes it. Change any harness
// source and every stamp is different, automatically.
//
// It also catches strictly more than a counter would. A locally hand-edited copy
// drifts too, which is worth knowing, so the warning built on this says "differs
// from the harness this server ships" rather than claiming to know which.

// VersionPlaceholder is what the canonical sources carry where the fingerprint
// goes. Deliberately a shape no comment or prose would contain, because the
// stamping guard counts occurrences and a stray mention in a comment would break
// it.
const VersionPlaceholder = "@HARNESS_VERSION@"

// Canonical paths within the embedded harness FS (opencrank.HarnessFS). Named
// here because the fingerprint is defined over exactly these files; the embed
// package documents the same names as part of its contract.
const (
	LuaSourcePath = "lua/mcp_harness.lua"
	CHeaderPath   = "c-harness/mcp_harness.h"
	CSourcePath   = "c-harness/mcp_harness.c"
)

// fingerprintLen keeps the stamp short enough to read in a response or an error
// message. This distinguishes versions of one file that this project wrote; it is
// not defending against anyone constructing a collision.
const fingerprintLen = 12

// fingerprintOf hashes the given canonical sources together.
//
// Each file's name and length are folded in before its bytes, so moving text
// between two files of a pair still changes the result - a plain concatenation
// would not.
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
// source, or "" for a file that carries no stamp.
//
// The C source is stampless on purpose - the C pair's stamp lives in the header,
// where the `#define` a game compiles against belongs.
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

// Fingerprints returns every fingerprint this binary considers current, for
// checking a reported one against.
//
// Both are returned because the server does not otherwise know whether the game
// answering it is C or Lua, and it does not need to: matching either means the
// game is carrying a current harness.
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
