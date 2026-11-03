package gopdf

import "errors"

var ErrAnnotationNotFound = errors.New("annotation not found at specified index")

// ModifyAnnotation modifies an existing annotation on the specified page.
// pageIndex is 0-based, annotIndex is the position in the page's annotation list.
// Only non-zero fields in the provided option are applied.
//
// Example:
//
//	pdf.ModifyAnnotation(0, 0, gopdf.AnnotationOption{
//	    Content: "Updated note",
//	    Color:   [3]uint8{255, 0, 0},
//	})
func (gp *GoPdf) ModifyAnnotation(pageIndex, annotIndex int, opt AnnotationOption) error {
	page := gp.getPageByIndex(pageIndex)
	if page == nil {
		return ErrPageOutOfRange
	}
	if annotIndex < 0 || annotIndex >= len(page.LinkObjIds) {
		return ErrAnnotationNotFound
	}

	objID := page.LinkObjIds[annotIndex]
	objIdx := objID - 1
	if objIdx < 0 || objIdx >= len(gp.pdfObjs) {
		return ErrAnnotationNotFound
	}

	annot, ok := gp.pdfObjs[objIdx].(annotationObj)
	if !ok {
		return ErrAnnotationNotFound
	}

	// Apply non-zero fields from opt.
	if opt.Content != "" {
		annot.opt.Content = opt.Content
	}
	if opt.Title != "" {
		annot.opt.Title = opt.Title
	}
	if opt.Color != [3]uint8{0, 0, 0} {
		annot.opt.Color = opt.Color
	}
	if opt.Opacity > 0 {
		annot.opt.Opacity = opt.Opacity
	}
	if opt.FontSize > 0 {
		annot.opt.FontSize = opt.FontSize
	}
	if opt.BorderWidth > 0 {
		annot.opt.BorderWidth = opt.BorderWidth
	}
	if opt.X != 0 || opt.Y != 0 {
		if opt.X != 0 {
			annot.opt.X = opt.X
		}
		if opt.Y != 0 {
			annot.opt.Y = opt.Y
		}
	}
	if opt.W > 0 {
		annot.opt.W = opt.W
	}
	if opt.H > 0 {
		annot.opt.H = opt.H
	}
	if opt.InteriorColor != nil {
		annot.opt.InteriorColor = opt.InteriorColor
	}
	if opt.OverlayText != "" {
		annot.opt.OverlayText = opt.OverlayText
	}

	gp.pdfObjs[objIdx] = annot
	return nil
}

// getPageByIndex returns the PageObj at the given 0-based page index.
func (gp *GoPdf) getPageByIndex(pageIndex int) *PageObj {
	pageCount := 0
	for _, obj := range gp.pdfObjs {
		if p, ok := obj.(*PageObj); ok {
			if pageCount == pageIndex {
				return p
			}
			pageCount++
		}
	}
	return nil
}
