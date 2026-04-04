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
}

// GenerateOptions configures PDF generation.
type GenerateOptions struct {
	PaperSize string
}
