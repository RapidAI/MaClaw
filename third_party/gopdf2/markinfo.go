package gopdf

import (
	"fmt"
	"io"
)

// MarkInfo represents the PDF MarkInfo dictionary, which indicates
// whether the document conforms to Tagged PDF conventions.
type MarkInfo struct {
	// Marked indicates whether the document conforms to Tagged PDF conventions.
	Marked bool
	// UserProperties indicates whether the document contains user properties.
	UserProperties bool
	// Suspects indicates whether the document contains suspects (tag structure may be incorrect).
	Suspects bool
}

// markInfoObj is the PDF MarkInfo dictionary object.
type markInfoObj struct {
	info MarkInfo
}

func (m markInfoObj) init(f func() *GoPdf) {}

func (m markInfoObj) getType() string {
	return "MarkInfo"
}

func (m markInfoObj) write(w io.Writer, objID int) error {
	io.WriteString(w, "<<\n")
	if m.info.Marked {
		io.WriteString(w, "  /Marked true\n")
	} else {
		io.WriteString(w, "  /Marked false\n")
	}
	if m.info.UserProperties {
		io.WriteString(w, "  /UserProperties true\n")
	}
	if m.info.Suspects {
		io.WriteString(w, "  /Suspects true\n")
	}
	io.WriteString(w, ">>\n")
	return nil
}

// SetMarkInfo sets the MarkInfo dictionary in the document catalog.
// This indicates whether the PDF is a Tagged PDF.
//
// Example:
//
//	pdf.SetMarkInfo(gopdf.MarkInfo{Marked: true})
func (gp *GoPdf) SetMarkInfo(info MarkInfo) {
	gp.markInfo = &info
}

// GetMarkInfo returns the current MarkInfo settings, or nil if not set.
func (gp *GoPdf) GetMarkInfo() *MarkInfo {
	return gp.markInfo
}

// FindPagesByLabel returns the 0-based page indices that match the given label string.
// This searches through the page label definitions to find pages with matching labels.
//
// Example:
//
//	pages := pdf.FindPagesByLabel("iii")  // find page labeled "iii"
//	pages := pdf.FindPagesByLabel("A-1")  // find page labeled "A-1"
func (gp *GoPdf) FindPagesByLabel(label string) []int {
	if len(gp.pageLabels) == 0 {
		return nil
	}

	totalPages := gp.GetNumberOfPages()
	if totalPages == 0 {
		return nil
	}

	var result []int
	for pageIdx := 0; pageIdx < totalPages; pageIdx++ {
		pageLabel := gp.computePageLabel(pageIdx)
		if pageLabel == label {
			result = append(result, pageIdx)
		}
	}
	return result
}

// computePageLabel computes the label string for a given 0-based page index.
func (gp *GoPdf) computePageLabel(pageIdx int) string {
	if len(gp.pageLabels) == 0 {
		return fmt.Sprintf("%d", pageIdx+1)
	}

	// Find the applicable label range.
	var applicable *PageLabel
	for i := range gp.pageLabels {
		if gp.pageLabels[i].PageIndex <= pageIdx {
			applicable = &gp.pageLabels[i]
		}
	}
	if applicable == nil {
		return fmt.Sprintf("%d", pageIdx+1)
	}

	offset := pageIdx - applicable.PageIndex
	start := applicable.Start
	if start <= 0 {
		start = 1
	}
	num := start + offset

	var numStr string
	switch applicable.Style {
	case PageLabelDecimal:
		numStr = fmt.Sprintf("%d", num)
	case PageLabelRomanUpper:
		numStr = toRoman(num, true)
	case PageLabelRomanLower:
		numStr = toRoman(num, false)
	case PageLabelAlphaUpper:
		numStr = toAlpha(num, true)
	case PageLabelAlphaLower:
		numStr = toAlpha(num, false)
	default:
		numStr = ""
	}

	return applicable.Prefix + numStr
}

// toRoman converts an integer to Roman numeral string.
func toRoman(n int, upper bool) string {
	if n <= 0 || n > 3999 {
		return fmt.Sprintf("%d", n)
	}
	vals := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	syms := []string{"M", "CM", "D", "CD", "C", "XC", "L", "XL", "X", "IX", "V", "IV", "I"}

	var result string
	for i, v := range vals {
		for n >= v {
			result += syms[i]
			n -= v
		}
	}
	if !upper {
		result = toLowerASCII(result)
	}
	return result
}

// toAlpha converts an integer to alphabetic label (1=A, 2=B, ..., 27=AA).
func toAlpha(n int, upper bool) string {
	if n <= 0 {
		return ""
	}
	var result string
	for n > 0 {
		n--
		ch := byte('A') + byte(n%26)
		result = string(ch) + result
		n /= 26
	}
	if !upper {
		result = toLowerASCII(result)
	}
	return result
}

func toLowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}
