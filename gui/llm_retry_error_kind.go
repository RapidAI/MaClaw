package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

type llmRetryErrorKind int

const (
	llmRetryErrorUnknown llmRetryErrorKind = iota
	llmRetryErrorTransientServer
	llmRetryErrorNetwork
	llmRetryErrorPeriodLimit
	llmRetryErrorContextWindow
)

func classifyLLMRetryError(err error) llmRetryErrorKind {
	switch {
	case err == nil:
		return llmRetryErrorUnknown
	case isHubPeriodLimitError(err):
		return llmRetryErrorPeriodLimit
	case isContextWindowExceeded(err):
		return llmRetryErrorContextWindow
	case isTransientServerError(err):
		return llmRetryErrorTransientServer
	case isNetworkLLMRetryError(err):
		return llmRetryErrorNetwork
	default:
		return llmRetryErrorUnknown
	}
}

func (k llmRetryErrorKind) Retryable() bool {
	return k == llmRetryErrorTransientServer || k == llmRetryErrorNetwork
}

func (k llmRetryErrorKind) TransientServer() bool {
	return k == llmRetryErrorTransientServer
}

func (k llmRetryErrorKind) ContextWindowExceeded() bool {
	return k == llmRetryErrorContextWindow
}

func isNetworkLLMRetryError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return hasLLMNetworkRetryMarker(err.Error())
}

func hasLLMNetworkRetryMarker(s string) bool {
	return strings.Contains(s, "context deadline exceeded") ||
		strings.Contains(s, "Client.Timeout") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "connection refused") ||
		strings.Contains(s, "SSE stream idle timeout")
}

func isRetryableLLMError(err error) bool {
	return classifyLLMRetryError(err).Retryable()
}

func isLLMHTTPStatusError(err error, status int) bool {
	if err == nil {
		return false
	}
	var httpErr *llm.HTTPStatusError
	if errors.As(err, &httpErr) && httpErr != nil {
		return httpErr.StatusCode == status
	}
	return strings.Contains(err.Error(), fmt.Sprintf("HTTP %d", status))
}

func isHubPeriodLimitError(err error) bool {
	if err == nil {
		return false
	}
	if hasHubPeriodLimitMarker(err.Error()) {
		return true
	}
	// HTTPStatusError.Error() only reports status+body_len; inspect the body.
	var httpErr *llm.HTTPStatusError
	if errors.As(err, &httpErr) && httpErr != nil && len(httpErr.Body) > 0 {
		return hasHubPeriodLimitMarker(string(httpErr.Body))
	}
	return false
}

func hasHubPeriodLimitMarker(s string) bool {
	s = strings.ToLower(s)
	return strings.Contains(s, "llm_service_period_limited") ||
		strings.Contains(s, "current period credit limit") ||
		strings.Contains(s, "period limit") ||
		strings.Contains(s, "period quota") ||
		strings.Contains(s, "period credit") ||
		strings.Contains(s, "周期限流") ||
		strings.Contains(s, "周期额度") ||
		strings.Contains(s, "period-limited")
}

// hasHubGatewayPressureMarker detects Hub/gateway soft rate-limit / queue pressure
// that should be retried or client-side throttled. Period-quota exhaustion is
// intentionally excluded (callers must check isHubPeriodLimitError first).
func hasHubGatewayPressureMarker(s string) bool {
	s = strings.ToLower(s)
	return strings.Contains(s, "llm_endpoint_user_rate_limited") ||
		strings.Contains(s, "llm_provider_queue_full") ||
		strings.Contains(s, "llm_provider_queue_timeout") ||
		strings.Contains(s, "llm_endpoint_concurrency_full") ||
		strings.Contains(s, "请求过于频繁") ||
		strings.Contains(s, "请求过快") ||
		strings.Contains(s, "排队已超时") ||
		strings.Contains(s, "上游队列已满") ||
		strings.Contains(s, "上游排队等待超时") ||
		strings.Contains(s, "网关并发已满") ||
		strings.Contains(s, "调用频率超限")
}

func isHubRateLimitWaitCanceledError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	if strings.Contains(s, "llm_endpoint_user_rate_limit_wait_canceled") ||
		strings.Contains(s, "限流排队等待时被取消") ||
		strings.Contains(s, "canceled while waiting in hub user rate-limit queue") {
		return true
	}
	var httpErr *llm.HTTPStatusError
	if errors.As(err, &httpErr) && httpErr != nil && len(httpErr.Body) > 0 {
		body := strings.ToLower(string(httpErr.Body))
		return strings.Contains(body, "llm_endpoint_user_rate_limit_wait_canceled")
	}
	return false
}

func isTransientServerError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	if isHubPeriodLimitError(err) {
		return false
	}
	// Client/parent canceled while Hub was pacing — do not auto-retry as if the server is busy.
	if isHubRateLimitWaitCanceledError(err) {
		return false
	}
	if strings.Contains(s, "服务繁忙") ||
		strings.Contains(s, "网络错误") {
		return true
	}
	if strings.Contains(s, "429") ||
		strings.Contains(s, "rate limit") ||
		strings.Contains(s, "rate_limit") ||
		strings.Contains(s, "too many requests") ||
		hasHubGatewayPressureMarker(s) {
		return true
	}
	if strings.Contains(s, "quota exceeded") ||
		strings.Contains(s, "overloaded") ||
		strings.Contains(s, "server is overloaded") ||
		strings.Contains(s, "overloaded_error") ||
		strings.Contains(s, `"code":"1305"`) ||
		strings.Contains(s, `"code": "1305"`) ||
		strings.Contains(s, "鏈嶅姟绻佸繖") ||
		strings.Contains(s, "鏈嶅姟杩囪浇") {
		return true
	}
	if strings.Contains(s, "http 500") ||
		strings.Contains(s, "http 502") ||
		strings.Contains(s, "http 503") ||
		strings.Contains(s, "http 504") ||
		strings.Contains(s, "bad gateway") ||
		strings.Contains(s, "service unavailable") ||
		strings.Contains(s, "gateway timeout") ||
		strings.Contains(s, "鏈嶅姟绔") ||
		strings.Contains(s, "缃戝叧閿欒") ||
		strings.Contains(s, "缃戝叧瓒呮椂") ||
		strings.Contains(s, "鏈嶅姟鏆傛椂") {
		return true
	}
	if strings.Contains(s, "http 408") || strings.Contains(s, "request timeout") {
		return true
	}
	return (strings.Contains(s, `"code":"1234"`) || strings.Contains(s, `"code": "1234"`)) &&
		(strings.Contains(s, "缃戠粶閿欒") || strings.Contains(s, "缃戠粶閿欒"))
}

func isContextWindowExceeded(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "context_length_exceeded") ||
		strings.Contains(s, "context window") ||
		strings.Contains(s, "maximum context length") ||
		strings.Contains(s, "prompt is too long") ||
		strings.Contains(s, "input exceeds") ||
		strings.Contains(s, "too many tokens") ||
		strings.Contains(s, "token 鏁") ||
		strings.Contains(s, "瓒呭嚭涓婁笅鏂")
}
