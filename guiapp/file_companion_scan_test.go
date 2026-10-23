package guiapp

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func withCompanionScanHooks(t *testing.T, limit int64, recognize func([]byte, int) (string, error), vision func() bool) {
	t.Helper()
	prevLimit := fileCompanionOfficeInjectLimit
	prevRecognize := fileCompanionRecognizeScanPage
	prevVision := fileCompanionModelSeesImages
	fileCompanionOfficeInjectLimit = limit
	fileCompanionRecognizeScanPage = recognize
	fileCompanionModelSeesImages = vision
	t.Cleanup(func() {
		fileCompanionOfficeInjectLimit = prevLimit
		fileCompanionRecognizeScanPage = prevRecognize
		fileCompanionModelSeesImages = prevVision
	})
}

func TestFileCompanionPrepareScannedPDFInjectsOCRWindow(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	var calls []int
	var progress []string
	withCompanionScanHooks(t, 1, func(_ []byte, page int) (string, error) {
		calls = append(calls, page)
		return fmt.Sprintf("页%d正文", page), nil
	}, nil)
	jpegBytes := mustJPEG(t, 16, 16)
	page := imagePDFPage{jpeg: jpegBytes, width: 16, height: 16}
	pages := make([]imagePDFPage, 9)
	for i := range pages {
		pages[i] = page
	}
	path := filepath.Join(t.TempDir(), "book.pdf")
	if err := os.WriteFile(path, buildImagePDF(t, pages...), 0o644); err != nil {
		t.Fatal(err)
	}
	msg := IMUserMessage{Text: "解读\n\n" + agent.FilePathPromptPrefix + "\n" + path}
	out := app.fileCompanionPrepareScannedPDF(msg, path, "解读", func(s string) { progress = append(progress, s) })
	if len(calls) != 8 || calls[0] != 1 || calls[7] != 8 {
		t.Fatalf("ocr calls = %v", calls)
	}
	if len(progress) != 1 || progress[0] != fileCompanionScanProgress {
		t.Fatalf("progress = %v", progress)
	}
	for _, want := range []string{"页1正文", "页8正文", agent.AutoExtractBeginMarker, "PP-OCRv6"} {
		if !strings.Contains(out.Text, want) {
			t.Fatalf("missing %q:\n%s", want, out.Text)
		}
	}
	if strings.Contains(out.Text, "页9正文") || strings.Contains(out.Text, "ocr_recognize") || strings.Contains(out.Text, agent.AutoExtractNotice) || strings.Contains(out.Text, "truncated=true") {
		t.Fatalf("companion window drifted:\n%s", out.Text)
	}
	if got := agent.ExpandUserSelectedFilePathsWithContext(out.Text, 0); got != out.Text {
		t.Fatal("shared expander rewrote the companion window")
	}
	calls = nil
	progress = nil
	again := app.fileCompanionPrepareScannedPDF(IMUserMessage{Text: "继续"}, path, "继续", func(s string) { progress = append(progress, s) })
	if len(calls) != 1 || calls[0] != 9 {
		t.Fatalf("continue calls = %v", calls)
	}
	if strings.Contains(again.Text, "页1正文") || !strings.Contains(again.Text, "页9正文") {
		t.Fatalf("continue window:\n%s", again.Text)
	}
}

func TestFileCompanionPrepareScannedPDFKeepsNativeText(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	calls := 0
	withCompanionScanHooks(t, 1, func([]byte, int) (string, error) {
		calls++
		return "不应识别", nil
	}, nil)
	path := filepath.Join(t.TempDir(), "paper.pdf")
	if err := os.WriteFile(path, buildImagePDF(t, imagePDFPage{text: "Alpha Bravo Charlie Delta"}), 0o644); err != nil {
		t.Fatal(err)
	}
	out := app.fileCompanionPrepareScannedPDF(IMUserMessage{Text: "读"}, path, "读", nil)
	if calls != 0 || !strings.Contains(out.Text, "Alpha Bravo Charlie Delta") || !strings.Contains(out.Text, agent.AutoExtractBeginMarker) {
		t.Fatalf("calls=%d text=%s", calls, out.Text)
	}
	if strings.Contains(out.Text, "ocr_recognize") || strings.Contains(out.Text, "不应识别") {
		t.Fatalf("native page was sent to OCR:\n%s", out.Text)
	}
}

func TestFileCompanionPrepareScannedPDFLeavesSmallFiles(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	calls := 0
	withCompanionScanHooks(t, 0, func([]byte, int) (string, error) {
		calls++
		return "不应识别", nil
	}, nil)
	path := filepath.Join(t.TempDir(), "small.pdf")
	jpegBytes := mustJPEG(t, 16, 16)
	if err := os.WriteFile(path, buildImagePDF(t, imagePDFPage{jpeg: jpegBytes, width: 16, height: 16}), 0o644); err != nil {
		t.Fatal(err)
	}
	msg := IMUserMessage{Text: "读"}
	out := app.fileCompanionPrepareScannedPDF(msg, path, "读", nil)
	if calls != 0 || out.Text != msg.Text || len(out.Attachments) != 0 {
		t.Fatalf("small pdf changed: calls=%d text=%q attachments=%d", calls, out.Text, len(out.Attachments))
	}
}

func TestFileCompanionPrepareScannedPDFAttachesPageImage(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	withCompanionScanHooks(t, 1, func([]byte, int) (string, error) {
		return "公式正文", nil
	}, func() bool { return true })
	jpegBytes := mustJPEG(t, 16, 16)
	path := filepath.Join(t.TempDir(), "scan.pdf")
	if err := os.WriteFile(path, buildImagePDF(t, imagePDFPage{jpeg: jpegBytes, width: 16, height: 16}), 0o644); err != nil {
		t.Fatal(err)
	}
	out := app.fileCompanionPrepareScannedPDF(IMUserMessage{Text: "读"}, path, "读", nil)
	if len(out.Attachments) != 1 {
		t.Fatalf("attachments=%d text=%s", len(out.Attachments), out.Text)
	}
	att := out.Attachments[0]
	if att.Type != "image" || att.MimeType != "image/jpeg" || !strings.HasSuffix(att.FileName, "-page-1.jpg") {
		t.Fatalf("attachment = %+v", att.FileName)
	}
	raw, err := base64.StdEncoding.DecodeString(att.Data)
	if err != nil || !bytes.Equal(raw, jpegBytes) {
		t.Fatalf("page image mismatch err=%v", err)
	}
	if !strings.Contains(out.Text, "公式正文") || strings.Contains(out.Text, "ocr_recognize") {
		t.Fatalf("vision window:\n%s", out.Text)
	}
}

func TestFileCompanionScanReadPagesUsesBuiltinOCR(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	calls := 0
	withCompanionScanHooks(t, 1, func([]byte, int) (string, error) {
		calls++
		return "公式正文", nil
	}, nil)
	path := filepath.Join(t.TempDir(), "scan.pdf")
	jpegBytes := mustJPEG(t, 16, 16)
	if err := os.WriteFile(path, buildImagePDF(t, imagePDFPage{jpeg: jpegBytes, width: 16, height: 16}), 0o644); err != nil {
		t.Fatal(err)
	}
	text, ok := app.fileCompanionToolReadPages(path, map[string]interface{}{"pages": "1"})
	if !ok || !strings.Contains(text, "公式正文") || !strings.Contains(text, "PP-OCRv6") || strings.Contains(text, "没有可提取的文字") {
		t.Fatalf("read = %q ok=%v", text, ok)
	}
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
	text, ok = app.fileCompanionToolReadPages(path, map[string]interface{}{"pages": "1"})
	if !ok || !strings.Contains(text, "公式正文") || calls != 1 {
		t.Fatalf("cached read = %q calls=%d ok=%v", text, calls, ok)
	}
}

func TestFileCompanionScanPrompts(t *testing.T) {
	ordinary := fileCompanionPrompt("a.pdf", ".pdf")
	if !strings.Contains(ordinary, "PP-OCRv6") || strings.Contains(ordinary, "不要使用工具") || strings.Contains(ordinary, "ocr_recognize") {
		t.Fatalf("ordinary prompt = %q", ordinary)
	}
	paper := fileCompanionPaperPrompt("论文.pdf")
	if !strings.Contains(paper, "扫描页识别正文") || strings.Contains(paper, "PDF 版本") || strings.Contains(paper, "不要声称已经生成") {
		t.Fatalf("paper prompt = %q", paper)
	}
}
