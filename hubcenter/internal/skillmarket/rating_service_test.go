package skillmarket

import (
	"context"
	"testing"
)

// ── Task 14.5: 评分去重一致性 & 平均分计算正确性 ────────────────────────

func TestRatingService_DedupConsistency(t *testing.T) {
	store := newTestStore(t)
	svc := NewRatingService(store)
	ctx := context.Background()

	// 同一 email 多次评分
	emails := []string{"a@test.com", "b@test.com", "c@test.com"}
	for _, email := range emails {
		// 先评 +1
		if _, err := svc.SubmitRating(ctx, "skill-1", email, 1, "uploader@test.com"); err != nil {
			t.Fatal(err)
		}
		// 再评 +2（覆盖）
		if _, err := svc.SubmitRating(ctx, "skill-1", email, 2, "uploader@test.com"); err != nil {
			t.Fatal(err)
		}
	}

	stats, err := svc.GetStats(ctx, "skill-1")
	if err != nil {
		t.Fatal(err)
	}
	if stats.UniqueRaters != 3 {
		t.Errorf("unique_raters: got %d, want 3", stats.UniqueRaters)
	}
	// 全部覆盖为 +2, 平均 = 2.0
	if stats.AverageScore != 2.0 {
		t.Errorf("avg_score: got %f, want 2.0", stats.AverageScore)
	}
}

func TestRatingService_AverageWithZero(t *testing.T) {
	store := newTestStore(t)
	svc := NewRatingService(store)
	ctx := context.Background()

	scores := map[string]int{
		"a@test.com": 2,
		"b@test.com": 0,
		"c@test.com": -2,
		"d@test.com": 0,
	}
	for email, score := range scores {
		if _, err := svc.SubmitRating(ctx, "skill-2", email, score, "up@test.com"); err != nil {
			t.Fatal(err)
		}
	}

	stats, _ := svc.GetStats(ctx, "skill-2")
	if stats.UniqueRaters != 4 {
		t.Errorf("unique_raters: got %d, want 4", stats.UniqueRaters)
	}
	// (2 + 0 + -2 + 0) / 4 = 0
	if stats.AverageScore != 0 {
		t.Errorf("avg_score: got %f, want 0", stats.AverageScore)
	}
}

func TestRatingService_SelfRateRejected(t *testing.T) {
	store := newTestStore(t)
	svc := NewRatingService(store)
	ctx := context.Background()

	_, err := svc.SubmitRating(ctx, "skill-1", "uploader@test.com", 2, "uploader@test.com")
	if err == nil {
		t.Error("expected error for self-rating")
	}
}

func TestRatingService_InvalidScore(t *testing.T) {
	store := newTestStore(t)
	svc := NewRatingService(store)
	ctx := context.Background()

	for _, score := range []int{-3, 3, 10, -10} {
		_, err := svc.SubmitRating(ctx, "skill-1", "user@test.com", score, "up@test.com")
		if err == nil {
			t.Errorf("expected error for score %d", score)
		}
	}
}

func TestRatingService_EmergencyTakedown(t *testing.T) {
	store := newTestStore(t)
	svc := NewRatingService(store)
	ctx := context.Background()

	// -2 分应触发紧急下架
	takedown, err := svc.SubmitRating(ctx, "skill-1", "user@test.com", -2, "up@test.com")
	if err != nil {
		t.Fatal(err)
	}
	if !takedown {
		t.Error("expected emergency takedown for -2 score")
	}

	// 其他分数不触发
	takedown2, _ := svc.SubmitRating(ctx, "skill-1", "user2@test.com", -1, "up@test.com")
	if takedown2 {
		t.Error("unexpected takedown for -1 score")
	}
}

// ── Task 14.6: Trial Manager 单元测试 ───────────────────────────────────

func TestTrialManager_CalcInterval(t *testing.T) {
	// CalcInterval(n) = 2^(n-2) hours for n >= 2
	tests := []struct {
		n    int
		want int // hours
	}{
		{1, 0},
		{2, 1},
		{3, 2},
		{4, 4},
		{5, 8},
		{6, 16},
	}
	for _, tt := range tests {
		got := CalcInterval(tt.n)
		wantDur := int(got.Hours())
		if wantDur != tt.want {
			t.Errorf("CalcInterval(%d) = %v hours, want %d", tt.n, wantDur, tt.want)
		}
	}
}
