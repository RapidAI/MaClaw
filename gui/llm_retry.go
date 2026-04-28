package main

import (
	"context"
	"errors"
	"net"
	"strings"
)

// isRetryableLLMError returns true for timeout and temporary network errors
// that are worth retrying once.
func isRetryableLLMError(err error) bool {
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
	s := err.Error()
	return strings.Contains(s, "context deadline exceeded") ||
		strings.Contains(s, "Client.Timeout") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "connection refused") ||
		strings.Contains(s, "SSE stream idle timeout") ||
		strings.Contains(s, "HTTP 502") ||
		strings.Contains(s, "HTTP 503") ||
		strings.Contains(s, "HTTP 504")
}

// isContextWindowExceeded returns true when the LLM API rejects the request
// because the input exceeds the model's context window. This is NOT a
// retryable error in the normal sense — the caller must reduce the input
// size before retrying.
//
// Inspired by Codex CLI's ContextWindowExceeded error variant which triggers
// progressive head truncation (remove oldest entry, retry) instead of failing.
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
