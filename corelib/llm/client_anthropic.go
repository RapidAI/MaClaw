package llm

// client_anthropic.go provides a non-streaming Anthropic Messages API request
// function, parallel to DoOpenAIRequest in client.go.
//
// Used by corelib/agent.RunLoop when cfg.Protocol == "anthropic".

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/RapidAI/CodeClaw/corelib"
)

type AnthropicMessagesRequestOptions struct {
	Stream bool
	Tools  []map[string]interface{}
}

func BuildAnthropicMessagesRequestBody(
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	opts AnthropicMessagesRequestOptions,
) map[string]interface{} {
	converted := ConvertToAnthropicMessages(messages)
	reqBody := map[string]interface{}{
		"model":      cfg.Model,
		"messages":   converted.Messages,
		"max_tokens": 4096,
		"stream":     opts.Stream,
	}
	if converted.SystemText != "" {
		reqBody["system"] = converted.SystemText
	}
	if len(opts.Tools) > 0 {
		if at := ConvertToAnthropicTools(opts.Tools); len(at) > 0 {
			reqBody["tools"] = at
		}
	}
	return reqBody
}

func BuildAnthropicMessagesRequestData(
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	opts AnthropicMessagesRequestOptions,
) (endpoint string, body []byte, err error) {
	endpoint = corelib.AnthropicMessagesEndpoint(cfg.URL)
	body, err = json.Marshal(BuildAnthropicMessagesRequestBody(cfg, messages, opts))
	return endpoint, body, err
}

// DoAnthropicRequest sends a non-streaming Anthropic Messages API request.
// It converts OpenAI-style messages/tools to Anthropic format, sends the
// request, and converts the response back to the unified *Response type.
func DoAnthropicRequest(
	ctx context.Context,
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	tools []map[string]interface{},
	client *http.Client,
) (*Response, error) {
	endpoint, data, err := BuildAnthropicMessagesRequestData(cfg, messages, AnthropicMessagesRequestOptions{Stream: false, Tools: tools})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", cfg.UserAgent())
	req.Header.Set("anthropic-version", "2023-06-01")
	corelib.SetAnthropicAuthHeaders(req, cfg.Key)

	log.Printf("[LLM] POST %s model=%s protocol=anthropic", endpoint, cfg.Model)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("[%s] %w", endpoint, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if resp.StatusCode != http.StatusOK {
		msg := string(body)
		if len(msg) > 512 {
			msg = msg[:512] + "..."
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, msg)
	}

	// Parse Anthropic response format.
	return parseAnthropicResponseBody(body)
}

// parseAnthropicResponseBody parses a non-streaming Anthropic Messages API response.
func parseAnthropicResponseBody(body []byte) (*Response, error) {
	var raw struct {
		Content    []AnthropicContentBlock `json:"content"`
		StopReason string                  `json:"stop_reason"`
		Usage      *Usage                  `json:"usage,omitempty"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse anthropic response: %w", err)
	}

	msg := Message{Role: "assistant"}
	var textParts []string
	for _, block := range raw.Content {
		switch block.Type {
		case "text":
			textParts = append(textParts, block.Text)
		case "tool_use":
			argsJSON, _ := json.Marshal(block.Input)
			msg.ToolCalls = append(msg.ToolCalls, ToolCall{
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
	msg.Content = StripAllExtra(joinStrings(textParts))

	finishReason := "stop"
	if raw.StopReason == "tool_use" {
		finishReason = "tool_calls"
	} else if raw.StopReason == "max_tokens" {
		finishReason = "length"
	}

	return &Response{
		Choices: []Choice{{
			Message:      msg,
			FinishReason: finishReason,
		}},
		Usage: raw.Usage,
	}, nil
}

func joinStrings(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}
	result := parts[0]
	for _, p := range parts[1:] {
		result += "\n" + p
	}
	return result
}
