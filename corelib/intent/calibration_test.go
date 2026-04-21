package intent

import (
	"testing"
)

// TestRunGridSearch_KeywordOnly runs calibration using only the keyword channel
// (simulating embedding scores from keyword confidence). This validates the
// grid search mechanics without requiring a real embedder or LLM.
func TestRunGridSearch_KeywordOnly(t *testing.T) {
	cases := ProductionCases()
	if len(cases) < 50 {
		t.Fatalf("expected at least 50 calibration cases, got %d", len(cases))
	}

	registry := NewKeywordRegistry()
	affinity := NewToolAffinityRegistry()

	// Build a keyword-based scorer that returns L1 classification as a
	// single-element score list (simulating a channel that always returns
	// the keyword result).
	keywordScorer := func(text string) []labelScore {
		result, _ := classifyByKeywords(registry, affinity, MessageContext{Text: text})
		if result.Primary == LabelUnknown {
			return nil
		}
		return []labelScore{LabelScore(result.Primary, result.Confidence)}
	}

	report := RunGridSearch(
		cases,
		keywordScorer, // embedding channel = keyword scores
		nil,           // no tree channel
		[]float64{1.0}, // alpha=1.0 (embedding only since no tree)
		[]float64{0.05, 0.10, 0.15},
	)

	t.Logf("Best: alpha=%.2f delta=%.2f accuracy=%.3f (%d/%d)",
		report.Best.Alpha, report.Best.Delta,
		report.Best.Accuracy, report.Best.Correct, report.Best.Total)

	// Keyword-only should get at least 60% accuracy on production cases.
	if report.Best.Accuracy < 0.60 {
		t.Errorf("keyword-only accuracy too low: %.3f", report.Best.Accuracy)
	}

	if len(report.Errors) > 0 {
		t.Logf("Errors (%d):", len(report.Errors))
		for _, e := range report.Errors {
			t.Logf("  %q: expected=%s got=%s (score=%.3f)", 
				truncateText(e.Message, 40), e.Expected, e.Got, e.Score)
		}
	}
}

// TestRunGridSearch_DualChannel simulates dual-channel fusion with synthetic
// scores to verify the grid search correctly finds optimal parameters.
func TestRunGridSearch_DualChannel(t *testing.T) {
	cases := []CalibrationCase{
		{Message: "开发游戏", ExpectedLabel: LabelCoding},
		{Message: "修复bug", ExpectedLabel: LabelBugFix},
		{Message: "登录服务器", ExpectedLabel: LabelSSH},
		{Message: "翻译文档", ExpectedLabel: LabelNonCoding},
		{Message: "继续", ExpectedLabel: LabelContinuation},
	}

	// Synthetic embedding scorer: returns correct label with high score.
	embScorer := func(text string) []labelScore {
		for _, c := range cases {
			if c.Message == text {
				return []labelScore{
					LabelScore(c.ExpectedLabel, 0.85),
					LabelScore(LabelUnknown, 0.10),
				}
			}
		}
		return nil
	}

	// Synthetic tree scorer: also returns correct label.
	treeScorer := func(text string) []labelScore {
		for _, c := range cases {
			if c.Message == text {
				return []labelScore{
					LabelScore(c.ExpectedLabel, 0.90),
				}
			}
		}
		return nil
	}

	report := RunGridSearch(cases, embScorer, treeScorer, nil, nil)

	// With perfect synthetic scores, accuracy should be 100%.
	if report.Best.Accuracy != 1.0 {
		t.Errorf("expected 100%% accuracy with perfect scores, got %.3f", report.Best.Accuracy)
	}
	if len(report.Errors) != 0 {
		t.Errorf("expected 0 errors, got %d", len(report.Errors))
	}
}

// TestRunGridSearch_EmptyScorers verifies behavior when both scorers are nil.
func TestRunGridSearch_EmptyScorers(t *testing.T) {
	cases := []CalibrationCase{
		{Message: "test", ExpectedLabel: LabelCoding},
	}

	report := RunGridSearch(cases, nil, nil, []float64{0.5}, []float64{0.1})

	// No scores → all LOW → only ambiguous/unknown expected labels count as correct.
	if report.Best.Accuracy != 0.0 {
		t.Errorf("expected 0%% accuracy with no scorers, got %.3f", report.Best.Accuracy)
	}
}

// TestFormatReport verifies the report formatting doesn't panic.
func TestFormatReport(t *testing.T) {
	report := CalibrationReport{
		Best: GridPoint{
			Alpha: 0.15, Delta: 0.10, Accuracy: 0.95,
			Correct: 95, Total: 100,
			ClearCount: 80, AmbiguousCount: 15, LowCount: 5,
		},
		Errors: []CalibrationError{
			{Message: "test message", Expected: LabelCoding, Got: LabelSSH, Score: 0.45, Verdict: VerdictAmbiguous},
		},
	}

	output := FormatReport(report)
	if output == "" {
		t.Error("expected non-empty report")
	}
	if !containsSubstring(output, "alpha=0.15") {
		t.Error("report should contain alpha value")
	}
	if !containsSubstring(output, "Remaining errors") {
		t.Error("report should contain errors section")
	}
}

// TestProductionCases_Coverage verifies that production cases cover all
// major intent labels.
func TestProductionCases_Coverage(t *testing.T) {
	cases := ProductionCases()
	covered := make(map[IntentLabel]int)
	for _, c := range cases {
		covered[c.ExpectedLabel]++
	}

	// Every non-unknown label should have at least 2 cases.
	requiredLabels := []IntentLabel{
		LabelCoding, LabelBugFix, LabelMaintenance, LabelSSH,
		LabelBrowser, LabelSearch, LabelNonCoding, LabelDocumentDelivery,
		LabelOffice, LabelContinuation,
	}
	for _, label := range requiredLabels {
		if covered[label] < 2 {
			t.Errorf("label %s has only %d cases (need at least 2)", label, covered[label])
		}
	}

	t.Logf("Total cases: %d, label coverage: %v", len(cases), covered)
}
