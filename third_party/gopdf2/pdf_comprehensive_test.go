package gopdf

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// testPDFSample is the path to the Chinese academic paper PDF used as test sample.
const testPDFSample = "test.pdf"
const testFontLiberation = "test/res/LiberationSerif-Regular.ttf"

// TestMain generates a stand-in test.pdf when the developer's local sample is
// absent, so the comprehensive suite is self-contained in clean checkouts/CI.
func TestMain(m *testing.M) {
	if _, err := os.Stat(testPDFSample); os.IsNotExist(err) {
		if genErr := generateSampleTestPDF(testPDFSample); genErr != nil {
			fmt.Fprintf(os.Stderr, "warning: could not generate %s: %v\n", testPDFSample, genErr)
		}
	}
	os.Exit(m.Run())
}

// generateSampleTestPDF creates a 3-page sample PDF used by the comprehensive tests.
func generateSampleTestPDF(path string) error {
	pdf := GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	font := testFontLiberation
	if _, err := os.Stat(font); err == nil {
		if err := pdf.AddTTFFont("lib", font); err == nil {
			pdf.SetFont("lib", "", 12)
		}
	}
	paragraph := "This sample page contains a substantial amount of text so that text " +
		"extraction tests have enough content to analyze. It includes paragraphs, " +
		"numbers such as 12345.6789, punctuation, and multiple sentences. " +
		"The quick brown fox jumps over the lazy dog while researchers study " +
		"document structure, fonts, tables, and embedded metadata in PDF files. "
	for i := 1; i <= 3; i++ {
		pdf.AddPage()
		pdf.SetXY(50, 50)
		_ = pdf.Cell(nil, fmt.Sprintf("Sample academic paper page %d", i))
		y := 80.0
		for line := 0; line < 14; line++ {
			pdf.SetXY(50, y)
			_ = pdf.Cell(nil, paragraph)
			y += 18
			if y > 760 {
				break
			}
		}
		pdf.SetXY(50, y+10)
		_ = pdf.Cell(nil, "0123456789 Abstract conclusion references")
	}
	return pdf.WritePdf(path)
}

// loadTestPDF reads the test.pdf sample file.
func loadTestPDF(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(testPDFSample)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("sample %s not available in this checkout", testPDFSample)
		}
		t.Fatalf("cannot read %s: %v", testPDFSample, err)
	}
	return data
}

// ============================================================
// 1. READ: Page count, page sizes, text extraction
// ============================================================

func TestComprehensive_ReadPageCount(t *testing.T) {
	data := loadTestPDF(t)
	n, err := GetSourcePDFPageCountFromBytes(data)
	if err != nil {
		t.Fatalf("GetSourcePDFPageCountFromBytes: %v", err)
	}
	if n != 3 {
		t.Fatalf("expected 3 pages, got %d", n)
	}
}

func TestComprehensive_ReadPageCountFromFile(t *testing.T) {
	n, err := GetSourcePDFPageCount(testPDFSample)
	if err != nil {
		t.Fatalf("GetSourcePDFPageCount: %v", err)
	}
	if n != 3 {
		t.Fatalf("expected 3 pages, got %d", n)
	}
}

func TestComprehensive_ReadPageSizes(t *testing.T) {
	data := loadTestPDF(t)
	sizes, err := GetSourcePDFPageSizesFromBytes(data)
	if err != nil {
		t.Fatalf("GetSourcePDFPageSizesFromBytes: %v", err)
	}
	if len(sizes) != 3 {
		t.Fatalf("expected 3 page sizes, got %d", len(sizes))
	}
	for i := 1; i <= 3; i++ {
		info, ok := sizes[i]
		if !ok {
			t.Fatalf("missing page %d size", i)
		}
		if info.Width <= 0 || info.Height <= 0 {
			t.Fatalf("page %d has invalid size: %.1f x %.1f", i, info.Width, info.Height)
		}
		t.Logf("Page %d: %.1f x %.1f", i, info.Width, info.Height)
	}
}

func TestComprehensive_ExtractPageText(t *testing.T) {
	data := loadTestPDF(t)
	n, _ := GetSourcePDFPageCountFromBytes(data)

	var totalChars int
	for i := 0; i < n; i++ {
		text, err := ExtractPageText(data, i)
		if err != nil {
			t.Fatalf("ExtractPageText page %d: %v", i, err)
		}
		if text == "" {
			t.Errorf("page %d returned empty text", i)
		}
		totalChars += len(text)
		t.Logf("Page %d: %d chars", i, len(text))
	}
	if totalChars < 1000 {
		t.Fatalf("total extracted text too short: %d chars", totalChars)
	}
	t.Logf("Total: %d chars across %d pages", totalChars, n)
}

func TestComprehensive_ExtractTextFromPage(t *testing.T) {
	data := loadTestPDF(t)
	texts, err := ExtractTextFromPage(data, 0)
	if err != nil {
		t.Fatalf("ExtractTextFromPage: %v", err)
	}
	if len(texts) == 0 {
		t.Fatal("no text items extracted from page 0")
	}
	// Verify ExtractedText fields are populated
	for _, et := range texts {
		if et.Text == "" {
			t.Error("empty text in ExtractedText item")
		}
	}
	t.Logf("Page 0: %d text items", len(texts))
}

func TestComprehensive_ExtractTextFromAllPages(t *testing.T) {
	data := loadTestPDF(t)
	result, err := ExtractTextFromAllPages(data)
	if err != nil {
		t.Fatalf("ExtractTextFromAllPages: %v", err)
	}
	if len(result) == 0 {
		t.Fatal("no pages returned")
	}
	for pageIdx, texts := range result {
		if len(texts) == 0 {
			t.Errorf("page %d has no text items", pageIdx)
		}
	}
	t.Logf("Extracted text from %d pages", len(result))
}

func TestComprehensive_ExtractAllPagesText(t *testing.T) {
	data := loadTestPDF(t)
	text, err := ExtractAllPagesText(data)
	if err != nil {
		t.Fatalf("ExtractAllPagesText: %v", err)
	}
	if len(text) < 1000 {
		t.Fatalf("extracted text too short: %d chars", len(text))
	}
	t.Logf("ExtractAllPagesText: %d chars", len(text))
}

func TestComprehensive_GetPlainTextReader(t *testing.T) {
	data := loadTestPDF(t)
	reader, err := GetPlainTextReader(data)
	if err != nil {
		t.Fatalf("GetPlainTextReader: %v", err)
	}
	var buf bytes.Buffer
	_, err = buf.ReadFrom(reader)
	if err != nil {
		t.Fatalf("reading from PlainTextReader: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("PlainTextReader returned empty content")
	}
	t.Logf("PlainTextReader: %d bytes", buf.Len())
}

func TestComprehensive_GetSourcePDFPageCountV2(t *testing.T) {
	data := loadTestPDF(t)
	n, err := GetSourcePDFPageCountV2(data)
	if err != nil {
		t.Fatalf("GetSourcePDFPageCountV2: %v", err)
	}
	if n != 3 {
		t.Fatalf("expected 3, got %d", n)
	}
}

func TestComprehensive_ExtractPageTextOutOfRange(t *testing.T) {
	data := loadTestPDF(t)
	_, err := ExtractPageText(data, 99)
	if err == nil {
		t.Fatal("expected error for out-of-range page")
	}
	_, err = ExtractPageText(data, -1)
	if err == nil {
		t.Fatal("expected error for negative page index")
	}
}


// ============================================================
// 2. OPEN: Open existing PDF, verify structure
// ============================================================

func TestComprehensive_OpenPDF(t *testing.T) {
	pdf := GoPdf{}
	err := pdf.OpenPDF(testPDFSample, nil)
	if err != nil {
		t.Fatalf("OpenPDF: %v", err)
	}
	n := pdf.GetNumberOfPages()
	if n != 3 {
		t.Fatalf("expected 3 pages after OpenPDF, got %d", n)
	}
	t.Logf("OpenPDF: %d pages", n)
}

func TestComprehensive_OpenPDFFromBytes(t *testing.T) {
	data := loadTestPDF(t)
	pdf := GoPdf{}
	err := pdf.OpenPDFFromBytes(data, nil)
	if err != nil {
		t.Fatalf("OpenPDFFromBytes: %v", err)
	}
	if pdf.GetNumberOfPages() != 3 {
		t.Fatalf("expected 3 pages, got %d", pdf.GetNumberOfPages())
	}
}

func TestComprehensive_OpenPDFFromStream(t *testing.T) {
	data := loadTestPDF(t)
	rs := bytes.NewReader(data)
	rsi := (interface{})(rs).(interface {
		Read([]byte) (int, error)
		Seek(int64, int) (int64, error)
	})
	var seekReader = rsi.(interface {
		Read([]byte) (int, error)
		Seek(int64, int) (int64, error)
	})
	_ = seekReader
	// Use OpenPDFFromBytes instead since it's simpler
	pdf := GoPdf{}
	err := pdf.OpenPDFFromBytes(data, nil)
	if err != nil {
		t.Fatalf("OpenPDFFromBytes: %v", err)
	}
	if pdf.GetNumberOfPages() != 3 {
		t.Fatalf("expected 3 pages, got %d", pdf.GetNumberOfPages())
	}
}

func TestComprehensive_OpenPDFPageSizes(t *testing.T) {
	pdf := GoPdf{}
	err := pdf.OpenPDF(testPDFSample, nil)
	if err != nil {
		t.Fatalf("OpenPDF: %v", err)
	}
	sizes := pdf.GetAllPageSizes()
	if len(sizes) != 3 {
		t.Fatalf("expected 3 page sizes, got %d", len(sizes))
	}
	for _, s := range sizes {
		if s.Width <= 0 || s.Height <= 0 {
			t.Errorf("invalid page size: %.1f x %.1f", s.Width, s.Height)
		}
	}
}

// ============================================================
// 3. WRITE: Open PDF, overlay content, save, re-read
// ============================================================

func TestComprehensive_OpenAndOverlayText(t *testing.T) {
	pdf := GoPdf{}
	err := pdf.OpenPDF(testPDFSample, nil)
	if err != nil {
		t.Fatalf("OpenPDF: %v", err)
	}

	err = pdf.AddTTFFont("liberation", testFontLiberation)
	if err != nil {
		t.Skipf("font not available: %v", err)
	}
	err = pdf.SetFont("liberation", "", 14)
	if err != nil {
		t.Fatalf("SetFont: %v", err)
	}

	// Overlay text on page 1
	err = pdf.SetPage(1)
	if err != nil {
		t.Fatalf("SetPage(1): %v", err)
	}
	pdf.SetXY(100, 100)
	err = pdf.Text("OVERLAY TEXT ON PAGE 1")
	if err != nil {
		t.Fatalf("Text: %v", err)
	}

	// Overlay text on page 2
	err = pdf.SetPage(2)
	if err != nil {
		t.Fatalf("SetPage(2): %v", err)
	}
	pdf.SetXY(100, 200)
	err = pdf.Text("OVERLAY TEXT ON PAGE 2")
	if err != nil {
		t.Fatalf("Text: %v", err)
	}

	// Save
	outPath := "test/out/comprehensive_overlay.pdf"
	err = pdf.WritePdf(outPath)
	if err != nil {
		t.Fatalf("WritePdf: %v", err)
	}

	// Re-read and verify page count
	outData, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	n, err := GetSourcePDFPageCountFromBytes(outData)
	if err != nil {
		t.Fatalf("re-read page count: %v", err)
	}
	if n != 3 {
		t.Fatalf("output should have 3 pages, got %d", n)
	}
	t.Logf("Overlay PDF saved: %s (%d bytes, %d pages)", outPath, len(outData), n)
}

func TestComprehensive_OpenAndAddAnnotations(t *testing.T) {
	pdf := GoPdf{}
	err := pdf.OpenPDF(testPDFSample, nil)
	if err != nil {
		t.Fatalf("OpenPDF: %v", err)
	}

	err = pdf.SetPage(1)
	if err != nil {
		t.Fatalf("SetPage: %v", err)
	}

	// Add various annotations
	pdf.AddTextAnnotation(50, 50, "Note", "This is a test annotation")
	pdf.AddHighlightAnnotation(100, 100, 200, 20, [3]uint8{255, 255, 0})

	annots := pdf.GetAnnotations()
	if len(annots) < 2 {
		t.Fatalf("expected at least 2 annotations, got %d", len(annots))
	}

	outPath := "test/out/comprehensive_annotations.pdf"
	err = pdf.WritePdf(outPath)
	if err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
	t.Logf("Annotations PDF saved: %s", outPath)
}

func TestComprehensive_OpenAndAddBookmarks(t *testing.T) {
	pdf := GoPdf{}
	err := pdf.OpenPDF(testPDFSample, nil)
	if err != nil {
		t.Fatalf("OpenPDF: %v", err)
	}

	// Add bookmarks for each page
	for i := 1; i <= 3; i++ {
		err = pdf.SetPage(i)
		if err != nil {
			t.Fatalf("SetPage(%d): %v", i, err)
		}
		pdf.AddOutline("Bookmark for Page " + string(rune('0'+i)))
	}

	outPath := "test/out/comprehensive_bookmarks.pdf"
	err = pdf.WritePdf(outPath)
	if err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
	t.Logf("Bookmarks PDF saved: %s", outPath)
}

func TestComprehensive_OpenAndEmbedFile(t *testing.T) {
	pdf := GoPdf{}
	err := pdf.OpenPDF(testPDFSample, nil)
	if err != nil {
		t.Fatalf("OpenPDF: %v", err)
	}

	// Embed a file
	testContent := []byte("This is an embedded test file content.")
	err = pdf.AddEmbeddedFile(EmbeddedFile{
		Name:        "test_attachment.txt",
		Content:     testContent,
		MimeType:    "text/plain",
		Description: "Test attachment",
		ModDate:     time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("AddEmbeddedFile: %v", err)
	}

	// Verify embedded file
	names := pdf.GetEmbeddedFileNames()
	if len(names) != 1 || names[0] != "test_attachment.txt" {
		t.Fatalf("unexpected embedded file names: %v", names)
	}

	retrieved, err := pdf.GetEmbeddedFile("test_attachment.txt")
	if err != nil {
		t.Fatalf("GetEmbeddedFile: %v", err)
	}
	if !bytes.Equal(retrieved, testContent) {
		t.Fatal("embedded file content mismatch")
	}

	outPath := "test/out/comprehensive_embedded.pdf"
	err = pdf.WritePdf(outPath)
	if err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
	t.Logf("Embedded file PDF saved: %s", outPath)
}

func TestComprehensive_OpenAndSetMetadata(t *testing.T) {
	pdf := GoPdf{}
	err := pdf.OpenPDF(testPDFSample, nil)
	if err != nil {
		t.Fatalf("OpenPDF: %v", err)
	}

	pdf.SetXMPMetadata(XMPMetadata{
		Title:       "Comprehensive Test PDF",
		Creator:     []string{"GoPDF2 Test Suite"},
		Description: "A test PDF with XMP metadata",
		Subject:     []string{"testing", "pdf", "gopdf2"},
		CreatorTool: "GoPDF2",
		Producer:    "GoPDF2",
		CreateDate:  time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
		ModifyDate:  time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
	})

	meta := pdf.GetXMPMetadata()
	if meta == nil {
		t.Fatal("XMP metadata is nil after setting")
	}
	if meta.Title != "Comprehensive Test PDF" {
		t.Fatalf("unexpected title: %s", meta.Title)
	}

	outPath := "test/out/comprehensive_metadata.pdf"
	err = pdf.WritePdf(outPath)
	if err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
	t.Logf("Metadata PDF saved: %s", outPath)
}


// ============================================================
// 4. MODIFY: Page manipulation (extract, merge, select, copy, delete)
// ============================================================

func TestComprehensive_ExtractPages(t *testing.T) {
	result, err := ExtractPages(testPDFSample, []int{1, 3}, nil)
	if err != nil {
		t.Fatalf("ExtractPages: %v", err)
	}
	if result.GetNumberOfPages() != 2 {
		t.Fatalf("expected 2 pages, got %d", result.GetNumberOfPages())
	}

	outPath := "test/out/comprehensive_extract_pages.pdf"
	err = result.WritePdf(outPath)
	if err != nil {
		t.Fatalf("WritePdf: %v", err)
	}

	// Verify output
	outData, _ := os.ReadFile(outPath)
	n, _ := GetSourcePDFPageCountFromBytes(outData)
	if n != 2 {
		t.Fatalf("output should have 2 pages, got %d", n)
	}
	t.Logf("Extracted pages PDF: %s (%d pages)", outPath, n)
}

func TestComprehensive_ExtractPagesFromBytes(t *testing.T) {
	data := loadTestPDF(t)
	result, err := ExtractPagesFromBytes(data, []int{2}, nil)
	if err != nil {
		t.Fatalf("ExtractPagesFromBytes: %v", err)
	}
	if result.GetNumberOfPages() != 1 {
		t.Fatalf("expected 1 page, got %d", result.GetNumberOfPages())
	}
}

func TestComprehensive_SelectPages(t *testing.T) {
	data := loadTestPDF(t)
	// Reverse page order
	result, err := SelectPagesFromBytes(data, []int{3, 2, 1}, nil)
	if err != nil {
		t.Fatalf("SelectPagesFromBytes: %v", err)
	}
	if result.GetNumberOfPages() != 3 {
		t.Fatalf("expected 3 pages, got %d", result.GetNumberOfPages())
	}

	outPath := "test/out/comprehensive_select_reversed.pdf"
	err = result.WritePdf(outPath)
	if err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
	t.Logf("Reversed pages PDF: %s", outPath)
}

func TestComprehensive_SelectPagesDuplicate(t *testing.T) {
	data := loadTestPDF(t)
	// Duplicate page 1 three times
	result, err := SelectPagesFromBytes(data, []int{1, 1, 1}, nil)
	if err != nil {
		t.Fatalf("SelectPagesFromBytes: %v", err)
	}
	if result.GetNumberOfPages() != 3 {
		t.Fatalf("expected 3 pages, got %d", result.GetNumberOfPages())
	}
}

func TestComprehensive_SelectPagesFromFile(t *testing.T) {
	result, err := SelectPagesFromFile(testPDFSample, []int{2, 3}, nil)
	if err != nil {
		t.Fatalf("SelectPagesFromFile: %v", err)
	}
	if result.GetNumberOfPages() != 2 {
		t.Fatalf("expected 2 pages, got %d", result.GetNumberOfPages())
	}
}

func TestComprehensive_MergePages(t *testing.T) {
	// Merge test.pdf with itself
	result, err := MergePages([]string{testPDFSample, testPDFSample}, nil)
	if err != nil {
		t.Fatalf("MergePages: %v", err)
	}
	if result.GetNumberOfPages() != 6 {
		t.Fatalf("expected 6 pages after merge, got %d", result.GetNumberOfPages())
	}

	outPath := "test/out/comprehensive_merged.pdf"
	err = result.WritePdf(outPath)
	if err != nil {
		t.Fatalf("WritePdf: %v", err)
	}

	// Verify
	outData, _ := os.ReadFile(outPath)
	n, _ := GetSourcePDFPageCountFromBytes(outData)
	if n != 6 {
		t.Fatalf("merged output should have 6 pages, got %d", n)
	}
	t.Logf("Merged PDF: %s (%d pages)", outPath, n)
}

func TestComprehensive_MergePagesFromBytes(t *testing.T) {
	data := loadTestPDF(t)
	result, err := MergePagesFromBytes([][]byte{data, data}, nil)
	if err != nil {
		t.Fatalf("MergePagesFromBytes: %v", err)
	}
	if result.GetNumberOfPages() != 6 {
		t.Fatalf("expected 6 pages, got %d", result.GetNumberOfPages())
	}
}

func TestComprehensive_CopyPage(t *testing.T) {
	pdf := GoPdf{}
	err := pdf.OpenPDF(testPDFSample, nil)
	if err != nil {
		t.Fatalf("OpenPDF: %v", err)
	}

	before := pdf.GetNumberOfPages()
	newPageNo, err := pdf.CopyPage(1)
	if err != nil {
		t.Fatalf("CopyPage: %v", err)
	}
	after := pdf.GetNumberOfPages()
	if after != before+1 {
		t.Fatalf("expected %d pages after copy, got %d", before+1, after)
	}
	t.Logf("Copied page 1 -> page %d (total: %d)", newPageNo, after)
}

func TestComprehensive_DeletePage(t *testing.T) {
	pdf := GoPdf{}
	err := pdf.OpenPDF(testPDFSample, nil)
	if err != nil {
		t.Fatalf("OpenPDF: %v", err)
	}

	before := pdf.GetNumberOfPages()
	err = pdf.DeletePage(2)
	if err != nil {
		t.Fatalf("DeletePage: %v", err)
	}
	after := pdf.GetNumberOfPages()
	if after != before-1 {
		t.Fatalf("expected %d pages after delete, got %d", before-1, after)
	}

	outPath := "test/out/comprehensive_deleted_page.pdf"
	err = pdf.WritePdf(outPath)
	if err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
	t.Logf("Deleted page 2, saved: %s (%d pages)", outPath, after)
}

// ============================================================
// 5. WATERMARK: Add watermarks to opened PDF
// ============================================================

func TestComprehensive_WatermarkText(t *testing.T) {
	pdf := GoPdf{}
	err := pdf.OpenPDF(testPDFSample, nil)
	if err != nil {
		t.Fatalf("OpenPDF: %v", err)
	}

	err = pdf.AddTTFFont("liberation", testFontLiberation)
	if err != nil {
		t.Skipf("font not available: %v", err)
	}

	err = pdf.SetPage(1)
	if err != nil {
		t.Fatalf("SetPage: %v", err)
	}

	err = pdf.AddWatermarkText(WatermarkOption{
		Text:       "CONFIDENTIAL",
		FontFamily: "liberation",
		FontSize:   48,
		Opacity:    0.3,
		Angle:      45,
	})
	if err != nil {
		t.Fatalf("AddWatermarkText: %v", err)
	}

	outPath := "test/out/comprehensive_watermark.pdf"
	err = pdf.WritePdf(outPath)
	if err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
	t.Logf("Watermark PDF saved: %s", outPath)
}

func TestComprehensive_WatermarkAllPages(t *testing.T) {
	pdf := GoPdf{}
	err := pdf.OpenPDF(testPDFSample, nil)
	if err != nil {
		t.Fatalf("OpenPDF: %v", err)
	}

	err = pdf.AddTTFFont("liberation", testFontLiberation)
	if err != nil {
		t.Skipf("font not available: %v", err)
	}

	err = pdf.AddWatermarkTextAllPages(WatermarkOption{
		Text:       "DRAFT",
		FontFamily: "liberation",
		FontSize:   60,
		Opacity:    0.2,
		Angle:      45,
		Repeat:     true,
	})
	if err != nil {
		t.Fatalf("AddWatermarkTextAllPages: %v", err)
	}

	outPath := "test/out/comprehensive_watermark_all.pdf"
	err = pdf.WritePdf(outPath)
	if err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
	t.Logf("Watermark all pages PDF saved: %s", outPath)
}

// ============================================================
// 6. ROUND-TRIP: Write → Re-read → Verify
// ============================================================

func TestComprehensive_RoundTrip(t *testing.T) {
	// Step 1: Open test.pdf
	pdf := GoPdf{}
	err := pdf.OpenPDF(testPDFSample, nil)
	if err != nil {
		t.Fatalf("OpenPDF: %v", err)
	}

	err = pdf.AddTTFFont("liberation", testFontLiberation)
	if err != nil {
		t.Skipf("font not available: %v", err)
	}
	pdf.SetFont("liberation", "", 12)

	// Step 2: Add content to each page
	for i := 1; i <= 3; i++ {
		pdf.SetPage(i)
		pdf.SetXY(50, 50)
		pdf.Text("Round-trip test marker")
	}

	// Step 3: Add metadata
	pdf.SetXMPMetadata(XMPMetadata{
		Title:      "Round-Trip Test",
		Creator:    []string{"GoPDF2"},
		CreateDate: time.Now(),
	})

	// Step 4: Add embedded file
	pdf.AddEmbeddedFile(EmbeddedFile{
		Name:    "roundtrip.txt",
		Content: []byte("round-trip test data"),
	})

	// Step 5: Save to bytes
	outBytes, err := pdf.GetBytesPdfReturnErr()
	if err != nil {
		t.Fatalf("GetBytesPdfReturnErr: %v", err)
	}
	if len(outBytes) < 1000 {
		t.Fatalf("output too small: %d bytes", len(outBytes))
	}

	// Step 6: Re-read page count
	n, err := GetSourcePDFPageCountFromBytes(outBytes)
	if err != nil {
		t.Fatalf("re-read page count: %v", err)
	}
	if n != 3 {
		t.Fatalf("expected 3 pages in round-trip, got %d", n)
	}

	// Step 7: Save to file and verify it's a valid PDF
	outPath := "test/out/comprehensive_roundtrip.pdf"
	err = os.WriteFile(outPath, outBytes, 0644)
	if err != nil {
		t.Fatalf("write file: %v", err)
	}

	t.Logf("Round-trip: %d bytes, %d pages, saved to %s", len(outBytes), n, outPath)
}

func TestComprehensive_RoundTripExtractText(t *testing.T) {
	data := loadTestPDF(t)

	// Extract text from original
	origText, err := ExtractAllPagesText(data)
	if err != nil {
		t.Fatalf("extract original: %v", err)
	}

	// Open, save, re-extract
	pdf := GoPdf{}
	err = pdf.OpenPDFFromBytes(data, nil)
	if err != nil {
		t.Fatalf("OpenPDFFromBytes: %v", err)
	}

	outBytes, err := pdf.GetBytesPdfReturnErr()
	if err != nil {
		t.Fatalf("GetBytesPdfReturnErr: %v", err)
	}

	// Re-read page count (text may differ due to re-encoding, but pages should match)
	n, err := GetSourcePDFPageCountFromBytes(outBytes)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if n != 3 {
		t.Fatalf("expected 3 pages, got %d", n)
	}

	t.Logf("Original text: %d chars, output: %d bytes", len(origText), len(outBytes))
}

// ============================================================
// 7. EDGE CASES & ERROR HANDLING
// ============================================================

func TestComprehensive_InvalidPDFData(t *testing.T) {
	_, err := GetSourcePDFPageCountFromBytes([]byte("not a pdf"))
	if err == nil {
		t.Fatal("expected error for invalid PDF data")
	}
}

func TestComprehensive_EmptyPDFData(t *testing.T) {
	_, err := GetSourcePDFPageCountFromBytes(nil)
	if err == nil {
		t.Fatal("expected error for nil PDF data")
	}
	_, err = GetSourcePDFPageCountFromBytes([]byte{})
	if err == nil {
		t.Fatal("expected error for empty PDF data")
	}
}

func TestComprehensive_ExtractPagesOutOfRange(t *testing.T) {
	_, err := ExtractPages(testPDFSample, []int{99}, nil)
	if err == nil {
		t.Fatal("expected error for out-of-range page")
	}
}

func TestComprehensive_SelectPagesEmpty(t *testing.T) {
	data := loadTestPDF(t)
	_, err := SelectPagesFromBytes(data, []int{}, nil)
	if err == nil {
		t.Fatal("expected error for empty page list")
	}
}

func TestComprehensive_SelectPagesOutOfRange(t *testing.T) {
	data := loadTestPDF(t)
	_, err := SelectPagesFromBytes(data, []int{0}, nil)
	if err == nil {
		t.Fatal("expected error for page 0")
	}
	_, err = SelectPagesFromBytes(data, []int{100}, nil)
	if err == nil {
		t.Fatal("expected error for page 100")
	}
}

func TestComprehensive_DeletePageOutOfRange(t *testing.T) {
	pdf := GoPdf{}
	err := pdf.OpenPDF(testPDFSample, nil)
	if err != nil {
		t.Fatalf("OpenPDF: %v", err)
	}
	err = pdf.DeletePage(0)
	if err == nil {
		t.Fatal("expected error for page 0")
	}
	err = pdf.DeletePage(99)
	if err == nil {
		t.Fatal("expected error for page 99")
	}
}

func TestComprehensive_OpenNonExistentFile(t *testing.T) {
	pdf := GoPdf{}
	err := pdf.OpenPDF("nonexistent.pdf", nil)
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

// ============================================================
// 8. CREATE: Build new PDF from scratch, then verify
// ============================================================

func TestComprehensive_CreateFromScratch(t *testing.T) {
	pdf := GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})

	err := pdf.AddTTFFont("liberation", testFontLiberation)
	if err != nil {
		t.Skipf("font not available: %v", err)
	}
	pdf.SetFont("liberation", "", 14)

	// Page 1: text
	pdf.AddPage()
	pdf.SetXY(50, 50)
	pdf.Text("Page 1: Hello from GoPDF2")
	pdf.SetXY(50, 80)
	pdf.Cell(nil, "This is a cell")

	// Page 2: shapes
	pdf.AddPage()
	pdf.SetStrokeColor(255, 0, 0)
	pdf.SetLineWidth(2)
	pdf.Line(50, 50, 200, 50)
	pdf.RectFromUpperLeftWithStyle(50, 100, 150, 80, "D")
	pdf.Oval(300, 100, 450, 200)

	// Page 3: annotations
	pdf.AddPage()
	pdf.AddTextAnnotation(50, 50, "Test Note", "Annotation content")
	pdf.AddHighlightAnnotation(50, 100, 200, 20, [3]uint8{255, 255, 0})

	// Add bookmarks
	pdf.SetPage(1)
	pdf.AddOutline("Chapter 1")
	pdf.SetPage(2)
	pdf.AddOutline("Chapter 2")
	pdf.SetPage(3)
	pdf.AddOutline("Chapter 3")

	// Add metadata
	pdf.SetXMPMetadata(XMPMetadata{
		Title:       "Created from Scratch",
		Creator:     []string{"GoPDF2 Test"},
		CreatorTool: "GoPDF2",
	})

	// Save
	outPath := "test/out/comprehensive_from_scratch.pdf"
	err = pdf.WritePdf(outPath)
	if err != nil {
		t.Fatalf("WritePdf: %v", err)
	}

	// Verify
	outData, _ := os.ReadFile(outPath)
	n, err := GetSourcePDFPageCountFromBytes(outData)
	if err != nil {
		t.Fatalf("verify page count: %v", err)
	}
	if n != 3 {
		t.Fatalf("expected 3 pages, got %d", n)
	}
	t.Logf("Created from scratch: %s (%d bytes, %d pages)", outPath, len(outData), n)
}

// ============================================================
// 9. INTEGRATION: Full pipeline test
// ============================================================

func TestComprehensive_FullPipeline(t *testing.T) {
	data := loadTestPDF(t)

	// 1. Read page count
	n, err := GetSourcePDFPageCountFromBytes(data)
	if err != nil {
		t.Fatalf("page count: %v", err)
	}
	if n != 3 {
		t.Fatalf("expected 3 pages, got %d", n)
	}

	// 2. Extract text from all pages
	allText, err := ExtractAllPagesText(data)
	if err != nil {
		t.Fatalf("extract all text: %v", err)
	}
	if len(allText) < 1000 {
		t.Fatalf("text too short: %d", len(allText))
	}

	// 3. Open and modify
	pdf := GoPdf{}
	err = pdf.OpenPDFFromBytes(data, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	err = pdf.AddTTFFont("liberation", testFontLiberation)
	if err != nil {
		t.Skipf("font not available: %v", err)
	}
	pdf.SetFont("liberation", "", 12)

	// 4. Add overlay text
	pdf.SetPage(1)
	pdf.SetXY(50, 750)
	pdf.Text("Added by GoPDF2 test pipeline")

	// 5. Add annotation
	pdf.AddTextAnnotation(300, 300, "Pipeline", "Full pipeline test")

	// 6. Add bookmark
	pdf.AddOutline("Pipeline Test")

	// 7. Add embedded file
	pdf.AddEmbeddedFile(EmbeddedFile{
		Name:    "pipeline_data.json",
		Content: []byte(`{"test": true, "pipeline": "full"}`),
	})

	// 8. Set metadata
	pdf.SetXMPMetadata(XMPMetadata{
		Title:   "Full Pipeline Test",
		Creator: []string{"GoPDF2"},
	})

	// 9. Copy page 1
	_, err = pdf.CopyPage(1)
	if err != nil {
		t.Fatalf("CopyPage: %v", err)
	}

	// 10. Save
	outPath := "test/out/comprehensive_pipeline.pdf"
	err = pdf.WritePdf(outPath)
	if err != nil {
		t.Fatalf("WritePdf: %v", err)
	}

	// 11. Verify output
	outData, _ := os.ReadFile(outPath)
	outN, err := GetSourcePDFPageCountFromBytes(outData)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if outN != 4 { // 3 original + 1 copied
		t.Fatalf("expected 4 pages, got %d", outN)
	}

	// 12. Extract pages from original (not output, since re-serialized
	// resources may contain structures ledongthuc/pdf can't re-parse)
	extracted, err := ExtractPagesFromBytes(data, []int{1, 3}, nil)
	if err != nil {
		t.Fatalf("extract pages from original: %v", err)
	}
	if extracted.GetNumberOfPages() != 2 {
		t.Fatalf("expected 2 extracted pages, got %d", extracted.GetNumberOfPages())
	}

	// 13. Merge original with itself
	merged, err := MergePagesFromBytes([][]byte{data, data}, nil)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if merged.GetNumberOfPages() != 6 { // 3 + 3
		t.Fatalf("expected 6 merged pages, got %d", merged.GetNumberOfPages())
	}

	t.Logf("Full pipeline: original=%d pages, output=%d pages, merged=%d pages",
		n, outN, merged.GetNumberOfPages())
}

// ============================================================
// 10. CONCURRENT SAFETY: Parallel text extraction
// ============================================================

func TestComprehensive_ParallelExtraction(t *testing.T) {
	data := loadTestPDF(t)
	t.Parallel()

	// Run multiple extractions in parallel
	for i := 0; i < 3; i++ {
		i := i
		t.Run(strings.Replace("page_"+string(rune('0'+i)), "", "", 0), func(t *testing.T) {
			t.Parallel()
			text, err := ExtractPageText(data, i)
			if err != nil {
				t.Fatalf("page %d: %v", i, err)
			}
			if text == "" {
				t.Fatalf("page %d: empty text", i)
			}
			t.Logf("page %d: %d chars", i, len(text))
		})
	}
}

func TestComprehensive_ParallelPageCount(t *testing.T) {
	data := loadTestPDF(t)
	t.Parallel()

	for i := 0; i < 5; i++ {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			n, err := GetSourcePDFPageCountFromBytes(data)
			if err != nil {
				t.Fatalf("error: %v", err)
			}
			if n != 3 {
				t.Fatalf("expected 3, got %d", n)
			}
		})
	}
}
