package screenshot

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func decodePNG(t *testing.T, pngBytes []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("png.Decode: %v", err)
	}
	return img
}

func grayAt(t *testing.T, img image.Image, x, y int) uint8 {
	t.Helper()
	c := color.GrayModel.Convert(img.At(x, y)).(color.Gray)
	return c.Y
}

func TestDecodeRawToPNGRejectsWrongSize(t *testing.T) {
	if _, err := DecodeRawToPNG(make([]byte, rawSize-1)); err == nil {
		t.Fatal("DecodeRawToPNG: expected an error for a short buffer, got nil")
	}
	if _, err := DecodeRawToPNG(make([]byte, rawSize+1)); err == nil {
		t.Fatal("DecodeRawToPNG: expected an error for a long buffer, got nil")
	}
}

// TestDecodeRawToPNGPolarityMatchesRealHardware pins the bit-to-color
// mapping to an absolute value, confirmed against the real Simulator by
// internal/contracttest (the C fixture clears to kColorBlack and its
// decoded screenshot comes back all zero). The other tests in this file
// only check internal consistency (uniform, or differs from background),
// since they were written before that mapping was known - they'd still
// pass under a mutation that globally inverted polarity. This test is the
// one that catches that.
func TestDecodeRawToPNGPolarityMatchesRealHardware(t *testing.T) {
	allClear := make([]byte, rawSize)
	pngBytes, err := DecodeRawToPNG(allClear)
	if err != nil {
		t.Fatalf("DecodeRawToPNG: %v", err)
	}
	if got := grayAt(t, decodePNG(t, pngBytes), 0, 0); got != 0 {
		t.Fatalf("all-bits-clear decoded to %d, want 0 (black) to match the real Simulator", got)
	}
}

func TestDecodeRawToPNGAllBitsSetIsOneSolidColor(t *testing.T) {
	raw := make([]byte, rawSize)
	for i := range raw {
		raw[i] = 0xFF
	}

	pngBytes, err := DecodeRawToPNG(raw)
	if err != nil {
		t.Fatalf("DecodeRawToPNG: %v", err)
	}
	img := decodePNG(t, pngBytes)

	want := grayAt(t, img, 0, 0)
	for y := 0; y < Height; y++ {
		for x := 0; x < Width; x++ {
			if got := grayAt(t, img, x, y); got != want {
				t.Fatalf("pixel (%d,%d) = %d, want %d (all-bits-set should be one solid color)", x, y, got, want)
			}
		}
	}
}

func TestDecodeRawToPNGAllBitsClearIsTheOppositeSolidColor(t *testing.T) {
	allSet := make([]byte, rawSize)
	for i := range allSet {
		allSet[i] = 0xFF
	}
	setPNG, err := DecodeRawToPNG(allSet)
	if err != nil {
		t.Fatalf("DecodeRawToPNG: %v", err)
	}
	setColor := grayAt(t, decodePNG(t, setPNG), 0, 0)

	allClear := make([]byte, rawSize)
	clearPNG, err := DecodeRawToPNG(allClear)
	if err != nil {
		t.Fatalf("DecodeRawToPNG: %v", err)
	}
	img := decodePNG(t, clearPNG)

	want := grayAt(t, img, 0, 0)
	if want == setColor {
		t.Fatalf("all-bits-clear decoded to %d, same as all-bits-set (%d), want the opposite color", want, setColor)
	}
	for y := 0; y < Height; y++ {
		for x := 0; x < Width; x++ {
			if got := grayAt(t, img, x, y); got != want {
				t.Fatalf("pixel (%d,%d) = %d, want %d (all-bits-clear should be one solid color)", x, y, got, want)
			}
		}
	}
}

func TestDecodeRawToPNGSingleBitAtColumnZero(t *testing.T) {
	raw := make([]byte, rawSize)
	raw[0] = 0x80 // MSB of the first byte: column 0 per the SDK's docs

	pngBytes, err := DecodeRawToPNG(raw)
	if err != nil {
		t.Fatalf("DecodeRawToPNG: %v", err)
	}
	img := decodePNG(t, pngBytes)

	background := grayAt(t, img, 1, 0)
	pixel := grayAt(t, img, 0, 0)
	if pixel == background {
		t.Fatalf("pixel (0,0) = %d, same as background %d, want the opposite", pixel, background)
	}
}

func TestDecodeRawToPNGSingleBitAtColumnSeven(t *testing.T) {
	raw := make([]byte, rawSize)
	raw[0] = 0x01 // LSB of the first byte: column 7 per the SDK's MSB-first docs

	pngBytes, err := DecodeRawToPNG(raw)
	if err != nil {
		t.Fatalf("DecodeRawToPNG: %v", err)
	}
	img := decodePNG(t, pngBytes)

	background := grayAt(t, img, 6, 0)
	pixel := grayAt(t, img, 7, 0)
	if pixel == background {
		t.Fatalf("pixel (7,0) = %d, same as background %d, want the opposite", pixel, background)
	}
	// Every other bit in the byte is clear, so column 0..6 should all match
	// the background - proves the set bit landed on exactly column 7, not
	// some other column in the same byte.
	for x := 0; x < 7; x++ {
		if got := grayAt(t, img, x, 0); got != background {
			t.Fatalf("pixel (%d,0) = %d, want background %d - the single set bit should only affect column 7", x, got, background)
		}
	}
}

func TestDecodeRawToPNGIgnoresRowPaddingBytes(t *testing.T) {
	clean := make([]byte, rawSize)
	cleanPNG, err := DecodeRawToPNG(clean)
	if err != nil {
		t.Fatalf("DecodeRawToPNG: %v", err)
	}

	withGarbage := make([]byte, rawSize)
	for row := 0; row < Height; row++ {
		withGarbage[row*RowBytes+50] = 0xFF
		withGarbage[row*RowBytes+51] = 0xFF
	}
	garbagePNG, err := DecodeRawToPNG(withGarbage)
	if err != nil {
		t.Fatalf("DecodeRawToPNG: %v", err)
	}

	if !bytes.Equal(cleanPNG, garbagePNG) {
		t.Fatal("garbage in the row-padding bytes changed the decoded image, want it ignored")
	}
}
