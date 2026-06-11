package knowledge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVisionDescriberUsesUnifiedOpenAIRequestBuilderForQwen(t *testing.T) {
	var captured map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer vision-key" {
			t.Fatalf("Authorization = %q, want bearer key", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	describer := NewVisionDescriber(&VisionLLMConfig{
		Enabled:   true,
		BaseURL:   srv.URL,
		APIKey:    "vision-key",
		Model:     "qwen-vl-plus",
		MaxTokens: 123,
	}, nil)
	got, err := describer.callVisionAPI(context.Background(), "aW1hZ2U=", "image/png", "describe")
	if err != nil {
		t.Fatalf("callVisionAPI returned error: %v", err)
	}
	if got != "ok" {
		t.Fatalf("response = %q, want ok", got)
	}
	for _, key := range []string{"stream_options", "parallel_tool_calls", "store", "metadata", "response_format", "tool_choice", "function_call", "logprobs", "top_logprobs"} {
		if _, ok := captured[key]; ok {
			t.Fatalf("Qwen vision request leaked %s: %#v", key, captured)
		}
	}
	if got := captured["model"]; got != "qwen-vl-plus" {
		t.Fatalf("model = %#v, want qwen-vl-plus", got)
	}
	if got := captured["max_tokens"]; got != float64(123) {
		t.Fatalf("max_tokens = %#v, want 123", got)
	}
	messages := captured["messages"].([]interface{})
	content := messages[0].(map[string]interface{})["content"].([]interface{})
	if content[1].(map[string]interface{})["type"] != "image_url" {
		t.Fatalf("image_url content missing: %#v", content)
	}
}
