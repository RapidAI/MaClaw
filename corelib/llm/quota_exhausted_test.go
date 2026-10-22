package llm

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsQuotaExhaustedError(t *testing.T) {
	body := []byte(`{"error":{"data":{"code":14018,"msg":"额度已用尽，请前往获取更多额度"}}}`)
	if !IsQuotaExhaustedError(&HTTPStatusError{StatusCode: 429, Body: body}) {
		t.Fatal("body-free 429 with code 14018 was not quota exhausted")
	}
	if !IsQuotaExhaustedError(fmt.Errorf("wrap: %w", errors.New("WorkBuddy 429: 额度已用尽"))) {
		t.Fatal("wrapped text marker missed")
	}
	if IsQuotaExhaustedError(&HTTPStatusError{StatusCode: 429, Body: []byte(`{"error":"rate limit, slow down"}`)}) {
		t.Fatal("a burst rate limit is not quota exhaustion")
	}
}
