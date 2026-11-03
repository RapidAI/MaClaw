package guiapp

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	state.note(page, dir, dir, "sess")
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
	state.note(filepath.Join(t.TempDir(), "gone.html"), "", "", "")
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
	deck.note(filepath.Join(t.TempDir(), "gone.pptx"), "", "", "")
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
	note, images := renderPPTXVisualPreview(deck, "", 0)
	if len(images) != 3 {
		t.Fatalf("images = %d, want 3", len(images))
	}
	for _, image := range images {
		if image.Purpose != agent.VisualPreviewImagePurpose || image.Base64 == "" {
			t.Fatalf("image = %#v", image)
		}
	}
	if !strings.Contains(note, "第 1-3 页") || !strings.Contains(note, "start_line 设为 4") || !strings.Contains(note, "slide_offset 设为 3") || !strings.Contains(note, "max_slides 设为 3") {
		t.Fatalf("note = %q", note)
	}
	later, laterImages := renderPPTXVisualPreview(deck, "", 3)
	if len(laterImages) != 1 || !strings.Contains(later, "第 4-4 页") || !strings.Contains(later, "这是最后几页") {
		t.Fatalf("later slides = %d note = %q", len(laterImages), later)
	}
	path, offset := legacyVisualPreviewRequest("office", `{"action":"preview_pptx","file_path":"deck.pptx","slide_offset":6}`)
	if path != "deck.pptx" || offset != 6 {
		t.Fatalf("preview page = %q offset %d", path, offset)
	}
	if _, err := os.Stat(strings.TrimSuffix(deck, filepath.Ext(deck)) + "_preview"); err == nil {
		t.Fatal("slide preview was written beside the deck")
	}
	if images[0].Caption == "" || !strings.Contains(images[0].Caption, "第 1-3 页") {
		t.Fatalf("caption = %q", images[0].Caption)
	}
}

func TestLoadSlidePreviewsRejectsCorruptPNG(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "slide_001.png")
	if err := os.WriteFile(bad, []byte("CACHED"), 0o644); err != nil {
		t.Fatal(err)
	}
	if images, _, _ := loadSlidePreviews([]string{bad}); len(images) != 0 {
		t.Fatalf("corrupt slide was attached: %d", len(images))
	}
	good := filepath.Join(dir, "slide_004.png")
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(good, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	images, first, last := loadSlidePreviews([]string{good})
	if len(images) != 1 || first != 4 || last != 4 || images[0].Base64 == "" {
		t.Fatalf("images=%d first=%d last=%d", len(images), first, last)
	}
}

func TestSelectSlidePreviewPathsUsesRequestedPage(t *testing.T) {
	paths := []string{
		"deck_preview/slide_001.png",
		"deck_preview/slide_002.png",
		"deck_preview/slide_004.png",
		"deck_preview/slide_005.png",
		"deck_preview/slide_006.png",
		"deck_preview/slide_007.png",
	}
	got := selectSlidePreviewPaths(paths, 3, 3)
	if strings.Join(got, ",") != "deck_preview/slide_004.png,deck_preview/slide_005.png,deck_preview/slide_006.png" {
		t.Fatalf("window = %#v", got)
	}
	if gap := selectSlidePreviewPaths(paths, 0, 3); strings.Join(gap, ",") != "deck_preview/slide_001.png,deck_preview/slide_002.png" {
		t.Fatalf("gap window = %#v", gap)
	}
	if missed := selectSlidePreviewPaths(paths, 7, 3); missed != nil {
		t.Fatalf("missing start = %#v", missed)
	}
}

func TestVisualPreviewNoteKeepsJSON(t *testing.T) {
	other := `{"ok":true}`
	if got := appendVisualPreviewNote(other, "预览图已附上"); got != other {
		t.Fatalf("json result changed: %s", got)
	}
	preview := `{"images":["slide_001.png"],"slide_count":4,"next_offset":4}`
	if got := appendVisualPreviewNote(preview, "预览图已附上：第 1-3 页"); got != "预览图已附上：第 1-3 页" {
		t.Fatalf("preview json kept its own next page: %s", got)
	}
	if got := appendVisualPreviewNote("wrote", "预览图已附上"); !strings.Contains(got, "wrote") || !strings.Contains(got, "预览图已附上") {
		t.Fatalf("prose note = %q", got)
	}
}

func TestVisualPreviewPaneOnlyWhenTheWriteDidNotOpenIt(t *testing.T) {
	dir := t.TempDir()
	page := filepath.Join(dir, "index.html")
	sheet := filepath.Join(dir, "app.css")
	deck := filepath.Join(dir, "deck.pptx")
	if visualPreviewNeedsPane(page, page) {
		t.Fatal("html write would open the pane a second time")
	}
	if !visualPreviewNeedsPane(sheet, page) {
		t.Fatal("css write did not switch the pane to the page")
	}
	if !visualPreviewNeedsPane(deck, deck) {
		t.Fatal("pptx write did not open the pane")
	}
	if got := visualPreviewDisplayPath(filepath.Join(dir, "slides", "deck.pptx"), dir); got != "slides/deck.pptx" {
		t.Fatalf("display path = %q", got)
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

func TestDeletedHTMLIsNotReusedForCSS(t *testing.T) {
	dir := t.TempDir()
	page := filepath.Join(dir, "page.html")
	sheet := filepath.Join(dir, "app.css")
	if err := os.WriteFile(page, []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sheet, []byte("a{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	state := &visualPreviewState{}
	state.note(page, dir, dir, "sess")
	state.take()
	if err := os.Remove(page); err != nil {
		t.Fatal(err)
	}
	state.note(sheet, dir, dir, "sess")
	if state.hasPath() {
		t.Fatal("deleted html was selected for the stylesheet")
	}
}

func TestPreviewBrowserDeadKeepsUsableProcess(t *testing.T) {
	if previewBrowserDead(nil) {
		t.Fatal("nil error recycled the browser")
	}
	if !previewBrowserDead(previewTestError("打开预览页失败: cdp connection closed")) {
		t.Fatal("dead connection was kept")
	}
	if previewBrowserDead(previewTestError("预览页未在时限内加载完成")) || previewBrowserDead(previewTestError("预览截图为空")) {
		t.Fatal("a loaded browser was treated as dead")
	}
}

type previewTestError string

func (e previewTestError) Error() string { return string(e) }

func TestVisualPreviewPromptOnce(t *testing.T) {
	cb := &sharedAgentLoopCallbacks{}
	once := cb.systemPromptWithTaskAnchor("base")
	twice := cb.systemPromptWithTaskAnchor(once)
	if strings.Count(twice, visualPreviewPromptSentence) != 1 {
		t.Fatalf("prompt sentence count = %d", strings.Count(twice, visualPreviewPromptSentence))
	}
}

func TestPptxPreviewRevisionTracksRewrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deck.pptx")
	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	first := pptxPreviewRevision(path)
	if first == "" {
		t.Fatal("missing revision")
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	second := pptxPreviewRevision(path)
	if second == first {
		t.Fatal("rewrite kept the same revision")
	}
	if again := pptxPreviewRevision(path); again != second {
		t.Fatalf("revision changed without a rewrite: %s then %s", second, again)
	}
}

func TestVisualPreviewRevisionFollowsWrittenFile(t *testing.T) {
	dir := t.TempDir()
	page := filepath.Join(dir, "index.html")
	sheet := filepath.Join(dir, "app.css")
	if got := visualPreviewRevisionPath(sheet, page); got != sheet {
		t.Fatalf("stylesheet revision path = %s", got)
	}
	if got := visualPreviewRevisionPath(page, page); got != page {
		t.Fatalf("page revision path = %s", got)
	}
}
