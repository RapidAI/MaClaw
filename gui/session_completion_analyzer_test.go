package main

import "testing"

func TestCompletionAnalyzerUsesSignalKinds(t *testing.T) {
	analyzer := NewCompletionAnalyzer(CompletionAnalyzerConfig{})
	if got := analyzer.Analyze([]string{"I need to continue with the tests."}, "", nil); got != CompletionIncomplete {
		t.Fatalf("Analyze incomplete = %v, want %v", got, CompletionIncomplete)
	}
	if got := analyzer.Analyze([]string{"All done, changes applied."}, "", nil); got != CompletionCompleted {
		t.Fatalf("Analyze completed = %v, want %v", got, CompletionCompleted)
	}
}
