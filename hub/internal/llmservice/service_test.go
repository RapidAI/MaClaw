package llmservice

import (
	"context"
	"errors"
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
		ResetVouchers: []ResetVoucher{
			{ID: "voucher-user-id", UserID: "user-123", Email: "old@example.com"},
			{ID: "voucher-phone", Email: "phone:19900001112"},
			{ID: "voucher-other", UserID: "other-user", Email: "other@example.com"},
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
	if len(got.ResetVouchers) != 1 || got.ResetVouchers[0].UserID != "other-user" {
		t.Fatalf("unexpected reset vouchers after purge: %#v", got.ResetVouchers)
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

func TestBillingReservationReducesAdmissionAvailabilityAndReleases(t *testing.T) {
	now := time.Now().UTC()
	reg := &Registry{Grants: []Grant{{
		ID: "g", UserID: "user", Email: "user@example.com", ServiceGroupID: "coding",
		StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), CreditsTotal: 10,
	}}}
	reserved, ok := ReserveBillingCreditsForUserID(reg, "user", "user@example.com", []string{"coding"}, "request-1", 7, now.Add(time.Minute), now)
	if !ok || reserved != 7 {
		t.Fatalf("reservation = %v, %v", reserved, ok)
	}
	if got := AvailableCreditsForServiceGroupsForUserID(reg, "user", "user@example.com", []string{"coding"}, now); got != 3 {
		t.Fatalf("available after reservation = %v, want 3", got)
	}
	if _, ok := ReserveBillingCreditsForUserID(reg, "user", "user@example.com", []string{"coding"}, "request-2", 4, now.Add(time.Minute), now); ok {
		t.Fatal("second reservation exceeded remaining balance")
	}
	if !ReleaseBillingReservation(reg, "request-1", now) {
		t.Fatal("reservation not released")
	}
	if got := AvailableCreditsForServiceGroupsForUserID(reg, "user", "user@example.com", []string{"coding"}, now); got != 10 {
		t.Fatalf("available after release = %v, want 10", got)
	}
}

func TestExpiredSentBillingReservationRemainsUntilSettlement(t *testing.T) {
	now := time.Now().UTC()
	reg := &Registry{Grants: []Grant{{
		ID: "g", UserID: "user", Email: "user@example.com", ServiceGroupID: "coding",
		StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), CreditsTotal: 10,
	}}}
	if _, ok := ReserveBillingCreditsForUserID(reg, "user", "user@example.com", []string{"coding"}, "request-sent", 7, now.Add(time.Minute), now); !ok {
		t.Fatal("reserve")
	}
	if !MarkBillingReservationSent(reg, "request-sent", now) {
		t.Fatal("mark sent")
	}
	later := now.Add(2 * time.Minute)
	pruneExpiredBillingReservations(reg, later)
	if len(reg.BillingReservations) != 1 || reg.BillingReservations[0].SentAt.IsZero() {
		t.Fatalf("sent reservation was expired: %#v", reg.BillingReservations)
	}
	if got := AvailableCreditsForServiceGroupsForUserID(reg, "user", "user@example.com", []string{"coding"}, later); got != 3 {
		t.Fatalf("available after sent quote expiry = %v, want 3", got)
	}
	if !ReleaseBillingReservation(reg, "request-sent", later) {
		t.Fatal("settlement release")
	}
}

func TestSentBillingReservationKeepsFrozenOfficialDetails(t *testing.T) {
	now := time.Now().UTC()
	reg := &Registry{Grants: []Grant{{ID: "g", UserID: "user", Email: "user@example.com", ServiceGroupID: "coding", StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), CreditsTotal: 10}}}
	if _, ok := ReserveBillingCreditsForUserID(reg, "user", "user@example.com", []string{"coding"}, "official-request", 2, now.Add(time.Minute), now); !ok {
		t.Fatal("reserve")
	}
	if !MarkBillingReservationSent(reg, "official-request", now) || !SetBillingReservationBillingDetails(reg, "official-request", MaClawOfficialProviderID, 0.5, 2) {
		t.Fatal("freeze official reservation details")
	}
	items := SentBillingReservationsForUserID(reg, "user", "user@example.com")
	if len(items) != 1 || items[0].ProviderID != MaClawOfficialProviderID || items[0].ProviderMultiplier != 0.5 || items[0].BillingGroupMultiplier != 2 {
		t.Fatalf("sent reservations=%#v", items)
	}
}

func TestSentLocalBillingReservationAgesOutAsUsageUnresolved(t *testing.T) {
	now := time.Now().UTC()
	reg := &Registry{Grants: []Grant{{
		ID: "g", UserID: "user", Email: "user@example.com", ServiceGroupID: "coding",
		StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(2 * time.Hour), CreditsTotal: 10,
	}}}
	if _, ok := ReserveBillingCreditsForUserID(reg, "user", "user@example.com", []string{"coding"}, "local-request", 7, now.Add(time.Minute), now); !ok {
		t.Fatal("reserve")
	}
	if !MarkBillingReservationSent(reg, "local-request", now) ||
		!SetBillingReservationBillingDetails(reg, "local-request", "third-party", 1, 1) {
		t.Fatal("freeze local reservation details")
	}
	// Inside the recovery window the hold still counts and no marker is set.
	within := now.Add(SentLocalBillingReservationMaxAge - time.Minute)
	pruneExpiredBillingReservations(reg, within)
	if len(reg.BillingReservations) != 1 || reg.BillingReservations[0].Status != "" {
		t.Fatalf("reservation marked inside recovery window: %#v", reg.BillingReservations)
	}
	if got := AvailableCreditsForServiceGroupsForUserID(reg, "user", "user@example.com", []string{"coding"}, within); got != 3 {
		t.Fatalf("available inside recovery window = %v, want 3", got)
	}
	// Past the window the response is considered lost: the hold is released
	// from the balance while the row stays as usage_unresolved evidence (design
	// §8) instead of being silently deleted.
	later := now.Add(SentLocalBillingReservationMaxAge + time.Minute)
	pruneExpiredBillingReservations(reg, later)
	if len(reg.BillingReservations) != 1 || reg.BillingReservations[0].Status != BillingReservationUsageUnresolved {
		t.Fatalf("sent local reservation not marked usage_unresolved: %#v", reg.BillingReservations)
	}
	if got := AvailableCreditsForServiceGroupsForUserID(reg, "user", "user@example.com", []string{"coding"}, later); got != 10 {
		t.Fatalf("available after usage_unresolved = %v, want 10", got)
	}
	if items := SentBillingReservations(reg); len(items) != 0 {
		t.Fatalf("terminal reservation still scanned for reconciliation: %#v", items)
	}
	// A late settlement may still finalize the request; the marker row is then
	// released by the normal path.
	if !ReleaseBillingReservation(reg, "local-request", later) {
		t.Fatal("late settlement release")
	}
	if len(reg.BillingReservations) != 0 {
		t.Fatalf("marker row survived settlement: %#v", reg.BillingReservations)
	}
}

func TestUsageUnresolvedReservationExpiresAfterRetentionAndDoesNotReviveHold(t *testing.T) {
	now := time.Now().UTC()
	reg := &Registry{Grants: []Grant{{
		ID: "g", UserID: "user", Email: "user@example.com", ServiceGroupID: "coding",
		StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(90 * 24 * time.Hour), CreditsTotal: 10,
	}}}
	if _, ok := ReserveBillingCreditsForUserID(reg, "user", "user@example.com", []string{"coding"}, "lost-request", 7, now.Add(time.Minute), now); !ok {
		t.Fatal("reserve")
	}
	if !MarkBillingReservationSent(reg, "lost-request", now) ||
		!SetBillingReservationBillingDetails(reg, "lost-request", "third-party", 1, 1) {
		t.Fatal("freeze local reservation details")
	}
	aged := now.Add(SentLocalBillingReservationMaxAge + time.Minute)
	pruneExpiredBillingReservations(reg, aged)
	if len(reg.BillingReservations) != 1 || reg.BillingReservations[0].Status != BillingReservationUsageUnresolved {
		t.Fatalf("reservation not marked usage_unresolved: %#v", reg.BillingReservations)
	}
	// A retry reusing the same request ID must not resurrect the released hold
	// as if it were still live: the terminal row is skipped and a fresh hold is
	// established against the (already released) balance instead.
	if held, ok := ReserveBillingCreditsForUserID(reg, "user", "user@example.com", []string{"coding"}, "lost-request", 7, aged.Add(time.Minute), aged); !ok || held != 7 {
		t.Fatalf("retry after usage_unresolved must establish a fresh hold: held=%v ok=%v", held, ok)
	}
	live, terminal := 0, 0
	for _, reservation := range reg.BillingReservations {
		if reservation.Status == "" {
			live++
		} else {
			terminal++
		}
	}
	if live != 1 || terminal != 1 {
		t.Fatalf("expected one fresh hold plus the terminal evidence row: %#v", reg.BillingReservations)
	}
	// The fresh hold counts against the balance again.
	if got := AvailableCreditsForServiceGroupsForUserID(reg, "user", "user@example.com", []string{"coding"}, aged); got != 3 {
		t.Fatalf("available after retry = %v, want 3", got)
	}
	// Past the retention window the evidence row is dropped so the registry
	// cannot grow without bound on lost responses.
	pruneExpiredBillingReservations(reg, now.Add(BillingReservationTerminalRetention+time.Hour))
	if len(reg.BillingReservations) != 0 {
		t.Fatalf("terminal reservation kept past retention: %#v", reg.BillingReservations)
	}
}

func TestSentOfficialBillingReservationNeverAgesOutLocally(t *testing.T) {
	now := time.Now().UTC()
	reg := &Registry{Grants: []Grant{{
		ID: "g", UserID: "user", Email: "user@example.com", ServiceGroupID: "coding",
		StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(3 * time.Hour), CreditsTotal: 10,
	}}}
	if _, ok := ReserveBillingCreditsForUserID(reg, "user", "user@example.com", []string{"coding"}, "official-request", 2, now.Add(time.Minute), now); !ok {
		t.Fatal("reserve")
	}
	if !MarkBillingReservationSent(reg, "official-request", now) ||
		!SetBillingReservationBillingDetails(reg, "official-request", MaClawOfficialProviderID, 1, 1) {
		t.Fatal("freeze official reservation details")
	}
	// Official reservations keep waiting for HubCenter's authenticated
	// reconciliation no matter how old the dispatch is.
	later := now.Add(SentLocalBillingReservationMaxAge + time.Hour)
	pruneExpiredBillingReservations(reg, later)
	if len(reg.BillingReservations) != 1 || reg.BillingReservations[0].Status != "" {
		t.Fatalf("official reservation aged out locally: %#v", reg.BillingReservations)
	}
	if got := AvailableCreditsForServiceGroupsForUserID(reg, "user", "user@example.com", []string{"coding"}, later); got != 8 {
		t.Fatalf("available for old official reservation = %v, want 8", got)
	}
	if items := SentBillingReservations(reg); len(items) != 1 {
		t.Fatalf("official reservation dropped from reconciliation scan: %#v", items)
	}
}

func TestSentBillingReservationsIncludesEveryOwner(t *testing.T) {
	now := time.Now().UTC()
	reg := &Registry{BillingReservations: []BillingReservation{
		{RequestID: "sent-a", UserID: "user-a", ServiceGroupIDs: []string{"group-a"}, SentAt: now},
		{RequestID: "sent-b", UserID: "user-b", ServiceGroupIDs: []string{"group-b"}, SentAt: now},
		{RequestID: "unsent", UserID: "user-c", ServiceGroupIDs: []string{"group-c"}},
	}}
	items := SentBillingReservations(reg)
	if len(items) != 2 {
		t.Fatalf("sent reservations = %#v, want both sent entries", items)
	}
	items[0].ServiceGroupIDs[0] = "mutated"
	if reg.BillingReservations[0].ServiceGroupIDs[0] == "mutated" {
		t.Fatal("returned reservations must not alias registry service groups")
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

func TestGrantDefaultServiceForNewUserIssuesBindingLimitCard(t *testing.T) {
	ctx := context.Background()
	system := newTestSystemSettings()
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{
			{ID: "welcome", Name: "Welcome", AccessPolicy: AccessPolicyFree},
			{ID: "recharge", Name: "Recharge", AccessPolicy: AccessPolicyGrantRequired},
		},
		DefaultNewUserLimitCard: NewUserLimitCard{
			ServiceGroupIDs: []string{"welcome", "recharge"},
			PeriodLimits:    CreditPeriodLimits{FiveHour: 10, Daily: 25},
		},
		DefaultNewUserBenefitMode: NewUserBenefitModeLimitCard,
	}
	if err := SaveRegistry(ctx, system, reg); err != nil {
		t.Fatal(err)
	}
	if err := GrantDefaultServiceForNewUserID(ctx, system, "user-1", "user@example.com"); err != nil {
		t.Fatal(err)
	}
	saved, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatal(err)
	}
	var card *Grant
	limitCardCount := 0
	for i := range saved.Grants {
		if saved.Grants[i].Source == "new_user_limit_card" {
			limitCardCount++
			if saved.Grants[i].ServiceGroupID == "welcome" {
				card = &saved.Grants[i]
			}
		}
	}
	if limitCardCount != 2 || card == nil || !card.ExpiresAt.IsZero() || card.Permanent || card.PeriodLimits != (CreditPeriodLimits{}) || card.RollingFiveHour || card.AnchoredFiveHour {
		t.Fatalf("unexpected new-user limit card: %#v", card)
	}
	effective := effectiveGrantForRegistry(saved, *card)
	if !effective.Permanent || effective.PeriodLimits.FiveHour != 10 || effective.PeriodLimits.Daily != 25 || effective.RollingFiveHour || !effective.AnchoredFiveHour {
		t.Fatalf("limit-card view did not apply current settings: %#v", effective)
	}
	now := time.Now().UTC()
	if allowed, _, code, _, _, _, _ := BillingEligibilityForServiceGroupsForUserID(saved, "user-1", "user@example.com", []string{"welcome"}, now); !allowed || code != "" {
		t.Fatalf("limit-card welcome group should be allowed, code=%q", code)
	}
	if got := ApplyCreditUsageToRegistryForUserID(saved, "user-1", "user@example.com", []string{"welcome"}, 11, now); got != 10 {
		t.Fatalf("limit-card usage = %v, want 10", got)
	}
	if allowed, _, code, _, _, _, _ := BillingEligibilityForServiceGroupsForUserID(saved, "user-1", "user@example.com", []string{"welcome"}, now); allowed || code != "LLM_SERVICE_PERIOD_LIMITED" {
		t.Fatalf("limit-card exhaustion should block use, allowed=%v code=%q", allowed, code)
	}
}

func TestNewUserBenefitModeIssuesExactlyOneBenefit(t *testing.T) {
	ctx := context.Background()
	t.Run("credits", func(t *testing.T) {
		system := newTestSystemSettings()
		if err := SaveRegistry(ctx, system, &Registry{
			ModelServiceGroups: []ModelServiceGroup{
				{ID: "credits", Name: "Credits", AccessPolicy: AccessPolicyGrantRequired},
				{ID: "welcome", Name: "Welcome", AccessPolicy: AccessPolicyFree},
			},
			DefaultNewUserServiceGroups: []string{"credits"},
			DefaultNewUserLimitCard:     NewUserLimitCard{ServiceGroupIDs: []string{"welcome"}, PeriodLimits: CreditPeriodLimits{FiveHour: 10}},
			DefaultNewUserBenefitMode:   NewUserBenefitModeCredits,
		}); err != nil {
			t.Fatal(err)
		}
		if err := GrantDefaultServiceForNewUser(ctx, system, "credits@example.com"); err != nil {
			t.Fatal(err)
		}
		saved, err := LoadRegistry(ctx, system)
		if err != nil {
			t.Fatal(err)
		}
		if len(saved.Grants) != 1 || saved.Grants[0].Source != "new_user_default" {
			t.Fatalf("credits mode must issue only the Credits benefit: %#v", saved.Grants)
		}
	})

	t.Run("limit card", func(t *testing.T) {
		system := newTestSystemSettings()
		if err := SaveRegistry(ctx, system, &Registry{
			ModelServiceGroups: []ModelServiceGroup{
				{ID: "credits", Name: "Credits", AccessPolicy: AccessPolicyGrantRequired},
				{ID: "welcome", Name: "Welcome", AccessPolicy: AccessPolicyFree},
			},
			DefaultNewUserServiceGroups: []string{"credits"},
			DefaultNewUserLimitCard:     NewUserLimitCard{ServiceGroupIDs: []string{"welcome"}, PeriodLimits: CreditPeriodLimits{FiveHour: 10}},
			DefaultNewUserBenefitMode:   NewUserBenefitModeLimitCard,
		}); err != nil {
			t.Fatal(err)
		}
		if err := GrantDefaultServiceForNewUser(ctx, system, "card@example.com"); err != nil {
			t.Fatal(err)
		}
		if err := GrantEmailConfirmedBenefitForUser(ctx, system, "card@example.com"); err != nil {
			t.Fatal(err)
		}
		if err := GrantPhoneVerifiedBenefitForUser(ctx, system, "card@example.com"); err != nil {
			t.Fatal(err)
		}
		saved, err := LoadRegistry(ctx, system)
		if err != nil {
			t.Fatal(err)
		}
		if len(saved.Grants) != 1 || saved.Grants[0].Source != "new_user_limit_card" || saved.Grants[0].CreditsTotal != 0 {
			t.Fatalf("limit-card mode must issue no Credits grants: %#v", saved.Grants)
		}
	})
}

func TestNewUserBenefitModeDefaultsToCredits(t *testing.T) {
	reg := &Registry{DefaultNewUserBenefitMode: "unexpected"}
	reg.Normalize()
	if reg.NewUserBenefitMode() != NewUserBenefitModeCredits {
		t.Fatalf("invalid mode = %q, want %q", reg.NewUserBenefitMode(), NewUserBenefitModeCredits)
	}
}

func TestNewUserBenefitModeIsPersistedForNewRegistry(t *testing.T) {
	ctx := context.Background()
	system := newTestSystemSettings()
	if err := SaveRegistry(ctx, system, &Registry{}); err != nil {
		t.Fatal(err)
	}
	raw, err := system.Get(ctx, RegistryKey)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"default_new_user_benefit_mode":"credits"`) {
		t.Fatalf("default benefit mode was not persisted: %s", raw)
	}
}

func TestNewUserLimitCardExistingQualificationUsesCurrentPolicy(t *testing.T) {
	ctx := context.Background()
	system := newTestSystemSettings()
	reg := &Registry{
		ModelServiceGroups:        []ModelServiceGroup{{ID: "welcome", Name: "Welcome", AccessPolicy: AccessPolicyFree}},
		DefaultNewUserBenefitMode: NewUserBenefitModeLimitCard,
		DefaultNewUserLimitCard: NewUserLimitCard{
			ServiceGroupIDs: []string{"welcome"},
			DurationDays:    30,
			PeriodLimits:    CreditPeriodLimits{FiveHour: 10, Daily: 20},
		},
	}
	if err := SaveRegistry(ctx, system, reg); err != nil {
		t.Fatal(err)
	}
	if err := GrantDefaultServiceForNewUserID(ctx, system, "user-1", "user@example.com"); err != nil {
		t.Fatal(err)
	}
	issued, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatal(err)
	}
	if len(issued.Grants) != 1 {
		t.Fatalf("issued grants = %#v", issued.Grants)
	}
	startedAt := issued.Grants[0].StartsAt
	if !issued.Grants[0].ExpiresAt.IsZero() || issued.Grants[0].Permanent || issued.Grants[0].RollingFiveHour || issued.Grants[0].PeriodLimits != (CreditPeriodLimits{}) {
		t.Fatalf("qualification persisted a policy snapshot: %#v", issued.Grants[0])
	}
	if got := ApplyCreditUsageToRegistry(issued, "user@example.com", []string{"welcome"}, 7, startedAt.Add(time.Hour)); got != 7 {
		t.Fatalf("initial usage = %v, want 7", got)
	}
	issued.DefaultNewUserLimitCard.DurationDays = 0
	issued.DefaultNewUserLimitCard.PeriodLimits = CreditPeriodLimits{FiveHour: 50, Daily: 80}
	if err := SaveRegistry(ctx, system, issued); err != nil {
		t.Fatal(err)
	}

	updated, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatal(err)
	}
	grant := updated.Grants[0]
	if !grant.ExpiresAt.IsZero() || grant.Permanent || grant.RollingFiveHour || grant.PeriodLimits != (CreditPeriodLimits{}) || len(grant.UsageEvents) != 0 {
		t.Fatalf("qualification stored policy data after update: %#v", grant)
	}
	effective := effectiveGrantForRegistry(updated, grant)
	if !effective.Permanent || effective.ExpiresAt.Year() != 9999 || effective.RollingFiveHour || !effective.AnchoredFiveHour || effective.PeriodLimits.FiveHour != 50 || effective.PeriodLimits.Daily != 80 {
		t.Fatalf("qualification view did not follow current policy: %#v", effective)
	}

	updated.DefaultNewUserLimitCard.DurationDays = 5
	if err := SaveRegistry(ctx, system, updated); err != nil {
		t.Fatal(err)
	}
	expiring, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatal(err)
	}
	if grant := expiring.Grants[0]; !grant.ExpiresAt.IsZero() || grant.Permanent || grant.PeriodLimits != (CreditPeriodLimits{}) {
		t.Fatalf("qualification should remain policy-free in storage: %#v", grant)
	}
	effective = effectiveGrantForRegistry(expiring, expiring.Grants[0])
	if got := effective.ExpiresAt; !got.Equal(startedAt.UTC().AddDate(0, 0, 5)) || effective.Permanent {
		t.Fatalf("current duration did not reapply from qualification start: effective=%#v want expiry=%s", effective, startedAt.UTC().AddDate(0, 0, 5))
	}
}

func TestNewUserLimitCardUsesAnchoredFiveHourWindow(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	reg := &Registry{
		ModelServiceGroups:      []ModelServiceGroup{{ID: "welcome", AccessPolicy: AccessPolicyFree}},
		DefaultNewUserLimitCard: NewUserLimitCard{ServiceGroupIDs: []string{"welcome"}, PeriodLimits: CreditPeriodLimits{FiveHour: 10}},
		Grants:                  []Grant{{ID: "card", Email: "user@example.com", ServiceGroupID: "welcome", Source: "new_user_limit_card", StartsAt: now.Add(-time.Hour)}},
	}
	if got := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"welcome"}, 10, now); got != 10 {
		t.Fatalf("first usage = %v, want 10", got)
	}
	if got := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"welcome"}, 1, now.Add(4*time.Hour+59*time.Minute)); got != 0 {
		t.Fatalf("usage before anchored expiry = %v, want 0", got)
	}
	if got := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"welcome"}, 1, now.Add(5*time.Hour)); got != 1 {
		t.Fatalf("usage after anchored expiry = %v, want 1", got)
	}
	// A later request must not move the next reset. The window remains anchored
	// to the original first use (10:00), so it resets again at 20:00.
	if got := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"welcome"}, 9, now.Add(6*time.Hour)); got != 9 {
		t.Fatalf("usage within second anchored window = %v, want 9", got)
	}
	if got := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"welcome"}, 1, now.Add(9*time.Hour+59*time.Minute)); got != 0 {
		t.Fatalf("usage before second anchored expiry = %v, want 0", got)
	}
	if got := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"welcome"}, 1, now.Add(10*time.Hour)); got != 1 {
		t.Fatalf("usage after second anchored expiry = %v, want 1", got)
	}
}

func TestNewUserLimitCardAnchorSurvivesNormalization(t *testing.T) {
	anchor := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	reg := &Registry{
		ModelServiceGroups:      []ModelServiceGroup{{ID: "welcome", AccessPolicy: AccessPolicyFree}},
		DefaultNewUserLimitCard: NewUserLimitCard{ServiceGroupIDs: []string{"welcome"}, PeriodLimits: CreditPeriodLimits{FiveHour: 10}},
		Grants:                  []Grant{{ID: "card", Email: "user@example.com", ServiceGroupID: "welcome", Source: "new_user_limit_card", StartsAt: anchor.Add(-time.Hour)}},
	}
	if got := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"welcome"}, 10, anchor); got != 10 {
		t.Fatalf("first usage = %v, want 10", got)
	}
	reg.Normalize()
	if got := reg.Grants[0].PeriodUsage.FiveHour.WindowStart; !got.Equal(anchor) {
		t.Fatalf("anchor changed during normalization: got %s want %s", got, anchor)
	}
	if got := AvailableCreditsForServiceGroups(reg, "user@example.com", []string{"welcome"}, anchor.Add(4*time.Hour+59*time.Minute)); got != 0 {
		t.Fatalf("credits before persisted anchor expiry = %v, want 0", got)
	}
	if got := AvailableCreditsForServiceGroups(reg, "user@example.com", []string{"welcome"}, anchor.Add(5*time.Hour)); got != 10 {
		t.Fatalf("credits after persisted anchor expiry = %v, want 10", got)
	}
}

func TestPermanentNewUserLimitCardDoesNotExpireWithLegacyStoredDate(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	reg := &Registry{
		ModelServiceGroups:      []ModelServiceGroup{{ID: "welcome", AccessPolicy: AccessPolicyFree}},
		DefaultNewUserLimitCard: NewUserLimitCard{ServiceGroupIDs: []string{"welcome"}, PeriodLimits: CreditPeriodLimits{FiveHour: 10}},
		Grants: []Grant{{
			ID: "card", Email: "user@example.com", ServiceGroupID: "welcome", Source: "new_user_limit_card",
			StartsAt: now.Add(-48 * time.Hour),
		}},
	}
	if allowed, _, code, _, available, _, _ := BillingEligibilityForServiceGroups(reg, "user@example.com", []string{"welcome"}, now); !allowed || code != "" || available != 10 {
		t.Fatalf("permanent limit card should remain billable, allowed=%v code=%q available=%v", allowed, code, available)
	}
	if got := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"welcome"}, 3, now); got != 3 {
		t.Fatalf("permanent limit-card usage = %v, want 3", got)
	}
}

func rechargeWelcomePointCardRegistry(now time.Time) *Registry {
	return &Registry{
		ModelServiceGroups:      []ModelServiceGroup{{ID: "redeem", Name: "充值服务组", AccessPolicy: AccessPolicyGrantRequired}},
		DefaultNewUserLimitCard: NewUserLimitCard{ServiceGroupIDs: []string{"redeem"}, PeriodLimits: CreditPeriodLimits{FiveHour: 10, Daily: 25}},
		Grants: []Grant{
			{ID: "paid", Email: "user@example.com", ServiceGroupID: "redeem", Source: "card", StartsAt: now.Add(-time.Hour), ExpiresAt: now.AddDate(1, 0, 0), CreditsTotal: 1000},
			{ID: "welcome", Email: "user@example.com", ServiceGroupID: "redeem", Source: "new_user_limit_card", StartsAt: now.Add(-time.Hour)},
		},
	}
}

func TestNewUserLimitCardOnRechargeGroupIsConsumedBeforePointCards(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	reg := rechargeWelcomePointCardRegistry(now)
	if available := AvailableCreditsForServiceGroups(reg, "user@example.com", []string{"redeem"}, now); available != 1010 {
		t.Fatalf("available before usage = %v, want 10 welcome + 1000 point card", available)
	}
	if got := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"redeem"}, 8, now); got != 8 {
		t.Fatalf("first usage = %v, want 8", got)
	}
	if reg.Grants[1].PeriodUsage.FiveHour.CreditsUsed != 8 || reg.Grants[0].CreditsUsed != 0 {
		t.Fatalf("welcome benefit must be charged first: paid=%#v welcome=%#v", reg.Grants[0], reg.Grants[1])
	}
	if available := AvailableCreditsForServiceGroups(reg, "user@example.com", []string{"redeem"}, now); available != 1002 {
		t.Fatalf("available after partial welcome usage = %v, want 2 welcome + 1000 point card", available)
	}
	if got := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"redeem"}, 5, now); got != 5 {
		t.Fatalf("overflow usage = %v, want 5", got)
	}
	if reg.Grants[1].PeriodUsage.FiveHour.CreditsUsed != 10 || reg.Grants[0].CreditsUsed != 3 {
		t.Fatalf("remaining welcome then point card: paid used=%v welcome five-hour=%v", reg.Grants[0].CreditsUsed, reg.Grants[1].PeriodUsage.FiveHour.CreditsUsed)
	}
	if allowed, _, code, _, available, _, _ := BillingEligibilityForServiceGroups(reg, "user@example.com", []string{"redeem"}, now); !allowed || code != "" || available < 997 {
		t.Fatalf("exhausted welcome period must fall back to point cards, allowed=%v code=%q available=%v", allowed, code, available)
	}
}

func TestNewUserLimitCardOnRechargeGroupOverflowsInOneCharge(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	reg := rechargeWelcomePointCardRegistry(now)
	if got := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"redeem"}, 15, now); got != 15 {
		t.Fatalf("usage = %v, want 15", got)
	}
	if reg.Grants[1].PeriodUsage.FiveHour.CreditsUsed != 10 || reg.Grants[0].CreditsUsed != 5 {
		t.Fatalf("one request must drain welcome then point card: paid used=%v welcome five-hour=%v", reg.Grants[0].CreditsUsed, reg.Grants[1].PeriodUsage.FiveHour.CreditsUsed)
	}
}

func TestDuplicateExhaustedNewUserLimitCardDoesNotMintSecondAllowanceOnRechargeGroup(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	reg := rechargeWelcomePointCardRegistry(now)
	reg.Grants = append([]Grant{{
		ID: "welcome-exhausted", Email: "user@example.com", ServiceGroupID: "redeem", Source: "new_user_limit_card",
		StartsAt:    now.Add(-time.Hour),
		PeriodUsage: CreditPeriodUsage{FiveHour: GrantUsageWindow{WindowStart: now, CreditsUsed: 10}},
	}}, reg.Grants...)
	if got := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"redeem"}, 5, now); got != 5 {
		t.Fatalf("usage = %v, want 5 from the point card", got)
	}
	if reg.Grants[2].PeriodUsage.FiveHour.CreditsUsed != 0 || reg.Grants[1].CreditsUsed != 5 {
		t.Fatalf("exhausted duplicate must not mint a second welcome allowance: %#v", reg.Grants)
	}
	if available := AvailableCreditsForServiceGroups(reg, "user@example.com", []string{"redeem"}, now); available != 995 {
		t.Fatalf("available after overflowing the exhausted duplicate = %v, want 995 point-card remainder", available)
	}
}

func TestDuplicateExhaustedNewUserLimitCardDoesNotMintSecondAllowanceOnFreeGroup(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	reg := &Registry{
		ModelServiceGroups:      []ModelServiceGroup{{ID: "welcome", AccessPolicy: AccessPolicyFree}},
		DefaultNewUserLimitCard: NewUserLimitCard{ServiceGroupIDs: []string{"welcome"}, PeriodLimits: CreditPeriodLimits{FiveHour: 10, Daily: 25}},
		Grants: []Grant{
			{
				ID: "exhausted", Email: "user@example.com", ServiceGroupID: "welcome", Source: "new_user_limit_card",
				StartsAt:    now.Add(-time.Hour),
				PeriodUsage: CreditPeriodUsage{FiveHour: GrantUsageWindow{WindowStart: now, CreditsUsed: 10}},
			},
			{
				ID: "duplicate", Email: "user@example.com", ServiceGroupID: "welcome", Source: "new_user_limit_card",
				StartsAt: now.Add(-time.Hour),
			},
		},
	}
	if got := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"welcome"}, 5, now); got != 0 {
		t.Fatalf("usage = %v, want 0", got)
	}
	if reg.Grants[1].PeriodUsage.FiveHour.CreditsUsed != 0 {
		t.Fatalf("duplicate qualification must not become a second allowance: %#v", reg.Grants)
	}
	if allowed, _, code, _, _, _, _ := BillingEligibilityForServiceGroups(reg, "user@example.com", []string{"welcome"}, now); allowed || code != "LLM_SERVICE_PERIOD_LIMITED" {
		t.Fatalf("exhausted duplicate must stay period-limited, allowed=%v code=%q", allowed, code)
	}
}

func TestNewUserLimitCardCannotBeBypassedByAnotherGrantOnSameFreeGroup(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	reg := &Registry{ModelServiceGroups: []ModelServiceGroup{{ID: "welcome", AccessPolicy: AccessPolicyFree}}, DefaultNewUserLimitCard: NewUserLimitCard{ServiceGroupIDs: []string{"welcome"}, PeriodLimits: CreditPeriodLimits{FiveHour: 10, Daily: 25}}, Grants: []Grant{
		{
			ID: "limit-card", Email: "user@example.com", ServiceGroupID: "welcome", Source: "new_user_limit_card",
			StartsAt: now.Add(-time.Hour),
		},
		{
			ID: "legacy-gift", Email: "user@example.com", ServiceGroupID: "welcome", Source: "new_user_default",
			StartsAt: now.Add(-time.Hour), ExpiresAt: now.AddDate(0, 0, 30), CreditsTotal: 100,
		},
	}}
	if got := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"welcome"}, 11, now); got != 10 {
		t.Fatalf("usage must be constrained by limit card, got %v want 10", got)
	}
	if reg.Grants[0].CreditsUsed != 10 || reg.Grants[1].CreditsUsed != 0 {
		t.Fatalf("usage must be charged only to the limit card: %#v", reg.Grants)
	}
	if available := AvailableCreditsForServiceGroups(reg, "user@example.com", []string{"welcome"}, now); available != 0 {
		t.Fatalf("legacy credits must not expand the active limit-card allowance, got %v", available)
	}
	if allowed, _, code, _, _, _, _ := BillingEligibilityForServiceGroups(reg, "user@example.com", []string{"welcome"}, now); allowed || code != "LLM_SERVICE_PERIOD_LIMITED" {
		t.Fatalf("exhausted limit card must still deny despite legacy credits, allowed=%v code=%q", allowed, code)
	}
}

func TestNewUserLimitCardCannotBeBypassedByAnotherGrantOnSameFreeGroupWhenDuplicated(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	reg := &Registry{ModelServiceGroups: []ModelServiceGroup{{ID: "welcome", AccessPolicy: AccessPolicyFree}}, DefaultNewUserLimitCard: NewUserLimitCard{ServiceGroupIDs: []string{"welcome"}, PeriodLimits: CreditPeriodLimits{FiveHour: 10}}, Grants: []Grant{
		{
			ID: "validity-only", Email: "user@example.com", ServiceGroupID: "welcome", Source: "new_user_limit_card",
			StartsAt: now.Add(-time.Hour),
		},
		{
			ID: "limit-card", Email: "user@example.com", ServiceGroupID: "welcome", Source: "new_user_limit_card",
			StartsAt: now.Add(-time.Hour),
		},
		{
			ID: "legacy-gift", Email: "user@example.com", ServiceGroupID: "welcome", Source: "new_user_default",
			StartsAt: now.Add(-time.Hour), ExpiresAt: now.AddDate(0, 0, 30), CreditsTotal: 100,
		},
	}}
	if got := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"welcome"}, 11, now); got != 10 {
		t.Fatalf("usage must stay constrained by the later limit card, got %v want 10", got)
	}
	if reg.Grants[0].CreditsUsed+reg.Grants[1].CreditsUsed != 10 || reg.Grants[2].CreditsUsed != 0 {
		t.Fatalf("usage must be charged to exactly one qualification, never the legacy grant: %#v", reg.Grants)
	}
	if available := AvailableCreditsForServiceGroups(reg, "user@example.com", []string{"welcome"}, now); available != 0 {
		t.Fatalf("duplicate qualifications must not expose a second allowance, got %v", available)
	}
	if summaries := creditGrantSummariesForOwner(reg, newUserAccountRef("", "user@example.com"), now); len(summaries) != 1 {
		t.Fatalf("duplicate qualifications must have one status-card summary, got %#v", summaries)
	}
}

func TestNewUserLimitCardLeavesOtherSelectedFreeGroupsIndependent(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	reg := &Registry{Grants: []Grant{
		{
			ID: "limit-card", Email: "user@example.com", ServiceGroupID: "welcome", Source: "new_user_limit_card",
			StartsAt: now.Add(-time.Hour), ExpiresAt: now.AddDate(1, 0, 0), Permanent: true, RollingFiveHour: true,
			PeriodLimits: CreditPeriodLimits{FiveHour: 10},
		},
		{
			ID: "other-grant", Email: "user@example.com", ServiceGroupID: "other", Source: "admin_grant",
			StartsAt: now.Add(-time.Hour), ExpiresAt: now.AddDate(0, 0, 30), CreditsTotal: 100,
		},
	}}
	if got := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"welcome", "other"}, 20, now); got != 20 {
		t.Fatalf("combined usage = %v, want 20", got)
	}
	if reg.Grants[0].CreditsUsed != 0 || reg.Grants[1].CreditsUsed != 20 {
		t.Fatalf("only the limit-card group must be constrained: %#v", reg.Grants)
	}
}

func TestNewUserLimitCardDoesNotBlockOtherFreeProviderRoute(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{
			{ID: "welcome", AccessPolicy: AccessPolicyFree},
			{ID: "fallback", AccessPolicy: AccessPolicyFree},
		},
		Grants: []Grant{{
			ID: "limit-card", Email: "user@example.com", ServiceGroupID: "welcome", Source: "new_user_limit_card",
			StartsAt:    now.Add(-time.Hour),
			UsageEvents: []CreditUsageEvent{{OccurredAt: now.Add(-time.Minute), CreditsUsed: 10}},
		}},
		DefaultNewUserLimitCard: NewUserLimitCard{ServiceGroupIDs: []string{"welcome"}, PeriodLimits: CreditPeriodLimits{FiveHour: 10}},
	}
	allowed, policy, code, _, _, _, _ := BillingEligibilityForServiceGroups(reg, "user@example.com", []string{"welcome", "fallback"}, now)
	if !allowed || policy != AccessPolicyFree || code != "" {
		t.Fatalf("independent free fallback must remain usable, allowed=%v policy=%q code=%q", allowed, policy, code)
	}
}

func TestNewUserLimitCardDoesNotConsumeCreditsThroughOtherFreeProviderRoute(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{
			{ID: "welcome", AccessPolicy: AccessPolicyFree},
			{ID: "fallback", AccessPolicy: AccessPolicyFree},
		},
		Grants: []Grant{{
			ID: "limit-card", Email: "user@example.com", ServiceGroupID: "welcome", Source: "new_user_limit_card",
			StartsAt: now.Add(-time.Hour),
		}},
		DefaultNewUserLimitCard: NewUserLimitCard{ServiceGroupIDs: []string{"welcome"}, PeriodLimits: CreditPeriodLimits{FiveHour: 10}},
	}
	if got := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"welcome", "fallback"}, 4, now); got != 0 {
		t.Fatalf("free fallback route must not consume the welcome limit card, got %v", got)
	}
	if used := reg.Grants[0].CreditsUsed; used != 0 {
		t.Fatalf("free fallback route changed welcome card usage to %v", used)
	}
	if got := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"welcome"}, 4, now); got != 4 {
		t.Fatalf("direct welcome route usage = %v, want 4", got)
	}
}

func TestNewUserLimitCardKeepsItsUsageOutOfLifetimeCreditTotals(t *testing.T) {
	now := time.Now().UTC()
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:           "welcome",
			AccessPolicy: AccessPolicyFree,
			Models:       []ModelServiceModel{{Name: "gpt-welcome", ProviderIDs: []string{"provider-a"}}},
		}},
		Grants: []Grant{{
			ID: "limit-card", Email: "user@example.com", ServiceGroupID: "welcome", Source: "new_user_limit_card",
			StartsAt: now.Add(-time.Hour),
		}},
		DefaultNewUserLimitCard: NewUserLimitCard{
			ServiceGroupIDs: []string{"welcome"},
			PeriodLimits:    CreditPeriodLimits{FiveHour: 10, Daily: 25},
		},
	}
	if got := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"welcome"}, 4, now); got != 4 {
		t.Fatalf("limit-card usage = %v, want 4", got)
	}
	status, _, err := ResolveStatusFromRegistry(context.Background(), reg, nil, "user@example.com", "https://hub.example.com/api/llm/v1")
	if err != nil {
		t.Fatalf("ResolveStatusFromRegistry: %v", err)
	}
	if !status.Active || status.CreditsTotal != 0 || status.CreditsUsed != 0 || status.CreditsRemaining != 0 || status.CreditsAvailable != 0 {
		t.Fatalf("limit card must retain unlimited lifetime totals and its own period availability: %#v", status)
	}
	if status.NearestExpiresAt != "" || status.EffectiveExpiresAt != "" {
		t.Fatalf("permanent limit card must not expose its storage sentinel as an expiry: %#v", status)
	}
	if len(status.CreditGrants) != 1 || status.CreditGrants[0].PeriodUsage == nil || status.CreditGrants[0].PeriodUsage.FiveHour.CreditsUsed != 4 {
		t.Fatalf("limit-card period usage is missing from status: %#v", status.CreditGrants)
	}
}

func TestNewUserLimitCardOnlyKeepsSupportedPeriodLimits(t *testing.T) {
	reg := &Registry{DefaultNewUserLimitCard: NewUserLimitCard{PeriodLimits: CreditPeriodLimits{
		FiveHour: 10,
		Daily:    25,
		Weekly:   50,
		Monthly:  100,
	}}}
	reg.Normalize()
	if got := reg.DefaultNewUserLimitCard.PeriodLimits; got.FiveHour != 10 || got.Daily != 25 || got.Weekly != 0 || got.Monthly != 0 {
		t.Fatalf("new-user limit-card period limits = %#v, want only five-hour and daily", got)
	}
}

func TestNewUserLimitCardViewAppliesToRechargeGroup(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "recharge", AccessPolicy: AccessPolicyGrantRequired}},
		DefaultNewUserLimitCard: NewUserLimitCard{
			ServiceGroupIDs: []string{"recharge"},
			PeriodLimits:    CreditPeriodLimits{FiveHour: 10, Daily: 20},
		},
		Grants: []Grant{{
			ID: "qualification", Email: "user@example.com", ServiceGroupID: "recharge", Source: "new_user_limit_card", StartsAt: now.Add(-time.Hour),
		}},
	}
	if effective := effectiveGrantForRegistry(reg, reg.Grants[0]); effective.PeriodLimits.FiveHour != 10 || effective.PeriodLimits.Daily != 20 || !effective.Permanent || effective.ExpiresAt.IsZero() {
		t.Fatalf("recharge group should receive a welcome-card policy projection: %#v", effective)
	}
	if allowed, policy, code, _, credits, _, _ := BillingEligibilityForServiceGroups(reg, "user@example.com", []string{"recharge"}, now); !allowed || policy != AccessPolicyGrantRequired || code != "" || credits != 10 {
		t.Fatalf("recharge group should be unlocked by a welcome qualification, allowed=%v policy=%q code=%q credits=%v", allowed, policy, code, credits)
	}
}

func TestNewUserLimitCardRemovedFromPolicyCannotFallBackToLegacySnapshot(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "welcome", AccessPolicy: AccessPolicyFree}},
		Grants: []Grant{{
			ID: "legacy", Email: "user@example.com", ServiceGroupID: "welcome", Source: "new_user_limit_card",
			StartsAt: now.Add(-time.Hour), ExpiresAt: now.AddDate(1, 0, 0), Permanent: true, RollingFiveHour: true,
			PeriodLimits: CreditPeriodLimits{FiveHour: 10, Daily: 20},
		}},
	}
	effective := effectiveGrantForRegistry(reg, reg.Grants[0])
	if !effective.ExpiresAt.IsZero() || effective.Permanent || effective.RollingFiveHour || effective.PeriodLimits != (CreditPeriodLimits{}) {
		t.Fatalf("removed qualification must not use its legacy snapshot: %#v", effective)
	}
	if allowed, policy, code, _, _, _, _ := BillingEligibilityForServiceGroups(reg, "user@example.com", []string{"welcome"}, now); !allowed || policy != AccessPolicyFree || code != "" {
		t.Fatalf("removed qualification must restore the free-group path, allowed=%v policy=%q code=%q", allowed, policy, code)
	}
	if summaries := creditGrantSummariesForOwner(reg, newUserAccountRef("", "user@example.com"), now); len(summaries) != 0 {
		t.Fatalf("removed qualification must not remain in the user's active card view: %#v", summaries)
	}
}

func TestNewUserLimitCardWithOnlyValidityDoesNotConsumeCredits(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "welcome", Name: "Welcome", AccessPolicy: AccessPolicyFree}},
		Grants: []Grant{{
			ID: "card", Email: "user@example.com", ServiceGroupID: "welcome", Source: "new_user_limit_card",
			StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), CreditsTotal: 0,
		}},
	}
	allowed, _, code, _, _, _, _ := BillingEligibilityForServiceGroups(reg, "user@example.com", []string{"welcome"}, now)
	if !allowed || code != "" {
		t.Fatalf("validity-only welcome card should allow access, allowed=%v code=%q", allowed, code)
	}
	if used := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"welcome"}, 100, now); used != 0 {
		t.Fatalf("validity-only welcome card usage = %v, want 0", used)
	}
}

func TestNewUserLimitCardExpiryDoesNotChangeFreeGroupBehavior(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "welcome", Name: "Welcome", AccessPolicy: AccessPolicyFree}},
		Grants: []Grant{{
			ID: "expired-card", Email: "user@example.com", ServiceGroupID: "welcome", Source: "new_user_limit_card",
			StartsAt: now.AddDate(0, 0, -2), ExpiresAt: now.AddDate(0, 0, -1), PeriodLimits: CreditPeriodLimits{FiveHour: 10, Daily: 20},
		}},
	}
	allowed, policy, code, _, _, _, _ := BillingEligibilityForServiceGroups(reg, "user@example.com", []string{"welcome"}, now)
	if !allowed || policy != AccessPolicyFree || code != "" {
		t.Fatalf("expired card must restore the free group behavior, allowed=%v policy=%q code=%q", allowed, policy, code)
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

func TestGrantUserReferralBenefitRejectsSystemFree(t *testing.T) {
	ctx := context.Background()
	system := newTestSystemSettings()
	now := time.Now().UTC()
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{
			SystemFreeTemplate(),
			{ID: "redeem", Name: "充值服务组", AccessPolicy: AccessPolicyGrantRequired},
		},
		DefaultNewUserServiceGroups: []string{"redeem"},
		DefaultNewUserCredits:       1000,
	}
	EnsureSystemFreeServiceGroup(reg)
	if err := SaveRegistry(ctx, system, reg); err != nil {
		t.Fatalf("SaveRegistry: %v", err)
	}
	if _, err := GrantUserReferralBenefitForUserID(ctx, system, "invitee-1", "phone:17090134628", "referral-1", SystemFreeServiceGroupID, 30, 100, now); err == nil {
		t.Fatal("expected system-free referral grant to fail")
	}
	if err := SaveRegistry(ctx, system, &Registry{
		ModelServiceGroups: []ModelServiceGroup{
			SystemFreeTemplate(),
			{ID: "redeem", Name: "充值服务组", AccessPolicy: AccessPolicyGrantRequired},
			{ID: "free-welcome", Name: "Welcome", AccessPolicy: AccessPolicyFree},
		},
		DefaultNewUserServiceGroups: []string{"redeem"},
		DefaultNewUserCredits:       1000,
	}); err != nil {
		t.Fatalf("SaveRegistry free group: %v", err)
	}
	if _, err := GrantUserReferralBenefitForUserID(ctx, system, "invitee-1", "phone:17090134628", "referral-free", "free-welcome", 30, 100, now); err == nil {
		t.Fatal("expected free-policy referral grant to fail")
	}
	saved, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	for _, grant := range saved.Grants {
		if grant.ServiceGroupID == SystemFreeServiceGroupID || grant.ServiceGroupID == "free-welcome" {
			t.Fatalf("referral must not create a free-policy grant: %#v", grant)
		}
	}
	grantID, err := GrantUserReferralBenefitForUserID(ctx, system, "invitee-1", "phone:17090134628", "referral-1", "redeem", 30, 100, now)
	if err != nil || grantID == "" {
		t.Fatalf("redeem referral grant: id=%q err=%v", grantID, err)
	}
	saved, err = LoadRegistry(ctx, system)
	if err != nil {
		t.Fatalf("LoadRegistry after redeem grant: %v", err)
	}
	if len(saved.UserBindings) != 0 {
		t.Fatalf("referral grant must not bind the user: %#v", saved.UserBindings)
	}
	found := false
	for _, grant := range saved.Grants {
		if grant.ID == grantID {
			found = true
			if grant.ServiceGroupID != "redeem" || grant.CreditsTotal != 100 || grant.Source != "user_referral" {
				t.Fatalf("unexpected referral grant: %#v", grant)
			}
		}
		if grant.ServiceGroupID == SystemFreeServiceGroupID {
			t.Fatalf("redeem referral grant leaked a system-free grant: %#v", grant)
		}
	}
	if !found {
		t.Fatal("expected redeem referral grant")
	}
}

func TestGrantDefaultServiceForNewUserSkipsSystemFree(t *testing.T) {
	ctx := context.Background()
	system := newTestSystemSettings()
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{
			SystemFreeTemplate(),
			{ID: "redeem", Name: "Redeem", AccessPolicy: AccessPolicyGrantRequired},
		},
		DefaultNewUserServiceGroups: []string{SystemFreeServiceGroupID, "redeem"},
		DefaultNewUserCredits:       1000,
		DefaultNewUserDurationDays:  30,
	}
	EnsureSystemFreeServiceGroup(reg)
	if err := SaveRegistry(ctx, system, reg); err != nil {
		t.Fatalf("SaveRegistry: %v", err)
	}
	if err := GrantDefaultServiceForNewUserID(ctx, system, "user-new", "newuser@example.com"); err != nil {
		t.Fatalf("GrantDefaultServiceForNewUserID: %v", err)
	}
	saved, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if len(saved.Grants) != 1 || saved.Grants[0].ServiceGroupID != "redeem" {
		t.Fatalf("new-user grants=%#v, want one redeem grant", saved.Grants)
	}
	for _, grant := range saved.Grants {
		if IsSystemFreeServiceGroup(grant.ServiceGroupID) {
			t.Fatalf("new-user default must skip system-free: %#v", grant)
		}
	}
}

func TestDetachSystemFreeFromReferralOwners(t *testing.T) {
	ctx := context.Background()
	system := newTestSystemSettings()
	now := time.Now().UTC()
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{
			SystemFreeTemplate(),
			{ID: "redeem", Name: "Redeem", AccessPolicy: AccessPolicyGrantRequired},
		},
		UserBindings: []UserBinding{
			{UserID: "invitee-1", Email: "invitee@example.com", ServiceGroupIDs: []string{SystemFreeServiceGroupID, "redeem"}},
			{UserID: "inviter-1", Email: "inviter@example.com", ServiceGroupIDs: []string{SystemFreeServiceGroupID}},
			{UserID: "employee-1", Email: "employee@example.com", ServiceGroupIDs: []string{SystemFreeServiceGroupID}},
		},
		Grants: []Grant{
			{ID: "g-referral", UserID: "invitee-1", Email: "invitee@example.com", ServiceGroupID: "redeem", Source: "user_referral", CardID: "ref-1", StartsAt: now, ExpiresAt: now.Add(24 * time.Hour), CreditsTotal: 100},
			{ID: "g-inviter", UserID: "inviter-1", Email: "inviter@example.com", ServiceGroupID: "redeem", Source: "user_referral", CardID: "ref-1", StartsAt: now, ExpiresAt: now.Add(24 * time.Hour), CreditsTotal: 200},
			{ID: "g-old-free", UserID: "invitee-2", Email: "invitee2@example.com", ServiceGroupID: SystemFreeServiceGroupID, Source: "user_referral", CardID: "ref-2", StartsAt: now, ExpiresAt: now.Add(24 * time.Hour), CreditsTotal: 100},
			{ID: "g-expired-free", UserID: "invitee-3", Email: "invitee3@example.com", ServiceGroupID: SystemFreeServiceGroupID, Source: "user_referral", CardID: "ref-3", StartsAt: now.Add(-48 * time.Hour), ExpiresAt: now.Add(-time.Hour), CreditsTotal: 100},
			{ID: "g-invite", UserID: "invitee-1", Email: "invitee@example.com", ServiceGroupID: SystemFreeServiceGroupID, Source: "invitation_code", CardID: "ic-1", StartsAt: now, ExpiresAt: now.Add(24 * time.Hour), CreditsTotal: 999},
			{ID: "g-employee", UserID: "employee-1", Email: "employee@example.com", ServiceGroupID: SystemFreeServiceGroupID, Source: "invitation_code", CardID: "ic-2", StartsAt: now, ExpiresAt: now.Add(24 * time.Hour), CreditsTotal: 1},
		},
	}
	EnsureSystemFreeServiceGroup(reg)
	if err := SaveRegistry(ctx, system, reg); err != nil {
		t.Fatalf("SaveRegistry: %v", err)
	}
	changed, err := DetachSystemFreeFromReferralOwners(ctx, system, "redeem")
	if err != nil || changed == 0 {
		t.Fatalf("detach changed=%d err=%v", changed, err)
	}
	saved, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	foundInviteLeak := false
	reissued := false
	for _, grant := range saved.Grants {
		if grant.ID == "g-invite" {
			foundInviteLeak = true
			if !grant.Frozen {
				t.Fatalf("leaked invitee system-free grant must be frozen: %#v", grant)
			}
		}
		if grant.ID == "g-old-free" && !grant.Frozen {
			t.Fatalf("legacy system-free referral grant must be frozen: %#v", grant)
		}
		if grant.UserID == "invitee-2" && grant.ServiceGroupID == "redeem" && grant.Source == "user_referral" && grant.CreditsTotal == 100 && !grant.Frozen {
			reissued = true
		}
		if grant.UserID == "invitee-3" && grant.ServiceGroupID == "redeem" {
			t.Fatalf("expired system-free referral must not be reissued: %#v", grant)
		}
		if grant.ID == "g-referral" && grant.Frozen {
			t.Fatalf("metered redeem referral grant must stay active: %#v", grant)
		}
		if grant.ID == "g-employee" && (grant.Frozen || !IsSystemFreeServiceGroup(grant.ServiceGroupID)) {
			t.Fatalf("unrelated employee grant was rewritten: %#v", grant)
		}
	}
	if !foundInviteLeak {
		t.Fatal("leaked invitation-code grant should be retained for audit")
	}
	if !reissued {
		t.Fatal("frozen system-free referral credits should be reissued onto redeem")
	}
	hasGroup := func(ids []string, want string) bool {
		for _, id := range ids {
			if strings.EqualFold(id, want) {
				return true
			}
		}
		return false
	}
	for _, binding := range saved.UserBindings {
		if binding.UserID == "invitee-1" && hasGroup(binding.ServiceGroupIDs, SystemFreeServiceGroupID) {
			t.Fatalf("invitee still bound to system-free: %#v", binding)
		}
		if binding.UserID == "inviter-1" && !hasGroup(binding.ServiceGroupIDs, SystemFreeServiceGroupID) {
			t.Fatalf("inviter system-free binding must be kept: %#v", binding)
		}
		if binding.UserID == "employee-1" && !hasGroup(binding.ServiceGroupIDs, SystemFreeServiceGroupID) {
			t.Fatalf("unrelated employee binding lost system-free: %#v", binding)
		}
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
		DefaultNewUserLimitCard:     NewUserLimitCard{ServiceGroupIDs: []string{"deleted-limit-card", "coding-basic"}},
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
	if len(reg.DefaultNewUserLimitCard.ServiceGroupIDs) != 1 || reg.DefaultNewUserLimitCard.ServiceGroupIDs[0] != "coding-basic" {
		t.Fatalf("DefaultNewUserLimitCard.ServiceGroupIDs = %#v, want [coding-basic]", reg.DefaultNewUserLimitCard.ServiceGroupIDs)
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

func TestResolveStatusShowsWelcomeCardAlongsideExistingCard(t *testing.T) {
	now := time.Now().UTC()
	reg := &Registry{
		ModelServiceGroups:      []ModelServiceGroup{{ID: "redeem", Name: "Redeem", AccessPolicy: AccessPolicyFree, Models: []ModelServiceModel{{Name: "auto", ProviderIDs: []string{"provider-a"}}}}},
		DefaultNewUserLimitCard: NewUserLimitCard{ServiceGroupIDs: []string{"redeem"}, PeriodLimits: CreditPeriodLimits{FiveHour: 100, Daily: 200}},
		Grants: []Grant{
			{ID: "paid", Email: "user@example.com", ServiceGroupID: "redeem", Source: "card", StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour), CreditsTotal: 1000},
			{ID: "welcome", Email: "user@example.com", ServiceGroupID: "redeem", Source: "new_user_limit_card", StartsAt: now.Add(-time.Hour)},
		},
	}
	status, _, err := ResolveStatusFromRegistry(context.Background(), reg, nil, "user@example.com", "https://hub.example.com/api/llm/v1")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, grant := range status.CreditGrants {
		if grant.Source == "new_user_limit_card" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("welcome card missing beside existing card: %#v", status.CreditGrants)
	}
}

func TestResolveStatusShowsWelcomeCardAlongsideExistingRechargeCard(t *testing.T) {
	now := time.Now().UTC()
	reg := &Registry{
		ModelServiceGroups:      []ModelServiceGroup{{ID: "redeem", Name: "Redeem", AccessPolicy: AccessPolicyGrantRequired, Models: []ModelServiceModel{{Name: "auto", ProviderIDs: []string{"provider-a"}}}}},
		DefaultNewUserLimitCard: NewUserLimitCard{ServiceGroupIDs: []string{"redeem"}, PeriodLimits: CreditPeriodLimits{FiveHour: 100, Daily: 200}},
		Grants: []Grant{
			{ID: "paid", Email: "user@example.com", ServiceGroupID: "redeem", Source: "card", StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour), CreditsTotal: 1000},
			{ID: "welcome", Email: "user@example.com", ServiceGroupID: "redeem", Source: "new_user_limit_card", StartsAt: now.Add(-time.Hour)},
		},
	}
	status, _, err := ResolveStatusFromRegistry(context.Background(), reg, nil, "user@example.com", "https://hub.example.com/api/llm/v1")
	if err != nil {
		t.Fatal(err)
	}
	var paid, welcome bool
	for _, grant := range status.CreditGrants {
		paid = paid || grant.Source == "card"
		welcome = welcome || grant.Source == "new_user_limit_card"
	}
	if !paid || !welcome {
		t.Fatalf("expected recharge and welcome cards together, got %#v", status.CreditGrants)
	}
	if !status.Active {
		t.Fatalf("expected recharge route active with welcome card, got %#v", status)
	}
}

func TestIssueNewUserLimitCardsAllowsGrantRequiredGroup(t *testing.T) {
	now := time.Now().UTC()
	reg := &Registry{
		ModelServiceGroups:      []ModelServiceGroup{{ID: "redeem", AccessPolicy: AccessPolicyGrantRequired}},
		DefaultNewUserLimitCard: NewUserLimitCard{ServiceGroupIDs: []string{"redeem"}, PeriodLimits: CreditPeriodLimits{FiveHour: 10}},
		Grants:                  []Grant{{ID: "paid", Email: "user@example.com", ServiceGroupID: "redeem", Source: "card", StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), CreditsTotal: 100}},
	}
	if issued := IssueNewUserLimitCards(reg, []VoucherUser{{ID: "u1", Email: "user@example.com"}}, now); issued != 1 {
		t.Fatalf("issued = %d, want 1", issued)
	}
	if issued := IssueNewUserLimitCards(reg, []VoucherUser{{ID: "u1", Email: "user@example.com"}}, now); issued != 0 {
		t.Fatalf("second issuance = %d, want idempotent 0", issued)
	}
	status, _, err := ResolveStatusFromRegistry(context.Background(), reg, nil, "user@example.com", "https://hub.example.com/api/llm/v1")
	if err != nil {
		t.Fatal(err)
	}
	var paid, welcome bool
	for _, grant := range status.CreditGrants {
		paid = paid || grant.Source == "card"
		welcome = welcome || grant.Source == "new_user_limit_card"
	}
	if !paid || !welcome {
		t.Fatalf("expected both recharge and welcome cards after issue, got %#v", status.CreditGrants)
	}
}

func TestIssueNewUserLimitCardsBackfillsUsersWhoAlreadyHaveCreditsCards(t *testing.T) {
	now := time.Now().UTC()
	reg := &Registry{
		ModelServiceGroups:      []ModelServiceGroup{{ID: "redeem", Name: "充值服务组", AccessPolicy: AccessPolicyGrantRequired}},
		DefaultNewUserLimitCard: NewUserLimitCard{ServiceGroupIDs: []string{"redeem"}, PeriodLimits: CreditPeriodLimits{FiveHour: 100, Daily: 200}},
		Grants: []Grant{
			{ID: "paid-1", Email: "user@example.com", ServiceGroupID: "redeem", Source: "card", StartsAt: now.Add(-40 * 24 * time.Hour), ExpiresAt: now.Add(300 * 24 * time.Hour), CreditsTotal: 50000, CreditsUsed: 18133.78},
			{ID: "paid-2", Email: "user@example.com", ServiceGroupID: "redeem", Source: "card", StartsAt: now.Add(-50 * 24 * time.Hour), ExpiresAt: now.Add(290 * 24 * time.Hour), CreditsTotal: 10000, CreditsUsed: 10000},
			{ID: "gift", Email: "user@example.com", ServiceGroupID: "redeem", Source: "new_user_default", StartsAt: now.Add(-60 * 24 * time.Hour), ExpiresAt: now.Add(-30 * 24 * time.Hour), CreditsTotal: 300, CreditsUsed: 300},
		},
	}
	if issued := IssueNewUserLimitCards(reg, []VoucherUser{{ID: "u1", Email: "user@example.com"}}, now); issued != 1 {
		t.Fatalf("issued = %d, want 1 for recharge-card holder with a historical credits gift", issued)
	}
	if issued := IssueNewUserLimitCards(reg, []VoucherUser{{ID: "u1", Email: "user@example.com"}}, now); issued != 0 {
		t.Fatalf("second issuance = %d, want idempotent 0", issued)
	}
	status, _, err := ResolveStatusFromRegistry(context.Background(), reg, nil, "user@example.com", "https://hub.example.com/api/llm/v1")
	if err != nil {
		t.Fatal(err)
	}
	var paid, welcome bool
	for _, grant := range status.CreditGrants {
		paid = paid || grant.Source == "card"
		welcome = welcome || grant.Source == "new_user_limit_card"
	}
	if !paid || !welcome {
		t.Fatalf("expected recharge cards and welcome card together after backfill, got %#v", status.CreditGrants)
	}
}

func TestEnsureNewUserLimitCardShowsBesideStackedRechargeCards(t *testing.T) {
	now := time.Now().UTC()
	reg := &Registry{
		ModelServiceGroups:        []ModelServiceGroup{{ID: "redeem", Name: "充值服务组", AccessPolicy: AccessPolicyGrantRequired}},
		DefaultNewUserBenefitMode: NewUserBenefitModeLimitCard,
		DefaultNewUserLimitCard:   NewUserLimitCard{ServiceGroupIDs: []string{"redeem"}, PeriodLimits: CreditPeriodLimits{FiveHour: 100, Daily: 200}},
		Grants: []Grant{
			{ID: "paid-1", Email: "user@example.com", ServiceGroupID: "redeem", Source: "card", StartsAt: now.Add(-40 * 24 * time.Hour), ExpiresAt: now.Add(300 * 24 * time.Hour), CreditsTotal: 50000, CreditsUsed: 18136.33},
			{ID: "paid-2", Email: "user@example.com", ServiceGroupID: "redeem", Source: "card", StartsAt: now.Add(-50 * 24 * time.Hour), ExpiresAt: now.Add(290 * 24 * time.Hour), CreditsTotal: 10000, CreditsUsed: 10000},
			{ID: "paid-3", Email: "user@example.com", ServiceGroupID: "redeem", Source: "card", StartsAt: now.Add(-45 * 24 * time.Hour), ExpiresAt: now.Add(295 * 24 * time.Hour), CreditsTotal: 10000, CreditsUsed: 10000},
			{ID: "paid-4", Email: "user@example.com", ServiceGroupID: "redeem", Source: "card", StartsAt: now.Add(-44 * 24 * time.Hour), ExpiresAt: now.Add(296 * 24 * time.Hour), CreditsTotal: 10000, CreditsUsed: 10000},
		},
	}
	issued, err := ensureNewUserLimitCardForRegistry(context.Background(), nil, reg, "u1", "user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !issued {
		t.Fatal("expected welcome card backfill for recharge-card holder")
	}
	status, _, err := ResolveStatusFromRegistryForUser(context.Background(), reg, nil, "u1", "user@example.com", "https://hub.example.com/api/llm/v1")
	if err != nil {
		t.Fatal(err)
	}
	var welcome *ActiveGrant
	for i := range status.CreditGrants {
		if status.CreditGrants[i].Source == "new_user_limit_card" {
			welcome = &status.CreditGrants[i]
			break
		}
	}
	if welcome == nil {
		t.Fatalf("welcome card missing from service-exchange status: %#v", status.CreditGrants)
	}
	if welcome.PeriodLimits == nil || welcome.PeriodLimits.FiveHour != 100 || welcome.PeriodLimits.Daily != 200 {
		t.Fatalf("welcome period limits = %#v", welcome.PeriodLimits)
	}
	if NeedsNewUserLimitCardBackfill(reg, "u1", "user@example.com") {
		t.Fatal("backfill should be idempotent after a live welcome card exists")
	}
	issued, err = ensureNewUserLimitCardForRegistry(context.Background(), nil, reg, "u1", "user@example.com")
	if err != nil || issued {
		t.Fatalf("second backfill issued=%v err=%v, want issued=false", issued, err)
	}
}

func TestNeedsNewUserLimitCardBackfillSkipsCreditsMode(t *testing.T) {
	reg := &Registry{
		ModelServiceGroups:        []ModelServiceGroup{{ID: "redeem", AccessPolicy: AccessPolicyGrantRequired}},
		DefaultNewUserBenefitMode: NewUserBenefitModeCredits,
		DefaultNewUserLimitCard:   NewUserLimitCard{ServiceGroupIDs: []string{"redeem"}, PeriodLimits: CreditPeriodLimits{FiveHour: 10}},
	}
	if NeedsNewUserLimitCardBackfill(reg, "u1", "user@example.com") {
		t.Fatal("credits mode must not backfill a limit card")
	}
	issued, err := ensureNewUserLimitCardForRegistry(context.Background(), nil, reg, "u1", "user@example.com")
	if err != nil || issued {
		t.Fatalf("credits mode issued=%v err=%v, want issued=false", issued, err)
	}
	reg.DefaultNewUserBenefitMode = NewUserBenefitModeLimitCard
	reg.DefaultNewUserLimitCard.ServiceGroupIDs = nil
	if NeedsNewUserLimitCardBackfill(reg, "u1", "user@example.com") {
		t.Fatal("empty policy groups must not backfill")
	}
	reg.DefaultNewUserLimitCard.ServiceGroupIDs = []string{"gone"}
	if NeedsNewUserLimitCardBackfill(reg, "u1", "user@example.com") {
		t.Fatal("missing policy groups must not backfill")
	}
}

func TestIssueNewUserLimitCardsBackfillsWhenPriorLimitCardIsOnAnotherGroup(t *testing.T) {
	now := time.Now().UTC()
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{
			{ID: "welcome", AccessPolicy: AccessPolicyFree},
			{ID: "redeem", Name: "充值服务组", AccessPolicy: AccessPolicyGrantRequired},
		},
		DefaultNewUserLimitCard: NewUserLimitCard{ServiceGroupIDs: []string{"redeem"}, PeriodLimits: CreditPeriodLimits{FiveHour: 100, Daily: 200}},
		Grants: []Grant{
			{ID: "old-welcome", Email: "user@example.com", ServiceGroupID: "welcome", Source: "new_user_limit_card", StartsAt: now.Add(-time.Hour)},
			{ID: "paid", Email: "user@example.com", ServiceGroupID: "redeem", Source: "card", StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour), CreditsTotal: 1000},
		},
	}
	if issued := IssueNewUserLimitCards(reg, []VoucherUser{{Email: "user@example.com"}}, now); issued != 1 {
		t.Fatalf("issued = %d, want 1 so a prior free-group qualification does not block the recharge-group overlay", issued)
	}
	status, _, err := ResolveStatusFromRegistry(context.Background(), reg, nil, "user@example.com", "https://hub.example.com/api/llm/v1")
	if err != nil {
		t.Fatal(err)
	}
	foundRedeemWelcome := false
	for _, grant := range status.CreditGrants {
		if grant.Source == "new_user_limit_card" && grant.ServiceGroupID == "redeem" {
			foundRedeemWelcome = true
		}
	}
	if !foundRedeemWelcome {
		t.Fatalf("expected welcome card on redeem beside existing credits card, got %#v", status.CreditGrants)
	}
}

func TestIssueNewUserLimitCardsReissuesExpiredSameGroupQualification(t *testing.T) {
	now := time.Now().UTC()
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "redeem", Name: "充值服务组", AccessPolicy: AccessPolicyGrantRequired}},
		DefaultNewUserLimitCard: NewUserLimitCard{
			ServiceGroupIDs: []string{"redeem"},
			DurationDays:    30,
			PeriodLimits:    CreditPeriodLimits{FiveHour: 100, Daily: 200},
		},
		Grants: []Grant{
			{ID: "expired-welcome", Email: "user@example.com", ServiceGroupID: "redeem", Source: "new_user_limit_card", StartsAt: now.AddDate(0, 0, -60)},
			{ID: "paid", Email: "user@example.com", ServiceGroupID: "redeem", Source: "card", StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour), CreditsTotal: 80000},
		},
	}
	if issued := IssueNewUserLimitCards(reg, []VoucherUser{{Email: "user@example.com"}}, now); issued != 1 {
		t.Fatalf("issued = %d, want 1 so an expired same-group qualification can be backfilled", issued)
	}
	status, _, err := ResolveStatusFromRegistry(context.Background(), reg, nil, "user@example.com", "https://hub.example.com/api/llm/v1")
	if err != nil {
		t.Fatal(err)
	}
	foundLiveWelcome := false
	for _, grant := range status.CreditGrants {
		if grant.Source == "new_user_limit_card" && grant.Status != "expired" {
			foundLiveWelcome = true
		}
	}
	if !foundLiveWelcome {
		t.Fatalf("expected a live welcome card beside recharge credits after expired backfill, got %#v", status.CreditGrants)
	}
	limitCards := 0
	for _, grant := range reg.Grants {
		if grant.Source == "new_user_limit_card" {
			limitCards++
		}
	}
	if limitCards != 1 {
		t.Fatalf("expired same-group qualification should be replaced, got %d limit cards", limitCards)
	}
}

func TestHasNewUserLimitCardForServiceGroupIgnoresExpiredDuplicate(t *testing.T) {
	now := time.Now().UTC()
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "redeem", AccessPolicy: AccessPolicyGrantRequired}},
		DefaultNewUserLimitCard: NewUserLimitCard{
			ServiceGroupIDs: []string{"redeem"},
			DurationDays:    30,
			PeriodLimits:    CreditPeriodLimits{FiveHour: 100, Daily: 200},
		},
		Grants: []Grant{
			{ID: "expired-welcome", Email: "user@example.com", ServiceGroupID: "redeem", Source: "new_user_limit_card", StartsAt: now.AddDate(0, 0, -60)},
			{ID: "live-welcome", Email: "user@example.com", ServiceGroupID: "redeem", Source: "new_user_limit_card", StartsAt: now.Add(-time.Hour)},
		},
	}
	if !hasNewUserLimitCardForServiceGroup(reg, newUserAccountRef("", "user@example.com"), "redeem", now) {
		t.Fatal("a later live welcome card must count even when an expired duplicate is stored first")
	}
}

func TestCreditGrantSummariesPreferLaterLiveWelcomeCardOverExpiredDuplicate(t *testing.T) {
	now := time.Now().UTC()
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "redeem", AccessPolicy: AccessPolicyGrantRequired}},
		DefaultNewUserLimitCard: NewUserLimitCard{
			ServiceGroupIDs: []string{"redeem"},
			DurationDays:    30,
			PeriodLimits:    CreditPeriodLimits{FiveHour: 100, Daily: 200},
		},
		Grants: []Grant{
			{ID: "expired-welcome", Email: "user@example.com", ServiceGroupID: "redeem", Source: "new_user_limit_card", StartsAt: now.AddDate(0, 0, -60)},
			{ID: "live-welcome", Email: "user@example.com", ServiceGroupID: "redeem", Source: "new_user_limit_card", StartsAt: now.Add(-time.Hour)},
			{ID: "paid", Email: "user@example.com", ServiceGroupID: "redeem", Source: "card", StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour), CreditsTotal: 1000},
		},
	}
	summaries := creditGrantSummariesForOwner(reg, newUserAccountRef("", "user@example.com"), now)
	var liveWelcome, paid bool
	for _, grant := range summaries {
		if grant.Source == "new_user_limit_card" && grant.Status != "expired" {
			liveWelcome = true
		}
		paid = paid || grant.Source == "card"
	}
	if !liveWelcome || !paid {
		t.Fatalf("expected live welcome card and recharge card, got %#v", summaries)
	}
	if summaries[0].Source != "new_user_limit_card" {
		t.Fatalf("welcome card should sort ahead of recharge cards, got %#v", summaries)
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

func TestResetNewUserLimitCardUsage(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	oldFiveHourStart := fiveHourWindowStart(now)
	reg := &Registry{Grants: []Grant{
		{Source: "new_user_limit_card", Email: "a@example.com", UsageEvents: []CreditUsageEvent{{OccurredAt: now.Add(-time.Hour), CreditsUsed: 10}}, PeriodUsage: CreditPeriodUsage{
			FiveHour: GrantUsageWindow{WindowStart: oldFiveHourStart, CreditsUsed: 8},
			Daily:    GrantUsageWindow{CreditsUsed: 20},
		}},
		{Source: "credits", Email: "b@example.com", UsageEvents: []CreditUsageEvent{{OccurredAt: now.Add(-time.Hour), CreditsUsed: 5}}},
	}}
	if got := ResetNewUserLimitCardUsage(reg, now); got != 1 {
		t.Fatalf("reset count = %d, want 1", got)
	}
	if got := reg.Grants[0]; len(got.UsageEvents) != 0 || got.PeriodUsage.Daily.CreditsUsed != 0 {
		t.Fatalf("limit-card usage not reset: %#v", got)
	}
	if got := reg.Grants[0].PeriodUsage.FiveHour; got.CreditsUsed != 0 || !got.WindowStart.Equal(now) {
		t.Fatalf("five-hour window not restarted: %#v want start=%s", got, now)
	}
	if len(reg.Grants[1].UsageEvents) != 1 {
		t.Fatalf("non-limit grant usage was changed: %#v", reg.Grants[1])
	}
}

func welcomeLimitCardRegistry(grants ...Grant) *Registry {
	return &Registry{
		ModelServiceGroups:      []ModelServiceGroup{{ID: "welcome", AccessPolicy: AccessPolicyFree}},
		DefaultNewUserLimitCard: NewUserLimitCard{ServiceGroupIDs: []string{"welcome"}, PeriodLimits: CreditPeriodLimits{FiveHour: 10, Daily: 25}},
		Grants:                  grants,
	}
}

func TestResetVoucherIssueAndRedeem(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	oldFiveHourStart := fiveHourWindowStart(now)
	reg := welcomeLimitCardRegistry(Grant{
		ID: "g1", UserID: "u1", Email: "a@example.com", ServiceGroupID: "welcome", Source: "new_user_limit_card",
		StartsAt: now.Add(-time.Hour),
		PeriodUsage: CreditPeriodUsage{
			FiveHour: GrantUsageWindow{WindowStart: oldFiveHourStart, CreditsUsed: 10},
			Daily:    GrantUsageWindow{CreditsUsed: 9},
		},
	})
	if got := IssueResetVouchers(reg, []VoucherUser{{ID: "u1", Email: "a@example.com"}}, 7, now); got != 1 || len(reg.ResetVouchers) != 1 {
		t.Fatalf("issue result=%d vouchers=%#v", got, reg.ResetVouchers)
	}
	voucherID := reg.ResetVouchers[0].ID
	redeemAt := now.Add(time.Hour)
	if ok, err := RedeemResetVoucher(reg, "u1", "a@example.com", voucherID, redeemAt); !ok || err != nil {
		t.Fatalf("redeem = %v, %v", ok, err)
	}
	if reg.ResetVouchers[0].RedeemedAt == nil || reg.Grants[0].PeriodUsage.Daily.CreditsUsed != 0 {
		t.Fatalf("voucher/usage not consumed: %#v %#v", reg.ResetVouchers[0], reg.Grants[0])
	}
	if got := reg.Grants[0].PeriodUsage.FiveHour; got.CreditsUsed != 0 || !got.WindowStart.Equal(redeemAt) {
		t.Fatalf("five-hour window not restarted at redeem instant: %#v want start=%s used=0", got, redeemAt)
	}
	if ok, err := RedeemResetVoucher(reg, "u1", "a@example.com", voucherID, now.Add(2*time.Hour)); ok || err == nil {
		t.Fatalf("second redeem = %v, %v", ok, err)
	}
}

func TestResetVoucherRestartsAnchoredFiveHourWindow(t *testing.T) {
	// 12:30 sits inside the UTC epoch window 10:00–15:00. Redeeming must not
	// leave the next reset at 15:00; it must start a fresh five-hour cycle.
	now := time.Date(2026, 9, 1, 12, 30, 0, 0, time.UTC)
	reg := welcomeLimitCardRegistry(Grant{
		ID: "g1", UserID: "u1", Email: "a@example.com", ServiceGroupID: "welcome", Source: "new_user_limit_card",
		StartsAt: now.Add(-2 * time.Hour),
		PeriodUsage: CreditPeriodUsage{
			FiveHour: GrantUsageWindow{WindowStart: now.Add(-2 * time.Hour), CreditsUsed: 10},
			Daily:    GrantUsageWindow{WindowStart: dayWindowStart(now), CreditsUsed: 10},
		},
	})
	if got := IssueResetVouchers(reg, []VoucherUser{{ID: "u1", Email: "a@example.com"}}, 7, now); got != 1 {
		t.Fatalf("issue result=%d", got)
	}
	if ok, err := RedeemResetVoucher(reg, "u1", "a@example.com", reg.ResetVouchers[0].ID, now); !ok || err != nil {
		t.Fatalf("redeem = %v, %v", ok, err)
	}
	summaries := creditGrantSummariesForOwner(reg, newUserAccountRef("u1", "a@example.com"), now)
	if len(summaries) != 1 || summaries[0].PeriodUsage == nil {
		t.Fatalf("status grants = %#v", summaries)
	}
	if fh := summaries[0].PeriodUsage.FiveHour; !fh.WindowStart.Equal(now) || !fh.WindowEnd.Equal(now.Add(5*time.Hour)) || fh.CreditsUsed != 0 {
		t.Fatalf("status five-hour window = %#v, want start=%s end=%s used=0", fh, now, now.Add(5*time.Hour))
	}
	if got := AvailableCreditsForServiceGroups(reg, "a@example.com", []string{"welcome"}, now); got != 10 {
		t.Fatalf("credits immediately after redeem = %v, want 10", got)
	}
	if got := ApplyCreditUsageToRegistry(reg, "a@example.com", []string{"welcome"}, 10, now); got != 10 {
		t.Fatalf("usage in new window = %v, want 10", got)
	}
	if got := ApplyCreditUsageToRegistry(reg, "a@example.com", []string{"welcome"}, 1, now.Add(4*time.Hour+59*time.Minute)); got != 0 {
		t.Fatalf("usage before restarted window expiry = %v, want 0", got)
	}
	if got := ApplyCreditUsageToRegistry(reg, "a@example.com", []string{"welcome"}, 1, now.Add(5*time.Hour)); got != 1 {
		t.Fatalf("usage after restarted window expiry = %v, want 1", got)
	}
}

func TestResetVoucherDailyWindowUsesAccountTimezone(t *testing.T) {
	now := time.Date(2026, 9, 1, 16, 30, 0, 0, time.UTC)
	reg := welcomeLimitCardRegistry(Grant{
		ID: "g1", UserID: "u1", Email: "a@example.com", ServiceGroupID: "welcome", Source: "new_user_limit_card",
		StartsAt: now.Add(-time.Hour), PeriodUsage: CreditPeriodUsage{Daily: GrantUsageWindow{CreditsUsed: 9}},
	})
	reg.UserBillingTimezones = map[string]string{"a@example.com": "Asia/Shanghai"}
	if got := IssueResetVouchers(reg, []VoucherUser{{ID: "u1", Email: "a@example.com"}}, 7, now); got != 1 {
		t.Fatalf("issue result=%d", got)
	}
	if ok, err := RedeemResetVoucher(reg, "u1", "a@example.com", reg.ResetVouchers[0].ID, now); !ok || err != nil {
		t.Fatalf("redeem = %v, %v", ok, err)
	}
	wantDay := grantDayWindowStart(effectiveGrantForRegistry(reg, reg.Grants[0]), now)
	if got := reg.Grants[0].PeriodUsage.Daily.WindowStart; !got.Equal(wantDay) {
		t.Fatalf("daily window start = %s, want %s", got, wantDay)
	}
}

func TestResetVoucherRejectsWrongUserAndExpiry(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	reg := welcomeLimitCardRegistry(Grant{
		ID: "g1", UserID: "u1", Email: "a@example.com", ServiceGroupID: "welcome", Source: "new_user_limit_card", StartsAt: now.Add(-time.Hour),
	})
	IssueResetVouchers(reg, []VoucherUser{{ID: "u1", Email: "a@example.com"}}, 1, now)
	id := reg.ResetVouchers[0].ID
	if ok, err := RedeemResetVoucher(reg, "u2", "b@example.com", id, now); ok || err == nil {
		t.Fatalf("wrong user redeem = %v, %v", ok, err)
	}
	if ok, err := RedeemResetVoucher(reg, "u1", "a@example.com", id, now.Add(2*24*time.Hour)); ok || err == nil {
		t.Fatalf("expired redeem = %v, %v", ok, err)
	}
}

func TestResetVoucherUserIDIsAuthoritative(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	reg := welcomeLimitCardRegistry(Grant{
		ID: "g1", UserID: "u1", Email: "shared@example.com", ServiceGroupID: "welcome", Source: "new_user_limit_card", StartsAt: now.Add(-time.Hour),
	})
	IssueResetVouchers(reg, []VoucherUser{{ID: "u1", Email: "shared@example.com"}}, 1, now)
	if ok, err := RedeemResetVoucher(reg, "u2", "shared@example.com", reg.ResetVouchers[0].ID, now); ok || err == nil {
		t.Fatalf("same-email different user redeemed voucher: %v, %v", ok, err)
	}
}

func TestIssueResetVouchersKeepsIDOnlyUsersDistinct(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	reg := welcomeLimitCardRegistry(
		Grant{ID: "c1", UserID: "u1", ServiceGroupID: "welcome", Source: "new_user_limit_card", StartsAt: now.Add(-time.Hour)},
		Grant{ID: "c2", UserID: "u2", ServiceGroupID: "welcome", Source: "new_user_limit_card", StartsAt: now.Add(-time.Hour)},
	)
	users := []VoucherUser{{ID: "u1"}, {ID: "u2"}}
	if got := IssueResetVouchers(reg, users, 3, now); got != 2 || len(reg.ResetVouchers) != 2 {
		t.Fatalf("issue result=%d vouchers=%#v", got, reg.ResetVouchers)
	}
	if reg.ResetVouchers[0].UserID == reg.ResetVouchers[1].UserID {
		t.Fatalf("id-only users were merged: %#v", reg.ResetVouchers)
	}
}

func TestIssueResetVouchersDeduplicatesTargetsWithinIssuance(t *testing.T) {
	now := time.Now().UTC()
	reg := welcomeLimitCardRegistry(Grant{
		ID: "g1", UserID: "u1", Email: "a@example.com", ServiceGroupID: "welcome", Source: "new_user_limit_card", StartsAt: now.Add(-time.Hour),
	})
	if got := IssueResetVouchers(reg, []VoucherUser{{ID: "u1", Email: "a@example.com"}, {ID: "u1", Email: "a@example.com"}}, 1, now); got != 1 {
		t.Fatalf("expected one voucher for duplicate target, got %d", got)
	}
}

func TestIssueResetVouchersSkipsUsersWithoutLiveNewUserBenefit(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	reg := welcomeLimitCardRegistry(
		Grant{ID: "live", UserID: "live", Email: "live@example.com", ServiceGroupID: "welcome", Source: "new_user_limit_card", StartsAt: now.Add(-time.Hour)},
		Grant{ID: "email-only", Email: "emailonly@example.com", ServiceGroupID: "welcome", Source: "new_user_limit_card", StartsAt: now.Add(-time.Hour)},
		Grant{ID: "expired", UserID: "expired", Email: "expired@example.com", ServiceGroupID: "welcome", Source: "new_user_limit_card", StartsAt: now.AddDate(0, 0, -60)},
		Grant{ID: "frozen", UserID: "frozen", Email: "frozen@example.com", ServiceGroupID: "welcome", Source: "new_user_limit_card", StartsAt: now.Add(-time.Hour), Frozen: true},
		Grant{ID: "credits", UserID: "credits", Email: "credits@example.com", ServiceGroupID: "welcome", Source: "new_user_default", StartsAt: now.Add(-time.Hour), ExpiresAt: now.AddDate(0, 0, 30), CreditsTotal: 100},
		Grant{ID: "removed", UserID: "removed", Email: "removed@example.com", ServiceGroupID: "gone", Source: "new_user_limit_card", StartsAt: now.Add(-time.Hour)},
	)
	reg.DefaultNewUserLimitCard.DurationDays = 30
	users := []VoucherUser{
		{ID: "live", Email: "live@example.com"},
		{ID: "u-email", Email: "emailonly@example.com"},
		{ID: "expired", Email: "expired@example.com"},
		{ID: "frozen", Email: "frozen@example.com"},
		{ID: "credits", Email: "credits@example.com"},
		{ID: "removed", Email: "removed@example.com"},
		{ID: "none", Email: "none@example.com"},
	}
	if got := IssueResetVouchers(reg, users, 7, now); got != 2 || len(reg.ResetVouchers) != 2 {
		t.Fatalf("issue result=%d vouchers=%#v, want 2 for live welcome-card holders only", got, reg.ResetVouchers)
	}
	gotIDs := map[string]string{}
	for _, voucher := range reg.ResetVouchers {
		gotIDs[voucher.Email] = voucher.UserID
	}
	if gotIDs["live@example.com"] != "live" || gotIDs["emailonly@example.com"] != "u-email" {
		t.Fatalf("issued vouchers to unexpected users: %#v", reg.ResetVouchers)
	}
}

func TestRedeemResetVoucherRequiresLiveNewUserLimitCard(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	expiredUsage := CreditPeriodUsage{Daily: GrantUsageWindow{CreditsUsed: 9}}
	reg := welcomeLimitCardRegistry(
		Grant{ID: "expired", UserID: "expired", Email: "expired@example.com", ServiceGroupID: "welcome", Source: "new_user_limit_card", StartsAt: now.AddDate(0, 0, -60), PeriodUsage: expiredUsage},
		Grant{ID: "live", UserID: "live", Email: "live@example.com", ServiceGroupID: "welcome", Source: "new_user_limit_card", StartsAt: now.Add(-time.Hour), PeriodUsage: expiredUsage},
		Grant{ID: "stale", UserID: "live", Email: "live@example.com", ServiceGroupID: "welcome", Source: "new_user_limit_card", StartsAt: now.AddDate(0, 0, -60), PeriodUsage: expiredUsage},
	)
	reg.DefaultNewUserLimitCard.DurationDays = 30
	reg.ResetVouchers = []ResetVoucher{
		{ID: "v-expired", UserID: "expired", Email: "expired@example.com", IssuedAt: now, ExpiresAt: now.AddDate(0, 0, 7)},
		{ID: "v-live", UserID: "live", Email: "live@example.com", IssuedAt: now, ExpiresAt: now.AddDate(0, 0, 7)},
	}

	if ok, err := RedeemResetVoucher(reg, "expired", "expired@example.com", "v-expired", now); ok || !errors.Is(err, errNoActiveNewUserLimitCard) {
		t.Fatalf("expired card redeem = %v, %v", ok, err)
	}
	if reg.ResetVouchers[0].RedeemedAt != nil {
		t.Fatalf("expired-card voucher was consumed: %#v", reg.ResetVouchers[0])
	}
	if got := reg.Grants[0].PeriodUsage.Daily.CreditsUsed; got != 9 {
		t.Fatalf("expired grant usage changed: %#v", reg.Grants[0].PeriodUsage)
	}

	if ok, err := RedeemResetVoucher(reg, "live", "live@example.com", "v-live", now); !ok || err != nil {
		t.Fatalf("live card redeem = %v, %v", ok, err)
	}
	if reg.Grants[1].PeriodUsage.Daily.CreditsUsed != 0 {
		t.Fatalf("live grant usage not reset: %#v", reg.Grants[1].PeriodUsage)
	}
	if got := reg.Grants[2].PeriodUsage.Daily.CreditsUsed; got != 9 {
		t.Fatalf("stale grant on the same user was reset: %#v", reg.Grants[2].PeriodUsage)
	}
}

func TestResetVoucherOmittedFromStatusWithoutLiveNewUserLimitCard(t *testing.T) {
	now := time.Now().UTC()
	reg := welcomeLimitCardRegistry(Grant{
		ID: "expired", UserID: "u1", Email: "a@example.com", ServiceGroupID: "welcome", Source: "new_user_limit_card", StartsAt: now.AddDate(0, 0, -60),
	})
	reg.DefaultNewUserLimitCard.DurationDays = 30
	reg.ResetVouchers = []ResetVoucher{{ID: "v1", UserID: "u1", Email: "a@example.com", IssuedAt: now, ExpiresAt: now.AddDate(0, 0, 7)}}
	status, _, err := ResolveStatusFromRegistryForUser(context.Background(), reg, nil, "u1", "a@example.com", "https://hub.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(status.ResetVouchers) != 0 {
		t.Fatalf("expired-card user still saw vouchers: %#v", status.ResetVouchers)
	}

	reg.Grants[0].StartsAt = now.Add(-time.Hour)
	status, _, err = ResolveStatusFromRegistryForUser(context.Background(), reg, nil, "u1", "a@example.com", "https://hub.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(status.ResetVouchers) != 1 || status.ResetVouchers[0].ID != "v1" {
		t.Fatalf("live-card user missing voucher: %#v", status.ResetVouchers)
	}
}
