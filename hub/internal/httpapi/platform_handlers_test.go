package httpapi

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	corea2a "github.com/RapidAI/CodeClaw/corelib/a2a"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
)

type fakePlatformTenantRepo struct {
	items []*store.Tenant
}

type platformHubTestDeps struct {
	store    *store.Store
	provider *sqlite.Provider
}

func newPlatformHubTestDeps(t *testing.T) *platformHubTestDeps {
	t.Helper()
	provider, err := sqlite.NewProvider(sqlite.Config{DSN: t.TempDir() + `\hub-platform-test.db`, WAL: true, BusyTimeoutMS: 5000, MaxReadOpenConns: 4, MaxReadIdleConns: 2, MaxWriteOpenConns: 1, MaxWriteIdleConns: 1})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	return &platformHubTestDeps{store: sqlite.NewStore(provider), provider: provider}
}

func (f fakePlatformTenantRepo) Create(ctx context.Context, tenant *store.Tenant) error {
	_ = ctx
	_ = tenant
	return nil
}

func (f fakePlatformTenantRepo) DeleteByID(ctx context.Context, id string) error {
	_ = ctx
	_ = id
	return nil
}
func (f fakePlatformTenantRepo) GetByID(ctx context.Context, id string) (*store.Tenant, error) {
	_ = ctx
	for _, item := range f.items {
		if item != nil && item.ID == id {
			return item, nil
		}
	}
	return nil, nil
}

func (f fakePlatformTenantRepo) GetBySlug(ctx context.Context, slug string) (*store.Tenant, error) {
	_ = ctx
	for _, item := range f.items {
		if item != nil && item.Slug == slug {
			return item, nil
		}
	}
	return nil, nil
}

func (f fakePlatformTenantRepo) List(ctx context.Context) ([]*store.Tenant, error) {
	_ = ctx
	return f.items, nil
}

func (f fakePlatformTenantRepo) EnsureDefault(ctx context.Context) (*store.Tenant, error) {
	_ = ctx
	if len(f.items) > 0 {
		return f.items[0], nil
	}
	return &store.Tenant{ID: store.DefaultTenantID, Slug: "default", Name: "Default", Status: "active"}, nil
}

type failingPlatformSettingsRepo struct {
	raw string
}

func (f failingPlatformSettingsRepo) Get(ctx context.Context, key string) (string, error) {
	_ = ctx
	_ = key
	return f.raw, nil
}

func (f failingPlatformSettingsRepo) Set(ctx context.Context, key, valueJSON string) error {
	_ = ctx
	_ = valueJSON
	if key == platformRequestNonceRegistryKey {
		return nil
	}
	return errors.New("set failed")
}

type fakePlatformMachineSender struct {
	calls int
	err   error
}

func (f *fakePlatformMachineSender) SendToMachine(machineID string, msg any) error {
	_ = machineID
	_ = msg
	f.calls++
	return f.err
}

func TestPlatformAwareMachineSenderPrefersPlatformCallback(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	callbackCalls := 0
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/a2a/employees/platform-employee-1/messages" {
			t.Fatalf("unexpected callback path %s", r.URL.Path)
		}
		if got := r.Header.Get("X-VE-Callback-Secret"); got != "secret-1" {
			t.Fatalf("unexpected callback secret %q", got)
		}
		if r.Header.Get("X-VE-Callback-Timestamp") == "" || r.Header.Get("X-VE-Callback-Nonce") == "" {
			t.Fatalf("callback missing replay headers")
		}
		if got := r.Header.Get("X-VE-Hub-Tenant-ID"); got != "tenant-a" {
			t.Fatalf("unexpected Hub tenant header %q", got)
		}
		if got := r.Header.Get("X-VE-Hub-Employee-ID"); got != "ve_employee_1" {
			t.Fatalf("unexpected Hub employee header %q", got)
		}
		if got := r.Header.Get("X-VE-Hub-Account-ID"); got != "ve-account-1" {
			t.Fatalf("unexpected Hub account header %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode callback body: %v", err)
		}
		if body["payload"] == nil {
			t.Fatalf("callback body missing payload: %#v", body)
		}
		callbackCalls++
		w.WriteHeader(http.StatusAccepted)
	}))
	defer callback.Close()

	provider := platformProviderEntry{PlatformID: "platform-1", CallbackBaseURL: callback.URL, CallbackSecret: "secret-1", RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "ve-account-1", Status: veStatusActive, OnlineStatus: "platform", RegisteredAt: time.Now().UTC().Format(time.RFC3339)}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}

	fallback := &fakePlatformMachineSender{err: nil}
	sender := platformAwareMachineSender{fallback: fallback, system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}}
	if err := sender.SendToMachine("ve_employee_1", map[string]any{"type": "discussion.message"}); err != nil {
		t.Fatalf("SendToMachine returned error: %v", err)
	}
	if callbackCalls != 1 {
		t.Fatalf("expected one platform callback, got %d", callbackCalls)
	}
	if fallback.calls != 0 {
		t.Fatalf("fallback should not be used for platform employees when callback succeeds, got %d calls", fallback.calls)
	}
}

func TestPlatformA2APayloadExtractsEnvelopeIDs(t *testing.T) {
	envelope := corea2a.NewGroupEnvelope("env-1", corea2a.GroupMessageDiscussionMessage, "maclaw-a", time.Now().UTC())
	envelope.SessionID = "discussion-1"
	envelope.Message = &corea2a.GroupDiscussionMessage{ID: "message-1", SessionID: "discussion-1", FromID: "maclaw-a", Content: "hello"}

	payload := platformA2APayload(map[string]any{
		"type": "ve:discussion_message",
		"payload": map[string]any{
			"envelope": envelope,
		},
	})
	if payload["request_id"] != "env-1" || payload["hub_discussion_id"] != "discussion-1" || payload["hub_message_id"] != "message-1" || payload["content"] != "hello" {
		t.Fatalf("unexpected platform A2A payload: %#v", payload)
	}
	if payload["event_type"] != "ve:discussion_message" {
		t.Fatalf("event_type=%#v", payload["event_type"])
	}
	if payload["protocol_event_type"] != string(corea2a.GroupMessageDiscussionMessage) {
		t.Fatalf("protocol_event_type=%#v", payload["protocol_event_type"])
	}
}

func TestPlatformA2APayloadPreservesOuterEventType(t *testing.T) {
	envelope := corea2a.NewGroupEnvelope("env-cancel", corea2a.GroupMessageDiscussionResult, "maclaw-a", time.Now().UTC())
	envelope.SessionID = "discussion-1"

	payload := platformA2APayload(map[string]any{
		"type": "ve:discussion_cancel",
		"payload": map[string]any{
			"envelope": envelope,
		},
	})
	if payload["event_type"] != "ve:discussion_cancel" {
		t.Fatalf("event_type=%#v", payload["event_type"])
	}
	if payload["protocol_event_type"] != string(corea2a.GroupMessageDiscussionResult) {
		t.Fatalf("protocol_event_type=%#v", payload["protocol_event_type"])
	}
}

func TestPlatformAwareMachineSenderRoutesA2AInviteAndCancel(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	seen := make(chan string, 2)
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.URL.Path
		w.WriteHeader(http.StatusAccepted)
	}))
	defer callback.Close()

	provider := platformProviderEntry{PlatformID: "platform-1", CallbackBaseURL: callback.URL, CallbackSecret: "secret-1", RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "ve-account-1", Status: veStatusActive, OnlineStatus: "platform", RegisteredAt: time.Now().UTC().Format(time.RFC3339)}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}

	sender := platformAwareMachineSender{system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}}
	if err := sender.SendToMachine("ve_employee_1", map[string]any{"type": "ve:discussion_invite"}); err != nil {
		t.Fatalf("invite SendToMachine returned error: %v", err)
	}
	if err := sender.SendToMachine("ve_employee_1", map[string]any{"type": "ve:discussion_cancel"}); err != nil {
		t.Fatalf("cancel SendToMachine returned error: %v", err)
	}

	got := []string{<-seen, <-seen}
	want := []string{"/a2a/employees/platform-employee-1/invite", "/a2a/employees/platform-employee-1/cancel"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("callback paths=%v want %v", got, want)
	}
}

func TestPlatformA2AEndpointSuffixDoesNotTreatInvitationResponseAsInvite(t *testing.T) {
	if got := platformA2AEndpointSuffix(map[string]any{"payload": map[string]any{"envelope": corea2a.NewGroupEnvelope("env-response", corea2a.GroupMessageInvitationResponse, "maclaw-a", time.Now().UTC())}}); got != "/messages" {
		t.Fatalf("invitation response suffix=%q want /messages", got)
	}
	if got := platformA2AEndpointSuffix(map[string]any{"payload": map[string]any{"envelope": corea2a.NewGroupEnvelope("env-invite", corea2a.GroupMessageInvitation, "maclaw-a", time.Now().UTC())}}); got != "/invite" {
		t.Fatalf("invitation suffix=%q want /invite", got)
	}
}

func TestPostPlatformCallbackAddsReplayHeaders(t *testing.T) {
	seen := make(chan http.Header, 1)
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Clone()
		w.WriteHeader(http.StatusAccepted)
	}))
	defer callback.Close()

	postPlatformCallback(platformProviderEntry{CallbackBaseURL: callback.URL, CallbackSecret: "secret-1"}, "/api/hub/callback/migration", map[string]any{"migration_id": "mig-1", "status": "approved"})
	select {
	case header := <-seen:
		if header.Get("X-VE-Callback-Secret") != "secret-1" || header.Get("X-VE-Callback-Timestamp") == "" || header.Get("X-VE-Callback-Nonce") == "" {
			t.Fatalf("callback headers incomplete: %#v", header)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("callback was not posted")
	}
}

func TestPostPlatformTenantCallbacksSendsTenantReadiness(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	seen := make(chan map[string]any, 1)
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/hub/callback/tenant" {
			t.Fatalf("unexpected callback path %s", r.URL.Path)
		}
		if got := r.Header.Get("X-VE-Callback-Secret"); got != "secret-1" {
			t.Fatalf("unexpected callback secret %q", got)
		}
		if r.Header.Get("X-VE-Callback-Timestamp") == "" || r.Header.Get("X-VE-Callback-Nonce") == "" {
			t.Fatalf("callback missing replay headers")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode callback body: %v", err)
		}
		seen <- body
		w.WriteHeader(http.StatusAccepted)
	}))
	defer callback.Close()

	provider := platformProviderEntry{PlatformID: "platform-1", CallbackBaseURL: callback.URL, CallbackSecret: "secret-1", VirtualMailDomain: "ve.example.com", RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a", VirtualMailDomain: "tenant-a.custom.example.com"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	postPlatformTenantCallbacks(context.Background(), settings, &store.Tenant{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active", PrimaryDomain: "tenant-a.example.com"}, "")
	select {
	case body := <-seen:
		if body["hub_tenant_id"] != "tenant-a" || body["status"] != "active" || body["virtual_mail_domain"] != "tenant-a.custom.example.com" || body["ve_enabled"] != true {
			t.Fatalf("unexpected tenant callback body: %#v", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("tenant callback was not posted")
	}
}

func TestPlatformAwareMachineSenderFallsBackWhenCallbackFails(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer callback.Close()

	provider := platformProviderEntry{PlatformID: "platform-1", CallbackBaseURL: callback.URL, RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", Status: veStatusActive}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}

	fallback := &fakePlatformMachineSender{err: nil}
	sender := platformAwareMachineSender{fallback: fallback, system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}}
	if err := sender.SendToMachine("ve_employee_1", map[string]any{"type": "discussion.message"}); err != nil {
		t.Fatalf("SendToMachine returned error: %v", err)
	}
	if fallback.calls != 1 {
		t.Fatalf("expected fallback after callback failure, got %d calls", fallback.calls)
	}
}

func TestPlatformAwareMachineSenderReturnsCallbackErrorWithoutFallback(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	provider := platformProviderEntry{PlatformID: "platform-1", CallbackBaseURL: "http://127.0.0.1:1", RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", Status: veStatusActive}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}

	sender := platformAwareMachineSender{system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}}
	if err := sender.SendToMachine("ve_employee_1", map[string]any{"type": "discussion.message"}); err == nil {
		t.Fatal("expected callback error without fallback")
	} else if errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpdatePlatformEmployeeStatusDisablesTenantScopedRegistryEntry(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	provider := platformProviderEntry{PlatformID: "platform-1", RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", Status: veStatusActive, OnlineStatus: "platform"}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}

	tenantID, updated, err := updatePlatformEmployeeStatus(context.Background(), settings, fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}, "platform-1", "platform-employee-1", veStatusDisabled)
	if err != nil {
		t.Fatalf("update status: %v", err)
	}
	if !updated || tenantID != "tenant-a" {
		t.Fatalf("unexpected update result tenant=%q updated=%v", tenantID, updated)
	}
	updatedRegistry := loadVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings))
	if len(updatedRegistry.Employees) != 1 {
		t.Fatalf("unexpected registry: %#v", updatedRegistry)
	}
	got := updatedRegistry.Employees[0]
	if got.Status != veStatusDisabled || got.OnlineStatus != veOnlineStatusOffline || got.DisabledAt == "" {
		t.Fatalf("employee was not disabled correctly: %#v", got)
	}
}

type fakePlatformUserRepo struct {
	items []*store.User
}

func (f fakePlatformUserRepo) Create(ctx context.Context, user *store.User) error {
	_ = ctx
	_ = user
	return nil
}

func (f fakePlatformUserRepo) GetByID(ctx context.Context, id string) (*store.User, error) {
	_ = ctx
	for _, item := range f.items {
		if item != nil && item.ID == id {
			return item, nil
		}
	}
	return nil, nil
}

func (f fakePlatformUserRepo) GetByEmail(ctx context.Context, email string) (*store.User, error) {
	_ = ctx
	for _, item := range f.items {
		if item != nil && strings.EqualFold(item.Email, email) {
			return item, nil
		}
	}
	return nil, nil
}

func (f fakePlatformUserRepo) GetByTenantEmail(ctx context.Context, tenantID, email string) (*store.User, error) {
	_ = ctx
	for _, item := range f.items {
		if item != nil && item.TenantID == tenantID && strings.EqualFold(item.Email, email) {
			return item, nil
		}
	}
	return nil, nil
}

func (f fakePlatformUserRepo) List(ctx context.Context) ([]*store.User, error) {
	_ = ctx
	return f.items, nil
}

func (f fakePlatformUserRepo) ListByTenant(ctx context.Context, tenantID string) ([]*store.User, error) {
	_ = ctx
	out := make([]*store.User, 0, len(f.items))
	for _, item := range f.items {
		if item != nil && item.TenantID == tenantID {
			out = append(out, item)
		}
	}
	return out, nil
}

func (f fakePlatformUserRepo) DeleteByEmail(ctx context.Context, email string) error {
	_ = ctx
	_ = email
	return nil
}

func (f fakePlatformUserRepo) DeleteByTenantEmail(ctx context.Context, tenantID, email string) error {
	_ = ctx
	_ = tenantID
	_ = email
	return nil
}

func (f fakePlatformUserRepo) UpdateSmartRoute(ctx context.Context, userID string, enabled bool) error {
	_ = ctx
	_ = userID
	_ = enabled
	return nil
}

type fakePlatformViewerTokenRepo struct {
	items []*store.ViewerToken
}

func (f *fakePlatformViewerTokenRepo) Create(ctx context.Context, token *store.ViewerToken) error {
	_ = ctx
	f.items = append(f.items, token)
	return nil
}

func (f *fakePlatformViewerTokenRepo) GetByTokenHash(ctx context.Context, tokenHash string) (*store.ViewerToken, error) {
	_ = ctx
	_ = tokenHash
	return nil, nil
}

func (f *fakePlatformViewerTokenRepo) ExtendExpiry(ctx context.Context, tokenID string, expiresAt time.Time) error {
	_ = ctx
	_ = tokenID
	_ = expiresAt
	return nil
}

func TestPlatformSourceUsersForTenantExcludesPlatformEmployees(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	now := time.Now().UTC()
	users := fakePlatformUserRepo{items: []*store.User{
		{ID: "real-1", TenantID: "tenant-a", Email: "real@example.com", Status: "active", UpdatedAt: now},
		{ID: "ve-account-1", TenantID: "tenant-a", Email: "worker@tenant.ve.test", Status: "active", UpdatedAt: now},
		{ID: "real-other", TenantID: "tenant-b", Email: "other@example.com", Status: "active", UpdatedAt: now},
	}}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "ve-account-1", OwnerEmail: "worker@tenant.ve.test", Status: veStatusActive}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}

	items, err := platformSourceUsersForTenant(context.Background(), settings, users, "tenant-a")
	if err != nil {
		t.Fatalf("source users: %v", err)
	}
	if len(items) != 1 || items[0]["id"] != "real-1" {
		t.Fatalf("expected only real user, got %#v", items)
	}
}

func TestPlatformSourceUserViewerTokenUsesTenantUser(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	users := fakePlatformUserRepo{items: []*store.User{{ID: "src-local", TenantID: "tenant-a", Email: "other@example.com", Status: "active"}, {ID: "real-1", TenantID: "tenant-a", Email: "real@example.com", Status: "active"}}}
	viewerTokens := &fakePlatformViewerTokenRepo{}
	identity := auth.NewIdentityService(users, nil, nil, nil, viewerTokens, nil, nil, nil, "open", true, nil, "")
	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/source-users/src-local/viewer-token", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "source_user_id": "src-local", "external_id": "real-1", "email": "real@example.com"})
	rec := httptest.NewRecorder()
	PlatformSourceUserViewerTokenHandler(settings, fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}, users, identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("viewer token status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if strings.TrimSpace(resp["hub_llm_viewer_token"].(string)) == "" || resp["source_user_id"] != "real-1" || resp["hub_tenant_id"] != "tenant-a" {
		t.Fatalf("unexpected token response: %#v", resp)
	}
	if len(viewerTokens.items) != 1 || viewerTokens.items[0].UserID != "real-1" || viewerTokens.items[0].TenantID != "tenant-a" {
		t.Fatalf("viewer token was not issued for source user: %#v", viewerTokens.items)
	}
}

func TestPlatformEmployeeRegisterRequiresUserRepoAndEmployeeID(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	tenants := fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}

	missingRepoReq := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "employee_id": "platform-employee-1", "virtual_email": "worker@tenant.test", "name": "Worker"})
	missingRepoRec := httptest.NewRecorder()
	PlatformEmployeeRegisterHandler(settings, tenants, nil).ServeHTTP(missingRepoRec, missingRepoReq)
	if missingRepoRec.Code != http.StatusServiceUnavailable || !bytes.Contains(missingRepoRec.Body.Bytes(), []byte("USER_REPOSITORY_UNAVAILABLE")) {
		t.Fatalf("missing repo status=%d body=%s", missingRepoRec.Code, missingRepoRec.Body.String())
	}

	missingEmployeeReq := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "virtual_email": "worker@tenant.test", "name": "Worker"})
	missingEmployeeRec := httptest.NewRecorder()
	PlatformEmployeeRegisterHandler(settings, tenants, fakePlatformUserRepo{}).ServeHTTP(missingEmployeeRec, missingEmployeeReq)
	if missingEmployeeRec.Code != http.StatusBadRequest || !bytes.Contains(missingEmployeeRec.Body.Bytes(), []byte("EMPLOYEE_ID_REQUIRED")) {
		t.Fatalf("missing employee id status=%d body=%s", missingEmployeeRec.Code, missingEmployeeRec.Body.String())
	}

	platformOnlyReq := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "platform_employee_id": "platform-employee-2", "virtual_email": "worker2@tenant.test", "name": "Worker 2"})
	platformOnlyRec := httptest.NewRecorder()
	PlatformEmployeeRegisterHandler(settings, tenants, fakePlatformUserRepo{}).ServeHTTP(platformOnlyRec, platformOnlyReq)
	if platformOnlyRec.Code != http.StatusOK {
		t.Fatalf("platform employee id register status=%d body=%s", platformOnlyRec.Code, platformOnlyRec.Body.String())
	}
	registry := loadVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings))
	if len(registry.Employees) != 1 || registry.Employees[0].PlatformEmployeeID != "platform-employee-2" || registry.Employees[0].MachineID != "ve_platform-employee-2" {
		t.Fatalf("unexpected registered employee: %#v", registry.Employees)
	}
}
func TestPlatformEmployeeRegisterInvalidLLMServiceGroupDoesNotCreateDigitalEmployee(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	if err := llmservice.SaveRegistry(context.Background(), tenantSystem, &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "ops-pro", Name: "Ops Pro"}}}); err != nil {
		t.Fatalf("save llm registry: %v", err)
	}
	tenants := fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}

	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "employee_id": "platform-employee-1", "virtual_email": "worker@tenant.test", "name": "Worker", "llm_service_group_id": "missing-group"})
	rec := httptest.NewRecorder()
	PlatformEmployeeRegisterHandler(settings, tenants, fakePlatformUserRepo{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "LLM_SERVICE_GROUP_ENTITLEMENT_FAILED") {
		t.Fatalf("register status=%d body=%s", rec.Code, rec.Body.String())
	}
	registry := loadVERegistry(context.Background(), tenantSystem)
	if len(registry.Employees) != 0 {
		t.Fatalf("digital employee should not be created for invalid llm service group: %#v", registry.Employees)
	}
}
func TestPlatformEmployeeRegisterGrantsRequestedLLMServiceGroup(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	if err := llmservice.SaveRegistry(context.Background(), tenantSystem, &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "ops-pro", Name: "Ops Pro", AccessPolicy: llmservice.AccessPolicyGrantRequired, Models: []llmservice.ModelServiceModel{{Name: "gpt-test", ProviderIDs: []string{"hub-provider"}}}}}}); err != nil {
		t.Fatalf("save llm registry: %v", err)
	}
	tenants := fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}

	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "employee_id": "platform-employee-1", "virtual_email": "worker@tenant.test", "name": "Worker", "llm_service_group_id": "ops-pro"})
	rec := httptest.NewRecorder()
	PlatformEmployeeRegisterHandler(settings, tenants, fakePlatformUserRepo{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", rec.Code, rec.Body.String())
	}
	saved, err := llmservice.LoadRegistry(context.Background(), tenantSystem)
	if err != nil {
		t.Fatalf("load llm registry: %v", err)
	}
	status, _, err := llmservice.ResolveStatusFromRegistry(context.Background(), saved, nil, "worker@tenant.test", "https://hub.example/api/llm/v1")
	if err != nil {
		t.Fatalf("resolve status: %v", err)
	}
	if !status.Active || len(status.ServiceGroupIDs) != 1 || status.ServiceGroupIDs[0] != "ops-pro" || status.DefaultModel != "gpt-test" {
		t.Fatalf("status = %#v", status)
	}
	if len(status.ActiveGrants) != 1 || status.ActiveGrants[0].ServiceGroupID != "ops-pro" || status.ActiveGrants[0].Source != "ve_platform_employee" {
		t.Fatalf("active grants = %#v", status.ActiveGrants)
	}
}

func TestPlatformEmployeeViewerTokenUsesExistingEmployeeAccount(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_platform-employee-1", MachineID: "ve_platform-employee-1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "hub-account-1", OwnerEmail: "worker@tenant.test", Status: veStatusActive}}}
	if err := saveVERegistry(context.Background(), tenantSystem, registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	users := fakePlatformUserRepo{items: []*store.User{{ID: "hub-account-1", TenantID: "tenant-a", Email: "worker@tenant.test", Status: "active"}}}
	viewerTokens := &fakePlatformViewerTokenRepo{}
	identity := auth.NewIdentityService(users, nil, nil, nil, viewerTokens, nil, nil, nil, "open", true, nil, "")
	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees/platform-employee-1/viewer-token", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "platform_employee_id": "platform-employee-1", "hub_employee_id": "ve_platform-employee-1", "hub_account_id": "hub-account-1"})
	rec := httptest.NewRecorder()
	PlatformEmployeeViewerTokenHandler(settings, fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}, users, identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("viewer token status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if strings.TrimSpace(resp["hub_llm_viewer_token"].(string)) == "" || resp["hub_account_id"] != "hub-account-1" || resp["hub_employee_id"] != "ve_platform-employee-1" {
		t.Fatalf("unexpected token response: %#v", resp)
	}
	if len(viewerTokens.items) != 1 || viewerTokens.items[0].UserID != "hub-account-1" || viewerTokens.items[0].TenantID != "tenant-a" {
		t.Fatalf("viewer token was not issued for bound account: %#v", viewerTokens.items)
	}
}

func TestPlatformEmployeeExistsInTenantMatchesPlatformEmployeeID(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", Status: veStatusActive}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	if !platformEmployeeExistsInTenant(context.Background(), settings, "tenant-a", "platform-1", "platform-employee-1") {
		t.Fatal("expected platform employee to be found in tenant registry")
	}
	if platformEmployeeExistsInTenant(context.Background(), settings, "tenant-b", "platform-1", "platform-employee-1") {
		t.Fatal("employee should not be visible across tenants")
	}
	if platformEmployeeExistsInTenant(context.Background(), settings, "tenant-a", "other-platform", "platform-employee-1") {
		t.Fatal("employee should not match a different platform")
	}
}

func TestPlatformKnowledgeImportValidatesHubTenantAndEmployee(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", Status: veStatusActive}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}

	handler := PlatformKnowledgeImportHandler(settings, fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}, {ID: "tenant-b", Slug: "tenant-b", Name: "Tenant B", Status: "active"}}})

	goodReq := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/knowledge/imports", "platform-1", privateKey, map[string]any{"import_id": "kimp-1", "hub_tenant_id": "tenant-a", "platform_employee_id": "platform-employee-1", "title": "Case Pack"})
	goodRec := httptest.NewRecorder()
	handler.ServeHTTP(goodRec, goodReq)
	if goodRec.Code != http.StatusAccepted {
		t.Fatalf("expected accepted knowledge import, got status %d body %s", goodRec.Code, goodRec.Body.String())
	}

	crossTenantReq := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/knowledge/imports", "platform-1", privateKey, map[string]any{"import_id": "kimp-2", "hub_tenant_id": "tenant-b", "platform_employee_id": "platform-employee-1", "title": "Case Pack"})
	crossTenantRec := httptest.NewRecorder()
	handler.ServeHTTP(crossTenantRec, crossTenantReq)
	if crossTenantRec.Code != http.StatusNotFound {
		t.Fatalf("expected cross-tenant employee lookup to be rejected, got status %d body %s", crossTenantRec.Code, crossTenantRec.Body.String())
	}
}

func TestPlatformMigrationCallbackIncludesTargetHubIdentity(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	callbackBody := make(chan map[string]any, 2)
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode callback: %v", err)
		}
		callbackBody <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer callback.Close()
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), CallbackBaseURL: callback.URL, CallbackSecret: "secret-1", RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "hub-account-1", Status: veStatusActive}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	handler := PlatformMigrationSubmitHandler(settings, fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}})
	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/migrations", "platform-1", privateKey, map[string]any{"migration_id": "mig-1", "hub_tenant_id": "tenant-a", "target_employee_id": "platform-employee-1"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("migration submit status=%d body=%s", rec.Code, rec.Body.String())
	}
	seenCompleted := false
	for i := 0; i < 2; i++ {
		select {
		case body := <-callbackBody:
			if body["status"] == "completed" {
				seenCompleted = true
			}
			if body["hub_tenant_id"] != "tenant-a" || body["hub_employee_id"] != "ve_employee_1" || body["hub_account_id"] != "hub-account-1" {
				t.Fatalf("callback missing target identity: %#v", body)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("callback was not posted")
		}
	}
	if !seenCompleted {
		t.Fatal("migration callback never reached completed status")
	}
}

func TestPlatformMigrationRejectsMismatchedTargetIdentity(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "hub-account-1", Status: veStatusActive}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	handler := PlatformMigrationSubmitHandler(settings, fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}})
	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/migrations", "platform-1", privateKey, map[string]any{"migration_id": "mig-1", "hub_tenant_id": "tenant-a", "target_employee_id": "platform-employee-1", "target_hub_employee_id": "other-employee"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || !bytes.Contains(rec.Body.Bytes(), []byte("EMPLOYEE_IDENTITY_MISMATCH")) {
		t.Fatalf("identity mismatch status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPlatformKnowledgeImportCallbackIncludesEmployeeIdentity(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	callbackBody := make(chan map[string]any, 2)
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode callback: %v", err)
		}
		callbackBody <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer callback.Close()
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), CallbackBaseURL: callback.URL, CallbackSecret: "secret-1", RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "hub-account-1", Status: veStatusActive}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	handler := PlatformKnowledgeImportHandler(settings, fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}})
	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/knowledge/imports", "platform-1", privateKey, map[string]any{"import_id": "kimp-1", "hub_tenant_id": "tenant-a", "platform_employee_id": "platform-employee-1"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("knowledge submit status=%d body=%s", rec.Code, rec.Body.String())
	}
	seenCompleted := false
	for i := 0; i < 2; i++ {
		select {
		case body := <-callbackBody:
			if body["status"] == "completed" {
				seenCompleted = true
			}
			if body["hub_tenant_id"] != "tenant-a" || body["hub_employee_id"] != "ve_employee_1" || body["hub_account_id"] != "hub-account-1" {
				t.Fatalf("callback missing employee identity: %#v", body)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("callback was not posted")
		}
	}
	if !seenCompleted {
		t.Fatal("knowledge import callback never reached completed status")
	}
}

func TestPlatformEmployeeStatusAcceptsPlatformEmployeeID(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", Status: veStatusActive, OnlineStatus: "platform"}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}

	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees/ve_employee_1/status", "platform-1", privateKey, map[string]any{"platform_employee_id": "platform-employee-1", "service_status": "disabled"})
	rec := httptest.NewRecorder()
	PlatformEmployeeStatusHandler(settings, fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status update status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated := loadVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings))
	if len(updated.Employees) != 1 || updated.Employees[0].Status != veStatusDisabled {
		t.Fatalf("employee status was not updated: %#v", updated.Employees)
	}
}

func TestNormalizePlatformEmployeeDeletedStatusDisablesEmployee(t *testing.T) {
	for _, status := range []string{"deleted", "removed"} {
		if got := normalizePlatformEmployeeStatus(status); got != veStatusDisabled {
			t.Fatalf("normalize %q = %q, want %q", status, got, veStatusDisabled)
		}
	}
}

func TestPlatformEmployeeStatusRejectsMismatchedHubIdentity(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "hub-account-1", Status: veStatusActive, OnlineStatus: "platform"}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	handler := PlatformEmployeeStatusHandler(settings, fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}})
	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees/ve_employee_1/status", "platform-1", privateKey, map[string]any{"platform_employee_id": "platform-employee-1", "hub_tenant_id": "tenant-a", "hub_employee_id": "other-employee", "service_status": "disabled"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || !bytes.Contains(rec.Body.Bytes(), []byte("EMPLOYEE_IDENTITY_MISMATCH")) {
		t.Fatalf("identity mismatch status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated := loadVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings))
	if len(updated.Employees) != 1 || updated.Employees[0].Status != veStatusActive {
		t.Fatalf("employee status should not change: %#v", updated.Employees)
	}
}

func TestPlatformSyncJobRunValidatesEmployeeIdentity(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "hub-account-1", Status: veStatusActive, OnlineStatus: "platform"}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	tenants := fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}

	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/sync/jobs/sync-1/run", "platform-1", privateKey, map[string]any{"job_id": "sync-1", "hub_tenant_id": "tenant-a", "platform_employee_id": "platform-employee-1", "hub_employee_id": "ve_employee_1", "hub_account_id": "hub-account-1"})
	rec := httptest.NewRecorder()
	PlatformSyncJobRunHandler(settings, tenants).ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted || !bytes.Contains(rec.Body.Bytes(), []byte("hub_sync_job_id")) {
		t.Fatalf("sync run status=%d body=%s", rec.Code, rec.Body.String())
	}

	badReq := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/sync/jobs/sync-1/run", "platform-1", privateKey, map[string]any{"job_id": "sync-1", "hub_tenant_id": "tenant-a", "platform_employee_id": "platform-employee-1", "hub_employee_id": "other-employee"})
	badRec := httptest.NewRecorder()
	PlatformSyncJobRunHandler(settings, tenants).ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusForbidden || !bytes.Contains(badRec.Body.Bytes(), []byte("EMPLOYEE_IDENTITY_MISMATCH")) {
		t.Fatalf("identity mismatch status=%d body=%s", badRec.Code, badRec.Body.String())
	}
}

func TestPlatformSyncJobRunPostsCallbacks(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	callbackBody := make(chan map[string]any, 2)
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/hub/callback/sync" {
			t.Fatalf("unexpected callback path %s", r.URL.Path)
		}
		if r.Header.Get("X-VE-Callback-Secret") != "secret-1" || r.Header.Get("X-VE-Callback-Timestamp") == "" || r.Header.Get("X-VE-Callback-Nonce") == "" {
			t.Fatalf("callback headers incomplete: %#v", r.Header)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode callback: %v", err)
		}
		callbackBody <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer callback.Close()
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), CallbackBaseURL: callback.URL, CallbackSecret: "secret-1", RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "hub-account-1", Status: veStatusActive, OnlineStatus: "platform"}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/sync/jobs/sync-1/run", "platform-1", privateKey, map[string]any{"job_id": "sync-1", "hub_tenant_id": "tenant-a", "platform_employee_id": "platform-employee-1"})
	rec := httptest.NewRecorder()
	PlatformSyncJobRunHandler(settings, fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}).ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("sync run status=%d body=%s", rec.Code, rec.Body.String())
	}
	seenCompleted := false
	for i := 0; i < 2; i++ {
		select {
		case body := <-callbackBody:
			if body["status"] == "completed" {
				seenCompleted = true
			}
			if body["job_id"] != "sync-1" || body["hub_tenant_id"] != "tenant-a" || body["hub_employee_id"] != "ve_employee_1" || body["hub_account_id"] != "hub-account-1" {
				t.Fatalf("callback missing sync identity: %#v", body)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("sync callback was not posted")
		}
	}
	if !seenCompleted {
		t.Fatal("sync callback never reached completed status")
	}
}

func TestPlatformTenantDomainsReturnsSaveFailure(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	rawRegistry, err := json.Marshal(platformProviderRegistry{Providers: []platformProviderEntry{provider}})
	if err != nil {
		t.Fatalf("marshal provider registry: %v", err)
	}
	settings := failingPlatformSettingsRepo{raw: string(rawRegistry)}
	tenants := fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}

	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/providers/tenant-domains", "platform-1", privateKey, map[string]any{"tenant_domains": []map[string]any{{"hub_tenant_id": "tenant-a"}}})
	rec := httptest.NewRecorder()
	PlatformTenantDomainsHandler(settings, tenants).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError || !bytes.Contains(rec.Body.Bytes(), []byte("TENANT_DOMAINS_SAVE_FAILED")) {
		t.Fatalf("tenant domain save failure status=%d body=%s", rec.Code, rec.Body.String())
	}
}
func TestPlatformTenantEndpointsRequireActiveTenants(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-inactive"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	deletedAt := time.Now().UTC()
	tenants := fakePlatformTenantRepo{items: []*store.Tenant{
		{ID: "tenant-active", Slug: "active", Name: "Active", Status: "active"},
		{ID: "tenant-inactive", Slug: "inactive", Name: "Inactive", Status: "inactive"},
		{ID: "tenant-deleted", Slug: "deleted", Name: "Deleted", Status: "active", DeletedAt: &deletedAt},
	}}

	listReq := newSignedPlatformJSONRequest(t, http.MethodGet, "/api/platform/tenants", "platform-1", privateKey, map[string]any{})
	listRec := httptest.NewRecorder()
	PlatformTenantsListHandler(settings, tenants).ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("tenant list status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	if !bytes.Contains(listRec.Body.Bytes(), []byte("tenant-active")) || !bytes.Contains(listRec.Body.Bytes(), []byte("tenant-inactive")) || bytes.Contains(listRec.Body.Bytes(), []byte("tenant-deleted")) {
		t.Fatalf("tenant list should include active and inactive non-deleted tenants: %s", listRec.Body.String())
	}
	if !bytes.Contains(listRec.Body.Bytes(), []byte(`"ve_enabled":false`)) {
		t.Fatalf("inactive tenant should be returned with ve_enabled=false: %s", listRec.Body.String())
	}

	tenantDomainsReq := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/providers/tenant-domains", "platform-1", privateKey, map[string]any{"tenant_domains": []map[string]any{{"hub_tenant_id": "tenant-active", "tenant_id": "source-active"}, {"hub_tenant_id": "tenant-inactive", "tenant_id": "source-inactive"}}})
	tenantDomainsRec := httptest.NewRecorder()
	PlatformTenantDomainsHandler(settings, tenants).ServeHTTP(tenantDomainsRec, tenantDomainsReq)
	if tenantDomainsRec.Code != http.StatusOK || !bytes.Contains(tenantDomainsRec.Body.Bytes(), []byte(`"tenant_domain_count":1`)) {
		t.Fatalf("tenant domain update status=%d body=%s", tenantDomainsRec.Code, tenantDomainsRec.Body.String())
	}
	updatedProviders := loadPlatformProviderRegistry(context.Background(), settings)
	if len(updatedProviders.Providers) != 1 || len(updatedProviders.Providers[0].TenantDomains) != 1 || updatedProviders.Providers[0].TenantDomains[0].HubTenantID != "tenant-active" {
		t.Fatalf("tenant domain update should retain only active tenants: %#v", updatedProviders)
	}

	invalidTenantDomainsReq := newSignedPlatformRawRequest(t, http.MethodPost, "/api/platform/providers/tenant-domains", "platform-1", privateKey, []byte(`{"tenant_domains"`))
	invalidTenantDomainsRec := httptest.NewRecorder()
	PlatformTenantDomainsHandler(settings, tenants).ServeHTTP(invalidTenantDomainsRec, invalidTenantDomainsReq)
	if invalidTenantDomainsRec.Code != http.StatusBadRequest {
		t.Fatalf("invalid tenant domain json status=%d body=%s", invalidTenantDomainsRec.Code, invalidTenantDomainsRec.Body.String())
	}

	migrationReq := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/migrations", "platform-1", privateKey, map[string]any{"migration_id": "mig-1", "hub_tenant_id": "tenant-inactive"})
	migrationRec := httptest.NewRecorder()
	PlatformMigrationSubmitHandler(settings, tenants).ServeHTTP(migrationRec, migrationReq)
	if migrationRec.Code != http.StatusNotFound {
		t.Fatalf("inactive tenant migration status=%d body=%s", migrationRec.Code, migrationRec.Body.String())
	}

	sourceUsersReq := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/source-users/sync", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-inactive"})
	sourceUsersRec := httptest.NewRecorder()
	PlatformSourceUsersSyncHandler(settings, fakePlatformUserRepo{}, tenants).ServeHTTP(sourceUsersRec, sourceUsersReq)
	if sourceUsersRec.Code != http.StatusNotFound {
		t.Fatalf("inactive tenant source users status=%d body=%s", sourceUsersRec.Code, sourceUsersRec.Body.String())
	}

	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", Status: veStatusActive}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-inactive", settings), registry); err != nil {
		t.Fatalf("save inactive tenant ve registry: %v", err)
	}
	knowledgeReq := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/knowledge/imports", "platform-1", privateKey, map[string]any{"import_id": "kimp-inactive", "hub_tenant_id": "tenant-inactive", "platform_employee_id": "platform-employee-1"})
	knowledgeRec := httptest.NewRecorder()
	PlatformKnowledgeImportHandler(settings, tenants).ServeHTTP(knowledgeRec, knowledgeReq)
	if knowledgeRec.Code != http.StatusNotFound {
		t.Fatalf("inactive tenant knowledge import status=%d body=%s", knowledgeRec.Code, knowledgeRec.Body.String())
	}

	syncReq := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/sync/jobs/sync-inactive/run", "platform-1", privateKey, map[string]any{"job_id": "sync-inactive", "hub_tenant_id": "tenant-inactive", "platform_employee_id": "platform-employee-1"})
	syncRec := httptest.NewRecorder()
	PlatformSyncJobRunHandler(settings, tenants).ServeHTTP(syncRec, syncReq)
	if syncRec.Code != http.StatusNotFound {
		t.Fatalf("inactive tenant sync status=%d body=%s", syncRec.Code, syncRec.Body.String())
	}

	updatedTenant, updated, err := updatePlatformEmployeeStatus(context.Background(), settings, tenants, "platform-1", "platform-employee-1", veStatusDisabled)
	if err != nil {
		t.Fatalf("update inactive tenant employee status: %v", err)
	}
	if updated || updatedTenant != "" {
		t.Fatalf("inactive tenant employee should not be updated, tenant=%q updated=%v", updatedTenant, updated)
	}
}

func TestPlatformTenantsListIncludesVirtualEmployeeImportFields(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), VirtualMailDomain: "ve.example.com", RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-b", VirtualMailDomain: "custom-b.ve.example.com"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	tenants := fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active", PrimaryDomain: "tenant-a.example.com", SettingsJSON: `{"email_domains":["tenant-a.example.com","team-a.example.com"]}`, UpdatedAt: time.Now().UTC()}, {ID: "tenant-b", Slug: "tenant-b", Name: "Tenant B", Status: "active", PrimaryDomain: "tenant-b.example.com", UpdatedAt: time.Now().UTC()}}}

	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/tenants/list", "platform-1", privateKey, map[string]any{})
	rec := httptest.NewRecorder()
	PlatformTenantsListHandler(settings, tenants).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("tenant list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Tenants []struct {
			HubTenantID       string   `json:"hub_tenant_id"`
			Domains           []string `json:"domains"`
			VirtualMailDomain string   `json:"virtual_mail_domain"`
			VEEnabled         bool     `json:"ve_enabled"`
		} `json:"tenants"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Tenants) != 2 {
		t.Fatalf("expected two tenants, got %#v", resp.Tenants)
	}
	if resp.Tenants[0].HubTenantID != "tenant-a" || resp.Tenants[0].VirtualMailDomain != "tenant-a.ve.example.com" || !resp.Tenants[0].VEEnabled {
		t.Fatalf("tenant-a missing import fields: %#v", resp.Tenants[0])
	}
	if len(resp.Tenants[0].Domains) != 2 || resp.Tenants[0].Domains[1] != "team-a.example.com" {
		t.Fatalf("tenant-a missing email domains: %#v", resp.Tenants[0])
	}
	if resp.Tenants[1].HubTenantID != "tenant-b" || resp.Tenants[1].VirtualMailDomain != "custom-b.ve.example.com" || !resp.Tenants[1].VEEnabled {
		t.Fatalf("tenant-b missing custom import fields: %#v", resp.Tenants[1])
	}
}

func TestPlatformLLMOptionsAreTenantScoped(t *testing.T) {
	deps := newPlatformHubTestDeps(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := deps.store.Tenants.Create(ctx, &store.Tenant{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active", PrimaryDomain: "tenant-a.example.com", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if err := llmservice.SaveRegistry(ctx, scopedSystemSettingsForTenant("tenant-a", deps.store.System), &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}, {ID: "ops-pro", Name: "Ops Pro"}}, DefaultNewUserServiceGroups: []string{"coding-basic"}}); err != nil {
		t.Fatalf("save llm registry: %v", err)
	}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(ctx, deps.store.System, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}

	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/llm/options", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a"})
	rec := httptest.NewRecorder()
	PlatformLLMOptionsHandler(deps.store.System, deps.store.Tenants).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("options status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		DefaultServiceGroupID string `json:"default_service_group_id"`
		ServiceGroups         []struct {
			ID string `json:"id"`
		} `json:"service_groups"`
		Endpoints []struct {
			URL string `json:"url"`
		} `json:"endpoints"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.DefaultServiceGroupID != "coding-basic" || len(resp.ServiceGroups) != 2 || resp.ServiceGroups[1].ID != "ops-pro" || len(resp.Endpoints) != 1 {
		t.Fatalf("unexpected llm options: %#v", resp)
	}
}

func TestPlatformTenantAdminsListAndAuthenticateAreTenantScoped(t *testing.T) {
	deps := newPlatformHubTestDeps(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for _, tenant := range []*store.Tenant{
		{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active", PrimaryDomain: "tenant-a.example.com", CreatedAt: now, UpdatedAt: now},
		{ID: "tenant-b", Slug: "tenant-b", Name: "Tenant B", Status: "active", PrimaryDomain: "tenant-b.example.com", CreatedAt: now, UpdatedAt: now},
		{ID: "tenant-disabled", Slug: "tenant-disabled", Name: "Tenant Disabled", Status: "disabled", PrimaryDomain: "disabled.example.com", CreatedAt: now, UpdatedAt: now},
	} {
		if err := deps.store.Tenants.Create(ctx, tenant); err != nil {
			t.Fatalf("create tenant %s: %v", tenant.ID, err)
		}
	}
	adminSvc := auth.NewAdminService(deps.store.Admins, deps.store.System, deps.store.AdminAudit)
	if _, err := adminSvc.CreateTenantAdmin(ctx, "tenant-a", "shared", "pass-a-123", "shared-a@example.com", "Tenant A Admin", "tenant_admin"); err != nil {
		t.Fatalf("create tenant-a admin: %v", err)
	}
	if _, err := adminSvc.CreateTenantAdmin(ctx, "tenant-b", "shared", "pass-b-123", "shared-b@example.com", "Tenant B Admin", "tenant_admin"); err != nil {
		t.Fatalf("create tenant-b admin: %v", err)
	}
	if _, err := adminSvc.CreateTenantAdmin(ctx, "tenant-disabled", "disabled-admin", "disabled-123", "disabled@example.com", "Disabled Admin", "tenant_admin"); err != nil {
		t.Fatalf("create disabled tenant admin: %v", err)
	}

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(ctx, deps.store.System, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}

	listReq := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/tenant-admins/list", "platform-1", privateKey, map[string]any{})
	listRec := httptest.NewRecorder()
	PlatformTenantAdminsListHandler(deps.store.System, deps.store.Tenants, adminSvc).ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("tenant admin list status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	var listResp struct {
		TenantIDs []string `json:"tenant_ids"`
		Admins    []struct {
			HubTenantID string `json:"hub_tenant_id"`
			Username    string `json:"username"`
			Email       string `json:"email"`
		} `json:"admins"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode tenant admin list: %v", err)
	}
	if len(listResp.TenantIDs) != 2 || len(listResp.Admins) != 2 {
		t.Fatalf("unexpected tenant admin list response: %#v", listResp)
	}
	for _, admin := range listResp.Admins {
		if admin.HubTenantID == "tenant-disabled" {
			t.Fatalf("disabled tenant admin leaked into platform list: %#v", listResp)
		}
	}

	authReq := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/tenant-admins/authenticate", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-b", "username": "shared", "password": "pass-b-123"})
	authRec := httptest.NewRecorder()
	PlatformTenantAdminAuthenticateHandler(deps.store.System, deps.store.Tenants, adminSvc).ServeHTTP(authRec, authReq)
	if authRec.Code != http.StatusOK {
		t.Fatalf("tenant admin auth status=%d body=%s", authRec.Code, authRec.Body.String())
	}
	var authResp struct {
		OK    bool `json:"ok"`
		Admin struct {
			HubTenantID string `json:"hub_tenant_id"`
			Email       string `json:"email"`
		} `json:"admin"`
	}
	if err := json.Unmarshal(authRec.Body.Bytes(), &authResp); err != nil {
		t.Fatalf("decode tenant admin auth: %v", err)
	}
	if !authResp.OK || authResp.Admin.HubTenantID != "tenant-b" || authResp.Admin.Email != "shared-b@example.com" {
		t.Fatalf("tenant admin auth did not use tenant-b scope: %#v", authResp)
	}

	badReq := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/tenant-admins/authenticate", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-b", "username": "shared", "password": "pass-a-123"})
	badRec := httptest.NewRecorder()
	PlatformTenantAdminAuthenticateHandler(deps.store.System, deps.store.Tenants, adminSvc).ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected tenant-a password to fail in tenant-b scope, status=%d body=%s", badRec.Code, badRec.Body.String())
	}

	disabledReq := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/tenant-admins/authenticate", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-disabled", "username": "disabled-admin", "password": "disabled-123"})
	disabledRec := httptest.NewRecorder()
	PlatformTenantAdminAuthenticateHandler(deps.store.System, deps.store.Tenants, adminSvc).ServeHTTP(disabledRec, disabledReq)
	if disabledRec.Code != http.StatusNotFound {
		t.Fatalf("expected disabled tenant auth to be hidden, status=%d body=%s", disabledRec.Code, disabledRec.Body.String())
	}
}

func TestPlatformProviderRegisterStoresSignedProvider(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	publicKey := testPlatformPublicKeyPEM(t, privateKey)
	payload := map[string]any{
		"platform_id":             "platform-1",
		"platform_name":           "VE Test",
		"callback_base_url":       "https://ve.example.com/",
		"public_key":              publicKey,
		"public_key_fingerprint":  "SHA256:test",
		"virtual_mail_domain":     "VE.EXAMPLE.COM",
		"callback_secret":         "secret-1",
		"requested_features":      []string{"employees", "tenants"},
		"registration_request_id": "hreq_1",
	}
	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/providers/register", "platform-1", privateKey, payload)
	rec := httptest.NewRecorder()

	PlatformProviderRegisterHandler(settings)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		OK                 bool   `json:"ok"`
		RegistrationStatus string `json:"registration_status"`
		PlatformID         string `json:"platform_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.OK || resp.RegistrationStatus != "active" || resp.PlatformID != "platform-1" {
		t.Fatalf("unexpected response: %#v", resp)
	}
	registry := loadPlatformProviderRegistry(context.Background(), settings)
	idx := registry.find("platform-1")
	if idx < 0 {
		t.Fatal("provider was not stored")
	}
	stored := registry.Providers[idx]
	if stored.PlatformName != "VE Test" || stored.CallbackBaseURL != "https://ve.example.com" || stored.VirtualMailDomain != "ve.example.com" || stored.RegistrationStatus != "active" {
		t.Fatalf("unexpected stored provider: %#v", stored)
	}
}

func newSignedPlatformJSONRequest(t *testing.T, method, target, platformID string, privateKey *rsa.PrivateKey, payload any) *http.Request {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return newSignedPlatformRawRequest(t, method, target, platformID, privateKey, body)
}

func newSignedPlatformRawRequest(t *testing.T, method, target, platformID string, privateKey *rsa.PrivateKey, body []byte) *http.Request {
	t.Helper()
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	var nonceBytes [8]byte
	if _, err := rand.Read(nonceBytes[:]); err != nil {
		t.Fatalf("generate nonce: %v", err)
	}
	nonce := strings.ReplaceAll(t.Name(), "/", "-") + "-" + base64.RawURLEncoding.EncodeToString(nonceBytes[:])
	digest := sha256.Sum256(platformSignaturePayload(method, target, timestamp, nonce, body))
	sig, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign payload: %v", err)
	}
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-VE-Platform-ID", platformID)
	req.Header.Set("X-VE-Signature", base64.StdEncoding.EncodeToString(sig))
	req.Header.Set("X-VE-Timestamp", timestamp)
	req.Header.Set("X-VE-Nonce", nonce)
	return req
}

func testPlatformPublicKeyPEM(t *testing.T, privateKey *rsa.PrivateKey) string {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}
