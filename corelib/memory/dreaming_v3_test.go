package memory

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRecallDynamic_TemporalDemotion verifies that stale and temporally
// invalidated entries are demoted in recall ranking but not excluded.
func TestRecallDynamic_TemporalDemotion(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	pastInvalid := now.Add(-24 * time.Hour)

	// Entry 1: normal active entry.
	e1 := Entry{
		Content:  "SSH server api.rapidai.tech port 33 user root",
		Category: CategoryProjectKnowledge,
		Tags:     []string{"ssh", "server", "api"},
	}
	if err := store.Save(e1); err != nil {
		t.Fatal(err)
	}

	// Entry 2: stale entry (same topic, marked stale).
	e2 := Entry{
		Content:  "SSH server old.example.com port 22 user admin",
		Category: CategoryProjectKnowledge,
		Tags:     []string{"ssh", "server", "old"},
		Stale:    true,
	}
	if err := store.Save(e2); err != nil {
		t.Fatal(err)
	}

	// Entry 3: temporally invalidated (InvalidAt in the past).
	e3 := Entry{
		Content:   "Plan to visit Singapore next week for conference",
		Category:  CategoryProjectKnowledge,
		Tags:      []string{"singapore", "travel", "plan"},
		InvalidAt: &pastInvalid,
	}
	if err := store.Save(e3); err != nil {
		t.Fatal(err)
	}

	// Recall with query matching all three entries.
	results := store.RecallDynamic("SSH server", "", "", "")

	// The normal entry should rank higher than stale/invalidated ones.
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}

	// Find positions.
	var normalIdx, staleIdx, invalidIdx int
	normalIdx, staleIdx, invalidIdx = -1, -1, -1
	for i, r := range results {
		switch {
		case r.Content == e1.Content:
			normalIdx = i
		case r.Content == e2.Content:
			staleIdx = i
		case r.Content == e3.Content:
			invalidIdx = i
		}
	}

	if normalIdx == -1 {
		t.Fatal("normal entry not found in results")
	}

	// Stale entry should rank below normal (if present).
	if staleIdx != -1 && staleIdx < normalIdx {
		t.Errorf("stale entry (idx=%d) ranked above normal entry (idx=%d)", staleIdx, normalIdx)
	}

	// Invalidated entry should rank below normal (if present).
	if invalidIdx != -1 && invalidIdx < normalIdx {
		t.Errorf("invalidated entry (idx=%d) ranked above normal entry (idx=%d)", invalidIdx, normalIdx)
	}
}

// TestDetectTemporallyExpired_BoundaryUntilExpired verifies that entries with
// an expired Boundary.Until are marked stale.
func TestDetectTemporallyExpired_BoundaryUntilExpired(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}

	past := time.Now().Add(-48 * time.Hour)
	e := Entry{
		Content:  "Sprint 42 deadline next Friday",
		Category: CategoryProjectKnowledge,
		Tags:     []string{"sprint", "deadline"},
		Boundary: &MemoryBoundary{Until: &past},
	}
	if err := store.Save(e); err != nil {
		t.Fatal(err)
	}

	count := store.detectTemporallyExpired()
	if count != 1 {
		t.Fatalf("expected 1 temporal expiry, got %d", count)
	}

	// Verify entry is now stale.
	entries := store.List("", "Sprint 42")
	if len(entries) == 0 {
		t.Fatal("entry not found")
	}
	if !entries[0].Stale {
		t.Error("entry should be marked stale after temporal expiry")
	}
}

// TestDetectTemporallyExpired_ValidAtOldNoActivity verifies that entries with
// old ValidAt and no recent activity are marked stale.
func TestDetectTemporallyExpired_ValidAtOldNoActivity(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}

	oldTime := time.Now().Add(-45 * 24 * time.Hour)   // 45 days ago
	oldUpdate := time.Now().Add(-20 * 24 * time.Hour) // 20 days ago (> 14 day window)
	e := Entry{
		Content:  "Meeting with investors scheduled for May 1st",
		Category: CategoryProjectKnowledge,
		Tags:     []string{"meeting", "investors"},
		ValidAt:  &oldTime,
	}
	if err := store.Save(e); err != nil {
		t.Fatal(err)
	}

	// Directly manipulate UpdatedAt to simulate an old entry.
	// (Save and UpdateEntriesByID both set UpdatedAt=now, so we must go under the lock.)
	store.mu.Lock()
	for i := range store.entries {
		if store.entries[i].Content == e.Content {
			store.entries[i].UpdatedAt = oldUpdate
			store.entries[i].CreatedAt = oldUpdate
			break
		}
	}
	store.mu.Unlock()

	count := store.detectTemporallyExpired()
	if count != 1 {
		t.Fatalf("expected 1 temporal expiry, got %d", count)
	}
}

// TestDetectTemporallyExpired_RecentActivityProtects verifies that entries
// with old ValidAt but recent activity are NOT marked stale.
func TestDetectTemporallyExpired_RecentActivityProtects(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}

	oldTime := time.Now().Add(-45 * 24 * time.Hour) // 45 days ago
	recentUpdate := time.Now().Add(-3 * 24 * time.Hour) // 3 days ago (< 14 day window)
	e := Entry{
		Content:   "Long-running research project on memory systems",
		Category:  CategoryProjectKnowledge,
		Tags:      []string{"research", "memory"},
		ValidAt:   &oldTime,
		UpdatedAt: recentUpdate,
	}
	if err := store.Save(e); err != nil {
		t.Fatal(err)
	}

	count := store.detectTemporallyExpired()
	if count != 0 {
		t.Fatalf("expected 0 temporal expiry (recent activity protects), got %d", count)
	}
}

// TestDetectTemporallyExpired_PinnedSkipped verifies pinned entries are never
// marked stale by temporal detection.
func TestDetectTemporallyExpired_PinnedSkipped(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}

	past := time.Now().Add(-48 * time.Hour)
	e := Entry{
		Content:  "Important pinned deadline that passed",
		Category: CategoryProjectKnowledge,
		Tags:     []string{"deadline"},
		Boundary: &MemoryBoundary{Until: &past},
		Pinned:   true,
	}
	if err := store.Save(e); err != nil {
		t.Fatal(err)
	}

	count := store.detectTemporallyExpired()
	if count != 0 {
		t.Fatalf("expected 0 temporal expiry (pinned protects), got %d", count)
	}
}

// TestFormatMemorySummary_BasicStructure verifies the summary output has
// expected sections and formatting.
func TestFormatMemorySummary_BasicStructure(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}

	// Add entries in different categories.
	entries := []Entry{
		{Content: "User is a product manager at TechCorp", Category: CategoryUserFact, Tags: []string{"role"}, Strength: 5.0},
		{Content: "Project MaClaw desktop AI assistant v2.0", Category: CategoryProjectKnowledge, Tags: []string{"maclaw"}, Strength: 4.0},
		{Content: "Prefer concise answers without filler", Category: "preference", Tags: []string{"style"}, Strength: 3.0},
	}
	for _, e := range entries {
		if err := store.Save(e); err != nil {
			t.Fatal(err)
		}
	}

	summary := store.FormatMemorySummary("")

	// Check basic structure.
	if summary == "" {
		t.Fatal("summary should not be empty")
	}
	if !strings.Contains(summary, "📋 记忆概览") {
		t.Error("summary missing header")
	}
	if !strings.Contains(summary, "🧑") {
		t.Error("summary missing user_info section")
	}
	if !strings.Contains(summary, "💼") {
		t.Error("summary missing projects section")
	}
	if !strings.Contains(summary, "⚙️") {
		t.Error("summary missing preferences section")
	}
	if !strings.Contains(summary, "product manager") {
		t.Error("summary missing user fact content")
	}
}

// TestFormatMemorySummary_OwnerIDFilter verifies multi-tenant isolation.
func TestFormatMemorySummary_OwnerIDFilter(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}

	e1 := Entry{Content: "UserA's secret project", Category: CategoryProjectKnowledge, OwnerID: "user_a", Strength: 5.0}
	e2 := Entry{Content: "UserB's other project", Category: CategoryProjectKnowledge, OwnerID: "user_b", Strength: 5.0}
	for _, e := range []Entry{e1, e2} {
		if err := store.Save(e); err != nil {
			t.Fatal(err)
		}
	}

	summaryA := store.FormatMemorySummary("user_a")
	summaryB := store.FormatMemorySummary("user_b")

	if strings.Contains(summaryA, "UserB") {
		t.Error("user_a summary should not contain user_b's entry")
	}
	if strings.Contains(summaryB, "UserA") {
		t.Error("user_b summary should not contain user_a's entry")
	}
}

// TestDetectTemporallyExpired_PreferenceNotExpired verifies that stable-fact
// categories (user_fact, preference, instruction) are never marked stale by
// time-based expiry, even with old ValidAt and no recent activity.
// These entries represent facts true until contradicted, not time-bound events.
func TestDetectTemporallyExpired_PreferenceNotExpired(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}

	oldTime := time.Now().Add(-90 * 24 * time.Hour) // 90 days ago
	oldUpdate := time.Now().Add(-60 * 24 * time.Hour) // 60 days ago (well beyond activity window)

	// User preferences should never expire based on time alone.
	cases := []struct {
		name     string
		category Category
	}{
		{"user_fact", CategoryUserFact},
		{"preference", CategoryPreference},
		{"instruction", CategoryInstruction},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := Entry{
				Content:  "Stable fact: " + tc.name,
				Category: tc.category,
				Tags:     []string{"test"},
				ValidAt:  &oldTime,
			}
			if err := store.Save(e); err != nil {
				t.Fatal(err)
			}

			// Force old timestamps.
			store.mu.Lock()
			for i := range store.entries {
				if store.entries[i].Content == e.Content {
					store.entries[i].UpdatedAt = oldUpdate
					store.entries[i].CreatedAt = oldUpdate
					break
				}
			}
			store.mu.Unlock()
		})
	}

	count := store.detectTemporallyExpired()
	if count != 0 {
		t.Fatalf("expected 0 temporal expiry (stable-fact categories protected), got %d", count)
	}
}
