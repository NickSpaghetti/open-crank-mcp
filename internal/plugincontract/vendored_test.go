package plugincontract

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// The vendored schemas are refreshed from upstream, never hand-edited to make
// plugin-upstream-check pass. Update these hashes only alongside a refresh.
var vendoredSchemaSHA256 = map[string]string{
	"plugin/schemas/1.0.0/mcp.schema.json":    "6539175bfcdf43085855183e86da40ea94b166547a72b47ae9a0a390516d3acb",
	"plugin/schemas/1.0.0/plugin.schema.json": "0a4aad95ce337878ad38802ebf0daa3fde76abe3f65400c86bcbb1ec0b3ab883",
}

func TestVendoredSchemasUnedited(t *testing.T) {
	for path, want := range vendoredSchemaSHA256 {
		b, err := os.ReadFile(filepath.Join(repoRoot, path))
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		// A Windows checkout may convert line endings.
		sum := sha256.Sum256(bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")))
		if got := hex.EncodeToString(sum[:]); got != want {
			t.Errorf("%s sha256 = %s, want %s", path, got, want)
		}
	}
}
