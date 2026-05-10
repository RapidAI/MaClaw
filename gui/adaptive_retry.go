package main

import (
	"fmt"
	"time"
)

// FailureCategory classifies a failed tool or LLM operation.
type FailureCategory string

type RetryAction string

const (
	FailureTransient   FailureCategory = "transient"
	FailureNetwork     FailureCategory = "network"
	FailurePeriodLimit FailureCategory = "period_limit"
	FailurePermission  FailureCategory = "permission"
	FailureArgs        FailureCategory = "args"
	FailureLogic       FailureCategory = "logic"
	FailureUnknown     FailureCategory = "unknown"

	// Kept as alias for backward compatibility in tests and trace logs.
	FailureRateLimit = FailureTransient
)

const (
	RetryActionRetry   RetryAction = "retry"
	RetryActionFix     RetryAction = "fix"
	RetryActionSkip    RetryAction = "skip"
	RetryActionDisable RetryAction = "disable"
)

const (
	defaultMaxFailures  = 5
	maxNetworkRetries   = 3
	maxTransientRetries = 3
	baseRetryDelay      = 1 * time.Second
	baseTransientDelay  = 5 * time.Second
)

// RetryDecision describes the next retry action.
type RetryDecision struct {
	Action       RetryAction
	Delay        time.Duration
	ErrorContext string
	Attempt      int
}

// AdaptiveRetry classifies failures and chooses retry behavior.
type AdaptiveRetry struct {
	failureCounts map[string]int
	maxFailures   int
	disabledTools map[string]bool
	recorder      *TrajectoryRecorder
}

// NewAdaptiveRetry creates an adaptive retry controller.
func NewAdaptiveRetry(recorder *TrajectoryRecorder) *AdaptiveRetry {
	return &AdaptiveRetry{
		failureCounts: make(map[string]int),
		maxFailures:   defaultMaxFailures,
		disabledTools: make(map[string]bool),
		recorder:      recorder,
	}
}

// Classify maps an error to a retry failure category.
func (r *AdaptiveRetry) Classify(toolName string, err error) FailureCategory {
	return classifyAdaptiveRetryFailure(err)
}

// Decide chooses a retry strategy for a failure category and attempt count.
func (r *AdaptiveRetry) Decide(toolName string, category FailureCategory, attempt int) RetryDecision {
	if r.failureCounts[toolName] >= r.maxFailures {
		return RetryDecision{
			Action:       RetryActionDisable,
			ErrorContext: fmt.Sprintf("tool %s failed %d times and has been disabled; use an alternative path", toolName, r.failureCounts[toolName]),
			Attempt:      attempt,
		}
	}

	switch category {
	case FailurePeriodLimit:
		return RetryDecision{
			Action:       RetryActionSkip,
			ErrorContext: fmt.Sprintf("MaClaw period quota is exhausted for tool %s; wait for quota recovery or switch provider", toolName),
			Attempt:      attempt,
		}
	case FailureTransient:
		if attempt >= maxTransientRetries {
			return RetryDecision{
				Action:       RetryActionSkip,
				ErrorContext: fmt.Sprintf("tool %s transient server error retry limit reached (%d); skip", toolName, maxTransientRetries),
				Attempt:      attempt,
			}
		}
		return RetryDecision{
			Action:  RetryActionRetry,
			Delay:   baseTransientDelay * time.Duration(1<<uint(attempt)),
			Attempt: attempt,
		}
	case FailureNetwork:
		if attempt >= maxNetworkRetries {
			return RetryDecision{
				Action:       RetryActionSkip,
				ErrorContext: fmt.Sprintf("tool %s network retry limit reached (%d); skip", toolName, maxNetworkRetries),
				Attempt:      attempt,
			}
		}
		return RetryDecision{
			Action:  RetryActionRetry,
			Delay:   baseRetryDelay * time.Duration(1<<uint(attempt)),
			Attempt: attempt,
		}
	case FailureArgs, FailureLogic:
		return RetryDecision{
			Action:       RetryActionFix,
			ErrorContext: fmt.Sprintf("tool %s failed with %s; adjust arguments or logic before retrying", toolName, string(category)),
			Attempt:      attempt,
		}
	case FailurePermission:
		return RetryDecision{
			Action:       RetryActionSkip,
			ErrorContext: fmt.Sprintf("tool %s failed because of permission or authentication; fix credentials before retrying", toolName),
			Attempt:      attempt,
		}
	default:
		if attempt >= 1 {
			return RetryDecision{
				Action:       RetryActionSkip,
				ErrorContext: fmt.Sprintf("tool %s failed with an unknown error after %d attempts; skip", toolName, attempt),
				Attempt:      attempt,
			}
		}
		return RetryDecision{
			Action:  RetryActionRetry,
			Delay:   baseRetryDelay,
			Attempt: attempt,
		}
	}
}

// RecordFailure records a failed retry decision.
func (r *AdaptiveRetry) RecordFailure(toolName string, category FailureCategory, decision RetryDecision) {
	r.failureCounts[toolName]++
	if r.failureCounts[toolName] >= r.maxFailures {
		r.disabledTools[toolName] = true
	}
	if r.recorder == nil {
		return
	}
	content := fmt.Sprintf("tool=%s category=%s action=%s attempt=%d",
		toolName, string(category), decision.Action, decision.Attempt)
	r.recorder.Record("system", content, nil, "", "adaptive_retry")
}

// IsDisabled reports whether a tool has been disabled after repeated failures.
func (r *AdaptiveRetry) IsDisabled(toolName string) bool {
	return r.disabledTools[toolName]
}
