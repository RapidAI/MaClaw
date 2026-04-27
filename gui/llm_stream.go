package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/freeproxy"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

type tokenStreamFilter struct {
	writeFn func(string)
	flushFn func()
}

var guiFuncCallBlock = regexp.MustCompile(`(?s)<\|FunctionCallBegin\|>.*?<\|FunctionCallEnd\|>\s*`)

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
//  1. JSON parse failure — arguments string is not valid JSON (truncated mid-token)
//  2. Required field missing — JSON parses OK but a required field is absent,
//     indicating the model ran out of output tokens before generating all fields.
//     This happens when a large field (e.g. write_file content) consumes the
//     entire output budget, leaving no room for subsequent fields like path.
//
// Truncated tool calls are removed and a hint is appended to msg.Content so
// the LLM learns to produce shorter arguments on the next iteration.
// Returns the (possibly modified) finishReason.
func filterTruncatedToolCalls(msg *llm.Message, finishReason string) string {
	if len(msg.ToolCalls) == 0 {
		return finishReason
	}

	// Primary signal: finish_reason="length" means the model hit max_output_tokens.
	isLengthTruncated := finishReason == "length"

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
			// Case 1: JSON parse failure — the arguments are not valid JSON.
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
			// when hitting max_output_tokens — still treat as truncation.
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
		return finishReason
	}
	msg.ToolCalls = validCalls
	hint := fmt.Sprintf("\n\n[系统提示] 以下工具调用的参数不完整（被截断或缺少必需字段）：%s。"+
		"请将大文件内容拆分为多次写入（每次不超过 5000 字符），或使用 bash 工具通过脚本写入。",
		strings.Join(truncatedNames, ", "))
	msg.Content += hint
	if len(msg.ToolCalls) == 0 {
		return "stop"
	}
	return finishReason
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
// This is NOT a general parameter validation — it specifically detects the
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

// classifyOpenAIHTTPError 解析 OpenAI API 错误响应，返回友好的中文提示。
func classifyOpenAIHTTPError(statusCode int, body []byte) string {
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

	// Hub wraps upstream provider auth failures as LLM_UPSTREAM_AUTH_FAILED
	// and rate limits as LLM_UPSTREAM_RATE_LIMITED with descriptive Chinese
	// messages. Surface them directly so the user (or admin) knows the
	// problem is the upstream provider, not their own credentials.
	if (code == "LLM_UPSTREAM_AUTH_FAILED" || code == "LLM_UPSTREAM_RATE_LIMITED") && msg != "" {
		return msg
	}

	switch {
	case code == "insufficient_quota" || typ == "insufficient_quota":
		return "OpenAI 账号额度不足，请检查账单和付费计划 (insufficient_quota)"
	case statusCode == http.StatusTooManyRequests:
		if strings.Contains(string(body), "rate_limit") {
			return "OpenAI API 请求频率超限，请稍后再试 (rate_limit)"
		}
		return "OpenAI API 请求过于频繁，请稍后再试 (HTTP 429)"
	case statusCode == http.StatusUnauthorized:
		return "OpenAI 认证失败，API Key 无效或已过期，请重新登录 (HTTP 401)"
	case statusCode == http.StatusForbidden:
		return "OpenAI 拒绝访问，账号可能被限制或无权使用该模型 (HTTP 403)"
	case statusCode == http.StatusBadGateway:
		// Hub wraps upstream provider errors with specific error codes.
		if code == "LLM_UPSTREAM_AUTH_FAILED" || code == "LLM_UPSTREAM_FAILED" || code == "LLM_UPSTREAM_RATE_LIMITED" {
			if msg != "" {
				return msg
			}
		}
		return "API 网关错误，上游服务不可用，请稍后再试 (HTTP 502)"
	case statusCode == http.StatusServiceUnavailable:
		return "API 服务暂时不可用，请稍后再试 (HTTP 503)"
	case statusCode == http.StatusGatewayTimeout:
		return "API 网关超时，上游服务响应过慢，请稍后再试 (HTTP 504)"
	case statusCode >= 500:
		return fmt.Sprintf("API 服务端错误，请稍后再试 (HTTP %d)", statusCode)
	default:
		return fmt.Sprintf("OpenAI API 错误 (HTTP %d): %s", statusCode, truncateLLMBody(body, 200))
	}
}

// truncateLLMBody 截断错误 body 用于日志显示。
// 如果 body 包含 HTML 标签，先剥离标签再截断，避免原始 HTML 透传到用户界面。
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
	// Always use the streaming path even when onToken is nil (e.g. WeChat IM
	// standalone mode). The non-streaming DoOpenAIRequest path uses io.ReadAll
	// which blocks until the entire SSE stream finishes — causing multi-minute
	// delays when the API returns SSE despite stream:false. A noop callback
	// lets us stream incrementally and discard tokens we don't need to display.
	if onToken == nil {
		onToken = func(string) {}
	}
	if cfg.IsResponsesWebSocket() {
		return h.doResponsesWSLLMRequestStream(reqCtx, cfg, messages, tools, httpClient, withFirstTokenMetrics(onToken, metrics), metrics)
	}
	if cfg.IsResponsesAPI() {
		return h.doResponsesAPILLMRequestStream(reqCtx, cfg, messages, tools, httpClient, withFirstTokenMetrics(onToken, metrics), metrics)
	}
	if cfg.Protocol == "anthropic" {
		return h.doAnthropicLLMRequestStream(reqCtx, cfg, messages, tools, httpClient, withFirstTokenMetrics(onToken, metrics), metrics)
	}
	return h.doOpenAILLMRequestStream(reqCtx, cfg, messages, tools, httpClient, withFirstTokenMetrics(onToken, metrics), metrics)
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
	log.Printf("[LLM Stream] POST %s model=%s protocol=%s", endpoint, cfg.Model, cfg.Protocol)

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
		log.Printf("[LLM Stream] HTTP 404: endpoint=%s content_type=%q body=%s", endpoint, resp.Header.Get("Content-Type"), truncateLLMBody(body, 500))
		return nil, fmt.Errorf("HTTP 404: %s (endpoint=%s, model=%s, protocol=%s)", truncateLLMBody(body, 200), endpoint, cfg.Model, cfg.Protocol)
	}

	// 对已知 HTTP 错误提供友好提示，避免原始 HTML/JSON body 直接透传给用户。
	// 覆盖 4xx（客户端错误，404 已单独处理）和 5xx（网关/服务端错误）。
	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		log.Printf("[LLM Stream] HTTP %d: endpoint=%s content_type=%q body=%s", resp.StatusCode, endpoint, resp.Header.Get("Content-Type"), truncateLLMBody(body, 500))
		friendlyMsg := classifyOpenAIHTTPError(resp.StatusCode, body)
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
		// bodyReader includes the peeked bytes via MultiReader — we must
		// read from it (not resp.Body) to get the complete response body.
		body, _ := io.ReadAll(io.LimitReader(bodyReader, 256*1024))
		parsed, parseErr := llm.ParseNonStreamOpenAIResponseBody(body)
		if parseErr != nil {
			snippet := string(body)
			if len(snippet) > 500 {
				snippet = snippet[:500] + "..."
			}
			log.Printf("[LLM Stream] non-SSE parse failed: content_type=%q body_len=%d err=%v body=%s", contentType, len(body), parseErr, snippet)
		}
		return parsed, parseErr
	}

	// filteredBuf accumulates the content that actually reaches onToken
	// (after all stream filters). msg.Content reads from filteredBuf so
	// the backend response is identical to what the frontend received via
	// streaming — eliminating the data-flow fork between contentBuf (raw)
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
			log.Printf("[LLM Stream] SSE idle timeout (%v) — aborting stalled request", guiSSEIdleTimeout)
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
			reasoningBuf.WriteString(delta.ReasoningContent)
		}
		if delta.Content != "" {
			contentBuf.WriteString(delta.Content)
			tf.Write(delta.Content)
			// Early-terminate if the repetition filter detected degeneration.
			if repf.Halted() {
				log.Printf("[LLM Stream] repetition filter halted output (suppressed %d runes)", repf.SuppressedRunes())
				finishReason = "stop"
				break
			}
			// Early-terminate if a role prefix hallucination was detected.
			if rpf.Halted() {
				log.Printf("[LLM Stream] role prefix filter halted output (suppressed %d runes)", rpf.SuppressedRunes())
				finishReason = "stop"
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
	// Apply stripXMLToolCalls to filteredBuf too — the stream filter chain
	// does not handle XML-formatted tool calls from free proxy models.
	if filtered := filteredBuf.String(); filtered != "" {
		content = stripXMLToolCalls(filtered)
	}
	reasoning := reasoningBuf.String()
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
		if xmlCalls := freeproxy.ParseXMLToolCalls(contentBuf.String()); len(xmlCalls) > 0 {
			for _, xc := range xmlCalls {
				msg.ToolCalls = append(msg.ToolCalls, llm.ToolCall{
					ID: xc.ID, Type: xc.Type,
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name: xc.Function.Name, Arguments: xc.Function.Arguments,
					},
				})
			}
			finishReason = "tool_calls"
		}
	}

	if finishReason == "" {
		finishReason = "stop"
	}

	// Detect and filter truncated tool calls caused by output token limit.
	finishReason = filterTruncatedToolCalls(&msg, finishReason)

	return &llm.Response{
		Choices: []llm.Choice{{Message: msg, FinishReason: finishReason}},
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
	endpoint := corelib.AnthropicMessagesEndpoint(cfg.URL)

	converted := convertToAnthropicMessages(messages)

	reqBody := map[string]interface{}{
		"model":      cfg.Model,
		"messages":   converted.Messages,
		"max_tokens": 4096,
		"stream":     true,
	}
	if converted.SystemText != "" {
		reqBody["system"] = converted.SystemText
	}
	if len(tools) > 0 {
		if at := convertToAnthropicTools(tools); len(at) > 0 {
			reqBody["tools"] = at
		}
	}

	data, _ := json.Marshal(reqBody)
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

	httpDoStartedAt := time.Now()
	resp, err := httpClient.Do(req)
	if metrics != nil {
		metrics.HTTPDoNanos += time.Since(httpDoStartedAt).Nanoseconds()
	}
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// 对 HTTP 错误提供友好提示，避免 HTML 错误页面透传到聊天界面。
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		friendlyMsg := classifyOpenAIHTTPError(resp.StatusCode, body)
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
			log.Printf("[LLM Stream] Anthropic SSE idle timeout (%v) — aborting stalled request", guiSSEIdleTimeout)
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
				StopReason string `json:"stop_reason,omitempty"`
				Usage      *struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage,omitempty"`
			} `json:"message,omitempty"`
			Usage *struct {
				OutputTokens int `json:"output_tokens"`
			} `json:"usage,omitempty"`
		}
		if err := json.Unmarshal([]byte(payload), &evt); err != nil {
			continue
		}

		switch evt.Type {
		case "content_block_start":
			acc := &blockAccum{blockType: evt.ContentBlock.Type}
			if evt.ContentBlock.Type == "text" && evt.ContentBlock.Text != "" {
				acc.text.WriteString(evt.ContentBlock.Text)
				tf.Write(evt.ContentBlock.Text)
			}
			if evt.ContentBlock.Type == "tool_use" {
				acc.toolID = evt.ContentBlock.ID
				acc.toolName = evt.ContentBlock.Name
			}
			blocks[evt.Index] = acc

		case "content_block_delta":
			acc, ok := blocks[evt.Index]
			if !ok {
				continue
			}
			if evt.Delta.Type == "text_delta" && evt.Delta.Text != "" {
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
			if evt.Delta.Type == "input_json_delta" && evt.Delta.PartialJSON != "" {
				acc.toolArgs.WriteString(evt.Delta.PartialJSON)
				if acc.toolArgs.Len() > guiMaxToolArgumentsBytes {
					toolName := acc.toolName
					if toolName == "" {
						toolName = fmt.Sprintf("tool_use_%d", evt.Index)
					}
					return nil, fmt.Errorf("tool arguments too large for %s: %d bytes exceeds limit %d", toolName, acc.toolArgs.Len(), guiMaxToolArgumentsBytes)
				}
			}

		case "message_delta":
			if evt.Delta.StopReason != "" {
				stopReason = evt.Delta.StopReason
			}
			// Anthropic sends output_tokens in message_delta.usage
			if evt.Usage != nil && usage != nil {
				usage.OutputTokens = evt.Usage.OutputTokens
				usage.CompletionTokens = evt.Usage.OutputTokens
				usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
			}

		case "message_stop":
			// End of stream

		case "message_start":
			if evt.Message.StopReason != "" {
				stopReason = evt.Message.StopReason
			}
			// Anthropic sends input_tokens in message_start.message.usage
			if evt.Message.Usage != nil {
				usage = &llm.Usage{
					InputTokens:  evt.Message.Usage.InputTokens,
					PromptTokens: evt.Message.Usage.InputTokens,
				}
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
		switch acc.blockType {
		case "text":
			textParts = append(textParts, acc.text.String())
		case "tool_use":
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
	if filtered := filteredBufAnth.String(); filtered != "" {
		msg.Content = stripXMLToolCalls(filtered)
	}

	finishReason := "stop"
	if stopReason == "tool_use" {
		finishReason = "tool_calls"
	} else if stopReason == "max_tokens" {
		finishReason = "length"
	}

	return &llm.Response{
		Choices: []llm.Choice{{Message: msg, FinishReason: finishReason}},
		Usage:   usage,
	}, nil
}

// parseNonStreamAnthropicResponse handles the fallback case where the provider
// returned a normal JSON response instead of SSE for Anthropic protocol.
func parseNonStreamAnthropicResponse(resp *http.Response, requestBody []byte) (*llm.Response, error) {
	return llm.ParseNonStreamAnthropicResponse(resp)
}
