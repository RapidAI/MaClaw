package llmservice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	corellm "github.com/RapidAI/CodeClaw/corelib/llm"
)

// MemberProbeBudget is the output budget and stream flag for one member probe.
// Nil token pointers keep the short probe default. OutputCap is the saved
// member limit. Stream asks the protocol's streaming wire.
type MemberProbeBudget struct {
	MaxTokens           *int64
	MaxCompletionTokens *int64
	OutputCap           int64
	Stream              bool
}

// NewMemberProbeRequest builds the upstream request a local member probe sends.
// The returned bytes are that request body, before a vendor RoundTrip rewrites
// it. OpenCode is handled by the caller; this builder is the native protocol.
func NewMemberProbeRequest(ctx context.Context, cfg corelib.MaclawLLMConfig, tools, toolChoice json.RawMessage, budget MemberProbeBudget) (*http.Request, []byte, error) {
	prompt := "Reply with exactly: pong"
	toolsSet := len(bytes.TrimSpace(tools)) > 0 && string(bytes.TrimSpace(tools)) != "null"
	maxTokens := 64
	if toolsSet {
		prompt = "Call one of the available tools now."
		maxTokens = 256
	}
	output := MemberProbeOutputBody(budget.OutputCap, budget.MaxTokens, budget.MaxCompletionTokens, toolsSet)
	// Match the live single-field converters. Anthropic reads max_tokens and
	// falls back to max_completion_tokens. Responses and Google read
	// max_completion_tokens and fall back to max_tokens.
	order := []string{"max_completion_tokens", "max_tokens"}
	if strings.EqualFold(cfg.Protocol, "anthropic") {
		order = []string{"max_tokens", "max_completion_tokens"}
	}
	maxTokens = MemberProbeFieldBudget(output, maxTokens, order...)
	// The builders treat option 0 as "omitted, use the model default". A caller
	// who sent 0 chose that budget. Leaving the flag unset raises it to 65536
	// and the member cap then clamps the default instead of the caller's zero.
	keepZero := maxTokens == 0
	messages := []interface{}{map[string]string{"role": "user", "content": prompt}}
	parsedTools, toolChoiceValue := memberProbeToolValues(tools, toolChoice)
	if cfg.IsGoogleProtocol() {
		endpoint, body, err := corellm.BuildGoogleGenerateContentRequestData(cfg, messages, corellm.GoogleGenerateRequestOptions{
			Stream:                  budget.Stream,
			Tools:                   parsedTools,
			ToolChoice:              toolChoiceValue,
			ExplicitToolReplacement: len(parsedTools) > 0,
			MaxOutputTokens:         maxTokens,
			KeepZeroMaxOutputTokens: keepZero,
		})
		if err != nil {
			return nil, nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if key := strings.TrimSpace(cfg.Key); key != "" {
			req.Header.Set("x-goog-api-key", key)
		}
		if ua := cfg.UserAgent(); ua != "" {
			req.Header.Set("User-Agent", ua)
		}
		return finishMemberProbeRequest(req, body, nil, budget.Stream)
	}
	if cfg.Protocol == "anthropic" {
		endpoint, body, err := corellm.BuildAnthropicMessagesRequestData(cfg, messages, corellm.AnthropicMessagesRequestOptions{
			Stream:            budget.Stream,
			MaxTokens:         maxTokens,
			KeepZeroMaxTokens: keepZero,
		})
		if err != nil {
			return nil, nil, err
		}
		body = InjectMemberProbeTools(body, parsedTools, toolChoiceValue, true, cfg.MemberDropFields)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if cfg.Key != "" {
			req.Header.Set("x-api-key", cfg.Key)
		}
		req.Header.Set("anthropic-version", "2023-06-01")
		if ua := cfg.UserAgent(); ua != "" {
			req.Header.Set("User-Agent", ua)
		}
		corelib.SetCodeGenClientNameHeaderIfNeededWithName(req, cfg.UserAgent())
		corelib.ApplyOpenCodeSessionHeader(req, cfg)
		return finishMemberProbeRequest(req, body, nil, budget.Stream)
	}
	if cfg.IsResponsesAPI() {
		req, body, _, err := corellm.NewResponsesAPIRequest(ctx, cfg, messages, corellm.ResponsesAPIRequestOptions{
			Stream:     budget.Stream,
			Tools:      parsedTools,
			ToolChoice: toolChoiceValue,
			ExtraBody:  map[string]interface{}{"max_output_tokens": maxTokens},
		})
		return finishMemberProbeRequest(req, body, err, budget.Stream)
	}
	extra := map[string]interface{}{}
	for key, value := range output {
		extra[key] = value
	}
	// Kimi Code and K2/K3 reject every explicit temperature. The probe used to
	// send 0 and the status check failed with HTTP 400 before the model ran.
	if !corelib.KimiRequestOmitsSampling(cfg, cfg.Model) {
		extra["temperature"] = 0
	}
	req, body, _, err := corellm.NewOpenAIChatRequest(ctx, cfg, messages, corellm.OpenAIChatRequestOptions{
		Stream:     budget.Stream,
		Tools:      parsedTools,
		ToolChoice: toolChoiceValue,
		ExtraBody:  extra,
	})
	return finishMemberProbeRequest(req, body, err, budget.Stream)
}

// finishMemberProbeRequest asks a streaming probe for SSE. Gateways that
// answer with one JSON completion unless Accept is text/event-stream would
// otherwise fail the end-marker check. A non-stream probe keeps its headers.
func finishMemberProbeRequest(req *http.Request, body []byte, err error, stream bool) (*http.Request, []byte, error) {
	if err != nil || req == nil || !stream {
		return req, body, err
	}
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	req.Header.Set("Accept", "text/event-stream")
	return req, body, nil
}

func memberProbeToolValues(tools, toolChoice json.RawMessage) ([]map[string]interface{}, interface{}) {
	tools = bytes.TrimSpace(tools)
	if len(tools) == 0 || string(tools) == "null" {
		return nil, nil
	}
	var parsed []map[string]interface{}
	if err := json.Unmarshal(tools, &parsed); err != nil || len(parsed) == 0 {
		return nil, nil
	}
	toolChoice = bytes.TrimSpace(toolChoice)
	if len(toolChoice) == 0 || string(toolChoice) == "null" {
		return parsed, nil
	}
	var choice interface{}
	if err := json.Unmarshal(toolChoice, &choice); err != nil {
		return parsed, nil
	}
	return parsed, choice
}

// InjectMemberProbeTools adds the probe's tools unless the member dropped them.
func InjectMemberProbeTools(body []byte, tools []map[string]interface{}, toolChoice interface{}, anthropic bool, drop []string) []byte {
	if len(tools) == 0 {
		return body
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(body, &doc); err != nil {
		return body
	}
	changed := false
	if anthropic {
		converted := make([]map[string]interface{}, 0, len(tools))
		for _, tool := range tools {
			fn, _ := tool["function"].(map[string]interface{})
			if fn == nil {
				converted = append(converted, tool)
				continue
			}
			item := map[string]interface{}{"name": fn["name"]}
			if desc, ok := fn["description"]; ok {
				item["description"] = desc
			}
			if schema, ok := fn["parameters"]; ok {
				item["input_schema"] = schema
			} else {
				item["input_schema"] = map[string]interface{}{"type": "object"}
			}
			converted = append(converted, item)
		}
		if !memberProbeDropListed(drop, "tools") {
			doc["tools"] = converted
			changed = true
		}
		if changed && !memberProbeDropListed(drop, "tool_choice") {
			if choice := anthropicProbeToolChoice(toolChoice); choice != nil {
				doc["tool_choice"] = choice
			}
		}
	} else if !memberProbeDropListed(drop, "tools") {
		doc["tools"] = tools
		changed = true
		if toolChoice != nil && !memberProbeDropListed(drop, "tool_choice") {
			doc["tool_choice"] = toolChoice
		}
	}
	if !changed {
		return body
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return body
	}
	return out
}

func memberProbeDropListed(drop []string, field string) bool {
	field = strings.TrimSpace(field)
	if field == "" {
		return false
	}
	for _, item := range drop {
		if strings.TrimSpace(item) == field {
			return true
		}
	}
	return false
}

func anthropicProbeToolChoice(choice interface{}) interface{} {
	switch value := choice.(type) {
	case string:
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "", "none":
			return nil
		case "auto":
			return map[string]interface{}{"type": "auto"}
		case "required", "any":
			return map[string]interface{}{"type": "any"}
		default:
			name := strings.TrimSpace(value)
			if name == "" {
				return nil
			}
			return map[string]interface{}{"type": "tool", "name": name}
		}
	case map[string]interface{}:
		if fn, ok := value["function"].(map[string]interface{}); ok {
			if name, _ := fn["name"].(string); strings.TrimSpace(name) != "" {
				return map[string]interface{}{"type": "tool", "name": strings.TrimSpace(name)}
			}
		}
		switch strings.ToLower(strings.TrimSpace(fmt.Sprint(value["type"]))) {
		case "auto":
			return map[string]interface{}{"type": "auto"}
		case "any", "required":
			return map[string]interface{}{"type": "any"}
		case "tool":
			name, _ := value["name"].(string)
			if strings.TrimSpace(name) == "" {
				return nil
			}
			return map[string]interface{}{"type": "tool", "name": strings.TrimSpace(name)}
		}
	}
	return nil
}
