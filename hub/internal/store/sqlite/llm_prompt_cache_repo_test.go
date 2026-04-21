package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func TestLLMPromptCacheRepositoryRoundTrip(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	expiresAt := now.Add(time.Hour)

	entry := &store.LLMPromptCacheEntry{
		CacheKey:          "cache-1",
		ProviderID:        "openai",
		Model:             "gpt-5",
		Kind:              "metadata",
		InputHash:         "hash-1",
		Payload:           []byte("payload"),
		CachedInputTokens: 42,
		CacheWriteTokens:  7,
		CreatedAt:         now,
		AccessedAt:        now,
		ExpiresAt:         &expiresAt,
	}
	if err := st.LLMPromptCache.Put(ctx, entry); err != nil {
		t.Fatalf("put cache entry: %v", err)
	}

	got, err := st.LLMPromptCache.Get(ctx, entry.CacheKey)
	if err != nil {
		t.Fatalf("get cache entry: %v", err)
	}
	if got == nil || got.ProviderID != entry.ProviderID || got.HitCount != 1 {
		t.Fatalf("unexpected cache entry: %#v", got)
	}

	stats, err := st.LLMPromptCache.Stats(ctx, now)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Entries != 1 || stats.TotalBytes != int64(len(entry.Payload)) || stats.TotalHits != 1 {
		t.Fatalf("unexpected stats: %#v", stats)
	}

	if err := st.LLMPromptCache.Delete(ctx, entry.CacheKey); err != nil {
		t.Fatalf("delete cache entry: %v", err)
	}
	got, err = st.LLMPromptCache.Get(ctx, entry.CacheKey)
	if err != nil {
		t.Fatalf("get deleted cache entry: %v", err)
	}
	if got != nil {
		t.Fatalf("expected deleted cache entry to be nil, got %#v", got)
	}
}

func TestLLMPromptCacheRepositoryDeleteExpiredAndTrim(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	expiredAt := now.Add(-time.Minute)
	futureAt := now.Add(time.Hour)

	entries := []*store.LLMPromptCacheEntry{
		{CacheKey: "expired", ProviderID: "p", Model: "m", Kind: "metadata", InputHash: "h1", Payload: []byte("1234"), CreatedAt: now.Add(-2 * time.Hour), AccessedAt: now.Add(-2 * time.Hour), ExpiresAt: &expiredAt},
		{CacheKey: "old", ProviderID: "p", Model: "m", Kind: "metadata", InputHash: "h2", Payload: []byte("123456"), CreatedAt: now.Add(-time.Hour), AccessedAt: now.Add(-time.Hour), ExpiresAt: &futureAt},
		{CacheKey: "new", ProviderID: "p", Model: "m", Kind: "metadata", InputHash: "h3", Payload: []byte("12345678"), CreatedAt: now, AccessedAt: now, ExpiresAt: &futureAt},
	}
	for _, entry := range entries {
		if err := st.LLMPromptCache.Put(ctx, entry); err != nil {
			t.Fatalf("put %s: %v", entry.CacheKey, err)
		}
	}

	deleted, err := st.LLMPromptCache.DeleteExpired(ctx, now)
	if err != nil {
		t.Fatalf("delete expired: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted expired = %d, want 1", deleted)
	}

	trimmed, err := st.LLMPromptCache.TrimToBytes(ctx, 8)
	if err != nil {
		t.Fatalf("trim to bytes: %v", err)
	}
	if trimmed != 1 {
		t.Fatalf("trimmed = %d, want 1", trimmed)
	}

	stats, err := st.LLMPromptCache.Stats(ctx, now)
	if err != nil {
		t.Fatalf("stats after trim: %v", err)
	}
	if stats.Entries != 1 || stats.TotalBytes != 8 {
		t.Fatalf("unexpected stats after trim: %#v", stats)
	}
}
