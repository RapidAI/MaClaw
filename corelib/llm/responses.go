package llm

// Responses API request builder.
// Parallels the Chat Completions builder in client.go for the
// OpenAI Responses API (POST /v1/responses).

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/oauth"
)

// ResponsesAPIRequestOptions controls how a Responses API request is built.
type ResponsesAPIRequestOptions struct {
	Stream    bool
	Tools     []map[string]interface{}
	ExtraBody map[string]interface{}
}

// responsesReservedKeys are top-level keys that ExtraBody must not override.
var responsesReservedKeys = map[string]bool{
	"model":                 true,
	"input":                 true,
	"instructions":          true,
	"stream":                true,
	"tools":                 true,
	"function_call":         true,
	"max_completion_tokens": true,
	"max_tokens":            true,
	"parallel_tool_calls":   true,
	"response_format":       true,
	"tool_choice":           true,
	"stream_options":        true,
	"logprobs":              true,
	"top_logprobs":          true,
	"service_tier":          true,
	"reasoning_effort":      true,
	"modalities":            true,
	"prediction":            true,
	"audio":                 true,
	"web_search_options":    true,
}

// BuildResponsesAPIRequestData constructs the endpoint URL and JSON body
// for a Responses API request.
func BuildResponsesAPIRequestData(
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	opts ResponsesAPIRequestOptions,
) (endpoint string, body []byte, err error) {
	endpoint = BuildResponsesEndpoint(cfg.URL)
	messages = normalizeOpenAIChatToolCallLinkage(messages)

	if cfg.NeedsConservativeOpenAICompatSanitization() {
		messages = SanitizeConservativeOpenAICompatMessages(messages)
	} else {
		messages = SanitizeOpenAICompatRequestMessages(messages, true)
	}
	converted := ConvertToResponsesInput(messages)

	reqBody := map[string]interface{}{
		"model":  cfg.UpstreamModel(),
		"input":  converted.Input,
		"stream": opts.Stream,
	}
	if converted.Instructions != "" {
		reqBody["instructions"] = converted.Instructions
	}
	toolsInput := opts.Tools
	toolsInput = sanitizeOpenAIChatToolsForSDK(toolsInput)
	if cfg.NeedsConservativeOpenAICompatSanitization() {
		toolsInput = corelib.SanitizeCodeGenOpenAIChatTools(toolsInput)
	}
	if tools := ConvertToResponsesTools(toolsInput); len(tools) > 0 {
		reqBody["tools"] = tools
	}
	if opts.ExtraBody != nil {
		if _, ok := opts.ExtraBody["max_output_tokens"]; !ok {
			if v, ok := opts.ExtraBody["max_completion_tokens"]; ok {
				reqBody["max_output_tokens"] = v
			} else if v, ok := opts.ExtraBody["max_tokens"]; ok {
				reqBody["max_output_tokens"] = v
			}
		}
		if _, ok := opts.ExtraBody["text"]; !ok {
			if format := responsesTextFormatFromChatResponseFormat(opts.ExtraBody["response_format"]); format != nil {
				reqBody["text"] = map[string]interface{}{"format": format}
			}
		}
	}
	for k, v := range opts.ExtraBody {
		if responsesReservedKeys[k] {
			continue
		}
		reqBody[k] = v
	}
	// Ensure max_output_tokens is set for Responses API (analogous to ensureMaxOutputTokens for Chat API)
	if _, ok := reqBody["max_output_tokens"]; !ok {
		limit := cfg.EffectiveMaxOutputTokens()
		cacheKey := strings.ToLower(strings.TrimSpace(cfg.Model))
		if cached, ok := maxOutputTokensCache.Load(cacheKey); ok {
			if cachedLimit, isInt := cached.(int); isInt && cachedLimit > 0 && cachedLimit < limit {
				limit = cachedLimit
			}
		}
		reqBody["max_output_tokens"] = limit
	}
	if cfg.NeedsConservativeOpenAICompatSanitization() {
		corelib.SanitizeCodeGenOpenAICompatBody(reqBody)
	}

	body, err = json.Marshal(reqBody)
	return endpoint, body, err
}

func responsesTextFormatFromChatResponseFormat(raw interface{}) map[string]interface{} {
	format := sanitizeOpenAIResponseFormatForSDK(raw)
	if format == nil {
		return nil
	}
	if schema := toStringInterfaceMap(format["json_schema"]); schema != nil && format["type"] == "json_schema" {
		out := map[string]interface{}{"type": "json_schema"}
		for k, v := range schema {
			out[k] = v
		}
		return out
	}
	return format
}

// NewResponsesAPIRequest creates an *http.Request for the Responses API.
// Return signature matches NewOpenAIChatRequest: (req, body, endpoint, err).
func NewResponsesAPIRequest(
	ctx context.Context,
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	opts ResponsesAPIRequestOptions,
) (*http.Request, []byte, string, error) {
	endpoint, data, err := BuildResponsesAPIRequestData(cfg, messages, opts)
	if err != nil {
		return nil, nil, endpoint, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, nil, endpoint, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", cfg.UserAgent())
	req.Header.Set("originator", "codex_cli_rs")
	if cfg.Key != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Key)
	}
	corelib.SetCodeGenClientNameHeaderIfNeededWithName(req, cfg.UserAgent())
	// Codex subscription headers for chatgpt.com/backend-api
	if IsCodexSubscriptionEndpoint(cfg.URL) {
		req.Header.Set("OpenAI-Beta", "responses=experimental")
		if accountID, _ := oauth.ExtractAccountIDFromJWT(cfg.Key); accountID != "" {
			req.Header.Set("chatgpt-account-id", accountID)
		}
	}
	return req, data, endpoint, nil
}
