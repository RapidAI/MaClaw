package main

// Responses API WebSocket streaming transport.
// Parallels doResponsesAPILLMRequestStream for the OpenAI Responses API
// but uses WebSocket (wss://) instead of HTTP+SSE.

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/oauth"
)

// responsesWSRequestChannel is one live Responses WebSocket reservation. Its
// connection ID is generated only after a successful dial and is retained with
// that concrete socket until Close. It is deliberately not derived from a
// URL, model/configuration field, loop ID, request ID, or provider payload.
//
// The channel performs exactly one response.create exchange. A caller that
// needs a retry/fallback/reconnect must create a new channel and therefore a
// new request surface before sending another request.
type responsesWSRequestChannel struct {
	conn         *websocket.Conn
	wsURL        string
	connectionID string
	closeOnce    sync.Once
	useMu        sync.Mutex
	used         bool
}

func openResponsesWSRequestChannel(ctx context.Context, cfg corelib.MaclawLLMConfig, httpClient *http.Client, metrics *llmStreamMetrics) (*responsesWSRequestChannel, error) {
	wsURL := responsesWSEndpoint(cfg.URL)
	upstreamModel := cfg.UpstreamModel()
	log.Printf("[LLM Stream] WS %s model=%s configured_model=%s wire_api=responses-ws", wsURL, upstreamModel, cfg.Model)

	var tlsConfig *tls.Config
	if httpClient != nil {
		if transport, ok := httpClient.Transport.(*http.Transport); ok && transport.TLSClientConfig != nil {
			tlsConfig = transport.TLSClientConfig
		}
	}
	dialer := websocket.Dialer{
		HandshakeTimeout: time.Duration(cfg.EffectiveTimeoutSec()) * time.Second,
		TLSClientConfig:  tlsConfig,
	}
	headers := buildResponsesWSHeaders(cfg, wsURL)
	if llm.IsCodexSubscriptionEndpoint(cfg.URL) {
		headers.Set("OpenAI-Beta", "responses_websockets=2026-02-06")
		if accountID, _ := oauth.ExtractAccountIDFromJWT(cfg.Key); accountID != "" {
			headers.Set("chatgpt-account-id", accountID)
		}
	}

	startedAt := time.Now()
	conn, response, err := dialer.DialContext(ctx, wsURL, headers)
	if metrics != nil {
		metrics.HTTPDoNanos += time.Since(startedAt).Nanoseconds()
	}
	if err != nil {
		if response != nil {
			body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
			response.Body.Close()
			return nil, fmt.Errorf("%s", classifyResponsesAPIHTTPError(response.StatusCode, body, wsURL, upstreamModel, cfg.ProviderName))
		}
		return nil, fmt.Errorf("WebSocket dial failed: %w [url=%s]", err, wsURL)
	}
	connectionID, err := newResponsesWSConnectionID()
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &responsesWSRequestChannel{conn: conn, wsURL: wsURL, connectionID: connectionID}, nil
}

func newResponsesWSConnectionID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate responses websocket connection id: %w", err)
	}
	return "responses-ws:" + hex.EncodeToString(value[:]), nil
}

func (c *responsesWSRequestChannel) Close() {
	if c == nil {
		return
	}
	c.closeOnce.Do(func() {
		if c.conn != nil {
			_ = c.conn.Close()
		}
	})
}

// responsesWSEndpoint converts an HTTP base URL to a WebSocket URL for the
// Responses API WebSocket mode.
// For chatgpt.com/backend-api/codex → wss://chatgpt.com/backend-api/codex/responses
// For api.openai.com/v1 → wss://api.openai.com/v1/responses
func responsesWSEndpoint(baseURL string) string {
	u := strings.TrimRight(baseURL, "/")
	u = strings.Replace(u, "https://", "wss://", 1)
	u = strings.Replace(u, "http://", "ws://", 1)
	if llm.IsCodexSubscriptionEndpoint(u) {
		if strings.HasSuffix(u, "/codex/responses") {
			return u
		}
		if strings.HasSuffix(u, "/codex") {
			return u + "/responses"
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
	policies ...agent.ToolSurfaceInvocationPolicy,
) ([]byte, error) {
	policy := agent.DefaultToolSurfaceInvocationPolicy(agent.ToolSurfaceEnvelopeResponses)
	if len(policies) > 0 {
		policy = policies[0]
	}
	normalizedPolicy, err := agent.NormalizeToolSurfaceInvocationPolicy(policy)
	if err != nil {
		return nil, fmt.Errorf("invalid responses websocket invocation policy: %w", err)
	}
	if normalizedPolicy.Envelope != agent.ToolSurfaceEnvelopeResponses {
		return nil, fmt.Errorf("responses websocket invocation policy envelope %q is not responses", normalizedPolicy.Envelope)
	}
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
	convTools := llm.ConvertToResponsesTools(toolsInput)
	if len(convTools) > 0 {
		frame["tools"] = convTools
	} else {
		// An empty tool surface is an explicit replacement, never an omitted
		// field whose meaning a stateful compatible provider may inherit.
		frame["tools"] = []map[string]interface{}{}
	}
	policyFields, err := agent.ToolSurfaceInvocationPolicyWireFields(normalizedPolicy)
	if err != nil {
		return nil, fmt.Errorf("project responses websocket invocation policy: %w", err)
	}
	for key, value := range policyFields {
		frame[key] = value
	}
	// Ensure max_output_tokens is set (same logic as BuildResponsesAPIRequestData).
	// Skip for Codex subscription endpoints (chatgpt.com/backend-api) — they don't support this parameter.
	if !llm.IsCodexSubscriptionEndpoint(cfg.URL) {
		shouldInject := true
		limit := cfg.EffectiveMaxOutputTokens()
		cacheKey := strings.ToLower(strings.TrimSpace(cfg.Model))
		if cached, ok := llm.LoadMaxOutputTokensCache(cacheKey); ok {
			if cached == 0 {
				shouldInject = false
			} else if cached > 0 && cached < limit {
				limit = cached
			}
		}
		if shouldInject {
			frame["max_output_tokens"] = limit
		}
	}
	// The WebSocket Responses transport has its own request builder, so it must
	// apply the same provider-native reasoning mapping as the HTTP Responses
	// path. Without this, an explicit global setting was silently ignored for
	// DeepSeek and other compatible providers using responses-ws.
	corelib.ApplyReasoningControls(cfg, frame, corelib.ReasoningAPIResponses)
	return json.Marshal(frame)
}

// verifyResponsesWSFrameToolSurface is deliberately applied after the frame
// builder's provider-specific conversion. It proves the actual `tools` field
// about to be written on this socket is a complete replacement for the
// request-owned input surface.
func verifyResponsesWSFrameToolSurface(frameData []byte, tools []map[string]interface{}, expectedPolicy agent.ToolSurfaceInvocationPolicy, evidence ...agent.ToolSurfacePlanEvidence) (agent.ToolSurfaceReceipt, error) {
	planEvidence := agent.ToolSurfacePlanEvidence{}
	if len(evidence) > 0 {
		planEvidence = evidence[0]
	}
	var frame map[string]interface{}
	if err := json.Unmarshal(frameData, &frame); err != nil {
		// Keep malformed-frame failures on the same request-owned manifest
		// projection as every later WS receipt. Returning an empty receipt would
		// make a manifest/terminal audit chain impossible to reconcile even
		// though the host had already rendered the surface and plan evidence.
		return agent.VerifyToolSurfaceRequestPayloadWithAuditEvidence(tools, nil, expectedPolicy, planEvidence)
	}
	return agent.VerifyToolSurfaceRequestPayloadWithAuditEvidence(tools, frame, expectedPolicy, planEvidence)
}

func buildResponsesWSHeaders(cfg corelib.MaclawLLMConfig, wsURL string) http.Header {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+cfg.Key)
	headers.Set("User-Agent", cfg.UserAgent())
	headers.Set("originator", "codex_cli_rs")
	if corelib.IsCodeGenURL(wsURL) {
		headers.Set(corelib.CodeGenClientNameHeader, corelib.NormalizeCodeGenClientName(cfg.UserAgent()))
	}
	llm.ApplyWorkloadHintHeaderMap(headers, cfg)
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
	channel, err := openResponsesWSRequestChannel(reqCtx, cfg, httpClient, metrics)
	if err != nil {
		return nil, err
	}
	defer channel.Close()
	return h.streamResponsesWSRequestChannel(reqCtx, cfg, messages, tools, onToken, metrics, channel)
}

// streamResponsesWSRequestChannel sends one response.create frame and consumes
// its matching event stream through an already-reserved live socket.
func (h *IMMessageHandler) streamResponsesWSRequestChannel(
	reqCtx context.Context,
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	tools []map[string]interface{},
	onToken llm.TokenCallback,
	metrics *llmStreamMetrics,
	channel *responsesWSRequestChannel,
) (*llm.Response, error) {
	dispatch, err := h.streamResponsesWSRequestChannelVerified(reqCtx, cfg, messages, tools, onToken, metrics, channel, agent.ToolSurfacePlanEvidence{}, agent.DefaultToolSurfaceInvocationPolicy(agent.ToolSurfaceEnvelopeResponses))
	return dispatch.Response, err
}

// streamResponsesWSRequestChannelVerified keeps payload verification evidence
// in the same return value as the response read from this live socket. A
// correlation-bound request owner must consume this form before it binds a
// provider response ID.
func (h *IMMessageHandler) streamResponsesWSRequestChannelVerified(
	reqCtx context.Context,
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	tools []map[string]interface{},
	onToken llm.TokenCallback,
	metrics *llmStreamMetrics,
	channel *responsesWSRequestChannel,
	evidence agent.ToolSurfacePlanEvidence,
	policy agent.ToolSurfaceInvocationPolicy,
) (agent.VerifiedToolSurfaceDispatch, error) {
	if channel == nil || channel.conn == nil || strings.TrimSpace(channel.connectionID) == "" {
		return agent.VerifiedToolSurfaceDispatch{}, fmt.Errorf("Responses API WebSocket request channel is unavailable")
	}
	channel.useMu.Lock()
	if channel.used {
		channel.useMu.Unlock()
		return agent.VerifiedToolSurfaceDispatch{}, fmt.Errorf("Responses API WebSocket request channel already used")
	}
	channel.used = true
	channel.useMu.Unlock()
	conn := channel.conn
	wsURL := channel.wsURL

	// -------------------------------------------------------------------
	// Build and send response.create frame
	// -------------------------------------------------------------------
	frameData, err := buildResponsesWSFrame(cfg, messages, tools, policy)
	if err != nil {
		return agent.VerifiedToolSurfaceDispatch{}, fmt.Errorf("failed to build response.create frame: %w", err)
	}
	receipt, err := verifyResponsesWSFrameToolSurface(frameData, tools, policy, evidence)
	if err != nil {
		return agent.VerifiedToolSurfaceDispatch{Receipt: receipt}, err
	}
	if err := conn.WriteMessage(websocket.TextMessage, frameData); err != nil {
		receipt.Handoff = agent.ToolSurfaceHandoffAmbiguous
		return agent.VerifiedToolSurfaceDispatch{Receipt: receipt}, fmt.Errorf("failed to send response.create frame: %w", err)
	}
	receipt.Handoff = agent.ToolSurfaceHandoffStarted

	// -------------------------------------------------------------------
	// 6. Token stream filters (same chain as SSE path)
	// -------------------------------------------------------------------
	var filteredBufWS strings.Builder
	filteredOnTokenWS := func(delta string) {
		filteredBufWS.WriteString(delta)
		if onToken != nil {
			onToken(delta)
		}
	}
	rpfWS := newRolePrefixStreamFilter(filteredOnTokenWS)
	repfWS := newRepetitionFilter(rpfWS.Write)
	tcf := newToolCallFilter(repfWS.Write)
	fcf := newFuncCallFilter(tcf.Callback())
	thinkReasoningCbWS := func(delta string) {
		if onToken != nil && delta != "" {
			onToken("\x01" + delta)
		}
	}
	reasoningRoleFilterWS := newRolePrefixStreamFilter(thinkReasoningCbWS)
	tf := newThinkFilterWithReasoning(fcf.Callback(), thinkReasoningCbWS)

	// -------------------------------------------------------------------
	// 7. Accumulators
	// -------------------------------------------------------------------
	var contentBuf strings.Builder
	var reasoningBuf strings.Builder
	itemAccums := make(map[int]*responsesItemAccum)
	var finishReason string
	var usage *llm.Usage
	// Preserve only provider-issued response identity observed in the Responses
	// lifecycle envelope. This is parser evidence, not a WebSocket connection
	// identity and not a substitute for the Coding S1-C adapter lifecycle.
	// Freezing the first non-empty value prevents tool calls accumulated from
	// one response from later being attributed to another response on a malformed
	// or multiplexed-compatible stream.
	responseID := ""

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
				if contentBuf.Len() > 0 || reasoningBuf.Len() > 0 || len(itemAccums) > 0 {
					break // return partial content
				}
				return agent.VerifiedToolSurfaceDispatch{Receipt: receipt}, fmt.Errorf("WebSocket unexpected close (code=%d): %w", closeCode, err)
			}
			// Generic read error
			if contentBuf.Len() > 0 || reasoningBuf.Len() > 0 || len(itemAccums) > 0 {
				break // return partial content
			}
			if wsTimedOut.Load() {
				if metrics != nil {
					metrics.IdleTimeoutCount++
				}
				return agent.VerifiedToolSurfaceDispatch{Receipt: receipt}, fmt.Errorf("WebSocket idle timeout (%v): no data received from %s", guiSSEIdleTimeout, wsURL)
			}
			return agent.VerifiedToolSurfaceDispatch{Receipt: receipt}, fmt.Errorf("WebSocket read error: %w", err)
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
		if eventResponseID := responsesWSResponseID(msgData); eventResponseID != "" {
			if responseID == "" {
				responseID = eventResponseID
			} else if responseID != eventResponseID {
				return agent.VerifiedToolSurfaceDispatch{Receipt: receipt}, fmt.Errorf("Responses API WebSocket stream response ID changed: %q -> %q", responseID, eventResponseID)
			}
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

		case responsesEventReasoningSummaryTextDelta, responsesEventReasoningTextDelta, responsesEventReasoningContentDelta, responsesEventReasoningDelta:
			var rd struct {
				Delta string `json:"delta"`
			}
			if json.Unmarshal(msgData, &rd) != nil {
				continue
			}
			if rd.Delta != "" {
				reasoningBuf.WriteString(rd.Delta)
				reasoningRoleFilterWS.Write(rd.Delta)
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
					return agent.VerifiedToolSurfaceDispatch{Receipt: receipt}, fmt.Errorf("tool arguments too large for %s: %d bytes exceeds limit %d", toolName, acc.args.Len(), guiMaxToolArgumentsBytes)
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
			// Some compatible Responses endpoints send a final reasoning item
			// but no reasoning delta events. Use its display-safe summary as a
			// fallback, without duplicating a streamed summary.
			var done struct {
				Item responsesAPIReasoningOutputItem `json:"item"`
			}
			if json.Unmarshal(msgData, &done) == nil {
				if emitted := appendResponsesReasoningSummary(&reasoningBuf, done.Item.DisplaySummary()); emitted != "" {
					reasoningRoleFilterWS.Write(emitted)
				}
			}

		case responsesEventCompleted:
			if eventUsage := llm.ExtractResponsesAPIUsageFromEventPayload(msgData); eventUsage != nil {
				usage = eventUsage
			}
			for _, summary := range responsesCompletedReasoningSummaries(msgData) {
				if emitted := appendResponsesReasoningSummary(&reasoningBuf, summary); emitted != "" {
					reasoningRoleFilterWS.Write(emitted)
				}
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
			return agent.VerifiedToolSurfaceDispatch{Receipt: receipt}, fmt.Errorf("Responses API error: %s (code=%s)", errMsg, failed.Response.Error.Code)

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
			return agent.VerifiedToolSurfaceDispatch{Receipt: receipt}, fmt.Errorf("Responses API WebSocket error: %s (code=%s, status=%d)", errMsg, errFrame.Error.Code, errFrame.Status)
		}
	}

postLoop:
	// -------------------------------------------------------------------
	// Post-loop: idle timeout with no content
	// -------------------------------------------------------------------
	if wsTimedOut.Load() && contentBuf.Len() == 0 && reasoningBuf.Len() == 0 && len(itemAccums) == 0 {
		if metrics != nil {
			metrics.IdleTimeoutCount++
		}
		return agent.VerifiedToolSurfaceDispatch{Receipt: receipt}, fmt.Errorf("WebSocket idle timeout (%v): no data received from %s", guiSSEIdleTimeout, wsURL)
	}

	// -------------------------------------------------------------------
	// Flush filters and build final response
	// -------------------------------------------------------------------
	tf.Flush()
	reasoningRoleFilterWS.Flush()
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

	return agent.VerifiedToolSurfaceDispatch{Response: &llm.Response{
		ResponseID: responseID,
		Choices:    []llm.Choice{{Message: msg, FinishReason: finishReason, TruncatedToolNames: truncatedTools, TruncatedToolArgs: truncatedToolArgs}},
		Usage:      usage,
	}, Receipt: receipt}, nil
}

// responsesWSResponseID extracts the provider response ID from a Responses
// WebSocket lifecycle frame. It intentionally ignores client trace fields,
// local request identifiers and tool-call IDs: none can stand in for a
// provider response correlation value.
func responsesWSResponseID(frame []byte) string {
	var envelope struct {
		Response struct {
			ID string `json:"id"`
		} `json:"response"`
	}
	if json.Unmarshal(frame, &envelope) != nil {
		return ""
	}
	return strings.TrimSpace(envelope.Response.ID)
}
