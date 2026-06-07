package memory

import (
	"strings"
	"testing"
)

// TestHandleTool_RecallDispatch_MutualExclusion verifies that mutually exclusive
// parameter combinations return clear error messages.
func TestHandleTool_RecallDispatch_MutualExclusion(t *testing.T) {
	store := newTestStoreWithEntries(t, 5)

	tests := []struct {
		name     string
		args     map[string]interface{}
		wantErr  string
	}{
		{
			name: "exhaustive+cursor is mutually exclusive",
			args: map[string]interface{}{
				"action": "recall",
				"query":  "test query",
				"mode":   "exhaustive",
				"cursor": "some-cursor-token",
			},
			wantErr: "cannot combine exhaustive mode with cursor pagination",
		},
		{
			name: "session+cursor is mutually exclusive",
			args: map[string]interface{}{
				"action":  "recall",
				"query":   "test query",
				"session": true,
				"cursor":  "some-cursor-token",
			},
			wantErr: "cannot combine session mode with cursor pagination",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HandleTool(store, tt.args, ToolOptions{OwnerID: "user1"})
			if result != tt.wantErr {
				t.Errorf("got %q, want %q", result, tt.wantErr)
			}
		})
	}
}

// TestHandleTool_RecallDispatch_InvalidCursor verifies that an invalid/expired
// cursor returns an appropriate error.
func TestHandleTool_RecallDispatch_InvalidCursor(t *testing.T) {
	store := newTestStoreWithEntries(t, 5)

	result := HandleTool(store, map[string]interface{}{
		"action": "recall",
		"query":  "test",
		"cursor": "totally-invalid-cursor-token",
	}, ToolOptions{OwnerID: "user1"})

	// Should contain an error about cursor being expired or invalid.
	if !strings.Contains(result, "expired") && !strings.Contains(result, "invalid") && !strings.Contains(result, "not found") {
		t.Errorf("expected error about invalid cursor, got: %q", result)
	}
}

// TestHandleTool_RecallDispatch_ExhaustiveMode verifies that mode=exhaustive
// routes to RecallExhaustive and returns appropriate fields.
func TestHandleTool_RecallDispatch_ExhaustiveMode(t *testing.T) {
	store := newTestStoreWithEntries(t, 10)

	result := HandleTool(store, map[string]interface{}{
		"action": "recall",
		"query":  "golang concurrency",
		"mode":   "exhaustive",
	}, ToolOptions{OwnerID: "user1"})

	// Result should either contain "exhaustive mode" marker (when entries match
	// above threshold) or "No relevant memories" (clean empty result).
	// It should NOT contain pagination fields (cursor, has_more, page) or
	// session fields (session_exhausted).
	if strings.Contains(result, "has_more:") || strings.Contains(result, "session_exhausted:") {
		t.Errorf("exhaustive mode should not contain pagination/session fields, got: %q", result)
	}
	// Should not contain error messages.
	if strings.Contains(result, "cannot combine") || strings.Contains(result, "not available") {
		t.Errorf("exhaustive mode routing failed, got: %q", result)
	}
	// Verify it routed correctly: either found results with exhaustive marker or clean "no results".
	validResult := strings.Contains(result, "exhaustive mode") || result == "No relevant memories found."
	if !validResult {
		t.Errorf("unexpected exhaustive result format, got: %q", result)
	}
}

// TestHandleTool_RecallDispatch_ExhaustiveModeWithTruncation verifies that
// exhaustive mode includes truncated/total_matching when caps are hit.
func TestHandleTool_RecallDispatch_ExhaustiveModeWithTruncation(t *testing.T) {
	// This test verifies the response formatting when RecallExhaustive
	// returns a truncated result. We test the formatter directly since
	// generating 100+ entries that exceed threshold is hard in unit tests.
	store := newTestStoreWithEntries(t, 3)
	truncatedResult := &ExhaustiveResult{
		Entries:       []Entry{{Content: "entry1", Category: "user_fact"}, {Content: "entry2", Category: "user_fact"}},
		Truncated:     true,
		TotalMatching: 150,
	}
	formatted := formatExhaustiveResultForTool(store, "test query", truncatedResult, false)
	if !strings.Contains(formatted, "truncated: true") {
		t.Errorf("expected 'truncated: true' in result, got: %q", formatted)
	}
	if !strings.Contains(formatted, "total_matching: 150") {
		t.Errorf("expected 'total_matching: 150' in result, got: %q", formatted)
	}
	if !strings.Contains(formatted, "exhaustive mode") {
		t.Errorf("expected 'exhaustive mode' marker in result, got: %q", formatted)
	}
}

// TestHandleTool_RecallDispatch_ScrollSession verifies that session=true
// routes to ScrollSessionManager and returns session-specific fields.
func TestHandleTool_RecallDispatch_ScrollSession(t *testing.T) {
	store := newTestStoreWithEntries(t, 10)

	result := HandleTool(store, map[string]interface{}{
		"action":  "recall",
		"query":   "golang concurrency",
		"session": true,
	}, ToolOptions{OwnerID: "user1", LoopID: "loop-1"})

	// Should contain session_exhausted field (either true or false).
	if !strings.Contains(result, "session_exhausted") {
		t.Errorf("expected session_exhausted field in result, got: %q", result)
	}
}

// TestHandleTool_RecallDispatch_ScrollSessionExhausted verifies that repeated
// scroll session advances eventually return session_exhausted: true.
func TestHandleTool_RecallDispatch_ScrollSessionExhausted(t *testing.T) {
	store := newTestStoreWithEntries(t, 3)

	opts := ToolOptions{OwnerID: "user1", LoopID: "loop-exhaust"}
	args := map[string]interface{}{
		"action":  "recall",
		"query":   "golang concurrency",
		"session": true,
	}

	// Call multiple times until exhausted.
	var lastResult string
	for i := 0; i < 20; i++ {
		lastResult = HandleTool(store, args, opts)
		if strings.Contains(lastResult, "session_exhausted: true") {
			return // success
		}
	}
	t.Errorf("expected session_exhausted: true after many advances, last result: %q", lastResult)
}

// TestHandleTool_RecallDispatch_NoNewParams_BackwardCompatible verifies that
// without new params, the response does NOT contain new fields.
func TestHandleTool_RecallDispatch_NoNewParams_BackwardCompatible(t *testing.T) {
	store := newTestStoreWithEntries(t, 5)

	result := HandleTool(store, map[string]interface{}{
		"action": "recall",
		"query":  "golang concurrency",
	}, ToolOptions{OwnerID: "user1"})

	// Should NOT contain pagination/exhaustive/session fields.
	forbiddenFields := []string{"cursor:", "has_more:", "page:", "truncated:", "total_matching:", "session_exhausted:"}
	for _, field := range forbiddenFields {
		if strings.Contains(result, field) {
			t.Errorf("backward compatibility violated: response contains %q when no new params given. Result: %q", field, result)
		}
	}
}

// TestHandleTool_RecallDispatch_UnrecognizedParams verifies that unrecognized
// parameters are ignored without error.
func TestHandleTool_RecallDispatch_UnrecognizedParams(t *testing.T) {
	store := newTestStoreWithEntries(t, 5)

	result := HandleTool(store, map[string]interface{}{
		"action":             "recall",
		"query":              "golang concurrency",
		"unknown_param":      "should be ignored",
		"another_weird_thing": 42,
	}, ToolOptions{OwnerID: "user1"})

	// Should NOT be an error — should return recall results or "No relevant memories"
	if strings.Contains(result, "unknown") || strings.Contains(result, "error") || strings.Contains(result, "unsupported") {
		t.Errorf("unrecognized params should be ignored, got: %q", result)
	}
}

// TestHandleTool_RecallDispatch_SessionDefaultLoopID verifies that session=true
// works even without an explicit LoopID (falls back to "default").
func TestHandleTool_RecallDispatch_SessionDefaultLoopID(t *testing.T) {
	store := newTestStoreWithEntries(t, 5)

	// No LoopID set — should use "default" as fallback.
	result := HandleTool(store, map[string]interface{}{
		"action":  "recall",
		"query":   "golang concurrency",
		"session": true,
	}, ToolOptions{OwnerID: "user1"})

	// Should not error — session_exhausted should appear.
	if strings.Contains(result, "not available") {
		t.Errorf("expected session to work with default LoopID, got: %q", result)
	}
}

// newTestStoreWithEntries creates a test store with N entries for dispatch tests.
func newTestStoreWithEntries(t *testing.T, n int) *Store {
	t.Helper()
	dir := t.TempDir()
	store, err := NewStore(dir + "/test_memories.json")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Stop() })

	// Use unique, non-overlapping content to avoid substring dedup.
	topics := []string{
		"golang concurrency patterns and goroutines",
		"python data science with pandas numpy",
		"rust ownership model and borrow checker",
		"javascript async await promises",
		"kubernetes deployment strategies rolling updates",
		"docker container networking bridge mode",
		"postgresql query optimization indexes",
		"redis caching patterns and eviction policies",
		"terraform infrastructure as code modules",
		"react hooks useEffect useState lifecycle",
	}

	for i := 0; i < n; i++ {
		entry := Entry{
			Content:  topics[i%len(topics)],
			Category: "user_fact",
			Tags:     []string{"programming", topics[i%len(topics)][:4]},
			OwnerID:  "user1",
		}
		store.Save(entry)
	}
	return store
}
