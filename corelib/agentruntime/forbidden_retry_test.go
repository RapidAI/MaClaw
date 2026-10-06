package agentruntime

import (
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
