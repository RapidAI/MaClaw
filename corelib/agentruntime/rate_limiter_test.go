package agentruntime

import (
	"testing"
	"time"
)

func TestConsumeTokenBucketRefillsAndDenies(t *testing.T) {
	now := time.Unix(100, 0)
	next, allowed, retry := ConsumeTokenBucket(0, 1, 1, time.Time{}, now)
	if !allowed || next != 0 || retry != 0 {
		t.Fatalf("new bucket next=%v allowed=%v retry=%s", next, allowed, retry)
	}
	next, allowed, retry = ConsumeTokenBucket(next, 1, 1, now, now)
	if allowed || retry <= 0 {
		t.Fatalf("empty bucket allowed=%v retry=%s next=%v", allowed, retry, next)
	}
	next, allowed, retry = ConsumeTokenBucket(next, 1, 1, now, now.Add(time.Second))
	if !allowed || next != 0 || retry != 0 {
		t.Fatalf("refilled bucket next=%v allowed=%v retry=%s", next, allowed, retry)
	}
}

func TestTenantTokenBucketBurstRefillAndRetry(t *testing.T) {
	limiter := NewTenantTokenBucket(2, 2, 4)
	now := time.Unix(100, 0)
	if ok, wait := limiter.Allow("tenant-a", now); !ok || wait != 0 {
		t.Fatalf("first burst admission = %v/%v", ok, wait)
	}
	if ok, wait := limiter.Allow("tenant-a", now); !ok || wait != 0 {
		t.Fatalf("second burst admission = %v/%v", ok, wait)
	}
	ok, wait := limiter.Allow("tenant-a", now)
	if ok || wait <= 0 || wait > 500*time.Millisecond {
		t.Fatalf("expected bounded retry after burst exhaustion, got %v/%v", ok, wait)
	}
	if ok, wait := limiter.Allow("tenant-a", now.Add(500*time.Millisecond)); !ok || wait != 0 {
		t.Fatalf("half-second refill admission = %v/%v", ok, wait)
	}
}

func TestTenantTokenBucketBoundsCardinalityWithLRUEviction(t *testing.T) {
	limiter := NewTenantTokenBucket(1, 1, 1)
	now := time.Unix(200, 0)
	if ok, _ := limiter.Allow("tenant-a", now); !ok {
		t.Fatal("first tenant should be admitted")
	}
	if ok, _ := limiter.Allow("tenant-a", now); ok {
		t.Fatal("tracked tenant should be rate limited after consuming its token")
	}
	if ok, _ := limiter.Allow("tenant-b", now); !ok {
		t.Fatal("new tenant should be admitted after bounded LRU eviction")
	}
	if limiter.EvictedTenants() != 1 {
		t.Fatalf("evicted tenant count = %d, want 1", limiter.EvictedTenants())
	}
}

func TestTenantTokenBucketDisabledAndEmptyTenant(t *testing.T) {
	for _, limiter := range []*TenantTokenBucket{
		NewTenantTokenBucket(0, 1, 1),
		NewTenantTokenBucket(-1, 1, 1),
		NewTenantTokenBucket(1, 0, 1),
	} {
		if ok, wait := limiter.Allow("tenant", time.Unix(1, 0)); !ok || wait != 0 {
			t.Fatalf("disabled limiter rejected request: %v/%v", ok, wait)
		}
	}
	limiter := NewTenantTokenBucket(1, 1, 1)
	if ok, wait := limiter.Allow("", time.Unix(1, 0)); !ok || wait != 0 {
		t.Fatalf("empty tenant should bypass limiter: %v/%v", ok, wait)
	}
}
