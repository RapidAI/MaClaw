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

	"github.com/RapidAI/CodeClaw/corelib/freeproxy"
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
	if strings.Contains(u, "chatgpt.com") {
		return u + "/codex/responses"
	}
	return u + "/responses"
}

// buildResponsesWSFrame constructs the JSON frame for a response.create
// WebSocket message, reusing the existing conversion functions.
func buildResponsesWSFrame(
	cfg MaclawLLMConfig,
	messages []interface{},
	tools []map[string]interface{},
) ([]byte, error) {
	converted := llm.ConvertToResponsesInput(messages)
	input := converted.Input
	if input == nil {
		input = []interface{}{}
	}
	frame := map[string]interface{}{
		"type":  "response.create",
		"model": cfg.Model,
		"input": input,
		"store": false,
	}
	if converted.Instructions != "" {
		frame["instructions"] = converted.Instructions
	}
	if convTools := llm.ConvertToResponsesTools(tools); len(convTools) > 0 {
		frame["tools"] = convTools
	}
	return json.Marshal(frame)
}

// doResponsesWSLLMRequestStream streams an LLM response over WebSocket using
// the OpenAI Responses API WebSocket transport. The event types and accumulator
// logic mirror the SSE path in doResponsesAPILLMRequestStream.
func (h *IMMessageHandler) doResponsesWSLLMRequestStream(
	reqCtx context.Context,
	cfg MaclawLLMConfig,
	messages []interface{},
	tools []map[string]interface{},
	httpClient *http.Client,
	onToken TokenCallback,
	metrics *llmStreamMetrics,
) (*llmResponse, error) {
	// -------------------------------------------------------------------
	// 1. Construct WebSocket URL
	// -------------------------------------------------------------------
	wsURL := responsesWSEndpoint(cfg.URL)
	log.Printf("[LLM Stream] WS %s model=%s wire_api=responses-ws", wsURL, cfg.Model)

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
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+cfg.Key)
	headers.Set("User-Agent", cfg.UserAgent())
	headers.Set("originator", "codex_cli_rs")
	// Codex subscription headers for chatgpt.com/backend-api
	if strings.Contains(cfg.URL, "chatgpt.com") {
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
			return nil, fmt.Errorf("%s", classifyResponsesAPIHTTPError(resp.StatusCode, body, wsURL, cfg.Model))
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
	tcf := newToolCallFilter(onToken)
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

		switch baseFrame.Type {
		case "response.output_item.added":
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
			if acc.itemType == "function_call" {
				if cid, _ := added.Item["call_id"].(string); cid != "" {
					acc.callID = cid
				}
				if n, _ := added.Item["name"].(string); n != "" {
					acc.name = n
				}
			}
			itemAccums[added.OutputIndex] = acc

		case "response.output_text.delta":
			var td struct {
				Delta string `json:"delta"`
			}
			if json.Unmarshal(msgData, &td) != nil {
				continue
			}
			if td.Delta != "" {
				contentBuf.WriteString(td.Delta)
				tf.Write(td.Delta)
			}

		case "response.function_call_arguments.delta":
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

		case "response.function_call_arguments.done":
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

		case "response.output_item.done":
			// No-op: items finalized when building the response below.

		case "response.completed":
			var completed struct {
				Response struct {
					Usage *struct {
						InputTokens  int `json:"input_tokens"`
						OutputTokens int `json:"output_tokens"`
						TotalTokens  int `json:"total_tokens"`
					} `json:"usage"`
				} `json:"response"`
			}
			if json.Unmarshal(msgData, &completed) != nil {
				continue
			}
			if completed.Response.Usage != nil {
				u := completed.Response.Usage
				usage = &llm.Usage{
					InputTokens:      u.InputTokens,
					OutputTokens:     u.OutputTokens,
					TotalTokens:      u.TotalTokens,
					PromptTokens:     u.InputTokens,
					CompletionTokens: u.OutputTokens,
				}
			}
			goto postLoop

		case "response.failed":
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

		case "response.incomplete":
			finishReason = "length"
			goto postLoop

		case "error":
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

	content := stripXMLToolCalls(stripFunctionCalls(stripThinkTags(contentBuf.String())))
	msg := llmMessage{
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
			if !ok || acc.itemType != "function_call" {
				continue
			}
			msg.ToolCalls = append(msg.ToolCalls, llmToolCall{
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

	// Fallback: parse XML tool calls from content (same as SSE path).
	if len(msg.ToolCalls) == 0 {
		if xmlCalls := freeproxy.ParseXMLToolCalls(contentBuf.String()); len(xmlCalls) > 0 {
			for _, xc := range xmlCalls {
				msg.ToolCalls = append(msg.ToolCalls, llmToolCall{
					ID: xc.ID, Type: xc.Type,
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name: xc.Function.Name, Arguments: xc.Function.Arguments,
					},
				})
			}
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

	return &llmResponse{
		Choices: []llmChoice{{Message: msg, FinishReason: finishReason}},
		Usage:   usage,
	}, nil
}
