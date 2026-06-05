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
