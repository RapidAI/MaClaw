package guiapp

import (
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

// The adaptive retry controller now lives in corelib/agentruntime so GUI,
// srv and TUI share one failure classification and retry policy. These
// aliases keep existing GUI call sites and tests unchanged; the GUI
// TrajectoryRecorder satisfies agentruntime.TrajectoryRecordSink implicitly.
type FailureCategory = agentruntime.FailureCategory
type RetryAction = agentruntime.RetryAction
type RetryDecision = agentruntime.RetryDecision
type AdaptiveRetry = agentruntime.AdaptiveRetry

const (
	FailureTransient   = agentruntime.FailureTransient
	FailureNetwork     = agentruntime.FailureNetwork
	FailurePeriodLimit = agentruntime.FailurePeriodLimit
	FailurePermission  = agentruntime.FailurePermission
	FailureArgs        = agentruntime.FailureArgs
	FailureLogic       = agentruntime.FailureLogic
	FailureUnknown     = agentruntime.FailureUnknown

	// Kept as alias for backward compatibility in tests and trace logs.
	FailureRateLimit = agentruntime.FailureRateLimit
)

const (
	RetryActionRetry   = agentruntime.RetryActionRetry
	RetryActionFix     = agentruntime.RetryActionFix
	RetryActionSkip    = agentruntime.RetryActionSkip
	RetryActionDisable = agentruntime.RetryActionDisable
)

// Test-pinned constants, forwarded from the shared implementation.
const (
	adaptiveRetryReviewThreshold            = agentruntime.AdaptiveRetryReviewThreshold
	adaptiveRetryReviewedFailureCountPrefix = agentruntime.AdaptiveRetryReviewedFailureCountPrefix
	defaultMaxFailures                      = agentruntime.DefaultAdaptiveRetryMaxFailures
)
const (
	maxTransientRetries = agentruntime.AdaptiveRetryMaxTransientRetries
	baseTransientDelay  = agentruntime.AdaptiveRetryBaseTransientDelay
	maxTransientDelay   = agentruntime.AdaptiveRetryMaxTransientDelay
)

func NewAdaptiveRetry(recorder *TrajectoryRecorder) *AdaptiveRetry {
	return agentruntime.NewAdaptiveRetry(recorder)
}

func NewAdaptiveRetryForLoop(template *AdaptiveRetry, recorder *TrajectoryRecorder) *AdaptiveRetry {
	return agentruntime.NewAdaptiveRetryForLoop(template, recorder)
}

func adaptiveRetrySafeTagValue(value string) string {
	return agentruntime.AdaptiveRetrySafeTagValue(value)
}
