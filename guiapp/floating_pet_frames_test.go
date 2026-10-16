package guiapp

import (
	"bytes"
	"image/png"
	"testing"

	"github.com/RapidAI/CodeClaw/guiapp/petpack"
)

func TestDesktopPetStillPNGUsesPackFrame(t *testing.T) {
	prev := petpack.EnsureGlobal()
	t.Cleanup(func() { petpack.SetGlobalForTest(prev) })

	reg := petpack.NewRegistry(t.TempDir(), petpack.BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	petpack.SetGlobalForTest(reg)

	logo := []byte("app-icon-logo")
	got := desktopPetStillPNG("clawmate", "default", "idle", 88, petpack.NewFrameCache(), logo)
	if bytes.Equal(got, logo) {
		t.Fatal("desktop pet still fell back to the app icon")
	}
	img, err := png.Decode(bytes.NewReader(got))
	if err != nil {
		t.Fatalf("decode pack still: %v", err)
	}
	if img.Bounds().Dx() != 88 || img.Bounds().Dy() != 88 {
		t.Fatalf("frame bounds = %v, want 88x88", img.Bounds())
	}
	transparent := 0
	for y := 0; y < 88; y++ {
		for x := 0; x < 88; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a == 0 {
				transparent++
			}
		}
	}
	if transparent == 0 {
		t.Fatal("pack still has no transparent pixels; pet would paint as a solid badge")
	}
}

func TestDesktopPetStillPNGUsesRequestedPixelSize(t *testing.T) {
	prev := petpack.EnsureGlobal()
	t.Cleanup(func() { petpack.SetGlobalForTest(prev) })

	reg := petpack.NewRegistry(t.TempDir(), petpack.BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	petpack.SetGlobalForTest(reg)

	got := desktopPetStillPNG("clawmate", "default", "idle", 176, petpack.NewFrameCache(), nil)
	img, err := png.Decode(bytes.NewReader(got))
	if err != nil {
		t.Fatalf("decode pack still: %v", err)
	}
	if img.Bounds().Dx() != 176 || img.Bounds().Dy() != 176 {
		t.Fatalf("frame bounds = %v, want 176x176", img.Bounds())
	}
}

func TestDesktopPetStillPNGFallsBackToLogo(t *testing.T) {
	prev := petpack.EnsureGlobal()
	t.Cleanup(func() { petpack.SetGlobalForTest(prev) })

	reg := petpack.NewRegistry(t.TempDir(), nil)
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	petpack.SetGlobalForTest(reg)

	logo := []byte("app-icon-logo")
	got := desktopPetStillPNG("missing-pet", "default", "idle", 88, nil, logo)
	if !bytes.Equal(got, logo) {
		t.Fatalf("missing pack returned %d bytes, want logo fallback", len(got))
	}
}
