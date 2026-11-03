package gopdf

import (
	"os"
	"sort"
	"testing"
)

// ============================================================
// Tests for Round 2 features: Paper Sizes, Page Rotation,
// Page Selection/Reordering, Embedded Files, Geometry
// ============================================================

// ============================================================
// Paper Sizes Tests
// ============================================================

func TestPaperSize(t *testing.T) {
	tests := []struct {
		name   string
		expectW, expectH float64
	}{
		{"a4", 595, 842},
		{"A4", 595, 842},
		{"a4-l", 842, 595},
		{"letter", 612, 792},
		{"legal", 612, 1008},
		{"tabloid", 792, 1224},
		{"ledger", 1224, 792},
		{"a0", 2384, 3371},
		{"a10", 74, 105},
		{"b0", 2835, 4008},
		{"b5", 516, 729},
	}

	for _, tt := range tests {
		r := PaperSize(tt.name)
		if r == nil {
			t.Fatalf("PaperSize(%q) returned nil", tt.name)
		}
		if r.W != tt.expectW || r.H != tt.expectH {
			t.Errorf("PaperSize(%q) = {W:%.0f, H:%.0f}, want {W:%.0f, H:%.0f}",
				tt.name, r.W, r.H, tt.expectW, tt.expectH)
		}
	}
}

func TestPaperSizeUnknown(t *testing.T) {
	r := PaperSize("unknown")
	if r != nil {
		t.Fatalf("PaperSize(\"unknown\") should return nil, got %+v", r)
	}
}

func TestPaperSizeReturnsCopy(t *testing.T) {
	r1 := PaperSize("a4")
	r2 := PaperSize("a4")
	r1.W = 999
	if r2.W == 999 {
		t.Fatal("PaperSize should return a copy, not a reference to the global")
	}
}

func TestPaperSizeNames(t *testing.T) {
	names := PaperSizeNames()
	if len(names) == 0 {
		t.Fatal("PaperSizeNames returned empty list")
	}
	// Check that a4 is in the list.
	sort.Strings(names)
	found := false
	for _, n := range names {
		if n == "a4" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("PaperSizeNames does not contain 'a4'")
	}
}

// ============================================================
// Page Rotation Tests
// ============================================================

func TestSetPageRotation(t *testing.T) {
	ensureOutDir(t)
	pdf := newPDFWithFont(t)
	pdf.AddPage()
	pdf.SetXY(50, 50)
	pdf.Cell(nil, "Rotated page")

	err := pdf.SetPageRotation(1, 90)
	if err != nil {
		t.Fatalf("SetPageRotation: %v", err)
	}

	angle, err := pdf.GetPageRotation(1)
	if err != nil {
		t.Fatalf("GetPageRotation: %v", err)
	}
	if angle != 90 {
		t.Fatalf("expected rotation 90, got %d", angle)
	}

	err = pdf.WritePdf(resOutDir + "/page_rotation.pdf")
	if err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
}

func TestSetPageRotation270(t *testing.T) {
	pdf := newPDFWithFont(t)
	pdf.AddPage()

	err := pdf.SetPageRotation(1, 270)
	if err != nil {
		t.Fatalf("SetPageRotation: %v", err)
	}

	angle, _ := pdf.GetPageRotation(1)
	if angle != 270 {
		t.Fatalf("expected 270, got %d", angle)
	}
}

func TestSetPageRotationNegative(t *testing.T) {
	pdf := newPDFWithFont(t)
	pdf.AddPage()

	// -90 should normalize to 270
	err := pdf.SetPageRotation(1, -90)
	if err != nil {
		t.Fatalf("SetPageRotation: %v", err)
	}

	angle, _ := pdf.GetPageRotation(1)
	if angle != 270 {
		t.Fatalf("expected 270 for -90, got %d", angle)
	}
}

func TestSetPageRotationInvalid(t *testing.T) {
	pdf := newPDFWithFont(t)
	pdf.AddPage()

	err := pdf.SetPageRotation(1, 45)
	if err != ErrInvalidRotation {
		t.Fatalf("expected ErrInvalidRotation, got: %v", err)
	}
}

func TestSetPageRotationOutOfRange(t *testing.T) {
	pdf := newPDFWithFont(t)
	pdf.AddPage()

	err := pdf.SetPageRotation(0, 90)
	if err != ErrPageOutOfRange {
		t.Fatalf("expected ErrPageOutOfRange, got: %v", err)
	}

	err = pdf.SetPageRotation(2, 90)
	if err != ErrPageOutOfRange {
		t.Fatalf("expected ErrPageOutOfRange, got: %v", err)
	}
}

func TestGetPageRotationDefault(t *testing.T) {
	pdf := newPDFWithFont(t)
	pdf.AddPage()

	angle, err := pdf.GetPageRotation(1)
	if err != nil {
		t.Fatalf("GetPageRotation: %v", err)
	}
	if angle != 0 {
		t.Fatalf("expected default rotation 0, got %d", angle)
	}
}

// ============================================================
// Page Selection / Reordering Tests
// ============================================================

func TestSelectPages(t *testing.T) {
	ensureOutDir(t)
	pdf := newPDFWithFont(t)

	// Create 3 pages.
	for i := 1; i <= 3; i++ {
		pdf.AddPage()
		pdf.SetXY(50, 50)
		pdf.Cell(nil, "Page "+string(rune('0'+i)))
	}

	// Reverse order.
	newPdf, err := pdf.SelectPages([]int{3, 2, 1})
	if err != nil {
		t.Fatalf("SelectPages: %v", err)
	}

	if newPdf.GetNumberOfPages() != 3 {
		t.Fatalf("expected 3 pages, got %d", newPdf.GetNumberOfPages())
	}

	err = newPdf.WritePdf(resOutDir + "/select_pages_reversed.pdf")
	if err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
}

func TestSelectPagesDuplicate(t *testing.T) {
	pdf := newPDFWithFont(t)
	pdf.AddPage()
	pdf.SetXY(50, 50)
	pdf.Cell(nil, "Only page")

	// Duplicate page 1 three times.
	newPdf, err := pdf.SelectPages([]int{1, 1, 1})
	if err != nil {
		t.Fatalf("SelectPages: %v", err)
	}

	if newPdf.GetNumberOfPages() != 3 {
		t.Fatalf("expected 3 pages, got %d", newPdf.GetNumberOfPages())
	}
}

func TestSelectPagesOutOfRange(t *testing.T) {
	pdf := newPDFWithFont(t)
	pdf.AddPage()

	_, err := pdf.SelectPages([]int{2})
	if err != ErrPageOutOfRange {
		t.Fatalf("expected ErrPageOutOfRange, got: %v", err)
	}
}

func TestSelectPagesEmpty(t *testing.T) {
	pdf := newPDFWithFont(t)
	pdf.AddPage()

	_, err := pdf.SelectPages([]int{})
	if err != ErrNoPages {
		t.Fatalf("expected ErrNoPages, got: %v", err)
	}
}

func TestSelectPagesFromFile(t *testing.T) {
	ensureOutDir(t)

	// First create a test PDF.
	pdf := newPDFWithFont(t)
	for i := 1; i <= 3; i++ {
		pdf.AddPage()
		pdf.SetXY(50, 50)
		pdf.Cell(nil, "Page "+string(rune('0'+i)))
	}
	srcPath := resOutDir + "/select_pages_src.pdf"
	if err := pdf.WritePdf(srcPath); err != nil {
		t.Fatalf("WritePdf: %v", err)
	}

	// Select pages 2, 1 from file.
	newPdf, err := SelectPagesFromFile(srcPath, []int{2, 1}, nil)
	if err != nil {
		t.Fatalf("SelectPagesFromFile: %v", err)
	}

	if newPdf.GetNumberOfPages() != 2 {
		t.Fatalf("expected 2 pages, got %d", newPdf.GetNumberOfPages())
	}

	err = newPdf.WritePdf(resOutDir + "/select_pages_from_file.pdf")
	if err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
}

func TestSelectPagesFromBytes(t *testing.T) {
	// Create a test PDF in memory.
	pdf := newPDFWithFont(t)
	for i := 1; i <= 2; i++ {
		pdf.AddPage()
		pdf.SetXY(50, 50)
		pdf.Cell(nil, "Page")
	}
	data, err := pdf.GetBytesPdfReturnErr()
	if err != nil {
		t.Fatalf("GetBytesPdfReturnErr: %v", err)
	}

	newPdf, err := SelectPagesFromBytes(data, []int{2, 1}, nil)
	if err != nil {
		t.Fatalf("SelectPagesFromBytes: %v", err)
	}

	if newPdf.GetNumberOfPages() != 2 {
		t.Fatalf("expected 2 pages, got %d", newPdf.GetNumberOfPages())
	}
}

// ============================================================
// Embedded File Tests
// ============================================================

func TestAddEmbeddedFile(t *testing.T) {
	ensureOutDir(t)
	pdf := newPDFWithFont(t)
	pdf.AddPage()
	pdf.SetXY(50, 50)
	pdf.Cell(nil, "PDF with embedded file")

	err := pdf.AddEmbeddedFile(EmbeddedFile{
		Name:     "test.txt",
		Content:  []byte("Hello, this is an embedded text file."),
		MimeType: "text/plain",
	})
	if err != nil {
		t.Fatalf("AddEmbeddedFile: %v", err)
	}

	err = pdf.WritePdf(resOutDir + "/embedded_file.pdf")
	if err != nil {
		t.Fatalf("WritePdf: %v", err)
	}

	// Verify the file was created and is non-empty.
	info, err := os.Stat(resOutDir + "/embedded_file.pdf")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("output PDF is empty")
	}
}

func TestAddMultipleEmbeddedFiles(t *testing.T) {
	ensureOutDir(t)
	pdf := newPDFWithFont(t)
	pdf.AddPage()
	pdf.SetXY(50, 50)
	pdf.Cell(nil, "PDF with multiple embedded files")

	err := pdf.AddEmbeddedFile(EmbeddedFile{
		Name:        "readme.txt",
		Content:     []byte("This is a readme file."),
		MimeType:    "text/plain",
		Description: "A readme file",
	})
	if err != nil {
		t.Fatalf("AddEmbeddedFile 1: %v", err)
	}

	err = pdf.AddEmbeddedFile(EmbeddedFile{
		Name:     "data.csv",
		Content:  []byte("name,value\nfoo,1\nbar,2\n"),
		MimeType: "text/csv",
	})
	if err != nil {
		t.Fatalf("AddEmbeddedFile 2: %v", err)
	}

	err = pdf.WritePdf(resOutDir + "/embedded_files_multi.pdf")
	if err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
}

func TestAddEmbeddedFileEmptyName(t *testing.T) {
	pdf := newPDFWithFont(t)
	pdf.AddPage()

	err := pdf.AddEmbeddedFile(EmbeddedFile{
		Name:    "",
		Content: []byte("data"),
	})
	if err != ErrEmptyString {
		t.Fatalf("expected ErrEmptyString, got: %v", err)
	}
}

func TestAddEmbeddedFileEmptyContent(t *testing.T) {
	pdf := newPDFWithFont(t)
	pdf.AddPage()

	err := pdf.AddEmbeddedFile(EmbeddedFile{
		Name:    "empty.txt",
		Content: nil,
	})
	if err != ErrEmptyString {
		t.Fatalf("expected ErrEmptyString, got: %v", err)
	}
}

// ============================================================
// Geometry Tests
// ============================================================

func TestRectFromContains(t *testing.T) {
	r := RectFrom{X: 10, Y: 10, W: 100, H: 50}

	if !r.Contains(50, 30) {
		t.Fatal("point (50,30) should be inside rect")
	}
	if r.Contains(5, 30) {
		t.Fatal("point (5,30) should be outside rect")
	}
	if r.Contains(50, 70) {
		t.Fatal("point (50,70) should be outside rect")
	}
}

func TestRectFromContainsRect(t *testing.T) {
	outer := RectFrom{X: 0, Y: 0, W: 100, H: 100}
	inner := RectFrom{X: 10, Y: 10, W: 50, H: 50}
	outside := RectFrom{X: 90, Y: 90, W: 50, H: 50}

	if !outer.ContainsRect(inner) {
		t.Fatal("inner should be contained in outer")
	}
	if outer.ContainsRect(outside) {
		t.Fatal("outside should not be contained in outer")
	}
}

func TestRectFromIntersects(t *testing.T) {
	r1 := RectFrom{X: 0, Y: 0, W: 100, H: 100}
	r2 := RectFrom{X: 50, Y: 50, W: 100, H: 100}
	r3 := RectFrom{X: 200, Y: 200, W: 50, H: 50}

	if !r1.Intersects(r2) {
		t.Fatal("r1 and r2 should intersect")
	}
	if r1.Intersects(r3) {
		t.Fatal("r1 and r3 should not intersect")
	}
}

func TestRectFromIntersection(t *testing.T) {
	r1 := RectFrom{X: 0, Y: 0, W: 100, H: 100}
	r2 := RectFrom{X: 50, Y: 50, W: 100, H: 100}

	inter := r1.Intersection(r2)
	if inter.X != 50 || inter.Y != 50 || inter.W != 50 || inter.H != 50 {
		t.Fatalf("intersection = %+v, want {50 50 50 50}", inter)
	}

	// Non-overlapping.
	r3 := RectFrom{X: 200, Y: 200, W: 50, H: 50}
	inter2 := r1.Intersection(r3)
	if !inter2.IsEmpty() {
		t.Fatalf("non-overlapping intersection should be empty, got %+v", inter2)
	}
}

func TestRectFromUnion(t *testing.T) {
	r1 := RectFrom{X: 10, Y: 10, W: 50, H: 50}
	r2 := RectFrom{X: 30, Y: 30, W: 80, H: 80}

	u := r1.Union(r2)
	if u.X != 10 || u.Y != 10 || u.W != 100 || u.H != 100 {
		t.Fatalf("union = %+v, want {10 10 100 100}", u)
	}
}

func TestRectFromArea(t *testing.T) {
	r := RectFrom{X: 0, Y: 0, W: 10, H: 5}
	if r.Area() != 50 {
		t.Fatalf("area = %f, want 50", r.Area())
	}

	empty := RectFrom{X: 0, Y: 0, W: 0, H: 5}
	if empty.Area() != 0 {
		t.Fatalf("empty area = %f, want 0", empty.Area())
	}
}

func TestRectFromCenter(t *testing.T) {
	r := RectFrom{X: 10, Y: 20, W: 100, H: 50}
	c := r.Center()
	if c.X != 60 || c.Y != 45 {
		t.Fatalf("center = %+v, want {60 45}", c)
	}
}

func TestRectFromNormalize(t *testing.T) {
	r := RectFrom{X: 100, Y: 100, W: -50, H: -30}
	n := r.Normalize()
	if n.X != 50 || n.Y != 70 || n.W != 50 || n.H != 30 {
		t.Fatalf("normalize = %+v, want {50 70 50 30}", n)
	}
}

func TestMatrixIdentity(t *testing.T) {
	m := IdentityMatrix()
	if !m.IsIdentity() {
		t.Fatal("IdentityMatrix should be identity")
	}
}

func TestMatrixTranslate(t *testing.T) {
	m := TranslateMatrix(10, 20)
	x, y := m.TransformPoint(0, 0)
	if x != 10 || y != 20 {
		t.Fatalf("translate (0,0) = (%f,%f), want (10,20)", x, y)
	}
}

func TestMatrixScale(t *testing.T) {
	m := ScaleMatrix(2, 3)
	x, y := m.TransformPoint(5, 10)
	if x != 10 || y != 30 {
		t.Fatalf("scale (5,10) = (%f,%f), want (10,30)", x, y)
	}
}

func TestMatrixMultiply(t *testing.T) {
	// Translate then scale.
	translate := TranslateMatrix(10, 0)
	scale := ScaleMatrix(2, 2)
	combined := scale.Multiply(translate)

	x, y := combined.TransformPoint(5, 0)
	// scale(translate(5,0)) = scale(15,0) = (30,0)
	if x != 30 || y != 0 {
		t.Fatalf("combined (5,0) = (%f,%f), want (30,0)", x, y)
	}
}

func TestDistance(t *testing.T) {
	p1 := Point{X: 0, Y: 0}
	p2 := Point{X: 3, Y: 4}
	d := Distance(p1, p2)
	if d != 5 {
		t.Fatalf("distance = %f, want 5", d)
	}
}

// ============================================================
// Paper Size with Document Creation Test
// ============================================================

func TestPaperSizeWithDocument(t *testing.T) {
	ensureOutDir(t)

	size := PaperSize("a5")
	if size == nil {
		t.Fatal("PaperSize(a5) returned nil")
	}

	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *size})
	pdf.AddPage()

	if err := pdf.AddTTFFont(fontFamily, resFontPath); err != nil {
		t.Skipf("font not available: %v", err)
	}
	if err := pdf.SetFont(fontFamily, "", 14); err != nil {
		t.Fatalf("SetFont: %v", err)
	}

	pdf.SetXY(50, 50)
	pdf.Cell(nil, "A5 page")

	err := pdf.WritePdf(resOutDir + "/paper_size_a5.pdf")
	if err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
}
