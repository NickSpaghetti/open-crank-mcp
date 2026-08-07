package build

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ReadBundleID reads bundleID out of a built .pdx's pdxinfo file. A .pdx is
// a directory on the Simulator target, not an archive, so pdxinfo is just
// <pdxPath>/pdxinfo - a plain key=value text file.
//
// A missing bundleID is a real case, not a corrupt build: `pdc` happily compiles
// a project whose Source/pdxinfo has no bundleID, or none at all, and synthesises
// a pdxinfo carrying only pdxversion and buildtime. Several of the SDK's own
// Lua examples ship that way. So the error says what to do about it, because
// everything the harness does is keyed on the bundle ID and there is no way to
// proceed without one.
func ReadBundleID(pdxPath string) (string, error) {
	path := filepath.Join(pdxPath, "pdxinfo")
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, value, found := strings.Cut(scanner.Text(), "=")
		if found && key == "bundleID" {
			return value, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	return "", fmt.Errorf(
		"no bundleID in %s, so there is no way to find the game's data directory - "+
			"and every harness tool talks through it. `pdc` does not require one and "+
			"will build without it, which is why this only surfaces now. Add a line like "+
			"`bundleID=com.example.yourgame` to your project's Source/pdxinfo and rebuild",
		path)
}
