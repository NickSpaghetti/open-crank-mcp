// Package screenshot decodes the C harness's raw framebuffer dump into a PNG.
// The Lua harness writes a PNG itself, so only the C path comes through here.
package screenshot

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
)

const (
	// The SDK's LCD_COLUMNS, LCD_ROWS and LCD_ROWSIZE, pinned in c-harness/test/test_sdk_contract.c.
	Width    = 400
	Height   = 240
	RowBytes = 52

	rawSize = Height * RowBytes
)

// setBitIsWhite is the framebuffer's bit polarity, which the SDK never documents.
// Pinned empirically by internal/contracttest.
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

// unpackGray expands the 1-bpp dump into an 8-bit grayscale image. Separate from
// encoding so the two can be benchmarked; zlib dominates (decode_bench_test.go).
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
