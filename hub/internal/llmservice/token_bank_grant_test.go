package llmservice

import (
	"context"
	"errors"
	"testing"
	"time"
)

type tokenBankCenterFake struct {
	withdrawal  TokenBankCenterWithdrawal
	withdrawN   int
	finishN     int
	lastReissue bool
	withdrawErr error
	finishErr   error
}

func (f *tokenBankCenterFake) WithdrawTokenBank(context.Context, TokenBankCenterWithdraw) (TokenBankCenterWithdrawal, error) {
	f.withdrawN++
	if f.withdrawErr != nil {
		return TokenBankCenterWithdrawal{}, f.withdrawErr
	}
	return f.withdrawal, nil
}

func (f *tokenBankCenterFake) FinishTokenBankGrant(_ context.Context, _, _ string, reissue bool) error {
	f.finishN++
	f.lastReissue = reissue
	return f.finishErr
}

func tokenBankRegistryWithGroup(t *testing.T, system *testSystemSettings, groupID string) {
	t.Helper()
	reg := &Registry{ModelServiceGroups: []ModelServiceGroup{{ID: groupID, Name: groupID}}}
	if err := SaveRegistry(context.Background(), system, reg); err != nil {
		t.Fatal(err)
	}
}

func TestIssueTokenBankGrantErrorsWhenServiceGroupMissing(t *testing.T) {
	system := newTestSystemSettings()
	_, _, err := IssueTokenBankGrant(context.Background(), system, "user-1", "owner@example.com", "missing", "req-1", 2_500_000)
	if !errors.Is(err, ErrTokenBankServiceGroupMissing) {
		t.Fatalf("IssueTokenBankGrant() error = %v, want missing service group", err)
	}
	reg, err := LoadRegistry(context.Background(), system)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Grants) != 0 {
		t.Fatalf("grants = %+v, want none", reg.Grants)
	}
}

func TestIssueTokenBankGrantIsPermanentAndIdempotent(t *testing.T) {
	system := newTestSystemSettings()
	tokenBankRegistryWithGroup(t, system, "paid")
	id, created, err := IssueTokenBankGrant(context.Background(), system, "user-1", "owner@example.com", "paid", "req-1", 2_500_000)
	if err != nil || !created || id == "" {
		t.Fatalf("first grant id=%q created=%v err=%v", id, created, err)
	}
	again, created, err := IssueTokenBankGrant(context.Background(), system, "user-1", "owner@example.com", "paid", "req-1", 2_500_000)
	if err != nil || created || again != id {
		t.Fatalf("replay id=%q created=%v err=%v, want %s created=false", again, created, err, id)
	}
	reg, err := LoadRegistry(context.Background(), system)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Grants) != 1 {
		t.Fatalf("grant count = %d, want 1", len(reg.Grants))
	}
	grant := reg.Grants[0]
	if grant.Source != TokenBankGrantSource || !grant.Permanent || grant.CardID != "tbk:req-1" {
		t.Fatalf("grant identity = %+v", grant)
	}
	if grant.ExpiresAt != tokenBankGrantExpiresAt {
		t.Fatalf("expires = %s, want far-future sentinel", grant.ExpiresAt)
	}
	if grant.CreditsTotal != 2.5 {
		t.Fatalf("credits = %v, want 2.5 exactly", grant.CreditsTotal)
	}
	if SumTokenBankGrantMicro(reg.Grants, "owner@example.com") != 2_500_000 {
		t.Fatalf("reconcile micro = %d, want 2500000", SumTokenBankGrantMicro(reg.Grants, "owner@example.com"))
	}
}

func TestTokenBankGrantDoesNotQueueTheNextGrantUntilYear9999(t *testing.T) {
	now := time.Date(2026, 10, 2, 8, 0, 0, 0, time.UTC)
	owner := newUserAccountRef("user-1", "owner@example.com")
	bank := Grant{
		ID: "bank", UserID: "user-1", Email: "owner@example.com",
		ServiceGroupID: "paid", Source: TokenBankGrantSource, Permanent: true,
		StartsAt: now.Add(-time.Hour), ExpiresAt: tokenBankGrantExpiresAt,
		CreditsTotal: 2.5,
	}
	if got := nextGrantStart(&Registry{Grants: []Grant{bank}}, owner, "paid", now); !got.Equal(now) {
		t.Fatalf("only token-bank grant queues the next grant at %s, want %s", got, now)
	}
	cardExpiry := now.Add(10 * 24 * time.Hour)
	card := Grant{
		ID: "card", UserID: "user-1", Email: "owner@example.com",
		ServiceGroupID: "paid", Source: "card",
		StartsAt: now.Add(-time.Hour), ExpiresAt: cardExpiry, CreditsTotal: 5,
	}
	if got := nextGrantStart(&Registry{Grants: []Grant{bank, card}}, owner, "paid", now); !got.Equal(cardExpiry) {
		t.Fatalf("next grant = %s, want the card expiry %s", got, cardExpiry)
	}
	spent := card
	spent.ID = "spent"
	spent.CreditsUsed = 5
	if got := nextGrantStart(&Registry{Grants: []Grant{bank, spent}}, owner, "paid", now); !got.Equal(cardExpiry) {
		t.Fatalf("next grant beside an exhausted card = %s, want %s", got, cardExpiry)
	}
	if got := nextGrantStart(&Registry{Grants: []Grant{card}}, owner, "paid", now); !got.Equal(cardExpiry) {
		t.Fatalf("card alone = %s, want %s", got, cardExpiry)
	}
}

func TestPromoteQueuedMeteredGrantsIgnoresTokenBankWindow(t *testing.T) {
	now := time.Date(2026, 10, 2, 8, 0, 0, 0, time.UTC)
	queuedStart := now.Add(48 * time.Hour)
	duration := 30 * 24 * time.Hour
	reg := &Registry{Grants: []Grant{
		{
			ID: "bank", Email: "owner@example.com", ServiceGroupID: "paid",
			Source: TokenBankGrantSource, Permanent: true,
			StartsAt: now.Add(-time.Hour), ExpiresAt: tokenBankGrantExpiresAt,
			CreditsTotal: 5,
		},
		{
			ID: "scheduled", Email: "owner@example.com", ServiceGroupID: "paid",
			Source:   "card",
			StartsAt: queuedStart, ExpiresAt: queuedStart.Add(duration),
			CreditsTotal: 100,
		},
	}}
	if changed := PromoteQueuedMeteredGrants(reg, now); changed != 0 {
		t.Fatalf("PromoteQueuedMeteredGrants() changed = %d, want 0", changed)
	}
	if !reg.Grants[1].StartsAt.Equal(queuedStart) {
		t.Fatalf("scheduled start = %s, want %s", reg.Grants[1].StartsAt, queuedStart)
	}
	reg.Grants = append(reg.Grants, Grant{
		ID: "active", Email: "owner@example.com", ServiceGroupID: "paid", Source: "card",
		StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour), CreditsTotal: 10,
	})
	if changed := PromoteQueuedMeteredGrants(reg, now); changed != 1 {
		t.Fatalf("PromoteQueuedMeteredGrants() changed = %d, want 1", changed)
	}
	if !reg.Grants[1].StartsAt.Equal(now) || reg.Grants[1].ExpiresAt.Sub(reg.Grants[1].StartsAt) != duration {
		t.Fatalf("queued card window = %s .. %s, want start %s and duration %s", reg.Grants[1].StartsAt, reg.Grants[1].ExpiresAt, now, duration)
	}
}

func TestIssueTokenBankGrantRejectsADifferentServiceGroup(t *testing.T) {
	system := newTestSystemSettings()
	reg := &Registry{ModelServiceGroups: []ModelServiceGroup{{ID: "paid"}, {ID: "other"}}}
	if err := SaveRegistry(context.Background(), system, reg); err != nil {
		t.Fatal(err)
	}
	id, created, err := IssueTokenBankGrant(context.Background(), system, "user-1", "owner@example.com", "paid", "req-1", 2_500_000)
	if err != nil || !created || id == "" {
		t.Fatalf("first grant id=%q created=%v err=%v", id, created, err)
	}
	again, created, err := IssueTokenBankGrant(context.Background(), system, "user-1", "owner@example.com", "other", "req-1", 2_500_000)
	if !errors.Is(err, ErrTokenBankGrantGroupMismatch) || created || again != id {
		t.Fatalf("other group id=%q created=%v err=%v, want mismatch for %s", again, created, err, id)
	}
	loaded, err := LoadRegistry(context.Background(), system)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Grants) != 1 || loaded.Grants[0].ServiceGroupID != "paid" || loaded.Grants[0].CreditsTotal != 2.5 {
		t.Fatalf("grants = %+v, want the original paid grant untouched", loaded.Grants)
	}
	same, created, err := IssueTokenBankGrant(context.Background(), system, "user-1", "owner@example.com", "PAID", "req-1", 2_500_000)
	if err != nil || created || same != id {
		t.Fatalf("case-insensitive replay id=%q created=%v err=%v", same, created, err)
	}
}

func TestPullTokenBankGrantDoesNotFundADifferentServiceGroup(t *testing.T) {
	system := newTestSystemSettings()
	tokenBankRegistryWithGroup(t, system, "paid")
	fake := &tokenBankCenterFake{withdrawal: TokenBankCenterWithdrawal{AmountMicro: 1_000_000, Status: "issued", Created: true}}
	first, err := PullTokenBankGrant(context.Background(), system, fake, "user-1", "owner@example.com", "paid", "req-group", 1_000_000, true, "self", "")
	if err != nil || !first.Created || fake.finishN != 1 {
		t.Fatalf("first = %+v finish=%d err=%v", first, fake.finishN, err)
	}
	reg, err := LoadRegistry(context.Background(), system)
	if err != nil {
		t.Fatal(err)
	}
	reg.ModelServiceGroups = append(reg.ModelServiceGroups, ModelServiceGroup{ID: "other", Name: "other"})
	if err := SaveRegistry(context.Background(), system, reg); err != nil {
		t.Fatal(err)
	}
	fake.withdrawal.Created = false
	second, err := PullTokenBankGrant(context.Background(), system, fake, "user-1", "owner@example.com", "other", "req-group", 1_000_000, true, "self", "")
	if !errors.Is(err, ErrTokenBankGrantGroupMismatch) {
		t.Fatalf("second = %+v err=%v, want group mismatch", second, err)
	}
	if second.GrantID != first.GrantID || fake.finishN != 2 || fake.lastReissue {
		t.Fatalf("second grant=%q finish=%d reissue=%v, want the original grant bound", second.GrantID, fake.finishN, fake.lastReissue)
	}
	loaded, err := LoadRegistry(context.Background(), system)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Grants) != 1 || loaded.Grants[0].ServiceGroupID != "paid" || loaded.Grants[0].CreditsTotal != 1 {
		t.Fatalf("grants = %+v, want one grant still on paid", loaded.Grants)
	}
}

func TestPullTokenBankGrantDoesNotDebitWhenGroupIsMissing(t *testing.T) {
	system := newTestSystemSettings()
	fake := &tokenBankCenterFake{withdrawal: TokenBankCenterWithdrawal{AmountMicro: 1_000_000, Status: "issued"}}
	_, err := PullTokenBankGrant(context.Background(), system, fake, "user-1", "owner@example.com", "missing", "req-1", 0, true, "self", "")
	if !errors.Is(err, ErrTokenBankServiceGroupMissing) {
		t.Fatalf("error = %v, want missing group", err)
	}
	if fake.withdrawN != 0 {
		t.Fatalf("withdraw calls = %d, want 0", fake.withdrawN)
	}
}

func TestPullTokenBankGrantRetriesWithoutASecondDebit(t *testing.T) {
	system := newTestSystemSettings()
	tokenBankRegistryWithGroup(t, system, "paid")
	fake := &tokenBankCenterFake{withdrawal: TokenBankCenterWithdrawal{AmountMicro: 1_000_000, Status: "issued", Created: true}}
	first, err := PullTokenBankGrant(context.Background(), system, fake, "user-1", "owner@example.com", "paid", "req-retry", 1_000_000, true, "self", "")
	if err != nil || !first.Created || fake.withdrawN != 1 || fake.finishN != 1 || fake.lastReissue {
		t.Fatalf("first = %+v withdraw=%d finish=%d reissue=%v err=%v", first, fake.withdrawN, fake.finishN, fake.lastReissue, err)
	}
	fake.withdrawal.Created = false
	fake.withdrawal.GrantID = first.GrantID
	fake.withdrawal.Status = "bound"
	second, err := PullTokenBankGrant(context.Background(), system, fake, "user-1", "owner@example.com", "paid", "req-retry", 1_000_000, true, "self", "")
	if err != nil || second.Created || second.GrantID != first.GrantID {
		t.Fatalf("replay = %+v err=%v", second, err)
	}
	if fake.withdrawN != 2 {
		t.Fatalf("withdraw calls = %d, want 2 (the second is a replay, not a second debit on the hub)", fake.withdrawN)
	}
	reg, err := LoadRegistry(context.Background(), system)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Grants) != 1 || reg.Grants[0].CreditsTotal != 1 {
		t.Fatalf("grants = %+v, want one grant of 1 credit", reg.Grants)
	}
	delta, within := TokenBankReconcile(SumTokenBankGrantMicro(reg.Grants, "owner@example.com"), 1_000_000)
	if !within || delta != 0 {
		t.Fatalf("reconcile delta=%d within=%v", delta, within)
	}
}

func TestPullTokenBankGrantReissuesWhenTheRecordedGrantIsGone(t *testing.T) {
	system := newTestSystemSettings()
	tokenBankRegistryWithGroup(t, system, "paid")
	fake := &tokenBankCenterFake{withdrawal: TokenBankCenterWithdrawal{
		AmountMicro: 1_000_000,
		Status:      "bound",
		GrantID:     "grant-old",
		Created:     false,
	}}
	got, err := PullTokenBankGrant(context.Background(), system, fake, "user-1", "owner@example.com", "paid", "req-reissue", 1_000_000, false, "self", "")
	if err != nil || !got.Created || !fake.lastReissue || fake.finishN != 1 {
		t.Fatalf("reissue result=%+v finish=%d reissue=%v err=%v", got, fake.finishN, fake.lastReissue, err)
	}
}

func TestTokenBankReconcileAllowsOneMicro(t *testing.T) {
	if _, within := TokenBankReconcile(1_000_001, 1_000_000); !within {
		t.Fatal("1 micro must be inside tolerance")
	}
	if _, within := TokenBankReconcile(1_000_002, 1_000_000); within {
		t.Fatal("2 micro must be outside tolerance")
	}
}

func TestResolveTokenBankServiceGroupRefusesToGuess(t *testing.T) {
	system := newTestSystemSettings()
	reg := &Registry{ModelServiceGroups: []ModelServiceGroup{{ID: "default"}, {ID: "a"}, {ID: "b"}}}
	if err := SaveRegistry(context.Background(), system, reg); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveTokenBankServiceGroup(context.Background(), system, "")
	if err != nil || got != "" {
		t.Fatalf("ambiguous resolve = %q err=%v, want empty", got, err)
	}
	got, err = ResolveTokenBankServiceGroup(context.Background(), system, "missing")
	if !errors.Is(err, ErrTokenBankServiceGroupMissing) {
		t.Fatalf("configured missing error = %v", err)
	}
}

func TestSaveRegistryKeepsTokenBankGrantMissingFromStaleSnapshot(t *testing.T) {
	ctx := context.Background()
	system := newTestSystemSettings()
	tokenBankRegistryWithGroup(t, system, "paid")
	if _, _, err := IssueTokenBankGrant(ctx, system, "user-1", "owner@example.com", "paid", "req-1", 2_500_000); err != nil {
		t.Fatal(err)
	}
	current, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatal(err)
	}
	current.Grants = append(current.Grants, Grant{
		ID: "card-old", Email: "owner@example.com", ServiceGroupID: "paid", Source: "card", CreditsTotal: 4,
	})
	if err := SaveRegistry(ctx, system, current); err != nil {
		t.Fatal(err)
	}
	stale, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatal(err)
	}
	stale.Grants = []Grant{{
		ID: "card-new", Email: "owner@example.com", ServiceGroupID: "paid", Source: "card", CreditsTotal: 3,
	}}
	if err := SaveRegistry(ctx, system, stale); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatal(err)
	}
	var bank, oldCard, newCard int
	for _, grant := range loaded.Grants {
		switch grant.ID {
		case "card-old":
			oldCard++
		case "card-new":
			newCard++
		default:
			if grant.Source == TokenBankGrantSource {
				bank++
				if grant.CardID != "tbk:req-1" || grant.CreditsTotal != 2.5 {
					t.Fatalf("token-bank grant = %+v", grant)
				}
			}
		}
	}
	if len(loaded.Grants) != 2 || bank != 1 || oldCard != 0 || newCard != 1 {
		t.Fatalf("grants = %+v, want the token-bank grant kept, the old card dropped, and the new card kept", loaded.Grants)
	}
}

func TestSaveRegistryKeepsHigherTokenBankCreditsUsed(t *testing.T) {
	ctx := context.Background()
	system := newTestSystemSettings()
	tokenBankRegistryWithGroup(t, system, "paid")
	if _, _, err := IssueTokenBankGrant(ctx, system, "user-1", "owner@example.com", "paid", "req-1", 2_500_000); err != nil {
		t.Fatal(err)
	}
	current, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatal(err)
	}
	if len(current.Grants) != 1 {
		t.Fatalf("grants = %+v, want the token-bank grant", current.Grants)
	}
	current.Grants[0].CreditsUsed = 1
	if err := SaveRegistry(ctx, system, current); err != nil {
		t.Fatal(err)
	}
	stale, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatal(err)
	}
	stale.Grants[0].CreditsUsed = 0
	if err := SaveRegistry(ctx, system, stale); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Grants) != 1 || loaded.Grants[0].CreditsUsed != 1 {
		t.Fatalf("credits used = %+v, want 1", loaded.Grants)
	}
}

func TestUserHasAvailableCreditsIncludesNewUserWindow(t *testing.T) {
	now := time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)
	started := now.Add(-time.Hour)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{
			{ID: "system-free", Name: "System Free", AccessPolicy: AccessPolicyFree},
			{ID: "paid", Name: "Paid", AccessPolicy: AccessPolicyGrantRequired},
		},
		DefaultNewUserLimitCard: NewUserLimitCard{
			ServiceGroupIDs: []string{"system-free"},
			PeriodLimits:    CreditPeriodLimits{FiveHour: 1000, Daily: 2000},
		},
		Grants: []Grant{{
			ID: "welcome", UserID: "user-1", Email: "owner@example.com",
			ServiceGroupID: "system-free", Source: "new_user_limit_card",
			StartsAt: started, CreatedAt: started,
		}},
	}
	if !UserHasAvailableCredits(context.Background(), reg, "user-1", "owner@example.com", now) {
		t.Fatal("new-user window with remaining quota must count as available credits")
	}
	if UserHasAvailableCredits(context.Background(), reg, "other", "other@example.com", now) {
		t.Fatal("another user's window must not count")
	}
	reg.Grants[0].PeriodUsage.FiveHour = GrantUsageWindow{WindowStart: now.Add(-time.Minute), CreditsUsed: 1000}
	if UserHasAvailableCredits(context.Background(), reg, "user-1", "owner@example.com", now) {
		t.Fatal("exhausted new-user window must not count as available credits")
	}
	reg.Grants = append(reg.Grants, Grant{
		ID: "bank", UserID: "user-1", Email: "owner@example.com",
		ServiceGroupID: "paid", Source: TokenBankGrantSource,
		StartsAt: started, ExpiresAt: tokenBankGrantExpiresAt, Permanent: true,
		CreditsTotal: 106.66,
	})
	if !UserHasAvailableCredits(context.Background(), reg, "user-1", "owner@example.com", now) {
		t.Fatal("token-bank grant balance must count as available credits")
	}
}

func TestUserHasAvailableCreditsIgnoresHoldsOnAnotherGroup(t *testing.T) {
	now := time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)
	started := now.Add(-time.Hour)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{
			{ID: "system-free", Name: "System Free", AccessPolicy: AccessPolicyFree},
			{ID: "paid", Name: "Paid", AccessPolicy: AccessPolicyGrantRequired},
		},
		DefaultNewUserLimitCard: NewUserLimitCard{
			ServiceGroupIDs: []string{"system-free"},
			PeriodLimits:    CreditPeriodLimits{FiveHour: 1000, Daily: 2000},
		},
		Grants: []Grant{{
			ID: "welcome", UserID: "user-1", Email: "owner@example.com",
			ServiceGroupID: "system-free", Source: "new_user_limit_card",
			StartsAt: started, CreatedAt: started,
		}},
		BillingReservations: []BillingReservation{{
			RequestID: "req-hold", UserID: "user-1", Email: "owner@example.com",
			ServiceGroupIDs: []string{"paid"}, Credits: 5000,
			ExpiresAt: now.Add(time.Hour), CreatedAt: now,
		}},
	}
	if !UserHasAvailableCredits(context.Background(), reg, "user-1", "owner@example.com", now) {
		t.Fatal("a hold on another group must not hide the new-user window")
	}
	reg.BillingReservations[0].ServiceGroupIDs = []string{"system-free"}
	reg.BillingReservations[0].Credits = 1000
	if !UserHasAvailableCredits(context.Background(), reg, "user-1", "owner@example.com", now) {
		t.Fatal("an in-flight hold must not withdraw token-bank credits before usage is recorded")
	}
	if got := AvailableCreditsForServiceGroupsForUserID(reg, "user-1", "owner@example.com", []string{"system-free"}, now); got != 0 {
		t.Fatalf("admission available = %v, want 0 while the window is fully held", got)
	}
}

func TestUserHasAvailableCreditsIgnoresDeletedGroupsAndKeepsFreeRoutes(t *testing.T) {
	now := time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)
	started := now.Add(-time.Hour)
	orphan := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "paid", AccessPolicy: AccessPolicyGrantRequired}},
		Grants: []Grant{{
			ID: "orphan", UserID: "user-1", Email: "owner@example.com",
			ServiceGroupID: "gone", Source: "card",
			StartsAt: started, ExpiresAt: started.Add(24 * time.Hour),
			CreditsTotal: 50,
		}},
	}
	if UserHasAvailableCredits(context.Background(), orphan, "user-1", "owner@example.com", now) {
		t.Fatal("credits on a deleted service group must not block a token-bank pull")
	}
	free := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "system-free", AccessPolicy: AccessPolicyFree}},
		UserBindings: []UserBinding{{
			UserID: "user-1", Email: "owner@example.com", ServiceGroupIDs: []string{"system-free"},
		}},
	}
	if !UserHasAvailableCredits(context.Background(), free, "user-1", "owner@example.com", now) {
		t.Fatal("an unrestricted free route must count as still usable")
	}
	unlimited := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "paid", AccessPolicy: AccessPolicyGrantRequired}},
		Grants: []Grant{{
			ID: "gift", UserID: "user-1", Email: "owner@example.com",
			ServiceGroupID: "paid", Source: "gift",
			StartsAt: started, ExpiresAt: started.Add(24 * time.Hour),
		}},
	}
	if !UserHasAvailableCredits(context.Background(), unlimited, "user-1", "owner@example.com", now) {
		t.Fatal("an unlimited grant must count as still usable")
	}
}

func TestUserHasAvailableCreditsIgnoresBuiltinFallbackGroup(t *testing.T) {
	now := time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)
	started := now.Add(-time.Hour)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "paid", Name: "Paid", AccessPolicy: AccessPolicyGrantRequired}},
	}
	reg.Normalize()
	if !IsBuiltinModelServiceGroupID(reg.DefaultNewUserServiceGroups[0]) {
		t.Fatalf("default new-user groups = %#v, want the builtin fallback", reg.DefaultNewUserServiceGroups)
	}
	if UserHasAvailableCredits(context.Background(), reg, "user-1", "owner@example.com", now) {
		t.Fatal("the builtin fallback group must not count as a usable free route")
	}

	reg.ModelServiceGroups = append(reg.ModelServiceGroups, ModelServiceGroup{
		ID: "system-free", Name: "System Free", AccessPolicy: AccessPolicyFree,
	})
	reg.DefaultNewUserLimitCard = NewUserLimitCard{
		ServiceGroupIDs: []string{"system-free"},
		PeriodLimits:    CreditPeriodLimits{FiveHour: 1000, Daily: 2000},
	}
	reg.Grants = []Grant{{
		ID: "welcome", UserID: "user-1", Email: "owner@example.com",
		ServiceGroupID: "system-free", Source: "new_user_limit_card",
		StartsAt: started, CreatedAt: started,
	}}
	reg.Normalize()
	if !UserHasAvailableCredits(context.Background(), reg, "user-1", "owner@example.com", now) {
		t.Fatal("a remaining welcome window must still block a pull when the builtin fallback is present")
	}
	reg.Grants[0].PeriodUsage.FiveHour = GrantUsageWindow{WindowStart: now.Add(-time.Minute), CreditsUsed: 1000}
	if UserHasAvailableCredits(context.Background(), reg, "user-1", "owner@example.com", now) {
		t.Fatal("an exhausted welcome window must pull even though the builtin fallback is free")
	}

	fallbackBalance := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "paid", Name: "Paid", AccessPolicy: AccessPolicyGrantRequired}},
		Grants: []Grant{{
			ID: "stranded", UserID: "user-1", Email: "owner@example.com",
			ServiceGroupID: DefaultModelServiceGroupID, Source: "new_user_default",
			StartsAt: started, ExpiresAt: started.Add(30 * 24 * time.Hour),
			CreditsTotal: 300,
		}},
	}
	fallbackBalance.Normalize()
	if UserHasAvailableCredits(context.Background(), fallbackBalance, "user-1", "owner@example.com", now) {
		t.Fatal("credits on the builtin fallback group must not block a token-bank pull")
	}
}

func TestUserHasAvailableCreditsDoesNotEarlyStartAcrossGroups(t *testing.T) {
	now := time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)
	started := now.Add(-time.Hour)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{
			{ID: "system-free", Name: "System Free", AccessPolicy: AccessPolicyFree},
			{ID: "paid", Name: "Paid", AccessPolicy: AccessPolicyGrantRequired},
		},
		DefaultNewUserLimitCard: NewUserLimitCard{
			ServiceGroupIDs: []string{"system-free"},
			PeriodLimits:    CreditPeriodLimits{FiveHour: 1000, Daily: 2000},
		},
		Grants: []Grant{
			{
				ID: "welcome", UserID: "user-1", Email: "owner@example.com",
				ServiceGroupID: "system-free", Source: "new_user_limit_card",
				StartsAt: started, CreatedAt: started,
				PeriodUsage: CreditPeriodUsage{FiveHour: GrantUsageWindow{WindowStart: now.Add(-time.Minute), CreditsUsed: 1000}},
			},
			{
				ID: "queued-paid", UserID: "user-1", Email: "owner@example.com",
				ServiceGroupID: "paid", Source: "card",
				StartsAt: now.Add(24 * time.Hour), ExpiresAt: now.Add(48 * time.Hour),
				CreditsTotal: 100,
			},
		},
	}
	if UserHasAvailableCredits(context.Background(), reg, "user-1", "owner@example.com", now) {
		t.Fatal("a future grant on another group must not count while the welcome window is exhausted")
	}

	sameGroup := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "paid", Name: "Paid", AccessPolicy: AccessPolicyGrantRequired}},
		Grants: []Grant{
			{
				ID: "spent", UserID: "user-1", Email: "owner@example.com",
				ServiceGroupID: "paid", Source: "card",
				StartsAt: started, ExpiresAt: started.Add(48 * time.Hour),
				CreditsTotal: 10, CreditsUsed: 10,
			},
			{
				ID: "queued-paid", UserID: "user-1", Email: "owner@example.com",
				ServiceGroupID: "paid", Source: "card",
				StartsAt: now.Add(24 * time.Hour), ExpiresAt: now.Add(48 * time.Hour),
				CreditsTotal: 100,
			},
		},
	}
	if !UserHasAvailableCredits(context.Background(), sameGroup, "user-1", "owner@example.com", now) {
		t.Fatal("a future grant that billing would start on this group must still count")
	}
}

func TestUserHasAvailableCreditsKeepsQueuedUnmeteredUnlimited(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 30, 0, 0, time.UTC)
	startsAt := now.Add(2 * time.Hour)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID: "coding-basic", Name: "Coding Basic", AccessPolicy: AccessPolicyGrantRequired,
		}},
		Grants: []Grant{{
			ID: "spent", Email: "user@example.com", ServiceGroupID: "coding-basic", Source: "card",
			StartsAt: now.Add(-24 * time.Hour), ExpiresAt: now.Add(24 * time.Hour),
			CreditsTotal: 100, CreditsUsed: 100,
		}, {
			ID: "queued-unlimited", Email: "user@example.com", ServiceGroupID: "coding-basic", Source: "admin",
			StartsAt: startsAt, ExpiresAt: startsAt.Add(30 * 24 * time.Hour),
		}},
	}
	if !UserHasAvailableCredits(context.Background(), reg, "", "user@example.com", now) {
		t.Fatal("a queued unlimited grant that billing would start must count as still usable")
	}

	reg.Grants[0].CreditsTotal = 5000
	reg.Grants[0].CreditsUsed = 10
	reg.Grants[0].PeriodLimits = CreditPeriodLimits{FiveHour: 100}
	reg.Grants[0].PeriodUsage = CreditPeriodUsage{FiveHour: GrantUsageWindow{WindowStart: fiveHourWindowStart(now), CreditsUsed: 100}}
	if UserHasAvailableCredits(context.Background(), reg, "", "user@example.com", now) {
		t.Fatal("a period limit must not turn a queued unlimited grant into available service")
	}
}
