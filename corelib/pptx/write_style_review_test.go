package pptx

import (
	"path/filepath"
	"strings"
	"testing"
)

// Radar series are stroked: PowerPoint reads the series colour from
// <c:spPr><a:ln>, and a bare <a:solidFill> there only colours the markers.
// Without an explicit outline the radar web keeps Office's default blue.
func TestRadarSeriesCarriesBrandStroke(t *testing.T) {
	path := filepath.Join(t.TempDir(), "radar.pptx")
	outline := Outline{
		Theme: "consulting",
		Slides: []OutlineSlide{{
			Title: "能力对照",
			Charts: []OutlineChart{{
				ChartType:  "radar",
				Categories: []string{"延迟", "覆盖", "成本"},
				Series: []OutlineChartSeries{
					{Name: "现状", Values: []float64{3, 4, 2}},
					{Name: "目标", Values: []float64{5, 5, 4}},
				},
			}},
		}},
	}
	if err := WriteFile(path, outline); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	radar := chartPartXML(t, path, "<c:radarChart")
	if radar == "" {
		t.Fatal("missing radar chart part")
	}
	if !strings.Contains(radar, "<a:ln") {
		t.Fatal("radar series carries no <a:ln> stroke: the web keeps Office default blue")
	}
	if !strings.Contains(radar, `val="061F32"`) {
		t.Fatal("radar series stroke is not the deck navy 061F32")
	}
}

// Line series are stroked too; the palette recolour must survive the
// line-chart write path (FillColor maps onto <a:ln> there).
func TestLineSeriesCarriesBrandStroke(t *testing.T) {
	path := filepath.Join(t.TempDir(), "line.pptx")
	outline := Outline{
		Theme: "consulting",
		Slides: []OutlineSlide{{
			Title: "增长曲线",
			Charts: []OutlineChart{{
				ChartType:  "line",
				Categories: []string{"Q1", "Q2", "Q3"},
				Series:     []OutlineChartSeries{{Name: "收入", Values: []float64{1, 2, 3}}},
			}},
		}},
	}
	if err := WriteFile(path, outline); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	line := chartPartXML(t, path, "<c:lineChart")
	if line == "" {
		t.Fatal("missing line chart part")
	}
	if !strings.Contains(line, "<a:ln") || !strings.Contains(line, `val="061F32"`) {
		t.Fatal("line series stroke is missing or not the deck navy 061F32")
	}
}

// The consulting divider's giant index is a section number, not a page
// number: the first section of a titled deck sits on page 3 and must read 01.
func TestConsultingSectionDividerNumbersSections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sections.pptx")
	outline := Outline{
		Title: "增长战略",
		Slides: []OutlineSlide{
			{Title: "背景", Layout: "bullets", Bullets: []string{"市场增速放缓"}},
			{Title: "三大增长引擎", Layout: "section", Kicker: "PART"},
			{Title: "落地路径", Layout: "bullets", Bullets: []string{"先做试点"}},
		},
	}
	if err := WriteFile(path, outline); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	files := zipNamedContents(t, path)
	divider := ""
	for name, body := range files {
		if strings.HasPrefix(name, "ppt/slides/slide") && strings.Contains(body, "三大增长引擎") {
			divider = body
		}
	}
	if divider == "" {
		t.Fatal("missing consulting divider slide")
	}
	if !strings.Contains(divider, "<a:t>01</a:t>") {
		t.Fatal("divider giant index is not the section number 01")
	}
	if strings.Contains(divider, "<a:t>03</a:t>") {
		t.Fatal("divider still numbers by page (03) instead of by section")
	}
}

func chartPartXML(t *testing.T, path, marker string) string {
	t.Helper()
	for name, body := range zipNamedContents(t, path) {
		if strings.HasPrefix(name, "ppt/charts/chart") && strings.Contains(body, marker) {
			return body
		}
	}
	return ""
}
