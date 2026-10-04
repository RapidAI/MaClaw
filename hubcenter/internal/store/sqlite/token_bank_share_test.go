package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func newTokenBankShareRepo(t *testing.T) (*TokenBankRepo, *Provider) {
	t.Helper()
	provider := newTokenBankTestProvider(t, filepath.Join(t.TempDir(), "tbk-share.db"))
	repo := newTokenBankTestRepo(t, provider)
	return repo, provider
}

func shareFixture(id, owner, fingerprint string) TokenBankShare {
	return TokenBankShare{
		ID:             id,
		OwnerUserID:    owner,
		OwnerEmail:     owner + "@example.com",
		DisplayName:    "Share " + id,
		APIURL:         "https://api.example.com/v1",
		Protocol:       "openai",
		EncryptedKey:   "enc:" + fingerprint,
		KeyFingerprint: fingerprint,
	}
}

func modelFixture(name string) TokenBankShareModel {
	return TokenBankShareModel{
		ModelName: name,
		MemberID:  "member-" + name,
		Enabled:   true,
		Available: true,
	}
}

// --- CreateShare ----------------------------------------------------------

func TestTokenBankCreateShareWritesModelsInOneTransaction(t *testing.T) {
	repo, _ := newTokenBankShareRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()

	share, created, err := repo.CreateShare(ctx, shareFixture("share-1", "user-1", "fp-1"),
		[]TokenBankShareModel{modelFixture("gpt-4o"), modelFixture("gpt-4o-mini")}, 0, now)
	if err != nil {
		t.Fatalf("CreateShare() error = %v", err)
	}
	if !created {
		t.Fatalf("created = false, want true")
	}
	if share.Status != TokenBankShareStatusActive {
		t.Fatalf("status = %q, want %q", share.Status, TokenBankShareStatusActive)
	}
	if share.Visibility != "public" {
		t.Fatalf("visibility = %q, want public (defaulted)", share.Visibility)
	}
	if share.Protocol != "openai" {
		t.Fatalf("protocol = %q, want openai (defaulted)", share.Protocol)
	}

	models, err := repo.ListModels(ctx, "share-1")
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("models = %d, want 2", len(models))
	}
	// ListModels orders by name; assert that too so the GUI does not reshuffle.
	if models[0].ModelName != "gpt-4o" || models[1].ModelName != "gpt-4o-mini" {
		t.Fatalf("models order = [%s %s], want [gpt-4o gpt-4o-mini]",
			models[0].ModelName, models[1].ModelName)
	}
	for _, m := range models {
		// The upsert must have filled the defaults the settlement path relies on.
		if m.Tier != "mid" || m.TierMultiplier != 1 {
			t.Fatalf("model %s tier=%q multiplier=%v, want mid/1", m.ModelName, m.Tier, m.TierMultiplier)
		}
		if m.ArrayID != "token_bank_mid" {
			t.Fatalf("model %s array_id=%q, want token_bank_mid", m.ModelName, m.ArrayID)
		}
		if !m.Enabled || !m.Available {
			t.Fatalf("model %s enabled=%v available=%v, want both true", m.ModelName, m.Enabled, m.Available)
		}
	}
}

func TestTokenBankShareWindowRoundTripsAndCanBeReplaced(t *testing.T) {
	repo, _ := newTokenBankShareRepo(t)
	ctx := context.Background()
	cheap := modelFixture("gpt-4o")
	cheap.ShareWindowJSON = `{"days":[1,2,3,4,5],"start":"22:00","end":"08:00"}`
	always := modelFixture("gpt-4o-mini")
	if _, _, err := repo.CreateShare(ctx, shareFixture("share-1", "user-1", "fp-1"),
		[]TokenBankShareModel{cheap, always}, 0, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	models, err := repo.ListModels(ctx, "share-1")
	if err != nil {
		t.Fatal(err)
	}
	if models[0].ShareWindowJSON != cheap.ShareWindowJSON {
		t.Fatalf("cheap window = %q", models[0].ShareWindowJSON)
	}
	if models[1].ShareWindowJSON != "" {
		t.Fatalf("missing window = %q, want always", models[1].ShareWindowJSON)
	}
	cheap.ShareWindowJSON = ""
	if _, err := repo.SyncShareModels(ctx, "share-1", "user-1", []TokenBankShareModel{cheap, always}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	models, err = repo.ListModels(ctx, "share-1")
	if err != nil {
		t.Fatal(err)
	}
	if models[0].ShareWindowJSON != "" {
		t.Fatalf("cleared window = %q", models[0].ShareWindowJSON)
	}
}

func TestTokenBankCreateShareIsIdempotentOnOwnerAndFingerprint(t *testing.T) {
	repo, _ := newTokenBankShareRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()

	first, created, err := repo.CreateShare(ctx, shareFixture("share-1", "user-1", "fp-1"),
		[]TokenBankShareModel{modelFixture("gpt-4o")}, 0, now)
	if err != nil || !created {
		t.Fatalf("first CreateShare() created=%v error=%v, want created", created, err)
	}

	// A double-click, or a retry after a dropped response: the client submits a
	// *different* id this time (the id is generated per request), and the store
	// must still hand back the original row rather than create a second share.
	second, created, err := repo.CreateShare(ctx, shareFixture("share-2", "user-1", "fp-1"),
		[]TokenBankShareModel{modelFixture("gpt-4o")}, 0, now)
	if err != nil {
		t.Fatalf("second CreateShare() error = %v", err)
	}
	if created {
		t.Fatalf("second CreateShare() created = true, want false (idempotent on owner+fingerprint)")
	}
	if second.ID != first.ID {
		t.Fatalf("second CreateShare() returned id %q, want the original %q", second.ID, first.ID)
	}

	shares, err := repo.ListShares(ctx, "user-1", "", 0, 0)
	if err != nil {
		t.Fatalf("ListShares() error = %v", err)
	}
	if len(shares) != 1 {
		t.Fatalf("shares = %d, want 1 after idempotent re-submit", len(shares))
	}
}

func TestTokenBankCreateShareDistinctFingerprintCreatesSecondShare(t *testing.T) {
	repo, _ := newTokenBankShareRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if _, _, err := repo.CreateShare(ctx, shareFixture("share-1", "user-1", "fp-1"), nil, 0, now); err != nil {
		t.Fatalf("CreateShare(fp-1) error = %v", err)
	}
	if _, created, err := repo.CreateShare(ctx, shareFixture("share-2", "user-1", "fp-2"), nil, 0, now); err != nil || !created {
		t.Fatalf("CreateShare(fp-2) created=%v error=%v, want created", created, err)
	}
	shares, err := repo.ListShares(ctx, "user-1", "", 0, 0)
	if err != nil {
		t.Fatalf("ListShares() error = %v", err)
	}
	if len(shares) != 2 {
		t.Fatalf("shares = %d, want 2 different keys", len(shares))
	}
}

func TestTokenBankCreateShareEnforcesPerUserCap(t *testing.T) {
	repo, _ := newTokenBankShareRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()

	for i := 0; i < 3; i++ {
		id := "share-" + string(rune('a'+i))
		if _, _, err := repo.CreateShare(ctx, shareFixture(id, "user-1", "fp-"+id), nil, 3, now); err != nil {
			t.Fatalf("CreateShare(%s) error = %v", id, err)
		}
	}
	// The idempotency check runs before the cap, so re-submitting an existing
	// key at the cap must still succeed...
	if _, created, err := repo.CreateShare(ctx, shareFixture("share-a2", "user-1", "fp-share-a"), nil, 3, now); err != nil || created {
		t.Fatalf("re-submit at cap created=%v error=%v, want idempotent hit", created, err)
	}
	// ...but a genuinely new key must be refused.
	_, _, err := repo.CreateShare(ctx, shareFixture("share-d", "user-1", "fp-share-d"), nil, 3, now)
	if !errors.Is(err, ErrTokenBankShareLimitReached) {
		t.Fatalf("CreateShare at cap error = %v, want ErrTokenBankShareLimitReached", err)
	}
}

func TestTokenBankTakeOutShareFreesASlotUnderTheCap(t *testing.T) {
	repo, _ := newTokenBankShareRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if _, _, err := repo.CreateShare(ctx, shareFixture("share-1", "user-1", "fp-1"), nil, 1, now); err != nil {
		t.Fatalf("CreateShare(share-1) error = %v", err)
	}
	if _, _, err := repo.CreateShare(ctx, shareFixture("share-2", "user-1", "fp-2"), nil, 1, now); !errors.Is(err, ErrTokenBankShareLimitReached) {
		t.Fatalf("CreateShare at cap error = %v, want ErrTokenBankShareLimitReached", err)
	}
	if ok, err := repo.TakeOutShare(ctx, "share-1", "user-1"); err != nil || !ok {
		t.Fatalf("TakeOutShare(share-1) ok=%v error=%v, want ok", ok, err)
	}
	// The cap counts only live shares: withdrawing a provider returns the slot.
	if _, created, err := repo.CreateShare(ctx, shareFixture("share-2", "user-1", "fp-2"), nil, 1, now); err != nil || !created {
		t.Fatalf("CreateShare after takeout created=%v error=%v, want created", created, err)
	}
}

func TestTokenBankCreateShareRequiresMandatoryFields(t *testing.T) {
	repo, _ := newTokenBankShareRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()

	cases := []struct {
		name   string
		mutate func(*TokenBankShare)
	}{
		{"missing id", func(s *TokenBankShare) { s.ID = "" }},
		{"missing owner", func(s *TokenBankShare) { s.OwnerUserID = "" }},
		{"missing fingerprint", func(s *TokenBankShare) { s.KeyFingerprint = "" }},
		{"missing encrypted key", func(s *TokenBankShare) { s.EncryptedKey = "" }},
		{"missing display name", func(s *TokenBankShare) { s.DisplayName = "" }},
		{"missing api url", func(s *TokenBankShare) { s.APIURL = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			share := shareFixture("share-x", "user-1", "fp-x")
			tc.mutate(&share)
			if _, _, err := repo.CreateShare(ctx, share, nil, 0, now); err == nil {
				t.Fatalf("CreateShare(%s) error = nil, want validation error", tc.name)
			}
		})
	}
}

func TestTokenBankCreateShareRollsBackWhenAModelIsInvalid(t *testing.T) {
	repo, _ := newTokenBankShareRepo(t)
	ctx := context.Background()

	// The second model has no name; the transaction must roll back entirely so
	// the caller never sees a half-created share.
	_, _, err := repo.CreateShare(ctx, shareFixture("share-1", "user-1", "fp-1"),
		[]TokenBankShareModel{modelFixture("gpt-4o"), {MemberID: "no-name"}}, 0, time.Now().UTC())
	if err == nil {
		t.Fatalf("CreateShare(invalid model) error = nil, want error")
	}
	if _, err := repo.LoadShare(ctx, "share-1", ""); !errors.Is(err, ErrTokenBankShareNotFound) {
		t.Fatalf("LoadShare after rollback error = %v, want ErrTokenBankShareNotFound", err)
	}
}

// --- ListShares / ListShareOwners ----------------------------------------

func TestTokenBankListSharesFiltersByOwnerAndStatus(t *testing.T) {
	repo, _ := newTokenBankShareRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if _, _, err := repo.CreateShare(ctx, shareFixture("share-1", "user-1", "fp-1"), nil, 0, now); err != nil {
		t.Fatalf("CreateShare(share-1) error = %v", err)
	}
	if _, _, err := repo.CreateShare(ctx, shareFixture("share-2", "user-2", "fp-2"), nil, 0, now); err != nil {
		t.Fatalf("CreateShare(share-2) error = %v", err)
	}
	if err := repo.SetSharePaused(ctx, "share-2", "", "probing failures", true, now); err != nil {
		t.Fatalf("SetSharePaused() error = %v", err)
	}

	all, err := repo.ListShares(ctx, "", "", 0, 0)
	if err != nil {
		t.Fatalf("ListShares(all) error = %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ListShares(all) = %d, want 2", len(all))
	}
	byOwner, err := repo.ListShares(ctx, "user-1", "", 0, 0)
	if err != nil {
		t.Fatalf("ListShares(user-1) error = %v", err)
	}
	if len(byOwner) != 1 || byOwner[0].ID != "share-1" {
		t.Fatalf("ListShares(user-1) = %+v, want only share-1", byOwner)
	}
	paused, err := repo.ListShares(ctx, "", TokenBankShareStatusPaused, 0, 0)
	if err != nil {
		t.Fatalf("ListShares(paused) error = %v", err)
	}
	if len(paused) != 1 || paused[0].ID != "share-2" {
		t.Fatalf("ListShares(paused) = %+v, want only share-2", paused)
	}
}

func TestTokenBankListSharesHonoursLimitAndOffset(t *testing.T) {
	repo, _ := newTokenBankShareRepo(t)
	ctx := context.Background()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Distinct created_at values so the ORDER BY created_at DESC, id ASC
	// ordering is deterministic and the page boundaries are predictable.
	for i := 0; i < 5; i++ {
		id := "share-" + string(rune('a'+i))
		if _, _, err := repo.CreateShare(ctx, shareFixture(id, "user-1", "fp-"+id), nil, 0,
			base.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("CreateShare(%s) error = %v", id, err)
		}
	}
	page1, err := repo.ListShares(ctx, "user-1", "", 2, 0)
	if err != nil {
		t.Fatalf("ListShares(page1) error = %v", err)
	}
	page2, err := repo.ListShares(ctx, "user-1", "", 2, 2)
	if err != nil {
		t.Fatalf("ListShares(page2) error = %v", err)
	}
	if len(page1) != 2 || len(page2) != 2 {
		t.Fatalf("page sizes = %d/%d, want 2/2", len(page1), len(page2))
	}
	if page1[0].ID == page2[0].ID {
		t.Fatalf("page1[0]=%q equals page2[0]=%q, offset ignored", page1[0].ID, page2[0].ID)
	}
	// Newest first: the last created share leads the first page.
	if page1[0].ID != "share-e" {
		t.Fatalf("page1[0] = %q, want share-e (newest first)", page1[0].ID)
	}
}

func TestTokenBankListShareOwnersAggregatesWithoutNPlusOne(t *testing.T) {
	repo, provider := newTokenBankShareRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if _, _, err := repo.CreateShare(ctx, shareFixture("share-1", "user-1", "fp-1"),
		[]TokenBankShareModel{modelFixture("gpt-4o"), modelFixture("gpt-4o-mini")}, 0, now); err != nil {
		t.Fatalf("CreateShare(share-1) error = %v", err)
	}
	if _, _, err := repo.CreateShare(ctx, shareFixture("share-2", "user-1", "fp-2"),
		[]TokenBankShareModel{modelFixture("claude-sonnet")}, 0, now); err != nil {
		t.Fatalf("CreateShare(share-2) error = %v", err)
	}
	if _, _, err := repo.CreateShare(ctx, shareFixture("share-3", "user-2", "fp-3"),
		[]TokenBankShareModel{modelFixture("gemini-pro")}, 0, now); err != nil {
		t.Fatalf("CreateShare(share-3) error = %v", err)
	}
	// Token counters stay on the model row. Earned is the account cache: a
	// planted model.earned_micro must not move the grid, because that column
	// is not what settlement has always written and takeout deletes the row.
	if _, err := provider.Write.ExecContext(ctx,
		`UPDATE token_bank_models SET used_input_tokens = ?, used_output_tokens = ?, earned_micro = ?
		  WHERE share_id = ? AND model_name = ?`, 100, 50, 1, "share-1", "gpt-4o"); err != nil {
		t.Fatalf("seed usage: %v", err)
	}
	if _, err := repo.AppendLedger(ctx, TokenBankLedgerEntry{
		ID: "earn:grid", UserID: "user-1", Bucket: TokenBankBucketEarned,
		AmountMicro: 7_000_000, BizKey: "earn:grid", CreatedAt: now,
	}); err != nil {
		t.Fatalf("seed earned ledger: %v", err)
	}

	owners, err := repo.ListShareOwners(ctx, 0, 0)
	if err != nil {
		t.Fatalf("ListShareOwners() error = %v", err)
	}
	if len(owners) != 2 {
		t.Fatalf("owners = %d, want 2 (one row per owner)", len(owners))
	}
	// Ordered by earned_micro DESC, so user-1 (7 credits) leads.
	if owners[0].OwnerUserID != "user-1" {
		t.Fatalf("owners[0] = %q, want user-1 (highest earner first)", owners[0].OwnerUserID)
	}
	if owners[0].ShareCount != 2 {
		t.Fatalf("user-1 share_count = %d, want 2", owners[0].ShareCount)
	}
	if owners[0].ModelCount != 3 {
		t.Fatalf("user-1 model_count = %d, want 3", owners[0].ModelCount)
	}
	if owners[0].UsedTokens != 150 {
		t.Fatalf("user-1 used_tokens = %d, want 150", owners[0].UsedTokens)
	}
	if owners[0].EarnedMicro != 7_000_000 {
		t.Fatalf("user-1 earned_micro = %d, want 7000000", owners[0].EarnedMicro)
	}
}

// --- Overview -------------------------------------------------------------

func TestTokenBankOverviewCountsSharesModelsAndCredits(t *testing.T) {
	repo, provider := newTokenBankShareRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if _, _, err := repo.CreateShare(ctx, shareFixture("share-1", "user-1", "fp-1"),
		[]TokenBankShareModel{modelFixture("gpt-4o"), modelFixture("gpt-4o-mini")}, 0, now); err != nil {
		t.Fatalf("CreateShare(share-1) error = %v", err)
	}
	if _, _, err := repo.CreateShare(ctx, shareFixture("share-2", "user-2", "fp-2"),
		[]TokenBankShareModel{modelFixture("claude-sonnet")}, 0, now); err != nil {
		t.Fatalf("CreateShare(share-2) error = %v", err)
	}
	// Withdrawn shares are excluded from the provider counts.
	revoked := shareFixture("share-3", "user-3", "fp-3")
	revoked.Status = TokenBankShareStatusRevoked
	if _, _, err := repo.CreateShare(ctx, revoked, []TokenBankShareModel{modelFixture("gemini-pro")}, 0, now); err != nil {
		t.Fatalf("CreateShare(share-3) error = %v", err)
	}

	// A planted model counter must not become the platform earned total.
	// Earned is the account cache, same as granted, withdrawn, and frozen.
	if _, err := provider.Write.ExecContext(ctx,
		`UPDATE token_bank_models SET earned_micro = 99 WHERE share_id = ?`, "share-1"); err != nil {
		t.Fatalf("seed model counter: %v", err)
	}

	// Credit totals come from the account cache, which requires ledger rows.
	for _, pair := range [][2]any{
		{"grant:g1", 3_000_000}, {"withdraw:w1", 1_000_000}, {"freeze:f1", 500_000},
	} {
		id := pair[0].(string)
		var bucket string
		switch {
		case strings.HasPrefix(id, "grant:"):
			bucket = TokenBankBucketGranted
		case strings.HasPrefix(id, "withdraw:"):
			bucket = TokenBankBucketWithdrawn
		default:
			bucket = TokenBankBucketFrozen
		}
		if _, err := repo.AppendLedger(ctx, TokenBankLedgerEntry{
			ID: id, UserID: "user-1", Bucket: bucket, AmountMicro: int64(pair[1].(int)),
			BizKey: id, CreatedAt: now,
		}); err != nil {
			t.Fatalf("AppendLedger(%s) error = %v", id, err)
		}
	}
	if _, err := repo.AppendLedger(ctx, TokenBankLedgerEntry{
		ID: "earn:e1", UserID: "user-1", Bucket: TokenBankBucketEarned,
		AmountMicro: 4_000_000, BizKey: "earn:e1", CreatedAt: now,
	}); err != nil {
		t.Fatalf("AppendLedger(earn) error = %v", err)
	}

	ov, err := repo.Overview(ctx)
	if err != nil {
		t.Fatalf("Overview() error = %v", err)
	}
	if ov.ShareOwners != 2 {
		t.Fatalf("ShareOwners = %d, want 2 (revoked owner excluded)", ov.ShareOwners)
	}
	if ov.ShareCount != 2 {
		t.Fatalf("ShareCount = %d, want 2 (revoked share excluded)", ov.ShareCount)
	}
	// The revoked share's model must not be counted either.
	if ov.ModelCount != 3 {
		t.Fatalf("ModelCount = %d, want 3 (revoked share's model excluded)", ov.ModelCount)
	}
	if ov.GrantedMicro != 3_000_000 || ov.WithdrawnMicro != 1_000_000 || ov.FrozenMicro != 500_000 {
		t.Fatalf("credit totals = granted %d / withdrawn %d / frozen %d, want 3000000/1000000/500000",
			ov.GrantedMicro, ov.WithdrawnMicro, ov.FrozenMicro)
	}
	if ov.EarnedMicro != 4_000_000 {
		t.Fatalf("EarnedMicro = %d, want 4000000 from the account cache", ov.EarnedMicro)
	}
}

// --- SetSharePaused / TakeOutShare / RenameShareKey -----------------------

func TestTokenBankSetSharePausedScopesToOwner(t *testing.T) {
	repo, _ := newTokenBankShareRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if _, _, err := repo.CreateShare(ctx, shareFixture("share-1", "user-1", "fp-1"), nil, 0, now); err != nil {
		t.Fatalf("CreateShare() error = %v", err)
	}
	// A different owner pausing someone else's share must look like not-found,
	// not forbidden: otherwise the endpoint enumerates share ids.
	if err := repo.SetSharePaused(ctx, "share-1", "intruder", "nope", true, now); !errors.Is(err, ErrTokenBankShareNotFound) {
		t.Fatalf("cross-owner pause error = %v, want ErrTokenBankShareNotFound", err)
	}
	if err := repo.SetSharePaused(ctx, "share-1", "user-1", "manual", true, now); err != nil {
		t.Fatalf("owner pause error = %v", err)
	}
	share, err := repo.LoadShare(ctx, "share-1", "")
	if err != nil {
		t.Fatalf("LoadShare() error = %v", err)
	}
	if share.Status != TokenBankShareStatusPaused {
		t.Fatalf("status = %q, want paused", share.Status)
	}
	if share.PausedReason != "manual" {
		t.Fatalf("paused_reason = %q, want manual", share.PausedReason)
	}
	// Unpausing clears the reason.
	if err := repo.SetSharePaused(ctx, "share-1", "", "", false, now); err != nil {
		t.Fatalf("admin unpause error = %v", err)
	}
	share, err = repo.LoadShare(ctx, "share-1", "")
	if err != nil {
		t.Fatalf("LoadShare() error = %v", err)
	}
	if share.Status != TokenBankShareStatusActive || share.PausedReason != "" {
		t.Fatalf("after unpause status=%q reason=%q, want active/empty", share.Status, share.PausedReason)
	}
}

func TestTokenBankUnpauseClearsCapLastError(t *testing.T) {
	repo, _ := newTokenBankShareRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if _, _, err := repo.CreateShare(ctx, shareFixture("share-1", "user-1", "fp-1"), nil, 0, now); err != nil {
		t.Fatalf("CreateShare() error = %v", err)
	}

	pause := func(reason string) {
		t.Helper()
		if err := repo.SetSharePaused(ctx, "share-1", "user-1", reason, true, now); err != nil {
			t.Fatalf("pause %q: %v", reason, err)
		}
	}
	unpause := func() {
		t.Helper()
		if err := repo.SetSharePaused(ctx, "share-1", "user-1", "", false, now); err != nil {
			t.Fatalf("unpause: %v", err)
		}
	}
	load := func() *TokenBankShare {
		t.Helper()
		share, err := repo.LoadShare(ctx, "share-1", "")
		if err != nil {
			t.Fatalf("LoadShare() error = %v", err)
		}
		return share
	}

	// The settler stores the same sentence in paused_reason and last_error.
	pause("token cap: anomaly")
	if err := repo.NoteShareModelError(ctx, "share-1", "", "token cap: anomaly"); err != nil {
		t.Fatalf("note cap: %v", err)
	}
	if err := repo.SetSharePaused(ctx, "share-1", "intruder", "", false, now); !errors.Is(err, ErrTokenBankShareNotFound) {
		t.Fatalf("cross-owner unpause error = %v, want ErrTokenBankShareNotFound", err)
	}
	if share := load(); share.LastError != "token cap: anomaly" || share.Status != TokenBankShareStatusPaused {
		t.Fatalf("intruder unpause changed share: status=%q last_error=%q", share.Status, share.LastError)
	}
	unpause()
	if share := load(); share.Status != TokenBankShareStatusActive || share.PausedReason != "" || share.LastError != "" {
		t.Fatalf("cap unpause = status %q reason %q last_error %q, want active and empty",
			share.Status, share.PausedReason, share.LastError)
	}

	// A model-prefixed copy of the same note is the same pause.
	pause("token cap: anomaly")
	if err := repo.NoteShareModelError(ctx, "share-1", "glm-5", "token cap: anomaly"); err != nil {
		t.Fatalf("note prefixed cap: %v", err)
	}
	if share := load(); share.LastError != "glm-5: token cap: anomaly" {
		t.Fatalf("prefixed last_error = %q", share.LastError)
	}
	unpause()
	if share := load(); share.LastError != "" || share.PausedReason != "" {
		t.Fatalf("prefixed unpause left reason %q last_error %q", share.PausedReason, share.LastError)
	}

	// A cap note still clears when the stored pause reason is empty.
	if err := repo.NoteShareModelError(ctx, "share-1", "", "token cap: monthly"); err != nil {
		t.Fatalf("note monthly: %v", err)
	}
	pause("")
	unpause()
	if share := load(); share.LastError != "" {
		t.Fatalf("empty-reason unpause left last_error %q", share.LastError)
	}

	// Pausing leaves an unrelated upstream error alone, and so does unpause.
	if err := repo.NoteShareModelError(ctx, "share-1", "", "upstream timeout"); err != nil {
		t.Fatalf("note upstream: %v", err)
	}
	pause("token cap: daily")
	if share := load(); share.LastError != "upstream timeout" || share.PausedReason != "token cap: daily" {
		t.Fatalf("pause changed upstream error: reason %q last_error %q", share.PausedReason, share.LastError)
	}
	unpause()
	if share := load(); share.Status != TokenBankShareStatusActive || share.PausedReason != "" || share.LastError != "upstream timeout" {
		t.Fatalf("unpause of unrelated error = status %q reason %q last_error %q",
			share.Status, share.PausedReason, share.LastError)
	}

	// % and _ in a pause reason are literals. They must not wipe a different error.
	if err := repo.NoteShareModelError(ctx, "share-1", "glm-5", "1000"); err != nil {
		t.Fatalf("note 1000: %v", err)
	}
	pause("100%")
	unpause()
	if share := load(); share.LastError != "glm-5: 1000" {
		t.Fatalf("percent reason cleared last_error %q", share.LastError)
	}
	if err := repo.NoteShareModelError(ctx, "share-1", "glm-5", "100%"); err != nil {
		t.Fatalf("note 100%%: %v", err)
	}
	pause("100%")
	unpause()
	if share := load(); share.LastError != "" {
		t.Fatalf("matching percent reason left last_error %q", share.LastError)
	}
	if err := repo.NoteShareModelError(ctx, "share-1", "glm-5", "axb"); err != nil {
		t.Fatalf("note axb: %v", err)
	}
	pause("a_b")
	unpause()
	if share := load(); share.LastError != "glm-5: axb" {
		t.Fatalf("underscore reason cleared last_error %q", share.LastError)
	}
	if err := repo.NoteShareModelError(ctx, "share-1", "glm-5", "a_b"); err != nil {
		t.Fatalf("note a_b: %v", err)
	}
	pause("a_b")
	unpause()
	if share := load(); share.LastError != "" {
		t.Fatalf("matching underscore reason left last_error %q", share.LastError)
	}
}

func TestTokenBankSetSharePausedRejectsUnknownShare(t *testing.T) {
	repo, _ := newTokenBankShareRepo(t)
	if err := repo.SetSharePaused(context.Background(), "ghost", "", "x", true, time.Now().UTC()); !errors.Is(err, ErrTokenBankShareNotFound) {
		t.Fatalf("error = %v, want ErrTokenBankShareNotFound", err)
	}
	if err := repo.SetSharePaused(context.Background(), "", "", "x", true, time.Now().UTC()); !errors.Is(err, ErrTokenBankShareNotFound) {
		t.Fatalf("empty id error = %v, want ErrTokenBankShareNotFound", err)
	}
}

func TestTokenBankTakeOutShareKeepsUsageHistoryAndFreesTheKey(t *testing.T) {
	repo, provider := newTokenBankShareRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if _, _, err := repo.CreateShare(ctx, shareFixture("share-1", "user-1", "fp-1"),
		[]TokenBankShareModel{modelFixture("gpt-4o")}, 0, now); err != nil {
		t.Fatalf("CreateShare() error = %v", err)
	}
	// A settled call left a usage row carrying its own snapshot of owner/name.
	if _, err := provider.Write.ExecContext(ctx,
		`INSERT INTO token_bank_usage (request_id, share_id, owner_user_id, share_display_name,
			model_name, net_micro, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"req-1", "share-1", "user-1", "Share share-1", "gpt-4o", 9_000_000, now.Format(time.RFC3339)); err != nil {
		t.Fatalf("seed usage: %v", err)
	}

	ok, err := repo.TakeOutShare(ctx, "share-1", "user-1")
	if err != nil || !ok {
		t.Fatalf("TakeOutShare() ok=%v error=%v, want ok", ok, err)
	}
	if _, err := repo.LoadShare(ctx, "share-1", ""); !errors.Is(err, ErrTokenBankShareNotFound) {
		t.Fatalf("LoadShare after takeout error = %v, want ErrTokenBankShareNotFound", err)
	}
	models, err := repo.ListModels(ctx, "share-1")
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("models after takeout = %d, want 0", len(models))
	}
	// The history survives: that is the whole reason the snapshot columns exist.
	var count int
	if err := provider.Read.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM token_bank_usage WHERE share_id = ?`, "share-1").Scan(&count); err != nil {
		t.Fatalf("count usage: %v", err)
	}
	if count != 1 {
		t.Fatalf("usage rows after takeout = %d, want 1 (history must survive)", count)
	}
	// Hard delete => the (owner, fingerprint) unique row is gone, so the same
	// key can be shared again.
	if _, created, err := repo.CreateShare(ctx, shareFixture("share-9", "user-1", "fp-1"), nil, 0, now); err != nil || !created {
		t.Fatalf("re-share same key created=%v error=%v, want created", created, err)
	}
}

func TestTokenBankTakeOutShareScopesToOwner(t *testing.T) {
	repo, _ := newTokenBankShareRepo(t)
	ctx := context.Background()
	if _, _, err := repo.CreateShare(ctx, shareFixture("share-1", "user-1", "fp-1"), nil, 0, time.Now().UTC()); err != nil {
		t.Fatalf("CreateShare() error = %v", err)
	}
	if _, err := repo.TakeOutShare(ctx, "share-1", "intruder"); !errors.Is(err, ErrTokenBankShareNotFound) {
		t.Fatalf("cross-owner takeout error = %v, want ErrTokenBankShareNotFound", err)
	}
	if _, err := repo.LoadShare(ctx, "share-1", ""); err != nil {
		t.Fatalf("share must still exist after refused takeout, error = %v", err)
	}
}

func TestTokenBankRenameShareKeyKeepsShareIdentityAndEarnings(t *testing.T) {
	repo, provider := newTokenBankShareRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if _, _, err := repo.CreateShare(ctx, shareFixture("share-1", "user-1", "fp-old"),
		[]TokenBankShareModel{modelFixture("gpt-4o")}, 0, now); err != nil {
		t.Fatalf("CreateShare() error = %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx,
		`UPDATE token_bank_models SET earned_micro = 5_000_000 WHERE share_id = ?`, "share-1"); err != nil {
		t.Fatalf("seed earnings: %v", err)
	}

	if err := repo.RenameShareKey(ctx, "share-1", "user-1", "enc:new", "fp-new", "https://api.example.com/v2", "anthropic", now.Add(time.Minute)); err != nil {
		t.Fatalf("RenameShareKey() error = %v", err)
	}
	share, err := repo.LoadShare(ctx, "share-1", "")
	if err != nil {
		t.Fatalf("LoadShare() error = %v", err)
	}
	if share.KeyFingerprint != "fp-new" || share.EncryptedKey != "enc:new" {
		t.Fatalf("key after rotation = %q/%q, want fp-new/enc:new", share.KeyFingerprint, share.EncryptedKey)
	}
	if share.APIURL != "https://api.example.com/v2" || share.Protocol != "anthropic" {
		t.Fatalf("endpoint after rotation = %q %q", share.APIURL, share.Protocol)
	}
	// In-place rotation must not orphan the accumulated earnings.
	models, err := repo.ListModels(ctx, "share-1")
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(models) != 1 || models[0].EarnedMicro != 5_000_000 {
		t.Fatalf("models after rotation = %+v, want the original with earnings intact", models)
	}
}

func TestTokenBankRenameShareKeyRejectsRotationOntoAnotherShare(t *testing.T) {
	repo, _ := newTokenBankShareRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if _, _, err := repo.CreateShare(ctx, shareFixture("share-1", "user-1", "fp-1"), nil, 0, now); err != nil {
		t.Fatalf("CreateShare(share-1) error = %v", err)
	}
	if _, _, err := repo.CreateShare(ctx, shareFixture("share-2", "user-1", "fp-2"), nil, 0, now); err != nil {
		t.Fatalf("CreateShare(share-2) error = %v", err)
	}
	// Rotating share-2 onto share-1's fingerprint would collide on the unique
	// (owner, fingerprint) index; that must surface as a duplicate, not a 500.
	if err := repo.RenameShareKey(ctx, "share-2", "user-1", "enc:dup", "fp-1", "", "", now); !errors.Is(err, ErrTokenBankDuplicateKey) {
		t.Fatalf("colliding rotation error = %v, want ErrTokenBankDuplicateKey", err)
	}
	if err := repo.RenameShareKey(ctx, "ghost", "", "enc:x", "fp-x", "", "", now); !errors.Is(err, ErrTokenBankShareNotFound) {
		t.Fatalf("unknown share rotation error = %v, want ErrTokenBankShareNotFound", err)
	}
	if err := repo.RenameShareKey(ctx, "share-1", "", "", "fp-x", "", "", now); err == nil {
		t.Fatalf("rotation without a key error = nil, want error")
	}
}

func TestTokenBankLoadShareScopesToOwner(t *testing.T) {
	repo, _ := newTokenBankShareRepo(t)
	ctx := context.Background()
	if _, _, err := repo.CreateShare(ctx, shareFixture("share-1", "user-1", "fp-1"), nil, 0, time.Now().UTC()); err != nil {
		t.Fatalf("CreateShare() error = %v", err)
	}
	if _, err := repo.LoadShare(ctx, "share-1", "intruder"); !errors.Is(err, ErrTokenBankShareNotFound) {
		t.Fatalf("cross-owner load error = %v, want ErrTokenBankShareNotFound", err)
	}
	if _, err := repo.LoadShare(ctx, "share-1", "user-1"); err != nil {
		t.Fatalf("owner load error = %v", err)
	}
}

// --- models ---------------------------------------------------------------

func TestTokenBankSetModelTierUpdatesOnlyTheNamedModel(t *testing.T) {
	repo, _ := newTokenBankShareRepo(t)
	ctx := context.Background()

	if _, _, err := repo.CreateShare(ctx, shareFixture("share-1", "user-1", "fp-1"),
		[]TokenBankShareModel{modelFixture("gpt-4o"), modelFixture("gpt-4o-mini")}, 0, time.Now().UTC()); err != nil {
		t.Fatalf("CreateShare() error = %v", err)
	}
	if err := repo.SetModelTier(ctx, "share-1", "gpt-4o", "high", 2.0, "token_bank_high", time.Now().UTC()); err != nil {
		t.Fatalf("SetModelTier() error = %v", err)
	}
	models, err := repo.ListModels(ctx, "share-1")
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	byName := map[string]TokenBankShareModel{}
	for _, m := range models {
		byName[m.ModelName] = m
	}
	if got := byName["gpt-4o"]; got.Tier != "high" || got.TierMultiplier != 2.0 || got.ArrayID != "token_bank_high" {
		t.Fatalf("gpt-4o after re-tier = %+v, want high/2/token_bank_high", got)
	}
	// Re-tiering one model must not touch its sibling: tier is per-model
	// precisely so the rate can change without touching routing.
	if got := byName["gpt-4o-mini"]; got.Tier != "mid" || got.TierMultiplier != 1 {
		t.Fatalf("gpt-4o-mini after sibling re-tier = %+v, want mid/1 unchanged", got)
	}
}

func TestTokenBankSetModelTierRejectsUnknownShareOrModel(t *testing.T) {
	repo, _ := newTokenBankShareRepo(t)
	ctx := context.Background()
	if _, _, err := repo.CreateShare(ctx, shareFixture("share-1", "user-1", "fp-1"),
		[]TokenBankShareModel{modelFixture("gpt-4o")}, 0, time.Now().UTC()); err != nil {
		t.Fatalf("CreateShare() error = %v", err)
	}
	if err := repo.SetModelTier(ctx, "share-1", "ghost", "high", 2, "", time.Now().UTC()); !errors.Is(err, ErrTokenBankShareNotFound) {
		t.Fatalf("unknown model error = %v, want ErrTokenBankShareNotFound", err)
	}
	if err := repo.SetModelTier(ctx, "ghost", "gpt-4o", "high", 2, "", time.Now().UTC()); !errors.Is(err, ErrTokenBankShareNotFound) {
		t.Fatalf("unknown share error = %v, want ErrTokenBankShareNotFound", err)
	}
	if err := repo.SetModelTier(ctx, "", "gpt-4o", "high", 2, "", time.Now().UTC()); err == nil {
		t.Fatalf("empty share id error = nil, want validation error")
	}
}

func TestTokenBankCountShareModels(t *testing.T) {
	repo, _ := newTokenBankShareRepo(t)
	ctx := context.Background()
	if _, _, err := repo.CreateShare(ctx, shareFixture("share-1", "user-1", "fp-1"),
		[]TokenBankShareModel{modelFixture("gpt-4o"), modelFixture("gpt-4o-mini")}, 0, time.Now().UTC()); err != nil {
		t.Fatalf("CreateShare() error = %v", err)
	}
	n, err := repo.CountShareModels(ctx, "share-1")
	if err != nil {
		t.Fatalf("CountShareModels() error = %v", err)
	}
	if n != 2 {
		t.Fatalf("CountShareModels() = %d, want 2", n)
	}
	n, err = repo.CountShareModels(ctx, "ghost")
	if err != nil {
		t.Fatalf("CountShareModels(ghost) error = %v", err)
	}
	if n != 0 {
		t.Fatalf("CountShareModels(ghost) = %d, want 0", n)
	}
}

// TestTokenBankSyncShareModelsAddsNewModelsToALiveShare covers the capability
// CreateShare alone cannot provide. CreateShare is idempotent on
// (owner, fingerprint) and returns early for a known key, so a user who enables
// a new model upstream and presses 分享 again would otherwise never get it into
// the pool — and the model upsert's ON CONFLICT branch would be dead code.
func TestTokenBankSyncShareModelsAddsNewModelsToALiveShare(t *testing.T) {
	repo, _ := newTokenBankShareRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if _, _, err := repo.CreateShare(ctx, shareFixture("share-1", "user-1", "fp-1"),
		[]TokenBankShareModel{modelFixture("gpt-4o")}, 0, now); err != nil {
		t.Fatalf("CreateShare() error = %v", err)
	}
	// Same key, one extra model — exactly what a re-probe produces.
	retired, err := repo.SyncShareModels(ctx, "share-1", "user-1",
		[]TokenBankShareModel{modelFixture("gpt-4o"), modelFixture("gpt-4o-mini")}, now)
	if err != nil {
		t.Fatalf("SyncShareModels() error = %v", err)
	}
	if retired != 0 {
		t.Fatalf("retired = %d, want 0 (nothing dropped)", retired)
	}
	models, err := repo.ListModels(ctx, "share-1")
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("models = %d, want 2 after sync", len(models))
	}
}

func TestTokenBankSyncShareModelsPreservesEarnings(t *testing.T) {
	repo, provider := newTokenBankShareRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if _, _, err := repo.CreateShare(ctx, shareFixture("share-1", "user-1", "fp-1"),
		[]TokenBankShareModel{modelFixture("gpt-4o")}, 0, now); err != nil {
		t.Fatalf("CreateShare() error = %v", err)
	}
	if _, err := provider.Write.ExecContext(ctx,
		`UPDATE token_bank_models SET used_input_tokens = 1234, used_output_tokens = 567,
		        earned_micro = 9_000_000 WHERE share_id = ? AND model_name = ?`, "share-1", "gpt-4o"); err != nil {
		t.Fatalf("seed usage: %v", err)
	}

	// A re-sync that flips the tier and marks the model unavailable must leave
	// the counters alone: re-probing is not a reason to forget what was earned.
	if _, err := repo.SyncShareModels(ctx, "share-1", "user-1",
		[]TokenBankShareModel{{ModelName: "gpt-4o", MemberID: "member-2", Tier: "high",
			TierMultiplier: 2, ArrayID: "token_bank_high", Enabled: true, Available: false}}, now); err != nil {
		t.Fatalf("SyncShareModels() error = %v", err)
	}

	models, err := repo.ListModels(ctx, "share-1")
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("models = %d, want 1 (updated in place, not duplicated)", len(models))
	}
	m := models[0]
	// Routing/rate fields follow the new probe...
	if m.Tier != "high" || m.TierMultiplier != 2 || m.ArrayID != "token_bank_high" || m.MemberID != "member-2" {
		t.Fatalf("model after sync = %+v, want the new tier/member/array", m)
	}
	// ...availability follows it too (§3.1: the server probe wins)...
	if m.Available {
		t.Fatalf("available = true after a probe marked it down, want false")
	}
	// ...but the counters must survive.
	if m.UsedInputTokens != 1234 || m.UsedOutputTokens != 567 || m.EarnedMicro != 9_000_000 {
		t.Fatalf("counters after sync = in %d / out %d / earned %d, want 1234/567/9000000 preserved",
			m.UsedInputTokens, m.UsedOutputTokens, m.EarnedMicro)
	}
}

// TestTokenBankSyncShareModelsDisablesRetiredModels is the other half: a model
// the user unticked must stop being routed to, but its row — and therefore its
// usage history — stays, because a settlement may still be in flight against
// it and because the counters are the only record of that model's earnings.
func TestTokenBankSyncShareModelsDisablesRetiredModels(t *testing.T) {
	repo, _ := newTokenBankShareRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if _, _, err := repo.CreateShare(ctx, shareFixture("share-1", "user-1", "fp-1"),
		[]TokenBankShareModel{modelFixture("gpt-4o"), modelFixture("gpt-4o-mini")}, 0, now); err != nil {
		t.Fatalf("CreateShare() error = %v", err)
	}
	retired, err := repo.SyncShareModels(ctx, "share-1", "user-1",
		[]TokenBankShareModel{modelFixture("gpt-4o")}, now)
	if err != nil {
		t.Fatalf("SyncShareModels() error = %v", err)
	}
	if retired != 1 {
		t.Fatalf("retired = %d, want 1", retired)
	}
	models, err := repo.ListModels(ctx, "share-1")
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("models = %d, want 2 (retired is disabled, not deleted)", len(models))
	}
	byName := map[string]TokenBankShareModel{}
	for _, m := range models {
		byName[m.ModelName] = m
	}
	if !byName["gpt-4o"].Enabled {
		t.Fatalf("gpt-4o enabled = false, want still enabled")
	}
	if byName["gpt-4o-mini"].Enabled {
		t.Fatalf("gpt-4o-mini enabled = true, want disabled after falling out of the list")
	}
	// Re-adding it must re-enable the same row rather than create a second one.
	if _, err := repo.SyncShareModels(ctx, "share-1", "user-1",
		[]TokenBankShareModel{modelFixture("gpt-4o"), modelFixture("gpt-4o-mini")}, now); err != nil {
		t.Fatalf("SyncShareModels(re-add) error = %v", err)
	}
	models, err = repo.ListModels(ctx, "share-1")
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("models = %d after re-add, want 2 (same rows re-enabled)", len(models))
	}
	for _, m := range models {
		if !m.Enabled {
			t.Fatalf("model %s enabled = false after re-add, want true", m.ModelName)
		}
	}
}

func TestTokenBankSyncShareModelsScopesToOwnerAndValidates(t *testing.T) {
	repo, _ := newTokenBankShareRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if _, _, err := repo.CreateShare(ctx, shareFixture("share-1", "user-1", "fp-1"),
		[]TokenBankShareModel{modelFixture("gpt-4o")}, 0, now); err != nil {
		t.Fatalf("CreateShare() error = %v", err)
	}
	if _, err := repo.SyncShareModels(ctx, "share-1", "intruder",
		[]TokenBankShareModel{modelFixture("gpt-4o")}, now); !errors.Is(err, ErrTokenBankShareNotFound) {
		t.Fatalf("cross-owner sync error = %v, want ErrTokenBankShareNotFound", err)
	}
	if _, err := repo.SyncShareModels(ctx, "ghost", "",
		[]TokenBankShareModel{modelFixture("gpt-4o")}, now); !errors.Is(err, ErrTokenBankShareNotFound) {
		t.Fatalf("unknown share sync error = %v, want ErrTokenBankShareNotFound", err)
	}
	if _, err := repo.SyncShareModels(ctx, "", "",
		[]TokenBankShareModel{modelFixture("gpt-4o")}, now); !errors.Is(err, ErrTokenBankShareNotFound) {
		t.Fatalf("empty share sync error = %v, want ErrTokenBankShareNotFound", err)
	}
	if _, err := repo.SyncShareModels(ctx, "share-1", "user-1",
		[]TokenBankShareModel{{MemberID: "no-name"}}, now); err == nil {
		t.Fatalf("unnamed model error = nil, want error")
	}
	if _, err := repo.SyncShareModels(ctx, "share-1", "user-1",
		[]TokenBankShareModel{modelFixture("gpt-4o"), modelFixture("gpt-4o")}, now); err == nil {
		t.Fatalf("duplicate model error = nil, want error")
	}
}

// --- price book -----------------------------------------------------------

func TestTokenBankUpsertPriceRuleIsIdempotentOnPattern(t *testing.T) {
	repo, _ := newTokenBankShareRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()

	first, err := repo.UpsertPriceRule(ctx, TokenBankPriceRule{
		ModelPattern: "gpt-4o", UnitInputPer10K: 3, UnitOutputPer10K: 6,
		UnitCachedReadPer10K: 0.3, UnitCacheWritePer10K: 3.75,
	}, now)
	if err != nil {
		t.Fatalf("UpsertPriceRule() error = %v", err)
	}
	if first.ID == "" {
		t.Fatalf("price rule id is empty, want a deterministic id")
	}
	second, err := repo.UpsertPriceRule(ctx, TokenBankPriceRule{
		ModelPattern: "gpt-4o", UnitInputPer10K: 9, UnitOutputPer10K: 18,
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("UpsertPriceRule(update) error = %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("re-upsert changed id %q -> %q, want stable", first.ID, second.ID)
	}
	rules, err := repo.ListPriceRules(ctx)
	if err != nil {
		t.Fatalf("ListPriceRules() error = %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("rules = %d, want 1 (in-place update)", len(rules))
	}
	if rules[0].UnitInputPer10K != 9 {
		t.Fatalf("unit_input = %v, want 9", rules[0].UnitInputPer10K)
	}
}

func TestTokenBankUpsertPriceRuleRejectsBadInput(t *testing.T) {
	repo, _ := newTokenBankShareRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if _, err := repo.UpsertPriceRule(ctx, TokenBankPriceRule{ModelPattern: "  "}, now); err == nil {
		t.Fatalf("empty pattern error = nil, want error")
	}
	if _, err := repo.UpsertPriceRule(ctx, TokenBankPriceRule{ModelPattern: "x", UnitInputPer10K: -1}, now); err == nil {
		t.Fatalf("negative unit error = nil, want error")
	}
}

func TestTokenBankDeletePriceRuleMatchesIdOrPattern(t *testing.T) {
	repo, _ := newTokenBankShareRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()

	rule, err := repo.UpsertPriceRule(ctx, TokenBankPriceRule{ModelPattern: "gpt-4o"}, now)
	if err != nil {
		t.Fatalf("UpsertPriceRule() error = %v", err)
	}
	if ok, err := repo.DeletePriceRule(ctx, rule.ID); err != nil || !ok {
		t.Fatalf("DeletePriceRule(id) ok=%v error=%v, want ok", ok, err)
	}
	if _, err := repo.UpsertPriceRule(ctx, TokenBankPriceRule{ModelPattern: "gpt-4o"}, now); err != nil {
		t.Fatalf("re-upsert error = %v", err)
	}
	if ok, err := repo.DeletePriceRule(ctx, "gpt-4o"); err != nil || !ok {
		t.Fatalf("DeletePriceRule(pattern) ok=%v error=%v, want ok", ok, err)
	}
	if ok, err := repo.DeletePriceRule(ctx, "gpt-4o"); err != nil || ok {
		t.Fatalf("DeletePriceRule(missing) ok=%v error=%v, want not-ok", ok, err)
	}
}

func TestTokenBankResolvePriceExactBeatsPrefix(t *testing.T) {
	repo, _ := newTokenBankShareRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()

	for _, rule := range []TokenBankPriceRule{
		{ModelPattern: "gpt-4o-*", UnitInputPer10K: 1},
		{ModelPattern: "gpt-4o-mini", UnitInputPer10K: 2},
		{ModelPattern: "gpt-4o", UnitInputPer10K: 3},
	} {
		if _, err := repo.UpsertPriceRule(ctx, rule, now); err != nil {
			t.Fatalf("UpsertPriceRule(%s) error = %v", rule.ModelPattern, err)
		}
	}

	// Exact match wins outright even though `gpt-4o-*` is a prefix too.
	got, ok, err := repo.ResolvePrice(ctx, "gpt-4o")
	if err != nil || !ok {
		t.Fatalf("ResolvePrice(gpt-4o) ok=%v error=%v, want found", ok, err)
	}
	if got.ModelPattern != "gpt-4o" {
		t.Fatalf("ResolvePrice(gpt-4o) = %q, want exact gpt-4o", got.ModelPattern)
	}

	got, ok, err = repo.ResolvePrice(ctx, "gpt-4o-mini")
	if err != nil || !ok {
		t.Fatalf("ResolvePrice(gpt-4o-mini) ok=%v error=%v, want found", ok, err)
	}
	if got.ModelPattern != "gpt-4o-mini" {
		t.Fatalf("ResolvePrice(gpt-4o-mini) = %q, want exact gpt-4o-mini", got.ModelPattern)
	}

	// No exact rule: the glob covers it.
	got, ok, err = repo.ResolvePrice(ctx, "gpt-4o-2024-11-20")
	if err != nil || !ok {
		t.Fatalf("ResolvePrice(dated) ok=%v error=%v, want found via prefix", ok, err)
	}
	if got.ModelPattern != "gpt-4o-*" {
		t.Fatalf("ResolvePrice(dated) = %q, want gpt-4o-*", got.ModelPattern)
	}
}

func TestTokenBankResolvePriceLongestPrefixWins(t *testing.T) {
	repo, _ := newTokenBankShareRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()

	for _, rule := range []TokenBankPriceRule{
		{ModelPattern: "gpt-*", UnitInputPer10K: 1},
		{ModelPattern: "gpt-4-*", UnitInputPer10K: 2},
		{ModelPattern: "gpt-4o-*", UnitInputPer10K: 3},
	} {
		if _, err := repo.UpsertPriceRule(ctx, rule, now); err != nil {
			t.Fatalf("UpsertPriceRule(%s) error = %v", rule.ModelPattern, err)
		}
	}
	got, ok, err := repo.ResolvePrice(ctx, "gpt-4o-mini")
	if err != nil || !ok {
		t.Fatalf("ResolvePrice() ok=%v error=%v, want found", ok, err)
	}
	if got.ModelPattern != "gpt-4o-*" {
		t.Fatalf("ResolvePrice(gpt-4o-mini) = %q, want the longest prefix gpt-4o-*", got.ModelPattern)
	}
}

func TestTokenBankResolvePriceReportsNoMatch(t *testing.T) {
	repo, _ := newTokenBankShareRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if _, err := repo.UpsertPriceRule(ctx, TokenBankPriceRule{ModelPattern: "gpt-4o-*"}, now); err != nil {
		t.Fatalf("UpsertPriceRule() error = %v", err)
	}
	// A prefix that is not a *prefix* of the model must not match.
	if _, ok, err := repo.ResolvePrice(ctx, "claude-sonnet"); err != nil || ok {
		t.Fatalf("ResolvePrice(claude-sonnet) ok=%v error=%v, want no match", ok, err)
	}
	// The caller falls back to settings defaults, which it can only do if an
	// unmatched model reports false rather than the zero rule with no error.
	if _, ok, err := repo.ResolvePrice(ctx, ""); err != nil || ok {
		t.Fatalf("ResolvePrice(empty) ok=%v error=%v, want no match", ok, err)
	}
}

func TestTokenBankWildcardPrefixOnlyHonoursTrailingStar(t *testing.T) {
	cases := []struct {
		pattern string
		wantPfx string
		wantOK  bool
	}{
		{"gpt-4o-*", "gpt-4o-", true},
		{"*", "", false},
		{"gpt-4o", "", false},  // no star at all
		{"gpt-*o", "", false},  // mid-pattern star
		{"a*b*", "", false},    // trailing star, but the prefix `a*b` still has a star
		{"gpt-4o?", "", false}, // question-mark wildcard is not honoured
	}
	for _, tc := range cases {
		got, ok := tokenBankWildcardPrefix(tc.pattern)
		if ok != tc.wantOK || got != tc.wantPfx {
			t.Fatalf("tokenBankWildcardPrefix(%q) = (%q, %v), want (%q, %v)",
				tc.pattern, got, ok, tc.wantPfx, tc.wantOK)
		}
	}
}

func TestTokenBankSortedPricePatternsOrdersByPrefixLength(t *testing.T) {
	rules := []TokenBankPriceRule{
		{ModelPattern: "gpt-*"},
		{ModelPattern: "gpt-4o-*"},
		{ModelPattern: "gpt-4-*"},
	}
	got := SortedPricePatterns(rules)
	want := []string{"gpt-4o-*", "gpt-4-*", "gpt-*"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("SortedPricePatterns() = %v, want %v", got, want)
		}
	}
}

// --- settings -------------------------------------------------------------

func TestTokenBankSettingsDefaultsAndForwardCompatibility(t *testing.T) {
	def := DefaultTokenBankSettings()
	if def.FeeRate != 0.10 || def.FeeTarget != "provider" {
		t.Fatalf("default fee = %v/%q, want 0.10/provider", def.FeeRate, def.FeeTarget)
	}
	if def.CreditShareMaxRatio != 0.5 || def.CreditShareLinkTTLHours != 168 {
		t.Fatalf("default share limits = %v/%d, want 0.5/168", def.CreditShareMaxRatio, def.CreditShareLinkTTLHours)
	}
	if def.MaxSharesPerUser != 20 {
		t.Fatalf("default max shares = %d, want 20", def.MaxSharesPerUser)
	}
	if def.CanaryWindowHours != 24 || def.CanaryWindow() != 24*time.Hour {
		t.Fatalf("default canary window = %d / %s, want 24h", def.CanaryWindowHours, def.CanaryWindow())
	}

	// An entirely empty blob yields the defaults.
	if got := ParseTokenBankSettings(""); !reflect.DeepEqual(got, def) {
		t.Fatalf("ParseTokenBankSettings(\"\") = %+v, want defaults", got)
	}
	// A corrupt blob must not strand the feature.
	if got := ParseTokenBankSettings("{not json"); !reflect.DeepEqual(got, def) {
		t.Fatalf("ParseTokenBankSettings(corrupt) = %+v, want defaults", got)
	}

	// A blob written before a field existed must be repaired, not zeroed.
	legacy := `{"fee_rate":0.25,"fee_target":"consumer"}`
	got := ParseTokenBankSettings(legacy)
	if got.FeeRate != 0.25 || got.FeeTarget != "consumer" {
		t.Fatalf("legacy fee = %v/%q, want 0.25/consumer (stored values honoured)", got.FeeRate, got.FeeTarget)
	}
	if got.MaxSharesPerUser != def.MaxSharesPerUser || got.CreditShareMaxRatio != def.CreditShareMaxRatio {
		t.Fatalf("legacy blob left unset caps at zero: %+v, want defaults filled in", got)
	}
	if got.CanaryWindowHours != 24 {
		t.Fatalf("legacy blob canary window = %d, want the 24h default", got.CanaryWindowHours)
	}

	// fee_rate 0 is a legitimate "no fee" and must survive the repair pass.
	if got := ParseTokenBankSettings(`{"fee_rate":0}`); got.FeeRate != 0 {
		t.Fatalf("fee_rate 0 = %v, want 0 (zero is meaningful here)", got.FeeRate)
	}
	// Zero canary hours means a new model skips the window. A negative value
	// cannot be read that way, so it falls back to the default.
	if got := ParseTokenBankSettings(`{"canary_window_hours":0}`); got.CanaryWindowHours != 0 || got.CanaryWindow() != 0 {
		t.Fatalf("canary 0 = %d / %s, want 0", got.CanaryWindowHours, got.CanaryWindow())
	}
	if got := ParseTokenBankSettings(`{"canary_window_hours":-3}`); got.CanaryWindowHours != 24 {
		t.Fatalf("negative canary = %d, want 24", got.CanaryWindowHours)
	}
	// An out-of-range ratio is repaired; a bogus fee_target is normalised.
	if got := ParseTokenBankSettings(`{"credit_share_max_ratio":1.7,"fee_target":"banker"}`); got.CreditShareMaxRatio != def.CreditShareMaxRatio || got.FeeTarget != "provider" {
		t.Fatalf("out-of-range repair = %v/%q, want defaults/provider", got.CreditShareMaxRatio, got.FeeTarget)
	}
}
