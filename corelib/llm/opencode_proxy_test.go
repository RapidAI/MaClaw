package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestPrepareOpenCodeChatBodySeparatesPaidAndFree(t *testing.T) {
	paidBody := map[string]interface{}{"model": "glm-5.3-flash", "stream": false}
	paid, ok := PrepareOpenCodeChatBody(corelib.MaclawLLMConfig{
		URL: "https://opencode.ai/zen/v1", Model: "glm-5.3-flash", Key: "sk-paid",
	}, paidBody)
	if !ok || paid.URL != "https://opencode.ai/zen/go/v1" || paid.IsResponsesAPI() {
		t.Fatalf("paid cfg = %+v", paid)
	}
	if strings.Contains(mustJSON(t, paidBody), `"name":"bash"`) || paidBody["stream"] != false {
		t.Fatalf("paid body = %#v", paidBody)
	}

	freeBody := map[string]interface{}{"model": "space-bunny-free", "max_tokens": 65536}
	free, ok := PrepareOpenCodeChatBody(corelib.MaclawLLMConfig{
		URL: "https://opencode.ai/zen/go/v1", Model: "space-bunny-free",
	}, freeBody)
	if !ok || free.URL != "https://opencode.ai/zen/v1" {
		t.Fatalf("free cfg = %+v", free)
	}
	text := mustJSON(t, freeBody)
	for _, want := range []string{`"stream":true`, `"name":"bash"`, `"name":"read"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("free body missing %s: %s", want, text)
		}
	}
	if strings.Contains(text, "max_tokens") {
		t.Fatalf("free body kept the default cap: %s", text)
	}

	museBody := map[string]interface{}{
		"model":    "muse-spark-1.2-contributor-free",
		"messages": []interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
	}
	muse, ok := PrepareOpenCodeChatBody(corelib.MaclawLLMConfig{
		URL: "https://opencode.ai/zen/go/v1", Model: "muse-spark-1.2-contributor-free",
	}, museBody)
	if !ok || !muse.IsResponsesAPI() || muse.URL != "https://opencode.ai/zen/v1" {
		t.Fatalf("muse cfg = %+v", muse)
	}
	if _, injected := museBody["tools"]; injected {
		t.Fatalf("responses prepare changed the chat body: %#v", museBody)
	}
}

func TestForwardOpenCodeChatOmitsKeyWhenEmpty(t *testing.T) {
	var sawAuth, sawClient, sawPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		sawClient = r.Header.Get("x-opencode-client")
		sawPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"PING"},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()
	client := srv.Client()
	client.Transport = openCodeRewriteTransport{base: client.Transport, host: srv.Listener.Addr().String()}
	body, status, err := ForwardOpenCodeChat(context.Background(), corelib.MaclawLLMConfig{
		URL: "https://opencode.ai/zen/v1", Model: "glm-5.3-flash", AgentType: "OpenCode",
	}, map[string]interface{}{"model": "glm-5.3-flash", "messages": []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}}, client)
	if err != nil || status != http.StatusOK {
		t.Fatalf("status=%d err=%v body=%s", status, err, body)
	}
	if sawAuth != "" || sawClient != "cli" {
		t.Fatalf("auth=%q client=%q", sawAuth, sawClient)
	}
	if sawPath != "/zen/go/v1/chat/completions" {
		t.Fatalf("path = %s", sawPath)
	}
	if !strings.Contains(string(body), "PING") {
		t.Fatalf("body = %s", body)
	}
}

type openCodeRewriteTransport struct {
	base http.RoundTripper
	host string
}

func (t openCodeRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = "http"
	clone.URL.Host = t.host
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(clone)
}

func TestForwardOpenCodeChatKeepsPartialStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"pong\"}}]}\n\n")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			return
		}
		_ = conn.Close()
	}))
	defer srv.Close()
	client := srv.Client()
	client.Transport = openCodeRewriteTransport{base: client.Transport, host: srv.Listener.Addr().String()}
	body, status, err := ForwardOpenCodeChat(context.Background(), corelib.MaclawLLMConfig{
		URL: "https://opencode.ai/zen/v1", Model: "space-bunny-free", AgentType: "OpenCode",
	}, map[string]interface{}{
		"model": "space-bunny-free", "messages": []interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
	}, client)
	if err != nil || status != http.StatusOK || !strings.Contains(string(body), "pong") {
		t.Fatalf("status=%d err=%v body=%s", status, err, body)
	}
}

func TestMarshalOpenCodeResponseKeepsToolArgumentMarkup(t *testing.T) {
	raw, err := marshalOpenCodeResponse(&Response{Choices: []Choice{{Message: Message{
		Content: "a < b",
		ToolCalls: []ToolCall{{ID: "c1", Type: "function", Function: ToolCallFunction{
			Name: "bash", Arguments: `{"command":"echo <hi>"}`,
		}}},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `\u003c`) || !strings.Contains(string(raw), "echo <hi>") {
		t.Fatalf("body = %s", raw)
	}
}

func mustJSON(t *testing.T, body map[string]interface{}) string {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
