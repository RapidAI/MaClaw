package corelib

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLLMPromptResponseCacheStoresMemoryAndDisk(t *testing.T) {
	cfg := DefaultLLMPromptCacheConfig().WithDefaults()
	cfg.TTLSeconds = 60
	cache := NewLLMPromptResponseCache(t.TempDir())

	cache.Set("key", []byte("body"), cfg)
	if got, ok := cache.Get("key", cfg); !ok || string(got) != "body" {
		t.Fatalf("memory get = %q/%v, want body/true", got, ok)
	}

	reloaded := NewLLMPromptResponseCache(cache.dir)
	if got, ok := reloaded.Get("key", cfg); !ok || string(got) != "body" {
		t.Fatalf("disk get = %q/%v, want body/true", got, ok)
	}
}

func TestLLMPromptResponseCacheSingleflightCachesResult(t *testing.T) {
	cfg := DefaultLLMPromptCacheConfig().WithDefaults()
	cfg.TTLSeconds = 60
	cfg.SingleflightWaitTimeoutMS = 1000
	cache := NewLLMPromptResponseCache(t.TempDir())
	var calls atomic.Int32
	start := make(chan struct{})
	var wg sync.WaitGroup
	type result struct {
		body   string
		shared bool
	}
	results := make(chan result, 2)

	fn := func() ([]byte, error) {
		calls.Add(1)
		<-start
		return []byte("shared"), nil
	}
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, shared, err := cache.DoSingleflightWithShared(context.Background(), "key", cfg, fn)
			if err != nil {
				results <- result{body: err.Error()}
				return
			}
			results <- result{body: string(body), shared: shared}
		}()
		if i == 0 {
			deadline := time.Now().Add(time.Second)
			for calls.Load() == 0 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
		}
	}
	close(start)
	wg.Wait()
	close(results)

	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
	var sharedResults int
	for result := range results {
		if result.body != "shared" {
			t.Fatalf("result = %q, want shared", result.body)
		}
		if result.shared {
			sharedResults++
		}
	}
	if sharedResults != 1 {
		t.Fatalf("shared results = %d, want 1", sharedResults)
	}
	if got, ok := cache.Get("key", cfg); !ok || string(got) != "shared" {
		t.Fatalf("cached get = %q/%v, want shared/true", got, ok)
	}
}

func TestLLMPromptResponseCacheSingleflightCanSkipStore(t *testing.T) {
	cfg := DefaultLLMPromptCacheConfig().WithDefaults()
	cfg.TTLSeconds = 60
	cache := NewLLMPromptResponseCache(t.TempDir())

	body, shared, stored, err := cache.DoSingleflightWithSharedStore(context.Background(), "key", cfg, func() ([]byte, bool, error) {
		return []byte("transient"), false, nil
	})
	if err != nil {
		t.Fatalf("DoSingleflightWithSharedStore: %v", err)
	}
	if string(body) != "transient" || shared || stored {
		t.Fatalf("body/shared/stored = %q/%v/%v, want transient/false/false", body, shared, stored)
	}
	if got, ok := cache.Get("key", cfg); ok {
		t.Fatalf("skipped-store get = %q/true, want miss", got)
	}
}

func TestLLMPromptResponseCacheSingleflightEmptyBodyNotStored(t *testing.T) {
	cfg := DefaultLLMPromptCacheConfig().WithDefaults()
	cfg.TTLSeconds = 60
	cache := NewLLMPromptResponseCache(t.TempDir())

	body, shared, stored, err := cache.DoSingleflightWithSharedStore(context.Background(), "key", cfg, func() ([]byte, bool, error) {
		return nil, true, nil
	})
	if err != nil {
		t.Fatalf("DoSingleflightWithSharedStore: %v", err)
	}
	if len(body) != 0 || shared || stored {
		t.Fatalf("body/shared/stored = %q/%v/%v, want empty/false/false", body, shared, stored)
	}
	if got, ok := cache.Get("key", cfg); ok {
		t.Fatalf("empty-body get = %q/true, want miss", got)
	}
}

func TestLLMPromptResponseCacheEscapesUnsafeDiskKeys(t *testing.T) {
	cfg := DefaultLLMPromptCacheConfig().WithDefaults()
	cfg.TTLSeconds = 60
	dir := t.TempDir()
	cache := NewLLMPromptResponseCache(dir)
	key := `..\escape/key`

	cache.Set(key, []byte("body"), cfg)
	if got, ok := cache.Get(key, cfg); !ok || string(got) != "body" {
		t.Fatalf("get = %q/%v, want body/true", got, ok)
	}
	if _, err := os.Stat(filepath.Join(dir, "..", "escape", "key.json")); !os.IsNotExist(err) {
		t.Fatalf("unsafe cache key wrote outside cache dir: %v", err)
	}
}

func TestLLMPromptResponseCacheAppliesDefaultLimits(t *testing.T) {
	cache := NewLLMPromptResponseCache(t.TempDir())
	cache.Set("key", []byte("body"), LLMPromptCacheConfig{})
	if got, ok := cache.Get("key", LLMPromptCacheConfig{}); !ok || string(got) != "body" {
		t.Fatalf("get = %q/%v, want body/true", got, ok)
	}
}

func TestLLMPromptResponseCacheKeepsMemoryEntryLargerThanDiskLimit(t *testing.T) {
	cfg := DefaultLLMPromptCacheConfig().WithDefaults()
	cfg.DiskMaxBytes = 3
	dir := t.TempDir()
	cache := NewLLMPromptResponseCache(dir)

	if stored := cache.Set("key", []byte("body"), cfg); !stored {
		t.Fatal("Set oversized disk entry = false, want memory store")
	}
	if got, ok := cache.Get("key", cfg); !ok || string(got) != "body" {
		t.Fatalf("memory get = %q/%v, want body/true", got, ok)
	}
	if _, err := os.Stat(filepath.Join(dir, "key.json")); !os.IsNotExist(err) {
		t.Fatalf("oversized cache file exists: %v", err)
	}
	if got, ok := NewLLMPromptResponseCache(dir).Get("key", cfg); ok {
		t.Fatalf("reloaded oversized disk get = %q/true, want miss", got)
	}
}

func TestLLMPromptResponseCacheSkipsEntryLargerThanMemoryAndDiskLimit(t *testing.T) {
	cfg := DefaultLLMPromptCacheConfig().WithDefaults()
	cfg.MemoryMaxBytes = 3
	cfg.DiskMaxBytes = 3
	dir := t.TempDir()
	cache := NewLLMPromptResponseCache(dir)

	if stored := cache.Set("key", []byte("body"), cfg); stored {
		t.Fatal("Set oversized memory/disk entry = true, want false")
	}
	if got, ok := cache.Get("key", cfg); ok {
		t.Fatalf("oversized get = %q/true, want miss", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "key.json")); !os.IsNotExist(err) {
		t.Fatalf("oversized cache file exists: %v", err)
	}
}

func TestLLMPromptResponseCacheSetReportsFalseWhenOnlyDiskWriteFails(t *testing.T) {
	cfg := DefaultLLMPromptCacheConfig().WithDefaults()
	cfg.MemoryMaxBytes = 1
	cfg.DiskMaxBytes = 1024
	dirParent := t.TempDir()
	dirPath := filepath.Join(dirParent, "cache-dir-is-file")
	if err := os.WriteFile(dirPath, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write cache dir file: %v", err)
	}
	cache := NewLLMPromptResponseCache(dirPath)

	if stored := cache.Set("key", []byte("body"), cfg); stored {
		t.Fatal("Set = true, want false when memory disabled and disk write fails")
	}
	if got, ok := cache.Get("key", cfg); ok {
		t.Fatalf("get after failed store = %q/true, want miss", got)
	}
}

func TestLLMPromptResponseCacheSetReportsFalseWhenDiskPrunesNewEntry(t *testing.T) {
	cfg := DefaultLLMPromptCacheConfig().WithDefaults()
	cfg.MemoryMaxBytes = 1
	cfg.DiskMaxBytes = 4
	dir := t.TempDir()
	cache := NewLLMPromptResponseCache(dir)

	if stored := cache.Set("key", []byte("body"), cfg); stored {
		t.Fatal("Set = true, want false when disk limit prunes written entry")
	}
	if got, ok := cache.Get("key", cfg); ok {
		t.Fatalf("get after pruned store = %q/true, want miss", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "key.json")); !os.IsNotExist(err) {
		t.Fatalf("pruned cache file exists: %v", err)
	}
}

func TestLLMPromptResponseCacheDeleteRemovesMemoryAndDisk(t *testing.T) {
	dir := t.TempDir()
	cache := NewLLMPromptResponseCache(dir)
	cache.Set("key", []byte("body"), LLMPromptCacheConfig{})
	cache.Delete("key")
	if got, ok := cache.Get("key", LLMPromptCacheConfig{}); ok {
		t.Fatalf("get after delete = %q/true, want miss", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "key.json")); !os.IsNotExist(err) {
		t.Fatalf("cache file still exists after delete: %v", err)
	}
}

func TestWriteLLMPromptResponseCacheFileAtomicOverwritesCleanly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "entry.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatalf("write old: %v", err)
	}
	if err := writeLLMPromptResponseCacheFileAtomic(path, []byte("new")); err != nil {
		t.Fatalf("write atomic: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "new" {
		t.Fatalf("read atomic = %q/%v, want new/nil", got, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".llm_resp_") {
			t.Fatalf("temporary cache file left behind: %s", entry.Name())
		}
	}
}

func TestLLMPromptResponseCachePruneDiskRemovesTempFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".llm_resp_leftover.tmp"), []byte("partial"), 0o600); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	cache := NewLLMPromptResponseCache(dir)
	cache.Set("key", []byte("body"), LLMPromptCacheConfig{})

	if _, err := os.Stat(filepath.Join(dir, ".llm_resp_leftover.tmp")); !os.IsNotExist(err) {
		t.Fatalf("temp file still exists after prune: %v", err)
	}
}

func TestLLMPromptResponseCacheDeletesExpiredDiskEntryOnRead(t *testing.T) {
	dir := t.TempDir()
	key := "expired"
	entry := LLMPromptResponseCacheEntry{Key: key, Body: []byte("body"), ExpiresAt: time.Now().Add(-time.Minute), LastUsed: time.Now().Add(-time.Minute), Size: 4}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal entry: %v", err)
	}
	path := filepath.Join(dir, key+".json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write expired entry: %v", err)
	}

	cache := NewLLMPromptResponseCache(dir)
	if got, ok := cache.Get(key, LLMPromptCacheConfig{}); ok {
		t.Fatalf("expired get = %q/true, want miss", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expired cache file still exists after read: %v", err)
	}
}

func TestLLMPromptResponseCacheTouchesDiskEntryOnRead(t *testing.T) {
	dir := t.TempDir()
	key := "touch"
	if err := writeLLMPromptResponseCacheTestEntry(dir, key, "body", time.Hour); err != nil {
		t.Fatalf("write entry: %v", err)
	}
	path := filepath.Join(dir, key+".json")
	oldTime := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes old: %v", err)
	}

	cache := NewLLMPromptResponseCache(dir)
	if got, ok := cache.Get(key, LLMPromptCacheConfig{}); !ok || string(got) != "body" {
		t.Fatalf("disk get = %q/%v, want body/true", got, ok)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat entry: %v", err)
	}
	if !info.ModTime().After(oldTime) {
		t.Fatalf("modtime = %s, want after %s", info.ModTime(), oldTime)
	}
}

func TestLLMPromptResponseCacheTouchesDiskEntryOnMemoryRead(t *testing.T) {
	dir := t.TempDir()
	key := "memory_touch"
	cache := NewLLMPromptResponseCache(dir)
	cache.Set(key, []byte("body"), LLMPromptCacheConfig{})
	path := filepath.Join(dir, key+".json")
	oldTime := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes old: %v", err)
	}
	cache.mu.Lock()
	cache.entries[key].LastUsed = oldTime
	cache.mu.Unlock()

	if got, ok := cache.Get(key, LLMPromptCacheConfig{}); !ok || string(got) != "body" {
		t.Fatalf("memory get = %q/%v, want body/true", got, ok)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat entry: %v", err)
	}
	if !info.ModTime().After(oldTime) {
		t.Fatalf("modtime = %s, want after %s", info.ModTime(), oldTime)
	}
}

func TestMigrateLLMPromptResponseCacheDirCopiesJsonFilesWithoutOverwrite(t *testing.T) {
	fromDir := t.TempDir()
	toDir := t.TempDir()
	if err := writeLLMPromptResponseCacheTestEntry(fromDir, "llm_resp_a", "old-a", time.Hour); err != nil {
		t.Fatalf("write source a: %v", err)
	}
	if err := writeLLMPromptResponseCacheTestEntry(fromDir, "llm_resp_b", "old-b", time.Hour); err != nil {
		t.Fatalf("write source b: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fromDir, "note.txt"), []byte("skip"), 0o600); err != nil {
		t.Fatalf("write note: %v", err)
	}
	if err := os.WriteFile(filepath.Join(toDir, "llm_resp_b.json"), []byte("new-b"), 0o600); err != nil {
		t.Fatalf("write existing b: %v", err)
	}

	copied, err := MigrateLLMPromptResponseCacheDir(fromDir, toDir)
	if err != nil {
		t.Fatalf("MigrateLLMPromptResponseCacheDir: %v", err)
	}
	if copied != 1 {
		t.Fatalf("copied = %d, want 1", copied)
	}
	if got, ok := NewLLMPromptResponseCache(toDir).Get("llm_resp_a", LLMPromptCacheConfig{}); !ok || string(got) != "old-a" {
		t.Fatalf("dest a = %q/%v, want old-a/true", got, ok)
	}
	if got, err := os.ReadFile(filepath.Join(toDir, "llm_resp_b.json")); err != nil || string(got) != "new-b" {
		t.Fatalf("dest b = %q/%v, want new-b/nil", got, err)
	}
	if _, err := os.Stat(filepath.Join(toDir, "note.txt")); !os.IsNotExist(err) {
		t.Fatalf("non-json file migrated: %v", err)
	}
}

func TestLLMPromptResponseCacheRejectsMismatchedDiskEntry(t *testing.T) {
	dir := t.TempDir()
	if err := writeLLMPromptResponseCacheTestEntry(dir, "other_key", "poison", time.Hour); err != nil {
		t.Fatalf("write mismatched entry: %v", err)
	}
	if err := os.Rename(filepath.Join(dir, "other_key.json"), filepath.Join(dir, "key.json")); err != nil {
		t.Fatalf("rename mismatched entry: %v", err)
	}

	cache := NewLLMPromptResponseCache(dir)
	if got, ok := cache.Get("key", LLMPromptCacheConfig{}); ok {
		t.Fatalf("mismatched disk entry get = %q/true, want miss", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "key.json")); !os.IsNotExist(err) {
		t.Fatalf("mismatched disk entry still exists after miss: %v", err)
	}
}

func TestLLMPromptResponseCacheRejectsMismatchedDiskSize(t *testing.T) {
	dir := t.TempDir()
	key := "key"
	entry := LLMPromptResponseCacheEntry{Key: key, Body: []byte("body"), ExpiresAt: time.Now().Add(time.Hour), LastUsed: time.Now(), Size: 999}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal entry: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, key+".json"), data, 0o600); err != nil {
		t.Fatalf("write entry: %v", err)
	}

	cache := NewLLMPromptResponseCache(dir)
	if got, ok := cache.Get(key, LLMPromptCacheConfig{}); ok {
		t.Fatalf("mismatched size get = %q/true, want miss", got)
	}
	if _, err := os.Stat(filepath.Join(dir, key+".json")); !os.IsNotExist(err) {
		t.Fatalf("mismatched size entry still exists after miss: %v", err)
	}
}

func TestLLMPromptResponseCacheStatusAndMaintain(t *testing.T) {
	cfg := DefaultLLMPromptCacheConfig().WithDefaults()
	cfg.TTLSeconds = 60
	dir := t.TempDir()
	cache := NewLLMPromptResponseCache(dir)
	// Use Get/Set without relying on MaybeMaintain side effects for counters.
	cache.mu.Lock()
	cache.entries["alive"] = &LLMPromptResponseCacheEntry{
		Key: "alive", Body: []byte("ok"), ExpiresAt: time.Now().Add(time.Hour),
		LastUsed: time.Now(), Size: 2,
	}
	cache.mu.Unlock()
	if _, ok := cache.Get("alive", cfg); !ok {
		t.Fatal("expected hit")
	}
	if _, ok := cache.Get("missing", cfg); ok {
		t.Fatal("expected miss")
	}
	st := cache.Status()
	if st.Hits != 1 || st.Misses != 1 || st.MemoryEntries != 1 || st.MemoryBytes != 2 {
		t.Fatalf("status = %+v, want hits=1 misses=1 entries=1 bytes=2", st)
	}

	// Force an expired disk entry and ensure Maintain removes it.
	expired := LLMPromptResponseCacheEntry{
		Key: "expired", Body: []byte("x"), ExpiresAt: time.Now().Add(-time.Hour),
		LastUsed: time.Now().Add(-time.Hour), Size: 1,
	}
	data, err := json.Marshal(expired)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "expired.json"), data, 0o600); err != nil {
		t.Fatalf("write expired: %v", err)
	}
	if deleted := cache.Maintain(cfg); deleted < 1 {
		t.Fatalf("Maintain deleted = %d, want >= 1", deleted)
	}
	if _, err := os.Stat(filepath.Join(dir, "expired.json")); !os.IsNotExist(err) {
		t.Fatalf("expired entry still present: %v", err)
	}
}

func TestLLMPromptResponseCacheMaybeMaintainThrottled(t *testing.T) {
	cfg := DefaultLLMPromptCacheConfig().WithDefaults()
	cache := NewLLMPromptResponseCache(t.TempDir())
	cache.MaybeMaintain(cfg, time.Hour)
	first := atomic.LoadInt64(&cache.lastMaintainUnix)
	if first == 0 {
		t.Fatal("expected lastMaintainUnix set")
	}
	cache.MaybeMaintain(cfg, time.Hour)
	if atomic.LoadInt64(&cache.lastMaintainUnix) != first {
		t.Fatal("MaybeMaintain should throttle within interval")
	}
}

func TestMigrateLLMPromptResponseCacheDirSkipsInvalidAndExpiredEntries(t *testing.T) {
	fromDir := t.TempDir()
	toDir := t.TempDir()
	if err := writeLLMPromptResponseCacheTestEntry(fromDir, "llm_resp_valid", "valid", time.Hour); err != nil {
		t.Fatalf("write valid entry: %v", err)
	}
	if err := writeLLMPromptResponseCacheTestEntry(fromDir, "llm_resp_expired", "expired", -time.Hour); err != nil {
		t.Fatalf("write expired entry: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fromDir, "llm_resp_invalid.json"), []byte("not-json"), 0o600); err != nil {
		t.Fatalf("write invalid entry: %v", err)
	}
	if err := writeLLMPromptResponseCacheTestEntry(fromDir, "llm_resp_mismatch", "mismatch", time.Hour); err != nil {
		t.Fatalf("write mismatch entry: %v", err)
	}
	if err := os.Rename(filepath.Join(fromDir, "llm_resp_mismatch.json"), filepath.Join(fromDir, "llm_resp_wrong.json")); err != nil {
		t.Fatalf("rename mismatch entry: %v", err)
	}

	copied, err := MigrateLLMPromptResponseCacheDir(fromDir, toDir)
	if err != nil {
		t.Fatalf("MigrateLLMPromptResponseCacheDir: %v", err)
	}
	if copied != 1 {
		t.Fatalf("copied = %d, want 1", copied)
	}
	if _, err := os.Stat(filepath.Join(toDir, "llm_resp_valid.json")); err != nil {
		t.Fatalf("valid entry not migrated: %v", err)
	}
	for _, name := range []string{"llm_resp_expired.json", "llm_resp_invalid.json", "llm_resp_wrong.json"} {
		if _, err := os.Stat(filepath.Join(toDir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s migrated, stat err=%v", name, err)
		}
	}
}

func writeLLMPromptResponseCacheTestEntry(dir, key, body string, ttl time.Duration) error {
	entry := LLMPromptResponseCacheEntry{Key: key, Body: []byte(body), ExpiresAt: time.Now().Add(ttl), LastUsed: time.Now(), Size: int64(len(body))}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, key+".json"), data, 0o600)
}
