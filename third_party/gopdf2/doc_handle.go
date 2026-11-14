package gopdf

import (
	"fmt"
	"strings"
)

// ============================================================
// PDFDoc — a parsed document handle. Parse once, query many: the PyMuPDF
// document model. All extraction results are cached per page, so walking
// every page through several APIs decompresses each content stream once
// instead of once per API.
// ============================================================

// PDFDoc is a handle to a parsed PDF document.
//
// Example:
//
//	doc, err := gopdf.OpenPDFDocument(data)
//	if err != nil { log.Fatal(err) }
//	for p := 0; p < doc.PageCount(); p++ {
//	    for _, para := range doc.Paragraphs(p) { fmt.Println(para.Text) }
//	}
type PDFDoc struct {
	parser *rawPDFParser

	linesCache    map[int][]TextLine
	parasCache    map[int][]TextParagraph
	wordsCache    map[int][]TextWord
	drawingsCache map[int][]Drawing
	tablesCache   map[int][]Table
	imagesCache   map[int][]ExtractedImage
	annotsCache   map[int][]ExtractedAnnotation
	labelsCache   []PageLabel
	infoCache     *PdfInfo
	tocCache      []TOCEntry
	rootObj       *rawPDFObject
}

// OpenPDFDocument parses raw PDF bytes once and returns a document handle
// for repeated extraction queries. Prefer this over the package-level
// Extract* functions when more than one query runs against the same file.
func OpenPDFDocument(pdfData []byte) (*PDFDoc, error) {
	parser, err := newRawPDFParser(pdfData)
	if err != nil {
		return nil, err
	}
	return &PDFDoc{
		parser:        parser,
		linesCache:    map[int][]TextLine{},
		parasCache:    map[int][]TextParagraph{},
		wordsCache:    map[int][]TextWord{},
		drawingsCache: map[int][]Drawing{},
		tablesCache:   map[int][]Table{},
		imagesCache:   map[int][]ExtractedImage{},
		annotsCache:   map[int][]ExtractedAnnotation{},
	}, nil
}

// OpenPDFDocumentFromFile parses a PDF file into a document handle.
func OpenPDFDocumentFromFile(path string) (*PDFDoc, error) {
	data, err := readFileBytes(path)
	if err != nil {
		return nil, err
	}
	return OpenPDFDocument(data)
}

// PageCount returns the number of pages.
func (d *PDFDoc) PageCount() int {
	return len(d.parser.pages)
}

// checkPage validates a 0-based page index.
func (d *PDFDoc) checkPage(pageIndex int) error {
	if pageIndex < 0 || pageIndex >= len(d.parser.pages) {
		return fmt.Errorf("page index %d out of range (0..%d)", pageIndex, len(d.parser.pages)-1)
	}
	return nil
}

// Lines returns the merged, reading-order text lines of a page.
func (d *PDFDoc) Lines(pageIndex int) ([]TextLine, error) {
	if err := d.checkPage(pageIndex); err != nil {
		return nil, err
	}
	if lines, ok := d.linesCache[pageIndex]; ok {
		return lines, nil
	}
	lines := GroupIntoTextLines(d.runs(pageIndex))
	d.linesCache[pageIndex] = lines
	return lines, nil
}

// Paragraphs returns the reconstructed paragraphs of a page.
func (d *PDFDoc) Paragraphs(pageIndex int) ([]TextParagraph, error) {
	if err := d.checkPage(pageIndex); err != nil {
		return nil, err
	}
	if paras, ok := d.parasCache[pageIndex]; ok {
		return paras, nil
	}
	lines, err := d.Lines(pageIndex)
	if err != nil {
		return nil, err
	}
	paras := ParagraphsFromLines(lines)
	d.parasCache[pageIndex] = paras
	return paras, nil
}

// Words returns the position-aware words of a page in reading order.
func (d *PDFDoc) Words(pageIndex int) ([]TextWord, error) {
	if err := d.checkPage(pageIndex); err != nil {
		return nil, err
	}
	if words, ok := d.wordsCache[pageIndex]; ok {
		return words, nil
	}
	lines, err := d.Lines(pageIndex)
	if err != nil {
		return nil, err
	}
	var words []TextWord
	for _, ln := range lines {
		words = append(words, ln.Words...)
	}
	d.wordsCache[pageIndex] = words
	return words, nil
}

// Text returns the page text as newline-joined lines.
func (d *PDFDoc) Text(pageIndex int) (string, error) {
	lines, err := d.Lines(pageIndex)
	if err != nil {
		return "", err
	}
	out := ""
	for _, ln := range lines {
		out += ln.Text + "\n"
	}
	return out, nil
}

// Drawings returns the vector graphics of a page.
func (d *PDFDoc) Drawings(pageIndex int) ([]Drawing, error) {
	if err := d.checkPage(pageIndex); err != nil {
		return nil, err
	}
	if dr, ok := d.drawingsCache[pageIndex]; ok {
		return dr, nil
	}
	dr := d.extractDrawings(pageIndex)
	d.drawingsCache[pageIndex] = dr
	return dr, nil
}

// Tables detects the ruled tables of a page (uses cached drawings+lines).
func (d *PDFDoc) Tables(pageIndex int) ([]Table, error) {
	if err := d.checkPage(pageIndex); err != nil {
		return nil, err
	}
	if tb, ok := d.tablesCache[pageIndex]; ok {
		return tb, nil
	}
	drawings, err := d.Drawings(pageIndex)
	if err != nil {
		return nil, err
	}
	lines, err := d.Lines(pageIndex)
	if err != nil {
		return nil, err
	}
	tables := detectTable(drawings, lines)
	d.tablesCache[pageIndex] = tables
	return tables, nil
}

// Images returns the images of a page with placements.
func (d *PDFDoc) Images(pageIndex int) ([]ExtractedImage, error) {
	if err := d.checkPage(pageIndex); err != nil {
		return nil, err
	}
	if im, ok := d.imagesCache[pageIndex]; ok {
		return im, nil
	}
	images := d.extractImages(pageIndex)
	d.imagesCache[pageIndex] = images
	return images, nil
}

// TextInRect returns the text inside a visual-space region of a page.
func (d *PDFDoc) TextInRect(pageIndex int, clip ClipRect) (string, error) {
	lines, err := d.Lines(pageIndex)
	if err != nil {
		return "", err
	}
	out := ""
	for _, ln := range lines {
		inRect := false
		for _, w := range ln.Words {
			cx := w.X + w.Width/2
			cy := w.Y - w.Height/2
			if cx >= clip.X && cx <= clip.X+clip.W && cy >= clip.Y && cy <= clip.Y+clip.H {
				inRect = true
				break
			}
		}
		if inRect {
			out += ln.Text + "\n"
		}
	}
	return out, nil
}

// Search searches all pages for the query.
func (d *PDFDoc) Search(query string, opt SearchOptions) []TextSearchResult {
	var results []TextSearchResult
	for i := range d.parser.pages {
		var pr []TextSearchResult
		if opt.Dehyphenate {
			pr = d.searchDehyphenated(i, query, opt.CaseInsensitive)
		} else {
			pr, _ = searchTextOnPage(d.parser, i, query, opt.CaseInsensitive)
		}
		results = append(results, pr...)
	}
	return results
}

// Info returns the document metadata (cached).
func (d *PDFDoc) Info() PdfInfo {
	if d.infoCache == nil {
		info := d.parser.readInfoDict()
		d.infoCache = &info
	}
	return *d.infoCache
}

// TOC returns the outline tree (cached).
func (d *PDFDoc) TOC() []TOCEntry {
	if d.tocCache == nil {
		d.tocCache = d.extractTOC()
	}
	return d.tocCache
}

// ---- internal helpers bridging PDFDoc to the package-level with-parser
// extraction functions ----

func (d *PDFDoc) runs(pageIndex int) []ExtractedText {
	runs, _ := extractRunsWithParser(d.parser, pageIndex)
	return runs
}

func (d *PDFDoc) extractDrawings(pageIndex int) []Drawing {
	dr, _ := extractDrawingsWithParser(d.parser, pageIndex)
	return dr
}

func (d *PDFDoc) extractImages(pageIndex int) []ExtractedImage {
	im, _ := extractImagesWithParser(d.parser, pageIndex)
	return im
}

func (d *PDFDoc) searchDehyphenated(pageIndex int, query string, caseInsensitive bool) []TextSearchResult {
	return searchDehyphenated(d.parser, pageIndex, query, caseInsensitive)
}

func (d *PDFDoc) extractTOC() []TOCEntry {
	return extractTOCWithParser(d.parser)
}

// PageSize returns the visual size of a page in points, honoring /Rotate
// (width and height swap for 90/270).
func (d *PDFDoc) PageSize(pageIndex int) (PageInfo, error) {
	if err := d.checkPage(pageIndex); err != nil {
		return PageInfo{}, err
	}
	page := d.parser.pages[pageIndex]
	w := page.mediaBox[2] - page.mediaBox[0]
	h := page.mediaBox[3] - page.mediaBox[1]
	if page.hasCrop {
		// the visible area is the CropBox (PyMuPDF page.rect convention)
		w = page.cropBox[2] - page.cropBox[0]
		h = page.cropBox[3] - page.cropBox[1]
	}
	if page.rotate == 90 || page.rotate == 270 {
		w, h = h, w
	}
	return PageInfo{Width: w, Height: h, PageNumber: pageIndex + 1}, nil
}

// PageSizes returns the visual sizes of all pages (1-based PageNumber).
func (d *PDFDoc) PageSizes() []PageInfo {
	out := make([]PageInfo, 0, len(d.parser.pages))
	for i := range d.parser.pages {
		if pi, err := d.PageSize(i); err == nil {
			out = append(out, pi)
		}
	}
	return out
}

// Annotations returns the annotations of a page (cached).
func (d *PDFDoc) Annotations(pageIndex int) ([]ExtractedAnnotation, error) {
	if err := d.checkPage(pageIndex); err != nil {
		return nil, err
	}
	if a, ok := d.annotsCache[pageIndex]; ok {
		return a, nil
	}
	annots := extractAnnotsWithParser(d.parser, pageIndex)
	d.annotsCache[pageIndex] = annots
	return annots, nil
}

// OpenPDFDocumentWithPassword parses an encrypted PDF into a document
// handle, decrypting with the given user or owner password.
func OpenPDFDocumentWithPassword(pdfData []byte, password string) (*PDFDoc, error) {
	decrypted := pdfData
	if detectEncryption(pdfData) > 0 {
		dc, err := authenticate(pdfData, password)
		if err != nil {
			return nil, err
		}
		if dc != nil {
			decrypted = decryptPDF(pdfData, dc)
		}
	}
	return OpenPDFDocument(decrypted)
}

// Links returns the link annotations of a page.
func (d *PDFDoc) Links(pageIndex int) ([]ExtractedLink, error) {
	if err := d.checkPage(pageIndex); err != nil {
		return nil, err
	}
	return extractLinksFromPage(d.parser, d.parser.pages[pageIndex], pageIndex), nil
}

// Fonts returns the fonts used by a page.
func (d *PDFDoc) Fonts(pageIndex int) ([]ExtractedFont, error) {
	if err := d.checkPage(pageIndex); err != nil {
		return nil, err
	}
	return extractFontsFromResources(d.parser, d.parser.pages[pageIndex].resources), nil
}

// Formatted returns the page text in the requested format
// (FormatText / FormatBlocks / FormatWords / FormatHTML / FormatJSON).
func (d *PDFDoc) Formatted(pageIndex int, format TextExtractionFormat) (interface{}, error) {
	if err := d.checkPage(pageIndex); err != nil {
		return nil, err
	}
	texts := d.runs(pageIndex)
	switch format {
	case FormatText:
		return formatAsText(texts), nil
	case FormatBlocks:
		return formatAsBlocks(texts), nil
	case FormatWords:
		return formatAsWords(texts), nil
	case FormatHTML:
		return formatAsHTML(texts, pageIndex), nil
	case FormatJSON:
		return formatAsJSON(texts, pageIndex, d.parser)
	default:
		return nil, fmt.Errorf("unsupported format: %d", format)
	}
}

// HasForms reports whether the document contains an AcroForm.
func (d *PDFDoc) HasForms() bool {
	if d.rootObj == nil {
		obj, ok := d.parser.objects[d.parser.root]
		if !ok {
			return false
		}
		d.rootObj = &obj
	}
	return extractRef(d.rootObj.dict, "/AcroForm") > 0
}

// FormFields reads the interactive form fields of the document (PyMuPDF
// widgets parity): name, type, rectangle, current value.
func (d *PDFDoc) FormFields() []FormField {
	return extractFormFieldsWithParser(d.parser)
}

// extractFormFieldsWithParser walks /AcroForm /Fields and reads the core
// field properties.
func extractFormFieldsWithParser(parser *rawPDFParser) []FormField {
	if parser.root <= 0 {
		return nil
	}
	rootObj, ok := parser.objects[parser.root]
	if !ok {
		return nil
	}
	acroRef := extractRef(rootObj.dict, "/AcroForm")
	if acroRef <= 0 {
		return nil
	}
	acroObj, ok := parser.objects[acroRef]
	if !ok {
		return nil
	}
	var fields []FormField
	for _, ref := range extractRefArray(acroObj.dict, "/Fields") {
		obj, ok := parser.objects[ref]
		if !ok {
			continue
		}
		f := FormField{
			Name: extractStringFromDict(obj.dict, "/T"),
		}
		switch extractName(obj.dict, "/FT") {
		case "Btn":
			f.Type = FormFieldCheckbox
		case "Ch":
			f.Type = FormFieldChoice
		case "Sig":
			f.Type = FormFieldSignature
		default:
			f.Type = FormFieldText
		}
		if r := extractNumberArray(obj.dict, "/Rect"); len(r) >= 4 {
			// widget rects are PDF y-up; convert to top-down against the
			// field's own page box when resolvable
			f.X, f.W = r[0], r[2]-r[0]
			f.H = r[3] - r[1]
			f.Y = r[3]
		}
		// value: string or name (/Yes)
		if v := extractStringFromDict(obj.dict, "/V"); v != "" {
			f.Value = v
		} else if vi := stringsIndex(obj.dict, "/V"); vi >= 0 {
			rest := trimSpaces(obj.dict[vi+2:])
			if strings.HasPrefix(rest, "/") {
				f.Value = rest[1:minStr(len(rest), 1+firstWordLen(rest[1:]))]
			}
		}
		fields = append(fields, f)
	}
	return fields
}

func stringsIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func trimSpaces(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\r' || s[0] == '\n') {
		s = s[1:]
	}
	return s
}

func firstWordLen(s string) int {
	n := 0
	for n < len(s) && s[n] != ' ' && s[n] != '\t' && s[n] != '\r' && s[n] != '\n' && s[n] != '/' {
		n++
	}
	return n
}

func minStr(a, b int) int {
	if a < b {
		return a
	}
	return b
}
