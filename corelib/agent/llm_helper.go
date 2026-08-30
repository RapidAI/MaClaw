package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

const (
	simpleLLMMaxAttempts    = 5
	simpleLLMInitialBackoff = 200 * time.Millisecond
)

// thinkTagPattern matches <think>...</think> blocks produced by reasoning
// models (DeepSeek, Kimi, QwQ, etc.).
var thinkTagPattern = regexp.MustCompile(`(?si)<think>.*?</think>|<think>.*$`)

// StripThinkingTags removes <think>...</think> blocks from LLM output.
func StripThinkingTags(s string) string {
	if !strings.Contains(s, "<think>") {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(thinkTagPattern.ReplaceAllString(s, ""))
}

// LLMSimpleResponse is a minimal response from a simple LLM request.
type LLMSimpleResponse struct {
	Content string
}

// SimpleLLMRequestOptions describes an optional machine-readable response
// contract for a simple request.  Most simple requests intentionally remain
// free-form; control-plane callers must opt in and handle a provider that
// rejects the requested capability rather than accepting silently degraded
// output.
type SimpleLLMRequestOptions struct {
	ResponseFormat         interface{}
	PreserveResponseFormat bool
}

type llmHTTPError struct {
	statusCode int
	message    string
}

func (e *llmHTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.statusCode, e.message)
}

func (e *llmHTTPError) StatusCode() int {
	return e.statusCode
}

func newLLMHTTPError(statusCode int, message string) error {
	return &llmHTTPError{statusCode: statusCode, message: message}
}

func shouldRetrySimpleLLMError(err error) bool {
	if err == nil {
		return false
	}
	if llm.IsTransientTokenValidationError(err) {
		return true
	}
	var httpErr *llmHTTPError
	if errors.As(err, &httpErr) {
		return httpErr.statusCode == http.StatusRequestTimeout || httpErr.statusCode == http.StatusTooManyRequests || httpErr.statusCode >= http.StatusInternalServerError
	}
	var statusErr *llm.HTTPStatusError
	if errors.As(err, &statusErr) && statusErr != nil {
		return statusErr.StatusCode == http.StatusRequestTimeout || statusErr.StatusCode == http.StatusTooManyRequests || statusErr.StatusCode >= http.StatusInternalServerError
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

func waitSimpleLLMBackoff(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// dumpLLMContext reports LLM HTTP failures without persisting prompt/request
// bodies. Those bodies may contain browser observations, screenshots OCR, or
// user secrets, so writing them under ~/.maclaw would leak private context.
func dumpLLMContext(statusCode int, respMsg string, requestBody []byte) error {
	ctxLen := len(requestBody)
	if statusCode != http.StatusInternalServerError {
		return newLLMHTTPError(statusCode, fmt.Sprintf("%s (context %d bytes)", respMsg, ctxLen))
	}
	return fmt.Errorf("HTTP %d (context %d bytes, request body not dumped): %s", statusCode, ctxLen, respMsg)
}

// DoSimpleLLMRequest sends a simple chat completion request (no tool calling)
// supporting both OpenAI and Anthropic protocols.
func DoSimpleLLMRequest(cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client, timeout time.Duration) (*LLMSimpleResponse, error) {
	return DoSimpleLLMRequestContext(context.Background(), cfg, messages, client, timeout)
}

// DoSimpleLLMRequestContext is the cancellable counterpart of
// DoSimpleLLMRequest. Control-plane classifiers use it so a cancelled agent
// request does not leave an unrelated LLM classification running in the
// background. It performs no tool calling.
func DoSimpleLLMRequestContext(parent context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client, timeout time.Duration) (*LLMSimpleResponse, error) {
	return DoSimpleLLMRequestContextWithOptions(parent, cfg, messages, client, timeout, SimpleLLMRequestOptions{})
}

// DoSimpleLLMRequestContextWithOptions is the control-plane counterpart of
// DoSimpleLLMRequestContext. It preserves a supplied structured-output
// contract on both Chat Completions and Responses API requests.
func DoSimpleLLMRequestContextWithOptions(parent context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client, timeout time.Duration, options SimpleLLMRequestOptions) (*LLMSimpleResponse, error) {
	if options.ResponseFormat != nil && cfg.Protocol == "anthropic" {
		return nil, fmt.Errorf("simple LLM structured-output contract is unsupported for Anthropic protocol")
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	startedAt := time.Now()
	var lastErr error
	backoff := simpleLLMInitialBackoff
	for attempt := 1; attempt <= simpleLLMMaxAttempts; attempt++ {
		var (
			resp *LLMSimpleResponse
			err  error
		)
		if cfg.IsResponsesAPI() || cfg.IsResponsesWebSocket() {
			resp, err = doSimpleResponsesRequest(ctx, cfg, messages, client, options)
		} else if cfg.Protocol == "anthropic" {
			resp, err = doSimpleAnthropicRequest(ctx, cfg, messages, client)
		} else {
			resp, err = doSimpleOpenAIRequest(ctx, cfg, messages, client, options)
		}
		if err == nil {
			if attempt > 1 {
				log.Printf("[LLM Simple] succeeded on attempt %d/%d for model=%s protocol=%s after %s", attempt, simpleLLMMaxAttempts, cfg.Model, cfg.Protocol, time.Since(startedAt).Round(time.Millisecond))
			}
			return resp, nil
		}
		lastErr = err
		if !shouldRetrySimpleLLMError(err) || attempt == simpleLLMMaxAttempts {
			break
		}
		elapsed := time.Since(startedAt).Round(time.Millisecond)
		wait := backoff
		if llm.IsTransientTokenValidationError(err) && wait < time.Second {
			wait = time.Second
		}
		log.Printf("[LLM Simple] attempt %d/%d failed for model=%s protocol=%s after %s: %v; retrying in %s", attempt, simpleLLMMaxAttempts, cfg.Model, cfg.Protocol, elapsed, err, wait)
		if err := waitSimpleLLMBackoff(ctx, wait); err != nil {
			lastErr = err
			log.Printf("[LLM Simple] stop retrying for model=%s protocol=%s after %s: %v", cfg.Model, cfg.Protocol, time.Since(startedAt).Round(time.Millisecond), err)
			break
		}
		backoff *= 2
	}
	if shouldRetrySimpleLLMError(lastErr) {
		return nil, fmt.Errorf("simple LLM request failed after %d attempts: %w", simpleLLMMaxAttempts, lastErr)
	}
	return nil, lastErr
}

func doSimpleOpenAIRequest(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client, options SimpleLLMRequestOptions) (*LLMSimpleResponse, error) {
	req, data, endpoint, err := llm.NewOpenAIChatRequest(ctx, cfg, messages, llm.OpenAIChatRequestOptions{
		Stream:                 true,
		ResponseFormat:         options.ResponseFormat,
		PreserveResponseFormat: options.PreserveResponseFormat,
	})
	if err != nil {
		return nil, err
	}
	log.Printf("[LLM Simple] POST %s model=%s configured_model=%s protocol=%s (stream=true)", endpoint, cfg.UpstreamModel(), cfg.Model, cfg.Protocol)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if readErr != nil && len(body) == 0 {
		// Context timeout or network error during body read with no data received.
		// Return the read error directly instead of trying to parse empty/partial body.
		return nil, readErr
	}
	if resp.StatusCode != http.StatusOK {
		// Keep Body for UserFacingError (structured detail). HTTP 500 still uses
		// dumpLLMContext so prompt bodies are never written to disk.
		if resp.StatusCode == http.StatusInternalServerError {
			msg := llm.UserFacingHTTPStatus(resp.StatusCode, body)
			if msg == "" {
				msg = fmt.Sprintf("llm request failed body_len=%d", len(body))
			}
			return nil, dumpLLMContext(resp.StatusCode, msg, data)
		}
		return nil, &llm.HTTPStatusError{StatusCode: resp.StatusCode, Body: append([]byte(nil), body...)}
	}

	parsed, err := llm.ParseNonStreamOpenAIResponseBody(body)
	if err != nil {
		return nil, fmt.Errorf("parse response: %w (body_len=%d)", err, len(body))
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("no response from model")
	}
	text := parsed.Choices[0].Message.Content
	if text == "" {
		text = parsed.Choices[0].Message.ReasoningContent
	}
	if text == "" {
		return nil, fmt.Errorf("no response from model")
	}
	return &LLMSimpleResponse{Content: StripThinkingTags(text)}, nil
}

func doSimpleResponsesRequest(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client, options SimpleLLMRequestOptions) (*LLMSimpleResponse, error) {
	extraBody := map[string]interface{}(nil)
	if options.ResponseFormat != nil {
		extraBody = map[string]interface{}{"response_format": options.ResponseFormat}
	}
	req, data, endpoint, err := llm.NewResponsesAPIRequest(ctx, cfg, messages, llm.ResponsesAPIRequestOptions{
		Stream:                 false,
		ExtraBody:              extraBody,
		PreserveResponseFormat: options.PreserveResponseFormat,
	})
	if err != nil {
		return nil, err
	}
	log.Printf("[LLM Simple] POST %s model=%s configured_model=%s protocol=%s wire_api=%s (stream=false)", endpoint, cfg.UpstreamModel(), cfg.Model, cfg.Protocol, cfg.WireAPI)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if readErr != nil && len(body) == 0 {
		return nil, readErr
	}
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusInternalServerError {
			msg := llm.UserFacingHTTPStatus(resp.StatusCode, body)
			if msg == "" {
				msg = fmt.Sprintf("llm responses request failed body_len=%d", len(body))
			}
			return nil, dumpLLMContext(resp.StatusCode, msg, data)
		}
		return nil, &llm.HTTPStatusError{StatusCode: resp.StatusCode, Body: append([]byte(nil), body...)}
	}

	parsed, err := llm.ParseNonStreamResponsesAPIBody(body)
	if err != nil {
		return nil, fmt.Errorf("parse responses response: %w (body_len=%d)", err, len(body))
	}
	if parsed == nil || len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("no response from model")
	}
	text := parsed.Choices[0].Message.Content
	if text == "" {
		text = parsed.Choices[0].Message.ReasoningContent
	}
	if text == "" {
		return nil, fmt.Errorf("no response from model")
	}
	return &LLMSimpleResponse{Content: StripThinkingTags(text)}, nil
}

func doSimpleAnthropicRequest(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client) (*LLMSimpleResponse, error) {
	resp, err := llm.DoAnthropicRequest(ctx, cfg, messages, nil, client)
	if err != nil {
		return nil, err
	}
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
	return &LLMSimpleResponse{Content: StripThinkingTags(text)}, nil
}
