package build

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ReadBundleID reads bundleID out of <pdxPath>/pdxinfo, a key=value text file.
// pdc builds fine without a bundleID, so the error names the remedy.
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
