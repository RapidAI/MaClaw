package context

import (
	"errors"
	"testing"
)

func TestNewCompressor(t *testing.T) {
	config := CompressConfig{
		ThresholdRatio:   0.80,
		ProtectedTurns:   5,
		MaxContextTokens: 10000,
	}
	summarize := func(text string) (string, error) {
		return "summary", nil
	}
	c := NewCompressor(config, summarize)
	if c == nil {
		t.Fatal("NewCompressor returned nil")
	}
	if c.config.ThresholdRatio != 0.80 {
		t.Errorf("expected ThresholdRatio 0.80, got %f", c.config.ThresholdRatio)
	}
	if c.config.ProtectedTurns != 5 {
		t.Errorf("expected ProtectedTurns 5, got %d", c.config.ProtectedTurns)
	}
}

func TestShouldCompress_BelowThreshold(t *testing.T) {
	config := CompressConfig{
		ThresholdRatio:   0.80,
		ProtectedTurns:   5,
		MaxContextTokens: 10000,
	}
	c := NewCompressor(config, nil)

	// Small messages well below threshold (80% of 10000 = 8000 tokens)
	messages := []Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there"},
	}
	if c.ShouldCompress(messages) {
		t.Error("ShouldCompress should return false for small messages")
	}
}

func TestShouldCompress_AboveThreshold(t *testing.T) {
	config := CompressConfig{
		ThresholdRatio:   0.80,
		ProtectedTurns:   5,
		MaxContextTokens: 100, // 80% = 80 tokens threshold
	}
	c := NewCompressor(config, nil)

	// Create messages that exceed 80 tokens (each char ~0.25 tokens for ASCII)
	// 400 chars = ~100 tokens > 80 threshold
	longContent := make([]byte, 400)
	for i := range longContent {
		longContent[i] = 'a'
	}
	messages := []Message{
		{Role: "user", Content: string(longContent)},
	}
	if !c.ShouldCompress(messages) {
		t.Error("ShouldCompress should return true when exceeding threshold")
	}
}

func TestCompress_EmptyMessages(t *testing.T) {
	config := CompressConfig{
		ThresholdRatio:   0.80,
		ProtectedTurns:   5,
		MaxContextTokens: 10000,
	}
	c := NewCompressor(config, nil)

	result, err := c.Compress([]Message{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Messages) != 0 {
		t.Errorf("expected 0 messages, got %d", len(result.Messages))
	}
	if result.OriginalTokens != 0 {
		t.Errorf("expected 0 original tokens, got %d", result.OriginalTokens)
	}
}

func TestCompress_AllWithinProtectedWindow(t *testing.T) {
	config := CompressConfig{
		ThresholdRatio:   0.80,
		ProtectedTurns:   5,
		MaxContextTokens: 10000,
	}
	c := NewCompressor(config, nil)

	messages := []Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi"},
		{Role: "user", Content: "How are you?"},
	}
	result, err := c.Compress(messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Messages) != 3 {
		t.Errorf("expected 3 messages unchanged, got %d", len(result.Messages))
	}
	if result.OriginalTokens != result.CompressedTokens {
		t.Errorf("expected tokens unchanged, got original=%d compressed=%d",
			result.OriginalTokens, result.CompressedTokens)
	}
}

func TestCompress_SummarizesOlderMessages(t *testing.T) {
	config := CompressConfig{
		ThresholdRatio:   0.80,
		ProtectedTurns:   2,
		MaxContextTokens: 10000,
	}
	summarizeCalled := false
	summarize := func(text string) (string, error) {
		summarizeCalled = true
		return "Summary of older conversation", nil
	}
	c := NewCompressor(config, summarize)

	messages := []Message{
		{Role: "user", Content: "First message"},
		{Role: "assistant", Content: "First response"},
		{Role: "user", Content: "Second message"},
		{Role: "assistant", Content: "Second response"},
		{Role: "user", Content: "Third message"},
		{Role: "assistant", Content: "Third response"},
	}

	result, err := c.Compress(messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !summarizeCalled {
		t.Error("summarize callback was not called")
	}

	// Result should be: [summary] + [marker] + [last 2 messages]
	if len(result.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result.Messages))
	}

	// First message should be the summary
	if result.Messages[0].Role != "system" {
		t.Errorf("expected summary role 'system', got '%s'", result.Messages[0].Role)
	}
	if result.Messages[0].Content != "Summary of older conversation" {
		t.Errorf("unexpected summary content: %s", result.Messages[0].Content)
	}

	// Second message should be the marker
	if result.Messages[1].Role != "system" {
		t.Errorf("expected marker role 'system', got '%s'", result.Messages[1].Role)
	}
	if result.MarkerText == "" {
		t.Error("MarkerText should not be empty")
	}

	// Last 2 messages should be the protected turns
	if result.Messages[2].Content != "Third message" {
		t.Errorf("expected protected message 'Third message', got '%s'", result.Messages[2].Content)
	}
	if result.Messages[3].Content != "Third response" {
		t.Errorf("expected protected message 'Third response', got '%s'", result.Messages[3].Content)
	}
}

func TestCompress_FallbackOnSummarizationFailure(t *testing.T) {
	config := CompressConfig{
		ThresholdRatio:   0.80,
		ProtectedTurns:   2,
		MaxContextTokens: 100, // Low threshold to trigger truncation
	}
	summarize := func(text string) (string, error) {
		return "", errors.New("LLM unavailable")
	}
	c := NewCompressor(config, summarize)

	messages := []Message{
		{Role: "user", Content: "First message with some content"},
		{Role: "assistant", Content: "First response with some content"},
		{Role: "user", Content: "Second message"},
		{Role: "assistant", Content: "Second response"},
		{Role: "user", Content: "Third message"},
		{Role: "assistant", Content: "Third response"},
	}

	result, err := c.Compress(messages)
	if err != nil {
		t.Fatalf("unexpected error on fallback: %v", err)
	}

	// Should have a marker message
	if result.MarkerText == "" {
		t.Error("MarkerText should not be empty in fallback")
	}

	// Protected messages should be preserved at the end
	lastTwo := result.Messages[len(result.Messages)-2:]
	if lastTwo[0].Content != "Third message" {
		t.Errorf("expected 'Third message', got '%s'", lastTwo[0].Content)
	}
	if lastTwo[1].Content != "Third response" {
		t.Errorf("expected 'Third response', got '%s'", lastTwo[1].Content)
	}

	// First message should be the marker
	if result.Messages[0].Role != "system" {
		t.Errorf("expected first message role 'system' (marker), got '%s'", result.Messages[0].Role)
	}
}

func TestCompress_MarkerContainsTokenRatio(t *testing.T) {
	config := CompressConfig{
		ThresholdRatio:   0.80,
		ProtectedTurns:   1,
		MaxContextTokens: 10000,
	}
	summarize := func(text string) (string, error) {
		return "short", nil
	}
	c := NewCompressor(config, summarize)

	messages := []Message{
		{Role: "user", Content: "A longer message that has some content to compress"},
		{Role: "assistant", Content: "A longer response with details"},
		{Role: "user", Content: "Final question"},
	}

	result, err := c.Compress(messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Marker should contain [compressed] prefix and token counts
	if result.MarkerText == "" {
		t.Fatal("MarkerText should not be empty")
	}
	if !contains(result.MarkerText, "[compressed]") {
		t.Errorf("MarkerText should contain '[compressed]', got: %s", result.MarkerText)
	}
	if !contains(result.MarkerText, "→") {
		t.Errorf("MarkerText should contain '→', got: %s", result.MarkerText)
	}
	if !contains(result.MarkerText, "reduction") {
		t.Errorf("MarkerText should contain 'reduction', got: %s", result.MarkerText)
	}
}

func TestCompress_ChronologicalOrder(t *testing.T) {
	config := CompressConfig{
		ThresholdRatio:   0.80,
		ProtectedTurns:   3,
		MaxContextTokens: 10000,
	}
	summarize := func(text string) (string, error) {
		return "Summary of early conversation", nil
	}
	c := NewCompressor(config, summarize)

	messages := []Message{
		{Role: "user", Content: "msg1"},
		{Role: "assistant", Content: "msg2"},
		{Role: "user", Content: "msg3"},
		{Role: "assistant", Content: "msg4"},
		{Role: "user", Content: "msg5"},
		{Role: "assistant", Content: "msg6"},
		{Role: "user", Content: "msg7"},
		{Role: "assistant", Content: "msg8"},
	}

	result, err := c.Compress(messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Protected messages (last 3) should maintain order
	protected := result.Messages[len(result.Messages)-3:]
	if protected[0].Content != "msg6" {
		t.Errorf("expected 'msg6', got '%s'", protected[0].Content)
	}
	if protected[1].Content != "msg7" {
		t.Errorf("expected 'msg7', got '%s'", protected[1].Content)
	}
	if protected[2].Content != "msg8" {
		t.Errorf("expected 'msg8', got '%s'", protected[2].Content)
	}

	// Summary/marker should come before protected messages
	if result.Messages[0].Role != "system" {
		t.Errorf("first message should be system (summary), got '%s'", result.Messages[0].Role)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
