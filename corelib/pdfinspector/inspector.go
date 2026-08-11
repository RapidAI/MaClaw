// Package pdfinspector classifies PDFs before they enter a text-extraction or
// OCR pipeline. It is deliberately pure Go: no native executables, OCR engine,
// Python runtime, or network service is required.
package pdfinspector

import (
	"bytes"
	"fmt"
	"os"
	"unicode"

	gopdf "github.com/VantageDataChat/GoPDF2"
)

// Type is the coarse PDF classification used to route a document.
type Type string

const (
	// TextBased has meaningful native text on every non-empty page that was
	// inspected. It can be sent directly to a text extractor.
	TextBased Type = "text_based"
	// Scanned has image-only pages and needs OCR for all of them.
	Scanned Type = "scanned"
	// ImageBased has images plus only a small native-text overlay, such as page
	// numbers. OCR is normally more useful than the overlay.
	ImageBased Type = "image_based"
	// Mixed contains both text pages and pages that need OCR.
	Mixed Type = "mixed"
)

// Options controls the conservative meaningful-text heuristic. Zero values use
// the same defaults as DefaultOptions.
type Options struct {
	// MinTextItems is the minimum count of extracted text fragments required for
	// a page to be considered text based. The default is 1. A single rich text
	// operation is common in valid PDFs, so short overlay rejection is handled
	// by the text-diversity heuristic as well.
	MinTextItems int
	// MinTextRunes is the minimum number of distinct non-space runes required
	// for a text page. The default is 5.
	MinTextRunes int
}

// DefaultOptions is calibrated to accept a single full text run while ignoring
// short page-number and decorative-text overlays on scanned PDFs.
var DefaultOptions = Options{MinTextItems: 1, MinTextRunes: 5}

// MaxPages is the upper bound for a single pure-Go inspection. Classification
// materializes per-page evidence and runs document-wide extractors, so reject
// pathological page trees before allocating attacker-controlled slices.
const MaxPages = 10_000

// PageResult records the evidence and routing decision for one 1-indexed PDF
// page. Classification is one of text_based, scanned, image_based, or empty.
type PageResult struct {
	Page             int    `json:"page"`
	Classification   Type   `json:"classification"`
	TextItems        int    `json:"text_items"`
	VisibleTextRunes int    `json:"visible_text_runes"`
	UniqueTextRunes  int    `json:"unique_text_runes"`
	Images           int    `json:"images"`
	NeedsOCR         bool   `json:"needs_ocr"`
	OCRReason        string `json:"ocr_reason,omitempty"`
}

// Result is the document-level result. Page numbers in PagesNeedingOCR are
// 1-indexed so callers can pass them directly to user-facing OCR APIs.
type Result struct {
	PDFType         Type         `json:"pdf_type"`
	PageCount       int          `json:"page_count"`
	PagesSampled    int          `json:"pages_sampled"`
	PagesWithText   int          `json:"pages_with_text"`
	PagesWithImages int          `json:"pages_with_images"`
	Confidence      float64      `json:"confidence"`
	OCRRecommended  bool         `json:"ocr_recommended"`
	PagesNeedingOCR []int        `json:"pages_needing_ocr"`
	Pages           []PageResult `json:"pages"`
}

// DetectFile reads and classifies a PDF from path.
func DetectFile(path string) (Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, fmt.Errorf("read PDF: %w", err)
	}
	return Detect(data)
}

// Detect classifies a PDF using DefaultOptions.
func Detect(data []byte) (Result, error) {
	return DetectWithOptions(data, DefaultOptions)
}

// DetectWithOptions classifies a PDF by inspecting each page's pure-Go text
// and image extraction results. A page is text-based only when its native text
// has enough independent fragments and character diversity; this keeps a tiny
// invisible OCR/page-number overlay from masking a scanned page.
func DetectWithOptions(data []byte, options Options) (Result, error) {
	return detectWithExtractors(data, options, gopdf.GetSourcePDFPageCountFromBytes, gopdf.ExtractTextFromAllPages, gopdf.ExtractImagesFromAllPages)
}

// detectWithExtractors makes PDF classification fail closed when an upstream
// parser panics on malformed content. It is kept separate from DetectWithOptions
// so the panic boundary can be tested without changing the public API.
func detectWithExtractors(data []byte, options Options, pageCountOf func([]byte) (int, error), extractText func([]byte) (map[int][]gopdf.ExtractedText, error), extractImages func([]byte) (map[int][]gopdf.ExtractedImage, error)) (result Result, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = Result{}
			err = fmt.Errorf("inspect PDF: parser panicked: %v", recovered)
		}
	}()
	options = normalizeOptions(options)
	if len(data) == 0 || !looksLikePDF(data) {
		return Result{}, fmt.Errorf("invalid PDF data")
	}

	pageCount, err := pageCountOf(data)
	if err != nil {
		return Result{}, fmt.Errorf("read PDF page tree: %w", err)
	}
	if pageCount < 1 {
		return Result{}, fmt.Errorf("PDF has no pages")
	}
	if pageCount > MaxPages {
		return Result{}, fmt.Errorf("PDF has too many pages (%d; maximum %d)", pageCount, MaxPages)
	}

	// Parse each representation once for the whole document. Calling the
	// single-page GoPDF2 helpers in this loop would rebuild their PDF parser for
	// every page, which turns classification of large documents quadratic.
	textsByPage, textErr := extractText(data)
	imagesByPage, imageErr := extractImages(data)
	if textErr != nil && imageErr != nil {
		return Result{}, fmt.Errorf("inspect PDF: text: %v; images: %v", textErr, imageErr)
	}
	result = Result{PageCount: pageCount, PagesSampled: pageCount, Pages: make([]PageResult, 0, pageCount)}
	var imageOnlyPages, weakOverlayPages, emptyPages int
	for pageIndex := 0; pageIndex < pageCount; pageIndex++ {
		texts := textsByPage[pageIndex]
		images := imagesByPage[pageIndex]

		visibleRunes, uniqueRunes := visibleTextStats(texts)
		page := PageResult{
			Page:             pageIndex + 1,
			TextItems:        len(texts),
			VisibleTextRunes: visibleRunes,
			UniqueTextRunes:  uniqueRunes,
			Images:           len(images),
		}
		if page.Images > 0 {
			result.PagesWithImages++
		}
		if isMeaningfulText(page, options) {
			page.Classification = TextBased
			result.PagesWithText++
		} else {
			switch {
			case imageErr != nil:
				// Text extraction can succeed even when an uncommon image filter
				// defeats the image inspector. Treat text-poor pages as needing OCR
				// instead of calling them empty: the latter would make the knowledge
				// pipeline skip a potentially scanned page entirely.
				page.Classification = ImageBased
				page.NeedsOCR = true
				page.OCRReason = "image_inspection_unavailable"
				weakOverlayPages++
			case page.Images > 0 && page.TextItems == 0:
				page.Classification = Scanned
				page.NeedsOCR = true
				page.OCRReason = "scanned"
				imageOnlyPages++
			case page.Images > 0:
				page.Classification = ImageBased
				page.NeedsOCR = true
				page.OCRReason = "image_based"
				weakOverlayPages++
			case page.TextItems > 0:
				// Vector/outlines or an unreliable native text layer. There is no
				// raster XObject to call it scanned, but OCR is still the safe route.
				page.Classification = ImageBased
				page.NeedsOCR = true
				page.OCRReason = "insufficient_text"
				weakOverlayPages++
			default:
				page.Classification = "empty"
				page.NeedsOCR = true
				page.OCRReason = "no_text"
				emptyPages++
			}
		}
		if page.NeedsOCR {
			result.PagesNeedingOCR = append(result.PagesNeedingOCR, page.Page)
		}
		result.Pages = append(result.Pages, page)
	}

	result.PDFType, result.Confidence = classify(result.PagesWithText, imageOnlyPages, weakOverlayPages, emptyPages, pageCount)
	result.OCRRecommended = len(result.PagesNeedingOCR) > 0
	return result, nil
}

func normalizeOptions(options Options) Options {
	if options.MinTextItems <= 0 {
		options.MinTextItems = DefaultOptions.MinTextItems
	}
	if options.MinTextRunes <= 0 {
		options.MinTextRunes = DefaultOptions.MinTextRunes
	}
	return options
}

func looksLikePDF(data []byte) bool {
	data = data[:min(len(data), 1024)]
	return bytes.Contains(data, []byte("%PDF-"))
}

func visibleTextStats(texts []gopdf.ExtractedText) (int, int) {
	seen := make(map[rune]struct{})
	visible := 0
	for _, item := range texts {
		for _, r := range item.Text {
			if !unicode.IsSpace(r) && !unicode.IsControl(r) {
				seen[r] = struct{}{}
				visible++
			}
		}
	}
	return visible, len(seen)
}

func isMeaningfulText(page PageResult, options Options) bool {
	if page.TextItems < options.MinTextItems || page.UniqueTextRunes < options.MinTextRunes {
		return false
	}
	// A page with several fragments is unlikely to be a page-number overlay.
	// A single fragment must be substantively longer: many generators emit an
	// entire normal page in one Tj/TJ operation, while scan overlays are short.
	return page.TextItems >= max(3, options.MinTextItems) || (page.VisibleTextRunes >= 20 && page.UniqueTextRunes >= options.MinTextRunes)
}

func classify(textPages, scannedPages, imageBasedPages, emptyPages, totalPages int) (Type, float64) {
	ocrPages := scannedPages + imageBasedPages
	if textPages > 0 && ocrPages > 0 {
		return Mixed, 0.70
	}
	if textPages > 0 {
		// Empty pages do not invalidate a document's native text route.
		return TextBased, float64(textPages) / float64(max(1, totalPages-emptyPages))
	}
	if imageBasedPages > 0 {
		return ImageBased, 0.80
	}
	// Includes true scans and structurally empty pages. The latter are routed
	// to OCR as a conservative fallback; callers can inspect Pages for detail.
	return Scanned, 0.95
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
