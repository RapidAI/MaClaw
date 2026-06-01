package main

// Responses API SSE streaming parser.
// Parallels doOpenAILLMRequestStream / doAnthropicLLMRequestStream for the
// OpenAI Responses API (POST /v1/responses) which uses named SSE events
// (event: + data: pairs) instead of bare data: lines.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
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

// classifyResponsesAPIHTTPError maps HTTP error status codes from the
// Responses API to user-friendly Chinese error messages.
func classifyResponsesAPIHTTPError(statusCode int, body []byte, endpoint, model, providerName string) string {
	providerDisplay := llmProviderDisplayName(providerName)
	if friendly := classifyHubErrorBody(body); friendly != "" {
		return friendly
	}
	switch statusCode {
	case 401:
		return fmt.Sprintf("%s OAuth token 已过期，请重新登录账号 (HTTP 401)", providerDisplay)
	case 429:
		return fmt.Sprintf("%s 订阅额度已达上限，请稍后再试 (HTTP 429)", providerDisplay)
	case 403:
		if strings.Contains(strings.ToLower(string(body)), "insufficient_quota") {
			return fmt.Sprintf("%s 订阅状态异常，请检查订阅是否有效 (HTTP 403)", providerDisplay)
		}
		return fmt.Sprintf("%s 拒绝访问 (HTTP 403)", providerDisplay)
	default:
		return fmt.Sprintf("%s Responses API 错误 (HTTP %d): %s [url=%s model=%s]", providerDisplay, statusCode, truncateLLMBody(body, 512), endpoint, model)
	}
}

// doResponsesAPILLMRequest sends a non-streaming request using the Responses API
// and returns the parsed response. Follows the same pattern as doOpenAILLMRequest.
func (h *IMMessageHandler) doResponsesAPILLMRequest(cfg corelib.MaclawLLMConfig, messages []interface{}, tools []map[string]interface{}, httpClient *http.Client) (*llm.Response, error) {
	req, data, endpoint, err := llm.NewResponsesAPIRequest(context.Background(), cfg, messages, llm.ResponsesAPIRequestOptions{
		Stream: false,
		Tools:  tools,
	})
	if err != nil {
		return nil, err
	}
	log.Printf("[LLM] POST %s model=%s wire_api=responses", endpoint, cfg.Model)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("[%s] %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if resp.StatusCode == http.StatusInternalServerError {
			return nil, dumpLLMContext(resp.StatusCode, classifyResponsesAPIHTTPError(resp.StatusCode, body, endpoint, cfg.Model, cfg.ProviderName), data, h.app.GetTempDir())
		}
		return nil, fmt.Errorf("%s", classifyResponsesAPIHTTPError(resp.StatusCode, body, endpoint, cfg.Model, cfg.ProviderName))
	}

	return llm.ParseNonStreamResponsesAPIResponse(resp)
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
	log.Printf("[LLM Stream] POST %s model=%s wire_api=responses", endpoint, cfg.Model)

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
		return nil, fmt.Errorf("HTTP 404: %s (endpoint=%s, model=%s, wire_api=responses)", string(body), endpoint, cfg.Model)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("%s", classifyResponsesAPIHTTPError(resp.StatusCode, body, endpoint, cfg.Model, cfg.ProviderName))
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("%s", classifyResponsesAPIHTTPError(resp.StatusCode, body, endpoint, cfg.Model, cfg.ProviderName))
	}

	// -----------------------------------------------------------------------
	// Token stream filters (same chain as OpenAI path)
	// -----------------------------------------------------------------------
	var filteredBufResp strings.Builder
	filteredOnTokenResp := func(delta string) {
		filteredBufResp.WriteString(delta)
		onToken(delta)
	}
	rpfResp := newRolePrefixStreamFilter(filteredOnTokenResp)
	repfResp := newRepetitionFilter(rpfResp.Write)
	tcf := newToolCallFilter(repfResp.Write)
	fcf := newFuncCallFilter(tcf.Callback())
	tf := newThinkFilter(fcf.Callback())

	// -----------------------------------------------------------------------
	// Accumulators
	// -----------------------------------------------------------------------
	var contentBuf strings.Builder
	itemAccums := make(map[int]*responsesItemAccum)
	var finishReason string
	var usage *llm.Usage

	// -----------------------------------------------------------------------
	// SSE idle timeout watchdog (same pattern as OpenAI/Anthropic paths)
	// -----------------------------------------------------------------------
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

	idleTimer := time.NewTimer(guiSSEIdleTimeout)
	defer idleTimer.Stop()
	sseTimedOut := false
	watchdogDone := make(chan struct{})
	go func() {
		select {
		case <-idleTimer.C:
			sseTimedOut = true
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
	// SSE event loop — Responses API uses named events (event: + data: pairs)
	// -----------------------------------------------------------------------
	var currentEventType string
	firstSSEWaitStartedAt := time.Now()

	for scanner.Scan() {
		idleTimer.Reset(guiSSEIdleTimeout)
		line := scanner.Text()

		// Track event type from "event:" lines.
		if strings.HasPrefix(line, "event:") {
			currentEventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}

		// Process "data:" lines.
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		if metrics != nil && metrics.FirstSSEWaitNanos == 0 {
			metrics.FirstSSEWaitNanos = time.Since(firstSSEWaitStartedAt).Nanoseconds()
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}

		evtType := currentEventType
		currentEventType = "" // reset after consuming

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
					return nil, fmt.Errorf("tool arguments too large for %s: %d bytes exceeds limit %d", toolName, acc.args.Len(), guiMaxToolArgumentsBytes)
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
			// No additional action needed; items are finalized when building
			// the response below. The accumulator already has all data.

		case responsesEventCompleted:
			if eventUsage := llm.ExtractResponsesAPIUsageFromEventPayload([]byte(payload)); eventUsage != nil {
				usage = eventUsage
			}

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
			return nil, fmt.Errorf("Responses API error: %s (code=%s)", errMsg, failed.Response.Error.Code)

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
		if len(itemAccums) == 0 && contentBuf.Len() == 0 {
			return nil, fmt.Errorf("SSE stream read error: %w", err)
		}
	}
	if sseTimedOut && contentBuf.Len() == 0 && len(itemAccums) == 0 {
		if metrics != nil {
			metrics.IdleTimeoutCount++
		}
		return nil, fmt.Errorf("SSE stream idle timeout (%v): no data received from %s", guiSSEIdleTimeout, endpoint)
	}

	// -----------------------------------------------------------------------
	// Flush filters and build final response
	// -----------------------------------------------------------------------
	tf.Flush()
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
		Role:    "assistant",
		Content: content,
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

	// Fallback: parse XML tool calls from content (same as OpenAI path).
	if len(msg.ToolCalls) == 0 {
		if xmlCalls := parseXMLContentToolCalls(contentBuf.String()); len(xmlCalls) > 0 {
			msg.ToolCalls = append(msg.ToolCalls, xmlCalls...)
			finishReason = "tool_calls"
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
	finishReason, truncatedTools := filterTruncatedToolCalls(&msg, finishReason)

	return &llm.Response{
		Choices: []llm.Choice{{Message: msg, FinishReason: finishReason, TruncatedToolNames: truncatedTools}},
		Usage:   usage,
	}, nil
}
