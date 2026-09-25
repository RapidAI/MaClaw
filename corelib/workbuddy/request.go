package workbuddy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

const defaultSystemPrompt = "You are a helpful assistant."

var blockedTemplates = []struct{ from, to string }{
	{
		"You are Claude Code, Anthropic's official CLI for Claude.",
		"You are Claude Code, Anthropic's official CLI tool for Claude.",
	},
	{
		"Main branch (you will usually use this for PRs)",
		"Default branch (you will usually use this for PRs)",
	},
}

// ApplyHeaders adds the account and product headers the upstream requires.
func ApplyHeaders(h http.Header, cfg corelib.MaclawLLMConfig) {
	if h == nil || !Matches(cfg) {
		return
	}
	profile, ok := ProfileByName(cfg.ProviderName)
	if !ok {
		profile, ok = ProfileByURL(cfg.URL)
	}
	origin := strings.TrimSpace(cfg.WorkBuddyOrigin)
	if origin == "" && ok {
		origin = profile.Origin
	}
	h.Set("User-Agent", UserAgent)
	h.Set("X-Product", Product)
	h.Set("X-Requested-With", "XMLHttpRequest")
	if origin != "" {
		h.Set("Origin", origin)
		h.Set("Referer", strings.TrimRight(origin, "/")+"/")
	}
	setOrAbsent(h, "X-User-Id", "X-No-User-Id", cfg.WorkBuddyUserID)
	setOrAbsent(h, "X-Enterprise-Id", "X-No-Enterprise-Id", cfg.WorkBuddyEnterpriseID)
	if strings.TrimSpace(cfg.WorkBuddyDomain) != "" {
		h.Set("X-Domain", strings.TrimSpace(cfg.WorkBuddyDomain))
	} else {
		h.Set("X-No-Department-Info", "1")
	}
	if token := strings.TrimSpace(cfg.WorkBuddyRefreshToken); token != "" {
		h.Set("X-Refresh-Token", token)
	}
}

func setOrAbsent(h http.Header, name, absentName, value string) {
	value = strings.TrimSpace(value)
	if value != "" {
		h.Set(name, value)
		h.Del(absentName)
		return
	}
	h.Set(absentName, "1")
}

// PrepareBody forces streaming, a leading system message, template rewrites,
// and the highest thinking level for hy3 models. originalStream is the value
// the caller asked for.
func PrepareBody(payload []byte) (prepared []byte, originalStream bool, ok bool) {
	var obj map[string]any
	if err := json.Unmarshal(payload, &obj); err != nil {
		return payload, false, false
	}
	originalStream, _ = obj["stream"].(bool)
	if !needsRewrite(payload, obj) {
		return payload, originalStream, true
	}
	obj["stream"] = true
	ensureSystemPrompt(obj)
	rewriteMessages(obj)
	forceMaxThinking(obj)
	out, err := json.Marshal(obj)
	if err != nil {
		return payload, originalStream, false
	}
	return out, originalStream, true
}

func needsRewrite(payload []byte, obj map[string]any) bool {
	if stream, _ := obj["stream"].(bool); !stream {
		return true
	}
	if !hasLeadingSystem(obj) {
		return true
	}
	model, _ := obj["model"].(string)
	if strings.HasPrefix(model, "hy3") {
		if effort, _ := obj["reasoning_effort"].(string); effort != "high" {
			return true
		}
	}
	return bytes.Contains(payload, []byte("Anthropic's official CLI for Claude.")) ||
		bytes.Contains(payload, []byte("Main branch (you will usually use this for PRs)"))
}

func hasLeadingSystem(obj map[string]any) bool {
	messages, ok := obj["messages"].([]any)
	if !ok || len(messages) == 0 {
		return false
	}
	msg, ok := messages[0].(map[string]any)
	if !ok {
		return false
	}
	role, _ := msg["role"].(string)
	return role == "system"
}

func ensureSystemPrompt(obj map[string]any) {
	messages, ok := obj["messages"].([]any)
	if !ok {
		return
	}
	if len(messages) > 0 {
		if msg, ok := messages[0].(map[string]any); ok {
			if role, _ := msg["role"].(string); role == "system" {
				return
			}
		}
	}
	obj["messages"] = append([]any{map[string]any{"role": "system", "content": defaultSystemPrompt}}, messages...)
}

func rewriteMessages(obj map[string]any) {
	messages, ok := obj["messages"].([]any)
	if !ok {
		return
	}
	for _, raw := range messages {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch content := msg["content"].(type) {
		case string:
			msg["content"] = sanitizeText(content)
		case []any:
			for _, partRaw := range content {
				part, ok := partRaw.(map[string]any)
				if !ok {
					continue
				}
				if text, ok := part["text"].(string); ok {
					part["text"] = sanitizeText(text)
				}
			}
		}
	}
}

func sanitizeText(s string) string {
	for _, item := range blockedTemplates {
		s = strings.ReplaceAll(s, item.from, item.to)
	}
	return s
}

func forceMaxThinking(obj map[string]any) {
	model, _ := obj["model"].(string)
	if !strings.HasPrefix(model, "hy3") {
		return
	}
	if effort, _ := obj["reasoning_effort"].(string); effort == "high" {
		return
	}
	obj["reasoning_effort"] = "high"
}

// Transport rewrites WorkBuddy chat calls. Non-stream callers receive one
// aggregated chat.completion; stream callers keep the upstream SSE body.
type Transport struct {
	Base http.RoundTripper
}

// WrapClient returns a client whose chat calls to either edition are translated.
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

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	if !shouldAdapt(req) {
		return base.RoundTrip(req)
	}
	payload, err := readBody(req.Body)
	if err != nil {
		return nil, err
	}
	prepared, originalStream, ok := PrepareBody(payload)
	if !ok {
		prepared = payload
	}
	outgoing, err := http.NewRequestWithContext(req.Context(), req.Method, req.URL.String(), bytes.NewReader(prepared))
	if err != nil {
		return nil, err
	}
	outgoing.Header = req.Header.Clone()
	outgoing.Header.Del(MarkerHeader)
	outgoing.ContentLength = int64(len(prepared))
	outgoing.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(prepared)), nil
	}
	resp, err := base.RoundTrip(outgoing)
	if err != nil || resp == nil {
		return resp, err
	}
	wantsStream := originalStream && strings.Contains(strings.ToLower(req.Header.Get("Accept")), "text/event-stream")
	if resp.StatusCode != http.StatusOK || wantsStream {
		return resp, nil
	}
	raw, err := readLimited(resp.Body, maxAggregateBytes)
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	if msg, ok := upstreamBusinessError(raw); ok {
		return upstreamFailure(resp, msg), nil
	}
	if !looksLikeSSE(raw) {
		return replaceBody(resp, raw, resp.Header.Get("Content-Type")), nil
	}
	model := ""
	var probe map[string]any
	if json.Unmarshal(prepared, &probe) == nil {
		model, _ = probe["model"].(string)
	}
	aggregated, err := AggregateSSE(bytes.NewReader(raw), model)
	if err != nil {
		return upstreamFailure(resp, err.Error()), nil
	}
	return replaceBody(resp, aggregated, "application/json"), nil
}

func upstreamBusinessError(raw []byte) (string, bool) {
	var env struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if json.Unmarshal(raw, &env) != nil || env.Code == 0 {
		return "", false
	}
	msg := strings.TrimSpace(env.Msg)
	if msg == "" {
		msg = fmt.Sprintf("上游返回 code=%d", env.Code)
	}
	return msg, true
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

func readBody(body io.ReadCloser) ([]byte, error) {
	if body == nil {
		return nil, nil
	}
	defer body.Close()
	return io.ReadAll(body)
}

func readLimited(body io.Reader, limit int64) ([]byte, error) {
	if body == nil {
		return nil, nil
	}
	raw, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("上游响应超过 %d 字节", limit)
	}
	return raw, nil
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

func shouldAdapt(req *http.Request) bool {
	if req == nil || req.URL == nil || req.Method != http.MethodPost {
		return false
	}
	if !strings.HasSuffix(req.URL.Path, "/chat/completions") {
		return false
	}
	if req.Header.Get(MarkerHeader) == "1" {
		return true
	}
	_, ok := ProfileByURL(req.URL.String())
	return ok
}

func looksLikeSSE(body []byte) bool {
	text := strings.TrimSpace(string(body))
	return strings.HasPrefix(text, "data:") || strings.Contains(text, "\ndata:")
}

// AggregateSSE folds an upstream event stream into one chat.completion body.
func AggregateSSE(r io.Reader, fallbackModel string) ([]byte, error) {
	var (
		role, respModel, respID, finish, streamErr string
		created                                    int64
		usage                                      map[string]any
		toolCalls                                  []map[string]any
		toolIndex                                  = map[int]map[string]any{}
		content                                    strings.Builder
		reasoning                                  strings.Builder
	)
	err := readSSE(r, func(data string) {
		var chunk map[string]any
		if json.Unmarshal([]byte(data), &chunk) != nil {
			return
		}
		if errObj, ok := chunk["error"].(map[string]any); ok {
			if msg, _ := errObj["message"].(string); msg != "" {
				streamErr = msg
			}
		}
		if v, ok := chunk["id"].(string); ok && v != "" {
			respID = v
		}
		if v, ok := chunk["model"].(string); ok && v != "" {
			respModel = v
		}
		if v, ok := chunk["created"].(float64); ok {
			created = int64(v)
		}
		if v, ok := chunk["usage"].(map[string]any); ok {
			usage = v
		}
		choices, _ := chunk["choices"].([]any)
		for _, raw := range choices {
			choice, _ := raw.(map[string]any)
			if delta, ok := choice["delta"].(map[string]any); ok {
				if v, ok := delta["role"].(string); ok && v != "" {
					role = v
				}
				if v, ok := delta["content"].(string); ok {
					content.WriteString(v)
				}
				if v, ok := delta["reasoning_content"].(string); ok {
					reasoning.WriteString(v)
				}
				if calls, ok := delta["tool_calls"].([]any); ok {
					toolCalls = mergeToolCalls(calls, toolIndex, toolCalls)
				}
			}
			if v, ok := choice["finish_reason"].(string); ok && v != "" {
				finish = v
			}
		}
	})
	if err != nil {
		return nil, err
	}
	if streamErr != "" && content.Len() == 0 && len(toolCalls) == 0 {
		return nil, fmt.Errorf("%s", streamErr)
	}
	if role == "" {
		role = "assistant"
	}
	if respModel == "" {
		respModel = fallbackModel
	}
	if respID == "" {
		respID = "chatcmpl-workbuddy"
	}
	if created == 0 {
		created = time.Now().Unix()
	}
	if finish == "" {
		finish = "stop"
	}
	message := map[string]any{"role": role, "content": content.String()}
	if reasoning.Len() > 0 {
		message["reasoning_content"] = reasoning.String()
	}
	if len(toolCalls) > 0 {
		for _, call := range toolCalls {
			delete(call, "index")
		}
		message["tool_calls"] = toolCalls
	}
	result := map[string]any{
		"id":      respID,
		"object":  "chat.completion",
		"created": created,
		"model":   respModel,
		"choices": []map[string]any{{
			"index":         0,
			"message":       message,
			"finish_reason": finish,
		}},
	}
	if usage != nil {
		result["usage"] = usage
	}
	return json.Marshal(result)
}

func readSSE(r io.Reader, fn func(data string)) error {
	buf, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(buf), "\n") {
		data := strings.TrimSpace(line)
		for strings.HasPrefix(data, "data:") {
			data = strings.TrimSpace(strings.TrimPrefix(data, "data:"))
		}
		if data == "" || data == "[DONE]" {
			continue
		}
		fn(data)
	}
	return nil
}

func mergeToolCalls(chunks []any, index map[int]map[string]any, acc []map[string]any) []map[string]any {
	for _, raw := range chunks {
		call, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		idx := 0
		if f, ok := call["index"].(float64); ok {
			idx = int(f)
		}
		merged, exists := index[idx]
		if !exists {
			merged = map[string]any{"index": idx, "type": "function"}
			index[idx] = merged
			acc = append(acc, merged)
		}
		for key, value := range call {
			if key == "index" {
				continue
			}
			if key == "function" {
				incoming, _ := value.(map[string]any)
				current, _ := merged["function"].(map[string]any)
				if current == nil {
					current = map[string]any{}
					merged["function"] = current
				}
				for fk, fv := range incoming {
					if fk == "arguments" {
						if s, ok := fv.(string); ok {
							prev, _ := current["arguments"].(string)
							current["arguments"] = prev + s
							continue
						}
					}
					if s, ok := fv.(string); ok && s != "" {
						current[fk] = fv
					}
				}
				continue
			}
			if s, ok := value.(string); ok && s != "" {
				merged[key] = s
			}
		}
	}
	return acc
}
