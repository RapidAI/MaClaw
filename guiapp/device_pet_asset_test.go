package guiapp

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/RapidAI/CodeClaw/guiapp/petpack"
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

func TestDevicePetCharacterFramesShowRichActionsOnAVisibleRoute(t *testing.T) {
	reg := petpack.NewRegistry(t.TempDir(), petpack.BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	frames, frameMS := devicePetFramesForPack(reg, petpack.DefaultPackID, petpack.VariantDefault)
	if len(frames) != devicePetFrameCount {
		t.Fatalf("rendered frames=%d want=%d", len(frames), devicePetFrameCount)
	}
	if frameMS != devicePetRouteFrameMS {
		t.Fatalf("route frame ms=%d want=%d", frameMS, devicePetRouteFrameMS)
	}
	again, _ := devicePetFramesForPack(reg, petpack.DefaultPackID, petpack.VariantDefault)
	if len(again) != len(frames) {
		t.Fatalf("second render frames=%d want=%d", len(again), len(frames))
	}
	for index := range frames {
		if !bytes.Equal(devicePetRGB565A8(frames[index]), devicePetRGB565A8(again[index])) {
			t.Fatalf("frame %d is not deterministic", index)
		}
	}

	// Reel order for a pack that has the full action set: rest, walk out,
	// turn at the far edge, run back through center, then jump, bend, and sing
	// held on the resting anchor. The pet's opaque body stays inside the frame
	// the whole way, which is what the round buddy screen can show.
	centers := make([]float64, len(frames))
	var idleTop, jumpTop int
	for index, frame := range frames {
		box, top := devicePetTestBounds(frame)
		if !box.ok {
			t.Fatalf("frame %d has no visible pet", index)
		}
		if box.minX < devicePetTrackMargin || box.minY < devicePetTrackMargin ||
			box.maxX > devicePetAssetWidth-1-devicePetTrackMargin ||
			box.maxY > devicePetAssetHeight-1-devicePetTrackMargin {
			t.Fatalf("frame %d leaves the safe area: %+v", index, box)
		}
		centers[index] = float64(box.minX+box.maxX) / 2
		switch index {
		case 0:
			idleTop = top
		case 5:
			jumpTop = top
		}
	}
	if centers[1] <= centers[0]+8 {
		t.Fatalf("walk did not move right: centers=%v", centers)
	}
	if centers[2] <= centers[1] {
		t.Fatalf("walk did not reach the far edge: centers=%v", centers)
	}
	if centers[4] >= centers[2]-8 {
		t.Fatalf("run did not come back from the edge: centers=%v", centers)
	}
	if math.Abs(centers[4]-centers[0]) > 24 {
		t.Fatalf("route does not return near the resting anchor: centers=%v", centers)
	}
	if jumpTop >= idleTop-8 {
		t.Fatalf("jump top %d is not above idle top %d", jumpTop, idleTop)
	}
	distinct := 0
	first := devicePetRGB565A8(frames[0])
	for _, frame := range frames[1:] {
		if !bytes.Equal(first, devicePetRGB565A8(frame)) {
			distinct++
		}
	}
	if distinct < 6 {
		t.Fatalf("rich reel collapsed to %d distinct frames", distinct)
	}
	t.Logf("centers=%v idleTop=%d jumpTop=%d", centers, idleTop, jumpTop)
}

func devicePetTestBounds(frame image.Image) (petOpaqueBox, int) {
	if frame == nil {
		return petOpaqueBox{}, 0
	}
	bounds := frame.Bounds()
	canvas := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			r, g, b, a := frame.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			if a == 0 {
				continue
			}
			canvas.SetNRGBA(x, y, color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)})
		}
	}
	box := petOpaqueBoxOf(canvas)
	return box, box.minY
}

func TestLayoutDevicePetFrameKeepsTravelInsideTheFrame(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, devicePetAssetWidth, devicePetAssetHeight))
	for y := 80; y < 200; y++ {
		for x := 90; x < 150; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: 240, G: 180, B: 90, A: 255})
		}
	}
	rest := petOpaqueBoxOf(layoutDevicePetFrame(src, 1, 0, false))
	far := petOpaqueBoxOf(layoutDevicePetFrame(src, 1, 1, false))
	turned := petOpaqueBoxOf(layoutDevicePetFrame(src, 1, 0, true))
	if !rest.ok || !far.ok || !turned.ok {
		t.Fatal("layout dropped the pet")
	}
	if far.minX <= rest.minX {
		t.Fatalf("along=1 did not move right: rest=%+v far=%+v", rest, far)
	}
	if far.maxX > devicePetAssetWidth-1-devicePetTrackMargin || far.minX < devicePetTrackMargin {
		t.Fatalf("route walked off the frame: %+v", far)
	}
	// The blob sits left of center. Mirroring it across the canvas moves that
	// mass to the right, which is the turn at the edge of the route.
	if turned.minX <= rest.minX {
		t.Fatalf("turn did not mirror the silhouette: turned=%+v rest=%+v", turned, rest)
	}
	if turned.maxX > devicePetAssetWidth-1-devicePetTrackMargin || turned.minX < devicePetTrackMargin {
		t.Fatalf("turned pet left the frame: %+v", turned)
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
