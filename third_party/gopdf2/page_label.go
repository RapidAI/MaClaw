package gopdf

import (
	"fmt"
	"io"
	"sort"
)

// PageLabelStyle defines the numbering style for page labels.
type PageLabelStyle string

const (
	// PageLabelDecimal uses decimal Arabic numerals (1, 2, 3, ...).
	PageLabelDecimal PageLabelStyle = "D"
	// PageLabelRomanUpper uses uppercase Roman numerals (I, II, III, ...).
	PageLabelRomanUpper PageLabelStyle = "R"
	// PageLabelRomanLower uses lowercase Roman numerals (i, ii, iii, ...).
	PageLabelRomanLower PageLabelStyle = "r"
	// PageLabelAlphaUpper uses uppercase letters (A, B, C, ...).
	PageLabelAlphaUpper PageLabelStyle = "A"
	// PageLabelAlphaLower uses lowercase letters (a, b, c, ...).
	PageLabelAlphaLower PageLabelStyle = "a"
	// PageLabelNone uses no numbering; only the prefix is shown.
	PageLabelNone PageLabelStyle = ""
)

// PageLabel defines a page labeling range.
// A page label range starts at a specific page index (0-based) and
// applies to all subsequent pages until the next range.
type PageLabel struct {
	// PageIndex is the 0-based page index where this label range starts.
	PageIndex int

	// Style is the numbering style.
	Style PageLabelStyle

	// Prefix is an optional string prefix prepended to the page number.
	// For example, "A-" would produce labels like "A-1", "A-2", etc.
	Prefix string

	// Start is the starting number for this range (default 1).
	// For example, Start=5 with Style=Decimal produces "5", "6", "7", ...
	Start int
}

// SetPageLabels sets the page label ranges for the document.
// Page labels define how page numbers are displayed in PDF viewers.
//
// Example:
//
//	pdf.SetPageLabels([]gopdf.PageLabel{
//	    {PageIndex: 0, Style: gopdf.PageLabelRomanLower, Start: 1},  // i, ii, iii (cover pages)
//	    {PageIndex: 3, Style: gopdf.PageLabelDecimal, Start: 1},     // 1, 2, 3, ... (main content)
//	    {PageIndex: 10, Style: gopdf.PageLabelAlphaUpper, Prefix: "Appendix ", Start: 1}, // Appendix A, B, ...
//	})
func (gp *GoPdf) SetPageLabels(labels []PageLabel) {
	gp.pageLabels = labels
}

// GetPageLabels returns the current page label ranges.
func (gp *GoPdf) GetPageLabels() []PageLabel {
	return gp.pageLabels
}

// pageLabelObj is the PDF PageLabels number tree object.
type pageLabelObj struct {
	labels []PageLabel
}

func (p pageLabelObj) init(f func() *GoPdf) {}

func (p pageLabelObj) getType() string {
	return "PageLabels"
}

func (p pageLabelObj) write(w io.Writer, objID int) error {
	// Sort labels by page index.
	sorted := make([]PageLabel, len(p.labels))
	copy(sorted, p.labels)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].PageIndex < sorted[j].PageIndex
	})

	io.WriteString(w, "<<\n")
	io.WriteString(w, "  /Nums [\n")

	for _, label := range sorted {
		fmt.Fprintf(w, "    %d <<\n", label.PageIndex)
		if label.Style != "" {
			fmt.Fprintf(w, "      /S /%s\n", string(label.Style))
		}
		if label.Prefix != "" {
			fmt.Fprintf(w, "      /P (%s)\n", escapeAnnotString(label.Prefix))
		}
		start := label.Start
		if start <= 0 {
			start = 1
		}
		fmt.Fprintf(w, "      /St %d\n", start)
		io.WriteString(w, "    >>\n")
	}

	io.WriteString(w, "  ]\n")
	io.WriteString(w, ">>\n")
	return nil
}
