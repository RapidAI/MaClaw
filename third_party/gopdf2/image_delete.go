package gopdf

import (
	"fmt"
)

// DeleteImages removes all image elements from the specified page (1-based).
// Returns the number of images removed.
//
// Example:
//
//	pdf.SetPage(1)
//	n, err := pdf.DeleteImages(1)
//	fmt.Printf("Removed %d images\n", n)
func (gp *GoPdf) DeleteImages(pageNo int) (int, error) {
	return gp.DeleteElementsByType(pageNo, ElementImage)
}

// DeleteImagesFromAllPages removes all image elements from every page.
// Returns the total number of images removed.
//
// Example:
//
//	total, err := pdf.DeleteImagesFromAllPages()
func (gp *GoPdf) DeleteImagesFromAllPages() (int, error) {
	total := 0
	numPages := gp.GetNumberOfPages()
	for i := 1; i <= numPages; i++ {
		n, err := gp.DeleteImages(i)
		if err != nil {
			return total, fmt.Errorf("page %d: %w", i, err)
		}
		total += n
	}
	return total, nil
}

// DeleteImageByIndex removes a specific image element from a page.
// The imageIndex is the 0-based index among image elements only (not all elements).
//
// Example:
//
//	// Remove the first image on page 1
//	err := pdf.DeleteImageByIndex(1, 0)
func (gp *GoPdf) DeleteImageByIndex(pageNo int, imageIndex int) error {
	content := gp.findContentObj(pageNo)
	if content == nil {
		return fmt.Errorf("%w: page %d", ErrContentObjNotFound, pageNo)
	}

	imgCount := 0
	for i, c := range content.listCache.caches {
		if classifyElement(c) == ElementImage {
			if imgCount == imageIndex {
				return gp.DeleteElement(pageNo, i)
			}
			imgCount++
		}
	}
	return fmt.Errorf("image index %d out of range (found %d images)", imageIndex, imgCount)
}
