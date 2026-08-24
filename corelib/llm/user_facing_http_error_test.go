package llm

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestUserFacingErrorCreditsExhaustedFromHTTPStatusError(t *testing.T) {
	body := []byte(`{"ok":false,"code":"LLM_SERVICE_CREDITS_EXHAUSTED","message":"selected model grant credits are exhausted"}`)
	err := &HTTPStatusError{StatusCode: http.StatusForbidden, Body: body}

	// Error() stays body-free for logs.
	if got := err.Error(); !strings.Contains(got, "body_len=") || strings.Contains(got, "CREDITS") {
		t.Fatalf("Error() should keep body private, got %q", got)
	}

	got := UserFacingError(err)
	if !strings.Contains(got, "额度已用尽") {
		t.Fatalf("UserFacingError = %q, want credits exhausted Chinese message", got)
	}
	if strings.Contains(got, "body_len") {
		t.Fatalf("user-facing message must not show body_len: %q", got)
	}
}

func TestUserFacingErrorCreditsRequired(t *testing.T) {
	body := []byte(`{"ok":false,"code":"LLM_SERVICE_CREDITS_REQUIRED","message":"selected model requires an active grant with remaining credits"}`)
	got := UserFacingHTTPStatus(http.StatusForbidden, body)
	if !strings.Contains(got, "有效额度") {
		t.Fatalf("got %q, want credits required message", got)
	}
}

func TestUserFacingErrorPeriodLimitedWithRetry(t *testing.T) {
	body := []byte(`{"ok":false,"code":"LLM_SERVICE_PERIOD_LIMITED","message":"limit","retry_after_seconds":90}`)
	got := UserFacingHTTPStatus(http.StatusForbidden, body)
	if !strings.Contains(got, "周期限流") || !strings.Contains(got, "2 分钟") {
		t.Fatalf("got %q, want period limit with retry", got)
	}
}

func TestUserFacingErrorNestedOpenAIEnvelopeHubCode(t *testing.T) {
	body := []byte(`{"error":{"code":"LLM_SERVICE_CREDITS_EXHAUSTED","message":"selected model grant credits are exhausted","type":"invalid_request_error"}}`)
	got := UserFacingHTTPStatus(http.StatusForbidden, body)
	if !strings.Contains(got, "额度已用尽") {
		t.Fatalf("got %q, want credits exhausted from nested envelope", got)
	}
}

func TestUserFacingErrorFallsBackToErrorString(t *testing.T) {
	err := errors.New("connection reset")
	if got := UserFacingError(err); got != "connection reset" {
		t.Fatalf("got %q", got)
	}
}

func TestUserFacingErrorOfficialOwnerUnreachable(t *testing.T) {
	err := errors.New("maclaw official: tenant bound to node hc-3 but owner https://hubs2.maclaw.top is unreachable")
	got := UserFacingError(err)
	if got != "官方模型当前绑定的节点不可达，请稍后重试" {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, "hubs2") {
		t.Fatalf("must not leak owner URL: %q", got)
	}
	wrapped := fmt.Errorf("%w: tenant bound to node hc-3 but owner https://hubs2.maclaw.top is unreachable", ErrOfficialOwnerUnreachable)
	if got := UserFacingError(wrapped); got != "官方模型当前绑定的节点不可达，请稍后重试" {
		t.Fatalf("sentinel wrap got %q", got)
	}
}

func TestUserFacingHTTPStatusTenantBoundCode(t *testing.T) {
	body := []byte(`{"code":"TENANT_BOUND_TO_NODE","error":{"message":"tenant bound to node hc-3, please redirect","code":"TENANT_BOUND_TO_NODE"}}`)
	got := UserFacingHTTPStatus(http.StatusConflict, body)
	if got != "官方模型正在切换服务节点，请稍后重试" {
		t.Fatalf("got %q", got)
	}
}

func TestUserFacingErrorHTTPStatusWithoutKnownBody(t *testing.T) {
	err := &HTTPStatusError{StatusCode: http.StatusForbidden, Body: []byte(`plain forbidden`)}
	got := UserFacingError(err)
	if !strings.Contains(got, "HTTP 403") {
		t.Fatalf("got %q, want generic 403 guidance", got)
	}
	// Plain non-JSON bodies are not echoed (may be arbitrary/sensitive).
	if strings.Contains(got, "plain forbidden") {
		t.Fatalf("should not echo unstructured body: %q", got)
	}
	if strings.Contains(got, "body_len") {
		t.Fatalf("should not fall back to body_len when status is classifiable: %q", got)
	}
}

func TestUserFacingErrorExtractsOpenCodeModelErrorFromString(t *testing.T) {
	err := errors.New(`POST "https://opencode.ai/zen/v1/chat/completions": 401 Unauthorized {"type":"ModelError","message":"Free promotion has ended for DeepSeek V4 Flash Free."}`)
	got := UserFacingError(err)
	if !strings.Contains(got, "免费活动已结束") {
		t.Fatalf("got %q, want ended-promotion guidance from error string", got)
	}
}

func TestExtractJSONObjectFromTextIgnoresTrailingText(t *testing.T) {
	got := extractJSONObjectFromText(`POST url: 401 Unauthorized {"type":"ModelError","message":"gone"} leftover`)
	if string(got) != `{"type":"ModelError","message":"gone"}` {
		t.Fatalf("got %s", got)
	}
}

func TestUserFacingErrorWithProviderNamesOpenCode(t *testing.T) {
	err := errors.New(`POST "https://opencode.ai/zen/v1/chat/completions": 401 Unauthorized {"type":"ModelError","message":"Free promotion has ended for DeepSeek V4 Flash Free."}`)
	got := UserFacingErrorWithProvider(err, "OpenCode")
	if !strings.Contains(got, "OpenCode") || !strings.Contains(got, "免费活动已结束") {
		t.Fatalf("got %q, want named OpenCode ended-promotion guidance", got)
	}
}

func TestUserFacingHTTPStatusOpenCodeFreePromotionEnded(t *testing.T) {
	body := []byte(`{"type":"ModelError","message":"Free promotion has ended for DeepSeek V4 Flash Free. You can continue using the model by subscribing to OpenCode Go - https://opencode.ai/go"}`)
	got := UserFacingHTTPStatusWithProvider(http.StatusUnauthorized, body, "OpenCode")
	if !strings.Contains(got, "免费活动已结束") {
		t.Fatalf("got %q, want ended-promotion guidance", got)
	}
	if strings.Contains(got, "认证失败") || strings.Contains(got, "重新登录") {
		t.Fatalf("ended free promotion must not look like an invalid API key: %q", got)
	}
}

func TestUserFacingHTTPStatusModelErrorIsNotAuthFailure(t *testing.T) {
	body := []byte(`{"type":"ModelError","message":"model deepseek-v4-flash-free is not available for this account"}`)
	got := UserFacingHTTPStatusWithProvider(http.StatusUnauthorized, body, "OpenCode")
	if !strings.Contains(got, "当前模型不可用") {
		t.Fatalf("got %q, want model-unavailable guidance", got)
	}
	if strings.Contains(got, "认证失败") {
		t.Fatalf("ModelError must not look like an invalid API key: %q", got)
	}
	if !strings.Contains(got, "not available") {
		t.Fatalf("got %q, want original model message", got)
	}
}

func TestUserFacingHTTPStatusIncludesOpenAI400Detail(t *testing.T) {
	body := []byte(`{"error":{"message":"max_tokens is too large: 200000","type":"invalid_request_error","code":"invalid_request_error"}}`)
	got := UserFacingHTTPStatusWithProvider(http.StatusBadRequest, body, "DeepSeek")
	if !strings.Contains(got, "HTTP 400") || !strings.Contains(got, "max_tokens is too large") {
		t.Fatalf("got %q, want 400 with server message", got)
	}
	if strings.Contains(got, "body_len") {
		t.Fatalf("must not show body_len: %q", got)
	}
}

func TestUserFacingHTTPStatusIncludesUpstreamFailureDetail(t *testing.T) {
	body := []byte(`{"ok":false,"code":"LLM_UPSTREAM_FAILED","message":"upstream LLM provider \"deepseek\" is temporarily unavailable"}`)
	got := UserFacingHTTPStatus(http.StatusBadGateway, body)
	if !strings.Contains(got, "temporarily unavailable") {
		t.Fatalf("got %q, want upstream message", got)
	}
}

func TestUserFacingHTTPStatusIncludes500Detail(t *testing.T) {
	body := []byte(`{"error":{"message":"internal engine crash id=abc","type":"server_error"}}`)
	got := UserFacingHTTPStatus(http.StatusInternalServerError, body)
	if !strings.Contains(got, "HTTP 500") || !strings.Contains(got, "internal engine crash") {
		t.Fatalf("got %q, want 500 with detail", got)
	}
}

func TestUserFacingHTTPStatusAnthropicOverloaded(t *testing.T) {
	body := []byte(`{"type":"error","error":{"type":"overloaded_error","message":"[1305]请负载均衡"}}`)
	got := UserFacingHTTPStatusWithProvider(http.StatusOK, body, "智谱")
	// status may be non-2xx in practice; still classify overloaded from body.
	if !strings.Contains(got, "超载") && !strings.Contains(got, "overloaded") {
		// HTTP 200 with error body falls into status>0 branch after overloaded check
		t.Fatalf("got %q, want overloaded classification", got)
	}
}

func TestUserFacingHTTPStatusEndpointRateLimited(t *testing.T) {
	body := []byte(`{"ok":false,"code":"LLM_ENDPOINT_USER_RATE_LIMITED","message":"user request rate exceeded","retry_after_seconds":2}`)
	got := UserFacingHTTPStatus(http.StatusTooManyRequests, body)
	if !strings.Contains(got, "请求过快") && !strings.Contains(got, "排队") {
		t.Fatalf("got %q, want hub user rate limit message", got)
	}
}

func TestUserFacingHTTPStatusRedactsSecretsInDetail(t *testing.T) {
	body := []byte(`{"error":{"message":"bad key api_key=sk-secret-123456"}}`)
	got := UserFacingHTTPStatus(http.StatusUnauthorized, body)
	if strings.Contains(got, "sk-secret") {
		t.Fatalf("secret leaked in %q", got)
	}
	if !strings.Contains(got, "[redacted]") && !strings.Contains(got, "HTTP 401") {
		t.Fatalf("got %q, want redacted or status guidance", got)
	}
}

func TestStreamHTTPStatusErrorIgnoresZeroStatus(t *testing.T) {
	if err := streamHTTPStatusError(0, []byte(`{"error":"x"}`)); err != nil {
		t.Fatalf("status 0 must not invent HTTPStatusError, got %v", err)
	}
	if err := newHTTPStatusError(http.StatusOK, []byte(`ok`)); err != nil {
		t.Fatalf("HTTP 200 must not invent HTTPStatusError, got %v", err)
	}
	err := newHTTPStatusError(http.StatusForbidden, []byte(`{"code":"LLM_SERVICE_CREDITS_EXHAUSTED"}`))
	var httpErr *HTTPStatusError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusForbidden {
		t.Fatalf("want HTTPStatusError 403, got %v", err)
	}
	if UserFacingError(err) == "" || strings.Contains(UserFacingError(err), "body_len") {
		t.Fatalf("user-facing credits message missing: %q", UserFacingError(err))
	}
}
