package docgen

import (
	"fmt"
	"math"
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
	rule := mustRGB(style.Colors.Rule)
	switch style.Motif {
	case MotifRail:
		pdf.SetFillColor(accent[0], accent[1], accent[2])
		pdf.RectFromUpperLeftWithStyle(0, 0, 8, layout.pageH, "F")
		// Three ticks at the top-right answer the color column; a small
		// chevron at the column's foot points the eye onward.
		pdfTickRow(pdf, layout.pageW-52, 14, 16, 3.2, 4, 3, 1,
			cssHex(style.Colors.Accent), "FFFFFF")
		pdfChev(pdf, 1.6, layout.pageH-30, 4.8, 6, cssHex(style.Colors.Gold))
	case MotifFrame:
		pdf.SetStrokeColor(gold[0], gold[1], gold[2])
		pdf.SetLineWidth(1.15)
		pdf.RectFromUpperLeftWithStyle(16, 14, layout.pageW-32, layout.pageH-28, "D")
		pdf.SetStrokeColor(navy[0], navy[1], navy[2])
		pdf.SetLineWidth(0.45)
		pdf.RectFromUpperLeftWithStyle(20, 18, layout.pageW-40, layout.pageH-36, "D")
		// A diamond seals the top-left corner; a small triangle nods from the
		// bottom-right — jewelry on the frame's miters.
		pdfDia(pdf, 16, 14, 7, 7, cssHex(style.Colors.Gold))
		pdfTriDown(pdf, layout.pageW-24, layout.pageH-19, 8, 6, cssHex(style.Colors.Accent))
	case MotifBand:
		pdf.SetFillColor(navy[0], navy[1], navy[2])
		pdf.RectFromUpperLeftWithStyle(0, 0, layout.pageW, 8, "F")
		pdfTickRow(pdf, layout.marginX, 14, 22, 5, 4, 3, 1,
			cssHex(style.Colors.Accent), cssHex(style.Colors.Navy))
		pdfChev(pdf, layout.pageW-52, 1.6, 9, 4.8, cssHex(style.Colors.Gold))
		pdfDia(pdf, layout.pageW-36, 4, 8, 8, blendHex(style.Colors.OnDark, style.Colors.Navy, 0.45))
	case MotifLedger:
		pdf.SetFillColor(navy[0], navy[1], navy[2])
		pdf.RectFromUpperLeftWithStyle(layout.marginX, 18, layout.contentW, 3.4, "F")
		// Three shrinking gold squares taper the rule's right end.
		for i := 0; i < 3; i++ {
			d := 3.4 - float64(i)*0.9
			pdf.SetFillColor(gold[0], gold[1], gold[2])
			pdf.RectFromUpperLeftWithStyle(layout.marginX+layout.contentW-8-float64(i)*7, 18+(3.4-d)/2, d, d, "F")
		}
		// A notched triangle dips below the rule's left like a ledger marker.
		pdfTriDown(pdf, layout.marginX, 21.4, 9, 6, cssHex(style.Colors.Accent))
	case MotifSidebar:
		pdf.SetFillColor(navy[0], navy[1], navy[2])
		pdf.RectFromUpperLeftWithStyle(0, 0, 14, layout.pageH, "F")
		pdf.SetFillColor(accent[0], accent[1], accent[2])
		pdf.RectFromUpperLeftWithStyle(14, 0, 2, layout.pageH, "F")
		// A ring crowns the column; three gold dots march down toward the footer.
		pdfRing(pdf, 7, 26, 4, cssHex(style.Colors.Gold), 0.9)
		for i := 0; i < 3; i++ {
			d := 3.2 - float64(i)*0.8
			pdf.SetFillColor(gold[0], gold[1], gold[2])
			pdf.RectFromUpperLeftWithStyle(7-d/2, layout.pageH-64-float64(i)*14, d, d, "F")
		}
	case MotifPlate:
		pdf.SetFillColor(navy[0], navy[1], navy[2])
		pdf.RectFromUpperLeftWithStyle(layout.marginX, 10, layout.contentW, 18, "F")
		pdf.SetFillColor(accent[0], accent[1], accent[2])
		pdf.RectFromUpperLeftWithStyle(layout.marginX, 28, layout.contentW, 2, "F")
		paintCornerTicks(pdf, layout, gold)
		// Four onDark dots taper toward the plate's right end.
		for i := 0; i < 4; i++ {
			d := 2.6 - float64(i)*0.5
			pdf.SetFillColor(gold[0], gold[1], gold[2])
			pdf.RectFromUpperLeftWithStyle(layout.marginX+layout.contentW-20-float64(i)*8, 19-d/2, d, d, "F")
		}
		// A small chevron inside the plate's left edge leads the title in.
		pdfChev(pdf, layout.marginX+6, 15, 8, 8, cssHex(style.Colors.OnDark))
	case MotifBaseline:
		// Swiss grid: one hairline baseline, a square marker at its left, and
		// a faint ruled column on the right edge of the page.
		pdf.SetFillColor(navy[0], navy[1], navy[2])
		pdf.RectFromUpperLeftWithStyle(layout.marginX, 16, 7, 7, "F")
		pdf.SetFillColor(rule[0], rule[1], rule[2])
		pdf.RectFromUpperLeftWithStyle(layout.marginX, 26, layout.contentW, 0.9, "F")
		pdf.SetFillColor(accent[0], accent[1], accent[2])
		pdf.RectFromUpperLeftWithStyle(layout.marginX, 26, 34, 0.9, "F")
		faint := mustRGB(blendHex(style.Colors.Rule, "FFFFFF", 0.55))
		for i := 0; i < 3; i++ {
			pdf.SetFillColor(faint[0], faint[1], faint[2])
			pdf.RectFromUpperLeftWithStyle(layout.marginX, layout.pageH-52+float64(i)*9, layout.contentW, 0.5, "F")
		}
	case MotifRibbon:
		// A hanging ribbon: full-width navy band with a folded parallelogram
		// tab tucking under its right end and a gold under-edge.
		pdf.SetFillColor(navy[0], navy[1], navy[2])
		pdf.RectFromUpperLeftWithStyle(0, 0, layout.pageW, 22, "F")
		pdfPara(pdf, layout.pageW-64, 0, 26, 30, 9, blendHex(style.Colors.Accent, style.Colors.Navy, 0.3))
		pdf.SetFillColor(gold[0], gold[1], gold[2])
		pdf.RectFromUpperLeftWithStyle(0, 22, layout.pageW, 1.6, "F")
		pdfChev(pdf, layout.pageW-30, 6.5, 10, 9, cssHex(style.Colors.OnDark))
	case MotifApex:
		// Angular apex marks: a pale summit pair behind the header, an accent
		// peak riding the top edge, a hairline under it all.
		pdfTri(pdf, layout.pageW-150, 0, 74, 34, blendHex(style.Colors.Navy, "FFFFFF", 0.82))
		pdfTri(pdf, layout.pageW-96, 0, 62, 30, blendHex(style.Colors.Navy, "FFFFFF", 0.68))
		pdfTri(pdf, layout.pageW-58, 0, 58, 28, cssHex(style.Colors.Navy))
		pdf.SetFillColor(accent[0], accent[1], accent[2])
		pdf.RectFromUpperLeftWithStyle(0, 0, layout.pageW, 3, "F")
		pdf.SetFillColor(gold[0], gold[1], gold[2])
		pdf.RectFromUpperLeftWithStyle(0, 3, 56, 1.6, "F")
	default:
		pdf.SetFillColor(navy[0], navy[1], navy[2])
		pdf.RectFromUpperLeftWithStyle(0, 0, layout.pageW, 14, "F")
		pdf.SetFillColor(accent[0], accent[1], accent[2])
		pdf.RectFromUpperLeftWithStyle(0, 14, layout.pageW, 3, "F")
		// A gold hairline under the accent turns the strip into a double rule,
		// and a small triangle+diamond pair caps the bar's right end.
		pdf.SetFillColor(gold[0], gold[1], gold[2])
		pdf.RectFromUpperLeftWithStyle(0, 18.6, layout.pageW, 0.6, "F")
		pdfTri(pdf, layout.pageW-34, 4.4, 10, 7, cssHex(style.Colors.OnDark))
		pdfDia(pdf, layout.pageW-46, 8, 8, 8, cssHex(style.Colors.Gold))
	}
}

// paintCornerTicks draws the four small L marks the plate motif uses to frame
// the content area. Each tick is two thin rects sharing a corner.
func paintCornerTicks(pdf *gopdf.GoPdf, layout pdfPageLayout, gold [3]uint8) {
	const inset, arm, t = 12.0, 14.0, 1.4
	pdf.SetFillColor(gold[0], gold[1], gold[2])
	corners := [][4]float64{
		{inset, inset, arm, t}, {inset, inset, t, arm},
		{layout.pageW - inset - arm, inset, arm, t}, {layout.pageW - inset - t, inset, t, arm},
		{inset, layout.pageH - inset - t, arm, t}, {inset, layout.pageH - inset - arm, t, arm},
		{layout.pageW - inset - arm, layout.pageH - inset - t, arm, t}, {layout.pageW - inset - t, layout.pageH - inset - arm, t, arm},
	}
	for _, c := range corners {
		pdf.RectFromUpperLeftWithStyle(c[0], c[1], c[2], c[3], "F")
	}
}

func blendHex(a, b string, t float64) string {
	ca, cb := mustRGB(a), mustRGB(b)
	mix := func(x, y uint8) uint8 {
		v := math.Round(float64(x)*(1-t) + float64(y)*t)
		if v < 0 {
			v = 0
		}
		if v > 255 {
			v = 255
		}
		return uint8(v)
	}
	return formatHex([3]uint8{mix(ca[0], cb[0]), mix(ca[1], cb[1]), mix(ca[2], cb[2])})
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
	case MotifBaseline:
		title = cssHex(style.Colors.Ink)
		subtitle = cssHex(style.Colors.Meta)
		size = "19pt"
	case MotifApex:
		subtitle = cssHex(style.Colors.Gold)
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

// headingMark is the glyph or number prefix a motif puts before a heading.
// index is the 1-based H2 counter. The masthead keeps its fullwidth bar and
// adds the section number; band, sidebar, and plate lead with the number.
// Frame, rail, and ledger stay plain so their frames and rules carry the
// structure alone.
func headingMark(style Style, prefix string, index int) string {
	if style.ID == "" || prefix != "## " {
		return ""
	}
	accent := cssHex(style.Colors.Accent)
	gold := cssHex(style.Colors.Gold)
	switch style.Motif {
	case MotifMasthead:
		return fmt.Sprintf(`<span style="color:%s">%02d %s </span>`, accent, index, styleMarkGlyph)
	case MotifBand, MotifSidebar, MotifPlate:
		return fmt.Sprintf(`<span style="color:%s">%02d </span><span style="color:%s">/ </span>`, accent, index, gold)
	case MotifBaseline:
		return fmt.Sprintf(`<span style="color:%s">%02d </span><span style="color:%s">■ </span>`, gold, index, accent)
	case MotifRibbon:
		return fmt.Sprintf(`<span style="color:%s">%02d </span><span style="color:%s">◆ </span>`, accent, index, gold)
	case MotifApex:
		return fmt.Sprintf(`<span style="color:%s">▲ </span><span style="color:%s">%02d </span>`, gold, accent, index)
	default:
		return ""
	}
}
