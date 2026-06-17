package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llm"
	corememory "github.com/RapidAI/CodeClaw/corelib/memory"
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

func TestClassify_HubPeriodLimit_NotTransient(t *testing.T) {
	r := NewAdaptiveRetry(nil)
	err := errors.New(`HTTP 429: {"code":"LLM_SERVICE_PERIOD_LIMITED","message":"current period credit limit is exhausted"}`)
	cat := r.Classify("llm_request", err)
	if cat != FailurePeriodLimit {
		t.Fatalf("expected FailurePeriodLimit, got %s", cat)
	}
}

func TestClassify_HubPeriodLimitEnglishText_NotTransient(t *testing.T) {
	r := NewAdaptiveRetry(nil)
	err := errors.New("HTTP 429: MaClaw Official period quota is exhausted; recovers in about 1h")
	cat := r.Classify("llm_request", err)
	if cat != FailurePeriodLimit {
		t.Fatalf("expected FailurePeriodLimit, got %s", cat)
	}
	if isRetryableLLMError(err) {
		t.Fatal("period quota text should not be retryable")
	}
}

func TestDecide_HubPeriodLimit_SkipsRetry(t *testing.T) {
	r := NewAdaptiveRetry(nil)
	d := r.Decide("llm_request", FailurePeriodLimit, 0)
	if d.Action != "skip" {
		t.Fatalf("expected skip for Hub period limit, got %s", d.Action)
	}
	if d.Delay != 0 {
		t.Fatalf("expected no delay for Hub period limit, got %v", d.Delay)
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

func TestClassify_Transient_ZhipuOverloadedError(t *testing.T) {
	r := NewAdaptiveRetry(nil)
	// Matches the exact error format from 智谱 GLM via Anthropic SDK
	err := errors.New(`HTTP 200: body_len=302: POST "https://open.bigmodel.cn/api/anthropic/v1/messages": 200 OK {"type":"error","error":{"type":"overloaded_error","code":"1305","message":"[1305]请负载均衡"}}`)
	cat := r.Classify("llm_request", err)
	if cat != FailureTransient {
		t.Errorf("expected FailureTransient for 智谱 overloaded_error, got %s", cat)
	}
}

func TestClassify_Transient_ZhipuCode1305(t *testing.T) {
	r := NewAdaptiveRetry(nil)
	err := errors.New(`{"code":"1305","message":"服务繁忙"}`)
	cat := r.Classify("llm_request", err)
	if cat != FailureTransient {
		t.Errorf("expected FailureTransient for 智谱 code:1305, got %s", cat)
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
	if d0.Action != "retry" || d0.Delay != 10*time.Second {
		t.Errorf("attempt 0: expected retry/10s, got %s/%v", d0.Action, d0.Delay)
	}

	d1 := r.Decide("llm_request", FailureTransient, 1)
	if d1.Action != "retry" || d1.Delay != 20*time.Second {
		t.Errorf("attempt 1: expected retry/20s, got %s/%v", d1.Action, d1.Delay)
	}

	d2 := r.Decide("llm_request", FailureTransient, 2)
	if d2.Action != "retry" || d2.Delay != 40*time.Second {
		t.Errorf("attempt 2: expected retry/40s, got %s/%v", d2.Action, d2.Delay)
	}

	d3 := r.Decide("llm_request", FailureTransient, 3)
	if d3.Action != "retry" || d3.Delay != 60*time.Second {
		t.Errorf("attempt 3: expected retry/60s (capped), got %s/%v", d3.Action, d3.Delay)
	}

	d4 := r.Decide("llm_request", FailureTransient, 4)
	if d4.Action != "retry" || d4.Delay != 60*time.Second {
		t.Errorf("attempt 4: expected retry/60s (capped), got %s/%v", d4.Action, d4.Delay)
	}

	d5 := r.Decide("llm_request", FailureTransient, 5)
	if d5.Action != "skip" {
		t.Errorf("attempt 5: expected skip, got %s", d5.Action)
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

func TestIsRetryableLLMError_ExcludesHubPeriodLimit(t *testing.T) {
	err := errors.New("MaClaw 官方周期限流：当前周期额度已用尽，约 2 小时 后恢复。")
	if isRetryableLLMError(err) {
		t.Error("expected false for Hub period limit")
	}
	if isTransientServerError(err) {
		t.Error("period limit should not be classified as transient")
	}
}

func TestIsRetryableLLMError_ExcludesHubPeriodLimitCode(t *testing.T) {
	err := errors.New(`HTTP 429: {"code":"LLM_SERVICE_PERIOD_LIMITED","message":"current period credit limit is exhausted"}`)
	if isRetryableLLMError(err) {
		t.Error("expected false for Hub period limit code")
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
		decision := r.Decide("llm_request", FailureNetwork, attempt)
		if decision.Action == RetryActionRetry {
			r.RecordFailure("llm_request", FailureNetwork, decision)
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
		r.RecordFailure("llm_request", FailureTransient, RetryDecision{Action: RetryActionRetry, Attempt: i})
	}

	if !r.IsDisabled("llm_request") {
		t.Error("should be disabled after 5 cumulative failures")
	}

	d := r.Decide("llm_request", FailureTransient, 0)
	if d.Action != "disable" {
		t.Errorf("expected disable, got %s", d.Action)
	}
}

func TestRecordFailurePersistsMemoryTrace(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)

	r := NewAdaptiveRetry(nil)
	r.SetMemoryStore(store)
	r.RecordFailure("write_file", FailureArgs, RetryDecision{
		Action:       RetryActionFix,
		Attempt:      2,
		ErrorContext: "adjust args before retry",
	})

	entries := store.SearchDirectByID("adaptive-retry-write_file-args")
	if len(entries) != 1 {
		t.Fatalf("expected one adaptive retry memory, got %d: %#v", len(entries), entries)
	}
	entry := entries[0]
	if entry.SourceType != string(experienceTraceSourceToolUsage) || entry.SourceURL != "experience://adaptive_retry/write_file/args" {
		t.Fatalf("unexpected adaptive retry metadata: %#v", entry)
	}
	for _, want := range []string{experienceTraceKindToolRecoveryPattern.String(), "adaptive_retry", "tool:write_file", "category:args", "action:fix"} {
		if !hasTag(entry.Tags, want) {
			t.Fatalf("adaptive retry memory missing tag %q: %#v", want, entry.Tags)
		}
	}
	for _, want := range []string{"Failure count: 1", "adjust args before retry", "Safety: retry evidence only"} {
		if !strings.Contains(entry.Content, want) {
			t.Fatalf("adaptive retry memory missing %q: %s", want, entry.Content)
		}
	}

	snapshot := buildExperienceLearningSnapshot(nil, store)
	if snapshot.TraceKindCounts[experienceTraceKindToolMemory.String()] != 1 || snapshot.TraceSourceCounts[string(experienceTraceSourceToolUsage)] != 1 {
		t.Fatalf("adaptive retry memory should surface as tool-memory trace: %#v/%#v", snapshot.TraceKindCounts, snapshot.TraceSourceCounts)
	}
}

func TestRecordFailurePersistsProviderMetadata(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)

	r := NewAdaptiveRetry(nil)
	r.SetMemoryStore(store)
	r.RecordFailure("llm_request", FailureTransient, RetryDecision{
		Action:       RetryActionRetry,
		Attempt:      0,
		ProviderName: " ChatFire ",
		Model:        " gpt-5.1-codex-mini ",
		WireAPI:      " responses ",
	})

	entries := store.SearchDirectByID("adaptive-retry-llm_request-transient")
	if len(entries) != 1 {
		t.Fatalf("expected provider-scoped retry memory, got %#v", entries)
	}
	entry := entries[0]
	for _, want := range []string{"Provider: ChatFire", "Model: gpt-5.1-codex-mini", "Wire API: responses"} {
		if !strings.Contains(entry.Content, want) {
			t.Fatalf("provider retry memory missing %q: %s", want, entry.Content)
		}
	}
	for _, want := range []string{"provider:chatfire", "model:gpt-5-1-codex-mini", "wire_api:responses"} {
		if !hasTag(entry.Tags, want) {
			t.Fatalf("provider retry memory missing tag %q: %#v", want, entry.Tags)
		}
	}

	snapshot := buildExperienceLearningSnapshot(nil, store)
	if len(snapshot.ToolRecoverySummaries) != 1 {
		t.Fatalf("expected one tool recovery summary, got %#v", snapshot.ToolRecoverySummaries)
	}
	summary := snapshot.ToolRecoverySummaries[0]
	if summary.ProviderName != "ChatFire" || summary.Model != "gpt-5.1-codex-mini" || summary.WireAPI != "responses" {
		t.Fatalf("summary should expose provider metadata: %#v", summary)
	}
	app := &App{memoryStore: store}
	result := app.QueryExperienceToolRecoverySummaries(ExperienceToolRecoveryQuery{})
	if result.ProviderCounts["ChatFire"] != 1 || result.ModelCounts["gpt-5.1-codex-mini"] != 1 || result.WireAPICounts["responses"] != 1 {
		t.Fatalf("provider/model/wire_api governance counts missing: %#v", result)
	}
}

func TestRecordFailureUpdatesExistingMemoryTrace(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)

	r := NewAdaptiveRetry(nil)
	r.SetMemoryStore(store)
	r.RecordFailure("llm_request", FailureTransient, RetryDecision{Action: RetryActionRetry, Attempt: 0})
	initial := store.SearchDirectByID("adaptive-retry-llm_request-transient")
	if len(initial) != 1 {
		t.Fatalf("expected initial adaptive retry memory, got %#v", initial)
	}
	firstObserved := adaptiveRetryTestContentField(initial[0].Content, "First observed at")
	if firstObserved == "" {
		t.Fatalf("expected initial first observed timestamp: %s", initial[0].Content)
	}
	r.RecordFailure("llm_request", FailureTransient, RetryDecision{Action: RetryActionRetry, Attempt: 1, Delay: 10 * time.Second})

	entries := store.SearchDirectByID("adaptive-retry-llm_request-transient")
	if len(entries) != 1 {
		t.Fatalf("expected one updated adaptive retry memory, got %d: %#v", len(entries), entries)
	}
	if !strings.Contains(entries[0].Content, "Failure count: 2") || !strings.Contains(entries[0].Content, "Delay: 10s") {
		t.Fatalf("expected updated retry evidence content, got: %s", entries[0].Content)
	}
	if field := adaptiveRetryTestContentField(entries[0].Content, "First observed at"); field != firstObserved {
		t.Fatalf("expected first observed timestamp to be preserved, got %q want %q in %s", field, firstObserved, entries[0].Content)
	}
	if field := adaptiveRetryTestContentField(entries[0].Content, "Last observed at"); field == "" {
		t.Fatalf("expected last observed timestamp in retry evidence: %s", entries[0].Content)
	}
}

func adaptiveRetryTestContentField(content, name string) string {
	prefix := "- " + name + ":"
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

func TestAgentLoopToolFailurePersistsAdaptiveRetryMemory(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)

	retry := NewAdaptiveRetry(nil)
	retry.SetMemoryStore(store)
	handler := &IMMessageHandler{}
	result := handler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		ToolCall: llm.ToolCall{
			ID: "call_bad_args",
			Function: llm.ToolCallFunction{
				Name:      "write_file",
				Arguments: `{"path":`,
			},
		},
		AdaptiveRetry: retry,
	})
	if result.FailureKind != toolFailureArgumentParse {
		t.Fatalf("expected argument parse failure, got kind=%q text=%q", result.FailureKind, result.Text)
	}

	entries := store.SearchDirectByID("adaptive-retry-write_file-args")
	if len(entries) != 1 {
		t.Fatalf("expected one adaptive retry memory for failed tool call, got %d: %#v", len(entries), entries)
	}
	entry := entries[0]
	for _, want := range []string{experienceTraceKindToolRecoveryPattern.String(), "adaptive_retry", "tool:write_file", "category:args", "action:fix"} {
		if !hasTag(entry.Tags, want) {
			t.Fatalf("tool failure memory missing tag %q: %#v", want, entry.Tags)
		}
	}
	for _, want := range []string{"Failure count: 1", "argument_parse", "unexpected end of JSON input", "Safety: retry evidence only"} {
		if !strings.Contains(entry.Content, want) {
			t.Fatalf("tool failure memory missing %q: %s", want, entry.Content)
		}
	}
}

func TestToolRecoveryReviewUsesConsecutiveCategoryStreak(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)

	retry := NewAdaptiveRetry(nil)
	retry.maxFailures = 99
	retry.SetMemoryStore(store)
	retry.RecordFailure("network_tool", FailureTransient, RetryDecision{Action: RetryActionRetry, Attempt: 0})
	retry.RecordFailure("network_tool", FailureTransient, RetryDecision{Action: RetryActionRetry, Attempt: 1})
	retry.RecordFailure("network_tool", FailureArgs, RetryDecision{Action: RetryActionFix, Attempt: 2})
	retry.RecordFailure("network_tool", FailureTransient, RetryDecision{Action: RetryActionRetry, Attempt: 3})
	retry.RecordFailure("network_tool", FailureTransient, RetryDecision{Action: RetryActionRetry, Attempt: 4})

	entries := store.SearchDirectByID("adaptive-retry-network_tool-transient")
	if len(entries) != 1 {
		t.Fatalf("expected transient adaptive retry memory, got %#v", entries)
	}
	if hasTag(entries[0].Tags, experienceReviewRequiredTag) || !hasTag(entries[0].Tags, "failure_count:2") {
		t.Fatalf("interrupted transient failures should not require review yet: %#v", entries[0].Tags)
	}

	retry.RecordFailure("network_tool", FailureTransient, RetryDecision{Action: RetryActionRetry, Attempt: 5})
	entries = store.SearchDirectByID("adaptive-retry-network_tool-transient")
	if len(entries) != 1 {
		t.Fatalf("expected transient adaptive retry memory after retry streak, got %#v", entries)
	}
	if hasTag(entries[0].Tags, experienceReviewRequiredTag) || !hasTag(entries[0].Tags, "failure_count:3") {
		t.Fatalf("retryable transient failures should stay out of review while retrying: %#v", entries[0].Tags)
	}

	retry.RecordFailure("network_tool", FailureTransient, RetryDecision{Action: RetryActionSkip, Attempt: 6})
	entries = store.SearchDirectByID("adaptive-retry-network_tool-transient")
	if len(entries) != 1 {
		t.Fatalf("expected transient adaptive retry memory after streak, got %#v", entries)
	}
	if !hasTag(entries[0].Tags, experienceReviewRequiredTag) || !hasTag(entries[0].Tags, "failure_count:4") {
		t.Fatalf("terminal transient failure should require review: %#v", entries[0].Tags)
	}
}

func TestReviewedToolRecoveryUsesFailureCountCooldown(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)

	retry := NewAdaptiveRetry(nil)
	retry.SetMemoryStore(store)
	for i := 0; i < adaptiveRetryReviewThreshold; i++ {
		retry.RecordFailure("write_file", FailureArgs, RetryDecision{Action: RetryActionFix, Attempt: i})
	}
	app := &App{memoryStore: store}
	if _, err := app.ReviewExperienceTrace("memory:adaptive-retry-write_file-args", ExperienceTraceReviewRequest{Outcome: "approved", Note: "known noisy args", Reviewer: "owner"}); err != nil {
		t.Fatalf("ReviewExperienceTrace: %v", err)
	}

	retry.RecordFailure("write_file", FailureArgs, RetryDecision{Action: RetryActionFix, Attempt: 3})
	entries := store.SearchDirectByID("adaptive-retry-write_file-args")
	if len(entries) != 1 {
		t.Fatalf("expected one adaptive retry memory, got %#v", entries)
	}
	if hasTag(entries[0].Tags, experienceReviewRequiredTag) || !hasTag(entries[0].Tags, experienceReviewResolvedTag) || !hasTag(entries[0].Tags, adaptiveRetryReviewedFailureCountPrefix+"3") || hasTag(entries[0].Tags, "failure_count:1") {
		t.Fatalf("reviewed recovery should stay resolved during cooldown: %#v", entries[0].Tags)
	}
	if !strings.Contains(entries[0].Content, "Experience review record:") || !strings.Contains(entries[0].Content, "known noisy args") {
		t.Fatalf("review audit should be preserved across retry memory updates: %s", entries[0].Content)
	}

	retry.RecordFailure("write_file", FailureArgs, RetryDecision{Action: RetryActionFix, Attempt: 4})
	retry.RecordFailure("write_file", FailureArgs, RetryDecision{Action: RetryActionFix, Attempt: 5})
	entries = store.SearchDirectByID("adaptive-retry-write_file-args")
	if len(entries) != 1 {
		t.Fatalf("expected one adaptive retry memory after cooldown, got %#v", entries)
	}
	if !hasTag(entries[0].Tags, experienceReviewRequiredTag) || !hasTag(entries[0].Tags, "failure_count:6") || hasTag(entries[0].Tags, experienceReviewResolvedTag) {
		t.Fatalf("repeated failures after cooldown should require fresh review: %#v", entries[0].Tags)
	}
}

func TestRepeatedToolFailureRequiresRecoveryReview(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)

	retry := NewAdaptiveRetry(nil)
	retry.SetMemoryStore(store)
	for i := 0; i < adaptiveRetryReviewThreshold; i++ {
		retry.RecordFailure("write_file", FailureArgs, RetryDecision{Action: RetryActionFix, Attempt: i})
	}

	entries := store.SearchDirectByID("adaptive-retry-write_file-args")
	if len(entries) != 1 {
		t.Fatalf("expected one adaptive retry memory, got %d: %#v", len(entries), entries)
	}
	entry := entries[0]
	if !hasTag(entry.Tags, experienceReviewRequiredTag) || !hasTag(entry.Tags, "failure_count:3") {
		t.Fatalf("repeated recovery should require review and expose count tag: %#v", entry.Tags)
	}

	snapshot := buildExperienceLearningSnapshot(nil, store)
	if snapshot.ReviewRequiredTraceCount != 1 || snapshot.TraceKindCounts[experienceTraceKindToolRecoveryPattern.String()] != 1 || snapshot.NextActionKindCounts[experienceGovernanceActionReviewSignal.String()] != 1 {
		t.Fatalf("expected tool recovery review trace: %#v", snapshot)
	}
	found := false
	for _, detail := range snapshot.TraceDetails {
		if detail.Kind == experienceTraceKindToolRecoveryPattern.String() {
			found = true
			if !detail.ReviewRequired || !strings.Contains(detail.ReviewAction, "failure recovery") || !strings.Contains(detail.Impact, "needs review") {
				t.Fatalf("unexpected tool recovery review detail: %#v", detail)
			}
		}
	}
	if !found {
		t.Fatalf("tool recovery review detail missing: %#v", snapshot.TraceDetails)
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
