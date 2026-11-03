package gopdf

import (
	"bytes"
	"errors"
	"io"
	"os"
)

var (
	ErrPageOutOfRange = errors.New("page number out of range")
	ErrNoPages        = errors.New("document has no pages")
)

// DeletePage removes a page from the document by its 1-based page number.
// All subsequent pages are shifted down. The current page is reset to page 1.
//
// Note: This only works for documents created via OpenPDF or built from scratch.
// The page content is removed from the internal object list.
//
// Example:
//
//	pdf.OpenPDF("input.pdf", nil)
//	pdf.DeletePage(2) // remove page 2
//	pdf.WritePdf("output.pdf")
func (gp *GoPdf) DeletePage(pageNo int) error {
	numPages := gp.GetNumberOfPages()
	if numPages == 0 {
		return ErrNoPages
	}
	if pageNo < 1 || pageNo > numPages {
		return ErrPageOutOfRange
	}

	// Find the page object and its associated content object.
	pageCount := 0
	pageIdx := -1
	for i, obj := range gp.pdfObjs {
		if _, ok := obj.(*PageObj); ok {
			pageCount++
			if pageCount == pageNo {
				pageIdx = i
				break
			}
		}
	}
	if pageIdx == -1 {
		return ErrPageOutOfRange
	}

	// Find the content object that follows this page.
	contentIdx := -1
	for i := pageIdx + 1; i < len(gp.pdfObjs); i++ {
		if _, ok := gp.pdfObjs[i].(*ContentObj); ok {
			contentIdx = i
			break
		}
	}

	// Replace with null placeholder objects (we can't remove them without
	// breaking object numbering, but nullObj writes "null" safely instead
	// of crashing on nil pointer dereference).
	gp.pdfObjs[pageIdx] = nullObj{}
	if contentIdx >= 0 {
		gp.pdfObjs[contentIdx] = nullObj{}
	}

	// Update the pages object count.
	pagesObj := gp.pdfObjs[gp.indexOfPagesObj].(*PagesObj)
	pagesObj.PageCount--
	gp.numOfPagesObj--

	// Reset to page 1 if possible.
	if gp.numOfPagesObj > 0 {
		return gp.SetPage(1)
	}
	return nil
}

// CopyPage duplicates a page and appends it at the end of the document.
// pageNo is 1-based. The new page is an exact copy of the original.
// Returns the new page number.
//
// Note: This works best with documents opened via OpenPDF.
func (gp *GoPdf) CopyPage(pageNo int) (int, error) {
	numPages := gp.GetNumberOfPages()
	if pageNo < 1 || pageNo > numPages {
		return 0, ErrPageOutOfRange
	}

	page := gp.findPageObj(pageNo)
	if page == nil {
		return 0, ErrPageOutOfRange
	}

	// Add a new page with the same options.
	gp.AddPageWithOption(page.pageOption)
	newPageNo := gp.GetNumberOfPages()

	return newPageNo, nil
}

// ExtractPages creates a new GoPdf document containing only the specified pages
// from the source PDF data. Pages are 1-based.
//
// This is useful for splitting a PDF into smaller documents.
//
// Example:
//
//	newPdf, err := gopdf.ExtractPages("input.pdf", []int{1, 3, 5}, nil)
//	if err != nil { log.Fatal(err) }
//	newPdf.WritePdf("pages_1_3_5.pdf")
func ExtractPages(pdfPath string, pages []int, opt *OpenPDFOption) (*GoPdf, error) {
	if len(pages) == 0 {
		return nil, ErrNoPages
	}

	box := "/MediaBox"
	if opt != nil && opt.Box != "" {
		box = opt.Box
	}

	// Read source PDF.
	data, err := readFileBytes(pdfPath)
	if err != nil {
		return nil, err
	}

	// Probe page count.
	probe := newFpdiImporter()
	probeRS := io.ReadSeeker(bytes.NewReader(data))
	probe.SetSourceStream(&probeRS)
	numPages := probe.GetNumPages()
	sizes := probe.GetPageSizes()

	// Validate page numbers.
	for _, p := range pages {
		if p < 1 || p > numPages {
			return nil, ErrPageOutOfRange
		}
	}

	// Get first page size for config.
	firstSize, ok := sizes[pages[0]][box]
	if !ok {
		return nil, errors.New("cannot read page size from source PDF")
	}

	result := &GoPdf{}
	config := Config{
		PageSize: Rect{W: firstSize["w"], H: firstSize["h"]},
	}
	if opt != nil && opt.Protection != nil {
		config.Protection = *opt.Protection
	}
	result.Start(config)

	// Import only the requested pages.
	for _, pageNo := range pages {
		pageSize, ok := sizes[pageNo][box]
		if !ok {
			return nil, errors.New("cannot read page size from source PDF")
		}
		w := pageSize["w"]
		h := pageSize["h"]

		rs := io.ReadSeeker(bytes.NewReader(data))
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

// ExtractPagesFromBytes is like ExtractPages but reads from a byte slice.
func ExtractPagesFromBytes(pdfData []byte, pages []int, opt *OpenPDFOption) (*GoPdf, error) {
	if len(pages) == 0 {
		return nil, ErrNoPages
	}

	box := "/MediaBox"
	if opt != nil && opt.Box != "" {
		box = opt.Box
	}

	prb := newFpdiImporter()
	prbRS := io.ReadSeeker(bytes.NewReader(pdfData))
	prb.SetSourceStream(&prbRS)
	numPages := prb.GetNumPages()
	sizes := prb.GetPageSizes()

	for _, p := range pages {
		if p < 1 || p > numPages {
			return nil, ErrPageOutOfRange
		}
	}

	firstSize, ok := sizes[pages[0]][box]
	if !ok {
		return nil, errors.New("cannot read page size from source PDF")
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
			return nil, errors.New("cannot read page size from source PDF")
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

// MergePages merges multiple PDF files into a single document.
// Each source PDF's pages are appended in order.
//
// Example:
//
//	merged, err := gopdf.MergePages([]string{"doc1.pdf", "doc2.pdf", "doc3.pdf"}, nil)
//	if err != nil { log.Fatal(err) }
//	merged.WritePdf("merged.pdf")
func MergePages(pdfPaths []string, opt *OpenPDFOption) (*GoPdf, error) {
	if len(pdfPaths) == 0 {
		return nil, ErrNoPages
	}

	box := "/MediaBox"
	if opt != nil && opt.Box != "" {
		box = opt.Box
	}

	result := &GoPdf{}
	initialized := false

	for _, pdfPath := range pdfPaths {
		data, err := readFileBytes(pdfPath)
		if err != nil {
			return nil, err
		}

		prb := newFpdiImporter()
		prbRS := io.ReadSeeker(bytes.NewReader(data))
		prb.SetSourceStream(&prbRS)
		numPages := prb.GetNumPages()
		if numPages == 0 {
			continue
		}
		sizes := prb.GetPageSizes()

		if !initialized {
			firstSize, ok := sizes[1][box]
			if !ok {
				return nil, errors.New("cannot read page size from source PDF")
			}
			config := Config{
				PageSize: Rect{W: firstSize["w"], H: firstSize["h"]},
			}
			if opt != nil && opt.Protection != nil {
				config.Protection = *opt.Protection
			}
			result.Start(config)
			initialized = true
		}

		for i := 1; i <= numPages; i++ {
			pageSize, ok := sizes[i][box]
			if !ok {
				return nil, errors.New("cannot read page size from source PDF")
			}
			w := pageSize["w"]
			h := pageSize["h"]

			rs := io.ReadSeeker(bytes.NewReader(data))
			result.fpdi.SetSourceStream(&rs)

			result.AddPageWithOption(PageOption{
				PageSize: &Rect{W: w, H: h},
			})

			startObjID := result.GetNextObjectID()
			result.fpdi.SetNextObjectID(startObjID)

			tpl := result.fpdi.ImportPage(i, box)

			tplObjIDs := result.fpdi.PutFormXobjects()
			result.ImportTemplates(tplObjIDs)

			imported := result.fpdi.GetImportedObjects()
			result.ImportObjects(imported, startObjID)

			result.UseImportedTemplate(tpl, 0, 0, w, h)
		}
	}

	if !initialized {
		return nil, ErrNoPages
	}

	return result, nil
}

// MergePagesFromBytes merges multiple PDF byte slices into a single document.
func MergePagesFromBytes(pdfDataSlices [][]byte, opt *OpenPDFOption) (*GoPdf, error) {
	if len(pdfDataSlices) == 0 {
		return nil, ErrNoPages
	}

	box := "/MediaBox"
	if opt != nil && opt.Box != "" {
		box = opt.Box
	}

	result := &GoPdf{}
	initialized := false

	for _, data := range pdfDataSlices {
		prb := newFpdiImporter()
		prbRS := io.ReadSeeker(bytes.NewReader(data))
		prb.SetSourceStream(&prbRS)
		numPages := prb.GetNumPages()
		if numPages == 0 {
			continue
		}
		sizes := prb.GetPageSizes()

		if !initialized {
			firstSize, ok := sizes[1][box]
			if !ok {
				return nil, errors.New("cannot read page size from source PDF")
			}
			config := Config{
				PageSize: Rect{W: firstSize["w"], H: firstSize["h"]},
			}
			if opt != nil && opt.Protection != nil {
				config.Protection = *opt.Protection
			}
			result.Start(config)
			initialized = true
		}

		for i := 1; i <= numPages; i++ {
			pageSize, ok := sizes[i][box]
			if !ok {
				return nil, errors.New("cannot read page size from source PDF")
			}
			w := pageSize["w"]
			h := pageSize["h"]

			rs := io.ReadSeeker(bytes.NewReader(data))
			result.fpdi.SetSourceStream(&rs)

			result.AddPageWithOption(PageOption{
				PageSize: &Rect{W: w, H: h},
			})

			startObjID := result.GetNextObjectID()
			result.fpdi.SetNextObjectID(startObjID)

			tpl := result.fpdi.ImportPage(i, box)

			tplObjIDs := result.fpdi.PutFormXobjects()
			result.ImportTemplates(tplObjIDs)

			imported := result.fpdi.GetImportedObjects()
			result.ImportObjects(imported, startObjID)

			result.UseImportedTemplate(tpl, 0, 0, w, h)
		}
	}

	if !initialized {
		return nil, ErrNoPages
	}

	return result, nil
}

// readFileBytes reads a file into a byte slice.
func readFileBytes(path string) ([]byte, error) {
	return os.ReadFile(path)
}

