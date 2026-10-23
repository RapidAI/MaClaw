package agent

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	gopdf2 "github.com/VantageDataChat/GoPDF2"
)

func TestPDFScanPagePayloadsReadsClassicAndObjectStream(t *testing.T) {
	jpegBytes := mustJPEG(t, 24, 24)
	page := imagePDFPage{jpeg: jpegBytes, width: 24, height: 24}
	classic := buildImagePDF(t, page)
	if !plaintextHasPDFPageObject(classic) {
		t.Fatal("classic fixture lost its page object")
	}
	got, err := PDFScanPagePayloads(classic, 1, 8)
	if err != nil || len(got) != 1 || got[0].PageNumber != 1 || got[0].MIME != "image/jpeg" || !bytes.Equal(got[0].Data, jpegBytes) {
		t.Fatalf("classic payloads=%d err=%v", len(got), err)
	}
	stream := buildObjectStreamImagePDF(t, page)
	if plaintextHasPDFPageObject(stream) {
		t.Fatal("object-stream fixture still has a plaintext page object")
	}
	got, err = PDFScanPagePayloads(stream, 1, 8)
	if err != nil || len(got) != 1 || !bytes.Equal(got[0].Data, jpegBytes) || got[0].MIME != "image/jpeg" {
		t.Fatalf("object-stream payloads=%d err=%v", len(got), err)
	}
}

func TestPDFSelectableTextReadsPageTreeOnly(t *testing.T) {
	jpegBytes := mustJPEG(t, 24, 24)
	page := imagePDFPage{jpeg: jpegBytes, width: 24, height: 24}
	scan := buildObjectStreamImagePDF(t, page, page)
	got, err := PDFSelectableText(scan, []int{1, 2})
	if err != nil || strings.TrimSpace(got[1]) != "" || strings.TrimSpace(got[2]) != "" {
		t.Fatalf("object-stream scan text=%v err=%v", got, err)
	}
	classic := buildImagePDF(t, imagePDFPage{text: "Alpha Bravo Charlie Delta"}, page)
	got, err = PDFSelectableText(classic, []int{1, 2})
	if err != nil || !strings.Contains(got[1], "Alpha Bravo Charlie Delta") || strings.TrimSpace(got[2]) != "" {
		t.Fatalf("classic text=%v err=%v", got, err)
	}
}

func TestOpenPDFPageTreeParsesObjectStreamOnce(t *testing.T) {
	jpegBytes := mustJPEG(t, 24, 24)
	page := imagePDFPage{jpeg: jpegBytes, width: 24, height: 24}
	stream := buildObjectStreamImagePDF(t, page, page)
	tree, err := OpenPDFPageTree(stream)
	if err != nil {
		t.Fatal(err)
	}
	texts, err := tree.SelectableText([]int{1, 2})
	if err != nil || strings.TrimSpace(texts[1]) != "" || strings.TrimSpace(texts[2]) != "" {
		t.Fatalf("object-stream text=%v err=%v", texts, err)
	}
	batch, pictures, err := tree.OCRImagePages(1, 2, 0, func(string) (string, error) {
		return "扫描正文", nil
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if tree.PageCount() != 2 || tree.treeOpens() != 1 {
		t.Fatalf("object-stream opens=%d pages=%d", tree.treeOpens(), tree.PageCount())
	}
	if batch.Recognized != 2 || len(pictures) != 2 || !bytes.Equal(pictures[0].Data, jpegBytes) {
		t.Fatalf("object-stream batch=%+v pictures=%d", batch, len(pictures))
	}

	classic := buildImagePDF(t, page, page)
	ctree, err := OpenPDFPageTree(classic)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ctree.SelectableText([]int{1}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ctree.OCRImagePages(1, 1, 2, func(string) (string, error) {
		return "甲", nil
	}, false); err != nil {
		t.Fatal(err)
	}
	if ctree.PageCount() != 2 || ctree.treeOpens() != 1 {
		t.Fatalf("classic opens=%d pages=%d", ctree.treeOpens(), ctree.PageCount())
	}
}

func TestPDFPageDictResolvesEachLeafOnce(t *testing.T) {
	jpegBytes := mustJPEG(t, 24, 24)
	page := imagePDFPage{jpeg: jpegBytes, width: 24, height: 24}
	stream := buildObjectStreamImagePDF(t, page, page, page, page)
	tree, err := OpenPDFPageTree(stream)
	if err != nil {
		t.Fatal(err)
	}
	recognize := func(string) (string, error) { return "扫描正文", nil }
	_, pictures, err := tree.OCRImagePages(1, 2, 3, recognize, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(pictures) != 2 || !bytes.Equal(pictures[0].Data, jpegBytes) || !bytes.Equal(pictures[1].Data, jpegBytes) {
		t.Fatalf("first pictures=%d", len(pictures))
	}
	if got := tree.pageKidResolves(); got != 2 {
		t.Fatalf("first window resolved %d kids", got)
	}
	_, pictures, err = tree.OCRImagePages(3, 2, 0, recognize, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(pictures) != 2 || pictures[0].PageNumber != 3 || !bytes.Equal(pictures[1].Data, jpegBytes) {
		t.Fatalf("second pictures=%d", len(pictures))
	}
	if got := tree.pageKidResolves(); got != 4 {
		t.Fatalf("second window resolved %d kids", got)
	}
	if tree.treeOpens() != 1 {
		t.Fatalf("opens=%d", tree.treeOpens())
	}
	if _, err := tree.SelectableText([]int{1, 2, 3, 4}); err != nil {
		t.Fatal(err)
	}
	if got := tree.pageKidResolves(); got != 4 {
		t.Fatalf("text reread resolved %d kids", got)
	}
}

func TestPDFPageDictSkipsUnvisitedSubtree(t *testing.T) {
	jpegBytes := mustJPEG(t, 24, 24)
	page := imagePDFPage{jpeg: jpegBytes, width: 24, height: 24}
	stream := buildNestedObjectStreamImagePDF(t, page, page, page, page)
	tree, err := OpenPDFPageTree(stream)
	if err != nil {
		t.Fatal(err)
	}
	recognize := func(string) (string, error) { return "扫描正文", nil }
	if _, _, err := tree.OCRImagePages(1, 1, 2, recognize, true); err != nil {
		t.Fatal(err)
	}
	if got := tree.pageKidResolves(); got != 2 {
		t.Fatalf("page 1 resolved %d kids", got)
	}
	_, pictures, err := tree.OCRImagePages(3, 1, 4, recognize, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(pictures) != 1 || pictures[0].PageNumber != 3 || !bytes.Equal(pictures[0].Data, jpegBytes) {
		t.Fatalf("page 3 pictures=%d", len(pictures))
	}
	if got := tree.pageKidResolves(); got != 4 {
		t.Fatalf("page 3 resolved %d kids", got)
	}
	if tree.PageCount() != 4 || tree.treeOpens() != 1 {
		t.Fatalf("pages=%d opens=%d", tree.PageCount(), tree.treeOpens())
	}
}

func TestPDFPageDictIndexesOnlyTheRequestedLeaf(t *testing.T) {
	pages := make([]imagePDFPage, 4)
	jpegs := make([][]byte, 4)
	for i := 0; i < 4; i++ {
		jpegs[i] = solidJPEG(t, 24, 24, uint8(40*(i+1)), 16, 32)
		pages[i] = imagePDFPage{jpeg: jpegs[i], width: 24, height: 24}
	}
	tree, err := OpenPDFPageTree(buildObjectStreamImagePDF(t, pages...))
	if err != nil {
		t.Fatal(err)
	}
	recognize := func(string) (string, error) { return "丁", nil }
	_, pictures, err := tree.OCRImagePages(4, 1, 5, recognize, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(pictures) != 1 || pictures[0].PageNumber != 4 || !bytes.Equal(pictures[0].Data, jpegs[3]) {
		t.Fatalf("page 4 pictures=%d", len(pictures))
	}
	if got := tree.pageKidResolves(); got != 1 {
		t.Fatalf("page 4 resolved %d kids", got)
	}
	if got := tree.pageImageCollects(); got != 1 {
		t.Fatalf("page 4 collected %d", got)
	}
	_, pictures, err = tree.OCRImagePages(4, 1, 5, recognize, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(pictures) != 1 || !bytes.Equal(pictures[0].Data, jpegs[3]) {
		t.Fatalf("cached page 4 pictures=%d", len(pictures))
	}
	if tree.pageKidResolves() != 1 || tree.pageImageCollects() != 1 || tree.treeOpens() != 1 {
		t.Fatalf("cached resolves=%d collects=%d opens=%d", tree.pageKidResolves(), tree.pageImageCollects(), tree.treeOpens())
	}
	_, pictures, err = tree.OCRImagePages(1, 1, 2, recognize, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(pictures) != 1 || pictures[0].PageNumber != 1 || !bytes.Equal(pictures[0].Data, jpegs[0]) {
		t.Fatalf("page 1 pictures=%d", len(pictures))
	}
	if tree.pageKidResolves() != 2 || tree.pageImageCollects() != 2 {
		t.Fatalf("after page 1 resolves=%d collects=%d", tree.pageKidResolves(), tree.pageImageCollects())
	}
}

func TestOpenCachedPDFPageTreeReusesObjectStreamParse(t *testing.T) {
	jpegBytes := mustJPEG(t, 24, 24)
	page := imagePDFPage{jpeg: jpegBytes, width: 24, height: 24}
	data := buildObjectStreamImagePDF(t, page, page)
	path := filepath.Join(t.TempDir(), "scan.pdf")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	before := pdfPageTreeFileReads
	tree, err := OpenCachedPDFPageTree(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tree.SelectableText([]int{1, 2}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := tree.OCRImagePages(1, 1, 2, func(string) (string, error) {
		return "甲", nil
	}, false); err != nil {
		t.Fatal(err)
	}
	if tree.treeOpens() != 1 {
		t.Fatalf("opens=%d", tree.treeOpens())
	}
	again, err := OpenCachedPDFPageTree(path)
	if err != nil {
		t.Fatal(err)
	}
	if again != tree || again.treeOpens() != 1 || pdfPageTreeFileReads-before != 1 {
		t.Fatalf("reopen tree=%p opens=%d reads=%d", again, again.treeOpens(), pdfPageTreeFileReads-before)
	}
	replaced := buildObjectStreamImagePDF(t, page, page, page)
	if err := os.WriteFile(path, replaced, 0o644); err != nil {
		t.Fatal(err)
	}
	third, err := OpenCachedPDFPageTree(path)
	if err != nil {
		t.Fatal(err)
	}
	if third == tree || third.PageCount() != 3 || pdfPageTreeFileReads-before != 2 {
		t.Fatalf("replaced pages=%d reads=%d same=%v", third.PageCount(), pdfPageTreeFileReads-before, third == tree)
	}
}

func TestOCRImagePDFPageBatchHonorsBudget(t *testing.T) {
	jpegBytes := mustJPEG(t, 32, 32)
	page := imagePDFPage{jpeg: jpegBytes, width: 32, height: 32}
	data := buildImagePDF(t, page, page, page)
	if !plaintextHasPDFPageObject(data) {
		t.Fatal("classic page objects must stay on the raw parser")
	}
	var calls int
	batch, err := OCRImagePDFPageBatch(data, 1, 2, 0, func(string) (string, error) {
		calls++
		return "甲", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || batch.NextPage != 3 || batch.Recognized != 2 || batch.TotalPages != 3 {
		t.Fatalf("calls=%d batch=%+v", calls, batch)
	}
	if strings.Contains(batch.Text, "## Page 3\n") {
		t.Fatalf("page past the budget was written:\n%s", batch.Text)
	}
	var stopped int
	stoppedBatch, err := OCRImagePDFPageBatch(data, 1, 8, 2, func(string) (string, error) {
		stopped++
		return "甲", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if stopped != 1 || stoppedBatch.NextPage != 2 || strings.Contains(stoppedBatch.Text, "## Page 2\n") {
		t.Fatalf("stop before a finished page: calls=%d batch=%+v\n%s", stopped, stoppedBatch, stoppedBatch.Text)
	}
}

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
	filter        string
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
		case len(page.jpeg) > 0 && page.text != "":
			extra := fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n%s\nendstream", page.width, page.height, len(page.jpeg), page.jpeg)
			builtPages[i] = built{
				resources: "/Font << /F1 3 0 R >> /XObject << /Im1 0 0 R >>",
				content:   "BT /F1 12 Tf 72 720 Td (" + page.text + ") Tj ET\nq 200 0 0 200 72 500 cm /Im1 Do Q",
				extra:     []string{extra},
			}
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

func TestEmbeddedJPEGPayloadReturnsOriginalBytes(t *testing.T) {
	jpegBytes := mustJPEG(t, 32, 40)
	got, err := embeddedPDFImagePayload(gopdf2.ExtractedImage{
		Name: "/Im1", Width: 32, Height: 40, Filter: "DCTDecode", Data: jpegBytes,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, jpegBytes) {
		t.Fatalf("jpeg payload changed %d bytes into %d", len(jpegBytes), len(got))
	}
}

func TestOCRImagePDFPageBatchReadsObjectStreamJPEG(t *testing.T) {
	jpeg1 := solidJPEG(t, 32, 32, 20, 40, 80)
	jpeg2 := solidJPEG(t, 32, 40, 200, 10, 10)
	data := buildObjectStreamImagePDF(t,
		imagePDFPage{jpeg: jpeg1, width: 32, height: 32},
		imagePDFPage{jpeg: jpeg2, width: 32, height: 40},
	)
	_, rawErr := gopdf2.ExtractImagesFromPage(data, 0)
	if rawErr == nil || !strings.Contains(rawErr.Error(), "out of range (0..-1)") {
		t.Fatalf("raw parser = %v; object-stream page tree should be invisible", rawErr)
	}
	if plaintextHasPDFPageObject(data) {
		t.Fatal("compressed page tree should skip the raw parser")
	}
	var got [][]byte
	batch, err := OCRImagePDFPageBatch(data, 1, 8, 0, func(imageBase64 string) (string, error) {
		raw, decErr := base64.StdEncoding.DecodeString(imageBase64)
		if decErr != nil {
			return "", decErr
		}
		got = append(got, raw)
		return "字", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if batch.Recognized != 2 || batch.TotalPages != 2 || len(got) != 2 {
		t.Fatalf("batch=%+v payloads=%d", batch, len(got))
	}
	if !bytes.Equal(got[0], jpeg1) || !bytes.Equal(got[1], jpeg2) {
		t.Fatalf("page images were not the object-stream JPEGs")
	}
	got = nil
	second, err := OCRImagePDFPageBatch(data, 2, 1, 0, func(imageBase64 string) (string, error) {
		raw, decErr := base64.StdEncoding.DecodeString(imageBase64)
		if decErr != nil {
			return "", decErr
		}
		got = append(got, raw)
		return "乙", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Recognized != 1 || len(got) != 1 || !bytes.Equal(got[0], jpeg2) {
		t.Fatalf("start page 2 batch=%+v payloads=%d", second, len(got))
	}
}

func TestOCRImagePDFPageBatchKeepsUnreadableObjectStreamImage(t *testing.T) {
	data := buildObjectStreamImagePDF(t, imagePDFPage{
		jpeg:   []byte("not-a-jpeg"),
		width:  32,
		height: 32,
		filter: "CCITTFaxDecode",
	})
	batch, err := OCRImagePDFPageBatch(data, 1, 8, 0, func(string) (string, error) {
		t.Fatal("a non-jpeg page image must not be sent to OCR")
		return "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if batch.Recognized != 0 || !strings.Contains(batch.Text, ocrPDFPageErrorMarker) || strings.Contains(batch.Text, pdfEmptyPageMarker) {
		t.Fatalf("batch=%+v", batch)
	}
}

func TestExtractPDFTextObjectStreamSkipsBlindRawParse(t *testing.T) {
	jpeg := mustJPEG(t, 32, 32)
	scan := buildObjectStreamImagePDF(t,
		imagePDFPage{jpeg: jpeg, width: 32, height: 32},
		imagePDFPage{jpeg: jpeg, width: 32, height: 32},
	)
	if _, skip := pdfLayoutPass(scan, 2); !skip {
		t.Fatal("object-stream scan still extracts each page with the raw parser")
	}
	classic := buildImagePDF(t, imagePDFPage{jpeg: jpeg, width: 32, height: 32})
	if _, skip := pdfLayoutPass(classic, 1); skip {
		t.Fatal("classic page tree skipped the plain extractor")
	}
	textPage := buildImagePDF(t, imagePDFPage{text: "Alpha Bravo Charlie Delta"})
	if _, skip := pdfLayoutPass(textPage, 1); skip {
		t.Fatal("a text page skipped the plain extractor")
	}
}

func TestExtractPDFTextOCRsObjectStreamPageImage(t *testing.T) {
	restoreOfficeImagePDFOCR(t)
	setImageOnlyPDFOCRPageLimitForTest(t, 16)
	jpeg1 := solidJPEG(t, 32, 32, 20, 40, 80)
	jpeg3 := solidJPEG(t, 32, 40, 200, 10, 10)
	data := buildObjectStreamImagePDF(t,
		imagePDFPage{jpeg: jpeg1, width: 32, height: 32},
		imagePDFPage{},
		imagePDFPage{jpeg: jpeg3, width: 32, height: 40},
	)
	if plaintextHasPDFPageObject(data) {
		t.Fatal("compressed page tree should skip the raw parser")
	}
	all, allErr := gopdf2.ExtractImagesFromAllPages(data)
	if allErr != nil || len(all) != 0 {
		t.Fatalf("ExtractImagesFromAllPages = %d, %v; an object-stream page tree is an empty map", len(all), allErr)
	}
	var got [][]byte
	SetOfficeImagePDFOCR(nil, func(imageBase64 string) (string, error) {
		raw, err := base64.StdEncoding.DecodeString(imageBase64)
		if err != nil {
			return "", err
		}
		got = append(got, raw)
		return "第" + strconv.Itoa(len(got)) + "页", nil
	})
	path := filepath.Join(t.TempDir(), "scan.pdf")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	text, err := ExtractPDFText(path)
	if err != nil {
		t.Fatalf("ExtractPDFText: %v", err)
	}
	if len(got) != 2 || !bytes.Equal(got[0], jpeg1) || !bytes.Equal(got[1], jpeg3) {
		t.Fatalf("payloads=%d", len(got))
	}
	if !strings.Contains(text, "# text_source: ocr") || !strings.Contains(text, "## Page 1\n第1页") || !strings.Contains(text, "## Page 2\n"+pdfEmptyPageMarker) || !strings.Contains(text, "## Page 3\n第2页") {
		t.Fatalf("transcript:\n%s", text)
	}
}

func TestExtractPDFTextObjectStreamUnreadablePageStaysAnError(t *testing.T) {
	restoreOfficeImagePDFOCR(t)
	setImageOnlyPDFOCRPageLimitForTest(t, 16)
	jpeg2 := mustJPEG(t, 32, 32)
	data := buildObjectStreamImagePDF(t,
		imagePDFPage{jpeg: []byte("not-a-jpeg"), width: 32, height: 32, filter: "CCITTFaxDecode"},
		imagePDFPage{jpeg: jpeg2, width: 32, height: 32},
	)
	calls := 0
	SetOfficeImagePDFOCR(nil, func(string) (string, error) {
		calls++
		return "第二页", nil
	})
	path := filepath.Join(t.TempDir(), "scan.pdf")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	text, err := ExtractPDFText(path)
	if err != nil {
		t.Fatalf("ExtractPDFText: %v", err)
	}
	if calls != 1 || !strings.Contains(text, "## Page 1\n[page extract error: pdf page image is not a decodable jpeg]") || !strings.Contains(text, "## Page 2\n第二页") {
		t.Fatalf("calls=%d text:\n%s", calls, text)
	}
	if strings.Contains(text, "## Page 1\n"+pdfEmptyPageMarker) {
		t.Fatalf("unreadable image was an empty page:\n%s", text)
	}
}

func TestExtractPDFTextObjectStreamPageLimitSkipsUnreadImage(t *testing.T) {
	restoreOfficeImagePDFOCR(t)
	setImageOnlyPDFOCRPageLimitForTest(t, 1)
	data := buildObjectStreamImagePDF(t,
		imagePDFPage{jpeg: mustJPEG(t, 32, 32), width: 32, height: 32},
		imagePDFPage{jpeg: []byte("not-a-jpeg"), width: 32, height: 32, filter: "CCITTFaxDecode"},
		imagePDFPage{},
	)
	calls := 0
	SetOfficeImagePDFOCR(nil, func(string) (string, error) {
		calls++
		return "第一页", nil
	})
	path := filepath.Join(t.TempDir(), "scan.pdf")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	text, err := ExtractPDFText(path)
	if err != nil {
		t.Fatalf("ExtractPDFText: %v", err)
	}
	if calls != 1 || !strings.Contains(text, "ocr_truncated: true") || !strings.Contains(text, "## Page 1\n第一页") || !strings.Contains(text, "## Page 2\n"+pdfOCRSkippedPageMarker) || !strings.Contains(text, "## Page 3\n"+pdfEmptyPageMarker) {
		t.Fatalf("calls=%d text:\n%s", calls, text)
	}
	if strings.Contains(text, "[page extract error") {
		t.Fatalf("a page past the cap was extracted:\n%s", text)
	}
}

func TestExtractPDFTextVisionReadsObjectStreamPageImage(t *testing.T) {
	restoreOfficeImagePDFOCR(t)
	resetImageOnlyPDFVisionState()
	setImageOnlyPDFOCRPageLimitForTest(t, 1)
	jpeg1 := solidJPEG(t, 32, 32, 11, 22, 33)
	jpeg2 := solidJPEG(t, 32, 40, 44, 55, 66)
	data := buildObjectStreamImagePDF(t,
		imagePDFPage{jpeg: jpeg1, width: 32, height: 32},
		imagePDFPage{jpeg: jpeg2, width: 32, height: 40},
	)
	all, allErr := gopdf2.ExtractImagesFromAllPages(data)
	if allErr != nil || len(all) != 0 {
		t.Fatalf("ExtractImagesFromAllPages = %d, %v", len(all), allErr)
	}
	calls := 0
	SetOfficeImagePDFOCR(nil, func(string) (string, error) {
		calls++
		return "ocr-only", nil
	})
	path := filepath.Join(t.TempDir(), "scan.pdf")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	text, err := extractPDFText(path, officeReadSettings{imagePDFPreferVision: true})
	if err != nil {
		t.Fatalf("extractPDFText: %v", err)
	}
	if calls != 0 || !strings.Contains(text, "# text_source: vision") || !strings.Contains(text, "vision_truncated: true") || !strings.Contains(text, "## Page 1\n"+pdfVisionPageMarker) || !strings.Contains(text, "## Page 2\n"+pdfVisionSkippedPageMarker) {
		t.Fatalf("calls=%d text:\n%s", calls, text)
	}
	pages, truncated, err := ImageOnlyPDFPages(path)
	if err != nil || !truncated || len(pages) != 1 || pages[0].PageNumber != 1 || pages[0].MIME != "image/jpeg" || !bytes.Equal(pages[0].Data, jpeg1) {
		t.Fatalf("pages=%d truncated=%v err=%v", len(pages), truncated, err)
	}
}

func TestPDFHasEmbeddedImageProbesObjectStream(t *testing.T) {
	jpeg := mustJPEG(t, 32, 32)
	scan := buildObjectStreamImagePDF(t, imagePDFPage{jpeg: jpeg, width: 32, height: 32})
	if !pdfHasEmbeddedImage(scan) {
		t.Fatal("object-stream scan was reported as having no image")
	}
	tiny := buildObjectStreamImagePDF(t, imagePDFPage{jpeg: mustJPEG(t, 4, 4), width: 4, height: 4})
	if pdfHasEmbeddedImage(tiny) {
		t.Fatal("a 4px decoration counted as a page image")
	}
	blank := buildObjectStreamImagePDF(t, imagePDFPage{})
	if pdfHasEmbeddedImage(blank) {
		t.Fatal("a blank object-stream page counted as an image")
	}
	classic := buildImagePDF(t, imagePDFPage{jpeg: jpeg, width: 32, height: 32})
	if !pdfHasEmbeddedImage(classic) {
		t.Fatal("classic page image was missed")
	}

	early := make([]imagePDFPage, ocrPDFImageProbePages)
	early[ocrPDFImageProbePages-1] = imagePDFPage{jpeg: jpeg, width: 32, height: 32}
	if !pdfHasEmbeddedImage(buildObjectStreamImagePDF(t, early...)) {
		t.Fatal("an image on the last probed page was missed")
	}
	late := make([]imagePDFPage, ocrPDFImageProbePages+1)
	late[ocrPDFImageProbePages] = imagePDFPage{jpeg: jpeg, width: 32, height: 32}
	if pdfHasEmbeddedImage(buildObjectStreamImagePDF(t, late...)) {
		t.Fatal("the probe walked past its page cap")
	}
}

func solidJPEG(t *testing.T, w, h int, r, g, b uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i] = r
		img.Pix[i+1] = g
		img.Pix[i+2] = b
		img.Pix[i+3] = 255
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 70}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// buildObjectStreamImagePDF stores the page tree in an object stream.
// The image and content streams stay ordinary objects, which is how a
// PDF 1.5 scan keeps JPEG XObjects while hiding /Type /Page from a raw
// "N gen obj" scan.
func buildObjectStreamImagePDF(t *testing.T, pages ...imagePDFPage) []byte {
	t.Helper()
	if len(pages) == 0 {
		t.Fatal("no pages")
	}
	n := len(pages)
	catalogNum := 2
	pagesNum := 3
	firstPageNum := 4
	firstClassic := firstPageNum + n
	nums := make([]int, 0, 2+n)
	bodies := make([]string, 0, 2+n)
	nums = append(nums, catalogNum)
	bodies = append(bodies, fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pagesNum))
	kids := make([]string, n)
	for i := 0; i < n; i++ {
		kids[i] = fmt.Sprintf("%d 0 R", firstPageNum+i)
	}
	nums = append(nums, pagesNum)
	bodies = append(bodies, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), n))
	for i, page := range pages {
		imageNum := firstClassic + 2*i
		contentNum := imageNum + 1
		nums = append(nums, firstPageNum+i)
		if objectStreamPageHasImage(page) {
			bodies = append(bodies, fmt.Sprintf("<< /Type /Page /Parent %d 0 R /MediaBox [0 0 200 200] /Resources << /XObject << /Im1 %d 0 R >> >> /Contents %d 0 R >>", pagesNum, imageNum, contentNum))
		} else {
			bodies = append(bodies, fmt.Sprintf("<< /Type /Page /Parent %d 0 R /MediaBox [0 0 200 200] /Contents %d 0 R >>", pagesNum, contentNum))
		}
	}
	stream, first := encodeObjectStream(nums, bodies)
	compressed, zipErr := zlibWrap([]byte(stream))
	if zipErr != nil {
		t.Fatal(zipErr)
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.5\n")
	offsets := make([]int, firstClassic+2*n+1)
	offsets[1] = writePDFStreamObject(&buf, 1, fmt.Sprintf("/Type /ObjStm /N %d /First %d /Filter /FlateDecode", len(nums), first), compressed)
	content := []byte("q 200 0 0 200 0 0 cm /Im1 Do Q")
	for i, page := range pages {
		imageNum := firstClassic + 2*i
		contentNum := imageNum + 1
		pageContent := content
		if objectStreamPageHasImage(page) {
			filter := page.filter
			if filter == "" {
				filter = "DCTDecode"
			}
			dict := fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /%s", page.width, page.height, filter)
			offsets[imageNum] = writePDFStreamObject(&buf, imageNum, dict, page.jpeg)
		} else {
			pageContent = nil
		}
		offsets[contentNum] = writePDFStreamObject(&buf, contentNum, "", pageContent)
	}
	xrefNum := firstClassic + 2*n
	xrefAt := buf.Len()
	size := xrefNum + 1
	var table bytes.Buffer
	writeXRefEntry(&table, 0, 0, 65535)
	writeXRefEntry(&table, 1, offsets[1], 0)
	for i := 0; i < len(nums); i++ {
		writeXRefEntry(&table, 2, 1, i)
	}
	for num := firstClassic; num < xrefNum; num++ {
		if offsets[num] == 0 {
			// A blank page has no image object. Keep the object number free
			// so the content-stream numbers stay paired with each page.
			writeXRefEntry(&table, 0, 0, 65535)
			continue
		}
		writeXRefEntry(&table, 1, offsets[num], 0)
	}
	writeXRefEntry(&table, 1, xrefAt, 0)
	if table.Len() != size*7 {
		t.Fatalf("xref entries=%d size=%d", table.Len()/7, size)
	}
	writePDFStreamObject(&buf, xrefNum, fmt.Sprintf("/Type /XRef /Size %d /Index [0 %d] /W [1 4 2] /Root %d 0 R", size, size, catalogNum), table.Bytes())
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF\n", xrefAt)
	return buf.Bytes()
}

// buildNestedObjectStreamImagePDF splits four pages under two /Pages nodes
// so a lookup can skip the other subtree by /Count.
func buildNestedObjectStreamImagePDF(t *testing.T, pages ...imagePDFPage) []byte {
	t.Helper()
	if len(pages) != 4 {
		t.Fatal("nested fixture wants 4 pages")
	}
	n := len(pages)
	catalogNum := 2
	rootNum := 3
	midA := 4
	midB := 5
	firstPageNum := 6
	firstClassic := 10
	nums := []int{catalogNum, rootNum, midA, midB}
	bodies := []string{
		fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", rootNum),
		fmt.Sprintf("<< /Type /Pages /Kids [%d 0 R %d 0 R] /Count %d >>", midA, midB, n),
		fmt.Sprintf("<< /Type /Pages /Parent %d 0 R /Kids [%d 0 R %d 0 R] /Count 2 >>", rootNum, firstPageNum, firstPageNum+1),
		fmt.Sprintf("<< /Type /Pages /Parent %d 0 R /Kids [%d 0 R %d 0 R] /Count 2 >>", rootNum, firstPageNum+2, firstPageNum+3),
	}
	for i, page := range pages {
		parent := midA
		if i >= 2 {
			parent = midB
		}
		imageNum := firstClassic + 2*i
		contentNum := imageNum + 1
		nums = append(nums, firstPageNum+i)
		if !objectStreamPageHasImage(page) {
			t.Fatal("nested fixture page needs an image")
		}
		bodies = append(bodies, fmt.Sprintf("<< /Type /Page /Parent %d 0 R /MediaBox [0 0 200 200] /Resources << /XObject << /Im1 %d 0 R >> >> /Contents %d 0 R >>", parent, imageNum, contentNum))
	}
	stream, first := encodeObjectStream(nums, bodies)
	compressed, zipErr := zlibWrap([]byte(stream))
	if zipErr != nil {
		t.Fatal(zipErr)
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.5\n")
	xrefNum := firstClassic + 2*n
	offsets := make([]int, xrefNum+1)
	offsets[1] = writePDFStreamObject(&buf, 1, fmt.Sprintf("/Type /ObjStm /N %d /First %d /Filter /FlateDecode", len(nums), first), compressed)
	content := []byte("q 200 0 0 200 0 0 cm /Im1 Do Q")
	for i, page := range pages {
		imageNum := firstClassic + 2*i
		contentNum := imageNum + 1
		filter := page.filter
		if filter == "" {
			filter = "DCTDecode"
		}
		dict := fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /%s", page.width, page.height, filter)
		offsets[imageNum] = writePDFStreamObject(&buf, imageNum, dict, page.jpeg)
		offsets[contentNum] = writePDFStreamObject(&buf, contentNum, "", content)
	}
	xrefAt := buf.Len()
	size := xrefNum + 1
	var table bytes.Buffer
	writeXRefEntry(&table, 0, 0, 65535)
	writeXRefEntry(&table, 1, offsets[1], 0)
	for i := 0; i < len(nums); i++ {
		writeXRefEntry(&table, 2, 1, i)
	}
	for num := firstClassic; num < xrefNum; num++ {
		writeXRefEntry(&table, 1, offsets[num], 0)
	}
	writeXRefEntry(&table, 1, xrefAt, 0)
	if table.Len() != size*7 {
		t.Fatalf("xref entries=%d size=%d", table.Len()/7, size)
	}
	writePDFStreamObject(&buf, xrefNum, fmt.Sprintf("/Type /XRef /Size %d /Index [0 %d] /W [1 4 2] /Root %d 0 R", size, size, catalogNum), table.Bytes())
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF\n", xrefAt)
	return buf.Bytes()
}

func objectStreamPageHasImage(page imagePDFPage) bool {
	return len(page.jpeg) > 0 || page.width > 0 || page.height > 0 || page.filter != ""
}

func zlibWrap(payload []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	if _, err := w.Write(payload); err != nil {
		_ = w.Close()
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encodeObjectStream(nums []int, bodies []string) (string, int) {
	offs := make([]int, len(bodies))
	at := 0
	for i, body := range bodies {
		offs[i] = at
		at += len(body)
		if i+1 < len(bodies) {
			at++
		}
	}
	var header strings.Builder
	for i, num := range nums {
		fmt.Fprintf(&header, "%d %d ", num, offs[i])
	}
	var body strings.Builder
	for i, text := range bodies {
		if i > 0 {
			body.WriteByte('\n')
		}
		body.WriteString(text)
	}
	return header.String() + body.String(), header.Len()
}

func writePDFStreamObject(buf *bytes.Buffer, num int, dict string, stream []byte) int {
	offset := buf.Len()
	if dict != "" {
		dict += " "
	}
	fmt.Fprintf(buf, "%d 0 obj\n<< %s/Length %d >>\nstream\n", num, dict, len(stream))
	buf.Write(stream)
	buf.WriteString("\nendstream\nendobj\n")
	return offset
}

func writeXRefEntry(buf *bytes.Buffer, typ, a, b int) {
	var entry [7]byte
	entry[0] = byte(typ)
	entry[1] = byte(a >> 24)
	entry[2] = byte(a >> 16)
	entry[3] = byte(a >> 8)
	entry[4] = byte(a)
	entry[5] = byte(b >> 8)
	entry[6] = byte(b)
	buf.Write(entry[:])
}
