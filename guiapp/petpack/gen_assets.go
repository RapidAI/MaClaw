//go:build ignore

// Generate official figurative PNG state frames for bundled packs.
// Run: go run gen_assets.go
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

type skinPalette struct {
	id     string
	accent color.NRGBA
	body   color.NRGBA
	head   color.NRGBA
	eye    color.NRGBA
}

func main() {
	skins := []skinPalette{
		{id: "clawmate", accent: nrgba(99, 102, 241, 255), body: nrgba(111, 125, 92, 255), head: nrgba(248, 250, 252, 255), eye: nrgba(45, 55, 72, 255)},
		{id: "mini-claw", accent: nrgba(37, 99, 235, 255), body: nrgba(147, 197, 253, 255), head: nrgba(239, 246, 255, 255), eye: nrgba(30, 58, 138, 255)},
		{id: "dev-claw", accent: nrgba(16, 185, 129, 255), body: nrgba(55, 65, 81, 255), head: nrgba(31, 41, 55, 255), eye: nrgba(52, 211, 153, 255)},
		{id: "focus-claw", accent: nrgba(167, 139, 250, 255), body: nrgba(196, 181, 253, 255), head: nrgba(245, 243, 255, 255), eye: nrgba(91, 33, 182, 255)},
	}
	states := []string{"idle", "listening", "thinking", "speaking", "done", "alert", "quiet"}
	root := "bundled"
	for _, s := range skins {
		dir := filepath.Join(root, s.id, "native")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			panic(err)
		}
		for i, st := range states {
			img := drawPet(256, s, st, i)
			path := filepath.Join(dir, st+".png")
			if err := writePNG(path, img); err != nil {
				panic(err)
			}
			fmt.Println("wrote", path)
		}
		// preview
		prev := drawPet(256, s, "idle", 0)
		if err := writePNG(filepath.Join(root, s.id, "preview.png"), prev); err != nil {
			panic(err)
		}
	}
}

func nrgba(r, g, b, a uint8) color.NRGBA { return color.NRGBA{R: r, G: g, B: b, A: a} }

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func drawPet(sz int, p skinPalette, state string, stateIdx int) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, sz, sz))
	// transparent bg
	cx, cy := float64(sz)/2, float64(sz)*0.48
	headR := float64(sz) * 0.28
	// soft ground shadow
	fillEllipse(out, cx, float64(sz)*0.82, float64(sz)*0.28, float64(sz)*0.06, nrgba(0, 0, 0, 40))
	// body
	bodyY := float64(sz) * 0.68
	fillEllipse(out, cx, bodyY, float64(sz)*0.22, float64(sz)*0.18, p.body)
	// arms (claw feel)
	armLift := 0.0
	switch state {
	case "listening":
		armLift = -8
	case "thinking":
		armLift = -4
	case "speaking":
		armLift = 6
	case "done":
		armLift = 10
	case "alert":
		armLift = -12
	}
	strokeLine(out, cx-headR*0.9, bodyY-10, cx-headR*1.35, bodyY+armLift-20, 10, p.accent)
	strokeLine(out, cx+headR*0.9, bodyY-10, cx+headR*1.35, bodyY+armLift-20, 10, p.accent)
	// feet
	fillEllipse(out, cx-28, float64(sz)*0.86, 18, 10, p.eye)
	fillEllipse(out, cx+28, float64(sz)*0.86, 18, 10, p.eye)
	// head with soft shade
	fillCircle(out, cx, cy, headR, p.head)
	// ear antenna
	strokeLine(out, cx, cy-headR+4, cx, cy-headR-28, 8, p.accent)
	fillCircle(out, cx, cy-headR-30, 8, p.accent)
	// face
	eyeOpen := 1.0
	mouthOpen := 0.2
	switch state {
	case "listening":
		eyeOpen = 1.15
		mouthOpen = 0.1
	case "thinking":
		eyeOpen = 0.75
		mouthOpen = 0.05
	case "speaking":
		mouthOpen = 0.7 + 0.1*math.Sin(float64(stateIdx))
	case "done":
		mouthOpen = 0.55
	case "alert":
		eyeOpen = 1.25
		mouthOpen = 0.35
	case "quiet":
		eyeOpen = 0.55
		mouthOpen = 0.08
	}
	eyeY := cy - 4
	eyeR := 9 * eyeOpen
	fillCircle(out, cx-22, eyeY, eyeR, p.eye)
	fillCircle(out, cx+22, eyeY, eyeR, p.eye)
	// eye highlight
	fillCircle(out, cx-18, eyeY-3, 3, nrgba(255, 255, 255, 220))
	fillCircle(out, cx+26, eyeY-3, 3, nrgba(255, 255, 255, 220))
	// cheeks
	if state == "speaking" || state == "done" {
		fillCircle(out, cx-34, cy+12, 7, nrgba(251, 146, 160, 120))
		fillCircle(out, cx+34, cy+12, 7, nrgba(251, 146, 160, 120))
	}
	// mouth
	mouthY := cy + 22
	if mouthOpen > 0.4 {
		fillEllipse(out, cx, mouthY, 14, 6+mouthOpen*8, p.eye)
	} else {
		strokeLine(out, cx-12, mouthY, cx+12, mouthY+mouthOpen*6, 4, p.eye)
	}
	// badge / signal
	fillRoundRect(out, int(cx-20), int(bodyY-6), 40, 22, p.accent)
	// state ring accent
	if state != "idle" && state != "quiet" {
		ring := p.accent
		ring.A = 90
		strokeCircle(out, cx, cy, headR+10+float64(stateIdx%3), 4, ring)
	}
	return out
}

func fillCircle(img *image.NRGBA, cx, cy, r float64, c color.NRGBA) {
	fillEllipse(img, cx, cy, r, r, c)
}

func fillEllipse(img *image.NRGBA, cx, cy, rx, ry float64, c color.NRGBA) {
	b := img.Bounds()
	minX := int(math.Floor(cx - rx - 1))
	maxX := int(math.Ceil(cx + rx + 1))
	minY := int(math.Floor(cy - ry - 1))
	maxY := int(math.Ceil(cy + ry + 1))
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			if x < b.Min.X || y < b.Min.Y || x >= b.Max.X || y >= b.Max.Y {
				continue
			}
			dx := (float64(x) + 0.5 - cx) / rx
			dy := (float64(y) + 0.5 - cy) / ry
			if dx*dx+dy*dy <= 1 {
				img.SetNRGBA(x, y, alphaOver(img.NRGBAAt(x, y), c))
			}
		}
	}
}

func strokeCircle(img *image.NRGBA, cx, cy, r, w float64, c color.NRGBA) {
	b := img.Bounds()
	minX := int(math.Floor(cx - r - w - 1))
	maxX := int(math.Ceil(cx + r + w + 1))
	minY := int(math.Floor(cy - r - w - 1))
	maxY := int(math.Ceil(cy + r + w + 1))
	outer := r + w/2
	inner := r - w/2
	if inner < 0 {
		inner = 0
	}
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			if x < b.Min.X || y < b.Min.Y || x >= b.Max.X || y >= b.Max.Y {
				continue
			}
			dx := float64(x) + 0.5 - cx
			dy := float64(y) + 0.5 - cy
			d := math.Sqrt(dx*dx + dy*dy)
			if d <= outer && d >= inner {
				img.SetNRGBA(x, y, alphaOver(img.NRGBAAt(x, y), c))
			}
		}
	}
}

func strokeLine(img *image.NRGBA, x0, y0, x1, y1, w float64, c color.NRGBA) {
	steps := int(math.Hypot(x1-x0, y1-y0)) + 1
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		x := x0 + (x1-x0)*t
		y := y0 + (y1-y0)*t
		fillCircle(img, x, y, w/2, c)
	}
}

func fillRoundRect(img *image.NRGBA, x, y, w, h int, c color.NRGBA) {
	for py := y; py < y+h; py++ {
		for px := x; px < x+w; px++ {
			if px < 0 || py < 0 || px >= img.Bounds().Max.X || py >= img.Bounds().Max.Y {
				continue
			}
			// soft corners
			cx := px
			if cx < x+4 {
				// left
			}
			img.SetNRGBA(px, py, alphaOver(img.NRGBAAt(px, py), c))
		}
	}
}

func alphaOver(dst, src color.NRGBA) color.NRGBA {
	if src.A == 0 {
		return dst
	}
	if src.A == 255 {
		return src
	}
	sa := float64(src.A) / 255
	da := float64(dst.A) / 255
	outA := sa + da*(1-sa)
	if outA <= 0 {
		return color.NRGBA{}
	}
	r := (float64(src.R)*sa + float64(dst.R)*da*(1-sa)) / outA
	g := (float64(src.G)*sa + float64(dst.G)*da*(1-sa)) / outA
	b := (float64(src.B)*sa + float64(dst.B)*da*(1-sa)) / outA
	return color.NRGBA{R: uint8(r + 0.5), G: uint8(g + 0.5), B: uint8(b + 0.5), A: uint8(outA*255 + 0.5)}
}
