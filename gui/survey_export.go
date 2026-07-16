package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/excel"
)

// buildSurveyExportXLSX builds multi-sheet excel from Hub survey JSON detail + responses wrapper.
func buildSurveyExportXLSX(detailJSON, responsesJSON []byte) (excel.WriteData, error) {
	var sv struct {
		Title     string `json:"title"`
		ShortCode string `json:"short_code"`
		Settings  struct {
			Anonymous bool `json:"anonymous"`
		} `json:"settings"`
		Questions []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Title    string `json:"title"`
			Position int    `json:"position"`
			Options  []struct {
				ID    string `json:"id"`
				Label string `json:"label"`
			} `json:"options"`
		} `json:"questions"`
		Bindings []struct {
			GroupID   string `json:"group_id"`
			GroupName string `json:"group_name"`
		} `json:"bindings"`
	}
	if err := json.Unmarshal(detailJSON, &sv); err != nil {
		return excel.WriteData{}, err
	}
	groupNames := map[string]string{}
	for _, b := range sv.Bindings {
		if gid := strings.TrimSpace(b.GroupID); gid != "" {
			groupNames[gid] = b.GroupName
		}
	}
	var wrap struct {
		Responses []struct {
			ID             string          `json:"id"`
			RespondentKey  string          `json:"respondent_key"`
			RespondentName string          `json:"respondent_name"`
			SubmittedAt    string          `json:"submitted_at"`
			GroupID        string          `json:"group_id"`
			Platform       string          `json:"platform"`
			Answers        json.RawMessage `json:"answers"`
		} `json:"responses"`
	}
	if err := json.Unmarshal(responsesJSON, &wrap); err != nil {
		return excel.WriteData{}, err
	}

	// Design §8 frozen column order (group_name empty when snapshot unavailable).
	header := []excel.WriteCell{
		{Value: "response_id"},
		{Value: "submitted_at"},
		{Value: "platform"},
		{Value: "group_id"},
		{Value: "group_name"},
		{Value: "respondent_key"},
		{Value: "respondent_name"},
	}
	for i, q := range sv.Questions {
		pos := q.Position
		if pos == 0 {
			pos = i
		}
		header = append(header, excel.WriteCell{Value: fmt.Sprintf("Q%d: %s", pos+1, q.Title)})
	}
	rows := [][]excel.WriteCell{header}
	for _, resp := range wrap.Responses {
		key, name := resp.RespondentKey, resp.RespondentName
		if sv.Settings.Anonymous {
			key, name = "anonymous", ""
		}
		row := []excel.WriteCell{
			{Value: resp.ID},
			{Value: resp.SubmittedAt},
			{Value: resp.Platform},
			{Value: resp.GroupID},
			{Value: groupNames[strings.TrimSpace(resp.GroupID)]},
			{Value: key},
			{Value: name},
		}
		var answers map[string]any
		_ = json.Unmarshal(resp.Answers, &answers)
		for _, q := range sv.Questions {
			row = append(row, excel.WriteCell{Value: formatSurveyAnswerCell(q.Type, q.Options, answers[q.ID])})
		}
		rows = append(rows, row)
	}

	sumRows := [][]excel.WriteCell{
		{{Value: "metric"}, {Value: "value"}},
		{{Value: "response_count"}, {Value: float64(len(wrap.Responses))}},
		{{Value: "title"}, {Value: sv.Title}},
		{{Value: "short_code"}, {Value: sv.ShortCode}},
		{{Value: "question_id"}, {Value: "option_or_metric"}, {Value: "label"}, {Value: "count"}, {Value: "percent"}},
	}
	// simple option counts for choice questions
	for _, q := range sv.Questions {
		if q.Type != "single_choice" && q.Type != "multi_choice" {
			continue
		}
		counts := map[string]int{}
		for _, o := range q.Options {
			counts[o.ID] = 0
		}
		for _, resp := range wrap.Responses {
			var answers map[string]any
			_ = json.Unmarshal(resp.Answers, &answers)
			v := answers[q.ID]
			switch q.Type {
			case "single_choice":
				if id, ok := v.(string); ok {
					counts[id]++
				}
			case "multi_choice":
				switch arr := v.(type) {
				case []any:
					for _, x := range arr {
						if id, ok := x.(string); ok {
							counts[id]++
						}
					}
				}
			}
		}
		denom := float64(len(wrap.Responses))
		for _, o := range q.Options {
			c := counts[o.ID]
			pct := 0.0
			if denom > 0 {
				pct = float64(c) / denom * 100
			}
			sumRows = append(sumRows, []excel.WriteCell{
				{Value: q.ID}, {Value: o.ID}, {Value: o.Label}, {Value: float64(c)}, {Value: pct},
			})
		}
	}

	return excel.WriteData{
		Sheets: []excel.WriteSheet{
			{Name: "responses", Rows: rows},
			{Name: "summary", Rows: sumRows},
		},
	}, nil
}

func formatSurveyAnswerCell(qType string, options []struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}, v any) string {
	if v == nil {
		return ""
	}
	labelOf := func(id string) string {
		for _, o := range options {
			if o.ID == id {
				return o.Label
			}
		}
		return id
	}
	switch qType {
	case "single_choice":
		if id, ok := v.(string); ok {
			return labelOf(id)
		}
	case "multi_choice":
		var ids []string
		switch arr := v.(type) {
		case []any:
			for _, x := range arr {
				if id, ok := x.(string); ok {
					ids = append(ids, id)
				}
			}
		case []string:
			ids = arr
		case string:
			// answers sometimes arrive as JSON-encoded array string
			var parsed []any
			if err := json.Unmarshal([]byte(arr), &parsed); err == nil {
				for _, x := range parsed {
					if id, ok := x.(string); ok {
						ids = append(ids, id)
					}
				}
			} else if arr != "" {
				ids = []string{arr}
			}
		}
		set := map[string]struct{}{}
		for _, id := range ids {
			set[id] = struct{}{}
		}
		var labels []string
		for _, o := range options {
			if _, ok := set[o.ID]; ok {
				labels = append(labels, o.Label)
			}
		}
		if len(labels) == 0 && len(ids) > 0 {
			return strings.Join(ids, ", ")
		}
		return strings.Join(labels, ", ")
	}
	return fmt.Sprint(v)
}

func defaultSurveyExportName(shortCode string, now time.Time) string {
	return filepath.Base(fmt.Sprintf("survey_%s_%s.xlsx", shortCode, now.UTC().Format("20060102_150405")))
}

func writeSurveyExcelFile(path string, data excel.WriteData) error {
	return excel.WriteFile(path, data)
}
