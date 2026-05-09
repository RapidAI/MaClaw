package main

import (
	"context"
	"errors"
	"net"
	"strings"
)

// isRetryableLLMError returns true for errors that are worth retrying.
// Covers both transient server errors (429, 408, 5xx) and client-side
// connectivity errors (timeout, connection refused).
func isRetryableLLMError(err error) bool {
	if err == nil {
		return false
	}
	if isTransientServerError(err) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	s := err.Error()
	return strings.Contains(s, "context deadline exceeded") ||
		strings.Contains(s, "Client.Timeout") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "connection refused") ||
		strings.Contains(s, "SSE stream idle timeout")
}

func isHubPeriodLimitError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "llm_service_period_limited") ||
		strings.Contains(s, "current period credit limit") ||
		strings.Contains(s, "period limit") ||
		strings.Contains(s, "period quota") ||
		strings.Contains(s, "period credit") ||
		strings.Contains(s, "\u5468\u671f\u9650\u6d41") ||
		strings.Contains(s, "\u5f53\u524d\u5468\u671f\u989d\u5ea6\u5df2\u7528\u5c3d") ||
		strings.Contains(s, "鍛ㄦ湡闄愭祦") ||
		strings.Contains(s, "褰撳墠鍛ㄦ湡棰濆害宸茬敤灏")
}

// isTransientServerError returns true for recoverable server-side errors.
// These are errors where the server is temporarily unable to process the
// request, but may succeed if retried after a delay.
//
// This is the single source of truth for "is this error worth retrying
// with exponential backoff". Both AdaptiveRetry.Classify() and the
// fallback retry path in the agent loop delegate to this function.
//
// Covered error types:
//   - HTTP 429 (Too Many Requests / rate limit)
//   - HTTP 408 (Request Timeout — server-side)
//   - HTTP 502 (Bad Gateway)
//   - HTTP 503 (Service Unavailable)
//   - HTTP 504 (Gateway Timeout)
//   - HTTP 5xx (other server errors)
//   - API-specific transient errors (quota exceeded, overloaded, etc.)
//   - 智谱 API code:1234 transient "网络错误"
func isTransientServerError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())

	// Hub grant period limits are quota-window state, not a short transient
	// provider throttle. Retrying immediately only delays the clear user
	// message that already includes the recovery time.
	if isHubPeriodLimitError(err) {
		return false
	}

	// --- Rate limit (429) ---
	if strings.Contains(s, "429") ||
		strings.Contains(s, "rate limit") ||
		strings.Contains(s, "rate_limit") ||
		strings.Contains(s, "too many requests") ||
		strings.Contains(s, "请求过于频繁") ||
		strings.Contains(s, "调用频率") ||
		strings.Contains(s, "限流") {
		return true
	}

	// --- Quota / capacity ---
	// Note: "insufficient_quota" is NOT included here — it means the
	// account has no remaining credits, which is a permanent condition
	// that retrying won't fix. Only transient capacity errors are covered.
	if strings.Contains(s, "quota exceeded") ||
		strings.Contains(s, "overloaded") ||
		strings.Contains(s, "server is overloaded") ||
		strings.Contains(s, "服务繁忙") ||
		strings.Contains(s, "服务过载") {
		return true
	}

	// --- Server errors (5xx) ---
	if strings.Contains(s, "http 500") ||
		strings.Contains(s, "http 502") ||
		strings.Contains(s, "http 503") ||
		strings.Contains(s, "http 504") ||
		strings.Contains(s, "bad gateway") ||
		strings.Contains(s, "service unavailable") ||
		strings.Contains(s, "gateway timeout") ||
		strings.Contains(s, "服务端错误") ||
		strings.Contains(s, "网关错误") ||
		strings.Contains(s, "网关超时") ||
		strings.Contains(s, "服务暂时不可用") {
		return true
	}

	// --- Request timeout (408) ---
	if strings.Contains(s, "http 408") ||
		strings.Contains(s, "request timeout") {
		return true
	}

	// --- 智谱 API code:1234 transient "网络错误" ---
	if (strings.Contains(s, `"code":"1234"`) || strings.Contains(s, `"code": "1234"`)) &&
		strings.Contains(s, "网络错误") {
		return true
	}

	return false
}

// isContextWindowExceeded returns true when the LLM API rejects the request
// because the input exceeds the model's context window. This is NOT a
// retryable error in the normal sense — the caller must reduce the input
// size before retrying.
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
		strings.Contains(s, "请求的 token 数超过") ||
		strings.Contains(s, "token 数量超过") ||
		strings.Contains(s, "超出上下文长度")
}
