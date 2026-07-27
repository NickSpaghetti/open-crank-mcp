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
	return "", fmt.Errorf("no bundleID found in %s", path)
}
