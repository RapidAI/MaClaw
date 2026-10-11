package agentservice

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// periodLimitHTTPError is what the provider actually returns when the period
// quota is gone. Its Error() is only "HTTP 429: body_len=N" — the marker lives
// in Body, so anything that keeps just the message loses the classification.
func periodLimitHTTPError() *llm.HTTPStatusError {
	return &llm.HTTPStatusError{
		StatusCode: 429,
		Body:       []byte(`{"error":{"code":"LLM_SERVICE_PERIOD_LIMITED","message":"MaClaw 官方周期限流：当前周期额度已用尽，约 6 小时 后恢复。"}}`),
	}
}

// TestLoopFailureErrorSurvivesBeyondUserFacingText pins the reason the cause has
// to travel with the result: the user-facing text alone cannot be classified.
// Draining the Body into prose is exactly what the old errors.New(result.Error)
// did, and it left every layer above the loop matching substrings.
func TestLoopFailureErrorSurvivesBeyondUserFacingText(t *testing.T) {
	cause := periodLimitHTTPError()
	text := "LLM call failed: MaClaw 官方周期限流：当前周期额度已用尽，约 6 小时 后恢复。"
	result := &agent.LoopResult{Error: text, Err: cause}

	err := loopFailureError(result)
	if err == nil {
		t.Fatal("loopFailureError returned nil for a failed result")
	}
	if !errors.Is(err, ErrLLMPeriodLimited) {
		t.Fatalf("errors.Is(ErrLLMPeriodLimited)=false, want true; err=%v", err)
	}
	var httpErr *llm.HTTPStatusError
	if !errors.As(err, &httpErr) {
		t.Fatal("errors.As(*llm.HTTPStatusError) failed: the provider status is gone")
	}
	if httpErr.StatusCode != 429 {
		t.Fatalf("StatusCode=%d want 429", httpErr.StatusCode)
	}
	if !agentruntime.IsHubPeriodLimitError(err) {
		t.Fatal("IsHubPeriodLimitError=false, want true")
	}
}

// TestLoopFailureErrorTextStaysReaderFacing guarantees Error() is byte-identical
// to the loop's text. That text becomes run.Error, is persisted and shown to the
// reader; wrapping must not append the raw HTTP status to it.
func TestLoopFailureErrorTextStaysReaderFacing(t *testing.T) {
	text := "LLM call failed: 上游暂时不可用，请稍后重试。"
	err := loopFailureError(&agent.LoopResult{Error: text, Err: periodLimitHTTPError()})
	if got := err.Error(); got != text {
		t.Fatalf("Error()=%q want %q — the raw cause leaked into reader-facing text", got, text)
	}
}

// TestLoopFailureErrorLeavesOtherFailuresUnclassified keeps the sentinel honest:
// a transport fault is not a period quota, and must not borrow its retry advice.
func TestLoopFailureErrorLeavesOtherFailuresUnclassified(t *testing.T) {
	cause := &llm.HTTPStatusError{StatusCode: 500, Body: []byte(`{"error":"internal"}`)}
	err := loopFailureError(&agent.LoopResult{Error: "LLM call failed: 服务繁忙", Err: cause})
	if errors.Is(err, ErrLLMPeriodLimited) {
		t.Fatal("errors.Is(ErrLLMPeriodLimited)=true for a 500, want false")
	}
	var httpErr *llm.HTTPStatusError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != 500 {
		t.Fatalf("the original status did not survive; err=%v", err)
	}
	if code := ErrorCode(err); code != "request_error" {
		t.Fatalf("ErrorCode=%q want request_error", code)
	}
}

// TestLoopFailureErrorWithoutCause covers a loop that reported text but no cause.
func TestLoopFailureErrorWithoutCause(t *testing.T) {
	err := loopFailureError(&agent.LoopResult{Error: "cancelled during LLM retry"})
	if err == nil || err.Error() != "cancelled during LLM retry" {
		t.Fatalf("Error()=%v want the loop text unchanged", err)
	}
	if errors.Is(err, ErrLLMPeriodLimited) {
		t.Fatal("a causeless failure must not be classified as a period limit")
	}
	if err := loopFailureError(nil); err == nil {
		t.Fatal("loopFailureError(nil) returned nil, want a non-nil error")
	}
}

// TestErrorCodeClassifiesPeriodLimit ties the sentinel to the wire code.
func TestErrorCodeClassifiesPeriodLimit(t *testing.T) {
	err := loopFailureError(&agent.LoopResult{Error: "LLM call failed: 周期额度已用尽", Err: periodLimitHTTPError()})
	if code := ErrorCode(err); code != "llm_period_limited" {
		t.Fatalf("ErrorCode=%q want llm_period_limited", code)
	}
}

// TestRunFailurePayloadCarriesCode checks the terminal event carries the stable
// code next to the localized text.
func TestRunFailurePayloadCarriesCode(t *testing.T) {
	err := loopFailureError(&agent.LoopResult{Error: "LLM call failed: 周期额度已用尽", Err: periodLimitHTTPError()})
	payload := runFailurePayload(err.Error(), err)
	if payload["error"] != err.Error() {
		t.Fatalf("error=%v want the reader-facing text", payload["error"])
	}
	if payload["error_code"] != "llm_period_limited" {
		t.Fatalf("error_code=%v want llm_period_limited", payload["error_code"])
	}
	// An unclassified failure still reports the text, without inventing a code.
	plain := runFailurePayload("boom", fmt.Errorf("boom"))
	if plain["error"] != "boom" {
		t.Fatalf("error=%v want boom", plain["error"])
	}
	if code, ok := plain["error_code"]; !ok || code != "request_error" {
		t.Fatalf("error_code=%v (%v) want request_error", code, ok)
	}
}

// TestLoopResultErrorCarriesCause documents the contract the loop must keep:
// text for the reader, cause for the caller. If a future edit drops Err, the
// executor silently loses the classification again.
func TestLoopResultErrorCarriesCause(t *testing.T) {
	cause := periodLimitHTTPError()
	result := agent.LoopResult{Error: "LLM call failed: 周期额度已用尽", Err: cause}
	if result.Err == nil {
		t.Fatal("LoopResult.Err is nil: the cause cannot cross the executor boundary")
	}
	if !strings.Contains(result.Error, "周期") {
		t.Fatalf("Error=%q should stay localized for the reader", result.Error)
	}
}
