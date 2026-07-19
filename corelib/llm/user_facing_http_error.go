package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const userFacingDetailMaxRunes = 300

// Redact common secret-like key=value pairs from user-visible error detail.
var secretLikeFieldRe = regexp.MustCompile(`(?i)(api[_-]?key|authorization|bearer|token|secret|password)\s*[:=]\s*\S+`)

// UserFacingError returns a clear, non-sensitive message for UI display.
// HTTPStatusError keeps Error() body-free (logs must not echo raw upstream bodies);
// this helper re-reads structured Hub/provider fields from Body and prefers
// actionable detail over opaque "HTTP N: body_len=M".
func UserFacingError(err error) string {
	if err == nil {
		return ""
	}
	var httpErr *HTTPStatusError
	if errors.As(err, &httpErr) && httpErr != nil {
		if msg := UserFacingHTTPStatus(httpErr.StatusCode, httpErr.Body); msg != "" {
			return msg
		}
		return httpErr.Error()
	}
	return err.Error()
}

// UserFacingHTTPStatus maps an LLM HTTP status + response body to a user message.
func UserFacingHTTPStatus(statusCode int, body []byte) string {
	return UserFacingHTTPStatusWithProvider(statusCode, body, "")
}

// UserFacingHTTPStatusWithProvider is like UserFacingHTTPStatus but names the
// configured provider in generic (non-Hub-code) messages.
func UserFacingHTTPStatusWithProvider(statusCode int, body []byte, providerName string) string {
	provider := strings.TrimSpace(providerName)
	if provider == "" {
		provider = "模型服务"
	}

	fields := extractLLMErrorFields(body)
	if msg := classifyStructuredCode(fields, statusCode, provider); msg != "" {
		return msg
	}

	code, typ, msg := fields.Code, fields.Type, fields.Message
	bodyLower := strings.ToLower(string(body))
	msgLower := strings.ToLower(msg)

	switch {
	case strings.Contains(msgLower, "no active model service entitlement"):
		return "当前账号没有可用的模型服务权益，请开通模型服务、检查订阅状态，或切换其他模型提供方 (HTTP 403)"
	case code == "insufficient_quota" || typ == "insufficient_quota" ||
		strings.Contains(strings.ToLower(code), "insufficient_quota") ||
		strings.Contains(msgLower, "insufficient_quota"):
		return fmt.Sprintf("%s 账号额度不足，请检查账单、付费计划或切换提供方 (insufficient_quota)", provider)
	case typ == "overloaded_error" || strings.Contains(strings.ToLower(typ), "overloaded") ||
		strings.Contains(bodyLower, "overloaded_error") || strings.Contains(bodyLower, `"overloaded`):
		return fmt.Sprintf("%s 服务器超载，请稍后再试 (overloaded)", provider)
	}

	switch {
	case statusCode == http.StatusBadRequest:
		return withDetail(fmt.Sprintf("%s 请求无效 (HTTP 400)", provider), msg, body)
	case statusCode == http.StatusUnauthorized:
		return withDetail(fmt.Sprintf("%s 认证失败，API Key 无效或已过期，请重新登录 (HTTP 401)", provider), msg, body)
	case statusCode == http.StatusPaymentRequired:
		return withDetail(fmt.Sprintf("%s 支付或额度问题，请检查账户余额与订阅 (HTTP 402)", provider), msg, body)
	case statusCode == http.StatusForbidden:
		return withDetail(fmt.Sprintf("%s 拒绝访问，账号可能被限制、额度不足或无权使用该模型 (HTTP 403)", provider), msg, body)
	case statusCode == http.StatusNotFound:
		return withDetail(fmt.Sprintf("%s 接口或模型不存在 (HTTP 404)", provider), msg, body)
	case statusCode == http.StatusRequestTimeout:
		return withDetail(fmt.Sprintf("%s 请求超时，请稍后重试 (HTTP 408)", provider), msg, body)
	case statusCode == http.StatusTooManyRequests:
		if strings.Contains(bodyLower, "rate_limit") || strings.Contains(strings.ToLower(code), "rate") {
			return withDetail(fmt.Sprintf("%s API 请求频率超限，请稍后再试 (rate_limit)", provider), msg, body)
		}
		return withDetail(fmt.Sprintf("%s API 请求过于频繁，请稍后再试 (HTTP 429)", provider), msg, body)
	case statusCode == http.StatusBadGateway:
		return withDetail("API 网关错误，上游服务不可用，请稍后再试 (HTTP 502)", msg, body)
	case statusCode == http.StatusServiceUnavailable:
		return withDetail("API 服务暂时不可用，请稍后再试 (HTTP 503)", msg, body)
	case statusCode == http.StatusGatewayTimeout:
		return withDetail("API 网关超时，上游响应过慢，请稍后再试 (HTTP 504)", msg, body)
	case statusCode >= 500:
		return withDetail(fmt.Sprintf("API 服务器错误，请稍后再试 (HTTP %d)", statusCode), msg, body)
	case statusCode > 0:
		return withDetail(fmt.Sprintf("%s API 错误 (HTTP %d)", provider, statusCode), msg, body)
	default:
		if msg != "" {
			return summarizeUserFacingMessage(msg)
		}
		return safeBodySnippet(body)
	}
}

// llmErrorFields are the structured bits we safely surface from error bodies.
type llmErrorFields struct {
	Code              string
	Type              string
	Message           string
	RetryAfterSeconds int64
	RetryAfterAt      string
}

func withDetail(base, msg string, body []byte) string {
	detail := summarizeUserFacingMessage(msg)
	if detail == "" {
		detail = safeBodySnippet(body)
	}
	if detail == "" || strings.Contains(base, detail) {
		return base
	}
	return base + "：" + detail
}

// ClassifyHubErrorBody parses Hub LLM denial JSON and returns a user-facing message.
func ClassifyHubErrorBody(body []byte) string {
	return classifyStructuredCode(extractLLMErrorFields(body), 0, "模型服务")
}

// classifyStructuredCode maps Hub/provider machine codes to UI text.
// Returns empty when code is not a known structured denial.
func classifyStructuredCode(f llmErrorFields, statusCode int, provider string) string {
	code := strings.TrimSpace(f.Code)
	if code == "" {
		return ""
	}
	if msg := ClassifyHubServiceError(code, f.Message, f.RetryAfterSeconds, f.RetryAfterAt); msg != "" {
		return msg
	}

	detail := summarizeUserFacingMessage(f.Message)
	switch code {
	case "LLM_MODEL_FORBIDDEN":
		return "当前账号没有可用的模型服务权益，请开通模型服务、检查订阅状态，或切换其他模型提供方 (HTTP 403)"
	case "LLM_MODEL_NOT_FOUND":
		if detail != "" {
			return fmt.Sprintf("模型不可用：%s (HTTP %d)", detail, statusOr(statusCode, http.StatusNotFound))
		}
		return fmt.Sprintf("请求的模型未授权或不存在 (HTTP %d)", statusOr(statusCode, http.StatusNotFound))
	case "LLM_ENDPOINT_USER_RATE_LIMITED":
		if retry := formatHubRetryText(f.RetryAfterSeconds, f.RetryAfterAt); retry != "" {
			return "MaClaw官方请求过快，Hub 排队已超时，请约 " + retry + " 后重试。"
		}
		return "MaClaw官方请求过快，Hub 本地排队已超时，请稍后再试。"
	case "LLM_ENDPOINT_USER_RATE_LIMIT_WAIT_CANCELED":
		return "请求在 Hub 限流排队等待时被取消。"
	case "LLM_PROVIDER_QUEUE_FULL":
		return "MaClaw官方上游队列已满，请稍后再试。"
	case "LLM_PROVIDER_QUEUE_TIMEOUT":
		return "MaClaw官方上游排队等待超时，请稍后再试。"
	case "LLM_ENDPOINT_CONCURRENCY_FULL":
		return "MaClaw官方网关并发已满，请稍后再试。"
	case "LLM_UPSTREAM_AUTH_FAILED", "LLM_OFFICIAL_AUTH_FAILED":
		if detail != "" {
			return detail
		}
		return "上游模型认证失败，请检查服务商 API Key 或联系管理员"
	case "LLM_UPSTREAM_RATE_LIMITED", "LLM_OFFICIAL_RATE_LIMITED":
		if detail != "" {
			return detail
		}
		return "上游模型限流，请稍后再试"
	case "LLM_UPSTREAM_FAILED":
		if detail != "" {
			return detail
		}
		return "上游模型服务暂时不可用，请稍后再试"
	case "LLM_OFFICIAL_UNAVAILABLE":
		if detail != "" {
			return detail
		}
		return "MaClaw 官方服务暂时不可用，请稍后再试"
	case "LLM_OFFICIAL_AUTHORIZATION_DENIED":
		if detail != "" {
			return detail
		}
		return "MaClaw 官方拒绝了当前租户授权，请检查 Hub 授权状态"
	}

	// Other LLM_* machine codes: prefer server message, else code+status.
	if strings.HasPrefix(code, "LLM_") {
		if detail != "" {
			return detail
		}
		if statusCode > 0 {
			return fmt.Sprintf("%s 错误 %s (HTTP %d)", provider, code, statusCode)
		}
		return fmt.Sprintf("%s 错误 %s", provider, code)
	}
	return ""
}

// ClassifyHubServiceError maps Hub LLM service denial codes to clear UI text.
func ClassifyHubServiceError(code, message string, retryAfterSeconds int64, retryAfterAt string) string {
	switch strings.TrimSpace(code) {
	case "LLM_SERVICE_PERIOD_LIMITED":
		if retry := formatHubRetryText(retryAfterSeconds, retryAfterAt); retry != "" {
			return "MaClaw 官方周期限流：当前周期额度已用尽，约 " + retry + " 后恢复。"
		}
		return "MaClaw 官方周期限流：当前周期额度已用尽。"
	case "LLM_SERVICE_CREDITS_EXHAUSTED":
		return "MaClaw 官方额度已用尽：请兑换额度或切换其他模型提供方。"
	case "LLM_SERVICE_GRANT_QUEUED":
		if retry := formatHubRetryText(retryAfterSeconds, retryAfterAt); retry != "" {
			return "MaClaw 官方授权尚未生效：约 " + retry + " 后生效。"
		}
		return "MaClaw 官方授权尚未生效，请稍后再试。"
	case "LLM_SERVICE_GRANT_EXPIRED":
		return "MaClaw 官方授权已过期：请兑换新的授权额度或切换其他模型提供方。"
	case "LLM_SERVICE_CREDITS_REQUIRED":
		return "MaClaw 官方需要有效额度：请兑换额度或切换其他模型提供方。"
	}
	if strings.HasPrefix(code, "LLM_SERVICE_") && strings.TrimSpace(message) != "" {
		return summarizeUserFacingMessage(message)
	}
	return ""
}

func statusOr(status, fallback int) int {
	if status > 0 {
		return status
	}
	return fallback
}

func extractLLMErrorFields(body []byte) llmErrorFields {
	var out llmErrorFields
	if len(body) == 0 {
		return out
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		// SDK-prefixed body: "... {json}"
		if i := strings.IndexByte(string(body), '{'); i > 0 {
			return extractLLMErrorFields(body[i:])
		}
		return out
	}

	out.Code = mapString(payload, "code")
	out.Type = mapString(payload, "type")
	out.Message = firstNonEmpty(
		mapString(payload, "message"),
		mapString(payload, "msg"),
		mapString(payload, "detail"),
		mapString(payload, "error_description"),
		mapString(payload, "error_msg"),
	)
	out.RetryAfterAt = mapString(payload, "retry_after_at")
	out.RetryAfterSeconds = anyToInt64(payload["retry_after_seconds"])

	switch errVal := payload["error"].(type) {
	case map[string]any:
		if out.Code == "" {
			out.Code = mapString(errVal, "code")
		}
		// Anthropic: outer type="error", nested error.type is the real class.
		if nestedType := mapString(errVal, "type"); nestedType != "" && (out.Type == "" || out.Type == "error") {
			out.Type = nestedType
		}
		if out.Message == "" {
			out.Message = mapString(errVal, "message")
		}
	case string:
		if out.Message == "" {
			out.Message = strings.TrimSpace(errVal)
		}
	}
	return out
}

func mapString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return strings.TrimSpace(s)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func anyToInt64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(n), 10, 64)
		return parsed
	default:
		return 0
	}
}

func formatHubRetryText(seconds int64, retryAfterAt string) string {
	if seconds <= 0 && retryAfterAt != "" {
		if retryAt, err := time.Parse(time.RFC3339, retryAfterAt); err == nil {
			seconds = int64((time.Until(retryAt) + time.Second - 1) / time.Second)
		}
	}
	if seconds <= 0 {
		return ""
	}
	if seconds < 60 {
		return fmt.Sprintf("%d 秒", seconds)
	}
	minutes := (seconds + 59) / 60
	if minutes < 60 {
		return fmt.Sprintf("%d 分钟", minutes)
	}
	hours := (minutes + 59) / 60
	if hours < 24 {
		return fmt.Sprintf("%d 小时", hours)
	}
	days := (hours + 23) / 24
	return fmt.Sprintf("%d 天", days)
}

func summarizeUserFacingMessage(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return ""
	}
	msg = secretLikeFieldRe.ReplaceAllString(msg, "[redacted]")
	msg = strings.Join(strings.Fields(msg), " ")
	runes := []rune(msg)
	if len(runes) > userFacingDetailMaxRunes {
		return string(runes[:userFacingDetailMaxRunes]) + "…"
	}
	return msg
}

// safeBodySnippet returns a short, redacted snippet only for JSON-ish error
// payloads when structured field extraction missed a usable message.
func safeBodySnippet(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	s := strings.TrimSpace(string(body))
	lower := strings.ToLower(s)
	if strings.Contains(lower, "<html") || strings.Contains(lower, "<!doctype") ||
		strings.Contains(lower, "<head>") || strings.Contains(lower, "<center>") {
		return ""
	}
	if !strings.HasPrefix(s, "{") && !strings.HasPrefix(s, "[") {
		return ""
	}
	if !strings.Contains(lower, "message") && !strings.Contains(lower, "error") && !strings.Contains(lower, "detail") {
		return ""
	}
	s = secretLikeFieldRe.ReplaceAllString(s, "[redacted]")
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	runes := []rune(s)
	if len(runes) > userFacingDetailMaxRunes {
		return string(runes[:userFacingDetailMaxRunes]) + "…"
	}
	return s
}
