package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
)

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(raw)
}

// newSettlerTestRepo opens a real store so the adapter is exercised against the
// actual schema. A fake repository would let the adapter's field mapping drift
// away from the columns, which is exactly the mistake this test exists to catch.
func newSettlerTestRepo(t *testing.T, name string) (*sqlite.TokenBankRepo, *store.Store) {
	t.Helper()
	provider, err := sqlite.NewProvider(sqlite.Config{
		DSN:               filepath.Join(t.TempDir(), name),
		WAL:               false,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  2,
		MaxReadIdleConns:  1,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	if err := sqlite.EnsureLLMTables(provider.Write); err != nil {
		t.Fatalf("EnsureLLMTables: %v", err)
	}
	return sqlite.NewTokenBankRepo(provider), sqlite.NewStore(provider)
}

// createShare records a share with one model at the given tier.
func createShare(t *testing.T, repo *sqlite.TokenBankRepo, shareID, owner, displayName, modelName, tier string, multiplier float64) {
	t.Helper()
	_, _, err := repo.CreateShare(context.Background(), sqlite.TokenBankShare{
		ID:          shareID,
		OwnerUserID: owner,
		DisplayName: displayName,
		Status:      "available",
		// Settlement never touches the encrypted credential, but the store
		// requires a fingerprint and a ciphertext to create a share at all, so
		// the fixture supplies opaque placeholders.
		KeyFingerprint: "fp-" + shareID,
		EncryptedKey:   "ciphertext-" + shareID,
		APIURL:         "https://upstream.example/v1",
	}, []sqlite.TokenBankShareModel{{
		ModelName:      modelName,
		MemberID:       llmservice.TokenBankMemberID(shareID, modelName),
		ArrayID:        llmservice.TokenBankArrayForTier(tier),
		Tier:           tier,
		TierMultiplier: multiplier,
		Enabled:        true,
		Available:      true,
	}}, 10, time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateShare: %v", err)
	}
}

func TestTokenBankProxySettlerResolvesShareAndCreditsOwner(t *testing.T) {
	repo, st := newSettlerTestRepo(t, "tbk-settler.db")
	ctx := context.Background()
	createShare(t, repo, "share-1", "owner-1", "my llama", "llama-3.3-70b", "high", 1.2)

	settler := newTokenBankProxySettler(repo, st.System)
	if settler == nil {
		t.Fatalf("newTokenBankProxySettler returned nil for a live repo")
	}

	view, found, err := settler.TokenBankShareForPublish(ctx, "share-1", "llama-3.3-70b")
	if err != nil {
		t.Fatalf("TokenBankShareForPublish: %v", err)
	}
	if !found {
		t.Fatalf("found = false, want true for a live share")
	}
	if view.OwnerUserID != "owner-1" || view.ShareDisplayName != "my llama" {
		t.Fatalf("owner/name = %q/%q, want owner-1/my llama", view.OwnerUserID, view.ShareDisplayName)
	}
	if view.Tier != "high" || view.TierMultiplier != 1.2 {
		t.Fatalf("tier/multiplier = %q/%v, want high/1.2", view.Tier, view.TierMultiplier)
	}
	if view.FeeRate <= 0 || view.FeeRate > 1 {
		t.Fatalf("FeeRate = %v, want a sane default in (0,1]", view.FeeRate)
	}

	outcome, err := settler.SettleTokenBankUsage(ctx, llmservice.TokenBankSettlementInput{
		RequestID:        "req-1",
		ShareID:          "share-1",
		Model:            "llama-3.3-70b",
		OwnerID:          view.OwnerUserID,
		ShareDisplayName: view.ShareDisplayName,
		Tier:             view.Tier,
		TierMultiplier:   view.TierMultiplier,
		FeeRate:          view.FeeRate,
		InputTokens:      1_000_000,
		OutputTokens:     500_000,
		UnitInputPer10K:  3,
		UnitOutputPer10K: 6,
		ChargedMicro:     1_000_000_000,
		ConsumerHubID:    "hub-9",
		ConsumerTenantID: "tenant-9",
	})
	if err != nil {
		t.Fatalf("SettleTokenBankUsage: %v", err)
	}
	if !outcome.Applied || outcome.NetMicro <= 0 {
		t.Fatalf("outcome = %+v, want applied with a positive net", outcome)
	}

	balance, err := repo.Balance(ctx, "owner-1")
	if err != nil {
		t.Fatalf("Balance: %v", err)
	}
	if balance.EarnedMicro != outcome.NetMicro {
		t.Fatalf("earned = %d, want %d (the settled net)", balance.EarnedMicro, outcome.NetMicro)
	}
	share, err := repo.LoadShare(ctx, "share-1", "")
	if err != nil {
		t.Fatalf("LoadShare: %v", err)
	}
	if share.TotalEarnedMicro != outcome.NetMicro {
		t.Fatalf("share earned = %d, want %d", share.TotalEarnedMicro, outcome.NetMicro)
	}
	models, err := repo.ListModels(ctx, "share-1")
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 1 || models[0].EarnedMicro != outcome.NetMicro {
		t.Fatalf("model earned = %+v, want %d", models, outcome.NetMicro)
	}
	usage, err := repo.ListUsage(ctx, "owner-1", 10, "share-1")
	if err != nil {
		t.Fatalf("ListUsage: %v", err)
	}
	if len(usage) != 1 || usage[0].ConsumerHubID != "hub-9" || usage[0].ConsumerTenantID != "tenant-9" {
		t.Fatalf("usage consumer = %+v, want hub-9/tenant-9", usage)
	}
}

func TestTokenBankProxySettlerModelNameIsCaseInsensitive(t *testing.T) {
	// A member id encodes the model exactly, but an operator can rename a model
	// through the admin API with different casing and still hit the same route.
	// Matching case-insensitively means a settlement is not silently dropped by
	// a capitalisation difference.
	repo, st := newSettlerTestRepo(t, "tbk-settler-case.db")
	createShare(t, repo, "share-1", "owner-1", "s", "Meta-Llama/Llama-3.3-70B", "mid", 1.0)

	settler := newTokenBankProxySettler(repo, st.System)
	view, found, err := settler.TokenBankShareForPublish(context.Background(), "share-1", "meta-llama/llama-3.3-70b")
	if err != nil {
		t.Fatalf("TokenBankShareForPublish: %v", err)
	}
	if !found {
		t.Fatalf("found = false, want true (case-insensitive match)")
	}
	// The canonical name from the share is returned, not the caller's spelling.
	if view.Model != "Meta-Llama/Llama-3.3-70B" {
		t.Fatalf("Model = %q, want the share's canonical spelling", view.Model)
	}
}

func TestTokenBankProxySettlerUnknownShareOrModelNotFounded(t *testing.T) {
	repo, st := newSettlerTestRepo(t, "tbk-settler-missing.db")
	createShare(t, repo, "share-1", "owner-1", "s", "llama", "mid", 1.0)
	settler := newTokenBankProxySettler(repo, st.System)
	ctx := context.Background()

	if _, found, err := settler.TokenBankShareForPublish(ctx, "share-nope", "llama"); err != nil || found {
		t.Fatalf("unknown share: found=%v err=%v, want false/nil", found, err)
	}
	if _, found, err := settler.TokenBankShareForPublish(ctx, "share-1", "qwen"); err != nil || found {
		t.Fatalf("retired model: found=%v err=%v, want false/nil", found, err)
	}
	if _, found, err := settler.TokenBankShareForPublish(ctx, "", "llama"); err != nil || found {
		t.Fatalf("empty share: found=%v err=%v, want false/nil", found, err)
	}
}

func TestTokenBankProxySettlerUsesPriceBookUnits(t *testing.T) {
	// §5: the owner is paid from the platform price book, not from whatever the
	// consumer's service group charged. The adapter is where that resolution
	// happens, so this test pins that a matching rule overrides the defaults.
	repo, st := newSettlerTestRepo(t, "tbk-settler-book.db")
	ctx := context.Background()
	createShare(t, repo, "share-1", "owner-1", "s", "llama-3.3-70b", "mid", 1.0)

	if err := st.System.Set(ctx, sqlite.TokenBankSettingsKey, mustJSON(t, sqlite.DefaultTokenBankSettings())); err != nil {
		t.Fatalf("Set settings: %v", err)
	}
	if _, err := repo.UpsertPriceRule(ctx, sqlite.TokenBankPriceRule{
		ID: "rule-llama", ModelPattern: "llama-3.3-*",
		UnitInputPer10K: 7, UnitOutputPer10K: 9,
	}, time.Now().UTC()); err != nil {
		t.Fatalf("UpsertPriceRule: %v", err)
	}

	settler := newTokenBankProxySettler(repo, st.System)
	view, found, err := settler.TokenBankShareForPublish(ctx, "share-1", "llama-3.3-70b")
	if err != nil || !found {
		t.Fatalf("TokenBankShareForPublish: found=%v err=%v", found, err)
	}
	if view.UnitInputPer10K != 7 || view.UnitOutputPer10K != 9 {
		t.Fatalf("units = %v/%v, want 7/9 from the price book", view.UnitInputPer10K, view.UnitOutputPer10K)
	}
	// The rule left cache blank, which is stored as 0. That must not wipe the
	// platform cache defaults, or cached tokens are billed at the input rate.
	defaults := sqlite.DefaultTokenBankSettings()
	if view.UnitCachedReadPer10K != defaults.DefaultUnitCachedReadPer10K ||
		view.UnitCacheWritePer10K != defaults.DefaultUnitCacheWritePer10K {
		t.Fatalf("cache units = %v/%v, want defaults %v/%v",
			view.UnitCachedReadPer10K, view.UnitCacheWritePer10K,
			defaults.DefaultUnitCachedReadPer10K, defaults.DefaultUnitCacheWritePer10K)
	}
	if view.PriceBookID != "rule-llama" {
		t.Fatalf("PriceBookID = %q, want rule-llama", view.PriceBookID)
	}

	if _, err := repo.UpsertPriceRule(ctx, sqlite.TokenBankPriceRule{
		ID: "rule-llama", ModelPattern: "llama-3.3-*",
		UnitInputPer10K: 7, UnitOutputPer10K: 9,
		UnitCachedReadPer10K: 1.25, UnitCacheWritePer10K: 2.5,
	}, time.Now().UTC()); err != nil {
		t.Fatalf("UpsertPriceRule cache: %v", err)
	}
	view, found, err = settler.TokenBankShareForPublish(ctx, "share-1", "llama-3.3-70b")
	if err != nil || !found {
		t.Fatalf("TokenBankShareForPublish with cache rates: found=%v err=%v", found, err)
	}
	if view.UnitCachedReadPer10K != 1.25 || view.UnitCacheWritePer10K != 2.5 {
		t.Fatalf("cache units = %v/%v, want 1.25/2.5 from the price book", view.UnitCachedReadPer10K, view.UnitCacheWritePer10K)
	}
	fallback, ok, err := settler.(*tokenBankProxySettler).TokenBankFallbackPrice(ctx, "llama-3.3-70b")
	if err != nil || !ok {
		t.Fatalf("TokenBankFallbackPrice: ok=%v err=%v", ok, err)
	}
	if fallback.UnitInputPer10K != 7 || fallback.UnitCachedReadPer10K != 1.25 || fallback.UnitCacheWritePer10K != 2.5 {
		t.Fatalf("fallback units = %v/%v/%v, want 7/1.25/2.5",
			fallback.UnitInputPer10K, fallback.UnitCachedReadPer10K, fallback.UnitCacheWritePer10K)
	}
}

func TestTokenBankProxySettlerFallsBackToDefaultUnits(t *testing.T) {
	// With no matching rule the platform defaults apply, so a share is never
	// settled at a zero rate merely because the book has no entry yet. A zero
	// rate would look like "this model earns nothing", which is not the intent.
	repo, st := newSettlerTestRepo(t, "tbk-settler-nobook.db")
	ctx := context.Background()
	createShare(t, repo, "share-1", "owner-1", "s", "unlisted-model", "mid", 1.0)

	defaults := sqlite.DefaultTokenBankSettings()
	defaults.DefaultUnitInputPer10K = 3.5
	defaults.DefaultUnitOutputPer10K = 4.5
	if err := st.System.Set(ctx, sqlite.TokenBankSettingsKey, mustJSON(t, defaults)); err != nil {
		t.Fatalf("Set settings: %v", err)
	}

	settler := newTokenBankProxySettler(repo, st.System)
	view, found, err := settler.TokenBankShareForPublish(ctx, "share-1", "unlisted-model")
	if err != nil || !found {
		t.Fatalf("TokenBankShareForPublish: found=%v err=%v", found, err)
	}
	if view.UnitInputPer10K != 3.5 || view.UnitOutputPer10K != 4.5 {
		t.Fatalf("units = %v/%v, want the settings defaults 3.5/4.5", view.UnitInputPer10K, view.UnitOutputPer10K)
	}
	if view.PriceBookID != "" {
		t.Fatalf("PriceBookID = %q, want empty for an unlisted model", view.PriceBookID)
	}
}

func TestNewTokenBankProxySettlerNilRepoIsNil(t *testing.T) {
	// bootstrap installs the result unconditionally, so a nil repository must
	// yield a nil settler (a no-op hook) rather than a panic-on-call value.
	if got := newTokenBankProxySettler(nil, nil); got != nil {
		t.Fatalf("newTokenBankProxySettler(nil, nil) = %v, want nil", got)
	}
}

func TestTokenBankProxySettlerUsesConfiguredFeeRate(t *testing.T) {
	repo, st := newSettlerTestRepo(t, "tbk-settler-fee.db")
	ctx := context.Background()
	createShare(t, repo, "share-1", "owner-1", "s", "llama", "mid", 1.0)

	// The admin saved a non-default fee. Settlement must read the same blob the
	// admin tab shows, or the owner sees one rate and is paid at another.
	settings := sqlite.DefaultTokenBankSettings()
	settings.FeeRate = 0.25
	if err := st.System.Set(ctx, sqlite.TokenBankSettingsKey, mustJSON(t, settings)); err != nil {
		t.Fatalf("Set settings: %v", err)
	}

	settler := newTokenBankProxySettler(repo, st.System)
	view, found, err := settler.TokenBankShareForPublish(ctx, "share-1", "llama")
	if err != nil || !found {
		t.Fatalf("TokenBankShareForPublish: found=%v err=%v", found, err)
	}
	if view.FeeRate != 0.25 {
		t.Fatalf("FeeRate = %v, want 0.25 from settings", view.FeeRate)
	}
}

func TestTokenBankProxySettlerBadFeeBlobFallsBackToDefault(t *testing.T) {
	repo, st := newSettlerTestRepo(t, "tbk-settler-badfee.db")
	ctx := context.Background()
	createShare(t, repo, "share-1", "owner-1", "s", "llama", "mid", 1.0)

	if err := st.System.Set(ctx, sqlite.TokenBankSettingsKey, "{ not json"); err != nil {
		t.Fatalf("Set settings: %v", err)
	}

	settler := newTokenBankProxySettler(repo, st.System)
	view, found, err := settler.TokenBankShareForPublish(ctx, "share-1", "llama")
	if err != nil || !found {
		t.Fatalf("TokenBankShareForPublish: found=%v err=%v", found, err)
	}
	// A corrupt blob must not strand settlement: the documented default applies.
	if view.FeeRate != sqlite.DefaultTokenBankSettings().FeeRate {
		t.Fatalf("FeeRate = %v, want the default %v", view.FeeRate, sqlite.DefaultTokenBankSettings().FeeRate)
	}
}

func TestPauseTokenBankShareRecordsTheCap(t *testing.T) {
	repo, st := newSettlerTestRepo(t, "tbk-settler-pause.db")
	ctx := context.Background()
	createShare(t, repo, "share-1", "owner-1", "s", "llama", "mid", 1.0)
	raw := newTokenBankProxySettler(repo, st.System)
	settler, ok := raw.(*tokenBankProxySettler)
	if !ok {
		t.Fatalf("settler type = %T", raw)
	}
	if err := settler.PauseTokenBankShare(ctx, "share-1", "daily"); err != nil {
		t.Fatalf("PauseTokenBankShare: %v", err)
	}
	share, err := repo.LoadShare(ctx, "share-1", "owner-1")
	if err != nil {
		t.Fatal(err)
	}
	if share.Status != sqlite.TokenBankShareStatusPaused {
		t.Fatalf("status = %q, want paused", share.Status)
	}
	if !strings.Contains(share.PausedReason, "token cap: daily") || !strings.Contains(share.LastError, "token cap: daily") {
		t.Fatalf("paused_reason/last_error = %q/%q", share.PausedReason, share.LastError)
	}
}

func TestPauseTokenBankShareIgnoresUsageSpike(t *testing.T) {
	repo, st := newSettlerTestRepo(t, "tbk-settler-spike.db")
	ctx := context.Background()
	createShare(t, repo, "share-1", "owner-1", "s", "llama", "mid", 1.0)
	raw := newTokenBankProxySettler(repo, st.System)
	settler, ok := raw.(*tokenBankProxySettler)
	if !ok {
		t.Fatalf("settler type = %T", raw)
	}
	if err := settler.PauseTokenBankShare(ctx, "share-1", "anomaly"); err != nil {
		t.Fatalf("PauseTokenBankShare: %v", err)
	}
	share, err := repo.LoadShare(ctx, "share-1", "owner-1")
	if err != nil {
		t.Fatal(err)
	}
	if share.Status == sqlite.TokenBankShareStatusPaused || share.PausedReason != "" {
		t.Fatalf("status/reason = %q/%q, want the share left unpaused", share.Status, share.PausedReason)
	}
}

func TestResumeSharesPausedForUsageSpikeLeavesOtherPauses(t *testing.T) {
	repo, _ := newSettlerTestRepo(t, "tbk-settler-resume-spike.db")
	ctx := context.Background()
	now := time.Now().UTC()
	for _, id := range []string{"spike", "daily", "manual"} {
		createShare(t, repo, id, "owner-1", id, "llama", "mid", 1.0)
	}
	if err := repo.SetSharePaused(ctx, "spike", "", "token cap: anomaly", true, now); err != nil {
		t.Fatal(err)
	}
	if err := repo.NoteShareModelError(ctx, "spike", "glm-5", "token cap: anomaly"); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetSharePaused(ctx, "daily", "", "token cap: daily", true, now); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetSharePaused(ctx, "manual", "", "manual", true, now); err != nil {
		t.Fatal(err)
	}

	resumeSharesPausedForUsageSpike(ctx, repo, nil)

	spike, err := repo.LoadShare(ctx, "spike", "owner-1")
	if err != nil {
		t.Fatal(err)
	}
	if spike.Status != sqlite.TokenBankShareStatusActive || spike.PausedReason != "token cap: anomaly" || spike.LastError != "" {
		t.Fatalf("spike = status %q reason %q last_error %q, want active, spike note kept, last_error clear", spike.Status, spike.PausedReason, spike.LastError)
	}
	for _, id := range []string{"daily", "manual"} {
		share, err := repo.LoadShare(ctx, id, "owner-1")
		if err != nil {
			t.Fatal(err)
		}
		if share.Status != sqlite.TokenBankShareStatusPaused {
			t.Fatalf("%s status = %q, want paused", id, share.Status)
		}
	}

	resumeSharesPausedForUsageSpike(ctx, repo, nil)
	again, err := repo.LoadShare(ctx, "spike", "owner-1")
	if err != nil {
		t.Fatal(err)
	}
	if again.Status != sqlite.TokenBankShareStatusActive {
		t.Fatalf("second resume status = %q, want active", again.Status)
	}
}

func TestResumeUsageSpikePauseResumesRegistryMembers(t *testing.T) {
	repo, st := newSettlerTestRepo(t, "tbk-settler-resume-members.db")
	ctx := context.Background()
	now := time.Now().UTC()
	createShare(t, repo, "spike", "owner-1", "spike", "llama", "mid", 1.0)
	createShare(t, repo, "daily", "owner-1", "daily", "llama", "mid", 1.0)
	if err := repo.SetSharePaused(ctx, "spike", "", "token cap: anomaly", true, now); err != nil {
		t.Fatal(err)
	}
	if err := repo.NoteShareModelError(ctx, "spike", "llama", "token cap: anomaly"); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetSharePaused(ctx, "daily", "", "token cap: daily", true, now); err != nil {
		t.Fatal(err)
	}
	svc := llmservice.NewService(st.System)
	if err := svc.EnsureTokenBankArrays(ctx); err != nil {
		t.Fatal(err)
	}
	spikeID := llmservice.TokenBankMemberID("spike", "llama")
	dailyID := llmservice.TokenBankMemberID("daily", "llama")
	specs := []llmservice.TokenBankPublishSpec{
		{
			ShareID:     "spike",
			OwnerUserID: "owner-1",
			DisplayName: "spike",
			Model:       "llama",
			ArrayID:     llmservice.TokenBankArrayForTier("mid"),
			APIURL:      "https://upstream.example/v1",
			APIKey:      "sk-test",
			Protocol:    "openai",
			SharePaused: true,
		},
		{
			ShareID:     "daily",
			OwnerUserID: "owner-1",
			DisplayName: "daily",
			Model:       "llama",
			ArrayID:     llmservice.TokenBankArrayForTier("mid"),
			APIURL:      "https://upstream.example/v1",
			APIKey:      "sk-daily",
			Protocol:    "openai",
			SharePaused: true,
		},
	}
	if _, err := svc.PublishTokenBankShare(ctx, specs); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{spikeID, dailyID} {
		paused, err := svc.GetProvider(ctx, id)
		if err != nil || paused == nil || !paused.Paused {
			t.Fatalf("published member %s = %+v err=%v, want paused", id, paused, err)
		}
	}

	resumeSharesPausedForUsageSpike(ctx, repo, svc)

	share, err := repo.LoadShare(ctx, "spike", "owner-1")
	if err != nil {
		t.Fatal(err)
	}
	if share.Status != sqlite.TokenBankShareStatusActive || share.PausedReason != "token cap: anomaly" || share.LastError != "" {
		t.Fatalf("share = status %q reason %q last_error %q, want active, spike note kept, last_error clear", share.Status, share.PausedReason, share.LastError)
	}
	live, err := svc.GetProvider(ctx, spikeID)
	if err != nil || live == nil || live.Paused {
		t.Fatalf("spike member after resume = %+v err=%v, want unpaused", live, err)
	}
	if _, err := svc.SetTokenBankSharePaused(ctx, "spike", true); err != nil {
		t.Fatal(err)
	}
	resumeSharesPausedForUsageSpike(ctx, repo, svc)
	again, err := svc.GetProvider(ctx, spikeID)
	if err != nil || again == nil || again.Paused {
		t.Fatalf("spike member after registry clobber = %+v err=%v, want unpaused", again, err)
	}
	share, err = repo.LoadShare(ctx, "spike", "owner-1")
	if err != nil {
		t.Fatal(err)
	}
	if share.Status != sqlite.TokenBankShareStatusActive || share.PausedReason != "token cap: anomaly" {
		t.Fatalf("share after clobber = status %q reason %q, want active with the spike note", share.Status, share.PausedReason)
	}
	dailyShare, err := repo.LoadShare(ctx, "daily", "owner-1")
	if err != nil {
		t.Fatal(err)
	}
	if dailyShare.Status != sqlite.TokenBankShareStatusPaused {
		t.Fatalf("daily status = %q, want paused", dailyShare.Status)
	}
	dailyMember, err := svc.GetProvider(ctx, dailyID)
	if err != nil || dailyMember == nil || !dailyMember.Paused {
		t.Fatalf("daily member after resume = %+v err=%v, want still paused", dailyMember, err)
	}
}
