package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestDoResponsesAPIRequestRetriesTransientServerError(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			http.Error(w, `{"error":"temporary"}`, http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]}`))
	}))
	defer server.Close()

	resp, err := DoResponsesAPIRequest(context.Background(), corelib.MaclawLLMConfig{
		URL:     server.URL + "/v1",
		Model:   "gpt-test",
		WireAPI: "responses",
	}, []interface{}{map[string]string{"role": "user", "content": "hi"}}, nil, server.Client())
	if err != nil {
		t.Fatalf("DoResponsesAPIRequest returned error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if got := resp.Choices[0].Message.Content; got != "ok" {
		t.Fatalf("content = %q, want ok", got)
	}
}
