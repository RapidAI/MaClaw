package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestDoOpenAIRequestStream_CompactsAfterToollessCompat400(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/chat/completions" {
			t.Fatalf("path = %q, want /api/v1/chat/completions", r.URL.Path)
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		mu.Lock()
		bodies = append(bodies, body)
		attempt := len(bodies)
		mu.Unlock()

		if attempt < 3 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"compat reject"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"compact ok\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
	}))
	defer server.Close()
	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	client := server.Client()
	client.Transport = rewriteHostRoundTripper{base: client.Transport, target: serverURL}

	cfg := corelib.MaclawLLMConfig{
		URL:          "http://codegen.qianxin-inc.cn/api/v1",
		Model:        "qax-codegen/Auto",
		ProviderName: "CodeGen",
		Protocol:     "openai",
	}
	messages := []interface{}{
		map[string]interface{}{"role": "system", "content": strings.Repeat("runtime context\n", 900) + "\n## 当前任务\n生成测试报告\n"},
		map[string]interface{}{"role": "user", "content": "生成测试策略阶段文档"},
	}
	tools := []map[string]interface{}{{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "write_file",
			"description": "write a file",
			"parameters":  map[string]interface{}{"type": "object"},
		},
	}}

	var streamed strings.Builder
	resp, err := DoOpenAIRequestStream(context.Background(), cfg, messages, tools, client, func(token string) {
		streamed.WriteString(token)
	})
	if err != nil {
		t.Fatalf("DoOpenAIRequestStream returned error: %v", err)
	}
	if resp == nil || len(resp.Choices) == 0 || resp.Choices[0].Message.Content != "compact ok" {
		t.Fatalf("response = %#v, streamed=%q", resp, streamed.String())
	}

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 3 {
		t.Fatalf("request count = %d, want 3", len(bodies))
	}
	if _, ok := bodies[0]["tools"]; !ok {
		t.Fatalf("first request should include tools: %#v", bodies[0])
	}
	if _, ok := bodies[1]["tools"]; ok {
		t.Fatalf("second request should omit tools: %#v", bodies[1])
	}
	if _, ok := bodies[2]["tools"]; ok {
		t.Fatalf("compact request should omit tools: %#v", bodies[2])
	}
	compactMessages, _ := bodies[2]["messages"].([]interface{})
	if len(compactMessages) != 2 {
		t.Fatalf("compact messages len = %d, want 2: %#v", len(compactMessages), compactMessages)
	}
	user, _ := compactMessages[1].(map[string]interface{})
	userContent, _ := user["content"].(string)
	if !strings.Contains(userContent, "[Compatibility retry]") || !strings.Contains(userContent, "生成测试策略阶段文档") {
		t.Fatalf("compact user content = %q", userContent)
	}
}

type rewriteHostRoundTripper struct {
	base   http.RoundTripper
	target *url.URL
}

func (r rewriteHostRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = r.target.Scheme
	clone.URL.Host = r.target.Host
	clone.Host = r.target.Host
	base := r.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(clone)
}
