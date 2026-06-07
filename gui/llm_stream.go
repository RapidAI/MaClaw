package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	guiThinkOpen     = "<think>"
	guiThinkClose    = "</think>"
	guiFuncCallOpen  = "<|FunctionCallBegin|>"
	guiFuncCallClose = "<|FunctionCallEnd|>"
	guiToolCallOpen  = "<tool_call>"
	guiToolCallClose = "</tool_call>"
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
// Returns the (possibly modified) finishReason and the list of truncated
// tool names (nil if none were truncated).
func filterTruncatedToolCalls(msg *llm.Message, finishReason string) (string, []string) {
	if len(msg.ToolCalls) == 0 {
		return finishReason, nil
	}

	// Primary signal: finish_reason="length" means the model hit max_output_tokens.
	isLengthTruncated := normalizeLLMFinishReason(finishReason) == llmFinishReasonLength

	var validCalls []llm.ToolCall
	var truncatedNames []string
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
			log.Printf("[LLM Stream] truncated tool call (invalid JSON): %s args=%d bytes finish_reason=%s", tc.Function.Name, len(args), finishReason)
		} else if missingField := detectTruncatedRequiredField(tc.Function.Name, parsed); missingField != "" {
			// Case 2: JSON valid but required field missing.
			// With finish_reason="length" this is definitely truncation.
			// Without "length" but with large args (>4000 bytes), some API
			// proxies (e.g. 智谱 GLM) return "stop" instead of "length"
			// when hitting max_output_tokens  - still treat as truncation.
			if isLengthTruncated || len(args) > 4000 {
				truncatedNames = append(truncatedNames, tc.Function.Name)
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
		return finishReason, nil
	}
	msg.ToolCalls = validCalls
	// Do NOT append hint to msg.Content  - the agent loop will inject it
	// as a separate system message. Keeping msg.Content clean ensures the
	// assistant message in conversation history only contains the LLM's
	// own text, not system-injected recovery instructions.
	if len(msg.ToolCalls) == 0 {
		return llmFinishReasonStop.String(), truncatedNames
	}
	return finishReason, truncatedNames
}

// truncatedRequiredFields maps tool names to their required fields for
// truncation detection. Only tools with large-content fields that can
// consume the entire output budget need to be listed here.
var truncatedRequiredFields = map[string][]string{
	"write_file": {"path", "content"},
	"edit_file":  {"path", "old_string", "new_string"},
}

// detectTruncatedRequiredField checks if a parsed tool call argument map is
// missing a required field, which indicates the output was truncated by the
// model's max_output_tokens limit.
//
// This is NOT a general parameter validation  - it specifically detects the
// pattern where a large field (e.g. content) consumed the entire output
// budget, preventing subsequent required fields from being generated.
func detectTruncatedRequiredField(toolName string, parsed map[string]interface{}) string {
	fields, ok := truncatedRequiredFields[toolName]
	if !ok {
		return ""
	}
	for _, f := range fields {
		if _, exists := parsed[f]; !exists {
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
		return friendly
	}
	if strings.HasPrefix(hubCode, "LLM_UPSTREAM_") && strings.TrimSpace(hubMessage) != "" {
		return hubMessage
	}
	return ""
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
	msg := errBody.Error.Message

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
	case code == "insufficient_quota" || typ == "insufficient_quota":
		return fmt.Sprintf("%s 账号额度不足，请检查账单和付费计划 (insufficient_quota)", providerDisplay)
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
		return fmt.Sprintf("%s API 错误 (HTTP %d)", providerDisplay, statusCode)
	}
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
	downstream llm.TokenCallback
	inside     bool
	trimNext   bool
	pending    strings.Builder
	emitted    bool
}

func newFlushableThinkFilter(downstream llm.TokenCallback) *flushableThinkFilter {
	return &flushableThinkFilter{downstream: downstream}
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
			f.inside = false
			f.trimNext = true
			f.pending.Reset()
			f.pending.WriteString(remainder)
			continue
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
	return llm.StripThinkTags(s)
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
	f := newFlushableThinkFilter(downstream)
	return tokenStreamFilter{writeFn: f.Write, flushFn: f.Flush}
}

func newFuncCallFilter(downstream llm.TokenCallback) tokenStreamFilter {
	f := newFlushableTagFilter(downstream, guiFuncCallOpen, guiFuncCallClose)
	return tokenStreamFilter{writeFn: f.Write, flushFn: f.Flush}
}

func newToolCallFilter(downstream llm.TokenCallback) tokenStreamFilter {
	f := newFlushableTagFilter(downstream, guiToolCallOpen, guiToolCallClose)
	return tokenStreamFilter{writeFn: f.Write, flushFn: f.Flush}
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
	requestBuildStartedAt := time.Now()
	req, _, endpoint, err := llm.NewOpenAIChatRequest(reqCtx, cfg, messages, llm.OpenAIChatRequestOptions{
		Stream: true,
		Tools:  tools,
		ExtraBody: map[string]interface{}{
			"stream_options": map[string]interface{}{
				"include_usage": true,
			},
		},
	})
	if err != nil {
		return nil, err
	}
	if metrics != nil {
		metrics.RequestBuildNanos += time.Since(requestBuildStartedAt).Nanoseconds()
	}
	log.Printf("[LLM Stream] POST %s model=%s protocol=%s %s", endpoint, cfg.Model, cfg.Protocol, llm.RequestTraceLogFields(reqCtx))

	httpDoStartedAt := time.Now()
	resp, err := httpClient.Do(req)
	if metrics != nil {
		metrics.HTTPDoNanos += time.Since(httpDoStartedAt).Nanoseconds()
	}
	if err != nil {
		return nil, fmt.Errorf("[%s] %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		log.Printf("[LLM Stream] HTTP 404: endpoint=%s content_type=%q body_len=%d", endpoint, resp.Header.Get("Content-Type"), len(body))
		return nil, fmt.Errorf("HTTP 404 (endpoint=%s, model=%s, protocol=%s, body_len=%d)", endpoint, cfg.Model, cfg.Protocol, len(body))
	}

	// Provide friendly HTTP errors instead of raw HTML/JSON bodies.
	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		log.Printf("[LLM Stream] HTTP %d: endpoint=%s content_type=%q body_len=%d", resp.StatusCode, endpoint, resp.Header.Get("Content-Type"), len(body))
		friendlyMsg := classifyOpenAIHTTPError(resp.StatusCode, body, cfg.ProviderName)
		return nil, fmt.Errorf("%s [url=%s model=%s]", friendlyMsg, endpoint, cfg.Model)
	}

	// Detect SSE: check Content-Type first, then sniff the body prefix.
	// Some API gateways (e.g. NewAPI, OneAPI) return SSE data but with a
	// non-standard Content-Type like "application/octet-stream" or "text/plain".
	contentType := resp.Header.Get("Content-Type")
	isSSE := strings.Contains(contentType, "text/event-stream")

	var bodyReader io.Reader = resp.Body
	if !isSSE {
		peek := make([]byte, 64)
		n, _ := resp.Body.Read(peek)
		peek = peek[:n]
		trimmed := bytes.TrimLeft(peek, " \\t\\r\\n")
		if bytes.HasPrefix(trimmed, []byte("data:")) {
			isSSE = true
		}
		bodyReader = io.MultiReader(bytes.NewReader(peek), resp.Body)
	}

	if !isSSE {
		// bodyReader includes the peeked bytes via MultiReader  - we must
		// read from it (not resp.Body) to get the complete response body.
		body, _ := io.ReadAll(io.LimitReader(bodyReader, 256*1024))
		parsed, parseErr := llm.ParseNonStreamOpenAIResponseBody(body)
		if parseErr != nil {
			log.Printf("[LLM Stream] non-SSE parse failed: content_type=%q body_len=%d err=%T", contentType, len(body), parseErr)
		}
		return parsed, parseErr
	}

	// filteredBuf accumulates the content that actually reaches onToken
	// (after all stream filters). msg.Content reads from filteredBuf so
	// the backend response is identical to what the frontend received via
	// streaming  - eliminating the data-flow fork between contentBuf (raw)
	// and the filter chain output that caused Browser: prefix leaks.
	var filteredBuf strings.Builder
	filteredOnToken := func(delta string) {
		filteredBuf.WriteString(delta)
		onToken(delta)
	}
	rpf := newRolePrefixStreamFilter(filteredOnToken)
	repf := newRepetitionFilter(rpf.Write)
	tcf := newToolCallFilter(repf.Write)
	fcf := newFuncCallFilter(tcf.Callback())
	tf := newThinkFilter(fcf.Callback())
	var contentBuf strings.Builder
	type toolAccum struct {
		id   string
		typ  string
		name strings.Builder
		args strings.Builder
	}
	toolAccums := make(map[int]*toolAccum)
	var reasoningBuf strings.Builder
	var finishReason string
	var usage *llm.Usage

	scanner := bufio.NewScanner(bodyReader)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

	// SSE idle watchdog: if no data line arrives within guiSSEIdleTimeout,
	// close the response body so scanner.Scan() unblocks. This prevents indefinite
	// hangs when the API establishes an SSE connection but stops sending data.
	idleTimer := time.NewTimer(guiSSEIdleTimeout)
	defer idleTimer.Stop()
	sseTimedOut := false
	watchdogDone := make(chan struct{})
	go func() {
		select {
		case <-idleTimer.C:
			sseTimedOut = true
			if metrics != nil {
				metrics.IdleTimeoutAfterToken = !metrics.FirstTokenAt.IsZero()
			}
			log.Printf("[LLM Stream] SSE idle timeout (%v)  - aborting stalled request", guiSSEIdleTimeout)
			resp.Body.Close() // unblocks scanner.Scan()
		case <-watchdogDone:
		case <-reqCtx.Done():
		}
	}()
	defer close(watchdogDone)

	firstSSEWaitStartedAt := time.Now()
	for scanner.Scan() {
		idleTimer.Reset(guiSSEIdleTimeout)
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		if metrics != nil && metrics.FirstSSEWaitNanos == 0 {
			metrics.FirstSSEWaitNanos = time.Since(firstSSEWaitStartedAt).Nanoseconds()
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}

		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]
		delta := choice.Delta

		if delta.ReasoningContent != "" {
			beforeReasoning := stripRolePrefixReasoningForDisplay(reasoningBuf.String())
			reasoningBuf.WriteString(delta.ReasoningContent)
			afterReasoning := stripRolePrefixReasoningForDisplay(reasoningBuf.String())
			// Forward only sanitized reasoning deltas. The \x01 prefix lets
			// the frontend distinguish thinking tokens from content tokens.
			if len(afterReasoning) > len(beforeReasoning) {
				onToken("\x01" + afterReasoning[len(beforeReasoning):])
			}
		}
		if delta.Content != "" {
			contentBuf.WriteString(delta.Content)
			tf.Write(delta.Content)
			// Early-terminate if the repetition filter detected degeneration.
			if repf.Halted() {
				log.Printf("[LLM Stream] repetition filter halted output (suppressed %d runes)", repf.SuppressedRunes())
				finishReason = llmFinishReasonStop.String()
				break
			}
			// Early-terminate if a role prefix hallucination was detected.
			if rpf.Halted() {
				log.Printf("[LLM Stream] role prefix filter halted output (suppressed %d runes)", rpf.SuppressedRunes())
				finishReason = llmFinishReasonStop.String()
				break
			}
		}
		for _, tc := range delta.ToolCalls {
			acc, ok := toolAccums[tc.Index]
			if !ok {
				acc = &toolAccum{}
				toolAccums[tc.Index] = acc
			}
			if tc.ID != "" {
				acc.id = tc.ID
			}
			if tc.Type != "" {
				acc.typ = tc.Type
			}
			if tc.Function.Name != "" {
				acc.name.WriteString(tc.Function.Name)
			}
			if tc.Function.Arguments != "" {
				acc.args.WriteString(tc.Function.Arguments)
				if acc.args.Len() > guiMaxToolArgumentsBytes {
					toolName := acc.name.String()
					if toolName == "" {
						toolName = fmt.Sprintf("tool_call_%d", tc.Index)
					}
					return nil, fmt.Errorf("tool arguments too large for %s: %d bytes exceeds limit %d", toolName, acc.args.Len(), guiMaxToolArgumentsBytes)
				}
			}
		}
		if choice.FinishReason != nil {
			finishReason = *choice.FinishReason
			log.Printf("[LLM Stream] finish_reason=%q received; closing SSE stream without waiting for trailing DONE/usage", finishReason)
			break
		}
	}

	// If the watchdog fired and we got no content, return a retryable error
	// so the agent loop's retry logic can re-attempt the request.
	if err := scanner.Err(); err != nil {
		if len(toolAccums) == 0 && contentBuf.Len() == 0 && reasoningBuf.Len() == 0 {
			return nil, fmt.Errorf("SSE stream read error: %w", err)
		}
	}
	if sseTimedOut && contentBuf.Len() == 0 && reasoningBuf.Len() == 0 && len(toolAccums) == 0 {
		if metrics != nil {
			metrics.IdleTimeoutCount++
		}
		return nil, fmt.Errorf("SSE stream idle timeout (%v): no data received from %s", guiSSEIdleTimeout, endpoint)
	}

	tf.Flush()
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
	content := stripXMLToolCalls(stripFunctionCalls(stripThinkTags(contentBuf.String())))
	// Use the filtered content (identical to what onToken received) as the
	// primary source for msg.Content. This ensures the backend response and
	// the frontend's streamed content are from the same data path.
	// Fall back to contentBuf only when filteredBuf is empty (e.g. all content
	// was inside <think> tags or was entirely tool calls).
	// Apply stripXMLToolCalls to filteredBuf too  - the stream filter chain
	// does not handle XML-formatted tool calls from models that emit tools in content.
	filteredStr := filteredBuf.String()
	if filteredStr != "" {
		content = stripXMLToolCalls(filteredStr)
	}

	// --- Browser diagnostic CP5: Stream filter results ---
	// Use rpf.Halted() as proxy for "raw content had Browser: prefix" to
	// avoid calling contentBuf.String() (which copies the entire buffer).
	// If rpf halted, it definitely found a Browser: prefix in the raw stream.
	// If rpf didn't halt but filteredBuf has Browser:, that's a filter bug.
	BrowserDiagCP5_StreamFilter(
		rpf.Halted(), rpf.SuppressedRunes(),
		repf.Halted(), repf.SuppressedRunes(),
		rpf.Halted(),
		browserDiagHasBrowserRolePrefix(filteredStr),
		filteredStr,
	)
	reasoning := stripRolePrefixReasoningForDisplay(reasoningBuf.String())
	if content == "" && reasoning != "" {
		content = stripXMLToolCalls(stripFunctionCalls(stripThinkTags(reasoning)))
	}
	msg := llm.Message{
		Role:             "assistant",
		Content:          content,
		ReasoningContent: reasoning,
	}
	if len(toolAccums) > 0 {
		maxIdx := 0
		for idx := range toolAccums {
			if idx > maxIdx {
				maxIdx = idx
			}
		}
		for i := 0; i <= maxIdx; i++ {
			if acc, ok := toolAccums[i]; ok {
				msg.ToolCalls = append(msg.ToolCalls, llm.ToolCall{
					ID: acc.id, Type: acc.typ,
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name: acc.name.String(), Arguments: acc.args.String(),
					},
				})
			}
		}
	}
	if len(msg.ToolCalls) == 0 {
		if xmlCalls := parseXMLContentToolCalls(contentBuf.String()); len(xmlCalls) > 0 {
			msg.ToolCalls = append(msg.ToolCalls, xmlCalls...)
			finishReason = llmFinishReasonToolCalls.String()
		}
	}

	if finishReason == "" {
		finishReason = llmFinishReasonStop.String()
	}

	// Detect and filter truncated tool calls caused by output token limit.
	finishReason, truncatedTools := filterTruncatedToolCalls(&msg, finishReason)

	return &llm.Response{
		Choices: []llm.Choice{{Message: msg, FinishReason: finishReason, TruncatedToolNames: truncatedTools}},
		Usage:   usage,
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
	endpoint, data, err := llm.BuildAnthropicMessagesRequestData(cfg, messages, llm.AnthropicMessagesRequestOptions{Stream: true, Tools: tools})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if metrics != nil {
		metrics.RequestBuildNanos += time.Since(requestBuildStartedAt).Nanoseconds()
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", cfg.UserAgent())
	req.Header.Set("anthropic-version", "2023-06-01")
	corelib.SetAnthropicAuthHeaders(req, cfg.Key)
	corelib.SetCodeGenClientNameHeaderIfNeededWithName(req, cfg.UserAgent())

	httpDoStartedAt := time.Now()
	resp, err := httpClient.Do(req)
	if metrics != nil {
		metrics.HTTPDoNanos += time.Since(httpDoStartedAt).Nanoseconds()
	}
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Provide friendly HTTP errors instead of raw HTML pages.
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		friendlyMsg := classifyOpenAIHTTPError(resp.StatusCode, body, cfg.ProviderName)
		return nil, fmt.Errorf("%s [url=%s model=%s protocol=anthropic]", friendlyMsg, endpoint, cfg.Model)
	}

	// Fallback: if provider doesn't return SSE
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/event-stream") {
		return parseNonStreamAnthropicResponse(resp, data)
	}

	// SSE parsing for Anthropic
	type blockAccum struct {
		blockType string // "text" or "tool_use"
		text      strings.Builder
		toolID    string
		toolName  string
		toolArgs  strings.Builder
	}
	blocks := make(map[int]*blockAccum)
	var stopReason string
	var usage *llm.Usage

	var filteredBufAnth strings.Builder
	filteredOnTokenAnth := func(delta string) {
		filteredBufAnth.WriteString(delta)
		onToken(delta)
	}
	rpfAnth := newRolePrefixStreamFilter(filteredOnTokenAnth)
	repfAnth := newRepetitionFilter(rpfAnth.Write)
	fcf := newFuncCallFilter(repfAnth.Write)
	tf := newThinkFilter(func(s string) { fcf.Write(s) })

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

	// SSE idle watchdog (same as OpenAI path).
	anthIdleTimer := time.NewTimer(guiSSEIdleTimeout)
	defer anthIdleTimer.Stop()
	anthTimedOut := false
	anthWatchdogDone := make(chan struct{})
	go func() {
		select {
		case <-anthIdleTimer.C:
			anthTimedOut = true
			if metrics != nil {
				metrics.IdleTimeoutAfterToken = !metrics.FirstTokenAt.IsZero()
			}
			log.Printf("[LLM Stream] Anthropic SSE idle timeout (%v)  - aborting stalled request", guiSSEIdleTimeout)
			resp.Body.Close()
		case <-anthWatchdogDone:
		case <-reqCtx.Done():
		}
	}()
	defer close(anthWatchdogDone)

	firstSSEWaitStartedAt := time.Now()
	for scanner.Scan() {
		anthIdleTimer.Reset(guiSSEIdleTimeout)
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		if metrics != nil && metrics.FirstSSEWaitNanos == 0 {
			metrics.FirstSSEWaitNanos = time.Since(firstSSEWaitStartedAt).Nanoseconds()
		}
		payload := strings.TrimPrefix(line, "data:")
		payload = strings.TrimPrefix(payload, " ")

		var evt struct {
			Type         string `json:"type"`
			Index        int    `json:"index"`
			ContentBlock struct {
				Type  string                 `json:"type"`
				ID    string                 `json:"id,omitempty"`
				Name  string                 `json:"name,omitempty"`
				Text  string                 `json:"text,omitempty"`
				Input map[string]interface{} `json:"input,omitempty"`
			} `json:"content_block,omitempty"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text,omitempty"`
				PartialJSON string `json:"partial_json,omitempty"`
				StopReason  string `json:"stop_reason,omitempty"`
			} `json:"delta,omitempty"`
			Message struct {
				StopReason string     `json:"stop_reason,omitempty"`
				Usage      *llm.Usage `json:"usage,omitempty"`
			} `json:"message,omitempty"`
			Usage *llm.Usage `json:"usage,omitempty"`
		}
		if err := json.Unmarshal([]byte(payload), &evt); err != nil {
			continue
		}

		switch normalizeAnthropicStreamEventType(evt.Type) {
		case anthropicStreamEventContentBlockStart:
			acc := &blockAccum{blockType: evt.ContentBlock.Type}
			blockKind := normalizeAnthropicContentBlockKind(evt.ContentBlock.Type)
			if blockKind == anthropicContentBlockText && evt.ContentBlock.Text != "" {
				acc.text.WriteString(evt.ContentBlock.Text)
				tf.Write(evt.ContentBlock.Text)
			}
			if blockKind == anthropicContentBlockToolUse {
				acc.toolID = evt.ContentBlock.ID
				acc.toolName = evt.ContentBlock.Name
			}
			blocks[evt.Index] = acc

		case anthropicStreamEventContentBlockDelta:
			acc, ok := blocks[evt.Index]
			if !ok {
				continue
			}
			deltaKind := normalizeAnthropicDeltaKind(evt.Delta.Type)
			if deltaKind == anthropicDeltaText && evt.Delta.Text != "" {
				acc.text.WriteString(evt.Delta.Text)
				tf.Write(evt.Delta.Text)
				// Early-terminate if the repetition filter detected degeneration.
				if repfAnth.Halted() {
					log.Printf("[LLM Stream Anthropic] repetition filter halted output (suppressed %d runes)", repfAnth.SuppressedRunes())
					stopReason = "end_turn"
					goto anthDone
				}
				// Early-terminate if a role prefix hallucination was detected.
				if rpfAnth.Halted() {
					log.Printf("[LLM Stream Anthropic] role prefix filter halted output (suppressed %d runes)", rpfAnth.SuppressedRunes())
					stopReason = "end_turn"
					goto anthDone
				}
			}
			if deltaKind == anthropicDeltaInputJSON && evt.Delta.PartialJSON != "" {
				acc.toolArgs.WriteString(evt.Delta.PartialJSON)
				if acc.toolArgs.Len() > guiMaxToolArgumentsBytes {
					toolName := acc.toolName
					if toolName == "" {
						toolName = fmt.Sprintf("tool_use_%d", evt.Index)
					}
					return nil, fmt.Errorf("tool arguments too large for %s: %d bytes exceeds limit %d", toolName, acc.toolArgs.Len(), guiMaxToolArgumentsBytes)
				}
			}

		case anthropicStreamEventMessageDelta:
			if evt.Delta.StopReason != "" {
				stopReason = evt.Delta.StopReason
			}
			// Anthropic sends output_tokens in message_delta.usage
			if evt.Usage != nil && usage != nil {
				usage.OutputTokens = evt.Usage.OutputTokens
				usage.CompletionTokens = evt.Usage.OutputTokens
				usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
			}

		case anthropicStreamEventMessageStop:
			// End of stream

		case "message_start":
			if evt.Message.StopReason != "" {
				stopReason = evt.Message.StopReason
			}
			// Anthropic sends input_tokens in message_start.message.usage
			if evt.Message.Usage != nil {
				usage = evt.Message.Usage
			}
		}
	}
anthDone:
	// Check for scanner errors (network interruption, etc.)
	if err := scanner.Err(); err != nil {
		if len(blocks) == 0 {
			return nil, fmt.Errorf("SSE stream read error: %w", err)
		}
	}
	// If the watchdog fired and we got no content, return a retryable error.
	if anthTimedOut && len(blocks) == 0 {
		if metrics != nil {
			metrics.IdleTimeoutCount++
		}
		return nil, fmt.Errorf("SSE stream idle timeout (%v): no data received from %s", guiSSEIdleTimeout, endpoint)
	}
	tf.Flush()
	fcf.Flush()
	repfAnth.Flush()
	rpfAnth.Flush()
	if repfAnth.Halted() {
		log.Printf("[LLM Stream Anthropic] repetition filter halted: suppressed %d runes", repfAnth.SuppressedRunes())
	}
	if rpfAnth.Halted() {
		log.Printf("[LLM Stream Anthropic] role prefix filter halted: suppressed %d runes", rpfAnth.SuppressedRunes())
	}

	// --- Browser diagnostic CP5 (Anthropic path) ---
	filteredStrAnth := filteredBufAnth.String()
	BrowserDiagCP5_StreamFilter(
		rpfAnth.Halted(), rpfAnth.SuppressedRunes(),
		repfAnth.Halted(), repfAnth.SuppressedRunes(),
		rpfAnth.Halted(),
		browserDiagHasBrowserRolePrefix(filteredStrAnth),
		filteredStrAnth,
	)

	// Assemble llm.Response
	msg := llm.Message{Role: "assistant"}
	var textParts []string
	// Iterate blocks in index order
	maxIdx := 0
	for idx := range blocks {
		if idx > maxIdx {
			maxIdx = idx
		}
	}
	for i := 0; i <= maxIdx; i++ {
		acc, ok := blocks[i]
		if !ok {
			continue
		}
		switch normalizeAnthropicContentBlockKind(acc.blockType) {
		case anthropicContentBlockText:
			textParts = append(textParts, acc.text.String())
		case anthropicContentBlockToolUse:
			msg.ToolCalls = append(msg.ToolCalls, llm.ToolCall{
				ID:   acc.toolID,
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name:      acc.toolName,
					Arguments: acc.toolArgs.String(),
				},
			})
		}
	}
	msg.Content = stripFunctionCalls(stripThinkTags(strings.Join(textParts, "\n")))
	if filteredStrAnth != "" {
		msg.Content = stripXMLToolCalls(filteredStrAnth)
	}

	finishReason := llmFinishReasonStop.String()
	if normalizeAnthropicContentBlockKind(stopReason) == anthropicContentBlockToolUse {
		finishReason = llmFinishReasonToolCalls.String()
	} else if stopReason == "max_tokens" {
		finishReason = llmFinishReasonLength.String()
	}

	// Detect and filter truncated tool calls caused by output token limit.
	finishReason, truncatedTools := filterTruncatedToolCalls(&msg, finishReason)

	return &llm.Response{
		Choices: []llm.Choice{{Message: msg, FinishReason: finishReason, TruncatedToolNames: truncatedTools}},
		Usage:   usage,
	}, nil
}

// parseNonStreamAnthropicResponse handles the fallback case where the provider
// returned a normal JSON response instead of SSE for Anthropic protocol.
func parseNonStreamAnthropicResponse(resp *http.Response, requestBody []byte) (*llm.Response, error) {
	return llm.ParseNonStreamAnthropicResponse(resp)
}
