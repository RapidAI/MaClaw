package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// TokenCallback is called with each text delta from the LLM streaming response.
type TokenCallback func(delta string)

// NewRoundCallback is called when a new agent loop iteration starts LLM generation.
type NewRoundCallback func()

// stripThinkTags removes <think>...</think> blocks (including the tags) from
// LLM output. Some models (e.g. kimi, deepseek) include reasoning traces
// wrapped in these tags that should not be shown to end users.
var reThinkBlock = regexp.MustCompile(`(?s)<think>.*?</think>\s*`)

func stripThinkTags(s string) string {
	return strings.TrimSpace(reThinkBlock.ReplaceAllString(s, ""))
}

// ---------------------------------------------------------------------------
// thinkFilter — stateful streaming filter for <think>...</think> blocks
// ---------------------------------------------------------------------------

// thinkFilter wraps a TokenCallback to suppress <think>...</think> content
// in real-time during streaming. It uses a simple state machine:
//   - outside: normal text is forwarded; watch for "<think>"
//   - inside:  all text is swallowed; watch for "</think>"
//
// Because SSE deltas can split tags across chunks (e.g. "<thi" + "nk>"),
// a small pending buffer holds ambiguous prefixes until they can be resolved.
type thinkFilter struct {
	downstream TokenCallback
	inside     bool           // true while between <think> and </think>
	pending    strings.Builder // buffered text that might be part of a tag
}

const (
	thinkOpen  = "<think>"
	thinkClose = "</think>"

	funcCallOpen  = "<|FunctionCallBegin|>"
	funcCallClose = "<|FunctionCallEnd|>"
)

// newThinkFilter returns a TokenCallback that filters out <think> blocks
// before forwarding to downstream.
func newThinkFilter(downstream TokenCallback) *thinkFilter {
	return &thinkFilter{downstream: downstream}
}

// Write processes a new delta from the stream.
func (f *thinkFilter) Write(delta string) {
	f.pending.WriteString(delta)
	f.drain()
}

// Flush should be called when the stream ends to emit any remaining buffered
// text (only matters if a partial "<thi" was buffered but never completed).
func (f *thinkFilter) Flush() {
	if f.pending.Len() > 0 && !f.inside {
		f.downstream(f.pending.String())
		f.pending.Reset()
	}
}

func (f *thinkFilter) drain() {
	for f.pending.Len() > 0 {
		s := f.pending.String()
		if f.inside {
			// Look for closing tag.
			idx := strings.Index(s, thinkClose)
			if idx >= 0 {
				// Skip everything up to and including </think> plus trailing whitespace.
				after := s[idx+len(thinkClose):]
				after = strings.TrimLeft(after, " \t\r\n")
				f.pending.Reset()
				f.pending.WriteString(after)
				f.inside = false
				continue
			}
			// No full closing tag found. Keep only the tail that could be a
			// partial "</think>" prefix to prevent unbounded buffer growth
			// while inside a long <think> block.
			tail := f.partialTagTail(s, thinkClose)
			f.pending.Reset()
			if tail != "" {
				f.pending.WriteString(tail)
			}
			return
		}

		// Outside: look for opening tag.
		idx := strings.Index(s, thinkOpen)
		if idx >= 0 {
			// Emit text before the tag.
			if idx > 0 {
				f.downstream(s[:idx])
			}
			after := s[idx+len(thinkOpen):]
			f.pending.Reset()
			f.pending.WriteString(after)
			f.inside = true
			continue
		}
		// Check if the tail could be a partial "<think>" prefix.
		if f.couldBePrefix(s, thinkOpen) {
			// Emit the safe portion (everything except the ambiguous tail).
			safe := f.safePrefixLen(s, thinkOpen)
			if safe > 0 {
				f.downstream(s[:safe])
				rest := s[safe:]
				f.pending.Reset()
				f.pending.WriteString(rest)
			}
			return // wait for more data
		}
		// No tag at all — emit everything.
		f.downstream(s)
		f.pending.Reset()
		return
	}
}

// couldBePrefix returns true if the tail of s could be the start of tag.
func (f *thinkFilter) couldBePrefix(s, tag string) bool {
	return hasPartialTagSuffix(s, tag)
}

// partialTagTail returns the longest suffix of s that is a proper prefix of
// tag (i.e. could become the full tag with more data). Returns "" if no
// suffix matches.
// Delegates to the shared package-level helper.
func (f *thinkFilter) partialTagTail(s, tag string) string {
	return partialTagTail(s, tag)
}

// safePrefixLen returns the length of s that can be safely emitted, i.e.
// everything except the trailing portion that could be a partial tag start.
// Delegates to the shared package-level helper.
func (f *thinkFilter) safePrefixLen(s, tag string) int {
	return safeEmitLen(s, tag)
}

// ---------------------------------------------------------------------------
// funcCallFilter — stateful streaming filter for <|FunctionCallBegin|>...<|FunctionCallEnd|>
// ---------------------------------------------------------------------------

// funcCallFilter wraps a TokenCallback to suppress function-call markup that
// some free/low-quality providers leak into their text output.
type funcCallFilter struct {
	downstream TokenCallback
	inside     bool
	pending    strings.Builder
}

func newFuncCallFilter(downstream TokenCallback) *funcCallFilter {
	return &funcCallFilter{downstream: downstream}
}

func (f *funcCallFilter) Write(delta string) {
	f.pending.WriteString(delta)
	f.drain()
}

func (f *funcCallFilter) Flush() {
	// If inside an unclosed block, discard pending (incomplete func call markup).
	if f.pending.Len() > 0 && !f.inside {
		f.downstream(f.pending.String())
		f.pending.Reset()
	}
}

func (f *funcCallFilter) drain() {
	for f.pending.Len() > 0 {
		s := f.pending.String()
		if f.inside {
			idx := strings.Index(s, funcCallClose)
			if idx >= 0 {
				after := s[idx+len(funcCallClose):]
				f.pending.Reset()
				f.pending.WriteString(after)
				f.inside = false
				continue
			}
			tail := partialTagTail(s, funcCallClose)
			f.pending.Reset()
			if tail != "" {
				f.pending.WriteString(tail)
			}
			return
		}
		idx := strings.Index(s, funcCallOpen)
		if idx >= 0 {
			if idx > 0 {
				f.downstream(s[:idx])
			}
			after := s[idx+len(funcCallOpen):]
			f.pending.Reset()
			f.pending.WriteString(after)
			f.inside = true
			continue
		}
		if hasPartialTagSuffix(s, funcCallOpen) {
			safe := safeEmitLen(s, funcCallOpen)
			if safe > 0 {
				f.downstream(s[:safe])
				rest := s[safe:]
				f.pending.Reset()
				f.pending.WriteString(rest)
			}
			return
		}
		f.downstream(s)
		f.pending.Reset()
		return
	}
}

// Shared helpers for partial-tag detection (used by both filters).

func partialTagTail(s, tag string) string {
	maxCheck := len(tag) - 1
	if maxCheck > len(s) {
		maxCheck = len(s)
	}
	for i := maxCheck; i >= 1; i-- {
		suffix := s[len(s)-i:]
		if strings.HasPrefix(tag, suffix) {
			return suffix
		}
	}
	return ""
}

func hasPartialTagSuffix(s, tag string) bool {
	return partialTagTail(s, tag) != ""
}

func safeEmitLen(s, tag string) int {
	maxCheck := len(tag) - 1
	if maxCheck > len(s) {
		maxCheck = len(s)
	}
	for i := maxCheck; i >= 1; i-- {
		suffix := s[len(s)-i:]
		if strings.HasPrefix(tag, suffix) {
			return len(s) - i
		}
	}
	return len(s)
}

// stripFunctionCalls removes <|FunctionCallBegin|>...<|FunctionCallEnd|> blocks
// from a complete string (non-streaming path).
var reFuncCallBlock = regexp.MustCompile(`(?s)<\|FunctionCallBegin\|>.*?<\|FunctionCallEnd\|>`)

func stripFunctionCalls(s string) string {
	return reFuncCallBlock.ReplaceAllString(s, "")
}

// ---------------------------------------------------------------------------
// Streaming LLM request — dispatches to OpenAI or Anthropic
// ---------------------------------------------------------------------------

// doLLMRequestStream sends a streaming LLM request. Text deltas are pushed
// via onToken in real-time. The full llmResponse (with assembled tool_calls)
// is returned when the stream ends.
// If onToken is nil, falls back to the non-streaming doLLMRequest.
// If the provider doesn't support streaming, automatically falls back.
func (h *IMMessageHandler) doLLMRequestStream(
	cfg MaclawLLMConfig,
	messages []interface{},
	tools []map[string]interface{},
	httpClient *http.Client,
	onToken TokenCallback,
) (*llmResponse, error) {
	if onToken == nil {
		return h.doLLMRequest(cfg, messages, tools, httpClient)
	}
	if cfg.Protocol == "anthropic" {
		return h.doAnthropicLLMRequestStream(cfg, messages, tools, httpClient, onToken)
	}
	return h.doOpenAILLMRequestStream(cfg, messages, tools, httpClient, onToken)
}

// ---------------------------------------------------------------------------
// OpenAI SSE streaming
// ---------------------------------------------------------------------------

// openAIStreamDelta mirrors the delta object inside a streaming chunk.
type openAIStreamDelta struct {
	Content          string                   `json:"content,omitempty"`
	ReasoningContent string                   `json:"reasoning_content,omitempty"`
	ToolCalls        []openAIStreamToolDelta  `json:"tool_calls,omitempty"`
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
}

func (h *IMMessageHandler) doOpenAILLMRequestStream(
	cfg MaclawLLMConfig,
	messages []interface{},
	tools []map[string]interface{},
	httpClient *http.Client,
	onToken TokenCallback,
) (*llmResponse, error) {
	endpoint := strings.TrimRight(cfg.URL, "/") + "/chat/completions"

	reqBody := map[string]interface{}{
		"model":    cfg.Model,
		"messages": messages,
		"stream":   true,
	}
	if len(tools) > 0 {
		reqBody["tools"] = tools
	}

	data, _ := json.Marshal(reqBody)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", cfg.UserAgent())
	if cfg.Key != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Key)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Fallback: if provider doesn't return SSE, parse as normal JSON response.
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/event-stream") {
		return parseNonStreamOpenAIResponse(resp, data)
	}

	// SSE parsing — wrap onToken with think-tag filter and func-call filter
	fcf := newFuncCallFilter(onToken)
	tf := newThinkFilter(func(s string) { fcf.Write(s) })
	var contentBuf strings.Builder
	type toolAccum struct {
		id       string
		typ      string
		name     strings.Builder
		args     strings.Builder
	}
	toolAccums := make(map[int]*toolAccum)
	var reasoningBuf strings.Builder
	var finishReason string

	scanner := bufio.NewScanner(resp.Body)
	// Increase scanner buffer for large chunks
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimPrefix(line, "data:")
		payload = strings.TrimPrefix(payload, " ")
		if payload == "[DONE]" {
			break
		}

		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue // skip malformed chunks
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]
		delta := choice.Delta

		// Reasoning content delta (kimi-k2.5, deepseek, etc.)
		// Only accumulate for post-stream fallback; never push to frontend
		// — reasoning_content is internal chain-of-thought, not user-facing.
		if delta.ReasoningContent != "" {
			reasoningBuf.WriteString(delta.ReasoningContent)
		}

		// Text content delta
		if delta.Content != "" {
			contentBuf.WriteString(delta.Content)
			tf.Write(delta.Content)
		}

		// Tool call deltas
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
			}
		}

		if choice.FinishReason != nil {
			finishReason = *choice.FinishReason
		}
	}
	// Check for scanner errors (network interruption, etc.)
	if err := scanner.Err(); err != nil {
		// If we already accumulated some content, return what we have
		// rather than failing entirely — partial response is better than none.
		if contentBuf.Len() == 0 && len(toolAccums) == 0 {
			return nil, fmt.Errorf("SSE stream read error: %w", err)
		}
	}
	tf.Flush()
	fcf.Flush()

	// Assemble llmResponse
	content := stripFunctionCalls(stripThinkTags(contentBuf.String()))
	reasoning := reasoningBuf.String()
	if content == "" && reasoning != "" {
		content = stripFunctionCalls(stripThinkTags(reasoning))
	}
	msg := llmMessage{
		Role:             "assistant",
		Content:          content,
		ReasoningContent: reasoning,
	}
	// Collect tool calls in index order
	if len(toolAccums) > 0 {
		maxIdx := 0
		for idx := range toolAccums {
			if idx > maxIdx {
				maxIdx = idx
			}
		}
		for i := 0; i <= maxIdx; i++ {
			acc, ok := toolAccums[i]
			if !ok {
				continue
			}
			msg.ToolCalls = append(msg.ToolCalls, llmToolCall{
				ID:   acc.id,
				Type: acc.typ,
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name:      acc.name.String(),
					Arguments: acc.args.String(),
				},
			})
		}
	}

	if finishReason == "" {
		finishReason = "stop"
	}

	return &llmResponse{
		Choices: []llmChoice{{Message: msg, FinishReason: finishReason}},
	}, nil
}

// parseNonStreamOpenAIResponse handles the fallback case where the provider
// returned a normal JSON response instead of SSE.
func parseNonStreamOpenAIResponse(resp *http.Response, requestBody []byte) (*llmResponse, error) {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if resp.StatusCode != http.StatusOK {
		msg := string(body)
		if len(msg) > 512 {
			msg = msg[:512] + "..."
		}
		return nil, dumpLLMContext(resp.StatusCode, msg, requestBody)
	}
	// Guard: some gateways return SSE or plain-text even when stream=false.
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		preview := string(trimmed)
		if len(preview) > 256 {
			preview = preview[:256] + "..."
		}
		return nil, fmt.Errorf("expected JSON but got: %s", preview)
	}

	var result llmResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	// Strip <think>...</think> blocks from content.
	for i := range result.Choices {
		result.Choices[i].Message.Content = stripFunctionCalls(stripThinkTags(result.Choices[i].Message.Content))
		if result.Choices[i].Message.Content == "" && result.Choices[i].Message.ReasoningContent != "" {
			result.Choices[i].Message.Content = stripFunctionCalls(stripThinkTags(result.Choices[i].Message.ReasoningContent))
		}
	}
	return &result, nil
}

// ---------------------------------------------------------------------------
// Shared Anthropic message/tool conversion helpers
// ---------------------------------------------------------------------------

// anthropicConvertedMessages holds the result of converting OpenAI-style
// messages and tools into Anthropic API format.
type anthropicConvertedMessages struct {
	SystemText string
	Messages   []interface{}
}

// convertToAnthropicMessages converts OpenAI-style conversation messages
// into Anthropic Messages API format, separating the system prompt.
// Shared by both streaming and non-streaming Anthropic paths.
func convertToAnthropicMessages(messages []interface{}) anthropicConvertedMessages {
	var result anthropicConvertedMessages
	for _, m := range messages {
		mm, ok := m.(map[string]interface{})
		if !ok {
			if ms, ok2 := m.(map[string]string); ok2 {
				mm = make(map[string]interface{}, len(ms))
				for k, v := range ms {
					mm[k] = v
				}
			} else {
				result.Messages = append(result.Messages, m)
				continue
			}
		}
		role, _ := mm["role"].(string)
		switch role {
		case "system":
			if content, _ := mm["content"].(string); content != "" {
				result.SystemText = content
			}
		case "assistant":
			var contentBlocks []interface{}
			if text, _ := mm["content"].(string); text != "" {
				contentBlocks = append(contentBlocks, map[string]interface{}{
					"type": "text", "text": text,
				})
			}
			if tcs, ok := mm["tool_calls"].([]llmToolCall); ok {
				for _, tc := range tcs {
					var inputObj interface{}
					_ = json.Unmarshal([]byte(tc.Function.Arguments), &inputObj)
					if inputObj == nil {
						inputObj = map[string]interface{}{}
					}
					contentBlocks = append(contentBlocks, map[string]interface{}{
						"type": "tool_use", "id": tc.ID,
						"name": tc.Function.Name, "input": inputObj,
					})
				}
			}
			if len(contentBlocks) > 0 {
				result.Messages = append(result.Messages, map[string]interface{}{
					"role": "assistant", "content": contentBlocks,
				})
			}
		case "tool":
			toolCallID, _ := mm["tool_call_id"].(string)
			content, _ := mm["content"].(string)
			toolResultBlock := map[string]interface{}{
				"type": "tool_result", "tool_use_id": toolCallID, "content": content,
			}
			merged := false
			if len(result.Messages) > 0 {
				if lastMsg, ok := result.Messages[len(result.Messages)-1].(map[string]interface{}); ok {
					if lastRole, _ := lastMsg["role"].(string); lastRole == "user" {
						if blocks, ok := lastMsg["content"].([]interface{}); ok && len(blocks) > 0 {
							if firstBlock, ok := blocks[0].(map[string]interface{}); ok {
								if firstBlock["type"] == "tool_result" {
									lastMsg["content"] = append(blocks, toolResultBlock)
									merged = true
								}
							}
						}
					}
				}
			}
			if !merged {
				result.Messages = append(result.Messages, map[string]interface{}{
					"role": "user", "content": []interface{}{toolResultBlock},
				})
			}
		default:
			result.Messages = append(result.Messages, map[string]interface{}{
				"role": role, "content": mm["content"],
			})
		}
	}
	return result
}

// convertToAnthropicTools converts OpenAI-style tool definitions to Anthropic format.
func convertToAnthropicTools(tools []map[string]interface{}) []map[string]interface{} {
	var anthropicTools []map[string]interface{}
	for _, t := range tools {
		fn, _ := t["function"].(map[string]interface{})
		if fn == nil {
			continue
		}
		at := map[string]interface{}{"name": fn["name"]}
		if desc, ok := fn["description"]; ok {
			at["description"] = desc
		}
		if params, ok := fn["parameters"]; ok {
			at["input_schema"] = params
		} else {
			at["input_schema"] = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
		}
		anthropicTools = append(anthropicTools, at)
	}
	return anthropicTools
}

// ---------------------------------------------------------------------------
// Anthropic SSE streaming
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) doAnthropicLLMRequestStream(
	cfg MaclawLLMConfig,
	messages []interface{},
	tools []map[string]interface{},
	httpClient *http.Client,
	onToken TokenCallback,
) (*llmResponse, error) {
	endpoint := strings.TrimRight(cfg.URL, "/") + "/v1/messages"

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
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", cfg.UserAgent())
	req.Header.Set("anthropic-version", "2023-06-01")
	if cfg.Key != "" {
		req.Header.Set("x-api-key", cfg.Key)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

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

	fcf := newFuncCallFilter(onToken)
	tf := newThinkFilter(func(s string) { fcf.Write(s) })

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
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
			} `json:"message,omitempty"`
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
			}
			if evt.Delta.Type == "input_json_delta" && evt.Delta.PartialJSON != "" {
				acc.toolArgs.WriteString(evt.Delta.PartialJSON)
			}

		case "message_delta":
			if evt.Delta.StopReason != "" {
				stopReason = evt.Delta.StopReason
			}

		case "message_stop":
			// End of stream

		case "message_start":
			if evt.Message.StopReason != "" {
				stopReason = evt.Message.StopReason
			}
		}
	}
	// Check for scanner errors (network interruption, etc.)
	if err := scanner.Err(); err != nil {
		if len(blocks) == 0 {
			return nil, fmt.Errorf("SSE stream read error: %w", err)
		}
	}
	tf.Flush()
	fcf.Flush()

	// Assemble llmResponse
	msg := llmMessage{Role: "assistant"}
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
			msg.ToolCalls = append(msg.ToolCalls, llmToolCall{
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

	finishReason := "stop"
	if stopReason == "tool_use" {
		finishReason = "tool_calls"
	} else if stopReason == "max_tokens" {
		finishReason = "length"
	}

	return &llmResponse{
		Choices: []llmChoice{{Message: msg, FinishReason: finishReason}},
	}, nil
}

// parseNonStreamAnthropicResponse handles the fallback case where the provider
// returned a normal JSON response instead of SSE for Anthropic protocol.
func parseNonStreamAnthropicResponse(resp *http.Response, requestBody []byte) (*llmResponse, error) {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if resp.StatusCode != http.StatusOK {
		msg := string(body)
		if len(msg) > 512 {
			msg = msg[:512] + "..."
		}
		return nil, dumpLLMContext(resp.StatusCode, msg, requestBody)
	}

	var anthropicResp struct {
		Content    []anthropicContentBlock `json:"content"`
		StopReason string                  `json:"stop_reason"`
	}
	if err := json.Unmarshal(body, &anthropicResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	llmMsg := llmMessage{Role: "assistant"}
	var textParts []string
	for _, block := range anthropicResp.Content {
		switch block.Type {
		case "text":
			textParts = append(textParts, block.Text)
		case "tool_use":
			argsJSON, _ := json.Marshal(block.Input)
			llmMsg.ToolCalls = append(llmMsg.ToolCalls, llmToolCall{
				ID:   block.ID,
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name:      block.Name,
					Arguments: string(argsJSON),
				},
			})
		}
	}
	llmMsg.Content = stripFunctionCalls(stripThinkTags(strings.Join(textParts, "\n")))
	finishReason := "stop"
	if anthropicResp.StopReason == "tool_use" {
		finishReason = "tool_calls"
	} else if anthropicResp.StopReason == "max_tokens" {
		finishReason = "length"
	}

	return &llmResponse{
		Choices: []llmChoice{{Message: llmMsg, FinishReason: finishReason}},
	}, nil
}
