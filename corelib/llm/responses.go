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
	Stream                  bool
	Tools                   []map[string]interface{}
	ExplicitToolReplacement bool
	ExtraBody               map[string]interface{}
	// ToolChoice and ParallelToolCalls are the request owner's explicit
	// callable-surface controls. They intentionally cannot be supplied via
	// ExtraBody, whose reserved-key filter would otherwise make their authority
	// depend on mutable map composition.
	ToolChoice        interface{}
	ParallelToolCalls *bool
	// PreserveResponseFormat keeps a host control-plane response contract when
	// a conservative OpenAI-compatible relay sanitizes ordinary chat fields.
	// The caller owns the protocol-failure path, so silently dropping the
	// contract would incorrectly turn a machine-readable request into prose.
	PreserveResponseFormat bool
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

// isOpenAIResponsesEndpoint reports whether the URL targets OpenAI's own
// Responses API (platform endpoint or Codex subscription backend).
func isOpenAIResponsesEndpoint(rawURL string) bool {
	u := strings.ToLower(strings.TrimSpace(rawURL))
	return strings.Contains(u, "api.openai.com") || IsCodexSubscriptionEndpoint(u)
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
	toolsInput := PrepareOpenAIChatToolsForWire(cfg, opts.Tools)
	tools := ConvertToResponsesTools(toolsInput)
	if len(tools) > 0 || opts.ExplicitToolReplacement {
		// An explicit replacement must remain [] after conversion, never nil
		// (which JSON encodes as null) or omitted.
		if tools == nil {
			tools = make([]map[string]interface{}, 0)
		}
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
			if format := responsesTextFormatFromChatResponseFormat(cfg, opts.ExtraBody["response_format"], opts.PreserveResponseFormat); format != nil {
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
	if opts.ToolChoice != nil {
		if toolChoice := sanitizeResponsesToolChoice(opts.ToolChoice); toolChoice != nil {
			reqBody["tool_choice"] = toolChoice
		}
	}
	if opts.ParallelToolCalls != nil {
		reqBody["parallel_tool_calls"] = *opts.ParallelToolCalls
	}
	// Explicit user choices are translated to each provider's native shape;
	// auto preserves the endpoint's own default.
	corelib.ApplyReasoningControls(cfg, reqBody, corelib.ReasoningAPIResponses)
	// Ensure max_output_tokens is set for Responses API (analogous to ensureMaxOutputTokens for Chat API).
	// Skip for Codex subscription endpoints (chatgpt.com/backend-api) — they don't support this parameter;
	// output length is controlled server-side.
	if IsCodexSubscriptionEndpoint(cfg.URL) {
		// Codex endpoints reject max_output_tokens entirely. Remove it even if
		// ExtraBody or prior conversion logic injected it.
		delete(reqBody, "max_output_tokens")
	} else if _, ok := reqBody["max_output_tokens"]; !ok {
		// A cached value of 0 means the endpoint rejects the parameter entirely — skip injection.
		shouldInject := true
		limit := cfg.EffectiveMaxOutputTokens()
		cacheKey := strings.ToLower(strings.TrimSpace(cfg.Model))
		if cached, ok := maxOutputTokensCache.Load(cacheKey); ok {
			if cachedLimit, isInt := cached.(int); isInt {
				if cachedLimit == 0 {
					shouldInject = false
				} else if cachedLimit < limit {
					limit = cachedLimit
				}
			}
		}
		if shouldInject {
			reqBody["max_output_tokens"] = limit
		}
	}
	if cfg.NeedsConservativeOpenAICompatSanitization() {
		corelib.SanitizeCodeGenOpenAICompatBody(reqBody)
		if opts.PreserveResponseFormat && opts.ExtraBody != nil {
			if format := responsesTextFormatFromChatResponseFormat(cfg, opts.ExtraBody["response_format"], true); format != nil {
				reqBody["text"] = map[string]interface{}{"format": format}
			}
		}
		if opts.ToolChoice != nil {
			if toolChoice := sanitizeResponsesToolChoice(opts.ToolChoice); toolChoice != nil {
				reqBody["tool_choice"] = toolChoice
			}
		}
		if opts.ParallelToolCalls != nil {
			reqBody["parallel_tool_calls"] = *opts.ParallelToolCalls
		}
	}

	body, err = json.Marshal(reqBody)
	return endpoint, body, err
}

func sanitizeResponsesToolChoice(raw interface{}) interface{} {
	switch value := raw.(type) {
	case string:
		switch strings.TrimSpace(value) {
		case "none", "auto", "required":
			return strings.TrimSpace(value)
		default:
			return nil
		}
	default:
		choice := toStringInterfaceMap(raw)
		if choice == nil || strings.TrimSpace(stringValue(choice["type"])) != "function" {
			return nil
		}
		name := strings.TrimSpace(stringValue(choice["name"]))
		if name == "" {
			return nil
		}
		return map[string]interface{}{"type": "function", "name": name}
	}
}

func responsesTextFormatFromChatResponseFormat(cfg corelib.MaclawLLMConfig, raw interface{}, preserveContract bool) map[string]interface{} {
	// Keep this conversion aligned with Chat Completions.  A provider may expose
	// a Responses-shaped endpoint while supporting only JSON-object mode; sending
	// the stricter schema in that case turns a valid control-plane request into a
	// transport rejection.  This is provider capability normalization, never an
	// intent/text-based routing exception.
	format := sanitizeOpenAIResponseFormatForProvider(cfg, raw, preserveContract)
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
	ApplyProviderAuthHeaders(req, cfg)
	ApplyWorkloadHintHeaders(req, cfg)
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
