package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib/needledata"
	"github.com/RapidAI/CodeClaw/corelib/needleruntime"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "generate-seed":
		err = cmdGenerateSeed(os.Args[2:])
	case "export-dataset":
		err = cmdExportDataset(os.Args[2:])
	case "export-needle":
		err = cmdExportNeedle(os.Args[2:])
	case "compile-go-artifact":
		err = cmdCompileGoArtifact(os.Args[2:])
	case "inspect-logs":
		err = cmdInspectLogs(os.Args[2:])
	case "report-dataset":
		err = cmdReportDataset(os.Args[2:])
	case "eval-shadow":
		err = cmdEvalShadow(os.Args[2:])
	case "eval-predictions":
		err = cmdEvalPredictions(os.Args[2:])
	case "eval-localization":
		err = cmdEvalLocalization(os.Args[2:])
	case "quality-gate":
		err = cmdQualityGate(os.Args[2:])
	case "calibrate-threshold":
		err = cmdCalibrateThreshold(os.Args[2:])
	case "promote-artifact":
		err = cmdPromoteArtifact(os.Args[2:])
	case "run-pipeline":
		err = cmdRunPipeline(os.Args[2:])
	case "active-path":
		err = cmdActivePath(os.Args[2:])
	case "inspect-runtime":
		err = cmdInspectRuntime(os.Args[2:])
	case "encode":
		err = cmdEncode(os.Args[2:])
	case "predict":
		err = cmdPredict(os.Args[2:])
	case "predict-records":
		err = cmdPredictRecords(os.Args[2:])
	case "bench-runtime":
		err = cmdBenchRuntime(os.Args[2:])
	case "smoke-pipeline":
		err = cmdSmokePipeline(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func cmdEvalLocalization(args []string) error {
	fs := flag.NewFlagSet("eval-localization", flag.ExitOnError)
	in := fs.String("in", "", "bug-localization prediction JSONL")
	out := fs.String("out", "", "optional JSON report path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*in) == "" {
		return fmt.Errorf("-in is required")
	}
	rows, err := needledata.ReadLocalizationPredictions(*in)
	if err != nil {
		return err
	}
	report := needledata.EvaluateLocalizations(rows)
	data, _ := json.MarshalIndent(report, "", "  ")
	if *out != "" {
		if err := os.WriteFile(*out, data, 0o644); err != nil {
			return err
		}
	}
	fmt.Println(string(data))
	return nil
}

func cmdGenerateSeed(args []string) error {
	fs := flag.NewFlagSet("generate-seed", flag.ExitOnError)
	out := fs.String("out", filepath.Join("data", "needle", "seed.jsonl"), "output JSONL path")
	perIntent := fs.Int("per-intent", 12, "synthetic examples per intent label")
	perReview := fs.Int("per-review", 16, "synthetic examples per workflow review label")
	seed := fs.Int64("seed", 7, "deterministic random seed")
	if err := fs.Parse(args); err != nil {
		return err
	}
	records := needledata.GenerateSeedRecords(needledata.GenerateOptions{PerIntent: *perIntent, PerWorkflowReview: *perReview, Seed: *seed})
	if err := needledata.WriteJSONL(*out, records); err != nil {
		return err
	}
	fmt.Printf("wrote %d records to %s\n", len(records), *out)
	return nil
}

func cmdExportDataset(args []string) error {
	fs := flag.NewFlagSet("export-dataset", flag.ExitOnError)
	in := fs.String("in", "", "event JSONL file or directory")
	out := fs.String("out", filepath.Join("data", "needle", "train.jsonl"), "training JSONL path")
	evalOut := fs.String("eval-out", "", "optional eval JSONL path")
	typesCSV := fs.String("types", "", "comma-separated event types to include")
	onlySuccess := fs.Bool("only-success", true, "only export successful, uncorrected events")
	deduplicate := fs.Bool("deduplicate", true, "drop duplicate task/input records during export")
	maxRecords := fs.Int("max", 0, "maximum records to export")
	holdout := fs.Float64("holdout", 0.1, "holdout ratio when eval-out is set")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*in) == "" {
		return fmt.Errorf("-in is required")
	}
	events, err := needledata.ReadEvents([]string{*in})
	if err != nil {
		return err
	}
	records := needledata.EventsToTrainingRecords(events, needledata.ExportOptions{IncludeTypes: parseTypes(*typesCSV), OnlySuccess: *onlySuccess, MaxRecords: *maxRecords, Deduplicate: *deduplicate})
	train := records
	var eval []needledata.TrainingRecord
	if *evalOut != "" {
		train, eval = needledata.SplitHoldout(records, *holdout)
	}
	if err := needledata.WriteJSONL(*out, train); err != nil {
		return err
	}
	if *evalOut != "" {
		if err := needledata.WriteJSONL(*evalOut, eval); err != nil {
			return err
		}
	}
	fmt.Printf("read %d events, exported %d train", len(events), len(train))
	if *evalOut != "" {
		fmt.Printf(", %d eval", len(eval))
	}
	fmt.Println()
	return nil
}

func cmdExportNeedle(args []string) error {
	fs := flag.NewFlagSet("export-needle", flag.ExitOnError)
	in := fs.String("in", "", "training JSONL file exported by generate-seed or export-dataset")
	out := fs.String("out", filepath.Join("data", "needle", "needle_train.jsonl"), "Needle-format JSONL path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*in) == "" {
		return fmt.Errorf("-in is required")
	}
	records, err := needledata.ReadTrainingRecords(*in)
	if err != nil {
		return err
	}
	if err := needledata.WriteNeedleJSONL(*out, records); err != nil {
		return err
	}
	fmt.Printf("wrote %d Needle examples to %s\n", len(records), *out)
	return nil
}

func cmdCompileGoArtifact(args []string) error {
	fs := flag.NewFlagSet("compile-go-artifact", flag.ExitOnError)
	in := fs.String("in", "", "training JSONL file exported by generate-seed or export-dataset")
	outDir := fs.String("out", filepath.Join("models", "needle-go-artifact"), "output MacLaw Needle artifact directory")
	tasksCSV := fs.String("tasks", "", "optional comma-separated task names to include")
	dim := fs.Int("dim", 64, "hashed embedding dimensions")
	splitByTask := fs.Bool("split-by-task", false, "write one artifact subdirectory per selected task")
	minConfCSV := fs.String("task-min-conf", "", "optional task=min_conf pairs for collection artifacts")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*in) == "" {
		return fmt.Errorf("-in is required")
	}
	if *dim <= 0 || *dim > 4096 {
		return fmt.Errorf("-dim must be between 1 and 4096")
	}
	records, err := needledata.ReadTrainingRecords(*in)
	if err != nil {
		return err
	}
	selected := filterRecordsByTasks(records, parseTypes(*tasksCSV))
	if len(selected) == 0 {
		return fmt.Errorf("no training records selected")
	}
	if *splitByTask {
		return compileGoArtifactsByTask(selected, *outDir, *dim, parseTaskFloatMap(*minConfCSV))
	}
	artifact, err := buildHashedLinearArtifact(selected, *dim)
	if err != nil {
		return err
	}
	if err := writeGoArtifact(*outDir, artifact); err != nil {
		return err
	}
	printJSON(map[string]any{
		"records":         len(selected),
		"tasks":           artifact.Tasks,
		"labels":          artifact.Labels,
		"dim":             artifact.Dim,
		"out":             *outDir,
		"runtime_inspect": needleruntime.Inspect(needleruntime.Options{Enabled: true, ModelPath: *outDir}),
	})
	return nil
}

func compileGoArtifactsByTask(records []needledata.TrainingRecord, outDir string, dim int, taskMinConf map[string]float64) error {
	groups := map[string][]needledata.TrainingRecord{}
	for _, rec := range records {
		task := strings.TrimSpace(rec.Task)
		if task == "" {
			continue
		}
		groups[task] = append(groups[task], rec)
	}
	if len(groups) == 0 {
		return fmt.Errorf("no task-tagged training records selected")
	}
	root := map[string]any{
		"format":  "maclaw-needle-collection",
		"version": needleruntime.ArtifactVersion,
		"dim":     dim,
		"tasks":   map[string]any{},
	}
	taskEntries := root["tasks"].(map[string]any)
	compiled := make([]map[string]any, 0, len(groups))
	for _, task := range sortedKeys(groupsToSet(groups)) {
		artifact, err := buildHashedLinearArtifact(groups[task], dim)
		if err != nil {
			return fmt.Errorf("compile task %s: %w", task, err)
		}
		taskDir := filepath.Join(outDir, safePathName(task))
		if err := writeGoArtifact(taskDir, artifact); err != nil {
			return fmt.Errorf("write task %s artifact: %w", task, err)
		}
		inspect := needleruntime.Inspect(needleruntime.Options{Enabled: true, ModelPath: taskDir})
		entry := map[string]any{"task": task, "records": len(groups[task]), "out": taskDir, "labels": artifact.Labels, "runtime_inspect": inspect}
		compiled = append(compiled, entry)
		taskEntry := map[string]any{"path": safePathName(task), "records": len(groups[task]), "labels": artifact.Labels}
		if minConf := taskMinConf[task]; minConf > 0 {
			taskEntry["min_conf"] = minConf
			entry["min_conf"] = minConf
		}
		taskEntries[task] = taskEntry
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(outDir, "collection.json"), root); err != nil {
		return err
	}
	printJSON(map[string]any{"out": outDir, "collection": filepath.Join(outDir, "collection.json"), "artifacts": compiled})
	return nil
}

func cmdInspectLogs(args []string) error {
	fs := flag.NewFlagSet("inspect-logs", flag.ExitOnError)
	in := fs.String("in", "", "event JSONL file or directory")
	mismatches := fs.Int("mismatches", 20, "maximum shadow mismatches to include; 0 means all")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*in) == "" {
		return fmt.Errorf("-in is required")
	}
	events, err := needledata.ReadEvents([]string{*in})
	if err != nil {
		return err
	}
	printJSON(map[string]any{"report": needledata.BuildEventReport(events), "shadow_eval": needledata.EvaluateShadowEvents(events, *mismatches)})
	return nil
}

func cmdReportDataset(args []string) error {
	fs := flag.NewFlagSet("report-dataset", flag.ExitOnError)
	in := fs.String("in", "", "training JSONL file or event JSONL file/directory")
	events := fs.Bool("events", false, "treat input as event logs instead of training records")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*in) == "" {
		return fmt.Errorf("-in is required")
	}
	if *events {
		es, err := needledata.ReadEvents([]string{*in})
		if err != nil {
			return err
		}
		printJSON(needledata.BuildEventReport(es))
		return nil
	}
	records, err := needledata.ReadTrainingRecords(*in)
	if err != nil {
		return err
	}
	printJSON(needledata.BuildTrainingReport(records))
	return nil
}

func cmdEvalShadow(args []string) error {
	fs := flag.NewFlagSet("eval-shadow", flag.ExitOnError)
	in := fs.String("in", "", "event JSONL file or directory")
	mismatches := fs.Int("mismatches", 20, "maximum mismatches to print; 0 means all")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*in) == "" {
		return fmt.Errorf("-in is required")
	}
	events, err := needledata.ReadEvents([]string{*in})
	if err != nil {
		return err
	}
	printJSON(needledata.EvaluateShadowEvents(events, *mismatches))
	return nil
}

func cmdEvalPredictions(args []string) error {
	fs := flag.NewFlagSet("eval-predictions", flag.ExitOnError)
	recordsPath := fs.String("records", "", "training/eval records JSONL")
	predPath := fs.String("predictions", "", "prediction JSONL with id/name or id/decision")
	tasksCSV := fs.String("tasks", "", "optional comma-separated task names to evaluate")
	mismatches := fs.Int("mismatches", 20, "maximum mismatches to print; 0 means all")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*recordsPath) == "" || strings.TrimSpace(*predPath) == "" {
		return fmt.Errorf("-records and -predictions are required")
	}
	records, err := needledata.ReadTrainingRecords(*recordsPath)
	if err != nil {
		return err
	}
	records = filterRecordsByTasks(records, parseTypes(*tasksCSV))
	if len(records) == 0 {
		return fmt.Errorf("no records selected")
	}
	preds, err := needledata.ReadPredictionRecords(*predPath)
	if err != nil {
		return err
	}
	printJSON(needledata.EvaluateTrainingPredictionRecords(records, preds, *mismatches))
	return nil
}

func cmdQualityGate(args []string) error {
	fs := flag.NewFlagSet("quality-gate", flag.ExitOnError)
	recordsPath := fs.String("records", "", "training/eval records JSONL")
	predPath := fs.String("predictions", "", "prediction JSONL with id/name or id/decision")
	tasksCSV := fs.String("tasks", "", "optional comma-separated task names to evaluate")
	minRecords := fs.Int("min-records", 200, "minimum dataset records required")
	minAccuracy := fs.Float64("min-accuracy", 0.92, "minimum overall accuracy")
	minTaskAccuracy := fs.Float64("min-task-accuracy", 0.88, "minimum accuracy for every task with predictions")
	minAcceptedRatio := fs.Float64("min-accepted-ratio", 0.5, "minimum accepted prediction ratio across selected records; 0 disables")
	maxRejectedRatio := fs.Float64("max-rejected-ratio", 0.5, "maximum rejected prediction ratio across selected records; 0 disables")
	maxMissingRatio := fs.Float64("max-missing-ratio", 0.05, "maximum missing prediction ratio across selected records; 0 disables")
	maxMismatches := fs.Int("max-mismatches", 20, "maximum mismatches retained before failing; -1 disables")
	mismatches := fs.Int("mismatches", 1000, "maximum mismatches to read into the eval summary")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*recordsPath) == "" || strings.TrimSpace(*predPath) == "" {
		return fmt.Errorf("-records and -predictions are required")
	}
	records, err := needledata.ReadTrainingRecords(*recordsPath)
	if err != nil {
		return err
	}
	records = filterRecordsByTasks(records, parseTypes(*tasksCSV))
	if len(records) == 0 {
		return fmt.Errorf("no records selected")
	}
	preds, err := needledata.ReadPredictionRecords(*predPath)
	if err != nil {
		return err
	}
	eval := needledata.EvaluateTrainingPredictionRecords(records, preds, *mismatches)
	report := needledata.BuildTrainingReport(records)
	gate := needledata.ApplyQualityGate(eval, report, needledata.QualityGateConfig{MinRecords: *minRecords, MinAccuracy: *minAccuracy, MinTaskAccuracy: *minTaskAccuracy, MinAcceptedRatio: *minAcceptedRatio, MaxRejectedRatio: *maxRejectedRatio, MaxMissingRatio: *maxMissingRatio, MaxMismatches: *maxMismatches})
	printJSON(gate)
	if !gate.Passed {
		return fmt.Errorf("quality gate failed: %s", strings.Join(gate.Reasons, ","))
	}
	return nil
}

func cmdCalibrateThreshold(args []string) error {
	fs := flag.NewFlagSet("calibrate-threshold", flag.ExitOnError)
	recordsPath := fs.String("records", "", "training/eval records JSONL")
	predPath := fs.String("predictions", "", "prediction JSONL with decision confidence")
	tasksCSV := fs.String("tasks", "", "optional comma-separated task names to evaluate")
	thresholdsCSV := fs.String("thresholds", "", "optional comma-separated thresholds; defaults to 0.50..0.95")
	minAccuracy := fs.Float64("min-accuracy", 0.92, "minimum accuracy for recommended threshold")
	minAcceptedRatio := fs.Float64("min-accepted-ratio", 0.5, "minimum accepted ratio for recommended threshold")
	byTask := fs.Bool("by-task", false, "also compute recommended thresholds per task")
	updateCollection := fs.String("update-collection", "", "optional collection artifact root/collection.json to update with per-task recommended min_conf")
	reportPath := fs.String("report", "", "optional calibration report JSON path; defaults to calibration.json beside updated collection")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*recordsPath) == "" || strings.TrimSpace(*predPath) == "" {
		return fmt.Errorf("-records and -predictions are required")
	}
	records, err := needledata.ReadTrainingRecords(*recordsPath)
	if err != nil {
		return err
	}
	records = filterRecordsByTasks(records, parseTypes(*tasksCSV))
	if len(records) == 0 {
		return fmt.Errorf("no records selected")
	}
	preds, err := needledata.ReadPredictionRecords(*predPath)
	if err != nil {
		return err
	}
	thresholds := parseFloatCSV(*thresholdsCSV)
	points := needledata.CalibrateThresholds(records, preds, thresholds)
	best, ok := needledata.BestThreshold(points, *minAccuracy, *minAcceptedRatio)
	result := map[string]any{"thresholds": points, "recommended": best, "recommended_found": ok, "min_accuracy": *minAccuracy, "min_accepted_ratio": *minAcceptedRatio}
	if *byTask || strings.TrimSpace(*updateCollection) != "" {
		calibrations := needledata.CalibrateThresholdsByTask(records, preds, thresholds, *minAccuracy, *minAcceptedRatio)
		result["by_task"] = calibrations
		if strings.TrimSpace(*updateCollection) != "" {
			updated, err := updateCollectionMinConf(*updateCollection, calibrations)
			if err != nil {
				return err
			}
			result["updated_collection"] = updated
		}
	}
	if strings.TrimSpace(*reportPath) == "" && strings.TrimSpace(*updateCollection) != "" {
		*reportPath = filepath.Join(collectionDir(*updateCollection), "calibration.json")
	}
	if strings.TrimSpace(*reportPath) != "" {
		result["report_path"] = *reportPath
		if err := writeJSON(*reportPath, result); err != nil {
			return err
		}
	}
	printJSON(result)
	return nil
}

func cmdActivePath(args []string) error {
	fs := flag.NewFlagSet("active-path", flag.ExitOnError)
	dataDir := fs.String("data-dir", defaultDataDir(), "MacLaw data directory")
	task := fs.String("task", "", "optional task name to resolve inside an active collection")
	if err := fs.Parse(args); err != nil {
		return err
	}
	active := needledata.DefaultModelDir(*dataDir)
	selectedTask := strings.TrimSpace(*task)
	resolved, taskResolved, err := resolveModelPathForTaskDetailed(active, selectedTask)
	if err != nil {
		return err
	}
	printJSON(map[string]any{"data_dir": *dataDir, "active_model_path": active, "task": selectedTask, "task_resolved": taskResolved, "resolved_model_path": resolved})
	return nil
}

func cmdPromoteArtifact(args []string) error {
	fs := flag.NewFlagSet("promote-artifact", flag.ExitOnError)
	modelPath := fs.String("model", "", "candidate local Needle artifact directory or manifest.json")
	recordsPath := fs.String("records", "", "training/eval records JSONL")
	predPath := fs.String("predictions", "", "prediction JSONL with id/name or id/decision")
	outDir := fs.String("out", "", "destination active artifact directory; defaults to <data-dir>/needle/models/active")
	dataDir := fs.String("data-dir", defaultDataDir(), "MacLaw data directory used when -out is empty")
	tasksCSV := fs.String("tasks", "", "optional comma-separated task names to evaluate")
	minRecords := fs.Int("min-records", 200, "minimum dataset records required")
	minAccuracy := fs.Float64("min-accuracy", 0.92, "minimum overall accuracy")
	minTaskAccuracy := fs.Float64("min-task-accuracy", 0.88, "minimum accuracy for every task with predictions")
	minAcceptedRatio := fs.Float64("min-accepted-ratio", 0.5, "minimum accepted prediction ratio across selected records; 0 disables")
	maxRejectedRatio := fs.Float64("max-rejected-ratio", 0.5, "maximum rejected prediction ratio across selected records; 0 disables")
	maxMissingRatio := fs.Float64("max-missing-ratio", 0.05, "maximum missing prediction ratio across selected records; 0 disables")
	maxMismatches := fs.Int("max-mismatches", 20, "maximum mismatches retained before failing; -1 disables")
	mismatches := fs.Int("mismatches", 1000, "maximum mismatches to read into the eval summary")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*modelPath) == "" || strings.TrimSpace(*recordsPath) == "" || strings.TrimSpace(*predPath) == "" {
		return fmt.Errorf("-model, -records, and -predictions are required")
	}
	if strings.TrimSpace(*outDir) == "" {
		*outDir = needledata.DefaultModelDir(*dataDir)
	}
	selectedTasks := parseTypes(*tasksCSV)
	inspect, err := inspectCandidateArtifact(*modelPath, selectedTasks)
	if err != nil {
		return err
	}
	records, err := needledata.ReadTrainingRecords(*recordsPath)
	if err != nil {
		return err
	}
	records = filterRecordsByTasks(records, selectedTasks)
	if len(records) == 0 {
		return fmt.Errorf("no records selected")
	}
	preds, err := needledata.ReadPredictionRecords(*predPath)
	if err != nil {
		return err
	}
	eval := needledata.EvaluateTrainingPredictionRecords(records, preds, *mismatches)
	report := needledata.BuildTrainingReport(records)
	gate := needledata.ApplyQualityGate(eval, report, needledata.QualityGateConfig{MinRecords: *minRecords, MinAccuracy: *minAccuracy, MinTaskAccuracy: *minTaskAccuracy, MinAcceptedRatio: *minAcceptedRatio, MaxRejectedRatio: *maxRejectedRatio, MaxMissingRatio: *maxMissingRatio, MaxMismatches: *maxMismatches})
	if !gate.Passed {
		printJSON(map[string]any{"promoted": false, "quality_gate": gate, "runtime_inspect": inspect})
		return fmt.Errorf("quality gate failed: %s", strings.Join(gate.Reasons, ","))
	}
	if err := copyArtifactDir(*modelPath, *outDir); err != nil {
		return err
	}
	writeJSON(filepath.Join(*outDir, "promotion.json"), map[string]any{
		"promoted_at":     time.Now().UTC().Format(time.RFC3339),
		"source":          *modelPath,
		"quality_gate":    gate,
		"runtime_inspect": inspect,
		"calibration":     readOptionalJSON(filepath.Join(collectionDir(*modelPath), "calibration.json")),
	})
	promotedInspect, err := inspectCandidateArtifact(*outDir, selectedTasks)
	if err != nil {
		return err
	}
	printJSON(map[string]any{"promoted": true, "out": *outDir, "quality_gate": gate, "runtime_inspect": promotedInspect})
	return nil
}

func cmdRunPipeline(args []string) error {
	fs := flag.NewFlagSet("run-pipeline", flag.ExitOnError)
	recordsPath := fs.String("records", "", "training/eval records JSONL")
	evalRecordsPath := fs.String("eval-records", "", "optional held-out eval records JSONL; when empty, -holdout can split -records")
	holdout := fs.Float64("holdout", 0.0, "optional holdout ratio from -records for calibration/eval; 0 evaluates on all records")
	deduplicate := fs.Bool("deduplicate", true, "drop duplicate task/input records before splitting and training")
	outDir := fs.String("out-dir", filepath.Join("data", "needle", "pipeline"), "pipeline output directory")
	tasksCSV := fs.String("tasks", "", "optional comma-separated task names to include")
	dim := fs.Int("dim", 64, "hashed embedding dimensions")
	minConf := fs.Float64("min-conf", 0.0, "prediction min-conf before calibration; 0 uses runtime default")
	thresholdsCSV := fs.String("thresholds", "", "optional comma-separated calibration thresholds")
	minAccuracy := fs.Float64("min-accuracy", 0.92, "minimum quality-gate and calibration accuracy")
	minTaskAccuracy := fs.Float64("min-task-accuracy", 0.88, "minimum per-task quality-gate accuracy")
	minAcceptedRatio := fs.Float64("min-accepted-ratio", 0.5, "minimum quality-gate and calibration accepted ratio")
	maxRejectedRatio := fs.Float64("max-rejected-ratio", 0.5, "maximum rejected prediction ratio")
	maxMissingRatio := fs.Float64("max-missing-ratio", 0.05, "maximum missing prediction ratio")
	maxLeakage := fs.Int("max-leakage", 0, "maximum normalized train/eval input overlaps; -1 disables")
	minRecords := fs.Int("min-records", 200, "minimum dataset records required")
	maxMismatches := fs.Int("max-mismatches", 20, "maximum mismatches retained before failing; -1 disables")
	mismatches := fs.Int("mismatches", 1000, "maximum mismatches to read into the eval summary")
	promote := fs.Bool("promote", false, "promote candidate to active slot when quality gate passes")
	outActive := fs.String("out", "", "promotion destination; defaults to <data-dir>/needle/models/active")
	dataDir := fs.String("data-dir", defaultDataDir(), "MacLaw data directory used when -out is empty")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*recordsPath) == "" {
		return fmt.Errorf("-records is required")
	}
	if strings.TrimSpace(*outDir) == "" {
		return fmt.Errorf("-out-dir is required")
	}
	records, err := needledata.ReadTrainingRecords(*recordsPath)
	if err != nil {
		return err
	}
	selectedTasks := parseTypes(*tasksCSV)
	records = filterRecordsByTasks(records, selectedTasks)
	if len(records) == 0 {
		return fmt.Errorf("no records selected")
	}
	inputRecords := len(records)
	if *deduplicate {
		records = needledata.DeduplicateTrainingRecords(records)
		if len(records) == 0 {
			return fmt.Errorf("no records selected after deduplication")
		}
	}
	trainRecords := records
	evalRecords := records
	if strings.TrimSpace(*evalRecordsPath) != "" {
		evalRecords, err = needledata.ReadTrainingRecords(*evalRecordsPath)
		if err != nil {
			return err
		}
		evalRecords = filterRecordsByTasks(evalRecords, selectedTasks)
		if len(evalRecords) == 0 {
			return fmt.Errorf("no eval records selected")
		}
	} else if *holdout > 0 {
		trainRecords, evalRecords = needledata.SplitHoldout(records, *holdout)
		if len(trainRecords) == 0 || len(evalRecords) == 0 {
			return fmt.Errorf("holdout split produced train=%d eval=%d records", len(trainRecords), len(evalRecords))
		}
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return err
	}
	trainPath := filepath.Join(*outDir, "train.jsonl")
	evalPath := filepath.Join(*outDir, "eval.jsonl")
	if err := needledata.WriteJSONL(trainPath, trainRecords); err != nil {
		return err
	}
	if err := needledata.WriteJSONL(evalPath, evalRecords); err != nil {
		return err
	}
	candidate := filepath.Join(*outDir, "candidate")
	if err := compileGoArtifactsByTask(trainRecords, candidate, *dim, nil); err != nil {
		return err
	}
	predictionsPath := filepath.Join(*outDir, "predictions.jsonl")
	if _, _, err := writeRuntimePredictionsForModel(predictionsPath, candidate, *minConf, evalRecords); err != nil {
		return err
	}
	predictions, err := needledata.ReadPredictionRecords(predictionsPath)
	if err != nil {
		return err
	}
	calibrations := needledata.CalibrateThresholdsByTask(evalRecords, predictions, parseFloatCSV(*thresholdsCSV), *minAccuracy, *minAcceptedRatio)
	updated, err := updateCollectionMinConf(candidate, calibrations)
	if err != nil {
		return err
	}
	calibrationReport := map[string]any{"by_task": calibrations, "updated_collection": updated, "min_accuracy": *minAccuracy, "min_accepted_ratio": *minAcceptedRatio}
	calibrationPath := filepath.Join(candidate, "calibration.json")
	if err := writeJSON(calibrationPath, calibrationReport); err != nil {
		return err
	}
	if _, _, err := writeRuntimePredictionsForModel(predictionsPath, candidate, *minConf, evalRecords); err != nil {
		return err
	}
	predictions, err = needledata.ReadPredictionRecords(predictionsPath)
	if err != nil {
		return err
	}
	eval := needledata.EvaluateTrainingPredictionRecords(evalRecords, predictions, *mismatches)
	report := needledata.BuildTrainingReport(trainRecords)
	leakage := needledata.BuildLeakageReport(trainRecords, evalRecords, 20)
	gate := needledata.ApplyQualityGateWithLeakage(eval, report, leakage, needledata.QualityGateConfig{MinRecords: *minRecords, MinAccuracy: *minAccuracy, MinTaskAccuracy: *minTaskAccuracy, MinAcceptedRatio: *minAcceptedRatio, MaxRejectedRatio: *maxRejectedRatio, MaxMissingRatio: *maxMissingRatio, MaxLeakage: *maxLeakage, MaxMismatches: *maxMismatches})
	result := map[string]any{"candidate": candidate, "input_records": inputRecords, "records": len(records), "deduplicated": *deduplicate, "duplicates_removed": inputRecords - len(records), "train_records": len(trainRecords), "eval_records": len(evalRecords), "train_path": trainPath, "eval_path": evalPath, "predictions": predictionsPath, "calibration_report": calibrationPath, "updated_collection": updated, "quality_gate": gate, "leakage": leakage, "runtime_inspect": needleruntime.Inspect(needleruntime.Options{Enabled: true, ModelPath: candidate, MinConf: *minConf})}
	if *promote {
		if !gate.Passed {
			printJSON(result)
			return fmt.Errorf("quality gate failed: %s", strings.Join(gate.Reasons, ","))
		}
		if strings.TrimSpace(*outActive) == "" {
			*outActive = needledata.DefaultModelDir(*dataDir)
		}
		if err := copyArtifactDir(candidate, *outActive); err != nil {
			return err
		}
		writeJSON(filepath.Join(*outActive, "promotion.json"), map[string]any{"promoted_at": time.Now().UTC().Format(time.RFC3339), "source": candidate, "quality_gate": gate, "runtime_inspect": result["runtime_inspect"], "calibration": calibrationReport})
		result["promoted"] = true
		result["out"] = *outActive
	} else {
		result["promoted"] = false
	}
	printJSON(result)
	return nil
}

func cmdInspectRuntime(args []string) error {
	fs := flag.NewFlagSet("inspect-runtime", flag.ExitOnError)
	modelPath := fs.String("model", "", "local Needle artifact directory or manifest.json")
	task := fs.String("task", "", "optional task to inspect inside a collection artifact")
	enabled := fs.Bool("enabled", true, "inspect as if the runtime switch is enabled")
	minConf := fs.Float64("min-conf", 0.78, "minimum confidence threshold")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*task) != "" {
		resolved, _, err := resolveModelPathForTaskDetailed(*modelPath, *task)
		if err != nil {
			return err
		}
		printJSON(needleruntime.Inspect(needleruntime.Options{Enabled: *enabled, ModelPath: resolved, MinConf: *minConf}))
		return nil
	}
	if collection := inspectCollectionArtifact(*modelPath, *enabled, *minConf); collection != nil {
		printJSON(collection)
		return nil
	}
	printJSON(needleruntime.Inspect(needleruntime.Options{Enabled: *enabled, ModelPath: *modelPath, MinConf: *minConf}))
	return nil
}

func inspectCollectionArtifact(modelPath string, enabled bool, minConf float64) map[string]any {
	inspect := needleruntime.Inspect(needleruntime.Options{Enabled: enabled, ModelPath: modelPath, MinConf: minConf})
	if inspect.Collection == nil {
		return nil
	}
	items := make(map[string]any, len(inspect.Collection.Tasks))
	for _, task := range sortedKeys(collectionInfoTaskSet(inspect.Collection.Tasks)) {
		entry := inspect.Collection.Tasks[task]
		items[task] = map[string]any{
			"path":            entry.Path,
			"resolved_path":   entry.ResolvedPath,
			"records":         entry.Records,
			"labels":          entry.Labels,
			"min_conf":        entry.MinConf,
			"runtime_inspect": entry.RuntimeInspect,
		}
	}
	return map[string]any{"collection": true, "format": inspect.Collection.Format, "version": inspect.Collection.Version, "dim": inspect.Collection.Dim, "model_path": inspect.Collection.ModelPath, "usable": inspect.Usable, "tasks": items, "runtime_inspect": inspect}
}

func collectionInfoTaskSet(tasks map[string]needleruntime.CollectionTaskInfo) map[string]bool {
	out := make(map[string]bool, len(tasks))
	for task := range tasks {
		out[task] = true
	}
	return out
}
func collectionManifestTaskSet(tasks map[string]needleruntime.CollectionTaskEntry) map[string]bool {
	out := make(map[string]bool, len(tasks))
	for task := range tasks {
		out[task] = true
	}
	return out
}

func inspectCandidateArtifact(modelPath string, tasks map[string]bool) (needleruntime.InspectResult, error) {
	inspect := needleruntime.Inspect(needleruntime.Options{Enabled: true, ModelPath: modelPath})
	if inspect.Usable {
		return inspect, nil
	}
	if len(tasks) == 1 {
		for task := range tasks {
			if taskPath := collectionTaskPath(modelPath, task); taskPath != "" {
				inspect = needleruntime.Inspect(needleruntime.Options{Enabled: true, ModelPath: taskPath})
				if inspect.Usable {
					return inspect, nil
				}
			}
		}
	}
	if inspect.Error != "" {
		return inspect, fmt.Errorf("candidate artifact is not usable: %s", inspect.Error)
	}
	return inspect, fmt.Errorf("candidate artifact is not usable: %v", inspect.Warnings)
}

func updateCollectionMinConf(path string, calibrations map[string]needledata.TaskThresholdCalibration) (map[string]float64, error) {
	collection, ok, err := needleruntime.LoadCollection(path)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%s is not a Needle collection", path)
	}
	updated := map[string]float64{}
	for task, calibration := range calibrations {
		if !calibration.RecommendedFound {
			continue
		}
		entry, ok := collection.Tasks[task]
		if !ok {
			continue
		}
		entry.MinConf = calibration.Recommended.Threshold
		collection.Tasks[task] = entry
		updated[task] = entry.MinConf
	}
	if len(updated) == 0 {
		return updated, nil
	}
	return updated, writeJSON(collectionJSONPath(path), collection)
}

func collectionJSONPath(path string) string {
	path = strings.TrimSpace(path)
	if strings.EqualFold(filepath.Base(path), "collection.json") {
		return path
	}
	return filepath.Join(path, "collection.json")
}

func collectionDir(path string) string {
	path = strings.TrimSpace(path)
	if strings.EqualFold(filepath.Base(path), "collection.json") {
		return filepath.Dir(path)
	}
	return path
}

func collectionTaskPath(root, task string) string {
	path, ok, err := resolveModelPathForTaskDetailed(root, task)
	if err != nil || !ok {
		return ""
	}
	return path
}

func cmdEncode(args []string) error {
	fs := flag.NewFlagSet("encode", flag.ExitOnError)
	modelPath := fs.String("model", "", "local Needle artifact directory or manifest.json")
	task := fs.String("task", needledata.EventWorkflowReview, "Needle task name")
	text := fs.String("text", "", "input text to encode")
	choicesCSV := fs.String("choices", "confirm,supplement,skip,cancel,switch_task,other", "comma-separated choices")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*modelPath) == "" || strings.TrimSpace(*text) == "" {
		return fmt.Errorf("-model and -text are required")
	}
	rt, err := needleruntime.New(needleruntime.Options{Enabled: true, ModelPath: *modelPath})
	if err != nil {
		return err
	}
	encoded, err := rt.Encode(needleruntime.Request{Task: *task, Text: *text, Choices: parseCSV(*choicesCSV)})
	if err != nil {
		return err
	}
	printJSON(encoded)
	return nil
}

func cmdPredict(args []string) error {
	fs := flag.NewFlagSet("predict", flag.ExitOnError)
	modelPath := fs.String("model", "", "local Needle artifact directory or manifest.json")
	task := fs.String("task", needledata.EventWorkflowReview, "Needle task name")
	text := fs.String("text", "", "input text to predict")
	choicesCSV := fs.String("choices", "", "optional comma-separated choices; defaults by task")
	minConf := fs.Float64("min-conf", 0.78, "minimum confidence threshold")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*modelPath) == "" || strings.TrimSpace(*text) == "" {
		return fmt.Errorf("-model and -text are required")
	}
	choices := parseCSV(*choicesCSV)
	if len(choices) == 0 {
		choices = choicesForTask(*task)
	}
	rt, err := needleruntime.New(needleruntime.Options{Enabled: true, ModelPath: *modelPath, MinConf: *minConf})
	if err != nil {
		return err
	}
	decision, accepted, reason, err := rt.PredictDetailed(context.Background(), needleruntime.Request{Task: *task, Text: *text, Choices: choices})
	if err != nil {
		return err
	}
	printJSON(map[string]any{"decision": decision, "accepted": accepted, "reject_reason": reason})
	return nil
}

func cmdPredictRecords(args []string) error {
	fs := flag.NewFlagSet("predict-records", flag.ExitOnError)
	modelPath := fs.String("model", "", "local Needle artifact directory or manifest.json")
	in := fs.String("in", "", "training/eval records JSONL")
	out := fs.String("out", filepath.Join("data", "needle", "predictions.jsonl"), "prediction JSONL output")
	minConf := fs.Float64("min-conf", 0.0, "minimum confidence threshold; 0 uses runtime default")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*modelPath) == "" || strings.TrimSpace(*in) == "" {
		return fmt.Errorf("-model and -in are required")
	}
	records, err := needledata.ReadTrainingRecords(*in)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		return err
	}
	if _, _, err := writeRuntimePredictionsForModel(*out, *modelPath, *minConf, records); err != nil {
		return err
	}
	fmt.Printf("wrote %d predictions to %s\n", len(records), *out)
	return nil
}

func cmdBenchRuntime(args []string) error {
	fs := flag.NewFlagSet("bench-runtime", flag.ExitOnError)
	modelPath := fs.String("model", "", "local Needle artifact directory or manifest.json")
	task := fs.String("task", needledata.EventWorkflowReview, "Needle task name")
	text := fs.String("text", "looks good, continue", "input text to predict repeatedly")
	choicesCSV := fs.String("choices", "", "optional comma-separated choices; defaults by task")
	iterations := fs.Int("n", 1000, "number of measured predictions")
	warmup := fs.Int("warmup", 20, "number of warmup predictions")
	minConf := fs.Float64("min-conf", 0.78, "minimum confidence threshold")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *iterations <= 0 {
		return fmt.Errorf("-n must be greater than 0")
	}
	rt, err := needleruntime.New(needleruntime.Options{Enabled: true, ModelPath: *modelPath, MinConf: *minConf})
	if err != nil {
		return err
	}
	choices := parseCSV(*choicesCSV)
	if len(choices) == 0 {
		choices = choicesForTask(*task)
	}
	req := needleruntime.Request{Task: *task, Text: *text, Choices: choices}
	ctx := context.Background()
	for i := 0; i < *warmup; i++ {
		_, _, err := rt.Predict(ctx, req)
		if err != nil {
			return err
		}
	}
	durations := make([]time.Duration, 0, *iterations)
	accepted := 0
	sources := map[string]int{}
	var last needleruntimeDecisionView
	startAll := time.Now()
	for i := 0; i < *iterations; i++ {
		start := time.Now()
		decision, ok, reason, err := rt.PredictDetailed(ctx, req)
		elapsed := time.Since(start)
		if err != nil {
			return err
		}
		durations = append(durations, elapsed)
		if ok {
			accepted++
		}
		sources[decision.Source]++
		last = needleruntimeDecisionView{Name: decision.Name, Confidence: decision.Confidence, Source: decision.Source, Accepted: ok, RejectReason: reason}
	}
	total := time.Since(startAll)
	printJSON(map[string]any{
		"iterations":      *iterations,
		"warmup":          *warmup,
		"ns_per_op":       float64(total.Nanoseconds()) / float64(*iterations),
		"predictions_sec": float64(*iterations) / total.Seconds(),
		"total_ms":        float64(total.Microseconds()) / 1000.0,
		"avg_us":          avgDurationUS(durations),
		"p50_us":          percentileDurationUS(durations, 0.50),
		"p95_us":          percentileDurationUS(durations, 0.95),
		"p99_us":          percentileDurationUS(durations, 0.99),
		"accepted":        accepted,
		"accepted_ratio":  float64(accepted) / float64(*iterations),
		"sources":         sources,
		"last_prediction": last,
		"runtime_inspect": needleruntime.Inspect(needleruntime.Options{Enabled: true, ModelPath: *modelPath, MinConf: *minConf}),
	})
	return nil
}

func cmdSmokePipeline(args []string) error {
	fs := flag.NewFlagSet("smoke-pipeline", flag.ExitOnError)
	outDir := fs.String("out-dir", filepath.Join("data", "needle", "smoke"), "directory for generated smoke artifacts")
	modelPath := fs.String("model", "", "optional local Needle artifact directory or manifest.json")
	perIntent := fs.Int("per-intent", 2, "synthetic examples per intent label")
	perReview := fs.Int("per-review", 3, "synthetic examples per workflow review label")
	seed := fs.Int64("seed", 7, "deterministic random seed")
	predictTask := fs.String("predict-task", needledata.EventWorkflowReview, "task to run through the local runtime; empty means all records")
	minConf := fs.Float64("min-conf", 0.0, "minimum confidence threshold; 0 uses runtime default")
	mismatches := fs.Int("mismatches", 20, "maximum mismatches to include in the eval summary")
	calibrate := fs.Bool("calibrate", true, "include threshold calibration summary")
	minCalAccuracy := fs.Float64("min-cal-accuracy", 0.92, "minimum accuracy for calibrated threshold recommendations")
	minCalAcceptedRatio := fs.Float64("min-cal-accepted-ratio", 0.5, "minimum accepted ratio for calibrated threshold recommendations")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*outDir) == "" {
		return fmt.Errorf("-out-dir is required")
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return err
	}

	records := needledata.GenerateSeedRecords(needledata.GenerateOptions{PerIntent: *perIntent, PerWorkflowReview: *perReview, Seed: *seed})
	if strings.TrimSpace(*modelPath) == "" {
		compiledPath := filepath.Join(*outDir, "compiled-go-artifact")
		if strings.TrimSpace(*predictTask) == "" {
			if err := compileGoArtifactsByTask(records, compiledPath, 128, nil); err != nil {
				return err
			}
		} else {
			artifact, err := buildHashedLinearArtifact(filterRecordsByTask(records, *predictTask), 128)
			if err != nil {
				return err
			}
			if err := writeGoArtifact(compiledPath, artifact); err != nil {
				return err
			}
		}
		*modelPath = compiledPath
	}
	seedPath := filepath.Join(*outDir, "seed.jsonl")
	needlePath := filepath.Join(*outDir, "needle_train.jsonl")
	predPath := filepath.Join(*outDir, "predictions.jsonl")
	if err := needledata.WriteJSONL(seedPath, records); err != nil {
		return err
	}
	if err := needledata.WriteNeedleJSONL(needlePath, records); err != nil {
		return err
	}

	evalRecords := filterRecordsByTask(records, *predictTask)
	if len(evalRecords) == 0 {
		return fmt.Errorf("no records available for predict-task %q", *predictTask)
	}
	rt, err := needleruntime.New(needleruntime.Options{Enabled: true, ModelPath: *modelPath, MinConf: *minConf})
	if err != nil {
		return err
	}
	_, accepted, err := writeRuntimePredictions(predPath, rt, evalRecords)
	if err != nil {
		return err
	}
	predictionRecords, err := needledata.ReadPredictionRecords(predPath)
	if err != nil {
		return err
	}
	eval := needledata.EvaluateTrainingPredictionRecords(evalRecords, predictionRecords, *mismatches)
	result := map[string]any{
		"seed_records":       len(records),
		"evaluated_records":  len(evalRecords),
		"accepted":           accepted,
		"accepted_ratio":     float64(accepted) / float64(len(evalRecords)),
		"seed_path":          seedPath,
		"needle_path":        needlePath,
		"predictions_path":   predPath,
		"runtime_inspect":    needleruntime.Inspect(needleruntime.Options{Enabled: true, ModelPath: *modelPath, MinConf: *minConf}),
		"prediction_summary": eval,
	}
	if *calibrate {
		result["threshold_calibration"] = needledata.CalibrateThresholdsByTask(evalRecords, predictionRecords, nil, *minCalAccuracy, *minCalAcceptedRatio)
	}
	printJSON(result)
	return nil
}

func filterRecordsByTask(records []needledata.TrainingRecord, task string) []needledata.TrainingRecord {
	task = strings.TrimSpace(task)
	if task == "" {
		return records
	}
	filtered := make([]needledata.TrainingRecord, 0, len(records))
	for _, rec := range records {
		if rec.Task == task {
			filtered = append(filtered, rec)
		}
	}
	return filtered
}

func writeRuntimePredictions(path string, rt *needleruntime.Runtime, records []needledata.TrainingRecord) (map[string]needledata.Decision, int, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, 0, err
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	predictions := make(map[string]needledata.Decision, len(records))
	accepted := 0
	for _, rec := range records {
		decision, ok, reason, err := rt.PredictDetailed(context.Background(), needleruntime.Request{Task: rec.Task, Text: firstUserMessage(rec.Messages), Choices: choicesForTask(rec.Task)})
		if err != nil {
			return nil, 0, fmt.Errorf("predict %s: %w", rec.ID, err)
		}
		if ok {
			accepted++
		}
		predictions[rec.ID] = decision
		if err := enc.Encode(map[string]any{"id": rec.ID, "decision": decision, "accepted": ok, "reject_reason": reason}); err != nil {
			return nil, 0, err
		}
	}
	if err := w.Flush(); err != nil {
		return nil, 0, err
	}
	return predictions, accepted, nil
}

func writeRuntimePredictionsForModel(path, modelPath string, minConf float64, records []needledata.TrainingRecord) (map[string]needledata.Decision, int, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, 0, err
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	predictions := make(map[string]needledata.Decision, len(records))
	accepted := 0
	rt, err := needleruntime.New(needleruntime.Options{Enabled: true, ModelPath: modelPath, MinConf: minConf})
	if err != nil {
		return nil, 0, fmt.Errorf("load model: %w", err)
	}
	for _, rec := range records {
		decision, ok, reason, err := rt.PredictDetailed(context.Background(), needleruntime.Request{Task: rec.Task, Text: firstUserMessage(rec.Messages), Choices: choicesForTask(rec.Task)})
		if err != nil {
			return nil, 0, fmt.Errorf("predict %s: %w", rec.ID, err)
		}
		if ok {
			accepted++
		}
		predictions[rec.ID] = decision
		if err := enc.Encode(map[string]any{"id": rec.ID, "decision": decision, "accepted": ok, "reject_reason": reason}); err != nil {
			return nil, 0, err
		}
	}
	if err := w.Flush(); err != nil {
		return nil, 0, err
	}
	return predictions, accepted, nil
}

func resolveModelPathForTask(modelPath, task string) string {
	path, ok, err := resolveModelPathForTaskDetailed(modelPath, task)
	if err == nil && ok {
		return path
	}
	return modelPath
}

func resolveModelPathForTaskDetailed(modelPath, task string) (string, bool, error) {
	modelPath = strings.TrimSpace(modelPath)
	task = strings.TrimSpace(task)
	if task == "" {
		return modelPath, false, nil
	}
	path, ok, err := needleruntime.ResolveCollectionTaskPath(modelPath, task)
	if err != nil {
		return "", false, err
	}
	if !ok {
		if collection, collectionOK, collectionErr := needleruntime.LoadCollection(modelPath); collectionErr != nil {
			return "", false, collectionErr
		} else if collectionOK {
			return "", false, fmt.Errorf("Needle collection has no model for task %q; available tasks: %s", task, strings.Join(sortedKeys(collectionManifestTaskSet(collection.Tasks)), ","))
		}
		return modelPath, false, nil
	}
	if _, err := os.Stat(filepath.Join(path, "manifest.json")); err != nil {
		return "", true, fmt.Errorf("Needle collection task %s missing manifest at %s: %w", task, path, err)
	}
	return path, true, nil
}

type goArtifactData struct {
	Tasks  []string
	Labels []string
	Dim    int
	Vocab  map[string]int
	Head   []int8
	Bias   []float32
}

func buildHashedLinearArtifact(records []needledata.TrainingRecord, dim int) (*goArtifactData, error) {
	if dim <= 0 {
		return nil, fmt.Errorf("hashed linear dimension must be positive, got %d", dim)
	}
	labelIndex := map[string]int{}
	taskSet := map[string]bool{}
	var labels []string
	for _, rec := range records {
		name := strings.TrimSpace(rec.Expected.Name)
		if name == "" {
			continue
		}
		if _, ok := labelIndex[name]; !ok {
			labelIndex[name] = len(labels)
			labels = append(labels, name)
		}
		if strings.TrimSpace(rec.Task) != "" {
			taskSet[rec.Task] = true
		}
	}
	if len(labels) == 0 {
		return nil, fmt.Errorf("training records do not contain expected labels")
	}
	tasks := sortedKeys(taskSet)
	if len(tasks) != 1 {
		return nil, fmt.Errorf("hashed linear artifact requires exactly one task, got %d (%s); pass -tasks for one MacLaw micro-decision", len(tasks), strings.Join(tasks, ","))
	}
	counts := make([]float64, len(labels))
	head := make([][]float64, len(labels))
	for i := range head {
		head[i] = make([]float64, dim)
	}
	for _, rec := range records {
		idx, ok := labelIndex[strings.TrimSpace(rec.Expected.Name)]
		if !ok {
			continue
		}
		features := hashedFeatures(recordText(rec), dim)
		counts[idx]++
		for j, v := range features {
			head[idx][j] += v
		}
	}
	for i := range head {
		if counts[i] == 0 {
			continue
		}
		for j := range head[i] {
			head[i][j] /= counts[i]
		}
	}
	head = oneVsRestRows(head, counts)
	return &goArtifactData{
		Tasks:  tasks,
		Labels: labels,
		Dim:    dim,
		Vocab:  hashedVocab(dim),
		Head:   quantizeRows(head),
		Bias:   logPriorBias(counts),
	}, nil
}

func oneVsRestRows(centroids [][]float64, counts []float64) [][]float64 {
	if len(centroids) == 0 {
		return centroids
	}
	dim := len(centroids[0])
	out := make([][]float64, len(centroids))
	for i := range centroids {
		out[i] = make([]float64, dim)
		otherCount := 0.0
		other := make([]float64, dim)
		for k := range centroids {
			if k == i || counts[k] == 0 {
				continue
			}
			otherCount += counts[k]
			for j := 0; j < dim; j++ {
				other[j] += centroids[k][j] * counts[k]
			}
		}
		if otherCount > 0 {
			for j := range other {
				other[j] /= otherCount
			}
		}
		for j := 0; j < dim; j++ {
			out[i][j] = centroids[i][j] - other[j]
		}
	}
	return out
}

func writeGoArtifact(outDir string, artifact *goArtifactData) error {
	if artifact == nil {
		return fmt.Errorf("nil artifact")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	tok := map[string]any{"model": map[string]any{"vocab": artifact.Vocab}}
	if err := writeJSON(filepath.Join(outDir, "tokenizer.json"), tok); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(outDir, "labels.json"), artifact.Labels); err != nil {
		return err
	}
	weightPath := filepath.Join(outDir, "needle.q8")
	if err := writeQ8Weight(weightPath, artifact); err != nil {
		return err
	}
	sha, err := fileSHA256(weightPath)
	if err != nil {
		return err
	}
	manifest := map[string]any{
		"format":        "maclaw-needle",
		"version":       needleruntime.ArtifactVersion,
		"runtime":       "go",
		"tasks":         artifact.Tasks,
		"weight_path":   "needle.q8",
		"weight_sha256": sha,
		"tokenizer":     "tokenizer.json",
		"labels":        "labels.json",
		"quant":         "q8-sparse-hash-linear",
		"weight_header": map[string]any{
			"vocab_size":  artifact.Dim,
			"hidden_size": artifact.Dim,
			"num_labels":  len(artifact.Labels),
			"flags":       needleruntime.WeightFlagSparseHashHead,
			"data_offset": 32,
		},
		"notes": "Compiled by maclaw-needle compile-go-artifact from MacLaw training records.",
	}
	return writeJSON(filepath.Join(outDir, "manifest.json"), manifest)
}

func writeQ8Weight(path string, artifact *goArtifactData) error {
	buf := make([]byte, 0, 32+len(artifact.Head)+len(artifact.Bias)*4)
	header := make([]byte, 32)
	copy(header[0:8], []byte(needleruntime.WeightMagic))
	binary.LittleEndian.PutUint32(header[8:12], needleruntime.WeightVersion)
	binary.LittleEndian.PutUint32(header[12:16], uint32(artifact.Dim))
	binary.LittleEndian.PutUint32(header[16:20], uint32(artifact.Dim))
	binary.LittleEndian.PutUint32(header[20:24], uint32(len(artifact.Labels)))
	binary.LittleEndian.PutUint32(header[24:28], needleruntime.WeightFlagSparseHashHead)
	binary.LittleEndian.PutUint32(header[28:32], 32)
	buf = append(buf, header...)
	buf = append(buf, int8Bytes(artifact.Head)...)
	for _, v := range artifact.Bias {
		var raw [4]byte
		binary.LittleEndian.PutUint32(raw[:], math.Float32bits(v))
		buf = append(buf, raw[:]...)
	}
	return os.WriteFile(path, buf, 0o644)
}

func hashedFeatures(text string, dim int) []float64 {
	features := make([]float64, dim)
	tokens := splitTextTokens(text)
	if len(tokens) == 0 {
		return features
	}
	for _, token := range tokens {
		id := hashTokenID(token, dim)
		features[id]++
	}
	inv := 1.0 / float64(len(tokens))
	for i := range features {
		features[i] *= inv
	}
	return features
}

func splitTextTokens(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
	})
}

func hashTokenID(token string, dim int) int {
	var h uint64 = 14695981039346656037
	for i := 0; i < len(token); i++ {
		h ^= uint64(token[i])
		h *= 1099511628211
	}
	return int(h % uint64(dim))
}

func hashedVocab(dim int) map[string]int {
	vocab := make(map[string]int, dim)
	for i := 0; i < dim; i++ {
		vocab[fmt.Sprintf("__h%d", i)] = i
	}
	return vocab
}

func quantizeRows(rows [][]float64) []int8 {
	if len(rows) == 0 {
		return nil
	}
	out := make([]int8, 0, len(rows)*len(rows[0]))
	for _, row := range rows {
		maxAbs := 0.0
		for _, v := range row {
			if math.Abs(v) > maxAbs {
				maxAbs = math.Abs(v)
			}
		}
		if maxAbs == 0 {
			maxAbs = 1
		}
		scale := 64.0 / maxAbs
		for _, v := range row {
			q := math.Round(v * scale)
			if q > 127 {
				q = 127
			}
			if q < -128 {
				q = -128
			}
			out = append(out, int8(q))
		}
	}
	return out
}

func logPriorBias(counts []float64) []float32 {
	bias := make([]float32, len(counts))
	total := 0.0
	for _, c := range counts {
		total += c + 1
	}
	for i, c := range counts {
		bias[i] = float32(math.Log((c + 1) / total))
	}
	return bias
}

func int8Bytes(in []int8) []byte {
	out := make([]byte, len(in))
	for i, v := range in {
		out[i] = byte(v)
	}
	return out
}

func recordText(rec needledata.TrainingRecord) string {
	return renderRuntimePrompt(rec.Task, firstUserMessage(rec.Messages))
}

func renderRuntimePrompt(task, text string) string {
	choices := strings.Join(choicesForTask(task), ", ")
	if choices == "" {
		choices = "none"
	}
	return strings.TrimSpace("Task: " + task + "\nChoices: " + choices + "\nUser: " + strings.TrimSpace(text))
}

func filterRecordsByTasks(records []needledata.TrainingRecord, tasks map[string]bool) []needledata.TrainingRecord {
	if len(tasks) == 0 {
		return records
	}
	filtered := make([]needledata.TrainingRecord, 0, len(records))
	for _, rec := range records {
		if tasks[rec.Task] {
			filtered = append(filtered, rec)
		}
	}
	return filtered
}

func groupsToSet(groups map[string][]needledata.TrainingRecord) map[string]bool {
	out := make(map[string]bool, len(groups))
	for k := range groups {
		out[k] = true
	}
	return out
}

func safePathName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "task"
	}
	var b strings.Builder
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "task"
	}
	return b.String()
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func readOptionalJSON(path string) any {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil
	}
	return v
}

func copyArtifactDir(src, dst string) error {
	if _, err := os.Stat(filepath.Join(strings.TrimSpace(src), "collection.json")); err == nil {
		return copyDirTree(strings.TrimSpace(src), dst)
	}
	srcManifest := manifestPathForCopy(src)
	srcDir := filepath.Dir(srcManifest)
	info, err := os.Stat(srcManifest)
	if err != nil {
		return fmt.Errorf("read source manifest: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("source manifest is a directory: %s", srcManifest)
	}
	return copyDirTree(srcDir, dst)
}

func copyDirTree(srcDir, dst string) error {
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

func manifestPathForCopy(path string) string {
	path = strings.TrimSpace(path)
	if strings.EqualFold(filepath.Base(path), "manifest.json") {
		return path
	}
	return filepath.Join(path, "manifest.json")
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func defaultDataDir() string {
	if dir := strings.TrimSpace(os.Getenv("MACLAW_DATA_DIR")); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Join(".", ".maclaw")
	}
	return filepath.Join(home, ".maclaw")
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

type needleruntimeDecisionView struct {
	Name         string  `json:"name"`
	Confidence   float64 `json:"confidence"`
	Source       string  `json:"source"`
	Accepted     bool    `json:"accepted"`
	RejectReason string  `json:"reject_reason,omitempty"`
}

func parseTypes(csv string) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.Split(csv, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out[part] = true
		}
	}
	return out
}

func firstUserMessage(messages []needledata.ChatMessage) string {
	for _, msg := range messages {
		if msg.Role == "user" {
			return msg.Content
		}
	}
	return ""
}

func choicesForTask(task string) []string {
	switch task {
	case needledata.EventWorkflowReview:
		return []string{"confirm", "supplement", "skip", "cancel", "switch_task", "other"}
	case needledata.EventIntentGate:
		return []string{"route_ssh", "route_browser", "route_web_search", "route_office", "route_coding", "route_workflow", "no_call"}
	case needledata.EventMemoryExtractGate:
		return []string{"extract_memory", "no_extract"}
	case needledata.EventSmartApproval:
		return []string{"safe", "unsafe", "unknown"}
	default:
		return nil
	}
}

func avgDurationUS(durations []time.Duration) float64 {
	if len(durations) == 0 {
		return 0
	}
	var total time.Duration
	for _, d := range durations {
		total += d
	}
	return float64(total.Microseconds()) / float64(len(durations))
}

func percentileDurationUS(durations []time.Duration, p float64) float64 {
	if len(durations) == 0 {
		return 0
	}
	copyDurations := append([]time.Duration(nil), durations...)
	sort.Slice(copyDurations, func(i, j int) bool { return copyDurations[i] < copyDurations[j] })
	idx := int(float64(len(copyDurations)-1) * p)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(copyDurations) {
		idx = len(copyDurations) - 1
	}
	return float64(copyDurations[idx].Microseconds())
}

func parseCSV(csv string) []string {
	var out []string
	for _, part := range strings.Split(csv, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func parseFloatCSV(csv string) []float64 {
	var out []float64
	for _, part := range strings.Split(csv, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		var v float64
		if _, err := fmt.Sscanf(part, "%f", &v); err == nil {
			out = append(out, v)
		}
	}
	return out
}

func parseTaskFloatMap(csv string) map[string]float64 {
	out := map[string]float64{}
	for _, part := range strings.Split(csv, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		var v float64
		if _, err := fmt.Sscanf(value, "%f", &v); err == nil && v > 0 {
			out[key] = v
		}
	}
	return out
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: maclaw-needle <command> [options]

commands:
  generate-seed   generate MacLaw synthetic micro-decision training records
  export-dataset  convert local Needle event logs into training/eval JSONL
  export-needle   convert MacLaw training records into cactus Needle JSONL
  compile-go-artifact compile training records into a pure-Go q8 artifact
  inspect-logs    summarize local Needle event logs
  report-dataset  summarize training records or event logs
  eval-shadow     compare shadow Needle predictions against final decisions
  eval-predictions evaluate a prediction JSONL against exported records
  eval-localization evaluate ranked bug-localization predictions (Hit@K/MRR/latency)
  quality-gate    enforce promotion thresholds for a trained artifact
  calibrate-threshold choose/update min-conf by accuracy and accepted coverage
  promote-artifact copy a candidate artifact into the active slot after quality gate
  run-pipeline    compile, predict, calibrate, gate, and optionally promote
  active-path     print the active local Needle artifact path, optionally resolved by task
  inspect-runtime inspect local Needle runtime/artifact readiness
  encode          render and tokenize one local Needle request
  predict         run one local Needle prediction
  predict-records run local Needle predictions for records JSONL
  bench-runtime   benchmark local Needle prediction latency
  smoke-pipeline  generate seed data, export Needle JSONL, predict, and eval`)
}

func printJSON(v any) {
	data, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(data))
}
