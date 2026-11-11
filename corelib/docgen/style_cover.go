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
	case MotifBaseline:
		paintBaselineCover(pdf, layout, style, spec)
	case MotifRibbon:
		paintRibbonCover(pdf, layout, style, spec)
	case MotifApex:
		paintApexCover(pdf, layout, style, spec)
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
	// A quiet dot grid fills the band's quiet right side; a pale triangle and
	// a gold diamond lean against it for depth.
	pdfDotGrid(pdf, layout.pageW-124, 64, 2.2, 9, 6, 3, blendHex(style.Colors.OnDark, style.Colors.Navy, 0.55))
	pdfTri(pdf, layout.pageW-96, 150, 60, 42, blendHex(style.Colors.OnDark, style.Colors.Navy, 0.82))
	pdfDia(pdf, layout.pageW-56, 210, 14, 14, style.Colors.Gold)
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
	// A chevron trail climbs the column's lower half toward the content.
	pdfChev(pdf, 24, layout.pageH-220, 12, 8, blendHex(style.Colors.Gold, style.Colors.Navy, 0.3))
	pdfDia(pdf, colW/2, 130, 10, 10, blendHex(style.Colors.OnDark, style.Colors.Navy, 0.6))
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
	// Diamonds flank the title's gold rule; a star crowns the brand line.
	pdfDia(pdf, cx-52, 449, 8, 8, style.Colors.Gold)
	pdfDia(pdf, cx+52, 449, 8, 8, style.Colors.Gold)
	pdfStar(pdf, cx, 92, 7, style.Colors.Accent)
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
	// A marching chevron and a pale diamond give the band's right end motion.
	pdfChev(pdf, layout.pageW-72, bandH-44, 14, 10, style.Colors.Gold)
	pdfDia(pdf, layout.pageW-46, bandH-38, 12, 12, blendHex(style.Colors.OnDark, style.Colors.Navy, 0.5))
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
	// A notched triangle dips under the heavy rule like a ledger index tab.
	pdfTriDown(pdf, x, 102, 12, 8, style.Colors.Accent)
	pdfDia(pdf, x+w-8, 99, 12, 12, style.Colors.Gold)
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
	// A light triangle points up from the column's foot toward the title.
	pdfTri(pdf, 22, layout.pageH-64, 20, 14, blendHex(style.Colors.Gold, style.Colors.Navy, 0.3))
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
	// A chevron leads in from the plate's left edge; a diamond marks its end.
	pdfChev(pdf, x, plateH/2-5, 10, 10, style.Colors.Gold)
	pdfDia(pdf, layout.pageW-layout.marginX-48, plateH/2, 10, 10, blendHex(style.Colors.OnDark, style.Colors.Navy, 0.5))
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

// --- Polygon marks ----------------------------------------------------------
//
// GoPDF2 takes points in the same upper-left-origin space coverFill uses, so
// these compose freely with the rect/oval vocabulary. Each helper is a
// three-to-six point path — cheap to paint on every page.

// pdfPoly fills a closed polygon with a hex color.
func pdfPoly(pdf *gopdf.GoPdf, pts []gopdf.Point, hex string) {
	if pdf == nil || len(pts) < 3 {
		return
	}
	rgb := mustRGB(hex)
	pdf.SetFillColor(rgb[0], rgb[1], rgb[2])
	pdf.Polygon(pts, "F")
}

// pdfDia fills a diamond centered at (cx, cy).
func pdfDia(pdf *gopdf.GoPdf, cx, cy, w, h float64, hex string) {
	pdfPoly(pdf, []gopdf.Point{
		{X: cx, Y: cy - h/2}, {X: cx + w/2, Y: cy},
		{X: cx, Y: cy + h/2}, {X: cx - w/2, Y: cy},
	}, hex)
}

// pdfTri fills an upright triangle in the box (x, y, w, h).
func pdfTri(pdf *gopdf.GoPdf, x, y, w, h float64, hex string) {
	pdfPoly(pdf, []gopdf.Point{
		{X: x + w/2, Y: y}, {X: x + w, Y: y + h}, {X: x, Y: y + h},
	}, hex)
}

// pdfTriDown fills a downward triangle.
func pdfTriDown(pdf *gopdf.GoPdf, x, y, w, h float64, hex string) {
	pdfPoly(pdf, []gopdf.Point{
		{X: x, Y: y}, {X: x + w, Y: y}, {X: x + w/2, Y: y + h},
	}, hex)
}

// pdfPara fills a parallelogram skewed right by slant.
func pdfPara(pdf *gopdf.GoPdf, x, y, w, h, slant float64, hex string) {
	pdfPoly(pdf, []gopdf.Point{
		{X: x + slant, Y: y}, {X: x + w, Y: y},
		{X: x + w - slant, Y: y + h}, {X: x, Y: y + h},
	}, hex)
}

// pdfChev fills a right-pointing chevron in the box (x, y, w, h).
func pdfChev(pdf *gopdf.GoPdf, x, y, w, h float64, hex string) {
	notch := w * 0.42
	pdfPoly(pdf, []gopdf.Point{
		{X: x, Y: y}, {X: x + w - notch, Y: y}, {X: x + w, Y: y + h/2},
		{X: x + w - notch, Y: y + h}, {X: x, Y: y + h}, {X: x + notch, Y: y + h/2},
	}, hex)
}

// pdfChevRow fills n chevrons marching right, each paler than the last.
func pdfChevRow(pdf *gopdf.GoPdf, x, y, w, h, gap float64, n int, color, bg string) {
	for i := 0; i < n; i++ {
		pdfChev(pdf, x+float64(i)*(w+gap), y, w, h, blendHex(color, bg, float64(i)*0.25))
	}
}

// pdfStar fills a four-point sparkle centered at (cx, cy).
func pdfStar(pdf *gopdf.GoPdf, cx, cy, r float64, hex string) {
	const waist = 0.22
	w := r * waist
	pdfPoly(pdf, []gopdf.Point{
		{X: cx, Y: cy - r}, {X: cx + w, Y: cy - w}, {X: cx + r, Y: cy},
		{X: cx + w, Y: cy + w}, {X: cx, Y: cy + r}, {X: cx - w, Y: cy + w},
		{X: cx - r, Y: cy}, {X: cx - w, Y: cy - w},
	}, hex)
}

// --- Baseline: swiss grid ---------------------------------------------------
// A spare field: one square marker, quiet ruled lines, oversized title. The
// geometry is the rhythm, not the ornament.

func paintBaselineCover(pdf *gopdf.GoPdf, layout pdfPageLayout, style Style, spec Spec) {
	x := layout.marginX
	w := layout.contentW
	coverFill(pdf, style.Colors.Navy, x, 60, 10, 10)
	coverText(pdf, x+20, 58, w-20, 18, spec.Brand, 10, style.Colors.Meta, false, "")
	// A ghost diamond balances the square.
	pdfDia(pdf, layout.pageW-layout.marginX-30, 66, 16, 16, coverPanelTint(style.Colors.Accent, 0.5))
	coverText(pdf, x, 150, w, 130, coverTitle(spec), 26, style.Colors.Ink, true, "")
	splitRulePDF(pdf, x, 300, w*0.55, style.Colors.Accent, coverPanelTint(style.Colors.Rule, 0.45))
	coverText(pdf, x, 320, w, 54, spec.Subtitle, 11.5, style.Colors.Meta, false, "")
	// The baseline grid: quiet full-width rules marching down the lower page.
	gridTop := layout.pageH - 252
	for i := 0; i < 5; i++ {
		coverFill(pdf, coverPanelTint(style.Colors.Rule, 0.3), x, gridTop+float64(i)*30, w, 0.6)
	}
	coverFill(pdf, style.Colors.Navy, x, gridTop, 90, 2)
	coverMetaLine(pdf, x, gridTop+158, w/2-8, "项目 / PROJECT", coverProjectName(spec), style.Colors.Meta, style.Colors.Ink, "")
	coverText(pdf, x+w/2+8, gridTop+173, w/2-8, 20, coverMetaDate(spec), 10.5, style.Colors.Meta, false, "")
	if strings.TrimSpace(spec.FooterHint) != "" {
		coverText(pdf, x, layout.pageH-56, w, 18, spec.FooterHint, 9.5, style.Colors.Meta, false, "")
	}
}

// --- Ribbon: hanging band ----------------------------------------------------
// The title rides a full-width ribbon with a folded tail; everything else
// stays quiet so the fold reads clearly.

func paintRibbonCover(pdf *gopdf.GoPdf, layout pdfPageLayout, style Style, spec Spec) {
	const bandY, bandH = 190.0, 118.0
	x := layout.marginX
	w := layout.contentW
	// The fold: a darker parallelogram tail tucks behind the ribbon's right
	// end, then the ribbon crosses the page.
	pdfPara(pdf, layout.pageW-88, bandY-14, 42, bandH+28, 12, blendHex(style.Colors.Navy, "000000", 0.25))
	coverFill(pdf, style.Colors.Navy, 0, bandY, layout.pageW, bandH)
	coverFill(pdf, style.Colors.Gold, 0, bandY+bandH, layout.pageW, 2.4)
	pdfChev(pdf, layout.pageW-46, bandY+bandH/2-9, 16, 18, blendHex(style.Colors.Gold, style.Colors.Navy, 0.15))
	coverText(pdf, x, 60, w, 18, spec.Brand, 10.5, style.Colors.Meta, false, "")
	pdfTickRow(pdf, x, 88, 18, 2.6, 6, 4, 2, style.Colors.Accent, "FFFFFF")
	coverText(pdf, x, bandY+28, w-96, 74, coverTitle(spec), 25, style.Colors.OnDark, true, "")
	coverFill(pdf, style.Colors.Gold, x, bandY+bandH-22, 60, 2.4)
	coverText(pdf, x, bandY+bandH+56, w, 56, spec.Subtitle, 12, style.Colors.Meta, false, "")
	coverMetaLine(pdf, x, bandY+bandH+128, w/2-8, "项目 / PROJECT", coverProjectName(spec), style.Colors.Meta, style.Colors.Ink, "")
	coverText(pdf, x+w/2+8, bandY+bandH+143, w/2-8, 20, coverMetaDate(spec), 10.5, style.Colors.Meta, false, "")
	if strings.TrimSpace(spec.FooterHint) != "" {
		coverText(pdf, x, layout.pageH-64, w, 18, spec.FooterHint, 9.5, style.Colors.Meta, false, "")
	}
	// A footer strip echoes the ribbon.
	coverFill(pdf, style.Colors.Navy, 0, layout.pageH-12, layout.pageW, 12)
	pdfDia(pdf, layout.pageW/2, layout.pageH-6, 8, 8, style.Colors.Gold)
}

// --- Apex: layered summits --------------------------------------------------
// Stacked triangles climb the right edge — motion and altitude without
// pictures. The title sits low-left against them.

func paintApexCover(pdf *gopdf.GoPdf, layout pdfPageLayout, style Style, spec Spec) {
	x := layout.marginX
	w := layout.contentW
	coverFill(pdf, style.Colors.Accent, 0, 0, layout.pageW, 4)
	coverFill(pdf, style.Colors.Gold, 0, 4, 72, 2)
	// The summit stack: three peaks, back pale to front navy.
	pdfTri(pdf, layout.pageW-210, 60, 130, 88, coverPanelTint(style.Colors.Navy, 0.14))
	pdfTri(pdf, layout.pageW-140, 60, 108, 74, coverPanelTint(style.Colors.Navy, 0.35))
	pdfTri(pdf, layout.pageW-90, 60, 90, 62, style.Colors.Navy)
	pdfDia(pdf, layout.pageW-45, 138, 10, 10, style.Colors.Gold)
	coverText(pdf, x, 64, w-170, 18, spec.Brand, 10, style.Colors.Meta, false, "")
	pdfTickRow(pdf, x, 92, 16, 2.4, 5, 4, 2, style.Colors.Accent, "FFFFFF")
	coverText(pdf, x, 240, w, 130, coverTitle(spec), 26, style.Colors.Navy, true, "")
	// A small climbing chevron trail under the title carries the peak motif.
	pdfChevRow(pdf, x, 392, 22, 12, 7, 3, style.Colors.Accent, "FFFFFF")
	coverText(pdf, x, 424, w, 56, spec.Subtitle, 11.5, style.Colors.Meta, false, "")
	splitRulePDF(pdf, x, 498, w*0.5, style.Colors.Gold, coverPanelTint(style.Colors.Rule, 0.4))
	coverMetaLine(pdf, x, 522, w/2-8, "项目 / PROJECT", coverProjectName(spec), style.Colors.Meta, style.Colors.Ink, "")
	coverText(pdf, x+w/2+8, 537, w/2-8, 20, coverMetaDate(spec), 10.5, style.Colors.Meta, false, "")
	if strings.TrimSpace(spec.FooterHint) != "" {
		coverText(pdf, x, layout.pageH-64, w, 18, spec.FooterHint, 9.5, style.Colors.Meta, false, "")
	}
}
