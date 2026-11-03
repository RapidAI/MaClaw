package gopdf

import (
	"compress/flate"
	"errors"
	"fmt"
	"io"
	"time"
)

// EmbeddedFile represents a file to be embedded in the PDF.
type EmbeddedFile struct {
	Name        string    // Display name of the file
	Content     []byte    // File content
	MimeType    string    // MIME type (e.g. "application/pdf", "text/plain")
	Description string    // Optional description
	ModDate     time.Time // Modification date (default: now)
}

// EmbeddedFileInfo contains metadata about an embedded file.
type EmbeddedFileInfo struct {
	Name        string
	MimeType    string
	Description string
	Size        int       // uncompressed size in bytes
	ModDate     time.Time // modification date
}

// ErrEmbeddedFileNotFound is returned when the specified embedded file name does not exist.
var ErrEmbeddedFileNotFound = errors.New("embedded file not found")

// embeddedFileStreamObj is the PDF stream object for the embedded file content.
type embeddedFileStreamObj struct {
	data     []byte
	mimeType string
	modDate  time.Time
}

func (e embeddedFileStreamObj) init(f func() *GoPdf) {}

func (e embeddedFileStreamObj) getType() string {
	return "EmbeddedFile"
}

func (e embeddedFileStreamObj) write(w io.Writer, objID int) error {
	// Compress the data.
	var compressed []byte
	{
		var buf []byte
		bw := &byteWriter{buf: &buf}
		zw, err := flate.NewWriter(bw, flate.DefaultCompression)
		if err != nil {
			return err
		}
		if _, err := zw.Write(e.data); err != nil {
			return err
		}
		if err := zw.Close(); err != nil {
			return err
		}
		compressed = *bw.buf
	}

	io.WriteString(w, "<<\n")
	io.WriteString(w, "/Type /EmbeddedFile\n")
	if e.mimeType != "" {
		fmt.Fprintf(w, "/Subtype /%s\n", escapeMimeType(e.mimeType))
	}
	fmt.Fprintf(w, "/Length %d\n", len(compressed))
	io.WriteString(w, "/Filter /FlateDecode\n")
	io.WriteString(w, "/Params <<\n")
	fmt.Fprintf(w, "  /Size %d\n", len(e.data))
	if !e.modDate.IsZero() {
		fmt.Fprintf(w, "  /ModDate (D:%s)\n", e.modDate.Format("20060102150405"))
	}
	io.WriteString(w, ">>\n")
	io.WriteString(w, ">>\n")
	io.WriteString(w, "stream\n")
	w.Write(compressed)
	io.WriteString(w, "\nendstream\n")
	return nil
}

// fileSpecObj is the PDF file specification object.
type fileSpecObj struct {
	name        string
	description string
	streamObjID int // 1-based object ID of the embedded file stream
}

func (f fileSpecObj) init(fn func() *GoPdf) {}

func (f fileSpecObj) getType() string {
	return "Filespec"
}

func (f fileSpecObj) write(w io.Writer, objID int) error {
	io.WriteString(w, "<<\n")
	io.WriteString(w, "/Type /Filespec\n")
	fmt.Fprintf(w, "/F (%s)\n", escapeAnnotString(f.name))
	fmt.Fprintf(w, "/UF (%s)\n", escapeAnnotString(f.name))
	if f.description != "" {
		fmt.Fprintf(w, "/Desc (%s)\n", escapeAnnotString(f.description))
	}
	fmt.Fprintf(w, "/EF << /F %d 0 R >>\n", f.streamObjID)
	io.WriteString(w, ">>\n")
	return nil
}

// byteWriter is a simple io.Writer that appends to a byte slice.
type byteWriter struct {
	buf *[]byte
}

func (bw *byteWriter) Write(p []byte) (int, error) {
	*bw.buf = append(*bw.buf, p...)
	return len(p), nil
}

// escapeMimeType converts a MIME type to a PDF name-safe string.
// e.g. "application/pdf" -> "application#2Fpdf"
func escapeMimeType(mime string) string {
	result := make([]byte, 0, len(mime))
	for i := 0; i < len(mime); i++ {
		if mime[i] == '/' {
			result = append(result, '#', '2', 'F')
		} else {
			result = append(result, mime[i])
		}
	}
	return string(result)
}

// embeddedFileRef tracks an embedded file for the Names dictionary.
type embeddedFileRef struct {
	name          string
	fileSpecObjID int
	streamObjIdx  int // 0-based index of the stream object
}

// AddEmbeddedFile attaches a file to the PDF document.
// The file will appear in the PDF viewer's attachment panel.
//
// Example:
//
//	data, _ := os.ReadFile("report.csv")
//	pdf.AddEmbeddedFile(gopdf.EmbeddedFile{
//	    Name:     "report.csv",
//	    Content:  data,
//	    MimeType: "text/csv",
//	})
func (gp *GoPdf) AddEmbeddedFile(ef EmbeddedFile) error {
	if ef.Name == "" {
		return ErrEmptyString
	}
	if len(ef.Content) == 0 {
		return ErrEmptyString
	}
	if ef.ModDate.IsZero() {
		ef.ModDate = time.Now()
	}

	// Add the embedded file stream object.
	streamIdx := gp.addObj(embeddedFileStreamObj{
		data:     ef.Content,
		mimeType: ef.MimeType,
		modDate:  ef.ModDate,
	})
	streamObjID := streamIdx + 1 // PDF object IDs are 1-based

	// Add the file specification object.
	fileSpecIdx := gp.addObj(fileSpecObj{
		name:        ef.Name,
		description: ef.Description,
		streamObjID: streamObjID,
	})

	// Track embedded files for the Names dictionary.
	if gp.embeddedFiles == nil {
		gp.embeddedFiles = make([]embeddedFileRef, 0)
	}
	gp.embeddedFiles = append(gp.embeddedFiles, embeddedFileRef{
		name:          ef.Name,
		fileSpecObjID: fileSpecIdx + 1,
		streamObjIdx:  streamIdx,
	})

	return nil
}

// GetEmbeddedFile retrieves the content of an embedded file by name.
//
// Example:
//
//	data, err := pdf.GetEmbeddedFile("report.csv")
func (gp *GoPdf) GetEmbeddedFile(name string) ([]byte, error) {
	for _, ref := range gp.embeddedFiles {
		if ref.name == name {
			streamIdx := ref.streamObjIdx
			if streamIdx < 0 || streamIdx >= len(gp.pdfObjs) {
				return nil, ErrEmbeddedFileNotFound
			}
			if s, ok := gp.pdfObjs[streamIdx].(embeddedFileStreamObj); ok {
				out := make([]byte, len(s.data))
				copy(out, s.data)
				return out, nil
			}
			return nil, ErrEmbeddedFileNotFound
		}
	}
	return nil, ErrEmbeddedFileNotFound
}

// DeleteEmbeddedFile removes an embedded file by name.
//
// Example:
//
//	err := pdf.DeleteEmbeddedFile("report.csv")
func (gp *GoPdf) DeleteEmbeddedFile(name string) error {
	for i, ref := range gp.embeddedFiles {
		if ref.name == name {
			gp.embeddedFiles = append(gp.embeddedFiles[:i], gp.embeddedFiles[i+1:]...)
			streamIdx := ref.streamObjIdx
			fileSpecIdx := ref.fileSpecObjID - 1
			if streamIdx >= 0 && streamIdx < len(gp.pdfObjs) {
				gp.pdfObjs[streamIdx] = nullObj{}
			}
			if fileSpecIdx >= 0 && fileSpecIdx < len(gp.pdfObjs) {
				gp.pdfObjs[fileSpecIdx] = nullObj{}
			}
			return nil
		}
	}
	return ErrEmbeddedFileNotFound
}

// UpdateEmbeddedFile replaces the content of an existing embedded file.
//
// Example:
//
//	err := pdf.UpdateEmbeddedFile("report.csv", gopdf.EmbeddedFile{
//	    Name:     "report.csv",
//	    Content:  newData,
//	    MimeType: "text/csv",
//	})
func (gp *GoPdf) UpdateEmbeddedFile(name string, ef EmbeddedFile) error {
	for _, ref := range gp.embeddedFiles {
		if ref.name == name {
			streamIdx := ref.streamObjIdx
			if streamIdx < 0 || streamIdx >= len(gp.pdfObjs) {
				return ErrEmbeddedFileNotFound
			}
			if _, ok := gp.pdfObjs[streamIdx].(embeddedFileStreamObj); !ok {
				return ErrEmbeddedFileNotFound
			}
			modDate := ef.ModDate
			if modDate.IsZero() {
				modDate = time.Now()
			}
			gp.pdfObjs[streamIdx] = embeddedFileStreamObj{
				data:     ef.Content,
				mimeType: ef.MimeType,
				modDate:  modDate,
			}
			fileSpecIdx := ref.fileSpecObjID - 1
			if fileSpecIdx >= 0 && fileSpecIdx < len(gp.pdfObjs) {
				if fs, ok := gp.pdfObjs[fileSpecIdx].(fileSpecObj); ok && ef.Description != "" {
					fs.description = ef.Description
					gp.pdfObjs[fileSpecIdx] = fs
				}
			}
			return nil
		}
	}
	return ErrEmbeddedFileNotFound
}

// GetEmbeddedFileInfo returns metadata about an embedded file.
//
// Example:
//
//	info, err := pdf.GetEmbeddedFileInfo("report.csv")
//	fmt.Println(info.Size, info.MimeType)
func (gp *GoPdf) GetEmbeddedFileInfo(name string) (EmbeddedFileInfo, error) {
	for _, ref := range gp.embeddedFiles {
		if ref.name == name {
			streamIdx := ref.streamObjIdx
			fileSpecIdx := ref.fileSpecObjID - 1
			info := EmbeddedFileInfo{Name: name}
			if streamIdx >= 0 && streamIdx < len(gp.pdfObjs) {
				if s, ok := gp.pdfObjs[streamIdx].(embeddedFileStreamObj); ok {
					info.MimeType = s.mimeType
					info.Size = len(s.data)
					info.ModDate = s.modDate
				}
			}
			if fileSpecIdx >= 0 && fileSpecIdx < len(gp.pdfObjs) {
				if fs, ok := gp.pdfObjs[fileSpecIdx].(fileSpecObj); ok {
					info.Description = fs.description
				}
			}
			return info, nil
		}
	}
	return EmbeddedFileInfo{}, ErrEmbeddedFileNotFound
}

// GetEmbeddedFileNames returns the names of all embedded files.
//
// Example:
//
//	names := pdf.GetEmbeddedFileNames()
func (gp *GoPdf) GetEmbeddedFileNames() []string {
	names := make([]string, len(gp.embeddedFiles))
	for i, ref := range gp.embeddedFiles {
		names[i] = ref.name
	}
	return names
}

// GetEmbeddedFileCount returns the number of embedded files.
//
// Example:
//
//	count := pdf.GetEmbeddedFileCount()
func (gp *GoPdf) GetEmbeddedFileCount() int {
	return len(gp.embeddedFiles)
}
