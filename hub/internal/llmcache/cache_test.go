package llmcache

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type mockRepo struct {
	items map[string]*store.LLMPromptCacheEntry
}

func (m *mockRepo) Get(_ context.Context, cacheKey string) (*store.LLMPromptCacheEntry, error) {
	entry := m.items[cacheKey]
	if entry == nil {
		return nil, nil
	}
	copyEntry := *entry
	copyEntry.Payload = append([]byte(nil), entry.Payload...)
	copyEntry.HitCount++
	m.items[cacheKey] = &copyEntry
	return &copyEntry, nil
}

func (m *mockRepo) Put(_ context.Context, entry *store.LLMPromptCacheEntry) error {
	copyEntry := *entry
	copyEntry.Payload = append([]byte(nil), entry.Payload...)
	m.items[entry.CacheKey] = &copyEntry
	return nil
}

func (m *mockRepo) Delete(_ context.Context, cacheKey string) error {
	delete(m.items, cacheKey)
	return nil
}

func (m *mockRepo) Purge(_ context.Context) (int64, error) {
	count := int64(len(m.items))
	m.items = map[string]*store.LLMPromptCacheEntry{}
	return count, nil
}
func (m *mockRepo) DeleteExpired(_ context.Context, now time.Time) (int64, error) {
	deleted := int64(0)
	for key, entry := range m.items {
		if entry.ExpiresAt != nil && !entry.ExpiresAt.After(now) {
			delete(m.items, key)
			deleted++
		}
	}
	return deleted, nil
}

func (m *mockRepo) TrimToBytes(_ context.Context, maxBytes int64) (int64, error) {
	type kv struct {
		key string
		t   time.Time
	}
	keys := make([]kv, 0, len(m.items))
	total := int64(0)
	for key, entry := range m.items {
		total += entry.PayloadBytes
		keys = append(keys, kv{key: key, t: entry.AccessedAt})
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].t.Before(keys[j].t) })
	trimmed := int64(0)
	for _, item := range keys {
		if total <= maxBytes {
			break
		}
		total -= m.items[item.key].PayloadBytes
		delete(m.items, item.key)
		trimmed++
	}
	return trimmed, nil
}

func (m *mockRepo) ListRecent(_ context.Context, limit int) ([]*store.LLMPromptCacheEntry, error) {
	if limit <= 0 {
		limit = 20
	}
	items := make([]*store.LLMPromptCacheEntry, 0, len(m.items))
	for _, entry := range m.items {
		copyEntry := *entry
		copyEntry.Payload = append([]byte(nil), entry.Payload...)
		items = append(items, &copyEntry)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].AccessedAt.Equal(items[j].AccessedAt) {
			if items[i].CreatedAt.Equal(items[j].CreatedAt) {
				return items[i].CacheKey > items[j].CacheKey
			}
			return items[i].CreatedAt.After(items[j].CreatedAt)
		}
		return items[i].AccessedAt.After(items[j].AccessedAt)
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}
func (m *mockRepo) Stats(_ context.Context, now time.Time) (*store.LLMPromptCacheStats, error) {
	stats := &store.LLMPromptCacheStats{}
	for _, entry := range m.items {
		stats.Entries++
		stats.TotalBytes += entry.PayloadBytes
		stats.TotalHits += entry.HitCount
		if entry.ExpiresAt != nil && !entry.ExpiresAt.After(now) {
			stats.ExpiredEntries++
			stats.ExpiredBytes += entry.PayloadBytes
		}
	}
	return stats, nil
}

func TestCacheEvictsMemoryByBytesAndFallsBackToRepo(t *testing.T) {
	repo := &mockRepo{items: map[string]*store.LLMPromptCacheEntry{}}
	cache := New(repo, Config{MemoryMaxEntries: 2, MemoryMaxBytes: 5})
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	for i, payload := range []string{"aaa", "bbb", "ccc"} {
		entry := &store.LLMPromptCacheEntry{
			CacheKey:     fmt.Sprintf("k%d", i+1),
			ProviderID:   "p",
			Model:        "m",
			Kind:         "metadata",
			InputHash:    fmt.Sprintf("h%d", i+1),
			Payload:      []byte(payload),
			PayloadBytes: int64(len(payload)),
			CreatedAt:    now.Add(time.Duration(i) * time.Minute),
			AccessedAt:   now.Add(time.Duration(i) * time.Minute),
		}
		if err := cache.Put(ctx, entry); err != nil {
			t.Fatalf("put entry %d: %v", i, err)
		}
	}

	status, err := cache.Status(ctx, now)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.MemoryEntries != 1 || status.MemoryBytes != 3 {
		t.Fatalf("unexpected memory status: %#v", status)
	}

	got, err := cache.Get(ctx, "k1")
	if err != nil {
		t.Fatalf("get k1: %v", err)
	}
	if got == nil || string(got.Payload) != "aaa" {
		t.Fatalf("unexpected fallback entry: %#v", got)
	}
}

func TestCacheStatusIncludesDiskAndExpiredCounts(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	expired := now.Add(-time.Minute)
	repo := &mockRepo{items: map[string]*store.LLMPromptCacheEntry{
		"alive":   {CacheKey: "alive", PayloadBytes: 10, AccessedAt: now, ExpiresAt: &now},
		"expired": {CacheKey: "expired", PayloadBytes: 20, AccessedAt: now.Add(-time.Hour), ExpiresAt: &expired, HitCount: 3},
	}}
	cache := New(repo, Config{MemoryMaxEntries: 4, MemoryMaxBytes: 64})
	if err := cache.Put(context.Background(), &store.LLMPromptCacheEntry{CacheKey: "mem", Payload: []byte("abc"), PayloadBytes: 3, AccessedAt: now, CreatedAt: now}); err != nil {
		t.Fatalf("put memory entry: %v", err)
	}

	status, err := cache.Status(context.Background(), now)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.DiskEntries != 3 || status.DiskBytes != 33 {
		t.Fatalf("unexpected disk size status: %#v", status)
	}
	if status.DiskExpired != 2 || status.DiskExpiredBytes != 30 {
		t.Fatalf("unexpected expired status: %#v", status)
	}
}

func TestCacheDeleteClearsMemoryAndRepo(t *testing.T) {
	repo := &mockRepo{items: map[string]*store.LLMPromptCacheEntry{}}
	cache := New(repo, Config{MemoryMaxEntries: 4, MemoryMaxBytes: 64})
	ctx := context.Background()
	now := time.Now().UTC()
	for _, key := range []string{"a", "b"} {
		if err := cache.Put(ctx, &store.LLMPromptCacheEntry{CacheKey: key, Payload: []byte("abc"), PayloadBytes: 3, AccessedAt: now, CreatedAt: now}); err != nil {
			t.Fatalf("put %s: %v", key, err)
		}
	}
	for _, key := range []string{"a", "b"} {
		if err := cache.Delete(ctx, key); err != nil {
			t.Fatalf("delete %s: %v", key, err)
		}
	}
	status, err := cache.Status(ctx, now)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.MemoryEntries != 0 || status.MemoryBytes != 0 || status.DiskEntries != 0 || status.DiskBytes != 0 {
		t.Fatalf("unexpected status after delete: %#v", status)
	}
}
