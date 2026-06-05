package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestGuiSSEIdleTimeoutIsConservative(t *testing.T) {
	if guiSSEIdleTimeout < 4*time.Minute {
		t.Fatalf("guiSSEIdleTimeout = %s, want at least 4m", guiSSEIdleTimeout)
	}
}

func TestClassifyOpenAIHTTPErrorUsesConfiguredProviderName(t *testing.T) {
	body := []byte(`{"error":{"message":"forbidden","type":"forbidden"}}`)
	got := classifyOpenAIHTTPError(403, body, "MaClaw官方")
	if !strings.Contains(got, "MaClaw官方 拒绝访问") {
		t.Fatalf("expected configured provider name in error, got %q", got)
	}
	if strings.Contains(got, "OpenAI 拒绝访问") {
		t.Fatalf("error should not present OpenAI as the provider: %q", got)
	}
}

func TestClassifyResponsesAPIHTTPErrorUsesConfiguredProviderName(t *testing.T) {
	body := []byte(`{"error":{"message":"forbidden","type":"forbidden"}}`)
	got := classifyResponsesAPIHTTPError(403, body, "https://example.test/v1/responses", "gpt-test", "MaClaw官方")
	if !strings.Contains(got, "MaClaw官方 拒绝访问") {
		t.Fatalf("expected configured provider name in responses error, got %q", got)
	}
	if strings.Contains(got, "OpenAI 拒绝访问") || strings.Contains(got, "ChatGPT") {
		t.Fatalf("responses error should not present protocol/product names as provider: %q", got)
	}
}

func TestClassifyHTTPErrorDoesNotEchoUnknownBody(t *testing.T) {
	body := []byte(`Browser: SECRET_CLASSIFY_BODY`)
	openAI := classifyOpenAIHTTPError(418, body, "MaClaw官方")
	if strings.Contains(openAI, "SECRET_CLASSIFY_BODY") || strings.Contains(openAI, "Browser:") {
		t.Fatalf("OpenAI-compatible error echoed body: %q", openAI)
	}
	responses := classifyResponsesAPIHTTPError(418, body, "https://example.test/v1/responses", "gpt-test", "MaClaw官方")
	if strings.Contains(responses, "SECRET_CLASSIFY_BODY") || strings.Contains(responses, "Browser:") {
		t.Fatalf("Responses API error echoed body: %q", responses)
	}
}

func TestClassifyResponsesAPIHTTPErrorReportsHubPeriodLimit(t *testing.T) {
	body := []byte(`{"ok":false,"code":"LLM_SERVICE_PERIOD_LIMITED","message":"current period credit limit is exhausted","retry_after_seconds":90}`)
	got := classifyResponsesAPIHTTPError(403, body, "https://example.test/v1/responses", "gpt-test", "MaClaw\u5b98\u65b9")
	if !strings.Contains(got, "\u5468\u671f\u9650\u6d41") || !strings.Contains(got, "2 \u5206\u949f") {
		t.Fatalf("expected period-limit retry message, got %q", got)
	}
	if strings.Contains(got, "\u62d2\u7edd\u8bbf\u95ee") || strings.Contains(got, "Responses API") {
		t.Fatalf("period limit should not be presented as a generic responses error: %q", got)
	}
}

func TestClassifyResponsesAPIHTTPErrorParsesStringRetryAfterSeconds(t *testing.T) {
	body := []byte(`{"ok":false,"code":"LLM_SERVICE_PERIOD_LIMITED","message":"current period credit limit is exhausted","retry_after_seconds":"90"}`)
	got := classifyResponsesAPIHTTPError(403, body, "https://example.test/v1/responses", "gpt-test", "MaClaw\u5b98\u65b9")
	if !strings.Contains(got, "\u5468\u671f\u9650\u6d41") || !strings.Contains(got, "2 \u5206\u949f") {
		t.Fatalf("expected period-limit retry message from string retry_after_seconds, got %q", got)
	}
}

func TestClassifyResponsesAPIHTTPErrorSurfacesTopLevelHubUpstreamRateLimit(t *testing.T) {
	body := []byte(`{"ok":false,"code":"LLM_UPSTREAM_RATE_LIMITED","message":"官方上游通道限流，请稍后再试"}`)
	got := classifyResponsesAPIHTTPError(429, body, "https://example.test/v1/responses", "gpt-test", "MaClaw\u5b98\u65b9")
	if got != "官方上游通道限流，请稍后再试" {
		t.Fatalf("expected top-level upstream rate-limit message, got %q", got)
	}
	if strings.Contains(got, "订阅额度") || strings.Contains(got, "Responses API") {
		t.Fatalf("upstream rate limit should not be presented as a generic responses error: %q", got)
	}
}

func TestClassifyOpenAICompatibleHTTPErrorUsesConfiguredProviderName(t *testing.T) {
	got, ok := classifyOpenAICompatibleHTTPError(errors.New("[https://example.test/v1/chat/completions] HTTP 403: forbidden"), "MaClaw官方")
	if !ok {
		t.Fatal("expected HTTP error to be classified")
	}
	if !strings.Contains(got, "MaClaw官方 拒绝访问") {
		t.Fatalf("expected configured provider name in normalized error, got %q", got)
	}
	if strings.Contains(got, "OpenAI 拒绝访问") {
		t.Fatalf("normalized error should not present OpenAI as the provider: %q", got)
	}
}

func TestClassifyOpenAICompatibleHTTPErrorUsesStructuredBody(t *testing.T) {
	err := &llm.HTTPStatusError{StatusCode: 429, Body: []byte(`{"ok":false,"code":"LLM_SERVICE_PERIOD_LIMITED","message":"limit","retry_after_seconds":90}`)}
	got, ok := classifyOpenAICompatibleHTTPError(err, "MaClaw\u5b98\u65b9")
	if !ok {
		t.Fatal("expected structured HTTP error to be classified")
	}
	if !strings.Contains(got, "\u5468\u671f\u9650\u6d41") || !strings.Contains(got, "2 \u5206\u949f") {
		t.Fatalf("expected hub period-limit message from structured body, got %q", got)
	}
	if strings.Contains(got, "body_len") || strings.Contains(got, "limit") {
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

func TestClassifyOpenAIHTTPErrorReportsHubPeriodLimit(t *testing.T) {
	body := []byte(`{"ok":false,"code":"LLM_SERVICE_PERIOD_LIMITED","message":"current period credit limit is exhausted","retry_after_seconds":90,"retry_after_at":"2026-05-05T06:00:00Z"}`)
	got := classifyOpenAIHTTPError(403, body, "MaClaw官方")
	if !strings.Contains(got, "周期限流") || !strings.Contains(got, "2 分钟") {
		t.Fatalf("expected period-limit retry message, got %q", got)
	}
	if strings.Contains(got, "拒绝访问") || strings.Contains(got, "LLM 服务") {
		t.Fatalf("period limit should not be presented as a generic service error: %q", got)
	}
}

func TestClassifyOpenAIHTTPErrorReportsHubPeriodLimitFromHTTP429(t *testing.T) {
	body := []byte(`{"ok":false,"code":"LLM_SERVICE_PERIOD_LIMITED","message":"current period credit limit is exhausted","retry_after_seconds":90}`)
	got := classifyOpenAIHTTPError(429, body, "MaClaw官方")
	if !strings.Contains(got, "周期限流") || !strings.Contains(got, "2 分钟") {
		t.Fatalf("expected period-limit retry message from HTTP 429, got %q", got)
	}
	if strings.Contains(got, "请求频率") || strings.Contains(got, "rate_limit") {
		t.Fatalf("period limit should not be presented as generic provider rate limit: %q", got)
	}
}

func TestClassifyOpenAIHTTPErrorSurfacesTopLevelHubUpstreamAuthFailure(t *testing.T) {
	body := []byte(`{"ok":false,"code":"LLM_UPSTREAM_AUTH_FAILED","message":"官方上游服务认证失败，请联系管理员检查服务商配置"}`)
	got := classifyOpenAIHTTPError(502, body, "MaClaw官方")
	if got != "官方上游服务认证失败，请联系管理员检查服务商配置" {
		t.Fatalf("expected top-level upstream auth message, got %q", got)
	}
	if strings.Contains(got, "网关错误") || strings.Contains(got, "HTTP 502") {
		t.Fatalf("upstream auth failure should not be presented as generic gateway error: %q", got)
	}
}

func TestClassifyOpenAIHTTPErrorReportsHubCreditsExhausted(t *testing.T) {
	body := []byte(`{"ok":false,"code":"LLM_SERVICE_CREDITS_EXHAUSTED","message":"selected model grant credits are exhausted"}`)
	got := classifyOpenAIHTTPError(403, body, "MaClaw官方")
	if !strings.Contains(got, "额度已用尽") {
		t.Fatalf("expected credits exhausted message, got %q", got)
	}
}

func TestClassifyOpenAIHTTPErrorReportsHubQueuedGrant(t *testing.T) {
	body := []byte(`{"ok":false,"code":"LLM_SERVICE_GRANT_QUEUED","message":"selected model grant is not active yet","retry_after_seconds":7200}`)
	got := classifyOpenAIHTTPError(403, body, "MaClaw官方")
	if !strings.Contains(got, "授权尚未生效") || !strings.Contains(got, "2 小时") {
		t.Fatalf("expected queued grant retry message, got %q", got)
	}
	if strings.Contains(got, "授权已过期") || strings.Contains(got, "拒绝访问") {
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
	// "<thi" at end of stream is not a real tag — should be emitted
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
	f.Write(`<|FunctionCallBegin|>stuff<|FunctionCallEnd|>继续`)
	f.Flush()
	if got := out.String(); got != "继续" {
		t.Errorf("expected %q, got %q", "继续", got)
	}
}

func TestFuncCallFilter_SplitAcrossChunks(t *testing.T) {
	var out strings.Builder
	f := newFuncCallFilter(func(s string) { out.WriteString(s) })
	f.Write("好的<|FunctionCall")
	f.Write("Begin|>中间<|FunctionCallEnd|>后面")
	f.Flush()
	if got := out.String(); got != "好的后面" {
		t.Errorf("expected %q, got %q", "好的后面", got)
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
	input := "前<|FunctionCallBegin|>中<|FunctionCallEnd|>后"
	var out strings.Builder
	f := newFuncCallFilter(func(s string) { out.WriteString(s) })
	for _, c := range input {
		f.Write(string(c))
	}
	f.Flush()
	if got := out.String(); got != "前后" {
		t.Errorf("expected %q, got %q", "前后", got)
	}
}

func TestStripFunctionCalls(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"hello world", "hello world"},
		{`好的<|FunctionCallBegin|>[{"name":"x"}]<|FunctionCallEnd|>`, "好的"},
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
	// Simulate the real chain: thinkFilter -> funcCallFilter -> output
	var out strings.Builder
	fcf := newFuncCallFilter(func(s string) { out.WriteString(s) })
	tf := newThinkFilter(func(s string) { fcf.Write(s) })
	tf.Write("<think>reasoning</think>好的<|FunctionCallBegin|>call<|FunctionCallEnd|>结果")
	tf.Flush()
	fcf.Flush()
	if got := out.String(); got != "好的结果" {
		t.Errorf("expected %q, got %q", "好的结果", got)
	}
}
