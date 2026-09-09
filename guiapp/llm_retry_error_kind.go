package guiapp

import (
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

// LLM retry classification now lives in corelib/agentruntime so GUI, srv and
// TUI share one transient/period-limit/network vocabulary. These aliases and
// wrappers keep existing GUI call sites unchanged.
type llmRetryErrorKind = agentruntime.LLMRetryErrorKind

const (
	llmRetryErrorUnknown         = agentruntime.LLMRetryErrorUnknown
	llmRetryErrorTransientServer = agentruntime.LLMRetryErrorTransientServer
	llmRetryErrorNetwork         = agentruntime.LLMRetryErrorNetwork
	llmRetryErrorPeriodLimit     = agentruntime.LLMRetryErrorPeriodLimit
	llmRetryErrorContextWindow   = agentruntime.LLMRetryErrorContextWindow
)

func classifyLLMRetryError(err error) llmRetryErrorKind {
	return agentruntime.ClassifyLLMRetryError(err)
}

func isNetworkLLMRetryError(err error) bool {
	return agentruntime.IsNetworkLLMRetryError(err)
}

func hasLLMNetworkRetryMarker(s string) bool {
	return agentruntime.HasLLMNetworkRetryMarker(s)
}

func isRetryableLLMError(err error) bool {
	return agentruntime.IsRetryableLLMError(err)
}

func isLLMHTTPStatusError(err error, status int) bool {
	return agentruntime.IsLLMHTTPStatusError(err, status)
}

func isHubPeriodLimitError(err error) bool {
	return agentruntime.IsHubPeriodLimitError(err)
}

func hasHubPeriodLimitMarker(s string) bool {
	return agentruntime.HasHubPeriodLimitMarker(s)
}

func hasHubGatewayPressureMarker(s string) bool {
	return agentruntime.HasHubGatewayPressureMarker(s)
}

func isHubRateLimitWaitCanceledError(err error) bool {
	return agentruntime.IsHubRateLimitWaitCanceledError(err)
}

func isTransientServerError(err error) bool {
	return agentruntime.IsTransientServerError(err)
}

func isContextWindowExceeded(err error) bool {
	return agentruntime.IsContextWindowExceeded(err)
}
