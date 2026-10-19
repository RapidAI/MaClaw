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
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	gopdf2 "github.com/VantageDataChat/GoPDF2"
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

	numPages, pageErr := gopdf2.GetSourcePDFPageCountFromBytes(data)
	if pageErr != nil || numPages < 1 {
		return "", errOfficeImageOCRUnavailable
	}
	if err := validatePDFPageCount(numPages); err != nil {
		return "", err
	}
	imagesByPage, imageErr := gopdf2.ExtractImagesFromAllPages(data)
	if imageErr != nil {
		return "", imageErr
	}

	limit := currentImageOnlyPDFOCRPageLimit()
	var body strings.Builder
	ocrPages := 0
	recognized := 0
	truncated := false
	for pageIndex := 0; pageIndex < numPages; pageIndex++ {
		images := selectablePDFImages(imagesByPage[pageIndex])
		if len(images) == 0 {
			writeOfficePDFPage(&body, pageIndex+1, pdfEmptyPageMarker)
			continue
		}
		if ocrPages >= limit {
			truncated = true
			writeOfficePDFPage(&body, pageIndex+1, pdfOCRSkippedPageMarker)
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
	decoded, _, err := image.Decode(bytes.NewReader(payload))
	if err != nil || decoded == nil {
		return nil, fmt.Errorf("decode embedded PDF image %s: %v", img.Name, err)
	}
	if bytes.HasPrefix(payload, []byte{0xFF, 0xD8}) {
		return payload, nil
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
	numPages, pageErr := gopdf2.GetSourcePDFPageCountFromBytes(data)
	if pageErr != nil || numPages < 1 {
		return "", nil, false, errOfficeImageOCRUnavailable
	}
	if err := validatePDFPageCount(numPages); err != nil {
		return "", nil, false, err
	}
	imagesByPage, imageErr := gopdf2.ExtractImagesFromAllPages(data)
	if imageErr != nil {
		return "", nil, false, imageErr
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
		images := selectablePDFImages(imagesByPage[pageIndex])
		plan := imageOnlyPDFPlannedPage{number: pageIndex + 1}
		if len(images) == 0 {
			plan.empty = true
			plans = append(plans, plan)
			continue
		}
		if imagePages >= limit {
			truncated = true
			plan.capped = true
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
		_, gotAny := renderPDFPageExtracts(numPages, func(pageIndex int) (string, error) {
			return gopdf2.ExtractPageText(data, pageIndex)
		})
		if gotAny {
			return true
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
