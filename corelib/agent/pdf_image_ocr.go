package agent

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	gopdf2 "github.com/VantageDataChat/GoPDF2"
	lpdf "github.com/ledongthuc/pdf"
)

// Image-only PDFs (a WPS/Office export of a figure, or a scan stored as JPEG
// page images) have no text operators. Native extraction correctly returns
// empty text; without this fallback the office reader reports extract_error
// and the chat never sees the page.
const (
	imageOnlyPDFOCRPageLimitDefault = 16
	maxImagesPerPDFPage             = 4
	minEmbeddedPDFImagePixels       = 8

	// Match the desktop vision upload envelope (guiapp local image picker):
	// a page image the model is asked to see must be one a provider will accept.
	// Local OCR does not use this envelope. A page that cannot be attached
	// falls through to OCR.
	maxImageOnlyPDFPageBytes       int64 = 20 * 1024 * 1024
	maxImageOnlyPDFPagePixels      int64 = 40 * 1024 * 1024
	maxImageOnlyPDFPagesTotalBytes int64 = 20 * 1024 * 1024
	// Stop a compressed image bomb before image.Decode allocates the bitmap.
	// This is wider than the vision upload cap so a high-resolution scan can
	// still be OCR'd when it is too large to attach.
	maxEmbeddedPDFImageDecodePixels int64 = 80_000_000

	pdfOCRSkippedPageMarker       = "[image page not recognized: page limit]"
	pdfOCRNoTextMarker            = "[no text recognized]"
	pdfVisionPageMarker           = "[image attached]"
	pdfVisionSkippedPageMarker    = "[image page not attached: page limit]"
	pdfVisionBudgetPageMarker     = "[image page not attached: upload limit]"
	pdfVisionUnreadablePageMarker = "[image page not readable]"

	imageOnlyPDFVisionLead           = "这份 PDF 没有文字层。页面图片已附在本条消息里，请直接看图回答。不要再调用 office、read_document 或 OCR。"
	imageOnlyPDFVisionLeadTruncated  = "这份 PDF 没有文字层。已附上的页面请直接看图。其余图片页这次没有附上，需要那些页时再用 OCR。"
	imageOnlyPDFVisionLeadMixed      = "这份 PDF 没有文字层。已附上的页面请直接看图。没有附上的图片页，文字已经写在对应页下面。不要再调用 office、read_document 或 OCR。"
	imageOnlyPDFVisionLeadUnresolved = "这份 PDF 没有文字层。已附上的页面请直接看图。没有附上、也没有识别出文字的页面，需要时再用 OCR。"
)

// PicturePDFTranscriptOwnsVisionPolicy reports that text already contains the
// picture-PDF vision transcript. That transcript tells the model whether the
// attached pages are enough or which pages still need OCR. A later generic
// vision note must not override it.
func PicturePDFTranscriptOwnsVisionPolicy(text string) bool {
	if strings.Contains(text, "# text_source: vision") {
		return true
	}
	for _, lead := range []string{
		imageOnlyPDFVisionLead,
		imageOnlyPDFVisionLeadTruncated,
		imageOnlyPDFVisionLeadMixed,
		imageOnlyPDFVisionLeadUnresolved,
	} {
		if strings.Contains(text, lead) {
			return true
		}
	}
	return false
}

// StripAttachedImageInstructions removes host lines that are true only while
// this message still carries the image bytes. Those lines tell the model to
// look at the attached pages and not to read them with tools. The auto-extract
// notice says the same thing when the body under it is that vision transcript.
// A text extract keeps the notice. The picker note that says to analyze
// images on this message is removed from every copy. A text-only copy must
// not keep those instructions.
func StripAttachedImageInstructions(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	visionTranscript := PicturePDFTranscriptOwnsVisionPolicy(text)
	for _, lead := range []string{
		imageOnlyPDFVisionLead,
		imageOnlyPDFVisionLeadTruncated,
		imageOnlyPDFVisionLeadMixed,
		imageOnlyPDFVisionLeadUnresolved,
	} {
		text = strings.ReplaceAll(text, lead, "")
	}
	if visionTranscript {
		text = strings.ReplaceAll(text, AutoExtractNotice, "")
	}
	text = strings.ReplaceAll(text, AttachedImageReadHint, "")
	return strings.TrimSpace(text)
}

// errOfficeImageOCRUnavailable means the host has not wired a local OCR
// engine, or the engine is switched off. Callers keep the existing
// scan/OCR failure instead of inventing text.
var errOfficeImageOCRUnavailable = errors.New("office image OCR unavailable")

// errImageOnlyPDFNoPictures means native text is empty and no page image
// could be attached. Callers keep the scan/OCR failure.
var errImageOnlyPDFNoPictures = errors.New("image-only PDF has no attachable pictures")

// errImageOnlyPDFOverBudget means the pictures exist but the vision upload
// budget was already spent. Callers should say so, and the text path should
// OCR instead of claiming the pages are attached.
var errImageOnlyPDFOverBudget = errors.New("image-only PDF pictures exceed the vision upload budget")

// imageOnlyPDFVisionAssembleCalls counts cache misses. Tests use it to prove
// a chat turn does not decode the same picture PDF twice.
var imageOnlyPDFVisionAssembleCalls int

// officeImagePDFOCR recognizes one embedded page image. imageBase64 is
// standard base64 (no data-URL prefix) of a JPEG or PNG that image.Decode
// accepts. The returned text is the reading-order transcript, without boxes.
type officeImagePDFOCR func(imageBase64 string) (string, error)

var (
	officeImagePDFOCRMu        sync.RWMutex
	officeImagePDFOCRReady     func() bool
	officeImagePDFOCRRecognize officeImagePDFOCR
	imageOnlyPDFOCRPageLimit   = imageOnlyPDFOCRPageLimitDefault
)

// SetOfficeImagePDFOCR wires the process-local OCR engine used when a PDF has
// pictures but no native text. ready may be nil; a nil ready is treated as
// available whenever recognize is set. A nil recognize disables the fallback.
// The desktop host points this at the shared PP-OCRv6 engine.
func SetOfficeImagePDFOCR(ready func() bool, recognize officeImagePDFOCR) {
	officeImagePDFOCRMu.Lock()
	defer officeImagePDFOCRMu.Unlock()
	if recognize == nil {
		officeImagePDFOCRReady = nil
		officeImagePDFOCRRecognize = nil
		return
	}
	officeImagePDFOCRReady = ready
	officeImagePDFOCRRecognize = recognize
}

func officeImageOCRHooks() (func() bool, officeImagePDFOCR) {
	officeImagePDFOCRMu.RLock()
	defer officeImagePDFOCRMu.RUnlock()
	return officeImagePDFOCRReady, officeImagePDFOCRRecognize
}

func officeImageOCRAvailable() bool {
	ready, recognize := officeImageOCRHooks()
	if recognize == nil {
		return false
	}
	if ready != nil && !ready() {
		return false
	}
	return true
}

func currentImageOnlyPDFOCRPageLimit() int {
	officeImagePDFOCRMu.RLock()
	limit := imageOnlyPDFOCRPageLimit
	officeImagePDFOCRMu.RUnlock()
	if limit < 1 {
		return 1
	}
	return limit
}

// imageOnlyPDFSource is one pass over an image-only PDF.
// A classic page tree still comes from ExtractImagesFromAllPages.
// An object-stream page tree shares the ledongthuc open that counted the
// pages. Pages past the OCR cap are classified from the image dictionary,
// so their JPEG bytes stay in the file.
type imageOnlyPDFSource struct {
	reader   pdfPageImageReader
	rawBlind bool
	byPage   map[int][]gopdf2.ExtractedImage
}

func openImageOnlyPDFSource(data []byte) (*imageOnlyPDFSource, int, error) {
	src := &imageOnlyPDFSource{reader: pdfPageImageReader{data: data}}
	numPages, err := src.reader.numPages()
	if err != nil || numPages < 1 {
		return nil, 0, errOfficeImageOCRUnavailable
	}
	if err := validatePDFPageCount(numPages); err != nil {
		return nil, 0, err
	}
	src.rawBlind = src.reader.rawBlind
	if src.rawBlind {
		return src, numPages, nil
	}
	byPage, imageErr := gopdf2.ExtractImagesFromAllPages(data)
	if imageErr != nil {
		return nil, 0, imageErr
	}
	src.byPage = byPage
	return src, numPages, nil
}

func (s *imageOnlyPDFSource) images(pageIndex int) ([]gopdf2.ExtractedImage, error) {
	if s == nil {
		return nil, errOfficeImageOCRUnavailable
	}
	if !s.rawBlind {
		return selectablePDFImages(s.byPage[pageIndex]), nil
	}
	images, err := s.reader.extract(pageIndex)
	if err != nil {
		return nil, err
	}
	return selectablePDFImages(images), nil
}

func (s *imageOnlyPDFSource) hasImage(pageIndex int) (bool, error) {
	if s == nil {
		return false, errOfficeImageOCRUnavailable
	}
	if !s.rawBlind {
		return len(selectablePDFImages(s.byPage[pageIndex])) > 0, nil
	}
	return s.reader.dictHasSelectableImage(pageIndex)
}

// recognizeImageOnlyPDF OCRs embedded page images when native text extraction
// found nothing. Blank pages stay marked empty and are not sent to OCR.
// A page cap keeps a long scan from blocking the chat turn; the header says
// when later image pages were left unread.
func recognizeImageOnlyPDF(data []byte) (text string, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			text = ""
			err = fmt.Errorf("image PDF OCR panicked: %v", recovered)
		}
	}()
	if len(data) == 0 {
		return "", errOfficeImageOCRUnavailable
	}
	if !officeImageOCRAvailable() {
		if pdfHasEmbeddedImage(data) {
			log.Printf("[office-pdf] native text empty and an embedded image is present; local OCR is not available")
		}
		return "", errOfficeImageOCRUnavailable
	}
	_, recognize := officeImageOCRHooks()
	if recognize == nil {
		return "", errOfficeImageOCRUnavailable
	}

	src, numPages, openErr := openImageOnlyPDFSource(data)
	if openErr != nil {
		return "", openErr
	}

	limit := currentImageOnlyPDFOCRPageLimit()
	var body strings.Builder
	ocrPages := 0
	recognized := 0
	truncated := false
	for pageIndex := 0; pageIndex < numPages; pageIndex++ {
		// The cap counts pages sent to OCR. Later pages are only classified,
		// so an object-stream scan does not copy every remaining JPEG.
		if ocrPages >= limit {
			has, hasErr := src.hasImage(pageIndex)
			if hasErr != nil {
				return "", hasErr
			}
			if !has {
				writeOfficePDFPage(&body, pageIndex+1, pdfEmptyPageMarker)
				continue
			}
			truncated = true
			writeOfficePDFPage(&body, pageIndex+1, pdfOCRSkippedPageMarker)
			continue
		}
		images, imageErr := src.images(pageIndex)
		if errors.Is(imageErr, errPDFPageImageUnreadable) {
			writeOfficePDFPage(&body, pageIndex+1, fmt.Sprintf("[page extract error: %v]", imageErr))
			continue
		}
		if imageErr != nil {
			return "", imageErr
		}
		if len(images) == 0 {
			writeOfficePDFPage(&body, pageIndex+1, pdfEmptyPageMarker)
			continue
		}
		ocrPages++
		pageText, pageErr := recognizePDFPageImages(images, recognize)
		if pageErr != nil {
			writeOfficePDFPage(&body, pageIndex+1, fmt.Sprintf("[page extract error: %v]", pageErr))
			continue
		}
		if strings.TrimSpace(pageText) == "" {
			writeOfficePDFPage(&body, pageIndex+1, pdfOCRNoTextMarker)
			continue
		}
		recognized++
		writeOfficePDFPage(&body, pageIndex+1, pageText)
	}
	if recognized == 0 {
		return "", fmt.Errorf("image PDF OCR produced no text")
	}
	var out strings.Builder
	out.WriteString("# text_source: ocr\n")
	if truncated {
		fmt.Fprintf(&out, "# ocr_truncated: true\n# note: 本次只识别了前 %d 个含图片的页面，后面的图片页未识别。\n", limit)
	}
	out.WriteByte('\n')
	out.WriteString(body.String())
	log.Printf("[office-pdf] recognized image-only PDF pages=%d ocr_pages=%d chars=%d truncated=%t", numPages, recognized, len([]rune(out.String())), truncated)
	return out.String(), nil
}

// OCRPDFPageBatchMax is how many image pages one ocr_recognize call reads.
// A scanned book stays on the built-in engine by continuing at NextPage,
// instead of one call OCRing hundreds of pages inside the chat turn.
const OCRPDFPageBatchMax = 8

// OCRPDFMaxBytes is the on-disk cap for built-in PP-OCRv6 on a scanned PDF.
// It is above the 32 MiB text-inject cap. A scan inside this cap is read
// from its page images; it is not split into smaller files.
const OCRPDFMaxBytes int64 = 256 << 20

// ocrPDFImageProbePages caps how many imageless pages one call parses.
// ExtractImagesFromPage rebuilds the xref every time. A text PDF has no
// page image; walking every page would repeat that parse thousands of times.
// A scan has an image well inside this window.
const ocrPDFImageProbePages = 32

// ErrOCRPDFNoPageImage means the PDF has no embedded page image the built-in
// engine can read. It is not a license to install another OCR stack.
var ErrOCRPDFNoPageImage = errors.New("pdf has no page image for builtin ocr")

// errPDFPageImageUnreadable means the page has an image XObject whose bytes
// are not a JPEG this path can hand to PP-OCRv6. The page is unfinished.
// It is not a broken container, so page 1 does not abort the rest of the book.
var errPDFPageImageUnreadable = errors.New("pdf page image is not a decodable jpeg")

// ocrPDFPageErrorMarker is the body of a page whose image could not be read.
// The page stays unfinished so a later call can replace it. Blank pages and
// pages with no recognized words are finished results.
const ocrPDFPageErrorMarker = "[page extract error]"

type ocrTranscriptPage struct {
	number int
	body   string
}

// OCRTranscriptMaxPage is the highest "## Page N" header already stored in an
// ocr_recognize transcript. The header is its own line, the same shape
// writeOfficePDFPage writes. A mention inside a paragraph is not a header.
// Zero means the file is not one of those transcripts.
func OCRTranscriptMaxPage(text string) int {
	maxPage := 0
	for _, page := range parseOCRTranscript(text) {
		if page.number > maxPage {
			maxPage = page.number
		}
	}
	return maxPage
}

// OCRTranscriptPageDone reports that this page already has a finished body.
// An extract-error page is not done.
func OCRTranscriptPageDone(text string, page int) bool {
	if page < 1 {
		return false
	}
	body, ok := ocrTranscriptIndex(text)[page]
	return ok && !ocrTranscriptBodyBroken(body)
}

// OCRTranscriptPageBody returns the stored body of page, without the header.
// ok is false when that page has no header.
func OCRTranscriptPageBody(text string, page int) (string, bool) {
	if page < 1 {
		return "", false
	}
	body, ok := ocrTranscriptIndex(text)[page]
	return body, ok
}

// OCRTranscriptFirstMissing is the first page that has no header, counting
// from 1. An extract-error page has a header, so the book moves past it
// instead of retrying that page forever. Asking for that page again still
// re-reads it, because OCRTranscriptPageDone is false for the error body.
// The result is one past the highest header when there is no gap.
// Zero means the text has no page header.
func OCRTranscriptFirstMissing(text string) int {
	pages := ocrTranscriptIndex(text)
	if len(pages) == 0 {
		return 0
	}
	maxPage := 0
	for n := range pages {
		if n > maxPage {
			maxPage = n
		}
	}
	for i := 1; i <= maxPage; i++ {
		if _, ok := pages[i]; !ok {
			return i
		}
	}
	return maxPage + 1
}

// OCRTranscriptNextDonePage is the next finished page after start, or 0 when
// every later page still needs to be read. The batch stops there so a short
// gap does not OCR the pages that are already written.
func OCRTranscriptNextDonePage(text string, start int) int {
	if start < 1 {
		start = 1
	}
	next := 0
	for n, body := range ocrTranscriptIndex(text) {
		if n <= start || ocrTranscriptBodyBroken(body) {
			continue
		}
		if next == 0 || n < next {
			next = n
		}
	}
	return next
}

// MergeOCRTranscript folds addition into existing. A finished page is kept.
// An extract-error page is replaced when addition has a finished body for it.
// Pages are written in page order, so a later batch cannot leave a gap behind
// a higher page number or duplicate a page the file already has.
func MergeOCRTranscript(existing, addition string) string {
	pages := ocrTranscriptIndex(existing)
	for _, page := range parseOCRTranscript(addition) {
		prev, ok := pages[page.number]
		if !ok || (ocrTranscriptBodyBroken(prev) && !ocrTranscriptBodyBroken(page.body)) {
			pages[page.number] = page.body
		}
	}
	if len(pages) == 0 {
		return ""
	}
	nums := make([]int, 0, len(pages))
	for n := range pages {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	var b strings.Builder
	for i, n := range nums {
		if i > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "## Page %d\n%s", n, pages[n])
	}
	return b.String()
}

func ocrTranscriptHeader(line string) (int, bool) {
	line = strings.TrimSpace(line)
	const marker = "## Page "
	if !strings.HasPrefix(line, marker) {
		return 0, false
	}
	rest := line[len(marker):]
	n := 0
	digits := 0
	for digits < len(rest) {
		c := rest[digits]
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
		digits++
		if n > 1_000_000 {
			return 0, false
		}
	}
	if digits == 0 || digits != len(rest) || n < 1 {
		return 0, false
	}
	return n, true
}

func parseOCRTranscript(text string) []ocrTranscriptPage {
	var pages []ocrTranscriptPage
	var cur *ocrTranscriptPage
	flush := func() {
		if cur == nil {
			return
		}
		cur.body = strings.TrimRight(cur.body, "\n")
		pages = append(pages, *cur)
		cur = nil
	}
	for _, line := range strings.Split(text, "\n") {
		if n, ok := ocrTranscriptHeader(line); ok {
			flush()
			cur = &ocrTranscriptPage{number: n}
			continue
		}
		if cur == nil {
			continue
		}
		cur.body += line
		cur.body += "\n"
	}
	flush()
	return pages
}

func ocrTranscriptIndex(text string) map[int]string {
	out := make(map[int]string)
	for _, page := range parseOCRTranscript(text) {
		prev, ok := out[page.number]
		if !ok || (ocrTranscriptBodyBroken(prev) && !ocrTranscriptBodyBroken(page.body)) {
			out[page.number] = page.body
		}
	}
	return out
}

func ocrTranscriptBodyBroken(body string) bool {
	body = strings.TrimSpace(body)
	return body == ocrPDFPageErrorMarker || strings.HasPrefix(body, ocrPDFPageErrorMarker+":")
}

// SourcePDFPageCount is the 1-based page count, or an error when the
// container cannot be read. Callers that only need the count use this
// instead of extracting every page image.
func SourcePDFPageCount(data []byte) (n int, err error) {
	defer func() {
		if recover() != nil {
			n = 0
			err = errOfficeImageOCRUnavailable
		}
	}()
	if len(data) == 0 {
		return 0, errOfficeImageOCRUnavailable
	}
	n, err = gopdf2.GetSourcePDFPageCountFromBytes(data)
	if err != nil || n < 1 {
		return 0, errOfficeImageOCRUnavailable
	}
	if err = validatePDFPageCount(n); err != nil {
		return 0, err
	}
	return n, nil
}

// OCRPDFPageBatch is one built-in OCR slice of a scanned PDF.
type OCRPDFPageBatch struct {
	Text       string
	NextPage   int
	TotalPages int
	Recognized int
}

// OCRImagePDFPageBatch OCRs embedded page images with the host recognizer.
// startPage is 1-based. endPage is 1-based and exclusive; zero means no
// extra stop. Blank pages are recorded and do not spend the batch.
// NextPage is the next PDF page to pass, or 0 when the file is finished.
func OCRImagePDFPageBatch(data []byte, startPage, maxPages, endPage int, recognize officeImagePDFOCR) (OCRPDFPageBatch, error) {
	batch, _, err := ocrImagePDFPageBatch(data, startPage, maxPages, endPage, recognize, false)
	return batch, err
}

// OCRImagePDFPageBatchWithPictures is OCRImagePDFPageBatch plus the page
// image already decoded for each image page. Callers that also attach those
// pictures must not walk the PDF a second time.
func OCRImagePDFPageBatchWithPictures(data []byte, startPage, maxPages, endPage int, recognize officeImagePDFOCR) (OCRPDFPageBatch, []ImageOnlyPDFPage, error) {
	return ocrImagePDFPageBatch(data, startPage, maxPages, endPage, recognize, true)
}

func ocrImagePDFPageBatch(data []byte, startPage, maxPages, endPage int, recognize officeImagePDFOCR, collect bool) (OCRPDFPageBatch, []ImageOnlyPDFPage, error) {
	if len(data) == 0 || recognize == nil {
		return OCRPDFPageBatch{}, nil, errOfficeImageOCRUnavailable
	}
	pages := pdfPageImageReader{data: data}
	return ocrImagePDFPages(&pages, startPage, maxPages, endPage, recognize, collect)
}

// ocrImagePDFPages reads images from an existing reader. Callers that already
// opened the page tree for text reuse that open instead of parsing again.
func ocrImagePDFPages(pages *pdfPageImageReader, startPage, maxPages, endPage int, recognize officeImagePDFOCR, collect bool) (batch OCRPDFPageBatch, pictures []ImageOnlyPDFPage, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			batch = OCRPDFPageBatch{}
			pictures = nil
			err = fmt.Errorf("image PDF OCR panicked: %v", recovered)
		}
	}()
	if pages == nil || len(pages.data) == 0 || recognize == nil {
		return OCRPDFPageBatch{}, nil, errOfficeImageOCRUnavailable
	}
	if startPage < 1 {
		startPage = 1
	}
	if maxPages < 1 {
		maxPages = 1
	}
	if maxPages > OCRPDFPageBatchMax {
		maxPages = OCRPDFPageBatchMax
	}
	// One page at a time. ExtractImagesFromAllPages would decode the whole
	// scan on every 8-page call. When the raw parser cannot see the page
	// tree, the open that counted the pages is the one that reads images.
	numPages, pageErr := pages.numPages()
	if pageErr != nil || numPages < 1 {
		return OCRPDFPageBatch{}, nil, errOfficeImageOCRUnavailable
	}
	if err := validatePDFPageCount(numPages); err != nil {
		return OCRPDFPageBatch{}, nil, err
	}
	if startPage > numPages {
		return OCRPDFPageBatch{TotalPages: numPages}, nil, nil
	}

	var body strings.Builder
	budget := maxPages
	recognized := 0
	sawImage := false
	pageErrors := 0
	page := startPage
	for page <= numPages {
		// The batch is full, or the next pages are already written.
		// Either way the next page is parsed by a later call.
		if budget == 0 || (endPage > 0 && page >= endPage) {
			break
		}
		images, imageErr := pages.extract(page - 1)
		if imageErr != nil {
			// A broken container on the first page stops the call before
			// every page fails the same way. An image that is present but
			// not a JPEG is only that page, including page 1.
			if !errors.Is(imageErr, errPDFPageImageUnreadable) && startPage <= 1 && !sawImage && pageErrors == 0 && page == startPage {
				return OCRPDFPageBatch{}, nil, errOfficeImageOCRUnavailable
			}
			budget--
			pageErrors++
			writeOfficePDFPage(&body, page, ocrPDFPageErrorMarker)
			page++
			continue
		}
		selected := selectablePDFImages(images)
		if len(selected) == 0 {
			if !sawImage && pageErrors == 0 && page-startPage+1 >= ocrPDFImageProbePages && page < numPages {
				return OCRPDFPageBatch{TotalPages: numPages}, nil, ErrOCRPDFNoPageImage
			}
			writeOfficePDFPage(&body, page, pdfEmptyPageMarker)
			page++
			continue
		}
		sawImage = true
		budget--
		if collect {
			if payload, payloadErr := embeddedPDFImagePayload(selected[0]); payloadErr == nil && visionUploadAccepts(payload) {
				mime := "image/png"
				if bytes.HasPrefix(payload, []byte{0xFF, 0xD8}) {
					mime = "image/jpeg"
				}
				pictures = append(pictures, ImageOnlyPDFPage{
					PageNumber: page,
					MIME:       mime,
					Data:       append([]byte(nil), payload...),
				})
			}
		}
		pageText, pageErr := recognizePDFPageImages(selected, recognize)
		if pageErr != nil {
			writeOfficePDFPage(&body, page, ocrPDFPageErrorMarker)
		} else if strings.TrimSpace(pageText) == "" {
			writeOfficePDFPage(&body, page, pdfOCRNoTextMarker)
		} else {
			recognized++
			writeOfficePDFPage(&body, page, pageText)
		}
		page++
	}
	if !sawImage && pageErrors == 0 && page > numPages && startPage <= 1 {
		return OCRPDFPageBatch{TotalPages: numPages}, nil, ErrOCRPDFNoPageImage
	}
	next := 0
	if page <= numPages {
		next = page
	}
	return OCRPDFPageBatch{
		Text:       body.String(),
		NextPage:   next,
		TotalPages: numPages,
		Recognized: recognized,
	}, pictures, nil
}

// pdfPageImageReader loads one page's embedded images.
//
// GoPDF2's raw parser only sees classic "N gen obj" dictionaries. A PDF 1.5
// page tree stored in an object stream makes that parser report zero pages
// (page index out of range 0..-1) even when the page count is known and each
// page's JPEG is an ordinary DCTDecode stream. The page count already uses
// ledongthuc/pdf, which reads object streams. After the first such miss,
// later pages in this call skip the raw parser and use that page tree.
type pdfPageImageReader struct {
	data     []byte
	checked  bool
	rawBlind bool
	reader   *lpdf.Reader
	openErr  error
	opened   bool
	opens    int
	// pageRoot remembers leaf page dictionaries on this reader. ledongthuc
	// inflates an object stream again on every indirect resolve, and Page
	// walks Kids from the root, so a later 8-page window would redo that
	// walk. Kids are indexed only when a lookup visits them. A Pages node
	// whose /Count equals its kid count resolves only the requested kid.
	pageRoot        *pdfPageNode
	pageKidResolves int
	pageCount       int
	imageCollects   int
}

// pdfPageNode is one lazy page-tree node. A Pages node keeps /Count and does
// not index its Kids until a page in that subtree is requested.
type pdfPageNode struct {
	expanded      bool
	resolved      bool
	isPage        bool
	count         int
	dict          lpdf.Value
	kids          []pdfPageNode
	kidArray      lpdf.Value
	resolves      *int
	resources     lpdf.Value
	resourcesDone bool
	images        []gopdf2.ExtractedImage
	imagesErr     error
	imagesDone    bool
}

func (r *pdfPageImageReader) extract(pageIndex int) (images []gopdf2.ExtractedImage, err error) {
	defer func() {
		if recover() != nil {
			images = nil
			err = errOfficeImageOCRUnavailable
		}
	}()
	if r == nil {
		return nil, errOfficeImageOCRUnavailable
	}
	r.classify()
	if !r.rawBlind {
		images, err = gopdf2.ExtractImagesFromPage(r.data, pageIndex)
		if err == nil {
			return images, nil
		}
		if !rawPDFPageTreeMissing(err) {
			return nil, err
		}
		r.rawBlind = true
	}
	return r.imagesFromPageTree(pageIndex)
}

// plaintextHasPDFPageObject reports whether a classic page dictionary is
// visible in the file bytes. An object-stream page tree is compressed, so
// the raw parser cannot see it. A longer name such as /Type /Pages or
// /Type /PageLabels does not count.
func plaintextHasPDFPageObject(data []byte) bool {
	return hasPDFPageToken(data, []byte("/Type /Page")) || hasPDFPageToken(data, []byte("/Type/Page"))
}

func hasPDFPageToken(data, prefix []byte) bool {
	for len(data) > 0 {
		i := bytes.Index(data, prefix)
		if i < 0 {
			return false
		}
		after := i + len(prefix)
		if after >= len(data) || !pdfNameContinues(data[after]) {
			return true
		}
		data = data[after:]
	}
	return false
}

func pdfNameContinues(b byte) bool {
	return b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' || b >= '0' && b <= '9'
}

func (r *pdfPageImageReader) classify() {
	if r == nil || r.checked {
		return
	}
	r.checked = true
	// No classic page dictionary in the file bytes. The raw parser would
	// scan every stream, then report zero pages.
	r.rawBlind = !plaintextHasPDFPageObject(r.data)
}

func (r *pdfPageImageReader) numPages() (int, error) {
	if r == nil {
		return 0, errOfficeImageOCRUnavailable
	}
	if r.pageCount > 0 {
		return r.pageCount, nil
	}
	r.classify()
	if !r.rawBlind {
		n, err := gopdf2.GetSourcePDFPageCountFromBytes(r.data)
		if err != nil || n < 1 {
			return n, err
		}
		r.pageCount = n
		return n, nil
	}
	reader, err := r.pageTree()
	if err != nil || reader == nil {
		return 0, errOfficeImageOCRUnavailable
	}
	n := reader.NumPage()
	if n < 1 {
		return 0, errOfficeImageOCRUnavailable
	}
	r.pageCount = n
	return n, nil
}

func rawPDFPageTreeMissing(err error) bool {
	return err != nil && strings.Contains(err.Error(), "out of range (0..-1)")
}

func (r *pdfPageImageReader) imagesFromPageTree(pageIndex int) ([]gopdf2.ExtractedImage, error) {
	if pageIndex < 0 {
		return nil, fmt.Errorf("page index %d out of range", pageIndex)
	}
	n, err := r.numPages()
	if err != nil {
		return nil, err
	}
	if pageIndex >= n {
		return nil, fmt.Errorf("page index %d out of range", pageIndex)
	}
	node, nodeErr := r.pageNode(pageIndex + 1)
	if nodeErr != nil {
		return nil, nodeErr
	}
	if node == nil || node.dict.IsNull() {
		return nil, nil
	}
	if node.imagesDone {
		return node.images, node.imagesErr
	}
	res, resErr := node.pageResources()
	if resErr != nil {
		return nil, resErr
	}
	var images []gopdf2.ExtractedImage
	unreadable := collectPDFPageImages(res, r.data, 0, &images)
	node.imagesDone = true
	r.imageCollects++
	if len(images) == 0 && unreadable {
		node.imagesErr = errPDFPageImageUnreadable
		return nil, node.imagesErr
	}
	node.images = images
	return images, nil
}

// dictHasSelectableImage reports whether the page has an image XObject a
// caller would OCR, using width and height only. It does not copy the stream.
func (r *pdfPageImageReader) dictHasSelectableImage(pageIndex int) (has bool, err error) {
	defer func() {
		if recover() != nil {
			has = false
			err = errOfficeImageOCRUnavailable
		}
	}()
	if r == nil || pageIndex < 0 {
		return false, errOfficeImageOCRUnavailable
	}
	n, countErr := r.numPages()
	if countErr != nil || n < 1 {
		return false, errOfficeImageOCRUnavailable
	}
	if pageIndex >= n {
		return false, fmt.Errorf("page index %d out of range", pageIndex)
	}
	node, nodeErr := r.pageNode(pageIndex + 1)
	if nodeErr != nil {
		return false, nodeErr
	}
	if node == nil || node.dict.IsNull() {
		return false, nil
	}
	res, resErr := node.pageResources()
	if resErr != nil {
		return false, resErr
	}
	return pdfResourcesHaveSelectableImage(res, 0), nil
}

func pdfResourcesHaveSelectableImage(resources lpdf.Value, depth int) bool {
	if depth > maxPDFImageResourceDepth || resources.IsNull() {
		return false
	}
	xobjects := resources.Key("XObject")
	for _, name := range xobjects.Keys() {
		obj := xobjects.Key(name)
		switch pdfValueName(obj.Key("Subtype")) {
		case "Image":
			if pdfImageDictIsPageImage(obj) {
				return true
			}
		case "Form":
			if pdfResourcesHaveSelectableImage(obj.Key("Resources"), depth+1) {
				return true
			}
		}
	}
	return false
}

func pdfImageDictIsPageImage(obj lpdf.Value) bool {
	if obj.Kind() != lpdf.Stream {
		return false
	}
	width := int(obj.Key("Width").Int64())
	height := int(obj.Key("Height").Int64())
	if width > 0 && height > 0 && (width < minEmbeddedPDFImagePixels || height < minEmbeddedPDFImagePixels) {
		return false
	}
	return true
}

func (r *pdfPageImageReader) pageTree() (*lpdf.Reader, error) {
	if r.opened {
		return r.reader, r.openErr
	}
	r.opened = true
	r.opens++
	reader, err := lpdf.NewReader(bytes.NewReader(r.data), int64(len(r.data)))
	if err != nil {
		r.openErr = err
		return nil, err
	}
	r.reader = reader
	return reader, nil
}

const maxPDFPageTreeDepth = 32

// pageNode returns the 1-based leaf. A nil node is a missing page, matching
// ledongthuc Page, and is not an error. The walk reuses a leaf already
// stored on this reader.
func (r *pdfPageImageReader) pageNode(pageNumber int) (node *pdfPageNode, err error) {
	defer func() {
		if recover() != nil {
			node = nil
			err = errOfficeImageOCRUnavailable
		}
	}()
	if r == nil || pageNumber < 1 {
		return nil, fmt.Errorf("page %d out of range", pageNumber)
	}
	reader, openErr := r.pageTree()
	if openErr != nil || reader == nil {
		if openErr == nil {
			openErr = errOfficeImageOCRUnavailable
		}
		return nil, openErr
	}
	if r.pageRoot == nil {
		root := reader.Trailer().Key("Root").Key("Pages")
		r.pageRoot = &pdfPageNode{dict: root, resolves: &r.pageKidResolves}
	}
	return r.pageRoot.pageNodeAt(pageNumber, 0)
}

func (n *pdfPageNode) pageResources() (v lpdf.Value, err error) {
	defer func() {
		if recover() != nil {
			err = errOfficeImageOCRUnavailable
		}
	}()
	if n == nil {
		return lpdf.Value{}, nil
	}
	if n.resourcesDone {
		return n.resources, nil
	}
	if n.dict.IsNull() {
		n.resourcesDone = true
		return lpdf.Value{}, nil
	}
	v = lpdf.Page{V: n.dict}.Resources()
	n.resources = v
	n.resourcesDone = true
	return v, nil
}

func (n *pdfPageNode) expand() (err error) {
	defer func() {
		if recover() != nil {
			err = errOfficeImageOCRUnavailable
		}
	}()
	if n == nil || n.expanded {
		return nil
	}
	n.expanded = true
	if n.dict.IsNull() {
		n.isPage = true
		n.count = 1
		return nil
	}
	kids := n.dict.Key("Kids")
	if n.dict.Key("Type").Name() == "Page" || kids.Kind() != lpdf.Array || kids.Len() == 0 {
		n.isPage = true
		n.count = 1
		return nil
	}
	n.kidArray = kids
	n.kids = make([]pdfPageNode, kids.Len())
	if c := int(n.dict.Key("Count").Int64()); c > 0 {
		n.count = c
	}
	return nil
}

func (n *pdfPageNode) child(i int) (kid *pdfPageNode, err error) {
	defer func() {
		if recover() != nil {
			kid = nil
			err = errOfficeImageOCRUnavailable
		}
	}()
	if n == nil || i < 0 || i >= len(n.kids) {
		return nil, nil
	}
	kid = &n.kids[i]
	if kid.resolves == nil {
		kid.resolves = n.resolves
	}
	if !kid.resolved {
		dict := n.kidArray.Index(i)
		kid.dict = dict
		kid.resolved = true
		if n.resolves != nil {
			*n.resolves++
		}
	}
	return kid, nil
}

func (n *pdfPageNode) span(depth int) (int, error) {
	if n == nil {
		return 0, nil
	}
	if depth > maxPDFPageTreeDepth {
		return 0, errOfficeImageOCRUnavailable
	}
	if err := n.expand(); err != nil {
		return 0, err
	}
	if n.count > 0 {
		return n.count, nil
	}
	if n.isPage {
		return 1, nil
	}
	total := 0
	for i := range n.kids {
		kid, err := n.child(i)
		if err != nil {
			return 0, err
		}
		if kid == nil {
			continue
		}
		part, err := kid.span(depth + 1)
		if err != nil {
			return 0, err
		}
		total += part
	}
	if total > 0 {
		n.count = total
	}
	return total, nil
}

func (n *pdfPageNode) pageNodeAt(page, depth int) (*pdfPageNode, error) {
	if n == nil || page < 1 {
		return nil, nil
	}
	if depth > maxPDFPageTreeDepth {
		return nil, errOfficeImageOCRUnavailable
	}
	if err := n.expand(); err != nil {
		return nil, err
	}
	if n.isPage {
		if page == 1 && !n.dict.IsNull() {
			return n, nil
		}
		return nil, nil
	}
	// Each kid is one page when /Count matches the kid array. Index only
	// the requested kid so a late page does not resolve every page before it.
	if n.count > 0 && n.count == len(n.kids) {
		if page > n.count {
			return nil, nil
		}
		kid, err := n.child(page - 1)
		if err != nil || kid == nil {
			return nil, err
		}
		return kid.pageNodeAt(1, depth+1)
	}
	if n.count > 0 && page > n.count {
		return nil, nil
	}
	remain := page
	for i := 0; i < len(n.kids); i++ {
		kid, err := n.child(i)
		if err != nil {
			return nil, err
		}
		if kid == nil {
			continue
		}
		span, err := kid.span(depth + 1)
		if err != nil {
			return nil, err
		}
		if span < 1 {
			continue
		}
		if remain <= span {
			return kid.pageNodeAt(remain, depth+1)
		}
		remain -= span
	}
	return nil, nil
}

const maxPDFImageResourceDepth = 4

func collectPDFPageImages(resources lpdf.Value, file []byte, depth int, out *[]gopdf2.ExtractedImage) (unreadable bool) {
	if depth > maxPDFImageResourceDepth || resources.IsNull() || out == nil {
		return false
	}
	xobjects := resources.Key("XObject")
	for _, name := range xobjects.Keys() {
		obj := xobjects.Key(name)
		switch pdfValueName(obj.Key("Subtype")) {
		case "Image":
			img, ok := extractedImageFromXObject(name, obj, file)
			if ok {
				*out = append(*out, img)
				continue
			}
			if obj.Kind() == lpdf.Stream {
				unreadable = true
			}
		case "Form":
			if collectPDFPageImages(obj.Key("Resources"), file, depth+1, out) {
				unreadable = true
			}
		}
	}
	return unreadable
}

func extractedImageFromXObject(name string, obj lpdf.Value, file []byte) (gopdf2.ExtractedImage, bool) {
	if obj.Kind() != lpdf.Stream {
		return gopdf2.ExtractedImage{}, false
	}
	filters := pdfFilterNames(obj.Key("Filter"))
	if len(filters) != 1 || filters[0] != "DCTDecode" {
		return gopdf2.ExtractedImage{}, false
	}
	payload, ok := rawPDFStream(obj, file)
	if !ok {
		return gopdf2.ExtractedImage{}, false
	}
	return gopdf2.ExtractedImage{
		Name:             "/" + name,
		Width:            int(obj.Key("Width").Int64()),
		Height:           int(obj.Key("Height").Int64()),
		BitsPerComponent: int(obj.Key("BitsPerComponent").Int64()),
		ColorSpace:       pdfValueName(obj.Key("ColorSpace")),
		Filter:           filters[0],
		Data:             payload,
	}, true
}

func pdfValueName(v lpdf.Value) string {
	if v.Kind() == lpdf.Name {
		return v.Name()
	}
	return ""
}

func pdfFilterNames(v lpdf.Value) []string {
	switch v.Kind() {
	case lpdf.Name:
		if name := v.Name(); name != "" {
			return []string{name}
		}
	case lpdf.Array:
		names := make([]string, 0, v.Len())
		for i := 0; i < v.Len(); i++ {
			if name := pdfValueName(v.Index(i)); name != "" {
				names = append(names, name)
			}
		}
		return names
	}
	return nil
}

func rawPDFStream(obj lpdf.Value, file []byte) ([]byte, bool) {
	offset, ok := ledongthucStreamOffset(obj)
	if !ok || offset < 0 {
		return nil, false
	}
	n := obj.Key("Length").Int64()
	if n <= 0 || offset > int64(len(file)) || n > int64(len(file))-offset {
		return nil, false
	}
	out := make([]byte, n)
	copy(out, file[int(offset):int(offset)+int(n)])
	return out, true
}

// ledongthucStreamOffset is the file offset of a stream's first data byte.
// Value.Reader panics on DCTDecode, and the stream offset is not exported.
// The layout is ledongthuc/pdf's stream {dict, objptr, offset int64} at the
// module version pinned in go.mod. TestOCRImagePDFPageBatchReadsObjectStreamJPEG
// fails when that layout moves.
func ledongthucStreamOffset(v lpdf.Value) (int64, bool) {
	rv := reflect.ValueOf(&v).Elem()
	field := rv.FieldByName("data")
	if !field.IsValid() {
		return 0, false
	}
	field = reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
	if field.Kind() != reflect.Interface || field.IsNil() {
		return 0, false
	}
	concrete := field.Elem()
	if !concrete.IsValid() || concrete.Kind() != reflect.Struct || concrete.Type().Name() != "stream" {
		return 0, false
	}
	type ifaceHeader struct {
		_    unsafe.Pointer
		data unsafe.Pointer
	}
	ptr := (*ifaceHeader)(unsafe.Pointer(field.UnsafeAddr())).data
	if ptr == nil {
		return 0, false
	}
	type streamLayout struct {
		_      uintptr
		_      uint32
		_      uint16
		_      uint16
		offset int64
	}
	return (*streamLayout)(ptr).offset, true
}

func writeOfficePDFPage(b *strings.Builder, pageNumber int, body string) {
	if b.Len() > 0 {
		b.WriteString("\n\n")
	}
	fmt.Fprintf(b, "## Page %d\n%s", pageNumber, body)
}

func pdfHasEmbeddedImage(data []byte) (found bool) {
	defer func() {
		if recover() != nil {
			found = false
		}
	}()
	reader := pdfPageImageReader{data: data}
	reader.classify()
	// A classic page tree still answers from one all-pages extract. An
	// object-stream tree has no classic page dictionary, so that extract
	// is an empty map. Probe dictionaries until the first real image.
	if !reader.rawBlind {
		imagesByPage, err := gopdf2.ExtractImagesFromAllPages(data)
		if err != nil {
			return false
		}
		for _, images := range imagesByPage {
			if len(selectablePDFImages(images)) > 0 {
				return true
			}
		}
		return false
	}
	n, err := reader.numPages()
	if err != nil || n < 1 {
		return false
	}
	if n > ocrPDFImageProbePages {
		n = ocrPDFImageProbePages
	}
	for i := 0; i < n; i++ {
		has, hasErr := reader.dictHasSelectableImage(i)
		if hasErr != nil {
			return false
		}
		if has {
			return true
		}
	}
	return false
}

func selectablePDFImages(images []gopdf2.ExtractedImage) []gopdf2.ExtractedImage {
	if len(images) == 0 {
		return nil
	}
	selected := make([]gopdf2.ExtractedImage, 0, len(images))
	for _, img := range images {
		if len(img.Data) == 0 {
			continue
		}
		// A known tiny decoration is not a page. Unknown dimensions still go
		// through decode, which applies the same pixel floor.
		if img.Width > 0 && img.Height > 0 && (img.Width < minEmbeddedPDFImagePixels || img.Height < minEmbeddedPDFImagePixels) {
			continue
		}
		selected = append(selected, img)
	}
	if len(selected) == 0 {
		return nil
	}
	sort.SliceStable(selected, func(i, j int) bool {
		areaI := selected[i].Width * selected[i].Height
		areaJ := selected[j].Width * selected[j].Height
		if areaI != areaJ {
			return areaI > areaJ
		}
		return selected[i].Name < selected[j].Name
	})
	if len(selected) > maxImagesPerPDFPage {
		selected = selected[:maxImagesPerPDFPage]
	}
	sort.SliceStable(selected, func(i, j int) bool {
		if selected[i].Y != selected[j].Y {
			return selected[i].Y < selected[j].Y
		}
		if selected[i].X != selected[j].X {
			return selected[i].X < selected[j].X
		}
		return selected[i].Name < selected[j].Name
	})
	return selected
}

func recognizePDFPageImages(images []gopdf2.ExtractedImage, recognize officeImagePDFOCR) (string, error) {
	var b strings.Builder
	var lastErr error
	for _, img := range images {
		payload, err := embeddedPDFImagePayload(img)
		if err != nil {
			lastErr = err
			continue
		}
		text, err := recognize(base64.StdEncoding.EncodeToString(payload))
		if err != nil {
			lastErr = err
			continue
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(text)
	}
	if b.Len() > 0 {
		return b.String(), nil
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", nil
}

// recognizePreparedPDFImages OCRs pictures that were not attached. decoded
// aligns with images; a nil entry is decoded here. covered is how many of
// those pictures produced text.
func recognizePreparedPDFImages(images []gopdf2.ExtractedImage, decoded [][]byte) (text string, covered int) {
	if len(images) == 0 || !officeImageOCRAvailable() {
		return "", 0
	}
	_, recognize := officeImageOCRHooks()
	if recognize == nil {
		return "", 0
	}
	var b strings.Builder
	for i, img := range images {
		payload := []byte(nil)
		if i < len(decoded) {
			payload = decoded[i]
		}
		if len(payload) == 0 {
			var err error
			payload, err = embeddedPDFImagePayload(img)
			if err != nil {
				continue
			}
		}
		pageText, err := recognize(base64.StdEncoding.EncodeToString(payload))
		if err != nil {
			continue
		}
		pageText = strings.TrimSpace(pageText)
		if pageText == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(pageText)
		covered++
	}
	return b.String(), covered
}

func embeddedPDFImagePayload(img gopdf2.ExtractedImage) ([]byte, error) {
	payload := bytes.TrimLeft(img.Data, "\r\n\t ")
	if idx := bytes.Index(payload, []byte{0xFF, 0xD8}); idx > 0 && idx < 32 {
		payload = payload[idx:]
	}
	config, _, configErr := image.DecodeConfig(bytes.NewReader(payload))
	if configErr != nil || config.Width <= 0 || config.Height <= 0 {
		return nil, fmt.Errorf("decode embedded PDF image %s: %v", img.Name, configErr)
	}
	pixels := int64(config.Width) * int64(config.Height)
	if config.Width < minEmbeddedPDFImagePixels || config.Height < minEmbeddedPDFImagePixels {
		return nil, fmt.Errorf("embedded PDF image %s is too small", img.Name)
	}
	if pixels > maxEmbeddedPDFImageDecodePixels {
		return nil, fmt.Errorf("embedded PDF image %s exceeds the decode safety limit", img.Name)
	}
	// JPEG bytes are what the OCR engine decodes. A scan page is about
	// 2000x3000; expanding it here throws the bitmap away and the engine
	// decodes the same JPEG again to downscale it.
	if bytes.HasPrefix(payload, []byte{0xFF, 0xD8}) {
		return payload, nil
	}
	decoded, _, err := image.Decode(bytes.NewReader(payload))
	if err != nil || decoded == nil {
		return nil, fmt.Errorf("decode embedded PDF image %s: %v", img.Name, err)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, decoded); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}

func visionUploadAccepts(payload []byte) bool {
	if len(payload) == 0 || int64(len(payload)) > maxImageOnlyPDFPageBytes {
		return false
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(payload))
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return false
	}
	return int64(config.Width)*int64(config.Height) <= maxImageOnlyPDFPagePixels
}

// ImageOnlyPDFPage is one embedded picture from a PDF that has no text layer.
// PageNumber is 1-based. MIME is image/jpeg or image/png. Data is the raw
// image bytes the vision request should carry.
type ImageOnlyPDFPage struct {
	PageNumber int
	MIME       string
	Data       []byte
}

// ImageOnlyPDFPages returns the picture pages of an image-only PDF. A PDF
// that already has native text, or that has no attachable picture, returns
// nil pages and a nil error. truncated is true when picture pages past the
// page cap were left out. The same cap and image selection feed the vision
// transcript, so the attached bytes match the [image attached] markers.
func ImageOnlyPDFPages(filePath string) (pages []ImageOnlyPDFPage, truncated bool, err error) {
	return imageOnlyPDFPagesWithinBudget(filePath, 0)
}

func imageOnlyPDFPagesWithinBudget(filePath string, usedBytes int64) (pages []ImageOnlyPDFPage, truncated bool, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			pages = nil
			truncated = false
			err = fmt.Errorf("image PDF page extract panicked: %v", recovered)
		}
	}()
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return nil, false, nil
	}
	if usedBytes < 0 {
		usedBytes = 0
	}
	// The 32 MiB snapshot gate is for ordinary office text. A scanned PDF
	// inside the OCR cap is copied from page images instead.
	if info, statErr := os.Stat(filePath); statErr == nil && !info.IsDir() && info.Size() > autoInjectSizeLimit() {
		if strings.EqualFold(filepath.Ext(filePath), ".pdf") && info.Size() <= OCRPDFMaxBytes {
			return oversizedScanVisionPages(filePath, usedBytes)
		}
	}
	snapshot, cleanup, snapErr := SnapshotBoundedDocumentInput(filePath, ".pdf")
	if snapErr != nil {
		return nil, false, snapErr
	}
	defer cleanup()
	if _, preflightErr := preflightOfficeReadContainerIfPresent(snapshot); preflightErr != nil {
		return nil, false, preflightErr
	}
	if sniffOfficeFormat(snapshot) != "pdf" {
		return nil, false, nil
	}
	data, readErr := os.ReadFile(snapshot)
	if readErr != nil {
		return nil, false, readErr
	}
	// The chat expand that just produced the vision transcript already decoded
	// these pages. Reuse that result instead of parsing the PDF again.
	if _, cached, cacheTruncated, ok := loadImageOnlyPDFVisionCache(data, usedBytes); ok {
		return cached, cacheTruncated, nil
	}
	if pdfNativeTextPresent(data) {
		return nil, false, nil
	}
	_, pages, truncated, err = assembleImageOnlyPDF(data, usedBytes)
	if errors.Is(err, errImageOnlyPDFNoPictures) || errors.Is(err, errOfficeImageOCRUnavailable) {
		return nil, false, nil
	}
	return pages, truncated, err
}

// PDFScanPagePayloads copies embedded page images for a 1-based window.
// It does not apply the office-read size cap; the caller already bounded the bytes.
// A page with no attachable picture is omitted. A file with no pictures returns nil, nil.
// One image per page is enough for a scan: after position sorting, the first picture is the page.
func PDFScanPagePayloads(data []byte, startPage, maxPages int) (pages []ImageOnlyPDFPage, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			pages = nil
			err = fmt.Errorf("scan page image extract panicked: %v", recovered)
		}
	}()
	if len(data) == 0 {
		return nil, nil
	}
	reader := pdfPageImageReader{data: data}
	return scanPagePayloads(&reader, startPage, maxPages)
}

func scanPagePayloads(reader *pdfPageImageReader, startPage, maxPages int) ([]ImageOnlyPDFPage, error) {
	if reader == nil || len(reader.data) == 0 {
		return nil, nil
	}
	if startPage < 1 {
		startPage = 1
	}
	if maxPages < 1 {
		maxPages = 1
	}
	if maxPages > OCRPDFPageBatchMax {
		maxPages = OCRPDFPageBatchMax
	}
	numPages, pageErr := reader.numPages()
	if pageErr != nil || numPages < 1 {
		return nil, nil
	}
	if err := validatePDFPageCount(numPages); err != nil {
		return nil, err
	}
	if startPage > numPages {
		return nil, nil
	}
	end := startPage + maxPages - 1
	if end > numPages {
		end = numPages
	}
	out := make([]ImageOnlyPDFPage, 0, end-startPage+1)
	for page := startPage; page <= end; page++ {
		images, imageErr := reader.extract(page - 1)
		if imageErr != nil {
			continue
		}
		selected := selectablePDFImages(images)
		if len(selected) == 0 {
			continue
		}
		payload, payloadErr := embeddedPDFImagePayload(selected[0])
		if payloadErr != nil || !visionUploadAccepts(payload) {
			continue
		}
		mime := "image/png"
		if bytes.HasPrefix(payload, []byte{0xFF, 0xD8}) {
			mime = "image/jpeg"
		}
		out = append(out, ImageOnlyPDFPage{
			PageNumber: page,
			MIME:       mime,
			Data:       append([]byte(nil), payload...),
		})
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// oversizedScanVisionFileReads counts ReadFile calls on the oversized vision
// path. A chat turn that already walked the scan must not increment it again.
var oversizedScanVisionFileReads int

// A few windows of page images, keyed by the prompt path. Hashing a 30 MiB
// scan just to reuse eight JPEGs reads the file the cache was meant to avoid.
// One slot is not enough: a turn that names two scans would keep only the
// last, and the first would be read again when its pages are attached.
const oversizedScanPictureCacheMax = 4

type oversizedScanPictureCacheEntry struct {
	size   int64
	mod    time.Time
	pages  []ImageOnlyPDFPage
	total  int
	loaded time.Time
}

var oversizedScanPictureCacheState = struct {
	mu    sync.Mutex
	items map[string]oversizedScanPictureCacheEntry
}{items: map[string]oversizedScanPictureCacheEntry{}}

func loadOversizedScanPictures(filePath string, info os.FileInfo) ([]ImageOnlyPDFPage, int, bool) {
	if info == nil {
		return nil, 0, false
	}
	key := filepath.Clean(filePath)
	oversizedScanPictureCacheState.mu.Lock()
	defer oversizedScanPictureCacheState.mu.Unlock()
	entry, ok := oversizedScanPictureCacheState.items[key]
	if !ok || entry.size != info.Size() || !entry.mod.Equal(info.ModTime()) {
		return nil, 0, false
	}
	if time.Since(entry.loaded) >= officeExtractCacheTTL {
		delete(oversizedScanPictureCacheState.items, key)
		return nil, 0, false
	}
	return cloneImageOnlyPDFPages(entry.pages), entry.total, len(entry.pages) > 0
}

func storeOversizedScanPictures(filePath string, info os.FileInfo, pages []ImageOnlyPDFPage, total int) {
	if info == nil || len(pages) == 0 {
		return
	}
	key := filepath.Clean(filePath)
	cloned := cloneImageOnlyPDFPages(pages)
	oversizedScanPictureCacheState.mu.Lock()
	defer oversizedScanPictureCacheState.mu.Unlock()
	now := time.Now()
	for cachedKey, entry := range oversizedScanPictureCacheState.items {
		if now.Sub(entry.loaded) >= officeExtractCacheTTL {
			delete(oversizedScanPictureCacheState.items, cachedKey)
		}
	}
	oversizedScanPictureCacheState.items[key] = oversizedScanPictureCacheEntry{
		size: info.Size(), mod: info.ModTime(), pages: cloned, total: total, loaded: now,
	}
	for len(oversizedScanPictureCacheState.items) > oversizedScanPictureCacheMax {
		var oldestKey string
		var oldest time.Time
		for cachedKey, entry := range oversizedScanPictureCacheState.items {
			if cachedKey == key {
				continue
			}
			if oldestKey == "" || entry.loaded.Before(oldest) {
				oldestKey = cachedKey
				oldest = entry.loaded
			}
		}
		if oldestKey == "" {
			break
		}
		delete(oversizedScanPictureCacheState.items, oldestKey)
	}
}

// pdfPageTreeFileReads counts ReadFile calls that miss the page-tree cache.
// A later ocr_recognize or pdf_read_pages on the same scan must not increment it.
var pdfPageTreeFileReads int

// Two open scans. Each entry keeps the file bytes plus the ledongthuc reader,
// so a third file drops the oldest instead of pinning every document.
const pdfPageTreeFileCacheMax = 2

type pdfPageTreeFileCacheEntry struct {
	size   int64
	mod    time.Time
	tree   *PDFPageTree
	loaded time.Time
}

var pdfPageTreeFileCacheState = struct {
	mu    sync.Mutex
	items map[string]pdfPageTreeFileCacheEntry
}{items: map[string]pdfPageTreeFileCacheEntry{}}

func pdfPageTreeFileKey(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	abs = filepath.Clean(abs)
	if filepath.Separator == '\\' {
		abs = strings.ToLower(abs)
	}
	return abs
}

// CachedPDFPageTree returns the page tree opened for this path when the
// size and mtime still match and the entry is inside the office-extract
// window. A hit slides that window forward so a long run of 8-page OCR
// calls keeps the same parse.
func CachedPDFPageTree(path string, info os.FileInfo) (*PDFPageTree, bool) {
	if info == nil || info.IsDir() || info.Size() <= 0 {
		return nil, false
	}
	key := pdfPageTreeFileKey(path)
	if key == "" {
		return nil, false
	}
	pdfPageTreeFileCacheState.mu.Lock()
	defer pdfPageTreeFileCacheState.mu.Unlock()
	entry, ok := pdfPageTreeFileCacheState.items[key]
	if !ok || entry.tree == nil || entry.size != info.Size() || !entry.mod.Equal(info.ModTime()) {
		if ok {
			delete(pdfPageTreeFileCacheState.items, key)
		}
		return nil, false
	}
	if time.Since(entry.loaded) >= officeExtractCacheTTL {
		delete(pdfPageTreeFileCacheState.items, key)
		return nil, false
	}
	entry.loaded = time.Now()
	pdfPageTreeFileCacheState.items[key] = entry
	return entry.tree, true
}

// RememberPDFPageTree keeps tree for later reads of the same path. An entry
// that already matches size and mtime stays: replacing it would drop a parse
// that just walked the page tree.
func RememberPDFPageTree(path string, info os.FileInfo, tree *PDFPageTree) {
	if info == nil || tree == nil || info.IsDir() || info.Size() <= 0 || info.Size() > OCRPDFMaxBytes {
		return
	}
	key := pdfPageTreeFileKey(path)
	if key == "" {
		return
	}
	pdfPageTreeFileCacheState.mu.Lock()
	defer pdfPageTreeFileCacheState.mu.Unlock()
	now := time.Now()
	for cachedKey, entry := range pdfPageTreeFileCacheState.items {
		if now.Sub(entry.loaded) >= officeExtractCacheTTL {
			delete(pdfPageTreeFileCacheState.items, cachedKey)
		}
	}
	if entry, ok := pdfPageTreeFileCacheState.items[key]; ok && entry.tree != nil && entry.size == info.Size() && entry.mod.Equal(info.ModTime()) {
		entry.loaded = now
		pdfPageTreeFileCacheState.items[key] = entry
		return
	}
	pdfPageTreeFileCacheState.items[key] = pdfPageTreeFileCacheEntry{
		size: info.Size(), mod: info.ModTime(), tree: tree, loaded: now,
	}
	for len(pdfPageTreeFileCacheState.items) > pdfPageTreeFileCacheMax {
		var oldestKey string
		var oldest time.Time
		for cachedKey, entry := range pdfPageTreeFileCacheState.items {
			if cachedKey == key {
				continue
			}
			if oldestKey == "" || entry.loaded.Before(oldest) {
				oldestKey = cachedKey
				oldest = entry.loaded
			}
		}
		if oldestKey == "" {
			break
		}
		delete(pdfPageTreeFileCacheState.items, oldestKey)
	}
}

// OpenCachedPDFPageTree opens path, or returns the tree already opened for
// the same size and mtime. The caller shares that tree across OCR windows.
func OpenCachedPDFPageTree(path string) (*PDFPageTree, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() || info.Size() <= 0 {
		return nil, errOfficeImageOCRUnavailable
	}
	if info.Size() > OCRPDFMaxBytes {
		return nil, errOfficeReadInputTooLarge
	}
	if tree, ok := CachedPDFPageTree(path, info); ok {
		return tree, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	pdfPageTreeFileReads++
	after, statErr := os.Stat(path)
	if statErr != nil || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) || int64(len(data)) != info.Size() {
		return nil, ErrOfficeReadSourceChanged
	}
	tree, err := OpenPDFPageTree(data)
	if err != nil {
		return nil, err
	}
	RememberPDFPageTree(path, after, tree)
	return tree, nil
}

// PDFSelectableText reads selectable text for 1-based pages from the page
// tree. The raw parser is not used. ExtractPageText falls back to that
// parser when the page tree returns no words, and on an object-stream scan
// that fallback walks every stream in the file.
// PDFPageTree is one parse of an already-loaded PDF. Page count, selectable
// text, and image OCR share it, so an object-stream scan opens ledongthuc once.
// mu serializes those calls when a later window reuses the tree.
type PDFPageTree struct {
	mu    sync.Mutex
	pages pdfPageImageReader
}

// OpenPDFPageTree keeps data for later page reads. It does not parse until
// the first count, text, or image call.
func OpenPDFPageTree(data []byte) (*PDFPageTree, error) {
	if len(data) == 0 {
		return nil, errOfficeImageOCRUnavailable
	}
	return &PDFPageTree{pages: pdfPageImageReader{data: data}}, nil
}

// Bytes is the PDF this tree was opened on.
func (t *PDFPageTree) Bytes() []byte {
	if t == nil {
		return nil
	}
	return t.pages.data
}

// PageCount is the number of pages, or 0 when the file cannot be counted.
// Object-stream files use the cached ledongthuc reader. Classic files use
// GoPDF2 and do not open that reader.
func (t *PDFPageTree) PageCount() (n int) {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	defer func() {
		if recover() != nil {
			n = 0
		}
	}()
	counted, err := t.pages.numPages()
	if err != nil || counted < 1 {
		return 0
	}
	return counted
}

func (t *PDFPageTree) treeOpens() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.pages.opens
}

func (t *PDFPageTree) pageKidResolves() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.pages.pageKidResolves
}

func (t *PDFPageTree) pageImageCollects() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.pages.imageCollects
}

// SelectableText reads 1-based pages from the page tree. A page outside the
// file is omitted. A parser miss is an empty string, not an error.
func (t *PDFPageTree) SelectableText(pages []int) (text map[int]string, err error) {
	if t == nil || len(pages) == 0 {
		return map[int]string{}, nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	defer func() {
		if recover() != nil {
			text = nil
			err = errOfficeImageOCRUnavailable
		}
	}()
	reader, openErr := t.pages.pageTree()
	if openErr != nil || reader == nil {
		if openErr == nil {
			openErr = errOfficeImageOCRUnavailable
		}
		return nil, openErr
	}
	n := t.pages.pageCount
	if n < 1 {
		n = reader.NumPage()
		if n > 0 {
			t.pages.pageCount = n
		}
	}
	out := make(map[int]string, len(pages))
	for _, page := range pages {
		if page < 1 || page > n {
			continue
		}
		node, nodeErr := t.pages.pageNode(page)
		if nodeErr != nil || node == nil || node.dict.IsNull() {
			out[page] = ""
			continue
		}
		res, resErr := node.pageResources()
		if resErr != nil || res.Key("Font").IsNull() {
			out[page] = ""
			continue
		}
		out[page] = selectableTextOnPage(lpdf.Page{V: node.dict})
	}
	return out, nil
}

// PageHasSelectableText reports selectable words on one page. A parser miss
// is not text, so a scan still takes the OCR path.
func (t *PDFPageTree) PageHasSelectableText(page int) bool {
	if t == nil || page < 1 {
		return false
	}
	texts, err := t.SelectableText([]int{page})
	if err != nil {
		return false
	}
	return strings.TrimSpace(texts[page]) != ""
}

// OCRImagePages runs one OCR window on this tree. collect keeps the first
// picture of each image page for vision attachment.
func (t *PDFPageTree) OCRImagePages(startPage, maxPages, endPage int, recognize officeImagePDFOCR, collect bool) (OCRPDFPageBatch, []ImageOnlyPDFPage, error) {
	if t == nil {
		return OCRPDFPageBatch{}, nil, errOfficeImageOCRUnavailable
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return ocrImagePDFPages(&t.pages, startPage, maxPages, endPage, recognize, collect)
}

// ScanPagePayloads copies embedded page images from this tree.
func (t *PDFPageTree) ScanPagePayloads(startPage, maxPages int) (pages []ImageOnlyPDFPage, err error) {
	if t == nil {
		return nil, nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	defer func() {
		if recovered := recover(); recovered != nil {
			pages = nil
			err = fmt.Errorf("scan page image extract panicked: %v", recovered)
		}
	}()
	return scanPagePayloads(&t.pages, startPage, maxPages)
}

func PDFSelectableText(data []byte, pages []int) (text map[int]string, err error) {
	defer func() {
		if recover() != nil {
			text = nil
			err = errOfficeImageOCRUnavailable
		}
	}()
	if len(data) == 0 || len(pages) == 0 {
		return map[int]string{}, nil
	}
	tree, openErr := OpenPDFPageTree(data)
	if openErr != nil {
		return nil, openErr
	}
	return tree.SelectableText(pages)
}

func selectableTextOnPage(parsed lpdf.Page) (text string) {
	defer func() {
		if recover() != nil {
			text = ""
		}
	}()
	if parsed.V.IsNull() || parsed.V.Key("Contents").Kind() == lpdf.Null {
		return ""
	}
	rows, rowErr := parsed.GetTextByRow()
	if rowErr != nil {
		plain, plainErr := parsed.GetPlainText(nil)
		if plainErr != nil {
			return ""
		}
		return strings.TrimSpace(plain)
	}
	var b strings.Builder
	for _, row := range rows {
		if row == nil {
			continue
		}
		words := append([]lpdf.Text(nil), row.Content...)
		sort.SliceStable(words, func(a, c int) bool {
			return words[a].X < words[c].X
		})
		line := strings.Builder{}
		for _, word := range words {
			piece := strings.TrimSpace(word.S)
			if piece == "" {
				continue
			}
			if line.Len() > 0 {
				line.WriteByte(' ')
			}
			line.WriteString(piece)
		}
		if line.Len() == 0 {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line.String())
	}
	return strings.TrimSpace(b.String())
}

// oversizedScanVisionPages attaches one OCR window of a PDF that is past the
// text-inject cap and inside the built-in OCR cap. It does not snapshot the
// whole file through the 32 MiB office-read gate. Pictures remembered for
// this path, size, and mtime are reused without reading the file again.
func oversizedScanVisionPages(filePath string, usedBytes int64) ([]ImageOnlyPDFPage, bool, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, false, err
	}
	if info.IsDir() || info.Size() <= 0 {
		return nil, false, errImageOnlyPDFNoPictures
	}
	if info.Size() > OCRPDFMaxBytes {
		return nil, false, errOfficeReadInputTooLarge
	}
	payloads, pageCount, ok := loadOversizedScanPictures(filePath, info)
	if !ok {
		oversizedScanVisionFileReads++
		data, readErr := os.ReadFile(filePath)
		if readErr != nil {
			return nil, false, readErr
		}
		if int64(len(data)) != info.Size() || int64(len(data)) > OCRPDFMaxBytes {
			return nil, false, errOfficeReadInputTooLarge
		}
		after, statErr := os.Stat(filePath)
		if statErr != nil || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
			return nil, false, ErrOfficeReadSourceChanged
		}
		payloads, err = PDFScanPagePayloads(data, 1, OCRPDFPageBatchMax)
		if err != nil || len(payloads) == 0 {
			if err == nil {
				err = errImageOnlyPDFNoPictures
			}
			return nil, false, err
		}
		reader := pdfPageImageReader{data: data}
		if counted, countErr := reader.numPages(); countErr == nil {
			pageCount = counted
		}
		storeOversizedScanPictures(filePath, after, payloads, pageCount)
	}
	if usedBytes < 0 {
		usedBytes = 0
	}
	if usedBytes >= maxImageOnlyPDFPagesTotalBytes {
		return nil, false, errImageOnlyPDFOverBudget
	}
	out := make([]ImageOnlyPDFPage, 0, len(payloads))
	total := usedBytes
	for _, page := range payloads {
		if len(page.Data) == 0 {
			continue
		}
		if total+int64(len(page.Data)) > maxImageOnlyPDFPagesTotalBytes {
			break
		}
		total += int64(len(page.Data))
		out = append(out, page)
	}
	if len(out) == 0 {
		return nil, false, errImageOnlyPDFOverBudget
	}
	return out, pageCount > len(out), nil
}

// formatOversizedScanPDF injects one PP-OCRv6 window for a PDF that is past
// the text-inject cap and still inside OCRPDFMaxBytes. A file with no page
// image returns ok=false so the caller keeps the terminal size error.
// The block does not set truncated=true: that flag would overlay office,
// and office hits the same 32 MiB wall.
func formatOversizedScanPDF(filePath, resolved string, size int64, settings officeReadSettings) (string, int, bool) {
	if size <= 0 || size > OCRPDFMaxBytes {
		return "", 0, false
	}
	if !strings.EqualFold(filepath.Ext(filePath), ".pdf") && !strings.EqualFold(filepath.Ext(resolved), ".pdf") {
		return "", 0, false
	}
	before, statErr := os.Stat(resolved)
	if statErr != nil || before.IsDir() || before.Size() != size {
		return "", 0, false
	}
	f, err := os.Open(resolved)
	if err != nil {
		return "", 0, false
	}
	var header [5]byte
	n, readErr := f.Read(header[:])
	if readErr != nil || n < 5 || string(header[:]) != "%PDF-" {
		f.Close()
		return "", 0, false
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		f.Close()
		return "", 0, false
	}
	data, err := io.ReadAll(io.LimitReader(f, OCRPDFMaxBytes+1))
	f.Close()
	if err != nil || int64(len(data)) == 0 || int64(len(data)) > OCRPDFMaxBytes {
		return "", 0, false
	}
	after, statErr := os.Stat(resolved)
	if statErr != nil || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) || int64(len(data)) != before.Size() {
		return "", 0, false
	}
	// One page tree for the text check and the OCR walk. A second open
	// reparses every object stream in a scan that has no words.
	tree, openErr := OpenPDFPageTree(data)
	if openErr != nil {
		return "", 0, false
	}
	// A text page with a logo is still a text PDF. Page 1 is enough.
	// pdfNativeTextPresent walks every page, and ExtractPageText falls
	// back to a raw scan of every stream when this page has no words.
	if tree.PageHasSelectableText(1) {
		return "", 0, false
	}
	ready, recognize := officeImageOCRHooks()
	ocrReady := recognize != nil && (ready == nil || ready())
	var batch OCRPDFPageBatch
	var pictures []ImageOnlyPDFPage
	if ocrReady {
		var ocrErr error
		batch, pictures, ocrErr = tree.OCRImagePages(1, OCRPDFPageBatchMax, 0, recognize, true)
		if ocrErr != nil || (len(pictures) == 0 && strings.TrimSpace(batch.Text) == "") {
			return "", 0, false
		}
	} else {
		var payloadErr error
		pictures, payloadErr = tree.ScanPagePayloads(1, OCRPDFPageBatchMax)
		if payloadErr != nil || len(pictures) == 0 {
			return "", 0, false
		}
	}
	total := batch.TotalPages
	if total < 1 {
		total = tree.PageCount()
	}
	if len(pictures) > 0 {
		storeOversizedScanPictures(filePath, after, pictures, total)
	}
	used := settings.imagePDFVisionUsedBytes
	if used < 0 {
		used = 0
	}
	var attached []ImageOnlyPDFPage
	var visionBytes int64
	if settings.imagePDFPreferVision && used < maxImageOnlyPDFPagesTotalBytes {
		for _, page := range pictures {
			if len(page.Data) == 0 {
				continue
			}
			if used+visionBytes+int64(len(page.Data)) > maxImageOnlyPDFPagesTotalBytes {
				break
			}
			attached = append(attached, page)
			visionBytes += int64(len(page.Data))
		}
	}
	body := ""
	next := 0
	recognized := false
	ran := false
	if ocrReady {
		body = strings.TrimSpace(batch.Text)
		next = batch.NextPage
		ran = true
		recognized = batch.Recognized > 0 && body != ""
	}
	if runes := []rune(body); len(runes) > 30000 {
		body = string(runes[:30000]) + "\n\n（本窗文字已截断）"
	}
	windowEnd := oversizedScanWindowEnd(batch, pictures)
	lead := oversizedScanLead(1, windowEnd, total, next, recognized, ran, len(attached) > 0)
	note := oversizedScanNote(next, recognized, ran)
	includeBody := ran && body != ""
	var b strings.Builder
	fmt.Fprintf(&b, "%spath=%q format=%q note=%q ---\n", AutoExtractBeginMarker, filePath, "pdf", note)
	if len(attached) > 0 {
		b.WriteString("# text_source: vision\n")
		fmt.Fprintf(&b, "# vision_bytes: %d\n", visionBytes)
	} else if includeBody {
		b.WriteString("# text_source: ocr\n")
	}
	if next > 0 || !recognized {
		b.WriteString("# scan_continue: ocr_recognize\n")
	}
	b.WriteString(lead)
	b.WriteString("\n\n")
	if includeBody {
		b.WriteString(body)
		b.WriteByte('\n')
	}
	if len(attached) > 0 && !strings.Contains(body, pdfVisionPageMarker) {
		b.WriteString(pdfVisionPageMarker)
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "%spath=%q ---", AutoExtractEndMarker, filePath)
	text := b.String()
	log.Printf("[auto-extract] oversized scan pages=%d next=%d chars=%d vision=%t", total, next, len([]rune(body)), len(attached) > 0)
	RememberPDFPageTree(resolved, after, tree)
	if pdfPageTreeFileKey(filePath) != pdfPageTreeFileKey(resolved) {
		RememberPDFPageTree(filePath, after, tree)
	}
	return text, EstimateTextTokens(text), true
}

// oversizedScanWindowEnd is the last PDF page this inject covered.
// NextPage is the page a later ocr_recognize call should pass, so the span
// is the page before it. An image count is not a page number.
func oversizedScanWindowEnd(batch OCRPDFPageBatch, pictures []ImageOnlyPDFPage) int {
	end := 0
	if batch.NextPage > 1 {
		end = batch.NextPage - 1
	} else if batch.NextPage == 0 && batch.TotalPages > 0 && strings.TrimSpace(batch.Text) != "" {
		end = batch.TotalPages
	}
	if end < 1 {
		for _, page := range pictures {
			if page.PageNumber > end {
				end = page.PageNumber
			}
		}
	}
	if end < 1 {
		end = 1
	}
	if batch.TotalPages > 0 && end > batch.TotalPages {
		end = batch.TotalPages
	}
	return end
}

func oversizedScanLead(start, end, total, next int, recognized, ran, images bool) string {
	span := fmt.Sprintf("第 %d 页", start)
	if end > start {
		span = fmt.Sprintf("第 %d 到 %d 页", start, end)
	}
	totalNote := ""
	if total > 0 {
		totalNote = fmt.Sprintf("，共 %d 页", total)
	}
	more := "已到文末。"
	if next > 0 {
		more = fmt.Sprintf("后续调用 ocr_recognize，start_page=%d。", next)
	}
	switch {
	case recognized && images:
		return fmt.Sprintf("这是扫描件。宿主已用本机 PP-OCRv6 识别%s%s。这几页的图片也附在本条消息里，公式以图片为准。%s不要拆分或缩小文件，不要安装其他 OCR 引擎。", span, totalNote, more)
	case recognized:
		return fmt.Sprintf("这是扫描件。宿主已用本机 PP-OCRv6 识别%s%s。%s不要拆分或缩小文件，不要安装其他 OCR 引擎。", span, totalNote, more)
	case ran && images:
		return fmt.Sprintf("这是扫描件。宿主已看过%s%s，这几页没有写出可用正文。这几页的图片也附在本条消息里，公式以图片为准。%s不要拆分或缩小文件，不要安装其他 OCR 引擎。", span, totalNote, more)
	case ran:
		return fmt.Sprintf("这是扫描件。宿主已看过%s%s，这几页没有写出可用正文。%s不要拆分或缩小文件，不要安装其他 OCR 引擎。", span, totalNote, more)
	case images:
		return fmt.Sprintf("这是扫描件。识别模型还在准备，%s以图片附上%s。请稍后调用 ocr_recognize，start_page=1。不要拆分或缩小文件，不要安装其他 OCR 引擎。", span, totalNote)
	default:
		return "这是扫描件，本机 PP-OCRv6 尚未就绪。请调用 ocr_recognize，start_page=1。不要拆分或缩小文件，不要安装其他 OCR 引擎。"
	}
}

func oversizedScanNote(next int, recognized, ran bool) string {
	if recognized && next > 0 {
		return fmt.Sprintf("扫描页已用本机 PP-OCRv6 识别。后续调用 ocr_recognize，start_page=%d。不要拆分或缩小，不要安装其他引擎。", next)
	}
	if recognized {
		return "扫描页已用本机 PP-OCRv6 识别，已到文末。不要拆分或缩小，不要安装其他引擎。"
	}
	if ran && next > 0 {
		return fmt.Sprintf("扫描页已看过，没有写出可用正文。后续调用 ocr_recognize，start_page=%d。不要拆分或缩小，不要安装其他引擎。", next)
	}
	if ran {
		return "扫描页已看过，没有写出可用正文。请调用 ocr_recognize（本机 PP-OCRv6）。不要拆分或缩小，不要安装其他引擎。"
	}
	return "扫描件请调用 ocr_recognize（本机 PP-OCRv6），start_page 从 1 起。不要拆分或缩小，不要安装其他引擎。"
}

// pdfFileWithinBuiltinOCRCap reports a PDF the built-in engine is allowed to
// open. A missing file is not inside the cap.
func pdfFileWithinBuiltinOCRCap(filePath string) bool {
	info, err := os.Stat(strings.TrimSpace(filePath))
	if err != nil || info.IsDir() {
		return false
	}
	return info.Size() > 0 && info.Size() <= OCRPDFMaxBytes
}

// SelectedImageOnlyPDFPageAttachments turns picture pages of PDFs listed in
// the current file-picker marker into vision attachments. usedBytes counts
// image bytes already attached on this turn so the shared upload cap still
// holds. Text PDFs contribute nothing.
func SelectedImageOnlyPDFPageAttachments(userText string, usedBytes int64) ([]MessageAttachment, []string) {
	paths := SelectedLocalFilePathsFromPrompt(userText)
	if len(paths) == 0 {
		return nil, nil
	}
	if usedBytes < 0 {
		usedBytes = 0
	}
	attachments := make([]MessageAttachment, 0)
	notes := make([]string, 0)
	seen := make(map[string]struct{})
	total := usedBytes
	for _, path := range paths {
		if !strings.EqualFold(filepath.Ext(path), ".pdf") {
			continue
		}
		key := strings.ToLower(path)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		// A finished extract that is not a vision transcript has native text
		// or an OCR/error body. Do not open that PDF again. Keep a packed
		// transcript that still says a page image was attached.
		if body, found := autoExtractBodyForPath(userText, path); found &&
			!strings.Contains(body, "# text_source: vision") &&
			!strings.Contains(body, pdfVisionPageMarker) {
			continue
		}
		pages, _, err := imageOnlyPDFPagesWithinBudget(path, total)
		base := filepath.Base(path)
		if errors.Is(err, errImageOnlyPDFOverBudget) {
			notes = append(notes, fmt.Sprintf("[Host note: picture pages in %q were not attached because this turn is already at the vision upload limit]", base))
			continue
		}
		if err != nil {
			notes = append(notes, fmt.Sprintf("[Host note: picture pages in %q were not attached]", base))
			continue
		}
		if len(pages) == 0 {
			continue
		}
		stem := strings.TrimSuffix(base, filepath.Ext(base))
		if stem == "" || stem == "." {
			stem = "pdf"
		}
		seq := make(map[int]int)
		for _, page := range pages {
			if len(page.Data) == 0 {
				continue
			}
			total += int64(len(page.Data))
			seq[page.PageNumber]++
			n := seq[page.PageNumber]
			ext := ".png"
			if page.MIME == "image/jpeg" {
				ext = ".jpg"
			}
			name := fmt.Sprintf("%s-page-%d%s", stem, page.PageNumber, ext)
			if n > 1 {
				name = fmt.Sprintf("%s-page-%d-%d%s", stem, page.PageNumber, n, ext)
			}
			attachments = append(attachments, MessageAttachment{
				Type:     "image",
				FileName: name,
				MimeType: page.MIME,
				Data:     base64.StdEncoding.EncodeToString(page.Data),
				Size:     int64(len(page.Data)),
			})
		}
	}
	return attachments, notes
}

func visionTranscriptForImageOnlyPDF(data []byte, usedBytes int64) (string, error) {
	text, pages, truncated, err := assembleImageOnlyPDF(data, usedBytes)
	if err != nil {
		return "", err
	}
	log.Printf("[office-pdf] image-only PDF prepared for the vision model images=%d truncated=%t", len(pages), truncated)
	return text, nil
}

func assembleImageOnlyPDF(data []byte, usedBytes int64) (text string, pages []ImageOnlyPDFPage, truncated bool, err error) {
	if usedBytes < 0 {
		usedBytes = 0
	}
	if cachedText, cachedPages, cachedTruncated, ok := loadImageOnlyPDFVisionCache(data, usedBytes); ok {
		return cachedText, cachedPages, cachedTruncated, nil
	}
	imageOnlyPDFVisionAssembleCalls++
	src, numPages, openErr := openImageOnlyPDFSource(data)
	if openErr != nil {
		return "", nil, false, openErr
	}
	limit := currentImageOnlyPDFOCRPageLimit()
	collected := make([]ImageOnlyPDFPage, 0)
	plans := make([]imageOnlyPDFPlannedPage, 0, numPages)
	imagePages := 0
	attachedPages := 0
	overBudget := false
	total := usedBytes
	var attachedBytes int64
	for pageIndex := 0; pageIndex < numPages; pageIndex++ {
		plan := imageOnlyPDFPlannedPage{number: pageIndex + 1}
		if imagePages >= limit {
			has, hasErr := src.hasImage(pageIndex)
			if hasErr != nil {
				return "", nil, false, hasErr
			}
			if !has {
				plan.empty = true
			} else {
				truncated = true
				plan.capped = true
			}
			plans = append(plans, plan)
			continue
		}
		images, imageErr := src.images(pageIndex)
		if errors.Is(imageErr, errPDFPageImageUnreadable) {
			plan.unreadable = true
			plans = append(plans, plan)
			continue
		}
		if imageErr != nil {
			return "", nil, false, imageErr
		}
		if len(images) == 0 {
			plan.empty = true
			plans = append(plans, plan)
			continue
		}
		imagePages++
		for _, img := range images {
			// The upload budget is already spent. Keep the picture for OCR
			// and do not describe it as attached. Decode waits until this
			// document actually has a page the model will see.
			if total >= maxImageOnlyPDFPagesTotalBytes {
				plan.budget = true
				overBudget = true
				plan.pending = append(plan.pending, img)
				plan.decoded = append(plan.decoded, nil)
				continue
			}
			payload, payloadErr := embeddedPDFImagePayload(img)
			if payloadErr != nil {
				plan.unreadable = true
				continue
			}
			if !visionUploadAccepts(payload) {
				plan.pending = append(plan.pending, img)
				plan.decoded = append(plan.decoded, payload)
				continue
			}
			if total+int64(len(payload)) > maxImageOnlyPDFPagesTotalBytes {
				plan.budget = true
				overBudget = true
				plan.pending = append(plan.pending, img)
				plan.decoded = append(plan.decoded, payload)
				continue
			}
			mime := "image/png"
			if bytes.HasPrefix(payload, []byte{0xFF, 0xD8}) {
				mime = "image/jpeg"
			}
			total += int64(len(payload))
			attachedBytes += int64(len(payload))
			plan.attached = append(plan.attached, ImageOnlyPDFPage{
				PageNumber: pageIndex + 1,
				MIME:       mime,
				Data:       append([]byte(nil), payload...),
			})
		}
		if len(plan.attached) > 0 {
			attachedPages++
			collected = append(collected, plan.attached...)
		}
		plans = append(plans, plan)
	}
	// Nothing was attached. The caller OCRs the whole file once. Doing it
	// here as well would decode every picture twice.
	if attachedPages == 0 {
		if overBudget {
			return "", nil, truncated, errImageOnlyPDFOverBudget
		}
		return "", nil, truncated, errImageOnlyPDFNoPictures
	}
	var body strings.Builder
	ocrPages := 0
	unresolvedPages := 0
	for _, plan := range plans {
		switch {
		case plan.empty:
			writeOfficePDFPage(&body, plan.number, pdfEmptyPageMarker)
			continue
		case plan.capped:
			writeOfficePDFPage(&body, plan.number, pdfVisionSkippedPageMarker)
			continue
		}
		ocrText, covered := recognizePreparedPDFImages(plan.pending, plan.decoded)
		if covered > 0 {
			ocrPages++
		}
		if plan.unreadable || covered < len(plan.pending) {
			unresolvedPages++
		}
		parts := make([]string, 0, 3)
		if len(plan.attached) > 0 {
			parts = append(parts, pdfVisionPageMarker)
		}
		if ocrText != "" {
			parts = append(parts, ocrText)
		}
		if plan.unreadable || covered < len(plan.pending) {
			marker := pdfVisionUnreadablePageMarker
			if plan.budget {
				marker = pdfVisionBudgetPageMarker
			}
			parts = append(parts, marker)
		}
		if len(parts) == 0 {
			parts = append(parts, pdfVisionUnreadablePageMarker)
		}
		writeOfficePDFPage(&body, plan.number, strings.Join(parts, "\n\n"))
	}
	if ocrPages > 0 || unresolvedPages > 0 {
		log.Printf("[office-pdf] picture pages left out of the vision upload ocr_pages=%d unresolved_pages=%d", ocrPages, unresolvedPages)
	}
	lead := imageOnlyPDFVisionLead
	switch {
	case truncated:
		lead = imageOnlyPDFVisionLeadTruncated
	case unresolvedPages > 0:
		lead = imageOnlyPDFVisionLeadUnresolved
	case ocrPages > 0:
		lead = imageOnlyPDFVisionLeadMixed
	}
	var out strings.Builder
	out.WriteString("# text_source: vision\n")
	fmt.Fprintf(&out, "# vision_bytes: %d\n", attachedBytes)
	if truncated {
		fmt.Fprintf(&out, "# vision_truncated: true\n# note: 本次只附上前 %d 个含图片的页面，后面的图片页未附上。\n", limit)
	}
	out.WriteString(lead)
	out.WriteString("\n\n")
	out.WriteString(body.String())
	text = out.String()
	storeImageOnlyPDFVisionCache(data, usedBytes, text, collected, truncated)
	return text, collected, truncated, nil
}

// imageOnlyPDFPlannedPage is one PDF page during a vision assembly. Pending
// pictures were not attached; their decoded bytes are reused for OCR when
// this document still has at least one picture for the model.
type imageOnlyPDFPlannedPage struct {
	number     int
	empty      bool
	capped     bool
	attached   []ImageOnlyPDFPage
	pending    []gopdf2.ExtractedImage
	decoded    [][]byte
	budget     bool
	unreadable bool
}

func pdfNativeTextPresent(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	numPages, pageErr := gopdf2.GetSourcePDFPageCountFromBytes(data)
	if pageErr == nil && numPages > 0 && validatePDFPageCount(numPages) == nil {
		for _, body := range PDFPageBodies(data, numPages) {
			if strings.TrimSpace(body) != "" {
				return true
			}
		}
	}
	text, err := gopdf2.ExtractAllPagesText(data)
	return err == nil && strings.TrimSpace(text) != ""
}

const imageOnlyPDFVisionCacheMaxBytes = 32 * 1024 * 1024

type imageOnlyPDFVisionCacheEntry struct {
	text      string
	pages     []ImageOnlyPDFPage
	truncated bool
	bytes     int
	loaded    time.Time
}

var imageOnlyPDFVisionCacheState = struct {
	mu    sync.Mutex
	items map[string]imageOnlyPDFVisionCacheEntry
	total int
}{items: map[string]imageOnlyPDFVisionCacheEntry{}}

func imageOnlyPDFVisionCacheKey(data []byte, usedBytes int64) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]) + ":" + strconv.Itoa(currentImageOnlyPDFOCRPageLimit()) + ":" + strconv.FormatInt(usedBytes, 10)
}

func loadImageOnlyPDFVisionCache(data []byte, usedBytes int64) (string, []ImageOnlyPDFPage, bool, bool) {
	if len(data) == 0 {
		return "", nil, false, false
	}
	key := imageOnlyPDFVisionCacheKey(data, usedBytes)
	imageOnlyPDFVisionCacheState.mu.Lock()
	defer imageOnlyPDFVisionCacheState.mu.Unlock()
	entry, ok := imageOnlyPDFVisionCacheState.items[key]
	if !ok {
		return "", nil, false, false
	}
	if time.Since(entry.loaded) >= officeExtractCacheTTL {
		imageOnlyPDFVisionCacheState.total -= entry.bytes
		delete(imageOnlyPDFVisionCacheState.items, key)
		return "", nil, false, false
	}
	return entry.text, cloneImageOnlyPDFPages(entry.pages), entry.truncated, true
}

func storeImageOnlyPDFVisionCache(data []byte, usedBytes int64, text string, pages []ImageOnlyPDFPage, truncated bool) {
	if len(data) == 0 || text == "" {
		return
	}
	cloned := cloneImageOnlyPDFPages(pages)
	size := len(text)
	for _, page := range cloned {
		size += len(page.Data)
	}
	if size > imageOnlyPDFVisionCacheMaxBytes {
		return
	}
	key := imageOnlyPDFVisionCacheKey(data, usedBytes)
	imageOnlyPDFVisionCacheState.mu.Lock()
	defer imageOnlyPDFVisionCacheState.mu.Unlock()
	now := time.Now()
	for cachedKey, entry := range imageOnlyPDFVisionCacheState.items {
		if now.Sub(entry.loaded) >= officeExtractCacheTTL {
			imageOnlyPDFVisionCacheState.total -= entry.bytes
			delete(imageOnlyPDFVisionCacheState.items, cachedKey)
		}
	}
	if previous, ok := imageOnlyPDFVisionCacheState.items[key]; ok {
		imageOnlyPDFVisionCacheState.total -= previous.bytes
	}
	for len(imageOnlyPDFVisionCacheState.items) > 0 && imageOnlyPDFVisionCacheState.total+size > imageOnlyPDFVisionCacheMaxBytes {
		var oldestKey string
		var oldest time.Time
		for cachedKey, entry := range imageOnlyPDFVisionCacheState.items {
			if oldestKey == "" || entry.loaded.Before(oldest) {
				oldestKey = cachedKey
				oldest = entry.loaded
			}
		}
		imageOnlyPDFVisionCacheState.total -= imageOnlyPDFVisionCacheState.items[oldestKey].bytes
		delete(imageOnlyPDFVisionCacheState.items, oldestKey)
	}
	imageOnlyPDFVisionCacheState.items[key] = imageOnlyPDFVisionCacheEntry{
		text: text, pages: cloned, truncated: truncated, bytes: size, loaded: now,
	}
	imageOnlyPDFVisionCacheState.total += size
}

func cloneImageOnlyPDFPages(pages []ImageOnlyPDFPage) []ImageOnlyPDFPage {
	if len(pages) == 0 {
		return nil
	}
	cloned := make([]ImageOnlyPDFPage, len(pages))
	for i, page := range pages {
		cloned[i] = page
		cloned[i].Data = append([]byte(nil), page.Data...)
	}
	return cloned
}
