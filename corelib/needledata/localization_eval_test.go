package needledata

import "testing"

func TestEvaluateLocalizations(t *testing.T) {
	got := EvaluateLocalizations([]LocalizationPrediction{
		{ExpectedFiles: []string{"a.go"}, ExpectedSymbols: []string{"Fix"}, RankedFiles: []string{"a.go"}, RankedSymbols: []string{"Fix"}, ToolCalls: 2, DurationMS: 10, IrrelevantReads: 0},
		{ExpectedFiles: []string{"b.go"}, ExpectedSymbols: []string{"Run"}, RankedFiles: []string{"x.go", "b.go"}, RankedSymbols: []string{"X", "Run"}, ToolCalls: 4, DurationMS: 30, IrrelevantReads: 2},
	})
	if got.FileHitAt1 != .5 || got.FileHitAt3 != 1 || got.FunctionHitAt1 != .5 || got.MRR != .75 {
		t.Fatalf("unexpected metrics: %+v", got)
	}
	if got.MedianToolCalls != 3 || got.MedianDurationMS != 20 || got.MedianIrrelevantReads != 1 {
		t.Fatalf("unexpected medians: %+v", got)
	}
}
