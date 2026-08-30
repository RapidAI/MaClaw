package pptx

import (
	"archive/zip"
	"io"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestWriteFileEmbedsNativeBarAndRadarCharts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "charts.pptx")
	outline := Outline{
		Title: "证据",
		Slides: []OutlineSlide{
			{
				Title:   "公开证据快照",
				Bullets: []string{"Stars / Forks 是公开对照"},
				Charts: []OutlineChart{{
					ChartType:  "bar",
					Title:      "GitHub",
					Categories: []string{"Stars", "Forks"},
					Series:     []OutlineChartSeries{{Name: "公开", Values: []float64{139, 26}}},
				}},
			},
			{
				Title: "外包 / RPA / 数字员工",
				Charts: []OutlineChart{{
					ChartType:  "radar",
					Title:      "能力对照",
					Categories: []string{"延迟", "覆盖", "成本"},
					Series: []OutlineChartSeries{
						{Name: "外包", Values: []float64{3, 4, 2}},
						{Name: "数字员工", Values: []float64{5, 5, 4}},
					},
				}},
			},
		},
	}
	if err := WriteFile(path, outline); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// GoPPT's reader does not reconstruct ChartShape from XML; native charts
	// are proven by the OOXML chart parts PowerPoint actually edits.
	files := zipNamedContents(t, path)
	if _, ok := files["ppt/charts/chart1.xml"]; !ok {
		t.Fatalf("missing native chart part; zip=%v", zipNames(files))
	}
	if _, ok := files["ppt/charts/chart2.xml"]; !ok {
		t.Fatalf("missing second native chart part; zip=%v", zipNames(files))
	}
	joined := files["ppt/charts/chart1.xml"] + files["ppt/charts/chart2.xml"]
	for _, want := range []string{"<c:barChart", "<c:radarChart", "139", "Stars", "数字员工"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("chart XML missing %q", want)
		}
	}
	slideXML := ""
	for name, body := range files {
		if strings.HasPrefix(name, "ppt/slides/slide") && strings.HasSuffix(name, ".xml") {
			slideXML += body
		}
	}
	if !strings.Contains(slideXML, "drawingml/2006/chart") {
		t.Fatal("slide XML does not reference a native chart graphic")
	}
}

func zipNamedContents(t *testing.T, path string) map[string]string {
	t.Helper()
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer r.Close()
	out := make(map[string]string, len(r.File))
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		body, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read %s: %v", f.Name, err)
		}
		out[f.Name] = string(body)
	}
	return out
}

func zipNames(files map[string]string) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func TestValidateSlideChartsRejectsMalformedInput(t *testing.T) {
	if err := ValidateSlideCharts(nil); err != nil {
		t.Fatalf("empty charts must pass: %v", err)
	}
	tooMany := make([]OutlineChart, MaxSlideCharts+1)
	for i := range tooMany {
		tooMany[i] = OutlineChart{
			ChartType:  "bar",
			Categories: []string{"a"},
			Series:     []OutlineChartSeries{{Name: "s", Values: []float64{1}}},
		}
	}
	if err := ValidateSlideCharts(tooMany); err == nil || !strings.Contains(err.Error(), "pptx_slide_charts_too_many") {
		t.Fatalf("too many charts: %v", err)
	}
	if err := ValidateSlideCharts([]OutlineChart{{ChartType: "spaghetti", Categories: []string{"a"}, Series: []OutlineChartSeries{{Name: "s", Values: []float64{1}}}}}); err == nil || !strings.Contains(err.Error(), "pptx_chart_type_unsupported") {
		t.Fatalf("unsupported type: %v", err)
	}
	if err := ValidateSlideCharts([]OutlineChart{{ChartType: "bar", Categories: []string{"a"}, Series: []OutlineChartSeries{{Name: "s", Values: []float64{1, 2}}}}}); err == nil || !strings.Contains(err.Error(), "pptx_chart_series_length_mismatch") {
		t.Fatalf("length mismatch: %v", err)
	}
	if err := ValidateSlideCharts([]OutlineChart{{ChartType: "pie", Categories: []string{"a", "b"}, Series: []OutlineChartSeries{
		{Name: "one", Values: []float64{1, 2}},
		{Name: "two", Values: []float64{3, 4}},
	}}}); err == nil || !strings.Contains(err.Error(), "pptx_chart_pie_single_series") {
		t.Fatalf("pie extra series: %v", err)
	}
	if err := ValidateSlideCharts([]OutlineChart{{ChartType: "bar", Categories: []string{"a"}, Series: []OutlineChartSeries{{Name: "s", Values: []float64{math.Inf(1)}}}}}); err == nil || !strings.Contains(err.Error(), "pptx_chart_value_invalid") {
		t.Fatalf("non-finite: %v", err)
	}
	if err := ValidateSlideCharts([]OutlineChart{{ChartType: "柱状图", Categories: []string{"a"}, Series: []OutlineChartSeries{{Name: "s", Values: []float64{1}}}}}); err != nil {
		t.Fatalf("chinese column alias: %v", err)
	}
	if err := ValidateSlideCharts([]OutlineChart{{ChartType: "雷达图", Categories: []string{"a", "b"}, Series: []OutlineChartSeries{{Name: "s", Values: []float64{1, 2}}}}}); err != nil {
		t.Fatalf("chinese radar alias: %v", err)
	}
	if err := ValidateSlideCharts([]OutlineChart{{ChartType: "bar chart", Categories: []string{"a"}, Series: []OutlineChartSeries{{Name: "s", Values: []float64{1}}}}}); err != nil {
		t.Fatalf("bar chart alias: %v", err)
	}
	if err := ValidateSlideCharts([]OutlineChart{{ChartType: "bar-chart", Categories: []string{"a"}, Series: []OutlineChartSeries{{Name: "s", Values: []float64{1}}}}}); err != nil {
		t.Fatalf("bar-chart alias: %v", err)
	}
	if err := ValidateSlideCharts([]OutlineChart{{ChartType: "radar_chart", Categories: []string{"a"}, Series: []OutlineChartSeries{{Name: "s", Values: []float64{1}}}}}); err != nil {
		t.Fatalf("radar_chart alias: %v", err)
	}
}

func TestWriteFileChineseChartTypeWritesBarChart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "zh.pptx")
	err := WriteFile(path, Outline{Slides: []OutlineSlide{{
		Title: "公开证据",
		Charts: []OutlineChart{{
			ChartType:  "柱状图",
			Categories: []string{"Stars", "Forks"},
			Series:     []OutlineChartSeries{{Name: "公开", Values: []float64{139, 26}}},
		}},
	}}})
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	files := zipNamedContents(t, path)
	body := files["ppt/charts/chart1.xml"]
	if !strings.Contains(body, "<c:barChart") || !strings.Contains(body, "139") {
		t.Fatalf("柱状图 must emit a native bar chart, got %s", body)
	}
}

func TestWriteFileEmbedsHorizontalBarAndPie(t *testing.T) {
	path := filepath.Join(t.TempDir(), "more.pptx")
	err := WriteFile(path, Outline{Slides: []OutlineSlide{
		{Title: "条形", Charts: []OutlineChart{{
			ChartType:  "bar_h",
			Categories: []string{"L0", "L4"},
			Series:     []OutlineChartSeries{{Name: "成熟度", Values: []float64{1, 5}}},
		}}},
		{Title: "构成", Charts: []OutlineChart{{
			ChartType:  "pie",
			Categories: []string{"外包", "自研"},
			Series:     []OutlineChartSeries{{Name: "占比", Values: []float64{30, 70}}},
		}}},
	}})
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	files := zipNamedContents(t, path)
	joined := files["ppt/charts/chart1.xml"] + files["ppt/charts/chart2.xml"]
	for _, want := range []string{`<c:barDir val="bar"/>`, "<c:pieChart", "成熟度", "自研"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("chart XML missing %q", want)
		}
	}
}

func TestWriteFileRejectsInvalidChartBeforeWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.pptx")
	err := WriteFile(path, Outline{Slides: []OutlineSlide{{
		Title: "t",
		Charts: []OutlineChart{{
			ChartType:  "nope",
			Categories: []string{"a"},
			Series:     []OutlineChartSeries{{Name: "s", Values: []float64{1}}},
		}},
	}}})
	if err == nil || !strings.Contains(err.Error(), "pptx_chart_type_unsupported") {
		t.Fatalf("invalid chart must fail closed: %v", err)
	}
}
