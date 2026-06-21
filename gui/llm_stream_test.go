package main

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestGuiSSEIdleTimeoutIsConservative(t *testing.T) {
	if guiSSEIdleTimeout < 4*time.Minute {
		t.Fatalf("guiSSEIdleTimeout = %s, want at least 4m", guiSSEIdleTimeout)
	}
}

func TestClassifyOpenAIHTTPErrorUsesConfiguredProviderName(t *testing.T) {
	body := []byte(`{"error":{"message":"forbidden","type":"forbidden"}}`)
	got := classifyOpenAIHTTPError(403, body, "MaClawOfficial")
	if !strings.Contains(got, "MaClawOfficial") || !strings.Contains(got, "HTTP 403") {
		t.Fatalf("expected configured provider name in error, got %q", got)
	}
	if strings.Contains(got, "OpenAI") {
		t.Fatalf("error should not present OpenAI as the provider: %q", got)
	}
}

func TestClassifyOpenAIHTTPErrorSurfacesBadRequestMessage(t *testing.T) {
	body := []byte(`{"error":{"message":"messages[0].role: unknown variant developer","type":"invalid_request_error"}}`)
	got := classifyOpenAIHTTPError(400, body, "Qwen")
	if !strings.Contains(got, "Qwen API invalid request (HTTP 400)") {
		t.Fatalf("expected provider-specific HTTP 400 message, got %q", got)
	}
	if !strings.Contains(got, "unknown variant developer") {
		t.Fatalf("expected upstream error.message to be surfaced, got %q", got)
	}
}

func TestClassifyOpenAIHTTPErrorSurfacesTopLevelBadRequestMessage(t *testing.T) {
	body := []byte(`{"message":"messages[1].role must not be system"}`)
	got := classifyOpenAIHTTPError(400, body, "Qwen")
	if !strings.Contains(got, "Qwen API invalid request (HTTP 400)") {
		t.Fatalf("expected provider-specific HTTP 400 message, got %q", got)
	}
	if !strings.Contains(got, "messages[1].role must not be system") {
		t.Fatalf("expected top-level message to be surfaced, got %q", got)
	}
}

func TestSummarizeProviderHTTPErrorMessageTruncatesRunes(t *testing.T) {
	msg := strings.Repeat("请求参数错误", 80)
	got := summarizeProviderHTTPErrorMessage(msg)
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("expected ellipsis, got %q", got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("truncated message is not valid UTF-8: %q", got)
	}
	if runeCount := len([]rune(strings.TrimSuffix(got, "..."))); runeCount != 300 {
		t.Fatalf("truncated rune count = %d, want 300", runeCount)
	}
}

func TestClassifyResponsesAPIHTTPErrorUsesConfiguredProviderName(t *testing.T) {
	body := []byte(`{"error":{"message":"forbidden","type":"forbidden"}}`)
	got := classifyResponsesAPIHTTPError(403, body, "https://example.test/v1/responses", "gpt-test", "MaClawOfficial")
	if !strings.Contains(got, "MaClawOfficial") || !strings.Contains(got, "HTTP 403") {
		t.Fatalf("expected configured provider name in responses error, got %q", got)
	}
	if strings.Contains(got, "OpenAI") || strings.Contains(got, "ChatGPT") {
		t.Fatalf("responses error should not present protocol/product names as provider: %q", got)
	}
}

func TestClassifyHTTPErrorDoesNotEchoUnknownBody(t *testing.T) {
	body := []byte(`Browser: SECRET_CLASSIFY_BODY`)
	openAI := classifyOpenAIHTTPError(418, body, "MaClawOfficial")
	if strings.Contains(openAI, "SECRET_CLASSIFY_BODY") || strings.Contains(openAI, "Browser:") {
		t.Fatalf("OpenAI-compatible error echoed body: %q", openAI)
	}
	responses := classifyResponsesAPIHTTPError(418, body, "https://example.test/v1/responses", "gpt-test", "MaClawOfficial")
	if strings.Contains(responses, "SECRET_CLASSIFY_BODY") || strings.Contains(responses, "Browser:") {
		t.Fatalf("Responses API error echoed body: %q", responses)
	}
}

func TestClassifyResponsesAPIHTTPErrorReportsHubPeriodLimit(t *testing.T) {
	body := []byte(`{"ok":false,"code":"LLM_SERVICE_PERIOD_LIMITED","message":"current period credit limit is exhausted","retry_after_seconds":90}`)
	got := classifyResponsesAPIHTTPError(403, body, "https://example.test/v1/responses", "gpt-test", "MaClawOfficial")
	if !strings.Contains(got, "quota is rate-limited") || !strings.Contains(got, "2 分钟") {
		t.Fatalf("expected period-limit retry message, got %q", got)
	}
	if strings.Contains(got, "Responses API") || strings.Contains(got, "forbidden") {
		t.Fatalf("period limit should not be presented as a generic responses error: %q", got)
	}
}

func TestClassifyResponsesAPIHTTPErrorParsesStringRetryAfterSeconds(t *testing.T) {
	body := []byte(`{"ok":false,"code":"LLM_SERVICE_PERIOD_LIMITED","message":"current period credit limit is exhausted","retry_after_seconds":"90"}`)
	got := classifyResponsesAPIHTTPError(403, body, "https://example.test/v1/responses", "gpt-test", "MaClawOfficial")
	if !strings.Contains(got, "quota is rate-limited") || !strings.Contains(got, "2 分钟") {
		t.Fatalf("expected period-limit retry message from string retry_after_seconds, got %q", got)
	}
}

func TestClassifyResponsesAPIHTTPErrorSurfacesTopLevelHubUpstreamRateLimit(t *testing.T) {
	body := []byte(`{"ok":false,"code":"LLM_UPSTREAM_RATE_LIMITED","message":"upstream limited"}`)
	got := classifyResponsesAPIHTTPError(429, body, "https://example.test/v1/responses", "gpt-test", "MaClawOfficial")
	if got != "upstream limited" {
		t.Fatalf("expected top-level upstream rate-limit message, got %q", got)
	}
	if strings.Contains(got, "Responses API") {
		t.Fatalf("upstream rate limit should not be presented as a generic responses error: %q", got)
	}
}

func TestClassifyOpenAICompatibleHTTPErrorUsesConfiguredProviderName(t *testing.T) {
	got, ok := classifyOpenAICompatibleHTTPError(errors.New("[https://example.test/v1/chat/completions] HTTP 403: forbidden"), "MaClawOfficial")
	if !ok {
		t.Fatal("expected HTTP error to be classified")
	}
	if !strings.Contains(got, "MaClawOfficial") || !strings.Contains(got, "HTTP 403") {
		t.Fatalf("expected configured provider name in normalized error, got %q", got)
	}
	if strings.Contains(got, "OpenAI") {
		t.Fatalf("normalized error should not present OpenAI as the provider: %q", got)
	}
}

func TestClassifyOpenAICompatibleHTTPErrorUsesStructuredBody(t *testing.T) {
	err := &llm.HTTPStatusError{StatusCode: 429, Body: []byte(`{"ok":false,"code":"LLM_SERVICE_PERIOD_LIMITED","message":"limit","retry_after_seconds":90}`)}
	got, ok := classifyOpenAICompatibleHTTPError(err, "MaClawOfficial")
	if !ok {
		t.Fatal("expected structured HTTP error to be classified")
	}
	if !strings.Contains(got, "quota is rate-limited") || !strings.Contains(got, "2 分钟") {
		t.Fatalf("expected hub period-limit message from structured body, got %q", got)
	}
	if strings.Contains(got, "body_len") || strings.Contains(got, `"message":"limit"`) {
		t.Fatalf("structured body should inform classification without being echoed: %q", got)
	}
}

func TestClassifyOpenAICompatibleHTTPErrorParsesStatusWithoutColon(t *testing.T) {
	got, ok := classifyOpenAICompatibleHTTPError(errors.New("HTTP 404 (endpoint=https://example.test/v1/chat/completions, body_len=0)"), "Custom1")
	if !ok {
		t.Fatal("expected HTTP status without colon to be classified")
	}
	if !strings.Contains(got, "Custom1") || !strings.Contains(got, "HTTP 404") {
		t.Fatalf("unexpected classification: %q", got)
	}
}

func TestIsLLMHTTPStatusErrorUsesStructuredStatus(t *testing.T) {
	err := &llm.HTTPStatusError{StatusCode: 500, Body: []byte(`{"error":"server"}`)}
	if !isLLMHTTPStatusError(err, 500) {
		t.Fatal("expected structured HTTP 500 to match")
	}
	if isLLMHTTPStatusError(err, 400) {
		t.Fatal("structured HTTP 500 should not match HTTP 400")
	}
}

func TestClassifyOpenAICompatibleHTTPError_ZhipuOverloadedHTTP200(t *testing.T) {
	// Simulates the exact error format from 智谱 GLM overloaded_error via Anthropic SDK.
	// The body is wrapped with non-JSON prefix, so json.Unmarshal fails, but the
	// default branch should detect "overloaded" in the body text.
	err := errors.New(`HTTP 200: body_len=302: POST "https://open.bigmodel.cn/api/anthropic/v1/messages": 200 OK {"type":"error","error":{"type":"overloaded_error","code":"1305","message":"[1305]请负载均衡"}}`)
	got, ok := classifyOpenAICompatibleHTTPError(err, "智谱编程")
	if !ok {
		t.Fatal("expected HTTP 200 error to be classified")
	}
	if !strings.Contains(got, "超载") && !strings.Contains(got, "overloaded") {
		t.Fatalf("expected overloaded message, got %q", got)
	}
	if strings.Contains(got, "body_len") || strings.Contains(got, `"type":"error"`) {
		t.Fatalf("raw JSON should not leak into user-facing message: %q", got)
	}
}

func TestClassifyOpenAIHTTPErrorReportsHubPeriodLimit(t *testing.T) {
	body := []byte(`{"ok":false,"code":"LLM_SERVICE_PERIOD_LIMITED","message":"current period credit limit is exhausted","retry_after_seconds":90,"retry_after_at":"2026-05-05T06:00:00Z"}`)
	got := classifyOpenAIHTTPError(403, body, "MaClawOfficial")
	if !strings.Contains(got, "quota is rate-limited") || !strings.Contains(got, "2 分钟") {
		t.Fatalf("expected period-limit retry message, got %q", got)
	}
	if strings.Contains(got, "forbidden") || strings.Contains(got, "rate_limit") {
		t.Fatalf("period limit should not be presented as a generic service error: %q", got)
	}
}

func TestClassifyOpenAIHTTPErrorReportsHubPeriodLimitFromHTTP429(t *testing.T) {
	body := []byte(`{"ok":false,"code":"LLM_SERVICE_PERIOD_LIMITED","message":"current period credit limit is exhausted","retry_after_seconds":90}`)
	got := classifyOpenAIHTTPError(429, body, "MaClawOfficial")
	if !strings.Contains(got, "quota is rate-limited") || !strings.Contains(got, "2 分钟") {
		t.Fatalf("expected period-limit retry message from HTTP 429, got %q", got)
	}
	if strings.Contains(got, "rate_limit") {
		t.Fatalf("period limit should not be presented as generic provider rate limit: %q", got)
	}
}

func TestClassifyOpenAIHTTPErrorSurfacesTopLevelHubUpstreamAuthFailure(t *testing.T) {
	body := []byte(`{"ok":false,"code":"LLM_UPSTREAM_AUTH_FAILED","message":"upstream auth failed"}`)
	got := classifyOpenAIHTTPError(502, body, "MaClawOfficial")
	if got != "upstream auth failed" {
		t.Fatalf("expected top-level upstream auth message, got %q", got)
	}
	if strings.Contains(got, "HTTP 502") {
		t.Fatalf("upstream auth failure should not be presented as generic gateway error: %q", got)
	}
}

func TestClassifyOpenAIHTTPErrorSurfacesOfficialUnavailable(t *testing.T) {
	body := []byte(`{"ok":false,"code":"LLM_OFFICIAL_UNAVAILABLE","message":"MaClaw official service is temporarily unavailable"}`)
	got := classifyOpenAIHTTPError(503, body, "MaClawOfficial")
	if got != "MaClaw official service is temporarily unavailable" {
		t.Fatalf("expected official unavailable message, got %q", got)
	}
	if strings.Contains(got, "HTTP 503") {
		t.Fatalf("official unavailable should not be presented as generic 503: %q", got)
	}
}

func TestClassifyOpenAIHTTPErrorAppendsHubDiagnostics(t *testing.T) {
	body := []byte(`{"ok":false,"code":"LLM_OFFICIAL_UNAVAILABLE","message":"MaClaw official service is temporarily unavailable","request_id":"llm_123","failure_stage":"upstream_provider","provider_id":"maclaw-official","upstream_host":"api.deepseek.com","upstream_status":504,"hub_status":504,"elapsed_ms":120001}`)
	got := classifyOpenAIHTTPError(504, body, "MaClawOfficial")
	for _, want := range []string{
		"MaClaw official service is temporarily unavailable",
		"request_id=llm_123",
		"failure_stage=upstream_provider",
		"provider_id=maclaw-official",
		"upstream_host=api.deepseek.com",
		"upstream_status=504",
		"hub_status=504",
		"elapsed_ms=120001",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in %q", want, got)
		}
	}
}

func TestClassifyOpenAIHTTPErrorCleansHubDiagnostics(t *testing.T) {
	body := []byte(`{"ok":false,"code":"LLM_OFFICIAL_UNAVAILABLE","message":"MaClaw official service is temporarily unavailable","request_id":"llm_123\ntrace","failure_stage":"upstream_provider","provider_id":"maclaw_official","upstream_host":"hubcenter.example.com\napi.deepseek.com","upstream_status":504,"hub_status":504}`)
	got := classifyOpenAIHTTPError(504, body, "MaClawOfficial")
	if !strings.Contains(got, "request_id=llm_123 trace") {
		t.Fatalf("expected compacted request_id diagnostics, got %q", got)
	}
	if !strings.Contains(got, "upstream_host=hubcenter.example.com api.deepseek.com") {
		t.Fatalf("expected compacted upstream_host diagnostics, got %q", got)
	}
	if strings.Contains(got, "\n") {
		t.Fatalf("diagnostics should not contain raw newlines: %q", got)
	}
}

func TestClassifyOpenAIHTTPErrorReportsHubCreditsExhausted(t *testing.T) {
	body := []byte(`{"ok":false,"code":"LLM_SERVICE_CREDITS_EXHAUSTED","message":"selected model grant credits are exhausted"}`)
	got := classifyOpenAIHTTPError(403, body, "MaClawOfficial")
	if !strings.Contains(got, "credits are exhausted") {
		t.Fatalf("expected credits exhausted message, got %q", got)
	}
}

func TestClassifyOpenAIHTTPErrorReportsHubQueuedGrant(t *testing.T) {
	body := []byte(`{"ok":false,"code":"LLM_SERVICE_GRANT_QUEUED","message":"selected model grant is not active yet","retry_after_seconds":7200}`)
	got := classifyOpenAIHTTPError(403, body, "MaClawOfficial")
	if !strings.Contains(got, "grant is not active yet") || !strings.Contains(got, "2 小时") {
		t.Fatalf("expected queued grant retry message, got %q", got)
	}
	if strings.Contains(got, "expired") || strings.Contains(got, "forbidden") {
		t.Fatalf("queued grant should not be presented as expired or forbidden: %q", got)
	}
}

func TestThinkFilter_BasicBlock(t *testing.T) {
	var out strings.Builder
	tf := newThinkFilter(func(s string) { out.WriteString(s) })
	tf.Write("<think>reasoning here</think>Hello world")
	tf.Flush()
	if got := out.String(); got != "Hello world" {
		t.Errorf("expected %q, got %q", "Hello world", got)
	}
}

func TestThinkFilter_SplitOpenTag(t *testing.T) {
	var out strings.Builder
	tf := newThinkFilter(func(s string) { out.WriteString(s) })
	tf.Write("Hi <thi")
	tf.Write("nk>secret</think> there")
	tf.Flush()
	if got := out.String(); got != "Hi there" {
		t.Errorf("expected %q, got %q", "Hi there", got)
	}
}

func TestThinkFilter_SplitCloseTag(t *testing.T) {
	var out strings.Builder
	tf := newThinkFilter(func(s string) { out.WriteString(s) })
	tf.Write("<think>secret</thi")
	tf.Write("nk>visible")
	tf.Flush()
	if got := out.String(); got != "visible" {
		t.Errorf("expected %q, got %q", "visible", got)
	}
}

func TestThinkFilter_NoThinkTags(t *testing.T) {
	var out strings.Builder
	tf := newThinkFilter(func(s string) { out.WriteString(s) })
	tf.Write("just normal text")
	tf.Flush()
	if got := out.String(); got != "just normal text" {
		t.Errorf("expected %q, got %q", "just normal text", got)
	}
}

func TestThinkFilter_MultipleBlocks(t *testing.T) {
	var out strings.Builder
	tf := newThinkFilter(func(s string) { out.WriteString(s) })
	tf.Write("<think>a</think>X<think>b</think>Y")
	tf.Flush()
	if got := out.String(); got != "XY" {
		t.Errorf("expected %q, got %q", "XY", got)
	}
}

func TestThinkFilter_CharByChar(t *testing.T) {
	// Simulate extreme fragmentation: one char per delta
	input := "<think>hidden</think>visible"
	var out strings.Builder
	tf := newThinkFilter(func(s string) { out.WriteString(s) })
	for _, c := range input {
		tf.Write(string(c))
	}
	tf.Flush()
	if got := out.String(); got != "visible" {
		t.Errorf("expected %q, got %q", "visible", got)
	}
}

func TestThinkFilter_TrailingWhitespace(t *testing.T) {
	var out strings.Builder
	tf := newThinkFilter(func(s string) { out.WriteString(s) })
	tf.Write("<think>x</think>\n\nHello")
	tf.Flush()
	if got := out.String(); got != "Hello" {
		t.Errorf("expected %q, got %q", "Hello", got)
	}
}

func TestThinkFilter_PartialOpenNeverCompleted(t *testing.T) {
	// "<thi" at end of stream is not a real tag 閳?should be emitted
	var out strings.Builder
	tf := newThinkFilter(func(s string) { out.WriteString(s) })
	tf.Write("hello <thi")
	tf.Flush()
	if got := out.String(); got != "hello <thi" {
		t.Errorf("expected %q, got %q", "hello <thi", got)
	}
}

func TestThinkFilter_LongThinkBlock(t *testing.T) {
	// Simulate a long reasoning block delivered in many small chunks.
	// The filter should not accumulate unbounded buffer.
	var out strings.Builder
	tf := newThinkFilter(func(s string) { out.WriteString(s) })
	tf.Write("<think>")
	for i := 0; i < 1000; i++ {
		tf.Write("reasoning reasoning reasoning ")
	}
	tf.Write("</think>Answer")
	tf.Flush()
	if got := out.String(); got != "Answer" {
		t.Errorf("expected %q, got %q", "Answer", got)
	}
}

func TestThinkFilter_CloseTagSplitAcrossManyChunks(t *testing.T) {
	var out strings.Builder
	tf := newThinkFilter(func(s string) { out.WriteString(s) })
	tf.Write("<think>secret")
	tf.Write("</")
	tf.Write("th")
	tf.Write("ink>")
	tf.Write("visible")
	tf.Flush()
	if got := out.String(); got != "visible" {
		t.Errorf("expected %q, got %q", "visible", got)
	}
}

func TestThinkFilter_FalseAlarmPartialTag(t *testing.T) {
	// Text ending with "<" that is NOT a think tag
	var out strings.Builder
	tf := newThinkFilter(func(s string) { out.WriteString(s) })
	tf.Write("a < b")
	tf.Flush()
	if got := out.String(); got != "a < b" {
		t.Errorf("expected %q, got %q", "a < b", got)
	}
}

// ---------------------------------------------------------------------------
// ThinkFilter reasoning callback tests
// ---------------------------------------------------------------------------

func TestThinkFilter_ReasoningCallback_BasicBlock(t *testing.T) {
	var content, reasoning strings.Builder
	tf := newThinkFilterWithReasoning(
		func(s string) { content.WriteString(s) },
		func(s string) { reasoning.WriteString(s) },
	)
	tf.Write("<think>I need to analyze this</think>The answer is 42")
	tf.Flush()
	if got := content.String(); got != "The answer is 42" {
		t.Errorf("content: expected %q, got %q", "The answer is 42", got)
	}
	if got := reasoning.String(); got != "I need to analyze this" {
		t.Errorf("reasoning: expected %q, got %q", "I need to analyze this", got)
	}
}

func TestThinkFilter_ReasoningCallback_ChunkedDelivery(t *testing.T) {
	var content, reasoning strings.Builder
	tf := newThinkFilterWithReasoning(
		func(s string) { content.WriteString(s) },
		func(s string) { reasoning.WriteString(s) },
	)
	tf.Write("<think>step 1, ")
	tf.Write("step 2, ")
	tf.Write("step 3</think>")
	tf.Write("result")
	tf.Flush()
	if got := content.String(); got != "result" {
		t.Errorf("content: expected %q, got %q", "result", got)
	}
	if got := reasoning.String(); got != "step 1, step 2, step 3" {
		t.Errorf("reasoning: expected %q, got %q", "step 1, step 2, step 3", got)
	}
}

func TestThinkFilter_ReasoningCallback_SplitCloseTag(t *testing.T) {
	var content, reasoning strings.Builder
	tf := newThinkFilterWithReasoning(
		func(s string) { content.WriteString(s) },
		func(s string) { reasoning.WriteString(s) },
	)
	tf.Write("<think>thinking</thi")
	tf.Write("nk>visible")
	tf.Flush()
	if got := content.String(); got != "visible" {
		t.Errorf("content: expected %q, got %q", "visible", got)
	}
	if got := reasoning.String(); got != "thinking" {
		t.Errorf("reasoning: expected %q, got %q", "thinking", got)
	}
}

func TestThinkFilter_ReasoningCallback_MultipleBlocks(t *testing.T) {
	var content, reasoning strings.Builder
	tf := newThinkFilterWithReasoning(
		func(s string) { content.WriteString(s) },
		func(s string) { reasoning.WriteString(s) },
	)
	tf.Write("<think>first</think>A<think>second</think>B")
	tf.Flush()
	if got := content.String(); got != "AB" {
		t.Errorf("content: expected %q, got %q", "AB", got)
	}
	if got := reasoning.String(); got != "firstsecond" {
		t.Errorf("reasoning: expected %q, got %q", "firstsecond", got)
	}
}

func TestThinkFilter_ReasoningCallback_UnclosedThinkAtStreamEnd(t *testing.T) {
	var content, reasoning strings.Builder
	tf := newThinkFilterWithReasoning(
		func(s string) { content.WriteString(s) },
		func(s string) { reasoning.WriteString(s) },
	)
	tf.Write("<think>partial thinking never closed")
	tf.Flush()
	if got := content.String(); got != "" {
		t.Errorf("content: expected empty, got %q", got)
	}
	if got := reasoning.String(); got != "partial thinking never closed" {
		t.Errorf("reasoning: expected %q, got %q", "partial thinking never closed", got)
	}
}

func TestThinkFilter_ReasoningCallback_CharByChar(t *testing.T) {
	input := "<think>hidden</think>visible"
	var content, reasoning strings.Builder
	tf := newThinkFilterWithReasoning(
		func(s string) { content.WriteString(s) },
		func(s string) { reasoning.WriteString(s) },
	)
	for _, c := range input {
		tf.Write(string(c))
	}
	tf.Flush()
	if got := content.String(); got != "visible" {
		t.Errorf("content: expected %q, got %q", "visible", got)
	}
	if got := reasoning.String(); got != "hidden" {
		t.Errorf("reasoning: expected %q, got %q", "hidden", got)
	}
}

// ---------------------------------------------------------------------------
// funcCallFilter tests
// ---------------------------------------------------------------------------

func TestFuncCallFilter_FullBlock(t *testing.T) {
	var out strings.Builder
	f := newFuncCallFilter(func(s string) { out.WriteString(s) })
	f.Write(`hello<|FunctionCallBegin|>[{"name":"set_nickname"}]<|FunctionCallEnd|>`)
	f.Flush()
	if got := out.String(); got != "hello" {
		t.Errorf("expected %q, got %q", "hello", got)
	}
}

func TestFuncCallFilter_TextAfterBlock(t *testing.T) {
	var out strings.Builder
	f := newFuncCallFilter(func(s string) { out.WriteString(s) })
	f.Write(`<|FunctionCallBegin|>stuff<|FunctionCallEnd|>after`)
	f.Flush()
	if got := out.String(); got != "after" {
		t.Errorf("expected %q, got %q", "after", got)
	}
}

func TestFuncCallFilter_SplitAcrossChunks(t *testing.T) {
	var out strings.Builder
	f := newFuncCallFilter(func(s string) { out.WriteString(s) })
	f.Write("before<|FunctionCall")
	f.Write("Begin|>hidden<|FunctionCallEnd|>after")
	f.Flush()
	if got := out.String(); got != "beforeafter" {
		t.Errorf("expected %q, got %q", "beforeafter", got)
	}
}

func TestFuncCallFilter_NoMarkers(t *testing.T) {
	var out strings.Builder
	f := newFuncCallFilter(func(s string) { out.WriteString(s) })
	f.Write("normal text")
	f.Flush()
	if got := out.String(); got != "normal text" {
		t.Errorf("expected %q, got %q", "normal text", got)
	}
}

func TestToolCallFilter_CodexBlockSplitAcrossChunks(t *testing.T) {
	var out strings.Builder
	f := newToolCallFilter(func(s string) { out.WriteString(s) })
	f.Write("before<turn: tool")
	f.Write(`_call><invoke name="bash"><parameter name="command">dir</parameter></invoke></turn>after`)
	f.Flush()
	if got := out.String(); got != "beforeafter" {
		t.Errorf("expected %q, got %q", "beforeafter", got)
	}
}

func TestToolCallFilter_PlainToolCallSuppressesJSON(t *testing.T) {
	var out strings.Builder
	f := newToolCallFilter(func(s string) { out.WriteString(s) })
	f.Write("步骤1：查看磁盘\nTOOL")
	f.Write(`_CALL
{
  "function": "ssh_execute_command",
  "args": {"host":"example.com","password":"<redacted>","command":"df -h"}
}`)
	f.Flush()
	if got := out.String(); got != "步骤1：查看磁盘\n" {
		t.Errorf("expected visible prefix only, got %q", got)
	}
}

func TestToolCallFilter_BareJSONToolCallsSuppressesJSON(t *testing.T) {
	var out strings.Builder
	f := newToolCallFilter(func(s string) { out.WriteString(s) })
	f.Write(`{"tool_calls":[{"function":{"name":"bash",`)
	f.Write(`"arguments":"{\"command\":\"dir\"}"}}]}`)
	f.Flush()
	if got := out.String(); got != "" {
		t.Errorf("expected bare JSON tool call to be suppressed, got %q", got)
	}
}

func TestToolCallFilter_BareJSONLegacyFunctionCallSuppressesJSON(t *testing.T) {
	var out strings.Builder
	f := newToolCallFilter(func(s string) { out.WriteString(s) })
	f.Write(`{"function_call":{"name":"bash",`)
	f.Write(`"arguments":"{\"command\":\"dir\"}"}}`)
	f.Flush()
	if got := out.String(); got != "" {
		t.Errorf("expected bare JSON function_call to be suppressed, got %q", got)
	}
}

func TestToolCallFilter_AngleArrayToolCallDoesNotLeakPrefix(t *testing.T) {
	var out strings.Builder
	f := newToolCallFilter(func(s string) { out.WriteString(s) })
	f.Write("visible\n<tool")
	f.Write(`_call[]>
{"name":"write_file","arguments":{"file_path":"e:\\CRM\\docs\\technical-design.md","content":"hello"}}`)
	f.Flush()
	if got := out.String(); got != "visible\n" {
		t.Errorf("expected visible prefix only, got %q", got)
	}
}

func TestToolCallFilter_AngleArrayToolCallCharByCharDoesNotLeakBracket(t *testing.T) {
	input := `<tool_call[]>
{"name":"write_file","arguments":{"file_path":"e:\\CRM\\docs\\technical-design.md","content":"hello"}}`
	var out strings.Builder
	f := newToolCallFilter(func(s string) { out.WriteString(s) })
	for _, c := range input {
		f.Write(string(c))
	}
	f.Flush()
	if got := out.String(); got != "" {
		t.Errorf("expected no leaked tool text, got %q", got)
	}
}

func TestToolCallFilter_AllowsOrdinaryToolCallsExplanation(t *testing.T) {
	var out strings.Builder
	f := newToolCallFilter(func(s string) { out.WriteString(s) })
	f.Write("OpenAI tool_calls should stay structured.")
	f.Flush()
	if got := out.String(); got != "OpenAI tool_calls should stay structured." {
		t.Errorf("expected ordinary tool_calls explanation to pass through, got %q", got)
	}
}

func TestToolCallFilter_BareJSONAllowsOrdinaryJSON(t *testing.T) {
	var out strings.Builder
	f := newToolCallFilter(func(s string) { out.WriteString(s) })
	f.Write(`{"name":"Alice","city":"Beijing"}`)
	if got := out.String(); got != "" {
		t.Errorf("expected ordinary JSON to buffer until flush, got %q", got)
	}
	f.Flush()
	if got := out.String(); got != `{"name":"Alice","city":"Beijing"}` {
		t.Errorf("expected ordinary JSON to pass through, got %q", got)
	}
}

func TestThinkFilter_DetailsBlockSplitAcrossChunks(t *testing.T) {
	var out strings.Builder
	tf := newThinkFilter(func(s string) { out.WriteString(s) })
	tf.Write("before<det")
	tf.Write("ails><summary>思考过程</summary>hidden</details>after")
	tf.Flush()
	if got := out.String(); got != "beforeafter" {
		t.Errorf("expected details block suppressed, got %q", got)
	}
}

func TestThinkFilter_DetailsPartialFlushesPlainText(t *testing.T) {
	var out strings.Builder
	tf := newThinkFilter(func(s string) { out.WriteString(s) })
	tf.Write("hello <det")
	tf.Flush()
	if got := out.String(); got != "hello <det" {
		t.Errorf("expected partial non-tag text to flush, got %q", got)
	}
}

func TestFuncCallFilter_MultipleBlocks(t *testing.T) {
	var out strings.Builder
	f := newFuncCallFilter(func(s string) { out.WriteString(s) })
	f.Write("a<|FunctionCallBegin|>x<|FunctionCallEnd|>b<|FunctionCallBegin|>y<|FunctionCallEnd|>c")
	f.Flush()
	if got := out.String(); got != "abc" {
		t.Errorf("expected %q, got %q", "abc", got)
	}
}

func TestFuncCallFilter_CharByChar(t *testing.T) {
	input := "before<|FunctionCallBegin|>hidden<|FunctionCallEnd|>after"
	var out strings.Builder
	f := newFuncCallFilter(func(s string) { out.WriteString(s) })
	for _, c := range input {
		f.Write(string(c))
	}
	f.Flush()
	if got := out.String(); got != "beforeafter" {
		t.Errorf("expected %q, got %q", "beforeafter", got)
	}
}

func TestStripFunctionCalls(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"hello world", "hello world"},
		{`before<|FunctionCallBegin|>[{"name":"x"}]<|FunctionCallEnd|>`, "before"},
		{`a<|FunctionCallBegin|>x<|FunctionCallEnd|>b<|FunctionCallBegin|>y<|FunctionCallEnd|>c`, "abc"},
	}
	for _, tt := range tests {
		got := stripFunctionCalls(tt.input)
		if got != tt.want {
			t.Errorf("stripFunctionCalls(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestThinkAndFuncCallFilterChained(t *testing.T) {
	var out strings.Builder
	fcf := newFuncCallFilter(func(s string) { out.WriteString(s) })
	tf := newThinkFilter(func(s string) { fcf.Write(s) })
	tf.Write("<think>reasoning</think>before<|FunctionCallBegin|>call<|FunctionCallEnd|>after")
	tf.Flush()
	fcf.Flush()
	if got := out.String(); got != "beforeafter" {
		t.Errorf("expected %q, got %q", "beforeafter", got)
	}
}
