package ocr

import (
	"image"
	"math"
)

// cropBox perspective-warps the quadrilateral box out of src into an upright
// rectangle, mirroring PaddleOCR's get_rotate_crop_image: output size from
// the longer horizontal/vertical edge lengths, cv2.getPerspectiveTransform +
// warpPerspective with BORDER_REPLICATE. Boxes taller than wide (ratio >=
// 1.5) are rotated 90° counterclockwise like the reference does.
func cropBox(src *image.RGBA, box [4][2]float32) *image.RGBA {
	return cropBoxS(src, box, nil)
}

// cropBoxS is cropBox with an optional per-Engine scratch; the returned
// image aliases scratch memory when sc is non-nil and is only valid until
// the next scratch-using call (Engine.Recognize consumes it immediately).
func cropBoxS(src *image.RGBA, box [4][2]float32, sc *engineScratch) *image.RGBA {
	w, h, rotate := cropOutputSize(box)

	srcPts := [4]point{
		{float64(box[0][0]), float64(box[0][1])},
		{float64(box[1][0]), float64(box[1][1])},
		{float64(box[2][0]), float64(box[2][1])},
		{float64(box[3][0]), float64(box[3][1])},
	}
	dstPts := [4]point{
		{0, 0},
		{float64(w), 0},
		{float64(w), float64(h)},
		{0, float64(h)},
	}
	hm := homography(srcPts, dstPts)
	out := warpPerspectiveS(src, hm, w, h, sc)

	if rotate {
		out = rotate90CCWS(out, sc)
	}
	return out
}

func cropOutputSize(box [4][2]float32) (w, h int, rotate bool) {
	dist := func(a, b [2]float32) float64 {
		return math.Hypot(float64(a[0]-b[0]), float64(a[1]-b[1]))
	}
	w = int(math.Max(dist(box[0], box[1]), dist(box[2], box[3])))
	h = int(math.Max(dist(box[0], box[3]), dist(box[1], box[2])))
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return w, h, float64(h)/float64(w) >= 1.5
}

func recWidthForBox(box [4][2]float32) int {
	w, h, rotate := cropOutputSize(box)
	if rotate {
		w, h = h, w
	}
	rw := int(math.Ceil(float64(recHeight) * float64(w) / float64(h)))
	if rw < 1 {
		return 1
	}
	if rw > recMaxWidth {
		return recMaxWidth
	}
	return rw
}

// mat3 is a row-major 3x3 matrix.
type mat3 [9]float64

// homography solves the 3x3 projective transform mapping src to dst (4
// correspondences) via an 8x8 linear system with partial pivoting.
func homography(src, dst [4]point) mat3 {
	var a [8][9]float64
	for i := 0; i < 4; i++ {
		x, y := src[i].x, src[i].y
		u, v := dst[i].x, dst[i].y
		r := i * 2
		a[r] = [9]float64{x, y, 1, 0, 0, 0, -u * x, -u * y, u}
		a[r+1] = [9]float64{0, 0, 0, x, y, 1, -v * x, -v * y, v}
	}
	// Gaussian elimination with partial pivoting.
	for col := 0; col < 8; col++ {
		piv := col
		for r := col + 1; r < 8; r++ {
			if math.Abs(a[r][col]) > math.Abs(a[piv][col]) {
				piv = r
			}
		}
		a[col], a[piv] = a[piv], a[col]
		if math.Abs(a[col][col]) < 1e-12 {
			continue
		}
		for r := 0; r < 8; r++ {
			if r == col {
				continue
			}
			f := a[r][col] / a[col][col]
			for c := col; c < 9; c++ {
				a[r][c] -= f * a[col][c]
			}
		}
	}
	var h mat3
	for i := 0; i < 8; i++ {
		if math.Abs(a[i][i]) > 1e-12 {
			h[i] = a[i][8] / a[i][i]
		}
	}
	h[8] = 1
	return h
}

// apply maps (x,y) through the homography.
func (h mat3) apply(x, y float64) (float64, float64) {
	d := h[6]*x + h[7]*y + h[8]
	if math.Abs(d) < 1e-12 {
		d = 1e-12
	}
	return (h[0]*x + h[1]*y + h[2]) / d, (h[3]*x + h[4]*y + h[5]) / d
}

// warpPerspective produces a w x h image by inverse-warping dst pixels into
// src with bilinear sampling; out-of-bounds samples replicate the border
// (OpenCV BORDER_REPLICATE).
func warpPerspective(src *image.RGBA, fwd mat3, w, h int) *image.RGBA {
	return warpPerspectiveS(src, fwd, w, h, nil)
}

// warpPerspectiveS is warpPerspective with an optional scratch target.
func warpPerspectiveS(src *image.RGBA, fwd mat3, w, h int, sc *engineScratch) *image.RGBA {
	inv := invert3x3(fwd)
	var dst *image.RGBA
	if sc != nil {
		dst = rgbaScratch(&sc.crop, w, h)
	} else {
		dst = image.NewRGBA(image.Rect(0, 0, w, h))
	}
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// +0.5: OpenCV maps pixel centers.
			sx, sy := inv.apply(float64(x)+0.5, float64(y)+0.5)
			sx -= 0.5
			sy -= 0.5
			r, g, b := bilinearBGR(src, sx, sy, sw, sh)
			o := y*dst.Stride + x*4
			dst.Pix[o] = r
			dst.Pix[o+1] = g
			dst.Pix[o+2] = b
			dst.Pix[o+3] = 255
		}
	}
	return dst
}

// bilinearBGR samples src at fractional (x, y) with border clamping,
// returning R, G, B.
func bilinearBGR(src *image.RGBA, x, y float64, w, h int) (uint8, uint8, uint8) {
	x = min(max(x, 0), float64(w-1))
	y = min(max(y, 0), float64(h-1))
	x0 := int(x)
	y0 := int(y)
	x1 := min(x0+1, w-1)
	y1 := min(y0+1, h-1)
	fx := x - float64(x0)
	fy := y - float64(y0)

	px := func(px, py int) (float64, float64, float64) {
		o := py*src.Stride + px*4
		return float64(src.Pix[o]), float64(src.Pix[o+1]), float64(src.Pix[o+2])
	}
	lerp := func(a, b, t float64) float64 { return a + (b-a)*t }
	r00, g00, b00 := px(x0, y0)
	r10, g10, b10 := px(x1, y0)
	r01, g01, b01 := px(x0, y1)
	r11, g11, b11 := px(x1, y1)
	r := lerp(lerp(r00, r10, fx), lerp(r01, r11, fx), fy)
	g := lerp(lerp(g00, g10, fx), lerp(g01, g11, fx), fy)
	b := lerp(lerp(b00, b10, fx), lerp(b01, b11, fx), fy)
	clamp := func(v float64) uint8 {
		return uint8(min(max(int(v+0.5), 0), 255))
	}
	return clamp(r), clamp(g), clamp(b)
}

// invert3x3 inverts a 3x3 matrix stored row-major in [9]float64.
func invert3x3(m mat3) mat3 {
	det := m[0]*(m[4]*m[8]-m[5]*m[7]) - m[1]*(m[3]*m[8]-m[5]*m[6]) + m[2]*(m[3]*m[7]-m[4]*m[6])
	if math.Abs(det) < 1e-12 {
		return mat3{1, 0, 0, 0, 1, 0, 0, 0, 1}
	}
	inv := mat3{
		m[4]*m[8] - m[5]*m[7], m[2]*m[7] - m[1]*m[8], m[1]*m[5] - m[2]*m[4],
		m[5]*m[6] - m[3]*m[8], m[0]*m[8] - m[2]*m[6], m[2]*m[3] - m[0]*m[5],
		m[3]*m[7] - m[4]*m[6], m[1]*m[6] - m[0]*m[7], m[0]*m[4] - m[1]*m[3],
	}
	for i := range inv {
		inv[i] /= det
	}
	return inv
}

// rotate90CCW rotates an image 90° counterclockwise (PaddleOCR rotates tall
// crops so text reads horizontally).
func rotate90CCW(src *image.RGBA) *image.RGBA {
	return rotate90CCWS(src, nil)
}

// rotate90CCWS is rotate90CCW with an optional scratch target.
func rotate90CCWS(src *image.RGBA, sc *engineScratch) *image.RGBA {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	var dst *image.RGBA
	if sc != nil {
		dst = rgbaScratch(&sc.rot, h, w)
	} else {
		dst = image.NewRGBA(image.Rect(0, 0, h, w))
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(h-1-y, x, src.At(x, y))
		}
	}
	return dst
}
