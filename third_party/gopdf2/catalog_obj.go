package gopdf

import (
	"fmt"
	"io"
)

// CatalogObj : catalog dictionary
type CatalogObj struct { //impl IObj
	outlinesObjID      int
	namesObjID         int // index of Names dictionary object (-1 = none)
	pageLabelsObjID    int // index of PageLabels object (-1 = none)
	metadataObjID      int // index of XMP Metadata stream object (-1 = none)
	ocPropertiesObjID  int // index of OCProperties object (-1 = none)
	acroFormObjID      int // index of AcroForm object (-1 = none)
	markInfoObjID      int // index of MarkInfo object (-1 = none)
	pageLayout         string
	pageMode           string
}

func (c *CatalogObj) init(funcGetRoot func() *GoPdf) {
	c.outlinesObjID = -1
	c.namesObjID = -1
	c.pageLabelsObjID = -1
	c.metadataObjID = -1
	c.ocPropertiesObjID = -1
	c.acroFormObjID = -1
	c.markInfoObjID = -1
}

func (c *CatalogObj) getType() string {
	return "Catalog"
}

func (c *CatalogObj) write(w io.Writer, objID int) error {
	io.WriteString(w, "<<\n")
	fmt.Fprintf(w, "  /Type /%s\n", c.getType())
	io.WriteString(w, "  /Pages 2 0 R\n")
	if c.outlinesObjID >= 0 {
		io.WriteString(w, "  /PageMode /UseOutlines\n")
		fmt.Fprintf(w, "  /Outlines %d 0 R\n", c.outlinesObjID)
	}
	if c.namesObjID >= 0 {
		fmt.Fprintf(w, "  /Names %d 0 R\n", c.namesObjID)
	}
	if c.pageLabelsObjID >= 0 {
		fmt.Fprintf(w, "  /PageLabels %d 0 R\n", c.pageLabelsObjID)
	}
	if c.metadataObjID >= 0 {
		fmt.Fprintf(w, "  /Metadata %d 0 R\n", c.metadataObjID)
	}
	if c.ocPropertiesObjID >= 0 {
		fmt.Fprintf(w, "  /OCProperties %d 0 R\n", c.ocPropertiesObjID)
	}
	if c.acroFormObjID >= 0 {
		fmt.Fprintf(w, "  /AcroForm %d 0 R\n", c.acroFormObjID)
	}
	if c.markInfoObjID >= 0 {
		fmt.Fprintf(w, "  /MarkInfo %d 0 R\n", c.markInfoObjID)
	}
	if c.pageLayout != "" {
		fmt.Fprintf(w, "  /PageLayout /%s\n", c.pageLayout)
	}
	if c.pageMode != "" && c.outlinesObjID < 0 {
		fmt.Fprintf(w, "  /PageMode /%s\n", c.pageMode)
	}
	io.WriteString(w, ">>\n")
	return nil
}

func (c *CatalogObj) SetIndexObjOutlines(index int) {
	c.outlinesObjID = index + 1
}

// SetIndexObjNames sets the Names dictionary object reference.
func (c *CatalogObj) SetIndexObjNames(index int) {
	c.namesObjID = index + 1
}

// SetIndexObjPageLabels sets the PageLabels object reference.
func (c *CatalogObj) SetIndexObjPageLabels(index int) {
	c.pageLabelsObjID = index + 1
}

// SetIndexObjMetadata sets the XMP Metadata stream object reference.
func (c *CatalogObj) SetIndexObjMetadata(index int) {
	c.metadataObjID = index + 1
}

// SetIndexObjOCProperties sets the OCProperties object reference.
func (c *CatalogObj) SetIndexObjOCProperties(index int) {
	c.ocPropertiesObjID = index + 1
}

// SetIndexObjAcroForm sets the AcroForm object reference.
func (c *CatalogObj) SetIndexObjAcroForm(index int) {
	c.acroFormObjID = index + 1
}

// SetIndexObjMarkInfo sets the MarkInfo object reference.
func (c *CatalogObj) SetIndexObjMarkInfo(index int) {
	c.markInfoObjID = index + 1
}
