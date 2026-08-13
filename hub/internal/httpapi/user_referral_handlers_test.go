package httpapi

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	storesqlite "github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
)

func TestValidateUserReferralCredits(t *testing.T) {
	for _, tc := range []struct {
		name    string
		inviter float64
		invitee float64
		wantErr bool
	}{
		{name: "whole credits", inviter: 10, invitee: 5},
		{name: "cent credits", inviter: 10.25, invitee: 5.50},
		{name: "negative", inviter: -0.01, invitee: 0, wantErr: true},
		{name: "third decimal", inviter: 1.001, invitee: 0, wantErr: true},
		{name: "above cap", inviter: userReferralMaxRewardCredits + .01, invitee: 0, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateUserReferralCredits(tc.inviter, tc.invitee)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateUserReferralCredits(%v, %v) error=%v wantErr=%v", tc.inviter, tc.invitee, err, tc.wantErr)
			}
		})
	}
	if err := validateUserReferralCredits(0, 0); err != nil {
		t.Fatalf("zero credits: %v", err)
	}
}

func TestUserReferralURLUsesNormalizedForwardedOrigin(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://internal.local/api/me/invitations", nil)
	req.Host = "internal.local:8080"
	req.Header.Set("X-Forwarded-Proto", "https, http")
	req.Header.Set("X-Forwarded-Host", "hub.example.com, internal.local")
	if got, want := userReferralURL(req, "rf_demo_code"), "https://hub.example.com/invite/rf_demo_code"; got != want {
		t.Fatalf("userReferralURL() = %q, want %q", got, want)
	}
}

func TestUserReferralURLRejectsInvalidForwardedOrigin(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://internal.local/api/me/invitations", nil)
	req.Host = "bad host"
	req.Header.Set("X-Forwarded-Proto", "javascript")
	req.Header.Set("X-Forwarded-Host", "bad host")
	if got, want := userReferralURL(req, "rf/demo"), "/invite/rf%2Fdemo"; got != want {
		t.Fatalf("userReferralURL() = %q, want %q", got, want)
	}
}

func TestPublicUserReferralEmailSendCodeUnavailableDependenciesDoNotPanic(t *testing.T) {
	handler := PublicUserReferralEmailSendCodeHandler(nil, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/public/referral-registration/email/send-code", strings.NewReader(`{"email":"new@example.com"}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "REGISTRATION_UNAVAILABLE") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUserReferralDailyMetricsAreTenantScopedAndAggregated(t *testing.T) {
	services := newAdminRouterTestContext(t)
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	ctx := context.Background()
	day := time.Date(2026, time.August, 13, 9, 0, 0, 0, time.UTC)
	for range 2 {
		if err := repo.IncrementDailyMetric(ctx, store.DefaultTenantID, userReferralMetricLanding, day); err != nil {
			t.Fatalf("increment default tenant landing metric: %v", err)
		}
	}
	if err := repo.IncrementDailyMetric(ctx, "other-tenant", userReferralMetricLanding, day); err != nil {
		t.Fatalf("increment other tenant metric: %v", err)
	}
	if err := repo.IncrementDailyMetric(ctx, store.DefaultTenantID, userReferralMetricRewardFailed, day); err != nil {
		t.Fatalf("increment default tenant reward metric: %v", err)
	}
	items, err := repo.ListDailyMetrics(ctx, store.DefaultTenantID, day, day)
	if err != nil {
		t.Fatalf("list daily metrics: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("metrics=%#v, want two default-tenant events", items)
	}
	got := map[string]int64{}
	for _, item := range items {
		if item == nil || item.TenantID != store.DefaultTenantID || item.Date != "2026-08-13" {
			t.Fatalf("unexpected metric row: %#v", item)
		}
		got[item.Event] = item.Count
	}
	if got[userReferralMetricLanding] != 2 || got[userReferralMetricRewardFailed] != 1 {
		t.Fatalf("aggregated metrics=%#v", got)
	}
}

func TestUserReferralRewardMetricEventsAreIdempotentAndTenantScoped(t *testing.T) {
	services := newAdminRouterTestContext(t)
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	ctx := context.Background()
	occurredAt := time.Date(2026, time.August, 13, 9, 0, 0, 0, time.UTC)
	created, err := repo.RecordRewardMetricEvent(ctx, store.DefaultTenantID, "grant-1", userReferralMetricRewardUsed, occurredAt)
	if err != nil || !created {
		t.Fatalf("first reward metric event = created:%v err:%v", created, err)
	}
	created, err = repo.RecordRewardMetricEvent(ctx, store.DefaultTenantID, "grant-1", userReferralMetricRewardUsed, occurredAt.Add(time.Hour))
	if err != nil || created {
		t.Fatalf("duplicate reward metric event = created:%v err:%v", created, err)
	}
	if _, err := repo.RecordRewardMetricEvent(ctx, "other-tenant", "grant-1", userReferralMetricRewardUsed, occurredAt); err != nil {
		t.Fatalf("other tenant metric: %v", err)
	}
	items, err := repo.ListDailyMetrics(ctx, store.DefaultTenantID, occurredAt, occurredAt)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Event != userReferralMetricRewardUsed || items[0].Count != 1 {
		t.Fatalf("idempotent metrics=%#v", items)
	}
}

func TestGetUserReferralMetricsHandlerValidatesRangeAndUsesDurableBacklog(t *testing.T) {
	services := newAdminRouterTestContext(t)
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := repo.IncrementDailyMetric(ctx, store.DefaultTenantID, userReferralMetricLanding, now); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateReferral(ctx, &store.UserReferral{ID: "metric-reward-failed", TenantID: store.DefaultTenantID, InviterUserID: "inviter", InviteeUserID: "invitee", Status: "reward_failed", RegisteredAt: now, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create durable failed referral: %v", err)
	}
	h := GetUserReferralMetricsHandler(repo, services.store.System)
	request := func(target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req = req.WithContext(WithRequestTenant(req.Context(), store.DefaultTenantID))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	if rec := request("/api/admin/user-referrals/metrics?days=0"); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "INVALID_METRICS_RANGE") {
		t.Fatalf("invalid range response=%d %s", rec.Code, rec.Body.String())
	}
	rec := request("/api/admin/user-referrals/metrics?days=1")
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "inviter") || strings.Contains(rec.Body.String(), "invitee") {
		t.Fatalf("metrics must contain aggregates only: %s", rec.Body.String())
	}
	var payload struct {
		Metrics             []store.UserReferralDailyMetric `json:"metrics"`
		RewardFailedBacklog int                             `json:"reward_failed_backlog"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode metrics: %v", err)
	}
	if payload.RewardFailedBacklog != 1 || len(payload.Metrics) != 1 || payload.Metrics[0].Event != userReferralMetricLanding {
		t.Fatalf("metrics payload=%#v", payload)
	}
}

func TestGetUserReferralMetricsHandlerReconcilesExpiredReferralGrantOnce(t *testing.T) {
	services := newAdminRouterTestContext(t)
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := llmservice.SaveRegistry(ctx, services.store.System, &llmservice.Registry{Grants: []llmservice.Grant{{
		ID: "expired-referral-grant", UserID: "invitee", Email: "invitee@example.com", ServiceGroupID: "group-a", Source: "user_referral",
		StartsAt: now.AddDate(0, 0, -3), ExpiresAt: now.AddDate(0, 0, -1), CreatedAt: now.AddDate(0, 0, -3), CreditsTotal: 10,
	}}}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	h := GetUserReferralMetricsHandler(repo, userReferralMetricSystemSettings{SystemSettingsRepository: services.store.System, repo: repo})
	for range 2 {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/user-referrals/metrics?days=30", nil).WithContext(WithRequestTenant(ctx, store.DefaultTenantID))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("metrics status=%d body=%s", rec.Code, rec.Body.String())
		}
	}
	items, err := repo.ListDailyMetrics(ctx, store.DefaultTenantID, now.AddDate(0, 0, -2), now)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Event == userReferralMetricRewardExpired {
			if item.Count != 1 {
				t.Fatalf("expiry metric count=%d, want 1", item.Count)
			}
			return
		}
	}
	t.Fatalf("missing expiry metric: %#v", items)
}

func TestUserReferralMetricsRouteIsNotCapturedAsInviterID(t *testing.T) {
	services := newAdminRouterTestContext(t)
	globalToken := issueHubAdminToken(t, services.handler)
	token := issueTenantAdminToken(t, services.handler, globalToken, "referral-metrics", "referral-metrics-admin")
	rec := doHubAdminJSONRequest(t, services.handler, http.MethodGet, "/api/admin/user-referrals/metrics?days=7", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics route status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		From                string `json:"from"`
		To                  string `json:"to"`
		RewardFailedBacklog int    `json:"reward_failed_backlog"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode metrics route=%v body=%s", err, rec.Body.String())
	}
	if payload.From == "" || payload.To == "" || payload.RewardFailedBacklog != 0 {
		t.Fatalf("unexpected metrics route payload=%#v", payload)
	}
}

func TestUserReferralLandingAndModerationRecordOperationalMetrics(t *testing.T) {
	services := newAdminRouterTestContext(t)
	ctx := context.Background()
	now := time.Now().UTC()
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	inviter := &store.User{ID: "metric-inviter", TenantID: store.DefaultTenantID, Email: "metric-inviter@example.com", SN: "SN-metric-inviter", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	invitee := &store.User{ID: "metric-invitee", TenantID: store.DefaultTenantID, Email: "metric-invitee@example.com", SN: "SN-metric-invitee", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	for _, user := range []*store.User{inviter, invitee} {
		if err := services.store.Users.Create(ctx, user); err != nil {
			t.Fatal(err)
		}
	}
	plain := "rf_metric_landing_0123456789"
	encrypted, err := llmservice.EncryptCardCode(plain)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateCode(ctx, &store.UserReferralCode{ID: "metric-code", TenantID: store.DefaultTenantID, InviterUserID: inviter.ID, CodeHash: userReferralCodeHash(store.DefaultTenantID, plain), EncryptedCode: encrypted, Status: "active", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	cfg := UserReferralConfig{Enabled: true, DurationDays: 30, Downloads: defaultUserReferralDownloads(), SessionEpoch: "metric-epoch"}
	raw, _ := json.Marshal(cfg)
	if err := ScopedSystemSettingsForTenant(store.DefaultTenantID, services.store.System).Set(ctx, userReferralSettingsKey, string(raw)); err != nil {
		t.Fatal(err)
	}
	landing := httptest.NewRequest(http.MethodGet, "/invite/"+plain, nil)
	landing.Header.Set("Accept", "application/json")
	landing.Header.Set("User-Agent", "metric-test")
	landing.TLS = &tls.ConnectionState{}
	landingRec := httptest.NewRecorder()
	services.handler.ServeHTTP(landingRec, landing)
	if landingRec.Code != http.StatusOK {
		t.Fatalf("landing status=%d body=%s", landingRec.Code, landingRec.Body.String())
	}
	if err := repo.CreateReferral(ctx, &store.UserReferral{ID: "metric-reject", TenantID: store.DefaultTenantID, ReferralCodeID: "metric-code", InviterUserID: inviter.ID, InviteeUserID: invitee.ID, Status: "reserved", RegisteredAt: now, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	moderate := httptest.NewRequest(http.MethodPost, "/api/admin/user-referrals/metric-reject/reject", strings.NewReader(`{"reason":"risk confirmed"}`))
	moderate.SetPathValue("referral_id", "metric-reject")
	moderate.SetPathValue("action", "reject")
	moderate = moderate.WithContext(WithRequestTenant(moderate.Context(), store.DefaultTenantID))
	moderateRec := httptest.NewRecorder()
	ModerateUserReferralHandler(nil, repo, services.store.System, nil).ServeHTTP(moderateRec, moderate)
	if moderateRec.Code != http.StatusOK {
		t.Fatalf("reject status=%d body=%s", moderateRec.Code, moderateRec.Body.String())
	}
	metrics, err := repo.ListDailyMetrics(ctx, store.DefaultTenantID, now, now)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int64{}
	for _, metric := range metrics {
		counts[metric.Event] += metric.Count
	}
	if counts[userReferralMetricLanding] != 1 || counts[userReferralMetricRiskRejected] != 1 {
		t.Fatalf("operational counts=%#v", counts)
	}
}

func TestUpdateUserReferralConfigRejectsInvalidCredits(t *testing.T) {
	services := newAdminRouterTestContext(t)
	admin := &store.AdminUser{ID: "referral-credit-admin", Scope: "tenant", TenantID: store.DefaultTenantID}
	for _, tc := range []struct {
		name  string
		value string
	}{
		{name: "negative", value: "-1"},
		{name: "three decimals", value: "1.001"},
		{name: "over maximum", value: fmt.Sprintf("%.2f", userReferralMaxRewardCredits+.01)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"enabled":false,"inviter_credits":` + tc.value + `,"invitee_credits":0,"duration_days":30,"daily_reward_cap":20}`
			req := httptest.NewRequest(http.MethodPut, "/api/admin/user-referrals/config", strings.NewReader(body))
			req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, admin))
			rec := httptest.NewRecorder()
			UpdateUserReferralConfigHandler(services.store.System, nil).ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "INVALID_REFERRAL_CREDITS") {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
	valid := `{"enabled":false,"inviter_credits":10.25,"invitee_credits":5.50,"duration_days":30,"daily_reward_cap":20,"service_group_id":""}`
	if err := llmservice.SaveRegistry(context.Background(), services.store.System, &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "referral-credits"}}}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	valid = `{"enabled":false,"inviter_credits":10.25,"invitee_credits":5.50,"duration_days":30,"daily_reward_cap":20,"service_group_id":"referral-credits"}`
	req := httptest.NewRequest(http.MethodPut, "/api/admin/user-referrals/config", strings.NewReader(valid))
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, admin))
	rec := httptest.NewRecorder()
	UpdateUserReferralConfigHandler(services.store.System, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid cents status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUserReferralLandingJSONRespectsTenantRegistrationPolicy(t *testing.T) {
	services := newAdminRouterTestContext(t)
	ctx := context.Background()
	if _, err := services.db.ExecContext(ctx, `UPDATE tenants SET settings_json = ? WHERE id = ?`, `{"allow_user_registration":false}`, store.DefaultTenantID); err != nil {
		t.Fatalf("disable registrations: %v", err)
	}
	now := time.Now().UTC()
	if err := services.store.Users.Create(ctx, &store.User{ID: "referrer-policy", TenantID: store.DefaultTenantID, Email: "referrer-policy@example.com", SN: "SN-policy", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create inviter: %v", err)
	}
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	plain := "rf_test_policy_0123456789"
	encrypted, err := llmservice.EncryptCardCode(plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if err := repo.CreateCode(ctx, &store.UserReferralCode{ID: "ref-code-policy", TenantID: store.DefaultTenantID, InviterUserID: "referrer-policy", CodeHash: userReferralCodeHash(store.DefaultTenantID, plain), EncryptedCode: encrypted, Status: "active", CreatedAt: now}); err != nil {
		t.Fatalf("create referral code: %v", err)
	}
	cfg := UserReferralConfig{Enabled: true, InviterCredits: 0, InviteeCredits: 0, DurationDays: 30, Downloads: defaultUserReferralDownloads()}
	raw, _ := json.Marshal(cfg)
	if err := ScopedSystemSettingsForTenant(store.DefaultTenantID, services.store.System).Set(ctx, userReferralSettingsKey, string(raw)); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if stored, err := ScopedSystemSettingsForTenant(store.DefaultTenantID, services.store.System).Get(ctx, userReferralSettingsKey); err != nil || !strings.Contains(stored, `"enabled":true`) {
		t.Fatalf("stored config=%q err=%v", stored, err)
	}
	if found, err := repo.GetCodeByHash(ctx, store.DefaultTenantID, userReferralCodeHash(store.DefaultTenantID, plain)); err != nil || found == nil {
		t.Fatalf("stored code=%#v err=%v", found, err)
	}

	req := httptest.NewRequest(http.MethodGet, "/invite/"+plain, nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	services.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("landing status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Available bool `json:"available"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode landing: %v", err)
	}
	if payload.Available {
		t.Fatalf("landing must not be available when tenant registration is disabled: %s", rec.Body.String())
	}
}

func TestPublicUserReferralRegisterCreatesUserAndAttributionAtomically(t *testing.T) {
	services := newAdminRouterTestContext(t)
	ctx := context.Background()
	now := time.Now().UTC()
	inviter := &store.User{ID: "atomic-http-inviter", TenantID: store.DefaultTenantID, Email: "atomic-http-inviter@example.com", SN: "SN-atomic-http-inviter", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	if err := services.store.Users.Create(ctx, inviter); err != nil {
		t.Fatalf("create inviter: %v", err)
	}
	repo := services.store.UserReferrals
	plain := "rf_atomic_http_0123456789"
	encrypted, err := llmservice.EncryptCardCode(plain)
	if err != nil {
		t.Fatal(err)
	}
	codeHash := userReferralCodeHash(store.DefaultTenantID, plain)
	if err := repo.CreateCode(ctx, &store.UserReferralCode{ID: "atomic-http-code", TenantID: store.DefaultTenantID, InviterUserID: inviter.ID, CodeHash: codeHash, EncryptedCode: encrypted, Status: "active", CreatedAt: now}); err != nil {
		t.Fatalf("create referral code: %v", err)
	}
	if err := llmservice.SaveRegistry(ctx, services.store.System, &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "atomic-referral-group"}}}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	cfg := UserReferralConfig{Enabled: true, SessionEpoch: "atomic-http-epoch", InviterCredits: 10, InviteeCredits: 5, DurationDays: 30, DailyRewardCap: 20, DailyNetworkClientReviewCap: 100, ServiceGroupID: "atomic-referral-group", Downloads: defaultUserReferralDownloads()}
	raw, _ := json.Marshal(cfg)
	if err := ScopedSystemSettingsForTenant(store.DefaultTenantID, services.store.System).Set(ctx, userReferralSettingsKey, string(raw)); err != nil {
		t.Fatalf("save referral config: %v", err)
	}

	email := "atomic-http-invitee@example.com"
	landing := httptest.NewRequest(http.MethodGet, "/invite/"+plain, nil)
	landing.Header.Set("User-Agent", "atomic-http-browser")
	landingRec := httptest.NewRecorder()
	if err := newUserReferralRegistrationSession(ctx, repo, landingRec, landing, store.DefaultTenantID, codeHash, cfg.SessionEpoch); err != nil {
		t.Fatalf("create registration session: %v", err)
	}
	cookie := landingRec.Result().Cookies()[0]
	register := httptest.NewRequest(http.MethodPost, "/invite/"+plain+"/register", strings.NewReader(`{"email":"`+email+`","verify_code":"123456"}`))
	register.Header.Set("User-Agent", "atomic-http-browser")
	register.Header.Set("Content-Type", "application/json")
	register.AddCookie(cookie)
	if _, reserved, err := reserveUserReferralIdentity(ctx, repo, register, store.DefaultTenantID, codeHash, "email", email); err != nil || !reserved {
		t.Fatalf("reserve identity reserved=%v err=%v", reserved, err)
	}
	verifyKey := referralVerificationKey(codeHash, email)
	deleteVerifyCode(store.DefaultTenantID, verifyKey)
	if !storeVerifyCode(store.DefaultTenantID, verifyKey, "123456") {
		t.Fatal("store verification code")
	}
	t.Cleanup(func() { deleteVerifyCode(store.DefaultTenantID, verifyKey) })
	rec := httptest.NewRecorder()
	services.handler.ServeHTTP(rec, register)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register status=%d body=%s", rec.Code, rec.Body.String())
	}
	invitee, err := services.store.Users.GetByTenantEmail(ctx, store.DefaultTenantID, email)
	if err != nil || invitee == nil {
		t.Fatalf("registered invitee=%#v err=%v", invitee, err)
	}
	referral, err := repo.GetReferralForInvitee(ctx, store.DefaultTenantID, invitee.ID)
	if err != nil || referral == nil {
		t.Fatalf("registered referral=%#v err=%v", referral, err)
	}
	if referral.InviterUserID != inviter.ID || referral.Status != "rewarded" || referral.InviterGrantID == "" || referral.InviteeGrantID == "" {
		t.Fatalf("unexpected referral=%#v", referral)
	}
}

func TestUserReferralLandingHTMLSecurityHeaders(t *testing.T) {
	services := newAdminRouterTestContext(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := services.store.Users.Create(ctx, &store.User{ID: "referrer-headers", TenantID: store.DefaultTenantID, Email: "referrer-headers@example.com", SN: "SN-headers", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create inviter: %v", err)
	}
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	plain := "rf_test_headers_012345678"
	encrypted, err := llmservice.EncryptCardCode(plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if err := repo.CreateCode(ctx, &store.UserReferralCode{ID: "ref-code-headers", TenantID: store.DefaultTenantID, InviterUserID: "referrer-headers", CodeHash: userReferralCodeHash(store.DefaultTenantID, plain), EncryptedCode: encrypted, Status: "active", CreatedAt: now}); err != nil {
		t.Fatalf("create referral code: %v", err)
	}
	cfg := UserReferralConfig{Enabled: true, DurationDays: 30, Downloads: defaultUserReferralDownloads()}
	raw, _ := json.Marshal(cfg)
	if err := ScopedSystemSettingsForTenant(store.DefaultTenantID, services.store.System).Set(ctx, userReferralSettingsKey, string(raw)); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if found, err := repo.GetCodeByHash(ctx, store.DefaultTenantID, userReferralCodeHash(store.DefaultTenantID, plain)); err != nil || found == nil {
		t.Fatalf("stored code=%#v err=%v", found, err)
	}
	req := httptest.NewRequest(http.MethodGet, "/invite/"+plain, nil)
	rec := httptest.NewRecorder()
	services.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("landing status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("Referrer-Policy=%q", got)
	}
	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options=%q", got)
	}
	if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "connect-src 'self'") {
		t.Fatalf("unexpected CSP=%q", rec.Header().Get("Content-Security-Policy"))
	}
}

func TestUserReferralLandingExposesOnlyValidatedTenantLogo(t *testing.T) {
	services := newAdminRouterTestContext(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if _, err := services.db.ExecContext(ctx, `UPDATE tenants SET settings_json = ? WHERE id = ?`, `{"logo_url":"https://cdn.example.test/acme.svg"}`, store.DefaultTenantID); err != nil {
		t.Fatalf("save tenant logo: %v", err)
	}
	if err := services.store.Users.Create(ctx, &store.User{ID: "referrer-logo", TenantID: store.DefaultTenantID, Email: "referrer-logo@example.com", SN: "SN-logo", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create inviter: %v", err)
	}
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	plain := "rf_test_logo_012345678"
	encrypted, err := llmservice.EncryptCardCode(plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if err := repo.CreateCode(ctx, &store.UserReferralCode{ID: "ref-code-logo", TenantID: store.DefaultTenantID, InviterUserID: "referrer-logo", CodeHash: userReferralCodeHash(store.DefaultTenantID, plain), EncryptedCode: encrypted, Status: "active", CreatedAt: now}); err != nil {
		t.Fatalf("create referral code: %v", err)
	}
	cfgData, _ := json.Marshal(UserReferralConfig{Enabled: true, DurationDays: 30, Downloads: defaultUserReferralDownloads()})
	if err := ScopedSystemSettingsForTenant(store.DefaultTenantID, services.store.System).Set(ctx, userReferralSettingsKey, string(cfgData)); err != nil {
		t.Fatalf("save config: %v", err)
	}
	jsonReq := httptest.NewRequest(http.MethodGet, "/invite/"+plain, nil)
	jsonReq.Header.Set("Accept", "application/json")
	jsonRec := httptest.NewRecorder()
	services.handler.ServeHTTP(jsonRec, jsonReq)
	if jsonRec.Code != http.StatusOK || !strings.Contains(jsonRec.Body.String(), `"logo_url":"https://cdn.example.test/acme.svg"`) {
		t.Fatalf("json landing status=%d body=%s", jsonRec.Code, jsonRec.Body.String())
	}
	htmlRec := httptest.NewRecorder()
	services.handler.ServeHTTP(htmlRec, httptest.NewRequest(http.MethodGet, "/invite/"+plain, nil))
	if htmlRec.Code != http.StatusOK || !strings.Contains(htmlRec.Body.String(), `src="https://cdn.example.test/acme.svg"`) || !strings.Contains(htmlRec.Header().Get("Content-Security-Policy"), "https://cdn.example.test") {
		t.Fatalf("html landing status=%d csp=%q body=%s", htmlRec.Code, htmlRec.Header().Get("Content-Security-Policy"), htmlRec.Body.String())
	}
	if _, err := services.db.ExecContext(ctx, `UPDATE tenants SET settings_json = ? WHERE id = ?`, `{"logo_url":"http://bad.example.test/logo.svg"}`, store.DefaultTenantID); err != nil {
		t.Fatalf("save invalid logo: %v", err)
	}
	jsonRec = httptest.NewRecorder()
	services.handler.ServeHTTP(jsonRec, jsonReq)
	if strings.Contains(jsonRec.Body.String(), "bad.example.test") {
		t.Fatalf("invalid logo leaked into landing response: %s", jsonRec.Body.String())
	}
}

func TestPublicUserReferralRegistrationStatusAndAccountCheck(t *testing.T) {
	services := newAdminRouterTestContext(t)
	ctx := context.Background()
	now := time.Now().UTC()
	const inviterID = "referrer-public-check"
	const code = "rf_test_public_check_012345678"
	if err := services.store.Users.Create(ctx, &store.User{ID: inviterID, TenantID: store.DefaultTenantID, Email: "referrer-public-check@example.com", SN: "SN-public-check", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create inviter: %v", err)
	}
	if err := services.store.Users.Create(ctx, &store.User{ID: "existing-public-check", TenantID: store.DefaultTenantID, Email: "existing-public-check@example.com", SN: "SN-existing-public-check", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create existing user: %v", err)
	}
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	encrypted, err := llmservice.EncryptCardCode(code)
	if err != nil {
		t.Fatalf("encrypt code: %v", err)
	}
	codeHash := userReferralCodeHash(store.DefaultTenantID, code)
	if err := repo.CreateCode(ctx, &store.UserReferralCode{ID: "ref-code-public-check", TenantID: store.DefaultTenantID, InviterUserID: inviterID, CodeHash: codeHash, EncryptedCode: encrypted, Status: "active", CreatedAt: now}); err != nil {
		t.Fatalf("create referral code: %v", err)
	}
	downloads := defaultUserReferralDownloads()
	downloads.WindowsAMD64 = "https://downloads.example.test/acme/windows.exe"
	cfg := UserReferralConfig{Enabled: true, DurationDays: 30, Downloads: downloads, SessionEpoch: "public-check-epoch"}
	raw, _ := json.Marshal(cfg)
	if err := ScopedSystemSettingsForTenant(store.DefaultTenantID, services.store.System).Set(ctx, userReferralSettingsKey, string(raw)); err != nil {
		t.Fatalf("save referral config: %v", err)
	}
	if err := ScopedSystemSettingsForTenant(store.DefaultTenantID, services.store.System).Set(ctx, registrationAuthConfigKey, `{"method":"mixed"}`); err != nil {
		t.Fatalf("save registration auth: %v", err)
	}

	landing := httptest.NewRequest(http.MethodGet, "/invite/"+code, nil)
	landing.Header.Set("User-Agent", "public-check-browser")
	landingRec := httptest.NewRecorder()
	services.handler.ServeHTTP(landingRec, landing)
	if landingRec.Code != http.StatusOK {
		t.Fatalf("landing status=%d body=%s", landingRec.Code, landingRec.Body.String())
	}
	cookies := landingRec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected registration cookie, got %#v", cookies)
	}
	newRequest := func(method, target, body string) *http.Request {
		r := httptest.NewRequest(method, target, strings.NewReader(body))
		r.Header.Set("User-Agent", "public-check-browser")
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(cookies[0])
		return r
	}

	statusRec := httptest.NewRecorder()
	services.handler.ServeHTTP(statusRec, newRequest(http.MethodGet, "/invite/"+code+"/registration/status", ""))
	if statusRec.Code != http.StatusOK || !strings.Contains(statusRec.Body.String(), `"registration_method":"mixed"`) || !strings.Contains(statusRec.Body.String(), downloads.WindowsAMD64) {
		t.Fatalf("status=%d body=%s", statusRec.Code, statusRec.Body.String())
	}
	if strings.Contains(statusRec.Body.String(), code) || strings.Contains(statusRec.Body.String(), inviterID) {
		t.Fatalf("status leaked referral data: %s", statusRec.Body.String())
	}

	eligibleRec := httptest.NewRecorder()
	services.handler.ServeHTTP(eligibleRec, newRequest(http.MethodPost, "/invite/"+code+"/registration/account-check", `{"email":"brand-new-public-check@example.com"}`))
	if eligibleRec.Code != http.StatusOK || !strings.Contains(eligibleRec.Body.String(), `"eligible":true`) {
		t.Fatalf("eligible check status=%d body=%s", eligibleRec.Code, eligibleRec.Body.String())
	}

	existingRec := httptest.NewRecorder()
	services.handler.ServeHTTP(existingRec, newRequest(http.MethodPost, "/invite/"+code+"/registration/account-check", `{"email":"existing-public-check@example.com"}`))
	if existingRec.Code != http.StatusConflict || !strings.Contains(existingRec.Body.String(), `"reason":"existing_user"`) || !strings.Contains(existingRec.Body.String(), downloads.WindowsAMD64) {
		t.Fatalf("existing check status=%d body=%s", existingRec.Code, existingRec.Body.String())
	}
	if strings.Contains(existingRec.Body.String(), code) || strings.Contains(existingRec.Body.String(), inviterID) {
		t.Fatalf("existing check leaked referral data: %s", existingRec.Body.String())
	}

	invalidRec := httptest.NewRecorder()
	services.handler.ServeHTTP(invalidRec, newRequest(http.MethodPost, "/invite/"+code+"/registration/account-check", `{"email":"a@example.com","phone":"13800138000"}`))
	if invalidRec.Code != http.StatusBadRequest || !strings.Contains(invalidRec.Body.String(), "CONTACT_REQUIRED") {
		t.Fatalf("invalid check status=%d body=%s", invalidRec.Code, invalidRec.Body.String())
	}
}

func TestPublicUserReferralRegistrationStatusAndAccountCheckRequireSession(t *testing.T) {
	services := newAdminRouterTestContext(t)
	for _, tc := range []struct {
		method string
		target string
		body   string
	}{
		{method: http.MethodGet, target: "/invite/not-a-real-code/registration/status"},
		{method: http.MethodPost, target: "/invite/not-a-real-code/registration/account-check", body: `{"email":"check@example.com"}`},
	} {
		req := httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		services.handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s status=%d body=%s", tc.method, tc.target, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "downloads") || strings.Contains(rec.Body.String(), "existing_user") {
			t.Fatalf("%s %s leaked registration data: %s", tc.method, tc.target, rec.Body.String())
		}
	}
}

func TestPublicUserReferralRegistrationStatusRecoversCompletedReferral(t *testing.T) {
	services := newAdminRouterTestContext(t)
	ctx := context.Background()
	now := time.Now().UTC()
	const inviterID = "referrer-status-recovery"
	const inviteeID = "invitee-status-recovery"
	const code = "rf_test_status_recovery_012345678"
	for _, user := range []*store.User{
		{ID: inviterID, TenantID: store.DefaultTenantID, Email: "referrer-status-recovery@example.com", SN: "SN-status-recovery-inviter", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
		{ID: inviteeID, TenantID: store.DefaultTenantID, Email: "invitee-status-recovery@example.com", SN: "SN-status-recovery-invitee", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
	} {
		if err := services.store.Users.Create(ctx, user); err != nil {
			t.Fatalf("create user %s: %v", user.ID, err)
		}
	}
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	encrypted, err := llmservice.EncryptCardCode(code)
	if err != nil {
		t.Fatalf("encrypt code: %v", err)
	}
	codeHash := userReferralCodeHash(store.DefaultTenantID, code)
	if err := repo.CreateCode(ctx, &store.UserReferralCode{ID: "ref-code-status-recovery", TenantID: store.DefaultTenantID, InviterUserID: inviterID, CodeHash: codeHash, EncryptedCode: encrypted, Status: "active", CreatedAt: now}); err != nil {
		t.Fatalf("create referral code: %v", err)
	}
	cfg := UserReferralConfig{Enabled: true, DurationDays: 30, Downloads: defaultUserReferralDownloads(), SessionEpoch: "status-recovery-epoch"}
	raw, _ := json.Marshal(cfg)
	if err := ScopedSystemSettingsForTenant(store.DefaultTenantID, services.store.System).Set(ctx, userReferralSettingsKey, string(raw)); err != nil {
		t.Fatalf("save config: %v", err)
	}
	seed := httptest.NewRequest(http.MethodGet, "/invite/"+code, nil)
	seed.Header.Set("User-Agent", "status-recovery-browser")
	seedRec := httptest.NewRecorder()
	services.handler.ServeHTTP(seedRec, seed)
	if seedRec.Code != http.StatusOK || len(seedRec.Result().Cookies()) != 1 {
		t.Fatalf("seed status=%d cookies=%#v", seedRec.Code, seedRec.Result().Cookies())
	}
	cookie := seedRec.Result().Cookies()[0]
	sessionHash := userReferralRegistrationSessionTokenHash(store.DefaultTenantID, cookie.Value)
	for _, tc := range []struct {
		name   string
		status string
		want   string
	}{
		{name: "review", status: "reserved", want: "approval_pending"},
		{name: "pending", status: "attributed", want: "registered_reward_pending"},
		{name: "rewarded", status: "rewarded", want: "registered_rewarded"},
		{name: "failed", status: "reward_failed", want: "registered_reward_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			referralID := "ref-status-" + tc.name
			if err := repo.CreateReferral(ctx, &store.UserReferral{ID: referralID, TenantID: store.DefaultTenantID, ReferralCodeID: "ref-code-status-recovery", InviterUserID: inviterID, InviteeUserID: inviteeID, Status: tc.status, RegisteredAt: now, DurationDays: 30, CreatedAt: now, UpdatedAt: now}); err != nil {
				t.Fatalf("create referral: %v", err)
			}
			if err := repo.MarkRegistrationSessionCompleted(ctx, store.DefaultTenantID, sessionHash, inviteeID, referralID, now); err != nil {
				t.Fatalf("complete session: %v", err)
			}
			req := httptest.NewRequest(http.MethodGet, "/invite/"+code+"/registration/status", nil)
			req.Header.Set("User-Agent", "status-recovery-browser")
			req.AddCookie(cookie)
			rec := httptest.NewRecorder()
			services.handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"registration_status":"`+tc.want+`"`) {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), code) || strings.Contains(rec.Body.String(), inviterID) || strings.Contains(rec.Body.String(), inviteeID) {
				t.Fatalf("recovery response leaked identity: %s", rec.Body.String())
			}
			if tc.name != "failed" {
				if _, err := services.db.ExecContext(ctx, `DELETE FROM user_referrals WHERE tenant_id = ? AND id = ?`, store.DefaultTenantID, referralID); err != nil {
					t.Fatalf("cleanup referral: %v", err)
				}
			}
		})
	}
	for _, tc := range []struct {
		name   string
		status string
		want   string
	}{
		{name: "expired", status: "expired", want: "registered_expired"},
		{name: "rejected", status: "rejected", want: "registered_rejected"},
		{name: "revoked", status: "revoked", want: "registered_revoked"},
	} {
		if got := userReferralRegistrationRecoveryStatus(tc.status); got != tc.want {
			t.Fatalf("recovery status %s=%q; want %q", tc.status, got, tc.want)
		}
	}
}

func TestCleanupExpiredUserReferralsScopesReservedExpiryToActiveTenants(t *testing.T) {
	services := newAdminRouterTestContext(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	if err := services.store.Tenants.Create(ctx, &store.Tenant{ID: "referral-inactive-tenant", Slug: "referral-inactive", Name: "Referral inactive", Status: "inactive", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create inactive tenant: %v", err)
	}
	for _, referral := range []*store.UserReferral{
		{ID: "referral-cleanup-active", TenantID: store.DefaultTenantID, ReferralCodeID: "code", InviterUserID: "inviter", InviteeUserID: "invitee-active", Status: "reserved", RegisteredAt: now.Add(-8 * 24 * time.Hour), CreatedAt: now.Add(-8 * 24 * time.Hour), UpdatedAt: now.Add(-8 * 24 * time.Hour)},
		{ID: "referral-cleanup-inactive", TenantID: "referral-inactive-tenant", ReferralCodeID: "code", InviterUserID: "inviter", InviteeUserID: "invitee-inactive", Status: "reserved", RegisteredAt: now.Add(-8 * 24 * time.Hour), CreatedAt: now.Add(-8 * 24 * time.Hour), UpdatedAt: now.Add(-8 * 24 * time.Hour)},
	} {
		if err := services.store.UserReferrals.CreateReferral(ctx, referral); err != nil {
			t.Fatalf("create referral %s: %v", referral.ID, err)
		}
	}
	result, err := CleanupExpiredUserReferrals(ctx, services.store.UserReferrals, services.store.Tenants, now)
	if err != nil || result.ExpiredReserved != 1 {
		t.Fatalf("cleanup result=%#v err=%v; want one active-tenant expiry", result, err)
	}
	active, err := services.store.UserReferrals.GetReferralByID(ctx, store.DefaultTenantID, "referral-cleanup-active")
	if err != nil || active == nil || active.Status != "expired" {
		t.Fatalf("active referral=%#v err=%v; want expired", active, err)
	}
	inactive, err := services.store.UserReferrals.GetReferralByID(ctx, "referral-inactive-tenant", "referral-cleanup-inactive")
	if err != nil || inactive == nil || inactive.Status != "reserved" {
		t.Fatalf("inactive referral=%#v err=%v; want unchanged reserved", inactive, err)
	}
}

func TestPublicUserReferralEmailEnrollRequiresCompletedReferralSession(t *testing.T) {
	services := newAdminRouterTestContext(t)
	ctx := context.Background()
	now := time.Now().UTC()
	const code = "rf_test_email_enroll_0123456789"
	const inviterID = "referrer-email-enroll"
	const inviteeID = "invitee-email-enroll"
	if err := services.store.Users.Create(ctx, &store.User{ID: inviterID, TenantID: store.DefaultTenantID, Email: "referrer-email-enroll@example.com", SN: "SN-email-enroll-inviter", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create inviter: %v", err)
	}
	if err := services.store.Users.Create(ctx, &store.User{ID: inviteeID, TenantID: store.DefaultTenantID, Email: "invitee-email-enroll@example.com", SN: "SN-email-enroll-invitee", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create invitee: %v", err)
	}
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	encrypted, err := llmservice.EncryptCardCode(code)
	if err != nil {
		t.Fatalf("encrypt referral code: %v", err)
	}
	codeHash := userReferralCodeHash(store.DefaultTenantID, code)
	if err := repo.CreateCode(ctx, &store.UserReferralCode{ID: "ref-code-email-enroll", TenantID: store.DefaultTenantID, InviterUserID: inviterID, CodeHash: codeHash, EncryptedCode: encrypted, Status: "active", CreatedAt: now}); err != nil {
		t.Fatalf("create referral code: %v", err)
	}
	cfg := UserReferralConfig{Enabled: true, DurationDays: 30, Downloads: defaultUserReferralDownloads(), SessionEpoch: "email-enroll-epoch"}
	raw, _ := json.Marshal(cfg)
	if err := ScopedSystemSettingsForTenant(store.DefaultTenantID, services.store.System).Set(ctx, userReferralSettingsKey, string(raw)); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if err := repo.CreateReferral(ctx, &store.UserReferral{ID: "referral-email-enroll", TenantID: store.DefaultTenantID, ReferralCodeID: "ref-code-email-enroll", InviterUserID: inviterID, InviteeUserID: inviteeID, Status: "rewarded", RegisteredAt: now, DurationDays: 30, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create referral: %v", err)
	}
	seed := httptest.NewRequest(http.MethodGet, "/invite/"+code, nil)
	seed.Header.Set("User-Agent", "email-enroll-desktop")
	session, err := newUserReferralRegistrationSessionToken(ctx, repo, seed, store.DefaultTenantID, codeHash, cfg.SessionEpoch)
	if err != nil {
		t.Fatalf("create desktop session: %v", err)
	}
	sessionRequest := httptest.NewRequest(http.MethodPost, "/api/public/referral-registration/email/enroll", nil)
	sessionRequest.Header.Set("User-Agent", "email-enroll-desktop")
	sessionRequest.Header.Set(userReferralRegistrationHeader, session)
	sessionRequest.Header.Set(userReferralRegistrationTenantHeader, store.DefaultTenantID)
	if _, reserved, err := reserveUserReferralIdentity(ctx, repo, sessionRequest, store.DefaultTenantID, codeHash, "email", "invitee-email-enroll@example.com"); err != nil || !reserved {
		t.Fatalf("reserve completed referral identity reserved=%v err=%v", reserved, err)
	}
	request := func(token string) *http.Request {
		body := `{"email":"invitee-email-enroll@example.com","machine_name":"Referral Desktop","platform":"windows","client_id":"email-enroll-client"}`
		r := httptest.NewRequest(http.MethodPost, "/api/public/referral-registration/email/enroll", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("User-Agent", "email-enroll-desktop")
		r.Header.Set(userReferralRegistrationHeader, token)
		r.Header.Set(userReferralRegistrationTenantHeader, store.DefaultTenantID)
		return r
	}
	rec := httptest.NewRecorder()
	services.handler.ServeHTTP(rec, request(session))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"machine_id"`) || !strings.Contains(rec.Body.String(), inviteeID) {
		t.Fatalf("email enrollment status=%d body=%s", rec.Code, rec.Body.String())
	}
	badRec := httptest.NewRecorder()
	services.handler.ServeHTTP(badRec, request("not-a-valid-referral-session"))
	if badRec.Code != http.StatusNotFound || strings.Contains(badRec.Body.String(), `"machine_id"`) {
		t.Fatalf("invalid session status=%d body=%s", badRec.Code, badRec.Body.String())
	}
}

func TestUpdateUserReferralConfigRequiresConfiguredServiceGroup(t *testing.T) {
	services := newAdminRouterTestContext(t)
	token := issueTenantAdminTokenForTest(t, services, "referral-settings")
	resp := doHubAdminJSONRequest(t, services.handler, http.MethodPut, "/api/admin/user-referrals/config", map[string]any{"enabled": true, "inviter_credits": 100, "invitee_credits": 50, "duration_days": 30, "service_group_id": "does-not-exist"}, token)
	if resp.Code != http.StatusBadRequest || !strings.Contains(resp.Body.String(), "SERVICE_GROUP_NOT_FOUND") {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestUpdateUserReferralConfigRejectsUnsafeDownloadURL(t *testing.T) {
	services := newAdminRouterTestContext(t)
	token := issueTenantAdminTokenForTest(t, services, "referral-download-url")
	resp := doHubAdminJSONRequest(t, services.handler, http.MethodPut, "/api/admin/user-referrals/config", map[string]any{
		"downloads": map[string]any{"windows_amd64": "http://downloads.example/installer.exe"},
	}, token)
	if resp.Code != http.StatusBadRequest || !strings.Contains(resp.Body.String(), "INVALID_DOWNLOAD_URL") {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestUpdateUserReferralConfigRejectsStaleVersion(t *testing.T) {
	services := newAdminRouterTestContext(t)
	token := issueTenantAdminTokenForTest(t, services, "referral-version")
	get := doHubAdminJSONRequest(t, services.handler, http.MethodGet, "/api/admin/user-referrals/config", nil, token)
	if get.Code != http.StatusOK || get.Header().Get("ETag") == "" {
		t.Fatalf("get status=%d etag=%q body=%s", get.Code, get.Header().Get("ETag"), get.Body.String())
	}
	first := doHubAdminJSONRequest(t, services.handler, http.MethodPut, "/api/admin/user-referrals/config", map[string]any{"duration_days": 45}, token)
	if first.Code != http.StatusOK {
		t.Fatalf("first update status=%d body=%s", first.Code, first.Body.String())
	}
	var body strings.Builder
	_ = json.NewEncoder(&body).Encode(map[string]any{"duration_days": 60})
	req := httptest.NewRequest(http.MethodPut, "/api/admin/user-referrals/config", strings.NewReader(body.String()))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("If-Match", get.Header().Get("ETag"))
	rec := httptest.NewRecorder()
	services.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "USER_REFERRAL_CONFIG_CONFLICT") {
		t.Fatalf("stale update status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestBoundUsersExposeReferralIndicator(t *testing.T) {
	services := newAdminRouterTestContext(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for _, user := range []*store.User{
		{ID: "referrer-indicator", TenantID: store.DefaultTenantID, Email: "referrer-indicator@example.com", SN: "SN-a", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
		{ID: "invitee-indicator", TenantID: store.DefaultTenantID, Email: "invitee-indicator@example.com", SN: "SN-b", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
	} {
		if err := services.store.Users.Create(ctx, user); err != nil {
			t.Fatalf("create user: %v", err)
		}
	}
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	if err := repo.CreateReferral(ctx, &store.UserReferral{ID: "referral-indicator", TenantID: store.DefaultTenantID, ReferralCodeID: "code", InviterUserID: "referrer-indicator", InviteeUserID: "invitee-indicator", Status: "rewarded", RegisteredAt: now, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create referral: %v", err)
	}
	token := issueHubAdminToken(t, services.handler)
	resp := doHubAdminJSONRequest(t, services.handler, http.MethodGet, "/api/admin/users", nil, token)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"referral":{"referral_id":"referral-indicator","inviter_user_id":"referrer-indicator","inviter_display_name":"r***@example.com","reward_status":"rewarded"`) {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestBoundUsersHideRevokedReferralIndicator(t *testing.T) {
	services := newAdminRouterTestContext(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for _, user := range []*store.User{
		{ID: "referrer-revoked", TenantID: store.DefaultTenantID, Email: "referrer-revoked@example.com", SN: "SN-r1", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
		{ID: "invitee-revoked", TenantID: store.DefaultTenantID, Email: "invitee-revoked@example.com", SN: "SN-r2", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
	} {
		if err := services.store.Users.Create(ctx, user); err != nil {
			t.Fatalf("create user: %v", err)
		}
	}
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	if err := repo.CreateReferral(ctx, &store.UserReferral{ID: "referral-revoked", TenantID: store.DefaultTenantID, ReferralCodeID: "code", InviterUserID: "referrer-revoked", InviteeUserID: "invitee-revoked", Status: "revoked", RegisteredAt: now, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create referral: %v", err)
	}
	resp := doHubAdminJSONRequest(t, services.handler, http.MethodGet, "/api/admin/users", nil, issueHubAdminToken(t, services.handler))
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	if strings.Contains(resp.Body.String(), `"referral":`) {
		t.Fatalf("revoked referral must not appear as an active invitation badge: %s", resp.Body.String())
	}
}

func TestBoundUsersFilterByRegistrationSource(t *testing.T) {
	services := newAdminRouterTestContext(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for _, user := range []*store.User{
		{ID: "referrer-source", TenantID: store.DefaultTenantID, Email: "referrer-source@example.com", SN: "SN-s1", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
		{ID: "invitee-source", TenantID: store.DefaultTenantID, Email: "invitee-source@example.com", SN: "SN-s2", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
		{ID: "direct-source", TenantID: store.DefaultTenantID, Email: "direct-source@example.com", SN: "SN-s3", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
	} {
		if err := services.store.Users.Create(ctx, user); err != nil {
			t.Fatalf("create user: %v", err)
		}
	}
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	if err := repo.CreateReferral(ctx, &store.UserReferral{ID: "referral-source", TenantID: store.DefaultTenantID, ReferralCodeID: "code", InviterUserID: "referrer-source", InviteeUserID: "invitee-source", Status: "rewarded", RegisteredAt: now, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create referral: %v", err)
	}
	token := issueHubAdminToken(t, services.handler)
	referral := doHubAdminJSONRequest(t, services.handler, http.MethodGet, "/api/admin/users?registration_source=referral", nil, token)
	if referral.Code != http.StatusOK || !strings.Contains(referral.Body.String(), "invitee-source@example.com") || strings.Contains(referral.Body.String(), "direct-source@example.com") {
		t.Fatalf("referral filter status=%d body=%s", referral.Code, referral.Body.String())
	}
	direct := doHubAdminJSONRequest(t, services.handler, http.MethodGet, "/api/admin/users?registration_source=direct", nil, token)
	if direct.Code != http.StatusOK || !strings.Contains(direct.Body.String(), "direct-source@example.com") || strings.Contains(direct.Body.String(), "invitee-source@example.com") {
		t.Fatalf("direct filter status=%d body=%s", direct.Code, direct.Body.String())
	}
	invalid := doHubAdminJSONRequest(t, services.handler, http.MethodGet, "/api/admin/users?registration_source=unknown", nil, token)
	if invalid.Code != http.StatusBadRequest || !strings.Contains(invalid.Body.String(), "INVALID_REGISTRATION_SOURCE") {
		t.Fatalf("invalid source status=%d body=%s", invalid.Code, invalid.Body.String())
	}
}

func TestUserReferralCodeRotationInvalidatesOldCode(t *testing.T) {
	services := newAdminRouterTestContext(t)
	ctx := context.Background()
	now := time.Now().UTC()
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	if err := repo.CreateCode(ctx, &store.UserReferralCode{ID: "rotation-code", TenantID: store.DefaultTenantID, InviterUserID: "rotation-user", CodeHash: "rotation-hash", EncryptedCode: "encrypted", Status: "active", CreatedAt: now}); err != nil {
		t.Fatalf("create code: %v", err)
	}
	if err := repo.RotateCode(ctx, store.DefaultTenantID, "rotation-code", now); err != nil {
		t.Fatalf("rotate code: %v", err)
	}
	active, err := repo.GetActiveCodeForInviter(ctx, store.DefaultTenantID, "rotation-user")
	if err != nil || active != nil {
		t.Fatalf("rotated code should not remain active: code=%#v err=%v", active, err)
	}
}

func TestGetActiveUserReferralCodeForInviterUsesNewestStableRecord(t *testing.T) {
	services := newAdminRouterTestContext(t)
	ctx := context.Background()
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	now := time.Now().UTC().Truncate(time.Second)
	// The unique active-code constraint normally prevents this state. Seed a
	// legacy-corrupt record to make the deterministic read contract explicit.
	for _, code := range []*store.UserReferralCode{
		{ID: "legacy-active-old", TenantID: store.DefaultTenantID, InviterUserID: "legacy-inviter", CodeHash: "legacy-hash-old", EncryptedCode: "legacy-encrypted-old", Status: "rotated", CreatedAt: now},
		{ID: "legacy-active-new", TenantID: store.DefaultTenantID, InviterUserID: "legacy-inviter", CodeHash: "legacy-hash-new", EncryptedCode: "legacy-encrypted-new", Status: "active", CreatedAt: now.Add(time.Second)},
	} {
		if err := repo.CreateCode(ctx, code); err != nil {
			t.Fatalf("create code %s: %v", code.ID, err)
		}
	}
	active, err := repo.GetActiveCodeForInviter(ctx, store.DefaultTenantID, "legacy-inviter")
	if err != nil || active == nil || active.ID != "legacy-active-new" {
		t.Fatalf("active code=%#v err=%v", active, err)
	}
}

func TestUserInvitationHandlersReturnServiceUnavailableForMissingDependencies(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/me/invitations", nil)
	recorder := httptest.NewRecorder()
	GetMyUserInvitationsHandler(nil, nil, nil).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), "USER_REFERRAL_UNAVAILABLE") {
		t.Fatalf("get handler status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	request = httptest.NewRequest(http.MethodPost, "/api/me/invitations/rotate", nil)
	recorder = httptest.NewRecorder()
	RotateMyUserInvitationHandler(nil, nil, nil).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), "USER_REFERRAL_UNAVAILABLE") {
		t.Fatalf("rotate handler status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	request = httptest.NewRequest(http.MethodPost, "/api/admin/user-referrals/referral/retry", nil)
	recorder = httptest.NewRecorder()
	RetryUserReferralRewardHandler(nil, nil, nil, nil).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), "USER_REFERRAL_UNAVAILABLE") {
		t.Fatalf("retry handler status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	request = httptest.NewRequest(http.MethodPost, "/api/admin/user-referrals/referral/approve", nil)
	recorder = httptest.NewRecorder()
	ModerateUserReferralHandler(nil, nil, nil, nil).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), "USER_REFERRAL_UNAVAILABLE") {
		t.Fatalf("moderation handler status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestUserReferralIdempotencyReplayAndConflict(t *testing.T) {
	userReferralIdempotency.Lock()
	userReferralIdempotency.records = map[string]userReferralIdempotencyRecord{}
	userReferralIdempotency.Unlock()
	request := httptest.NewRequest(http.MethodPost, "/invite/rf_test/register", nil)
	request.Header.Set("Idempotency-Key", "replay-key")
	key, replay, err := beginUserReferralIdempotency(request, "payload-a")
	if err != nil || key != "replay-key" || replay != nil {
		t.Fatalf("first idempotency lookup key=%q replay=%#v err=%v", key, replay, err)
	}
	finishUserReferralIdempotency(key, "payload-a", http.StatusCreated, []byte(`{"registered":true}`))
	_, replay, err = beginUserReferralIdempotency(request, "payload-a")
	if err != nil || replay == nil || replay.status != http.StatusCreated {
		t.Fatalf("expected a replay record, got %#v err=%v", replay, err)
	}
	conflict := httptest.NewRequest(http.MethodPost, "/invite/rf_test/register", nil)
	conflict.Header.Set("Idempotency-Key", "replay-key")
	if _, _, err := beginUserReferralIdempotency(conflict, "payload-b"); err == nil {
		t.Fatal("same key with a different request must conflict")
	}
}

func TestPersistedUserReferralIdempotencySurvivesProcessCacheReset(t *testing.T) {
	services := newAdminRouterTestContext(t)
	ctx := context.Background()
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	request := httptest.NewRequest(http.MethodPost, "/invite/rf_test/register", nil)
	request.Header.Set("Idempotency-Key", "persistent-replay-key")
	keyHash, replay, err := beginPersistedUserReferralIdempotency(ctx, repo, store.DefaultTenantID, request, "payload-a")
	if err != nil || keyHash == "" || replay != nil {
		t.Fatalf("first persistent idempotency lookup key=%q replay=%#v err=%v", keyHash, replay, err)
	}
	if err := finishPersistedUserReferralIdempotency(ctx, repo, store.DefaultTenantID, keyHash, "payload-a", http.StatusCreated, []byte(`{"registered":true}`)); err != nil {
		t.Fatalf("save persistent idempotency: %v", err)
	}
	// A fresh repository instance represents a new Hub process, which cannot
	// rely on the former process-local map.
	freshRepo := storesqlite.NewUserReferralRepository(services.db, services.db)
	_, replay, err = beginPersistedUserReferralIdempotency(ctx, freshRepo, store.DefaultTenantID, request, "payload-a")
	if err != nil || replay == nil || replay.status != http.StatusCreated || string(replay.payload) != `{"registered":true}` {
		t.Fatalf("expected durable replay after cache reset: replay=%#v err=%v", replay, err)
	}
	if _, _, err := beginPersistedUserReferralIdempotency(ctx, freshRepo, store.DefaultTenantID, request, "payload-b"); err == nil {
		t.Fatal("same durable key with a different request must conflict")
	}
}

func TestReferralRegistrationKeepsSuccessWhenRewardProcessingFails(t *testing.T) {
	services := newAdminRouterTestContext(t)
	ctx := context.Background()
	now := time.Now().UTC()
	inviter := &store.User{ID: "reward-fail-inviter", TenantID: store.DefaultTenantID, Email: "reward-fail-inviter@example.com", SN: "SN-reward-fail-inviter", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	invitee := &store.User{ID: "reward-fail-invitee", TenantID: store.DefaultTenantID, Email: "reward-fail-invitee@example.com", SN: "SN-reward-fail-invitee", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	for _, user := range []*store.User{inviter, invitee} {
		if err := services.store.Users.Create(ctx, user); err != nil {
			t.Fatalf("create user: %v", err)
		}
	}
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	identity := auth.NewIdentityService(services.store.Users, nil, nil, nil, nil, nil, services.store.System, nil, "open", true, nil, "")
	code := &store.UserReferralCode{ID: "reward-fail-code", TenantID: store.DefaultTenantID, InviterUserID: inviter.ID, CodeHash: "test-code-hash", Status: "active", CreatedAt: now}
	cfg := UserReferralConfig{Enabled: true, InviterCredits: 10, InviteeCredits: 5, DurationDays: 30, ServiceGroupID: "missing-service-group", Downloads: defaultUserReferralDownloads()}
	req := httptest.NewRequest(http.MethodPost, "/invite/test/register", nil)
	req.Header.Set("Idempotency-Key", "reward-failure-success-key")
	rec := httptest.NewRecorder()
	writeReferralRegistrationResult(rec, req, repo, services.store.System, identity, store.DefaultTenantID, cfg, code, invitee, nil, userReferralIdempotencyKeyHash(store.DefaultTenantID, "reward-failure-success-key"), "reward-failure-fingerprint", services.store.FailureLogs)
	if rec.Code != http.StatusCreated {
		t.Fatalf("registration status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Registered   bool   `json:"registered"`
		RewardStatus string `json:"reward_status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil || !payload.Registered || payload.RewardStatus != "reward_failed" {
		t.Fatalf("unexpected successful registration payload=%s err=%v", rec.Body.String(), err)
	}
	referral, err := repo.GetReferralForInvitee(ctx, store.DefaultTenantID, invitee.ID)
	if err != nil || referral == nil || referral.Status != "reward_failed" {
		t.Fatalf("referral status=%#v err=%v", referral, err)
	}
	logs, total, err := services.store.FailureLogs.List(ctx, store.FailureEventLogFilter{TenantID: store.DefaultTenantID, TenantScoped: true, Category: "user_referral", Limit: 20})
	if err != nil || total != 1 || len(logs) != 1 {
		t.Fatalf("reward failure log count logs=%#v total=%d err=%v", logs, total, err)
	}
	log := logs[0]
	if log.EventCode != "reward_failed" || log.EntityID != referral.ID || log.Email != "" || log.ClientIP != "" || strings.Contains(log.DetailsJSON, inviter.Email) || strings.Contains(log.DetailsJSON, invitee.Email) {
		t.Fatalf("reward failure log must be tenant-scoped and PII-free: %#v", log)
	}
	replayRequest := httptest.NewRequest(http.MethodPost, "/invite/test/register", nil)
	replayRequest.Header.Set("Idempotency-Key", "reward-failure-success-key")
	_, replay, err := beginPersistedUserReferralIdempotency(ctx, repo, store.DefaultTenantID, replayRequest, "reward-failure-fingerprint")
	if err != nil || replay == nil || replay.status != http.StatusCreated || !strings.Contains(string(replay.payload), `"reward_status":"reward_failed"`) {
		t.Fatalf("reward failure success should be idempotent: replay=%#v err=%v", replay, err)
	}
}

func TestReconcileUserReferralRewardsRecoversTenantScopedFailures(t *testing.T) {
	services := newAdminRouterTestContext(t)
	ctx := context.Background()
	now := time.Now().UTC().Add(-time.Minute)
	otherTenant := &store.Tenant{ID: "tenant-referral-recovery-other", Slug: "referral-recovery-other", Name: "Referral Recovery Other", Status: "active", CreatedAt: now, UpdatedAt: now}
	if err := services.store.Tenants.Create(ctx, otherTenant); err != nil {
		t.Fatalf("create other tenant: %v", err)
	}
	users := []*store.User{
		{ID: "recovery-inviter", TenantID: store.DefaultTenantID, Email: "recovery-inviter@example.com", SN: "SN-recovery-inviter", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
		{ID: "recovery-invitee", TenantID: store.DefaultTenantID, Email: "recovery-invitee@example.com", SN: "SN-recovery-invitee", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
		{ID: "recovery-other-inviter", TenantID: otherTenant.ID, Email: "recovery-other-inviter@example.com", SN: "SN-recovery-other-inviter", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
		{ID: "recovery-other-invitee", TenantID: otherTenant.ID, Email: "recovery-other-invitee@example.com", SN: "SN-recovery-other-invitee", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
	}
	for _, user := range users {
		if err := services.store.Users.Create(ctx, user); err != nil {
			t.Fatalf("create user %s: %v", user.ID, err)
		}
	}
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	for _, referral := range []*store.UserReferral{
		{ID: "recovery-default-failed", TenantID: store.DefaultTenantID, ReferralCodeID: "recovery-code", InviterUserID: users[0].ID, InviteeUserID: users[1].ID, Status: "reward_failed", RegisteredAt: now, ServiceGroupID: "recovery-group", InviterCredits: 40, InviteeCredits: 20, DurationDays: 30, CreatedAt: now, UpdatedAt: now},
		{ID: "recovery-other-failed", TenantID: otherTenant.ID, ReferralCodeID: "recovery-other-code", InviterUserID: users[2].ID, InviteeUserID: users[3].ID, Status: "reward_failed", RegisteredAt: now, ServiceGroupID: "recovery-group", InviterCredits: 30, InviteeCredits: 10, DurationDays: 30, CreatedAt: now, UpdatedAt: now},
	} {
		if err := repo.CreateReferral(ctx, referral); err != nil {
			t.Fatalf("create referral %s: %v", referral.ID, err)
		}
	}
	for _, tenantID := range []string{store.DefaultTenantID, otherTenant.ID} {
		if err := llmservice.SaveRegistry(ctx, ScopedSystemSettingsForTenant(tenantID, services.store.System), &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "recovery-group", Name: "Recovery"}}}); err != nil {
			t.Fatalf("save registry for %s: %v", tenantID, err)
		}
	}
	identity := auth.NewIdentityService(services.store.Users, nil, nil, nil, nil, nil, services.store.System, nil, "open", true, nil, "")
	result, err := ReconcileUserReferralRewards(ctx, identity, repo, services.store.System, services.store.Tenants, services.store.FailureLogs)
	if err != nil || result.Scanned != 2 || result.Rewarded != 2 || result.Failed != 0 {
		t.Fatalf("recovery result=%#v err=%v", result, err)
	}
	for _, tc := range []struct {
		tenantID   string
		referralID string
		grants     int
	}{
		{store.DefaultTenantID, "recovery-default-failed", 2},
		{otherTenant.ID, "recovery-other-failed", 2},
	} {
		referral, err := repo.GetReferralByID(ctx, tc.tenantID, tc.referralID)
		if err != nil || referral == nil || referral.Status != "rewarded" || referral.InviterGrantID == "" || referral.InviteeGrantID == "" {
			t.Fatalf("recovered referral %s=%#v err=%v", tc.referralID, referral, err)
		}
		registry, err := llmservice.LoadRegistry(ctx, ScopedSystemSettingsForTenant(tc.tenantID, services.store.System))
		if err != nil || registry == nil {
			t.Fatalf("load recovered registry %s: %#v err=%v", tc.tenantID, registry, err)
		}
		count := 0
		for _, grant := range registry.Grants {
			if grant.Source == "user_referral" && grant.CardID == tc.referralID {
				count++
			}
		}
		if count != tc.grants {
			t.Fatalf("recovered grants for %s=%d want %d", tc.referralID, count, tc.grants)
		}
	}
	// A second startup pass must be a no-op: completed grants are never replayed.
	second, err := ReconcileUserReferralRewards(ctx, identity, repo, services.store.System, services.store.Tenants, services.store.FailureLogs)
	if err != nil || second.Scanned != 0 || second.Rewarded != 0 || second.Failed != 0 {
		t.Fatalf("second recovery must be idempotent: result=%#v err=%v", second, err)
	}
}

func TestReconcileUserReferralRewardsKeepsFailureRetryableAndPIIFree(t *testing.T) {
	services := newAdminRouterTestContext(t)
	ctx := context.Background()
	now := time.Now().UTC()
	inviter := &store.User{ID: "recovery-fail-inviter", TenantID: store.DefaultTenantID, Email: "recovery-fail-inviter@example.com", SN: "SN-recovery-fail-inviter", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	invitee := &store.User{ID: "recovery-fail-invitee", TenantID: store.DefaultTenantID, Email: "recovery-fail-invitee@example.com", SN: "SN-recovery-fail-invitee", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	for _, user := range []*store.User{inviter, invitee} {
		if err := services.store.Users.Create(ctx, user); err != nil {
			t.Fatal(err)
		}
	}
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	referral := &store.UserReferral{ID: "recovery-still-failed", TenantID: store.DefaultTenantID, ReferralCodeID: "recovery-fail-code", InviterUserID: inviter.ID, InviteeUserID: invitee.ID, Status: "attributed", RegisteredAt: now, ServiceGroupID: "missing-recovery-group", InviterCredits: 10, InviteeCredits: 5, DurationDays: 30, CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateReferral(ctx, referral); err != nil {
		t.Fatal(err)
	}
	identity := auth.NewIdentityService(services.store.Users, nil, nil, nil, nil, nil, services.store.System, nil, "open", true, nil, "")
	result, err := ReconcileUserReferralRewards(ctx, identity, repo, services.store.System, services.store.Tenants, services.store.FailureLogs)
	if err != nil || result.Scanned != 1 || result.Rewarded != 0 || result.Failed != 1 {
		t.Fatalf("recovery result=%#v err=%v", result, err)
	}
	updated, err := repo.GetReferralByID(ctx, store.DefaultTenantID, referral.ID)
	if err != nil || updated == nil || updated.Status != "reward_failed" {
		t.Fatalf("failed recovery status=%#v err=%v", updated, err)
	}
	logs, total, err := services.store.FailureLogs.List(ctx, store.FailureEventLogFilter{TenantID: store.DefaultTenantID, TenantScoped: true, Category: "user_referral", Limit: 20})
	if err != nil || total != 1 || len(logs) != 1 || logs[0].EntityID != referral.ID || logs[0].Email != "" || logs[0].ClientIP != "" || strings.Contains(logs[0].DetailsJSON, inviter.Email) || strings.Contains(logs[0].DetailsJSON, invitee.Email) {
		t.Fatalf("recovery failure log must be PII-free: logs=%#v total=%d err=%v", logs, total, err)
	}
}

func TestReferralRegistrationReservesRewardAfterDailyCap(t *testing.T) {
	services := newAdminRouterTestContext(t)
	ctx := context.Background()
	now := time.Now().UTC()
	inviter := &store.User{ID: "daily-cap-inviter", TenantID: store.DefaultTenantID, Email: "daily-cap-inviter@example.com", SN: "SN-daily-cap-inviter", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	priorInvitee := &store.User{ID: "daily-cap-prior", TenantID: store.DefaultTenantID, Email: "daily-cap-prior@example.com", SN: "SN-daily-cap-prior", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	otherInviter := &store.User{ID: "daily-cap-other-inviter", TenantID: store.DefaultTenantID, Email: "daily-cap-other-inviter@example.com", SN: "SN-daily-cap-other-inviter", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	otherInvitee := &store.User{ID: "daily-cap-other-invitee", TenantID: store.DefaultTenantID, Email: "daily-cap-other-invitee@example.com", SN: "SN-daily-cap-other-invitee", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	invitee := &store.User{ID: "daily-cap-invitee", TenantID: store.DefaultTenantID, Email: "daily-cap-invitee@example.com", SN: "SN-daily-cap-invitee", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	for _, user := range []*store.User{inviter, priorInvitee, otherInviter, otherInvitee, invitee} {
		if err := services.store.Users.Create(ctx, user); err != nil {
			t.Fatalf("create user: %v", err)
		}
	}
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	if err := repo.CreateReferral(ctx, &store.UserReferral{ID: "daily-cap-prior-referral", TenantID: store.DefaultTenantID, ReferralCodeID: "old-code", InviterUserID: inviter.ID, InviteeUserID: priorInvitee.ID, Status: "rewarded", RegisteredAt: now, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create prior rewarded referral: %v", err)
	}
	if err := repo.CreateReferral(ctx, &store.UserReferral{ID: "daily-cap-other-referral", TenantID: store.DefaultTenantID, ReferralCodeID: "other-code", InviterUserID: otherInviter.ID, InviteeUserID: otherInvitee.ID, Status: "rewarded", RegisteredAt: now, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create other inviter referral: %v", err)
	}
	identity := auth.NewIdentityService(services.store.Users, nil, nil, nil, nil, nil, services.store.System, nil, "open", true, nil, "")
	code := &store.UserReferralCode{ID: "daily-cap-code", TenantID: store.DefaultTenantID, InviterUserID: inviter.ID, CodeHash: "daily-cap-code-hash", Status: "active", CreatedAt: now}
	cfg := UserReferralConfig{Enabled: true, InviterCredits: 10, InviteeCredits: 5, DurationDays: 30, DailyRewardCap: 1, ServiceGroupID: "missing-service-group", Downloads: defaultUserReferralDownloads()}
	req := httptest.NewRequest(http.MethodPost, "/invite/test/register", nil)
	req.Header.Set("Idempotency-Key", "daily-cap-key")
	rec := httptest.NewRecorder()
	writeReferralRegistrationResult(rec, req, repo, services.store.System, identity, store.DefaultTenantID, cfg, code, invitee, nil, userReferralIdempotencyKeyHash(store.DefaultTenantID, "daily-cap-key"), "daily-cap-fingerprint")
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"reward_status":"reserved"`) {
		t.Fatalf("daily cap registration response=%d %s", rec.Code, rec.Body.String())
	}
	referral, err := repo.GetReferralForInvitee(ctx, store.DefaultTenantID, invitee.ID)
	if err != nil || referral == nil || referral.Status != "reserved" {
		t.Fatalf("daily cap referral=%#v err=%v", referral, err)
	}
	history, err := repo.ListStatusHistory(ctx, store.DefaultTenantID, referral.ID)
	if err != nil || len(history) != 1 || history[0].ToStatus != "reserved" || !strings.Contains(history[0].Reason, "daily reward cap") {
		t.Fatalf("daily cap history=%#v err=%v", history, err)
	}
}

func TestReferralRegistrationDoesNotRewardAtomicallyReservedReferral(t *testing.T) {
	services := newAdminRouterTestContext(t)
	ctx := context.Background()
	now := time.Now().UTC()
	inviter := &store.User{ID: "atomic-reserved-inviter", TenantID: store.DefaultTenantID, Email: "atomic-reserved-inviter@example.com", SN: "SN-atomic-reserved-inviter", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	invitee := &store.User{ID: "atomic-reserved-invitee", TenantID: store.DefaultTenantID, Email: "atomic-reserved-invitee@example.com", SN: "SN-atomic-reserved-invitee", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	for _, user := range []*store.User{inviter, invitee} {
		if err := services.store.Users.Create(ctx, user); err != nil {
			t.Fatal(err)
		}
	}
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	identity := auth.NewIdentityService(services.store.Users, nil, nil, nil, nil, nil, services.store.System, nil, "open", true, nil, "")
	code := &store.UserReferralCode{ID: "atomic-reserved-code", TenantID: store.DefaultTenantID, InviterUserID: inviter.ID, CodeHash: "atomic-reserved-hash", Status: "active", CreatedAt: now}
	referral := &store.UserReferral{ID: "atomic-reserved-referral", TenantID: store.DefaultTenantID, ReferralCodeID: code.ID, InviterUserID: inviter.ID, InviteeUserID: invitee.ID, Status: "reserved", RegisteredAt: now, ServiceGroupID: "coding", InviterCredits: 10, InviteeCredits: 5, DurationDays: 30, CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateReferral(ctx, referral); err != nil {
		t.Fatal(err)
	}
	cfg := UserReferralConfig{Enabled: true, InviterCredits: 10, InviteeCredits: 5, DurationDays: 30, ServiceGroupID: "coding", Downloads: defaultUserReferralDownloads()}
	req := httptest.NewRequest(http.MethodPost, "/invite/test/register", nil)
	req.Header.Set("Idempotency-Key", "atomic-reserved-key")
	rec := httptest.NewRecorder()
	writeReferralRegistrationResult(rec, req, repo, services.store.System, identity, store.DefaultTenantID, cfg, code, invitee, referral, userReferralIdempotencyKeyHash(store.DefaultTenantID, "atomic-reserved-key"), "atomic-reserved-fingerprint")
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"reward_status":"reserved"`) {
		t.Fatalf("reserved registration response=%d %s", rec.Code, rec.Body.String())
	}
	stored, err := repo.GetReferralByID(ctx, store.DefaultTenantID, referral.ID)
	if err != nil || stored == nil || stored.Status != "reserved" || stored.InviterGrantID != "" || stored.InviteeGrantID != "" {
		t.Fatalf("reserved referral must not receive automatic grants: %#v err=%v", stored, err)
	}
}

func TestUserReferralInviterSummaryUsesIssuedGrantLedger(t *testing.T) {
	services := newAdminRouterTestContext(t)
	ctx := context.Background()
	now := time.Now().UTC()
	inviter := &store.User{ID: "summary-inviter", TenantID: store.DefaultTenantID, Email: "summary-inviter@example.com", SN: "SN-summary-inviter", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	reservedInvitee := &store.User{ID: "summary-reserved", TenantID: store.DefaultTenantID, Email: "summary-reserved@example.com", SN: "SN-summary-reserved", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	rewardedInvitee := &store.User{ID: "summary-rewarded", TenantID: store.DefaultTenantID, Email: "summary-rewarded@example.com", SN: "SN-summary-rewarded", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	for _, user := range []*store.User{inviter, reservedInvitee, rewardedInvitee} {
		if err := services.store.Users.Create(ctx, user); err != nil {
			t.Fatalf("create user: %v", err)
		}
	}
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	if err := repo.CreateReferral(ctx, &store.UserReferral{ID: "summary-reserved-referral", TenantID: store.DefaultTenantID, ReferralCodeID: "summary-code", InviterUserID: inviter.ID, InviteeUserID: reservedInvitee.ID, Status: "reserved", RegisteredAt: now, InviterCredits: 100, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create reserved referral: %v", err)
	}
	if err := repo.CreateReferral(ctx, &store.UserReferral{ID: "summary-rewarded-referral", TenantID: store.DefaultTenantID, ReferralCodeID: "summary-code", InviterUserID: inviter.ID, InviteeUserID: rewardedInvitee.ID, Status: "rewarded", RegisteredAt: now.Add(time.Second), InviterCredits: 50, InviterGrantID: "summary-rewarded-grant", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create rewarded referral: %v", err)
	}
	if err := llmservice.SaveRegistry(ctx, services.store.System, &llmservice.Registry{Grants: []llmservice.Grant{{ID: "summary-rewarded-grant", UserID: inviter.ID, Email: inviter.Email, ServiceGroupID: "coding", Source: "user_referral", CardID: "summary-rewarded-referral", StartsAt: now, ExpiresAt: now.Add(30 * 24 * time.Hour), CreditsTotal: 50, CreditsUsed: 12}}}); err != nil {
		t.Fatalf("save grant registry: %v", err)
	}
	h := ListUserReferralInvitersHandler(repo, services.store.System)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/user-referrals?page=1", nil)
	req = req.WithContext(auth.WithTenant(req.Context(), store.DefaultTenantID))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("summary status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		Inviters []struct {
			InviteeCount     int     `json:"invitee_count"`
			CreditsGranted   float64 `json:"credits_granted"`
			CreditsConsumed  float64 `json:"credits_consumed"`
			CreditsAvailable float64 `json:"credits_available"`
			CreditsExpired   float64 `json:"credits_expired"`
		} `json:"inviters"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil || len(response.Inviters) != 1 {
		t.Fatalf("summary payload=%s err=%v", rec.Body.String(), err)
	}
	got := response.Inviters[0]
	if got.InviteeCount != 1 || got.CreditsGranted != 50 || got.CreditsConsumed != 12 || got.CreditsAvailable != 38 || got.CreditsExpired != 0 {
		t.Fatalf("summary=%#v; reserved referral must not count as a successful invitation or granted Credits", got)
	}
}

func TestUserReferralGrantLedgerSeparatesAvailableAndExpiredCredits(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	registry := &llmservice.Registry{Grants: []llmservice.Grant{
		{ID: "active", CreditsTotal: 10, CreditsUsed: 3, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)},
		{ID: "expired", CreditsTotal: 8, CreditsUsed: 2, StartsAt: now.Add(-48 * time.Hour), ExpiresAt: now.Add(-time.Hour)},
		{ID: "frozen", CreditsTotal: 6, CreditsUsed: 1, Frozen: true, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)},
	}}
	ledger := userReferralGrantLedgerSnapshot(registry, []string{"active", "expired", "frozen"}, now)
	if ledger.Granted != 24 || ledger.Consumed != 6 || ledger.Available != 7 || ledger.Expired != 6 {
		t.Fatalf("ledger=%#v", ledger)
	}
}

func TestUserReferralInviteeDetailUsesLiveGrantStates(t *testing.T) {
	services := newAdminRouterTestContext(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	inviter := &store.User{ID: "detail-state-inviter", TenantID: store.DefaultTenantID, Email: "detail-state-inviter@example.test", SN: "SN-detail-state-inviter", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	invitee := &store.User{ID: "detail-state-invitee", TenantID: store.DefaultTenantID, Email: "detail-state-invitee@example.test", SN: "SN-detail-state-invitee", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	for _, user := range []*store.User{inviter, invitee} {
		if err := services.store.Users.Create(ctx, user); err != nil {
			t.Fatalf("create user %s: %v", user.ID, err)
		}
	}
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	referral := &store.UserReferral{ID: "detail-state-referral", TenantID: store.DefaultTenantID, ReferralCodeID: "detail-state-code", InviterUserID: inviter.ID, InviteeUserID: invitee.ID, Status: "rewarded", RegisteredAt: now, ServiceGroupID: "detail-state-group", InviterCredits: 10, InviteeCredits: 5, DurationDays: 30, InviterGrantID: "detail-state-active", InviteeGrantID: "detail-state-expired", CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateReferral(ctx, referral); err != nil {
		t.Fatalf("create referral: %v", err)
	}
	registry := &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "detail-state-group"}}, Grants: []llmservice.Grant{
		{ID: referral.InviterGrantID, UserID: inviter.ID, Email: inviter.Email, ServiceGroupID: referral.ServiceGroupID, Source: "user_referral", CardID: referral.ID, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), CreatedAt: now.Add(-time.Hour), CreditsTotal: 10},
		{ID: referral.InviteeGrantID, UserID: invitee.ID, Email: invitee.Email, ServiceGroupID: referral.ServiceGroupID, Source: "user_referral", CardID: referral.ID, StartsAt: now.Add(-48 * time.Hour), ExpiresAt: now.Add(-time.Hour), CreatedAt: now.Add(-48 * time.Hour), CreditsTotal: 5},
	}}
	if err := llmservice.SaveRegistry(ctx, services.store.System, registry); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	admin := &store.AdminUser{ID: "detail-state-admin", Scope: "tenant", TenantID: store.DefaultTenantID}
	req := httptest.NewRequest(http.MethodGet, "/api/admin/user-referrals/detail?page=1", nil)
	req.SetPathValue("inviter_id", inviter.ID)
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, admin))
	rec := httptest.NewRecorder()
	ListUserReferralInviteesHandler(repo, services.store.System).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"inviter_grant_state":"active"`) || !strings.Contains(rec.Body.String(), `"invitee_grant_state":"expired"`) {
		t.Fatalf("missing live grant states: %s", rec.Body.String())
	}
}

func TestUserReferralInviterActivityIncludesRetryableRewardFailureOnly(t *testing.T) {
	services := newAdminRouterTestContext(t)
	ctx := context.Background()
	now := time.Now().UTC()
	inviter := &store.User{ID: "activity-inviter", TenantID: store.DefaultTenantID, Email: "activity-inviter@example.com", SN: "SN-activity-inviter", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	invitees := []*store.User{
		{ID: "activity-rewarded", TenantID: store.DefaultTenantID, Email: "activity-rewarded@example.com", SN: "SN-activity-rewarded", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
		{ID: "activity-failed", TenantID: store.DefaultTenantID, Email: "activity-failed@example.com", SN: "SN-activity-failed", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
		{ID: "activity-reserved", TenantID: store.DefaultTenantID, Email: "activity-reserved@example.com", SN: "SN-activity-reserved", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
		{ID: "activity-revoked", TenantID: store.DefaultTenantID, Email: "activity-revoked@example.com", SN: "SN-activity-revoked", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
	}
	if err := services.store.Users.Create(ctx, inviter); err != nil {
		t.Fatal(err)
	}
	for _, invitee := range invitees {
		if err := services.store.Users.Create(ctx, invitee); err != nil {
			t.Fatal(err)
		}
	}
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	for i, status := range []string{"rewarded", "reward_failed", "reserved", "revoked"} {
		if err := repo.CreateReferral(ctx, &store.UserReferral{ID: "activity-referral-" + status, TenantID: store.DefaultTenantID, ReferralCodeID: "activity-code", InviterUserID: inviter.ID, InviteeUserID: invitees[i].ID, Status: status, RegisteredAt: now.Add(time.Duration(i) * time.Second), CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	items, total, err := repo.ListInviterSummaries(ctx, store.UserReferralFilter{TenantID: store.DefaultTenantID, Limit: 20})
	if err != nil || total != 1 || len(items) != 1 || items[0].InviteeCount != 2 {
		t.Fatalf("successful inviter activity items=%#v total=%d err=%v", items, total, err)
	}
	inviteeItems, inviteeTotal, err := repo.ListInvitees(ctx, store.UserReferralFilter{TenantID: store.DefaultTenantID, InviterUserID: inviter.ID, Limit: 20})
	if err != nil || inviteeTotal != 2 || len(inviteeItems) != 2 || inviteeItems[0].Status != "reward_failed" || inviteeItems[1].Status != "rewarded" {
		t.Fatalf("successful invitee activity items=%#v total=%d err=%v", inviteeItems, inviteeTotal, err)
	}
	reviewItems, reviewTotal, err := repo.ListReferralInviteesForReview(ctx, store.UserReferralFilter{TenantID: store.DefaultTenantID, InviterUserID: inviter.ID, Limit: 20})
	if err != nil || reviewTotal != 4 || len(reviewItems) != 4 {
		t.Fatalf("admin invitee review items=%#v total=%d err=%v", reviewItems, reviewTotal, err)
	}
}

func TestListReservedUserReferralsHandlerIsTenantScopedAndStable(t *testing.T) {
	services := newAdminRouterTestContext(t)
	ctx := context.Background()
	now := time.Now().UTC().Add(-time.Minute)
	otherTenant := &store.Tenant{ID: "tenant-referral-review-other", Slug: "referral-review-other", Name: "Referral Review Other", Status: "active", CreatedAt: now, UpdatedAt: now}
	if err := services.store.Tenants.Create(ctx, otherTenant); err != nil {
		t.Fatal(err)
	}
	users := []*store.User{
		{ID: "review-inviter-a", TenantID: store.DefaultTenantID, Email: "review-inviter-a@example.com", SN: "SN-review-inviter-a", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
		{ID: "review-invitee-a", TenantID: store.DefaultTenantID, Email: "review-invitee-a@example.com", SN: "SN-review-invitee-a", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
		{ID: "review-inviter-b", TenantID: store.DefaultTenantID, Email: "review-inviter-b@example.com", SN: "SN-review-inviter-b", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
		{ID: "review-invitee-b", TenantID: store.DefaultTenantID, Email: "review-invitee-b@example.com", SN: "SN-review-invitee-b", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
		{ID: "review-other-inviter", TenantID: otherTenant.ID, Email: "review-other-inviter@example.com", SN: "SN-review-other-inviter", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
		{ID: "review-other-invitee", TenantID: otherTenant.ID, Email: "review-other-invitee@example.com", SN: "SN-review-other-invitee", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
		{ID: "review-invitee-rewarded", TenantID: store.DefaultTenantID, Email: "review-invitee-rewarded@example.com", SN: "SN-review-invitee-rewarded", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now},
	}
	for _, user := range users {
		if err := services.store.Users.Create(ctx, user); err != nil {
			t.Fatal(err)
		}
	}
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	for _, referral := range []*store.UserReferral{
		{ID: "review-a", TenantID: store.DefaultTenantID, ReferralCodeID: "review-code", InviterUserID: users[0].ID, InviteeUserID: users[1].ID, Status: "reserved", RegisteredAt: now, CreatedAt: now, UpdatedAt: now},
		{ID: "review-b", TenantID: store.DefaultTenantID, ReferralCodeID: "review-code", InviterUserID: users[2].ID, InviteeUserID: users[3].ID, Status: "reserved", RegisteredAt: now.Add(time.Second), CreatedAt: now, UpdatedAt: now},
		{ID: "review-rewarded", TenantID: store.DefaultTenantID, ReferralCodeID: "review-code", InviterUserID: users[0].ID, InviteeUserID: users[6].ID, Status: "rewarded", RegisteredAt: now.Add(2 * time.Second), CreatedAt: now, UpdatedAt: now},
		{ID: "review-other", TenantID: otherTenant.ID, ReferralCodeID: "review-other-code", InviterUserID: users[4].ID, InviteeUserID: users[5].ID, Status: "reserved", RegisteredAt: now, CreatedAt: now, UpdatedAt: now},
	} {
		if err := repo.CreateReferral(ctx, referral); err != nil {
			t.Fatalf("create referral %s: %v", referral.ID, err)
		}
	}
	if err := repo.CreateStatusHistory(ctx, &store.UserReferralStatusHistory{ID: "review-a-history", TenantID: store.DefaultTenantID, ReferralID: "review-a", ToStatus: "reserved", Reason: "network/client daily registration review threshold reached; awaiting review", ActorUserID: "system", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	h := ListReservedUserReferralsHandler(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/user-referrals/review-queue?page=1", nil)
	req = req.WithContext(WithRequestTenant(req.Context(), store.DefaultTenantID))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("review queue status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		Referrals []struct {
			ReferralID   string `json:"referral_id"`
			ReviewReason string `json:"review_reason"`
		} `json:"referrals"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil || response.Total != 2 || len(response.Referrals) != 2 || response.Referrals[0].ReferralID != "review-a" || response.Referrals[1].ReferralID != "review-b" || !strings.Contains(response.Referrals[0].ReviewReason, "network/client") {
		t.Fatalf("review queue=%s parsed=%#v err=%v", rec.Body.String(), response, err)
	}
}

func TestModerateUserReferralRevocationFreezesBenefitsBeforeTerminalStatus(t *testing.T) {
	services := newAdminRouterTestContext(t)
	ctx := context.Background()
	now := time.Now().UTC()
	inviter := &store.User{ID: "revoke-inviter", TenantID: store.DefaultTenantID, Email: "revoke-inviter@example.com", SN: "SN-revoke-inviter", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	invitee := &store.User{ID: "revoke-invitee", TenantID: store.DefaultTenantID, Email: "revoke-invitee@example.com", SN: "SN-revoke-invitee", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	for _, user := range []*store.User{inviter, invitee} {
		if err := services.store.Users.Create(ctx, user); err != nil {
			t.Fatalf("create user: %v", err)
		}
	}
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	referral := &store.UserReferral{ID: "revoke-referral", TenantID: store.DefaultTenantID, ReferralCodeID: "revoke-code", InviterUserID: inviter.ID, InviteeUserID: invitee.ID, Status: "rewarded", RegisteredAt: now, InviterCredits: 10, InviteeCredits: 5, CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateReferral(ctx, referral); err != nil {
		t.Fatalf("create referral: %v", err)
	}
	if err := llmservice.SaveRegistry(ctx, services.store.System, &llmservice.Registry{Grants: []llmservice.Grant{
		{ID: "revoke-inviter-grant", UserID: inviter.ID, Email: inviter.Email, ServiceGroupID: "coding", Source: "user_referral", CardID: referral.ID, StartsAt: now, ExpiresAt: now.Add(30 * 24 * time.Hour), CreditsTotal: 10},
		{ID: "revoke-invitee-grant", UserID: invitee.ID, Email: invitee.Email, ServiceGroupID: "coding", Source: "user_referral", CardID: referral.ID, StartsAt: now, ExpiresAt: now.Add(30 * 24 * time.Hour), CreditsTotal: 5},
	}}); err != nil {
		t.Fatalf("save grants: %v", err)
	}
	identity := auth.NewIdentityService(services.store.Users, nil, nil, nil, nil, nil, services.store.System, nil, "open", true, nil, "")
	h := ModerateUserReferralHandler(identity, repo, services.store.System, services.store.AdminAudit)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/user-referrals/"+referral.ID+"/revoke", strings.NewReader(`{"reason":"confirmed duplicate"}`))
	req.SetPathValue("referral_id", referral.ID)
	req.SetPathValue("action", "revoke")
	req = req.WithContext(WithRequestTenant(req.Context(), store.DefaultTenantID))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated, err := repo.GetReferralByID(ctx, store.DefaultTenantID, referral.ID)
	if err != nil || updated == nil || updated.Status != "revoked" {
		t.Fatalf("referral after revoke=%#v err=%v", updated, err)
	}
	registry, err := llmservice.LoadRegistry(ctx, services.store.System)
	if err != nil || registry == nil {
		t.Fatalf("load grants: %#v err=%v", registry, err)
	}
	for _, grant := range registry.Grants {
		if grant.CardID == referral.ID && !grant.Frozen {
			t.Fatalf("referral grant remained spendable after revocation: %#v", grant)
		}
	}
}

func TestModerateUserReferralApprovalReturnsRewardedFinalStatus(t *testing.T) {
	services := newAdminRouterTestContext(t)
	ctx := context.Background()
	now := time.Now().UTC()
	inviter := &store.User{ID: "approve-inviter", TenantID: store.DefaultTenantID, Email: "approve-inviter@example.com", SN: "SN-approve-inviter", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	invitee := &store.User{ID: "approve-invitee", TenantID: store.DefaultTenantID, Email: "approve-invitee@example.com", SN: "SN-approve-invitee", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	for _, user := range []*store.User{inviter, invitee} {
		if err := services.store.Users.Create(ctx, user); err != nil {
			t.Fatal(err)
		}
	}
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	referral := &store.UserReferral{ID: "approve-referral", TenantID: store.DefaultTenantID, ReferralCodeID: "approve-code", InviterUserID: inviter.ID, InviteeUserID: invitee.ID, Status: "reserved", RegisteredAt: now, ServiceGroupID: "approve-group", InviterCredits: 10, InviteeCredits: 5, DurationDays: 30, CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateReferral(ctx, referral); err != nil {
		t.Fatal(err)
	}
	if err := llmservice.SaveRegistry(ctx, services.store.System, &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "approve-group"}}}); err != nil {
		t.Fatal(err)
	}
	identity := auth.NewIdentityService(services.store.Users, nil, nil, nil, nil, nil, services.store.System, nil, "open", true, nil, "")
	h := ModerateUserReferralHandler(identity, repo, services.store.System, services.store.AdminAudit)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/user-referrals/approve-referral/approve", strings.NewReader(`{"reason":"reviewed"}`))
	req.SetPathValue("referral_id", referral.ID)
	req.SetPathValue("action", "approve")
	req = req.WithContext(WithRequestTenant(req.Context(), store.DefaultTenantID))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"rewarded"`) {
		t.Fatalf("approve response=%d %s", rec.Code, rec.Body.String())
	}
	stored, err := repo.GetReferralByID(ctx, store.DefaultTenantID, referral.ID)
	if err != nil || stored == nil || stored.Status != "rewarded" {
		t.Fatalf("approved referral=%#v err=%v", stored, err)
	}
}

func TestUserReferralPublicRateLimit(t *testing.T) {
	userReferralRateLimits.Lock()
	userReferralRateLimits.entries = map[string]userReferralRateCounter{}
	userReferralRateLimits.Unlock()
	request := httptest.NewRequest(http.MethodGet, "/invite/rf_test", nil)
	request.RemoteAddr = "203.0.113.10:4321"
	for i := 0; i < 2; i++ {
		if !allowUserReferralPublicRequest(request, "test", 2) {
			t.Fatalf("request %d should be accepted", i)
		}
	}
	if allowUserReferralPublicRequest(request, "test", 2) {
		t.Fatal("third request must be rate limited")
	}
	otherIP := httptest.NewRequest(http.MethodGet, "/invite/rf_test", nil)
	otherIP.RemoteAddr = "203.0.113.11:4321"
	if !allowUserReferralPublicRequest(otherIP, "test", 2) {
		t.Fatal("a different client IP must have an independent limit")
	}
}

func TestUserReferralNetworkClientRiskReservesOnlyAfterDailyThreshold(t *testing.T) {
	userReferralNetworkClientRisk.Lock()
	userReferralNetworkClientRisk.entries = map[string]userReferralDailyRiskCounter{}
	userReferralNetworkClientRisk.Unlock()
	now := time.Date(2026, time.August, 13, 9, 30, 0, 0, time.UTC)
	request := httptest.NewRequest(http.MethodPost, "/invite/rf_test/register", nil)
	request.RemoteAddr = "203.0.113.99:40001"
	request.Header.Set("User-Agent", "MaClaw-Test/1.0")
	const reviewCap = 3
	for i := 0; i < reviewCap; i++ {
		if userReferralNeedsNetworkClientReview(store.DefaultTenantID, request, reviewCap, now) {
			t.Fatalf("registration %d must remain automatically rewardable", i+1)
		}
	}
	if !userReferralNeedsNetworkClientReview(store.DefaultTenantID, request, reviewCap, now) {
		t.Fatal("registration above daily shared network/client threshold must be reserved for review")
	}
	otherTenant := store.DefaultTenantID + "_other"
	if userReferralNeedsNetworkClientReview(otherTenant, request, reviewCap, now) {
		t.Fatal("risk signal must be tenant-isolated")
	}
	otherClient := httptest.NewRequest(http.MethodPost, "/invite/rf_test/register", nil)
	otherClient.RemoteAddr = request.RemoteAddr
	otherClient.Header.Set("User-Agent", "MaClaw-Test/2.0")
	if userReferralNeedsNetworkClientReview(store.DefaultTenantID, otherClient, reviewCap, now) {
		t.Fatal("a distinct client signature must not inherit another signature's review count")
	}
	if userReferralNeedsNetworkClientReview(store.DefaultTenantID, request, reviewCap, now.Add(24*time.Hour)) {
		t.Fatal("daily risk counter must reset on the next UTC day")
	}
}

func TestUserReferralConfigDefaultsAndClampsNetworkClientReviewCap(t *testing.T) {
	if got := defaultUserReferralConfig().DailyNetworkClientReviewCap; got != 3 {
		t.Fatalf("default network/client review cap=%d, want 3", got)
	}
	if got := normalizeUserReferralConfig(UserReferralConfig{DailyNetworkClientReviewCap: -1}).DailyNetworkClientReviewCap; got != 3 {
		t.Fatalf("negative network/client review cap=%d, want default 3", got)
	}
	if got := normalizeUserReferralConfig(UserReferralConfig{DailyNetworkClientReviewCap: 100001}).DailyNetworkClientReviewCap; got != 100000 {
		t.Fatalf("network/client review cap=%d, want 100000", got)
	}
}

func TestUserReferralRegistrationSessionIsBoundAndHttpOnly(t *testing.T) {
	services := newAdminRouterTestContext(t)
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	ctx := context.Background()
	landing := httptest.NewRequest(http.MethodGet, "/invite/rf_test", nil)
	landing.Header.Set("User-Agent", "test-agent")
	landing.TLS = &tls.ConnectionState{}
	landingRecorder := httptest.NewRecorder()
	if err := newUserReferralRegistrationSession(ctx, repo, landingRecorder, landing, "tenant-a", "code-hash", "epoch-a"); err != nil {
		t.Fatal(err)
	}
	response := landingRecorder.Result()
	if len(response.Cookies()) != 1 || !response.Cookies()[0].HttpOnly || !response.Cookies()[0].Secure || response.Cookies()[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("unexpected registration session cookie: %#v", response.Cookies())
	}
	registration := httptest.NewRequest(http.MethodPost, "/invite/rf_test/register", nil)
	registration.Header.Set("User-Agent", "test-agent")
	registration.AddCookie(response.Cookies()[0])
	if !verifyUserReferralRegistrationSession(ctx, repo, httptest.NewRecorder(), registration, "tenant-a", "code-hash", "epoch-a") {
		t.Fatal("matching referral registration session should validate")
	}
	wrongTenant := httptest.NewRequest(http.MethodPost, "/invite/rf_test/register", nil)
	wrongTenant.Header.Set("User-Agent", "test-agent")
	wrongTenant.AddCookie(response.Cookies()[0])
	if verifyUserReferralRegistrationSession(ctx, repo, httptest.NewRecorder(), wrongTenant, "tenant-b", "code-hash", "epoch-a") {
		t.Fatal("registration session must not cross a tenant boundary")
	}
}

func TestUserReferralRegistrationSessionSurvivesRepositoryRestart(t *testing.T) {
	services := newAdminRouterTestContext(t)
	ctx := context.Background()
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	landing := httptest.NewRequest(http.MethodGet, "/invite/rf_test", nil)
	landing.Header.Set("User-Agent", "restart-test-agent")
	landingRecorder := httptest.NewRecorder()
	if err := newUserReferralRegistrationSession(ctx, repo, landingRecorder, landing, "tenant-a", "code-hash", "epoch-a"); err != nil {
		t.Fatal(err)
	}
	response := landingRecorder.Result()
	if len(response.Cookies()) != 1 {
		t.Fatalf("session cookie count=%d", len(response.Cookies()))
	}
	registration := httptest.NewRequest(http.MethodPost, "/invite/rf_test/register", nil)
	registration.Header.Set("User-Agent", "restart-test-agent")
	registration.AddCookie(response.Cookies()[0])
	// A new repository instance emulates a different Hub process after restart.
	freshRepo := storesqlite.NewUserReferralRepository(services.db, services.db)
	if !verifyUserReferralRegistrationSession(ctx, freshRepo, httptest.NewRecorder(), registration, "tenant-a", "code-hash", "epoch-a") {
		t.Fatal("persisted session should remain valid after process restart")
	}
	wrongAgent := httptest.NewRequest(http.MethodPost, "/invite/rf_test/register", nil)
	wrongAgent.Header.Set("User-Agent", "other-agent")
	wrongAgent.AddCookie(response.Cookies()[0])
	if verifyUserReferralRegistrationSession(ctx, freshRepo, httptest.NewRecorder(), wrongAgent, "tenant-a", "code-hash", "epoch-a") {
		t.Fatal("persisted session must remain bound to its browser user agent")
	}
}

func TestUserReferralRegistrationSessionEpochInvalidatesAndClearsCookie(t *testing.T) {
	services := newAdminRouterTestContext(t)
	ctx := context.Background()
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	landing := httptest.NewRequest(http.MethodGet, "/invite/rf_test", nil)
	landing.Header.Set("User-Agent", "epoch-test-agent")
	landingRecorder := httptest.NewRecorder()
	if err := newUserReferralRegistrationSession(ctx, repo, landingRecorder, landing, "tenant-a", "code-hash", "enabled-epoch-1"); err != nil {
		t.Fatal(err)
	}
	registration := httptest.NewRequest(http.MethodPost, "/invite/rf_test/register", nil)
	registration.Header.Set("User-Agent", "epoch-test-agent")
	registration.AddCookie(landingRecorder.Result().Cookies()[0])
	rec := httptest.NewRecorder()
	if verifyUserReferralRegistrationSession(ctx, repo, rec, registration, "tenant-a", "code-hash", "enabled-epoch-2") {
		t.Fatal("session minted before disable/enable must not revive")
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != userReferralRegistrationCookie || cookies[0].MaxAge >= 0 {
		t.Fatalf("expected session clearing cookie, got %#v", cookies)
	}
}

func TestUserReferralHandoffClaimIsSingleUseAndCreatesDesktopSession(t *testing.T) {
	services := newAdminRouterTestContext(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := services.store.Users.Create(ctx, &store.User{ID: "referrer-handoff", TenantID: store.DefaultTenantID, Email: "referrer-handoff@example.com", SN: "SN-handoff", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create inviter: %v", err)
	}
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	plain := "rf_test_handoff_0123456789"
	encrypted, err := llmservice.EncryptCardCode(plain)
	if err != nil {
		t.Fatalf("encrypt referral code: %v", err)
	}
	codeHash := userReferralCodeHash(store.DefaultTenantID, plain)
	if err := repo.CreateCode(ctx, &store.UserReferralCode{ID: "ref-code-handoff", TenantID: store.DefaultTenantID, InviterUserID: "referrer-handoff", CodeHash: codeHash, EncryptedCode: encrypted, Status: "active", CreatedAt: now}); err != nil {
		t.Fatalf("create referral code: %v", err)
	}
	cfg := UserReferralConfig{Enabled: true, DurationDays: 30, Downloads: defaultUserReferralDownloads(), SessionEpoch: "handoff-epoch"}
	raw, _ := json.Marshal(cfg)
	if err := ScopedSystemSettingsForTenant(store.DefaultTenantID, services.store.System).Set(ctx, userReferralSettingsKey, string(raw)); err != nil {
		t.Fatalf("save config: %v", err)
	}

	landing := httptest.NewRequest(http.MethodGet, "/invite/"+plain, nil)
	landing.Header.Set("User-Agent", "handoff-browser")
	landingRecorder := httptest.NewRecorder()
	if err := newUserReferralRegistrationSession(ctx, repo, landingRecorder, landing, store.DefaultTenantID, codeHash, cfg.SessionEpoch); err != nil {
		t.Fatalf("new browser session: %v", err)
	}
	cookie := landingRecorder.Result().Cookies()[0]

	handoffReq := httptest.NewRequest(http.MethodPost, "/invite/"+plain+"/handoff", nil)
	handoffReq.Header.Set("User-Agent", "handoff-browser")
	handoffReq.Host = "hub.example.test"
	handoffReq.AddCookie(cookie)
	handoffRec := httptest.NewRecorder()
	services.handler.ServeHTTP(handoffRec, handoffReq)
	if handoffRec.Code != http.StatusCreated {
		t.Fatalf("handoff status=%d body=%s", handoffRec.Code, handoffRec.Body.String())
	}
	var handoffPayload struct {
		Handoff string `json:"handoff"`
	}
	if err := json.Unmarshal(handoffRec.Body.Bytes(), &handoffPayload); err != nil {
		t.Fatalf("decode handoff: %v", err)
	}
	token, _, ok := strings.Cut(handoffPayload.Handoff, "?hub_url=")
	if !ok || len(token) < 16 {
		t.Fatalf("unsafe handoff payload=%q", handoffPayload.Handoff)
	}

	claimBody, _ := json.Marshal(map[string]string{"handoff": token})
	claimReq := httptest.NewRequest(http.MethodPost, "/api/public/referral-handoffs/claim", strings.NewReader(string(claimBody)))
	claimReq.Header.Set("Content-Type", "application/json")
	claimReq.Header.Set("User-Agent", "handoff-browser")
	if stored, lookupErr := repo.GetHandoff(ctx, userReferralHandoffTokenHash(token), time.Now().UTC()); lookupErr != nil || stored == nil {
		t.Fatalf("handoff lookup before claim=%#v err=%v", stored, lookupErr)
	}
	claimRec := httptest.NewRecorder()
	claimIdentity := auth.NewIdentityService(services.store.Users, services.store.Enrollments, services.store.EmailBlocks, services.store.Machines, services.store.ViewerTokens, services.store.LoginTokens, services.store.System, nil, "open", true, nil, "")
	PublicUserReferralHandoffClaimHandler(claimIdentity, repo, services.store.System, services.store.Tenants).ServeHTTP(claimRec, claimReq)
	if claimRec.Code != http.StatusOK {
		t.Fatalf("claim status=%d body=%s", claimRec.Code, claimRec.Body.String())
	}
	var claimPayload struct {
		RegistrationSession string `json:"registration_session"`
		Tenant              struct {
			ID string `json:"id"`
		} `json:"tenant"`
	}
	if err := json.Unmarshal(claimRec.Body.Bytes(), &claimPayload); err != nil {
		t.Fatalf("decode claim: %v", err)
	}
	if claimPayload.RegistrationSession == "" || claimPayload.Tenant.ID != store.DefaultTenantID {
		t.Fatalf("invalid claim %#v", claimPayload)
	}

	desktopReq := httptest.NewRequest(http.MethodPost, "/api/public/referral-registration/register", nil)
	desktopReq.Header.Set(userReferralRegistrationHeader, claimPayload.RegistrationSession)
	desktopReq.Header.Set(userReferralRegistrationTenantHeader, store.DefaultTenantID)
	if tenantID, _, _, found, err := resolvePublicReferralRequest(ctx, desktopReq, "", repo, services.store.System, services.store.Tenants, services.store.Users); err != nil || tenantID != store.DefaultTenantID || found == nil || found.ID != "ref-code-handoff" {
		t.Fatalf("desktop session resolve tenant=%q code=%#v err=%v", tenantID, found, err)
	}

	replayReq := httptest.NewRequest(http.MethodPost, "/api/public/referral-handoffs/claim", strings.NewReader(string(claimBody)))
	replayReq.Header.Set("Content-Type", "application/json")
	replayReq.Header.Set("User-Agent", "handoff-browser")
	replayRec := httptest.NewRecorder()
	PublicUserReferralHandoffClaimHandler(claimIdentity, repo, services.store.System, services.store.Tenants).ServeHTTP(replayRec, replayReq)
	if replayRec.Code != http.StatusGone {
		t.Fatalf("replay status=%d body=%s", replayRec.Code, replayRec.Body.String())
	}
}

func TestUserReferralIdentityReservationBindsIdentityToOneSessionAndCode(t *testing.T) {
	services := newAdminRouterTestContext(t)
	ctx := context.Background()
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	makeSession := func(codeHash, agent string) (*http.Request, string) {
		landing := httptest.NewRequest(http.MethodGet, "/invite/rf_test", nil)
		landing.Header.Set("User-Agent", agent)
		landingRecorder := httptest.NewRecorder()
		if err := newUserReferralRegistrationSession(ctx, repo, landingRecorder, landing, "tenant-a", codeHash, "epoch-a"); err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "/invite/rf_test/register", nil)
		request.Header.Set("User-Agent", agent)
		request.AddCookie(landingRecorder.Result().Cookies()[0])
		return request, landingRecorder.Result().Cookies()[0].Value
	}
	first, _ := makeSession("code-a", "reservation-agent-a")
	identityHash, reserved, err := reserveUserReferralIdentity(ctx, repo, first, "tenant-a", "code-a", "email", "New.User@example.com")
	if err != nil || !reserved {
		t.Fatalf("first reservation identity=%q reserved=%v err=%v", identityHash, reserved, err)
	}
	if !verifyUserReferralIdentityReservation(ctx, repo, first, "tenant-a", "code-a", identityHash) {
		t.Fatal("owner session and code must be able to complete its reservation")
	}
	second, _ := makeSession("code-b", "reservation-agent-b")
	if _, reserved, err := reserveUserReferralIdentity(ctx, repo, second, "tenant-a", "code-b", "email", "new.user@example.com"); err != nil || reserved {
		t.Fatalf("competing invitation reserved=%v err=%v", reserved, err)
	}
	if verifyUserReferralIdentityReservation(ctx, repo, second, "tenant-a", "code-b", identityHash) {
		t.Fatal("competing referral must not complete another invitation's reservation")
	}
	if _, reserved, err := reserveUserReferralIdentity(ctx, repo, first, "tenant-a", "code-a", "email", "new.user@example.com"); err != nil || !reserved {
		t.Fatalf("same session refresh reserved=%v err=%v", reserved, err)
	}
}

func TestUserReferralIdentityHashNormalizesUnicodeEmailBeforeReservation(t *testing.T) {
	composed := "caf\u00e9@example.com"
	decomposed := "cafe\u0301@example.com"
	first := userReferralIdentityHash("tenant-a", "email", composed)
	second := userReferralIdentityHash("tenant-a", "email", decomposed)
	if first == "" || first != second {
		t.Fatalf("NFC-equivalent referral identity hashes differ: %q != %q", first, second)
	}
}

func TestUserReferralPhoneIdentityNormalizesChinaNationalAndE164Forms(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  string
	}{
		{value: "13800138000", want: "+8613800138000"},
		{value: "+86 138-0013-8000", want: "+8613800138000"},
		{value: "0086 13800138000", want: "+8613800138000"},
		{value: "+1 (415) 555-2671", want: "+14155552671"},
	} {
		if got := userReferralNormalizedIdentityValue("phone", tc.value); got != tc.want {
			t.Fatalf("normalize phone %q = %q, want %q", tc.value, got, tc.want)
		}
	}
	// Referral reservation and idempotency use canonical E.164; country-code
	// aliases must not create distinct invitation attributions.
	if a, b := userReferralIdentityHash("tenant-a", "phone", "13800138000"), userReferralIdentityHash("tenant-a", "phone", "+86 138 0013 8000"); a == "" || a != b {
		t.Fatalf("national and E.164 phone hashes differ: %q != %q", a, b)
	}
}

func TestUserReferralE164ReservationBlocksCountryCodeAliasesAndHonorsLegacyReservation(t *testing.T) {
	services := newAdminRouterTestContext(t)
	ctx := context.Background()
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	makeRequest := func(session, agent string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/api/public/referral-registration/phone/send-code", nil)
		req.Header.Set(userReferralRegistrationHeader, session)
		req.Header.Set(userReferralRegistrationTenantHeader, store.DefaultTenantID)
		req.Header.Set("User-Agent", agent)
		return req
	}
	first := makeRequest("e164-first", "e164-agent-a")
	if err := repo.SaveRegistrationSession(ctx, &store.UserReferralRegistrationSession{TenantID: store.DefaultTenantID, TokenHash: userReferralRegistrationSessionTokenHash(store.DefaultTenantID, "e164-first"), CodeHash: "e164-code-a", ConfigEpoch: "e164-epoch", UserAgentHash: referralUserAgentHash(first), ExpiresAt: time.Now().UTC().Add(time.Hour), CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("save first session: %v", err)
	}
	canonicalHash, reserved, err := reserveUserReferralIdentity(ctx, repo, first, store.DefaultTenantID, "e164-code-a", "phone", "13800138000")
	if err != nil || !reserved {
		t.Fatalf("reserve canonical phone hash=%q reserved=%v err=%v", canonicalHash, reserved, err)
	}
	second := makeRequest("e164-second", "e164-agent-b")
	if err := repo.SaveRegistrationSession(ctx, &store.UserReferralRegistrationSession{TenantID: store.DefaultTenantID, TokenHash: userReferralRegistrationSessionTokenHash(store.DefaultTenantID, "e164-second"), CodeHash: "e164-code-b", ConfigEpoch: "e164-epoch", UserAgentHash: referralUserAgentHash(second), ExpiresAt: time.Now().UTC().Add(time.Hour), CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("save second session: %v", err)
	}
	if _, reserved, err := reserveUserReferralIdentity(ctx, repo, second, store.DefaultTenantID, "e164-code-b", "phone", "+86 13800138000"); err != nil || reserved {
		t.Fatalf("country-code alias reserved=%v err=%v", reserved, err)
	}
	legacyValue := "13900139000"
	legacyHash := userReferralLegacyPhoneIdentityHash(store.DefaultTenantID, legacyValue)
	if _, err := repo.ReserveIdentity(ctx, &store.UserReferralIdentityReservation{TenantID: store.DefaultTenantID, IdentityHash: legacyHash, CodeHash: "legacy-code", SessionHash: userReferralRegistrationSessionTokenHash(store.DefaultTenantID, "e164-first"), ReservedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour)}, time.Now().UTC()); err != nil {
		t.Fatalf("seed legacy reservation: %v", err)
	}
	if !verifyUserReferralIdentityReservationForValue(ctx, repo, first, store.DefaultTenantID, "legacy-code", "phone", "+86 13900139000") {
		t.Fatal("legacy digits-only reservation must remain completable by its owner session")
	}
}

func TestUserReferralAdminPaginationRejectsExplicitInvalidPagesAndOversizedQuery(t *testing.T) {
	for _, target := range []string{
		"/api/admin/user-referrals?page=0",
		"/api/admin/user-referrals?page=not-a-page",
		"/api/admin/user-referrals/review-queue?page=-1",
	} {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		recorder := httptest.NewRecorder()
		if strings.Contains(target, "review-queue") {
			ListReservedUserReferralsHandler(nil).ServeHTTP(recorder, request)
		} else {
			ListUserReferralInvitersHandler(nil, nil).ServeHTTP(recorder, request)
		}
		if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "INVALID_PAGINATION") {
			t.Fatalf("target=%s status=%d body=%s", target, recorder.Code, recorder.Body.String())
		}
	}

	oversized := httptest.NewRequest(http.MethodGet, "/api/admin/user-referrals?page=1&query="+strings.Repeat("a", 129), nil)
	oversizedRecorder := httptest.NewRecorder()
	ListUserReferralInvitersHandler(nil, nil).ServeHTTP(oversizedRecorder, oversized)
	if oversizedRecorder.Code != http.StatusBadRequest || !strings.Contains(oversizedRecorder.Body.String(), "INVALID_QUERY") {
		t.Fatalf("oversized query status=%d body=%s", oversizedRecorder.Code, oversizedRecorder.Body.String())
	}
}

func TestReferralRecommendedDownload(t *testing.T) {
	downloads := defaultUserReferralDownloads()
	for _, tc := range []struct {
		userAgent string
		label     string
		url       string
	}{
		{userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64)", label: "Windows x64", url: downloads.WindowsAMD64},
		{userAgent: "Mozilla/5.0 (Macintosh; Mac OS X 14_0; arm64)", label: "macOS Apple Silicon", url: downloads.MacOSARM64},
		{userAgent: "Mozilla/5.0 (X11; Linux aarch64)", label: "Linux ARM64", url: downloads.LinuxARM64},
		{userAgent: "Mozilla/5.0 (compatible; crawler)", label: "", url: ""},
	} {
		r := httptest.NewRequest(http.MethodGet, "/invite/test", nil)
		r.Header.Set("User-Agent", tc.userAgent)
		if label, url := referralRecommendedDownload(r, downloads); label != tc.label || url != tc.url {
			t.Fatalf("UA %q recommendation=(%q,%q), want=(%q,%q)", tc.userAgent, label, url, tc.label, tc.url)
		}
	}
}

func TestUserReferralStatusHistoryIsTenantScopedAndOrdered(t *testing.T) {
	services := newAdminRouterTestContext(t)
	ctx := context.Background()
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	now := time.Now().UTC().Add(-time.Minute)
	for _, item := range []*store.UserReferralStatusHistory{
		{ID: "history-older", TenantID: store.DefaultTenantID, ReferralID: "referral-history", FromStatus: "attributed", ToStatus: "reward_failed", Reason: "provider unavailable", ActorUserID: "system", CreatedAt: now},
		{ID: "history-newer", TenantID: store.DefaultTenantID, ReferralID: "referral-history", FromStatus: "reward_failed", ToStatus: "rewarded", ActorUserID: "admin-1", CreatedAt: now.Add(time.Second)},
		{ID: "history-other-tenant", TenantID: "tenant_other", ReferralID: "referral-history", FromStatus: "attributed", ToStatus: "revoked", Reason: "other tenant", CreatedAt: now.Add(2 * time.Second)},
	} {
		if err := repo.CreateStatusHistory(ctx, item); err != nil {
			t.Fatalf("create status history: %v", err)
		}
	}
	items, err := repo.ListStatusHistory(ctx, store.DefaultTenantID, "referral-history")
	if err != nil || len(items) != 2 {
		t.Fatalf("history items=%#v err=%v", items, err)
	}
	if items[0].ID != "history-newer" || items[1].Reason != "provider unavailable" || items[0].ActorUserID != "admin-1" {
		t.Fatalf("unexpected ordered history: %#v", items)
	}
}

func TestResolvePublicReferralRejectsInactiveInviter(t *testing.T) {
	services := newAdminRouterTestContext(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := services.store.Users.Create(ctx, &store.User{ID: "inactive-referrer", TenantID: store.DefaultTenantID, Email: "inactive-referrer@example.com", SN: "SN-inactive", Status: "disabled", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	repo := storesqlite.NewUserReferralRepository(services.db, services.db)
	plain := "rf_inactive_referrer_123456"
	encrypted, err := llmservice.EncryptCardCode(plain)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateCode(ctx, &store.UserReferralCode{ID: "inactive-referrer-code", TenantID: store.DefaultTenantID, InviterUserID: "inactive-referrer", CodeHash: userReferralCodeHash(store.DefaultTenantID, plain), EncryptedCode: encrypted, Status: "active", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	cfg := UserReferralConfig{Enabled: true, DurationDays: 30, Downloads: defaultUserReferralDownloads()}
	raw, _ := json.Marshal(cfg)
	if err := ScopedSystemSettingsForTenant(store.DefaultTenantID, services.store.System).Set(ctx, userReferralSettingsKey, string(raw)); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/invite/"+plain, nil)
	rec := httptest.NewRecorder()
	services.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "INVITATION_UNAVAILABLE") {
		t.Fatalf("inactive inviter landing status=%d body=%s", rec.Code, rec.Body.String())
	}
}
