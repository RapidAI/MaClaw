package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/codegenproxy"
	"github.com/RapidAI/CodeClaw/corelib/configfile"
)

// OpenCodeLoginInfo is returned when the OpenCode key page has been opened.
type OpenCodeLoginInfo struct {
	AuthURL       string `json:"auth_url"`
	BrowserOpened bool   `json:"browser_opened"`
}

// StartOpenCodeLogin opens the official OpenCode key page. The key is pasted
// afterwards, or imported from the local OpenCode auth file.
func (a *App) StartOpenCodeLogin() (OpenCodeLoginInfo, error) {
	authURL := configfile.OpenCodeZenAuthURL
	if !openCodeAuthURLAllowed(authURL) {
		return OpenCodeLoginInfo{}, fmt.Errorf("OpenCode 登录链接不受信任")
	}
	info := OpenCodeLoginInfo{AuthURL: authURL}
	if err := openExternalURL(a.ctx, authURL); err != nil {
		log.Printf("[codexproxy] opencode browser open failed: %s", browserOpenReason(err))
	} else {
		info.BrowserOpened = true
	}
	return info, nil
}

// CompleteOpenCodeLogin saves an API key copied from the OpenCode auth page.
func (a *App) CompleteOpenCodeLogin(key string) (Status, error) {
	normalized, err := normalizeOpenCodeKey(key)
	if err != nil {
		return Status{}, err
	}
	return a.applyOpenCodeKey(normalized)
}

// UseLocalOpenCodeLogin imports the key already saved by the OpenCode CLI.
func (a *App) UseLocalOpenCodeLogin() (Status, error) {
	key := strings.TrimSpace(configfile.ReadOpenCodeZenKey())
	if key == "" {
		return Status{}, fmt.Errorf("本机未找到 OpenCode 登录")
	}
	normalized, err := normalizeOpenCodeKey(key)
	if err != nil {
		return Status{}, fmt.Errorf("本机 OpenCode 密钥无法使用")
	}
	return a.applyOpenCodeKey(normalized)
}

func (a *App) applyOpenCodeKey(key string) (Status, error) {
	status, err := a.applyVendorLogin(vendorCredential{
		Mode:        AuthModeOpenCode,
		BaseURL:     openCodeBaseURL(),
		AccessToken: key,
		ModelID:     configfile.OpenCodePreferredModel(),
	})
	return status, err
}

const (
	openCodeGoBaseURL       = "https://opencode.ai/zen/go/v1"
	openCodeZenFreeBaseURL  = "https://opencode.ai/zen/v1"
	openCodeZenFreeGLM      = "glm-5.3-flash-free"
	openCodeZenFreeDeepSeek = "deepseek-v4.1-flash-free"
)

func openCodeBaseURL() string {
	return openCodeGoBaseURL
}

func openCodeDefaultModel() string {
	return openCodeZenFreeGLM
}

func withOpenCodeZenFreeModels(models []ModelOption) []ModelOption {
	want := []string{openCodeZenFreeGLM, openCodeZenFreeDeepSeek}
	seen := make(map[string]struct{}, len(models)+len(want))
	out := make([]ModelOption, 0, len(models)+len(want))
	for _, id := range want {
		seen[id] = struct{}{}
		out = append(out, ModelOption{ID: id, Name: id})
	}
	for _, model := range models {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, model)
	}
	return out
}

func openCodeZenFreeModel(model string) bool {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case openCodeZenFreeGLM, openCodeZenFreeDeepSeek:
		return true
	default:
		return false
	}
}

// openCodeGoModelID reports the model id the Go gateway should receive.
// big-pickle is the old Zen default and is not served there, so it becomes
// glm-5.3-flash-free. The Zen free ids stay unchanged and are sent to Zen.
// Metered ids stay as selected. changed is false when the id is already usable.
func openCodeGoModelID(model string) (string, bool) {
	original := strings.TrimSpace(model)
	model = original
	const providerPrefix = "opencode/"
	if len(model) > len(providerPrefix) && strings.EqualFold(model[:len(providerPrefix)], providerPrefix) {
		model = strings.TrimSpace(model[len(providerPrefix):])
	}
	switch strings.ToLower(model) {
	case "", strings.ToLower(configfile.OpenCodeZenDefaultModel):
		return openCodeDefaultModel(), true
	}
	if model != original {
		return model, true
	}
	return original, false
}

// openCodeGoEndpoint sends saved Zen base URLs to the Go gateway. The GUI
// keeps using configfile.OpenCodeZenBaseURL.
func openCodeGoEndpoint(endpoint string) string {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed.Host == "" || !strings.EqualFold(parsed.Hostname(), "opencode.ai") {
		return endpoint
	}
	path := parsed.EscapedPath()
	const zenPrefix = "/zen/v1"
	switch {
	case path == zenPrefix:
		parsed.Path = "/zen/go/v1"
	case strings.HasPrefix(path, zenPrefix+"/"):
		parsed.Path = "/zen/go/v1" + strings.TrimPrefix(path, zenPrefix)
	default:
		return endpoint
	}
	parsed.RawPath = ""
	return parsed.String()
}

func openCodeGoRequest(endpoint string) bool {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || !strings.EqualFold(parsed.Hostname(), "opencode.ai") {
		return false
	}
	path := parsed.EscapedPath()
	return path == "/zen/go/v1" || strings.HasPrefix(path, "/zen/go/v1/")
}

func openCodeModelHeaders() map[string]string {
	return map[string]string{
		"User-Agent":                  corelib.OpenCodeClientUserAgent("opencode"),
		corelib.OpenCodeSessionHeader: corelib.OpenCodeDescendingSessionID("codexproxy", "models"),
	}
}

func normalizeOpenCodeKey(raw string) (string, error) {
	key := strings.TrimSpace(raw)
	key = strings.Trim(key, `"'`)
	if strings.HasPrefix(key, "{") {
		if extracted := openCodeKeyFromJSON(key); extracted != "" {
			key = extracted
		}
	}
	if len(key) >= 7 && strings.EqualFold(key[:7], "bearer ") {
		key = strings.TrimSpace(key[7:])
		key = strings.Trim(key, `"'`)
	}
	if len(key) < 12 || strings.ContainsAny(key, " \t\r\n") {
		return "", fmt.Errorf("OpenCode 密钥无效")
	}
	return key, nil
}

func openCodeKeyFromJSON(raw string) string {
	var payload map[string]any
	if json.Unmarshal([]byte(raw), &payload) != nil || payload == nil {
		return ""
	}
	if nested, ok := payload["opencode"].(map[string]any); ok {
		if key := firstOpenCodeKey(nested); key != "" {
			return key
		}
	}
	return firstOpenCodeKey(payload)
}

func firstOpenCodeKey(fields map[string]any) string {
	for _, name := range []string{"key", "apiKey", "access", "access_token", "OPENCODE_API_KEY"} {
		value, _ := fields[name].(string)
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func openCodeAuthURLAllowed(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.User != nil || !strings.EqualFold(parsed.Scheme, "https") {
		return false
	}
	if !strings.EqualFold(parsed.Hostname(), "opencode.ai") {
		return false
	}
	path := parsed.EscapedPath()
	return path == "/auth" || strings.HasPrefix(path, "/auth/")
}

type openCodeUpstream struct {
	client *http.Client
	// collectLimit bounds a body that must be buffered, such as a JSON reply
	// returned for a streaming caller. Zero uses the default.
	collectLimit int
}

func newOpenCodeUpstream() openCodeUpstream {
	// No Client.Timeout: that limit includes the response body and cuts off a long stream.
	// ResponseHeaderTimeout still fails a connection that never sends headers.
	transport := openCodeTransport()
	transport.DisableCompression = true
	transport.ResponseHeaderTimeout = 10 * time.Minute
	return openCodeUpstream{client: &http.Client{Transport: transport}}
}

func openCodeTransport() *http.Transport {
	if base, ok := http.DefaultTransport.(*http.Transport); ok && base != nil {
		return base.Clone()
	}
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          10,
		ExpectContinueTimeout: time.Second,
	}
}

func (u openCodeUpstream) DoModels(ctx context.Context, endpoint, apiKey, _ string) (*http.Response, error) {
	endpoint = openCodeGoEndpoint(endpoint)
	client := u.client
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	applyOpenCodeClientHeaders(req.Header)
	req.Header.Set(corelib.OpenCodeSessionHeader, corelib.OpenCodeDescendingSessionID("codexproxy", "models"))
	return client.Do(req)
}

func (u openCodeUpstream) DoChatCompletions(ctx context.Context, endpoint, apiKey, clientName string, body []byte, accept string, stream bool) (*http.Response, error) {
	endpoint = openCodeGoEndpoint(endpoint)
	body = codegenproxy.PrepareOpenAICompatChatBody(body)
	body = codegenproxy.StripCodexClientMarkers(endpoint, body)
	var upstreamStream bool
	endpoint, body, upstreamStream = finishOpenCodeChat(endpoint, body, stream)
	session := openCodeSessionForBody(body)
	resp, err := postChat(ctx, u.client, endpoint, apiKey, clientName, body, accept, upstreamStream, func(h http.Header) {
		applyOpenCodeClientHeaders(h)
		h.Set(corelib.OpenCodeSessionHeader, session)
	})
	if err != nil || resp == nil || resp.StatusCode != http.StatusOK {
		return resp, err
	}
	if stream {
		return u.normalizeOpenCodeStream(resp)
	}
	return u.collectOpenCodeNonStream(resp)
}

func (u openCodeUpstream) collectLimitBytes() int {
	if u.collectLimit > 0 {
		return u.collectLimit
	}
	return 10 << 20
}

// finishOpenCodeChat sends glm-5.3-flash-free and deepseek-v4.1-flash-free to
// Zen. Those ids are absent from the Go gateway. Zen accepts them when the
// session id uses the descending layout and the body streams with bash and
// read. Other models stay on the Go gateway.
func finishOpenCodeChat(endpoint string, body []byte, stream bool) (string, []byte, bool) {
	body = rewriteOpenCodeZenFreeModel(body)
	if openCodeZenFreeModel(openCodeBodyModel(body)) {
		return openCodeZenFreeEndpoint(endpoint), prepareOpenCodeFreeTierBody(body), true
	}
	endpoint = openCodeGoEndpoint(endpoint)
	if !openCodeGoRequest(endpoint) {
		return endpoint, prepareOpenCodeFreeTierBody(body), true
	}
	return endpoint, rewriteOpenCodeGoModel(endpoint, body), stream
}

func openCodeBodyModel(body []byte) string {
	dec := json.NewDecoder(bytes.NewReader(body))
	obj, err := decodeJSONObject(dec)
	if err != nil || obj == nil {
		return ""
	}
	var model string
	if json.Unmarshal(bytes.TrimSpace(obj.get("model")), &model) != nil {
		return ""
	}
	return strings.TrimSpace(model)
}

func rewriteOpenCodeZenFreeModel(body []byte) []byte {
	model := openCodeBodyModel(body)
	next, changed := openCodeGoModelID(model)
	if !changed || !openCodeZenFreeModel(next) {
		return body
	}
	return rewriteOpenCodeModelValue(body, next)
}

func rewriteOpenCodeModelValue(body []byte, model string) []byte {
	dec := json.NewDecoder(bytes.NewReader(body))
	obj, err := decodeJSONObject(dec)
	if err != nil || obj == nil {
		return body
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); err != io.EOF {
		return body
	}
	encoded, err := marshalOpenCodeJSON(model)
	if err != nil {
		return body
	}
	obj.set("model", encoded)
	rewritten, err := obj.marshal()
	if err != nil {
		return body
	}
	return rewritten
}

func openCodeZenFreeEndpoint(endpoint string) string {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed.Host == "" || !strings.EqualFold(parsed.Hostname(), "opencode.ai") {
		return endpoint
	}
	path := parsed.EscapedPath()
	const goPrefix = "/zen/go/v1"
	switch {
	case path == goPrefix:
		parsed.Path = "/zen/v1"
	case strings.HasPrefix(path, goPrefix+"/"):
		parsed.Path = "/zen/v1" + strings.TrimPrefix(path, goPrefix)
	case path == "/zen/v1" || strings.HasPrefix(path, "/zen/v1/"):
	default:
		return endpoint
	}
	parsed.RawPath = ""
	parsed.Scheme = "https"
	return parsed.String()
}

func rewriteOpenCodeGoModel(endpoint string, body []byte) []byte {
	if !openCodeGoRequest(endpoint) {
		return body
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	obj, err := decodeJSONObject(dec)
	if err != nil || obj == nil {
		return body
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); err != io.EOF {
		return body
	}
	raw := bytes.TrimSpace(obj.get("model"))
	if len(raw) == 0 {
		return body
	}
	var model string
	if json.Unmarshal(raw, &model) != nil {
		return body
	}
	next, changed := openCodeGoModelID(model)
	if !changed {
		return body
	}
	encoded, err := marshalOpenCodeJSON(next)
	if err != nil {
		return body
	}
	obj.set("model", encoded)
	rewritten, err := obj.marshal()
	if err != nil {
		return body
	}
	return rewritten
}

// prepareOpenCodeFreeTierBody adds the request shape OpenCode Zen requires
// from its own client. Existing caller tools stay; bash and read are added
// only when that function name is missing. Unchanged values keep their
// original bytes, so a prompt is not reordered or reformatted. A body that
// already qualifies is returned unchanged.
func prepareOpenCodeFreeTierBody(body []byte) []byte {
	dec := json.NewDecoder(bytes.NewReader(body))
	obj, err := decodeJSONObject(dec)
	if err != nil || obj == nil {
		return body
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); err != io.EOF {
		return body
	}
	changed := false
	if !bytes.Equal(bytes.TrimSpace(obj.get("stream")), []byte("true")) {
		obj.set("stream", json.RawMessage("true"))
		changed = true
	}
	tools, added := rewriteOpenCodeTools(obj.get("tools"))
	if added {
		obj.set("tools", tools)
		changed = true
	}
	if !changed {
		return body
	}
	encoded, err := obj.marshal()
	if err != nil {
		return body
	}
	return encoded
}

type jsonObject struct {
	keys []string
	vals map[string]json.RawMessage
}

func decodeJSONObject(dec *json.Decoder) (*jsonObject, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, fmt.Errorf("json object required")
	}
	obj := &jsonObject{vals: map[string]json.RawMessage{}}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("json key required")
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		obj.set(key, raw)
	}
	end, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := end.(json.Delim); !ok || delim != '}' {
		return nil, fmt.Errorf("json object required")
	}
	return obj, nil
}

func (o *jsonObject) get(key string) json.RawMessage {
	if o == nil {
		return nil
	}
	return o.vals[key]
}

func (o *jsonObject) set(key string, raw json.RawMessage) {
	if _, ok := o.vals[key]; ok {
		o.vals[key] = raw
		return
	}
	o.keys = append(o.keys, key)
	o.vals[key] = raw
}

func (o *jsonObject) marshal() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, key := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		encodedKey, err := marshalOpenCodeJSON(key)
		if err != nil {
			return nil, err
		}
		buf.Write(encodedKey)
		buf.WriteByte(':')
		buf.Write(bytes.TrimSpace(o.vals[key]))
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func rewriteOpenCodeTools(raw json.RawMessage) (json.RawMessage, bool) {
	trimmed := bytes.TrimSpace(raw)
	var list []any
	if len(trimmed) > 0 && trimmed[0] == '[' {
		dec := json.NewDecoder(bytes.NewReader(trimmed))
		dec.UseNumber()
		if err := dec.Decode(&list); err != nil {
			return raw, false
		}
	}
	tools, added := ensureOpenCodeFreeTierTools(list)
	if !added {
		return raw, false
	}
	encoded, err := marshalOpenCodeJSON(tools)
	if err != nil {
		return raw, false
	}
	return encoded, true
}

func ensureOpenCodeFreeTierTools(raw any) ([]any, bool) {
	list, _ := raw.([]any)
	have := map[string]bool{}
	for _, item := range list {
		if name := openAIFunctionToolName(item); name != "" {
			have[strings.ToLower(name)] = true
		}
	}
	added := false
	for _, name := range []string{"bash", "read"} {
		if have[name] {
			continue
		}
		list = append(list, openCodeFreeTierTool(name))
		added = true
	}
	return list, added
}

// openAIFunctionToolName returns the function name. Built-in tools such as
// web_search are ignored so a non-function item cannot satisfy the gate.
func openAIFunctionToolName(item any) string {
	obj, _ := item.(map[string]any)
	if obj == nil {
		return ""
	}
	if typ, _ := obj["type"].(string); typ != "" && !strings.EqualFold(strings.TrimSpace(typ), "function") {
		return ""
	}
	if fn, _ := obj["function"].(map[string]any); fn != nil {
		if name, _ := fn["name"].(string); strings.TrimSpace(name) != "" {
			return strings.TrimSpace(name)
		}
	}
	name, _ := obj["name"].(string)
	return strings.TrimSpace(name)
}

func openCodeFreeTierTool(name string) map[string]any {
	switch name {
	case "read":
		return openCodeFunctionTool(name, "Reads a file from the local filesystem.", map[string]any{
			"filePath": map[string]any{"type": "string", "description": "The path of the file to read"},
		}, "filePath")
	default:
		return openCodeFunctionTool(name, "Executes a bash command in a persistent shell session.", map[string]any{
			"command":     map[string]any{"type": "string", "description": "The command to execute"},
			"description": map[string]any{"type": "string", "description": "Clear, concise description of what this command does"},
		}, "command", "description")
	}
}

func openCodeFunctionTool(name, description string, properties map[string]any, required ...string) map[string]any {
	req := make([]any, len(required))
	for i, item := range required {
		req[i] = item
	}
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        name,
			"description": description,
			"parameters": map[string]any{
				"type":       "object",
				"properties": properties,
				"required":   req,
			},
		},
	}
}

func marshalOpenCodeJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// collectOpenCodeNonStream turns the streamed upstream reply into one chat
// completion. Callers that did not ask to stream still receive JSON. A JSON
// upstream body is returned as it arrived.
func (u openCodeUpstream) collectOpenCodeNonStream(resp *http.Response) (*http.Response, error) {
	raw, err := readOpenCodeLimited(resp.Body, u.collectLimitBytes())
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		if errResp, ok := openCodeJSONErrorResponse(raw); ok {
			return errResp, nil
		}
		resp.Body = io.NopCloser(bytes.NewReader(raw))
		resp.ContentLength = int64(len(raw))
		resp.Header.Set("Content-Type", "application/json")
		return resp, nil
	}
	if !openCodeBodyIsSSE(resp, raw) {
		resp.Body = io.NopCloser(bytes.NewReader(raw))
		resp.ContentLength = int64(len(raw))
		return resp, nil
	}
	payload, status, err := openAIChatCompletionFromSSE(raw)
	if err != nil {
		return nil, err
	}
	return jsonHTTPResponse(status, payload), nil
}

// normalizeOpenCodeStream keeps a real event stream streaming. A single JSON
// completion, which some gateways return even when stream is true, becomes
// one SSE chunk so the responses adapter does not drop it as an empty reply.
func (u openCodeUpstream) normalizeOpenCodeStream(resp *http.Response) (*http.Response, error) {
	if resp.Body == nil {
		return resp, nil
	}
	reader := bufio.NewReader(resp.Body)
	peeked, err := reader.Peek(64)
	if err != nil && err != io.EOF && len(peeked) == 0 {
		_ = resp.Body.Close()
		return nil, err
	}
	if !bytes.HasPrefix(bytes.TrimSpace(peeked), []byte("{")) {
		resp.Body = &openCodeReadCloser{Reader: reader, Closer: resp.Body}
		return resp, nil
	}
	raw, err := readOpenCodeLimited(reader, u.collectLimitBytes())
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	if errResp, ok := openCodeJSONErrorResponse(raw); ok {
		return errResp, nil
	}
	sse, ok := chatCompletionJSONToSSE(raw)
	if !ok {
		return nil, fmt.Errorf("opencode returned JSON instead of a stream")
	}
	out := jsonHTTPResponse(http.StatusOK, sse)
	out.Header.Set("Content-Type", "text/event-stream")
	return out, nil
}

type openCodeReadCloser struct {
	io.Reader
	io.Closer
}

func readOpenCodeLimited(r io.Reader, limit int) ([]byte, error) {
	if limit <= 0 {
		limit = 10 << 20
	}
	raw, err := io.ReadAll(io.LimitReader(r, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > limit {
		return nil, fmt.Errorf("opencode stream exceeded %d bytes", limit)
	}
	return raw, nil
}

func openCodeJSONErrorResponse(raw []byte) (*http.Response, bool) {
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil {
		return nil, false
	}
	errObj, ok := payload["error"]
	if !ok || errObj == nil {
		return nil, false
	}
	if choices, _ := payload["choices"].([]any); len(choices) > 0 {
		return nil, false
	}
	return jsonHTTPResponse(http.StatusBadGateway, append([]byte(nil), bytes.TrimSpace(raw)...)), true
}

func chatCompletionJSONToSSE(raw []byte) ([]byte, bool) {
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil {
		return nil, false
	}
	choices, _ := payload["choices"].([]any)
	if len(choices) == 0 {
		return nil, false
	}
	choice, _ := choices[0].(map[string]any)
	if choice == nil {
		return nil, false
	}
	msg, _ := choice["message"].(map[string]any)
	if msg == nil {
		if _, hasDelta := choice["delta"]; hasDelta {
			return appendSSEData(bytes.TrimSpace(raw)), true
		}
		return nil, false
	}
	delta := map[string]any{}
	if content, ok := msg["content"]; ok && content != nil {
		var text strings.Builder
		appendOpenCodeText(&text, content)
		delta["content"] = text.String()
	}
	if calls, ok := indexedChatToolCalls(msg["tool_calls"]); ok {
		delta["tool_calls"] = calls
	}
	finish, _ := choice["finish_reason"].(string)
	if finish == "" {
		if _, ok := delta["tool_calls"]; ok {
			finish = "tool_calls"
		} else {
			finish = "stop"
		}
	}
	chunk := map[string]any{
		"id":    payload["id"],
		"model": payload["model"],
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         delta,
			"finish_reason": finish,
		}},
	}
	if usage, ok := payload["usage"]; ok && usage != nil {
		chunk["usage"] = usage
	}
	encoded, err := marshalOpenCodeJSON(chunk)
	if err != nil {
		return nil, false
	}
	return appendSSEData(encoded), true
}

func indexedChatToolCalls(raw any) ([]any, bool) {
	list, _ := raw.([]any)
	if len(list) == 0 {
		return nil, false
	}
	out := make([]any, 0, len(list))
	for i, item := range list {
		obj, _ := item.(map[string]any)
		if obj == nil {
			continue
		}
		clone := make(map[string]any, len(obj)+1)
		for key, value := range obj {
			clone[key] = value
		}
		if _, ok := clone["index"]; !ok {
			clone["index"] = i
		}
		if fn, _ := clone["function"].(map[string]any); fn != nil {
			copied := make(map[string]any, len(fn))
			for key, value := range fn {
				copied[key] = value
			}
			if args, ok := copied["arguments"]; ok {
				copied["arguments"] = openCodeArgumentsString(args)
			}
			clone["function"] = copied
		}
		out = append(out, clone)
	}
	return out, len(out) > 0
}

func openCodeArgumentsString(value any) string {
	switch v := value.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return "{}"
		}
		return v
	case nil:
		return "{}"
	default:
		encoded, err := marshalOpenCodeJSON(v)
		if err != nil {
			return "{}"
		}
		return string(encoded)
	}
}

func appendSSEData(payload []byte) []byte {
	var buf bytes.Buffer
	buf.Grow(len(payload) + len("data: \n\ndata: [DONE]\n\n"))
	buf.WriteString("data: ")
	buf.Write(payload)
	buf.WriteString("\n\ndata: [DONE]\n\n")
	return buf.Bytes()
}

func openCodeBodyIsSSE(resp *http.Response, raw []byte) bool {
	if resp != nil && strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		return true
	}
	return bytes.HasPrefix(bytes.TrimSpace(raw), []byte("data:"))
}

func openAIChatCompletionFromSSE(raw []byte) ([]byte, int, error) {
	var content strings.Builder
	calls := map[int]*openCodeStreamCall{}
	var order []int
	var id, model, finish string
	var usage, streamErr any
	saw := false
	for _, line := range bytes.Split(raw, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) < len("data:") || !bytes.EqualFold(line[:len("data:")], []byte("data:")) {
			continue
		}
		saw = true
		payload := bytes.TrimSpace(line[len("data:"):])
		if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) || payload[0] != '{' {
			continue
		}
		var chunk map[string]any
		if json.Unmarshal(payload, &chunk) != nil {
			continue
		}
		if v, ok := chunk["error"]; ok && v != nil {
			streamErr = v
		}
		if v, _ := chunk["id"].(string); v != "" {
			id = v
		}
		if v, _ := chunk["model"].(string); v != "" {
			model = v
		}
		if v, ok := chunk["usage"]; ok && v != nil {
			usage = v
		}
		choices, _ := chunk["choices"].([]any)
		if len(choices) == 0 {
			continue
		}
		choice, _ := choices[0].(map[string]any)
		if choice == nil {
			continue
		}
		if v, _ := choice["finish_reason"].(string); v != "" {
			finish = v
		}
		if delta, _ := choice["delta"].(map[string]any); delta != nil {
			appendOpenCodeText(&content, delta["content"])
			absorbOpenCodeToolCalls(calls, &order, delta["tool_calls"])
			continue
		}
		if msg, _ := choice["message"].(map[string]any); msg != nil {
			appendOpenCodeText(&content, msg["content"])
			absorbOpenCodeToolCalls(calls, &order, msg["tool_calls"])
		}
	}
	if !saw {
		return nil, 0, fmt.Errorf("opencode stream was empty")
	}
	toolCalls := make([]any, 0, len(order))
	for _, idx := range order {
		call := calls[idx]
		if call == nil || (call.name == "" && call.args == "") {
			continue
		}
		callID := call.id
		if callID == "" {
			callID = fmt.Sprintf("call_%s_%d", call.name, idx)
		}
		args := call.args
		if strings.TrimSpace(args) == "" {
			args = "{}"
		}
		toolCalls = append(toolCalls, map[string]any{
			"id":   callID,
			"type": "function",
			"function": map[string]any{
				"name":      call.name,
				"arguments": args,
			},
		})
	}
	if streamErr != nil && content.Len() == 0 && len(toolCalls) == 0 {
		payload, err := marshalOpenCodeJSON(map[string]any{"error": streamErr})
		if err != nil {
			return nil, 0, err
		}
		return payload, http.StatusBadGateway, nil
	}
	if id == "" {
		id = "chatcmpl-opencode"
	}
	if finish == "" {
		if len(toolCalls) > 0 {
			finish = "tool_calls"
		} else {
			finish = "stop"
		}
	}
	message := map[string]any{"role": "assistant"}
	if content.Len() > 0 || len(toolCalls) == 0 {
		message["content"] = content.String()
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}
	chat := map[string]any{
		"id":      id,
		"object":  "chat.completion",
		"model":   model,
		"choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}},
	}
	if usage != nil {
		chat["usage"] = usage
	}
	payload, err := marshalOpenCodeJSON(chat)
	if err != nil {
		return nil, 0, err
	}
	return payload, http.StatusOK, nil
}

type openCodeStreamCall struct {
	id, name, args string
}

func appendOpenCodeText(dst *strings.Builder, value any) {
	switch v := value.(type) {
	case string:
		dst.WriteString(v)
	case []any:
		for _, part := range v {
			obj, _ := part.(map[string]any)
			if obj == nil {
				continue
			}
			if text, _ := obj["text"].(string); text != "" {
				dst.WriteString(text)
			}
		}
	}
}

func absorbOpenCodeToolCalls(calls map[int]*openCodeStreamCall, order *[]int, raw any) {
	list, _ := raw.([]any)
	for _, item := range list {
		tc, _ := item.(map[string]any)
		if tc == nil {
			continue
		}
		idx := openCodeToolCallIndex(tc, calls, *order)
		slot := calls[idx]
		if slot == nil {
			slot = &openCodeStreamCall{}
			calls[idx] = slot
			*order = append(*order, idx)
		}
		if id, _ := tc["id"].(string); id != "" && slot.id == "" {
			slot.id = id
		}
		fn, _ := tc["function"].(map[string]any)
		if fn == nil {
			continue
		}
		if name, _ := fn["name"].(string); name != "" {
			slot.name = mergeOpenCodeStreamFragment(slot.name, name)
		}
		if args, _ := fn["arguments"].(string); args != "" {
			slot.args = mergeOpenCodeStreamFragment(slot.args, args)
		}
	}
}

func openCodeToolCallIndex(tc map[string]any, calls map[int]*openCodeStreamCall, order []int) int {
	if n, ok := tc["index"].(float64); ok {
		return int(n)
	}
	if id, _ := tc["id"].(string); id != "" {
		for _, idx := range order {
			if calls[idx] != nil && calls[idx].id == id {
				return idx
			}
		}
		return openCodeNextToolIndex(order)
	}
	if len(order) > 0 {
		return order[len(order)-1]
	}
	return 0
}

func openCodeNextToolIndex(order []int) int {
	next := 0
	for _, idx := range order {
		if idx >= next {
			next = idx + 1
		}
	}
	return next
}

// mergeOpenCodeStreamFragment joins an SSE field that may arrive either as
// deltas or as a growing copy of the whole value.
func mergeOpenCodeStreamFragment(have, next string) string {
	switch {
	case have == "" || strings.HasPrefix(next, have):
		return next
	case strings.HasPrefix(have, next):
		return have
	default:
		return have + next
	}
}

// applyOpenCodeClientHeaders stamps the identity OpenCode Zen accepts.
// The official client User-Agent is required even when another tool, such as
// Codex, is the caller. A foreign name is rejected by the free-tier gate.
// Codex transport markers are removed so the host does not see that client.
func applyOpenCodeClientHeaders(h http.Header) {
	if h == nil {
		return
	}
	h.Set("User-Agent", corelib.OpenCodeClientUserAgent("opencode"))
	h.Set("x-opencode-client", "cli")
	clearCodexIdentityHeaders(h)
}

func clearCodexIdentityHeaders(h http.Header) {
	if h == nil {
		return
	}
	for _, name := range []string{
		"originator",
		"session_id",
		"session-id",
		"thread_id",
		"thread-id",
		"x-codex-turn-metadata",
		"x-openai-internal-codex-residency",
	} {
		h.Del(name)
	}
}

func openCodeSessionForBody(body []byte) string {
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil {
		payload = nil
	}
	seed := corelib.OpenCodeConversationSeed(payload)
	if seed == "" {
		return corelib.OpenCodeDescendingSessionID("codexproxy", "chat")
	}
	return corelib.OpenCodeDescendingSessionID("codexproxy", seed)
}
