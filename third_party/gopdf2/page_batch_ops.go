package gopdf

import "errors"

// DeletePages removes multiple pages from the document in a single operation.
// Pages are specified as 1-based page numbers. Duplicate page numbers are
// ignored. Pages are deleted in reverse order to maintain correct numbering.
//
// Example:
//
//	pdf.DeletePages([]int{2, 4, 6}) // remove pages 2, 4, and 6
func (gp *GoPdf) DeletePages(pages []int) error {
	if len(pages) == 0 {
		return nil
	}
	numPages := gp.GetNumberOfPages()
	if numPages == 0 {
		return ErrNoPages
	}

	// Validate all page numbers first.
	for _, p := range pages {
		if p < 1 || p > numPages {
			return ErrPageOutOfRange
		}
	}

	// Deduplicate and sort descending so we delete from the end first.
	// This avoids page number shifting issues.
	seen := make(map[int]bool, len(pages))
	unique := make([]int, 0, len(pages))
	for _, p := range pages {
		if !seen[p] {
			seen[p] = true
			unique = append(unique, p)
		}
	}

	// Sort descending (simple insertion sort — page lists are small).
	for i := 1; i < len(unique); i++ {
		for j := i; j > 0 && unique[j] > unique[j-1]; j-- {
			unique[j], unique[j-1] = unique[j-1], unique[j]
		}
	}

	// Cannot delete all pages.
	if len(unique) >= numPages {
		return errors.New("cannot delete all pages from document")
	}

	// Delete each page from highest to lowest.
	for _, p := range unique {
		if err := gp.DeletePage(p); err != nil {
			return err
		}
	}
	return nil
}

// MovePage moves a page from one position to another.
// Both from and to are 1-based page numbers. The page at position 'from'
// is removed and re-inserted at position 'to'.
//
// This works by using SelectPages to reorder the page sequence.
//
// Example:
//
//	pdf.MovePage(3, 1) // move page 3 to become page 1
//	pdf.MovePage(1, 5) // move page 1 to position 5
func (gp *GoPdf) MovePage(from, to int) error {
	numPages := gp.GetNumberOfPages()
	if numPages == 0 {
		return ErrNoPages
	}
	if from < 1 || from > numPages {
		return ErrPageOutOfRange
	}
	if to < 1 || to > numPages {
		return ErrPageOutOfRange
	}
	if from == to {
		return nil // no-op
	}

	// Build the new page order.
	order := make([]int, 0, numPages)
	for i := 1; i <= numPages; i++ {
		if i != from {
			order = append(order, i)
		}
	}

	// Insert 'from' at position 'to-1' (0-based index).
	insertIdx := to - 1
	if from < to {
		// When moving forward, the removal shifts indices down by 1.
		insertIdx = to - 2
		if insertIdx < 0 {
			insertIdx = 0
		}
	}
	// Clamp to valid range.
	if insertIdx > len(order) {
		insertIdx = len(order)
	}

	// Insert at position.
	newOrder := make([]int, 0, numPages)
	newOrder = append(newOrder, order[:insertIdx]...)
	newOrder = append(newOrder, from)
	newOrder = append(newOrder, order[insertIdx:]...)

	// Use SelectPages to create a reordered document, then swap internals.
	reordered, err := gp.SelectPages(newOrder)
	if err != nil {
		return err
	}

	// Swap the internal state from the reordered document.
	gp.swapFrom(reordered)
	return nil
}

// swapFrom replaces the receiver's internal state with that of another GoPdf.
// This is used by MovePage to apply the reordered document.
func (gp *GoPdf) swapFrom(other *GoPdf) {
	gp.pdfObjs = other.pdfObjs
	gp.config = other.config
	gp.indexOfCatalogObj = other.indexOfCatalogObj
	gp.indexOfPagesObj = other.indexOfPagesObj
	gp.numOfPagesObj = other.numOfPagesObj
	gp.indexOfFirstPageObj = other.indexOfFirstPageObj
	gp.indexEncodingObjFonts = other.indexEncodingObjFonts
	gp.indexOfContent = other.indexOfContent
	gp.indexOfProcSet = other.indexOfProcSet
	gp.buf = other.buf
	gp.pdfProtection = other.pdfProtection
	gp.encryptionObjID = other.encryptionObjID
	gp.compressLevel = other.compressLevel
	gp.isUseInfo = other.isUseInfo
	gp.info = other.info
	gp.outlines = other.outlines
	gp.indexOfOutlinesObj = other.indexOfOutlinesObj
	gp.fpdi = other.fpdi
	gp.margins = other.margins
	gp.anchors = other.anchors
	gp.placeHolderTexts = other.placeHolderTexts
	gp.embeddedFiles = other.embeddedFiles
	gp.pdfVersion = other.pdfVersion
	gp.pageLabels = other.pageLabels
	gp.xmpMetadata = other.xmpMetadata
	gp.ocgs = other.ocgs
	gp.formFields = other.formFields

	// Copy Current fields individually to avoid copying the sync.Mutex
	// inside SMaskMap / ExtGStatesMap / TransparencyMap.
	gp.curr.setXCount = other.curr.setXCount
	gp.curr.X = other.curr.X
	gp.curr.Y = other.curr.Y
	gp.curr.IndexOfFontObj = other.curr.IndexOfFontObj
	gp.curr.CountOfFont = other.curr.CountOfFont
	gp.curr.CountOfL = other.curr.CountOfL
	gp.curr.FontSize = other.curr.FontSize
	gp.curr.FontStyle = other.curr.FontStyle
	gp.curr.FontFontCount = other.curr.FontFontCount
	gp.curr.FontType = other.curr.FontType
	gp.curr.IndexOfColorSpaceObj = other.curr.IndexOfColorSpaceObj
	gp.curr.CountOfColorSpace = other.curr.CountOfColorSpace
	gp.curr.CharSpacing = other.curr.CharSpacing
	gp.curr.FontISubset = other.curr.FontISubset
	gp.curr.IndexOfPageObj = other.curr.IndexOfPageObj
	gp.curr.CountOfImg = other.curr.CountOfImg
	gp.curr.ImgCaches = other.curr.ImgCaches
	gp.curr.txtColorMode = other.curr.txtColorMode
	gp.curr.grayFill = other.curr.grayFill
	gp.curr.grayStroke = other.curr.grayStroke
	gp.curr.lineWidth = other.curr.lineWidth
	gp.curr.pageSize = other.curr.pageSize
	gp.curr.trimBox = other.curr.trimBox
	gp.curr.transparency = other.curr.transparency
}
