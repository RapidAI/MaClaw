package gopdf

import (
	"fmt"
	"io"
)

// acroFormObj is the PDF AcroForm dictionary object.
type acroFormObj struct {
	fieldObjIDs []int  // 1-based object IDs of form field objects
	fontRefs    []acroFormFont // fonts used in form fields
	needAppearances bool
}

type acroFormFont struct {
	name  string // e.g. "F1"
	objID int    // 1-based
}

func (a acroFormObj) init(fn func() *GoPdf) {}

func (a acroFormObj) getType() string {
	return "AcroForm"
}

func (a acroFormObj) write(w io.Writer, objID int) error {
	io.WriteString(w, "<<\n")

	// Fields array
	io.WriteString(w, "/Fields [")
	for _, id := range a.fieldObjIDs {
		fmt.Fprintf(w, "%d 0 R ", id)
	}
	io.WriteString(w, "]\n")

	// NeedAppearances — tells viewers to generate appearances
	io.WriteString(w, "/NeedAppearances true\n")

	// Default resources with fonts
	if len(a.fontRefs) > 0 {
		io.WriteString(w, "/DR << /Font <<")
		for _, f := range a.fontRefs {
			fmt.Fprintf(w, " /%s %d 0 R", f.name, f.objID)
		}
		io.WriteString(w, " >> >>\n")
	}

	// Default appearance
	io.WriteString(w, "/DA (/Helv 12 Tf 0 0 0 rg)\n")

	io.WriteString(w, ">>\n")
	return nil
}
