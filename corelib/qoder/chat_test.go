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

func TestChatBodyCarriesAgentRouterContract(t *testing.T) {
	raw, err := chatBodyJSON(ChatRequest{
		Model:    "dfmodel",
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatal(err)
	}
	if body["agent_id"] != "agent_common" || body["task_id"] != "common" || body["session_type"] != "qodercli" || body["chat_task"] != "FREE_INPUT" {
		t.Fatalf("router fields = agent_id:%v task_id:%v session_type:%v chat_task:%v", body["agent_id"], body["task_id"], body["session_type"], body["chat_task"])
	}
	if body["chat_record_id"] == "" || body["chat_record_id"] != body["request_set_id"] {
		t.Fatalf("chat_record_id = %v, request_set_id = %v", body["chat_record_id"], body["request_set_id"])
	}
	if body["data_policy_agreed"] != true || body["source"] != float64(1) || body["stream"] != true {
		t.Fatalf("wire flags = policy:%v source:%v stream:%v", body["data_policy_agreed"], body["source"], body["stream"])
	}
	config, _ := body["model_config"].(map[string]any)
	if config["key"] != "dfmodel" || config["source"] != "system" {
		t.Fatalf("model_config = %#v", config)
	}
	business, _ := body["business"].(map[string]any)
	if business["type"] != "agent" || business["scene"] != "cli" || business["stage"] != "start" || business["product"] != "cli" {
		t.Fatalf("business = %#v", business)
	}
	adapted, stream := prepareAgentChatBody([]byte(`{"model":"qwen3.8-max","stream":false,"session_id":"sess-1","task_id":"","messages":[{"role":"user","content":"hello"}]}`))
	if stream {
		t.Fatal("caller stream flag was rewritten")
	}
	if !strings.Contains(adapted, `"task_id":"common"`) || strings.Contains(adapted, `"task_id":""`) {
		t.Fatalf("transport body dropped the router task: %s", adapted)
	}
	if !strings.Contains(adapted, `"model":"qmodel_38max"`) || !strings.Contains(adapted, `"session_id":"sess-1"`) || strings.Contains(adapted, "qwen3.8-max") {
		t.Fatalf("transport body = %s", adapted)
	}
}

func TestAgentChatModelMatchesSignedCatalogKey(t *testing.T) {
	if got := agentChatModel(`{"model":"qwen3.8-max"}`); got != "qmodel_38max" {
		t.Fatalf("signer model = %s", got)
	}
	if got := agentChatModel(`not-json`); got != "qmodel_38max" {
		t.Fatalf("signer fallback = %s", got)
	}
	adapted, _ := prepareAgentChatBody([]byte(`{"model":"dfmodel","max_completion_tokens":128,"text_content":"dropped","contents":[],"messages":[{"role":"user","content":"hi"}]}`))
	if strings.Contains(adapted, "max_completion_tokens") || strings.Contains(adapted, "text_content") || strings.Contains(adapted, `"contents"`) {
		t.Fatalf("openai-only fields survived: %s", adapted)
	}
	if !strings.Contains(adapted, `"max_tokens":128`) {
		t.Fatalf("output cap was not folded: %s", adapted)
	}
	kept, _ := prepareAgentChatBody([]byte(`{"model":"dfmodel","max_tokens":32,"max_completion_tokens":8,"messages":[{"role":"user","content":"hi"}]}`))
	if !strings.Contains(kept, `"max_tokens":32`) || strings.Contains(kept, "max_completion_tokens") {
		t.Fatalf("explicit cap was replaced: %s", kept)
	}
	cleared, _ := prepareAgentChatBody([]byte(`{"model":"dfmodel","max_tokens":0,"messages":[{"role":"user","content":"hi"}]}`))
	if strings.Contains(cleared, "max_tokens") {
		t.Fatalf("zero cap survived: %s", cleared)
	}
	if agentChatModel(adapted) != "dfmodel" {
		t.Fatalf("signer model = %s", agentChatModel(adapted))
	}
}

func TestChatBodyFallsBackToCatalogModelKey(t *testing.T) {
	raw, err := chatBodyJSON(ChatRequest{
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"model":"qmodel_38max"`) || strings.Contains(raw, "qwen3.8-max") {
		t.Fatalf("empty model leaked a display name: %s", raw)
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
