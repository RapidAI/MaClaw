package gopdf

import (
	"bytes"
	"io"
)

// SelectPages rearranges the document to contain only the specified pages
// in the given order. Pages are 1-based and may be repeated.
//
// This works by exporting the current document to bytes, then re-importing
// only the selected pages in the specified order.
//
// Example:
//
//	// Reverse a 3-page document
//	newPdf, err := pdf.SelectPages([]int{3, 2, 1})
//
//	// Duplicate page 1 three times
//	newPdf, err := pdf.SelectPages([]int{1, 1, 1})
func (gp *GoPdf) SelectPages(pages []int) (*GoPdf, error) {
	if len(pages) == 0 {
		return nil, ErrNoPages
	}

	numPages := gp.GetNumberOfPages()
	for _, p := range pages {
		if p < 1 || p > numPages {
			return nil, ErrPageOutOfRange
		}
	}

	// Export current document to bytes.
	data, err := gp.GetBytesPdfReturnErr()
	if err != nil {
		return nil, err
	}

	return selectPagesFromBytes(data, pages, nil)
}

// SelectPagesFromFile creates a new GoPdf with pages from a PDF file
// rearranged in the specified order. Pages are 1-based and may be repeated.
//
// Example:
//
//	newPdf, err := gopdf.SelectPagesFromFile("input.pdf", []int{3, 1, 2}, nil)
//	newPdf.WritePdf("reordered.pdf")
func SelectPagesFromFile(pdfPath string, pages []int, opt *OpenPDFOption) (*GoPdf, error) {
	if len(pages) == 0 {
		return nil, ErrNoPages
	}

	data, err := readFileBytes(pdfPath)
	if err != nil {
		return nil, err
	}

	return selectPagesFromBytes(data, pages, opt)
}

// SelectPagesFromBytes creates a new GoPdf with pages from PDF bytes
// rearranged in the specified order.
func SelectPagesFromBytes(pdfData []byte, pages []int, opt *OpenPDFOption) (*GoPdf, error) {
	if len(pages) == 0 {
		return nil, ErrNoPages
	}
	return selectPagesFromBytes(pdfData, pages, opt)
}

func selectPagesFromBytes(pdfData []byte, pages []int, opt *OpenPDFOption) (*GoPdf, error) {
	box := "/MediaBox"
	if opt != nil && opt.Box != "" {
		box = opt.Box
	}

	// Probe page count and sizes.
	probe := newFpdiImporter()
	probeRS := io.ReadSeeker(bytes.NewReader(pdfData))
	probe.SetSourceStream(&probeRS)
	numPages := probe.GetNumPages()
	sizes := probe.GetPageSizes()

	for _, p := range pages {
		if p < 1 || p > numPages {
			return nil, ErrPageOutOfRange
		}
	}

	// Use first selected page's size for config.
	firstSize, ok := sizes[pages[0]][box]
	if !ok {
		firstSize, ok = sizes[pages[0]]["/MediaBox"]
		if !ok {
			return nil, ErrNoPages
		}
	}

	result := &GoPdf{}
	config := Config{
		PageSize: Rect{W: firstSize["w"], H: firstSize["h"]},
	}
	if opt != nil && opt.Protection != nil {
		config.Protection = *opt.Protection
	}
	result.Start(config)

	for _, pageNo := range pages {
		pageSize, ok := sizes[pageNo][box]
		if !ok {
			pageSize = sizes[pageNo]["/MediaBox"]
		}
		w := pageSize["w"]
		h := pageSize["h"]

		rs := io.ReadSeeker(bytes.NewReader(pdfData))
		result.fpdi.SetSourceStream(&rs)

		result.AddPageWithOption(PageOption{
			PageSize: &Rect{W: w, H: h},
		})

		startObjID := result.GetNextObjectID()
		result.fpdi.SetNextObjectID(startObjID)

		tpl := result.fpdi.ImportPage(pageNo, box)

		tplObjIDs := result.fpdi.PutFormXobjects()
		result.ImportTemplates(tplObjIDs)

		imported := result.fpdi.GetImportedObjects()
		result.ImportObjects(imported, startObjID)

		result.UseImportedTemplate(tpl, 0, 0, w, h)
	}

	return result, nil
}
