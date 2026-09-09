package agentservice

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSQLiteDistributedRateLimiterSharesAdmissionAcrossHandles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime_rate_limit.db")
	first, err := NewSQLiteDistributedRateLimiter(path, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	allowed, retry, err := first.Allow(context.Background(), "tenant-a")
	if err != nil || !allowed || retry != 0 {
		t.Fatalf("first allow=%v retry=%s err=%v", allowed, retry, err)
	}
	second, err := NewSQLiteDistributedRateLimiter(path, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	allowed, retry, err = second.Allow(context.Background(), "tenant-a")
	if err != nil || allowed || retry <= 0 {
		t.Fatalf("shared second allow=%v retry=%s err=%v", allowed, retry, err)
	}
	allowed, _, err = second.Allow(context.Background(), "tenant-b")
	if err != nil || !allowed {
		t.Fatalf("other tenant must have its own bucket, allow=%v err=%v", allowed, err)
	}
}

func TestSQLiteDistributedRateLimiterEmptyTenantIsUnlimited(t *testing.T) {
	limiter, err := NewSQLiteDistributedRateLimiter(filepath.Join(t.TempDir(), "runtime_rate_limit.db"), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = limiter.Close() })
	allowed, retry, err := limiter.Allow(context.Background(), "  ")
	if err != nil || !allowed || retry != 0 {
		t.Fatalf("empty tenant allow=%v retry=%s err=%v", allowed, retry, err)
	}
}

func TestSQLiteDistributedRateLimiterClosedRejects(t *testing.T) {
	limiter, err := NewSQLiteDistributedRateLimiter(filepath.Join(t.TempDir(), "runtime_rate_limit.db"), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := limiter.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := limiter.Allow(context.Background(), "tenant"); err == nil {
		t.Fatal("closed limiter must fail")
	}
}

func TestSQLiteDistributedRateLimiterHonorsContextCancel(t *testing.T) {
	limiter, err := NewSQLiteDistributedRateLimiter(filepath.Join(t.TempDir(), "runtime_rate_limit.db"), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = limiter.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := limiter.Allow(ctx, "tenant"); err == nil {
		t.Fatal("canceled context must fail")
	}
}
