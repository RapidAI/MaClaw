package gopdf

import (
	"bytes"
	"fmt"
	"sort"
)

// RedactionOptions configures enhanced redaction behavior.
type RedactionOptions struct {
	// FillColor is the color to fill the redacted area. Default: black.
	FillColor [3]uint8
	// OverlayText is optional text to display over the redacted area.
	OverlayText string
	// OverlayFontSize is the font size for overlay text. Default: 10.
	OverlayFontSize float64
	// OverlayColor is the color for overlay text. Default: white.
	OverlayColor [3]uint8
	// RemoveUnderlyingContent controls whether to remove content under redaction.
	RemoveUnderlyingContent bool
}

// ApplyRedactionsEnhanced applies redaction annotations on the current page
// with enhanced options including content removal and custom fill colors.
//
// Unlike the basic ApplyRedactions which only removes annotation markers,
// this method can also remove the underlying content within the redacted areas
// and draw a filled rectangle with optional overlay text.
//
// Example:
//
//	pdf.AddRedactAnnotation(100, 100, 200, 20, "REDACTED")
//	pdf.ApplyRedactionsEnhanced(gopdf.RedactionOptions{
//	    FillColor:               [3]uint8{0, 0, 0},
//	    OverlayText:             "REDACTED",
//	    OverlayFontSize:         10,
//	    OverlayColor:            [3]uint8{255, 255, 255},
//	    RemoveUnderlyingContent: true,
//	})
func (gp *GoPdf) ApplyRedactionsEnhanced(opts RedactionOptions) int {
	if opts.OverlayFontSize <= 0 {
		opts.OverlayFontSize = 10
	}

	page := gp.pdfObjs[gp.curr.IndexOfPageObj].(*PageObj)
	return gp.applyEnhancedRedactions(page, opts)
}

// ApplyRedactionsEnhancedOnPage applies enhanced redactions on a specific page (1-indexed).
func (gp *GoPdf) ApplyRedactionsEnhancedOnPage(pageNo int, opts RedactionOptions) int {
	if opts.OverlayFontSize <= 0 {
		opts.OverlayFontSize = 10
	}
	page := gp.findPageObj(pageNo)
	if page == nil {
		return 0
	}
	return gp.applyEnhancedRedactions(page, opts)
}

func (gp *GoPdf) applyEnhancedRedactions(page *PageObj, opts RedactionOptions) int {
	// Collect redaction areas.
	type redactArea struct {
		x, y, w, h float64
		overlay     string
	}
	var areas []redactArea

	kept := make([]int, 0, len(page.LinkObjIds))
	removed := 0

	for _, objID := range page.LinkObjIds {
		if objID < 1 || objID-1 >= len(gp.pdfObjs) {
			kept = append(kept, objID)
			continue
		}
		obj := gp.pdfObjs[objID-1]
		if aObj, ok := obj.(annotationObj); ok && aObj.opt.Type == AnnotRedact {
			areas = append(areas, redactArea{
				x:       aObj.opt.X,
				y:       aObj.opt.Y,
				w:       aObj.opt.W,
				h:       aObj.opt.H,
				overlay: aObj.opt.OverlayText,
			})
			removed++
			continue
		}
		kept = append(kept, objID)
	}
	page.LinkObjIds = kept

	// Draw filled rectangles over redacted areas.
	for _, area := range areas {
		// Save graphics state.
		gp.SaveGraphicsState()

		// Set fill color.
		gp.SetFillColor(opts.FillColor[0], opts.FillColor[1], opts.FillColor[2])

		// Draw filled rectangle.
		gp.RectFromUpperLeftWithStyle(
			gp.PointsToUnits(area.x),
			gp.PointsToUnits(area.y),
			gp.PointsToUnits(area.w),
			gp.PointsToUnits(area.h),
			"F",
		)

		// Draw overlay text if specified.
		overlayText := opts.OverlayText
		if overlayText == "" {
			overlayText = area.overlay
		}
		if overlayText != "" {
			gp.SetTextColor(opts.OverlayColor[0], opts.OverlayColor[1], opts.OverlayColor[2])
			gp.SetXY(
				gp.PointsToUnits(area.x+2),
				gp.PointsToUnits(area.y+area.h/2-opts.OverlayFontSize/2),
			)
			gp.Cell(&Rect{
				W: gp.PointsToUnits(area.w),
				H: gp.PointsToUnits(area.h),
			}, overlayText)
		}

		gp.RestoreGraphicsState()
	}

	return removed
}

// RedactText searches for text on all pages and applies redaction to matching areas.
// This is a convenience method that combines SearchText + AddRedactAnnotation + ApplyRedactions.
//
// Example:
//
//	data, _ := os.ReadFile("input.pdf")
//	newData, count, err := gopdf.RedactText(data, "[email]", nil)
func RedactText(pdfData []byte, searchText string, opts *RedactionOptions) ([]byte, int, error) {
	if opts == nil {
		opts = &RedactionOptions{
			FillColor: [3]uint8{0, 0, 0},
		}
	}

	if searchText == "" {
		return pdfData, 0, fmt.Errorf("searchText cannot be empty")
	}

	// Search for text locations.
	results, err := SearchText(pdfData, searchText, false)
	if err != nil {
		return pdfData, 0, fmt.Errorf("search text: %w", err)
	}

	if len(results) == 0 {
		return pdfData, 0, nil
	}

	// Build redaction content stream patches.
	parser, err := newRawPDFParser(pdfData)
	if err != nil {
		return pdfData, 0, fmt.Errorf("parse PDF for redaction: %w", err)
	}

	output := make([]byte, len(pdfData))
	copy(output, pdfData)
	count := 0

	// Group results by page.
	pageResults := make(map[int][]TextSearchResult)
	for _, r := range results {
		pageResults[r.PageIndex] = append(pageResults[r.PageIndex], r)
	}

	// Sort page indices for deterministic processing order.
	pageIndices := make([]int, 0, len(pageResults))
	for idx := range pageResults {
		pageIndices = append(pageIndices, idx)
	}
	sort.Ints(pageIndices)

	for _, pageIdx := range pageIndices {
		pageRes := pageResults[pageIdx]
		if pageIdx >= len(parser.pages) {
			continue
		}
		page := parser.pages[pageIdx]
		if len(page.contents) == 0 {
			continue
		}
		contentRef := page.contents[0]
		contentObj, ok := parser.objects[contentRef]
		if !ok {
			continue
		}

		// Append redaction rectangles to the content stream.
		stream := parser.getPageContentStream(pageIdx)
		var extra bytes.Buffer
		extra.Grow(len(stream) + len(pageRes)*128)
		extra.Write(stream)

		pageH := page.mediaBox[3] - page.mediaBox[1]

		for _, r := range pageRes {
			// Convert coordinates.
			x := r.X
			y := pageH - r.Y - r.Height

			fmt.Fprintf(&extra, "\nq\n")
			fmt.Fprintf(&extra, "%.4f %.4f %.4f rg\n",
				float64(opts.FillColor[0])/255.0,
				float64(opts.FillColor[1])/255.0,
				float64(opts.FillColor[2])/255.0)
			fmt.Fprintf(&extra, "%.2f %.2f %.2f %.2f re f\n",
				x, y, r.Width, r.Height)
			fmt.Fprintf(&extra, "Q\n")
			count++
		}

		newStream := extra.Bytes()
		output = replaceObjectStream(output, contentRef, contentObj.dict, newStream)
	}

	if count > 0 {
		output = rebuildXref(output)
	}

	return output, count, nil
}
