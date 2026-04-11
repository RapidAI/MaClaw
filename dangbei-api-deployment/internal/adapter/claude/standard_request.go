package claude

import (
	"fmt"
	"strings"

	"ds2api/internal/config"
	"ds2api/internal/util"
)

type claudeNormalizedRequest struct {
	Standard           util.StandardRequest
	NormalizedMessages []any
}

func normalizeClaudeRequest(store *config.Store, req map[string]any) (claudeNormalizedRequest, error) {
	model, _ := req["model"].(string)
	messagesRaw, _ := req["messages"].([]any)
	if strings.TrimSpace(model) == "" || len(messagesRaw) == 0 {
		return claudeNormalizedRequest{}, fmt.Errorf("Request must include 'model' and 'messages'.")
	}
	if _, ok := req["max_tokens"]; !ok {
		req["max_tokens"] = 8192
	}
	// Clamp max_tokens to provider limit (e.g. Alibaba Bailian caps at 65536).
	if mt, ok := req["max_tokens"]; ok {
		if mtf, ok := mt.(float64); ok && mtf > 65536 {
			req["max_tokens"] = 65536
		}
	}
	normalizedMessages := normalizeClaudeMessages(messagesRaw)
	payload := cloneMap(req)
	payload["messages"] = normalizedMessages
	toolsRequested, _ := req["tools"].([]any)
	if len(toolsRequested) > 0 && !hasSystemMessage(normalizedMessages) {
		payload["messages"] = append([]any{map[string]any{"role": "system", "content": buildClaudeToolPrompt(toolsRequested)}}, normalizedMessages...)
	}

	dsPayload := util.ConvertClaudeToDeepSeek(payload, store)
	dsModel, _ := dsPayload["model"].(string)
	thinkingEnabled, searchEnabled, ok := config.GetModelConfig(dsModel)
	if !ok {
		thinkingEnabled = false
		searchEnabled = false
	}
	finalPrompt := util.MessagesPrepare(toMessageMaps(dsPayload["messages"]))
	toolNames := extractClaudeToolNames(toolsRequested)

	return claudeNormalizedRequest{
		Standard: util.StandardRequest{
			Surface:        "anthropic_messages",
			RequestedModel: strings.TrimSpace(model),
			ResolvedModel:  dsModel,
			ResponseModel:  strings.TrimSpace(model),
			Messages:       payload["messages"].([]any),
			FinalPrompt:    finalPrompt,
			ToolNames:      toolNames,
			Stream:         util.ToBool(req["stream"]),
			Thinking:       thinkingEnabled,
			Search:         searchEnabled,
		},
		NormalizedMessages: normalizedMessages,
	}, nil
}
