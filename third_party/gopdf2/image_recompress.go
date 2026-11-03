package gopdf

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"regexp"
	"strconv"
	"strings"
)

// Pre-compiled regex for xref rebuilding.
var reObjHeaderLine = regexp.MustCompile(`(?m)^(\d+) 0 obj`)

// RecompressOption configures how images are recompressed.
type RecompressOption struct {
	// Format is the target format: "jpeg" or "png". Default: "jpeg".
	Format string
	// JPEGQuality is the JPEG quality (1-100). Default: 75.
	JPEGQuality int
	// MaxWidth limits the maximum image width. 0 means no limit.
	MaxWidth int
	// MaxHeight limits the maximum image height. 0 means no limit.
	MaxHeight int
}

func (o *RecompressOption) defaults() {
	if o.Format == "" {
		o.Format = "jpeg"
	}
	if o.JPEGQuality <= 0 || o.JPEGQuality > 100 {
		o.JPEGQuality = 75
	}
}

// RecompressImages recompresses all images in the given PDF data and
// returns the modified PDF data.
//
// Example:
//
//	data, _ := os.ReadFile("input.pdf")
//	smaller, err := gopdf.RecompressImages(data, gopdf.RecompressOption{
//	    Format:      "jpeg",
//	    JPEGQuality: 60,
//	})
//	os.WriteFile("output.pdf", smaller, 0644)
func RecompressImages(pdfData []byte, opt RecompressOption) ([]byte, error) {
	opt.defaults()
	parser, err := newRawPDFParser(pdfData)
	if err != nil {
		return nil, fmt.Errorf("parse PDF: %w", err)
	}

	// Collect all image XObject object numbers.
	imageObjs := make(map[int]bool)
	for _, page := range parser.pages {
		for _, objNum := range page.resources.xobjs {
			obj, ok := parser.objects[objNum]
			if !ok {
				continue
			}
			if strings.Contains(obj.dict, "/Subtype /Image") ||
				strings.Contains(obj.dict, "/Subtype/Image") {
				imageObjs[objNum] = true
			}
		}
	}

	if len(imageObjs) == 0 {
		return pdfData, nil
	}

	result := make([]byte, len(pdfData))
	copy(result, pdfData)

	for objNum := range imageObjs {
		obj := parser.objects[objNum]
		recompressed, newDict, err := recompressImageObj(obj, opt)
		if err != nil {
			continue
		}
		result = replaceObjectStream(result, objNum, newDict, recompressed)
	}

	result = rebuildXref(result)
	return result, nil
}

func recompressImageObj(obj rawPDFObject, opt RecompressOption) ([]byte, string, error) {
	imgData := obj.stream
	if imgData == nil {
		return nil, "", fmt.Errorf("no stream data")
	}

	filter := extractFilterValue(obj.dict)
	var img image.Image
	var err error

	switch filter {
	case "DCTDecode":
		img, err = jpeg.Decode(bytes.NewReader(imgData))
	case "FlateDecode", "":
		img, _, err = image.Decode(bytes.NewReader(imgData))
		if err != nil {
			return nil, "", fmt.Errorf("cannot decode FlateDecode image: %w", err)
		}
	default:
		return nil, "", fmt.Errorf("unsupported filter: %s", filter)
	}
	if err != nil {
		return nil, "", fmt.Errorf("decode image: %w", err)
	}

	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	if (opt.MaxWidth > 0 && w > opt.MaxWidth) || (opt.MaxHeight > 0 && h > opt.MaxHeight) {
		img = downscaleImage(img, opt.MaxWidth, opt.MaxHeight)
		bounds = img.Bounds()
		w = bounds.Dx()
		h = bounds.Dy()
	}

	var buf bytes.Buffer
	var newFilter string

	switch opt.Format {
	case "jpeg":
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: opt.JPEGQuality})
		newFilter = "DCTDecode"
	case "png":
		err = png.Encode(&buf, img)
		newFilter = "FlateDecode"
	default:
		return nil, "", fmt.Errorf("unsupported target format: %s", opt.Format)
	}
	if err != nil {
		return nil, "", fmt.Errorf("encode image: %w", err)
	}

	newDict := fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /%s /Length %d >>",
		w, h, newFilter, buf.Len())

	return buf.Bytes(), newDict, nil
}

func downscaleImage(src image.Image, maxW, maxH int) image.Image {
	bounds := src.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()

	scaleW := 1.0
	scaleH := 1.0
	if maxW > 0 && w > maxW {
		scaleW = float64(maxW) / float64(w)
	}
	if maxH > 0 && h > maxH {
		scaleH = float64(maxH) / float64(h)
	}
	scale := scaleW
	if scaleH < scale {
		scale = scaleH
	}

	newW := int(float64(w) * scale)
	newH := int(float64(h) * scale)
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	// Use nearest-neighbor with direct pixel access for performance.
	invScale := 1.0 / scale
	for y := 0; y < newH; y++ {
		srcY := int(float64(y) * invScale)
		if srcY >= h {
			srcY = h - 1
		}
		srcY += bounds.Min.Y
		for x := 0; x < newW; x++ {
			srcX := int(float64(x) * invScale)
			if srcX >= w {
				srcX = w - 1
			}
			r, g, b, a := src.At(bounds.Min.X+srcX, srcY).RGBA()
			dst.Pix[(y*dst.Stride)+(x*4)+0] = uint8(r >> 8)
			dst.Pix[(y*dst.Stride)+(x*4)+1] = uint8(g >> 8)
			dst.Pix[(y*dst.Stride)+(x*4)+2] = uint8(b >> 8)
			dst.Pix[(y*dst.Stride)+(x*4)+3] = uint8(a >> 8)
		}
	}
	return dst
}

func replaceObjectStream(pdfData []byte, objNum int, newDict string, newStream []byte) []byte {
	objHeader := fmt.Sprintf("%d 0 obj\n", objNum)
	idx := bytes.Index(pdfData, []byte(objHeader))
	if idx < 0 {
		objHeader = fmt.Sprintf("%d 0 obj\r\n", objNum)
		idx = bytes.Index(pdfData, []byte(objHeader))
	}
	if idx < 0 {
		return pdfData
	}

	endIdx := bytes.Index(pdfData[idx:], []byte("endobj"))
	if endIdx < 0 {
		return pdfData
	}
	endIdx += idx + len("endobj")

	// Pre-allocate result buffer with estimated capacity.
	estSize := len(pdfData) - (endIdx - idx) + len(objHeader) + len(newDict) + len(newStream) + 32
	var result bytes.Buffer
	result.Grow(estSize)
	result.Write(pdfData[:idx])
	fmt.Fprintf(&result, "%d 0 obj\n", objNum)
	result.WriteString(newDict)
	result.WriteString("\nstream\n")
	result.Write(newStream)
	result.WriteString("\nendstream\nendobj")
	result.Write(pdfData[endIdx:])

	return result.Bytes()
}

func rebuildXref(pdfData []byte) []byte {
	xrefIdx := bytes.LastIndex(pdfData, []byte("xref\n"))
	if xrefIdx < 0 {
		xrefIdx = bytes.LastIndex(pdfData, []byte("xref\r\n"))
	}
	if xrefIdx < 0 {
		return pdfData
	}

	objOffsets := make(map[int]int)
	matches := reObjHeaderLine.FindAllSubmatchIndex(pdfData, -1)
	for _, m := range matches {
		numStr := string(pdfData[m[2]:m[3]])
		num, _ := strconv.Atoi(numStr)
		objOffsets[num] = m[0]
	}

	if len(objOffsets) == 0 {
		return pdfData
	}

	maxObj := 0
	for n := range objOffsets {
		if n > maxObj {
			maxObj = n
		}
	}

	var xref bytes.Buffer
	xref.Grow(20*(maxObj+1) + 32)
	xref.WriteString("xref\n")
	fmt.Fprintf(&xref, "0 %d\n", maxObj+1)
	xref.WriteString("0000000000 65535 f \n")
	for i := 1; i <= maxObj; i++ {
		if off, ok := objOffsets[i]; ok {
			fmt.Fprintf(&xref, "%010d 00000 n \n", off)
		} else {
			xref.WriteString("0000000000 00000 f \n")
		}
	}

	trailerIdx := bytes.Index(pdfData[xrefIdx:], []byte("trailer"))
	if trailerIdx < 0 {
		return pdfData
	}
	trailerStart := xrefIdx + trailerIdx

	trailerContent := pdfData[trailerStart:]
	startxrefIdx := bytes.Index(trailerContent, []byte("startxref"))
	if startxrefIdx < 0 {
		return pdfData
	}
	trailerDict := trailerContent[:startxrefIdx]

	var result bytes.Buffer
	result.Write(pdfData[:xrefIdx])
	newXrefOff := result.Len()
	result.Write(xref.Bytes())
	result.Write(trailerDict)
	fmt.Fprintf(&result, "startxref\n%d\n%%%%EOF\n", newXrefOff)

	return result.Bytes()
}
