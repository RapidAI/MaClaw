package agentruntime

import (
	"errors"
	"testing"
)

func TestRecordFailure_NotCalledOnSkip(t *testing.T) {
	r := NewAdaptiveRetry(nil)

	for attempt := 0; attempt < 4; attempt++ {
		decision := r.Decide("llm_request", FailureNetwork, attempt)
		if decision.Action == RetryActionRetry {
			r.RecordFailure("llm_request", FailureNetwork, decision)
		}
	}

	if r.failureCounts["llm_request"] != 3 {
		t.Errorf("expected 3 failures recorded, got %d", r.failureCounts["llm_request"])
	}
	if r.IsDisabled("llm_request") {
		t.Error("should not be disabled after 3 failures (threshold is 5)")
	}
}

func TestNewAdaptiveRetryForLoop_ZeroMaxFailures_UsesDefault(t *testing.T) {
	template := &AdaptiveRetry{maxFailures: 0} // zero is treated as "not set"
	loop := NewAdaptiveRetryForLoop(template, nil)
	if loop.MaxFailures() != DefaultAdaptiveRetryMaxFailures {
		t.Errorf("zero maxFailures: expected default %d, got %d", DefaultAdaptiveRetryMaxFailures, loop.MaxFailures())
	}
}

// TestDecideDisablesAfterThresholdForNormalizableToolName pins the fix that
// Decide/IsDisabled must derive map keys exactly like RecordFailure: a tool
// name needing normalization ("Glob") previously accumulated failures under
// "glob" while the disable fence consulted "Glob" and never triggered.
func TestDecideDisablesAfterThresholdForNormalizableToolName(t *testing.T) {
	r := NewAdaptiveRetry(nil)
	for i := 0; i < DefaultAdaptiveRetryMaxFailures; i++ {
		r.RecordFailure("Glob", FailureLogic, RetryDecision{Action: RetryActionFix, Attempt: i})
	}
	if got := r.Decide("Glob", FailureLogic, DefaultAdaptiveRetryMaxFailures); got.Action != RetryActionDisable {
		t.Fatalf("expected disable after threshold, got %v", got.Action)
	}
	if !r.IsDisabled("Glob") {
		t.Fatal("IsDisabled must consult the normalized key")
	}
}

func TestClassifyAdaptiveRetryFailureMapsNetworkKind(t *testing.T) {
	// "connection reset" is recognized by the shared LLM classifier as a
	// network error; the adaptive retry classifier must honor that instead of
	// falling through to unknown.
	if got := ClassifyAdaptiveRetryFailure(errors.New("read: connection reset by peer")); got != FailureNetwork {
		t.Fatalf("connection reset classified as %v", got)
	}
}
