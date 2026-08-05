package main

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

func TestDevicePetRGB565A8PreservesTransparency(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	source.SetNRGBA(0, 0, color.NRGBA{R: 255, G: 0, B: 0, A: 255})
	source.SetNRGBA(1, 0, color.NRGBA{R: 0, G: 255, B: 0, A: 0})

	pixels := devicePetRGB565A8(source)
	wantLen := devicePetAssetWidth * devicePetAssetHeight * 3
	if len(pixels) != wantLen {
		t.Fatalf("RGB565A8 bytes=%d want=%d", len(pixels), wantLen)
	}
	y := devicePetAssetHeight / 2
	left := (y*devicePetAssetWidth + devicePetAssetWidth/4 - 1) * 3
	right := (y*devicePetAssetWidth + devicePetAssetWidth*3/4 - 1) * 3
	if pixels[left] != 0x00 || pixels[left+1] != 0xf8 || pixels[left+2] != 0xff {
		t.Fatalf("opaque red pixel=%#v", pixels[left:left+3])
	}
	if pixels[right+2] != 0 {
		t.Fatalf("transparent pixel alpha=%d", pixels[right+2])
	}
}

func TestDevicePetAssetUsesPackNativeResolution(t *testing.T) {
	if devicePetAssetWidth != 256 || devicePetAssetHeight != 256 {
		t.Fatalf("pet asset dimensions=%dx%d want 256x256", devicePetAssetWidth, devicePetAssetHeight)
	}
	pixels := devicePetRGB565A8(image.NewNRGBA(image.Rect(0, 0, 256, 256)))
	if len(pixels) != 256*256*3 {
		t.Fatalf("RGB565A8 bytes=%d want=%d", len(pixels), 256*256*3)
	}
}

func TestDevicePetRGB565A8UsesFilteredScaling(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	source.SetNRGBA(0, 0, color.NRGBA{A: 255})
	source.SetNRGBA(1, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 255})

	pixels := devicePetRGB565A8(source)
	foundIntermediate := false
	y := devicePetAssetHeight / 2
	for x := 0; x < devicePetAssetWidth; x++ {
		i := (y*devicePetAssetWidth + x) * 3
		value := uint16(pixels[i]) | uint16(pixels[i+1])<<8
		if pixels[i+2] == 255 && value != 0 && value != 0xffff {
			foundIntermediate = true
			break
		}
	}
	if !foundIntermediate {
		t.Fatal("scaled black-to-white edge contains no filtered intermediate pixels")
	}
}

func TestEnsureDevicePetMotionFramesAddsDistinctSecondFrame(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := 6; y < 30; y++ {
		for x := 4; x < 28; x++ {
			source.SetNRGBA(x, y, color.NRGBA{R: 60, G: 170, B: 240, A: 255})
		}
	}
	frames := ensureDevicePetMotionFrames([]image.Image{source})
	if len(frames) != devicePetFrameCount {
		t.Fatalf("motion frames=%d want=%d", len(frames), devicePetFrameCount)
	}
	first := devicePetRGB565A8(frames[0])
	second := devicePetRGB565A8(frames[1])
	if bytes.Equal(first, second) {
		t.Fatal("generated motion frame is identical to the source frame")
	}
}
