package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	openai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

func openAISDKChatRaw(ctx context.Context, cfg corelib.MaclawLLMConfig, body []byte, client *http.Client) ([]byte, int, error) {
	client = HTTPClientForRequestContext(ctx, client)
	if !json.Valid(body) {
		return nil, 0, fmt.Errorf("parse openai request body: invalid JSON")
	}
	var responseBody []byte
	var response *http.Response
	// The SDK's raw-body adapter is inside this request owner boundary. Keep the
	// client's redirect policy intact for ordinary callers, while the adapter
	// itself only prevents transport-level body replay after it replaces the
	// exact JSON payload.
	rawClient := openAISDKClientWithRawBody(client, body, nil)
	openaiClient := openai.NewClient(openAISDKOptions(cfg, rawClient)...)
	var err error
	_, err = openaiClient.Chat.Completions.New(
		ctx,
		openai.ChatCompletionNewParams{},
		option.WithResponseBodyInto(&responseBody),
		option.WithResponseInto(&response),
	)
	status := http.StatusOK
	if response != nil {
		status = response.StatusCode
	}
	if err != nil {
		if apiErr := openAISDKError(err); apiErr != nil {
			status = apiErr.StatusCode
			if len(responseBody) == 0 && strings.TrimSpace(apiErr.RawJSON()) != "" {
				responseBody = []byte(apiErr.RawJSON())
			}
		}
		if len(responseBody) == 0 {
			if raw := extractJSONObjectFromText(err.Error()); len(raw) > 0 {
				responseBody = raw
			}
		}
		return responseBody, status, err
	}
	return responseBody, status, nil
}

func openAISDKChatStream(ctx context.Context, cfg corelib.MaclawLLMConfig, body []byte, client *http.Client, onToken TokenCallback, onReasoning TokenCallback) (*Response, int, []byte, error) {
	client = HTTPClientForRequestContext(ctx, client)
	if !json.Valid(body) {
		return nil, 0, nil, fmt.Errorf("parse openai stream request body: invalid JSON")
	}
	return openAIHTTPChatStream(ctx, cfg, body, client, onToken, onReasoning)
}

func openAIHTTPChatStream(ctx context.Context, cfg corelib.MaclawLLMConfig, body []byte, client *http.Client, onToken TokenCallback, onReasoning TokenCallback) (*Response, int, []byte, error) {
	client = HTTPClientForRequestContext(ctx, client)
	endpoint := BuildOpenAIChatCompletionsEndpoint(corelib.NormalizeGLMCodingPlanOpenAIBaseURL(cfg.URL, cfg.UserAgent()))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", cfg.UserAgent())
	req.Header.Set("Cache-Control", "no-cache")
	if cfg.Key != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Key)
	}
	ApplyProviderAuthHeaders(req, cfg)
	ApplyWorkloadHintHeaders(req, cfg)
	corelib.SetCodeGenClientNameHeaderIfNeededWithName(req, cfg.UserAgent())

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, nil, err
	}
	defer resp.Body.Close()
	status := resp.StatusCode
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if status != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
		// Preserve the bounded response body for callers that can classify
		// provider-specific errors (for example, hub entitlement failures).
		// HTTPStatusError.Error intentionally exposes only status and length.
		return nil, status, raw, newHTTPStatusError(status, raw)
	}
	if !strings.Contains(contentType, "text/event-stream") {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
		if json.Valid(raw) {
			result, err := ParseNonStreamOpenAIResponseBody(raw)
			if err != nil {
				return nil, status, raw, err
			}
			if len(result.Choices) > 0 {
				msg := result.Choices[0].Message
				if onToken != nil && msg.Content != "" {
					onToken(msg.Content)
				}
				if onReasoning != nil && msg.ReasoningContent != "" {
					onReasoning(msg.ReasoningContent)
				}
			}
			return result, status, nil, nil
		}
		return nil, status, raw, fmt.Errorf("parse openai stream response: expected SSE event stream or JSON body (body_len=%d)", len(raw))
	}
	result, err := parseSSEStreamWithReasoning(resp.Body, onToken, onReasoning)
	if err != nil {
		// parseSSEStreamWithReasoning retains a safe partial response when the
		// transport breaks after SSE deltas. Propagate it so callers do not
		// retry and duplicate visible output or tool calls.
		return result, status, nil, err
	}
	return result, status, nil, nil
}

func openAISDKChatStreamUnused(ctx context.Context, cfg corelib.MaclawLLMConfig, body []byte, client *http.Client, onToken TokenCallback, onReasoning TokenCallback) (*Response, int, []byte, error) {
	// This compatibility implementation is currently dormant, but it still
	// constructs a real SDK client. Preserve the owner-scoped redirect/replay
	// policy before wrapping that client with the raw-body adapter, so enabling
	// this path cannot reintroduce a hidden successor request.
	client = HTTPClientForRequestContext(ctx, client)
	capture := &openAISDKStreamCapture{limit: 512 * 1024}
	streamClient := openAISDKClientWithRawBody(client, body, capture)
	openaiClient := openai.NewClient(openAISDKOptions(cfg, streamClient)...)
	stream := openaiClient.Chat.Completions.NewStreaming(ctx, openai.ChatCompletionNewParams{})

	var contentBuf, reasoningBuf strings.Builder
	var finishReason string
	var usage *Usage
	contentFilter := newContentToolCallDeltaFilter(onToken)

	type toolCallAcc struct {
		ID      string
		Type    string
		Name    string
		ArgsBuf strings.Builder
	}
	toolCalls := map[int]*toolCallAcc{}
	legacyFunctionCall := &toolCallAcc{Type: "function"}
	legacyFunctionCallSeen := false

	for stream.Next() {
		chunk := stream.Current()
		shouldStopAfterChunk := false
		if chunk.Usage.TotalTokens > 0 || chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 {
			usage = &Usage{
				PromptTokens:     int(chunk.Usage.PromptTokens),
				CompletionTokens: int(chunk.Usage.CompletionTokens),
				TotalTokens:      int(chunk.Usage.TotalTokens),
				InputTokens:      int(chunk.Usage.PromptTokens),
				OutputTokens:     int(chunk.Usage.CompletionTokens),
			}
		} else if parsed := openAISDKChunkUsage(chunk.RawJSON()); parsed != nil {
			usage = parsed
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		delta := choice.Delta
		if delta.Content != "" {
			contentBuf.WriteString(delta.Content)
			contentFilter.Write(delta.Content)
		}
		if reasoning := openAISDKChunkReasoningContent(chunk.RawJSON()); reasoning != "" {
			reasoningBuf.WriteString(reasoning)
			if onReasoning != nil {
				onReasoning(reasoning)
			}
		}
		if choice.FinishReason != "" {
			finishReason = choice.FinishReason
			if finishReason == "function_call" {
				finishReason = "tool_calls"
			}
			shouldStopAfterChunk = true
		}
		if legacy := openAISDKChunkLegacyFunctionCall(chunk.RawJSON()); legacy != nil {
			legacyFunctionCallSeen = true
			if legacy.Name != "" {
				if legacyFunctionCall.Name == "" {
					legacyFunctionCall.Name = legacy.Name
				} else {
					legacyFunctionCall.Name += legacy.Name
				}
			}
			if legacy.Arguments != "" {
				legacyFunctionCall.ArgsBuf.WriteString(legacy.Arguments)
				if legacyFunctionCall.ArgsBuf.Len() > maxToolArgumentsBytes {
					_ = stream.Close()
					return nil, http.StatusOK, nil, fmt.Errorf("tool arguments too large for %s: %d bytes", legacyFunctionCall.Name, legacyFunctionCall.ArgsBuf.Len())
				}
			}
		}
		for _, tc := range delta.ToolCalls {
			idx := int(tc.Index)
			acc := toolCalls[idx]
			if acc == nil {
				acc = &toolCallAcc{}
				toolCalls[idx] = acc
			}
			if tc.ID != "" {
				acc.ID = tc.ID
			}
			if tc.Type != "" {
				acc.Type = tc.Type
			}
			if tc.Function.Name != "" {
				acc.Name = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				acc.ArgsBuf.WriteString(tc.Function.Arguments)
				if acc.ArgsBuf.Len() > maxToolArgumentsBytes {
					_ = stream.Close()
					return nil, http.StatusOK, nil, fmt.Errorf("tool arguments too large for %s: %d bytes", acc.Name, acc.ArgsBuf.Len())
				}
			}
		}
		if shouldStopAfterChunk {
			_ = stream.Close()
			break
		}
	}
	if err := stream.Err(); err != nil {
		if apiErr := openAISDKError(err); apiErr != nil {
			body := []byte(apiErr.RawJSON())
			return nil, apiErr.StatusCode, body, err
		}
		return nil, 0, nil, err
	}
	contentFilter.Flush()

	// Diagnostic: log stream metrics for truncation investigation
	{
		var tcArgSizes []string
		for idx := 0; idx <= len(toolCalls); idx++ {
			if acc, ok := toolCalls[idx]; ok {
				tcArgSizes = append(tcArgSizes, fmt.Sprintf("%s=%d", acc.Name, acc.ArgsBuf.Len()))
			}
		}
		if len(tcArgSizes) > 0 {
			log.Printf("[LLM-stream-diag] SDK stream done: finish_reason=%q content=%d reasoning=%d tool_calls=%d tool_args=[%s]",
				finishReason, contentBuf.Len(), reasoningBuf.Len(), len(toolCalls), strings.Join(tcArgSizes, ", "))
		}
	}
	if raw := bytes.TrimSpace(capture.body()); len(raw) > 0 && !json.Valid(raw) && !bytes.Contains(raw, []byte("data:")) {
		return nil, capture.statusCode(), capture.body(), fmt.Errorf("parse openai stream response: expected SSE event stream or JSON body (body_len=%d)", len(raw))
	}
	if !capture.isEventStream() {
		if contentType := strings.ToLower(strings.TrimSpace(capture.responseContentType())); contentType != "" && !strings.Contains(contentType, "json") {
			return nil, capture.statusCode(), capture.body(), fmt.Errorf("parse openai stream response: expected SSE event stream or JSON body (body_len=%d)", len(capture.body()))
		}
		if raw := capture.body(); len(raw) > 0 && json.Valid(raw) {
			result, err := ParseNonStreamOpenAIResponseBody(raw)
			if err != nil {
				return nil, capture.statusCode(), raw, err
			}
			if onToken != nil && len(result.Choices) > 0 && result.Choices[0].Message.Content != "" {
				onToken(result.Choices[0].Message.Content)
			}
			if onReasoning != nil && len(result.Choices) > 0 && result.Choices[0].Message.ReasoningContent != "" {
				onReasoning(result.Choices[0].Message.ReasoningContent)
			}
			return result, capture.statusCode(), nil, nil
		} else if len(raw) > 0 {
			return nil, capture.statusCode(), raw, fmt.Errorf("parse openai stream response: expected SSE event stream or JSON body (body_len=%d)", len(raw))
		}
	}

	msg := Message{
		Role:             "assistant",
		Content:          StripAllExtra(contentBuf.String()),
		ReasoningContent: reasoningBuf.String(),
	}
	if len(toolCalls) > 0 {
		maxIdx := 0
		for idx := range toolCalls {
			if idx > maxIdx {
				maxIdx = idx
			}
		}
		for i := 0; i <= maxIdx; i++ {
			acc := toolCalls[i]
			if acc == nil {
				continue
			}
			msg.ToolCalls = append(msg.ToolCalls, ToolCall{
				ID:   acc.ID,
				Type: normalizeToolCallType(acc.Type),
				Function: ToolCallFunction{
					Name:      acc.Name,
					Arguments: acc.ArgsBuf.String(),
				},
			})
		}
	}
	if len(toolCalls) == 0 && legacyFunctionCallSeen {
		if call, ok := normalizePlainContentToolCallWithID("", legacyFunctionCall.Type, legacyFunctionCall.Name, json.RawMessage(legacyFunctionCall.ArgsBuf.String())); ok {
			msg.ToolCalls = append(msg.ToolCalls, call)
			msg.Content = ""
			finishReason = "tool_calls"
		}
	}
	if len(msg.ToolCalls) == 0 {
		rawContent := contentBuf.String()
		if contentCalls, malformed := ParseContentToolCallsDetailed(rawContent); len(contentCalls) > 0 {
			msg.ToolCalls = append(msg.ToolCalls, contentCalls...)
			msg.Content = ""
			finishReason = "tool_calls"
		} else if malformed {
			msg.Content = MalformedContentToolCallErrorMsg
			finishReason = "stop"
		}
	}
	if msg.Content == "" && msg.ReasoningContent == "" && len(msg.ToolCalls) == 0 && finishReason == "" && usage == nil {
		return nil, capture.statusCode(), capture.body(), fmt.Errorf("parse openai stream response: empty stream response (body_len=%d)", len(capture.body()))
	}
	var truncatedTools []string
	if capture.isEventStream() {
		if parsed, err := ParseSSEToResponse(capture.body()); err == nil && parsed != nil && len(parsed.Choices) > 0 {
			parsedChoice := parsed.Choices[0]
			if msg.ReasoningContent == "" {
				msg.ReasoningContent = parsedChoice.Message.ReasoningContent
			}
			if msg.Content == "" {
				msg.Content = parsedChoice.Message.Content
			}
			truncatedTools = parsedChoice.TruncatedToolNames
			if len(truncatedTools) > 0 {
				msg.ToolCalls = parsedChoice.Message.ToolCalls
				if parsedChoice.FinishReason != "" {
					finishReason = parsedChoice.FinishReason
				}
			} else if len(msg.ToolCalls) == 0 && len(parsedChoice.Message.ToolCalls) > 0 {
				msg.ToolCalls = parsedChoice.Message.ToolCalls
			}
			if finishReason == "" {
				finishReason = parsedChoice.FinishReason
			}
			if usage == nil {
				usage = parsed.Usage
			}
		}
	}
	var truncatedToolArgs map[string]string
	if len(truncatedTools) == 0 {
		finishReason, truncatedTools, truncatedToolArgs = filterStreamTruncatedToolCalls(&msg, finishReason)
	}
	return &Response{Choices: []Choice{{Message: msg, FinishReason: finishReason, TruncatedToolNames: truncatedTools, TruncatedToolArgs: truncatedToolArgs}}, Usage: usage}, http.StatusOK, nil, nil
}

func openAISDKChunkLegacyFunctionCall(raw string) *sseFunctionCallDelta {
	var payload struct {
		Choices []struct {
			Delta struct {
				FunctionCall *sseFunctionCallDelta `json:"function_call"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if strings.TrimSpace(raw) == "" || json.Unmarshal([]byte(raw), &payload) != nil || len(payload.Choices) == 0 {
		return nil
	}
	return payload.Choices[0].Delta.FunctionCall
}

func openAISDKChunkReasoningContent(raw string) string {
	var payload struct {
		Choices []struct {
			Delta struct {
				ReasoningContent string `json:"reasoning_content"`
				Reasoning        string `json:"reasoning"`
				Thinking         string `json:"thinking"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if strings.TrimSpace(raw) == "" || json.Unmarshal([]byte(raw), &payload) != nil || len(payload.Choices) == 0 {
		return ""
	}
	delta := payload.Choices[0].Delta
	return openAIReasoningText(delta.ReasoningContent, delta.Reasoning, delta.Thinking)
}

func openAISDKChunkUsage(raw string) *Usage {
	var payload struct {
		Usage *Usage `json:"usage"`
	}
	if strings.TrimSpace(raw) == "" || json.Unmarshal([]byte(raw), &payload) != nil {
		return nil
	}
	return payload.Usage
}

func openAISDKOptions(cfg corelib.MaclawLLMConfig, client *http.Client) []option.RequestOption {
	opts := []option.RequestOption{
		option.WithBaseURL(openAISDKBaseURL(cfg)),
		option.WithAPIKey(cfg.Key),
		option.WithHTTPClient(client),
		option.WithHeader("User-Agent", cfg.UserAgent()),
		option.WithMaxRetries(0),
	}
	if corelib.IsCodeGenURL(cfg.URL) {
		opts = append(opts, option.WithHeader(corelib.CodeGenClientNameHeader, corelib.NormalizeCodeGenClientName(cfg.UserAgent())))
	}
	if strings.EqualFold(strings.TrimSpace(cfg.ProviderName), "xAI-Grok") &&
		strings.EqualFold(strings.TrimSpace(cfg.AuthType), "oauth") {
		opts = append(opts, option.WithHeader("X-XAI-Token-Auth", "xai-grok-cli"))
	}
	if timeout := cfg.EffectiveTimeoutSec(); timeout > 0 {
		opts = append(opts, option.WithRequestTimeout(time.Duration(timeout)*time.Second))
	}
	opts = append(opts, option.WithHeader("Cache-Control", "no-cache"))
	return opts
}

func openAISDKBaseURL(cfg corelib.MaclawLLMConfig) string {
	base := corelib.NormalizeGLMCodingPlanOpenAIBaseURL(strings.TrimSpace(cfg.URL), cfg.UserAgent())
	base = strings.TrimRight(base, "/")
	lower := strings.ToLower(base)
	if strings.HasSuffix(lower, "/chat/completions") {
		base = strings.TrimRight(base[:len(base)-len("/chat/completions")], "/")
	}
	if base == "" {
		return "https://api.openai.com/v1"
	}
	if parsed, err := url.Parse(base); err == nil && strings.Trim(parsed.Path, "/") == "" {
		return base + "/v1"
	}
	return base
}

func openAISDKError(err error) *openai.Error {
	var apiErr *openai.Error
	if errors.As(err, &apiErr) {
		return apiErr
	}
	return nil
}

type openAISDKStreamCapture struct {
	mu          sync.Mutex
	limit       int
	status      int
	contentType string
	buf         strings.Builder
}

func (c *openAISDKStreamCapture) captureResponse(resp *http.Response) {
	if resp == nil {
		return
	}
	c.mu.Lock()
	c.status = resp.StatusCode
	c.contentType = resp.Header.Get("Content-Type")
	c.mu.Unlock()
}

func (c *openAISDKStreamCapture) write(p []byte) {
	if c == nil || len(p) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.limit <= 0 || c.buf.Len() >= c.limit {
		return
	}
	remaining := c.limit - c.buf.Len()
	if len(p) > remaining {
		p = p[:remaining]
	}
	_, _ = c.buf.Write(p)
}

func (c *openAISDKStreamCapture) body() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return []byte(c.buf.String())
}

func (c *openAISDKStreamCapture) statusCode() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.status == 0 {
		return http.StatusOK
	}
	return c.status
}

func (c *openAISDKStreamCapture) responseContentType() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.contentType
}

func (c *openAISDKStreamCapture) isEventStream() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Contains(strings.ToLower(c.contentType), "text/event-stream")
}

type openAISDKCaptureTransport struct {
	base    http.RoundTripper
	capture *openAISDKStreamCapture
}

func (t openAISDKCaptureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	if err != nil || resp == nil || resp.Body == nil {
		return resp, err
	}
	t.capture.captureResponse(resp)
	resp.Body = &openAISDKCaptureBody{ReadCloser: resp.Body, capture: t.capture}
	return resp, nil
}

type openAISDKRawBodyTransport struct {
	base    http.RoundTripper
	body    []byte
	capture *openAISDKStreamCapture
}

func (t openAISDKRawBodyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	body := append([]byte(nil), t.body...)
	req.Body = io.NopCloser(bytes.NewReader(body))
	// The raw-body adapter normally retains the SDK's redirect compatibility.
	// At a request-owner boundary, however, a GetBody hook would allow net/http
	// to replay the exact payload without returning to RunLoop for a fresh
	// surface/manifest/receipt. The final receipt wrapper independently clears
	// this hook at its wire boundary; doing the same here keeps this adapter safe
	// when it is the outer wrapper around that receipt transport.
	if TransparentRequestRetriesDisabled(req.Context()) {
		req.GetBody = nil
	} else {
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(body)), nil
		}
	}
	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := base.RoundTrip(req)
	if err != nil || resp == nil || resp.Body == nil || t.capture == nil {
		return resp, err
	}
	t.capture.captureResponse(resp)
	resp.Body = &openAISDKCaptureBody{ReadCloser: resp.Body, capture: t.capture}
	return resp, nil
}

type openAISDKCaptureBody struct {
	io.ReadCloser
	capture *openAISDKStreamCapture
}

func (b *openAISDKCaptureBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.capture.write(p[:n])
	}
	return n, err
}

func openAISDKClientWithCapture(client *http.Client, capture *openAISDKStreamCapture) *http.Client {
	if client == nil {
		client = http.DefaultClient
	}
	clone := *client
	clone.Transport = openAISDKCaptureTransport{base: client.Transport, capture: capture}
	return &clone
}

func openAISDKClientWithRawBody(client *http.Client, body []byte, capture *openAISDKStreamCapture) *http.Client {
	if client == nil {
		client = http.DefaultClient
	}
	clone := *client
	clone.Transport = openAISDKRawBodyTransport{base: client.Transport, body: body, capture: capture}
	return &clone
}
