package tool

import (
	"testing"
	"time"
)

func TestExplainRoutingHintAdjustmentReportsAvoidEvidence(t *testing.T) {
	t.Parallel()
	tracker, err := NewUsageTracker("")
	if err != nil {
		t.Fatalf("NewUsageTracker: %v", err)
	}
	now := time.Now()
	for i := 0; i < contextOutcomeMinRecords; i++ {
		tracker.RecordExperience(ToolExperience{
			ToolName:    "fragile_tool",
			QueryTokens: []string{"deploy", "rollback"},
			Success:     false,
			Timestamp:   now.Add(time.Duration(i) * time.Minute),
			TaskType:    "deployment",
		})
	}

	got := tracker.ExplainRoutingHintAdjustment("fragile_tool", []string{"deploy"})
	if got.Adjustment >= 0 || got.Direction != "avoid" {
		t.Fatalf("adjustment = %+v, want negative avoid direction", got)
	}
	if got.MatchingRecords != contextOutcomeMinRecords || got.Failures != contextOutcomeMinRecords || got.FailureRate < 0.99 {
		t.Fatalf("evidence stats = %+v, want all failures", got)
	}
	if !routingHintExplanationHasReason(got.Reasons, "context_failure_rate") {
		t.Fatalf("reasons = %v, want context_failure_rate", got.Reasons)
	}
}

func TestRoutingHintAdjustmentMatchesExplanation(t *testing.T) {
	t.Parallel()
	tracker, err := NewUsageTracker("")
	if err != nil {
		t.Fatalf("NewUsageTracker: %v", err)
	}
	now := time.Now()
	for i := 0; i < contextOutcomeMinRecords; i++ {
		tracker.RecordExperience(ToolExperience{
			ToolName:     "failed_tool",
			QueryTokens:  []string{"browser", "research"},
			Success:      false,
			Timestamp:    now.Add(time.Duration(i) * time.Minute),
			TaskType:     "research",
			RecoveryTool: "browser_open",
		})
	}

	explanation := tracker.ExplainRoutingHintAdjustment("browser", []string{"browser"})
	adjustment := tracker.RoutingHintAdjustment("browser_open", []string{"browser"})
	if explanation.Adjustment != adjustment {
		t.Fatalf("explanation adjustment %.6f != direct adjustment %.6f", explanation.Adjustment, adjustment)
	}
	if adjustment <= 0 || explanation.Direction != "prefer" {
		t.Fatalf("adjustment = %+v, want positive recovery preference", explanation)
	}
}

func routingHintExplanationHasReason(reasons []string, want string) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}
