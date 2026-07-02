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
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
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

func TestPlatformAwareMachineSenderDoesNotPostPlatformCallback(t *testing.T) {
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
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "ve-account-1", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline, RegisteredAt: time.Now().UTC().Format(time.RFC3339)}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}

	fallback := &fakePlatformMachineSender{err: nil}
	sender := platformAwareMachineSender{fallback: fallback, system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}}
	if err := sender.SendToMachine("ve_employee_1", map[string]any{"type": "discussion.message"}); err != nil {
		t.Fatalf("SendToMachine returned error: %v", err)
	}
	if callbackCalls != 0 {
		t.Fatalf("platform callback must not be used for execution events, got %d", callbackCalls)
	}
	if fallback.calls != 0 {
		t.Fatalf("fallback should not be used for platform employees when callback succeeds, got %d calls", fallback.calls)
	}
}

func TestPlatformAwareMachineSenderFallsBackForPhysicalDigitalEmployee(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_machine-a", MachineID: "machine-a", EmployeeType: veEmployeeTypePhysical, Name: "Desktop Worker", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	fallback := &fakePlatformMachineSender{err: nil}
	sender := platformAwareMachineSender{fallback: fallback, system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}}
	if err := sender.SendToMachine("machine-a", map[string]any{"type": "ve:discussion_message"}); err != nil {
		t.Fatalf("SendToMachine returned error: %v", err)
	}
	if fallback.calls != 1 {
		t.Fatalf("physical GUI/TUI employee should use websocket fallback, got %d calls", fallback.calls)
	}
}

func waitForDiscussionMessages(t *testing.T, svc *GroupDiscussionService, tenantID, sessionID string, want int, accept func([]corea2a.Message) bool) corea2a.HubDiscussionDetail {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var detail corea2a.HubDiscussionDetail
	var err error
	for time.Now().Before(deadline) {
		detail, err = svc.GetDiscussionDetail(tenantID, sessionID)
		if err == nil && len(detail.Messages) >= want && (accept == nil || accept(detail.Messages)) {
			return detail
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("GetDiscussionDetail: %v", err)
	}
	t.Fatalf("timed out waiting for discussion messages, got %#v", detail.Messages)
	return detail
}

func waitForFallbackMessages(t *testing.T, sender *captureGroupDiscussionSender, want int, accept func([]sentGroupDiscussionMessage) bool) []sentGroupDiscussionMessage {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var messages []sentGroupDiscussionMessage
	for time.Now().Before(deadline) {
		messages = sender.snapshotMessages()
		if len(messages) >= want && (accept == nil || accept(messages)) {
			return messages
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for fallback messages, got %#v", messages)
	return messages
}

func TestPlatformAwareMachineSenderFallsBackForNonMacLawRuntimeProvider(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: "https://runtime.example", TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_other_runtime", MachineID: "machine-other-runtime", PlatformID: "platform-1", PlatformEmployeeID: "platform-other-runtime", RuntimeProviderID: "other-runtime", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	fallback := &fakePlatformMachineSender{err: nil}
	sender := platformAwareMachineSender{fallback: fallback, system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}}
	if err := sender.SendToMachine("machine-other-runtime", map[string]any{"type": "ve:discussion_message"}); err != nil {
		t.Fatalf("SendToMachine returned error: %v", err)
	}
	if fallback.calls != 1 {
		t.Fatalf("non-MaClaw runtime provider should use websocket fallback, got %d calls", fallback.calls)
	}
}

func TestPlatformAwareMachineSenderFallsBackForLegacyPlatformIDOnlyEmployee(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_emp_legacy", MachineID: "ve_emp_legacy", PlatformID: "platform-1", Name: "Legacy Desktop Worker", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	fallback := &fakePlatformMachineSender{err: nil}
	sender := platformAwareMachineSender{fallback: fallback, system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}}
	if err := sender.SendToMachine("ve_emp_legacy", map[string]any{"type": "ve:discussion_message"}); err != nil {
		t.Fatalf("SendToMachine returned error: %v", err)
	}
	if fallback.calls != 1 {
		t.Fatalf("legacy platform-id-only employee should use websocket fallback, got %d calls", fallback.calls)
	}
}

func TestGroupDiscussionMessageFallsBackForPhysicalDigitalEmployee(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_machine-a", MachineID: "machine-a", EmployeeType: veEmployeeTypePhysical, Name: "Desktop Worker", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	groupSvc := NewGroupDiscussionService()
	session, err := groupSvc.CreateSession("tenant-a", CreateSessionRequest{Topic: "Direct", Goal: "hello", Participants: []corea2a.Participant{{ID: "maclaw-gui", RoleCode: "initiator"}, {ID: "machine-a", RoleCode: "speak"}}, DecisionPolicy: corea2a.PolicyMajority})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	fallback := &captureGroupDiscussionSender{}
	handler := NewGroupDiscussionHandler(groupSvc, platformAwareMachineSender{fallback: fallback, system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}})
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+session.ID+"/messages", `{"id":"hub-msg-physical-1","from_id":"maclaw-gui","content":"hello desktop"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("group message status=%d body=%s", w.Code, w.Body.String())
	}
	if len(fallback.messages) != 1 || fallback.messages[0].machineID != "machine-a" {
		t.Fatalf("physical employee should receive websocket delivery, got %#v", fallback.messages)
	}
}

func TestGroupDiscussionMessagePrefersPhysicalIDOverPlatformEmployeeIDCollision(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	runtimeCalls := 0
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimeCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":{"content":"wrong target"}}`))
	}))
	defer runtime.Close()
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: runtime.URL, TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{
		{ID: "ve_runtime", MachineID: "runtime-machine", PlatformID: "platform-1", PlatformEmployeeID: "machine-a", RuntimeProviderID: maclawSrvRuntimePlatformID, Name: "Runtime Worker", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline},
		{ID: "ve_machine-a", MachineID: "machine-a", EmployeeType: veEmployeeTypePhysical, Name: "Desktop Worker", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline},
	}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	groupSvc := NewGroupDiscussionService()
	session, err := groupSvc.CreateSession("tenant-a", CreateSessionRequest{Topic: "Direct", Goal: "hello", Participants: []corea2a.Participant{{ID: "maclaw-gui", RoleCode: "initiator"}, {ID: "machine-a", RoleCode: "speak"}}, DecisionPolicy: corea2a.PolicyMajority})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	fallback := &captureGroupDiscussionSender{}
	handler := NewGroupDiscussionHandler(groupSvc, platformAwareMachineSender{fallback: fallback, system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}})
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+session.ID+"/messages", `{"id":"hub-msg-physical-collision","from_id":"maclaw-gui","content":"hello desktop"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("group message status=%d body=%s", w.Code, w.Body.String())
	}
	if runtimeCalls != 0 {
		t.Fatalf("physical id collision must not call runtime, got %d", runtimeCalls)
	}
	if len(fallback.messages) != 1 || fallback.messages[0].machineID != "machine-a" {
		t.Fatalf("physical employee should receive websocket delivery, got %#v", fallback.messages)
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

func TestPlatformRuntimeLogValuesRedactIdentifiers(t *testing.T) {
	endpoint := runtimeDiscussionEndpointLogValue("https://runtime.example/api/runtime/virtual-employees/platform-employee-1/discussion-messages")
	if endpoint != "runtime.example/api/runtime/virtual-employees/*/discussion-messages" {
		t.Fatalf("endpoint log value = %q", endpoint)
	}
	redacted := platformLogID("platform-employee-1")
	if redacted == "" || strings.Contains(redacted, "platform-employee-1") || !strings.HasPrefix(redacted, "sha256:") {
		t.Fatalf("platform employee log id was not redacted: %q", redacted)
	}
	transportErr := sanitizeRuntimeDeliveryErrorText(`Post "https://runtime.example/api/runtime/virtual-employees/platform-employee-1/discussion-messages": dial tcp`, "platform-employee-1")
	if strings.Contains(transportErr, "platform-employee-1") || !strings.Contains(transportErr, "virtual-employees/*/discussion-messages") {
		t.Fatalf("runtime transport error was not redacted: %q", transportErr)
	}
	statusErr := sanitizeRuntimeDeliveryErrorText(`{"error":"runtime failed for platform-employee-1"}`, "platform-employee-1")
	if strings.Contains(statusErr, "platform-employee-1") {
		t.Fatalf("runtime status error was not redacted: %q", statusErr)
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

func TestPlatformAwareMachineSenderDoesNotRouteA2AInviteAndCancelThroughPlatform(t *testing.T) {
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
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "ve-account-1", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline, RegisteredAt: time.Now().UTC().Format(time.RFC3339)}}}
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

	select {
	case got := <-seen:
		t.Fatalf("platform callback must not receive invite/cancel execution event, got %s", got)
	default:
	}
}

func TestPlatformAwareMachineSenderDoesNotExecuteMacLawSrvInviteOrCancel(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	runtimeCalls := 0
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimeCalls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer runtime.Close()

	provider := platformProviderEntry{PlatformID: maclawSrvRuntimePlatformID, CallbackBaseURL: runtime.URL, CallbackSecret: "srv-secret", RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: runtime.URL, AdminSecret: "srv-secret", TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_srv", MachineID: "ve_srv", PlatformID: maclawSrvRuntimePlatformID, PlatformEmployeeID: "srv-user-1", OwnerUserID: "hub-account-1", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}

	fallback := &fakePlatformMachineSender{err: nil}
	sender := platformAwareMachineSender{fallback: fallback, system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}}
	if err := sender.SendToMachine("ve_srv", map[string]any{"type": "ve:discussion_invite"}); err != nil {
		t.Fatalf("invite SendToMachine returned error: %v", err)
	}
	if err := sender.SendToMachine("ve_srv", map[string]any{"type": "ve:discussion_cancel"}); err != nil {
		t.Fatalf("cancel SendToMachine returned error: %v", err)
	}
	if runtimeCalls != 0 {
		t.Fatalf("MaClawSrv runtime should only execute discussion messages, got %d calls", runtimeCalls)
	}
	if fallback.calls != 0 {
		t.Fatalf("headless MaClawSrv employees should not use GUI websocket fallback, got %d calls", fallback.calls)
	}
}

func TestTrustedInviteAutoAcceptsMacLawSrvPlatformEmployee(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":{"content":"ok"}}`))
	}))
	defer runtime.Close()

	provider := platformProviderEntry{PlatformID: maclawSrvRuntimePlatformID, RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: runtime.URL, AdminSecret: "runtime-secret", TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "runtime-machine-1", PlatformID: maclawSrvRuntimePlatformID, PlatformEmployeeID: "platform-employee-1", RuntimeProviderID: maclawSrvRuntimePlatformID, OwnerUserID: "hub-account-1", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	groupSvc := NewGroupDiscussionService()
	session, err := groupSvc.CreateSession("tenant-a", CreateSessionRequest{Topic: "Direct", Goal: "hello", Participants: []corea2a.Participant{{ID: "maclaw-gui", RoleCode: "initiator"}}, DecisionPolicy: corea2a.PolicyMajority})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	fallback := &captureGroupDiscussionSender{}
	handler := NewGroupDiscussionHandler(groupSvc, platformAwareMachineSender{fallback: fallback, system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}})
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+session.ID+"/invites", `{"from_id":"maclaw-gui","to_id":"runtime-machine-1","role":"speak","trusted":true}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("invite status=%d body=%s", w.Code, w.Body.String())
	}
	detail, err := groupSvc.GetDiscussionDetail("tenant-a", session.ID)
	if err != nil {
		t.Fatalf("GetDiscussionDetail: %v", err)
	}
	if role := participantRole(detail.Session, "runtime-machine-1"); role != "speak" {
		t.Fatalf("platform invite should auto-join as speak, got role %q participants=%#v", role, detail.Session.Participants)
	}
	if messages := fallback.snapshotMessages(); len(messages) != 0 {
		t.Fatalf("auto-accepted platform invite should not use GUI fallback, got %#v", messages)
	}
}

func TestTrustedInviteRejectsMissingMacLawSrvRuntimeEmployee(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/platform/runtime/report" {
			t.Fatalf("unexpected runtime path %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "users": []map[string]any{}})
	}))
	defer runtime.Close()
	provider := platformProviderEntry{PlatformID: maclawSrvRuntimePlatformID, RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: runtime.URL, AdminSecret: "runtime-secret", TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_deleted", MachineID: "runtime-machine-deleted", PlatformID: maclawSrvRuntimePlatformID, PlatformEmployeeID: "deleted-employee", RuntimeProviderID: maclawSrvRuntimePlatformID, OwnerUserID: "hub-account-1", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	groupSvc := NewGroupDiscussionService()
	session, err := groupSvc.CreateSession("tenant-a", CreateSessionRequest{Topic: "Direct", Goal: "hello", Participants: []corea2a.Participant{{ID: "maclaw-gui", RoleCode: "initiator"}}, DecisionPolicy: corea2a.PolicyMajority})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	handler := NewGroupDiscussionHandler(groupSvc, platformAwareMachineSender{system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}})
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+session.ID+"/invites", `{"from_id":"maclaw-gui","to_id":"runtime-machine-deleted","role":"speak","trusted":true}`))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "not active") {
		t.Fatalf("invite status=%d body=%s", w.Code, w.Body.String())
	}
	if invites := groupSvc.ListInvitations("tenant-a", "runtime-machine-deleted", "pending"); len(invites) != 0 {
		t.Fatalf("missing runtime invite should not remain pending, got %#v", invites)
	}
}

func TestTrustedInviteRejectsPlatformEmployeeWithoutRuntime(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	provider := platformProviderEntry{PlatformID: maclawSrvRuntimePlatformID, RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "runtime-machine-1", PlatformID: maclawSrvRuntimePlatformID, PlatformEmployeeID: "platform-employee-1", RuntimeProviderID: maclawSrvRuntimePlatformID, OwnerUserID: "hub-account-1", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	groupSvc := NewGroupDiscussionService()
	session, err := groupSvc.CreateSession("tenant-a", CreateSessionRequest{Topic: "Direct", Goal: "hello", Participants: []corea2a.Participant{{ID: "maclaw-gui", RoleCode: "initiator"}}, DecisionPolicy: corea2a.PolicyMajority})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	handler := NewGroupDiscussionHandler(groupSvc, platformAwareMachineSender{system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}})
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+session.ID+"/invites", `{"from_id":"maclaw-gui","to_id":"runtime-machine-1","role":"speak","trusted":true}`))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "runtime is not configured") {
		t.Fatalf("invite status=%d body=%s", w.Code, w.Body.String())
	}
	if invites := groupSvc.ListInvitations("tenant-a", "runtime-machine-1", "pending"); len(invites) != 0 {
		t.Fatalf("missing runtime invite should not remain pending, got %#v", invites)
	}
}

func TestUntrustedInviteRejectsMacLawSrvPlatformEmployee(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer runtime.Close()
	provider := platformProviderEntry{PlatformID: maclawSrvRuntimePlatformID, RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: runtime.URL, AdminSecret: "runtime-secret", TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "runtime-machine-1", PlatformID: maclawSrvRuntimePlatformID, PlatformEmployeeID: "platform-employee-1", RuntimeProviderID: maclawSrvRuntimePlatformID, OwnerUserID: "hub-account-1", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	groupSvc := NewGroupDiscussionService()
	session, err := groupSvc.CreateSession("tenant-a", CreateSessionRequest{Topic: "Direct", Goal: "hello", Participants: []corea2a.Participant{{ID: "maclaw-gui", RoleCode: "initiator"}}, DecisionPolicy: corea2a.PolicyMajority})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	handler := NewGroupDiscussionHandler(groupSvc, platformAwareMachineSender{system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}})
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+session.ID+"/invites", `{"from_id":"maclaw-gui","to_id":"runtime-machine-1","role":"speak"}`))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "trusted invitation is required") {
		t.Fatalf("invite status=%d body=%s", w.Code, w.Body.String())
	}
	if invites := groupSvc.ListInvitations("tenant-a", "runtime-machine-1", "pending"); len(invites) != 0 {
		t.Fatalf("untrusted platform invite should not remain pending, got %#v", invites)
	}
	invites := groupSvc.ListInvitations("tenant-a", "runtime-machine-1", "reject")
	if len(invites) != 1 {
		t.Fatalf("untrusted platform invite should be marked rejected, got %#v", invites)
	}
}

func TestGroupDiscussionMessageRoutesPlatformEmployeeToMacLawSrvRuntime(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	seen := make(chan map[string]any, 1)
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/platform/runtime/report" {
			writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "users": []map[string]any{{"employee_id": "platform-employee-1", "runtime_status": "ready"}}})
			return
		}
		if r.URL.Path != "/api/runtime/virtual-employees/platform-employee-1/discussion-messages" {
			t.Fatalf("unexpected runtime path %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode runtime body: %v", err)
		}
		seen <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":{"content":"reply from runtime"}}`))
	}))
	defer runtime.Close()
	provider := platformProviderEntry{PlatformID: "platform-1", RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: runtime.URL, AdminSecret: "runtime-secret", TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "runtime-machine-1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", RuntimeProviderID: maclawSrvRuntimePlatformID, OwnerUserID: "hub-account-1", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	groupSvc := NewGroupDiscussionService()
	session, err := groupSvc.CreateSession("tenant-a", CreateSessionRequest{Topic: "Direct", Goal: "hello", Participants: []corea2a.Participant{{ID: "maclaw-gui", RoleCode: "initiator"}, {ID: "ve_employee_1", RoleCode: "speak"}}, DecisionPolicy: corea2a.PolicyMajority})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	fallback := &captureGroupDiscussionSender{}
	handler := NewGroupDiscussionHandler(groupSvc, platformAwareMachineSender{fallback: fallback, system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}})
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+session.ID+"/messages", `{"id":"hub-msg-platform-reply","from_id":"maclaw-gui","content":"hello platform employee"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("group message status=%d body=%s", w.Code, w.Body.String())
	}
	select {
	case body := <-seen:
		content := fmt.Sprint(body["content"])
		if !strings.Contains(content, "当前消息来自 maclaw-gui: hello platform employee") || body["hub_message_id"] != "hub-msg-platform-reply" {
			t.Fatalf("unexpected runtime payload: %#v", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for MaClawSrv runtime")
	}
	detail := waitForDiscussionMessages(t, groupSvc, "tenant-a", session.ID, 2, func(messages []corea2a.Message) bool {
		return len(messages) >= 2 && messages[1].FromID == "ve_employee_1" && messages[1].Content == "reply from runtime"
	})
	if len(detail.Messages) != 2 || detail.Messages[1].FromID != "ve_employee_1" || detail.Messages[1].Content != "reply from runtime" {
		t.Fatalf("unexpected runtime reply messages: %#v", detail.Messages)
	}
	fallbackMessages := waitForFallbackMessages(t, fallback, 1, func(messages []sentGroupDiscussionMessage) bool {
		return len(messages) == 1 && messages[0].machineID == "maclaw-gui"
	})
	if len(fallbackMessages) != 1 || fallbackMessages[0].machineID != "maclaw-gui" {
		t.Fatalf("runtime reply should notify GUI through fallback only, got %#v", fallbackMessages)
	}
}

func TestGroupDiscussionMessageIncludesSharedContextForPlatformRuntime(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	seen := make(chan map[string]any, 1)
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/platform/runtime/report" {
			writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "users": []map[string]any{{"employee_id": "platform-employee-1", "runtime_status": "ready"}}})
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode runtime body: %v", err)
		}
		seen <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":{"content":"context-aware reply"}}`))
	}))
	defer runtime.Close()
	provider := platformProviderEntry{PlatformID: "platform-1", RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: runtime.URL, AdminSecret: "runtime-secret", TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "runtime-machine-1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", RuntimeProviderID: maclawSrvRuntimePlatformID, OwnerUserID: "hub-account-1", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	groupSvc := NewGroupDiscussionService()
	session, err := groupSvc.CreateSession("tenant-a", CreateSessionRequest{Topic: "US travel", Goal: "Discuss options", Participants: []corea2a.Participant{{ID: "maclaw-gui", RoleCode: "initiator"}, {ID: "anna-machine", RoleCode: "speak"}, {ID: "ve_employee_1", RoleCode: "speak"}}, DecisionPolicy: corea2a.PolicyMajority})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := groupSvc.AddDiscussionMessage("tenant-a", session.ID, corea2a.GroupDiscussionMessage{FromID: "anna-machine", Kind: corea2a.MessageAnswer, Content: "先搞定签证，再考虑机票。"}); err != nil {
		t.Fatalf("seed message: %v", err)
	}
	handler := NewGroupDiscussionHandler(groupSvc, platformAwareMachineSender{system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}})
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+session.ID+"/messages", `{"id":"hub-msg-context","from_id":"maclaw-gui","to_ids":["ve_employee_1"],"content":"@小妍 你有啥看法？"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("group message status=%d body=%s", w.Code, w.Body.String())
	}
	select {
	case body := <-seen:
		content := fmt.Sprint(body["content"])
		if !strings.Contains(content, "多人 Hub 群聊") || !strings.Contains(content, "先搞定签证") || !strings.Contains(content, "当前消息来自 maclaw-gui") {
			t.Fatalf("runtime content missing shared context: %s", content)
		}
		if !strings.Contains(content, "Participants:") || !strings.Contains(content, "- anna-machine (speak)") || !strings.Contains(content, "- ve_employee_1 (speak)") {
			t.Fatalf("runtime content missing participant roster: %s", content)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for MaClawSrv runtime")
	}
}

func TestGroupDiscussionMessagePersistsRepliesFromMultiplePlatformEmployees(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/platform/runtime/report" {
			writeJSON(w, http.StatusOK, map[string]any{
				"status": "ok",
				"users": []map[string]any{
					{"employee_id": "platform-employee-1", "runtime_status": "ready"},
					{"employee_id": "platform-employee-2", "runtime_status": "ready"},
				},
			})
			return
		}
		employeeID := strings.TrimPrefix(r.URL.Path, "/api/runtime/virtual-employees/")
		employeeID = strings.TrimSuffix(employeeID, "/discussion-messages")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":{"content":"reply from ` + employeeID + `"}}`))
	}))
	defer runtime.Close()
	provider := platformProviderEntry{PlatformID: "platform-1", RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: runtime.URL, AdminSecret: "runtime-secret", TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{
		{ID: "ve_employee_1", MachineID: "runtime-machine-1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", RuntimeProviderID: maclawSrvRuntimePlatformID, OwnerUserID: "hub-account-1", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline},
		{ID: "ve_employee_2", MachineID: "runtime-machine-2", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-2", RuntimeProviderID: maclawSrvRuntimePlatformID, OwnerUserID: "hub-account-2", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline},
	}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	groupSvc := NewGroupDiscussionService()
	session, err := groupSvc.CreateSession("tenant-a", CreateSessionRequest{Topic: "Direct", Goal: "hello", Participants: []corea2a.Participant{{ID: "maclaw-gui", RoleCode: "initiator"}, {ID: "ve_employee_1", RoleCode: "speak"}, {ID: "ve_employee_2", RoleCode: "speak"}}, DecisionPolicy: corea2a.PolicyMajority})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	fallback := &captureGroupDiscussionSender{}
	handler := NewGroupDiscussionHandler(groupSvc, platformAwareMachineSender{fallback: fallback, system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}})
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+session.ID+"/messages", `{"id":"hub-msg-multi-runtime","from_id":"maclaw-gui","content":"hello platform employees"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("group message status=%d body=%s", w.Code, w.Body.String())
	}
	detail := waitForDiscussionMessages(t, groupSvc, "tenant-a", session.ID, 3, func(messages []corea2a.Message) bool {
		seen := map[string]bool{}
		for _, msg := range messages {
			seen[msg.Content] = true
		}
		return seen["reply from platform-employee-1"] && seen["reply from platform-employee-2"]
	})
	ids := map[string]bool{}
	for _, msg := range detail.Messages {
		if msg.FromID == "ve_employee_1" || msg.FromID == "ve_employee_2" {
			if ids[msg.ID] {
				t.Fatalf("runtime replies must use unique ids, duplicate %s in %#v", msg.ID, detail.Messages)
			}
			ids[msg.ID] = true
		}
	}
	if len(ids) != 2 {
		t.Fatalf("expected two platform replies, got messages %#v", detail.Messages)
	}
	waitForFallbackMessages(t, fallback, 2, nil)
}

func TestGroupDiscussionStreamMessagesDoNotExecutePlatformRuntime(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	runtimeCalls := make(chan string, 2)
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimeCalls <- r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":{"content":"should not run"}}`))
	}))
	defer runtime.Close()
	provider := platformProviderEntry{PlatformID: "platform-1", RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: runtime.URL, AdminSecret: "runtime-secret", TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "runtime-machine-1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", RuntimeProviderID: maclawSrvRuntimePlatformID, OwnerUserID: "hub-account-1", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	groupSvc := NewGroupDiscussionService()
	session, err := groupSvc.CreateSession("tenant-a", CreateSessionRequest{Topic: "Direct", Goal: "hello", Participants: []corea2a.Participant{{ID: "maclaw-gui", RoleCode: "initiator"}, {ID: "ve_employee_1", RoleCode: "speak"}}, DecisionPolicy: corea2a.PolicyMajority})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	handler := NewGroupDiscussionHandler(groupSvc, platformAwareMachineSender{system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}})
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)

	for _, body := range []string{
		`{"id":"hub-msg-stream-chunk","from_id":"maclaw-gui","kind":"stream_chunk","content":"partial"}`,
		`{"id":"hub-msg-stream-end","from_id":"maclaw-gui","kind":"stream_end"}`,
	} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+session.ID+"/messages", body))
		if w.Code != http.StatusOK {
			t.Fatalf("group message status=%d body=%s", w.Code, w.Body.String())
		}
	}

	select {
	case path := <-runtimeCalls:
		t.Fatalf("stream messages must not execute platform runtime, got %s", path)
	case <-time.After(150 * time.Millisecond):
	}
	detail, err := groupSvc.GetDiscussionDetail("tenant-a", session.ID)
	if err != nil {
		t.Fatalf("GetDiscussionDetail: %v", err)
	}
	if len(detail.Messages) != 2 || detail.Messages[0].Kind != corea2a.MessageStreamChunk || detail.Messages[1].Kind != corea2a.MessageStreamEnd {
		t.Fatalf("stream messages should still persist, got %#v", detail.Messages)
	}
}

func TestMacLawSrvDiscussionContextCoalescesSharedStreamHistory(t *testing.T) {
	session := &corea2a.Session{
		Topic:          "Shared topic",
		ContextSummary: "[compressed shared group memory]\n- [anna] older fact",
		Participants: []corea2a.Participant{
			{ID: "maclaw-gui", RoleCode: "initiator"},
			{ID: "ve_employee_1", RoleCode: "speak"},
			{ID: "ve_employee_2", RoleCode: "speak"},
		},
		Messages: []corea2a.Message{
			{ID: "m1", FromID: "ve_employee_1", Kind: corea2a.MessageStreamChunk, Content: "Anna knows "},
			{ID: "m2", FromID: "ve-employee-1", Kind: corea2a.MessageStreamChunk, Content: "the prior plan."},
			{ID: "m3", FromID: "ve_employee_1", Kind: corea2a.MessageStreamEnd},
			{ID: "m4", FromID: "maclaw-gui", Kind: corea2a.MessageStatement, Content: "@Xiaoyan continue from Anna"},
		},
	}

	got := macLawSrvDiscussionContext(session, corea2a.GroupDiscussionMessage{ID: "m4", FromID: "maclaw-gui", Kind: corea2a.MessageStatement, Content: "@Xiaoyan continue from Anna"}, "ve_employee_2")
	if !strings.Contains(got, "[ve_employee_1] Anna knows the prior plan.") {
		t.Fatalf("context should coalesce prior stream chunks: %q", got)
	}
	if !strings.Contains(got, "older fact") {
		t.Fatalf("context should include compressed memory: %q", got)
	}
	if strings.Contains(got, "@Xiaoyan continue from Anna\n[maclaw-gui]") || strings.Contains(got, "[maclaw-gui] @Xiaoyan continue from Anna") {
		t.Fatalf("context should not duplicate current targeted input in recent history: %q", got)
	}
}

func TestMacLawSrvDiscussionRecentContextKeepsMentionedMessagesVisible(t *testing.T) {
	messages := []corea2a.Message{
		{ID: "m1", FromID: "maclaw-gui", Kind: corea2a.MessageStatement, Content: "@Anna summarize contract", ToIDs: []string{"ve_employee_1"}},
		{ID: "m2", FromID: "ve_employee_1", Kind: corea2a.MessageStatement, Content: "Key risk is renewal."},
		{ID: "m3", FromID: "maclaw-gui", Kind: corea2a.MessageStatement, Content: "@Xiaoyan check Anna's point", ToIDs: []string{"ve_employee_2"}},
	}

	got := strings.Join(macLawSrvDiscussionRecentContext(messages, corea2a.GroupDiscussionMessage{ID: "m3", Content: "@Xiaoyan check Anna's point"}), "\n")
	if !strings.Contains(got, "@Anna summarize contract") || !strings.Contains(got, "Key risk is renewal.") {
		t.Fatalf("@ targeted messages should remain shared context: %q", got)
	}
	if strings.Contains(got, "@Xiaoyan check Anna's point") {
		t.Fatalf("current message should not be duplicated in recent context: %q", got)
	}
}

func TestMacLawSrvDiscussionRecentContextKeepsOlderRepeatedInput(t *testing.T) {
	messages := []corea2a.Message{
		{ID: "m1", FromID: "maclaw-gui", Kind: corea2a.MessageStatement, Content: "repeat request"},
		{ID: "m2", FromID: "ve_employee_1", Kind: corea2a.MessageStatement, Content: "answer between repeats"},
		{ID: "m3", FromID: "maclaw-gui", Kind: corea2a.MessageStatement, Content: "repeat request"},
	}

	got := strings.Join(macLawSrvDiscussionRecentContext(messages, corea2a.GroupDiscussionMessage{FromID: "maclaw-gui", Kind: corea2a.MessageStatement, Content: "repeat request"}), "\n")
	if strings.Count(got, "repeat request") != 1 {
		t.Fatalf("context should skip only current repeated input: %q", got)
	}
	if !strings.Contains(got, "answer between repeats") {
		t.Fatalf("context should keep intervening message: %q", got)
	}
}

func TestMacLawSrvDiscussionContextUsesOnlyPostSummaryRecentMessages(t *testing.T) {
	session := &corea2a.Session{
		ContextSummary: "[compressed shared group memory]\n- [anna] summarized old fact",
		SummaryUpToID:  "m2",
		Messages: []corea2a.Message{
			{ID: "m1", FromID: "anna", Kind: corea2a.MessageStatement, Content: "old detail one"},
			{ID: "m2", FromID: "anna", Kind: corea2a.MessageStatement, Content: "old detail two"},
			{ID: "m3", FromID: "anna", Kind: corea2a.MessageStatement, Content: "recent detail"},
		},
	}

	got := macLawSrvDiscussionContext(session, corea2a.GroupDiscussionMessage{FromID: "maclaw-gui", Content: "continue"}, "ve_employee_1")
	if strings.Contains(got, "old detail one") || strings.Contains(got, "old detail two") {
		t.Fatalf("context should not repeat summarized raw messages: %q", got)
	}
	if !strings.Contains(got, "summarized old fact") || !strings.Contains(got, "recent detail") {
		t.Fatalf("context should include summary and post-summary recent messages: %q", got)
	}
}

func TestGroupDiscussionMessageReturnsBeforeSlowMacLawSrvRuntimeReply(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	called := make(chan struct{}, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/platform/runtime/report" {
			writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "users": []map[string]any{{"employee_id": "platform-employee-1", "runtime_status": "ready"}}})
			return
		}
		called <- struct{}{}
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":{"content":"slow runtime reply"}}`))
	}))
	defer runtime.Close()
	defer func() {
		releaseOnce.Do(func() { close(release) })
	}()
	provider := platformProviderEntry{PlatformID: "platform-1", RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: runtime.URL, AdminSecret: "runtime-secret", TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "runtime-machine-1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", RuntimeProviderID: maclawSrvRuntimePlatformID, OwnerUserID: "hub-account-1", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	groupSvc := NewGroupDiscussionService()
	session, err := groupSvc.CreateSession("tenant-a", CreateSessionRequest{Topic: "Direct", Goal: "hello", Participants: []corea2a.Participant{{ID: "maclaw-gui", RoleCode: "initiator"}, {ID: "ve_employee_1", RoleCode: "speak"}}, DecisionPolicy: corea2a.PolicyMajority})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	handler := NewGroupDiscussionHandler(groupSvc, platformAwareMachineSender{system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}})
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)
	started := time.Now()
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+session.ID+"/messages", `{"id":"hub-msg-slow-runtime","from_id":"maclaw-gui","content":"hello slow runtime"}`))
	elapsed := time.Since(started)
	if w.Code != http.StatusOK {
		t.Fatalf("group message status=%d body=%s", w.Code, w.Body.String())
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("Hub message POST waited for runtime response: %s", elapsed)
	}
	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("MaClawSrv runtime was not called")
	}
	releaseOnce.Do(func() { close(release) })
	waitForDiscussionMessages(t, groupSvc, "tenant-a", session.ID, 2, func(messages []corea2a.Message) bool {
		return len(messages) >= 2 && messages[1].Content == "slow runtime reply"
	})
}

func TestPlatformRuntimeDeliveryLimiterRejectsWhenFull(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	runtimeCalled := false
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/platform/runtime/report" {
			writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "users": []map[string]any{{"employee_id": "platform-employee-1", "runtime_status": "ready"}}})
			return
		}
		runtimeCalled = true
		writeJSON(w, http.StatusOK, map[string]any{"message": map[string]any{"content": "should not run"}})
	}))
	defer runtime.Close()
	provider := platformProviderEntry{PlatformID: "platform-1", RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: runtime.URL, AdminSecret: "runtime-secret", TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "runtime-machine-1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", RuntimeProviderID: maclawSrvRuntimePlatformID, OwnerUserID: "hub-account-1", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	veRuntimeDeliverySemaphore = make(chan struct{}, veRuntimeDeliveryConcurrency)
	for i := 0; i < veRuntimeDeliveryConcurrency; i++ {
		veRuntimeDeliverySemaphore <- struct{}{}
	}
	defer func() { veRuntimeDeliverySemaphore = make(chan struct{}, veRuntimeDeliveryConcurrency) }()
	before := globalVEMetrics.snapshot().RuntimeDelivery

	sender := platformAwareMachineSender{system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}}
	session := &corea2a.Session{ID: "session-runtime-busy", TenantID: "tenant-a", Participants: []corea2a.Participant{{ID: "maclaw-gui", RoleCode: "initiator"}, {ID: "ve_employee_1", RoleCode: "speak"}}}
	handled, reply, err := sender.SendDiscussionMessage(session, corea2a.GroupDiscussionMessage{ID: "msg-runtime-busy", FromID: "maclaw-gui", Kind: corea2a.MessageStatement, Content: "hello"}, corea2a.Participant{ID: "ve_employee_1", RoleCode: "speak"})
	if !handled || reply != nil || err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("handled=%v reply=%#v err=%v, want busy handled error", handled, reply, err)
	}
	if runtimeCalled {
		t.Fatal("runtime message endpoint should not be called when limiter is full")
	}
	after := globalVEMetrics.snapshot().RuntimeDelivery
	if got := veMetricUint(after, "rejected_total") - veMetricUint(before, "rejected_total"); got != 1 {
		t.Fatalf("runtime rejected delta=%d, want 1; metrics=%#v", got, after)
	}
}

func TestGroupDiscussionMessageReturnsServiceUnavailableWhenRuntimeBusy(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/platform/runtime/report" {
			writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "users": []map[string]any{{"employee_id": "platform-employee-1", "runtime_status": "ready"}}})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"message": map[string]any{"content": "should not run"}})
	}))
	defer runtime.Close()
	provider := platformProviderEntry{PlatformID: "platform-1", RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: runtime.URL, AdminSecret: "runtime-secret", TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "runtime-machine-1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", RuntimeProviderID: maclawSrvRuntimePlatformID, OwnerUserID: "hub-account-1", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	veRuntimeDeliverySemaphore = make(chan struct{}, veRuntimeDeliveryConcurrency)
	for i := 0; i < veRuntimeDeliveryConcurrency; i++ {
		veRuntimeDeliverySemaphore <- struct{}{}
	}
	defer func() { veRuntimeDeliverySemaphore = make(chan struct{}, veRuntimeDeliveryConcurrency) }()
	groupSvc := NewGroupDiscussionService()
	session, err := groupSvc.CreateSession("tenant-a", CreateSessionRequest{Topic: "Runtime busy", Participants: []corea2a.Participant{{ID: "maclaw-gui", RoleCode: "initiator"}, {ID: "ve_employee_1", RoleCode: "speak"}}, DecisionPolicy: corea2a.PolicyMajority})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	handler := NewGroupDiscussionHandler(groupSvc, platformAwareMachineSender{system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}})
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+session.ID+"/messages", `{"id":"hub-msg-runtime-busy","from_id":"maclaw-gui","content":"hello"}`))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("group message status=%d body=%s", w.Code, w.Body.String())
	}
	if w.Header().Get("Retry-After") != "1" || !bytes.Contains(w.Body.Bytes(), []byte("VE_RUNTIME_BUSY")) {
		t.Fatalf("unexpected busy response headers=%v body=%s", w.Header(), w.Body.String())
	}
}

func TestGroupDiscussionMessageReturnsServiceUnavailableWhenRuntimeCircuitOpen(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/platform/runtime/report" {
			writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "users": []map[string]any{{"employee_id": "platform-employee-1", "runtime_status": "ready"}}})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"message": map[string]any{"content": "should not run"}})
	}))
	defer runtime.Close()
	provider := platformProviderEntry{PlatformID: "platform-1", RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	runtimeEntry := macLawSrvRuntimeEntry{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: runtime.URL, AdminSecret: "runtime-secret", TenantIDs: []string{"tenant-a"}}
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{runtimeEntry}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	employee := digitalEmployeeEntry{ID: "ve_employee_1", MachineID: "runtime-machine-1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", RuntimeProviderID: maclawSrvRuntimePlatformID, OwnerUserID: "hub-account-1", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{employee}}); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	circuitKey := veRuntimeDeliveryCircuitKey("tenant-a", employee, runtimeEntry)
	veRuntimeDeliveryCircuit.Lock()
	veRuntimeDeliveryCircuit.Entries = map[string]veRuntimeDeliveryCircuitEntry{circuitKey: {Failures: veRuntimeCircuitFailureLimit, OpenUntil: time.Now().Add(veRuntimeCircuitOpenDuration)}}
	veRuntimeDeliveryCircuit.Unlock()
	groupSvc := NewGroupDiscussionService()
	session, err := groupSvc.CreateSession("tenant-a", CreateSessionRequest{Topic: "Runtime circuit", Participants: []corea2a.Participant{{ID: "maclaw-gui", RoleCode: "initiator"}, {ID: "ve_employee_1", RoleCode: "speak"}}, DecisionPolicy: corea2a.PolicyMajority})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	handler := NewGroupDiscussionHandler(groupSvc, platformAwareMachineSender{system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}})
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+session.ID+"/messages", `{"id":"hub-msg-runtime-circuit-open","from_id":"maclaw-gui","content":"hello"}`))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("group message status=%d body=%s", w.Code, w.Body.String())
	}
	if w.Header().Get("Retry-After") != "5" || !bytes.Contains(w.Body.Bytes(), []byte("VE_RUNTIME_CIRCUIT_OPEN")) {
		t.Fatalf("unexpected circuit response headers=%v body=%s", w.Header(), w.Body.String())
	}
}

func TestPlatformRuntimeDeliveryCircuitOpensAfterFailures(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	runtimeCalls := 0
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/platform/runtime/report" {
			writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "users": []map[string]any{{"employee_id": "platform-employee-1", "runtime_status": "ready"}}})
			return
		}
		runtimeCalls++
		http.Error(w, "runtime down", http.StatusBadGateway)
	}))
	defer runtime.Close()
	provider := platformProviderEntry{PlatformID: "platform-1", RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: runtime.URL, AdminSecret: "runtime-secret", TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "runtime-machine-1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", RuntimeProviderID: maclawSrvRuntimePlatformID, OwnerUserID: "hub-account-1", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	veRuntimeDeliveryCircuit.Lock()
	veRuntimeDeliveryCircuit.Entries = map[string]veRuntimeDeliveryCircuitEntry{}
	veRuntimeDeliveryCircuit.Unlock()
	before := globalVEMetrics.snapshot().RuntimeDelivery
	sender := platformAwareMachineSender{system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}}
	session := &corea2a.Session{ID: "session-runtime-circuit", TenantID: "tenant-a", Participants: []corea2a.Participant{{ID: "maclaw-gui", RoleCode: "initiator"}, {ID: "ve_employee_1", RoleCode: "speak"}}}
	msg := corea2a.GroupDiscussionMessage{ID: "msg-runtime-circuit", FromID: "maclaw-gui", Kind: corea2a.MessageStatement, Content: "hello"}
	for i := 0; i < veRuntimeCircuitFailureLimit; i++ {
		handled, _, err := sender.SendDiscussionMessage(session, msg, corea2a.Participant{ID: "ve_employee_1", RoleCode: "speak"})
		if !handled || err == nil {
			t.Fatalf("attempt %d handled=%v err=%v, want runtime failure", i+1, handled, err)
		}
	}
	handled, _, err := sender.SendDiscussionMessage(session, msg, corea2a.Participant{ID: "ve_employee_1", RoleCode: "speak"})
	if !handled || err == nil || !strings.Contains(err.Error(), "circuit is open") {
		t.Fatalf("circuit attempt handled=%v err=%v, want open circuit", handled, err)
	}
	if runtimeCalls != veRuntimeCircuitFailureLimit {
		t.Fatalf("runtime calls=%d, want %d; open circuit should skip runtime", runtimeCalls, veRuntimeCircuitFailureLimit)
	}
	after := globalVEMetrics.snapshot().RuntimeDelivery
	if got := veMetricUint(after, "circuit_open_total") - veMetricUint(before, "circuit_open_total"); got != 1 {
		t.Fatalf("circuit_open delta=%d, want 1; metrics=%#v", got, after)
	}
	if got := veMetricUint(after, "http_status_failed_total") - veMetricUint(before, "http_status_failed_total"); got != uint64(veRuntimeCircuitFailureLimit) {
		t.Fatalf("http_status_failed delta=%d, want %d; metrics=%#v", got, veRuntimeCircuitFailureLimit, after)
	}
	if got := veMetricUint(after, "circuit_rejected_total") - veMetricUint(before, "circuit_rejected_total"); got != 1 {
		t.Fatalf("circuit_rejected delta=%d, want 1; metrics=%#v", got, after)
	}
}

func TestPlatformRuntimeDeliveryCountsEmptyReplyFailure(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/platform/runtime/report" {
			writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "users": []map[string]any{{"employee_id": "platform-employee-empty", "runtime_status": "ready"}}})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"done\":true,\"content\":\"   \"}\n\n"))
	}))
	defer runtime.Close()
	provider := platformProviderEntry{PlatformID: "platform-empty", RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: runtime.URL, AdminSecret: "runtime-secret", TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_empty", MachineID: "runtime-machine-empty", PlatformID: "platform-empty", PlatformEmployeeID: "platform-employee-empty", RuntimeProviderID: maclawSrvRuntimePlatformID, OwnerUserID: "hub-account-1", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	veRuntimeDeliveryCircuit.Lock()
	veRuntimeDeliveryCircuit.Entries = map[string]veRuntimeDeliveryCircuitEntry{}
	veRuntimeDeliveryCircuit.Unlock()
	before := globalVEMetrics.snapshot().RuntimeDelivery
	sender := platformAwareMachineSender{system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}}
	session := &corea2a.Session{ID: "session-runtime-empty", TenantID: "tenant-a", Participants: []corea2a.Participant{{ID: "maclaw-gui", RoleCode: "initiator"}, {ID: "ve_employee_empty", RoleCode: "speak"}}}

	handled, reply, err := sender.SendDiscussionMessage(session, corea2a.GroupDiscussionMessage{ID: "msg-runtime-empty", FromID: "maclaw-gui", Kind: corea2a.MessageStatement, Content: "hello"}, corea2a.Participant{ID: "ve_employee_empty", RoleCode: "speak"})
	if !handled || reply != nil || err == nil || !strings.Contains(err.Error(), "assistant content") {
		t.Fatalf("handled=%v reply=%#v err=%v, want empty reply error", handled, reply, err)
	}
	after := globalVEMetrics.snapshot().RuntimeDelivery
	if got := veMetricUint(after, "empty_reply_total") - veMetricUint(before, "empty_reply_total"); got != 1 {
		t.Fatalf("empty_reply delta=%d, want 1; metrics=%#v", got, after)
	}
	if got := veMetricUint(after, "failed_total") - veMetricUint(before, "failed_total"); got != 1 {
		t.Fatalf("failed delta=%d, want 1; metrics=%#v", got, after)
	}
}

func TestPlatformRuntimeDeliveryClassifiesEventStreamHTTPStatusFailure(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/platform/runtime/report" {
			writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "users": []map[string]any{{"employee_id": "platform-employee-sse-fail", "runtime_status": "ready"}}})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("data: {\"error\":\"runtime down\"}\n\n"))
	}))
	defer runtime.Close()
	provider := platformProviderEntry{PlatformID: "platform-sse-fail", RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: runtime.URL, AdminSecret: "runtime-secret", TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_sse_fail", MachineID: "runtime-machine-sse-fail", PlatformID: "platform-sse-fail", PlatformEmployeeID: "platform-employee-sse-fail", RuntimeProviderID: maclawSrvRuntimePlatformID, OwnerUserID: "hub-account-1", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	veRuntimeDeliveryCircuit.Lock()
	veRuntimeDeliveryCircuit.Entries = map[string]veRuntimeDeliveryCircuitEntry{}
	veRuntimeDeliveryCircuit.Unlock()
	before := globalVEMetrics.snapshot().RuntimeDelivery
	sender := platformAwareMachineSender{system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}}
	session := &corea2a.Session{ID: "session-runtime-sse-fail", TenantID: "tenant-a", Participants: []corea2a.Participant{{ID: "maclaw-gui", RoleCode: "initiator"}, {ID: "ve_employee_sse_fail", RoleCode: "speak"}}}

	handled, reply, err := sender.SendDiscussionMessage(session, corea2a.GroupDiscussionMessage{ID: "msg-runtime-sse-fail", FromID: "maclaw-gui", Kind: corea2a.MessageStatement, Content: "hello"}, corea2a.Participant{ID: "ve_employee_sse_fail", RoleCode: "speak"})
	if !handled || reply != nil || err == nil || !strings.Contains(err.Error(), "returned status 502") {
		t.Fatalf("handled=%v reply=%#v err=%v, want HTTP status error", handled, reply, err)
	}
	after := globalVEMetrics.snapshot().RuntimeDelivery
	if got := veMetricUint(after, "http_status_failed_total") - veMetricUint(before, "http_status_failed_total"); got != 1 {
		t.Fatalf("http_status_failed delta=%d, want 1; metrics=%#v", got, after)
	}
}

func TestPlatformRuntimeDeliveryCircuitPrunesExpiredEntries(t *testing.T) {
	veRuntimeDeliveryCircuit.Lock()
	veRuntimeDeliveryCircuit.Entries = map[string]veRuntimeDeliveryCircuitEntry{
		"expired":       {Failures: veRuntimeCircuitFailureLimit, OpenUntil: time.Now().Add(-time.Second)},
		"active":        {Failures: veRuntimeCircuitFailureLimit, OpenUntil: time.Now().Add(time.Second)},
		"stale-failure": {Failures: 1, LastFailure: time.Now().Add(-veRuntimeCircuitFailureWindow - time.Second)},
		"fresh-failure": {Failures: 1, LastFailure: time.Now()},
	}
	veRuntimeDeliveryCircuit.Unlock()

	size := veRuntimeDeliveryCircuitSize()
	if size != 2 {
		t.Fatalf("circuit size=%d, want 2 active/fresh entries", size)
	}
	snapshot := globalVEMetrics.snapshot().RuntimeDelivery
	if got := veMetricUint(snapshot, "circuit_entries"); got != 2 {
		t.Fatalf("circuit_entries=%d, want 2; metrics=%#v", got, snapshot)
	}
}

func TestGroupDiscussionMessageRoutesMacLawSrvEmployeeDirectly(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	runtimePayloads := make(chan map[string]any, 2)
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/platform/runtime/report" {
			writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "users": []map[string]any{{"employee_id": "srv-user-1", "runtime_status": "ready"}}})
			return
		}
		if r.URL.Path != "/api/runtime/virtual-employees/srv-user-1/discussion-messages" {
			t.Fatalf("unexpected runtime path %s", r.URL.Path)
		}
		if got := r.Header.Get("X-MaClaw-Admin-Secret"); got != "srv-secret" {
			t.Fatalf("unexpected runtime secret %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode runtime body: %v", err)
		}
		runtimePayloads <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":{"content":"reply from headless maclaw"}}`))
	}))
	defer runtime.Close()
	provider := platformProviderEntry{PlatformID: maclawSrvRuntimePlatformID, CallbackBaseURL: runtime.URL, CallbackSecret: "srv-secret", RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: runtime.URL, AdminSecret: "srv-secret", TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_srv", MachineID: "ve_srv", PlatformID: maclawSrvRuntimePlatformID, PlatformEmployeeID: "srv-user-1", OwnerUserID: "hub-account-1", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	groupSvc := NewGroupDiscussionService()
	session, err := groupSvc.CreateSession("tenant-a", CreateSessionRequest{Topic: "Direct", Goal: "hello", Participants: []corea2a.Participant{{ID: "maclaw-gui", RoleCode: "initiator"}, {ID: "ve_srv", RoleCode: "speak"}}, DecisionPolicy: corea2a.PolicyMajority})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	fallback := &captureGroupDiscussionSender{}
	handler := NewGroupDiscussionHandler(groupSvc, platformAwareMachineSender{fallback: fallback, system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}})
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)
	body := `{"id":"hub-msg-direct-1","from_id":"maclaw-gui","content":"hello srv"}`
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+session.ID+"/messages", body))
	if w.Code != http.StatusOK {
		t.Fatalf("group message status=%d body=%s", w.Code, w.Body.String())
	}
	select {
	case body := <-runtimePayloads:
		content := fmt.Sprint(body["content"])
		if body["hub_discussion_id"] != session.ID || !strings.Contains(content, "hello srv") || body["hub_message_id"] == "" {
			t.Fatalf("unexpected runtime body: %#v", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("MaClawSrv runtime was not called")
	}
	detail := waitForDiscussionMessages(t, groupSvc, "tenant-a", session.ID, 2, func(messages []corea2a.Message) bool {
		return len(messages) >= 2 && messages[1].FromID == "ve_srv" && messages[1].Content == "reply from headless maclaw"
	})
	if len(detail.Messages) != 2 || detail.Messages[0].FromID != "maclaw-gui" || detail.Messages[1].FromID != "ve_srv" || detail.Messages[1].Content != "reply from headless maclaw" {
		t.Fatalf("unexpected direct runtime messages: %#v", detail.Messages)
	}
	fallbackMessages := waitForFallbackMessages(t, fallback, 1, func(messages []sentGroupDiscussionMessage) bool {
		return len(messages) == 1 && messages[0].machineID == "maclaw-gui"
	})
	if len(fallbackMessages) != 1 || fallbackMessages[0].machineID != "maclaw-gui" {
		t.Fatalf("runtime reply should notify GUI through fallback only, got %#v", fallbackMessages)
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+session.ID+"/messages", body))
	if w.Code != http.StatusOK {
		t.Fatalf("duplicate group message status=%d body=%s", w.Code, w.Body.String())
	}
	select {
	case body := <-runtimePayloads:
		t.Fatalf("duplicate Hub message should not call runtime again, got %#v", body)
	case <-time.After(150 * time.Millisecond):
	}
	detail, err = groupSvc.GetDiscussionDetail("tenant-a", session.ID)
	if err != nil {
		t.Fatalf("GetDiscussionDetail after duplicate: %v", err)
	}
	fallbackMessages = fallback.snapshotMessages()
	if len(detail.Messages) != 2 || len(fallbackMessages) != 1 {
		t.Fatalf("duplicate delivery should not duplicate messages or GUI notification, messages=%#v fallback=%#v", detail.Messages, fallbackMessages)
	}
}

func TestGroupDiscussionMessageRejectsMissingMacLawSrvRuntimeEmployee(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	deliveryCalls := 0
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/platform/runtime/report":
			writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "users": []map[string]any{}})
		case "/api/runtime/virtual-employees/deleted-employee/discussion-messages":
			deliveryCalls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"message":{"content":"should not be called"}}`))
		default:
			t.Fatalf("unexpected runtime path %s", r.URL.Path)
		}
	}))
	defer runtime.Close()
	provider := platformProviderEntry{PlatformID: maclawSrvRuntimePlatformID, CallbackBaseURL: runtime.URL, CallbackSecret: "srv-secret", RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: runtime.URL, AdminSecret: "srv-secret", TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_deleted", MachineID: "ve_deleted", PlatformID: maclawSrvRuntimePlatformID, PlatformEmployeeID: "deleted-employee", RuntimeProviderID: maclawSrvRuntimePlatformID, OwnerUserID: "hub-account-1", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	groupSvc := NewGroupDiscussionService()
	session, err := groupSvc.CreateSession("tenant-a", CreateSessionRequest{Topic: "Direct", Goal: "hello", Participants: []corea2a.Participant{{ID: "maclaw-gui", RoleCode: "initiator"}, {ID: "ve_deleted", RoleCode: "speak"}}, DecisionPolicy: corea2a.PolicyMajority})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	handler := NewGroupDiscussionHandler(groupSvc, platformAwareMachineSender{system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}})
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+session.ID+"/messages", `{"id":"hub-msg-deleted-runtime","from_id":"maclaw-gui","content":"hello deleted"}`))
	if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "not active") {
		t.Fatalf("group message status=%d body=%s", w.Code, w.Body.String())
	}
	if deliveryCalls != 0 {
		t.Fatalf("deleted runtime employee should not receive discussion delivery, got %d calls", deliveryCalls)
	}
}

func TestGroupDiscussionMessageBypassesPlatformForMacLawSrvRuntimeEmployee(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	platformCalls := 0
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		platformCalls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer platform.Close()
	runtimeCalls := 0
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/platform/runtime/report" {
			writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "users": []map[string]any{{"employee_id": "platform-employee-1", "runtime_status": "ready"}}})
			return
		}
		runtimeCalls++
		if r.URL.Path != "/api/runtime/virtual-employees/platform-employee-1/discussion-messages" {
			t.Fatalf("unexpected runtime path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":{"content":"runtime direct reply"}}`))
	}))
	defer runtime.Close()
	provider := platformProviderEntry{PlatformID: "platform-1", CallbackBaseURL: platform.URL, CallbackSecret: "platform-secret", RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: runtime.URL, AdminSecret: "runtime-secret", TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", RuntimeProviderID: maclawSrvRuntimePlatformID, OwnerUserID: "hub-account-1", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	groupSvc := NewGroupDiscussionService()
	session, err := groupSvc.CreateSession("tenant-a", CreateSessionRequest{Topic: "Direct", Goal: "hello", Participants: []corea2a.Participant{{ID: "maclaw-gui", RoleCode: "initiator"}, {ID: "ve_employee_1", RoleCode: "speak"}}, DecisionPolicy: corea2a.PolicyMajority})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	fallback := &captureGroupDiscussionSender{}
	handler := NewGroupDiscussionHandler(groupSvc, platformAwareMachineSender{fallback: fallback, system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}})
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+session.ID+"/messages", `{"id":"hub-msg-platform-managed","from_id":"maclaw-gui","content":"hello runtime"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("group message status=%d body=%s", w.Code, w.Body.String())
	}
	detail := waitForDiscussionMessages(t, groupSvc, "tenant-a", session.ID, 2, func(messages []corea2a.Message) bool {
		return len(messages) >= 2 && messages[1].Content == "runtime direct reply"
	})
	if runtimeCalls != 1 || platformCalls != 0 {
		t.Fatalf("expected direct runtime call only, runtime=%d platform=%d", runtimeCalls, platformCalls)
	}
	fallbackMessages := waitForFallbackMessages(t, fallback, 1, nil)
	if len(detail.Messages) != 2 || detail.Messages[1].Content != "runtime direct reply" || len(fallbackMessages) != 1 {
		t.Fatalf("unexpected direct runtime result messages=%#v fallback=%#v", detail.Messages, fallbackMessages)
	}
}

func TestGroupDiscussionMessageRoutesLegacyPlatformEmployeeToRuntimeWhenConfigured(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	platformCalls := 0
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		platformCalls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer platform.Close()
	runtimeCalls := 0
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimeCalls++
		if r.URL.Path != "/api/runtime/virtual-employees/platform-employee-legacy/discussion-messages" {
			t.Fatalf("unexpected runtime path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":{"content":"legacy runtime reply"}}`))
	}))
	defer runtime.Close()
	provider := platformProviderEntry{PlatformID: "platform-1", CallbackBaseURL: platform.URL, CallbackSecret: "platform-secret", RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: runtime.URL, AdminSecret: "runtime-secret", TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_legacy", MachineID: "ve_legacy", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-legacy", OwnerUserID: "hub-account-1", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	groupSvc := NewGroupDiscussionService()
	session, err := groupSvc.CreateSession("tenant-a", CreateSessionRequest{Topic: "Direct", Goal: "hello", Participants: []corea2a.Participant{{ID: "maclaw-gui", RoleCode: "initiator"}, {ID: "ve_legacy", RoleCode: "speak"}}, DecisionPolicy: corea2a.PolicyMajority})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	fallback := &captureGroupDiscussionSender{}
	handler := NewGroupDiscussionHandler(groupSvc, platformAwareMachineSender{fallback: fallback, system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}})
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+session.ID+"/messages", `{"id":"hub-msg-legacy-runtime","from_id":"maclaw-gui","content":"hello legacy"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("group message status=%d body=%s", w.Code, w.Body.String())
	}
	detail := waitForDiscussionMessages(t, groupSvc, "tenant-a", session.ID, 2, func(messages []corea2a.Message) bool {
		return len(messages) >= 2 && messages[1].Content == "legacy runtime reply"
	})
	if runtimeCalls != 1 || platformCalls != 0 {
		t.Fatalf("expected MaClawSrv runtime only, runtime=%d platform=%d", runtimeCalls, platformCalls)
	}
	fallbackMessages := waitForFallbackMessages(t, fallback, 1, nil)
	if len(detail.Messages) != 2 || detail.Messages[1].Content != "legacy runtime reply" || len(fallbackMessages) != 1 {
		t.Fatalf("unexpected runtime result messages=%#v fallback=%#v", detail.Messages, fallbackMessages)
	}
}

func TestGroupDiscussionMessageUsesSessionTenantForRuntimeLookup(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	runtimeACalls := make(chan string, 1)
	runtimeA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimeACalls <- r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":{"content":"tenant a reply"}}`))
	}))
	defer runtimeA.Close()
	runtimeBCalls := make(chan string, 1)
	runtimeB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/platform/runtime/report" {
			writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "users": []map[string]any{{"employee_id": "platform-employee-b", "runtime_status": "ready"}}})
			return
		}
		runtimeBCalls <- r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":{"content":"tenant b reply"}}`))
	}))
	defer runtimeB.Close()
	provider := platformProviderEntry{PlatformID: "platform-1", RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}, {HubTenantID: "tenant-b"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{
		{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: runtimeA.URL, AdminSecret: "runtime-a-secret", TenantIDs: []string{"tenant-a"}},
		{RuntimeID: maclawSrvRuntimePlatformID + "_b", BaseURL: runtimeB.URL, AdminSecret: "runtime-b-secret", TenantIDs: []string{"tenant-b"}},
	}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	registryA := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_shared", MachineID: "shared-machine", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-a", RuntimeProviderID: maclawSrvRuntimePlatformID, Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registryA); err != nil {
		t.Fatalf("save tenant-a registry: %v", err)
	}
	registryB := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_shared", MachineID: "shared-machine", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-b", RuntimeProviderID: maclawSrvRuntimePlatformID, Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-b", settings), registryB); err != nil {
		t.Fatalf("save tenant-b registry: %v", err)
	}
	groupSvc := NewGroupDiscussionService()
	session, err := groupSvc.CreateSession("tenant-b", CreateSessionRequest{Topic: "Tenant scoped", Goal: "hello", Participants: []corea2a.Participant{{ID: "maclaw-gui", RoleCode: "initiator"}, {ID: "shared-machine", RoleCode: "speak"}}, DecisionPolicy: corea2a.PolicyMajority})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	fallback := &captureGroupDiscussionSender{}
	handler := NewGroupDiscussionHandler(groupSvc, platformAwareMachineSender{fallback: fallback, system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}, {ID: "tenant-b", Slug: "tenant-b", Name: "Tenant B", Status: "active"}}}})
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)
	w := httptest.NewRecorder()
	req := groupReq(http.MethodPost, "/api/a2a/consultations/"+session.ID+"/messages", `{"id":"hub-msg-tenant-b","from_id":"maclaw-gui","content":"hello tenant b"}`)
	req = req.WithContext(WithRequestTenant(req.Context(), "tenant-b"))
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("group message status=%d body=%s", w.Code, w.Body.String())
	}
	select {
	case path := <-runtimeBCalls:
		if path != "/api/runtime/virtual-employees/platform-employee-b/discussion-messages" {
			t.Fatalf("unexpected tenant-b runtime path %s", path)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("tenant-b runtime was not called")
	}
	select {
	case path := <-runtimeACalls:
		t.Fatalf("tenant-a runtime must not receive tenant-b message, got %s", path)
	case <-time.After(150 * time.Millisecond):
	}
	detail := waitForDiscussionMessages(t, groupSvc, "tenant-b", session.ID, 2, func(messages []corea2a.Message) bool {
		return len(messages) >= 2 && messages[1].Content == "tenant b reply"
	})
	if len(detail.Messages) != 2 || detail.Messages[1].Content != "tenant b reply" {
		t.Fatalf("unexpected tenant-b detail: %#v", detail.Messages)
	}
}

func TestGroupDiscussionMessageReturnsDeliveryErrorWhenRuntimeMissing(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	provider := platformProviderEntry{PlatformID: "platform-1", CallbackBaseURL: "https://platform.invalid", CallbackSecret: "secret-1", RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "runtime-machine-1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "hub-account-1", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	groupSvc := NewGroupDiscussionService()
	session, err := groupSvc.CreateSession("tenant-a", CreateSessionRequest{Topic: "Direct", Goal: "hello", Participants: []corea2a.Participant{{ID: "maclaw-gui", RoleCode: "initiator"}, {ID: "runtime-machine-1", RoleCode: "speak"}}, DecisionPolicy: corea2a.PolicyMajority})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	fallback := &fakePlatformMachineSender{err: nil}
	handler := NewGroupDiscussionHandler(groupSvc, platformAwareMachineSender{fallback: fallback, system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}})
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+session.ID+"/messages", `{"from_id":"maclaw-gui","content":"hello platform employee"}`))
	if w.Code != http.StatusBadGateway || !bytes.Contains(w.Body.Bytes(), []byte("MESSAGE_DELIVERY_FAILED")) {
		t.Fatalf("delivery failure status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("MaClawSrv runtime is not configured")) {
		t.Fatalf("expected runtime missing error, got %s", w.Body.String())
	}
	if fallback.calls != 0 {
		t.Fatalf("runtime missing should not use desktop fallback, got %d", fallback.calls)
	}
	detail, err := groupSvc.GetDiscussionDetail("tenant-a", session.ID)
	if err != nil {
		t.Fatalf("GetDiscussionDetail: %v", err)
	}
	if len(detail.Messages) != 0 {
		t.Fatalf("failed runtime delivery should roll back persisted sender message, got %#v", detail.Messages)
	}
}

func TestGroupDiscussionMessageDoesNotDeliverDisabledPlatformEmployee(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	runtimeCalls := 0
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimeCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":{"content":"should not send"}}`))
	}))
	defer runtime.Close()
	provider := platformProviderEntry{PlatformID: "platform-1", RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: runtime.URL, AdminSecret: "runtime-secret", TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "runtime-machine-1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", RuntimeProviderID: maclawSrvRuntimePlatformID, OwnerUserID: "hub-account-1", Status: veStatusDisabled, OnlineStatus: veOnlineStatusOffline}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	groupSvc := NewGroupDiscussionService()
	session, err := groupSvc.CreateSession("tenant-a", CreateSessionRequest{Topic: "Direct", Goal: "hello", Participants: []corea2a.Participant{{ID: "maclaw-gui", RoleCode: "initiator"}, {ID: "runtime-machine-1", RoleCode: "speak"}}, DecisionPolicy: corea2a.PolicyMajority})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	fallback := &fakePlatformMachineSender{err: nil}
	handler := NewGroupDiscussionHandler(groupSvc, platformAwareMachineSender{fallback: fallback, system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}})
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+session.ID+"/messages", `{"from_id":"maclaw-gui","content":"hello disabled platform employee"}`))
	if w.Code != http.StatusBadGateway || !bytes.Contains(w.Body.Bytes(), []byte("MESSAGE_DELIVERY_FAILED")) {
		t.Fatalf("delivery failure status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("is not active")) {
		t.Fatalf("expected inactive employee error, got %s", w.Body.String())
	}
	if runtimeCalls != 0 {
		t.Fatalf("disabled platform employee must not call runtime, got %d", runtimeCalls)
	}
	if fallback.calls != 0 {
		t.Fatalf("disabled platform employee must not use desktop fallback, got %d", fallback.calls)
	}
	detail, err := groupSvc.GetDiscussionDetail("tenant-a", session.ID)
	if err != nil {
		t.Fatalf("GetDiscussionDetail: %v", err)
	}
	if len(detail.Messages) != 0 {
		t.Fatalf("failed disabled delivery should roll back persisted sender message, got %#v", detail.Messages)
	}
}

func TestGroupDiscussionMessageDoesNotFallbackWhenPlatformProviderMissingRuntime(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "runtime-machine-1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "hub-account-1", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	groupSvc := NewGroupDiscussionService()
	session, err := groupSvc.CreateSession("tenant-a", CreateSessionRequest{Topic: "Direct", Goal: "hello", Participants: []corea2a.Participant{{ID: "maclaw-gui", RoleCode: "initiator"}, {ID: "runtime-machine-1", RoleCode: "speak"}}, DecisionPolicy: corea2a.PolicyMajority})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	fallback := &fakePlatformMachineSender{err: nil}
	handler := NewGroupDiscussionHandler(groupSvc, platformAwareMachineSender{fallback: fallback, system: settings, tenants: fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}})
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+session.ID+"/messages", `{"from_id":"maclaw-gui","content":"hello platform employee"}`))
	if w.Code != http.StatusBadGateway || !bytes.Contains(w.Body.Bytes(), []byte("MESSAGE_DELIVERY_FAILED")) {
		t.Fatalf("delivery failure status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("MaClawSrv runtime is not configured")) {
		t.Fatalf("expected runtime missing error, got %s", w.Body.String())
	}
	if fallback.calls != 0 {
		t.Fatalf("platform employee without runtime must not use desktop fallback, got %d", fallback.calls)
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

func TestUpdatePlatformEmployeeStatusDisablesTenantScopedRegistryEntry(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	provider := platformProviderEntry{PlatformID: "platform-1", RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
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

type createCountingPlatformUserRepo struct {
	fakePlatformUserRepo
	creates int
	deletes int
}

type failKeySystemSettingsRepo struct {
	testSystemSettingsRepo
	failKey string
}

func (f *failKeySystemSettingsRepo) Set(ctx context.Context, key, valueJSON string) error {
	if key == f.failKey || strings.HasSuffix(key, ":"+f.failKey) {
		return errors.New("set failed")
	}
	return f.testSystemSettingsRepo.Set(ctx, key, valueJSON)
}

func (f *createCountingPlatformUserRepo) Create(ctx context.Context, user *store.User) error {
	_ = ctx
	f.creates++
	f.items = append(f.items, user)
	return nil
}

func (f *createCountingPlatformUserRepo) DeleteByTenantEmail(ctx context.Context, tenantID, email string) error {
	_ = ctx
	f.deletes++
	kept := f.items[:0]
	for _, item := range f.items {
		if item != nil && item.TenantID == tenantID && strings.EqualFold(item.Email, email) {
			continue
		}
		kept = append(kept, item)
	}
	f.items = kept
	return nil
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

type captureDeletePlatformUserRepo struct {
	fakePlatformUserRepo
	deletedTenantID string
	deletedEmail    string
}

func (f *captureDeletePlatformUserRepo) DeleteByTenantEmail(ctx context.Context, tenantID, email string) error {
	_ = ctx
	f.deletedTenantID = tenantID
	f.deletedEmail = email
	return nil
}

func (f fakePlatformUserRepo) UpdateSmartRoute(ctx context.Context, userID string, enabled bool) error {
	_ = ctx
	_ = userID
	_ = enabled
	return nil
}

func (f fakePlatformUserRepo) MarkEmailVerified(ctx context.Context, tenantID, email string) error {
	_ = ctx
	_ = tenantID
	_ = email
	return nil
}

func (f fakePlatformUserRepo) GetByTenantIdentity(ctx context.Context, tenantID, identityType, value string) (*store.User, error) {
	_ = ctx
	_ = tenantID
	_ = identityType
	_ = value
	return nil, nil
}

func (f fakePlatformUserRepo) ListIdentitiesByUser(ctx context.Context, tenantID, userID string) ([]*store.UserIdentity, error) {
	_ = ctx
	_ = tenantID
	_ = userID
	return nil, nil
}

func (f fakePlatformUserRepo) UpsertIdentity(ctx context.Context, identity *store.UserIdentity) error {
	_ = ctx
	_ = identity
	return nil
}

type fakePlatformViewerTokenRepo struct {
	items []*store.ViewerToken
}

type fakePlatformMachineLister struct {
	items []*store.Machine
}

func (f fakePlatformMachineLister) ListByTenant(ctx context.Context, tenantID string) ([]*store.Machine, error) {
	_ = ctx
	out := make([]*store.Machine, 0, len(f.items))
	for _, item := range f.items {
		if item != nil && item.TenantID == tenantID {
			out = append(out, item)
		}
	}
	return out, nil
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

func (f *fakePlatformViewerTokenRepo) DeleteByUserID(ctx context.Context, userID string) (int64, error) {
	_ = ctx
	_ = userID
	return 0, nil
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

	items, stats, err := platformSourceUsersForTenantWithStats(context.Background(), settings, users, "tenant-a")
	if err != nil {
		t.Fatalf("source users: %v", err)
	}
	if len(items) != 1 || items[0]["id"] != "real-1" {
		t.Fatalf("expected only real user, got %#v", items)
	}
	if stats.ExcludedPlatformEmployees != 1 || stats.ExcludedDesktopEnrolled != 0 {
		t.Fatalf("unexpected source user sync stats: %#v", stats)
	}

	included, includedStats, err := platformSourceUsersForTenantWithStatsAndOptions(context.Background(), settings, users, "tenant-a", platformSourceUserSyncOptions{IncludePlatformEmployees: true}, nil)
	if err != nil {
		t.Fatalf("source users with platform employees: %v", err)
	}
	if len(included) != 2 || includedStats.ExcludedPlatformEmployees != 0 {
		t.Fatalf("expected platform employees to be included on request, got items=%#v stats=%#v", included, includedStats)
	}
	var flagged bool
	for _, item := range included {
		if item["id"] == "ve-account-1" && item["is_virtual_employee"] == true && item["account_type"] == "virtual_employee" {
			flagged = true
		}
	}
	if !flagged {
		t.Fatalf("included platform employee should be flagged as virtual, got %#v", included)
	}
}

func TestPlatformSourceUsersForTenantExcludesDesktopEnrolledUsers(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	now := time.Now().UTC()
	users := fakePlatformUserRepo{items: []*store.User{
		{ID: "gui-user", TenantID: "tenant-a", Email: "gui@example.com", Status: "active", UpdatedAt: now},
		{ID: "source-user", TenantID: "tenant-a", Email: "source@example.com", Status: "active", UpdatedAt: now},
		{ID: "inactive-user", TenantID: "tenant-a", Email: "inactive@example.com", Status: "inactive", UpdatedAt: now},
		{ID: "other-tenant-gui", TenantID: "tenant-b", Email: "other@example.com", Status: "active", UpdatedAt: now},
	}}
	machines := fakePlatformMachineLister{items: []*store.Machine{
		{ID: "machine-gui", TenantID: "tenant-a", UserID: "gui-user"},
		{ID: "machine-other", TenantID: "tenant-b", UserID: "other-tenant-gui"},
	}}

	items, stats, err := platformSourceUsersForTenantWithStats(context.Background(), settings, users, "tenant-a", machines)
	if err != nil {
		t.Fatalf("source users: %v", err)
	}
	if len(items) != 1 || items[0]["id"] != "source-user" {
		t.Fatalf("expected only non-desktop source user, got %#v", items)
	}
	if stats.ExcludedDesktopEnrolled != 1 || stats.ExcludedPlatformEmployees != 0 {
		t.Fatalf("unexpected source user sync stats: %#v", stats)
	}
}

func TestPlatformSourceUsersForTenantIncludesApprovedUsers(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	now := time.Now().UTC()
	users := fakePlatformUserRepo{items: []*store.User{
		{ID: "approved-user", TenantID: "tenant-a", Email: "approved@example.com", Status: "STATUSAPPROVED", UpdatedAt: now},
		{ID: "inactive-user", TenantID: "tenant-a", Email: "inactive@example.com", Status: "inactive", UpdatedAt: now},
	}}

	items, stats, err := platformSourceUsersForTenantWithStats(context.Background(), settings, users, "tenant-a")
	if err != nil {
		t.Fatalf("source users: %v", err)
	}
	if len(items) != 1 || items[0]["id"] != "approved-user" || stats.ExcludedDesktopEnrolled != 0 || stats.ExcludedPlatformEmployees != 0 {
		t.Fatalf("expected approved Hub user to be synced as source user, items=%#v stats=%#v", items, stats)
	}
}

func TestPlatformSourceUsersForTenantCountsPlatformEmployeeBeforeDesktopMachine(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve-1", MachineID: "machine-ve", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "ve-account", OwnerEmail: "ve@example.com", Status: veStatusActive}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	now := time.Now().UTC()
	users := fakePlatformUserRepo{items: []*store.User{
		{ID: "ve-account", TenantID: "tenant-a", Email: "ve@example.com", Status: "active", UpdatedAt: now},
		{ID: "source-user", TenantID: "tenant-a", Email: "source@example.com", Status: "active", UpdatedAt: now},
	}}
	machines := fakePlatformMachineLister{items: []*store.Machine{{ID: "machine-ve", TenantID: "tenant-a", UserID: "ve-account"}}}

	items, stats, err := platformSourceUsersForTenantWithStats(context.Background(), settings, users, "tenant-a", machines)
	if err != nil {
		t.Fatalf("source users: %v", err)
	}
	if len(items) != 1 || items[0]["id"] != "source-user" {
		t.Fatalf("expected only source user, got %#v", items)
	}
	if stats.ExcludedPlatformEmployees != 1 || stats.ExcludedDesktopEnrolled != 0 {
		t.Fatalf("expected platform employee exclusion to win over desktop machine count, got %#v", stats)
	}
}

func TestPlatformSourceUsersSyncHandlerExcludesDesktopEnrolledUsers(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	now := time.Now().UTC()
	users := fakePlatformUserRepo{items: []*store.User{
		{ID: "gui-user", TenantID: "tenant-a", Email: "gui@example.com", Status: "active", UpdatedAt: now},
		{ID: "source-user", TenantID: "tenant-a", Email: "source@example.com", Status: "active", UpdatedAt: now},
	}}
	machines := fakePlatformMachineLister{items: []*store.Machine{{ID: "machine-gui", TenantID: "tenant-a", UserID: "gui-user"}}}
	tenants := fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}

	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/source-users/sync", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a"})
	rec := httptest.NewRecorder()
	PlatformSourceUsersSyncHandler(settings, users, tenants, machines).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("source users sync status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		Users []struct {
			ID string `json:"id"`
		} `json:"users"`
		SyncSummary struct {
			ExcludedDesktopEnrolled int `json:"excluded_desktop_enrolled"`
		} `json:"sync_summary"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].ID != "source-user" {
		t.Fatalf("expected sync response to exclude desktop user, got %#v", resp.Items)
	}
	if len(resp.Users) != 1 || resp.Users[0].ID != "source-user" {
		t.Fatalf("expected users alias to exclude desktop user, got %#v", resp.Users)
	}
	if resp.SyncSummary.ExcludedDesktopEnrolled != 1 {
		t.Fatalf("expected desktop exclusion count, got %#v", resp.SyncSummary)
	}

	includeReq := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/source-users/sync", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "include_desktop_enrolled": true})
	includeRec := httptest.NewRecorder()
	PlatformSourceUsersSyncHandler(settings, users, tenants, machines).ServeHTTP(includeRec, includeReq)
	if includeRec.Code != http.StatusOK {
		t.Fatalf("include desktop sync status=%d body=%s", includeRec.Code, includeRec.Body.String())
	}
	resp = struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		Users []struct {
			ID string `json:"id"`
		} `json:"users"`
		SyncSummary struct {
			ExcludedDesktopEnrolled int `json:"excluded_desktop_enrolled"`
		} `json:"sync_summary"`
	}{}
	if err := json.Unmarshal(includeRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode include desktop response: %v", err)
	}
	if len(resp.Items) != 2 || resp.SyncSummary.ExcludedDesktopEnrolled != 0 {
		t.Fatalf("expected include_desktop_enrolled to return both users without exclusion, got items=%#v summary=%#v", resp.Items, resp.SyncSummary)
	}
}

func TestPlatformSourceUsersSyncHandlerExcludesRealDesktopEnrollment(t *testing.T) {
	deps := newPlatformHubTestDeps(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := deps.store.Tenants.Create(ctx, &store.Tenant{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	identity := auth.NewIdentityService(deps.store.Users, deps.store.Enrollments, deps.store.EmailBlocks, deps.store.Machines, deps.store.ViewerTokens, deps.store.LoginTokens, deps.store.System, nil, "open", true, nil, "")
	identity.SetTenantRepository(deps.store.Tenants)
	if _, err := identity.StartEnrollment(auth.WithTenant(ctx, "tenant-a"), "gui@example.com", "office-pc", "darwin", "gui-client-1", ""); err != nil {
		t.Fatalf("start desktop enrollment: %v", err)
	}
	if err := deps.store.Users.Create(ctx, &store.User{ID: "source-user", TenantID: "tenant-a", Email: "source@example.com", SN: "SN-SOURCE", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create source user: %v", err)
	}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(ctx, deps.store.System, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}

	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/source-users/sync", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a"})
	rec := httptest.NewRecorder()
	PlatformSourceUsersSyncHandler(deps.store.System, deps.store.Users, deps.store.Tenants, deps.store.Machines).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("source users sync status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"items"`
		Users []struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"users"`
		SyncSummary struct {
			ExcludedDesktopEnrolled int `json:"excluded_desktop_enrolled"`
		} `json:"sync_summary"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].ID != "source-user" || resp.Items[0].Email != "source@example.com" {
		t.Fatalf("expected real desktop enrollment to be excluded, got %#v", resp.Items)
	}
	if len(resp.Users) != 1 || resp.Users[0].ID != "source-user" || resp.Users[0].Email != "source@example.com" {
		t.Fatalf("expected users alias to exclude real desktop enrollment, got %#v", resp.Users)
	}
	if resp.SyncSummary.ExcludedDesktopEnrolled != 1 {
		t.Fatalf("expected desktop exclusion count for real enrollment, got %#v", resp.SyncSummary)
	}
}

func TestPlatformSourceUserViewerTokenUsesTenantUser(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	ctx := context.Background()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(ctx, settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	if err := llmservice.SaveRegistry(ctx, tenantSystem, &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "ops-pro", Name: "Ops Pro", AccessPolicy: llmservice.AccessPolicyGrantRequired}}}); err != nil {
		t.Fatalf("save llm registry: %v", err)
	}
	users := fakePlatformUserRepo{items: []*store.User{{ID: "src-local", TenantID: "tenant-a", Email: "other@example.com", Status: "active"}, {ID: "real-1", TenantID: "tenant-a", Email: "real@example.com", Status: "active"}}}
	viewerTokens := &fakePlatformViewerTokenRepo{}
	identity := auth.NewIdentityService(users, nil, nil, nil, viewerTokens, nil, nil, nil, "open", true, nil, "")
	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/source-users/src-local/viewer-token", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "source_user_id": "src-local", "external_id": "real-1", "email": "real@example.com", "llm_service_group_id": "ops-pro"})
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
	reg, err := llmservice.LoadRegistry(ctx, tenantSystem)
	if err != nil {
		t.Fatalf("load llm registry: %v", err)
	}
	binding := platformTestFindUserBinding(reg, "real@example.com")
	if binding == nil || len(binding.ServiceGroupIDs) != 1 || binding.ServiceGroupIDs[0] != "ops-pro" {
		t.Fatalf("viewer token did not grant requested service group: %#v", reg.UserBindings)
	}
	if len(reg.Grants) != 1 || !strings.EqualFold(reg.Grants[0].Email, "real@example.com") || reg.Grants[0].ServiceGroupID != "ops-pro" || reg.Grants[0].Source != "ve_platform_source_user_token" {
		t.Fatalf("viewer token did not create requested service group grant: %#v", reg.Grants)
	}
}

func TestPlatformSourceUserViewerTokenRejectsDesktopEnrolledUser(t *testing.T) {
	deps := newPlatformHubTestDeps(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := deps.store.Tenants.Create(ctx, &store.Tenant{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	identity := auth.NewIdentityService(deps.store.Users, deps.store.Enrollments, deps.store.EmailBlocks, deps.store.Machines, deps.store.ViewerTokens, deps.store.LoginTokens, deps.store.System, nil, "open", true, nil, "")
	identity.SetTenantRepository(deps.store.Tenants)
	enroll, err := identity.StartEnrollment(auth.WithTenant(ctx, "tenant-a"), "gui@example.com", "office-pc", "darwin", "gui-client-1", "")
	if err != nil {
		t.Fatalf("start desktop enrollment: %v", err)
	}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(ctx, deps.store.System, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}

	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/source-users/"+enroll.UserID+"/viewer-token", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "source_user_id": enroll.UserID, "external_id": enroll.UserID, "email": "gui@example.com"})
	rec := httptest.NewRecorder()
	PlatformSourceUserViewerTokenHandler(deps.store.System, deps.store.Tenants, deps.store.Users, identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound || !bytes.Contains(rec.Body.Bytes(), []byte("SOURCE_USER_NOT_FOUND")) {
		t.Fatalf("desktop source viewer token status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPlatformSourceUserViewerTokenRejectsPlatformEmployeeAccount(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "ve-account-1", OwnerEmail: "worker@tenant.test", Status: veStatusActive}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	users := fakePlatformUserRepo{items: []*store.User{{ID: "ve-account-1", TenantID: "tenant-a", Email: "worker@tenant.test", Status: "active"}}}
	identity := auth.NewIdentityService(users, nil, nil, nil, &fakePlatformViewerTokenRepo{}, nil, nil, nil, "open", true, nil, "")
	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/source-users/ve-account-1/viewer-token", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "source_user_id": "ve-account-1", "external_id": "ve-account-1", "email": "worker@tenant.test"})
	rec := httptest.NewRecorder()
	PlatformSourceUserViewerTokenHandler(settings, fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}, users, identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound || !bytes.Contains(rec.Body.Bytes(), []byte("SOURCE_USER_NOT_FOUND")) {
		t.Fatalf("platform employee account source viewer token status=%d body=%s", rec.Code, rec.Body.String())
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

	platformOnlyReq := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "platform_employee_id": "platform-employee-2", "virtual_email": "worker2@tenant.test", "name": "Worker 2", "runtime_provider_id": "maclawsrv", "runtime_base_url": "https://runtime.example", "runtime_api_key": "runtime-secret"})
	platformOnlyRec := httptest.NewRecorder()
	PlatformEmployeeRegisterHandler(settings, tenants, fakePlatformUserRepo{}).ServeHTTP(platformOnlyRec, platformOnlyReq)
	if platformOnlyRec.Code != http.StatusOK {
		t.Fatalf("platform employee id register status=%d body=%s", platformOnlyRec.Code, platformOnlyRec.Body.String())
	}
	registry := loadVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings))
	if len(registry.Employees) != 1 || registry.Employees[0].PlatformEmployeeID != "platform-employee-2" || registry.Employees[0].MachineID != "ve_platform-employee-2" || registry.Employees[0].RuntimeProviderID != maclawSrvRuntimePlatformID {
		t.Fatalf("unexpected registered employee: %#v", registry.Employees)
	}
	runtimeRegistry := loadMacLawSrvRuntimeRegistry(context.Background(), settings)
	if len(runtimeRegistry.Runtimes) != 1 || runtimeRegistry.Runtimes[0].BaseURL != "https://runtime.example" || runtimeRegistry.Runtimes[0].AdminSecret != "runtime-secret" || !reflect.DeepEqual(runtimeRegistry.Runtimes[0].TenantIDs, []string{"tenant-a"}) {
		t.Fatalf("unexpected runtime registry: %#v", runtimeRegistry.Runtimes)
	}
}

func TestPlatformEmployeeRegisterStoresAvatarDataURL(t *testing.T) {
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
	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "platform_employee_id": "platform-employee-avatar", "virtual_email": "avatar@tenant.test", "name": "Avatar Worker", "avatar_data_url": testAvatarPNGDataURL, "runtime_provider_id": "maclawsrv", "runtime_base_url": "https://runtime.example"})
	rec := httptest.NewRecorder()
	PlatformEmployeeRegisterHandler(settings, tenants, fakePlatformUserRepo{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", rec.Code, rec.Body.String())
	}
	registry := loadVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings))
	if len(registry.Employees) != 1 || registry.Employees[0].AvatarDataURL != testAvatarPNGDataURL {
		t.Fatalf("avatar not stored: %#v", registry.Employees)
	}
}

func TestPlatformEmployeeRegisterAllowsOneMiBAvatarDataURL(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	payload := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, make([]byte, veAvatarImageMaxBytes-8)...)
	avatar := "data:image/png;base64," + base64.StdEncoding.EncodeToString(payload)
	if len(avatar) <= 1024*1024 {
		t.Fatalf("test avatar should exceed old encoded limit")
	}
	tenants := fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}
	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "platform_employee_id": "platform-employee-big-avatar", "virtual_email": "big-avatar@tenant.test", "name": "Big Avatar Worker", "avatar_data_url": avatar, "runtime_provider_id": "maclawsrv", "runtime_base_url": "https://runtime.example"})
	rec := httptest.NewRecorder()
	PlatformEmployeeRegisterHandler(settings, tenants, fakePlatformUserRepo{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPlatformEmployeeRegisterRequiresMacLawSrvRuntimeURLWhenRuntimeMissing(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), CallbackBaseURL: "https://platform.example/callback", RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	tenantRepo := fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}
	users := &createCountingPlatformUserRepo{}
	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "platform_employee_id": "platform-employee-1", "virtual_email": "worker@tenant.test", "name": "Worker", "runtime_provider_id": "maclawsrv"})
	rec := httptest.NewRecorder()
	PlatformEmployeeRegisterHandler(settings, tenantRepo, users).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !bytes.Contains(rec.Body.Bytes(), []byte("MACLAWSRV_RUNTIME_REQUIRED")) {
		t.Fatalf("register status=%d body=%s", rec.Code, rec.Body.String())
	}
	if users.creates != 0 || len(users.items) != 0 {
		t.Fatalf("missing runtime must not create user, creates=%d users=%#v", users.creates, users.items)
	}
	registry := loadVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings))
	if len(registry.Employees) != 0 {
		t.Fatalf("employee should not be registered without runtime_base_url: %#v", registry.Employees)
	}
	if runtimeRegistry := loadMacLawSrvRuntimeRegistry(context.Background(), settings); len(runtimeRegistry.Runtimes) != 0 {
		t.Fatalf("runtime registry should not be created without runtime_base_url: %#v", runtimeRegistry.Runtimes)
	}
}

func TestPlatformEmployeeRegisterRejectsUnsupportedRuntimeProvider(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), CallbackBaseURL: "https://platform.example/callback", RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	tenants := fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}
	users := &createCountingPlatformUserRepo{}
	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "platform_employee_id": "platform-employee-1", "virtual_email": "worker@tenant.test", "name": "Worker", "runtime_provider_id": "other-runtime", "runtime_base_url": "https://runtime.example", "runtime_api_key": "runtime-secret"})
	rec := httptest.NewRecorder()
	PlatformEmployeeRegisterHandler(settings, tenants, users).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !bytes.Contains(rec.Body.Bytes(), []byte("UNSUPPORTED_RUNTIME_PROVIDER")) {
		t.Fatalf("expected unsupported runtime provider, status=%d body=%s", rec.Code, rec.Body.String())
	}
	if users.creates != 0 || len(users.items) != 0 {
		t.Fatalf("unsupported runtime must not create user, creates=%d users=%#v", users.creates, users.items)
	}
	registry := loadVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings))
	if len(registry.Employees) != 0 {
		t.Fatalf("unsupported runtime provider must not register employee: %#v", registry.Employees)
	}
	if runtimeRegistry := loadMacLawSrvRuntimeRegistry(context.Background(), settings); len(runtimeRegistry.Runtimes) != 0 {
		t.Fatalf("unsupported runtime provider must not create runtime registry: %#v", runtimeRegistry.Runtimes)
	}
}

func TestPlatformEmployeeRegisterRequiresRuntimeForCurrentTenant(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: "https://runtime-a.example", AdminSecret: "runtime-a-secret", TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	tenants := fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}, {ID: "tenant-b", Slug: "tenant-b", Name: "Tenant B", Status: "active"}}}
	users := &createCountingPlatformUserRepo{}
	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-b", "platform_employee_id": "platform-employee-b", "virtual_email": "worker-b@tenant.test", "name": "Worker B"})
	rec := httptest.NewRecorder()
	PlatformEmployeeRegisterHandler(settings, tenants, users).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !bytes.Contains(rec.Body.Bytes(), []byte("MACLAWSRV_RUNTIME_REQUIRED")) {
		t.Fatalf("register status=%d body=%s", rec.Code, rec.Body.String())
	}
	if users.creates != 0 || len(users.items) != 0 {
		t.Fatalf("wrong-tenant runtime must not create user, creates=%d users=%#v", users.creates, users.items)
	}
	if runtime, ok := loadMacLawSrvRuntimeRegistry(context.Background(), settings).findForTenant("tenant-b"); ok {
		t.Fatalf("tenant-b should not inherit tenant-a runtime: %#v", runtime)
	}
	registry := loadVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-b", settings))
	if len(registry.Employees) != 0 {
		t.Fatalf("employee should not be registered with another tenant runtime: %#v", registry.Employees)
	}
}

func TestPlatformEmployeeRegisterDoesNotFetchRuntimeReportOrMarkOnline(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	reportCalled := false
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reportCalled = true
		if r.URL.Path != "/api/platform/runtime/report" {
			t.Fatalf("unexpected runtime path: %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "users": []map[string]any{}})
	}))
	defer runtime.Close()
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	tenants := fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}
	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees", "platform-1", privateKey, map[string]any{
		"hub_tenant_id":        "tenant-a",
		"platform_employee_id": "platform-employee-new",
		"virtual_email":        "new-worker@tenant.test",
		"name":                 "New Worker",
		"runtime_base_url":     runtime.URL,
		"runtime_api_key":      "runtime-secret",
	})
	rec := httptest.NewRecorder()
	PlatformEmployeeRegisterHandler(settings, tenants, fakePlatformUserRepo{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", rec.Code, rec.Body.String())
	}
	if reportCalled {
		t.Fatal("registration should not synchronously fetch runtime report")
	}
	registry := loadVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings))
	if len(registry.Employees) != 1 || registry.Employees[0].Status != veStatusActive || registry.Employees[0].OnlineStatus != veOnlineStatusOffline {
		t.Fatalf("fresh registration should remain active/offline until runtime reports ready: %#v", registry.Employees)
	}
}

func TestPlatformEmployeeRegisterKeepsRuntimesTenantScoped(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	tenants := fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}, {ID: "tenant-b", Slug: "tenant-b", Name: "Tenant B", Status: "active"}}}
	users := &createCountingPlatformUserRepo{}
	for _, tc := range []struct {
		tenantID string
		email    string
		empID    string
		baseURL  string
	}{
		{tenantID: "tenant-a", email: "worker-a@tenant.test", empID: "platform-employee-a", baseURL: "https://runtime-a.example"},
		{tenantID: "tenant-b", email: "worker-b@tenant.test", empID: "platform-employee-b", baseURL: "https://runtime-b.example"},
	} {
		req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees", "platform-1", privateKey, map[string]any{"hub_tenant_id": tc.tenantID, "platform_employee_id": tc.empID, "virtual_email": tc.email, "name": tc.empID, "runtime_base_url": tc.baseURL, "runtime_api_key": tc.empID + "-secret"})
		rec := httptest.NewRecorder()
		PlatformEmployeeRegisterHandler(settings, tenants, users).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("register %s status=%d body=%s", tc.tenantID, rec.Code, rec.Body.String())
		}
	}
	runtimeRegistry := loadMacLawSrvRuntimeRegistry(context.Background(), settings)
	if len(runtimeRegistry.Runtimes) != 2 {
		t.Fatalf("expected one runtime per base URL, got %#v", runtimeRegistry.Runtimes)
	}
	if runtime, ok := runtimeRegistry.findForTenant("tenant-a"); !ok || runtime.BaseURL != "https://runtime-a.example" {
		t.Fatalf("tenant-a runtime = %#v ok=%v", runtime, ok)
	}
	if runtime, ok := runtimeRegistry.findForTenant("tenant-b"); !ok || runtime.BaseURL != "https://runtime-b.example" {
		t.Fatalf("tenant-b runtime = %#v ok=%v", runtime, ok)
	}
}

func TestPlatformEmployeeRegisterUpdatesExistingPlatformEmployeeID(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: "https://runtime.example", AdminSecret: "runtime-secret", TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	registeredAt := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	seed := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_old-employee-id", MachineID: "ve_old-employee-id", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "old-account", OwnerEmail: "old@tenant.test", Name: "Old Worker", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline, AvatarDataURL: testAvatarPNGDataURL, AccessPolicy: "private", Whitelist: []string{"user-a"}, Blacklist: []string{"user-b"}, VisibleGroupIDs: []string{"dept-legal"}, Resident: true, RegisteredAt: registeredAt}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), seed); err != nil {
		t.Fatalf("save seed registry: %v", err)
	}
	tenants := fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}
	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "employee_id": "new-employee-id", "platform_employee_id": "platform-employee-1", "virtual_email": "new@tenant.test", "name": "New Worker"})
	rec := httptest.NewRecorder()
	PlatformEmployeeRegisterHandler(settings, tenants, fakePlatformUserRepo{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		HubAccountID string               `json:"hub_account_id"`
		Employee     digitalEmployeeEntry `json:"employee"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	registry := loadVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings))
	if len(registry.Employees) != 1 {
		t.Fatalf("expected registration to update existing employee, got %#v", registry.Employees)
	}
	got := registry.Employees[0]
	if got.ID != "ve_new-employee-id" || got.MachineID != "ve_new-employee-id" || got.PlatformEmployeeID != "platform-employee-1" || got.Name != "New Worker" || got.RegisteredAt != registeredAt {
		t.Fatalf("unexpected updated employee: %#v", got)
	}
	if !got.Resident || got.AccessPolicy != "private" || !reflect.DeepEqual(got.Whitelist, []string{"user-a"}) || !reflect.DeepEqual(got.Blacklist, []string{"user-b"}) || !reflect.DeepEqual(got.VisibleGroupIDs, []string{"dept-legal"}) {
		t.Fatalf("hub-managed fields should be preserved on platform info update: %#v", got)
	}
	if got.AvatarDataURL != testAvatarPNGDataURL {
		t.Fatalf("existing avatar should be preserved when platform update omits avatar: %#v", got)
	}
	if resp.HubAccountID != resp.Employee.OwnerUserID || resp.HubAccountID != got.OwnerUserID {
		t.Fatalf("register response hub_account_id should match final employee owner: body=%s saved=%#v", rec.Body.String(), got)
	}
}

func TestPlatformEmployeeRegisterUpdateBackfillsMissingRegisteredAt(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: "https://runtime.example", AdminSecret: "runtime-secret", TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	seed := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee-1", MachineID: "ve_employee-1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", Status: veStatusActive}}}
	if err := saveVERegistry(context.Background(), tenantSystem, seed); err != nil {
		t.Fatalf("save seed registry: %v", err)
	}
	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "platform_employee_id": "platform-employee-1", "virtual_email": "worker@tenant.test", "name": "Worker"})
	rec := httptest.NewRecorder()
	PlatformEmployeeRegisterHandler(settings, fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}, fakePlatformUserRepo{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated := loadVERegistry(context.Background(), tenantSystem)
	if len(updated.Employees) != 1 || strings.TrimSpace(updated.Employees[0].RegisteredAt) == "" {
		t.Fatalf("missing registered_at should be backfilled on platform update: %#v", updated.Employees)
	}
}

func TestPlatformEmployeeRegisterUpdateReplacesAvatarWhenProvided(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: "https://runtime.example", AdminSecret: "runtime-secret", TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	newAvatar := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte{0xff, 0xd8, 0xff, 0xdb, 0x00, 0x43, 0x00})
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	seed := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee-1", MachineID: "ve_employee-1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", AvatarDataURL: testAvatarPNGDataURL, Status: veStatusActive, RegisteredAt: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)}}}
	if err := saveVERegistry(context.Background(), tenantSystem, seed); err != nil {
		t.Fatalf("save seed registry: %v", err)
	}
	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "platform_employee_id": "platform-employee-1", "virtual_email": "worker@tenant.test", "name": "Worker", "avatar_data_url": newAvatar})
	rec := httptest.NewRecorder()
	PlatformEmployeeRegisterHandler(settings, fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}, fakePlatformUserRepo{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated := loadVERegistry(context.Background(), tenantSystem)
	if len(updated.Employees) != 1 || updated.Employees[0].AvatarDataURL != newAvatar {
		t.Fatalf("provided avatar should replace old avatar: %#v", updated.Employees)
	}
}

func TestPlatformEmployeeRegisterUpdateDoesNotReactivateDisabledEmployee(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: "https://runtime.example", AdminSecret: "runtime-secret", TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	disabledAt := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	seed := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee-1", MachineID: "ve_employee-1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", Status: veStatusDisabled, OnlineStatus: veOnlineStatusOffline, DisabledAt: disabledAt, Resident: true, RegisteredAt: disabledAt}}}
	if err := saveVERegistry(context.Background(), tenantSystem, seed); err != nil {
		t.Fatalf("save seed registry: %v", err)
	}
	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "platform_employee_id": "platform-employee-1", "virtual_email": "worker@tenant.test", "name": "Renamed Worker"})
	rec := httptest.NewRecorder()
	PlatformEmployeeRegisterHandler(settings, fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}, fakePlatformUserRepo{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated := loadVERegistry(context.Background(), tenantSystem)
	if len(updated.Employees) != 1 {
		t.Fatalf("expected one employee after update: %#v", updated.Employees)
	}
	got := updated.Employees[0]
	if got.Status != veStatusDisabled || got.OnlineStatus != veOnlineStatusOffline || got.DisabledAt != disabledAt || got.Resident {
		t.Fatalf("platform info update must not reactivate disabled employee or keep disabled resident: %#v", got)
	}
}

func TestPlatformEmployeeRegisterUpdateClearsStaleTerminalFieldsForActiveEmployee(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: "https://runtime.example", AdminSecret: "runtime-secret", TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	seed := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee-1", MachineID: "ve_employee-1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", Status: veStatusActive, DisabledAt: "2026-01-01T00:00:00Z", RejectReason: "stale", RejectedAt: "2026-01-02T00:00:00Z", RegisteredAt: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)}}}
	if err := saveVERegistry(context.Background(), tenantSystem, seed); err != nil {
		t.Fatalf("save seed registry: %v", err)
	}
	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "platform_employee_id": "platform-employee-1", "virtual_email": "worker@tenant.test", "name": "Worker"})
	rec := httptest.NewRecorder()
	PlatformEmployeeRegisterHandler(settings, fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}, fakePlatformUserRepo{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated := loadVERegistry(context.Background(), tenantSystem)
	got := updated.Employees[0]
	if got.Status != veStatusActive || got.DisabledAt != "" || got.RejectReason != "" || got.RejectedAt != "" {
		t.Fatalf("active employee should not keep stale terminal fields: %#v", got)
	}
}

func TestPlatformEmployeeRegisterUpdateDoesNotReactivateRejectedEmployee(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: "https://runtime.example", AdminSecret: "runtime-secret", TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	rejectedAt := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	seed := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee-1", MachineID: "ve_employee-1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", Status: veStatusRejected, RejectReason: "not allowed", RejectedAt: rejectedAt, RegisteredAt: rejectedAt}}}
	if err := saveVERegistry(context.Background(), tenantSystem, seed); err != nil {
		t.Fatalf("save seed registry: %v", err)
	}
	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "platform_employee_id": "platform-employee-1", "virtual_email": "worker@tenant.test", "name": "Renamed Worker"})
	rec := httptest.NewRecorder()
	PlatformEmployeeRegisterHandler(settings, fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}, fakePlatformUserRepo{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated := loadVERegistry(context.Background(), tenantSystem)
	got := updated.Employees[0]
	if got.Status != veStatusRejected || got.RejectReason != "not allowed" || got.RejectedAt != rejectedAt {
		t.Fatalf("platform info update must not reactivate rejected employee or drop rejection detail: %#v", got)
	}
}

func TestPlatformEmployeeRegisterUpdateReturnsNormalizedResidentFlag(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: "https://runtime.example", AdminSecret: "runtime-secret", TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	registeredAt := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	seed := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{
		{ID: "ve_first", MachineID: "ve_first", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-first", Status: veStatusActive, Resident: true, RegisteredAt: registeredAt},
		{ID: "ve_second", MachineID: "ve_second", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-second", Status: veStatusActive, Resident: true, RegisteredAt: registeredAt},
	}}
	if err := saveVERegistry(context.Background(), tenantSystem, seed); err != nil {
		t.Fatalf("save seed registry: %v", err)
	}
	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "platform_employee_id": "platform-employee-second", "virtual_email": "worker@tenant.test", "name": "Second Worker"})
	rec := httptest.NewRecorder()
	PlatformEmployeeRegisterHandler(settings, fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}, fakePlatformUserRepo{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Employee digitalEmployeeEntry `json:"employee"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	if resp.Employee.Resident {
		t.Fatalf("response should return normalized non-resident second employee: %s", rec.Body.String())
	}
	updated := loadVERegistry(context.Background(), tenantSystem)
	if len(updated.Employees) != 2 || !updated.Employees[0].Resident || updated.Employees[1].Resident {
		t.Fatalf("registry resident flags not normalized: %#v", updated.Employees)
	}
}

func TestPlatformEmployeeRegisterDoesNotOverwritePhysicalEmployeeIDCollision(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: "https://runtime.example", AdminSecret: "runtime-secret", TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	seed := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_emp_1", MachineID: "ve_emp_1", EmployeeType: veEmployeeTypePhysical, PlatformID: "platform-1", Name: "Desktop Worker", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), tenantSystem, seed); err != nil {
		t.Fatalf("save seed registry: %v", err)
	}
	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "employee_id": "emp_1", "platform_employee_id": "platform-employee-1", "virtual_email": "worker@tenant.test", "name": "Platform Worker"})
	rec := httptest.NewRecorder()
	PlatformEmployeeRegisterHandler(settings, fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}, fakePlatformUserRepo{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", rec.Code, rec.Body.String())
	}
	registry := loadVERegistry(context.Background(), tenantSystem)
	if len(registry.Employees) != 2 {
		t.Fatalf("physical employee should not be overwritten: %#v", registry.Employees)
	}
	byID := map[string]digitalEmployeeEntry{}
	for _, employee := range registry.Employees {
		byID[employee.ID] = employee
	}
	if byID["ve_emp_1"].EmployeeType != veEmployeeTypePhysical || byID["ve_emp_1"].PlatformEmployeeID != "" {
		t.Fatalf("physical employee mutated: %#v", byID["ve_emp_1"])
	}
	if got := byID["ve_platform-employee-1"]; got.EmployeeType != veEmployeeTypeVirtual || got.PlatformEmployeeID != "platform-employee-1" {
		t.Fatalf("platform employee not registered separately: %#v", got)
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
	users := &createCountingPlatformUserRepo{}

	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "employee_id": "platform-employee-1", "virtual_email": "worker@tenant.test", "name": "Worker", "llm_service_group_id": "missing-group", "runtime_base_url": "https://runtime.example", "runtime_api_key": "runtime-secret"})
	rec := httptest.NewRecorder()
	PlatformEmployeeRegisterHandler(settings, tenants, users).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "LLM_SERVICE_GROUP_ENTITLEMENT_FAILED") {
		t.Fatalf("register status=%d body=%s", rec.Code, rec.Body.String())
	}
	if users.creates != 0 || len(users.items) != 0 {
		t.Fatalf("invalid llm service group must not create user, creates=%d users=%#v", users.creates, users.items)
	}
	if runtimeRegistry := loadMacLawSrvRuntimeRegistry(context.Background(), settings); len(runtimeRegistry.Runtimes) != 0 {
		t.Fatalf("invalid llm service group must not create runtime registry: %#v", runtimeRegistry.Runtimes)
	}
	registry := loadVERegistry(context.Background(), tenantSystem)
	if len(registry.Employees) != 0 {
		t.Fatalf("digital employee should not be created for invalid llm service group: %#v", registry.Employees)
	}
}

func TestPlatformEmployeeRegisterLLMEntitlementSaveFailureRollsBackCreatedUser(t *testing.T) {
	settings := &failKeySystemSettingsRepo{failKey: llmservice.RegistryKey}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: "https://runtime.example", AdminSecret: "runtime-secret", TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	if err := settings.testSystemSettingsRepo.Set(context.Background(), "tenant:tenant-a:"+llmservice.RegistryKey, mustJSON(t, llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "ops-pro", Name: "Ops Pro"}}})); err != nil {
		t.Fatalf("seed llm registry: %v", err)
	}
	users := &createCountingPlatformUserRepo{}
	tenants := fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}
	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "employee_id": "platform-employee-1", "virtual_email": "worker@tenant.test", "name": "Worker", "llm_service_group_id": "ops-pro"})
	rec := httptest.NewRecorder()
	PlatformEmployeeRegisterHandler(settings, tenants, users).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "LLM_SERVICE_GROUP_ENTITLEMENT_FAILED") {
		t.Fatalf("register status=%d body=%s", rec.Code, rec.Body.String())
	}
	if users.creates != 1 || users.deletes != 1 || len(users.items) != 0 {
		t.Fatalf("llm entitlement save failure must roll back created user, creates=%d deletes=%d users=%#v", users.creates, users.deletes, users.items)
	}
	registry := loadVERegistry(context.Background(), tenantSystem)
	if len(registry.Employees) != 0 {
		t.Fatalf("employee should not be registered when llm entitlement save fails: %#v", registry.Employees)
	}
}

func TestPlatformEmployeeRegisterLLMEntitlementSaveFailureDoesNotCreateRuntime(t *testing.T) {
	settings := &failKeySystemSettingsRepo{failKey: llmservice.RegistryKey}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := settings.testSystemSettingsRepo.Set(context.Background(), "tenant:tenant-a:"+llmservice.RegistryKey, mustJSON(t, llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "ops-pro", Name: "Ops Pro"}}})); err != nil {
		t.Fatalf("seed llm registry: %v", err)
	}
	users := &createCountingPlatformUserRepo{}
	tenants := fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}
	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "employee_id": "platform-employee-1", "virtual_email": "worker@tenant.test", "name": "Worker", "llm_service_group_id": "ops-pro", "runtime_base_url": "https://runtime.example", "runtime_api_key": "runtime-secret"})
	rec := httptest.NewRecorder()
	PlatformEmployeeRegisterHandler(settings, tenants, users).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "LLM_SERVICE_GROUP_ENTITLEMENT_FAILED") {
		t.Fatalf("register status=%d body=%s", rec.Code, rec.Body.String())
	}
	if users.creates != 1 || users.deletes != 1 || len(users.items) != 0 {
		t.Fatalf("llm entitlement save failure must roll back created user, creates=%d deletes=%d users=%#v", users.creates, users.deletes, users.items)
	}
	if runtimeRegistry := loadMacLawSrvRuntimeRegistry(context.Background(), settings); len(runtimeRegistry.Runtimes) != 0 {
		t.Fatalf("llm entitlement save failure must not create runtime registry: %#v", runtimeRegistry.Runtimes)
	}
	registry := loadVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings))
	if len(registry.Employees) != 0 {
		t.Fatalf("employee should not be registered when llm entitlement save fails: %#v", registry.Employees)
	}
}

func TestPlatformEmployeeRegisterRegistrySaveFailureRollsBackCreatedUser(t *testing.T) {
	settings := &failKeySystemSettingsRepo{failKey: veRegistryKey}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	if err := settings.testSystemSettingsRepo.Set(context.Background(), "tenant:tenant-a:"+llmservice.RegistryKey, mustJSON(t, llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "ops-pro", Name: "Ops Pro", AccessPolicy: llmservice.AccessPolicyGrantRequired}}})); err != nil {
		t.Fatalf("seed llm registry: %v", err)
	}
	users := &createCountingPlatformUserRepo{}
	tenants := fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}
	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "platform_employee_id": "platform-employee-1", "virtual_email": "worker@tenant.test", "name": "Worker", "llm_service_group_id": "ops-pro", "runtime_base_url": "https://runtime.example", "runtime_api_key": "runtime-secret"})
	rec := httptest.NewRecorder()
	PlatformEmployeeRegisterHandler(settings, tenants, users).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "VE_REGISTRY_SAVE_FAILED") {
		t.Fatalf("register status=%d body=%s", rec.Code, rec.Body.String())
	}
	if users.creates != 1 || users.deletes != 1 || len(users.items) != 0 {
		t.Fatalf("registry save failure should roll back created user, creates=%d deletes=%d users=%#v", users.creates, users.deletes, users.items)
	}
	if runtimeRegistry := loadMacLawSrvRuntimeRegistry(context.Background(), settings); len(runtimeRegistry.Runtimes) != 0 {
		t.Fatalf("registry save failure must not create runtime registry: %#v", runtimeRegistry.Runtimes)
	}
	llmRegistry, err := llmservice.LoadRegistry(context.Background(), tenantSystem)
	if err != nil {
		t.Fatalf("load llm registry: %v", err)
	}
	if len(llmRegistry.UserBindings) != 0 || len(llmRegistry.Grants) != 0 {
		t.Fatalf("registry save failure must not create llm bindings or grants: %#v", llmRegistry)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return string(data)
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

	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "employee_id": "platform-employee-1", "virtual_email": "worker@tenant.test", "name": "Worker", "llm_service_group_id": "ops-pro", "runtime_base_url": "https://runtime.example", "runtime_api_key": "runtime-secret"})
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
	registry := loadVERegistry(context.Background(), tenantSystem)
	if len(registry.Employees) != 1 || registry.Employees[0].OnlineStatus != veOnlineStatusOffline {
		t.Fatalf("registered employee should not be marked online before runtime report confirms ready: %#v", registry.Employees)
	}
}

func TestPlatformEmployeeViewerTokenUsesExistingEmployeeAccount(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	ctx := context.Background()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active"}
	if err := savePlatformProviderRegistry(ctx, settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	if err := llmservice.SaveRegistry(ctx, tenantSystem, &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "ops-pro", Name: "Ops Pro", AccessPolicy: llmservice.AccessPolicyGrantRequired}}}); err != nil {
		t.Fatalf("save llm registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_platform-employee-1", MachineID: "ve_platform-employee-1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "hub-account-1", OwnerEmail: "worker@tenant.test", Status: veStatusActive}}}
	if err := saveVERegistry(ctx, tenantSystem, registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	users := fakePlatformUserRepo{items: []*store.User{{ID: "hub-account-1", TenantID: "tenant-a", Email: "worker@tenant.test", Status: "active"}}}
	viewerTokens := &fakePlatformViewerTokenRepo{}
	identity := auth.NewIdentityService(users, nil, nil, nil, viewerTokens, nil, nil, nil, "open", true, nil, "")
	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees/platform-employee-1/viewer-token", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "platform_employee_id": "platform-employee-1", "hub_employee_id": "ve_platform-employee-1", "hub_account_id": "hub-account-1", "llm_service_group_id": "ops-pro"})
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
	reg, err := llmservice.LoadRegistry(ctx, tenantSystem)
	if err != nil {
		t.Fatalf("load llm registry: %v", err)
	}
	binding := platformTestFindUserBinding(reg, "worker@tenant.test")
	if binding == nil || len(binding.ServiceGroupIDs) != 1 || binding.ServiceGroupIDs[0] != "ops-pro" {
		t.Fatalf("viewer token did not grant requested service group: %#v", reg.UserBindings)
	}
	if len(reg.Grants) != 1 || !strings.EqualFold(reg.Grants[0].Email, "worker@tenant.test") || reg.Grants[0].ServiceGroupID != "ops-pro" || reg.Grants[0].Source != "ve_platform_employee_token" {
		t.Fatalf("viewer token did not create requested service group grant: %#v", reg.Grants)
	}
	req = newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees/platform-employee-1/viewer-token", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "platform_employee_id": "platform-employee-1", "hub_employee_id": "ve_platform-employee-1", "hub_account_id": "hub-account-1", "llm_service_group_id": "ops-pro"})
	rec = httptest.NewRecorder()
	PlatformEmployeeViewerTokenHandler(settings, fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}, users, identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("second viewer token status = %d body=%s", rec.Code, rec.Body.String())
	}
	reg, err = llmservice.LoadRegistry(ctx, tenantSystem)
	if err != nil {
		t.Fatalf("reload llm registry: %v", err)
	}
	binding = platformTestFindUserBinding(reg, "worker@tenant.test")
	if binding == nil || len(binding.ServiceGroupIDs) != 1 || len(reg.Grants) != 1 {
		t.Fatalf("second viewer token duplicated entitlement state: binding=%#v grants=%#v", binding, reg.Grants)
	}
}

func platformTestFindUserBinding(reg *llmservice.Registry, email string) *llmservice.UserBinding {
	if reg == nil {
		return nil
	}
	for i := range reg.UserBindings {
		if strings.EqualFold(strings.TrimSpace(reg.UserBindings[i].Email), strings.TrimSpace(email)) {
			return &reg.UserBindings[i]
		}
	}
	return nil
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
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
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

func TestPlatformEmployeeStatusActiveRejectsMissingMacLawSrvRuntimeEmployee(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/platform/runtime/report" {
			t.Fatalf("unexpected runtime path: %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "users": []map[string]any{}})
	}))
	defer runtime.Close()
	provider := platformProviderEntry{PlatformID: maclawSrvRuntimePlatformID, PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: runtime.URL, TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{
		ID:                 "ve_deleted",
		MachineID:          "ve_deleted",
		PlatformID:         maclawSrvRuntimePlatformID,
		PlatformEmployeeID: "deleted-employee",
		RuntimeProviderID:  maclawSrvRuntimePlatformID,
		Status:             veStatusDisabled,
		OnlineStatus:       veOnlineStatusOffline,
	}}}
	if err := saveVERegistry(context.Background(), tenantSystem, registry); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees/deleted-employee/status", maclawSrvRuntimePlatformID, privateKey, map[string]any{
		"hub_tenant_id":        "tenant-a",
		"platform_employee_id": "deleted-employee",
		"service_status":       "active",
	})
	rec := httptest.NewRecorder()
	PlatformEmployeeStatusHandler(settings, fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}).ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict || !bytes.Contains(rec.Body.Bytes(), []byte("VE_RUNTIME_MISSING")) {
		t.Fatalf("status active missing runtime status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated := loadVERegistry(context.Background(), tenantSystem)
	if len(updated.Employees) != 1 || updated.Employees[0].Status != veStatusDisabled || updated.Employees[0].OnlineStatus != veOnlineStatusOffline {
		t.Fatalf("missing runtime employee should stay disabled/offline: %#v", updated.Employees)
	}
}

func TestPlatformEmployeeStatusActiveWithoutTenantRejectsMissingMacLawSrvRuntimeEmployee(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/platform/runtime/report" {
			t.Fatalf("unexpected runtime path: %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "users": []map[string]any{}})
	}))
	defer runtime.Close()
	provider := platformProviderEntry{PlatformID: maclawSrvRuntimePlatformID, PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: runtime.URL, TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{
		ID:                 "ve_deleted",
		MachineID:          "ve_deleted",
		PlatformID:         maclawSrvRuntimePlatformID,
		PlatformEmployeeID: "deleted-employee",
		RuntimeProviderID:  maclawSrvRuntimePlatformID,
		Status:             veStatusDisabled,
		OnlineStatus:       veOnlineStatusOffline,
	}}}
	if err := saveVERegistry(context.Background(), tenantSystem, registry); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees/deleted-employee/status", maclawSrvRuntimePlatformID, privateKey, map[string]any{
		"platform_employee_id": "deleted-employee",
		"service_status":       "active",
	})
	rec := httptest.NewRecorder()
	PlatformEmployeeStatusHandler(settings, fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}).ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict || !bytes.Contains(rec.Body.Bytes(), []byte("VE_RUNTIME_MISSING")) {
		t.Fatalf("status active missing runtime without tenant status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated := loadVERegistry(context.Background(), tenantSystem)
	if len(updated.Employees) != 1 || updated.Employees[0].Status != veStatusDisabled || updated.Employees[0].OnlineStatus != veOnlineStatusOffline {
		t.Fatalf("missing runtime employee should stay disabled/offline: %#v", updated.Employees)
	}
}

func TestNormalizePlatformEmployeeDeletedStatusDisablesEmployee(t *testing.T) {
	for _, status := range []string{"deleted", "removed"} {
		if got := normalizePlatformEmployeeStatus(status); got != veStatusDisabled {
			t.Fatalf("normalize %q = %q, want %q", status, got, veStatusDisabled)
		}
	}
}

func TestPlatformEmployeeStatusDeleteVERegistryRemovesRegistryEntry(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "hub-account-1", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}

	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees/platform-employee-1/status", "platform-1", privateKey, map[string]any{
		"hub_tenant_id":        "tenant-a",
		"platform_employee_id": "platform-employee-1",
		"hub_employee_id":      "ve_employee_1",
		"hub_account_id":       "hub-account-1",
		"service_status":       "deleted",
		"delete_ve_registry":   true,
		"ve_registry_status":   "deleted",
	})
	rec := httptest.NewRecorder()
	PlatformEmployeeStatusHandler(settings, fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status delete registry=%d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"hub_status":"deleted"`)) {
		t.Fatalf("status delete registry response should report deleted: %s", rec.Body.String())
	}
	updated := loadVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings))
	if len(updated.Employees) != 0 {
		t.Fatalf("status delete left registry entry: %#v", updated.Employees)
	}
}

func TestPlatformEmployeeStatusDeleteVERegistryWithoutTenantRemovesRegistryEntry(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "hub-account-1", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline, Resident: true}}}
	if err := saveVERegistry(context.Background(), tenantSystem, registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}

	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees/platform-employee-1/status", "platform-1", privateKey, map[string]any{
		"platform_employee_id": "platform-employee-1",
		"service_status":       "deleted",
		"delete_ve_registry":   true,
	})
	rec := httptest.NewRecorder()
	PlatformEmployeeStatusHandler(settings, fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status delete registry without tenant=%d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"hub_status":"deleted"`)) {
		t.Fatalf("status delete registry without tenant response should report deleted: %s", rec.Body.String())
	}
	updated := loadVERegistry(context.Background(), tenantSystem)
	if len(updated.Employees) != 0 {
		t.Fatalf("status delete without tenant left registry entry: %#v", updated.Employees)
	}
}

func TestPlatformEmployeeStatusDeleteVERegistryWithoutTenantMatchesIdentity(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}, {HubTenantID: "tenant-b"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	tenantASystem := scopedSystemSettingsForTenant("tenant-a", settings)
	tenantBSystem := scopedSystemSettingsForTenant("tenant-b", settings)
	if err := saveVERegistry(context.Background(), tenantASystem, digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_a", MachineID: "ve_employee_a", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "hub-account-a", Status: veStatusActive}}}); err != nil {
		t.Fatalf("save tenant a registry: %v", err)
	}
	if err := saveVERegistry(context.Background(), tenantBSystem, digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_b", MachineID: "ve_employee_b", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "hub-account-b", Status: veStatusActive}}}); err != nil {
		t.Fatalf("save tenant b registry: %v", err)
	}

	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees/platform-employee-1/status", "platform-1", privateKey, map[string]any{
		"platform_employee_id": "platform-employee-1",
		"hub_employee_id":      "ve_employee_b",
		"hub_account_id":       "hub-account-b",
		"service_status":       "deleted",
		"delete_ve_registry":   true,
	})
	rec := httptest.NewRecorder()
	tenants := fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}, {ID: "tenant-b", Slug: "tenant-b", Name: "Tenant B", Status: "active"}}}
	PlatformEmployeeStatusHandler(settings, tenants).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status delete registry identity match=%d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"hub_tenant_id":"tenant-b"`)) {
		t.Fatalf("status delete registry response should resolve tenant b: %s", rec.Body.String())
	}
	if updatedA := loadVERegistry(context.Background(), tenantASystem); len(updatedA.Employees) != 1 {
		t.Fatalf("tenant a registry should remain: %#v", updatedA.Employees)
	}
	if updatedB := loadVERegistry(context.Background(), tenantBSystem); len(updatedB.Employees) != 0 {
		t.Fatalf("tenant b registry should be removed: %#v", updatedB.Employees)
	}
}

func TestPlatformEmployeeDeleteRemovesRegistryEntry(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "hub-account-1", OwnerEmail: "worker@tenant.test", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}

	req := newSignedPlatformJSONRequest(t, http.MethodDelete, "/api/platform/employees/platform-employee-1", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "platform_employee_id": "platform-employee-1", "hub_employee_id": "ve_employee_1", "hub_account_id": "hub-account-1", "virtual_email": "worker@tenant.test"})
	rec := httptest.NewRecorder()
	PlatformEmployeeDeleteHandler(settings, fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}, fakePlatformUserRepo{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"ve_registry_deleted":true`)) {
		t.Fatalf("delete response should report registry deletion: %s", rec.Body.String())
	}
	updated := loadVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings))
	if len(updated.Employees) != 0 {
		t.Fatalf("employee registry entry was not removed: %#v", updated.Employees)
	}
}

func TestPlatformEmployeeDeleteWithoutTenantRemovesRegistryEntry(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "hub-account-1", OwnerEmail: "worker@tenant.test", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), tenantSystem, registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}

	req := newSignedPlatformJSONRequest(t, http.MethodDelete, "/api/platform/employees/platform-employee-1", "platform-1", privateKey, map[string]any{"platform_employee_id": "platform-employee-1", "hub_employee_id": "ve_employee_1", "hub_account_id": "hub-account-1", "virtual_email": "worker@tenant.test"})
	rec := httptest.NewRecorder()
	PlatformEmployeeDeleteHandler(settings, fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}, fakePlatformUserRepo{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete without tenant status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"hub_tenant_id":"tenant-a"`)) || !bytes.Contains(rec.Body.Bytes(), []byte(`"ve_registry_deleted":true`)) {
		t.Fatalf("delete without tenant response should report resolved tenant and registry deletion: %s", rec.Body.String())
	}
	updated := loadVERegistry(context.Background(), tenantSystem)
	if len(updated.Employees) != 0 {
		t.Fatalf("delete without tenant left registry entry: %#v", updated.Employees)
	}
}

func TestPlatformEmployeeDeleteLookupMatchesIdentity(t *testing.T) {
	entry := digitalEmployeeEntry{ID: "ve_employee_1", MachineID: "ve_employee_1", OwnerUserID: "hub-account-1"}
	if !platformEmployeeDeleteLookupMatches(entry, "ve_employee_1", "hub-account-1") {
		t.Fatal("expected exact employee and account identity to match")
	}
	if platformEmployeeDeleteLookupMatches(entry, "ve_employee_2", "hub-account-1") {
		t.Fatal("expected mismatched hub employee identity to be rejected")
	}
	if platformEmployeeDeleteLookupMatches(entry, "ve_employee_1", "hub-account-2") {
		t.Fatal("expected mismatched hub account identity to be rejected")
	}
	if !platformEmployeeDeleteLookupMatches(digitalEmployeeEntry{ID: "ve_employee_1"}, "ve_employee_1", "hub-account-1") {
		t.Fatal("expected missing owner binding to keep legacy delete lookup behavior")
	}
}

func TestPlatformEmployeeDeleteLookupScoreRanksIdentityStrength(t *testing.T) {
	entry := digitalEmployeeEntry{ID: "ve_employee_1", MachineID: "ve_employee_1", OwnerUserID: "hub-account-1"}
	if got := platformEmployeeDeleteLookupScore(entry, "ve_employee_1", "hub-account-1"); got != 3 {
		t.Fatalf("full identity score=%d, want 3", got)
	}
	if got := platformEmployeeDeleteLookupScore(entry, "ve_employee_1", ""); got != 2 {
		t.Fatalf("employee identity score=%d, want 2", got)
	}
	if got := platformEmployeeDeleteLookupScore(entry, "", "hub-account-1"); got != 1 {
		t.Fatalf("account identity score=%d, want 1", got)
	}
	if got := platformEmployeeDeleteLookupScore(digitalEmployeeEntry{ID: "ve_employee_1"}, "", "hub-account-1"); got != 0 {
		t.Fatalf("weak legacy score=%d, want 0", got)
	}
}

func TestPlatformEmployeeDeleteRejectsMismatchedHubIdentity(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "hub-account-1", Status: veStatusActive}}}
	if err := saveVERegistry(context.Background(), tenantSystem, registry); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	req := newSignedPlatformJSONRequest(t, http.MethodDelete, "/api/platform/employees/platform-employee-1", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "platform_employee_id": "platform-employee-1", "hub_employee_id": "other-employee", "hub_account_id": "hub-account-1"})
	rec := httptest.NewRecorder()
	PlatformEmployeeDeleteHandler(settings, fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}, fakePlatformUserRepo{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || !bytes.Contains(rec.Body.Bytes(), []byte("EMPLOYEE_IDENTITY_MISMATCH")) {
		t.Fatalf("delete identity mismatch status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated := loadVERegistry(context.Background(), tenantSystem)
	if len(updated.Employees) != 1 {
		t.Fatalf("mismatched delete should not remove registry entry: %#v", updated.Employees)
	}
}

func TestDeletePlatformEmployeeInTenantMatchesIdentity(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{
		{ID: "ve_employee_a", MachineID: "ve_employee_a", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "hub-account-a", Status: veStatusActive},
		{ID: "ve_employee_b", MachineID: "ve_employee_b", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "hub-account-b", Status: veStatusActive},
	}}
	if err := saveVERegistry(context.Background(), tenantSystem, registry); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	removed, err := deletePlatformEmployeeInTenant(context.Background(), settings, "tenant-a", "platform-1", "platform-employee-1", "ve_employee_b", "hub-account-b")
	if err != nil {
		t.Fatalf("delete registry entry: %v", err)
	}
	if !removed {
		t.Fatal("expected matching registry entry to be removed")
	}
	updated := loadVERegistry(context.Background(), tenantSystem)
	if len(updated.Employees) != 1 || updated.Employees[0].ID != "ve_employee_a" {
		t.Fatalf("delete should keep non-matching duplicate entry: %#v", updated.Employees)
	}
}

func TestDeletePlatformEmployeeInTenantPrefersStrongIdentityMatch(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{
		{ID: "ve_employee_weak", MachineID: "ve_employee_weak", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", Status: veStatusActive},
		{ID: "ve_employee_strong", MachineID: "ve_employee_strong", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "hub-account-strong", Status: veStatusActive},
	}}
	if err := saveVERegistry(context.Background(), tenantSystem, registry); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	removed, err := deletePlatformEmployeeInTenant(context.Background(), settings, "tenant-a", "platform-1", "platform-employee-1", "", "hub-account-strong")
	if err != nil {
		t.Fatalf("delete registry entry: %v", err)
	}
	if !removed {
		t.Fatal("expected strongly matching registry entry to be removed")
	}
	updated := loadVERegistry(context.Background(), tenantSystem)
	if len(updated.Employees) != 1 || updated.Employees[0].ID != "ve_employee_weak" {
		t.Fatalf("delete should prefer strong identity match: %#v", updated.Employees)
	}
}

func TestPlatformEmployeeDeleteResolvesOwnerEmailByUserID(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "hub-account-1", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	users := &captureDeletePlatformUserRepo{fakePlatformUserRepo: fakePlatformUserRepo{items: []*store.User{{ID: "hub-account-1", TenantID: "tenant-a", Email: "worker@tenant.test", Status: "active"}}}}

	req := newSignedPlatformJSONRequest(t, http.MethodDelete, "/api/platform/employees/platform-employee-1", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "platform_employee_id": "platform-employee-1", "hub_account_id": "hub-account-1"})
	rec := httptest.NewRecorder()
	PlatformEmployeeDeleteHandler(settings, fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}, users).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	if users.deletedTenantID != "tenant-a" || users.deletedEmail != "worker@tenant.test" {
		t.Fatalf("expected delete by resolved owner email, tenant=%q email=%q", users.deletedTenantID, users.deletedEmail)
	}
}

func TestPlatformEmployeeDeleteFallsBackToRequestedVirtualEmail(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	users := &captureDeletePlatformUserRepo{fakePlatformUserRepo: fakePlatformUserRepo{items: []*store.User{{ID: "hub-account-1", TenantID: "tenant-a", Email: "worker@tenant.test", Status: "active"}}}}

	req := newSignedPlatformJSONRequest(t, http.MethodDelete, "/api/platform/employees/platform-employee-1", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "platform_employee_id": "platform-employee-1", "virtual_email": "worker@tenant.test"})
	rec := httptest.NewRecorder()
	PlatformEmployeeDeleteHandler(settings, fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}, users).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	if users.deletedTenantID != "tenant-a" || users.deletedEmail != "worker@tenant.test" {
		t.Fatalf("expected delete by requested virtual email fallback, tenant=%q email=%q", users.deletedTenantID, users.deletedEmail)
	}
}

func TestPlatformEmployeeDeleteRejectsMismatchedVirtualEmail(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "hub-account-1", OwnerEmail: "worker@tenant.test", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}

	req := newSignedPlatformJSONRequest(t, http.MethodDelete, "/api/platform/employees/platform-employee-1", "platform-1", privateKey, map[string]any{"hub_tenant_id": "tenant-a", "platform_employee_id": "platform-employee-1", "virtual_email": "other@tenant.test"})
	rec := httptest.NewRecorder()
	PlatformEmployeeDeleteHandler(settings, fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}}, fakePlatformUserRepo{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("mismatched email delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated := loadVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings))
	if len(updated.Employees) != 1 {
		t.Fatalf("mismatched email should not remove registry entry: %#v", updated.Employees)
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
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "hub-account-1", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
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

func TestPlatformEmployeeStatusDeleteRegistryRejectsMismatchedHubIdentity(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider := platformProviderEntry{PlatformID: "platform-1", PublicKeyPEM: testPlatformPublicKeyPEM(t, privateKey), RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "hub-account-1", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), tenantSystem, registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	handler := PlatformEmployeeStatusHandler(settings, fakePlatformTenantRepo{items: []*store.Tenant{{ID: "tenant-a", Slug: "tenant-a", Name: "Tenant A", Status: "active"}}})
	req := newSignedPlatformJSONRequest(t, http.MethodPost, "/api/platform/employees/platform-employee-1/status", "platform-1", privateKey, map[string]any{"platform_employee_id": "platform-employee-1", "hub_tenant_id": "tenant-a", "hub_employee_id": "other-employee", "service_status": "deleted", "delete_ve_registry": true})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || !bytes.Contains(rec.Body.Bytes(), []byte("EMPLOYEE_IDENTITY_MISMATCH")) {
		t.Fatalf("delete registry identity mismatch status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated := loadVERegistry(context.Background(), tenantSystem)
	if len(updated.Employees) != 1 || updated.Employees[0].Status != veStatusActive {
		t.Fatalf("delete registry identity mismatch should not remove employee: %#v", updated.Employees)
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
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "hub-account-1", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
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
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "hub-account-1", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
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
