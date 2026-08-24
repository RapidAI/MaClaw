package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
)

func TestAdminTestLLMProviderChatRequiresSuccessfulCompletion(t *testing.T) {
	tests := []struct {
		name        string
		response    string
		wantSuccess bool
		wantError   string
	}{
		{
			name:        "completion returned",
			response:    `{"choices":[{"message":{"content":"pong"}}]}`,
			wantSuccess: true,
		},
		{
			name:      "error envelope returned with HTTP 200",
			response:  `{"error":{"message":"model overloaded"}}`,
			wantError: "model returned an error: model overloaded",
		},
		{
			name:      "empty completion returned",
			response:  `{"choices":[{"message":{"content":""}}]}`,
			wantError: "model returned no completion content",
		},
		{
			name:      "malformed response returned",
			response:  `{not json}`,
			wantError: "invalid model response:",
		},
		{
			name:      "anthropic error response returned with HTTP 200",
			response:  `{"type":"error","error":{"type":"overloaded_error","message":"overloaded"}}`,
			wantError: "model returned an error: overloaded",
		},
		{
			name:      "top level error message returned with HTTP 200",
			response:  `{"type":"error","message":"model unavailable"}`,
			wantError: "model returned an error: model unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/chat/completions" {
					t.Errorf("path = %q", r.URL.Path)
				}
				var payload struct {
					Model string `json:"model"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatalf("decode request: %v", err)
				}
				if payload.Model != "selected-model" {
					t.Errorf("model = %q, want selected-model", payload.Model)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.response))
			}))
			defer upstream.Close()

			body, err := json.Marshal(map[string]string{
				"api_url":  upstream.URL + "/v1",
				"model":    "selected-model",
				"protocol": "openai",
			})
			if err != nil {
				t.Fatalf("marshal request: %v", err)
			}
			req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers/test-chat", bytes.NewReader(body))
			rec := httptest.NewRecorder()
			adminTestLLMProviderChat(nil)(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			var got struct {
				Success bool   `json:"success"`
				Error   string `json:"error"`
				Model   string `json:"model"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if got.Success != tt.wantSuccess {
				t.Fatalf("success = %v body=%s", got.Success, rec.Body.String())
			}
			if tt.wantSuccess && got.Model != "selected-model" {
				t.Errorf("model = %q, want selected-model", got.Model)
			}
			if tt.wantError != "" && !strings.Contains(got.Error, tt.wantError) {
				t.Errorf("error = %q, want substring %q", got.Error, tt.wantError)
			}
		})
	}
}

func TestAdminTestLLMProviderChatUsesResponsesAPI(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("path = %q, want /v1/responses", r.URL.Path)
		}
		var payload struct {
			Model  string `json:"model"`
			Input  any    `json:"input"`
			Stream bool   `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		input, _ := json.Marshal(payload.Input)
		if payload.Model != "selected-model" || !strings.Contains(string(input), "Reply with exactly: pong") || payload.Stream {
			t.Errorf("payload = %#v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"pong"}]}]}`))
	}))
	defer upstream.Close()

	body, err := json.Marshal(map[string]string{
		"api_url":  upstream.URL + "/v1",
		"model":    "selected-model",
		"protocol": "openai",
		"wire_api": "responses",
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers/test-chat", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	adminTestLLMProviderChat(nil)(rec, req)

	var got struct {
		Success bool   `json:"success"`
		Reply   string `json:"reply"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if rec.Code != http.StatusOK || !got.Success || got.Reply != "pong" {
		t.Fatalf("status=%d response=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminTestLLMProviderChatUsesAnthropicAPI(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %q, want /v1/messages", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Errorf("x-api-key = %q, want test-key", got)
		}
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Errorf("anthropic-version = %q", got)
		}
		var payload struct {
			Model     string `json:"model"`
			MaxTokens int    `json:"max_tokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload.Model != "selected-model" || payload.MaxTokens != 64 {
			t.Errorf("payload = %#v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"pong"}],"stop_reason":"end_turn"}`))
	}))
	defer upstream.Close()

	body, err := json.Marshal(map[string]string{
		"api_url":  upstream.URL,
		"api_key":  "test-key",
		"model":    "selected-model",
		"protocol": "anthropic",
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers/test-chat", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	adminTestLLMProviderChat(nil)(rec, req)

	var got struct {
		Success bool   `json:"success"`
		Reply   string `json:"reply"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if rec.Code != http.StatusOK || !got.Success || got.Reply != "pong" {
		t.Fatalf("status=%d response=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminTestLLMProviderChatAnthropicThinkingFallback(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %q, want /v1/messages", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"thinking","thinking":"checking reachability"}],"stop_reason":"max_tokens"}`))
	}))
	defer upstream.Close()

	body, err := json.Marshal(map[string]string{
		"api_url":  upstream.URL,
		"model":    "selected-model",
		"protocol": "anthropic",
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers/test-chat", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	adminTestLLMProviderChat(nil)(rec, req)

	var got struct {
		Success bool   `json:"success"`
		Reply   string `json:"reply"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if rec.Code != http.StatusOK || !got.Success || strings.TrimSpace(got.Reply) == "" {
		t.Fatalf("status=%d response=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminTestLLMProviderChatOpenAIReasoningFallback(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			MaxTokens int `json:"max_tokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload.MaxTokens != 64 {
			t.Errorf("max_tokens = %d, want 64", payload.MaxTokens)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"","reasoning_content":"reasoning through the probe"},"finish_reason":"length"}]}`))
	}))
	defer upstream.Close()

	body, err := json.Marshal(map[string]string{
		"api_url":  upstream.URL + "/v1",
		"model":    "selected-model",
		"protocol": "openai",
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers/test-chat", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	adminTestLLMProviderChat(nil)(rec, req)

	var got struct {
		Success bool   `json:"success"`
		Reply   string `json:"reply"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if rec.Code != http.StatusOK || !got.Success || got.Error != "" {
		t.Fatalf("status=%d response=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminTestLLMProviderChatProviderIDDoesNotUseCallerEndpoint(t *testing.T) {
	svc := llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer stored-key" {
			t.Errorf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"pong"}}]}`))
	}))
	defer upstream.Close()
	if err := svc.AddProvider(t.Context(), llmpool.ProviderConfig{
		ID: "configured", Name: "Configured", APIURL: upstream.URL + "/v1", APIKey: "stored-key", Models: []string{"selected-model"},
	}); err != nil {
		t.Fatalf("add provider: %v", err)
	}

	body, err := json.Marshal(map[string]string{
		"provider_id": "configured",
		"api_url":     "https://attacker.example/v1",
		"model":       "selected-model",
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers/test-chat", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	adminTestLLMProviderChat(svc)(rec, req)

	var got struct {
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if rec.Code != http.StatusOK || !got.Success {
		t.Fatalf("status=%d response=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminTestLLMProviderChatRejectsUnknownProviderID(t *testing.T) {
	body := []byte(`{"provider_id":"missing","api_url":"https://attacker.example/v1","model":"selected-model"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers/test-chat", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	svc := llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}})
	adminTestLLMProviderChat(svc)(rec, req)

	var got struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if rec.Code != http.StatusOK || got.Success || got.Error != "provider not found" {
		t.Fatalf("status=%d response=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminTestLLMProviderChatTrimsProviderID(t *testing.T) {
	svc := llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"pong"}}]}`))
	}))
	defer upstream.Close()
	if err := svc.AddProvider(t.Context(), llmpool.ProviderConfig{
		ID: "configured", Name: "Configured", APIURL: upstream.URL + "/v1", Models: []string{"selected-model"},
	}); err != nil {
		t.Fatalf("add provider: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers/test-chat", strings.NewReader(`{"provider_id":" configured ","model":"selected-model"}`))
	rec := httptest.NewRecorder()
	adminTestLLMProviderChat(svc)(rec, req)

	var got struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if rec.Code != http.StatusOK || !got.Success {
		t.Fatalf("status=%d response=%s", rec.Code, rec.Body.String())
	}
}

func TestLLMProviderTestTimeoutUsesConfiguredProviderTimeout(t *testing.T) {
	if got, want := llmProviderTestTimeout(corelib.MaclawLLMConfig{TimeoutSec: 900}), 900*time.Second; got != want {
		t.Fatalf("timeout = %s, want %s", got, want)
	}
}

func TestLLMProviderTestRequestErrorExplainsTimeout(t *testing.T) {
	for _, err := range []error{
		context.DeadlineExceeded,
		&url.Error{Op: "Post", URL: "https://provider.example/v1/chat/completions", Err: context.DeadlineExceeded},
	} {
		got := llmProviderTestRequestError(err, corelib.MaclawLLMConfig{TimeoutSec: 900})
		if !strings.Contains(got, "timed out after 15m0s") {
			t.Fatalf("timeout error = %q", got)
		}
	}
}

func TestReadLLMProviderTestResponse(t *testing.T) {
	t.Run("within limit", func(t *testing.T) {
		body, truncated, err := readLLMProviderTestResponse(strings.NewReader(`{"choices":[{"message":{"content":"pong"}}]}`))
		if err != nil || truncated || !strings.Contains(string(body), "pong") {
			t.Fatalf("body=%q truncated=%v err=%v", body, truncated, err)
		}
	})
	t.Run("over limit", func(t *testing.T) {
		body, truncated, err := readLLMProviderTestResponse(strings.NewReader(strings.Repeat("x", llmProviderTestMaxResponseBytes+1)))
		if err != nil || !truncated || len(body) != llmProviderTestMaxResponseBytes+1 {
			t.Fatalf("len=%d truncated=%v err=%v", len(body), truncated, err)
		}
	})
}

func TestLLMProviderTestResponseReadErrorExplainsTimeout(t *testing.T) {
	got := llmProviderTestResponseReadError(&url.Error{Op: "read", Err: context.DeadlineExceeded}, corelib.MaclawLLMConfig{TimeoutSec: 900})
	if !strings.Contains(got, "provider response timed out after 15m0s") {
		t.Fatalf("timeout error = %q", got)
	}
}

func TestLLMProviderTestHTTPErrorIsUserFacingAndRedactsSecrets(t *testing.T) {
	got := llmProviderTestHTTPError(http.StatusUnauthorized, []byte(`{"error":{"message":"invalid api_key=super-secret-key"}}`))
	if strings.Contains(got, "super-secret-key") {
		t.Fatalf("secret leaked in %q", got)
	}
	if !strings.Contains(got, "HTTP 401") {
		t.Fatalf("unexpected error message %q", got)
	}
}

func TestReadLLMProviderTestResponseReturnsReadError(t *testing.T) {
	want := errors.New("broken response stream")
	_, truncated, err := readLLMProviderTestResponse(errReader{err: want})
	if !errors.Is(err, want) || truncated {
		t.Fatalf("truncated=%v err=%v", truncated, err)
	}
}

type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

var _ io.Reader = errReader{}
