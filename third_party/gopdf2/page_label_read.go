package gopdf

import (
	"sort"
	"strings"
)

// ============================================================
// Page label reading from existing PDFs — PyMuPDF parity.
// ============================================================

// ExtractPageLabels reads the page label ranges of an existing PDF.
// Labels define how viewers number pages (roman numerals for a cover,
// decimal for the body, "Appendix A" prefixes, ...).
//
// Example:
//
//	labels, err := gopdf.ExtractPageLabels(data)
//	for _, l := range labels {
//	    fmt.Printf("from page %d: style %q prefix %q start %d\n",
//	        l.PageIndex, l.Style, l.Prefix, l.Start)
//	}
func ExtractPageLabels(pdfData []byte) ([]PageLabel, error) {
	parser, err := newRawPDFParser(pdfData)
	if err != nil {
		return nil, err
	}
	return extractPageLabelsWithParser(parser), nil
}

// ExtractPageLabelsFromFile reads the page label ranges from a file.
func ExtractPageLabelsFromFile(path string) ([]PageLabel, error) {
	data, err := readFileBytes(path)
	if err != nil {
		return nil, err
	}
	return ExtractPageLabels(data)
}

// PageLabels returns the document's page label ranges (cached).
func (d *PDFDoc) PageLabels() []PageLabel {
	if d.labelsCache == nil {
		d.labelsCache = extractPageLabelsWithParser(d.parser)
	}
	return d.labelsCache
}

// Rotation returns the /Rotate degrees (0/90/180/270) of a page.
func (d *PDFDoc) Rotation(pageIndex int) (int, error) {
	if err := d.checkPage(pageIndex); err != nil {
		return 0, err
	}
	return d.parser.pages[pageIndex].rotate, nil
}

// extractPageLabelsWithParser walks /PageLabels /Nums.
func extractPageLabelsWithParser(parser *rawPDFParser) []PageLabel {
	if parser.root <= 0 {
		return nil
	}
	rootObj, ok := parser.objects[parser.root]
	if !ok {
		return nil
	}
	plRef := extractRef(rootObj.dict, "/PageLabels")
	if plRef <= 0 {
		return nil
	}
	plObj, ok := parser.objects[plRef]
	if !ok {
		return nil
	}
	// /Nums [ idx << /S /r /St 1 /P (pre) >> ... ]
	numsIdx := strings.Index(plObj.dict, "/Nums")
	if numsIdx < 0 {
		return nil
	}
	rest := plObj.dict[numsIdx+5:]
	start := strings.Index(rest, "[")
	if start < 0 {
		return nil
	}
	end := strings.LastIndex(rest, "]")
	if end <= start {
		return nil
	}
	arr := rest[start+1 : end]
	tokens := tokenizeBytes([]byte(arr))

	var labels []PageLabel
	haveCur := false
	cur := PageLabel{Start: 1}
	inDict := false
	prevName := ""

	for i := 0; i < len(tokens); i++ {
		tokB := tokens[i]
		tok := string(tokB)
		isNum := false
		if v, ok := fastParseFloat(tokB); ok {
			isNum = true
			if !inDict {
				if haveCur {
					labels = append(labels, cur)
				}
				cur = PageLabel{PageIndex: int(v), Start: 1}
				haveCur = true
				prevName = ""
				continue
			}
			// number inside the label dict: belongs to /St
			if prevName == "/St" {
				cur.Start = int(v)
			}
			prevName = ""
			continue
		}
		_ = isNum
		switch tok {
		case string(tokDictOpen):
			inDict = true
			prevName = ""
		case string(tokDictClose):
			if haveCur {
				labels = append(labels, cur)
				haveCur = false
				cur = PageLabel{Start: 1}
			}
			inDict = false
			prevName = ""
		default:
			if !inDict {
				prevName = tok
				continue
			}
			switch {
			case tok == "/S":
				if i+1 < len(tokens) {
					cur.Style = PageLabelStyle(strings.TrimPrefix(string(tokens[i+1]), "/"))
					i++
				}
				prevName = tok
			case tok == "/St":
				prevName = tok
			case tok == "/P":
				if i+1 < len(tokens) {
					cur.Prefix = unescapePDFString(strings.Trim(string(tokens[i+1]), "()"))
					i++
				}
				prevName = tok
			default:
				prevName = tok
			}
		}
	}
	if haveCur {
		labels = append(labels, cur)
	}
	sort.Slice(labels, func(i, j int) bool { return labels[i].PageIndex < labels[j].PageIndex })
	return labels
}
