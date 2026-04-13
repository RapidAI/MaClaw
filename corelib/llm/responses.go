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
	"model":        true,
	"input":        true,
	"instructions": true,
	"stream":       true,
	"tools":        true,
}

// BuildResponsesAPIRequestData constructs the endpoint URL and JSON body
// for a Responses API request.
func BuildResponsesAPIRequestData(
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	opts ResponsesAPIRequestOptions,
) (endpoint string, body []byte, err error) {
	endpoint = strings.TrimRight(cfg.URL, "/")
	// For chatgpt.com/backend-api (Codex subscription), use /codex/responses
	// For api.openai.com/v1 (API key), use /responses
	if strings.Contains(endpoint, "chatgpt.com") {
		if !strings.HasSuffix(endpoint, "/codex/responses") {
			endpoint = strings.TrimSuffix(endpoint, "/codex")
			endpoint += "/codex/responses"
		}
	} else {
		endpoint += "/responses"
	}

	converted := ConvertToResponsesInput(messages)

	reqBody := map[string]interface{}{
		"model":  cfg.Model,
		"input":  converted.Input,
		"stream": opts.Stream,
	}
	if converted.Instructions != "" {
		reqBody["instructions"] = converted.Instructions
	}
	if tools := ConvertToResponsesTools(opts.Tools); len(tools) > 0 {
		reqBody["tools"] = tools
	}
	for k, v := range opts.ExtraBody {
		if responsesReservedKeys[k] {
			continue
		}
		reqBody[k] = v
	}

	body, err = json.Marshal(reqBody)
	return endpoint, body, err
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
	// Codex subscription headers for chatgpt.com/backend-api
	if strings.Contains(cfg.URL, "chatgpt.com") {
		req.Header.Set("OpenAI-Beta", "responses=experimental")
		if accountID, _ := oauth.ExtractAccountIDFromJWT(cfg.Key); accountID != "" {
			req.Header.Set("chatgpt-account-id", accountID)
		}
	}
	return req, data, endpoint, nil
}
