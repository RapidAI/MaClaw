//go:build windows

package guiapp

import (
	"image"
	"testing"
)

func TestNormalizeFloatingNativeSizeTracksPetWindow(t *testing.T) {
	minWindow := minPetSize + 16
	maxWindow := maxPetSize + 16
	cases := []struct {
		in, want int
	}{
		{0, minWindow},
		{-4, minWindow},
		{40, minWindow},
		{minWindow, minWindow},
		{136, 136},
		{240, 240},
		{249, 249},
		{maxWindow, maxWindow},
		{400, maxWindow},
	}
	for _, tc := range cases {
		if got := normalizeFloatingNativeSize(tc.in); got != tc.want {
			t.Fatalf("normalizeFloatingNativeSize(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestRearingResizeRebuildsTheFrameBuffers(t *testing.T) {
	w := &windowsFloatingWindow{size: 240, distMap: floatingDistanceMap(240)}
	w.distMap[0] = -1
	w.ApplyPetRearing("", 240, "notice", nil)
	if w.distMap[0] != -1 {
		t.Fatal("same window size rebuilt the distance map")
	}
	// Familiar at the default 224 is a 249 window. Grown and settled reach 256.
	w.ApplyPetRearing("", 249, "", nil)
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.size != 249 || len(w.distMap) != 249*249 {
		t.Fatalf("size %d, map %d", w.size, len(w.distMap))
	}
	if w.distMap[0] < 0 {
		t.Fatal("resized window kept the previous distance map")
	}
	if w.baseImg == nil || w.baseImg.Bounds().Dx() != 249 || w.baseImg.Bounds().Dy() != 249 {
		t.Fatal("fallback image was not rebuilt for the new window")
	}
}

func TestRearingAdornmentChangeKeepsTheDistanceMap(t *testing.T) {
	w := &windowsFloatingWindow{size: 256, distMap: floatingDistanceMap(256)}
	w.distMap[0] = -1
	w.petFrameCache = map[petFrameCacheKey]*image.NRGBA{{Size: 1}: nil}
	slots := []string{"mouse_charm"}
	w.ApplyPetRearing("", 256, "", slots)
	slots[0] = "other"
	if w.distMap[0] != -1 || w.size != 256 {
		t.Fatal("adornment-only change rebuilt the window buffers")
	}
	if len(w.petRearingAdornments) != 1 || w.petRearingAdornments[0] != "mouse_charm" {
		t.Fatalf("stored slots = %v", w.petRearingAdornments)
	}
	if len(w.petFrameCache) != 0 {
		t.Fatal("adornment change kept the cached frame")
	}
	w.petFrameCache = map[petFrameCacheKey]*image.NRGBA{{Size: 1}: nil}
	w.ApplyPetRearing("", 256, "", []string{"mouse_charm"})
	if len(w.petFrameCache) != 1 || w.distMap[0] != -1 {
		t.Fatal("repeating the same adornments cleared the frame")
	}
}

func TestSeededRearingAdornmentsReachTheFirstFrame(t *testing.T) {
	prevNow := petCharacterFrameNow
	petCharacterFrameNow = func() int64 { return 1_700_000_000_000 }
	t.Cleanup(func() { petCharacterFrameNow = prevNow })
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() { app.closePetRearingDBLocked() })
	app.petRearing.pack = "ragdoll"
	app.petRearing.loaded = 7_200_000
	app.petRearing.announced = "familiar"
	app.savePetRearingLocked()
	app.closePetRearingDBLocked()
	app.petRearing.pack = ""
	app.petRearing.loaded = 0
	app.petRearing.running = false

	seeded := &windowsFloatingWindow{app: app}
	seeded.petRearingAdornments = app.copyPetRearingAdornmentSlots("ragdoll")
	if len(seeded.petRearingAdornments) != 1 || seeded.petRearingAdornments[0] != "mouse_charm" {
		t.Fatalf("seed = %v", seeded.petRearingAdornments)
	}
	withCharm := opaqueFramePixels(seeded.renderCharacterPackFrame(256, "ragdoll", "default", "idle"))
	bare := opaqueFramePixels((&windowsFloatingWindow{}).renderCharacterPackFrame(256, "ragdoll", "default", "idle"))
	if withCharm <= bare {
		t.Fatalf("first frame hid the saved charm: bare=%d charm=%d", bare, withCharm)
	}
}

func opaqueFramePixels(img *image.NRGBA) int {
	if img == nil {
		return 0
	}
	n := 0
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] >= 16 {
			n++
		}
	}
	return n
}
