package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	stdDraw "image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	coreim "github.com/RapidAI/CodeClaw/corelib/im"
	"github.com/RapidAI/CodeClaw/corelib/textutil"
	xdraw "golang.org/x/image/draw"
)

const (
	deviceResponseImageMaxDimension       = 64
	deviceResponseImageMaxBase64Len       = 24 << 20
	deviceResponseImageCaptionRunes       = 20
	deviceResponseImageMaxSourceDimension = 8192
	deviceResponseImageMaxSourcePixels    = 16 << 20
)

type preparedDeviceResponseImage struct {
	Data     string
	MIMEType string
	FileName string
	Width    int
	Height   int
	Size     int64
}

func deviceResponseImageCaption(values ...string) string {
	for _, value := range values {
		if caption := strings.TrimSpace(textutil.StripMarkdown(value)); caption != "" {
			return truncateThirdPartyOutputText(caption, deviceResponseImageCaptionRunes)
		}
	}
	return ""
}

func clientSupportsAgentImage(capabilities agent.ClientCapabilities) bool {
	return capabilities.SupportsOutputMIME("image", coreim.ThirdPartyRGB565MIME) ||
		capabilities.SupportsOutputMIME("image", "image/png")
}

func prepareDeviceResponseImage(raw string, capabilities agent.ClientCapabilities) (preparedDeviceResponseImage, error) {
	raw = strings.TrimSpace(raw)
	if comma := strings.Index(raw, ","); strings.HasPrefix(strings.ToLower(raw), "data:image/") && comma >= 0 {
		raw = raw[comma+1:]
	}
	if raw == "" {
		return preparedDeviceResponseImage{}, fmt.Errorf("image data is empty")
	}
	if len(raw) > deviceResponseImageMaxBase64Len {
		return preparedDeviceResponseImage{}, fmt.Errorf("image data is too large")
	}
	encoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return preparedDeviceResponseImage{}, fmt.Errorf("decode image base64: %w", err)
	}
	if len(encoded) == 0 || len(encoded) > thirdPartyMaxMediaBytes {
		return preparedDeviceResponseImage{}, fmt.Errorf("image data exceeds transport limit")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(encoded))
	if err != nil {
		return preparedDeviceResponseImage{}, fmt.Errorf("decode image config: %w", err)
	}
	if err := validateDeviceResponseSourceDimensions(config.Width, config.Height); err != nil {
		return preparedDeviceResponseImage{}, err
	}
	if !capabilities.SupportsOutputMIME("image", coreim.ThirdPartyRGB565MIME) {
		if !capabilities.SupportsOutputMIME("image", "image/png") {
			return preparedDeviceResponseImage{}, fmt.Errorf("client does not support image output")
		}
		if format != "png" {
			return preparedDeviceResponseImage{}, fmt.Errorf("client requires PNG image output, got %s", format)
		}
		width, height := proportionalImageSize(config.Width, config.Height, capabilities.Output.Image)
		if width != config.Width || height != config.Height {
			source, _, err := image.Decode(bytes.NewReader(encoded))
			if err != nil {
				return preparedDeviceResponseImage{}, fmt.Errorf("decode image: %w", err)
			}
			resized := image.NewNRGBA(image.Rect(0, 0, width, height))
			xdraw.CatmullRom.Scale(resized, resized.Bounds(), source, source.Bounds(), xdraw.Src, nil)
			var output bytes.Buffer
			if err := png.Encode(&output, resized); err != nil {
				return preparedDeviceResponseImage{}, fmt.Errorf("encode resized PNG: %w", err)
			}
			encoded = output.Bytes()
			raw = base64.StdEncoding.EncodeToString(encoded)
		}
		return preparedDeviceResponseImage{
			Data: raw, MIMEType: "image/png", FileName: "image.png",
			Width: width, Height: height, Size: int64(len(encoded)),
		}, nil
	}

	source, _, err := image.Decode(bytes.NewReader(encoded))
	if err != nil {
		return preparedDeviceResponseImage{}, fmt.Errorf("decode image: %w", err)
	}
	width, height := proportionalImageSize(source.Bounds().Dx(), source.Bounds().Dy(), capabilities.Output.Image)
	if width < 1 || height < 1 {
		return preparedDeviceResponseImage{}, fmt.Errorf("image has invalid dimensions")
	}

	resized := image.NewNRGBA(image.Rect(0, 0, width, height))
	stdDraw.Draw(resized, resized.Bounds(), &image.Uniform{C: color.NRGBA{R: 8, G: 17, B: 28, A: 255}}, image.Point{}, stdDraw.Src)
	xdraw.CatmullRom.Scale(resized, resized.Bounds(), source, source.Bounds(), xdraw.Over, nil)
	pixels := make([]byte, width*height*2)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, g, b, _ := resized.At(x, y).RGBA()
			value := uint16(((r >> 11) << 11) | ((g >> 10) << 5) | (b >> 11))
			offset := (y*width + x) * 2
			pixels[offset], pixels[offset+1] = byte(value>>8), byte(value)
		}
	}
	return preparedDeviceResponseImage{
		Data: base64.StdEncoding.EncodeToString(pixels), MIMEType: coreim.ThirdPartyRGB565MIME,
		FileName: "image.rgb565", Width: width, Height: height, Size: int64(len(pixels)),
	}, nil
}

func validateDeviceResponseSourceDimensions(width, height int) error {
	if width < 1 || height < 1 {
		return fmt.Errorf("image has invalid dimensions")
	}
	if width > deviceResponseImageMaxSourceDimension || height > deviceResponseImageMaxSourceDimension ||
		int64(width)*int64(height) > deviceResponseImageMaxSourcePixels {
		return fmt.Errorf("image source dimensions are too large: %dx%d", width, height)
	}
	return nil
}

func proportionalImageSize(width, height int, imageCaps *agent.ClientImageCapabilities) (int, int) {
	if width < 1 || height < 1 {
		return 0, 0
	}
	maxWidth, maxHeight := deviceResponseImageMaxDimension, deviceResponseImageMaxDimension
	if imageCaps != nil {
		if imageCaps.MaxWidth > 0 && imageCaps.MaxWidth < maxWidth {
			maxWidth = imageCaps.MaxWidth
		}
		if imageCaps.MaxHeight > 0 && imageCaps.MaxHeight < maxHeight {
			maxHeight = imageCaps.MaxHeight
		}
	}
	if width <= maxWidth && height <= maxHeight {
		return width, height
	}
	if int64(maxWidth)*int64(height) <= int64(maxHeight)*int64(width) {
		scaledHeight := int((int64(height)*int64(maxWidth) + int64(width)/2) / int64(width))
		if scaledHeight < 1 {
			scaledHeight = 1
		}
		return maxWidth, scaledHeight
	}
	scaledWidth := int((int64(width)*int64(maxHeight) + int64(height)/2) / int64(height))
	if scaledWidth < 1 {
		scaledWidth = 1
	}
	return scaledWidth, maxHeight
}
