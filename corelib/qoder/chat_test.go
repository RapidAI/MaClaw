package qoder

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestChatProbeBudgetExceedsOAuthClient(t *testing.T) {
	// corelib/oauth keeps token calls at 15s. This probe must outlive that,
	// or Test & Save dies with Client.Timeout while awaiting headers.
	if chatProbeHeaderTimeout <= 15*time.Second {
		t.Fatalf("header budget %s is not longer than the OAuth client", chatProbeHeaderTimeout)
	}
	if chatProbeTotalTimeout < chatProbeHeaderTimeout {
		t.Fatalf("total budget %s is shorter than the header budget %s", chatProbeTotalTimeout, chatProbeHeaderTimeout)
	}
}

func TestChatBodyUsesCatalogModelKey(t *testing.T) {
	raw, err := chatBodyJSON(ChatRequest{
		Model:    "qwen3.8-max",
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"model":"qmodel_38max"`) {
		t.Fatalf("body = %s, want the catalog key", raw)
	}
	if strings.Contains(raw, "qwen3.8-max") {
		t.Fatalf("legacy display name leaked into the signed body: %s", raw)
	}
	marked, err := chatBodyJSON(ChatRequest{
		Model:    "qfmodel",
		Messages: []ChatMessage{{Role: "user", Content: "a<b"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(marked, `\u003c`) {
		t.Fatalf("HTML escaping changed the signed body: %s", marked)
	}
}

// TestChatProbeContract runs ChatProbe against a fake gate that serves the
// observed SSE envelope shape and asserts the signed-wire contract: the
// /algo chat path, the COSY bearer, and the encoded body.
func TestChatProbeContract(t *testing.T) {
	var gotPath, gotAuth, gotXCosy, gotAccept, gotPolicy string
	var gotBodyLen int
	var gotBodyLenValue string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		gotAuth = r.Header.Get("Authorization")
		gotXCosy = r.Header.Get("Cosy-Key")
		gotPolicy = r.Header.Get("Cosy-Data-Policy")
		gotAccept = r.Header.Get("Accept")
		// The body arrives as the wasm's custom-encoded text; record its size
		// and take a prefix for the contract assertion.
		buf := make([]byte, 64)
		n, _ := r.Body.Read(buf)
		gotBodyLen = n
		gotBodyLenValue = string(buf[:n])

		w.Header().Set("Content-Type", "text/event-stream")
		writeChatEvent := func(body string) {
			inner, _ := json.Marshal(body)
			_, _ = w.Write([]byte("data:" + `{"headers":{"Content-Type":["application/json"]},"body":` + string(inner) + "}" + "\n\n"))
		}
		writeChatEvent(`{"choices":[{"delta":{"content":"","role":"assistant"},"index":0}]}`)
		writeChatEvent(`{"choices":[{"delta":{"content":"ok"},"index":0}]}`)
		writeChatEvent(`{"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"credits":0.004},"index":0}`)
		_, _ = w.Write([]byte("data:[DONE]\n\n"))
	}))
	defer server.Close()

	profile := CNProfile()
	profile.ChatOrigin = server.URL
	content, err := ChatProbe(context.Background(), profile, "01a11487-c7fa-7aab-b1b4-0ec70fcd5743", "tok-chat", "qfmodel", "只回复ok")
	if err != nil {
		t.Fatalf("ChatProbe: %v", err)
	}
	if content != "ok" {
		t.Fatalf("content = %q, want ok", content)
	}
	if !strings.HasPrefix(gotPath, "/algo/api/v2/service/pro/sse/agent_chat_generation") {
		t.Fatalf("path %q, want the signed chat route", gotPath)
	}
	if !strings.HasPrefix(gotAuth, "Bearer COSY.") {
		t.Fatalf("Authorization %q, want the wasm COSY bearer", gotAuth)
	}
	if strings.Contains(gotAuth, "tok-chat") {
		t.Fatalf("device token must be folded into the COSY blob, not sent raw: %q", gotAuth)
	}
	if gotXCosy == "" {
		t.Fatalf("Cosy headers missing")
	}
	if gotPolicy != "agree" {
		t.Fatalf("Cosy-Data-Policy = %q, want agree (session carries data_policy_agreed)", gotPolicy)
	}
	if gotAccept != "text/event-stream" {
		t.Fatalf("Accept %q", gotAccept)
	}
	if gotBodyLen == 0 {
		t.Fatalf("empty request body")
	}
	if strings.Contains(gotBodyLenValue, `"role":"user"`) {
		t.Fatalf("body should be wasm-encoded, got plaintext JSON prefix %q", gotBodyLenValue)
	}
	_ = gotBodyLenValue
}

func TestChatProbeReportsHTTPErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model gone", http.StatusUnauthorized)
	}))
	defer server.Close()

	profile := CNProfile()
	profile.ChatOrigin = server.URL
	_, err := ChatProbe(context.Background(), profile, "01a11487-c7fa-7aab-b1b4-0ec70fcd5743", "tok-chat", "qfmodel", "hello")
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") || !strings.Contains(err.Error(), "model gone") {
		t.Fatalf("err = %v, want the status and the response body", err)
	}
}

func TestChatProbeGlobalUsesItsOwnOrigin(t *testing.T) {
	if GlobalProfile().ChatOrigin == CNProfile().ChatOrigin || !strings.Contains(GlobalProfile().ChatOrigin, "api3.qoder.sh") {
		t.Fatalf("global origin = %q, want api3.qoder.sh distinct from the CN gateway", GlobalProfile().ChatOrigin)
	}
	var gotHost string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data:{\"body\":\"{\\\"choices\\\":[{\\\"delta\\\":{\\\"content\\\":\\\"ok\\\"}}]}\"}\n\n"))
	}))
	defer server.Close()

	profile := GlobalProfile()
	profile.ChatOrigin = server.URL
	content, err := ChatProbe(context.Background(), profile, "01a11487-c7fa-7aab-b1b4-0ec70fcd5743", "tok-chat", "qfmodel", "hello")
	if err != nil {
		t.Fatalf("ChatProbe global: %v", err)
	}
	if content != "ok" {
		t.Fatalf("content = %q", content)
	}
	if gotHost != strings.TrimPrefix(server.URL, "http://") {
		t.Fatalf("global probe host = %q, want the international profile origin %q", gotHost, server.URL)
	}
}
