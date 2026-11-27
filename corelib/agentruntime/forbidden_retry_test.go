package agentruntime

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestClassifyLLMRetryError_ForbiddenBackoff(t *testing.T) {
	// Transient gateway 403 (HA authorization-sync gap) must classify as a
	// transient server error so both the desktop fallback retry and the
	// adaptive retry back off and retry it.
	transient := &llm.HTTPStatusError{
		StatusCode: http.StatusForbidden,
		Body:       []byte(`{"error":"authorization denied: no active authorization for hub=h tenant=t group=g"}`),
	}
	kind := ClassifyLLMRetryError(fmt.Errorf("LLM call failed: %w", transient))
	if kind != LLMRetryErrorTransientServer || !kind.Retryable() {
		t.Fatalf("transient 403 kind = %v, want retryable transient server", kind)
	}
	// Adaptive classification composes with the same kind: permission-category
	// skip decisions must not swallow a transient 403 either.
	if got := ClassifyAdaptiveRetryFailure(transient); got != FailureTransient {
		t.Fatalf("adaptive category = %v, want transient", got)
	}
	// Permanent denials stay non-retryable.
	permanent := &llm.HTTPStatusError{
		StatusCode: http.StatusForbidden,
		Body:       []byte(`{"error":{"type":"content_policy_violation","message":"blocked by content policy"}}`),
	}
	if kind := ClassifyLLMRetryError(permanent); kind.Retryable() {
		t.Fatalf("content-policy 403 kind = %v, must not be retryable", kind)
	}
	if got := ClassifyAdaptiveRetryFailure(permanent); got != FailurePermission {
		t.Fatalf("adaptive category = %v, want permission", got)
	}
}

func TestClassifyLLMRetryError_ResponsesStreamFailure(t *testing.T) {
	server := errors.New("Responses API stream failed: The server had an error while processing your request. (code=server_error)")
	if kind := ClassifyLLMRetryError(server); kind != LLMRetryErrorTransientServer || !IsTransientServerError(server) {
		t.Fatalf("server_error kind = %v, transient = %v", kind, IsTransientServerError(server))
	}
	leftover := errors.New("LLM call failed: Responses API stream failed: payload_len=115")
	if !IsRetryableLLMError(leftover) {
		t.Fatal("unparsed stream failure should stay retryable")
	}
	blocked := errors.New("Responses API stream failed: blocked by content filter (code=content_filter) overloaded connection reset timeout")
	if IsTransientServerError(blocked) || ClassifyLLMRetryError(blocked).Retryable() {
		t.Fatal("a permanent stream error must not fall through to overloaded or connection reset")
	}
	if got := ClassifyAdaptiveRetryFailure(blocked); got == FailureTransient || got == FailureNetwork {
		t.Fatalf("adaptive category = %v, want a non-retry", got)
	}
	if got := ClassifyAdaptiveRetryFailure(server); got != FailureTransient {
		t.Fatalf("adaptive category = %v, want transient", got)
	}
	quota := errors.New("Responses API stream failed: 额度已用尽")
	if IsTransientServerError(quota) || IsRetryableLLMError(quota) {
		t.Fatal("quota inside a stream frame stays terminal")
	}
	period := errors.New("Responses API stream failed: LLM_SERVICE_PERIOD_LIMITED")
	if IsTransientServerError(period) || IsRetryableLLMError(period) {
		t.Fatal("a period limit stays terminal even inside a stream frame")
	}
	if !IsTransientServerError(errors.New("HTTP 503")) {
		t.Fatal("an ordinary HTTP 503 stays transient")
	}
	serverClose := errors.New("WebSocket unexpected close (code=1011): websocket: close 1011 (internal server error)")
	if kind := ClassifyLLMRetryError(serverClose); kind != LLMRetryErrorTransientServer || !kind.Retryable() {
		t.Fatalf("server close kind = %v, want retryable transient server", kind)
	}
	if got := ClassifyAdaptiveRetryFailure(serverClose); got != FailureTransient {
		t.Fatalf("adaptive category = %v, want transient", got)
	}
	for _, code := range []string{"1006", "1008"} {
		closed := errors.New("WebSocket unexpected close (code=" + code + ")")
		if IsTransientServerError(closed) || ClassifyLLMRetryError(closed).Retryable() {
			t.Fatalf("close %s must not be retried", code)
		}
	}
	quotedClose := errors.New("Responses API stream failed: blocked by content filter (code=content_filter) WebSocket unexpected close (code=1011)")
	if IsTransientServerError(quotedClose) || ClassifyLLMRetryError(quotedClose).Retryable() {
		t.Fatal("a permanent frame that quotes a close code must not fall through")
	}
}
