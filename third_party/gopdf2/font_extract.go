package gopdf

import (
	"fmt"
)

// ExtractedFont holds information about a font found in a PDF.
type ExtractedFont struct {
	// Name is the font resource name (e.g., "F1").
	Name string
	// BaseFont is the PostScript font name (e.g., "Helvetica", "TimesNewRoman").
	BaseFont string
	// Subtype is the font type (e.g., "Type1", "TrueType", "Type0").
	Subtype string
	// Encoding is the font encoding (e.g., "WinAnsiEncoding", "Identity-H").
	Encoding string
	// ObjNum is the PDF object number of the font.
	ObjNum int
	// IsEmbedded indicates whether the font data is embedded in the PDF.
	IsEmbedded bool
	// Data is the raw font data (if embedded and extractable). May be nil.
	Data []byte
}

// ExtractFontsFromPage extracts font information from a specific page (0-based).
//
// Example:
//
//	data, _ := os.ReadFile("input.pdf")
//	fonts, _ := gopdf.ExtractFontsFromPage(data, 0)
//	for _, f := range fonts {
//	    fmt.Printf("%s: %s (%s) embedded=%v\n", f.Name, f.BaseFont, f.Subtype, f.IsEmbedded)
//	}
func ExtractFontsFromPage(pdfData []byte, pageIndex int) ([]ExtractedFont, error) {
	parser, err := newRawPDFParser(pdfData)
	if err != nil {
		return nil, err
	}
	if pageIndex < 0 || pageIndex >= len(parser.pages) {
		return nil, fmt.Errorf("page index %d out of range (0..%d)", pageIndex, len(parser.pages)-1)
	}
	page := parser.pages[pageIndex]
	return extractFontsFromResources(parser, page.resources), nil
}

// ExtractFontsFromAllPages extracts font information from all pages.
func ExtractFontsFromAllPages(pdfData []byte) (map[int][]ExtractedFont, error) {
	parser, err := newRawPDFParser(pdfData)
	if err != nil {
		return nil, err
	}
	result := make(map[int][]ExtractedFont, len(parser.pages))
	for i, page := range parser.pages {
		fonts := extractFontsFromResources(parser, page.resources)
		if len(fonts) > 0 {
			result[i] = fonts
		}
	}
	return result, nil
}

func extractFontsFromResources(parser *rawPDFParser, res rawPDFResources) []ExtractedFont {
	var fonts []ExtractedFont
	for name, objNum := range res.fonts {
		obj, ok := parser.objects[objNum]
		if !ok {
			continue
		}
		f := ExtractedFont{
			Name:   name,
			ObjNum: objNum,
		}
		f.BaseFont = extractName(obj.dict, "/BaseFont")
		f.Subtype = extractName(obj.dict, "/Subtype")
		f.Encoding = extractName(obj.dict, "/Encoding")

		// Check for embedded font data.
		descRef := extractRef(obj.dict, "/FontDescriptor")
		if descRef > 0 {
			if descObj, ok := parser.objects[descRef]; ok {
				// Look for /FontFile, /FontFile2, or /FontFile3.
				for _, key := range []string{"/FontFile", "/FontFile2", "/FontFile3"} {
					ffRef := extractRef(descObj.dict, key)
					if ffRef > 0 {
						f.IsEmbedded = true
						if ffObj, ok := parser.objects[ffRef]; ok && ffObj.stream != nil {
							f.Data = ffObj.stream
						}
						break
					}
				}
			}
		}

		fonts = append(fonts, f)
	}
	return fonts
}
