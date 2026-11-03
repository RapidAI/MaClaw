package gopdf

import (
	"fmt"
	"io"
)

// namesObj is the PDF Names dictionary object.
// It holds the EmbeddedFiles name tree.
type namesObj struct {
	embeddedFiles []embeddedFileRef
}

func (n namesObj) init(f func() *GoPdf) {}

func (n namesObj) getType() string {
	return "Names"
}

func (n namesObj) write(w io.Writer, objID int) error {
	io.WriteString(w, "<<\n")
	if len(n.embeddedFiles) > 0 {
		io.WriteString(w, "  /EmbeddedFiles <<\n")
		io.WriteString(w, "    /Names [\n")
		for _, ef := range n.embeddedFiles {
			fmt.Fprintf(w, "      (%s) %d 0 R\n", escapeAnnotString(ef.name), ef.fileSpecObjID)
		}
		io.WriteString(w, "    ]\n")
		io.WriteString(w, "  >>\n")
	}
	io.WriteString(w, ">>\n")
	return nil
}
