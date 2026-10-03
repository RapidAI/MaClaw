package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTokenBankBadges(t *testing.T) {
	rank, life := TokenBankBadges(1, 10000*1_000_000)
	if rank != "gold" || life != "pillar" {
		t.Fatalf("top pillar = %s %s", rank, life)
	}
	rank, life = TokenBankBadges(2, 1000*1_000_000)
	if rank != "silver" || life != "steady" {
		t.Fatalf("silver steady = %s %s", rank, life)
	}
	rank, life = TokenBankBadges(3, 100*1_000_000)
	if rank != "bronze" || life != "contributor" {
		t.Fatalf("bronze contributor = %s %s", rank, life)
	}
	rank, life = TokenBankBadges(4, 1)
	if rank != "" || life != "sprout" {
		t.Fatalf("sprout = %q %q", rank, life)
	}
	rank, life = TokenBankBadges(0, 0)
	if rank != "" || life != "" {
		t.Fatalf("no usage = %q %q, want empty", rank, life)
	}
}

func TestTokenBankPrivateShareAndExtraKeysRoundTrip(t *testing.T) {
	repo, _ := newTokenBankShareRepo(t)
	ctx := context.Background()
	share := shareFixture("shr-private", "owner-a", "fp-primary")
	share.Visibility = TokenBankVisibilityPrivate
	audience, err := MarshalTokenBankAudiences([]TokenBankAudience{{HubID: "Hub-A", TenantID: "Ten-1"}})
	if err != nil {
		t.Fatal(err)
	}
	share.AudienceJSON = audience
	extra, err := MarshalTokenBankExtraKeys([]TokenBankStoredKey{{Fingerprint: "fp-extra", Encrypted: "enc-extra"}})
	if err != nil {
		t.Fatal(err)
	}
	share.ExtraKeysJSON = extra
	stored, created, err := repo.CreateShare(ctx, share, []TokenBankShareModel{modelFixture("gpt")}, 0, time.Now().UTC())
	if err != nil || !created {
		t.Fatalf("create = %v created=%v", err, created)
	}
	loaded, err := repo.LoadShare(ctx, stored.ID, share.OwnerUserID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Visibility != TokenBankVisibilityPrivate {
		t.Fatalf("visibility = %q", loaded.Visibility)
	}
	parsed := ParseTokenBankAudiences(loaded.AudienceJSON)
	if len(parsed) != 1 || parsed[0].HubID != "Hub-A" || parsed[0].TenantID != "Ten-1" {
		t.Fatalf("audiences = %+v", parsed)
	}
	keys := ParseTokenBankExtraKeys(loaded.ExtraKeysJSON)
	if len(keys) != 1 || keys[0].Fingerprint != "fp-extra" {
		t.Fatalf("extra keys = %+v", keys)
	}

	next, err := MarshalTokenBankExtraKeys(append(keys, TokenBankStoredKey{Fingerprint: "fp-two", Encrypted: "enc-two"}))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateShareExtraKeys(ctx, stored.ID, share.OwnerUserID, next, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateShareExtraKeysCAS(ctx, stored.ID, share.OwnerUserID, loaded.ExtraKeysJSON, "[]", time.Now().UTC()); !errors.Is(err, ErrTokenBankShareConflict) {
		t.Fatalf("stale extra-key write = %v, want conflict", err)
	}
	kept, err := repo.LoadShare(ctx, stored.ID, share.OwnerUserID)
	if err != nil {
		t.Fatal(err)
	}
	keptKeys := ParseTokenBankExtraKeys(kept.ExtraKeysJSON)
	if len(keptKeys) != 2 || keptKeys[0].Fingerprint != "fp-extra" || keptKeys[1].Fingerprint != "fp-two" {
		t.Fatalf("stale write changed keys = %+v", keptKeys)
	}
	taken, err := repo.OwnerKeyFingerprintTaken(ctx, share.OwnerUserID, "fp-primary", "")
	if err != nil || !taken {
		t.Fatalf("primary taken = %v err=%v", taken, err)
	}
	taken, err = repo.OwnerKeyFingerprintTaken(ctx, share.OwnerUserID, "fp-primary", stored.ID)
	if err != nil || taken {
		t.Fatalf("exceptShare should skip the primary, taken=%v err=%v", taken, err)
	}
	taken, err = repo.OwnerKeyFingerprintTaken(ctx, share.OwnerUserID, "fp-two", stored.ID)
	if err != nil || !taken {
		t.Fatalf("extra key on the excepted share still counts, taken=%v err=%v", taken, err)
	}
}

func TestTokenBankCanaryUntilSurvivesUpsert(t *testing.T) {
	repo, _ := newTokenBankShareRepo(t)
	ctx := context.Background()
	share := shareFixture("shr-canary", "owner-a", "fp-canary")
	first := time.Date(2026, 10, 2, 1, 0, 0, 0, time.UTC)
	kept := modelFixture("gpt")
	kept.CanaryUntil = first
	if _, _, err := repo.CreateShare(ctx, share, []TokenBankShareModel{kept}, 0, first); err != nil {
		t.Fatal(err)
	}
	later := first.Add(48 * time.Hour)
	kept.CanaryUntil = later
	fresh := modelFixture("old")
	if _, err := repo.SyncShareModels(ctx, share.ID, share.OwnerUserID, []TokenBankShareModel{kept, fresh}, later); err != nil {
		t.Fatal(err)
	}
	models, err := repo.ListModels(ctx, share.ID)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]TokenBankShareModel{}
	for _, model := range models {
		byName[model.ModelName] = model
	}
	if got := byName["gpt"].CanaryUntil; !got.Equal(first) {
		t.Fatalf("existing canary = %s, want %s", got, first)
	}
	if !byName["old"].CanaryUntil.IsZero() {
		t.Fatalf("a sync that leaves CanaryUntil empty must stay grandfathered, got %s", byName["old"].CanaryUntil)
	}
}

func TestTokenBankLeaderboardUsesUsageNotEarnedColumn(t *testing.T) {
	repo, provider := newTokenBankShareRepo(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 2, 3, 0, 0, 0, time.UTC)
	for _, item := range []struct {
		id, owner, fp string
	}{
		{"s-a", "owner-a", "fp-a"},
		{"s-b", "owner-b", "fp-b"},
		{"s-c", "owner-c", "fp-c"},
	} {
		share := shareFixture(item.id, item.owner, item.fp)
		if _, _, err := repo.CreateShare(ctx, share, []TokenBankShareModel{modelFixture("gpt")}, 0, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := provider.Write.Exec(`UPDATE token_bank_models SET earned_micro = 999000000 WHERE share_id = 's-c'`); err != nil {
		t.Fatal(err)
	}
	insert := `INSERT INTO token_bank_usage (request_id, share_id, owner_user_id, model_name, net_micro, input_tokens, output_tokens, created_at) VALUES (?, ?, ?, 'gpt', ?, ?, ?, ?)`
	if _, err := provider.Write.Exec(insert, "req-a", "s-a", "owner-a", int64(5_000_000), int64(10), int64(2), now.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Write.Exec(insert, "req-b", "s-b", "owner-b", int64(1_000_000), int64(1), int64(1), now.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Write.Exec(insert, "req-zero", "s-c", "owner-c", int64(0), int64(1), int64(0), now.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.Leaderboard(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("leaders = %+v, want the two owners with usage", rows)
	}
	if rows[0].OwnerUserID != "owner-a" || rows[0].EarnedMicro != 5_000_000 || rows[0].OwnerEmail != "owner-a@example.com" {
		t.Fatalf("first = %+v", rows[0])
	}
	if rows[1].OwnerUserID != "owner-b" || rows[1].EarnedMicro != 1_000_000 {
		t.Fatalf("second = %+v", rows[1])
	}
	earned, rank, err := repo.SharerStanding(ctx, "owner-a")
	if err != nil || earned != 5_000_000 || rank != 1 {
		t.Fatalf("standing a = %d rank %d err %v", earned, rank, err)
	}
	earned, rank, err = repo.SharerStanding(ctx, "owner-c")
	if err != nil || earned != 0 || rank != 0 {
		t.Fatalf("owner with only the stale column = %d rank %d err %v", earned, rank, err)
	}
}
