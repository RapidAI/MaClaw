package intent

import (
	"testing"
)

// TestRunGridSearch_KeywordOnlyDisabled verifies that the retained keyword
// compatibility entrypoint does not act as a semantic scoring channel.
func TestRunGridSearch_KeywordOnlyDisabled(t *testing.T) {
	cases := ProductionCases()
	if len(cases) < 50 {
		t.Fatalf("expected at least 50 calibration cases, got %d", len(cases))
	}

	registry := NewKeywordRegistry()
	affinity := NewToolAffinityRegistry()

	keywordScorer := func(text string) []labelScore {
		result, _ := classifyByKeywords(registry, affinity, MessageContext{Text: text})
		if result.Primary == LabelUnknown {
			return nil
		}
		return []labelScore{LabelScore(result.Primary, result.Confidence)}
	}

	report := RunGridSearch(
		cases,
		keywordScorer,  // embedding channel = keyword scores
		nil,            // no tree channel
		[]float64{1.0}, // alpha=1.0 (embedding only since no tree)
		[]float64{0.05, 0.10, 0.15},
	)

	t.Logf("Best: alpha=%.2f delta=%.2f accuracy=%.3f (%d/%d)",
		report.Best.Alpha, report.Best.Delta,
		report.Best.Accuracy, report.Best.Correct, report.Best.Total)

	if report.Best.ClearCount != 0 {
		t.Errorf("keyword-only disabled scorer produced %d clear decisions", report.Best.ClearCount)
	}
	if report.Best.LowCount != report.Best.Total {
		t.Errorf("keyword-only disabled scorer low count=%d total=%d", report.Best.LowCount, report.Best.Total)
	}
	if report.Best.Accuracy > 0.10 {
		t.Errorf("keyword-only disabled scorer accuracy unexpectedly high: %.3f", report.Best.Accuracy)
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
