package skillmarket

import (
	"context"
	"testing"
	"time"
)

// ── Task 25.3: 频率限制单元测试 ─────────────────────────────────────────

func setupRateLimiterTest(t *testing.T) (*Store, *RateLimiter) {
	t.Helper()
	store := newTestStore(t)
	tierSvc := NewTierService(store)
	rl := NewRateLimiter(store, tierSvc)
	return store, rl
}

func TestRateLimiter_AllowUnderLimit(t *testing.T) {
	store, rl := setupRateLimiterTest(t)
	ctx := context.Background()

	u := createTestUser(t, store, "dev@test.com", 0)
	// Tier 1 默认: 5/hour, 20/day
	_ = rl.TierSvc().RecalculateTier(ctx, u.ID, 0, 0, 0)

	// 无提交记录，应允许
	if err := rl.CheckRateLimit(ctx, u.Email, u.ID); err != nil {
		t.Errorf("expected allow, got: %v", err)
	}
}

func TestRateLimiter_HourlyExceeded(t *testing.T) {
	store, rl := setupRateLimiterTest(t)
	ctx := context.Background()

	u := createTestUser(t, store, "spammer@test.com", 0)
	_ = rl.TierSvc().RecalculateTier(ctx, u.ID, 0, 0, 0)

	// 创建 5 个 pending 提交（Tier 1 限制 5/hour）
	now := time.Now()
	for i := 0; i < 5; i++ {
		sub := &SkillSubmission{
			ID: generateID(), Email: u.Email, UserID: u.ID,
			Status: "pending", ZipPath: "/tmp/test.zip",
			CreatedAt: now, UpdatedAt: now,
		}
		if err := store.CreateSubmission(ctx, sub); err != nil {
			t.Fatal(err)
		}
	}

	// 应被拒绝
	err := rl.CheckRateLimit(ctx, u.Email, u.ID)
	if err == nil {
		t.Error("expected rate limit error")
	}
}

func TestRateLimiter_FailedNotCounted(t *testing.T) {
	store, rl := setupRateLimiterTest(t)
	ctx := context.Background()

	u := createTestUser(t, store, "dev2@test.com", 0)
	_ = rl.TierSvc().RecalculateTier(ctx, u.ID, 0, 0, 0)

	// 创建 5 个 failed 提交
	now := time.Now()
	for i := 0; i < 5; i++ {
		sub := &SkillSubmission{
			ID: generateID(), Email: u.Email, UserID: u.ID,
			Status: "failed", ZipPath: "/tmp/test.zip",
			CreatedAt: now, UpdatedAt: now,
		}
		_ = store.CreateSubmission(ctx, sub)
	}

	// failed 不计入，应允许
	if err := rl.CheckRateLimit(ctx, u.Email, u.ID); err != nil {
		t.Errorf("failed submissions should not count: %v", err)
	}
}

func TestRateLimiter_HigherTierMoreAllowance(t *testing.T) {
	store, rl := setupRateLimiterTest(t)
	ctx := context.Background()

	u := createTestUser(t, store, "pro@test.com", 0)
	// 升到 Tier 2: 10/hour
	_ = rl.TierSvc().RecalculateTier(ctx, u.ID, 3, 0.5, 50)

	now := time.Now()
	for i := 0; i < 6; i++ {
		sub := &SkillSubmission{
			ID: generateID(), Email: u.Email, UserID: u.ID,
			Status: "pending", ZipPath: "/tmp/test.zip",
			CreatedAt: now, UpdatedAt: now,
		}
		_ = store.CreateSubmission(ctx, sub)
	}

	// Tier 2 允许 10/hour, 6 个应该还行
	if err := rl.CheckRateLimit(ctx, u.Email, u.ID); err != nil {
		t.Errorf("tier 2 should allow 6 submissions: %v", err)
	}
}

func TestRateLimiter_SizeLimit(t *testing.T) {
	store, rl := setupRateLimiterTest(t)
	ctx := context.Background()

	u := createTestUser(t, store, "dev3@test.com", 0)
	_ = rl.TierSvc().RecalculateTier(ctx, u.ID, 0, 0, 0)

	// Tier 1: 10MB limit
	if err := rl.CheckSizeLimit(ctx, u.ID, 5<<20); err != nil {
		t.Errorf("5MB should be under limit: %v", err)
	}
	if err := rl.CheckSizeLimit(ctx, u.ID, 15<<20); err == nil {
		t.Error("15MB should exceed tier 1 limit")
	}
}
