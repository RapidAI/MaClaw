package ocr

import "image"

// Per-Engine scratch. Engine.Recognize calls are mutex-serialized, so one
// scratch set per Engine is safe and nothing is shared across Engines.
// The buffers below dominate the pipeline's non-onnxrt allocation churn on
// large screenshots (det input planes, RGBA resize/crop targets, the DB
// bitmap and contour label grid); reusing them across calls cuts the GC
// pressure without changing any result. Engine.Close drops the whole set so
// a Manager idle-unload actually frees the memory.
//
// Every consumer fully overwrites what it checks out (resize, warp and the
// normalize loops write each pixel/element), except where noted — those
// clear the buffer first. Pooled buffers never escape into returned
// results: det/rec input tensors die with the enclosing Graph.Run, and
// crops are consumed by recPreprocess immediately.
type engineScratch struct {
	conv     *image.RGBA // toRGBA target when the source isn't already RGBA
	detImg   *image.RGBA // detPreprocess resize target
	detIn    []float32   // detPreprocess input tensor backing (3*rh*rw)
	crop     *image.RGBA // cropBox warpPerspective output
	rot      *image.RGBA // cropBox 90° rotation output
	recImg   *image.RGBA // recPreprocess resize target (recHeight x rw)
	recIn    []float32   // recPreprocess input tensor backing (3*recHeight*rw)
	recBatch []float32   // same-width dynamic-batch rec input backing
	bitmap   []uint8     // dbPostProcess binarized prob map (cleared on reuse)
	labels   []int32     // findContours label grid (cleared on reuse)
	xs       []float64   // fillPoly scanline intersections
}

// release drops all pooled buffers so they can be GC'd.
func (s *engineScratch) release() { *s = engineScratch{} }

// rgbaScratch returns an RGBA image of exactly w x h backed by *p,
// reallocating only when the cached buffer is too small. The image is
// standard layout (zero-based Rect, Stride = 4*w, len(Pix) = 4*w*h), so it
// is interchangeable with image.NewRGBA output for draw/At/Set. Contents
// are unspecified; callers must overwrite every pixel.
func rgbaScratch(p **image.RGBA, w, h int) *image.RGBA {
	need := w * h * 4
	img := *p
	if img == nil || cap(img.Pix) < need {
		img = &image.RGBA{
			Pix:    make([]uint8, need),
			Stride: w * 4,
			Rect:   image.Rect(0, 0, w, h),
		}
		*p = img
		return img
	}
	img.Pix = img.Pix[:need]
	img.Stride = w * 4
	img.Rect = image.Rect(0, 0, w, h)
	return img
}

// f32Scratch returns a slice of length n backed by *p, growing it when the
// cached capacity is too small. Contents are unspecified.
func f32Scratch(p *[]float32, n int) []float32 {
	if cap(*p) < n {
		*p = make([]float32, n)
	}
	return (*p)[:n]
}

// u8Scratch is f32Scratch for bytes.
func u8Scratch(p *[]uint8, n int) []uint8 {
	if cap(*p) < n {
		*p = make([]uint8, n)
	}
	return (*p)[:n]
}

// i32Scratch is f32Scratch for int32.
func i32Scratch(p *[]int32, n int) []int32 {
	if cap(*p) < n {
		*p = make([]int32, n)
	}
	return (*p)[:n]
}
