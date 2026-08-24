package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	"image/png"

	"github.com/RapidAI/CodeClaw/corelib/computeruse"
)

var somOverlayPalette = []color.RGBA{
	{R: 255, G: 80, B: 80, A: 255},
	{R: 80, G: 180, B: 255, A: 255},
	{R: 80, G: 220, B: 120, A: 255},
	{R: 255, G: 200, B: 60, A: 255},
	{R: 200, G: 120, B: 255, A: 255},
	{R: 255, G: 140, B: 60, A: 255},
}

// annotateSoMOverlay draws numbered-box outlines onto a PNG (base64) so a
// vision model can see the same eN marks listed in the observe text.
func annotateSoMOverlay(pngB64 string, marks []computeruse.MarkedElement) string {
	if pngB64 == "" || len(marks) == 0 {
		return pngB64
	}
	raw, err := base64.StdEncoding.DecodeString(pngB64)
	if err != nil {
		return pngB64
	}
	src, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return pngB64
	}
	b := src.Bounds()
	dst := image.NewRGBA(b)
	draw.Draw(dst, b, src, b.Min, draw.Src)
	max := len(marks)
	if max > 40 {
		max = 40
	}
	for i := 0; i < max; i++ {
		m := marks[i]
		col := somOverlayPalette[i%len(somOverlayPalette)]
		drawRectOutline(dst, m.BBox[0], m.BBox[1], m.BBox[2], m.BBox[3], col, 2)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return pngB64
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func drawRectOutline(img *image.RGBA, x, y, w, h int, col color.RGBA, thickness int) {
	if w < 2 || h < 2 || thickness < 1 {
		return
	}
	b := img.Bounds()
	x2 := x + w - 1
	y2 := y + h - 1
	for t := 0; t < thickness; t++ {
		for px := x; px <= x2; px++ {
			plotRGBA(img, b, px, y+t, col)
			plotRGBA(img, b, px, y2-t, col)
		}
		for py := y; py <= y2; py++ {
			plotRGBA(img, b, x+t, py, col)
			plotRGBA(img, b, x2-t, py, col)
		}
	}
}

func plotRGBA(img *image.RGBA, b image.Rectangle, x, y int, col color.RGBA) {
	if x < b.Min.X || y < b.Min.Y || x >= b.Max.X || y >= b.Max.Y {
		return
	}
	img.SetRGBA(x, y, col)
}
