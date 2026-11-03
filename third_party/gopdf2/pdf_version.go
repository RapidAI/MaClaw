package gopdf

// PDFVersion represents the PDF specification version.
type PDFVersion int

const (
	// PDFVersion14 is PDF 1.4 (Acrobat 5). Supports transparency.
	PDFVersion14 PDFVersion = 14
	// PDFVersion15 is PDF 1.5 (Acrobat 6). Supports object/xref streams.
	PDFVersion15 PDFVersion = 15
	// PDFVersion16 is PDF 1.6 (Acrobat 7). Supports OpenType fonts.
	PDFVersion16 PDFVersion = 16
	// PDFVersion17 is PDF 1.7 (Acrobat 8, ISO 32000-1). Default.
	PDFVersion17 PDFVersion = 17
	// PDFVersion20 is PDF 2.0 (ISO 32000-2).
	PDFVersion20 PDFVersion = 20
)

// String returns the PDF version header string (e.g. "1.7").
func (v PDFVersion) String() string {
	switch v {
	case PDFVersion14:
		return "1.4"
	case PDFVersion15:
		return "1.5"
	case PDFVersion16:
		return "1.6"
	case PDFVersion17:
		return "1.7"
	case PDFVersion20:
		return "2.0"
	default:
		return "1.7"
	}
}

// Header returns the full PDF header line (e.g. "%PDF-1.7").
func (v PDFVersion) Header() string {
	return "%PDF-" + v.String()
}

// SetPDFVersion sets the PDF version for the output document.
// Default is PDF 1.7. This affects the header and may enable
// version-specific features.
//
// Example:
//
//	pdf.SetPDFVersion(gopdf.PDFVersion20) // output PDF 2.0
func (gp *GoPdf) SetPDFVersion(v PDFVersion) {
	gp.pdfVersion = v
}

// GetPDFVersion returns the current PDF version setting.
func (gp *GoPdf) GetPDFVersion() PDFVersion {
	if gp.pdfVersion == 0 {
		return PDFVersion17
	}
	return gp.pdfVersion
}
