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
	anthropicClient := anthropic.NewClient(anthropicSDKOptions(ctx, cfg, client)...)
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
	client = HTTPClientForRequestContext(ctx, client)
	body = anthropicSDKBodyWithoutStream(body)
	cfg = bindOpenCodeSessionFromJSONBody(ctx, cfg, body)
	var response *http.Response
	anthropicClient := anthropic.NewClient(anthropicSDKOptions(ctx, cfg, client)...)
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

func anthropicSDKMessageStream(ctx context.Context, cfg corelib.MaclawLLMConfig, body []byte, client *http.Client, onToken TokenCallback, onReasoning TokenCallback) (*Response, int, []byte, error) {
	client = HTTPClientForRequestContext(ctx, client)
	body = anthropicSDKBodyWithoutStream(body)
	cfg = bindOpenCodeSessionFromJSONBody(ctx, cfg, body)
	// GLM-5.3-Flash keeps emitting tokens after the route deadline. The OpenAI
	// stream detaches that deadline once SSE headers arrive. This SDK call
	// used the route context for the whole body, so a live 200 was cut at
	// 10 minutes with context.DeadlineExceeded and surfaced as 请求超时.
	// After headers the body has no wall-clock cap. Idle silence, a stop,
	// and an earlier host deadline still abort it.
	reqCtx, cancelReq, markHeaders, headerDeadline := streamingRequestContext(ctx)
	defer cancelReq()
	capture := &openAISDKStreamCapture{limit: 512 * 1024}
	streamClient := openAISDKClientWithCapture(client, capture)
	streamClient = httpClientWithStreamBodyDeadline(streamClient, markHeaders)
	anthropicClient := anthropic.NewClient(anthropicSDKOptions(reqCtx, cfg, streamClient)...)
	stream := anthropicClient.Messages.NewStreaming(reqCtx, anthropic.MessageNewParams{}, anthropicopt.WithRequestBody("application/json", body))
	defer stream.Close()

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
		emitAnthropicSDKDeltas(raw, contentFilter.Write, onReasoning)
	}
	if err := stream.Err(); err != nil {
		err = normalizeStreamSetupError(headerDeadline, err)
		contentFilter.Flush()
		body := capture.body()
		if len(body) == 0 {
			if raw := anthropicSDKRawJSON(err); raw != "" {
				body = []byte(raw)
			}
		}
		if resp := assembleAnthropicPartialSSE(rawSSE.String()); resp != nil {
			return resp, capture.statusCode(), body, err
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
		if err == nil && len(resp.Choices) > 0 {
			if onToken != nil && resp.Choices[0].Message.Content != "" {
				onToken(resp.Choices[0].Message.Content)
			}
			if onReasoning != nil && resp.Choices[0].Message.ReasoningContent != "" {
				onReasoning(resp.Choices[0].Message.ReasoningContent)
			}
		}
		return resp, capture.statusCode(), raw, err
	}
	return &Response{Choices: []Choice{{Message: Message{Role: "assistant"}, FinishReason: "stop"}}}, capture.statusCode(), nil, nil
}

func assembleAnthropicPartialSSE(raw string) *Response {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	// Keep a visible prefix even when parse also returns a read error.
	resp, _ := parseAnthropicSSEStream(strings.NewReader(raw), nil)
	if !anthropicResponseHasVisibleDelta(resp) {
		return nil
	}
	return resp
}

func anthropicResponseHasVisibleDelta(resp *Response) bool {
	if resp == nil || len(resp.Choices) == 0 {
		return false
	}
	msg := resp.Choices[0].Message
	return strings.TrimSpace(msg.Content) != "" ||
		strings.TrimSpace(msg.ReasoningContent) != "" ||
		len(msg.ToolCalls) > 0
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

func emitAnthropicSDKDeltas(raw string, onToken TokenCallback, onReasoning TokenCallback) {
	if onToken == nil && onReasoning == nil {
		return
	}
	var event struct {
		Type  string `json:"type"`
		Delta struct {
			Type     string `json:"type,omitempty"`
			Text     string `json:"text,omitempty"`
			Thinking string `json:"thinking,omitempty"`
		} `json:"delta,omitempty"`
		ContentBlock *struct {
			Type     string `json:"type"`
			Text     string `json:"text,omitempty"`
			Thinking string `json:"thinking,omitempty"`
		} `json:"content_block,omitempty"`
	}
	if json.Unmarshal([]byte(raw), &event) != nil {
		return
	}
	if event.Type == "content_block_start" && event.ContentBlock != nil {
		appendAnthropicThinking(nil, onReasoning, anthropicThinkingBlockText(event.ContentBlock.Type, event.ContentBlock.Thinking, event.ContentBlock.Text))
		return
	}
	if event.Type != "content_block_delta" {
		return
	}
	if onToken != nil && event.Delta.Type == "text_delta" && event.Delta.Text != "" {
		onToken(event.Delta.Text)
	}
	appendAnthropicThinking(nil, onReasoning, anthropicDeltaThinkingText(event.Delta.Type, event.Delta.Thinking, event.Delta.Text))
}

// httpClientWithStreamBodyDeadline lets a live Anthropic SSE body outlive the
// route deadline. markHeaders is called once response headers are in. A 200
// that loses that claim was already canceled by the route timer. Silence on
// the body is still cut by SSEIdleTimeout. Non-200 bodies stay on the route
// deadline because they are not token streams.
func httpClientWithStreamBodyDeadline(client *http.Client, markHeaders func() bool) *http.Client {
	if client == nil {
		client = http.DefaultClient
	}
	clone := *client
	clone.Transport = streamBodyDeadlineTransport{base: client.Transport, markHeaders: markHeaders}
	return &clone
}

type streamBodyDeadlineTransport struct {
	base        http.RoundTripper
	markHeaders func() bool
}

func (t streamBodyDeadlineTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	if err != nil || resp == nil || resp.Body == nil || resp.StatusCode != http.StatusOK {
		return resp, err
	}
	if t.markHeaders != nil && !t.markHeaders() {
		_ = resp.Body.Close()
		return nil, context.DeadlineExceeded
	}
	resp.Body = newIdleTimeoutReader(resp.Body, SSEIdleTimeout)
	return resp, nil
}

func anthropicSDKOptions(ctx context.Context, cfg corelib.MaclawLLMConfig, client *http.Client) []anthropicopt.RequestOption {
	cfg = bindOpenCodeSession(ctx, cfg, nil)
	opts := []anthropicopt.RequestOption{
		anthropicopt.WithBaseURL(anthropicSDKBaseURL(cfg.URL)),
		anthropicopt.WithAPIKey(cfg.Key),
		anthropicopt.WithAuthToken(cfg.Key),
		anthropicopt.WithHTTPClient(client),
		anthropicopt.WithHeader("User-Agent", corelib.OpenCodeWireUserAgent(cfg.URL, cfg.UserAgent())),
		anthropicopt.WithMaxRetries(0),
	}
	if corelib.IsCodeGenURL(cfg.URL) {
		opts = append(opts, anthropicopt.WithHeader(corelib.CodeGenClientNameHeader, corelib.NormalizeCodeGenClientName(cfg.UserAgent())))
	}
	if name, value, ok := corelib.OpenCodeSessionHeaderForConfig(cfg); ok {
		opts = append(opts, anthropicopt.WithHeader(name, value))
	}
	for key, value := range WorkloadHintHeaderValues(cfg) {
		opts = append(opts, anthropicopt.WithHeader(key, value))
	}
	// A caller deadline (caption's 6s callCtx, agent-loop timeout) already
	// bounds the request. Layering WithRequestTimeout (floored at 240s) races
	// context cancel the same way http.Client.Timeout did. A live SSE body
	// drops that deadline after headers and keeps the owned route mark.
	// WithRequestTimeout there would put the short SDK timer back on a
	// stream that runs until the model finishes, the caller stops, or the
	// connection goes idle.
	if ctx != nil {
		if _, ok := ctx.Deadline(); ok {
			return opts
		}
		if _, owned := OwnedRouteDeadline(ctx); owned {
			return opts
		}
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
