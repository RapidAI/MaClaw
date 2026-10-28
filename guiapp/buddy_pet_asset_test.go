package guiapp

import (
	"encoding/base64"
	"testing"

	"github.com/RapidAI/CodeClaw/buddy/protocol"
	"github.com/RapidAI/CodeClaw/corelib"
)

// A pet skin or variant change must refresh the companion, while size/motion
// settings must not trigger a 4-second asset transfer.
func TestBuddyPetAssetChanged(t *testing.T) {
	base := corelib.AppConfig{PetSkin: "clawmate", PetVariant: "default"}
	if buddyPetAssetChanged(base, base) {
		t.Fatal("identical configs reported as changed")
	}
	skin := base
	skin.PetSkin = "penguin"
	if !buddyPetAssetChanged(base, skin) {
		t.Fatal("pet skin change not detected")
	}
	variant := base
	variant.PetVariant = "figurative"
	if !buddyPetAssetChanged(base, variant) {
		t.Fatal("pet variant change not detected")
	}
	unrelated := base
	unrelated.PetSize = 200
	unrelated.PetReducedMotion = true
	if buddyPetAssetChanged(base, unrelated) {
		t.Fatal("unrelated pet settings triggered an asset refresh")
	}
}

// The buddy can only show the GUI pet when the render pipeline produces
// visible, correctly sized frames. An all-transparent asset would look exactly
// like a failed transfer on the device.
func TestDevicePetAssetForBuddyHasVisibleFrames(t *testing.T) {
	a := &App{}
	asset := a.devicePetAssetForConfig(corelib.AppConfig{})
	if asset.Width <= 0 || asset.Height <= 0 || asset.Data == "" {
		t.Fatal("default GUI pet produced no asset")
	}
	encoded := append([]string{asset.Data}, asset.Frames...)
	frames := 0
	minX, minY, maxX, maxY := protocol.AssetWidth, protocol.AssetHeight, 0, 0
	for _, item := range encoded {
		raw, err := base64.StdEncoding.DecodeString(item)
		if err != nil || len(raw) != asset.Width*asset.Height*3 {
			continue
		}
		small := downscalePetRGB565A8(raw, asset.Width, asset.Height,
			protocol.AssetWidth, protocol.AssetHeight)
		visible := 0
		for y := 0; y < protocol.AssetHeight; y++ {
			for x := 0; x < protocol.AssetWidth; x++ {
				if small[(y*protocol.AssetWidth+x)*3+2] == 0 {
					continue
				}
				visible++
				if x < minX {
					minX = x
				}
				if x > maxX {
					maxX = x
				}
				if y < minY {
					minY = y
				}
				if y > maxY {
					maxY = y
				}
			}
		}
		if visible == 0 {
			t.Fatalf("frame %d is fully transparent", frames)
		}
		frames++
	}
	if frames == 0 {
		t.Fatal("no usable frames")
	}
	t.Logf("frames=%d source=%dx%d content bbox x=%d..%d y=%d..%d",
		frames, asset.Width, asset.Height, minX, maxX, minY, maxY)
}
