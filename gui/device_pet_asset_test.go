package main

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"github.com/RapidAI/CodeClaw/gui/petpack"
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

func TestDevicePetSyntheticMotionKeepsBottomAnchor(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := 8; y < 32; y++ {
		for x := 6; x < 26; x++ {
			source.SetNRGBA(x, y, color.NRGBA{R: 80, G: 180, B: 240, A: 255})
		}
	}
	motion := devicePetMotionFrame(source)
	for x := 0; x < motion.Bounds().Dx(); x++ {
		if _, _, _, alpha := motion.At(x, motion.Bounds().Dy()-1).RGBA(); alpha != 0 {
			return
		}
	}
	t.Fatal("synthetic breathing moved the pet's bottom anchor off its baseline")
}

func TestEnsureDevicePetMotionFramesTurnsStaticSequenceIntoClosedLoop(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := 6; y < 30; y++ {
		for x := 4; x < 28; x++ {
			source.SetNRGBA(x, y, color.NRGBA{R: 60, G: 170, B: 240, A: 255})
		}
	}
	static := make([]image.Image, devicePetFrameCount)
	for index := range static {
		static[index] = source
	}
	frames := ensureDevicePetMotionFrames(static)
	if len(frames) != devicePetFrameCount {
		t.Fatalf("motion frames=%d want=%d", len(frames), devicePetFrameCount)
	}
	distinct := 0
	first := devicePetRGB565A8(frames[0])
	for _, frame := range frames[1:] {
		if !bytes.Equal(first, devicePetRGB565A8(frame)) {
			distinct++
		}
	}
	if distinct < devicePetFrameCount/2 {
		t.Fatalf("synthetic loop has only %d distinct motion phases", distinct)
	}
	if delta := perceptualDevicePetFrameDelta(frames[len(frames)-1], frames[0]); delta == 0 {
		t.Fatal("synthetic loop freezes on its wrap segment")
	}
}

func TestEnsureDevicePetMotionFramesPreservesAuthoredInitialHold(t *testing.T) {
	first := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	second := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	second.SetNRGBA(4, 4, color.NRGBA{R: 255, A: 255})
	frames := []image.Image{first, first, second}
	got := ensureDevicePetMotionFrames(frames)
	if got[1] != first {
		t.Fatal("an authored initial hold was replaced by synthetic motion")
	}
}

func TestDevicePetCharacterFramesUseDeterministicIdleLoop(t *testing.T) {
	reg := petpack.NewRegistry(t.TempDir(), petpack.BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	frames := devicePetRenderedFrames(reg, petpack.DefaultPackID, petpack.VariantDefault)
	if len(frames) != devicePetFrameCount {
		t.Fatalf("rendered frames=%d want=%d", len(frames), devicePetFrameCount)
	}
	resolved, err := reg.Resolve(petpack.DefaultPackID, petpack.VariantDefault)
	if err != nil {
		t.Fatal(err)
	}
	rig, err := petpack.NewRigRenderer(resolved)
	if err != nil || rig == nil {
		t.Fatalf("create deterministic rig renderer: renderer=%v err=%v", rig, err)
	}
	for index, frame := range frames {
		want := rig.Render(petpack.StateIdle, index*devicePetFrameMS, devicePetAssetWidth)
		if want == nil {
			t.Fatalf("expected idle-loop frame %d", index)
		}
		if gotPixels, wantPixels := devicePetRGB565A8(frame), devicePetRGB565A8(want); !bytes.Equal(gotPixels, wantPixels) {
			t.Fatalf("frame %d contains performance-director transition or non-looping layers", index)
		}
	}

	// A closed device loop includes the wrap from the final sample back to the
	// first. Guard against accidentally sampling a character entry/exit again:
	// that produces a wrap delta far larger than any authored adjacent segment
	// and is perceived as a periodic jump on the LCD.
	deltas := make([]uint64, len(frames))
	var adjacentMax uint64
	for index := range frames {
		deltas[index] = perceptualDevicePetFrameDelta(frames[index], frames[(index+1)%len(frames)])
		if index+1 < len(frames) && deltas[index] > adjacentMax {
			adjacentMax = deltas[index]
		}
	}
	if adjacentMax == 0 {
		t.Fatal("idle loop has no visible motion")
	}
	if wrap := deltas[len(deltas)-1]; wrap > adjacentMax+adjacentMax/4 {
		t.Fatalf("idle loop wrap delta=%d exceeds adjacent max=%d by more than 25%% (all deltas=%v)", wrap, adjacentMax, deltas)
	}
}

func perceptualDevicePetFrameDelta(first, second image.Image) uint64 {
	if first == nil || second == nil || first.Bounds().Size() != second.Bounds().Size() {
		return ^uint64(0)
	}
	abs := func(a, b uint32) uint64 {
		if a > b {
			return uint64(a - b)
		}
		return uint64(b - a)
	}
	var delta uint64
	firstBounds, secondBounds := first.Bounds(), second.Bounds()
	for y := 0; y < firstBounds.Dy(); y++ {
		for x := 0; x < firstBounds.Dx(); x++ {
			fr, fg, fb, fa := first.At(firstBounds.Min.X+x, firstBounds.Min.Y+y).RGBA()
			sr, sg, sb, sa := second.At(secondBounds.Min.X+x, secondBounds.Min.Y+y).RGBA()
			// RGBA channels are premultiplied, matching what reaches the LCD after
			// compositing and ignoring meaningless RGB beneath fully clear pixels.
			delta += abs(fr, sr) + abs(fg, sg) + abs(fb, sb) + abs(fa, sa)
		}
	}
	return delta
}
