package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/intent"
)

func TestBackgroundManagedSemanticTurnClassifiesBeforeLegacyDispatch(t *testing.T) {
	var requestTools []map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var request struct {
			Tools []map[string]interface{} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode model request: %v", err)
		}
		requestTools = request.Tools
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp-background-semantic","choices":[{"message":{"role":"assistant","content":"完成"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	h := NewIMMessageHandlerStandalone(StandaloneConfig{
		UnifiedClassifier: intent.New(intent.Config{
			Embedder:   nil,
			LLMTimeout: time.Second,
			LLMFunc: func(_, _ string) (string, error) {
				return `{"top":[{"skill":"screenshot","score":0.99}]}`, nil
			},
		}),
		LLMConfigFunc: func() corelib.MaclawLLMConfig {
			return corelib.MaclawLLMConfig{URL: server.URL, Model: "test-model", Protocol: "openai"}
		},
		MaxIterationsFunc: func() int { return 1 },
	})
	defer h.memory.Stop()
	h.SetBackgroundLoopManager(NewBackgroundLoopManager(nil))

	resp, handled := h.handleBackgroundIMRoute(IMUserMessage{
		UserID: "background-semantic-user", Platform: "desktop", Text: "截取当前桌面",
		RequestID: "background-semantic-request", IsBackground: true,
	}, nil, server.Client(), nil)
	if !handled || resp == nil {
		t.Fatalf("background route handled=%v response=%#v", handled, resp)
	}
	if resp.Error != "" || resp.Text != "完成" {
		t.Fatalf("background response=%+v", resp)
	}
	if len(requestTools) == 0 {
		t.Fatal("managed background turn sent no tool surface")
	}
	// The governed surface renders the turn's grant plus the grant-less
	// discovery meta-tool; nothing broader may leak into a background turn.
	if len(requestTools) != 2 {
		t.Fatalf("managed background turn published broad tool surface: %#v", requestTools)
	}
	var function map[string]interface{}
	for _, tool := range requestTools {
		fn, _ := tool["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		if name == "screenshot" {
			function = fn
			continue
		}
		if name != semanticToolsSearchName {
			t.Fatalf("managed background turn published unexpected tool %q in %#v", name, requestTools)
		}
	}
	if function == nil {
		t.Fatalf("managed background turn did not publish screenshot: %#v", requestTools)
	}
	description, _ := function["description"].(string)
	if !strings.Contains(strings.ToLower(description), "may briefly leave the list") {
		t.Fatalf("background tool is not described as a turn-scoped grant: %q", description)
	}
	parameters, _ := function["parameters"].(map[string]interface{})
	properties, _ := parameters["properties"].(map[string]interface{})
	if len(properties) != 1 || properties["display"] == nil {
		t.Fatalf("background managed surface exposed legacy-shaped parameters: %#v", parameters)
	}
}
