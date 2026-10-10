package docgen

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gopdf "github.com/VantageDataChat/GoPDF2"
)

func TestPaperImageMovesIntactWhenThePageIsShort(t *testing.T) {
	gen := New()
	if !gen.HasFont() {
		t.Skip("跳过：系统未找到中文字体")
	}
	layout, err := resolvePDFPageLayout("a4")
	if err != nil {
		t.Fatal(err)
	}
	spec := Spec{
		Title:      "论文解读",
		Subtitle:   "paper.pdf",
		Brand:      "MaClaw",
		Colorful:   true,
		PaperSize:  "A4",
		FooterHint: "由 MaClaw 伴读生成。网上核对的地址标有（可能），不是论文原文。",
	}
	pdf, err := gen.newMeasurementPDF(layout)
	if err != nil {
		t.Fatal(err)
	}
	endY, err := pdf.InsertHTMLBox(layout.marginX, layout.marginY, layout.contentW, 120, buildTitleHTML(spec), gopdf.HTMLBoxOption{
		DefaultFontFamily: "regular",
		DefaultFontSize:   11,
		BoldFontFamily:    "bold",
		LineSpacing:       2,
	})
	if err != nil {
		t.Fatal(err)
	}
	startY := endY + 20
	remain := layout.footerY - 4 - startY

	dir := filepath.Join(t.TempDir(), "fig.assets")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	imgPath := filepath.Join(dir, "p11-1.jpg")
	writeSolidJPEG(t, imgPath, 480, 640)
	imageLine := "![原文第11页](" + filepath.ToSlash(imgPath) + ")"

	sentence := "实验比较了不同目标在统一协议下的泄漏表现。\n\n"
	var body strings.Builder
	body.WriteString("## 方法原理\n\n")
	placed := false
	for i := 0; i < 80; i++ {
		trial := body.String() + "## 主要原理图及说明\n\n"
		height, err := gen.measureMarkdownHeight(layout, startY, strings.TrimSpace(trial), true)
		if err != nil {
			t.Fatal(err)
		}
		if height >= remain-160 && height <= remain-24 {
			body.WriteString("## 主要原理图及说明\n\n")
			body.WriteString(imageLine)
			body.WriteString("\n\n图中给出热力图。\n")
			placed = true
			break
		}
		if height > remain-24 {
			t.Fatalf("spacer overshot the page: height %.1f remain %.1f", height, remain)
		}
		body.WriteString(sentence)
	}
	if !placed {
		t.Fatal("spacer did not fill the page")
	}
	spec.Content = body.String()
	data, err := gen.Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	var raw strings.Builder
	for page := 0; page < 6; page++ {
		texts, err := gopdf.ExtractTextFromPage(data, page)
		if err != nil || len(texts) == 0 {
			break
		}
		for _, item := range texts {
			raw.WriteString(item.Text)
		}
	}
	text := raw.String()
	if !strings.Contains(string(data), "DCTDecode") {
		t.Fatalf("figure was dropped (missing=%v path=%v alt=%v)", strings.Contains(text, "图片未找到"), strings.Contains(text, "fig.assets"), strings.Contains(text, "原文第11页"))
	}
	if strings.Contains(text, "fig.assets") || strings.Contains(text, "p11-1") || strings.Contains(text, "图片未找到") {
		t.Fatalf("image path was written as text: %s", text)
	}
}

func TestTwoTallFiguresBothStayInThePDF(t *testing.T) {
	gen := New()
	if !gen.HasFont() {
		t.Skip("跳过：系统未找到中文字体")
	}
	dir := t.TempDir()
	var body strings.Builder
	body.WriteString("## 主要原理图及说明\n\n")
	// No caption. A caption adds just enough height that a measurement which
	// has already dropped the second chart can land past the footer and hide
	// the drop.
	for i := 1; i <= 2; i++ {
		path := filepath.Join(dir, "p"+string(rune('0'+i))+".jpg")
		writeSolidJPEG(t, path, 400, 900)
		body.WriteString("![](" + filepath.ToSlash(path) + ")\n\n")
	}
	data, err := gen.Generate(Spec{
		Title:     "论文解读",
		Brand:     "MaClaw",
		Colorful:  true,
		PaperSize: "A4",
		Content:   body.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "DCTDecode"); got < 2 {
		t.Fatalf("embedded figures = %d, want both", got)
	}
}

func writeSolidJPEG(t *testing.T, path string, width, height int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: 30, G: 90, B: 180, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}
