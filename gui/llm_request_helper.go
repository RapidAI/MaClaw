package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// dumpLLMContext reports LLM HTTP failures without persisting prompt/request
// bodies. Those bodies may contain browser observations, screenshots OCR, or
// user secrets, so writing them under ~/.maclaw would leak private context.
func dumpLLMContext(statusCode int, respMsg string, requestBody []byte, tempDir string) error {
	_ = tempDir
	ctxLen := len(requestBody)
	if statusCode != http.StatusInternalServerError {
		return fmt.Errorf("HTTP %d: %s (context %d bytes)", statusCode, respMsg, ctxLen)
	}
	return fmt.Errorf("HTTP %d (context %d bytes, request body not dumped): %s", statusCode, ctxLen, respMsg)
}

// llmSimpleResponse is a minimal response from a simple (non-tool-calling) LLM request.
type llmSimpleResponse struct {
	Content string
}

// simpleLLMRequestOptions describes a control-plane request whose output must
// follow a machine-readable contract.  Ordinary simple requests intentionally
// retain their existing provider behaviour.
type simpleLLMRequestOptions struct {
	ResponseFormat         interface{}
	PreserveResponseFormat bool
}

// attachLightweightHubHint marks classify/summary/intent helper calls so Hub
// L1 attributes them as P2 instead of falling through to body heuristics.
func attachLightweightHubHint(cfg corelib.MaclawLLMConfig, task llm.TaskType) corelib.MaclawLLMConfig {
	if !corelib.IsHubManagedLLMEndpoint(cfg.URL, cfg.Model) && !cfg.HubManaged {
		return cfg
	}
	return cfg.WithHubWorkloadHints(string(task), "", "")
}

// doSimpleLLMRequest sends a simple chat completion request (no tool calling)
// to the configured LLM, supporting both OpenAI and Anthropic protocols.
// It returns the text content of the assistant's reply.
func doSimpleLLMRequest(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client, timeout time.Duration) (*llmSimpleResponse, error) {
	return doSimpleLLMRequestWithOptions(ctx, cfg, messages, client, timeout, simpleLLMRequestOptions{})
}

func doSimpleLLMRequestWithOptions(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client, timeout time.Duration, opts simpleLLMRequestOptions) (*llmSimpleResponse, error) {
	ctx = llm.WithRequestTraceIfMissing(ctx, "simple_llm")
	lease, trace, acquireErr := acquireLLMSchedulerLease(ctx)
	if acquireErr != nil {
		return nil, acquireErr
	}
	defer lease.Release()
	scheduledCtx, scheduledCancel := context.WithCancel(ctx)
	lease.SetCancel(scheduledCancel)
	defer scheduledCancel()
	var resp *llmSimpleResponse
	var err error
	if cfg.IsResponsesAPI() || cfg.IsResponsesWebSocket() {
		resp, err = doSimpleResponsesRequest(scheduledCtx, cfg, messages, client, timeout, opts)
	} else if cfg.Protocol == "anthropic" {
		resp, err = doSimpleAnthropicRequest(scheduledCtx, cfg, messages, client, timeout, opts)
	} else {
		resp, err = doSimpleOpenAIRequest(scheduledCtx, cfg, messages, client, timeout, opts)
	}
	globalLLMScheduler.ObserveResult(trace, err)
	return resp, err
}

func doSimpleOpenAIRequest(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client, timeout time.Duration, requestOpts ...simpleLLMRequestOptions) (*llmSimpleResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	opts := simpleLLMRequestOptions{}
	if len(requestOpts) > 0 {
		opts = requestOpts[0]
	}
	// Preserve the mature simple-request path for ordinary callers.  Only the
	// control-plane caller below opts into a response contract, so this change
	// cannot silently alter compatibility retries for summaries, OCR, etc.
	if opts.ResponseFormat == nil {
		parsed, err := llm.DoOpenAIRequest(ctx, cfg, messages, nil, client)
		if err != nil {
			var httpErr *llm.HTTPStatusError
			if errors.As(err, &httpErr) && httpErr != nil && len(httpErr.Body) > 0 {
				body := strings.TrimSpace(string(httpErr.Body))
				if len(body) > 512 {
					body = body[:512]
				}
				return nil, fmt.Errorf("%w %s", err, body)
			}
			msg := err.Error()
			if strings.HasPrefix(msg, "HTTP 500:") {
				endpoint, data, buildErr := llm.BuildOpenAIChatRequestData(cfg, messages, llm.OpenAIChatRequestOptions{Stream: false})
				if buildErr != nil {
					return nil, err
				}
				_ = endpoint
				return nil, dumpLLMContext(http.StatusInternalServerError, "llm request failed", data, "")
			}
			return nil, err
		}
		if len(parsed.Choices) == 0 {
			return nil, fmt.Errorf("no response from model")
		}
		text := parsed.Choices[0].Message.Content
		if text == "" {
			text = parsed.Choices[0].Message.ReasoningContent
		}
		return &llmSimpleResponse{Content: stripThinkingTags(text)}, nil
	}

	req, data, endpoint, err := llm.NewOpenAIChatRequest(ctx, cfg, messages, llm.OpenAIChatRequestOptions{
		Stream:                 false,
		ResponseFormat:         opts.ResponseFormat,
		PreserveResponseFormat: opts.PreserveResponseFormat,
	})
	if err != nil {
		return nil, err
	}
	traceFields := llm.RequestTraceLogFields(req.Context())
	upstreamModel := cfg.UpstreamModel()
	log.Printf("[LLM] POST %s model=%s configured_model=%s protocol=%s simple=true structured=%t %s", endpoint, upstreamModel, cfg.Model, cfg.Protocol, opts.ResponseFormat != nil, traceFields)
	startedAt := time.Now()
	if client == nil {
		client = http.DefaultClient
	}
	httpResp, err := client.Do(req)
	if err != nil {
		log.Printf("[LLM] done %s model=%s configured_model=%s protocol=%s simple=true structured=%t status=error elapsed=%s err=%v %s", endpoint, upstreamModel, cfg.Model, cfg.Protocol, opts.ResponseFormat != nil, time.Since(startedAt).Round(time.Millisecond), err, traceFields)
		return nil, fmt.Errorf("[%s] %w", endpoint, err)
	}
	defer httpResp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(httpResp.Body, 256*1024))
	if readErr != nil {
		return nil, readErr
	}
	log.Printf("[LLM] done %s model=%s configured_model=%s protocol=%s simple=true structured=%t status=%d elapsed=%s body_len=%d %s", endpoint, upstreamModel, cfg.Model, cfg.Protocol, opts.ResponseFormat != nil, httpResp.StatusCode, time.Since(startedAt).Round(time.Millisecond), len(body), traceFields)
	if httpResp.StatusCode != http.StatusOK {
		if httpResp.StatusCode == http.StatusInternalServerError {
			return nil, dumpLLMContext(httpResp.StatusCode, "llm request failed", data, "")
		}
		return nil, &llm.HTTPStatusError{StatusCode: httpResp.StatusCode, Body: append([]byte(nil), body...)}
	}
	parsed, err := llm.ParseNonStreamOpenAIResponseBody(body)
	if err != nil {
		return nil, err
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("no response from model")
	}
	text := parsed.Choices[0].Message.Content
	if text == "" {
		text = parsed.Choices[0].Message.ReasoningContent
	}
	return &llmSimpleResponse{Content: stripThinkingTags(text)}, nil
}

func doSimpleResponsesRequest(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client, timeout time.Duration, requestOpts ...simpleLLMRequestOptions) (*llmSimpleResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	opts := simpleLLMRequestOptions{}
	if len(requestOpts) > 0 {
		opts = requestOpts[0]
	}
	if client == nil {
		client = http.DefaultClient
	}

	requestBody := map[string]interface{}(nil)
	if opts.ResponseFormat != nil {
		requestBody = map[string]interface{}{"response_format": opts.ResponseFormat}
	}
	req, data, endpoint, err := llm.NewResponsesAPIRequest(ctx, cfg, messages, llm.ResponsesAPIRequestOptions{
		Stream:                 false,
		ExtraBody:              requestBody,
		PreserveResponseFormat: opts.PreserveResponseFormat,
	})
	if err != nil {
		return nil, err
	}
	traceFields := llm.RequestTraceLogFields(req.Context())
	upstreamModel := cfg.UpstreamModel()
	log.Printf("[LLM] POST %s model=%s configured_model=%s wire_api=responses simple=true %s", endpoint, upstreamModel, cfg.Model, traceFields)

	startedAt := time.Now()
	httpResp, err := client.Do(req)
	if err != nil {
		log.Printf("[LLM] done %s model=%s configured_model=%s wire_api=responses simple=true status=error elapsed=%s err=%v %s", endpoint, upstreamModel, cfg.Model, time.Since(startedAt).Round(time.Millisecond), err, traceFields)
		return nil, fmt.Errorf("[%s] %w", endpoint, err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(httpResp.Body, 4096))
		log.Printf("[LLM] done %s model=%s configured_model=%s wire_api=responses simple=true status=%d elapsed=%s body_len=%d %s", endpoint, upstreamModel, cfg.Model, httpResp.StatusCode, time.Since(startedAt).Round(time.Millisecond), len(body), traceFields)
		if httpResp.StatusCode == http.StatusInternalServerError {
			return nil, dumpLLMContext(httpResp.StatusCode, classifyResponsesAPIHTTPError(httpResp.StatusCode, body, endpoint, upstreamModel, cfg.ProviderName), data, "")
		}
		return nil, fmt.Errorf("%s", classifyResponsesAPIHTTPError(httpResp.StatusCode, body, endpoint, upstreamModel, cfg.ProviderName))
	}

	parsed, parseErr := llm.ParseNonStreamResponsesAPIResponse(httpResp)
	log.Printf("[LLM] done %s model=%s configured_model=%s wire_api=responses simple=true status=%d elapsed=%s parse_err=%v %s", endpoint, upstreamModel, cfg.Model, httpResp.StatusCode, time.Since(startedAt).Round(time.Millisecond), parseErr, traceFields)
	if parseErr != nil {
		return nil, parseErr
	}
	if parsed == nil || len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("no response from model")
	}
	text := parsed.Choices[0].Message.Content
	if text == "" {
		text = parsed.Choices[0].Message.ReasoningContent
	}
	if text == "" {
		return nil, fmt.Errorf("no text response from model")
	}
	return &llmSimpleResponse{Content: stripThinkingTags(text)}, nil
}

func doSimpleAnthropicRequest(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client, timeout time.Duration, requestOpts ...simpleLLMRequestOptions) (*llmSimpleResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if len(requestOpts) > 0 && requestOpts[0].ResponseFormat != nil {
		// Anthropic's Messages API has no response_format analogue.  The tree
		// parser remains the validator and reports a protocol failure if prose
		// is returned; this branch is intentionally not a lexical fallback.
		log.Printf("[LLM] structured simple request uses Anthropic prompt contract only")
	}

	traceFields := llm.RequestTraceLogFields(ctx)
	upstreamModel := cfg.UpstreamModel()
	endpoint := corelib.AnthropicMessagesEndpoint(cfg.URL)
	log.Printf("[LLM] POST %s model=%s configured_model=%s protocol=anthropic simple=true sdk=anthropic-sdk-go %s", endpoint, upstreamModel, cfg.Model, traceFields)
	startedAt := time.Now()
	resp, err := llm.DoAnthropicRequest(ctx, cfg, messages, nil, client)
	if err != nil {
		log.Printf("[LLM] done %s model=%s configured_model=%s protocol=anthropic simple=true status=error elapsed=%s err=%v %s", endpoint, upstreamModel, cfg.Model, time.Since(startedAt).Round(time.Millisecond), err, traceFields)
		return nil, err
	}
	log.Printf("[LLM] done %s model=%s configured_model=%s protocol=anthropic simple=true status=200 elapsed=%s %s", endpoint, upstreamModel, cfg.Model, time.Since(startedAt).Round(time.Millisecond), traceFields)
	if resp == nil || len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no response from model")
	}
	text := resp.Choices[0].Message.Content
	if text == "" {
		text = resp.Choices[0].Message.ReasoningContent
	}
	if text == "" {
		return nil, fmt.Errorf("no text response from model")
	}
	return &llmSimpleResponse{Content: stripThinkingTags(text)}, nil
}
