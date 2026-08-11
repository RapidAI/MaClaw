package ocr

import (
	"image"
	"math"

	"github.com/RapidAI/CodeClaw/corelib/onnxrt"
	xdraw "golang.org/x/image/draw"
)

// Detection preprocessing mirrors PaddleOCR's DetResizeForTest + NormalizeImage
// + ToCHWImage pipeline for the official PP-OCRv6 ONNX models:
//
//   - DecodeImage(img_mode=BGR): we feed B, G, R channel order.
//   - DetResizeForTest(null): paddlex resolves the defaults per model; for all
//     PP-OCRv6 det tiers that is limit_side_len=960, limit_type="max" (see
//     paddlex text_detection/predictor.py _get_text_det_resize_defaults, which
//     lists PP-OCRv6_*_det in _TEXT_DET_MAX_LIMIT_MODELS).
//   - NormalizeImage: scale 1/255, mean [0.485 0.456 0.406], std
//     [0.229 0.224 0.225], applied per channel in BGR order.
//   - ToCHWImage: HWC -> CHW.
//
// Recognition preprocessing mirrors RecResizeImg(image_shape=[3,48,320]) plus
// the normalization that the inference pipeline applies ((x/255 - 0.5) / 0.5,
// BGR). Verified empirically against onnxruntime + the PaddleOCR reference.

const (
	detLimitSideLen = 960  // limit_type="max": shrink only when max(h,w) exceeds this
	detMaxSideLimit = 4000 // absolute cap after limiting
	recHeight       = 48
	recMaxWidth     = 3200 // paddlex ResizeNormImage max_imgW
)

var (
	detMean = [3]float32{0.485, 0.456, 0.406}
	detStd  = [3]float32{0.229, 0.224, 0.225}
)

// detPreprocess converts an image into the det model input tensor
// [1,3,H,W] and reports the final resize ratios (resized/original).
func detPreprocess(img image.Image) (t *onnxrt.Tensor, ratioH, ratioW float64) {
	return detPreprocessS(img, nil)
}

// detPreprocessS is detPreprocess with an optional per-Engine scratch: when
// sc is non-nil the resize target and the tensor backing buffer are reused
// across calls. The returned tensor then aliases scratch memory and is only
// valid until the next scratch-using call — fine for Engine.Recognize,
// which feeds it straight into Graph.Run (Run does not retain inputs).
func detPreprocessS(img image.Image, sc *engineScratch) (t *onnxrt.Tensor, ratioH, ratioW float64) {
	src := toRGBAS(img, sc)
	w, h := src.Bounds().Dx(), src.Bounds().Dy()

	ratio := 1.0
	if max(h, w) > detLimitSideLen {
		ratio = float64(detLimitSideLen) / float64(max(h, w))
	}
	// Truncation here is deliberate: DetResizeForTest.resize_image_type0 uses
	// int(h*ratio) (floor), NOT round(); only the subsequent snap to a multiple
	// of 32 rounds. "Fixing" this to rounding would break parity with Paddle.
	rh, rw := int(float64(h)*ratio), int(float64(w)*ratio)
	if max(rh, rw) > detMaxSideLimit {
		r2 := float64(detMaxSideLimit) / float64(max(rh, rw))
		rh, rw = int(float64(rh)*r2), int(float64(rw)*r2)
	}
	// Python round() is round-half-even; keep that for box-level parity.
	rh = max(int(math.RoundToEven(float64(rh)/32))*32, 32)
	rw = max(int(math.RoundToEven(float64(rw)/32))*32, 32)

	var dst **image.RGBA
	if sc != nil {
		dst = &sc.detImg
	}
	resized := resizeRGBAS(src, rw, rh, dst)
	ratioH = float64(rh) / float64(h)
	ratioW = float64(rw) / float64(w)

	n := 3 * rh * rw
	var buf []float32
	if sc != nil {
		buf = f32Scratch(&sc.detIn, n)
	} else {
		buf = make([]float32, n)
	}
	t = &onnxrt.Tensor{Shape: []int{1, 3, rh, rw}, DType: onnxrt.DFloat32, F32: buf}
	pix := resized.Pix
	stride := resized.Stride
	for y := 0; y < rh; y++ {
		row := y * stride
		for x := 0; x < rw; x++ {
			o := row + x*4
			for c := 0; c < 3; c++ {
				// channel 0=B, 1=G, 2=R
				v := float32(pix[o+2-c]) * (1.0 / 255.0)
				t.F32[c*rh*rw+y*rw+x] = (v - detMean[c]) / detStd[c]
			}
		}
	}
	return t, ratioH, ratioW
}

// recPreprocess converts a cropped text line into the rec model input tensor
// [1,3,48,W'] where W' = ceil(48*w/h) capped at recMaxWidth.
func recPreprocess(crop *image.RGBA) *onnxrt.Tensor {
	return recPreprocessS(crop, nil)
}

// recWidth returns the model width for a cropped text line. Keeping it
// separate lets Engine group equal-width lines into one dynamic-batch rec run
// without padding a CTC sequence (padding can otherwise create characters).
func recWidth(crop *image.RGBA) int {
	w, h := crop.Bounds().Dx(), crop.Bounds().Dy()
	rw := int(math.Ceil(float64(recHeight) * float64(w) / float64(h)))
	if rw < 1 {
		return 1
	}
	if rw > recMaxWidth {
		return recMaxWidth
	}
	return rw
}

// recPreprocessS is recPreprocess with an optional per-Engine scratch; the
// returned tensor aliases scratch memory when sc is non-nil (same lifetime
// rule as detPreprocessS). A single max-grown buffer replaces per-width
// pooling: the Engine is serialized, so the buffer just grows to the widest
// line seen and serves every narrower one.
func recPreprocessS(crop *image.RGBA, sc *engineScratch) *onnxrt.Tensor {

	rw := recWidth(crop)

	var dst **image.RGBA
	if sc != nil {
		dst = &sc.recImg
	}
	resized := resizeRGBAS(crop, rw, recHeight, dst)

	n := 3 * recHeight * rw
	var buf []float32
	if sc != nil {
		buf = f32Scratch(&sc.recIn, n)
	} else {
		buf = make([]float32, n)
	}
	t := &onnxrt.Tensor{Shape: []int{1, 3, recHeight, rw}, DType: onnxrt.DFloat32, F32: buf}
	recPreprocessInto(resized, t.F32, rw)
	return t
}

// recPreprocessInto normalizes crop into one CHW [3,48,rw] model sample.
// dst must hold exactly at least 3*recHeight*rw floats. The caller controls
// the resize target, so it can reuse one image scratch while assembling a
// batch into disjoint tensor samples.
func recPreprocessInto(resized *image.RGBA, dst []float32, rw int) {
	need := 3 * recHeight * rw
	if len(dst) < need {
		panic("ocr: recPreprocessInto destination too short")
	}
	pix := resized.Pix
	stride := resized.Stride
	for y := 0; y < recHeight; y++ {
		row := y * stride
		for x := 0; x < rw; x++ {
			o := row + x*4
			for c := 0; c < 3; c++ {
				v := float32(pix[o+2-c])/255.0 - 0.5
				dst[c*recHeight*rw+y*rw+x] = v / 0.5
			}
		}
	}
}

// toRGBA converts any image.Image to a zero-based RGBA image.
func toRGBA(img image.Image) *image.RGBA {
	return toRGBAS(img, nil)
}

// toRGBAS is toRGBA with an optional scratch target for the conversion.
func toRGBAS(img image.Image, sc *engineScratch) *image.RGBA {
	if rgba, ok := img.(*image.RGBA); ok && rgba.Bounds().Min == (image.Point{}) {
		return rgba
	}
	b := img.Bounds()
	var dst *image.RGBA
	if sc != nil {
		dst = rgbaScratch(&sc.conv, b.Dx(), b.Dy())
	} else {
		dst = image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	}
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			dst.Set(x, y, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

// resizeRGBA scales src to exactly w x h with bilinear sampling
// (ApproxBiLinear matches OpenCV INTER_LINEAR closely enough for OCR).
func resizeRGBA(src *image.RGBA, w, h int) *image.RGBA {
	return resizeRGBAS(src, w, h, nil)
}

// resizeRGBAS is resizeRGBA with an optional scratch target: when dst is
// non-nil the scaled image is written into rgbaScratch(dst, w, h).
func resizeRGBAS(src *image.RGBA, w, h int, dst **image.RGBA) *image.RGBA {
	if src.Bounds().Dx() == w && src.Bounds().Dy() == h {
		return src
	}
	var out *image.RGBA
	if dst != nil {
		out = rgbaScratch(dst, w, h)
	} else {
		out = image.NewRGBA(image.Rect(0, 0, w, h))
	}
	xdraw.ApproxBiLinear.Scale(out, out.Bounds(), src, src.Bounds(), xdraw.Src, nil)
	return out
}
