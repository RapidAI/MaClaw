package gopdf

import (
	"bytes"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// PageTransformOptions configures a page-level transformation.
type PageTransformOptions struct {
	// ScaleX is the horizontal scale factor (1.0 = no change).
	ScaleX float64
	// ScaleY is the vertical scale factor (1.0 = no change).
	ScaleY float64
	// Rotation is the rotation angle in degrees (clockwise).
	Rotation float64
	// TranslateX is the horizontal translation in points.
	TranslateX float64
	// TranslateY is the vertical translation in points.
	TranslateY float64
	// OriginX, OriginY define the transformation origin point.
	// Default: center of the page.
	OriginX, OriginY float64
	// UsePageCenter uses the page center as the origin (overrides OriginX/Y).
	UsePageCenter bool
}

// ScalePage scales the content of the current page by the given factors.
// sx and sy are scale factors (1.0 = no change, 0.5 = half size, 2.0 = double).
//
// Example:
//
//	pdf.ScalePage(0.5, 0.5) // scale to 50%
func (gp *GoPdf) ScalePage(sx, sy float64) {
	gp.TransformPage(PageTransformOptions{
		ScaleX:        sx,
		ScaleY:        sy,
		UsePageCenter: true,
	})
}

// TransformPage applies a combined transformation to the current page content.
// IMPORTANT: You must call TransformPageEnd() after drawing content to restore
// the graphics state. Failing to do so will corrupt the page's graphics state.
//
// Example:
//
//	pdf.TransformPage(gopdf.PageTransformOptions{
//	    ScaleX:    0.8,
//	    ScaleY:    0.8,
//	    Rotation:  45,
//	    UsePageCenter: true,
//	})
//	// ... draw content ...
//	pdf.TransformPageEnd()
func (gp *GoPdf) TransformPage(opts PageTransformOptions) {
	if opts.ScaleX == 0 {
		opts.ScaleX = 1
	}
	if opts.ScaleY == 0 {
		opts.ScaleY = 1
	}

	// Build the transformation matrix.
	m := IdentityMatrix()

	// Get page center if needed.
	ox, oy := opts.OriginX, opts.OriginY
	if opts.UsePageCenter {
		ox = gp.config.PageSize.W / 2
		oy = gp.config.PageSize.H / 2
	}

	// Translate to origin, apply transforms, translate back.
	if ox != 0 || oy != 0 {
		m = m.Multiply(TranslateMatrix(ox, oy))
	}

	if opts.Rotation != 0 {
		m = m.Multiply(RotateMatrix(opts.Rotation))
	}

	if opts.ScaleX != 1 || opts.ScaleY != 1 {
		m = m.Multiply(ScaleMatrix(opts.ScaleX, opts.ScaleY))
	}

	if ox != 0 || oy != 0 {
		m = m.Multiply(TranslateMatrix(-ox, -oy))
	}

	if opts.TranslateX != 0 || opts.TranslateY != 0 {
		m = m.Multiply(TranslateMatrix(opts.TranslateX, opts.TranslateY))
	}

	if m.IsIdentity() {
		return
	}

	// Apply the matrix by wrapping the content stream with a cm operator.
	gp.SaveGraphicsState()

	// Write the cm operator via a custom cache content entry.
	content := gp.getContent()
	content.listCache.append(&cacheContentMatrix{
		a: m.A, b: m.B, c: m.C, d: m.D, e: m.E, f: m.F,
	})
}

// TransformPageEnd should be called after TransformPage to restore the graphics state.
func (gp *GoPdf) TransformPageEnd() {
	gp.RestoreGraphicsState()
}

// ============================================================
// Static page transformation for existing PDF data
// ============================================================

// ScalePageInPDF scales a page in existing PDF data.
// pageIndex is 0-based. Returns the modified PDF data.
//
// Example:
//
//	data, _ := os.ReadFile("input.pdf")
//	scaled, _ := gopdf.ScalePageInPDF(data, 0, 0.5, 0.5)
//	os.WriteFile("output.pdf", scaled, 0644)
func ScalePageInPDF(pdfData []byte, pageIndex int, sx, sy float64) ([]byte, error) {
	return TransformPageInPDF(pdfData, pageIndex, PageTransformOptions{
		ScaleX:        sx,
		ScaleY:        sy,
		UsePageCenter: true,
	})
}

// RotatePageInPDF rotates a page in existing PDF data.
// pageIndex is 0-based, degrees is clockwise rotation.
func RotatePageInPDF(pdfData []byte, pageIndex int, degrees float64) ([]byte, error) {
	return TransformPageInPDF(pdfData, pageIndex, PageTransformOptions{
		Rotation:      degrees,
		UsePageCenter: true,
	})
}

// TransformPageInPDF applies a transformation to a page in existing PDF data.
func TransformPageInPDF(pdfData []byte, pageIndex int, opts PageTransformOptions) ([]byte, error) {
	parser, err := newRawPDFParser(pdfData)
	if err != nil {
		return nil, fmt.Errorf("parse PDF: %w", err)
	}

	if pageIndex < 0 || pageIndex >= len(parser.pages) {
		return nil, fmt.Errorf("page index %d out of range", pageIndex)
	}

	page := parser.pages[pageIndex]
	if len(page.contents) == 0 {
		return pdfData, nil
	}
	contentRef := page.contents[0]

	contentObj, ok := parser.objects[contentRef]
	if !ok {
		return pdfData, nil
	}

	stream := parser.getPageContentStream(pageIndex)
	if len(stream) == 0 {
		return pdfData, nil
	}

	// Build transformation matrix.
	if opts.ScaleX == 0 {
		opts.ScaleX = 1
	}
	if opts.ScaleY == 0 {
		opts.ScaleY = 1
	}

	m := IdentityMatrix()
	ox, oy := opts.OriginX, opts.OriginY
	if opts.UsePageCenter {
		ox = (page.mediaBox[2] - page.mediaBox[0]) / 2
		oy = (page.mediaBox[3] - page.mediaBox[1]) / 2
	}

	if ox != 0 || oy != 0 {
		m = m.Multiply(TranslateMatrix(ox, oy))
	}
	if opts.Rotation != 0 {
		m = m.Multiply(RotateMatrix(opts.Rotation))
	}
	if opts.ScaleX != 1 || opts.ScaleY != 1 {
		m = m.Multiply(ScaleMatrix(opts.ScaleX, opts.ScaleY))
	}
	if ox != 0 || oy != 0 {
		m = m.Multiply(TranslateMatrix(-ox, -oy))
	}
	if opts.TranslateX != 0 || opts.TranslateY != 0 {
		m = m.Multiply(TranslateMatrix(opts.TranslateX, opts.TranslateY))
	}

	if m.IsIdentity() {
		return pdfData, nil
	}

	// Wrap the content stream with q ... Q and the cm operator.
	var newStream bytes.Buffer
	newStream.Grow(len(stream) + 128)
	fmt.Fprintf(&newStream, "q\n%.6f %.6f %.6f %.6f %.6f %.6f cm\n",
		m.A, m.B, m.C, m.D, m.E, m.F)
	newStream.Write(stream)
	newStream.WriteString("\nQ\n")

	result := make([]byte, len(pdfData))
	copy(result, pdfData)
	result = replaceObjectStream(result, contentRef, contentObj.dict, newStream.Bytes())
	result = rebuildXref(result)

	return result, nil
}

// ============================================================
// Matrix inverse for advanced transformations
// ============================================================

// Inverse returns the inverse of the matrix, or the identity matrix if not invertible.
func (m Matrix) Inverse() Matrix {
	det := m.A*m.D - m.B*m.C
	if math.Abs(det) < 1e-10 {
		return IdentityMatrix()
	}
	invDet := 1.0 / det
	return Matrix{
		A: m.D * invDet,
		B: -m.B * invDet,
		C: -m.C * invDet,
		D: m.A * invDet,
		E: (m.C*m.F - m.D*m.E) * invDet,
		F: (m.B*m.E - m.A*m.F) * invDet,
	}
}

// Determinant returns the determinant of the matrix.
func (m Matrix) Determinant() float64 {
	return m.A*m.D - m.B*m.C
}

// TransformRect applies the matrix transformation to a rectangle.
func (m Matrix) TransformRect(r RectFrom) RectFrom {
	// Transform all four corners and compute bounding box.
	x1, y1 := m.TransformPoint(r.X, r.Y)
	x2, y2 := m.TransformPoint(r.X+r.W, r.Y)
	x3, y3 := m.TransformPoint(r.X, r.Y+r.H)
	x4, y4 := m.TransformPoint(r.X+r.W, r.Y+r.H)

	minX := math.Min(math.Min(x1, x2), math.Min(x3, x4))
	maxX := math.Max(math.Max(x1, x2), math.Max(x3, x4))
	minY := math.Min(math.Min(y1, y2), math.Min(y3, y4))
	maxY := math.Max(math.Max(y1, y2), math.Max(y3, y4))

	return RectFrom{X: minX, Y: minY, W: maxX - minX, H: maxY - minY}
}

// SkewMatrix returns a skew transformation matrix.
func SkewMatrix(angleX, angleY float64) Matrix {
	tanX := math.Tan(angleX * math.Pi / 180)
	tanY := math.Tan(angleY * math.Pi / 180)
	return Matrix{A: 1, B: tanY, C: tanX, D: 1, E: 0, F: 0}
}

// String returns a string representation of the matrix.
func (m Matrix) String() string {
	return fmt.Sprintf("[%.4f %.4f %.4f %.4f %.4f %.4f]", m.A, m.B, m.C, m.D, m.E, m.F)
}

// ParseMatrix parses a matrix from a PDF content stream "a b c d e f" format.
// Handles cases where negative numbers may not have spaces before them (e.g., "1 0 0 1-10 20").
func ParseMatrix(s string) (Matrix, error) {
	// Use a custom tokenizer that handles negative numbers without spaces.
	var vals [6]float64
	count := 0
	i := 0
	s = strings.TrimSpace(s)

	for i < len(s) && count < 6 {
		// Skip whitespace.
		for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\r' || s[i] == '\n') {
			i++
		}
		if i >= len(s) {
			break
		}

		// Find the end of this number.
		start := i
		if s[i] == '-' || s[i] == '+' {
			i++
		}
		hasDigit := false
		hasDot := false
		for i < len(s) {
			if s[i] >= '0' && s[i] <= '9' {
				hasDigit = true
				i++
			} else if s[i] == '.' && !hasDot {
				hasDot = true
				i++
			} else {
				break
			}
		}
		if !hasDigit {
			return IdentityMatrix(), fmt.Errorf("unexpected character at position %d", start)
		}

		v, err := strconv.ParseFloat(s[start:i], 64)
		if err != nil {
			return IdentityMatrix(), fmt.Errorf("parse value %d: %w", count, err)
		}
		vals[count] = v
		count++
	}

	if count < 6 {
		return IdentityMatrix(), fmt.Errorf("need 6 values, got %d", count)
	}
	return Matrix{A: vals[0], B: vals[1], C: vals[2], D: vals[3], E: vals[4], F: vals[5]}, nil
}
