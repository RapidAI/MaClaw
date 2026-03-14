package center

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/config"
)

type fakeSettingsRepo struct {
	mu     sync.Mutex
	values map[string]string
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
	cfg.Center.BaseURL = "https://hubs.rapidai.tech"
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

func TestSendHeartbeatUsesStoredRegistration(t *testing.T) {
	var (
		gotPath   string
		gotSecret string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		gotSecret = payload["hub_secret"]
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
		Disabled:   true,
		HubID:      "hub_disabled",
		HubSecret:  "secret_disabled",
		LastError:  "hub has been disabled by Hub Center",
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

func TestRegisterFallsBackToStoredAdminEmail(t *testing.T) {
	var gotOwnerEmail string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/hubs/register" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		gotOwnerEmail, _ = payload["owner_email"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hub_id":"hub_fallback","hub_secret":"secret_fallback"}`))
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
