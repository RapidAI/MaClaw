package gopdf

import (
	"strconv"
	"strings"
)

// ============================================================
// TOC (outline/bookmark) reading from existing PDFs, and writing a full
// nested TOC tree. PyMuPDF parity: doc.get_toc() / doc.set_toc().
// ============================================================

// TOCEntry is one outline entry read from an existing PDF.
type TOCEntry struct {
	// Level is the hierarchy depth (1 = top level).
	Level int
	// Title is the bookmark text.
	Title string
	// Page is the 1-based destination page (0 when unresolved, e.g. named
	// destinations).
	Page int
	// Y is the destination vertical position in points from the page top.
	Y float64
}

// tocMaxNodes bounds outline traversal on hostile inputs.
const tocMaxNodes = 4096

// ExtractTOC reads the outline (bookmark) tree from raw PDF bytes and
// returns it as a flat, level-tagged list in reading order — the same
// shape PyMuPDF's get_toc() produces.
//
// Example:
//
//	entries, err := gopdf.ExtractTOC(data)
//	for _, e := range entries {
//	    fmt.Printf("%*s%d %s -> page %d\n", e.Level, "", e.Level, e.Title, e.Page)
//	}
func ExtractTOC(pdfData []byte) ([]TOCEntry, error) {
	parser, err := newRawPDFParser(pdfData)
	if err != nil {
		return nil, err
	}
	return extractTOCWithParser(parser), nil
}

// extractTOCWithParser reads the outline tree using an existing parser.
func extractTOCWithParser(parser *rawPDFParser) []TOCEntry {
	if parser.root <= 0 {
		return nil
	}
	rootObj, ok := parser.objects[parser.root]
	if !ok {
		return nil
	}
	outlinesRef := extractRef(rootObj.dict, "/Outlines")
	if outlinesRef <= 0 {
		return nil
	}
	// page obj number -> 1-based page index
	pageIndex := make(map[int]int, len(parser.pages))
	for i, pg := range parser.pages {
		pageIndex[pg.objNum] = i + 1
	}

	var entries []TOCEntry
	visited := make(map[int]bool, 64)

	var walk func(objNum int, level int)
	walk = func(objNum int, level int) {
		if level > 32 || len(entries) >= tocMaxNodes {
			return
		}
		for cur := firstOutlineChild(parser, objNum); cur > 0; {
			if visited[cur] {
				return
			}
			visited[cur] = true
			node, ok := parser.objects[cur]
			if !ok {
				return
			}
			page, y := resolveOutlineDest(parser, node.dict, pageIndex)
			entries = append(entries, TOCEntry{
				Level: level,
				Title: strings.TrimSpace(extractStringFromDict(node.dict, "/Title")),
				Page:  page,
				Y:     y,
			})
			// children first (depth-first, reading order)
			walk(cur, level+1)
			cur = extractRef(node.dict, "/Next")
		}
	}
	walk(outlinesRef, 1)
	return entries
}

// ExtractTOCFromFile reads the outline tree from a PDF file.
func ExtractTOCFromFile(path string) ([]TOCEntry, error) {
	data, err := readFileBytes(path)
	if err != nil {
		return nil, err
	}
	return ExtractTOC(data)
}

// firstOutlineChild resolves the /First entry of an outlines root or node.
func firstOutlineChild(parser *rawPDFParser, objNum int) int {
	obj, ok := parser.objects[objNum]
	if !ok {
		return 0
	}
	return extractRef(obj.dict, "/First")
}

// resolveOutlineDest extracts the destination page and Y from /Dest or /A.
// Returns (0, 0) for named destinations and broken links.
func resolveOutlineDest(parser *rawPDFParser, dict string, pageIndex map[int]int) (int, float64) {
	var arr string
	if i := strings.Index(dict, "/Dest"); i >= 0 {
		rest := strings.TrimLeft(dict[i+len("/Dest"):], " \t\r\n")
		switch {
		case strings.HasPrefix(rest, "["):
			// direct destination array: /Dest [ N 0 R /XYZ x y z ]
			if end := strings.Index(rest, "]"); end >= 0 {
				arr = rest[1:end]
			}
		case rest[0] >= '0' && rest[0] <= '9':
			// indirect reference to a destination array object
			if ref, _ := parseObjRefAt(rest); ref > 0 {
				if obj, ok := parser.objects[ref]; ok {
					arr = obj.dict
				}
			}
		default:
			return 0, 0 // named destination — unresolved
		}
	} else if aRef := extractRef(dict, "/A"); aRef > 0 {
		// action form: /A << /S /GoTo /D [ ... ] >>
		if obj, ok := parser.objects[aRef]; ok {
			if di := strings.Index(obj.dict, "/D"); di >= 0 {
				rest := strings.TrimLeft(obj.dict[di+2:], " \t\r\n")
				if strings.HasPrefix(rest, "[") {
					if end := strings.Index(rest, "]"); end >= 0 {
						arr = rest[1:end]
					}
				}
			}
		}
	}
	if arr == "" {
		return 0, 0
	}

	// first ref in the array is the page
	pageRef, _ := parseObjRefAt(arr)
	if pageRef <= 0 {
		return 0, 0
	}
	page, ok := pageIndex[pageRef]
	if !ok {
		return 0, 0
	}
	// /XYZ x y zoom: y is the second-to-last number; /Fit has none.
	// arr is already bracket-stripped, so parse the fields directly.
	fields := strings.Fields(arr)
	nums := make([]float64, 0, len(fields))
	for _, f := range fields {
		if v, err := strconv.ParseFloat(f, 64); err == nil {
			nums = append(nums, v)
		}
	}
	y := 0.0
	if len(nums) >= 2 {
		y = nums[len(nums)-2]
	}
	// y is PDF-up relative to the page box: convert to top-down using the
	// page's (inherited) media box from the parsed page list
	if page <= 0 || page > len(parser.pages) {
		return page, 0
	}
	mb := parser.pages[page-1].mediaBox
	return page, mb[3] - mb[1] - y
}

