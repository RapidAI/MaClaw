package gopdf

import "errors"

// ErrInvalidRotation is returned when a rotation angle is not a multiple of 90.
var ErrInvalidRotation = errors.New("rotation must be a multiple of 90 degrees (0, 90, 180, 270)")

// SetPageRotation sets the display rotation for a page.
// pageNo is 1-based. angle must be a multiple of 90 (0, 90, 180, 270).
// This sets the /Rotate entry in the page dictionary, which tells PDF viewers
// how to display the page. It does not modify the page content.
//
// Example:
//
//	pdf.SetPageRotation(1, 90)  // rotate page 1 by 90 degrees clockwise
func (gp *GoPdf) SetPageRotation(pageNo int, angle int) error {
	numPages := gp.GetNumberOfPages()
	if pageNo < 1 || pageNo > numPages {
		return ErrPageOutOfRange
	}
	if angle%90 != 0 {
		return ErrInvalidRotation
	}
	// Normalize to 0-359 range.
	angle = ((angle % 360) + 360) % 360

	page := gp.findPageObj(pageNo)
	if page == nil {
		return ErrPageOutOfRange
	}
	page.rotation = angle
	return nil
}

// GetPageRotation returns the display rotation angle for a page.
// pageNo is 1-based. Returns 0 if no rotation is set.
func (gp *GoPdf) GetPageRotation(pageNo int) (int, error) {
	numPages := gp.GetNumberOfPages()
	if pageNo < 1 || pageNo > numPages {
		return 0, ErrPageOutOfRange
	}
	page := gp.findPageObj(pageNo)
	if page == nil {
		return 0, ErrPageOutOfRange
	}
	return page.rotation, nil
}
