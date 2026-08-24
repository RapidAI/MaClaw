package browser

import (
	"testing"
	"time"
)

func TestRetryStrategyUsesShortDeterministicWaits(t *testing.T) {
	r := NewRetryStrategy(3, 3, nil)
	first := r.Decide(FailureElementNotFound, StepSpec{Action: "click"}, 0, nil)
	if !first.ShouldRetry || first.WaitBefore > time.Second || first.NeedsLLM {
		t.Fatalf("first element retry = %+v, want short retry without LLM", first)
	}
	second := r.Decide(FailureElementNotFound, StepSpec{Action: "click"}, 1, nil)
	if !second.ShouldRetry || second.WaitBefore > time.Second || second.NeedsLLM {
		t.Fatalf("second element retry = %+v, want short retry without LLM", second)
	}
	third := r.Decide(FailureElementNotFound, StepSpec{Action: "click"}, 2, nil)
	if third.ShouldRetry {
		t.Fatalf("third element retry = %+v, want fail fast", third)
	}
}

func TestClassifyFailurePolicyDeniedDoesNotRetry(t *testing.T) {
	r := NewRetryStrategy(3, 3, nil)
	err := policyDenied("browser policy blocked URL scheme: javascript")
	if got := r.ClassifyFailure(err, StepSpec{Action: "navigate"}); got != FailureNetworkBlocked {
		t.Fatalf("ClassifyFailure=%v", got)
	}
	decision := r.Decide(FailureNetworkBlocked, StepSpec{Action: "navigate"}, 0, nil)
	if decision.ShouldRetry {
		t.Fatalf("policy denied should not retry: %+v", decision)
	}
}

func TestRetryStrategyCapsDefaultTimeoutRetries(t *testing.T) {
	r := NewRetryStrategy(3, 3, nil)
	first := r.Decide(FailureTimeout, StepSpec{Action: "wait"}, 0, nil)
	if !first.ShouldRetry || first.AdjustedStep == nil || first.AdjustedStep.Timeout != 10*time.Second {
		t.Fatalf("first timeout retry = %+v, want 10s adjusted timeout", first)
	}
	second := r.Decide(FailureTimeout, StepSpec{Action: "wait"}, 1, nil)
	if !second.ShouldRetry || second.AdjustedStep == nil || second.AdjustedStep.Timeout != 20*time.Second {
		t.Fatalf("second timeout retry = %+v, want 20s adjusted timeout", second)
	}
}
