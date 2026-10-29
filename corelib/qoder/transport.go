// Transport: the OpenAI→Qoder signed wire bridge. Agent-loop chat calls ride
// the normal OpenAI provider path (URL = the shared model server front); this
// RoundTripper translates them into the /algo signed contract — wasm COSY
// bearer, encoded body — and unwraps the upstream SSE envelope back into
// standard OpenAI chunks, so the generic OpenAI stream parser stays untouched.
//
// The translation is two steps with the tool-surface gate between them.
// adaptTransport rewrites the OpenAI JSON into the agent_chat JSON while the
// body is still JSON. The gate verifies that JSON. Transport, nested under
// the gate, signs those verified bytes. Encoding above the gate makes the
// gate parse COSY ciphertext and report invalid outbound request JSON.
package qoder

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

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
//
// When the client already carries a tool-surface gate, the JSON rewrite stays
// above the gate and the COSY encoder is nested underneath it. The gate then
// verifies the agent_chat JSON, and the encoder signs exactly the bytes the
// gate releases. Callers without a gate (probes, vision checks) keep a single
// transport that rewrites and encodes, because nothing is inspecting the JSON.
func WrapClientWithEdition(client *http.Client, uid, edition string) *http.Client {
	if client == nil {
		client = &http.Client{Transport: http.DefaultTransport}
	}
	if _, ok := client.Transport.(*Transport); ok {
		return client
	}
	if _, ok := client.Transport.(*adaptTransport); ok {
		return client
	}
	if gate, ok := client.Transport.(corelib.ToolSurfaceWireGate); ok {
		if encoder, nested := gate.ToolSurfaceNext().(*Transport); nested {
			// A later wrap can learn the edition the first one missed. Replace
			// the encoder in place so an empty first identity cannot stick, and
			// do not stack a second encoder under the gate.
			if !encoder.pinnedIdentityMatches(uid, edition) {
				gate.SetToolSurfaceNext(encoder.withPinnedIdentity(uid, edition))
			}
		} else {
			prev := gate.ToolSurfaceNext()
			if prev == nil {
				prev = http.DefaultTransport
			}
			gate.SetToolSurfaceNext(&Transport{Base: prev, uid: uid, edition: edition})
		}
		clone := *client
		clone.Transport = &adaptTransport{Base: client.Transport}
		return &clone
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

// adaptTransport rewrites an OpenAI chat body into the agent_chat JSON the
// tool-surface gate verifies. It does not sign or encode.
type adaptTransport struct {
	Base http.RoundTripper
}

type qoderWireMeta struct {
	adapted        bool
	originalStream bool
}

type qoderWireMetaKey struct{}

func (t *adaptTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	if req == nil || !adaptTarget(req) {
		return base.RoundTrip(req)
	}
	if req.Body == nil {
		return nil, fmt.Errorf("qoder adapt: missing request body")
	}
	payload, err := io.ReadAll(req.Body)
	if req.Body != nil {
		_ = req.Body.Close()
	}
	if err != nil {
		return nil, err
	}
	bodyJSON, originalStream := prepareAgentChatBody(payload)
	adapted := []byte(bodyJSON)
	outgoing := req.Clone(context.WithValue(req.Context(), qoderWireMetaKey{}, qoderWireMeta{
		adapted:        true,
		originalStream: originalStream,
	}))
	outgoing.Body = io.NopCloser(bytes.NewReader(adapted))
	outgoing.ContentLength = int64(len(adapted))
	// The tool-surface gate must be the last rewind point. A GetBody here would
	// let a replay resend this JSON without a new receipt.
	outgoing.GetBody = nil
	if outgoing.Header == nil {
		outgoing.Header = make(http.Header)
	}
	outgoing.Header.Set("Content-Length", fmt.Sprintf("%d", len(adapted)))
	outgoing.Header.Set("Content-Type", "application/json")
	return base.RoundTrip(outgoing)
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

// pinnedIdentityMatches reports whether pinning uid and edition would leave
// this encoder's identity unchanged. Empty new values keep the identity
// already pinned.
func (t *Transport) pinnedIdentityMatches(uid, edition string) bool {
	if t == nil {
		return false
	}
	return t.uid == preferNonEmpty(uid, t.uid) && t.edition == preferNonEmpty(edition, t.edition)
}

// withPinnedIdentity returns a copy that keeps the network hop and test hooks,
// and adopts whichever identity values the new wrap actually knows.
func (t *Transport) withPinnedIdentity(uid, edition string) *Transport {
	copy := *t
	copy.uid = preferNonEmpty(uid, t.uid)
	copy.edition = preferNonEmpty(edition, t.edition)
	return &copy
}

func preferNonEmpty(next, prev string) string {
	if strings.TrimSpace(next) != "" {
		return next
	}
	return prev
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
// the global one talks to api3.qoder.sh.
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
	// talks to gateway.qoder.com.cn, the global one to api3.qoder.sh even
	// though both providers share one OpenAI front on api2-v2); custom
	// deployments and test servers override with the request's own origin.
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

	bodyJSON, originalStream := t.canonicalChatBody(req, payload)
	model := agentChatModel(bodyJSON)
	prepared, err := signing.PrepareInferRequest(endpoint, bodyJSON, model, "system")
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
	return unwrapChatResponse(req.Context(), resp, originalStream, model), nil
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

// agentChatModel extracts the catalog key the signer must send. It is the
// same key applyAgentChatContract wrote into the body; a display name such
// as qwen3.8-max is not a model the node will start.
func agentChatModel(bodyJSON string) string {
	var probe struct {
		Model string `json:"model"`
	}
	if json.Unmarshal([]byte(bodyJSON), &probe) == nil {
		if model := canonicalQoderModelKey(probe.Model); model != "" {
			return model
		}
	}
	return canonicalQoderModelKey(DefaultModel)
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

// canonicalChatBody returns the JSON PrepareInferRequest must sign. After the
// tool-surface gate has verified an adapted body, those bytes are signed as
// released. A second prepareAgentChatBody would be a tool-surface rewrite the
// gate can no longer see.
func (t *Transport) canonicalChatBody(req *http.Request, payload []byte) (string, bool) {
	if req != nil {
		if meta, ok := req.Context().Value(qoderWireMetaKey{}).(qoderWireMeta); ok && meta.adapted {
			return string(payload), meta.originalStream
		}
	}
	return prepareAgentChatBody(payload)
}

// applyAgentChatContract fills the fields api3's agent_router uses to select
// a flow. A body that only carries business.type/scene and an empty task_id
// is accepted by the domestic gateway and rejected by the international one:
// "[FAIL]node:agent_router msg:None flow nodes found for router agent_router".
// The international CLI selects the agent_common / common flow instead.
func applyAgentChatContract(obj map[string]any) {
	if obj == nil {
		return
	}
	obj["stream"] = true
	// text_content is dropped upstream and then fails the role check.
	// contents hits a fastjson type error. max_completion_tokens is the
	// OpenAI name; the signed body carries the cap as max_tokens.
	delete(obj, "text_content")
	delete(obj, "contents")
	// A non-positive cap asks the node for an empty completion. Fold the
	// OpenAI name into max_tokens and drop a zero so the gateway default applies.
	if cap, ok := positiveTokenCap(obj["max_tokens"]); ok {
		obj["max_tokens"] = cap
	} else if cap, ok := positiveTokenCap(obj["max_completion_tokens"]); ok {
		obj["max_tokens"] = cap
	} else {
		delete(obj, "max_tokens")
	}
	delete(obj, "max_completion_tokens")
	fillAgentChatID(obj, "request_id")
	fillAgentChatID(obj, "request_set_id")
	fillAgentChatID(obj, "session_id")
	if record, _ := obj["chat_record_id"].(string); strings.TrimSpace(record) == "" {
		obj["chat_record_id"] = obj["request_set_id"]
	}
	obj["agent_id"] = "agent_common"
	obj["task_id"] = "common"
	obj["session_type"] = "qodercli"
	obj["chat_task"] = "FREE_INPUT"
	obj["is_reply"] = true
	obj["is_retry"] = false
	obj["source"] = 1
	obj["version"] = "3"
	obj["business"] = map[string]any{
		"product":  "cli",
		"version":  "1.0.0",
		"type":     "agent",
		"scene":    "cli",
		"stage":    "start",
		"id":       newUUID(),
		"begin_at": time.Now().UnixMilli(),
	}
	model, _ := obj["model"].(string)
	model = canonicalQoderModelKey(model)
	if model == "" {
		model = canonicalQoderModelKey(DefaultModel)
	}
	obj["model"] = model
	// The signer argument and model_config.key are the same catalog key.
	// A display name here (qwen3.8-max) is a model the node will not start.
	obj["model_config"] = map[string]any{"key": model, "source": "system"}
	obj["data_policy_agreed"] = true
}

// positiveTokenCap accepts a JSON number that can be a generation cap.
func positiveTokenCap(value any) (int, bool) {
	var n int
	switch v := value.(type) {
	case int:
		n = v
	case int32:
		n = int(v)
	case int64:
		n = int(v)
	case float32:
		n = int(v)
	case float64:
		n = int(v)
	default:
		return 0, false
	}
	if n <= 0 {
		return 0, false
	}
	return n, true
}

// fillAgentChatID keeps a caller-supplied id and replaces a missing or blank one.
func fillAgentChatID(obj map[string]any, key string) {
	if id, _ := obj[key].(string); strings.TrimSpace(id) != "" {
		return
	}
	obj[key] = newUUID()
}

// prepareAgentChatBody merges the OpenAI chat payload into the /algo wire
// body and reports whether the caller asked for streaming.
func prepareAgentChatBody(payload []byte) (string, bool) {
	var obj map[string]any
	if err := json.Unmarshal(payload, &obj); err != nil || obj == nil {
		obj = map[string]any{}
	}
	originalStream, _ := obj["stream"].(bool)
	applyAgentChatContract(obj)
	out, err := marshalAgentChatJSON(obj)
	if err != nil {
		return string(payload), originalStream
	}
	return string(out), originalStream
}

// marshalAgentChatJSON emits the agent_chat object the way JSON.stringify does.
// encoding/json's default HTML escaping rewrites '&', '<' and '>' inside tool
// descriptions and prompts, which changes the signed bytes without changing
// the parsed document.
func marshalAgentChatJSON(value any) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	out := buf.Bytes()
	if len(out) > 0 && out[len(out)-1] == '\n' {
		out = out[:len(out)-1]
	}
	return out, nil
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
		if n > 0 && !writer.ingestBytes(buf[:n]) {
			return nil
		}
		if readErr != nil {
			// A final data line may arrive without its trailing newline.
			writer.flushPartial()
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
}

// envelopeWriter buffers partial lines and forwards translated frames.
// completionID is the one provider response id for this HTTP completion.
// Qoder's envelope is one generation, but inner chunks switch from a
// gateway chatcmpl-* id to the upstream completion UUID. OpenAI's parser
// freezes the first id and rejects a later one, which aborts a turn the
// user has already seen. The adapter therefore publishes the first id for
// the whole completion; it does not ask the parser to merge two responses.
type envelopeWriter struct {
	w                  io.Writer
	head               strings.Builder
	frame              []byte
	scratch            []byte
	tailBuf            []byte
	tailCode           int
	tailText           string
	hasBody            bool
	completionID       string
	completionIDQuoted string
	idRewritten        bool
	idSpliceFailed     bool
}

// ingest consumes one upstream chunk; false stops the pump (terminal event).
// One read often holds many SSE lines. Walk that buffer once and keep only
// the unfinished tail, instead of copying the tail after every line.
func (e *envelopeWriter) ingest(chunk string) bool {
	e.head.WriteString(chunk)
	return e.finishIngest()
}

func (e *envelopeWriter) ingestBytes(chunk []byte) bool {
	// One allocation for this read. A tool-argument line is much larger than
	// the tokens before it, and Builder would otherwise double its way there.
	e.head.Grow(len(chunk))
	e.head.Write(chunk)
	return e.finishIngest()
}

func (e *envelopeWriter) finishIngest() bool {
	text := e.head.String()
	off := 0
	for {
		rel := strings.IndexByte(text[off:], '\n')
		if rel < 0 {
			break
		}
		line := strings.TrimRight(text[off:off+rel], "\r")
		off += rel + 1
		if !e.line(line) {
			e.keepTail(text, off)
			return false
		}
	}
	e.keepTail(text, off)
	return true
}

// keepTail retains the unparsed suffix. text aliases the builder buffer and
// is invalid after the next builder mutation, so the suffix is copied first.
func (e *envelopeWriter) keepTail(text string, off int) {
	if off == 0 {
		return
	}
	if off >= len(text) {
		e.head.Reset()
		return
	}
	rest := strings.Clone(text[off:])
	e.head.Reset()
	e.head.WriteString(rest)
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
	// msgCode is captured while the envelope keys are scanned. A normal token
	// does not get a second walk or a second copy of its delta.
	if !e.scanEnvelope(payload) {
		var envelope struct {
			Body string `json:"body"`
		}
		if json.Unmarshal([]byte(payload), &envelope) != nil {
			return true
		}
		if message, bad := upstreamTailError(payload); bad {
			return e.writeErrorFrame("", message)
		}
		if envelope.Body == "" {
			return true
		}
		return e.publishChunkBytes([]byte(envelope.Body))
	}
	if e.tailCode != 0 {
		return e.writeErrorFrame("", fmt.Sprintf("Qoder 上游错误 (HTTP %d): %s", e.tailCode, e.tailText))
	}
	if !e.hasBody || len(e.scratch) == 0 {
		return true
	}
	if id, ok := leadingCompletionID(e.scratch); ok {
		return e.writePlainCompletion(e.scratch, id)
	}
	return e.publishChunkBytes(e.scratch)
}

// publishChunkBytes forwards a chunk that is not the plain leading-id shape.
// When the parsed id is already the one this response publishes, the body is
// written from the scan buffer. Copying it to a string first duplicated every
// tool-argument delta.
func (e *envelopeWriter) publishChunkBytes(body []byte) bool {
	// A choices-only delta has no completion id and no error code. Escapes are
	// absent too, so a raw scan sees every key; encoding/json would only copy it.
	if bytes.IndexByte(body, '\\') < 0 && !tailHasIDOrCodeKey(body) {
		return e.writeDataBytes(body)
	}
	var head struct {
		ID      json.RawMessage `json:"id"`
		Code    json.RawMessage `json:"code"`
		Message string          `json:"message"`
	}
	if err := json.Unmarshal(body, &head); err != nil {
		return true
	}
	if code := jsonCodeString(head.Code); code != "" {
		// Keep the machine code on the error object. Flattening it into the
		// sentence is what made a billing denial (code 110) look like ordinary
		// prose by the time UserFacingError classified the failure.
		return e.writeErrorFrame(code, fmt.Sprintf("Qoder 上游错误 (%s): %s", code, head.Message))
	}
	rawID := bytes.TrimSpace(head.ID)
	if len(rawID) == 0 || rawID[0] == '"' {
		id := completionIDText(head.ID)
		// encoding/json keeps the last top-level id, so a match here is the id
		// the OpenAI parser will see. An earlier different key is ignored.
		if id == "" || e.completionID == "" || id == e.completionID {
			if e.completionID == "" && id != "" {
				e.completionID = strings.Clone(id)
			}
			return e.writeDataBytes(body)
		}
		if handled, ok := e.writeSplicedTopLevelID(body, e.quotedCompletionID()); handled {
			if ok {
				e.noteIDRewritten(id)
			}
			return ok
		}
	}
	return e.writeData(e.stableCompletionBody(string(body), head.ID))
}

// writeSplicedTopLevelID replaces every top-level id in body and writes one
// SSE frame. handled is false when the byte scan cannot see the id; the
// caller then falls back to the structural rewrite. body is not retained.
func (e *envelopeWriter) writeSplicedTopLevelID(body []byte, quoted string) (handled, ok bool) {
	spans := topLevelJSONStringSpans(body, "id")
	if len(spans) == 0 || quoted == "" {
		return false, false
	}
	extra := 0
	for _, sp := range spans {
		extra += len(quoted) - (sp[1] - sp[0])
	}
	const prefix = "data: "
	buf := e.frameBuf(len(prefix) + len(body) + extra + 2)
	buf = append(buf, prefix...)
	prev := 0
	for _, sp := range spans {
		if sp[0] < prev || sp[1] < sp[0] || sp[1] > len(body) {
			return false, false
		}
		buf = append(buf, body[prev:sp[0]]...)
		buf = append(buf, quoted...)
		prev = sp[1]
	}
	buf = append(buf, body[prev:]...)
	return true, e.writeFrame(buf)
}

func (e *envelopeWriter) flushPartial() bool {
	if e.head.Len() == 0 {
		return true
	}
	// String aliases the builder buffer; copy before Reset.
	line := strings.Clone(strings.TrimRight(e.head.String(), "\r\n"))
	e.head.Reset()
	line = strings.TrimSpace(line)
	if line == "" {
		return true
	}
	return e.line(line)
}

func upstreamTailError(payload string) (string, bool) {
	var failure struct {
		Message string `json:"message"`
		MsgInfo int    `json:"msgCode"`
	}
	if json.Unmarshal([]byte(payload), &failure) != nil || failure.MsgInfo == 0 {
		return "", false
	}
	return fmt.Sprintf("Qoder 上游错误 (HTTP %d): %s", failure.MsgInfo, failure.Message), true
}

// envelopeBody unescapes the top-level "body" string without encoding/json.
// A false result falls back to Unmarshal; the token path is this scanner.
func (e *envelopeWriter) envelopeBody(payload string) (string, bool) {
	if !e.scanEnvelope(payload) || !e.hasBody {
		return "", false
	}
	return string(e.scratch), true
}

// scanEnvelope fills scratch with the raw body and records a top-level
// msgCode. Trailing junk is rejected so the result matches encoding/json.
func (e *envelopeWriter) scanEnvelope(payload string) bool {
	e.tailCode = 0
	e.tailText = ""
	e.hasBody = false
	e.scratch = e.scratch[:0]
	s := skipJSONSpaceStr(payload)
	if len(s) == 0 || s[0] != '{' {
		return false
	}
	s = skipJSONSpaceStr(s[1:])
	if len(s) > 0 && s[0] == '}' {
		return skipJSONSpaceStr(s[1:]) == ""
	}
	for {
		if len(s) == 0 || s[0] != '"' {
			return false
		}
		key, rest, ok := rawJSONString(s)
		if !ok || strings.IndexByte(key, '\\') >= 0 {
			return false
		}
		s = skipJSONSpaceStr(rest)
		if len(s) == 0 || s[0] != ':' {
			return false
		}
		s = skipJSONSpaceStr(s[1:])
		switch key {
		case "body":
			e.scratch = e.scratch[:0]
			e.scratch, s, ok = appendJSONString(e.scratch, s)
			if !ok {
				return false
			}
			e.hasBody = true
		case "message":
			e.tailBuf = e.tailBuf[:0]
			e.tailBuf, s, ok = appendJSONString(e.tailBuf, s)
			if !ok {
				return false
			}
			e.tailText = string(e.tailBuf)
		case "msgCode":
			var n int
			n, s, ok = takeJSONInt(s)
			if !ok {
				return false
			}
			e.tailCode = n
		default:
			s, ok = skipJSONValue(s)
			if !ok {
				return false
			}
		}
		s = skipJSONSpaceStr(s)
		if len(s) == 0 {
			return false
		}
		switch s[0] {
		case ',':
			s = skipJSONSpaceStr(s[1:])
		case '}':
			return skipJSONSpaceStr(s[1:]) == ""
		default:
			return false
		}
	}
}

func takeJSONInt(s string) (int, string, bool) {
	rest, ok := skipJSONNumber(s)
	if !ok {
		return 0, "", false
	}
	num := s[:len(s)-len(rest)]
	if num == "" || num[0] == '-' {
		return 0, "", false
	}
	n := 0
	for i := 0; i < len(num); i++ {
		if num[i] < '0' || num[i] > '9' {
			return 0, "", false
		}
		n = n*10 + int(num[i]-'0')
		if n < 0 {
			return 0, "", false
		}
	}
	return n, rest, true
}

func skipJSONSpaceStr(s string) string {
	i := 0
	for i < len(s) && isJSONSpace(s[i]) {
		i++
	}
	return s[i:]
}

func rawJSONString(s string) (content, rest string, ok bool) {
	if len(s) == 0 || s[0] != '"' {
		return "", s, false
	}
	i := 1
	for i < len(s) {
		if s[i] == '\\' {
			if i+1 >= len(s) {
				return "", s, false
			}
			i += 2
			continue
		}
		if s[i] == '"' {
			return s[1:i], s[i+1:], true
		}
		i++
	}
	return "", s, false
}

func appendJSONString(dst []byte, s string) ([]byte, string, bool) {
	if len(s) == 0 || s[0] != '"' {
		return dst, s, false
	}
	// Escapes only shrink, so the raw content length is enough for one alloc.
	// Small deltas already fit in scratch and skip this scan.
	if len(s) > 1 && cap(dst)-len(dst) < len(s)-1 {
		if content, _, ok := rawJSONString(s); ok && len(content) > cap(dst)-len(dst) {
			grown := make([]byte, len(dst), len(dst)+len(content))
			copy(grown, dst)
			dst = grown
		}
	}
	i := 1
	for i < len(s) {
		c := s[i]
		if c == '"' {
			return dst, s[i+1:], true
		}
		if c == '\\' {
			if i+1 >= len(s) {
				return dst, s, false
			}
			switch s[i+1] {
			case '"', '\\', '/':
				dst = append(dst, s[i+1])
				i += 2
			case 'b':
				dst = append(dst, '\b')
				i += 2
			case 'f':
				dst = append(dst, '\f')
				i += 2
			case 'n':
				dst = append(dst, '\n')
				i += 2
			case 'r':
				dst = append(dst, '\r')
				i += 2
			case 't':
				dst = append(dst, '\t')
				i += 2
			case 'u':
				r, n, ok := decodeJSONU(s[i:])
				if !ok {
					return dst, s, false
				}
				dst = utf8.AppendRune(dst, r)
				i += n
			default:
				return dst, s, false
			}
			continue
		}
		if c < 0x20 {
			return dst, s, false
		}
		// Ordinary text, including the unescaped delta, is copied in one run.
		j := i + 1
		for j < len(s) {
			c := s[j]
			if c == '"' || c == '\\' || c < 0x20 {
				break
			}
			j++
		}
		dst = append(dst, s[i:j]...)
		i = j
	}
	return dst, s, false
}

func decodeJSONU(s string) (rune, int, bool) {
	if len(s) < 6 || s[0] != '\\' || s[1] != 'u' {
		return 0, 0, false
	}
	r, ok := hex4(s[2:6])
	if !ok {
		return 0, 0, false
	}
	if utf16.IsSurrogate(rune(r)) {
		// encoding/json consumes a pair only when it decodes to one rune.
		// Two high surrogates stay two replacement characters.
		if len(s) >= 12 && s[6] == '\\' && s[7] == 'u' {
			if r2, ok2 := hex4(s[8:12]); ok2 {
				if dec := utf16.DecodeRune(rune(r), rune(r2)); dec != utf8.RuneError {
					return dec, 12, true
				}
			}
		}
		return utf8.RuneError, 6, true
	}
	return rune(r), 6, true
}

func hex4(s string) (uint16, bool) {
	var v uint16
	for i := 0; i < 4; i++ {
		c := s[i]
		var d byte
		switch {
		case c >= '0' && c <= '9':
			d = c - '0'
		case c >= 'a' && c <= 'f':
			d = c - 'a' + 10
		case c >= 'A' && c <= 'F':
			d = c - 'A' + 10
		default:
			return 0, false
		}
		v = v<<4 | uint16(d)
	}
	return v, true
}

func skipJSONValue(s string) (string, bool) {
	if len(s) == 0 {
		return "", false
	}
	switch s[0] {
	case '"':
		_, rest, ok := rawJSONString(s)
		return rest, ok
	case '{', '[':
		return skipJSONContainer(s)
	case 't':
		if strings.HasPrefix(s, "true") {
			return s[4:], true
		}
	case 'f':
		if strings.HasPrefix(s, "false") {
			return s[5:], true
		}
	case 'n':
		if strings.HasPrefix(s, "null") {
			return s[4:], true
		}
	default:
		if s[0] == '-' || (s[0] >= '0' && s[0] <= '9') {
			return skipJSONNumber(s)
		}
	}
	return "", false
}

func skipJSONContainer(s string) (string, bool) {
	if len(s) == 0 || (s[0] != '{' && s[0] != '[') {
		return "", false
	}
	closeCh := byte('}')
	if s[0] == '[' {
		closeCh = ']'
	}
	open := s[0]
	s = skipJSONSpaceStr(s[1:])
	if len(s) > 0 && s[0] == closeCh {
		return s[1:], true
	}
	for {
		if open == '{' {
			if len(s) == 0 || s[0] != '"' {
				return "", false
			}
			_, rest, ok := rawJSONString(s)
			if !ok {
				return "", false
			}
			s = skipJSONSpaceStr(rest)
			if len(s) == 0 || s[0] != ':' {
				return "", false
			}
			s = skipJSONSpaceStr(s[1:])
		}
		var ok bool
		s, ok = skipJSONValue(s)
		if !ok {
			return "", false
		}
		s = skipJSONSpaceStr(s)
		if len(s) == 0 {
			return "", false
		}
		switch s[0] {
		case ',':
			s = skipJSONSpaceStr(s[1:])
		case closeCh:
			return s[1:], true
		default:
			return "", false
		}
	}
}

func skipJSONNumber(s string) (string, bool) {
	i := 0
	if i < len(s) && s[i] == '-' {
		i++
	}
	if i >= len(s) || s[i] < '0' || s[i] > '9' {
		return "", false
	}
	if s[i] == '0' {
		i++
	} else {
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	}
	if i < len(s) && s[i] == '.' {
		i++
		start := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == start {
			return "", false
		}
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		start := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == start {
			return "", false
		}
	}
	return s[i:], true
}

func (e *envelopeWriter) writeData(body string) bool {
	return e.writeDataBytes([]byte(body))
}

func (e *envelopeWriter) writeDataBytes(body []byte) bool {
	const prefix = "data: "
	buf := e.frameBuf(len(prefix) + len(body) + 2)
	buf = append(buf, prefix...)
	buf = append(buf, body...)
	return e.writeFrame(buf)
}

// writeSplicedPlainID publishes {"id":quoted} plus the already-checked tail
// without building a second copy of the token delta.
func (e *envelopeWriter) writeSplicedPlainID(quoted string, tail []byte) bool {
	const lead = `data: {"id":`
	buf := e.frameBuf(len(lead) + len(quoted) + len(tail) + 2)
	buf = append(buf, lead...)
	buf = append(buf, quoted...)
	buf = append(buf, tail...)
	return e.writeFrame(buf)
}

func (e *envelopeWriter) frameBuf(n int) []byte {
	buf := e.frame[:0]
	if cap(buf) < n {
		buf = make([]byte, 0, n)
	}
	return buf
}

func (e *envelopeWriter) writeFrame(buf []byte) bool {
	// One frame, one Write. The pipe otherwise wakes the reader three times
	// per token. The buffer is reused; Writer must not retain the slice.
	buf = append(buf, '\n', '\n')
	e.frame = buf
	_, err := e.w.Write(buf)
	return err == nil
}

// plainLeadingCompletionID reports the id of a chunk whose only id key is a
// plain leading string. Any other shape, including a nested "id" inside tool
// arguments, returns false so the structural path can see every top-level copy.
func plainLeadingCompletionID(body string) (string, bool) {
	id, ok := leadingCompletionID([]byte(body))
	if !ok {
		return "", false
	}
	return string(id), true
}

var jsonIDPrefix = []byte(`{"id":"`)

func leadingCompletionID(body []byte) ([]byte, bool) {
	if !bytes.HasPrefix(body, jsonIDPrefix) {
		return nil, false
	}
	rest := body[len(jsonIDPrefix):]
	end := bytes.IndexByte(rest, '"')
	if end <= 0 || bytes.IndexByte(rest[:end], '\\') >= 0 {
		return nil, false
	}
	tail := rest[end+1:]
	if !tailContinuesObject(tail) || tailHasIDOrCodeKey(tail) {
		return nil, false
	}
	return rest[:end], true
}

// tailContinuesObject accepts a plain leading id inside one JSON object.
// object, created, and model sit between id and choices on a normal chunk;
// the prefix splice still rewrites only that leading id. A second id or a
// code key is rejected by the caller. Trailing junk after the object is not
// part of this shape.
func tailContinuesObject(tail []byte) bool {
	if len(tail) == 0 || (tail[0] != ',' && tail[0] != '}') {
		return false
	}
	end := len(tail)
	for end > 0 && isJSONSpace(tail[end-1]) {
		end--
	}
	return end > 0 && tail[end-1] == '}'
}

// tailHasIDOrCodeKey reports a second id or an upstream error key. One pass:
// a nested tool-call id sits before the argument text, and quotes inside
// string values are escaped, so this does not match prose.
func tailHasIDOrCodeKey(tail []byte) bool {
	for {
		i := bytes.IndexByte(tail, '"')
		if i < 0 {
			return false
		}
		tail = tail[i+1:]
		// encoding/json matches object keys without case, so "ID" is a second id.
		if quotedKeyIs(tail, "id") || quotedKeyIs(tail, "code") {
			return true
		}
	}
}

func quotedKeyIs(s []byte, key string) bool {
	if len(s) <= len(key) || s[len(key)] != '"' {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != key[i] {
			return false
		}
	}
	return true
}

func bytesEqualString(b []byte, s string) bool {
	if len(b) != len(s) {
		return false
	}
	for i := range b {
		if b[i] != s[i] {
			return false
		}
	}
	return true
}

// writePlainCompletion publishes a leading plain id. The replacement is
// copied straight into the frame. If it cannot be quoted, the foreign id is
// dropped so the parser keeps the delta instead of aborting the generation.
func (e *envelopeWriter) writePlainCompletion(body, id []byte) bool {
	if e.completionID == "" {
		// id aliases the chunk; copy so the next delta can reuse scratch.
		e.completionID = string(id)
		return e.writeDataBytes(body)
	}
	if bytesEqualString(id, e.completionID) {
		return e.writeDataBytes(body)
	}
	quoted := e.quotedCompletionID()
	tail := body[len(jsonIDPrefix)+len(id)+1:]
	if !e.idRewritten {
		e.noteIDRewritten(string(id))
	}
	if quoted == "" {
		if len(tail) > 0 && tail[0] == ',' {
			tail = tail[1:]
		}
		buf := make([]byte, 0, 1+len(tail))
		buf = append(buf, '{')
		buf = append(buf, tail...)
		return e.writeDataBytes(buf)
	}
	return e.writeSplicedPlainID(quoted, tail)
}

// completionIDText reads a completion id. null, numbers, and empty values are
// not ids; they must not fail the chunk, or its delta would be dropped.
func completionIDText(raw json.RawMessage) string {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	var id string
	if json.Unmarshal(raw, &id) != nil || strings.TrimSpace(id) == "" {
		return ""
	}
	// Keep the bytes the first chunk already published. Trimming here would
	// make the next chunk a different id and abort the generation.
	return id
}

// stableCompletionBody publishes one completion id for this HTTP response.
// The first non-empty string id is kept. A later chunk that carries a
// different id is the same generation under the gateway's other label: only
// that top-level field is spliced, so tool arguments, nested ids, and numeric
// usage stay byte-for-byte. A null or numeric id is not a second completion;
// it is published as the id already chosen, or omitted when none exists yet.
// Chunks with no id pass through; this does not invent an id.
func (e *envelopeWriter) stableCompletionBody(body string, raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) > 0 && raw[0] != '"' {
		if rewritten, ok := e.publishNonStringID(body); ok {
			return rewritten
		}
		return body
	}
	id := completionIDText(raw)
	if id == "" {
		return body
	}
	if e.completionID == "" {
		e.completionID = strings.Clone(id)
		return body
	}
	if id == e.completionID {
		return body
	}
	if rewritten, ok := spliceTopLevelJSONString(body, "id", e.quotedCompletionID()); ok {
		e.noteIDRewritten(id)
		return rewritten
	}
	// The byte splice keeps tool arguments and usage integers intact. If the
	// chunk is still one JSON object but that scan cannot see the id, replace
	// the field structurally. Publishing the original body would hand the
	// OpenAI parser a second completion id and abort the generation.
	if rewritten, ok := rewriteTopLevelIDField(body, e.completionID); ok {
		e.noteIDRewritten(id)
		return rewritten
	}
	// A parseable object still carries the delta. Dropping the foreign id
	// keeps that delta; publishing it would abort the generation.
	if rewritten, ok := omitTopLevelID(body); ok {
		e.noteIDRewritten(id)
		return rewritten
	}
	if !e.idSpliceFailed {
		log.Printf("[qoder-transport] completion id %q could not be applied over upstream chunk id %q", e.completionID, id)
		e.idSpliceFailed = true
	}
	return body
}

// omitTopLevelID removes the top-level id and leaves every other value raw.
func omitTopLevelID(body string) (string, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &obj); err != nil {
		return "", false
	}
	key, _, ok := topLevelIDField(obj)
	if !ok {
		return "", false
	}
	delete(obj, key)
	out, err := marshalAgentChatJSON(obj)
	if err != nil {
		return "", false
	}
	return string(out), true
}

// rewriteTopLevelIDField replaces the top-level id and leaves every other
// value as raw JSON, so nested tool-call ids and non-float integers survive.
func rewriteTopLevelIDField(body, id string) (string, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &obj); err != nil {
		return "", false
	}
	key, raw, ok := topLevelIDField(obj)
	if !ok {
		return "", false
	}
	var current string
	if err := json.Unmarshal(raw, &current); err != nil || current == "" {
		return "", false
	}
	quoted := quoteJSONString(id)
	if quoted == "" {
		return "", false
	}
	if key != "id" {
		delete(obj, key)
	}
	obj["id"] = json.RawMessage(quoted)
	out, err := marshalAgentChatJSON(obj)
	if err != nil {
		return "", false
	}
	return string(out), true
}

// publishNonStringID keeps a chunk whose id is null or a number. The OpenAI
// parser rejects that value and drops the whole chunk, including its delta.
func (e *envelopeWriter) publishNonStringID(body string) (string, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &obj); err != nil {
		return "", false
	}
	key, _, ok := topLevelIDField(obj)
	if !ok {
		return body, true
	}
	if e.completionID == "" {
		delete(obj, key)
	} else {
		quoted := e.quotedCompletionID()
		if key != "id" || quoted == "" {
			delete(obj, key)
		}
		if quoted != "" {
			obj["id"] = json.RawMessage(quoted)
		}
		if !e.idRewritten {
			log.Printf("[qoder-transport] completion id kept as %q; upstream chunk id is not a string", e.completionID)
			e.idRewritten = true
		}
	}
	out, err := marshalAgentChatJSON(obj)
	if err != nil {
		return "", false
	}
	return string(out), true
}

func topLevelIDField(obj map[string]json.RawMessage) (string, json.RawMessage, bool) {
	if raw, ok := obj["id"]; ok {
		return "id", raw, true
	}
	for key, raw := range obj {
		if strings.EqualFold(key, "id") {
			return key, raw, true
		}
	}
	return "", nil, false
}

func (e *envelopeWriter) noteIDRewritten(upstreamID string) {
	if e.idRewritten {
		return
	}
	log.Printf("[qoder-transport] completion id kept as %q; upstream chunk id %q belongs to the same response", e.completionID, upstreamID)
	e.idRewritten = true
}

func (e *envelopeWriter) quotedCompletionID() string {
	if e.completionIDQuoted == "" {
		e.completionIDQuoted = quoteJSONString(e.completionID)
	}
	return e.completionIDQuoted
}

func quoteJSONString(value string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		fallback, _ := json.Marshal(value)
		return string(fallback)
	}
	return strings.TrimRight(buf.String(), "\n")
}

// replaceTopLevelJSONString splices every top-level string value of key.
// Nested keys are left untouched. The replacement is encoded as a JSON string.
func replaceTopLevelJSONString(body, key, value string) (string, bool) {
	return spliceTopLevelJSONString(body, key, quoteJSONString(value))
}

func spliceTopLevelJSONString(body, key, quoted string) (string, bool) {
	out, ok := spliceTopLevelJSONBytes([]byte(body), key, quoted)
	if !ok {
		return "", false
	}
	return string(out), true
}

func spliceTopLevelJSONBytes(body []byte, key, quoted string) ([]byte, bool) {
	spans := topLevelJSONStringSpans(body, key)
	if len(spans) == 0 || quoted == "" {
		return nil, false
	}
	extra := 0
	for _, sp := range spans {
		extra += len(quoted) - (sp[1] - sp[0])
	}
	out := make([]byte, 0, len(body)+extra)
	prev := 0
	for _, sp := range spans {
		if sp[0] < prev || sp[1] < sp[0] || sp[1] > len(body) {
			return nil, false
		}
		out = append(out, body[prev:sp[0]]...)
		out = append(out, quoted...)
		prev = sp[1]
	}
	out = append(out, body[prev:]...)
	return out, true
}

// topLevelJSONStringSpans returns [start, end) offsets of each top-level
// string value for key, including the surrounding quotes.
func topLevelJSONStringSpans(body []byte, key string) [][2]int {
	i := 0
	n := len(body)
	for i < n && isJSONSpace(body[i]) {
		i++
	}
	if i >= n || body[i] != '{' {
		return nil
	}
	i++
	depth := 1
	var spans [][2]int
	for i < n && depth > 0 {
		switch body[i] {
		case '"':
			end, ok := scanJSONStringEnd(body, i)
			if !ok {
				return nil
			}
			if depth == 1 && jsonStringTokenIs(body[i:end], key) {
				j := end
				for j < n && isJSONSpace(body[j]) {
					j++
				}
				if j < n && body[j] == ':' {
					j++
					for j < n && isJSONSpace(body[j]) {
						j++
					}
					if j < n && body[j] == '"' {
						valEnd, ok := scanJSONStringEnd(body, j)
						if !ok {
							return nil
						}
						spans = append(spans, [2]int{j, valEnd})
						i = valEnd
						continue
					}
				}
			}
			i = end
		case '{', '[':
			depth++
			i++
		case '}', ']':
			depth--
			i++
		default:
			i++
		}
	}
	return spans
}

func scanJSONStringEnd(body []byte, i int) (int, bool) {
	if i >= len(body) || body[i] != '"' {
		return 0, false
	}
	i++
	for i < len(body) {
		switch body[i] {
		case '\\':
			if i+1 >= len(body) {
				return 0, false
			}
			i += 2
		case '"':
			return i + 1, true
		default:
			i++
		}
	}
	return 0, false
}

func jsonStringTokenIs(token []byte, want string) bool {
	if len(token) >= 2 && token[0] == '"' && token[len(token)-1] == '"' && bytesEqualString(token[1:len(token)-1], want) {
		return true
	}
	decoded, ok := decodeJSONStringToken(token)
	if !ok {
		return false
	}
	// Object keys are case-insensitive to encoding/json. A second "ID"
	// would otherwise survive the splice and abort the generation.
	return decoded == want || (want == "id" && strings.EqualFold(decoded, want))
}

func decodeJSONStringToken(token []byte) (string, bool) {
	var s string
	if err := json.Unmarshal(token, &s); err != nil {
		return "", false
	}
	return s, true
}

func isJSONSpace(b byte) bool {
	return b == ' ' || b == '\n' || b == '\r' || b == '\t'
}

// jsonCodeString reads a JSON string or number. A number must not fail the
// surrounding object: Qoder sends billing code 110 as either shape, and a
// string field rejects the number and drops the whole denial.
func jsonCodeString(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return ""
		}
		return strings.TrimSpace(s)
	}
	if raw[0] == '-' || (raw[0] >= '0' && raw[0] <= '9') {
		var n json.Number
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if dec.Decode(&n) != nil {
			return ""
		}
		return n.String()
	}
	return ""
}

func (e *envelopeWriter) writeErrorFrame(code, message string) bool {
	errObj := map[string]any{"message": message, "type": "upstream_error"}
	if code = strings.TrimSpace(code); code != "" {
		errObj["code"] = code
	}
	payload, err := json.Marshal(map[string]any{"error": errObj})
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
	sink := &aggregateSink{content: content}
	reader := bufio.NewReaderSize(r, 32*1024)
	buf := make([]byte, 16*1024)
	for {
		n, err := reader.Read(buf)
		if n > 0 && !sink.ingestBytes(buf[:n]) {
			// The error frame is terminal, same as the streaming pump.
			// Flushing the tail would fold the next completion in after it.
			break
		}
		if err != nil {
			if err == io.EOF {
				sink.flushPartial()
			}
			break
		}
	}
	sink.close()
	// A tool-only generation has no text. Treating that as "no output" used to
	// discard the call and surface an upstream tail instead of the completion.
	if sink.errMessage != "" && content.Len() == 0 && sink.reasoning.Len() == 0 && len(sink.toolCalls) == 0 {
		return nil, fmt.Errorf("%s", sink.errMessage)
	}
	message := map[string]any{"role": "assistant", "content": content.String()}
	if sink.reasoning.Len() > 0 {
		message["reasoning_content"] = sink.reasoning.String()
	}
	if calls := sink.assembledToolCalls(); len(calls) > 0 {
		message["tool_calls"] = calls
		// A tool-call generation often finishes on the same chunk that carries
		// the call. Content-only folding used to keep finish_reason=tool_calls
		// and drop the call, so the agent treated the preamble as the answer.
		if sink.finishReason == "" || sink.finishReason == "stop" {
			sink.finishReason = "tool_calls"
		}
	}
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
	// Same encoder as the signed request: default Marshal rewrites '<' inside
	// tool arguments, which inflates a diagram-sized write and is unnecessary
	// on a response we do not sign.
	return marshalAgentChatJSON(resp)
}

// aggregateSink reuses the envelope line parser with an accumulating writer.
type aggregateSink struct {
	content      *strings.Builder
	reasoning    strings.Builder
	finishReason string
	usage        json.RawMessage
	errMessage   string
	writer       envelopeWriter
	toolCalls    map[int]*aggToolCall
}

// aggToolCall accumulates one streamed tool call. Arguments arrive in
// fragments; the call id and name usually arrive once.
type aggToolCall struct {
	id   string
	typ  string
	name string
	args strings.Builder
}

func (a *aggregateSink) ingest(chunk string) bool {
	a.writer.w = a
	return a.writer.ingest(chunk)
}

func (a *aggregateSink) ingestBytes(chunk []byte) bool {
	a.writer.w = a
	return a.writer.ingestBytes(chunk)
}

func (a *aggregateSink) flushPartial() {
	a.writer.w = a
	_ = a.writer.flushPartial()
}

func (a *aggregateSink) Write(p []byte) (int, error) {
	text := strings.TrimSpace(string(p))
	if strings.HasPrefix(text, "data:") {
		text = strings.TrimSpace(strings.TrimPrefix(text, "data:"))
	}
	if text == "" || text == "[DONE]" {
		return len(p), nil
	}
	var chunk struct {
		Code    json.RawMessage `json:"code"`
		Message string          `json:"message"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
		Choices []struct {
			Delta struct {
				Content   string         `json:"content"`
				Reasoning string         `json:"reasoning_content"`
				Reason    string         `json:"reasoning"`
				Thinking  string         `json:"thinking"`
				ToolCalls []aggToolDelta `json:"tool_calls"`
			} `json:"delta"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage json.RawMessage `json:"usage"`
	}
	if json.Unmarshal([]byte(text), &chunk) != nil {
		return len(p), nil
	}
	if code := jsonCodeString(chunk.Code); code != "" {
		a.errMessage = fmt.Sprintf("Qoder 上游错误 (%s): %s", code, chunk.Message)
		return len(p), nil
	}
	if chunk.Error.Message != "" && len(chunk.Choices) == 0 {
		a.errMessage = chunk.Error.Message
		return len(p), nil
	}
	if len(chunk.Choices) > 0 {
		delta := chunk.Choices[0].Delta
		a.content.WriteString(delta.Content)
		switch {
		case delta.Reasoning != "":
			a.reasoning.WriteString(delta.Reasoning)
		case delta.Reason != "":
			a.reasoning.WriteString(delta.Reason)
		case delta.Thinking != "":
			a.reasoning.WriteString(delta.Thinking)
		}
		a.accumulateToolCalls(delta.ToolCalls)
		if chunk.Choices[0].FinishReason != "" {
			a.finishReason = chunk.Choices[0].FinishReason
		}
	}
	if usage := bytes.TrimSpace(chunk.Usage); len(usage) > 0 && !bytes.Equal(usage, []byte("null")) {
		a.usage = append(json.RawMessage(nil), usage...)
	}
	return len(p), nil
}

type aggToolDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

func (a *aggregateSink) accumulateToolCalls(calls []aggToolDelta) {
	for _, call := range calls {
		if call.Index < 0 {
			continue
		}
		acc := a.toolCalls[call.Index]
		if acc == nil {
			acc = &aggToolCall{}
			if a.toolCalls == nil {
				a.toolCalls = map[int]*aggToolCall{}
			}
			a.toolCalls[call.Index] = acc
		}
		if call.ID != "" {
			acc.id = call.ID
		}
		if call.Type != "" {
			acc.typ = call.Type
		}
		if call.Function.Name != "" {
			acc.name = call.Function.Name
		}
		appendToolArguments(&acc.args, call.Function.Arguments)
	}
}

func appendToolArguments(dst *strings.Builder, raw json.RawMessage) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return
	}
	if raw[0] == '"' {
		var text string
		if json.Unmarshal(raw, &text) == nil {
			dst.WriteString(text)
		}
		return
	}
	if dst.Len() == 0 {
		dst.Write(raw)
	}
}

func (a *aggregateSink) assembledToolCalls() []any {
	if len(a.toolCalls) == 0 {
		return nil
	}
	idxs := make([]int, 0, len(a.toolCalls))
	for idx := range a.toolCalls {
		if idx < 0 {
			continue
		}
		idxs = append(idxs, idx)
	}
	sort.Ints(idxs)
	out := make([]any, 0, len(idxs))
	for _, i := range idxs {
		acc := a.toolCalls[i]
		if acc == nil || (acc.name == "" && acc.args.Len() == 0) {
			continue
		}
		typ := acc.typ
		if typ == "" {
			typ = "function"
		}
		out = append(out, map[string]any{
			"id":   acc.id,
			"type": typ,
			"function": map[string]any{
				"name":      acc.name,
				"arguments": acc.args.String(),
			},
		})
	}
	return out
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
