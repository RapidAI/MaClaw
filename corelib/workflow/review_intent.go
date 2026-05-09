package workflow

import "strings"

// ParseReviewIntent normalizes classifier output into the strict review-intent
// enum. It intentionally accepts only exact enum labels, plus the legacy label
// "modify" as an alias for supplement.
func ParseReviewIntent(raw string) ReviewIntent {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(ReviewIntentConfirm):
		return ReviewIntentConfirm
	case string(ReviewIntentSupplement), "modify":
		return ReviewIntentSupplement
	case string(ReviewIntentSkip):
		return ReviewIntentSkip
	case string(ReviewIntentCancel):
		return ReviewIntentCancel
	case string(ReviewIntentSwitchTask):
		return ReviewIntentSwitchTask
	case string(ReviewIntentOther):
		return ReviewIntentOther
	default:
		return ReviewIntentOther
	}
}
