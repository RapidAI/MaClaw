package agent

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestQuotaExhausted429IsNotRetried(t *testing.T) {
	err := &llm.HTTPStatusError{StatusCode: 429, Body: []byte(`{"error":{"data":{"code":14018,"msg":"额度已用尽"}}}`)}
	if shouldRetrySimpleLLMError(err) {
		t.Fatal("an exhausted quota was retried")
	}
	if !shouldRetrySimpleLLMError(&llm.HTTPStatusError{StatusCode: 429, Body: []byte(`{"error":"slow down"}`)}) {
		t.Fatal("a burst 429 must still retry")
	}
}
