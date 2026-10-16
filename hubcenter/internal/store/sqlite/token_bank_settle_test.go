package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func newSettleTestRepo(t *testing.T, name string) *TokenBankRepo {
	t.Helper()
	provider := newTokenBankTestProvider(t, filepath.Join(t.TempDir(), name))
	return newTokenBankTestRepo(t, provider)
}

// settleInput builds a minimal settlement. The numbers are chosen so the legs
// are worth asserting by hand:
//
//	input  1_000_000 tokens @ 3.0 credits/10k = 300.0 credits = 300_000_000 micro
//	output   500_000 tokens @ 6.0 credits/10k = 300.0 credits = 300_000_000 micro
//	base 600 credits -> gross(×1.2) 720 -> fee(10%) 72 -> net 648
func settleInput(requestID string) TokenBankSettlement {
	return TokenBankSettlement{
		RequestID:        requestID,
		ShareID:          "share-1",
		OwnerID:          "owner-1",
		ShareDisplayName: "my llama",
		ModelName:        "llama-3.3-70b",
		InputTokens:      1_000_000,
		OutputTokens:     500_000,
		Tier:             "high",
		TierMultiplier:   1.2,
		FeeRate:          0.1,
		ChargedMicro:     800_000_000,
		UnitInputPer10K:  3.0,
		UnitOutputPer10K: 6.0,
		PriceBookID:      "pb-1",
		CreatedAt:        time.Now().UTC(),
	}
}

func TestComputeTokenBankSettlementRoundsEachLegUp(t *testing.T) {
	// One token below the per-leg break-even must still charge one microcredit:
	// the owner did work, and a leg rounding to zero is free work.
	gross, fee, net := ComputeTokenBankSettlement(1, 0, 0, 0, 0.0001, 0, 0, 0, 1, 0)
	if gross != 1 {
		t.Fatalf("gross for a single token = %d, want 1", gross)
	}
	if fee != 0 {
		t.Fatalf("fee = %d, want 0 (10%% of 1 rounds to 0)", fee)
	}
	if net != 1 {
		t.Fatalf("net = %d, want 1", net)
	}
	if gross2, _, _ := ComputeTokenBankSettlement(0, 0, 0, 0, 3, 6, 0, 0, 1, 0.1); gross2 != 0 {
		t.Fatalf("gross for zero tokens = %d, want 0", gross2)
	}
}

func TestComputeTokenBankSettlementCacheLegsReplaceInputPrice(t *testing.T) {
	// Measured input already includes cache read and cache write. Those tokens
	// are priced at the cache rates, not again at the input rate. Charging both
	// is what made list-price gross exceed the consumer charge.
	//
	//	input 1_000_000, cached 400_000, write 100_000
	//	normal 500_000 @ 3.0 = 150 credits
	//	cache  400_000 @ 0.3 = 12
	//	write  100_000 @ 3.75 = 37.5
	gross, _, _ := ComputeTokenBankSettlement(1_000_000, 0, 400_000, 100_000, 3, 0, 0.3, 3.75, 1, 0)
	const want = int64(199_500_000)
	if gross != want {
		t.Fatalf("gross = %d, want %d", gross, want)
	}
	// The old additive formula billed the cached half again at the input rate.
	if gross >= 300_000_000 {
		t.Fatalf("gross = %d, cache was added on top of the full input", gross)
	}
}

func TestComputeTokenBankSettlementCachePriceDoesNotApplyToPlainInput(t *testing.T) {
	gross, _, _ := ComputeTokenBankSettlement(10_000, 0, 0, 0, 3, 6, 9, 9, 1, 0)
	if gross != 3_000_000 {
		t.Fatalf("gross = %d, want 3000000 (plain input stays on the input rate)", gross)
	}
}

func TestComputeTokenBankSettlementUnsetCacheRateStaysOnInput(t *testing.T) {
	// A resolved cache unit of 0 means the platform default for that direction
	// is also 0. Those tokens stay on the input price. Peeling them off would
	// drop them from the bill and underpay the sharer. A blank price-book
	// field is resolved to the platform default before it reaches here.
	gross, _, _ := ComputeTokenBankSettlement(10_000, 0, 8_000, 0, 3, 0, 0, 0, 1, 0)
	if gross != 3_000_000 {
		t.Fatalf("gross with unset cache rates = %d, want 3000000", gross)
	}
	negative, _, _ := ComputeTokenBankSettlement(10_000, 0, 8_000, 2_000, 3, 0, -1, -1, 1, 0)
	if negative != 3_000_000 {
		t.Fatalf("gross with negative cache rates = %d, want 3000000", negative)
	}
	// A positive read rate replaces the input price for that subset only.
	// 2_000 normal @ 3 = 600_000; 8_000 cache @ 0.3 = 240_000.
	priced, _, _ := ComputeTokenBankSettlement(10_000, 0, 8_000, 0, 3, 0, 0.3, 0, 1, 0)
	if priced != 840_000 {
		t.Fatalf("gross with a cache-read rate = %d, want 840000", priced)
	}
	// The two directions are independent. An unset read rate leaves those
	// tokens on input, while a positive write rate still replaces its subset.
	// The write is clamped to the input left after the measured read: 2_000.
	// 8_000 @ 3 = 2_400_000; 2_000 @ 3.75 = 750_000.
	mixed, _, _ := ComputeTokenBankSettlement(10_000, 0, 8_000, 2_000, 3, 0, 0, 3.75, 1, 0)
	if mixed != 3_150_000 {
		t.Fatalf("gross with only a write rate = %d, want 3150000", mixed)
	}
}

func TestComputeTokenBankSettlementCacheAboveInputIsClamped(t *testing.T) {
	// Cache that the input does not contain is not an extra leg. Consumer
	// billing clamps the same way, so the two sides stay on one measurement.
	gross, _, _ := ComputeTokenBankSettlement(0, 0, 1_000_000, 1_000_000, 0, 0, 2, 4, 1, 0)
	if gross != 0 {
		t.Fatalf("gross = %d, want 0 when input does not contain the cache tokens", gross)
	}
	// 10_000 input can hold 8_000 reads and only 2_000 of the reported writes.
	capped, _, _ := ComputeTokenBankSettlement(10_000, 0, 8_000, 5_000, 1, 0, 2, 4, 1, 0)
	if capped != 2_400_000 {
		t.Fatalf("capped gross = %d, want 2400000", capped)
	}
}

func TestComputeTokenBankSettlementClampsFeeRate(t *testing.T) {
	// A fee rate above 1 (a settings typo) must not produce a negative net.
	gross, fee, net := ComputeTokenBankSettlement(1_000_000, 0, 0, 0, 3, 0, 0, 0, 1, 5)
	if gross != 300_000_000 {
		t.Fatalf("gross = %d, want 300000000", gross)
	}
	if fee != gross {
		t.Fatalf("fee = %d, want %d (clamped to 100%%)", fee, gross)
	}
	if net != 0 {
		t.Fatalf("net = %d, want 0", net)
	}
}

func TestSettleTokenBankUsageFormulaUsesThePricedSplit(t *testing.T) {
	repo := newSettleTestRepo(t, "tbk-settle-cache-formula.db")
	ctx := context.Background()

	unset := settleInput("req-cache-unset")
	unset.CachedInputTokens = 400_000
	unset.CacheWriteTokens = 100_000
	out, err := repo.SettleTokenBankUsage(ctx, unset)
	if err != nil {
		t.Fatalf("unset cache SettleTokenBankUsage() error = %v", err)
	}
	wantGross, _, _ := ComputeTokenBankSettlement(
		unset.InputTokens, unset.OutputTokens, unset.CachedInputTokens, unset.CacheWriteTokens,
		unset.UnitInputPer10K, unset.UnitOutputPer10K, unset.UnitCachedReadPer10K, unset.UnitCacheWritePer10K,
		unset.TierMultiplier, unset.FeeRate)
	if out.GrossMicro != wantGross || out.GrossMicro != 720_000_000 {
		t.Fatalf("unset gross = %d, want %d and 720000000", out.GrossMicro, wantGross)
	}
	rows, err := repo.ListUsage(ctx, unset.OwnerID, 10, unset.ShareID)
	if err != nil {
		t.Fatalf("ListUsage() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("usage rows = %d, want 1", len(rows))
	}
	for _, piece := range []string{
		`"billable_input_tokens":1000000`,
		`"priced_cached_tokens":0`,
		`"priced_cache_write_tokens":0`,
		`"priced_output_tokens":500000`,
	} {
		if !strings.Contains(rows[0].FormulaJSON, piece) {
			t.Fatalf("formula = %s, want %s", rows[0].FormulaJSON, piece)
		}
	}

	priced := unset
	priced.RequestID = "req-cache-priced"
	priced.UnitCachedReadPer10K = 0.3
	priced.UnitCacheWritePer10K = 3.75
	pricedOut, err := repo.SettleTokenBankUsage(ctx, priced)
	if err != nil {
		t.Fatalf("priced cache SettleTokenBankUsage() error = %v", err)
	}
	wantPriced, _, _ := ComputeTokenBankSettlement(
		priced.InputTokens, priced.OutputTokens, priced.CachedInputTokens, priced.CacheWriteTokens,
		priced.UnitInputPer10K, priced.UnitOutputPer10K, priced.UnitCachedReadPer10K, priced.UnitCacheWritePer10K,
		priced.TierMultiplier, priced.FeeRate)
	if pricedOut.GrossMicro != wantPriced {
		t.Fatalf("priced gross = %d, want %d", pricedOut.GrossMicro, wantPriced)
	}
	if pricedOut.GrossMicro >= out.GrossMicro {
		t.Fatalf("priced gross = %d, cache replaced the input price and should be below %d", pricedOut.GrossMicro, out.GrossMicro)
	}
	rows, err = repo.ListUsage(ctx, priced.OwnerID, 10, priced.ShareID)
	if err != nil {
		t.Fatalf("ListUsage() error = %v", err)
	}
	var formula string
	for _, row := range rows {
		if row.RequestID == priced.RequestID {
			formula = row.FormulaJSON
		}
	}
	for _, piece := range []string{
		`"billable_input_tokens":500000`,
		`"priced_cached_tokens":400000`,
		`"priced_cache_write_tokens":100000`,
		`"priced_output_tokens":500000`,
	} {
		if !strings.Contains(formula, piece) {
			t.Fatalf("formula = %s, want %s", formula, piece)
		}
	}
}

func TestSettleTokenBankUsageCreditsOwner(t *testing.T) {
	repo := newSettleTestRepo(t, "tbk-settle.db")
	ctx := context.Background()

	out, err := repo.SettleTokenBankUsage(ctx, settleInput("req-1"))
	if err != nil {
		t.Fatalf("SettleTokenBankUsage() error = %v", err)
	}
	if !out.Applied {
		t.Fatalf("Applied = false, want true")
	}
	if out.GrossMicro != 720_000_000 || out.FeeMicro != 72_000_000 || out.NetMicro != 648_000_000 {
		t.Fatalf("gross/fee/net = %d/%d/%d, want 720000000/72000000/648000000",
			out.GrossMicro, out.FeeMicro, out.NetMicro)
	}
	if out.NetClamped {
		t.Fatalf("NetClamped = true, want false (charged exceeds net)")
	}

	balance, err := repo.Balance(ctx, "owner-1")
	if err != nil {
		t.Fatalf("Balance() error = %v", err)
	}
	if got := balance.EarnedMicro; got != 648_000_000 {
		t.Fatalf("earned = %d, want 648000000", got)
	}
	if got := balance.AvailableMicro(); got != 648_000_000 {
		t.Fatalf("available = %d, want 648000000", got)
	}
}

func TestSettleTokenBankUsageMaintainsShareAndModelEarnings(t *testing.T) {
	repo := newSettleTestRepo(t, "tbk-settle-counters.db")
	ctx := context.Background()
	now := time.Now().UTC()
	// The stored spelling differs in case from the settlement. The counter
	// update matches the way the token counters already match.
	if _, _, err := repo.CreateShare(ctx, shareFixture("share-1", "owner-1", "fp-1"),
		[]TokenBankShareModel{modelFixture("Llama-3.3-70b")}, 0, now); err != nil {
		t.Fatalf("CreateShare() error = %v", err)
	}
	in := settleInput("req-1")
	in.ConsumerHubID = "hub-9"
	in.ConsumerTenantID = "tenant-9"

	out, err := repo.SettleTokenBankUsage(ctx, in)
	if err != nil {
		t.Fatalf("SettleTokenBankUsage() error = %v", err)
	}
	if !out.Applied || out.NetMicro != 648_000_000 {
		t.Fatalf("outcome = %+v, want applied net 648000000", out)
	}
	assertEarnedCounters(t, repo, out.NetMicro, in.InputTokens, in.OutputTokens)

	replay, err := repo.SettleTokenBankUsage(ctx, in)
	if err != nil {
		t.Fatalf("replay SettleTokenBankUsage() error = %v", err)
	}
	if replay.Applied {
		t.Fatalf("replay Applied = true, want false")
	}
	assertEarnedCounters(t, repo, out.NetMicro, in.InputTokens, in.OutputTokens)

	rows, err := repo.ListUsage(ctx, "owner-1", 10, "share-1")
	if err != nil {
		t.Fatalf("ListUsage() error = %v", err)
	}
	if len(rows) != 1 || rows[0].ConsumerHubID != "hub-9" || rows[0].ConsumerTenantID != "tenant-9" {
		t.Fatalf("usage consumer = %+v, want one row hub-9/tenant-9", rows)
	}
	if rows[0].SelfUse {
		t.Fatal("SelfUse = true on an outside call")
	}

	in2 := in
	in2.RequestID = "req-2"
	second, err := repo.SettleTokenBankUsage(ctx, in2)
	if err != nil {
		t.Fatalf("second SettleTokenBankUsage() error = %v", err)
	}
	assertEarnedCounters(t, repo, out.NetMicro+second.NetMicro, in.InputTokens*2, in.OutputTokens*2)
}

func TestSettleTokenBankUsageCreditsSelfUse(t *testing.T) {
	repo := newSettleTestRepo(t, "tbk-settle-self-use.db")
	ctx := context.Background()
	now := time.Now().UTC()
	if _, _, err := repo.CreateShare(ctx, shareFixture("share-1", "owner-1", "fp-self"),
		[]TokenBankShareModel{modelFixture("Llama-3.3-70b")}, 0, now); err != nil {
		t.Fatalf("CreateShare() error = %v", err)
	}
	in := settleInput("req-self")
	in.ConsumerHubID = "hub-owner"
	in.SelfUse = true

	out, err := repo.SettleTokenBankUsage(ctx, in)
	if err != nil {
		t.Fatalf("SettleTokenBankUsage() error = %v", err)
	}
	if !out.Applied || out.NetMicro != 648_000_000 {
		t.Fatalf("outcome = %+v, want the same net as an outside call", out)
	}
	balance, err := repo.Balance(ctx, "owner-1")
	if err != nil {
		t.Fatalf("Balance() error = %v", err)
	}
	if balance.EarnedMicro != out.NetMicro {
		t.Fatalf("earned = %d, want %d on a self-use call", balance.EarnedMicro, out.NetMicro)
	}
	rows, err := repo.ListUsage(ctx, "owner-1", 10, "share-1")
	if err != nil {
		t.Fatalf("ListUsage() error = %v", err)
	}
	if len(rows) != 1 || !rows[0].SelfUse || rows[0].ChargedMicro != in.ChargedMicro || rows[0].NetMicro != out.NetMicro {
		t.Fatalf("usage = %+v, want one self-use row with both the charge and the earning", rows)
	}
	if !strings.Contains(rows[0].FormulaJSON, `"self_use":true`) {
		t.Fatalf("formula = %s, want self_use recorded", rows[0].FormulaJSON)
	}
	exported, err := repo.UsageExport(ctx, "owner-1", 30)
	if err != nil {
		t.Fatalf("UsageExport() error = %v", err)
	}
	if len(exported) != 1 || !exported[0].SelfUse || exported[0].ChargedMicro != in.ChargedMicro || exported[0].NetMicro != out.NetMicro {
		t.Fatalf("export = %+v, want the self-use mark beside the charge and the earning", exported)
	}
}

func assertEarnedCounters(t *testing.T, repo *TokenBankRepo, wantEarned, wantIn, wantOut int64) {
	t.Helper()
	ctx := context.Background()
	share, err := repo.LoadShare(ctx, "share-1", "")
	if err != nil {
		t.Fatalf("LoadShare() error = %v", err)
	}
	if share.TotalEarnedMicro != wantEarned {
		t.Fatalf("share total_earned_micro = %d, want %d", share.TotalEarnedMicro, wantEarned)
	}
	models, err := repo.ListModels(ctx, "share-1")
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(models) != 1 || models[0].EarnedMicro != wantEarned ||
		models[0].UsedInputTokens != wantIn || models[0].UsedOutputTokens != wantOut {
		t.Fatalf("model counters = %+v, want earned %d in %d out %d", models, wantEarned, wantIn, wantOut)
	}
}

func TestSettleTokenBankUsageIsIdempotent(t *testing.T) {
	repo := newSettleTestRepo(t, "tbk-settle-idem.db")
	ctx := context.Background()
	in := settleInput("req-1")

	first, err := repo.SettleTokenBankUsage(ctx, in)
	if err != nil {
		t.Fatalf("first SettleTokenBankUsage() error = %v", err)
	}
	if !first.Applied {
		t.Fatalf("first Applied = false, want true")
	}

	// A retry — the proxy re-running the settlement after a network hiccup, or
	// HA replaying the same movement — must converge, not double-credit.
	second, err := repo.SettleTokenBankUsage(ctx, in)
	if err != nil {
		t.Fatalf("second SettleTokenBankUsage() error = %v", err)
	}
	if second.Applied {
		t.Fatalf("second Applied = true, want false (already settled)")
	}
	if second.NetMicro != first.NetMicro {
		t.Fatalf("replay net = %d, want %d (canonical from the first write)", second.NetMicro, first.NetMicro)
	}

	balance, err := repo.Balance(ctx, "owner-1")
	if err != nil {
		t.Fatalf("Balance() error = %v", err)
	}
	if got := balance.EarnedMicro; got != 648_000_000 {
		t.Fatalf("earned after replay = %d, want 648000000", got)
	}

	var ledgerCount int
	if err := repo.read.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM token_bank_ledger WHERE user_id = 'owner-1' AND bucket = ?`,
		TokenBankBucketEarned).Scan(&ledgerCount); err != nil {
		t.Fatalf("count ledger: %v", err)
	}
	if ledgerCount != 1 {
		t.Fatalf("ledger rows = %d, want 1", ledgerCount)
	}
}

func TestSettleTokenBankUsageReplayReturnsCanonicalAmounts(t *testing.T) {
	repo := newSettleTestRepo(t, "tbk-settle-canonical.db")
	ctx := context.Background()

	if _, err := repo.SettleTokenBankUsage(ctx, settleInput("req-1")); err != nil {
		t.Fatalf("first settle: %v", err)
	}

	// The retry carries a different tier multiplier (an admin regrade landed in
	// between). The reply must describe what was actually applied, not what the
	// caller would have applied had it been first.
	changed := settleInput("req-1")
	changed.TierMultiplier = 2.0
	out, err := repo.SettleTokenBankUsage(ctx, changed)
	if err != nil {
		t.Fatalf("replay settle: %v", err)
	}
	if out.Applied {
		t.Fatalf("Applied = true, want false")
	}
	if out.NetMicro != 648_000_000 {
		t.Fatalf("replay net = %d, want 648000000 (the original, not the regraded 1080000000)", out.NetMicro)
	}
}

func TestSettleTokenBankUsageDistinctModelsSettleSeparately(t *testing.T) {
	repo := newSettleTestRepo(t, "tbk-settle-two-models.db")
	ctx := context.Background()

	a := settleInput("req-1")
	a.ModelName = "llama-70b"
	b := settleInput("req-1")
	b.ModelName = "qwen-32b"

	if _, err := repo.SettleTokenBankUsage(ctx, a); err != nil {
		t.Fatalf("settle a: %v", err)
	}
	outB, err := repo.SettleTokenBankUsage(ctx, b)
	if err != nil {
		t.Fatalf("settle b: %v", err)
	}
	// Same request id, different model: both are real usage and both pay.
	if !outB.Applied {
		t.Fatalf("second model Applied = false, want true")
	}
	balance, err := repo.Balance(ctx, "owner-1")
	if err != nil {
		t.Fatalf("Balance() error = %v", err)
	}
	if got := balance.EarnedMicro; got != 2*648_000_000 {
		t.Fatalf("earned = %d, want 1296000000", got)
	}
}

func TestSettleTokenBankUsageClampsToCharged(t *testing.T) {
	repo := newSettleTestRepo(t, "tbk-settle-clamp.db")
	ctx := context.Background()

	in := settleInput("req-1")
	in.ChargedMicro = 100_000_000 // the consumer paid far less than the model "earned"

	out, err := repo.SettleTokenBankUsage(ctx, in)
	if err != nil {
		t.Fatalf("SettleTokenBankUsage() error = %v", err)
	}
	if !out.NetClamped {
		t.Fatalf("NetClamped = false, want true")
	}
	if out.NetMicro != 100_000_000 {
		t.Fatalf("net = %d, want 100000000 (clamped to charged)", out.NetMicro)
	}
	// The clamp is applied to the payout only; gross and fee still describe the
	// model's list price, so the owner can see what was withheld by the clamp.
	if out.GrossMicro != 720_000_000 || out.FeeMicro != 72_000_000 {
		t.Fatalf("gross/fee = %d/%d, want 720000000/72000000", out.GrossMicro, out.FeeMicro)
	}
}

func TestSettleTokenBankUsageZeroChargedPaysNothing(t *testing.T) {
	repo := newSettleTestRepo(t, "tbk-settle-free.db")
	ctx := context.Background()

	in := settleInput("req-1")
	in.ChargedMicro = 0 // free route

	out, err := repo.SettleTokenBankUsage(ctx, in)
	if err != nil {
		t.Fatalf("SettleTokenBankUsage() error = %v", err)
	}
	if !out.Applied {
		t.Fatalf("Applied = false, want true (the usage happened)")
	}
	if !out.NetClamped || out.NetMicro != 0 {
		t.Fatalf("net/clamped = %d/%v, want 0/true", out.NetMicro, out.NetClamped)
	}
	// The usage row exists so the operator can explain the zero; no ledger row
	// exists because there was no movement.
	balance, err := repo.Balance(ctx, "owner-1")
	if err != nil {
		t.Fatalf("Balance() error = %v", err)
	}
	if got := balance.EarnedMicro; got != 0 {
		t.Fatalf("earned = %d, want 0", got)
	}
	rows, err := repo.ListUsage(ctx, "owner-1", 10, "")
	if err != nil {
		t.Fatalf("ListUsage() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("usage rows = %d, want 1", len(rows))
	}
	if !rows[0].NetClamped {
		t.Fatalf("row NetClamped = false, want true")
	}
}

func TestSettleTokenBankUsageMissingOwnerIsNotApplied(t *testing.T) {
	repo := newSettleTestRepo(t, "tbk-settle-noowner.db")
	ctx := context.Background()

	in := settleInput("req-1")
	in.OwnerID = "" // the share was withdrawn between dispatch and settle

	out, err := repo.SettleTokenBankUsage(ctx, in)
	if err != nil {
		t.Fatalf("SettleTokenBankUsage() error = %v", err)
	}
	if out.Applied || out.NetMicro != 0 {
		t.Fatalf("applied/net = %v/%d, want false/0", out.Applied, out.NetMicro)
	}
	rows, err := repo.ListUsage(ctx, "owner-1", 10, "")
	if err != nil {
		t.Fatalf("ListUsage() error = %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("usage rows = %d, want 0", len(rows))
	}
}

func TestSettleTokenBankUsageRequiresKeys(t *testing.T) {
	repo := newSettleTestRepo(t, "tbk-settle-keys.db")
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		mut  func(*TokenBankSettlement)
	}{
		{"no request", func(s *TokenBankSettlement) { s.RequestID = "" }},
		{"no share", func(s *TokenBankSettlement) { s.ShareID = "" }},
		{"no model", func(s *TokenBankSettlement) { s.ModelName = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := settleInput("req-1")
			tc.mut(&in)
			if _, err := repo.SettleTokenBankUsage(ctx, in); err == nil {
				t.Fatalf("SettleTokenBankUsage() error = nil, want a key error")
			}
		})
	}
}

// TestSettleTokenBankUsageConcurrentIsExactlyOnce is the E1 concurrency test
// §10 P0-3 calls for: many goroutines settling the same call, and many
// settling distinct calls, must never lose an earned credit and never mint a
// duplicate one.
func TestSettleTokenBankUsageConcurrentIsExactlyOnce(t *testing.T) {
	repo := newSettleTestRepo(t, "tbk-settle-concurrent.db")
	ctx := context.Background()

	const (
		distinctCalls = 24
		duplicates    = 4
	)
	var wg sync.WaitGroup
	errs := make(chan error, distinctCalls*duplicates)
	for i := 0; i < distinctCalls; i++ {
		for j := 0; j < duplicates; j++ {
			wg.Add(1)
			go func(i, j int) {
				defer wg.Done()
				in := settleInput("req-" + itoa(i))
				if _, err := repo.SettleTokenBankUsage(ctx, in); err != nil {
					errs <- err
				}
			}(i, j)
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent settle: %v", err)
	}

	balance, err := repo.Balance(ctx, "owner-1")
	if err != nil {
		t.Fatalf("Balance() error = %v", err)
	}
	want := int64(distinctCalls) * 648_000_000
	if got := balance.EarnedMicro; got != want {
		t.Fatalf("earned = %d, want %d (exactly once per distinct call)", got, want)
	}

	var usageCount, ledgerCount int
	if err := repo.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM token_bank_usage`).Scan(&usageCount); err != nil {
		t.Fatalf("count usage: %v", err)
	}
	if err := repo.read.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM token_bank_ledger WHERE bucket = ?`, TokenBankBucketEarned).Scan(&ledgerCount); err != nil {
		t.Fatalf("count ledger: %v", err)
	}
	if usageCount != distinctCalls {
		t.Fatalf("usage rows = %d, want %d", usageCount, distinctCalls)
	}
	if ledgerCount != distinctCalls {
		t.Fatalf("ledger rows = %d, want %d", ledgerCount, distinctCalls)
	}
}

func TestListUsageAndSumUsageMicro(t *testing.T) {
	repo := newSettleTestRepo(t, "tbk-settle-read.db")
	ctx := context.Background()

	base := time.Now().UTC().Add(-time.Hour)
	for i := 0; i < 3; i++ {
		in := settleInput("req-" + itoa(i))
		in.ShareID = "share-" + itoa(i%2)
		in.CreatedAt = base.Add(time.Duration(i) * time.Minute)
		if _, err := repo.SettleTokenBankUsage(ctx, in); err != nil {
			t.Fatalf("settle %d: %v", i, err)
		}
	}

	all, err := repo.ListUsage(ctx, "owner-1", 10, "")
	if err != nil {
		t.Fatalf("ListUsage() error = %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("ListUsage rows = %d, want 3", len(all))
	}
	// Newest first.
	if all[0].RequestID != "req-2" {
		t.Fatalf("first row = %q, want req-2 (newest)", all[0].RequestID)
	}

	scoped, err := repo.ListUsage(ctx, "owner-1", 10, "share-0")
	if err != nil {
		t.Fatalf("ListUsage(share) error = %v", err)
	}
	if len(scoped) != 2 {
		t.Fatalf("scoped rows = %d, want 2", len(scoped))
	}

	gross, fee, net, err := repo.SumUsageMicro(ctx, "owner-1", time.Time{})
	if err != nil {
		t.Fatalf("SumUsageMicro() error = %v", err)
	}
	if net != 3*648_000_000 || gross != 3*720_000_000 || fee != 3*72_000_000 {
		t.Fatalf("sum = %d/%d/%d, want 1944000000/2160000000/216000000", gross, fee, net)
	}

	// A window after the first call drops it.
	gross, _, net, err = repo.SumUsageMicro(ctx, "owner-1", base.Add(30*time.Second))
	if err != nil {
		t.Fatalf("SumUsageMicro(window) error = %v", err)
	}
	if net != 2*648_000_000 || gross != 2*720_000_000 {
		t.Fatalf("windowed sum = %d/%d, want 1296000000/1440000000", gross, net)
	}
}

// itoa avoids importing strconv into a test file that mostly does not need it,
// and keeps the goroutine bodies allocation-light.
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
