package guiapp

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/docgen"
	gopdf "github.com/VantageDataChat/GoPDF2"
)

// newCompanionChatExportApp builds an app with one granted companion file and
// routes the save dialog to a fixed destination, so an export test asserts the
// PDF rather than whichever folder a desktop would have offered.
func newCompanionChatExportApp(t *testing.T, dest string) (*App, string) {
	t.Helper()
	app := &App{testHomeDir: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app.ctx = ctx
	app.seedFileCompanionPaths(nil)

	dir := t.TempDir()
	path := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(path, []byte("# alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FileCompanionOpen(path); err != nil {
		t.Fatal(err)
	}
	previous := fileCompanionSavePDFDialog
	t.Cleanup(func() { fileCompanionSavePDFDialog = previous })
	fileCompanionSavePDFDialog = func(_ *App, _ string) (string, error) { return dest, nil }
	return app, path
}

func TestFileCompanionExportChatPDFWritesEveryTurn(t *testing.T) {
	if !docgen.New().HasFont() {
		t.Skip("no usable font on this machine for PDF rendering")
	}
	out := filepath.Join(t.TempDir(), "chat")
	app, path := newCompanionChatExportApp(t, out)
	turns := []CompanionChatTurn{
		{Role: "user", Content: "这段讲了什么？"},
		{Role: "assistant", Content: "## 要点\n\n第一点最重要。\n\n- 甲\n- 乙\n"},
		{Role: "user", Content: "第二点呢？"},
		{Role: "assistant", Content: "第二点讲的是适用边界。\n"},
	}
	dest, err := app.FileCompanionExportChatPDF(path, turns)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if dest != out+".pdf" {
		t.Fatalf("dest %q", dest)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "%PDF") {
		t.Fatalf("not a PDF (%d bytes)", len(data))
	}
	var got strings.Builder
	for page := 0; page < 8; page++ {
		items, pageErr := gopdf.ExtractTextFromPage(data, page)
		if pageErr != nil || len(items) == 0 {
			break
		}
		for _, item := range items {
			got.WriteString(item.Text)
		}
	}
	text := strings.Join(strings.Fields(got.String()), "")
	// Both sides of the conversation have to be there. An answer-only export
	// would pass every check except these two.
	for _, want := range []string{"这段讲了什么", "第二点呢", "第一点最重要", "适用边界"} {
		if !strings.Contains(text, want) {
			t.Fatalf(" transcript is missing %q: %s", want, text)
		}
	}
}

// A figure attached to one reply must reach the transcript as pixels. This is
// the guard for the data-URL path end to end: every earlier failure mode here
// produced a valid PDF with no picture in it.
func TestFileCompanionExportChatPDFEmbedsFigures(t *testing.T) {
	if !docgen.New().HasFont() {
		t.Skip("no usable font on this machine for PDF rendering")
	}
	out := filepath.Join(t.TempDir(), "chat-figure")
	app, path := newCompanionChatExportApp(t, out)
	turns := []CompanionChatTurn{
		{Role: "user", Content: "展示图 8.1"},
		{Role: "assistant", Content: "图中给出结果：\n\n![](" + testFigureDataURL(t) + ")\n"},
	}
	dest, err := app.FileCompanionExportChatPDF(path, turns)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "DCTDecode") != 1 {
		t.Fatalf("figure was dropped from the transcript")
	}
}

// Math must be typeset, not echoed as source. A reader cannot tell a correctly
// rendered formula from a misparsed one by looking at the file size, so this
// asserts the rendered glyphs.
func TestFileCompanionExportChatPDFRendersMath(t *testing.T) {
	if !docgen.New().HasFont() {
		t.Skip("no usable font on this machine for PDF rendering")
	}
	out := filepath.Join(t.TempDir(), "chat-math")
	app, path := newCompanionChatExportApp(t, out)
	turns := []CompanionChatTurn{
		{Role: "assistant", Content: "行内 $\\alpha$ 与 $\\beta$ 均已证明。\n"},
	}
	dest, err := app.FileCompanionExportChatPDF(path, turns)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	items, err := gopdf.ExtractTextFromPage(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	for _, item := range items {
		got.WriteString(item.Text)
	}
	text := got.String()
	if strings.Contains(text, "\\alpha") || strings.Contains(text, "$") {
		t.Fatalf("formula source reached the page: %s", text)
	}
	if !strings.Contains(text, "α") || !strings.Contains(text, "β") {
		t.Fatalf("formula was not typeset: %s", text)
	}
}

func TestFileCompanionExportChatPDFRejectsEmptyAndUngrantedPaths(t *testing.T) {
	app, path := newCompanionChatExportApp(t, filepath.Join(t.TempDir(), "unused"))
	dir := filepath.Dir(path)

	// Nothing to write: turns that are all blank must not call the dialog at
	// all, let alone produce a file.
	if _, err := app.FileCompanionExportChatPDF(path, []CompanionChatTurn{{Role: "user", Content: "   "}, {Role: "assistant", Content: ""}}); err == nil || !strings.Contains(err.Error(), "没有可导出的内容") {
		t.Fatalf("blank transcript: %v", err)
	}
	if _, err := app.FileCompanionExportChatPDF(filepath.Join(dir, "other.md"), []CompanionChatTurn{{Role: "user", Content: "正文"}}); err == nil {
		t.Fatal("unopened file exported")
	}
	if _, err := (&App{}).FileCompanionExportChatPDF(path, []CompanionChatTurn{{Role: "user", Content: "正文"}}); err == nil {
		t.Fatal("app-less export succeeded")
	}
}

// A cancelled dialog is a decision, not a failure: no error, no file.
func TestFileCompanionExportChatPDFCancelledDialogWritesNothing(t *testing.T) {
	out := filepath.Join(t.TempDir(), "cancelled")
	app, path := newCompanionChatExportApp(t, out)
	fileCompanionSavePDFDialog = func(_ *App, _ string) (string, error) { return "", nil }
	dest, err := app.FileCompanionExportChatPDF(path, []CompanionChatTurn{{Role: "user", Content: "提问"}})
	if err != nil {
		t.Fatalf("cancel reported an error: %v", err)
	}
	if dest != "" {
		t.Fatalf("cancel returned %q", dest)
	}
	if _, statErr := os.Stat(out + ".pdf"); statErr == nil {
		t.Fatal("cancelled export wrote a file")
	}
}

// The open document must survive an export that names it as the destination.
func TestFileCompanionExportChatPDFRefusesToOverwriteTheOpenFile(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app.ctx = ctx
	app.seedFileCompanionPaths(nil)

	dir := t.TempDir()
	path := filepath.Join(dir, "notes.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	grantFileCompanionSourceForTest(t, app, path)
	previous := fileCompanionSavePDFDialog
	defer func() { fileCompanionSavePDFDialog = previous }()
	fileCompanionSavePDFDialog = func(_ *App, _ string) (string, error) { return path, nil }

	if _, err := app.FileCompanionExportChatPDF(path, []CompanionChatTurn{{Role: "user", Content: "提问"}}); err == nil || !strings.Contains(err.Error(), "不能覆盖") {
		t.Fatalf("overwrite guard: %v", err)
	}
	if got, readErr := os.ReadFile(path); readErr != nil || string(got) != "%PDF-1.4\n" {
		t.Fatalf("source changed: %q %v", string(got), readErr)
	}
}

func TestFileCompanionExportChatCloudUploadsTheTranscript(t *testing.T) {
	if !docgen.New().HasFont() {
		t.Skip("no usable font on this machine for PDF rendering")
	}
	dialogDest := filepath.Join(t.TempDir(), "unused")
	app, path := newCompanionChatExportApp(t, dialogDest)
	var gotName string
	var gotPDF []byte
	previous := fileCompanionUploadExportPDF
	t.Cleanup(func() { fileCompanionUploadExportPDF = previous })
	fileCompanionUploadExportPDF = func(_ *App, filename string, raw []byte) (*MobileDocumentDraftSummary, error) {
		gotName = filename
		gotPDF = append([]byte(nil), raw...)
		return &MobileDocumentDraftSummary{ID: "draft-1", Title: filename}, nil
	}
	summary, err := app.FileCompanionExportChatCloud(path, []CompanionChatTurn{
		{Role: "user", Content: "这段讲了什么？"},
		{Role: "assistant", Content: "第一点最重要。"},
	})
	if err != nil {
		t.Fatalf("cloud export: %v", err)
	}
	if summary == nil || summary.ID != "draft-1" {
		t.Fatalf("summary: %+v", summary)
	}
	if gotName != "notes-伴读对话.pdf" {
		t.Fatalf("cloud name: %q", gotName)
	}
	if !strings.HasPrefix(string(gotPDF), "%PDF") {
		t.Fatalf("cloud payload is not a PDF (%d bytes)", len(gotPDF))
	}
	// The dialog destination must stay untouched: cloud export does not ask
	// the reader where to save.
	if _, statErr := os.Stat(dialogDest + ".pdf"); statErr == nil {
		t.Fatal("cloud export wrote the dialog destination")
	}
}

func TestFileCompanionExportChatCloudWrapsATempDirFailure(t *testing.T) {
	app, path := newCompanionChatExportApp(t, filepath.Join(t.TempDir(), "unused"))
	previousDir := fileCompanionMkdirTemp
	t.Cleanup(func() { fileCompanionMkdirTemp = previousDir })
	fileCompanionMkdirTemp = func(string, string) (string, error) {
		return "", errors.New("mkdir temp: There is not enough space on the disk.")
	}
	called := false
	previousUpload := fileCompanionUploadExportPDF
	t.Cleanup(func() { fileCompanionUploadExportPDF = previousUpload })
	fileCompanionUploadExportPDF = func(*App, string, []byte) (*MobileDocumentDraftSummary, error) {
		called = true
		return &MobileDocumentDraftSummary{ID: "x"}, nil
	}
	_, err := app.FileCompanionExportChatCloud(path, []CompanionChatTurn{{Role: "user", Content: "提问"}})
	if err == nil || !strings.Contains(err.Error(), "创建目录失败") || !strings.Contains(err.Error(), "not enough space") {
		t.Fatalf("temp dir: %v", err)
	}
	if called {
		t.Fatal("upload ran without a file")
	}
}

func TestFileCompanionExportChatCloudSkipsABlankOrClosedFile(t *testing.T) {
	app, path := newCompanionChatExportApp(t, filepath.Join(t.TempDir(), "unused"))
	called := false
	previous := fileCompanionUploadExportPDF
	t.Cleanup(func() { fileCompanionUploadExportPDF = previous })
	fileCompanionUploadExportPDF = func(_ *App, _ string, _ []byte) (*MobileDocumentDraftSummary, error) {
		called = true
		return &MobileDocumentDraftSummary{ID: "draft-1"}, nil
	}
	if _, err := app.FileCompanionExportChatCloud(path, []CompanionChatTurn{{Role: "user", Content: "  "}}); err == nil || !strings.Contains(err.Error(), "没有可导出的内容") {
		t.Fatalf("blank transcript: %v", err)
	}
	if _, err := app.FileCompanionExportChatCloud(filepath.Join(filepath.Dir(path), "other.md"), []CompanionChatTurn{{Role: "user", Content: "正文"}}); err == nil {
		t.Fatal("unopened file uploaded")
	}
	if called {
		t.Fatal("upload ran without a transcript")
	}
}

func TestFileCompanionExportWebChatCloudRequiresTheOpenSession(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	called := false
	previous := fileCompanionUploadExportPDF
	t.Cleanup(func() { fileCompanionUploadExportPDF = previous })
	fileCompanionUploadExportPDF = func(_ *App, filename string, raw []byte) (*MobileDocumentDraftSummary, error) {
		called = true
		if filename != "对话.pdf" || !strings.HasPrefix(string(raw), "%PDF") {
			t.Fatalf("web upload %q (%d bytes)", filename, len(raw))
		}
		return &MobileDocumentDraftSummary{ID: "web-1", Title: filename}, nil
	}
	if _, err := app.FileCompanionExportWebChatCloud("missing", []CompanionChatTurn{{Role: "user", Content: "提问"}}); err == nil || !strings.Contains(err.Error(), "not open") {
		t.Fatalf("closed session: %v", err)
	}
	if called {
		t.Fatal("closed session uploaded")
	}
	if !docgen.New().HasFont() {
		t.Skip("no usable font on this machine for PDF rendering")
	}
	opened, err := app.FileCompanionOpenReference("https://example.com/watch")
	if err != nil {
		t.Fatal(err)
	}
	summary, err := app.FileCompanionExportWebChatCloud(opened.SessionID, []CompanionChatTurn{{Role: "user", Content: "网页里的问题"}})
	if err != nil {
		t.Fatalf("web cloud export: %v", err)
	}
	if summary == nil || summary.ID != "web-1" || !called {
		t.Fatalf("summary: %+v called=%v", summary, called)
	}
}

func TestFileCompanionUploadRenderedChatPDFKeepsTheNameInsideTheTempDir(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	called := false
	previous := fileCompanionUploadExportPDF
	t.Cleanup(func() { fileCompanionUploadExportPDF = previous })
	fileCompanionUploadExportPDF = func(_ *App, filename string, raw []byte) (*MobileDocumentDraftSummary, error) {
		called = true
		if filename != "secret.pdf" || !strings.HasPrefix(string(raw), "%PDF") {
			t.Fatalf("upload name %q (%d bytes)", filename, len(raw))
		}
		return &MobileDocumentDraftSummary{ID: "kept"}, nil
	}
	if _, err := app.fileCompanionUploadRenderedChatPDF("..", "标题", "副标题", "正文"); err == nil || !strings.Contains(err.Error(), "没有可导出的内容") {
		t.Fatalf("parent name: %v", err)
	}
	if called {
		t.Fatal("parent name uploaded")
	}
	if !docgen.New().HasFont() {
		t.Skip("no usable font on this machine for PDF rendering")
	}
	summary, err := app.fileCompanionUploadRenderedChatPDF(filepath.Join("..", "..", "secret.pdf"), "标题", "副标题", "正文")
	if err != nil {
		t.Fatalf("escaped name: %v", err)
	}
	if summary == nil || summary.ID != "kept" || !called {
		t.Fatalf("summary: %+v called=%v", summary, called)
	}
}

func TestFileCompanionChatFileNameKeepsADocumentName(t *testing.T) {
	if got := fileCompanionChatFileName("C:/notes/周报 .md"); got != "周报-伴读对话.pdf" {
		t.Fatalf("suffix: %q", got)
	}
	if got := fileCompanionChatFileName(""); got != "document-伴读对话.pdf" {
		t.Fatalf("empty source: %q", got)
	}
	long := fileCompanionChatFileName("C:/notes/" + strings.Repeat("名", 120) + ".md")
	if len([]rune(strings.TrimSuffix(long, fileCompanionChatNameSuffix))) > 60 {
		t.Fatalf("long name not capped: %q", long)
	}
}

// Blank turns would otherwise render as a heading with nothing under it.
func TestFileCompanionChatMarkdownSkipsBlankTurns(t *testing.T) {
	if got, count := fileCompanionChatDocument([]CompanionChatTurn{{Role: "user", Content: "  "}, {Role: "assistant", Content: ""}}); got != "" || count != 0 {
		t.Fatalf("blank turns produced %q (count %d)", got, count)
	}
	got, count := fileCompanionChatDocument([]CompanionChatTurn{
		{Role: "user", Content: "提问"},
		{Role: "assistant", Content: "   "},
		{Role: "assistant", Content: "回答"},
	})
	if strings.Count(got, "## ") != 2 {
		t.Fatalf("expected two headings in %q", got)
	}
	if count != 2 {
		t.Fatalf("counted %d turns, want the two that carry text", count)
	}
	// The answer is the first one, so it is 第 1 个回答. A single running
	// number over both roles would call it 第 2 个回答 and leave the reader
	// pairing 第 1 个问题 with nothing.
	if !strings.Contains(got, "## 第 1 个问题") || !strings.Contains(got, "## 第 1 个回答") {
		t.Fatalf("turn labels lost their place numbering: %q", got)
	}
}

// A transcript pairs each question with its own answer, so the two sides are
// numbered apart and a long session stays readable.
func TestFileCompanionChatDocumentNumbersEachSideApart(t *testing.T) {
	got, count := fileCompanionChatDocument([]CompanionChatTurn{
		{Role: "user", Content: "问题一"},
		{Role: "assistant", Content: "回答一"},
		{Role: "user", Content: "问题二"},
		{Role: "assistant", Content: "回答二"},
	})
	if count != 4 {
		t.Fatalf("count = %d, want 4", count)
	}
	for _, want := range []string{"## 第 1 个问题", "## 第 1 个回答", "## 第 2 个问题", "## 第 2 个回答"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	// Each label appears exactly once: a shared counter would produce a
	// duplicate instead of a pair.
	if strings.Count(got, "## 第 2 个回答") != 1 {
		t.Fatalf("answer numbering is not per side: %q", got)
	}
}

// testFigureDataURL builds a real JPEG at run time. A hand-written base64
// constant would be a second copy of the encoder's output to keep in step, and
// an invalid one fails as "图片未找到" — indistinguishable from the bug this
// test is meant to catch.
func testFigureDataURL(t *testing.T) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 240, 180))
	for y := 0; y < 180; y++ {
		for x := 0; x < 240; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 180, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// An answer's own headings have to sit inside the turn, not beside it. Both
// render at the same size otherwise, and the reader cannot tell where the
// answer starts.
func TestFileCompanionChatDocumentNestsTheAnswersOwnHeadings(t *testing.T) {
	got, _ := fileCompanionChatDocument([]CompanionChatTurn{
		{Role: "user", Content: "讲讲这个方法"},
		{Role: "assistant", Content: "## 方法原理\n\n正文。\n\n### 细节\n\n更多。\n"},
	})
	if !strings.Contains(got, "## 第 1 个回答") {
		t.Fatalf("turn heading lost its level: %q", got)
	}
	if !strings.Contains(got, "### 方法原理") {
		t.Fatalf("the answer's own heading was not nested: %q", got)
	}
	if !strings.Contains(got, "#### 细节") {
		t.Fatalf("the answer's deeper heading was not nested: %q", got)
	}
	// The original levels must not survive, or the collision is still there.
	if strings.Contains(got, "\n## 方法原理") {
		t.Fatalf("the answer kept a heading at the turn's level: %q", got)
	}
}

// An indented "##" is still a heading once the renderer trims it. Gluing the
// extra hash onto the indent promotes it to a level-one heading. The indent
// sits after a sentence so the document's own TrimSpace does not eat it.
func TestFileCompanionChatDocumentNestsAnIndentedHeading(t *testing.T) {
	got, _ := fileCompanionChatDocument([]CompanionChatTurn{
		{Role: "assistant", Content: "先说一句。\n  ## 缩进的小节\n\n正文。\n\t### 更深\n\u3000## 全角缩进\n"},
	})
	if !strings.Contains(got, "  ### 缩进的小节") {
		t.Fatalf("indented heading was not nested: %q", got)
	}
	if !strings.Contains(got, "\t#### 更深") {
		t.Fatalf("tab-indented heading was not nested: %q", got)
	}
	if !strings.Contains(got, "\u3000### 全角缩进") {
		t.Fatalf("fullwidth-indented heading was not nested: %q", got)
	}
	if strings.Contains(got, "#  ##") || strings.Contains(got, "# \t###") || strings.Contains(got, "#  ###") || strings.Contains(got, "# \u3000##") {
		t.Fatalf("indented heading was promoted: %q", got)
	}
}

// Turns are one document. An answer that opens a fence and never closes it
// would otherwise keep the next turn's heading inside that code block.
func TestFileCompanionChatDocumentClosesAFenceBeforeTheNextTurn(t *testing.T) {
	got, count := fileCompanionChatDocument([]CompanionChatTurn{
		{Role: "assistant", Content: "```python\nprint(1)\n## 仍是代码\n"},
		{Role: "user", Content: "然后呢？"},
	})
	if count != 2 {
		t.Fatalf("turns: %d", count)
	}
	if strings.Contains(got, "### 仍是代码") {
		t.Fatalf("a heading inside an open fence was nested: %q", got)
	}
	question := strings.Index(got, "## 第 1 个问题")
	if question < 0 {
		t.Fatalf("next turn heading missing: %q", got)
	}
	before := got[:question]
	open := strings.Index(before, "```python")
	close := strings.LastIndex(before, "```")
	if open < 0 || close <= open {
		t.Fatalf("fence did not close before the next turn: %q", got)
	}
}

// A ``` between $$ and $$ is formula text. It must not open a fence, or the
// heading after the formula stays at the turn's size and a closer written
// for that fence starts a real code block in the PDF.
func TestFileCompanionChatDocumentIgnoresAFenceInsideDisplayMath(t *testing.T) {
	got, _ := fileCompanionChatDocument([]CompanionChatTurn{
		{Role: "assistant", Content: "$$\n```\nE = mc^2\n$$\n\n## 小节\n"},
		{Role: "user", Content: "下一问"},
	})
	if !strings.Contains(got, "### 小节") {
		t.Fatalf("heading after math was not nested: %q", got)
	}
	if strings.Count(got, "```") != 1 {
		t.Fatalf("a fence marker inside math grew a closer: %q", got)
	}
	if !strings.Contains(got, "## 第 1 个问题") {
		t.Fatalf("next turn heading missing: %q", got)
	}
	plain, _ := fileCompanionChatDocument([]CompanionChatTurn{
		{Role: "assistant", Content: "$$E = mc^2$$\n\n## 小节\n"},
	})
	if !strings.Contains(plain, "### 小节") {
		t.Fatalf("heading after a one-line formula was not nested: %q", plain)
	}
}

// An unclosed $$ covers only its own line. A fence after it is still a fence.
func TestFileCompanionChatDocumentUnclosedMathDoesNotHideAFence(t *testing.T) {
	got, _ := fileCompanionChatDocument([]CompanionChatTurn{
		{Role: "assistant", Content: "$$\n```python\nprint(1)\n## 仍是代码\n"},
		{Role: "user", Content: "然后呢？"},
	})
	if strings.Contains(got, "### 仍是代码") {
		t.Fatalf("a heading inside the fence was nested: %q", got)
	}
	question := strings.Index(got, "## 第 1 个问题")
	if question < 0 {
		t.Fatalf("next turn heading missing: %q", got)
	}
	if strings.Count(got[:question], "```") < 2 {
		t.Fatalf("fence after an unclosed formula was not closed: %q", got)
	}
}

func TestFileCompanionChatDocumentDoesNotCloseAFinishedFenceTwice(t *testing.T) {
	got, _ := fileCompanionChatDocument([]CompanionChatTurn{
		{Role: "assistant", Content: "```\n# 注释\n```\n\n## 小节\n"},
	})
	if strings.Count(got, "```") != 2 {
		t.Fatalf("fence markers: %q", got)
	}
	if !strings.Contains(got, "### 小节") {
		t.Fatalf("heading after a closed fence was not nested: %q", got)
	}
}

// A "#" inside a code fence is content, and a "#" that does not begin a heading
// must not be touched either.
func TestFileCompanionChatDocumentLeavesNonHeadingsAlone(t *testing.T) {
	got, _ := fileCompanionChatDocument([]CompanionChatTurn{
		{Role: "assistant", Content: "```shell\n# 这是注释\n## 也是注释\n```\n\n#标签 不是标题\n\n##### 已经很深\n"},
	})
	if !strings.Contains(got, "# 这是注释") || !strings.Contains(got, "## 也是注释") {
		t.Fatalf("a comment inside a fence was rewritten: %q", got)
	}
	if !strings.Contains(got, "#标签 不是标题") {
		t.Fatalf("a hash without a space was treated as a heading: %q", got)
	}
	// Level five is as deep as the renderer goes, so it stays put rather than
	// becoming an unrecognised six.
	if !strings.Contains(got, "##### 已经很深") || strings.Contains(got, "######") {
		t.Fatalf("a deep heading was pushed past the renderer's range: %q", got)
	}
}

// A level-one heading is already larger than the turn heading, so it stays.
// Demoting it would put the answer's own top level beside the turn's, which is
// the collision this whole step exists to avoid.
func TestFileCompanionChatDocumentLeavesATopLevelHeadingAlone(t *testing.T) {
	got, _ := fileCompanionChatDocument([]CompanionChatTurn{
		{Role: "assistant", Content: "# 我的分析\n\n正文。\n"},
	})
	if !strings.Contains(got, "# 我的分析") {
		t.Fatalf("a level-one heading was demoted: %q", got)
	}
	if strings.Contains(got, "## 我的分析") {
		t.Fatalf("a level-one heading landed on the turn heading's level: %q", got)
	}
}
