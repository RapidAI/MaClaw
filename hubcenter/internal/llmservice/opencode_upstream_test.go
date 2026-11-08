package llmservice

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	corellm "github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func TestOpenCodeChatSSEKeepsToolNameAndReasoning(t *testing.T) {
	sse := openCodeChatSSE("deepseek-v4.1-flash-free", "", "think", "tool_calls", []corellm.ToolCall{{
		ID: "call_1", Type: "function",
		Function: corellm.ToolCallFunction{Name: "bash", Arguments: `{"command":"python gen.py"}`},
	}}, nil)
	if !strings.Contains(sse, `"name":"bash"`) || !strings.Contains(sse, `"reasoning_content":"think"`) || strings.Contains(sse, `"content":"think"`) {
		t.Fatalf("sse = %s", sse)
	}
}

func TestOpenCodeZenFreeWireModelRewritesBigPickle(t *testing.T) {
	if got := openCodeZenFreeWireModel([]byte(`{"model":"big-pickle"}`), "big-pickle"); got != "glm-5.3-flash-free" {
		t.Fatalf("payload wire = %s", got)
	}
	if got := openCodeZenFreeWireModel(nil, "opencode/deepseek-v4.1-flash-free"); got != "deepseek-v4.1-flash-free" {
		t.Fatalf("caller wire = %s", got)
	}
	if got := openCodeZenFreeWireModel([]byte(`{"model":"deepseek-v4.1-flash"}`), "deepseek-v4.1-flash"); got != "deepseek-v4.1-flash" {
		t.Fatalf("metered wire = %s", got)
	}
}

func TestPlanOpenCodeStreamAppliesMemberExtraAfterZenRewrite(t *testing.T) {
	plan, ok, err := planOpenCodeStream(&llmpool.ProviderConfig{
		APIURL:     "https://opencode.ai/zen/v1",
		ExtraBody:  map[string]any{"reasoning_effort": "none", "max_tokens": 128},
		DropFields: []string{"thinking"},
	}, map[string]any{
		"model":      "space-bunny-free",
		"max_tokens": 65536,
		"thinking":   map[string]any{"type": "enabled"},
		"messages":   []any{map[string]any{"role": "user", "content": "hi"}},
	}, "space-bunny-free")
	if err != nil || !ok || plan.responses {
		t.Fatalf("ok=%v responses=%v err=%v", ok, plan.responses, err)
	}
	payload := string(plan.payload)
	if !strings.Contains(payload, `"reasoning_effort":"none"`) || !strings.Contains(payload, `"max_tokens":128`) {
		t.Fatalf("payload = %s", payload)
	}
	if strings.Contains(payload, "thinking") {
		t.Fatalf("thinking survived: %s", payload)
	}
}

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
	if sawPath != "/inference/go/openai/v1/chat/completions" || sawAuth != "Bearer sk-paid" {
		t.Fatalf("path=%s auth=%s", sawPath, sawAuth)
	}
	if strings.Contains(sawBody, `"name":"shell"`) || strings.Contains(sawBody, `"stream":true`) {
		t.Fatalf("paid body = %s", sawBody)
	}
	if !strings.Contains(string(resp.Body), "PAID") || !strings.Contains(string(resp.Body), `"model":"alias"`) {
		t.Fatalf("response = %s", resp.Body)
	}
}

func TestTryForwardOpenCodeKeyedFreeModelStaysFree(t *testing.T) {
	var sawPath, sawAuth, sawBody string
	client := openCodeTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		sawAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		sawBody = string(raw)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"PING\"}}]}\n\ndata: [DONE]\n\n")
	})
	body := map[string]any{"model": "deepseek-v4.1-flash-free", "messages": []any{map[string]any{"role": "user", "content": "hi"}}}
	resp, handled, err := tryForwardOpenCode(context.Background(), client, &llmpool.ProviderConfig{
		ID: "oc", APIURL: "https://opencode.ai/zen/go/v1", APIKey: "oc_sk_test", AgentType: "OpenCode",
	}, body, "deepseek-v4.1-flash-free", "", false)
	if err != nil || !handled || resp == nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("handled=%v err=%v resp=%#v", handled, err, resp)
	}
	if sawPath != "/inference/openai/v1/chat/completions" || sawAuth != "Bearer oc_sk_test" {
		t.Fatalf("path=%s auth=%s", sawPath, sawAuth)
	}
	if !strings.Contains(sawBody, `"model":"deepseek-v4.1-flash-free"`) {
		t.Fatalf("body = %s", sawBody)
	}
	metered := strings.ReplaceAll(sawBody, `"model":"deepseek-v4.1-flash-free"`, "")
	if strings.Contains(metered, "deepseek-v4.1-flash") {
		t.Fatalf("free id was rewritten to the metered model: %s", sawBody)
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
	if sawPath != "/inference/openai/v1/chat/completions" || sawAuth != "" {
		t.Fatalf("path=%s auth=%q", sawPath, sawAuth)
	}
	if !strings.Contains(sawBody, `"stream":true`) || !strings.Contains(sawBody, `"name":"shell"`) {
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
		if r.URL.Path != "/inference/openai/v1/chat/completions" {
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
	if plan.endpoint != "https://opencode.ai/inference/openai/v1/chat/completions" {
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

func TestPlanOpenCodeStreamKeepsMemberOutputCap(t *testing.T) {
	cases := []struct {
		name   string
		cap    int64
		sent   int
		want   string
		absent bool
	}{
		{name: "cap equal to the zen default stays", cap: 65536, sent: 65536, want: `"max_tokens":65536`},
		{name: "stream default above the cap clamps", cap: 16384, sent: 65536, want: `"max_tokens":16384`},
		{name: "caller default under a higher cap stays", cap: 100000, sent: 65536, want: `"max_tokens":65536`},
		{name: "smaller caller budget stays", cap: 65536, sent: 128, want: `"max_tokens":128`},
		{name: "no cap still drops the zen default", cap: 0, sent: 65536, absent: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, ok, err := planOpenCodeStream(&llmpool.ProviderConfig{
				APIURL:                    "https://opencode.ai/zen/v1",
				MaxOutputTokensPerRequest: tc.cap,
			}, map[string]any{
				"model":      "space-bunny-free",
				"max_tokens": tc.sent,
				"messages":   []any{map[string]any{"role": "user", "content": "hi"}},
			}, "space-bunny-free")
			if err != nil || !ok || plan.responses {
				t.Fatalf("ok=%v responses=%v err=%v", ok, plan.responses, err)
			}
			payload := string(plan.payload)
			if tc.absent {
				if strings.Contains(payload, "max_tokens") {
					t.Fatalf("zen default survived with no member cap: %s", payload)
				}
				return
			}
			if !strings.Contains(payload, tc.want) {
				t.Fatalf("payload = %s, want %s", payload, tc.want)
			}
		})
	}
}

func TestOpenCodeMuseKeepsMemberOutputCap(t *testing.T) {
	cases := []struct {
		name       string
		cap        int64
		maxTokens  *int
		completion *int
		want       string
		absent     bool
	}{
		{name: "small caller budget", cap: 16384, maxTokens: intPtr(128), want: `"max_output_tokens":128`},
		{name: "over cap", cap: 16384, maxTokens: intPtr(100000), want: `"max_output_tokens":16384`},
		{name: "cap equal to the zen default", cap: 65536, maxTokens: intPtr(65536), want: `"max_output_tokens":65536`},
		{name: "caller default under a higher cap", cap: 100000, maxTokens: intPtr(65536), want: `"max_output_tokens":65536`},
		{name: "no cap still omits the zen default", cap: 0, maxTokens: intPtr(65536), absent: true},
		{name: "no cap keeps a smaller caller budget", cap: 0, maxTokens: intPtr(128), want: `"max_output_tokens":128`},
		{name: "omitted budget becomes the cap", cap: 16384, want: `"max_output_tokens":16384`},
		{name: "omitted budget without a cap stays omitted", cap: 0, absent: true},
		{name: "completion wins over max_tokens", cap: 16384, maxTokens: intPtr(100), completion: intPtr(50), want: `"max_output_tokens":50`},
		{name: "completion alone is the budget", cap: 16384, completion: intPtr(2000), want: `"max_output_tokens":2000`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var saw []string
			provider := &llmpool.ProviderConfig{
				ID:                        "oc",
				APIURL:                    "https://opencode.ai/zen/v1",
				AgentType:                 "OpenCode",
				MaxOutputTokensPerRequest: tc.cap,
			}
			client := openCodeTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				mu.Lock()
				saw = append(saw, string(raw))
				mu.Unlock()
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "event: response.output_text.delta\ndata: {\"delta\":\"pong\"}\n\nevent: response.completed\ndata: {\"response\":{\"id\":\"resp-1\"}}\n\n")
			})
			check := func(kind string) {
				t.Helper()
				mu.Lock()
				defer mu.Unlock()
				if len(saw) == 0 {
					t.Fatalf("%s sent no body", kind)
				}
				got := saw[len(saw)-1]
				if tc.absent {
					if strings.Contains(got, "max_output_tokens") {
						t.Fatalf("%s output budget = %s", kind, got)
					}
					return
				}
				if !strings.Contains(got, tc.want) {
					t.Fatalf("%s body = %s, want %s", kind, got, tc.want)
				}
			}
			body := map[string]any{
				"model":    "muse-spark-1.2-contributor-free",
				"messages": []any{map[string]any{"role": "user", "content": "hi"}},
			}
			if tc.maxTokens != nil {
				body["max_tokens"] = *tc.maxTokens
			}
			if tc.completion != nil {
				body["max_completion_tokens"] = *tc.completion
			}
			resp, handled, err := tryForwardOpenCode(context.Background(), client, provider, body, "muse-spark-1.2-contributor-free", "alias", false)
			if err != nil || !handled || resp == nil || resp.StatusCode != http.StatusOK {
				t.Fatalf("forward handled=%v err=%v resp=%#v", handled, err, resp)
			}
			check("forward")

			streamBody := map[string]any{
				"model":    "muse-spark-1.2-contributor-free",
				"messages": []any{map[string]any{"role": "user", "content": "hi"}},
			}
			if tc.maxTokens != nil {
				streamBody["max_tokens"] = *tc.maxTokens
			}
			if tc.completion != nil {
				streamBody["max_completion_tokens"] = *tc.completion
			}
			streamed, err := streamOpenCodeResponses(context.Background(), client, openCodeMaclawConfig(provider, "muse-spark-1.2-contributor-free"), streamBody, "alias", httptest.NewRecorder())
			if err != nil || streamed == nil || streamed.statusCode != http.StatusOK {
				t.Fatalf("stream err=%v result=%#v", err, streamed)
			}
			check("stream")
		})
	}
}

func intPtr(n int) *int {
	return &n
}
