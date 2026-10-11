package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
)

// Tray icon colours, one per connection state.
var (
	iconConnected = color.RGBA{R: 0x00, G: 0xA8, B: 0x9D, A: 0xFF} // teal
	iconIdle      = color.RGBA{R: 0x80, G: 0x80, B: 0x80, A: 0xFF} // grey
	iconBusy      = color.RGBA{R: 0xF2, G: 0xA9, B: 0x00, A: 0xFF} // amber
)

// generateIcon procedurally draws a simple filled-circle tray icon in the
// given colour and encodes it as PNG bytes, so the app doesn't depend on any
// external image asset being installed alongside the binary.
func generateIcon(fill color.RGBA) []byte {
	const size = 32
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	ring := color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF} // white outline
	empty := color.RGBA{R: 0, G: 0, B: 0, A: 0}            // transparent

	cx, cy := float64(size)/2, float64(size)/2
	outerR := float64(size)/2 - 1
	innerR := outerR - 2.5

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			dist := dx*dx + dy*dy
			switch {
			case dist <= innerR*innerR:
				img.Set(x, y, fill)
			case dist <= outerR*outerR:
				img.Set(x, y, ring)
			default:
				img.Set(x, y, empty)
			}
		}
	}

	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
