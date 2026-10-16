package main

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestWriteRedactedErrorProjectsRuntimeRateLimit(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeRedactedError(recorder, &agentservice.RateLimitError{RetryAfter: 1500 * time.Millisecond}, "")
	if recorder.Code != 429 || recorder.Header().Get("Retry-After") != "2" {
		t.Fatalf("status=%d retry-after=%q body=%s", recorder.Code, recorder.Header().Get("Retry-After"), recorder.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["code"] != "rate_limited" || body["retry_after_seconds"] != float64(2) {
		t.Fatalf("unexpected rate-limit response: %#v", body)
	}
}
