package enterpriseknowledge

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestClientCacheLeaseReuse(t *testing.T) {
	dir := t.TempDir()
	c0, err := OpenMetaOnly(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := SeedLibraryForTest(c0, "lib1", "One", "active", true); err != nil {
		t.Fatal(err)
	}
	c0.Close()

	cache := NewClientCache(8, time.Minute)
	t.Cleanup(cache.Clear)

	l1, err := cache.LeaseMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	l2, err := cache.LeaseMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if l1.Client != l2.Client {
		t.Fatal("expected same pooled client")
	}
	if cache.Len() != 1 {
		t.Fatalf("len=%d", cache.Len())
	}
	l1.Release()
	l2.Release()
	if cache.Len() != 1 {
		t.Fatalf("entry should remain until idle TTL, len=%d", cache.Len())
	}

	abs, _ := filepath.Abs(dir)
	l3, err := cache.LeaseMeta(abs)
	if err != nil {
		t.Fatal(err)
	}
	if cache.Len() != 1 {
		t.Fatalf("abs path should hit same key, len=%d", cache.Len())
	}
	l3.Release()
}

func TestSearchActiveFromDataDirUsesCache(t *testing.T) {
	DefaultCache.Clear()
	dir := t.TempDir()
	// Clear cache before TempDir cleanup so Windows can delete open SQLite files.
	t.Cleanup(func() { DefaultCache.Clear() })

	c, err := OpenMetaOnly(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := SeedLibraryForTest(c, "lib1", "One", "active", true); err != nil {
		t.Fatal(err)
	}
	c.Close()

	hits, err := SearchActiveFromDataDir(context.Background(), dir, "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	if hits == nil {
		t.Fatal("hits should be non-nil empty slice")
	}
	if DefaultCache.Len() != 1 {
		t.Fatalf("expected cached client after search, len=%d", DefaultCache.Len())
	}
	// Drop cache before function returns so t.TempDir can remove files on Windows.
	DefaultCache.Clear()
}

func TestClientCacheDoesNotGrowPastMax(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	for _, dir := range []string{dir1, dir2} {
		c0, err := OpenMetaOnly(dir)
		if err != nil {
			t.Fatal(err)
		}
		c0.Close()
	}
	cache := NewClientCache(1, time.Minute)
	t.Cleanup(cache.Clear)

	l1, err := cache.LeaseMeta(dir1)
	if err != nil {
		t.Fatal(err)
	}
	// Hold l1 so dir1 cannot be idle-evicted; second dir must not grow the pool past max.
	l2, err := cache.LeaseMeta(dir2)
	if err != nil {
		t.Fatal(err)
	}
	if cache.Len() > 1 {
		t.Fatalf("pool grew past max=1: len=%d", cache.Len())
	}
	// Uncached lease: Release closes the client (cache/key empty).
	if l2.cache != nil || l2.key != "" {
		// Only acceptable if somehow pooled after eviction — then len must still be 1.
		if cache.Len() != 1 {
			t.Fatalf("expected uncached second lease when max=1 busy, cache=%v key=%q len=%d", l2.cache != nil, l2.key, cache.Len())
		}
	}
	l1.Release()
	l2.Release()
	if cache.Len() > 1 {
		t.Fatalf("after release len=%d", cache.Len())
	}
}

func TestClientCacheSoftInvalidate(t *testing.T) {
	dir := t.TempDir()
	c0, err := OpenMetaOnly(dir)
	if err != nil {
		t.Fatal(err)
	}
	c0.Close()

	cache := NewClientCache(8, time.Minute)
	t.Cleanup(cache.Clear)
	l1, err := cache.LeaseMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Soft-invalidate while leased: client must stay usable.
	cache.Invalidate(dir)
	if l1.Client == nil || l1.Client.isClosed() {
		t.Fatal("leased client should not be force-closed by Invalidate")
	}
	if _, err := l1.Client.ListLibraries(); err != nil {
		t.Fatalf("leased client unusable after soft invalidate: %v", err)
	}
	// Entry still present until last Release.
	if cache.Len() != 1 {
		t.Fatalf("len after soft invalidate want 1, got %d", cache.Len())
	}
	l1.Release()
	if cache.Len() != 0 {
		t.Fatalf("entry should drop on last Release after Invalidate, len=%d", cache.Len())
	}
}

func TestClientCacheIdleEvict(t *testing.T) {
	dir := t.TempDir()
	c0, err := OpenMetaOnly(dir)
	if err != nil {
		t.Fatal(err)
	}
	c0.Close()

	cache := NewClientCache(8, 20*time.Millisecond)
	t.Cleanup(cache.Clear)
	l, err := cache.LeaseMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	l.Release()
	time.Sleep(40 * time.Millisecond)
	// Trigger eviction on next lease of another dir.
	dir2 := t.TempDir()
	c1, err := OpenMetaOnly(dir2)
	if err != nil {
		t.Fatal(err)
	}
	c1.Close()
	l2, err := cache.LeaseMeta(dir2)
	if err != nil {
		t.Fatal(err)
	}
	l2.Release()
	// After lease of dir2, idle dir1 should be gone (evict on lease).
	if cache.Len() > 1 {
		// dir1 may still be present if abs keys differ and eviction only runs idle TTL
		// Force by leasing dir2 again after sleep so evictLocked runs.
		time.Sleep(40 * time.Millisecond)
		l3, err := cache.LeaseMeta(dir2)
		if err != nil {
			t.Fatal(err)
		}
		l3.Release()
	}
	if cache.Len() != 1 {
		t.Fatalf("want 1 after idle eviction, got %d", cache.Len())
	}
}
