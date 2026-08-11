package knowledge

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/pdfinspector"
	gopdf "github.com/VantageDataChat/GoPDF2"
)

const (
	metaPDFType       = "pdf_type"
	metaPDFTextSource = "text_source"
	metaPDFOCRReason  = "ocr_reason"
)

const (
	pdfOCRRenderDPI    = 120.0
	pdfOCRMinRenderDPI = 72.0
	// Keeping one rendered OCR page below 12 MP bounds the RGBA working image
	// to about 48 MiB before PNG/base64 overhead. Pages are processed one at a
	// time so a large PDF cannot retain every rendered page in memory.
	maxPDFOCRRenderPixels = 12_000_000.0
)

// pdfOCRExtraction is kept internal because the import pipeline needs both
// the text nodes and the routing decision when native text extraction fails.
type pdfOCRExtraction struct {
	required  bool
	detection pdfinspector.Result
	nodes     []DocumentNode
}

// pdfHasNoRecoverableContent distinguishes an intentionally blank PDF from a
// failed text/OCR extraction. Inspector and native extraction use different
// pure-Go paths, so successful native nodes take precedence over a conservative
// inspector result with no text/image evidence.
func pdfHasNoRecoverableContent(detection pdfinspector.Result, nativeNodes []DocumentNode) bool {
	for _, node := range nativeNodes {
		if strings.TrimSpace(node.Text) != "" {
			return false
		}
	}
	return detection.PageCount > 0 && len(detection.Pages) == detection.PageCount && len(detection.PagesNeedingOCR) == 0 && detection.PagesWithText == 0 && detection.PagesWithImages == 0
}

// extractPDFOCRNodes renders only the pages selected by pdfinspector and sends
// them to the configured native OCR provider. It deliberately does not OCR
// text-based pages, which keeps normal PDF imports on the fast native path.
func (s *SQLiteStore) extractPDFOCRNodes(ctx context.Context, source Source, filePath string) (pdfOCRExtraction, error) {
	return s.extractPDFOCRNodesWithNativeFallback(ctx, source, filePath, nil, nil, false)
}

// extractPDFOCRNodesWithNativeFallback also routes a page to OCR when the PDF
// inspector found native text but the main text extractor did not produce a
// searchable node for that page. The inspector and reader use different pure-Go
// extraction paths; without this fallback a mixed PDF could silently lose such
// a page during merge.
func (s *SQLiteStore) extractPDFOCRNodesWithNativeFallback(ctx context.Context, source Source, filePath string, nativeNodes []DocumentNode, nativeErr error, useNativeFallback bool) (pdfOCRExtraction, error) {
	if err := ctx.Err(); err != nil {
		return pdfOCRExtraction{}, err
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return pdfOCRExtraction{}, fmt.Errorf("read PDF for OCR: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return pdfOCRExtraction{}, err
	}
	detection, err := pdfinspector.Detect(data)
	if err != nil {
		return pdfOCRExtraction{}, fmt.Errorf("inspect PDF for OCR routing: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return pdfOCRExtraction{}, err
	}
	if useNativeFallback {
		detection = routeMissingNativePDFTextPagesToOCR(detection, nativeNodes, nativeErr)
	}
	// An entirely blank page has no content for either native extraction or OCR.
	// pdfinspector intentionally routes it conservatively so generic callers can
	// decide how to handle it, but treating a structural separator as an OCR
	// requirement makes an otherwise valid import fail on the expected empty
	// response.
	detection = skipStructurallyEmptyPDFPagesForOCR(detection)
	result := pdfOCRExtraction{required: len(detection.PagesNeedingOCR) > 0, detection: detection}
	if !result.required {
		if pdfHasNoRecoverableContent(detection, nativeNodes) {
			return result, fmt.Errorf("PDF has no readable content")
		}
		return result, nil
	}
	ocrProvider := s.currentPDFOCRProvider()
	if ocrProvider == nil || !ocrProvider.IsAvailable() {
		return result, fmt.Errorf("PDF requires local OCR, but the built-in OCR engine is unavailable")
	}

	pageSizes, err := safePDFPageSizes(data)
	if err != nil {
		return pdfOCRExtraction{}, fmt.Errorf("read PDF page sizes for OCR: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return pdfOCRExtraction{}, err
	}
	nodes := make([]DocumentNode, 0, len(detection.PagesNeedingOCR))
	for _, pageNumber := range detection.PagesNeedingOCR {
		if err := ctx.Err(); err != nil {
			return pdfOCRExtraction{}, err
		}
		pageImage, err := renderPDFPageForOCR(data, pageNumber, pageSizes)
		if err != nil {
			return pdfOCRExtraction{}, err
		}
		if err := ctx.Err(); err != nil {
			return pdfOCRExtraction{}, err
		}
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, pageImage); err != nil {
			return pdfOCRExtraction{}, fmt.Errorf("encode PDF page %d for OCR: %w", pageNumber, err)
		}
		if err := ctx.Err(); err != nil {
			return pdfOCRExtraction{}, err
		}
		ocrResults, err := ocrProvider.Recognize(base64.StdEncoding.EncodeToString(encoded.Bytes()))
		// OCRProvider cannot receive a context, but cancellation may have happened
		// while its synchronous recognition call was in flight. Never turn that
		// stale result into nodes or let an OCR error mask a cancelled import.
		if err := ctx.Err(); err != nil {
			return pdfOCRExtraction{}, err
		}
		if err != nil {
			return pdfOCRExtraction{}, fmt.Errorf("OCR PDF page %d: %w", pageNumber, err)
		}
		text := joinPDFOCRText(ocrResults)
		if text == "" {
			return pdfOCRExtraction{}, fmt.Errorf("OCR PDF page %d returned no readable text", pageNumber)
		}
		reason := pdfOCRReason(detection, pageNumber)
		nodes = append(nodes, DocumentNode{
			ID:       NewID("kdn"),
			SourceID: source.ID,
			Type:     "page",
			Title:    fmt.Sprintf("%s p.%d (OCR)", source.Title, pageNumber),
			Text:     text,
			Page:     pageNumber,
			Metadata: map[string]string{
				"relative_path":   source.RelativePath,
				"format":          SourceKindPDF,
				metaPDFType:       string(detection.PDFType),
				metaPDFTextSource: "ocr",
				metaPDFOCRReason:  reason,
			},
			TokenCount: estimateTokens(text),
		})
	}
	result.nodes = nodes
	return result, nil
}

func safePDFPageSizes(data []byte) (sizes map[int]gopdf.PageInfo, err error) {
	return safePDFPageSizesWith(data, gopdf.GetSourcePDFPageSizesFromBytes)
}

func safePDFPageSizesWith(data []byte, pageSizes func([]byte) (map[int]gopdf.PageInfo, error)) (sizes map[int]gopdf.PageInfo, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			sizes = nil
			err = fmt.Errorf("read PDF page sizes for OCR panicked: %v", recovered)
		}
	}()
	return pageSizes(data)
}

func routeMissingNativePDFTextPagesToOCR(detection pdfinspector.Result, nativeNodes []DocumentNode, nativeErr error) pdfinspector.Result {
	if len(detection.Pages) == 0 {
		return detection
	}
	nativePages := make(map[int]struct{}, len(nativeNodes))
	for _, node := range nativeNodes {
		if node.Page > 0 && strings.TrimSpace(node.Text) != "" {
			nativePages[node.Page] = struct{}{}
		}
	}
	for i := range detection.Pages {
		page := &detection.Pages[i]
		// A separator page has no recoverable content. In particular, do not
		// overwrite its no_text route when the native extractor failed for a
		// different page in the same document.
		if isStructurallyEmptyPDFPage(*page) {
			continue
		}
		if page.NeedsOCR {
			continue
		}
		_, found := nativePages[page.Page]
		if nativeErr != nil || (page.Classification == pdfinspector.TextBased && !found) {
			page.NeedsOCR = true
			page.OCRReason = "native_text_unavailable"
		}
	}
	return rebuildPDFOCRRouting(detection)
}

// skipStructurallyEmptyPDFPagesForOCR removes pages that have no text or
// images from the knowledge-base OCR route. This is deliberately narrower than
// the inspector's empty-page classification: a page with even weak text or an
// image may contain recoverable content and must still be sent to OCR.
func skipStructurallyEmptyPDFPagesForOCR(detection pdfinspector.Result) pdfinspector.Result {
	for i := range detection.Pages {
		page := &detection.Pages[i]
		if page.NeedsOCR && isStructurallyEmptyPDFPage(*page) {
			page.NeedsOCR = false
			page.OCRReason = ""
		}
	}
	return rebuildPDFOCRRouting(detection)
}

func isStructurallyEmptyPDFPage(page pdfinspector.PageResult) bool {
	return page.Classification == "empty" && page.Images == 0 && page.TextItems == 0
}

// rebuildPDFOCRRouting keeps the document-level route synchronized with the
// authoritative per-page flags after a pipeline-specific adjustment.
func rebuildPDFOCRRouting(detection pdfinspector.Result) pdfinspector.Result {
	if len(detection.Pages) == 0 {
		return detection
	}
	detection.PagesNeedingOCR = make([]int, 0, len(detection.Pages))
	for _, page := range detection.Pages {
		if page.NeedsOCR {
			detection.PagesNeedingOCR = append(detection.PagesNeedingOCR, page.Page)
		}
	}
	detection.OCRRecommended = len(detection.PagesNeedingOCR) > 0
	return detection
}

func renderPDFPageForOCR(data []byte, pageNumber int, sizes map[int]gopdf.PageInfo) (image.Image, error) {
	return renderPDFPageForOCRWith(data, pageNumber, sizes, gopdf.RenderPageToImage)
}

// renderPDFPageForOCRWith protects the importer boundary from renderer panics
// caused by malformed page resources. Rendering is only an OCR fallback, so a
// normal error is preferable to taking down an import transaction.
func renderPDFPageForOCRWith(data []byte, pageNumber int, sizes map[int]gopdf.PageInfo, render func([]byte, int, gopdf.RenderOption) (image.Image, error)) (img image.Image, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			img = nil
			err = fmt.Errorf("render PDF page %d for OCR panicked: %v", pageNumber, recovered)
		}
	}()
	page, ok := sizes[pageNumber]
	if !ok || page.Width <= 0 || page.Height <= 0 || math.IsNaN(page.Width) || math.IsNaN(page.Height) || math.IsInf(page.Width, 0) || math.IsInf(page.Height, 0) {
		return nil, fmt.Errorf("PDF page %d has invalid dimensions", pageNumber)
	}
	basePixels := page.Width * page.Height
	maxDPI := 72.0 * math.Sqrt(maxPDFOCRRenderPixels/basePixels)
	if maxDPI < pdfOCRMinRenderDPI {
		return nil, fmt.Errorf("PDF page %d is too large for local OCR at the minimum %.0f DPI", pageNumber, pdfOCRMinRenderDPI)
	}
	dpi := math.Min(pdfOCRRenderDPI, maxDPI)
	img, err = render(data, pageNumber-1, gopdf.RenderOption{DPI: dpi})
	if err != nil {
		return nil, fmt.Errorf("render PDF page %d for OCR: %w", pageNumber, err)
	}
	bounds := img.Bounds()
	if int64(bounds.Dx())*int64(bounds.Dy()) > int64(maxPDFOCRRenderPixels) {
		return nil, fmt.Errorf("PDF page %d render exceeds local OCR pixel limit", pageNumber)
	}
	return img, nil
}

func mergePDFNodes(nativeNodes []DocumentNode, nativeErr error, ocr pdfOCRExtraction) ([]DocumentNode, error) {
	if !ocr.required {
		return nativeNodes, nativeErr
	}
	textPages := make(map[int]struct{}, ocr.detection.PagesWithText)
	for _, page := range ocr.detection.Pages {
		if !page.NeedsOCR {
			textPages[page.Page] = struct{}{}
		} else {
			// A fallback route may override an inspector text-page decision. Do not
			// retain a stale PagesWithText entry and silently drop OCR output.
			delete(textPages, page.Page)
		}
	}
	merged := make([]DocumentNode, 0, len(nativeNodes)+len(ocr.nodes))
	for _, node := range nativeNodes {
		if _, keep := textPages[node.Page]; !keep {
			continue
		}
		if node.Metadata == nil {
			node.Metadata = map[string]string{}
		} else {
			node.Metadata = cloneStringMap(node.Metadata)
		}
		node.Metadata[metaPDFType] = string(ocr.detection.PDFType)
		node.Metadata[metaPDFTextSource] = "native"
		merged = append(merged, node)
	}
	merged = append(merged, ocr.nodes...)
	// Native extraction completes before OCR starts, so appending OCR nodes
	// directly would put a scanned page after later native pages in a mixed PDF
	// (p.1, p.3, p.2). Keep nodes page-ordered for card generation and context
	// retrieval while preserving the parser's order within a page.
	sort.SliceStable(merged, func(i, j int) bool {
		return pdfNodePageOrder(merged[i]) < pdfNodePageOrder(merged[j])
	})
	if nativeErr != nil && pdfExtractionCoversAllContentPages(nativeNodes, ocr) {
		return merged, nil
	}
	return merged, nativeErr
}

func pdfNodePageOrder(node DocumentNode) int {
	if node.Page > 0 {
		return node.Page
	}
	return math.MaxInt
}

func cloneStringMap(values map[string]string) map[string]string {
	clone := make(map[string]string, len(values)+2)
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

// pdfExtractionCoversAllContentPages reports whether the merged native/OCR
// result has usable text for each non-empty page. A native extractor can
// return an error after successfully producing some pages; that error is safe
// to suppress only when those native pages plus OCR pages cover the document.
func pdfExtractionCoversAllContentPages(nativeNodes []DocumentNode, ocr pdfOCRExtraction) bool {
	covered := make(map[int]struct{}, len(nativeNodes)+len(ocr.nodes))
	for _, node := range ocr.nodes {
		if node.Page > 0 && strings.TrimSpace(node.Text) != "" {
			covered[node.Page] = struct{}{}
		}
	}
	if len(ocr.detection.Pages) > 0 {
		nativePages := make(map[int]struct{}, len(ocr.detection.Pages))
		for _, page := range ocr.detection.Pages {
			if !page.NeedsOCR && !isStructurallyEmptyPDFPage(page) {
				nativePages[page.Page] = struct{}{}
			}
		}
		for _, node := range nativeNodes {
			if node.Page <= 0 || strings.TrimSpace(node.Text) == "" {
				continue
			}
			if _, expected := nativePages[node.Page]; expected {
				covered[node.Page] = struct{}{}
			}
		}
		contentPages := 0
		for _, page := range ocr.detection.Pages {
			if isStructurallyEmptyPDFPage(page) {
				continue
			}
			contentPages++
			if _, ok := covered[page.Page]; !ok {
				return false
			}
		}
		if contentPages == 0 {
			return false
		}
		return true
	}
	if len(ocr.detection.PagesNeedingOCR) == 0 || len(ocr.detection.PagesNeedingOCR) != ocr.detection.PageCount {
		return false
	}
	for _, page := range ocr.detection.PagesNeedingOCR {
		if _, ok := covered[page]; !ok {
			return false
		}
	}
	return true
}

func joinPDFOCRText(results []OCRResult) string {
	parts := make([]string, 0, len(results))
	for _, result := range results {
		if text := strings.TrimSpace(result.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func pdfOCRReason(result pdfinspector.Result, pageNumber int) string {
	for _, page := range result.Pages {
		if page.Page == pageNumber {
			return page.OCRReason
		}
	}
	return "scanned"
}
