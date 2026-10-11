package guiapp

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// writeCompanionFigurePNG writes a real, decodable PNG above the minimum size
// so inventory, vision and display all treat it as a usable drawing.
func writeCompanionFigurePNG(t *testing.T, path string, width, height int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// Noise, not a gradient: a smooth gradient compresses far below the
	// minimum figure size and would be dropped as a decorative icon.
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			hash := (x*7919 + y*104729) % 1021
			canvas.Set(x, y, color.RGBA{R: uint8(hash % 251), G: uint8((hash * 7) % 251), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, canvas); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFileCompanionFiguresReadsMarkdownSourcesInOrder(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	dir := t.TempDir()
	writeCompanionFigurePNG(t, filepath.Join(dir, "images", "架构图.png"), 220, 160)
	writeCompanionFigurePNG(t, filepath.Join(dir, "flow.png"), 200, 140)
	writeCompanionFigurePNG(t, filepath.Join(dir, "tiny.png"), 4, 4)
	doc := filepath.Join(dir, "notes.md")
	body := strings.Join([]string{
		"# 说明",
		"",
		"先看整体：",
		"![架构图](images/%E6%9E%B6%E6%9E%84%E5%9B%BE.png)",
		"",
		"再看流程：",
		"![流程](<flow.png>)",
		"",
		"图标不是正文配图：",
		"![装饰](tiny.png)",
		"",
		"网图不在本地：",
		"![远程](https://example.com/a.png)",
		"",
		"缺一张：",
		"![丢失](missing.png)",
		"",
	}, "\n")
	if err := os.WriteFile(doc, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	canonical, err := fileCompanionCanonicalPath(doc)
	if err != nil {
		t.Fatal(err)
	}
	figures := app.fileCompanionFigures(canonical)
	if len(figures) != 2 {
		t.Fatalf("figures = %d (%v), want the two real drawings", len(figures), figures)
	}
	if filepath.Base(figures[0].path) != "架构图.png" {
		t.Fatalf("first figure = %s, want the percent-encoded target resolved first", figures[0].path)
	}
	if figures[0].caption != "架构图" {
		t.Fatalf("first caption = %q", figures[0].caption)
	}
	if filepath.Base(figures[1].path) != "flow.png" {
		t.Fatalf("second figure = %s, want the angle-bracket target resolved", figures[1].path)
	}
	// The second call must not rescan the file.
	app.fileCompanionFigures(canonical)
}

func TestFileCompanionFiguresReadsHTMLImageTags(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	dir := t.TempDir()
	writeCompanionFigurePNG(t, filepath.Join(dir, "shot.png"), 200, 120)
	writeCompanionFigurePNG(t, filepath.Join(dir, "plain.png"), 200, 120)
	doc := filepath.Join(dir, "page.html")
	body := `<html><body><p>说明</p><img alt="截图" src="shot.png"><img src="plain.png"><img src="data:image/png;base64,AAAA"></body></html>`
	if err := os.WriteFile(doc, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	canonical, err := fileCompanionCanonicalPath(doc)
	if err != nil {
		t.Fatal(err)
	}
	figures := app.fileCompanionFigures(canonical)
	if len(figures) != 2 {
		t.Fatalf("figures = %+v, want the two local img tags", figures)
	}
	if figures[0].caption != "截图" {
		t.Fatalf("caption = %q, want the alt attribute as the caption", figures[0].caption)
	}
	if figures[1].caption != "plain" {
		t.Fatalf("caption = %q, want the file name when no alt reads through", figures[1].caption)
	}
}

// A document is untrusted input. Its image tags may only reach drawings beside
// it, never an arbitrary path, because the inventory is what the vision request
// is built from.
func TestFileCompanionFiguresStayInsideTheDocumentDirectory(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	dir := t.TempDir()
	elsewhere := t.TempDir()
	writeCompanionFigurePNG(t, filepath.Join(dir, "sub", "p.png"), 200, 140)
	writeCompanionFigurePNG(t, filepath.Join(dir, "sub", "assets", "ok.png"), 200, 140)
	writeCompanionFigurePNG(t, filepath.Join(dir, "outside.png"), 200, 140)
	writeCompanionFigurePNG(t, filepath.Join(elsewhere, "leak.png"), 200, 140)
	doc := filepath.Join(dir, "sub", "notes.md")
	body := strings.Join([]string{
		"![自身目录](p.png)",
		"![子目录](assets/ok.png)",
		"![跳出文档目录](../outside.png)",
		"![绝对路径](<" + filepath.ToSlash(filepath.Join(elsewhere, "leak.png")) + ">)",
		"",
	}, "\n")
	if err := os.WriteFile(doc, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	canonical, err := fileCompanionCanonicalPath(doc)
	if err != nil {
		t.Fatal(err)
	}
	figures := app.fileCompanionFigures(canonical)
	if len(figures) != 2 {
		t.Fatalf("figures = %+v, want only the two drawings beside the document", figures)
	}
	if filepath.Base(figures[0].path) != "p.png" || filepath.Base(figures[1].path) != "ok.png" {
		t.Fatalf("figures = %+v, want the two in-document drawings", figures)
	}
}

// Opening a tab and serving a thumbnail never pay for a Word or PDF parse.
// The inventory for heavy formats is built on the first ordinary question;
// markdown and HTML are cheap enough to restore right away.
func TestFileCompanionOpenDoesNotParseHeavyDocumentsForFigures(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	dir := t.TempDir()
	heavy := filepath.Join(dir, "report.docx")
	writeCompanionTestDocx(t, heavy)
	if _, err := app.FileCompanionOpen(heavy); err != nil {
		t.Fatal(err)
	}
	heavyCanonical, err := fileCompanionCanonicalPath(heavy)
	if err != nil {
		t.Fatal(err)
	}
	st := app.companionRuntime()
	st.mu.Lock()
	_, extracted := st.figures[heavyCanonical]
	st.mu.Unlock()
	if extracted {
		t.Fatal("opening a Word file extracted its figures during the open")
	}
	// A thumbnail request may not become the parse either.
	if _, err := app.FileCompanionFigureDataURL(heavyCanonical); err == nil {
		t.Fatal("unlisted image served through the figure binding")
	}
	st.mu.Lock()
	_, extracted = st.figures[heavyCanonical]
	st.mu.Unlock()
	if extracted {
		t.Fatal("the figure binding parsed the Word file as a side effect")
	}

	// The cheap formats still restore figures for a reopened window.
	writeCompanionFigurePNG(t, filepath.Join(dir, "arch.png"), 220, 160)
	notes := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(notes, []byte("![整体架构](arch.png)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FileCompanionOpen(notes); err != nil {
		t.Fatal(err)
	}
	notesCanonical, err := fileCompanionCanonicalPath(notes)
	if err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	cached := st.figures[notesCanonical]
	st.mu.Unlock()
	if cached == nil || len(cached.figures) != 1 {
		t.Fatalf("markdown figures = %+v, want the drawing restored at open", cached)
	}
}

// An extension only claims to be a picture. A text file wearing .png must not
// become a vision attachment or a broken thumbnail.
func TestFileCompanionFiguresRejectFilesThatAreNotImages(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake.png")
	if err := os.WriteFile(fake, []byte(strings.Repeat("这不是图片，只是碰巧叫 .png 的文本。", 64)), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(doc, []byte("![假图](fake.png)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	canonical, err := fileCompanionCanonicalPath(doc)
	if err != nil {
		t.Fatal(err)
	}
	if figures := app.fileCompanionFigures(canonical); len(figures) != 0 {
		t.Fatalf("figures = %+v, want the mislabeled text file dropped", figures)
	}
}

func TestFileCompanionReplyCarriesTheFiguresTheAnswerMentions(t *testing.T) {
	figures := []companionFigure{
		{path: "/doc/figures/a.png", caption: "整体架构"},
		{path: "/doc/figures/b.png", caption: "训练流程"},
		{path: "/doc/figures/c.png", caption: "消融实验"},
	}

	got := fileCompanionFiguresForReply(figures, "这篇讲了什么", "方法如图2所示，训练阶段分两步。")
	if len(got) != 1 || got[0].Index != 2 || got[0].Label != "图2" {
		t.Fatalf("mentioned figures = %+v, want only 图2", got)
	}
	if got[0].Caption != "训练流程" || got[0].Path != "/doc/figures/b.png" {
		t.Fatalf("figure = %+v", got[0])
	}

	// Several references keep document order, and "图10" is not read as 图1.
	got = fileCompanionFiguresForReply(figures, "解释一下", "先看图3，再看图 1，最后回看图2。")
	if len(got) != 3 || got[0].Index != 1 || got[1].Index != 2 || got[2].Index != 3 {
		t.Fatalf("figures = %+v, want all three in order", got)
	}

	// A caption match counts even without a number.
	got = fileCompanionFiguresForReply(figures, "图表在哪", "这张训练流程说明了数据流向。")
	if len(got) != 1 || got[0].Index != 2 {
		t.Fatalf("caption match = %+v", got)
	}

	// An answer that talks about nothing visual stays text only.
	if got := fileCompanionFiguresForReply(figures, "翻译这段", "这是翻译后的句子。"); len(got) != 0 {
		t.Fatalf("unrelated reply carried figures: %+v", got)
	}

	// A whole-document question still gets the leading figures.
	got = fileCompanionFiguresForReply(figures, "总结一下这篇文档", "这篇文档介绍了三段内容。")
	if len(got) != 3 {
		t.Fatalf("summary fallback = %+v, want the leading figures", got)
	}
	if got := fileCompanionFiguresForReply(figures, "", "文中只有文字。"); len(got) != 0 {
		t.Fatalf("empty question carried figures: %+v", got)
	}
}

func TestFileCompanionFigureNoteNumbersFiguresInOrder(t *testing.T) {
	note := fileCompanionFigureNote([]companionFigure{
		{path: "/doc/a.png", caption: "整体架构"},
		{path: "/doc/b.png"},
	}, true)
	if !strings.Contains(note, "图1 整体架构") || !strings.Contains(note, "图2 b.png") {
		t.Fatalf("figure note = %q", note)
	}
	if !strings.Contains(note, "图N") {
		t.Fatalf("figure note does not tell the model how to refer to them: %q", note)
	}
	blind := fileCompanionFigureNote([]companionFigure{{path: "/doc/a.png", caption: "整体架构"}}, false)
	if !strings.Contains(blind, "不能看图") {
		t.Fatalf("non-vision note = %q, want the model told it cannot see the image", blind)
	}
}

// The figure note rides behind the file-path footer of the turn text. The
// visible user bubble must end at that footer, and a vision turn's stored
// multimodal content must never print base64 into a restored bubble.
func TestFileCompanionDisplayHidesFigureBookkeeping(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	_, canonical := companionFigureTestDocument(t, app)
	if err := app.ensureFileCompanionHandler("file-companion:visible", fileCompanionSessionID(canonical)); err != nil {
		t.Fatal(err)
	}
	turn := fileCompanionUserTurn(canonical, "总结一下", "")
	msg, _ := app.fileCompanionAttachFigures(IMUserMessage{Text: turn}, []companionFigure{
		{path: "/doc/figures/arch.png", caption: "整体架构", size: 4096},
	})
	if !strings.Contains(msg.Text, "原文中的图片") {
		t.Fatalf("turn text = %q, want the figure note appended", msg.Text)
	}
	if visible := fileCompanionVisibleUserText(msg.Text); visible != "总结一下" {
		t.Fatalf("visible user text = %q, want the note and footer stripped", visible)
	}

	// The same turn as the agent loop persists it: text block plus image blocks.
	stored := []interface{}{
		map[string]interface{}{"type": string(imContentBlockText), "text": msg.Text},
		map[string]interface{}{"type": string(imContentBlockImageURL), "image_url": map[string]interface{}{"url": "data:image/png;base64,AAAA"}},
	}
	out := fileCompanionDisplayMessages([]agent.ConversationEntry{
		{Role: "user", Content: stored},
		{Role: "assistant", Content: "这篇文档讲了数据流。"},
	}, []companionFigure{{path: "/doc/figures/arch.png", caption: "整体架构"}})
	if len(out) != 2 {
		t.Fatalf("display messages = %d", len(out))
	}
	if out[0].Text != "总结一下" {
		t.Fatalf("restored user bubble = %q, want the question only", out[0].Text)
	}
	if out[1].Figures == nil || len(out[1].Figures) != 1 || out[1].Figures[0].Label != "图1" {
		t.Fatalf("restored figures = %+v, want 图1 matched through the question", out[1].Figures)
	}
}

// docx with one PNG in word/media, referenced from the main document part.
func writeCompanionTestDocx(t *testing.T, path string) {
	t.Helper()
	var canvas bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 240, 180))
	for y := 0; y < 180; y++ {
		for x := 0; x < 240; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 200, A: 255})
		}
	}
	if err := png.Encode(&canvas, img); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	w := zip.NewWriter(file)
	write := func(name, body string) {
		part, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	write("[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`)
	write("_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`)
	write("word/_rels/document.xml.rels", `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/image1.png"/>
</Relationships>`)
	write("word/document.xml", `<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"
 xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"
 xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"
 xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
 <w:body><w:p><w:r><w:t>正文</w:t></w:r></w:p>
 <w:p><w:r><w:drawing><wp:inline><wp:docPr descr="整体架构"/><a:graphic><a:graphicData>
 <pic:pic xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:blipFill>
 <a:blip r:embed="rId1"/></pic:blipFill></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>
 </w:body></w:document>`)
	media, err := w.Create("word/media/image1.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := media.Write(canvas.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFileCompanionDocxFiguresAreExtractedOncePerDocument(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	dir := t.TempDir()
	doc := filepath.Join(dir, "report.docx")
	writeCompanionTestDocx(t, doc)
	canonical, err := fileCompanionCanonicalPath(doc)
	if err != nil {
		t.Fatal(err)
	}
	figures := app.fileCompanionFigures(canonical)
	if len(figures) != 1 {
		t.Fatalf("docx figures = %+v, want the embedded drawing", figures)
	}
	if _, statErr := os.Stat(figures[0].path); statErr != nil {
		t.Fatalf("cached figure missing: %v", statErr)
	}
	if figures[0].caption != "整体架构" {
		t.Fatalf("caption = %q, want the alt text from the document", figures[0].caption)
	}
}

func companionFigureTestDocument(t *testing.T, app *App) (path, canonical string) {
	t.Helper()
	dir := t.TempDir()
	writeCompanionFigurePNG(t, filepath.Join(dir, "figures", "arch.png"), 260, 180)
	writeCompanionFigurePNG(t, filepath.Join(dir, "figures", "flow.png"), 240, 160)
	path = filepath.Join(dir, "notes.md")
	body := "# 标题\n\n![整体架构](figures/arch.png)\n\n正文讲了数据流。\n\n![训练流程](figures/flow.png)\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	canonical, err := fileCompanionCanonicalPath(path)
	if err != nil {
		t.Fatal(err)
	}
	app.seedFileCompanionPaths(nil)
	if _, err := app.FileCompanionOpen(path); err != nil {
		t.Fatal(err)
	}
	return path, canonical
}

func TestFileCompanionTurnAttachsFiguresOnlyForVisionModels(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	_, canonical := companionFigureTestDocument(t, app)
	if err := app.ensureFileCompanionHandler("file-companion:test", fileCompanionSessionID(canonical)); err != nil {
		t.Fatal(err)
	}
	figures := app.fileCompanionFigures(canonical)
	if len(figures) != 2 {
		t.Fatalf("inventory = %+v, want two figures", figures)
	}

	// Without vision the turn only learns the figure list; no image bytes leave.
	msg, sent := app.fileCompanionAttachFigures(IMUserMessage{Text: "总结"}, figures)
	if len(sent) != 2 || len(msg.Attachments) != 0 {
		t.Fatalf("non-vision turn sent %d figures and %d attachments", len(sent), len(msg.Attachments))
	}
	if !strings.Contains(msg.Text, "图1 整体架构") || !strings.Contains(msg.Text, "图2 训练流程") {
		t.Fatalf("figure list missing from the turn: %s", msg.Text)
	}

	app.imHandler.standaloneConfig = &StandaloneConfig{
		LLMConfigFunc: func() corelib.MaclawLLMConfig { return corelib.MaclawLLMConfig{SupportsVision: true} },
	}
	msg, sent = app.fileCompanionAttachFigures(IMUserMessage{Text: "总结"}, figures)
	if len(sent) != 2 || len(msg.Attachments) != 2 {
		t.Fatalf("vision turn sent %d figures and %d attachments, want two of each", len(sent), len(msg.Attachments))
	}
	first := msg.Attachments[0]
	if first.Type != "image" || !strings.HasPrefix(first.MimeType, "image/") || first.Data == "" {
		t.Fatalf("attachment = %+v", first)
	}
	if first.FileName != "arch.png" {
		t.Fatalf("first attachment = %s, want figures in document order", first.FileName)
	}
}

func TestFileCompanionTurnSkipsFiguresForSelectionAndPaper(t *testing.T) {
	// A rewritten selection lands back in the file, and the paper
	// interpretation places its own drawings in the generated PDF.
	cases := []struct {
		name      string
		selection string
		paper     bool
		want      bool
	}{
		{"ordinary question", "", false, true},
		{"selection rewrite", "选中一段", false, false},
		{"blank selection", "   ", false, true},
		{"paper turn", "", true, false},
	}
	for _, item := range cases {
		if got := fileCompanionTurnUsesFigures(item.selection, item.paper, false); got != item.want {
			t.Fatalf("%s = %v, want %v", item.name, got, item.want)
		}
	}
	if fileCompanionTurnUsesFigures("", false, true) {
		t.Fatal("an image file already travels as its own attachment")
	}
}

func TestFileCompanionFigureDataURLServesOnlyListedImages(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	st := app.companionRuntime()
	st.mu.Lock()
	st.process = true
	st.mu.Unlock()
	dir := t.TempDir()
	pictured := filepath.Join(dir, "assets", "chart.png")
	writeCompanionFigurePNG(t, pictured, 300, 200)
	outsider := filepath.Join(dir, "secret.png")
	writeCompanionFigurePNG(t, outsider, 300, 200)
	doc := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(doc, []byte("![图表](assets/chart.png)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FileCompanionOpen(doc); err != nil {
		t.Fatal(err)
	}
	canonical, err := fileCompanionCanonicalPath(pictured)
	if err != nil {
		t.Fatal(err)
	}
	dataURL, err := app.FileCompanionFigureDataURL(canonical)
	if err != nil {
		t.Fatalf("listed figure: %v", err)
	}
	if !strings.HasPrefix(dataURL, "data:image/png;base64,") {
		t.Fatalf("data url = %.40s…, want png bytes", dataURL)
	}
	outside, err := fileCompanionCanonicalPath(outsider)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.FileCompanionFigureDataURL(outside); err == nil {
		t.Fatal("unrelated image served through the figure binding")
	}
	if _, err := app.FileCompanionFigureDataURL("no/such/image.png"); err == nil {
		t.Fatal("missing image served through the figure binding")
	}
}

func TestCompanionFigureImageDownscalesOversizedDrawings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.png")
	writeCompanionFigurePNG(t, path, 1800, 1200)
	// The inline limit is forced below this file's actual size; a 1800×1200
	// drawing is only tens of kilobytes and would otherwise pass through raw.
	const forcedLimit = 4096
	data, mime, err := companionFigureImage(path, fileCompanionFigureDisplayEdge, forcedLimit)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(data)) <= forcedLimit {
		t.Fatalf("oversized source was served raw (%d bytes)", len(data))
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("re-encoded figure is not a decodable image: %v", err)
	}
	if cfg.Width > fileCompanionFigureDisplayEdge || cfg.Height > fileCompanionFigureDisplayEdge {
		t.Fatalf("scaled size = %dx%d, want within %d", cfg.Width, cfg.Height, fileCompanionFigureDisplayEdge)
	}
	if format != "png" || mime != "image/png" {
		t.Fatalf("format = %s mime = %s", format, mime)
	}

	// A small drawing keeps its own bytes so the window shows the original.
	small := filepath.Join(dir, "small.png")
	writeCompanionFigurePNG(t, small, 120, 90)
	original, err := os.ReadFile(small)
	if err != nil {
		t.Fatal(err)
	}
	data, mime, err = companionFigureImage(small, fileCompanionFigureDisplayEdge, fileCompanionFigureInlineLimit)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, original) || mime != "image/png" {
		t.Fatalf("small image was re-encoded: %d bytes mime=%s", len(data), mime)
	}

	// A drawing too big for the inline limit but within the pixel edge comes
	// back untouched when re-encoding cannot shrink it: noise re-encodes to
	// the very same bytes, and the original must not lose quality for nothing.
	sameDims := filepath.Join(dir, "incompressible.png")
	writeCompanionFigurePNG(t, sameDims, 1600, 1600)
	original, err = os.ReadFile(sameDims)
	if err != nil {
		t.Fatal(err)
	}
	data, mime, err = companionFigureImage(sameDims, fileCompanionFigureDisplayEdge, fileCompanionFigureInlineLimit)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, original) {
		t.Fatalf("incompressible image was re-encoded for nothing: %d -> %d bytes", len(original), len(data))
	}
	if mime != "image/png" {
		t.Fatalf("mime = %s", mime)
	}

	// A tiny file claiming huge pixels is a decompression bomb, refused before
	// the pixels exist. A hand-built JPEG header is enough: the guard reads the
	// frame header, not the scan data.
	bomb := filepath.Join(dir, "bomb.jpg")
	header := []byte{
		0xFF, 0xD8, // SOI
		0xFF, 0xC0, 0x00, 0x0B, 0x08, // SOF0, length 11, 8-bit
		0x9C, 0x40, 0x9C, 0x40, // 40000 x 40000
		0x01, 0x01, 0x22, 0x00, // one component
		0xFF, 0xD9, // EOI
	}
	if err := os.WriteFile(bomb, header, 0o644); err != nil {
		t.Fatal(err)
	}
	// The forced limit pushes the tiny header past the raw passthrough, so the
	// pixel check is the thing under test.
	if _, _, err := companionFigureImage(bomb, fileCompanionFigureDisplayEdge, 8); err == nil {
		t.Fatal("decompression bomb was decoded")
	}

	// A large JPEG stays JPEG instead of being pushed through as PNG pixels.
	jpegPath := filepath.Join(dir, "wide.jpg")
	canvas := image.NewRGBA(image.Rect(0, 0, 2400, 1600))
	for y := 0; y < 1600; y++ {
		for x := 0; x < 2400; x++ {
			canvas.Set(x, y, color.RGBA{R: uint8(x >> 3), G: uint8(y >> 3), B: 90, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, canvas, &jpeg.Options{Quality: 92}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jpegPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	data, mime, err = companionFigureImage(jpegPath, fileCompanionFigureVisionEdge, forcedLimit)
	if err != nil {
		t.Fatal(err)
	}
	if mime != "image/jpeg" {
		t.Fatalf("mime = %s, want jpeg kept", mime)
	}
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err != nil || cfg.Width > fileCompanionFigureVisionEdge {
		t.Fatalf("vision image = %+v err=%v", cfg, err)
	}
}
