package main

import (
	"errors"
	"testing"
	"time"
)

// --- Classify: transient server errors ---

func TestClassify_Transient_429(t *testing.T) {
	r := NewAdaptiveRetry(nil)
	err := errors.New("OpenAI API 请求过于频繁 (HTTP 429) [url=https://codegen.qianxin-inc.cn/api/v1]")
	cat := r.Classify("llm_request", err)
	if cat != FailureTransient {
		t.Errorf("expected FailureTransient, got %s", cat)
	}
}

func TestClassify_Transient_TooManyRequests(t *testing.T) {
	r := NewAdaptiveRetry(nil)
	err := errors.New("Error: Too Many Requests")
	cat := r.Classify("llm_request", err)
	if cat != FailureTransient {
		t.Errorf("expected FailureTransient, got %s", cat)
	}
}

func TestClassify_Transient_QuotaExceeded(t *testing.T) {
	r := NewAdaptiveRetry(nil)
	err := errors.New("quota exceeded for model gpt-4")
	cat := r.Classify("llm_request", err)
	if cat != FailureTransient {
		t.Errorf("expected FailureTransient, got %s", cat)
	}
}

func TestClassify_Transient_Chinese(t *testing.T) {
	r := NewAdaptiveRetry(nil)
	err := errors.New("调用频率超限，请稍后再试")
	cat := r.Classify("llm_request", err)
	if cat != FailureTransient {
		t.Errorf("expected FailureTransient, got %s", cat)
	}
}

func TestClassify_Transient_PrecedesNetwork(t *testing.T) {
	// 429 errors may contain network-related terms like "http".
	// Transient must be classified before network.
	r := NewAdaptiveRetry(nil)
	err := errors.New("HTTP 429 Too Many Requests: rate limit exceeded, service unavailable")
	cat := r.Classify("llm_request", err)
	if cat != FailureTransient {
		t.Errorf("expected FailureTransient (not FailureNetwork), got %s", cat)
	}
}

func TestClassify_Transient_502(t *testing.T) {
	r := NewAdaptiveRetry(nil)
	err := errors.New("API 网关错误，上游服务不可用，请稍后再试 (HTTP 502)")
	cat := r.Classify("llm_request", err)
	if cat != FailureTransient {
		t.Errorf("expected FailureTransient, got %s", cat)
	}
}

func TestClassify_Transient_503(t *testing.T) {
	r := NewAdaptiveRetry(nil)
	err := errors.New("API 服务暂时不可用，请稍后再试 (HTTP 503)")
	cat := r.Classify("llm_request", err)
	if cat != FailureTransient {
		t.Errorf("expected FailureTransient, got %s", cat)
	}
}

func TestClassify_Transient_504(t *testing.T) {
	r := NewAdaptiveRetry(nil)
	err := errors.New("API 网关超时，上游服务响应过慢，请稍后再试 (HTTP 504)")
	cat := r.Classify("llm_request", err)
	if cat != FailureTransient {
		t.Errorf("expected FailureTransient, got %s", cat)
	}
}

func TestClassify_Transient_500(t *testing.T) {
	r := NewAdaptiveRetry(nil)
	err := errors.New("API 服务端错误，请稍后再试 (HTTP 500)")
	cat := r.Classify("llm_request", err)
	if cat != FailureTransient {
		t.Errorf("expected FailureTransient, got %s", cat)
	}
}

func TestClassify_Transient_408(t *testing.T) {
	r := NewAdaptiveRetry(nil)
	err := errors.New("OpenAI API 错误 (HTTP 408): Request Timeout")
	cat := r.Classify("llm_request", err)
	if cat != FailureTransient {
		t.Errorf("expected FailureTransient, got %s", cat)
	}
}

func TestClassify_Transient_Overloaded(t *testing.T) {
	r := NewAdaptiveRetry(nil)
	err := errors.New("The server is overloaded, please try again later")
	cat := r.Classify("llm_request", err)
	if cat != FailureTransient {
		t.Errorf("expected FailureTransient, got %s", cat)
	}
}

func TestClassify_Transient_ZhipuCode1234(t *testing.T) {
	r := NewAdaptiveRetry(nil)
	err := errors.New(`{"code":"1234","message":"网络错误"}`)
	cat := r.Classify("llm_request", err)
	if cat != FailureTransient {
		t.Errorf("expected FailureTransient for 智谱 code:1234, got %s", cat)
	}
}

// --- Classify: network errors (client-side) ---

func TestClassify_Network_ConnectionRefused(t *testing.T) {
	r := NewAdaptiveRetry(nil)
	err := errors.New("dial tcp 127.0.0.1:8080: connection refused")
	cat := r.Classify("llm_request", err)
	if cat != FailureNetwork {
		t.Errorf("expected FailureNetwork, got %s", cat)
	}
}

func TestClassify_Network_Timeout(t *testing.T) {
	r := NewAdaptiveRetry(nil)
	err := errors.New("context deadline exceeded (Client.Timeout)")
	cat := r.Classify("llm_request", err)
	if cat != FailureNetwork {
		t.Errorf("expected FailureNetwork, got %s", cat)
	}
}

// --- Decide: transient exponential backoff ---

func TestDecide_Transient_ExponentialBackoff(t *testing.T) {
	r := NewAdaptiveRetry(nil)

	d0 := r.Decide("llm_request", FailureTransient, 0)
	if d0.Action != "retry" || d0.Delay != 5*time.Second {
		t.Errorf("attempt 0: expected retry/5s, got %s/%v", d0.Action, d0.Delay)
	}

	d1 := r.Decide("llm_request", FailureTransient, 1)
	if d1.Action != "retry" || d1.Delay != 10*time.Second {
		t.Errorf("attempt 1: expected retry/10s, got %s/%v", d1.Action, d1.Delay)
	}

	d2 := r.Decide("llm_request", FailureTransient, 2)
	if d2.Action != "retry" || d2.Delay != 20*time.Second {
		t.Errorf("attempt 2: expected retry/20s, got %s/%v", d2.Action, d2.Delay)
	}

	d3 := r.Decide("llm_request", FailureTransient, 3)
	if d3.Action != "skip" {
		t.Errorf("attempt 3: expected skip, got %s", d3.Action)
	}
}

func TestDecide_Network_StillWorks(t *testing.T) {
	r := NewAdaptiveRetry(nil)

	d0 := r.Decide("llm_request", FailureNetwork, 0)
	if d0.Action != "retry" || d0.Delay != 1*time.Second {
		t.Errorf("expected retry/1s, got %s/%v", d0.Action, d0.Delay)
	}

	d3 := r.Decide("llm_request", FailureNetwork, 3)
	if d3.Action != "skip" {
		t.Errorf("expected skip at attempt 3, got %s", d3.Action)
	}
}

// --- isRetryableLLMError / isTransientServerError ---

func TestIsRetryableLLMError_Includes429(t *testing.T) {
	err := errors.New("OpenAI API 请求过于频繁 (HTTP 429)")
	if !isRetryableLLMError(err) {
		t.Error("expected true for 429")
	}
}

func TestIsRetryableLLMError_Includes502(t *testing.T) {
	err := errors.New("API 网关错误 (HTTP 502)")
	if !isRetryableLLMError(err) {
		t.Error("expected true for 502")
	}
}

func TestIsRetryableLLMError_Includes408(t *testing.T) {
	err := errors.New("OpenAI API 错误 (HTTP 408): Request Timeout")
	if !isRetryableLLMError(err) {
		t.Error("expected true for 408")
	}
}

func TestIsRetryableLLMError_StillMatchesTimeout(t *testing.T) {
	err := errors.New("context deadline exceeded")
	if !isRetryableLLMError(err) {
		t.Error("expected true for timeout")
	}
}

func TestIsRetryableLLMError_NotFor401(t *testing.T) {
	err := errors.New("OpenAI 认证失败 (HTTP 401)")
	if isRetryableLLMError(err) {
		t.Error("expected false for 401 — not retryable")
	}
}

func TestIsRetryableLLMError_NotFor400(t *testing.T) {
	err := errors.New("Bad Request (HTTP 400)")
	if isRetryableLLMError(err) {
		t.Error("expected false for 400 — not retryable")
	}
}

func TestIsTransientServerError_Overloaded(t *testing.T) {
	err := errors.New("The server is overloaded, please try again later")
	if !isTransientServerError(err) {
		t.Error("expected true for overloaded")
	}
}

func TestIsTransientServerError_ChineseServiceBusy(t *testing.T) {
	err := errors.New("服务繁忙，请稍后重试")
	if !isTransientServerError(err) {
		t.Error("expected true for 服务繁忙")
	}
}

// --- RecordFailure counting ---

func TestRecordFailure_NotCalledOnSkip(t *testing.T) {
	r := NewAdaptiveRetry(nil)

	for attempt := 0; attempt < 4; attempt++ {
		decision := r.Decide("llm_request", FailureTransient, attempt)
		if decision.Action == "retry" {
			r.RecordFailure("llm_request", FailureTransient, decision)
		}
	}

	if r.failureCounts["llm_request"] != 3 {
		t.Errorf("expected 3 failures recorded, got %d", r.failureCounts["llm_request"])
	}
	if r.IsDisabled("llm_request") {
		t.Error("should not be disabled after 3 failures (threshold is 5)")
	}
}

func TestRecordFailure_DisablesAfterThreshold(t *testing.T) {
	r := NewAdaptiveRetry(nil)

	for i := 0; i < 5; i++ {
		r.RecordFailure("llm_request", FailureTransient, RetryDecision{Action: "retry", Attempt: i})
	}

	if !r.IsDisabled("llm_request") {
		t.Error("should be disabled after 5 cumulative failures")
	}

	d := r.Decide("llm_request", FailureTransient, 0)
	if d.Action != "disable" {
		t.Errorf("expected disable, got %s", d.Action)
	}
}

// --- Backward compatibility: FailureRateLimit alias ---

func TestFailureRateLimit_IsAliasForTransient(t *testing.T) {
	if FailureRateLimit != FailureTransient {
		t.Errorf("FailureRateLimit should be alias for FailureTransient")
	}
}

// --- Boundary: permanent errors must NOT be transient ---

func TestIsTransientServerError_NotForInsufficientQuota(t *testing.T) {
	// insufficient_quota means the account has no money — permanent, not transient.
	err := errors.New("OpenAI 账号额度不足，请检查账单和付费计划 (insufficient_quota)")
	if isTransientServerError(err) {
		t.Error("insufficient_quota should NOT be transient — it's a permanent billing issue")
	}
}

func TestClassify_InsufficientQuota_NotTransient(t *testing.T) {
	r := NewAdaptiveRetry(nil)
	err := errors.New("OpenAI 账号额度不足，请检查账单和付费计划 (insufficient_quota)")
	cat := r.Classify("llm_request", err)
	if cat == FailureTransient {
		t.Error("insufficient_quota should not be classified as transient")
	}
}

// --- Keyword case sensitivity ---

func TestClassify_Network_ClientTimeout_CaseInsensitive(t *testing.T) {
	// networkKeywords uses ToLower matching, so "Client.Timeout" in the
	// error message must match the lowercase keyword "client.timeout".
	r := NewAdaptiveRetry(nil)
	err := errors.New("net/http: request canceled (Client.Timeout exceeded)")
	cat := r.Classify("llm_request", err)
	if cat != FailureNetwork {
		t.Errorf("expected FailureNetwork for Client.Timeout, got %s", cat)
	}
}
