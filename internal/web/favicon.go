package web

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"net/http"
)

// The icon is drawn from this grid rather than checked in as a binary: 16×16 is
// the only size Chrome will use for a search engine, and at that size the art
// is easier to read and review as text than as a blob.
//
// Chrome asks for /favicon.ico on the search URL's origin whenever the
// descriptor carries no usable <Image>, which is why the descriptor carries
// none: an <Image> would have to be a 16×16 image/x-icon, and a data: URL is
// discarded outright. The onboarding page needs no icon link of its own — the
// browser probes the same path for a tab icon.
var glyph = [...]string{
	"................",
	"................",
	"................",
	"..######..##....",
	"..#....#..##....",
	"..#....#..##....",
	"..#....#..##....",
	"..######..##....",
	"..#....#..##....",
	"..#....#..##....",
	"..#....#........",
	"..######..##....",
	"................",
	"................",
	"................",
	"................",
}

var (
	iconBackground = color.RGBA{R: 0x11, G: 0x18, B: 0x27, A: 0xff}
	iconForeground = color.RGBA{R: 0xf9, G: 0xfa, B: 0xfb, A: 0xff}
)

var iconPNG = renderPNG()

// renderPNG encodes the grid as a PNG. Chrome and Firefox both decode by
// sniffing the bytes, so the .ico route can serve it under its own name; a real
// ICO container would add a header and nothing else at a single size.
func renderPNG() []byte {
	size := len(glyph)
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.Draw(img, img.Bounds(), image.NewUniform(iconBackground),
		image.Point{}, draw.Src)
	for y, row := range glyph {
		for x, pixel := range row {
			if pixel == '#' {
				img.Set(x, y, iconForeground)
			}
		}
	}

	var out bytes.Buffer
	// Encoding a 16×16 image into memory has no failure mode to handle at
	// runtime, and a panic here would surface on the first test run.
	if err := png.Encode(&out, img); err != nil {
		panic(err)
	}
	return out.Bytes()
}

func faviconICO(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	if _, err := w.Write(iconPNG); err != nil {
		log.Printf("favicon: %v", err)
	}
}
