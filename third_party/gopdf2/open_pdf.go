package gopdf

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
)

// OpenPDFOption configures how an existing PDF is opened.
type OpenPDFOption struct {
	// Box specifies which PDF box to use when importing pages.
	// Valid values: "/MediaBox", "/CropBox", "/BleedBox", "/TrimBox", "/ArtBox".
	// Default: "/MediaBox".
	Box string

	// Protection sets password protection on the output PDF.
	Protection *PDFProtectionConfig

	// Password is the user or owner password for opening encrypted PDFs.
	// If the PDF is encrypted and no password is provided, OpenPDF returns
	// ErrEncryptedPDF. Supports RC4 encryption (V1/V2, R2/R3).
	Password string
}

func (o *OpenPDFOption) box() string {
	if o != nil && o.Box != "" {
		return o.Box
	}
	return "/MediaBox"
}

// OpenPDF opens an existing PDF file and imports all pages so that new
// content can be drawn on top of them. After calling OpenPDF, use
// SetPage(n) to switch to a specific page (1-based), then use any
// drawing method (Text, Cell, Image, InsertHTMLBox, Line, etc.) to
// overlay content. Finally call WritePdf to save.
//
// Example:
//
//	pdf := gopdf.GoPdf{}
//	err := pdf.OpenPDF("input.pdf", nil)
//	if err != nil { log.Fatal(err) }
//
//	pdf.AddTTFFont("myfont", "font.ttf")
//	pdf.SetFont("myfont", "", 14)
//
//	pdf.SetPage(1)
//	pdf.SetXY(100, 100)
//	pdf.Cell(nil, "Hello on page 1")
//
//	pdf.WritePdf("output.pdf")
func (gp *GoPdf) OpenPDF(pdfPath string, opt *OpenPDFOption) (retErr error) {
	defer func() {
		if r := recover(); r != nil {
			retErr = fmt.Errorf("failed to parse PDF: %v", r)
		}
	}()
	f, err := os.Open(pdfPath)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	return gp.openPDFFromData(data, opt)
}

// OpenPDFFromBytes opens an existing PDF from a byte slice.
func (gp *GoPdf) OpenPDFFromBytes(pdfData []byte, opt *OpenPDFOption) (retErr error) {
	defer func() {
		if r := recover(); r != nil {
			retErr = fmt.Errorf("failed to parse PDF: %v", r)
		}
	}()
	return gp.openPDFFromData(pdfData, opt)
}

// OpenPDFFromStream opens an existing PDF from an io.ReadSeeker.
func (gp *GoPdf) OpenPDFFromStream(rs *io.ReadSeeker, opt *OpenPDFOption) (retErr error) {
	defer func() {
		if r := recover(); r != nil {
			retErr = fmt.Errorf("failed to parse PDF: %v", r)
		}
	}()
	data, err := io.ReadAll(*rs)
	if err != nil {
		return err
	}
	return gp.openPDFFromData(data, opt)
}

func (gp *GoPdf) openPDFFromData(data []byte, opt *OpenPDFOption) error {
	box := opt.box()

	// Phase 0: detect and decrypt encrypted PDFs.
	if encObjNum := detectEncryption(data); encObjNum > 0 {
		password := ""
		if opt != nil {
			password = opt.Password
		}
		if password == "" {
			// Try empty password first (some PDFs have empty user password).
			password = ""
		}
		dc, err := authenticate(data, password)
		if err != nil {
			if opt == nil || opt.Password == "" {
				return ErrEncryptedPDF
			}
			return err
		}
		if dc != nil {
			data = decryptPDF(data, dc)
		}
	}

	// Phase 1: probe page count and sizes with the built-in importer.
	probe := newFpdiImporter()
	probeRS := io.ReadSeeker(bytes.NewReader(data))
	probe.SetSourceStream(&probeRS)

	numPages := probe.GetNumPages()
	if numPages == 0 {
		return errors.New("PDF has no pages")
	}

	sizes := probe.GetPageSizes()
	firstSize, ok := sizes[1][box]
	if !ok {
		return errors.New("cannot read page size from source PDF")
	}

	// Phase 2: initialize the GoPdf document.
	config := Config{
		PageSize: Rect{W: firstSize["w"], H: firstSize["h"]},
	}
	if opt != nil && opt.Protection != nil {
		config.Protection = *opt.Protection
	}
	gp.Start(config)

	// Phase 3: import each page as a template drawn as the page background.
	for i := 1; i <= numPages; i++ {
		pageSize, ok := sizes[i][box]
		if !ok {
			return errors.New("cannot read page size from source PDF")
		}
		w := pageSize["w"]
		h := pageSize["h"]

		// Create a fresh stream reader for each page import.
		rs := io.ReadSeeker(bytes.NewReader(data))
		gp.fpdi.SetSourceStream(&rs)

		gp.AddPageWithOption(PageOption{
			PageSize: &Rect{W: w, H: h},
		})

		startObjID := gp.GetNextObjectID()
		gp.fpdi.SetNextObjectID(startObjID)

		tpl := gp.fpdi.ImportPage(i, box)

		tplObjIDs := gp.fpdi.PutFormXobjects()
		gp.ImportTemplates(tplObjIDs)

		imported := gp.fpdi.GetImportedObjects()
		gp.ImportObjects(imported, startObjID)

		// Draw the imported page as the background.
		gp.UseImportedTemplate(tpl, 0, 0, w, h)
	}

	// Position on page 1 so the caller can start drawing immediately.
	return gp.SetPage(1)
}
