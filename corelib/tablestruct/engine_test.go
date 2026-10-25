package tablestruct

import (
	"image"
	"image/color"
	"os"
	"testing"
)

func TestWiredUNETOnGrid(t *testing.T) {
	path := os.Getenv("PP_TABLE_UNET")
	if path == "" {
		t.Skip("PP_TABLE_UNET not set")
	}
	eng, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	const side = 256
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	for _, at := range []int{32, 128, 220} {
		for x := 8; x < side-8; x++ {
			img.SetRGBA(x, at, color.RGBA{A: 255})
			img.SetRGBA(x, at+1, color.RGBA{A: 255})
		}
		for y := 8; y < side-8; y++ {
			img.SetRGBA(at, y, color.RGBA{A: 255})
			img.SetRGBA(at+1, y, color.RGBA{A: 255})
		}
	}
	html, err := eng.HTML(img, []Line{{Text: "格", X: 40, Y: 40, W: 20, H: 16}})
	if err != nil {
		t.Fatal(err)
	}
	if html == "" {
		t.Fatal("grid image produced no table")
	}
	t.Log(html)
}
