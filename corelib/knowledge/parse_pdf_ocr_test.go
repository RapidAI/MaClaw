package knowledge

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/pdfinspector"
	gopdf "github.com/VantageDataChat/GoPDF2"
)

type stubPDFOCRProvider struct {
	text  string
	calls int
	err   error
}

type cancellingPDFOCRProvider struct {
	cancel context.CancelFunc
	calls  int
	err    error
}

func (s *cancellingPDFOCRProvider) Recognize(data string) ([]OCRResult, error) {
	if _, err := base64.StdEncoding.DecodeString(data); err != nil {
		return nil, fmt.Errorf("expected base64 PNG: %w", err)
	}
	s.calls++
	s.cancel()
	if s.err != nil {
		return nil, s.err
	}
	return []OCRResult{{Text: "stale OCR text"}}, nil
}

func (*cancellingPDFOCRProvider) IsAvailable() bool { return true }
func (*cancellingPDFOCRProvider) Close()            {}

func (s *stubPDFOCRProvider) Recognize(data string) ([]OCRResult, error) {
	if _, err := base64.StdEncoding.DecodeString(data); err != nil {
		return nil, fmt.Errorf("expected base64 PNG: %w", err)
	}
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return []OCRResult{{Text: s.text}}, nil
}

func (*stubPDFOCRProvider) IsAvailable() bool { return true }
func (*stubPDFOCRProvider) Close()            {}

func TestRenderPDFPageForOCRRecoversRendererPanic(t *testing.T) {
	_, err := renderPDFPageForOCRWith(
		[]byte("fixture"),
		1,
		map[int]gopdf.PageInfo{1: {Width: 612, Height: 792}},
		func([]byte, int, gopdf.RenderOption) (image.Image, error) { panic("malformed XObject") },
	)
	if err == nil || !strings.Contains(err.Error(), "panicked") {
		t.Fatalf("renderer panic error = %v", err)
	}
}

func TestSafePDFPageSizesRecoversParserPanic(t *testing.T) {
	_, err := safePDFPageSizesWith([]byte("fixture"), func([]byte) (map[int]gopdf.PageInfo, error) {
		panic("malformed page tree")
	})
	if err == nil || !strings.Contains(err.Error(), "panicked") {
		t.Fatalf("page size parser panic error = %v", err)
	}
}

func TestExtractPDFOCRNodesScannedPage(t *testing.T) {
	path := t.TempDir() + "/scan.pdf"
	if err := os.WriteFile(path, buildOCRTestPDF(ocrTestImagePage()), 0o600); err != nil {
		t.Fatal(err)
	}
	ocr := &stubPDFOCRProvider{text: "识别出的扫描内容"}
	store := &SQLiteStore{pdfOCR: ocr}
	source := Source{ID: "scan", Title: "Scan", RelativePath: "scan.pdf"}
	result, err := store.extractPDFOCRNodes(context.Background(), source, path)
	if err != nil {
		t.Fatalf("extractPDFOCRNodes: %v", err)
	}
	if !result.required || ocr.calls != 1 || len(result.nodes) != 1 {
		t.Fatalf("unexpected OCR result: %#v calls=%d", result, ocr.calls)
	}
	node := result.nodes[0]
	if node.Text != "识别出的扫描内容" || node.Page != 1 || node.Metadata[metaPDFTextSource] != "ocr" || node.Metadata[metaPDFType] != string(pdfinspector.Scanned) {
		t.Fatalf("unexpected OCR node: %#v", node)
	}
}

func TestExtractPDFOCRNodesTextPDFSkipsOCR(t *testing.T) {
	path := t.TempDir() + "/text.pdf"
	if err := os.WriteFile(path, buildOCRTestPDF(ocrTestTextPage("Alpha", "Bravo", "Charlie")), 0o600); err != nil {
		t.Fatal(err)
	}
	ocr := &stubPDFOCRProvider{text: "must not be used"}
	store := &SQLiteStore{pdfOCR: ocr}
	result, err := store.extractPDFOCRNodes(context.Background(), Source{ID: "text"}, path)
	if err != nil {
		t.Fatalf("extractPDFOCRNodes: %v", err)
	}
	if result.required || len(result.nodes) != 0 || ocr.calls != 0 {
		t.Fatalf("text PDF unexpectedly invoked OCR: %#v calls=%d", result, ocr.calls)
	}
}

func TestExtractPDFOCRNodesSkipsStructuralBlankPage(t *testing.T) {
	path := t.TempDir() + "/text-with-blank.pdf"
	if err := os.WriteFile(path, buildOCRTestPDF(
		ocrTestTextPage("A long native text line that should remain searchable without OCR."),
		ocrTestPage{},
	), 0o600); err != nil {
		t.Fatal(err)
	}
	ocr := &stubPDFOCRProvider{text: "must not be used"}
	result, err := (&SQLiteStore{pdfOCR: ocr}).extractPDFOCRNodes(context.Background(), Source{ID: "text"}, path)
	if err != nil {
		t.Fatalf("extractPDFOCRNodes: %v", err)
	}
	if result.required || result.detection.OCRRecommended || len(result.detection.PagesNeedingOCR) != 0 || ocr.calls != 0 {
		t.Fatalf("structural blank page unexpectedly invoked OCR: %#v calls=%d", result, ocr.calls)
	}
}

func TestExtractPDFOCRNodesRejectsEntirelyBlankPDF(t *testing.T) {
	path := t.TempDir() + "/blank.pdf"
	if err := os.WriteFile(path, buildOCRTestPDF(ocrTestPage{}), 0o600); err != nil {
		t.Fatal(err)
	}
	ocr := &stubPDFOCRProvider{text: "must not be used"}
	result, err := (&SQLiteStore{pdfOCR: ocr}).extractPDFOCRNodes(context.Background(), Source{ID: "blank"}, path)
	if err == nil || !strings.Contains(err.Error(), "no readable content") || result.required || ocr.calls != 0 {
		t.Fatalf("blank PDF must fail without OCR: result=%#v err=%v calls=%d", result, err, ocr.calls)
	}
}

func TestPDFHasNoRecoverableContentPreservesNativeFallbackText(t *testing.T) {
	detection := pdfinspector.Result{
		PageCount: 1,
		Pages:     []pdfinspector.PageResult{{Page: 1, Classification: "empty"}},
	}
	if pdfHasNoRecoverableContent(detection, []DocumentNode{{Page: 1, Text: "text parsed by the native reader"}}) {
		t.Fatal("native text must prevent an inspector false negative from rejecting the PDF")
	}
	if !pdfHasNoRecoverableContent(detection, nil) {
		t.Fatal("an entirely blank PDF should still be rejected")
	}
}

func TestExtractPDFOCRNodesNativeFailureSkipsStructuralBlankPage(t *testing.T) {
	path := t.TempDir() + "/scan-with-blank.pdf"
	if err := os.WriteFile(path, buildOCRTestPDF(ocrTestImagePage(), ocrTestPage{}), 0o600); err != nil {
		t.Fatal(err)
	}
	ocr := &stubPDFOCRProvider{text: "OCR scanned page"}
	result, err := (&SQLiteStore{pdfOCR: ocr}).extractPDFOCRNodesWithNativeFallback(
		context.Background(), Source{ID: "scan"}, path, nil, fmt.Errorf("native extractor failed"), true,
	)
	if err != nil {
		t.Fatalf("extractPDFOCRNodesWithNativeFallback: %v", err)
	}
	if !result.required || len(result.detection.PagesNeedingOCR) != 1 || result.detection.PagesNeedingOCR[0] != 1 || len(result.nodes) != 1 || ocr.calls != 1 {
		t.Fatalf("native fallback should OCR only the content page: %#v calls=%d", result, ocr.calls)
	}
	if result.detection.Pages[1].NeedsOCR || result.detection.Pages[1].OCRReason != "" {
		t.Fatalf("structural blank page was incorrectly rerouted: %#v", result.detection.Pages[1])
	}
}

func TestMergePDFNodesLeavesTextPDFNativeNodesUntouched(t *testing.T) {
	native := DocumentNode{Page: 1, Text: "Native page", Metadata: map[string]string{"format": SourceKindPDF}}
	merged, err := mergePDFNodes([]DocumentNode{native}, nil, pdfOCRExtraction{
		detection: pdfinspector.Result{PDFType: pdfinspector.TextBased},
	})
	if err != nil || len(merged) != 1 || merged[0].Text != native.Text || merged[0].Metadata[metaPDFTextSource] != "" {
		t.Fatalf("text-only PDF nodes changed unexpectedly: %#v, %v", merged, err)
	}
}

func TestRouteMissingNativePDFTextPageToOCR(t *testing.T) {
	detection := pdfinspector.Result{
		PDFType:         pdfinspector.Mixed,
		PagesWithText:   2,
		PagesNeedingOCR: []int{3},
		Pages: []pdfinspector.PageResult{
			{Page: 1, Classification: pdfinspector.TextBased},
			{Page: 2, Classification: pdfinspector.TextBased},
			{Page: 3, Classification: pdfinspector.Scanned, NeedsOCR: true, OCRReason: "scanned"},
		},
	}
	routed := routeMissingNativePDFTextPagesToOCR(detection, []DocumentNode{{Page: 1, Text: "only first page extracted"}}, nil)
	if len(routed.PagesNeedingOCR) != 2 || routed.PagesNeedingOCR[0] != 2 || routed.PagesNeedingOCR[1] != 3 || !routed.OCRRecommended || routed.Pages[1].OCRReason != "native_text_unavailable" {
		t.Fatalf("missing native page was not routed to OCR: %#v", routed)
	}
	merged, err := mergePDFNodes([]DocumentNode{{Page: 1, Text: "native"}}, nil, pdfOCRExtraction{
		required:  true,
		detection: routed,
		nodes: []DocumentNode{
			{Page: 2, Text: "OCR fallback", Metadata: map[string]string{metaPDFTextSource: "ocr"}},
			{Page: 3, Text: "scan OCR", Metadata: map[string]string{metaPDFTextSource: "ocr"}},
		},
	})
	if err != nil || len(merged) != 3 || merged[1].Text != "OCR fallback" || merged[2].Text != "scan OCR" {
		t.Fatalf("merged fallback pages = %#v, %v", merged, err)
	}
}

func TestMergePDFNodesOnlySuppressesNativeErrorWhenOCROwnsEveryPage(t *testing.T) {
	nativeErr := fmt.Errorf("native extractor failed")
	partial, err := mergePDFNodes(nil, nativeErr, pdfOCRExtraction{
		required:  true,
		detection: pdfinspector.Result{PageCount: 2, PagesNeedingOCR: []int{1}},
		nodes:     []DocumentNode{{Page: 1, Text: "OCR page one"}},
	})
	if err == nil || len(partial) != 1 {
		t.Fatalf("partial OCR must retain native error: nodes=%#v err=%v", partial, err)
	}
	complete, err := mergePDFNodes(nil, nativeErr, pdfOCRExtraction{
		required:  true,
		detection: pdfinspector.Result{PageCount: 2, PagesNeedingOCR: []int{1, 2}},
		nodes: []DocumentNode{
			{Page: 1, Text: "OCR page one"},
			{Page: 2, Text: "OCR page two"},
		},
	})
	if err != nil || len(complete) != 2 {
		t.Fatalf("full OCR should recover native failure: nodes=%#v err=%v", complete, err)
	}
}

func TestMergePDFNodesSuppressesNativeErrorWhenOCROwnsAllContentPages(t *testing.T) {
	nativeErr := fmt.Errorf("native extractor failed")
	merged, err := mergePDFNodes(nil, nativeErr, pdfOCRExtraction{
		required: true,
		detection: pdfinspector.Result{
			PageCount:       2,
			PagesNeedingOCR: []int{1},
			OCRRecommended:  true,
			Pages: []pdfinspector.PageResult{
				{Page: 1, Classification: pdfinspector.Scanned, NeedsOCR: true, OCRReason: "scanned", Images: 1},
				{Page: 2, Classification: "empty"},
			},
		},
		nodes: []DocumentNode{{Page: 1, Text: "OCR page one"}},
	})
	if err != nil || len(merged) != 1 || merged[0].Text != "OCR page one" {
		t.Fatalf("OCR coverage of all non-empty pages should recover native failure: nodes=%#v err=%v", merged, err)
	}
}

func TestMergePDFNodesSuppressesNativeErrorWhenNativeAndOCROwnContentPages(t *testing.T) {
	nativeErr := fmt.Errorf("native extractor reported a trailing error")
	detection := pdfinspector.Result{
		PageCount:       2,
		PagesWithText:   1,
		PagesNeedingOCR: []int{2},
		Pages: []pdfinspector.PageResult{
			{Page: 1, Classification: pdfinspector.TextBased},
			{Page: 2, Classification: pdfinspector.Scanned, NeedsOCR: true, OCRReason: "scanned", Images: 1},
		},
	}
	merged, err := mergePDFNodes([]DocumentNode{{Page: 1, Text: "native page one"}}, nativeErr, pdfOCRExtraction{
		required:  true,
		detection: detection,
		nodes:     []DocumentNode{{Page: 2, Text: "OCR page two"}},
	})
	if err != nil || len(merged) != 2 || merged[0].Text != "native page one" || merged[1].Text != "OCR page two" {
		t.Fatalf("complete native/OCR coverage should recover native failure: nodes=%#v err=%v", merged, err)
	}

	merged, err = mergePDFNodes(nil, nativeErr, pdfOCRExtraction{
		required:  true,
		detection: detection,
		nodes:     []DocumentNode{{Page: 2, Text: "OCR page two"}},
	})
	if err == nil || len(merged) != 1 {
		t.Fatalf("missing native text page must retain native error: nodes=%#v err=%v", merged, err)
	}
}

func TestMergePDFNodesRestoresPageOrderWithoutMutatingNativeMetadata(t *testing.T) {
	native := []DocumentNode{
		{Page: 1, Text: "native page one", Metadata: map[string]string{"format": SourceKindPDF}},
		{Page: 3, Text: "native page three", Metadata: map[string]string{"format": SourceKindPDF}},
	}
	merged, err := mergePDFNodes(native, nil, pdfOCRExtraction{
		required: true,
		detection: pdfinspector.Result{
			PDFType:       pdfinspector.Mixed,
			PagesWithText: 2,
			Pages: []pdfinspector.PageResult{
				{Page: 1, Classification: pdfinspector.TextBased},
				{Page: 2, Classification: pdfinspector.Scanned, NeedsOCR: true, OCRReason: "scanned", Images: 1},
				{Page: 3, Classification: pdfinspector.TextBased},
			},
		},
		nodes: []DocumentNode{{Page: 2, Text: "OCR page two"}},
	})
	if err != nil || len(merged) != 3 || merged[0].Page != 1 || merged[1].Page != 2 || merged[2].Page != 3 {
		t.Fatalf("mixed nodes must be page ordered: %#v, %v", merged, err)
	}
	if native[0].Metadata[metaPDFTextSource] != "" || native[0].Metadata[metaPDFType] != "" {
		t.Fatalf("merge mutated caller-owned native metadata: %#v", native[0].Metadata)
	}
}

func TestExtractPDFOCRNodesMixedPDFAddsOnlyScannedPage(t *testing.T) {
	path := t.TempDir() + "/mixed.pdf"
	if err := os.WriteFile(path, buildOCRTestPDF(ocrTestTextPage("A long native text line that should stay native."), ocrTestImagePage()), 0o600); err != nil {
		t.Fatal(err)
	}
	ocr := &stubPDFOCRProvider{text: "OCR only page two"}
	store := &SQLiteStore{pdfOCR: ocr}
	result, err := store.extractPDFOCRNodes(context.Background(), Source{ID: "mixed", Title: "Mixed"}, path)
	if err != nil {
		t.Fatalf("extractPDFOCRNodes: %v", err)
	}
	if !result.required || ocr.calls != 1 || len(result.nodes) != 1 || result.nodes[0].Page != 2 {
		t.Fatalf("unexpected mixed OCR result: %#v calls=%d", result, ocr.calls)
	}
}

func TestExtractPDFOCRNodesUnavailableForScan(t *testing.T) {
	path := t.TempDir() + "/scan.pdf"
	if err := os.WriteFile(path, buildOCRTestPDF(ocrTestImagePage()), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := (&SQLiteStore{}).extractPDFOCRNodes(context.Background(), Source{ID: "scan"}, path)
	if err == nil || !strings.Contains(err.Error(), "built-in OCR engine is unavailable") {
		t.Fatalf("unexpected unavailable OCR error: %v", err)
	}
}

func TestExtractPDFOCRNodesHonorsCancelledContextBeforeInspection(t *testing.T) {
	path := t.TempDir() + "/scan.pdf"
	if err := os.WriteFile(path, buildOCRTestPDF(ocrTestImagePage()), 0o600); err != nil {
		t.Fatal(err)
	}
	ocr := &stubPDFOCRProvider{text: "must not be called"}
	store := &SQLiteStore{}
	store.SetPDFOCRProvider(ocr)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := store.extractPDFOCRNodes(ctx, Source{ID: "scan"}, path)
	if err != context.Canceled || ocr.calls != 0 {
		t.Fatalf("cancelled extraction = %v, OCR calls=%d", err, ocr.calls)
	}
}

func TestExtractPDFOCRNodesHonorsCancellationDuringRecognition(t *testing.T) {
	path := t.TempDir() + "/scan.pdf"
	if err := os.WriteFile(path, buildOCRTestPDF(ocrTestImagePage()), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ocr := &cancellingPDFOCRProvider{cancel: cancel}
	result, err := (&SQLiteStore{pdfOCR: ocr}).extractPDFOCRNodes(ctx, Source{ID: "scan"}, path)
	if err != context.Canceled || len(result.nodes) != 0 || ocr.calls != 1 {
		t.Fatalf("cancellation during OCR = result=%#v err=%v calls=%d", result, err, ocr.calls)
	}
}

func TestExtractPDFOCRNodesPrioritizesCancellationOverRecognitionError(t *testing.T) {
	path := t.TempDir() + "/scan.pdf"
	if err := os.WriteFile(path, buildOCRTestPDF(ocrTestImagePage()), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ocr := &cancellingPDFOCRProvider{cancel: cancel, err: fmt.Errorf("OCR shutdown")}
	result, err := (&SQLiteStore{pdfOCR: ocr}).extractPDFOCRNodes(ctx, Source{ID: "scan"}, path)
	if err != context.Canceled || len(result.nodes) != 0 || ocr.calls != 1 {
		t.Fatalf("cancellation must win over OCR error: result=%#v err=%v calls=%d", result, err, ocr.calls)
	}
}

func TestImportFilesPDFOCRPreservesNativeAndOCRPages(t *testing.T) {
	root := t.TempDir()
	path := root + "/mixed.pdf"
	if err := os.WriteFile(path, buildOCRTestPDF(
		ocrTestTextPage("Native text on page one remains searchable."),
		ocrTestImagePage(),
	), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(root + "/knowledge.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.SetPDFOCRProvider(&stubPDFOCRProvider{text: "OCR text on page two"})

	result, err := store.ImportFiles(context.Background(), DirectoryImportRequest{
		RootPath: root, IncludeExts: []string{".pdf"}, DistillMode: DistillModeRules,
	}, []string{path})
	if err != nil {
		t.Fatalf("ImportFiles: %v", err)
	}
	if result.ImportedFiles != 1 || result.FailedFiles != 0 {
		t.Fatalf("unexpected import result: %#v", result)
	}
	sources, err := store.ListSources(context.Background(), ListSourcesOptions{Limit: 10})
	if err != nil || len(sources) != 1 {
		t.Fatalf("ListSources = %#v, %v", sources, err)
	}
	nodes, err := store.ListNodesBySource(context.Background(), sources[0].ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	var native, ocr DocumentNode
	for _, node := range nodes {
		switch node.Metadata[metaPDFTextSource] {
		case "native":
			native = node
		case "ocr":
			ocr = node
		}
	}
	if native.Page != 1 || !strings.Contains(native.Text, "Native text") || native.Metadata[metaPDFType] != string(pdfinspector.Mixed) {
		t.Fatalf("native page missing or incorrectly tagged: %#v", native)
	}
	if ocr.Page != 2 || ocr.Text != "OCR text on page two" || ocr.Metadata[metaPDFOCRReason] != "scanned" {
		t.Fatalf("OCR page missing or incorrectly tagged: %#v", ocr)
	}
}

func TestPDFImportSnapshotKeepsNativeTextAndOCROnOneVersion(t *testing.T) {
	root := t.TempDir()
	path := root + "/replace-between-native-and-ocr.pdf"
	if err := os.WriteFile(path, buildOCRTestPDF(
		ocrTestTextPage("Native text from the verified PDF snapshot."),
		ocrTestImagePage(),
	), 0o600); err != nil {
		t.Fatal(err)
	}
	source := Source{ID: "snapshot-pdf", Kind: SourceKindPDF, Title: "Snapshot PDF", RelativePath: "replace-between-native-and-ocr.pdf"}
	parsed, err := parseDocumentNodesForOfficeReadImport(source, path, SourceKindPDF)
	if err != nil || parsed == nil || parsed.input == nil || len(parsed.nodes) != 1 {
		if parsed != nil {
			parsed.close()
		}
		t.Fatalf("parse PDF snapshot = %#v, %v", parsed, err)
	}
	defer parsed.close()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	ocr := &stubPDFOCRProvider{text: "OCR text from the same verified snapshot."}
	result, err := (&SQLiteStore{pdfOCR: ocr}).extractPDFOCRNodesWithNativeFallback(
		context.Background(), source, parsed.input.path, parsed.nodes, nil, true,
	)
	if err != nil {
		t.Fatalf("OCR from PDF snapshot: %v", err)
	}
	merged, err := mergePDFNodes(parsed.nodes, nil, result)
	if err != nil || len(merged) != 2 || !strings.Contains(merged[0].Text, "Native text from the verified PDF snapshot") || merged[1].Text != "OCR text from the same verified snapshot." {
		t.Fatalf("merged snapshot PDF nodes = %#v, %v", merged, err)
	}
}

func TestImportFilesPDFWithStructuralBlankPageDoesNotRequireOCR(t *testing.T) {
	root := t.TempDir()
	path := root + "/text-with-blank.pdf"
	if err := os.WriteFile(path, buildOCRTestPDF(
		ocrTestTextPage("Native text remains searchable when followed by a blank separator page."),
		ocrTestPage{},
	), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(root + "/knowledge.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	result, err := store.ImportFiles(context.Background(), DirectoryImportRequest{
		RootPath: root, IncludeExts: []string{".pdf"}, DistillMode: DistillModeRules,
	}, []string{path})
	if err != nil {
		t.Fatalf("ImportFiles: %v", err)
	}
	if result.ImportedFiles != 1 || result.FailedFiles != 0 {
		t.Fatalf("blank structural page should not require OCR: %#v", result)
	}
	sources, err := store.ListSources(context.Background(), ListSourcesOptions{Limit: 10})
	if err != nil || len(sources) != 1 {
		t.Fatalf("ListSources = %#v, %v", sources, err)
	}
	nodes, err := store.ListNodesBySource(context.Background(), sources[0].ID, 20)
	if err != nil || len(nodes) != 1 || !strings.Contains(nodes[0].Text, "Native text remains searchable") {
		t.Fatalf("native PDF nodes = %#v, %v", nodes, err)
	}
}

func TestImportFilesEntirelyBlankPDFFailsWithoutCreatingSearchableSource(t *testing.T) {
	root := t.TempDir()
	path := root + "/blank.pdf"
	if err := os.WriteFile(path, buildOCRTestPDF(ocrTestPage{}), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(root + "/knowledge.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	result, err := store.ImportFiles(context.Background(), DirectoryImportRequest{
		RootPath: root, IncludeExts: []string{".pdf"}, DistillMode: DistillModeRules,
	}, []string{path})
	if err != nil {
		t.Fatalf("ImportFiles: %v", err)
	}
	if result.ImportedFiles != 0 || result.FailedFiles != 1 || len(result.FailedItems) != 1 || !strings.Contains(result.FailedItems[0].Error, "no readable content") {
		t.Fatalf("blank PDF should be reported as failed: %#v", result)
	}
	sources, err := store.ListSources(context.Background(), ListSourcesOptions{Limit: 10})
	if err != nil || len(sources) != 1 || sources[0].Status != StatusFailed || sources[0].NodeCount != 0 {
		t.Fatalf("blank PDF source state = %#v, %v", sources, err)
	}
}

func TestImportFilesPDFOCRFailsWhenOCRReturnsNoText(t *testing.T) {
	root := t.TempDir()
	path := root + "/scan.pdf"
	if err := os.WriteFile(path, buildOCRTestPDF(ocrTestImagePage()), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(root + "/knowledge.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.SetPDFOCRProvider(&stubPDFOCRProvider{})

	result, err := store.ImportFiles(context.Background(), DirectoryImportRequest{
		RootPath: root, IncludeExts: []string{".pdf"}, DistillMode: DistillModeRules,
	}, []string{path})
	if err != nil {
		t.Fatalf("ImportFiles: %v", err)
	}
	if result.ImportedFiles != 0 || result.FailedFiles != 1 || len(result.FailedItems) != 1 || !strings.Contains(result.FailedItems[0].Error, "returned no readable text") {
		t.Fatalf("empty OCR result was not reported as an import failure: %#v", result)
	}
}

func TestRefreshSourcePDFOCRKeepsScannedContent(t *testing.T) {
	root := t.TempDir()
	path := root + "/scan.pdf"
	if err := os.WriteFile(path, buildOCRTestPDF(ocrTestImagePage()), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(root + "/knowledge.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ocr := &stubPDFOCRProvider{text: "scanned page content"}
	store.SetPDFOCRProvider(ocr)
	request := DirectoryImportRequest{RootPath: root, IncludeExts: []string{".pdf"}, DistillMode: DistillModeRules}
	if result, err := store.ImportFiles(context.Background(), request, []string{path}); err != nil || result.ImportedFiles != 1 {
		t.Fatalf("ImportFiles = %#v, %v", result, err)
	}
	sources, err := store.ListSources(context.Background(), ListSourcesOptions{Limit: 10})
	if err != nil || len(sources) != 1 {
		t.Fatalf("ListSources = %#v, %v", sources, err)
	}
	preview, err := store.PreviewSourceRefresh(context.Background(), sources[0].ID)
	if err != nil || preview.Error != "" || preview.NewNodeCount != 1 {
		t.Fatalf("PreviewSourceRefresh = %#v, %v", preview, err)
	}
	refreshed, err := store.RefreshSource(context.Background(), sources[0].ID)
	if err != nil {
		t.Fatalf("RefreshSource: %v", err)
	}
	if refreshed.Status != StatusParsed || ocr.calls != 3 {
		t.Fatalf("unexpected refreshed source or OCR count: %#v calls=%d", refreshed, ocr.calls)
	}
	nodes, err := store.ListNodesBySource(context.Background(), refreshed.ID, 20)
	if err != nil || len(nodes) != 1 || nodes[0].Text != "scanned page content" || nodes[0].Metadata[metaPDFTextSource] != "ocr" {
		t.Fatalf("refreshed OCR nodes = %#v, %v", nodes, err)
	}
}

func TestRefreshSourcePDFOCRErrorPreservesExistingContent(t *testing.T) {
	root := t.TempDir()
	path := root + "/scan.pdf"
	if err := os.WriteFile(path, buildOCRTestPDF(ocrTestImagePage()), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(root + "/knowledge.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.SetPDFOCRProvider(&stubPDFOCRProvider{text: "previous OCR content"})
	request := DirectoryImportRequest{RootPath: root, IncludeExts: []string{".pdf"}, DistillMode: DistillModeRules}
	if result, err := store.ImportFiles(context.Background(), request, []string{path}); err != nil || result.ImportedFiles != 1 {
		t.Fatalf("ImportFiles = %#v, %v", result, err)
	}
	sources, err := store.ListSources(context.Background(), ListSourcesOptions{Limit: 10})
	if err != nil || len(sources) != 1 {
		t.Fatalf("ListSources = %#v, %v", sources, err)
	}
	before := sources[0]
	beforeNodes, err := store.ListNodesBySource(context.Background(), before.ID, 20)
	if err != nil || len(beforeNodes) != 1 {
		t.Fatalf("ListNodesBySource before refresh = %#v, %v", beforeNodes, err)
	}

	store.SetPDFOCRProvider(&stubPDFOCRProvider{err: fmt.Errorf("OCR temporarily unavailable")})
	if _, err := store.RefreshSource(context.Background(), before.ID); err == nil || !strings.Contains(err.Error(), "OCR temporarily unavailable") {
		t.Fatalf("RefreshSource error = %v, want OCR error", err)
	}

	after, err := store.GetSource(context.Background(), before.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != before.Status || after.ContentHash != before.ContentHash || after.ErrorMessage != before.ErrorMessage || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("failed refresh modified source: before=%#v after=%#v", before, after)
	}
	afterNodes, err := store.ListNodesBySource(context.Background(), before.ID, 20)
	if err != nil || len(afterNodes) != 1 || afterNodes[0].Text != "previous OCR content" || afterNodes[0].Metadata[metaPDFTextSource] != "ocr" {
		t.Fatalf("failed refresh modified nodes: %#v, %v", afterNodes, err)
	}
}

func TestReimportFilesPDFOCRErrorPreservesExistingContent(t *testing.T) {
	root := t.TempDir()
	path := root + "/scan.pdf"
	if err := os.WriteFile(path, buildOCRTestPDF(ocrTestImagePage()), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(root + "/knowledge.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.SetPDFOCRProvider(&stubPDFOCRProvider{text: "previous OCR content"})
	request := DirectoryImportRequest{RootPath: root, IncludeExts: []string{".pdf"}, DistillMode: DistillModeRules}
	if result, err := store.ImportFiles(context.Background(), request, []string{path}); err != nil || result.ImportedFiles != 1 {
		t.Fatalf("first ImportFiles = %#v, %v", result, err)
	}
	sources, err := store.ListSources(context.Background(), ListSourcesOptions{Limit: 10})
	if err != nil || len(sources) != 1 {
		t.Fatalf("ListSources = %#v, %v", sources, err)
	}
	before := sources[0]
	beforeNodes, err := store.ListNodesBySource(context.Background(), before.ID, 20)
	if err != nil || len(beforeNodes) != 1 {
		t.Fatalf("ListNodesBySource before reimport = %#v, %v", beforeNodes, err)
	}
	// Keep the document valid while ensuring hash de-duplication does not skip
	// the reimport attempt.
	updatedPDF := buildOCRTestPDF(ocrTestImagePage(), ocrTestImagePage())
	if err := os.WriteFile(path, updatedPDF, 0o600); err != nil {
		t.Fatal(err)
	}
	store.SetPDFOCRProvider(&stubPDFOCRProvider{err: fmt.Errorf("OCR temporarily unavailable")})
	result, err := store.ImportFiles(context.Background(), request, []string{path})
	if err != nil {
		t.Fatalf("second ImportFiles: %v", err)
	}
	if result.ImportedFiles != 0 || result.FailedFiles != 1 || len(result.FailedItems) != 1 || !strings.Contains(result.FailedItems[0].Error, "OCR temporarily unavailable") {
		t.Fatalf("failed reimport result = %#v", result)
	}

	after, err := store.GetSource(context.Background(), before.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != before.Status || after.ContentHash != before.ContentHash || after.ErrorMessage != before.ErrorMessage || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("failed reimport modified source: before=%#v after=%#v", before, after)
	}
	afterNodes, err := store.ListNodesBySource(context.Background(), before.ID, 20)
	if err != nil || len(afterNodes) != 1 || afterNodes[0].Text != "previous OCR content" || afterNodes[0].Metadata[metaPDFTextSource] != "ocr" {
		t.Fatalf("failed reimport modified nodes: %#v, %v", afterNodes, err)
	}
}

func TestPDFOCRProviderCanBeReconfiguredDuringExtraction(t *testing.T) {
	path := t.TempDir() + "/scan.pdf"
	if err := os.WriteFile(path, buildOCRTestPDF(ocrTestImagePage()), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &SQLiteStore{}
	providerA := &stubPDFOCRProvider{text: "OCR A"}
	providerB := &stubPDFOCRProvider{text: "OCR B"}
	store.SetPDFOCRProvider(providerA)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			if i%2 == 0 {
				store.SetPDFOCRProvider(providerA)
			} else {
				store.SetPDFOCRProvider(providerB)
			}
		}
	}()
	for i := 0; i < 10; i++ {
		result, err := store.extractPDFOCRNodes(context.Background(), Source{ID: "scan", Title: "Scan"}, path)
		if err != nil || len(result.nodes) != 1 {
			t.Fatalf("extractPDFOCRNodes = %#v, %v", result, err)
		}
	}
	<-done
}

type ocrTestPage struct {
	resources string
	content   string
	extra     []string
}

func ocrTestTextPage(lines ...string) ocrTestPage {
	var content strings.Builder
	content.WriteString("BT /F1 12 Tf 72 720 Td ")
	for _, line := range lines {
		fmt.Fprintf(&content, "(%s) Tj 0 -16 Td ", line)
	}
	content.WriteString("ET")
	return ocrTestPage{resources: "/Font << /F1 3 0 R >>", content: content.String()}
}

func ocrTestImagePage() ocrTestPage {
	return ocrTestPage{
		resources: "/XObject << /Im1 0 0 R >>",
		content:   "q 200 0 0 200 0 0 cm /Im1 Do Q",
		extra:     []string{"<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Length 3 >>\nstream\n\xFF\xFF\xFF\nendstream"},
	}
}

func buildOCRTestPDF(pages ...ocrTestPage) []byte {
	pageNums := make([]int, len(pages))
	next := 4
	for i, page := range pages {
		pageNums[i] = next
		next += 2 + len(page.extra)
	}
	refs := make([]string, len(pageNums))
	for i, n := range pageNums {
		refs[i] = fmt.Sprintf("%d 0 R", n)
	}
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [" + strings.Join(refs, " ") + "] /Count " + strconv.Itoa(len(pages)) + " >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	for i, page := range pages {
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
