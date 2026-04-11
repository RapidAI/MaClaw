package openai

import (
	"fmt"
	"strings"

	"ds2api/internal/config"
	"ds2api/internal/util"
)

func normalizeOpenAIChatRequest(store *config.Store, req map[string]any) (util.StandardRequest, error) {
	model, _ := req["model"].(string)
	messagesRaw, _ := req["messages"].([]any)
	if strings.TrimSpace(model) == "" || len(messagesRaw) == 0 {
		return util.StandardRequest{}, fmt.Errorf("Request must include 'model' and 'messages'.")
	}
	resolvedModel, ok := config.ResolveModel(store, model)
	if !ok {
		return util.StandardRequest{}, fmt.Errorf("Model '%s' is not available.", model)
	}
	thinkingEnabled, searchEnabled, _ := config.GetModelConfig(resolvedModel)
	responseModel := strings.TrimSpace(model)
	if responseModel == "" {
		responseModel = resolvedModel
	}
	finalPrompt, toolNames := buildOpenAIFinalPrompt(messagesRaw, req["tools"])
	passThrough := collectOpenAIChatPassThrough(req)

	return util.StandardRequest{
		Surface:        "openai_chat",
		RequestedModel: strings.TrimSpace(model),
		ResolvedModel:  resolvedModel,
		ResponseModel:  responseModel,
		Messages:       messagesRaw,
		FinalPrompt:    finalPrompt,
		ToolNames:      toolNames,
		Stream:         util.ToBool(req["stream"]),
		Thinking:       thinkingEnabled,
		Search:         searchEnabled,
		PassThrough:    passThrough,
	}, nil
}

func normalizeOpenAIResponsesRequest(store *config.Store, req map[string]any) (util.StandardRequest, error) {
	model, _ := req["model"].(string)
	model = strings.TrimSpace(model)
	if model == "" {
		return util.StandardRequest{}, fmt.Errorf("Request must include 'model'.")
	}
	resolvedModel, ok := config.ResolveModel(store, model)
	if !ok {
		return util.StandardRequest{}, fmt.Errorf("Model '%s' is not available.", model)
	}
	thinkingEnabled, searchEnabled, _ := config.GetModelConfig(resolvedModel)

	// Keep width-control as an explicit policy hook even if current default is true.
	allowWideInput := true
	if store != nil {
		allowWideInput = store.CompatWideInputStrictOutput()
	}
	var messagesRaw []any
	if allowWideInput {
		messagesRaw = responsesMessagesFromRequest(req)
	} else if msgs, ok := req["messages"].([]any); ok && len(msgs) > 0 {
		messagesRaw = msgs
	}
	if len(messagesRaw) == 0 {
		return util.StandardRequest{}, fmt.Errorf("Request must include 'input' or 'messages'.")
	}
	finalPrompt, toolNames := buildOpenAIFinalPrompt(messagesRaw, req["tools"])
	passThrough := collectOpenAIChatPassThrough(req)

	return util.StandardRequest{
		Surface:        "openai_responses",
		RequestedModel: model,
		ResolvedModel:  resolvedModel,
		ResponseModel:  model,
		Messages:       messagesRaw,
		FinalPrompt:    finalPrompt,
		ToolNames:      toolNames,
		Stream:         util.ToBool(req["stream"]),
		Thinking:       thinkingEnabled,
		Search:         searchEnabled,
		PassThrough:    passThrough,
	}, nil
}

func collectOpenAIChatPassThrough(req map[string]any) map[string]any {
	out := map[string]any{}
	for _, k := range []string{
		"temperature",
		"top_p",
		"max_tokens",
		"max_completion_tokens",
		"presence_penalty",
		"frequency_penalty",
		"stop",
	} {
		if v, ok := req[k]; ok {
			out[k] = v
		}
	}
	// Clamp max_tokens / max_completion_tokens to provider limit
	// (e.g. Alibaba Bailian caps at 65536).
	for _, k := range []string{"max_tokens", "max_completion_tokens"} {
		if v, ok := out[k]; ok {
			if f, ok := v.(float64); ok && f > 65536 {
				out[k] = float64(65536)
			}
		}
	}
	return out
}
