// Package screenshot decodes the C harness's raw framebuffer dump into a
// PNG. The Lua harness has no raw framebuffer accessor and already produces
// a real PNG directly via simulator.writeToFile, so this package only ever
// handles the C path.
package screenshot

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
)

const (
	// Width, Height, and RowBytes mirror the SDK's LCD_COLUMNS, LCD_ROWS,
	// and LCD_ROWSIZE, pinned via _Static_assert in
	// c-harness/test/test_sdk_contract.c.
	Width    = 400
	Height   = 240
	RowBytes = 52

	rawSize = Height * RowBytes
)

// setBitIsWhite is the raw framebuffer's bit polarity: whether a set bit
// means a white pixel (true) or a black pixel (false). The SDK's docs and
// headers give the row layout (MSB-first, 52-byte stride) but never state
// this. It's pinned empirically by internal/contracttest's C harness
// screenshot assertion, which clears the fixture's display to a known
// color before screenshotting.
const setBitIsWhite = true

// DecodeRawToPNG decodes a raw Playdate framebuffer dump (1 bit per pixel,
// MSB-first, 52-byte row stride, the last 2 bytes of each row being
// alignment padding) into a PNG-encoded image.
func DecodeRawToPNG(raw []byte) ([]byte, error) {
	img, err := unpackGray(raw)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encoding PNG: %w", err)
	}
	return buf.Bytes(), nil
}

// unpackGray expands the 1-bpp dump into an 8-bit grayscale image.
//
// Split out from the PNG encoding above so the two halves can be benchmarked
// separately, because their relative cost is the whole question about this
// function: the obvious complaint is that it touches every one of the 96,000
// pixels individually, and measuring says that loop is the cheap half and zlib
// is the rest. See decode_bench_test.go.
func unpackGray(raw []byte) (*image.Gray, error) {
	if len(raw) != rawSize {
		return nil, fmt.Errorf("raw screenshot is %d bytes, want %d (%d rows * %d bytes/row)", len(raw), rawSize, Height, RowBytes)
	}

	img := image.NewGray(image.Rect(0, 0, Width, Height))
	for row := 0; row < Height; row++ {
		for col := 0; col < Width; col++ {
			byteOffset := row*RowBytes + col/8
			bitPos := 7 - col%8
			bitSet := raw[byteOffset]&(1<<bitPos) != 0

			value := uint8(0)
			if bitSet == setBitIsWhite {
				value = 0xFF
			}
			img.SetGray(col, row, color.Gray{Y: value})
		}
	}
	return img, nil
}
