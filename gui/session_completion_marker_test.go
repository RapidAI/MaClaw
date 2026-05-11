package main

import "testing"

func TestClassifyGeminiACPTurnCompleteMarkerUsesTokens(t *testing.T) {
	tests := []struct {
		name string
		line string
		want sessionCompletionMarkerKind
	}{
		{name: "completed token", line: "[gemini-acp] turn complete: completed", want: sessionCompletionMarkerCompleted},
		{name: "success token", line: "[gemini-acp] turn complete: success", want: sessionCompletionMarkerCompleted},
		{name: "cancelled token", line: "[gemini-acp] turn complete: cancelled", want: sessionCompletionMarkerIncomplete},
		{name: "reject success substring", line: "[gemini-acp] turn complete: successor", want: sessionCompletionMarkerUnknown},
		{name: "reject done substring", line: "[gemini-acp] turn complete: undone", want: sessionCompletionMarkerUnknown},
		{name: "ignore other line", line: "[gemini-acp] session error: completed", want: sessionCompletionMarkerUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyGeminiACPTurnCompleteMarker(tt.line); got != tt.want {
				t.Fatalf("classifyGeminiACPTurnCompleteMarker(%q) = %q, want %q", tt.line, got, tt.want)
			}
		})
	}
}
