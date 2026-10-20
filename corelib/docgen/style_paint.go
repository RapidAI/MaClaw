package docgen

import (
	"fmt"
	"strings"

	gopdf "github.com/VantageDataChat/GoPDF2"
	ttfcore "github.com/VantageDataChat/GoPDF2/fontmaker/core"
)

func paintPageChrome(pdf *gopdf.GoPdf, layout pdfPageLayout, style Style) {
	if pdf == nil || style.ID == "" {
		return
	}
	navy := mustRGB(style.Colors.Navy)
	accent := mustRGB(style.Colors.Accent)
	gold := mustRGB(style.Colors.Gold)
	switch style.Motif {
	case MotifRail:
		pdf.SetFillColor(accent[0], accent[1], accent[2])
		pdf.RectFromUpperLeftWithStyle(0, 0, 8, layout.pageH, "F")
	case MotifFrame:
		pdf.SetStrokeColor(gold[0], gold[1], gold[2])
		pdf.SetLineWidth(1.15)
		pdf.RectFromUpperLeftWithStyle(16, 14, layout.pageW-32, layout.pageH-28, "D")
		pdf.SetStrokeColor(navy[0], navy[1], navy[2])
		pdf.SetLineWidth(0.45)
		pdf.RectFromUpperLeftWithStyle(20, 18, layout.pageW-40, layout.pageH-36, "D")
	case MotifBand:
		pdf.SetFillColor(navy[0], navy[1], navy[2])
		pdf.RectFromUpperLeftWithStyle(0, 0, layout.pageW, 8, "F")
		pdf.SetFillColor(accent[0], accent[1], accent[2])
		pdf.RectFromUpperLeftWithStyle(layout.marginX, 14, 72, 5, "F")
	case MotifLedger:
		pdf.SetFillColor(navy[0], navy[1], navy[2])
		pdf.RectFromUpperLeftWithStyle(layout.marginX, 18, layout.contentW, 3.4, "F")
	default:
		pdf.SetFillColor(navy[0], navy[1], navy[2])
		pdf.RectFromUpperLeftWithStyle(0, 0, layout.pageW, 14, "F")
		pdf.SetFillColor(accent[0], accent[1], accent[2])
		pdf.RectFromUpperLeftWithStyle(0, 14, layout.pageW, 3, "F")
	}
}

func paintTitleUnderline(pdf *gopdf.GoPdf, layout pdfPageLayout, style Style, y float64) {
	if pdf == nil || style.ID == "" {
		return
	}
	accent := mustRGB(style.Colors.Accent)
	navy := mustRGB(style.Colors.Navy)
	gold := mustRGB(style.Colors.Gold)
	switch style.Motif {
	case MotifFrame:
		pdf.SetStrokeColor(gold[0], gold[1], gold[2])
		pdf.SetLineWidth(0.9)
		pdf.Line(layout.marginX+36, y, layout.marginX+layout.contentW-36, y)
		pdf.SetStrokeColor(navy[0], navy[1], navy[2])
		pdf.SetLineWidth(0.35)
		pdf.Line(layout.marginX+36, y+3, layout.marginX+layout.contentW-36, y+3)
	case MotifLedger:
		pdf.SetStrokeColor(gold[0], gold[1], gold[2])
		pdf.SetLineWidth(0.7)
		pdf.Line(layout.marginX, y, layout.marginX+layout.contentW, y)
	case MotifRail:
		pdf.SetStrokeColor(accent[0], accent[1], accent[2])
		pdf.SetLineWidth(1.3)
		pdf.Line(layout.marginX, y, layout.marginX+layout.contentW, y)
	default:
		pdf.SetStrokeColor(accent[0], accent[1], accent[2])
		pdf.SetLineWidth(1.7)
		pdf.Line(layout.marginX, y, layout.marginX+layout.contentW, y)
	}
}

func styledTitleColors(style Style) (title, subtitle, project, brand, size string) {
	title = cssHex(style.Colors.Navy)
	subtitle = cssHex(style.Colors.Accent)
	project = cssHex(style.Colors.Ink)
	brand = cssHex(style.Colors.Meta)
	size = "18pt"
	switch style.Motif {
	case MotifFrame:
		title = cssHex(style.Colors.Accent)
		subtitle = cssHex(style.Colors.Gold)
	case MotifRail:
		title = cssHex(style.Colors.Ink)
		subtitle = cssHex(style.Colors.Accent)
	case MotifLedger:
		size = "15pt"
		subtitle = cssHex(style.Colors.Meta)
	}
	return title, subtitle, project, brand, size
}

// styleRuleGlyph is the body-font character used for a markdown rule.
// selectStyleRuleGlyph picks the first one the embedded face actually maps.
// A missing box-drawing glyph would print as .notdef.
var styleRuleGlyph = "━"

func selectStyleRuleGlyph(font []byte) {
	if len(font) == 0 {
		return
	}
	var parser ttfcore.TTFParser
	if err := parser.ParseFontData(font); err != nil {
		return
	}
	chars := parser.Chars()
	for _, r := range []rune{'━', '—', '－'} {
		if _, ok := chars[int(r)]; ok {
			styleRuleGlyph = string(r)
			return
		}
	}
}

func styledRuleHTML(style Style) string {
	if style.ID == "" {
		return "<hr/>"
	}
	return fmt.Sprintf(`<p style="font-size:6pt; color:%s">%s</p>`, cssHex(style.Colors.Rule), strings.Repeat(styleRuleGlyph, 68))
}

// styleMarkGlyph is a fullwidth bar from the CJK font. Block-element bars such
// as U+258D are missing from Noto Sans CJK and would print as .notdef.
const styleMarkGlyph = "\uFF5C"

func headingMark(style Style, prefix string) string {
	if style.ID == "" || style.Motif != MotifMasthead || prefix != "## " {
		return ""
	}
	return fmt.Sprintf(`<span style="color:%s">%s </span>`, cssHex(style.Colors.Accent), styleMarkGlyph)
}
