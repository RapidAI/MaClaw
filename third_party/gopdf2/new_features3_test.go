package gopdf

import (
	"os"
	"strings"
	"testing"
	"time"
)

// ============================================================
// Tests for Round 3 features: Content Element CRUD,
// PDF Version, Garbage Collection, Page Labels, ObjID
// ============================================================

// ============================================================
// ObjID Tests
// ============================================================

func TestObjID(t *testing.T) {
	id := ObjID(4)
	if id.Index() != 4 {
		t.Errorf("Index() = %d, want 4", id.Index())
	}
	if id.Ref() != 5 {
		t.Errorf("Ref() = %d, want 5", id.Ref())
	}
	if id.RefStr() != "5 0 R" {
		t.Errorf("RefStr() = %q, want %q", id.RefStr(), "5 0 R")
	}
	if !id.IsValid() {
		t.Error("IsValid() should be true for id=4")
	}
	if invalidObjID.IsValid() {
		t.Error("invalidObjID should not be valid")
	}
}

func TestNullObj(t *testing.T) {
	n := nullObj{}
	if n.getType() != "Null" {
		t.Errorf("getType() = %q, want %q", n.getType(), "Null")
	}
}

// ============================================================
// PDF Version Tests
// ============================================================

func TestPDFVersion(t *testing.T) {
	tests := []struct {
		v      PDFVersion
		str    string
		header string
	}{
		{PDFVersion14, "1.4", "%PDF-1.4"},
		{PDFVersion15, "1.5", "%PDF-1.5"},
		{PDFVersion16, "1.6", "%PDF-1.6"},
		{PDFVersion17, "1.7", "%PDF-1.7"},
		{PDFVersion20, "2.0", "%PDF-2.0"},
	}
	for _, tt := range tests {
		if tt.v.String() != tt.str {
			t.Errorf("PDFVersion(%d).String() = %q, want %q", tt.v, tt.v.String(), tt.str)
		}
		if tt.v.Header() != tt.header {
			t.Errorf("PDFVersion(%d).Header() = %q, want %q", tt.v, tt.v.Header(), tt.header)
		}
	}
}

func TestSetGetPDFVersion(t *testing.T) {
	pdf := GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})

	// Default should be 1.7.
	if pdf.GetPDFVersion() != PDFVersion17 {
		t.Errorf("default version = %v, want PDFVersion17", pdf.GetPDFVersion())
	}

	pdf.SetPDFVersion(PDFVersion20)
	if pdf.GetPDFVersion() != PDFVersion20 {
		t.Errorf("after set = %v, want PDFVersion20", pdf.GetPDFVersion())
	}
}

// ============================================================
// Garbage Collection Tests
// ============================================================

func TestGarbageCollectNoop(t *testing.T) {
	pdf := GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	removed := pdf.GarbageCollect(GCNone)
	if removed != 0 {
		t.Errorf("GCNone should remove 0, got %d", removed)
	}
}

func TestGarbageCollectAfterDelete(t *testing.T) {
	pdf := GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})

	pdf.AddTTFFont("liberation", "test/res/LiberationSerif-Regular.ttf")
	pdf.SetFont("liberation", "", 14)

	pdf.AddPage()
	pdf.SetX(50)
	pdf.SetY(50)
	pdf.Text("Page 1")

	pdf.AddPage()
	pdf.SetX(50)
	pdf.SetY(50)
	pdf.Text("Page 2")

	pdf.AddPage()
	pdf.SetX(50)
	pdf.SetY(50)
	pdf.Text("Page 3")

	totalBefore := pdf.GetObjectCount()
	liveBefore := pdf.GetLiveObjectCount()

	if totalBefore != liveBefore {
		t.Errorf("before delete: total=%d live=%d should be equal", totalBefore, liveBefore)
	}

	err := pdf.DeletePage(2)
	if err != nil {
		t.Fatalf("DeletePage: %v", err)
	}

	// After delete, total should be same but live should be less.
	if pdf.GetObjectCount() != totalBefore {
		t.Errorf("after delete: total changed from %d to %d", totalBefore, pdf.GetObjectCount())
	}
	if pdf.GetLiveObjectCount() >= liveBefore {
		t.Errorf("after delete: live should decrease, was %d now %d", liveBefore, pdf.GetLiveObjectCount())
	}

	removed := pdf.GarbageCollect(GCCompact)
	if removed == 0 {
		t.Error("GCCompact should remove deleted objects")
	}

	if pdf.GetObjectCount() != pdf.GetLiveObjectCount() {
		t.Errorf("after GC: total=%d live=%d should be equal", pdf.GetObjectCount(), pdf.GetLiveObjectCount())
	}
}

// ============================================================
// Page Labels Tests
// ============================================================

func TestPageLabels(t *testing.T) {
	pdf := GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	pdf.AddPage()
	pdf.AddPage()

	labels := []PageLabel{
		{PageIndex: 0, Style: PageLabelRomanLower, Start: 1},
		{PageIndex: 2, Style: PageLabelDecimal, Start: 1},
	}
	pdf.SetPageLabels(labels)

	got := pdf.GetPageLabels()
	if len(got) != 2 {
		t.Fatalf("GetPageLabels() len = %d, want 2", len(got))
	}
	if got[0].Style != PageLabelRomanLower {
		t.Errorf("label[0].Style = %q, want %q", got[0].Style, PageLabelRomanLower)
	}
	if got[1].PageIndex != 2 {
		t.Errorf("label[1].PageIndex = %d, want 2", got[1].PageIndex)
	}

	// Verify it can be written to PDF without error.
	pdf.AddTTFFont("liberation", "test/res/LiberationSerif-Regular.ttf")
	pdf.SetFont("liberation", "", 14)
	pdf.SetPage(1)
	pdf.SetX(50)
	pdf.SetY(50)
	pdf.Text("Page i")

	err := pdf.WritePdf("test/out/test_page_labels.pdf")
	if err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
}

// ============================================================
// Content Element CRUD Tests
// ============================================================

func helperCreateTestPDF(t *testing.T) *GoPdf {
	t.Helper()
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})

	err := pdf.AddTTFFont("liberation", "test/res/LiberationSerif-Regular.ttf")
	if err != nil {
		t.Fatalf("AddTTFFont: %v", err)
	}
	if err := pdf.SetFont("liberation", "", 14); err != nil {
		t.Fatalf("SetFont: %v", err)
	}

	pdf.AddPage()

	// Add various elements.
	pdf.SetX(50)
	pdf.SetY(50)
	pdf.Text("Hello World")

	pdf.Line(10, 100, 200, 100)
	pdf.Line(10, 110, 200, 110)

	pdf.RectFromUpperLeftWithStyle(50, 150, 100, 80, "D")

	pdf.Oval(50, 250, 150, 300)

	return pdf
}

func TestGetPageElements(t *testing.T) {
	pdf := helperCreateTestPDF(t)

	elements, err := pdf.GetPageElements(1)
	if err != nil {
		t.Fatalf("GetPageElements: %v", err)
	}

	if len(elements) == 0 {
		t.Fatal("expected elements on page 1, got 0")
	}

	// Check that we have at least one text, two lines, one rect, one oval.
	counts := map[ContentElementType]int{}
	for _, e := range elements {
		counts[e.Type]++
	}

	if counts[ElementText] < 1 {
		t.Errorf("expected at least 1 text element, got %d", counts[ElementText])
	}
	if counts[ElementLine] < 2 {
		t.Errorf("expected at least 2 line elements, got %d", counts[ElementLine])
	}
	if counts[ElementRectangle] < 1 {
		t.Errorf("expected at least 1 rectangle element, got %d", counts[ElementRectangle])
	}
	if counts[ElementOval] < 1 {
		t.Errorf("expected at least 1 oval element, got %d", counts[ElementOval])
	}
}

func TestGetPageElementsByType(t *testing.T) {
	pdf := helperCreateTestPDF(t)

	lines, err := pdf.GetPageElementsByType(1, ElementLine)
	if err != nil {
		t.Fatalf("GetPageElementsByType: %v", err)
	}
	if len(lines) < 2 {
		t.Errorf("expected at least 2 lines, got %d", len(lines))
	}
	for _, l := range lines {
		if l.Type != ElementLine {
			t.Errorf("expected ElementLine, got %s", l.Type)
		}
	}
}

func TestGetPageElementCount(t *testing.T) {
	pdf := helperCreateTestPDF(t)

	count, err := pdf.GetPageElementCount(1)
	if err != nil {
		t.Fatalf("GetPageElementCount: %v", err)
	}
	if count == 0 {
		t.Error("expected non-zero element count")
	}
}

func TestDeleteElement(t *testing.T) {
	pdf := helperCreateTestPDF(t)

	countBefore, _ := pdf.GetPageElementCount(1)

	err := pdf.DeleteElement(1, 0)
	if err != nil {
		t.Fatalf("DeleteElement: %v", err)
	}

	countAfter, _ := pdf.GetPageElementCount(1)
	if countAfter != countBefore-1 {
		t.Errorf("after delete: count = %d, want %d", countAfter, countBefore-1)
	}

	// Out of range.
	err = pdf.DeleteElement(1, 9999)
	if err == nil {
		t.Error("expected error for out-of-range index")
	}
}

func TestDeleteElementsByType(t *testing.T) {
	pdf := helperCreateTestPDF(t)

	removed, err := pdf.DeleteElementsByType(1, ElementLine)
	if err != nil {
		t.Fatalf("DeleteElementsByType: %v", err)
	}
	if removed < 2 {
		t.Errorf("expected at least 2 lines removed, got %d", removed)
	}

	// Verify no lines remain.
	lines, _ := pdf.GetPageElementsByType(1, ElementLine)
	if len(lines) != 0 {
		t.Errorf("expected 0 lines after delete, got %d", len(lines))
	}
}

func TestDeleteElementsInRect(t *testing.T) {
	pdf := helperCreateTestPDF(t)

	// Delete elements in the area where lines start (x=10, y=100..110).
	removed, err := pdf.DeleteElementsInRect(1, 0, 90, 30, 30)
	if err != nil {
		t.Fatalf("DeleteElementsInRect: %v", err)
	}
	if removed < 2 {
		t.Errorf("expected at least 2 elements removed in rect, got %d", removed)
	}
}

func TestClearPage(t *testing.T) {
	pdf := helperCreateTestPDF(t)

	err := pdf.ClearPage(1)
	if err != nil {
		t.Fatalf("ClearPage: %v", err)
	}

	count, _ := pdf.GetPageElementCount(1)
	if count != 0 {
		t.Errorf("after ClearPage: count = %d, want 0", count)
	}
}

func TestModifyTextElement(t *testing.T) {
	pdf := helperCreateTestPDF(t)

	// Find the first text element.
	elements, _ := pdf.GetPageElements(1)
	textIdx := -1
	for _, e := range elements {
		if e.Type == ElementText {
			textIdx = e.Index
			break
		}
	}
	if textIdx < 0 {
		t.Fatal("no text element found")
	}

	err := pdf.ModifyTextElement(1, textIdx, "Modified Text")
	if err != nil {
		t.Fatalf("ModifyTextElement: %v", err)
	}

	// Verify the change.
	elements, _ = pdf.GetPageElements(1)
	if elements[textIdx].Text != "Modified Text" {
		t.Errorf("text = %q, want %q", elements[textIdx].Text, "Modified Text")
	}

	// Type mismatch: try to modify a non-text element.
	lineIdx := -1
	for _, e := range elements {
		if e.Type == ElementLine {
			lineIdx = e.Index
			break
		}
	}
	if lineIdx >= 0 {
		err = pdf.ModifyTextElement(1, lineIdx, "oops")
		if err == nil {
			t.Error("expected error when modifying non-text element as text")
		}
	}
}

func TestModifyElementPosition(t *testing.T) {
	pdf := helperCreateTestPDF(t)

	elements, _ := pdf.GetPageElements(1)
	// Find a line element.
	lineIdx := -1
	for _, e := range elements {
		if e.Type == ElementLine {
			lineIdx = e.Index
			break
		}
	}
	if lineIdx < 0 {
		t.Fatal("no line element found")
	}

	err := pdf.ModifyElementPosition(1, lineIdx, 50, 500)
	if err != nil {
		t.Fatalf("ModifyElementPosition: %v", err)
	}

	elements, _ = pdf.GetPageElements(1)
	if elements[lineIdx].X != 50 || elements[lineIdx].Y != 500 {
		t.Errorf("position = (%.1f, %.1f), want (50, 500)", elements[lineIdx].X, elements[lineIdx].Y)
	}
}

func TestInsertLineElement(t *testing.T) {
	pdf := helperCreateTestPDF(t)

	countBefore, _ := pdf.GetPageElementCount(1)

	err := pdf.InsertLineElement(1, 0, 400, 500, 400)
	if err != nil {
		t.Fatalf("InsertLineElement: %v", err)
	}

	countAfter, _ := pdf.GetPageElementCount(1)
	if countAfter != countBefore+1 {
		t.Errorf("after insert: count = %d, want %d", countAfter, countBefore+1)
	}
}

func TestInsertRectElement(t *testing.T) {
	pdf := helperCreateTestPDF(t)

	countBefore, _ := pdf.GetPageElementCount(1)

	err := pdf.InsertRectElement(1, 200, 200, 50, 30, "DF")
	if err != nil {
		t.Fatalf("InsertRectElement: %v", err)
	}

	countAfter, _ := pdf.GetPageElementCount(1)
	if countAfter != countBefore+1 {
		t.Errorf("after insert: count = %d, want %d", countAfter, countBefore+1)
	}
}

func TestInsertOvalElement(t *testing.T) {
	pdf := helperCreateTestPDF(t)

	countBefore, _ := pdf.GetPageElementCount(1)

	err := pdf.InsertOvalElement(1, 300, 300, 400, 350)
	if err != nil {
		t.Fatalf("InsertOvalElement: %v", err)
	}

	countAfter, _ := pdf.GetPageElementCount(1)
	if countAfter != countBefore+1 {
		t.Errorf("after insert: count = %d, want %d", countAfter, countBefore+1)
	}
}

func TestInsertElementAt(t *testing.T) {
	pdf := helperCreateTestPDF(t)

	countBefore, _ := pdf.GetPageElementCount(1)

	newLine := &cacheContentLine{
		pageHeight: 842,
		x1:         0, y1: 0,
		x2: 100, y2: 100,
	}
	err := pdf.InsertElementAt(1, 0, newLine)
	if err != nil {
		t.Fatalf("InsertElementAt: %v", err)
	}

	countAfter, _ := pdf.GetPageElementCount(1)
	if countAfter != countBefore+1 {
		t.Errorf("after insert at: count = %d, want %d", countAfter, countBefore+1)
	}

	// The first element should now be a line.
	elements, _ := pdf.GetPageElements(1)
	if elements[0].Type != ElementLine {
		t.Errorf("first element type = %s, want Line", elements[0].Type)
	}
}

func TestReplaceElement(t *testing.T) {
	pdf := helperCreateTestPDF(t)

	// Replace the first element with a line.
	newLine := &cacheContentLine{
		pageHeight: 842,
		x1:         0, y1: 0,
		x2: 50, y2: 50,
	}
	err := pdf.ReplaceElement(1, 0, newLine)
	if err != nil {
		t.Fatalf("ReplaceElement: %v", err)
	}

	elements, _ := pdf.GetPageElements(1)
	if elements[0].Type != ElementLine {
		t.Errorf("replaced element type = %s, want Line", elements[0].Type)
	}
}

func TestContentElementCRUDInvalidPage(t *testing.T) {
	pdf := GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	_, err := pdf.GetPageElements(99)
	if err == nil {
		t.Error("expected error for invalid page")
	}

	err = pdf.DeleteElement(99, 0)
	if err == nil {
		t.Error("expected error for invalid page")
	}

	err = pdf.ClearPage(99)
	if err == nil {
		t.Error("expected error for invalid page")
	}

	err = pdf.InsertLineElement(99, 0, 0, 10, 10)
	if err == nil {
		t.Error("expected error for invalid page")
	}
}

func TestContentElementTypeString(t *testing.T) {
	if ElementText.String() != "Text" {
		t.Errorf("ElementText.String() = %q", ElementText.String())
	}
	if ElementUnknown.String() != "Unknown" {
		t.Errorf("ElementUnknown.String() = %q", ElementUnknown.String())
	}
}

// ============================================================
// Integration: Write PDF with CRUD modifications
// ============================================================

func TestContentElementCRUDWritePDF(t *testing.T) {
	pdf := helperCreateTestPDF(t)

	// Delete all lines.
	pdf.DeleteElementsByType(1, ElementLine)

	// Insert new elements.
	pdf.InsertLineElement(1, 50, 400, 500, 400)
	pdf.InsertRectElement(1, 50, 420, 200, 50, "DF")
	pdf.InsertOvalElement(1, 300, 420, 450, 470)

	// Modify text.
	elements, _ := pdf.GetPageElements(1)
	for _, e := range elements {
		if e.Type == ElementText {
			pdf.ModifyTextElement(1, e.Index, "CRUD Modified")
			break
		}
	}

	// Move the rectangle.
	elements, _ = pdf.GetPageElements(1)
	for _, e := range elements {
		if e.Type == ElementRectangle {
			pdf.ModifyElementPosition(1, e.Index, 100, 500)
			break
		}
	}

	os.MkdirAll("test/out", 0755)
	err := pdf.WritePdf("test/out/test_content_crud.pdf")
	if err != nil {
		t.Fatalf("WritePdf: %v", err)
	}

	// Verify file exists and is non-empty.
	info, err := os.Stat("test/out/test_content_crud.pdf")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Size() == 0 {
		t.Error("output PDF is empty")
	}
}

func TestGarbageCollectWritePDF(t *testing.T) {
	pdf := GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})

	pdf.AddTTFFont("liberation", "test/res/LiberationSerif-Regular.ttf")
	pdf.SetFont("liberation", "", 14)

	pdf.AddPage()
	pdf.SetX(50)
	pdf.SetY(50)
	pdf.Text("Page 1")

	pdf.AddPage()
	pdf.SetX(50)
	pdf.SetY(50)
	pdf.Text("Page 2 - will be deleted")

	pdf.AddPage()
	pdf.SetX(50)
	pdf.SetY(50)
	pdf.Text("Page 3")

	// Delete page 2 and compact.
	pdf.DeletePage(2)
	removed := pdf.GarbageCollect(GCCompact)
	if removed == 0 {
		t.Error("expected objects to be removed")
	}

	os.MkdirAll("test/out", 0755)
	err := pdf.WritePdf("test/out/test_gc_compact.pdf")
	if err != nil {
		t.Fatalf("WritePdf: %v", err)
	}

	info, err := os.Stat("test/out/test_gc_compact.pdf")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Size() == 0 {
		t.Error("output PDF is empty")
	}
}

func TestPDFVersionWritePDF(t *testing.T) {
	pdf := GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.SetPDFVersion(PDFVersion20)

	pdf.AddTTFFont("liberation", "test/res/LiberationSerif-Regular.ttf")
	pdf.SetFont("liberation", "", 14)

	pdf.AddPage()
	pdf.SetX(50)
	pdf.SetY(50)
	pdf.Text("PDF 2.0 document")

	os.MkdirAll("test/out", 0755)
	err := pdf.WritePdf("test/out/test_pdf_version.pdf")
	if err != nil {
		t.Fatalf("WritePdf: %v", err)
	}

	// Read back and check header.
	data, err := os.ReadFile("test/out/test_pdf_version.pdf")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	header := string(data[:8])
	if header != "%PDF-2.0" {
		t.Errorf("header = %q, want %%PDF-2.0", header)
	}
}

// ============================================================
// XMP Metadata Tests
// ============================================================

func TestXMPMetadata(t *testing.T) {
	pdf := GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})

	meta := XMPMetadata{
		Title:       "Test Document",
		Creator:     []string{"Author One", "Author Two"},
		Description: "A test PDF with XMP metadata",
		Subject:     []string{"testing", "metadata"},
		Rights:      "Copyright 2025",
		Language:    "en-US",
		CreatorTool: "GoPDF2",
		Producer:    "GoPDF2",
		CreateDate:  time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
		ModifyDate:  time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC),
		Keywords:    "test, pdf, xmp",
		PDFAPart:    1,
		PDFAConformance: "B",
	}
	pdf.SetXMPMetadata(meta)

	got := pdf.GetXMPMetadata()
	if got == nil {
		t.Fatal("GetXMPMetadata returned nil")
	}
	if got.Title != "Test Document" {
		t.Errorf("Title = %q, want %q", got.Title, "Test Document")
	}
	if len(got.Creator) != 2 {
		t.Errorf("Creator len = %d, want 2", len(got.Creator))
	}

	pdf.AddTTFFont("liberation", "test/res/LiberationSerif-Regular.ttf")
	pdf.SetFont("liberation", "", 14)
	pdf.AddPage()
	pdf.SetX(50)
	pdf.SetY(50)
	pdf.Text("Document with XMP metadata")

	os.MkdirAll("test/out", 0755)
	err := pdf.WritePdf("test/out/test_xmp_metadata.pdf")
	if err != nil {
		t.Fatalf("WritePdf: %v", err)
	}

	// Verify file contains XMP data.
	data, _ := os.ReadFile("test/out/test_xmp_metadata.pdf")
	content := string(data)
	if !contains(content, "x:xmpmeta") {
		t.Error("output PDF does not contain XMP metadata")
	}
	if !contains(content, "Test Document") {
		t.Error("output PDF does not contain title in XMP")
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && strings.Contains(s, substr)
}

// ============================================================
// Incremental Save Tests
// ============================================================

func TestIncrementalSave(t *testing.T) {
	// Create a base PDF.
	pdf := GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddTTFFont("liberation", "test/res/LiberationSerif-Regular.ttf")
	pdf.SetFont("liberation", "", 14)
	pdf.AddPage()
	pdf.SetX(50)
	pdf.SetY(50)
	pdf.Text("Original content")

	originalData, err := pdf.GetBytesPdfReturnErr()
	if err != nil {
		t.Fatalf("GetBytesPdfReturnErr: %v", err)
	}

	// Now do an incremental save (full).
	result, err := pdf.IncrementalSave(originalData, nil)
	if err != nil {
		t.Fatalf("IncrementalSave: %v", err)
	}

	// Result should be larger than original (appended data).
	if len(result) <= len(originalData) {
		t.Errorf("incremental result (%d bytes) should be larger than original (%d bytes)",
			len(result), len(originalData))
	}

	// Should still start with %PDF.
	if string(result[:5]) != "%PDF-" {
		t.Errorf("result doesn't start with %%PDF-, got %q", string(result[:5]))
	}

	// Should end with %%EOF.
	tail := string(result[len(result)-6:])
	if !strings.Contains(tail, "%%EOF") {
		t.Errorf("result doesn't end with %%%%EOF, tail = %q", tail)
	}

	os.MkdirAll("test/out", 0755)
	os.WriteFile("test/out/test_incremental.pdf", result, 0644)
}

// ============================================================
// Document Clone Tests
// ============================================================

func TestClone(t *testing.T) {
	// Create original document.
	original := GoPdf{}
	original.Start(Config{PageSize: *PageSizeA4})
	original.AddTTFFont("liberation", "test/res/LiberationSerif-Regular.ttf")
	original.SetFont("liberation", "", 14)
	original.AddPage()
	original.SetX(50)
	original.SetY(50)
	original.Text("Original document")
	original.SetPDFVersion(PDFVersion20)

	clone, err := original.Clone()
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}

	// Clone should be independent.
	if clone == nil {
		t.Fatal("Clone returned nil")
	}

	// Clone should have the same version.
	if clone.GetPDFVersion() != PDFVersion20 {
		t.Errorf("clone version = %v, want PDFVersion20", clone.GetPDFVersion())
	}

	// Clone should have pages.
	if clone.GetNumberOfPages() == 0 {
		t.Error("clone has 0 pages")
	}

	os.MkdirAll("test/out", 0755)
	err = clone.WritePdf("test/out/test_clone.pdf")
	if err != nil {
		t.Fatalf("clone WritePdf: %v", err)
	}

	info, _ := os.Stat("test/out/test_clone.pdf")
	if info.Size() == 0 {
		t.Error("clone PDF is empty")
	}
}

// ============================================================
// ObjID Extended Tests
// ============================================================

func TestObjIDMethods(t *testing.T) {
	pdf := GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	// CatalogObjID should be valid.
	catID := pdf.CatalogObjID()
	if !catID.IsValid() {
		t.Error("CatalogObjID is not valid")
	}
	if pdf.GetObjType(catID) != "Catalog" {
		t.Errorf("CatalogObj type = %q, want %q", pdf.GetObjType(catID), "Catalog")
	}

	// PagesObjID should be valid.
	pagesID := pdf.PagesObjID()
	if !pagesID.IsValid() {
		t.Error("PagesObjID is not valid")
	}

	// GetObjByID for invalid ID.
	obj := pdf.GetObjByID(invalidObjID)
	if obj != nil {
		t.Error("GetObjByID(invalidObjID) should return nil")
	}

	// GetObjID out of range.
	id := pdf.GetObjID(99999)
	if id.IsValid() {
		t.Error("GetObjID(99999) should return invalidObjID")
	}
}

// ============================================================
// GCDedup Test
// ============================================================

func TestGarbageCollectDedup(t *testing.T) {
	pdf := GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddTTFFont("liberation", "test/res/LiberationSerif-Regular.ttf")
	pdf.SetFont("liberation", "", 14)
	pdf.AddPage()
	pdf.SetX(50)
	pdf.SetY(50)
	pdf.Text("Page 1")
	pdf.AddPage()
	pdf.SetX(50)
	pdf.SetY(50)
	pdf.Text("Page 2")

	// Delete a page to create nulls, then dedup.
	pdf.DeletePage(2)
	removed := pdf.GarbageCollect(GCDedup)
	if removed == 0 {
		t.Error("GCDedup should remove at least the deleted objects")
	}

	os.MkdirAll("test/out", 0755)
	err := pdf.WritePdf("test/out/test_gc_dedup.pdf")
	if err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
}
