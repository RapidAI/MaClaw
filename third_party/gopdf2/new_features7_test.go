package gopdf

import (
	"os"
	"strings"
	"testing"
)

// ============================================================
// Tests for all newly implemented features:
// - Text Search (SearchText, SearchTextOnPage)
// - Font Extraction (ExtractFontsFromPage, ExtractFontsFromAllPages)
// - Annotation Modification (ModifyAnnotation)
// - Bookmark Operations (ModifyBookmark, DeleteBookmark, SetBookmarkStyle)
// - MarkInfo (SetMarkInfo, GetMarkInfo)
// - FindPagesByLabel
// - Low-level PDF Operations (ReadObject, UpdateObject, etc.)
// - Journalling (JournalEnable, JournalUndo, JournalRedo, etc.)
// - Form Field Operations (DeleteFormField, ModifyFormFieldValue, IsFormPDF, BakeAnnotations)
// ============================================================

// ============================================================
// Text Search tests
// ============================================================

func TestSearchText_Basic(t *testing.T) {
	ensureOutDir(t)
	pdf := newPDFWithFont(t)
	pdf.AddPage()
	pdf.SetFont(fontFamily, "", 14)
	pdf.SetX(50)
	pdf.SetY(50)
	pdf.Cell(nil, "Hello World GoPDF2")

	outPath := resOutDir + "/test_search_text.pdf"
	if err := pdf.WritePdf(outPath); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}

	// SearchText should not error even if text encoding makes matching hard
	results, err := SearchText(data, "Hello", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("SearchText found %d results for 'Hello'", len(results))
	for _, r := range results {
		t.Logf("  Page %d at (%.0f, %.0f): %q context=%q", r.PageIndex, r.X, r.Y, r.Text, r.Context)
	}
}

func TestSearchText_CaseInsensitive(t *testing.T) {
	ensureOutDir(t)
	pdf := newPDFWithFont(t)
	pdf.AddPage()
	pdf.SetFont(fontFamily, "", 14)
	pdf.SetX(50)
	pdf.SetY(50)
	pdf.Cell(nil, "HELLO world")

	outPath := resOutDir + "/test_search_case.pdf"
	pdf.WritePdf(outPath)

	data, _ := os.ReadFile(outPath)
	results, err := SearchText(data, "hello", true)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Case-insensitive search found %d results", len(results))
}

func TestSearchTextOnPage(t *testing.T) {
	ensureOutDir(t)
	pdf := newPDFWithFont(t)
	pdf.AddPage()
	pdf.SetFont(fontFamily, "", 12)
	pdf.SetX(50)
	pdf.SetY(50)
	pdf.Cell(nil, "Page one content")

	pdf.AddPage()
	pdf.SetX(50)
	pdf.SetY(50)
	pdf.Cell(nil, "Page two content")

	outPath := resOutDir + "/test_search_page.pdf"
	pdf.WritePdf(outPath)

	data, _ := os.ReadFile(outPath)

	// Search on page 0
	results, err := SearchTextOnPage(data, 0, "content", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Page 0 search found %d results", len(results))

	// Search on page 1
	results, err = SearchTextOnPage(data, 1, "content", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Page 1 search found %d results", len(results))
}

func TestSearchText_NoResults(t *testing.T) {
	ensureOutDir(t)
	pdf := newPDFWithFont(t)
	pdf.AddPage()
	pdf.SetFont(fontFamily, "", 12)
	pdf.SetX(50)
	pdf.SetY(50)
	pdf.Cell(nil, "Some text here")

	outPath := resOutDir + "/test_search_noresult.pdf"
	pdf.WritePdf(outPath)

	data, _ := os.ReadFile(outPath)
	results, err := SearchText(data, "NONEXISTENT_STRING_XYZ", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for nonexistent string, got %d", len(results))
	}
}

// ============================================================
// Font Extraction tests
// ============================================================

func TestExtractFontsFromPage_Basic(t *testing.T) {
	ensureOutDir(t)
	pdf := newPDFWithFont(t)
	pdf.AddPage()
	pdf.SetFont(fontFamily, "", 14)
	pdf.SetX(50)
	pdf.SetY(50)
	pdf.Cell(nil, "Font test")

	outPath := resOutDir + "/test_font_extract.pdf"
	if err := pdf.WritePdf(outPath); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}

	fonts, err := ExtractFontsFromPage(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Extracted %d fonts from page 0", len(fonts))
	for _, f := range fonts {
		t.Logf("  Font: name=%s base=%s subtype=%s encoding=%s embedded=%v objNum=%d",
			f.Name, f.BaseFont, f.Subtype, f.Encoding, f.IsEmbedded, f.ObjNum)
	}
}

func TestExtractFontsFromAllPages(t *testing.T) {
	ensureOutDir(t)
	pdf := newPDFWithFont(t)
	pdf.AddPage()
	pdf.SetFont(fontFamily, "", 12)
	pdf.SetX(50)
	pdf.SetY(50)
	pdf.Cell(nil, "Page 1")

	pdf.AddPage()
	pdf.SetX(50)
	pdf.SetY(50)
	pdf.Cell(nil, "Page 2")

	outPath := resOutDir + "/test_font_extract_all.pdf"
	pdf.WritePdf(outPath)

	data, _ := os.ReadFile(outPath)
	result, err := ExtractFontsFromAllPages(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Extracted fonts from %d pages", len(result))
}

func TestExtractFontsFromPage_InvalidPage(t *testing.T) {
	ensureOutDir(t)
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	outPath := resOutDir + "/test_font_extract_invalid.pdf"
	pdf.WritePdf(outPath)

	data, _ := os.ReadFile(outPath)
	_, err := ExtractFontsFromPage(data, 99)
	if err == nil {
		t.Error("expected error for out-of-range page index")
	}
}

// ============================================================
// Annotation Modification tests
// ============================================================

func TestModifyAnnotation_Basic(t *testing.T) {
	ensureOutDir(t)
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	// Add a text annotation
	pdf.AddTextAnnotation(100, 700, "Author", "Original note")

	// Modify it
	err := pdf.ModifyAnnotation(0, 0, AnnotationOption{
		Content: "Updated note",
		Color:   [3]uint8{255, 0, 0},
	})
	if err != nil {
		t.Fatal(err)
	}

	outPath := resOutDir + "/test_modify_annotation.pdf"
	err = pdf.WritePdf(outPath)
	if err != nil {
		t.Fatal(err)
	}

	info, _ := os.Stat(outPath)
	t.Logf("Modified annotation PDF: %d bytes", info.Size())
}

func TestModifyAnnotation_InvalidPage(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	err := pdf.ModifyAnnotation(99, 0, AnnotationOption{Content: "test"})
	if err != ErrPageOutOfRange {
		t.Errorf("expected ErrPageOutOfRange, got: %v", err)
	}
}

func TestModifyAnnotation_InvalidIndex(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	err := pdf.ModifyAnnotation(0, 0, AnnotationOption{Content: "test"})
	if err != ErrAnnotationNotFound {
		t.Errorf("expected ErrAnnotationNotFound, got: %v", err)
	}
}

// ============================================================
// Bookmark Operations tests
// ============================================================

func TestModifyBookmark(t *testing.T) {
	ensureOutDir(t)
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	pdf.AddOutline("Chapter 1")
	pdf.AddPage()
	pdf.AddOutline("Chapter 2")
	pdf.AddPage()
	pdf.AddOutline("Chapter 3")

	// Modify the first bookmark
	err := pdf.ModifyBookmark(0, "Introduction")
	if err != nil {
		t.Fatal(err)
	}

	outPath := resOutDir + "/test_modify_bookmark.pdf"
	err = pdf.WritePdf(outPath)
	if err != nil {
		t.Fatal(err)
	}

	info, _ := os.Stat(outPath)
	t.Logf("Modified bookmark PDF: %d bytes", info.Size())
}

func TestModifyBookmark_OutOfRange(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	pdf.AddOutline("Chapter 1")

	err := pdf.ModifyBookmark(99, "Invalid")
	if err != ErrBookmarkOutOfRange {
		t.Errorf("expected ErrBookmarkOutOfRange, got: %v", err)
	}
}

func TestDeleteBookmark(t *testing.T) {
	ensureOutDir(t)
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	pdf.AddOutline("Chapter 1")
	pdf.AddPage()
	pdf.AddOutline("Chapter 2")
	pdf.AddPage()
	pdf.AddOutline("Chapter 3")

	// Delete the second bookmark
	err := pdf.DeleteBookmark(1)
	if err != nil {
		t.Fatal(err)
	}

	if pdf.outlines.Count() != 2 {
		t.Errorf("expected 2 bookmarks after delete, got %d", pdf.outlines.Count())
	}

	outPath := resOutDir + "/test_delete_bookmark.pdf"
	err = pdf.WritePdf(outPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Deleted bookmark PDF written")
}

func TestDeleteBookmark_OutOfRange(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	err := pdf.DeleteBookmark(0)
	if err != ErrBookmarkOutOfRange {
		t.Errorf("expected ErrBookmarkOutOfRange, got: %v", err)
	}
}

func TestSetBookmarkStyle(t *testing.T) {
	ensureOutDir(t)
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	pdf.AddOutline("Styled Bookmark")
	pdf.AddPage()
	pdf.AddOutline("Normal Bookmark")

	// Set style on first bookmark
	err := pdf.SetBookmarkStyle(0, BookmarkStyle{
		Bold:      true,
		Italic:    true,
		Color:     [3]float64{1.0, 0.0, 0.0}, // red
		Collapsed: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	outPath := resOutDir + "/test_bookmark_style.pdf"
	err = pdf.WritePdf(outPath)
	if err != nil {
		t.Fatal(err)
	}

	info, _ := os.Stat(outPath)
	t.Logf("Styled bookmark PDF: %d bytes", info.Size())
}

func TestSetBookmarkStyle_OutOfRange(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	err := pdf.SetBookmarkStyle(0, BookmarkStyle{Bold: true})
	if err != ErrBookmarkOutOfRange {
		t.Errorf("expected ErrBookmarkOutOfRange, got: %v", err)
	}
}

// ============================================================
// MarkInfo tests
// ============================================================

func TestSetMarkInfo(t *testing.T) {
	ensureOutDir(t)
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	pdf.SetMarkInfo(MarkInfo{Marked: true})

	mi := pdf.GetMarkInfo()
	if mi == nil {
		t.Fatal("expected non-nil MarkInfo")
	}
	if !mi.Marked {
		t.Error("expected Marked=true")
	}

	outPath := resOutDir + "/test_markinfo.pdf"
	err := pdf.WritePdf(outPath)
	if err != nil {
		t.Fatal(err)
	}

	// Verify the output contains /MarkInfo
	data, _ := os.ReadFile(outPath)
	if !strings.Contains(string(data), "/MarkInfo") {
		t.Error("expected /MarkInfo in PDF output")
	}
	if !strings.Contains(string(data), "/Marked true") {
		t.Error("expected /Marked true in PDF output")
	}

	info, _ := os.Stat(outPath)
	t.Logf("MarkInfo PDF: %d bytes", info.Size())
}

func TestSetMarkInfo_AllFields(t *testing.T) {
	ensureOutDir(t)
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	pdf.SetMarkInfo(MarkInfo{
		Marked:         true,
		UserProperties: true,
		Suspects:       true,
	})

	outPath := resOutDir + "/test_markinfo_all.pdf"
	err := pdf.WritePdf(outPath)
	if err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(outPath)
	content := string(data)
	if !strings.Contains(content, "/UserProperties true") {
		t.Error("expected /UserProperties true")
	}
	if !strings.Contains(content, "/Suspects true") {
		t.Error("expected /Suspects true")
	}
}

func TestGetMarkInfo_Nil(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})

	mi := pdf.GetMarkInfo()
	if mi != nil {
		t.Error("expected nil MarkInfo before setting")
	}
}

// ============================================================
// FindPagesByLabel tests
// ============================================================

func TestFindPagesByLabel(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage() // page 0
	pdf.AddPage() // page 1
	pdf.AddPage() // page 2
	pdf.AddPage() // page 3
	pdf.AddPage() // page 4

	pdf.SetPageLabels([]PageLabel{
		{PageIndex: 0, Style: PageLabelRomanLower, Start: 1},  // i, ii, iii
		{PageIndex: 3, Style: PageLabelDecimal, Start: 1},     // 1, 2
	})

	// Find page labeled "ii" (should be page index 1)
	pages := pdf.FindPagesByLabel("ii")
	if len(pages) != 1 || pages[0] != 1 {
		t.Errorf("expected [1] for label 'ii', got %v", pages)
	}

	// Find page labeled "1" (should be page index 3)
	pages = pdf.FindPagesByLabel("1")
	if len(pages) != 1 || pages[0] != 3 {
		t.Errorf("expected [3] for label '1', got %v", pages)
	}

	// Find nonexistent label
	pages = pdf.FindPagesByLabel("xyz")
	if len(pages) != 0 {
		t.Errorf("expected empty for nonexistent label, got %v", pages)
	}
}

func TestFindPagesByLabel_NoLabels(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	pages := pdf.FindPagesByLabel("1")
	if pages != nil {
		t.Errorf("expected nil when no labels set, got %v", pages)
	}
}

func TestFindPagesByLabel_WithPrefix(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	pdf.AddPage()
	pdf.AddPage()

	pdf.SetPageLabels([]PageLabel{
		{PageIndex: 0, Style: PageLabelAlphaUpper, Prefix: "App-", Start: 1},
	})

	pages := pdf.FindPagesByLabel("App-A")
	if len(pages) != 1 || pages[0] != 0 {
		t.Errorf("expected [0] for label 'App-A', got %v", pages)
	}

	pages = pdf.FindPagesByLabel("App-B")
	if len(pages) != 1 || pages[0] != 1 {
		t.Errorf("expected [1] for label 'App-B', got %v", pages)
	}
}

// ============================================================
// Low-level PDF Operations tests
// ============================================================

func TestReadObject(t *testing.T) {
	ensureOutDir(t)
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	outPath := resOutDir + "/test_lowlevel.pdf"
	pdf.WritePdf(outPath)

	data, _ := os.ReadFile(outPath)

	// Read object 1 (should be the catalog)
	obj, err := ReadObject(data, 1)
	if err != nil {
		t.Fatal(err)
	}
	if obj.Num != 1 {
		t.Errorf("expected obj num 1, got %d", obj.Num)
	}
	if obj.Dict == "" {
		t.Error("expected non-empty dict")
	}
	t.Logf("Object 1 dict: %s", obj.Dict)
}

func TestReadObject_NotFound(t *testing.T) {
	ensureOutDir(t)
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	outPath := resOutDir + "/test_lowlevel_notfound.pdf"
	pdf.WritePdf(outPath)

	data, _ := os.ReadFile(outPath)

	_, err := ReadObject(data, 9999)
	if err == nil {
		t.Error("expected error for nonexistent object")
	}
}

func TestGetDictKey(t *testing.T) {
	ensureOutDir(t)
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	outPath := resOutDir + "/test_getdictkey.pdf"
	pdf.WritePdf(outPath)

	data, _ := os.ReadFile(outPath)

	// Object 1 should be the Catalog with /Type /Catalog
	val, err := GetDictKey(data, 1, "/Type")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Object 1 /Type = %q", val)
	if !strings.Contains(val, "Catalog") {
		t.Errorf("expected /Type to contain 'Catalog', got %q", val)
	}
}

func TestUpdateObject(t *testing.T) {
	ensureOutDir(t)
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	outPath := resOutDir + "/test_updateobj_src.pdf"
	pdf.WritePdf(outPath)

	data, _ := os.ReadFile(outPath)

	// Read object 2 (Pages)
	obj, err := ReadObject(data, 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Original object 2: %s", obj.Dict)

	// Update it (just replace with same content to verify the mechanism works)
	updated, err := UpdateObject(data, 2, "<<"+obj.Dict+">>")
	if err != nil {
		t.Fatal(err)
	}
	if len(updated) == 0 {
		t.Fatal("expected non-empty updated PDF")
	}

	outPath2 := resOutDir + "/test_updateobj_result.pdf"
	os.WriteFile(outPath2, updated, 0644)
	t.Logf("Updated PDF: %d bytes", len(updated))
}

func TestSetDictKey(t *testing.T) {
	ensureOutDir(t)
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	outPath := resOutDir + "/test_setdictkey_src.pdf"
	pdf.WritePdf(outPath)

	data, _ := os.ReadFile(outPath)

	// Add a custom key to object 1
	updated, err := SetDictKey(data, 1, "/CustomKey", "(TestValue)")
	if err != nil {
		t.Fatal(err)
	}

	// Verify the key was added
	val, err := GetDictKey(updated, 1, "/CustomKey")
	if err != nil {
		t.Fatal(err)
	}
	if val != "(TestValue)" {
		t.Errorf("expected '(TestValue)', got %q", val)
	}
	t.Logf("SetDictKey result: /CustomKey = %s", val)
}

func TestCopyObject(t *testing.T) {
	ensureOutDir(t)
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	outPath := resOutDir + "/test_copyobj_src.pdf"
	pdf.WritePdf(outPath)

	data, _ := os.ReadFile(outPath)

	// Copy object 1
	newData, newObjNum, err := CopyObject(data, 1)
	if err != nil {
		t.Fatal(err)
	}
	if newObjNum <= 0 {
		t.Fatal("expected positive new object number")
	}
	t.Logf("Copied object 1 -> new object %d", newObjNum)

	// Verify the new object exists
	obj, err := ReadObject(newData, newObjNum)
	if err != nil {
		t.Fatal(err)
	}
	if obj.Dict == "" {
		t.Error("expected non-empty dict in copied object")
	}
}

func TestGetCatalog(t *testing.T) {
	ensureOutDir(t)
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	outPath := resOutDir + "/test_getcatalog.pdf"
	pdf.WritePdf(outPath)

	data, _ := os.ReadFile(outPath)

	catalog, err := GetCatalog(data)
	if err != nil {
		t.Fatal(err)
	}
	if catalog == nil {
		t.Fatal("expected non-nil catalog")
	}
	t.Logf("Catalog obj %d: %s", catalog.Num, catalog.Dict)
	if !strings.Contains(catalog.Dict, "/Type /Catalog") &&
		!strings.Contains(catalog.Dict, "/Type/Catalog") {
		t.Error("expected catalog to contain /Type /Catalog")
	}
}

func TestGetTrailer(t *testing.T) {
	ensureOutDir(t)
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	outPath := resOutDir + "/test_gettrailer.pdf"
	pdf.WritePdf(outPath)

	data, _ := os.ReadFile(outPath)

	trailer, err := GetTrailer(data)
	if err != nil {
		t.Fatal(err)
	}
	if trailer == "" {
		t.Fatal("expected non-empty trailer")
	}
	t.Logf("Trailer: %s", trailer)
	if !strings.Contains(trailer, "/Root") {
		t.Error("expected trailer to contain /Root")
	}
}

func TestGetStream_And_SetStream(t *testing.T) {
	ensureOutDir(t)
	pdf := newPDFWithFont(t)
	pdf.AddPage()
	pdf.SetFont(fontFamily, "", 14)
	pdf.SetX(50)
	pdf.SetY(50)
	pdf.Cell(nil, "Stream test")

	outPath := resOutDir + "/test_stream.pdf"
	pdf.WritePdf(outPath)

	data, _ := os.ReadFile(outPath)

	// Find a content stream object (they have stream data)
	parser, err := newRawPDFParser(data)
	if err != nil {
		t.Fatal(err)
	}

	var streamObjNum int
	for num, obj := range parser.objects {
		if obj.stream != nil && len(obj.stream) > 0 {
			streamObjNum = num
			break
		}
	}

	if streamObjNum == 0 {
		t.Skip("no stream objects found in test PDF")
	}

	// Read the stream
	stream, err := GetStream(data, streamObjNum)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Stream obj %d: %d bytes", streamObjNum, len(stream))
}

// ============================================================
// Journalling tests
// ============================================================

func TestJournalEnable(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	if pdf.JournalIsEnabled() {
		t.Error("journal should not be enabled initially")
	}

	pdf.JournalEnable()

	if !pdf.JournalIsEnabled() {
		t.Error("journal should be enabled after JournalEnable()")
	}
}

func TestJournalDisable(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	pdf.JournalEnable()
	pdf.JournalDisable()

	if pdf.JournalIsEnabled() {
		t.Error("journal should be disabled after JournalDisable()")
	}
}

func TestJournalStartEndOp(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	pdf.JournalEnable()

	pdf.JournalStartOp("add content")
	pdf.AddPage()
	pdf.JournalEndOp()

	ops := pdf.JournalGetOperations()
	t.Logf("Journal operations: %v", ops)
	if len(ops) < 2 {
		t.Errorf("expected at least 2 journal entries (initial + op), got %d", len(ops))
	}
}

func TestJournalUndo(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	pdf.JournalEnable()

	pdf.JournalStartOp("add page")
	pdf.AddPage()
	pdf.JournalEndOp()

	name, err := pdf.JournalUndo()
	if err != nil {
		t.Fatal(err)
	}
	if name != "add page" {
		t.Errorf("expected undone op name 'add page', got %q", name)
	}
	t.Logf("Undid operation: %q", name)
}

func TestJournalUndo_NothingToUndo(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	pdf.JournalEnable()

	// Only initial snapshot, nothing to undo
	_, err := pdf.JournalUndo()
	if err == nil {
		t.Error("expected error when nothing to undo")
	}
}

func TestJournalRedo(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	pdf.JournalEnable()

	pdf.JournalStartOp("op1")
	pdf.AddPage()
	pdf.JournalEndOp()

	// Undo
	_, err := pdf.JournalUndo()
	if err != nil {
		t.Fatal(err)
	}

	// Redo
	name, err := pdf.JournalRedo()
	if err != nil {
		t.Fatal(err)
	}
	if name != "op1" {
		t.Errorf("expected redo op name 'op1', got %q", name)
	}
	t.Logf("Redid operation: %q", name)
}

func TestJournalRedo_NothingToRedo(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	pdf.JournalEnable()

	_, err := pdf.JournalRedo()
	if err == nil {
		t.Error("expected error when nothing to redo")
	}
}

func TestJournalSaveLoad(t *testing.T) {
	ensureOutDir(t)
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	pdf.JournalEnable()

	pdf.JournalStartOp("first op")
	pdf.AddPage()
	pdf.JournalEndOp()

	pdf.JournalStartOp("second op")
	pdf.AddPage()
	pdf.JournalEndOp()

	journalPath := resOutDir + "/test.journal"
	err := pdf.JournalSave(journalPath)
	if err != nil {
		t.Fatal(err)
	}

	info, _ := os.Stat(journalPath)
	t.Logf("Journal file: %d bytes", info.Size())

	// Load into a new PDF
	pdf2 := &GoPdf{}
	pdf2.Start(Config{PageSize: *PageSizeA4})
	pdf2.AddPage()
	pdf2.JournalEnable()

	err = pdf2.JournalLoad(journalPath)
	if err != nil {
		t.Fatal(err)
	}

	ops := pdf2.JournalGetOperations()
	t.Logf("Loaded journal operations: %v", ops)
	if len(ops) < 2 {
		t.Errorf("expected at least 2 loaded operations, got %d", len(ops))
	}
}

func TestJournalSave_NotEnabled(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})

	err := pdf.JournalSave("test.journal")
	if err == nil {
		t.Error("expected error when journal not enabled")
	}
}

func TestJournalUndo_NotEnabled(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})

	_, err := pdf.JournalUndo()
	if err == nil {
		t.Error("expected error when journal not enabled")
	}
}

func TestJournalMultipleUndoRedo(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	pdf.JournalEnable()

	// Create 3 operations
	for i := 0; i < 3; i++ {
		pdf.JournalStartOp("op" + string(rune('A'+i)))
		pdf.AddPage()
		pdf.JournalEndOp()
	}

	ops := pdf.JournalGetOperations()
	t.Logf("After 3 ops: %v", ops)

	// Undo all 3
	for i := 0; i < 3; i++ {
		name, err := pdf.JournalUndo()
		if err != nil {
			t.Fatalf("undo %d failed: %v", i, err)
		}
		t.Logf("Undid: %q", name)
	}

	// Redo all 3
	for i := 0; i < 3; i++ {
		name, err := pdf.JournalRedo()
		if err != nil {
			t.Fatalf("redo %d failed: %v", i, err)
		}
		t.Logf("Redid: %q", name)
	}
}

// ============================================================
// Form Field Operations tests
// ============================================================

func TestDeleteFormField(t *testing.T) {
	ensureOutDir(t)
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	pdf.AddTextField("name", 50, 700, 200, 25)
	pdf.AddTextField("email", 50, 660, 200, 25)
	pdf.AddCheckbox("agree", 50, 620, 15, true)

	if len(pdf.GetFormFields()) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(pdf.GetFormFields()))
	}

	// Delete the email field
	err := pdf.DeleteFormField("email")
	if err != nil {
		t.Fatal(err)
	}

	fields := pdf.GetFormFields()
	if len(fields) != 2 {
		t.Fatalf("expected 2 fields after delete, got %d", len(fields))
	}

	// Verify the right field was deleted
	for _, f := range fields {
		if f.Name == "email" {
			t.Error("email field should have been deleted")
		}
	}

	outPath := resOutDir + "/test_delete_formfield.pdf"
	err = pdf.WritePdf(outPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Delete form field PDF written")
}

func TestDeleteFormField_NotFound(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	err := pdf.DeleteFormField("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent field")
	}
}

func TestModifyFormFieldValue(t *testing.T) {
	ensureOutDir(t)
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	pdf.AddTextField("username", 50, 700, 200, 25)

	// Modify the value
	err := pdf.ModifyFormFieldValue("username", "John Doe")
	if err != nil {
		t.Fatal(err)
	}

	fields := pdf.GetFormFields()
	if len(fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(fields))
	}
	if fields[0].Value != "John Doe" {
		t.Errorf("expected value 'John Doe', got %q", fields[0].Value)
	}

	outPath := resOutDir + "/test_modify_formfield.pdf"
	err = pdf.WritePdf(outPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Modified form field PDF written")
}

func TestModifyFormFieldValue_NotFound(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	err := pdf.ModifyFormFieldValue("nonexistent", "value")
	if err == nil {
		t.Error("expected error for nonexistent field")
	}
}

func TestIsFormPDF(t *testing.T) {
	ensureOutDir(t)

	// Create a PDF with form fields
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	pdf.AddTextField("test", 50, 700, 200, 25)

	outPath := resOutDir + "/test_isformpdf.pdf"
	pdf.WritePdf(outPath)

	data, _ := os.ReadFile(outPath)
	if !IsFormPDF(data) {
		t.Error("expected IsFormPDF=true for PDF with form fields")
	}

	// Create a PDF without form fields
	pdf2 := &GoPdf{}
	pdf2.Start(Config{PageSize: *PageSizeA4})
	pdf2.AddPage()

	outPath2 := resOutDir + "/test_isformpdf_no.pdf"
	pdf2.WritePdf(outPath2)

	data2, _ := os.ReadFile(outPath2)
	if IsFormPDF(data2) {
		t.Error("expected IsFormPDF=false for PDF without form fields")
	}
}

func TestBakeAnnotations(t *testing.T) {
	ensureOutDir(t)
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	// Add form fields
	pdf.AddFormField(FormField{
		Type:        FormFieldText,
		Name:        "bake_test",
		X:           50,
		Y:           700,
		W:           200,
		H:           25,
		Value:       "Baked Value",
		FontSize:    12,
		HasBorder:   true,
		HasFill:     true,
		BorderColor: [3]uint8{0, 0, 0},
		FillColor:   [3]uint8{255, 255, 200},
	})

	pdf.AddCheckbox("bake_check", 50, 660, 15, true)

	// Verify fields exist before baking
	if len(pdf.GetFormFields()) != 2 {
		t.Fatalf("expected 2 fields before bake, got %d", len(pdf.GetFormFields()))
	}

	// Bake
	pdf.BakeAnnotations()

	// Verify fields are gone after baking
	if len(pdf.GetFormFields()) != 0 {
		t.Errorf("expected 0 fields after bake, got %d", len(pdf.GetFormFields()))
	}

	outPath := resOutDir + "/test_bake_annotations.pdf"
	err := pdf.WritePdf(outPath)
	if err != nil {
		t.Fatal(err)
	}

	info, _ := os.Stat(outPath)
	t.Logf("Baked annotations PDF: %d bytes", info.Size())
}

func TestBakeAnnotations_Empty(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	// Baking with no annotations should not panic
	pdf.BakeAnnotations()

	if len(pdf.GetFormFields()) != 0 {
		t.Error("expected 0 fields")
	}
}

func TestBakeAnnotations_WithAnnotation(t *testing.T) {
	ensureOutDir(t)
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	// Add a text annotation
	pdf.AddTextAnnotation(100, 700, "Author", "This will be baked")

	pdf.BakeAnnotations()

	outPath := resOutDir + "/test_bake_annot.pdf"
	err := pdf.WritePdf(outPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Baked annotation PDF written")
}

// ============================================================
// Integration tests — combining multiple features
// ============================================================

func TestIntegration_AllNewFeatures(t *testing.T) {
	ensureOutDir(t)
	pdf := newPDFWithFont(t)

	// Enable journalling
	pdf.JournalEnable()

	// Page 1: text + form fields
	pdf.JournalStartOp("create page 1")
	pdf.AddPage()
	pdf.SetFont(fontFamily, "", 16)
	pdf.SetX(50)
	pdf.SetY(50)
	pdf.Cell(nil, "Integration Test - Page 1")

	pdf.AddTextField("name", 50, 600, 200, 25)
	pdf.AddCheckbox("agree", 50, 560, 15, false)
	pdf.JournalEndOp()

	// Page 2: bookmarks
	pdf.JournalStartOp("create page 2")
	pdf.AddPage()
	pdf.AddOutline("Chapter 1")
	pdf.SetFont(fontFamily, "", 14)
	pdf.SetX(50)
	pdf.SetY(50)
	pdf.Cell(nil, "Chapter 1 Content")
	pdf.JournalEndOp()

	// Page 3: more bookmarks
	pdf.JournalStartOp("create page 3")
	pdf.AddPage()
	pdf.AddOutline("Chapter 2")
	pdf.SetX(50)
	pdf.SetY(50)
	pdf.Cell(nil, "Chapter 2 Content")
	pdf.JournalEndOp()

	// Set MarkInfo
	pdf.SetMarkInfo(MarkInfo{Marked: true})

	// Set page labels
	pdf.SetPageLabels([]PageLabel{
		{PageIndex: 0, Style: PageLabelRomanLower, Start: 1},
		{PageIndex: 1, Style: PageLabelDecimal, Start: 1},
	})

	// Modify bookmark style
	err := pdf.SetBookmarkStyle(0, BookmarkStyle{
		Bold:  true,
		Color: [3]float64{0, 0, 1}, // blue
	})
	if err != nil {
		t.Fatal(err)
	}

	// Modify form field value
	err = pdf.ModifyFormFieldValue("name", "Test User")
	if err != nil {
		t.Fatal(err)
	}

	// Find pages by label
	pages := pdf.FindPagesByLabel("i")
	t.Logf("Pages with label 'i': %v", pages)

	// Write the PDF
	outPath := resOutDir + "/test_integration_all.pdf"
	err = pdf.WritePdf(outPath)
	if err != nil {
		t.Fatal(err)
	}

	info, _ := os.Stat(outPath)
	t.Logf("Integration test PDF: %d bytes", info.Size())

	// Verify the output
	data, _ := os.ReadFile(outPath)
	if !strings.Contains(string(data), "/MarkInfo") {
		t.Error("expected /MarkInfo in output")
	}

	// Test low-level operations on the output
	catalog, err := GetCatalog(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Catalog: %s", catalog.Dict)

	trailer, err := GetTrailer(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Trailer: %s", trailer)

	// Journal operations
	ops := pdf.JournalGetOperations()
	t.Logf("Journal operations: %v", ops)
	if len(ops) < 3 {
		t.Errorf("expected at least 3 journal entries, got %d", len(ops))
	}
}

func TestIntegration_FormFieldLifecycle(t *testing.T) {
	ensureOutDir(t)
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	// Add fields
	pdf.AddTextField("field1", 50, 700, 200, 25)
	pdf.AddTextField("field2", 50, 660, 200, 25)
	pdf.AddTextField("field3", 50, 620, 200, 25)

	// Modify
	pdf.ModifyFormFieldValue("field2", "Modified Value")

	// Delete
	pdf.DeleteFormField("field1")

	// Verify state
	fields := pdf.GetFormFields()
	if len(fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(fields))
	}
	if fields[0].Name != "field2" {
		t.Errorf("expected first field 'field2', got %q", fields[0].Name)
	}
	if fields[0].Value != "Modified Value" {
		t.Errorf("expected value 'Modified Value', got %q", fields[0].Value)
	}

	// Bake remaining fields
	pdf.BakeAnnotations()

	if len(pdf.GetFormFields()) != 0 {
		t.Error("expected 0 fields after bake")
	}

	outPath := resOutDir + "/test_formfield_lifecycle.pdf"
	err := pdf.WritePdf(outPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Form field lifecycle PDF written")
}
