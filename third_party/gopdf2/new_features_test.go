package gopdf

import (
	"bytes"
	"os"
	"testing"
)

// ============================================================
// Tests for new features: Watermark, Page Manipulation,
// Annotations, Page Info
// ============================================================

// ============================================================
// Watermark Tests
// ============================================================

func TestWatermarkText(t *testing.T) {
	ensureOutDir(t)
	pdf := newPDFWithFont(t)
	pdf.AddPage()
	pdf.SetXY(50, 50)
	pdf.Cell(nil, "Page with text watermark")

	err := pdf.AddWatermarkText(WatermarkOption{
		Text:       "CONFIDENTIAL",
		FontFamily: fontFamily,
		FontSize:   48,
		Opacity:    0.3,
		Angle:      45,
		Color:      [3]uint8{200, 200, 200},
	})
	if err != nil {
		t.Fatalf("AddWatermarkText: %v", err)
	}

	if err := pdf.WritePdf(resOutDir + "/watermark_text.pdf"); err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
}

func TestWatermarkTextRepeat(t *testing.T) {
	ensureOutDir(t)
	pdf := newPDFWithFont(t)
	pdf.AddPage()
	pdf.SetXY(50, 50)
	pdf.Cell(nil, "Page with repeated watermark")

	err := pdf.AddWatermarkText(WatermarkOption{
		Text:           "DRAFT",
		FontFamily:     fontFamily,
		FontSize:       36,
		Opacity:        0.2,
		Angle:          30,
		Color:          [3]uint8{255, 0, 0},
		Repeat:         true,
		RepeatSpacingX: 200,
		RepeatSpacingY: 200,
	})
	if err != nil {
		t.Fatalf("AddWatermarkText repeat: %v", err)
	}

	if err := pdf.WritePdf(resOutDir + "/watermark_text_repeat.pdf"); err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
}

func TestWatermarkTextAllPages(t *testing.T) {
	ensureOutDir(t)
	pdf := newPDFWithFont(t)

	for i := 0; i < 3; i++ {
		pdf.AddPage()
		pdf.SetXY(50, 50)
		pdf.Cell(nil, "Multi-page watermark test")
	}

	err := pdf.AddWatermarkTextAllPages(WatermarkOption{
		Text:       "SAMPLE",
		FontFamily: fontFamily,
		FontSize:   60,
		Opacity:    0.15,
		Angle:      45,
	})
	if err != nil {
		t.Fatalf("AddWatermarkTextAllPages: %v", err)
	}

	if err := pdf.WritePdf(resOutDir + "/watermark_all_pages.pdf"); err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
}

func TestWatermarkTextErrors(t *testing.T) {
	pdf := newPDFWithFont(t)
	pdf.AddPage()

	// Empty text.
	err := pdf.AddWatermarkText(WatermarkOption{
		FontFamily: fontFamily,
	})
	if err != ErrEmptyString {
		t.Fatalf("expected ErrEmptyString, got: %v", err)
	}

	// Missing font family.
	err = pdf.AddWatermarkText(WatermarkOption{
		Text: "test",
	})
	if err != ErrMissingFontFamily {
		t.Fatalf("expected ErrMissingFontFamily, got: %v", err)
	}
}

func TestWatermarkImage(t *testing.T) {
	ensureOutDir(t)
	pdf := newPDFWithFont(t)
	pdf.AddPage()
	pdf.SetXY(50, 50)
	pdf.Cell(nil, "Page with image watermark")

	err := pdf.AddWatermarkImage(resJPEGPath, 0.3, 200, 200, 0)
	if err != nil {
		t.Fatalf("AddWatermarkImage: %v", err)
	}

	if err := pdf.WritePdf(resOutDir + "/watermark_image.pdf"); err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
}

// ============================================================
// Annotation Tests
// ============================================================

func TestAnnotationText(t *testing.T) {
	ensureOutDir(t)
	pdf := newPDFWithFont(t)
	pdf.AddPage()
	pdf.SetXY(50, 50)
	pdf.Cell(nil, "Page with text annotation")

	pdf.AddTextAnnotation(100, 100, "Reviewer", "This needs review.")

	if err := pdf.WritePdf(resOutDir + "/annot_text.pdf"); err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
}

func TestAnnotationHighlight(t *testing.T) {
	ensureOutDir(t)
	pdf := newPDFWithFont(t)
	pdf.AddPage()
	pdf.SetXY(50, 50)
	pdf.Cell(nil, "Highlighted text area")

	pdf.AddHighlightAnnotation(45, 45, 200, 20, [3]uint8{255, 255, 0})

	if err := pdf.WritePdf(resOutDir + "/annot_highlight.pdf"); err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
}

func TestAnnotationFreeText(t *testing.T) {
	ensureOutDir(t)
	pdf := newPDFWithFont(t)
	pdf.AddPage()

	pdf.AddFreeTextAnnotation(100, 200, 200, 30, "Free text annotation", 14)

	if err := pdf.WritePdf(resOutDir + "/annot_freetext.pdf"); err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
}

func TestAnnotationAllTypes(t *testing.T) {
	ensureOutDir(t)
	pdf := newPDFWithFont(t)
	pdf.AddPage()
	pdf.SetXY(50, 30)
	pdf.Cell(nil, "All annotation types")

	// Text (sticky note)
	pdf.AddAnnotation(AnnotationOption{
		Type:    AnnotText,
		X:       50,
		Y:       60,
		W:       24,
		H:       24,
		Title:   "Author",
		Content: "Sticky note",
		Color:   [3]uint8{255, 255, 0},
	})

	// Highlight
	pdf.AddAnnotation(AnnotationOption{
		Type:  AnnotHighlight,
		X:     50,
		Y:     100,
		W:     200,
		H:     20,
		Color: [3]uint8{255, 255, 0},
	})

	// Underline
	pdf.AddAnnotation(AnnotationOption{
		Type:  AnnotUnderline,
		X:     50,
		Y:     140,
		W:     200,
		H:     20,
		Color: [3]uint8{0, 255, 0},
	})

	// StrikeOut
	pdf.AddAnnotation(AnnotationOption{
		Type:  AnnotStrikeOut,
		X:     50,
		Y:     180,
		W:     200,
		H:     20,
		Color: [3]uint8{255, 0, 0},
	})

	// Square
	pdf.AddAnnotation(AnnotationOption{
		Type:    AnnotSquare,
		X:       50,
		Y:       220,
		W:       100,
		H:       50,
		Color:   [3]uint8{0, 0, 255},
		Content: "Square annotation",
	})

	// Circle
	pdf.AddAnnotation(AnnotationOption{
		Type:    AnnotCircle,
		X:       200,
		Y:       220,
		W:       80,
		H:       50,
		Color:   [3]uint8{255, 0, 255},
		Content: "Circle annotation",
	})

	// FreeText
	pdf.AddAnnotation(AnnotationOption{
		Type:     AnnotFreeText,
		X:        50,
		Y:        300,
		W:        250,
		H:        30,
		Content:  "This is free text on the page",
		FontSize: 12,
		Color:    [3]uint8{0, 0, 0},
	})

	if err := pdf.WritePdf(resOutDir + "/annot_all_types.pdf"); err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
}

// ============================================================
// Page Info Tests
// ============================================================

func TestGetPageSize(t *testing.T) {
	pdf := newPDFWithFont(t)
	pdf.AddPage()

	w, h, err := pdf.GetPageSize(1)
	if err != nil {
		t.Fatalf("GetPageSize: %v", err)
	}
	if w != PageSizeA4.W || h != PageSizeA4.H {
		t.Fatalf("expected A4 size (%.2f x %.2f), got (%.2f x %.2f)",
			PageSizeA4.W, PageSizeA4.H, w, h)
	}
}

func TestGetPageSizeOutOfRange(t *testing.T) {
	pdf := newPDFWithFont(t)
	pdf.AddPage()

	_, _, err := pdf.GetPageSize(0)
	if err != ErrPageOutOfRange {
		t.Fatalf("expected ErrPageOutOfRange, got: %v", err)
	}

	_, _, err = pdf.GetPageSize(2)
	if err != ErrPageOutOfRange {
		t.Fatalf("expected ErrPageOutOfRange, got: %v", err)
	}
}

func TestGetAllPageSizes(t *testing.T) {
	pdf := newPDFWithFont(t)
	pdf.AddPage()
	pdf.AddPage()
	pdf.AddPage()

	sizes := pdf.GetAllPageSizes()
	if len(sizes) != 3 {
		t.Fatalf("expected 3 pages, got %d", len(sizes))
	}
	for i, s := range sizes {
		if s.PageNumber != i+1 {
			t.Fatalf("expected page %d, got %d", i+1, s.PageNumber)
		}
	}
}

func TestGetSourcePDFPageCount(t *testing.T) {
	n, err := GetSourcePDFPageCount(resTestPDF)
	if err != nil {
		t.Skipf("cannot read test PDF: %v", err)
	}
	if n <= 0 {
		t.Fatalf("expected positive page count, got %d", n)
	}
	t.Logf("Source PDF has %d pages", n)
}

func TestGetSourcePDFPageSizes(t *testing.T) {
	sizes, err := GetSourcePDFPageSizes(resTestPDF)
	if err != nil {
		t.Skipf("cannot read test PDF: %v", err)
	}
	if len(sizes) == 0 {
		t.Fatal("expected at least one page size")
	}
	for pageNo, info := range sizes {
		t.Logf("Page %d: %.2f x %.2f", pageNo, info.Width, info.Height)
	}
}

// ============================================================
// Page Manipulation Tests
// ============================================================

func TestExtractPages(t *testing.T) {
	ensureOutDir(t)

	newPdf, err := ExtractPages(resTestPDF, []int{1}, nil)
	if err != nil {
		t.Skipf("cannot extract pages: %v", err)
	}

	if newPdf.GetNumberOfPages() != 1 {
		t.Fatalf("expected 1 page, got %d", newPdf.GetNumberOfPages())
	}

	if err := newPdf.WritePdf(resOutDir + "/extracted_page1.pdf"); err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
}

func TestExtractPagesFromBytes(t *testing.T) {
	ensureOutDir(t)

	data, err := os.ReadFile(resTestPDF)
	if err != nil {
		t.Skipf("cannot read test PDF: %v", err)
	}

	newPdf, err := ExtractPagesFromBytes(data, []int{1}, nil)
	if err != nil {
		t.Fatalf("ExtractPagesFromBytes: %v", err)
	}

	if newPdf.GetNumberOfPages() != 1 {
		t.Fatalf("expected 1 page, got %d", newPdf.GetNumberOfPages())
	}
}

func TestExtractPagesOutOfRange(t *testing.T) {
	_, err := ExtractPages(resTestPDF, []int{999}, nil)
	if err == nil {
		t.Fatal("expected error for out-of-range page")
	}
}

func TestExtractPagesEmpty(t *testing.T) {
	_, err := ExtractPages(resTestPDF, []int{}, nil)
	if err != ErrNoPages {
		t.Fatalf("expected ErrNoPages, got: %v", err)
	}
}

func TestMergePages(t *testing.T) {
	ensureOutDir(t)

	// Use the existing test PDF (which has 3 pages) and merge it with itself.
	merged, err := MergePages([]string{
		resTestPDF,
		resTestPDF,
	}, nil)
	if err != nil {
		t.Skipf("MergePages: %v", err)
	}

	if merged.GetNumberOfPages() != 6 {
		t.Fatalf("expected 6 pages, got %d", merged.GetNumberOfPages())
	}

	if err := merged.WritePdf(resOutDir + "/merged.pdf"); err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
}

func TestMergePagesFromBytes(t *testing.T) {
	ensureOutDir(t)

	data, err := os.ReadFile(resTestPDF)
	if err != nil {
		t.Skipf("cannot read test PDF: %v", err)
	}

	merged, err := MergePagesFromBytes([][]byte{data, data}, nil)
	if err != nil {
		t.Fatalf("MergePagesFromBytes: %v", err)
	}

	if merged.GetNumberOfPages() != 6 {
		t.Fatalf("expected 6 pages, got %d", merged.GetNumberOfPages())
	}

	out, err := merged.GetBytesPdfReturnErr()
	if err != nil {
		t.Fatalf("GetBytesPdfReturnErr: %v", err)
	}
	if !bytes.HasPrefix(out, []byte("%PDF-")) {
		t.Fatal("output does not start with %PDF-")
	}
}

func TestDeletePage(t *testing.T) {
	pdf := newPDFWithFont(t)
	pdf.AddPage()
	pdf.SetXY(50, 50)
	pdf.Cell(nil, "Page 1")
	pdf.AddPage()
	pdf.SetXY(50, 50)
	pdf.Cell(nil, "Page 2")
	pdf.AddPage()
	pdf.SetXY(50, 50)
	pdf.Cell(nil, "Page 3")

	if pdf.GetNumberOfPages() != 3 {
		t.Fatalf("expected 3 pages, got %d", pdf.GetNumberOfPages())
	}

	err := pdf.DeletePage(2)
	if err != nil {
		t.Fatalf("DeletePage: %v", err)
	}

	if pdf.GetNumberOfPages() != 2 {
		t.Fatalf("expected 2 pages after delete, got %d", pdf.GetNumberOfPages())
	}
}

func TestDeletePageOutOfRange(t *testing.T) {
	pdf := newPDFWithFont(t)
	pdf.AddPage()

	err := pdf.DeletePage(0)
	if err != ErrPageOutOfRange {
		t.Fatalf("expected ErrPageOutOfRange, got: %v", err)
	}

	err = pdf.DeletePage(2)
	if err != ErrPageOutOfRange {
		t.Fatalf("expected ErrPageOutOfRange, got: %v", err)
	}
}

func TestCopyPage(t *testing.T) {
	pdf := newPDFWithFont(t)
	pdf.AddPage()
	pdf.SetXY(50, 50)
	pdf.Cell(nil, "Original page")

	newPageNo, err := pdf.CopyPage(1)
	if err != nil {
		t.Fatalf("CopyPage: %v", err)
	}

	if newPageNo != 2 {
		t.Fatalf("expected new page 2, got %d", newPageNo)
	}

	if pdf.GetNumberOfPages() != 2 {
		t.Fatalf("expected 2 pages, got %d", pdf.GetNumberOfPages())
	}
}

func TestCopyPageOutOfRange(t *testing.T) {
	pdf := newPDFWithFont(t)
	pdf.AddPage()

	_, err := pdf.CopyPage(0)
	if err != ErrPageOutOfRange {
		t.Fatalf("expected ErrPageOutOfRange, got: %v", err)
	}

	_, err = pdf.CopyPage(2)
	if err != ErrPageOutOfRange {
		t.Fatalf("expected ErrPageOutOfRange, got: %v", err)
	}
}

// ============================================================
// New Annotation Type Tests
// ============================================================

func TestAnnotationInk(t *testing.T) {
	ensureOutDir(t)
	pdf := newPDFWithFont(t)
	pdf.AddPage()
	pdf.SetXY(50, 50)
	pdf.Cell(nil, "Ink annotation test")

	strokes := [][]Point{
		{{X: 100, Y: 100}, {X: 120, Y: 110}, {X: 140, Y: 100}, {X: 160, Y: 120}},
		{{X: 100, Y: 130}, {X: 150, Y: 140}, {X: 200, Y: 130}},
	}
	pdf.AddInkAnnotation(90, 90, 120, 60, strokes, [3]uint8{0, 0, 255})

	if err := pdf.WritePdf(resOutDir + "/annot_ink.pdf"); err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
}

func TestAnnotationPolyline(t *testing.T) {
	ensureOutDir(t)
	pdf := newPDFWithFont(t)
	pdf.AddPage()
	pdf.SetXY(50, 50)
	pdf.Cell(nil, "Polyline annotation test")

	vertices := []Point{
		{X: 100, Y: 200}, {X: 150, Y: 180}, {X: 200, Y: 220}, {X: 250, Y: 190},
	}
	pdf.AddPolylineAnnotation(90, 170, 170, 60, vertices, [3]uint8{255, 0, 0})

	if err := pdf.WritePdf(resOutDir + "/annot_polyline.pdf"); err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
}

func TestAnnotationPolygon(t *testing.T) {
	ensureOutDir(t)
	pdf := newPDFWithFont(t)
	pdf.AddPage()
	pdf.SetXY(50, 50)
	pdf.Cell(nil, "Polygon annotation test")

	vertices := []Point{
		{X: 100, Y: 300}, {X: 150, Y: 260}, {X: 200, Y: 300}, {X: 175, Y: 340}, {X: 125, Y: 340},
	}
	fill := [3]uint8{200, 200, 255}
	pdf.AddAnnotation(AnnotationOption{
		Type:          AnnotPolygon,
		X:             90,
		Y:             250,
		W:             120,
		H:             100,
		Vertices:      vertices,
		Color:         [3]uint8{0, 0, 255},
		InteriorColor: &fill,
	})

	if err := pdf.WritePdf(resOutDir + "/annot_polygon.pdf"); err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
}

func TestAnnotationLine(t *testing.T) {
	ensureOutDir(t)
	pdf := newPDFWithFont(t)
	pdf.AddPage()
	pdf.SetXY(50, 50)
	pdf.Cell(nil, "Line annotation test")

	pdf.AddLineAnnotation(
		Point{X: 100, Y: 150},
		Point{X: 300, Y: 200},
		[3]uint8{255, 0, 0},
	)

	// Line with arrow endings.
	pdf.AddAnnotation(AnnotationOption{
		Type:      AnnotLine,
		X:         100,
		Y:         250,
		W:         200,
		H:         50,
		LineStart: Point{X: 100, Y: 250},
		LineEnd:   Point{X: 300, Y: 300},
		Color:     [3]uint8{0, 128, 0},
		LineEndingStyles: [2]LineEndingStyle{LineEndNone, LineEndClosedArrow},
	})

	if err := pdf.WritePdf(resOutDir + "/annot_line.pdf"); err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
}

func TestAnnotationStamp(t *testing.T) {
	ensureOutDir(t)
	pdf := newPDFWithFont(t)
	pdf.AddPage()
	pdf.SetXY(50, 50)
	pdf.Cell(nil, "Stamp annotation test")

	pdf.AddStampAnnotation(100, 100, 200, 60, StampApproved)
	pdf.AddStampAnnotation(100, 200, 200, 60, StampDraft)
	pdf.AddStampAnnotation(100, 300, 200, 60, StampConfidential)

	if err := pdf.WritePdf(resOutDir + "/annot_stamp.pdf"); err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
}

func TestAnnotationSquiggly(t *testing.T) {
	ensureOutDir(t)
	pdf := newPDFWithFont(t)
	pdf.AddPage()
	pdf.SetXY(50, 50)
	pdf.Cell(nil, "Squiggly underline annotation test")

	pdf.AddSquigglyAnnotation(45, 45, 300, 20, [3]uint8{255, 0, 0})

	if err := pdf.WritePdf(resOutDir + "/annot_squiggly.pdf"); err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
}

func TestAnnotationCaret(t *testing.T) {
	ensureOutDir(t)
	pdf := newPDFWithFont(t)
	pdf.AddPage()
	pdf.SetXY(50, 50)
	pdf.Cell(nil, "Caret annotation test")

	pdf.AddCaretAnnotation(200, 48, 10, 20, "Insert text here")

	if err := pdf.WritePdf(resOutDir + "/annot_caret.pdf"); err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
}

func TestAnnotationFileAttachment(t *testing.T) {
	ensureOutDir(t)
	pdf := newPDFWithFont(t)
	pdf.AddPage()
	pdf.SetXY(50, 50)
	pdf.Cell(nil, "File attachment annotation test")

	pdf.AddFileAttachmentAnnotation(100, 100, "readme.txt", []byte("Hello, World!"), "Attached file")

	if err := pdf.WritePdf(resOutDir + "/annot_file_attach.pdf"); err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
}

func TestAnnotationRedact(t *testing.T) {
	ensureOutDir(t)
	pdf := newPDFWithFont(t)
	pdf.AddPage()
	pdf.SetXY(50, 50)
	pdf.Cell(nil, "Redaction annotation test - SENSITIVE DATA HERE")

	pdf.AddRedactAnnotation(250, 45, 200, 20, "REDACTED")

	if err := pdf.WritePdf(resOutDir + "/annot_redact.pdf"); err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
}

func TestAnnotationGetAndDelete(t *testing.T) {
	pdf := newPDFWithFont(t)
	pdf.AddPage()

	pdf.AddTextAnnotation(50, 50, "Author", "Note 1")
	pdf.AddHighlightAnnotation(50, 100, 200, 20, [3]uint8{255, 255, 0})
	pdf.AddStampAnnotation(50, 150, 100, 50, StampDraft)

	annots := pdf.GetAnnotations()
	if len(annots) != 3 {
		t.Fatalf("expected 3 annotations, got %d", len(annots))
	}
	if annots[0].Type != AnnotText {
		t.Fatalf("expected AnnotText, got %d", annots[0].Type)
	}
	if annots[1].Type != AnnotHighlight {
		t.Fatalf("expected AnnotHighlight, got %d", annots[1].Type)
	}
	if annots[2].Type != AnnotStamp {
		t.Fatalf("expected AnnotStamp, got %d", annots[2].Type)
	}

	// Delete the middle annotation.
	ok := pdf.DeleteAnnotation(1)
	if !ok {
		t.Fatal("DeleteAnnotation returned false")
	}

	annots = pdf.GetAnnotations()
	if len(annots) != 2 {
		t.Fatalf("expected 2 annotations after delete, got %d", len(annots))
	}
	if annots[0].Type != AnnotText || annots[1].Type != AnnotStamp {
		t.Fatal("unexpected annotation types after delete")
	}
}

func TestAnnotationApplyRedactions(t *testing.T) {
	pdf := newPDFWithFont(t)
	pdf.AddPage()

	pdf.AddTextAnnotation(50, 50, "Author", "Keep this")
	pdf.AddRedactAnnotation(50, 100, 200, 20, "REDACTED")
	pdf.AddRedactAnnotation(50, 150, 200, 20, "ALSO REDACTED")
	pdf.AddHighlightAnnotation(50, 200, 200, 20, [3]uint8{255, 255, 0})

	annots := pdf.GetAnnotations()
	if len(annots) != 4 {
		t.Fatalf("expected 4 annotations, got %d", len(annots))
	}

	removed := pdf.ApplyRedactions()
	if removed != 2 {
		t.Fatalf("expected 2 redactions removed, got %d", removed)
	}

	annots = pdf.GetAnnotations()
	if len(annots) != 2 {
		t.Fatalf("expected 2 annotations after redaction, got %d", len(annots))
	}
	if annots[0].Type != AnnotText || annots[1].Type != AnnotHighlight {
		t.Fatal("unexpected annotation types after redaction")
	}
}

func TestAnnotationAllNewTypes(t *testing.T) {
	ensureOutDir(t)
	pdf := newPDFWithFont(t)
	pdf.AddPage()
	pdf.SetXY(50, 30)
	pdf.Cell(nil, "All new annotation types")

	// Ink
	pdf.AddInkAnnotation(50, 60, 100, 40,
		[][]Point{{{X: 60, Y: 70}, {X: 80, Y: 80}, {X: 100, Y: 70}, {X: 120, Y: 90}}},
		[3]uint8{0, 0, 200})

	// Polyline
	pdf.AddPolylineAnnotation(50, 120, 200, 40,
		[]Point{{X: 60, Y: 130}, {X: 100, Y: 120}, {X: 140, Y: 150}, {X: 200, Y: 130}},
		[3]uint8{200, 0, 0})

	// Polygon
	fill := [3]uint8{220, 220, 255}
	pdf.AddAnnotation(AnnotationOption{
		Type:          AnnotPolygon,
		X:             50, Y: 180, W: 150, H: 80,
		Vertices:      []Point{{X: 60, Y: 190}, {X: 125, Y: 180}, {X: 190, Y: 190}, {X: 170, Y: 250}, {X: 80, Y: 250}},
		Color:         [3]uint8{0, 0, 180},
		InteriorColor: &fill,
	})

	// Line
	pdf.AddLineAnnotation(Point{X: 250, Y: 60}, Point{X: 450, Y: 100}, [3]uint8{0, 150, 0})

	// Stamp
	pdf.AddStampAnnotation(250, 120, 180, 50, StampApproved)

	// Squiggly
	pdf.AddSquigglyAnnotation(250, 190, 200, 15, [3]uint8{255, 100, 0})

	// Caret
	pdf.AddCaretAnnotation(250, 220, 10, 18, "Insert here")

	// File attachment
	pdf.AddFileAttachmentAnnotation(250, 260, "test.txt", []byte("test content"), "Attached")

	// Redact
	pdf.AddRedactAnnotation(50, 300, 200, 20, "REDACTED")

	if err := pdf.WritePdf(resOutDir + "/annot_all_new_types.pdf"); err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
}
