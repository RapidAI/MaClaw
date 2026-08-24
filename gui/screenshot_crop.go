package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
)

// cropPNGBase64 crops a PNG (base64) to the rectangle [x,y,w,h] in image pixels.
func cropPNGBase64(pngB64 string, x, y, w, h int) (string, error) {
	if w < 1 || h < 1 {
		return "", fmt.Errorf("invalid crop %dx%d", w, h)
	}
	raw, err := base64.StdEncoding.DecodeString(pngB64)
	if err != nil {
		return "", err
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	b := img.Bounds()
	if x < b.Min.X {
		w -= b.Min.X - x
		x = b.Min.X
	}
	if y < b.Min.Y {
		h -= b.Min.Y - y
		y = b.Min.Y
	}
	if x+w > b.Max.X {
		w = b.Max.X - x
	}
	if y+h > b.Max.Y {
		h = b.Max.Y - y
	}
	if w < 1 || h < 1 {
		return "", fmt.Errorf("crop outside image")
	}
	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}
	var cropped image.Image
	if s, ok := img.(subImager); ok {
		cropped = s.SubImage(image.Rect(x, y, x+w, y+h))
	} else {
		return "", fmt.Errorf("image type %T cannot crop", img)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, cropped); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}
