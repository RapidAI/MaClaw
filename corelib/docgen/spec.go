package docgen

import "time"

// Spec describes a generic Markdown-to-PDF document.
type Spec struct {
	Title          string
	Subtitle       string
	ProjectName    string
	Content        string
	FooterHint     string
	Brand          string
	FileNamePrefix string
	Timestamp      time.Time
	PaperSize      string
	// Colorful paints this document through InsertHTMLBox: distinct heading
	// colors, linked addresses, and a colored （可能） mark. Left false, the
	// muted HTML used by every other document stays as it is. Background
	// colors are not used, because InsertHTMLBox does not paint them.
	Colorful bool
}

// GenerateOptions configures PDF generation.
type GenerateOptions struct {
	PaperSize string
}
