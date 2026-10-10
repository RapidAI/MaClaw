package guiapp

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gopdf2 "github.com/VantageDataChat/GoPDF2"
)

func TestFileCompanionPaperChoosesBodyFiguresFirst(t *testing.T) {
	got := paperChooseFigurePages([]paperFigurePage{
		{page: 30, appendix: true},
		{page: 14},
		{page: 11},
		{page: 45, appendix: true},
		{page: 2},
	})
	want := []int{2, 11, 14, 30, 45}
	if len(got) != len(want) {
		t.Fatalf("pages = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("pages = %v", got)
		}
	}
	ordered := paperChooseFigurePages([]paperFigurePage{
		{page: 30, appendix: true},
		{page: 14},
		{page: 11},
		{page: 45, appendix: true},
	})
	body := []int{11, 14, 30, 45}
	if len(ordered) != len(body) {
		t.Fatalf("pages = %v", ordered)
	}
	for i := range body {
		if ordered[i] != body[i] {
			t.Fatalf("pages = %v", ordered)
		}
	}
}

func TestFileCompanionPaperCaptionLines(t *testing.T) {
	cases := []struct {
		line     string
		ok       bool
		appendix bool
	}{
		{"Figure 1: Leakage regimes", true, false},
		{"Figure C.1: BrowserUse", true, true},
		{"图 2：流程", true, false},
		{"图 H.1 附录", true, true},
		{"图片说明", false, false},
		{"  Fig. 3 something", true, false},
	}
	for _, item := range cases {
		appendix, ok := paperFigureCaptionHit(item.line)
		if ok != item.ok || appendix != item.appendix {
			t.Fatalf("%q ok=%v appendix=%v", item.line, ok, appendix)
		}
	}
}

func TestInsertPaperFigureLinesUnderNumberedHeading(t *testing.T) {
	markdown := "## 方法原理\n公式。\n\n## 3. 主要原理图及说明\n图 1：热力图。\n\n## 实验方法\n对比。\n"
	out := insertPaperFigureLines(markdown, []string{"![原文第11页](figures/p11-1.png)"})
	fig := strings.Index(out, "![原文第11页]")
	note := strings.Index(out, "图 1：热力图")
	method := strings.Index(out, "## 实验方法")
	if fig < 0 || note < 0 || method < 0 || fig > note || note > method {
		t.Fatalf("placement:\n%s", out)
	}
	if !strings.Contains(out, "![原文第11页](figures/p11-1.png)\n\n图 1：热力图") {
		t.Fatalf("image is glued to the caption:\n%s", out)
	}
	if strings.Count(out, "原理图") != 1 {
		t.Fatalf("heading duplicated:\n%s", out)
	}
	if !strings.Contains(out, "公式。") {
		t.Fatalf("other section moved:\n%s", out)
	}
}

func TestFileCompanionPaperRasterizesCaptionPages(t *testing.T) {
	font := `C:\Windows\Fonts\arial.ttf`
	if _, err := os.Stat(font); err != nil {
		t.Skip("no arial")
	}
	engine := paperRasterEngines()
	if engine.python == "" && engine.cairo == "" && engine.ppm == "" {
		t.Skip("no pdf renderer")
	}
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "paper.pdf")
	writePaperPDFWithCaption(t, pdfPath, font)
	data, err := os.ReadFile(pdfPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(paperChooseFigurePages(paperFigureCaptionPages(data))) == 0 {
		t.Skip("caption text was not extracted from the generated pdf")
	}
	markdown := "## 主要原理图及说明\n文字说明。\n\n## 实验方法\n对比。\n"
	out := fileCompanionEmbedPaperFigures(pdfPath, dir, markdown, "fc_raster")
	if strings.Count(out, "![原文第1页](") != 1 || !strings.Contains(out, ".png") {
		t.Fatalf("rasterized figure:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "figures", "p1-1.png")); err != nil {
		t.Fatal(err)
	}
	methodAt := strings.Index(out, "## 实验方法")
	figureAt := strings.Index(out, "![原文第1页](")
	noteAt := strings.Index(out, "文字说明。")
	if figureAt < 0 || methodAt < 0 || noteAt < 0 || figureAt > noteAt || noteAt > methodAt {
		t.Fatalf("figure placement:\n%s", out)
	}
}

func TestFileCompanionPaperEmbedsExtractedFigures(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "paper.pdf")
	writePaperPDFWithFigures(t, pdfPath)
	markdown := "## 主要原理图及说明\n文字说明模块和数据流。\n\n## 实验方法\n对比基线。\n"
	out := fileCompanionEmbedPaperFigures(pdfPath, dir, markdown, "fc_fig")
	if !strings.Contains(out, "文字说明模块和数据流。") || !strings.Contains(out, "## 实验方法") {
		t.Fatalf("section text moved: %s", out)
	}
	figureAt := strings.Index(out, "![原文第1页](")
	noteAt := strings.Index(out, "文字说明模块和数据流。")
	methodAt := strings.Index(out, "## 实验方法")
	if figureAt < 0 || noteAt < 0 || methodAt < 0 || figureAt > noteAt || noteAt > methodAt {
		t.Fatalf("figure was not placed under the heading:\n%s", out)
	}
	if strings.Count(out, "![原文第") != 1 {
		t.Fatalf("figure count:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "figures", "p1-1.jpg")); err != nil {
		t.Fatal(err)
	}
	plain := fileCompanionEmbedPaperFigures(filepath.Join(dir, "notes.md"), dir, markdown, "fc_fig")
	if plain != markdown {
		t.Fatalf("non-pdf changed: %s", plain)
	}
}

func writePaperPDFWithFigures(t *testing.T, path string) {
	t.Helper()
	large, err := gopdf2.ImageHolderByBytes(paperTestJPEG(t, 220, 180))
	if err != nil {
		t.Fatal(err)
	}
	small, err := gopdf2.ImageHolderByBytes(paperTestJPEG(t, 32, 32))
	if err != nil {
		t.Fatal(err)
	}
	pdf := &gopdf2.GoPdf{}
	pdf.Start(gopdf2.Config{PageSize: *gopdf2.PageSizeA4})
	pdf.AddPage()
	if err := pdf.ImageByHolder(small, 40, 700, &gopdf2.Rect{W: 24, H: 24}); err != nil {
		t.Fatal(err)
	}
	if err := pdf.ImageByHolder(large, 40, 200, &gopdf2.Rect{W: 220, H: 180}); err != nil {
		t.Fatal(err)
	}
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
}

func writePaperPDFWithCaption(t *testing.T, path, font string) {
	t.Helper()
	large, err := gopdf2.ImageHolderByBytes(paperTestJPEG(t, 220, 180))
	if err != nil {
		t.Fatal(err)
	}
	pdf := &gopdf2.GoPdf{}
	pdf.Start(gopdf2.Config{PageSize: *gopdf2.PageSizeA4})
	pdf.AddPage()
	if err := pdf.AddTTFFont("arial", font); err != nil {
		t.Fatal(err)
	}
	if err := pdf.SetFont("arial", "", 14); err != nil {
		t.Fatal(err)
	}
	pdf.SetXY(48, 48)
	if err := pdf.Cell(&gopdf2.Rect{W: 460, H: 24}, "Figure 1: demo chart"); err != nil {
		t.Fatal(err)
	}
	if err := pdf.ImageByHolder(large, 48, 120, &gopdf2.Rect{W: 220, H: 180}); err != nil {
		t.Fatal(err)
	}
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
}

func paperTestJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 255), G: uint8(y % 255), B: 160, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	if encoded.Len() < paperFigureMinBytes {
		t.Fatalf("jpeg too small: %d", encoded.Len())
	}
	return encoded.Bytes()
}
