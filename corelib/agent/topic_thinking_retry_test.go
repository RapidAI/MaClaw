package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestConfirmWithLLMRetriesWhenThinkingOffIsRejected(t *testing.T) {
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
			_, _ = w.Write([]byte(`{"error":{"message":"该模型始终思考，不支持关闭思考；请使用 low、high 或 max。"}}`))
			return
		}
		if thinking["type"] != "enabled" {
			t.Errorf("retry thinking = %#v, want enabled", body["thinking"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"new"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	d := NewTopicSwitchDetector(func() (*http.Client, corelib.MaclawLLMConfig) {
		return srv.Client(), corelib.MaclawLLMConfig{
			URL: srv.URL, Key: "test-key", Model: "agent-topic-thinker", ThinkingMode: "disabled",
		}
	})
	d.LLMTimeout = 2 * time.Second
	if got := d.ConfirmWithLLM("old topic", "new question"); got != TopicNew {
		t.Fatalf("ConfirmWithLLM() = %v, want %v", got, TopicNew)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
}
