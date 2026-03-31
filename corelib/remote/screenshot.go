package remote

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"math"
	"runtime/debug"
	"strings"
)

// pngMagicBytes is the 8-byte PNG file header signature.
var pngMagicBytes = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}

// ParseScreenshotOutput extracts and validates base64-encoded PNG data from
// the screenshot command's stdout output.
func ParseScreenshotOutput(stdout string) (string, error) {
	cleaned := strings.TrimPrefix(stdout, "\xEF\xBB\xBF")
	cleaned = strings.TrimSpace(cleaned)
	cleaned = strings.Join(strings.Fields(cleaned), "")
	cleaned = stripNonBase64(cleaned)

	if cleaned == "" {
		return "", fmt.Errorf("screenshot command produced no output")
	}

	decoded, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(cleaned, "="))
		if err != nil {
			preview := cleaned
			if len(preview) > 80 {
				preview = preview[:80] + "..."
			}
			return "", fmt.Errorf("invalid base64 encoding (len=%d, preview=%s)", len(cleaned), preview)
		}
	}

	if len(decoded) < len(pngMagicBytes) || !bytes.Equal(decoded[:len(pngMagicBytes)], pngMagicBytes) {
		return "", fmt.Errorf("output is not PNG (decoded %d bytes, header=%x)", len(decoded), safeHeader(decoded, 8))
	}

	canonical := base64.StdEncoding.EncodeToString(decoded)
	return canonical, nil
}

func stripNonBase64(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '+' || c == '/' || c == '=' {
			b.WriteByte(c)
		}
	}
	return b.String()
}

func safeHeader(data []byte, n int) []byte {
	if len(data) < n {
		return data
	}
	return data[:n]
}

const blankImageThreshold = 3

// IsBlankImage decodes a base64-encoded PNG and returns true if the image is
// effectively blank (all or nearly all black pixels).
func IsBlankImage(base64Data string) bool {
	decoded, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return false
	}
	img, err := png.Decode(bytes.NewReader(decoded))
	if err != nil {
		return false
	}
	bounds := img.Bounds()
	if bounds.Dx() == 0 || bounds.Dy() == 0 {
		return true
	}
	return isImageBlank(img, blankImageThreshold)
}

func isImageBlank(img image.Image, threshold uint32) bool {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	step := 1
	totalPixels := w * h
	if totalPixels > 10000 {
		step = int(isqrt(uint64(totalPixels / 10000)))
		if step < 1 {
			step = 1
		}
	}

	var totalBrightness uint64
	var count uint64

	for y := bounds.Min.Y; y < bounds.Max.Y; y += step {
		for x := bounds.Min.X; x < bounds.Max.X; x += step {
			r, g, b, _ := img.At(x, y).RGBA()
			brightness := (r>>8 + g>>8 + b>>8) / 3
			totalBrightness += uint64(brightness)
			count++
		}
	}

	if count == 0 {
		return true
	}
	avg := totalBrightness / count
	return avg <= uint64(threshold)
}

func isqrt(n uint64) uint64 {
	if n == 0 {
		return 0
	}
	x := n
	y := (x + 1) / 2
	for y < x {
		x = y
		y = (x + n/x) / 2
	}
	return x
}

// DownsizeScreenshotBase64 checks if the base64-encoded PNG data exceeds
// sizeLimit when decoded. If so, it scales it down proportionally.
func DownsizeScreenshotBase64(base64Data string, sizeLimit int) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return base64Data, err
	}
	if len(decoded) <= sizeLimit {
		return base64Data, nil
	}

	img, err := png.Decode(bytes.NewReader(decoded))
	if err != nil {
		return base64Data, fmt.Errorf("png decode for downsize: %w", err)
	}
	decoded = nil // free raw bytes — img holds the pixel data now

	bounds := img.Bounds()
	origW := bounds.Dx()
	origH := bounds.Dy()
	if origW == 0 || origH == 0 {
		return base64Data, nil
	}

	ratio := float64(sizeLimit) / float64(len(decoded)) * 0.85
	scale := math.Sqrt(ratio)
	if scale >= 1.0 {
		scale = 0.7
	}

	newW := int(float64(origW) * scale)
	newH := int(float64(origH) * scale)
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	for y := 0; y < newH; y++ {
		srcY := bounds.Min.Y + y*origH/newH
		for x := 0; x < newW; x++ {
			srcX := bounds.Min.X + x*origW/newW
			dst.Set(x, y, img.At(srcX, srcY))
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return base64Data, fmt.Errorf("png encode after downsize: %w", err)
	}

	result := base64.StdEncoding.EncodeToString(buf.Bytes())
	return result, nil
}

// IsBlankImageBytes checks if raw PNG bytes represent a blank (all-black) image.
// Use this when you already have decoded bytes to avoid a redundant base64 decode.
func IsBlankImageBytes(decoded []byte) bool {
	img, err := png.Decode(bytes.NewReader(decoded))
	if err != nil {
		return false
	}
	bounds := img.Bounds()
	if bounds.Dx() == 0 || bounds.Dy() == 0 {
		return true
	}
	return isImageBlank(img, blankImageThreshold)
}

// ParseScreenshotOutputOpt is an optimised version of ParseScreenshotOutput
// that also performs blank-image detection on the already-decoded bytes,
// avoiding a second base64-decode + PNG-decode round-trip.
//
// Returns (base64Data, isBlank, error).
func ParseScreenshotOutputOpt(stdout string) (string, bool, error) {
	cleaned := strings.TrimPrefix(stdout, "\xEF\xBB\xBF")
	cleaned = strings.TrimSpace(cleaned)
	cleaned = strings.Join(strings.Fields(cleaned), "")
	cleaned = stripNonBase64(cleaned)

	if cleaned == "" {
		return "", false, fmt.Errorf("screenshot command produced no output")
	}

	decoded, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(cleaned, "="))
		if err != nil {
			preview := cleaned
			if len(preview) > 80 {
				preview = preview[:80] + "..."
			}
			return "", false, fmt.Errorf("invalid base64 encoding (len=%d, preview=%s)", len(cleaned), preview)
		}
	}

	if len(decoded) < len(pngMagicBytes) || !bytes.Equal(decoded[:len(pngMagicBytes)], pngMagicBytes) {
		return "", false, fmt.Errorf("output is not PNG (decoded %d bytes, header=%x)", len(decoded), safeHeader(decoded, 8))
	}

	// Blank check on already-decoded bytes — no extra base64 round-trip.
	blank := IsBlankImageBytes(decoded)

	canonical := base64.StdEncoding.EncodeToString(decoded)
	return canonical, blank, nil
}

// ReleaseScreenshotMemory aggressively returns screenshot-related memory to
// the OS. Call this after the screenshot pipeline completes to avoid holding
// large image buffers until the next GC cycle (which can take minutes).
func ReleaseScreenshotMemory() {
	debug.FreeOSMemory()
}
