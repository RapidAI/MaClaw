package gopdf

// DocumentStats contains summary statistics about the PDF document.
type DocumentStats struct {
	// PageCount is the total number of pages.
	PageCount int
	// ObjectCount is the total number of PDF objects.
	ObjectCount int
	// LiveObjectCount is the number of non-null objects.
	LiveObjectCount int
	// FontCount is the number of font objects.
	FontCount int
	// ImageCount is the number of image objects.
	ImageCount int
	// ContentStreamCount is the number of content stream objects.
	ContentStreamCount int
	// HasOutlines indicates whether the document has bookmarks.
	HasOutlines bool
	// HasEmbeddedFiles indicates whether the document has attachments.
	HasEmbeddedFiles bool
	// HasXMPMetadata indicates whether XMP metadata is set.
	HasXMPMetadata bool
	// HasPageLabels indicates whether page labels are defined.
	HasPageLabels bool
	// HasOCGs indicates whether optional content groups are defined.
	HasOCGs bool
	// PDFVersion is the configured PDF version.
	PDFVersion PDFVersion
}

// GetDocumentStats returns summary statistics about the document.
//
// Example:
//
//	stats := pdf.GetDocumentStats()
//	fmt.Printf("Pages: %d, Fonts: %d, Images: %d\n",
//	    stats.PageCount, stats.FontCount, stats.ImageCount)
func (gp *GoPdf) GetDocumentStats() DocumentStats {
	stats := DocumentStats{
		PageCount:       gp.GetNumberOfPages(),
		ObjectCount:     gp.GetObjectCount(),
		LiveObjectCount: gp.GetLiveObjectCount(),
		HasOutlines:     gp.outlines != nil && gp.outlines.Count() > 0,
		HasEmbeddedFiles: len(gp.embeddedFiles) > 0,
		HasXMPMetadata:  gp.xmpMetadata != nil,
		HasPageLabels:   len(gp.pageLabels) > 0,
		HasOCGs:         len(gp.ocgs) > 0,
		PDFVersion:      gp.pdfVersion,
	}

	for _, obj := range gp.pdfObjs {
		if obj == nil {
			continue
		}
		switch obj.getType() {
		case "Font", subsetFont:
			stats.FontCount++
		case "Image":
			stats.ImageCount++
		case "Content":
			stats.ContentStreamCount++
		}
	}

	return stats
}

// FontInfo describes a font used in the document.
type FontInfo struct {
	// Family is the font family name.
	Family string
	// Style is the font style (Regular, Bold, Italic, etc.).
	Style int
	// IsEmbedded indicates whether the font file is embedded.
	IsEmbedded bool
	// Index is the internal object index.
	Index int
}

// GetFonts returns information about all fonts in the document.
func (gp *GoPdf) GetFonts() []FontInfo {
	var fonts []FontInfo
	for i, obj := range gp.pdfObjs {
		if obj == nil {
			continue
		}
		switch o := obj.(type) {
		case *FontObj:
			fonts = append(fonts, FontInfo{
				Family:     o.Family,
				Style:      0,
				IsEmbedded: o.IsEmbedFont,
				Index:      i,
			})
		case *SubsetFontObj:
			fonts = append(fonts, FontInfo{
				Family:     o.GetFamily(),
				Style:      0,
				IsEmbedded: true,
				Index:      i,
			})
		}
	}
	return fonts
}
