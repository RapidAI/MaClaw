package gopdf

// ScrubOption controls which sensitive data to remove from the PDF.
type ScrubOption struct {
	// Metadata removes standard PDF metadata (/Info dictionary).
	Metadata bool
	// XMLMetadata removes XMP metadata stream.
	XMLMetadata bool
	// EmbeddedFiles removes all embedded file attachments.
	EmbeddedFiles bool
	// PageLabels removes page label definitions.
	PageLabels bool
}

// DefaultScrubOption returns a ScrubOption with all options enabled.
func DefaultScrubOption() ScrubOption {
	return ScrubOption{
		Metadata:      true,
		XMLMetadata:   true,
		EmbeddedFiles: true,
		PageLabels:    true,
	}
}

// Scrub removes potentially sensitive data from the PDF document.
// This is inspired by PyMuPDF's Document.scrub() method.
//
// By default (with DefaultScrubOption), it removes:
//   - Standard PDF metadata (author, title, subject, etc.)
//   - XMP metadata streams
//   - Embedded file attachments
//   - Page label definitions
//
// After scrubbing, call GarbageCollect(GCCompact) and save with a new
// filename to ensure removed data is physically purged.
//
// Example:
//
//	pdf.Scrub(gopdf.DefaultScrubOption())
//	pdf.GarbageCollect(gopdf.GCCompact)
//	pdf.WritePdf("scrubbed.pdf")
func (gp *GoPdf) Scrub(opt ScrubOption) {
	if opt.Metadata {
		gp.isUseInfo = false
		gp.info = nil
	}

	if opt.XMLMetadata {
		gp.xmpMetadata = nil
	}

	if opt.EmbeddedFiles {
		gp.embeddedFiles = nil
	}

	if opt.PageLabels {
		gp.pageLabels = nil
	}
}
