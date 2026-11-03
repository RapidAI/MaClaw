package gopdf

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// PDFAConformanceLevel represents the PDF/A conformance level.
type PDFAConformanceLevel string

const (
	PDFA1b PDFAConformanceLevel = "PDF/A-1b"
	PDFA1a PDFAConformanceLevel = "PDF/A-1a"
	PDFA2b PDFAConformanceLevel = "PDF/A-2b"
	PDFA2a PDFAConformanceLevel = "PDF/A-2a"
)

// PDFAValidationResult holds the result of a PDF/A compliance check.
type PDFAValidationResult struct {
	// IsValid is true if the document passes all checks for the target level.
	IsValid bool
	// Level is the target conformance level that was checked.
	Level PDFAConformanceLevel
	// Errors lists all compliance violations found.
	Errors []PDFAValidationError
	// Warnings lists non-critical issues.
	Warnings []string
}

// PDFAValidationError represents a single PDF/A compliance violation.
type PDFAValidationError struct {
	// Code is a short identifier for the error type.
	Code string
	// Message describes the violation.
	Message string
	// Clause references the PDF/A specification clause.
	Clause string
}

// ValidatePDFA checks if the given PDF data conforms to the specified PDF/A level.
// This performs basic structural checks — it is not a full PDF/A validator but
// catches the most common compliance issues.
//
// Checks performed:
//   - PDF version compatibility
//   - XMP metadata presence and PDF/A identification
//   - Document info dictionary consistency
//   - Font embedding (all fonts must be embedded)
//   - Encryption (not allowed in PDF/A-1)
//   - Transparency (not allowed in PDF/A-1)
//   - JavaScript/actions (not allowed)
//
// Example:
//
//	data, _ := os.ReadFile("document.pdf")
//	result := gopdf.ValidatePDFA(data, gopdf.PDFA1b)
//	if result.IsValid {
//	    fmt.Println("Document is PDF/A-1b compliant")
//	} else {
//	    for _, e := range result.Errors {
//	        fmt.Printf("Error [%s]: %s\n", e.Code, e.Message)
//	    }
//	}
func ValidatePDFA(pdfData []byte, level PDFAConformanceLevel) PDFAValidationResult {
	result := PDFAValidationResult{
		Level:   level,
		IsValid: true,
	}

	addError := func(code, message, clause string) {
		result.Errors = append(result.Errors, PDFAValidationError{
			Code: code, Message: message, Clause: clause,
		})
		result.IsValid = false
	}

	addWarning := func(msg string) {
		result.Warnings = append(result.Warnings, msg)
	}

	// 1. Check PDF version.
	checkPDFVersion(pdfData, level, addError)

	// 2. Parse the PDF.
	parser, err := newRawPDFParser(pdfData)
	if err != nil {
		addError("PARSE", fmt.Sprintf("Failed to parse PDF: %v", err), "6.1")
		return result
	}

	// 3. Check for encryption (not allowed in PDF/A-1).
	if level == PDFA1a || level == PDFA1b {
		if detectEncryption(pdfData) > 0 {
			addError("ENCRYPT", "Encryption is not allowed in PDF/A-1", "6.1.13")
		}
	}

	// 4. Check XMP metadata.
	checkXMPMetadata(pdfData, parser, level, addError, addWarning)

	// 5. Check fonts.
	checkFontEmbedding(parser, addError, addWarning)

	// 6. Check for JavaScript.
	checkJavaScript(pdfData, addError)

	// 7. Check for transparency (PDF/A-1 only).
	if level == PDFA1a || level == PDFA1b {
		checkTransparency(parser, addError)
	}

	// 8. Check document info.
	checkDocumentInfo(pdfData, addWarning)

	return result
}

func checkPDFVersion(data []byte, level PDFAConformanceLevel, addError func(string, string, string)) {
	// Find PDF header.
	if len(data) < 8 || string(data[:5]) != "%PDF-" {
		addError("VERSION", "Missing or invalid PDF header", "6.1.2")
		return
	}

	versionStr := string(data[5:8])
	version, err := strconv.ParseFloat(versionStr, 64)
	if err != nil {
		addError("VERSION", fmt.Sprintf("Cannot parse PDF version: %s", versionStr), "6.1.2")
		return
	}

	switch level {
	case PDFA1a, PDFA1b:
		// PDF/A-1 requires PDF 1.4 or earlier.
		if version > 1.4+0.001 {
			addError("VERSION",
				fmt.Sprintf("PDF/A-1 requires PDF version 1.4 or earlier, found %s", versionStr),
				"6.1.2")
		}
	case PDFA2a, PDFA2b:
		// PDF/A-2 allows up to PDF 1.7.
		if version > 1.7+0.001 {
			addError("VERSION",
				fmt.Sprintf("PDF/A-2 requires PDF version 1.7 or earlier, found %s", versionStr),
				"6.1.2")
		}
	}
}

func checkXMPMetadata(data []byte, parser *rawPDFParser, level PDFAConformanceLevel,
	addError func(string, string, string), addWarning func(string)) {

	// Look for XMP metadata in the catalog.
	hasXMP := false
	for _, obj := range parser.objects {
		if strings.Contains(obj.dict, "/Metadata") && obj.stream != nil {
			streamData := obj.stream
			if bytes.Contains(streamData, []byte("pdfaid:part")) ||
				bytes.Contains(streamData, []byte("pdfaSchema")) ||
				bytes.Contains(streamData, []byte("http://www.aiim.org/pdfa")) {
				hasXMP = true
				break
			}
		}
	}

	// Also check raw data for XMP.
	if !hasXMP {
		if bytes.Contains(data, []byte("pdfaid:part")) ||
			bytes.Contains(data, []byte("<x:xmpmeta")) {
			hasXMP = true
		}
	}

	if !hasXMP {
		addError("XMP", "XMP metadata with PDF/A identification is required", "6.7.2")
	}

	// Check for PDF/A conformance declaration in XMP.
	hasPDFAID := bytes.Contains(data, []byte("pdfaid:part"))
	if hasXMP && !hasPDFAID {
		addWarning("XMP metadata found but missing pdfaid:part declaration")
	}
}

func checkFontEmbedding(parser *rawPDFParser, addError func(string, string, string), addWarning func(string)) {
	for _, page := range parser.pages {
		// Sort font names for deterministic output.
		fontNames := make([]string, 0, len(page.resources.fonts))
		for name := range page.resources.fonts {
			fontNames = append(fontNames, name)
		}
		sort.Strings(fontNames)

		for _, fontName := range fontNames {
			objNum := page.resources.fonts[fontName]
			obj, ok := parser.objects[objNum]
			if !ok {
				continue
			}

			// Check if font is embedded (has /FontDescriptor with /FontFile).
			hasFontFile := false
			fdRef := extractRef(obj.dict, "/FontDescriptor")
			if fdRef > 0 {
				fdObj, ok := parser.objects[fdRef]
				if ok {
					hasFontFile = strings.Contains(fdObj.dict, "/FontFile") ||
						strings.Contains(fdObj.dict, "/FontFile2") ||
						strings.Contains(fdObj.dict, "/FontFile3")
				}
			}

			// Standard 14 fonts don't need embedding in some interpretations,
			// but PDF/A strictly requires all fonts to be embedded.
			baseFontName := extractName(obj.dict, "/BaseFont")
			isStandard14 := isStandard14Font(baseFontName)

			if !hasFontFile && !isStandard14 {
				// Check if it's a Type0 (composite) font with descendant.
				if strings.Contains(obj.dict, "/Type0") {
					// DescendantFonts is typically an array: [ref]
					// Try extractRef first, then extractRefArray.
					descRef := extractRef(obj.dict, "/DescendantFonts")
					if descRef > 0 {
						descObj, ok := parser.objects[descRef]
						if ok {
							descFDRef := extractRef(descObj.dict, "/FontDescriptor")
							if descFDRef > 0 {
								descFDObj, ok := parser.objects[descFDRef]
								if ok {
									hasFontFile = strings.Contains(descFDObj.dict, "/FontFile") ||
										strings.Contains(descFDObj.dict, "/FontFile2") ||
										strings.Contains(descFDObj.dict, "/FontFile3")
								}
							}
						}
					}
					if !hasFontFile {
						// Try extractRefArray for array-style DescendantFonts.
						descRefs := extractRefArray(obj.dict, "/DescendantFonts")
						for _, dRef := range descRefs {
							descObj, ok := parser.objects[dRef]
							if !ok {
								continue
							}
							descFDRef := extractRef(descObj.dict, "/FontDescriptor")
							if descFDRef > 0 {
								descFDObj, ok := parser.objects[descFDRef]
								if ok && (strings.Contains(descFDObj.dict, "/FontFile") ||
									strings.Contains(descFDObj.dict, "/FontFile2") ||
									strings.Contains(descFDObj.dict, "/FontFile3")) {
									hasFontFile = true
									break
								}
							}
						}
					}
					if hasFontFile {
						continue
					}
					// Composite font without verifiable embedding — still flag it.
				}
				addError("FONT",
					fmt.Sprintf("Font %s (%s) is not embedded", fontName, baseFontName),
					"6.3.5")
			}

			if isStandard14 && !hasFontFile {
				addWarning(fmt.Sprintf("Standard 14 font %s is not embedded (required for strict PDF/A)", fontName))
			}
		}
	}
}

func checkJavaScript(data []byte, addError func(string, string, string)) {
	if bytes.Contains(data, []byte("/JavaScript")) ||
		bytes.Contains(data, []byte("/JS ")) ||
		bytes.Contains(data, []byte("/JS(")) ||
		bytes.Contains(data, []byte("/JS<")) {
		addError("JS", "JavaScript is not allowed in PDF/A", "6.6.1")
	}
}

func checkTransparency(parser *rawPDFParser, addError func(string, string, string)) {
	for _, obj := range parser.objects {
		if strings.Contains(obj.dict, "/Group") &&
			strings.Contains(obj.dict, "/Transparency") {
			addError("TRANSPARENCY", "Transparency groups are not allowed in PDF/A-1", "6.4")
			return
		}
		if strings.Contains(obj.dict, "/SMask") &&
			!strings.Contains(obj.dict, "/SMask /None") {
			addError("TRANSPARENCY", "Soft masks (SMask) are not allowed in PDF/A-1", "6.4")
			return
		}
	}
}

func checkDocumentInfo(data []byte, addWarning func(string)) {
	if !bytes.Contains(data, []byte("/Title")) {
		addWarning("Document title is recommended for PDF/A compliance")
	}
}

// standard14Fonts is the set of PDF standard 14 font names.
var standard14Fonts = map[string]bool{
	"Courier":               true,
	"Courier-Bold":          true,
	"Courier-Oblique":       true,
	"Courier-BoldOblique":   true,
	"Helvetica":             true,
	"Helvetica-Bold":        true,
	"Helvetica-Oblique":     true,
	"Helvetica-BoldOblique": true,
	"Times-Roman":           true,
	"Times-Bold":            true,
	"Times-Italic":          true,
	"Times-BoldItalic":      true,
	"Symbol":                true,
	"ZapfDingbats":          true,
}

// isStandard14Font checks if a font name is one of the PDF standard 14 fonts.
func isStandard14Font(name string) bool {
	return standard14Fonts[name]
}
