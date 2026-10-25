package formula

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"io"
	"math"

	"github.com/RapidAI/CodeClaw/corelib/onnxrt"
	xdraw "golang.org/x/image/draw"
)

// Eval preprocessing matches Texo EvalMERImageProcessor: crop the white
// margin, resize so the short side is 384 (bilinear), thumbnail so the long
// side is 384 (bicubic; CatmullRom here), center-pad with black, then
// grayscale and (x/255 - mean) / std. The two-step sizes follow torchvision
// F.resize and Pillow thumbnail; a very wide intermediate is skipped.

const (
	canvas         = 384
	unimerMean     = 0.7931
	unimerStd      = 0.1738
	maxResizeSide  = 4096
	inkThreshold   = 200
	maxImageSide   = 12000
	maxImagePixels = 48_000_000
	// maxImageBytes matches the ocr_recognize encoded-input cap.
	maxImageBytes = 25 << 20
)

// DecodeImage reads a PNG, JPEG, or GIF. The encoded input is capped first.
// Width and height are then taken from the header before the bitmap is
// allocated, so a small file that declares a huge image is rejected instead
// of exhausting memory.
func DecodeImage(r io.Reader) (image.Image, error) {
	rs, err := imageReadSeeker(r)
	if err != nil {
		return nil, err
	}
	if err := rejectHugeEncoded(rs); err != nil {
		return nil, err
	}
	cfg, _, err := image.DecodeConfig(rs)
	if err != nil {
		return nil, fmt.Errorf("formula: decode image: %w", err)
	}
	if err := imageTooLarge(cfg.Width, cfg.Height); err != nil {
		return nil, err
	}
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("formula: decode image: %w", err)
	}
	img, _, err := image.Decode(rs)
	if err != nil {
		return nil, fmt.Errorf("formula: decode image: %w", err)
	}
	return img, nil
}

func imageReadSeeker(r io.Reader) (io.ReadSeeker, error) {
	if rs, ok := r.(io.ReadSeeker); ok {
		return rs, nil
	}
	data, err := io.ReadAll(io.LimitReader(r, maxImageBytes+1))
	if err != nil {
		return nil, fmt.Errorf("formula: read image: %w", err)
	}
	if int64(len(data)) > maxImageBytes {
		return nil, fmt.Errorf("formula: image is larger than %d bytes", maxImageBytes)
	}
	return bytes.NewReader(data), nil
}

func rejectHugeEncoded(rs io.ReadSeeker) error {
	cur, err := rs.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil
	}
	end, endErr := rs.Seek(0, io.SeekEnd)
	if _, err := rs.Seek(cur, io.SeekStart); err != nil {
		return fmt.Errorf("formula: decode image: %w", err)
	}
	if endErr != nil || end < cur {
		return nil
	}
	if end-cur > maxImageBytes {
		return fmt.Errorf("formula: image is %d bytes; limit is %d", end-cur, maxImageBytes)
	}
	return nil
}

func imageTooLarge(w, h int) error {
	if w < 1 || h < 1 {
		return fmt.Errorf("formula: empty image")
	}
	if w > maxImageSide || h > maxImageSide || w > maxImagePixels/h {
		return fmt.Errorf("formula: image is %d×%d; limit is %d px on a side and %d megapixels", w, h, maxImageSide, maxImagePixels/1_000_000)
	}
	return nil
}

func preprocess(img image.Image) (*onnxrt.Tensor, error) {
	if img == nil {
		return nil, fmt.Errorf("formula: nil image")
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if err := imageTooLarge(w, h); err != nil {
		return nil, err
	}
	src := toRGBA(img)
	cropped := cropMargin(src)
	cw, ch := cropped.Bounds().Dx(), cropped.Bounds().Dy()
	if cw == 0 || ch == 0 {
		return nil, fmt.Errorf("formula: empty crop")
	}
	midW, midH, dstW, dstH := layoutSize(cw, ch, canvas)
	var content *image.RGBA
	if midW <= maxResizeSide && midH <= maxResizeSide {
		mid := scaleRGBA(cropped, midW, midH, xdraw.BiLinear)
		content = scaleRGBA(mid, dstW, dstH, xdraw.CatmullRom)
	} else {
		content = scaleRGBA(cropped, dstW, dstH, xdraw.CatmullRom)
	}
	plate := image.NewRGBA(image.Rect(0, 0, canvas, canvas))
	padX := (canvas - dstW) / 2
	padY := (canvas - dstH) / 2
	draw.Draw(plate, image.Rect(padX, padY, padX+dstW, padY+dstH), content, content.Bounds().Min, draw.Src)
	return normalizeGray(plate), nil
}

func toRGBA(img image.Image) *image.RGBA {
	b := img.Bounds()
	if r, ok := img.(*image.RGBA); ok && r.Rect.Min.X == 0 && r.Rect.Min.Y == 0 && r.Stride == r.Rect.Dx()*4 {
		return r
	}
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
	return dst
}

// cropMargin drops the near-white border. Ink is a min-max scaled luma
// below 200. A flat image, or one with no ink, is returned unchanged.
func cropMargin(src *image.RGBA) *image.RGBA {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	luma := make([]uint8, w*h)
	minV, maxV := 255, 0
	for y := 0; y < h; y++ {
		row := src.Pix[y*src.Stride:]
		for x := 0; x < w; x++ {
			r := int(row[x*4])
			g := int(row[x*4+1])
			b := int(row[x*4+2])
			y8 := (299*r + 587*g + 114*b + 500) / 1000
			if y8 > 255 {
				y8 = 255
			}
			luma[y*w+x] = uint8(y8)
			if y8 < minV {
				minV = y8
			}
			if y8 > maxV {
				maxV = y8
			}
		}
	}
	if minV == maxV {
		return src
	}
	span := float64(maxV - minV)
	minX, minY := w, h
	maxX, maxY := -1, -1
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			nf := (float64(luma[y*w+x]) - float64(minV)) / span * 255
			if nf < inkThreshold {
				if x < minX {
					minX = x
				}
				if y < minY {
					minY = y
				}
				if x > maxX {
					maxX = x
				}
				if y > maxY {
					maxY = y
				}
			}
		}
	}
	if maxX < 0 {
		return src
	}
	out := image.NewRGBA(image.Rect(0, 0, maxX-minX+1, maxY-minY+1))
	draw.Draw(out, out.Bounds(), src, image.Pt(src.Rect.Min.X+minX, src.Rect.Min.Y+minY), draw.Src)
	return out
}

// layoutSize is torchvision F.resize(image, size) followed by Pillow thumbnail
// into (size, size). The returned mid size is the short-side resize; dst is
// the thumbnail that makes the long side `size`.
func layoutSize(srcW, srcH, size int) (midW, midH, dstW, dstH int) {
	if srcW <= srcH {
		midW = size
		midH = truncMulDiv(size, srcH, srcW)
	} else {
		midH = size
		midW = truncMulDiv(size, srcW, srcH)
	}
	if midW < 1 {
		midW = 1
	}
	if midH < 1 {
		midH = 1
	}
	dstW, dstH = thumbnailSize(midW, midH, size, size)
	return midW, midH, dstW, dstH
}

func truncMulDiv(scale, num, den int) int {
	if den == 0 {
		return 0
	}
	return int(float64(scale) * float64(num) / float64(den))
}

func thumbnailSize(w, h, boxW, boxH int) (int, int) {
	if boxW >= w && boxH >= h {
		return w, h
	}
	aspect := float64(w) / float64(h)
	bx, by := float64(boxW), float64(boxH)
	if bx/by >= aspect {
		nw := roundAspect(by*aspect, func(n float64) float64 {
			return math.Abs(aspect - n/by)
		})
		return nw, boxH
	}
	nh := roundAspect(bx/aspect, func(n float64) float64 {
		if n == 0 {
			return 0
		}
		return math.Abs(aspect - bx/n)
	})
	return boxW, nh
}

func roundAspect(number float64, key func(float64) float64) int {
	fl := math.Floor(number)
	ce := math.Ceil(number)
	best := fl
	if key(ce) < key(fl) {
		best = ce
	}
	if best < 1 {
		return 1
	}
	return int(best)
}

func scaleRGBA(src *image.RGBA, w, h int, sc xdraw.Scaler) *image.RGBA {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	if src.Bounds().Dx() == w && src.Bounds().Dy() == h {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	sc.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Src, nil)
	return dst
}

func normalizeGray(img *image.RGBA) *onnxrt.Tensor {
	t := onnxrt.NewFloat(1, 3, canvas, canvas)
	plane := canvas * canvas
	for y := 0; y < canvas; y++ {
		row := img.Pix[y*img.Stride:]
		for x := 0; x < canvas; x++ {
			r := float32(row[x*4])
			g := float32(row[x*4+1])
			b := float32(row[x*4+2])
			yv := 0.299*r + 0.587*g + 0.114*b
			yi := int(yv + 0.5)
			if yi > 255 {
				yi = 255
			}
			if yi < 0 {
				yi = 0
			}
			n := (float32(yi)/255 - unimerMean) / unimerStd
			i := y*canvas + x
			t.F32[i] = n
			t.F32[plane+i] = n
			t.F32[2*plane+i] = n
		}
	}
	return t
}
