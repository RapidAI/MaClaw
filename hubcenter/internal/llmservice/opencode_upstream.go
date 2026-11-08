package llmservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	corellm "github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

// tryForwardOpenCode handles an OpenCode provider. A stored key is a paid
// account and is sent as a bearer token, the same way Codex is. An empty key
// is the anonymous free tier. The model id selects Zen or the Go gateway.
func tryForwardOpenCode(ctx context.Context, client *http.Client, provider *llmpool.ProviderConfig, body map[string]any, upstreamModel, responseModel string, retry bool) (*providerForwardResponse, bool, error) {
	if provider == nil || !corelib.IsOpenCodeURL(provider.APIURL) {
		return nil, false, nil
	}
	cfg := openCodeMaclawConfig(provider, upstreamModel)
	ctx = corellm.OpenCodeFreeWithoutLocalWorkspace(ctx)
	fwd := make(map[string]interface{}, len(body)+1)
	for k, v := range body {
		fwd[k] = v
	}
	if strings.TrimSpace(upstreamModel) != "" {
		fwd["model"] = upstreamModel
	}
	prepared, ok := corellm.PrepareOpenCodeChatBody(cfg, fwd)
	if !ok {
		return nil, false, nil
	}
	wireModel, _ := fwd["model"].(string)
	// Metered models stay on the same forwarder as Codex, including a provider
	// whose WireAPI is responses. Only Zen free chat models and the
	// Responses-only ids need the collected stream.
	if !corelib.OpenCodeUsesResponsesAPI(prepared.URL, wireModel) && !corelib.OpenCodeZenAgentModel(wireModel) {
		prepared.Model = wireModel
		prepared = corelib.BindOpenCodeSessionID(ctx, prepared)
		var data []byte
		var status int
		var err error
		if retry {
			data, status, _, err = corelib.ForwardOpenAICompatRequestWithRetry(ctx, prepared, fwd, client, responseModel)
		} else {
			data, status, err = corelib.ForwardOpenAICompatRequest(ctx, prepared, fwd, client, responseModel)
		}
		return finishOpenCodeForward(provider, data, status, err, responseModel)
	}
	data, status, err := forwardOpenCodeSpecial(ctx, prepared, fwd, client, retry)
	if err != nil {
		var httpErr *corellm.HTTPStatusError
		if errors.As(err, &httpErr) && httpErr != nil && status == 0 {
			status = httpErr.StatusCode
			data = httpErr.Body
			err = nil
		}
	}
	return finishOpenCodeForward(provider, data, status, err, responseModel)
}

func forwardOpenCodeSpecial(ctx context.Context, cfg corelib.MaclawLLMConfig, body map[string]interface{}, client *http.Client, retry bool) ([]byte, int, error) {
	attempts := 1
	if retry {
		attempts = 3
	}
	var data []byte
	var status int
	var err error
	for attempt := 1; attempt <= attempts; attempt++ {
		fwd := make(map[string]interface{}, len(body))
		for k, v := range body {
			fwd[k] = v
		}
		data, status, err = corellm.ForwardOpenCodeChat(ctx, cfg, fwd, client)
		if err == nil || !corelib.ShouldRetryLLMUpstream(status, err) || ctx.Err() != nil || attempt == attempts {
			return data, status, err
		}
		select {
		case <-ctx.Done():
			return data, status, ctx.Err()
		case <-time.After(time.Duration(attempt*attempt) * 150 * time.Millisecond):
		}
	}
	return data, status, err
}

func finishOpenCodeForward(provider *llmpool.ProviderConfig, data []byte, status int, err error, responseModel string) (*providerForwardResponse, bool, error) {
	if err != nil {
		if status >= 400 && status <= 599 {
			if len(data) == 0 {
				data = []byte(err.Error())
			}
			return &providerForwardResponse{StatusCode: status, Body: data}, true, nil
		}
		id := ""
		if provider != nil {
			id = provider.ID
		}
		return nil, true, fmt.Errorf("forward to %s: %w", id, err)
	}
	if strings.TrimSpace(responseModel) != "" {
		data = corelib.OverrideOpenAIResponseModel(data, responseModel)
	}
	if status == 0 {
		status = http.StatusOK
	}
	return &providerForwardResponse{StatusCode: status, Body: data}, true, nil
}

// OpenCodeCatalogURL is the model list for an OpenCode provider. The standard
// inference catalog lists the -free ids and the metered ids as separate models.
// An empty URL with the OpenCode client uses that catalog, and an empty key is
// allowed.
func OpenCodeCatalogURL(apiURL, agentType string) string {
	apiURL = strings.TrimSpace(apiURL)
	if corelib.IsOpenCodeURL(apiURL) || (apiURL == "" && strings.EqualFold(RequestAgentType(agentType), "OpenCode")) {
		return "https://opencode.ai/inference/v1"
	}
	return apiURL
}

// ForwardProviderCompletion runs one non-streaming completion through the
// same OpenCode and vendor routing as a live proxy request. The admin
// availability test uses it so a Zen free model is not parsed as JSON while
// the upstream is still speaking SSE.
func ForwardProviderCompletion(ctx context.Context, client *http.Client, provider *llmpool.ProviderConfig, body map[string]any, model string) (int, []byte, error) {
	resp, err := forwardToProviderAttempts(ctx, client, provider, body, model, model, false)
	if err != nil {
		return 0, nil, err
	}
	if resp == nil {
		return 0, nil, fmt.Errorf("empty response")
	}
	return resp.StatusCode, resp.Body, nil
}

func openCodeMaclawConfig(provider *llmpool.ProviderConfig, model string) corelib.MaclawLLMConfig {
	agent := RequestAgentType(provider.AgentType)
	// The editor default is openclaw. An OpenCode host still has to present
	// the official client, unless the admin picked a different User-Agent.
	if agent == "" || strings.EqualFold(agent, "openclaw") {
		agent = "OpenCode"
	}
	return WithMemberUpstreamOverrides(corelib.MaclawLLMConfig{
		URL:        provider.APIURL,
		Key:        strings.TrimSpace(provider.APIKey),
		Model:      model,
		Protocol:   provider.Protocol,
		WireAPI:    provider.WireAPI,
		AgentType:  agent,
		TimeoutSec: provider.UpstreamTimeoutSec,
	}, provider)
}

type openCodeStreamPlan struct {
	cfg       corelib.MaclawLLMConfig
	endpoint  string
	payload   []byte
	responses bool
}

// planOpenCodeStream rewrites a streaming OpenCode request onto the gateway
// that model accepts. Responses-only models are flagged so the caller can
// translate the upstream stream back into chat SSE.
func planOpenCodeStream(provider *llmpool.ProviderConfig, body map[string]any, upstreamModel string) (openCodeStreamPlan, bool, error) {
	if provider == nil || !corelib.IsOpenCodeURL(provider.APIURL) {
		return openCodeStreamPlan{}, false, nil
	}
	cfg := openCodeMaclawConfig(provider, upstreamModel)
	chat := make(map[string]interface{}, len(body)+1)
	for k, v := range body {
		chat[k] = v
	}
	if strings.TrimSpace(upstreamModel) != "" {
		chat["model"] = upstreamModel
	}
	cfg, _ = corellm.PrepareOpenCodeChatBody(cfg, chat)
	// Keep the caller's map intact. A later failover retry would otherwise
	// send the injected Zen tools to a different provider.
	wireModel, _ := chat["model"].(string)
	if corelib.OpenCodeUsesResponsesAPI(cfg.URL, wireModel) {
		return openCodeStreamPlan{cfg: cfg, responses: true}, true, nil
	}
	// Zen chat injects tools and may delete the default output cap. Apply
	// member overrides after that rewrite so extra_body and drop_fields win.
	corelib.FinishMemberUpstreamBody(cfg, chat)
	payload, err := json.Marshal(chat)
	if err != nil {
		return openCodeStreamPlan{}, true, err
	}
	endpoint := corellm.BuildOpenAIChatCompletionsEndpoint(cfg.URL)
	return openCodeStreamPlan{cfg: cfg, endpoint: endpoint, payload: payload}, true, nil
}

func streamOpenCodeResponses(ctx context.Context, client *http.Client, cfg corelib.MaclawLLMConfig, body map[string]any, responseModel string, dst ProxyStreamWriter) (*providerStreamResult, error) {
	messages, _ := body["messages"].([]interface{})
	resp, err := corellm.DoResponsesAPIRequestStreamWithOptions(ctx, cfg, messages, openCodeProxyTools(body["tools"]), client, nil, nil, corellm.ResponsesAPIRequestOptions{
		ExtraBody: corellm.OpenCodeResponsesBudgetExtra(body),
	})
	if err != nil {
		var httpErr *corellm.HTTPStatusError
		if errors.As(err, &httpErr) && httpErr != nil {
			return &providerStreamResult{statusCode: httpErr.StatusCode, errorBody: httpErr.Body}, nil
		}
		return nil, err
	}
	content := ""
	reasoning := ""
	finish := "stop"
	var toolCalls []corellm.ToolCall
	var usage *corellm.Usage
	if resp != nil {
		usage = resp.Usage
		if len(resp.Choices) > 0 {
			choice := corellm.NormalizeOpenCodeFreeChoice(resp.Choices[0])
			content = choice.Message.Content
			reasoning = choice.Message.ReasoningContent
			toolCalls = choice.Message.ToolCalls
			if strings.TrimSpace(choice.FinishReason) != "" {
				finish = choice.FinishReason
			}
		}
	}
	model := strings.TrimSpace(responseModel)
	if model == "" {
		model = cfg.UpstreamModel()
	}
	sse := openCodeChatSSE(model, content, reasoning, finish, toolCalls, usage)
	result := &providerStreamResult{statusCode: http.StatusOK}
	if err := proxyProviderSSE(strings.NewReader(sse), dst, model, result, body); err != nil {
		return result, err
	}
	if !result.wroteBusinessStream {
		return result, fmt.Errorf("upstream stream ended before business data")
	}
	return result, nil
}

func openCodeProxyTools(raw any) []map[string]interface{} {
	switch tools := raw.(type) {
	case []map[string]interface{}:
		return tools
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(tools))
		for _, tool := range tools {
			item, ok := tool.(map[string]interface{})
			if ok {
				out = append(out, item)
			}
		}
		return out
	default:
		return nil
	}
}

func streamOpenCodeFreeClient(ctx context.Context, payload []byte, responseModel, upstreamModel, apiKey string, body map[string]any, dst ProxyStreamWriter, errBody []byte) (*providerStreamResult, bool, error) {
	if !corelib.OpenCodeZenAgentModel(openCodeZenFreeWireModel(payload, upstreamModel)) {
		return nil, false, nil
	}
	if errBody != nil && !bytes.Contains(errBody, []byte("FreeUsageLimitError")) {
		return nil, false, nil
	}
	// The identified client is the same one the GUI uses. Hub does not attach
	// this server's working directory; that checkout is not the caller's project.
	resp, err := corellm.CompleteOpenCodeFreeClient(corellm.OpenCodeFreeWithKey(ctx, apiKey), payload)
	if err != nil || resp == nil {
		var httpErr *corellm.HTTPStatusError
		if errors.As(err, &httpErr) && httpErr != nil {
			return &providerStreamResult{statusCode: httpErr.StatusCode, errorBody: httpErr.Body}, true, nil
		}
		if errBody == nil && err != nil {
			return nil, true, err
		}
		return nil, false, nil
	}
	content := ""
	reasoning := ""
	finish := "stop"
	var toolCalls []corellm.ToolCall
	usage := resp.Usage
	if len(resp.Choices) > 0 {
		msg := corellm.PresentOpenCodeFreeMessage(corellm.OpenAIDeclaredToolNames(body["tools"]), resp.Choices[0].Message)
		content = msg.Content
		reasoning = msg.ReasoningContent
		toolCalls = msg.ToolCalls
		if strings.TrimSpace(resp.Choices[0].FinishReason) != "" {
			finish = resp.Choices[0].FinishReason
		}
		finish = corellm.OpenCodeFreeFinishReason(finish, toolCalls)
	}
	model := strings.TrimSpace(responseModel)
	if model == "" {
		model = strings.TrimSpace(upstreamModel)
	}
	sse := openCodeChatSSE(model, content, reasoning, finish, toolCalls, usage)
	result := &providerStreamResult{statusCode: http.StatusOK, outputText: content}
	if err := proxyProviderSSE(strings.NewReader(sse), dst, model, result, body); err != nil {
		return result, true, err
	}
	if !result.wroteBusinessStream {
		return result, true, fmt.Errorf("upstream stream ended before business data")
	}
	return result, true, nil
}

// openCodeZenFreeWireModel is the id the free client will send. The planned
// payload already rewrote big-pickle; the caller model is the fallback.
func openCodeZenFreeWireModel(payload []byte, upstreamModel string) string {
	raw := strings.TrimSpace(upstreamModel)
	if len(payload) > 0 {
		var body struct {
			Model string `json:"model"`
		}
		if json.Unmarshal(payload, &body) == nil && strings.TrimSpace(body.Model) != "" {
			raw = body.Model
		}
	}
	return corelib.OpenCodeWireModel("", "", raw)
}

func openCodeChatSSE(model, content, reasoning, finish string, toolCalls []corellm.ToolCall, usage *corellm.Usage) string {
	delta := map[string]any{"role": "assistant", "content": content}
	if strings.TrimSpace(reasoning) != "" {
		delta["reasoning_content"] = reasoning
	}
	if len(toolCalls) > 0 {
		calls := make([]any, 0, len(toolCalls))
		for i, call := range toolCalls {
			kind := strings.TrimSpace(call.Type)
			if kind == "" {
				kind = "function"
			}
			calls = append(calls, map[string]any{
				"index": i, "id": call.ID, "type": kind,
				"function": map[string]any{"name": call.Function.Name, "arguments": call.Function.Arguments},
			})
		}
		delta["tool_calls"] = calls
	}
	first, _ := json.Marshal(map[string]any{
		"id": "chatcmpl-opencode", "object": "chat.completion.chunk", "model": model,
		"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}},
	})
	lastChunk := map[string]any{
		"id": "chatcmpl-opencode", "object": "chat.completion.chunk", "model": model,
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finish}},
	}
	if usage != nil && (usage.PromptTokens > 0 || usage.CompletionTokens > 0) {
		lastChunk["usage"] = map[string]any{
			"prompt_tokens": usage.PromptTokens, "completion_tokens": usage.CompletionTokens,
			"total_tokens": usage.PromptTokens + usage.CompletionTokens,
		}
	}
	last, _ := json.Marshal(lastChunk)
	return "data: " + string(first) + "\n\ndata: " + string(last) + "\n\ndata: [DONE]\n\n"
}
