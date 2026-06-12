package corelib

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ForwardOpenAICompatRequest forwards an OpenAI chat/completions style request
// to the configured upstream and returns an OpenAI-compatible JSON response.
// It centralizes protocol conversion in corelib so business layers only handle
// provider selection and persistence.
func ForwardOpenAICompatRequest(ctx context.Context, cfg MaclawLLMConfig, body map[string]interface{}, client *http.Client, responseModel string) ([]byte, int, error) {
	if client == nil {
		client = http.DefaultClient
	}

	// Shallow-copy body and remove streaming-only fields that are meaningless
	// when all forward paths force stream:false.
	clean := make(map[string]interface{}, len(body))
	for k, v := range body {
		clean[k] = v
	}
	delete(clean, "stream_options")
	delete(clean, "provider")
	delete(clean, "model_provider")

	wireAPI := strings.ToLower(strings.TrimSpace(cfg.WireAPI))
	switch {
	case wireAPI == "responses" || wireAPI == "responses-ws":
		return forwardResponsesCompat(ctx, cfg, clean, client, responseModel)
	case strings.EqualFold(strings.TrimSpace(cfg.Protocol), "anthropic"):
		return forwardAnthropicCompatRequest(ctx, cfg, clean, client, responseModel)
	default:
		// OpenAI-compatible path: preserve caller fields, with narrowly scoped
		// CodeGen compatibility fixes applied immediately before forwarding.
		// Most OpenAI-compatible providers (GLM, GPT, Kimi, etc.) support tool calling natively.
		// If a provider rejects role:"tool" (e.g. some DeepSeek endpoints),
		// the Hub's failover mechanism will try the next provider.
		return forwardOpenAICompatRequest(ctx, cfg, clean, client, responseModel)
	}
}

// ForwardOpenAICompatStreamRequest forwards an OpenAI chat/completions style
// streaming request to the configured upstream and returns the live response
// body to the caller. The caller owns closing resp.Body.
func ForwardOpenAICompatStreamRequest(ctx context.Context, cfg MaclawLLMConfig, body map[string]interface{}, client *http.Client) (*http.Response, error) {
	if client == nil {
		client = http.DefaultClient
	}
	clean := make(map[string]interface{}, len(body))
	for k, v := range body {
		clean[k] = v
	}
	delete(clean, "provider")
	delete(clean, "model_provider")

	if wireAPI := strings.ToLower(strings.TrimSpace(cfg.WireAPI)); wireAPI == "responses" || wireAPI == "responses-ws" {
		return nil, fmt.Errorf("streaming passthrough is not supported for responses wire api")
	}
	if strings.EqualFold(strings.TrimSpace(cfg.Protocol), "anthropic") {
		return nil, fmt.Errorf("streaming passthrough is not supported for anthropic protocol")
	}
	return forwardOpenAICompatStreamRequest(ctx, cfg, clean, client)
}

func forwardOpenAICompatStreamRequest(ctx context.Context, cfg MaclawLLMConfig, body map[string]interface{}, client *http.Client) (*http.Response, error) {
	fwd := make(map[string]interface{}, len(body))
	for k, v := range body {
		fwd[k] = v
	}
	fwd["model"] = cfg.UpstreamModel()
	fwd["stream"] = true
	sanitizeOpenAICompatForwardBody(cfg, fwd)

	jsonBody, err := json.Marshal(fwd)
	if err != nil {
		return nil, fmt.Errorf("marshal request body: %w", err)
	}
	upstreamURL := openAIChatCompletionsEndpointForConfig(cfg)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("User-Agent", cfg.UserAgent())
	if cfg.Key != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Key)
	}
	SetCodeGenClientNameHeaderIfNeededWithName(req, cfg.UserAgent())
	return client.Do(req)
}

// RewriteOpenAIStreamDataModel rewrites a single SSE data payload's model
// field when it contains JSON. Non-JSON payloads such as [DONE] pass through.
func RewriteOpenAIStreamDataModel(payload []byte, model string) []byte {
	if strings.TrimSpace(model) == "" {
		return payload
	}
	trimmed := strings.TrimSpace(string(payload))
	if trimmed == "" || trimmed == "[DONE]" || !json.Valid([]byte(trimmed)) {
		return payload
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &obj); err != nil {
		return payload
	}
	obj["model"] = model
	data, err := json.Marshal(obj)
	if err != nil {
		return payload
	}
	return data
}

// OpenAIStreamUsageFromData extracts usage from an SSE data JSON payload.
func OpenAIStreamUsageFromData(payload []byte) TokenUsageStat {
	trimmed := strings.TrimSpace(string(payload))
	if trimmed == "" || trimmed == "[DONE]" || !json.Valid([]byte(trimmed)) {
		return TokenUsageStat{}
	}
	return parseOpenAIUsageJSON([]byte(trimmed))
}

func parseOpenAIUsageJSON(body []byte) TokenUsageStat {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return TokenUsageStat{}
	}
	usage, _ := payload["usage"].(map[string]interface{})
	if usage == nil {
		return TokenUsageStat{}
	}
	stat := TokenUsageStat{
		InputTokens:       int64(numberToInt64(firstOpenAICompatNonNil(usage["prompt_tokens"], usage["input_tokens"]))),
		OutputTokens:      int64(numberToInt64(firstOpenAICompatNonNil(usage["completion_tokens"], usage["output_tokens"]))),
		TotalTokens:       int64(numberToInt64(usage["total_tokens"])),
		CachedInputTokens: int64(numberToInt64(openAICompatCachedUsageValue(usage))),
		CacheWriteTokens:  int64(numberToInt64(openAICompatCacheWriteUsageValue(usage))),
		Requests:          1,
	}
	if stat.TotalTokens <= 0 {
		stat.TotalTokens = stat.InputTokens + stat.OutputTokens
	}
	if stat.CachedInputTokens > 0 || stat.CacheWriteTokens > 0 {
		stat.CachedRequests = 1
	}
	return stat
}

func firstOpenAICompatNonNil(values ...interface{}) interface{} {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func openAICompatCachedUsageValue(usage map[string]interface{}) interface{} {
	return firstOpenAICompatNonNil(
		openAICompatLookupMapValue(usage, "prompt_tokens_details", "cached_tokens"),
		openAICompatLookupMapValue(usage, "input_tokens_details", "cached_tokens"),
		usage["cache_read_input_tokens"],
		usage["cached_input_tokens"],
	)
}

func openAICompatCacheWriteUsageValue(usage map[string]interface{}) interface{} {
	return firstOpenAICompatNonNil(
		usage["cache_creation_input_tokens"],
		usage["cache_write_input_tokens"],
		openAICompatLookupMapValue(usage, "prompt_tokens_details", "cache_write_tokens"),
		openAICompatLookupMapValue(usage, "prompt_tokens_details", "cache_creation_input_tokens"),
		openAICompatLookupMapValue(usage, "input_tokens_details", "cache_write_tokens"),
		openAICompatLookupMapValue(usage, "input_tokens_details", "cache_creation_input_tokens"),
	)
}

func openAICompatLookupMapValue(root map[string]interface{}, key string, nested string) interface{} {
	child := mapFromAny(root[key])
	if child == nil {
		return nil
	}
	return child[nested]
}

func sanitizeOpenAICompatForwardStreamOptions(body map[string]interface{}) {
	if body == nil {
		return
	}
	stream, _ := body["stream"].(bool)
	if !stream {
		delete(body, "stream_options")
		return
	}
	opts := mapFromAny(body["stream_options"])
	if opts == nil {
		delete(body, "stream_options")
		return
	}
	out := map[string]interface{}{}
	if includeUsage, ok := opts["include_usage"].(bool); ok {
		out["include_usage"] = includeUsage
	}
	if len(out) == 0 {
		delete(body, "stream_options")
		return
	}
	body["stream_options"] = out
}

func numberToInt64(v interface{}) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	case json.Number:
		value, _ := n.Int64()
		return value
	default:
		return 0
	}
}

func forwardOpenAICompatRequest(ctx context.Context, cfg MaclawLLMConfig, body map[string]interface{}, client *http.Client, responseModel string) ([]byte, int, error) {
	fwd := make(map[string]interface{}, len(body))
	for k, v := range body {
		fwd[k] = v
	}
	fwd["model"] = cfg.UpstreamModel()
	fwd["stream"] = false
	sanitizeOpenAICompatForwardBody(cfg, fwd)
	return forwardOpenAICompatRequestWithSDK(ctx, cfg, fwd, client, responseModel)
}

func sanitizeOpenAICompatForwardBody(cfg MaclawLLMConfig, body map[string]interface{}) {
	sanitizeOpenAICompatForwardBodyWithOptions(cfg, body, true, false)
}

func sanitizeOpenAICompatForwardBodyForResponses(cfg MaclawLLMConfig, body map[string]interface{}) {
	sanitizeOpenAICompatForwardBodyWithOptions(cfg, body, true, true)
}

func sanitizeOpenAICompatForwardBodyWithOptions(cfg MaclawLLMConfig, body map[string]interface{}, dropOrphanedToolHistory bool, preserveStandaloneToolResults bool) {
	if body == nil {
		return
	}
	sanitizeOpenAICompatForwardStreamOptions(body)
	if cfg.NeedsConservativeOpenAICompatSanitization() {
		SanitizeCodeGenOpenAICompatBody(body)
	}
	normalizeOpenAICompatForwardToolChoice(body)
	isDeepSeekFlash := IsDeepSeekFlashOpenAICompat(cfg)
	isGLMCodingPlan := IsGLMCodingPlanOpenAICompat(cfg)
	if IsDeepSeekThinking(cfg) {
		// DeepSeek V4+ thinking mode: explicitly enable thinking so the API
		// returns reasoning_content. Required for deepseek-v4-flash and similar.
		if _, hasThinking := body["thinking"]; !hasThinking {
			body["thinking"] = map[string]interface{}{"type": "enabled"}
		}
	}
	if isDeepSeekFlash {
		normalizeDeepSeekFlashForwardBody(body)
	}
	if isDeepSeekFlash || isGLMCodingPlan {
		normalizeOpenAICompatForwardTools(body)
	}
	if isGLMCodingPlan || isDeepSeekFlash {
		normalizeOpenAICompatForwardTextOnlyMessages(body, isGLMCodingPlan)
	}
	if isDeepSeekFlash {
		ensureDeepSeekFlashForwardJSONResponseInstruction(body)
	}
	sanitizeOpenAICompatForwardStructuredFields(body, dropOrphanedToolHistory, preserveStandaloneToolResults)
}

func sanitizeOpenAICompatForwardStructuredFields(body map[string]interface{}, dropOrphanedToolHistory bool, preserveStandaloneToolResults bool) {
	if messages := sanitizeOpenAICompatForwardMessagesForSDK(body["messages"]); len(messages) > 0 {
		if dropOrphanedToolHistory {
			messages = sanitizeOpenAICompatForwardOrphanedToolHistory(messages, preserveStandaloneToolResults)
		}
		body["messages"] = messages
	}
	if tools := sanitizeOpenAICompatForwardToolsForSDK(body["tools"]); len(tools) > 0 {
		body["tools"] = tools
	} else {
		delete(body, "tools")
	}
	if toolChoice, ok := body["tool_choice"]; ok {
		if sanitized := sanitizeOpenAICompatForwardToolChoiceForSDK(toolChoice); sanitized != nil {
			body["tool_choice"] = sanitized
		} else {
			delete(body, "tool_choice")
		}
	}
	if responseFormat, ok := body["response_format"]; ok {
		if sanitized := sanitizeOpenAICompatForwardResponseFormatForSDK(responseFormat); sanitized != nil {
			body["response_format"] = sanitized
		} else {
			delete(body, "response_format")
		}
	}
	if functionCall, ok := body["function_call"]; ok {
		if sanitized := sanitizeOpenAICompatForwardFunctionCallForSDK(functionCall); sanitized != nil {
			body["function_call"] = sanitized
		} else {
			delete(body, "function_call")
		}
	}
}

func sanitizeOpenAICompatForwardOrphanedToolHistory(messages []interface{}, preserveStandaloneToolResults bool) []interface{} {
	if len(messages) == 0 {
		return messages
	}
	stripToolCalls := make(map[int]bool)
	validToolMessages := make(map[int]bool)
	dropToolMessages := make(map[int]bool)
	hasAssistantToolCalls := false
	for i, item := range messages {
		msg := mapFromAny(item)
		if msg == nil || strings.TrimSpace(fmt.Sprint(msg["role"])) != "assistant" {
			continue
		}
		ids := openAICompatForwardToolCallIDs(msg["tool_calls"])
		if len(ids) == 0 {
			continue
		}
		hasAssistantToolCalls = true
		idSet := make(map[string]bool, len(ids))
		for _, id := range ids {
			idSet[id] = true
		}
		j := i + 1
		for j < len(messages) {
			next := mapFromAny(messages[j])
			if next == nil || strings.TrimSpace(fmt.Sprint(next["role"])) != "tool" {
				break
			}
			j++
		}
		found := map[string]bool{}
		extra := false
		for k := i + 1; k < j; k++ {
			toolMsg := mapFromAny(messages[k])
			callID := strings.TrimSpace(fmt.Sprint(toolMsg["tool_call_id"]))
			if callID == "" || callID == "<nil>" || !idSet[callID] {
				extra = true
				continue
			}
			found[callID] = true
		}
		complete := !extra && len(found) == len(idSet)
		if complete {
			for k := i + 1; k < j; k++ {
				validToolMessages[k] = true
			}
			continue
		}
		stripToolCalls[i] = true
		for k := i + 1; k < j; k++ {
			dropToolMessages[k] = true
		}
	}
	if preserveStandaloneToolResults && !hasAssistantToolCalls {
		return messages
	}
	for i, item := range messages {
		msg := mapFromAny(item)
		if msg == nil || strings.TrimSpace(fmt.Sprint(msg["role"])) != "tool" {
			continue
		}
		if !validToolMessages[i] {
			dropToolMessages[i] = true
		}
	}
	if len(stripToolCalls) == 0 && len(dropToolMessages) == 0 {
		return messages
	}
	out := make([]interface{}, 0, len(messages))
	for i, item := range messages {
		if dropToolMessages[i] {
			continue
		}
		if !stripToolCalls[i] {
			out = append(out, item)
			continue
		}
		msg := mapFromAny(item)
		clean := make(map[string]interface{}, len(msg))
		for k, v := range msg {
			if k == "tool_calls" {
				continue
			}
			clean[k] = v
		}
		out = append(out, clean)
	}
	return out
}

func openAICompatForwardToolCallIDs(raw interface{}) []string {
	items := openAICompatForwardSlice(raw)
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		call := mapFromAny(item)
		if call == nil {
			continue
		}
		id := strings.TrimSpace(fmt.Sprint(call["id"]))
		if id != "" && id != "<nil>" {
			out = append(out, id)
		}
	}
	return out
}

func sanitizeOpenAICompatForwardMessagesForSDK(raw interface{}) []interface{} {
	items := openAICompatForwardMessageSlice(raw)
	if len(items) == 0 {
		return nil
	}
	out := make([]interface{}, 0, len(items))
	var pendingToolCallIDs []string
	for _, item := range items {
		m := mapFromAny(item)
		if m == nil {
			pendingToolCallIDs = nil
			continue
		}
		role := strings.TrimSpace(fmt.Sprint(m["role"]))
		switch role {
		case "system", "developer":
			pendingToolCallIDs = nil
			out = append(out, map[string]interface{}{
				"role":    role,
				"content": sanitizeOpenAICompatForwardMessageContent(m["content"], false),
			})
		case "user":
			pendingToolCallIDs = nil
			clean := map[string]interface{}{
				"role":    role,
				"content": sanitizeOpenAICompatForwardMessageContent(m["content"], true),
			}
			if name := strings.TrimSpace(fmt.Sprint(m["name"])); name != "" && name != "<nil>" {
				clean["name"] = name
			}
			out = append(out, clean)
		case "assistant":
			clean := map[string]interface{}{"role": role}
			if _, ok := m["content"]; ok {
				clean["content"] = sanitizeOpenAICompatForwardMessageContent(m["content"], true)
			}
			if toolCalls := sanitizeOpenAICompatForwardToolCallsForSDK(m["tool_calls"]); len(toolCalls) > 0 {
				clean["tool_calls"] = toolCalls
				pendingToolCallIDs = openAICompatForwardToolCallIDs(toolCalls)
			} else {
				pendingToolCallIDs = nil
			}
			if functionCall := sanitizeOpenAICompatForwardFunctionCallForSDK(m["function_call"]); functionCall != nil {
				clean["function_call"] = functionCall
			}
			out = append(out, clean)
		case "tool":
			callID := strings.TrimSpace(fmt.Sprint(m["tool_call_id"]))
			if (callID == "" || callID == "<nil>") && len(pendingToolCallIDs) > 0 {
				callID = pendingToolCallIDs[0]
				pendingToolCallIDs = pendingToolCallIDs[1:]
			} else if len(pendingToolCallIDs) > 0 && callID == pendingToolCallIDs[0] {
				pendingToolCallIDs = pendingToolCallIDs[1:]
			}
			if callID == "" || callID == "<nil>" {
				continue
			}
			out = append(out, map[string]interface{}{
				"role":         "tool",
				"tool_call_id": callID,
				"content":      stringifyOpenAICompatForwardToolOutput(m["content"]),
			})
		default:
			pendingToolCallIDs = nil
			if role == "" || role == "<nil>" {
				continue
			}
			out = append(out, map[string]interface{}{
				"role":    role,
				"content": sanitizeOpenAICompatForwardMessageContent(m["content"], true),
			})
		}
	}
	return out
}

func sanitizeOpenAICompatForwardMessageContent(raw interface{}, allowBlocks bool) interface{} {
	switch v := raw.(type) {
	case nil:
		return ""
	case string:
		return v
	}
	if !allowBlocks {
		return openAICompatForwardTextContent(raw)
	}
	blocks := openAICompatForwardSlice(raw)
	if len(blocks) == 0 {
		return stringifyOpenAICompatForwardToolOutput(raw)
	}
	out := make([]interface{}, 0, len(blocks))
	for _, block := range blocks {
		m := mapFromAny(block)
		if m == nil {
			continue
		}
		switch strings.TrimSpace(fmt.Sprint(m["type"])) {
		case "text", "input_text", "output_text":
			text := strings.TrimSpace(fmt.Sprint(firstOpenAICompatNonNil(m["text"], m["content"])))
			if text != "" && text != "<nil>" {
				out = append(out, map[string]interface{}{"type": "text", "text": text})
			}
		case "image_url":
			if imageURL := mapFromAny(m["image_url"]); imageURL != nil {
				clean := map[string]interface{}{}
				if url := strings.TrimSpace(fmt.Sprint(imageURL["url"])); url != "" && url != "<nil>" {
					clean["url"] = url
				}
				if detail := strings.TrimSpace(fmt.Sprint(imageURL["detail"])); detail != "" && detail != "<nil>" {
					clean["detail"] = detail
				}
				if len(clean) > 0 {
					out = append(out, map[string]interface{}{"type": "image_url", "image_url": clean})
				}
			}
		case "input_audio":
			if audio := mapFromAny(m["input_audio"]); audio != nil {
				clean := map[string]interface{}{}
				if data := strings.TrimSpace(fmt.Sprint(audio["data"])); data != "" && data != "<nil>" {
					clean["data"] = data
				}
				if format := strings.TrimSpace(fmt.Sprint(audio["format"])); format != "" && format != "<nil>" {
					clean["format"] = format
				}
				if len(clean) > 0 {
					out = append(out, map[string]interface{}{"type": "input_audio", "input_audio": clean})
				}
			}
		case "file":
			if file := mapFromAny(m["file"]); file != nil {
				clean := map[string]interface{}{}
				for _, key := range []string{"file_id", "filename", "file_data"} {
					if value := strings.TrimSpace(fmt.Sprint(file[key])); value != "" && value != "<nil>" {
						clean[key] = value
					}
				}
				if len(clean) > 0 {
					out = append(out, map[string]interface{}{"type": "file", "file": clean})
				}
			}
		}
	}
	if len(out) == 0 {
		return openAICompatForwardTextContent(raw)
	}
	return out
}

func sanitizeOpenAICompatForwardToolCallsForSDK(raw interface{}) []interface{} {
	items := openAICompatForwardSlice(raw)
	if len(items) == 0 {
		return nil
	}
	out := make([]interface{}, 0, len(items))
	for _, item := range items {
		call := mapFromAny(item)
		if call == nil {
			continue
		}
		id := strings.TrimSpace(fmt.Sprint(call["id"]))
		fn := mapFromAny(call["function"])
		name := ""
		if fn != nil {
			name = strings.TrimSpace(fmt.Sprint(fn["name"]))
		}
		if name == "" || name == "<nil>" {
			continue
		}
		if id == "" || id == "<nil>" {
			id = randomOpenAICompatForwardToolCallID()
		}
		args := "{}"
		if rawArgs, ok := fn["arguments"]; ok {
			args = stringifyOpenAICompatForwardFunctionArguments(rawArgs)
		}
		out = append(out, map[string]interface{}{
			"id":   id,
			"type": "function",
			"function": map[string]interface{}{
				"name":      name,
				"arguments": args,
			},
		})
	}
	return out
}

func randomOpenAICompatForwardToolCallID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "call_generated"
	}
	return fmt.Sprintf("call_%x", b)
}

func sanitizeOpenAICompatForwardToolsForSDK(raw interface{}) []interface{} {
	items := openAICompatForwardSlice(raw)
	if len(items) == 0 {
		return nil
	}
	out := make([]interface{}, 0, len(items))
	for _, item := range items {
		tool := mapFromAny(item)
		if tool == nil {
			continue
		}
		fn := mapFromAny(tool["function"])
		if fn == nil {
			continue
		}
		name := strings.TrimSpace(fmt.Sprint(fn["name"]))
		if name == "" || name == "<nil>" {
			continue
		}
		cleanFn := map[string]interface{}{"name": name}
		if desc := strings.TrimSpace(fmt.Sprint(fn["description"])); desc != "" && desc != "<nil>" {
			cleanFn["description"] = desc
		}
		if params, ok := fn["parameters"]; ok && params != nil {
			cleanFn["parameters"] = params
		}
		if strict, ok := fn["strict"].(bool); ok {
			cleanFn["strict"] = strict
		}
		out = append(out, map[string]interface{}{"type": "function", "function": cleanFn})
	}
	return out
}

func sanitizeOpenAICompatForwardToolChoiceForSDK(raw interface{}) interface{} {
	if s, ok := raw.(string); ok {
		choice := strings.TrimSpace(s)
		switch choice {
		case "none", "auto", "required":
			return choice
		default:
			return nil
		}
	}
	m := mapFromAny(raw)
	if m == nil {
		return nil
	}
	typ := strings.TrimSpace(fmt.Sprint(m["type"]))
	if typ == "" || typ == "<nil>" {
		typ = "function"
	}
	if typ != "function" {
		return nil
	}
	fn := mapFromAny(m["function"])
	if fn == nil {
		return nil
	}
	name := strings.TrimSpace(fmt.Sprint(fn["name"]))
	if name == "" || name == "<nil>" {
		return nil
	}
	return map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": name}}
}

func sanitizeOpenAICompatForwardFunctionCallForSDK(raw interface{}) interface{} {
	if s, ok := raw.(string); ok {
		choice := strings.TrimSpace(s)
		switch choice {
		case "none", "auto":
			return choice
		default:
			return nil
		}
	}
	m := mapFromAny(raw)
	if m == nil {
		return nil
	}
	name := strings.TrimSpace(fmt.Sprint(m["name"]))
	if name == "" || name == "<nil>" {
		return nil
	}
	args := stringifyOpenAICompatForwardFunctionArguments(m["arguments"])
	return map[string]interface{}{"name": name, "arguments": args}
}

func stringifyOpenAICompatForwardFunctionArguments(raw interface{}) string {
	switch v := raw.(type) {
	case nil:
		return "{}"
	case string:
		text := strings.TrimSpace(v)
		if text != "" && json.Valid([]byte(text)) {
			return text
		}
	default:
		data, err := json.Marshal(v)
		if err == nil && len(data) > 0 && string(data) != "null" && json.Valid(data) {
			return string(data)
		}
	}
	return "{}"
}

func sanitizeOpenAICompatForwardResponseFormatForSDK(raw interface{}) interface{} {
	m := mapFromAny(raw)
	if m == nil {
		return nil
	}
	typ := strings.TrimSpace(fmt.Sprint(m["type"]))
	switch typ {
	case "text", "json_object":
		return map[string]interface{}{"type": typ}
	case "json_schema":
		js := mapFromAny(m["json_schema"])
		if js == nil {
			return nil
		}
		name := strings.TrimSpace(fmt.Sprint(js["name"]))
		if name == "" || name == "<nil>" {
			return nil
		}
		schema, ok := js["schema"]
		if !ok || schema == nil {
			return nil
		}
		clean := map[string]interface{}{"name": name, "schema": schema}
		if desc := strings.TrimSpace(fmt.Sprint(js["description"])); desc != "" && desc != "<nil>" {
			clean["description"] = desc
		}
		if strict, ok := js["strict"].(bool); ok {
			clean["strict"] = strict
		}
		return map[string]interface{}{"type": "json_schema", "json_schema": clean}
	default:
		return nil
	}
}

func sanitizeCodeGenOpenAICompatForwardBody(cfg MaclawLLMConfig, body map[string]interface{}) {
	sanitizeOpenAICompatForwardBody(cfg, body)
}

func normalizeOpenAICompatForwardToolChoice(body map[string]interface{}) {
	if _, ok := body["tool_choice"]; !ok {
		return
	}
	if !openAICompatForwardHasTools(body) {
		delete(body, "tool_choice")
	}
}

func openAICompatForwardHasTools(body map[string]interface{}) bool {
	tools, ok := body["tools"]
	return ok && openAICompatForwardArrayLen(tools) > 0
}

func openAICompatForwardArrayLen(value interface{}) int {
	return len(openAICompatForwardSlice(value))
}

func normalizeDeepSeekFlashForwardBody(body map[string]interface{}) {
	if n, ok := openAICompatPositiveIntValue(body["n"]); ok && n > 1 {
		body["n"] = 1
	}
	if tc, ok := body["tool_choice"]; ok {
		switch v := tc.(type) {
		case string:
			if strings.TrimSpace(v) == "required" {
				body["tool_choice"] = "auto"
			}
		default:
			if mapFromAny(v) != nil {
				body["tool_choice"] = "auto"
			}
		}
	}
	if rf := mapFromAny(body["response_format"]); rf != nil {
		if strings.TrimSpace(fmt.Sprint(rf["type"])) == "json_schema" {
			body["response_format"] = map[string]interface{}{"type": "json_object"}
		}
	}
}

func openAICompatPositiveIntValue(value interface{}) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, v > 0
	case int64:
		return int(v), v > 0
	case float64:
		if v == float64(int(v)) && v > 0 {
			return int(v), true
		}
	case json.Number:
		if i, err := v.Int64(); err == nil && i > 0 {
			return int(i), true
		}
	}
	return 0, false
}

func normalizeOpenAICompatForwardTools(body map[string]interface{}) {
	if tools, ok := body["tools"]; ok {
		body["tools"] = normalizeOpenAICompatForwardToolsValue(tools)
	}
	if functions, ok := body["functions"]; ok {
		body["functions"] = normalizeOpenAICompatForwardFunctionsValue(functions)
	}
}

func normalizeOpenAICompatForwardToolsValue(tools interface{}) interface{} {
	switch x := tools.(type) {
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, tool := range x {
			if m := mapFromAny(tool); m != nil {
				out[i] = normalizeOpenAICompatForwardTool(m)
			} else {
				out[i] = tool
			}
		}
		return out
	case []map[string]interface{}:
		out := make([]map[string]interface{}, len(x))
		for i, tool := range x {
			out[i] = normalizeOpenAICompatForwardTool(tool)
		}
		return out
	case []map[string]string:
		out := make([]interface{}, 0, len(x))
		for _, tool := range x {
			if m := mapFromAny(tool); m != nil {
				out = append(out, normalizeOpenAICompatForwardTool(m))
			}
		}
		return out
	default:
		items := openAICompatForwardSlice(tools)
		if len(items) == 0 {
			return tools
		}
		out := make([]interface{}, 0, len(items))
		for _, tool := range items {
			if m := mapFromAny(tool); m != nil {
				out = append(out, normalizeOpenAICompatForwardTool(m))
			}
		}
		return out
	}
}

func normalizeOpenAICompatForwardFunctionsValue(functions interface{}) interface{} {
	switch x := functions.(type) {
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, fn := range x {
			if m := mapFromAny(fn); m != nil {
				out[i] = normalizeOpenAICompatForwardFunction(m)
			} else {
				out[i] = fn
			}
		}
		return out
	case []map[string]interface{}:
		out := make([]map[string]interface{}, len(x))
		for i, fn := range x {
			out[i] = normalizeOpenAICompatForwardFunction(fn)
		}
		return out
	case []map[string]string:
		out := make([]interface{}, 0, len(x))
		for _, fn := range x {
			if m := mapFromAny(fn); m != nil {
				out = append(out, normalizeOpenAICompatForwardFunction(m))
			}
		}
		return out
	default:
		items := openAICompatForwardSlice(functions)
		if len(items) == 0 {
			return functions
		}
		out := make([]interface{}, 0, len(items))
		for _, fn := range items {
			if m := mapFromAny(fn); m != nil {
				out = append(out, normalizeOpenAICompatForwardFunction(m))
			}
		}
		return out
	}
}

func normalizeOpenAICompatForwardTool(tool map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(tool)+1)
	for k, v := range tool {
		out[k] = v
	}
	if fn := mapFromAny(out["function"]); fn != nil {
		out["function"] = normalizeOpenAICompatForwardFunction(fn)
	}
	if _, ok := out["type"]; !ok {
		out["type"] = "function"
	}
	return out
}

func normalizeOpenAICompatForwardFunction(fn map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(fn)+1)
	for k, v := range fn {
		out[k] = v
	}
	if params, ok := out["parameters"]; ok && params != nil {
		out["parameters"] = normalizeOpenAICompatForwardSchemaShape(params)
	} else {
		out["parameters"] = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
	}
	return out
}

func normalizeOpenAICompatForwardSchemaShape(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(x)+2)
		for k, val := range x {
			if k == "properties" {
				out[k] = normalizeOpenAICompatForwardSchemaProperties(val)
			} else {
				out[k] = normalizeOpenAICompatForwardSchemaShape(val)
			}
		}
		typ := strings.TrimSpace(fmt.Sprint(out["type"]))
		if typ == "" || typ == "<nil>" {
			out["type"] = "object"
			typ = "object"
		}
		if typ == "object" {
			if _, ok := out["properties"]; !ok {
				out["properties"] = map[string]interface{}{}
			}
		}
		if typ == "array" {
			if _, ok := out["items"]; !ok {
				out["items"] = map[string]interface{}{"type": "string"}
			}
		}
		return out
	case map[string]string:
		out := make(map[string]interface{}, len(x)+2)
		for k, val := range x {
			out[k] = val
		}
		typ := strings.TrimSpace(fmt.Sprint(out["type"]))
		if typ == "" || typ == "<nil>" {
			out["type"] = "object"
			typ = "object"
		}
		if typ == "object" {
			if _, ok := out["properties"]; !ok {
				out["properties"] = map[string]interface{}{}
			}
		}
		if typ == "array" {
			if _, ok := out["items"]; !ok {
				out["items"] = map[string]interface{}{"type": "string"}
			}
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, val := range x {
			out[i] = normalizeOpenAICompatForwardSchemaShape(val)
		}
		return out
	case []map[string]interface{}:
		out := make([]interface{}, 0, len(x))
		for _, val := range x {
			out = append(out, normalizeOpenAICompatForwardSchemaShape(val))
		}
		return out
	default:
		return v
	}
}

func normalizeOpenAICompatForwardSchemaProperties(v interface{}) interface{} {
	props := mapFromAny(v)
	if props == nil {
		return v
	}
	out := make(map[string]interface{}, len(props))
	for name, schema := range props {
		out[name] = normalizeOpenAICompatForwardSchemaShape(schema)
	}
	return out
}

func ensureDeepSeekFlashForwardJSONResponseInstruction(body map[string]interface{}) {
	responseFormat := mapFromAny(body["response_format"])
	if responseFormat == nil || strings.TrimSpace(fmt.Sprint(responseFormat["type"])) != "json_object" {
		return
	}
	messages := openAICompatForwardMessageSlice(body["messages"])
	if len(messages) == 0 || openAICompatForwardMessagesMentionJSON(messages) {
		return
	}
	body["messages"] = prependOrMergeOpenAICompatForwardSystemMessage(messages, "Respond with a valid JSON object.")
}

func openAICompatForwardMessagesMentionJSON(messages []interface{}) bool {
	for _, message := range messages {
		m := mapFromAny(message)
		if m == nil {
			continue
		}
		if strings.Contains(strings.ToLower(openAICompatForwardTextContent(m["content"])), "json") {
			return true
		}
	}
	return false
}

func prependOrMergeOpenAICompatForwardSystemMessage(messages []interface{}, content string) []interface{} {
	if len(messages) == 0 {
		return []interface{}{map[string]interface{}{"role": "system", "content": content}}
	}
	first := mapFromAny(messages[0])
	if first != nil {
		if role, _ := first["role"].(string); role == "system" {
			patched := make(map[string]interface{}, len(first))
			for k, v := range first {
				patched[k] = v
			}
			existing := strings.TrimSpace(openAICompatForwardTextContent(first["content"]))
			if existing == "" {
				patched["content"] = content
			} else {
				patched["content"] = existing + "\n\n" + content
			}
			out := make([]interface{}, len(messages))
			copy(out, messages)
			out[0] = patched
			return out
		}
	}
	out := make([]interface{}, 0, len(messages)+1)
	out = append(out, map[string]interface{}{"role": "system", "content": content})
	out = append(out, messages...)
	return out
}

func normalizeOpenAICompatForwardTextOnlyMessages(body map[string]interface{}, fillEmptyUser bool) {
	raw := openAICompatForwardMessageSlice(body["messages"])
	if len(raw) == 0 {
		return
	}
	out := make([]interface{}, 0, len(raw))
	for _, msg := range raw {
		m := mapFromAny(msg)
		if m == nil {
			out = append(out, msg)
			continue
		}
		cp := make(map[string]interface{}, len(m))
		for k, v := range m {
			cp[k] = v
		}
		if role, _ := cp["role"].(string); role == "developer" {
			cp["role"] = "system"
		}
		cp["content"] = openAICompatForwardTextContent(cp["content"])
		if fillEmptyUser {
			if role, _ := cp["role"].(string); role == "user" && strings.TrimSpace(fmt.Sprint(cp["content"])) == "" {
				cp["content"] = "[No user content provided]"
			}
		}
		out = append(out, cp)
	}
	body["messages"] = out
}

func openAICompatForwardMessageSlice(raw interface{}) []interface{} {
	switch v := raw.(type) {
	case []interface{}:
		return v
	case []map[string]interface{}:
		out := make([]interface{}, 0, len(v))
		for _, item := range v {
			out = append(out, item)
		}
		return out
	case []map[string]string:
		out := make([]interface{}, 0, len(v))
		for _, item := range v {
			out = append(out, item)
		}
		return out
	default:
		return openAICompatForwardSlice(raw)
	}
}

func openAICompatForwardSlice(raw interface{}) []interface{} {
	switch v := raw.(type) {
	case nil:
		return nil
	case []interface{}:
		return v
	case []map[string]interface{}:
		out := make([]interface{}, 0, len(v))
		for _, item := range v {
			out = append(out, item)
		}
		return out
	case []map[string]string:
		out := make([]interface{}, 0, len(v))
		for _, item := range v {
			out = append(out, item)
		}
		return out
	default:
		data, err := json.Marshal(v)
		if err != nil || len(data) == 0 || string(data) == "null" {
			return nil
		}
		var out []interface{}
		if err := json.Unmarshal(data, &out); err != nil {
			return nil
		}
		return out
	}
}

func openAICompatForwardTextContent(raw interface{}) string {
	switch v := raw.(type) {
	case nil:
		return ""
	case string:
		return v
	case []interface{}:
		return joinOpenAICompatForwardTextBlocks(v)
	case []map[string]interface{}:
		blocks := make([]interface{}, 0, len(v))
		for _, item := range v {
			blocks = append(blocks, item)
		}
		return joinOpenAICompatForwardTextBlocks(blocks)
	case []map[string]string:
		blocks := make([]interface{}, 0, len(v))
		for _, item := range v {
			blocks = append(blocks, item)
		}
		return joinOpenAICompatForwardTextBlocks(blocks)
	default:
		blocks := openAICompatForwardSlice(v)
		if len(blocks) == 0 {
			return fmt.Sprint(v)
		}
		return joinOpenAICompatForwardTextBlocks(blocks)
	}
}

func joinOpenAICompatForwardTextBlocks(blocks []interface{}) string {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		m := mapFromAny(block)
		if m == nil {
			continue
		}
		switch strings.TrimSpace(fmt.Sprint(m["type"])) {
		case "text", "input_text", "output_text":
		default:
			continue
		}
		text, _ := m["text"].(string)
		text = strings.TrimSpace(text)
		if text == "" {
			text, _ = m["content"].(string)
			text = strings.TrimSpace(text)
		}
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func forwardAnthropicCompatRequest(ctx context.Context, cfg MaclawLLMConfig, body map[string]interface{}, client *http.Client, responseModel string) ([]byte, int, error) {
	anthropicReq := openaiToAnthropic(body, cfg.UpstreamModel())
	respBody, statusCode, err := forwardAnthropicMessageWithSDK(ctx, cfg, anthropicReq, client)
	if err != nil {
		if statusCode >= 400 {
			return openAICompatAnthropicUpstreamError(statusCode, respBody)
		}
		return nil, statusCode, err
	}
	if statusCode >= 400 {
		return openAICompatAnthropicUpstreamError(statusCode, respBody)
	}
	var respMap map[string]interface{}
	if err := json.Unmarshal(respBody, &respMap); err != nil {
		return nil, 0, fmt.Errorf("parse anthropic response: %w", err)
	}
	model := strings.TrimSpace(responseModel)
	if model == "" {
		model = cfg.UpstreamModel()
	}
	openaiResp := anthropicToOpenAI(respMap, model)
	data, err := json.Marshal(openaiResp)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal openai response: %w", err)
	}
	return data, http.StatusOK, nil
}

func openAICompatForwardUpstreamErrorMessage(statusCode int, body []byte) string {
	msg := openAICompatForwardExtractErrorMessage(body)
	if msg == "" {
		return fmt.Sprintf("upstream error (HTTP %d): body_len=%d", statusCode, len(body))
	}
	return fmt.Sprintf("upstream error (HTTP %d): %s", statusCode, msg)
}

func openAICompatForwardExtractErrorMessage(body []byte) string {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	if errorObj, ok := payload["error"].(map[string]interface{}); ok {
		if msg := strings.TrimSpace(fmt.Sprint(errorObj["message"])); msg != "" && msg != "<nil>" {
			return openAICompatForwardCompactErrorMessage(msg)
		}
	}
	if msg := strings.TrimSpace(fmt.Sprint(payload["message"])); msg != "" && msg != "<nil>" {
		return openAICompatForwardCompactErrorMessage(msg)
	}
	return ""
}

func openAICompatForwardCompactErrorMessage(msg string) string {
	msg = strings.Join(strings.Fields(msg), " ")
	const limit = 300
	runes := []rune(msg)
	if len(runes) <= limit {
		return msg
	}
	return string(runes[:limit]) + "..."
}

// openaiToResponses converts an OpenAI Chat Completions request body
// to a Responses API request body.
func openaiToResponses(body map[string]interface{}, model string) map[string]interface{} {
	responsesReq := map[string]interface{}{
		"model":  model,
		"stream": false,
	}
	messages := openAICompatForwardMessageSlice(body["messages"])
	converted := convertOpenAIToResponsesInput(messages)
	responsesReq["input"] = converted.Input
	if converted.Instructions != "" {
		responsesReq["instructions"] = converted.Instructions
	}
	if tools := openAICompatForwardSlice(body["tools"]); len(tools) > 0 {
		responsesReq["tools"] = convertResponsesToolsAny(tools)
	}
	if toolChoice, ok := body["tool_choice"]; ok {
		if converted := openAICompatResponsesToolChoiceFromChatToolChoice(toolChoice); converted != nil {
			responsesReq["tool_choice"] = converted
		}
	}
	if _, ok := body["max_output_tokens"]; !ok {
		if v, ok := body["max_completion_tokens"]; ok {
			responsesReq["max_output_tokens"] = v
		} else if v, ok := body["max_tokens"]; ok {
			responsesReq["max_output_tokens"] = v
		}
	} else {
		responsesReq["max_output_tokens"] = body["max_output_tokens"]
	}
	if _, ok := body["text"]; !ok {
		if format := openAICompatResponsesTextFormatFromChatResponseFormat(body["response_format"]); format != nil {
			responsesReq["text"] = map[string]interface{}{"format": format}
		}
	} else {
		responsesReq["text"] = body["text"]
	}
	for _, key := range []string{"metadata", "store", "reasoning"} {
		if v, ok := body[key]; ok {
			responsesReq[key] = v
		}
	}
	return responsesReq
}

func openAICompatResponsesToolChoiceFromChatToolChoice(raw interface{}) interface{} {
	if s, ok := raw.(string); ok {
		choice := strings.TrimSpace(s)
		switch choice {
		case "auto", "none", "required":
			return choice
		default:
			return nil
		}
	}
	choice := mapFromAny(raw)
	if choice == nil {
		return nil
	}
	typ := strings.TrimSpace(fmt.Sprint(choice["type"]))
	if typ == "" || typ == "<nil>" {
		typ = "function"
	}
	if typ != "function" {
		return nil
	}
	fn := mapFromAny(choice["function"])
	if fn == nil {
		return nil
	}
	name := strings.TrimSpace(fmt.Sprint(fn["name"]))
	if name == "" || name == "<nil>" {
		return nil
	}
	return map[string]interface{}{"type": "function", "name": name}
}

func openAICompatResponsesTextFormatFromChatResponseFormat(raw interface{}) map[string]interface{} {
	format := mapFromAny(raw)
	if format == nil {
		return nil
	}
	typ := strings.TrimSpace(fmt.Sprint(format["type"]))
	switch typ {
	case "text", "json_object":
		return map[string]interface{}{"type": typ}
	case "json_schema":
		schema := mapFromAny(format["json_schema"])
		if schema == nil {
			return nil
		}
		name := strings.TrimSpace(fmt.Sprint(schema["name"]))
		if name == "" || name == "<nil>" {
			return nil
		}
		if _, ok := schema["schema"]; !ok {
			return nil
		}
		out := map[string]interface{}{"type": "json_schema"}
		for k, v := range schema {
			out[k] = v
		}
		return out
	default:
		return nil
	}
}

func convertResponsesToolsAny(tools []interface{}) []map[string]interface{} {
	typed := make([]map[string]interface{}, 0, len(tools))
	for _, item := range tools {
		if m := mapFromAny(item); m != nil {
			typed = append(typed, m)
		}
	}
	return convertResponsesTools(typed)
}

type responsesConvertedInput struct {
	Instructions string
	Input        []interface{}
}

func convertOpenAIToResponsesInput(messages []interface{}) responsesConvertedInput {
	var result responsesConvertedInput
	for _, m := range messages {
		mm := mapFromAny(m)
		if mm == nil {
			continue
		}
		role, _ := mm["role"].(string)
		switch role {
		case "system", "developer":
			if content := openAICompatForwardTextContent(mm["content"]); content != "" {
				if result.Instructions != "" {
					result.Instructions += "\n"
				}
				result.Instructions += content
			}
		case "user":
			result.Input = append(result.Input, map[string]interface{}{
				"type": "message",
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "input_text", "text": openAICompatForwardTextContent(mm["content"])},
				},
			})
		case "assistant":
			if text := openAICompatForwardTextContent(mm["content"]); text != "" {
				result.Input = append(result.Input, map[string]interface{}{
					"type": "message",
					"role": "assistant",
					"content": []interface{}{
						map[string]interface{}{"type": "output_text", "text": text},
					},
				})
			}
			for _, tc := range extractOpenAIToolCalls(mm) {
				result.Input = append(result.Input, map[string]interface{}{
					"type":      "function_call",
					"call_id":   tc.ID,
					"name":      tc.Name,
					"arguments": tc.Arguments,
				})
			}
		case "tool":
			result.Input = append(result.Input, map[string]interface{}{
				"type":    "function_call_output",
				"call_id": stringFromMap(mm, "tool_call_id"),
				"output":  stringifyOpenAICompatForwardToolOutput(mm["content"]),
			})
		}
	}
	if result.Input == nil {
		result.Input = []interface{}{}
	}
	return result
}

func stringifyOpenAICompatForwardToolOutput(raw interface{}) string {
	switch raw.(type) {
	case nil:
		return ""
	case string, []interface{}, []map[string]interface{}, []map[string]string:
		if text := openAICompatForwardTextContent(raw); text != "" {
			return text
		}
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return fmt.Sprint(raw)
	}
	return string(data)
}

func convertResponsesTools(tools []map[string]interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(tools))
	for _, t := range tools {
		fn := mapFromAny(t["function"])
		if fn == nil {
			continue
		}
		tool := map[string]interface{}{"type": "function"}
		if name, ok := fn["name"]; ok {
			tool["name"] = name
		}
		if desc, ok := fn["description"]; ok {
			tool["description"] = desc
		}
		if params, ok := fn["parameters"]; ok {
			tool["parameters"] = params
		}
		if strict, ok := fn["strict"].(bool); ok {
			tool["strict"] = strict
		}
		out = append(out, tool)
	}
	return out
}

// responsesToOpenAIUsage converts Responses API usage into OpenAI-compatible
// chat completion usage while preserving prompt-cache counters.
func responsesToOpenAIUsage(raw interface{}) map[string]interface{} {
	usage := mapFromAny(raw)
	var promptTokens, completionTokens float64
	if usage != nil {
		promptTokens = float64(numberToInt64(usage["input_tokens"]))
		completionTokens = float64(numberToInt64(usage["output_tokens"]))
	}
	result := map[string]interface{}{
		"prompt_tokens":     promptTokens,
		"completion_tokens": completionTokens,
		"total_tokens":      promptTokens + completionTokens,
	}
	if usage == nil {
		return result
	}
	cacheRead := numberToInt64(openAICompatCachedUsageValue(usage))
	cacheWrite := numberToInt64(openAICompatCacheWriteUsageValue(usage))
	if cacheRead > 0 || cacheWrite > 0 {
		result["prompt_tokens_details"] = map[string]interface{}{
			"cached_tokens": cacheRead,
		}
	}
	if cacheRead > 0 {
		result["cache_read_input_tokens"] = float64(cacheRead)
	}
	if cacheWrite > 0 {
		result["cache_write_input_tokens"] = float64(cacheWrite)
		details := result["prompt_tokens_details"].(map[string]interface{})
		details["cache_creation_input_tokens"] = cacheWrite
	}
	return result
}

// responsesToOpenAI converts a Responses API response body
// to an OpenAI Chat Completions response body.
func responsesToOpenAI(resp map[string]interface{}, model string) map[string]interface{} {
	var contentBuilder strings.Builder
	var toolCalls []interface{}
	for _, item := range openAICompatForwardSlice(resp["output"]) {
		itemMap := mapFromAny(item)
		if itemMap == nil {
			continue
		}
		itemType, _ := itemMap["type"].(string)
		switch itemType {
		case "message":
			for _, block := range openAICompatForwardSlice(itemMap["content"]) {
				blockMap := mapFromAny(block)
				if blockMap == nil {
					continue
				}
				blockType, _ := blockMap["type"].(string)
				if blockType == "output_text" {
					text, _ := blockMap["text"].(string)
					contentBuilder.WriteString(text)
				}
			}
		case "function_call":
			callID, _ := itemMap["call_id"].(string)
			if callID == "" {
				callID, _ = itemMap["id"].(string)
			}
			name, _ := itemMap["name"].(string)
			args, _ := itemMap["arguments"].(string)
			if callID != "" && name != "" {
				toolCalls = append(toolCalls, map[string]interface{}{
					"id":   callID,
					"type": "function",
					"function": map[string]interface{}{
						"name":      name,
						"arguments": args,
					},
				})
			}
		}
	}
	contentText := contentBuilder.String()
	message := map[string]interface{}{
		"role":    "assistant",
		"content": contentText,
	}
	finishReason := "stop"
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
		finishReason = "tool_calls"
	}
	usage := responsesToOpenAIUsage(resp["usage"])
	id, _ := resp["id"].(string)
	if id == "" {
		id = "chatcmpl-proxy"
	}
	return map[string]interface{}{
		"id":     id,
		"object": "chat.completion",
		"model":  model,
		"choices": []interface{}{
			map[string]interface{}{
				"index":         0,
				"message":       message,
				"finish_reason": finishReason,
			},
		},
		"usage": usage,
	}
}

func forwardResponsesCompat(ctx context.Context, cfg MaclawLLMConfig, body map[string]interface{}, client *http.Client, responseModel string) ([]byte, int, error) {
	fwd := make(map[string]interface{}, len(body))
	for k, v := range body {
		fwd[k] = v
	}
	sanitizeOpenAICompatForwardBodyForResponses(cfg, fwd)
	responsesReq := openaiToResponses(fwd, cfg.UpstreamModel())
	jsonBody, err := json.Marshal(responsesReq)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal responses request: %w", err)
	}
	upstreamURL := openAIResponsesEndpoint(cfg.URL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", cfg.UserAgent())
	if cfg.Key != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Key)
	}
	SetCodeGenClientNameHeaderIfNeededWithName(req, cfg.UserAgent())
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("read response body: %w", err)
	}
	if resp.StatusCode >= 400 {
		errResp := map[string]interface{}{
			"error": map[string]interface{}{
				"message": openAICompatForwardUpstreamErrorMessage(resp.StatusCode, respBody),
				"type":    "server_error",
			},
		}
		data, _ := json.Marshal(errResp)
		return data, resp.StatusCode, nil
	}
	var respMap map[string]interface{}
	if err := json.Unmarshal(respBody, &respMap); err != nil {
		return nil, 0, fmt.Errorf("parse responses api response: %w", err)
	}
	model := strings.TrimSpace(responseModel)
	if model == "" {
		model = cfg.UpstreamModel()
	}
	openaiResp := responsesToOpenAI(respMap, model)
	data, err := json.Marshal(openaiResp)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal openai response: %w", err)
	}
	return data, http.StatusOK, nil
}

func openAIChatCompletionsEndpointForConfig(cfg MaclawLLMConfig) string {
	return openAIChatCompletionsEndpoint(NormalizeGLMCodingPlanOpenAIBaseURL(cfg.URL, cfg.UserAgent()))
}

// openAIChatCompletionsEndpoint constructs the chat/completions URL from a base URL.
// If the URL already ends with "/v1", it appends "/chat/completions" directly;
// otherwise it appends "/v1/chat/completions".
// This avoids double "/v1" when the base URL is e.g. "https://api.deepseek.com/v1".
func openAIChatCompletionsEndpoint(baseURL string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(trimmed, "/chat/completions") {
		return trimmed
	}
	if strings.HasSuffix(trimmed, "/paas/v4") {
		return trimmed + "/chat/completions"
	}
	return appendV1Path(baseURL, "/chat/completions")
}

// openAIResponsesEndpoint constructs the responses API URL from a base URL.
// If the URL already ends with "/v1", it appends "/responses" directly;
// otherwise it appends "/v1/responses".
func openAIResponsesEndpoint(baseURL string) string {
	return appendV1Path(baseURL, "/responses")
}

// appendV1Path appends a sub-path under /v1 to a base URL.
// If the base URL already ends with "/v1", the sub-path is appended directly;
// otherwise "/v1" is prepended to the sub-path.
// This is the shared logic behind openAIChatCompletionsEndpoint,
// openAIResponsesEndpoint, and AnthropicMessagesEndpoint.
func appendV1Path(baseURL, subPath string) string {
	trimmed := strings.TrimRight(baseURL, "/")
	if openAICompatEndpointHasVersionSuffix(trimmed) {
		return trimmed + subPath
	}
	return trimmed + "/v1" + subPath
}

func openAICompatEndpointHasVersionSuffix(endpoint string) bool {
	lastSlash := strings.LastIndex(endpoint, "/")
	if lastSlash < 0 || lastSlash == len(endpoint)-1 {
		return false
	}
	segment := strings.ToLower(endpoint[lastSlash+1:])
	if len(segment) < 2 || segment[0] != 'v' {
		return false
	}
	for _, r := range segment[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// sanitizeToolMessages converts tool-calling messages to plain user/assistant
// format for upstream providers that don't support the OpenAI tool calling
// protocol. Specifically:
//   - role:"tool" messages ->role:"user" with content "[Tool Result] ..."
//   - role:"assistant" messages with tool_calls ->keep as assistant, replace
//     content with a text summary of the tool calls, remove tool_calls field
//   - Consecutive converted tool results are merged into a single user message
//     to avoid violating user/assistant alternation requirements.
//   - All other messages pass through unchanged.
func sanitizeToolMessages(raw interface{}) interface{} {
	msgs := openAICompatForwardSlice(raw)
	if len(msgs) == 0 {
		return raw
	}
	out := make([]interface{}, 0, len(msgs))
	for _, m := range msgs {
		msg := mapFromAny(m)
		if msg == nil {
			out = append(out, m)
			continue
		}
		role, _ := msg["role"].(string)
		switch role {
		case "tool":
			// Convert tool result to user message
			content := openAICompatForwardTextContent(msg["content"])
			name, _ := msg["name"].(string)
			label := "[Tool Result"
			if name != "" {
				label += ": " + name
			}
			label += "] "
			newContent := label + content

			// Merge with previous user message if the last output is also
			// a user message (from a prior tool conversion), to avoid
			// consecutive user messages that some APIs reject.
			if len(out) > 0 {
				prev, ok := out[len(out)-1].(map[string]interface{})
				if ok {
					prevRole, _ := prev["role"].(string)
					if prevRole == "user" {
						prevContent, _ := prev["content"].(string)
						prev["content"] = prevContent + "\n" + newContent
						continue
					}
				}
			}
			out = append(out, map[string]interface{}{
				"role":    "user",
				"content": newContent,
			})
		case "assistant":
			toolCalls, hasToolCalls := msg["tool_calls"]
			if !hasToolCalls || toolCalls == nil {
				out = append(out, m)
				continue
			}
			// Convert assistant tool_calls to plain text
			content := openAICompatForwardTextContent(msg["content"])
			callSummary := summarizeToolCalls(toolCalls)
			if content != "" && callSummary != "" {
				content = content + "\n" + callSummary
			} else if callSummary != "" {
				content = callSummary
			}
			cleaned := make(map[string]interface{}, len(msg))
			for k, v := range msg {
				if k == "tool_calls" {
					continue
				}
				cleaned[k] = v
			}
			cleaned["content"] = content
			out = append(out, cleaned)
		default:
			out = append(out, m)
		}
	}
	return out
}

// summarizeToolCalls converts a tool_calls array into a human-readable text
// summary for providers that don't support tool calling.
func summarizeToolCalls(raw interface{}) string {
	calls := openAICompatForwardSlice(raw)
	if len(calls) == 0 {
		return ""
	}
	var b strings.Builder
	for i, c := range calls {
		call := mapFromAny(c)
		if call == nil {
			continue
		}
		fn := mapFromAny(call["function"])
		if fn == nil {
			continue
		}
		name, _ := fn["name"].(string)
		args := stringifyOpenAICompatForwardSummaryArguments(fn["arguments"])
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("[Tool Call: ")
		b.WriteString(name)
		b.WriteString("]")
		if args != "" && args != "{}" {
			// Truncate very long arguments to avoid bloating context
			if len(args) > 200 {
				args = args[:200] + "..."
			}
			b.WriteString(" ")
			b.WriteString(args)
		}
	}
	return b.String()
}

func stringifyOpenAICompatForwardSummaryArguments(raw interface{}) string {
	switch v := raw.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		data, err := json.Marshal(v)
		if err == nil && len(data) > 0 && string(data) != "null" {
			return string(data)
		}
		return fmt.Sprint(v)
	}
}

// OverrideOpenAIResponseModel rewrites the top-level model field in an
// OpenAI-compatible JSON response when present.
func OverrideOpenAIResponseModel(body []byte, model string) []byte {
	if strings.TrimSpace(model) == "" {
		return body
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	payload["model"] = model
	data, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return data
}
