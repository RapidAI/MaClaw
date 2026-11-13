package gopdf

import (
	"errors"
	"strconv"
	"time"
)

// ============================================================
// Document metadata — read the /Info dictionary of existing PDFs.
// PyMuPDF parity: doc.metadata. The writer side is SetInfo(PdfInfo).
// ============================================================

// ErrNoMetadata is returned when the PDF carries no /Info dictionary.
var ErrNoMetadata = errors.New("PDF has no /Info metadata dictionary")

// GetPDFInfo reads document metadata (title, author, subject, keywords,
// creator, producer, dates) from raw PDF bytes.
//
// Example:
//
//	info, err := gopdf.GetPDFInfo(data)
//	fmt.Println(info.Title, info.Author)
func GetPDFInfo(pdfData []byte) (PdfInfo, error) {
	parser, err := newRawPDFParser(pdfData)
	if err != nil {
		return PdfInfo{}, err
	}
	return parser.readInfoDict(), nil
}

// GetPDFInfoFromFile reads document metadata from a PDF file.
func GetPDFInfoFromFile(path string) (PdfInfo, error) {
	data, err := readFileBytes(path)
	if err != nil {
		return PdfInfo{}, err
	}
	return GetPDFInfo(data)
}

// readInfoDict decodes the /Info dictionary fields. The dict is either an
// indirect object or an inline trailer dict.
func (p *rawPDFParser) readInfoDict() PdfInfo {
	var info PdfInfo
	var d string
	switch {
	case p.info > 0:
		obj, ok := p.objects[p.info]
		if !ok {
			return info
		}
		d = obj.dict
	case p.infoDict != "":
		d = p.infoDict
	default:
		return info
	}
	info.Title = decodeInfoText(extractStringFromDict(d, "/Title"))
	info.Author = decodeInfoText(extractStringFromDict(d, "/Author"))
	info.Subject = decodeInfoText(extractStringFromDict(d, "/Subject"))
	info.Keywords = decodeInfoText(extractStringFromDict(d, "/Keywords"))
	info.Creator = decodeInfoText(extractStringFromDict(d, "/Creator"))
	info.Producer = decodeInfoText(extractStringFromDict(d, "/Producer"))
	info.CreationDate = parsePDFDate(extractStringFromDict(d, "/CreationDate"))
	info.ModDate = parsePDFDate(extractStringFromDict(d, "/ModDate"))
	return info
}

// decodeInfoText trims the decoded string (extractStringFromDict already
// handles hex strings and UTF-16BE).
func decodeInfoText(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\x00' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}

// parsePDFDate parses a PDF date string "D:YYYYMMDDHHmmSSOHH'mm'".
// Missing trailing components default to zero; unparsable input returns
// the zero time.
func parsePDFDate(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	s = stringsTrimPrefix(s, "D:")
	if len(s) < 4 {
		return time.Time{}
	}
	year, _ := strconv.Atoi(clampDigits(s, 4))
	rest := s[4:]
	mon, rest := nextDigits(rest, 2, 1)
	day, rest := nextDigits(rest, 2, 1)
	hour, rest := nextDigits(rest, 2, 0)
	minute, rest := nextDigits(rest, 2, 0)
	sec, _ := nextDigits(rest, 2, 0)
	if mon < 1 || mon > 12 || day < 1 || day > 31 {
		return time.Time{}
	}
	return time.Date(year, time.Month(mon), day, hour, minute, sec, 0, time.UTC)
}

func clampDigits(s string, n int) string {
	if len(s) < n {
		return s + stringsRepeat("0", n-len(s))
	}
	return s[:n]
}

func nextDigits(s string, n int, def int) (int, string) {
	if len(s) == 0 || s[0] < '0' || s[0] > '9' {
		return def, s
	}
	v, _ := strconv.Atoi(clampDigits(s, n))
	if len(s) >= n {
		return v, s[n:]
	}
	return v, ""
}

func stringsTrimPrefix(s, prefix string) string {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):]
	}
	return s
}

func stringsRepeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}
