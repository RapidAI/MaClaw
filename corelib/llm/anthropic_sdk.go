package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	anthropic "github.com/anthropics/anthropic-sdk-go"
	anthropicopt "github.com/anthropics/anthropic-sdk-go/option"
)

type AnthropicModelListItem struct {
	ID          string
	DisplayName string
}

func ListAnthropicModelsWithSDK(ctx context.Context, cfg corelib.MaclawLLMConfig, client *http.Client) ([]AnthropicModelListItem, error) {
	if client == nil {
		client = http.DefaultClient
	}
	anthropicClient := anthropic.NewClient(anthropicSDKOptions(cfg, client)...)
	page, err := anthropicClient.Models.List(ctx, anthropic.ModelListParams{})
	if err != nil {
		if status, raw := anthropicSDKErrorStatusAndRaw(err); raw != "" {
			return nil, fmt.Errorf("HTTP %d: body_len=%d: %w", status, len(raw), err)
		}
		return nil, err
	}
	items := make([]AnthropicModelListItem, 0, len(page.Data))
	for _, model := range page.Data {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		items = append(items, AnthropicModelListItem{
			ID:          id,
			DisplayName: strings.TrimSpace(model.DisplayName),
		})
	}
	return items, nil
}

func anthropicSDKMessage(ctx context.Context, cfg corelib.MaclawLLMConfig, body []byte, client *http.Client) (*Response, int, []byte, error) {
	if client == nil {
		client = http.DefaultClient
	}
	body = anthropicSDKBodyWithoutStream(body)
	var response *http.Response
	anthropicClient := anthropic.NewClient(anthropicSDKOptions(cfg, client)...)
	msg, err := anthropicClient.Messages.New(
		ctx,
		anthropic.MessageNewParams{},
		anthropicopt.WithRequestBody("application/json", body),
		anthropicopt.WithResponseInto(&response),
	)
	status := http.StatusOK
	if response != nil {
		status = response.StatusCode
	}
	if err != nil {
		if apiStatus, raw := anthropicSDKErrorStatusAndRaw(err); raw != "" {
			if apiStatus > 0 {
				status = apiStatus
			}
			return nil, status, []byte(raw), err
		}
		return nil, status, nil, err
	}
	var responseBody []byte
	if msg != nil && strings.TrimSpace(msg.RawJSON()) != "" {
		responseBody = []byte(msg.RawJSON())
	}
	resp, parseErr := parseAnthropicResponseBody(responseBody)
	return resp, status, responseBody, parseErr
}

func anthropicSDKMessageStream(ctx context.Context, cfg corelib.MaclawLLMConfig, body []byte, client *http.Client, onToken TokenCallback) (*Response, int, []byte, error) {
	if client == nil {
		client = http.DefaultClient
	}
	body = anthropicSDKBodyWithoutStream(body)
	capture := &openAISDKStreamCapture{limit: 512 * 1024}
	streamClient := openAISDKClientWithCapture(client, capture)
	anthropicClient := anthropic.NewClient(anthropicSDKOptions(cfg, streamClient)...)
	stream := anthropicClient.Messages.NewStreaming(ctx, anthropic.MessageNewParams{}, anthropicopt.WithRequestBody("application/json", body))

	var rawSSE strings.Builder
	contentFilter := newContentToolCallDeltaFilter(onToken)
	for stream.Next() {
		event := stream.Current()
		raw := event.RawJSON()
		if strings.TrimSpace(raw) == "" {
			continue
		}
		rawSSE.WriteString("data: ")
		rawSSE.WriteString(raw)
		rawSSE.WriteString("\n\n")
		emitAnthropicSDKTextDelta(raw, contentFilter.Write)
	}
	if err := stream.Err(); err != nil {
		body := capture.body()
		if len(body) == 0 {
			if raw := anthropicSDKRawJSON(err); raw != "" {
				body = []byte(raw)
			}
		}
		return nil, capture.statusCode(), body, err
	}
	contentFilter.Flush()
	if raw := rawSSE.String(); strings.TrimSpace(raw) != "" {
		resp, err := parseAnthropicSSEStream(strings.NewReader(raw), nil)
		return resp, capture.statusCode(), nil, err
	}
	if raw := capture.body(); len(raw) > 0 && json.Valid(raw) {
		resp, err := parseAnthropicResponseBody(raw)
		if err == nil && onToken != nil && len(resp.Choices) > 0 && resp.Choices[0].Message.Content != "" {
			onToken(resp.Choices[0].Message.Content)
		}
		return resp, capture.statusCode(), raw, err
	}
	return &Response{Choices: []Choice{{Message: Message{Role: "assistant"}, FinishReason: "stop"}}}, capture.statusCode(), nil, nil
}

func anthropicSDKBodyWithoutStream(body []byte) []byte {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	delete(payload, "stream")
	if patched, err := json.Marshal(payload); err == nil {
		return patched
	}
	return body
}

func emitAnthropicSDKTextDelta(raw string, onToken TokenCallback) {
	if onToken == nil {
		return
	}
	var event struct {
		Type  string `json:"type"`
		Delta struct {
			Type string `json:"type,omitempty"`
			Text string `json:"text,omitempty"`
		} `json:"delta,omitempty"`
	}
	if json.Unmarshal([]byte(raw), &event) == nil && event.Type == "content_block_delta" && event.Delta.Type == "text_delta" && event.Delta.Text != "" {
		onToken(event.Delta.Text)
	}
}

func anthropicSDKOptions(cfg corelib.MaclawLLMConfig, client *http.Client) []anthropicopt.RequestOption {
	opts := []anthropicopt.RequestOption{
		anthropicopt.WithBaseURL(anthropicSDKBaseURL(cfg.URL)),
		anthropicopt.WithAPIKey(cfg.Key),
		anthropicopt.WithAuthToken(cfg.Key),
		anthropicopt.WithHTTPClient(client),
		anthropicopt.WithHeader("User-Agent", cfg.UserAgent()),
		anthropicopt.WithMaxRetries(0),
	}
	if corelib.IsCodeGenURL(cfg.URL) {
		opts = append(opts, anthropicopt.WithHeader(corelib.CodeGenClientNameHeader, corelib.NormalizeCodeGenClientName(cfg.UserAgent())))
	}
	if timeout := cfg.EffectiveTimeoutSec(); timeout > 0 {
		opts = append(opts, anthropicopt.WithRequestTimeout(time.Duration(timeout)*time.Second))
	}
	return opts
}

func anthropicSDKBaseURL(raw string) string {
	return corelib.AnthropicBaseURL(raw)
}

func anthropicSDKRawJSON(err error) string {
	type rawJSONError interface {
		RawJSON() string
	}
	if err == nil {
		return ""
	}
	if raw, ok := err.(rawJSONError); ok {
		return raw.RawJSON()
	}
	return ""
}

func anthropicSDKErrorStatusAndRaw(err error) (int, string) {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode, strings.TrimSpace(apiErr.RawJSON())
	}
	return 0, strings.TrimSpace(anthropicSDKRawJSON(err))
}
