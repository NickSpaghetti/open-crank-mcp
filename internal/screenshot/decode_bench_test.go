package screenshot

import (
	"os"
	"path/filepath"
	"testing"
)

// testdata/missile-command.raw is a real getDisplayFrame() dump, captured from
// missile-command's C port running in the Simulator - not synthetic. That
// matters for the payload half of what this measures: PNG output size depends
// entirely on how well zlib handles real dithered game content, and uniform
// synthetic input would flatter it.
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

// BenchmarkDecodeRawToPNG exists to keep this honest rather than to chase a
// number. The reasoning that said this path was worth optimising - 96,000
// SetGray calls and an 8-bit grayscale encoding of 1-bpp source - implied a
// large win, and measuring against a real game said otherwise on both counts:
// a get_screenshot round trip is ~33ms and frame-bound (press_button, which
// decodes nothing, costs the same), and the PNG comes out at well under 2KB
// because zlib already removes the redundancy. So this benchmark's job is to
// show what the decode actually costs relative to that 33ms floor, and to make
// any future claim about it checkable instead of argued.
func BenchmarkDecodeRawToPNG(b *testing.B) {
	raw := realDump(b)
	b.SetBytes(int64(len(raw)))
	for b.Loop() {
		if _, err := DecodeRawToPNG(raw); err != nil {
			b.Fatalf("DecodeRawToPNG: %v", err)
		}
	}
}

// TestDecodeRawToPNGRealDumpSize records the actual encoded size of a real
// frame, since that is the number that reaches the model's context on every
// get_screenshot (base64'd, so about 4/3 of this). Not an assertion about
// compression - a failure here means the output grew by more than 4x, which
// would mean something changed about the encoding, not about the game.
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

// BenchmarkUnpackGray isolates the pixel loop from the PNG encoding, which is
// what decides whether the loop is worth rewriting. Measured together on one
// machine: DecodeRawToPNG 773us, UnpackGray 128us - so the 96,000-pixel loop is
// 16% of this function and zlib is the other 84%. Against a 33ms frame-bound
// round trip, making the loop infinitely fast would save 0.4%. That is the whole
// argument for leaving it as the readable version rather than writing into
// img.Pix by hand, and it is why this benchmark is worth keeping even though
// nothing was optimised: the next person to look at that loop and see 96,000
// function calls can re-run this instead of rewriting it.
func BenchmarkUnpackGray(b *testing.B) {
	raw := realDump(b)
	b.SetBytes(int64(len(raw)))
	for b.Loop() {
		if _, err := unpackGray(raw); err != nil {
			b.Fatalf("unpackGray: %v", err)
		}
	}
}
