package corelib

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSanitizeDropsHubThinkingWhenMemberAsks(t *testing.T) {
	body := map[string]interface{}{
		"model": "deepseek-v4-flash",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hi"},
		},
	}
	sanitizeOpenAICompatForwardBody(MaclawLLMConfig{
		URL:              "https://api.deepseek.com/v1",
		Model:            "deepseek-v4-flash",
		MemberExtraBody:  map[string]any{"reasoning_effort": "none"},
		MemberDropFields: []string{"thinking"},
	}, body)
	if _, ok := body["thinking"]; ok {
		t.Fatalf("thinking survived: %#v", body["thinking"])
	}
	if body["reasoning_effort"] != "none" {
		t.Fatalf("reasoning_effort = %#v", body["reasoning_effort"])
	}
}

func TestSanitizeStillStampsDeepSeekThinkingWithoutMemberOverrides(t *testing.T) {
	body := map[string]interface{}{
		"model": "deepseek-v4-flash",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hi"},
		},
	}
	sanitizeOpenAICompatForwardBody(MaclawLLMConfig{
		URL:   "https://api.deepseek.com/v1",
		Model: "deepseek-v4-flash",
	}, body)
	thinking, _ := body["thinking"].(map[string]interface{})
	if thinking["type"] != "enabled" {
		t.Fatalf("thinking = %#v", body["thinking"])
	}
}

func TestForwardResponsesCompatAppliesMemberOverrides(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Errorf("decode responses body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp","output":[]}`))
	}))
	defer server.Close()

	_, _, err := forwardResponsesCompat(context.Background(), MaclawLLMConfig{
		URL:     server.URL,
		Key:     "sk-test",
		Model:   "gpt-4.1",
		WireAPI: "responses",
		MemberExtraBody: map[string]any{
			"metadata": map[string]any{"user_id": "member"},
			"stream":   true,
			"thinking": "drop-me",
		},
		MemberDropFields: []string{"thinking", "model"},
	}, map[string]interface{}{
		"model": "gpt-4.1",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hi"},
		},
	}, server.Client(), "")
	if err != nil {
		t.Fatal(err)
	}
	metadata, _ := got["metadata"].(map[string]any)
	if metadata["user_id"] != "member" {
		t.Fatalf("metadata = %#v", got["metadata"])
	}
	if _, ok := got["thinking"]; ok {
		t.Fatalf("thinking = %#v", got["thinking"])
	}
	if got["model"] != "gpt-4.1" || got["stream"] != false {
		t.Fatalf("protected fields = model %#v stream %#v", got["model"], got["stream"])
	}
}

func TestForwardAnthropicCompatAppliesMemberOverrides(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Errorf("decode anthropic body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude","content":[{"type":"text","text":"pong"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer server.Close()

	_, _, err := ForwardOpenAICompatRequest(context.Background(), MaclawLLMConfig{
		URL:      server.URL,
		Key:      "sk-test",
		Model:    "claude-sonnet",
		Protocol: "anthropic",
		MemberExtraBody: map[string]any{
			"metadata": map[string]any{"user_id": "member"},
			"thinking": map[string]any{"type": "disabled"},
		},
		MemberDropFields: []string{"thinking"},
	}, map[string]interface{}{
		"model": "claude-sonnet",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hi"},
		},
		"thinking": map[string]interface{}{"type": "enabled"},
	}, server.Client(), "")
	if got == nil {
		t.Fatalf("upstream was not called: %v", err)
	}
	metadata, _ := got["metadata"].(map[string]any)
	if metadata["user_id"] != "member" {
		t.Fatalf("metadata = %#v body=%#v err=%v", got["metadata"], got, err)
	}
	if _, ok := got["thinking"]; ok {
		t.Fatalf("thinking = %#v", got["thinking"])
	}
	if got["model"] != "claude-sonnet" {
		t.Fatalf("model = %#v", got["model"])
	}
}
