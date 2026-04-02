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
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/freeproxy"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// TokenCallback is called with each text delta from the LLM streaming response.
type TokenCallback = llm.TokenCallback

type tokenStreamFilter struct {
	writeFn func(string)
	flushFn func()
}

func (f tokenStreamFilter) Callback() TokenCallback {
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
	return llm.StripFunctionCalls(s)
}

func stripXMLToolCalls(s string) string {
	return llm.StripXMLToolCalls(s)
}

// ---------------------------------------------------------------------------
// Filter factory functions using corelib/llm
// ---------------------------------------------------------------------------

func newThinkFilter(downstream TokenCallback) tokenStreamFilter {
	f := llm.NewThinkFilter(downstream)
	return tokenStreamFilter{writeFn: f, flushFn: func() {}}
}

func newFuncCallFilter(downstream TokenCallback) tokenStreamFilter {
	f := llm.NewTagFilter(downstream, "<|FunctionCallBegin|>", "<|FunctionCallEnd|>")
	return tokenStreamFilter{writeFn: f, flushFn: func() {}}
}

func newToolCallFilter(downstream TokenCallback) tokenStreamFilter {
	f := llm.NewTagFilter(downstream, "<tool_call>", "</tool_call>")
	return tokenStreamFilter{writeFn: f, flushFn: func() {}}
}

// ---------------------------------------------------------------------------
// doLLMRequestStream sends a streaming LLM request.
// The ctx parameter carries cancellation from the LoopContext so that
// in-flight HTTP requests are aborted promptly when the user cancels.
func (h *IMMessageHandler) doLLMRequestStream(
	reqCtx context.Context,
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
		return h.doAnthropicLLMRequestStream(reqCtx, cfg, messages, tools, httpClient, onToken)
	}
	return h.doOpenAILLMRequestStream(reqCtx, cfg, messages, tools, httpClient, onToken)
}

// ---------------------------------------------------------------------------
// OpenAI SSE streaming
// ---------------------------------------------------------------------------

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
	Usage *llm.Usage `json:"usage,omitempty"`
}

func (h *IMMessageHandler) doOpenAILLMRequestStream(
	reqCtx context.Context,
	cfg MaclawLLMConfig,
	messages []interface{},
	tools []map[string]interface{},
	httpClient *http.Client,
	onToken TokenCallback,
) (*llmResponse, error) {
	endpoint := strings.TrimRight(cfg.URL, "/") + "/chat/completions"
	log.Printf("[LLM Stream] POST %s model=%s protocol=%s", endpoint, cfg.Model, cfg.Protocol)

	if needsSystemMerge(cfg) {
		messages = mergeSystemIntoUser(messages)
	}

	reqBody := map[string]interface{}{
		"model":    cfg.Model,
		"messages": messages,
		"stream":   true,
		"stream_options": map[string]interface{}{
			"include_usage": true,
		},
	}
	if len(tools) > 0 {
		reqBody["tools"] = tools
	}

	data, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(data))
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
		return nil, fmt.Errorf("[%s] %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("HTTP 404: %s (endpoint=%s, model=%s, protocol=%s)", string(body), endpoint, cfg.Model, cfg.Protocol)
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
		return parseNonStreamOpenAIResponse(resp, data)
	}

	tcf := newToolCallFilter(onToken)
	fcf := newFuncCallFilter(tcf.Callback())
	tf := newThinkFilter(fcf.Callback())
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
	var usage *llm.Usage

	scanner := bufio.NewScanner(bodyReader)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
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
		}
		for _, tc := range delta.ToolCalls {
			acc, ok := toolAccums[tc.Index]
			if !ok {
				acc = &toolAccum{}
				toolAccums[tc.Index] = acc
			}
			if tc.ID != "" { acc.id = tc.ID }
			if tc.Type != "" { acc.typ = tc.Type }
			if tc.Function.Name != "" { acc.name.WriteString(tc.Function.Name) }
			if tc.Function.Arguments != "" { acc.args.WriteString(tc.Function.Arguments) }
		}
		if choice.FinishReason != nil {
			finishReason = *choice.FinishReason
		}
	}

	tf.Flush()
	fcf.Flush()
	tcf.Flush()
	content := stripXMLToolCalls(stripFunctionCalls(stripThinkTags(contentBuf.String())))
	reasoning := reasoningBuf.String()
	if content == "" && reasoning != "" {
		content = stripXMLToolCalls(stripFunctionCalls(stripThinkTags(reasoning)))
	}
	msg := llmMessage{
		Role:             "assistant",
		Content:          content,
		ReasoningContent: reasoning,
	}
	if len(toolAccums) > 0 {
		maxIdx := 0
		for idx := range toolAccums { if idx > maxIdx { maxIdx = idx } }
		for i := 0; i <= maxIdx; i++ {
			if acc, ok := toolAccums[i]; ok {
				msg.ToolCalls = append(msg.ToolCalls, llmToolCall{
					ID:   acc.id, Type: acc.typ,
					Function: struct { Name string `json:"name"`; Arguments string `json:"arguments"` }{
						Name: acc.name.String(), Arguments: acc.args.String(),
					},
				})
			}
		}
	}
	if len(msg.ToolCalls) == 0 {
		if xmlCalls := freeproxy.ParseXMLToolCalls(contentBuf.String()); len(xmlCalls) > 0 {
			for _, xc := range xmlCalls {
				msg.ToolCalls = append(msg.ToolCalls, llmToolCall{
					ID: xc.ID, Type: xc.Type,
					Function: struct { Name string `json:"name"`; Arguments string `json:"arguments"` }{
						Name: xc.Function.Name, Arguments: xc.Function.Arguments,
					},
				})
			}
			finishReason = "tool_calls"
		}
	}

	if finishReason == "" { finishReason = "stop" }

	return &llmResponse{
		Choices: []llmChoice{{Message: msg, FinishReason: finishReason}},
		Usage:   usage,
	}, nil
}

func parseNonStreamOpenAIResponse(resp *http.Response, requestBody []byte) (*llmResponse, error) {
	return llm.ParseNonStreamOpenAIResponse(resp)
}

// ---------------------------------------------------------------------------
// Anthropic SSE streaming
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) doAnthropicLLMRequestStream(
	reqCtx context.Context,
	cfg MaclawLLMConfig,
	messages []interface{},
	tools []map[string]interface{},
	httpClient *http.Client,
	onToken TokenCallback,
) (*llmResponse, error) {
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
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", cfg.UserAgent())
	req.Header.Set("anthropic-version", "2023-06-01")
	corelib.SetAnthropicAuthHeaders(req, cfg.Key)

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
	var usage *llmUsage

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
				Type         string `json:"type"`
				Text         string `json:"text,omitempty"`
				PartialJSON  string `json:"partial_json,omitempty"`
				StopReason   string `json:"stop_reason,omitempty"`
			} `json:"delta,omitempty"`
			Message struct {
				StopReason string    `json:"stop_reason,omitempty"`
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
			}
			if evt.Delta.Type == "input_json_delta" && evt.Delta.PartialJSON != "" {
				acc.toolArgs.WriteString(evt.Delta.PartialJSON)
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
				usage = &llmUsage{
					InputTokens:  evt.Message.Usage.InputTokens,
					PromptTokens: evt.Message.Usage.InputTokens,
				}
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
		Usage:   usage,
	}, nil
}

// parseNonStreamAnthropicResponse handles the fallback case where the provider
// returned a normal JSON response instead of SSE for Anthropic protocol.
func parseNonStreamAnthropicResponse(resp *http.Response, requestBody []byte) (*llmResponse, error) {
	return llm.ParseNonStreamAnthropicResponse(resp)
}
