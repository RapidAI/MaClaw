package gopdf

import (
	"bytes"
	"errors"
	"io"
)

// PageInfo contains information about a PDF page.
type PageInfo struct {
	// Width is the page width in points.
	Width float64
	// Height is the page height in points.
	Height float64
	// PageNumber is the 1-based page number.
	PageNumber int
}

// GetPageSize returns the size of the specified page (1-based).
// Returns width and height in document units.
func (gp *GoPdf) GetPageSize(pageNo int) (w, h float64, err error) {
	numPages := gp.GetNumberOfPages()
	if pageNo < 1 || pageNo > numPages {
		return 0, 0, ErrPageOutOfRange
	}

	page := gp.findPageObj(pageNo)
	if page == nil {
		return 0, 0, ErrPageOutOfRange
	}

	if !page.pageOption.isEmpty() {
		return gp.PointsToUnits(page.pageOption.PageSize.W),
			gp.PointsToUnits(page.pageOption.PageSize.H), nil
	}

	return gp.PointsToUnits(gp.config.PageSize.W),
		gp.PointsToUnits(gp.config.PageSize.H), nil
}

// findPageObj finds the n-th PageObj (1-based) in the pdfObjs slice.
func (gp *GoPdf) findPageObj(pageNo int) *PageObj {
	count := 0
	for _, obj := range gp.pdfObjs {
		if p, ok := obj.(*PageObj); ok {
			count++
			if count == pageNo {
				return p
			}
		}
	}
	return nil
}

// GetAllPageSizes returns the sizes of all pages in the document.
func (gp *GoPdf) GetAllPageSizes() []PageInfo {
	numPages := gp.GetNumberOfPages()
	result := make([]PageInfo, 0, numPages)

	for i := 1; i <= numPages; i++ {
		w, h, err := gp.GetPageSize(i)
		if err != nil {
			continue
		}
		result = append(result, PageInfo{
			Width:      w,
			Height:     h,
			PageNumber: i,
		})
	}
	return result
}

// GetSourcePDFPageCount returns the number of pages in a source PDF file
// without importing it.
func GetSourcePDFPageCount(pdfPath string) (int, error) {
	data, err := readFileBytes(pdfPath)
	if err != nil {
		return 0, err
	}
	return GetSourcePDFPageCountFromBytes(data)
}

// GetSourcePDFPageCountFromBytes returns the number of pages in a source PDF
// from a byte slice without importing it.
func GetSourcePDFPageCountFromBytes(pdfData []byte) (int, error) {
	n, err := GetSourcePDFPageCountV2(pdfData)
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, errors.New("PDF has no pages or is invalid")
	}
	return n, nil
}

// GetSourcePDFPageSizes returns the page sizes of all pages in a source PDF file.
// The returned map is keyed by 1-based page number.
func GetSourcePDFPageSizes(pdfPath string) (map[int]PageInfo, error) {
	data, err := readFileBytes(pdfPath)
	if err != nil {
		return nil, err
	}
	return GetSourcePDFPageSizesFromBytes(data)
}

// GetSourcePDFPageSizesFromBytes returns the page sizes from a PDF byte slice.
func GetSourcePDFPageSizesFromBytes(pdfData []byte) (map[int]PageInfo, error) {
	imp := newFpdiImporter()
	rs := io.ReadSeeker(bytes.NewReader(pdfData))
	imp.SetSourceStream(&rs)

	numPages := imp.GetNumPages()
	if numPages == 0 {
		return nil, errors.New("PDF has no pages or is invalid")
	}

	sizes := imp.GetPageSizes()
	result := make(map[int]PageInfo, numPages)

	for i := 1; i <= numPages; i++ {
		mediaBox, ok := sizes[i]["/MediaBox"]
		if !ok {
			continue
		}
		result[i] = PageInfo{
			Width:      mediaBox["w"],
			Height:     mediaBox["h"],
			PageNumber: i,
		}
	}

	return result, nil
}
