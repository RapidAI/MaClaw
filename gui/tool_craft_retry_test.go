package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// --- Sub-task 5.1 [PBT-exploration] + 5.2 [PBT-fix] ---
//
// Originally an exploration test that would have failed on unfixed code
// (generateScript would return the error immediately without retrying).
// The fix (Task 2) is already applied, so this test PASSES — it confirms
// that code:1234 errors now trigger retry with exponential backoff.
//
// **Validates: Requirements 2.1**

// TestGenerateScript_Code1234_RetriesOnTransientError verifies that
// generateScript retries when the LLM API returns a 智谱 code:1234
// transient "网络错误" error. The mock server always returns this error,
// so all retries are exhausted and the returned error should be the
// human-readable exhaustion message (not the raw JSON).
//
// Expected duration: ~14 seconds (2s + 4s + 8s retry backoff sleeps).
func TestGenerateScript_Code1234_RetriesOnTransientError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow retry test in short mode")
	}

	tests := []struct {
		name     string
		jsonBody string
	}{
		{
			name:     "compact JSON code:1234",
			jsonBody: `{"type":"error","error":{"message":"网络错误，错误id：20250715，请稍后重试","code":"1234"}}`,
		},
		{
			name:     "spaced JSON code: 1234",
			jsonBody: `{"type":"error","error":{"message":"网络错误，错误id：20250716，请稍后重试","code": "1234"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requestCount int32

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&requestCount, 1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(tt.jsonBody))
			}))
			defer srv.Close()

			cfg := corelib.MaclawLLMConfig{
				URL:        srv.URL,
				Key:        "test-key",
				Model:      "test-model",
				TimeoutSec: 10,
			}
			request := craftToolRequest{
				Task:            "test task",
				RuntimeLanguage: "python",
			}
			runtimes := craftRuntimeAvailability{Python: "/usr/bin/python"}
			previous := craftAttemptResult{}
			client := srv.Client()

			start := time.Now()
			_, err := generateScript(cfg, request, runtimes, previous, client)
			elapsed := time.Since(start)

			if err == nil {
				t.Fatal("expected error from generateScript, got nil")
			}

			// Verify retry happened: should have made 4 requests (1 initial + 3 retries)
			count := atomic.LoadInt32(&requestCount)
			if count != 4 {
				t.Errorf("expected 4 requests (1 + 3 retries), got %d", count)
			}

			// Verify elapsed time indicates retries occurred (at least 2+4=6 seconds)
			if elapsed < 6*time.Second {
				t.Errorf("expected at least 6s of backoff sleep, elapsed %v", elapsed)
			}

			// Verify the error message is the human-readable exhaustion message
			errMsg := err.Error()
			if !strings.Contains(errMsg, "code:1234") && !strings.Contains(errMsg, "临时故障") {
				t.Errorf("expected human-readable code:1234 exhaustion message, got: %s", errMsg)
			}

			// Verify the error does NOT contain raw JSON
			if strings.Contains(errMsg, `"type":"error"`) {
				t.Errorf("error should not contain raw JSON, got: %s", errMsg)
			}
		})
	}
}

// --- Sub-task 5.3 [PBT-preservation] ---
//
// **Validates: Requirements 3.1, 3.2**

// TestGenerateScript_NonRetryableErrors_FailImmediately verifies that
// non-retryable errors (HTTP 401, 403, 500, etc.) cause generateScript
// to return immediately without retrying. This is a preservation test
// ensuring the fix for code:1234 doesn't regress non-retryable error handling.
func TestGenerateScript_NonRetryableErrors_FailImmediately(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{
			name:       "HTTP 401 Unauthorized",
			statusCode: http.StatusUnauthorized,
			body:       `{"error":{"message":"Invalid API key","type":"invalid_request_error"}}`,
		},
		{
			name:       "HTTP 403 Forbidden",
			statusCode: http.StatusForbidden,
			body:       `{"error":{"message":"Access denied","type":"permission_error"}}`,
		},
		{
			name:       "HTTP 400 non-code1234",
			statusCode: http.StatusBadRequest,
			body:       `{"error":{"message":"Invalid request format","type":"invalid_request_error"}}`,
		},
		{
			name:       "HTTP 400 code:9999 (not 1234)",
			statusCode: http.StatusBadRequest,
			body:       `{"error":{"message":"Unknown error","code":"9999"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requestCount int32

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&requestCount, 1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			cfg := corelib.MaclawLLMConfig{
				URL:        srv.URL,
				Key:        "test-key",
				Model:      "test-model",
				TimeoutSec: 5,
			}
			request := craftToolRequest{
				Task:            "test task",
				RuntimeLanguage: "python",
			}
			runtimes := craftRuntimeAvailability{Python: "/usr/bin/python"}
			previous := craftAttemptResult{}
			client := srv.Client()

			start := time.Now()
			_, err := generateScript(cfg, request, runtimes, previous, client)
			elapsed := time.Since(start)

			if err == nil {
				t.Fatal("expected error from generateScript, got nil")
			}

			// Should have made exactly 1 request (no retries)
			count := atomic.LoadInt32(&requestCount)
			if count != 1 {
				t.Errorf("expected 1 request (no retries), got %d", count)
			}

			// Should return quickly (well under 2 seconds, the first backoff)
			if elapsed > 2*time.Second {
				t.Errorf("non-retryable error should return immediately, took %v", elapsed)
			}
		})
	}
}

// TestGenerateScript_429_StillRetries verifies that HTTP 429 rate limit
// errors continue to trigger retry with exponential backoff. This is a
// preservation test ensuring the code:1234 fix doesn't break existing
// 429 retry behavior.
//
// **Validates: Requirements 3.1**
//
// Expected duration: ~14 seconds (2s + 4s + 8s retry backoff sleeps).
func TestGenerateScript_429_StillRetries(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow retry test in short mode")
	}

	var requestCount int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"Rate limit exceeded","type":"rate_limit_error"}}`))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{
		URL:        srv.URL,
		Key:        "test-key",
		Model:      "test-model",
		TimeoutSec: 10,
	}
	request := craftToolRequest{
		Task:            "test task",
		RuntimeLanguage: "python",
	}
	runtimes := craftRuntimeAvailability{Python: "/usr/bin/python"}
	previous := craftAttemptResult{}
	client := srv.Client()

	start := time.Now()
	_, err := generateScript(cfg, request, runtimes, previous, client)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from generateScript, got nil")
	}

	// Should have made 4 requests (1 initial + 3 retries)
	count := atomic.LoadInt32(&requestCount)
	if count != 4 {
		t.Errorf("expected 4 requests (1 + 3 retries), got %d", count)
	}

	// Verify elapsed time indicates retries occurred
	if elapsed < 6*time.Second {
		t.Errorf("expected at least 6s of backoff sleep, elapsed %v", elapsed)
	}

	// Verify the error message mentions 429
	errMsg := err.Error()
	if !strings.Contains(errMsg, "429") {
		t.Errorf("expected 429 in exhaustion message, got: %s", errMsg)
	}
}

// TestGenerateScript_SuccessAfterRetry verifies that generateScript
// returns the script content when the LLM succeeds after initial failures.
func TestGenerateScript_SuccessAfterRetry(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow retry test in short mode")
	}

	var requestCount int32
	expectedScript := "print('hello world')"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)
		w.Header().Set("Content-Type", "application/json")

		if count <= 2 {
			// First 2 requests: return code:1234 error
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"type":"error","error":{"message":"网络错误，错误id：20250715，请稍后重试","code":"1234"}}`))
			return
		}

		// Third request: return success
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"content": expectedScript,
						"role":    "assistant",
					},
					"finish_reason": "stop",
				},
			},
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{
		URL:        srv.URL,
		Key:        "test-key",
		Model:      "test-model",
		TimeoutSec: 10,
	}
	request := craftToolRequest{
		Task:            "test task",
		RuntimeLanguage: "python",
	}
	runtimes := craftRuntimeAvailability{Python: "/usr/bin/python"}
	previous := craftAttemptResult{}
	client := srv.Client()

	script, err := generateScript(cfg, request, runtimes, previous, client)
	if err != nil {
		t.Fatalf("expected success after retry, got error: %v", err)
	}

	if script != expectedScript {
		t.Errorf("expected script %q, got %q", expectedScript, script)
	}

	count := atomic.LoadInt32(&requestCount)
	if count != 3 {
		t.Errorf("expected 3 requests (2 failures + 1 success), got %d", count)
	}
}

// TestGenerateScript_Code1234WithoutWangluoCuowu_NoRetry verifies that
// an error containing code:1234 but NOT containing "网络错误" is treated
// as non-retryable. The retry predicate requires BOTH conditions.
func TestGenerateScript_Code1234WithoutWangluoCuowu_NoRetry(t *testing.T) {
	var requestCount int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		// code:1234 but different error message (not "网络错误")
		w.Write([]byte(`{"type":"error","error":{"message":"参数错误，请检查请求","code":"1234"}}`))
	}))
	defer srv.Close()

	cfg := corelib.MaclawLLMConfig{
		URL:        srv.URL,
		Key:        "test-key",
		Model:      "test-model",
		TimeoutSec: 5,
	}
	request := craftToolRequest{
		Task:            "test task",
		RuntimeLanguage: "python",
	}
	runtimes := craftRuntimeAvailability{Python: "/usr/bin/python"}
	previous := craftAttemptResult{}
	client := srv.Client()

	start := time.Now()
	_, err := generateScript(cfg, request, runtimes, previous, client)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Should NOT retry — only 1 request
	count := atomic.LoadInt32(&requestCount)
	if count != 1 {
		t.Errorf("expected 1 request (no retry for code:1234 without 网络错误), got %d", count)
	}

	if elapsed > 2*time.Second {
		t.Errorf("should return immediately, took %v", elapsed)
	}
}

// TestGenerateScript_ErrorPatterns_TableDriven is a table-driven test
// approximating property-based testing by covering multiple representative
// error patterns. It verifies the retry predicate classifies each error
// correctly: retryable (code:1234+网络错误 or 429) vs non-retryable.
//
// **Validates: Requirements 2.1, 3.1, 3.2**
func TestGenerateScript_ErrorPatterns_TableDriven(t *testing.T) {
	tests := []struct {
		name          string
		statusCode    int
		body          string
		shouldRetry   bool
		expectInError string // substring expected in the final error
	}{
		// Retryable: code:1234 + 网络错误 (compact)
		{
			name:          "code:1234 compact + 网络错误",
			statusCode:    400,
			body:          `{"type":"error","error":{"message":"网络错误，错误id：20250715，请稍后重试","code":"1234"}}`,
			shouldRetry:   true,
			expectInError: "code:1234",
		},
		// Retryable: code:1234 + 网络错误 (spaced)
		{
			name:          "code:1234 spaced + 网络错误",
			statusCode:    400,
			body:          `{"error":{"code": "1234", "message": "网络错误"}}`,
			shouldRetry:   true,
			expectInError: "code:1234",
		},
		// Retryable: HTTP 429
		{
			name:          "HTTP 429 rate limit",
			statusCode:    429,
			body:          `{"error":{"message":"Rate limit exceeded"}}`,
			shouldRetry:   true,
			expectInError: "429",
		},
		// Non-retryable: HTTP 401
		{
			name:        "HTTP 401 auth error",
			statusCode:  401,
			body:        `{"error":{"message":"Invalid API key"}}`,
			shouldRetry: false,
		},
		// Non-retryable: HTTP 403
		{
			name:        "HTTP 403 forbidden",
			statusCode:  403,
			body:        `{"error":{"message":"Access denied"}}`,
			shouldRetry: false,
		},
		// Non-retryable: code:1234 without 网络错误
		{
			name:        "code:1234 without 网络错误",
			statusCode:  400,
			body:        `{"error":{"message":"参数错误","code":"1234"}}`,
			shouldRetry: false,
		},
		// Non-retryable: different code with 网络错误
		{
			name:        "code:5678 with 网络错误",
			statusCode:  400,
			body:        `{"error":{"message":"网络错误","code":"5678"}}`,
			shouldRetry: false,
		},
		// Non-retryable: generic 400
		{
			name:        "generic HTTP 400",
			statusCode:  400,
			body:        `{"error":{"message":"Bad request"}}`,
			shouldRetry: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.shouldRetry && testing.Short() {
				t.Skip("skipping slow retry test in short mode")
			}

			var requestCount int32

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&requestCount, 1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				fmt.Fprint(w, tt.body)
			}))
			defer srv.Close()

			cfg := corelib.MaclawLLMConfig{
				URL:        srv.URL,
				Key:        "test-key",
				Model:      "test-model",
				TimeoutSec: 10,
			}
			request := craftToolRequest{
				Task:            "test task",
				RuntimeLanguage: "python",
			}
			runtimes := craftRuntimeAvailability{Python: "/usr/bin/python"}
			client := srv.Client()

			_, err := generateScript(cfg, request, runtimes, craftAttemptResult{}, client)
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			count := atomic.LoadInt32(&requestCount)
			if tt.shouldRetry {
				if count != 4 {
					t.Errorf("expected 4 requests (retryable), got %d", count)
				}
				if tt.expectInError != "" && !strings.Contains(err.Error(), tt.expectInError) {
					t.Errorf("expected %q in error, got: %s", tt.expectInError, err.Error())
				}
			} else {
				if count != 1 {
					t.Errorf("expected 1 request (non-retryable), got %d", count)
				}
			}
		})
	}
}
