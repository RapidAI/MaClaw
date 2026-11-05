package guiapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
		t.Fatalf("a discovery background turn must not require a preselected screenshot: %+v", resp)
	}
	assertDiscoveryBackgroundTools(t, requestTools)
}

func TestBackgroundDiscoverySkipsSentenceRewrites(t *testing.T) {
	sentences := []string{
		"将以下信息保存到知识库：file-cn.example",
		"向 znsoft@163.com 发送一封邮件",
		"使用 book-pdf skill 编写教程",
	}
	for _, text := range sentences {
		t.Run(text, func(t *testing.T) {
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
				_, _ = w.Write([]byte(`{"id":"resp-background-discovery","choices":[{"message":{"role":"assistant","content":"完成"},"finish_reason":"stop"}]}`))
			}))
			defer server.Close()

			h := NewIMMessageHandlerStandalone(StandaloneConfig{
				UnifiedClassifier: intent.New(intent.Config{Embedder: nil, LLMTimeout: time.Second}),
				LLMConfigFunc: func() corelib.MaclawLLMConfig {
					return corelib.MaclawLLMConfig{URL: server.URL, Model: "test-model", Protocol: "openai"}
				},
				MaxIterationsFunc: func() int { return 1 },
			})
			defer h.memory.Stop()
			h.SetBackgroundLoopManager(NewBackgroundLoopManager(nil))

			resp, handled := h.handleBackgroundIMRoute(IMUserMessage{
				UserID: "background-discovery-user", Platform: "desktop", Text: text,
				RequestID: "background-discovery-request", IsBackground: true,
			}, nil, server.Client(), nil)
			if !handled || resp == nil {
				t.Fatalf("background route handled=%v response=%#v", handled, resp)
			}
			if resp.Error != "" {
				t.Fatalf("sentence rewrite became a required grant: %+v", resp)
			}
			assertDiscoveryBackgroundTools(t, requestTools)
		})
	}
}

func assertDiscoveryBackgroundTools(t *testing.T, requestTools []map[string]interface{}) {
	t.Helper()
	names := map[string]bool{}
	for _, tool := range requestTools {
		fn, _ := tool["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		names[name] = true
	}
	if !names[semanticToolsSearchName] {
		t.Fatalf("discovery background turn omitted tools_search: %#v", requestTools)
	}
	for _, closed := range []string{
		"screenshot", "bash", "write_file", "office", "generate_pdf",
		"mailbox_send", "knowledge_save_text", "current_datetime",
	} {
		if names[closed] {
			t.Fatalf("discovery background turn preselected %s: %#v", closed, requestTools)
		}
	}
}
