package gopdf

import (
	"fmt"
	"io"
)

// ObjID represents a typed PDF object identifier.
// It wraps the 0-based index into the pdfObjs array and provides
// safe conversion to 1-based PDF object references.
//
// This replaces raw int indices throughout the codebase, providing
// type safety and preventing accidental misuse of array indices
// as PDF object IDs (or vice versa).
type ObjID int

const invalidObjID ObjID = -1

// Index returns the 0-based array index.
func (id ObjID) Index() int { return int(id) }

// Ref returns the 1-based PDF object reference number.
func (id ObjID) Ref() int { return int(id) + 1 }

// RefStr returns the PDF indirect reference string (e.g. "5 0 R").
func (id ObjID) RefStr() string {
	return fmt.Sprintf("%d 0 R", id.Ref())
}

// IsValid returns true if the ObjID points to a valid object.
func (id ObjID) IsValid() bool { return id >= 0 }

// nullObj is a placeholder PDF object used when an object is logically
// deleted but its slot must be preserved to maintain object numbering.
// This fixes the nil-pointer crash that occurred when DeletePage set
// pdfObjs entries to nil.
type nullObj struct{}

func (n nullObj) init(f func() *GoPdf) {}
func (n nullObj) getType() string       { return "Null" }
func (n nullObj) write(w io.Writer, objID int) error {
	_, err := io.WriteString(w, "null\n")
	return err
}

// GetObjID returns the ObjID for the object at the given 0-based index.
// Returns invalidObjID if the index is out of range.
func (gp *GoPdf) GetObjID(index int) ObjID {
	if index < 0 || index >= len(gp.pdfObjs) {
		return invalidObjID
	}
	return ObjID(index)
}

// GetObjByID returns the IObj at the given ObjID.
// Returns nil if the ObjID is invalid or out of range.
func (gp *GoPdf) GetObjByID(id ObjID) IObj {
	if !id.IsValid() || id.Index() >= len(gp.pdfObjs) {
		return nil
	}
	return gp.pdfObjs[id.Index()]
}

// GetObjType returns the type string of the object at the given ObjID.
// Returns "" if the ObjID is invalid.
func (gp *GoPdf) GetObjType(id ObjID) string {
	obj := gp.GetObjByID(id)
	if obj == nil {
		return ""
	}
	return obj.getType()
}

// CatalogObjID returns the ObjID of the catalog object.
func (gp *GoPdf) CatalogObjID() ObjID {
	return ObjID(gp.indexOfCatalogObj)
}

// PagesObjID returns the ObjID of the pages object.
func (gp *GoPdf) PagesObjID() ObjID {
	return ObjID(gp.indexOfPagesObj)
}
