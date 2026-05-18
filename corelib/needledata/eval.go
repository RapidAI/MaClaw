package needledata

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

type EvalSummary struct {
	Total          int                   `json:"total"`
	Matched        int                   `json:"matched"`
	Accuracy       float64               `json:"accuracy"`
	AcceptedRatio  float64               `json:"accepted_ratio,omitempty"`
	Rejected       int                   `json:"rejected,omitempty"`
	RejectedRatio  float64               `json:"rejected_ratio,omitempty"`
	Missing        int                   `json:"missing,omitempty"`
	MissingRatio   float64               `json:"missing_ratio,omitempty"`
	ByRejectReason map[string]int        `json:"by_reject_reason,omitempty"`
	ByTask         map[string]EvalBucket `json:"by_task"`
	Mismatches     []EvalMismatch        `json:"mismatches,omitempty"`
}

type EvalBucket struct {
	Total          int            `json:"total"`
	Matched        int            `json:"matched"`
	Accuracy       float64        `json:"accuracy"`
	AcceptedRatio  float64        `json:"accepted_ratio,omitempty"`
	Rejected       int            `json:"rejected,omitempty"`
	RejectedRatio  float64        `json:"rejected_ratio,omitempty"`
	Missing        int            `json:"missing,omitempty"`
	MissingRatio   float64        `json:"missing_ratio,omitempty"`
	ByRejectReason map[string]int `json:"by_reject_reason,omitempty"`
}

type EvalMismatch struct {
	ID       string `json:"id"`
	Task     string `json:"task"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
	Input    string `json:"input,omitempty"`
}

type PredictionRecord struct {
	ID           string   `json:"id"`
	Decision     Decision `json:"decision"`
	Accepted     bool     `json:"accepted"`
	RejectReason string   `json:"reject_reason,omitempty"`
}

func EvaluateTrainingPredictions(records []TrainingRecord, predictions map[string]Decision, mismatchLimit int) EvalSummary {
	s := EvalSummary{ByTask: map[string]EvalBucket{}}
	for _, rec := range records {
		pred, ok := predictions[rec.ID]
		if !ok || strings.TrimSpace(pred.Name) == "" {
			continue
		}
		s.Total++
		bucket := s.ByTask[rec.Task]
		bucket.Total++
		matched := normalizeDecisionName(rec.Expected.Name) == normalizeDecisionName(pred.Name)
		if matched {
			s.Matched++
			bucket.Matched++
		} else if mismatchLimit <= 0 || len(s.Mismatches) < mismatchLimit {
			s.Mismatches = append(s.Mismatches, EvalMismatch{ID: rec.ID, Task: rec.Task, Expected: rec.Expected.Name, Actual: pred.Name, Input: firstUserMessage(rec.Messages)})
		}
		s.ByTask[rec.Task] = bucket
	}
	s.Accuracy = ratio(s.Matched, s.Total)
	s.AcceptedRatio = ratio(s.Total, len(records))
	for task, bucket := range s.ByTask {
		bucket.Accuracy = ratio(bucket.Matched, bucket.Total)
		bucket.AcceptedRatio = 1
		s.ByTask[task] = bucket
	}
	return s
}

func EvaluateTrainingPredictionRecords(records []TrainingRecord, predictions map[string]PredictionRecord, mismatchLimit int) EvalSummary {
	s := EvalSummary{ByTask: map[string]EvalBucket{}, ByRejectReason: map[string]int{}}
	for _, rec := range records {
		pred, ok := predictions[rec.ID]
		if !ok || strings.TrimSpace(pred.Decision.Name) == "" {
			s.Missing++
			bucket := s.ByTask[rec.Task]
			bucket.Missing++
			s.ByTask[rec.Task] = bucket
			continue
		}
		if !pred.Accepted {
			reason := strings.TrimSpace(pred.RejectReason)
			if reason == "" {
				reason = "not_accepted"
			}
			s.Rejected++
			s.ByRejectReason[reason]++
			bucket := s.ByTask[rec.Task]
			bucket.Rejected++
			if bucket.ByRejectReason == nil {
				bucket.ByRejectReason = map[string]int{}
			}
			bucket.ByRejectReason[reason]++
			s.ByTask[rec.Task] = bucket
			continue
		}
		s.Total++
		bucket := s.ByTask[rec.Task]
		bucket.Total++
		matched := normalizeDecisionName(rec.Expected.Name) == normalizeDecisionName(pred.Decision.Name)
		if matched {
			s.Matched++
			bucket.Matched++
		} else if mismatchLimit <= 0 || len(s.Mismatches) < mismatchLimit {
			s.Mismatches = append(s.Mismatches, EvalMismatch{ID: rec.ID, Task: rec.Task, Expected: rec.Expected.Name, Actual: pred.Decision.Name, Input: firstUserMessage(rec.Messages)})
		}
		s.ByTask[rec.Task] = bucket
	}
	s.Accuracy = ratio(s.Matched, s.Total)
	s.AcceptedRatio = ratio(s.Total, len(records))
	s.RejectedRatio = ratio(s.Rejected, len(records))
	s.MissingRatio = ratio(s.Missing, len(records))
	for task, bucket := range s.ByTask {
		den := bucket.Total + bucket.Rejected + bucket.Missing
		bucket.Accuracy = ratio(bucket.Matched, bucket.Total)
		bucket.AcceptedRatio = ratio(bucket.Total, den)
		bucket.RejectedRatio = ratio(bucket.Rejected, den)
		bucket.MissingRatio = ratio(bucket.Missing, den)
		s.ByTask[task] = bucket
	}
	if len(s.ByRejectReason) == 0 {
		s.ByRejectReason = nil
	}
	return s
}

func EvaluateShadowEvents(events []Event, mismatchLimit int) EvalSummary {
	s := EvalSummary{ByTask: map[string]EvalBucket{}, ByRejectReason: map[string]int{}}
	eligible := 0
	for _, e := range events {
		if strings.TrimSpace(e.FinalDecision.Name) == "" {
			continue
		}
		eligible++
		if e.NeedlePrediction == nil || strings.TrimSpace(e.NeedlePrediction.Name) == "" {
			s.Missing++
			bucket := s.ByTask[e.Type]
			bucket.Missing++
			s.ByTask[e.Type] = bucket
			continue
		}
		if reason := needleRejectReason(e.NeedlePrediction); reason != "" {
			s.Rejected++
			s.ByRejectReason[reason]++
			bucket := s.ByTask[e.Type]
			bucket.Rejected++
			if bucket.ByRejectReason == nil {
				bucket.ByRejectReason = map[string]int{}
			}
			bucket.ByRejectReason[reason]++
			s.ByTask[e.Type] = bucket
			continue
		}
		s.Total++
		bucket := s.ByTask[e.Type]
		bucket.Total++
		matched := normalizeDecisionName(e.FinalDecision.Name) == normalizeDecisionName(e.NeedlePrediction.Name)
		if matched {
			s.Matched++
			bucket.Matched++
		} else if mismatchLimit <= 0 || len(s.Mismatches) < mismatchLimit {
			s.Mismatches = append(s.Mismatches, EvalMismatch{ID: e.EventID, Task: e.Type, Expected: e.FinalDecision.Name, Actual: e.NeedlePrediction.Name, Input: e.Input.UserText})
		}
		s.ByTask[e.Type] = bucket
	}
	s.Accuracy = ratio(s.Matched, s.Total)
	s.AcceptedRatio = ratio(s.Total, eligible)
	s.RejectedRatio = ratio(s.Rejected, eligible)
	s.MissingRatio = ratio(s.Missing, eligible)
	for task, bucket := range s.ByTask {
		den := bucket.Total + bucket.Rejected + bucket.Missing
		bucket.Accuracy = ratio(bucket.Matched, bucket.Total)
		bucket.AcceptedRatio = ratio(bucket.Total, den)
		bucket.RejectedRatio = ratio(bucket.Rejected, den)
		bucket.MissingRatio = ratio(bucket.Missing, den)
		s.ByTask[task] = bucket
	}
	if len(s.ByRejectReason) == 0 {
		s.ByRejectReason = nil
	}
	return s
}

func ReadTrainingRecords(path string) ([]TrainingRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readTrainingRecordsFrom(f)
}

func readTrainingRecordsFrom(r io.Reader) ([]TrainingRecord, error) {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var records []TrainingRecord
	for s.Scan() {
		line := cleanJSONLLine(s.Text())
		if line == "" {
			continue
		}
		var rec TrainingRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			return nil, err
		}
		records = append(records, rec)
	}
	return records, s.Err()
}

func ReadPredictionJSONL(path string) (map[string]Decision, error) {
	records, err := ReadPredictionRecords(path)
	if err != nil {
		return nil, err
	}
	out := make(map[string]Decision, len(records))
	for id, rec := range records {
		out[id] = rec.Decision
	}
	return out, nil
}

func ReadPredictionRecords(path string) (map[string]PredictionRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]PredictionRecord{}
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for s.Scan() {
		line := cleanJSONLLine(s.Text())
		if line == "" {
			continue
		}
		var raw struct {
			ID           string         `json:"id"`
			Name         string         `json:"name"`
			Arguments    map[string]any `json:"arguments,omitempty"`
			Decision     *Decision      `json:"decision,omitempty"`
			Accepted     *bool          `json:"accepted,omitempty"`
			RejectReason string         `json:"reject_reason,omitempty"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			return nil, err
		}
		if raw.ID == "" {
			return nil, fmt.Errorf("prediction missing id")
		}
		decision := Decision{Name: raw.Name, Arguments: raw.Arguments}
		if raw.Decision != nil {
			decision = *raw.Decision
		}
		accepted := strings.TrimSpace(decision.Name) != "" && strings.TrimSpace(raw.RejectReason) == ""
		if raw.Accepted != nil {
			accepted = *raw.Accepted
		}
		out[raw.ID] = PredictionRecord{ID: raw.ID, Decision: decision, Accepted: accepted, RejectReason: strings.TrimSpace(raw.RejectReason)}
	}
	return out, s.Err()
}

func normalizeDecisionName(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func firstUserMessage(messages []ChatMessage) string {
	for _, msg := range messages {
		if msg.Role == "user" {
			return msg.Content
		}
	}
	return ""
}

func ratio(num, den int) float64 {
	if den == 0 {
		return 0
	}
	return float64(num) / float64(den)
}
