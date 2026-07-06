package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

type tokenStreamFilter struct {
	writeFn func(string)
	flushFn func()
}

var guiFuncCallBlock = regexp.MustCompile(`(?s)<\|FunctionCallBegin\|>.*?<\|FunctionCallEnd\|>\s*`)
var openAICompatibleHTTPStatusRe = regexp.MustCompile(`HTTP\s+(\d+)`)

const (
	guiThinkOpen          = "<think>"
	guiThinkClose         = "</think>"
	guiFuncCallOpen       = "<|FunctionCallBegin|>"
	guiFuncCallClose      = "<|FunctionCallEnd|>"
	guiToolCallOpen       = "<tool_call>"
	guiToolCallClose      = "</tool_call>"
	guiCodexToolCallOpen  = "<turn: tool_call>"
	guiCodexToolCallClose = "</turn>"
	// Keep the stream watchdog conservative: remote LLM gateways can stay
	// silent for minutes during long reasoning/tool-planning phases while the
	// upstream request is still healthy. A short 90s idle window caused false
	// "upstream is slow" failures for otherwise usable providers.
	guiSSEIdleTimeout        = 4 * time.Minute
	guiMaxToolArgumentsBytes = 180 * 1024
)

// filterTruncatedToolCalls checks for tool calls with invalid or incomplete
// JSON arguments when the model hit its output token limit
// (finish_reason="length"). Two cases are detected:
//
//  1. JSON parse failure  - arguments string is not valid JSON (truncated mid-token)
//  2. Required field missing  - JSON parses OK but a required field is absent,
//     indicating the model ran out of output tokens before generating all fields.
//     This happens when a large field (e.g. write_file content) consumes the
//     entire output budget, leaving no room for subsequent fields like path.
//
// Truncated tool calls are removed from msg.ToolCalls. The truncated tool
// names are returned so the caller (agent loop) can inject a recovery system
// message and continue the loop. msg.Content is NOT modified  - the hint
// belongs in a system message, not in the assistant's own text.
//
// Returns the (possibly modified) finishReason, the list of truncated
// tool names (nil if none were truncated), and a map of tool name to raw
// (incomplete) argument strings for truncated calls.
func filterTruncatedToolCalls(msg *llm.Message, finishReason string) (string, []string, map[string]string) {
	if len(msg.ToolCalls) == 0 {
		return finishReason, nil, nil
	}

	// Primary signal: finish_reason="length" means the model hit max_output_tokens.
	isLengthTruncated := normalizeLLMFinishReason(finishReason) == llmFinishReasonLength

	var validCalls []llm.ToolCall
	var truncatedNames []string
	var truncatedArgs map[string]string
	for _, tc := range msg.ToolCalls {
		args := strings.TrimSpace(tc.Function.Arguments)
		if args == "" {
			if isLengthTruncated {
				truncatedNames = append(truncatedNames, tc.Function.Name)
			} else {
				validCalls = append(validCalls, tc)
			}
			continue
		}
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(args), &parsed); err != nil {
			// Case 1: JSON parse failure  - the arguments are not valid JSON.
			// This is always a truncation or generation error regardless of
			// finish_reason. The tool handler would fail on json.Unmarshal
			// anyway, so removing the call and hinting is strictly better
			// than letting it through to produce a confusing error message.
			truncatedNames = append(truncatedNames, tc.Function.Name)
			if truncatedArgs == nil {
				truncatedArgs = make(map[string]string)
			}
			truncatedArgs[tc.Function.Name] = args
			log.Printf("[LLM Stream] truncated tool call (invalid JSON): %s args=%d bytes finish_reason=%s", tc.Function.Name, len(args), finishReason)
		} else if missingField := detectTruncatedRequiredField(tc.Function.Name, parsed); missingField != "" {
			// Case 2: JSON valid but required field missing.
			// With finish_reason="length" this is definitely truncation.
			// Without "length" but with large args (>4000 bytes), some API
			// proxies (e.g. 智谱 GLM) return "stop" instead of "length"
			// when hitting max_output_tokens  - still treat as truncation.
			if isLengthTruncated || len(args) > 4000 {
				truncatedNames = append(truncatedNames, tc.Function.Name)
				if truncatedArgs == nil {
					truncatedArgs = make(map[string]string)
				}
				truncatedArgs[tc.Function.Name] = args
				log.Printf("[LLM Stream] truncated tool call (missing required field %q): %s args=%d bytes finish_reason=%s",
					missingField, tc.Function.Name, len(args), finishReason)
			} else {
				// Small args + not length-truncated: genuine model error, let
				// the tool handler report the missing parameter normally.
				validCalls = append(validCalls, tc)
			}
		} else {
			validCalls = append(validCalls, tc)
		}
	}
	if len(truncatedNames) == 0 {
		return finishReason, nil, nil
	}
	msg.ToolCalls = validCalls
	// Do NOT append hint to msg.Content  - the agent loop will inject it
	// as a separate system message. Keeping msg.Content clean ensures the
	// assistant message in conversation history only contains the LLM's
	// own text, not system-injected recovery instructions.
	return finishReason, truncatedNames, truncatedArgs
}

// truncatedRequiredFields maps tool names to their required fields for
// truncation detection. These are the tool calls most likely to become unsafe
// when output stops after emitting only part of the argument object.
var truncatedRequiredFields = map[string][]string{
	"write_file": {"path", "content"},
	"edit_file":  {"path", "old_string", "new_string"},
	"edit_lines": {"path", "operation", "start_line"},
	"bash":       {"command"},
}

// detectTruncatedRequiredField checks if a parsed tool call argument map is
// missing a required field, which indicates the output was truncated by the
// model's max_output_tokens limit.
//
// This is NOT a general parameter validation  - it specifically detects the
// pattern where a large field (e.g. content) consumed the entire output
// budget, preventing subsequent required fields from being generated.
func detectTruncatedRequiredField(toolName string, parsed map[string]interface{}) string {
	fields, ok := truncatedRequiredFields[strings.TrimSpace(toolName)]
	if !ok {
		return ""
	}
	for _, f := range fields {
		value, exists := parsed[f]
		if !exists {
			return f
		}
		if s, ok := value.(string); ok && strings.TrimSpace(s) == "" {
			return f
		}
	}
	return ""
}

func llmProviderDisplayName(providerName string) string {
	providerName = strings.TrimSpace(providerName)
	if providerName != "" {
		return providerName
	}
	return "模型服务"
}

func classifyHubErrorBody(body []byte) string {
	var hubErr map[string]any
	_ = json.Unmarshal(body, &hubErr)
	hubCode, _ := hubErr["code"].(string)
	if hubCode == "" {
		return ""
	}
	hubMessage, _ := hubErr["message"].(string)
	retryAfterAt, _ := hubErr["retry_after_at"].(string)
	retryAfterSeconds := int64(0)
	switch v := hubErr["retry_after_seconds"].(type) {
	case float64:
		retryAfterSeconds = int64(v)
	case int64:
		retryAfterSeconds = v
	case int:
		retryAfterSeconds = int64(v)
	case string:
		if parsed, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			retryAfterSeconds = parsed
		}
	}
	if friendly := classifyHubLLMServiceError(hubCode, hubMessage, retryAfterSeconds, retryAfterAt); friendly != "" {
		return appendHubErrorDiagnostics(friendly, hubErr)
	}
	if (strings.HasPrefix(hubCode, "LLM_UPSTREAM_") || strings.HasPrefix(hubCode, "LLM_OFFICIAL_")) && strings.TrimSpace(hubMessage) != "" {
		return appendHubErrorDiagnostics(hubMessage, hubErr)
	}
	return ""
}

func appendHubErrorDiagnostics(message string, hubErr map[string]any) string {
	parts := make([]string, 0, 8)
	for _, key := range []string{"request_id", "failure_stage", "provider_id", "upstream_host"} {
		if value := hubErrorDiagnosticString(hubErr[key]); value != "" {
			parts = append(parts, key+"="+value)
		}
	}
	for _, key := range []string{"upstream_status", "hub_status", "elapsed_ms"} {
		if value := hubErrorNumberString(hubErr[key]); value != "" {
			parts = append(parts, key+"="+value)
		}
	}
	if len(parts) == 0 {
		return message
	}
	return message + " [" + strings.Join(parts, " ") + "]"
}

func hubErrorDiagnosticString(value any) string {
	trimmed := strings.Join(strings.Fields(strings.TrimSpace(fmt.Sprint(value))), " ")
	if trimmed == "" || trimmed == "<nil>" {
		return ""
	}
	const maxLen = 120
	if len(trimmed) > maxLen {
		return trimmed[:maxLen] + "..."
	}
	return trimmed
}

func hubErrorNumberString(value any) string {
	switch v := value.(type) {
	case float64:
		if v <= 0 {
			return ""
		}
		return strconv.FormatInt(int64(v), 10)
	case int64:
		if v <= 0 {
			return ""
		}
		return strconv.FormatInt(v, 10)
	case int:
		if v <= 0 {
			return ""
		}
		return strconv.Itoa(v)
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" || trimmed == "0" {
			return ""
		}
		return trimmed
	default:
		return ""
	}
}

// classifyOpenAIHTTPError parses OpenAI-compatible API error responses and
// returns a user-friendly message that names the configured provider, not the
// wire protocol.
func classifyOpenAIHTTPError(statusCode int, body []byte, providerName string) string {
	providerDisplay := llmProviderDisplayName(providerName)
	// 尝试解析 OpenAI 标准错误格式
	var errBody struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &errBody)
	code := errBody.Error.Code
	typ := errBody.Error.Type
	msg := extractOpenAIHTTPErrorMessage(body, errBody.Error.Message)

	if friendly := classifyHubErrorBody(body); friendly != "" {
		return friendly
	}

	// Hub wraps upstream provider auth failures as LLM_UPSTREAM_AUTH_FAILED
	// and rate limits as LLM_UPSTREAM_RATE_LIMITED with descriptive Chinese
	// messages. Surface them directly so the user (or admin) knows the
	// problem is the upstream provider, not their own credentials.
	if (code == "LLM_UPSTREAM_AUTH_FAILED" || code == "LLM_UPSTREAM_RATE_LIMITED") && msg != "" {
		return msg
	}

	switch {
	case typ == "overloaded_error" || strings.Contains(typ, "overloaded"):
		return fmt.Sprintf("%s 服务器超载，请稍后再试 (overloaded)", providerDisplay)
	case code == "insufficient_quota" || typ == "insufficient_quota":
		return fmt.Sprintf("%s 账号额度不足，请检查账单和付费计划 (insufficient_quota)", providerDisplay)
	case statusCode == http.StatusBadRequest && strings.TrimSpace(msg) != "":
		return fmt.Sprintf("%s API invalid request (HTTP 400): %s", providerDisplay, summarizeProviderHTTPErrorMessage(msg))
	case statusCode == http.StatusTooManyRequests:
		if strings.Contains(string(body), "rate_limit") {
			return fmt.Sprintf("%s API 请求频率超限，请稍后再试 (rate_limit)", providerDisplay)
		}
		return fmt.Sprintf("%s API 请求过于频繁，请稍后再试 (HTTP 429)", providerDisplay)
	case statusCode == http.StatusUnauthorized:
		return fmt.Sprintf("%s 认证失败，API Key 无效或已过期，请重新登录 (HTTP 401)", providerDisplay)
	case statusCode == http.StatusForbidden:
		return fmt.Sprintf("%s 拒绝访问，账号可能被限制或无权使用该模型 (HTTP 403)", providerDisplay)
	case statusCode == http.StatusBadGateway:
		// Hub wraps upstream provider errors with specific error codes.
		if code == "LLM_UPSTREAM_AUTH_FAILED" || code == "LLM_UPSTREAM_FAILED" || code == "LLM_UPSTREAM_RATE_LIMITED" {
			if msg != "" {
				return msg
			}
		}
		return "API 网关错误，上游服务不可用，请稍后再试 (HTTP 502)"
	case statusCode == http.StatusServiceUnavailable:
		return "API service temporarily unavailable; retry later (HTTP 503)"
	case statusCode == http.StatusGatewayTimeout:
		return "API gateway timeout; upstream is slow; retry later (HTTP 504)"
	case statusCode >= 500:
		return fmt.Sprintf("API server error; retry later (HTTP %d)", statusCode)
	default:
		// Check for overloaded signals in body even when JSON parse failed
		// (e.g. body is wrapped with non-JSON prefix from SDK error formatting).
		bodyLower := strings.ToLower(string(body))
		if strings.Contains(bodyLower, "overloaded") {
			return fmt.Sprintf("%s 服务器超载，请稍后再试 (overloaded)", providerDisplay)
		}
		return fmt.Sprintf("%s API 错误 (HTTP %d)", providerDisplay, statusCode)
	}
}

func extractOpenAIHTTPErrorMessage(body []byte, fallback string) string {
	if strings.TrimSpace(fallback) != "" {
		return fallback
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	for _, key := range []string{"message", "msg", "detail"} {
		msg, _ := payload[key].(string)
		if strings.TrimSpace(msg) != "" {
			return msg
		}
	}
	return ""
}

func summarizeProviderHTTPErrorMessage(msg string) string {
	msg = strings.Join(strings.Fields(msg), " ")
	const limit = 300
	runes := []rune(msg)
	if len(runes) <= limit {
		return msg
	}
	return string(runes[:limit]) + "..."
}

func classifyHubLLMServiceError(code, message string, retryAfterSeconds int64, retryAfterAt string) string {
	switch code {
	case "LLM_SERVICE_PERIOD_LIMITED":
		retryText := formatHubRetryText(retryAfterSeconds, retryAfterAt)
		if retryText != "" {
			return "MaClaw official quota is rate-limited; retry after " + retryText + "."
		}
		return "MaClaw official quota is rate-limited."
	case "LLM_SERVICE_CREDITS_EXHAUSTED":
		return "MaClaw official credits are exhausted. Redeem credits or switch provider."
	case "LLM_SERVICE_GRANT_QUEUED":
		retryText := formatHubRetryText(retryAfterSeconds, retryAfterAt)
		if retryText != "" {
			return "MaClaw official grant is not active yet; retry after " + retryText + "."
		}
		return "MaClaw official grant is not active yet; retry later."
	case "LLM_SERVICE_GRANT_EXPIRED":
		return "MaClaw official grant expired. Redeem a new grant or switch provider."
	case "LLM_SERVICE_CREDITS_REQUIRED":
		return "MaClaw official provider requires valid credits. Redeem credits or switch provider."
	}
	if strings.HasPrefix(code, "LLM_SERVICE_") && message != "" {
		return message
	}
	return ""
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
		return fmt.Sprintf("%d seconds", seconds)
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
	return fmt.Sprintf("%d days", days)
}

func classifyOpenAICompatibleHTTPError(err error, providerName string) (string, bool) {
	if err == nil {
		return "", false
	}
	var httpErr *llm.HTTPStatusError
	if errors.As(err, &httpErr) && httpErr != nil && httpErr.StatusCode > 0 {
		return classifyOpenAIHTTPError(httpErr.StatusCode, httpErr.Body, providerName), true
	}
	msg := err.Error()
	match := openAICompatibleHTTPStatusRe.FindStringSubmatchIndex(msg)
	if len(match) < 4 {
		return "", false
	}
	statusCode, parseErr := strconv.Atoi(msg[match[2]:match[3]])
	if parseErr != nil || statusCode <= 0 {
		return "", false
	}
	body := ""
	if colon := strings.Index(msg[match[1]:], ":"); colon >= 0 {
		body = strings.TrimSpace(msg[match[1]+colon+1:])
	}
	return classifyOpenAIHTTPError(statusCode, []byte(body), providerName), true
}

// truncateLLMBody trims an error body for logs and UI messages.
func truncateLLMBody(body []byte, maxLen int) string {
	s := string(body)
	if looksLikeHTML(s) {
		s = htmlTagStripRe.ReplaceAllString(s, " ")
		s = strings.Join(strings.Fields(s), " ") // collapse whitespace
		s = strings.TrimSpace(s)
	}
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}

var htmlTagStripRe = regexp.MustCompile(`<[^>]*>`)

// looksLikeHTML returns true if the string appears to contain HTML markup.
func looksLikeHTML(s string) bool {
	lower := strings.ToLower(s)
	return strings.Contains(lower, "<html") ||
		strings.Contains(lower, "<!doctype") ||
		strings.Contains(lower, "<center>") ||
		strings.Contains(lower, "<head>")
}

func partialSuffixLen(s, tag string) int {
	for i := 1; i < len(tag); i++ {
		if strings.HasSuffix(s, tag[:i]) {
			return i
		}
	}
	return 0
}

func consumeTagOpen(s, openTag string) (prefix string, remainder string, found bool) {
	idx := strings.Index(s, openTag)
	if idx < 0 {
		return "", s, false
	}
	return s[:idx], s[idx+len(openTag):], true
}

func consumeTagClose(s, closeTag string) (remainder string, found bool) {
	idx := strings.Index(s, closeTag)
	if idx < 0 {
		return s, false
	}
	return s[idx+len(closeTag):], true
}

func (f tokenStreamFilter) Callback() llm.TokenCallback {
	return f.Write
}

func (f tokenStreamFilter) Write(delta string) {
	if f.writeFn != nil {
		f.writeFn(delta)
	}
}

func (f tokenStreamFilter) Flush() {
	if f.flushFn != nil {
		f.flushFn()
	}
}

type flushableThinkFilter struct {
	downstream  llm.TokenCallback
	reasoningCb llm.TokenCallback // optional: receives thinking content (forwarded to frontend as reasoning)
	inside      bool
	trimNext    bool
	pending     strings.Builder
	emitted     bool
}

func newFlushableThinkFilter(downstream llm.TokenCallback) *flushableThinkFilter {
	return &flushableThinkFilter{downstream: downstream}
}

// SetReasoningCallback sets a callback that receives thinking content
// (text inside <think>...</think> tags) instead of discarding it.
// The callback is typically used to forward thinking tokens to the frontend
// with a \x01 prefix so they display in the reasoning/thinking UI.
func (f *flushableThinkFilter) SetReasoningCallback(cb llm.TokenCallback) {
	f.reasoningCb = cb
}

func (f *flushableThinkFilter) Write(delta string) {
	f.pending.WriteString(delta)
	f.drain(false)
}

func (f *flushableThinkFilter) Flush() {
	f.drain(true)
}

func (f *flushableThinkFilter) drain(force bool) {
	for {
		s := f.pending.String()
		if s == "" {
			return
		}

		if (f.trimNext || !f.emitted) && !f.inside {
			trimmed := strings.TrimLeft(s, " \t\r\n")
			if trimmed == "" {
				f.pending.Reset()
				return
			}
			if trimmed != s {
				f.pending.Reset()
				f.pending.WriteString(trimmed)
				f.trimNext = false
				continue
			}
			f.trimNext = false
		}

		if !f.inside {
			if prefix, remainder, found := consumeTagOpen(s, guiThinkOpen); found {
				if prefix != "" {
					f.downstream(prefix)
					f.emitted = true
				}
				f.inside = true
				f.pending.Reset()
				f.pending.WriteString(remainder)
				continue
			}

			if partialLen := partialSuffixLen(s, guiThinkOpen); partialLen > 0 {
				if force {
					f.downstream(s)
					f.emitted = true
					f.pending.Reset()
					return
				}
				if len(s) > partialLen {
					f.downstream(s[:len(s)-partialLen])
					f.emitted = true
					f.pending.Reset()
					f.pending.WriteString(guiThinkOpen[:partialLen])
				}
				return
			}

			f.downstream(s)
			f.emitted = true
			f.pending.Reset()
			return
		}

		if remainder, found := consumeTagClose(s, guiThinkClose); found {
			// Forward thinking content before the close tag to reasoning callback.
			if f.reasoningCb != nil {
				closeIdx := len(s) - len(remainder) - len(guiThinkClose)
				if closeIdx > 0 {
					f.reasoningCb(s[:closeIdx])
				}
			}
			f.inside = false
			f.trimNext = true
			f.pending.Reset()
			f.pending.WriteString(remainder)
			continue
		}

		// Inside <think> block: forward content to reasoning callback if set.
		if f.reasoningCb != nil {
			// Emit everything except possible partial close tag at the end.
			partLen := partialSuffixLen(s, guiThinkClose)
			if partLen > 0 {
				if len(s) > partLen {
					f.reasoningCb(s[:len(s)-partLen])
					f.pending.Reset()
					f.pending.WriteString(s[len(s)-partLen:])
				}
				if force {
					// Partial tag at end of stream — emit as reasoning too.
					f.reasoningCb(f.pending.String())
					f.pending.Reset()
				}
				return
			}
			// No partial close tag — emit all accumulated content as reasoning.
			f.reasoningCb(s)
			f.pending.Reset()
			return
		}

		if force {
			f.pending.Reset()
			return
		}
		if partialSuffixLen(s, guiThinkClose) == 0 {
			f.pending.Reset()
		}
		return
	}
}

type flushableDetailsFilter struct {
	downstream llm.TokenCallback
	inside     bool
	pending    strings.Builder
}

func newFlushableDetailsFilter(downstream llm.TokenCallback) *flushableDetailsFilter {
	return &flushableDetailsFilter{downstream: downstream}
}

func (f *flushableDetailsFilter) Write(delta string) {
	if f == nil || f.downstream == nil {
		return
	}
	f.pending.WriteString(delta)
	f.drain(false)
}

func (f *flushableDetailsFilter) Flush() {
	if f == nil || f.downstream == nil {
		return
	}
	f.drain(true)
}

func (f *flushableDetailsFilter) drain(force bool) {
	for {
		s := f.pending.String()
		if s == "" {
			return
		}
		lower := strings.ToLower(s)
		if !f.inside {
			if idx := strings.Index(lower, "<details"); idx >= 0 {
				if idx > 0 {
					f.downstream(s[:idx])
				}
				end := strings.IndexByte(s[idx:], '>')
				if end < 0 {
					f.pending.Reset()
					f.pending.WriteString(s[idx:])
					return
				}
				f.inside = true
				f.pending.Reset()
				f.pending.WriteString(s[idx+end+1:])
				continue
			}
			if partial := detailsOpenSuffixLen(lower); partial > 0 && !force {
				if len(s) > partial {
					f.downstream(s[:len(s)-partial])
					f.pending.Reset()
					f.pending.WriteString(s[len(s)-partial:])
				}
				return
			}
			f.downstream(s)
			f.pending.Reset()
			return
		}
		if idx := strings.Index(lower, "</details>"); idx >= 0 {
			f.inside = false
			f.pending.Reset()
			f.pending.WriteString(s[idx+len("</details>"):])
			continue
		}
		if partial := detailsCloseSuffixLen(lower); partial > 0 && !force {
			if len(s) > partial {
				f.pending.Reset()
				f.pending.WriteString(s[len(s)-partial:])
			}
			return
		}
		f.pending.Reset()
		return
	}
}

func detailsOpenSuffixLen(lower string) int {
	marker := "<details"
	max := len(marker) - 1
	if len(lower) < max {
		max = len(lower)
	}
	for i := max; i > 0; i-- {
		if strings.HasSuffix(lower, marker[:i]) {
			return i
		}
	}
	return 0
}

func detailsCloseSuffixLen(lower string) int {
	marker := "</details>"
	max := len(marker) - 1
	if len(lower) < max {
		max = len(lower)
	}
	for i := max; i > 0; i-- {
		if strings.HasSuffix(lower, marker[:i]) {
			return i
		}
	}
	return 0
}

type flushableTagFilter struct {
	downstream llm.TokenCallback
	openTag    string
	closeTag   string
	inside     bool
	pending    strings.Builder
}

func newFlushableTagFilter(downstream llm.TokenCallback, open, close string) *flushableTagFilter {
	return &flushableTagFilter{downstream: downstream, openTag: open, closeTag: close}
}

func (f *flushableTagFilter) Write(delta string) {
	f.pending.WriteString(delta)
	f.drain(false)
}

func (f *flushableTagFilter) Flush() {
	f.drain(true)
}

func (f *flushableTagFilter) drain(force bool) {
	for {
		s := f.pending.String()
		if s == "" {
			return
		}

		if !f.inside {
			if prefix, remainder, found := consumeTagOpen(s, f.openTag); found {
				if prefix != "" {
					f.downstream(prefix)
				}
				f.inside = true
				f.pending.Reset()
				f.pending.WriteString(remainder)
				continue
			}
			if partialLen := partialSuffixLen(s, f.openTag); partialLen > 0 {
				if force {
					f.downstream(s)
					f.pending.Reset()
					return
				}
				if len(s) > partialLen {
					f.downstream(s[:len(s)-partialLen])
					f.pending.Reset()
					f.pending.WriteString(f.openTag[:partialLen])
				}
				return
			}
			f.downstream(s)
			f.pending.Reset()
			return
		}

		if remainder, found := consumeTagClose(s, f.closeTag); found {
			f.inside = false
			f.pending.Reset()
			f.pending.WriteString(remainder)
			continue
		}

		if force {
			f.pending.Reset()
			return
		}
		if partialSuffixLen(s, f.closeTag) == 0 {
			f.pending.Reset()
		}
		return
	}
}

// NewRoundCallback is called when a new agent loop iteration starts LLM generation.
type NewRoundCallback func()

// StreamDoneCallback is called when a single LLM streaming round finishes,
// allowing the frontend to hide the "thinking" indicator before the full
// agent loop completes (tool execution may still be in progress).
type StreamDoneCallback func()

func stripThinkTags(s string) string {
	return llm.StripDetailsBlocks(llm.StripThinkTags(s))
}

func stripFunctionCalls(s string) string {
	return strings.TrimSpace(guiFuncCallBlock.ReplaceAllString(s, ""))
}

func stripXMLToolCalls(s string) string {
	return llm.StripXMLToolCalls(s)
}

// ---------------------------------------------------------------------------
// Filter factory functions using corelib/llm
// ---------------------------------------------------------------------------

func newThinkFilter(downstream llm.TokenCallback) tokenStreamFilter {
	return newThinkFilterWithReasoning(downstream, nil)
}

// newThinkFilterWithReasoning creates a think filter that optionally forwards
// thinking content (inside <think> tags) to the reasoningCb callback.
// This allows the frontend to display the model's thinking process in a
// collapsible UI section.
func newThinkFilterWithReasoning(downstream llm.TokenCallback, reasoningCb llm.TokenCallback) tokenStreamFilter {
	detailsFilter := newFlushableDetailsFilter(downstream)
	thinkFilter := newFlushableThinkFilter(detailsFilter.Write)
	if reasoningCb != nil {
		thinkFilter.SetReasoningCallback(reasoningCb)
	}
	return tokenStreamFilter{
		writeFn: thinkFilter.Write,
		flushFn: func() {
			thinkFilter.Flush()
			detailsFilter.Flush()
		},
	}
}

func newFuncCallFilter(downstream llm.TokenCallback) tokenStreamFilter {
	f := newFlushableTagFilter(downstream, guiFuncCallOpen, guiFuncCallClose)
	return tokenStreamFilter{writeFn: f.Write, flushFn: f.Flush}
}

func newToolCallFilter(downstream llm.TokenCallback) tokenStreamFilter {
	plainFilter := newPlainToolCallStreamFilter(downstream)
	xmlFilter := newFlushableTagFilter(plainFilter.Write, guiToolCallOpen, guiToolCallClose)
	codexFilter := newFlushableTagFilter(xmlFilter.Write, guiCodexToolCallOpen, guiCodexToolCallClose)
	return tokenStreamFilter{
		writeFn: codexFilter.Write,
		flushFn: func() {
			codexFilter.Flush()
			xmlFilter.Flush()
			plainFilter.Flush()
		},
	}
}

type plainToolCallStreamFilter struct {
	downstream llm.TokenCallback
	pending    strings.Builder
	suppressed bool
}

func newPlainToolCallStreamFilter(downstream llm.TokenCallback) *plainToolCallStreamFilter {
	return &plainToolCallStreamFilter{downstream: downstream}
}

func (f *plainToolCallStreamFilter) Write(delta string) {
	if f.suppressed {
		return
	}
	f.pending.WriteString(delta)
	f.drain(false)
}

func (f *plainToolCallStreamFilter) Flush() {
	f.drain(true)
}

func (f *plainToolCallStreamFilter) drain(force bool) {
	if f.suppressed {
		f.pending.Reset()
		return
	}
	s := f.pending.String()
	if s == "" {
		return
	}
	if looksLikeBareJSONToolCallStreamPrefix(s) {
		if !force {
			return
		}
		if calls, malformed := llm.ParseContentToolCallsDetailed(s); len(calls) > 0 || malformed || bareJSONToolCallTextLooksLikely(s) {
			f.suppressed = true
			f.pending.Reset()
			return
		}
	}
	idx := firstGUIContentToolCallMarkerIndex(strings.ToLower(s))
	if idx >= 0 {
		if idx > 0 {
			f.downstream(s[:idx])
		}
		f.suppressed = true
		f.pending.Reset()
		return
	}
	lower := strings.ToLower(s)
	partial := guiContentToolCallMarkerSuffixLen(lower)
	if partial > 0 && !force {
		if len(s) > partial {
			f.downstream(s[:len(s)-partial])
			f.pending.Reset()
			f.pending.WriteString(s[len(s)-partial:])
		}
		return
	}
	f.downstream(s)
	f.pending.Reset()
}

func firstGUIContentToolCallMarkerIndex(lower string) int {
	best := -1
	for _, marker := range []string{"<tool_call", "<turn: tool_call", "tool_call\n", "tool_call\r\n", "tool_call {"} {
		if idx := strings.Index(lower, marker); idx >= 0 && (best < 0 || idx < best) {
			best = idx
		}
	}
	return best
}

func guiContentToolCallMarkerSuffixLen(lower string) int {
	best := 0
	for _, marker := range []string{"<tool_call", "<turn: tool_call", "tool_call\n", "tool_call\r\n", "tool_call {"} {
		max := len(marker) - 1
		if len(lower) < max {
			max = len(lower)
		}
		for i := max; i > best; i-- {
			if strings.HasSuffix(lower, marker[:i]) {
				best = i
				break
			}
		}
	}
	return best
}

func bareJSONToolCallTextLooksLikely(content string) bool {
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "```") {
		lines := strings.Split(trimmed, "\n")
		if len(lines) >= 2 {
			trimmed = strings.TrimSpace(strings.Join(lines[1:], "\n"))
		}
	}
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return false
	}
	lower := strings.ToLower(trimmed)
	return strings.Contains(lower, `"tool_calls"`) ||
		strings.Contains(lower, `"function_call"`) ||
		(strings.Contains(lower, `"function"`) && strings.Contains(lower, `"arguments"`)) ||
		(strings.Contains(lower, `"name"`) && strings.Contains(lower, `"arguments"`))
}

func looksLikeBareJSONToolCallStreamPrefix(content string) bool {
	trimmed := strings.TrimLeft(content, " \t\r\n")
	if trimmed == "" {
		return true
	}
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix("```json", lower) || strings.HasPrefix(lower, "```json") {
		return true
	}
	return strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")
}

type llmStreamMetrics struct {
	RequestBuildNanos     int64
	HTTPDoNanos           int64
	FirstSSEWaitNanos     int64
	FirstTokenAt          time.Time
	IdleTimeoutCount      int
	IdleTimeoutAfterToken bool
	MaxTokenGapNanos      int64
}

// transactionalTokenBuffer manages streaming text tokens during an LLM call.
//
// Design: When the LLM produces both text and tool_calls in the same response,
// the text is often just a trivial preamble ("好的，让我搜索一下"). We want to
// suppress such filler so the user doesn't see flickering short text between
// tool executions. However, some models (DeepSeek, GLM 5.1) produce substantive
// reasoning text before tool calls — the user needs to see this to understand
// what the agent is doing and whether it's heading in the right direction.
//
// Strategy:
//   - Tokens are buffered until total accumulated length reaches a threshold.
//   - Once the threshold is crossed, all buffered tokens are flushed and
//     subsequent tokens pass through immediately (streaming mode).
//   - On Flush(): any remaining buffered tokens are forwarded.
//   - On Discard(): if still in buffered mode (below threshold), tokens are
//     dropped (trivial preamble). If already in streaming mode, nothing to
//     discard — tokens were already sent.
//
// This ensures:
//   - Short filler text ("好的") before tool calls → user never sees it.
//   - Substantive reasoning text → user sees it in real-time.
//   - Final text-only responses → always visible (Flush is called).
const tokenBufferFlushThreshold = 40 // runes; ~20 CJK chars or ~40 ASCII chars

type transactionalTokenBuffer struct {
	onToken   llm.TokenCallback
	deltas    []string
	runeCount int
	streaming bool // true once threshold crossed — tokens pass through directly
}

func newTransactionalTokenBuffer(onToken llm.TokenCallback) *transactionalTokenBuffer {
	if onToken == nil {
		onToken = func(string) {}
	}
	return &transactionalTokenBuffer{onToken: onToken}
}

func (b *transactionalTokenBuffer) Write(delta string) {
	if delta == "" {
		return
	}
	// Reasoning tokens (prefixed with \x01) always pass through immediately —
	// the user should always see the model's thinking process. They don't count
	// toward the content threshold that controls preamble suppression.
	if len(delta) > 0 && delta[0] == '\x01' {
		b.onToken(delta)
		return
	}
	if b.streaming {
		// Already past threshold — pass through immediately.
		b.onToken(delta)
		return
	}
	b.deltas = append(b.deltas, delta)
	b.runeCount += len([]rune(delta))
	if b.runeCount >= tokenBufferFlushThreshold {
		// Threshold crossed — flush all buffered tokens and switch to streaming.
		b.streaming = true
		for _, d := range b.deltas {
			b.onToken(d)
		}
		b.deltas = nil
	}
}

func (b *transactionalTokenBuffer) Flush() {
	// Forward any remaining buffered tokens (below threshold).
	for _, delta := range b.deltas {
		b.onToken(delta)
	}
	b.deltas = nil
	b.streaming = true
}

func (b *transactionalTokenBuffer) Discard() {
	// Only discard if we haven't already streamed tokens to the frontend.
	// If streaming=true, tokens were already sent — nothing to take back.
	if !b.streaming {
		b.deltas = nil
	}
}

func responseHasToolCalls(resp *llm.Response) bool {
	if resp == nil || len(resp.Choices) == 0 {
		return false
	}
	return len(resp.Choices[0].Message.ToolCalls) > 0
}

func withFirstTokenMetrics(onToken llm.TokenCallback, metrics *llmStreamMetrics) llm.TokenCallback {
	if onToken == nil {
		onToken = func(string) {}
	}
	var lastTokenAt time.Time
	return func(delta string) {
		now := time.Now()
		if metrics != nil && delta != "" {
			if metrics.FirstTokenAt.IsZero() {
				metrics.FirstTokenAt = now
			}
			if !lastTokenAt.IsZero() {
				gap := now.Sub(lastTokenAt).Nanoseconds()
				if gap > metrics.MaxTokenGapNanos {
					metrics.MaxTokenGapNanos = gap
				}
			}
			lastTokenAt = now
		}
		onToken(delta)
	}
}

// The ctx parameter carries cancellation from the LoopContext so that
// in-flight HTTP requests are aborted promptly when the user cancels.
func (h *IMMessageHandler) doLLMRequestStream(
	reqCtx context.Context,
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	tools []map[string]interface{},
	httpClient *http.Client,
	onToken llm.TokenCallback,
	metrics *llmStreamMetrics,
) (*llm.Response, error) {
	reqCtx = llm.WithRequestTraceIfMissing(reqCtx, "im_stream")
	// Always use the streaming path even when onToken is nil (e.g. WeChat IM
	// standalone mode). The non-streaming DoOpenAIRequest path uses io.ReadAll
	// which blocks until the entire SSE stream finishes  - causing multi-minute
	// delays when the API returns SSE despite stream:false. A noop callback
	// lets us stream incrementally and discard tokens we don't need to display.
	tokenBuffer := newTransactionalTokenBuffer(onToken)
	meteredOnToken := withFirstTokenMetrics(tokenBuffer.Write, metrics)
	if h.app != nil {
		if cachedResp, ok := h.app.cachedStreamHit(reqCtx, cfg, messages, tools, meteredOnToken); ok {
			if responseHasToolCalls(cachedResp) {
				tokenBuffer.Discard()
			} else {
				tokenBuffer.Flush()
			}
			return cachedResp, nil
		}
	}
	lease, trace, acquireErr := acquireLLMSchedulerLease(reqCtx)
	if acquireErr != nil {
		return nil, acquireErr
	}
	defer lease.Release()
	scheduledCtx, scheduledCancel := context.WithCancel(reqCtx)
	lease.SetCancel(scheduledCancel)
	defer scheduledCancel()
	var resp *llm.Response
	var err error
	if cfg.IsResponsesWebSocket() {
		resp, err = h.doResponsesWSLLMRequestStream(scheduledCtx, cfg, messages, tools, httpClient, meteredOnToken, metrics)
	} else if cfg.IsResponsesAPI() {
		resp, err = h.doResponsesAPILLMRequestStream(scheduledCtx, cfg, messages, tools, httpClient, meteredOnToken, metrics)
	} else if cfg.Protocol == "anthropic" {
		resp, err = h.doAnthropicLLMRequestStream(scheduledCtx, cfg, messages, tools, httpClient, meteredOnToken, metrics)
	} else {
		resp, err = h.doOpenAILLMRequestStream(scheduledCtx, cfg, messages, tools, httpClient, meteredOnToken, metrics)
	}
	globalLLMScheduler.ObserveResult(trace, err)
	if h.app != nil {
		h.app.observeLLMEndpointResult(cfg, err)
	}
	if err != nil || responseHasToolCalls(resp) {
		tokenBuffer.Discard()
	} else {
		tokenBuffer.Flush()
		if h.app != nil {
			h.app.storeStreamResponse(cfg, messages, tools, resp)
		}
	}
	return resp, err
}

// ---------------------------------------------------------------------------
// OpenAI SSE streaming
// ---------------------------------------------------------------------------

type openAIStreamDelta struct {
	Content          string                  `json:"content,omitempty"`
	ReasoningContent string                  `json:"reasoning_content,omitempty"`
	ToolCalls        []openAIStreamToolDelta `json:"tool_calls,omitempty"`
}

type openAIStreamToolDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function,omitempty"`
}

type openAIStreamChunk struct {
	Choices []struct {
		Delta        openAIStreamDelta `json:"delta"`
		FinishReason *string           `json:"finish_reason"`
	} `json:"choices"`
	Usage *llm.Usage `json:"usage,omitempty"`
}

func (h *IMMessageHandler) doOpenAILLMRequestStream(
	reqCtx context.Context,
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	tools []map[string]interface{},
	httpClient *http.Client,
	onToken llm.TokenCallback,
	metrics *llmStreamMetrics,
) (*llm.Response, error) {
	return h.doOpenAILLMRequestStreamSDK(reqCtx, cfg, messages, tools, httpClient, onToken, metrics)
}

func (h *IMMessageHandler) doOpenAILLMRequestStreamSDK(
	reqCtx context.Context,
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	tools []map[string]interface{},
	httpClient *http.Client,
	onToken llm.TokenCallback,
	metrics *llmStreamMetrics,
) (*llm.Response, error) {
	requestBuildStartedAt := time.Now()
	endpoint, reqBody, err := llm.BuildOpenAIChatRequestData(cfg, messages, llm.OpenAIChatRequestOptions{
		Stream: true,
		Tools:  tools,
	})
	if err != nil {
		return nil, err
	}
	if metrics != nil {
		metrics.RequestBuildNanos += time.Since(requestBuildStartedAt).Nanoseconds()
	}
	upstreamModel := cfg.UpstreamModel()
	log.Printf("[LLM Stream] POST %s model=%s configured_model=%s protocol=%s sdk=openai-go %s", endpoint, upstreamModel, cfg.Model, cfg.Protocol, llm.RequestTraceLogFields(reqCtx))

	var filteredBuf strings.Builder
	filteredOnToken := func(delta string) {
		filteredBuf.WriteString(delta)
		if onToken != nil {
			onToken(delta)
		}
	}
	rpf := newRolePrefixStreamFilter(filteredOnToken)
	repf := newRepetitionFilter(rpf.Write)
	tcf := newToolCallFilter(repf.Write)
	fcf := newFuncCallFilter(tcf.Callback())
	thinkReasoningCbSDK := func(delta string) {
		if onToken != nil && delta != "" {
			onToken("\x01" + delta)
		}
	}
	reasoningRoleFilter := newRolePrefixStreamFilter(thinkReasoningCbSDK)
	tf := newThinkFilterWithReasoning(fcf.Callback(), thinkReasoningCbSDK)

	httpDoStartedAt := time.Now()
	resp, err := llm.DoOpenAIRequestStreamWithReasoning(reqCtx, cfg, messages, tools, httpClient, func(delta string) {
		tf.Write(delta)
	}, func(delta string) {
		reasoningRoleFilter.Write(delta)
	})
	if metrics != nil {
		metrics.HTTPDoNanos += time.Since(httpDoStartedAt).Nanoseconds()
	}
	if err != nil {
		if friendly, ok := classifyOpenAICompatibleHTTPError(err, cfg.ProviderName); ok {
			log.Printf("[LLM Stream] SDK error endpoint=%s request=%s err=%v", endpoint, llm.SummarizeOpenAIChatRequestBody(reqBody), err)
			return nil, fmt.Errorf("%s [url=%s model=%s]", friendly, endpoint, upstreamModel)
		}
		return nil, fmt.Errorf("[%s] %w", endpoint, err)
	}

	tf.Flush()
	reasoningRoleFilter.Flush()
	fcf.Flush()
	tcf.Flush()
	repf.Flush()
	rpf.Flush()
	if repf.Halted() {
		log.Printf("[LLM Stream] repetition filter halted: suppressed %d runes", repf.SuppressedRunes())
	}
	if rpf.Halted() {
		log.Printf("[LLM Stream] role prefix filter halted: suppressed %d runes", rpf.SuppressedRunes())
	}
	if resp == nil || len(resp.Choices) == 0 {
		return resp, nil
	}

	choice := resp.Choices[0]
	msg := choice.Message
	rawContent := msg.Content
	content := stripXMLToolCalls(stripFunctionCalls(stripThinkTags(rawContent)))
	filteredStr := filteredBuf.String()
	if filteredStr != "" {
		content = stripXMLToolCalls(filteredStr)
	}
	BrowserDiagCP5_StreamFilter(
		rpf.Halted(), rpf.SuppressedRunes(),
		repf.Halted(), repf.SuppressedRunes(),
		rpf.Halted(),
		browserDiagHasBrowserRolePrefix(filteredStr),
		filteredStr,
	)
	reasoning := stripRolePrefixReasoningForDisplay(msg.ReasoningContent)
	if content == "" && reasoning != "" {
		content = stripXMLToolCalls(stripFunctionCalls(stripThinkTags(reasoning)))
	}
	msg.Content = content
	msg.ReasoningContent = reasoning
	finishReason := choice.FinishReason
	truncatedTools := append([]string(nil), choice.TruncatedToolNames...)
	if len(msg.ToolCalls) == 0 {
		if xmlCalls, malformed := parseXMLContentToolCallsDetailed(rawContent); len(xmlCalls) > 0 {
			msg.ToolCalls = append(msg.ToolCalls, xmlCalls...)
			finishReason = llmFinishReasonToolCalls.String()
			msg.Content = ""
		} else if malformed {
			msg.Content = llm.MalformedContentToolCallErrorMsg
			finishReason = llmFinishReasonStop.String()
		}
	}
	if finishReason == "" {
		finishReason = llmFinishReasonStop.String()
	}
	var truncatedToolArgs map[string]string
	if len(truncatedTools) == 0 {
		finishReason, truncatedTools, truncatedToolArgs = filterTruncatedToolCalls(&msg, finishReason)
	}
	// Enhanced diagnostic when truncation is detected via GUI path
	if len(truncatedTools) > 0 && resp != nil && resp.Usage != nil {
		u := resp.Usage
		log.Printf("[LLM-stream-diag] GUI truncation detected: truncated=%v input=%d output=%d total=%d reasoning_content=%d chars",
			truncatedTools, u.InputTokens, u.OutputTokens, u.TotalTokens, len(msg.ReasoningContent))
	}
	return &llm.Response{
		Choices: []llm.Choice{{Message: msg, FinishReason: finishReason, TruncatedToolNames: truncatedTools, TruncatedToolArgs: truncatedToolArgs}},
		Usage:   resp.Usage,
	}, nil
}

func parseNonStreamOpenAIResponse(resp *http.Response) (*llm.Response, error) {
	return llm.ParseNonStreamOpenAIResponse(resp)
}

// ---------------------------------------------------------------------------
// Anthropic SSE streaming
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) doAnthropicLLMRequestStream(
	reqCtx context.Context,
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	tools []map[string]interface{},
	httpClient *http.Client,
	onToken llm.TokenCallback,
	metrics *llmStreamMetrics,
) (*llm.Response, error) {
	requestBuildStartedAt := time.Now()
	if _, _, err := llm.BuildAnthropicMessagesRequestData(cfg, messages, llm.AnthropicMessagesRequestOptions{Stream: true, Tools: tools}); err != nil {
		return nil, err
	}
	if metrics != nil {
		metrics.RequestBuildNanos += time.Since(requestBuildStartedAt).Nanoseconds()
	}

	// Apply the same filter chain as the OpenAI path: role-prefix, repetition.
	// Without this, Anthropic-protocol responses (via OpenRouter, etc.) are
	// vulnerable to Browser: hallucinations and repetition degeneration.
	var filteredBuf strings.Builder
	filteredOnToken := func(delta string) {
		filteredBuf.WriteString(delta)
		if onToken != nil {
			onToken(delta)
		}
	}
	rpf := newRolePrefixStreamFilter(filteredOnToken)
	repf := newRepetitionFilter(rpf.Write)

	httpDoStartedAt := time.Now()
	resp, err := llm.DoAnthropicRequestStream(reqCtx, cfg, messages, tools, httpClient, repf.Write)
	if metrics != nil {
		metrics.HTTPDoNanos += time.Since(httpDoStartedAt).Nanoseconds()
		if metrics.FirstSSEWaitNanos == 0 {
			metrics.FirstSSEWaitNanos = metrics.HTTPDoNanos
		}
	}
	if err != nil {
		return nil, err
	}

	repf.Flush()
	rpf.Flush()
	if repf.Halted() {
		log.Printf("[LLM Stream] anthropic repetition filter halted: suppressed %d runes", repf.SuppressedRunes())
	}
	if rpf.Halted() {
		log.Printf("[LLM Stream] anthropic role prefix filter halted: suppressed %d runes", rpf.SuppressedRunes())
	}

	// Apply filteredBuf content to msg.Content for data flow consistency
	// (same mechanism as Fix #50 for OpenAI path).
	if resp != nil && len(resp.Choices) > 0 && filteredBuf.Len() > 0 {
		msg := resp.Choices[0].Message
		filtered := filteredBuf.String()
		if filtered != "" && (msg.Content == "" || len(filtered) <= len(msg.Content)) {
			msg.Content = filtered
			resp.Choices[0].Message = msg
		}
	}

	return resp, err
}

// parseNonStreamAnthropicResponse handles the fallback case where the provider
// returned a normal JSON response instead of SSE for Anthropic protocol.
func parseNonStreamAnthropicResponse(resp *http.Response, requestBody []byte) (*llm.Response, error) {
	return llm.ParseNonStreamAnthropicResponse(resp)
}
