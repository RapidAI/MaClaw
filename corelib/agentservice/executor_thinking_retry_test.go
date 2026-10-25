package agentservice

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestSimpleLLMRequestRetriesWhenThinkingOffIsRejected(t *testing.T) {
	const refusal = `{"error":{"message":"该模型始终思考，不支持关闭思考；请使用 low、high 或 max。"}}`
	for _, wire := range []string{"chat", "responses"} {
		wire := wire
		t.Run(wire, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]interface{}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("decode request: %v", err)
					http.Error(w, "bad json", http.StatusBadRequest)
					return
				}
				n := calls.Add(1)
				thinking, _ := body["thinking"].(map[string]interface{})
				if n == 1 {
					if thinking["type"] != "disabled" {
						t.Errorf("first thinking = %#v, want disabled", body["thinking"])
					}
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(refusal))
					return
				}
				if thinking["type"] != "enabled" {
					t.Errorf("retry thinking = %#v, want enabled", body["thinking"])
				}
				w.Header().Set("Content-Type", "application/json")
				if wire == "responses" {
					_, _ = w.Write([]byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`))
					return
				}
				_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
			}))
			defer srv.Close()

			cfg := corelib.MaclawLLMConfig{
				URL: srv.URL, Key: "test-key", Model: "svc-" + wire + "-thinker", ThinkingMode: "disabled",
			}
			if wire == "responses" {
				cfg.WireAPI = "responses"
			}
			text, err := simpleLLMRequest(context.Background(), cfg, []interface{}{
				map[string]interface{}{"role": "user", "content": "hello"},
			}, srv.Client())
			if err != nil {
				t.Fatalf("simpleLLMRequest: %v", err)
			}
			if text != "ok" {
				t.Fatalf("text = %q, want ok", text)
			}
			if calls.Load() != 2 {
				t.Fatalf("calls = %d, want 2", calls.Load())
			}
		})
	}
}

func TestSimpleLLMRequestDoesNotRetryOrdinaryBadRequest(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"unknown model"}}`))
	}))
	defer srv.Close()

	_, err := simpleLLMRequest(context.Background(), corelib.MaclawLLMConfig{
		URL: srv.URL, Key: "test-key", Model: "svc-plain-bad", ThinkingMode: "disabled",
	}, []interface{}{
		map[string]interface{}{"role": "user", "content": "hello"},
	}, srv.Client())
	if err == nil {
		t.Fatal("expected an error")
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}
