package pptx

import (
	"fmt"
	"strings"

	ppt "github.com/Vantagics/GoPPT"
)

// motif is the layout system a style paints with. The palette supplies color;
// the motif decides the cover, the content header, the cards, and the section
// page. A custom style may name one of these. An empty motif becomes report,
// or ink when the cover is dark.
type motif string

const (
	motifReport     motif = "report"     // 清爽专业: left rule, evidence cards
	motifSplit      motif = "split"      // 创意杂志: asymmetric block, one accent
	motifInk        motif = "ink"        // 电子墨水: paper, metadata, huge type
	motifDashboard  motif = "dashboard"  // 仪表盘: header and metric cards
	motifPoster     motif = "poster"     // 复古扁平: cream, color band, outlined cards
	motifSketch     motif = "sketch"     // 手绘技术: graphite corners, open points
	motifBoard      motif = "board"      // 白板: marker rule, sticky notes
	motifCraft      motif = "craft"      // 温暖手工: paper notes
	motifDefense    motif = "defense"    // 科研答辩: blue structure, red emphasis
	motifConsulting motif = "consulting" // 麦肯锡: pale field, hairlines, no navy block
	motifOfficial   motif = "official"   // 党政红: ivory, red panel, matte gold
	motifLesson     motif = "lesson"     // 教学课件: masthead band
)

type bodyKind int

const (
	bodyFilled bodyKind = iota
	bodyOpen
	bodyOutline
	bodyNotes
	bodyRail
)

func normalizeMotif(raw string, coverDark bool) (motif, error) {
	switch motif(strings.ToLower(strings.TrimSpace(raw))) {
	case "":
		if coverDark {
			return motifInk, nil
		}
		return motifReport, nil
	case motifReport, motifSplit, motifInk, motifDashboard, motifPoster, motifSketch,
		motifBoard, motifCraft, motifDefense, motifConsulting, motifOfficial, motifLesson:
		return motif(strings.ToLower(strings.TrimSpace(raw))), nil
	default:
		return "", fmt.Errorf("ppt_style_motif_invalid")
	}
}

func motifBody(m motif) bodyKind {
	switch m {
	case motifInk, motifConsulting, motifSketch:
		return bodyOpen
	case motifPoster:
		return bodyOutline
	case motifBoard, motifCraft:
		return bodyNotes
	case motifSplit:
		return bodyRail
	default:
		return bodyFilled
	}
}

func paintMotifCover(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total int) {
	switch theme.motif {
	case motifSplit:
		paintSplitCover(slide, theme, title, subtitle, kicker, page, total)
	case motifInk:
		paintInkCover(slide, theme, title, subtitle, kicker, page, total)
	case motifDashboard:
		paintDashboardCover(slide, theme, title, subtitle, kicker, page, total)
	case motifPoster:
		paintPosterCover(slide, theme, title, subtitle, kicker, page, total)
	case motifSketch:
		paintSketchCover(slide, theme, title, subtitle, kicker, page, total)
	case motifBoard:
		paintBoardCover(slide, theme, title, subtitle, kicker, page, total)
	case motifCraft:
		paintCraftCover(slide, theme, title, subtitle, kicker, page, total)
	case motifDefense:
		paintDefenseCover(slide, theme, title, subtitle, kicker, page, total)
	case motifConsulting:
		paintConsultingMotifCover(slide, theme, title, subtitle, kicker, page, total)
	case motifOfficial:
		paintOfficialCover(slide, theme, title, subtitle, kicker, page, total)
	case motifLesson:
		paintLessonCover(slide, theme, title, subtitle, kicker, page, total)
	default:
		paintReportCover(slide, theme, title, subtitle, kicker, page, total)
	}
}

func paintMotifChrome(slide *ppt.Slide, theme deckTheme, kicker, title, footer string, page, total int) contentBox {
	paintSolidBackground(slide, theme.paper)
	switch theme.motif {
	case motifSplit:
		return paintSplitChrome(slide, theme, kicker, title, footer, page, total)
	case motifInk:
		return paintInkChrome(slide, theme, kicker, title, footer, page, total)
	case motifDashboard:
		return paintDashboardChrome(slide, theme, kicker, title, footer, page, total)
	case motifPoster:
		return paintContentTitle(slide, theme, kicker, title, footer, page, total, 0.62, theme.ink, false)
	case motifSketch:
		return paintContentTitle(slide, theme, kicker, title, footer, page, total, 0.7, theme.ink, false)
	case motifBoard:
		return paintBoardChrome(slide, theme, kicker, title, footer, page, total)
	case motifCraft:
		rect(slide, 0, 0, deckSlideWidth, emuIn(0.1), theme.accent)
		return paintContentTitle(slide, theme, kicker, title, footer, page, total, 0.62, theme.navy2, false)
	case motifDefense:
		return paintDefenseChrome(slide, theme, kicker, title, footer, page, total)
	case motifConsulting:
		return paintConsultingChrome(slide, theme, kicker, title, footer, page, total)
	case motifOfficial:
		rect(slide, 0, 0, deckSlideWidth, emuIn(0.12), theme.accent)
		rect(slide, 0, emuIn(0.12), deckSlideWidth, emuIn(0.03), theme.gold)
		return paintContentTitle(slide, theme, kicker, title, footer, page, total, 0.62, theme.accent, false)
	case motifLesson:
		rect(slide, 0, 0, deckSlideWidth, emuIn(0.16), theme.navy)
		rect(slide, 0, emuIn(0.16), deckSlideWidth, emuIn(0.04), theme.accent)
		return paintContentTitle(slide, theme, kicker, title, footer, page, total, 0.62, theme.navy, false)
	default:
		rect(slide, 0, 0, emuIn(0.14), deckSlideHeight, theme.accent)
		return paintContentTitle(slide, theme, kicker, title, footer, page, total, 0.55, theme.accent, true)
	}
}

func paintMotifSection(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total, sectionNo int) {
	switch theme.motif {
	case motifConsulting:
		paintConsultingSection(slide, theme, title, subtitle, kicker, page, total, sectionNo)
	case motifOfficial:
		paintFieldSection(slide, theme.accent, theme.white, theme.gold, title, subtitle, kicker, page, total)
	case motifDefense:
		paintFieldSection(slide, theme.navy, theme.white, theme.accent, title, subtitle, kicker, page, total)
	case motifLesson:
		paintFieldSection(slide, theme.navy, theme.white, theme.accent, title, subtitle, kicker, page, total)
	case motifSplit:
		paintSplitSection(slide, theme, title, subtitle, kicker, page, total)
	default:
		paintSolidBackground(slide, theme.paper)
		if kicker != "" {
			deckText(slide, emuIn(0.75), emuIn(2.15), emuIn(11.5), emuIn(0.3), kicker, 14, true, textOn(theme.accent, theme.paper, theme), ppt.HorizontalLeft)
		}
		deckText(slide, emuIn(0.75), emuIn(2.55), emuIn(11.6), emuIn(1.6), title, coverTitlePointSize(title, 40, 11.6, 1.6), true, textOn(theme.navy, theme.paper, theme), ppt.HorizontalLeft)
		rect(slide, emuIn(0.75), emuIn(4.35), emuIn(1.8), emuIn(0.03), theme.accent)
		deckText(slide, emuIn(0.75), emuIn(4.55), emuIn(11), emuIn(0.8), subtitle, 18, false, textOn(theme.slate, theme.paper, theme), ppt.HorizontalLeft)
		paintPageMark(slide, page, total, textOn(theme.mute, theme.paper, theme))
	}
}

func paintReportCover(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total int) {
	paintSolidBackground(slide, theme.paper)
	rect(slide, 0, 0, emuIn(0.22), deckSlideHeight, theme.accent)
	rect(slide, 0, 0, emuIn(0.22), emuIn(0.22), theme.navy2)
	titleY := 0.72
	if kicker != "" {
		deckText(slide, emuIn(0.7), emuIn(0.42), emuIn(11), emuIn(0.26), kicker, 13, true, textOn(theme.navy2, theme.paper, theme), ppt.HorizontalLeft)
		titleY = 0.78
	}
	_, subY := placeCoverTitle(slide, 0.7, titleY, 11.6, 1.35, title, 40, textOn(theme.ink, theme.paper, theme), ppt.HorizontalLeft)
	deckText(slide, emuIn(0.7), emuIn(subY), emuIn(11.6), emuIn(0.55), subtitle, 16, false, textOn(theme.slate, theme.paper, theme), ppt.HorizontalLeft)
	// Three evidence cards are the report motif: pale field, color cap, quiet rule.
	stripes := []ppt.Color{theme.accent, theme.navy2, theme.gold}
	for i, stripe := range stripes {
		x := 0.7 + float64(i)*4.05
		y := 3.85
		rect(slide, emuIn(x), emuIn(y), emuIn(3.85), emuIn(2.85), theme.card)
		rect(slide, emuIn(x), emuIn(y), emuIn(3.85), emuIn(0.1), stripe)
		rect(slide, emuIn(x+0.28), emuIn(y+0.55), emuIn(1.7), emuIn(0.08), blend(stripe, theme.card, 0.35))
		rect(slide, emuIn(x+0.28), emuIn(y+1.05), emuIn(2.8), emuIn(0.045), blend(theme.slate, theme.card, 0.25))
		rect(slide, emuIn(x+0.28), emuIn(y+1.35), emuIn(2.2), emuIn(0.045), blend(theme.slate, theme.card, 0.18))
	}
	paintPageMark(slide, page, total, textOn(theme.mute, theme.paper, theme))
}

func paintSplitCover(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total int) {
	const split = 6.15
	rect(slide, 0, 0, emuIn(split), deckSlideHeight, theme.navy)
	rect(slide, emuIn(split), 0, deckSlideWidth-emuIn(split), deckSlideHeight, theme.paper)
	rect(slide, emuIn(split), 0, emuIn(0.14), deckSlideHeight, theme.accent)
	if kicker != "" {
		deckText(slide, emuIn(0.42), emuIn(0.48), emuIn(5.2), emuIn(0.28), kicker, 13, true, textOn(theme.gold, theme.navy, theme), ppt.HorizontalLeft)
	}
	placeCoverTitle(slide, 0.42, 1.15, 5.3, 2.8, title, 48, textOn(theme.white, theme.navy, theme), ppt.HorizontalLeft)
	// Three unequal rules stand in for the magazine's feature teasers.
	for i, w := range []float64{2.4, 1.35, 1.9} {
		rect(slide, emuIn(0.42), emuIn(5.15+float64(i)*0.42), emuIn(w), emuIn(0.045), theme.accent)
	}
	// Right side is the collage crop: a charcoal plate with one accent block.
	rect(slide, emuIn(6.7), emuIn(0.42), emuIn(6.15), emuIn(3.55), theme.navy2)
	rect(slide, emuIn(10.15), emuIn(2.55), emuIn(2.7), emuIn(2.35), theme.accent)
	deckText(slide, emuIn(6.7), emuIn(5.15), emuIn(6.1), emuIn(1.35), subtitle, 18, false, textOn(theme.ink, theme.paper, theme), ppt.HorizontalLeft)
	deckText(slide, emuIn(6.7), emuIn(6.85), emuIn(4), emuIn(0.3), pageMark(page, total), 12, false, textOn(theme.mute, theme.paper, theme), ppt.HorizontalLeft)
}

func paintInkCover(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total int) {
	paintSolidBackground(slide, theme.paper)
	rect(slide, emuIn(12.05), 0, deckSlideWidth-emuIn(12.05), deckSlideHeight, theme.navy)
	rect(slide, emuIn(12.28), emuIn(0.55), emuIn(0.55), emuIn(0.55), theme.accent)
	if kicker != "" {
		deckText(slide, emuIn(0.7), emuIn(0.38), emuIn(6), emuIn(0.26), kicker, 12, true, textOn(theme.navy, theme.paper, theme), ppt.HorizontalLeft)
	}
	deckText(slide, emuIn(7.2), emuIn(0.38), emuIn(4.4), emuIn(0.26), pageMark(page, total), 12, false, textOn(theme.mute, theme.paper, theme), ppt.HorizontalRight)
	rect(slide, emuIn(0.7), emuIn(0.78), emuIn(10.9), emuIn(0.012), theme.ink)
	placeCoverTitle(slide, 0.7, 1.15, 10.8, 3.15, title, 54, theme.ink, ppt.HorizontalLeft)
	rect(slide, emuIn(0.7), emuIn(5.15), emuIn(1.6), emuIn(0.045), theme.accent)
	deckText(slide, emuIn(0.7), emuIn(5.4), emuIn(10.8), emuIn(1.15), subtitle, 18, false, textOn(theme.slate, theme.paper, theme), ppt.HorizontalLeft)
}

func paintDashboardCover(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total int) {
	paintSolidBackground(slide, theme.paper)
	rect(slide, 0, 0, deckSlideWidth, emuIn(1.42), theme.white)
	rect(slide, emuIn(0.42), emuIn(0.48), emuIn(0.16), emuIn(0.16), theme.accent)
	titleY := 0.42
	if kicker != "" {
		deckText(slide, emuIn(0.72), emuIn(0.18), emuIn(8), emuIn(0.22), kicker, 12, true, textOn(theme.accent, theme.white, theme), ppt.HorizontalLeft)
		titleY = 0.46
	}
	deckText(slide, emuIn(0.72), emuIn(titleY), emuIn(10), emuIn(0.78), title, coverTitlePointSize(title, 28, 10, 0.78), true, textOn(theme.ink, theme.white, theme), ppt.HorizontalLeft)
	caps := []ppt.Color{theme.accent, theme.gold, theme.navy2, theme.navy}
	bars := []float64{1.15, 0.72, 1.45, 0.95}
	for i, cap := range caps {
		x := 0.42 + float64(i)*3.22
		rect(slide, emuIn(x), emuIn(1.64), emuIn(3.06), emuIn(1.72), theme.card)
		rect(slide, emuIn(x), emuIn(1.64), emuIn(3.06), emuIn(0.08), cap)
		rect(slide, emuIn(x+0.22), emuIn(2.15), emuIn(bars[i]), emuIn(0.28), cap)
		for s := 0; s < 5; s++ {
			h := 0.16 + float64((s*3+i)%4)*0.08
			rect(slide, emuIn(x+0.22+float64(s)*0.42), emuIn(2.95-h), emuIn(0.28), emuIn(h), blend(cap, theme.card, 0.25))
		}
	}
	rect(slide, emuIn(0.42), emuIn(3.58), emuIn(8.15), emuIn(3.45), theme.card)
	heights := []float64{0.7, 1.15, 0.85, 1.9, 1.25, 2.15, 1.05}
	colors := []ppt.Color{theme.accent, theme.navy, theme.gold, theme.accent, theme.navy2, theme.navy, theme.gold}
	for i, h := range heights {
		rect(slide, emuIn(0.75+float64(i)*1.05), emuIn(6.45-h), emuIn(0.62), emuIn(h), colors[i])
	}
	rect(slide, emuIn(8.75), emuIn(3.58), emuIn(4.15), emuIn(3.45), theme.card)
	rect(slide, emuIn(8.75), emuIn(3.58), emuIn(0.1), emuIn(3.45), theme.accent)
	deckText(slide, emuIn(9.1), emuIn(3.9), emuIn(3.55), emuIn(2.7), subtitle, 16, false, textOn(theme.slate, theme.card, theme), ppt.HorizontalLeft)
}

func paintPosterCover(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total int) {
	paintSolidBackground(slide, theme.paper)
	// Flat scene band: sky, sun, hills, and simple buildings. Outlines stay one weight.
	rect(slide, 0, 0, deckSlideWidth, emuIn(2.55), blend(theme.gold, theme.paper, 0.55))
	oval(slide, emuIn(11.35), emuIn(0.28), emuIn(0.95), emuIn(0.95), theme.gold)
	rect(slide, 0, emuIn(1.85), deckSlideWidth, emuIn(0.7), blend(theme.navy2, theme.paper, 0.45))
	buildings := []struct{ x, w, h float64 }{{0.55, 1.15, 1.35}, {1.85, 0.85, 0.9}, {2.85, 1.35, 1.55}, {4.4, 0.7, 0.75}}
	fills := []ppt.Color{theme.accent, theme.navy, theme.gold, theme.navy2}
	for i, b := range buildings {
		rect(slide, emuIn(b.x), emuIn(2.55-b.h), emuIn(b.w), emuIn(b.h), fills[i])
		strokeFrame(slide, emuIn(b.x), emuIn(2.55-b.h), emuIn(b.w), emuIn(b.h), emuIn(0.025), theme.ink)
	}
	titleY := 3.05
	if kicker != "" {
		deckText(slide, emuIn(0.55), emuIn(2.68), emuIn(8), emuIn(0.24), kicker, 13, true, textOn(theme.accent, theme.paper, theme), ppt.HorizontalLeft)
		titleY = 3.02
	}
	_, subY := placeCoverTitle(slide, 0.55, titleY, 12.2, 1.05, title, 36, theme.ink, ppt.HorizontalLeft)
	deckText(slide, emuIn(0.55), emuIn(subY), emuIn(12.2), emuIn(0.5), subtitle, 16, false, textOn(theme.slate, theme.paper, theme), ppt.HorizontalLeft)
	chips := []ppt.Color{theme.accent, theme.gold, theme.navy2}
	for i, chip := range chips {
		x := 0.55 + float64(i)*4.2
		y := 5.15
		rect(slide, emuIn(x), emuIn(y), emuIn(3.95), emuIn(1.85), theme.card)
		strokeFrame(slide, emuIn(x), emuIn(y), emuIn(3.95), emuIn(1.85), emuIn(0.028), theme.ink)
		rect(slide, emuIn(x+0.22), emuIn(y+0.28), emuIn(0.42), emuIn(0.42), chip)
		strokeFrame(slide, emuIn(x+0.22), emuIn(y+0.28), emuIn(0.42), emuIn(0.42), emuIn(0.02), theme.ink)
	}
}

func paintSketchCover(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total int) {
	paintSolidBackground(slide, theme.paper)
	cornerMarks(slide, theme.ink)
	titleY := 0.55
	if kicker != "" {
		deckText(slide, emuIn(0.7), emuIn(0.42), emuIn(10), emuIn(0.26), kicker, 13, true, textOn(theme.ink, theme.paper, theme), ppt.HorizontalLeft)
		titleY = 0.72
	}
	_, subY := placeCoverTitle(slide, 0.7, titleY, 11.5, 1.05, title, 36, theme.ink, ppt.HorizontalLeft)
	deckText(slide, emuIn(0.7), emuIn(subY), emuIn(11.5), emuIn(0.45), subtitle, 15, false, textOn(theme.slate, theme.paper, theme), ppt.HorizontalLeft)
	fills := []ppt.Color{theme.accent, theme.gold, blend(theme.navy2, theme.white, 0.35)}
	for i, fill := range fills {
		x := 0.7 + float64(i)*4.15
		y := 3.35
		rect(slide, emuIn(x), emuIn(y), emuIn(3.35), emuIn(2.85), theme.card)
		strokeFrame(slide, emuIn(x), emuIn(y), emuIn(3.35), emuIn(2.85), emuIn(0.02), theme.ink)
		oval(slide, emuIn(x+0.28), emuIn(y+0.28), emuIn(0.38), emuIn(0.38), fill)
		rect(slide, emuIn(x+0.85), emuIn(y+0.38), emuIn(1.8), emuIn(0.06), theme.ink)
		rect(slide, emuIn(x+0.28), emuIn(y+1.15), emuIn(2.6), emuIn(0.045), blend(theme.ink, theme.paper, 0.35))
		rect(slide, emuIn(x+0.28), emuIn(y+1.55), emuIn(2.1), emuIn(0.045), blend(theme.ink, theme.paper, 0.25))
		if i < 2 {
			rect(slide, emuIn(x+3.35), emuIn(y+1.3), emuIn(0.8), emuIn(0.045), theme.ink)
		}
	}
}

func paintBoardCover(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total int) {
	paintSolidBackground(slide, theme.paper)
	titleY := 0.48
	if kicker != "" {
		deckText(slide, emuIn(0.55), emuIn(0.2), emuIn(8), emuIn(0.24), kicker, 14, true, textOn(theme.navy2, theme.paper, theme), ppt.HorizontalLeft)
		titleY = 0.52
	}
	_, subY := placeCoverTitle(slide, 0.55, titleY, 12, 0.9, title, 36, theme.ink, ppt.HorizontalLeft)
	rect(slide, emuIn(0.55), emuIn(subY), emuIn(2.4), emuIn(0.09), theme.accent)
	deckText(slide, emuIn(0.55), emuIn(subY+0.2), emuIn(12.2), emuIn(0.4), subtitle, 15, false, textOn(theme.slate, theme.paper, theme), ppt.HorizontalLeft)
	strokes := []ppt.Color{theme.accent, theme.navy2, theme.gold}
	for i, stroke := range strokes {
		x := 0.55 + float64(i)*4.15
		strokeFrame(slide, emuIn(x), emuIn(2.55), emuIn(3.85), emuIn(4.15), emuIn(0.045), stroke)
		if i < 2 {
			rect(slide, emuIn(x+3.85), emuIn(4.45), emuIn(0.3), emuIn(0.08), theme.ink)
		}
	}
}

func paintCraftCover(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total int) {
	paintSolidBackground(slide, theme.paper)
	roundRect(slide, emuIn(0.5), emuIn(0.38), emuIn(8.6), emuIn(1.85), theme.accent)
	titleY := 0.78
	if kicker != "" {
		deckText(slide, emuIn(0.85), emuIn(0.48), emuIn(7.8), emuIn(0.24), kicker, 13, true, textOn(theme.ink, theme.accent, theme), ppt.HorizontalLeft)
		titleY = 0.82
	}
	placeCoverTitle(slide, 0.85, titleY, 7.8, 1.15, title, 34, textOn(theme.ink, theme.accent, theme), ppt.HorizontalLeft)
	notes := []ppt.Color{theme.card, blend(theme.gold, theme.white, 0.25), theme.white}
	offsets := []float64{0, 0.18, 0.06}
	for i, fill := range notes {
		x := 0.55 + float64(i)*4.2
		y := 2.6 + offsets[i]
		roundRect(slide, emuIn(x), emuIn(y), emuIn(3.9), emuIn(3.15), fill)
		rect(slide, emuIn(x+1.45), emuIn(y-0.08), emuIn(0.95), emuIn(0.28), blend(theme.gold, theme.white, 0.15))
	}
	deckText(slide, emuIn(0.7), emuIn(6.35), emuIn(12), emuIn(0.7), subtitle, 16, false, textOn(theme.ink, theme.paper, theme), ppt.HorizontalLeft)
}

func paintDefenseCover(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total int) {
	paintSolidBackground(slide, theme.paper)
	rect(slide, 0, 0, deckSlideWidth, emuIn(1.85), theme.navy)
	if kicker != "" {
		deckText(slide, emuIn(0.55), emuIn(0.18), emuIn(10), emuIn(0.26), kicker, 14, true, textOn(theme.white, theme.navy, theme), ppt.HorizontalLeft)
	}
	deckText(slide, emuIn(0.55), emuIn(0.5), emuIn(12.2), emuIn(1.1), title, coverTitlePointSize(title, 36, 12.2, 1.1), true, textOn(theme.white, theme.navy, theme), ppt.HorizontalLeft)
	rect(slide, emuIn(0.55), emuIn(2.05), emuIn(2.2), emuIn(0.06), theme.accent)
	rect(slide, emuIn(2.9), emuIn(2.05), emuIn(9.8), emuIn(0.02), theme.navy)
	deckText(slide, emuIn(0.55), emuIn(2.28), emuIn(12.2), emuIn(0.55), subtitle, 16, false, textOn(theme.ink, theme.paper, theme), ppt.HorizontalLeft)
	for i := 0; i < 3; i++ {
		x := 0.55 + float64(i)*4.2
		cap := theme.navy
		if i == 2 {
			cap = theme.accent
		}
		rect(slide, emuIn(x), emuIn(3.15), emuIn(3.95), emuIn(3.7), theme.card)
		rect(slide, emuIn(x), emuIn(3.15), emuIn(3.95), emuIn(0.1), cap)
		rect(slide, emuIn(x+0.28), emuIn(3.7), emuIn(2.2), emuIn(0.06), blend(cap, theme.card, 0.4))
		rect(slide, emuIn(x+0.28), emuIn(4.15), emuIn(3.2), emuIn(0.04), blend(theme.slate, theme.card, 0.2))
		rect(slide, emuIn(x+0.28), emuIn(4.5), emuIn(2.6), emuIn(0.04), blend(theme.slate, theme.card, 0.15))
	}
}

func paintConsultingMotifCover(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total int) {
	paintSolidBackground(slide, theme.paper)
	if kicker != "" {
		deckText(slide, emuIn(0.6), emuIn(0.38), emuIn(6), emuIn(0.26), kicker, 12, true, textOn(theme.accent, theme.paper, theme), ppt.HorizontalLeft)
	}
	deckText(slide, emuIn(8.4), emuIn(0.38), emuIn(4.3), emuIn(0.26), pageMark(page, total), 12, false, textOn(theme.mute, theme.paper, theme), ppt.HorizontalRight)
	placeCoverTitle(slide, 0.6, 0.85, 6.6, 2.4, title, 44, textOn(theme.ink, theme.paper, theme), ppt.HorizontalLeft)
	deckText(slide, emuIn(0.6), emuIn(3.5), emuIn(6.4), emuIn(1.3), subtitle, 16, false, textOn(theme.slate, theme.paper, theme), ppt.HorizontalLeft)
	// A 2x2 hairline matrix is the consulting cover's one structure. One cell is marked.
	const ox, oy, cell = 7.55, 1.55, 2.45
	hair := blend(theme.slate, theme.paper, 0.45)
	for row := 0; row < 2; row++ {
		for col := 0; col < 2; col++ {
			strokeFrame(slide, emuIn(ox+float64(col)*cell), emuIn(oy+float64(row)*cell), emuIn(cell), emuIn(cell), emuIn(0.015), hair)
		}
	}
	rect(slide, emuIn(ox+cell+0.85), emuIn(oy+0.85), emuIn(0.7), emuIn(0.7), theme.accent)
	rect(slide, emuIn(ox), emuIn(oy+cell-0.008), emuIn(cell*2), emuIn(0.016), theme.ink)
	rect(slide, emuIn(ox+cell-0.008), emuIn(oy), emuIn(0.016), emuIn(cell*2), theme.ink)
	rect(slide, emuIn(0.6), emuIn(6.55), deckSlideWidth-emuIn(1.2), emuIn(0.012), hair)
}

func paintOfficialCover(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total int) {
	const panel = 7.35
	paintSolidBackground(slide, theme.paper)
	rect(slide, 0, 0, emuIn(panel), deckSlideHeight, theme.accent)
	rect(slide, emuIn(panel), 0, emuIn(0.06), deckSlideHeight, theme.gold)
	strokeFrame(slide, emuIn(0.38), emuIn(0.38), emuIn(panel-0.76), deckSlideHeight-emuIn(0.76), emuIn(0.02), theme.gold)
	strokeFrame(slide, emuIn(0.52), emuIn(0.52), emuIn(panel-1.04), deckSlideHeight-emuIn(1.04), emuIn(0.012), theme.gold)
	if kicker != "" {
		deckText(slide, emuIn(0.85), emuIn(1.55), emuIn(5.6), emuIn(0.3), kicker, 14, true, textOn(theme.gold, theme.accent, theme), ppt.HorizontalLeft)
	}
	placeCoverTitle(slide, 0.85, 2.05, 5.6, 2.4, title, 36, textOn(theme.white, theme.accent, theme), ppt.HorizontalLeft)
	oval(slide, emuIn(0.9), emuIn(5.55), emuIn(0.72), emuIn(0.72), theme.gold)
	deckText(slide, emuIn(panel+0.4), emuIn(2.15), emuIn(5.3), emuIn(2.2), subtitle, 18, false, textOn(theme.ink, theme.paper, theme), ppt.HorizontalLeft)
	deckText(slide, emuIn(panel+0.4), emuIn(6.7), emuIn(4), emuIn(0.3), pageMark(page, total), 12, false, textOn(theme.mute, theme.paper, theme), ppt.HorizontalLeft)
}

func paintLessonCover(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total int) {
	paintSolidBackground(slide, theme.paper)
	rect(slide, 0, 0, deckSlideWidth, emuIn(2.35), theme.navy)
	rect(slide, 0, emuIn(2.35), deckSlideWidth, emuIn(0.1), theme.accent)
	if kicker != "" {
		deckText(slide, emuIn(0.6), emuIn(0.28), emuIn(11), emuIn(0.28), kicker, 14, true, textOn(theme.gold, theme.navy, theme), ppt.HorizontalLeft)
	}
	deckText(slide, emuIn(0.6), emuIn(0.7), emuIn(12), emuIn(1.25), title, coverTitlePointSize(title, 36, 12, 1.25), true, textOn(theme.white, theme.navy, theme), ppt.HorizontalLeft)
	deckText(slide, emuIn(0.55), emuIn(2.65), emuIn(12.2), emuIn(0.6), subtitle, 16, false, textOn(theme.ink, theme.paper, theme), ppt.HorizontalLeft)
	steps := []ppt.Color{theme.accent, theme.gold, theme.navy2}
	for i, step := range steps {
		x := 0.55 + float64(i)*4.2
		rect(slide, emuIn(x), emuIn(3.55), emuIn(3.95), emuIn(3.15), theme.card)
		oval(slide, emuIn(x+0.28), emuIn(3.85), emuIn(0.62), emuIn(0.62), step)
		rect(slide, emuIn(x+1.15), emuIn(4.05), emuIn(2.2), emuIn(0.08), step)
		rect(slide, emuIn(x+0.28), emuIn(4.85), emuIn(3.3), emuIn(0.045), blend(theme.slate, theme.card, 0.2))
		rect(slide, emuIn(x+0.28), emuIn(5.25), emuIn(2.7), emuIn(0.045), blend(theme.slate, theme.card, 0.15))
	}
}

func paintSplitChrome(slide *ppt.Slide, theme deckTheme, kicker, title, footer string, page, total int) contentBox {
	rect(slide, 0, 0, emuIn(0.12), deckSlideHeight, theme.accent)
	return paintContentTitle(slide, theme, kicker, title, footer, page, total, 0.5, theme.accent, false)
}

func paintInkChrome(slide *ppt.Slide, theme deckTheme, kicker, title, footer string, page, total int) contentBox {
	if kicker != "" {
		deckText(slide, emuIn(0.7), emuIn(0.22), emuIn(8), emuIn(0.24), kicker, 11, true, textOn(theme.navy, theme.paper, theme), ppt.HorizontalLeft)
	}
	rect(slide, emuIn(0.7), emuIn(0.52), deckSlideWidth-emuIn(1.4), emuIn(0.01), theme.ink)
	if title != "" {
		deckText(slide, emuIn(0.7), emuIn(0.66), emuIn(12), emuIn(0.5), title, 28, true, theme.ink, ppt.HorizontalLeft)
	}
	rect(slide, emuIn(0.7), emuIn(6.95), deckSlideWidth-emuIn(1.4), emuIn(0.01), theme.ink)
	if footer != "" {
		deckText(slide, emuIn(0.7), emuIn(7.08), emuIn(9), emuIn(0.28), footer, 11, false, textOn(theme.mute, theme.paper, theme), ppt.HorizontalLeft)
	}
	paintPageMark(slide, page, total, textOn(theme.mute, theme.paper, theme))
	return boxAfterHeader(0.7, 1.32, 0.5, 6.75)
}

func paintDashboardChrome(slide *ppt.Slide, theme deckTheme, kicker, title, footer string, page, total int) contentBox {
	const left = 0.55
	titleY, titleSize := 0.42, 28
	titleBoxH := 0.5
	if label, below := paintKickerBadge(slide, theme, kicker, left, 0.36, 11, 8); label != "" {
		titleY, titleSize = below, 26
	}
	if title != "" {
		deckText(slide, emuIn(left), emuIn(titleY), emuIn(11.5), emuIn(titleBoxH), title, titleSize, true, textOn(theme.navy, theme.paper, theme), ppt.HorizontalLeft)
	}
	if footer != "" {
		deckText(slide, emuIn(left), emuIn(7.08), emuIn(9), emuIn(0.28), footer, 11, false, textOn(theme.mute, theme.paper, theme), ppt.HorizontalLeft)
	}
	paintPageMark(slide, page, total, textOn(theme.mute, theme.paper, theme))
	return boxAfterHeader(left, titleY+titleBoxH+0.2, 0.45, 6.85)
}

func paintBoardChrome(slide *ppt.Slide, theme deckTheme, kicker, title, footer string, page, total int) contentBox {
	box := paintContentTitle(slide, theme, kicker, title, footer, page, total, 0.6, theme.accent, false)
	// Sit the marker underline just under the title box paintContentTitle uses.
	ruleY := 0.96
	if kicker != "" {
		ruleY = 1.12
	}
	rect(slide, emuIn(0.6), emuIn(ruleY), emuIn(2.6), emuIn(0.06), theme.accent)
	return box
}

func paintDefenseChrome(slide *ppt.Slide, theme deckTheme, kicker, title, footer string, page, total int) contentBox {
	box := paintContentTitle(slide, theme, kicker, title, footer, page, total, 0.55, theme.navy, false)
	ruleY := 0.96
	if kicker != "" {
		ruleY = 1.12
	}
	rect(slide, emuIn(0.55), emuIn(ruleY), emuIn(1.7), emuIn(0.035), theme.navy)
	rect(slide, emuIn(2.35), emuIn(ruleY), emuIn(0.4), emuIn(0.035), theme.accent)
	return box
}

func paintConsultingChrome(slide *ppt.Slide, theme deckTheme, kicker, title, footer string, page, total int) contentBox {
	const left = 0.6
	titleY := 0.36
	if kicker != "" {
		deckText(slide, emuIn(left), emuIn(0.22), emuIn(11), emuIn(0.24), kicker, 11, true, textOn(theme.accent, theme.paper, theme), ppt.HorizontalLeft)
		titleY = 0.5
	}
	if title != "" {
		deckText(slide, emuIn(left), emuIn(titleY), emuIn(12), emuIn(0.5), title, 28, true, theme.ink, ppt.HorizontalLeft)
	}
	rect(slide, emuIn(left), emuIn(titleY+0.58), deckSlideWidth-emuIn(left+0.5), emuIn(0.012), blend(theme.slate, theme.paper, 0.55))
	if footer != "" {
		deckText(slide, emuIn(left), emuIn(7.08), emuIn(9), emuIn(0.28), footer, 11, false, textOn(theme.mute, theme.paper, theme), ppt.HorizontalLeft)
	}
	paintPageMark(slide, page, total, textOn(theme.mute, theme.paper, theme))
	return boxAfterHeader(left, titleY+0.78, 0.5, 6.85)
}

func paintConsultingSection(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total, sectionNo int) {
	paintSolidBackground(slide, theme.paper)
	titleX := 0.75
	if sectionNo > 0 {
		deckText(slide, emuIn(0.55), emuIn(1.7), emuIn(3.2), emuIn(2.1), fmt.Sprintf("%02d", sectionNo), 96, true, blend(theme.navy, theme.paper, 0.72), ppt.HorizontalLeft)
		titleX = 4.0
	}
	if kicker != "" {
		deckText(slide, emuIn(titleX), emuIn(2.15), emuIn(8.5), emuIn(0.3), kicker, 14, true, textOn(theme.accent, theme.paper, theme), ppt.HorizontalLeft)
	}
	deckText(slide, emuIn(titleX), emuIn(2.6), emuIn(8.8), emuIn(1.5), title, coverTitlePointSize(title, 36, 8.8, 1.5), true, theme.ink, ppt.HorizontalLeft)
	deckText(slide, emuIn(titleX), emuIn(4.3), emuIn(8.8), emuIn(0.8), subtitle, 18, false, textOn(theme.slate, theme.paper, theme), ppt.HorizontalLeft)
	paintPageMark(slide, page, total, textOn(theme.mute, theme.paper, theme))
}

func paintFieldSection(slide *ppt.Slide, field, titleColor, rule ppt.Color, title, subtitle, kicker string, page, total int) {
	paintSolidBackground(slide, field)
	if kicker != "" {
		deckText(slide, emuIn(0.75), emuIn(2.15), emuIn(11.5), emuIn(0.3), kicker, 14, true, textOn(rule, field, deckTheme{white: titleColor, ink: titleColor, navy: titleColor, onDark: titleColor}), ppt.HorizontalLeft)
	}
	deckText(slide, emuIn(0.75), emuIn(2.55), emuIn(11.6), emuIn(1.5), title, coverTitlePointSize(title, 40, 11.6, 1.5), true, textOn(titleColor, field, deckTheme{white: titleColor, ink: titleColor, navy: field, onDark: titleColor}), ppt.HorizontalLeft)
	rect(slide, emuIn(0.75), emuIn(4.25), emuIn(1.6), emuIn(0.035), rule)
	deckText(slide, emuIn(0.75), emuIn(4.45), emuIn(11), emuIn(0.8), subtitle, 18, false, textOn(titleColor, field, deckTheme{white: titleColor, ink: titleColor, navy: field, onDark: titleColor}), ppt.HorizontalLeft)
	paintPageMark(slide, page, total, textOn(titleColor, field, deckTheme{white: titleColor, ink: titleColor, navy: field, onDark: titleColor}))
}

func paintSplitSection(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total int) {
	rect(slide, 0, 0, emuIn(7.4), deckSlideHeight, theme.navy)
	rect(slide, emuIn(7.4), 0, deckSlideWidth-emuIn(7.4), deckSlideHeight, theme.paper)
	rect(slide, emuIn(7.4), 0, emuIn(0.1), deckSlideHeight, theme.accent)
	if kicker != "" {
		deckText(slide, emuIn(0.55), emuIn(2.2), emuIn(6.4), emuIn(0.3), kicker, 14, true, textOn(theme.gold, theme.navy, theme), ppt.HorizontalLeft)
	}
	deckText(slide, emuIn(0.55), emuIn(2.65), emuIn(6.4), emuIn(2.2), title, coverTitlePointSize(title, 36, 6.4, 2.2), true, textOn(theme.white, theme.navy, theme), ppt.HorizontalLeft)
	deckText(slide, emuIn(7.8), emuIn(2.9), emuIn(4.8), emuIn(1.6), subtitle, 18, false, textOn(theme.ink, theme.paper, theme), ppt.HorizontalLeft)
	paintPageMark(slide, page, total, textOn(theme.mute, theme.paper, theme))
}

func placeCoverTitle(slide *ppt.Slide, x, y, w, h float64, title string, base int, color ppt.Color, align ppt.HorizontalAlignment) (size int, nextY float64) {
	title = sanitizeXMLText(strings.TrimSpace(title))
	if title == "" {
		return base, y
	}
	size = coverTitlePointSize(title, base, w, h)
	deckText(slide, emuIn(x), emuIn(y), emuIn(w), emuIn(h), title, size, true, color, align)
	nextY = y + float64(coverTitleLines(title, size, w))*float64(size)/72*1.35 + 0.14
	if nextY > y+h+0.08 {
		nextY = y + h + 0.08
	}
	if nextY > 6.05 {
		nextY = 6.05
	}
	return size, nextY
}

// boxAfterHeader is the body region under a header. A tall kicker cannot push
// the top past the footer, which would make card height negative.
func boxAfterHeader(left, top, right, bottom float64) contentBox {
	if bottom-top < 1.2 {
		top = bottom - 1.2
	}
	return contentBox{
		x: emuIn(left),
		y: emuIn(top),
		w: deckSlideWidth - emuIn(left+right),
		h: emuIn(bottom - top),
	}
}

func cornerMarks(slide *ppt.Slide, color ppt.Color) {
	const inset, arm, t = 0.36, 0.38, 0.016
	marks := [][2]float64{{inset, inset}, {13.333 - inset - arm, inset}, {inset, 7.5 - inset - t}, {13.333 - inset - arm, 7.5 - inset - t}}
	verts := [][2]float64{{inset, inset}, {13.333 - inset - t, inset}, {inset, 7.5 - inset - arm}, {13.333 - inset - t, 7.5 - inset - arm}}
	for _, m := range marks {
		rect(slide, emuIn(m[0]), emuIn(m[1]), emuIn(arm), emuIn(t), color)
	}
	for _, m := range verts {
		rect(slide, emuIn(m[0]), emuIn(m[1]), emuIn(t), emuIn(arm), color)
	}
}

func cardStripe(theme deckTheme) (ppt.Color, bool) {
	switch theme.motif {
	case motifDefense:
		return theme.navy, true
	case motifOfficial:
		return theme.gold, true
	case motifReport, motifDashboard, motifLesson:
		return theme.accent, true
	default:
		return theme.accent, false
	}
}

func buildOutlineCards(slide *ppt.Slide, theme deckTheme, box contentBox, texts []string) {
	cols, rows, cardW, cardH, y0, gap := cardGrid(box, len(texts))
	chips := []ppt.Color{theme.accent, theme.gold, theme.navy2, theme.navy}
	for i, text := range texts {
		x, y := cardOrigin(box, cols, cardW, cardH, gap, y0, i)
		strokeFrame(slide, x, y, cardW, cardH, emuIn(0.018), theme.ink)
		rect(slide, x+emuIn(0.18), y+emuIn(0.18), emuIn(0.28), emuIn(0.28), chips[i%len(chips)])
		paintCardText(slide, theme, theme.paper, x+emuIn(0.5), y, cardW-emuIn(0.62), cardH, i, text, rows)
	}
}

func buildNoteCards(slide *ppt.Slide, theme deckTheme, box contentBox, texts []string) {
	cols, rows, cardW, cardH, y0, gap := cardGrid(box, len(texts))
	for i, text := range texts {
		x, y := cardOrigin(box, cols, cardW, cardH, gap, y0, i)
		fill := noteFill(theme, i)
		roundRect(slide, x, y, cardW, cardH, fill)
		rect(slide, x+cardW/2-emuIn(0.35), y, emuIn(0.7), emuIn(0.12), theme.gold)
		paintCardText(slide, theme, fill, x, y, cardW, cardH, i, text, rows)
	}
}

func buildRailCards(slide *ppt.Slide, theme deckTheme, box contentBox, texts []string) {
	cols, rows, cardW, cardH, y0, gap := cardGrid(box, len(texts))
	for i, text := range texts {
		x, y := cardOrigin(box, cols, cardW, cardH, gap, y0, i)
		rect(slide, x, y, emuIn(0.08), cardH, theme.accent)
		paintCardText(slide, theme, theme.paper, x+emuIn(0.1), y, cardW-emuIn(0.14), cardH, i, text, rows)
	}
}

func buildFilledCards(slide *ppt.Slide, theme deckTheme, box contentBox, texts []string) {
	cols, rows, cardW, cardH, y0, gap := cardGrid(box, len(texts))
	stripe, striped := cardStripe(theme)
	for i, text := range texts {
		x, y := cardOrigin(box, cols, cardW, cardH, gap, y0, i)
		rect(slide, x, y, cardW, cardH, theme.card)
		if striped {
			rect(slide, x, y, cardW, emuIn(0.06), stripe)
		}
		paintCardText(slide, theme, theme.card, x, y, cardW, cardH, i, text, rows)
	}
}

func cardGrid(box contentBox, n int) (cols, rows int, cardW, cardH, y0, gap int64) {
	cols, rows = gridSpec(n, 3)
	if n == 4 {
		cols, rows = 2, 2
	}
	maxH := emuIn(2.60)
	if rows > 1 {
		maxH = emuIn(1.90)
	}
	cardW, gap = fitSpan(box.w, int64(cols), emuIn(0.16), 0)
	var vGap int64
	cardH, vGap = fitSpan(box.h, int64(rows), emuIn(0.16), maxH)
	if vGap < gap {
		gap = vGap
	}
	stack := cardH*int64(rows) + gap*int64(rows-1)
	y0 = box.y
	if stack < box.h {
		y0 = box.y + (box.h-stack)/2
	}
	return cols, rows, cardW, cardH, y0, gap
}

// fitSpan splits span into count items separated by gap, without growing past
// span. maxItem caps one item; zero means no cap. A short region shrinks the
// gap instead of pushing the last item out of the box.
func fitSpan(span, count, gap, maxItem int64) (item, usedGap int64) {
	if count < 1 {
		count = 1
	}
	if span < count {
		span = count
	}
	usedGap = gap
	if usedGap < 0 {
		usedGap = 0
	}
	if count > 1 && span <= usedGap*(count-1) {
		usedGap = 0
	}
	item = (span - usedGap*(count-1)) / count
	if item < 1 {
		item = 1
	}
	if maxItem > 0 && item > maxItem {
		item = maxItem
	}
	return item, usedGap
}

// remain is the leftover height inside a box after start, minus a bottom pad.
func remain(start, boxTop, boxH, pad int64) int64 {
	end := boxTop + boxH - pad
	if end <= start {
		return 0
	}
	return end - start
}

func cardOrigin(box contentBox, cols int, cardW, cardH, gap, y0 int64, i int) (x, y int64) {
	col := i % cols
	row := i / cols
	return box.x + int64(col)*(cardW+gap), y0 + int64(row)*(cardH+gap)
}

func paintCardText(slide *ppt.Slide, theme deckTheme, bg ppt.Color, x, y, cardW, cardH int64, index int, text string, rows int) {
	titleSize, bodySize := 18, 16
	if rows > 1 {
		titleSize, bodySize = 16, 15
	}
	textY := y + emuIn(0.46)
	if theme.motif == motifPoster {
		textY = y + emuIn(0.14)
	} else {
		deckText(slide, x+emuIn(0.18), y+emuIn(0.16), cardW-emuIn(0.36), emuIn(0.28), fmt.Sprintf("%02d", index+1), 12, true, readableAccent(theme, bg), ppt.HorizontalLeft)
	}
	head, body := splitCardLine(text)
	if head != "" {
		deckText(slide, x+emuIn(0.18), textY, cardW-emuIn(0.36), emuIn(0.42), head, titleSize, true, textOn(theme.navy, bg, theme), ppt.HorizontalLeft)
		bodyTop := textY + emuIn(0.46)
		deckText(slide, x+emuIn(0.18), bodyTop, cardW-emuIn(0.36), remain(bodyTop, y, cardH, emuIn(0.1)), body, bodySize, false, textOn(theme.slate, bg, theme), ppt.HorizontalLeft)
		return
	}
	deckText(slide, x+emuIn(0.18), textY, cardW-emuIn(0.36), remain(textY, y, cardH, emuIn(0.1)), text, bodySize+1, false, textOn(theme.ink, bg, theme), ppt.HorizontalLeft)
}

func noteFill(theme deckTheme, i int) ppt.Color {
	switch i % 3 {
	case 1:
		return blend(theme.gold, theme.white, 0.35)
	case 2:
		return blend(theme.accent, theme.white, 0.4)
	default:
		return theme.card
	}
}

func openPointRule(theme deckTheme) ppt.Color {
	switch theme.motif {
	case motifConsulting:
		return blend(theme.slate, theme.paper, 0.45)
	case motifSketch:
		return blend(theme.ink, theme.paper, 0.35)
	default:
		return blend(theme.ink, theme.paper, 0.25)
	}
}
