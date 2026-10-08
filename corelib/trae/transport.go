package trae

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// MarkerHeader tells the local transport to translate this chat request. It
// is removed before the request leaves the process.
const MarkerHeader = "X-Maclaw-Trae"

// Transport rewrites OpenAI chat calls for the SOLO agent backend: the URL
// becomes the realm's llm_utils_chat endpoint, the body carries SOLO's
// content-chip messages plus the function/config_name pair, request headers
// switch to the Cloud-IDE-JWT family, and the upstream event stream is
// translated back into OpenAI chunks. Non-stream callers receive one
// aggregated chat.completion.
type Transport struct {
	Base http.RoundTripper
}

// WrapClient returns a client whose chat calls to either realm are translated.
func WrapClient(client *http.Client) *http.Client {
	if client == nil {
		client = &http.Client{Transport: http.DefaultTransport}
	}
	if _, ok := client.Transport.(*Transport); ok {
		return client
	}
	clone := *client
	base := clone.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	if _, ok := base.(*Transport); ok {
		clone.Transport = base
		return &clone
	}
	clone.Transport = &Transport{Base: base}
	return &clone
}

// WrapClientForConfig is the single entrypoint the LLM engine calls. It
// returns the client unchanged for anything outside both realms.
func WrapClientForConfig(client *http.Client, cfg corelib.MaclawLLMConfig) *http.Client {
	if !Matches(cfg.ProviderName, cfg.URL) {
		return client
	}
	return WrapClient(client)
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	profile, ok := adaptTarget(req)
	if !ok {
		return base.RoundTrip(req)
	}
	payload, err := io.ReadAll(req.Body)
	if err != nil {
		_ = req.Body.Close()
		return nil, err
	}
	_ = req.Body.Close()
	token, originalStream := prepareBody(payload, profile)
	outgoing, err := http.NewRequestWithContext(req.Context(), http.MethodPost,
		profile.ChatHost+ChatPath, bytes.NewReader(token))
	if err != nil {
		return nil, err
	}
	outgoing.Header = req.Header.Clone()
	applyChatHeaders(profile, outgoing.Header, req.Header.Get("Authorization"), originalStream)
	outgoing.ContentLength = int64(len(token))
	outgoing.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(token)), nil
	}
	resp, err := base.RoundTrip(outgoing)
	if err != nil || resp == nil {
		return resp, err
	}
	return translateResponse(req.Context(), resp, profile, token, originalStream), nil
}

// adaptTarget reports calls aimed at a SOLO chat endpoint. A request to the
// realm chat host is translated wherever the client placed /chat/completions.
func adaptTarget(req *http.Request) (Profile, bool) {
	if req == nil || req.URL == nil || req.Method != http.MethodPost {
		return Profile{}, false
	}
	if !strings.HasSuffix(req.URL.Path, "/chat/completions") {
		return Profile{}, false
	}
	return ProfileByChatHost(req.URL.String())
}

// applyChatHeaders stamps the SOLO request family. The account identity rides
// from the access token's claims so it stays stable across token rotation.
// Version headers follow the realm the request was routed to.
func applyChatHeaders(profile Profile, h http.Header, authorization string, stream bool) {
	key := soloSessionKey(authorization)
	h.Del(MarkerHeader)
	h.Set("Authorization", "Cloud-IDE-JWT "+key)
	h.Set("X-Cloudide-Token", key)
	stampSoloAccountFamily(profile, h, key)
	trace := randomHex(16)
	if trace != "" {
		h.Set("X-Custom-Trace-Id", trace)
		span := trace
		if len(span) > 16 {
			span = span[:16]
		}
		h.Set("X-Flow-Traceparent", fmt.Sprintf("04-%s-%s-01", trace, span))
	}
	if stream {
		h.Set("Accept", "text/event-stream")
		if requestID := randomHex(16); requestID != "" {
			h.Set("X-Request-ID", requestID)
			h.Set("X-Trae-Request-ID", requestID)
		}
	}
}

// soloSessionKey splits the Authorization header into the bare access token.
func soloSessionKey(authorization string) string {
	key := strings.TrimSpace(authorization)
	key = strings.TrimPrefix(key, "Bearer ")
	return strings.TrimSpace(key)
}

// stampSoloAccountFamily is the header set every SOLO-hosted call shares: the
// desktop identity, the per-realm IDE versions, and the per-account machine
// pair derived from the access token's claims. Chat and catalog callers must
// agree on it or the upstream starts treating the endpoints as two clients.
func stampSoloAccountFamily(profile Profile, h http.Header, sessionKey string) {
	h.Set("User-Agent", UserAgentPrefix+profile.IDEVersion)
	h.Set("x-app-id", AppID)
	h.Set("X-App-Version", "default")
	h.Set("X-Ide-Version", profile.IDEVersion)
	h.Set("X-Ide-Version-Code", profile.IDEVersionCode)
	h.Set("X-App-Version-Code", profile.IDEVersionCode)
	h.Set("X-Ide-Version-Type", "stable")
	h.Set("X-Device-Type", "windows")
	h.Set("X-OS-Version", "1.0")
	h.Set("X-Device-Brand", "PC")
	h.Set("Request-Traffic-Type", "prod")
	identity, claims := accountContext(sessionKey)
	// Requests may carry the stored login-device pair (engine-stamped via
	// ApplyHeaders); it wins over the claims-derived fallback because the
	// upstream binds the token family to the pair it was minted against.
	if h.Get("X-Uid") == "" {
		if uid := firstNonEmpty(claims.UserID, claims.UID); uid != "" {
			h.Set("X-Uid", uid)
		}
	}
	if h.Get("X-Machine-Id") == "" && identity.MachineID != "" {
		h.Set("X-Machine-Id", identity.MachineID)
		h.Set("X-Device-Id", identity.DeviceID)
	}
}

// ApplyHeaders stamps the stored Trae device pair onto an outbound request so
// the transport keeps it over the claims-derived fallback. Empty inputs stay
// absent. The engine calls it before the transport translates the request.
func ApplyHeaders(h http.Header, machineID, deviceID, userID string) {
	if h == nil {
		return
	}
	if id := strings.TrimSpace(machineID); id != "" {
		h.Set("X-Machine-Id", id)
	}
	if id := strings.TrimSpace(deviceID); id != "" {
		h.Set("X-Device-Id", id)
	}
	if id := strings.TrimSpace(userID); id != "" {
		h.Set("X-Uid", id)
	}
}

// accountContext turns the access token into the account identity. Claims
// come first (stable across token rotation); an unparseable token falls back
// to a key digest so retries still send one consistent device pair.
func accountContext(key string) (AccountIdentity, jwtClaims) {
	trimmed := strings.TrimSpace(key)
	claims := parseJWTClaims(trimmed)
	uid := firstNonEmpty(claims.UserID, claims.UID)
	if uid == "" {
		uid = trimmed
	}
	return IdentityForUserID(uid), claims
}

// prepareBody converts one OpenAI chat payload into a SOLO llm_utils_chat
// body: content strings become text chips, the model doubles as config_name,
// and the SOLO function pool is pinned. Returns (body, originalStream).
func prepareBody(payload []byte, profile Profile) ([]byte, bool) {
	var obj map[string]any
	if err := json.Unmarshal(payload, &obj); err != nil || obj == nil {
		return payload, false
	}
	originalStream, _ := obj["stream"].(bool)
	rewriteMessages(obj)
	model := strings.TrimSpace(asString(obj["model"]))
	if model == "" {
		model = profile.DefaultModel
	}
	obj["model"] = model
	obj["config_name"] = model
	obj["function"] = "solo_work_lite"
	obj["stream"] = true
	normalizeToolChoice(obj)
	normalizeTools(obj)
	out, err := json.Marshal(obj)
	if err != nil {
		return payload, originalStream
	}
	return out, originalStream
}

func asString(raw any) string {
	if text, ok := raw.(string); ok {
		return strings.TrimSpace(text)
	}
	return ""
}

// rewriteMessages converts string contents into the text-chip arrays SOLO
// expects and swaps OpenAI assistant tool_calls onto the function_call shape.
func rewriteMessages(obj map[string]any) {
	messages, ok := obj["messages"].([]any)
	if !ok {
		return
	}
	for _, rawMessage := range messages {
		message, ok := rawMessage.(map[string]any)
		if !ok {
			continue
		}
		if role, _ := message["role"].(string); role == "assistant" {
			rewriteAssistantToolCalls(message)
		}
		switch content := message["content"].(type) {
		case string:
			message["content"] = []any{map[string]any{"type": "text", "text": content}}
		}
	}
}

func rewriteAssistantToolCalls(message map[string]any) {
	calls, ok := message["tool_calls"].([]any)
	if !ok {
		return
	}
	kept := make([]any, 0, len(calls))
	for _, rawCall := range calls {
		call, ok := rawCall.(map[string]any)
		if !ok {
			continue
		}
		if fn, ok := call["function"].(map[string]any); ok {
			if _, hasCall := call["function_call"]; !hasCall {
				call["function_call"] = fn
				delete(call, "function")
			}
		}
		if fc, ok := call["function_call"].(map[string]any); ok {
			if strings.TrimSpace(asString(fc["name"])) == "" {
				continue
			}
		}
		kept = append(kept, call)
	}
	if len(kept) == 0 {
		delete(message, "tool_calls")
		return
	}
	message["tool_calls"] = kept
}

// normalizeToolChoice flattens OpenAI object tool_choice into the strings
// SOLO accepts; "none" suppresses tools entirely.
func normalizeToolChoice(obj map[string]any) {
	raw, present := obj["tool_choice"]
	if !present {
		return
	}
	suppress := func() {
		delete(obj, "tool_choice")
		delete(obj, "tools")
		delete(obj, "functions")
	}
	switch choice := raw.(type) {
	case string:
		if strings.EqualFold(strings.TrimSpace(choice), "none") {
			suppress()
		}
	case map[string]any:
		kind := strings.ToLower(strings.TrimSpace(asString(choice["type"])))
		switch kind {
		case "none":
			suppress()
		case "auto", "required":
			obj["tool_choice"] = kind
		case "function":
			name := ""
			if fn, ok := choice["function"].(map[string]any); ok {
				name = asString(fn["name"])
			}
			if name = strings.TrimSpace(name); name != "" {
				obj["tool_choice"] = name
			} else {
				obj["tool_choice"] = "auto"
			}
		default:
			delete(obj, "tool_choice")
		}
	default:
		delete(obj, "tool_choice")
	}
}

// normalizeTools adapts OpenAI tool definitions: SOLO expects the JSON
// schema inside function.parameters serialized as a string.
func normalizeTools(obj map[string]any) {
	rawTools, present := obj["tools"]
	if !present {
		return
	}
	list, ok := rawTools.([]any)
	if !ok || len(list) == 0 {
		delete(obj, "tools")
		return
	}
	out := make([]any, 0, len(list))
	for _, rawItem := range list {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		fn, ok := item["function"].(map[string]any)
		if !ok {
			continue
		}
		if params, ok := fn["parameters"]; ok {
			if paramsMap, isMap := params.(map[string]any); isMap {
				if serialized, err := json.Marshal(paramsMap); err == nil {
					fn["parameters"] = string(serialized)
				}
			}
		}
		out = append(out, item)
	}
	if len(out) == 0 {
		delete(obj, "tools")
		return
	}
	obj["tools"] = out
}

// translateResponse routes the reply: upstream errors pass through verbatim,
// streaming calls get a live SOLO→OpenAI chunk translation, plain calls get
// one aggregated chat.completion.
func translateResponse(ctx context.Context, resp *http.Response, profile Profile, requestBody []byte, originalStream bool) *http.Response {
	if ctx == nil {
		ctx = context.Background()
	}
	if resp.StatusCode != http.StatusOK {
		return resp
	}
	model := ""
	var probe map[string]any
	if json.Unmarshal(requestBody, &probe) == nil {
		model = asString(probe["model"])
	}
	if model == "" {
		model = profile.DefaultModel
	}
	if originalStream {
		reader, writer := io.Pipe()
		translated := *resp
		translated.Body = reader
		translated.ContentLength = -1
		translated.Uncompressed = true
		translated.Header = resp.Header.Clone()
		translated.Header.Set("Content-Type", "text/event-stream")
		// The translated chunks no longer match the upstream byte count.
		translated.Header.Del("Content-Length")
		translated.Header.Del("Content-Encoding")
		go func() {
			err := translateSSE(ctx, resp.Body, writer, model)
			_ = resp.Body.Close()
			_ = writer.CloseWithError(err)
		}()
		return &translated
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAggregateBytes))
	_ = resp.Body.Close()
	if err != nil {
		return upstreamFailure(resp, err.Error())
	}
	var envelope struct {
		Code    any    `json:"code"`
		Message string `json:"message"`
		Msg     string `json:"msg"`
	}
	if json.Unmarshal(raw, &envelope) == nil {
		if code := strings.TrimSpace(anyTrim(envelope.Code)); code != "" && code != "0" {
			detail := envelope.Message
			if detail == "" {
				detail = envelope.Msg
			}
			return upstreamFailure(resp, fmt.Sprintf("Trae 上游错误 code=%s: %s", code, detail))
		}
	}
	if !looksLikeSSE(raw) {
		return replaceBody(resp, raw, resp.Header.Get("Content-Type"))
	}
	aggregated, err := aggregateSSE(bytes.NewReader(raw), model)
	if err != nil {
		return upstreamFailure(resp, err.Error())
	}
	return replaceBody(resp, aggregated, "application/json")
}

func upstreamFailure(resp *http.Response, message string) *http.Response {
	payload, err := json.Marshal(map[string]any{
		"error": map[string]any{"message": message, "type": "upstream_error"},
	})
	if err != nil {
		payload = []byte(`{"error":{"message":"upstream error"}}`)
	}
	resp.StatusCode = http.StatusBadGateway
	resp.Status = "502 Bad Gateway"
	return replaceBody(resp, payload, "application/json")
}

const maxAggregateBytes = 8 << 20

func looksLikeSSE(body []byte) bool {
	text := strings.TrimSpace(string(body))
	return strings.HasPrefix(text, "event:") || strings.HasPrefix(text, "data:") ||
		strings.Contains(text, "\nevent:") || strings.Contains(text, "\ndata:")
}

func replaceBody(resp *http.Response, payload []byte, contentType string) *http.Response {
	resp.Body = io.NopCloser(bytes.NewReader(payload))
	resp.ContentLength = int64(len(payload))
	resp.Header.Del("Content-Encoding")
	if contentType != "" {
		resp.Header.Set("Content-Type", contentType)
	}
	return resp
}

// ─────────────────────────────────────────────────────────────────────────────
// SOLO event stream → OpenAI chunks
// ─────────────────────────────────────────────────────────────────────────────

// soloEvent is one upstream event (event: + data: pair).
type soloEvent struct {
	kind         string
	response     string
	reasoning    string
	toolCalls    json.RawMessage
	usage        map[string]any
	finishReason string
	errorCode    int64
	errorMessage string
}

func parseSoloLine(eventName, dataLine string) (*soloEvent, error) {
	event := &soloEvent{kind: strings.TrimSpace(eventName)}
	if dataLine == "" {
		return event, nil
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(dataLine), &raw); err != nil {
		return nil, err
	}
	switch event.kind {
	case "output":
		event.response, _ = raw["response"].(string)
		event.reasoning, _ = raw["reasoning_content"].(string)
		if value, ok := raw["tool_calls"]; ok && value != nil {
			event.toolCalls, _ = json.Marshal(value)
		}
	case "token_usage":
		event.usage = raw
	case "done":
		event.finishReason, _ = raw["finish_reason"].(string)
	case "error":
		if value, ok := raw["code"].(float64); ok {
			event.errorCode = int64(value)
		}
		event.errorMessage, _ = raw["message"].(string)
		event.errorMessage = strings.TrimSpace(event.errorMessage)
	}
	return event, nil
}

// sseScanState accumulates one event: / data: pair across lines.
type sseScanState struct {
	event string
	data  strings.Builder
}

func (s *sseScanState) line(line string) *soloEvent {
	switch {
	case line == "":
		if s.event == "" {
			s.data.Reset()
			return nil
		}
		if s.data.Len() == 0 {
			// A chunk boundary can split "event:X" from its data line; keep
			// the name and wait for the data rather than emit an empty event.
			return nil
		}
		event, err := parseSoloLine(s.event, s.data.String())
		s.event = ""
		s.data.Reset()
		if err != nil {
			return nil
		}
		return event
	case strings.HasPrefix(line, "event:"):
		s.event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
	case strings.HasPrefix(line, "data:"):
		s.data.WriteString(strings.TrimPrefix(line, "data:"))
	}
	return nil
}

// sseScanner incrementally parses the upstream stream across Read chunks.
type sseScanner struct {
	state sseScanState
	head  strings.Builder
}

func (s *sseScanner) step(chunk []byte) []*soloEvent {
	s.head.WriteString(string(chunk))
	text := s.head.String()
	s.head.Reset()
	if !strings.Contains(text, "\n") {
		s.head.WriteString(text)
		return nil
	}
	var events []*soloEvent
	lines := strings.SplitAfter(text, "\n")
	for i, line := range lines {
		last := i == len(lines)-1
		switch {
		case last && line == "":
			// Source ended with \n: flush a pending event boundary.
			if event := s.state.line(""); event != nil {
				events = append(events, event)
			}
		case last:
			// Incomplete trailing line: carry over to the next chunk.
			s.head.WriteString(line)
		default:
			if event := s.state.line(strings.TrimRight(line, "\r\n")); event != nil {
				events = append(events, event)
			}
		}
	}
	return events
}

func (s *sseScanner) close() []*soloEvent {
	var events []*soloEvent
	if rest := s.head.String(); strings.TrimSpace(rest) != "" {
		s.head.Reset()
		if event := s.state.line(strings.TrimRight(rest, "\r\n")); event != nil {
			events = append(events, event)
		}
	}
	if event := s.state.line(""); event != nil {
		events = append(events, event)
	}
	return events
}

// chunkWriter emits OpenAI chat.completion.chunk frames.
type chunkWriter struct {
	w       *bufio.Writer
	id      string
	model   string
	created int64
}

func (c *chunkWriter) writeChunk(delta map[string]any, finish string, usage map[string]any) error {
	chunk := map[string]any{
		"id":      c.id,
		"object":  "chat.completion.chunk",
		"created": c.created,
		"model":   c.model,
		"choices": []any{map[string]any{
			"index": 0,
			"delta": delta,
		}},
	}
	choice := chunk["choices"].([]any)[0].(map[string]any)
	if finish != "" {
		choice["finish_reason"] = finish
	}
	if usage != nil {
		chunk["usage"] = usage
	}
	raw, err := json.Marshal(chunk)
	if err != nil {
		return err
	}
	if _, err := c.w.WriteString("data: " + string(raw) + "\n\n"); err != nil {
		return err
	}
	// SSE chunks must reach the reader as they are produced: a buffered frame
	// that only flushes after the loop would be lost when the loop breaks on
	// a terminal event before flushing.
	c.flush()
	return nil
}

func (c *chunkWriter) flush() {
	_ = c.w.Flush()
}

func (c *chunkWriter) writeDone() error {
	_, err := c.w.WriteString("data: [DONE]\n\n")
	if err != nil {
		return err
	}
	c.flush()
	return nil
}

// writeErrorFrame emits an OpenAI-style error object in the stream so the
// engine can classify the failure rather than see an empty answer.
func writeErrorFrame(w *bufio.Writer, message string) {
	payload, err := json.Marshal(map[string]any{"error": map[string]any{"message": message, "type": "upstream_error"}})
	if err != nil {
		return
	}
	_, _ = w.WriteString("data: " + string(payload) + "\n\n")
}

var chunkIDCounter atomic.Int64

func compactID() string {
	return fmt.Sprintf("chatcmpl-trae-%d-%d", time.Now().UnixNano(), chunkIDCounter.Add(1))
}

// translateSSE streams the upstream events back as OpenAI chunks.
func translateSSE(ctx context.Context, upstream io.Reader, sink io.Writer, model string) error {
	scanner := &sseScanner{}
	writer := &chunkWriter{
		w:       bufio.NewWriterSize(sink, 32*1024),
		id:      compactID(),
		model:   model,
		created: time.Now().Unix(),
	}
	var pendingUsage map[string]any
	sawDone := false
	stopped := false
	handle := func(event soloEvent) bool {
		switch event.kind {
		case "output":
			delta := map[string]any{}
			if event.response != "" {
				delta["content"] = event.response
			}
			if event.reasoning != "" {
				delta["reasoning_content"] = event.reasoning
			}
			if len(event.toolCalls) > 0 && string(event.toolCalls) != "null" {
				var calls []*toolCallFrame
				if json.Unmarshal(event.toolCalls, &calls) == nil {
					deltas := make([]map[string]any, 0, len(calls))
					for _, call := range calls {
						if call == nil {
							continue
						}
						deltas = append(deltas, call.norm())
					}
					if len(deltas) > 0 {
						delta["tool_calls"] = deltas
					}
				}
			}
			if len(delta) > 0 {
				if writer.writeChunk(delta, "", nil) != nil {
					stopped = true
					return false
				}
			}
		case "token_usage":
			if usage := normalizeUsage(event.usage); len(usage) > 0 {
				pendingUsage = usage
			}
		case "done":
			if writer.writeChunk(map[string]any{}, orStop(event.finishReason), pendingUsage) != nil {
				stopped = true
				return false
			}
			sawDone = true
			return false
		case "error":
			message := fmt.Sprintf("Trae 上游错误 code=%d: %s", event.errorCode, event.errorMessage)
			_ = writer.writeChunk(map[string]any{}, "", nil)
			writeErrorFrame(writer.w, message)
			sawDone = true
			return false
		}
		return true
	}
	serve := func(events []*soloEvent) bool {
		for _, event := range events {
			if event == nil {
				continue
			}
			if !handle(*event) {
				return false
			}
		}
		return true
	}
	reader := bufio.NewReaderSize(upstream, 32*1024)
	buf := make([]byte, 16*1024)
	for !stopped && !sawDone {
		n, readErr := reader.Read(buf)
		if n > 0 {
			// Per-chunk flush happens inside writeChunk, so a terminal-event
			// break here can no longer drop a finished batch.
			if !serve(scanner.step(buf[:n])) {
				break
			}
		}
		if readErr != nil {
			break
		}
		select {
		case <-ctx.Done():
			stopped = true
		default:
		}
	}
	// Every terminal branch closes the frame with [DONE]: the done/error
	// handler emitted the finish chunk, so the terminator follows here.
	if sawDone {
		return writer.writeDone()
	}
	if !serve(scanner.close()) {
		return writer.writeDone()
	}
	finish := "stop"
	if ctx.Err() != nil {
		finish = "abort"
	}
	_ = writer.writeChunk(map[string]any{}, finish, pendingUsage)
	return writer.writeDone()
}

// toolCallFrame maps one upstream tool_call entry onto the OpenAI delta.
type toolCallFrame map[string]any

func (f toolCallFrame) norm() map[string]any {
	if f == nil {
		return map[string]any{}
	}
	if fc, ok := f["function_call"].(map[string]any); ok {
		f["function"] = fc
		delete(f, "function_call")
	}
	if fn, ok := f["function"].(map[string]any); ok {
		delete(fn, "namespace")
		delete(fn, "partial_arguments")
	}
	return map[string]any(f)
}

// normalizeUsage keeps only the pieces that map onto OpenAI usage.
func normalizeUsage(raw map[string]any) map[string]any {
	if raw == nil {
		return nil
	}
	out := map[string]any{}
	for _, key := range []string{"prompt_tokens", "completion_tokens", "total_tokens", "reasoning_tokens"} {
		if v, ok := raw[key]; ok {
			out[key] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func orStop(finish string) string {
	if strings.TrimSpace(finish) == "" {
		return "stop"
	}
	return strings.TrimSpace(finish)
}

// aggregateSSE folds a fully-read upstream stream into one chat.completion.
func aggregateSSE(r io.Reader, fallbackModel string) ([]byte, error) {
	scanner := &sseScanner{}
	var (
		content      strings.Builder
		reasoning    strings.Builder
		toolCalls    = map[int]map[string]any{}
		toolOrder    []int
		usage        map[string]any
		finishReason = "stop"
		streamErr    string
	)
	consume := func(event soloEvent) {
		switch event.kind {
		case "output":
			content.WriteString(event.response)
			reasoning.WriteString(event.reasoning)
			mergeToolCallJSON(toolCalls, &toolOrder, event.toolCalls)
		case "token_usage":
			usage = event.usage
		case "done":
			if strings.TrimSpace(event.finishReason) != "" {
				finishReason = strings.TrimSpace(event.finishReason)
			}
		case "error":
			streamErr = fmt.Sprintf("Trae 上游错误 code=%d: %s", event.errorCode, event.errorMessage)
		}
	}
	buf := make([]byte, 16*1024)
	for {
		n, err := r.Read(buf)
		for _, event := range scanner.step(buf[:n]) {
			if event != nil {
				consume(*event)
			}
		}
		if err != nil {
			break
		}
	}
	for _, event := range scanner.close() {
		if event != nil {
			consume(*event)
		}
	}
	if streamErr != "" && content.Len() == 0 && len(toolOrder) == 0 {
		return nil, fmt.Errorf("%s", streamErr)
	}
	message := map[string]any{"role": "assistant", "content": content.String()}
	if reasoning.Len() > 0 {
		message["reasoning_content"] = reasoning.String()
	}
	if len(toolOrder) > 0 {
		calls := make([]any, 0, len(toolOrder))
		for _, index := range toolOrder {
			call := toolCalls[index]
			delete(call, "index")
			if _, ok := call["function"].(map[string]any); !ok {
				if fc, ok := call["function_call"].(map[string]any); ok {
					delete(call, "function_call")
					call["function"] = fc
				}
			}
			calls = append(calls, call)
		}
		message["tool_calls"] = calls
	}
	resp := map[string]any{
		"id":      compactID(),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   fallbackModel,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       message,
			"finish_reason": finishReason,
		}},
	}
	if normalized := normalizeUsage(usage); len(normalized) > 0 {
		resp["usage"] = normalized
	}
	return json.Marshal(resp)
}

func mergeToolCallJSON(toolCalls map[int]map[string]any, order *[]int, raw json.RawMessage) {
	if len(raw) == 0 || string(raw) == "null" {
		return
	}
	var list []*toolCallFrame
	if err := json.Unmarshal(raw, &list); err != nil {
		return
	}
	for _, frame := range list {
		if frame == nil {
			continue
		}
		call := frame.norm()
		index := 0
		if v, ok := call["index"].(float64); ok {
			index = int(v)
		}
		merged, seen := toolCalls[index]
		if !seen {
			merged = map[string]any{"index": index}
			toolCalls[index] = merged
			*order = append(*order, index)
		}
		mergeToolCallDelta(merged, call)
	}
}

func mergeToolCallDelta(merged, delta map[string]any) {
	if v, ok := delta["id"].(string); ok && v != "" {
		merged["id"] = v
	}
	if v, ok := delta["type"].(string); ok && v != "" {
		merged["type"] = v
	}
	function, _ := delta["function"].(map[string]any)
	if function == nil {
		function, _ = delta["function_call"].(map[string]any)
	}
	if function == nil {
		return
	}
	delete(function, "namespace")
	delete(function, "partial_arguments")
	accumulator, ok := merged["function"].(map[string]any)
	if !ok {
		accumulator = map[string]any{}
		merged["function"] = accumulator
	}
	if v, ok := function["name"].(string); ok && v != "" {
		accumulator["name"] = v
	}
	if v, ok := function["arguments"].(string); ok && v != "" {
		if prev, _ := accumulator["arguments"].(string); prev != "" {
			accumulator["arguments"] = prev + v
		} else {
			accumulator["arguments"] = v
		}
	}
}
