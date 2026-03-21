package im

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
)

// mockSettingsStore is an in-memory SystemSettingsStore for testing.
type mockSettingsStore struct {
	mu   sync.Mutex
	data map[string]string
}

func newMockSettingsStore() *mockSettingsStore {
	return &mockSettingsStore{data: make(map[string]string)}
}

func (s *mockSettingsStore) Get(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data[key], nil
}

func (s *mockSettingsStore) Set(_ context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value
	return nil
}

func TestDiscussionHistory_SaveAndLoad(t *testing.T) {
	store := newMockSettingsStore()
	ctx := context.Background()

	entry := DiscussionSummaryEntry{
		Topic:     "API design",
		Devices:   []string{"MacBook", "iMac"},
		Consensus: []string{"REST is good"},
		Timestamp: 1700000000,
	}
	SaveDiscussionSummary(ctx, store, "user1", entry)

	entries := LoadRelevantHistory(ctx, store, "user1")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Topic != "API design" {
		t.Fatalf("expected topic 'API design', got %s", entries[0].Topic)
	}
}

func TestDiscussionHistory_MaxEntries(t *testing.T) {
	store := newMockSettingsStore()
	ctx := context.Background()

	// Save 25 entries — should keep only last 20.
	for i := 0; i < 25; i++ {
		SaveDiscussionSummary(ctx, store, "user1", DiscussionSummaryEntry{
			Topic:     "topic",
			Timestamp: int64(1700000000 + i),
		})
	}

	// LoadRelevantHistory returns last 3, but let's verify the store has 20.
	key := "discussion_history_user1"
	raw, _ := store.Get(ctx, key)
	var h DiscussionHistory
	if err := json.Unmarshal([]byte(raw), &h); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(h.Entries) != 20 {
		t.Fatalf("expected 20 entries, got %d", len(h.Entries))
	}
	// First entry should be the 6th one saved (index 5).
	if h.Entries[0].Timestamp != 1700000005 {
		t.Fatalf("expected first entry timestamp 1700000005, got %d", h.Entries[0].Timestamp)
	}
}

func TestDiscussionHistory_LoadRelevantReturnsLast3(t *testing.T) {
	store := newMockSettingsStore()
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		SaveDiscussionSummary(ctx, store, "user1", DiscussionSummaryEntry{
			Topic:     "topic",
			Timestamp: int64(1700000000 + i),
		})
	}

	entries := LoadRelevantHistory(ctx, store, "user1")
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].Timestamp != 1700000002 {
		t.Fatalf("expected first relevant timestamp 1700000002, got %d", entries[0].Timestamp)
	}
}

func TestDiscussionHistory_EmptyStore(t *testing.T) {
	store := newMockSettingsStore()
	entries := LoadRelevantHistory(context.Background(), store, "user1")
	if entries != nil {
		t.Fatalf("expected nil for empty store, got %v", entries)
	}
}

func TestFormatHistorySummary_Empty(t *testing.T) {
	result := FormatHistorySummary(nil)
	if result != "" {
		t.Fatalf("expected empty string, got %q", result)
	}
}

func TestFormatHistorySummary_WithEntries(t *testing.T) {
	entries := []DiscussionSummaryEntry{
		{
			Topic:      "API design",
			Consensus:  []string{"REST is good"},
			Divergence: []string{"GraphQL vs REST"},
			Timestamp:  1700000000,
		},
	}
	result := FormatHistorySummary(entries)
	if result == "" {
		t.Fatal("expected non-empty summary")
	}
}
