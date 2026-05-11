package main

import "testing"

func TestClassifySessionCompletionSignal(t *testing.T) {
	tests := []struct {
		name string
		line string
		want sessionCompletionSignalKind
	}{
		{name: "completed", line: "All done, changes applied.", want: sessionCompletionSignalCompleted},
		{name: "incomplete", line: "I need to continue with the tests.", want: sessionCompletionSignalIncomplete},
		{name: "unknown", line: "Reviewing the current files.", want: sessionCompletionSignalUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifySessionCompletionSignal(tt.line); got != tt.want {
				t.Fatalf("classifySessionCompletionSignal(%q) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}
