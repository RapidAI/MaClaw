package llmservice

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func TestGenerateCardCodeFormat(t *testing.T) {
	seen := map[string]struct{}{}
	for i := 0; i < 200; i++ {
		code, err := GenerateCardCode()
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateCardCode(code); err != nil {
			t.Fatalf("ValidateCardCode(%q) error = %v", code, err)
		}
		if len(code) != CardCodeLength {
			t.Fatalf("len(%q) = %d, want %d", code, len(code), CardCodeLength)
		}
		if code != strings.ToUpper(code) {
			t.Fatalf("expected uppercase code, got %q", code)
		}
		if _, exists := seen[code]; exists {
			t.Fatalf("duplicate generated code: %q", code)
		}
		seen[code] = struct{}{}
	}
}

func TestValidateCardCodeRejectsInvalidFormat(t *testing.T) {
	invalid := []string{"", "ABC", "abc123", "1234567890123456789-", "1234567890123456789_", "123456789012345678901"}
	for _, code := range invalid {
		if err := ValidateCardCode(code); err == nil {
			t.Fatalf("expected invalid code %q to fail validation", code)
		}
	}
}

func TestEstimateCreditsWithFloorAppliesMinimumForTinySuccessfulRequests(t *testing.T) {
	tests := []struct {
		name       string
		tokens     int64
		multiplier float64
		want       float64
	}{
		{name: "missing usage", tokens: 0, multiplier: 1, want: MinimumRequestCredits},
		{name: "tiny usage", tokens: 100, multiplier: 1, want: MinimumRequestCredits},
		{name: "normal usage", tokens: 2000, multiplier: 1, want: 0.2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EstimateCreditsWithFloor(tt.tokens, tt.multiplier, DefaultTokensPerCredit); got != tt.want {
				t.Fatalf("EstimateCreditsWithFloor() = %.3f, want %.3f", got, tt.want)
			}
		})
	}
}

type testSystemSettings struct {
	data map[string]string
}

func newTestSystemSettings() *testSystemSettings {
	return &testSystemSettings{data: map[string]string{}}
}

func (s *testSystemSettings) Set(_ context.Context, key, valueJSON string) error {
	s.data[key] = valueJSON
	return nil
}

func (s *testSystemSettings) Get(_ context.Context, key string) (string, error) {
	return s.data[key], nil
}

type testUserRepository struct {
	byTenantEmail map[string]*store.User
	byTenantPhone map[string]*store.User
}

func (r *testUserRepository) Create(context.Context, *store.User) error { return nil }
func (r *testUserRepository) GetByID(context.Context, string) (*store.User, error) {
	return nil, nil
}
func (r *testUserRepository) GetByEmail(context.Context, string) (*store.User, error) {
	return nil, nil
}
func (r *testUserRepository) GetByTenantEmail(_ context.Context, tenantID, email string) (*store.User, error) {
	return r.byTenantEmail[store.NormalizeTenantID(tenantID)+"|"+normalizeEmail(email)], nil
}
func (r *testUserRepository) GetByTenantIdentity(_ context.Context, tenantID, identityType, value string) (*store.User, error) {
	if identityType != "phone" {
		return nil, nil
	}
	return r.byTenantPhone[store.NormalizeTenantID(tenantID)+"|"+strings.TrimSpace(value)], nil
}
func (r *testUserRepository) ListIdentitiesByUser(context.Context, string, string) ([]*store.UserIdentity, error) {
	return nil, nil
}
func (r *testUserRepository) UpsertIdentity(context.Context, *store.UserIdentity) error { return nil }
func (r *testUserRepository) List(context.Context) ([]*store.User, error)               { return nil, nil }
func (r *testUserRepository) ListByTenant(context.Context, string) ([]*store.User, error) {
	return nil, nil
}
func (r *testUserRepository) DeleteByEmail(context.Context, string) error { return nil }
func (r *testUserRepository) DeleteByTenantEmail(context.Context, string, string) error {
	return nil
}
func (r *testUserRepository) UpdateSmartRoute(context.Context, string, bool) error { return nil }
func (r *testUserRepository) MarkEmailVerified(context.Context, string, string) error {
	return nil
}

func TestBackfillRegistryUserIDsMigratesLegacyEmailAndPhoneRecords(t *testing.T) {
	ctx := context.Background()
	system := newTestSystemSettings()
	redeemedAt := time.Now().UTC()
	reg := &Registry{
		UserBindings: []UserBinding{{Email: "buyer@example.com", ServiceGroupIDs: []string{"coding-basic"}}},
		Grants: []Grant{
			{ID: "grant-email", Email: "buyer@example.com", ServiceGroupID: "coding-basic", Source: "card", StartsAt: redeemedAt.Add(-time.Hour), ExpiresAt: redeemedAt.Add(time.Hour), CreatedAt: redeemedAt},
			{ID: "grant-phone", Email: "phone:19900001112", ServiceGroupID: "coding-basic", Source: "card", StartsAt: redeemedAt.Add(-time.Hour), ExpiresAt: redeemedAt.Add(time.Hour), CreatedAt: redeemedAt},
		},
		Cards: []RechargeCard{{ID: "card-email", RedeemedByEmail: "buyer@example.com", RedeemedAt: &redeemedAt}, {ID: "card-phone", RedeemedByEmail: "phone:19900001112", RedeemedAt: &redeemedAt}},
	}
	if err := SaveRegistry(ctx, system, reg); err != nil {
		t.Fatalf("SaveRegistry() error = %v", err)
	}
	users := &testUserRepository{
		byTenantEmail: map[string]*store.User{
			store.DefaultTenantID + "|buyer@example.com": {ID: "user-email", TenantID: store.DefaultTenantID, Email: "buyer@example.com"},
		},
		byTenantPhone: map[string]*store.User{
			store.DefaultTenantID + "|19900001112": {ID: "user-phone", TenantID: store.DefaultTenantID, Email: "legacy@example.com"},
		},
	}
	changed, err := BackfillRegistryUserIDs(ctx, system, users, store.DefaultTenantID)
	if err != nil {
		t.Fatalf("BackfillRegistryUserIDs() error = %v", err)
	}
	if !changed {
		t.Fatal("expected registry to change")
	}
	got, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}
	if got.UserBindings[0].UserID != "user-email" || got.Grants[0].UserID != "user-email" || got.Cards[0].RedeemedByUserID != "user-email" {
		t.Fatalf("email records not backfilled: %#v", got)
	}
	if got.Grants[1].UserID != "user-phone" || got.Cards[1].RedeemedByUserID != "user-phone" {
		t.Fatalf("phone records not backfilled: %#v", got)
	}
}

func TestPurgeUserFromRegistryForUserRemovesCanonicalAndLegacyRecords(t *testing.T) {
	ctx := context.Background()
	system := newTestSystemSettings()
	redeemedAt := time.Now().UTC()
	if err := SaveRegistry(ctx, system, &Registry{
		UserBindings: []UserBinding{
			{UserID: "user-123", Email: "old@example.com", ServiceGroupIDs: []string{"coding-basic"}},
			{Email: "phone:19900001112", ServiceGroupIDs: []string{"coding-basic"}},
			{UserID: "other-user", Email: "other@example.com", ServiceGroupIDs: []string{"coding-basic"}},
		},
		Grants: []Grant{
			{ID: "grant-user-id", UserID: "user-123", Email: "old@example.com", ServiceGroupID: "coding-basic"},
			{ID: "grant-phone", Email: "phone:19900001112", ServiceGroupID: "coding-basic"},
			{ID: "grant-other", UserID: "other-user", Email: "other@example.com", ServiceGroupID: "coding-basic"},
		},
		Cards: []RechargeCard{
			{ID: "card-user-id", RedeemedByUserID: "user-123", RedeemedByEmail: "old@example.com", RedeemedAt: &redeemedAt},
			{ID: "card-phone", RedeemedByEmail: "phone:19900001112", RedeemedAt: &redeemedAt},
			{ID: "card-other", RedeemedByUserID: "other-user", RedeemedByEmail: "other@example.com", RedeemedAt: &redeemedAt},
		},
	}); err != nil {
		t.Fatalf("SaveRegistry() error = %v", err)
	}

	if err := PurgeUserFromRegistryForUser(ctx, system, "user-123", "phone:19900001112"); err != nil {
		t.Fatalf("PurgeUserFromRegistryForUser() error = %v", err)
	}
	got, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}
	if len(got.UserBindings) != 1 || got.UserBindings[0].UserID != "other-user" {
		t.Fatalf("unexpected user bindings after purge: %#v", got.UserBindings)
	}
	if len(got.Grants) != 1 || got.Grants[0].UserID != "other-user" {
		t.Fatalf("unexpected grants after purge: %#v", got.Grants)
	}
	if len(got.Cards) != 1 || got.Cards[0].RedeemedByUserID != "other-user" {
		t.Fatalf("unexpected cards after purge: %#v", got.Cards)
	}
}

func TestPurgeUserFromRegistryExceptReferralBenefitsForUserRetainsFrozenReferralGrant(t *testing.T) {
	ctx := context.Background()
	system := newTestSystemSettings()
	if err := SaveRegistry(ctx, system, &Registry{
		UserBindings: []UserBinding{{UserID: "deleted-user", Email: "deleted@example.com", ServiceGroupIDs: []string{"coding-basic"}}},
		Grants: []Grant{
			{ID: "ordinary-grant", UserID: "deleted-user", Email: "deleted@example.com", ServiceGroupID: "coding-basic"},
			{ID: "referral-grant", UserID: "deleted-user", Email: "deleted@example.com", ServiceGroupID: "coding-basic", Source: "user_referral", CardID: "referral-1", Frozen: true, CreditsTotal: 10},
		},
		Cards: []RechargeCard{{ID: "redeemed-card", RedeemedByUserID: "deleted-user", RedeemedByEmail: "deleted@example.com"}},
	}); err != nil {
		t.Fatalf("SaveRegistry() error = %v", err)
	}

	if err := PurgeUserFromRegistryExceptReferralBenefitsForUser(ctx, system, "deleted-user", "deleted@example.com"); err != nil {
		t.Fatalf("PurgeUserFromRegistryExceptReferralBenefitsForUser() error = %v", err)
	}
	got, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}
	if len(got.UserBindings) != 0 || len(got.Cards) != 0 {
		t.Fatalf("ordinary user state must be removed: %#v", got)
	}
	if len(got.Grants) != 1 || got.Grants[0].ID != "referral-grant" || !got.Grants[0].Frozen {
		t.Fatalf("frozen referral grant must remain as audit evidence: %#v", got.Grants)
	}
}

func TestRedeemCardForUserIDPersistsCanonicalUserID(t *testing.T) {
	ctx := context.Background()
	system := newTestSystemSettings()
	code := strings.Repeat("A", CardCodeLength)
	if err := SaveRegistry(ctx, system, &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "coding-basic", Name: "Coding", AccessPolicy: AccessPolicyGrantRequired}},
		Cards: []RechargeCard{{
			ID:              "card-user-id",
			CodeHash:        HashCode(code),
			ServiceGroupIDs: []string{"coding-basic"},
			DurationDays:    30,
			Credits:         10,
			CreatedAt:       time.Now().UTC(),
		}},
	}); err != nil {
		t.Fatalf("SaveRegistry() error = %v", err)
	}
	if _, err := RedeemCardForUserID(ctx, system, nil, "user-123", "phone:19900001112", code, "http://hub.test/api/llm/v1"); err != nil {
		t.Fatalf("RedeemCardForUserID() error = %v", err)
	}
	reg, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}
	if len(reg.Grants) != 1 || reg.Grants[0].UserID != "user-123" || reg.Grants[0].Email != "phone:19900001112" {
		t.Fatalf("grant user identity not persisted: %#v", reg.Grants)
	}
	if len(reg.Cards) != 1 || reg.Cards[0].RedeemedByUserID != "user-123" || reg.Cards[0].RedeemedByEmail != "phone:19900001112" {
		t.Fatalf("card redeemed user identity not persisted: %#v", reg.Cards)
	}
}

func TestRedeemCardForUserIDPreservesLegacyAccountWhenPassedAsUserID(t *testing.T) {
	ctx := context.Background()
	system := newTestSystemSettings()
	code := strings.Repeat("B", CardCodeLength)
	if err := SaveRegistry(ctx, system, &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "coding-basic", Name: "Coding", AccessPolicy: AccessPolicyGrantRequired}},
		Cards: []RechargeCard{{
			ID:              "card-legacy-phone",
			CodeHash:        HashCode(code),
			ServiceGroupIDs: []string{"coding-basic"},
			DurationDays:    30,
			Credits:         10,
			CreatedAt:       time.Now().UTC(),
		}},
	}); err != nil {
		t.Fatalf("SaveRegistry() error = %v", err)
	}
	if _, err := RedeemCardForUserID(ctx, system, nil, "phone:19900001112", "", code, "http://hub.test/api/llm/v1"); err != nil {
		t.Fatalf("RedeemCardForUserID() error = %v", err)
	}
	reg, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}
	if len(reg.Grants) != 1 || reg.Grants[0].UserID != "" || reg.Grants[0].Email != "phone:19900001112" {
		t.Fatalf("legacy phone account not persisted on grant: %#v", reg.Grants)
	}
	if len(reg.Cards) != 1 || reg.Cards[0].RedeemedByUserID != "" || reg.Cards[0].RedeemedByEmail != "phone:19900001112" {
		t.Fatalf("legacy phone account not persisted on card: %#v", reg.Cards)
	}
}

func TestApplyCreditUsageToRegistryForUserIDIgnoresStaleEmail(t *testing.T) {
	now := time.Now().UTC()
	reg := &Registry{Grants: []Grant{{
		ID:             "grant-user-id",
		UserID:         "user-123",
		Email:          "old@example.com",
		ServiceGroupID: "coding-basic",
		Source:         "card",
		StartsAt:       now.Add(-time.Hour),
		ExpiresAt:      now.Add(time.Hour),
		CreatedAt:      now.Add(-time.Hour),
		CreditsTotal:   10,
	}}}
	used := ApplyCreditUsageToRegistryForUserID(reg, "user-123", "new@example.com", []string{"coding-basic"}, 3, now)
	if used != 3 {
		t.Fatalf("used = %.3f, want 3", used)
	}
	if reg.Grants[0].CreditsUsed != 3 {
		t.Fatalf("CreditsUsed = %.3f, want 3", reg.Grants[0].CreditsUsed)
	}
}

func TestApplyCreditUsageSkipsFrozenReferralGrant(t *testing.T) {
	now := time.Now().UTC()
	reg := &Registry{Grants: []Grant{
		{ID: "frozen-referral", UserID: "user-1", Email: "user@example.com", ServiceGroupID: "coding", Source: "user_referral", CardID: "referral-1", StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), CreditsTotal: 10, Frozen: true},
		{ID: "active-card", UserID: "user-1", Email: "user@example.com", ServiceGroupID: "coding", Source: "card", StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), CreditsTotal: 10},
	}}
	used := ApplyCreditUsageToRegistryForUserID(reg, "user-1", "user@example.com", []string{"coding"}, 4, now)
	if used != 4 || reg.Grants[0].CreditsUsed != 0 || reg.Grants[1].CreditsUsed != 4 {
		t.Fatalf("frozen grant must remain unused: used=%v grants=%#v", used, reg.Grants)
	}
}

func TestApplyCreditUsageConsumesReferralRewardsFIFOByIssueTime(t *testing.T) {
	now := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	reg := &Registry{Grants: []Grant{
		// The older reward intentionally has the later expiry. It must still be
		// charged first, because every referral reward owns its own expiry window.
		{ID: "referral-old", UserID: "user-1", Email: "user@example.com", ServiceGroupID: "coding", Source: "user_referral", CardID: "referral-1", StartsAt: now.Add(-48 * time.Hour), CreatedAt: now.Add(-48 * time.Hour), ExpiresAt: now.Add(30 * 24 * time.Hour), CreditsTotal: 5},
		{ID: "referral-new", UserID: "user-1", Email: "user@example.com", ServiceGroupID: "coding", Source: "user_referral", CardID: "referral-2", StartsAt: now.Add(-24 * time.Hour), CreatedAt: now.Add(-24 * time.Hour), ExpiresAt: now.Add(24 * time.Hour), CreditsTotal: 5},
	}}
	if got := ApplyCreditUsageToRegistryForUserID(reg, "user-1", "user@example.com", []string{"coding"}, 6, now); got != 6 {
		t.Fatalf("used = %v, want 6", got)
	}
	if reg.Grants[0].CreditsUsed != 5 || reg.Grants[1].CreditsUsed != 1 {
		t.Fatalf("referral FIFO was not preserved: %#v", reg.Grants)
	}
}

func TestApplyCreditUsageKeepsExistingNonReferralExpiryOrder(t *testing.T) {
	now := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	reg := &Registry{Grants: []Grant{
		{ID: "card-older", Email: "user@example.com", ServiceGroupID: "coding", Source: "card", StartsAt: now.Add(-48 * time.Hour), CreatedAt: now.Add(-48 * time.Hour), ExpiresAt: now.Add(30 * 24 * time.Hour), CreditsTotal: 5},
		{ID: "card-expiring-first", Email: "user@example.com", ServiceGroupID: "coding", Source: "card", StartsAt: now.Add(-24 * time.Hour), CreatedAt: now.Add(-24 * time.Hour), ExpiresAt: now.Add(24 * time.Hour), CreditsTotal: 5},
	}}
	if got := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"coding"}, 2, now); got != 2 {
		t.Fatalf("used = %v, want 2", got)
	}
	if reg.Grants[0].CreditsUsed != 0 || reg.Grants[1].CreditsUsed != 2 {
		t.Fatalf("non-referral ordering changed: %#v", reg.Grants)
	}
}

func TestFrozenReferralGrantIsExcludedFromAllBillingEligibility(t *testing.T) {
	now := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "coding", AccessPolicy: AccessPolicyGrantRequired}},
		Grants: []Grant{{
			ID:             "revoked-referral",
			UserID:         "user-1",
			Email:          "user@example.com",
			ServiceGroupID: "coding",
			Source:         "user_referral",
			CardID:         "referral-1",
			StartsAt:       now.Add(-time.Hour),
			ExpiresAt:      now.Add(30 * 24 * time.Hour),
			CreditsTotal:   10,
			Frozen:         true,
		}},
	}
	reg.Normalize()

	if got := AvailableCreditsForServiceGroupsForUserID(reg, "user-1", "user@example.com", []string{"coding"}, now); got != 0 {
		t.Fatalf("available frozen referral credits = %v, want 0", got)
	}
	if HasAnyGrantForServiceGroups(reg, "user@example.com", []string{"coding"}) {
		t.Fatal("frozen referral grant must not count as any grant")
	}
	if HasActiveGrantForServiceGroups(reg, "user@example.com", []string{"coding"}, now) {
		t.Fatal("frozen referral grant must not count as active grant")
	}
	if got := GrantStartAtForServiceGroupsForUserID(reg, "user-1", "user@example.com", []string{"coding"}, now); got != nil {
		t.Fatalf("frozen referral grant start = %v, want nil", got)
	}
	allowed, policy, code, _, credits, active, any := BillingEligibilityForServiceGroupsForUserID(reg, "user-1", "user@example.com", []string{"coding"}, now)
	if allowed || policy != AccessPolicyGrantRequired || code != "LLM_SERVICE_CREDITS_REQUIRED" || credits != 0 || active || any {
		t.Fatalf("frozen referral eligibility leaked: allowed=%v policy=%q code=%q credits=%v active=%v any=%v", allowed, policy, code, credits, active, any)
	}
}

func TestFrozenReferralGrantDoesNotDelayNewGrant(t *testing.T) {
	now := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	reg := &Registry{Grants: []Grant{{
		ID:             "revoked-referral",
		Email:          "user@example.com",
		ServiceGroupID: "coding",
		Source:         "user_referral",
		StartsAt:       now.Add(-time.Hour),
		ExpiresAt:      now.Add(30 * 24 * time.Hour),
		CreditsTotal:   10,
		Frozen:         true,
	}}}
	if got := nextGrantStart(reg, newUserAccountRef("", "user@example.com"), "coding", now); !got.Equal(now) {
		t.Fatalf("next grant after frozen referral = %v, want %v", got, now)
	}
}

func TestFreezeUserReferralBenefits(t *testing.T) {
	ctx := context.Background()
	system := newTestSystemSettings()
	if err := SaveRegistry(ctx, system, &Registry{Grants: []Grant{{ID: "referral-grant", Email: "user@example.com", ServiceGroupID: "coding", Source: "user_referral", CardID: "referral-1"}, {ID: "card-grant", Email: "user@example.com", ServiceGroupID: "coding", Source: "card", CardID: "referral-1"}}}); err != nil {
		t.Fatal(err)
	}
	if err := FreezeUserReferralBenefits(ctx, system, "referral-1"); err != nil {
		t.Fatal(err)
	}
	reg, err := LoadRegistry(ctx, system)
	if err != nil || !reg.Grants[0].Frozen || reg.Grants[1].Frozen {
		t.Fatalf("unexpected frozen grants: reg=%#v err=%v", reg, err)
	}
}

func TestGrantDefaultServiceForNewUser(t *testing.T) {
	ctx := context.Background()
	system := newTestSystemSettings()
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:   "coding-basic",
			Name: "Coding Basic",
			Models: []ModelServiceModel{{
				Name:        "gpt-5",
				ProviderIDs: []string{"provider-a"},
			}},
		}},
		DefaultNewUserServiceGroups: []string{"coding-basic"},
		DefaultNewUserDurationDays:  7,
	}
	if err := SaveRegistry(ctx, system, reg); err != nil {
		t.Fatal(err)
	}

	if err := GrantDefaultServiceForNewUser(ctx, system, "newuser@example.com"); err != nil {
		t.Fatal(err)
	}

	saved, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Grants) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(saved.Grants))
	}
	grant := saved.Grants[0]
	if grant.Email != "newuser@example.com" {
		t.Fatalf("unexpected email: %q", grant.Email)
	}
	if grant.ServiceGroupID != "coding-basic" {
		t.Fatalf("unexpected service group: %q", grant.ServiceGroupID)
	}
	if grant.Source != "new_user_default" {
		t.Fatalf("unexpected grant source: %q", grant.Source)
	}
	duration := grant.ExpiresAt.Sub(grant.StartsAt)
	if duration < 7*24*time.Hour-time.Minute || duration > 7*24*time.Hour+time.Minute {
		t.Fatalf("unexpected duration: %s", duration)
	}
	if grant.CreditsTotal != 300 {
		t.Fatalf("expected initial 30%% credits grant 300, got %v", grant.CreditsTotal)
	}
}

func TestGrantInvitationCodeBenefitForUser(t *testing.T) {
	ctx := context.Background()
	system := newTestSystemSettings()
	if err := SaveRegistry(ctx, system, &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "invite-pro", Name: "Invite Pro"}},
	}); err != nil {
		t.Fatalf("SaveRegistry: %v", err)
	}

	if err := GrantInvitationCodeBenefitForUser(ctx, system, "user@example.com", "ic_invite_1", "invite-pro", 7, 1234.5678); err != nil {
		t.Fatalf("GrantInvitationCodeBenefitForUser: %v", err)
	}
	if err := GrantInvitationCodeBenefitForUser(ctx, system, "user@example.com", "ic_invite_1", "invite-pro", 7, 1234.5678); err != nil {
		t.Fatalf("GrantInvitationCodeBenefitForUser second call: %v", err)
	}

	saved, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if len(saved.Grants) != 1 {
		t.Fatalf("len(Grants) = %d, want 1", len(saved.Grants))
	}
	grant := saved.Grants[0]
	if grant.Email != "user@example.com" || grant.ServiceGroupID != "invite-pro" || grant.Source != "invitation_code" {
		t.Fatalf("unexpected grant identity: %#v", grant)
	}
	if grant.CardID != "ic_invite_1" {
		t.Fatalf("CardID = %q, want ic_invite_1", grant.CardID)
	}
	if grant.CreditsTotal != 1234.568 {
		t.Fatalf("CreditsTotal = %v, want rounded 1234.568", grant.CreditsTotal)
	}
	if got := grant.ExpiresAt.Sub(grant.StartsAt); got != 7*24*time.Hour {
		t.Fatalf("grant duration = %v, want 7 days", got)
	}
}

func TestGrantInvitationCodeBenefitForUserIDPersistsCanonicalUserID(t *testing.T) {
	ctx := context.Background()
	system := newTestSystemSettings()
	if err := SaveRegistry(ctx, system, &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "invite-pro", Name: "Invite Pro"}},
	}); err != nil {
		t.Fatalf("SaveRegistry: %v", err)
	}

	if err := GrantInvitationCodeBenefitForUserID(ctx, system, "user-123", "phone:19900001112", "ic_invite_1", "invite-pro", 7, 500); err != nil {
		t.Fatalf("GrantInvitationCodeBenefitForUserID: %v", err)
	}
	saved, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if len(saved.Grants) != 1 {
		t.Fatalf("len(Grants) = %d, want 1", len(saved.Grants))
	}
	grant := saved.Grants[0]
	if grant.UserID != "user-123" || grant.Email != "phone:19900001112" || grant.Source != "invitation_code" {
		t.Fatalf("unexpected invitation grant identity: %#v", grant)
	}
}

func TestGrantInvitationCodeBenefitForUserRequiresCompleteGrant(t *testing.T) {
	ctx := context.Background()
	system := newTestSystemSettings()
	if err := SaveRegistry(ctx, system, &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "invite-pro", Name: "Invite Pro"}},
	}); err != nil {
		t.Fatalf("SaveRegistry: %v", err)
	}

	if err := GrantInvitationCodeBenefitForUser(ctx, system, "user@example.com", "ic_invite_1", "invite-pro", 0, 1234); err != nil {
		t.Fatalf("GrantInvitationCodeBenefitForUser: %v", err)
	}
	if err := GrantInvitationCodeBenefitForUser(ctx, system, "user@example.com", "ic_invite_2", "invite-pro", 7, math.NaN()); err != nil {
		t.Fatalf("GrantInvitationCodeBenefitForUser with NaN credits: %v", err)
	}
	saved, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if len(saved.Grants) != 0 {
		t.Fatalf("len(Grants) = %d, want 0", len(saved.Grants))
	}
}

func TestGrantEmailConfirmedBenefitUsesRegistrationWindow(t *testing.T) {
	ctx := context.Background()
	system := newTestSystemSettings()
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:   "coding-basic",
			Name: "Coding Basic",
			Models: []ModelServiceModel{{
				Name:        "gpt-5",
				ProviderIDs: []string{"provider-a"},
			}},
		}},
		DefaultNewUserServiceGroups: []string{"coding-basic"},
		DefaultNewUserDurationDays:  7,
		DefaultNewUserCredits:       1000,
	}
	if err := SaveRegistry(ctx, system, reg); err != nil {
		t.Fatal(err)
	}
	if err := GrantDefaultServiceForNewUser(ctx, system, "newuser@example.com"); err != nil {
		t.Fatal(err)
	}
	before, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatal(err)
	}
	initial := before.Grants[0]

	if err := GrantEmailConfirmedBenefitForUser(ctx, system, "newuser@example.com"); err != nil {
		t.Fatal(err)
	}
	saved, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Grants) != 2 {
		t.Fatalf("expected 2 grants, got %d", len(saved.Grants))
	}
	confirmed := saved.Grants[1]
	if confirmed.Source != "new_user_email_confirmed" {
		t.Fatalf("unexpected source: %q", confirmed.Source)
	}
	if confirmed.CreditsTotal != 700 {
		t.Fatalf("expected email-confirmed 70%% credits grant 700, got %v", confirmed.CreditsTotal)
	}
	if !confirmed.StartsAt.Equal(initial.StartsAt) || !confirmed.ExpiresAt.Equal(initial.ExpiresAt) {
		t.Fatalf("confirmed grant should use registration window: initial=%s..%s confirmed=%s..%s", initial.StartsAt, initial.ExpiresAt, confirmed.StartsAt, confirmed.ExpiresAt)
	}
}

func TestGrantPhoneVerifiedBenefitForUserIDCompletesNewUserCredits(t *testing.T) {
	ctx := context.Background()
	system := newTestSystemSettings()
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:   "coding-basic",
			Name: "Coding Basic",
			Models: []ModelServiceModel{{
				Name:        "gpt-5",
				ProviderIDs: []string{"provider-a"},
			}},
		}},
		DefaultNewUserServiceGroups: []string{"coding-basic"},
		DefaultNewUserDurationDays:  7,
		DefaultNewUserCredits:       1000,
	}
	if err := SaveRegistry(ctx, system, reg); err != nil {
		t.Fatal(err)
	}
	if err := GrantDefaultServiceForNewUserID(ctx, system, "user-123", "phone:19900001112"); err != nil {
		t.Fatal(err)
	}
	if err := GrantPhoneVerifiedBenefitForUserID(ctx, system, "user-123", "phone:19900001112"); err != nil {
		t.Fatal(err)
	}
	saved, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Grants) != 2 {
		t.Fatalf("expected 2 grants, got %d", len(saved.Grants))
	}
	total := 0.0
	for _, grant := range saved.Grants {
		if grant.UserID != "user-123" || grant.Email != "phone:19900001112" {
			t.Fatalf("grant should persist canonical and phone identities: %#v", saved.Grants)
		}
		total += grant.CreditsTotal
	}
	if total != 1000 {
		t.Fatalf("expected phone registration credits to total 1000, got %v", total)
	}
}

func TestGrantEmailConfirmedBenefitRequiresRegistrationWindow(t *testing.T) {
	ctx := context.Background()
	system := newTestSystemSettings()
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:   "coding-basic",
			Name: "Coding Basic",
			Models: []ModelServiceModel{{
				Name:        "gpt-5",
				ProviderIDs: []string{"provider-a"},
			}},
		}},
		DefaultNewUserServiceGroups: []string{"coding-basic"},
		DefaultNewUserDurationDays:  7,
		DefaultNewUserCredits:       1000,
	}
	if err := SaveRegistry(ctx, system, reg); err != nil {
		t.Fatal(err)
	}

	if err := GrantEmailConfirmedBenefitForUser(ctx, system, "newuser@example.com"); err != nil {
		t.Fatal(err)
	}
	saved, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Grants) != 0 {
		t.Fatalf("expected no confirmed grant without registration grant, got %d", len(saved.Grants))
	}
}

func TestGrantEmailConfirmedBenefitDoesNotExtendExpiredRegistrationWindow(t *testing.T) {
	ctx := context.Background()
	system := newTestSystemSettings()
	now := time.Now().UTC()
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:   "coding-basic",
			Name: "Coding Basic",
			Models: []ModelServiceModel{{
				Name:        "gpt-5",
				ProviderIDs: []string{"provider-a"},
			}},
		}},
		DefaultNewUserServiceGroups: []string{"coding-basic"},
		DefaultNewUserDurationDays:  7,
		DefaultNewUserCredits:       1000,
		Grants: []Grant{{
			ID:             "grant_initial",
			Email:          "newuser@example.com",
			ServiceGroupID: "coding-basic",
			Source:         "new_user_default",
			StartsAt:       now.Add(-8 * 24 * time.Hour),
			ExpiresAt:      now.Add(-24 * time.Hour),
			CreatedAt:      now.Add(-8 * 24 * time.Hour),
			CreditsTotal:   300,
		}},
	}
	if err := SaveRegistry(ctx, system, reg); err != nil {
		t.Fatal(err)
	}

	if err := GrantEmailConfirmedBenefitForUser(ctx, system, "newuser@example.com"); err != nil {
		t.Fatal(err)
	}
	saved, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Grants) != 1 {
		t.Fatalf("expected no confirmed grant after registration benefit expired, got %d grants", len(saved.Grants))
	}
}

func TestGrantDefaultServiceForNewUserUsesNewUserDefaultsNotGlobalBinding(t *testing.T) {
	ctx := context.Background()
	system := newTestSystemSettings()
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{
			{ID: "global-svc", Name: "Global", AccessPolicy: AccessPolicyFree},
			{ID: "welcome-svc", Name: "Welcome", AccessPolicy: AccessPolicyFree},
		},
		GlobalServiceGroupIDs:       []string{"global-svc"},
		DefaultNewUserServiceGroups: []string{"welcome-svc"},
		DefaultNewUserDurationDays:  7,
	}
	if err := SaveRegistry(ctx, system, reg); err != nil {
		t.Fatal(err)
	}

	if err := GrantDefaultServiceForNewUser(ctx, system, "newuser@example.com"); err != nil {
		t.Fatal(err)
	}
	saved, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Grants) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(saved.Grants))
	}
	if saved.Grants[0].ServiceGroupID != "welcome-svc" {
		t.Fatalf("default grant service group = %q, want new-user default welcome-svc", saved.Grants[0].ServiceGroupID)
	}
}

func TestGrantDefaultServiceForNewUserIsIdempotent(t *testing.T) {
	ctx := context.Background()
	system := newTestSystemSettings()
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:   "coding-basic",
			Name: "Coding Basic",
			Models: []ModelServiceModel{{
				Name:        "gpt-5",
				ProviderIDs: []string{"provider-a"},
			}},
		}},
		DefaultNewUserServiceGroups: []string{"coding-basic"},
		DefaultNewUserDurationDays:  7,
	}
	if err := SaveRegistry(ctx, system, reg); err != nil {
		t.Fatal(err)
	}
	if err := GrantDefaultServiceForNewUser(ctx, system, "newuser@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := GrantDefaultServiceForNewUser(ctx, system, "newuser@example.com"); err != nil {
		t.Fatal(err)
	}
	saved, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, grant := range saved.Grants {
		if grant.Email == "newuser@example.com" && grant.ServiceGroupID == "coding-basic" && grant.Source == "new_user_default" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 default grant, got %d", count)
	}
}

func TestRegistryNormalizeDefaultsAccessPolicyToFree(t *testing.T) {
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:   "group-a",
			Name: "Group A",
		}},
	}
	reg.Normalize()
	group := reg.FindModelServiceGroup("group-a")
	if group == nil {
		t.Fatal("expected group-a to exist")
	}
	if group.AccessPolicy != AccessPolicyFree {
		t.Fatalf("expected access policy %q, got %q", AccessPolicyFree, group.AccessPolicy)
	}
}

func TestMaClawOfficialServiceGroupStillRequiresGrant(t *testing.T) {
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:           "redeem",
			Name:         "Redeem",
			AccessPolicy: AccessPolicyGrantRequired,
			Models: []ModelServiceModel{{
				Name:        "auto",
				ProviderIDs: []string{MaClawOfficialProviderID},
				ProviderConfigs: []ModelServiceProviderConfig{{
					ProviderID: MaClawOfficialProviderID,
				}},
			}},
		}},
		DefaultNewUserServiceGroups: []string{"redeem"},
	}

	status, models, err := ResolveStatusFromRegistry(context.Background(), reg, nil, "user@example.com", "https://hub.example/api/llm/v1")
	if err != nil {
		t.Fatalf("ResolveStatusFromRegistry: %v", err)
	}
	if status.Active {
		t.Fatalf("official service group should require a local grant: %+v", status)
	}
	if len(models) != 1 || len(models[0].ProviderIDs) != 1 || models[0].ProviderIDs[0] != MaClawOfficialProviderID {
		t.Fatalf("models without grant = %#v, want official model listed but inactive", models)
	}

	now := time.Now().UTC()
	reg.Grants = append(reg.Grants, Grant{
		ID:             "grant-1",
		Email:          "user@example.com",
		ServiceGroupID: "redeem",
		Source:         "test",
		StartsAt:       now.Add(-time.Hour),
		ExpiresAt:      now.Add(time.Hour),
		CreatedAt:      now.Add(-time.Hour),
		CreditsTotal:   100,
	})
	status, models, err = ResolveStatusFromRegistry(context.Background(), reg, nil, "user@example.com", "https://hub.example/api/llm/v1")
	if err != nil {
		t.Fatalf("ResolveStatusFromRegistry with grant: %v", err)
	}
	if !status.Active {
		t.Fatalf("official service group with grant should be active: %+v", status)
	}
	if len(models) != 1 || len(models[0].ProviderIDs) != 1 || models[0].ProviderIDs[0] != MaClawOfficialProviderID {
		t.Fatalf("authorized models = %#v", models)
	}
}

func TestRegistryNormalizeEnsuresBuiltinDefaultGroup(t *testing.T) {
	reg := &Registry{}
	reg.Normalize()
	if len(reg.ModelServiceGroups) == 0 {
		t.Fatal("expected builtin default model service group")
	}
	if reg.ModelServiceGroups[0].ID != DefaultModelServiceGroupID {
		t.Fatalf("expected first group id=%q, got %q", DefaultModelServiceGroupID, reg.ModelServiceGroups[0].ID)
	}
	if reg.ModelServiceGroups[0].Name != DefaultModelServiceGroupName {
		t.Fatalf("expected first group name=%q, got %q", DefaultModelServiceGroupName, reg.ModelServiceGroups[0].Name)
	}
	if len(reg.ModelServiceGroups[0].Models) != 0 {
		t.Fatalf("expected builtin default group to have no models, got %d", len(reg.ModelServiceGroups[0].Models))
	}

	reg = &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:     DefaultModelServiceGroupID,
			Name:   "Custom Default Name",
			Models: []ModelServiceModel{{Name: "gpt-5", ProviderIDs: []string{"provider-a"}}},
		}},
	}
	reg.Normalize()
	group := reg.FindModelServiceGroup(DefaultModelServiceGroupID)
	if group == nil {
		t.Fatal("expected builtin default group after normalize")
	}
	if len(group.Models) != 0 {
		t.Fatalf("expected builtin default group to stay permissionless, got %d models", len(group.Models))
	}
}

func TestRegistryNormalizeDefaultsNewUsersToBuiltinDefaultGroup(t *testing.T) {
	reg := &Registry{}
	reg.Normalize()
	if len(reg.DefaultNewUserServiceGroups) != 1 || reg.DefaultNewUserServiceGroups[0] != DefaultModelServiceGroupID {
		t.Fatalf("expected default new-user service groups [%q], got %#v", DefaultModelServiceGroupID, reg.DefaultNewUserServiceGroups)
	}
	if reg.DefaultNewUserDurationDays != 30 {
		t.Fatalf("expected default new-user duration 30, got %d", reg.DefaultNewUserDurationDays)
	}
}

func TestPurgeOrphanedServiceGroupReferences(t *testing.T) {
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{
			{ID: "coding-basic", Name: "Coding Basic", AccessPolicy: AccessPolicyFree},
		},
		GlobalServiceGroupIDs:       []string{"coding-basic", "deleted-global"},
		DefaultNewUserServiceGroups: []string{"deleted-default", "coding-basic"},
		GroupBindings: []GroupBinding{
			{GroupID: "ops", ServiceGroupIDs: []string{"coding-basic", "deleted-binding"}},
			{GroupID: "empty", ServiceGroupIDs: []string{"deleted-only"}},
		},
		UserBindings: []UserBinding{
			{Email: "user@example.com", ServiceGroupIDs: []string{"deleted-user", "coding-basic"}},
			{Email: "orphan@example.com", ServiceGroupIDs: []string{"deleted-orphan"}},
		},
		Cards: []RechargeCard{
			{ID: "card-1", ServiceGroupIDs: []string{"coding-basic", "deleted-card"}},
		},
		Grants: []Grant{
			{ID: "grant-1", Email: "user@example.com", ServiceGroupID: "coding-basic"},
			{ID: "grant-2", Email: "user@example.com", ServiceGroupID: "deleted-grant"},
		},
	}
	reg.Normalize()
	changed := reg.PurgeOrphanedServiceGroupReferences()
	if !changed {
		t.Fatal("expected PurgeOrphanedServiceGroupReferences to return true")
	}

	// GlobalServiceGroupIDs: only coding-basic remains.
	if len(reg.GlobalServiceGroupIDs) != 1 || reg.GlobalServiceGroupIDs[0] != "coding-basic" {
		t.Fatalf("GlobalServiceGroupIDs = %#v, want [coding-basic]", reg.GlobalServiceGroupIDs)
	}
	// DefaultNewUserServiceGroups: only coding-basic remains.
	if len(reg.DefaultNewUserServiceGroups) != 1 || reg.DefaultNewUserServiceGroups[0] != "coding-basic" {
		t.Fatalf("DefaultNewUserServiceGroups = %#v, want [coding-basic]", reg.DefaultNewUserServiceGroups)
	}
	// GroupBindings: "ops" keeps coding-basic; "empty" removed entirely.
	if len(reg.GroupBindings) != 1 || reg.GroupBindings[0].GroupID != "ops" {
		t.Fatalf("GroupBindings = %#v, want [ops->coding-basic]", reg.GroupBindings)
	}
	if len(reg.GroupBindings[0].ServiceGroupIDs) != 1 || reg.GroupBindings[0].ServiceGroupIDs[0] != "coding-basic" {
		t.Fatalf("GroupBindings[0].ServiceGroupIDs = %#v, want [coding-basic]", reg.GroupBindings[0].ServiceGroupIDs)
	}
	// UserBindings: user@example.com keeps coding-basic; orphan@example.com removed entirely.
	if len(reg.UserBindings) != 1 || reg.UserBindings[0].Email != "user@example.com" {
		t.Fatalf("UserBindings = %#v, want [user@example.com]", reg.UserBindings)
	}
	if len(reg.UserBindings[0].ServiceGroupIDs) != 1 || reg.UserBindings[0].ServiceGroupIDs[0] != "coding-basic" {
		t.Fatalf("UserBindings[0].ServiceGroupIDs = %#v, want [coding-basic]", reg.UserBindings[0].ServiceGroupIDs)
	}
	// Cards: deleted-card removed from ServiceGroupIDs.
	if len(reg.Cards) != 1 || len(reg.Cards[0].ServiceGroupIDs) != 1 || reg.Cards[0].ServiceGroupIDs[0] != "coding-basic" {
		t.Fatalf("Cards[0].ServiceGroupIDs = %#v, want [coding-basic]", reg.Cards[0].ServiceGroupIDs)
	}
	// Grants: grant-2 (referencing deleted-grant) removed.
	if len(reg.Grants) != 1 || reg.Grants[0].ID != "grant-1" {
		t.Fatalf("Grants = %#v, want [grant-1]", reg.Grants)
	}
}

func TestPurgeOrphanedServiceGroupReferencesNoChange(t *testing.T) {
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{
			{ID: "coding-basic", Name: "Coding Basic"},
		},
		GlobalServiceGroupIDs:       []string{"coding-basic"},
		DefaultNewUserServiceGroups: []string{"coding-basic"},
		UserBindings:                []UserBinding{{Email: "user@example.com", ServiceGroupIDs: []string{"coding-basic"}}},
	}
	reg.Normalize()
	changed := reg.PurgeOrphanedServiceGroupReferences()
	if changed {
		t.Fatal("expected PurgeOrphanedServiceGroupReferences to return false when all references are valid")
	}
}

func TestSelectBestModelForRequest(t *testing.T) {
	models := []AuthorizedModel{
		{Name: "doc-fast", CapabilityTags: []string{"document"}, ResolutionTier: 1, Priority: 10, CreditMultiplier: 1},
		{Name: "reasoning-pro", CapabilityTags: []string{"reasoning"}, ResolutionTier: 2, Priority: 50, CreditMultiplier: 2},
		{Name: "tool-lite", CapabilityTags: []string{"tools"}, ResolutionTier: 1, Priority: 20, CreditMultiplier: 1},
	}
	body := map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "Please analyze this PDF document and summarize it."}},
	}
	best := SelectBestModelForRequest(body, models)
	if best == nil || best.Name != "doc-fast" {
		t.Fatalf("expected doc-fast, got %#v", best)
	}
	body = map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "Use tools to search and fetch the answer."}},
		"tools":    []any{map[string]any{"type": "function"}},
	}
	best = SelectBestModelForRequest(body, models)
	if best == nil || best.Name != "tool-lite" {
		t.Fatalf("expected tool-lite, got %#v", best)
	}
}

func TestApplyCreditUsageToRegistry(t *testing.T) {
	now := time.Now().UTC()
	reg := &Registry{
		Grants: []Grant{{
			ID: "g1", Email: "user@example.com", ServiceGroupID: "coding-basic", Source: "card", StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), CreditsTotal: 10, CreditsUsed: 1,
		}, {
			ID: "g2", Email: "user@example.com", ServiceGroupID: "coding-basic", Source: "card", StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(2 * time.Hour), CreditsTotal: 5, CreditsUsed: 0,
		}},
	}
	used := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"coding-basic"}, 6.5, now)
	if used != 6.5 {
		t.Fatalf("expected used credits 6.5, got %v", used)
	}
	if reg.Grants[0].CreditsUsed != 7.5 {
		t.Fatalf("expected first grant credits used 7.5, got %v", reg.Grants[0].CreditsUsed)
	}
	if reg.Grants[1].CreditsUsed != 0 {
		t.Fatalf("expected second grant untouched, got %v", reg.Grants[1].CreditsUsed)
	}
	used = ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"coding-basic"}, 5, now)
	if used != 5 {
		t.Fatalf("expected used credits 5, got %v", used)
	}
	if reg.Grants[0].CreditsUsed != 10 {
		t.Fatalf("expected first grant exhausted, got %v", reg.Grants[0].CreditsUsed)
	}
	if reg.Grants[1].CreditsUsed != 2.5 {
		t.Fatalf("expected second grant credits used 2.5, got %v", reg.Grants[1].CreditsUsed)
	}
}

func TestApplyCreditUsageHonorsPeriodLimits(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 30, 0, 0, time.UTC)
	reg := &Registry{Grants: []Grant{{
		ID:             "g1",
		Email:          "user@example.com",
		ServiceGroupID: "coding-basic",
		Source:         "card",
		StartsAt:       now.Add(-time.Hour),
		ExpiresAt:      now.Add(24 * time.Hour),
		CreditsTotal:   100,
		PeriodLimits:   CreditPeriodLimits{FiveHour: 10, Daily: 15, Weekly: 40, Monthly: 80},
	}}}

	used := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"coding-basic"}, 12, now)
	if used != 10 {
		t.Fatalf("expected first charge to stop at 5-hour limit 10, got %v", used)
	}
	if reg.Grants[0].CreditsUsed != 10 || reg.Grants[0].PeriodUsage.FiveHour.CreditsUsed != 10 || reg.Grants[0].PeriodUsage.Daily.CreditsUsed != 10 {
		t.Fatalf("unexpected usage after first charge: %#v", reg.Grants[0])
	}

	used = ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"coding-basic"}, 5, now.Add(5*time.Hour))
	if used != 5 {
		t.Fatalf("expected next 5-hour window to allow remaining daily credits 5, got %v", used)
	}
	if reg.Grants[0].CreditsUsed != 15 || reg.Grants[0].PeriodUsage.Daily.CreditsUsed != 15 {
		t.Fatalf("unexpected usage after second charge: %#v", reg.Grants[0])
	}

	used = ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"coding-basic"}, 1, now.Add(6*time.Hour))
	if used != 0 {
		t.Fatalf("expected daily limit to block more usage, got %v", used)
	}
}

func TestApplyCreditUsageHonorsPeriodLimitsForUnlimitedGrant(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 30, 0, 0, time.UTC)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:           "coding-basic",
			Name:         "Coding Basic",
			AccessPolicy: AccessPolicyGrantRequired,
			Models:       []ModelServiceModel{{Name: "auto", ProviderIDs: []string{"provider-a"}}},
		}},
		Grants: []Grant{{
			ID:             "g1",
			Email:          "user@example.com",
			ServiceGroupID: "coding-basic",
			StartsAt:       now.Add(-time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreditsTotal:   0,
			PeriodLimits:   CreditPeriodLimits{FiveHour: 10},
		}},
	}

	used := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"coding-basic"}, 6, now)
	if used != 6 {
		t.Fatalf("expected first usage 6, got %v", used)
	}
	if got := reg.Grants[0].PeriodUsage.FiveHour.CreditsUsed; got != 6 {
		t.Fatalf("five-hour usage = %v, want 6", got)
	}
	if allowed, _, code, _, credits, _, _ := BillingEligibilityForServiceGroups(reg, "user@example.com", []string{"coding-basic"}, now); !allowed || code != "" || credits != 4 {
		t.Fatalf("expected unlimited grant to report remaining period credits before limit is exhausted, allowed=%v code=%q credits=%v", allowed, code, credits)
	}

	used = ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"coding-basic"}, 10, now)
	if used != 4 {
		t.Fatalf("expected second usage to stop at remaining period limit 4, got %v", used)
	}
	allowed, _, code, _, _, _, _ := BillingEligibilityForServiceGroups(reg, "user@example.com", []string{"coding-basic"}, now)
	if allowed || code != "LLM_SERVICE_PERIOD_LIMITED" {
		t.Fatalf("expected exhausted period limit, allowed=%v code=%q", allowed, code)
	}
	statusNow := time.Now().UTC()
	reg.Grants[0].StartsAt = statusNow.Add(-time.Hour)
	reg.Grants[0].ExpiresAt = statusNow.Add(24 * time.Hour)
	reg.Grants[0].PeriodUsage.FiveHour = GrantUsageWindow{WindowStart: fiveHourWindowStart(statusNow), CreditsUsed: 10}
	status, _, err := ResolveStatusFromRegistry(context.Background(), reg, nil, "user@example.com", "https://hub.example.com/api/llm/v1")
	if err != nil {
		t.Fatalf("ResolveStatusFromRegistry() error = %v", err)
	}
	if status.Active || len(status.CreditGrants) != 1 || status.CreditGrants[0].Status != "period_limited" {
		t.Fatalf("expected resolved status to show unlimited grant as period-limited, got %#v", status)
	}
	if len(status.ActiveGrants) != 0 {
		t.Fatalf("period-limited unlimited grant should not be exposed as active_grants, got %#v", status.ActiveGrants)
	}
}

func TestApplyCreditUsageSkipsMeteredGrantWhenUnmeteredUnlimitedGrantActive(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 30, 0, 0, time.UTC)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:           "coding-basic",
			Name:         "Coding Basic",
			AccessPolicy: AccessPolicyGrantRequired,
			Models:       []ModelServiceModel{{Name: "auto", ProviderIDs: []string{"provider-a"}}},
		}},
		Grants: []Grant{{
			ID:             "point-card",
			Email:          "user@example.com",
			ServiceGroupID: "coding-basic",
			Source:         "card",
			StartsAt:       now.Add(-time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreditsTotal:   100,
		}, {
			ID:             "unmetered-unlimited",
			Email:          "user@example.com",
			ServiceGroupID: "coding-basic",
			Source:         "admin",
			StartsAt:       now.Add(-time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreditsTotal:   0,
		}},
	}
	reg.Normalize()

	used := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"coding-basic"}, 12, now)
	if used != 0 {
		t.Fatalf("expected unmetered unlimited grant to skip metered charge, got %v", used)
	}
	if reg.Grants[0].CreditsUsed != 0 || reg.Grants[1].CreditsUsed != 0 {
		t.Fatalf("expected grants untouched, got %#v", reg.Grants)
	}
	allowed, policy, code, message, credits, hasActiveGrant, hasAnyGrant := BillingEligibilityForServiceGroups(reg, "user@example.com", []string{"coding-basic"}, now)
	if !allowed || policy != AccessPolicyGrantRequired || code != "" || message != "" || credits != 0 || !hasActiveGrant || !hasAnyGrant {
		t.Fatalf("unexpected unmetered unlimited eligibility: allowed=%v policy=%q code=%q message=%q credits=%v active=%v any=%v", allowed, policy, code, message, credits, hasActiveGrant, hasAnyGrant)
	}
}

func TestBillingEligibilityEarlyStartsQueuedUnmeteredUnlimitedGrantWhenCurrentGrantExhausted(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 30, 0, 0, time.UTC)
	startsAt := now.Add(2 * time.Hour)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:           "coding-basic",
			Name:         "Coding Basic",
			AccessPolicy: AccessPolicyGrantRequired,
			Models:       []ModelServiceModel{{Name: "auto", ProviderIDs: []string{"provider-a"}}},
		}},
		Grants: []Grant{{
			ID:             "spent-point-card",
			Email:          "user@example.com",
			ServiceGroupID: "coding-basic",
			Source:         "card",
			StartsAt:       now.Add(-24 * time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreditsTotal:   100,
			CreditsUsed:    100,
		}, {
			ID:             "queued-unmetered-unlimited",
			Email:          "user@example.com",
			ServiceGroupID: "coding-basic",
			Source:         "admin",
			StartsAt:       startsAt,
			ExpiresAt:      startsAt.Add(30 * 24 * time.Hour),
			CreditsTotal:   0,
		}}}
	reg.Normalize()

	allowed, policy, code, message, credits, hasActiveGrant, hasAnyGrant := BillingEligibilityForServiceGroups(reg, "user@example.com", []string{"coding-basic"}, now)
	if !allowed || policy != AccessPolicyGrantRequired || code != "" || message != "" || credits != 0 || !hasActiveGrant || !hasAnyGrant {
		t.Fatalf("unexpected queued unmetered eligibility: allowed=%v policy=%q code=%q message=%q credits=%v active=%v any=%v", allowed, policy, code, message, credits, hasActiveGrant, hasAnyGrant)
	}
	used := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"coding-basic"}, 12, now)
	if used != 0 {
		t.Fatalf("expected queued unmetered grant to skip metered charge, got %v", used)
	}
	if !reg.Grants[1].StartsAt.Equal(now) || !reg.Grants[1].ExpiresAt.Equal(now.Add(30*24*time.Hour)) {
		t.Fatalf("expected queued unmetered grant to shift with duration preserved, got %#v", reg.Grants[1])
	}
	if reg.Grants[0].CreditsUsed != 100 || reg.Grants[1].CreditsUsed != 0 {
		t.Fatalf("expected grants to remain uncharged, got %#v", reg.Grants)
	}

	statusNow := time.Now().UTC()
	reg.Grants[0].StartsAt = statusNow.Add(-24 * time.Hour)
	reg.Grants[0].ExpiresAt = statusNow.Add(24 * time.Hour)
	reg.Grants[1].StartsAt = statusNow.Add(2 * time.Hour)
	reg.Grants[1].ExpiresAt = reg.Grants[1].StartsAt.Add(30 * 24 * time.Hour)
	status, _, err := ResolveStatusFromRegistry(context.Background(), reg, nil, "user@example.com", "https://hub.example.com/api/llm/v1")
	if err != nil {
		t.Fatalf("ResolveStatusFromRegistry() error = %v", err)
	}
	if !status.Active || !status.SkipLLMConfig {
		t.Fatalf("expected queued unmetered unlimited grant to keep service active, got %#v", status)
	}
	if status.CreditsTotal != 0 || status.CreditsUsed != 0 || status.CreditsRemaining != 0 || status.CreditsAvailable != 0 {
		t.Fatalf("expected queued unmetered unlimited grant to be visibly unlimited, got total=%v used=%v remaining=%v available=%v", status.CreditsTotal, status.CreditsUsed, status.CreditsRemaining, status.CreditsAvailable)
	}
}

func TestBillingEligibilityUsesEarlierQueuedPointCardBeforeQueuedUnmeteredUnlimitedGrant(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 30, 0, 0, time.UTC)
	pointStart := now.Add(time.Hour)
	unlimitedStart := now.Add(2 * time.Hour)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:           "coding-basic",
			Name:         "Coding Basic",
			AccessPolicy: AccessPolicyGrantRequired,
			Models:       []ModelServiceModel{{Name: "auto", ProviderIDs: []string{"provider-a"}}},
		}},
		Grants: []Grant{{
			ID:             "spent-point-card",
			Email:          "user@example.com",
			ServiceGroupID: "coding-basic",
			Source:         "card",
			StartsAt:       now.Add(-24 * time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreditsTotal:   100,
			CreditsUsed:    100,
		}, {
			ID:             "queued-point-card",
			Email:          "user@example.com",
			ServiceGroupID: "coding-basic",
			Source:         "card",
			StartsAt:       pointStart,
			ExpiresAt:      pointStart.Add(30 * 24 * time.Hour),
			CreditsTotal:   100,
		}, {
			ID:             "queued-unmetered-unlimited",
			Email:          "user@example.com",
			ServiceGroupID: "coding-basic",
			Source:         "admin",
			StartsAt:       unlimitedStart,
			ExpiresAt:      unlimitedStart.Add(30 * 24 * time.Hour),
			CreditsTotal:   0,
		}}}
	reg.Normalize()

	allowed, policy, code, message, credits, hasActiveGrant, hasAnyGrant := BillingEligibilityForServiceGroups(reg, "user@example.com", []string{"coding-basic"}, now)
	if !allowed || policy != AccessPolicyGrantRequired || code != "" || message != "" || credits != 100 || !hasActiveGrant || !hasAnyGrant {
		t.Fatalf("unexpected queued point-card eligibility: allowed=%v policy=%q code=%q message=%q credits=%v active=%v any=%v", allowed, policy, code, message, credits, hasActiveGrant, hasAnyGrant)
	}
	used := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"coding-basic"}, 12, now)
	if used != 12 {
		t.Fatalf("expected queued point card to be charged, got %v", used)
	}
	if !reg.Grants[1].StartsAt.Equal(now) || reg.Grants[1].CreditsUsed != 12 {
		t.Fatalf("queued point card should shift and be charged, got %#v", reg.Grants[1])
	}
	if !reg.Grants[2].StartsAt.Equal(unlimitedStart) || reg.Grants[2].CreditsUsed != 0 {
		t.Fatalf("queued unmetered unlimited grant should stay queued, got %#v", reg.Grants[2])
	}
}

func TestBillingEligibilityDoesNotEarlyStartQueuedUnmeteredUnlimitedGrantWhenCurrentGrantPeriodLimited(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 30, 0, 0, time.UTC)
	unlimitedStart := now.Add(time.Hour)
	pointStart := now.Add(2 * time.Hour)
	windowStart := fiveHourWindowStart(now)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "grant-group", Name: "Grant", AccessPolicy: AccessPolicyGrantRequired}},
		Grants: []Grant{{
			ID:             "grant-monthly",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       now.Add(-24 * time.Hour),
			ExpiresAt:      now.AddDate(0, 1, 0),
			CreditsTotal:   5000,
			CreditsUsed:    100,
			PeriodLimits:   CreditPeriodLimits{FiveHour: 100},
			PeriodUsage:    CreditPeriodUsage{FiveHour: GrantUsageWindow{WindowStart: windowStart, CreditsUsed: 100}},
		}, {
			ID:             "grant-admin-unlimited-no-period",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "admin",
			StartsAt:       unlimitedStart,
			ExpiresAt:      unlimitedStart.Add(30 * 24 * time.Hour),
			CreditsTotal:   0,
		}, {
			ID:             "grant-point-card",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       pointStart,
			ExpiresAt:      pointStart.Add(365 * 24 * time.Hour),
			CreditsTotal:   10000,
		}}}
	reg.Normalize()

	allowed, policy, code, message, credits, hasActiveGrant, hasAnyGrant := BillingEligibilityForServiceGroups(reg, "user@example.com", []string{"grant-group"}, now)
	if !allowed || policy != AccessPolicyGrantRequired || code != "" || credits != 10000 || !hasActiveGrant || !hasAnyGrant {
		t.Fatalf("unexpected period-limited+queued-unmetered+point eligibility: allowed=%v policy=%q code=%q credits=%v active=%v any=%v message=%q", allowed, policy, code, credits, hasActiveGrant, hasAnyGrant, message)
	}
	used := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"grant-group"}, 3, now)
	if used != 3 || !reg.Grants[2].StartsAt.Equal(now) || reg.Grants[2].CreditsUsed != 3 {
		t.Fatalf("queued point card should be used, got used=%v grant=%#v", used, reg.Grants[2])
	}
	if !reg.Grants[1].StartsAt.Equal(unlimitedStart) || reg.Grants[1].CreditsUsed != 0 {
		t.Fatalf("queued unmetered unlimited grant should remain queued, got %#v", reg.Grants[1])
	}
}

func TestApplyCreditUsageStillChargesPeriodLimitedUnlimitedGrant(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 30, 0, 0, time.UTC)
	reg := &Registry{Grants: []Grant{{
		ID:             "limited-unlimited",
		Email:          "user@example.com",
		ServiceGroupID: "coding-basic",
		Source:         "admin",
		StartsAt:       now.Add(-time.Hour),
		ExpiresAt:      now.Add(24 * time.Hour),
		CreditsTotal:   0,
		PeriodLimits:   CreditPeriodLimits{FiveHour: 10},
	}}}
	reg.Normalize()

	used := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"coding-basic"}, 6, now)
	if used != 6 {
		t.Fatalf("expected period-limited unlimited grant to consume period credits, got %v", used)
	}
	if got := reg.Grants[0].PeriodUsage.FiveHour.CreditsUsed; got != 6 {
		t.Fatalf("five-hour usage = %v, want 6", got)
	}
}

func TestRedeemCardCopiesPeriodLimitsToGrant(t *testing.T) {
	ctx := context.Background()
	system := newTestSystemSettings()
	code := "ABCDE12345FGHIJ67890"
	limits := CreditPeriodLimits{FiveHour: 10, Daily: 20, Weekly: 50, Monthly: 100}
	if err := SaveRegistry(ctx, system, &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic", Models: []ModelServiceModel{{Name: "gpt-5", ProviderIDs: []string{"provider-a"}}}}},
		Cards: []RechargeCard{{
			ID:              "card-1",
			CodeHash:        HashCode(code),
			ServiceGroupIDs: []string{"coding-basic"},
			DurationDays:    30,
			Credits:         100,
			PeriodLimits:    limits,
			CreatedAt:       time.Now().UTC(),
		}},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := RedeemCard(ctx, system, nil, "user@example.com", code, "http://hub.test/api/llm/v1"); err != nil {
		t.Fatal(err)
	}
	saved, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Grants) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(saved.Grants))
	}
	if saved.Grants[0].PeriodLimits != limits {
		t.Fatalf("expected period limits %#v, got %#v", limits, saved.Grants[0].PeriodLimits)
	}
}

func TestSelectBestModelForRequestWithDebug(t *testing.T) {
	models := []AuthorizedModel{
		{Name: "doc-fast", CapabilityTags: []string{"document"}, ResolutionTier: 1, Priority: 10, CreditMultiplier: 1},
		{Name: "reasoning-pro", CapabilityTags: []string{"reasoning"}, ResolutionTier: 2, Priority: 50, CreditMultiplier: 2},
	}
	body := map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "Please analyze this PDF document and summarize it."}},
	}
	best, debug := SelectBestModelForRequestWithDebug(body, models)
	if best == nil || best.Name != "doc-fast" {
		t.Fatalf("expected doc-fast, got %#v", best)
	}
	if debug == nil {
		t.Fatal("expected debug info")
	}
	if debug.SelectedModel != "doc-fast" {
		t.Fatalf("selected model = %q, want doc-fast", debug.SelectedModel)
	}
	if len(debug.MatchedTags) != 1 || debug.MatchedTags[0] != "document" {
		t.Fatalf("matched tags = %#v, want [document]", debug.MatchedTags)
	}
	if debug.SelectionReason == "" {
		t.Fatal("expected selection reason")
	}
}

func TestBuildAuthorizedModelsTracksProviderScopedGroupsAndMultiplier(t *testing.T) {
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:   "group-a",
			Name: "Group A",
			Models: []ModelServiceModel{{
				Name:        "auto",
				ProviderIDs: []string{"provider-a"},
				ProviderConfigs: []ModelServiceProviderConfig{{
					ProviderID:       "provider-a",
					CapabilityTags:   []string{"reasoning"},
					Priority:         40,
					ResolutionTier:   2,
					CreditMultiplier: 2,
				}},
			}},
		}, {
			ID:   "group-b",
			Name: "Group B",
			Models: []ModelServiceModel{{
				Name:        "auto",
				ProviderIDs: []string{"provider-b"},
				ProviderConfigs: []ModelServiceProviderConfig{{
					ProviderID:       "provider-b",
					CapabilityTags:   []string{"document"},
					Priority:         80,
					ResolutionTier:   1,
					CreditMultiplier: 1,
				}},
			}},
		}, {
			ID:   "group-c",
			Name: "Group C",
			Models: []ModelServiceModel{{
				Name:        "auto",
				ProviderIDs: []string{"provider-a"},
				ProviderConfigs: []ModelServiceProviderConfig{{
					ProviderID:       "provider-a",
					CapabilityTags:   []string{"tools"},
					Priority:         60,
					ResolutionTier:   3,
					CreditMultiplier: 1.5,
				}},
			}},
		}},
	}
	models, _ := buildAuthorizedModels(reg, []string{"group-a", "group-b", "group-c"})
	if len(models) != 1 {
		t.Fatalf("expected 1 merged model, got %d", len(models))
	}
	model := models[0]
	if got := model.ProviderServiceGroups["provider-a"]; len(got) != 2 || got[0] != "group-a" || got[1] != "group-c" {
		t.Fatalf("provider-a groups = %#v", got)
	}
	if got := model.ProviderServiceGroups["provider-b"]; len(got) != 1 || got[0] != "group-b" {
		t.Fatalf("provider-b groups = %#v", got)
	}
	if got := model.ProviderCreditMultipliers["provider-a"]; got != 1.5 {
		t.Fatalf("provider-a multiplier = %v, want 1.5", got)
	}
	if got := model.ProviderCreditMultipliers["provider-b"]; got != 1 {
		t.Fatalf("provider-b multiplier = %v, want 1", got)
	}
	if !containsString(model.CapabilityTags, "reasoning") || !containsString(model.CapabilityTags, "document") || !containsString(model.CapabilityTags, "tools") {
		t.Fatalf("capability tags = %#v", model.CapabilityTags)
	}
	if model.Priority != 80 {
		t.Fatalf("priority = %d, want 80", model.Priority)
	}
	if model.ResolutionTier != 1 {
		t.Fatalf("resolution tier = %d, want 1", model.ResolutionTier)
	}
	if got := CreditMultiplierForProvider(&model, "provider-a"); got != 1.5 {
		t.Fatalf("CreditMultiplierForProvider(provider-a) = %v, want 1.5", got)
	}
	if got := CreditMultiplierForProvider(&model, "provider-b"); got != 1 {
		t.Fatalf("CreditMultiplierForProvider(provider-b) = %v, want 1", got)
	}
	if got := ServiceGroupIDsForProvider(&model, "provider-a"); len(got) != 2 || got[0] != "group-a" || got[1] != "group-c" {
		t.Fatalf("ServiceGroupIDsForProvider(provider-a) = %#v", got)
	}
}

func TestRegistryNormalizeMigratesLegacyModelFieldsToProviderConfigs(t *testing.T) {
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:   "group-a",
			Name: "Group A",
			Models: []ModelServiceModel{{
				Name:             "auto",
				ProviderIDs:      []string{"provider-a", "provider-b"},
				CapabilityTags:   []string{"reasoning", "tools"},
				Priority:         50,
				ResolutionTier:   2,
				CreditMultiplier: 1.2,
			}},
		}},
	}
	reg.Normalize()
	model := reg.ModelServiceGroups[1].Models[0]
	if len(model.ProviderConfigs) != 2 {
		t.Fatalf("provider configs = %#v", model.ProviderConfigs)
	}
	for _, cfg := range model.ProviderConfigs {
		if len(cfg.CapabilityTags) != 2 || cfg.Priority != 50 || cfg.ResolutionTier != 2 || cfg.CreditMultiplier != 1.2 {
			t.Fatalf("unexpected migrated config: %#v", cfg)
		}
	}
}

func TestRegistryNormalizeMergesDuplicateModelAliasesWithinGroup(t *testing.T) {
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:   "group-a",
			Name: "Group A",
			Models: []ModelServiceModel{{
				Name:        "auto",
				ProviderIDs: []string{"provider-a"},
				ProviderConfigs: []ModelServiceProviderConfig{{
					ProviderID:     "provider-a",
					CapabilityTags: []string{"document"},
					Priority:       60,
				}},
			}, {
				Name:        "AUTO",
				ProviderIDs: []string{"provider-b"},
				ProviderConfigs: []ModelServiceProviderConfig{{
					ProviderID:       "provider-b",
					CapabilityTags:   []string{"reasoning", "tools"},
					Priority:         80,
					ResolutionTier:   2,
					CreditMultiplier: 1.5,
				}},
			}},
		}},
	}
	reg.Normalize()
	if len(reg.ModelServiceGroups) < 2 {
		t.Fatalf("expected builtin default group plus custom group, got %#v", reg.ModelServiceGroups)
	}
	models := reg.ModelServiceGroups[1].Models
	if len(models) != 1 {
		t.Fatalf("expected duplicate aliases to merge into one model, got %#v", models)
	}
	model := models[0]
	if model.Name != "auto" {
		t.Fatalf("merged model name = %q, want auto", model.Name)
	}
	if len(model.ProviderIDs) != 2 || model.ProviderIDs[0] != "provider-a" || model.ProviderIDs[1] != "provider-b" {
		t.Fatalf("merged provider ids = %#v", model.ProviderIDs)
	}
	if len(model.ProviderConfigs) != 2 {
		t.Fatalf("merged provider configs = %#v", model.ProviderConfigs)
	}
	if !containsString(model.CapabilityTags, "document") || !containsString(model.CapabilityTags, "reasoning") || !containsString(model.CapabilityTags, "tools") {
		t.Fatalf("merged capability tags = %#v", model.CapabilityTags)
	}
}
func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func TestRedeemCardCreatesGrantsAndRejectsReuse(t *testing.T) {
	ctx := context.Background()
	system := newTestSystemSettings()
	code := "0123456789ABCDEFGHIJ"
	if err := SaveRegistry(ctx, system, &Registry{
		ModelServiceGroups: []ModelServiceGroup{
			{ID: "coding-basic", Name: "Coding Basic", Models: []ModelServiceModel{{Name: "gpt-5", ProviderIDs: []string{"provider-a"}}}},
			{ID: "coding-pro", Name: "Coding Pro", Models: []ModelServiceModel{{Name: "gpt-5", ProviderIDs: []string{"provider-b"}}}},
		},
		Cards: []RechargeCard{{
			ID:              "card-1",
			CodeHash:        HashCode(code),
			ServiceGroupIDs: []string{"coding-basic", "coding-pro"},
			DurationDays:    7,
			Credits:         90,
			CreatedAt:       time.Now().UTC(),
		}},
	}); err != nil {
		t.Fatal(err)
	}

	status, err := RedeemCard(ctx, system, nil, "User@Example.COM", strings.ToLower(code), "http://hub.test/api/llm/v1")
	if err != nil {
		t.Fatal(err)
	}
	if status == nil || !status.Active {
		t.Fatalf("expected active status, got %#v", status)
	}
	if status.CreditsAvailable != 90 {
		t.Fatalf("expected credits available 90, got %v", status.CreditsAvailable)
	}

	saved, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Grants) != 2 {
		t.Fatalf("expected 2 grants, got %d", len(saved.Grants))
	}
	for _, grant := range saved.Grants {
		if grant.Email != "user@example.com" {
			t.Fatalf("expected normalized email, got %q", grant.Email)
		}
		if grant.CreditsTotal != 45 {
			t.Fatalf("expected split credits 45, got %v", grant.CreditsTotal)
		}
		if grant.ExpiresAt.Sub(grant.StartsAt) != 7*24*time.Hour {
			t.Fatalf("expected 7-day grant, got %s", grant.ExpiresAt.Sub(grant.StartsAt))
		}
	}
	card, _ := saved.FindCardByID("card-1")
	if card == nil || card.RedeemedAt == nil || card.RedeemedByEmail != "user@example.com" {
		t.Fatalf("expected redeemed card, got %#v", card)
	}
	if _, err := RedeemCard(ctx, system, nil, "other@example.com", code, "http://hub.test/api/llm/v1"); err == nil {
		t.Fatal("expected reused card to be rejected")
	}
}

func TestPromoteQueuedMeteredGrantsStartsHistoricalPointCards(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	queuedStart := now.Add(24 * time.Hour)
	duration := 30 * 24 * time.Hour
	reg := &Registry{
		Grants: []Grant{
			{
				ID:             "active-metered",
				Email:          "user@example.com",
				ServiceGroupID: "coding-basic",
				Source:         "card",
				StartsAt:       now.Add(-time.Hour),
				ExpiresAt:      now.Add(24 * time.Hour),
				CreditsTotal:   100,
				CreditsUsed:    10,
			},
			{
				ID:             "queued-metered",
				Email:          "user@example.com",
				ServiceGroupID: "coding-basic",
				Source:         "card",
				StartsAt:       queuedStart,
				ExpiresAt:      queuedStart.Add(duration),
				CreditsTotal:   10000,
				CreditsUsed:    0,
			},
			{
				ID:             "queued-unmetered",
				Email:          "user@example.com",
				ServiceGroupID: "coding-basic",
				Source:         "card",
				StartsAt:       queuedStart,
				ExpiresAt:      queuedStart.Add(7 * 24 * time.Hour),
				CreditsTotal:   0,
			},
			{
				ID:             "queued-spent",
				Email:          "user@example.com",
				ServiceGroupID: "coding-basic",
				Source:         "card",
				StartsAt:       queuedStart,
				ExpiresAt:      queuedStart.Add(duration),
				CreditsTotal:   500,
				CreditsUsed:    500,
			},
		},
	}
	changed := PromoteQueuedMeteredGrants(reg, now)
	if changed != 1 {
		t.Fatalf("PromoteQueuedMeteredGrants() changed = %d, want 1", changed)
	}
	got := reg.Grants[1]
	if !got.StartsAt.Equal(now) {
		t.Fatalf("queued metered grant StartsAt = %s, want %s", got.StartsAt, now)
	}
	if got.ExpiresAt.Sub(got.StartsAt) != duration {
		t.Fatalf("duration not preserved: got %s want %s", got.ExpiresAt.Sub(got.StartsAt), duration)
	}
	if !reg.Grants[2].StartsAt.Equal(queuedStart) {
		t.Fatalf("unmetered queued grant should stay queued, starts=%s", reg.Grants[2].StartsAt)
	}
	if !reg.Grants[3].StartsAt.Equal(queuedStart) {
		t.Fatalf("spent queued grant should stay queued, starts=%s", reg.Grants[3].StartsAt)
	}
	if PromoteQueuedMeteredGrants(reg, now) != 0 {
		t.Fatal("second promote should be a no-op")
	}
}

func TestPromoteQueuedMeteredGrantsLeavesSoleScheduledGrantQueued(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	queuedStart := now.Add(2 * time.Hour)
	reg := &Registry{
		Grants: []Grant{{
			ID:             "scheduled-metered",
			Email:          "user@example.com",
			ServiceGroupID: "coding-basic",
			Source:         "card",
			StartsAt:       queuedStart,
			ExpiresAt:      queuedStart.Add(24 * time.Hour),
			CreditsTotal:   100,
		}},
	}
	if changed := PromoteQueuedMeteredGrants(reg, now); changed != 0 {
		t.Fatalf("PromoteQueuedMeteredGrants() changed = %d, want 0 for sole scheduled grant", changed)
	}
	if !reg.Grants[0].StartsAt.Equal(queuedStart) {
		t.Fatalf("sole scheduled grant StartsAt = %s, want %s", reg.Grants[0].StartsAt, queuedStart)
	}
}

func TestRedeemCardMeteredCreditsStartImmediatelyWhileActiveGrantExists(t *testing.T) {
	ctx := context.Background()
	system := newTestSystemSettings()
	now := time.Now().UTC()
	code := "KLMNOPQRST0123456789"
	if err := SaveRegistry(ctx, system, &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic", Models: []ModelServiceModel{{Name: "gpt-5", ProviderIDs: []string{"provider-a"}}}}},
		Cards: []RechargeCard{{
			ID:              "card-credits",
			CodeHash:        HashCode(code),
			ServiceGroupIDs: []string{"coding-basic"},
			DurationDays:    30,
			Credits:         10000,
			CreatedAt:       now,
		}},
		Grants: []Grant{{
			ID:             "grant-existing",
			Email:          "user@example.com",
			ServiceGroupID: "coding-basic",
			Source:         "card",
			StartsAt:       now.Add(-time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreatedAt:      now.Add(-time.Hour),
			CreditsTotal:   100,
			CreditsUsed:    10,
		}},
	}); err != nil {
		t.Fatal(err)
	}

	status, err := RedeemCard(ctx, system, nil, "user@example.com", code, "http://hub.test/api/llm/v1")
	if err != nil {
		t.Fatal(err)
	}
	if status == nil || status.CreditsAvailable < 10000 {
		t.Fatalf("expected metered top-up to increase available credits immediately, got %#v", status)
	}
	saved, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Grants) != 2 {
		t.Fatalf("expected 2 grants, got %d", len(saved.Grants))
	}
	newGrant := saved.Grants[1]
	if newGrant.StartsAt.After(now.Add(time.Minute)) {
		t.Fatalf("expected metered grant to start immediately, got starts_at=%s", newGrant.StartsAt)
	}
	if newGrant.CreditsTotal != 10000 {
		t.Fatalf("expected 10000 credits on new grant, got %v", newGrant.CreditsTotal)
	}
}

func TestResolveStatusKeepsPaidCreditsWhenUnlimitedGrantAlsoActive(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:           "coding-basic",
			Name:         "Coding Basic",
			AccessPolicy: AccessPolicyGrantRequired,
			Models:       []ModelServiceModel{{Name: "auto", ProviderIDs: []string{"provider-a"}}},
		}},
		Grants: []Grant{
			{
				ID:             "unlimited-gift",
				Email:          "phone:19900001111",
				ServiceGroupID: "coding-basic",
				Source:         "default_new_user",
				StartsAt:       now.Add(-time.Hour),
				ExpiresAt:      now.Add(7 * 24 * time.Hour),
				CreatedAt:      now.Add(-time.Hour),
				CreditsTotal:   0,
			},
			{
				ID:             "paid-point-card",
				Email:          "phone:19900001111",
				ServiceGroupID: "coding-basic",
				Source:         "card",
				StartsAt:       now.Add(-time.Minute),
				ExpiresAt:      now.Add(30 * 24 * time.Hour),
				CreatedAt:      now.Add(-time.Minute),
				CreditsTotal:   10000,
				CreditsUsed:    0,
			},
		},
	}
	status, _, err := ResolveStatusFromRegistry(ctx, reg, nil, "phone:19900001111", "http://hub.test/api/llm/v1")
	if err != nil {
		t.Fatalf("ResolveStatusFromRegistry() error = %v", err)
	}
	if !status.Active {
		t.Fatalf("expected active status, got %#v", status)
	}
	if status.CreditsAvailable < 10000 || status.CreditsRemaining < 10000 {
		t.Fatalf("expected paid point-card credits to remain visible beside unlimited gift, got total=%v remaining=%v available=%v grants=%#v", status.CreditsTotal, status.CreditsRemaining, status.CreditsAvailable, status.CreditGrants)
	}
}

func TestRedeemCardStacksExistingGrantForSameServiceGroup(t *testing.T) {
	ctx := context.Background()
	system := newTestSystemSettings()
	now := time.Now().UTC()
	code := "ABCDEFGHIJ0123456789"
	if err := SaveRegistry(ctx, system, &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic", Models: []ModelServiceModel{{Name: "gpt-5", ProviderIDs: []string{"provider-a"}}}}},
		Cards: []RechargeCard{{
			ID:              "card-1",
			CodeHash:        HashCode(code),
			ServiceGroupIDs: []string{"coding-basic"},
			DurationDays:    3,
			CreatedAt:       now,
		}},
		Grants: []Grant{{
			ID:             "grant-existing",
			Email:          "user@example.com",
			ServiceGroupID: "coding-basic",
			Source:         "card",
			StartsAt:       now.Add(-time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreatedAt:      now.Add(-time.Hour),
		}},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := RedeemCard(ctx, system, nil, "user@example.com", code, "http://hub.test/api/llm/v1"); err != nil {
		t.Fatal(err)
	}
	saved, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Grants) != 2 {
		t.Fatalf("expected 2 grants, got %d", len(saved.Grants))
	}
	newGrant := saved.Grants[1]
	if !newGrant.StartsAt.Equal(saved.Grants[0].ExpiresAt) {
		t.Fatalf("expected new grant to start after existing grant expires, got start=%s existing_expiry=%s", newGrant.StartsAt, saved.Grants[0].ExpiresAt)
	}
	if newGrant.ExpiresAt.Sub(newGrant.StartsAt) != 3*24*time.Hour {
		t.Fatalf("expected 3-day stacked grant, got %s", newGrant.ExpiresAt.Sub(newGrant.StartsAt))
	}
	status, err := ResolveServiceStatus(ctx, system, nil, "user@example.com", "http://hub.test/api/llm/v1")
	if err != nil {
		t.Fatal(err)
	}
	if status.EffectiveExpiresAt != newGrant.ExpiresAt.Format(time.RFC3339) {
		t.Fatalf("expected effective expiry to include stacked grant, got %q want %q", status.EffectiveExpiresAt, newGrant.ExpiresAt.Format(time.RFC3339))
	}
	if status.NearestExpiresAt != saved.Grants[0].ExpiresAt.Format(time.RFC3339) {
		t.Fatalf("expected nearest expiry to remain current grant expiry, got %q", status.NearestExpiresAt)
	}
}

func TestEffectiveExpiryIgnoresInvalidAndSpentQueuedGrants(t *testing.T) {
	ctx := context.Background()
	system := newTestSystemSettings()
	now := time.Now().UTC()
	currentExpiry := now.Add(24 * time.Hour)
	if err := SaveRegistry(ctx, system, &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic", Models: []ModelServiceModel{{Name: "gpt-5", ProviderIDs: []string{"provider-a"}}}}},
		Grants: []Grant{
			{ID: "grant-current", Email: "user@example.com", ServiceGroupID: "coding-basic", Source: "card", StartsAt: now.Add(-time.Hour), ExpiresAt: currentExpiry, CreatedAt: now.Add(-time.Hour)},
			{ID: "grant-spent", Email: "user@example.com", ServiceGroupID: "coding-basic", Source: "card", StartsAt: currentExpiry, ExpiresAt: currentExpiry.Add(7 * 24 * time.Hour), CreatedAt: now, CreditsTotal: 10, CreditsUsed: 10},
			{ID: "grant-missing-group", Email: "user@example.com", ServiceGroupID: "missing-group", Source: "card", StartsAt: currentExpiry, ExpiresAt: currentExpiry.Add(30 * 24 * time.Hour), CreatedAt: now},
		},
	}); err != nil {
		t.Fatal(err)
	}
	status, err := ResolveServiceStatus(ctx, system, nil, "user@example.com", "http://hub.test/api/llm/v1")
	if err != nil {
		t.Fatal(err)
	}
	if status.EffectiveExpiresAt != currentExpiry.Format(time.RFC3339) {
		t.Fatalf("expected invalid queued grants to be ignored, got %q want %q", status.EffectiveExpiresAt, currentExpiry.Format(time.RFC3339))
	}
}

func TestRedeemCardRejectsInvalidCodeFormat(t *testing.T) {
	ctx := context.Background()
	system := newTestSystemSettings()
	reg := &Registry{}
	if err := SaveRegistry(ctx, system, reg); err != nil {
		t.Fatal(err)
	}
	if _, err := RedeemCard(ctx, system, nil, "user@example.com", "bad-code", "http://hub.test/api/llm/v1"); err == nil {
		t.Fatal("expected invalid code format to be rejected")
	}
}

func TestBillingEligibilityForServiceGroups(t *testing.T) {
	now := time.Now().UTC()
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "free-group", Name: "Free", AccessPolicy: AccessPolicyFree}, {ID: "grant-group", Name: "Grant", AccessPolicy: AccessPolicyGrantRequired}},
		Grants: []Grant{{
			ID:             "grant-1",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       now.Add(-time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreditsTotal:   10,
			CreditsUsed:    10,
		}},
	}
	reg.Normalize()

	allowed, policy, code, _, credits, hasActiveGrant, hasAnyGrant := BillingEligibilityForServiceGroups(reg, "user@example.com", []string{"free-group"}, now)
	if !allowed || policy != AccessPolicyFree || code != "" || credits != 0 || hasActiveGrant || hasAnyGrant {
		t.Fatalf("unexpected free eligibility: allowed=%v policy=%q code=%q credits=%v active=%v any=%v", allowed, policy, code, credits, hasActiveGrant, hasAnyGrant)
	}

	allowed, policy, code, _, credits, hasActiveGrant, hasAnyGrant = BillingEligibilityForServiceGroups(reg, "user@example.com", []string{"grant-group"}, now)
	if allowed || policy != AccessPolicyGrantRequired || code != "LLM_SERVICE_CREDITS_EXHAUSTED" || credits != 0 || !hasActiveGrant || !hasAnyGrant {
		t.Fatalf("unexpected exhausted eligibility: allowed=%v policy=%q code=%q credits=%v active=%v any=%v", allowed, policy, code, credits, hasActiveGrant, hasAnyGrant)
	}

	allowed, policy, code, _, credits, hasActiveGrant, hasAnyGrant = BillingEligibilityForServiceGroups(reg, "other@example.com", []string{"grant-group"}, now)
	if allowed || policy != AccessPolicyGrantRequired || code != "LLM_SERVICE_CREDITS_REQUIRED" || credits != 0 || hasActiveGrant || hasAnyGrant {
		t.Fatalf("unexpected required eligibility: allowed=%v policy=%q code=%q credits=%v active=%v any=%v", allowed, policy, code, credits, hasActiveGrant, hasAnyGrant)
	}
}

func TestExplainBillingRoutes(t *testing.T) {
	now := time.Now().UTC()
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "grant-group", Name: "Grant", AccessPolicy: AccessPolicyGrantRequired}},
		Grants: []Grant{{
			ID:             "grant-1",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       now.Add(-time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreditsTotal:   12,
			CreditsUsed:    2,
		}},
	}
	reg.Normalize()
	models := []AuthorizedModel{{
		Name:                  "auto",
		ProviderIDs:           []string{"provider-a"},
		ProviderServiceGroups: map[string][]string{"provider-a": {"grant-group"}},
	}}

	routes := ExplainBillingRoutes(reg, "user@example.com", models, now)
	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %#v", routes)
	}
	if !routes[0].Eligible || routes[0].AccessPolicy != AccessPolicyGrantRequired || routes[0].CreditsAvailable != 10 {
		t.Fatalf("unexpected route diagnostic: %#v", routes[0])
	}
}

func TestExplainEntitlementDiagnosticIncludesBillingRoutes(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:           "grant-group",
			Name:         "Grant Group",
			AccessPolicy: AccessPolicyGrantRequired,
			Models: []ModelServiceModel{{
				Name:        "auto",
				ProviderIDs: []string{"provider-a"},
			}},
		}},
		UserBindings: []UserBinding{{
			Email:           "user@example.com",
			ServiceGroupIDs: []string{"grant-group"},
		}},
		Grants: []Grant{{
			ID:             "grant-1",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       now.Add(-time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreditsTotal:   8,
			CreditsUsed:    3,
		}},
	}
	reg.Normalize()

	diag, err := ExplainEntitlementDiagnosticFromRegistry(ctx, reg, nil, "user@example.com", "http://hub.test/api/llm/v1")
	if err != nil {
		t.Fatal(err)
	}
	if diag == nil || diag.ServiceStatus == nil {
		t.Fatalf("expected service status, got %#v", diag)
	}
	if len(diag.BillingRoutes) != 1 {
		t.Fatalf("expected 1 billing route, got %#v", diag.BillingRoutes)
	}
	route := diag.BillingRoutes[0]
	if route.ModelName != "auto" || route.ProviderID != "provider-a" {
		t.Fatalf("unexpected route identity: %#v", route)
	}
	if !route.Eligible || route.AccessPolicy != AccessPolicyGrantRequired {
		t.Fatalf("unexpected route eligibility: %#v", route)
	}
	if route.CreditsAvailable != 5 {
		t.Fatalf("credits available = %v, want 5", route.CreditsAvailable)
	}
}

func TestResolveStatusReportsPeriodLimitInactiveReason(t *testing.T) {
	now := time.Now().UTC()
	windowStart := fiveHourWindowStart(now)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:           "grant-group",
			Name:         "Grant",
			AccessPolicy: AccessPolicyGrantRequired,
			Models:       []ModelServiceModel{{Name: "auto", ProviderIDs: []string{"provider-a"}}},
		}},
		Grants: []Grant{{
			ID:             "grant-1",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       now.Add(-time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreditsTotal:   100,
			CreditsUsed:    10,
			PeriodLimits:   CreditPeriodLimits{FiveHour: 10},
			PeriodUsage:    CreditPeriodUsage{FiveHour: GrantUsageWindow{WindowStart: windowStart, CreditsUsed: 10}},
		}},
	}
	reg.Normalize()

	status, _, err := ResolveStatusFromRegistry(context.Background(), reg, nil, "user@example.com", "https://hub.example.com/api/llm/v1")
	if err != nil {
		t.Fatalf("ResolveStatusFromRegistry() error = %v", err)
	}
	if status.Active {
		t.Fatalf("status active = true, want false: %#v", status)
	}
	if len(status.InactiveReasons) == 0 || !strings.Contains(status.InactiveReasons[0], "current period credit limit is exhausted") {
		t.Fatalf("expected period-limit inactive reason, got %#v", status.InactiveReasons)
	}
}

func TestGrantSummaryReportsPeriodLimitRetry(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 30, 0, 0, time.UTC)
	windowStart := fiveHourWindowStart(now)
	grant := Grant{
		ID:             "g1",
		Email:          "user@example.com",
		ServiceGroupID: "coding-basic",
		Source:         "card",
		StartsAt:       now.Add(-time.Hour),
		ExpiresAt:      now.Add(24 * time.Hour),
		CreditsTotal:   100,
		CreditsUsed:    10,
		PeriodLimits:   CreditPeriodLimits{FiveHour: 10},
		PeriodUsage:    CreditPeriodUsage{FiveHour: GrantUsageWindow{WindowStart: windowStart, CreditsUsed: 10}},
	}

	summary := grantSummary(grant, now)
	if summary.Active || summary.Status != "period_limited" {
		t.Fatalf("expected period-limited inactive grant, got %#v", summary)
	}
	if summary.RetryAfterSeconds <= 0 {
		t.Fatalf("expected retry_after_seconds, got %#v", summary)
	}
	wantRetryAt := windowStart.Add(5 * time.Hour).Format(time.RFC3339)
	if summary.RetryAfterAt != wantRetryAt {
		t.Fatalf("retry_after_at = %q, want %q", summary.RetryAfterAt, wantRetryAt)
	}
}

func TestGrantSummaryReportsQueuedRetry(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 30, 0, 0, time.UTC)
	startsAt := now.Add(90 * time.Minute)
	grant := Grant{
		ID:             "g1",
		Email:          "user@example.com",
		ServiceGroupID: "coding-basic",
		Source:         "card",
		StartsAt:       startsAt,
		ExpiresAt:      startsAt.Add(24 * time.Hour),
		CreditsTotal:   100,
	}

	summary := grantSummary(grant, now)
	if summary.Active || summary.Status != "queued" {
		t.Fatalf("expected queued inactive grant, got %#v", summary)
	}
	if summary.RetryAfterSeconds != int64((90 * time.Minute).Seconds()) {
		t.Fatalf("retry_after_seconds = %d", summary.RetryAfterSeconds)
	}
	if summary.RetryAfterAt != startsAt.Format(time.RFC3339) {
		t.Fatalf("retry_after_at = %q, want %q", summary.RetryAfterAt, startsAt.Format(time.RFC3339))
	}
}

func TestCreditGrantSummariesKeepsExhaustedGrantVisible(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 30, 0, 0, time.UTC)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
		Grants: []Grant{{
			ID:             "grant-1",
			Email:          "user@example.com",
			ServiceGroupID: "coding-basic",
			Source:         "card",
			StartsAt:       now.Add(-time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreditsTotal:   10,
			CreditsUsed:    10,
		}},
	}
	reg.Normalize()

	summaries := creditGrantSummaries(reg, "user@example.com", now)
	if len(summaries) != 1 {
		t.Fatalf("summaries len = %d, want 1", len(summaries))
	}
	if summaries[0].Active || summaries[0].Status != "exhausted" {
		t.Fatalf("expected exhausted grant to remain visible, got %#v", summaries[0])
	}
}

func TestCreditGrantSummariesPrioritizesQueuedOverExhausted(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 30, 0, 0, time.UTC)
	startsAt := now.Add(2 * time.Hour)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
		Grants: []Grant{{
			ID:             "grant-old",
			Email:          "user@example.com",
			ServiceGroupID: "coding-basic",
			Source:         "card",
			StartsAt:       now.Add(-24 * time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreditsTotal:   100,
			CreditsUsed:    100,
		}, {
			ID:             "grant-next",
			Email:          "user@example.com",
			ServiceGroupID: "coding-basic",
			Source:         "card",
			StartsAt:       startsAt,
			ExpiresAt:      startsAt.Add(24 * time.Hour),
			CreditsTotal:   100,
		}},
	}
	reg.Normalize()

	summaries := creditGrantSummaries(reg, "user@example.com", now)
	if len(summaries) != 2 {
		t.Fatalf("summaries len = %d, want 2", len(summaries))
	}
	if summaries[0].Status != "queued" || summaries[1].Status != "exhausted" {
		t.Fatalf("expected queued grant before exhausted grant, got %#v", summaries)
	}
}

func TestCreditGrantSummariesKeepsLatestExpiredGrantVisibleWhenNoCurrentGrant(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 30, 0, 0, time.UTC)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
		Grants: []Grant{{
			ID:             "old-expired",
			Email:          "user@example.com",
			ServiceGroupID: "coding-basic",
			Source:         "card",
			StartsAt:       now.Add(-72 * time.Hour),
			ExpiresAt:      now.Add(-48 * time.Hour),
			CreditsTotal:   100,
			CreditsUsed:    20,
		}, {
			ID:             "latest-expired",
			Email:          "user@example.com",
			ServiceGroupID: "coding-basic",
			Source:         "card",
			StartsAt:       now.Add(-48 * time.Hour),
			ExpiresAt:      now.Add(-time.Hour),
			CreditsTotal:   100,
			CreditsUsed:    10,
		}},
	}
	reg.Normalize()

	summaries := creditGrantSummaries(reg, "user@example.com", now)
	if len(summaries) != 1 {
		t.Fatalf("summaries len = %d, want 1", len(summaries))
	}
	if summaries[0].Status != "expired" || summaries[0].ServiceGroupID != "coding-basic" || summaries[0].ExpiresAt != now.Add(-time.Hour) {
		t.Fatalf("expected latest expired grant summary, got %#v", summaries[0])
	}
}

func TestCreditGrantSummariesOmitsExpiredGrantWhenCurrentGrantExists(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 30, 0, 0, time.UTC)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
		Grants: []Grant{{
			ID:             "expired",
			Email:          "user@example.com",
			ServiceGroupID: "coding-basic",
			Source:         "card",
			StartsAt:       now.Add(-72 * time.Hour),
			ExpiresAt:      now.Add(-time.Hour),
			CreditsTotal:   100,
			CreditsUsed:    10,
		}, {
			ID:             "active",
			Email:          "user@example.com",
			ServiceGroupID: "coding-basic",
			Source:         "card",
			StartsAt:       now.Add(-time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreditsTotal:   100,
			CreditsUsed:    10,
		}},
	}
	reg.Normalize()

	summaries := creditGrantSummaries(reg, "user@example.com", now)
	if len(summaries) != 1 {
		t.Fatalf("summaries len = %d, want 1", len(summaries))
	}
	if summaries[0].Status != "active" {
		t.Fatalf("expected only current active grant summary, got %#v", summaries)
	}
}

func TestBillingEligibilityReportsPeriodLimit(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 30, 0, 0, time.UTC)
	windowStart := fiveHourWindowStart(now)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "grant-group", Name: "Grant", AccessPolicy: AccessPolicyGrantRequired}},
		Grants: []Grant{{
			ID:             "grant-1",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       now.Add(-time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreditsTotal:   100,
			CreditsUsed:    10,
			PeriodLimits:   CreditPeriodLimits{FiveHour: 10},
			PeriodUsage:    CreditPeriodUsage{FiveHour: GrantUsageWindow{WindowStart: windowStart, CreditsUsed: 10}},
		}},
	}
	reg.Normalize()

	allowed, policy, code, message, credits, hasActiveGrant, hasAnyGrant := BillingEligibilityForServiceGroups(reg, "user@example.com", []string{"grant-group"}, now)
	if allowed || policy != AccessPolicyGrantRequired || code != "LLM_SERVICE_PERIOD_LIMITED" || credits != 0 || !hasActiveGrant || !hasAnyGrant {
		t.Fatalf("unexpected period-limit eligibility: allowed=%v policy=%q code=%q credits=%v active=%v any=%v message=%q", allowed, policy, code, credits, hasActiveGrant, hasAnyGrant, message)
	}
	if !strings.Contains(message, windowStart.Add(5*time.Hour).Format(time.RFC3339)) {
		t.Fatalf("expected retry time in message, got %q", message)
	}

}

func TestBillingEligibilityReportsQueuedGrant(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 30, 0, 0, time.UTC)
	startsAt := now.Add(2 * time.Hour)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "grant-group", Name: "Grant", AccessPolicy: AccessPolicyGrantRequired}},
		Grants: []Grant{{
			ID:             "grant-1",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       startsAt,
			ExpiresAt:      startsAt.Add(24 * time.Hour),
			CreditsTotal:   100,
		}},
	}
	reg.Normalize()

	allowed, policy, code, message, credits, hasActiveGrant, hasAnyGrant := BillingEligibilityForServiceGroups(reg, "user@example.com", []string{"grant-group"}, now)
	if allowed || policy != AccessPolicyGrantRequired || code != "LLM_SERVICE_GRANT_QUEUED" || credits != 0 || hasActiveGrant || !hasAnyGrant {
		t.Fatalf("unexpected queued eligibility: allowed=%v policy=%q code=%q credits=%v active=%v any=%v message=%q", allowed, policy, code, credits, hasActiveGrant, hasAnyGrant, message)
	}
	if !strings.Contains(message, startsAt.Format(time.RFC3339)) {
		t.Fatalf("expected grant start time in message, got %q", message)
	}
}

func TestBillingEligibilityReportsExpiredGrant(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 30, 0, 0, time.UTC)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "grant-group", Name: "Grant", AccessPolicy: AccessPolicyGrantRequired}},
		Grants: []Grant{{
			ID:             "grant-1",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       now.Add(-48 * time.Hour),
			ExpiresAt:      now.Add(-time.Hour),
			CreditsTotal:   100,
			CreditsUsed:    10,
		}},
	}
	reg.Normalize()

	allowed, policy, code, message, credits, hasActiveGrant, hasAnyGrant := BillingEligibilityForServiceGroups(reg, "user@example.com", []string{"grant-group"}, now)
	if allowed || policy != AccessPolicyGrantRequired || code != "LLM_SERVICE_GRANT_EXPIRED" || credits != 0 || hasActiveGrant || !hasAnyGrant {
		t.Fatalf("unexpected expired eligibility: allowed=%v policy=%q code=%q credits=%v active=%v any=%v message=%q", allowed, policy, code, credits, hasActiveGrant, hasAnyGrant, message)
	}
	if !strings.Contains(message, "expired") {
		t.Fatalf("expected expired message, got %q", message)
	}
}

func TestBillingEligibilityReportsQueuedGrantWhenCurrentGrantExhausted(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 30, 0, 0, time.UTC)
	startsAt := now.Add(2 * time.Hour)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "grant-group", Name: "Grant", AccessPolicy: AccessPolicyGrantRequired}},
		Grants: []Grant{{
			ID:             "grant-old",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       now.Add(-24 * time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreditsTotal:   100,
			CreditsUsed:    100,
		}, {
			ID:             "grant-next",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       startsAt,
			ExpiresAt:      startsAt.Add(24 * time.Hour),
			CreditsTotal:   100,
		}},
	}
	reg.Normalize()

	allowed, policy, code, message, credits, hasActiveGrant, hasAnyGrant := BillingEligibilityForServiceGroups(reg, "user@example.com", []string{"grant-group"}, now)
	if !allowed || policy != AccessPolicyGrantRequired || code != "" || credits != 100 || !hasActiveGrant || !hasAnyGrant {
		t.Fatalf("unexpected exhausted+queued eligibility: allowed=%v policy=%q code=%q credits=%v active=%v any=%v message=%q", allowed, policy, code, credits, hasActiveGrant, hasAnyGrant, message)
	}
	if message != "" {
		t.Fatalf("expected no denial message, got %q", message)
	}
}

func TestApplyCreditUsageEarlyStartsQueuedGrantWhenCurrentGrantExhausted(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 30, 0, 0, time.UTC)
	startsAt := now.Add(2 * time.Hour)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "grant-group", Name: "Grant", AccessPolicy: AccessPolicyGrantRequired}},
		Grants: []Grant{{
			ID:             "grant-old",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       now.Add(-24 * time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreditsTotal:   100,
			CreditsUsed:    100,
		}, {
			ID:             "grant-next",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       startsAt,
			ExpiresAt:      startsAt.Add(24 * time.Hour),
			CreditsTotal:   100,
		}},
	}
	reg.Normalize()

	used := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"grant-group"}, 12, now)
	if used != 12 {
		t.Fatalf("used = %v, want 12", used)
	}
	if !reg.Grants[1].StartsAt.Equal(now) || !reg.Grants[1].ExpiresAt.Equal(now.Add(24*time.Hour)) {
		t.Fatalf("queued grant should shift to now with same duration, got %s..%s", reg.Grants[1].StartsAt, reg.Grants[1].ExpiresAt)
	}
	if reg.Grants[1].CreditsUsed != 12 {
		t.Fatalf("next grant credits used = %v, want 12", reg.Grants[1].CreditsUsed)
	}
}

func TestApplyCreditUsageEarlyStartsOnlyNextQueuedGrant(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 30, 0, 0, time.UTC)
	firstStart := now.Add(2 * time.Hour)
	secondStart := firstStart.Add(24 * time.Hour)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "grant-group", Name: "Grant", AccessPolicy: AccessPolicyGrantRequired}},
		Grants: []Grant{{
			ID:             "grant-old",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       now.Add(-24 * time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreditsTotal:   100,
			CreditsUsed:    100,
		}, {
			ID:             "grant-next",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       firstStart,
			ExpiresAt:      firstStart.Add(24 * time.Hour),
			CreditsTotal:   100,
		}, {
			ID:             "grant-later",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       secondStart,
			ExpiresAt:      secondStart.Add(24 * time.Hour),
			CreditsTotal:   200,
		}},
	}
	reg.Normalize()

	allowed, _, _, _, credits, _, _ := BillingEligibilityForServiceGroups(reg, "user@example.com", []string{"grant-group"}, now)
	if !allowed || credits != 100 {
		t.Fatalf("eligibility should expose only next queued grant, allowed=%v credits=%v", allowed, credits)
	}
	used := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"grant-group"}, 150, now)
	if used != 100 {
		t.Fatalf("used = %v, want 100", used)
	}
	if !reg.Grants[1].StartsAt.Equal(now) || reg.Grants[1].CreditsUsed != 100 {
		t.Fatalf("next grant should shift and be consumed, got %#v", reg.Grants[1])
	}
	if !reg.Grants[2].StartsAt.Equal(secondStart) || reg.Grants[2].CreditsUsed != 0 {
		t.Fatalf("later queued grant should stay queued, got %#v", reg.Grants[2])
	}
}

func TestApplyCreditUsageEarlyStartsOnlyOneQueuedGrantWithSameStart(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 30, 0, 0, time.UTC)
	startsAt := now.Add(2 * time.Hour)
	createdAt := now.Add(-time.Hour)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "grant-group", Name: "Grant", AccessPolicy: AccessPolicyGrantRequired}},
		Grants: []Grant{{
			ID:             "grant-old",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       now.Add(-24 * time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreditsTotal:   100,
			CreditsUsed:    100,
		}, {
			ID:             "grant-next-a",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       startsAt,
			ExpiresAt:      startsAt.Add(24 * time.Hour),
			CreatedAt:      createdAt,
			CreditsTotal:   100,
		}, {
			ID:             "grant-next-b",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       startsAt,
			ExpiresAt:      startsAt.Add(24 * time.Hour),
			CreatedAt:      createdAt,
			CreditsTotal:   200,
		}},
	}
	reg.Normalize()

	allowed, _, _, _, credits, _, _ := BillingEligibilityForServiceGroups(reg, "user@example.com", []string{"grant-group"}, now)
	if !allowed || credits != 100 {
		t.Fatalf("eligibility should expose only one queued grant, allowed=%v credits=%v", allowed, credits)
	}
	used := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"grant-group"}, 150, now)
	if used != 100 {
		t.Fatalf("used = %v, want 100", used)
	}
	if !reg.Grants[1].StartsAt.Equal(now) || reg.Grants[1].CreditsUsed != 100 {
		t.Fatalf("first same-start grant should shift and be consumed, got %#v", reg.Grants[1])
	}
	if !reg.Grants[2].StartsAt.Equal(startsAt) || reg.Grants[2].CreditsUsed != 0 {
		t.Fatalf("second same-start grant should stay queued, got %#v", reg.Grants[2])
	}
}

func TestApplyCreditUsageEarlyStartsOnlyOneQueuedGrantWithSameStartAndNoIDs(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 30, 0, 0, time.UTC)
	startsAt := now.Add(2 * time.Hour)
	createdAt := now.Add(-time.Hour)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "grant-group", Name: "Grant", AccessPolicy: AccessPolicyGrantRequired}},
		Grants: []Grant{{
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       now.Add(-24 * time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreditsTotal:   100,
			CreditsUsed:    100,
		}, {
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       startsAt,
			ExpiresAt:      startsAt.Add(24 * time.Hour),
			CreatedAt:      createdAt,
			CreditsTotal:   100,
		}, {
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       startsAt,
			ExpiresAt:      startsAt.Add(24 * time.Hour),
			CreatedAt:      createdAt,
			CreditsTotal:   200,
		}},
	}
	reg.Normalize()

	allowed, _, _, _, credits, _, _ := BillingEligibilityForServiceGroups(reg, "user@example.com", []string{"grant-group"}, now)
	if !allowed || credits != 100 {
		t.Fatalf("eligibility should expose only first no-id queued grant, allowed=%v credits=%v", allowed, credits)
	}
	used := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"grant-group"}, 150, now)
	if used != 100 {
		t.Fatalf("used = %v, want 100", used)
	}
	if !reg.Grants[1].StartsAt.Equal(now) || reg.Grants[1].CreditsUsed != 100 {
		t.Fatalf("first no-id grant should shift and be consumed, got %#v", reg.Grants[1])
	}
	if !reg.Grants[2].StartsAt.Equal(startsAt) || reg.Grants[2].CreditsUsed != 0 {
		t.Fatalf("second no-id grant should stay queued, got %#v", reg.Grants[2])
	}
}

func TestResolveStatusUsesQueuedGrantWhenCurrentGrantExhausted(t *testing.T) {
	now := time.Now().UTC()
	startsAt := now.Add(2 * time.Hour)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:           "grant-group",
			Name:         "Grant",
			AccessPolicy: AccessPolicyGrantRequired,
			Models:       []ModelServiceModel{{Name: "auto", ProviderIDs: []string{"provider-a"}}},
		}},
		Grants: []Grant{{
			ID:             "grant-old",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       now.Add(-24 * time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreditsTotal:   100,
			CreditsUsed:    100,
		}, {
			ID:             "grant-next",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       startsAt,
			ExpiresAt:      startsAt.Add(24 * time.Hour),
			CreditsTotal:   100,
		}},
	}
	reg.Normalize()

	status, _, err := ResolveStatusFromRegistry(context.Background(), reg, nil, "user@example.com", "https://hub.example.com/api/llm/v1")
	if err != nil {
		t.Fatalf("ResolveStatusFromRegistry() error = %v", err)
	}
	if !status.Active || !status.SkipLLMConfig || status.CreditsAvailable != 100 {
		t.Fatalf("expected queued grant to keep service active with credits, got %#v", status)
	}
	if status.EffectiveExpiresAt != startsAt.Add(24*time.Hour).Format(time.RFC3339) {
		t.Fatalf("effective expiry = %q, want queued grant expiry", status.EffectiveExpiresAt)
	}
}

func TestBillingEligibilityUsesQueuedPointCardWhenCurrentGrantPeriodLimited(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 30, 0, 0, time.UTC)
	startsAt := now.Add(2 * time.Hour)
	windowStart := fiveHourWindowStart(now)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "grant-group", Name: "Grant", AccessPolicy: AccessPolicyGrantRequired}},
		Grants: []Grant{{
			ID:             "grant-monthly",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       now.Add(-24 * time.Hour),
			ExpiresAt:      now.AddDate(0, 1, 0),
			CreditsTotal:   5000,
			CreditsUsed:    100,
			PeriodLimits:   CreditPeriodLimits{FiveHour: 100},
			PeriodUsage:    CreditPeriodUsage{FiveHour: GrantUsageWindow{WindowStart: windowStart, CreditsUsed: 100}},
		}, {
			ID:             "grant-point-card",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       startsAt,
			ExpiresAt:      startsAt.Add(365 * 24 * time.Hour),
			CreditsTotal:   10000,
		}},
	}
	reg.Normalize()

	allowed, policy, code, message, credits, hasActiveGrant, hasAnyGrant := BillingEligibilityForServiceGroups(reg, "user@example.com", []string{"grant-group"}, now)
	if !allowed || policy != AccessPolicyGrantRequired || code != "" || credits != 10000 || !hasActiveGrant || !hasAnyGrant {
		t.Fatalf("unexpected period-limited+point-card eligibility: allowed=%v policy=%q code=%q credits=%v active=%v any=%v message=%q", allowed, policy, code, credits, hasActiveGrant, hasAnyGrant, message)
	}
	used := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"grant-group"}, 3, now)
	if used != 3 || !reg.Grants[1].StartsAt.Equal(now) || reg.Grants[1].CreditsUsed != 3 {
		t.Fatalf("queued point card was not used correctly: used=%v grant=%#v", used, reg.Grants[1])
	}
}

func TestResolveStatusShowsAvailableCreditsWhenPeriodLimitedGrantIsCovered(t *testing.T) {
	now := time.Now().UTC()
	startsAt := now.Add(2 * time.Hour)
	windowStart := fiveHourWindowStart(now)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:           "grant-group",
			Name:         "Grant",
			AccessPolicy: AccessPolicyGrantRequired,
			Models:       []ModelServiceModel{{Name: "auto", ProviderIDs: []string{"provider-a"}}},
		}},
		Grants: []Grant{{
			ID:             "grant-monthly",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       now.Add(-24 * time.Hour),
			ExpiresAt:      now.AddDate(0, 1, 0),
			CreditsTotal:   5000,
			CreditsUsed:    100,
			PeriodLimits:   CreditPeriodLimits{FiveHour: 100},
			PeriodUsage:    CreditPeriodUsage{FiveHour: GrantUsageWindow{WindowStart: windowStart, CreditsUsed: 100}},
		}, {
			ID:             "grant-point-card",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       startsAt,
			ExpiresAt:      startsAt.Add(365 * 24 * time.Hour),
			CreditsTotal:   10000,
		}},
	}
	reg.Normalize()

	status, _, err := ResolveStatusFromRegistry(context.Background(), reg, nil, "user@example.com", "https://hub.example.com/api/llm/v1")
	if err != nil {
		t.Fatalf("ResolveStatusFromRegistry() error = %v", err)
	}
	if !status.Active || status.CreditsAvailable != 10000 || status.CreditsRemaining != 10000 {
		t.Fatalf("expected visible remaining credits to match current available credits, got %#v", status)
	}
	if status.CreditsTotal != 10100 || status.CreditsUsed != 100 {
		t.Fatalf("expected visible total to cover current remaining credits, got total=%v used=%v", status.CreditsTotal, status.CreditsUsed)
	}
}

func TestResolveStatusLiftsTotalWhenUnlimitedPeriodGrantFallsBackToPointCard(t *testing.T) {
	now := time.Now().UTC()
	startsAt := now.Add(2 * time.Hour)
	windowStart := fiveHourWindowStart(now)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:           "grant-group",
			Name:         "Grant",
			AccessPolicy: AccessPolicyGrantRequired,
			Models:       []ModelServiceModel{{Name: "auto", ProviderIDs: []string{"provider-a"}}},
		}},
		Grants: []Grant{{
			ID:             "grant-unlimited-monthly",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       now.Add(-24 * time.Hour),
			ExpiresAt:      now.AddDate(0, 1, 0),
			CreditsTotal:   0,
			PeriodLimits:   CreditPeriodLimits{FiveHour: 100},
			PeriodUsage:    CreditPeriodUsage{FiveHour: GrantUsageWindow{WindowStart: windowStart, CreditsUsed: 100}},
		}, {
			ID:             "grant-point-card",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       startsAt,
			ExpiresAt:      startsAt.Add(365 * 24 * time.Hour),
			CreditsTotal:   10000,
		}},
	}
	reg.Normalize()

	status, _, err := ResolveStatusFromRegistry(context.Background(), reg, nil, "user@example.com", "https://hub.example.com/api/llm/v1")
	if err != nil {
		t.Fatalf("ResolveStatusFromRegistry() error = %v", err)
	}
	if !status.Active || status.CreditsAvailable != 10000 || status.CreditsRemaining != 10000 || status.CreditsTotal != 10000 {
		t.Fatalf("expected fallback point-card totals to be visible, got %#v", status)
	}
}

func TestBillingEligibilityKeepsQueuedPeriodGrantWhenCurrentGrantPeriodLimited(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 30, 0, 0, time.UTC)
	startsAt := now.Add(2 * time.Hour)
	windowStart := fiveHourWindowStart(now)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "grant-group", Name: "Grant", AccessPolicy: AccessPolicyGrantRequired}},
		Grants: []Grant{{
			ID:             "grant-monthly",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       now.Add(-24 * time.Hour),
			ExpiresAt:      now.AddDate(0, 1, 0),
			CreditsTotal:   5000,
			CreditsUsed:    100,
			PeriodLimits:   CreditPeriodLimits{FiveHour: 100},
			PeriodUsage:    CreditPeriodUsage{FiveHour: GrantUsageWindow{WindowStart: windowStart, CreditsUsed: 100}},
		}, {
			ID:             "grant-next-monthly",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       startsAt,
			ExpiresAt:      startsAt.Add(30 * 24 * time.Hour),
			CreditsTotal:   5000,
			PeriodLimits:   CreditPeriodLimits{FiveHour: 100},
		}},
	}
	reg.Normalize()

	allowed, policy, code, message, credits, hasActiveGrant, hasAnyGrant := BillingEligibilityForServiceGroups(reg, "user@example.com", []string{"grant-group"}, now)
	if allowed || policy != AccessPolicyGrantRequired || code != "LLM_SERVICE_PERIOD_LIMITED" || credits != 0 || !hasActiveGrant || !hasAnyGrant {
		t.Fatalf("unexpected period-limited+queued-period eligibility: allowed=%v policy=%q code=%q credits=%v active=%v any=%v message=%q", allowed, policy, code, credits, hasActiveGrant, hasAnyGrant, message)
	}
	if !strings.Contains(message, windowStart.Add(5*time.Hour).Format(time.RFC3339)) {
		t.Fatalf("expected retry time in message, got %q", message)
	}
}

func TestBillingEligibilityKeepsQueuedPeriodGrantWhenAnyCurrentGrantPeriodLimited(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 30, 0, 0, time.UTC)
	startsAt := now.Add(2 * time.Hour)
	windowStart := fiveHourWindowStart(now)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "grant-group", Name: "Grant", AccessPolicy: AccessPolicyGrantRequired}},
		Grants: []Grant{{
			ID:             "grant-exhausted-point-card",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       now.Add(-24 * time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreditsTotal:   100,
			CreditsUsed:    100,
		}, {
			ID:             "grant-monthly",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       now.Add(-24 * time.Hour),
			ExpiresAt:      now.AddDate(0, 1, 0),
			CreditsTotal:   5000,
			CreditsUsed:    100,
			PeriodLimits:   CreditPeriodLimits{FiveHour: 100},
			PeriodUsage:    CreditPeriodUsage{FiveHour: GrantUsageWindow{WindowStart: windowStart, CreditsUsed: 100}},
		}, {
			ID:             "grant-next-monthly",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       startsAt,
			ExpiresAt:      startsAt.Add(30 * 24 * time.Hour),
			CreditsTotal:   5000,
			PeriodLimits:   CreditPeriodLimits{FiveHour: 100},
		}},
	}
	reg.Normalize()

	allowed, policy, code, message, credits, hasActiveGrant, hasAnyGrant := BillingEligibilityForServiceGroups(reg, "user@example.com", []string{"grant-group"}, now)
	if allowed || policy != AccessPolicyGrantRequired || code != "LLM_SERVICE_PERIOD_LIMITED" || credits != 0 || !hasActiveGrant || !hasAnyGrant {
		t.Fatalf("unexpected mixed-blocker eligibility: allowed=%v policy=%q code=%q credits=%v active=%v any=%v message=%q", allowed, policy, code, credits, hasActiveGrant, hasAnyGrant, message)
	}
	if !strings.Contains(message, windowStart.Add(5*time.Hour).Format(time.RFC3339)) {
		t.Fatalf("expected retry time in message, got %q", message)
	}
	used := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"grant-group"}, 3, now)
	if used != 0 || !reg.Grants[2].StartsAt.Equal(startsAt) || reg.Grants[2].CreditsUsed != 0 {
		t.Fatalf("queued period grant should not be early-started: used=%v grant=%#v", used, reg.Grants[2])
	}
}

func TestBillingEligibilityUsesQueuedPointCardEvenWhenQueuedPeriodGrantStartsEarlier(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 30, 0, 0, time.UTC)
	periodStart := now.Add(2 * time.Hour)
	pointStart := periodStart.Add(30 * 24 * time.Hour)
	windowStart := fiveHourWindowStart(now)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "grant-group", Name: "Grant", AccessPolicy: AccessPolicyGrantRequired}},
		Grants: []Grant{{
			ID:             "grant-monthly",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       now.Add(-24 * time.Hour),
			ExpiresAt:      now.AddDate(0, 1, 0),
			CreditsTotal:   5000,
			CreditsUsed:    100,
			PeriodLimits:   CreditPeriodLimits{FiveHour: 100},
			PeriodUsage:    CreditPeriodUsage{FiveHour: GrantUsageWindow{WindowStart: windowStart, CreditsUsed: 100}},
		}, {
			ID:             "grant-next-monthly",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       periodStart,
			ExpiresAt:      periodStart.Add(30 * 24 * time.Hour),
			CreditsTotal:   5000,
			PeriodLimits:   CreditPeriodLimits{FiveHour: 100},
		}, {
			ID:             "grant-point-card",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       pointStart,
			ExpiresAt:      pointStart.Add(365 * 24 * time.Hour),
			CreditsTotal:   10000,
		}},
	}
	reg.Normalize()

	allowed, policy, code, message, credits, hasActiveGrant, hasAnyGrant := BillingEligibilityForServiceGroups(reg, "user@example.com", []string{"grant-group"}, now)
	if !allowed || policy != AccessPolicyGrantRequired || code != "" || credits != 10000 || !hasActiveGrant || !hasAnyGrant {
		t.Fatalf("unexpected period-limited+queued-period+point eligibility: allowed=%v policy=%q code=%q credits=%v active=%v any=%v message=%q", allowed, policy, code, credits, hasActiveGrant, hasAnyGrant, message)
	}
	used := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"grant-group"}, 3, now)
	if used != 3 || !reg.Grants[2].StartsAt.Equal(now) || reg.Grants[2].CreditsUsed != 3 {
		t.Fatalf("queued point card should early-start despite earlier queued period grant: used=%v grant=%#v", used, reg.Grants[2])
	}
	if !reg.Grants[1].StartsAt.Equal(periodStart) || reg.Grants[1].CreditsUsed != 0 {
		t.Fatalf("queued period grant should remain queued: %#v", reg.Grants[1])
	}
}

func TestBillingEligibilitySkipsNonConsumableQueuedGrantWhenFindingPeriodLimitFallback(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 30, 0, 0, time.UTC)
	unlimitedStart := now.Add(time.Hour)
	pointStart := now.Add(2 * time.Hour)
	windowStart := fiveHourWindowStart(now)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "grant-group", Name: "Grant", AccessPolicy: AccessPolicyGrantRequired}},
		Grants: []Grant{{
			ID:             "grant-monthly",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       now.Add(-24 * time.Hour),
			ExpiresAt:      now.AddDate(0, 1, 0),
			CreditsTotal:   5000,
			CreditsUsed:    100,
			PeriodLimits:   CreditPeriodLimits{FiveHour: 100},
			PeriodUsage:    CreditPeriodUsage{FiveHour: GrantUsageWindow{WindowStart: windowStart, CreditsUsed: 100}},
		}, {
			ID:             "grant-admin-unlimited-no-period",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "admin",
			StartsAt:       unlimitedStart,
			ExpiresAt:      unlimitedStart.Add(30 * 24 * time.Hour),
			CreditsTotal:   0,
		}, {
			ID:             "grant-point-card",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       pointStart,
			ExpiresAt:      pointStart.Add(365 * 24 * time.Hour),
			CreditsTotal:   10000,
		}},
	}
	reg.Normalize()

	allowed, policy, code, message, credits, hasActiveGrant, hasAnyGrant := BillingEligibilityForServiceGroups(reg, "user@example.com", []string{"grant-group"}, now)
	if !allowed || policy != AccessPolicyGrantRequired || code != "" || credits != 10000 || !hasActiveGrant || !hasAnyGrant {
		t.Fatalf("unexpected period-limited+non-consumable+point eligibility: allowed=%v policy=%q code=%q credits=%v active=%v any=%v message=%q", allowed, policy, code, credits, hasActiveGrant, hasAnyGrant, message)
	}
	used := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"grant-group"}, 3, now)
	if used != 3 || !reg.Grants[2].StartsAt.Equal(now) || reg.Grants[2].CreditsUsed != 3 {
		t.Fatalf("queued point card should ignore earlier non-consumable queued grant: used=%v grant=%#v", used, reg.Grants[2])
	}
	if !reg.Grants[1].StartsAt.Equal(unlimitedStart) || reg.Grants[1].CreditsUsed != 0 {
		t.Fatalf("non-consumable queued grant should remain queued: %#v", reg.Grants[1])
	}
}

func TestBillingEligibilityAllowsUnlimitedGrant(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 30, 0, 0, time.UTC)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:           "grant-group",
			Name:         "Grant",
			AccessPolicy: AccessPolicyGrantRequired,
			Models:       []ModelServiceModel{{Name: "gpt-5", ProviderIDs: []string{"provider-a"}}},
		}},
		Grants: []Grant{{
			ID:             "grant-1",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "admin",
			StartsAt:       now.Add(-time.Hour),
			ExpiresAt:      now.AddDate(0, 0, 30),
			CreditsTotal:   0,
		}},
	}
	reg.Normalize()

	allowed, policy, code, message, credits, hasActiveGrant, hasAnyGrant := BillingEligibilityForServiceGroups(reg, "user@example.com", []string{"grant-group"}, now)
	if !allowed || policy != AccessPolicyGrantRequired || code != "" || message != "" || credits != 0 || !hasActiveGrant || !hasAnyGrant {
		t.Fatalf("unexpected unlimited eligibility: allowed=%v policy=%q code=%q message=%q credits=%v active=%v any=%v", allowed, policy, code, message, credits, hasActiveGrant, hasAnyGrant)
	}

	statusNow := time.Now().UTC()
	reg.Grants[0].StartsAt = statusNow.Add(-time.Hour)
	reg.Grants[0].ExpiresAt = statusNow.AddDate(0, 0, 30)
	status, _, err := ResolveStatusFromRegistry(context.Background(), reg, nil, "user@example.com", "https://hub.example.com/api/llm/v1")
	if err != nil {
		t.Fatalf("ResolveStatusFromRegistry() error = %v", err)
	}
	if !status.Active || !status.SkipLLMConfig {
		t.Fatalf("expected unlimited grant to keep service active, got %#v", status)
	}
}

func TestBillingEligibilityReportsPeriodLimitForUnlimitedGrant(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 30, 0, 0, time.UTC)
	windowStart := fiveHourWindowStart(now)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "grant-group", Name: "Grant", AccessPolicy: AccessPolicyGrantRequired}},
		Grants: []Grant{{
			ID:             "grant-1",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "admin",
			StartsAt:       now.Add(-time.Hour),
			ExpiresAt:      now.AddDate(0, 0, 30),
			CreditsTotal:   0,
			PeriodLimits:   CreditPeriodLimits{FiveHour: 10},
			PeriodUsage:    CreditPeriodUsage{FiveHour: GrantUsageWindow{WindowStart: windowStart, CreditsUsed: 10}},
		}},
	}
	reg.Normalize()

	allowed, policy, code, message, credits, hasActiveGrant, hasAnyGrant := BillingEligibilityForServiceGroups(reg, "user@example.com", []string{"grant-group"}, now)
	if allowed || policy != AccessPolicyGrantRequired || code != "LLM_SERVICE_PERIOD_LIMITED" || credits != 0 || !hasActiveGrant || !hasAnyGrant {
		t.Fatalf("unexpected unlimited period-limit eligibility: allowed=%v policy=%q code=%q credits=%v active=%v any=%v message=%q", allowed, policy, code, credits, hasActiveGrant, hasAnyGrant, message)
	}
	if !strings.Contains(message, windowStart.Add(5*time.Hour).Format(time.RFC3339)) {
		t.Fatalf("expected retry time in message, got %q", message)
	}

	statusNow := time.Now().UTC()
	statusWindowStart := fiveHourWindowStart(statusNow)
	reg.Grants[0].StartsAt = statusNow.Add(-time.Hour)
	reg.Grants[0].ExpiresAt = statusNow.AddDate(0, 0, 30)
	reg.Grants[0].PeriodUsage.FiveHour = GrantUsageWindow{WindowStart: statusWindowStart, CreditsUsed: 10}
	status, _, err := ResolveStatusFromRegistry(context.Background(), reg, nil, "user@example.com", "https://hub.example.com/api/llm/v1")
	if err != nil {
		t.Fatalf("ResolveStatusFromRegistry() error = %v", err)
	}
	if status.Active {
		t.Fatalf("expected unlimited period-limited grant to make service inactive, got %#v", status)
	}
	if len(status.CreditGrants) != 1 || status.CreditGrants[0].Status != "period_limited" || status.CreditGrants[0].RetryAfterSeconds <= 0 {
		t.Fatalf("expected period-limited credit grant summary, got %#v", status.CreditGrants)
	}
}

func TestBillingEligibilityAllowsUnlimitedGrantWhenAnotherGrantIsPeriodLimited(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 30, 0, 0, time.UTC)
	windowStart := fiveHourWindowStart(now)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:           "grant-group",
			Name:         "Grant",
			AccessPolicy: AccessPolicyGrantRequired,
			Models:       []ModelServiceModel{{Name: "auto", ProviderIDs: []string{"provider-a"}}},
		}},
		Grants: []Grant{{
			ID:             "grant-period-limited",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       now.Add(-time.Hour),
			ExpiresAt:      now.AddDate(0, 0, 30),
			CreditsTotal:   5000,
			CreditsUsed:    100,
			PeriodLimits:   CreditPeriodLimits{FiveHour: 100},
			PeriodUsage:    CreditPeriodUsage{FiveHour: GrantUsageWindow{WindowStart: windowStart, CreditsUsed: 100}},
		}, {
			ID:             "grant-unlimited",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "admin",
			StartsAt:       now.Add(-time.Hour),
			ExpiresAt:      now.AddDate(0, 0, 30),
			CreditsTotal:   0,
		}},
	}
	reg.Normalize()

	allowed, policy, code, message, credits, hasActiveGrant, hasAnyGrant := BillingEligibilityForServiceGroups(reg, "user@example.com", []string{"grant-group"}, now)
	if !allowed || policy != AccessPolicyGrantRequired || code != "" || message != "" || credits != 0 || !hasActiveGrant || !hasAnyGrant {
		t.Fatalf("unexpected mixed unlimited/period-limit eligibility: allowed=%v policy=%q code=%q message=%q credits=%v active=%v any=%v", allowed, policy, code, message, credits, hasActiveGrant, hasAnyGrant)
	}

	statusNow := time.Now().UTC()
	reg.Grants[0].StartsAt = statusNow.Add(-time.Hour)
	reg.Grants[0].ExpiresAt = statusNow.AddDate(0, 0, 30)
	reg.Grants[0].PeriodUsage.FiveHour = GrantUsageWindow{WindowStart: fiveHourWindowStart(statusNow), CreditsUsed: 100}
	reg.Grants[1].StartsAt = statusNow.Add(-time.Hour)
	reg.Grants[1].ExpiresAt = statusNow.AddDate(0, 0, 30)
	status, _, err := ResolveStatusFromRegistry(context.Background(), reg, nil, "user@example.com", "https://hub.example.com/api/llm/v1")
	if err != nil {
		t.Fatalf("ResolveStatusFromRegistry() error = %v", err)
	}
	if !status.Active || !status.SkipLLMConfig {
		t.Fatalf("expected unlimited grant to keep service active, got %#v", status)
	}
	if status.CreditsTotal != 0 || status.CreditsUsed != 0 || status.CreditsRemaining != 0 || status.CreditsAvailable != 0 {
		t.Fatalf("expected mixed unlimited service to remain visibly unlimited, got total=%v used=%v remaining=%v available=%v", status.CreditsTotal, status.CreditsUsed, status.CreditsRemaining, status.CreditsAvailable)
	}
}

func TestResolveStatusUsesDefaultServiceGroupsWhenNoEnterpriseBinding(t *testing.T) {
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:           "team-default",
			Name:         "Team Default",
			AccessPolicy: AccessPolicyFree,
			Models:       []ModelServiceModel{{Name: "gpt-default", ProviderIDs: []string{"provider-a"}}},
		}},
		DefaultNewUserServiceGroups: []string{"team-default"},
	}
	reg.Normalize()

	status, _, err := ResolveStatusFromRegistry(context.Background(), reg, nil, "user@example.com", "https://hub.example.com/api/llm/v1")
	if err != nil {
		t.Fatalf("ResolveStatusFromRegistry() error = %v", err)
	}
	if len(status.ServiceGroupIDs) != 1 || status.ServiceGroupIDs[0] != "team-default" {
		t.Fatalf("service groups = %#v, want default fallback", status.ServiceGroupIDs)
	}
	if !status.Active || status.DefaultModel != "gpt-default" {
		t.Fatalf("unexpected status from default fallback: %#v", status)
	}
}

func TestResolveStatusGlobalBindingOverridesDefaultServiceGroups(t *testing.T) {
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{
			{ID: "team-default", Name: "Team Default", AccessPolicy: AccessPolicyFree, Models: []ModelServiceModel{{Name: "gpt-default", ProviderIDs: []string{"provider-a"}}}},
			{ID: "global-svc", Name: "Global", AccessPolicy: AccessPolicyFree, Models: []ModelServiceModel{{Name: "gpt-global", ProviderIDs: []string{"provider-a"}}}},
		},
		GlobalServiceGroupIDs:       []string{"global-svc"},
		DefaultNewUserServiceGroups: []string{"team-default"},
	}
	reg.Normalize()

	status, _, err := ResolveStatusFromRegistry(context.Background(), reg, nil, "user@example.com", "https://hub.example.com/api/llm/v1")
	if err != nil {
		t.Fatalf("ResolveStatusFromRegistry() error = %v", err)
	}
	if len(status.ServiceGroupIDs) != 1 || status.ServiceGroupIDs[0] != "global-svc" {
		t.Fatalf("service groups = %#v, want global binding", status.ServiceGroupIDs)
	}
	if status.DefaultModel != "gpt-global" {
		t.Fatalf("default model = %q, want gpt-global", status.DefaultModel)
	}
}
func TestResolveStatusUserBindingOverridesDefaultServiceGroups(t *testing.T) {
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{
			{ID: "team-default", Name: "Team Default", AccessPolicy: AccessPolicyFree, Models: []ModelServiceModel{{Name: "gpt-default", ProviderIDs: []string{"provider-a"}}}},
			{ID: "vip", Name: "VIP", AccessPolicy: AccessPolicyFree, Models: []ModelServiceModel{{Name: "gpt-vip", ProviderIDs: []string{"provider-a"}}}},
		},
		UserBindings:                []UserBinding{{Email: "user@example.com", ServiceGroupIDs: []string{"vip"}}},
		DefaultNewUserServiceGroups: []string{"team-default"},
	}
	reg.Normalize()

	status, _, err := ResolveStatusFromRegistry(context.Background(), reg, nil, "user@example.com", "https://hub.example.com/api/llm/v1")
	if err != nil {
		t.Fatalf("ResolveStatusFromRegistry() error = %v", err)
	}
	if len(status.ServiceGroupIDs) != 1 || status.ServiceGroupIDs[0] != "vip" {
		t.Fatalf("service groups = %#v, want user binding only", status.ServiceGroupIDs)
	}
	if status.DefaultModel != "gpt-vip" {
		t.Fatalf("default model = %q, want gpt-vip", status.DefaultModel)
	}
}

type fakeLLMServiceGroupResolver struct {
	chain []string
	err   error
}

func (f fakeLLMServiceGroupResolver) ResolveUserGroupChain(ctx context.Context, email string) ([]string, error) {
	return f.chain, f.err
}

func TestEffectiveServiceGroupsGroupBindingOverridesGlobalAndDefault(t *testing.T) {
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{
			{ID: "team-default", Name: "Team Default", AccessPolicy: AccessPolicyFree},
			{ID: "global-svc", Name: "Global", AccessPolicy: AccessPolicyFree},
			{ID: "dept-svc", Name: "Department", AccessPolicy: AccessPolicyFree},
		},
		GlobalServiceGroupIDs:       []string{"global-svc"},
		GroupBindings:               []GroupBinding{{GroupID: "dept", ServiceGroupIDs: []string{"dept-svc"}}},
		DefaultNewUserServiceGroups: []string{"team-default"},
	}
	reg.Normalize()

	serviceGroupIDs, _, err := effectiveServiceGroupIDs(context.Background(), reg, fakeLLMServiceGroupResolver{chain: []string{"dept"}}, "user@example.com", time.Now().UTC())
	if err != nil {
		t.Fatalf("effectiveServiceGroupIDs() error = %v", err)
	}
	if len(serviceGroupIDs) != 1 || serviceGroupIDs[0] != "dept-svc" {
		t.Fatalf("service groups = %#v, want department binding over global/default", serviceGroupIDs)
	}
}

func TestEffectiveServiceGroupsUserBindingOverridesGroupGlobalAndDefault(t *testing.T) {
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{
			{ID: "team-default", Name: "Team Default", AccessPolicy: AccessPolicyFree},
			{ID: "global-svc", Name: "Global", AccessPolicy: AccessPolicyFree},
			{ID: "dept-svc", Name: "Department", AccessPolicy: AccessPolicyFree},
			{ID: "user-svc", Name: "User", AccessPolicy: AccessPolicyFree},
		},
		GlobalServiceGroupIDs:       []string{"global-svc"},
		GroupBindings:               []GroupBinding{{GroupID: "dept", ServiceGroupIDs: []string{"dept-svc"}}},
		UserBindings:                []UserBinding{{Email: "user@example.com", ServiceGroupIDs: []string{"user-svc"}}},
		DefaultNewUserServiceGroups: []string{"team-default"},
	}
	reg.Normalize()

	serviceGroupIDs, _, err := effectiveServiceGroupIDs(context.Background(), reg, fakeLLMServiceGroupResolver{chain: []string{"dept"}}, "user@example.com", time.Now().UTC())
	if err != nil {
		t.Fatalf("effectiveServiceGroupIDs() error = %v", err)
	}
	if len(serviceGroupIDs) != 1 || serviceGroupIDs[0] != "user-svc" {
		t.Fatalf("service groups = %#v, want user binding over group/global/default", serviceGroupIDs)
	}
}

func TestResolveStatusClosestGroupBindingOverridesAncestorAndDefault(t *testing.T) {
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{
			{ID: "team-default", Name: "Team Default", AccessPolicy: AccessPolicyFree, Models: []ModelServiceModel{{Name: "gpt-default", ProviderIDs: []string{"provider-a"}}}},
			{ID: "parent-svc", Name: "Parent", AccessPolicy: AccessPolicyFree, Models: []ModelServiceModel{{Name: "gpt-parent", ProviderIDs: []string{"provider-a"}}}},
			{ID: "child-svc", Name: "Child", AccessPolicy: AccessPolicyFree, Models: []ModelServiceModel{{Name: "gpt-child", ProviderIDs: []string{"provider-a"}}}},
		},
		GroupBindings: []GroupBinding{
			{GroupID: "parent", ServiceGroupIDs: []string{"parent-svc"}},
			{GroupID: "child", ServiceGroupIDs: []string{"child-svc"}},
		},
		DefaultNewUserServiceGroups: []string{"team-default"},
	}
	reg.Normalize()

	serviceGroupIDs, _, err := effectiveServiceGroupIDs(context.Background(), reg, fakeLLMServiceGroupResolver{chain: []string{"child", "parent"}}, "user@example.com", time.Now().UTC())
	if err != nil {
		t.Fatalf("effectiveServiceGroupIDs() error = %v", err)
	}
	if len(serviceGroupIDs) != 1 || serviceGroupIDs[0] != "child-svc" {
		t.Fatalf("service groups = %#v, want closest group binding", serviceGroupIDs)
	}
}

func TestAppliedGroupBindingsReturnsClosestKnownBinding(t *testing.T) {
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{
			{ID: "parent-svc", Name: "Parent"},
			{ID: "child-svc", Name: "Child"},
		},
		GroupBindings: []GroupBinding{
			{GroupID: "parent", ServiceGroupIDs: []string{"parent-svc"}},
			{GroupID: "child", ServiceGroupIDs: []string{"missing-svc", "child-svc"}},
		},
	}
	reg.Normalize()

	bindings := appliedGroupBindings(reg, []string{"child", "parent"})
	if len(bindings) != 1 {
		t.Fatalf("expected one applied group binding, got %#v", bindings)
	}
	if bindings[0].GroupID != "child" {
		t.Fatalf("group id = %q, want child", bindings[0].GroupID)
	}
	if len(bindings[0].ServiceGroupIDs) != 1 || bindings[0].ServiceGroupIDs[0] != "child-svc" {
		t.Fatalf("service group ids = %#v, want child-svc only", bindings[0].ServiceGroupIDs)
	}
}

func TestAppliedGroupBindingsFallsBackToAncestorWhenClosestBindingInvalid(t *testing.T) {
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "parent-svc", Name: "Parent"}},
		GroupBindings: []GroupBinding{
			{GroupID: "parent", ServiceGroupIDs: []string{"parent-svc"}},
			{GroupID: "child", ServiceGroupIDs: []string{"missing-svc"}},
		},
	}
	reg.Normalize()

	bindings := appliedGroupBindings(reg, []string{"child", "parent"})
	if len(bindings) != 1 || bindings[0].GroupID != "parent" {
		t.Fatalf("expected parent fallback binding, got %#v", bindings)
	}
}

func TestExplainEntitlementDiagnosticFiltersInvalidDirectUserBindingRefs(t *testing.T) {
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "vip", Name: "VIP", AccessPolicy: AccessPolicyFree, Models: []ModelServiceModel{{Name: "gpt-vip", ProviderIDs: []string{"provider-a"}}}}},
		UserBindings:       []UserBinding{{Email: "user@example.com", ServiceGroupIDs: []string{"missing", "vip"}}},
	}
	reg.Normalize()

	diag, err := ExplainEntitlementDiagnosticFromRegistry(context.Background(), reg, nil, "user@example.com", "https://hub.example.com/api/llm/v1")
	if err != nil {
		t.Fatalf("ExplainEntitlementDiagnosticFromRegistry() error = %v", err)
	}
	if len(diag.DirectUserBindings) != 1 {
		t.Fatalf("direct user bindings = %#v, want one", diag.DirectUserBindings)
	}
	if got := diag.DirectUserBindings[0].ServiceGroupIDs; len(got) != 1 || got[0] != "vip" {
		t.Fatalf("direct user binding service groups = %#v, want vip only", got)
	}
}

func TestRegistryNormalizeMergesDuplicateServiceBindings(t *testing.T) {
	reg := &Registry{
		GroupBindings: []GroupBinding{
			{GroupID: " Ops ", ServiceGroupIDs: []string{"svc-a"}},
			{GroupID: "ops", ServiceGroupIDs: []string{"svc-b", "svc-a"}},
			{GroupID: "empty", ServiceGroupIDs: []string{""}},
		},
		UserBindings: []UserBinding{
			{Email: " Lead@Example.COM ", ServiceGroupIDs: []string{"svc-a"}},
			{Email: "lead@example.com", ServiceGroupIDs: []string{"svc-c", "svc-a"}},
			{Email: "blank@example.com", ServiceGroupIDs: nil},
		},
	}
	reg.Normalize()

	if len(reg.GroupBindings) != 1 {
		t.Fatalf("group bindings = %#v, want one merged binding", reg.GroupBindings)
	}
	if reg.GroupBindings[0].GroupID != "Ops" {
		t.Fatalf("group id = %q, want first normalized group id", reg.GroupBindings[0].GroupID)
	}
	if got := reg.GroupBindings[0].ServiceGroupIDs; len(got) != 2 || got[0] != "svc-a" || got[1] != "svc-b" {
		t.Fatalf("group service ids = %#v, want svc-a, svc-b", got)
	}
	if len(reg.UserBindings) != 1 {
		t.Fatalf("user bindings = %#v, want one merged binding", reg.UserBindings)
	}
	if reg.UserBindings[0].Email != "lead@example.com" {
		t.Fatalf("email = %q, want normalized email", reg.UserBindings[0].Email)
	}
	if got := reg.UserBindings[0].ServiceGroupIDs; len(got) != 2 || got[0] != "svc-a" || got[1] != "svc-c" {
		t.Fatalf("user service ids = %#v, want svc-a, svc-c", got)
	}
}
