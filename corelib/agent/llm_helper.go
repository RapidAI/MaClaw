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
	"github.com/RapidAI/CodeClaw/corelib/lobsterai"
	"github.com/RapidAI/CodeClaw/corelib/qoder"
	"github.com/RapidAI/CodeClaw/corelib/trae"
	"github.com/RapidAI/CodeClaw/corelib/workbuddy"
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

// A recoverable model failure keeps the same task open. A period quota
// waits until the window Hub named, then the turn continues. A short
// outage uses the fast backoff and, if it is still down, a slower run of
// the same backoff. A denial that will not recover, and a wait the caller
// already cancelled, ends the turn.
const (
	maxPeriodLimitWaits       = 8
	maxTransientContinuations = 40
	minPeriodLimitWait        = 5 * time.Second
	maxPeriodLimitWait        = 6 * time.Hour
	periodLimitWaitSlack      = 2 * time.Second
)

// periodLimitWaitOverride shortens a real Hub wait in tests. Zero keeps
// the duration Hub asked for.
var periodLimitWaitOverride time.Duration

type llmContinueKind int

const (
	llmContinueStop llmContinueKind = iota
	llmContinueFast
	llmContinuePeriod
	llmContinueKeepGoing
)

// llmContinuationBudget counts how many times this turn has already stayed
// open for one model failure. Fast retries are the short outage budget.
// Period waits follow Hub's clock. Keep-going covers an outage that is
// still not a permanent denial after the fast budget is spent.
// fastOnly skips that flat tail. A compatibility host resends the whole
// prompt, so continuing every 32s would repeat a large request long after
// the exponential retries have already shown the gateway is still down.
type llmContinuationBudget struct {
	fast     int
	period   int
	extra    int
	fastOnly bool
}

func (b *llmContinuationBudget) next(err error, now time.Time) (time.Duration, llmContinueKind) {
	if b == nil || err == nil {
		return 0, llmContinueStop
	}
	if wait, ok := hubPeriodLimitWait(err, now); ok {
		if b.period >= maxPeriodLimitWaits {
			return 0, llmContinueStop
		}
		b.period++
		return wait, llmContinuePeriod
	}
	if !shouldRetrySimpleLLMError(err) {
		return 0, llmContinueStop
	}
	if b.fast < maxLLMRetries {
		b.fast++
		return llmRetryBackoff(b.fast), llmContinueFast
	}
	if b.fastOnly || b.extra >= maxTransientContinuations {
		return 0, llmContinueStop
	}
	b.extra++
	return maxLLMRetryBackoff, llmContinueKeepGoing
}

func formatContinuationWait(wait time.Duration) string {
	if wait <= 0 {
		return "片刻"
	}
	if wait < time.Minute {
		seconds := int((wait + time.Second - 1) / time.Second)
		return fmt.Sprintf("%d 秒", seconds)
	}
	if wait < time.Hour {
		minutes := int((wait + time.Minute - 1) / time.Minute)
		return fmt.Sprintf("%d 分钟", minutes)
	}
	hours := int((wait + time.Hour - 1) / time.Hour)
	return fmt.Sprintf("%d 小时", hours)
}

// hubPeriodLimitWait is the pause before the same task calls the model again.
// Hub has to name a recovery time. A period denial with no clock is not
// waited out here, so a missing field cannot spin the turn.
func hubPeriodLimitWait(err error, now time.Time) (time.Duration, bool) {
	if !hubPeriodLimitError(err) {
		return 0, false
	}
	wait := hubRetryAfter(err, now)
	if wait <= 0 {
		return 0, false
	}
	if periodLimitWaitOverride > 0 {
		return periodLimitWaitOverride, true
	}
	if wait < minPeriodLimitWait {
		wait = minPeriodLimitWait
	}
	if wait > maxPeriodLimitWait {
		wait = maxPeriodLimitWait
	}
	return wait + periodLimitWaitSlack, true
}

func hubPeriodLimitError(err error) bool {
	if err == nil || hubRateLimitWaitCanceled(err) {
		return false
	}
	return hubPeriodLimitText(hubDenialBody(err))
}

func hubPeriodLimitText(text string) bool {
	text = strings.ToLower(text)
	return strings.Contains(text, "llm_service_period_limited") ||
		strings.Contains(text, "current period credit limit") ||
		strings.Contains(text, "周期限流") ||
		strings.Contains(text, "周期额度")
}

func hubRateLimitWaitCanceled(err error) bool {
	text := strings.ToLower(hubDenialBody(err))
	return strings.Contains(text, "llm_endpoint_user_rate_limit_wait_canceled") ||
		strings.Contains(text, "canceled while waiting in hub user rate-limit queue") ||
		strings.Contains(text, "限流排队等待时被取消")
}

func hubDenialBody(err error) string {
	if err == nil {
		return ""
	}
	var statusErr *llm.HTTPStatusError
	if errors.As(err, &statusErr) && statusErr != nil && len(statusErr.Body) > 0 {
		return string(statusErr.Body)
	}
	var httpErr *llmHTTPError
	if errors.As(err, &httpErr) && httpErr != nil && httpErr.message != "" {
		return httpErr.message
	}
	return err.Error()
}

func hubRetryAfter(err error, now time.Time) time.Duration {
	var statusErr *llm.HTTPStatusError
	if errors.As(err, &statusErr) && statusErr != nil && len(statusErr.Body) > 0 {
		return llm.HubRetryWait(statusErr.Body, now)
	}
	var httpErr *llmHTTPError
	if errors.As(err, &httpErr) && httpErr != nil && httpErr.message != "" {
		if wait := llm.HubRetryWait([]byte(httpErr.message), now); wait > 0 {
			return wait
		}
	}
	return llm.HubRetryWait([]byte(err.Error()), now)
}

// hubDenialForbidsRetry reports a Hub answer that will not change on a
// short retry. A period quota is exhausted until the next period, and a
// canceled rate-limit wait means the caller already left the queue.
func hubDenialForbidsRetry(err error) bool {
	if err == nil {
		return false
	}
	var statusErr *llm.HTTPStatusError
	if errors.As(err, &statusErr) && statusErr != nil && hubDenialTextForbidsRetry(string(statusErr.Body)) {
		return true
	}
	var httpErr *llmHTTPError
	if errors.As(err, &httpErr) && httpErr != nil && hubDenialTextForbidsRetry(httpErr.message) {
		return true
	}
	return hubDenialTextForbidsRetry(err.Error())
}

// Markers match Hub's JSON body and the desktop classifier in agentruntime.
// This package cannot import agentruntime, because that package imports agent.
func hubDenialTextForbidsRetry(text string) bool {
	// A provider whose quota is used up answers the same 429 until it is
	// topped up: stop instead of retrying for minutes.
	if llm.IsQuotaExhaustedText(text) {
		return true
	}
	text = strings.ToLower(text)
	return strings.Contains(text, "llm_service_period_limited") ||
		strings.Contains(text, "llm_endpoint_user_rate_limit_wait_canceled") ||
		strings.Contains(text, "current period credit limit") ||
		strings.Contains(text, "canceled while waiting in hub user rate-limit queue") ||
		strings.Contains(text, "周期限流") ||
		strings.Contains(text, "周期额度") ||
		strings.Contains(text, "限流排队等待时被取消")
}

// incompleteHTTPBody reports a read that ended before the response was a
// complete document. An empty body and a 200 prefix must keep that error:
// parsing the prefix turns a route deadline into a non-retryable JSON error.
// A non-OK status still becomes an HTTP status error, so a short 502 page
// keeps the status that the outer retry already understands.
func incompleteHTTPBody(statusCode int, body []byte, readErr error) bool {
	return readErr != nil && (len(body) == 0 || statusCode == http.StatusOK)
}

// cleanRetryableLLMFailure is a completed gateway or transport rejection with
// no model payload. The same task can wait and send again. A partial response
// stays with the caller: retrying it would repeat visible output, and a
// period-quota or canceled rate-limit wait is not a short outage.
func cleanRetryableLLMFailure(resp *llm.Response, err error) bool {
	if err == nil || resp != nil || hubDenialForbidsRetry(err) {
		return false
	}
	return shouldRetrySimpleLLMError(err)
}

// gatewayRejectedLLMFailure is cleanRetryableLLMFailure restricted to failures
// the gateway itself answered with a status, plus token validation (which runs
// before the model). It is the gate for hosts that contain ambiguous delivery:
// a bare transport error means the request was written but no reply came back,
// so the model may already have processed it and a retry would duplicate the
// turn. Only a definitive rejection proves nothing was generated.
func gatewayRejectedLLMFailure(resp *llm.Response, err error) bool {
	if !cleanRetryableLLMFailure(resp, err) {
		return false
	}
	if llm.IsTransientTokenValidationError(err) {
		return true
	}
	var httpErr *llmHTTPError
	if errors.As(err, &httpErr) {
		return true
	}
	var statusErr *llm.HTTPStatusError
	if errors.As(err, &statusErr) && statusErr != nil {
		return true
	}
	// Headers arrived and the body was dropped before any payload. The coding
	// hosts suppress the immediate non-stream fallback for this case, so it has
	// to reach the outer backoff or the turn dies on the first dropped
	// connection. A gateway that names the same stall inside an HTTP 200
	// error frame ("upstream stream idle timeout") is that dropped body, not
	// a model rejection.
	if llm.IsUnexpectedStreamEOF(err) || llm.IsStreamIdleStall(err) {
		return true
	}
	return false
}

func shouldRetrySimpleLLMError(err error) bool {
	if err == nil || hubDenialForbidsRetry(err) {
		return false
	}
	if llm.IsTransientTokenValidationError(err) {
		return true
	}
	var httpErr *llmHTTPError
	if errors.As(err, &httpErr) {
		return httpErr.statusCode == http.StatusRequestTimeout ||
			httpErr.statusCode == http.StatusTooManyRequests ||
			(httpErr.statusCode == http.StatusForbidden && !llm.IsPermanentForbiddenError(err)) ||
			httpErr.statusCode >= http.StatusInternalServerError
	}
	var statusErr *llm.HTTPStatusError
	if errors.As(err, &statusErr) && statusErr != nil {
		return statusErr.StatusCode == http.StatusRequestTimeout ||
			statusErr.StatusCode == http.StatusTooManyRequests ||
			(statusErr.StatusCode == http.StatusForbidden && !llm.IsPermanentForbiddenError(err)) ||
			statusErr.StatusCode >= http.StatusInternalServerError
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	// Headers arrived and then the stream went silent. That is the idle
	// reader's stall, or the same stall named by the gateway, not a
	// body-parse failure. It waits for the outer backoff instead of an
	// immediate second request.
	if llm.IsStreamIdleStall(err) {
		return true
	}
	// The upstream closed the SSE body mid-frame. Coding hosts suppress the
	// immediate non-stream fallback, so this has to enter the same backoff
	// or the turn ends on the first dropped connection.
	if llm.IsUnexpectedStreamEOF(err) {
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
	if cfg.IsGoogleProtocol() {
		return doSimpleGoogleRequest(ctx, cfg, messages, client, options)
	}
	resp, err := doSimpleOpenAIRequestOnce(ctx, cfg, messages, client, options)
	// HTTP 400 is not an outer transport retry. A thinking-off refusal still
	// has one legal-effort retry, matching the non-stream Do helper.
	if next, ok := llm.ConfigAfterThinkingRejection(cfg, err); ok {
		return doSimpleOpenAIRequestOnce(ctx, next, messages, client, options)
	}
	return resp, err
}

func doSimpleGoogleRequest(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client, options SimpleLLMRequestOptions) (*LLMSimpleResponse, error) {
	resp, err := llm.DoGoogleRequestWithOptions(ctx, cfg, messages, client, llm.GoogleGenerateRequestOptions{
		ResponseFormat: options.ResponseFormat,
	})
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
		return nil, fmt.Errorf("no response from model")
	}
	return &LLMSimpleResponse{Content: StripThinkingTags(text)}, nil
}

func doSimpleOpenAIRequestOnce(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client, options SimpleLLMRequestOptions) (*LLMSimpleResponse, error) {
	if workbuddy.Matches(cfg) {
		client = workbuddy.WrapClient(client)
	}
	if trae.Matches(cfg.ProviderName, cfg.URL) {
		client = trae.WrapClient(client)
	}
	if lobsterai.Matches(cfg.ProviderName, cfg.URL) {
		client = lobsterai.WrapClient(client)
	}
	if qoder.Matches(cfg) {
		client = qoder.WrapClientForConfig(client, cfg)
	}
	// A structured control-plane call (intent tree) matches the desktop path:
	// one JSON body, not a token stream. Several OpenAI-compatible relays
	// drop json_schema when stream is set, then the model writes prose until
	// the classification budget expires.
	// This helper always parses one completion. Qoder's upstream is SSE-only;
	// asking for stream here would hand the parser the event stream. Leave
	// stream false so the signed transport aggregates that SSE into JSON.
	stream := options.ResponseFormat == nil && !qoder.Matches(cfg)
	req, data, endpoint, err := llm.NewOpenAIChatRequest(ctx, cfg, messages, llm.OpenAIChatRequestOptions{
		Stream:                 stream,
		ResponseFormat:         options.ResponseFormat,
		PreserveResponseFormat: options.PreserveResponseFormat,
	})
	if err != nil {
		return nil, err
	}
	log.Printf("[LLM Simple] POST %s model=%s configured_model=%s protocol=%s (stream=%t)", endpoint, cfg.UpstreamModel(), cfg.Model, cfg.Protocol, stream)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if incompleteHTTPBody(resp.StatusCode, body, readErr) {
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
	resp, err := doSimpleResponsesRequestOnce(ctx, cfg, messages, client, options)
	if next, ok := llm.ConfigAfterThinkingRejection(cfg, err); ok {
		return doSimpleResponsesRequestOnce(ctx, next, messages, client, options)
	}
	return resp, err
}

func doSimpleResponsesRequestOnce(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client, options SimpleLLMRequestOptions) (*LLMSimpleResponse, error) {
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
	if incompleteHTTPBody(resp.StatusCode, body, readErr) {
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
