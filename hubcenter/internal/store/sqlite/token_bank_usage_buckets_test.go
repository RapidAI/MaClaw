package sqlite

import (
	"context"
	"testing"
	"time"
)

func TestSumUsageBucketsSplitByChinaCivilDay(t *testing.T) {
	repo := newSettleTestRepo(t, "tbk-buckets.db")
	ctx := context.Background()
	now := time.Date(2026, 10, 2, 4, 0, 0, 0, time.UTC) // 12:00 CST on 2 Oct
	dayStart, monthStart := TokenBankCivilBounds(now)
	todayAt := dayStart.Add(time.Minute)
	yesterdayAt := dayStart.Add(-time.Minute)
	prevMonthAt := monthStart.Add(-time.Minute)

	insert := `INSERT INTO token_bank_usage (
		request_id, share_id, owner_user_id, model_name,
		input_tokens, output_tokens, cached_input_tokens, cache_write_tokens,
		gross_micro, fee_micro, net_micro, charged_micro, net_clamped, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	rows := []struct {
		id, share, owner, model string
		net, gross, fee         int64
		clamped                 int
		at                      time.Time
	}{
		{"t1", "share-1", "owner-a", "gpt-4o", 900, 1000, 100, 0, todayAt},
		{"y1", "share-1", "owner-a", "GPT-4o", 400, 500, 100, 1, yesterdayAt},
		{"m1", "share-1", "owner-a", "gpt-old", 50, 80, 30, 0, prevMonthAt},
		{"o1", "share-2", "owner-a", "gpt-4o", 7, 9, 2, 0, todayAt},
		{"x1", "share-1", "owner-b", "gpt-4o", 9999, 9999, 0, 0, todayAt},
	}
	for _, row := range rows {
		if _, err := repo.write.ExecContext(ctx, insert,
			row.id, row.share, row.owner, row.model,
			10, 2, 1, 1, row.gross, row.fee, row.net, row.net, row.clamped,
			row.at.UTC().Format(time.RFC3339)); err != nil {
			t.Fatalf("insert %s: %v", row.id, err)
		}
	}

	byShare, err := repo.SumUsageBucketsByShare(ctx, "owner-a", now)
	if err != nil {
		t.Fatalf("SumUsageBucketsByShare: %v", err)
	}
	share := byShare["share-1"]
	if share.Today.NetMicro != 900 || share.Today.Calls != 1 || share.Today.ClampedCalls != 0 {
		t.Fatalf("today = %+v, want net 900 calls 1", share.Today)
	}
	// 2 Oct is not the 1st, so yesterday is still this month.
	if share.Month.NetMicro != 1300 || share.Month.Calls != 2 || share.Month.ClampedCalls != 1 {
		t.Fatalf("month = %+v, want net 1300 calls 2 clamped 1", share.Month)
	}
	if share.All.NetMicro != 1350 || share.All.Calls != 3 || share.All.GrossMicro != 1580 || share.All.FeeMicro != 230 {
		t.Fatalf("all = %+v, want net 1350 gross 1580 fee 230", share.All)
	}
	if share.All.Tokens() != 3*(10+2+1+1) {
		t.Fatalf("tokens = %d, want %d", share.All.Tokens(), 3*14)
	}
	if byShare["share-2"].Today.NetMicro != 7 {
		t.Fatalf("share-2 today = %+v", byShare["share-2"].Today)
	}
	if _, leaked := byShare["share-1-other"]; leaked {
		t.Fatal("owner-b's row was grouped into owner-a")
	}
	other, err := repo.SumUsageBucketsByShare(ctx, "owner-b", now)
	if err != nil {
		t.Fatalf("owner-b: %v", err)
	}
	if other["share-1"].Today.NetMicro != 9999 || len(other) != 1 {
		t.Fatalf("owner-b buckets = %+v", other)
	}

	byModel, err := repo.SumUsageBucketsByModel(ctx, "owner-a", "share-1", now)
	if err != nil {
		t.Fatalf("SumUsageBucketsByModel: %v", err)
	}
	gpt := byModel["gpt-4o"]
	if gpt.Label == "" || gpt.Today.NetMicro != 900 || gpt.All.NetMicro != 1300 || gpt.All.Calls != 2 {
		t.Fatalf("gpt-4o = %+v, want today 900 and both casings summed to 1300", gpt)
	}
	if byModel["gpt-old"].All.NetMicro != 50 || byModel["gpt-old"].Today.Calls != 0 {
		t.Fatalf("gpt-old = %+v", byModel["gpt-old"])
	}
	if _, ok := byModel["gpt-4o"]; !ok {
		t.Fatal("model key was not folded to lower case")
	}

	if name, ok := CanonicalTokenBankRange(" Month "); !ok || name != TokenBankRangeMonth {
		t.Fatalf("CanonicalTokenBankRange(Month) = %q %v", name, ok)
	}
	if _, ok := CanonicalTokenBankRange("week"); ok {
		t.Fatal("week was accepted as a range")
	}
	if name, ok := CanonicalTokenBankRange(""); !ok || name != TokenBankRangeAll {
		t.Fatalf("empty range = %q %v", name, ok)
	}
}
