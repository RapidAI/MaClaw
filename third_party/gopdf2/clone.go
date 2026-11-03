package gopdf

// Clone creates a deep copy of the GoPdf instance by serializing and
// re-importing the document. The cloned document is independent — changes
// to the clone do not affect the original, and vice versa.
//
// This is useful for creating variants of a document (e.g. different
// watermarks or stamps) without re-generating from scratch.
//
// Note: Header/footer callback functions are NOT cloned (they are set to nil).
// Font data, images, and all page content are fully duplicated.
//
// Example:
//
//	original := &gopdf.GoPdf{}
//	original.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
//	original.AddPage()
//	// ... build document ...
//
//	clone, err := original.Clone()
//	if err != nil { log.Fatal(err) }
//	// clone is a fully independent copy
//	clone.SetPage(1)
//	clone.SetXY(100, 100)
//	clone.Text("Only in clone")
//	clone.WritePdf("clone.pdf")
func (gp *GoPdf) Clone() (*GoPdf, error) {
	// Serialize the current document to bytes.
	data, err := gp.GetBytesPdfReturnErr()
	if err != nil {
		return nil, err
	}

	// Re-import into a new GoPdf instance.
	clone := &GoPdf{}
	err = clone.OpenPDFFromBytes(data, nil)
	if err != nil {
		return nil, err
	}

	// Copy metadata that isn't part of the PDF stream.
	clone.pdfVersion = gp.pdfVersion
	if gp.xmpMetadata != nil {
		metaCopy := *gp.xmpMetadata
		clone.xmpMetadata = &metaCopy
	}
	if len(gp.pageLabels) > 0 {
		clone.pageLabels = make([]PageLabel, len(gp.pageLabels))
		copy(clone.pageLabels, gp.pageLabels)
	}

	return clone, nil
}
