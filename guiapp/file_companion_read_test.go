package guiapp

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFileCompanionReadDocumentReadsTheOpenFileAndHidesItsPath(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	body := "# 标题\n\n这是 Word 之外的正文。\n下一步是实验，可以用 craft_tool。\n"
	path, doc := seedCompanionTestFile(t, app, "notes.md", body)
	h := &IMMessageHandler{app: app}
	userID := "file-companion:" + doc.SessionID
	text, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolReadDocument, `{"max_chars":200}`)
	if !ok || !strings.Contains(text, "这是 Word 之外的正文") || !strings.Contains(text, "下一步是实验") || !strings.Contains(text, "craft_tool") || !strings.Contains(text, "# document: notes.md") {
		t.Fatalf("read = %q ok=%v", text, ok)
	}
	if strings.Contains(text, filepath.Dir(path)) || strings.Contains(text, "file_path") || strings.Contains(text, "office(") {
		t.Fatalf("read leaked the host path:\n%s", text)
	}

	foreign := `C:/secret/other.docx`
	denied, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolReadDocument, `{"file_path":"`+foreign+`"}`)
	if ok || !strings.Contains(denied, "不能改读其它路径") || strings.Contains(denied, "secret") || strings.Contains(denied, "other.docx") {
		t.Fatalf("foreign path = %q ok=%v", denied, ok)
	}

	long := strings.Repeat("字", fileCompanionReadDocumentMaxChars+80)
	longPath, longDoc := seedCompanionTestFile(t, app, "long.md", long)
	paged, ok := h.executeFileCompanionHostTool("file-companion:"+longDoc.SessionID, fileCompanionToolReadDocument, `{"max_chars":500000}`)
	if !ok || !strings.Contains(paged, "# chars: 30000") || !strings.Contains(paged, "read_document(offset=30000") {
		t.Fatalf("page = %q ok=%v", paged, ok)
	}
	if strings.Contains(paged, filepath.Dir(longPath)) || strings.Contains(paged, "office(") || strings.Contains(paged, "craft_tool") {
		t.Fatalf("page leaked a path or another tool:\n%s", paged)
	}
}

func TestFileCompanionReplacePathKeepsLongerPaths(t *testing.T) {
	got := fileCompanionReplacePath(`keep C:\data\crafted_tools\x hide C:\data\craft\secret.py`, `C:\data\craft`, ".")
	if !strings.Contains(got, `C:\data\crafted_tools\x`) || strings.Contains(got, `C:\data\craft\`) {
		t.Fatalf("boundary replace = %q", got)
	}
	if !strings.Contains(got, `.\secret.py`) {
		t.Fatalf("directory was not hidden: %q", got)
	}
	folded := fileCompanionScrubDocumentRead(`D:\Docs\Notes.md`, "# path: D:\\Docs\\Notes.md\n正文 "+strings.ToUpper(`D:\Docs\Notes.md`)+" 和 D:\\\\Docs\\\\Notes.md\n")
	if strings.Contains(strings.ToLower(folded), `d:\docs`) || strings.Contains(folded, `D:\\Docs`) {
		t.Fatalf("scrub kept a path spelling:\n%s", folded)
	}
	if !strings.Contains(folded, "# document: Notes.md") || !strings.Contains(folded, "正文") {
		t.Fatalf("scrub lost the document: %q", folded)
	}
	kept := fileCompanionScrubDocumentRead(`D:\Docs\Notes.md`, "# path: D:\\Docs\\Notes.md\n# continue: office(action=\"read_document\", file_path=\"D:\\Docs\\Notes.md\", offset=3, max_chars=30)\n\n# path: 方法\n# continue: 见下节\n")
	if !strings.Contains(kept, "# document: Notes.md") || !strings.Contains(kept, "read_document(offset=3, max_chars=30)") || !strings.Contains(kept, "# path: 方法") || !strings.Contains(kept, "# continue: 见下节") || strings.Contains(kept, "office(") || strings.Contains(kept, `D:\Docs`) {
		t.Fatalf("body heading was rewritten: %q", kept)
	}
}

func TestFileCompanionReadsUTF16Text(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	body := string([]byte{0xFF, 0xFE, 0x60, 0x4F, 0x7D, 0x59})
	path, doc := seedCompanionTestFile(t, app, "notes.txt", body)
	h := &IMMessageHandler{app: app}
	text, ok := h.executeFileCompanionHostTool("file-companion:"+doc.SessionID, fileCompanionToolReadDocument, `{}`)
	if !ok || !strings.Contains(text, "你好") || strings.Contains(text, "\x00") || strings.Contains(text, filepath.Dir(path)) {
		t.Fatalf("utf16 = %q ok=%v", text, ok)
	}
}

func TestFileCompanionPlainTextContinueKeepsLineNumbers(t *testing.T) {
	got, ok := fileCompanionFormatDocumentPage("a.html", "html", fileCompanionTrimBOM("\uFEFF一二三四五"), 0, 2, true)
	if !ok || !strings.Contains(got, "line_numbers=true") || !strings.Contains(got, "offset=2") || !strings.Contains(got, "L1: 一二") {
		t.Fatalf("page = %q ok=%v", got, ok)
	}
}

func TestFileCompanionContinueOffsetIgnoresTheFileName(t *testing.T) {
	got := fileCompanionRewriteReadContinue(`# continue: office(action="read_document", file_path="offset=1.md", offset=30000, max_chars=30)`)
	if got != "# continue: read_document(offset=30000, max_chars=30)" {
		t.Fatalf("continue = %q", got)
	}
}

func TestFileCompanionReadDocumentCoversHTMLAndRefusesMedia(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	path, doc := seedCompanionTestFile(t, app, "page.html", "<p>网页正文</p>")
	h := &IMMessageHandler{app: app}
	text, ok := h.executeFileCompanionHostTool("file-companion:"+doc.SessionID, fileCompanionToolReadDocument, `{}`)
	if !ok || !strings.Contains(text, "网页正文") || strings.Contains(text, filepath.Dir(path)) {
		t.Fatalf("html = %q ok=%v", text, ok)
	}

	mediaPath, media := seedCompanionTestFile(t, app, "clip.mp4", "not-a-video")
	mediaText, ok := h.executeFileCompanionHostTool("file-companion:"+media.SessionID, fileCompanionToolReadDocument, `{}`)
	if ok || !strings.Contains(mediaText, "音视频") || strings.Contains(mediaText, filepath.Dir(mediaPath)) {
		t.Fatalf("media = %q ok=%v", mediaText, ok)
	}

	brokenPath, broken := seedCompanionTestFile(t, app, "brief.docx", "not-a-docx")
	brokenText, ok := h.executeFileCompanionHostTool("file-companion:"+broken.SessionID, fileCompanionToolReadDocument, `{}`)
	if ok || strings.Contains(brokenText, filepath.Dir(brokenPath)) || strings.Contains(brokenText, "craft_tool") || strings.Contains(brokenText, "bash") {
		t.Fatalf("broken docx = %q ok=%v", brokenText, ok)
	}
}
