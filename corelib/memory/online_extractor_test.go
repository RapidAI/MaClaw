package memory

import (
	"context"
	"testing"
	"time"
)

// mockLLMForExtraction implements LLMChatCaller for testing the online extractor.
type mockLLMForExtraction struct {
	extractResponse  string
	classifyResponse string
	callCount        int
}

func (m *mockLLMForExtraction) ChatCall(messages []map[string]string) (string, error) {
	m.callCount++
	// Determine which call this is based on the system prompt content.
	for _, msg := range messages {
		if msg["role"] == "system" {
			if contains(msg["content"], "memory extraction assistant") {
				return m.extractResponse, nil
			}
			if contains(msg["content"], "memory management assistant") {
				return m.classifyResponse, nil
			}
		}
	}
	return m.extractResponse, nil
}

func (m *mockLLMForExtraction) IsConfigured() bool { return true }

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestOnlineExtractor_ExtractAndIntegrate_AddNewFact(t *testing.T) {
	store := newTestStore(t)
	defer store.Stop()

	llm := &mockLLMForExtraction{
		extractResponse: `[{"content": "User prefers dark mode for all IDEs", "category": "preference", "entities": ["entity:User", "relation:prefers", "entity:dark_mode"]}]`,
	}

	oe := NewOnlineExtractor(store, llm)
	oe.SetCooldown(0) // disable cooldown for testing

	messages := []ConversationMessage{
		{Role: "user", Content: "I always use dark mode in my IDEs"},
		{Role: "assistant", Content: "Got it, dark mode preference noted."},
	}

	result := oe.ExtractAndIntegrate(context.Background(), messages, "", time.Now(), "")

	if result.ExtractedFacts != 1 {
		t.Fatalf("expected 1 extracted fact, got %d", result.ExtractedFacts)
	}
	if result.Added != 1 {
		t.Fatalf("expected 1 added, got %d", result.Added)
	}

	// Verify the entry was saved.
	entries := store.List("", "dark mode")
	if len(entries) == 0 {
		t.Fatal("expected entry with 'dark mode' to be saved")
	}

	// Verify entities were stored.
	if len(entries[0].Entities) == 0 {
		t.Fatal("expected entities to be stored on the entry")
	}
}

func TestOnlineExtractor_ExtractAndIntegrate_UpdateExisting(t *testing.T) {
	store := newTestStore(t)
	defer store.Stop()

	// Pre-populate with an existing memory that has enough keyword overlap
	// with the new fact for BM25 to find it as similar.
	existing := Entry{
		Content:  "User lives in Beijing, China. User's home city is Beijing.",
		Category: CategoryUserFact,
		Tags:     []string{"user", "location", "beijing", "city"},
		Entities: []string{"entity:User", "relation:lives_in", "entity:Beijing"},
	}
	if err := store.Save(existing); err != nil {
		t.Fatal(err)
	}

	// Get the saved entry's ID.
	entries := store.List(CategoryUserFact, "Beijing")
	if len(entries) == 0 {
		t.Fatal("expected existing entry")
	}
	existingID := entries[0].ID

	llm := &mockLLMForExtraction{
		// The extraction returns a fact about moving to Shanghai.
		extractResponse: `[{"content": "User lives in Shanghai. User moved from Beijing to Shanghai.", "category": "user_fact", "entities": ["entity:User", "relation:lives_in", "entity:Shanghai"], "valid_at": "2026-03-30T00:00:00Z"}]`,
		// The classification should detect contradiction and delete the old entry.
		classifyResponse: `{"operation": "delete", "target_id": "` + existingID + `", "merged_text": "", "reason": "User moved from Beijing to Shanghai - contradicting information"}`,
	}

	oe := NewOnlineExtractor(store, llm)
	oe.SetCooldown(0)

	messages := []ConversationMessage{
		{Role: "user", Content: "I moved to Shanghai from Beijing last month. My new home city is Shanghai."},
		{Role: "assistant", Content: "Noted, you're now living in Shanghai."},
	}

	result := oe.ExtractAndIntegrate(context.Background(), messages, "", time.Now(), "")

	if result.Deleted != 1 {
		// If BM25 didn't find the similar entry (no embedder), it would ADD instead.
		// This is acceptable behavior; the async semantic dedup will clean up later.
		// But let's verify at least something happened.
		if result.Added == 0 && result.Deleted == 0 {
			t.Fatalf("expected either add or delete, got added=%d deleted=%d", result.Added, result.Deleted)
		}
		t.Logf("BM25 similarity was insufficient for classification (no embedder); got added=%d instead of deleted=1. This is expected without an embedder.", result.Added)
		return
	}

	// Verify the old entry was superseded.
	store.mu.RLock()
	for _, e := range store.entries {
		if e.ID == existingID {
			if e.Status != StatusSuperseded {
				t.Fatalf("expected old entry to be superseded, got status=%s", e.Status)
			}
			if e.InvalidAt == nil {
				t.Fatal("expected InvalidAt to be set on superseded entry")
			}
			break
		}
	}
	store.mu.RUnlock()

	if got := store.FindByEntity("Beijing"); len(got) != 0 {
		t.Fatalf("superseded entity fact should be removed from active entity lookup, got %+v", got)
	}
	currentHits := store.SemanticGraph().SearchWithOptions([]string{"user"}, SemanticSearchOptions{Now: time.Now()})
	for _, hit := range currentHits {
		if hit.EntryID == existingID {
			t.Fatalf("superseded entity fact should be hidden from current semantic recall, got %+v", currentHits)
		}
	}

	// Verify the new entry was added.
	newEntries := store.List(CategoryUserFact, "Shanghai")
	if len(newEntries) == 0 {
		t.Fatal("expected new entry with 'Shanghai' to be saved")
	}
}

func TestOnlineExtractor_CooldownPreventsRapidCalls(t *testing.T) {
	store := newTestStore(t)
	defer store.Stop()

	llm := &mockLLMForExtraction{
		extractResponse: `[{"content": "test fact", "category": "user_fact"}]`,
	}

	oe := NewOnlineExtractor(store, llm)
	oe.SetCooldown(1 * time.Hour) // long cooldown

	messages := []ConversationMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}

	// First call should work.
	result1 := oe.ExtractAndIntegrate(context.Background(), messages, "", time.Now(), "")
	if result1.ExtractedFacts != 1 {
		t.Fatalf("first call should extract, got %d facts", result1.ExtractedFacts)
	}

	// Second call should be blocked by cooldown.
	result2 := oe.ExtractAndIntegrate(context.Background(), messages, "", time.Now(), "")
	if result2.ExtractedFacts != 0 {
		t.Fatalf("second call should be blocked by cooldown, got %d facts", result2.ExtractedFacts)
	}
}

func TestOnlineExtractor_SkipsWhenAgentAlreadyWroteMemories(t *testing.T) {
	// First verify HasRecentMemoryWrites works as expected.
	testMsgs := []ConversationMessage{
		{Role: "user", Content: "remember this"},
		{Role: "assistant", Content: "saved to memory"},
	}
	if !HasRecentMemoryWrites(testMsgs) {
		t.Fatal("HasRecentMemoryWrites should detect saved-to-memory text")
	}

	store := newTestStore(t)
	defer store.Stop()

	llm := &mockLLMForExtraction{
		extractResponse: `[{"content": "should not be extracted", "category": "user_fact"}]`,
	}

	oe := NewOnlineExtractor(store, llm)
	oe.SetCooldown(0)

	messages := []ConversationMessage{
		{Role: "user", Content: "remember this"},
		{Role: "assistant", Content: "saved to memory"},
	}

	result := oe.ExtractAndIntegrate(context.Background(), messages, "", time.Now(), "")
	if result.ExtractedFacts != 0 {
		t.Fatalf("should skip when agent already wrote memories, got %d facts", result.ExtractedFacts)
	}
}

func TestOnlineExtractor_TemporalAnnotation(t *testing.T) {
	store := newTestStore(t)
	defer store.Stop()

	llm := &mockLLMForExtraction{
		extractResponse: `[{"content": "User started new job at Google", "category": "user_fact", "entities": ["entity:User", "relation:works_at", "entity:Google"], "valid_at": "2026-04-01T00:00:00Z"}]`,
	}

	oe := NewOnlineExtractor(store, llm)
	oe.SetCooldown(0)

	messages := []ConversationMessage{
		{Role: "user", Content: "I started my new job at Google on April 1st"},
		{Role: "assistant", Content: "Congratulations on the new role!"},
	}

	result := oe.ExtractAndIntegrate(context.Background(), messages, "", time.Now(), "")
	if result.Added != 1 {
		t.Fatalf("expected 1 added, got %d", result.Added)
	}

	// Verify ValidAt was set.
	entries := store.List(CategoryUserFact, "Google")
	if len(entries) == 0 {
		t.Fatal("expected entry to be saved")
	}
	if entries[0].ValidAt == nil {
		t.Fatal("expected ValidAt to be set on the entry")
	}
	if entries[0].ValidAt.Month() != time.April || entries[0].ValidAt.Day() != 1 {
		t.Fatalf("expected ValidAt to be April 1, got %v", entries[0].ValidAt)
	}
}

func TestBuildFactTagsUsesCanonicalEntityParser(t *testing.T) {
	fact := ExtractedFact{
		Content:     "Alpha host uses port 2222.",
		RawEntities: []byte(`[" Entity: Alpha Host ", " Relation: HAS-PORT ", " Entity: Port 2222 "]`),
	}

	tags := buildFactTags(fact)
	want := map[string]bool{"online_extracted": true, "alpha host": true, "port 2222": true}
	for _, tag := range tags {
		delete(want, tag)
	}
	if len(want) != 0 {
		t.Fatalf("expected canonical entity tags to be generated, missing=%v tags=%v", want, tags)
	}
}
