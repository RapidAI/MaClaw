package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
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
	return strings.Contains(err.Error(), fmt.Sprintf("HTTP %d", status))
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
		strings.Contains(s, "鍛ㄦ湡闄愭祦")
}

func isTransientServerError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	if isHubPeriodLimitError(err) {
		return false
	}
	if strings.Contains(s, "\u670d\u52a1\u7e41\u5fd9") ||
		strings.Contains(s, "\u8bf7\u7a0d\u540e\u91cd\u8bd5") ||
		strings.Contains(s, "\u7f51\u7edc\u9519\u8bef") ||
		strings.Contains(s, "\u8c03\u7528\u9891\u7387\u8d85\u9650") {
		return true
	}
	if strings.Contains(s, "429") ||
		strings.Contains(s, "rate limit") ||
		strings.Contains(s, "rate_limit") ||
		strings.Contains(s, "too many requests") ||
		strings.Contains(s, "璇锋眰杩囦簬棰戠箒") ||
		strings.Contains(s, "璋冪敤棰戠巼瓒呴檺") ||
		strings.Contains(s, "璋冪敤棰戠巼") ||
		strings.Contains(s, "闄愭祦") {
		return true
	}
	if strings.Contains(s, "quota exceeded") ||
		strings.Contains(s, "overloaded") ||
		strings.Contains(s, "server is overloaded") ||
		strings.Contains(s, "鏈嶅姟绻佸繖") ||
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
