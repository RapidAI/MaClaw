package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// Gross-margin view tests (§9 #20).
//
// The three cases below are the whole point of the view, and they are chosen to
// separate two things that are easy to conflate:
//
//	fee absorbs inversion   gross > charged, but net <= charged -> margin still > 0
//	clamped inversion       net > charged, so §5 ⑥ caps payout -> margin == 0
//
// A view that only reported the margin would show the first case as perfectly
// healthy and never surface it at all.

func newMarginTestRepo(t *testing.T, name string) *TokenBankRepo {
	t.Helper()
	provider := newTokenBankTestProvider(t, filepath.Join(t.TempDir(), name))
	return newTokenBankTestRepo(t, provider)
}

// seedMarginShare creates the share its settlements will refer to.
func seedMarginShare(t *testing.T, repo *TokenBankRepo, shareID, owner string) {
	t.Helper()
	share := shareFixture(shareID, owner, "fp-"+shareID)
	if _, _, err := repo.CreateShare(context.Background(), share,
		[]TokenBankShareModel{modelFixture("llama-3.3-70b")}, 0, time.Now().UTC()); err != nil {
		t.Fatalf("CreateShare(%s): %v", shareID, err)
	}
}

// settleOne settles a single call with an explicit consumer charge.
func settleOne(t *testing.T, repo *TokenBankRepo, requestID, shareID, owner, model string, chargedMicro int64) {
	t.Helper()
	in := settleInput(requestID)
	in.ShareID = shareID
	in.OwnerID = owner
	in.ModelName = model
	in.ChargedMicro = chargedMicro
	if _, err := repo.SettleTokenBankUsage(context.Background(), in); err != nil {
		t.Fatalf("SettleTokenBankUsage(%s): %v", requestID, err)
	}
}

// total returns the grand-total row, which UsageMargins always appends last.
func total(rows []TokenBankMarginRow) TokenBankMarginRow {
	if len(rows) == 0 {
		return TokenBankMarginRow{}
	}
	return rows[len(rows)-1]
}

func TestUsageMarginsHealthyCallSplitsChargeIntoPayoutAndMargin(t *testing.T) {
	// charged 800, gross 720, fee 72, net 648 -> margin 152.
	repo := newMarginTestRepo(t, "tbk-margin-healthy.db")
	seedMarginShare(t, repo, "share-1", "owner-1")
	settleOne(t, repo, "m-1", "share-1", "owner-1", "llama-3.3-70b", 800_000_000)

	rows, err := repo.UsageMargins(context.Background(), 30, TokenBankMarginByModel)
	if err != nil {
		t.Fatalf("UsageMargins: %v", err)
	}
	got := total(rows)
	if got.ChargedMicro != 800_000_000 {
		t.Fatalf("charged = %d, want 800000000", got.ChargedMicro)
	}
	if got.NetMicro != 648_000_000 {
		t.Fatalf("net = %d, want 648000000", got.NetMicro)
	}
	if got.MarginMicro != 152_000_000 {
		t.Fatalf("margin = %d, want 152000000 (charged - net)", got.MarginMicro)
	}
	if got.InvertedCount != 0 || got.ClampedCount != 0 {
		t.Fatalf("inverted/clamped = %d/%d, want 0/0", got.InvertedCount, got.ClampedCount)
	}
	if rate := got.MarginRate(); rate <= 0 || rate >= 1 {
		t.Fatalf("margin rate = %v, want between 0 and 1", rate)
	}
}

func TestUsageMarginsFlagsInversionTheFeeAbsorbed(t *testing.T) {
	// Regression for the whole point of §9 #20: the clamp is NOT the inversion
	// signal. Here gross(720) > charged(700), so the platform sold the call for
	// less than its own list price would pay out — but net(648) is still under
	// charged(700), so nothing is clamped and the margin stays positive.
	// A view that watched only `net_clamped` would report this as healthy.
	repo := newMarginTestRepo(t, "tbk-margin-absorbed.db")
	seedMarginShare(t, repo, "share-1", "owner-1")
	settleOne(t, repo, "m-1", "share-1", "owner-1", "llama-3.3-70b", 700_000_000)

	got := total(mustMargins(t, repo, 30, TokenBankMarginByModel))
	if got.InvertedCount != 1 {
		t.Fatalf("inverted = %d, want 1 (gross 720M > charged 700M)", got.InvertedCount)
	}
	if got.ClampedCount != 0 {
		t.Fatalf("clamped = %d, want 0 (the fee absorbed the gap)", got.ClampedCount)
	}
	if got.MarginMicro != 52_000_000 {
		t.Fatalf("margin = %d, want 52000000", got.MarginMicro)
	}
	if got.ShortfallMicro != 0 {
		t.Fatalf("shortfall = %d, want 0; the fee covered the gap, so the clamp held nothing back", got.ShortfallMicro)
	}
}

func TestUsageMarginsClampedInversionLeavesZeroMargin(t *testing.T) {
	// The severe case: net(648) > charged(600), so §5 ⑥ caps the payout at the
	// charge and the platform's margin is exactly zero — never negative, which
	// is why the margin figure alone cannot reveal inversion.
	repo := newMarginTestRepo(t, "tbk-margin-clamped.db")
	seedMarginShare(t, repo, "share-1", "owner-1")
	settleOne(t, repo, "m-1", "share-1", "owner-1", "llama-3.3-70b", 600_000_000)

	got := total(mustMargins(t, repo, 30, TokenBankMarginByModel))
	if got.ClampedCount != 1 {
		t.Fatalf("clamped = %d, want 1", got.ClampedCount)
	}
	if got.NetMicro != 600_000_000 {
		t.Fatalf("net = %d, want 600000000 (clamped to charged)", got.NetMicro)
	}
	if got.MarginMicro != 0 {
		t.Fatalf("margin = %d, want 0; §5 ⑥ caps the payout at the charge", got.MarginMicro)
	}
	// Unclamped net is 648. The clamp pays 600, so it holds back 48.
	if got.ShortfallMicro != 48_000_000 {
		t.Fatalf("shortfall = %d, want 48000000 (gross - fee - clamped net)", got.ShortfallMicro)
	}
}

func TestUsageMarginsFreeRouteHasNoRateButStillCountsInversion(t *testing.T) {
	// A free route charges nothing. §5 ⑥ clamps the payout to zero, so the
	// margin is zero and there is no rate to report — but the platform still
	// produced 720M of list-price work for free, which is the most extreme
	// inversion there is and must not vanish from the view.
	repo := newMarginTestRepo(t, "tbk-margin-free.db")
	seedMarginShare(t, repo, "share-1", "owner-1")
	settleOne(t, repo, "m-1", "share-1", "owner-1", "llama-3.3-70b", 0)

	got := total(mustMargins(t, repo, 30, TokenBankMarginByModel))
	if got.ChargedMicro != 0 || got.NetMicro != 0 {
		t.Fatalf("charged/net = %d/%d, want 0/0", got.ChargedMicro, got.NetMicro)
	}
	if got.MarginRate() != 0 {
		t.Fatalf("margin rate = %v, want 0; a free route has no rate (and no division by zero)", got.MarginRate())
	}
	if got.InvertedCount != 1 {
		t.Fatalf("inverted = %d, want 1; free work is the worst inversion", got.InvertedCount)
	}
	// List gross is 720 and the fee is 72. The clamp pays nothing, so the
	// sharer payout it holds back is 648, not the whole list price.
	if got.ShortfallMicro != 648_000_000 {
		t.Fatalf("shortfall = %d, want 648000000 (gross - fee)", got.ShortfallMicro)
	}
}

func TestUsageMarginsGroupsAndAlwaysAppendsTotal(t *testing.T) {
	repo := newMarginTestRepo(t, "tbk-margin-groups.db")
	seedMarginShare(t, repo, "share-1", "owner-1")

	in := settleInput("m-base")
	in.ShareID = "share-1"
	in.OwnerID = "owner-1"
	in.ChargedMicro = 800_000_000
	for _, model := range []string{"llama-3.3-70b", "qwen-2.5-72b"} {
		in.RequestID = "m-" + model
		in.ModelName = model
		if _, err := repo.SettleTokenBankUsage(context.Background(), in); err != nil {
			t.Fatalf("settle %s: %v", model, err)
		}
	}

	rows := mustMargins(t, repo, 30, TokenBankMarginByModel)
	// Two models plus the total.
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3 (2 models + total)", len(rows))
	}
	if rows[len(rows)-1].Key != "" {
		t.Fatalf("last row key = %q, want the empty total key", rows[len(rows)-1].Key)
	}
	// The total must equal the sum of the groups, or the view is lying.
	var sum int64
	for _, row := range rows[:len(rows)-1] {
		sum += row.NetMicro
	}
	if sum != rows[len(rows)-1].NetMicro {
		t.Fatalf("groups sum to %d but total is %d", sum, rows[len(rows)-1].NetMicro)
	}

	byShare := mustMargins(t, repo, 30, TokenBankMarginByShare)
	if len(byShare) != 2 || byShare[0].Key != "share-1" {
		t.Fatalf("share groups = %+v, want one share plus the total", byShare)
	}
	byDay := mustMargins(t, repo, 30, TokenBankMarginByDay)
	if len(byDay) != 2 {
		t.Fatalf("day groups = %d, want one day plus the total", len(byDay))
	}
}

func TestUsageMarginsUnknownGroupFallsBackToTotalOnly(t *testing.T) {
	// A group that is not whitelisted must degrade to the grand total rather
	// than reaching SQL as a column name.
	repo := newMarginTestRepo(t, "tbk-margin-unknown.db")
	seedMarginShare(t, repo, "share-1", "owner-1")
	settleOne(t, repo, "m-1", "share-1", "owner-1", "llama-3.3-70b", 800_000_000)

	rows := mustMargins(t, repo, 30, TokenBankMarginGroup("model_name; DROP TABLE token_bank_usage"))
	if len(rows) != 1 || rows[0].Key != "" {
		t.Fatalf("rows = %+v, want a single empty-key total", rows)
	}
	if rows[0].Calls != 1 {
		t.Fatalf("calls = %d, want 1", rows[0].Calls)
	}
}

func TestUsageMarginsEmptyWindowReturnsOnlyAZeroedTotal(t *testing.T) {
	repo := newMarginTestRepo(t, "tbk-margin-empty.db")
	rows := mustMargins(t, repo, 30, TokenBankMarginByModel)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want just the total", len(rows))
	}
	if rows[0].Calls != 0 || rows[0].MarginMicro != 0 {
		t.Fatalf("empty total = %+v, want zeroed", rows[0])
	}
}

func mustMargins(t *testing.T, repo *TokenBankRepo, days int, group TokenBankMarginGroup) []TokenBankMarginRow {
	t.Helper()
	rows, err := repo.UsageMargins(context.Background(), days, group)
	if err != nil {
		t.Fatalf("UsageMargins(%s): %v", group, err)
	}
	return rows
}
