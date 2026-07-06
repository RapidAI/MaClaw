package main

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

type tigerProxyUpstreamRequest struct {
	Path    string
	Headers http.Header
	Body    map[string]interface{}
}

func TestNormalizeListenAddressDefaultsToAllInterfaces(t *testing.T) {
	cases := map[string]string{
		"":            "0.0.0.0:18086",
		":18090":      "0.0.0.0:18090",
		"18091":       "0.0.0.0:18091",
		"localhost:7": "0.0.0.0:7",
		"*:18092":     "0.0.0.0:18092",
	}

	for in, want := range cases {
		got, err := normalizeListenAddress(in)
		if err != nil {
			t.Fatalf("normalizeListenAddress(%q) error: %v", in, err)
		}
		if got != want {
			t.Fatalf("normalizeListenAddress(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeModelOptionsStripsProviderPrefixAndDeduplicates(t *testing.T) {
	models := normalizeModelOptions([]ModelOption{
		{ID: "qax-codegen/Qwen-Flash", Name: "Qwen Flash", ContextWindow: 1000},
		{ID: "Qwen-Flash", Name: "duplicate", ContextWindow: 2000},
		{ID: " qax-codegen/Claude-Sonnet ", Name: "", ContextWindow: 3000},
		{ID: "", Name: "ignored"},
	})

	if len(models) != 2 {
		t.Fatalf("model count = %d, want 2: %+v", len(models), models)
	}
	if models[0].ID != "Qwen-Flash" || models[0].Name != "Qwen Flash" || models[0].ContextWindow != 1000 {
		t.Fatalf("first model = %+v, want normalized Qwen-Flash", models[0])
	}
	if models[1].ID != "Claude-Sonnet" || models[1].Name != "Claude-Sonnet" || models[1].ContextWindow != 3000 {
		t.Fatalf("second model = %+v, want fallback name Claude-Sonnet", models[1])
	}
}

func TestHasStartHiddenArg(t *testing.T) {
	if !hasStartHiddenArg([]string{"--hidden"}) {
		t.Fatal("hasStartHiddenArg should accept --hidden")
	}
	if !hasStartHiddenArg([]string{"/hidden"}) {
		t.Fatal("hasStartHiddenArg should accept /hidden")
	}
	if hasStartHiddenArg([]string{"--other"}) {
		t.Fatal("hasStartHiddenArg should ignore unrelated args")
	}
}

func TestTigerProxyUpstreamRequestsCarryTigerClawClientName(t *testing.T) {
	seen := make(chan http.Header, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","data":[]}`)
	}))
	defer upstream.Close()

	app := NewApp()
	err := app.restartProxy(Settings{
		ListenAddress: "127.0.0.1:0",
		APIKey:        "local-key",
		AccessToken:   "upstream-token",
		BaseURL:       upstream.URL,
	})
	if err != nil {
		t.Fatalf("restartProxy: %v", err)
	}
	defer app.stopProxy()

	req, err := http.NewRequest(http.MethodGet, tigerProxyTestBaseURL(t, app)+"/v1/models", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer local-key")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("proxy status = %d, want 200", resp.StatusCode)
	}

	var headers http.Header
	select {
	case headers = <-seen:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream did not receive proxied request")
	}
	if got := headers.Get("User-Agent"); got != corelib.CodeGenClientName {
		t.Fatalf("upstream User-Agent = %q, want %q", got, corelib.CodeGenClientName)
	}
	if got := headers.Get(corelib.CodeGenClientNameHeader); got != corelib.CodeGenClientName {
		t.Fatalf("upstream %s = %q, want %q", corelib.CodeGenClientNameHeader, got, corelib.CodeGenClientName)
	}
}

func TestTigerProxyOpenAIChatUpstreamServesChatAndResponsesClients(t *testing.T) {
	seen := make(chan tigerProxyUpstreamRequest, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode upstream body: %v", err)
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		seen <- tigerProxyUpstreamRequest{Path: r.URL.Path, Headers: r.Header.Clone(), Body: body}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-tigerproxy","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer upstream.Close()

	app := NewApp()
	err := app.restartProxy(Settings{
		ListenAddress: "127.0.0.1:0",
		APIKey:        "local-key",
		AccessToken:   "upstream-token",
		BaseURL:       upstream.URL,
	})
	if err != nil {
		t.Fatalf("restartProxy: %v", err)
	}
	defer app.stopProxy()

	baseURL := tigerProxyTestBaseURL(t, app)
	client := &http.Client{Timeout: 5 * time.Second}

	chatReq, err := http.NewRequest(http.MethodPost, baseURL+"/v1/chat/completions", strings.NewReader(`{"model":"qax-codegen/Auto","messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatalf("new chat request: %v", err)
	}
	chatReq.Header.Set("Content-Type", "application/json")
	chatReq.Header.Set("Authorization", "Bearer local-key")
	chatResp, err := client.Do(chatReq)
	if err != nil {
		t.Fatalf("chat request: %v", err)
	}
	_ = chatResp.Body.Close()
	if chatResp.StatusCode != http.StatusOK {
		t.Fatalf("chat status = %d, want 200", chatResp.StatusCode)
	}

	responsesReq, err := http.NewRequest(http.MethodPost, baseURL+"/v1/responses", strings.NewReader(`{"model":"qax-codegen/Auto","input":"hi from codex"}`))
	if err != nil {
		t.Fatalf("new responses request: %v", err)
	}
	responsesReq.Header.Set("Content-Type", "application/json")
	responsesReq.Header.Set("Authorization", "Bearer local-key")
	responsesResp, err := client.Do(responsesReq)
	if err != nil {
		t.Fatalf("responses request: %v", err)
	}
	responsesBody, _ := io.ReadAll(responsesResp.Body)
	_ = responsesResp.Body.Close()
	if responsesResp.StatusCode != http.StatusOK {
		t.Fatalf("responses status = %d, want 200; body=%s", responsesResp.StatusCode, responsesBody)
	}
	var responsePayload map[string]interface{}
	if err := json.Unmarshal(responsesBody, &responsePayload); err != nil {
		t.Fatalf("decode responses body: %v; body=%s", err, responsesBody)
	}
	if responsePayload["object"] != "response" {
		t.Fatalf("responses object = %#v, want response; body=%s", responsePayload["object"], responsesBody)
	}

	first := tigerProxyReadUpstreamRequest(t, seen)
	second := tigerProxyReadUpstreamRequest(t, seen)
	for i, req := range []tigerProxyUpstreamRequest{first, second} {
		if req.Path != "/chat/completions" {
			t.Fatalf("upstream request %d path = %q, want /chat/completions", i+1, req.Path)
		}
		if got := req.Headers.Get(corelib.CodeGenClientNameHeader); got != corelib.CodeGenClientName {
			t.Fatalf("upstream request %d %s = %q, want %q", i+1, corelib.CodeGenClientNameHeader, got, corelib.CodeGenClientName)
		}
	}
	if second.Body["messages"] == nil {
		t.Fatalf("responses request was not converted to chat messages: %#v", second.Body)
	}
}

func tigerProxyTestBaseURL(t *testing.T, app *App) string {
	t.Helper()
	var addr net.Addr
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if app != nil {
			app.mu.Lock()
			server := app.server
			app.mu.Unlock()
			if server != nil {
				addr = server.Addr()
			}
		}
		if addr != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if addr == nil {
		t.Fatal("proxy listener address is nil")
	}
	return "http://" + addr.String()
}

func tigerProxyReadUpstreamRequest(t *testing.T, seen <-chan tigerProxyUpstreamRequest) tigerProxyUpstreamRequest {
	t.Helper()
	select {
	case req := <-seen:
		return req
	case <-time.After(5 * time.Second):
		t.Fatal("upstream did not receive proxied request")
	}
	return tigerProxyUpstreamRequest{}
}
