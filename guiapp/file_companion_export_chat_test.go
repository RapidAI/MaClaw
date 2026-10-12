package guiapp

import (
	"bytes"
	"context"
	"encoding/base64"
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
	if got := fileCompanionChatMarkdown([]CompanionChatTurn{{Role: "user", Content: "  "}, {Role: "assistant", Content: ""}}); got != "" {
		t.Fatalf("blank turns produced %q", got)
	}
	got := fileCompanionChatMarkdown([]CompanionChatTurn{
		{Role: "user", Content: "提问"},
		{Role: "assistant", Content: "   "},
		{Role: "assistant", Content: "回答"},
	})
	if strings.Count(got, "## ") != 2 {
		t.Fatalf("expected two headings in %q", got)
	}
	if !strings.Contains(got, "## 第 1 个问题") || !strings.Contains(got, "## 第 2 个回答") {
		t.Fatalf("turn labels lost their place numbering: %q", got)
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
