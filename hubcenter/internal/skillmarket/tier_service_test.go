package skillmarket

import (
	"context"
	"testing"
)

// ── Task 24.4: TierService 属性测试 ─────────────────────────────────────

func TestTierService_Monotonicity(t *testing.T) {
	store := newTestStore(t)
	svc := NewTierService(store)
	ctx := context.Background()

	// 递增的指标序列，tier 应单调不减
	cases := []struct {
		published int
		avgRating float64
		downloads int
		wantTier  int
	}{
		{0, 0, 0, 1},
		{1, 0.3, 10, 1},
		{3, 0.5, 50, 2},
		{5, 0.8, 100, 2},
		{10, 1.0, 200, 3},
		{15, 1.2, 500, 3},
		{25, 1.5, 1000, 4},
		{50, 2.0, 5000, 4},
	}

	prevTier := 0
	for _, tc := range cases {
		if err := svc.RecalculateTier(ctx, "user-1", tc.published, tc.avgRating, tc.downloads); err != nil {
			t.Fatal(err)
		}
		tier, err := svc.GetTier(ctx, "user-1")
		if err != nil {
			t.Fatal(err)
		}
		if tier.Tier < prevTier {
			t.Errorf("tier decreased: %d -> %d (published=%d, rating=%.1f, downloads=%d)",
				prevTier, tier.Tier, tc.published, tc.avgRating, tc.downloads)
		}
		if tier.Tier != tc.wantTier {
			t.Errorf("published=%d rating=%.1f downloads=%d: got tier %d, want %d",
				tc.published, tc.avgRating, tc.downloads, tier.Tier, tc.wantTier)
		}
		prevTier = tier.Tier
	}
}

func TestTierService_Demotion(t *testing.T) {
	store := newTestStore(t)
	svc := NewTierService(store)
	ctx := context.Background()

	// 先升到 tier 3
	_ = svc.RecalculateTier(ctx, "user-2", 10, 1.0, 200)
	tier, _ := svc.GetTier(ctx, "user-2")
	if tier.Tier != 3 {
		t.Fatalf("expected tier 3, got %d", tier.Tier)
	}

	// 指标下降，应降级
	_ = svc.RecalculateTier(ctx, "user-2", 2, 0.3, 20)
	tier2, _ := svc.GetTier(ctx, "user-2")
	if tier2.Tier != 1 {
		t.Errorf("expected tier 1 after demotion, got %d", tier2.Tier)
	}
}

func TestTierService_GetLimits(t *testing.T) {
	store := newTestStore(t)
	svc := NewTierService(store)

	for tier := 1; tier <= 4; tier++ {
		limits := svc.GetLimits(tier)
		if limits.MaxUploadSizeMB <= 0 {
			t.Errorf("tier %d: MaxUploadSizeMB should be positive", tier)
		}
	}

	// 未知 tier 回退到 tier 1
	limits := svc.GetLimits(99)
	if limits.MaxUploadSizeMB != 10 {
		t.Errorf("unknown tier: got MaxUploadSizeMB=%d, want 10", limits.MaxUploadSizeMB)
	}
}
