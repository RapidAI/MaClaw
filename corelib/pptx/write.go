package pptx

import (
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"strings"

	ppt "github.com/Vantagics/GoPPT"
)

// Outline is the JSON contract for native deck generation. It matches the
// builtin pptx-gen skill's build_pptx.py input so either backend accepts the
// same outline document.
type Outline struct {
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
	// Theme is a catalog id (business, consulting, executive, academic, warm,
	// launch, tech, education, ceremony, minimal) or auto. Empty and auto
	// select from Purpose, title, and subtitle. An explicit id wins over that
	// text.
	Theme string `json:"theme,omitempty"`
	// Purpose is the deck's job in the user's words, used when Theme is auto.
	Purpose string         `json:"purpose,omitempty"`
	Slides  []OutlineSlide `json:"slides"`
}

// OutlineSlide is one content slide: a title, bullet lines, optional speaker
// notes, optional embedded images, and optional native PowerPoint charts.
type OutlineSlide struct {
	Title   string         `json:"title"`
	Bullets []string       `json:"bullets"`
	Notes   string         `json:"notes,omitempty"`
	Images  []OutlineImage `json:"images,omitempty"`
	Charts  []OutlineChart `json:"charts,omitempty"`
	// Kicker is the small eyebrow above the title.
	Kicker string `json:"kicker,omitempty"`
	// Layout is auto, bullets, cards, section, agenda, kpi, quote, or closing.
	// Empty means auto: a title-only page becomes a section divider, and two
	// to four short points become cards. Photos and charts keep the split
	// bullet layout so their geometry stays readable.
	Layout string `json:"layout,omitempty"`
}

// OutlineImage embeds one image file on a slide. Path is a local file (the
// caller resolves workspace-relative names before rendering). Width/Height
// are optional explicit sizes in inches; when omitted the image is fitted
// into its layout slot preserving the source aspect ratio.
type OutlineImage struct {
	Path   string  `json:"path"`
	Width  float64 `json:"width,omitempty"`
	Height float64 `json:"height,omitempty"`
}

// OutlineChart is a native editable PowerPoint chart object, not a picture.
// ChartType is bar/column, bar_h, line, radar, pie, or area. Pie takes exactly
// one series; every series Values length must match Categories.
type OutlineChart struct {
	ChartType  string               `json:"chart_type"`
	Title      string               `json:"title,omitempty"`
	Categories []string             `json:"categories"`
	Series     []OutlineChartSeries `json:"series"`
}

// OutlineChartSeries is one named data series on a chart.
type OutlineChartSeries struct {
	Name   string    `json:"name"`
	Values []float64 `json:"values"`
}

// defaultDeckFont renders Chinese well on Windows/macOS. NameEA must be set
// alongside Name: PowerPoint picks the East Asian typeface per run.
const defaultDeckFont = "Microsoft YaHei"

// WriteFile renders outline as a .pptx deck at path, creating the parent
// directory when needed. Generation is fully native (archive/zip + OOXML via
// GoPPT): no Python, no pip install, no bash step.
func WriteFile(path string, outline Outline) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("pptx_write_path_required")
	}
	if len(outline.Slides) == 0 && strings.TrimSpace(outline.Title) == "" {
		return fmt.Errorf("pptx_outline_empty: title 与 slides 不能同时为空")
	}
	if !strings.EqualFold(filepath.Ext(path), ".pptx") {
		path += ".pptx"
	}

	p := ppt.New()
	p.GetLayout().SetLayout(ppt.LayoutScreen16x9)
	theme := chooseOutlineTheme(outline)
	// ppt.New() starts with one blank slide: the title slide claims it, and an
	// outline without a title reuses it for the first content slide instead of
	// shipping a blank first page.
	first := p.GetActiveSlide()
	firstUsed := false
	total := len(outline.Slides)
	page := 1
	deckTitle := sanitizeXMLText(strings.TrimSpace(outline.Title))
	if deckTitle != "" {
		p.GetDocumentProperties().Title = deckTitle
		total++
		buildTitleSlide(first, outline, theme, total)
		firstUsed = true
		page = 2
	}
	footer := deckFooterLabel(deckTitle)
	// section counts real divider pages so the consulting divider's giant
	// index reads as a section number (01, 02, …) instead of a page number.
	section := 0
	for i, spec := range outline.Slides {
		slide := first
		if firstUsed {
			slide = p.CreateSlide()
		}
		firstUsed = true
		if effectiveLayout(spec) == "section" {
			section++
		}
		if err := buildContentSlide(slide, spec, theme, page+i, total, footer, section); err != nil {
			return err
		}
	}

	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("pptx_write_mkdir: %v", err)
		}
	}
	w, err := ppt.NewWriter(p, ppt.WriterPowerPoint2007)
	if err != nil {
		return fmt.Errorf("pptx_writer_unavailable: %v", err)
	}
	pw, ok := w.(*ppt.PPTXWriter)
	if !ok {
		return fmt.Errorf("pptx_writer_unavailable: unexpected writer %T", w)
	}
	if err := pw.Save(path); err != nil {
		return fmt.Errorf("pptx_write_failed: %v", err)
	}
	return nil
}

// 16x9 slide canvas in EMU: 12192000 x 6858000.
const (
	deckSlideWidth  = 12192000
	deckSlideHeight = 6858000
	deckMarginX     = 457200 // 0.5"
	deckGutter      = 91440  // 0.1"
)

func buildContentSlide(slide *ppt.Slide, spec OutlineSlide, theme deckTheme, page, total int, footer string, section int) error {
	if slide == nil {
		return nil
	}
	if err := ValidateSlideCharts(spec.Charts); err != nil {
		return err
	}
	switch effectiveLayout(spec) {
	case "section":
		buildSectionSlide(slide, spec, theme, page, total, section)
	case "closing":
		buildClosingSlide(slide, spec, theme, page, total)
	case "quote":
		buildQuoteSlide(slide, spec, theme, footer, page, total)
	case "agenda":
		box := paintLightChrome(slide, theme, spec.Kicker, spec.Title, footer, page, total)
		buildAgenda(slide, spec, theme, box)
	case "kpi":
		box := paintLightChrome(slide, theme, spec.Kicker, spec.Title, footer, page, total)
		buildKPI(slide, spec, theme, box)
	case "cards":
		box := paintLightChrome(slide, theme, spec.Kicker, spec.Title, footer, page, total)
		buildCards(slide, spec, theme, box)
	default:
		box := paintLightChrome(slide, theme, spec.Kicker, spec.Title, footer, page, total)
		region := computeSlideContentLayout(spec, box)
		buildBulletBody(slide, contentBox{x: region.bulletX, y: region.bulletY, w: region.bulletW, h: region.bulletH}, spec.Bullets, theme)
		if err := buildSlideImages(slide, spec.Images, region.imageX, region.imageY, region.imageW, region.imageH); err != nil {
			return err
		}
		if err := buildSlideCharts(slide, spec.Charts, region.chartX, region.chartY, region.chartW, region.chartH, theme); err != nil {
			return err
		}
	}
	if notes := sanitizeXMLText(strings.TrimSpace(spec.Notes)); notes != "" {
		slide.SetNotes(notes)
	}
	return nil
}

// slideContentLayout is the content-region split for bullets, images, and
// native charts. Charts take the right/bottom share so a title+bullets+chart
// slide stays readable instead of stacking three full-width bands.
type slideContentLayout struct {
	bulletX, bulletY, bulletW, bulletH int64
	imageX, imageY, imageW, imageH     int64
	chartX, chartY, chartW, chartH     int64
}

func computeSlideContentLayout(spec OutlineSlide, box contentBox) slideContentLayout {
	contentY := box.y
	contentHeight := box.h
	contentWidth := box.w
	layout := slideContentLayout{
		bulletX: box.x, bulletY: contentY, bulletW: contentWidth, bulletH: contentHeight,
		imageX: box.x, imageY: contentY, imageW: contentWidth, imageH: contentHeight,
		chartX: box.x, chartY: contentY, chartW: contentWidth, chartH: contentHeight,
	}
	hasBullets := slideHasTextBullets(spec)
	hasImages := len(spec.Images) > 0
	hasCharts := len(spec.Charts) > 0
	switch {
	case hasCharts && hasBullets:
		layout.bulletW = contentWidth * 38 / 100
		layout.chartX = box.x + layout.bulletW + deckGutter
		layout.chartW = contentWidth - layout.bulletW - deckGutter
		if hasImages {
			layout.imageH = contentHeight * 32 / 100
			layout.imageY = contentY + contentHeight - layout.imageH
			layout.bulletH = layout.imageY - contentY - deckGutter
			layout.chartH = layout.bulletH
		}
	case hasCharts && hasImages:
		layout.imageH = contentHeight * 36 / 100
		layout.imageY = contentY
		layout.chartY = layout.imageY + layout.imageH + deckGutter
		layout.chartH = contentHeight - layout.imageH - deckGutter
	case hasImages && hasBullets:
		// Side by side: a photo under a text block shrinks to a thumbnail.
		layout.imageW = contentWidth * 46 / 100
		layout.imageX = box.x + contentWidth - layout.imageW
		layout.imageY = contentY
		layout.imageH = contentHeight
		layout.bulletW = contentWidth - layout.imageW - deckGutter
		layout.bulletH = contentHeight
	}
	return layout
}

func slideHasTextBullets(spec OutlineSlide) bool {
	for _, bullet := range spec.Bullets {
		if strings.TrimSpace(bullet) != "" {
			return true
		}
	}
	return false
}

// MaxSlideImages caps embedded images per slide. The managed office schema
// uses the same ceiling as maxItems.
const MaxSlideImages = 4

// Chart payload ceilings. The managed office schema repeats these as maxItems
// hints; ValidateSlideCharts is the actual enforcement.
const (
	MaxSlideCharts     = 2
	MaxChartCategories = 24
	MaxChartSeries     = 8
	maxChartTitleRunes = 80
)

// ValidateSlideCharts rejects a malformed native-chart payload. Shared by the
// writer and the managed office canonicalizer so a bad chart never consumes
// the office grant.
func ValidateSlideCharts(charts []OutlineChart) error {
	if len(charts) == 0 {
		return nil
	}
	if len(charts) > MaxSlideCharts {
		return fmt.Errorf("pptx_slide_charts_too_many: %d > %d", len(charts), MaxSlideCharts)
	}
	for i, chart := range charts {
		if err := validateOutlineChart(chart, i); err != nil {
			return err
		}
	}
	return nil
}

func validateOutlineChart(chart OutlineChart, index int) error {
	kind := normalizeChartType(chart.ChartType)
	if kind == "" {
		return fmt.Errorf("pptx_chart_type_unsupported: chart %d type %q (use bar, column, bar_h, line, radar, pie, or area)", index+1, strings.TrimSpace(chart.ChartType))
	}
	if title := sanitizeXMLText(strings.TrimSpace(chart.Title)); len([]rune(title)) > maxChartTitleRunes {
		return fmt.Errorf("pptx_chart_title_too_long: chart %d", index+1)
	}
	if len(chart.Categories) == 0 {
		return fmt.Errorf("pptx_chart_categories_required: chart %d", index+1)
	}
	if len(chart.Categories) > MaxChartCategories {
		return fmt.Errorf("pptx_chart_categories_too_many: chart %d %d > %d", index+1, len(chart.Categories), MaxChartCategories)
	}
	seen := make(map[string]bool, len(chart.Categories))
	for _, cat := range chart.Categories {
		label := sanitizeXMLText(strings.TrimSpace(cat))
		if label == "" {
			return fmt.Errorf("pptx_chart_category_empty: chart %d", index+1)
		}
		if seen[label] {
			return fmt.Errorf("pptx_chart_category_duplicate: chart %d %q", index+1, label)
		}
		seen[label] = true
	}
	if len(chart.Series) == 0 {
		return fmt.Errorf("pptx_chart_series_required: chart %d", index+1)
	}
	if kind == "pie" && len(chart.Series) != 1 {
		return fmt.Errorf("pptx_chart_pie_single_series: chart %d", index+1)
	}
	if len(chart.Series) > MaxChartSeries {
		return fmt.Errorf("pptx_chart_series_too_many: chart %d %d > %d", index+1, len(chart.Series), MaxChartSeries)
	}
	for s, series := range chart.Series {
		if sanitizeXMLText(strings.TrimSpace(series.Name)) == "" {
			return fmt.Errorf("pptx_chart_series_name_required: chart %d series %d", index+1, s+1)
		}
		if len(series.Values) != len(chart.Categories) {
			return fmt.Errorf("pptx_chart_series_length_mismatch: chart %d series %d values=%d categories=%d", index+1, s+1, len(series.Values), len(chart.Categories))
		}
		for _, value := range series.Values {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return fmt.Errorf("pptx_chart_value_invalid: chart %d series %d", index+1, s+1)
			}
		}
	}
	return nil
}

func normalizeChartType(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.TrimSuffix(s, " chart")
	s = strings.TrimSuffix(s, "_chart")
	s = strings.TrimSuffix(s, "图")
	s = strings.TrimSuffix(s, "圖")
	switch s {
	case "bar", "column", "col", "bar clustered", "column clustered", "bar_clustered", "column_clustered", "柱状", "柱形", "柱狀":
		return "column"
	case "bar_h", "bar h", "horizontal bar", "horizontal_bar", "bar_horizontal", "条形", "條形":
		return "bar_h"
	case "line", "折线", "折線":
		return "line"
	case "radar", "雷达", "雷達":
		return "radar"
	case "pie", "饼", "餅":
		return "pie"
	case "area", "面积", "面積":
		return "area"
	default:
		return ""
	}
}

func buildSlideCharts(slide *ppt.Slide, charts []OutlineChart, x, y, width, height int64, theme deckTheme) error {
	if len(charts) == 0 {
		return nil
	}
	if width <= 0 || height <= 0 {
		return fmt.Errorf("pptx_slide_charts_no_room")
	}
	slot := width / int64(len(charts))
	for i, spec := range charts {
		shape := slide.CreateChartShape()
		shape.SetWidth(slot - 2*deckGutter).SetHeight(height)
		shape.SetOffsetX(x + int64(i)*slot + deckGutter)
		shape.SetOffsetY(y)
		if title := sanitizeXMLText(strings.TrimSpace(spec.Title)); title != "" {
			if ct := shape.GetTitle(); ct != nil {
				if ct.Font == nil {
					ct.Font = ppt.NewFont()
				}
				ct.SetText(title).SetVisible(true)
				ct.Font.SetName(defaultDeckFont).SetSize(14).SetBold(true)
				ct.Font.NameEA = defaultDeckFont
			}
		} else if ct := shape.GetTitle(); ct != nil {
			ct.SetVisible(false)
		}
		categories := make([]string, len(spec.Categories))
		for c, cat := range spec.Categories {
			categories[c] = sanitizeXMLText(strings.TrimSpace(cat))
		}
		chartType, err := newPlotChart(spec, categories, theme)
		if err != nil {
			return err
		}
		shape.GetPlotArea().SetType(chartType)
	}
	return nil
}

func outlineChartSeries(spec OutlineChart, categories []string, theme deckTheme) []*ppt.ChartSeries {
	out := make([]*ppt.ChartSeries, 0, len(spec.Series))
	kind := normalizeChartType(spec.ChartType)
	mono := kind == "pie"
	for i, series := range spec.Series {
		cs := ppt.NewChartSeriesOrdered(
			sanitizeXMLText(strings.TrimSpace(series.Name)),
			categories,
			series.Values,
		)
		if !mono {
			// Native charts ship with Office's default blue; recolor each
			// series from the deck palette so charts read as part of the
			// style. Pie stays multi-color or every slice collapses to one.
			color := chartSeriesColors(theme, i)
			cs.SetFillColor(color)
			if kind == "radar" {
				// PowerPoint strokes a radar series from <c:spPr><a:ln>;
				// the fill only colours its markers. Without the outline
				// the radar web keeps Office's default blue.
				cs.Outline = &ppt.SeriesOutline{Width: 2, Color: color}
			}
		}
		out = append(out, cs)
	}
	return out
}

func newPlotChart(spec OutlineChart, categories []string, theme deckTheme) (ppt.ChartType, error) {
	series := outlineChartSeries(spec, categories, theme)
	kind := normalizeChartType(spec.ChartType)
	switch kind {
	case "column", "bar_h":
		bar := ppt.NewBarChart()
		bar.SetBarGrouping(ppt.BarGroupingClustered)
		if kind == "bar_h" {
			bar.BarDirection = ppt.BarDirectionHorizontal
		}
		for _, item := range series {
			bar.AddSeries(item)
		}
		return bar, nil
	case "line":
		line := ppt.NewLineChart()
		for _, item := range series {
			line.AddSeries(item)
		}
		return line, nil
	case "radar":
		radar := ppt.NewRadarChart()
		for _, item := range series {
			radar.AddSeries(item)
		}
		return radar, nil
	case "pie":
		if len(series) == 0 {
			return nil, fmt.Errorf("pptx_chart_series_required")
		}
		pie := ppt.NewPieChart()
		pie.AddSeries(series[0])
		return pie, nil
	case "area":
		area := ppt.NewAreaChart()
		for _, item := range series {
			area.AddSeries(item)
		}
		return area, nil
	default:
		return nil, fmt.Errorf("pptx_chart_type_unsupported: type %q", strings.TrimSpace(spec.ChartType))
	}
}

// deckInchEMU converts inches to English Metric Units (1" = 914400 EMU).
const deckInchEMU = 914400

// buildSlideImages lays out images in a left-to-right row inside the region
// starting at y with the given height. Each image keeps its source aspect
// ratio unless the outline pins an explicit inch size. A missing or unreadable
// image file fails the whole write: the model must see the failure and fix
// the path instead of shipping a deck with silent gaps.
func buildSlideImages(slide *ppt.Slide, images []OutlineImage, x, y, contentWidth, height int64) error {
	if len(images) == 0 {
		return nil
	}
	if len(images) > MaxSlideImages {
		return fmt.Errorf("pptx_slide_images_too_many: %d > %d", len(images), MaxSlideImages)
	}
	if height <= 0 || contentWidth <= 0 {
		return fmt.Errorf("pptx_slide_images_no_room")
	}
	slot := contentWidth / int64(len(images))
	for i, spec := range images {
		path := strings.TrimSpace(spec.Path)
		if path == "" {
			return fmt.Errorf("pptx_slide_image_path_required: slide image %d", i+1)
		}
		shape, err := slide.AddImage(path)
		if err != nil {
			return fmt.Errorf("pptx_slide_image_unreadable: %s: %v", path, err)
		}
		width, h := imageBoxEMU(path, spec, slot-2*deckGutter, height)
		shape.SetWidth(width).SetHeight(h)
		shape.SetOffsetX(x + int64(i)*slot + (slot-width)/2)
		shape.SetOffsetY(y + (height-h)/2)
	}
	return nil
}

// imageBoxEMU resolves the rendered size of one image. Explicit inch
// dimensions win; otherwise the source aspect ratio is decoded from the file
// header and the image is fitted inside (maxW, maxH). Undecodable formats
// fall back to a 4:3 box.
func imageBoxEMU(path string, spec OutlineImage, maxW, maxH int64) (int64, int64) {
	if spec.Width > 0 || spec.Height > 0 {
		w := int64(spec.Width * deckInchEMU)
		h := int64(spec.Height * deckInchEMU)
		if w <= 0 && h > 0 {
			w = h * 4 / 3
		}
		if h <= 0 && w > 0 {
			h = w * 3 / 4
		}
		return w, h
	}
	aspectW, aspectH := int64(4), int64(3)
	if f, err := os.Open(path); err == nil {
		cfg, _, cfgErr := image.DecodeConfig(f)
		_ = f.Close()
		if cfgErr == nil && cfg.Width > 0 && cfg.Height > 0 {
			aspectW, aspectH = int64(cfg.Width), int64(cfg.Height)
		}
	}
	w, h := maxW, maxW*aspectH/aspectW
	if h > maxH {
		h = maxH
		w = maxH * aspectW / aspectH
	}
	return w, h
}

// sanitizeXMLText drops characters outside the XML 1.0Char set. PowerPoint's
// XML parts reject them outright, and Go regexp cannot express the surrogate
// range, so filter runes directly.
func sanitizeXMLText(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == 0x9 || r == 0xA || r == 0xD:
			return r
		case r >= 0x20 && r <= 0xD7FF:
			return r
		case r >= 0xE000 && r <= 0xFFFD:
			return r
		case r >= 0x10000 && r <= 0x10FFFF:
			return r
		default:
			return -1
		}
	}, s)
}
