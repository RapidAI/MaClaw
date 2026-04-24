package yolo

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"

	// Register JPEG decoder for screenshots that might be JPEG
	_ "image/jpeg"
)

// PreprocessBase64 decodes a base64 PNG/JPEG, resizes with letterbox padding,
// and normalizes to a [1, 3, size, size] float32 tensor (0-1 range, RGB order).
// Returns original image dimensions and the preprocessed tensor.
func PreprocessBase64(b64 string, size int) (imgW, imgH int, tensor *Tensor, err error) {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return 0, 0, nil, fmt.Errorf("base64 decode: %w", err)
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return 0, 0, nil, fmt.Errorf("image decode: %w", err)
	}

	bounds := img.Bounds()
	imgW = bounds.Dx()
	imgH = bounds.Dy()

	tensor = Letterbox(img, size)
	return imgW, imgH, tensor, nil
}

// Letterbox resizes an image to size×size with aspect-preserving padding (gray=114).
// Returns a [1, 3, size, size] tensor normalized to [0, 1].
func Letterbox(img image.Image, size int) *Tensor {
	bounds := img.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	// Compute scale to fit within size×size
	scale := float64(size) / float64(max(srcW, srcH))
	newW := int(float64(srcW) * scale)
	newH := int(float64(srcH) * scale)

	// Padding offsets (center the image)
	padX := (size - newW) / 2
	padY := (size - newH) / 2

	out := NewTensor(1, 3, size, size)

	// Fill with gray (114/255)
	gray := float32(114.0 / 255.0)
	for i := range out.Data {
		out.Data[i] = gray
	}

	// Nearest-neighbor resize + place into padded tensor
	for dy := 0; dy < newH; dy++ {
		srcY := int(float64(dy) / scale)
		if srcY >= srcH {
			srcY = srcH - 1
		}
		for dx := 0; dx < newW; dx++ {
			srcX := int(float64(dx) / scale)
			if srcX >= srcW {
				srcX = srcW - 1
			}

			r, g, b, _ := img.At(bounds.Min.X+srcX, bounds.Min.Y+srcY).RGBA()
			// RGBA returns 16-bit values, normalize to [0, 1]
			outY := padY + dy
			outX := padX + dx
			out.Set(float32(r)/65535.0, 0, 0, outY, outX) // R
			out.Set(float32(g)/65535.0, 0, 1, outY, outX) // G
			out.Set(float32(b)/65535.0, 0, 2, outY, outX) // B
		}
	}

	return out
}

// Ensure png encoder is available for tests
var _ = png.Encode
