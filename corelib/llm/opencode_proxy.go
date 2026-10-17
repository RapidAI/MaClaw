package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

var errOpenCodeEmptyResponse = fmt.Errorf("opencode returned an empty response")

// PrepareOpenCodeChatBody rewrites one OpenCode provider request.
// A stored key is a paid account and is forwarded with that key. An empty key
// is the anonymous free tier. The model id picks Zen or the Go gateway.
// Responses-only models are marked on the returned config and leave the chat
// body unchanged so the Responses builder can read the original messages.
func PrepareOpenCodeChatBody(cfg corelib.MaclawLLMConfig, body map[string]interface{}) (corelib.MaclawLLMConfig, bool) {
	if body == nil || !corelib.IsOpenCodeURL(cfg.URL) {
		return cfg, false
	}
	model, _ := body["model"].(string)
	model = strings.TrimSpace(model)
	if model == "" {
		model = cfg.UpstreamModel()
		if model != "" {
			body["model"] = model
		}
	}
	cfg.Model = model
	cfg.URL = corelib.OpenCodeUpstreamBase(cfg.URL, model)
	if corelib.OpenCodeUsesResponsesAPI(cfg.URL, model) {
		cfg.WireAPI = "responses"
		return cfg, true
	}
	applyOpenCodeZenFreeChat(cfg, body)
	return cfg, true
}

// ForwardOpenCodeChat sends a prepared OpenCode chat body and returns an
// OpenAI chat JSON payload. Zen free models stream upstream; that stream is
// collected into one JSON response.
func ForwardOpenCodeChat(ctx context.Context, cfg corelib.MaclawLLMConfig, body map[string]interface{}, client *http.Client) ([]byte, int, error) {
	cfg, ok := PrepareOpenCodeChatBody(cfg, body)
	if !ok {
		return nil, 0, fmt.Errorf("not an opencode provider")
	}
	// The model id decides the protocol. A provider-level WireAPI of
	// "responses" must not pull a chat free model onto that API.
	model, _ := body["model"].(string)
	if corelib.OpenCodeUsesResponsesAPI(cfg.URL, model) {
		return forwardOpenCodeResponses(ctx, cfg, body, client)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, 0, err
	}
	resp, status, errBody, err := openAIHTTPChatStream(ctx, cfg, raw, client, nil, nil)
	if err != nil {
		// A cut stream can already contain visible text or a tool call.
		// Returning that partial result stops the caller from retrying it.
		if data, ok := openCodePartialResponse(resp); ok {
			return data, status, nil
		}
		return errBody, status, err
	}
	if resp == nil {
		return nil, status, errOpenCodeEmptyResponse
	}
	data, err := marshalOpenCodeResponse(resp)
	if err != nil {
		return nil, status, err
	}
	return data, status, nil
}

func forwardOpenCodeResponses(ctx context.Context, cfg corelib.MaclawLLMConfig, body map[string]interface{}, client *http.Client) ([]byte, int, error) {
	resp, err := DoResponsesAPIRequestStream(ctx, cfg, openCodeForwardMessages(body["messages"]), openAIChatTools(body["tools"]), client, nil, nil)
	if err != nil {
		var httpErr *HTTPStatusError
		if errors.As(err, &httpErr) && httpErr != nil {
			return httpErr.Body, httpErr.StatusCode, err
		}
		return nil, 0, err
	}
	if resp == nil {
		return nil, http.StatusOK, errOpenCodeEmptyResponse
	}
	data, err := marshalOpenCodeResponse(resp)
	if err != nil {
		return nil, http.StatusOK, err
	}
	return data, http.StatusOK, nil
}

// marshalOpenCodeResponse keeps tool arguments intact. encoding/json would
// turn "<" in those arguments into "\u003c".
func marshalOpenCodeResponse(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func openCodePartialResponse(resp *Response) ([]byte, bool) {
	if resp == nil || len(resp.Choices) == 0 {
		return nil, false
	}
	msg := resp.Choices[0].Message
	if strings.TrimSpace(msg.Content) == "" && strings.TrimSpace(msg.ReasoningContent) == "" && len(msg.ToolCalls) == 0 {
		return nil, false
	}
	data, err := marshalOpenCodeResponse(resp)
	if err != nil {
		return nil, false
	}
	return data, true
}

func openCodeForwardMessages(raw interface{}) []interface{} {
	messages, _ := raw.([]interface{})
	return messages
}
