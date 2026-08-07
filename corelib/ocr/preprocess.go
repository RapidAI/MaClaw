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
	src := toRGBA(img)
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

	resized := resizeRGBA(src, rw, rh)
	ratioH = float64(rh) / float64(h)
	ratioW = float64(rw) / float64(w)

	t = onnxrt.NewFloat(1, 3, rh, rw)
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
	w, h := crop.Bounds().Dx(), crop.Bounds().Dy()
	rw := int(math.Ceil(float64(recHeight) * float64(w) / float64(h)))
	if rw < 1 {
		rw = 1
	}
	if rw > recMaxWidth {
		rw = recMaxWidth
	}
	resized := resizeRGBA(crop, rw, recHeight)

	t := onnxrt.NewFloat(1, 3, recHeight, rw)
	pix := resized.Pix
	stride := resized.Stride
	for y := 0; y < recHeight; y++ {
		row := y * stride
		for x := 0; x < rw; x++ {
			o := row + x*4
			for c := 0; c < 3; c++ {
				v := float32(pix[o+2-c])/255.0 - 0.5
				t.F32[c*recHeight*rw+y*rw+x] = v / 0.5
			}
		}
	}
	return t
}

// toRGBA converts any image.Image to a zero-based RGBA image.
func toRGBA(img image.Image) *image.RGBA {
	if rgba, ok := img.(*image.RGBA); ok && rgba.Bounds().Min == (image.Point{}) {
		return rgba
	}
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
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
	if src.Bounds().Dx() == w && src.Bounds().Dy() == h {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Src, nil)
	return dst
}
