package gopdf

// ============================================================
// Annotation extraction — PyMuPDF parity: page.annots. Reads the /Annots
// array of existing PDFs: subtype, rectangle (top-down visual points),
// contents, author and stroke color.
// ============================================================

// ExtractedAnnotation is one annotation read from an existing PDF.
type ExtractedAnnotation struct {
	// Type is the /Subtype name: "Text", "Highlight", "Square", ...
	Type string
	// X, Y, W, H locate the annotation rectangle in top-down page points
	// (unrotated page space, matching how annotations are stored).
	X, Y, W, H float64
	// Contents is the annotation text (sticky note body, free text).
	Contents string
	// Author is the /T title field.
	Author string
	// Color is the stroke color (0-255 RGB; 0,0,0 when absent).
	ColorR, ColorG, ColorB uint8
	// Page is the 1-based page number.
	Page int
}

// ExtractAnnotationsFromPage reads the annotations of one page (0-based).
//
// Example:
//
//	annots, err := gopdf.ExtractAnnotationsFromPage(data, 0)
//	for _, a := range annots {
//	    fmt.Println(a.Type, a.Author, a.Contents)
//	}
func ExtractAnnotationsFromPage(pdfData []byte, pageIndex int) ([]ExtractedAnnotation, error) {
	parser, err := newRawPDFParser(pdfData)
	if err != nil {
		return nil, err
	}
	return extractAnnotsWithParser(parser, pageIndex), nil
}

// ExtractAnnotationsFromFile reads the annotations of a page from a file.
func ExtractAnnotationsFromFile(path string, pageIndex int) ([]ExtractedAnnotation, error) {
	data, err := readFileBytes(path)
	if err != nil {
		return nil, err
	}
	return ExtractAnnotationsFromPage(data, pageIndex)
}

// ExtractAllAnnotations reads the annotations of every page, keyed by the
// 0-based page index.
func ExtractAllAnnotations(pdfData []byte) (map[int][]ExtractedAnnotation, error) {
	parser, err := newRawPDFParser(pdfData)
	if err != nil {
		return nil, err
	}
	out := make(map[int][]ExtractedAnnotation, len(parser.pages))
	for i := range parser.pages {
		out[i] = extractAnnotsWithParser(parser, i)
	}
	return out, nil
}

// extractAnnotsWithParser reads a page's /Annots with an existing parser.
func extractAnnotsWithParser(parser *rawPDFParser, pageIndex int) []ExtractedAnnotation {
	if pageIndex < 0 || pageIndex >= len(parser.pages) {
		return nil
	}
	page := parser.pages[pageIndex]
	pageObj, ok := parser.objects[page.objNum]
	if !ok {
		return nil
	}
	pageH := page.mediaBox[3] - page.mediaBox[1]

	// /Annots may be an array or a single reference
	annotRefs := extractRefArray(pageObj.dict, "/Annots")
	if len(annotRefs) == 0 {
		if r := extractRef(pageObj.dict, "/Annots"); r > 0 {
			annotRefs = []int{r}
		}
	}
	if len(annotRefs) == 0 {
		return nil
	}

	var out []ExtractedAnnotation
	for _, ref := range annotRefs {
		obj, ok := parser.objects[ref]
		if !ok {
			continue
		}
		dict := obj.dict
		a := ExtractedAnnotation{
			Type: extractName(dict, "/Subtype"),
			Page: pageIndex + 1,
		}
		// links, popups and widgets are separate APIs (page.get_links /
		// form fields parity) — not returned by the annotations reader
		if a.Type == "Link" || a.Type == "Popup" || a.Type == "Widget" {
			continue
		}
		// rectangle: PDF y-up [x1 y1 x2 y2] -> top-down
		if r := extractNumberArray(dict, "/Rect"); len(r) >= 4 {
			x0 := mathMinF(r[0], r[2])
			x1 := mathMaxF(r[0], r[2])
			y0 := mathMinF(r[1], r[3])
			y1 := mathMaxF(r[1], r[3])
			a.X, a.Y = x0, pageH-y1
			a.W, a.H = x1-x0, y1-y0
		}
		a.Contents = extractStringFromDict(dict, "/Contents")
		a.Author = extractStringFromDict(dict, "/T")
		if c := extractNumberArray(dict, "/C"); len(c) >= 3 {
			a.ColorR = uint8(clamp01v(c[0])*255 + 0.5)
			a.ColorG = uint8(clamp01v(c[1])*255 + 0.5)
			a.ColorB = uint8(clamp01v(c[2])*255 + 0.5)
		}
		out = append(out, a)
	}
	return out
}

func mathMinF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func mathMaxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
