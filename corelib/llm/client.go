package llm

// Unified OpenAI-compatible LLM HTTP client.
// All packages (gui, tui, hub/corelib/agent) should use these functions
// instead of implementing their own request/response logic.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

const maxToolArgumentsBytes = 180 * 1024

// HTTPStatusError carries an LLM HTTP error body for structured callers while
// keeping Error() body-free so logs and UI messages do not echo sensitive data.
type HTTPStatusError struct {
	StatusCode int
	Body       []byte
}

func (e *HTTPStatusError) Error() string {
	if e == nil {
		return "HTTP error"
	}
	return fmt.Sprintf("HTTP %d: body_len=%d", e.StatusCode, len(e.Body))
}

// OpenAIChatRequestOptions controls how an OpenAI-compatible chat/completions
// request is built.
type OpenAIChatRequestOptions struct {
	Stream         bool
	Tools          []map[string]interface{}
	ExtraBody      map[string]interface{}
	PassThrough    map[string]interface{}
	ToolChoice     interface{}
	ResponseFormat interface{}
}

var openAIChatPassThroughKeys = []string{
	"temperature",
	"top_p",
	"max_tokens",
	"max_completion_tokens",
	"presence_penalty",
	"frequency_penalty",
	"stop",
	"parallel_tool_calls",
	"user",
	"seed",
	"n",
	"logprobs",
	"top_logprobs",
	"stream_options",
	"metadata",
	"store",
}

func buildOpenAIChatRequestBody(
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	opts OpenAIChatRequestOptions,
) map[string]interface{} {
	// Provider-specific message adaptation
	if corelib.NeedsSystemMerge(cfg) {
		messages = corelib.MergeSystemIntoUser(messages)
	}
	if cfg.NeedsConservativeOpenAICompatSanitization() {
		messages = sanitizeConservativeOpenAICompatMessages(messages)
	}
	messages = sanitizeEmptyToolCalls(messages)
	messages = sanitizeInvalidToolCallArguments(messages)
	messages = sanitizeOrphanedToolCalls(messages)

	model := cfg.UpstreamModel()
	reqBody := map[string]interface{}{
		"model":    model,
		"messages": messages,
		"stream":   opts.Stream,
	}
	if len(opts.Tools) > 0 {
		if cfg.NeedsConservativeOpenAICompatSanitization() {
			reqBody["tools"] = corelib.SanitizeCodeGenOpenAIChatTools(opts.Tools)
		} else {
			reqBody["tools"] = opts.Tools
		}
	}
	if opts.ToolChoice != nil {
		reqBody["tool_choice"] = opts.ToolChoice
	}
	if opts.ResponseFormat != nil {
		reqBody["response_format"] = opts.ResponseFormat
	}
	for _, k := range openAIChatPassThroughKeys {
		if opts.PassThrough != nil {
			if v, ok := opts.PassThrough[k]; ok {
				reqBody[k] = v
			}
		}
	}
	for k, v := range opts.ExtraBody {
		switch k {
		case "model", "messages", "stream", "tools", "tool_choice", "response_format":
			continue
		default:
			reqBody[k] = v
		}
	}
	if cfg.NeedsConservativeOpenAICompatSanitization() {
		corelib.SanitizeCodeGenOpenAICompatBody(reqBody)
	}
	return reqBody
}

func BuildOpenAIChatRequestData(
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	opts OpenAIChatRequestOptions,
) (endpoint string, body []byte, err error) {
	endpoint = BuildOpenAIChatCompletionsEndpoint(cfg.URL)
	body, err = json.Marshal(buildOpenAIChatRequestBody(cfg, messages, opts))
	return endpoint, body, err
}

func BuildOpenAIChatCompletionsEndpoint(rawURL string) string {
	endpoint := strings.TrimRight(strings.TrimSpace(rawURL), "/")
	if strings.HasSuffix(endpoint, "/chat/completions") {
		return endpoint
	}
	if strings.HasSuffix(endpoint, "/v1") {
		return endpoint + "/chat/completions"
	}
	return endpoint + "/v1/chat/completions"
}

func NewOpenAIChatRequest(
	ctx context.Context,
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	opts OpenAIChatRequestOptions,
) (*http.Request, []byte, string, error) {
	endpoint, data, err := BuildOpenAIChatRequestData(cfg, messages, opts)
	if err != nil {
		return nil, nil, endpoint, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, nil, endpoint, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", cfg.UserAgent())
	if cfg.Key != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Key)
	}
	corelib.SetCodeGenClientNameHeaderIfNeededWithName(req, cfg.UserAgent())
	return req, data, endpoint, nil
}

func SummarizeOpenAIChatRequestBody(body []byte) string {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Sprintf("invalid_json req_len=%d", len(body))
	}
	messagesLen := jsonArrayLen(payload["messages"])
	toolsLen := jsonArrayLen(payload["tools"])
	functionsLen := jsonArrayLen(payload["functions"])
	_, hasStreamOptions := payload["stream_options"]
	_, hasToolChoice := payload["tool_choice"]
	_, hasResponseFormat := payload["response_format"]
	return fmt.Sprintf("req_len=%d model=%q stream=%v messages=%d tools=%d functions=%d stream_options=%t tool_choice=%t response_format=%t",
		len(body), payload["model"], payload["stream"], messagesLen, toolsLen, functionsLen, hasStreamOptions, hasToolChoice, hasResponseFormat)
}

func jsonArrayLen(value interface{}) int {
	switch v := value.(type) {
	case []interface{}:
		return len(v)
	case []map[string]interface{}:
		return len(v)
	default:
		return 0
	}
}

func sanitizeConservativeOpenAICompatMessages(messages []interface{}) []interface{} {
	normalized := make([]interface{}, 0, len(messages))
	for _, message := range messages {
		switch m := message.(type) {
		case map[string]interface{}:
			_, hasReasoning := m["reasoning_content"]
			contentIsNil := false
			if content, ok := m["content"]; ok && content == nil {
				contentIsNil = true
			}
			if !hasReasoning && !contentIsNil {
				normalized = append(normalized, message)
				continue
			}
			patched := make(map[string]interface{}, len(m))
			for k, v := range m {
				if k == "reasoning_content" {
					continue
				}
				if k == "content" && v == nil {
					patched[k] = ""
					continue
				}
				patched[k] = v
			}
			normalized = append(normalized, patched)
		case map[string]string:
			if _, ok := m["reasoning_content"]; !ok {
				normalized = append(normalized, message)
				continue
			}
			patched := make(map[string]string, len(m)-1)
			for k, v := range m {
				if k == "reasoning_content" {
					continue
				}
				patched[k] = v
			}
			normalized = append(normalized, patched)
		default:
			normalized = append(normalized, message)
		}
	}
	return normalized
}

func SanitizeConservativeOpenAICompatMessages(messages []interface{}) []interface{} {
	messages = sanitizeConservativeOpenAICompatMessages(messages)
	return sanitizeEmptyToolCalls(messages)
}

func sanitizeEmptyToolCalls(messages []interface{}) []interface{} {
	normalized := make([]interface{}, 0, len(messages))
	for _, message := range messages {
		if !hasEmptyToolCalls(message) {
			normalized = append(normalized, message)
			continue
		}
		normalized = append(normalized, copyMapWithout(message, "tool_calls"))
	}
	return normalized
}

func hasEmptyToolCalls(message interface{}) bool {
	m, ok := toMapInterface(message)
	if !ok {
		return false
	}
	raw, exists := m["tool_calls"]
	if !exists || raw == nil {
		return false
	}
	switch v := raw.(type) {
	case []interface{}:
		return len(v) == 0
	case []ToolCall:
		return len(v) == 0
	case []map[string]interface{}:
		return len(v) == 0
	default:
		data, err := json.Marshal(v)
		return err == nil && string(data) == "[]"
	}
}

func sanitizeInvalidToolCallArguments(messages []interface{}) []interface{} {
	normalized := make([]interface{}, 0, len(messages))
	for _, m := range messages {
		mm, ok := m.(map[string]interface{})
		if !ok {
			normalized = append(normalized, m)
			continue
		}
		role, _ := mm["role"].(string)
		if role != "assistant" {
			normalized = append(normalized, m)
			continue
		}
		patchedToolCalls, changed := sanitizeToolCallInvalidArguments(mm["tool_calls"])
		if !changed {
			normalized = append(normalized, m)
			continue
		}

		patched := make(map[string]interface{}, len(mm))
		for k, v := range mm {
			patched[k] = v
		}
		patched["tool_calls"] = patchedToolCalls
		normalized = append(normalized, patched)
	}
	return normalized
}

func sanitizeToolCallInvalidArguments(raw interface{}) (interface{}, bool) {
	switch toolCalls := raw.(type) {
	case []ToolCall:
		if len(toolCalls) == 0 {
			return raw, false
		}
		patchedCalls := make([]ToolCall, len(toolCalls))
		copy(patchedCalls, toolCalls)
		changed := false
		for i := range patchedCalls {
			args := strings.TrimSpace(patchedCalls[i].Function.Arguments)
			if args == "" || !json.Valid([]byte(args)) {
				patchedCalls[i].Function.Arguments = "{}"
				changed = true
			}
		}
		if !changed {
			return raw, false
		}
		return patchedCalls, true
	case []interface{}:
		if len(toolCalls) == 0 {
			return raw, false
		}
		patchedCalls := make([]interface{}, 0, len(toolCalls))
		changed := false
		for _, call := range toolCalls {
			callMap, ok := call.(map[string]interface{})
			if !ok {
				patchedCalls = append(patchedCalls, call)
				continue
			}
			patchedCall, patched := sanitizeToolCallMapInvalidArguments(callMap)
			if !patched {
				patchedCalls = append(patchedCalls, call)
				continue
			}
			patchedCalls = append(patchedCalls, patchedCall)
			changed = true
		}
		if !changed {
			return raw, false
		}
		return patchedCalls, true
	case []map[string]interface{}:
		if len(toolCalls) == 0 {
			return raw, false
		}
		patchedCalls := make([]map[string]interface{}, len(toolCalls))
		changed := false
		for i, call := range toolCalls {
			patchedCall, patched := sanitizeToolCallMapInvalidArguments(call)
			patchedCalls[i] = patchedCall
			if patched {
				changed = true
			}
		}
		if !changed {
			return raw, false
		}
		return patchedCalls, true
	default:
		return raw, false
	}
}

func sanitizeToolCallMapInvalidArguments(callMap map[string]interface{}) (map[string]interface{}, bool) {
	fn, ok := callMap["function"].(map[string]interface{})
	if !ok {
		return callMap, false
	}
	args, _ := fn["arguments"].(string)
	trimmed := strings.TrimSpace(args)
	if trimmed != "" && json.Valid([]byte(trimmed)) {
		return callMap, false
	}
	patchedCall := make(map[string]interface{}, len(callMap))
	for k, v := range callMap {
		patchedCall[k] = v
	}
	patchedFn := make(map[string]interface{}, len(fn))
	for k, v := range fn {
		patchedFn[k] = v
	}
	patchedFn["arguments"] = "{}"
	patchedCall["function"] = patchedFn
	return patchedCall, true
}

// sanitizeOrphanedToolCalls detects assistant messages with tool_calls that
// are NOT followed by the corresponding tool result messages, and strips the
// tool_calls field from those orphaned messages. This prevents HTTP 400 errors
// from providers like DeepSeek that strictly enforce:
//
//	"An assistant message with 'tool_calls' must be followed by tool messages
//	 responding to each 'tool_call_id'."
//
// This is a compat/migration layer for persisted conversation histories that
// were corrupted by a bug in trimHistoryWithSummary (missing group-align on
// the second-pass recentStart calculation). The root cause is fixed in
// gui/im_conversation_trim.go  - new compactions will not produce orphans.
// This function handles pre-existing corrupted data gracefully.
func sanitizeOrphanedToolCalls(messages []interface{}) []interface{} {
	if len(messages) == 0 {
		return messages
	}

	// Scan for assistant messages with tool_calls and verify each has
	// corresponding tool messages immediately following.
	// Only flag as orphaned if there IS a following message that isn't a tool
	// message  - if the assistant(tool_calls) is the last message, the API
	// expects tool results to come in the next request (normal flow).
	needsFix := false
	for i, m := range messages {
		if !msgIsAssistantWithToolCalls(m) {
			continue
		}
		tcIDs := extractToolCallIDs(m)
		if len(tcIDs) == 0 {
			continue
		}
		// Collect tool_call_ids from immediately following tool messages.
		j := i + 1
		for j < len(messages) {
			mm, ok := toMapInterface(messages[j])
			if !ok {
				break
			}
			if role, _ := mm["role"].(string); role != "tool" {
				break
			}
			j++
		}
		// If we reached the end of messages, this is the last group  - not orphaned.
		if j >= len(messages) {
			continue
		}
		// There are messages after the tool group. Check if all tool_call_ids
		// have corresponding tool messages.
		foundIDs := make(map[string]bool)
		for k := i + 1; k < j; k++ {
			mm, ok := toMapInterface(messages[k])
			if !ok {
				break
			}
			if tcID, _ := mm["tool_call_id"].(string); tcID != "" {
				foundIDs[tcID] = true
			}
		}
		for _, id := range tcIDs {
			if !foundIDs[id] {
				needsFix = true
				break
			}
		}
		if needsFix {
			break
		}
	}

	if !needsFix {
		return messages
	}

	// Build a fixed copy, stripping tool_calls from orphaned assistant messages.
	result := make([]interface{}, 0, len(messages))
	for i, m := range messages {
		if !msgIsAssistantWithToolCalls(m) {
			result = append(result, m)
			continue
		}
		tcIDs := extractToolCallIDs(m)
		if len(tcIDs) == 0 {
			result = append(result, m)
			continue
		}
		// Find the end of the following tool messages.
		j := i + 1
		for j < len(messages) {
			mm, ok := toMapInterface(messages[j])
			if !ok {
				break
			}
			if role, _ := mm["role"].(string); role != "tool" {
				break
			}
			j++
		}
		// If this is the last group (no messages after), keep as-is.
		if j >= len(messages) {
			result = append(result, m)
			continue
		}
		// Check following tool messages for completeness.
		foundIDs := make(map[string]bool)
		for k := i + 1; k < j; k++ {
			mm, ok := toMapInterface(messages[k])
			if !ok {
				break
			}
			if tcID, _ := mm["tool_call_id"].(string); tcID != "" {
				foundIDs[tcID] = true
			}
		}
		allPresent := true
		for _, id := range tcIDs {
			if !foundIDs[id] {
				allPresent = false
				break
			}
		}
		if allPresent {
			result = append(result, m)
			continue
		}
		// Orphaned: strip tool_calls from a copy of the message.
		log.Printf("[LLM sanitize] stripping orphaned tool_calls from assistant message at index %d (ids=%v)", i, tcIDs)
		patched := copyMapWithout(m, "tool_calls")
		result = append(result, patched)
	}
	return result
}

// msgIsAssistantWithToolCalls checks if a message is an assistant message
// with a non-empty tool_calls field.
func msgIsAssistantWithToolCalls(m interface{}) bool {
	mm, ok := toMapInterface(m)
	if !ok {
		return false
	}
	role, _ := mm["role"].(string)
	if role != "assistant" {
		return false
	}
	tc := mm["tool_calls"]
	if tc == nil {
		return false
	}
	switch v := tc.(type) {
	case []interface{}:
		return len(v) > 0
	case []ToolCall:
		return len(v) > 0
	default:
		// For other slice types, marshal to check.
		data, err := json.Marshal(v)
		if err != nil {
			return false
		}
		return len(data) > 2 // "[]" is 2 bytes
	}
}

// extractToolCallIDs extracts tool_call IDs from an assistant message.
func extractToolCallIDs(m interface{}) []string {
	mm, ok := toMapInterface(m)
	if !ok {
		return nil
	}
	tc := mm["tool_calls"]
	if tc == nil {
		return nil
	}
	switch v := tc.(type) {
	case []ToolCall:
		ids := make([]string, 0, len(v))
		for _, call := range v {
			if call.ID != "" {
				ids = append(ids, call.ID)
			}
		}
		return ids
	case []interface{}:
		ids := make([]string, 0, len(v))
		for _, call := range v {
			callMap, ok := call.(map[string]interface{})
			if !ok {
				continue
			}
			if id, _ := callMap["id"].(string); id != "" {
				ids = append(ids, id)
			}
		}
		return ids
	default:
		return nil
	}
}

// toMapInterface converts a message to map[string]interface{} if possible.
func toMapInterface(m interface{}) (map[string]interface{}, bool) {
	switch v := m.(type) {
	case map[string]interface{}:
		return v, true
	case map[string]string:
		// Convert map[string]string to map[string]interface{} for uniform access.
		result := make(map[string]interface{}, len(v))
		for k, val := range v {
			result[k] = val
		}
		return result, true
	default:
		return nil, false
	}
}

// copyMapWithout creates a shallow copy of a message map, excluding the
// specified key.
func copyMapWithout(m interface{}, excludeKey string) interface{} {
	switch v := m.(type) {
	case map[string]interface{}:
		patched := make(map[string]interface{}, len(v))
		for k, val := range v {
			if k == excludeKey {
				continue
			}
			patched[k] = val
		}
		return patched
	case map[string]string:
		patched := make(map[string]string, len(v))
		for k, val := range v {
			if k == excludeKey {
				continue
			}
			patched[k] = val
		}
		return patched
	default:
		return m
	}
}

// DoOpenAIRequest sends a non-streaming OpenAI-compatible chat completion
// request. It handles provider quirks (e.g. MiniMax system-role merge)
// in one place so callers don't need to worry about them.
//
// The caller provides a context for cancellation/timeout control.
// tools may be nil for simple requests without tool calling.
func DoOpenAIRequest(
	ctx context.Context,
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	tools []map[string]interface{},
	client *http.Client,
) (*Response, error) {
	result, _, err := DoOpenAIRequestRaw(ctx, cfg, messages, tools, client)
	return result, err
}

func DoOpenAIRequestRaw(
	ctx context.Context,
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	tools []map[string]interface{},
	client *http.Client,
) (*Response, []byte, error) {
	req, reqBody, endpoint, err := NewOpenAIChatRequest(ctx, cfg, messages, OpenAIChatRequestOptions{
		Stream: false,
		Tools:  tools,
	})
	if err != nil {
		return nil, nil, err
	}
	traceFields := RequestTraceLogFields(ctx)
	upstreamModel := cfg.UpstreamModel()
	log.Printf("[LLM] POST %s model=%s configured_model=%s protocol=%s %s", endpoint, upstreamModel, cfg.Model, cfg.Protocol, traceFields)

	startedAt := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[LLM] done %s model=%s configured_model=%s protocol=%s status=error elapsed=%s err=%v %s", endpoint, upstreamModel, cfg.Model, cfg.Protocol, time.Since(startedAt).Round(time.Millisecond), err, traceFields)
		return nil, nil, fmt.Errorf("[%s] %w", endpoint, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	requestSummary := ""
	if resp.StatusCode != http.StatusOK {
		requestSummary = " request=" + SummarizeOpenAIChatRequestBody(reqBody)
	}
	log.Printf("[LLM] done %s model=%s configured_model=%s protocol=%s status=%d elapsed=%s body_len=%d%s %s", endpoint, upstreamModel, cfg.Model, cfg.Protocol, resp.StatusCode, time.Since(startedAt).Round(time.Millisecond), len(body), requestSummary, traceFields)
	if resp.StatusCode != http.StatusOK {
		if ShouldRetryOpenAIWithoutTools(cfg, resp.StatusCode, messages, tools) {
			log.Printf("[LLM] retry_without_tools %s model=%s configured_model=%s reason=conservative_openai_compat_400 tools=%d %s", endpoint, upstreamModel, cfg.Model, len(tools), traceFields)
			req, reqBody, endpoint, err = NewOpenAIChatRequest(ctx, cfg, messages, OpenAIChatRequestOptions{Stream: false})
			if err != nil {
				return nil, body, err
			}
			retryStartedAt := time.Now()
			resp, err = client.Do(req)
			if err != nil {
				log.Printf("[LLM] retry_without_tools done %s model=%s configured_model=%s status=error elapsed=%s err=%v %s", endpoint, upstreamModel, cfg.Model, time.Since(retryStartedAt).Round(time.Millisecond), err, traceFields)
				return nil, body, fmt.Errorf("[%s] %w", endpoint, err)
			}
			defer resp.Body.Close()
			body, _ = io.ReadAll(io.LimitReader(resp.Body, 512*1024))
			requestSummary = ""
			if resp.StatusCode != http.StatusOK {
				requestSummary = " request=" + SummarizeOpenAIChatRequestBody(reqBody)
			}
			log.Printf("[LLM] retry_without_tools done %s model=%s configured_model=%s protocol=%s status=%d elapsed=%s body_len=%d%s %s", endpoint, upstreamModel, cfg.Model, cfg.Protocol, resp.StatusCode, time.Since(retryStartedAt).Round(time.Millisecond), len(body), requestSummary, traceFields)
			if resp.StatusCode == http.StatusOK {
				result, err := ParseNonStreamOpenAIResponseBody(body)
				if err != nil {
					return nil, body, err
				}
				return result, body, nil
			}
		}
		return nil, body, &HTTPStatusError{StatusCode: resp.StatusCode, Body: body}
	}

	result, err := ParseNonStreamOpenAIResponseBody(body)
	if err != nil {
		return nil, body, err
	}
	return result, body, nil
}

func ShouldRetryOpenAIWithoutTools(cfg corelib.MaclawLLMConfig, statusCode int, messages []interface{}, tools []map[string]interface{}) bool {
	return statusCode == http.StatusBadRequest &&
		len(tools) > 0 &&
		cfg.NeedsConservativeOpenAICompatSanitization() &&
		!hasOpenAIToolInteractionMessages(messages)
}

func hasOpenAIToolInteractionMessages(messages []interface{}) bool {
	for _, message := range messages {
		m, ok := toMapInterface(message)
		if !ok {
			continue
		}
		role, _ := m["role"].(string)
		if role == "tool" || role == "function" {
			return true
		}
		if _, ok := m["tool_calls"]; ok {
			return msgIsAssistantWithToolCalls(m)
		}
		if raw, ok := m["function_call"]; ok && !isEmptyFunctionCall(raw) {
			return true
		}
	}
	return false
}

func isEmptyFunctionCall(raw interface{}) bool {
	if raw == nil {
		return true
	}
	switch v := raw.(type) {
	case map[string]interface{}:
		return len(v) == 0
	case map[string]string:
		return len(v) == 0
	default:
		return false
	}
}

// sseChunk represents a single SSE chunk from an OpenAI-compatible streaming response.
type sseChunk struct {
	Choices []struct {
		Delta struct {
			Content          string             `json:"content"`
			ReasoningContent string             `json:"reasoning_content"`
			ToolCalls        []sseToolCallDelta `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *Usage `json:"usage,omitempty"`
}

// sseToolCallDelta represents a tool call fragment in an SSE delta.
type sseToolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

// ParseSSEToResponse parses an SSE-formatted response body (lines prefixed
// with "data: ") into a single *Response by accumulating all chunks.
// This handles the case where an API gateway returns streaming SSE format
// even though the request did not ask for streaming.
func ParseSSEToResponse(body []byte) (*Response, error) {
	var contentBuf, reasoningBuf strings.Builder
	var finishReason string
	var usage *Usage

	// toolCalls accumulated by index
	type toolCallAcc struct {
		ID      string
		Type    string
		Name    string
		ArgsBuf strings.Builder
	}
	toolCalls := make(map[int]*toolCallAcc)

	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}

		var chunk sseChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		delta := chunk.Choices[0].Delta
		contentBuf.WriteString(delta.Content)
		reasoningBuf.WriteString(delta.ReasoningContent)

		if chunk.Choices[0].FinishReason != "" {
			finishReason = chunk.Choices[0].FinishReason
		}

		// Accumulate tool calls by index
		for _, tc := range delta.ToolCalls {
			acc, ok := toolCalls[tc.Index]
			if !ok {
				acc = &toolCallAcc{}
				toolCalls[tc.Index] = acc
			}
			if tc.ID != "" {
				acc.ID = tc.ID
			}
			if tc.Type != "" {
				acc.Type = tc.Type
			}
			if tc.Function.Name != "" {
				acc.Name = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				acc.ArgsBuf.WriteString(tc.Function.Arguments)
				if acc.ArgsBuf.Len() > maxToolArgumentsBytes {
					return nil, fmt.Errorf("tool arguments too large for %s: %d bytes exceeds limit %d", acc.Name, acc.ArgsBuf.Len(), maxToolArgumentsBytes)
				}
			}
		}

	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("SSE stream read error: %w", err)
	}

	msg := Message{
		Role:             "assistant",
		Content:          StripAllExtra(contentBuf.String()),
		ReasoningContent: reasoningBuf.String(),
	}

	// Assemble tool calls in index order
	if len(toolCalls) > 0 {
		// Find max index
		maxIdx := 0
		for idx := range toolCalls {
			if idx > maxIdx {
				maxIdx = idx
			}
		}
		for i := 0; i <= maxIdx; i++ {
			acc, ok := toolCalls[i]
			if !ok {
				continue
			}
			msg.ToolCalls = append(msg.ToolCalls, ToolCall{
				ID:   acc.ID,
				Type: acc.Type,
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name:      acc.Name,
					Arguments: acc.ArgsBuf.String(),
				},
			})
		}
	}

	return &Response{
		Choices: []Choice{{
			Message:      msg,
			FinishReason: finishReason,
		}},
		Usage: usage,
	}, nil
}
