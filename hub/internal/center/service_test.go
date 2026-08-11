package center

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/hub/internal/config"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type fakeSettingsRepo struct {
	mu     sync.Mutex
	values map[string]string
}

type fakeCenterUsers struct {
	items []*store.User
}

func (f fakeCenterUsers) ListUsers(context.Context) ([]*store.User, error) {
	return f.items, nil
}

type fakeCenterMachines struct {
	items []device.MachineRuntimeInfo
}

func (f fakeCenterMachines) ListAllMachines(context.Context) ([]device.MachineRuntimeInfo, error) {
	return f.items, nil
}

type fakeCenterTenants struct {
	items []*store.Tenant
}

func (f fakeCenterTenants) List(context.Context) ([]*store.Tenant, error) {
	return f.items, nil
}

type fakeCenterUsage struct {
	tokenRows    []store.UserTokenSummary
	durationRows []store.UserDurationSummary
}

func (f fakeCenterUsage) SummarizeUserTokenUsage(context.Context, string, time.Time, time.Time) ([]store.UserTokenSummary, error) {
	return append([]store.UserTokenSummary(nil), f.tokenRows...), nil
}

func (f fakeCenterUsage) SummarizeUserDurations(context.Context, string, time.Time, time.Time, time.Time) ([]store.UserDurationSummary, error) {
	return append([]store.UserDurationSummary(nil), f.durationRows...), nil
}
func newFakeSettingsRepo() *fakeSettingsRepo {
	return &fakeSettingsRepo{values: map[string]string{}}
}

func (r *fakeSettingsRepo) Set(ctx context.Context, key, valueJSON string) error {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values[key] = valueJSON
	return nil
}

func (r *fakeSettingsRepo) Get(ctx context.Context, key string) (string, error) {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.values[key], nil
}

func TestSyncUserRouteUsesStoredRegistration(t *testing.T) {
	var (
		gotPath   string
		gotSecret string
		gotEmail  string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		gotSecret, _ = payload["hub_secret"].(string)
		gotEmail, _ = payload["email"].(string)
		if tenantID, _ := payload["tenant_id"].(string); tenantID != "tenant_acme" {
			t.Fatalf("tenant_id = %q, want tenant_acme", tenantID)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Center.BaseURL = server.URL
	cfg.Center.Enabled = true
	cfg.Server.PublicBaseURL = "https://hub.example.com"

	settings := newFakeSettingsRepo()
	svc := NewService(cfg, settings)
	err := settings.Set(context.Background(), systemKeyCenterRegistration, mustJSON(registrationRecord{
		Registered: true,
		HubID:      "hub_sync",
		HubSecret:  "secret_sync",
	}))
	if err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	if err := svc.SyncUserRoute(context.Background(), "User@Example.com", "tenant_acme"); err != nil {
		t.Fatalf("SyncUserRoute() error = %v", err)
	}
	if gotPath != "/api/hubs/hub_sync/user-links/sync" {
		t.Fatalf("sync path = %q, want %q", gotPath, "/api/hubs/hub_sync/user-links/sync")
	}
	if gotSecret != "secret_sync" {
		t.Fatalf("hub_secret = %q, want %q", gotSecret, "secret_sync")
	}
	if gotEmail != "user@example.com" {
		t.Fatalf("email = %q, want %q", gotEmail, "user@example.com")
	}
}

func TestSyncUserRouteReportsMissingCenterRegistration(t *testing.T) {
	cfg := config.Default()
	cfg.Center.Enabled = true
	cfg.Center.BaseURL = "https://hubs.example.com"
	svc := NewService(cfg, newFakeSettingsRepo())

	err := svc.SyncUserRouteReplaceAll(context.Background(), "phone:19900001111", store.DefaultTenantID)
	if err == nil || !strings.Contains(err.Error(), "hub center registration is missing or incomplete") {
		t.Fatalf("SyncUserRouteReplaceAll() error = %v, want missing registration", err)
	}
}

func TestUserUsageSyncStartDayUsesLimitedBackfill(t *testing.T) {
	svc := NewService(config.Default(), newFakeSettingsRepo())
	now := time.Date(2026, 6, 24, 15, 30, 0, 0, time.UTC)

	start, backfill := svc.reserveUserUsageSyncStartDay(now)
	if !backfill || !start.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("first start = %s backfill=%v, want 2026-01-01 backfill", start, backfill)
	}
	svc.restoreUserUsageBackfill()
	start, backfill = svc.reserveUserUsageSyncStartDay(now)
	if !backfill || !start.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("failed sync should not consume backfill, start = %s backfill=%v", start, backfill)
	}
	start, backfill = svc.reserveUserUsageSyncStartDay(now)
	if !backfill || !start.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("second success start = %s backfill=%v, want 2026-01-01 backfill", start, backfill)
	}

	start, backfill = svc.reserveUserUsageSyncStartDay(now)
	wantRecent := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)
	if backfill || !start.Equal(wantRecent) {
		t.Fatalf("recent start = %s backfill=%v, want %s without backfill", start, backfill, wantRecent)
	}

	end := userUsageSyncEndDayExclusive(now)
	wantEnd := time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)
	if !end.Equal(wantEnd) {
		t.Fatalf("end day exclusive = %s, want %s", end, wantEnd)
	}

	tenantIDs := centerSyncTenantIDs([]string{"tenant_default", "", "tenant_a", "tenant_a"})
	if len(tenantIDs) != 2 || tenantIDs[0] != "" || tenantIDs[1] != "tenant_a" {
		t.Fatalf("tenant ids = %#v, want default marker and tenant_a", tenantIDs)
	}
}

func TestSyncUserUsageAllowsEmailAndPhonePayloadRows(t *testing.T) {
	var got syncUserUsageRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/hubs/hub_sync/user-usage/sync" {
			t.Fatalf("path = %q, want user usage sync path", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	svc := NewService(config.Default(), newFakeSettingsRepo())
	svc.usageBackfills = 0
	svc.SetStatsProviders(nil, nil, fakeCenterUsage{
		tokenRows: []store.UserTokenSummary{
			{UserEmail: "u_1774182684297100200", Usage: store.UserTokenUsage{InputTokens: 99}},
			{UserEmail: "User@Example.com", Usage: store.UserTokenUsage{InputTokens: 10, OutputTokens: 5}},
			{UserEmail: "phone:19900001111", Usage: store.UserTokenUsage{InputTokens: 20}},
		},
		durationRows: []store.UserDurationSummary{
			{UserEmail: "u_1774182684297100200", DurationSeconds: 999},
			{UserEmail: "User@Example.com", DurationSeconds: 120},
			{UserEmail: "phone:19900001111", DurationSeconds: 240},
		},
	})

	if err := svc.syncUserUsage(context.Background(), server.URL, registrationRecord{HubID: "hub_sync", HubSecret: "secret_sync"}); err != nil {
		t.Fatalf("syncUserUsage() error = %v", err)
	}
	if got.HubSecret != "secret_sync" {
		t.Fatalf("hub_secret = %q, want secret_sync", got.HubSecret)
	}
	if got.SyncStartDay == "" || got.SyncEndDay == "" || len(got.TenantIDs) != 1 || got.TenantIDs[0] != "" {
		t.Fatalf("unexpected sync window or tenants: start=%q end=%q tenants=%#v", got.SyncStartDay, got.SyncEndDay, got.TenantIDs)
	}
	if len(got.Items) == 0 {
		t.Fatal("expected usage items")
	}
	seen := map[string]bool{}
	for _, item := range got.Items {
		switch item.UserEmail {
		case "user@example.com":
			if item.InputTokens != 10 || item.OutputTokens != 5 || item.DurationSeconds != 120 {
				t.Fatalf("unexpected email usage item: %#v", item)
			}
			seen[item.UserEmail] = true
		case "phone:19900001111":
			if item.InputTokens != 20 || item.DurationSeconds != 240 {
				t.Fatalf("unexpected phone usage item: %#v", item)
			}
			seen[item.UserEmail] = true
		case "u_1774182684297100200":
			t.Fatalf("uid account should be filtered: %#v", got.Items)
		default:
			t.Fatalf("unexpected account in payload: %#v", item)
		}
	}
	if !seen["user@example.com"] || !seen["phone:19900001111"] {
		t.Fatalf("expected email and phone accounts, saw %#v", seen)
	}
}

func TestDeleteUserRouteUsesStoredRegistration(t *testing.T) {
	var (
		gotMethod string
		gotPath   string
		gotSecret string
		gotEmail  string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		gotSecret, _ = payload["hub_secret"].(string)
		gotEmail, _ = payload["email"].(string)
		if tenantID, _ := payload["tenant_id"].(string); tenantID != "tenant_acme" {
			t.Fatalf("tenant_id = %q, want tenant_acme", tenantID)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Center.BaseURL = server.URL
	cfg.Center.Enabled = true
	cfg.Server.PublicBaseURL = "https://hub.example.com"

	settings := newFakeSettingsRepo()
	svc := NewService(cfg, settings)
	err := settings.Set(context.Background(), systemKeyCenterRegistration, mustJSON(registrationRecord{
		Registered: true,
		HubID:      "hub_sync",
		HubSecret:  "secret_sync",
	}))
	if err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	if err := svc.DeleteUserRoute(context.Background(), "User@Example.com", "tenant_acme"); err != nil {
		t.Fatalf("DeleteUserRoute() error = %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Fatalf("method = %q, want DELETE", gotMethod)
	}
	if gotPath != "/api/hubs/hub_sync/user-links/sync" {
		t.Fatalf("sync path = %q, want %q", gotPath, "/api/hubs/hub_sync/user-links/sync")
	}
	if gotSecret != "secret_sync" {
		t.Fatalf("hub_secret = %q, want %q", gotSecret, "secret_sync")
	}
	if gotEmail != "user@example.com" {
		t.Fatalf("email = %q, want %q", gotEmail, "user@example.com")
	}
}

func TestAllowsUserRouteRejectsEmailRoutedToDifferentHub(t *testing.T) {
	var gotEmail string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/entry/resolve-domain" {
			t.Fatalf("path = %q, want /api/entry/resolve-domain", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		gotEmail, _ = payload["email"].(string)
		if domain, _ := payload["domain"].(string); domain != "qianxin.com" {
			t.Fatalf("domain = %q, want qianxin.com", domain)
		}
		if tenantID, _ := payload["tenant_id"].(string); tenantID != "tenant_acme" {
			t.Fatalf("tenant_id = %q, want tenant_acme", tenantID)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"default_hub_id":"hub_target"}`))
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Center.BaseURL = server.URL
	cfg.Center.Enabled = true
	settings := newFakeSettingsRepo()
	svc := NewService(cfg, settings)
	if err := settings.Set(context.Background(), systemKeyCenterRegistration, mustJSON(registrationRecord{Registered: true, HubID: "hub_current", HubSecret: "secret"})); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	allowed, targetHubID, err := svc.AllowsUserRoute(context.Background(), "User@Qianxin.com", "tenant_acme")
	if err != nil {
		t.Fatalf("AllowsUserRoute() error = %v", err)
	}
	if allowed || targetHubID != "hub_target" {
		t.Fatalf("allowed=%t target=%q, want false hub_target", allowed, targetHubID)
	}
	if gotEmail != "user@qianxin.com" {
		t.Fatalf("email = %q, want user@qianxin.com", gotEmail)
	}
}

func TestAllowsUserRouteAllowsCurrentOrUnroutedHub(t *testing.T) {
	responses := []string{`{"default_hub_id":"hub_current"}`, `{}`}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := responses[0]
		responses = responses[1:]
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Center.BaseURL = server.URL
	cfg.Center.Enabled = true
	settings := newFakeSettingsRepo()
	svc := NewService(cfg, settings)
	if err := settings.Set(context.Background(), systemKeyCenterRegistration, mustJSON(registrationRecord{Registered: true, HubID: "hub_current", HubSecret: "secret"})); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	for i := 0; i < 2; i++ {
		allowed, _, err := svc.AllowsUserRoute(context.Background(), "user@example.com")
		if err != nil || !allowed {
			t.Fatalf("AllowsUserRoute #%d allowed=%t err=%v", i, allowed, err)
		}
	}
}

func TestRegistrationCapabilitiesIncludesTenantCounts(t *testing.T) {
	svc := NewService(config.Default(), newFakeSettingsRepo())
	svc.users = fakeCenterUsers{items: []*store.User{
		{Email: "alice@Acme.Example", TenantID: "tenant_a"},
		{Email: "bob@acme.example", TenantID: "tenant_a"},
		{Email: "cara@beta.example", TenantID: "tenant_b"},
	}}
	svc.machines = fakeCenterMachines{items: []device.MachineRuntimeInfo{
		{MachineID: "machine-a1", TenantID: "tenant_a"},
		{MachineID: "machine-a2", TenantID: "tenant_a"},
		{MachineID: "machine-b1", TenantID: "tenant_b"},
	}}
	svc.tenants = fakeCenterTenants{items: []*store.Tenant{
		{ID: "tenant_a", Name: "开发部", Status: "active", PrimaryDomain: "configured.example", SettingsJSON: `{"email_domains":["configured.example","team.configured.example","https://bad.example.com","bad..example.com"]}`},
		{ID: "tenant_inactive", Status: "inactive", PrimaryDomain: "inactive.example"},
		{ID: "tenant_deleted", Status: "deleted", PrimaryDomain: "deleted.example", DeletedAt: ptrTime(time.Now())},
	}}

	caps := svc.registrationCapabilities(context.Background())
	tenantUserCounts, ok := caps["tenant_user_counts"].(map[string]int)
	if !ok || tenantUserCounts["tenant_a"] != 2 || tenantUserCounts["tenant_b"] != 1 {
		t.Fatalf("tenant_user_counts = %#v", caps["tenant_user_counts"])
	}
	tenantMachineCounts, ok := caps["tenant_machine_counts"].(map[string]int)
	if !ok || tenantMachineCounts["tenant_a"] != 2 || tenantMachineCounts["tenant_b"] != 1 {
		t.Fatalf("tenant_machine_counts = %#v", caps["tenant_machine_counts"])
	}
	tenantDomains, ok := caps["tenant_domains"].(map[string][]string)
	if !ok || containsString(tenantDomains["tenant_a"], "acme.example") || !containsString(tenantDomains["tenant_a"], "configured.example") || !containsString(tenantDomains["tenant_a"], "team.configured.example") || len(tenantDomains["tenant_b"]) != 0 || containsString(tenantDomains["tenant_deleted"], "deleted.example") || containsString(tenantDomains["tenant_inactive"], "inactive.example") || containsString(tenantDomains["tenant_a"], "https://bad.example.com") || containsString(tenantDomains["tenant_a"], "bad..example.com") {
		t.Fatalf("tenant_domains = %#v", caps["tenant_domains"])
	}
	if caps["tenant_domain_source"] != "configured" {
		t.Fatalf("tenant_domain_source = %#v", caps["tenant_domain_source"])
	}
	tenantNames, ok := caps["tenant_names"].(map[string]string)
	if !ok || tenantNames["tenant_a"] != "开发部" || tenantNames["tenant_inactive"] != "" || tenantNames["tenant_deleted"] != "" {
		t.Fatalf("tenant_names = %#v", caps["tenant_names"])
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func ptrTime(v time.Time) *time.Time { return &v }

func TestAdvertisedEndpointPrefersConfiguredPublicBaseURL(t *testing.T) {
	cfg := config.Default()
	cfg.Server.PublicBaseURL = "https://hub.example.com"
	cfg.Server.ListenPort = 9399

	svc := NewService(cfg, newFakeSettingsRepo())
	baseURL, host, port, err := svc.advertisedEndpoint()
	if err != nil {
		t.Fatalf("advertisedEndpoint() error = %v", err)
	}
	if baseURL != "https://hub.example.com" {
		t.Fatalf("baseURL = %q, want %q", baseURL, "https://hub.example.com")
	}
	if host != "hub.example.com" {
		t.Fatalf("host = %q, want %q", host, "hub.example.com")
	}
	if port != 443 {
		t.Fatalf("port = %d, want %d", port, 443)
	}
}

func TestStatusIncludesAdminAndStartupFlags(t *testing.T) {
	cfg := config.Default()
	cfg.Center.Enabled = true
	cfg.Center.RegisterOnStartup = true
	cfg.Center.BaseURL = "https://hubs.mypapers.top"
	cfg.Server.PublicBaseURL = "https://hub.example.com"

	settings := newFakeSettingsRepo()
	_ = settings.Set(context.Background(), systemKeyAdminEmail, mustJSON(map[string]string{"value": "admin@example.com"}))

	svc := NewService(cfg, settings)
	status, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !status.RegisterOnStartup {
		t.Fatalf("RegisterOnStartup = false, want true")
	}
	if !status.AdminEmailPresent {
		t.Fatalf("AdminEmailPresent = false, want true")
	}
	if status.Host != "hub.example.com" {
		t.Fatalf("Host = %q, want %q", status.Host, "hub.example.com")
	}
}

func TestSendHeartbeatLockedDisablesDigitalEmployeeAuthorization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusLocked)
		_, _ = w.Write([]byte(`{"code":"HUB_DISABLED","message":"Hub has been disabled by Hub Center"}`))
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Center.BaseURL = server.URL
	cfg.Center.Enabled = true
	cfg.Server.PublicBaseURL = "https://hub.example.com"

	settings := newFakeSettingsRepo()
	expiresAt := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	if err := settings.Set(context.Background(), systemKeyCenterRegistration, mustJSON(registrationRecord{
		Registered: true,
		HubID:      "hub_disabled",
		HubSecret:  "secret_disabled",
		DigitalEmployeeAuthorization: &corelib.DigitalEmployeeAuthorization{
			Quota:     3,
			Enabled:   true,
			ExpiresAt: expiresAt,
			Active:    true,
		},
	})); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	svc := NewService(cfg, settings)
	if err := svc.sendHeartbeat(context.Background()); err != nil {
		t.Fatalf("SendHeartbeat() error = %v", err)
	}
	auth := LoadDigitalEmployeeAuthorization(context.Background(), settings)
	if auth == nil {
		t.Fatal("expected disabled digital employee authorization")
	}
	if auth.Active || auth.Enabled || auth.Reason != "disabled" {
		t.Fatalf("auth = %+v, want inactive disabled", *auth)
	}
}

func TestStatusForDisabledRecordForcesDigitalEmployeeAuthorizationInactive(t *testing.T) {
	cfg := config.Default()
	cfg.Center.Enabled = true
	cfg.Server.PublicBaseURL = "https://hub.example.com"

	settings := newFakeSettingsRepo()
	expiresAt := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	if err := settings.Set(context.Background(), systemKeyCenterRegistration, mustJSON(registrationRecord{
		Registered: true,
		Disabled:   true,
		HubID:      "hub_disabled",
		DigitalEmployeeAuthorization: &corelib.DigitalEmployeeAuthorization{
			Quota:     3,
			Enabled:   true,
			ExpiresAt: expiresAt,
			Active:    true,
		},
	})); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	status, err := NewService(cfg, settings).Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	auth := status.DigitalEmployeeAuthorization
	if auth == nil || auth.Active || auth.Enabled || auth.Reason != "disabled" || auth.Quota != 3 {
		t.Fatalf("status auth = %+v, want inactive disabled while preserving quota", auth)
	}
}

func TestLoadDigitalEmployeeAuthorizationForDisabledRecordForcesInactive(t *testing.T) {
	settings := newFakeSettingsRepo()
	expiresAt := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	if err := settings.Set(context.Background(), systemKeyCenterRegistration, mustJSON(registrationRecord{
		Registered: true,
		Disabled:   true,
		HubID:      "hub_disabled",
		HubSecret:  "secret_disabled",
		DigitalEmployeeAuthorization: &corelib.DigitalEmployeeAuthorization{
			Quota:     3,
			Enabled:   true,
			ExpiresAt: expiresAt,
			Active:    true,
		},
	})); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	auth := LoadDigitalEmployeeAuthorization(context.Background(), settings)
	if auth == nil || auth.Active || auth.Enabled || auth.Reason != "disabled" || auth.Quota != 3 {
		t.Fatalf("auth = %+v, want inactive disabled while preserving quota", auth)
	}
}

func TestLoadDigitalEmployeeAuthorizationForTenantDoesNotFallbackToHubAuthorization(t *testing.T) {
	now := time.Now().UTC()
	settings := newFakeSettingsRepo()
	hubAuth := corelib.NormalizeDigitalEmployeeAuthorization(corelib.DigitalEmployeeAuthorization{Quota: 3, Enabled: true, ExpiresAt: now.Add(24 * time.Hour).Format(time.RFC3339)}, now)
	tenantAuth := corelib.NormalizeDigitalEmployeeAuthorization(corelib.DigitalEmployeeAuthorization{Quota: 1, Enabled: true, ExpiresAt: now.Add(24 * time.Hour).Format(time.RFC3339)}, now)
	defaultTenantAuth := corelib.NormalizeDigitalEmployeeAuthorization(corelib.DigitalEmployeeAuthorization{Quota: 2, Enabled: true, ExpiresAt: now.Add(24 * time.Hour).Format(time.RFC3339)}, now)
	if err := settings.Set(context.Background(), systemKeyCenterRegistration, mustJSON(registrationRecord{
		Registered:                   true,
		HubID:                        "hub_tenant_auth",
		HubSecret:                    "secret_tenant_auth",
		DigitalEmployeeAuthorization: &hubAuth,
		DigitalEmployeeAuthorizations: map[string]*corelib.DigitalEmployeeAuthorization{
			"tenant_a":            &tenantAuth,
			store.DefaultTenantID: &defaultTenantAuth,
		},
	})); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	if auth := LoadDigitalEmployeeAuthorizationForTenant(context.Background(), settings, "tenant_a"); auth == nil || auth.Quota != 1 || !auth.Active {
		t.Fatalf("tenant_a auth = %+v, want tenant quota 1", auth)
	}
	if auth := LoadDigitalEmployeeAuthorizationForTenant(context.Background(), settings, "tenant_b"); auth != nil {
		t.Fatalf("tenant_b auth = %+v, want nil instead of hub-level fallback", auth)
	}
	if auth := LoadDigitalEmployeeAuthorizationForTenant(context.Background(), settings, store.DefaultTenantID); auth == nil || auth.Quota != 2 || !auth.Active {
		t.Fatalf("default tenant auth = %+v, want tenant quota 2", auth)
	}
	if auth := LoadDigitalEmployeeAuthorizationForTenant(context.Background(), settings, ""); auth == nil || auth.Quota != 3 || !auth.Active {
		t.Fatalf("default auth = %+v, want legacy hub quota 3", auth)
	}
}

func TestSendHeartbeatUsesStoredRegistration(t *testing.T) {
	var (
		gotPath   string
		gotSecret string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		gotSecret, _ = payload["hub_secret"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"status":"online"}`))
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Center.BaseURL = server.URL
	cfg.Center.Enabled = true
	cfg.Center.HeartbeatIntervalSec = 1
	cfg.Server.PublicBaseURL = "https://hub.example.com"

	settings := newFakeSettingsRepo()
	svc := NewService(cfg, settings)
	err := settings.Set(context.Background(), systemKeyCenterRegistration, mustJSON(registrationRecord{
		Registered: true,
		HubID:      "hub_123",
		HubSecret:  "secret_456",
	}))
	if err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	if err := svc.sendHeartbeat(context.Background()); err != nil {
		t.Fatalf("sendHeartbeat() error = %v", err)
	}

	if gotPath != "/api/hubs/hub_123/heartbeat" {
		t.Fatalf("heartbeat path = %q, want %q", gotPath, "/api/hubs/hub_123/heartbeat")
	}
	if gotSecret != "secret_456" {
		t.Fatalf("hub_secret = %q, want %q", gotSecret, "secret_456")
	}

	raw, err := settings.Get(context.Background(), systemKeyCenterRegistration)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	var record registrationRecord
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if record.LastRegisteredAt <= 0 {
		t.Fatalf("LastRegisteredAt = %d, want > 0", record.LastRegisteredAt)
	}
	if record.LastError != "" {
		t.Fatalf("LastError = %q, want empty", record.LastError)
	}
}

func TestSendHeartbeatStoresGenericAuthorizationPayloads(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"ok": true,
			"status": "online",
			"authorizations": {
				"llm_compute": {
					"tenants": {
						"tenant_acme": {
							"hub_id": "hub_auth_payload",
							"tenant_id": "tenant_acme",
							"allow_external_providers": true,
							"authorizations": [{"id": "external_payload", "active": true}]
						}
					}
				}
			}
		}`))
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Center.BaseURL = server.URL
	cfg.Center.Enabled = true
	cfg.Server.PublicBaseURL = "https://hub.example.com"

	settings := newFakeSettingsRepo()
	svc := NewService(cfg, settings)
	if err := settings.Set(context.Background(), systemKeyCenterRegistration, mustJSON(registrationRecord{
		Registered: true,
		HubID:      "hub_auth_payload",
		HubSecret:  "secret_payload",
	})); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	if err := svc.sendHeartbeat(context.Background()); err != nil {
		t.Fatalf("sendHeartbeat() error = %v", err)
	}
	status, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if len(status.Authorizations["llm_compute"]) == 0 {
		t.Fatalf("missing llm_compute authorization payload: %#v", status.Authorizations)
	}
	if !strings.Contains(string(status.Authorizations["llm_compute"]), "external_payload") {
		t.Fatalf("llm_compute authorization payload = %s", string(status.Authorizations["llm_compute"]))
	}
}

func TestStartBackgroundSyncStartsHeartbeatForRegisteredHub(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"status":"online"}`))
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Center.Enabled = true
	cfg.Center.BaseURL = server.URL
	cfg.Center.HeartbeatIntervalSec = 1
	cfg.Server.PublicBaseURL = "https://hub.example.com"

	settings := newFakeSettingsRepo()
	_ = settings.Set(context.Background(), systemKeyCenterRegistration, mustJSON(registrationRecord{
		Registered: true,
		HubID:      "hub_bg",
		HubSecret:  "secret_bg",
	}))

	svc := NewService(cfg, settings)
	svc.StartBackgroundSync()

	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if calls > 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("expected heartbeat to be sent, calls=%d", calls)
}

func TestStartBackgroundSyncTriggersDeviceCredentialRecoveryForRegisteredHub(t *testing.T) {
	cfg := config.Default()
	cfg.Center.Enabled = true

	settings := newFakeSettingsRepo()
	_ = settings.Set(context.Background(), systemKeyCenterRegistration, mustJSON(registrationRecord{
		Registered: true,
		HubID:      "hub_credentials",
		HubSecret:  "secret_credentials",
	}))

	svc := NewService(cfg, settings)
	recovered := make(chan struct{}, 1)
	svc.SetDeviceCredentialRecovery(func(context.Context) {
		recovered <- struct{}{}
	})
	svc.StartBackgroundSync()

	select {
	case <-recovered:
	case <-time.After(time.Second):
		t.Fatal("expected registered Hub to trigger device credential recovery")
	}
}

func TestRecoverDeviceCredentialsNowRunsHookSynchronously(t *testing.T) {
	svc := NewService(config.Default(), newFakeSettingsRepo())
	calls := 0
	svc.SetDeviceCredentialRecovery(func(ctx context.Context) {
		if ctx == nil {
			t.Fatal("recovery context is nil")
		}
		calls++
	})
	svc.RecoverDeviceCredentialsNow(context.Background())
	if calls != 1 {
		t.Fatalf("recovery calls=%d, want 1", calls)
	}
}

func TestBackupDeviceCredentialsDoesNotRetryUnauthorizedCenter(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if got := r.URL.Path; got != "/api/hubs/hub_credentials/device-credentials" {
			t.Fatalf("path = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("Authorization = %q", got)
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Center.Enabled = true
	cfg.Center.BaseURL = server.URL
	settings := newFakeSettingsRepo()
	_ = settings.Set(context.Background(), systemKeyCenterRegistration, mustJSON(registrationRecord{
		Registered: true, HubID: "hub_credentials", HubSecret: "secret",
	}))
	svc := NewService(cfg, settings)
	if err := svc.BackupDeviceCredentials(context.Background(), `{"tokens":{}}`); err == nil {
		t.Fatal("expected unauthorized backup failure")
	}
	if calls != 1 {
		t.Fatalf("backup calls = %d, want 1", calls)
	}
}

func TestBackupDeviceCredentialsRejectsOversizedSnapshotBeforeRequest(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Center.Enabled = true
	cfg.Center.BaseURL = server.URL
	settings := newFakeSettingsRepo()
	_ = settings.Set(context.Background(), systemKeyCenterRegistration, mustJSON(registrationRecord{
		Registered: true, HubID: "hub_credentials", HubSecret: "secret",
	}))
	svc := NewService(cfg, settings)
	err := svc.BackupDeviceCredentials(context.Background(), strings.Repeat("x", deviceCredentialBackupSnapshotMaxBytes+1))
	if err == nil || !strings.Contains(err.Error(), "snapshot exceeds") {
		t.Fatalf("BackupDeviceCredentials error=%v, want size limit error", err)
	}
	if called {
		t.Fatal("oversized snapshot reached Hub Center")
	}
}

func TestStartBackgroundSyncAutoRegistersWhenConfigured(t *testing.T) {
	var (
		registerCalls  int
		heartbeatCalls int
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/hubs/register":
			registerCalls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"hub_id":"hub_auto","hub_secret":"secret_auto"}`))
		case "/api/hubs/hub_auto/heartbeat":
			heartbeatCalls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"status":"online"}`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Center.Enabled = true
	cfg.Center.BaseURL = server.URL
	cfg.Center.RegisterOnStartup = true
	cfg.Center.HeartbeatIntervalSec = 1
	cfg.Server.PublicBaseURL = "https://hub.example.com"

	settings := newFakeSettingsRepo()
	_ = settings.Set(context.Background(), systemKeyAdminEmail, mustJSON(map[string]string{"value": "admin@example.com"}))

	svc := NewService(cfg, settings)
	svc.StartBackgroundSync()

	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if registerCalls > 0 {
			raw, err := settings.Get(context.Background(), systemKeyCenterRegistration)
			if err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			var record registrationRecord
			if err := json.Unmarshal([]byte(raw), &record); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}
			if !record.Registered || record.HubID != "hub_auto" || record.HubSecret != "secret_auto" {
				t.Fatalf("unexpected registration record: %+v", record)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("expected auto registration, registerCalls=%d heartbeatCalls=%d", registerCalls, heartbeatCalls)
}

func TestSendHeartbeatClearsRegistrationWhenHubIsUnregistered(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"code":"HUB_UNREGISTERED","message":"Hub is not registered"}`, http.StatusUnauthorized)
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Center.BaseURL = server.URL
	cfg.Center.Enabled = true
	cfg.Server.PublicBaseURL = "https://hub.example.com"

	settings := newFakeSettingsRepo()
	svc := NewService(cfg, settings)
	err := settings.Set(context.Background(), systemKeyCenterRegistration, mustJSON(registrationRecord{
		Registered: true,
		HubID:      "hub_deleted",
		HubSecret:  "secret_deleted",
	}))
	if err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	if err := svc.sendHeartbeat(context.Background()); err != nil {
		t.Fatalf("sendHeartbeat() error = %v", err)
	}

	raw, err := settings.Get(context.Background(), systemKeyCenterRegistration)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	var record registrationRecord
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if record.Registered {
		t.Fatalf("expected registration to be cleared, got %+v", record)
	}
	if record.HubID != "" || record.HubSecret != "" {
		t.Fatalf("expected hub credentials to be cleared, got %+v", record)
	}
}

func TestSendHeartbeatMarksHubDisabledWhenCenterLocksHub(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"code":"HUB_DISABLED","message":"Hub has been disabled by Hub Center"}`, http.StatusLocked)
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Center.BaseURL = server.URL
	cfg.Center.Enabled = true
	cfg.Server.PublicBaseURL = "https://hub.example.com"

	settings := newFakeSettingsRepo()
	svc := NewService(cfg, settings)
	err := settings.Set(context.Background(), systemKeyCenterRegistration, mustJSON(registrationRecord{
		Registered: true,
		HubID:      "hub_disabled",
		HubSecret:  "secret_disabled",
	}))
	if err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	if err := svc.sendHeartbeat(context.Background()); err != nil {
		t.Fatalf("sendHeartbeat() error = %v", err)
	}

	raw, err := settings.Get(context.Background(), systemKeyCenterRegistration)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	var record registrationRecord
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if !record.Disabled || record.Registered || record.PendingConfirmation {
		t.Fatalf("expected disabled registration state, got %+v", record)
	}
	if record.HubID == "" || record.HubSecret == "" {
		t.Fatalf("expected hub credentials to be retained, got %+v", record)
	}
}

func TestRegisterFailsWhenHubWasDisabledByCenter(t *testing.T) {
	cfg := config.Default()
	cfg.Center.Enabled = true
	cfg.Center.BaseURL = "http://127.0.0.1:9388"
	cfg.Server.PublicBaseURL = "https://hub.example.com"

	settings := newFakeSettingsRepo()
	_ = settings.Set(context.Background(), systemKeyCenterRegistration, mustJSON(registrationRecord{
		Disabled:  true,
		HubID:     "hub_disabled",
		HubSecret: "secret_disabled",
		LastError: "hub has been disabled by Hub Center",
	}))
	_ = settings.Set(context.Background(), systemKeyAdminEmail, mustJSON(map[string]string{"value": "admin@example.com"}))

	svc := NewService(cfg, settings)
	if _, err := svc.Register(context.Background(), "admin@example.com"); err == nil {
		t.Fatal("expected Register to fail while hub is disabled")
	}
}

func TestInstallationIDIsGeneratedOnceAndReused(t *testing.T) {
	cfg := config.Default()
	settings := newFakeSettingsRepo()
	svc := NewService(cfg, settings)

	first, err := svc.installationID(context.Background())
	if err != nil {
		t.Fatalf("installationID() first call error = %v", err)
	}
	second, err := svc.installationID(context.Background())
	if err != nil {
		t.Fatalf("installationID() second call error = %v", err)
	}

	if first == "" {
		t.Fatal("expected installation id to be generated")
	}
	if first != second {
		t.Fatalf("expected installation id to persist, got %q and %q", first, second)
	}
}

func TestRegisterFallsBackToConfiguredRecoveryOwnerEmail(t *testing.T) {
	var gotOwnerEmail string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/hubs/register" {
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			gotOwnerEmail, _ = payload["owner_email"].(string)
			_, _ = w.Write([]byte(`{"hub_id":"hub_recovered","hub_secret":"secret_recovered"}`))
			return
		}
		if strings.Contains(r.URL.Path, "/heartbeat") {
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		t.Fatalf("unexpected path %q", r.URL.Path)
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Center.Enabled = true
	cfg.Center.BaseURL = server.URL
	cfg.Center.InstallationID = "inst_recovery"
	cfg.Center.OwnerEmail = "owner@example.com"
	cfg.Server.PublicBaseURL = "https://hub.example.com"
	svc := NewService(cfg, newFakeSettingsRepo())
	if _, err := svc.Register(context.Background(), ""); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if gotOwnerEmail != "owner@example.com" {
		t.Fatalf("owner email = %q, want recovery owner", gotOwnerEmail)
	}
}

func TestRegisterFallsBackToStoredAdminEmail(t *testing.T) {
	var gotOwnerEmail string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/hubs/register" {
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			gotOwnerEmail, _ = payload["owner_email"].(string)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"hub_id":"hub_fallback","hub_secret":"secret_fallback"}`))
			return
		}
		// Accept heartbeat requests triggered by startHeartbeatLoop after registration.
		if strings.Contains(r.URL.Path, "/heartbeat") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		t.Fatalf("unexpected path %q", r.URL.Path)
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Center.Enabled = true
	cfg.Center.BaseURL = server.URL
	cfg.Server.PublicBaseURL = "https://hub.example.com"

	settings := newFakeSettingsRepo()
	_ = settings.Set(context.Background(), systemKeyAdminEmail, mustJSON(map[string]string{"value": "stored-admin@local.admin"}))

	svc := NewService(cfg, settings)
	status, err := svc.Register(context.Background(), "")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if gotOwnerEmail != "stored-admin@local.admin" {
		t.Fatalf("owner_email = %q, want %q", gotOwnerEmail, "stored-admin@local.admin")
	}
	if status == nil || !status.Registered || status.HubID != "hub_fallback" {
		t.Fatalf("unexpected registration status: %+v", status)
	}
}

func TestStatusAndRegisterUseStoredVisibility(t *testing.T) {
	var gotVisibility string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/heartbeat") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		gotVisibility, _ = payload["visibility"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hub_id":"hub_visibility","hub_secret":"secret_visibility"}`))
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Center.Enabled = true
	cfg.Center.BaseURL = server.URL
	cfg.Server.PublicBaseURL = "https://hub.example.com"

	settings := newFakeSettingsRepo()
	_ = settings.Set(context.Background(), systemKeyAdminEmail, mustJSON(map[string]string{"value": "admin@example.com"}))

	svc := NewService(cfg, settings)
	if _, err := svc.SetVisibility(context.Background(), "shared"); err != nil {
		t.Fatalf("SetVisibility() error = %v", err)
	}

	status, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.Visibility != "shared" {
		t.Fatalf("expected shared visibility in status, got %+v", status)
	}

	if _, err := svc.Register(context.Background(), "admin@example.com"); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if gotVisibility != "shared" {
		t.Fatalf("expected shared visibility in registration payload, got %q", gotVisibility)
	}
}

func TestStatusFallsBackToConfiguredCorporateEmailDomain(t *testing.T) {
	cfg := config.Default()
	cfg.Center.Enabled = true
	cfg.Center.BaseURL = "http://127.0.0.1:9388"
	cfg.Server.PublicBaseURL = "https://hub.example.com"
	cfg.Hub.CorporateEmailDomain = "@RAPIDAI.TECH"

	settings := newFakeSettingsRepo()
	svc := NewService(cfg, settings)

	status, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.CorporateEmailDomain != "rapidai.tech" {
		t.Fatalf("expected normalized configured corporate domain, got %+v", status)
	}
}

func TestRegisterUsesNormalizedStoredCorporateEmailDomain(t *testing.T) {
	var gotCorporateDomain string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/heartbeat") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		if r.URL.Path != "/api/hubs/register" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		gotCorporateDomain, _ = payload["corporate_email_domain"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hub_id":"hub_corp","hub_secret":"secret_corp"}`))
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Center.Enabled = true
	cfg.Center.BaseURL = server.URL
	cfg.Server.PublicBaseURL = "https://hub.example.com"

	settings := newFakeSettingsRepo()
	_ = settings.Set(context.Background(), systemKeyAdminEmail, mustJSON(map[string]string{"value": "admin@example.com"}))

	svc := NewService(cfg, settings)
	if _, err := svc.SetCorporateEmailDomain(context.Background(), "@RAPIDAI.TECH"); err != nil {
		t.Fatalf("SetCorporateEmailDomain() error = %v", err)
	}

	status, err := svc.Register(context.Background(), "admin@example.com")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if status == nil || !status.Registered {
		t.Fatalf("unexpected registration status: %+v", status)
	}
	if gotCorporateDomain != "rapidai.tech" {
		t.Fatalf("expected normalized corporate domain in registration payload, got %q", gotCorporateDomain)
	}
}

func TestStatusAndRegisterUseLegacyStoredCorporateDomain(t *testing.T) {
	var gotDomains []string
	var gotCorporateDomain string
	var gotAcceptPublicSignup bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/heartbeat") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		if r.URL.Path != "/api/hubs/register" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		gotCorporateDomain, _ = payload["corporate_email_domain"].(string)
		if rawDomains, ok := payload["corporate_email_domains"].([]any); ok {
			for _, item := range rawDomains {
				gotDomains = append(gotDomains, item.(string))
			}
		}
		gotAcceptPublicSignup, _ = payload["accept_public_signup"].(bool)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hub_id":"hub_legacy_domain","hub_secret":"secret_legacy_domain"}`))
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Center.Enabled = true
	cfg.Center.BaseURL = server.URL
	cfg.Server.PublicBaseURL = "https://hub.example.com"
	cfg.Hub.Visibility = "shared"

	settings := newFakeSettingsRepo()
	_ = settings.Set(context.Background(), systemKeyAdminEmail, mustJSON(map[string]string{"value": "admin@example.com"}))
	_ = settings.Set(context.Background(), systemKeyHubCorporateEmailDomain, mustJSON(map[string]string{"value": "@RAPIDAI.TECH"}))

	svc := NewService(cfg, settings)
	status, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.CorporateEmailDomain != "rapidai.tech" || len(status.CorporateEmailDomains) != 1 || status.CorporateEmailDomains[0] != "rapidai.tech" {
		t.Fatalf("unexpected legacy domain status: %+v", status)
	}
	if status.AcceptPublicSignup {
		t.Fatalf("expected legacy corporate domain to disable public signup, got %+v", status)
	}

	status, err = svc.Register(context.Background(), "admin@example.com")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if status == nil || !status.Registered {
		t.Fatalf("unexpected registration status: %+v", status)
	}
	if gotCorporateDomain != "rapidai.tech" {
		t.Fatalf("expected legacy corporate domain in registration payload, got %q", gotCorporateDomain)
	}
	if len(gotDomains) != 1 || gotDomains[0] != "rapidai.tech" {
		t.Fatalf("expected legacy domain to populate corporate_email_domains, got %#v", gotDomains)
	}
	if gotAcceptPublicSignup {
		t.Fatalf("expected legacy corporate domain to keep accept_public_signup false")
	}
}

func TestRegisterUsesCorporateEmailDomainsAndPublicSignup(t *testing.T) {
	var gotDomains []string
	var gotAcceptPublicSignup bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/hubs/hub_multi_domain/heartbeat" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		if r.URL.Path != "/api/hubs/register" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		for _, item := range payload["corporate_email_domains"].([]any) {
			gotDomains = append(gotDomains, item.(string))
		}
		gotAcceptPublicSignup, _ = payload["accept_public_signup"].(bool)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hub_id":"hub_multi_domain","hub_secret":"secret_multi_domain"}`))
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Center.Enabled = true
	cfg.Center.BaseURL = server.URL
	cfg.Server.PublicBaseURL = "https://hub.example.com"
	cfg.Hub.CorporateEmailDomains = []string{"@RAPIDAI.TECH", "subsidiary.example", "rapidai.tech"}

	settings := newFakeSettingsRepo()
	_ = settings.Set(context.Background(), systemKeyAdminEmail, mustJSON(map[string]string{"value": "admin@example.com"}))

	svc := NewService(cfg, settings)
	if _, err := svc.SetAcceptPublicSignup(context.Background(), true); err != nil {
		t.Fatalf("SetAcceptPublicSignup() error = %v", err)
	}

	status, err := svc.Register(context.Background(), "admin@example.com")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if status == nil || !status.Registered {
		t.Fatalf("unexpected registration status: %+v", status)
	}
	if len(gotDomains) != 2 || gotDomains[0] != "rapidai.tech" || gotDomains[1] != "subsidiary.example" {
		t.Fatalf("expected normalized corporate domains, got %#v", gotDomains)
	}
	if !gotAcceptPublicSignup {
		t.Fatalf("expected accept_public_signup to be true")
	}
	if status.CorporateEmailDomain != "rapidai.tech" || len(status.CorporateEmailDomains) != 2 {
		t.Fatalf("unexpected status domains: %+v", status)
	}
}

func TestRegisterFallsBackToSecondaryCenterNode(t *testing.T) {
	badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"temporary failure"}`, http.StatusInternalServerError)
	}))
	defer badServer.Close()

	goodCalls := 0
	goodServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/client/quality" && r.URL.Path != "/api/hubs/register" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if r.URL.Path == "/api/client/quality" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"quality_score":95,"routable":true,"service_status":"healthy"}`))
			return
		}
		goodCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hub_id":"hub_multi","hub_secret":"secret_multi"}`))
	}))
	defer goodServer.Close()

	cfg := config.Default()
	cfg.Center.Enabled = true
	cfg.Center.BaseURL = badServer.URL
	cfg.Center.BaseURLs = []string{badServer.URL, goodServer.URL}
	cfg.Server.PublicBaseURL = "https://hub.example.com"

	settings := newFakeSettingsRepo()
	_ = settings.Set(context.Background(), systemKeyAdminEmail, mustJSON(map[string]string{"value": "admin@example.com"}))

	svc := NewService(cfg, settings)
	status, err := svc.Register(context.Background(), "admin@example.com")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if goodCalls != 1 {
		t.Fatalf("expected secondary center to handle registration, calls=%d", goodCalls)
	}
	if status.ActiveBaseURL != goodServer.URL {
		t.Fatalf("ActiveBaseURL = %q, want %q", status.ActiveBaseURL, goodServer.URL)
	}
}

func TestSendHeartbeatFallsBackWhenNodeNotReady(t *testing.T) {
	firstServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/client/quality" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"quality_score":98,"routable":true,"service_status":"healthy"}`))
			return
		}
		http.Error(w, `{"code":"HUB_NOT_READY_ON_NODE","message":"Hub metadata is not available on this node yet."}`, http.StatusConflict)
	}))
	defer firstServer.Close()

	secondCalls := 0
	secondServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/client/quality" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"quality_score":90,"routable":true,"service_status":"healthy"}`))
			return
		}
		secondCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"status":"online"}`))
	}))
	defer secondServer.Close()

	cfg := config.Default()
	cfg.Center.Enabled = true
	cfg.Center.BaseURL = firstServer.URL
	cfg.Center.BaseURLs = []string{firstServer.URL, secondServer.URL}
	cfg.Server.PublicBaseURL = "https://hub.example.com"

	settings := newFakeSettingsRepo()
	svc := NewService(cfg, settings)
	if err := settings.Set(context.Background(), systemKeyCenterRegistration, mustJSON(registrationRecord{
		Registered: true,
		HubID:      "hub_ha",
		HubSecret:  "secret_ha",
	})); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	if err := svc.sendHeartbeat(context.Background()); err != nil {
		t.Fatalf("sendHeartbeat() error = %v", err)
	}
	if secondCalls != 1 {
		t.Fatalf("expected fallback heartbeat on secondary node, calls=%d", secondCalls)
	}

	raw, err := settings.Get(context.Background(), systemKeyCenterRegistration)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	var record registrationRecord
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if record.LastBaseURL != secondServer.URL {
		t.Fatalf("LastBaseURL = %q, want %q", record.LastBaseURL, secondServer.URL)
	}
	if record.LastError != "" {
		t.Fatalf("LastError = %q, want empty", record.LastError)
	}
}

func TestRestoreDeviceCredentialsFallsBackPastMissingHANode(t *testing.T) {
	firstServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/client/quality" {
			_, _ = w.Write([]byte(`{"quality_score":99,"routable":true}`))
			return
		}
		if r.URL.Path != "/api/hubs/hub_credentials/device-credentials" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		http.Error(w, `{"code":"DEVICE_CREDENTIALS_NOT_FOUND"}`, http.StatusNotFound)
	}))
	defer firstServer.Close()

	secondServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/client/quality" {
			_, _ = w.Write([]byte(`{"quality_score":90,"routable":true}`))
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret_credentials" {
			t.Fatalf("Authorization = %q", got)
		}
		_, _ = w.Write([]byte(`{"found":true,"device_credentials":"snapshot"}`))
	}))
	defer secondServer.Close()

	cfg := config.Default()
	cfg.Center.Enabled = true
	cfg.Center.BaseURLs = []string{firstServer.URL, secondServer.URL}
	cfg.Center.BaseURL = ""
	settings := newFakeSettingsRepo()
	_ = settings.Set(context.Background(), systemKeyCenterRegistration, mustJSON(registrationRecord{
		Registered: true, HubID: "hub_credentials", HubSecret: "secret_credentials", LastBaseURL: firstServer.URL,
	}))
	svc := NewService(cfg, settings)
	snapshot, found, err := svc.RestoreDeviceCredentials(context.Background())
	if err != nil || !found || snapshot != "snapshot" {
		t.Fatalf("RestoreDeviceCredentials() = (%q, %v, %v)", snapshot, found, err)
	}
	raw, err := settings.Get(context.Background(), systemKeyCenterRegistration)
	if err != nil {
		t.Fatal(err)
	}
	var record registrationRecord
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		t.Fatal(err)
	}
	if record.LastBaseURL != secondServer.URL {
		t.Fatalf("LastBaseURL = %q, want %q", record.LastBaseURL, secondServer.URL)
	}
}

func TestRestoreDeviceCredentialsReturnsAuthenticationFailure(t *testing.T) {
	var firstCalls, secondCalls int
	firstServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstCalls++
		http.Error(w, `{"code":"HUB_UNREGISTERED"}`, http.StatusUnauthorized)
	}))
	defer firstServer.Close()
	secondServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondCalls++
		_, _ = w.Write([]byte(`{"found":true,"device_credentials":"must-not-be-read"}`))
	}))
	defer secondServer.Close()

	cfg := config.Default()
	cfg.Center.Enabled = true
	cfg.Center.BaseURLs = []string{firstServer.URL}
	cfg.Center.BaseURL = ""
	settings := newFakeSettingsRepo()
	_ = settings.Set(context.Background(), systemKeyCenterRegistration, mustJSON(registrationRecord{
		Registered: true, HubID: "hub_credentials", HubSecret: "expired_secret",
	}))
	svc := NewService(cfg, settings)
	if _, found, err := svc.RestoreDeviceCredentials(context.Background()); err == nil || found {
		t.Fatalf("RestoreDeviceCredentials() should return authentication failure, found=%v err=%v", found, err)
	}
	if firstCalls != 1 || secondCalls != 0 {
		t.Fatalf("recovery calls = first:%d second:%d, want first:1 second:0", firstCalls, secondCalls)
	}
}

func TestRestoreDeviceCredentialsReturnsFailureForInvalidCenterResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Center.Enabled = true
	cfg.Center.BaseURL = server.URL
	settings := newFakeSettingsRepo()
	_ = settings.Set(context.Background(), systemKeyCenterRegistration, mustJSON(registrationRecord{
		Registered: true, HubID: "hub_credentials", HubSecret: "secret_credentials",
	}))
	svc := NewService(cfg, settings)
	if _, found, err := svc.RestoreDeviceCredentials(context.Background()); err == nil || found {
		t.Fatalf("RestoreDeviceCredentials() should reject invalid response, found=%v err=%v", found, err)
	}
}

func TestRestoreDeviceCredentialsRejectsOversizedSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret_credentials" {
			t.Fatalf("Authorization = %q", got)
		}
		_ = json.NewEncoder(w).Encode(deviceCredentialBackupResponse{
			Found:             true,
			DeviceCredentials: strings.Repeat("x", deviceCredentialBackupSnapshotMaxBytes+1),
		})
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Center.Enabled = true
	cfg.Center.BaseURL = server.URL
	settings := newFakeSettingsRepo()
	_ = settings.Set(context.Background(), systemKeyCenterRegistration, mustJSON(registrationRecord{
		Registered: true, HubID: "hub_credentials", HubSecret: "secret_credentials",
	}))
	svc := NewService(cfg, settings)
	if _, found, err := svc.RestoreDeviceCredentials(context.Background()); err == nil || found || !strings.Contains(err.Error(), "snapshot exceeding") {
		t.Fatalf("RestoreDeviceCredentials() should reject oversized snapshot, found=%v err=%v", found, err)
	}
}

func TestRefreshStatusClearsPendingConfirmationWhenHubWasRemoved(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"code":"HUB_UNREGISTERED","message":"Hub is not registered"}`, http.StatusUnauthorized)
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Center.BaseURL = server.URL
	cfg.Center.Enabled = true
	cfg.Server.PublicBaseURL = "https://hub.example.com"

	settings := newFakeSettingsRepo()
	svc := NewService(cfg, settings)
	err := settings.Set(context.Background(), systemKeyCenterRegistration, mustJSON(registrationRecord{
		PendingConfirmation: true,
		HubID:               "hub_pending_removed",
		HubSecret:           "secret_pending_removed",
		LastBaseURL:         server.URL,
		LastError:           "waiting for confirmation",
		LastRegisteredAt:    time.Now().Unix(),
	}))
	if err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	status, err := svc.RefreshStatus(context.Background())
	if err != nil {
		t.Fatalf("RefreshStatus() error = %v", err)
	}
	if status.Registered || status.PendingConfirmation || status.Disabled {
		t.Fatalf("expected pending registration to be cleared, got %+v", status)
	}
	if status.HubID != "" || status.ActiveBaseURL != "" {
		t.Fatalf("expected hub credentials/base url to be cleared, got %+v", status)
	}
	if status.LastError != "hub registration was removed by Hub Center" {
		t.Fatalf("unexpected LastError: %+v", status)
	}
	if status.LastRegisteredAt != 0 {
		t.Fatalf("expected LastRegisteredAt to reset, got %+v", status)
	}
}
