package llm

// stream.go provides streaming LLM request functions that call a TokenCallback
// for each text delta. Used by the TUI agent loop for real-time response display.

import (
	"bufio"
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

// DoOpenAIRequestStream sends a streaming OpenAI-compatible chat request.
// It calls onToken for each text delta, then returns the fully assembled Response.
// If the server returns non-SSE JSON (ignoring the stream flag), it falls back
// to parsing the response as a regular JSON response.
func DoOpenAIRequestStream(
	ctx context.Context,
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	tools []map[string]interface{},
	client *http.Client,
	onToken TokenCallback,
) (*Response, error) {
	return DoOpenAIRequestStreamWithReasoning(ctx, cfg, messages, tools, client, onToken, nil)
}

// DoOpenAIRequestStreamWithReasoning sends a streaming OpenAI-compatible chat request.
// It calls onToken for text deltas and onReasoning for reasoning_content deltas.
func DoOpenAIRequestStreamWithReasoning(
	ctx context.Context,
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	tools []map[string]interface{},
	client *http.Client,
	onToken TokenCallback,
	onReasoning TokenCallback,
) (*Response, error) {
	endpoint, reqBody, err := BuildOpenAIChatRequestData(cfg, messages, OpenAIChatRequestOptions{
		Stream: true,
		Tools:  tools,
	})
	if err != nil {
		return nil, err
	}
	traceFields := RequestTraceLogFields(ctx)
	upstreamModel := cfg.UpstreamModel()
	log.Printf("[LLM-stream] POST %s model=%s configured_model=%s %s", endpoint, upstreamModel, cfg.Model, traceFields)

	startedAt := time.Now()
	result, statusCode, body, err := openAISDKChatStream(ctx, cfg, reqBody, client, onToken, onReasoning)
	if err != nil {
		if statusCode == 0 {
			log.Printf("[LLM-stream] done %s model=%s configured_model=%s status=error elapsed=%s err=%v %s", endpoint, upstreamModel, cfg.Model, time.Since(startedAt).Round(time.Millisecond), err, traceFields)
			return nil, fmt.Errorf("[%s] %w", endpoint, err)
		}
		log.Printf("[LLM-stream] done %s model=%s configured_model=%s status=%d elapsed=%s body_len=%d request=%s %s", endpoint, upstreamModel, cfg.Model, statusCode, time.Since(startedAt).Round(time.Millisecond), len(body), SummarizeOpenAIChatRequestBody(reqBody), traceFields)
		compactRetryAttempted := false
		if ShouldRetryOpenAIWithoutTools(cfg, statusCode, messages, tools) {
			log.Printf("[LLM-stream] retry_without_tools %s model=%s configured_model=%s reason=conservative_openai_compat_400 tools=%d %s", endpoint, upstreamModel, cfg.Model, len(tools), traceFields)
			endpoint, reqBody, err = BuildOpenAIChatRequestData(cfg, messages, OpenAIChatRequestOptions{Stream: true})
			if err != nil {
				return nil, err
			}
			retryStartedAt := time.Now()
			result, statusCode, body, err = openAISDKChatStream(ctx, cfg, reqBody, client, onToken, onReasoning)
			if err != nil {
				if statusCode == 0 {
					log.Printf("[LLM-stream] retry_without_tools done %s model=%s configured_model=%s status=error elapsed=%s err=%v %s", endpoint, upstreamModel, cfg.Model, time.Since(retryStartedAt).Round(time.Millisecond), err, traceFields)
					return nil, fmt.Errorf("[%s] %w", endpoint, err)
				}
				log.Printf("[LLM-stream] retry_without_tools done %s model=%s configured_model=%s status=%d elapsed=%s body_len=%d request=%s %s", endpoint, upstreamModel, cfg.Model, statusCode, time.Since(retryStartedAt).Round(time.Millisecond), len(body), SummarizeOpenAIChatRequestBody(reqBody), traceFields)
				compactMessages := CompactOpenAICompatMessagesForToollessRetry(cfg, messages)
				if len(compactMessages) == 0 {
					return nil, fmt.Errorf("HTTP %d: body_len=%d", statusCode, len(body))
				}
				compactRetryAttempted = true
				log.Printf("[LLM-stream] retry_compact_without_tools %s model=%s configured_model=%s reason=conservative_openai_compat_400 %s", endpoint, upstreamModel, cfg.Model, traceFields)
				endpoint, reqBody, err = BuildOpenAIChatRequestData(cfg, compactMessages, OpenAIChatRequestOptions{Stream: true})
				if err != nil {
					return nil, err
				}
				compactStartedAt := time.Now()
				result, statusCode, body, err = openAISDKChatStream(ctx, cfg, reqBody, client, onToken, onReasoning)
				if err != nil {
					if statusCode == 0 {
						log.Printf("[LLM-stream] retry_compact_without_tools done %s model=%s configured_model=%s status=error elapsed=%s err=%v %s", endpoint, upstreamModel, cfg.Model, time.Since(compactStartedAt).Round(time.Millisecond), err, traceFields)
						return nil, fmt.Errorf("[%s] %w", endpoint, err)
					}
					log.Printf("[LLM-stream] retry_compact_without_tools done %s model=%s configured_model=%s status=%d elapsed=%s body_len=%d request=%s %s", endpoint, upstreamModel, cfg.Model, statusCode, time.Since(compactStartedAt).Round(time.Millisecond), len(body), SummarizeOpenAIChatRequestBody(reqBody), traceFields)
					return nil, fmt.Errorf("HTTP %d: body_len=%d", statusCode, len(body))
				}
				log.Printf("[LLM-stream] retry_compact_without_tools done %s model=%s configured_model=%s status=%d elapsed=%s %s", endpoint, upstreamModel, cfg.Model, statusCode, time.Since(compactStartedAt).Round(time.Millisecond), traceFields)
			} else {
				log.Printf("[LLM-stream] retry_without_tools done %s model=%s configured_model=%s status=%d elapsed=%s %s", endpoint, upstreamModel, cfg.Model, statusCode, time.Since(retryStartedAt).Round(time.Millisecond), traceFields)
			}
		}
		if statusCode != http.StatusOK && !compactRetryAttempted && ShouldRetryOpenAIWithCompact(cfg, statusCode, messages) {
			compactMessages := CompactOpenAICompatMessagesForToollessRetry(cfg, messages)
			log.Printf("[LLM-stream] retry_compact_without_tools %s model=%s configured_model=%s reason=conservative_openai_compat_400_direct %s", endpoint, upstreamModel, cfg.Model, traceFields)
			endpoint, reqBody, err = BuildOpenAIChatRequestData(cfg, compactMessages, OpenAIChatRequestOptions{Stream: true})
			if err != nil {
				return nil, err
			}
			compactStartedAt := time.Now()
			result, statusCode, body, err = openAISDKChatStream(ctx, cfg, reqBody, client, onToken, onReasoning)
			if err != nil {
				if statusCode == 0 {
					log.Printf("[LLM-stream] retry_compact_without_tools done %s model=%s configured_model=%s status=error elapsed=%s err=%v %s", endpoint, upstreamModel, cfg.Model, time.Since(compactStartedAt).Round(time.Millisecond), err, traceFields)
					return nil, fmt.Errorf("[%s] %w", endpoint, err)
				}
				log.Printf("[LLM-stream] retry_compact_without_tools done %s model=%s configured_model=%s status=%d elapsed=%s body_len=%d request=%s %s", endpoint, upstreamModel, cfg.Model, statusCode, time.Since(compactStartedAt).Round(time.Millisecond), len(body), SummarizeOpenAIChatRequestBody(reqBody), traceFields)
				return nil, fmt.Errorf("HTTP %d: body_len=%d", statusCode, len(body))
			}
			log.Printf("[LLM-stream] retry_compact_without_tools done %s model=%s configured_model=%s status=%d elapsed=%s %s", endpoint, upstreamModel, cfg.Model, statusCode, time.Since(compactStartedAt).Round(time.Millisecond), traceFields)
		}
		if statusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d: body_len=%d", statusCode, len(body))
		}
	}
	log.Printf("[LLM-stream] done %s model=%s configured_model=%s status=%d elapsed=%s %s", endpoint, upstreamModel, cfg.Model, statusCode, time.Since(startedAt).Round(time.Millisecond), traceFields)
	return result, nil
}

// DoAnthropicRequestStream sends a streaming Anthropic chat request.
// It calls onToken for each text delta, then returns the fully assembled Response.
func DoAnthropicRequestStream(
	ctx context.Context,
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	tools []map[string]interface{},
	client *http.Client,
	onToken TokenCallback,
) (*Response, error) {
	endpoint, data, err := BuildAnthropicMessagesRequestData(cfg, messages, AnthropicMessagesRequestOptions{Stream: true, Tools: tools})
	if err != nil {
		return nil, err
	}
	traceFields := RequestTraceLogFields(ctx)
	upstreamModel := cfg.UpstreamModel()
	log.Printf("[LLM-stream] POST %s model=%s configured_model=%s protocol=anthropic sdk=anthropic-sdk-go %s", endpoint, upstreamModel, cfg.Model, traceFields)
	startedAt := time.Now()
	result, statusCode, body, err := anthropicSDKMessageStream(ctx, cfg, data, client, onToken)
	if err != nil {
		log.Printf("[LLM-stream] done %s model=%s configured_model=%s protocol=anthropic status=%d elapsed=%s body_len=%d err=%v %s", endpoint, upstreamModel, cfg.Model, statusCode, time.Since(startedAt).Round(time.Millisecond), len(body), err, traceFields)
		return nil, fmt.Errorf("HTTP %d: body_len=%d: %w", statusCode, len(body), err)
	}
	if statusCode != http.StatusOK {
		log.Printf("[LLM-stream] done %s model=%s configured_model=%s protocol=anthropic status=%d elapsed=%s body_len=%d %s", endpoint, upstreamModel, cfg.Model, statusCode, time.Since(startedAt).Round(time.Millisecond), len(body), traceFields)
		return nil, fmt.Errorf("HTTP %d: body_len=%d", statusCode, len(body))
	}
	log.Printf("[LLM-stream] done %s model=%s configured_model=%s protocol=anthropic status=%d elapsed=%s %s", endpoint, upstreamModel, cfg.Model, statusCode, time.Since(startedAt).Round(time.Millisecond), traceFields)
	return result, nil
}

// parseSSEStream reads an OpenAI-compatible SSE stream, calling onToken for
// each text delta, and returns the fully assembled Response.
func parseSSEStream(body io.Reader, onToken TokenCallback) (*Response, error) {
	var contentBuf, reasoningBuf strings.Builder
	var finishReason string
	var usage *Usage
	contentFilter := newContentToolCallDeltaFilter(onToken)

	type toolCallAcc struct {
		ID      string
		Type    string
		Name    string
		ArgsBuf strings.Builder
	}
	toolCalls := make(map[int]*toolCallAcc)
	legacyFunctionCall := &toolCallAcc{Type: "function"}
	legacyFunctionCallSeen := false

	scanner := bufio.NewScanner(body)
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

		// Stream text deltas to the callback.
		if delta.Content != "" {
			contentBuf.WriteString(delta.Content)
			contentFilter.Write(delta.Content)
		}
		if delta.ReasoningContent != "" {
			reasoningBuf.WriteString(delta.ReasoningContent)
		}

		if chunk.Choices[0].FinishReason != "" {
			finishReason = chunk.Choices[0].FinishReason
			if finishReason == "function_call" {
				finishReason = "tool_calls"
			}
		}

		if delta.FunctionCall != nil {
			legacyFunctionCallSeen = true
			if delta.FunctionCall.Name != "" {
				if legacyFunctionCall.Name == "" {
					legacyFunctionCall.Name = delta.FunctionCall.Name
				} else {
					legacyFunctionCall.Name += delta.FunctionCall.Name
				}
			}
			if delta.FunctionCall.Arguments != "" {
				legacyFunctionCall.ArgsBuf.WriteString(delta.FunctionCall.Arguments)
				if legacyFunctionCall.ArgsBuf.Len() > maxToolArgumentsBytes {
					return nil, fmt.Errorf("tool arguments too large for %s: %d bytes", legacyFunctionCall.Name, legacyFunctionCall.ArgsBuf.Len())
				}
			}
		}

		// Accumulate tool calls by index.
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
					return nil, fmt.Errorf("tool arguments too large for %s: %d bytes", acc.Name, acc.ArgsBuf.Len())
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("SSE stream read: %w", err)
	}
	contentFilter.Flush()

	msg := Message{
		Role:             "assistant",
		Content:          StripAllExtra(contentBuf.String()),
		ReasoningContent: reasoningBuf.String(),
	}

	// Assemble tool calls in index order.
	if len(toolCalls) > 0 {
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
				Type: normalizeToolCallType(acc.Type),
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
	if len(toolCalls) == 0 && legacyFunctionCallSeen {
		if call, ok := normalizePlainContentToolCallWithID("", legacyFunctionCall.Type, legacyFunctionCall.Name, json.RawMessage(legacyFunctionCall.ArgsBuf.String())); ok {
			msg.ToolCalls = append(msg.ToolCalls, call)
			msg.Content = ""
			finishReason = "tool_calls"
		}
	}
	if len(msg.ToolCalls) == 0 {
		rawContent := contentBuf.String()
		if contentCalls, malformed := ParseContentToolCallsDetailed(rawContent); len(contentCalls) > 0 {
			msg.ToolCalls = append(msg.ToolCalls, contentCalls...)
			msg.Content = ""
			finishReason = "tool_calls"
		} else if malformed {
			msg.Content = MalformedContentToolCallErrorMsg
			finishReason = "stop"
		}
	}
	finishReason, truncatedTools := filterStreamTruncatedToolCalls(&msg, finishReason)

	return &Response{
		Choices: []Choice{{Message: msg, FinishReason: finishReason, TruncatedToolNames: truncatedTools}},
		Usage:   usage,
	}, nil
}

// parseAnthropicSSEStream reads an Anthropic SSE stream, calling onToken for
// each text delta, and returns the fully assembled Response.
func parseAnthropicSSEStream(body io.Reader, onToken TokenCallback) (*Response, error) {
	var contentBuf strings.Builder
	var finishReason string
	var usage *Usage
	contentFilter := newContentToolCallDeltaFilter(onToken)

	type toolCallAcc struct {
		ID      string
		Name    string
		ArgsBuf strings.Builder
	}
	var toolCalls []toolCallAcc
	var currentToolIdx int = -1

	scanner := bufio.NewScanner(body)
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

		var event struct {
			Type  string `json:"type"`
			Index int    `json:"index,omitempty"`
			Delta struct {
				Type        string `json:"type,omitempty"`
				Text        string `json:"text,omitempty"`
				PartialJSON string `json:"partial_json,omitempty"`
				StopReason  string `json:"stop_reason,omitempty"`
			} `json:"delta,omitempty"`
			ContentBlock *struct {
				Type string `json:"type"`
				ID   string `json:"id,omitempty"`
				Name string `json:"name,omitempty"`
				Text string `json:"text,omitempty"`
			} `json:"content_block,omitempty"`
			Message *struct {
				StopReason string `json:"stop_reason,omitempty"`
				Usage      *Usage `json:"usage,omitempty"`
			} `json:"message,omitempty"`
			Usage *Usage `json:"usage,omitempty"`
		}
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			continue
		}

		switch event.Type {
		case "content_block_start":
			if event.ContentBlock != nil {
				switch event.ContentBlock.Type {
				case "text":
					// Text block started, nothing to accumulate yet.
				case "tool_use":
					toolCalls = append(toolCalls, toolCallAcc{
						ID:   event.ContentBlock.ID,
						Name: event.ContentBlock.Name,
					})
					currentToolIdx = len(toolCalls) - 1
				}
			}
		case "content_block_delta":
			if event.Delta.Type == "text_delta" && event.Delta.Text != "" {
				contentBuf.WriteString(event.Delta.Text)
				contentFilter.Write(event.Delta.Text)
			}
			if event.Delta.Type == "input_json_delta" && event.Delta.PartialJSON != "" {
				if currentToolIdx >= 0 && currentToolIdx < len(toolCalls) {
					toolCalls[currentToolIdx].ArgsBuf.WriteString(event.Delta.PartialJSON)
				}
			}
		case "message_delta":
			if event.Delta.StopReason != "" {
				switch event.Delta.StopReason {
				case "tool_use":
					finishReason = "tool_calls"
				case "max_tokens":
					finishReason = "length"
				default:
					finishReason = "stop"
				}
			}
		case "message_start":
			if event.Message != nil && event.Message.Usage != nil {
				usage = event.Message.Usage
			}
		case "message_stop":
			// Stream complete.
		}

		// Capture output token usage from message_delta.
		if event.Usage != nil && event.Usage.OutputTokens > 0 {
			if usage == nil {
				usage = &Usage{}
			}
			usage.OutputTokens = event.Usage.OutputTokens
			usage.CompletionTokens = event.Usage.OutputTokens
			usage.TotalTokens = usage.InputTokens + usage.OutputTokens
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("Anthropic SSE stream read: %w", err)
	}
	contentFilter.Flush()

	msg := Message{
		Role:    "assistant",
		Content: StripAllExtra(contentBuf.String()),
	}

	for _, tc := range toolCalls {
		msg.ToolCalls = append(msg.ToolCalls, ToolCall{
			ID:   tc.ID,
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      tc.Name,
				Arguments: tc.ArgsBuf.String(),
			},
		})
	}

	if finishReason == "" {
		finishReason = "stop"
	}
	if len(msg.ToolCalls) == 0 {
		rawContent := contentBuf.String()
		if contentCalls, malformed := ParseContentToolCallsDetailed(rawContent); len(contentCalls) > 0 {
			msg.ToolCalls = append(msg.ToolCalls, contentCalls...)
			msg.Content = ""
			finishReason = "tool_calls"
		} else if malformed {
			msg.Content = MalformedContentToolCallErrorMsg
			finishReason = "stop"
		}
	}
	finishReason, truncatedTools := filterStreamTruncatedToolCalls(&msg, finishReason)

	return &Response{
		Choices: []Choice{{Message: msg, FinishReason: finishReason, TruncatedToolNames: truncatedTools}},
		Usage:   usage,
	}, nil
}

func filterStreamTruncatedToolCalls(msg *Message, finishReason string) (string, []string) {
	if msg == nil || len(msg.ToolCalls) == 0 {
		return finishReason, nil
	}
	isLengthTruncated := isStreamLengthFinishReason(finishReason)
	validCalls := make([]ToolCall, 0, len(msg.ToolCalls))
	var truncatedNames []string
	for _, tc := range msg.ToolCalls {
		args := strings.TrimSpace(tc.Function.Arguments)
		if args == "" {
			if isLengthTruncated {
				truncatedNames = append(truncatedNames, tc.Function.Name)
				continue
			}
			validCalls = append(validCalls, tc)
			continue
		}
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(args), &parsed); err != nil {
			truncatedNames = append(truncatedNames, tc.Function.Name)
			log.Printf("[LLM-stream] truncated tool call (invalid JSON): %s args=%d bytes finish_reason=%s", tc.Function.Name, len(args), finishReason)
			continue
		}
		if missing := streamTruncatedRequiredField(tc.Function.Name, parsed); missing != "" && (isLengthTruncated || len(args) > 4000) {
			truncatedNames = append(truncatedNames, tc.Function.Name)
			log.Printf("[LLM-stream] truncated tool call (missing required field %q): %s args=%d bytes finish_reason=%s", missing, tc.Function.Name, len(args), finishReason)
			continue
		}
		validCalls = append(validCalls, tc)
	}
	if len(truncatedNames) == 0 {
		return finishReason, nil
	}
	msg.ToolCalls = validCalls
	return finishReason, truncatedNames
}

func isStreamLengthFinishReason(reason string) bool {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "length", "max_tokens":
		return true
	default:
		return false
	}
}

var streamTruncatedRequiredFields = map[string][]string{
	"write_file": {"path", "content"},
	"edit_file":  {"path", "old_string", "new_string"},
	"edit_lines": {"path", "operation", "start_line"},
	"bash":       {"command"},
}

func streamTruncatedRequiredField(toolName string, parsed map[string]interface{}) string {
	fields := streamTruncatedRequiredFields[strings.TrimSpace(toolName)]
	for _, field := range fields {
		value, ok := parsed[field]
		if !ok {
			return field
		}
		if s, ok := value.(string); ok && strings.TrimSpace(s) == "" {
			return field
		}
	}
	return ""
}
