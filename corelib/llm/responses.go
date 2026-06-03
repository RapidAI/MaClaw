package llm

// Responses API request builder.
// Parallels the Chat Completions builder in client.go for the
// OpenAI Responses API (POST /v1/responses).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

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
	endpoint = BuildResponsesEndpoint(cfg.URL)

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
	if cfg.Key != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Key)
	}
	// Codex subscription headers for chatgpt.com/backend-api
	if IsCodexSubscriptionEndpoint(cfg.URL) {
		req.Header.Set("originator", "codex_cli_rs")
		req.Header.Set("OpenAI-Beta", "responses=experimental")
		if accountID, _ := oauth.ExtractAccountIDFromJWT(cfg.Key); accountID != "" {
			req.Header.Set("chatgpt-account-id", accountID)
		}
	}
	return req, data, endpoint, nil
}

// DoResponsesAPIRequest sends a non-streaming OpenAI Responses API request and
// converts the response into the shared chat-style Response type used by the
// agent loop. Streaming Responses support can be added behind this function
// without changing agent callers.
func DoResponsesAPIRequest(
	ctx context.Context,
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	tools []map[string]interface{},
	client *http.Client,
) (*Response, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		req, reqBody, endpoint, err := NewResponsesAPIRequest(ctx, cfg, messages, ResponsesAPIRequestOptions{
			Stream: false,
			Tools:  tools,
		})
		if err != nil {
			return nil, err
		}
		if attempt == 0 {
			log.Printf("[LLM-responses] POST %s model=%s protocol=%s tools=%d body_bytes=%d", endpoint, cfg.Model, cfg.Protocol, len(tools), len(reqBody))
		} else {
			log.Printf("[LLM-responses] retry %d POST %s model=%s protocol=%s tools=%d body_bytes=%d", attempt, endpoint, cfg.Model, cfg.Protocol, len(tools), len(reqBody))
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("[%s] %w", endpoint, err)
			if !sleepResponsesRetry(ctx, attempt) {
				return nil, lastErr
			}
			continue
		}

		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return ParseNonStreamResponsesAPIBody(body)
		}
		msg := string(body)
		if len(msg) > 512 {
			msg = msg[:512] + "..."
		}
		lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, msg)
		if !isResponsesRetryableStatus(resp.StatusCode) || attempt == 2 || !sleepResponsesRetry(ctx, attempt) {
			return nil, lastErr
		}
	}
	return nil, lastErr
}

func isResponsesRetryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

func sleepResponsesRetry(ctx context.Context, attempt int) bool {
	if attempt >= 2 {
		return false
	}
	delay := time.Duration(attempt+1) * 750 * time.Millisecond
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
