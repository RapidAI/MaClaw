package codegenproxy

import (
	"encoding/json"
	"strings"
)

// ── Anthropic request/response types ──

type anthropicRequest struct {
	Model     string                 `json:"model"`
	Messages  []anthropicMessage     `json:"messages"`
	System    interface{}            `json:"system,omitempty"` // string or []block
	MaxTokens int                    `json:"max_tokens"`
	Stream    bool                   `json:"stream"`
	Tools     []anthropicTool        `json:"tools,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

type anthropicMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string or []contentBlock
}

type anthropicContentBlock struct {
	Type      string                 `json:"type"`
	Text      string                 `json:"text,omitempty"`
	ID        string                 `json:"id,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Input     map[string]interface{} `json:"input,omitempty"`
	ToolUseID string                 `json:"tool_use_id,omitempty"`
	Content   interface{}            `json:"content,omitempty"` // for tool_result
}

type anthropicTool struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	InputSchema interface{} `json:"input_schema"`
}

type anthropicResponse struct {
	ID         string                  `json:"id"`
	Type       string                  `json:"type"`
	Role       string                  `json:"role"`
	Model      string                  `json:"model"`
	Content    []anthropicContentBlock `json:"content"`
	StopReason string                  `json:"stop_reason"`
	Usage      *anthropicUsage         `json:"usage,omitempty"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// ── OpenAI request/response types ──

type openaiChatRequest struct {
	Model        string           `json:"model"`
	Messages     []openaiMessage  `json:"messages"`
	Stream       bool             `json:"stream,omitempty"`
	MaxTokens    int              `json:"max_tokens,omitempty"`
	Tools        []openaiTool     `json:"tools,omitempty"`
	Functions    []openaiFunction `json:"functions,omitempty"`
	FunctionCall interface{}      `json:"function_call,omitempty"`
}

type openaiMessage struct {
	Role         string                    `json:"role"`
	Content      interface{}               `json:"content,omitempty"` // string or null
	ToolCalls    []openaiToolCall          `json:"tool_calls,omitempty"`
	ToolCallID   string                    `json:"tool_call_id,omitempty"`
	FunctionCall *openaiLegacyFunctionCall `json:"function_call,omitempty"`
}

type openaiTool struct {
	Type     string         `json:"type"`
	Function openaiFunction `json:"function"`
}

type openaiFunction struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Parameters  interface{} `json:"parameters"`
}

type openaiToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openaiLegacyFunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type openaiChatResponse struct {
	ID      string         `json:"id"`
	Choices []openaiChoice `json:"choices"`
	Usage   *openaiUsage   `json:"usage,omitempty"`
}

type openaiChoice struct {
	Message      openaiMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type openaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type openaiStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content      string                    `json:"content,omitempty"`
			ToolCalls    []streamToolCallDelta     `json:"tool_calls,omitempty"`
			FunctionCall *openaiLegacyFunctionCall `json:"function_call,omitempty"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason,omitempty"`
	} `json:"choices"`
}

type streamToolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

type streamToolCall struct {
	ID   string
	Name string
}

// convertAnthropicToOpenAI converts an Anthropic Messages API request to OpenAI chat completions format.
func convertAnthropicToOpenAI(req anthropicRequest) openaiChatRequest {
	result := openaiChatRequest{
		Model:     req.Model,
		Stream:    req.Stream,
		MaxTokens: req.MaxTokens,
	}

	// Convert system prompt
	if req.System != nil {
		systemText := extractSystemText(req.System)
		if systemText != "" {
			result.Messages = append(result.Messages, openaiMessage{
				Role:    "system",
				Content: systemText,
			})
		}
	}

	// Convert messages
	for _, msg := range req.Messages {
		converted := convertAnthropicMessage(msg)
		result.Messages = append(result.Messages, converted...)
	}

	// Convert tools
	for _, t := range req.Tools {
		result.Tools = append(result.Tools, openaiTool{
			Type: "function",
			Function: openaiFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}

	return result
}

func extractSystemText(system interface{}) string {
	switch v := system.(type) {
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, item := range v {
			if block, ok := item.(map[string]interface{}); ok {
				if text, ok := block["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func convertAnthropicMessage(msg anthropicMessage) []openaiMessage {
	switch msg.Role {
	case "user":
		return convertUserMessage(msg)
	case "assistant":
		return convertAssistantMessage(msg)
	default:
		return []openaiMessage{{Role: msg.Role, Content: contentToString(msg.Content)}}
	}
}

func convertUserMessage(msg anthropicMessage) []openaiMessage {
	// Content can be a string or array of blocks
	switch content := msg.Content.(type) {
	case string:
		return []openaiMessage{{Role: "user", Content: content}}
	case []interface{}:
		var userParts []string
		var toolResults []openaiMessage
		for _, item := range content {
			block, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			blockType, _ := block["type"].(string)
			switch blockType {
			case "text":
				if text, ok := block["text"].(string); ok {
					userParts = append(userParts, text)
				}
			case "tool_result":
				toolCallID, _ := block["tool_use_id"].(string)
				resultContent := extractToolResultContent(block["content"])
				toolResults = append(toolResults, openaiMessage{
					Role:       "tool",
					Content:    resultContent,
					ToolCallID: toolCallID,
				})
			}
		}
		var msgs []openaiMessage
		// Tool results come first (they respond to the previous assistant message)
		msgs = append(msgs, toolResults...)
		if len(userParts) > 0 {
			msgs = append(msgs, openaiMessage{Role: "user", Content: strings.Join(userParts, "\n")})
		}
		if len(msgs) == 0 {
			msgs = append(msgs, openaiMessage{Role: "user", Content: ""})
		}
		return msgs
	}
	return []openaiMessage{{Role: "user", Content: contentToString(msg.Content)}}
}

func convertAssistantMessage(msg anthropicMessage) []openaiMessage {
	switch content := msg.Content.(type) {
	case string:
		return []openaiMessage{{Role: "assistant", Content: content}}
	case []interface{}:
		result := openaiMessage{Role: "assistant"}
		var textParts []string
		for _, item := range content {
			block, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			blockType, _ := block["type"].(string)
			switch blockType {
			case "text":
				if text, ok := block["text"].(string); ok {
					textParts = append(textParts, text)
				}
			case "tool_use":
				id, _ := block["id"].(string)
				name, _ := block["name"].(string)
				input, _ := block["input"]
				argsJSON, _ := json.Marshal(input)
				result.ToolCalls = append(result.ToolCalls, openaiToolCall{
					ID:   id,
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      name,
						Arguments: string(argsJSON),
					},
				})
			}
		}
		if len(textParts) > 0 {
			result.Content = strings.Join(textParts, "\n")
		}
		return []openaiMessage{result}
	}
	return []openaiMessage{{Role: "assistant", Content: contentToString(msg.Content)}}
}

func extractToolResultContent(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, item := range v {
			if block, ok := item.(map[string]interface{}); ok {
				if text, ok := block["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func contentToString(content interface{}) string {
	if s, ok := content.(string); ok {
		return s
	}
	return ""
}

// convertOpenAIToAnthropic converts an OpenAI chat completion response to Anthropic Messages format.
func convertOpenAIToAnthropic(resp openaiChatResponse, model string) anthropicResponse {
	result := anthropicResponse{
		ID:    resp.ID,
		Type:  "message",
		Role:  "assistant",
		Model: model,
	}

	if resp.Usage != nil {
		result.Usage = &anthropicUsage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		}
	}

	if len(resp.Choices) == 0 {
		result.StopReason = "end_turn"
		return result
	}

	choice := resp.Choices[0]

	result.StopReason = "end_turn"
	if choice.FinishReason == "length" {
		result.StopReason = "max_tokens"
	}

	// Convert text content
	if text, ok := choice.Message.Content.(string); ok && text != "" {
		result.Content = append(result.Content, anthropicContentBlock{
			Type: "text",
			Text: text,
		})
	}

	// Convert tool calls
	hasToolUse := false
	for _, tc := range choice.Message.ToolCalls {
		input, ok := parseOpenAIToolArguments(tc.Function.Arguments)
		if !ok {
			continue
		}
		hasToolUse = true
		result.Content = append(result.Content, anthropicContentBlock{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: input,
		})
	}
	if fc := choice.Message.FunctionCall; fc != nil && fc.Name != "" {
		input, ok := parseOpenAIToolArguments(fc.Arguments)
		if ok {
			hasToolUse = true
			result.Content = append(result.Content, anthropicContentBlock{
				Type:  "tool_use",
				ID:    "call_legacy_function",
				Name:  fc.Name,
				Input: input,
			})
		}
	}
	if hasToolUse {
		result.StopReason = "tool_use"
	}

	if len(result.Content) == 0 {
		result.Content = []anthropicContentBlock{{Type: "text", Text: ""}}
	}

	return result
}

func parseOpenAIToolArguments(raw string) (map[string]interface{}, bool) {
	normalized, ok := normalizeOpenAIToolArguments(raw)
	if !ok {
		return nil, false
	}
	var input map[string]interface{}
	if err := json.Unmarshal([]byte(normalized), &input); err != nil || input == nil {
		return nil, false
	}
	return input, true
}

func normalizeOpenAIToolArguments(raw string) (string, bool) {
	args := strings.TrimSpace(raw)
	if args == "" {
		return "{}", true
	}
	for i := 0; i < 2; i++ {
		var input map[string]interface{}
		if err := json.Unmarshal([]byte(args), &input); err == nil && input != nil {
			return args, true
		}
		var encoded string
		if err := json.Unmarshal([]byte(args), &encoded); err != nil {
			return "", false
		}
		args = strings.TrimSpace(encoded)
	}
	return "", false
}
