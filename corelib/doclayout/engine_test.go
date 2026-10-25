package doclayout

import (
	"image"
	"os"
	"testing"
)

func TestAnalyzeSmoke(t *testing.T) {
	path := os.Getenv("PP_DOCLAYOUT")
	if path == "" {
		t.Skip("PP_DOCLAYOUT not set")
	}
	eng, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 80, 60))
	regions, err := eng.Analyze(img)
	if err != nil {
		t.Fatal(err)
	}
	for _, region := range regions {
		if region.Box.X0 < 0 || region.Box.Y0 < 0 || region.Box.X1 > 80 || region.Box.Y1 > 60 || region.Order < 0 {
			t.Fatalf("region %+v", region)
		}
		if region.Label == "" {
			t.Fatalf("empty label %+v", region)
		}
	}
}
