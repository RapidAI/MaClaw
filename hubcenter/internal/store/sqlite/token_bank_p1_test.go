package sqlite

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestSettleTokenBankUsageDailyCapCreditsThenReportsHit(t *testing.T) {
	repo := newSettleTestRepo(t, "tbk-cap-daily.db")
	ctx := context.Background()
	share := shareFixture("share-cap", "owner-cap", "fp-cap")
	share.DailyTokenCap = 10
	if _, _, err := repo.CreateShare(ctx, share, []TokenBankShareModel{modelFixture("llama")}, 0, time.Now().UTC()); err != nil {
		t.Fatalf("CreateShare: %v", err)
	}

	under := settleInput("cap-under")
	under.ShareID = share.ID
	under.OwnerID = share.OwnerUserID
	under.ModelName = "llama"
	under.InputTokens = 10
	under.OutputTokens = 0
	under.ChargedMicro = 1_000_000_000
	first, err := repo.SettleTokenBankUsage(ctx, under)
	if err != nil {
		t.Fatalf("under-cap settle: %v", err)
	}
	if !first.Applied || first.CapHit != "" {
		t.Fatalf("under-cap applied/hit = %v/%q, want true and empty", first.Applied, first.CapHit)
	}

	over := under
	over.RequestID = "cap-over"
	over.InputTokens = 1
	second, err := repo.SettleTokenBankUsage(ctx, over)
	if err != nil {
		t.Fatalf("over-cap settle: %v", err)
	}
	if !second.Applied || second.CapHit != "daily" || second.NetMicro <= 0 {
		t.Fatalf("over-cap applied/hit/net = %v/%q/%d, want credited with daily", second.Applied, second.CapHit, second.NetMicro)
	}

	replay, err := repo.SettleTokenBankUsage(ctx, over)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if replay.Applied || replay.CapHit != "" {
		t.Fatalf("replay applied/hit = %v/%q, want false and empty", replay.Applied, replay.CapHit)
	}

	loaded, err := repo.LoadShare(ctx, share.ID, share.OwnerUserID)
	if err != nil {
		t.Fatalf("LoadShare: %v", err)
	}
	if loaded.Status != TokenBankShareStatusActive {
		t.Fatalf("status = %q, want the store to credit without pausing", loaded.Status)
	}
}

func TestSettleTokenBankUsageMonthlyCap(t *testing.T) {
	repo := newSettleTestRepo(t, "tbk-cap-month.db")
	ctx := context.Background()
	share := shareFixture("share-month", "owner-month", "fp-month")
	share.MonthlyTokenCap = 5
	if _, _, err := repo.CreateShare(ctx, share, nil, 0, time.Now().UTC()); err != nil {
		t.Fatalf("CreateShare: %v", err)
	}
	in := settleInput("month-over")
	in.ShareID = share.ID
	in.OwnerID = share.OwnerUserID
	in.ModelName = "llama"
	in.InputTokens = 6
	in.OutputTokens = 0
	in.ChargedMicro = 1_000_000_000
	out, err := repo.SettleTokenBankUsage(ctx, in)
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	if !out.Applied || out.CapHit != "monthly" {
		t.Fatalf("applied/hit = %v/%q, want true/monthly", out.Applied, out.CapHit)
	}
}

func TestSettleTokenBankUsageDailyCapWinsOverAnomaly(t *testing.T) {
	repo := newSettleTestRepo(t, "tbk-cap-daily-wins.db")
	ctx := context.Background()
	share := shareFixture("share-both", "owner-both", "fp-both")
	share.DailyTokenCap = 10
	if _, _, err := repo.CreateShare(ctx, share, nil, 0, time.Now().UTC()); err != nil {
		t.Fatalf("CreateShare: %v", err)
	}
	loc := time.FixedZone("CST", 8*3600)
	dayStart := time.Date(time.Now().In(loc).Year(), time.Now().In(loc).Month(), time.Now().In(loc).Day(), 0, 0, 0, 0, loc)
	prev := settleInput("both-prev")
	prev.ShareID = share.ID
	prev.OwnerID = share.OwnerUserID
	prev.ModelName = "llama"
	prev.InputTokens = 70
	prev.OutputTokens = 0
	prev.ChargedMicro = 1_000_000_000
	prev.CreatedAt = dayStart.Add(-time.Hour).UTC()
	if _, err := repo.SettleTokenBankUsage(ctx, prev); err != nil {
		t.Fatalf("prev: %v", err)
	}
	today := prev
	today.RequestID = "both-today"
	today.InputTokens = 51
	today.CreatedAt = dayStart.Add(time.Hour).UTC()
	out, err := repo.SettleTokenBankUsage(ctx, today)
	if err != nil {
		t.Fatalf("today: %v", err)
	}
	if out.CapHit != "daily" {
		t.Fatalf("CapHit = %q, want daily (the hard cap wins)", out.CapHit)
	}
}

func TestSettleTokenBankUsageAnomaly(t *testing.T) {
	repo := newSettleTestRepo(t, "tbk-anomaly.db")
	ctx := context.Background()
	share := shareFixture("share-anom", "owner-anom", "fp-anom")
	if _, _, err := repo.CreateShare(ctx, share, nil, 0, time.Now().UTC()); err != nil {
		t.Fatalf("CreateShare: %v", err)
	}
	loc := time.FixedZone("CST", 8*3600)
	dayStart := time.Date(time.Now().In(loc).Year(), time.Now().In(loc).Month(), time.Now().In(loc).Day(), 0, 0, 0, 0, loc)

	base := settleInput("anom-prev")
	base.ShareID = share.ID
	base.OwnerID = share.OwnerUserID
	base.ModelName = "llama"
	base.OutputTokens = 0
	base.ChargedMicro = 1_000_000_000
	base.InputTokens = 70
	base.CreatedAt = dayStart.Add(-time.Hour).UTC()
	if _, err := repo.SettleTokenBankUsage(ctx, base); err != nil {
		t.Fatalf("prev: %v", err)
	}

	border := base
	border.RequestID = "anom-border"
	border.InputTokens = 50
	border.CreatedAt = dayStart.Add(time.Hour).UTC()
	borderOut, err := repo.SettleTokenBankUsage(ctx, border)
	if err != nil {
		t.Fatalf("border: %v", err)
	}
	if borderOut.CapHit != "" {
		t.Fatalf("CapHit at the old 5x boundary = %q, want empty", borderOut.CapHit)
	}

	over := border
	over.RequestID = "anom-over"
	over.InputTokens = 1
	overOut, err := repo.SettleTokenBankUsage(ctx, over)
	if err != nil {
		t.Fatalf("over: %v", err)
	}
	if !overOut.Applied || overOut.CapHit != "" || overOut.NetMicro <= 0 {
		t.Fatalf("applied/hit/net = %v/%q/%d, want credited with no pause", overOut.Applied, overOut.CapHit, overOut.NetMicro)
	}
}

func TestTokenBankUsageDailyIsOwnerScoped(t *testing.T) {
	repo := newSettleTestRepo(t, "tbk-usage-daily.db")
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := repo.write.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	insert := `INSERT INTO token_bank_usage (request_id, share_id, owner_user_id, model_name, net_micro, input_tokens, output_tokens, created_at)
		VALUES (?, ?, ?, ?, ?, 1, 1, ?)`
	rows := []struct {
		id, owner, model string
		net              int64
	}{
		{"a1", "owner-a", "alpha", 5},
		{"a2", "owner-a", "alpha", 7},
		{"a3", "owner-a", "beta", 3},
		{"b1", "owner-b", "gamma", 100},
	}
	for i, row := range rows {
		if _, err := tx.ExecContext(ctx, insert, row.id, "share", row.owner, row.model, row.net, now); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}
	for i := 0; i < 11; i++ {
		if _, err := tx.ExecContext(ctx, insert, "top-"+string(rune('a'+i)), "share", "owner-top", "m"+string(rune('a'+i)), int64(i+1), now); err != nil {
			t.Fatalf("top %d: %v", i, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	days, models, err := repo.UsageDaily(ctx, "owner-a", 30)
	if err != nil {
		t.Fatalf("UsageDaily: %v", err)
	}
	if len(days) != 1 || days[0].NetMicro != 15 {
		t.Fatalf("days = %+v, want one day totalling 15", days)
	}
	if len(models) != 2 || models[0].Model != "alpha" || models[0].NetMicro != 12 {
		t.Fatalf("models = %+v, want alpha 12 first", models)
	}
	other, _, err := repo.UsageDaily(ctx, "owner-b", 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 1 || other[0].NetMicro != 100 {
		t.Fatalf("owner-b days = %+v", other)
	}

	top, err := repo.UsageExport(ctx, "owner-top", 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 11 {
		t.Fatalf("export before the cap = %d, want 11", len(top))
	}
	ranked, modelsTop, err := repo.UsageDaily(ctx, "owner-top", 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(ranked) != 1 || len(modelsTop) != 10 || modelsTop[0].NetMicro != 11 {
		t.Fatalf("top models = %d first net %d, want 10 models with 11 first", len(modelsTop), modelsTop[0].NetMicro)
	}
}

func TestTokenBankUsageExportCapsAt5000(t *testing.T) {
	repo := newSettleTestRepo(t, "tbk-usage-export.db")
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := repo.write.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO token_bank_usage (request_id, share_id, owner_user_id, model_name, net_micro, created_at) VALUES (?, 's', 'owner-export', 'm', 1, ?)`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5001; i++ {
		if _, err := stmt.ExecContext(ctx, "req-"+strconv.Itoa(i), now); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}
	_ = stmt.Close()
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.UsageExport(ctx, "owner-export", 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5000 {
		t.Fatalf("export rows = %d, want 5000", len(rows))
	}
	foreign, err := repo.UsageExport(ctx, "someone-else", 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(foreign) != 0 {
		t.Fatalf("other owner rows = %d, want 0", len(foreign))
	}
}

func TestHubBelongsToUser(t *testing.T) {
	provider := newTokenBankTestProvider(t, filepath.Join(t.TempDir(), "tbk-self.db"))
	repo := newTokenBankTestRepo(t, provider)
	ctx := context.Background()

	missing, err := repo.HubBelongsToUser(ctx, "hub-1", "user-1")
	if err != nil || missing {
		t.Fatalf("missing table match/err = %v/%v, want false and nil", missing, err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := provider.Write.ExecContext(ctx, `CREATE TABLE sm_users (
		id TEXT PRIMARY KEY, email TEXT NOT NULL UNIQUE, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO sm_users (id, email, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		"user-1", "owner@example.test", now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO hub_user_links (id, hub_id, email, is_default, created_at, updated_at) VALUES (?, ?, ?, 1, ?, ?)`,
		"link-1", "hub-1", "owner@example.test", now, now); err != nil {
		t.Fatal(err)
	}
	own, err := repo.HubBelongsToUser(ctx, "hub-1", "user-1")
	if err != nil || !own {
		t.Fatalf("own hub = %v/%v, want true", own, err)
	}
	other, err := repo.HubBelongsToUser(ctx, "hub-2", "user-1")
	if err != nil || other {
		t.Fatalf("other hub = %v/%v, want false", other, err)
	}
	alone, err := repo.HubIsExclusiveToUser(ctx, "hub-1", "user-1")
	if err != nil || !alone {
		t.Fatalf("exclusive hub = %v/%v, want true", alone, err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO hub_user_links (id, hub_id, email, is_default, created_at, updated_at) VALUES (?, ?, ?, 0, ?, ?)`,
		"link-ghost", "hub-1", "ghost@example.test", now, now); err != nil {
		t.Fatal(err)
	}
	unresolved, err := repo.HubIsExclusiveToUser(ctx, "hub-1", "user-1")
	if err != nil || unresolved {
		t.Fatalf("hub with an unresolved email = %v/%v, want false", unresolved, err)
	}
	if _, err := provider.Write.ExecContext(ctx, `DELETE FROM hub_user_links WHERE id = ?`, "link-ghost"); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO sm_users (id, email, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		"user-2", "other@example.test", now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Write.ExecContext(ctx, `INSERT INTO hub_user_links (id, hub_id, email, is_default, created_at, updated_at) VALUES (?, ?, ?, 0, ?, ?)`,
		"link-2", "hub-1", "other@example.test", now, now); err != nil {
		t.Fatal(err)
	}
	shared, err := repo.HubIsExclusiveToUser(ctx, "hub-1", "user-1")
	if err != nil || shared {
		t.Fatalf("shared hub = %v/%v, want false", shared, err)
	}
	stillLinked, err := repo.HubBelongsToUser(ctx, "hub-1", "user-1")
	if err != nil || !stillLinked {
		t.Fatalf("owner still linked = %v/%v, want true", stillLinked, err)
	}
}
