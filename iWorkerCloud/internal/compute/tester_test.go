package compute

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProviderTester_OpenAI_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("expected Authorization 'Bearer test-key', got %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != "openclaw" {
			t.Errorf("expected User-Agent 'openclaw', got %q", got)
		}
		// Verify request body
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "gpt-4" {
			t.Errorf("expected model gpt-4, got %v", body["model"])
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"model":   "gpt-4-0613",
			"choices": []map[string]any{{"message": map[string]string{"content": "Hi"}}},
		})
	}))
	defer srv.Close()

	tester := &ProviderTester{Client: srv.Client()}
	result := tester.Test(&ComputeProvider{
		BaseURL:   srv.URL,
		APIKey:    "test-key",
		Protocol:  ProtocolOpenAI,
		Model:     "gpt-4",
		UserAgent: "openclaw",
	})

	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if result.Model != "gpt-4-0613" {
		t.Errorf("expected model gpt-4-0613, got %s", result.Model)
	}
	if result.Latency <= 0 {
		t.Error("expected positive latency")
	}
}

func TestProviderTester_Anthropic_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/messages") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "ant-key" {
			t.Errorf("expected x-api-key 'ant-key', got %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer ant-key" {
			t.Errorf("expected Authorization 'Bearer ant-key', got %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Errorf("expected anthropic-version '2023-06-01', got %q", got)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"model": "claude-3-haiku-20240307",
			"content": []map[string]any{
				{"type": "text", "text": "Hi"},
			},
		})
	}))
	defer srv.Close()

	tester := &ProviderTester{Client: srv.Client()}
	result := tester.Test(&ComputeProvider{
		BaseURL:   srv.URL,
		APIKey:    "ant-key",
		Protocol:  ProtocolAnthropic,
		UserAgent: "openclaw",
	})

	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if result.Model != "claude-3-haiku-20240307" {
		t.Errorf("expected model claude-3-haiku-20240307, got %s", result.Model)
	}
}

func TestProviderTester_Gemini_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/models/gemini-pro:generateContent") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("key"); got != "gem-key" {
			t.Errorf("expected key query param 'gem-key', got %q", got)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"modelVersion": "gemini-pro-001",
			"candidates": []map[string]any{
				{"content": map[string]any{"parts": []map[string]string{{"text": "Hi"}}}},
			},
		})
	}))
	defer srv.Close()

	tester := &ProviderTester{Client: srv.Client()}
	result := tester.Test(&ComputeProvider{
		BaseURL:  srv.URL,
		APIKey:   "gem-key",
		Protocol: ProtocolGemini,
		Model:    "gemini-pro",
	})

	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if result.Model != "gemini-pro-001" {
		t.Errorf("expected model gemini-pro-001, got %s", result.Model)
	}
}

func TestProviderTester_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	defer srv.Close()

	tester := &ProviderTester{Client: srv.Client()}
	result := tester.Test(&ComputeProvider{
		BaseURL:  srv.URL,
		APIKey:   "bad-key",
		Protocol: ProtocolOpenAI,
		Model:    "gpt-4",
	})

	if result.Success {
		t.Fatal("expected failure for 401 response")
	}
	if !strings.Contains(result.Error, "HTTP 401") {
		t.Errorf("expected error to contain 'HTTP 401', got: %s", result.Error)
	}
}

func TestProviderTester_ConnectionError(t *testing.T) {
	tester := &ProviderTester{Client: &http.Client{Timeout: 1 * time.Second}}
	result := tester.Test(&ComputeProvider{
		BaseURL:  "http://127.0.0.1:1", // unlikely to be listening
		APIKey:   "key",
		Protocol: ProtocolOpenAI,
		Model:    "gpt-4",
	})

	if result.Success {
		t.Fatal("expected failure for connection error")
	}
	if result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

func TestProviderTester_UnsupportedProtocol(t *testing.T) {
	tester := NewProviderTester()
	result := tester.Test(&ComputeProvider{
		BaseURL:  "https://example.com",
		APIKey:   "key",
		Protocol: "unknown",
	})

	if result.Success {
		t.Fatal("expected failure for unsupported protocol")
	}
	if !strings.Contains(result.Error, "unsupported protocol") {
		t.Errorf("expected 'unsupported protocol' error, got: %s", result.Error)
	}
}

func TestProviderTester_OpenAI_DefaultModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "gpt-3.5-turbo" {
			t.Errorf("expected default model gpt-3.5-turbo, got %v", body["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"model": "gpt-3.5-turbo"})
	}))
	defer srv.Close()

	tester := &ProviderTester{Client: srv.Client()}
	result := tester.Test(&ComputeProvider{
		BaseURL:  srv.URL,
		APIKey:   "key",
		Protocol: ProtocolOpenAI,
		Model:    "", // empty → should use default
	})

	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
}
