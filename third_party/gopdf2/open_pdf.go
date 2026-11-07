package gopdf

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
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
	// ErrEncryptedPDF. Supports RC4 (V1/V2, R2/R3) for OpenPDF modification; AES extraction via WithPassword APIs.
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

	// Import AcroForm form fields from the original document so
	// GetFormFields and ModifyFormFieldValue work after OpenPDF.
	importFormFieldsFromPDF(data, gp)

	// Position on page 1 so the caller can start drawing immediately.
	return gp.SetPage(1)
}

// importFormFieldsFromPDF parses AcroForm fields from the original PDF data
// and registers them in the GoPdf instance so GetFormFields and
// ModifyFormFieldValue work after OpenPDF.
func importFormFieldsFromPDF(data []byte, gp *GoPdf) {
	parser, err := newRawPDFParser(data)
	if err != nil {
		return
	}
	// find catalog → /AcroForm → /Fields
	catalog, ok := parser.objects[parser.root]
	if !ok {
		return
	}
	acroRef := extractRef(catalog.dict, "/AcroForm")
	if acroRef <= 0 {
		return
	}
	acroObj, ok := parser.objects[acroRef]
	if !ok {
		return
	}
	// parse /Fields array
	fieldsArr := extractRefArray(acroObj.dict, "/Fields")
	for _, fieldRef := range fieldsArr {
		fieldObj, ok := parser.objects[fieldRef]
		if !ok {
			continue
		}
		dict := fieldObj.dict
		// field name (/T)
		name := extractStringFromDict(dict, "/T")
		if name == "" {
			continue
		}
		// field value (/V)
		value := extractStringFromDict(dict, "/V")
		// field type (/FT)
		ft := extractName(dict, "/FT")
		var ftype FormFieldType
		switch ft {
		case "Tx":
			ftype = FormFieldText
		case "Btn":
			ftype = FormFieldButton
		case "Ch":
			ftype = FormFieldChoice
		default:
			ftype = FormFieldText
		}
		// position (/Rect [x1 y1 x2 y2])
		rect := extractNumberArray(dict, "/Rect")
		var x, y, w, h float64
		if len(rect) >= 4 {
			x = rect[0]
			y = rect[1]
			w = rect[2] - rect[0]
			h = rect[3] - rect[1]
		}
		// create GoPdf form field object
		ff := formFieldObj{
			field: FormField{
				Name:     name,
				Type:     ftype,
				X:        x,
				Y:        y,
				W:        w,
				H:        h,
				Value:    value,
				FontSize: 12,
			},
			pageRef: 1,
		}
		objIdx := gp.addObj(ff)
		gp.formFields = append(gp.formFields, formFieldRef{
			field:  ff.field,
			objIdx: objIdx,
		})
	}
}

// extractStringFromDict extracts a string value for a key from a dict,
// handling both literal strings (...) and hex strings <...>.
func extractStringFromDict(dict, key string) string {
	// Search for key followed by a delimiter to avoid matching /T as a
	// prefix of /Type, /TR, etc.
	searchKey := key + " "
	idx := strings.Index(dict, searchKey)
	if idx < 0 {
		searchKey = key + "\n"
		idx = strings.Index(dict, searchKey)
		if idx < 0 {
			return ""
		}
	}
	rest := strings.TrimLeft(dict[idx+len(key):], " \t\r\n")
	if strings.HasPrefix(rest, "(") {
		end := strings.Index(rest, ")")
		if end < 0 {
			return ""
		}
		return rest[1:end]
	}
	if strings.HasPrefix(rest, "<") {
		end := strings.Index(rest, ">")
		if end < 0 {
			return ""
		}
		hex := rest[1:end]
		data := hexBytes([]byte(hex))
		// decode as UTF-16BE if BOM present, else Latin-1
		if len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF {
			return decodeUTF16BE(data[2:])
		}
		return string(data)
	}
	return ""
}
