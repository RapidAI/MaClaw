package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestCallLightweightLLMNormalizesCodeGenAutoModel(t *testing.T) {
	var gotModel string
	handler := &IMMessageHandler{
		client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("Decode: %v", err)
			}
			gotModel, _ = body["model"].(string)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"simple"}}]}`)),
				Request:    r,
			}, nil
		})},
	}

	got := handler.callLightweightLLM(
		corelib.MaclawLLMConfig{URL: "https://codegen.qianxin-inc.cn/api/v1", Model: "auto"},
		"classify",
		"hi",
		2,
	)

	if strings.TrimSpace(got) != "simple" {
		t.Fatalf("response = %q, want simple", got)
	}
	if gotModel != corelib.CodeGenDefaultModelID {
		t.Fatalf("model = %q, want %q", gotModel, corelib.CodeGenDefaultModelID)
	}
}
