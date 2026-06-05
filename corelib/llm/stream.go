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
	req, _, endpoint, err := NewOpenAIChatRequest(ctx, cfg, messages, OpenAIChatRequestOptions{
		Stream: true,
		Tools:  tools,
	})
	if err != nil {
		return nil, err
	}
	traceFields := RequestTraceLogFields(ctx)
	log.Printf("[LLM-stream] POST %s model=%s %s", endpoint, cfg.Model, traceFields)

	startedAt := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[LLM-stream] done %s model=%s status=error elapsed=%s err=%v %s", endpoint, cfg.Model, time.Since(startedAt).Round(time.Millisecond), err, traceFields)
		return nil, fmt.Errorf("[%s] %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		log.Printf("[LLM-stream] done %s model=%s status=%d elapsed=%s body_len=%d %s", endpoint, cfg.Model, resp.StatusCode, time.Since(startedAt).Round(time.Millisecond), len(body), traceFields)
		return nil, fmt.Errorf("HTTP %d: body_len=%d", resp.StatusCode, len(body))
	}

	// Peek at the first bytes to detect SSE vs JSON without buffering the
	// entire response. SSE starts with "data:" (possibly after whitespace);
	// JSON starts with "{" or "[".
	peekReader := bufio.NewReaderSize(resp.Body, 4096)
	peeked, _ := peekReader.Peek(64)
	trimmed := strings.TrimLeft(string(peeked), " \t\r\n")

	if strings.HasPrefix(trimmed, "data:") || strings.HasPrefix(trimmed, "event:") {
		// True SSE stream  - parse incrementally, calling onToken per chunk.
		result, parseErr := parseSSEStream(peekReader, onToken)
		log.Printf("[LLM-stream] done %s model=%s status=%d elapsed=%s parse_err=%v %s", endpoint, cfg.Model, resp.StatusCode, time.Since(startedAt).Round(time.Millisecond), parseErr, traceFields)
		return result, parseErr
	}

	// Fallback: server returned plain JSON despite stream=true.
	// Read the full body and parse as non-stream response.
	body, err := io.ReadAll(io.LimitReader(peekReader, 512*1024))
	if err != nil {
		log.Printf("[LLM-stream] done %s model=%s status=%d elapsed=%s read_err=%v %s", endpoint, cfg.Model, resp.StatusCode, time.Since(startedAt).Round(time.Millisecond), err, traceFields)
		return nil, fmt.Errorf("read response: %w", err)
	}
	result, err := ParseNonStreamOpenAIResponseBody(body)
	if err != nil {
		log.Printf("[LLM-stream] done %s model=%s status=%d elapsed=%s body_len=%d parse_err=%v %s", endpoint, cfg.Model, resp.StatusCode, time.Since(startedAt).Round(time.Millisecond), len(body), err, traceFields)
		return nil, err
	}
	if onToken != nil && len(result.Choices) > 0 && result.Choices[0].Message.Content != "" {
		onToken(result.Choices[0].Message.Content)
	}
	log.Printf("[LLM-stream] done %s model=%s status=%d elapsed=%s body_len=%d fallback=json %s", endpoint, cfg.Model, resp.StatusCode, time.Since(startedAt).Round(time.Millisecond), len(body), traceFields)
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", cfg.UserAgent())
	req.Header.Set("anthropic-version", "2023-06-01")
	corelib.SetAnthropicAuthHeaders(req, cfg.Key)
	corelib.SetCodeGenClientNameHeaderIfNeededWithName(req, cfg.UserAgent())

	traceFields := RequestTraceLogFields(ctx)
	log.Printf("[LLM-stream] POST %s model=%s protocol=anthropic %s", endpoint, cfg.Model, traceFields)
	startedAt := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[LLM-stream] done %s model=%s protocol=anthropic status=error elapsed=%s err=%v %s", endpoint, cfg.Model, time.Since(startedAt).Round(time.Millisecond), err, traceFields)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		log.Printf("[LLM-stream] done %s model=%s protocol=anthropic status=%d elapsed=%s body_len=%d %s", endpoint, cfg.Model, resp.StatusCode, time.Since(startedAt).Round(time.Millisecond), len(body), traceFields)
		return nil, fmt.Errorf("HTTP %d: body_len=%d", resp.StatusCode, len(body))
	}

	// Peek at the first bytes to detect SSE vs JSON.
	peekReader := bufio.NewReaderSize(resp.Body, 4096)
	peeked, _ := peekReader.Peek(64)
	trimmed := strings.TrimLeft(string(peeked), " \t\r\n")

	if strings.HasPrefix(trimmed, "event:") || strings.HasPrefix(trimmed, "data:") {
		// True SSE stream  - parse incrementally.
		result, parseErr := parseAnthropicSSEStream(peekReader, onToken)
		log.Printf("[LLM-stream] done %s model=%s protocol=anthropic status=%d elapsed=%s parse_err=%v %s", endpoint, cfg.Model, resp.StatusCode, time.Since(startedAt).Round(time.Millisecond), parseErr, traceFields)
		return result, parseErr
	}

	// Fallback: server returned plain JSON despite stream=true.
	body, err := io.ReadAll(io.LimitReader(peekReader, 512*1024))
	if err != nil {
		log.Printf("[LLM-stream] done %s model=%s protocol=anthropic status=%d elapsed=%s read_err=%v %s", endpoint, cfg.Model, resp.StatusCode, time.Since(startedAt).Round(time.Millisecond), err, traceFields)
		return nil, fmt.Errorf("read response: %w", err)
	}
	result, err := parseAnthropicResponseBody(body)
	if err != nil {
		log.Printf("[LLM-stream] done %s model=%s protocol=anthropic status=%d elapsed=%s body_len=%d parse_err=%v %s", endpoint, cfg.Model, resp.StatusCode, time.Since(startedAt).Round(time.Millisecond), len(body), err, traceFields)
		return nil, err
	}
	if onToken != nil && len(result.Choices) > 0 && result.Choices[0].Message.Content != "" {
		onToken(result.Choices[0].Message.Content)
	}
	log.Printf("[LLM-stream] done %s model=%s protocol=anthropic status=%d elapsed=%s body_len=%d fallback=json %s", endpoint, cfg.Model, resp.StatusCode, time.Since(startedAt).Round(time.Millisecond), len(body), traceFields)
	return result, nil
}

// parseSSEStream reads an OpenAI-compatible SSE stream, calling onToken for
// each text delta, and returns the fully assembled Response.
func parseSSEStream(body io.Reader, onToken TokenCallback) (*Response, error) {
	var contentBuf, reasoningBuf strings.Builder
	var finishReason string
	var usage *Usage

	type toolCallAcc struct {
		ID      string
		Type    string
		Name    string
		ArgsBuf strings.Builder
	}
	toolCalls := make(map[int]*toolCallAcc)

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
			if onToken != nil {
				onToken(delta.Content)
			}
		}
		if delta.ReasoningContent != "" {
			reasoningBuf.WriteString(delta.ReasoningContent)
		}

		if chunk.Choices[0].FinishReason != "" {
			finishReason = chunk.Choices[0].FinishReason
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
		Choices: []Choice{{Message: msg, FinishReason: finishReason}},
		Usage:   usage,
	}, nil
}

// parseAnthropicSSEStream reads an Anthropic SSE stream, calling onToken for
// each text delta, and returns the fully assembled Response.
func parseAnthropicSSEStream(body io.Reader, onToken TokenCallback) (*Response, error) {
	var contentBuf strings.Builder
	var finishReason string
	var usage *Usage

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
				if onToken != nil {
					onToken(event.Delta.Text)
				}
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

	return &Response{
		Choices: []Choice{{Message: msg, FinishReason: finishReason}},
		Usage:   usage,
	}, nil
}
