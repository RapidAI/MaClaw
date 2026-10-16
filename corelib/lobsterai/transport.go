package lobsterai

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// MarkerHeader tells the local transport to re-route this chat request. It is
// removed before the request leaves the process.
const MarkerHeader = "X-Maclaw-LobsterAI"

// Transport re-routes OpenAI chat calls onto /api/proxy/v1/chat/completions
// and stamps the desktop client headers. The upstream already speaks
// OpenAI-shaped bodies and SSE, so the response passes through; only
// non-stream callers get aggregation, because the upstream only supports
// streaming (stream:false answers HTTP 500).
type Transport struct {
	Base http.RoundTripper
}

// WrapClient returns a client whose chat calls to LobsterAI are re-routed.
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
// returns the client unchanged for anything outside LobsterAI.
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
	if !shouldRewrite(req) {
		return base.RoundTrip(req)
	}
	payload, err := io.ReadAll(req.Body)
	if err != nil {
		_ = req.Body.Close()
		return nil, err
	}
	_ = req.Body.Close()
	originalStream := wantsStream(payload)
	token, _ := prepareBody(payload)
	outgoing, err := http.NewRequestWithContext(req.Context(), http.MethodPost,
		APIBase+ChatPath, bytes.NewReader(token))
	if err != nil {
		return nil, err
	}
	outgoing.Header = req.Header.Clone()
	applyChatHeaders(outgoing.Header, req.Header.Get("Authorization"))
	outgoing.ContentLength = int64(len(token))
	outgoing.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(token)), nil
	}
	resp, err := base.RoundTrip(outgoing)
	if err != nil || resp == nil {
		return resp, err
	}
	// Errors pass through so callers can classify upstream failures.
	if resp.StatusCode != http.StatusOK {
		return resp, nil
	}
	if originalStream {
		// Business refusals (quota, unknown model) hide inside HTTP 200 as a
		// leading error frame; pass the raw bytes through the filter so the
		// engine receives an OpenAI error chunk instead of an empty answer.
		resp.Body = newStreamErrorFilter(resp.Body)
		return resp, nil
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAggregateBytes))
	_ = resp.Body.Close()
	if err != nil {
		return upstreamFailure(resp, err.Error()), nil
	}
	if frame := sseErrorFrame(raw); frame != "" {
		return upstreamFailure(resp, frame), nil
	}
	if !looksLikeSSE(raw) {
		return replaceBody(raw, resp.Header.Get("Content-Type"), resp), nil
	}
	aggregated, err := aggregateSSE(bytes.NewReader(raw))
	if err != nil {
		return upstreamFailure(resp, err.Error()), nil
	}
	return replaceBody(aggregated, "application/json", resp), nil
}

func shouldRewrite(req *http.Request) bool {
	if req == nil || req.URL == nil || req.Method != http.MethodPost {
		return false
	}
	// An explicit marker wins over URL shape: the caller knows exactly which
	// endpoint carries the translated request.
	if req.Header.Get(MarkerHeader) == "1" {
		return true
	}
	if !strings.HasSuffix(req.URL.Path, "/chat/completions") {
		return false
	}
	return IsAPIBase(req.URL.String())
}

// applyChatHeaders stamps the account + desktop identity headers.
func applyChatHeaders(h http.Header, authorization string) {
	key := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(authorization), "Bearer "))
	h.Del(MarkerHeader)
	if key != "" {
		h.Set("Authorization", "Bearer "+key)
	}
	h.Set("X-LobsterAI-Client-Capabilities", ClientCapabilities)
	h.Set("X-LobsterAI-Client-Version", ClientVersion)
	h.Set("User-Agent", UserAgent)
}

// wantsStream reads the caller's stream flag before it gets forced on.
func wantsStream(payload []byte) bool {
	var obj map[string]any
	if json.Unmarshal(payload, &obj) != nil || obj == nil {
		return false
	}
	stream, _ := obj["stream"].(bool)
	return stream
}

// prepareBody forces stream and normalizes tool_choice. The body already
// speaks OpenAI; nothing else needs rewriting.
func prepareBody(payload []byte) ([]byte, bool) {
	var obj map[string]any
	if err := json.Unmarshal(payload, &obj); err != nil || obj == nil {
		return payload, false
	}
	originalStream, _ := obj["stream"].(bool)
	obj["stream"] = true
	if choice, ok := obj["tool_choice"]; ok {
		switch v := choice.(type) {
		case string:
			if strings.TrimSpace(v) == "" || strings.EqualFold(strings.TrimSpace(v), "none") {
				delete(obj, "tool_choice")
			}
		case nil:
			delete(obj, "tool_choice")
		}
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return payload, originalStream
	}
	return out, originalStream
}

const maxAggregateBytes = 8 << 20

func looksLikeSSE(body []byte) bool {
	text := strings.TrimSpace(string(body))
	return strings.HasPrefix(text, "data:") || strings.Contains(text, "\ndata:")
}

// sseErrorFrame reports the message of a leading {"error":{...}} SSE data
// frame. Business refusals (quota, unknown model) hide inside HTTP 200.
func sseErrorFrame(raw []byte) string {
	for _, line := range strings.Split(string(raw), "\n") {
		text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "data:"))
		if !strings.HasPrefix(text, "{") {
			continue
		}
		var chunk struct {
			Error *struct {
				Message string `json:"message"`
				Type    string `json:"type"`
				Code    int64  `json:"code"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(text), &chunk) == nil && chunk.Error != nil {
			detail := chunk.Error.Message
			if detail == "" {
				detail = chunk.Error.Type
			}
			if chunk.Error.Code != 0 {
				detail = fmt.Sprintf("code=%d %s", chunk.Error.Code, detail)
			}
			return fmt.Sprintf("LobsterAI 上游错误: %s", detail)
		}
	}
	return ""
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
	resp.Body = io.NopCloser(bytes.NewReader(payload))
	resp.ContentLength = int64(len(payload))
	resp.Header.Del("Content-Encoding")
	resp.Header.Set("Content-Type", "application/json")
	return resp
}

func replaceBody(payload []byte, contentType string, resp *http.Response) *http.Response {
	resp.Body = io.NopCloser(bytes.NewReader(payload))
	resp.ContentLength = int64(len(payload))
	resp.Header.Del("Content-Encoding")
	if contentType != "" {
		resp.Header.Set("Content-Type", contentType)
	}
	return resp
}

// aggregateSSE folds the upstream OpenAI-shaped stream (it is already SSE of
// chat.completion.chunk frames) into one chat.completion body.
func aggregateSSE(r io.Reader) ([]byte, error) {
	var (
		id        string
		model     string
		created   int64
		content   strings.Builder
		reasoning strings.Builder
		role      = "assistant"
		finish    = "stop"
		usage     map[string]any
		toolCalls = map[int]map[string]any{}
		toolOrder []int
	)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	sawContent := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var chunk map[string]any
		if json.Unmarshal([]byte(payload), &chunk) != nil {
			continue
		}
		if frame := chunk["error"]; frame != nil {
			if errText, ok := frame.(map[string]any); ok {
				message, _ := errText["message"].(string)
				return nil, fmt.Errorf("LobsterAI 上游错误: %s", message)
			}
		}
		if text, ok := chunk["id"].(string); ok && strings.TrimSpace(text) != "" {
			id = strings.TrimSpace(text)
		}
		if text, ok := chunk["model"].(string); ok && strings.TrimSpace(text) != "" {
			model = strings.TrimSpace(text)
		}
		if num, ok := chunk["created"].(float64); ok && created == 0 {
			created = int64(num)
		}
		if raw, ok := chunk["usage"].(map[string]any); ok {
			usage = raw
		}
		choices, _ := chunk["choices"].([]any)
		for _, rawChoice := range choices {
			choice, _ := rawChoice.(map[string]any)
			if choice == nil {
				continue
			}
			if text, ok := choice["finish_reason"].(string); ok && strings.TrimSpace(text) != "" {
				finish = strings.TrimSpace(text)
			}
			delta, _ := choice["delta"].(map[string]any)
			if delta != nil {
				if text, ok := delta["role"].(string); ok && strings.TrimSpace(text) != "" {
					role = strings.TrimSpace(text)
				}
				if text, ok := delta["content"].(string); ok && text != "" {
					content.WriteString(text)
					sawContent = true
				}
				if text, ok := delta["reasoning_content"].(string); ok && text != "" {
					reasoning.WriteString(text)
				}
				if calls, ok := delta["tool_calls"].([]any); ok {
					mergeToolCallDeltas(calls, toolCalls, &toolOrder)
				}
			}
			// Some upstreams ship the full message instead of deltas.
			if !sawContent {
				if message, ok := choice["message"].(map[string]any); ok {
					if text, ok := message["content"].(string); ok && text != "" {
						content.WriteString(text)
					}
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if id == "" {
		id = fmt.Sprintf("chatcmpl-lobster-%d", time.Now().UnixNano())
	}
	if created == 0 {
		created = time.Now().Unix()
	}
	message := map[string]any{"role": role, "content": content.String()}
	if reasoning.Len() > 0 {
		message["reasoning_content"] = reasoning.String()
	}
	if len(toolOrder) > 0 {
		calls := make([]any, 0, len(toolOrder))
		for _, index := range toolOrder {
			call := toolCalls[index]
			delete(call, "index")
			calls = append(calls, call)
		}
		message["tool_calls"] = calls
	}
	resp := map[string]any{
		"id":      id,
		"object":  "chat.completion",
		"created": created,
		"model":   model,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       message,
			"finish_reason": finish,
		}},
	}
	if usage != nil && len(usage) > 0 {
		resp["usage"] = usage
	}
	return json.Marshal(resp)
}

func mergeToolCallDeltas(chunks []any, toolCalls map[int]map[string]any, order *[]int) {
	for _, raw := range chunks {
		call, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		index := 0
		if v, ok := call["index"].(float64); ok {
			index = int(v)
		}
		merged, seen := toolCalls[index]
		if !seen {
			merged = map[string]any{"index": index, "type": "function"}
			toolCalls[index] = merged
			*order = append(*order, index)
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
				for fieldKey, fieldValue := range incoming {
					if fieldKey == "arguments" {
						if text, ok := fieldValue.(string); ok {
							prev, _ := current["arguments"].(string)
							current["arguments"] = prev + text
							continue
						}
					}
					if text, ok := fieldValue.(string); ok && text != "" {
						current[fieldKey] = text
					}
				}
				continue
			}
			if text, ok := value.(string); ok && text != "" {
				merged[key] = text
			}
		}
	}
}
