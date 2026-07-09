package corelib

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var ErrLLMPromptCacheInvalidResponse = errors.New("llm prompt cache invalid response")

// DefaultLLMPromptCacheMaxResponseBytes caps a single cached response body.
const DefaultLLMPromptCacheMaxResponseBytes = 2 * 1024 * 1024

type LLMPromptCacheOptions struct {
	Enabled                      bool
	NormalizeDeterministicParams bool
	IgnoreModelField             bool
	IgnoreUserField              bool
	IgnoreMetadataField          bool
}

type LLMPromptCacheDecision struct {
	Reason    string
	Cacheable bool
}

// LLMPromptCacheStoreDecision reports whether a completed response may be stored.
type LLMPromptCacheStoreDecision struct {
	Reason string
	Store  bool
}

func LLMPromptCacheable(body map[string]any, opts LLMPromptCacheOptions) LLMPromptCacheDecision {
	if !opts.Enabled {
		return LLMPromptCacheDecision{Reason: "disabled"}
	}
	if len(body) == 0 {
		return LLMPromptCacheDecision{Reason: "empty_body"}
	}
	if store, ok := body["store"].(bool); ok && store {
		return LLMPromptCacheDecision{Reason: "store"}
	}
	if n, ok := promptCacheIntValue(body["n"]); ok && n > 1 {
		return LLMPromptCacheDecision{Reason: "multi_choice"}
	}
	if value, ok := promptCacheFloatValue(body["temperature"]); ok && value != 0 {
		return LLMPromptCacheDecision{Reason: "temperature"}
	}
	if value, ok := promptCacheFloatValue(body["top_p"]); ok && value > 0 && value < 1 {
		return LLMPromptCacheDecision{Reason: "top_p"}
	}
	if value, ok := promptCacheFloatValue(body["presence_penalty"]); ok && value != 0 {
		return LLMPromptCacheDecision{Reason: "presence_penalty"}
	}
	if value, ok := promptCacheFloatValue(body["frequency_penalty"]); ok && value != 0 {
		return LLMPromptCacheDecision{Reason: "frequency_penalty"}
	}
	if hasNonEmptyPromptCacheMap(body["logit_bias"]) {
		return LLMPromptCacheDecision{Reason: "logit_bias"}
	}
	return LLMPromptCacheDecision{Cacheable: true}
}

func LLMPromptCacheKey(authorizedModel string, requestedModel string, body map[string]any, opts LLMPromptCacheOptions) (string, string, error) {
	authorizedModel = strings.TrimSpace(authorizedModel)
	if authorizedModel == "" {
		return "", "", fmt.Errorf("authorized model is required")
	}
	normalizedBody := NormalizeLLMPromptCacheBody(body, opts)
	canonicalBody, err := json.Marshal(normalizedBody)
	if err != nil {
		return "", "", err
	}
	requestedModel = strings.TrimSpace(requestedModel)
	if opts.IgnoreModelField {
		requestedModel = ""
	}
	fingerprint := map[string]any{
		"authorized_model": authorizedModel,
		"requested_model":  requestedModel,
		"body":             json.RawMessage(canonicalBody),
	}
	payload, err := json.Marshal(fingerprint)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])
	return "llm_resp_" + hash, hash, nil
}

func NormalizeLLMPromptCacheBody(body map[string]any, opts LLMPromptCacheOptions) map[string]any {
	normalized, _ := normalizePromptCacheValue(body).(map[string]any)
	if normalized == nil {
		return map[string]any{}
	}
	if opts.IgnoreModelField {
		delete(normalized, "model")
	}
	if opts.IgnoreUserField {
		delete(normalized, "user")
	}
	if opts.IgnoreMetadataField {
		delete(normalized, "metadata")
	}
	delete(normalized, "stream")
	delete(normalized, "stream_options")
	delete(normalized, "prompt_cache_key")
	delete(normalized, "safety_identifier")
	delete(normalized, "service_tier")
	if store, ok := normalized["store"].(bool); ok && !store {
		delete(normalized, "store")
	}
	if opts.NormalizeDeterministicParams {
		deleteDefaultDeterministicParams(normalized)
	}
	if tools, ok := normalized["tools"].([]any); ok && len(tools) == 0 {
		delete(normalized, "tools")
	}
	if functions, ok := normalized["functions"].([]any); ok && len(functions) == 0 {
		delete(normalized, "functions")
	}
	_, hasTools := normalized["tools"]
	_, hasFunctions := normalized["functions"]
	if !hasTools {
		delete(normalized, "tool_choice")
		delete(normalized, "parallel_tool_calls")
	} else if isDefaultPromptCacheToolChoice(normalized["tool_choice"]) {
		delete(normalized, "tool_choice")
	}
	if !hasFunctions {
		// Legacy OpenAI functions API: absent functions means function_call is noise.
		delete(normalized, "function_call")
	} else if isDefaultPromptCacheFunctionCall(normalized["function_call"]) {
		delete(normalized, "function_call")
	}
	if value, ok := normalized["parallel_tool_calls"].(bool); ok && value {
		delete(normalized, "parallel_tool_calls")
	}
	if value, ok := normalized["logprobs"].(bool); ok && !value {
		delete(normalized, "logprobs")
		delete(normalized, "top_logprobs")
	}
	if isDefaultPromptCacheResponseFormat(normalized["response_format"]) {
		delete(normalized, "response_format")
	}
	if isDefaultPromptCacheModalities(normalized["modalities"]) {
		delete(normalized, "modalities")
	}
	return normalized
}

// LLMPromptCacheShouldStore reports whether a completed upstream response is safe to cache.
// statusCode 0 is treated as HTTP 200 for callers that only have a body.
func LLMPromptCacheShouldStore(respBody []byte, statusCode int) LLMPromptCacheStoreDecision {
	return LLMPromptCacheShouldStoreWithLimit(respBody, statusCode, DefaultLLMPromptCacheMaxResponseBytes)
}

// LLMPromptCacheShouldStoreWithLimit is like LLMPromptCacheShouldStore with an explicit size cap.
// maxBytes <= 0 uses DefaultLLMPromptCacheMaxResponseBytes.
func LLMPromptCacheShouldStoreWithLimit(respBody []byte, statusCode int, maxBytes int) LLMPromptCacheStoreDecision {
	if maxBytes <= 0 {
		maxBytes = DefaultLLMPromptCacheMaxResponseBytes
	}
	if statusCode == 0 {
		statusCode = 200
	}
	if statusCode < 200 || statusCode >= 300 {
		return LLMPromptCacheStoreDecision{Reason: "status"}
	}
	if len(respBody) == 0 {
		return LLMPromptCacheStoreDecision{Reason: "empty_body"}
	}
	if len(respBody) > maxBytes {
		return LLMPromptCacheStoreDecision{Reason: "too_large"}
	}
	if !json.Valid(respBody) {
		return LLMPromptCacheStoreDecision{Reason: "invalid_json"}
	}
	if LLMPromptCacheResponseHasToolCall(respBody) {
		return LLMPromptCacheStoreDecision{Reason: "tool_calls"}
	}
	return LLMPromptCacheStoreDecision{Store: true}
}

// LLMPromptCacheResponseHasToolCall detects chat-completion and responses-API tool/function calls.
func LLMPromptCacheResponseHasToolCall(respBody []byte) bool {
	var payload map[string]any
	if len(respBody) == 0 || json.Unmarshal(respBody, &payload) != nil {
		return false
	}
	for _, rawChoice := range promptCacheAnySlice(payload["choices"]) {
		choice, _ := rawChoice.(map[string]any)
		if choice == nil {
			continue
		}
		finish := strings.ToLower(strings.TrimSpace(promptCacheStringValue(choice["finish_reason"])))
		if finish == "tool_calls" || finish == "function_call" {
			return true
		}
		message, _ := choice["message"].(map[string]any)
		if message == nil {
			continue
		}
		if len(promptCacheAnySlice(message["tool_calls"])) > 0 {
			return true
		}
		if functionCall, _ := message["function_call"].(map[string]any); len(functionCall) > 0 {
			return true
		}
	}
	for _, rawOutput := range promptCacheAnySlice(payload["output"]) {
		output, _ := rawOutput.(map[string]any)
		if output == nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(promptCacheStringValue(output["type"])), "function_call") {
			return true
		}
	}
	return false
}

// SynthesizeOpenAIChatCompletionSSE converts a cached non-streaming chat.completion
// JSON body into an OpenAI-compatible SSE stream (content and/or tool_calls + finish + [DONE]).
func SynthesizeOpenAIChatCompletionSSE(respBody []byte) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return nil, err
	}
	id := promptCacheStringValue(payload["id"])
	model := promptCacheStringValue(payload["model"])
	created, _ := promptCacheIntValue(payload["created"])
	choices := promptCacheAnySlice(payload["choices"])
	if len(choices) == 0 {
		return nil, fmt.Errorf("%w: missing choices", ErrLLMPromptCacheInvalidResponse)
	}

	var events []string
	for i, rawChoice := range choices {
		choice, _ := rawChoice.(map[string]any)
		if choice == nil {
			continue
		}
		index := i
		if idx, ok := promptCacheIntValue(choice["index"]); ok {
			index = int(idx)
		}
		message, _ := choice["message"].(map[string]any)
		role := "assistant"
		content := ""
		var toolCalls []any
		var functionCall map[string]any
		if message != nil {
			if r := strings.TrimSpace(promptCacheStringValue(message["role"])); r != "" {
				role = r
			}
			content = promptCacheMessageContentString(message["content"])
			toolCalls = promptCacheAnySlice(message["tool_calls"])
			if fc, ok := message["function_call"].(map[string]any); ok && len(fc) > 0 {
				functionCall = fc
			}
		}
		finish := promptCacheStringValue(choice["finish_reason"])
		if finish == "" {
			if len(toolCalls) > 0 {
				finish = "tool_calls"
			} else if functionCall != nil {
				finish = "function_call"
			} else {
				finish = "stop"
			}
		}

		delta := map[string]any{"role": role}
		if content != "" {
			delta["content"] = content
		}
		if len(toolCalls) > 0 {
			streamTCs := make([]any, 0, len(toolCalls))
			for ti, rawTC := range toolCalls {
				tc, _ := rawTC.(map[string]any)
				if tc == nil {
					continue
				}
				// OpenAI stream tool_calls include an index field for multi-call assembly.
				streamTC := map[string]any{"index": ti}
				for k, v := range tc {
					streamTC[k] = v
				}
				if _, ok := streamTC["type"]; !ok {
					streamTC["type"] = "function"
				}
				streamTCs = append(streamTCs, streamTC)
			}
			if len(streamTCs) > 0 {
				delta["tool_calls"] = streamTCs
			}
		}
		if functionCall != nil {
			delta["function_call"] = functionCall
		}
		contentChunk := map[string]any{
			"id":      id,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   model,
			"choices": []any{
				map[string]any{
					"index":         index,
					"delta":         delta,
					"finish_reason": nil,
				},
			},
		}
		contentJSON, err := json.Marshal(contentChunk)
		if err != nil {
			return nil, err
		}
		events = append(events, "data: "+string(contentJSON))

		finishChunk := map[string]any{
			"id":      id,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   model,
			"choices": []any{
				map[string]any{
					"index":         index,
					"delta":         map[string]any{},
					"finish_reason": finish,
				},
			},
		}
		if usage := payload["usage"]; usage != nil && i == len(choices)-1 {
			finishChunk["usage"] = usage
		}
		finishJSON, err := json.Marshal(finishChunk)
		if err != nil {
			return nil, err
		}
		events = append(events, "data: "+string(finishJSON))
	}
	events = append(events, "data: [DONE]")
	return []byte(strings.Join(events, "\n\n") + "\n\n"), nil
}

func deleteDefaultDeterministicParams(normalized map[string]any) {
	if value, ok := promptCacheFloatValue(normalized["temperature"]); ok && value == 0 {
		delete(normalized, "temperature")
	}
	if value, ok := promptCacheFloatValue(normalized["top_p"]); ok && value >= 1 {
		delete(normalized, "top_p")
	}
	if value, ok := promptCacheFloatValue(normalized["presence_penalty"]); ok && value == 0 {
		delete(normalized, "presence_penalty")
	}
	if value, ok := promptCacheFloatValue(normalized["frequency_penalty"]); ok && value == 0 {
		delete(normalized, "frequency_penalty")
	}
	if value, ok := promptCacheIntValue(normalized["n"]); ok && value <= 1 {
		delete(normalized, "n")
	}
	if value, ok := promptCacheIntValue(normalized["seed"]); ok && value == 0 {
		delete(normalized, "seed")
	}
	if value, ok := promptCacheIntValue(normalized["max_tokens"]); ok && value <= 0 {
		delete(normalized, "max_tokens")
	}
	if value, ok := promptCacheIntValue(normalized["max_completion_tokens"]); ok && value <= 0 {
		delete(normalized, "max_completion_tokens")
	}
}

func normalizePromptCacheValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			normalized := normalizePromptCacheValue(child)
			if normalized == nil {
				continue
			}
			out[key] = normalized
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, child := range typed {
			out = append(out, normalizePromptCacheValue(child))
		}
		return out
	case string, bool, nil:
		return typed
	default:
		return typed
	}
}

func promptCacheStringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return ""
	}
}

func promptCacheMessageContentString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		var b strings.Builder
		for _, part := range typed {
			switch p := part.(type) {
			case string:
				b.WriteString(p)
			case map[string]any:
				if t := strings.TrimSpace(promptCacheStringValue(p["type"])); t == "" || strings.EqualFold(t, "text") {
					b.WriteString(promptCacheStringValue(p["text"]))
				}
			}
		}
		return b.String()
	default:
		return promptCacheStringValue(value)
	}
}

func promptCacheAnySlice(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case []map[string]any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
}

func hasNonEmptyPromptCacheMap(value any) bool {
	m, ok := value.(map[string]any)
	return ok && len(m) > 0
}

func isDefaultPromptCacheResponseFormat(value any) bool {
	m, ok := value.(map[string]any)
	if !ok || len(m) != 1 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(promptCacheStringValue(m["type"])), "text")
}

func isDefaultPromptCacheModalities(value any) bool {
	items, ok := value.([]any)
	if !ok || len(items) != 1 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(promptCacheStringValue(items[0])), "text")
}

func isDefaultPromptCacheToolChoice(value any) bool {
	// "none" is NOT default when tools are present — it disables tool use.
	s := strings.ToLower(strings.TrimSpace(promptCacheStringValue(value)))
	return s == "" || s == "auto"
}

func isDefaultPromptCacheFunctionCall(value any) bool {
	// "none" is NOT default when functions are present — it disables function calls.
	s := strings.ToLower(strings.TrimSpace(promptCacheStringValue(value)))
	return s == "" || s == "auto"
}

func promptCacheFloatValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		f, err := typed.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func promptCacheIntValue(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int64:
		return typed, true
	case float64:
		return int64(typed), true
	case float32:
		return int64(typed), true
	case json.Number:
		i, err := typed.Int64()
		return i, err == nil
	default:
		return 0, false
	}
}
