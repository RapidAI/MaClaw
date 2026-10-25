package agent

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"image"
	"io"
	"math"
	"runtime"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/docgen"
	gopdf2 "github.com/VantageDataChat/GoPDF2"
)

func TestPlaceScanLinesMapsImageBox(t *testing.T) {
	got := PlaceScanLines(ScanImagePlacement{
		X: 72, Y: 100, DisplayW: 200, DisplayH: 100, PixelW: 40, PixelH: 20,
	}, []ScanTextLine{{Text: "恢复正文", BBox: [4]int{0, 0, 40, 10}}})
	if len(got) != 1 {
		t.Fatalf("lines = %d", len(got))
	}
	line := got[0]
	if math.Abs(line.X-72) > 0.01 || math.Abs(line.Y-100) > 0.01 || math.Abs(line.W-200) > 0.01 || math.Abs(line.H-50) > 0.01 {
		t.Fatalf("placed = %+v", line)
	}
}

func TestPlaceScanLinesKeepsWideFormula(t *testing.T) {
	place := ScanImagePlacement{X: 72, Y: 100, DisplayW: 200, DisplayH: 100, PixelW: 40, PixelH: 20}
	latex := strings.Repeat("a", 80)
	got := PlaceScanLines(place, []ScanTextLine{
		{Text: latex, Formula: true, BBox: [4]int{0, 0, 4, 4}},
		{Text: latex, BBox: [4]int{8, 0, 4, 4}},
	})
	if len(got) != 1 || !got[0].Formula || got[0].Text != latex {
		t.Fatalf("placed = %+v", got)
	}
	font, err := docgen.SystemCJKFontBytes()
	if err != nil {
		if runtime.GOOS != "windows" {
			t.Skip(err.Error())
		}
		t.Fatal(err)
	}
	src := buildRestorableScanPDF(t, mustJPEG(t, 40, 20), 72, 100, 200, 100)
	out, err := RestoreScannedPDF(src, map[int][]PlacedScanLine{1: got}, font)
	if err != nil {
		t.Fatal(err)
	}
	text, err := gopdf2.ExtractPageText(out, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, latex) {
		t.Fatalf("extracted = %q", text)
	}
}

func TestApplyFormulaLinesKeepsProse(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 120, 80))
	calls := 0
	lines := ApplyFormulaLines([]ScanTextLine{
		{Text: "a=b", BBox: [4]int{0, 0, 20, 10}},
		{Text: "E=mc^2", BBox: [4]int{0, 16, 40, 12}},
		{Text: "恢复正文", BBox: [4]int{0, 32, 40, 12}},
		{Text: "well-known", BBox: [4]int{0, 48, 40, 12}},
		{Text: "Table 1", BBox: [4]int{0, 64, 30, 10}},
		{Text: "第3章", BBox: [4]int{40, 0, 24, 12}},
	}, img, func(crop image.Image) (string, error) {
		calls++
		if crop == nil || crop.Bounds().Empty() {
			t.Fatal("empty crop")
		}
		if calls == 1 {
			b := crop.Bounds()
			if b.Min.X != 0 || b.Min.Y != 0 || b.Dx() != 24 || b.Dy() != 14 {
				t.Fatalf("first crop = %v", b)
			}
			return `a=b`, nil
		}
		return `E=mc^{2}`, nil
	})
	if calls != 2 {
		t.Fatalf("formula calls = %d", calls)
	}
	if !lines[0].Formula || lines[0].Text != "a=b" || !lines[1].Formula || lines[1].Text != `E=mc^{2}` {
		t.Fatalf("formula lines = %+v %+v", lines[0], lines[1])
	}
	for _, line := range lines[2:] {
		if line.Formula {
			t.Fatalf("prose marked formula: %+v", line)
		}
	}
	if lines[2].Text != "恢复正文" || lines[3].Text != "well-known" || lines[4].Text != "Table 1" || lines[5].Text != "第3章" {
		t.Fatalf("prose changed: %+v", lines[2:])
	}
}

func TestApplyFormulaLinesKeepsOCRWhenRecognizeFails(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 40, 20))
	lines := ApplyFormulaLines([]ScanTextLine{{Text: "a=b", BBox: [4]int{0, 0, 20, 10}}}, img, func(image.Image) (string, error) {
		return "", nil
	})
	if lines[0].Formula || lines[0].Text != "a=b" {
		t.Fatalf("line = %+v", lines[0])
	}
}

func TestApplyFormulaRegionsRecognizesTheBoxOnce(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 80, 40))
	calls := 0
	lines := ApplyFormulaRegions([]ScanTextLine{
		{Text: "分子", BBox: [4]int{2, 2, 20, 8}},
		{Text: "分母", BBox: [4]int{2, 14, 20, 8}},
		{Text: "旁边正文", BBox: [4]int{40, 2, 30, 12}},
	}, img, func(crop image.Image) (string, error) {
		calls++
		b := crop.Bounds()
		if b.Dx() < 30 || b.Dy() < 24 {
			t.Fatalf("crop = %v", b)
		}
		return `\frac{a}{b}`, nil
	}, []FormulaRegion{{X0: 0, Y0: 0, X1: 36, Y1: 28}})
	if calls != 1 || len(lines) != 2 || lines[0].Text != `\frac{a}{b}` || !lines[0].Formula || lines[1].Text != "旁边正文" || lines[1].Formula {
		t.Fatalf("calls=%d lines=%+v", calls, lines)
	}
}

func TestApplyFormulaRegionsInsertsWhenOCRMissedTheBox(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 80, 40))
	lines := ApplyFormulaRegions(nil, img, func(image.Image) (string, error) {
		return `x`, nil
	}, []FormulaRegion{{X0: 0, Y0: 0, X1: 20, Y1: 12}})
	if len(lines) != 1 || lines[0].Text != "x" || !lines[0].Formula || lines[0].BBox != [4]int{0, 0, 20, 12} {
		t.Fatalf("lines = %+v", lines)
	}
}

func TestApplyFormulaRegionsKeepsOCRWhenRecognizeFails(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 80, 40))
	in := []ScanTextLine{
		{Text: "分子", BBox: [4]int{2, 2, 20, 8}},
		{Text: "分母", BBox: [4]int{2, 14, 20, 8}},
	}
	lines := ApplyFormulaRegions(in, img, func(image.Image) (string, error) {
		return "", nil
	}, []FormulaRegion{{X0: 0, Y0: 0, X1: 36, Y1: 28}})
	if len(lines) != 2 || lines[0].Text != "分子" || lines[0].Formula || lines[1].Text != "分母" {
		t.Fatalf("lines = %+v", lines)
	}
}

func TestApplyFormulaCropsUsesLayoutMark(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 80, 40))
	calls := 0
	lines := ApplyFormulaCrops([]ScanTextLine{
		{Text: "恢复正文", BBox: [4]int{0, 0, 30, 12}},
		{Text: "旁边正文", BBox: [4]int{40, 0, 30, 12}},
	}, img, func(image.Image) (string, error) {
		calls++
		return `E=mc^{2}`, nil
	}, func(line ScanTextLine) bool {
		return line.BBox[0] == 0
	})
	if calls != 1 || !lines[0].Formula || lines[0].Text != `E=mc^{2}` || lines[1].Formula || lines[1].Text != "旁边正文" {
		t.Fatalf("calls=%d lines=%+v %+v", calls, lines[0], lines[1])
	}
}

func TestRestoreScannedPDFPaintsSelectableText(t *testing.T) {
	font, err := docgen.SystemCJKFontBytes()
	if err != nil {
		if runtime.GOOS != "windows" {
			t.Skip(err.Error())
		}
		t.Fatal(err)
	}
	jpegBytes := mustJPEG(t, 40, 20)
	src := buildRestorableScanPDF(t, jpegBytes, 72, 100, 200, 100)
	view, err := ScanPageForRestore(src, 1)
	if err != nil {
		t.Fatal(err)
	}
	if view.HasText || view.NoPicture || view.Placement.FullPage {
		t.Fatalf("view = %+v", view)
	}
	if math.Abs(view.Placement.X-72) > 0.6 || math.Abs(view.Placement.Y-100) > 0.6 ||
		math.Abs(view.Placement.DisplayW-200) > 0.6 || math.Abs(view.Placement.DisplayH-100) > 0.6 {
		t.Fatalf("placement = %+v", view.Placement)
	}
	if view.Placement.PixelW != 40 || view.Placement.PixelH != 20 {
		t.Fatalf("pixels = %d x %d", view.Placement.PixelW, view.Placement.PixelH)
	}
	placed := PlaceScanLines(view.Placement, []ScanTextLine{{Text: "恢复正文", BBox: [4]int{0, 0, 40, 10}}})
	out, err := RestoreScannedPDF(src, map[int][]PlacedScanLine{1: placed}, font)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, jpegBytes) {
		t.Fatal("restored pdf dropped the scan image")
	}
	text, err := gopdf2.ExtractPageText(out, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "恢复正文") {
		t.Fatalf("extracted = %q", text)
	}
	fragments, err := gopdf2.ExtractTextFromPage(out, 0)
	if err != nil {
		t.Fatal(err)
	}
	var near bool
	for _, frag := range fragments {
		if strings.Contains(frag.Text, "恢") || strings.Contains(frag.Text, "恢复正文") {
			if math.Abs(frag.X-72) < 8 {
				near = true
			}
		}
	}
	if !near {
		t.Fatalf("fragments = %+v", fragments)
	}
}

func TestRestoreScannedPDFKeepsLaterPages(t *testing.T) {
	font, err := docgen.SystemCJKFontBytes()
	if err != nil {
		if runtime.GOOS != "windows" {
			t.Skip(err.Error())
		}
		t.Fatal(err)
	}
	jpegBytes := mustJPEG(t, 40, 20)
	src := buildRestorableScanPDFPages(t, jpegBytes, [][4]float64{{72, 100, 200, 100}, {36, 40, 120, 80}})
	view, err := ScanPageForRestore(src, 1)
	if err != nil {
		t.Fatal(err)
	}
	placed := PlaceScanLines(view.Placement, []ScanTextLine{{Text: "恢复正文", BBox: [4]int{0, 0, 40, 10}}})
	out, err := RestoreScannedPDF(src, map[int][]PlacedScanLine{1: placed}, font)
	if err != nil {
		t.Fatal(err)
	}
	count, err := gopdf2.GetSourcePDFPageCountFromBytes(out)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("pages = %d", count)
	}
	text, err := gopdf2.ExtractPageText(out, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "恢复正文") {
		t.Fatalf("page1 = %q", text)
	}
	images, err := gopdf2.ExtractImagesFromPage(out, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(images) == 0 || math.Abs(images[0].X-36) > 0.6 || math.Abs(images[0].DisplayWidth-120) > 0.6 {
		t.Fatalf("page2 images = %+v", images)
	}
}

func TestScanRestoreParsesGeometryOnce(t *testing.T) {
	jpegBytes := mustJPEG(t, 40, 20)
	src := buildRestorableScanPDFPages(t, jpegBytes, [][4]float64{{72, 100, 200, 100}, {36, 40, 120, 80}})
	doc := NewScanRestore(src)
	first, err := doc.Page(1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := doc.Page(2)
	if err != nil {
		t.Fatal(err)
	}
	again, err := doc.Page(1)
	if err != nil {
		t.Fatal(err)
	}
	solo1, err := ScanPageForRestore(src, 1)
	if err != nil {
		t.Fatal(err)
	}
	solo2, err := ScanPageForRestore(src, 2)
	if err != nil {
		t.Fatal(err)
	}
	if doc.geomReads != 1 || doc.reader.pageCount != 2 || doc.reader.classicReads != 1 || doc.reader.opens != 1 || doc.reader.countParses != 0 || doc.reader.textFalls != 0 || doc.reader.pageRoot == nil {
		t.Fatalf("geom=%d pages=%d classic=%d opens=%d countParses=%d textFalls=%d pageRoot=%v", doc.geomReads, doc.reader.pageCount, doc.reader.classicReads, doc.reader.opens, doc.reader.countParses, doc.reader.textFalls, doc.reader.pageRoot != nil)
	}
	for _, pair := range [][2]ScanPageView{{first, solo1}, {again, solo1}, {second, solo2}} {
		if pair[0].HasText != pair[1].HasText || pair[0].NoPicture != pair[1].NoPicture || !scanPlacementEqual(pair[0].Placement, pair[1].Placement) {
			t.Fatalf("shared %+v solo %+v", pair[0], pair[1])
		}
	}
}

func TestRestoreScannedPDFReusesPageReads(t *testing.T) {
	font, err := docgen.SystemCJKFontBytes()
	if err != nil {
		if runtime.GOOS != "windows" {
			t.Skip(err.Error())
		}
		t.Fatal(err)
	}
	jpegBytes := mustJPEG(t, 40, 20)
	src := buildRestorableScanPDFPages(t, jpegBytes, [][4]float64{{72, 100, 200, 100}, {36, 40, 120, 80}})
	doc := NewScanRestore(src)
	if doc.Matches(int64(len(src)), 1) {
		t.Fatal("unbound reader matched a file")
	}
	if _, err := doc.Page(1); err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Page(2); err != nil {
		t.Fatal(err)
	}
	if doc.extracts != 2 {
		t.Fatalf("extracts = %d", doc.extracts)
	}
	view, err := doc.Page(1)
	if err != nil {
		t.Fatal(err)
	}
	if doc.extracts != 2 {
		t.Fatalf("cached page extracted again: %d", doc.extracts)
	}
	doc.BindSource(int64(len(src)), 42)
	if !doc.Matches(int64(len(src)), 42) || doc.Matches(int64(len(src)), 43) || doc.Bytes() == nil {
		t.Fatal("source stamp did not follow size and mtime")
	}
	placed := PlaceScanLines(view.Placement, []ScanTextLine{{Text: "恢复正文", BBox: [4]int{0, 0, 40, 10}}})
	out, err := RestoreScannedPDFWith(doc, map[int][]PlacedScanLine{1: placed}, font)
	if err != nil {
		t.Fatal(err)
	}
	if doc.extracts != 2 || doc.reader.classicReads != 1 || doc.reader.opens != 1 || doc.reader.countParses != 0 || doc.reader.textFalls != 0 {
		t.Fatalf("extracts=%d classic=%d opens=%d countParses=%d textFalls=%d", doc.extracts, doc.reader.classicReads, doc.reader.opens, doc.reader.countParses, doc.reader.textFalls)
	}
	if !bytes.Contains(out, jpegBytes) {
		t.Fatal("restored pdf dropped the scan image")
	}
	text, err := gopdf2.ExtractPageText(out, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "恢复正文") {
		t.Fatalf("extracted = %q", text)
	}
}

func TestOfficeImageReadDoesNotRetainClassicParse(t *testing.T) {
	jpegBytes := mustJPEG(t, 40, 20)
	src := buildRestorableScanPDFPages(t, jpegBytes, [][4]float64{{72, 100, 200, 100}, {36, 40, 120, 80}})
	var office pdfPageImageReader
	office.data = src
	if _, err := office.extract(0); err != nil {
		t.Fatal(err)
	}
	if _, err := office.extract(1); err != nil {
		t.Fatal(err)
	}
	if office.classicReads != 0 || office.pageExtracts != 2 {
		t.Fatalf("office classic=%d pageExtracts=%d", office.classicReads, office.pageExtracts)
	}
	doc := NewScanRestore(src)
	if _, err := doc.Page(1); err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Page(2); err != nil {
		t.Fatal(err)
	}
	if doc.reader.classicReads != 1 || doc.reader.pageExtracts != 0 {
		t.Fatalf("restore classic=%d pageExtracts=%d", doc.reader.classicReads, doc.reader.pageExtracts)
	}
}

func TestScanRestoreReadsTextWithoutImageParse(t *testing.T) {
	font, err := docgen.SystemCJKFontBytes()
	if err != nil {
		if runtime.GOOS != "windows" {
			t.Skip(err.Error())
		}
		t.Fatal(err)
	}
	pdf := gopdf2.GoPdf{}
	pdf.Start(gopdf2.Config{PageSize: *gopdf2.PageSizeA4})
	if err := pdf.AddTTFFontData("body", font); err != nil {
		t.Fatal(err)
	}
	pdf.AddPage()
	if err := pdf.SetFont("body", "", 14); err != nil {
		t.Fatal(err)
	}
	pdf.SetXY(72, 72)
	if err := pdf.Cell(nil, "已有正文"); err != nil {
		t.Fatal(err)
	}
	src, err := pdf.GetBytesPdfReturnErr()
	if err != nil {
		t.Fatal(err)
	}
	doc := NewScanRestore(src)
	view, err := doc.Page(1)
	if err != nil {
		t.Fatal(err)
	}
	if !view.HasText || doc.extracts != 0 || doc.reader.classicReads != 0 || doc.reader.textFalls != 0 {
		t.Fatalf("has=%v extracts=%d classic=%d textFalls=%d", view.HasText, doc.extracts, doc.reader.classicReads, doc.reader.textFalls)
	}
	if !doc.EveryPageHasText(1) || !doc.EveryPageIsDigital(1) || doc.reader.opens != 1 || doc.reader.countParses != 0 || doc.reader.textFalls != 0 || doc.extracts != 0 {
		t.Fatalf("opens=%d countParses=%d textFalls=%d extracts=%d", doc.reader.opens, doc.reader.countParses, doc.reader.textFalls, doc.extracts)
	}
}

func TestScanRestoreKeepsACoveringImageUnderPageText(t *testing.T) {
	font := loadScanRestoreFont(t)
	jpegBytes := mustJPEG(t, 300, 400)
	src := buildScanPDFWithText(t, font, jpegBytes, 20, 30, 550, 800, "页码")
	doc := NewScanRestore(src)
	view, err := doc.Page(1)
	if err != nil {
		t.Fatal(err)
	}
	if view.HasText || len(view.Payload) == 0 || len(view.Images) == 0 {
		t.Fatalf("covering page was treated as digital: has=%v images=%d", view.HasText, len(view.Images))
	}
	if !bytes.Equal(view.Payload, jpegBytes) {
		t.Fatal("ocr image is not the scan")
	}
	if !doc.EveryPageHasText(1) || doc.EveryPageIsDigital(1) {
		t.Fatal("page text and the covering image were not told apart")
	}
	if doc.reader.classicReads != 1 {
		t.Fatalf("classicReads=%d", doc.reader.classicReads)
	}
}

func TestScanRestoreKeepsATextPageThatOnlyHasALogo(t *testing.T) {
	font := loadScanRestoreFont(t)
	jpegBytes := mustJPEG(t, 40, 20)
	src := buildScanPDFWithText(t, font, jpegBytes, 72, 72, 36, 18, "已有正文")
	doc := NewScanRestore(src)
	view, err := doc.Page(1)
	if err != nil {
		t.Fatal(err)
	}
	if !view.HasText || len(view.Payload) != 0 || len(view.Images) != 0 {
		t.Fatalf("logo replaced the text page: has=%v payload=%d images=%d", view.HasText, len(view.Payload), len(view.Images))
	}
	if !doc.EveryPageIsDigital(1) || doc.extracts != 1 || doc.reader.classicReads != 1 {
		t.Fatalf("digital=%v extracts=%d classic=%d", doc.EveryPageIsDigital(1), doc.extracts, doc.reader.classicReads)
	}
}

func loadScanRestoreFont(t *testing.T) []byte {
	t.Helper()
	font, err := docgen.SystemCJKFontBytes()
	if err != nil {
		if runtime.GOOS != "windows" {
			t.Skip(err.Error())
		}
		t.Fatal(err)
	}
	return font
}

func buildScanPDFWithText(t *testing.T, font, jpeg []byte, x, y, w, h float64, text string) []byte {
	t.Helper()
	pdf := gopdf2.GoPdf{}
	pdf.Start(gopdf2.Config{PageSize: *gopdf2.PageSizeA4})
	if err := pdf.AddTTFFontData("body", font); err != nil {
		t.Fatal(err)
	}
	pdf.AddPage()
	holder, err := gopdf2.ImageHolderByBytes(jpeg)
	if err != nil {
		t.Fatal(err)
	}
	if err := pdf.ImageByHolder(holder, x, y, &gopdf2.Rect{W: w, H: h}); err != nil {
		t.Fatal(err)
	}
	if err := pdf.SetFont("body", "", 14); err != nil {
		t.Fatal(err)
	}
	pdf.SetXY(72, 18)
	if err := pdf.Cell(nil, text); err != nil {
		t.Fatal(err)
	}
	out, err := pdf.GetBytesPdfReturnErr()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestScanRestorePageCountAndSizeUseTextReader(t *testing.T) {
	jpegBytes := mustJPEG(t, 40, 20)
	src := buildRestorableScanPDFPages(t, jpegBytes, [][4]float64{{72, 100, 200, 100}, {36, 40, 120, 80}})
	want, err := gopdf2.GetSourcePDFPageSizesFromBytes(src)
	if err != nil {
		t.Fatal(err)
	}
	doc := NewScanRestore(src)
	n, err := doc.PageCount()
	if err != nil {
		t.Fatal(err)
	}
	if n != len(want) || n != 2 {
		t.Fatalf("pages = %d, sizes = %d", n, len(want))
	}
	for page := 1; page <= n; page++ {
		w, h := doc.pageSize(page)
		info, ok := want[page]
		if !ok || math.Abs(w-info.Width) > 0.01 || math.Abs(h-info.Height) > 0.01 {
			t.Fatalf("page %d size got %v,%v want %+v", page, w, h, info)
		}
	}
	if doc.EveryPageHasText(n) {
		t.Fatal("scan pages were treated as text")
	}
	if doc.EveryPageIsDigital(n) || doc.reader.classicReads != 0 {
		t.Fatalf("digital check parsed images or accepted the scan: classic=%d", doc.reader.classicReads)
	}
	if doc.reader.opens != 1 || doc.reader.countParses != 0 || doc.geomReads != 1 || doc.reader.pageRoot == nil {
		t.Fatalf("opens=%d countParses=%d geom=%d pageRoot=%v", doc.reader.opens, doc.reader.countParses, doc.geomReads, doc.reader.pageRoot != nil)
	}
	var office pdfPageImageReader
	office.data = src
	got, err := office.numPages()
	if err != nil {
		t.Fatal(err)
	}
	if got != n || office.countParses != 1 || office.opens != 0 || office.pageRoot != nil {
		t.Fatalf("office pages=%d countParses=%d opens=%d pageRoot=%v", got, office.countParses, office.opens, office.pageRoot != nil)
	}
}

func TestScanRestoreImageTopFollowsPageTreeNotFileOrder(t *testing.T) {
	jpegBytes := mustJPEG(t, 40, 20)
	// A decoy /Type /Page with its own MediaBox sits earlier in the file.
	// The page tree's only page omits MediaBox, so the image top is measured
	// from US Letter and has to move down to the real 842pt page.
	src := buildScanPDFWithEarlierPageObject(t, jpegBytes, "", "")
	doc := NewScanRestore(src)
	n, err := doc.PageCount()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("pages = %d", n)
	}
	view, err := doc.Page(1)
	if err != nil {
		t.Fatal(err)
	}
	if view.HasText || view.NoPicture {
		t.Fatalf("view = %+v", view)
	}
	const (
		ctmF    = 100.0
		ctmD    = 100.0
		pageH   = 842.0
		letterH = 792.0
		rawTop  = letterH - ctmF - ctmD
		shifted = rawTop + (pageH - letterH)
	)
	if math.Abs(view.Placement.DisplayW-200) > 0.6 || math.Abs(view.Placement.DisplayH-ctmD) > 0.6 {
		t.Fatalf("display = %+v", view.Placement)
	}
	if math.Abs(view.Placement.Y-shifted) > 0.6 {
		t.Fatalf("Y = %v, want %v (unshifted %v)", view.Placement.Y, shifted, rawTop)
	}
	if view.Placement.MirrorX || view.Placement.MirrorY || view.Placement.Turn != 0 {
		t.Fatalf("upright placement = %+v", view.Placement)
	}
	// The same decoy must not shift a page that already has its own MediaBox.
	owned := buildScanPDFWithEarlierPageObject(t, jpegBytes, "/MediaBox [0 0 595 842]", "")
	ownedView, err := NewScanRestore(owned).Page(1)
	if err != nil {
		t.Fatal(err)
	}
	ownedTop := pageH - ctmF - ctmD
	if math.Abs(ownedView.Placement.Y-ownedTop) > 0.6 {
		t.Fatalf("owned Y = %v, want %v", ownedView.Placement.Y, ownedTop)
	}
	// /MediaBox 7 0 R resolves to the real box, but the image extractor
	// only reads a direct array and still measures from US Letter.
	indirect := buildScanPDFWithEarlierPageObject(t, jpegBytes, "/MediaBox 7 0 R", "")
	indirectView, err := NewScanRestore(indirect).Page(1)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(indirectView.Placement.Y-shifted) > 0.6 {
		t.Fatalf("indirect Y = %v, want %v (unshifted %v)", indirectView.Placement.Y, shifted, rawTop)
	}
}

func TestScanRestorePlacesAMirroredImage(t *testing.T) {
	jpegBytes := mustJPEG(t, 40, 20)
	// 200 0 0 -100 72 200 cm. The extractor stores d as the display height and
	// measures the top as pageHeight - f - d, one height too low when d is
	// negative. The page omits MediaBox, so that top is also shifted from 792.
	src := buildScanPDFWithEarlierPageObject(t, jpegBytes, "", "200 0 0 -100 72 200")
	view, err := NewScanRestore(src).Page(1)
	if err != nil {
		t.Fatal(err)
	}
	if view.Placement.FullPage || view.Placement.MirrorX || !view.Placement.MirrorY || view.Placement.Turn != 0 ||
		math.Abs(view.Placement.DisplayW-200) > 0.6 || math.Abs(view.Placement.DisplayH-100) > 0.6 {
		t.Fatalf("display = %+v", view.Placement)
	}
	if math.Abs(view.Placement.X-72) > 0.6 || math.Abs(view.Placement.Y-642) > 0.6 {
		t.Fatalf("origin = %v,%v", view.Placement.X, view.Placement.Y)
	}
	// The file's top row is painted at the bottom of the rectangle.
	placed := PlaceScanLines(view.Placement, []ScanTextLine{{Text: "页", BBox: [4]int{0, 0, 40, 10}}})
	if len(placed) != 1 || math.Abs(placed[0].X-72) > 0.6 || math.Abs(placed[0].Y-692) > 0.6 || math.Abs(placed[0].H-50) > 0.6 {
		t.Fatalf("line = %+v", placed)
	}
	bottom := PlaceScanLines(view.Placement, []ScanTextLine{{Text: "页", BBox: [4]int{0, 10, 40, 10}}})
	if len(bottom) != 1 || math.Abs(bottom[0].Y-642) > 0.6 {
		t.Fatalf("bottom line = %+v", bottom)
	}
	font := loadScanRestoreFont(t)
	doc := NewScanRestore(src)
	if _, err := doc.Page(1); err != nil {
		t.Fatal(err)
	}
	out, err := RestoreScannedPDFWith(doc, map[int][]PlacedScanLine{1: placed}, font)
	if err != nil {
		t.Fatal(err)
	}
	if !pdfContentHas(out, "1 0 0 -1 0 0 cm") || !pdfContentHas(out, "100.00 72.00 -200.00 cm") || pdfContentHas(out, "-1 0 0 -1 0 0 cm") {
		t.Fatal("vertical mirror was not drawn")
	}
	// A direct MediaBox has no letter shift. Folding the negative height
	// still lands on the same rectangle.
	owned := buildScanPDFWithEarlierPageObject(t, jpegBytes, "/MediaBox [0 0 595 842]", "200 0 0 -100 72 200")
	ownedView, err := NewScanRestore(owned).Page(1)
	if err != nil {
		t.Fatal(err)
	}
	if ownedView.Placement.FullPage || !ownedView.Placement.MirrorY || ownedView.Placement.MirrorX || ownedView.Placement.Turn != 0 ||
		math.Abs(ownedView.Placement.Y-642) > 0.6 || math.Abs(ownedView.Placement.DisplayH-100) > 0.6 || math.Abs(ownedView.Placement.X-72) > 0.6 {
		t.Fatalf("owned = %+v", ownedView.Placement)
	}
	// Negative width and height move both edges and reflect both axes.
	flippedW := buildScanPDFWithEarlierPageObject(t, jpegBytes, "/MediaBox [0 0 595 842]", "-200 0 0 -100 272 200")
	flipDoc := NewScanRestore(flippedW)
	wView, err := flipDoc.Page(1)
	if err != nil {
		t.Fatal(err)
	}
	if wView.Placement.FullPage || !wView.Placement.MirrorX || !wView.Placement.MirrorY || wView.Placement.Turn != 0 ||
		math.Abs(wView.Placement.X-72) > 0.6 || math.Abs(wView.Placement.Y-642) > 0.6 ||
		math.Abs(wView.Placement.DisplayW-200) > 0.6 || math.Abs(wView.Placement.DisplayH-100) > 0.6 {
		t.Fatalf("flipped = %+v", wView.Placement)
	}
	both := PlaceScanLines(wView.Placement, []ScanTextLine{{Text: "页", BBox: [4]int{0, 0, 20, 10}}})
	if len(both) != 1 || math.Abs(both[0].X-172) > 0.6 || math.Abs(both[0].Y-692) > 0.6 || math.Abs(both[0].W-100) > 0.6 {
		t.Fatalf("both line = %+v", both)
	}
	bothOut, err := RestoreScannedPDFWith(flipDoc, map[int][]PlacedScanLine{1: both}, font)
	if err != nil {
		t.Fatal(err)
	}
	if !pdfContentHas(bothOut, "-1 0 0 -1 0 0 cm") || !pdfContentHas(bothOut, "100.00 -272.00 -200.00 cm") {
		t.Fatal("both mirrors were not drawn")
	}
}

func TestScanRestoreComposesTheImageCTM(t *testing.T) {
	jpegBytes := mustJPEG(t, 40, 20)
	// The flip and the image size are two cm operators. The last one alone is
	// a positive 200 by 100 whose f is -200, which lands below the page.
	split := "q\n1 0 0 -1 0 0 cm\nq\n200 0 0 100 72 -200 cm\n/Im1 Do\nQ\nQ\n"
	view, err := NewScanRestore(buildScanPDFWithEarlierPageObject(t, jpegBytes, "", split)).Page(1)
	if err != nil {
		t.Fatal(err)
	}
	if view.Placement.FullPage || view.Placement.MirrorX || !view.Placement.MirrorY || view.Placement.Turn != 0 ||
		math.Abs(view.Placement.X-72) > 0.6 || math.Abs(view.Placement.Y-642) > 0.6 ||
		math.Abs(view.Placement.DisplayW-200) > 0.6 || math.Abs(view.Placement.DisplayH-100) > 0.6 {
		t.Fatalf("split = %+v", view.Placement)
	}
	// Q restores the matrix from before q. The inner cm is not the image.
	restored := "200 0 0 -100 72 200 cm\nq\n10 0 0 10 0 0 cm\nQ\n/Im1 Do\n"
	owned, err := NewScanRestore(buildScanPDFWithEarlierPageObject(t, jpegBytes, "/MediaBox [0 0 595 842]", restored)).Page(1)
	if err != nil {
		t.Fatal(err)
	}
	if owned.Placement.FullPage || owned.Placement.MirrorX || !owned.Placement.MirrorY || owned.Placement.Turn != 0 ||
		math.Abs(owned.Placement.X-72) > 0.6 || math.Abs(owned.Placement.Y-642) > 0.6 ||
		math.Abs(owned.Placement.DisplayW-200) > 0.6 || math.Abs(owned.Placement.DisplayH-100) > 0.6 {
		t.Fatalf("restored = %+v", owned.Placement)
	}
}

func TestScanRestorePlacesATurnedImage(t *testing.T) {
	jpegBytes := mustJPEG(t, 40, 20)
	// 0 200 -100 0 172 200 is a quarter turn. a and d are 0, so the
	// axis-aligned size is missing. The pre-rotation rect is 200 by 100
	// at (22, 492) on an 842pt page, and GoPDF2 rotates it 90 degrees.
	const plain = "0 200 -100 0 172 200"
	direct := buildScanPDFWithEarlierPageObject(t, jpegBytes, "/MediaBox [0 0 595 842]", plain)
	view, err := NewScanRestore(direct).Page(1)
	if err != nil {
		t.Fatal(err)
	}
	if view.Placement.FullPage || view.Placement.MirrorX || view.Placement.MirrorY ||
		math.Abs(view.Placement.Turn-90) > 0.6 ||
		math.Abs(view.Placement.X-22) > 0.6 || math.Abs(view.Placement.Y-492) > 0.6 ||
		math.Abs(view.Placement.DisplayW-200) > 0.6 || math.Abs(view.Placement.DisplayH-100) > 0.6 {
		t.Fatalf("turned = %+v", view.Placement)
	}
	// The same matrix on a page that omits MediaBox uses the real page
	// height. The letter default must not move this rectangle.
	omit, err := NewScanRestore(buildScanPDFWithEarlierPageObject(t, jpegBytes, "", plain)).Page(1)
	if err != nil {
		t.Fatal(err)
	}
	if !scanPlacementEqual(view.Placement, omit.Placement) {
		t.Fatalf("omit = %+v direct = %+v", omit.Placement, view.Placement)
	}
	// Top half of a 40 by 20 image. The CTM maps it to x 72..122, PDF y 200..400.
	placed := PlaceScanLines(view.Placement, []ScanTextLine{{Text: "页", BBox: [4]int{0, 0, 40, 10}}})
	if len(placed) != 1 || math.Abs(placed[0].X-72) > 0.6 || math.Abs(placed[0].Y-442) > 0.6 ||
		math.Abs(placed[0].W-50) > 0.6 || math.Abs(placed[0].H-200) > 0.6 {
		t.Fatalf("line = %+v", placed)
	}
	font := loadScanRestoreFont(t)
	doc := NewScanRestore(direct)
	if _, err := doc.Page(1); err != nil {
		t.Fatal(err)
	}
	out, err := RestoreScannedPDFWith(doc, map[int][]PlacedScanLine{1: placed}, font)
	if err != nil {
		t.Fatal(err)
	}
	assertImageCorners(t, out, [4][2]float64{{172, 200}, {172, 400}, {72, 200}, {72, 400}})
	// det < 0 is the same center and angle with a vertical flip.
	flipped := buildScanPDFWithEarlierPageObject(t, jpegBytes, "/MediaBox [0 0 595 842]", "0 200 100 0 72 200")
	flipDoc := NewScanRestore(flipped)
	flipView, err := flipDoc.Page(1)
	if err != nil {
		t.Fatal(err)
	}
	if flipView.Placement.FullPage || flipView.Placement.MirrorX || !flipView.Placement.MirrorY ||
		math.Abs(flipView.Placement.Turn-90) > 0.6 ||
		math.Abs(flipView.Placement.X-22) > 0.6 || math.Abs(flipView.Placement.Y-492) > 0.6 ||
		math.Abs(flipView.Placement.DisplayW-200) > 0.6 || math.Abs(flipView.Placement.DisplayH-100) > 0.6 {
		t.Fatalf("flipped turn = %+v", flipView.Placement)
	}
	flipLine := PlaceScanLines(flipView.Placement, []ScanTextLine{{Text: "页", BBox: [4]int{0, 0, 40, 10}}})
	if len(flipLine) != 1 || math.Abs(flipLine[0].X-122) > 0.6 || math.Abs(flipLine[0].Y-442) > 0.6 ||
		math.Abs(flipLine[0].W-50) > 0.6 || math.Abs(flipLine[0].H-200) > 0.6 {
		t.Fatalf("flipped line = %+v", flipLine)
	}
	flipOut, err := RestoreScannedPDFWith(flipDoc, map[int][]PlacedScanLine{1: flipLine}, font)
	if err != nil {
		t.Fatal(err)
	}
	assertImageCorners(t, flipOut, [4][2]float64{{72, 200}, {72, 400}, {172, 200}, {172, 400}})
}

func TestScanRestoreTurnedExtentCoversThePage(t *testing.T) {
	jpegBytes := mustJPEG(t, 200, 200)
	img := gopdf2.ExtractedImage{Width: 200, Height: 200}
	// 0 842 -595 0 595 0 fills an A4 page. a and d are 0.
	turned := scanCTM{b: 842, c: -595, e: 595}
	baked := gopdf2.ExtractedImage{Width: 200, Height: 200, DisplayWidth: turned.a, DisplayHeight: turned.d}
	if scanImageCoversPage(baked, jpegBytes, 595, 842) {
		t.Fatal("zero a and d covered the page")
	}
	if !ctmCoversPage(turned, jpegBytes, img, 595, 842) {
		t.Fatal("quarter turn did not cover the page")
	}
	small := mustJPEG(t, 40, 20)
	if ctmCoversPage(turned, small, gopdf2.ExtractedImage{Width: 40, Height: 20}, 595, 842) {
		t.Fatal("a small image covered the page")
	}
	upright := scanCTM{a: 200, d: -100, e: 72, f: 200}
	w, h := ctmExtent(upright)
	if math.Abs(w-200) > 0.01 || math.Abs(h-100) > 0.01 {
		t.Fatalf("extent = %v,%v", w, h)
	}
	if ctmCoversPage(upright, jpegBytes, img, 595, 842) {
		t.Fatal("200 by 100 covered A4")
	}
	if ctmCoversPage(scanCTMIdent(), jpegBytes, img, 595, 842) {
		t.Fatal("identity covered the page")
	}
}

func assertImageCorners(t *testing.T, pdf []byte, want [4][2]float64) {
	t.Helper()
	byName, ok := NewScanRestore(pdf).reader.composedImageCTM(0)
	if !ok || len(byName) == 0 {
		t.Fatal("drawn pdf has no image matrix")
	}
	var m scanCTM
	for _, mat := range byName {
		m = mat
		break
	}
	got := [4][2]float64{
		{m.e, m.f},
		{m.a + m.e, m.b + m.f},
		{m.c + m.e, m.d + m.f},
		{m.a + m.c + m.e, m.b + m.d + m.f},
	}
	for i := range want {
		if math.Abs(got[i][0]-want[i][0]) > 1.5 || math.Abs(got[i][1]-want[i][1]) > 1.5 {
			t.Fatalf("corner %d = %v, want %v (matrix %+v)", i, got[i], want[i], m)
		}
	}
}

// buildScanPDFWithEarlierPageObject writes a one-page scan. An unused page
// object with a MediaBox is stored before the real page. pageMedia is the
// MediaBox entry on the page the tree draws, such as a direct array or
// "/MediaBox 7 0 R". An indirect entry adds object 7 as that array. cm is
// the image matrix, or the whole content stream when it already contains Do.
// An empty matrix is the upright 200 by 100 rect.
func buildScanPDFWithEarlierPageObject(t *testing.T, jpeg []byte, pageMedia, cm string) []byte {
	t.Helper()
	if cm == "" {
		cm = "200 0 0 100 72 100"
	}
	var content []byte
	if strings.Contains(cm, "Do") {
		content = []byte(cm)
		if !strings.HasSuffix(cm, "\n") {
			content = append(content, '\n')
		}
	} else {
		content = []byte("q\n" + cm + " cm\n/Im1 Do\nQ\n")
	}
	if pageMedia != "" {
		pageMedia = " " + strings.TrimSpace(pageMedia)
	}
	pageDict := "<< /Type /Page /Parent 2 0 R" + pageMedia + " /Resources << /XObject << /Im1 5 0 R >> >> /Contents 6 0 R >>"
	indirect := strings.Contains(pageMedia, "7 0 R")
	nobj := 6
	if indirect {
		nobj = 7
	}
	var body bytes.Buffer
	body.WriteString("%PDF-1.4\n")
	offsets := make([]int, nobj+1)
	writeObj := func(num int, data string) {
		offsets[num] = body.Len()
		fmt.Fprintf(&body, "%d 0 obj\n%s\nendobj\n", num, data)
	}
	writeObj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObj(2, "<< /Type /Pages /Count 1 /Kids [4 0 R] /MediaBox [0 0 595 842] >>")
	writeObj(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>")
	writeObj(4, pageDict)
	offsets[5] = body.Len()
	fmt.Fprintf(&body, "5 0 obj\n<< /Type /XObject /Subtype /Image /Width 40 /Height 20 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n", len(jpeg))
	body.Write(jpeg)
	body.WriteString("\nendstream\nendobj\n")
	offsets[6] = body.Len()
	fmt.Fprintf(&body, "6 0 obj\n<< /Length %d >>\nstream\n", len(content))
	body.Write(content)
	body.WriteString("endstream\nendobj\n")
	if indirect {
		writeObj(7, "[0 0 595 842]")
	}
	xrefAt := body.Len()
	fmt.Fprintf(&body, "xref\n0 %d\n", nobj+1)
	fmt.Fprintf(&body, "0000000000 65535 f \n")
	for i := 1; i <= nobj; i++ {
		fmt.Fprintf(&body, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&body, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", nobj+1, xrefAt)
	return body.Bytes()
}

func TestScanRestoreDrawsEveryImageAndReadsTheLargest(t *testing.T) {
	small := mustJPEG(t, 40, 20)
	large := mustJPEG(t, 80, 40)
	if bytes.Equal(small, large) {
		t.Fatal("jpeg fixtures are identical")
	}
	pdf := gopdf2.GoPdf{}
	pdf.Start(gopdf2.Config{PageSize: *gopdf2.PageSizeA4})
	pdf.AddPage()
	smallHolder, err := gopdf2.ImageHolderByBytes(small)
	if err != nil {
		t.Fatal(err)
	}
	if err := pdf.ImageByHolder(smallHolder, 72, 40, &gopdf2.Rect{W: 40, H: 20}); err != nil {
		t.Fatal(err)
	}
	largeHolder, err := gopdf2.ImageHolderByBytes(large)
	if err != nil {
		t.Fatal(err)
	}
	if err := pdf.ImageByHolder(largeHolder, 36, 200, &gopdf2.Rect{W: 400, H: 300}); err != nil {
		t.Fatal(err)
	}
	src, err := pdf.GetBytesPdfReturnErr()
	if err != nil {
		t.Fatal(err)
	}
	doc := NewScanRestore(src)
	view, err := doc.Page(1)
	if err != nil {
		t.Fatal(err)
	}
	if view.HasText || view.NoPicture || len(view.Images) != 2 {
		t.Fatalf("view images=%d has=%v none=%v", len(view.Images), view.HasText, view.NoPicture)
	}
	if view.Placement.PixelW != 80 || view.Placement.PixelH != 40 || math.Abs(view.Placement.DisplayW-400) > 0.6 {
		t.Fatalf("primary = %+v", view.Placement)
	}
	if !bytes.Equal(view.Payload, large) {
		t.Fatal("ocr image is not the larger scan")
	}
	font, err := docgen.SystemCJKFontBytes()
	if err != nil {
		if runtime.GOOS != "windows" {
			t.Skip(err.Error())
		}
		t.Fatal(err)
	}
	placed := PlaceScanLines(view.Placement, []ScanTextLine{{Text: "恢复正文", BBox: [4]int{0, 0, 40, 10}}})
	out, err := RestoreScannedPDFWith(doc, map[int][]PlacedScanLine{1: placed}, font)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, small) || !bytes.Contains(out, large) {
		t.Fatal("searchable pdf dropped an embedded image")
	}
}

func scanPlacementEqual(a, b ScanImagePlacement) bool {
	return math.Abs(a.X-b.X) < 0.6 && math.Abs(a.Y-b.Y) < 0.6 &&
		math.Abs(a.DisplayW-b.DisplayW) < 0.6 && math.Abs(a.DisplayH-b.DisplayH) < 0.6 &&
		a.PixelW == b.PixelW && a.PixelH == b.PixelH && a.FullPage == b.FullPage &&
		a.MirrorX == b.MirrorX && a.MirrorY == b.MirrorY &&
		math.Abs(a.Turn-b.Turn) < 0.6
}

func pdfContentHas(data []byte, needle string) bool {
	if bytes.Contains(data, []byte(needle)) {
		return true
	}
	rest := data
	for {
		i := bytes.Index(rest, []byte("stream"))
		if i < 0 {
			return false
		}
		rest = rest[i+len("stream"):]
		if len(rest) > 0 && (rest[0] == '\r' || rest[0] == '\n') {
			if rest[0] == '\r' && len(rest) > 1 && rest[1] == '\n' {
				rest = rest[2:]
			} else {
				rest = rest[1:]
			}
		}
		j := bytes.Index(rest, []byte("endstream"))
		if j < 0 {
			return false
		}
		raw := bytes.TrimRight(rest[:j], "\r\n")
		zr, err := zlib.NewReader(bytes.NewReader(raw))
		if err == nil {
			decoded, readErr := io.ReadAll(zr)
			zr.Close()
			if readErr == nil && bytes.Contains(decoded, []byte(needle)) {
				return true
			}
		}
		rest = rest[j+len("endstream"):]
	}
}

func buildRestorableScanPDF(t *testing.T, jpeg []byte, x, y, w, h float64) []byte {
	t.Helper()
	return buildRestorableScanPDFPages(t, jpeg, [][4]float64{{x, y, w, h}})
}

func buildRestorableScanPDFPages(t *testing.T, jpeg []byte, pages [][4]float64) []byte {
	t.Helper()
	pdf := gopdf2.GoPdf{}
	pdf.Start(gopdf2.Config{PageSize: *gopdf2.PageSizeA4})
	for _, page := range pages {
		holder, err := gopdf2.ImageHolderByBytes(jpeg)
		if err != nil {
			t.Fatal(err)
		}
		pdf.AddPage()
		if err := pdf.ImageByHolder(holder, page[0], page[1], &gopdf2.Rect{W: page[2], H: page[3]}); err != nil {
			t.Fatal(err)
		}
	}
	out, err := pdf.GetBytesPdfReturnErr()
	if err != nil {
		t.Fatal(err)
	}
	return out
}
