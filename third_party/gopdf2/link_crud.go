package gopdf

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// LinkInfo represents a link found on a PDF page.
type LinkInfo struct {
	// Index is the position of this link in the page's link list.
	Index int
	// X, Y, W, H define the link rectangle in document units.
	X, Y, W, H float64
	// URL is the external URL (empty for internal links).
	URL string
	// Anchor is the internal anchor name (empty for external links).
	Anchor string
	// IsExternal is true for external URL links, false for internal anchors.
	IsExternal bool
}

// GetLinks returns all links on the current page.
//
// Example:
//
//	links := pdf.GetLinks()
//	for _, l := range links {
//	    fmt.Printf("Link at (%.0f,%.0f): %s\n", l.X, l.Y, l.URL)
//	}
func (gp *GoPdf) GetLinks() []LinkInfo {
	page := gp.pdfObjs[gp.curr.IndexOfPageObj].(*PageObj)
	return gp.getLinksFromPage(page)
}

// GetLinksOnPage returns all links on the specified page (1-indexed).
func (gp *GoPdf) GetLinksOnPage(pageNo int) []LinkInfo {
	page := gp.findPageObj(pageNo)
	if page == nil {
		return nil
	}
	return gp.getLinksFromPage(page)
}

func (gp *GoPdf) getLinksFromPage(page *PageObj) []LinkInfo {
	var result []LinkInfo
	idx := 0
	for _, objID := range page.LinkObjIds {
		if objID < 1 || objID-1 >= len(gp.pdfObjs) {
			continue
		}
		obj := gp.pdfObjs[objID-1]
		switch v := obj.(type) {
		case annotObj:
			li := LinkInfo{
				Index: idx,
				X:     gp.PointsToUnits(v.x),
				Y:     gp.PointsToUnits(v.y),
				W:     gp.PointsToUnits(v.w),
				H:     gp.PointsToUnits(v.h),
			}
			if v.url != "" {
				li.URL = v.url
				li.IsExternal = true
			} else if v.anchor != "" {
				li.Anchor = v.anchor
			}
			result = append(result, li)
			idx++
		}
	}
	return result
}

// DeleteLink removes the link at the given index from the current page.
// Returns true if the link was removed.
func (gp *GoPdf) DeleteLink(index int) bool {
	page := gp.pdfObjs[gp.curr.IndexOfPageObj].(*PageObj)
	return gp.deleteLinkFromPage(page, index)
}

// DeleteLinkOnPage removes the link at the given index from the specified page (1-indexed).
func (gp *GoPdf) DeleteLinkOnPage(pageNo, linkIndex int) bool {
	page := gp.findPageObj(pageNo)
	if page == nil {
		return false
	}
	return gp.deleteLinkFromPage(page, linkIndex)
}

func (gp *GoPdf) deleteLinkFromPage(page *PageObj, targetIndex int) bool {
	linkIdx := 0
	for i, objID := range page.LinkObjIds {
		if objID < 1 || objID-1 >= len(gp.pdfObjs) {
			continue
		}
		obj := gp.pdfObjs[objID-1]
		if _, ok := obj.(annotObj); ok {
			if linkIdx == targetIndex {
				page.LinkObjIds = append(page.LinkObjIds[:i], page.LinkObjIds[i+1:]...)
				return true
			}
			linkIdx++
		}
	}
	return false
}

// DeleteAllLinks removes all links from the current page. Returns the count removed.
func (gp *GoPdf) DeleteAllLinks() int {
	page := gp.pdfObjs[gp.curr.IndexOfPageObj].(*PageObj)
	return gp.deleteAllLinksFromPage(page)
}

// DeleteAllLinksOnPage removes all links from the specified page (1-indexed).
func (gp *GoPdf) DeleteAllLinksOnPage(pageNo int) int {
	page := gp.findPageObj(pageNo)
	if page == nil {
		return 0
	}
	return gp.deleteAllLinksFromPage(page)
}

func (gp *GoPdf) deleteAllLinksFromPage(page *PageObj) int {
	kept := make([]int, 0, len(page.LinkObjIds))
	removed := 0
	for _, objID := range page.LinkObjIds {
		if objID < 1 || objID-1 >= len(gp.pdfObjs) {
			kept = append(kept, objID)
			continue
		}
		obj := gp.pdfObjs[objID-1]
		if _, ok := obj.(annotObj); ok {
			removed++
			continue
		}
		kept = append(kept, objID)
	}
	page.LinkObjIds = kept
	return removed
}

// ============================================================
// Extract links from existing PDF data (static functions)
// ============================================================

// ExtractedLink represents a link extracted from an existing PDF.
type ExtractedLink struct {
	// PageIndex is the 0-based page index.
	PageIndex int
	// Rect is the link rectangle [x1, y1, x2, y2] in PDF coordinates.
	Rect [4]float64
	// URI is the external URL (if any).
	URI string
	// Destination is the internal destination (if any).
	Destination string
	// IsExternal is true for URI links.
	IsExternal bool
}

// Pre-compiled regexes for link extraction.
var (
	reLinkAnnot = regexp.MustCompile(`/Subtype\s*/Link`)
	reLinkURI   = regexp.MustCompile(`/URI\s*\(((?:[^)\\]|\\.)*)\)`)
	reLinkRect  = regexp.MustCompile(`/Rect\s*\[\s*(-?[\d.]+)\s+(-?[\d.]+)\s+(-?[\d.]+)\s+(-?[\d.]+)\s*\]`)
	reLinkDest  = regexp.MustCompile(`/Dest\s*\[([^\]]*)\]`)
)

// ExtractLinks extracts all links from all pages of the given PDF data.
//
// Example:
//
//	data, _ := os.ReadFile("input.pdf")
//	links, _ := gopdf.ExtractLinks(data)
//	for _, l := range links {
//	    fmt.Printf("Page %d: %s\n", l.PageIndex, l.URI)
//	}
func ExtractLinks(pdfData []byte) ([]ExtractedLink, error) {
	parser, err := newRawPDFParser(pdfData)
	if err != nil {
		return nil, err
	}

	var results []ExtractedLink
	for pageIdx, page := range parser.pages {
		links := extractLinksFromPage(parser, page, pageIdx)
		results = append(results, links...)
	}
	return results, nil
}

// ExtractLinksFromPage extracts links from a specific page (0-based).
func ExtractLinksFromPage(pdfData []byte, pageIndex int) ([]ExtractedLink, error) {
	parser, err := newRawPDFParser(pdfData)
	if err != nil {
		return nil, err
	}
	if pageIndex < 0 || pageIndex >= len(parser.pages) {
		return nil, fmt.Errorf("page index %d out of range", pageIndex)
	}
	return extractLinksFromPage(parser, parser.pages[pageIndex], pageIndex), nil
}

func extractLinksFromPage(parser *rawPDFParser, page rawPDFPage, pageIdx int) []ExtractedLink {
	var results []ExtractedLink

	// Get the page object dictionary.
	pageObj, ok := parser.objects[page.objNum]
	if !ok {
		return nil
	}

	// Look for /Annots in the page dictionary.
	annotRefs := extractRefArray(pageObj.dict, "/Annots")
	for _, ref := range annotRefs {
		obj, ok := parser.objects[ref]
		if !ok {
			continue
		}
		dict := obj.dict
		if !reLinkAnnot.MatchString(dict) {
			continue
		}

		link := ExtractedLink{PageIndex: pageIdx}

		// Extract rectangle.
		if m := reLinkRect.FindStringSubmatch(dict); m != nil {
			link.Rect[0], _ = strconv.ParseFloat(m[1], 64)
			link.Rect[1], _ = strconv.ParseFloat(m[2], 64)
			link.Rect[2], _ = strconv.ParseFloat(m[3], 64)
			link.Rect[3], _ = strconv.ParseFloat(m[4], 64)
		}

		// Extract URI.
		if m := reLinkURI.FindStringSubmatch(dict); m != nil {
			link.URI = m[1]
			link.IsExternal = true
		}

		// Extract destination.
		if m := reLinkDest.FindStringSubmatch(dict); m != nil {
			link.Destination = strings.TrimSpace(m[1])
		}

		results = append(results, link)
	}

	return results
}
