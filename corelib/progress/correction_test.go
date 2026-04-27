package progress

import (
	"testing"
	"time"
)

func TestCorrectionStore_StoreAndConsume(t *testing.T) {
	cs := NewCorrectionStore()

	options := []CorrectionOption{
		NewCorrectionOption("改为打断", ActionReplace),
		NewCorrectionOption("改为排队", ActionQueue),
	}

	id := cs.Store("user1", "颜色改红色", ActionMerge, options)
	if id == "" {
		t.Fatal("expected non-empty correction ID")
	}

	// Consume option 0 should succeed.
	userID, msgText, origAction, chosen, ok := cs.Consume(id, 0)
	if !ok {
		t.Fatal("expected Consume to succeed")
	}
	if userID != "user1" || msgText != "颜色改红色" || origAction != ActionMerge {
		t.Errorf("unexpected values: user=%s msg=%s action=%s", userID, msgText, origAction)
	}
	if chosen.Label != "改为打断" || chosen.Action != "replace" {
		t.Errorf("unexpected chosen option: %+v", chosen)
	}

	// Second consume should fail (already consumed).
	_, _, _, _, ok2 := cs.Consume(id, 0)
	if ok2 {
		t.Error("expected second Consume to fail")
	}
}

func TestCorrectionStore_ConsumeOutOfRange(t *testing.T) {
	cs := NewCorrectionStore()

	options := []CorrectionOption{
		NewCorrectionOption("改为打断", ActionReplace),
	}

	id := cs.Store("user1", "test", ActionMerge, options)

	// Index out of range should fail.
	_, _, _, _, ok := cs.Consume(id, 1)
	if ok {
		t.Error("expected Consume with out-of-range index to fail")
	}

	// Negative index should fail.
	_, _, _, _, ok2 := cs.Consume(id, -1)
	if ok2 {
		t.Error("expected Consume with negative index to fail")
	}

	// Valid index should still work (entry not consumed by failed attempts).
	_, _, _, _, ok3 := cs.Consume(id, 0)
	if !ok3 {
		t.Error("expected Consume with valid index to succeed")
	}
}

func TestCorrectionStore_Expiry(t *testing.T) {
	cs := NewCorrectionStore()

	options := []CorrectionOption{
		NewCorrectionOption("改为打断", ActionReplace),
	}

	id := cs.Store("user1", "test", ActionMerge, options)

	// Should be available immediately (TTL is 120s).
	_, _, _, _, ok := cs.Consume(id, 0)
	if !ok {
		t.Fatal("expected Consume to succeed immediately")
	}
}

func TestCorrectionStore_InvalidateUser(t *testing.T) {
	cs := NewCorrectionStore()

	options := []CorrectionOption{
		NewCorrectionOption("改为打断", ActionReplace),
	}

	id1 := cs.Store("user1", "msg1", ActionMerge, options)
	id2 := cs.Store("user1", "msg2", ActionQueue, options)
	id3 := cs.Store("user2", "msg3", ActionMerge, options)

	cs.InvalidateUser("user1")

	// user1's corrections should be gone.
	_, _, _, _, ok1 := cs.Consume(id1, 0)
	_, _, _, _, ok2 := cs.Consume(id2, 0)
	if ok1 || ok2 {
		t.Error("expected user1 corrections to be invalidated")
	}

	// user2's correction should still exist.
	_, _, _, _, ok3 := cs.Consume(id3, 0)
	if !ok3 {
		t.Error("expected user2 correction to still exist")
	}
}

func TestCorrectionStore_Remove(t *testing.T) {
	cs := NewCorrectionStore()

	options := []CorrectionOption{
		NewCorrectionOption("改为打断", ActionReplace),
	}

	id := cs.Store("user1", "test", ActionMerge, options)

	// Remove should succeed.
	if !cs.Remove(id) {
		t.Error("expected Remove to succeed")
	}

	// Second Remove should fail.
	if cs.Remove(id) {
		t.Error("expected second Remove to fail")
	}

	// Consume should also fail.
	_, _, _, _, ok := cs.Consume(id, 0)
	if ok {
		t.Error("expected Consume after Remove to fail")
	}
}

func TestCorrectionStore_RemoveVsConsume_Race(t *testing.T) {
	cs := NewCorrectionStore()

	options := []CorrectionOption{
		NewCorrectionOption("改为打断", ActionReplace),
	}

	id := cs.Store("user1", "test", ActionMerge, options)

	// Simulate: Consume wins the race.
	_, _, _, _, ok := cs.Consume(id, 0)
	if !ok {
		t.Fatal("expected Consume to succeed")
	}

	// Remove should fail (already consumed).
	if cs.Remove(id) {
		t.Error("expected Remove to fail after Consume")
	}
}

func TestFormatCorrectionsText(t *testing.T) {
	corrections := []CorrectionOption{
		NewCorrectionOption("改为打断", ActionReplace),
		NewCorrectionOption("改为排队", ActionQueue),
	}

	result := FormatCorrectionsText("👌 收到，已纳入当前任务。", corrections)
	expected := "👌 收到，已纳入当前任务。\n  回复1: 改为打断 | 回复2: 改为排队"
	if result != expected {
		t.Errorf("expected:\n%s\ngot:\n%s", expected, result)
	}

	// Empty corrections should return original text.
	result2 := FormatCorrectionsText("hello", nil)
	if result2 != "hello" {
		t.Errorf("expected 'hello', got %q", result2)
	}
}

func TestActionFromString(t *testing.T) {
	tests := []struct {
		input    string
		expected ScheduleAction
		ok       bool
	}{
		{"merge", ActionMerge, true},
		{"queue", ActionQueue, true},
		{"replace", ActionReplace, true},
		{"status_query", ActionStatusQuery, true},
		{"unknown", ActionMerge, false},
		{"", ActionMerge, false},
	}
	for _, tt := range tests {
		got, ok := ActionFromString(tt.input)
		if got != tt.expected || ok != tt.ok {
			t.Errorf("ActionFromString(%q) = (%v, %v), want (%v, %v)",
				tt.input, got, ok, tt.expected, tt.ok)
		}
	}
}

func TestCorrectionStore_PurgeOnStore(t *testing.T) {
	cs := NewCorrectionStore()

	options := []CorrectionOption{
		NewCorrectionOption("test", ActionReplace),
	}

	// Store entries for different users to avoid ID collision.
	cs.Store("user1", "msg1", ActionMerge, options)
	cs.Store("user2", "msg2", ActionMerge, options)

	// Entries should exist.
	cs.mu.Lock()
	count := len(cs.entries)
	cs.mu.Unlock()
	if count != 2 {
		t.Errorf("expected 2 entries, got %d", count)
	}

	// Manually expire one entry for testing.
	cs.mu.Lock()
	for _, entry := range cs.entries {
		entry.CreatedAt = time.Now().Add(-time.Duration(DefaultCorrectionTTL+1) * time.Second)
		break // expire only the first one
	}
	cs.mu.Unlock()

	// Store a new entry — should trigger purge of the expired one.
	cs.Store("user3", "msg3", ActionMerge, options)

	cs.mu.Lock()
	count = len(cs.entries)
	cs.mu.Unlock()
	if count != 2 { // 1 non-expired + 1 new
		t.Errorf("expected 2 entries after purge, got %d", count)
	}
}
