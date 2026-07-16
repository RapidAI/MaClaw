package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBuildSurveyExportXLSXMultiSheetAndAnonymous(t *testing.T) {
	detail := map[string]any{
		"title":      "Vote",
		"short_code": "A3F9K2",
		"settings":   map[string]any{"anonymous": true},
		"questions": []map[string]any{
			{
				"id": "q1", "type": "single_choice", "title": "OK?", "position": 0,
				"options": []map[string]any{{"id": "opt_yes", "label": "是"}, {"id": "opt_no", "label": "否"}},
			},
			{
				"id": "q2", "type": "multi_choice", "title": "兴趣", "position": 1,
				"options": []map[string]any{
					{"id": "opt_c", "label": "C"},
					{"id": "opt_a", "label": "A"},
					{"id": "opt_b", "label": "B"},
				},
			},
		},
	}
	responses := map[string]any{
		"responses": []map[string]any{
			{
				"respondent_key":  "deadbeef",
				"respondent_name": "Secret",
				"submitted_at":    time.Now().UTC().Format(time.RFC3339),
				"group_id":        "g1",
				"platform":        "lansenger",
				"answers": map[string]any{
					"q1": "opt_yes",
					"q2": []string{"opt_a", "opt_c"},
				},
			},
		},
	}
	dRaw, _ := json.Marshal(detail)
	rRaw, _ := json.Marshal(responses)
	data, err := buildSurveyExportXLSX(dRaw, rRaw)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Sheets) != 2 {
		t.Fatalf("sheets=%d", len(data.Sheets))
	}
	if data.Sheets[0].Name != "responses" || data.Sheets[1].Name != "summary" {
		t.Fatalf("names=%q %q", data.Sheets[0].Name, data.Sheets[1].Name)
	}
	// anonymous placeholders
	if data.Sheets[0].Rows[1][0].Value != "anonymous" {
		t.Fatalf("key=%v", data.Sheets[0].Rows[1][0].Value)
	}
	if data.Sheets[0].Rows[1][1].Value != "" {
		t.Fatalf("name=%v", data.Sheets[0].Rows[1][1].Value)
	}
	// multi labels in option array order: C then A
	cell := data.Sheets[0].Rows[1][6].Value // after 5 fixed cols + q1
	if cell != "C, A" {
		// q columns: index 5 is q1, 6 is q2
		t.Fatalf("multi cell=%v header0=%v", cell, data.Sheets[0].Rows[0])
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "out.xlsx")
	if err := writeSurveyExcelFile(path, data); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil || st.Size() < 100 {
		t.Fatalf("xlsx missing or tiny: %v size=%v", err, st)
	}
}
