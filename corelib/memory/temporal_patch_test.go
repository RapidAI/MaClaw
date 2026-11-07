package memory

import (
	"context"
	"testing"
	"time"
)

func TestTemporalPatchDistinguishesPreserveClearAndRevalidate(t *testing.T) {
	now := time.Now()
	expired := now.Add(-time.Hour)
	entry := Entry{InvalidAt: &expired, Stale: true, Status: StatusActive}
	TemporalPatch{}.apply(&entry)
	if entry.InvalidAt == nil || !entry.Stale {
		t.Fatal("unspecified temporal fields should preserve existing state")
	}
	TemporalPatch{SetInvalidAt: true}.apply(&entry)
	if entry.InvalidAt != nil || !entry.Stale {
		t.Fatal("explicit clear should clear InvalidAt without inventing revalidation")
	}
	entry.InvalidAt = &expired
	TemporalPatch{Revalidated: true}.apply(&entry)
	if entry.InvalidAt != nil || entry.Stale || entry.Status != StatusActive {
		t.Fatalf("revalidation did not restore current state: %+v", entry)
	}
}

func TestOnlineUpdateRevalidatesExpiredFact(t *testing.T) {
	store := newTestStore(t)
	defer store.Stop()
	expired := time.Now().Add(-time.Hour)
	existing := Entry{
		ID:         "expired-preference",
		Content:    "User prefers dark mode for all IDEs",
		Category:   CategoryPreference,
		Tags:       []string{"dark_mode"},
		Status:     StatusActive,
		Stale:      true,
		InvalidAt:  &expired,
		SourceType: ManualMemorySourceType,
	}
	if err := store.Save(existing); err != nil {
		t.Fatal(err)
	}
	llm := &mockLLMForExtraction{
		extractResponse:  `[{"content":"User prefers dark mode for all IDEs and editors","category":"preference","revalidated":true}]`,
		classifyResponse: `{"operation":"update","target_id":"expired-preference","merged_text":"User prefers dark mode for all IDEs and editors","reason":"currently confirmed"}`,
	}
	var events []MutationEvent
	unsubscribe := store.SubscribeMutations(func(event MutationEvent) { events = append(events, event) })
	defer unsubscribe()
	extractor := NewOnlineExtractor(store, llm)
	extractor.SetCooldown(0)
	result := extractor.ExtractAndIntegrate(context.Background(), []ConversationMessage{
		{Role: "user", Content: "I still prefer dark mode for all IDEs and editors"},
		{Role: "assistant", Content: "Understood"},
	}, "", time.Now(), "")
	if result.Updated != 1 {
		t.Fatalf("update result = %+v", result)
	}
	updated := store.SearchDirectByID(existing.ID)[0]
	if updated.InvalidAt != nil || updated.Stale || updated.Status != StatusActive {
		t.Fatalf("revalidated online update retained expired state: %+v", updated)
	}
	if updated.SourceType != ManualMemorySourceType {
		t.Fatalf("online update rewrote original source type: %q", updated.SourceType)
	}
	if len(events) != 1 || events[0].Origin != "online_extraction" {
		t.Fatalf("online update event origin = %+v", events)
	}
}
