package docgen

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	gopdf "github.com/VantageDataChat/GoPDF2"
)

func tinyMermaidPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 40, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 40; x++ {
			img.Set(x, y, color.RGBA{R: 14, G: 42, B: 71, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExpandMermaidFencesReplacesTheDiagramAndLeavesOtherCode(t *testing.T) {
	var calls atomic.Int32
	SetMermaidRasterizer(func(source string) ([]byte, error) {
		calls.Add(1)
		if !strings.Contains(source, "ONLYSOURCE") {
			t.Errorf("drawer saw %q", source)
		}
		return tinyMermaidPNG(t), nil
	})
	t.Cleanup(func() { SetMermaidRasterizer(nil) })

	scratch := newDataURLScratch()
	t.Cleanup(scratch.cleanup)
	in := "前文。\n\n```mermaid\nflowchart LR\n  A[ONLYSOURCE] --> B\n```\n\n```python\nmermaid = 1\n```\n\n后文。\n"
	got := expandMermaidFences(in, scratch)
	if calls.Load() != 1 {
		t.Fatalf("drawer calls = %d, want 1", calls.Load())
	}
	if strings.Contains(got, "ONLYSOURCE") || strings.Contains(got, "flowchart") {
		t.Fatalf("diagram source remained: %s", got)
	}
	if !strings.Contains(got, "```python") || !strings.Contains(got, "mermaid = 1") {
		t.Fatalf("python fence was rewritten: %s", got)
	}
	if !strings.Contains(got, "![](") {
		t.Fatalf("diagram was not a picture: %s", got)
	}
	html := markdownToHTMLStyled(scratch, got, false, Style{})
	if !strings.Contains(html, "<img ") {
		t.Fatalf("picture was not embedded in HTML: %s", html)
	}
	if strings.Contains(html, "ONLYSOURCE") {
		t.Fatalf("source leaked into HTML: %s", html)
	}
}

func TestExpandMermaidFencesKeepsSourceWhenTheDrawerFails(t *testing.T) {
	SetMermaidRasterizer(func(string) ([]byte, error) {
		return nil, errors.New("parse")
	})
	t.Cleanup(func() { SetMermaidRasterizer(nil) })

	in := "```Mermaid title=x\nflowchart LR\n  A --> B\n```\n"
	got := expandMermaidFences(in, newDataURLScratch())
	if got != in {
		t.Fatalf("failed diagram was rewritten: %q", got)
	}
	html := markdownToHTML(got)
	if !strings.Contains(html, "flowchart") || !strings.Contains(html, "monospace") {
		t.Fatalf("source was not kept as code: %s", html)
	}
}

func TestExpandMermaidFencesLeavesAnUnclosedFence(t *testing.T) {
	var calls atomic.Int32
	SetMermaidRasterizer(func(string) ([]byte, error) {
		calls.Add(1)
		return tinyMermaidPNG(t), nil
	})
	t.Cleanup(func() { SetMermaidRasterizer(nil) })

	in := "开始\n\n```mermaid\nflowchart LR\n  A --> B\n"
	got := expandMermaidFences(in, newDataURLScratch())
	if got != in {
		t.Fatalf("unclosed fence changed: %q", got)
	}
	if calls.Load() != 0 {
		t.Fatalf("drawer was called for an unclosed fence: %d", calls.Load())
	}
}

func TestExpandMermaidFencesDrawsARepeatedDiagramOnce(t *testing.T) {
	var calls atomic.Int32
	SetMermaidRasterizer(func(source string) ([]byte, error) {
		calls.Add(1)
		if strings.Contains(source, "NO") {
			return nil, errors.New("parse")
		}
		return tinyMermaidPNG(t), nil
	})
	t.Cleanup(func() { SetMermaidRasterizer(nil) })

	scratch := newDataURLScratch()
	t.Cleanup(scratch.cleanup)
	in := "```mermaid\nflowchart LR\n  A-->B\n```\n\n```mermaid\nflowchart LR\n  A-->B\n```\n\n~~~mermaid\nflowchart TD\n  C-->D\n~~~\n\n```mermaid\nNO\n```\n\n```mermaid\nNO\n```\n"
	got := expandMermaidFences(in, scratch)
	if calls.Load() != 3 {
		t.Fatalf("drawer calls = %d, want 3 (one repeat, one other diagram, one failure)", calls.Load())
	}
	if strings.Count(got, "![](") != 3 {
		t.Fatalf("pictures = %d, want 3\n%s", strings.Count(got, "![]("), got)
	}
	if strings.Count(got, "NO") != 2 {
		t.Fatalf("failed diagram was not kept twice: %s", got)
	}
	first := strings.Index(got, "![](")
	second := strings.Index(got[first+1:], "![](")
	if first < 0 || second < 0 {
		t.Fatal(got)
	}
	end := func(at int) string {
		rest := got[at:]
		line := rest[:strings.IndexByte(rest, '\n')]
		return line
	}
	if end(first) != end(first+1+second) {
		t.Fatalf("repeated diagram did not share a picture: %s", got)
	}
}

func TestWritePNGRejectsAPNGWhoseChunksDoNotFinish(t *testing.T) {
	scratch := newDataURLScratch()
	t.Cleanup(scratch.cleanup)
	raw := append([]byte(nil), tinyMermaidPNG(t)...)
	if len(raw) < 32 {
		t.Fatal(len(raw))
	}
	// The header still parses. A checksum further in does not. Storing those
	// bytes would hand the PDF a file it cannot embed, and the whole document
	// would fail instead of keeping the diagram as source.
	raw[len(raw)-13] ^= 0xff
	if _, ok := scratch.writePNG(raw); ok {
		t.Fatal("a png with a broken chunk was stored")
	}
	if PNGBytesEmbeddable(raw) {
		t.Fatal("a png with a broken chunk was treated as embeddable")
	}
	if !PNGBytesEmbeddable(tinyMermaidPNG(t)) {
		t.Fatal("a complete png was not embeddable")
	}
	if PNGBytesEmbeddable([]byte("png")) {
		t.Fatal("text was treated as embeddable")
	}
}

func TestWritePNGKeepsAUsablePNGIntact(t *testing.T) {
	scratch := newDataURLScratch()
	t.Cleanup(scratch.cleanup)
	raw := tinyMermaidPNG(t)
	path, ok := scratch.writePNG(raw)
	if !ok {
		t.Fatal("png was refused")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatalf("png was re-encoded: %d bytes became %d", len(raw), len(got))
	}
	again, ok := scratch.writePNG(raw)
	if !ok || again != path {
		t.Fatalf("repeat write = %q ok=%v, want %q", again, ok, path)
	}
}

func TestExpandMermaidFencesWithoutADrawerKeepsTheSource(t *testing.T) {
	SetMermaidRasterizer(nil)
	in := "~~~mermaid\nflowchart LR\n  A --> B\n~~~\n"
	if got := expandMermaidFences(in, newDataURLScratch()); got != in {
		t.Fatalf("source changed without a drawer: %q", got)
	}
}

func TestGenerateDrawsMermaidOnceAndOmitsTheSource(t *testing.T) {
	gen := New()
	if !gen.HasFont() {
		t.Skip("跳过：系统未找到中文字体")
	}
	var calls atomic.Int32
	SetMermaidRasterizer(func(string) ([]byte, error) {
		calls.Add(1)
		return tinyMermaidPNG(t), nil
	})
	t.Cleanup(func() { SetMermaidRasterizer(nil) })

	pdf, err := gen.Generate(Spec{
		Title:     "流程图",
		Brand:     "MaClaw",
		PaperSize: "A4",
		Content:   "说明如下。\n\n```mermaid\nflowchart LR\n  A[ONLYSOURCE] --> B\n```\n\n图后说明。\n\n```python\nprint('keep')\n```\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("drawer calls = %d, want 1", calls.Load())
	}
	if !bytes.Contains(pdf, []byte("/Subtype /Image")) && !bytes.Contains(pdf, []byte("/Subtype/Image")) {
		t.Fatal("the diagram was not embedded")
	}
	text, err := gopdf.ExtractTextFromPage(pdf, 0)
	if err != nil {
		t.Fatal(err)
	}
	var page strings.Builder
	for _, item := range text {
		page.WriteString(item.Text)
	}
	got := page.String()
	if strings.Contains(got, "ONLYSOURCE") || strings.Contains(got, "flowchart") {
		t.Fatalf("diagram source was printed: %s", got)
	}
	if !strings.Contains(got, "图后说明") {
		t.Fatalf("body text missing: %s", got)
	}
}
