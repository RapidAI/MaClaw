package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/computeruse"
)

func TestAnnotateSoMOverlayDrawsBox(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 20, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 20; x++ {
			img.Set(x, y, color.White)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	src := base64.StdEncoding.EncodeToString(buf.Bytes())
	out := annotateSoMOverlay(src, []computeruse.MarkedElement{
		{Ref: "e0", BBox: [4]int{2, 2, 8, 8}},
	})
	if out == "" || out == src {
		t.Fatal("expected annotated png to differ")
	}
}
