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
	_, hasTools := normalized["tools"]
	if !hasTools {
		delete(normalized, "tool_choice")
		delete(normalized, "parallel_tool_calls")
	} else if isDefaultPromptCacheToolChoice(normalized["tool_choice"]) {
		delete(normalized, "tool_choice")
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
