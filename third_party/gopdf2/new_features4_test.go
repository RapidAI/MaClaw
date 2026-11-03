package gopdf

import (
	"os"
	"testing"
	"time"
)

// ============================================================
// Scrub tests
// ============================================================

func TestScrub_RemovesMetadata(t *testing.T) {
	pdf := setupTestPDF(t)
	pdf.SetInfo(PdfInfo{
		Title:   "Secret Doc",
		Author:  "Agent X",
		Subject: "Classified",
	})
	pdf.SetXMPMetadata(XMPMetadata{
		Title:      "Secret Doc",
		Creator:    []string{"Agent X"},
		CreateDate: time.Now(),
	})
	pdf.SetPageLabels([]PageLabel{
		{PageIndex: 0, Style: PageLabelDecimal, Start: 1},
	})

	// Verify data is set.
	if pdf.xmpMetadata == nil {
		t.Fatal("XMP metadata should be set")
	}
	if len(pdf.pageLabels) == 0 {
		t.Fatal("page labels should be set")
	}

	// Scrub with defaults.
	pdf.Scrub(DefaultScrubOption())

	if pdf.xmpMetadata != nil {
		t.Error("XMP metadata should be nil after scrub")
	}
	if len(pdf.pageLabels) != 0 {
		t.Error("page labels should be empty after scrub")
	}
	if pdf.isUseInfo {
		t.Error("info should be disabled after scrub")
	}

	// Should still produce valid PDF.
	err := pdf.WritePdf("test/out/scrubbed.pdf")
	if err != nil {
		t.Fatal(err)
	}
}

func TestScrub_SelectiveRemoval(t *testing.T) {
	pdf := setupTestPDF(t)
	pdf.SetInfo(PdfInfo{Title: "Keep This"})
	pdf.SetXMPMetadata(XMPMetadata{Title: "Remove This"})

	// Only remove XMP, keep standard metadata.
	pdf.Scrub(ScrubOption{
		XMLMetadata: true,
	})

	if pdf.xmpMetadata != nil {
		t.Error("XMP should be removed")
	}
	// info should still be set (we didn't scrub it).
	if pdf.info == nil {
		t.Error("standard info should be preserved")
	}
}

// ============================================================
// OCG (Optional Content Groups / Layers) tests
// ============================================================

func TestOCG_AddAndList(t *testing.T) {
	pdf := setupTestPDF(t)

	layer1 := pdf.AddOCG(OCG{Name: "Watermark", Intent: OCGIntentView, On: true})
	layer2 := pdf.AddOCG(OCG{Name: "Draft Notes", Intent: OCGIntentDesign, On: false})

	if layer1.Name != "Watermark" {
		t.Errorf("expected 'Watermark', got %q", layer1.Name)
	}
	if layer2.On != false {
		t.Error("layer2 should be off")
	}

	ocgs := pdf.GetOCGs()
	if len(ocgs) != 2 {
		t.Fatalf("expected 2 OCGs, got %d", len(ocgs))
	}
	if ocgs[0].Name != "Watermark" {
		t.Errorf("expected 'Watermark', got %q", ocgs[0].Name)
	}
	if ocgs[1].Name != "Draft Notes" {
		t.Errorf("expected 'Draft Notes', got %q", ocgs[1].Name)
	}

	// Should produce valid PDF with OCProperties.
	err := pdf.WritePdf("test/out/ocg_layers.pdf")
	if err != nil {
		t.Fatal(err)
	}

	// Verify file is non-empty.
	info, _ := os.Stat("test/out/ocg_layers.pdf")
	if info.Size() == 0 {
		t.Error("output PDF is empty")
	}
}

func TestOCG_DefaultIntent(t *testing.T) {
	pdf := setupTestPDF(t)
	layer := pdf.AddOCG(OCG{Name: "Test"})
	if layer.Intent != OCGIntentView {
		t.Errorf("expected default intent View, got %q", layer.Intent)
	}
}

// ============================================================
// PageLayout / PageMode tests
// ============================================================

func TestPageLayout_SetAndGet(t *testing.T) {
	pdf := setupTestPDF(t)

	// Default.
	if pdf.GetPageLayout() != PageLayoutSinglePage {
		t.Errorf("expected default SinglePage, got %q", pdf.GetPageLayout())
	}

	pdf.SetPageLayout(PageLayoutTwoColumnLeft)
	if pdf.GetPageLayout() != PageLayoutTwoColumnLeft {
		t.Errorf("expected TwoColumnLeft, got %q", pdf.GetPageLayout())
	}

	err := pdf.WritePdf("test/out/page_layout.pdf")
	if err != nil {
		t.Fatal(err)
	}
}

func TestPageMode_SetAndGet(t *testing.T) {
	pdf := setupTestPDF(t)

	if pdf.GetPageMode() != PageModeUseNone {
		t.Errorf("expected default UseNone, got %q", pdf.GetPageMode())
	}

	pdf.SetPageMode(PageModeUseThumbs)
	if pdf.GetPageMode() != PageModeUseThumbs {
		t.Errorf("expected UseThumbs, got %q", pdf.GetPageMode())
	}

	err := pdf.WritePdf("test/out/page_mode.pdf")
	if err != nil {
		t.Fatal(err)
	}
}

func TestPageMode_UseOC(t *testing.T) {
	pdf := setupTestPDF(t)
	pdf.AddOCG(OCG{Name: "Layer1", On: true})
	pdf.SetPageMode(PageModeUseOC)

	err := pdf.WritePdf("test/out/page_mode_oc.pdf")
	if err != nil {
		t.Fatal(err)
	}
}

// ============================================================
// Document Statistics tests
// ============================================================

func TestDocumentStats(t *testing.T) {
	pdf := setupTestPDF(t)

	stats := pdf.GetDocumentStats()
	if stats.PageCount != 1 {
		t.Errorf("expected 1 page, got %d", stats.PageCount)
	}
	if stats.ObjectCount == 0 {
		t.Error("expected non-zero object count")
	}
	if stats.HasOutlines {
		t.Error("should not have outlines")
	}
	if stats.HasEmbeddedFiles {
		t.Error("should not have embedded files")
	}
}

func TestDocumentStats_WithFeatures(t *testing.T) {
	pdf := setupTestPDF(t)
	pdf.SetXMPMetadata(XMPMetadata{Title: "Test"})
	pdf.SetPageLabels([]PageLabel{{PageIndex: 0, Style: PageLabelDecimal, Start: 1}})
	pdf.AddOCG(OCG{Name: "Layer", On: true})
	pdf.AddEmbeddedFile(EmbeddedFile{
		Name:    "test.txt",
		Content: []byte("hello"),
	})

	stats := pdf.GetDocumentStats()
	if !stats.HasXMPMetadata {
		t.Error("should have XMP metadata")
	}
	if !stats.HasPageLabels {
		t.Error("should have page labels")
	}
	if !stats.HasOCGs {
		t.Error("should have OCGs")
	}
	if !stats.HasEmbeddedFiles {
		t.Error("should have embedded files")
	}
}

func TestGetFonts(t *testing.T) {
	pdf := GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	err := pdf.AddTTFFont("testfont", "test/res/LiberationSerif-Regular.ttf")
	if err != nil {
		t.Skip("test font not available:", err)
	}
	err = pdf.SetFont("testfont", "", 12)
	if err != nil {
		t.Fatal(err)
	}
	pdf.SetXY(50, 50)
	pdf.Text("Hello")

	fonts := pdf.GetFonts()
	if len(fonts) == 0 {
		t.Error("expected at least 1 font")
	}
	found := false
	for _, f := range fonts {
		if f.Family == "testfont" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find 'testfont' in font list")
	}
}

// ============================================================
// TOC (Table of Contents) tests
// ============================================================

func TestTOC_GetEmpty(t *testing.T) {
	pdf := setupTestPDF(t)
	toc := pdf.GetTOC()
	if len(toc) != 0 {
		t.Errorf("expected empty TOC, got %d items", len(toc))
	}
}

func TestTOC_FlatOutlines(t *testing.T) {
	pdf := GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})

	pdf.AddPage()
	pdf.AddOutline("Chapter 1")

	pdf.AddPage()
	pdf.AddOutline("Chapter 2")

	pdf.AddPage()
	pdf.AddOutline("Chapter 3")

	toc := pdf.GetTOC()
	if len(toc) != 3 {
		t.Fatalf("expected 3 TOC items, got %d", len(toc))
	}
	if toc[0].Title != "Chapter 1" {
		t.Errorf("expected 'Chapter 1', got %q", toc[0].Title)
	}
	if toc[1].Title != "Chapter 2" {
		t.Errorf("expected 'Chapter 2', got %q", toc[1].Title)
	}
	if toc[2].Title != "Chapter 3" {
		t.Errorf("expected 'Chapter 3', got %q", toc[2].Title)
	}

	err := pdf.WritePdf("test/out/toc_flat.pdf")
	if err != nil {
		t.Fatal(err)
	}
}

func TestTOC_SetFlat(t *testing.T) {
	pdf := GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	pdf.AddPage()
	pdf.AddPage()

	err := pdf.SetTOC([]TOCItem{
		{Level: 1, Title: "Introduction", PageNo: 1},
		{Level: 1, Title: "Body", PageNo: 2},
		{Level: 1, Title: "Conclusion", PageNo: 3},
	})
	if err != nil {
		t.Fatal(err)
	}

	toc := pdf.GetTOC()
	if len(toc) != 3 {
		t.Fatalf("expected 3 TOC items, got %d", len(toc))
	}
	if toc[0].Title != "Introduction" {
		t.Errorf("expected 'Introduction', got %q", toc[0].Title)
	}

	err = pdf.WritePdf("test/out/toc_set_flat.pdf")
	if err != nil {
		t.Fatal(err)
	}
}

func TestTOC_SetHierarchical(t *testing.T) {
	pdf := GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	for i := 0; i < 5; i++ {
		pdf.AddPage()
	}

	err := pdf.SetTOC([]TOCItem{
		{Level: 1, Title: "Chapter 1", PageNo: 1},
		{Level: 2, Title: "Section 1.1", PageNo: 1, Y: 200},
		{Level: 2, Title: "Section 1.2", PageNo: 2},
		{Level: 1, Title: "Chapter 2", PageNo: 3},
		{Level: 2, Title: "Section 2.1", PageNo: 3, Y: 100},
		{Level: 3, Title: "Subsection 2.1.1", PageNo: 4},
		{Level: 1, Title: "Chapter 3", PageNo: 5},
	})
	if err != nil {
		t.Fatal(err)
	}

	toc := pdf.GetTOC()
	if len(toc) < 3 {
		t.Fatalf("expected at least 3 TOC items, got %d", len(toc))
	}

	err = pdf.WritePdf("test/out/toc_hierarchical.pdf")
	if err != nil {
		t.Fatal(err)
	}
}

func TestTOC_SetInvalidLevel(t *testing.T) {
	pdf := GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	// First item must be level 1.
	err := pdf.SetTOC([]TOCItem{
		{Level: 2, Title: "Bad", PageNo: 1},
	})
	if err == nil {
		t.Error("expected error for invalid first level")
	}

	// Level jump > 1.
	err = pdf.SetTOC([]TOCItem{
		{Level: 1, Title: "OK", PageNo: 1},
		{Level: 3, Title: "Bad jump", PageNo: 1},
	})
	if err == nil {
		t.Error("expected error for level jump > 1")
	}
}

func TestTOC_ClearOutlines(t *testing.T) {
	pdf := GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	pdf.AddOutline("Chapter 1")

	// Clear.
	err := pdf.SetTOC(nil)
	if err != nil {
		t.Fatal(err)
	}

	toc := pdf.GetTOC()
	if len(toc) != 0 {
		t.Errorf("expected empty TOC after clear, got %d", len(toc))
	}
}

// ============================================================
// Combined feature test — all new features in one PDF
// ============================================================

func TestAllNewFeatures4_Combined(t *testing.T) {
	pdf := GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})

	// Add OCG layers.
	pdf.AddOCG(OCG{Name: "Main Content", On: true})
	pdf.AddOCG(OCG{Name: "Annotations", Intent: OCGIntentDesign, On: false})

	// Set page layout and mode.
	pdf.SetPageLayout(PageLayoutOneColumn)
	pdf.SetPageMode(PageModeUseOC)

	// Add pages.
	for i := 0; i < 3; i++ {
		pdf.AddPage()
	}

	// Set TOC.
	err := pdf.SetTOC([]TOCItem{
		{Level: 1, Title: "Page 1", PageNo: 1},
		{Level: 1, Title: "Page 2", PageNo: 2},
		{Level: 1, Title: "Page 3", PageNo: 3},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Set metadata.
	pdf.SetInfo(PdfInfo{Title: "Combined Test", Author: "GoPDF2"})
	pdf.SetXMPMetadata(XMPMetadata{
		Title:      "Combined Test",
		Creator:    []string{"GoPDF2"},
		CreateDate: time.Now(),
	})

	// Get stats before scrub.
	stats := pdf.GetDocumentStats()
	if stats.PageCount != 3 {
		t.Errorf("expected 3 pages, got %d", stats.PageCount)
	}
	if !stats.HasOCGs {
		t.Error("should have OCGs")
	}
	if !stats.HasXMPMetadata {
		t.Error("should have XMP metadata")
	}

	err = pdf.WritePdf("test/out/all_features4.pdf")
	if err != nil {
		t.Fatal(err)
	}

	info, _ := os.Stat("test/out/all_features4.pdf")
	if info.Size() == 0 {
		t.Error("output PDF is empty")
	}
}

// ============================================================
// Helper
// ============================================================

func setupTestPDF(t *testing.T) *GoPdf {
	t.Helper()
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	return pdf
}
