package windsurf

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Upstream speaks the Devin Cascade API while presenting OpenAI chat and
// model endpoints to CodexProxy.
type Upstream struct{}

func (Upstream) DoModels(ctx context.Context, endpoint, apiKey, _ string) (*http.Response, error) {
	models, err := ListModels(ctx, APIRoot(endpoint), apiKey)
	if err != nil {
		return errorResponse(http.StatusBadGateway, err.Error()), nil
	}
	data := make([]map[string]string, 0, len(models))
	for _, model := range models {
		data = append(data, map[string]string{"id": model.ID, "name": model.Name})
	}
	raw, _ := json.Marshal(map[string]any{"object": "list", "data": data})
	return jsonResponse(http.StatusOK, raw), nil
}

func (Upstream) DoChatCompletions(ctx context.Context, endpoint, apiKey, _ string, body []byte, _ string, stream bool) (*http.Response, error) {
	req, err := parseOpenAIChat(body)
	if err != nil {
		return errorResponse(http.StatusBadRequest, err.Error()), nil
	}
	if !stream {
		return completeChat(ctx, APIRoot(endpoint), apiKey, req)
	}
	return streamChat(ctx, APIRoot(endpoint), apiKey, req)
}

// APIRoot drops the OpenAI suffix CodexProxy appends to the Devin API host.
func APIRoot(endpoint string) string {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed.Host == "" {
		return strings.TrimRight(strings.TrimSpace(endpoint), "/")
	}
	path := parsed.Path
	for _, suffix := range []string{"/chat/completions", "/models", "/v1"} {
		path = strings.TrimSuffix(path, suffix)
	}
	parsed.Path = path
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/")
}

func parseOpenAIChat(body []byte) (chatRequest, error) {
	var raw struct {
		Model               string          `json:"model"`
		Messages            []openAIMessage `json:"messages"`
		Tools               []openAITool    `json:"tools"`
		MaxTokens           int             `json:"max_tokens"`
		MaxCompletionTokens int             `json:"max_completion_tokens"`
		Temperature         *float64        `json:"temperature"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return chatRequest{}, fmt.Errorf("无法解析对话请求")
	}
	req := chatRequest{model: strings.TrimSpace(raw.Model), temperature: raw.Temperature}
	req.maxTokens = raw.MaxCompletionTokens
	if req.maxTokens <= 0 {
		req.maxTokens = raw.MaxTokens
	}
	for _, msg := range raw.Messages {
		item := chatMessage{role: strings.TrimSpace(msg.Role), text: messageText(msg.Content), toolCallID: msg.ToolCallID}
		for _, call := range msg.ToolCalls {
			name := call.Function.Name
			args := call.Function.Arguments
			if name == "" {
				name = call.Name
			}
			if args == "" {
				args = call.Arguments
			}
			item.toolCalls = append(item.toolCalls, toolCall{id: call.ID, name: name, args: args})
		}
		req.messages = append(req.messages, item)
	}
	for _, tool := range raw.Tools {
		name := tool.Function.Name
		if name == "" {
			name = tool.Name
		}
		if name == "" {
			continue
		}
		params := "{}"
		if len(tool.Function.Parameters) > 0 {
			params = string(tool.Function.Parameters)
		} else if len(tool.Parameters) > 0 {
			params = string(tool.Parameters)
		}
		desc := tool.Function.Description
		if desc == "" {
			desc = tool.Description
		}
		req.tools = append(req.tools, toolDef{name: name, description: desc, parameters: params})
	}
	return req, nil
}

type openAIMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	ToolCallID string          `json:"tool_call_id"`
	ToolCalls  []struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
		Function  struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
}

type openAITool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
	Function    struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

func messageText(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		var b strings.Builder
		for _, part := range parts {
			if part.Type == "" || part.Type == "text" {
				if b.Len() > 0 && part.Text != "" {
					b.WriteByte('\n')
				}
				b.WriteString(part.Text)
			}
		}
		return b.String()
	}
	return ""
}

func completeChat(ctx context.Context, apiServer, apiKey string, req chatRequest) (*http.Response, error) {
	var text, reasoning strings.Builder
	type call struct {
		id, name, args string
	}
	var calls []call
	finish := "stop"
	var prompt, completion int
	err := forEachChatEvent(ctx, apiServer, apiKey, req, func(event chatEvent) error {
		switch event.kind {
		case "text":
			text.WriteString(event.text)
		case "reasoning":
			reasoning.WriteString(event.text)
		case "tool_start":
			calls = append(calls, call{id: event.id, name: event.name})
		case "tool_args":
			if len(calls) == 0 || (event.id != "" && calls[len(calls)-1].id != event.id) {
				calls = append(calls, call{id: event.id})
			}
			calls[len(calls)-1].args += event.text
		case "finish":
			finish = event.reason
		case "usage":
			prompt = event.promptTokens
			completion = event.completionTokens
		}
		return nil
	})
	if err != nil {
		return errorResponse(http.StatusBadGateway, err.Error()), nil
	}
	if finish == "stop" && len(calls) > 0 {
		finish = "tool_calls"
	}
	message := map[string]any{"role": "assistant", "content": text.String()}
	if reasoning.Len() > 0 {
		message["reasoning_content"] = reasoning.String()
	}
	if len(calls) > 0 {
		toolCalls := make([]map[string]any, 0, len(calls))
		for _, call := range calls {
			toolCalls = append(toolCalls, map[string]any{
				"id":   call.id,
				"type": "function",
				"function": map[string]string{
					"name":      call.name,
					"arguments": call.args,
				},
			})
		}
		message["tool_calls"] = toolCalls
	}
	payload := map[string]any{
		"id":      "chatcmpl-devin",
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   req.model,
		"choices": []map[string]any{{
			"index":         0,
			"message":       message,
			"finish_reason": finish,
		}},
	}
	if prompt > 0 || completion > 0 {
		payload["usage"] = map[string]int{
			"prompt_tokens":     prompt,
			"completion_tokens": completion,
			"total_tokens":      prompt + completion,
		}
	}
	raw, _ := json.Marshal(payload)
	return jsonResponse(http.StatusOK, raw), nil
}

func streamChat(ctx context.Context, apiServer, apiKey string, req chatRequest) (*http.Response, error) {
	body, err := openChat(ctx, apiServer, apiKey, req)
	if err != nil {
		return errorResponse(http.StatusBadGateway, err.Error()), nil
	}
	pr, pw := io.Pipe()
	go func() {
		defer body.Close()
		write := func(payload map[string]any) error {
			raw, err := json.Marshal(payload)
			if err != nil {
				return err
			}
			if _, err := pw.Write([]byte("data: ")); err != nil {
				return err
			}
			if _, err := pw.Write(raw); err != nil {
				return err
			}
			_, err = pw.Write([]byte("\n\n"))
			return err
		}
		id := "chatcmpl-devin"
		created := time.Now().Unix()
		var prompt, completion int
		finish := "stop"
		toolIndex := map[string]int{}
		nextIndex := 0
		err := readChatEvents(body, func(event chatEvent) error {
			chunk := map[string]any{
				"id": id, "object": "chat.completion.chunk", "created": created, "model": req.model,
			}
			switch event.kind {
			case "text":
				chunk["choices"] = []map[string]any{{"index": 0, "delta": map[string]string{"content": event.text}}}
			case "reasoning":
				chunk["choices"] = []map[string]any{{"index": 0, "delta": map[string]string{"reasoning_content": event.text}}}
			case "tool_start":
				idx, ok := toolIndex[event.id]
				if !ok {
					idx = nextIndex
					toolIndex[event.id] = idx
					nextIndex++
				}
				chunk["choices"] = []map[string]any{{"index": 0, "delta": map[string]any{"tool_calls": []map[string]any{{
					"index": idx, "id": event.id, "type": "function",
					"function": map[string]string{"name": event.name, "arguments": ""},
				}}}}}
			case "tool_args":
				idx, ok := toolIndex[event.id]
				if !ok {
					idx = nextIndex
					if event.id != "" {
						toolIndex[event.id] = idx
					}
					nextIndex++
				}
				chunk["choices"] = []map[string]any{{"index": 0, "delta": map[string]any{"tool_calls": []map[string]any{{
					"index": idx, "function": map[string]string{"arguments": event.text},
				}}}}}
			case "finish":
				finish = event.reason
				return nil
			case "usage":
				prompt, completion = event.promptTokens, event.completionTokens
				return nil
			default:
				return nil
			}
			return write(chunk)
		})
		if err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		final := map[string]any{
			"id": id, "object": "chat.completion.chunk", "created": created, "model": req.model,
			"choices": []map[string]any{{"index": 0, "delta": map[string]any{}, "finish_reason": finish}},
		}
		if prompt > 0 || completion > 0 {
			final["usage"] = map[string]int{
				"prompt_tokens": prompt, "completion_tokens": completion, "total_tokens": prompt + completion,
			}
		}
		if err := write(final); err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		_, _ = pw.Write([]byte("data: [DONE]\n\n"))
		_ = pw.Close()
	}()
	header := make(http.Header)
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     header,
		Body:       pr,
	}, nil
}

func jsonResponse(status int, body []byte) *http.Response {
	header := make(http.Header)
	header.Set("Content-Type", "application/json")
	return &http.Response{
		StatusCode:    status,
		Header:        header,
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
	}
}

func errorResponse(status int, message string) *http.Response {
	raw, _ := json.Marshal(map[string]any{"error": map[string]string{"message": message, "type": "devin_error"}})
	return jsonResponse(status, raw)
}
