package guiapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestLLMRequestStreamRetriesWhenThinkingOffIsRejected(t *testing.T) {
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
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"choices":[{"delta":{"content":"ok"}}]}`,
			`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`,
			`data: [DONE]`,
			"",
		}, "\n\n")))
	}))
	defer srv.Close()

	resp, err := (&IMMessageHandler{}).doLLMRequestStream(
		context.Background(),
		corelib.MaclawLLMConfig{URL: srv.URL, Model: "custom-thinker", Protocol: "openai", ThinkingMode: "disabled"},
		llmEndpointCategoryMainStream,
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		nil,
		srv.Client(),
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("doLLMRequestStream: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
	if resp == nil || len(resp.Choices) == 0 || resp.Choices[0].Message.Content != "ok" {
		t.Fatalf("response = %#v", resp)
	}
}
