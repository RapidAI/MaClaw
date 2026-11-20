package guiapp

import (
	"image"
	"image/color"
	"math"
)

// computerUseHaloMargin is the transparent padding around the window.
// Inset rims sit on the window, so the padding is zero.
func computerUseHaloMargin(inset bool) int {
	if inset {
		return 0
	}
	return computerUseHaloGap + computerUseHaloBand
}

// computerUseHaloLimit is the thickness of one border strip.
func computerUseHaloLimit(inset bool) int {
	if inset {
		return computerUseHaloBand
	}
	return computerUseHaloMargin(false)
}

// computerUseHaloDims is the full rim canvas for a window of winW×winH.
// The canvas is capped so a pathological window cannot allocate a huge DIB.
// The on-screen halo is four strips cut from this canvas, not the canvas itself.
func computerUseHaloDims(winW, winH int, inset bool) (ow, oh int, ok bool) {
	if winW < 64 || winH < 64 {
		return 0, 0, false
	}
	margin := computerUseHaloMargin(inset)
	ow, oh = winW+2*margin, winH+2*margin
	if ow <= 0 || oh <= 0 || ow > 8192 || oh > 8192 {
		return 0, 0, false
	}
	return ow, oh, true
}

// computerUseHaloStrip is one border of the rim, in full-canvas pixels.
// Strips tile the border and leave the window interior out, so a maximized
// window does not get a full-screen layered surface.
type computerUseHaloStrip struct {
	x, y, w, h int
}

func computerUseHaloStrips(winW, winH int, inset bool) (strips [4]computerUseHaloStrip, ok bool) {
	ow, oh, ok := computerUseHaloDims(winW, winH, inset)
	if !ok {
		return strips, false
	}
	limit := computerUseHaloLimit(inset)
	if limit <= 0 || ow <= 2*limit || oh <= 2*limit {
		return strips, false
	}
	strips[0] = computerUseHaloStrip{0, 0, ow, limit}
	strips[1] = computerUseHaloStrip{0, oh - limit, ow, limit}
	strips[2] = computerUseHaloStrip{0, limit, limit, oh - 2*limit}
	strips[3] = computerUseHaloStrip{ow - limit, limit, limit, oh - 2*limit}
	return strips, true
}

// computerUseHaloStripIndex maps a canvas pixel to a border strip.
// The interior of the window is not on any strip.
func computerUseHaloStripIndex(x, y, ow, oh, limit int) (strip, localX, localY int, ok bool) {
	if limit <= 0 || x < 0 || y < 0 || x >= ow || y >= oh {
		return 0, 0, 0, false
	}
	switch {
	case y < limit:
		return 0, x, y, true
	case y >= oh-limit:
		return 1, x, y - (oh - limit), true
	case x < limit:
		return 2, x, y - limit, true
	case x >= ow-limit:
		return 3, x - (ow - limit), y - limit, true
	default:
		return 0, 0, 0, false
	}
}

// computerUseHaloProject splits a horizontal run across the border strips.
func computerUseHaloProject(x, y, n, ow, oh, limit int, write func(strip, localX, localY, n int)) {
	if write == nil || n <= 0 {
		return
	}
	for n > 0 {
		strip, lx, ly, ok := computerUseHaloStripIndex(x, y, ow, oh, limit)
		if !ok {
			return
		}
		room := limit - lx
		if strip < 2 {
			room = ow - lx
		}
		if room <= 0 {
			return
		}
		take := n
		if take > room {
			take = room
		}
		write(strip, lx, ly, take)
		x += take
		n -= take
	}
}

// computerUseHaloImage paints a soft light-blue rim.
// inset draws the rim inside the window (maximized windows have no outer
// pixels on screen). Otherwise the rim starts computerUseHaloGap pixels
// outside the window and fades across computerUseHaloBand.
func computerUseHaloImage(winW, winH int, inset bool) *image.NRGBA {
	ow, oh, ok := computerUseHaloDims(winW, winH, inset)
	if !ok {
		return nil
	}
	img := image.NewNRGBA(image.Rect(0, 0, ow, oh))
	computerUseHaloPaint(winW, winH, inset, func(x, y, n int, r, g, b, a uint8) {
		off := y*img.Stride + x*4
		haloFill4(img.Pix[off:off+n*4], n, r, g, b, a)
	})
	return img
}

// computerUseHaloPaint calls span for each horizontal run of the rim.
// The interior is left untouched, so a zeroed destination stays clear.
// Straight-edge pixels use the edge distance; only the corner quarters need
// the rounded-rect hypotenuse. Windows are at least 64px, so the 12px corner
// radius is never clamped and that shortcut matches computerUseHaloSDF.
func computerUseHaloPaint(winW, winH int, inset bool, span func(x, y, n int, r, g, b, a uint8)) bool {
	ow, oh, ok := computerUseHaloDims(winW, winH, inset)
	if !ok || span == nil {
		return false
	}
	margin := computerUseHaloMargin(inset)
	limit := computerUseHaloLimit(inset)
	rad := float64(computerUseHaloRadius)
	winWf, winHf := float64(winW), float64(winH)

	var runX, runN int
	var rr, rg, rb, ra uint8
	var open bool
	flush := func(y int) {
		if open && runN > 0 && ra != 0 {
			span(runX, y, runN, rr, rg, rb, ra)
		}
		open = false
		runN = 0
	}
	put := func(y, x int, r, g, b, a uint8) {
		if open && x == runX+runN && r == rr && g == rg && b == rb && a == ra {
			runN++
			return
		}
		flush(y)
		runX, runN = x, 1
		rr, rg, rb, ra = r, g, b, a
		open = true
	}
	colorAt := func(x, y int) (uint8, uint8, uint8, uint8) {
		wx := float64(x) + 0.5 - float64(margin)
		wy := float64(y) + 0.5 - float64(margin)
		dL, dR := wx, winWf-wx
		dT, dB := wy, winHf-wy
		var sdf float64
		if math.Min(dL, dR) < rad && math.Min(dT, dB) < rad {
			// Corner quarter: the rounded corner pulls the boundary in.
			sdf = computerUseHaloSDF(wx, wy, winWf, winHf, rad)
		} else {
			nearest := dL
			if dR < nearest {
				nearest = dR
			}
			if dT < nearest {
				nearest = dT
			}
			if dB < nearest {
				nearest = dB
			}
			sdf = -nearest
		}
		return computerUseHaloSpanColor(sdf, inset)
	}
	paintPixels := func(y, x0, x1 int) {
		for x := x0; x < x1; x++ {
			cr, cg, cb, ca := colorAt(x, y)
			put(y, x, cr, cg, cb, ca)
		}
		flush(y)
	}
	// A long top or bottom edge is one color. Emit it as a single run so a
	// maximized window does not evaluate the falloff once per pixel.
	paintX := func(y, x0, x1 int) {
		if x0 >= x1 {
			return
		}
		wy := float64(y) + 0.5 - float64(margin)
		edge := wy
		if bottom := winHf - wy; bottom < edge {
			edge = bottom
		}
		bound := edge
		if rad > bound {
			bound = rad
		}
		start := int(math.Ceil(bound + float64(margin) - 0.5))
		end := int(math.Floor(winWf+float64(margin)-bound-0.5)) + 1
		if start < x0 {
			start = x0
		}
		if end > x1 {
			end = x1
		}
		if start >= end {
			paintPixels(y, x0, x1)
			return
		}
		if start > x0 {
			paintPixels(y, x0, start)
		}
		cr, cg, cb, ca := computerUseHaloSpanColor(-edge, inset)
		if ca != 0 {
			span(start, y, end-start, cr, cg, cb, ca)
		}
		if end < x1 {
			paintPixels(y, end, x1)
		}
	}

	for y := 0; y < oh; y++ {
		if y < limit || y >= oh-limit {
			paintX(y, 0, ow)
			continue
		}
		paintX(y, 0, limit)
		paintX(y, ow-limit, ow)
	}
	return true
}

func computerUseHaloSpanColor(sdf float64, inset bool) (r, g, b, a uint8) {
	cov := computerUseHaloOuterCoverage(sdf)
	if inset {
		cov = computerUseHaloInsetCoverage(sdf)
	}
	if cov <= 0 {
		return 0, 0, 0, 0
	}
	if cov > 1 {
		cov = 1
	}
	core := computerUseHaloCore(sdf, inset)
	return uint8(126 + core*70), uint8(198 + core*30), 255, uint8(float64(computerUseHaloPeak) * cov)
}

// haloFill4 writes n packed pixels of the four bytes given, in that order.
func haloFill4(row []byte, n int, b0, b1, b2, b3 byte) {
	if n <= 0 || len(row) < n*4 {
		return
	}
	row[0], row[1], row[2], row[3] = b0, b1, b2, b3
	filled := 4
	want := n * 4
	for filled < want {
		cp := filled
		if remain := want - filled; cp > remain {
			cp = remain
		}
		copy(row[filled:filled+cp], row[:cp])
		filled += cp
	}
}

func computerUseHaloCore(sdf float64, inset bool) float64 {
	depth := sdf - float64(computerUseHaloGap)
	if inset {
		depth = -sdf
	}
	if depth < 0 || depth >= 5 {
		return 0
	}
	return 1 - depth/5
}

func computerUseHaloOuterCoverage(sdf float64) float64 {
	inner := float64(computerUseHaloGap)
	outer := inner + float64(computerUseHaloBand)
	if sdf <= inner || sdf >= outer {
		return 0
	}
	depth := sdf - inner
	fade := 1 - depth/float64(computerUseHaloBand)
	core := computerUseHaloCore(sdf, false)
	return math.Min(1, 0.5*math.Pow(fade, 1.35)+0.85*core)
}

func computerUseHaloInsetCoverage(sdf float64) float64 {
	if sdf >= 0 || sdf <= -float64(computerUseHaloBand) {
		return 0
	}
	depth := -sdf
	fade := 1 - depth/float64(computerUseHaloBand)
	core := computerUseHaloCore(sdf, true)
	return math.Min(1, 0.45*math.Pow(fade, 1.3)+0.75*core)
}

// computerUseHaloSDF is the signed distance to a rounded rectangle of size
// winW×winH. Positive is outside. px, py are in window space (origin at the
// top-left corner).
func computerUseHaloSDF(px, py, winW, winH, radius float64) float64 {
	if radius < 0 {
		radius = 0
	}
	halfW := winW / 2
	halfH := winH / 2
	if radius > halfW {
		radius = halfW
	}
	if radius > halfH {
		radius = halfH
	}
	qx := math.Abs(px-halfW) - halfW + radius
	qy := math.Abs(py-halfH) - halfH + radius
	ox := math.Max(qx, 0)
	oy := math.Max(qy, 0)
	return math.Hypot(ox, oy) + math.Min(math.Max(qx, qy), 0) - radius
}

// computerUseHaloColorNear is a readable check used by tests: the rim is
// light blue, so blue exceeds green and green exceeds red.
func computerUseHaloIsLightBlue(c color.NRGBA) bool {
	return c.A > 0 && c.B > c.G && c.G > c.R
}
