package needledata

import "strings"

type DatasetReport struct {
	Total            int                         `json:"total"`
	ByTask           map[string]DatasetTaskStats `json:"by_task"`
	ByLabel          map[string]int              `json:"by_label"`
	EmptyInput       int                         `json:"empty_input"`
	MissingLabel     int                         `json:"missing_label"`
	DuplicateInputs  int                         `json:"duplicate_inputs,omitempty"`
	DuplicateSamples []DatasetDuplicateSample    `json:"duplicate_samples,omitempty"`
}

type DatasetDuplicateSample struct {
	Task    string   `json:"task"`
	Input   string   `json:"input,omitempty"`
	Records []string `json:"records"`
}

type DatasetLeakageReport struct {
	TrainTotal int                 `json:"train_total"`
	EvalTotal  int                 `json:"eval_total"`
	Overlaps   int                 `json:"overlaps"`
	Samples    []DatasetLeakSample `json:"samples,omitempty"`
}

type DatasetLeakSample struct {
	Task    string `json:"task"`
	TrainID string `json:"train_id"`
	EvalID  string `json:"eval_id"`
	Input   string `json:"input,omitempty"`
}

type DatasetTaskStats struct {
	Total   int            `json:"total"`
	Labels  map[string]int `json:"labels"`
	MinText int            `json:"min_text,omitempty"`
	MaxText int            `json:"max_text,omitempty"`
}

type EventReport struct {
	Total                int                         `json:"total"`
	ByType               map[string]DatasetTaskStats `json:"by_type"`
	Successful           int                         `json:"successful"`
	UserCorrected        int                         `json:"user_corrected"`
	Redacted             int                         `json:"redacted"`
	NeedleShadow         int                         `json:"needle_shadow"`
	NeedleShadowAccept   int                         `json:"needle_shadow_accept"`
	NeedleShadowReject   int                         `json:"needle_shadow_reject,omitempty"`
	NeedleByRejectReason map[string]int              `json:"needle_by_reject_reason,omitempty"`
}

func BuildTrainingReport(records []TrainingRecord) DatasetReport {
	report := DatasetReport{ByTask: map[string]DatasetTaskStats{}, ByLabel: map[string]int{}}
	duplicates := map[string][]TrainingRecord{}
	for _, rec := range records {
		report.Total++
		label := strings.TrimSpace(rec.Expected.Name)
		if label == "" {
			report.MissingLabel++
		}
		text := strings.TrimSpace(firstUserMessage(rec.Messages))
		if text == "" {
			report.EmptyInput++
		}
		if key := leakageKey(rec); key != "" {
			duplicates[key] = append(duplicates[key], rec)
		}
		if label != "" {
			report.ByLabel[label]++
		}
		bucket := report.ByTask[rec.Task]
		if bucket.Labels == nil {
			bucket.Labels = map[string]int{}
		}
		bucket.Total++
		if label != "" {
			bucket.Labels[label]++
		}
		textLen := len([]rune(text))
		if textLen > 0 && (bucket.MinText == 0 || textLen < bucket.MinText) {
			bucket.MinText = textLen
		}
		if textLen > bucket.MaxText {
			bucket.MaxText = textLen
		}
		report.ByTask[rec.Task] = bucket
	}
	for _, items := range duplicates {
		if len(items) < 2 {
			continue
		}
		report.DuplicateInputs += len(items) - 1
		if len(report.DuplicateSamples) < 20 {
			ids := make([]string, 0, len(items))
			for _, item := range items {
				ids = append(ids, item.ID)
			}
			report.DuplicateSamples = append(report.DuplicateSamples, DatasetDuplicateSample{Task: items[0].Task, Input: firstUserMessage(items[0].Messages), Records: ids})
		}
	}
	return report
}

func BuildLeakageReport(train, eval []TrainingRecord, sampleLimit int) DatasetLeakageReport {
	report := DatasetLeakageReport{TrainTotal: len(train), EvalTotal: len(eval)}
	seen := map[string]TrainingRecord{}
	for _, rec := range train {
		key := leakageKey(rec)
		if key != "" {
			if _, ok := seen[key]; !ok {
				seen[key] = rec
			}
		}
	}
	for _, rec := range eval {
		key := leakageKey(rec)
		if key == "" {
			continue
		}
		trainRec, ok := seen[key]
		if !ok {
			continue
		}
		report.Overlaps++
		if sampleLimit <= 0 || len(report.Samples) < sampleLimit {
			report.Samples = append(report.Samples, DatasetLeakSample{Task: rec.Task, TrainID: trainRec.ID, EvalID: rec.ID, Input: firstUserMessage(rec.Messages)})
		}
	}
	return report
}

func leakageKey(rec TrainingRecord) string {
	text := strings.ToLower(strings.Join(strings.Fields(firstUserMessage(rec.Messages)), " "))
	if text == "" {
		return ""
	}
	return strings.TrimSpace(rec.Task) + "\x00" + text
}

func BuildEventReport(events []Event) EventReport {
	report := EventReport{ByType: map[string]DatasetTaskStats{}, NeedleByRejectReason: map[string]int{}}
	for _, e := range events {
		report.Total++
		if e.Outcome.Success {
			report.Successful++
		}
		if e.Outcome.UserCorrected {
			report.UserCorrected++
		}
		if e.Privacy.Redacted {
			report.Redacted++
		}
		if e.NeedlePrediction != nil && strings.TrimSpace(e.NeedlePrediction.Name) != "" {
			report.NeedleShadow++
			if reason := needleRejectReason(e.NeedlePrediction); reason != "" {
				report.NeedleShadowReject++
				report.NeedleByRejectReason[reason]++
			} else if e.NeedlePrediction.Confidence > 0 {
				report.NeedleShadowAccept++
			}
		}
		label := strings.TrimSpace(e.FinalDecision.Name)
		bucket := report.ByType[e.Type]
		if bucket.Labels == nil {
			bucket.Labels = map[string]int{}
		}
		bucket.Total++
		if label != "" {
			bucket.Labels[label]++
		}
		textLen := len([]rune(strings.TrimSpace(e.Input.UserText)))
		if textLen > 0 && (bucket.MinText == 0 || textLen < bucket.MinText) {
			bucket.MinText = textLen
		}
		if textLen > bucket.MaxText {
			bucket.MaxText = textLen
		}
		report.ByType[e.Type] = bucket
	}
	if len(report.NeedleByRejectReason) == 0 {
		report.NeedleByRejectReason = nil
	}
	return report
}

func needleRejectReason(d *Decision) string {
	if d == nil || d.Arguments == nil {
		return ""
	}
	reason, _ := d.Arguments["reject_reason"].(string)
	return strings.TrimSpace(reason)
}
