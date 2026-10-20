package agent

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestStripAttachedImageInstructionsKeepsTheTask(t *testing.T) {
	raw := "把配图版讲稿做完\n\n" + AutoExtractNotice + "\n\n" + imageOnlyPDFVisionLead + "\n\n## Page 1\n" + pdfVisionPageMarker
	got := StripAttachedImageInstructions(raw)
	if !strings.Contains(got, "把配图版讲稿做完") || !strings.Contains(got, pdfVisionPageMarker) || strings.Contains(got, AutoExtractNotice) {
		t.Fatalf("stripped = %q", got)
	}
	plain := "摘要\n\n" + AutoExtractNotice + "\n\n正文"
	if !strings.Contains(StripAttachedImageInstructions(plain), AutoExtractNotice) {
		t.Fatal("a text extract lost the auto-extract notice")
	}
	for _, lead := range []string{
		imageOnlyPDFVisionLead,
		imageOnlyPDFVisionLeadTruncated,
		imageOnlyPDFVisionLeadMixed,
		imageOnlyPDFVisionLeadUnresolved,
	} {
		if strings.Contains(StripAttachedImageInstructions("任务\n"+lead), lead) {
			t.Fatalf("lead remained: %s", lead)
		}
	}
	if StripAttachedImageInstructions("  "+imageOnlyPDFVisionLead+"  ") != "" {
		t.Fatal("a message that is only the attached-image instruction was kept")
	}
	withHint := "把配图版讲稿做完\n\n" + AttachedImageReadHint
	if got := StripAttachedImageInstructions(withHint); !strings.Contains(got, "把配图版讲稿做完") || strings.Contains(got, "Analyze attached images first") {
		t.Fatalf("image hint = %q", got)
	}
}

func TestExtractPDFTextOCRsEmbeddedPageImage(t *testing.T) {
	restoreOfficeImagePDFOCR(t)
	setImageOnlyPDFOCRPageLimitForTest(t, 16)
	var calls int
	SetOfficeImagePDFOCR(nil, func(imageBase64 string) (string, error) {
		calls++
		raw, err := base64.StdEncoding.DecodeString(imageBase64)
		if err != nil {
			t.Errorf("decode payload: %v", err)
		}
		if _, _, err := image.Decode(bytes.NewReader(raw)); err != nil {
			t.Errorf("payload is not an image: %v", err)
		}
		return "面向社会认知智能的中文大模型对话研究", nil
	})

	path := filepath.Join(t.TempDir(), "diagram.pdf")
	jpegBytes := mustJPEG(t, 32, 32)
	if err := os.WriteFile(path, buildImagePDF(t, imagePDFPage{jpeg: jpegBytes, width: 32, height: 32}, imagePDFPage{}), 0o600); err != nil {
		t.Fatal(err)
	}
	text, err := ExtractPDFText(path)
	if err != nil {
		t.Fatalf("ExtractPDFText: %v", err)
	}
	if calls != 1 {
		t.Fatalf("OCR calls = %d, want 1 (blank page must be skipped)", calls)
	}
	if !strings.Contains(text, "# text_source: ocr") {
		t.Fatalf("missing OCR source marker:\n%s", text)
	}
	if !strings.Contains(text, "## Page 1\n面向社会认知智能的中文大模型对话研究") {
		t.Fatalf("page 1 text missing:\n%s", text)
	}
	if !strings.Contains(text, "## Page 2\n"+pdfEmptyPageMarker) {
		t.Fatalf("blank page marker missing:\n%s", text)
	}
}

func TestExtractPDFTextWithoutOCRKeepsScanError(t *testing.T) {
	restoreOfficeImagePDFOCR(t)
	SetOfficeImagePDFOCR(nil, nil)
	path := filepath.Join(t.TempDir(), "diagram.pdf")
	jpegBytes := mustJPEG(t, 32, 32)
	if err := os.WriteFile(path, buildImagePDF(t, imagePDFPage{jpeg: jpegBytes, width: 32, height: 32}), 0o600); err != nil {
		t.Fatal(err)
	}
	text, err := ExtractPDFText(path)
	if text != "" || err == nil || !strings.Contains(err.Error(), "扫描件") {
		t.Fatalf("ExtractPDFText = %q, %v; want scan error", text, err)
	}
}

func TestExtractPDFTextDoesNotOCRNativeText(t *testing.T) {
	restoreOfficeImagePDFOCR(t)
	calls := 0
	SetOfficeImagePDFOCR(nil, func(string) (string, error) {
		calls++
		return "should-not-run", nil
	})
	path := filepath.Join(t.TempDir(), "paper.pdf")
	if err := os.WriteFile(path, buildImagePDF(t, imagePDFPage{text: "Alpha Bravo Charlie Delta"}), 0o600); err != nil {
		t.Fatal(err)
	}
	text, err := ExtractPDFText(path)
	if err != nil {
		t.Fatalf("ExtractPDFText: %v", err)
	}
	if calls != 0 {
		t.Fatalf("OCR calls = %d, want 0 for a text page", calls)
	}
	if strings.Contains(text, "should-not-run") || strings.Contains(text, "# text_source: ocr") {
		t.Fatalf("native text was replaced by OCR:\n%s", text)
	}
	if !strings.Contains(text, "Alpha") {
		t.Fatalf("native text missing:\n%s", text)
	}
}

func TestExtractPDFTextOCRPageLimitLeavesLaterImages(t *testing.T) {
	restoreOfficeImagePDFOCR(t)
	setImageOnlyPDFOCRPageLimitForTest(t, 1)
	calls := 0
	SetOfficeImagePDFOCR(nil, func(string) (string, error) {
		calls++
		return "first figure", nil
	})
	jpegBytes := mustJPEG(t, 32, 32)
	path := filepath.Join(t.TempDir(), "two.pdf")
	if err := os.WriteFile(path, buildImagePDF(t,
		imagePDFPage{jpeg: jpegBytes, width: 32, height: 32},
		imagePDFPage{jpeg: jpegBytes, width: 32, height: 32},
	), 0o600); err != nil {
		t.Fatal(err)
	}
	text, err := ExtractPDFText(path)
	if err != nil {
		t.Fatalf("ExtractPDFText: %v", err)
	}
	if calls != 1 {
		t.Fatalf("OCR calls = %d, want 1", calls)
	}
	if !strings.Contains(text, "first figure") || !strings.Contains(text, pdfOCRSkippedPageMarker) || !strings.Contains(text, "ocr_truncated: true") {
		t.Fatalf("limit markers missing:\n%s", text)
	}
}

func TestExtractPDFTextVisionSkipsOCRAndKeepsBlankPage(t *testing.T) {
	restoreOfficeImagePDFOCR(t)
	setImageOnlyPDFOCRPageLimitForTest(t, 16)
	calls := 0
	SetOfficeImagePDFOCR(nil, func(string) (string, error) {
		calls++
		return "ocr-only", nil
	})
	path := filepath.Join(t.TempDir(), "diagram.pdf")
	jpegBytes := mustJPEG(t, 32, 32)
	if err := os.WriteFile(path, buildImagePDF(t, imagePDFPage{jpeg: jpegBytes, width: 32, height: 32}, imagePDFPage{}), 0o600); err != nil {
		t.Fatal(err)
	}
	text, err := extractPDFText(path, officeReadSettings{imagePDFPreferVision: true})
	if err != nil {
		t.Fatalf("extractPDFText: %v", err)
	}
	if calls != 0 {
		t.Fatalf("OCR calls = %d, want 0 when the model can see images", calls)
	}
	if !strings.Contains(text, "# text_source: vision") || !strings.Contains(text, imageOnlyPDFVisionLead) || !strings.Contains(text, "## Page 1\n"+pdfVisionPageMarker) || !strings.Contains(text, "## Page 2\n"+pdfEmptyPageMarker) {
		t.Fatalf("vision transcript missing:\n%s", text)
	}
	if strings.Contains(text, "ocr-only") || strings.Contains(text, "# text_source: ocr") {
		t.Fatalf("vision transcript used OCR:\n%s", text)
	}

	pages, truncated, err := ImageOnlyPDFPages(path)
	if err != nil || truncated || len(pages) != 1 || pages[0].PageNumber != 1 || pages[0].MIME != "image/jpeg" {
		t.Fatalf("ImageOnlyPDFPages pages=%d truncated=%v err=%v", len(pages), truncated, err)
	}
	if _, _, err := image.Decode(bytes.NewReader(pages[0].Data)); err != nil {
		t.Fatalf("attached page is not an image: %v", err)
	}

	prompt := "看图\n" + FilePathPromptPrefix + "\n" + path + "\n"
	atts, notes := SelectedImageOnlyPDFPageAttachments(prompt, 0)
	if len(notes) != 0 || len(atts) != 1 || atts[0].MimeType != "image/jpeg" || atts[0].FileName != "diagram-page-1.jpg" {
		t.Fatalf("attachments=%+v notes=%v", atts, notes)
	}
	got := BuildUserContent(prompt, nil, "openai", true, nil, nil)
	if calls != 0 {
		t.Fatalf("BuildUserContent OCR calls = %d, want 0", calls)
	}
	assertVisionJPEGBlock(t, got)

	officeText, format, err := ExtractOfficeText(path)
	if err != nil {
		t.Fatalf("ExtractOfficeText: %v", err)
	}
	if calls != 1 || format != "pdf" || !strings.Contains(officeText, "# text_source: ocr") || !strings.Contains(officeText, "ocr-only") {
		t.Fatalf("office read calls=%d format=%q text:\n%s", calls, format, officeText)
	}
}

func TestExtractPDFTextVisionWithoutPicturesKeepsScanError(t *testing.T) {
	restoreOfficeImagePDFOCR(t)
	calls := 0
	SetOfficeImagePDFOCR(nil, func(string) (string, error) {
		calls++
		return "ocr-only", nil
	})
	path := filepath.Join(t.TempDir(), "blank.pdf")
	if err := os.WriteFile(path, buildImagePDF(t, imagePDFPage{}), 0o600); err != nil {
		t.Fatal(err)
	}
	text, err := extractPDFText(path, officeReadSettings{imagePDFPreferVision: true})
	if text != "" || err == nil || !strings.Contains(err.Error(), "扫描件") || calls != 0 {
		t.Fatalf("extractPDFText = %q calls=%d err=%v; want scan error and no OCR", text, calls, err)
	}
}

func TestExtractPDFTextVisionDoesNotReplaceNativeText(t *testing.T) {
	restoreOfficeImagePDFOCR(t)
	calls := 0
	SetOfficeImagePDFOCR(nil, func(string) (string, error) {
		calls++
		return "ocr-only", nil
	})
	path := filepath.Join(t.TempDir(), "paper.pdf")
	if err := os.WriteFile(path, buildImagePDF(t, imagePDFPage{text: "Alpha Bravo Charlie Delta"}), 0o600); err != nil {
		t.Fatal(err)
	}
	text, err := extractPDFText(path, officeReadSettings{imagePDFPreferVision: true})
	if err != nil {
		t.Fatalf("extractPDFText: %v", err)
	}
	if calls != 0 || strings.Contains(text, "# text_source:") || !strings.Contains(text, "Alpha") {
		t.Fatalf("native text changed calls=%d text:\n%s", calls, text)
	}
	pages, truncated, err := ImageOnlyPDFPages(path)
	if err != nil || truncated || len(pages) != 0 {
		t.Fatalf("ImageOnlyPDFPages = %d truncated=%v err=%v; want no pages for a text PDF", len(pages), truncated, err)
	}
}

func TestExtractPDFTextVisionPageLimit(t *testing.T) {
	restoreOfficeImagePDFOCR(t)
	setImageOnlyPDFOCRPageLimitForTest(t, 1)
	path := filepath.Join(t.TempDir(), "two.pdf")
	jpegBytes := mustJPEG(t, 32, 32)
	if err := os.WriteFile(path, buildImagePDF(t,
		imagePDFPage{jpeg: jpegBytes, width: 32, height: 32},
		imagePDFPage{jpeg: jpegBytes, width: 32, height: 32},
	), 0o600); err != nil {
		t.Fatal(err)
	}
	text, err := extractPDFText(path, officeReadSettings{imagePDFPreferVision: true})
	if err != nil {
		t.Fatalf("extractPDFText: %v", err)
	}
	if !strings.Contains(text, "# text_source: vision") || !strings.Contains(text, "vision_truncated: true") || !strings.Contains(text, pdfVisionPageMarker) || !strings.Contains(text, pdfVisionSkippedPageMarker) {
		t.Fatalf("limit markers missing:\n%s", text)
	}
	pages, truncated, err := ImageOnlyPDFPages(path)
	if err != nil || !truncated || len(pages) != 1 || pages[0].PageNumber != 1 {
		t.Fatalf("ImageOnlyPDFPages pages=%d truncated=%v err=%v", len(pages), truncated, err)
	}
}

func TestOfficeReadCacheKeyIncludesImagePDFVision(t *testing.T) {
	base := officeReadSettings{engine: OfficeExtractEngineLegacy, fallback: true}
	vision := base
	vision.imagePDFPreferVision = true
	if officeReadCacheKeySuffixForSettings(base) == officeReadCacheKeySuffixForSettings(vision) {
		t.Fatal("vision flag must change the office extract cache key")
	}
	if !strings.Contains(officeReadCacheKeySuffixForSettings(vision), "vision=true") {
		t.Fatalf("cache key = %q", officeReadCacheKeySuffixForSettings(vision))
	}
	spent := vision
	spent.imagePDFVisionUsedBytes = 10
	if officeReadCacheKeySuffixForSettings(vision) == officeReadCacheKeySuffixForSettings(spent) {
		t.Fatal("already-used vision bytes must change the office extract cache key")
	}
	if !strings.Contains(officeReadCacheKeySuffixForSettings(spent), "vision_used=10") {
		t.Fatalf("cache key = %q", officeReadCacheKeySuffixForSettings(spent))
	}
}

func TestExtractPDFTextVisionOverBudgetFallsThroughToOCR(t *testing.T) {
	restoreOfficeImagePDFOCR(t)
	calls := 0
	SetOfficeImagePDFOCR(nil, func(string) (string, error) {
		calls++
		return "ocr-budget", nil
	})
	path := filepath.Join(t.TempDir(), "diagram.pdf")
	jpegBytes := mustJPEG(t, 32, 32)
	if err := os.WriteFile(path, buildImagePDF(t, imagePDFPage{jpeg: jpegBytes, width: 32, height: 32}), 0o600); err != nil {
		t.Fatal(err)
	}
	text, err := extractPDFText(path, officeReadSettings{
		imagePDFPreferVision:    true,
		imagePDFVisionUsedBytes: maxImageOnlyPDFPagesTotalBytes,
	})
	if err != nil {
		t.Fatalf("extractPDFText: %v", err)
	}
	if calls != 1 || !strings.Contains(text, "# text_source: ocr") || !strings.Contains(text, "ocr-budget") {
		t.Fatalf("calls=%d text:\n%s", calls, text)
	}
	if strings.Contains(text, "# text_source: vision") || strings.Contains(text, pdfVisionPageMarker) {
		t.Fatalf("over-budget vision still claimed images:\n%s", text)
	}
}

func TestExtractPDFTextVisionOCRsPagesThatDoNotFit(t *testing.T) {
	restoreOfficeImagePDFOCR(t)
	resetImageOnlyPDFVisionState()
	calls := 0
	SetOfficeImagePDFOCR(nil, func(string) (string, error) {
		calls++
		return "page-two-ocr", nil
	})
	jpegBytes := mustJPEG(t, 36, 28)
	largeBytes := mustJPEG(t, 72, 48)
	path := filepath.Join(t.TempDir(), "two.pdf")
	if err := os.WriteFile(path, buildImagePDF(t,
		imagePDFPage{jpeg: jpegBytes, width: 36, height: 28},
		imagePDFPage{jpeg: largeBytes, width: 72, height: 48},
	), 0o600); err != nil {
		t.Fatal(err)
	}
	// Room for the small page, not the large one. The embedded stream can be
	// a few bytes longer than the original JPEG.
	used := maxImageOnlyPDFPagesTotalBytes - int64(len(jpegBytes)) - 64
	text, err := extractPDFText(path, officeReadSettings{
		imagePDFPreferVision:    true,
		imagePDFVisionUsedBytes: used,
	})
	if err != nil {
		t.Fatalf("extractPDFText: %v", err)
	}
	if calls != 1 {
		t.Fatalf("OCR calls = %d, want 1 for the page that was not attached", calls)
	}
	if !strings.Contains(text, "# text_source: vision") || !strings.Contains(text, imageOnlyPDFVisionLeadMixed) {
		t.Fatalf("mixed lead missing:\n%s", text)
	}
	if !strings.Contains(text, "## Page 1\n"+pdfVisionPageMarker) || !strings.Contains(text, "## Page 2\npage-two-ocr") {
		t.Fatalf("page split missing:\n%s", text)
	}
	if strings.Contains(text, pdfVisionBudgetPageMarker) || strings.Contains(text, imageOnlyPDFVisionLead+"\n") {
		t.Fatalf("unattached page was described as attached or left without OCR:\n%s", text)
	}
	pages, truncated, err := imageOnlyPDFPagesWithinBudget(path, used)
	if err != nil || truncated || len(pages) != 1 || pages[0].PageNumber != 1 {
		t.Fatalf("ImageOnlyPDFPages pages=%d truncated=%v err=%v", len(pages), truncated, err)
	}
	if _, err := extractPDFText(path, officeReadSettings{
		imagePDFPreferVision:    true,
		imagePDFVisionUsedBytes: used,
	}); err != nil {
		t.Fatalf("second extractPDFText: %v", err)
	}
	if calls != 1 || imageOnlyPDFVisionAssembleCalls != 1 {
		t.Fatalf("calls=%d assemble=%d; the mixed transcript was built again", calls, imageOnlyPDFVisionAssembleCalls)
	}
}

func TestExtractPDFTextVisionKeepsUnresolvedPageWhenOCRIsEmpty(t *testing.T) {
	restoreOfficeImagePDFOCR(t)
	resetImageOnlyPDFVisionState()
	calls := 0
	SetOfficeImagePDFOCR(nil, func(string) (string, error) {
		calls++
		return "", nil
	})
	jpegBytes := mustJPEG(t, 30, 30)
	largeBytes := mustJPEG(t, 70, 40)
	path := filepath.Join(t.TempDir(), "two.pdf")
	if err := os.WriteFile(path, buildImagePDF(t,
		imagePDFPage{jpeg: jpegBytes, width: 30, height: 30},
		imagePDFPage{jpeg: largeBytes, width: 70, height: 40},
	), 0o600); err != nil {
		t.Fatal(err)
	}
	text, err := extractPDFText(path, officeReadSettings{
		imagePDFPreferVision:    true,
		imagePDFVisionUsedBytes: maxImageOnlyPDFPagesTotalBytes - int64(len(jpegBytes)) - 64,
	})
	if err != nil {
		t.Fatalf("extractPDFText: %v", err)
	}
	if calls != 1 || !strings.Contains(text, "# text_source: vision") || !strings.Contains(text, imageOnlyPDFVisionLeadUnresolved) || !strings.Contains(text, "## Page 2\n"+pdfVisionBudgetPageMarker) {
		t.Fatalf("calls=%d text:\n%s", calls, text)
	}
	if strings.Contains(text, imageOnlyPDFVisionLeadMixed) || strings.Contains(text, "# text_source: ocr") {
		t.Fatalf("empty OCR replaced the vision transcript:\n%s", text)
	}
}

func TestSelectedImageOnlyPDFPageAttachmentsSkipsNonVisionExtract(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-paper.pdf")
	markerPath := strings.ReplaceAll(path, `\`, `/`)
	prompt := FilePathPromptPrefix + "\n" + path + "\n\n" +
		fmt.Sprintf("--- auto_extract: begin path=%q format=%q total_chars=11 injected_chars=11 injected_tokens=3 truncated=false ---\nHello paper\n--- auto_extract: end path=%q ---", markerPath, "pdf", markerPath)
	atts, notes := SelectedImageOnlyPDFPageAttachments(prompt, 0)
	if len(atts) != 0 || len(notes) != 0 {
		t.Fatalf("attachments=%d notes=%v; a text extract must not reopen the PDF", len(atts), notes)
	}
}

func TestImageOnlyPDFVisionAssembleOnce(t *testing.T) {
	resetImageOnlyPDFVisionState()
	path := filepath.Join(t.TempDir(), "diagram.pdf")
	jpegBytes := mustJPEG(t, 40, 24)
	if err := os.WriteFile(path, buildImagePDF(t, imagePDFPage{jpeg: jpegBytes, width: 40, height: 24}), 0o600); err != nil {
		t.Fatal(err)
	}
	text, err := extractPDFText(path, officeReadSettings{imagePDFPreferVision: true})
	if err != nil {
		t.Fatalf("extractPDFText: %v", err)
	}
	if !strings.Contains(text, "# vision_bytes: ") || visionAttachedBytes(text) <= 0 {
		t.Fatalf("vision byte header missing:\n%s", text)
	}
	pages, truncated, err := ImageOnlyPDFPages(path)
	if err != nil || truncated || len(pages) != 1 {
		t.Fatalf("ImageOnlyPDFPages pages=%d truncated=%v err=%v", len(pages), truncated, err)
	}
	if _, err := extractPDFText(path, officeReadSettings{imagePDFPreferVision: true}); err != nil {
		t.Fatalf("second extractPDFText: %v", err)
	}
	if imageOnlyPDFVisionAssembleCalls != 1 {
		t.Fatalf("assemble calls = %d, want 1", imageOnlyPDFVisionAssembleCalls)
	}
}

func resetImageOnlyPDFVisionState() {
	imageOnlyPDFVisionCacheState.mu.Lock()
	imageOnlyPDFVisionCacheState.items = map[string]imageOnlyPDFVisionCacheEntry{}
	imageOnlyPDFVisionCacheState.total = 0
	imageOnlyPDFVisionCacheState.mu.Unlock()
	imageOnlyPDFVisionAssembleCalls = 0
}

func TestSelectedImageOnlyPDFPageAttachmentsRespectsUsedBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diagram.pdf")
	jpegBytes := mustJPEG(t, 32, 32)
	if err := os.WriteFile(path, buildImagePDF(t, imagePDFPage{jpeg: jpegBytes, width: 32, height: 32}), 0o600); err != nil {
		t.Fatal(err)
	}
	prompt := FilePathPromptPrefix + "\n" + path + "\n"
	atts, notes := SelectedImageOnlyPDFPageAttachments(prompt, maxImageOnlyPDFPagesTotalBytes)
	if len(atts) != 0 || len(notes) == 0 {
		t.Fatalf("attachments=%d notes=%v; want the page dropped", len(atts), notes)
	}
}

func assertVisionJPEGBlock(t *testing.T, got interface{}) {
	t.Helper()
	blocks, ok := got.([]interface{})
	if !ok {
		t.Fatalf("content type = %T, want vision blocks", got)
	}
	sawText := false
	sawJPEG := false
	for _, block := range blocks {
		bm, ok := block.(map[string]interface{})
		if !ok {
			continue
		}
		switch bm["type"] {
		case "text":
			text, _ := bm["text"].(string)
			if strings.Contains(text, "# text_source: vision") && strings.Contains(text, pdfVisionPageMarker) && strings.Contains(text, imageOnlyPDFVisionLead) {
				sawText = true
			}
		case "image_url":
			imageURL, _ := bm["image_url"].(map[string]interface{})
			url, _ := imageURL["url"].(string)
			const prefix = "data:image/jpeg;base64,"
			if !strings.HasPrefix(url, prefix) {
				t.Fatalf("image url = %q", url[:min(len(url), 40)])
			}
			raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(url, prefix))
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := image.Decode(bytes.NewReader(raw)); err != nil {
				t.Fatal(err)
			}
			sawJPEG = true
		}
	}
	if !sawText || !sawJPEG {
		t.Fatalf("vision blocks text=%v jpeg=%v", sawText, sawJPEG)
	}
}

func TestExtractPDFTextSkipsOCRWhenEngineNotReady(t *testing.T) {
	restoreOfficeImagePDFOCR(t)
	calls := 0
	SetOfficeImagePDFOCR(func() bool { return false }, func(string) (string, error) {
		calls++
		return "nope", nil
	})
	path := filepath.Join(t.TempDir(), "diagram.pdf")
	jpegBytes := mustJPEG(t, 32, 32)
	if err := os.WriteFile(path, buildImagePDF(t, imagePDFPage{jpeg: jpegBytes, width: 32, height: 32}), 0o600); err != nil {
		t.Fatal(err)
	}
	text, err := ExtractPDFText(path)
	if text != "" || err == nil || calls != 0 {
		t.Fatalf("ExtractPDFText = %q calls=%d err=%v; want scan error and no OCR call", text, calls, err)
	}
}

func restoreOfficeImagePDFOCR(t *testing.T) {
	t.Helper()
	ready, recognize := officeImageOCRHooks()
	t.Cleanup(func() {
		SetOfficeImagePDFOCR(ready, recognize)
	})
}

func setImageOnlyPDFOCRPageLimitForTest(t *testing.T, n int) {
	t.Helper()
	officeImagePDFOCRMu.Lock()
	prev := imageOnlyPDFOCRPageLimit
	imageOnlyPDFOCRPageLimit = n
	officeImagePDFOCRMu.Unlock()
	t.Cleanup(func() {
		officeImagePDFOCRMu.Lock()
		imageOnlyPDFOCRPageLimit = prev
		officeImagePDFOCRMu.Unlock()
	})
}

func mustJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i] = 20
		img.Pix[i+1] = 40
		img.Pix[i+2] = 80
		img.Pix[i+3] = 255
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 60}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type imagePDFPage struct {
	text          string
	jpeg          []byte
	width, height int
}

func buildImagePDF(t *testing.T, pages ...imagePDFPage) []byte {
	t.Helper()
	type built struct {
		resources string
		content   string
		extra     []string
	}
	builtPages := make([]built, len(pages))
	for i, page := range pages {
		switch {
		case len(page.jpeg) > 0:
			extra := fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n%s\nendstream", page.width, page.height, len(page.jpeg), page.jpeg)
			builtPages[i] = built{
				resources: "/XObject << /Im1 0 0 R >>",
				content:   "q 200 0 0 200 72 500 cm /Im1 Do Q",
				extra:     []string{extra},
			}
		case page.text != "":
			builtPages[i] = built{
				resources: "/Font << /F1 3 0 R >>",
				content:   "BT /F1 12 Tf 72 720 Td (" + page.text + ") Tj ET",
			}
		default:
			builtPages[i] = built{content: ""}
		}
	}
	pageNums := make([]int, len(builtPages))
	next := 4
	for i, page := range builtPages {
		pageNums[i] = next
		next += 2 + len(page.extra)
	}
	refs := make([]string, len(pageNums))
	for i, n := range pageNums {
		refs[i] = fmt.Sprintf("%d 0 R", n)
	}
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [" + strings.Join(refs, " ") + "] /Count " + strconv.Itoa(len(builtPages)) + " >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	for i, page := range builtPages {
		objects = append(objects, "")
		objects = append(objects, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(page.content), page.content))
		objects = append(objects, page.extra...)
		resources := strings.ReplaceAll(page.resources, "/Im1 0 0 R", fmt.Sprintf("/Im1 %d 0 R", pageNums[i]+2))
		objects[pageNums[i]-1] = fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << %s >> /Contents %d 0 R >>", resources, pageNums[i]+1)
	}
	var output bytes.Buffer
	output.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for i, object := range objects {
		offsets[i+1] = output.Len()
		fmt.Fprintf(&output, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := output.Len()
	fmt.Fprintf(&output, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&output, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&output, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return output.Bytes()
}
