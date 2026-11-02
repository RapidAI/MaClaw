package docgen

import (
	"fmt"
	"strings"

	gopdf "github.com/VantageDataChat/GoPDF2"
)

// The cover page is drawn with raw primitives plus one text box per line:
// InsertHTMLBox paints text only, so every field, band, rule, and panel on
// the cover comes from Rect/Line calls shaped by the style motif. The body
// starts on page 2, and the cover carries no footer — its meta rows replace
// one.

// coverPanelTint mixes a style color toward white for the quiet fields behind
// cover meta text. t is the share of the style color that survives.
func coverPanelTint(hex string, t float64) string {
	return blendHex(hex, "FFFFFF", 1-t)
}

// coverText draws one text line inside the cover.
func coverText(pdf *gopdf.GoPdf, x, y, w, h float64, text string, sizePt float64, color string, bold bool, align string) {
	text = strings.TrimSpace(text)
	if pdf == nil || text == "" || w <= 0 || h <= 0 {
		return
	}
	weight, closeWeight := "", ""
	if bold {
		weight, closeWeight = "<b>", "</b>"
	}
	alignAttr := ""
	if align == "center" {
		alignAttr = "text-align:center; "
	} else if align == "right" {
		alignAttr = "text-align:right; "
	}
	// The bold tag must close inside the paragraph: an unclosed <b> makes the
	// HTML box drop the paragraph's inline color and fall back to default ink.
	html := fmt.Sprintf(`<p style="%sfont-size:%gpt; color:#%s">%s%s%s</p>`, alignAttr, sizePt, color, weight, escapeHTML(text), closeWeight)
	_, _ = pdf.InsertHTMLBox(x, y, w, h, html, gopdf.HTMLBoxOption{
		DefaultFontFamily: "regular",
		DefaultFontSize:   10,
		BoldFontFamily:    "bold",
		LineSpacing:       2,
	})
}

func coverFill(pdf *gopdf.GoPdf, hex string, x, y, w, h float64) {
	rgb := mustRGB(hex)
	pdf.SetFillColor(rgb[0], rgb[1], rgb[2])
	pdf.RectFromUpperLeftWithStyle(x, y, w, h, "F")
}

// coverMetaDate is the date line every cover shows.
func coverMetaDate(spec Spec) string {
	return fileTimestamp(spec).Format("2006-01-02")
}

func coverTitle(spec Spec) string {
	return fallbackText(spec.Title, spec.ProjectName)
}

// coverProjectName is the project line under the title. When it duplicates the
// title (workflow docs name both from the project), it stays empty — the old
// title block had the same guard.
func coverProjectName(spec Spec) string {
	project := strings.TrimSpace(spec.ProjectName)
	if project == "" || project == coverTitle(spec) {
		return ""
	}
	return project
}

// paintStyleCover draws page 1 for the bound style. The motif decides the
// composition; the colors only paint it.
func paintStyleCover(pdf *gopdf.GoPdf, layout pdfPageLayout, style Style, spec Spec) {
	if pdf == nil || style.ID == "" {
		return
	}
	switch style.Motif {
	case MotifRail:
		paintRailCover(pdf, layout, style, spec)
	case MotifFrame:
		paintFrameCover(pdf, layout, style, spec)
	case MotifBand:
		paintBandCover(pdf, layout, style, spec)
	case MotifLedger:
		paintLedgerCover(pdf, layout, style, spec)
	case MotifSidebar:
		paintSidebarCover(pdf, layout, style, spec)
	case MotifPlate:
		paintPlateCover(pdf, layout, style, spec)
	default:
		paintMastheadCover(pdf, layout, style, spec)
	}
}

func paintMastheadCover(pdf *gopdf.GoPdf, layout pdfPageLayout, style Style, spec Spec) {
	const bandH = 296
	x := layout.marginX
	w := layout.contentW
	coverFill(pdf, style.Colors.Navy, 0, 0, layout.pageW, bandH)
	coverFill(pdf, style.Colors.Accent, 0, bandH, layout.pageW, 3)
	coverText(pdf, x, 52, w, 20, spec.Brand, 10.5, style.Colors.OnDark, false, "")
	coverText(pdf, x, 108, w, 120, coverTitle(spec), 27, style.Colors.OnDark, true, "")
	coverFill(pdf, style.Colors.Accent, x, bandH-52, 64, 3)
	coverText(pdf, x, bandH-40, w, 26, spec.Subtitle, 11.5, style.Colors.OnDark, false, "")
	// A quiet dot grid fills the band's quiet right side.
	pdfDotGrid(pdf, layout.pageW-124, 64, 2.2, 9, 6, 3, blendHex(style.Colors.OnDark, style.Colors.Navy, 0.55))
	// Below the band the meta block reads as tagged fields.
	metaY := bandH + 56.0
	coverMetaLine(pdf, x, metaY, w/2-8, "项目 / PROJECT", coverProjectName(spec), style.Colors.Meta, style.Colors.Ink, "")
	coverMetaLine(pdf, x+w/2+8, metaY, w/2-8, "日期 / DATE", coverMetaDate(spec), style.Colors.Meta, style.Colors.Meta, "")
	if strings.TrimSpace(spec.FooterHint) != "" {
		coverText(pdf, x, layout.pageH-72, w, 18, spec.FooterHint, 9.5, style.Colors.Meta, false, "")
	}
}

func paintRailCover(pdf *gopdf.GoPdf, layout pdfPageLayout, style Style, spec Spec) {
	const colW = 170.0
	x := colW + 40
	w := layout.pageW - x - layout.marginX
	coverFill(pdf, style.Colors.Navy, 0, 0, colW, layout.pageH)
	coverFill(pdf, style.Colors.Accent, colW, 0, 3, layout.pageH)
	coverFill(pdf, style.Colors.Gold, 24, 56, 26, 26)
	coverText(pdf, 24, 96, colW-48, 60, spec.Brand, 10.5, style.Colors.OnDark, false, "")
	coverText(pdf, 24, layout.pageH-96, colW-48, 18, coverMetaDate(spec), 9.5, style.Colors.OnDark, false, "")
	pdfDotGrid(pdf, 38, layout.pageH-150, 3.2, 10, 1, 3, blendHex(style.Colors.Gold, style.Colors.Navy, 0.35))
	coverText(pdf, x, 176, w, 130, coverTitle(spec), 25, style.Colors.Ink, true, "")
	splitRulePDF(pdf, x, 345, w*0.6, style.Colors.Gold, coverPanelTint(style.Colors.Meta, 0.25))
	coverText(pdf, x, 366, w, 60, spec.Subtitle, 11.5, style.Colors.Meta, false, "")
	coverMetaLine(pdf, x, 452, w/2-8, "项目 / PROJECT", coverProjectName(spec), style.Colors.Meta, style.Colors.Navy, "")
	pdfTickRow(pdf, x+w/2+8, 470, 16, 2.4, 5, 4, 1, style.Colors.Gold, "FFFFFF")
	if strings.TrimSpace(spec.FooterHint) != "" {
		coverText(pdf, x, layout.pageH-96, w, 18, spec.FooterHint, 9.5, style.Colors.Meta, false, "")
	}
}

func paintFrameCover(pdf *gopdf.GoPdf, layout pdfPageLayout, style Style, spec Spec) {
	cx := layout.pageW / 2
	coverFill(pdf, "FFFFFF", 0, 0, layout.pageW, layout.pageH)
	pdf.SetStrokeColor(mustRGB(style.Colors.Gold)[0], mustRGB(style.Colors.Gold)[1], mustRGB(style.Colors.Gold)[2])
	pdf.SetLineWidth(1.2)
	pdf.RectFromUpperLeftWithStyle(30, 26, layout.pageW-60, layout.pageH-52, "D")
	pdf.SetStrokeColor(mustRGB(style.Colors.Navy)[0], mustRGB(style.Colors.Navy)[1], mustRGB(style.Colors.Navy)[2])
	pdf.SetLineWidth(0.5)
	pdf.RectFromUpperLeftWithStyle(38, 34, layout.pageW-76, layout.pageH-68, "D")
	// One gold mark sits on each inner corner to carry the dossier identity.
	goldRect := func(x, y float64) { coverFill(pdf, style.Colors.Gold, x, y, 5, 5) }
	goldRect(35, 31)
	goldRect(layout.pageW-40, 31)
	goldRect(35, layout.pageH-36)
	goldRect(layout.pageW-40, layout.pageH-36)
	// Small rings mark the frame's edge midpoints.
	pdfRing(pdf, cx, 26, 3, style.Colors.Gold, 0.8)
	pdfRing(pdf, cx, layout.pageH-26, 3, style.Colors.Gold, 0.8)
	pdfRing(pdf, 30, layout.pageH/2, 3, style.Colors.Gold, 0.8)
	pdfRing(pdf, layout.pageW-30, layout.pageH/2, 3, style.Colors.Gold, 0.8)
	coverText(pdf, cx-180, 108, 360, 20, spec.Brand, 10.5, style.Colors.Meta, false, "center")
	coverText(pdf, cx-210, 316, 420, 110, coverTitle(spec), 24, style.Colors.Accent, true, "center")
	coverFill(pdf, style.Colors.Gold, cx-36, 448, 72, 2.5)
	coverText(pdf, cx-210, 466, 420, 48, spec.Subtitle, 11.5, style.Colors.Gold, false, "center")
	coverText(pdf, cx-210, 534, 420, 24, coverProjectName(spec), 12, style.Colors.Ink, true, "center")
	coverText(pdf, cx-210, layout.pageH-118, 420, 18, coverMetaDate(spec), 9.5, style.Colors.Meta, false, "center")
}

func paintBandCover(pdf *gopdf.GoPdf, layout pdfPageLayout, style Style, spec Spec) {
	const bandH = 272
	x := layout.marginX
	w := layout.contentW
	coverFill(pdf, style.Colors.Navy, 0, 0, layout.pageW, bandH)
	coverText(pdf, x, 104, w, 110, coverTitle(spec), 26, style.Colors.OnDark, true, "")
	coverFill(pdf, style.Colors.Gold, x, bandH-58, 84, 6)
	coverText(pdf, x, bandH-42, w, 24, spec.Subtitle, 11.5, style.Colors.OnDark, false, "")
	coverText(pdf, x, 54, w, 18, spec.Brand, 10, style.Colors.OnDark, false, "")
	coverText(pdf, x, bandH+52, w, 24, coverProjectName(spec), 12.5, style.Colors.Ink, true, "")
	coverFill(pdf, coverPanelTint(style.Colors.Meta, 0.25), x, bandH+86, w, 0.8)
	coverMetaLine(pdf, x, bandH+98, w/2-8, "日期 / DATE", coverMetaDate(spec), style.Colors.Meta, style.Colors.Meta, "")
	pdfTickRow(pdf, x+w/2+8, bandH+106, 16, 2.4, 5, 4, 1, style.Colors.Gold, "FFFFFF")
	if strings.TrimSpace(spec.FooterHint) != "" {
		coverText(pdf, x, layout.pageH-88, w, 18, spec.FooterHint, 9.5, style.Colors.Meta, false, "")
	}
	coverFill(pdf, style.Colors.Accent, 0, layout.pageH-10, layout.pageW, 10)
	// Three light dots ride the bottom strip.
	for i := 0; i < 3; i++ {
		coverFill(pdf, style.Colors.OnDark, layout.pageW/2-18+float64(i)*14, layout.pageH-7.5, 3.4, 3.4)
	}
}

func paintLedgerCover(pdf *gopdf.GoPdf, layout pdfPageLayout, style Style, spec Spec) {
	x := layout.marginX
	w := layout.contentW
	coverText(pdf, x, 56, w, 18, spec.Brand, 10, style.Colors.Meta, false, "")
	coverFill(pdf, style.Colors.Navy, x, 96, w, 6)
	coverText(pdf, x, 128, w, 120, coverTitle(spec), 25, style.Colors.Navy, true, "")
	coverFill(pdf, style.Colors.Gold, x, 262, 72, 3)
	coverText(pdf, x, 280, w, 48, spec.Subtitle, 11.5, style.Colors.Meta, false, "")
	coverMetaLine(pdf, x, 340, w/2-8, "项目 / PROJECT", coverProjectName(spec), style.Colors.Meta, style.Colors.Ink, "")
	pdfTickRow(pdf, x+w/2+8, 352, 16, 2.4, 5, 4, 1, style.Colors.Gold, "FFFFFF")
	// A quiet ruled field above the meta row nods to the ledger grid. Anchored
	// to the page bottom so B5 keeps it clear of the date line.
	gridTop := layout.pageH - 282
	for i := 0; i < 4; i++ {
		y := gridTop + float64(i)*26
		coverFill(pdf, coverPanelTint(style.Colors.Navy, 0.16), x, y, w, 0.7)
	}
	coverFill(pdf, style.Colors.Accent, x, gridTop, 120, 2)
	coverText(pdf, x, layout.pageH-92, w/2, 18, coverMetaDate(spec), 9.5, style.Colors.Meta, false, "")
	if strings.TrimSpace(spec.FooterHint) != "" {
		// Right half only: a wrapped hint must not reach the date on the left.
		coverText(pdf, x+w/2+8, layout.pageH-92, w/2-8, 18, spec.FooterHint, 9.5, style.Colors.Meta, false, "right")
	}
}

func paintSidebarCover(pdf *gopdf.GoPdf, layout pdfPageLayout, style Style, spec Spec) {
	const colW = 64.0
	x := colW + 46
	w := layout.pageW - x - layout.marginX
	coverFill(pdf, style.Colors.Navy, 0, 0, colW, layout.pageH)
	coverFill(pdf, style.Colors.Accent, colW, 0, 4, layout.pageH)
	coverFill(pdf, style.Colors.Gold, 18, 56, 28, 28)
	pdfDotGrid(pdf, 27, 104, 3.2, 10, 1, 3, blendHex(style.Colors.Gold, style.Colors.Navy, 0.35))
	coverText(pdf, x, 168, w, 20, spec.Brand, 10.5, style.Colors.Meta, false, "")
	coverText(pdf, x, 196, w, 140, coverTitle(spec), 25, style.Colors.Ink, true, "")
	coverFill(pdf, style.Colors.Accent, x, 372, 64, 3)
	coverText(pdf, x, 392, w, 60, spec.Subtitle, 11.5, style.Colors.Meta, false, "")
	coverMetaLine(pdf, x, 488, w/2-8, "项目 / PROJECT", coverProjectName(spec), style.Colors.Meta, style.Colors.Navy, "")
	coverText(pdf, x, layout.pageH-96, w/2, 18, coverMetaDate(spec), 9.5, style.Colors.Meta, false, "")
	if strings.TrimSpace(spec.FooterHint) != "" {
		// Right half only: a wrapped hint must not reach the date on the left.
		coverText(pdf, x+w/2+8, layout.pageH-96, w/2-8, 18, spec.FooterHint, 9.5, style.Colors.Meta, false, "right")
	}
}

func paintPlateCover(pdf *gopdf.GoPdf, layout pdfPageLayout, style Style, spec Spec) {
	const plateH = 92.0
	x := layout.marginX
	w := layout.contentW
	coverFill(pdf, style.Colors.Navy, 0, 0, layout.pageW, plateH)
	coverFill(pdf, style.Colors.Accent, 0, plateH, layout.pageW, 3)
	coverText(pdf, x, 34, w/2, 20, spec.Brand, 10.5, style.Colors.OnDark, false, "")
	coverText(pdf, layout.pageW-layout.marginX-160, 36, 160, 18, coverMetaDate(spec), 9.5, style.Colors.OnDark, false, "right")
	paintCornerTicks(pdf, layout, mustRGB(style.Colors.Gold))
	for i := 0; i < 4; i++ {
		d := 2.6 - float64(i)*0.5
		coverFill(pdf, style.Colors.Gold, layout.pageW-layout.marginX-20-float64(i)*8, 19-d/2, d, d)
	}
	coverText(pdf, x, 286, w, 120, coverTitle(spec), 25, style.Colors.Ink, true, "")
	coverFill(pdf, style.Colors.Gold, x, 428, 60, 3)
	coverText(pdf, x, 448, w, 48, spec.Subtitle, 11.5, style.Colors.Meta, false, "")
	coverMetaLine(pdf, x, 520, w/2-8, "项目 / PROJECT", coverProjectName(spec), style.Colors.Meta, style.Colors.Navy, "")
	if strings.TrimSpace(spec.FooterHint) != "" {
		coverText(pdf, x, layout.pageH-88, w, 18, spec.FooterHint, 9.5, style.Colors.Meta, false, "")
	}
}

// --- Decorated body panels -------------------------------------------------
//
// Code fences and blockquotes get a tinted panel drawn behind the text. The
// pager measures the block first (same box width as the final insert), so the
// panel height matches the text exactly.

// decoratedBlockKind names the panel a body block gets. "" means plain flow.
type decoratedBlockKind string

const (
	panelCode  decoratedBlockKind = "code"
	panelQuote decoratedBlockKind = "quote"
)

func decoratedBlockKindOf(block string) decoratedBlockKind {
	trimmed := strings.TrimSpace(block)
	if trimmed == "" {
		return ""
	}
	if _, ok := markdownFenceMarker(trimmed); ok {
		return panelCode
	}
	if strings.HasPrefix(trimmed, ">") {
		return panelQuote
	}
	return ""
}

// Panel insets shared by the renderer and the pager's fit allowance: the text
// sits panelPadX into the panel and panelPadR short of its right edge.
const (
	panelPadX = 14.0
	panelPadR = 10.0
)

// paintBlockPanel draws the tinted field and the left marker bar under a
// decorated block. padX is the text inset the caller must use so the text
// stays off the marker bar.
func paintBlockPanel(pdf *gopdf.GoPdf, style Style, kind decoratedBlockKind, x, y, w, h float64) (padX float64) {
	const barW = 2.6
	padX = panelPadX
	switch kind {
	case panelCode:
		coverFill(pdf, coverPanelTint(style.Colors.Ink, 0.055), x, y, w, h)
		coverFill(pdf, coverPanelTint(style.Colors.Ink, 0.16), x, y, barW, h)
		coverFill(pdf, coverPanelTint(style.Colors.Ink, 0.30), x+barW, y, w-barW, 0.7)
	case panelQuote:
		coverFill(pdf, coverPanelTint(style.Colors.Quote, 0.06), x, y, w, h)
		coverFill(pdf, style.Colors.Quote, x, y, barW, h)
		coverFill(pdf, coverPanelTint(style.Colors.Quote, 0.35), x+barW, y, w-barW, 0.7)
	}
	return padX
}

// --- Cover decoration vocabulary -------------------------------------------
//
// Small composite marks the motifs share: dot grids, tick rows, split rules,
// and label/value meta pairs. Each stays a couple of primitives so a cover can
// carry two or three without turning noisy.

// pdfDotGrid draws a cols×rows grid of tiny filled squares.
func pdfDotGrid(pdf *gopdf.GoPdf, x, y, d, gap float64, cols, rows int, color string) {
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			coverFill(pdf, color, x+float64(c)*gap, y+float64(r)*gap, d, d)
		}
	}
}

// pdfTickRow draws n short bars; the first n0 carry the full color and the
// rest fade toward bg.
func pdfTickRow(pdf *gopdf.GoPdf, x, y, w, h, gap float64, n, n0 int, color, bg string) {
	for i := 0; i < n; i++ {
		c := color
		if i >= n0 {
			c = blendHex(color, bg, 0.6)
		}
		coverFill(pdf, c, x+float64(i)*(w+gap), y, w, h)
	}
}

// pdfRing strokes one small circle outline.
func pdfRing(pdf *gopdf.GoPdf, x, y, r float64, color string, widthPt float64) {
	rgb := mustRGB(color)
	pdf.SetStrokeColor(rgb[0], rgb[1], rgb[2])
	pdf.SetLineWidth(widthPt)
	pdf.Oval(x-r, y-r, x+r, y+r)
}

// coverMetaLine draws a small caps-style label above its value — the cover
// meta rows read as tagged fields instead of bare text. Both boxes must be at
// least 14pt tall: InsertHTMLBox fits lines against the option's default
// 10pt metrics, so a shorter box silently drops the text.
func coverMetaLine(pdf *gopdf.GoPdf, x, y, w float64, label, value, labelColor, valueColor string, align string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	coverText(pdf, x, y, w, 15, label, 7.5, labelColor, false, align)
	coverText(pdf, x, y+15, w, 20, value, 10.5, valueColor, true, align)
}

// splitRulePDF is the heading rule: a long hairline with a short heavy
// segment overlapping its left end.
func splitRulePDF(pdf *gopdf.GoPdf, x, y, w float64, heavy, quiet string) {
	coverFill(pdf, quiet, x, y, w, 0.7)
	coverFill(pdf, heavy, x, y-0.8, 46, 2)
}
