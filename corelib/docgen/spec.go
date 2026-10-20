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
	// Colorful paints formulas, links, and the （可能） mark. Text colors come
	// from the PDF style once one is bound. Background fills are not used in
	// HTML, because InsertHTMLBox does not paint them.
	Colorful bool
	// StyleID selects one PDF style. Empty uses the process active style,
	// which defaults to the first built-in style.
	StyleID string
	// boundStyle freezes the style for one render. Generate sets it so a
	// settings change cannot restyle later pages of the same document.
	boundStyle Style
}

// GenerateOptions configures PDF generation.
type GenerateOptions struct {
	PaperSize string
}
