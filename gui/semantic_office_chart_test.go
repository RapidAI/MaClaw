package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTrustedOfficeWriteArgsAllowedKeepsSlideCharts(t *testing.T) {
	args := map[string]interface{}{
		"path": "deck.pptx",
		"slides": []interface{}{
			map[string]interface{}{
				"title":   "公开证据",
				"bullets": []interface{}{"Stars / Forks"},
				"charts": []interface{}{
					map[string]interface{}{
						"chart_type": "bar",
						"title":      "GitHub",
						"categories": []interface{}{"Stars", "Forks"},
						"series": []interface{}{
							map[string]interface{}{"name": "公开", "values": []interface{}{139, 26}},
						},
					},
				},
			},
		},
	}
	path, data, err := semanticTrustedOfficeWriteArgsAllowed(args)
	if err != nil {
		t.Fatalf("admission rejected slide charts: %v", err)
	}
	if path != "deck.pptx" {
		t.Fatalf("path lost: %q", path)
	}
	slides, ok := data["slides"].([]interface{})
	if !ok || len(slides) != 1 {
		t.Fatalf("slides lost: %#v", data["slides"])
	}
	slide := slides[0].(map[string]interface{})
	charts, ok := slide["charts"].([]interface{})
	if !ok || len(charts) != 1 {
		t.Fatalf("charts stripped by admission: %#v", slide)
	}
}

func TestSemanticOfficeSlideChartCheckIsPreExecution(t *testing.T) {
	good := `{"path":"deck.pptx","slides":[{"title":"t","charts":[{"chart_type":"radar","categories":["a","b"],"series":[{"name":"s","values":[1,2]}]}]}]}`
	if err := semanticOfficeSlideChartCheck(good); err != nil {
		t.Fatalf("valid chart must pass: %v", err)
	}
	if err := semanticOfficeSlideChartCheck(`{"path":"deck.pptx","slides":[{"title":"t"}]}`); err != nil {
		t.Fatalf("slides without charts must pass: %v", err)
	}
	if err := semanticOfficeSlideChartCheck(`{"path":"book.xlsx","sheets":[]}`); err != nil {
		t.Fatalf("spreadsheet form must pass: %v", err)
	}

	err := semanticOfficeSlideChartCheck(`{"path":"deck.pptx","slides":[{"title":"t","charts":[{"chart_type":"spaghetti","categories":["a"],"series":[{"name":"s","values":[1]}]}]}]}`)
	if err == nil || !strings.Contains(err.Error(), "pptx_chart_type_unsupported") {
		t.Fatalf("unsupported type must be rejected: %v", err)
	}
	if !strings.Contains(err.Error(), "remains available") {
		t.Fatalf("rejection must state the grant is unconsumed: %v", err)
	}
	if got := semanticCanonicalRejectionText(err); !strings.Contains(got, "pptx_chart_type_unsupported") {
		t.Fatalf("detailed rejection must survive: %s", got)
	}

	err = semanticOfficeSlideChartCheck(`{"path":"deck.pptx","slides":[{"title":"t","charts":[{"chart_type":"bar","categories":["a"],"series":[{"name":"s","values":[1,2]}]}]}]}`)
	if err == nil || !strings.Contains(err.Error(), "pptx_chart_series_length_mismatch") {
		t.Fatalf("length mismatch must be rejected: %v", err)
	}

	err = semanticOfficeSlideChartCheck(`{"path":"deck.pptx","slides":[{"title":"t","charts":[{"chart_type":"pie","categories":["a","b"],"series":[{"name":"one","values":[1,2]},{"name":"two","values":[3,4]}]}]}]}`)
	if err == nil || !strings.Contains(err.Error(), "pptx_chart_pie_single_series") {
		t.Fatalf("pie extra series must be rejected: %v", err)
	}
}

func TestSemanticOfficeWriteInvocationArgsUnwrapsStringifiedCharts(t *testing.T) {
	in := `{"path":"deck.pptx","slides":[{"title":"t","charts":"[{\"chart_type\":\"bar\",\"categories\":[\"a\"],\"series\":[{\"name\":\"s\",\"values\":[1]}]}]"}]}`
	out := semanticOfficeWriteInvocationArgs(in)
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatal(err)
	}
	slides := parsed["slides"].([]interface{})
	slide := slides[0].(map[string]interface{})
	charts, ok := slide["charts"].([]interface{})
	if !ok || len(charts) != 1 {
		t.Fatalf("stringified charts not unwrapped: %#v", slide["charts"])
	}
}

func TestSemanticOfficeWriteInvocationArgsUnwrapsNestedChartArrays(t *testing.T) {
	in := `{"path":"deck.pptx","slides":[{"title":"t","charts":[{"chart_type":"bar","categories":"[\"a\",\"b\"]","series":[{"name":"s","values":"[1,2]"}]}]}]}`
	out := semanticOfficeWriteInvocationArgs(in)
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatal(err)
	}
	slide := parsed["slides"].([]interface{})[0].(map[string]interface{})
	chart := slide["charts"].([]interface{})[0].(map[string]interface{})
	cats, ok := chart["categories"].([]interface{})
	if !ok || len(cats) != 2 {
		t.Fatalf("categories not unwrapped: %#v", chart["categories"])
	}
	series := chart["series"].([]interface{})[0].(map[string]interface{})
	values, ok := series["values"].([]interface{})
	if !ok || len(values) != 2 {
		t.Fatalf("values not unwrapped: %#v", series["values"])
	}
}
