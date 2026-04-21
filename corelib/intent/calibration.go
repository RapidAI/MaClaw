package intent

import (
	"fmt"
	"log"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// Offline calibration tool for fusion parameters (alpha, delta).
//
// Runs a grid search over (alpha, delta) pairs on a labeled dataset to find
// the combination that maximizes routing accuracy.
//
// Ported from intent-fusion's calibration.py to Go.
// ---------------------------------------------------------------------------

// CalibrationCase is a single labeled test case.
type CalibrationCase struct {
	Message       string
	ExpectedLabel IntentLabel
	Note          string // optional description
}

// GridPoint holds the result of one (alpha, delta) evaluation.
type GridPoint struct {
	Alpha          float64
	Delta          float64
	LowThreshold   float64
	Accuracy       float64
	Correct        int
	Total          int
	ClearCount     int
	AmbiguousCount int
	LowCount       int
}

// CalibrationReport is the output of RunGridSearch.
type CalibrationReport struct {
	Best    GridPoint
	Grid    []GridPoint
	Errors  []CalibrationError // cases where the best config still fails
}

// CalibrationError records a single misclassification.
type CalibrationError struct {
	Message  string
	Expected IntentLabel
	Got      IntentLabel
	Score    float64
	Verdict  FusionVerdict
}

// DefaultAlphaValues returns the default alpha candidates for grid search.
func DefaultAlphaValues() []float64 {
	return []float64{0.0, 0.05, 0.10, 0.15, 0.20, 0.30, 0.50}
}

// DefaultDeltaValues returns the default delta candidates for grid search.
func DefaultDeltaValues() []float64 {
	return []float64{0.05, 0.08, 0.10, 0.12, 0.15, 0.20}
}

// RunGridSearch evaluates all (alpha, delta) combinations on the labeled cases
// and returns the best configuration.
//
// embScorer: function that returns embedding top-k scores for a message.
//
//	Pass nil to skip embedding channel (tree-only calibration).
//
// treeScorer: function that returns tree channel scores for a message.
//
//	Pass nil to skip tree channel (embedding-only calibration).
//
// At least one scorer must be non-nil.
func RunGridSearch(
	cases []CalibrationCase,
	embScorer func(text string) []labelScore,
	treeScorer func(text string) []labelScore,
	alphaValues []float64,
	deltaValues []float64,
) CalibrationReport {
	if alphaValues == nil {
		alphaValues = DefaultAlphaValues()
	}
	if deltaValues == nil {
		deltaValues = DefaultDeltaValues()
	}

	lowThreshold := DefaultLowThreshold

	var results []GridPoint

	for _, alpha := range alphaValues {
		for _, delta := range deltaValues {
			cfg := FusionConfig{
				Alpha:        alpha,
				Delta:        delta,
				LowThreshold: lowThreshold,
			}

			correct := 0
			clearCount := 0
			ambiguousCount := 0
			lowCount := 0

			for _, c := range cases {
				result := evalCase(c, embScorer, treeScorer, alpha, cfg)

				switch result.verdict {
				case VerdictClear:
					clearCount++
				case VerdictAmbiguous:
					ambiguousCount++
				case VerdictLow:
					lowCount++
				}
				if result.correct {
					correct++
				}
			}

			accuracy := 0.0
			if len(cases) > 0 {
				accuracy = float64(correct) / float64(len(cases))
			}

			point := GridPoint{
				Alpha:          alpha,
				Delta:          delta,
				LowThreshold:   lowThreshold,
				Accuracy:       accuracy,
				Correct:        correct,
				Total:          len(cases),
				ClearCount:     clearCount,
				AmbiguousCount: ambiguousCount,
				LowCount:       lowCount,
			}
			results = append(results, point)

			log.Printf("[calibration] alpha=%.2f delta=%.2f accuracy=%.3f (%d/%d) clear=%d ambiguous=%d low=%d",
				alpha, delta, accuracy, correct, len(cases),
				clearCount, ambiguousCount, lowCount)
		}
	}

	// Find best: highest accuracy, then fewest ambiguous (prefer decisive routing).
	sort.Slice(results, func(i, j int) bool {
		if results[i].Accuracy != results[j].Accuracy {
			return results[i].Accuracy > results[j].Accuracy
		}
		return results[i].AmbiguousCount < results[j].AmbiguousCount
	})

	best := results[0]

	// Collect errors for the best config.
	var errors []CalibrationError
	cfg := FusionConfig{Alpha: best.Alpha, Delta: best.Delta, LowThreshold: lowThreshold}
	for _, c := range cases {
		result := evalCase(c, embScorer, treeScorer, best.Alpha, cfg)
		if !result.correct {
			errors = append(errors, CalibrationError{
				Message:  c.Message,
				Expected: c.ExpectedLabel,
				Got:      result.got,
				Score:    result.score,
				Verdict:  result.verdict,
			})
		}
	}

	return CalibrationReport{
		Best:   best,
		Grid:   results,
		Errors: errors,
	}
}

// evalCaseResult holds the outcome of evaluating a single calibration case.
type evalCaseResult struct {
	verdict FusionVerdict
	got     IntentLabel
	score   float64
	correct bool
}

// evalCase scores a single calibration case and determines correctness.
func evalCase(
	c CalibrationCase,
	embScorer func(string) []labelScore,
	treeScorer func(string) []labelScore,
	alpha float64,
	cfg FusionConfig,
) evalCaseResult {
	var embTop, treeTop []labelScore
	if embScorer != nil {
		embTop = embScorer(c.Message)
	}
	if treeScorer != nil {
		treeTop = treeScorer(c.Message)
	}

	effectiveAlpha := alpha
	if embScorer == nil {
		effectiveAlpha = 0.0
	}
	if treeScorer == nil {
		effectiveAlpha = 1.0
	}

	candidates := MergeAndScore(embTop, treeTop, effectiveAlpha)
	result := Decide(candidates, cfg)

	got := LabelUnknown
	score := 0.0
	if len(candidates) > 0 {
		got = result.Top.Label
		score = result.Top.FinalScore
	}

	correct := false
	switch result.Verdict {
	case VerdictClear:
		correct = got == c.ExpectedLabel
	case VerdictAmbiguous:
		correct = got == c.ExpectedLabel ||
			(result.RunnerUp != nil && result.RunnerUp.Label == c.ExpectedLabel)
	case VerdictLow:
		correct = c.ExpectedLabel == LabelAmbiguous || c.ExpectedLabel == LabelUnknown
	}

	return evalCaseResult{
		verdict: result.Verdict,
		got:     got,
		score:   score,
		correct: correct,
	}
}

// FormatReport returns a human-readable summary of the calibration report.
func FormatReport(r CalibrationReport) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Best: alpha=%.2f, delta=%.2f → accuracy=%.3f (%d/%d)\n",
		r.Best.Alpha, r.Best.Delta, r.Best.Accuracy, r.Best.Correct, r.Best.Total)
	fmt.Fprintf(&b, "  CLEAR=%d  AMBIGUOUS=%d  LOW=%d\n\n",
		r.Best.ClearCount, r.Best.AmbiguousCount, r.Best.LowCount)

	if len(r.Errors) > 0 {
		fmt.Fprintf(&b, "Remaining errors (%d):\n", len(r.Errors))
		for _, e := range r.Errors {
			fmt.Fprintf(&b, "  [%s] %q → expected=%s got=%s (score=%.3f, verdict=%s)\n",
				e.Verdict, truncateText(e.Message, 40), e.Expected, e.Got, e.Score, e.Verdict)
		}
	}

	return b.String()
}
