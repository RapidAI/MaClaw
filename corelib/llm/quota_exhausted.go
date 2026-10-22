package llm

import (
	"errors"
	"strings"
)

// IsQuotaExhaustedText reports a provider saying the account's quota is
// used up (WorkBuddy/CodeBuddy code 14018 "额度已用尽", OpenAI
// insufficient_quota). Such a 429 is not a burst limit: waiting seconds does
// not fix it, so it must not be retried on the same provider.
func IsQuotaExhaustedText(text string) bool {
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)
	return strings.Contains(lower, `"code":14018`) ||
		strings.Contains(lower, `"code": 14018`) ||
		strings.Contains(lower, `"code":"14018"`) ||
		strings.Contains(text, "额度已用尽") ||
		strings.Contains(text, "额度已用完") ||
		strings.Contains(text, "額度已用盡") ||
		strings.Contains(lower, "insufficient_quota") ||
		strings.Contains(lower, "quota exhausted") ||
		strings.Contains(lower, "quota_exhausted")
}

// IsQuotaExhaustedError reads both the error text and, for a body-free
// HTTPStatusError, the response body.
func IsQuotaExhaustedError(err error) bool {
	if err == nil {
		return false
	}
	var statusErr *HTTPStatusError
	if errors.As(err, &statusErr) && statusErr != nil && IsQuotaExhaustedText(string(statusErr.Body)) {
		return true
	}
	return IsQuotaExhaustedText(err.Error())
}
