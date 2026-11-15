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

func TestDevicePetCharacterFramesStrollInsideTheFrame(t *testing.T) {
	reg := petpack.NewRegistry(t.TempDir(), petpack.BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	frames, frameMS := devicePetFramesForPack(reg, petpack.DefaultPackID, petpack.VariantDefault, 1, nil)
	if len(frames) != devicePetFrameCount {
		t.Fatalf("rendered frames=%d want=%d", len(frames), devicePetFrameCount)
	}
	if frameMS != devicePetRouteFrameMS {
		t.Fatalf("route frame ms=%d want=%d", frameMS, devicePetRouteFrameMS)
	}
	again, _ := devicePetFramesForPack(reg, petpack.DefaultPackID, petpack.VariantDefault, 1, nil)
	if len(again) != len(frames) {
		t.Fatalf("second render frames=%d want=%d", len(again), len(frames))
	}
	for index := range frames {
		if !bytes.Equal(devicePetRGB565A8(frames[index]), devicePetRGB565A8(again[index])) {
			t.Fatalf("frame %d is not deterministic", index)
		}
	}

	// The stroll is one clockwise lap. The last frame is one step from the
	// first, and the opaque body stays inside the round screen the whole way.
	centersX := make([]float64, len(frames))
	centersY := make([]float64, len(frames))
	for index, frame := range frames {
		box, _ := devicePetTestBounds(frame)
		if !box.ok {
			t.Fatalf("frame %d has no visible pet", index)
		}
		if box.minX < devicePetTrackMargin || box.minY < devicePetTrackMargin ||
			box.maxX > devicePetAssetWidth-1-devicePetTrackMargin ||
			box.maxY > devicePetAssetHeight-1-devicePetTrackMargin {
			t.Fatalf("frame %d leaves the safe area: %+v", index, box)
		}
		centersX[index] = float64(box.minX+box.maxX) / 2
		centersY[index] = float64(box.minY+box.maxY) / 2
	}
	minX, maxX := centersX[0], centersX[0]
	minY, maxY := centersY[0], centersY[0]
	farthest := 0.0
	for index := range centersX {
		if centersX[index] < minX {
			minX = centersX[index]
		}
		if centersX[index] > maxX {
			maxX = centersX[index]
		}
		if centersY[index] < minY {
			minY = centersY[index]
		}
		if centersY[index] > maxY {
			maxY = centersY[index]
		}
		dx := centersX[index] - centersX[0]
		dy := centersY[index] - centersY[0]
		if dist := math.Hypot(dx, dy); dist > farthest {
			farthest = dist
		}
	}
	spanX := maxX - minX
	spanY := maxY - minY
	if spanX < 24 {
		t.Fatalf("stroll is too narrow: spanX=%.1f centersX=%v", spanX, centersX)
	}
	if spanY < 16 {
		t.Fatalf("stroll is too short: spanY=%.1f centersY=%v", spanY, centersY)
	}
	maxStep := 0.0
	for index := range centersX {
		next := (index + 1) % len(centersX)
		step := math.Hypot(centersX[next]-centersX[index], centersY[next]-centersY[index])
		if step > maxStep {
			maxStep = step
		}
	}
	if limit := math.Max(spanX, spanY) * 0.75; maxStep > limit {
		t.Fatalf("stroll jumps %.1f (limit %.1f) centersX=%v centersY=%v", maxStep, limit, centersX, centersY)
	}
	closing := math.Hypot(centersX[len(centersX)-1]-centersX[0], centersY[len(centersY)-1]-centersY[0])
	if closing > farthest*0.55 {
		t.Fatalf("stroll does not return near the start: closing=%.1f farthest=%.1f centersX=%v centersY=%v", closing, farthest, centersX, centersY)
	}
	distinct := 0
	first := devicePetRGB565A8(frames[0])
	for _, frame := range frames[1:] {
		if !bytes.Equal(first, devicePetRGB565A8(frame)) {
			distinct++
		}
	}
	if distinct < 6 {
		t.Fatalf("stroll collapsed to %d distinct frames", distinct)
	}
	t.Logf("centersX=%v centersY=%v", centersX, centersY)
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

func TestDevicePetReelKeepsOneTranslation(t *testing.T) {
	narrow := image.NewNRGBA(image.Rect(0, 0, devicePetAssetWidth, devicePetAssetHeight))
	wide := image.NewNRGBA(image.Rect(0, 0, devicePetAssetWidth, devicePetAssetHeight))
	paint := func(dst *image.NRGBA, x0, x1 int) {
		for y := 80; y < 160; y++ {
			for x := x0; x < x1; x++ {
				dst.SetNRGBA(x, y, color.NRGBA{R: 240, G: 180, B: 90, A: 255})
			}
		}
	}
	paint(narrow, 90, 130)
	paint(wide, 70, 170)
	mark := color.NRGBA{R: 20, G: 20, B: 220, A: 255}
	narrow.SetNRGBA(100, 120, mark)
	wide.SetNRGBA(100, 120, mark)
	shots := []devicePetShot{
		{alongX: 0.5, alongY: -0.25},
		{alongX: 0.5, alongY: -0.25},
	}
	got := layoutDevicePetReel([]*image.NRGBA{narrow, wide}, shots, 1, math.Inf(1))
	find := func(frame image.Image) (int, int) {
		src, ok := frame.(*image.NRGBA)
		if !ok || src == nil {
			return -1, -1
		}
		for y := 0; y < src.Bounds().Dy(); y++ {
			for x := 0; x < src.Bounds().Dx(); x++ {
				p := src.NRGBAAt(x, y)
				if p.A > 200 && p.B > 180 && p.R < 40 {
					return x, y
				}
			}
		}
		return -1, -1
	}
	ax, ay := find(got[0])
	bx, by := find(got[1])
	if ax < 0 || bx < 0 {
		t.Fatal("reel lost the pose mark")
	}
	if ax != bx || ay != by {
		t.Fatalf("gait poses were shifted apart: (%d,%d) vs (%d,%d)", ax, ay, bx, by)
	}
}

func TestDevicePetLayoutKeepsTravelInsideTheFrame(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, devicePetAssetWidth, devicePetAssetHeight))
	for y := 80; y < 200; y++ {
		for x := 90; x < 150; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: 240, G: 180, B: 90, A: 255})
		}
	}
	rest := petOpaqueBoxOf(layoutDevicePetFrame(src, 1, 0, 0, false))
	far := petOpaqueBoxOf(layoutDevicePetFrame(src, 1, 1, 0, false))
	down := petOpaqueBoxOf(layoutDevicePetFrame(src, 1, 0, 1, false))
	turned := petOpaqueBoxOf(layoutDevicePetFrame(src, 1, 0, 0, true))
	if !rest.ok || !far.ok || !down.ok || !turned.ok {
		t.Fatal("layout dropped the pet")
	}
	if far.minX <= rest.minX {
		t.Fatalf("alongX=1 did not move right: rest=%+v far=%+v", rest, far)
	}
	if down.minY <= rest.minY {
		t.Fatalf("alongY=1 did not move down: rest=%+v down=%+v", rest, down)
	}
	if down.maxY > devicePetAssetHeight-1-devicePetTrackMargin || down.minY < devicePetTrackMargin {
		t.Fatalf("downward stroll left the frame: %+v", down)
	}
	if far.maxX > devicePetAssetWidth-1-devicePetTrackMargin || far.minX < devicePetTrackMargin {
		t.Fatalf("route walked off the frame: %+v", far)
	}
	// A dot on the blob's left side lands on the right side after the turn.
	// Centering keeps the bounding box in the same place, so the box itself
	// does not show the mirror.
	marked := image.NewNRGBA(src.Bounds())
	copy(marked.Pix, src.Pix)
	marked.SetNRGBA(92, 140, color.NRGBA{R: 20, G: 20, B: 220, A: 255})
	turnedMark := layoutDevicePetFrame(marked, 1, 0, 0, true)
	markX := -1
	for y := 0; y < turnedMark.Bounds().Dy(); y++ {
		for x := 0; x < turnedMark.Bounds().Dx(); x++ {
			p := turnedMark.NRGBAAt(x, y)
			if p.A > 200 && p.B > 180 && p.R < 40 {
				markX = x
			}
		}
	}
	if markX < 0 {
		t.Fatal("turned frame lost the facing mark")
	}
	turnedBox := petOpaqueBoxOf(turnedMark)
	if markX < (turnedBox.minX+turnedBox.maxX)/2 {
		t.Fatalf("turn kept the mark on the left: x=%d box=%+v", markX, turnedBox)
	}
	if turned.maxX > devicePetAssetWidth-1-devicePetTrackMargin || turned.minX < devicePetTrackMargin {
		t.Fatalf("turned pet left the frame: %+v", turned)
	}
}

func TestDevicePetGrowthKeepsFeetInsideTheFrame(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, devicePetAssetWidth, devicePetAssetHeight))
	for y := 200; y < 250; y++ {
		for x := 100; x < 150; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: 240, G: 180, B: 90, A: 255})
		}
	}
	same := devicePetApplyGrowth([]image.Image{src}, 0)
	if same[0] != src {
		t.Fatal("an unset rearing scale rewrote the frame")
	}
	base := petOpaqueBoxOf(src)
	grown := petOpaqueBoxOf(devicePetNRGBA(devicePetApplyGrowth([]image.Image{src}, 1.5)[0]))
	shrunk := petOpaqueBoxOf(devicePetNRGBA(devicePetApplyGrowth([]image.Image{src}, 0.5)[0]))
	limit := devicePetAssetHeight - 1 - devicePetTrackMargin
	if !grown.ok || grown.maxY > limit || grown.minY < devicePetTrackMargin || grown.maxX > devicePetAssetWidth-1-devicePetTrackMargin {
		t.Fatalf("grown pet left the frame: %+v", grown)
	}
	grownArea := (grown.maxX - grown.minX + 1) * (grown.maxY - grown.minY + 1)
	baseArea := (base.maxX - base.minX + 1) * (base.maxY - base.minY + 1)
	if grownArea <= baseArea {
		t.Fatalf("growth did not enlarge the pet: base %+v grown %+v", base, grown)
	}
	grownCenter := float64(grown.minY+grown.maxY) / 2
	if grownCenter < 180 {
		t.Fatalf("growth recentered a bottom-standing pet: %+v", grown)
	}
	shrunkArea := (shrunk.maxX - shrunk.minX + 1) * (shrunk.maxY - shrunk.minY + 1)
	shrunkCenter := float64(shrunk.minY+shrunk.maxY) / 2
	if !shrunk.ok || shrunkArea >= baseArea || shrunkCenter < 180 {
		t.Fatalf("shrink left the body: base %+v shrunk %+v", base, shrunk)
	}
}

func TestDevicePetCharacterGrowthEnlargesTheStroll(t *testing.T) {
	reg := petpack.NewRegistry(t.TempDir(), petpack.BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	base, baseMS := devicePetFramesForPack(reg, petpack.DefaultPackID, petpack.VariantDefault, 1, nil)
	grown, grownMS := devicePetFramesForPack(reg, petpack.DefaultPackID, petpack.VariantDefault, 1.18, nil)
	if len(base) != devicePetFrameCount || len(grown) != devicePetFrameCount {
		t.Fatalf("frames base=%d grown=%d", len(base), len(grown))
	}
	if baseMS != devicePetRouteFrameMS || grownMS != devicePetRouteFrameMS {
		t.Fatalf("frame ms base=%d grown=%d", baseMS, grownMS)
	}
	baseBox, _ := devicePetTestBounds(base[0])
	grownBox, _ := devicePetTestBounds(grown[0])
	baseArea := (baseBox.maxX - baseBox.minX + 1) * (baseBox.maxY - baseBox.minY + 1)
	grownArea := (grownBox.maxX - grownBox.minX + 1) * (grownBox.maxY - grownBox.minY + 1)
	if !grownBox.ok || grownArea <= baseArea*105/100 {
		t.Fatalf("settled scale did not enlarge the stroll: base %+v area %d grown %+v area %d", baseBox, baseArea, grownBox, grownArea)
	}
	for index, frame := range grown {
		box, _ := devicePetTestBounds(frame)
		if !box.ok || box.minX < devicePetTrackMargin || box.minY < devicePetTrackMargin ||
			box.maxX > devicePetAssetWidth-1-devicePetTrackMargin ||
			box.maxY > devicePetAssetHeight-1-devicePetTrackMargin {
			t.Fatalf("grown frame %d left the safe area: %+v", index, box)
		}
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
