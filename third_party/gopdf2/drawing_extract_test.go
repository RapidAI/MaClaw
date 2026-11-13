package gopdf

import (
	"math"
	"testing"
)

// TestExtractPageDrawings generates known vector graphics and verifies the
// extractor returns them with correct geometry and colors.
func TestExtractPageDrawings(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	// stroke line from (60, 100) to (200, 100)
	pdf.SetLineWidth(1)
	pdf.SetStrokeColor(255, 0, 0)
	pdf.Line(60, 100, 200, 100)

	// filled rect at (60, 150) 140x40
	pdf.SetFillColor(0, 0, 255)
	pdf.RectFromUpperLeftWithStyle(60, 150, 140, 40, "F")

	// stroked rect at (60, 220) 80x30
	pdf.SetStrokeColor(0, 128, 0)
	pdf.RectFromUpperLeftWithStyle(60, 220, 80, 30, "S")

	path := t.TempDir() + "/draw.pdf"
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	data := readFileBytesForTest(t, path)

	drawings, err := ExtractPageDrawings(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(drawings) < 3 {
		for i, d := range drawings {
			t.Logf("drawing %d: op=%s rect=%v pts=%d", i, d.Op, d.Rect, len(d.Points))
		}
		t.Fatalf("got %d drawings, want >= 3", len(drawings))
	}

	var line, fill, stroke *Drawing
	for i := range drawings {
		d := &drawings[i]
		if d.IsRect && d.Op == "f" && fill == nil {
			fill = d
		} else if d.IsRect && d.Op == "s" && stroke == nil {
			stroke = d
		} else if !d.IsRect && d.Op == "s" && line == nil {
			line = d
		}
	}
	if line == nil {
		t.Fatalf("stroked line not found")
	}
	if len(line.Points) < 2 {
		t.Fatalf("line points = %d", len(line.Points))
	}
	if math.Abs(line.Points[0][0]-60) > 1 || math.Abs(line.Points[0][1]-100) > 1 {
		t.Errorf("line start = %v, want ~(60,100)", line.Points[0])
	}
	if line.Color[0] != 1 || line.Color[1] != 0 {
		t.Errorf("line color = %v, want red", line.Color)
	}

	if fill == nil {
		t.Fatalf("filled rect not found")
	}
	if math.Abs(fill.Rect[0]-60) > 1 || math.Abs(fill.Rect[1]-150) > 1 ||
		math.Abs(fill.Rect[2]-140) > 1 || math.Abs(fill.Rect[3]-40) > 1 {
		t.Errorf("fill rect = %v, want [60 150 140 40]", fill.Rect)
	}
	if fill.Fill[2] != 1 {
		t.Errorf("fill color = %v, want blue", fill.Fill)
	}

	if stroke == nil {
		t.Fatalf("stroked rect not found")
	}
	if math.Abs(stroke.Rect[0]-60) > 1 || math.Abs(stroke.Rect[1]-220) > 1 {
		t.Errorf("stroke rect = %v, want [60 220 80 30]", stroke.Rect)
	}
}

// TestDrawingsRotatedPage verifies drawings map to visual space on a
// /Rotate 90 page.
func TestDrawingsRotatedPage(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	pdf.SetStrokeColor(255, 0, 0)
	pdf.Line(60, 100, 200, 100)
	if err := pdf.SetPageRotation(1, 90); err != nil {
		t.Fatal(err)
	}
	path := t.TempDir() + "/rotdraw.pdf"
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	data := readFileBytesForTest(t, path)
	drawings, err := ExtractPageDrawings(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(drawings) == 0 {
		t.Fatalf("no drawings on rotated page")
	}
	// unrotated start (60,100) -> rotate 90: (h-y, x) = (842-100, 60) = (742, 60)
	first := drawings[0].Points[0]
	if math.Abs(first[0]-742) > 2 || math.Abs(first[1]-60) > 2 {
		t.Errorf("rotated line start = %v, want ~(742, 60)", first)
	}
}

// TestDrawingsOnShowcaseTable verifies the extractor finds the table grid
// borders of the HTML showcase page.
func TestDrawingsOnShowcaseTable(t *testing.T) {
	data, err := readFileBytes("test/out/showcase/showcase.pdf")
	if err != nil {
		t.Skip("showcase not built")
	}
	drawings, err := ExtractPageDrawings(data, 2)
	if err != nil {
		t.Fatal(err)
	}
	rects := 0
	for _, d := range drawings {
		if d.IsRect {
			rects++
		}
	}
	if rects < 3 {
		t.Errorf("expected table cell rects on showcase page 3, got %d (total %d)", rects, len(drawings))
	}
}

func readFileBytesForTest(t *testing.T, path string) []byte {
	t.Helper()
	data, err := readFileBytes(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestImageExtractionRotatedPage verifies image placements map to visual
// space on a /Rotate 90 page.
func TestImageExtractionRotatedPage(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()
	holder, err := ImageHolderByPath("test/res/gopher02.png")
	if err != nil {
		t.Skipf("image fixture unavailable: %v", err)
	}
	if err := pdf.ImageByHolder(holder, 100, 200, &Rect{W: 80, H: 80}); err != nil {
		t.Fatal(err)
	}
	if err := pdf.SetPageRotation(1, 90); err != nil {
		t.Fatal(err)
	}
	path := t.TempDir() + "/imgrot.pdf"
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
	data := readFileBytesForTest(t, path)
	imgs, err := ExtractImagesFromPage(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(imgs) != 1 {
		t.Fatalf("got %d images, want 1", len(imgs))
	}
	img := imgs[0]
	// unrotated placement (100, 200, 80x80): rotate 90 -> (842-200-80, 100),
	// display dims swap to 80 wide (was height) x 80
	if math.Abs(img.X-(842-200-80)) > 2 || math.Abs(img.Y-100) > 2 {
		t.Errorf("rotated image pos = (%.0f, %.0f), want (482, 100)", img.X, img.Y)
	}
	if img.DisplayWidth != 80 || img.DisplayHeight != 80 {
		t.Errorf("display = %.0fx%.0f, want 80x80", img.DisplayWidth, img.DisplayHeight)
	}
}
