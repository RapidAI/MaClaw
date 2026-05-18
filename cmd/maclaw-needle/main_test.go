package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/needledata"
	"github.com/RapidAI/CodeClaw/corelib/needleruntime"
)

func TestBuildHashedLinearArtifactLoadsInRuntime(t *testing.T) {
	records := []needledata.TrainingRecord{
		{ID: "confirm-1", Task: needledata.EventWorkflowReview, Messages: []needledata.ChatMessage{{Role: "user", Content: "looks good continue"}}, Expected: needledata.Decision{Name: "confirm"}},
		{ID: "confirm-2", Task: needledata.EventWorkflowReview, Messages: []needledata.ChatMessage{{Role: "user", Content: "approved go ahead"}}, Expected: needledata.Decision{Name: "confirm"}},
		{ID: "cancel-1", Task: needledata.EventWorkflowReview, Messages: []needledata.ChatMessage{{Role: "user", Content: "cancel this workflow"}}, Expected: needledata.Decision{Name: "cancel"}},
		{ID: "cancel-2", Task: needledata.EventWorkflowReview, Messages: []needledata.ChatMessage{{Role: "user", Content: "stop and abandon it"}}, Expected: needledata.Decision{Name: "cancel"}},
	}

	artifact, err := buildHashedLinearArtifact(records, 32)
	if err != nil {
		t.Fatalf("buildHashedLinearArtifact returned error: %v", err)
	}
	outDir := filepath.Join(t.TempDir(), "artifact")
	if err := writeGoArtifact(outDir, artifact); err != nil {
		t.Fatalf("writeGoArtifact returned error: %v", err)
	}

	inspect := needleruntime.Inspect(needleruntime.Options{Enabled: true, ModelPath: outDir})
	if !inspect.Usable || inspect.Mode != "q8_linear" || inspect.Weight == nil || inspect.Tokenizer == nil {
		t.Fatalf("Inspect = %#v, want usable q8 artifact", inspect)
	}
	if !inspect.Weight.SparseHashHead || inspect.Weight.EmbeddingBytes != 0 || inspect.Weight.HeadBytes != 64 || inspect.Weight.BiasBytes != 8 || inspect.Weight.ExpectedSize != 104 {
		t.Fatalf("Inspect weight = %#v, want sparse head shape metadata", inspect.Weight)
	}
	manifestData, err := os.ReadFile(filepath.Join(outDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if manifest["quant"] != "q8-sparse-hash-linear" || manifest["weight_header"] == nil {
		t.Fatalf("manifest = %#v, want sparse q8 metadata", manifest)
	}

	rt, err := needleruntime.New(needleruntime.Options{Enabled: true, ModelPath: outDir, MinConf: 0.1})
	if err != nil {
		t.Fatalf("needleruntime.New returned error: %v", err)
	}
	decision, ok, err := rt.Predict(context.Background(), needleruntime.Request{Task: needledata.EventWorkflowReview, Text: "approved go ahead", Choices: choicesForTask(needledata.EventWorkflowReview)})
	if err != nil {
		t.Fatalf("Predict returned error: %v", err)
	}
	if !ok || strings.TrimSpace(decision.Name) == "" || decision.Source != "needle_q8" {
		t.Fatalf("Predict = %#v ok=%v, want accepted q8 decision", decision, ok)
	}
}

func TestBuildHashedLinearArtifactRejectsNonPositiveDimension(t *testing.T) {
	records := []needledata.TrainingRecord{
		{ID: "confirm-1", Task: needledata.EventWorkflowReview, Messages: []needledata.ChatMessage{{Role: "user", Content: "looks good continue"}}, Expected: needledata.Decision{Name: "confirm"}},
	}
	if _, err := buildHashedLinearArtifact(records, 0); err == nil {
		t.Fatal("expected non-positive dimension to be rejected")
	}
}

func TestBuildHashedLinearArtifactRejectsMultipleTasks(t *testing.T) {
	records := []needledata.TrainingRecord{
		{ID: "workflow-1", Task: needledata.EventWorkflowReview, Messages: []needledata.ChatMessage{{Role: "user", Content: "looks good continue"}}, Expected: needledata.Decision{Name: "confirm"}},
		{ID: "intent-1", Task: needledata.EventIntentGate, Messages: []needledata.ChatMessage{{Role: "user", Content: "open browser"}}, Expected: needledata.Decision{Name: "route_browser"}},
	}
	if _, err := buildHashedLinearArtifact(records, 32); err == nil {
		t.Fatal("expected mixed-task artifact build to be rejected")
	}
}

func TestCompileGoArtifactsByTaskWritesCollection(t *testing.T) {
	records := []needledata.TrainingRecord{
		{ID: "workflow-1", Task: needledata.EventWorkflowReview, Messages: []needledata.ChatMessage{{Role: "user", Content: "looks good continue"}}, Expected: needledata.Decision{Name: "confirm"}},
		{ID: "workflow-2", Task: needledata.EventWorkflowReview, Messages: []needledata.ChatMessage{{Role: "user", Content: "cancel this"}}, Expected: needledata.Decision{Name: "cancel"}},
		{ID: "intent-1", Task: needledata.EventIntentGate, Messages: []needledata.ChatMessage{{Role: "user", Content: "open browser"}}, Expected: needledata.Decision{Name: "route_browser"}},
		{ID: "intent-2", Task: needledata.EventIntentGate, Messages: []needledata.ChatMessage{{Role: "user", Content: "just chatting"}}, Expected: needledata.Decision{Name: "no_call"}},
	}
	out := t.TempDir()
	if err := compileGoArtifactsByTask(records, out, 16, map[string]float64{needledata.EventWorkflowReview: 0.42}); err != nil {
		t.Fatalf("compileGoArtifactsByTask returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "collection.json")); err != nil {
		t.Fatalf("collection.json missing: %v", err)
	}
	for _, task := range []string{needledata.EventWorkflowReview, needledata.EventIntentGate} {
		artifactDir := filepath.Join(out, task)
		if inspect := needleruntime.Inspect(needleruntime.Options{Enabled: true, ModelPath: artifactDir}); !inspect.Usable || len(inspect.Manifest.Tasks) != 1 || inspect.Manifest.Tasks[0] != task {
			t.Fatalf("Inspect(%s) = %#v, want single-task usable artifact", task, inspect)
		}
	}
	collectionInspect := needleruntime.Inspect(needleruntime.Options{Enabled: true, ModelPath: out})
	if collectionInspect.Collection.Tasks[needledata.EventWorkflowReview].MinConf != 0.42 || collectionInspect.Collection.Tasks[needledata.EventWorkflowReview].RuntimeInspect.MinConf != 0.42 {
		t.Fatalf("collection workflow min_conf = %#v", collectionInspect.Collection.Tasks[needledata.EventWorkflowReview])
	}
}

func TestPromoteArtifactSupportsTaskCollection(t *testing.T) {
	t.Setenv("MACLAW_DATA_DIR", t.TempDir())
	records := []needledata.TrainingRecord{
		{ID: "workflow-1", Task: needledata.EventWorkflowReview, Messages: []needledata.ChatMessage{{Role: "user", Content: "looks good continue"}}, Expected: needledata.Decision{Name: "confirm"}},
		{ID: "workflow-2", Task: needledata.EventWorkflowReview, Messages: []needledata.ChatMessage{{Role: "user", Content: "cancel this"}}, Expected: needledata.Decision{Name: "cancel"}},
		{ID: "intent-1", Task: needledata.EventIntentGate, Messages: []needledata.ChatMessage{{Role: "user", Content: "open browser"}}, Expected: needledata.Decision{Name: "route_browser"}},
		{ID: "intent-2", Task: needledata.EventIntentGate, Messages: []needledata.ChatMessage{{Role: "user", Content: "just chatting"}}, Expected: needledata.Decision{Name: "no_call"}},
	}
	root := t.TempDir()
	recordsPath := filepath.Join(root, "records.jsonl")
	if err := needledata.WriteJSONL(recordsPath, records); err != nil {
		t.Fatalf("WriteJSONL records: %v", err)
	}
	candidate := filepath.Join(root, "candidate")
	if err := compileGoArtifactsByTask(records, candidate, 16, nil); err != nil {
		t.Fatalf("compileGoArtifactsByTask returned error: %v", err)
	}
	rt, err := needleruntime.New(needleruntime.Options{Enabled: true, ModelPath: filepath.Join(candidate, needledata.EventWorkflowReview), MinConf: 0.1})
	if err != nil {
		t.Fatalf("needleruntime.New returned error: %v", err)
	}
	predictionsPath := filepath.Join(root, "predictions.jsonl")
	if _, _, err := writeRuntimePredictions(predictionsPath, rt, filterRecordsByTasks(records, map[string]bool{needledata.EventWorkflowReview: true})); err != nil {
		t.Fatalf("writeRuntimePredictions returned error: %v", err)
	}
	if err := cmdPromoteArtifact([]string{"-model", candidate, "-records", recordsPath, "-predictions", predictionsPath, "-tasks", needledata.EventWorkflowReview, "-min-records", "1", "-min-accuracy", "0", "-min-task-accuracy", "0", "-max-mismatches", "100"}); err != nil {
		t.Fatalf("cmdPromoteArtifact returned error: %v", err)
	}
	active := needledata.DefaultModelDir(os.Getenv("MACLAW_DATA_DIR"))
	if _, err := os.Stat(filepath.Join(active, "collection.json")); err != nil {
		t.Fatalf("promoted collection missing: %v", err)
	}
	if inspect := needleruntime.Inspect(needleruntime.Options{Enabled: true, ModelPath: filepath.Join(active, needledata.EventWorkflowReview)}); !inspect.Usable {
		t.Fatalf("promoted workflow artifact inspect = %#v, want usable", inspect)
	}
}

func TestPredictRecordsUsesCollectionTaskModels(t *testing.T) {
	records := []needledata.TrainingRecord{
		{ID: "workflow-1", Task: needledata.EventWorkflowReview, Messages: []needledata.ChatMessage{{Role: "user", Content: "looks good continue"}}, Expected: needledata.Decision{Name: "confirm"}},
		{ID: "intent-1", Task: needledata.EventIntentGate, Messages: []needledata.ChatMessage{{Role: "user", Content: "open browser"}}, Expected: needledata.Decision{Name: "route_browser"}},
	}
	root := t.TempDir()
	candidate := filepath.Join(root, "candidate")
	if err := compileGoArtifactsByTask(records, candidate, 16, nil); err != nil {
		t.Fatalf("compileGoArtifactsByTask returned error: %v", err)
	}
	predictionsPath := filepath.Join(root, "predictions.jsonl")
	predictions, _, err := writeRuntimePredictionsForModel(predictionsPath, candidate, 0.1, records)
	if err != nil {
		t.Fatalf("writeRuntimePredictionsForModel returned error: %v", err)
	}
	if len(predictions) != len(records) {
		t.Fatalf("predictions len = %d, want %d", len(predictions), len(records))
	}
	for _, rec := range records {
		if strings.TrimSpace(predictions[rec.ID].Name) == "" {
			t.Fatalf("prediction for %s is empty: %#v", rec.ID, predictions[rec.ID])
		}
	}
}

func TestPredictRecordsWritesRejectReason(t *testing.T) {
	records := []needledata.TrainingRecord{
		{ID: "workflow-1", Task: needledata.EventWorkflowReview, Messages: []needledata.ChatMessage{{Role: "user", Content: "looks good continue"}}, Expected: needledata.Decision{Name: "confirm"}},
		{ID: "workflow-2", Task: needledata.EventWorkflowReview, Messages: []needledata.ChatMessage{{Role: "user", Content: "cancel this"}}, Expected: needledata.Decision{Name: "cancel"}},
	}
	root := t.TempDir()
	artifact, err := buildHashedLinearArtifact(records, 16)
	if err != nil {
		t.Fatalf("buildHashedLinearArtifact returned error: %v", err)
	}
	candidate := filepath.Join(root, "candidate")
	if err := writeGoArtifact(candidate, artifact); err != nil {
		t.Fatalf("writeGoArtifact returned error: %v", err)
	}
	predictionsPath := filepath.Join(root, "predictions.jsonl")
	_, accepted, err := writeRuntimePredictionsForModel(predictionsPath, candidate, 1.01, records)
	if err != nil {
		t.Fatalf("writeRuntimePredictionsForModel returned error: %v", err)
	}
	if accepted != 0 {
		t.Fatalf("accepted = %d, want all rejected by min confidence", accepted)
	}
	data, err := os.ReadFile(predictionsPath)
	if err != nil {
		t.Fatalf("read predictions: %v", err)
	}
	if !strings.Contains(string(data), `"reject_reason":"below_min_confidence"`) {
		t.Fatalf("predictions JSONL missing reject reason: %s", string(data))
	}
}

func TestUpdateCollectionMinConfFromCalibration(t *testing.T) {
	records := []needledata.TrainingRecord{
		{ID: "workflow-1", Task: needledata.EventWorkflowReview, Messages: []needledata.ChatMessage{{Role: "user", Content: "looks good continue"}}, Expected: needledata.Decision{Name: "confirm"}},
		{ID: "intent-1", Task: needledata.EventIntentGate, Messages: []needledata.ChatMessage{{Role: "user", Content: "open browser"}}, Expected: needledata.Decision{Name: "route_browser"}},
	}
	root := t.TempDir()
	candidate := filepath.Join(root, "candidate")
	if err := compileGoArtifactsByTask(records, candidate, 16, nil); err != nil {
		t.Fatalf("compileGoArtifactsByTask returned error: %v", err)
	}
	calibrations := map[string]needledata.TaskThresholdCalibration{
		needledata.EventWorkflowReview: {Task: needledata.EventWorkflowReview, RecommendedFound: true, Recommended: needledata.ThresholdPoint{Threshold: 0.82}},
		needledata.EventIntentGate:     {Task: needledata.EventIntentGate, RecommendedFound: false, Recommended: needledata.ThresholdPoint{Threshold: 0.91}},
	}
	updated, err := updateCollectionMinConf(candidate, calibrations)
	if err != nil {
		t.Fatalf("updateCollectionMinConf returned error: %v", err)
	}
	if updated[needledata.EventWorkflowReview] != 0.82 || updated[needledata.EventIntentGate] != 0 {
		t.Fatalf("updated = %#v", updated)
	}
	inspect := needleruntime.Inspect(needleruntime.Options{Enabled: true, ModelPath: candidate})
	if inspect.Collection.Tasks[needledata.EventWorkflowReview].MinConf != 0.82 {
		t.Fatalf("workflow min_conf = %#v", inspect.Collection.Tasks[needledata.EventWorkflowReview])
	}
}

func TestCalibrateThresholdWritesReportForCollection(t *testing.T) {
	records := []needledata.TrainingRecord{
		{ID: "workflow-1", Task: needledata.EventWorkflowReview, Messages: []needledata.ChatMessage{{Role: "user", Content: "looks good continue"}}, Expected: needledata.Decision{Name: "confirm"}},
		{ID: "workflow-2", Task: needledata.EventWorkflowReview, Messages: []needledata.ChatMessage{{Role: "user", Content: "cancel this"}}, Expected: needledata.Decision{Name: "cancel"}},
	}
	root := t.TempDir()
	candidate := filepath.Join(root, "candidate")
	if err := compileGoArtifactsByTask(records, candidate, 16, nil); err != nil {
		t.Fatalf("compileGoArtifactsByTask returned error: %v", err)
	}
	recordsPath := filepath.Join(root, "records.jsonl")
	if err := needledata.WriteJSONL(recordsPath, records); err != nil {
		t.Fatalf("WriteJSONL returned error: %v", err)
	}
	predictionsPath := filepath.Join(root, "predictions.jsonl")
	if err := writeJSONL(predictionsPath, []needledata.PredictionRecord{
		{ID: "workflow-1", Decision: needledata.Decision{Name: "confirm", Confidence: 0.95}, Accepted: true},
		{ID: "workflow-2", Decision: needledata.Decision{Name: "cancel", Confidence: 0.90}, Accepted: true},
	}); err != nil {
		t.Fatalf("write predictions: %v", err)
	}
	if err := cmdCalibrateThreshold([]string{"-records", recordsPath, "-predictions", predictionsPath, "-update-collection", candidate, "-thresholds", "0.5,0.9", "-min-accuracy", "1", "-min-accepted-ratio", "0.5"}); err != nil {
		t.Fatalf("cmdCalibrateThreshold returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(candidate, "calibration.json")); err != nil {
		t.Fatalf("calibration report missing: %v", err)
	}
	inspect := needleruntime.Inspect(needleruntime.Options{Enabled: true, ModelPath: candidate})
	if inspect.Collection.Tasks[needledata.EventWorkflowReview].MinConf != 0.5 {
		t.Fatalf("workflow min_conf = %#v, want 0.5", inspect.Collection.Tasks[needledata.EventWorkflowReview])
	}
}

func TestInspectCollectionArtifactSummarizesTasks(t *testing.T) {
	records := []needledata.TrainingRecord{
		{ID: "workflow-1", Task: needledata.EventWorkflowReview, Messages: []needledata.ChatMessage{{Role: "user", Content: "looks good continue"}}, Expected: needledata.Decision{Name: "confirm"}},
		{ID: "intent-1", Task: needledata.EventIntentGate, Messages: []needledata.ChatMessage{{Role: "user", Content: "open browser"}}, Expected: needledata.Decision{Name: "route_browser"}},
	}
	root := t.TempDir()
	candidate := filepath.Join(root, "candidate")
	if err := compileGoArtifactsByTask(records, candidate, 16, nil); err != nil {
		t.Fatalf("compileGoArtifactsByTask returned error: %v", err)
	}
	got := inspectCollectionArtifact(candidate, true, 0.78)
	if got == nil || got["collection"] != true || got["usable"] != true {
		t.Fatalf("inspectCollectionArtifact = %#v, want usable collection", got)
	}
	tasks, ok := got["tasks"].(map[string]any)
	if !ok || len(tasks) != 2 || tasks[needledata.EventWorkflowReview] == nil || tasks[needledata.EventIntentGate] == nil {
		t.Fatalf("collection tasks = %#v, want workflow and intent", got["tasks"])
	}
}

func TestPromoteArtifactCopiesCandidateAfterQualityGate(t *testing.T) {
	t.Setenv("MACLAW_DATA_DIR", t.TempDir())
	records := []needledata.TrainingRecord{
		{ID: "confirm-1", Task: needledata.EventWorkflowReview, Messages: []needledata.ChatMessage{{Role: "user", Content: "looks good continue"}}, Expected: needledata.Decision{Name: "confirm"}},
		{ID: "cancel-1", Task: needledata.EventWorkflowReview, Messages: []needledata.ChatMessage{{Role: "user", Content: "cancel this workflow"}}, Expected: needledata.Decision{Name: "cancel"}},
	}
	root := t.TempDir()
	recordsPath := filepath.Join(root, "records.jsonl")
	if err := needledata.WriteJSONL(recordsPath, records); err != nil {
		t.Fatalf("WriteJSONL records: %v", err)
	}
	artifact, err := buildHashedLinearArtifact(records, 32)
	if err != nil {
		t.Fatalf("buildHashedLinearArtifact returned error: %v", err)
	}
	candidate := filepath.Join(root, "candidate")
	if err := writeGoArtifact(candidate, artifact); err != nil {
		t.Fatalf("writeGoArtifact returned error: %v", err)
	}
	rt, err := needleruntime.New(needleruntime.Options{Enabled: true, ModelPath: candidate, MinConf: 0.1})
	if err != nil {
		t.Fatalf("needleruntime.New returned error: %v", err)
	}
	predictionsPath := filepath.Join(root, "predictions.jsonl")
	if _, _, err := writeRuntimePredictions(predictionsPath, rt, records); err != nil {
		t.Fatalf("writeRuntimePredictions returned error: %v", err)
	}
	active := needledata.DefaultModelDir(os.Getenv("MACLAW_DATA_DIR"))
	if err := cmdPromoteArtifact([]string{"-model", candidate, "-records", recordsPath, "-predictions", predictionsPath, "-min-records", "1", "-min-accuracy", "0", "-min-task-accuracy", "0", "-max-mismatches", "100"}); err != nil {
		t.Fatalf("cmdPromoteArtifact returned error: %v", err)
	}
	for _, name := range []string{"manifest.json", "needle.q8", "tokenizer.json", "labels.json", "promotion.json"} {
		if _, err := os.Stat(filepath.Join(active, name)); err != nil {
			t.Fatalf("promoted artifact missing %s: %v", name, err)
		}
	}
	if inspect := needleruntime.Inspect(needleruntime.Options{Enabled: true, ModelPath: active}); !inspect.Usable {
		t.Fatalf("promoted artifact inspect = %#v, want usable", inspect)
	}
}

func TestFilterRecordsByTasks(t *testing.T) {
	records := []needledata.TrainingRecord{
		{ID: "1", Task: needledata.EventWorkflowReview},
		{ID: "2", Task: needledata.EventIntentGate},
	}
	got := filterRecordsByTasks(records, map[string]bool{needledata.EventIntentGate: true})
	if len(got) != 1 || got[0].ID != "2" {
		t.Fatalf("filterRecordsByTasks = %#v, want only intent record", got)
	}
	got = filterRecordsByTasks(records, nil)
	if len(got) != len(records) {
		t.Fatalf("filterRecordsByTasks(nil) len = %d, want %d", len(got), len(records))
	}
}

func TestResolveModelPathForTaskDetailedReportsMissingCollectionTask(t *testing.T) {
	records := []needledata.TrainingRecord{
		{ID: "workflow-1", Task: needledata.EventWorkflowReview, Messages: []needledata.ChatMessage{{Role: "user", Content: "looks good continue"}}, Expected: needledata.Decision{Name: "confirm"}},
	}
	root := t.TempDir()
	candidate := filepath.Join(root, "candidate")
	if err := compileGoArtifactsByTask(records, candidate, 16, nil); err != nil {
		t.Fatalf("compileGoArtifactsByTask returned error: %v", err)
	}
	_, ok, err := resolveModelPathForTaskDetailed(candidate, needledata.EventIntentGate)
	if err == nil || ok {
		t.Fatalf("resolveModelPathForTaskDetailed missing task ok=%v err=%v, want explicit error", ok, err)
	}
	if !strings.Contains(err.Error(), "available tasks") || !strings.Contains(err.Error(), needledata.EventWorkflowReview) {
		t.Fatalf("missing task error = %v", err)
	}
}

func TestResolveModelPathForTaskDetailedReportsMissingManifest(t *testing.T) {
	root := t.TempDir()
	collection := map[string]any{
		"format":  "maclaw-needle-collection",
		"version": 1,
		"tasks": map[string]any{
			needledata.EventWorkflowReview: map[string]any{"path": "workflow_review"},
		},
	}
	data, err := json.Marshal(collection)
	if err != nil {
		t.Fatalf("marshal collection: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "collection.json"), data, 0o644); err != nil {
		t.Fatalf("write collection: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "workflow_review"), 0o755); err != nil {
		t.Fatalf("mkdir task artifact: %v", err)
	}
	_, ok, err := resolveModelPathForTaskDetailed(root, needledata.EventWorkflowReview)
	if err == nil || !ok {
		t.Fatalf("resolveModelPathForTaskDetailed missing manifest ok=%v err=%v, want explicit error", ok, err)
	}
	if !strings.Contains(err.Error(), "missing manifest") {
		t.Fatalf("missing manifest error = %v", err)
	}
}

func TestSmokePipelineAllTasksBuildsCollectionAndCalibration(t *testing.T) {
	out := t.TempDir()
	if err := cmdSmokePipeline([]string{"-out-dir", out, "-predict-task", "", "-per-intent", "1", "-per-review", "1", "-min-conf", "0.1", "-mismatches", "5"}); err != nil {
		t.Fatalf("cmdSmokePipeline returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "compiled-go-artifact", "collection.json")); err != nil {
		t.Fatalf("smoke collection missing: %v", err)
	}
	predictions, err := needledata.ReadPredictionRecords(filepath.Join(out, "predictions.jsonl"))
	if err != nil {
		t.Fatalf("ReadPredictionRecords returned error: %v", err)
	}
	if len(predictions) == 0 {
		t.Fatal("smoke pipeline wrote no predictions")
	}
}

func TestRunPipelineWritesCandidateCalibrationAndPredictions(t *testing.T) {
	records := []needledata.TrainingRecord{
		{ID: "workflow-1", Task: needledata.EventWorkflowReview, Messages: []needledata.ChatMessage{{Role: "user", Content: "looks good continue"}}, Expected: needledata.Decision{Name: "confirm"}},
		{ID: "workflow-2", Task: needledata.EventWorkflowReview, Messages: []needledata.ChatMessage{{Role: "user", Content: "cancel this"}}, Expected: needledata.Decision{Name: "cancel"}},
		{ID: "intent-1", Task: needledata.EventIntentGate, Messages: []needledata.ChatMessage{{Role: "user", Content: "open browser"}}, Expected: needledata.Decision{Name: "route_browser"}},
		{ID: "intent-2", Task: needledata.EventIntentGate, Messages: []needledata.ChatMessage{{Role: "user", Content: "just chatting"}}, Expected: needledata.Decision{Name: "no_call"}},
	}
	root := t.TempDir()
	recordsPath := filepath.Join(root, "records.jsonl")
	if err := needledata.WriteJSONL(recordsPath, records); err != nil {
		t.Fatalf("WriteJSONL returned error: %v", err)
	}
	out := filepath.Join(root, "pipeline")
	if err := cmdRunPipeline([]string{"-records", recordsPath, "-out-dir", out, "-dim", "16", "-min-conf", "0.1", "-min-records", "1", "-min-accuracy", "0", "-min-task-accuracy", "0", "-min-accepted-ratio", "0", "-max-rejected-ratio", "0", "-max-missing-ratio", "0", "-max-mismatches", "100"}); err != nil {
		t.Fatalf("cmdRunPipeline returned error: %v", err)
	}
	for _, path := range []string{filepath.Join(out, "candidate", "collection.json"), filepath.Join(out, "candidate", "calibration.json"), filepath.Join(out, "predictions.jsonl"), filepath.Join(out, "train.jsonl"), filepath.Join(out, "eval.jsonl")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected pipeline artifact %s: %v", path, err)
		}
	}
	inspect := needleruntime.Inspect(needleruntime.Options{Enabled: true, ModelPath: filepath.Join(out, "candidate")})
	if inspect.Collection == nil || len(inspect.Collection.Tasks) != 2 {
		t.Fatalf("candidate inspect = %#v", inspect)
	}
}

func TestRunPipelineHoldoutWritesTrainAndEvalSplits(t *testing.T) {
	records := []needledata.TrainingRecord{
		{ID: "workflow-1", Task: needledata.EventWorkflowReview, Messages: []needledata.ChatMessage{{Role: "user", Content: "looks good continue"}}, Expected: needledata.Decision{Name: "confirm"}},
		{ID: "workflow-2", Task: needledata.EventWorkflowReview, Messages: []needledata.ChatMessage{{Role: "user", Content: "approved"}}, Expected: needledata.Decision{Name: "confirm"}},
		{ID: "workflow-3", Task: needledata.EventWorkflowReview, Messages: []needledata.ChatMessage{{Role: "user", Content: "cancel this"}}, Expected: needledata.Decision{Name: "cancel"}},
		{ID: "workflow-4", Task: needledata.EventWorkflowReview, Messages: []needledata.ChatMessage{{Role: "user", Content: "stop"}}, Expected: needledata.Decision{Name: "cancel"}},
	}
	root := t.TempDir()
	recordsPath := filepath.Join(root, "records.jsonl")
	if err := needledata.WriteJSONL(recordsPath, records); err != nil {
		t.Fatalf("WriteJSONL returned error: %v", err)
	}
	out := filepath.Join(root, "pipeline")
	if err := cmdRunPipeline([]string{"-records", recordsPath, "-out-dir", out, "-tasks", needledata.EventWorkflowReview, "-holdout", "0.5", "-dim", "16", "-min-conf", "0.1", "-min-records", "1", "-min-accuracy", "0", "-min-task-accuracy", "0", "-min-accepted-ratio", "0", "-max-rejected-ratio", "0", "-max-missing-ratio", "0", "-max-mismatches", "100"}); err != nil {
		t.Fatalf("cmdRunPipeline returned error: %v", err)
	}
	train, err := needledata.ReadTrainingRecords(filepath.Join(out, "train.jsonl"))
	if err != nil {
		t.Fatalf("read train split: %v", err)
	}
	eval, err := needledata.ReadTrainingRecords(filepath.Join(out, "eval.jsonl"))
	if err != nil {
		t.Fatalf("read eval split: %v", err)
	}
	if len(train) != 2 || len(eval) != 2 {
		t.Fatalf("split sizes train=%d eval=%d, want 2/2", len(train), len(eval))
	}
}

func writeJSONL[T any](path string, records []T) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, rec := range records {
		if err := enc.Encode(rec); err != nil {
			return err
		}
	}
	return nil
}
