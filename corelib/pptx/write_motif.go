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
		return paintPosterChrome(slide, theme, kicker, title, footer, page, total)
	case motifSketch:
		return paintSketchChrome(slide, theme, kicker, title, footer, page, total)
	case motifBoard:
		return paintBoardChrome(slide, theme, kicker, title, footer, page, total)
	case motifCraft:
		return paintCraftChrome(slide, theme, kicker, title, footer, page, total)
	case motifDefense:
		return paintDefenseChrome(slide, theme, kicker, title, footer, page, total)
	case motifConsulting:
		return paintConsultingChrome(slide, theme, kicker, title, footer, page, total)
	case motifOfficial:
		return paintOfficialChrome(slide, theme, kicker, title, footer, page, total)
	case motifLesson:
		return paintLessonChrome(slide, theme, kicker, title, footer, page, total)
	default:
		rect(slide, 0, 0, emuIn(0.14), deckSlideHeight, theme.accent)
		rect(slide, 0, 0, emuIn(0.14), emuIn(0.42), theme.navy2)
		rect(slide, emuIn(0.55), emuIn(6.96), deckSlideWidth-emuIn(0.97), emuIn(0.01), blend(theme.slate, theme.paper, 0.78))
		return paintContentTitle(slide, theme, kicker, title, footer, page, total, 0.55, theme.accent, true)
	}
}

func paintMotifSection(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total, sectionNo int) {
	switch theme.motif {
	case motifConsulting:
		paintConsultingSection(slide, theme, title, subtitle, kicker, page, total, sectionNo)
	case motifOfficial:
		paintOfficialSection(slide, theme, title, subtitle, kicker, page, total, sectionNo)
	case motifDefense:
		paintFieldSection(slide, theme.navy, theme.white, theme.accent, title, subtitle, kicker, page, total)
	case motifLesson:
		paintLessonSection(slide, theme, title, subtitle, kicker, page, total, sectionNo)
	case motifSplit:
		paintSplitSection(slide, theme, title, subtitle, kicker, page, total)
	case motifInk:
		paintInkSection(slide, theme, title, subtitle, kicker, page, total, sectionNo)
	case motifDashboard:
		paintDashboardSection(slide, theme, title, subtitle, kicker, page, total, sectionNo)
	case motifPoster:
		paintPosterSection(slide, theme, title, subtitle, kicker, page, total, sectionNo)
	case motifSketch:
		paintSketchSection(slide, theme, title, subtitle, kicker, page, total, sectionNo)
	case motifBoard:
		paintBoardSection(slide, theme, title, subtitle, kicker, page, total, sectionNo)
	case motifCraft:
		paintCraftSection(slide, theme, title, subtitle, kicker, page, total, sectionNo)
	default:
		paintReportSection(slide, theme, title, subtitle, kicker, page, total, sectionNo)
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
	rect(slide, emuIn(0.12), 0, emuIn(0.04), deckSlideHeight, theme.navy)
	rect(slide, deckSlideWidth-emuIn(1.35), 0, emuIn(0.95), emuIn(0.12), theme.navy)
	rect(slide, deckSlideWidth-emuIn(0.40), 0, emuIn(0.40), emuIn(0.12), theme.accent)
	rect(slide, emuIn(0.5), emuIn(6.96), deckSlideWidth-emuIn(0.92), emuIn(0.015), theme.ink)
	return paintContentTitle(slide, theme, kicker, title, footer, page, total, 0.5, theme.accent, false)
}

func paintInkChrome(slide *ppt.Slide, theme deckTheme, kicker, title, footer string, page, total int) contentBox {
	rect(slide, deckSlideWidth-emuIn(0.18), 0, emuIn(0.18), deckSlideHeight, theme.navy)
	rect(slide, deckSlideWidth-emuIn(0.18), emuIn(0.52), emuIn(0.18), emuIn(0.32), theme.accent)
	if kicker != "" {
		deckText(slide, emuIn(0.7), emuIn(0.22), emuIn(8), emuIn(0.24), kicker, 11, true, textOn(theme.navy, theme.paper, theme), ppt.HorizontalLeft)
	}
	rect(slide, emuIn(0.7), emuIn(0.52), deckSlideWidth-emuIn(1.4), emuIn(0.01), theme.ink)
	rect(slide, emuIn(0.7), emuIn(0.50), emuIn(0.65), emuIn(0.03), theme.accent)
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
	rect(slide, 0, 0, deckSlideWidth, emuIn(0.10), theme.navy)
	rect(slide, 0, 0, emuIn(3.2), emuIn(0.10), theme.accent)
	rect(slide, emuIn(3.2), 0, emuIn(1.1), emuIn(0.10), theme.gold)
	rect(slide, emuIn(0.22), emuIn(0.46), emuIn(0.14), emuIn(0.14), theme.accent)
	rect(slide, emuIn(0.22), emuIn(0.66), emuIn(0.14), emuIn(0.06), theme.gold)
	if label, below := paintKickerBadge(slide, theme, kicker, left, 0.36, 11, 8); label != "" {
		titleY, titleSize = below, 26
	}
	if title != "" {
		deckText(slide, emuIn(left), emuIn(titleY), emuIn(11.5), emuIn(titleBoxH), title, titleSize, true, textOn(theme.navy, theme.paper, theme), ppt.HorizontalLeft)
	}
	rect(slide, emuIn(left), emuIn(titleY+titleBoxH+0.08), deckSlideWidth-emuIn(left+0.45), emuIn(0.015), blend(theme.slate, theme.paper, 0.75))
	if footer != "" {
		deckText(slide, emuIn(left), emuIn(7.08), emuIn(9), emuIn(0.28), footer, 11, false, textOn(theme.mute, theme.paper, theme), ppt.HorizontalLeft)
	}
	paintPageMark(slide, page, total, textOn(theme.mute, theme.paper, theme))
	return boxAfterHeader(left, titleY+titleBoxH+0.2, 0.45, 6.85)
}

func paintPosterChrome(slide *ppt.Slide, theme deckTheme, kicker, title, footer string, page, total int) contentBox {
	bands := []ppt.Color{theme.accent, theme.gold, theme.navy2, theme.navy}
	segW := deckSlideWidth / int64(len(bands))
	for i, c := range bands {
		rect(slide, int64(i)*segW, 0, segW+emuIn(0.02), emuIn(0.14), c)
	}
	rect(slide, 0, emuIn(0.14), deckSlideWidth, emuIn(0.02), theme.ink)
	rect(slide, emuIn(0.62), emuIn(6.96), deckSlideWidth-emuIn(1.04), emuIn(0.02), theme.ink)
	return paintContentTitle(slide, theme, kicker, title, footer, page, total, 0.62, theme.ink, false)
}

func paintSketchChrome(slide *ppt.Slide, theme deckTheme, kicker, title, footer string, page, total int) contentBox {
	cornerMarks(slide, theme.ink)
	box := paintContentTitle(slide, theme, kicker, title, footer, page, total, 0.7, theme.ink, false)
	ruleY := 0.96
	if kicker != "" {
		ruleY = 1.12
	}
	rect(slide, emuIn(0.7), emuIn(ruleY), emuIn(1.8), emuIn(0.07), theme.accent)
	rect(slide, emuIn(2.58), emuIn(ruleY+0.02), emuIn(0.65), emuIn(0.03), theme.gold)
	rect(slide, emuIn(0.7), emuIn(ruleY+0.07), emuIn(3.6), emuIn(0.015), theme.ink)
	return box
}

func paintBoardChrome(slide *ppt.Slide, theme deckTheme, kicker, title, footer string, page, total int) contentBox {
	strokeFrame(slide, emuIn(0.18), emuIn(0.14), deckSlideWidth-emuIn(0.36), deckSlideHeight-emuIn(0.28), emuIn(0.02), blend(theme.slate, theme.paper, 0.65))
	markers := []ppt.Color{theme.accent, theme.navy2, theme.gold}
	for i, m := range markers {
		rect(slide, deckSlideWidth-emuIn(1.45-float64(i)*0.36), emuIn(0.14), emuIn(0.28), emuIn(0.08), m)
	}
	box := paintContentTitle(slide, theme, kicker, title, footer, page, total, 0.6, theme.accent, false)
	// Sit the marker underline just under the title box paintContentTitle uses.
	ruleY := 0.96
	if kicker != "" {
		ruleY = 1.12
	}
	rect(slide, emuIn(0.6), emuIn(ruleY), emuIn(2.6), emuIn(0.06), theme.accent)
	rect(slide, emuIn(3.3), emuIn(ruleY), emuIn(0.8), emuIn(0.06), theme.gold)
	return box
}

func paintCraftChrome(slide *ppt.Slide, theme deckTheme, kicker, title, footer string, page, total int) contentBox {
	rect(slide, 0, 0, deckSlideWidth, emuIn(0.1), theme.accent)
	rect(slide, 0, emuIn(0.1), deckSlideWidth, emuIn(0.04), theme.gold)
	box := paintContentTitle(slide, theme, kicker, title, footer, page, total, 0.62, theme.navy2, false)
	ruleY := 0.96
	if kicker != "" {
		ruleY = 1.12
	}
	roundRect(slide, emuIn(0.62), emuIn(ruleY), emuIn(1.85), emuIn(0.07), theme.accent)
	roundRect(slide, emuIn(2.58), emuIn(ruleY), emuIn(0.65), emuIn(0.07), theme.gold)
	return box
}

func paintDefenseChrome(slide *ppt.Slide, theme deckTheme, kicker, title, footer string, page, total int) contentBox {
	rect(slide, 0, 0, deckSlideWidth, emuIn(0.10), theme.navy)
	rect(slide, deckSlideWidth-emuIn(2.2), 0, emuIn(2.2), emuIn(0.10), theme.accent)
	box := paintContentTitle(slide, theme, kicker, title, footer, page, total, 0.55, theme.navy, false)
	ruleY := 0.96
	if kicker != "" {
		ruleY = 1.12
	}
	rect(slide, emuIn(0.55), emuIn(ruleY), emuIn(1.7), emuIn(0.035), theme.navy)
	rect(slide, emuIn(2.35), emuIn(ruleY), emuIn(0.4), emuIn(0.035), theme.accent)
	rect(slide, emuIn(0.55), emuIn(6.96), deckSlideWidth-emuIn(0.97), emuIn(0.014), blend(theme.navy, theme.paper, 0.72))
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
	rect(slide, emuIn(left), emuIn(6.95), deckSlideWidth-emuIn(left+0.5), emuIn(0.012), blend(theme.slate, theme.paper, 0.55))
	if footer != "" {
		deckText(slide, emuIn(left), emuIn(7.08), emuIn(9), emuIn(0.28), footer, 11, false, textOn(theme.mute, theme.paper, theme), ppt.HorizontalLeft)
	}
	paintPageMark(slide, page, total, textOn(theme.mute, theme.paper, theme))
	return boxAfterHeader(left, titleY+0.78, 0.5, 6.85)
}

func paintOfficialChrome(slide *ppt.Slide, theme deckTheme, kicker, title, footer string, page, total int) contentBox {
	rect(slide, 0, 0, deckSlideWidth, emuIn(0.12), theme.accent)
	rect(slide, 0, emuIn(0.12), deckSlideWidth, emuIn(0.03), theme.gold)
	box := paintContentTitle(slide, theme, kicker, title, footer, page, total, 0.62, theme.accent, false)
	ruleY := 0.96
	if kicker != "" {
		ruleY = 1.12
	}
	rect(slide, emuIn(0.62), emuIn(ruleY), emuIn(2.1), emuIn(0.04), theme.accent)
	rect(slide, emuIn(2.82), emuIn(ruleY+0.012), emuIn(9.9), emuIn(0.015), theme.gold)
	rect(slide, emuIn(0.62), emuIn(6.96), deckSlideWidth-emuIn(1.04), emuIn(0.018), theme.gold)
	return box
}

func paintLessonChrome(slide *ppt.Slide, theme deckTheme, kicker, title, footer string, page, total int) contentBox {
	rect(slide, 0, 0, deckSlideWidth, emuIn(0.16), theme.navy)
	rect(slide, 0, emuIn(0.16), deckSlideWidth, emuIn(0.04), theme.accent)
	box := paintContentTitle(slide, theme, kicker, title, footer, page, total, 0.62, theme.navy, false)
	ruleY := 0.96
	if kicker != "" {
		ruleY = 1.12
	}
	rect(slide, emuIn(0.62), emuIn(ruleY), emuIn(1.6), emuIn(0.045), theme.accent)
	rect(slide, emuIn(2.32), emuIn(ruleY+0.015), emuIn(0.55), emuIn(0.03), theme.gold)
	rect(slide, emuIn(0.62), emuIn(6.96), deckSlideWidth-emuIn(1.04), emuIn(0.015), blend(theme.navy, theme.paper, 0.72))
	return box
}

func paintReportSection(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total, sectionNo int) {
	paintSolidBackground(slide, theme.paper)
	rect(slide, 0, 0, emuIn(0.28), deckSlideHeight, theme.accent)
	rect(slide, 0, 0, emuIn(0.28), emuIn(0.55), theme.navy2)
	rect(slide, emuIn(0.65), emuIn(1.65), emuIn(11.9), emuIn(4.1), theme.card)
	rect(slide, emuIn(0.65), emuIn(1.65), emuIn(11.9), emuIn(0.10), theme.accent)
	stripes := []ppt.Color{theme.accent, theme.navy2, theme.gold}
	for i, c := range stripes {
		rect(slide, emuIn(11.95), emuIn(2.15+float64(i)*1.05), emuIn(0.22), emuIn(0.75), c)
	}
	if sectionNo > 0 && kicker == "" {
		kicker = fmt.Sprintf("%02d", sectionNo)
	}
	if kicker != "" {
		deckText(slide, emuIn(1.05), emuIn(2.10), emuIn(10.5), emuIn(0.32), kicker, 14, true, textOn(theme.accent, theme.card, theme), ppt.HorizontalLeft)
	}
	deckText(slide, emuIn(1.05), emuIn(2.55), emuIn(10.5), emuIn(1.55), title, coverTitlePointSize(title, 40, 10.5, 1.55), true, textOn(theme.navy, theme.card, theme), ppt.HorizontalLeft)
	rect(slide, emuIn(1.05), emuIn(4.25), emuIn(1.8), emuIn(0.045), theme.accent)
	rect(slide, emuIn(2.95), emuIn(4.25), emuIn(0.65), emuIn(0.045), theme.gold)
	deckText(slide, emuIn(1.05), emuIn(4.48), emuIn(10.5), emuIn(0.85), subtitle, 18, false, textOn(theme.slate, theme.card, theme), ppt.HorizontalLeft)
	paintPageMark(slide, page, total, textOn(theme.mute, theme.paper, theme))
}

func paintOfficialSection(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total, sectionNo int) {
	paintSolidBackground(slide, theme.accent)
	strokeFrame(slide, emuIn(0.38), emuIn(0.38), deckSlideWidth-emuIn(0.76), deckSlideHeight-emuIn(0.76), emuIn(0.022), theme.gold)
	strokeFrame(slide, emuIn(0.52), emuIn(0.52), deckSlideWidth-emuIn(1.04), deckSlideHeight-emuIn(1.04), emuIn(0.012), theme.gold)
	oval(slide, emuIn(11.45), emuIn(1.05), emuIn(0.72), emuIn(0.72), theme.gold)
	if kicker != "" {
		deckText(slide, emuIn(0.95), emuIn(2.05), emuIn(10.8), emuIn(0.32), kicker, 14, true, textOn(theme.gold, theme.accent, theme), ppt.HorizontalLeft)
	}
	deckText(slide, emuIn(0.95), emuIn(2.50), emuIn(11.0), emuIn(1.55), title, coverTitlePointSize(title, 40, 11.0, 1.55), true, textOn(theme.white, theme.accent, theme), ppt.HorizontalLeft)
	rect(slide, emuIn(0.95), emuIn(4.25), emuIn(2.2), emuIn(0.045), theme.gold)
	deckText(slide, emuIn(0.95), emuIn(4.48), emuIn(10.8), emuIn(0.85), subtitle, 18, false, textOn(theme.white, theme.accent, theme), ppt.HorizontalLeft)
	paintPageMark(slide, page, total, textOn(theme.gold, theme.accent, theme))
}

func paintLessonSection(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total, sectionNo int) {
	paintSolidBackground(slide, theme.navy)
	rect(slide, 0, deckSlideHeight-emuIn(0.28), deckSlideWidth, emuIn(0.28), theme.accent)
	oval(slide, emuIn(11.35), emuIn(0.95), emuIn(0.85), emuIn(0.85), theme.accent)
	oval(slide, emuIn(11.75), emuIn(1.45), emuIn(0.55), emuIn(0.55), theme.gold)
	if kicker != "" {
		deckText(slide, emuIn(0.75), emuIn(2.05), emuIn(10.4), emuIn(0.32), kicker, 14, true, textOn(theme.gold, theme.navy, theme), ppt.HorizontalLeft)
	}
	deckText(slide, emuIn(0.75), emuIn(2.50), emuIn(10.8), emuIn(1.55), title, coverTitlePointSize(title, 40, 10.8, 1.55), true, textOn(theme.white, theme.navy, theme), ppt.HorizontalLeft)
	rect(slide, emuIn(0.75), emuIn(4.25), emuIn(1.8), emuIn(0.05), theme.accent)
	deckText(slide, emuIn(0.75), emuIn(4.48), emuIn(10.8), emuIn(0.85), subtitle, 18, false, textOn(theme.white, theme.navy, theme), ppt.HorizontalLeft)
	paintPageMark(slide, page, total, textOn(theme.white, theme.navy, theme))
}

func paintInkSection(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total, sectionNo int) {
	paintSolidBackground(slide, theme.paper)
	rect(slide, emuIn(12.05), 0, deckSlideWidth-emuIn(12.05), deckSlideHeight, theme.navy)
	rect(slide, emuIn(12.28), emuIn(0.55), emuIn(0.55), emuIn(0.55), theme.accent)
	rect(slide, emuIn(0.7), emuIn(0.78), emuIn(10.9), emuIn(0.012), theme.ink)
	rect(slide, emuIn(0.7), emuIn(6.55), emuIn(10.9), emuIn(0.012), theme.ink)
	if kicker != "" {
		deckText(slide, emuIn(0.75), emuIn(1.95), emuIn(10.5), emuIn(0.3), kicker, 14, true, textOn(theme.accent, theme.paper, theme), ppt.HorizontalLeft)
	}
	deckText(slide, emuIn(0.75), emuIn(2.40), emuIn(10.6), emuIn(1.65), title, coverTitlePointSize(title, 44, 10.6, 1.65), true, theme.ink, ppt.HorizontalLeft)
	rect(slide, emuIn(0.75), emuIn(4.30), emuIn(1.6), emuIn(0.045), theme.accent)
	deckText(slide, emuIn(0.75), emuIn(4.52), emuIn(10.5), emuIn(0.85), subtitle, 18, false, textOn(theme.slate, theme.paper, theme), ppt.HorizontalLeft)
	paintPageMark(slide, page, total, textOn(theme.mute, theme.paper, theme))
}

func paintDashboardSection(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total, sectionNo int) {
	paintSolidBackground(slide, theme.paper)
	rect(slide, 0, 0, deckSlideWidth, emuIn(0.95), theme.white)
	rect(slide, emuIn(0.42), emuIn(0.38), emuIn(0.18), emuIn(0.18), theme.accent)
	rect(slide, emuIn(0.65), emuIn(1.55), emuIn(12.0), emuIn(4.45), theme.card)
	rect(slide, emuIn(0.65), emuIn(1.55), emuIn(0.12), emuIn(4.45), theme.accent)
	rect(slide, emuIn(0.65), emuIn(1.55), emuIn(12.0), emuIn(0.08), theme.navy)
	heights := []float64{0.85, 1.35, 1.05, 1.85, 1.45}
	bars := []ppt.Color{theme.accent, theme.gold, theme.navy2, theme.navy, theme.accent}
	for i, h := range heights {
		rect(slide, emuIn(9.85+float64(i)*0.48), emuIn(5.45-h), emuIn(0.32), emuIn(h), blend(bars[i], theme.card, 0.25))
	}
	if kicker != "" {
		deckText(slide, emuIn(1.10), emuIn(2.05), emuIn(8.4), emuIn(0.3), kicker, 14, true, textOn(theme.accent, theme.card, theme), ppt.HorizontalLeft)
	}
	deckText(slide, emuIn(1.10), emuIn(2.48), emuIn(8.4), emuIn(1.55), title, coverTitlePointSize(title, 38, 8.4, 1.55), true, textOn(theme.navy, theme.card, theme), ppt.HorizontalLeft)
	rect(slide, emuIn(1.10), emuIn(4.22), emuIn(1.6), emuIn(0.045), theme.gold)
	deckText(slide, emuIn(1.10), emuIn(4.45), emuIn(8.4), emuIn(0.85), subtitle, 18, false, textOn(theme.slate, theme.card, theme), ppt.HorizontalLeft)
	paintPageMark(slide, page, total, textOn(theme.mute, theme.paper, theme))
}

func paintPosterSection(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total, sectionNo int) {
	paintSolidBackground(slide, theme.paper)
	bands := []ppt.Color{theme.accent, theme.gold, theme.navy2, theme.navy}
	segW := deckSlideWidth / int64(len(bands))
	for i, c := range bands {
		rect(slide, int64(i)*segW, 0, segW+emuIn(0.02), emuIn(0.45), c)
	}
	rect(slide, 0, emuIn(0.45), deckSlideWidth, emuIn(0.03), theme.ink)
	rect(slide, emuIn(0.82), emuIn(1.62), emuIn(11.75), emuIn(4.35), blend(theme.ink, theme.paper, 0.65))
	rect(slide, emuIn(0.75), emuIn(1.55), emuIn(11.75), emuIn(4.35), theme.card)
	rect(slide, emuIn(0.75), emuIn(1.55), emuIn(11.75), emuIn(0.38), theme.gold)
	strokeFrame(slide, emuIn(0.75), emuIn(1.55), emuIn(11.75), emuIn(4.35), emuIn(0.028), theme.ink)
	rect(slide, emuIn(0.75), emuIn(1.93), emuIn(11.75), emuIn(0.024), theme.ink)
	if kicker != "" {
		deckText(slide, emuIn(1.15), emuIn(2.15), emuIn(10.8), emuIn(0.3), kicker, 14, true, textOn(theme.accent, theme.card, theme), ppt.HorizontalLeft)
	}
	deckText(slide, emuIn(1.15), emuIn(2.55), emuIn(10.8), emuIn(1.55), title, coverTitlePointSize(title, 38, 10.8, 1.55), true, theme.ink, ppt.HorizontalLeft)
	rect(slide, emuIn(1.15), emuIn(4.25), emuIn(1.8), emuIn(0.05), theme.accent)
	deckText(slide, emuIn(1.15), emuIn(4.48), emuIn(10.8), emuIn(0.85), subtitle, 18, false, textOn(theme.slate, theme.card, theme), ppt.HorizontalLeft)
	paintPageMark(slide, page, total, textOn(theme.mute, theme.paper, theme))
}

func paintSketchSection(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total, sectionNo int) {
	paintSolidBackground(slide, theme.paper)
	cornerMarks(slide, theme.ink)
	rect(slide, emuIn(0.85), emuIn(1.55), emuIn(11.6), emuIn(4.35), theme.card)
	strokeFrame(slide, emuIn(0.85), emuIn(1.55), emuIn(11.6), emuIn(4.35), emuIn(0.02), theme.ink)
	oval(slide, emuIn(11.15), emuIn(1.95), emuIn(0.65), emuIn(0.65), theme.accent)
	oval(slide, emuIn(11.55), emuIn(2.35), emuIn(0.45), emuIn(0.45), theme.gold)
	if kicker != "" {
		deckText(slide, emuIn(1.25), emuIn(2.10), emuIn(9.8), emuIn(0.3), kicker, 14, true, textOn(theme.ink, theme.card, theme), ppt.HorizontalLeft)
	}
	deckText(slide, emuIn(1.25), emuIn(2.52), emuIn(9.8), emuIn(1.55), title, coverTitlePointSize(title, 38, 9.8, 1.55), true, theme.ink, ppt.HorizontalLeft)
	rect(slide, emuIn(1.25), emuIn(4.22), emuIn(2.2), emuIn(0.08), theme.accent)
	rect(slide, emuIn(1.25), emuIn(4.30), emuIn(3.2), emuIn(0.018), theme.ink)
	deckText(slide, emuIn(1.25), emuIn(4.48), emuIn(9.8), emuIn(0.85), subtitle, 18, false, textOn(theme.slate, theme.card, theme), ppt.HorizontalLeft)
	paintPageMark(slide, page, total, textOn(theme.mute, theme.paper, theme))
}

func paintBoardSection(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total, sectionNo int) {
	paintSolidBackground(slide, theme.paper)
	strokeFrame(slide, emuIn(0.24), emuIn(0.20), deckSlideWidth-emuIn(0.48), deckSlideHeight-emuIn(0.40), emuIn(0.024), blend(theme.slate, theme.paper, 0.6))
	roundRect(slide, emuIn(0.85), emuIn(1.55), emuIn(11.6), emuIn(4.35), theme.card)
	strokeFrame(slide, emuIn(0.85), emuIn(1.55), emuIn(11.6), emuIn(4.35), emuIn(0.045), theme.accent)
	rect(slide, emuIn(5.85), emuIn(1.47), emuIn(1.6), emuIn(0.22), theme.gold)
	if kicker != "" {
		deckText(slide, emuIn(1.25), emuIn(2.10), emuIn(10.6), emuIn(0.3), kicker, 14, true, textOn(theme.navy2, theme.card, theme), ppt.HorizontalLeft)
	}
	deckText(slide, emuIn(1.25), emuIn(2.52), emuIn(10.6), emuIn(1.55), title, coverTitlePointSize(title, 38, 10.6, 1.55), true, theme.ink, ppt.HorizontalLeft)
	rect(slide, emuIn(1.25), emuIn(4.22), emuIn(2.6), emuIn(0.07), theme.accent)
	rect(slide, emuIn(3.95), emuIn(4.22), emuIn(0.9), emuIn(0.07), theme.navy2)
	deckText(slide, emuIn(1.25), emuIn(4.48), emuIn(10.6), emuIn(0.85), subtitle, 18, false, textOn(theme.slate, theme.card, theme), ppt.HorizontalLeft)
	paintPageMark(slide, page, total, textOn(theme.mute, theme.paper, theme))
}

func paintCraftSection(slide *ppt.Slide, theme deckTheme, title, subtitle, kicker string, page, total, sectionNo int) {
	paintSolidBackground(slide, theme.paper)
	roundRect(slide, emuIn(0.85), emuIn(1.55), emuIn(11.6), emuIn(4.35), theme.card)
	roundRect(slide, emuIn(1.15), emuIn(1.85), emuIn(11.0), emuIn(0.18), theme.accent)
	rect(slide, emuIn(5.95), emuIn(1.45), emuIn(1.4), emuIn(0.26), blend(theme.gold, theme.white, 0.15))
	oval(slide, emuIn(11.25), emuIn(4.75), emuIn(0.65), emuIn(0.65), theme.gold)
	if kicker != "" {
		deckText(slide, emuIn(1.25), emuIn(2.20), emuIn(10.4), emuIn(0.3), kicker, 14, true, textOn(theme.navy2, theme.card, theme), ppt.HorizontalLeft)
	}
	deckText(slide, emuIn(1.25), emuIn(2.60), emuIn(10.4), emuIn(1.55), title, coverTitlePointSize(title, 38, 10.4, 1.55), true, textOn(theme.navy, theme.card, theme), ppt.HorizontalLeft)
	roundRect(slide, emuIn(1.25), emuIn(4.28), emuIn(1.9), emuIn(0.08), theme.accent)
	deckText(slide, emuIn(1.25), emuIn(4.50), emuIn(10.0), emuIn(0.85), subtitle, 18, false, textOn(theme.slate, theme.card, theme), ppt.HorizontalLeft)
	paintPageMark(slide, page, total, textOn(theme.mute, theme.paper, theme))
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

func cardSurfaceColor(theme deckTheme) ppt.Color {
	if theme.card == theme.paper {
		return blend(theme.ink, theme.paper, 0.95)
	}
	return theme.card
}

func motifStepColor(theme deckTheme, i, total int) ppt.Color {
	switch theme.motif {
	case motifDefense:
		if total > 1 && i == total-1 {
			return theme.accent
		}
		return theme.navy
	case motifOfficial:
		if i%2 == 1 {
			return theme.gold
		}
		return theme.accent
	case motifConsulting:
		if total > 1 && i == total-1 {
			return theme.accent
		}
		return theme.navy
	default:
		ladder := []ppt.Color{theme.accent, theme.navy2, theme.gold, theme.navy}
		if i < 0 {
			i = 0
		}
		return ladder[i%len(ladder)]
	}
}

func buildOutlineCards(slide *ppt.Slide, theme deckTheme, box contentBox, texts []string) {
	cols, rows, cardW, cardH, y0, gap := cardGrid(box, len(texts))
	chips := []ppt.Color{theme.accent, theme.gold, theme.navy2, theme.navy}
	for i, text := range texts {
		x, y := cardOrigin(box, cols, cardW, cardH, gap, y0, i)
		rect(slide, x+emuIn(0.05), y+emuIn(0.05), cardW, cardH, blend(theme.ink, theme.paper, 0.65))
		rect(slide, x, y, cardW, cardH, theme.card)
		strokeFrame(slide, x, y, cardW, cardH, emuIn(0.022), theme.ink)
		chip := chips[i%len(chips)]
		rect(slide, x+cardW-emuIn(0.46), y+emuIn(0.14), emuIn(0.28), emuIn(0.28), chip)
		strokeFrame(slide, x+cardW-emuIn(0.46), y+emuIn(0.14), emuIn(0.28), emuIn(0.28), emuIn(0.016), theme.ink)
		paintCardText(slide, theme, theme.card, x, y, cardW, cardH, i, text, rows)
	}
}

func buildSketchCards(slide *ppt.Slide, theme deckTheme, box contentBox, texts []string) {
	cols, rows, cardW, cardH, y0, gap := cardGrid(box, len(texts))
	fills := []ppt.Color{theme.accent, theme.gold, blend(theme.navy2, theme.white, 0.35)}
	for i, text := range texts {
		x, y := cardOrigin(box, cols, cardW, cardH, gap, y0, i)
		bg := theme.card
		rect(slide, x, y, cardW, cardH, bg)
		strokeFrame(slide, x, y, cardW, cardH, emuIn(0.018), theme.ink)
		oval(slide, x+emuIn(0.58), y+emuIn(0.16), emuIn(0.24), emuIn(0.24), fills[i%len(fills)])
		rect(slide, x+emuIn(0.90), y+emuIn(0.27), emuIn(1.1), emuIn(0.025), theme.ink)
		if i%cols < cols-1 && i+1 < len(texts) && gap > emuIn(0.04) {
			rect(slide, x+cardW, y+cardH/2-emuIn(0.015), gap, emuIn(0.03), theme.ink)
		}
		paintCardText(slide, theme, bg, x, y, cardW, cardH, i, text, rows)
	}
}

func buildBoardCards(slide *ppt.Slide, theme deckTheme, box contentBox, texts []string) {
	cols, rows, cardW, cardH, y0, gap := cardGrid(box, len(texts))
	strokes := []ppt.Color{theme.accent, theme.navy2, theme.gold}
	for i, text := range texts {
		x, y := cardOrigin(box, cols, cardW, cardH, gap, y0, i)
		fill := noteFill(theme, i)
		stroke := strokes[i%len(strokes)]
		rect(slide, x, y, cardW, cardH, fill)
		strokeFrame(slide, x, y, cardW, cardH, emuIn(0.035), stroke)
		rect(slide, x+emuIn(0.58), y+emuIn(0.26), emuIn(0.95), emuIn(0.045), stroke)
		if i%cols < cols-1 && i+1 < len(texts) && gap > emuIn(0.04) {
			rect(slide, x+cardW, y+cardH/2-emuIn(0.02), gap, emuIn(0.04), theme.ink)
		}
		paintCardText(slide, theme, fill, x, y, cardW, cardH, i, text, rows)
	}
}

func buildNoteCards(slide *ppt.Slide, theme deckTheme, box contentBox, texts []string) {
	cols, rows, cardW, cardH, y0, gap := cardGrid(box, len(texts))
	for i, text := range texts {
		x, y := cardOrigin(box, cols, cardW, cardH, gap, y0, i)
		fill := noteFill(theme, i)
		roundRect(slide, x, y, cardW, cardH, fill)
		rect(slide, x+cardW/2-emuIn(0.42), y-emuIn(0.05), emuIn(0.84), emuIn(0.16), blend(theme.gold, theme.white, 0.2))
		paintCardText(slide, theme, fill, x, y, cardW, cardH, i, text, rows)
	}
}

func buildRailCards(slide *ppt.Slide, theme deckTheme, box contentBox, texts []string) {
	cols, rows, cardW, cardH, y0, gap := cardGrid(box, len(texts))
	rails := []ppt.Color{theme.accent, theme.navy, theme.gold, theme.navy2}
	bg := cardSurfaceColor(theme)
	for i, text := range texts {
		x, y := cardOrigin(box, cols, cardW, cardH, gap, y0, i)
		rail := rails[i%len(rails)]
		rect(slide, x, y, cardW, cardH, bg)
		rect(slide, x, y, emuIn(0.10), cardH, rail)
		rect(slide, x+cardW-emuIn(0.42), y, emuIn(0.42), emuIn(0.08), theme.accent)
		paintCardText(slide, theme, bg, x+emuIn(0.10), y, cardW-emuIn(0.14), cardH, i, text, rows)
	}
}

func buildDashboardCards(slide *ppt.Slide, theme deckTheme, box contentBox, texts []string) {
	cols, rows, cardW, cardH, y0, gap := cardGrid(box, len(texts))
	caps := []ppt.Color{theme.accent, theme.gold, theme.navy2, theme.navy}
	ratios := []float64{0.68, 0.52, 0.82, 0.60, 0.74, 0.45}
	for i, text := range texts {
		x, y := cardOrigin(box, cols, cardW, cardH, gap, y0, i)
		cap := caps[i%len(caps)]
		rect(slide, x, y, cardW, cardH, theme.card)
		strokeFrame(slide, x, y, cardW, cardH, emuIn(0.012), blend(theme.slate, theme.paper, 0.75))
		rect(slide, x, y, cardW, emuIn(0.075), cap)
		oval(slide, x+cardW-emuIn(0.32), y+emuIn(0.18), emuIn(0.14), emuIn(0.14), cap)
		textH := cardH
		if cardH >= emuIn(1.5) {
			barW := cardW - emuIn(0.36)
			barY := y + cardH - emuIn(0.14)
			rect(slide, x+emuIn(0.18), barY, barW, emuIn(0.045), blend(cap, theme.card, 0.80))
			rect(slide, x+emuIn(0.18), barY, int64(float64(barW)*ratios[i%len(ratios)]), emuIn(0.045), cap)
			textH = cardH - emuIn(0.12)
		}
		paintCardText(slide, theme, theme.card, x, y, cardW, textH, i, text, rows)
	}
}

func buildDefenseCards(slide *ppt.Slide, theme deckTheme, box contentBox, texts []string) {
	cols, rows, cardW, cardH, y0, gap := cardGrid(box, len(texts))
	for i, text := range texts {
		x, y := cardOrigin(box, cols, cardW, cardH, gap, y0, i)
		cap := theme.navy
		if len(texts) > 1 && i == len(texts)-1 {
			cap = theme.accent
		}
		rect(slide, x, y, cardW, cardH, theme.card)
		strokeFrame(slide, x, y, cardW, cardH, emuIn(0.012), blend(theme.navy, theme.card, 0.78))
		rect(slide, x, y, cardW, emuIn(0.085), cap)
		paintCardText(slide, theme, theme.card, x, y, cardW, cardH, i, text, rows)
	}
}

func buildOfficialCards(slide *ppt.Slide, theme deckTheme, box contentBox, texts []string) {
	cols, rows, cardW, cardH, y0, gap := cardGrid(box, len(texts))
	for i, text := range texts {
		x, y := cardOrigin(box, cols, cardW, cardH, gap, y0, i)
		rect(slide, x, y, cardW, cardH, theme.card)
		strokeFrame(slide, x, y, cardW, cardH, emuIn(0.015), blend(theme.gold, theme.card, 0.35))
		rect(slide, x, y, cardW, emuIn(0.07), theme.accent)
		rect(slide, x, y+emuIn(0.07), cardW, emuIn(0.025), theme.gold)
		paintCardText(slide, theme, theme.card, x, y, cardW, cardH, i, text, rows)
	}
}

func buildLessonCards(slide *ppt.Slide, theme deckTheme, box contentBox, texts []string) {
	cols, rows, cardW, cardH, y0, gap := cardGrid(box, len(texts))
	steps := []ppt.Color{theme.accent, theme.gold, theme.navy2}
	for i, text := range texts {
		x, y := cardOrigin(box, cols, cardW, cardH, gap, y0, i)
		step := steps[i%len(steps)]
		rect(slide, x, y, cardW, cardH, theme.card)
		strokeFrame(slide, x, y, cardW, cardH, emuIn(0.012), blend(theme.navy, theme.card, 0.80))
		oval(slide, x+emuIn(0.58), y+emuIn(0.16), emuIn(0.24), emuIn(0.24), step)
		rect(slide, x+emuIn(0.90), y+emuIn(0.26), emuIn(1.1), emuIn(0.045), step)
		paintCardText(slide, theme, theme.card, x, y, cardW, cardH, i, text, rows)
	}
}

func buildFilledCards(slide *ppt.Slide, theme deckTheme, box contentBox, texts []string) {
	cols, rows, cardW, cardH, y0, gap := cardGrid(box, len(texts))
	stripes := []ppt.Color{theme.accent, theme.navy2, theme.gold, theme.navy}
	for i, text := range texts {
		x, y := cardOrigin(box, cols, cardW, cardH, gap, y0, i)
		stripe := stripes[i%len(stripes)]
		rect(slide, x, y, cardW, cardH, theme.card)
		rect(slide, x, y, cardW, emuIn(0.075), stripe)
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
	deckText(slide, x+emuIn(0.18), y+emuIn(0.16), cardW-emuIn(0.36), emuIn(0.28), fmt.Sprintf("%02d", index+1), 12, true, readableAccent(theme, bg), ppt.HorizontalLeft)
	head, body := splitCardLine(text)
	if head != "" {
		deckText(slide, x+emuIn(0.18), textY, cardW-emuIn(0.36), emuIn(0.42), head, titleSize, true, textOn(theme.navy, bg, theme), ppt.HorizontalLeft)
		bodyTop := textY + emuIn(0.46)
		deckText(slide, x+emuIn(0.18), bodyTop, cardW-emuIn(0.36), remain(bodyTop, y, cardH, emuIn(0.1)), body, bodySize, false, textOn(theme.slate, bg, theme), ppt.HorizontalLeft)
		return
	}
	deckText(slide, x+emuIn(0.18), textY, cardW-emuIn(0.36), remain(textY, y, cardH, emuIn(0.1)), text, bodySize+1, false, textOn(theme.ink, bg, theme), ppt.HorizontalLeft)
}

func paintBulletRowChrome(slide *ppt.Slide, theme deckTheme, x, y, w, h int64, i int) (bg ppt.Color, padLeft int64) {
	stepColor := motifStepColor(theme, i, 6)
	switch theme.motif {
	case motifInk, motifConsulting:
		rect(slide, x, y+h-emuIn(0.014), w, emuIn(0.014), openPointRule(theme))
		rect(slide, x, y+emuIn(0.12), emuIn(0.045), h-emuIn(0.24), stepColor)
		return theme.paper, emuIn(0.18)
	case motifSketch:
		rect(slide, x, y, w, h, theme.card)
		strokeFrame(slide, x, y, w, h, emuIn(0.015), theme.ink)
		rect(slide, x, y, emuIn(0.08), h, stepColor)
		return theme.card, emuIn(0.22)
	case motifPoster:
		rect(slide, x+emuIn(0.04), y+emuIn(0.04), w, h, blend(theme.ink, theme.paper, 0.65))
		rect(slide, x, y, w, h, theme.card)
		strokeFrame(slide, x, y, w, h, emuIn(0.02), theme.ink)
		rect(slide, x, y, emuIn(0.14), h, stepColor)
		return theme.card, emuIn(0.26)
	case motifBoard:
		fill := noteFill(theme, i)
		rect(slide, x, y, w, h, fill)
		strokeFrame(slide, x, y, w, h, emuIn(0.028), stepColor)
		return fill, emuIn(0.22)
	case motifCraft:
		fill := noteFill(theme, i)
		roundRect(slide, x, y, w, h, fill)
		rect(slide, x+emuIn(0.12), y+emuIn(0.12), emuIn(0.06), h-emuIn(0.24), theme.gold)
		return fill, emuIn(0.28)
	case motifSplit:
		bg = cardSurfaceColor(theme)
		rect(slide, x, y, w, h, bg)
		rect(slide, x, y, emuIn(0.10), h, stepColor)
		return bg, emuIn(0.24)
	case motifOfficial:
		rect(slide, x, y, w, h, theme.card)
		strokeFrame(slide, x, y, w, h, emuIn(0.012), blend(theme.gold, theme.card, 0.35))
		rect(slide, x, y, emuIn(0.08), h, theme.accent)
		rect(slide, x+emuIn(0.08), y, emuIn(0.025), h, theme.gold)
		return theme.card, emuIn(0.24)
	default:
		bg = cardSurfaceColor(theme)
		rect(slide, x, y, w, h, bg)
		rect(slide, x, y, emuIn(0.08), h, stepColor)
		return bg, emuIn(0.22)
	}
}

func paintQuoteFrame(slide *ppt.Slide, theme deckTheme, box contentBox, markY, frameH int64) (bg ppt.Color, padX int64) {
	frameY := markY - emuIn(0.12)
	if frameY < box.y {
		frameY = box.y
	}
	if frameY+frameH > box.y+box.h {
		frameH = box.y + box.h - frameY
	}
	switch theme.motif {
	case motifConsulting, motifInk:
		rect(slide, box.x, frameY, emuIn(1.8), emuIn(0.025), theme.accent)
		rect(slide, box.x, frameY+frameH, box.w, emuIn(0.012), openPointRule(theme))
		return theme.paper, 0
	case motifPoster:
		rect(slide, box.x+emuIn(0.05), frameY+emuIn(0.05), box.w, frameH, blend(theme.ink, theme.paper, 0.65))
		rect(slide, box.x, frameY, box.w, frameH, theme.card)
		strokeFrame(slide, box.x, frameY, box.w, frameH, emuIn(0.024), theme.ink)
		rect(slide, box.x, frameY, emuIn(0.16), frameH, theme.accent)
		return theme.card, emuIn(0.32)
	case motifBoard:
		rect(slide, box.x, frameY, box.w, frameH, theme.card)
		strokeFrame(slide, box.x, frameY, box.w, frameH, emuIn(0.035), theme.accent)
		return theme.card, emuIn(0.28)
	case motifCraft:
		roundRect(slide, box.x, frameY, box.w, frameH, theme.card)
		rect(slide, box.x+box.w/2-emuIn(0.55), frameY-emuIn(0.06), emuIn(1.1), emuIn(0.18), blend(theme.gold, theme.white, 0.2))
		return theme.card, emuIn(0.28)
	case motifOfficial:
		rect(slide, box.x, frameY, box.w, frameH, theme.card)
		strokeFrame(slide, box.x, frameY, box.w, frameH, emuIn(0.018), theme.gold)
		rect(slide, box.x, frameY, emuIn(0.10), frameH, theme.accent)
		return theme.card, emuIn(0.28)
	default:
		bg = cardSurfaceColor(theme)
		rect(slide, box.x, frameY, box.w, frameH, bg)
		rect(slide, box.x, frameY, emuIn(0.10), frameH, theme.accent)
		return bg, emuIn(0.28)
	}
}

func paintAgendaRow(slide *ppt.Slide, theme deckTheme, x, y, w, rowH, numY, chipH int64, i, total int) (rowBg, chipColor ppt.Color) {
	chipColor = motifStepColor(theme, i, total)
	switch theme.motif {
	case motifPoster:
		rowBg = theme.card
		rect(slide, x+emuIn(0.04), y+emuIn(0.04), w, rowH, blend(theme.ink, theme.paper, 0.65))
		rect(slide, x, y, w, rowH, rowBg)
		strokeFrame(slide, x, y, w, rowH, emuIn(0.02), theme.ink)
		rect(slide, x+emuIn(0.16), numY, emuIn(0.72), chipH, chipColor)
		strokeFrame(slide, x+emuIn(0.16), numY, emuIn(0.72), chipH, emuIn(0.016), theme.ink)
		return rowBg, chipColor
	case motifBoard:
		rowBg = noteFill(theme, i)
		rect(slide, x, y, w, rowH, rowBg)
		strokeFrame(slide, x, y, w, rowH, emuIn(0.028), chipColor)
		rect(slide, x+emuIn(0.16), numY, emuIn(0.72), chipH, chipColor)
		return rowBg, chipColor
	case motifCraft:
		rowBg = noteFill(theme, i)
		roundRect(slide, x, y, w, rowH, rowBg)
		roundRect(slide, x+emuIn(0.16), numY, emuIn(0.72), chipH, theme.accent)
		return rowBg, theme.accent
	case motifLesson:
		rowBg = theme.card
		rect(slide, x, y, w, rowH, rowBg)
		strokeFrame(slide, x, y, w, rowH, emuIn(0.012), blend(theme.navy, rowBg, 0.80))
		roundRectPill(slide, x+emuIn(0.16), numY, emuIn(0.72), chipH, theme.accent)
		return rowBg, theme.accent
	case motifOfficial:
		rowBg = theme.card
		rect(slide, x, y, w, rowH, rowBg)
		strokeFrame(slide, x, y, w, rowH, emuIn(0.014), blend(theme.gold, rowBg, 0.35))
		rect(slide, x, y, emuIn(0.07), rowH, theme.gold)
		rect(slide, x+emuIn(0.16), numY, emuIn(0.72), chipH, theme.accent)
		return rowBg, theme.accent
	case motifSplit:
		rowBg = cardSurfaceColor(theme)
		rect(slide, x, y, w, rowH, rowBg)
		rect(slide, x, y, emuIn(0.08), rowH, theme.navy)
		rect(slide, x+emuIn(0.16), numY, emuIn(0.72), chipH, theme.accent)
		return rowBg, theme.accent
	default:
		rowBg = cardSurfaceColor(theme)
		rect(slide, x, y, w, rowH, rowBg)
		rect(slide, x, y, emuIn(0.06), rowH, chipColor)
		rect(slide, x+emuIn(0.16), numY, emuIn(0.72), chipH, theme.accent)
		return rowBg, theme.accent
	}
}

func paintKPICardFrame(slide *ppt.Slide, theme deckTheme, x, y, w, h int64, i, total int) (bg, accent ppt.Color) {
	accent = motifStepColor(theme, i, total)
	switch theme.motif {
	case motifPoster:
		bg = theme.card
		rect(slide, x+emuIn(0.05), y+emuIn(0.05), w, h, blend(theme.ink, theme.paper, 0.65))
		rect(slide, x, y, w, h, bg)
		rect(slide, x, y, w, emuIn(0.08), accent)
		strokeFrame(slide, x, y, w, h, emuIn(0.022), theme.ink)
		return bg, accent
	case motifSketch:
		bg = theme.card
		rect(slide, x, y, w, h, bg)
		strokeFrame(slide, x, y, w, h, emuIn(0.018), theme.ink)
		rect(slide, x, y, w, emuIn(0.065), accent)
		return bg, accent
	case motifBoard:
		bg = noteFill(theme, i)
		rect(slide, x, y, w, h, bg)
		strokeFrame(slide, x, y, w, h, emuIn(0.032), accent)
		return bg, accent
	case motifCraft:
		bg = noteFill(theme, i)
		roundRect(slide, x, y, w, h, bg)
		rect(slide, x+w/2-emuIn(0.38), y-emuIn(0.05), emuIn(0.76), emuIn(0.15), blend(theme.gold, theme.white, 0.2))
		return bg, accent
	case motifInk, motifConsulting:
		bg = cardSurfaceColor(theme)
		rect(slide, x, y, w, h, bg)
		rect(slide, x, y, w, emuIn(0.035), accent)
		rect(slide, x, y+h-emuIn(0.015), w, emuIn(0.015), openPointRule(theme))
		return bg, accent
	case motifSplit:
		bg = cardSurfaceColor(theme)
		rect(slide, x, y, w, h, bg)
		rect(slide, x, y, emuIn(0.10), h, accent)
		return bg, accent
	case motifOfficial:
		bg = theme.card
		rect(slide, x, y, w, h, bg)
		strokeFrame(slide, x, y, w, h, emuIn(0.015), blend(theme.gold, bg, 0.35))
		rect(slide, x, y, w, emuIn(0.07), theme.accent)
		rect(slide, x, y+emuIn(0.07), w, emuIn(0.025), theme.gold)
		return bg, theme.accent
	case motifDashboard:
		bg = theme.card
		rect(slide, x, y, w, h, bg)
		strokeFrame(slide, x, y, w, h, emuIn(0.012), blend(theme.slate, theme.paper, 0.75))
		rect(slide, x, y, w, emuIn(0.08), accent)
		if h >= emuIn(1.5) {
			barW := w - emuIn(0.44)
			barY := y + h - emuIn(0.20)
			rect(slide, x+emuIn(0.22), barY, barW, emuIn(0.055), blend(accent, bg, 0.80))
			rect(slide, x+emuIn(0.22), barY, barW*int64(60+(i*13)%32)/100, emuIn(0.055), accent)
		}
		return bg, accent
	default:
		bg = cardSurfaceColor(theme)
		rect(slide, x, y, w, h, bg)
		rect(slide, x, y, w, emuIn(0.075), accent)
		return bg, accent
	}
}

func paintTimelineNode(slide *ppt.Slide, theme deckTheme, x, y, d int64, color ppt.Color, i int) {
	badgeW := emuIn(0.68)
	badgeX := x - (badgeW-d)/2
	switch theme.motif {
	case motifPoster:
		rect(slide, badgeX, y, badgeW, d, color)
		strokeFrame(slide, badgeX, y, badgeW, d, emuIn(0.02), theme.ink)
	case motifBoard:
		rect(slide, badgeX, y, badgeW, d, theme.card)
		strokeFrame(slide, badgeX, y, badgeW, d, emuIn(0.028), color)
		color = theme.card
	case motifConsulting, motifInk:
		rect(slide, badgeX, y, badgeW, d, theme.paper)
		strokeFrame(slide, badgeX, y, badgeW, d, emuIn(0.015), openPointRule(theme))
		deckText(slide, badgeX, y+(d-emuIn(0.28))/2, badgeW, emuIn(0.28), fmt.Sprintf("%02d", i+1), 12, true, readableAccent(theme, theme.paper), ppt.HorizontalCenter)
		return
	case motifOfficial, motifReport, motifSplit:
		rect(slide, badgeX, y, badgeW, d, color)
	default:
		roundRectPill(slide, badgeX, y, badgeW, d, color)
	}
	numColor := bestContrast(color, theme.navy, theme.ink, theme.white)
	deckText(slide, badgeX, y+(d-emuIn(0.28))/2, badgeW, emuIn(0.28), fmt.Sprintf("%02d", i+1), 12, true, numColor, ppt.HorizontalCenter)
}

func paintTimelineCardFrame(slide *ppt.Slide, theme deckTheme, x, y, w, h int64, stepColor ppt.Color, i int) ppt.Color {
	switch theme.motif {
	case motifConsulting, motifInk:
		rect(slide, x, y, w, emuIn(0.025), stepColor)
		rect(slide, x, y+h-emuIn(0.015), w, emuIn(0.015), openPointRule(theme))
		return theme.paper
	case motifDashboard:
		rect(slide, x, y, w, h, theme.card)
		strokeFrame(slide, x, y, w, h, emuIn(0.012), blend(theme.slate, theme.paper, 0.75))
		rect(slide, x, y, w, emuIn(0.075), stepColor)
		return theme.card
	default:
		bg, _ := paintKPICardFrame(slide, theme, x, y, w, h, i, 6)
		return bg
	}
}

func paintComparisonCardFrame(slide *ppt.Slide, theme deckTheme, x, y, w, h int64, sideColor ppt.Color, col, i int) ppt.Color {
	switch theme.motif {
	case motifPoster:
		rect(slide, x+emuIn(0.04), y+emuIn(0.04), w, h, blend(theme.ink, theme.paper, 0.65))
		rect(slide, x, y, w, h, theme.card)
		rect(slide, x, y, w, emuIn(0.10), sideColor)
		strokeFrame(slide, x, y, w, h, emuIn(0.022), theme.ink)
		return theme.card
	case motifSketch:
		rect(slide, x, y, w, h, theme.card)
		strokeFrame(slide, x, y, w, h, emuIn(0.018), theme.ink)
		rect(slide, x, y, emuIn(0.09), h, sideColor)
		return theme.card
	case motifBoard:
		fill := noteFill(theme, col)
		rect(slide, x, y, w, h, fill)
		strokeFrame(slide, x, y, w, h, emuIn(0.032), sideColor)
		return fill
	case motifCraft:
		fill := noteFill(theme, col)
		roundRect(slide, x, y, w, h, fill)
		rect(slide, x, y, w, emuIn(0.08), sideColor)
		return fill
	case motifInk, motifConsulting:
		rect(slide, x, y, w, emuIn(0.03), sideColor)
		rect(slide, x, y+h-emuIn(0.015), w, emuIn(0.015), openPointRule(theme))
		return theme.paper
	default:
		bg := cardSurfaceColor(theme)
		rect(slide, x, y, w, h, bg)
		rect(slide, x, y, emuIn(0.10), h, sideColor)
		rect(slide, x, y, w, emuIn(0.05), sideColor)
		return bg
	}
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
