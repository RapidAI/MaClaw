package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

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

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
		return nil, dumpLLMContext(resp.StatusCode, string(body), data, h.app.GetTempDir())
	}

	tcf := newToolCallFilter(onToken)
	fcf := newFuncCallFilter(tcf.Callback())
	tf := newThinkFilter(fcf.Callback())

	var contentBuf strings.Builder
	type toolBlock struct {
		id   string
		name string
		args strings.Builder
	}
	toolBlocks := make(map[int]*toolBlock)
	var stopReason string
	var usage *llm.Usage

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))

		var evt struct {
			Type         string `json:"type"`
			Index        int    `json:"index"`
			ContentBlock struct {
				Type  string      `json:"type"`
				ID    string      `json:"id,omitempty"`
				Name  string      `json:"name,omitempty"`
				Text  string      `json:"text,omitempty"`
				Input interface{} `json:"input,omitempty"`
			} `json:"content_block,omitempty"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text,omitempty"`
				PartialJSON string `json:"partial_json,omitempty"`
			} `json:"delta,omitempty"`
			Message struct {
				StopReason string `json:"stop_reason,omitempty"`
				Usage      *struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage,omitempty"`
			} `json:"message,omitempty"`
		} // evt
		if err := json.Unmarshal([]byte(payload), &evt); err != nil {
			continue
		}

		switch evt.Type {
		case "message_start":
			if evt.Message.Usage != nil {
				usage = &llm.Usage{
					InputTokens:  evt.Message.Usage.InputTokens,
					OutputTokens: evt.Message.Usage.OutputTokens,
				}
			}
		case "content_block_start":
			if evt.ContentBlock.Type == "tool_use" {
				toolBlocks[evt.Index] = &toolBlock{
					id:   evt.ContentBlock.ID,
					name: evt.ContentBlock.Name,
				}
			}
		case "content_block_delta":
			if evt.Delta.Type == "text_delta" {
				contentBuf.WriteString(evt.Delta.Text)
				tf.Write(evt.Delta.Text)
			} else if evt.Delta.Type == "input_json_delta" {
				if b, ok := toolBlocks[evt.Index]; ok {
					b.args.WriteString(evt.Delta.PartialJSON)
				}
			}
		case "message_delta":
			if evt.Message.StopReason != "" {
				stopReason = evt.Message.StopReason
			}
			if evt.Message.Usage != nil {
				if usage == nil { usage = &llm.Usage{} }
				usage.OutputTokens = evt.Message.Usage.OutputTokens
			}
		}
	}

	msg := llmMessage{Role: "assistant"}
	tf.Flush()
	fcf.Flush()
	tcf.Flush()
	msg.Content = stripXMLToolCalls(stripFunctionCalls(stripThinkTags(contentBuf.String())))

	maxIdx := -1
	for idx := range toolBlocks { if idx > maxIdx { maxIdx = idx } }
	for i := 0; i <= maxIdx; i++ {
		if b, ok := toolBlocks[i]; ok {
			msg.ToolCalls = append(msg.ToolCalls, llmToolCall{
				ID: b.id, Type: "function",
				Function: struct { Name string `json:"name"`; Arguments string `json:"arguments"` }{
					Name: b.name, Arguments: b.args.String(),
				},
			})
		}
	}

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
		"stream_options": map[string]interface{}{
			"include_usage": true,
		},
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

func parseNonStreamAnthropicResponse(resp *http.Response, requestBody []byte) (*llmResponse, error) {
	return llm.ParseNonStreamAnthropicResponse(resp)
}
