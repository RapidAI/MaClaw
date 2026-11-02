package guiapp

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/pptx"
)

func TestClassifyVisualPreviewTarget(t *testing.T) {
	dir := t.TempDir()
	html := filepath.Join(dir, "index.html")
	css := filepath.Join(dir, "app.css")
	other := filepath.Join(dir, "other.css")
	deck := filepath.Join(dir, "deck.pptx")
	if err := os.WriteFile(html, []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(css, []byte("body{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("a{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	target, kind := classifyVisualPreviewTarget(css, "")
	if target != html || kind != "html" {
		t.Fatalf("css target = %s %s, want index.html", target, kind)
	}
	target, kind = classifyVisualPreviewTarget(deck, "")
	if target != deck || kind != "pptx" {
		t.Fatalf("pptx target = %s %s", target, kind)
	}
	target, kind = classifyVisualPreviewTarget(filepath.Join(dir, "main.go"), html)
	if target != "" || kind != "" {
		t.Fatalf("unrelated file was previewed: %s %s", target, kind)
	}
	alone := t.TempDir()
	sheet := filepath.Join(alone, "app.css")
	if err := os.WriteFile(sheet, []byte("a{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	page := filepath.Join(alone, "page.html")
	if err := os.WriteFile(page, []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	target, kind = classifyVisualPreviewTarget(sheet, page)
	if target != page || kind != "html" {
		t.Fatalf("css without index = %s %s, want last html", target, kind)
	}
}

func TestLegacyVisualPreviewPath(t *testing.T) {
	if got := legacyVisualPreviewPath("write_file", `{"path":"site/index.html"}`); got != "site/index.html" {
		t.Fatalf("write path = %q", got)
	}
	if got := legacyVisualPreviewPath("office", `{"action":"preview_pptx","file_path":"deck.pptx"}`); got != "deck.pptx" {
		t.Fatalf("preview path = %q", got)
	}
	if got := legacyVisualPreviewPath("office", `{"action":"read_pptx","file_path":"deck.pptx"}`); got != "" {
		t.Fatalf("read office was previewed: %q", got)
	}
}

func TestVisualPreviewWithoutVisionAttachesNoImage(t *testing.T) {
	dir := t.TempDir()
	page := filepath.Join(dir, "index.html")
	if err := os.WriteFile(page, []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	state := &visualPreviewState{}
	state.note(page, dir, "sess")
	got := state.finish(&App{}, false, agent.ToolExecutionResult{
		Result:  "wrote",
		Outcome: agent.ToolExecutionOutcomeOK,
	})
	if got.Outcome != agent.ToolExecutionOutcomeOK || len(got.ModelImages) != 0 {
		t.Fatalf("text model changed the write: %#v", got)
	}
	if !strings.Contains(got.Result, "当前模型看不到画面") || !strings.Contains(got.Result, "wrote") {
		t.Fatalf("note = %q", got.Result)
	}
}

func TestVisualPreviewRenderFailureKeepsWrite(t *testing.T) {
	state := &visualPreviewState{}
	state.note(filepath.Join(t.TempDir(), "gone.html"), "", "")
	got := state.finish(&App{}, true, agent.ToolExecutionResult{
		Result:  "wrote",
		Outcome: agent.ToolExecutionOutcomeOK,
	})
	if got.Outcome != agent.ToolExecutionOutcomeOK || len(got.ModelImages) != 0 {
		t.Fatalf("failed preview changed the write: %#v", got)
	}
	if !strings.Contains(got.Result, "没有预览图") || !strings.Contains(got.Result, "wrote") {
		t.Fatalf("note = %q", got.Result)
	}

	deck := &visualPreviewState{}
	deck.note(filepath.Join(t.TempDir(), "gone.pptx"), "", "")
	got = deck.finish(&App{}, true, agent.ToolExecutionResult{
		Result:  "Wrote presentation",
		Outcome: agent.ToolExecutionOutcomeOK,
	})
	if got.Outcome != agent.ToolExecutionOutcomeOK || len(got.ModelImages) != 0 {
		t.Fatalf("failed slide preview changed the write: %#v", got)
	}
	if !strings.Contains(got.Result, "没有预览图") {
		t.Fatalf("note = %q", got.Result)
	}
}

func TestPPTXVisualPreviewCapsAtThreeSlides(t *testing.T) {
	dir := t.TempDir()
	deck := filepath.Join(dir, "deck.pptx")
	if err := pptx.WriteFile(deck, pptx.Outline{
		Title: "预览",
		Slides: []pptx.OutlineSlide{
			{Title: "第一页", Bullets: []string{"甲"}},
			{Title: "第二页", Bullets: []string{"乙"}},
			{Title: "第三页", Bullets: []string{"丙"}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	note, images := renderPPTXVisualPreview(deck)
	if len(images) != 3 {
		t.Fatalf("images = %d, want 3", len(images))
	}
	for _, image := range images {
		if image.Purpose != agent.VisualPreviewImagePurpose || image.Base64 == "" {
			t.Fatalf("image = %#v", image)
		}
	}
	if !strings.Contains(note, "1-3") {
		t.Fatalf("note = %q", note)
	}
}

func TestHTMLVisualPreviewIncludesStylesheet(t *testing.T) {
	if os.Getenv("MACLAW_PREVIEW_BROWSER") == "" {
		t.Skip("set MACLAW_PREVIEW_BROWSER=1 to launch a private browser")
	}
	dir := t.TempDir()
	page := filepath.Join(dir, "index.html")
	sheet := filepath.Join(dir, "app.css")
	if err := os.WriteFile(page, []byte(`<!doctype html><html><head><link rel="stylesheet" href="app.css"></head><body></body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sheet, []byte(`html,body{margin:0;background:#ff0000;min-height:100vh}`), 0o644); err != nil {
		t.Fatal(err)
	}
	note, images, proc := renderHTMLVisualPreview(&App{}, nil, page)
	if proc != nil {
		defer proc.Close()
	}
	if len(images) != 1 || images[0].Purpose != agent.VisualPreviewImagePurpose {
		t.Fatalf("preview = %q images=%d", note, len(images))
	}
	if !previewPNGCenterRed(t, images[0].Base64) {
		t.Fatal("stylesheet did not reach the preview screenshot")
	}
}

func previewPNGCenterRed(t *testing.T, pngB64 string) bool {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(pngB64)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	bounds := img.Bounds()
	r, g, b, _ := img.At(bounds.Min.X+bounds.Dx()/2, bounds.Min.Y+bounds.Dy()/2).RGBA()
	return r>>8 > 200 && g>>8 < 40 && b>>8 < 40
}
