package gopdf

import (
	"fmt"
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

	linesCache     map[int][]TextLine
	parasCache     map[int][]TextParagraph
	wordsCache     map[int][]TextWord
	drawingsCache  map[int][]Drawing
	tablesCache    map[int][]Table
	imagesCache    map[int][]ExtractedImage
	infoCache      *PdfInfo
	tocCache       []TOCEntry
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
