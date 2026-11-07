package gopdf

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// IncrementalSave writes the document as an incremental update appended to
// the original PDF data (PDF §7.5.4): the appended cross-reference section
// carries /Prev back to the previous xref, so the original byte range is
// preserved verbatim (e.g. earlier signatures stay valid) while the new
// trailer points at gopdf's rebuilt object tree.
//
// gopdf rebuilds the full object tree for documents opened via OpenPDF
// (pages, fonts and content are re-emitted as gopdf objects), so the update
// writes every object in gp.pdfObjs and points /Root at gopdf's catalog.
//
// originalData is the original PDF bytes (e.g. from OpenPDFFromBytes).
// modifiedIndices is accepted for API compatibility and currently unused
// (all objects are written). The original file must end with a classic
// xref/startxref for /Prev chaining.
//
// Example:
//
//	original, _ := os.ReadFile("input.pdf")
//	pdf := gopdf.GoPdf{}
//	pdf.OpenPDFFromBytes(original, nil)
//	pdf.SetPage(1)
//	pdf.SetXY(100, 100)
//	pdf.Text("Added text")
//	result, _ := pdf.IncrementalSave(original, nil)
//	os.WriteFile("output.pdf", result, 0644)
func (gp *GoPdf) IncrementalSave(originalData []byte, modifiedIndices []int) ([]byte, error) {
	gp.prepare()
	if err := gp.Close(); err != nil {
		return nil, err
	}

	prevStartxref := findLastStartxref(originalData)
	size := len(gp.pdfObjs) + 1

	var buf bytes.Buffer
	buf.Write(originalData)
	if len(originalData) > 0 && originalData[len(originalData)-1] != '\n' {
		buf.WriteByte('\n')
	}

	// Write every object; record its new offset.
	newOffsets := make(map[int]int64)
	for idx, obj := range gp.pdfObjs {
		if obj == nil {
			continue
		}
		if _, isNull := obj.(nullObj); isNull {
			continue
		}
		objID := idx + 1
		newOffsets[objID] = int64(buf.Len())
		fmt.Fprintf(&buf, "%d 0 obj\n", objID)
		if err := obj.write(&buf, objID); err != nil {
			return nil, err
		}
		io.WriteString(&buf, "endobj\n\n")
	}

	// Cross-reference section covering every gopdf object number.
	xrefOffset := int64(buf.Len())
	io.WriteString(&buf, "xref\n")
	fmt.Fprintf(&buf, "0 %d\n", size)
	for num := 0; num < size; num++ {
		if num == 0 {
			io.WriteString(&buf, "0000000000 65535 f \n")
			continue
		}
		if off, ok := newOffsets[num]; ok {
			fmt.Fprintf(&buf, "%010d 00000 n \n", off)
		} else {
			io.WriteString(&buf, "0000000000 65535 f \n")
		}
	}

	io.WriteString(&buf, "trailer\n")
	fmt.Fprintf(&buf, "<<\n")
	fmt.Fprintf(&buf, "/Size %d\n", size+1)
	if gp.indexOfCatalogObj >= 0 && gp.indexOfCatalogObj < len(gp.pdfObjs) {
		fmt.Fprintf(&buf, "/Root %d 0 R\n", gp.indexOfCatalogObj+1)
	}
	if prevStartxref > 0 {
		fmt.Fprintf(&buf, "/Prev %d\n", prevStartxref)
	}
	if gp.encryptionObjID > 0 {
		fmt.Fprintf(&buf, "/Encrypt %d 0 R\n", gp.encryptionObjID)
	}
	io.WriteString(&buf, ">>\n")
	fmt.Fprintf(&buf, "startxref\n")
	fmt.Fprintf(&buf, "%d\n", xrefOffset)
	io.WriteString(&buf, "%%EOF\n")

	return buf.Bytes(), nil
}

// findLastStartxref returns the byte offset of the last startxref entry in
// the data (0 when absent).
func findLastStartxref(data []byte) int64 {
	sx := bytes.LastIndex(data, []byte("startxref"))
	if sx < 0 {
		return 0
	}
	rest := data[sx+len("startxref"):]
	fields := strings.Fields(string(rest[:min(len(rest), 32)]))
	if len(fields) == 0 {
		return 0
	}
	v, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// WriteIncrementalPdf writes the document as an incremental update to a file.
// originalData is the original PDF bytes. If modifiedIndices is nil, all
// objects are written.
func (gp *GoPdf) WriteIncrementalPdf(pdfPath string, originalData []byte, modifiedIndices []int) error {
	data, err := gp.IncrementalSave(originalData, modifiedIndices)
	if err != nil {
		return err
	}
	return os.WriteFile(pdfPath, data, 0644)
}
