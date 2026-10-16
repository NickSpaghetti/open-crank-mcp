package screenshot

import (
	"os"
	"path/filepath"
	"testing"
)

// testdata/missile-command.raw is a real getDisplayFrame() dump; synthetic input
// would flatter zlib.
func realDump(tb testing.TB) []byte {
	tb.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "missile-command.raw"))
	if err != nil {
		tb.Fatalf("reading the captured dump: %v", err)
	}
	if len(b) != rawSize {
		tb.Fatalf("captured dump is %d bytes, want %d", len(b), rawSize)
	}
	return b
}

// BenchmarkDecodeRawToPNG makes claims about decode cost checkable; a
// get_screenshot round trip is ~33ms and frame-bound regardless.
func BenchmarkDecodeRawToPNG(b *testing.B) {
	raw := realDump(b)
	b.SetBytes(int64(len(raw)))
	for b.Loop() {
		if _, err := DecodeRawToPNG(raw); err != nil {
			b.Fatalf("DecodeRawToPNG: %v", err)
		}
	}
}

// TestDecodeRawToPNGRealDumpSize logs the encoded size of a real frame, which
// reaches the model's context on every get_screenshot.
func TestDecodeRawToPNGRealDumpSize(t *testing.T) {
	raw := realDump(t)
	png, err := DecodeRawToPNG(raw)
	if err != nil {
		t.Fatalf("DecodeRawToPNG: %v", err)
	}
	t.Logf("real frame: %d raw bytes -> %d PNG bytes", len(raw), len(png))
	if len(png) > 4*len(raw) {
		t.Fatalf("PNG is %d bytes for a %d-byte frame, far larger than expected", len(png), len(raw))
	}
}

// BenchmarkUnpackGray isolates the pixel loop from PNG encoding. Measured at
// roughly 16% of DecodeRawToPNG; zlib is the rest.
func BenchmarkUnpackGray(b *testing.B) {
	raw := realDump(b)
	b.SetBytes(int64(len(raw)))
	for b.Loop() {
		if _, err := unpackGray(raw); err != nil {
			b.Fatalf("unpackGray: %v", err)
		}
	}
}
