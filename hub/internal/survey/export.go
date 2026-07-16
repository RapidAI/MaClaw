package survey

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/excel"
)

// BuildExportData builds multi-sheet excel data from survey + responses.
// Column order frozen by design §8; anonymous: respondent_key="anonymous", name empty.
func BuildExportData(sv *Survey, responses []Response) excel.WriteData {
	// Sheet1 responses — design column order
	header := []excel.WriteCell{
		{Value: "response_id"},
		{Value: "submitted_at"},
		{Value: "platform"},
		{Value: "group_id"},
		{Value: "group_name"},
		{Value: "respondent_key"},
		{Value: "respondent_name"},
	}
	for _, q := range sv.Questions {
		header = append(header, excel.WriteCell{Value: fmt.Sprintf("Q%d: %s", q.Position+1, q.Title)})
	}
	rows := [][]excel.WriteCell{header}
	for _, resp := range responses {
		key := resp.RespondentKey
		name := resp.RespondentName
		if sv.Settings.Anonymous {
			key = "anonymous"
			name = ""
		}
		row := []excel.WriteCell{
			{Value: resp.ID},
			{Value: resp.SubmittedAt.UTC().Format(time.RFC3339)},
			{Value: resp.Platform},
			{Value: resp.GroupID},
			{Value: ""}, // group_name snapshot not stored on response rows in MVP
			{Value: key},
			{Value: name},
		}
		m := JSONToAnswers(resp.Answers)
		for _, q := range sv.Questions {
			row = append(row, excel.WriteCell{Value: FormatAnswerCell(q, m[q.ID])})
		}
		rows = append(rows, row)
	}

	// Sheet2 summary
	stats := ComputeStats(sv, responses)
	sumRows := [][]excel.WriteCell{
		{{Value: "metric"}, {Value: "value"}},
		{{Value: "response_count"}, {Value: float64(stats.ResponseCount)}},
		{{Value: "title"}, {Value: sv.Title}},
		{{Value: "short_code"}, {Value: sv.ShortCode}},
	}
	if stats.TargetCount > 0 {
		sumRows = append(sumRows, []excel.WriteCell{{Value: "target_count"}, {Value: float64(stats.TargetCount)}})
	}
	sumRows = append(sumRows, []excel.WriteCell{{Value: "question_id"}, {Value: "option_or_metric"}, {Value: "label"}, {Value: "count"}, {Value: "percent"}})
	for _, qs := range stats.ByQuestion {
		switch qs.Type {
		case "single_choice", "multi_choice":
			for _, o := range qs.Options {
				sumRows = append(sumRows, []excel.WriteCell{
					{Value: qs.QuestionID},
					{Value: o.OptionID},
					{Value: o.Label},
					{Value: float64(o.Count)},
					{Value: o.Percent},
				})
			}
		case "rating":
			sumRows = append(sumRows, []excel.WriteCell{
				{Value: qs.QuestionID},
				{Value: "avg"},
				{Value: qs.Title},
				{Value: qs.RatingAvg},
				{Value: float64(qs.RatingN)},
			})
		case "text":
			sumRows = append(sumRows, []excel.WriteCell{
				{Value: qs.QuestionID},
				{Value: "text_count"},
				{Value: qs.Title},
				{Value: float64(qs.TextCount)},
				{Value: 0.0},
			})
		}
	}

	return excel.WriteData{
		Sheets: []excel.WriteSheet{
			{Name: "responses", Rows: rows},
			{Name: "summary", Rows: sumRows},
		},
	}
}

// WriteExportFile writes xlsx to path.
func WriteExportFile(path string, sv *Survey, responses []Response) error {
	data := BuildExportData(sv, responses)
	if err := excel.WriteFile(path, data); err != nil {
		return err
	}
	return nil
}

// DefaultExportFilename returns a suggested file name.
func DefaultExportFilename(sv *Survey, now time.Time) string {
	ts := now.UTC().Format("20060102_150405")
	return filepath.Base(fmt.Sprintf("survey_%s_%s.xlsx", sv.ShortCode, ts))
}
