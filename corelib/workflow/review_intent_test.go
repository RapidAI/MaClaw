package workflow

import "testing"

func TestParseReviewIntentExactEnums(t *testing.T) {
	tests := map[string]ReviewIntent{
		"confirm":     ReviewIntentConfirm,
		"supplement":  ReviewIntentSupplement,
		"modify":      ReviewIntentSupplement,
		"skip":        ReviewIntentSkip,
		"cancel":      ReviewIntentCancel,
		"switch_task": ReviewIntentSwitchTask,
		"other":       ReviewIntentOther,
		" CONFIRM ":   ReviewIntentConfirm,
	}

	for raw, want := range tests {
		if got := ParseReviewIntent(raw); got != want {
			t.Fatalf("ParseReviewIntent(%q)=%q, want %q", raw, got, want)
		}
	}
}

func TestParseReviewIntentDoesNotUseSubstringMatching(t *testing.T) {
	for _, raw := range []string{"not_confirm", "cancelled?", "supplemental", "please confirm"} {
		if got := ParseReviewIntent(raw); got != ReviewIntentOther {
			t.Fatalf("ParseReviewIntent(%q)=%q, want other", raw, got)
		}
	}
}
