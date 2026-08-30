package main

// Responses API SSE streaming parser.
// Parallels doOpenAILLMRequestStream / doAnthropicLLMRequestStream for the
// OpenAI Responses API (POST /v1/responses) which uses named SSE events
// (event: + data: pairs) instead of bare data: lines.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// responsesItemAccum tracks per-output-item state during SSE streaming.
type responsesItemAccum struct {
	itemType string // "message" or "function_call"
	callID   string
	name     string
	args     strings.Builder
}

// classifyResponsesAPIHTTPError maps Responses API HTTP errors to user-facing text.
// Shared classification comes from corelib; this layer adds Responses-specific
// bare-status phrasing and attaches endpoint/model for debugging.
func classifyResponsesAPIHTTPError(statusCode int, body []byte, endpoint, model, providerName string) string {
	providerDisplay := llmProviderDisplayName(providerName)
	if friendly := classifyHubErrorBody(body); friendly != "" {
		return friendly
	}

	// Bare status with no structured body: Responses product-specific wording.
	if len(bytes.TrimSpace(body)) == 0 {
		switch statusCode {
		case http.StatusUnauthorized:
			return fmt.Sprintf("%s OAuth token 已过期，请重新登录账号 (HTTP 401)", providerDisplay)
		case http.StatusTooManyRequests:
			return fmt.Sprintf("%s 订阅额度已达上限，请稍后再试 (HTTP 429)", providerDisplay)
		case http.StatusForbidden:
			return fmt.Sprintf("%s 拒绝访问 (HTTP 403)", providerDisplay)
		}
	}
	if statusCode == http.StatusForbidden && strings.Contains(strings.ToLower(string(body)), "insufficient_quota") {
		return fmt.Sprintf("%s 订阅状态异常，请检查订阅是否有效 (HTTP 403)", providerDisplay)
	}

	base := llm.UserFacingHTTPStatusWithProvider(statusCode, body, providerDisplay)
	if base == "" {
		return fmt.Sprintf("%s Responses API 错误 (HTTP %d) [url=%s model=%s]", providerDisplay, statusCode, endpoint, model)
	}
	return fmt.Sprintf("%s [url=%s model=%s]", base, endpoint, model)
}

// doResponsesAPILLMRequest sends a non-streaming request using the Responses API
// and returns the parsed response. Follows the same pattern as doOpenAILLMRequest.
func (h *IMMessageHandler) doResponsesAPILLMRequest(cfg corelib.MaclawLLMConfig, messages []interface{}, tools []map[string]interface{}, httpClient *http.Client) (*llm.Response, error) {
	return h.doResponsesAPILLMRequestWithContext(context.Background(), cfg, messages, tools, httpClient)
}

func (h *IMMessageHandler) doResponsesAPILLMRequestWithContext(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, tools []map[string]interface{}, httpClient *http.Client) (*llm.Response, error) {
	ctx = llm.WithRequestTraceIfMissing(ctx, "im_responses")
	lease, trace, acquireErr := acquireLLMSchedulerLease(ctx)
	if acquireErr != nil {
		return nil, acquireErr
	}
	defer lease.Release()
	scheduledCtx, scheduledCancel := context.WithCancel(ctx)
	lease.SetCancel(scheduledCancel)
	defer scheduledCancel()

	req, data, endpoint, err := llm.NewResponsesAPIRequest(scheduledCtx, cfg, messages, llm.ResponsesAPIRequestOptions{
		Stream: false,
		Tools:  tools,
	})
	if err != nil {
		return nil, err
	}
	traceFields := llm.RequestTraceLogFields(req.Context())
	upstreamModel := cfg.UpstreamModel()
	log.Printf("[LLM] POST %s model=%s configured_model=%s wire_api=responses %s", endpoint, upstreamModel, cfg.Model, traceFields)

	startedAt := time.Now()
	resp, err := httpClient.Do(req)
	globalLLMScheduler.ObserveResult(trace, err)
	if err != nil {
		log.Printf("[LLM] done %s model=%s configured_model=%s wire_api=responses status=error elapsed=%s err=%v %s", endpoint, upstreamModel, cfg.Model, time.Since(startedAt).Round(time.Millisecond), err, traceFields)
		return nil, fmt.Errorf("[%s] %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		log.Printf("[LLM] done %s model=%s configured_model=%s wire_api=responses status=%d elapsed=%s body_len=%d %s", endpoint, upstreamModel, cfg.Model, resp.StatusCode, time.Since(startedAt).Round(time.Millisecond), len(body), traceFields)
		if resp.StatusCode == http.StatusInternalServerError {
			err := dumpLLMContext(resp.StatusCode, classifyResponsesAPIHTTPError(resp.StatusCode, body, endpoint, upstreamModel, cfg.ProviderName), data, h.getTempDir())
			globalLLMScheduler.ObserveResult(trace, err)
			return nil, err
		}
		err := fmt.Errorf("%s", classifyResponsesAPIHTTPError(resp.StatusCode, body, endpoint, upstreamModel, cfg.ProviderName))
		globalLLMScheduler.ObserveResult(trace, err)
		return nil, err
	}

	parsed, parseErr := llm.ParseNonStreamResponsesAPIResponse(resp)
	globalLLMScheduler.ObserveResult(trace, parseErr)
	log.Printf("[LLM] done %s model=%s configured_model=%s wire_api=responses status=%d elapsed=%s parse_err=%v %s", endpoint, upstreamModel, cfg.Model, resp.StatusCode, time.Since(startedAt).Round(time.Millisecond), parseErr, traceFields)
	return parsed, parseErr
}

func (h *IMMessageHandler) doResponsesAPILLMRequestStream(
	reqCtx context.Context,
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	tools []map[string]interface{},
	httpClient *http.Client,
	onToken llm.TokenCallback,
	metrics *llmStreamMetrics,
) (*llm.Response, error) {
	// -----------------------------------------------------------------------
	// Build HTTP request
	// -----------------------------------------------------------------------
	requestBuildStartedAt := time.Now()
	req, _, endpoint, err := llm.NewResponsesAPIRequest(reqCtx, cfg, messages, llm.ResponsesAPIRequestOptions{
		Stream: true,
		Tools:  tools,
	})
	if err != nil {
		return nil, err
	}
	if metrics != nil {
		metrics.RequestBuildNanos += time.Since(requestBuildStartedAt).Nanoseconds()
	}
	upstreamModel := cfg.UpstreamModel()
	log.Printf("[LLM Stream] POST %s model=%s configured_model=%s wire_api=responses %s", endpoint, upstreamModel, cfg.Model, llm.RequestTraceLogFields(reqCtx))

	httpDoStartedAt := time.Now()
	resp, err := httpClient.Do(req)
	if metrics != nil {
		metrics.HTTPDoNanos += time.Since(httpDoStartedAt).Nanoseconds()
	}
	if err != nil {
		return nil, fmt.Errorf("[%s] %w", endpoint, err)
	}
	defer resp.Body.Close()

	// -----------------------------------------------------------------------
	// HTTP error handling
	// -----------------------------------------------------------------------
	if resp.StatusCode == http.StatusNotFound {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("HTTP 404 (endpoint=%s, model=%s, wire_api=responses, body_len=%d)", endpoint, upstreamModel, len(body))
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("%s", classifyResponsesAPIHTTPError(resp.StatusCode, body, endpoint, upstreamModel, cfg.ProviderName))
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("%s", classifyResponsesAPIHTTPError(resp.StatusCode, body, endpoint, upstreamModel, cfg.ProviderName))
	}

	// -----------------------------------------------------------------------
	// Token stream filters (same chain as OpenAI path)
	// -----------------------------------------------------------------------
	var filteredBufResp strings.Builder
	filteredOnTokenResp := func(delta string) {
		filteredBufResp.WriteString(delta)
		if onToken != nil {
			onToken(delta)
		}
	}
	rpfResp := newRolePrefixStreamFilter(filteredOnTokenResp)
	repfResp := newRepetitionFilter(rpfResp.Write)
	tcf := newToolCallFilter(repfResp.Write)
	fcf := newFuncCallFilter(tcf.Callback())
	thinkReasoningCbResp := func(delta string) {
		if onToken != nil && delta != "" {
			onToken("\x01" + delta)
		}
	}
	reasoningRoleFilterResp := newRolePrefixStreamFilter(thinkReasoningCbResp)
	tf := newThinkFilterWithReasoning(fcf.Callback(), thinkReasoningCbResp)

	// -----------------------------------------------------------------------
	// Accumulators
	// -----------------------------------------------------------------------
	var contentBuf strings.Builder
	var reasoningBuf strings.Builder
	itemAccums := make(map[int]*responsesItemAccum)
	var finishReason string
	var usage *llm.Usage
	// streamErr is set by terminal in-stream error events so the post-loop
	// path can still return the partially accumulated content/reasoning
	// alongside the error instead of dropping it.
	var streamErr error

	// -----------------------------------------------------------------------
	// SSE idle timeout watchdog (same pattern as OpenAI/Anthropic paths)
	// -----------------------------------------------------------------------
	scanner := bufio.NewScanner(resp.Body)
	scanner.Split(scanResponsesSSEEvent)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

	idleTimer := time.NewTimer(guiSSEIdleTimeout)
	defer idleTimer.Stop()
	var sseTimedOut atomic.Bool
	watchdogDone := make(chan struct{})
	go func() {
		select {
		case <-idleTimer.C:
			sseTimedOut.Store(true)
			if metrics != nil {
				metrics.IdleTimeoutAfterToken = !metrics.FirstTokenAt.IsZero()
			}
			log.Printf("[LLM Stream] Responses API SSE idle timeout (%v) — aborting stalled request", guiSSEIdleTimeout)
			resp.Body.Close()
		case <-watchdogDone:
		case <-reqCtx.Done():
		}
	}()
	defer close(watchdogDone)

	// -----------------------------------------------------------------------
	// SSE event loop — Responses API uses named events (event: + data: pairs).
	// Scan complete SSE events rather than individual lines: data may legally
	// span several lines, and some compatible gateways omit event: and put the
	// type in the JSON payload instead.
	// -----------------------------------------------------------------------
	firstSSEWaitStartedAt := time.Now()

	for scanner.Scan() {
		idleTimer.Reset(guiSSEIdleTimeout)
		evtType, payload, ok := parseResponsesSSEEvent(scanner.Text())
		if !ok {
			continue
		}
		if metrics != nil && metrics.FirstSSEWaitNanos == 0 {
			metrics.FirstSSEWaitNanos = time.Since(firstSSEWaitStartedAt).Nanoseconds()
		}
		if payload == "[DONE]" {
			break
		}

		switch normalizeResponsesEventType(evtType) {
		case responsesEventOutputItemAdded:
			var added struct {
				OutputIndex int                    `json:"output_index"`
				Item        map[string]interface{} `json:"item"`
			}
			if json.Unmarshal([]byte(payload), &added) != nil {
				continue
			}
			acc := &responsesItemAccum{}
			if t, _ := added.Item["type"].(string); t != "" {
				acc.itemType = t
			}
			if normalizeResponsesOutputItemKind(acc.itemType) == responsesOutputItemFunctionCall {
				if cid, _ := added.Item["call_id"].(string); cid != "" {
					acc.callID = cid
				}
				if n, _ := added.Item["name"].(string); n != "" {
					acc.name = n
				}
			}
			itemAccums[added.OutputIndex] = acc

		case responsesEventOutputTextDelta:
			var td struct {
				Delta string `json:"delta"`
			}
			if json.Unmarshal([]byte(payload), &td) != nil {
				continue
			}
			if td.Delta != "" {
				contentBuf.WriteString(td.Delta)
				tf.Write(td.Delta)
				// Early-terminate if filters detected degeneration.
				if repfResp.Halted() || rpfResp.Halted() {
					finishReason = "stop"
					goto postLoop
				}
			}

		case responsesEventReasoningSummaryTextDelta, responsesEventReasoningTextDelta, responsesEventReasoningContentDelta, responsesEventReasoningDelta:
			var rd struct {
				Delta string `json:"delta"`
			}
			if json.Unmarshal([]byte(payload), &rd) != nil {
				continue
			}
			if rd.Delta != "" {
				reasoningBuf.WriteString(rd.Delta)
				reasoningRoleFilterResp.Write(rd.Delta)
			}

		case responsesEventReasoningSummaryPartAdded:
			// Some compatible providers send the whole summary for a part in
			// one shot instead of streaming reasoning_summary_text.delta
			// events. appendResponsesReasoningSummary deduplicates against any
			// already-streamed deltas for the same summary.
			var sp struct {
				Part struct {
					Text string `json:"text"`
				} `json:"part"`
			}
			if json.Unmarshal([]byte(payload), &sp) == nil {
				if emitted := appendResponsesReasoningSummary(&reasoningBuf, sp.Part.Text); emitted != "" {
					reasoningRoleFilterResp.Write(emitted)
				}
			}

		case responsesEventFunctionCallArgumentsDelta:
			var ad struct {
				Delta       string `json:"delta"`
				OutputIndex int    `json:"output_index"`
			}
			if json.Unmarshal([]byte(payload), &ad) != nil {
				continue
			}
			acc := itemAccums[ad.OutputIndex]
			if acc == nil {
				continue
			}
			if ad.Delta != "" {
				acc.args.WriteString(ad.Delta)
				if acc.args.Len() > guiMaxToolArgumentsBytes {
					toolName := acc.name
					if toolName == "" {
						toolName = fmt.Sprintf("function_call_%d", ad.OutputIndex)
					}
					// Keep the partial response so already-accumulated
					// content/reasoning is not lost with the error.
					streamErr = fmt.Errorf("tool arguments too large for %s: %d bytes exceeds limit %d", toolName, acc.args.Len(), guiMaxToolArgumentsBytes)
					goto postLoop
				}
			}

		case responsesEventFunctionCallArgumentsDone:
			var done struct {
				Arguments   string `json:"arguments"`
				OutputIndex int    `json:"output_index"`
			}
			if json.Unmarshal([]byte(payload), &done) != nil {
				continue
			}
			acc := itemAccums[done.OutputIndex]
			if acc != nil && done.Arguments != "" {
				// Authoritative final value — replace accumulated deltas.
				acc.args.Reset()
				acc.args.WriteString(done.Arguments)
			}

		case responsesEventOutputItemDone:
			// Some compatible Responses endpoints send a final reasoning item
			// but no reasoning delta events. Use its display-safe summary as a
			// fallback, without duplicating a streamed summary.
			var done struct {
				Item responsesAPIReasoningOutputItem `json:"item"`
			}
			if json.Unmarshal([]byte(payload), &done) == nil {
				if emitted := appendResponsesReasoningSummary(&reasoningBuf, done.Item.DisplaySummary()); emitted != "" {
					reasoningRoleFilterResp.Write(emitted)
				}
			}

		case responsesEventCompleted:
			if eventUsage := llm.ExtractResponsesAPIUsageFromEventPayload([]byte(payload)); eventUsage != nil {
				usage = eventUsage
			}
			for _, summary := range responsesCompletedReasoningSummaries([]byte(payload)) {
				if emitted := appendResponsesReasoningSummary(&reasoningBuf, summary); emitted != "" {
					reasoningRoleFilterResp.Write(emitted)
				}
			}
			log.Printf("[LLM Stream Responses] response.completed received; closing SSE stream without waiting for trailing DONE")
			goto postLoop

		case responsesEventFailed:
			var failed struct {
				Response struct {
					Error struct {
						Message string `json:"message"`
						Code    string `json:"code"`
					} `json:"error"`
				} `json:"response"`
			}
			if json.Unmarshal([]byte(payload), &failed) != nil {
				continue
			}
			errMsg := failed.Response.Error.Message
			if errMsg == "" {
				errMsg = "unknown error"
			}
			// Keep the partial response so already-accumulated
			// content/reasoning is not lost with the error.
			streamErr = fmt.Errorf("Responses API error: %s (code=%s)", errMsg, failed.Response.Error.Code)
			goto postLoop

		case responsesEventIncomplete:
			// Return whatever partial content we have with finish reason "length".
			finishReason = "length"
			// Stream is done after incomplete event.
			goto postLoop
		}
	}

postLoop:
	// -----------------------------------------------------------------------
	// Post-loop error checks
	// -----------------------------------------------------------------------
	if err := scanner.Err(); err != nil {
		if len(itemAccums) == 0 && contentBuf.Len() == 0 && reasoningBuf.Len() == 0 {
			return nil, fmt.Errorf("SSE stream read error: %w", err)
		}
	}
	if sseTimedOut.Load() && contentBuf.Len() == 0 && reasoningBuf.Len() == 0 && len(itemAccums) == 0 {
		if metrics != nil {
			metrics.IdleTimeoutCount++
		}
		return nil, fmt.Errorf("SSE stream idle timeout (%v): no data received from %s", guiSSEIdleTimeout, endpoint)
	}

	// -----------------------------------------------------------------------
	// Flush filters and build final response
	// -----------------------------------------------------------------------
	tf.Flush()
	reasoningRoleFilterResp.Flush()
	fcf.Flush()
	tcf.Flush()
	repfResp.Flush()
	rpfResp.Flush()
	if repfResp.Halted() {
		log.Printf("[LLM Stream Responses] repetition filter halted: suppressed %d runes", repfResp.SuppressedRunes())
	}
	if rpfResp.Halted() {
		log.Printf("[LLM Stream Responses] role prefix filter halted: suppressed %d runes", rpfResp.SuppressedRunes())
	}

	content := stripXMLToolCalls(stripFunctionCalls(stripThinkTags(contentBuf.String())))
	if filtered := filteredBufResp.String(); filtered != "" {
		content = stripXMLToolCalls(filtered)
	}
	msg := llm.Message{
		Role:             "assistant",
		Content:          content,
		ReasoningContent: stripRolePrefixReasoningForDisplay(reasoningBuf.String()),
	}

	// Build tool calls from function_call accumulators (ordered by index).
	if len(itemAccums) > 0 {
		maxIdx := 0
		for idx := range itemAccums {
			if idx > maxIdx {
				maxIdx = idx
			}
		}
		for i := 0; i <= maxIdx; i++ {
			acc, ok := itemAccums[i]
			if !ok || normalizeResponsesOutputItemKind(acc.itemType) != responsesOutputItemFunctionCall {
				continue
			}
			msg.ToolCalls = append(msg.ToolCalls, llm.ToolCall{
				ID:   acc.callID,
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name:      acc.name,
					Arguments: acc.args.String(),
				},
			})
		}
	}

	// Fallback: parse tool calls emitted in content by compatible providers.
	if len(msg.ToolCalls) == 0 {
		if xmlCalls, malformed := parseXMLContentToolCallsDetailed(contentBuf.String()); len(xmlCalls) > 0 {
			msg.ToolCalls = append(msg.ToolCalls, xmlCalls...)
			msg.Content = ""
			finishReason = "tool_calls"
		} else if malformed {
			msg.Content = llm.MalformedContentToolCallErrorMsg
			finishReason = "stop"
		}
	}

	if finishReason == "" {
		if len(msg.ToolCalls) > 0 {
			finishReason = "tool_calls"
		} else {
			finishReason = "stop"
		}
	}

	// Detect and filter truncated tool calls caused by output token limit.
	finishReason, truncatedTools, truncatedToolArgs := filterTruncatedToolCalls(&msg, finishReason)

	return &llm.Response{
		Choices: []llm.Choice{{Message: msg, FinishReason: finishReason, TruncatedToolNames: truncatedTools, TruncatedToolArgs: truncatedToolArgs}},
		Usage:   usage,
	}, streamErr
}
