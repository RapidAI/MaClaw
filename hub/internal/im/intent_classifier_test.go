package im

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// mockLLMServer creates a test HTTP server that returns the given JSON content
// as an OpenAI-compatible chat completion response.
func mockLLMServer(content string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": content}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

// mockLLMServerSlow creates a test server that delays before responding.
func mockLLMServerSlow(delay time.Duration) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": `{"type":"broadcast","reason":"delayed"}`}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

// mockLLMServerError creates a test server that returns HTTP 500.
func mockLLMServerError() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
}

func testConfig(url string) func() *HubLLMConfig {
	return func() *HubLLMConfig {
		return &HubLLMConfig{
			Enabled:  true,
			APIURL:   url,
			APIKey:   "test-key",
			Model:    "test-model",
			Protocol: "openai",
		}
	}
}

func testProfiles() []DeviceProfile {
	return []DeviceProfile{
		{MachineID: "m1", Name: "MacBook", ProjectPath: "/go/project", Language: "Go", Framework: "gin"},
		{MachineID: "m2", Name: "iMac", ProjectPath: "/py/project", Language: "Python", Framework: "django"},
	}
}

func TestIntentClassifier_RouteSingle(t *testing.T) {
	srv := mockLLMServer(`{"type":"route_single","target_id":"m1","reason":"Go project"}`)
	defer srv.Close()

	cb := DefaultCircuitBreaker()
	ic := NewIntentClassifier(testConfig(srv.URL), cb, NewLLMSemaphore(5))

	result, err := ic.Classify(context.Background(), "user1", "帮我看看Go代码", testProfiles(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Type != IntentRouteSingle {
		t.Fatalf("expected route_single, got %s", result.Type)
	}
	if result.TargetID != "m1" {
		t.Fatalf("expected target m1, got %s", result.TargetID)
	}
}

func TestIntentClassifier_Broadcast(t *testing.T) {
	srv := mockLLMServer(`{"type":"broadcast","reason":"general question"}`)
	defer srv.Close()

	cb := DefaultCircuitBreaker()
	ic := NewIntentClassifier(testConfig(srv.URL), cb, NewLLMSemaphore(5))

	result, err := ic.Classify(context.Background(), "user1", "今天天气怎么样", testProfiles(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Type != IntentBroadcast {
		t.Fatalf("expected broadcast, got %s", result.Type)
	}
}

func TestIntentClassifier_Discuss(t *testing.T) {
	srv := mockLLMServer(`{"type":"discuss","topic":"API design","reason":"collaboration needed"}`)
	defer srv.Close()

	cb := DefaultCircuitBreaker()
	ic := NewIntentClassifier(testConfig(srv.URL), cb, NewLLMSemaphore(5))

	result, err := ic.Classify(context.Background(), "user1", "大家讨论下API设计", testProfiles(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Type != IntentDiscuss {
		t.Fatalf("expected discuss, got %s", result.Type)
	}
	if result.Topic != "API design" {
		t.Fatalf("expected topic 'API design', got %s", result.Topic)
	}
}

func TestIntentClassifier_TimeoutDegradesToBroadcast(t *testing.T) {
	// Server delays 10s, but classify timeout is 5s.
	srv := mockLLMServerSlow(10 * time.Second)
	defer srv.Close()

	cb := DefaultCircuitBreaker()
	ic := NewIntentClassifier(testConfig(srv.URL), cb, NewLLMSemaphore(5))

	start := time.Now()
	result, err := ic.Classify(context.Background(), "user1", "hello", testProfiles(), nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Type != IntentBroadcast {
		t.Fatalf("expected broadcast on timeout, got %s", result.Type)
	}
	// Should complete within ~6s (5s timeout + margin).
	if elapsed > 8*time.Second {
		t.Fatalf("took too long: %v", elapsed)
	}
}

func TestIntentClassifier_LLMErrorDegradesToBroadcast(t *testing.T) {
	srv := mockLLMServerError()
	defer srv.Close()

	cb := DefaultCircuitBreaker()
	ic := NewIntentClassifier(testConfig(srv.URL), cb, NewLLMSemaphore(5))

	result, err := ic.Classify(context.Background(), "user1", "hello", testProfiles(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Type != IntentBroadcast {
		t.Fatalf("expected broadcast on error, got %s", result.Type)
	}
}

func TestIntentClassifier_JSONParseFailureDegrades(t *testing.T) {
	srv := mockLLMServer("this is not json at all")
	defer srv.Close()

	cb := DefaultCircuitBreaker()
	ic := NewIntentClassifier(testConfig(srv.URL), cb, NewLLMSemaphore(5))

	result, err := ic.Classify(context.Background(), "user1", "hello", testProfiles(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Type != IntentBroadcast {
		t.Fatalf("expected broadcast on parse failure, got %s", result.Type)
	}
}

func TestIntentClassifier_CacheHit(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": `{"type":"broadcast","reason":"cached"}`}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	cb := DefaultCircuitBreaker()
	ic := NewIntentClassifier(testConfig(srv.URL), cb, NewLLMSemaphore(5))
	profiles := testProfiles()

	// First call — hits LLM.
	_, _ = ic.Classify(context.Background(), "user1", "same message", profiles, nil)
	if callCount != 1 {
		t.Fatalf("expected 1 LLM call, got %d", callCount)
	}

	// Second call with same params — should hit cache.
	result, _ := ic.Classify(context.Background(), "user1", "same message", profiles, nil)
	if callCount != 1 {
		t.Fatalf("expected cache hit (still 1 LLM call), got %d", callCount)
	}
	if result.Type != IntentBroadcast {
		t.Fatalf("expected broadcast from cache, got %s", result.Type)
	}
}

func TestIntentClassifier_CacheMissOnDifferentText(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": `{"type":"broadcast","reason":"ok"}`}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	cb := DefaultCircuitBreaker()
	ic := NewIntentClassifier(testConfig(srv.URL), cb, NewLLMSemaphore(5))
	profiles := testProfiles()

	_, _ = ic.Classify(context.Background(), "user1", "message A", profiles, nil)
	_, _ = ic.Classify(context.Background(), "user1", "message B", profiles, nil)
	if callCount != 2 {
		t.Fatalf("expected 2 LLM calls for different texts, got %d", callCount)
	}
}

func TestIntentClassifier_NilConfigDegrades(t *testing.T) {
	cb := DefaultCircuitBreaker()
	ic := NewIntentClassifier(func() *HubLLMConfig { return nil }, cb, NewLLMSemaphore(5))

	result, err := ic.Classify(context.Background(), "user1", "hello", testProfiles(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Type != IntentBroadcast {
		t.Fatalf("expected broadcast when config is nil, got %s", result.Type)
	}
}

func TestIntentClassifier_RouteSingleByName(t *testing.T) {
	// LLM returns device name instead of ID — should be resolved.
	srv := mockLLMServer(`{"type":"route_single","target_id":"MacBook","reason":"Go project"}`)
	defer srv.Close()

	cb := DefaultCircuitBreaker()
	ic := NewIntentClassifier(testConfig(srv.URL), cb, NewLLMSemaphore(5))

	result, err := ic.Classify(context.Background(), "user1", "Go代码", testProfiles(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Type != IntentRouteSingle {
		t.Fatalf("expected route_single, got %s", result.Type)
	}
	if result.TargetID != "m1" {
		t.Fatalf("expected resolved target m1, got %s", result.TargetID)
	}
}

func TestIntentClassifier_RouteSingleUnknownTarget(t *testing.T) {
	srv := mockLLMServer(`{"type":"route_single","target_id":"nonexistent","reason":"?"}`)
	defer srv.Close()

	cb := DefaultCircuitBreaker()
	ic := NewIntentClassifier(testConfig(srv.URL), cb, NewLLMSemaphore(5))

	result, err := ic.Classify(context.Background(), "user1", "hello", testProfiles(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Unknown target → degrades to broadcast.
	if result.Type != IntentBroadcast {
		t.Fatalf("expected broadcast for unknown target, got %s", result.Type)
	}
}

func TestIntentClassifier_ErrorTriggersCircuitBreaker(t *testing.T) {
	srv := mockLLMServerError()
	defer srv.Close()

	cb := NewCircuitBreaker(3, time.Minute)
	ic := NewIntentClassifier(testConfig(srv.URL), cb, NewLLMSemaphore(5))

	for i := 0; i < 3; i++ {
		_, _ = ic.Classify(context.Background(), "user1", fmt.Sprintf("msg%d", i), testProfiles(), nil)
	}
	if !cb.IsOpen() {
		t.Fatal("expected circuit breaker to be open after 3 failures")
	}
}

func TestIntentClassifier_MarkdownFencedJSON(t *testing.T) {
	// LLM wraps JSON in markdown code fences.
	content := "```json\n{\"type\":\"broadcast\",\"reason\":\"fenced\"}\n```"
	srv := mockLLMServer(content)
	defer srv.Close()

	cb := DefaultCircuitBreaker()
	ic := NewIntentClassifier(testConfig(srv.URL), cb, NewLLMSemaphore(5))

	result, err := ic.Classify(context.Background(), "user1", "hello", testProfiles(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Type != IntentBroadcast {
		t.Fatalf("expected broadcast, got %s", result.Type)
	}
}
