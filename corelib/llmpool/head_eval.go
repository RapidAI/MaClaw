package llmpool

import (
	"errors"
	"strings"
	"time"
)

const (
	GateMinReviews      = 200
	GateMinAccuracy     = 0.85
	GateMinPlanRecall   = 0.80
	GateMinDesignRecall = 0.80
	GateCount           = 5
)

type EvalWindow struct {
	Reviews      int            `json:"reviews"`
	ByClass      map[string]int `json:"by_class,omitempty"`
	Correct      int            `json:"correct"`
	PlanGold     int            `json:"plan_gold"`
	PlanHit      int            `json:"plan_hit"`
	DesignGold   int            `json:"design_gold"`
	DesignHit    int            `json:"design_hit"`
	RuleAgree    int            `json:"rule_agree"`
	RuleCompared int            `json:"rule_compared"`
}

type GoldEvalRow struct {
	Gold      string
	Predicted string
	RuleClass string
}

func (w EvalWindow) Accuracy() float64 {
	if w.Reviews <= 0 {
		return 0
	}
	return float64(w.Correct) / float64(w.Reviews)
}

func (w EvalWindow) PlanRecall() float64 {
	if w.PlanGold <= 0 {
		return 0
	}
	return float64(w.PlanHit) / float64(w.PlanGold)
}

func (w EvalWindow) DesignRecall() float64 {
	if w.DesignGold <= 0 {
		return 0
	}
	return float64(w.DesignHit) / float64(w.DesignGold)
}

func (w EvalWindow) RuleAgreement() float64 {
	if w.RuleCompared <= 0 {
		return 0
	}
	return float64(w.RuleAgree) / float64(w.RuleCompared)
}

func (w EvalWindow) CoversAllClasses() bool {
	if w.ByClass == nil {
		return false
	}
	for _, class := range FrozenWorkloadClasses {
		if w.ByClass[class] <= 0 {
			return false
		}
	}
	return true
}

func AccumulateEvalWindow(rows []GoldEvalRow) EvalWindow {
	out := EvalWindow{ByClass: map[string]int{}}
	for _, row := range rows {
		gold := NormalizeWorkloadClass(row.Gold)
		if gold == "" {
			continue
		}
		out.Reviews++
		out.ByClass[gold]++
		pred := strings.TrimSpace(row.Predicted)
		if pred == gold {
			out.Correct++
		}
		if gold == WorkloadClassPlan {
			out.PlanGold++
			if pred == gold {
				out.PlanHit++
			}
		}
		if gold == WorkloadClassDesign {
			out.DesignGold++
			if pred == gold {
				out.DesignHit++
			}
		}
		rule := strings.TrimSpace(row.RuleClass)
		if rule != "" {
			out.RuleCompared++
			if rule == pred {
				out.RuleAgree++
			}
		}
	}
	return out
}

type GateReport struct {
	ReviewCoverage bool       `json:"review_coverage"`
	Accuracy       bool       `json:"accuracy"`
	Recall         bool       `json:"recall"`
	TwoWindows     bool       `json:"two_windows"`
	Artifact       bool       `json:"artifact"`
	Passed         int        `json:"passed"`
	Total          int        `json:"total"`
	CanPromote     bool       `json:"can_promote"`
	Ready          bool       `json:"ready"`
	NotReadyReason string     `json:"not_ready_reason,omitempty"`
	Suggestion     string     `json:"suggestion"`
	Recent         EvalWindow `json:"recent"`
	Previous       EvalWindow `json:"previous"`
	Coverage       EvalWindow `json:"coverage"`
}

func windowMeetsQuality(w EvalWindow) bool {
	return w.Accuracy() >= GateMinAccuracy && w.PlanRecall() >= GateMinPlanRecall && w.DesignRecall() >= GateMinDesignRecall
}

func EvaluatePromoteGates(recent, previous EvalWindow, artifactReady bool) GateReport {
	return evaluateGateWindows(recent, previous, recent, artifactReady)
}

// evaluateGateWindows builds the report: coverage counts every qualifying
// human-labeled row, quality gates only the P3/P4 human rows the head would
// actually rewrite.
func evaluateGateWindows(recent, previous, coverage EvalWindow, artifactReady bool) GateReport {
	report := GateReport{
		Recent:   recent,
		Previous: previous,
		Coverage: coverage,
		Total:    GateCount,
		Artifact: artifactReady,
		Ready:    true,
	}
	report.ReviewCoverage = coverage.Reviews >= GateMinReviews && coverage.CoversAllClasses()
	report.Accuracy = recent.Accuracy() >= GateMinAccuracy
	report.Recall = recent.PlanRecall() >= GateMinPlanRecall && recent.DesignRecall() >= GateMinDesignRecall
	report.TwoWindows = windowMeetsQuality(recent) && windowMeetsQuality(previous)
	for _, ok := range []bool{report.ReviewCoverage, report.Accuracy, report.Recall, report.TwoWindows, report.Artifact} {
		if ok {
			report.Passed++
		}
	}
	report.CanPromote = report.Passed == GateCount
	if report.CanPromote {
		report.Suggestion = "gates green; canary or on is allowed"
		return report
	}
	report.Suggestion = "gates not met; stay in shadow or type PROMOTE with a reason"
	return report
}

// GateNotReadyReport is the only legal report when the corpus is unreadable
// or the embedder is down: nothing passes, and PROMOTE must not paint it green.
func GateNotReadyReport(reason string, artifactReady bool) GateReport {
	return GateReport{
		Total:          GateCount,
		Artifact:       artifactReady,
		Ready:          false,
		NotReadyReason: strings.TrimSpace(reason),
		Suggestion:     "gate eval not ready: " + strings.TrimSpace(reason),
	}
}

const (
	GoldSourceHuman = "human"
	GoldSourceAuto  = "auto"
)

// GateEvalSample is one corpus row as gate input. Gates only count human gold.
type GateEvalSample struct {
	ID         string
	Preview    string
	Embedding  []float32
	GoldClass  string
	GoldSource string
	RuleSource string
	LabeledAt  time.Time
}

// EvaluateHeadGate re-scores the corpus with this exact version's weights
// (never the persisted HeadClass). Rows qualify only when gold_source=human,
// labeled_at >= the version's trained_at, and id is outside sample_ids.
// Rows without preview are skipped, never counted as correct.
func EvaluateHeadGate(head *ClassificationHead, run *TrainRun, corpusReady bool, samples []GateEvalSample, embed func(string) ([]float32, error), artifactReady bool, now time.Time) GateReport {
	if !corpusReady {
		return GateNotReadyReport("corpus_unavailable", artifactReady)
	}
	if head == nil || !head.Ready() {
		return GateNotReadyReport("artifact_missing", artifactReady)
	}
	trainedAt := time.Time{}
	if run != nil && strings.TrimSpace(run.TrainedAt) != "" {
		if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(run.TrainedAt)); err == nil {
			trainedAt = parsed
		}
	} else if head != nil && strings.TrimSpace(head.TrainedAt) != "" {
		if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(head.TrainedAt)); err == nil {
			trainedAt = parsed
		}
	}
	recentCutoff := now.Add(-7 * 24 * time.Hour)
	prevCutoff := now.Add(-14 * 24 * time.Hour)
	var recentAll, recentQuality, prevQuality []GoldEvalRow
	for _, sample := range samples {
		if strings.TrimSpace(sample.GoldSource) != GoldSourceHuman {
			continue
		}
		if sample.LabeledAt.Before(trainedAt) {
			continue
		}
		if run.HasSampleID(sample.ID) {
			continue
		}
		gold := NormalizeWorkloadClass(sample.GoldClass)
		if !IsWorkloadClass(gold) {
			continue
		}
		preview := strings.TrimSpace(sample.Preview)
		if preview == "" {
			continue
		}
		vec := sample.Embedding
		if len(vec) < HeadDim {
			if embed == nil {
				return GateNotReadyReport("embedder_unavailable", artifactReady)
			}
			got, err := embed(preview)
			if err != nil || len(got) < HeadDim {
				return GateNotReadyReport("embedder_unavailable", artifactReady)
			}
			vec = got
		}
		pred := head.Predict(vec).Class
		row := GoldEvalRow{Gold: gold, Predicted: pred}
		inRecent := !sample.LabeledAt.Before(recentCutoff)
		inPrevious := sample.LabeledAt.Before(recentCutoff) && !sample.LabeledAt.Before(prevCutoff)
		if inRecent {
			recentAll = append(recentAll, row)
		}
		quality := strings.TrimSpace(sample.RuleSource) == ClassSourceHeuristic || strings.TrimSpace(sample.RuleSource) == ClassSourceFallback
		if !quality {
			continue
		}
		if inRecent {
			recentQuality = append(recentQuality, row)
		} else if inPrevious {
			prevQuality = append(prevQuality, row)
		}
	}
	return evaluateGateWindows(AccumulateEvalWindow(recentQuality), AccumulateEvalWindow(prevQuality), AccumulateEvalWindow(recentAll), artifactReady)
}

func DistributeComplete(ack map[string]string) bool {
	if len(ack) == 0 {
		return true
	}
	for _, status := range ack {
		if !strings.EqualFold(strings.TrimSpace(status), "acked") {
			return false
		}
	}
	return true
}

func pipelineRank(mode string) int {
	switch NormalizePipelineMode(mode) {
	case PipelineOn:
		return 3
	case PipelineCanary:
		return 2
	case PipelineShadow:
		return 1
	default:
		return 0
	}
}

func AllowPipelineChange(current, next string, servingReady, distributeComplete bool, gates GateReport, override, reason string) error {
	current = NormalizePipelineMode(current)
	next = NormalizePipelineMode(next)
	if next == current {
		return nil
	}
	// Demotions (including on->canary) are always allowed and never gated.
	if pipelineRank(next) < pipelineRank(current) {
		return nil
	}
	if next == PipelineOff {
		return nil
	}
	if next == PipelineShadow {
		if !servingReady {
			return ErrServingRequired
		}
		return nil
	}
	if next == PipelineCanary || next == PipelineOn {
		if current == PipelineOff {
			return ErrPipelineHopBlocked
		}
		if !servingReady {
			return ErrServingRequired
		}
		if !distributeComplete {
			return ErrDistributeIncomplete
		}
		if gates.CanPromote {
			return nil
		}
		if strings.EqualFold(strings.TrimSpace(override), PromoteOverride) && strings.TrimSpace(reason) != "" {
			return nil
		}
		return errPromoteBlocked
	}
	return nil
}

var (
	errPromoteBlocked       = promoteBlockedError{}
	ErrPipelineHopBlocked   = errors.New("pipeline must enter shadow before canary or on")
	ErrServingRequired      = errors.New("pipeline needs a ready serving head")
	ErrDistributeIncomplete = errors.New("pipeline needs completed serving distribute")
)

func IsPipelineHopBlocked(err error) bool {
	return errors.Is(err, ErrPipelineHopBlocked)
}

func IsServingRequired(err error) bool {
	return errors.Is(err, ErrServingRequired)
}

func IsDistributeIncomplete(err error) bool {
	return errors.Is(err, ErrDistributeIncomplete)
}

func IsPipelineRuleBlocked(err error) bool {
	return IsPipelineHopBlocked(err) || IsServingRequired(err) || IsDistributeIncomplete(err)
}

type promoteBlockedError struct{}

func (promoteBlockedError) Error() string {
	return "promote blocked: eval page gates are not green"
}

func IsPromoteBlocked(err error) bool {
	_, ok := err.(promoteBlockedError)
	return ok
}
