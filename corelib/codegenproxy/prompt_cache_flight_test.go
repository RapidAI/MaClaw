package codegenproxy

import (
	"bytes"
	"context"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func TestPromptCacheFlightLeaderFinishesSharedPayload(t *testing.T) {
	s := NewServer("127.0.0.1:0")
	flight, leader := s.joinPromptCacheFlight("k1")
	if !leader || flight == nil {
		t.Fatalf("expected leader flight")
	}

	var got atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
			f, isLeader := s.joinPromptCacheFlight("k1")
			if isLeader {
				t.Errorf("waiter should not be leader")
				return
			}
			payload, ok := waitPromptCacheFlight(req, f, time.Second)
			if !ok || string(payload) != `{"ok":true}` {
				t.Errorf("shared payload = %q ok=%v", payload, ok)
				return
			}
			got.Add(1)
		}()
	}

	time.Sleep(20 * time.Millisecond)
	s.finishPromptCacheFlight("k1", flight, []byte(`{"ok":true}`), true)
	wg.Wait()
	if got.Load() != 3 {
		t.Fatalf("shared waiters = %d, want 3", got.Load())
	}
}

func TestPromptCacheFlightFailureUnblocksWaiters(t *testing.T) {
	s := NewServer("127.0.0.1:0")
	flight, leader := s.joinPromptCacheFlight("k2")
	if !leader {
		t.Fatal("expected leader")
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		req := httptest.NewRequest("POST", "/", nil)
		f, isLeader := s.joinPromptCacheFlight("k2")
		if isLeader {
			t.Errorf("waiter became leader")
			return
		}
		payload, ok := waitPromptCacheFlight(req, f, time.Second)
		if ok || payload != nil {
			t.Errorf("want failure unlock, got ok=%v payload=%q", ok, payload)
		}
	}()
	time.Sleep(20 * time.Millisecond)
	s.finishPromptCacheFlight("k2", flight, nil, false)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("waiter did not unlock")
	}
}

func TestWritePromptCacheHitPayloadJSONAndSSE(t *testing.T) {
	body := []byte(`{"id":"c1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`)
	rec := httptest.NewRecorder()
	if !writePromptCacheHitPayload(rec, body, false) {
		t.Fatal("json write failed")
	}
	if rec.Code != 200 || rec.Header().Get("X-Cache") != "HIT" {
		t.Fatalf("json response code/header = %d %q", rec.Code, rec.Header().Get("X-Cache"))
	}
	if string(rec.Body.Bytes()) != string(body) {
		t.Fatalf("json body mismatch")
	}

	rec2 := httptest.NewRecorder()
	if !writePromptCacheHitPayload(rec2, body, true) {
		t.Fatal("sse write failed")
	}
	if rec2.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("sse content-type = %q", rec2.Header().Get("Content-Type"))
	}
	if !bytes.Contains(rec2.Body.Bytes(), []byte("data: [DONE]")) {
		t.Fatalf("sse missing DONE: %s", rec2.Body.String())
	}

	toolBody := []byte(`{"id":"c2","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
	rec3 := httptest.NewRecorder()
	if !writePromptCacheHitPayload(rec3, toolBody, true) {
		t.Fatal("tool_calls sse write failed")
	}
	if !bytes.Contains(rec3.Body.Bytes(), []byte(`"tool_calls"`)) || !bytes.Contains(rec3.Body.Bytes(), []byte("read_file")) {
		t.Fatalf("tool_calls sse missing fields: %s", rec3.Body.String())
	}
	if !bytes.Contains(rec3.Body.Bytes(), []byte(`"finish_reason":"tool_calls"`)) {
		t.Fatalf("tool_calls sse missing finish: %s", rec3.Body.String())
	}
}

func TestWritePromptCacheHitResponsesPayloadJSONAndSSE(t *testing.T) {
	body := []byte(`{"id":"chatcmpl-resp-cache","choices":[{"message":{"role":"assistant","content":"cached responses ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`)
	rec := httptest.NewRecorder()
	if !writePromptCacheHitResponsesPayload(rec, body, "m", "req1", false) {
		t.Fatal("responses json write failed")
	}
	if rec.Code != 200 || rec.Header().Get("X-Cache") != "HIT" {
		t.Fatalf("json code/header = %d %q", rec.Code, rec.Header().Get("X-Cache"))
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"object":"response"`)) {
		t.Fatalf("responses body missing object: %s", rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("cached responses ok")) {
		t.Fatalf("responses body missing text: %s", rec.Body.String())
	}

	rec2 := httptest.NewRecorder()
	if !writePromptCacheHitResponsesPayload(rec2, body, "m", "req2", true) {
		t.Fatal("responses sse write failed")
	}
	if rec2.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("sse content-type = %q", rec2.Header().Get("Content-Type"))
	}
	if rec2.Header().Get("X-Cache") != "HIT" {
		t.Fatalf("sse X-Cache = %q", rec2.Header().Get("X-Cache"))
	}
	if !bytes.Contains(rec2.Body.Bytes(), []byte("response.completed")) {
		t.Fatalf("sse missing response.completed: %s", rec2.Body.String())
	}
	if !bytes.Contains(rec2.Body.Bytes(), []byte("cached responses ok")) {
		t.Fatalf("sse missing text: %s", rec2.Body.String())
	}
}

func TestWritePromptCacheHitResponsesPayloadToolCallsSSE(t *testing.T) {
	body := []byte(`{"id":"chatcmpl-tool","choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"README.md\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`)
	rec := httptest.NewRecorder()
	if !writePromptCacheHitResponsesPayload(rec, body, "m", "req-tool", true) {
		t.Fatal("tool_call responses sse write failed")
	}
	if rec.Header().Get("X-Cache") != "HIT" {
		t.Fatalf("X-Cache = %q, want HIT", rec.Header().Get("X-Cache"))
	}
	out := rec.Body.String()
	if !bytes.Contains([]byte(out), []byte("response.function_call_arguments.done")) {
		t.Fatalf("missing function_call args done: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("read_file")) {
		t.Fatalf("missing tool name: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("response.completed")) {
		t.Fatalf("missing response.completed: %s", out)
	}
}

func TestStorePromptCacheChatPayloadAllowsToolCalls(t *testing.T) {
	s := NewServer("127.0.0.1:0")
	cache := llmpool.NewCache(nil, llmpool.CacheConfig{MemoryMaxEntries: 8, MemoryMaxBytes: 1 << 20})
	body := []byte(`{"id":"chatcmpl-tool","choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
	stored, reason := s.storePromptCacheChatPayload(context.Background(), cache, "llm_resp_test_tool", "m", body, 200)
	if !stored {
		t.Fatalf("store tool_calls = false reason=%q, want true", reason)
	}
	entry, err := cache.Get(context.Background(), "llm_resp_test_tool")
	if err != nil || entry == nil || len(entry.Payload) == 0 {
		t.Fatalf("get after store: err=%v entry=%v", err, entry)
	}
}
