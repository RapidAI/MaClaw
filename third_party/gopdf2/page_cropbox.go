package gopdf

// SetPageCropBox sets the CropBox for a page. The CropBox defines the visible
// area of the page when displayed or printed. Content outside the CropBox is
// clipped (hidden) but not removed.
//
// pageNo is 1-based. The box coordinates are in document units:
//   - left, top: the lower-left corner of the crop area
//   - right, bottom: the upper-right corner of the crop area
//
// In PDF coordinate system, (0,0) is the bottom-left of the page.
//
// Example:
//
//	// Crop page 1 to show only the center area
//	pdf.SetPageCropBox(1, Box{Left: 50, Top: 50, Right: 545, Bottom: 792})
//
//	// Remove crop box from page 1
//	pdf.ClearPageCropBox(1)
func (gp *GoPdf) SetPageCropBox(pageNo int, box Box) error {
	numPages := gp.GetNumberOfPages()
	if pageNo < 1 || pageNo > numPages {
		return ErrPageOutOfRange
	}
	page := gp.findPageObj(pageNo)
	if page == nil {
		return ErrPageOutOfRange
	}
	page.cropBox = &Box{
		Left:   box.Left,
		Top:    box.Top,
		Right:  box.Right,
		Bottom: box.Bottom,
	}
	return nil
}

// GetPageCropBox returns the CropBox for a page, or nil if no CropBox is set.
// pageNo is 1-based.
func (gp *GoPdf) GetPageCropBox(pageNo int) (*Box, error) {
	numPages := gp.GetNumberOfPages()
	if pageNo < 1 || pageNo > numPages {
		return nil, ErrPageOutOfRange
	}
	page := gp.findPageObj(pageNo)
	if page == nil {
		return nil, ErrPageOutOfRange
	}
	if page.cropBox == nil {
		return nil, nil
	}
	// Return a copy to prevent external mutation.
	b := *page.cropBox
	return &b, nil
}

// ClearPageCropBox removes the CropBox from a page, restoring the full
// MediaBox as the visible area.
// pageNo is 1-based.
func (gp *GoPdf) ClearPageCropBox(pageNo int) error {
	numPages := gp.GetNumberOfPages()
	if pageNo < 1 || pageNo > numPages {
		return ErrPageOutOfRange
	}
	page := gp.findPageObj(pageNo)
	if page == nil {
		return ErrPageOutOfRange
	}
	page.cropBox = nil
	return nil
}
