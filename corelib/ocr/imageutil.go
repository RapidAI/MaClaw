package ocr

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"strings"

	xdraw "golang.org/x/image/draw"
)

// DefaultMaxLongEdge caps the longest side of images prepared for OCR when
// the caller passes maxEdge <= 0. Matches the historical sidecar behavior.
const DefaultMaxLongEdge = 2560

// PrepareImageBase64 decodes a PNG/JPEG base64 image and, when the longest
// edge exceeds maxEdge, downscales it for OCR. Returns re-encoded PNG base64
// and scale factors (orig/new) so bboxes can be mapped back to original
// coordinates.
//
// Fast path: when the image is already under maxEdge, only DecodeConfig is
// used (no full pixel decode / re-encode).
//
// Migrated from corelib/browser/ocr_rapidocr.go (prepareOCRImageBase64).
func PrepareImageBase64(pngBase64 string, maxEdge int) (outB64 string, scaleX, scaleY float64, err error) {
	if maxEdge <= 0 {
		maxEdge = DefaultMaxLongEdge
	}
	raw, err := decodeImageBytes(pngBase64)
	if err != nil {
		return "", 1, 1, err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return "", 1, 1, fmt.Errorf("decode image config: %w", err)
	}
	w, h := cfg.Width, cfg.Height
	if w <= 0 || h <= 0 {
		return "", 1, 1, fmt.Errorf("invalid image size %dx%d", w, h)
	}
	// Guard against decompression bombs: a tiny PNG can declare huge
	// dimensions and the full decode below would allocate w*h pixels.
	// 50 MP comfortably covers 8K screenshots (33 MP).
	if int64(w)*int64(h) > 50_000_000 {
		return "", 1, 1, fmt.Errorf("image too large: %dx%d exceeds 50MP limit", w, h)
	}
	long := max(w, h)
	if long <= maxEdge {
		// Already small enough — keep pixels as-is, but always hand the caller
		// pure std base64 (never a data: URI prefix).
		return pureStdBase64(pngBase64, raw), 1, 1, nil
	}

	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return "", 1, 1, fmt.Errorf("decode image: %w", err)
	}
	bounds := img.Bounds()
	// Prefer actual pixel bounds if they differ from config (rare, but safer).
	if bw, bh := bounds.Dx(), bounds.Dy(); bw > 0 && bh > 0 {
		w, h = bw, bh
		long = max(w, h)
		if long <= maxEdge {
			return pureStdBase64(pngBase64, raw), 1, 1, nil
		}
	}
	newW := w * maxEdge / long
	newH := h * maxEdge / long
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, bounds, xdraw.Src, nil)
	var buf bytes.Buffer
	// Pre-size roughly: RGBA PNG often compresses well; avoid tiny buffer growth.
	buf.Grow(newW * newH / 4)
	if err := png.Encode(&buf, dst); err != nil {
		return "", 1, 1, fmt.Errorf("encode resized png: %w", err)
	}
	scaleX = float64(w) / float64(newW)
	scaleY = float64(h) / float64(newH)
	return base64.StdEncoding.EncodeToString(buf.Bytes()), scaleX, scaleY, nil
}

// ScaleResults maps OCR results obtained on a downscaled image back to the
// original coordinate space. scaleX/scaleY are the orig/new factors returned
// by PrepareImageBase64.
//
// Migrated from corelib/browser/ocr_rapidocr.go (scaleOCRResults).
func ScaleResults(results []Result, scaleX, scaleY float64) []Result {
	if len(results) == 0 || (scaleX == 1 && scaleY == 1) {
		return results
	}
	if scaleX <= 0 {
		scaleX = 1
	}
	if scaleY <= 0 {
		scaleY = 1
	}
	out := make([]Result, len(results))
	for i, r := range results {
		bw := int(float64(r.BBox[2])*scaleX + 0.5)
		bh := int(float64(r.BBox[3])*scaleY + 0.5)
		if r.BBox[2] > 0 && bw < 1 {
			bw = 1
		}
		if r.BBox[3] > 0 && bh < 1 {
			bh = 1
		}
		out[i] = Result{
			Text:       r.Text,
			Confidence: r.Confidence,
			BBox: [4]int{
				int(float64(r.BBox[0])*scaleX + 0.5),
				int(float64(r.BBox[1])*scaleY + 0.5),
				bw,
				bh,
			},
			Box: r.Box,
		}
		for j := 0; j < 4; j++ {
			out[i].Box[j] = [2]int{
				int(float64(r.Box[j][0])*scaleX + 0.5),
				int(float64(r.Box[j][1])*scaleY + 0.5),
			}
		}
	}
	return out
}

func decodeImageBytes(b64 string) ([]byte, error) {
	b64 = stripBase64Payload(b64)
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(b64)
		if err != nil {
			return nil, fmt.Errorf("base64 decode: %w", err)
		}
	}
	return raw, nil
}

// stripBase64Payload removes data-URI prefixes and surrounding whitespace.
func stripBase64Payload(b64 string) string {
	b64 = strings.TrimSpace(b64)
	if i := strings.Index(b64, ","); i >= 0 && strings.Contains(strings.ToLower(b64[:i]), "base64") {
		return strings.TrimSpace(b64[i+1:])
	}
	return b64
}

// pureStdBase64 returns a consumer-safe std base64 string for raw image
// bytes. Strips data: URI prefixes. Reuses the cleaned payload when it is
// already std-padded base64 (matches EncodedLen) so the common screenshot
// path does not re-encode or re-decode multi-MB strings on every call.
func pureStdBase64(orig string, raw []byte) string {
	cleaned := stripBase64Payload(orig)
	// decodeImageBytes already verified `cleaned` decodes (std or raw-std).
	// Std encoding with padding has a fixed length; reuse without another decode.
	if len(cleaned) == base64.StdEncoding.EncodedLen(len(raw)) {
		return cleaned
	}
	return base64.StdEncoding.EncodeToString(raw)
}
