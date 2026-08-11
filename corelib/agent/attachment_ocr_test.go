package agent

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

func testImageAttachment() MessageAttachment {
	return MessageAttachment{
		Type:     "image",
		FileName: "shot.png",
		MimeType: "image/png",
		Data:     base64.StdEncoding.EncodeToString([]byte("fake-png")),
	}
}

func TestBuildUserContent_NoVisionWithOCRAppendsText(t *testing.T) {
	maclawpath.SetBaseDir(t.TempDir())
	ocr := func(_, _ string) (string, error) { return "识别出的文字", nil }
	got := BuildUserContent("看看这张图", []MessageAttachment{testImageAttachment()}, "openai", false, ocr, nil)
	text, ok := got.(string)
	if !ok {
		t.Fatalf("content type = %T, want string for non-vision model", got)
	}
	if !strings.Contains(text, "当前模型不支持图片理解") {
		t.Fatalf("missing save-to-local note: %q", text)
	}
	if !strings.Contains(text, "识别出的文字") {
		t.Fatalf("missing OCR text: %q", text)
	}
	if !strings.Contains(text, "本地 OCR 识别") {
		t.Fatalf("missing OCR section header: %q", text)
	}
}

func TestBuildUserContent_NoVisionWithoutOCRKeepsOldBehavior(t *testing.T) {
	maclawpath.SetBaseDir(t.TempDir())
	got := BuildUserContent("看看这张图", []MessageAttachment{testImageAttachment()}, "openai", false, nil, nil)
	text, ok := got.(string)
	if !ok {
		t.Fatalf("content type = %T, want string", got)
	}
	if !strings.Contains(text, "当前模型不支持图片理解") {
		t.Fatalf("missing save-to-local note: %q", text)
	}
	if strings.Contains(text, "本地 OCR 识别") {
		t.Fatalf("unexpected OCR section without recognizer: %q", text)
	}
}

func TestBuildUserContent_VisionIgnoresOCR(t *testing.T) {
	called := false
	ocr := func(_, _ string) (string, error) { called = true; return "x", nil }
	got := BuildUserContent("看看这张图", []MessageAttachment{testImageAttachment()}, "openai", true, ocr, nil)
	blocks, ok := got.([]interface{})
	if !ok {
		t.Fatalf("content type = %T, want multimodal blocks for vision model", got)
	}
	if called {
		t.Fatal("OCR recognizer invoked for a vision-capable model")
	}
	foundImage := false
	for _, b := range blocks {
		if bm, ok := b.(map[string]interface{}); ok && bm["type"] == "image_url" {
			foundImage = true
		}
	}
	if !foundImage {
		t.Fatalf("no image_url block in %+v", blocks)
	}
}

func TestBuildUserContentRoutesMislabelledAndUnnamedOfficeAttachmentsAwayFromVision(t *testing.T) {
	base := t.TempDir()
	maclawpath.SetBaseDir(base)
	for _, tc := range []struct {
		name     string
		fileName string
		mimeType string
		wantExt  string
	}{
		{"doc", "report.doc", "image/png", ".doc"},
		{"docx conflicting filename", "cover.png", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", ".docx"},
		{"docx", "", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", ".docx"},
		{"ppt", "", "application/vnd.ms-powerpoint", ".ppt"},
		{"pptx", "", "application/vnd.openxmlformats-officedocument.presentationml.presentation", ".pptx"},
		{"xls", "", "application/vnd.ms-excel", ".xls"},
		{"xlsx", "", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", ".xlsx"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const encoded = "bm90LWEtcmVhbC1vZmZpY2UtZG9jdW1lbnQ="
			got := BuildUserContent("inspect", []MessageAttachment{{
				Type: "image", FileName: tc.fileName, MimeType: tc.mimeType, Data: encoded,
			}}, "openai", true, nil, nil)
			text, ok := got.(string)
			if !ok {
				t.Fatalf("binary document entered vision content: %#v", got)
			}
			if strings.Contains(text, encoded) || strings.Contains(text, "data:image/") {
				t.Fatalf("attachment payload leaked into context: %q", text)
			}
			if !strings.Contains(text, "[附件:") || !strings.Contains(text, tc.wantExt) {
				t.Fatalf("missing staged document identity %q: %q", tc.wantExt, text)
			}
			entries, err := filepath.Glob(filepath.Join(corelib.MaclawDataDir(), "im_files", "*"+tc.wantExt))
			if err != nil || len(entries) == 0 {
				t.Fatalf("staged %s files = %v, err=%v", tc.wantExt, entries, err)
			}
			foundCurrent := false
			for _, entry := range entries {
				if strings.Contains(text, entry) {
					foundCurrent = true
					break
				}
			}
			if !foundCurrent {
				t.Fatalf("current staged %s path missing from attachment text %q; entries=%v", tc.wantExt, text, entries)
			}
		})
	}
}

func TestNormalizeBinaryDocumentAttachmentFilename(t *testing.T) {
	for _, tc := range []struct {
		name     string
		fileName string
		mimeType string
		want     string
	}{
		{"existing office suffix wins", "quarterly.doc", "image/png", "quarterly.doc"},
		{"mime replaces unrelated suffix", "quarterly.png", "application/vnd.ms-excel", "quarterly.xls"},
		{"mime fills missing suffix", "quarterly", "application/vnd.ms-powerpoint", "quarterly.ppt"},
		{"unnamed stays empty for generated fallback", "", "application/msword", ""},
		{"ordinary file unchanged", "quarterly.png", "image/png", "quarterly.png"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeBinaryDocumentAttachmentFilename(tc.fileName, tc.mimeType); got != tc.want {
				t.Fatalf("NormalizeBinaryDocumentAttachmentFilename(%q, %q) = %q, want %q", tc.fileName, tc.mimeType, got, tc.want)
			}
		})
	}
}

func TestBuildUserContentRoutesConflictingOfficeAttachmentIntoRealAutoExtract(t *testing.T) {
	base := t.TempDir()
	maclawpath.SetBaseDir(base)
	clearOfficeReadEnvironment(t)

	fixture := filepath.Join(t.TempDir(), "source.docx")
	const want = "attachment OfficeRead end-to-end body"
	writeMinimalDOCX(t, fixture, want)
	raw, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}

	got := BuildUserContent("inspect", []MessageAttachment{{
		Type:     "image",
		FileName: "cover.png",
		MimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		Data:     base64.StdEncoding.EncodeToString(raw),
	}}, "openai", true, nil, nil)
	text, ok := got.(string)
	if !ok {
		t.Fatalf("Office attachment entered vision content: %#v", got)
	}
	if !strings.Contains(text, "cover.docx") || !strings.Contains(text, AutoExtractBeginMarker) {
		t.Fatalf("missing normalized Office attachment route: %q", text)
	}
	if !strings.Contains(text, want) {
		t.Fatalf("auto-extract missing OfficeRead text %q: %q", want, text)
	}
}

func TestBuildUserContentStagesAndAutoExtractsAllOfficeFormats(t *testing.T) {
	base := t.TempDir()
	maclawpath.SetBaseDir(base)
	clearOfficeReadEnvironment(t)

	restore := stubOfficeReadExtract(t, func(path string) (string, string, error) {
		format := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
		if !isOfficeReadFormat(format) {
			t.Fatalf("OfficeRead received unexpected staged path %q", path)
		}
		return "attachment " + format + " extracted body", format, nil
	})
	defer restore()

	for _, tc := range []struct {
		format string
		mime   string
	}{
		{format: "doc", mime: "application/msword"},
		{format: "docx", mime: "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
		{format: "ppt", mime: "application/vnd.ms-powerpoint"},
		{format: "pptx", mime: "application/vnd.openxmlformats-officedocument.presentationml.presentation"},
		{format: "xls", mime: "application/vnd.ms-excel"},
		{format: "xlsx", mime: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
	} {
		t.Run(tc.format, func(t *testing.T) {
			fixture := filepath.Join(t.TempDir(), "source."+tc.format)
			writeValidOfficeDefaultRouteFixture(t, fixture, tc.format)
			raw, err := os.ReadFile(fixture)
			if err != nil {
				t.Fatal(err)
			}

			got := BuildUserContent("inspect", []MessageAttachment{{
				Type:     "image",
				FileName: "mislabelled.png",
				MimeType: tc.mime,
				Data:     base64.StdEncoding.EncodeToString(raw),
			}}, "openai", true, nil, nil)
			text, ok := got.(string)
			if !ok {
				t.Fatalf("%s attachment entered vision content: %#v", tc.format, got)
			}
			if !strings.Contains(text, "mislabelled."+tc.format) || !strings.Contains(text, AutoExtractBeginMarker) {
				t.Fatalf("%s attachment was not staged for automatic extraction: %q", tc.format, text)
			}
			if !strings.Contains(text, "attachment "+tc.format+" extracted body") || !strings.Contains(text, `format="`+tc.format+`"`) {
				t.Fatalf("%s attachment did not use the shared OfficeRead extraction route: %q", tc.format, text)
			}
		})
	}
}

func TestBuildUserContent_OCRFailureKeepsDescription(t *testing.T) {
	maclawpath.SetBaseDir(t.TempDir())
	ocr := func(_, _ string) (string, error) { return "", errors.New("ocr boom") }
	got := BuildUserContent("", []MessageAttachment{testImageAttachment()}, "openai", false, ocr, nil)
	text, ok := got.(string)
	if !ok {
		t.Fatalf("content type = %T, want string", got)
	}
	if !strings.Contains(text, "当前模型不支持图片理解") {
		t.Fatalf("missing save-to-local note: %q", text)
	}
	if strings.Contains(text, "本地 OCR 识别") {
		t.Fatalf("OCR section present despite failure: %q", text)
	}
}

func TestStripImageOCRNotes(t *testing.T) {
	att := testImageAttachment()
	ocr := func(_, _ string) (string, error) { return "第一行\n第二行\n第三行", nil }
	note := RecognizeImageTextNote(ocr, &att, "shot.png")
	if note == "" {
		t.Fatal("RecognizeImageTextNote returned empty note")
	}
	text := "用户问题\n[用户发送了图片 shot.png，已保存到 /tmp/x，当前模型不支持图片理解]" + note + "\n后续描述"

	stripped := StripImageOCRNotes(text)
	if strings.Contains(stripped, "第一行") || strings.Contains(stripped, "第三行") {
		t.Fatalf("OCR body survived stripping: %q", stripped)
	}
	if !strings.Contains(stripped, "历史消息，正文已省略") {
		t.Fatalf("missing placeholder: %q", stripped)
	}
	for _, want := range []string{"用户问题", "当前模型不支持图片理解", "后续描述", "本地 OCR 识别"} {
		if !strings.Contains(stripped, want) {
			t.Fatalf("stripped text lost %q: %q", want, stripped)
		}
	}
	if strings.Contains(stripped, "image_ocr: begin") || strings.Contains(stripped, "image_ocr: end") {
		t.Fatalf("markers leaked into stripped text: %q", stripped)
	}

	// No note → unchanged.
	plain := "没有注记的普通文本"
	if got := StripImageOCRNotes(plain); got != plain {
		t.Fatalf("StripImageOCRNotes modified plain text: %q", got)
	}
}

func TestAnnotateHistoryAttachmentTextStripsOCRNote(t *testing.T) {
	att := testImageAttachment()
	ocr := func(_, _ string) (string, error) { return "一大段识别文字", nil }
	note := RecognizeImageTextNote(ocr, &att, "shot.png")
	text := "[用户发送了图片 shot.png，已保存到 /tmp/x，当前模型不支持图片理解]" + note

	got := AnnotateHistoryAttachmentText(text)
	if strings.Contains(got, "一大段识别文字") {
		t.Fatalf("history still carries OCR body: %q", got)
	}
	if !strings.Contains(got, "[之前发送的图片") {
		t.Fatalf("history marker not renamed: %q", got)
	}
	if !strings.Contains(got, "正文已省略") {
		t.Fatalf("missing OCR placeholder: %q", got)
	}
}
