package needledata

type QualityGateConfig struct {
	MinRecords       int     `json:"min_records"`
	MinAccuracy      float64 `json:"min_accuracy"`
	MinTaskAccuracy  float64 `json:"min_task_accuracy"`
	MinAcceptedRatio float64 `json:"min_accepted_ratio,omitempty"`
	MaxRejectedRatio float64 `json:"max_rejected_ratio,omitempty"`
	MaxMissingRatio  float64 `json:"max_missing_ratio,omitempty"`
	MaxLeakage       int     `json:"max_leakage,omitempty"`
	MaxMismatches    int     `json:"max_mismatches"`
}

type QualityGateResult struct {
	Passed  bool                 `json:"passed"`
	Reasons []string             `json:"reasons,omitempty"`
	Config  QualityGateConfig    `json:"config"`
	Eval    EvalSummary          `json:"eval"`
	Report  DatasetReport        `json:"report"`
	Leakage DatasetLeakageReport `json:"leakage,omitempty"`
}

func DefaultQualityGateConfig() QualityGateConfig {
	return QualityGateConfig{MinRecords: 200, MinAccuracy: 0.92, MinTaskAccuracy: 0.88, MinAcceptedRatio: 0.5, MaxRejectedRatio: 0.5, MaxMissingRatio: 0.05, MaxMismatches: 20}
}

func ApplyQualityGate(eval EvalSummary, report DatasetReport, cfg QualityGateConfig) QualityGateResult {
	return ApplyQualityGateWithLeakage(eval, report, DatasetLeakageReport{}, cfg)
}

func ApplyQualityGateWithLeakage(eval EvalSummary, report DatasetReport, leakage DatasetLeakageReport, cfg QualityGateConfig) QualityGateResult {
	if cfg.MinRecords == 0 && cfg.MinAccuracy == 0 && cfg.MinTaskAccuracy == 0 && cfg.MinAcceptedRatio == 0 && cfg.MaxRejectedRatio == 0 && cfg.MaxMissingRatio == 0 && cfg.MaxLeakage == 0 && cfg.MaxMismatches == 0 {
		cfg = DefaultQualityGateConfig()
	}
	res := QualityGateResult{Passed: true, Config: cfg, Eval: eval, Report: report, Leakage: leakage}
	if report.Total < cfg.MinRecords {
		res.Passed = false
		res.Reasons = append(res.Reasons, "not_enough_records")
	}
	if report.EmptyInput > 0 {
		res.Passed = false
		res.Reasons = append(res.Reasons, "dataset_has_empty_input")
	}
	if report.MissingLabel > 0 {
		res.Passed = false
		res.Reasons = append(res.Reasons, "dataset_has_missing_label")
	}
	if report.DuplicateInputs > 0 {
		res.Passed = false
		res.Reasons = append(res.Reasons, "dataset_has_duplicate_input")
	}
	if eval.Total == 0 {
		res.Passed = false
		res.Reasons = append(res.Reasons, "no_predictions")
	} else if eval.Accuracy < cfg.MinAccuracy {
		res.Passed = false
		res.Reasons = append(res.Reasons, "accuracy_below_threshold")
	}
	if cfg.MinAcceptedRatio > 0 && eval.AcceptedRatio < cfg.MinAcceptedRatio {
		res.Passed = false
		res.Reasons = append(res.Reasons, "accepted_ratio_below_threshold")
	}
	if cfg.MaxRejectedRatio > 0 && eval.RejectedRatio > cfg.MaxRejectedRatio {
		res.Passed = false
		res.Reasons = append(res.Reasons, "rejected_ratio_above_threshold")
	}
	if cfg.MaxMissingRatio > 0 && eval.MissingRatio > cfg.MaxMissingRatio {
		res.Passed = false
		res.Reasons = append(res.Reasons, "missing_ratio_above_threshold")
	}
	if cfg.MaxLeakage >= 0 && leakage.Overlaps > cfg.MaxLeakage {
		res.Passed = false
		res.Reasons = append(res.Reasons, "train_eval_leakage")
	}
	for _, bucket := range eval.ByTask {
		if bucket.Total > 0 && bucket.Accuracy < cfg.MinTaskAccuracy {
			res.Passed = false
			res.Reasons = append(res.Reasons, "task_accuracy_below_threshold")
			break
		}
	}
	for _, bucket := range eval.ByTask {
		if cfg.MinAcceptedRatio > 0 && bucket.AcceptedRatio < cfg.MinAcceptedRatio {
			res.Passed = false
			res.Reasons = append(res.Reasons, "task_accepted_ratio_below_threshold")
			break
		}
	}
	if cfg.MaxMismatches >= 0 && len(eval.Mismatches) > cfg.MaxMismatches {
		res.Passed = false
		res.Reasons = append(res.Reasons, "too_many_mismatches")
	}
	return res
}
