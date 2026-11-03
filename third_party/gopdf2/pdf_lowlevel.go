package gopdf

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// ============================================================
// Low-level PDF Operations — direct read/write of PDF objects,
// dictionary keys, streams, catalog, and trailer.
// These are standalone functions operating on raw PDF bytes,
// similar to PyMuPDF's xref_object / update_object / etc.
// ============================================================

// PDFObject represents a parsed low-level PDF object.
type PDFObject struct {
	// Num is the object number.
	Num int
	// Generation is the generation number (usually 0).
	Generation int
	// Dict is the dictionary content (between << >>).
	Dict string
	// Stream is the raw stream data (nil if not a stream object).
	Stream []byte
	// Raw is the full raw object content between "N 0 obj" and "endobj".
	Raw string
}

// ReadObject reads a PDF object definition by object number.
// Returns the parsed object with its dictionary and optional stream.
//
// Example:
//
//	data, _ := os.ReadFile("input.pdf")
//	obj, err := gopdf.ReadObject(data, 5)
//	fmt.Println(obj.Dict)
func ReadObject(pdfData []byte, objNum int) (*PDFObject, error) {
	start, end, err := findObjectBounds(pdfData, objNum)
	if err != nil {
		return nil, err
	}

	raw := string(pdfData[start:end])
	obj := &PDFObject{
		Num:        objNum,
		Generation: 0,
		Raw:        raw,
	}

	// Extract dictionary.
	obj.Dict = extractDict([]byte(raw))

	// Extract stream if present.
	streamStart := strings.Index(raw, "stream\n")
	if streamStart < 0 {
		streamStart = strings.Index(raw, "stream\r\n")
	}
	if streamStart >= 0 {
		streamEnd := strings.Index(raw, "\nendstream")
		if streamEnd < 0 {
			streamEnd = strings.Index(raw, "\r\nendstream")
		}
		if streamEnd > streamStart {
			dataStart := streamStart + len("stream\n")
			if raw[streamStart+len("stream")] == '\r' {
				dataStart = streamStart + len("stream\r\n")
			}
			obj.Stream = []byte(raw[dataStart:streamEnd])
		}
	}

	return obj, nil
}

// UpdateObject replaces the content of a PDF object and returns the modified PDF.
// newContent should be the full object body (dictionary + optional stream).
//
// Example:
//
//	updated, err := gopdf.UpdateObject(data, 5, "<< /Type /Page /MediaBox [0 0 612 792] >>")
func UpdateObject(pdfData []byte, objNum int, newContent string) ([]byte, error) {
	start, end, err := findObjectBounds(pdfData, objNum)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	buf.Write(pdfData[:start])
	fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj", objNum, newContent)
	buf.Write(pdfData[end:])

	return rebuildXref(buf.Bytes()), nil
}

// GetDictKey reads a dictionary key value from a PDF object.
// Returns the raw value string, or empty string if not found.
//
// Example:
//
//	val, err := gopdf.GetDictKey(data, 5, "/Type")
//	// val might be "/Page"
func GetDictKey(pdfData []byte, objNum int, key string) (string, error) {
	obj, err := ReadObject(pdfData, objNum)
	if err != nil {
		return "", err
	}
	return extractDictKeyValue(obj.Dict, key), nil
}

// SetDictKey sets or updates a dictionary key in a PDF object.
// Returns the modified PDF data.
//
// Example:
//
//	updated, err := gopdf.SetDictKey(data, 5, "/MediaBox", "[0 0 595 842]")
func SetDictKey(pdfData []byte, objNum int, key, value string) ([]byte, error) {
	obj, err := ReadObject(pdfData, objNum)
	if err != nil {
		return nil, err
	}

	newDict := setDictKeyValue(obj.Dict, key, value)

	var newContent string
	if obj.Stream != nil {
		newContent = fmt.Sprintf("<<%s>>\nstream\n%s\nendstream", newDict, string(obj.Stream))
	} else {
		newContent = fmt.Sprintf("<<%s>>", newDict)
	}

	return UpdateObject(pdfData, objNum, newContent)
}

// GetStream reads the stream data from a PDF object.
// Returns nil if the object has no stream.
//
// Example:
//
//	stream, err := gopdf.GetStream(data, 10)
func GetStream(pdfData []byte, objNum int) ([]byte, error) {
	obj, err := ReadObject(pdfData, objNum)
	if err != nil {
		return nil, err
	}
	return obj.Stream, nil
}

// SetStream replaces the stream data in a PDF object.
// The /Length key in the dictionary is automatically updated.
//
// Example:
//
//	updated, err := gopdf.SetStream(data, 10, []byte("BT /F1 12 Tf 100 700 Td (Hello) Tj ET"))
func SetStream(pdfData []byte, objNum int, streamData []byte) ([]byte, error) {
	obj, err := ReadObject(pdfData, objNum)
	if err != nil {
		return nil, err
	}

	newDict := setDictKeyValue(obj.Dict, "/Length", strconv.Itoa(len(streamData)))
	newContent := fmt.Sprintf("<<%s>>\nstream\n%s\nendstream", newDict, string(streamData))

	return UpdateObject(pdfData, objNum, newContent)
}

// CopyObject duplicates a PDF object and returns the modified PDF data
// along with the new object number.
//
// Example:
//
//	newData, newObjNum, err := gopdf.CopyObject(data, 5)
func CopyObject(pdfData []byte, objNum int) ([]byte, int, error) {
	obj, err := ReadObject(pdfData, objNum)
	if err != nil {
		return nil, 0, err
	}

	// Find the highest object number.
	matches := reObjHeader.FindAllSubmatch(pdfData, -1)
	maxObj := 0
	for _, m := range matches {
		n, _ := strconv.Atoi(string(m[1]))
		if n > maxObj {
			maxObj = n
		}
	}
	newObjNum := maxObj + 1

	// Insert the new object before the xref table.
	xrefIdx := bytes.LastIndex(pdfData, []byte("xref\n"))
	if xrefIdx < 0 {
		xrefIdx = bytes.LastIndex(pdfData, []byte("xref\r\n"))
	}
	if xrefIdx < 0 {
		return nil, 0, fmt.Errorf("cannot find xref table")
	}

	var buf bytes.Buffer
	buf.Write(pdfData[:xrefIdx])
	fmt.Fprintf(&buf, "%d 0 obj\n", newObjNum)
	// Write the raw content (dict + stream).
	if obj.Stream != nil {
		fmt.Fprintf(&buf, "<<%s>>\nstream\n%s\nendstream\n", obj.Dict, string(obj.Stream))
	} else if obj.Dict != "" {
		fmt.Fprintf(&buf, "<<%s>>\n", obj.Dict)
	} else {
		buf.WriteString(obj.Raw)
		buf.WriteByte('\n')
	}
	buf.WriteString("endobj\n\n")
	buf.Write(pdfData[xrefIdx:])

	return rebuildXref(buf.Bytes()), newObjNum, nil
}

// GetCatalog returns the PDF Catalog dictionary object.
//
// Example:
//
//	catalog, err := gopdf.GetCatalog(data)
//	fmt.Println(catalog.Dict)
func GetCatalog(pdfData []byte) (*PDFObject, error) {
	parser, err := newRawPDFParser(pdfData)
	if err != nil {
		return nil, err
	}
	if parser.root <= 0 {
		return nil, fmt.Errorf("no catalog found")
	}
	return ReadObject(pdfData, parser.root)
}

// GetTrailer returns the PDF trailer dictionary as a string.
//
// Example:
//
//	trailer, err := gopdf.GetTrailer(data)
//	fmt.Println(trailer)
func GetTrailer(pdfData []byte) (string, error) {
	trailerIdx := bytes.LastIndex(pdfData, []byte("trailer"))
	if trailerIdx < 0 {
		return "", fmt.Errorf("no trailer found")
	}

	startxrefIdx := bytes.Index(pdfData[trailerIdx:], []byte("startxref"))
	if startxrefIdx < 0 {
		return "", fmt.Errorf("no startxref found")
	}

	trailerContent := string(pdfData[trailerIdx : trailerIdx+startxrefIdx])
	return strings.TrimSpace(trailerContent), nil
}

// --- internal helpers ---

// findObjectBounds locates the byte range of "N 0 obj ... endobj" in pdfData.
func findObjectBounds(pdfData []byte, objNum int) (int, int, error) {
	header := fmt.Sprintf("%d 0 obj", objNum)
	idx := bytes.Index(pdfData, []byte(header))
	if idx < 0 {
		return 0, 0, fmt.Errorf("object %d not found", objNum)
	}

	endMarker := []byte("endobj")
	endIdx := bytes.Index(pdfData[idx:], endMarker)
	if endIdx < 0 {
		return 0, 0, fmt.Errorf("endobj not found for object %d", objNum)
	}
	endIdx = idx + endIdx + len(endMarker)

	return idx, endIdx, nil
}

// extractDictKeyValue extracts the value for a given key from a PDF dictionary string.
func extractDictKeyValue(dict, key string) string {
	idx := strings.Index(dict, key)
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(dict[idx+len(key):])
	if len(rest) == 0 {
		return ""
	}

	// Handle different value types.
	switch rest[0] {
	case '/': // name
		end := strings.IndexAny(rest[1:], " /\n\r\t>")
		if end < 0 {
			return rest
		}
		return rest[:end+1]
	case '(': // string
		depth := 0
		for i, c := range rest {
			if c == '(' {
				depth++
			} else if c == ')' {
				depth--
				if depth == 0 {
					return rest[:i+1]
				}
			}
		}
		return rest
	case '[': // array
		depth := 0
		for i, c := range rest {
			if c == '[' {
				depth++
			} else if c == ']' {
				depth--
				if depth == 0 {
					return rest[:i+1]
				}
			}
		}
		return rest
	case '<': // dict or hex string
		if len(rest) > 1 && rest[1] == '<' {
			depth := 0
			for i := 0; i < len(rest)-1; i++ {
				if rest[i] == '<' && rest[i+1] == '<' {
					depth++
					i++
				} else if rest[i] == '>' && rest[i+1] == '>' {
					depth--
					i++
					if depth == 0 {
						return rest[:i+1]
					}
				}
			}
		}
		end := strings.Index(rest, ">")
		if end >= 0 {
			return rest[:end+1]
		}
		return rest
	default: // number, bool, reference, etc.
		end := strings.IndexAny(rest, " /\n\r\t>")
		if end < 0 {
			return rest
		}
		// Check for "N 0 R" reference pattern.
		if end < len(rest) {
			afterFirst := strings.TrimSpace(rest[end:])
			if strings.HasPrefix(afterFirst, "0 R") {
				return rest[:end] + " 0 R"
			}
		}
		return rest[:end]
	}
}

// setDictKeyValue sets or updates a key in a PDF dictionary string.
func setDictKeyValue(dict, key, value string) string {
	// Try to replace existing key.
	idx := strings.Index(dict, key)
	if idx >= 0 {
		// Find the end of the current value.
		rest := dict[idx+len(key):]
		valStart := 0
		for valStart < len(rest) && (rest[valStart] == ' ' || rest[valStart] == '\t') {
			valStart++
		}
		oldVal := extractDictKeyValue(dict, key)
		if oldVal != "" {
			oldEntry := key + " " + oldVal
			if strings.Contains(dict, key+" "+oldVal) {
				return strings.Replace(dict, oldEntry, key+" "+value, 1)
			}
		}
	}

	// Key not found — add before closing >>.
	trimmed := strings.TrimRight(dict, " \t\n\r")
	return trimmed + "\n" + key + " " + value + "\n"
}

// extractFilterValue is defined in image_extract.go

// extractName is defined in text_extract.go
