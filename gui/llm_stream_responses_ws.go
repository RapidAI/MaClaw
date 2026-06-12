package main

// Responses API WebSocket streaming transport.
// Parallels doResponsesAPILLMRequestStream for the OpenAI Responses API
// but uses WebSocket (wss://) instead of HTTP+SSE.

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/oauth"
)

// responsesWSEndpoint converts an HTTP base URL to a WebSocket URL for the
// Responses API WebSocket mode.
// For chatgpt.com/backend-api → wss://chatgpt.com/backend-api/codex/responses
// For api.openai.com/v1 → wss://api.openai.com/v1/responses
func responsesWSEndpoint(baseURL string) string {
	u := strings.TrimRight(baseURL, "/")
	u = strings.Replace(u, "https://", "wss://", 1)
	u = strings.Replace(u, "http://", "ws://", 1)
	if llm.IsCodexSubscriptionEndpoint(u) {
		if strings.HasSuffix(u, "/codex/responses") {
			return u
		}
		return u + "/codex/responses"
	}
	if strings.HasSuffix(u, "/responses") {
		return u
	}
	if responsesWSEndpointHasVersionSuffix(u) {
		return u + "/responses"
	}
	return u + "/v1/responses"
}

func responsesWSEndpointHasVersionSuffix(endpoint string) bool {
	lastSlash := strings.LastIndex(endpoint, "/")
	if lastSlash < 0 || lastSlash == len(endpoint)-1 {
		return false
	}
	segment := strings.ToLower(endpoint[lastSlash+1:])
	if len(segment) < 2 || segment[0] != 'v' {
		return false
	}
	for _, r := range segment[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// buildResponsesWSFrame constructs the JSON frame for a response.create
// WebSocket message, reusing the existing conversion functions.
func buildResponsesWSFrame(
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	tools []map[string]interface{},
) ([]byte, error) {
	if cfg.NeedsConservativeOpenAICompatSanitization() {
		messages = llm.SanitizeConservativeOpenAICompatMessages(messages)
	} else {
		messages = llm.SanitizeOpenAICompatRequestMessages(messages, true)
	}
	converted := llm.ConvertToResponsesInput(messages)
	input := converted.Input
	if input == nil {
		input = []interface{}{}
	}
	frame := map[string]interface{}{
		"type":  "response.create",
		"model": cfg.UpstreamModel(),
		"input": input,
	}
	if !cfg.NeedsConservativeOpenAICompatSanitization() {
		frame["store"] = false
	}
	if converted.Instructions != "" {
		frame["instructions"] = converted.Instructions
	}
	toolsInput := llm.SanitizeOpenAIChatToolsForSDK(tools)
	if cfg.NeedsConservativeOpenAICompatSanitization() {
		toolsInput = corelib.SanitizeCodeGenOpenAIChatTools(toolsInput)
	}
	if convTools := llm.ConvertToResponsesTools(toolsInput); len(convTools) > 0 {
		frame["tools"] = convTools
	}
	return json.Marshal(frame)
}

func buildResponsesWSHeaders(cfg corelib.MaclawLLMConfig, wsURL string) http.Header {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+cfg.Key)
	headers.Set("User-Agent", cfg.UserAgent())
	headers.Set("originator", "codex_cli_rs")
	if corelib.IsCodeGenURL(wsURL) {
		headers.Set(corelib.CodeGenClientNameHeader, corelib.NormalizeCodeGenClientName(cfg.UserAgent()))
	}
	return headers
}

// doResponsesWSLLMRequestStream streams an LLM response over WebSocket using
// the OpenAI Responses API WebSocket transport. The event types and accumulator
// logic mirror the SSE path in doResponsesAPILLMRequestStream.
func (h *IMMessageHandler) doResponsesWSLLMRequestStream(
	reqCtx context.Context,
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	tools []map[string]interface{},
	httpClient *http.Client,
	onToken llm.TokenCallback,
	metrics *llmStreamMetrics,
) (*llm.Response, error) {
	// -------------------------------------------------------------------
	// 1. Construct WebSocket URL
	// -------------------------------------------------------------------
	wsURL := responsesWSEndpoint(cfg.URL)
	upstreamModel := cfg.UpstreamModel()
	log.Printf("[LLM Stream] WS %s model=%s configured_model=%s wire_api=responses-ws", wsURL, upstreamModel, cfg.Model)

	// -------------------------------------------------------------------
	// 2. Extract TLS config from httpClient transport if available
	// -------------------------------------------------------------------
	var tlsConfig *tls.Config
	if t, ok := httpClient.Transport.(*http.Transport); ok && t.TLSClientConfig != nil {
		tlsConfig = t.TLSClientConfig
	}

	// -------------------------------------------------------------------
	// 3. Create dialer and set request headers
	// -------------------------------------------------------------------
	dialer := websocket.Dialer{
		HandshakeTimeout: time.Duration(cfg.EffectiveTimeoutSec()) * time.Second,
		TLSClientConfig:  tlsConfig,
	}
	headers := buildResponsesWSHeaders(cfg, wsURL)
	// Codex subscription headers for chatgpt.com/backend-api
	if llm.IsCodexSubscriptionEndpoint(cfg.URL) {
		headers.Set("OpenAI-Beta", "responses_websockets=2026-02-06")
		if accountID, _ := oauth.ExtractAccountIDFromJWT(cfg.Key); accountID != "" {
			headers.Set("chatgpt-account-id", accountID)
		}
	}

	// -------------------------------------------------------------------
	// 4. Dial WebSocket
	// -------------------------------------------------------------------
	requestBuildStartedAt := time.Now()
	conn, resp, err := dialer.DialContext(reqCtx, wsURL, headers)
	if metrics != nil {
		metrics.HTTPDoNanos += time.Since(requestBuildStartedAt).Nanoseconds()
	}
	if err != nil {
		if resp != nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			return nil, fmt.Errorf("%s", classifyResponsesAPIHTTPError(resp.StatusCode, body, wsURL, upstreamModel, cfg.ProviderName))
		}
		return nil, fmt.Errorf("WebSocket dial failed: %w [url=%s]", err, wsURL)
	}
	defer conn.Close()

	// -------------------------------------------------------------------
	// 5. Build and send response.create frame
	// -------------------------------------------------------------------
	frameData, err := buildResponsesWSFrame(cfg, messages, tools)
	if err != nil {
		return nil, fmt.Errorf("failed to build response.create frame: %w", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, frameData); err != nil {
		return nil, fmt.Errorf("failed to send response.create frame: %w", err)
	}

	// -------------------------------------------------------------------
	// 6. Token stream filters (same chain as SSE path)
	// -------------------------------------------------------------------
	var filteredBufWS strings.Builder
	filteredOnTokenWS := func(delta string) {
		filteredBufWS.WriteString(delta)
		onToken(delta)
	}
	rpfWS := newRolePrefixStreamFilter(filteredOnTokenWS)
	repfWS := newRepetitionFilter(rpfWS.Write)
	tcf := newToolCallFilter(repfWS.Write)
	fcf := newFuncCallFilter(tcf.Callback())
	tf := newThinkFilter(fcf.Callback())

	// -------------------------------------------------------------------
	// 7. Accumulators
	// -------------------------------------------------------------------
	var contentBuf strings.Builder
	itemAccums := make(map[int]*responsesItemAccum)
	var finishReason string
	var usage *llm.Usage

	// -------------------------------------------------------------------
	// 8. Idle timeout watchdog
	// -------------------------------------------------------------------
	idleTimer := time.NewTimer(guiSSEIdleTimeout)
	defer idleTimer.Stop()
	var wsTimedOut atomic.Bool
	watchdogDone := make(chan struct{})
	go func() {
		select {
		case <-idleTimer.C:
			wsTimedOut.Store(true)
			if metrics != nil {
				metrics.IdleTimeoutAfterToken = !metrics.FirstTokenAt.IsZero()
			}
			log.Printf("[LLM Stream] Responses API WS idle timeout (%v) — closing connection", guiSSEIdleTimeout)
			conn.Close()
		case <-watchdogDone:
		case <-reqCtx.Done():
			conn.Close()
		}
	}()
	defer close(watchdogDone)

	// -------------------------------------------------------------------
	// 9. Ping goroutine — keep connection alive through proxies
	// -------------------------------------------------------------------
	pingDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second))
			case <-pingDone:
				return
			}
		}
	}()
	defer close(pingDone)

	// -------------------------------------------------------------------
	// 10. Pong handler — reset idle timer on pong
	// -------------------------------------------------------------------
	conn.SetPongHandler(func(string) error {
		idleTimer.Reset(guiSSEIdleTimeout)
		return nil
	})

	// -------------------------------------------------------------------
	// 11. Read JSON frames loop
	// -------------------------------------------------------------------
	firstFrameWaitStartedAt := time.Now()
	var baseFrame struct {
		Type string `json:"type"`
	}

	for {
		_, msgData, err := conn.ReadMessage()
		if err != nil {
			// Handle WebSocket close errors
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				break
			}
			if websocket.IsUnexpectedCloseError(err) {
				closeCode := -1
				if ce, ok := err.(*websocket.CloseError); ok {
					closeCode = ce.Code
				}
				if contentBuf.Len() > 0 || len(itemAccums) > 0 {
					break // return partial content
				}
				return nil, fmt.Errorf("WebSocket unexpected close (code=%d): %w", closeCode, err)
			}
			// Generic read error
			if contentBuf.Len() > 0 || len(itemAccums) > 0 {
				break // return partial content
			}
			if wsTimedOut.Load() {
				if metrics != nil {
					metrics.IdleTimeoutCount++
				}
				return nil, fmt.Errorf("WebSocket idle timeout (%v): no data received from %s", guiSSEIdleTimeout, wsURL)
			}
			return nil, fmt.Errorf("WebSocket read error: %w", err)
		}

		// Reset idle timer on every received frame
		idleTimer.Reset(guiSSEIdleTimeout)

		if metrics != nil && metrics.FirstSSEWaitNanos == 0 {
			metrics.FirstSSEWaitNanos = time.Since(firstFrameWaitStartedAt).Nanoseconds()
		}

		// Parse frame type
		if json.Unmarshal(msgData, &baseFrame) != nil {
			continue
		}

		switch normalizeResponsesEventType(baseFrame.Type) {
		case responsesEventOutputItemAdded:
			var added struct {
				OutputIndex int                    `json:"output_index"`
				Item        map[string]interface{} `json:"item"`
			}
			if json.Unmarshal(msgData, &added) != nil {
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
			if json.Unmarshal(msgData, &td) != nil {
				continue
			}
			if td.Delta != "" {
				contentBuf.WriteString(td.Delta)
				tf.Write(td.Delta)
				// Early-terminate if filters detected degeneration.
				if repfWS.Halted() || rpfWS.Halted() {
					finishReason = "stop"
					goto postLoop
				}
			}

		case responsesEventFunctionCallArgumentsDelta:
			var ad struct {
				Delta       string `json:"delta"`
				OutputIndex int    `json:"output_index"`
			}
			if json.Unmarshal(msgData, &ad) != nil {
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
			if json.Unmarshal(msgData, &done) != nil {
				continue
			}
			acc := itemAccums[done.OutputIndex]
			if acc != nil && done.Arguments != "" {
				// Authoritative final value — replace accumulated deltas.
				acc.args.Reset()
				acc.args.WriteString(done.Arguments)
			}

		case responsesEventOutputItemDone:
			// No-op: items finalized when building the response below.

		case responsesEventCompleted:
			if eventUsage := llm.ExtractResponsesAPIUsageFromEventPayload(msgData); eventUsage != nil {
				usage = eventUsage
			}
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
			if json.Unmarshal(msgData, &failed) != nil {
				continue
			}
			errMsg := failed.Response.Error.Message
			if errMsg == "" {
				errMsg = "unknown error"
			}
			return nil, fmt.Errorf("Responses API error: %s (code=%s)", errMsg, failed.Response.Error.Code)

		case responsesEventIncomplete:
			finishReason = "length"
			goto postLoop

		case responsesEventError:
			var errFrame struct {
				Status int `json:"status"`
				Error  struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal(msgData, &errFrame) != nil {
				continue
			}
			errMsg := errFrame.Error.Message
			if errMsg == "" {
				errMsg = "unknown error"
			}
			return nil, fmt.Errorf("Responses API WebSocket error: %s (code=%s, status=%d)", errMsg, errFrame.Error.Code, errFrame.Status)
		}
	}

postLoop:
	// -------------------------------------------------------------------
	// Post-loop: idle timeout with no content
	// -------------------------------------------------------------------
	if wsTimedOut.Load() && contentBuf.Len() == 0 && len(itemAccums) == 0 {
		if metrics != nil {
			metrics.IdleTimeoutCount++
		}
		return nil, fmt.Errorf("WebSocket idle timeout (%v): no data received from %s", guiSSEIdleTimeout, wsURL)
	}

	// -------------------------------------------------------------------
	// Flush filters and build final response
	// -------------------------------------------------------------------
	tf.Flush()
	fcf.Flush()
	tcf.Flush()
	repfWS.Flush()
	rpfWS.Flush()
	if repfWS.Halted() {
		log.Printf("[LLM Stream WS] repetition filter halted: suppressed %d runes", repfWS.SuppressedRunes())
	}
	if rpfWS.Halted() {
		log.Printf("[LLM Stream WS] role prefix filter halted: suppressed %d runes", rpfWS.SuppressedRunes())
	}

	content := stripXMLToolCalls(stripFunctionCalls(stripThinkTags(contentBuf.String())))
	if filtered := filteredBufWS.String(); filtered != "" {
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
	finishReason, truncatedTools := filterTruncatedToolCalls(&msg, finishReason)

	return &llm.Response{
		Choices: []llm.Choice{{Message: msg, FinishReason: finishReason, TruncatedToolNames: truncatedTools}},
		Usage:   usage,
	}, nil
}
