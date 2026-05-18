package needledata

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateSeedRecordsIncludesMacLawTasks(t *testing.T) {
	records := GenerateSeedRecords(GenerateOptions{PerIntent: 1, PerWorkflowReview: 1, Seed: 1})
	seen := map[string]bool{}
	for _, rec := range records {
		seen[rec.Task] = true
		if rec.Expected.Name == "" {
			t.Fatalf("record %s has empty expected decision", rec.ID)
		}
	}
	for _, task := range []string{EventIntentGate, EventWorkflowReview, EventMemoryExtractGate} {
		if !seen[task] {
			t.Fatalf("missing task %s in generated records", task)
		}
	}
}

func TestEventsToTrainingRecordsFiltersSuccessAndRedacts(t *testing.T) {
	events := []Event{
		{EventID: "ok", Type: EventToolRouting, Input: EventInput{UserText: "use token=secret123 on /tmp/project", AvailableTools: []ToolSummary{{Name: "ssh", Description: "remote shell"}}}, FinalDecision: Decision{Name: "ssh"}, Outcome: EventOutcome{Success: true}},
		{EventID: "bad", Type: EventToolRouting, Input: EventInput{UserText: "bad"}, FinalDecision: Decision{Name: "ssh"}, Outcome: EventOutcome{Success: false}},
	}
	recs := EventsToTrainingRecords(events, ExportOptions{OnlySuccess: true})
	if len(recs) != 1 {
		t.Fatalf("records = %d, want 1", len(recs))
	}
	if recs[0].Expected.Name != "ssh" {
		t.Fatalf("expected decision = %q", recs[0].Expected.Name)
	}
	redacted := RedactEvent(events[0])
	if redacted.Input.UserText == events[0].Input.UserText || !redacted.Privacy.Redacted {
		t.Fatalf("event was not redacted: %#v", redacted)
	}
}

func TestEventsToTrainingRecordsDeduplicates(t *testing.T) {
	events := []Event{
		{EventID: "1", Type: EventWorkflowReview, Input: EventInput{UserText: "Continue now"}, FinalDecision: Decision{Name: "confirm"}, Outcome: EventOutcome{Success: true}},
		{EventID: "2", Type: EventWorkflowReview, Input: EventInput{UserText: " continue  now "}, FinalDecision: Decision{Name: "confirm"}, Outcome: EventOutcome{Success: true}},
		{EventID: "3", Type: EventWorkflowReview, Input: EventInput{UserText: "cancel"}, FinalDecision: Decision{Name: "cancel"}, Outcome: EventOutcome{Success: true}},
	}
	records := EventsToTrainingRecords(events, ExportOptions{OnlySuccess: true, Deduplicate: true})
	if len(records) != 2 || records[0].ID != "1" || records[1].ID != "3" {
		t.Fatalf("dedup records = %#v", records)
	}
	records = DeduplicateTrainingRecords([]TrainingRecord{
		{ID: "1", Task: EventWorkflowReview, Messages: []ChatMessage{{Role: "user", Content: "Continue now"}}, Expected: Decision{Name: "confirm"}},
		{ID: "2", Task: EventWorkflowReview, Messages: []ChatMessage{{Role: "user", Content: "continue now"}}, Expected: Decision{Name: "confirm"}},
	})
	if len(records) != 1 || records[0].ID != "1" {
		t.Fatalf("DeduplicateTrainingRecords = %#v", records)
	}
}

func TestEvaluateShadowEvents(t *testing.T) {
	events := []Event{
		{EventID: "1", Type: EventWorkflowReview, NeedlePrediction: &Decision{Name: "confirm"}, FinalDecision: Decision{Name: "confirm"}},
		{EventID: "2", Type: EventWorkflowReview, NeedlePrediction: &Decision{Name: "skip", Arguments: map[string]any{"reject_reason": "below_min_confidence"}}, FinalDecision: Decision{Name: "cancel"}},
		{EventID: "3", Type: EventToolRouting, NeedlePrediction: &Decision{Name: "ssh"}, FinalDecision: Decision{Name: "ssh"}},
		{EventID: "4", Type: EventWorkflowReview, FinalDecision: Decision{Name: "confirm"}},
	}
	summary := EvaluateShadowEvents(events, 10)
	if summary.Total != 2 || summary.Matched != 2 || summary.Rejected != 1 || summary.Missing != 1 {
		t.Fatalf("summary = %#v, want total=3 matched=2", summary)
	}
	if len(summary.Mismatches) != 0 || summary.ByRejectReason["below_min_confidence"] != 1 || summary.AcceptedRatio != 0.5 || summary.RejectedRatio != 0.25 || summary.MissingRatio != 0.25 {
		t.Fatalf("mismatches = %#v", summary.Mismatches)
	}
	report := BuildEventReport(events)
	if report.NeedleShadowReject != 1 || report.NeedleByRejectReason["below_min_confidence"] != 1 {
		t.Fatalf("event report reject stats = %#v", report)
	}
}

func TestBuildTrainingReportAndQualityGate(t *testing.T) {
	records := []TrainingRecord{
		{ID: "1", Task: EventWorkflowReview, Messages: []ChatMessage{{Role: "user", Content: "continue"}}, Expected: Decision{Name: "confirm"}},
		{ID: "2", Task: EventWorkflowReview, Messages: []ChatMessage{{Role: "user", Content: "cancel"}}, Expected: Decision{Name: "cancel"}},
	}
	report := BuildTrainingReport(records)
	if report.Total != 2 || report.ByTask[EventWorkflowReview].Labels["confirm"] != 1 || report.ByLabel["cancel"] != 1 {
		t.Fatalf("BuildTrainingReport = %#v", report)
	}
	eval := EvaluateTrainingPredictions(records, map[string]Decision{"1": {Name: "confirm"}, "2": {Name: "cancel"}}, 10)
	gate := ApplyQualityGate(eval, report, QualityGateConfig{MinRecords: 2, MinAccuracy: 1, MinTaskAccuracy: 1, MaxMismatches: 0})
	if !gate.Passed {
		t.Fatalf("quality gate should pass: %#v", gate)
	}
	gate = ApplyQualityGate(eval, report, QualityGateConfig{MinRecords: 3, MinAccuracy: 1, MinTaskAccuracy: 1, MaxMismatches: 0})
	if gate.Passed || len(gate.Reasons) == 0 {
		t.Fatalf("quality gate should fail min records: %#v", gate)
	}
	report.EmptyInput = 1
	gate = ApplyQualityGate(eval, report, QualityGateConfig{MinRecords: 2, MinAccuracy: 1, MinTaskAccuracy: 1, MaxMismatches: 0})
	if gate.Passed {
		t.Fatalf("quality gate should fail empty input: %#v", gate)
	}
	rejectedEval := EvaluateTrainingPredictionRecords(records, map[string]PredictionRecord{
		"1": {ID: "1", Decision: Decision{Name: "confirm"}, Accepted: true},
		"2": {ID: "2", Decision: Decision{Name: "cancel"}, Accepted: false, RejectReason: "below_min_confidence"},
	}, 10)
	gate = ApplyQualityGate(rejectedEval, BuildTrainingReport(records), QualityGateConfig{MinRecords: 2, MinAccuracy: 1, MinTaskAccuracy: 1, MinAcceptedRatio: 0.75, MaxRejectedRatio: 0.25, MaxMismatches: 0})
	if gate.Passed || len(gate.Reasons) == 0 {
		t.Fatalf("quality gate should fail rejected/accepted ratios: %#v", gate)
	}
	dupReport := BuildTrainingReport([]TrainingRecord{
		{ID: "1", Task: EventWorkflowReview, Messages: []ChatMessage{{Role: "user", Content: "continue now"}}, Expected: Decision{Name: "confirm"}},
		{ID: "2", Task: EventWorkflowReview, Messages: []ChatMessage{{Role: "user", Content: " continue  now "}}, Expected: Decision{Name: "confirm"}},
	})
	if dupReport.DuplicateInputs != 1 || len(dupReport.DuplicateSamples) != 1 {
		t.Fatalf("duplicate report = %#v", dupReport)
	}
	gate = ApplyQualityGate(eval, dupReport, QualityGateConfig{MinRecords: 1, MinAccuracy: 1, MinTaskAccuracy: 1, MaxMismatches: 0})
	if gate.Passed || len(gate.Reasons) == 0 || gate.Reasons[0] != "dataset_has_duplicate_input" {
		t.Fatalf("quality gate should fail duplicate input: %#v", gate)
	}
}

func TestSplitHoldoutPreservesTaskLabelCoverage(t *testing.T) {
	var records []TrainingRecord
	for i := 0; i < 4; i++ {
		records = append(records,
			TrainingRecord{ID: fmt.Sprintf("w-confirm-%d", i), Task: EventWorkflowReview, Expected: Decision{Name: "confirm"}},
			TrainingRecord{ID: fmt.Sprintf("w-cancel-%d", i), Task: EventWorkflowReview, Expected: Decision{Name: "cancel"}},
			TrainingRecord{ID: fmt.Sprintf("i-route-%d", i), Task: EventIntentGate, Expected: Decision{Name: "route_browser"}},
			TrainingRecord{ID: fmt.Sprintf("i-no-call-%d", i), Task: EventIntentGate, Expected: Decision{Name: "no_call"}},
		)
	}
	train, eval := SplitHoldout(records, 0.25)
	if len(train) != 12 || len(eval) != 4 {
		t.Fatalf("split sizes train=%d eval=%d, want 12/4", len(train), len(eval))
	}
	seen := map[string]bool{}
	for _, rec := range eval {
		seen[rec.Task+":"+rec.Expected.Name] = true
	}
	for _, key := range []string{EventWorkflowReview + ":confirm", EventWorkflowReview + ":cancel", EventIntentGate + ":route_browser", EventIntentGate + ":no_call"} {
		if !seen[key] {
			t.Fatalf("eval split missing %s: %#v", key, eval)
		}
	}
}

func TestBuildLeakageReportAndQualityGate(t *testing.T) {
	train := []TrainingRecord{
		{ID: "train-1", Task: EventWorkflowReview, Messages: []ChatMessage{{Role: "user", Content: " Continue  now "}}, Expected: Decision{Name: "confirm"}},
	}
	evalRecords := []TrainingRecord{
		{ID: "eval-1", Task: EventWorkflowReview, Messages: []ChatMessage{{Role: "user", Content: "continue now"}}, Expected: Decision{Name: "confirm"}},
	}
	leakage := BuildLeakageReport(train, evalRecords, 10)
	if leakage.Overlaps != 1 || len(leakage.Samples) != 1 || leakage.Samples[0].TrainID != "train-1" {
		t.Fatalf("BuildLeakageReport = %#v", leakage)
	}
	eval := EvaluateTrainingPredictions(evalRecords, map[string]Decision{"eval-1": {Name: "confirm"}}, 10)
	gate := ApplyQualityGateWithLeakage(eval, BuildTrainingReport(train), leakage, QualityGateConfig{MinRecords: 1, MinAccuracy: 1, MinTaskAccuracy: 1, MaxLeakage: 0, MaxMismatches: 0})
	if gate.Passed || len(gate.Reasons) == 0 || gate.Reasons[0] != "train_eval_leakage" {
		t.Fatalf("quality gate should fail leakage: %#v", gate)
	}
}

func TestJSONLReadersAcceptUTF8BOM(t *testing.T) {
	events, err := readEventsFrom(strings.NewReader("\ufeff{\"event_id\":\"e1\",\"type\":\"intent_gate\"}\n"))
	if err != nil {
		t.Fatalf("readEventsFrom returned error: %v", err)
	}
	if len(events) != 1 || events[0].EventID != "e1" {
		t.Fatalf("events = %#v", events)
	}

	records, err := readTrainingRecordsFrom(strings.NewReader("\ufeff{\"id\":\"r1\",\"task\":\"workflow_review\"}\n"))
	if err != nil {
		t.Fatalf("readTrainingRecordsFrom returned error: %v", err)
	}
	if len(records) != 1 || records[0].ID != "r1" {
		t.Fatalf("records = %#v", records)
	}

	predictionPath := filepath.Join(t.TempDir(), "predictions.jsonl")
	if err := os.WriteFile(predictionPath, []byte("\xef\xbb\xbf{\"id\":\"p1\",\"name\":\"confirm\"}\n"), 0o644); err != nil {
		t.Fatalf("write predictions: %v", err)
	}
	predictions, err := ReadPredictionJSONL(predictionPath)
	if err != nil {
		t.Fatalf("ReadPredictionJSONL returned error: %v", err)
	}
	if predictions["p1"].Name != "confirm" {
		t.Fatalf("predictions = %#v", predictions)
	}
}

func TestPredictionRecordsCarryRejectReason(t *testing.T) {
	path := filepath.Join(t.TempDir(), "predictions.jsonl")
	data := `{"id":"r1","decision":{"name":"confirm"},"accepted":true}` + "\n" +
		`{"id":"r2","decision":{"name":"cancel"},"accepted":false,"reject_reason":"outside_choices"}` + "\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write predictions: %v", err)
	}
	predictions, err := ReadPredictionRecords(path)
	if err != nil {
		t.Fatalf("ReadPredictionRecords returned error: %v", err)
	}
	if !predictions["r1"].Accepted || predictions["r2"].Accepted || predictions["r2"].RejectReason != "outside_choices" {
		t.Fatalf("prediction records = %#v", predictions)
	}
	records := []TrainingRecord{
		{ID: "r1", Task: EventWorkflowReview, Messages: []ChatMessage{{Role: "user", Content: "continue"}}, Expected: Decision{Name: "confirm"}},
		{ID: "r2", Task: EventWorkflowReview, Messages: []ChatMessage{{Role: "user", Content: "cancel"}}, Expected: Decision{Name: "cancel"}},
	}
	summary := EvaluateTrainingPredictionRecords(records, predictions, 10)
	if summary.Total != 1 || summary.Matched != 1 || summary.Rejected != 1 || summary.ByRejectReason["outside_choices"] != 1 || summary.AcceptedRatio != 0.5 || summary.RejectedRatio != 0.5 {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestCalibrateThresholds(t *testing.T) {
	records := []TrainingRecord{
		{ID: "r1", Task: EventWorkflowReview, Expected: Decision{Name: "confirm"}},
		{ID: "r2", Task: EventWorkflowReview, Expected: Decision{Name: "cancel"}},
		{ID: "r3", Task: EventWorkflowReview, Expected: Decision{Name: "skip"}},
	}
	predictions := map[string]PredictionRecord{
		"r1": {ID: "r1", Decision: Decision{Name: "confirm", Confidence: 0.95}, Accepted: true},
		"r2": {ID: "r2", Decision: Decision{Name: "confirm", Confidence: 0.60}, Accepted: true},
		"r3": {ID: "r3", Decision: Decision{Name: "skip", Confidence: 0.80}, Accepted: true},
	}
	points := CalibrateThresholds(records, predictions, []float64{0.5, 0.75, 0.9})
	if len(points) != 3 || points[0].Accepted != 3 || points[0].Matched != 2 || points[1].Accepted != 2 || points[1].Accuracy != 1 {
		t.Fatalf("points = %#v", points)
	}
	best, ok := BestThreshold(points, 1, 0.5)
	if !ok || best.Threshold != 0.75 {
		t.Fatalf("BestThreshold = %#v ok=%v, want 0.75", best, ok)
	}
	byTask := CalibrateThresholdsByTask(records, predictions, []float64{0.5, 0.75}, 1, 0.5)
	if !byTask[EventWorkflowReview].RecommendedFound || byTask[EventWorkflowReview].Recommended.Threshold != 0.75 {
		t.Fatalf("CalibrateThresholdsByTask = %#v", byTask)
	}
}

func TestTrainingRecordToNeedleExample(t *testing.T) {
	rec := TrainingRecord{
		ID:       "r1",
		Task:     EventWorkflowReview,
		Messages: []ChatMessage{{Role: "system", Content: reviewSystemPrompt()}, {Role: "user", Content: "continue"}},
		Expected: Decision{Name: "confirm"},
	}
	ex, err := TrainingRecordToNeedleExample(rec)
	if err != nil {
		t.Fatalf("TrainingRecordToNeedleExample returned error: %v", err)
	}
	if err := ValidateNeedleExample(ex); err != nil {
		t.Fatalf("ValidateNeedleExample returned error: %v", err)
	}
	if !strings.Contains(ex.Query, "workflow_review") || !strings.Contains(ex.Answers, "confirm") || !strings.Contains(ex.Tools, "supplement") {
		t.Fatalf("unexpected Needle example: %#v", ex)
	}
}
