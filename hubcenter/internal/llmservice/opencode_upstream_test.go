package llmservice

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func TestTryForwardOpenCodeKeepsPaidOnChatGateway(t *testing.T) {
	var sawPath, sawAuth, sawBody string
	client := openCodeTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		sawAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		sawBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"c1","choices":[{"message":{"role":"assistant","content":"PAID"},"finish_reason":"stop"}]}`)
	})
	body := map[string]any{"model": "glm-5.3-flash", "messages": []any{map[string]any{"role": "user", "content": "hi"}}}
	resp, handled, err := tryForwardOpenCode(context.Background(), client, &llmpool.ProviderConfig{
		ID: "oc", APIURL: "https://opencode.ai/zen/v1", APIKey: "sk-paid", AgentType: "OpenCode",
	}, body, "glm-5.3-flash", "alias", false)
	if err != nil || !handled || resp == nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("handled=%v err=%v resp=%#v", handled, err, resp)
	}
	if sawPath != "/zen/go/v1/chat/completions" || sawAuth != "Bearer sk-paid" {
		t.Fatalf("path=%s auth=%s", sawPath, sawAuth)
	}
	if strings.Contains(sawBody, `"name":"bash"`) || strings.Contains(sawBody, `"stream":true`) {
		t.Fatalf("paid body = %s", sawBody)
	}
	if !strings.Contains(string(resp.Body), "PAID") || !strings.Contains(string(resp.Body), `"model":"alias"`) {
		t.Fatalf("response = %s", resp.Body)
	}
}

func TestTryForwardOpenCodeFreeOmitsKeyAndKeepsCallerBody(t *testing.T) {
	var sawPath, sawAuth, sawBody string
	client := openCodeTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		sawAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		sawBody = string(raw)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"PING\"}}]}\n\ndata: [DONE]\n\n")
	})
	body := map[string]any{"model": "space-bunny-free", "messages": []any{map[string]any{"role": "user", "content": "hi"}}}
	resp, handled, err := tryForwardOpenCode(context.Background(), client, &llmpool.ProviderConfig{
		ID: "oc", APIURL: "https://opencode.ai/zen/go/v1", WireAPI: "responses",
	}, body, "space-bunny-free", "", false)
	if err != nil || !handled || resp == nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("handled=%v err=%v resp=%#v", handled, err, resp)
	}
	if sawPath != "/zen/v1/chat/completions" || sawAuth != "" {
		t.Fatalf("path=%s auth=%q", sawPath, sawAuth)
	}
	if !strings.Contains(sawBody, `"stream":true`) || !strings.Contains(sawBody, `"name":"bash"`) {
		t.Fatalf("free body = %s", sawBody)
	}
	if _, leaked := body["tools"]; leaked {
		t.Fatalf("caller body gained tools: %#v", body)
	}
	if !strings.Contains(string(resp.Body), "PING") {
		t.Fatalf("response = %s", resp.Body)
	}
}

func TestTryForwardOpenCodeFreeRetriesGatewayError(t *testing.T) {
	var hits int
	client := openCodeTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			http.Error(w, "unavailable", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"pong\"}}]}\n\ndata: [DONE]\n\n")
	})
	resp, handled, err := tryForwardOpenCode(context.Background(), client, &llmpool.ProviderConfig{
		ID: "oc", APIURL: "https://opencode.ai/zen/v1", AgentType: "OpenCode",
	}, map[string]any{
		"model": "space-bunny-free", "messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}, "space-bunny-free", "", true)
	if err != nil || !handled || resp == nil || hits != 2 || !strings.Contains(string(resp.Body), "pong") {
		t.Fatalf("hits=%d handled=%v err=%v resp=%#v", hits, handled, err, resp)
	}
}

func TestForwardProviderCompletionCollectsFreeTierStream(t *testing.T) {
	client := openCodeTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/zen/v1/chat/completions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"pong\"}}]}\n\ndata: [DONE]\n\n")
	})
	status, body, err := ForwardProviderCompletion(context.Background(), client, &llmpool.ProviderConfig{
		APIURL: "https://opencode.ai/zen/v1", AgentType: "OpenCode",
	}, map[string]any{
		"model": "space-bunny-free", "messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}, "space-bunny-free")
	if err != nil || status != http.StatusOK || !strings.Contains(string(body), "pong") {
		t.Fatalf("status=%d err=%v body=%s", status, err, body)
	}
}

func TestPlanOpenCodeStreamDoesNotMutateCaller(t *testing.T) {
	body := map[string]any{"model": "space-bunny-free", "stream": true}
	plan, ok, err := planOpenCodeStream(&llmpool.ProviderConfig{
		APIURL: "https://opencode.ai/zen/go/v1", WireAPI: "responses",
	}, body, "space-bunny-free")
	if err != nil || !ok || plan.responses {
		t.Fatalf("ok=%v responses=%v err=%v", ok, plan.responses, err)
	}
	if plan.endpoint != "https://opencode.ai/zen/v1/chat/completions" {
		t.Fatalf("endpoint = %s", plan.endpoint)
	}
	if _, leaked := body["tools"]; leaked {
		t.Fatalf("caller body = %#v", body)
	}
	if !strings.Contains(string(plan.payload), `"name":"read"`) {
		t.Fatalf("payload = %s", plan.payload)
	}
}

func openCodeTestClient(t *testing.T, handler http.HandlerFunc) *http.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client := srv.Client()
	base := client.Transport
	client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		clone := req.Clone(req.Context())
		clone.URL.Scheme = "http"
		clone.URL.Host = srv.Listener.Addr().String()
		if base == nil {
			base = http.DefaultTransport
		}
		return base.RoundTrip(clone)
	})
	return client
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
