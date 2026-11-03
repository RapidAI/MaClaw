package gopdf

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ColorspaceTarget specifies the target colorspace for conversion.
type ColorspaceTarget int

const (
	// ColorspaceGray converts to DeviceGray.
	ColorspaceGray ColorspaceTarget = iota
	// ColorspaceCMYK converts to DeviceCMYK.
	ColorspaceCMYK
	// ColorspaceRGB converts to DeviceRGB.
	ColorspaceRGB
)

// ConvertColorspaceOption configures colorspace conversion.
type ConvertColorspaceOption struct {
	// Target is the target colorspace.
	Target ColorspaceTarget
}

// ConvertColorspace converts all color operators in the PDF content streams
// to the specified target colorspace. Returns the modified PDF data.
//
// This converts color-setting operators (rg, RG, k, K, g, G) in content
// streams. It does NOT convert image colorspaces — use RecompressImages
// for that.
//
// Example:
//
//	data, _ := os.ReadFile("input.pdf")
//	gray, err := gopdf.ConvertColorspace(data, gopdf.ConvertColorspaceOption{
//	    Target: gopdf.ColorspaceGray,
//	})
//	os.WriteFile("grayscale.pdf", gray, 0644)
func ConvertColorspace(pdfData []byte, opt ConvertColorspaceOption) ([]byte, error) {
	parser, err := newRawPDFParser(pdfData)
	if err != nil {
		return nil, fmt.Errorf("parse PDF: %w", err)
	}

	result := make([]byte, len(pdfData))
	copy(result, pdfData)

	modified := false
	for _, page := range parser.pages {
		for _, contentRef := range page.contents {
			obj, ok := parser.objects[contentRef]
			if !ok || obj.stream == nil {
				continue
			}

			converted := convertStreamColorspace(obj.stream, opt.Target)
			if bytes.Equal(converted, obj.stream) {
				continue
			}

			// Compress the converted stream.
			var compressed bytes.Buffer
			w, err := zlib.NewWriterLevel(&compressed, zlib.DefaultCompression)
			if err != nil {
				continue
			}
			w.Write(converted)
			w.Close()

			newDict := buildCleanedDict(obj.dict, compressed.Len())
			result = replaceObjectStream(result, contentRef, newDict, compressed.Bytes())
			modified = true
		}
	}

	if !modified {
		return pdfData, nil
	}

	result = rebuildXref(result)
	return result, nil
}

// convertStreamColorspace converts color operators in a content stream.
func convertStreamColorspace(stream []byte, target ColorspaceTarget) []byte {
	lines := strings.Split(string(stream), "\n")
	var result []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			result = append(result, line)
			continue
		}
		converted := convertColorLine(trimmed, target)
		result = append(result, converted)
	}

	return []byte(strings.Join(result, "\n"))
}

// convertColorLine converts a single content stream line's color operators.
func convertColorLine(line string, target ColorspaceTarget) string {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return line
	}
	op := parts[len(parts)-1]

	switch op {
	case "rg": // RGB non-stroking
		if len(parts) >= 4 {
			r := parseColorFloat(parts[len(parts)-4])
			g := parseColorFloat(parts[len(parts)-3])
			b := parseColorFloat(parts[len(parts)-2])
			return convertRGBOp(r, g, b, false, target)
		}
	case "RG": // RGB stroking
		if len(parts) >= 4 {
			r := parseColorFloat(parts[len(parts)-4])
			g := parseColorFloat(parts[len(parts)-3])
			b := parseColorFloat(parts[len(parts)-2])
			return convertRGBOp(r, g, b, true, target)
		}
	case "k": // CMYK non-stroking
		if len(parts) >= 5 {
			c := parseColorFloat(parts[len(parts)-5])
			m := parseColorFloat(parts[len(parts)-4])
			y := parseColorFloat(parts[len(parts)-3])
			k := parseColorFloat(parts[len(parts)-2])
			return convertCMYKOp(c, m, y, k, false, target)
		}
	case "K": // CMYK stroking
		if len(parts) >= 5 {
			c := parseColorFloat(parts[len(parts)-5])
			m := parseColorFloat(parts[len(parts)-4])
			y := parseColorFloat(parts[len(parts)-3])
			k := parseColorFloat(parts[len(parts)-2])
			return convertCMYKOp(c, m, y, k, true, target)
		}
	case "g": // Gray non-stroking
		if len(parts) >= 2 {
			gray := parseColorFloat(parts[len(parts)-2])
			return convertGrayOp(gray, false, target)
		}
	case "G": // Gray stroking
		if len(parts) >= 2 {
			gray := parseColorFloat(parts[len(parts)-2])
			return convertGrayOp(gray, true, target)
		}
	}

	return line
}

// convertRGBOp converts an RGB color operation to the target colorspace.
func convertRGBOp(r, g, b float64, stroking bool, target ColorspaceTarget) string {
	switch target {
	case ColorspaceGray:
		gray := rgbToGray(r, g, b)
		if stroking {
			return fmt.Sprintf("%.4f G", gray)
		}
		return fmt.Sprintf("%.4f g", gray)
	case ColorspaceCMYK:
		c, m, y, k := rgbToCMYK(r, g, b)
		if stroking {
			return fmt.Sprintf("%.4f %.4f %.4f %.4f K", c, m, y, k)
		}
		return fmt.Sprintf("%.4f %.4f %.4f %.4f k", c, m, y, k)
	default:
		if stroking {
			return fmt.Sprintf("%.4f %.4f %.4f RG", r, g, b)
		}
		return fmt.Sprintf("%.4f %.4f %.4f rg", r, g, b)
	}
}

// convertCMYKOp converts a CMYK color operation to the target colorspace.
func convertCMYKOp(c, m, y, k float64, stroking bool, target ColorspaceTarget) string {
	switch target {
	case ColorspaceGray:
		r, g, b := cmykToRGB(c, m, y, k)
		gray := rgbToGray(r, g, b)
		if stroking {
			return fmt.Sprintf("%.4f G", gray)
		}
		return fmt.Sprintf("%.4f g", gray)
	case ColorspaceRGB:
		r, g, b := cmykToRGB(c, m, y, k)
		if stroking {
			return fmt.Sprintf("%.4f %.4f %.4f RG", r, g, b)
		}
		return fmt.Sprintf("%.4f %.4f %.4f rg", r, g, b)
	default:
		if stroking {
			return fmt.Sprintf("%.4f %.4f %.4f %.4f K", c, m, y, k)
		}
		return fmt.Sprintf("%.4f %.4f %.4f %.4f k", c, m, y, k)
	}
}

// convertGrayOp converts a gray color operation to the target colorspace.
func convertGrayOp(gray float64, stroking bool, target ColorspaceTarget) string {
	switch target {
	case ColorspaceRGB:
		if stroking {
			return fmt.Sprintf("%.4f %.4f %.4f RG", gray, gray, gray)
		}
		return fmt.Sprintf("%.4f %.4f %.4f rg", gray, gray, gray)
	case ColorspaceCMYK:
		k := 1.0 - gray
		if stroking {
			return fmt.Sprintf("0.0000 0.0000 0.0000 %.4f K", k)
		}
		return fmt.Sprintf("0.0000 0.0000 0.0000 %.4f k", k)
	default:
		if stroking {
			return fmt.Sprintf("%.4f G", gray)
		}
		return fmt.Sprintf("%.4f g", gray)
	}
}

// rgbToGray converts RGB to grayscale using the luminance formula.
func rgbToGray(r, g, b float64) float64 {
	return 0.299*r + 0.587*g + 0.114*b
}

// rgbToCMYK converts RGB to CMYK.
func rgbToCMYK(r, g, b float64) (c, m, y, k float64) {
	k = 1.0 - math.Max(r, math.Max(g, b))
	if k >= 1.0 {
		return 0, 0, 0, 1
	}
	c = (1.0 - r - k) / (1.0 - k)
	m = (1.0 - g - k) / (1.0 - k)
	y = (1.0 - b - k) / (1.0 - k)
	return
}

// cmykToRGB converts CMYK to RGB.
func cmykToRGB(c, m, y, k float64) (r, g, b float64) {
	r = (1.0 - c) * (1.0 - k)
	g = (1.0 - m) * (1.0 - k)
	b = (1.0 - y) * (1.0 - k)
	return
}

// parseColorFloat parses a float from a string, returning 0 on error.
func parseColorFloat(s string) float64 {
	s = strings.TrimSpace(s)
	v, _ := strconv.ParseFloat(s, 64)
	return v
}
