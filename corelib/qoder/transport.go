// Transport: the OpenAI→Qoder signed wire bridge. Agent-loop chat calls ride
// the normal OpenAI provider path (URL = the shared model server front); this
// RoundTripper translates them into the /algo signed contract — wasm COSY
// bearer, encoded body — and unwraps the upstream SSE envelope back into
// standard OpenAI chunks, so the generic OpenAI stream parser stays untouched.
package qoder

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
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/oauth"
	"github.com/RapidAI/CodeClaw/corelib/qoder/cosy"
)

// Matches reports whether cfg points at a Qoder edition (provider name or the
// shared chat base URL).
func Matches(cfg corelib.MaclawLLMConfig) bool {
	if _, ok := ProfileByName(cfg.ProviderName); ok {
		return true
	}
	return IsChatBaseURL(cfg.URL)
}

// WrapClientForConfig is the entrypoint the LLM engine calls. It returns the
// client unchanged for anything outside a Qoder edition. The uid + edition
// resolve at wrap time from the edition's credential row: user ids survive
// token rotations, so in-flight chats cannot mis-route between editions.
func WrapClientForConfig(client *http.Client, cfg corelib.MaclawLLMConfig) *http.Client {
	if !Matches(cfg) {
		return client
	}
	var uid, edition string
	if profile, ok := ProfileByName(cfg.ProviderName); ok {
		edition = profile.StoreID
		if store := oauth.NewFileCredentialStore(oauth.DefaultCredentialStorePath()); store != nil {
			if cred, err := store.Read(profile.StoreID); err == nil && cred != nil {
				uid = strings.TrimSpace(cred.UserID)
			}
		}
	}
	return WrapClientWithEdition(client, uid, edition)
}

// WrapClient returns a client whose Qoder chat calls are translated.
func WrapClient(client *http.Client) *http.Client {
	return WrapClientWithEdition(client, "", "")
}

// WrapClientWithEdition pins the signing uid + credential edition; zero values
// mean the transport resolves both per request from the credential store.
func WrapClientWithEdition(client *http.Client, uid, edition string) *http.Client {
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
	clone.Transport = &Transport{Base: base, uid: uid, edition: edition}
	return &clone
}

// Transport is one OpenAI→Qoder translation transport. credentialFor resolves
// the uid for a bearer device token (test hook); production callers pin the
// uid + edition at wrap time. alwaysAdapt forces the translation for every
// POST (test hook); production targets are selected by adaptTarget.
type Transport struct {
	Base          http.RoundTripper
	uid           string
	edition       string
	credentialFor func(bearer string) (string, string, bool)
	alwaysAdapt   bool
}

// uidFor resolves the signing uid + owning edition: the pinned pair wins,
// then the test hook, then the store lookup keyed by the in-flight bearer.
func (t *Transport) uidFor(bearer string) (uid, storeID string, ok bool) {
	if t.uid != "" || t.edition != "" {
		return t.uid, t.edition, true
	}
	if t.credentialFor != nil {
		return t.credentialFor(bearer)
	}
	return storedUIDForToken(bearer)
}

// storedUIDForToken looks up the user id + owning edition that belongs to a
// device token via the file credential store's known Qoder rows. The edition
// decides the chat gateway: the CN CLI talks to gateway.qoder.com.cn while
// the global one talks to api2-v2.qoder.sh.
func storedUIDForToken(bearer string) (string, string, bool) {
	store := oauth.NewFileCredentialStore(oauth.DefaultCredentialStorePath())
	var fallbackUID, fallbackStore string
	for _, storeID := range []string{StoreCN, StoreGlobal} {
		cred, err := store.Read(storeID)
		if err != nil || cred == nil || strings.TrimSpace(cred.UserID) == "" {
			continue
		}
		profile, _ := ProfileByStoreID(storeID)
		if strings.TrimSpace(cred.AccessToken) == bearer {
			return cred.UserID, storeID, true
		}
		if fallbackUID == "" {
			fallbackUID, fallbackStore = cred.UserID, storeID
		}
		_ = profile
	}
	if fallbackUID != "" {
		return fallbackUID, fallbackStore, true
	}
	return "", "", false
}

// adaptTarget reports chat calls aimed at the Qoder OpenAI front.
func adaptTarget(req *http.Request) bool {
	if req == nil || req.URL == nil || req.Method != http.MethodPost {
		return false
	}
	return IsChatBaseURL(req.URL.String())
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	if !(t.alwaysAdapt || adaptTarget(req)) {
		return base.RoundTrip(req)
	}
	// The signed chat gateway follows the token's EDITION first (the CN CLI
	// talks to gateway.qoder.com.cn, the global one to api2-v2.qoder.sh even
	// though both providers share one OpenAI front); custom deployments and
	// test servers override with the request's own origin.
	bearer := strings.TrimPrefix(strings.TrimSpace(req.Header.Get("Authorization")), "Bearer ")
	uid, storeID, _ := t.uidFor(bearer)
	var endpoint, endpointName string
	if profile, ok := ProfileByStoreID(storeID); ok {
		endpoint, endpointName = profile.ChatOrigin, profile.Name
	} else if profile, ok := ProfileByChatHost(req.URL.String()); ok {
		endpoint, endpointName = profile.ChatOrigin, profile.Name
	} else {
		endpoint = req.URL.Scheme + "://" + req.URL.Host
		endpointName = "request-origin"
	}
	log.Printf("[qoder-transport] chat via %s endpoint=%s uid=%t", endpointName, endpoint, uid != "")

	payload, err := io.ReadAll(req.Body)
	if err != nil {
		_ = req.Body.Close()
		return nil, err
	}
	_ = req.Body.Close()

	signing, err := cosy.New(MachineID(), ClientVersion, uid)
	if err != nil {
		return nil, fmt.Errorf("Qoder 签名上下文初始化失败: %w", err)
	}
	defer signing.Close()
	if err := signing.RefreshAuthFields(bearer); err != nil {
		return nil, fmt.Errorf("Qoder 签名材料注入失败: %w", err)
	}

	bodyJSON, originalStream := prepareAgentChatBody(payload)
	prepared, err := signing.PrepareInferRequest(endpoint, bodyJSON, agentChatModel(bodyJSON), "system")
	if err != nil {
		return nil, fmt.Errorf("Qoder 签名请求构建失败: %w", err)
	}

	outgoing, err := http.NewRequestWithContext(req.Context(), http.MethodPost, prepared.URL, strings.NewReader(prepared.Body))
	if err != nil {
		return nil, err
	}
	outgoing.Header = req.Header.Clone()
	for key := range outgoing.Header {
		outgoing.Header.Del(key)
	}
	for key, value := range prepared.Headers {
		outgoing.Header.Set(key, value)
	}
	outgoing.Header.Set("Accept", "text/event-stream")
	outgoing.Header.Set("User-Agent", UserAgent)
	outgoing.ContentLength = int64(len(prepared.Body))
	outgoing.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(prepared.Body)), nil
	}

	resp, err := base.RoundTrip(outgoing)
	if err != nil || resp == nil {
		return resp, err
	}
	return unwrapChatResponse(req.Context(), resp, originalStream, agentChatModel(bodyJSON)), nil
}

// ProfileByChatHost picks the edition whose ChatOrigin hosts the request.
func ProfileByChatHost(rawURL string) (Profile, bool) {
	for _, profile := range []Profile{CNProfile(), GlobalProfile()} {
		if profile.ChatOrigin != "" && requestHost(rawURL) == requestHost(profile.ChatOrigin) {
			return profile, true
		}
	}
	return Profile{}, false
}

// agentChatModel extracts the model key from the prepared body JSON.
func agentChatModel(bodyJSON string) string {
	var probe struct {
		Model string `json:"model"`
	}
	if json.Unmarshal([]byte(bodyJSON), &probe) == nil && strings.TrimSpace(probe.Model) != "" {
		return canonicalQoderModelKey(probe.Model)
	}
	return DefaultModel
}

// canonicalQoderModelKey maps legacy display-style names onto the real
// catalog keys; unknown names pass through (the agent_chat node validates).
func canonicalQoderModelKey(model string) string {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "qwen3.8-max", "qwen 3.8 max", "qwen3.8 max":
		return "qmodel_38max"
	case "qwen3.8-flash", "qwen 3.8 flash", "qwen3.8 flash":
		return "qfmodel"
	default:
		return strings.TrimSpace(model)
	}
}

// prepareAgentChatBody merges the OpenAI chat payload into the /algo wire
// body and reports whether the caller asked for streaming.
func prepareAgentChatBody(payload []byte) (string, bool) {
	var obj map[string]any
	if err := json.Unmarshal(payload, &obj); err != nil || obj == nil {
		obj = map[string]any{}
	}
	originalStream, _ := obj["stream"].(bool)
	if model := canonicalQoderModelKey(agentChatModel(string(payload))); strings.TrimSpace(model) != "" {
		obj["model"] = model
	}
	obj["stream"] = true
	// Session metadata the agent_chat_generation node expects. Per-request ids
	// are accepted (the session key is informational for the probe flow).
	if _, ok := obj["request_id"]; !ok {
		obj["request_id"] = newUUID()
	}
	if _, ok := obj["request_set_id"]; !ok {
		obj["request_set_id"] = newUUID()
	}
	if _, ok := obj["session_id"]; !ok {
		obj["session_id"] = newUUID()
	}
	obj["task_id"] = ""
	obj["business"] = map[string]any{"type": "agent", "scene": "cli"}
	obj["model_config"] = map[string]any{"key": obj["model"], "source": "system"}
	obj["data_policy_agreed"] = true
	out, err := json.Marshal(obj)
	if err != nil {
		return string(payload), originalStream
	}
	return string(out), originalStream
}

// ─────────────────────────────────────────────────────────────────────────────
// Response envelope → OpenAI SSE
// ─────────────────────────────────────────────────────────────────────────────

// unwrapChatResponse rewires the reply: streaming calls get a live
// envelope→OpenAI translation, plain calls get one aggregated completion.
func unwrapChatResponse(ctx context.Context, resp *http.Response, originalStream bool, model string) *http.Response {
	if ctx == nil {
		ctx = context.Background()
	}
	if resp.StatusCode != http.StatusOK {
		return resp
	}
	if originalStream {
		reader, writer := io.Pipe()
		translated := *resp
		translated.Body = reader
		translated.ContentLength = -1
		translated.Uncompressed = true
		translated.Header = resp.Header.Clone()
		translated.Header.Set("Content-Type", "text/event-stream")
		translated.Header.Del("Content-Length")
		translated.Header.Del("Content-Encoding")
		go func() {
			err := unwrapSSE(ctx, resp.Body, writer, model)
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
	aggregated, aggErr := aggregateEnvelopeStream(bytes.NewReader(raw), model)
	if aggErr != nil {
		return upstreamFailure(resp, aggErr.Error())
	}
	return replaceBody(resp, aggregated, "application/json")
}

const maxAggregateBytes = 8 << 20

// unwrapSSE streams the envelope events back as plain OpenAI chunks.
func unwrapSSE(ctx context.Context, upstream io.Reader, sink io.Writer, model string) error {
	reader := bufio.NewReaderSize(upstream, 32*1024)
	writer := &envelopeWriter{w: sink}
	buf := make([]byte, 16*1024)
	for {
		n, readErr := reader.Read(buf)
		if n > 0 {
			if !writer.ingest(string(buf[:n])) {
				break
			}
		}
		if readErr != nil {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	return nil
}

// envelopeWriter buffers partial lines and forwards translated frames.
type envelopeWriter struct {
	w    io.Writer
	head strings.Builder
}

// ingest consumes one upstream chunk; false stops the pump (terminal event).
func (e *envelopeWriter) ingest(chunk string) bool {
	e.head.WriteString(chunk)
	for {
		text := e.head.String()
		idx := strings.IndexByte(text, '\n')
		if idx < 0 {
			return true
		}
		line := strings.TrimRight(text[:idx], "\r")
		e.head.Reset()
		e.head.WriteString(text[idx+1:])
		if !e.line(line) {
			return false
		}
	}
}

func (e *envelopeWriter) line(line string) bool {
	if !strings.HasPrefix(line, "data:") {
		// event: lines and blanks carry no payload; error events are folded
		// into the data frame that follows (or the trailing plain JSON).
		return true
	}
	payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if payload == "" {
		return true
	}
	if payload == "[DONE]" {
		_, err := io.WriteString(e.w, "data: [DONE]\n\n")
		return err == nil
	}
	var envelope chatEnvelope
	if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
		// A plain-JSON tail ({"success":false,…}) is an upstream failure.
		var failure struct {
			Message string `json:"message"`
			MsgInfo int    `json:"msgCode"`
		}
		if json.Unmarshal([]byte(payload), &failure) == nil && failure.MsgInfo != 0 {
			return e.writeErrorFrame(fmt.Sprintf("Qoder 上游错误 (HTTP %d): %s", failure.MsgInfo, failure.Message))
		}
		return true
	}
	if envelope.Body == "" {
		// Telemetry tail (firstTokenDuration…) carries no chunk.
		return true
	}
	var chunk chatChunk
	if err := json.Unmarshal([]byte(envelope.Body), &chunk); err != nil {
		return true
	}
	if chunk.Code != "" {
		return e.writeErrorFrame(fmt.Sprintf("Qoder 上游错误 (%s): %s", chunk.Code, chunk.Message))
	}
	if _, err := fmt.Fprintf(e.w, "data: %s\n\n", envelope.Body); err != nil {
		return false
	}
	return true
}

func (e *envelopeWriter) writeErrorFrame(message string) bool {
	payload, err := json.Marshal(map[string]any{"error": map[string]any{"message": message, "type": "upstream_error"}})
	if err != nil {
		return true
	}
	_, _ = e.w.Write([]byte("data: " + string(payload) + "\n\n"))
	return false
}

// aggregateEnvelopeStream folds a fully-read envelope stream into one
// chat.completion for non-stream callers.
func aggregateEnvelopeStream(r io.Reader, model string) ([]byte, error) {
	content := &strings.Builder{}
	sink := &aggregateSink{content: content, model: model}
	reader := bufio.NewReaderSize(r, 32*1024)
	buf := make([]byte, 16*1024)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			sink.ingest(string(buf[:n]))
		}
		if err != nil {
			break
		}
	}
	sink.close()
	if sink.errMessage != "" && content.Len() == 0 {
		return nil, fmt.Errorf("%s", sink.errMessage)
	}
	message := map[string]any{"role": "assistant", "content": content.String()}
	resp := map[string]any{
		"id":      "chatcmpl-qoder-" + newUUID(),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       message,
			"finish_reason": sink.finishReason,
		}},
	}
	if sink.usage != nil {
		resp["usage"] = sink.usage
	}
	return json.Marshal(resp)
}

// aggregateSink reuses the envelope line parser with an accumulating writer.
type aggregateSink struct {
	content      *strings.Builder
	model        string
	finishReason string
	usage        any
	errMessage   string
	writer       envelopeWriter
}

func (a *aggregateSink) ingest(chunk string) {
	// Route through the same line parser; the writer callbacks fold chunks.
	a.writer.w = a
	_ = a.writer.ingest(chunk)
}

func (a *aggregateSink) Write(p []byte) (int, error) {
	text := string(p)
	if strings.HasPrefix(text, "data: ") {
		text = strings.TrimPrefix(text, "data: ")
	}
	if strings.TrimSpace(text) == "data: [DONE]" || strings.TrimSpace(text) == "[DONE]" {
		return len(p), nil
	}
	var chunk chatChunk
	var inner map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(text, "data:"))), &inner) != nil {
		return len(p), nil
	}
	if raw, err := json.Marshal(inner); err == nil {
		_ = json.Unmarshal(raw, &chunk)
	}
	if chunk.Code != "" {
		a.errMessage = fmt.Sprintf("Qoder 上游错误 (%s): %s", chunk.Code, chunk.Message)
		return len(p), nil
	}
	// Extract delta content and finish/usage via generic parsing.
	if choices, ok := inner["choices"].([]any); ok && len(choices) > 0 {
		choice, _ := choices[0].(map[string]any)
		if delta, ok := choice["delta"].(map[string]any); ok {
			if v, ok := delta["content"].(string); ok {
				a.content.WriteString(v)
			}
		}
		if finish, ok := choice["finish_reason"].(string); ok && finish != "" {
			a.finishReason = finish
		}
	}
	if usage, ok := inner["usage"]; ok && usage != nil {
		a.usage = usage
	}
	return len(p), nil
}

func (a *aggregateSink) close() {
	if a.finishReason == "" {
		a.finishReason = "stop"
	}
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

func replaceBody(resp *http.Response, payload []byte, contentType string) *http.Response {
	resp.Body = io.NopCloser(bytes.NewReader(payload))
	resp.ContentLength = int64(len(payload))
	resp.Header.Del("Content-Encoding")
	if contentType != "" {
		resp.Header.Set("Content-Type", contentType)
	}
	return resp
}
