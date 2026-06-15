package main

import (
	"testing"

	workflow "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func TestNormalizeWorkflowReviewIntentExactEnums(t *testing.T) {
	tests := map[string]workflow.ReviewIntent{
		"confirm":     workflow.ReviewIntentConfirm,
		"supplement":  workflow.ReviewIntentSupplement,
		"modify":      workflow.ReviewIntentSupplement,
		"skip":        workflow.ReviewIntentSkip,
		"cancel":      workflow.ReviewIntentCancel,
		"switch_task": workflow.ReviewIntentSwitchTask,
		"other":       workflow.ReviewIntentOther,
		" CONFIRM ":   workflow.ReviewIntentConfirm,
	}

	for raw, want := range tests {
		if got := normalizeWorkflowReviewIntent(raw); got != want {
			t.Fatalf("normalizeWorkflowReviewIntent(%q)=%q, want %q", raw, got, want)
		}
	}
}

func TestNormalizeWorkflowReviewIntentDoesNotUseSubstringMatching(t *testing.T) {
	for _, raw := range []string{"not_confirm", "cancelled?", "supplemental", "please confirm"} {
		if got := normalizeWorkflowReviewIntent(raw); got != workflow.ReviewIntentOther {
			t.Fatalf("normalizeWorkflowReviewIntent(%q)=%q, want other", raw, got)
		}
	}
}
