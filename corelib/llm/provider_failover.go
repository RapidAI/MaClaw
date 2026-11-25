package llm

import (
	"errors"
	"net/http"
	"strings"
)

// ProviderFailoverEligible reports whether switching to another configured
// provider can plausibly recover the request. HTTP 429 is the historical
// quota/rate-limit case; gateway 5xx, request timeouts, and transport failures
// leave the turn with no usable response, so waiting on the same endpoint
// cannot help. 401/403 stay on the credential-refresh path, and 4xx request
// errors are deterministic for every provider.
func ProviderFailoverEligible(err error) bool {
	if err == nil {
		return false
	}
	var statusErr interface{ HTTPStatusCode() int }
	if errors.As(err, &statusErr) {
		status := statusErr.HTTPStatusCode()
		switch status {
		case http.StatusRequestTimeout, http.StatusTooManyRequests,
			http.StatusInternalServerError, http.StatusBadGateway,
			http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true
		}
		// A typed 4xx is deterministic for this request. Do not let a marker in
		// its body or wrapper text turn it into a provider outage.
		if status >= 400 && status < 500 {
			return false
		}
	}
	var timeoutErr interface{ Timeout() bool }
	if errors.As(err, &timeoutErr) && timeoutErr.Timeout() {
		return true
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	if text == "eof" || strings.Contains(text, ": eof") || strings.Contains(text, " eof") {
		return true
	}
	for _, marker := range []string{
		"connection refused", "connection reset", "connection attempt failed",
		"connection closed", "connection aborted", "forcibly closed",
		"server closed idle connection", "use of closed network connection",
		"client connection lost", "no such host", "network is unreachable",
		"network is down", "temporary failure in name resolution",
		"tls handshake timeout", "i/o timeout", "broken pipe", "wsarecv", "wsasend",
		"网络错误", "连接失败", "请求超时",
		// A provider that wraps an HTTP error as plain text loses the status
		// interface; these markers keep the retry able to change endpoints.
		"http 408", "http 429", "http 500", "http 502", "http 503", "http 504",
		"too many requests", "rate limit", "rate_limit",
		"bad gateway", "service unavailable", "gateway timeout",
		"server is overloaded", "overloaded_error",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
