package codegenproxy

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	llmcompat "github.com/RapidAI/CodeClaw/corelib/llm"
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
	Strict      interface{} `json:"strict,omitempty"`
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

func convertOpenAIResponsesRequestToChat(body []byte) ([]byte, string, error) {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, "", err
	}
	model, _ := payload["model"].(string)
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, "", fmt.Errorf("model is required")
	}
	messages := responsesInputToChatMessages(payload["input"])
	if instructions, _ := payload["instructions"].(string); strings.TrimSpace(instructions) != "" {
		messages = append([]interface{}{map[string]interface{}{"role": "system", "content": instructions}}, messages...)
	}
	if len(messages) == 0 {
		return nil, model, fmt.Errorf("input is required")
	}
	chat := map[string]interface{}{
		"model":    model,
		"messages": messages,
	}
	if v, ok := payload["max_output_tokens"]; ok {
		chat["max_tokens"] = v
	}
	for _, key := range []string{"temperature", "top_p", "stream"} {
		if v, ok := payload[key]; ok {
			chat[key] = v
		}
	}
	if responseFormat := responsesTextFormatToChatResponseFormat(payload["text"]); responseFormat != nil {
		chat["response_format"] = responseFormat
	}
	if tools := responsesToolsToChatTools(payload["tools"]); len(tools) > 0 {
		chat["tools"] = tools
	}
	if toolChoice := responsesToolChoiceToChatToolChoice(payload["tool_choice"]); toolChoice != nil {
		chat["tool_choice"] = toolChoice
	}
	out, err := json.Marshal(chat)
	return out, model, err
}

func responsesToolChoiceToChatToolChoice(value interface{}) interface{} {
	if s, ok := value.(string); ok {
		choice := strings.TrimSpace(s)
		switch choice {
		case "auto", "none", "required":
			return choice
		default:
			return nil
		}
	}
	choice := codeGenProxyMapFromAny(value)
	if choice == nil {
		return nil
	}
	typ, _ := choice["type"].(string)
	typ = strings.TrimSpace(typ)
	if typ == "" {
		typ = "function"
	}
	if typ != "function" {
		return nil
	}
	name, _ := choice["name"].(string)
	if strings.TrimSpace(name) == "" {
		return nil
	}
	return map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": name}}
}

func responsesTextFormatToChatResponseFormat(value interface{}) map[string]interface{} {
	text := codeGenProxyMapFromAny(value)
	if text == nil {
		return nil
	}
	format := codeGenProxyMapFromAny(text["format"])
	if format == nil {
		return nil
	}
	typ, _ := format["type"].(string)
	typ = strings.TrimSpace(typ)
	switch typ {
	case "text", "json_object":
		return map[string]interface{}{"type": typ}
	case "json_schema":
		name, _ := format["name"].(string)
		if strings.TrimSpace(name) == "" {
			return nil
		}
		schema, ok := format["schema"]
		if !ok || schema == nil {
			return nil
		}
		jsonSchema := map[string]interface{}{"name": name, "schema": schema}
		if desc, _ := format["description"].(string); strings.TrimSpace(desc) != "" {
			jsonSchema["description"] = desc
		}
		if strict, ok := format["strict"].(bool); ok {
			jsonSchema["strict"] = strict
		}
		return map[string]interface{}{"type": "json_schema", "json_schema": jsonSchema}
	default:
		return nil
	}
}

func responsesInputToChatMessages(input interface{}) []interface{} {
	switch v := input.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []interface{}{map[string]interface{}{"role": "user", "content": v}}
	case []interface{}:
		messages := make([]interface{}, 0, len(v))
		for _, item := range v {
			msg := responsesInputItemToChatMessage(item)
			if msg != nil {
				messages = append(messages, msg)
			}
		}
		return messages
	default:
		msg := responsesInputItemToChatMessage(v)
		if msg == nil {
			return nil
		}
		return []interface{}{msg}
	}
}

func responsesInputItemToChatMessage(item interface{}) map[string]interface{} {
	m := codeGenProxyMapFromAny(item)
	if m == nil {
		return nil
	}
	itemType, _ := m["type"].(string)
	switch itemType {
	case "function_call":
		name, _ := m["name"].(string)
		callID, _ := m["call_id"].(string)
		if callID == "" {
			callID, _ = m["id"].(string)
		}
		arguments := responsesContentToJSONText(m["arguments"])
		if strings.TrimSpace(arguments) == "" {
			arguments = "{}"
		}
		if strings.TrimSpace(name) == "" || strings.TrimSpace(callID) == "" {
			return nil
		}
		return map[string]interface{}{
			"role":    "assistant",
			"content": nil,
			"tool_calls": []interface{}{map[string]interface{}{
				"id":   callID,
				"type": "function",
				"function": map[string]interface{}{
					"name":      name,
					"arguments": arguments,
				},
			}},
		}
	case "function_call_output":
		callID, _ := m["call_id"].(string)
		if callID == "" {
			callID, _ = m["id"].(string)
		}
		content := responsesOutputToText(m["output"])
		if strings.TrimSpace(content) == "" {
			content = responsesOutputToText(m["content"])
		}
		if strings.TrimSpace(content) == "" {
			return nil
		}
		if strings.TrimSpace(callID) == "" {
			return map[string]interface{}{"role": "user", "content": content}
		}
		return map[string]interface{}{"role": "tool", "tool_call_id": callID, "content": content}
	}
	role, _ := m["role"].(string)
	role = strings.TrimSpace(role)
	if role == "" {
		role = "user"
	}
	switch role {
	case "developer":
		role = "system"
	case "assistant", "system", "user", "tool":
	default:
		role = "user"
	}
	content := responsesContentToText(m["content"])
	if strings.TrimSpace(content) == "" {
		content = responsesContentToText(m["text"])
	}
	if strings.TrimSpace(content) == "" {
		content = responsesContentToText(m["input"])
	}
	if strings.TrimSpace(content) == "" {
		return nil
	}
	if role == "tool" {
		toolCallID, _ := m["tool_call_id"].(string)
		if toolCallID == "" {
			toolCallID, _ = m["call_id"].(string)
		}
		if toolCallID == "" {
			toolCallID, _ = m["id"].(string)
		}
		if strings.TrimSpace(toolCallID) != "" {
			return map[string]interface{}{"role": role, "tool_call_id": toolCallID, "content": content}
		}
	}
	return map[string]interface{}{"role": role, "content": content}
}

func responsesContentToText(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, item := range v {
			text := responsesContentToText(item)
			if strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]interface{}:
		for _, key := range []string{"text", "input_text", "output_text", "refusal", "output"} {
			if text, _ := v[key].(string); text != "" {
				return text
			}
		}
		return ""
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(data)
	}
}

func responsesContentToJSONText(value interface{}) string {
	if text, ok := value.(string); ok {
		return text
	}
	if text := responsesContentToText(value); strings.TrimSpace(text) != "" {
		return text
	}
	data, err := json.Marshal(value)
	if err != nil || string(data) == "null" {
		return ""
	}
	return string(data)
}

func responsesOutputToText(value interface{}) string {
	if text := responsesContentToText(value); strings.TrimSpace(text) != "" {
		return text
	}
	data, err := json.Marshal(value)
	if err != nil || string(data) == "null" {
		return ""
	}
	return string(data)
}

func responsesToolsToChatTools(value interface{}) []interface{} {
	items := codeGenProxySliceFromAny(value)
	if len(items) == 0 {
		return nil
	}
	tools := make([]interface{}, 0, len(items))
	for _, item := range items {
		tool := codeGenProxyMapFromAny(item)
		if tool == nil || tool["type"] != "function" {
			continue
		}
		name, _ := tool["name"].(string)
		if strings.TrimSpace(name) == "" {
			continue
		}
		fn := map[string]interface{}{
			"name":       name,
			"parameters": map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		}
		for _, key := range []string{"description", "parameters", "strict"} {
			if v, ok := tool[key]; ok {
				fn[key] = v
			}
		}
		tools = append(tools, map[string]interface{}{"type": "function", "function": fn})
	}
	return tools
}

func convertOpenAIChatResponseToResponses(body []byte, model string) ([]byte, error) {
	var chat openaiChatResponse
	if err := json.Unmarshal(body, &chat); err != nil {
		return nil, err
	}
	id := chat.ID
	if strings.TrimSpace(id) == "" {
		id = "resp_" + shortSHA256(string(body))
	}
	output := []interface{}{}
	if len(chat.Choices) > 0 {
		msg := chat.Choices[0].Message
		text := openAIMessageContentToString(msg.Content)
		hasFunctionOutput := len(msg.ToolCalls) > 0 || msg.FunctionCall != nil
		if text != "" || !hasFunctionOutput {
			output = append(output, map[string]interface{}{
				"id":     "msg_" + shortSHA256(id+":message"),
				"type":   "message",
				"status": "completed",
				"role":   "assistant",
				"content": []interface{}{map[string]interface{}{
					"type":        "output_text",
					"text":        text,
					"annotations": []interface{}{},
				}},
			})
		}
		for i, tc := range msg.ToolCalls {
			callID := tc.ID
			if callID == "" {
				callID = fmt.Sprintf("call_%s_%d", shortSHA256(id), i)
			}
			output = append(output, map[string]interface{}{
				"id":        "fc_" + shortSHA256(callID),
				"type":      "function_call",
				"status":    "completed",
				"call_id":   callID,
				"name":      tc.Function.Name,
				"arguments": tc.Function.Arguments,
			})
		}
		if msg.FunctionCall != nil {
			output = append(output, map[string]interface{}{
				"id":        "fc_" + shortSHA256(id+":legacy_function"),
				"type":      "function_call",
				"status":    "completed",
				"call_id":   "call_legacy_function",
				"name":      msg.FunctionCall.Name,
				"arguments": msg.FunctionCall.Arguments,
			})
		}
	}
	usage := map[string]interface{}{}
	if chat.Usage != nil {
		usage = map[string]interface{}{
			"input_tokens":  chat.Usage.PromptTokens,
			"output_tokens": chat.Usage.CompletionTokens,
			"total_tokens":  chat.Usage.TotalTokens,
		}
	}
	resp := map[string]interface{}{
		"id":                  id,
		"object":              "response",
		"created_at":          float64(time.Now().Unix()),
		"status":              "completed",
		"model":               model,
		"output":              output,
		"parallel_tool_calls": false,
		"tools":               []interface{}{},
		"tool_choice":         "auto",
		"temperature":         0,
		"top_p":               0,
		"metadata":            map[string]interface{}{},
		"instructions":        nil,
		"incomplete_details":  nil,
		"error":               nil,
		"usage":               usage,
	}
	return json.Marshal(resp)
}

func openAIMessageContentToString(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, item := range v {
			if text := openAIMessageContentToString(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]interface{}:
		for _, key := range []string{"text", "content"} {
			if text, _ := v[key].(string); text != "" {
				return text
			}
		}
		return ""
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(data)
	}
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
	case nil:
		return ""
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, item := range v {
			if block := codeGenProxyMapFromAny(item); block != nil {
				if text, ok := block["text"].(string); ok {
					parts = append(parts, text)
					continue
				}
				if nested := extractToolResultContent(block["content"]); strings.TrimSpace(nested) != "" {
					parts = append(parts, nested)
				}
				continue
			}
			if text := extractToolResultContent(item); strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	default:
		data, err := json.Marshal(v)
		if err == nil && len(data) > 0 && string(data) != "null" {
			return string(data)
		}
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

	// Some OpenAI-compatible providers emit tool calls as assistant text
	// instead of populating tool_calls. Convert those into Anthropic
	// tool_use blocks so Claude Code executes them instead of rendering XML/JSON.
	if !hasToolUse {
		if text, ok := choice.Message.Content.(string); ok && text != "" {
			if blocks, malformed := contentToolCallsToAnthropicBlocks(text); malformed {
				result.Content = append(result.Content, anthropicContentBlock{
					Type: "text",
					Text: llmcompat.MalformedContentToolCallErrorMsg,
				})
			} else if len(blocks) > 0 {
				hasToolUse = true
				result.Content = append(result.Content, blocks...)
			} else {
				result.Content = append(result.Content, anthropicContentBlock{
					Type: "text",
					Text: text,
				})
			}
		}
	} else if text, ok := choice.Message.Content.(string); ok && text != "" {
		if blocks, malformed := contentToolCallsToAnthropicBlocks(text); len(blocks) == 0 && !malformed {
			result.Content = append([]anthropicContentBlock{{
				Type: "text",
				Text: text,
			}}, result.Content...)
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

func contentToolCallsToAnthropicBlocks(content string) ([]anthropicContentBlock, bool) {
	if !mayContainContentToolCall(content) {
		return nil, false
	}
	calls, malformed := llmcompat.ParseContentToolCallsDetailed(content)
	if len(calls) == 0 {
		return nil, malformed
	}
	blocks := make([]anthropicContentBlock, 0, len(calls))
	skippedMalformed := false
	for i, call := range calls {
		name := strings.TrimSpace(call.Function.Name)
		if name == "" {
			skippedMalformed = true
			continue
		}
		input, ok := parseOpenAIToolArguments(call.Function.Arguments)
		if !ok {
			skippedMalformed = true
			continue
		}
		id := strings.TrimSpace(call.ID)
		if id == "" {
			id = fmt.Sprintf("call_content_%d_%d", time.Now().UnixNano(), i)
		}
		blocks = append(blocks, anthropicContentBlock{
			Type:  "tool_use",
			ID:    id,
			Name:  name,
			Input: input,
		})
	}
	return blocks, malformed || skippedMalformed
}

func mayContainContentToolCall(content string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, "<tool_call") ||
		strings.Contains(lower, "<turn: tool_call") ||
		strings.Contains(lower, "tool_call") ||
		strings.Contains(lower, "tool_calls") ||
		strings.Contains(lower, "function_call") ||
		strings.Contains(lower, `"function"`) ||
		strings.Contains(lower, `"tool"`) ||
		strings.Contains(lower, `"tool_name"`) ||
		strings.Contains(lower, `"name"`) {
		return true
	}
	return strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "```")
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
