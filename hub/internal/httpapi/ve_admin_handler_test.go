package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	corea2a "github.com/RapidAI/CodeClaw/corelib/a2a"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	securitypkg "github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
	"golang.org/x/sync/singleflight"
)

func tenantAdminContext(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, adminUserContextKey, &store.AdminUser{ID: "admin-tenant", Scope: "tenant", TenantID: tenantID})
}

type fakeVEMachineAuth struct {
	principals map[string]*auth.MachinePrincipal
	token      string
}

type fakeVEOwnerLookup struct {
	users map[string]*store.User
}

type fakeVEMachineLookup struct {
	machines map[string]*store.Machine
}

type fakeVEMachineEventSender struct {
	messages []sentVEMachineEvent
	err      error
}

type fakeVEMachinePresence struct {
	infos map[string]*device.MachineRuntimeInfo
}

type slowVEMachinePresence struct {
	infos   map[string]*device.MachineRuntimeInfo
	started chan struct{}
	release chan struct{}
	once    sync.Once
	mu      sync.Mutex
	count   int
}

type fakeVEVisibilityResolver struct {
	paths map[string][]string
	err   error
}

type sentVEMachineEvent struct {
	machineID string
	msg       map[string]any
}

const testAvatarPNGDataURL = "data:image/png;base64,iVBORw0KGgo="

func (f *fakeVEMachineEventSender) SendToMachine(machineID string, msg any) error {
	mapped, _ := msg.(map[string]any)
	f.messages = append(f.messages, sentVEMachineEvent{machineID: machineID, msg: mapped})
	return f.err
}

func (f fakeVEOwnerLookup) GetByID(ctx context.Context, id string) (*store.User, error) {
	_ = ctx
	return f.users[id], nil
}

func (f fakeVEMachineLookup) GetByID(ctx context.Context, id string) (*store.Machine, error) {
	_ = ctx
	return f.machines[id], nil
}

func (f fakeVEMachineAuth) AuthenticateMachine(ctx context.Context, machineID, rawToken string) (*auth.MachinePrincipal, error) {
	_ = ctx
	if rawToken != f.token {
		return nil, errors.New("bad token")
	}
	principal := f.principals[machineID]
	if principal == nil {
		return nil, errors.New("bad machine")
	}
	return principal, nil
}

func (f fakeVEMachinePresence) GetMachineInfo(ctx context.Context, machineID string) (*device.MachineRuntimeInfo, error) {
	_ = ctx
	return f.infos[machineID], nil
}

func (f *slowVEMachinePresence) GetMachineInfo(ctx context.Context, machineID string) (*device.MachineRuntimeInfo, error) {
	f.mu.Lock()
	f.count++
	f.mu.Unlock()
	f.once.Do(func() { close(f.started) })
	select {
	case <-f.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return f.infos[machineID], nil
}

func (f *slowVEMachinePresence) Count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.count
}

func (f fakeVEVisibilityResolver) RequesterGroupPath(ctx context.Context, tenantID, userID string) ([]string, error) {
	_ = ctx
	_ = tenantID
	if f.err != nil {
		return nil, f.err
	}
	return f.paths[userID], nil
}

func TestDigitalEmployeeRegisterApproveAndDiscover(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 2)
	authn := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-a": {TenantID: "tenant-a", UserID: "user-a", MachineID: "machine-a"},
			"machine-b": {TenantID: "tenant-a", UserID: "user-b", MachineID: "machine-b"},
		},
	}

	registerRR := doVEMachineJSON(t, VERegisterHandler(settings, authn), http.MethodPost, "/api/ve/register", map[string]any{
		"name":              "Legal Researcher",
		"skill_description": "Contract review",
		"avatar_data_url":   testAvatarPNGDataURL,
		"access_policy":     "public",
	}, "machine-a", "machine-token")
	if registerRR.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", registerRR.Code, registerRR.Body.String())
	}
	if !bytes.Contains(registerRR.Body.Bytes(), []byte(`"employee_type":"physical"`)) {
		t.Fatalf("registered GUI digital employee should be physical: %s", registerRR.Body.String())
	}

	statusRR := doVEMachineJSON(t, VEStatusHandler(settings, authn), http.MethodGet, "/api/ve/status", nil, "machine-a", "machine-token")
	if statusRR.Code != http.StatusOK || !bytes.Contains(statusRR.Body.Bytes(), []byte(`"registered":true`)) || !bytes.Contains(statusRR.Body.Bytes(), []byte(`"status":"pending"`)) {
		t.Fatalf("unexpected status response: status=%d body=%s", statusRR.Code, statusRR.Body.String())
	}

	approveReq := httptest.NewRequest(http.MethodPost, "/api/ve/ve_machine-a/approve", nil)
	approveReq = approveReq.WithContext(tenantAdminContext(approveReq.Context(), "tenant-a"))
	approveReq.SetPathValue("id", "ve_machine-a")
	approveRec := httptest.NewRecorder()
	VEAdminActionHandler(settings, "approve").ServeHTTP(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve status=%d body=%s", approveRec.Code, approveRec.Body.String())
	}

	discoverRR := doVEMachineJSON(t, VEDiscoverableHandler(settings, authn), http.MethodGet, "/api/ve/discoverable", nil, "machine-b", "machine-token")
	if discoverRR.Code != http.StatusOK || !bytes.Contains(discoverRR.Body.Bytes(), []byte(`"id":"ve_machine-a"`)) || !bytes.Contains(discoverRR.Body.Bytes(), []byte(`"avatar_data_url":"`+testAvatarPNGDataURL+`"`)) {
		t.Fatalf("discover status=%d body=%s", discoverRR.Code, discoverRR.Body.String())
	}
}

func TestDigitalEmployeeRegisterRejectsInvalidAvatarDataURL(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 2)
	authn := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-a": {TenantID: "tenant-a", UserID: "user-a", MachineID: "machine-a"},
		},
	}

	for _, avatarDataURL := range []string{
		"javascript:alert(1)",
		"data:image/png;base64,QUJD",
		strings.Replace(testAvatarPNGDataURL, "data:image/png", "data:image/jpeg", 1),
		"data:image/png;base64,%%%",
		"data:image/png;base64,QR==",
		"data:image/png;base64," + strings.Repeat("A", veAvatarDataURLMaxSize),
	} {
		t.Run(avatarDataURL, func(t *testing.T) {
			rr := doVEMachineJSON(t, VERegisterHandler(settings, authn), http.MethodPost, "/api/ve/register", map[string]any{
				"name":              "Legal Researcher",
				"skill_description": "Contract review",
				"avatar_data_url":   avatarDataURL,
				"access_policy":     "public",
			}, "machine-a", "machine-token")
			if rr.Code != http.StatusBadRequest || !bytes.Contains(rr.Body.Bytes(), []byte(`"code":"INVALID_INPUT"`)) {
				t.Fatalf("register status=%d body=%s", rr.Code, rr.Body.String())
			}
		})
	}
}

func TestDigitalEmployeeRegisterAcceptsOneMiBAvatarDataURL(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 2)
	authn := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-a": {TenantID: "tenant-a", UserID: "user-a", MachineID: "machine-a"},
		},
	}
	payload := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, make([]byte, veAvatarImageMaxBytes-8)...)
	avatar := "data:image/png;base64," + base64.StdEncoding.EncodeToString(payload)
	rr := doVEMachineJSON(t, VERegisterHandler(settings, authn), http.MethodPost, "/api/ve/register", map[string]any{
		"name":              "Legal Researcher",
		"skill_description": "Contract review",
		"avatar_data_url":   avatar,
		"access_policy":     "public",
	}, "machine-a", "machine-token")
	if rr.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestLoadVERegistryDropsInvalidAvatarDataURL(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	seed := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{
		{ID: "ve_bad", MachineID: "machine-bad", AvatarDataURL: "data:image/png;base64,QUJD"},
		{ID: "ve_huge", MachineID: "machine-huge", AvatarDataURL: "data:image/png;base64," + strings.Repeat("A", veAvatarDataURLMaxSize)},
		{ID: "ve_good", MachineID: "machine-good", AvatarDataURL: " " + testAvatarPNGDataURL + " "},
	}}
	data, err := json.Marshal(seed)
	if err != nil {
		t.Fatalf("marshal seed: %v", err)
	}
	if err := settings.Set(context.Background(), veRegistryKey, string(data)); err != nil {
		t.Fatalf("seed registry: %v", err)
	}

	registry := loadVERegistry(context.Background(), settings)
	badIdx := registry.findByIDOrMachineIDOrPlatformEmployeeID("ve_bad")
	goodIdx := registry.findByIDOrMachineIDOrPlatformEmployeeID("ve_good")
	if badIdx < 0 || registry.Employees[badIdx].AvatarDataURL != "" {
		t.Fatalf("invalid avatar should be cleared: %+v", registry.Employees)
	}
	hugeIdx := registry.findByIDOrMachineIDOrPlatformEmployeeID("ve_huge")
	if hugeIdx < 0 || registry.Employees[hugeIdx].AvatarDataURL != "" {
		t.Fatalf("oversized avatar should be cleared: %+v", registry.Employees)
	}
	if goodIdx < 0 || registry.Employees[goodIdx].AvatarDataURL != testAvatarPNGDataURL {
		t.Fatalf("valid avatar should be trimmed and preserved: %+v", registry.Employees)
	}
}

func TestDigitalEmployeeRegistryLookupMatchesGeneratedAliases(t *testing.T) {
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_machine-a", MachineID: "machine-a", PlatformEmployeeID: "platform-a"}}}

	if idx := registry.findByMachineID("ve-machine-a"); idx != 0 {
		t.Fatalf("findByMachineID alias idx=%d, want 0", idx)
	}
	if idx := registry.findByID("ve-machine-a"); idx != 0 {
		t.Fatalf("findByID dash alias idx=%d, want 0", idx)
	}
	if idx := registry.findByIDOrMachineID("machine-a"); idx != 0 {
		t.Fatalf("findByIDOrMachineID machine idx=%d, want 0", idx)
	}
	if idx := registry.findByIDOrMachineIDOrPlatformEmployeeID("ve-machine-a"); idx != 0 {
		t.Fatalf("findByIDOrMachineIDOrPlatformEmployeeID alias idx=%d, want 0", idx)
	}
	if idx := registry.findByIDOrMachineIDOrPlatformEmployeeID("platform-a"); idx != 0 {
		t.Fatalf("findByIDOrMachineIDOrPlatformEmployeeID platform idx=%d, want 0", idx)
	}
	if idx := registry.findByPlatformEmployeeID("ve-machine-a"); idx >= 0 {
		t.Fatalf("platform employee id should stay exact, got idx=%d", idx)
	}
}

func TestReusableVEDirectSessionMatchesParticipantAliases(t *testing.T) {
	session := &corea2a.Session{Status: corea2a.SessionOpen, Participants: []corea2a.Participant{
		{ID: "machine-b", RoleCode: "initiator"},
		{ID: "machine-a", RoleCode: "speak"},
	}}

	if !isReusableVEDirectSession(session, "ve-machine-b", "ve_machine-a") {
		t.Fatal("expected direct session reuse to match generated participant aliases")
	}

	session.Participants = append(session.Participants, corea2a.Participant{ID: "ve-machine-a", RoleCode: "speak"})
	if !isReusableVEDirectSession(session, "ve-machine-b", "ve_machine-a") {
		t.Fatal("expected duplicate participant aliases to still be reusable as a direct session")
	}
}

func TestUpsertVEAccessRequestMatchesMachineAliases(t *testing.T) {
	requests := &digitalEmployeeAccessRequestStore{Requests: []digitalEmployeeAccessRequest{{
		ID:                 "req-existing",
		Status:             "pending",
		RequesterMachineID: "machine-b",
		TargetMachineID:    "machine-a",
		CreatedAt:          "created",
	}}}

	updated := upsertVEAccessRequest(requests, digitalEmployeeAccessRequest{
		ID:                 "req-new",
		Status:             "pending",
		RequesterMachineID: "ve-machine-b",
		TargetMachineID:    "ve_machine-a",
	})

	if updated.ID != "req-existing" || updated.CreatedAt != "created" || len(requests.Requests) != 1 {
		t.Fatalf("expected alias request to update existing pending request, got updated=%+v requests=%+v", updated, requests.Requests)
	}
}

func TestDigitalEmployeeRegisterAutoApprove(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 2)
	if err := scopedSystemSettingsForTenant("tenant-a", settings).Set(context.Background(), veGroupConfigKey, `{"max_group_participants":5,"auto_approve":true}`); err != nil {
		t.Fatalf("set ve config: %v", err)
	}
	authn := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-a": {TenantID: "tenant-a", UserID: "user-a", MachineID: "machine-a"},
			"machine-b": {TenantID: "tenant-a", UserID: "user-b", MachineID: "machine-b"},
		},
	}

	registerRR := doVEMachineJSON(t, VERegisterHandler(settings, authn), http.MethodPost, "/api/ve/register", map[string]any{
		"name":              "Auto Worker",
		"skill_description": "Auto approval",
		"access_policy":     "public",
	}, "machine-a", "machine-token")
	if registerRR.Code != http.StatusOK || !bytes.Contains(registerRR.Body.Bytes(), []byte(`"status":"active"`)) {
		t.Fatalf("register status=%d body=%s", registerRR.Code, registerRR.Body.String())
	}

	discoverRR := doVEMachineJSON(t, VEDiscoverableHandler(settings, authn), http.MethodGet, "/api/ve/discoverable", nil, "machine-b", "machine-token")
	if discoverRR.Code != http.StatusOK || !bytes.Contains(discoverRR.Body.Bytes(), []byte(`"id":"ve_machine-a"`)) {
		t.Fatalf("discover status=%d body=%s", discoverRR.Code, discoverRR.Body.String())
	}
}

func TestDigitalEmployeeRegisterAutoApproveRespectsQuota(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 1)
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	if err := tenantSystem.Set(context.Background(), veGroupConfigKey, `{"max_group_participants":5,"auto_approve":true}`); err != nil {
		t.Fatalf("set ve config: %v", err)
	}
	seed := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_machine-a", MachineID: "machine-a", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	data, err := json.Marshal(seed)
	if err != nil {
		t.Fatalf("marshal registry: %v", err)
	}
	if err := tenantSystem.Set(context.Background(), veRegistryKey, string(data)); err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	authn := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-b": {TenantID: "tenant-a", UserID: "user-b", MachineID: "machine-b"},
		},
	}

	registerRR := doVEMachineJSON(t, VERegisterHandler(settings, authn), http.MethodPost, "/api/ve/register", map[string]any{"name": "Overflow Worker"}, "machine-b", "machine-token")
	if registerRR.Code != http.StatusOK || !bytes.Contains(registerRR.Body.Bytes(), []byte(`"status":"pending"`)) {
		t.Fatalf("register status=%d body=%s", registerRR.Code, registerRR.Body.String())
	}
}

func TestDigitalEmployeeRegisterAutoApproveDoesNotBypassDisabledEmployee(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 2)
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	if err := tenantSystem.Set(context.Background(), veGroupConfigKey, `{"max_group_participants":5,"auto_approve":true}`); err != nil {
		t.Fatalf("set ve config: %v", err)
	}
	seed := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_machine-a", MachineID: "machine-a", Status: veStatusDisabled, OnlineStatus: veOnlineStatusOffline}}}
	data, err := json.Marshal(seed)
	if err != nil {
		t.Fatalf("marshal registry: %v", err)
	}
	if err := tenantSystem.Set(context.Background(), veRegistryKey, string(data)); err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	authn := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-a": {TenantID: "tenant-a", UserID: "user-a", MachineID: "machine-a"},
		},
	}

	registerRR := doVEMachineJSON(t, VERegisterHandler(settings, authn), http.MethodPost, "/api/ve/register", map[string]any{"name": "Disabled Worker"}, "machine-a", "machine-token")
	if registerRR.Code != http.StatusOK || !bytes.Contains(registerRR.Body.Bytes(), []byte(`"status":"pending"`)) {
		t.Fatalf("register status=%d body=%s", registerRR.Code, registerRR.Body.String())
	}
}

func TestDigitalEmployeeDiscoverableExcludesOwnMachineCaseInsensitive(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 2)
	authn := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-a": {TenantID: "tenant-a", UserID: "user-a", MachineID: "machine-a"},
			"MACHINE-A": {TenantID: "tenant-a", UserID: "user-a", MachineID: "MACHINE-A"},
		},
	}

	registerRR := doVEMachineJSON(t, VERegisterHandler(settings, authn), http.MethodPost, "/api/ve/register", map[string]any{
		"name":              "Legal Researcher",
		"skill_description": "Contract review",
		"access_policy":     "public",
	}, "machine-a", "machine-token")
	if registerRR.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", registerRR.Code, registerRR.Body.String())
	}

	approveReq := httptest.NewRequest(http.MethodPost, "/api/ve/ve_machine-a/approve", nil)
	approveReq = approveReq.WithContext(tenantAdminContext(approveReq.Context(), "tenant-a"))
	approveReq.SetPathValue("id", "ve_machine-a")
	approveRec := httptest.NewRecorder()
	VEAdminActionHandler(settings, "approve").ServeHTTP(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve status=%d body=%s", approveRec.Code, approveRec.Body.String())
	}

	discoverRR := doVEMachineJSON(t, VEDiscoverableHandler(settings, authn), http.MethodGet, "/api/ve/discoverable", nil, "MACHINE-A", "machine-token")
	if discoverRR.Code != http.StatusOK {
		t.Fatalf("discover status=%d body=%s", discoverRR.Code, discoverRR.Body.String())
	}
	if bytes.Contains(discoverRR.Body.Bytes(), []byte(`"id":"ve_machine-a"`)) {
		t.Fatalf("own digital employee leaked in discover response: %s", discoverRR.Body.String())
	}
}

func TestDiscoverableShowsActiveMaClawSrvRuntimeEmployeeOnline(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 2)
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/platform/runtime/report" {
			t.Fatalf("unexpected runtime path: %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ok",
			"users":  []map[string]any{{"employee_id": "srv-user-1", "runtime_status": "ready"}},
		})
	}))
	defer runtime.Close()
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: runtime.URL, TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{
		{ID: "ve_srv", MachineID: "ve_srv", PlatformID: maclawSrvRuntimePlatformID, PlatformEmployeeID: "srv-user-1", Name: "Srv Employee", Status: veStatusActive},
		{ID: "ve_physical", MachineID: "machine-offline", Name: "Physical Offline", Status: veStatusActive},
	}}
	data, err := json.Marshal(registry)
	if err != nil {
		t.Fatalf("marshal registry: %v", err)
	}
	if err := tenantSystem.Set(context.Background(), veRegistryKey, string(data)); err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	authn := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-gui": {TenantID: "tenant-a", UserID: "user-gui", MachineID: "machine-gui"},
		},
	}

	discoverRR := doVEMachineJSON(t, VEDiscoverableHandler(settings, authn), http.MethodGet, "/api/ve/discoverable", nil, "machine-gui", "machine-token")
	if discoverRR.Code != http.StatusOK {
		t.Fatalf("discover status=%d body=%s", discoverRR.Code, discoverRR.Body.String())
	}
	var out struct {
		Employees []digitalEmployeeEntry `json:"employees"`
	}
	if err := json.Unmarshal(discoverRR.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode discover: %v", err)
	}
	byID := map[string]digitalEmployeeEntry{}
	for _, emp := range out.Employees {
		byID[emp.ID] = emp
	}
	if byID["ve_srv"].OnlineStatus != veOnlineStatusOnline {
		t.Fatalf("MaClawSrv runtime employee online_status = %q, want online; body=%s", byID["ve_srv"].OnlineStatus, discoverRR.Body.String())
	}
	if _, ok := byID["ve_physical"]; ok {
		t.Fatalf("physical employee without heartbeat should be hidden from discoverable list: %s", discoverRR.Body.String())
	}
}

func TestDiscoverableUsesRuntimePresenceForPhysicalEmployees(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 3)
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{
		{ID: "ve_stale_online", MachineID: "machine-stale", EmployeeType: veEmployeeTypePhysical, Name: "Stale Online", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline},
		{ID: "ve_live_online", MachineID: "machine-live", EmployeeType: veEmployeeTypePhysical, Name: "Live Online", Status: veStatusActive, OnlineStatus: veOnlineStatusOffline},
	}}
	data, err := json.Marshal(registry)
	if err != nil {
		t.Fatalf("marshal registry: %v", err)
	}
	if err := tenantSystem.Set(context.Background(), veRegistryKey, string(data)); err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	authn := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-gui": {TenantID: "tenant-a", UserID: "user-gui", MachineID: "machine-gui"},
		},
	}
	presence := fakeVEMachinePresence{infos: map[string]*device.MachineRuntimeInfo{
		"machine-live": {MachineID: "machine-live", Online: true},
	}}

	discoverRR := doVEMachineJSON(t, VEDiscoverableHandler(settings, authn, presence), http.MethodGet, "/api/ve/discoverable", nil, "machine-gui", "machine-token")
	if discoverRR.Code != http.StatusOK {
		t.Fatalf("discover status=%d body=%s", discoverRR.Code, discoverRR.Body.String())
	}
	var out struct {
		Employees []digitalEmployeeEntry `json:"employees"`
	}
	if err := json.Unmarshal(discoverRR.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode discover: %v", err)
	}
	byID := map[string]digitalEmployeeEntry{}
	for _, emp := range out.Employees {
		byID[emp.ID] = emp
	}
	if _, ok := byID["ve_stale_online"]; ok {
		t.Fatalf("stale physical employee should be hidden from discoverable list: %s", discoverRR.Body.String())
	}
	if byID["ve_live_online"].OnlineStatus != veOnlineStatusOnline {
		t.Fatalf("live physical online_status = %q, want online; body=%s", byID["ve_live_online"].OnlineStatus, discoverRR.Body.String())
	}
}

func TestDiscoverableFiltersByVisibleGroups(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 4)
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{
		{ID: "ve_global", MachineID: "machine-global", Name: "Global", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline},
		{ID: "ve_legal", MachineID: "machine-legal", Name: "Legal", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline, VisibleGroupIDs: []string{"dept-legal"}},
		{ID: "ve_finance", MachineID: "machine-finance", Name: "Finance", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline, VisibleGroupIDs: []string{"dept-finance"}},
	}}
	if err := saveVERegistry(context.Background(), tenantSystem, registry); err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	authn := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-gui": {TenantID: "tenant-a", UserID: "user-gui", MachineID: "machine-gui"},
		},
	}
	visibility := fakeVEVisibilityResolver{paths: map[string][]string{
		"user-gui": []string{"root", "dept-legal", "dept-legal-child"},
	}}

	discoverRR := doVEMachineJSON(t, VEDiscoverableHandler(settings, authn, visibility), http.MethodGet, "/api/ve/discoverable", nil, "machine-gui", "machine-token")
	if discoverRR.Code != http.StatusOK {
		t.Fatalf("discover status=%d body=%s", discoverRR.Code, discoverRR.Body.String())
	}
	var out struct {
		Employees []digitalEmployeeEntry `json:"employees"`
	}
	if err := json.Unmarshal(discoverRR.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode discover: %v", err)
	}
	byID := map[string]bool{}
	for _, employee := range out.Employees {
		byID[employee.ID] = true
	}
	if !byID["ve_global"] || !byID["ve_legal"] {
		t.Fatalf("expected global and matching group employees, got %#v body=%s", byID, discoverRR.Body.String())
	}
	if byID["ve_finance"] {
		t.Fatalf("non-matching group employee leaked: %s", discoverRR.Body.String())
	}
}

func TestDiscoverableKeepsGlobalEmployeesWhenVisibilityResolverFails(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 4)
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{
		{ID: "ve_global", MachineID: "machine-global", Name: "Global", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline},
		{ID: "ve_legal", MachineID: "machine-legal", Name: "Legal", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline, VisibleGroupIDs: []string{"dept-legal"}},
	}}
	if err := saveVERegistry(context.Background(), tenantSystem, registry); err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	authn := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-gui": {TenantID: "tenant-a", UserID: "user-gui", MachineID: "machine-gui"},
		},
	}
	visibility := fakeVEVisibilityResolver{err: errors.New("group lookup failed")}

	discoverRR := doVEMachineJSON(t, VEDiscoverableHandler(settings, authn, visibility), http.MethodGet, "/api/ve/discoverable", nil, "machine-gui", "machine-token")
	if discoverRR.Code != http.StatusOK {
		t.Fatalf("discover status=%d body=%s", discoverRR.Code, discoverRR.Body.String())
	}
	var out struct {
		Employees []digitalEmployeeEntry `json:"employees"`
	}
	if err := json.Unmarshal(discoverRR.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode discover: %v", err)
	}
	byID := map[string]bool{}
	for _, employee := range out.Employees {
		byID[employee.ID] = true
	}
	if !byID["ve_global"] {
		t.Fatalf("global employee should remain visible: %s", discoverRR.Body.String())
	}
	if byID["ve_legal"] {
		t.Fatalf("restricted employee leaked when resolver failed: %s", discoverRR.Body.String())
	}
}

func TestVERegistryHasVisibleGroupRestrictions(t *testing.T) {
	if veRegistryHasVisibleGroupRestrictions(digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{
		{ID: "ve_global", Status: veStatusActive, VisibleGroupIDs: nil},
		{ID: "ve_blank", Status: veStatusActive, VisibleGroupIDs: []string{"  "}},
		{ID: "ve_pending", Status: veStatusPending, VisibleGroupIDs: []string{"dept-legal"}},
	}}) {
		t.Fatal("blank or non-active visible groups should not be treated as restricted")
	}
	if !veRegistryHasVisibleGroupRestrictions(digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{
		{ID: "ve_restricted", Status: veStatusActive, VisibleGroupIDs: []string{"dept-legal"}},
	}}) {
		t.Fatal("non-empty visible group should be treated as restricted")
	}
}

func TestVESecurityVisibilityResolverRejectsCrossTenantUserLookup(t *testing.T) {
	resolver := veSecurityVisibilityResolver{
		users: fakeVEOwnerLookup{users: map[string]*store.User{
			"user-a": {ID: "user-a", TenantID: "tenant-b", Email: "user@example.com"},
		}},
	}
	if _, err := resolver.RequesterGroupPath(context.Background(), "tenant-a", "user-a"); err == nil {
		t.Fatal("expected tenant mismatch to reject user lookup")
	}
}

func TestVESecurityVisibilityResolverRejectsMissingRequesterEmail(t *testing.T) {
	resolver := veSecurityVisibilityResolver{
		users: fakeVEOwnerLookup{users: map[string]*store.User{}},
	}
	if _, err := resolver.RequesterGroupPath(context.Background(), "tenant-a", "user-a"); err == nil {
		t.Fatal("expected missing requester email to be rejected")
	}
}

func TestVEAdminListIncludesEmployeeTypes(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 3)
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{
		{ID: "ve_gui", MachineID: "machine-gui", Name: "GUI Employee", Status: veStatusPending, OnlineStatus: veOnlineStatusOnline},
		{ID: "ve_stale_platform", MachineID: "ve_stale_platform", PlatformID: "platform-1", Name: "Stale Platform Field", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline},
		{ID: "ve_srv", MachineID: "ve_srv", PlatformID: "maclawsrv", PlatformEmployeeID: "srv-user-1", Name: "Srv Employee", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline},
		{ID: "ve_runtime", MachineID: "ve_runtime", RuntimeProviderID: "maclawsrv", Name: "Runtime Employee", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline},
		{ID: "ve_other_runtime", MachineID: "ve_other_runtime", RuntimeProviderID: "other-runtime", Name: "Other Runtime Employee", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline},
		{ID: "ve_bad", MachineID: "machine-bad", EmployeeType: "legacy", Name: "Legacy Employee", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline},
		{ID: "ve_conflict", MachineID: "ve_conflict", EmployeeType: veEmployeeTypePhysical, PlatformID: "maclawsrv", PlatformEmployeeID: "srv-user-2", Name: "Conflict Employee", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline},
	}}
	seed, err := json.Marshal(registry)
	if err != nil {
		t.Fatalf("marshal registry: %v", err)
	}
	if err := tenantSystem.Set(context.Background(), veRegistryKey, string(seed)); err != nil {
		t.Fatalf("seed registry: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/ve/list", nil)
	req = req.WithContext(tenantAdminContext(req.Context(), "tenant-a"))
	rec := httptest.NewRecorder()
	VEAdminListHandler(settings, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Employees []digitalEmployeeEntry `json:"employees"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	typesByID := map[string]string{}
	for _, employee := range out.Employees {
		typesByID[employee.ID] = employee.EmployeeType
	}
	if typesByID["ve_gui"] != veEmployeeTypePhysical || typesByID["ve_stale_platform"] != veEmployeeTypePhysical || typesByID["ve_bad"] != veEmployeeTypePhysical || typesByID["ve_other_runtime"] != veEmployeeTypePhysical || typesByID["ve_srv"] != veEmployeeTypeVirtual || typesByID["ve_runtime"] != veEmployeeTypePhysical || typesByID["ve_conflict"] != veEmployeeTypePhysical {
		t.Fatalf("unexpected employee types: %#v", typesByID)
	}
	raw, err := tenantSystem.Get(context.Background(), veRegistryKey)
	if err != nil {
		t.Fatalf("load repaired registry: %v", err)
	}
	var repaired digitalEmployeeRegistry
	if err := json.Unmarshal([]byte(raw), &repaired); err != nil {
		t.Fatalf("decode repaired registry: %v", err)
	}
	repairedTypesByID := map[string]string{}
	for _, employee := range repaired.Employees {
		repairedTypesByID[employee.ID] = employee.EmployeeType
	}
	if repairedTypesByID["ve_bad"] != veEmployeeTypePhysical || repairedTypesByID["ve_stale_platform"] != veEmployeeTypePhysical || repairedTypesByID["ve_other_runtime"] != veEmployeeTypePhysical || repairedTypesByID["ve_conflict"] != veEmployeeTypePhysical || repairedTypesByID["ve_runtime"] != veEmployeeTypePhysical {
		t.Fatalf("registry employee types were not repaired in storage: %s", raw)
	}
}

func TestVEAdminListUsesRuntimePresenceForPhysicalEmployees(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 3)
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{
		{ID: "ve_stale_online", MachineID: "machine-stale", EmployeeType: veEmployeeTypePhysical, Name: "Stale Online", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline},
		{ID: "ve_live_online", MachineID: "machine-live", EmployeeType: veEmployeeTypePhysical, Name: "Live Online", Status: veStatusActive, OnlineStatus: veOnlineStatusOffline},
		{ID: "ve_srv", MachineID: "ve_srv", PlatformID: maclawSrvRuntimePlatformID, PlatformEmployeeID: "srv-user-1", Name: "Runtime Employee", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline},
	}}
	data, err := json.Marshal(registry)
	if err != nil {
		t.Fatalf("marshal registry: %v", err)
	}
	if err := tenantSystem.Set(context.Background(), veRegistryKey, string(data)); err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	presence := fakeVEMachinePresence{infos: map[string]*device.MachineRuntimeInfo{
		"machine-live": {MachineID: "machine-live", Online: true},
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/ve/list", nil)
	req = req.WithContext(tenantAdminContext(req.Context(), "tenant-a"))
	rec := httptest.NewRecorder()
	VEAdminListHandler(settings, presence).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Employees []digitalEmployeeEntry `json:"employees"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	byID := map[string]digitalEmployeeEntry{}
	for _, emp := range out.Employees {
		byID[emp.ID] = emp
	}
	if byID["ve_stale_online"].OnlineStatus != veOnlineStatusOffline {
		t.Fatalf("stale physical online_status = %q, want offline; body=%s", byID["ve_stale_online"].OnlineStatus, rec.Body.String())
	}
	if byID["ve_live_online"].OnlineStatus != veOnlineStatusOnline {
		t.Fatalf("live physical online_status = %q, want online; body=%s", byID["ve_live_online"].OnlineStatus, rec.Body.String())
	}
	if byID["ve_srv"].OnlineStatus != veOnlineStatusOffline {
		t.Fatalf("runtime employee without runtime report online_status = %q, want offline", byID["ve_srv"].OnlineStatus)
	}
	if byID["ve_srv"].Status != veStatusActive {
		t.Fatalf("runtime employee without runtime report status = %q, want active", byID["ve_srv"].Status)
	}
}

func TestVEAdminListUsesMacLawSrvRuntimeReportForVirtualEmployees(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 3)
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/platform/runtime/report" {
			t.Fatalf("unexpected runtime path: %s", r.URL.Path)
		}
		if got := r.Header.Get("X-Hub-Tenant-ID"); got != "tenant-a" {
			t.Fatalf("runtime report tenant header = %q, want tenant-a", got)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ok",
			"users": []map[string]any{
				{"employee_id": "live-employee", "runtime_status": "ready"},
				{"employee_id": "attention-employee", "runtime_status": "attention"},
			},
		})
	}))
	defer runtime.Close()
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: runtime.URL, TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{
		{ID: "ve_live", MachineID: "ve_live", PlatformID: maclawSrvRuntimePlatformID, PlatformEmployeeID: "live-employee", Name: "Live Runtime", Status: veStatusActive, OnlineStatus: veOnlineStatusOffline},
		{ID: "ve_attention", MachineID: "ve_attention", PlatformID: maclawSrvRuntimePlatformID, PlatformEmployeeID: "attention-employee", Name: "Attention Runtime", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline},
		{ID: "ve_deleted", MachineID: "ve_deleted", PlatformID: maclawSrvRuntimePlatformID, PlatformEmployeeID: "deleted-employee", Name: "Deleted Runtime", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline},
	}}
	if err := saveVERegistry(context.Background(), tenantSystem, registry); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/ve/list", nil)
	req = req.WithContext(tenantAdminContext(req.Context(), "tenant-a"))
	rec := httptest.NewRecorder()
	VEAdminListHandler(settings, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Employees []digitalEmployeeEntry `json:"employees"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	byID := map[string]digitalEmployeeEntry{}
	for _, emp := range out.Employees {
		byID[emp.ID] = emp
	}
	if byID["ve_live"].OnlineStatus != veOnlineStatusOnline {
		t.Fatalf("live runtime online_status = %q, want online; body=%s", byID["ve_live"].OnlineStatus, rec.Body.String())
	}
	if byID["ve_deleted"].OnlineStatus != veOnlineStatusOffline {
		t.Fatalf("deleted runtime online_status = %q, want offline; body=%s", byID["ve_deleted"].OnlineStatus, rec.Body.String())
	}
	if byID["ve_deleted"].Status != veStatusDisabled {
		t.Fatalf("deleted runtime status = %q, want disabled; body=%s", byID["ve_deleted"].Status, rec.Body.String())
	}
	if !byID["ve_deleted"].RuntimeMissing || !byID["ve_deleted"].HistoryRetained {
		t.Fatalf("deleted runtime should be marked runtime_missing/history_retained: %+v", byID["ve_deleted"])
	}
	if byID["ve_attention"].OnlineStatus != veOnlineStatusOffline {
		t.Fatalf("attention runtime online_status = %q, want offline; body=%s", byID["ve_attention"].OnlineStatus, rec.Body.String())
	}
	if byID["ve_attention"].Status != veStatusActive {
		t.Fatalf("attention runtime status = %q, want active; body=%s", byID["ve_attention"].Status, rec.Body.String())
	}
}

func TestVEAdminListKeepsExplicitPhysicalMacLawSrvEmployeePhysical(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 1)
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/platform/runtime/report" {
			t.Fatalf("unexpected runtime path: %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "users": []map[string]any{}})
	}))
	defer runtime.Close()
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: runtime.URL, TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{
		ID:                 "ve_physical",
		MachineID:          "machine-physical",
		EmployeeType:       veEmployeeTypePhysical,
		PlatformID:         maclawSrvRuntimePlatformID,
		PlatformEmployeeID: "stale-platform-employee",
		RuntimeProviderID:  maclawSrvRuntimePlatformID,
		Name:               "Physical Worker",
		Status:             veStatusActive,
		OnlineStatus:       veOnlineStatusOnline,
	}}}
	if err := saveVERegistry(context.Background(), tenantSystem, registry); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/ve/list", nil)
	req = req.WithContext(tenantAdminContext(req.Context(), "tenant-a"))
	rec := httptest.NewRecorder()
	VEAdminListHandler(settings, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Employees []digitalEmployeeEntry `json:"employees"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(out.Employees) != 1 || out.Employees[0].Status != veStatusActive || out.Employees[0].EmployeeType != veEmployeeTypePhysical {
		t.Fatalf("explicit physical employee should not be disabled by runtime report: %#v body=%s", out.Employees, rec.Body.String())
	}
}

func TestVEAdminListSkipsMacLawSrvRuntimeReportWithoutRuntimeEmployees(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 1)
	runtimeCalled := false
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimeCalled = true
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "users": []map[string]any{}})
	}))
	defer runtime.Close()
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: runtime.URL, TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{
		{ID: "ve_physical", MachineID: "machine-physical", EmployeeType: veEmployeeTypePhysical, Name: "Physical", Status: veStatusActive, OnlineStatus: veOnlineStatusOffline},
	}}
	if err := saveVERegistry(context.Background(), tenantSystem, registry); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/ve/list", nil)
	req = req.WithContext(tenantAdminContext(req.Context(), "tenant-a"))
	rec := httptest.NewRecorder()
	VEAdminListHandler(settings, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	if runtimeCalled {
		t.Fatalf("runtime report was called for registry without MaClawSrv runtime employees")
	}
}

func TestMacLawSrvRuntimePresenceCacheCopiesAndEvicts(t *testing.T) {
	macLawSrvRuntimePresenceCache.Lock()
	macLawSrvRuntimePresenceCache.items = map[string]cachedMacLawSrvRuntimePresence{}
	macLawSrvRuntimePresenceCache.Unlock()
	t.Cleanup(func() {
		macLawSrvRuntimePresenceCache.Lock()
		macLawSrvRuntimePresenceCache.items = map[string]cachedMacLawSrvRuntimePresence{}
		macLawSrvRuntimePresenceCache.Unlock()
	})

	key := "runtime\x00tenant-a"
	presence := macLawSrvRuntimePresence{Loaded: true, Reported: map[string]bool{"emp-1": true}, Ready: map[string]bool{"emp-1": true}}
	setCachedMacLawSrvRuntimePresence(key, presence, time.Now().Add(time.Minute))
	presence.Ready["emp-1"] = false
	cached, ok := getCachedMacLawSrvRuntimePresence(key, time.Now())
	if !ok || !cached.Ready["emp-1"] {
		t.Fatalf("cache did not isolate stored presence: ok=%v cached=%#v", ok, cached)
	}
	cached.Ready["emp-1"] = false
	cachedAgain, ok := getCachedMacLawSrvRuntimePresence(key, time.Now())
	if !ok || !cachedAgain.Ready["emp-1"] {
		t.Fatalf("cache did not isolate returned presence: ok=%v cached=%#v", ok, cachedAgain)
	}

	for i := 0; i < macLawSrvRuntimePresenceCacheMaxItems+10; i++ {
		setCachedMacLawSrvRuntimePresence(fmt.Sprintf("runtime-%03d\x00tenant-a", i), macLawSrvRuntimePresence{Loaded: true, Reported: map[string]bool{"emp": true}, Ready: map[string]bool{"emp": true}}, time.Now().Add(time.Duration(i+1)*time.Second))
	}
	macLawSrvRuntimePresenceCache.Lock()
	size := len(macLawSrvRuntimePresenceCache.items)
	macLawSrvRuntimePresenceCache.Unlock()
	if size > macLawSrvRuntimePresenceCacheMaxItems {
		t.Fatalf("cache size = %d, want <= %d", size, macLawSrvRuntimePresenceCacheMaxItems)
	}
}

func TestMacLawSrvRuntimePresenceCacheKeyIncludesSecretFingerprint(t *testing.T) {
	base := macLawSrvRuntimeEntry{BaseURL: "https://runtime.example/", AdminSecret: "secret-a"}
	same := macLawSrvRuntimeEntry{BaseURL: "https://runtime.example", AdminSecret: "secret-a"}
	rotated := macLawSrvRuntimeEntry{BaseURL: "https://runtime.example", AdminSecret: "secret-b"}

	key := macLawSrvRuntimePresenceCacheKey(base, "tenant-a")
	if key != macLawSrvRuntimePresenceCacheKey(same, "tenant-a") {
		t.Fatalf("cache key should normalize equivalent base URLs")
	}
	if key == macLawSrvRuntimePresenceCacheKey(rotated, "tenant-a") {
		t.Fatalf("cache key should change after admin secret rotation")
	}
	if strings.Contains(key, "secret-a") {
		t.Fatalf("cache key leaked admin secret: %q", key)
	}
}

func TestVEAdminVisibilityHandlerUpdatesVisibleGroups(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 1)
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	securitySvc, _ := newSecurityHandlerTestService(t, "ve-visibility.db")
	securityCtx := securitypkg.WithTenant(t.Context(), "tenant-a")
	root, err := securitySvc.GetGroupTree(securityCtx)
	if err != nil || root == nil {
		t.Fatalf("load security root: %v", err)
	}
	legal, err := securitySvc.CreateGroup(securityCtx, "Legal", root.ID)
	if err != nil {
		t.Fatalf("create legal group: %v", err)
	}
	finance, err := securitySvc.CreateGroup(securityCtx, "Finance", root.ID)
	if err != nil {
		t.Fatalf("create finance group: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{
		{ID: "ve_legal", MachineID: "machine-legal", Name: "Legal", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline},
	}}
	if err := saveVERegistry(context.Background(), tenantSystem, registry); err != nil {
		t.Fatalf("seed registry: %v", err)
	}

	reqBody, err := json.Marshal(map[string][]string{"visible_group_ids": []string{" " + legal.ID + " ", legal.ID, finance.ID}})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/ve/ve_legal/visibility", strings.NewReader(string(reqBody)))
	req = req.WithContext(tenantAdminContext(req.Context(), "tenant-a"))
	req.SetPathValue("id", "ve_legal")
	rec := httptest.NewRecorder()
	VEAdminVisibilityHandler(settings, securitySvc).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("visibility status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated := loadVERegistry(context.Background(), tenantSystem)
	got := updated.Employees[0].VisibleGroupIDs
	want := []string{legal.ID, finance.ID}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("visible groups = %#v, want %#v", got, want)
	}

	clearReq := httptest.NewRequest(http.MethodPut, "/api/ve/ve_legal/visibility", strings.NewReader(`{"visible_group_ids":[]}`))
	clearReq = clearReq.WithContext(tenantAdminContext(clearReq.Context(), "tenant-a"))
	clearReq.SetPathValue("id", "ve_legal")
	clearRec := httptest.NewRecorder()
	VEAdminVisibilityHandler(settings, nil).ServeHTTP(clearRec, clearReq)
	if clearRec.Code != http.StatusOK {
		t.Fatalf("clear visibility status=%d body=%s", clearRec.Code, clearRec.Body.String())
	}
	updated = loadVERegistry(context.Background(), tenantSystem)
	if len(updated.Employees[0].VisibleGroupIDs) != 0 {
		t.Fatalf("clear visible groups = %#v, want empty", updated.Employees[0].VisibleGroupIDs)
	}
}

func TestVEAdminVisibilityHandlerRejectsGroupsWithoutSecurityService(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{
		{ID: "ve_legal", MachineID: "machine-legal", Name: "Legal", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline},
	}}
	if err := saveVERegistry(context.Background(), tenantSystem, registry); err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/ve/ve_legal/visibility", strings.NewReader(`{"visible_group_ids":["dept-legal"]}`))
	req = req.WithContext(tenantAdminContext(req.Context(), "tenant-a"))
	req.SetPathValue("id", "ve_legal")
	rec := httptest.NewRecorder()
	VEAdminVisibilityHandler(settings, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("visibility without security service status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestVEAdminVisibilityHandlerRejectsUnknownGroupWithoutMutating(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	securitySvc, _ := newSecurityHandlerTestService(t, "ve-visibility-unknown.db")
	securityCtx := securitypkg.WithTenant(t.Context(), "tenant-a")
	root, err := securitySvc.GetGroupTree(securityCtx)
	if err != nil || root == nil {
		t.Fatalf("load security root: %v", err)
	}
	legal, err := securitySvc.CreateGroup(securityCtx, "Legal", root.ID)
	if err != nil {
		t.Fatalf("create legal group: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{
		{ID: "ve_legal", MachineID: "machine-legal", Name: "Legal", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline, VisibleGroupIDs: []string{legal.ID}},
	}}
	if err := saveVERegistry(context.Background(), tenantSystem, registry); err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/ve/ve_legal/visibility", strings.NewReader(`{"visible_group_ids":["missing-group"]}`))
	req = req.WithContext(tenantAdminContext(req.Context(), "tenant-a"))
	req.SetPathValue("id", "ve_legal")
	rec := httptest.NewRecorder()
	VEAdminVisibilityHandler(settings, securitySvc).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown group status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated := loadVERegistry(context.Background(), tenantSystem)
	got := updated.Employees[0].VisibleGroupIDs
	if len(got) != 1 || got[0] != legal.ID {
		t.Fatalf("visible groups mutated after validation failure: %#v", got)
	}
}

func TestVEAdminVisibilityHandlerRejectsRootGroupWithoutMutating(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	securitySvc, _ := newSecurityHandlerTestService(t, "ve-visibility-root.db")
	securityCtx := securitypkg.WithTenant(t.Context(), "tenant-a")
	root, err := securitySvc.GetGroupTree(securityCtx)
	if err != nil || root == nil {
		t.Fatalf("load security root: %v", err)
	}
	legal, err := securitySvc.CreateGroup(securityCtx, "Legal", root.ID)
	if err != nil {
		t.Fatalf("create legal group: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{
		{ID: "ve_legal", MachineID: "machine-legal", Name: "Legal", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline, VisibleGroupIDs: []string{legal.ID}},
	}}
	if err := saveVERegistry(context.Background(), tenantSystem, registry); err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	reqBody, err := json.Marshal(map[string][]string{"visible_group_ids": []string{root.ID}})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/ve/ve_legal/visibility", strings.NewReader(string(reqBody)))
	req = req.WithContext(tenantAdminContext(req.Context(), "tenant-a"))
	req.SetPathValue("id", "ve_legal")
	rec := httptest.NewRecorder()
	VEAdminVisibilityHandler(settings, securitySvc).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("root group status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated := loadVERegistry(context.Background(), tenantSystem)
	got := updated.Employees[0].VisibleGroupIDs
	if len(got) != 1 || got[0] != legal.ID {
		t.Fatalf("visible groups mutated after root validation failure: %#v", got)
	}
}

func TestVEAdminVisibilityHandlerReturnsNotFoundBeforeGroupValidation(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	securitySvc, _ := newSecurityHandlerTestService(t, "ve-visibility-not-found.db")
	req := httptest.NewRequest(http.MethodPut, "/api/ve/missing/visibility", strings.NewReader(`{"visible_group_ids":["missing-group"]}`))
	req = req.WithContext(tenantAdminContext(req.Context(), "tenant-a"))
	req.SetPathValue("id", "missing")
	rec := httptest.NewRecorder()
	VEAdminVisibilityHandler(settings, securitySvc).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing employee visibility status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestVEAdminVisibilityHandlerAcceptsVisibleDepartmentAlias(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	securitySvc, _ := newSecurityHandlerTestService(t, "ve-visibility-alias.db")
	securityCtx := securitypkg.WithTenant(t.Context(), "tenant-a")
	root, err := securitySvc.GetGroupTree(securityCtx)
	if err != nil || root == nil {
		t.Fatalf("load security root: %v", err)
	}
	legal, err := securitySvc.CreateGroup(securityCtx, "Legal", root.ID)
	if err != nil {
		t.Fatalf("create legal group: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{
		{ID: "ve_legal", MachineID: "machine-legal", Name: "Legal", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline},
	}}
	if err := saveVERegistry(context.Background(), tenantSystem, registry); err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	reqBody, err := json.Marshal(map[string][]string{"visible_department_ids": []string{legal.ID}})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/ve/ve_legal/visibility", strings.NewReader(string(reqBody)))
	req = req.WithContext(tenantAdminContext(req.Context(), "tenant-a"))
	req.SetPathValue("id", "ve_legal")
	rec := httptest.NewRecorder()
	VEAdminVisibilityHandler(settings, securitySvc).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("visibility alias status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated := loadVERegistry(context.Background(), tenantSystem)
	got := updated.Employees[0].VisibleGroupIDs
	if len(got) != 1 || got[0] != legal.ID {
		t.Fatalf("visible department alias groups = %#v, want %#v", got, []string{legal.ID})
	}
}

func TestVEAdminVisibilityHandlerPrefersVisibleGroupIDsOverAlias(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	securitySvc, _ := newSecurityHandlerTestService(t, "ve-visibility-prefer-groups.db")
	securityCtx := securitypkg.WithTenant(t.Context(), "tenant-a")
	root, err := securitySvc.GetGroupTree(securityCtx)
	if err != nil || root == nil {
		t.Fatalf("load security root: %v", err)
	}
	legal, err := securitySvc.CreateGroup(securityCtx, "Legal", root.ID)
	if err != nil {
		t.Fatalf("create legal group: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{
		{ID: "ve_legal", MachineID: "machine-legal", Name: "Legal", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline, VisibleGroupIDs: []string{legal.ID}},
	}}
	if err := saveVERegistry(context.Background(), tenantSystem, registry); err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	reqBody, err := json.Marshal(map[string][]string{
		"visible_group_ids":      []string{},
		"visible_department_ids": []string{legal.ID},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/ve/ve_legal/visibility", strings.NewReader(string(reqBody)))
	req = req.WithContext(tenantAdminContext(req.Context(), "tenant-a"))
	req.SetPathValue("id", "ve_legal")
	rec := httptest.NewRecorder()
	VEAdminVisibilityHandler(settings, securitySvc).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("visibility status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated := loadVERegistry(context.Background(), tenantSystem)
	if got := updated.Employees[0].VisibleGroupIDs; len(got) != 0 {
		t.Fatalf("visible_group_ids should win over alias and clear groups, got %#v", got)
	}
}

func TestVEAdminVisibilityHandlerRejectsMissingVisibilityFieldWithoutMutating(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	securitySvc, _ := newSecurityHandlerTestService(t, "ve-visibility-missing-field.db")
	securityCtx := securitypkg.WithTenant(t.Context(), "tenant-a")
	root, err := securitySvc.GetGroupTree(securityCtx)
	if err != nil || root == nil {
		t.Fatalf("load security root: %v", err)
	}
	legal, err := securitySvc.CreateGroup(securityCtx, "Legal", root.ID)
	if err != nil {
		t.Fatalf("create legal group: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{
		{ID: "ve_legal", MachineID: "machine-legal", Name: "Legal", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline, VisibleGroupIDs: []string{legal.ID}},
	}}
	if err := saveVERegistry(context.Background(), tenantSystem, registry); err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/ve/ve_legal/visibility", strings.NewReader(`{}`))
	req = req.WithContext(tenantAdminContext(req.Context(), "tenant-a"))
	req.SetPathValue("id", "ve_legal")
	rec := httptest.NewRecorder()
	VEAdminVisibilityHandler(settings, securitySvc).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing visibility field status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated := loadVERegistry(context.Background(), tenantSystem)
	if got := updated.Employees[0].VisibleGroupIDs; len(got) != 1 || got[0] != legal.ID {
		t.Fatalf("visible groups mutated after missing field: %#v", got)
	}
}

func TestVESettingsPreservesVisibleGroups(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 1)
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{
		{ID: "ve_machine-a", MachineID: "machine-a", OwnerUserID: "user-a", Name: "Old", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline, VisibleGroupIDs: []string{"dept-legal"}, Resident: true},
	}}
	if err := saveVERegistry(context.Background(), tenantSystem, registry); err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	authn := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-a": {TenantID: "tenant-a", UserID: "user-a", MachineID: "machine-a"},
		},
	}

	rr := doVEMachineJSON(t, VESettingsHandler(settings, authn), http.MethodPut, "/api/ve/settings", map[string]any{
		"name":              "Updated",
		"skill_description": "Updated skill",
		"access_policy":     "public",
	}, "machine-a", "machine-token")
	if rr.Code != http.StatusOK {
		t.Fatalf("settings status=%d body=%s", rr.Code, rr.Body.String())
	}
	updated := loadVERegistry(context.Background(), tenantSystem)
	got := updated.Employees[0].VisibleGroupIDs
	if len(got) != 1 || got[0] != "dept-legal" {
		t.Fatalf("visible groups = %#v, want preserved dept-legal", got)
	}
	if !updated.Employees[0].Resident {
		t.Fatalf("resident flag should be preserved")
	}
}

func TestVESettingsReturnsNormalizedResidentFlag(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 2)
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{
		{ID: "ve_machine-a", MachineID: "machine-a", OwnerUserID: "user-a", Name: "First", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline, Resident: true},
		{ID: "ve_machine-b", MachineID: "machine-b", OwnerUserID: "user-b", Name: "Second", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline, Resident: true},
	}}
	rawRegistry, err := json.Marshal(registry)
	if err != nil {
		t.Fatalf("marshal registry: %v", err)
	}
	if err := tenantSystem.Set(context.Background(), veRegistryKey, string(rawRegistry)); err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	authn := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-b": {TenantID: "tenant-a", UserID: "user-b", MachineID: "machine-b"},
		},
	}

	rr := doVEMachineJSON(t, VESettingsHandler(settings, authn), http.MethodPut, "/api/ve/settings", map[string]any{
		"name":              "Updated Second",
		"skill_description": "Updated skill",
		"access_policy":     "public",
	}, "machine-b", "machine-token")
	if rr.Code != http.StatusOK {
		t.Fatalf("settings status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Employee digitalEmployeeEntry `json:"employee"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Employee.Resident {
		t.Fatalf("response should reflect normalized non-resident second entry: %s", rr.Body.String())
	}
	updated := loadVERegistry(context.Background(), tenantSystem)
	if !updated.Employees[0].Resident || updated.Employees[1].Resident {
		t.Fatalf("registry resident flags not normalized: %+v", updated.Employees)
	}
}

func TestExplicitPhysicalEmployeeTypeWinsOverLegacyPlatformFields(t *testing.T) {
	entry := digitalEmployeeEntry{EmployeeType: veEmployeeTypePhysical, PlatformID: "maclawsrv", PlatformEmployeeID: "srv-user-2", RuntimeProviderID: maclawSrvRuntimePlatformID}
	if got := inferVEEmployeeType(entry); got != veEmployeeTypePhysical {
		t.Fatalf("explicit physical employee type should win over stale platform fields, got %q", got)
	}
}

func TestVEAdminResidentHandlerEnforcesSingleActiveResidentPerTenant(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{
		{ID: "ve_a", MachineID: "machine-a", Name: "A", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline},
		{ID: "ve_b", MachineID: "machine-b", Name: "B", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline},
	}}
	if err := saveVERegistry(context.Background(), tenantSystem, registry); err != nil {
		t.Fatalf("seed registry: %v", err)
	}

	setResident := func(id string, resident bool) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]bool{"resident": resident})
		req := httptest.NewRequest(http.MethodPut, "/api/ve/"+id+"/resident", strings.NewReader(string(body)))
		req = req.WithContext(tenantAdminContext(req.Context(), "tenant-a"))
		req.SetPathValue("id", id)
		rec := httptest.NewRecorder()
		VEAdminResidentHandler(settings).ServeHTTP(rec, req)
		return rec
	}

	if rec := setResident("ve_a", true); rec.Code != http.StatusOK {
		t.Fatalf("set ve_a resident status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := setResident("ve_b", true); rec.Code != http.StatusOK {
		t.Fatalf("set ve_b resident status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated := loadVERegistry(context.Background(), tenantSystem)
	if updated.Employees[0].Resident || !updated.Employees[1].Resident {
		t.Fatalf("expected only ve_b resident, got %+v", updated.Employees)
	}
	if rec := setResident("ve_b", false); rec.Code != http.StatusOK {
		t.Fatalf("clear ve_b resident status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated = loadVERegistry(context.Background(), tenantSystem)
	if updated.Employees[0].Resident || updated.Employees[1].Resident {
		t.Fatalf("expected no resident after clear, got %+v", updated.Employees)
	}
}

func TestVEAdminResidentHandlerRejectsMissingMacLawSrvRuntimeEmployee(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 1)
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/platform/runtime/report" {
			t.Fatalf("unexpected runtime path: %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "users": []map[string]any{}})
	}))
	defer runtime.Close()
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
		Name:               "Deleted Runtime Worker",
		Status:             veStatusActive,
		OnlineStatus:       veOnlineStatusOnline,
	}}}
	if err := saveVERegistry(context.Background(), tenantSystem, registry); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/ve/ve_deleted/resident", strings.NewReader(`{"resident":true}`))
	req = req.WithContext(tenantAdminContext(req.Context(), "tenant-a"))
	req.SetPathValue("id", "ve_deleted")
	rec := httptest.NewRecorder()
	VEAdminResidentHandler(settings).ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict || !bytes.Contains(rec.Body.Bytes(), []byte("VE_RUNTIME_MISSING")) {
		t.Fatalf("resident missing runtime status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated := loadVERegistry(context.Background(), tenantSystem)
	if len(updated.Employees) != 1 || updated.Employees[0].Status != veStatusDisabled || updated.Employees[0].OnlineStatus != veOnlineStatusOffline || updated.Employees[0].Resident {
		t.Fatalf("missing runtime employee should be disabled/offline/non-resident: %#v", updated.Employees)
	}
}

func TestVEAdminResidentHandlerRejectsInactiveEmployee(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{
		ID:           "ve_pending",
		MachineID:    "machine-pending",
		Name:         "Pending",
		Status:       veStatusPending,
		OnlineStatus: veOnlineStatusOnline,
	}}}
	if err := saveVERegistry(context.Background(), tenantSystem, registry); err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/ve/ve_pending/resident", strings.NewReader(`{"resident":true}`))
	req = req.WithContext(tenantAdminContext(req.Context(), "tenant-a"))
	req.SetPathValue("id", "ve_pending")
	rec := httptest.NewRecorder()
	VEAdminResidentHandler(settings).ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected conflict for pending resident, got %d body=%s", rec.Code, rec.Body.String())
	}
	updated := loadVERegistry(context.Background(), tenantSystem)
	if updated.Employees[0].Resident {
		t.Fatalf("pending employee should not be resident: %+v", updated.Employees[0])
	}
}

func TestVEAdminResidentHandlerIsScopedPerTenant(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	for _, tenantID := range []string{"tenant-a", "tenant-b"} {
		tenantSystem := scopedSystemSettingsForTenant(tenantID, settings)
		registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{
			ID:           "ve_" + tenantID,
			MachineID:    "machine-" + tenantID,
			Name:         tenantID,
			Status:       veStatusActive,
			OnlineStatus: veOnlineStatusOnline,
		}}}
		if err := saveVERegistry(context.Background(), tenantSystem, registry); err != nil {
			t.Fatalf("seed %s registry: %v", tenantID, err)
		}
	}

	setResident := func(tenantID, id string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/api/ve/"+id+"/resident", strings.NewReader(`{"resident":true}`))
		req = req.WithContext(tenantAdminContext(req.Context(), tenantID))
		req.SetPathValue("id", id)
		rec := httptest.NewRecorder()
		VEAdminResidentHandler(settings).ServeHTTP(rec, req)
		return rec
	}

	if rec := setResident("tenant-a", "ve_tenant-a"); rec.Code != http.StatusOK {
		t.Fatalf("set tenant-a resident status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := setResident("tenant-b", "ve_tenant-b"); rec.Code != http.StatusOK {
		t.Fatalf("set tenant-b resident status=%d body=%s", rec.Code, rec.Body.String())
	}
	for _, tenantID := range []string{"tenant-a", "tenant-b"} {
		updated := loadVERegistry(context.Background(), scopedSystemSettingsForTenant(tenantID, settings))
		if len(updated.Employees) != 1 || !updated.Employees[0].Resident {
			t.Fatalf("%s should keep its own resident, got %+v", tenantID, updated.Employees)
		}
	}
}

func TestDigitalEmployeeInitiateCreatesMachineIDDiscussion(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 2)
	authn := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-a": {TenantID: "tenant-a", UserID: "user-a", MachineID: "machine-a"},
			"machine-b": {TenantID: "tenant-a", UserID: "user-b", MachineID: "machine-b"},
		},
	}

	registerRR := doVEMachineJSON(t, VERegisterHandler(settings, authn), http.MethodPost, "/api/ve/register", map[string]any{
		"name":              "Legal Researcher",
		"skill_description": "Contract review",
		"access_policy":     "public",
	}, "machine-a", "machine-token")
	if registerRR.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", registerRR.Code, registerRR.Body.String())
	}

	approveReq := httptest.NewRequest(http.MethodPost, "/api/ve/ve_machine-a/approve", nil)
	approveReq = approveReq.WithContext(tenantAdminContext(approveReq.Context(), "tenant-a"))
	approveReq.SetPathValue("id", "ve_machine-a")
	approveRec := httptest.NewRecorder()
	VEAdminActionHandler(settings, "approve").ServeHTTP(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve status=%d body=%s", approveRec.Code, approveRec.Body.String())
	}

	groupSvc := NewGroupDiscussionService()
	req := httptest.NewRequest(http.MethodPost, "/api/ve/ve_machine-a/initiate", nil)
	req.Header.Set("X-Machine-ID", "machine-b")
	req.Header.Set("Authorization", "Bearer machine-token")
	req.SetPathValue("id", "ve_machine-a")
	rec := httptest.NewRecorder()
	VEInitiateHandler(settings, groupSvc, authn).ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("initiate status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		SessionID  string                       `json:"session_id"`
		VEID       string                       `json:"ve_id"`
		VEName     string                       `json:"ve_name"`
		Discussion corea2a.HubDiscussionSummary `json:"discussion"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode initiate response: %v body=%s", err, rec.Body.String())
	}
	if out.SessionID == "" || out.VEID != "ve_machine-a" || out.VEName != "Legal Researcher" {
		t.Fatalf("unexpected initiate response: %+v", out)
	}
	if out.Discussion.LocalRelation != "initiated_by_me" || out.Discussion.Readonly {
		t.Fatalf("initiator discussion relation = %+v", out.Discussion)
	}
	if out.Discussion.Topic != "\u6570\u5b57\u5458\u5de5\u4f1a\u8bdd\uff1aLegal Researcher" {
		t.Fatalf("discussion topic = %q", out.Discussion.Topic)
	}

	mine, err := groupSvc.ListDiscussionSummaries("tenant-a", ListSessionsFilter{ParticipantID: "machine-b"})
	if err != nil || len(mine) != 1 || mine[0].LocalRelation != "initiated_by_me" || mine[0].Readonly {
		t.Fatalf("initiator summaries err=%v items=%+v", err, mine)
	}

	reuseReq := httptest.NewRequest(http.MethodPost, "/api/ve/ve_machine-a/initiate", nil)
	reuseReq.Header.Set("X-Machine-ID", "machine-b")
	reuseReq.Header.Set("Authorization", "Bearer machine-token")
	reuseReq.SetPathValue("id", "ve_machine-a")
	reuseRec := httptest.NewRecorder()
	VEInitiateHandler(settings, groupSvc, authn).ServeHTTP(reuseRec, reuseReq)
	if reuseRec.Code != http.StatusOK {
		t.Fatalf("reuse initiate status=%d body=%s", reuseRec.Code, reuseRec.Body.String())
	}
	var reused struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(reuseRec.Body.Bytes(), &reused); err != nil {
		t.Fatalf("decode reused initiate response: %v body=%s", err, reuseRec.Body.String())
	}
	if reused.SessionID != out.SessionID {
		t.Fatalf("reused session_id=%q, want existing %q", reused.SessionID, out.SessionID)
	}
	mine, err = groupSvc.ListDiscussionSummaries("tenant-a", ListSessionsFilter{ParticipantID: "machine-b"})
	if err != nil || len(mine) != 1 {
		t.Fatalf("reuse should not create duplicate discussion err=%v items=%+v", err, mine)
	}

	invited, err := groupSvc.ListDiscussionSummaries("tenant-a", ListSessionsFilter{ParticipantID: "machine-a"})
	if err != nil || len(invited) != 1 || invited[0].LocalRelation != "owned_ve_invited" || !invited[0].Readonly {
		t.Fatalf("target summaries err=%v items=%+v", err, invited)
	}

	if _, err := groupSvc.SetDiscussionState("tenant-a", out.SessionID, "cancel"); err != nil {
		t.Fatalf("close existing direct discussion: %v", err)
	}
	freshReq := httptest.NewRequest(http.MethodPost, "/api/ve/ve_machine-a/initiate", nil)
	freshReq.Header.Set("X-Machine-ID", "machine-b")
	freshReq.Header.Set("Authorization", "Bearer machine-token")
	freshReq.SetPathValue("id", "ve_machine-a")
	freshRec := httptest.NewRecorder()
	VEInitiateHandler(settings, groupSvc, authn).ServeHTTP(freshRec, freshReq)
	if freshRec.Code != http.StatusCreated {
		t.Fatalf("fresh initiate after close status=%d body=%s", freshRec.Code, freshRec.Body.String())
	}
	var fresh struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(freshRec.Body.Bytes(), &fresh); err != nil {
		t.Fatalf("decode fresh initiate response: %v body=%s", err, freshRec.Body.String())
	}
	if fresh.SessionID == "" || fresh.SessionID == out.SessionID {
		t.Fatalf("fresh session_id=%q should differ from closed %q", fresh.SessionID, out.SessionID)
	}
}

func TestReusableVEDirectSessionRejectsGroupDiscussion(t *testing.T) {
	session := &corea2a.Session{
		Status: corea2a.SessionOpen,
		Participants: []corea2a.Participant{
			{ID: "machine-b", RoleCode: "initiator"},
			{ID: "machine-a", RoleCode: "speak"},
			{ID: "machine-c", RoleCode: "review"},
		},
	}
	if isReusableVEDirectSession(session, "machine-b", "machine-a") {
		t.Fatal("group discussion must not be reused as a direct digital employee session")
	}

	session.Participants = session.Participants[:2]
	if !isReusableVEDirectSession(session, "machine-b", "machine-a") {
		t.Fatal("open two-party initiator session should be reusable")
	}

	session.Participants[1].RoleCode = "observe"
	if isReusableVEDirectSession(session, "machine-b", "machine-a") {
		t.Fatal("two-party observer session must not be reused as a direct digital employee session")
	}
}

func TestDigitalEmployeeInitiateAcceptsPlatformEmployeeID(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 2)
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_platform-employee-1", MachineID: "ve_platform-employee-1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", RuntimeProviderID: maclawSrvRuntimePlatformID, Name: "Runtime Worker", SkillDescription: "Runtime", AccessPolicy: "public", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), tenantSystem, registry); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	authn := fakeVEMachineAuth{token: "machine-token", principals: map[string]*auth.MachinePrincipal{"machine-gui": {TenantID: "tenant-a", UserID: "user-gui", MachineID: "machine-gui"}}}
	groupSvc := NewGroupDiscussionService()
	req := httptest.NewRequest(http.MethodPost, "/api/ve/platform-employee-1/initiate", nil)
	req.Header.Set("X-Machine-ID", "machine-gui")
	req.Header.Set("Authorization", "Bearer machine-token")
	req.SetPathValue("id", "platform-employee-1")
	rec := httptest.NewRecorder()
	VEInitiateHandler(settings, groupSvc, authn).ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("initiate by platform_employee_id status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		VEID       string                       `json:"ve_id"`
		Discussion corea2a.HubDiscussionSummary `json:"discussion"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode initiate response: %v body=%s", err, rec.Body.String())
	}
	if out.VEID != "ve_platform-employee-1" || len(out.Discussion.ParticipantIDs) != 2 || out.Discussion.ParticipantIDs[1] != "ve_platform-employee-1" {
		t.Fatalf("unexpected platform employee initiate response: %#v", out)
	}
}

func TestDigitalEmployeeInitiateRejectsMissingMacLawSrvRuntimeEmployee(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 2)
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/platform/runtime/report" {
			t.Fatalf("unexpected runtime path: %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "users": []map[string]any{}})
	}))
	defer runtime.Close()
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
		Name:               "Deleted Runtime Worker",
		AccessPolicy:       "public",
		Status:             veStatusActive,
		OnlineStatus:       veOnlineStatusOnline,
	}}}
	if err := saveVERegistry(context.Background(), tenantSystem, registry); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	authn := fakeVEMachineAuth{token: "machine-token", principals: map[string]*auth.MachinePrincipal{"machine-gui": {TenantID: "tenant-a", UserID: "user-gui", MachineID: "machine-gui"}}}
	req := httptest.NewRequest(http.MethodPost, "/api/ve/deleted-employee/initiate", nil)
	req.Header.Set("X-Machine-ID", "machine-gui")
	req.Header.Set("Authorization", "Bearer machine-token")
	req.SetPathValue("id", "deleted-employee")
	rec := httptest.NewRecorder()
	VEInitiateHandler(settings, NewGroupDiscussionService(), authn).ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict || !bytes.Contains(rec.Body.Bytes(), []byte("VE_NOT_ACTIVE")) {
		t.Fatalf("missing runtime employee initiate status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDigitalEmployeeInitiateRejectsNonReadyMacLawSrvRuntimeEmployee(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 2)
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/platform/runtime/report" {
			t.Fatalf("unexpected runtime path: %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ok",
			"users":  []map[string]any{{"employee_id": "attention-employee", "runtime_status": "attention"}},
		})
	}))
	defer runtime.Close()
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: runtime.URL, TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{
		ID:                 "ve_attention",
		MachineID:          "ve_attention",
		PlatformID:         maclawSrvRuntimePlatformID,
		PlatformEmployeeID: "attention-employee",
		RuntimeProviderID:  maclawSrvRuntimePlatformID,
		Name:               "Attention Runtime Worker",
		AccessPolicy:       "public",
		Status:             veStatusActive,
		OnlineStatus:       veOnlineStatusOnline,
	}}}
	if err := saveVERegistry(context.Background(), tenantSystem, registry); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	authn := fakeVEMachineAuth{token: "machine-token", principals: map[string]*auth.MachinePrincipal{"machine-gui": {TenantID: "tenant-a", UserID: "user-gui", MachineID: "machine-gui"}}}
	req := httptest.NewRequest(http.MethodPost, "/api/ve/attention-employee/initiate", nil)
	req.Header.Set("X-Machine-ID", "machine-gui")
	req.Header.Set("Authorization", "Bearer machine-token")
	req.SetPathValue("id", "attention-employee")
	rec := httptest.NewRecorder()
	VEInitiateHandler(settings, NewGroupDiscussionService(), authn).ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict || !bytes.Contains(rec.Body.Bytes(), []byte("VE_NOT_ONLINE")) {
		t.Fatalf("non-ready runtime employee initiate status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDigitalEmployeeHistoryAcceptsPlatformEmployeeIDCaseInsensitive(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 2)
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_platform-employee-1", MachineID: "ve_platform-employee-1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", RuntimeProviderID: maclawSrvRuntimePlatformID, Name: "Runtime Worker", SkillDescription: "Runtime", AccessPolicy: "public", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), tenantSystem, registry); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	groupSvc := NewGroupDiscussionService()
	if _, err := groupSvc.CreateSession("tenant-a", CreateSessionRequest{
		Topic: "Runtime conversation",
		Goal:  "Talk to runtime worker",
		Participants: []corea2a.Participant{
			{ID: "machine-gui", RoleCode: "initiator"},
			{ID: "ve_platform-employee-1", RoleCode: "speak", Name: "Runtime Worker"},
		},
		DecisionPolicy: corea2a.PolicyMajority,
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/ve/platform-employee-1/history?limit=5", nil)
	req = req.WithContext(tenantAdminContext(req.Context(), "tenant-a"))
	req.SetPathValue("id", "PLATFORM-EMPLOYEE-1")
	rec := httptest.NewRecorder()
	VEHistoryHandler(settings, groupSvc, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte("Runtime conversation")) {
		t.Fatalf("history by platform employee id status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDigitalEmployeeHistoryReflectsMissingMacLawSrvRuntimeEmployeeOffline(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 2)
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/platform/runtime/report" {
			t.Fatalf("unexpected runtime path: %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "users": []map[string]any{}})
	}))
	defer runtime.Close()
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
		Name:               "Deleted Runtime Worker",
		Status:             veStatusActive,
		OnlineStatus:       veOnlineStatusOnline,
	}}}
	if err := saveVERegistry(context.Background(), tenantSystem, registry); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	groupSvc := NewGroupDiscussionService()
	req := httptest.NewRequest(http.MethodGet, "/api/ve/deleted-employee/history?limit=5", nil)
	req = req.WithContext(tenantAdminContext(req.Context(), "tenant-a"))
	req.SetPathValue("id", "deleted-employee")
	rec := httptest.NewRecorder()
	VEHistoryHandler(settings, groupSvc, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"status":"disabled"`)) || !bytes.Contains(rec.Body.Bytes(), []byte(`"online_status":"offline"`)) {
		t.Fatalf("history should reflect runtime absence status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDigitalEmployeeInitiateRejectsInactiveSelfAndAccessDenied(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 2)
	authn := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-a": {UserID: "user-a", MachineID: "machine-a"},
			"machine-b": {UserID: "user-b", MachineID: "machine-b"},
		},
	}
	groupSvc := NewGroupDiscussionService()

	registerRR := doVEMachineJSON(t, VERegisterHandler(settings, authn), http.MethodPost, "/api/ve/register", map[string]any{
		"name":          "Private Analyst",
		"access_policy": "whitelist",
		"whitelist":     []string{"user-c"},
	}, "machine-a", "machine-token")
	if registerRR.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", registerRR.Code, registerRR.Body.String())
	}

	inactiveReq := httptest.NewRequest(http.MethodPost, "/api/ve/ve_machine-a/initiate", nil)
	inactiveReq.Header.Set("X-Machine-ID", "machine-b")
	inactiveReq.Header.Set("Authorization", "Bearer machine-token")
	inactiveReq.SetPathValue("id", "ve_machine-a")
	inactiveRec := httptest.NewRecorder()
	VEInitiateHandler(settings, groupSvc, authn).ServeHTTP(inactiveRec, inactiveReq)
	if inactiveRec.Code != http.StatusConflict {
		t.Fatalf("expected inactive target to be rejected, got %d body=%s", inactiveRec.Code, inactiveRec.Body.String())
	}

	approveReq := httptest.NewRequest(http.MethodPost, "/api/ve/ve_machine-a/approve", nil)
	approveReq.SetPathValue("id", "ve_machine-a")
	approveRec := httptest.NewRecorder()
	VEAdminActionHandler(settings, "approve").ServeHTTP(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve status=%d body=%s", approveRec.Code, approveRec.Body.String())
	}

	selfReq := httptest.NewRequest(http.MethodPost, "/api/ve/ve_machine-a/initiate", nil)
	selfReq.Header.Set("X-Machine-ID", "machine-a")
	selfReq.Header.Set("Authorization", "Bearer machine-token")
	selfReq.SetPathValue("id", "ve_machine-a")
	selfRec := httptest.NewRecorder()
	VEInitiateHandler(settings, groupSvc, authn).ServeHTTP(selfRec, selfReq)
	if selfRec.Code != http.StatusBadRequest {
		t.Fatalf("expected self chat to be rejected, got %d body=%s", selfRec.Code, selfRec.Body.String())
	}

	deniedReq := httptest.NewRequest(http.MethodPost, "/api/ve/ve_machine-a/initiate", nil)
	deniedReq.Header.Set("X-Machine-ID", "machine-b")
	deniedReq.Header.Set("Authorization", "Bearer machine-token")
	deniedReq.SetPathValue("id", "ve_machine-a")
	deniedRec := httptest.NewRecorder()
	VEInitiateHandler(settings, groupSvc, authn).ServeHTTP(deniedRec, deniedReq)
	if deniedRec.Code != http.StatusForbidden {
		t.Fatalf("expected access denied, got %d body=%s", deniedRec.Code, deniedRec.Body.String())
	}
}

func TestDigitalEmployeePerRequestConfirmationAllowLongAndBlock(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 2)
	authn := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-a": {UserID: "owner-a", MachineID: "machine-a"},
			"machine-b": {UserID: "user-b", MachineID: "machine-b"},
			"machine-c": {UserID: "user-c", MachineID: "machine-c"},
		},
	}
	sender := &fakeVEMachineEventSender{}
	groupSvc := NewGroupDiscussionService()

	registerRR := doVEMachineJSON(t, VERegisterHandler(settings, authn), http.MethodPost, "/api/ve/register", map[string]any{
		"name":          "Private Analyst",
		"access_policy": "per_request",
	}, "machine-a", "machine-token")
	if registerRR.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", registerRR.Code, registerRR.Body.String())
	}
	approveReq := httptest.NewRequest(http.MethodPost, "/api/ve/ve_machine-a/approve", nil)
	approveReq.SetPathValue("id", "ve_machine-a")
	approveRec := httptest.NewRecorder()
	VEAdminActionHandler(settings, "approve").ServeHTTP(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve status=%d body=%s", approveRec.Code, approveRec.Body.String())
	}

	pendingReq := httptest.NewRequest(http.MethodPost, "/api/ve/ve_machine-a/initiate", nil)
	pendingReq.Header.Set("X-Machine-ID", "machine-b")
	pendingReq.Header.Set("Authorization", "Bearer machine-token")
	pendingReq.SetPathValue("id", "ve_machine-a")
	pendingRec := httptest.NewRecorder()
	VEInitiateHandler(settings, groupSvc, authn, sender).ServeHTTP(pendingRec, pendingReq)
	if pendingRec.Code != http.StatusAccepted || !strings.Contains(pendingRec.Body.String(), "pending_confirmation") {
		t.Fatalf("expected pending confirmation, got %d body=%s", pendingRec.Code, pendingRec.Body.String())
	}
	if len(sender.messages) == 0 || sender.messages[0].machineID != "machine-a" || sender.messages[0].msg["type"] != "ve:auth_request" {
		t.Fatalf("expected auth request event to owner, got %#v", sender.messages)
	}
	var pendingBody map[string]any
	if err := json.NewDecoder(pendingRec.Body).Decode(&pendingBody); err != nil {
		t.Fatalf("decode pending: %v", err)
	}
	requestID, _ := pendingBody["request_id"].(string)
	if requestID == "" {
		t.Fatalf("missing request_id in %v", pendingBody)
	}
	if expiresAt, _ := pendingBody["expires_at"].(string); expiresAt == "" {
		t.Fatalf("missing expires_at in pending response: %v", pendingBody)
	}
	repeatedPendingReq := httptest.NewRequest(http.MethodPost, "/api/ve/ve_machine-a/initiate", nil)
	repeatedPendingReq.Header.Set("X-Machine-ID", "machine-b")
	repeatedPendingReq.Header.Set("Authorization", "Bearer machine-token")
	repeatedPendingReq.SetPathValue("id", "ve_machine-a")
	repeatedPendingRec := httptest.NewRecorder()
	VEInitiateHandler(settings, groupSvc, authn, sender).ServeHTTP(repeatedPendingRec, repeatedPendingReq)
	if repeatedPendingRec.Code != http.StatusAccepted || !strings.Contains(repeatedPendingRec.Body.String(), requestID) {
		t.Fatalf("repeated pending request should return existing request id %q, got %d body=%s", requestID, repeatedPendingRec.Code, repeatedPendingRec.Body.String())
	}
	requests := loadVEAccessRequests(context.Background(), settings)
	if len(requests.Requests) == 0 {
		t.Fatalf("missing stored request after pending confirmation")
	}
	requests.Requests[0].ExpiresAt = time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	if err := saveVEAccessRequests(context.Background(), settings, requests); err != nil {
		t.Fatalf("save expired request: %v", err)
	}
	expiredRR := doVEMachineJSON(t, VEAuthRespondHandler(settings, authn, sender), http.MethodPost, "/api/ve/auth/respond", map[string]any{
		"request_id": requestID,
		"decision":   "allow_once",
	}, "machine-a", "machine-token")
	if expiredRR.Code != http.StatusConflict {
		t.Fatalf("expired request should reject response, got %d body=%s", expiredRR.Code, expiredRR.Body.String())
	}
	foundExpiredAuthResult := false
	for _, sent := range sender.messages {
		if sent.machineID != "machine-b" || sent.msg["type"] != "ve:auth_result" {
			continue
		}
		payload, _ := sent.msg["payload"].(map[string]any)
		if payload["request_id"] == requestID && payload["decision"] == "timeout" && payload["status"] == "expired" {
			foundExpiredAuthResult = true
		}
	}
	if !foundExpiredAuthResult {
		t.Fatalf("expired request should notify requester with timeout auth_result, messages=%#v", sender.messages)
	}
	freshPendingReq := httptest.NewRequest(http.MethodPost, "/api/ve/ve_machine-a/initiate", nil)
	freshPendingReq.Header.Set("X-Machine-ID", "machine-b")
	freshPendingReq.Header.Set("Authorization", "Bearer machine-token")
	freshPendingReq.SetPathValue("id", "ve_machine-a")
	freshPendingRec := httptest.NewRecorder()
	VEInitiateHandler(settings, groupSvc, authn, sender).ServeHTTP(freshPendingRec, freshPendingReq)
	if freshPendingRec.Code != http.StatusAccepted || !strings.Contains(freshPendingRec.Body.String(), "pending_confirmation") {
		t.Fatalf("expired pending request should be replaced, got %d body=%s", freshPendingRec.Code, freshPendingRec.Body.String())
	}
	var freshPendingBody map[string]any
	if err := json.NewDecoder(freshPendingRec.Body).Decode(&freshPendingBody); err != nil {
		t.Fatalf("decode fresh pending: %v", err)
	}
	requestID, _ = freshPendingBody["request_id"].(string)
	if requestID == "" || requestID == pendingBody["request_id"] {
		t.Fatalf("fresh pending request id = %q, previous=%v", requestID, pendingBody["request_id"])
	}

	allowOnceRR := doVEMachineJSON(t, VEAuthRespondHandler(settings, authn, sender), http.MethodPost, "/api/ve/auth/respond", map[string]any{
		"request_id": requestID,
		"decision":   "allow_once",
	}, "machine-a", "machine-token")
	if allowOnceRR.Code != http.StatusOK {
		t.Fatalf("allow once status=%d body=%s", allowOnceRR.Code, allowOnceRR.Body.String())
	}
	foundAuthResultWithMachineID := false
	for _, sent := range sender.messages {
		if sent.machineID != "machine-b" || sent.msg["type"] != "ve:auth_result" {
			continue
		}
		payload, _ := sent.msg["payload"].(map[string]any)
		if payload["target_machine_id"] == "machine-a" && payload["target_ve_id"] == "ve_machine-a" {
			foundAuthResultWithMachineID = true
		}
	}
	if !foundAuthResultWithMachineID {
		t.Fatalf("auth_result should include target machine id, messages=%#v", sender.messages)
	}
	onceReq := httptest.NewRequest(http.MethodPost, "/api/ve/ve_machine-a/initiate", nil)
	onceReq.Header.Set("X-Machine-ID", "machine-b")
	onceReq.Header.Set("Authorization", "Bearer machine-token")
	onceReq.SetPathValue("id", "ve_machine-a")
	onceRec := httptest.NewRecorder()
	VEInitiateHandler(settings, groupSvc, authn, sender).ServeHTTP(onceRec, onceReq)
	if onceRec.Code != http.StatusCreated || !strings.Contains(onceRec.Body.String(), "session_id") {
		t.Fatalf("allow_once should permit next initiate, got %d body=%s", onceRec.Code, onceRec.Body.String())
	}
	requests = loadVEAccessRequests(context.Background(), settings)
	foundUsedAllowOnce := false
	for _, req := range requests.Requests {
		if req.ID == requestID && req.Status == "used" {
			foundUsedAllowOnce = true
		}
	}
	if !foundUsedAllowOnce {
		t.Fatalf("allow_once request should be consumed after initiate: %+v", requests.Requests)
	}

	secondPendingReq := httptest.NewRequest(http.MethodPost, "/api/ve/ve_machine-a/initiate", nil)
	secondPendingReq.Header.Set("X-Machine-ID", "machine-b")
	secondPendingReq.Header.Set("Authorization", "Bearer machine-token")
	secondPendingReq.SetPathValue("id", "ve_machine-a")
	secondPendingRec := httptest.NewRecorder()
	VEInitiateHandler(settings, NewGroupDiscussionService(), authn, sender).ServeHTTP(secondPendingRec, secondPendingReq)
	if secondPendingRec.Code != http.StatusAccepted || !strings.Contains(secondPendingRec.Body.String(), "pending_confirmation") {
		t.Fatalf("used allow_once should require confirmation again, got %d body=%s", secondPendingRec.Code, secondPendingRec.Body.String())
	}
	var secondPendingBody map[string]any
	if err := json.NewDecoder(secondPendingRec.Body).Decode(&secondPendingBody); err != nil {
		t.Fatalf("decode second pending: %v", err)
	}
	requestID, _ = secondPendingBody["request_id"].(string)
	if requestID == "" {
		t.Fatalf("missing second request_id in %v", secondPendingBody)
	}

	allowRR := doVEMachineJSON(t, VEAuthRespondHandler(settings, authn, sender), http.MethodPost, "/api/ve/auth/respond", map[string]any{
		"request_id": requestID,
		"decision":   "allow_long",
	}, "machine-a", "machine-token")
	if allowRR.Code != http.StatusOK {
		t.Fatalf("allow status=%d body=%s", allowRR.Code, allowRR.Body.String())
	}
	registry := loadVERegistry(context.Background(), settings)
	idx := registry.findByIDOrMachineIDOrPlatformEmployeeID("ve_machine-a")
	if idx < 0 || !containsVEValue(registry.Employees[idx].Whitelist, "user-b") {
		t.Fatalf("allow_long should whitelist requester: %+v", registry.Employees)
	}

	replayRR := doVEMachineJSON(t, VEAuthRespondHandler(settings, authn, sender), http.MethodPost, "/api/ve/auth/respond", map[string]any{
		"request_id": requestID,
		"decision":   "deny",
	}, "machine-a", "machine-token")
	if replayRR.Code != http.StatusConflict {
		t.Fatalf("handled request should reject replay, got %d body=%s", replayRR.Code, replayRR.Body.String())
	}

	blockPendingReq := httptest.NewRequest(http.MethodPost, "/api/ve/ve_machine-a/initiate", nil)
	blockPendingReq.Header.Set("X-Machine-ID", "machine-c")
	blockPendingReq.Header.Set("Authorization", "Bearer machine-token")
	blockPendingReq.SetPathValue("id", "ve_machine-a")
	blockPendingRec := httptest.NewRecorder()
	VEInitiateHandler(settings, NewGroupDiscussionService(), authn, sender).ServeHTTP(blockPendingRec, blockPendingReq)
	if blockPendingRec.Code != http.StatusAccepted || !strings.Contains(blockPendingRec.Body.String(), "pending_confirmation") {
		t.Fatalf("expected pending confirmation for blocked requester, got %d body=%s", blockPendingRec.Code, blockPendingRec.Body.String())
	}
	var blockPendingBody map[string]any
	if err := json.NewDecoder(blockPendingRec.Body).Decode(&blockPendingBody); err != nil {
		t.Fatalf("decode block pending: %v", err)
	}
	blockRequestID, _ := blockPendingBody["request_id"].(string)
	if blockRequestID == "" {
		t.Fatalf("missing block request_id in %v", blockPendingBody)
	}

	blockRR := doVEMachineJSON(t, VEAuthRespondHandler(settings, authn, sender), http.MethodPost, "/api/ve/auth/respond", map[string]any{
		"request_id": blockRequestID,
		"decision":   "block",
	}, "machine-a", "machine-token")
	if blockRR.Code != http.StatusOK {
		t.Fatalf("block status=%d body=%s", blockRR.Code, blockRR.Body.String())
	}
	registry = loadVERegistry(context.Background(), settings)
	idx = registry.findByIDOrMachineIDOrPlatformEmployeeID("ve_machine-a")
	if idx < 0 || !containsVEValue(registry.Employees[idx].Whitelist, "user-b") || !containsVEValue(registry.Employees[idx].Blacklist, "user-c") {
		t.Fatalf("block should move requester from whitelist to blacklist: %+v", registry.Employees)
	}

	blockedReq := httptest.NewRequest(http.MethodPost, "/api/ve/ve_machine-a/initiate", nil)
	blockedReq.Header.Set("X-Machine-ID", "machine-c")
	blockedReq.Header.Set("Authorization", "Bearer machine-token")
	blockedReq.SetPathValue("id", "ve_machine-a")
	blockedRec := httptest.NewRecorder()
	VEInitiateHandler(settings, NewGroupDiscussionService(), authn, sender).ServeHTTP(blockedRec, blockedReq)
	if blockedRec.Code != http.StatusForbidden || !strings.Contains(blockedRec.Body.String(), "VE_ACCESS_DENIED") {
		t.Fatalf("blacklisted requester should be denied before confirmation, got %d body=%s", blockedRec.Code, blockedRec.Body.String())
	}
}

func TestDigitalEmployeeAccessPolicyFallsBackToMachineID(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 2)
	authn := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-a": {UserID: "owner-a", MachineID: "machine-a"},
			"machine-d": {MachineID: "machine-d"},
		},
	}

	registerRR := doVEMachineJSON(t, VERegisterHandler(settings, authn), http.MethodPost, "/api/ve/register", map[string]any{
		"name":          "Machine Scoped Analyst",
		"access_policy": "whitelist",
		"whitelist":     []string{"machine-d"},
	}, "machine-a", "machine-token")
	if registerRR.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", registerRR.Code, registerRR.Body.String())
	}
	approveReq := httptest.NewRequest(http.MethodPost, "/api/ve/ve_machine-a/approve", nil)
	approveReq.SetPathValue("id", "ve_machine-a")
	approveRec := httptest.NewRecorder()
	VEAdminActionHandler(settings, "approve").ServeHTTP(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve status=%d body=%s", approveRec.Code, approveRec.Body.String())
	}

	initReq := httptest.NewRequest(http.MethodPost, "/api/ve/ve_machine-a/initiate", nil)
	initReq.Header.Set("X-Machine-ID", "machine-d")
	initReq.Header.Set("Authorization", "Bearer machine-token")
	initReq.SetPathValue("id", "ve_machine-a")
	initRec := httptest.NewRecorder()
	VEInitiateHandler(settings, NewGroupDiscussionService(), authn).ServeHTTP(initRec, initReq)
	if initRec.Code != http.StatusCreated || !strings.Contains(initRec.Body.String(), "session_id") {
		t.Fatalf("machine-id whitelist fallback should allow initiate, got %d body=%s", initRec.Code, initRec.Body.String())
	}
}

func TestDigitalEmployeePerRequestAllowLongFallsBackToMachineID(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 2)
	authn := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-a": {UserID: "owner-a", MachineID: "machine-a"},
			"machine-d": {MachineID: "machine-d"},
		},
	}
	sender := &fakeVEMachineEventSender{}

	registerRR := doVEMachineJSON(t, VERegisterHandler(settings, authn), http.MethodPost, "/api/ve/register", map[string]any{
		"name":          "Machine Confirm Analyst",
		"access_policy": "per_request",
	}, "machine-a", "machine-token")
	if registerRR.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", registerRR.Code, registerRR.Body.String())
	}
	approveReq := httptest.NewRequest(http.MethodPost, "/api/ve/ve_machine-a/approve", nil)
	approveReq.SetPathValue("id", "ve_machine-a")
	approveRec := httptest.NewRecorder()
	VEAdminActionHandler(settings, "approve").ServeHTTP(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve status=%d body=%s", approveRec.Code, approveRec.Body.String())
	}

	pendingReq := httptest.NewRequest(http.MethodPost, "/api/ve/ve_machine-a/initiate", nil)
	pendingReq.Header.Set("X-Machine-ID", "machine-d")
	pendingReq.Header.Set("Authorization", "Bearer machine-token")
	pendingReq.SetPathValue("id", "ve_machine-a")
	pendingRec := httptest.NewRecorder()
	VEInitiateHandler(settings, NewGroupDiscussionService(), authn, sender).ServeHTTP(pendingRec, pendingReq)
	if pendingRec.Code != http.StatusAccepted {
		t.Fatalf("pending status=%d body=%s", pendingRec.Code, pendingRec.Body.String())
	}
	var pendingBody map[string]any
	if err := json.NewDecoder(pendingRec.Body).Decode(&pendingBody); err != nil {
		t.Fatalf("decode pending: %v", err)
	}
	requestID, _ := pendingBody["request_id"].(string)
	if requestID == "" {
		t.Fatalf("missing request_id in %v", pendingBody)
	}

	allowRR := doVEMachineJSON(t, VEAuthRespondHandler(settings, authn, sender), http.MethodPost, "/api/ve/auth/respond", map[string]any{
		"request_id": requestID,
		"decision":   "allow_long",
	}, "machine-a", "machine-token")
	if allowRR.Code != http.StatusOK {
		t.Fatalf("allow_long status=%d body=%s", allowRR.Code, allowRR.Body.String())
	}
	registry := loadVERegistry(context.Background(), settings)
	idx := registry.findByIDOrMachineIDOrPlatformEmployeeID("ve_machine-a")
	if idx < 0 || !containsVEValue(registry.Employees[idx].Whitelist, "machine-d") {
		t.Fatalf("allow_long should whitelist machine id fallback: %+v", registry.Employees)
	}
}

func TestDigitalEmployeeAccessListsCompareCaseInsensitiveAndTrimmed(t *testing.T) {
	values := normalizeVEStringList([]string{" User-A ", "user-a", "USER-B"})
	if len(values) != 2 {
		t.Fatalf("normalizeVEStringList length = %d values=%v, want 2", len(values), values)
	}
	entry := digitalEmployeeEntry{
		AccessPolicy: "whitelist",
		Whitelist:    values,
		Blacklist:    []string{" blocked-user "},
	}
	if !veAccessAllowed(entry, "user-a") {
		t.Fatalf("whitelist should match case-insensitively: %+v", entry)
	}
	if !veAccessAllowed(entry, " user-b ") {
		t.Fatalf("whitelist should match trimmed requester ids: %+v", entry)
	}
	if veAccessAllowed(entry, "BLOCKED-USER") {
		t.Fatalf("blacklist should override whitelist case-insensitively: %+v", entry)
	}
}

func TestVEAdminActionEventType(t *testing.T) {
	cases := map[string]string{
		"approve": "ve:approved",
		"reject":  "ve:rejected",
		"disable": "ve:disabled",
	}
	for action, want := range cases {
		if got := veAdminActionEventType(action); got != want {
			t.Fatalf("veAdminActionEventType(%q) = %q, want %q", action, got, want)
		}
	}
}

func TestVEAdminActionPostsPlatformEmployeeCallback(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 2)
	callbackBodies := make(chan map[string]any, 1)
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/hub/callback/employee" {
			t.Fatalf("unexpected callback path %s", r.URL.Path)
		}
		if r.Header.Get("X-VE-Callback-Secret") != "secret-1" || r.Header.Get("X-VE-Callback-Timestamp") == "" || r.Header.Get("X-VE-Callback-Nonce") == "" {
			t.Fatalf("callback headers incomplete: %#v", r.Header)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode callback: %v", err)
		}
		callbackBodies <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer callback.Close()
	provider := platformProviderEntry{PlatformID: "platform-1", CallbackBaseURL: callback.URL, CallbackSecret: "secret-1", RegistrationStatus: "active", TenantDomains: []platformTenantDomain{{HubTenantID: "tenant-a"}}}
	if err := savePlatformProviderRegistry(context.Background(), settings, platformProviderRegistry{Providers: []platformProviderEntry{provider}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "hub-account-1", Status: veStatusPending, OnlineStatus: veOnlineStatusOnline}}}
	if err := saveVERegistry(context.Background(), scopedSystemSettingsForTenant("tenant-a", settings), registry); err != nil {
		t.Fatalf("save ve registry: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/ve/ve_employee_1/approve?tenant_id=tenant-a", nil)
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, &store.AdminUser{ID: "admin-1", Scope: "tenant", TenantID: "tenant-a"}))
	req.SetPathValue("id", "ve_employee_1")
	rec := httptest.NewRecorder()
	VEAdminActionHandler(settings, "approve").ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve status=%d body=%s", rec.Code, rec.Body.String())
	}
	select {
	case body := <-callbackBodies:
		if body["employee_id"] != "platform-employee-1" || body["hub_tenant_id"] != "tenant-a" || body["hub_employee_id"] != "ve_employee_1" || body["hub_account_id"] != "hub-account-1" || body["hub_status"] != "published" {
			t.Fatalf("callback body missing employee identity: %#v", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("employee callback was not posted")
	}
}

func TestDigitalEmployeeAdminActionEmitsMachineEvents(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 1)
	authn := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-a": {TenantID: "tenant-a", UserID: "user-a", MachineID: "machine-a"},
		},
	}
	if rr := doVEMachineJSON(t, VERegisterHandler(settings, authn), http.MethodPost, "/api/ve/register", map[string]any{"name": "Legal Researcher"}, "machine-a", "machine-token"); rr.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", rr.Code, rr.Body.String())
	}

	sender := &fakeVEMachineEventSender{}
	req := httptest.NewRequest(http.MethodPost, "/api/ve/ve_machine-a/approve", nil)
	req = req.WithContext(tenantAdminContext(req.Context(), "tenant-a"))
	req.SetPathValue("id", "ve_machine-a")
	rec := httptest.NewRecorder()
	VEAdminActionHandler(settings, "approve", sender).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(sender.messages) != 3 {
		t.Fatalf("sent events = %d, want 3: %#v", len(sender.messages), sender.messages)
	}
	wantTypes := []string{"ve:approved", "ve:status_change", "ve:list_update"}
	for i, wantType := range wantTypes {
		got := sender.messages[i]
		if got.machineID != "machine-a" || got.msg["type"] != wantType {
			t.Fatalf("event[%d] = machine=%q type=%v, want machine-a %s", i, got.machineID, got.msg["type"], wantType)
		}
		payload, _ := got.msg["payload"].(map[string]any)
		if payload["action"] != "approve" {
			t.Fatalf("event[%d] payload action=%v, want approve", i, payload["action"])
		}
	}
}

func TestDigitalEmployeeAdminApproveRejectsMissingMacLawSrvRuntimeEmployee(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 1)
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/platform/runtime/report" {
			t.Fatalf("unexpected runtime path: %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "users": []map[string]any{}})
	}))
	defer runtime.Close()
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
		Name:               "Deleted Runtime Worker",
		Status:             veStatusPending,
		OnlineStatus:       veOnlineStatusOnline,
	}}}
	if err := saveVERegistry(context.Background(), tenantSystem, registry); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/ve/ve_deleted/approve", nil)
	req = req.WithContext(tenantAdminContext(req.Context(), "tenant-a"))
	req.SetPathValue("id", "ve_deleted")
	rec := httptest.NewRecorder()
	VEAdminActionHandler(settings, "approve").ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict || !bytes.Contains(rec.Body.Bytes(), []byte("VE_RUNTIME_MISSING")) {
		t.Fatalf("approve missing runtime status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated := loadVERegistry(context.Background(), tenantSystem)
	if len(updated.Employees) != 1 || updated.Employees[0].Status != veStatusDisabled || updated.Employees[0].OnlineStatus != veOnlineStatusOffline {
		t.Fatalf("missing runtime employee should be marked disabled/offline: %#v", updated.Employees)
	}
}

func TestDigitalEmployeeAdminApproveRejectsNonReadyMacLawSrvRuntimeEmployee(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 1)
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/platform/runtime/report" {
			t.Fatalf("unexpected runtime path: %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ok",
			"users":  []map[string]any{{"employee_id": "attention-employee", "runtime_status": "attention"}},
		})
	}))
	defer runtime.Close()
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: runtime.URL, TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{
		ID:                 "ve_attention",
		MachineID:          "ve_attention",
		PlatformID:         maclawSrvRuntimePlatformID,
		PlatformEmployeeID: "attention-employee",
		RuntimeProviderID:  maclawSrvRuntimePlatformID,
		Name:               "Attention Runtime Worker",
		Status:             veStatusPending,
		OnlineStatus:       veOnlineStatusOnline,
	}}}
	if err := saveVERegistry(context.Background(), tenantSystem, registry); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/ve/ve_attention/approve", nil)
	req = req.WithContext(tenantAdminContext(req.Context(), "tenant-a"))
	req.SetPathValue("id", "ve_attention")
	rec := httptest.NewRecorder()
	VEAdminActionHandler(settings, "approve").ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict || !bytes.Contains(rec.Body.Bytes(), []byte("VE_NOT_ONLINE")) {
		t.Fatalf("approve non-ready runtime status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated := loadVERegistry(context.Background(), tenantSystem)
	if len(updated.Employees) != 1 || updated.Employees[0].Status != veStatusPending || updated.Employees[0].OnlineStatus != veOnlineStatusOffline {
		t.Fatalf("non-ready runtime employee should stay pending/offline: %#v", updated.Employees)
	}
}

func TestVEAdminActionDeleteRemovesDigitalEmployee(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	now := time.Now().UTC().Format(time.RFC3339)
	if err := saveVERegistry(context.Background(), tenantSystem, digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{
		{ID: "ve_keep", MachineID: "machine-keep", Name: "Keep", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline, RegisteredAt: now},
		{ID: "ve_ghost", MachineID: "machine-ghost", PlatformID: maclawSrvRuntimePlatformID, PlatformEmployeeID: "deleted-employee", Name: "Deleted Runtime Worker", Status: veStatusDisabled, OnlineStatus: veOnlineStatusOffline, Resident: true, RegisteredAt: now},
	}}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/ve/deleted-employee", nil)
	req = req.WithContext(tenantAdminContext(req.Context(), "tenant-a"))
	req.SetPathValue("id", "deleted-employee")
	rec := httptest.NewRecorder()
	VEAdminActionHandler(settings, "delete").ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated := loadVERegistry(context.Background(), tenantSystem)
	if len(updated.Employees) != 1 {
		t.Fatalf("employees len=%d, want 1: %#v", len(updated.Employees), updated.Employees)
	}
	if updated.Employees[0].ID != "ve_keep" {
		t.Fatalf("remaining employee=%q, want ve_keep", updated.Employees[0].ID)
	}
	if updated.findByIDOrMachineIDOrPlatformEmployeeID("deleted-employee") >= 0 {
		t.Fatal("deleted platform employee still found")
	}
}

func TestVEAdminActionDeleteRejectsActivePhysicalEmployee(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	if err := saveVERegistry(context.Background(), tenantSystem, digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{
		ID: "ve_active", MachineID: "machine-active", EmployeeType: veEmployeeTypePhysical, Name: "Active Worker", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline,
	}}}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/ve/ve_active", nil)
	req = req.WithContext(tenantAdminContext(req.Context(), "tenant-a"))
	req.SetPathValue("id", "ve_active")
	rec := httptest.NewRecorder()
	VEAdminActionHandler(settings, "delete").ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict || !bytes.Contains(rec.Body.Bytes(), []byte("VE_DELETE_ACTIVE_FORBIDDEN")) {
		t.Fatalf("delete active status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated := loadVERegistry(context.Background(), tenantSystem)
	if len(updated.Employees) != 1 || updated.Employees[0].ID != "ve_active" {
		t.Fatalf("active employee should remain: %#v", updated.Employees)
	}
}

func TestVEAdminActionDeleteAllowsActiveMissingMacLawSrvRuntimeEmployee(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/platform/runtime/report" {
			t.Fatalf("unexpected runtime path: %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "users": []map[string]any{}})
	}))
	defer runtime.Close()
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: runtime.URL, TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	if err := saveVERegistry(context.Background(), tenantSystem, digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{
		ID: "ve_ghost", MachineID: "machine-ghost", PlatformID: maclawSrvRuntimePlatformID, PlatformEmployeeID: "deleted-employee", Name: "Deleted Runtime Worker", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline,
	}}}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/ve/deleted-employee", nil)
	req = req.WithContext(tenantAdminContext(req.Context(), "tenant-a"))
	req.SetPathValue("id", "deleted-employee")
	rec := httptest.NewRecorder()
	VEAdminActionHandler(settings, "delete").ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("delete missing runtime status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated := loadVERegistry(context.Background(), tenantSystem)
	if len(updated.Employees) != 0 {
		t.Fatalf("missing runtime employee should be removed: %#v", updated.Employees)
	}
}

func newVEAdminForceDeleteTestServices(t *testing.T) (*store.Store, *auth.AdminService, func()) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "force-delete.db")
	provider, err := sqlite.NewProvider(sqlite.Config{DSN: dbPath, WAL: true, BusyTimeoutMS: 5000, MaxReadOpenConns: 2, MaxReadIdleConns: 1, MaxWriteOpenConns: 1, MaxWriteIdleConns: 1})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		_ = provider.Close()
		t.Fatalf("run migrations: %v", err)
	}
	st := sqlite.NewStore(provider)
	admins := auth.NewAdminService(st.Admins, st.System, st.AdminAudit)
	cleanup := func() { _ = provider.Close() }
	return st, admins, cleanup
}

func TestVEAdminForceDeleteRejectsWrongAdminPassword(t *testing.T) {
	st, admins, cleanup := newVEAdminForceDeleteTestServices(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := admins.CreateTenantAdmin(ctx, "tenant-a", "tenant-admin", "correct-password", "tenant-admin@example.com", "Tenant Admin", "tenant_admin"); err != nil {
		t.Fatalf("create tenant admin: %v", err)
	}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", st.System)
	if err := saveVERegistry(ctx, tenantSystem, digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_ghost", MachineID: "machine-ghost", Name: "Ghost", Status: veStatusDisabled, OnlineStatus: veOnlineStatusOffline}}}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/ve/ve_ghost/force-delete", strings.NewReader(`{"admin_password":"wrong-password"}`))
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, &store.AdminUser{ID: "admin-tenant", Username: "tenant-admin", Scope: "tenant", TenantID: "tenant-a"}))
	req.SetPathValue("id", "ve_ghost")
	rec := httptest.NewRecorder()
	VEAdminForceDeleteHandler(st.System, NewGroupDiscussionService(), admins).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized || !bytes.Contains(rec.Body.Bytes(), []byte("INVALID_ADMIN_PASSWORD")) {
		t.Fatalf("wrong password status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated := loadVERegistry(ctx, tenantSystem)
	if len(updated.Employees) != 1 {
		t.Fatalf("wrong password should not delete registry: %#v", updated.Employees)
	}
}

func TestVEAdminForceDeleteRemovesRegistryAndHistory(t *testing.T) {
	st, admins, cleanup := newVEAdminForceDeleteTestServices(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := admins.CreateTenantAdmin(ctx, "tenant-a", "tenant-admin", "correct-password", "tenant-admin@example.com", "Tenant Admin", "tenant_admin"); err != nil {
		t.Fatalf("create tenant admin: %v", err)
	}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", st.System)
	if err := saveVERegistry(ctx, tenantSystem, digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_ghost", MachineID: "machine-ghost", PlatformEmployeeID: "deleted-employee", Name: "Ghost", Status: veStatusDisabled, OnlineStatus: veOnlineStatusOffline}}}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	groupSvc := NewGroupDiscussionService()
	if _, err := groupSvc.CreateSession("tenant-a", CreateSessionRequest{Topic: "ghost history", Participants: []corea2a.Participant{{ID: "owner", RoleCode: "initiator"}, {ID: "machine-ghost", RoleCode: "speak"}}, DecisionPolicy: corea2a.PolicyMajority}); err != nil {
		t.Fatalf("create ghost session: %v", err)
	}
	if _, err := groupSvc.CreateSession("tenant-a", CreateSessionRequest{Topic: "keep history", Participants: []corea2a.Participant{{ID: "owner", RoleCode: "initiator"}, {ID: "other-machine", RoleCode: "speak"}}, DecisionPolicy: corea2a.PolicyMajority}); err != nil {
		t.Fatalf("create keep session: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/ve/ve_ghost/force-delete", strings.NewReader(`{"admin_password":"correct-password"}`))
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, &store.AdminUser{ID: "admin-tenant", Username: "tenant-admin", Scope: "tenant", TenantID: "tenant-a"}))
	req.SetPathValue("id", "ve_ghost")
	rec := httptest.NewRecorder()
	VEAdminForceDeleteHandler(st.System, groupSvc, admins).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("force delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated := loadVERegistry(ctx, tenantSystem)
	if len(updated.Employees) != 0 {
		t.Fatalf("force delete should remove registry: %#v", updated.Employees)
	}
	ghostHistory, err := groupSvc.ListDiscussionSummaries("tenant-a", ListSessionsFilter{ParticipantID: "machine-ghost"})
	if err != nil {
		t.Fatalf("list ghost history: %v", err)
	}
	if len(ghostHistory) != 0 {
		t.Fatalf("force delete should remove ghost history: %#v", ghostHistory)
	}
	keepHistory, err := groupSvc.ListDiscussionSummaries("tenant-a", ListSessionsFilter{ParticipantID: "other-machine"})
	if err != nil {
		t.Fatalf("list keep history: %v", err)
	}
	if len(keepHistory) != 1 {
		t.Fatalf("force delete should keep unrelated history: %#v", keepHistory)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"deleted_history_sessions":1`)) {
		t.Fatalf("force delete response should include deleted history count: %s", rec.Body.String())
	}
}

func TestDigitalEmployeeAuthorizationBlocksRegisterAndDiscovery(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	authn := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-a": {TenantID: "tenant-a", UserID: "user-a", MachineID: "machine-a"},
			"machine-b": {TenantID: "tenant-a", UserID: "user-b", MachineID: "machine-b"},
		},
	}

	registerRR := doVEMachineJSON(t, VERegisterHandler(settings, authn), http.MethodPost, "/api/ve/register", map[string]any{
		"name": "Legal Researcher",
	}, "machine-a", "machine-token")
	if registerRR.Code != http.StatusForbidden {
		t.Fatalf("expected inactive authorization to block registration, got %d body=%s", registerRR.Code, registerRR.Body.String())
	}

	enableVEDigitalEmployeeAuthorization(t, settings, 1)
	if rr := doVEMachineJSON(t, VERegisterHandler(settings, authn), http.MethodPost, "/api/ve/register", map[string]any{"name": "Legal Researcher"}, "machine-a", "machine-token"); rr.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", rr.Code, rr.Body.String())
	}
	approveReq := httptest.NewRequest(http.MethodPost, "/api/ve/ve_machine-a/approve", nil)
	approveReq = approveReq.WithContext(tenantAdminContext(approveReq.Context(), "tenant-a"))
	approveReq.SetPathValue("id", "ve_machine-a")
	approveRec := httptest.NewRecorder()
	VEAdminActionHandler(settings, "approve").ServeHTTP(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve status=%d body=%s", approveRec.Code, approveRec.Body.String())
	}

	disableVEDigitalEmployeeAuthorization(t, settings)
	discoverRR := doVEMachineJSON(t, VEDiscoverableHandler(settings, authn), http.MethodGet, "/api/ve/discoverable", nil, "machine-b", "machine-token")
	if discoverRR.Code != http.StatusOK || bytes.Contains(discoverRR.Body.Bytes(), []byte(`"id":"ve_machine-a"`)) {
		t.Fatalf("expected inactive authorization to hide employees, status=%d body=%s", discoverRR.Code, discoverRR.Body.String())
	}
}

func TestVEDiscoverableReturnsETagAndNotModified(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 1)
	authn := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-a": {TenantID: "tenant-a", UserID: "user-a", MachineID: "machine-a"},
			"machine-b": {TenantID: "tenant-a", UserID: "user-b", MachineID: "machine-b"},
		},
	}
	if rr := doVEMachineJSON(t, VERegisterHandler(settings, authn), http.MethodPost, "/api/ve/register", map[string]any{"name": "Legal Researcher"}, "machine-a", "machine-token"); rr.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", rr.Code, rr.Body.String())
	}
	approveReq := httptest.NewRequest(http.MethodPost, "/api/ve/ve_machine-a/approve", nil)
	approveReq = approveReq.WithContext(tenantAdminContext(approveReq.Context(), "tenant-a"))
	approveReq.SetPathValue("id", "ve_machine-a")
	approveRec := httptest.NewRecorder()
	VEAdminActionHandler(settings, "approve").ServeHTTP(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve status=%d body=%s", approveRec.Code, approveRec.Body.String())
	}

	handler := VEDiscoverableHandler(settings, authn)
	firstRR := doVEMachineJSON(t, handler, http.MethodGet, "/api/ve/discoverable", nil, "machine-b", "machine-token")
	if firstRR.Code != http.StatusOK {
		t.Fatalf("first discover status=%d body=%s", firstRR.Code, firstRR.Body.String())
	}
	etag := firstRR.Header().Get("ETag")
	if etag == "" {
		t.Fatalf("expected ETag on discoverable response")
	}
	if cacheControl := firstRR.Header().Get("Cache-Control"); !strings.Contains(cacheControl, "max-age=5") {
		t.Fatalf("unexpected Cache-Control: %q", cacheControl)
	}
	if firstRR.Header().Get("X-VE-Cache") != "miss" {
		t.Fatalf("expected initial cache miss header, got %q", firstRR.Header().Get("X-VE-Cache"))
	}
	if !bytes.Contains(firstRR.Body.Bytes(), []byte(`"id":"ve_machine-a"`)) {
		t.Fatalf("discoverable response should include approved employee: %s", firstRR.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/ve/discoverable", nil)
	req.Header.Set("X-Machine-ID", "machine-b")
	req.Header.Set("Authorization", "Bearer machine-token")
	req.Header.Set("If-None-Match", etag)
	secondRR := httptest.NewRecorder()
	handler.ServeHTTP(secondRR, req)
	if secondRR.Code != http.StatusNotModified {
		t.Fatalf("second discover status=%d body=%s", secondRR.Code, secondRR.Body.String())
	}
	if secondRR.Header().Get("X-VE-Cache") != "hit" {
		t.Fatalf("expected cache hit header on 304, got %q", secondRR.Header().Get("X-VE-Cache"))
	}
	if secondRR.Body.Len() != 0 {
		t.Fatalf("304 response should be empty, got %q", secondRR.Body.String())
	}
}

func TestVEMetricsTracksDiscoverableRequests(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 1)
	authn := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-a": {TenantID: "tenant-a", UserID: "user-a", MachineID: "machine-a"},
			"machine-b": {TenantID: "tenant-a", UserID: "user-b", MachineID: "machine-b"},
		},
	}
	if rr := doVEMachineJSON(t, VERegisterHandler(settings, authn), http.MethodPost, "/api/ve/register", map[string]any{"name": "Legal Researcher"}, "machine-a", "machine-token"); rr.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", rr.Code, rr.Body.String())
	}
	approveReq := httptest.NewRequest(http.MethodPost, "/api/ve/ve_machine-a/approve", nil)
	approveReq = approveReq.WithContext(tenantAdminContext(approveReq.Context(), "tenant-a"))
	approveReq.SetPathValue("id", "ve_machine-a")
	approveRec := httptest.NewRecorder()
	VEAdminActionHandler(settings, "approve").ServeHTTP(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve status=%d body=%s", approveRec.Code, approveRec.Body.String())
	}

	before := globalVEMetrics.snapshot().Discoverable
	handler := VEDiscoverableHandler(settings, authn)
	firstRR := doVEMachineJSON(t, handler, http.MethodGet, "/api/ve/discoverable", nil, "machine-b", "machine-token")
	if firstRR.Code != http.StatusOK {
		t.Fatalf("first discover status=%d body=%s", firstRR.Code, firstRR.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/api/ve/discoverable", nil)
	req.Header.Set("X-Machine-ID", "machine-b")
	req.Header.Set("Authorization", "Bearer machine-token")
	req.Header.Set("If-None-Match", firstRR.Header().Get("ETag"))
	secondRR := httptest.NewRecorder()
	handler.ServeHTTP(secondRR, req)
	if secondRR.Code != http.StatusNotModified {
		t.Fatalf("second discover status=%d body=%s", secondRR.Code, secondRR.Body.String())
	}
	after := globalVEMetrics.snapshot().Discoverable
	if got := veMetricUint(after, "requests_total") - veMetricUint(before, "requests_total"); got != 2 {
		t.Fatalf("requests delta=%d, want 2; metrics=%#v", got, after)
	}
	if got := veMetricUint(after, "success_total") - veMetricUint(before, "success_total"); got != 1 {
		t.Fatalf("success delta=%d, want 1; metrics=%#v", got, after)
	}
	if got := veMetricUint(after, "not_modified_total") - veMetricUint(before, "not_modified_total"); got != 1 {
		t.Fatalf("not_modified delta=%d, want 1; metrics=%#v", got, after)
	}
	if got := veMetricUint(after, "employees_returned_total") - veMetricUint(before, "employees_returned_total"); got != 1 {
		t.Fatalf("employees_returned delta=%d, want 1; metrics=%#v", got, after)
	}

	metricsRR := httptest.NewRecorder()
	metricsReq := httptest.NewRequest(http.MethodGet, "/api/admin/ve/metrics", nil)
	metricsReq = metricsReq.WithContext(tenantAdminContext(metricsReq.Context(), "tenant-a"))
	VEMetricsHandler().ServeHTTP(metricsRR, metricsReq)
	if metricsRR.Code != http.StatusOK || !bytes.Contains(metricsRR.Body.Bytes(), []byte(`"discoverable"`)) {
		t.Fatalf("metrics status=%d body=%s", metricsRR.Code, metricsRR.Body.String())
	}
}

func TestVEMetricsExposeOperationalLimits(t *testing.T) {
	rr := httptest.NewRecorder()
	VEMetricsHandler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/admin/ve/metrics", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("metrics status=%d body=%s", rr.Code, rr.Body.String())
	}
	var snapshot veMetricsSnapshot
	if err := json.Unmarshal(rr.Body.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode metrics: %v body=%s", err, rr.Body.String())
	}
	if veMetricUint(snapshot.Discoverable, "build_concurrency_limit") != uint64(veDiscoverableBuildConcurrency) {
		t.Fatalf("discoverable limits missing/wrong: %#v", snapshot.Discoverable)
	}
	if veMetricUint(snapshot.Discoverable, "cache_max_keys") != uint64(veDiscoverableCacheMaxKeys) {
		t.Fatalf("discoverable cache max missing/wrong: %#v", snapshot.Discoverable)
	}
	if veMetricUint(snapshot.RuntimeDelivery, "concurrency_limit") != uint64(veRuntimeDeliveryConcurrency) {
		t.Fatalf("runtime limits missing/wrong: %#v", snapshot.RuntimeDelivery)
	}
	if veMetricUint(snapshot.RuntimeDelivery, "delivery_timeout_sec") != uint64(platformA2ADeliveryTimeout.Seconds()) {
		t.Fatalf("runtime timeout missing/wrong: %#v", snapshot.RuntimeDelivery)
	}
	if veMetricUint(snapshot.RuntimeDelivery, "circuit_failure_limit") != uint64(veRuntimeCircuitFailureLimit) {
		t.Fatalf("runtime circuit config missing/wrong: %#v", snapshot.RuntimeDelivery)
	}
	if veMetricUint(snapshot.RuntimeDelivery, "circuit_failure_window_seconds") != uint64(veRuntimeCircuitFailureWindow.Seconds()) {
		t.Fatalf("runtime circuit failure window missing/wrong: %#v", snapshot.RuntimeDelivery)
	}
}

func TestVEMetricsTrackInFlightHighWaterMarks(t *testing.T) {
	globalVEMetrics.BuildInFlightMax.Store(0)
	globalVERuntimeDeliveryMetrics.InFlightMax.Store(0)
	beforeRuntimeInFlight := globalVERuntimeDeliveryMetrics.InFlight.Load()

	releaseDiscoverableOne, ok := acquireVEDiscoverableBuildSlot()
	if !ok {
		t.Fatal("expected first discoverable build slot")
	}
	defer releaseDiscoverableOne()
	releaseDiscoverableTwo, ok := acquireVEDiscoverableBuildSlot()
	if !ok {
		t.Fatal("expected second discoverable build slot")
	}
	defer releaseDiscoverableTwo()
	releaseRuntimeOne, ok := acquireVERuntimeDeliverySlot()
	if !ok {
		t.Fatal("expected first runtime delivery slot")
	}
	defer releaseRuntimeOne(nil)
	releaseRuntimeTwo, ok := acquireVERuntimeDeliverySlot()
	if !ok {
		t.Fatal("expected second runtime delivery slot")
	}
	defer releaseRuntimeTwo(nil)

	snapshot := globalVEMetrics.snapshot()
	if got := veMetricUint(snapshot.Discoverable, "build_in_flight_max"); got < 2 {
		t.Fatalf("build_in_flight_max=%d, want >=2; metrics=%#v", got, snapshot.Discoverable)
	}
	if got := veMetricUint(snapshot.RuntimeDelivery, "in_flight_max"); got < uint64(beforeRuntimeInFlight+2) {
		t.Fatalf("in_flight_max=%d, want >=%d; metrics=%#v", got, beforeRuntimeInFlight+2, snapshot.RuntimeDelivery)
	}
}

func TestVESlotReleaseIsIdempotent(t *testing.T) {
	beforeRuntimeInFlight := globalVERuntimeDeliveryMetrics.InFlight.Load()
	beforeRuntimeCompleted := globalVERuntimeDeliveryMetrics.CompletedTotal.Load()

	releaseDiscoverable, ok := acquireVEDiscoverableBuildSlot()
	if !ok {
		t.Fatal("expected discoverable build slot")
	}
	releaseDiscoverable()
	done := make(chan struct{})
	go func() {
		releaseDiscoverable()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("second discoverable release blocked")
	}

	releaseRuntime, ok := acquireVERuntimeDeliverySlot()
	if !ok {
		t.Fatal("expected runtime delivery slot")
	}
	releaseRuntime(nil)
	releaseRuntime(errors.New("ignored duplicate release"))
	if got := globalVERuntimeDeliveryMetrics.InFlight.Load(); got != beforeRuntimeInFlight {
		t.Fatalf("runtime in_flight=%d, want %d", got, beforeRuntimeInFlight)
	}
	if got := globalVERuntimeDeliveryMetrics.CompletedTotal.Load() - beforeRuntimeCompleted; got != 1 {
		t.Fatalf("runtime completed delta=%d, want 1", got)
	}
}

func TestVEDiscoverableCacheExpiryBoundaries(t *testing.T) {
	now := time.Now()
	key := "tenant-a|user-cache-boundary|machine-cache-boundary"
	payload := []byte(`{"employees":[],"max_group_participants":3}`)
	etag := veResponseETag(payload)

	veDiscoverableCache.Lock()
	veDiscoverableCache.Entries = map[string]veDiscoverableCacheEntry{
		key: {Data: payload, ETag: etag, ExpiresAt: now, Employees: 0},
	}
	veDiscoverableCache.Unlock()
	if _, ok := getVEDiscoverableCache(key, now.Add(-time.Nanosecond)); !ok {
		t.Fatal("cache should be fresh before ExpiresAt")
	}
	if _, ok := getVEDiscoverableCache(key, now); ok {
		t.Fatal("cache should expire at ExpiresAt")
	}

	veDiscoverableCache.Lock()
	veDiscoverableCache.Entries = map[string]veDiscoverableCacheEntry{
		key: {Data: payload, ETag: etag, ExpiresAt: now, Employees: 0},
	}
	veDiscoverableCache.Unlock()
	if _, ok := getVEDiscoverableStaleCache(key, now.Add(veDiscoverableStaleTTL-time.Nanosecond)); !ok {
		t.Fatal("cache should be stale-servable before stale TTL")
	}
	if _, ok := getVEDiscoverableStaleCache(key, now.Add(veDiscoverableStaleTTL)); ok {
		t.Fatal("cache should not be stale-servable at stale TTL")
	}
	if size := veDiscoverableCacheSize(); size != 0 {
		t.Fatalf("expired stale cache entry should be removed, size=%d", size)
	}
}

func TestVEDiscoverableCacheEvictsOldestEntryWhenFull(t *testing.T) {
	originalMaxKeys := veDiscoverableCacheMaxKeys
	veDiscoverableCacheMaxKeys = 2
	defer func() { veDiscoverableCacheMaxKeys = originalMaxKeys }()
	now := time.Now()
	payload := []byte(`{"employees":[],"max_group_participants":3}`)
	etag := veResponseETag(payload)

	veDiscoverableCache.Lock()
	veDiscoverableCache.Entries = map[string]veDiscoverableCacheEntry{
		"old": {Data: payload, ETag: etag, ExpiresAt: now.Add(time.Second), Employees: 0},
		"new": {Data: payload, ETag: etag, ExpiresAt: now.Add(time.Hour), Employees: 0},
	}
	veDiscoverableCache.Unlock()

	setVEDiscoverableCache("inserted", payload, etag, 0, now)

	if size := veDiscoverableCacheSize(); size != 2 {
		t.Fatalf("cache size=%d, want 2", size)
	}
	if _, ok := getVEDiscoverableCache("old", now); ok {
		t.Fatal("oldest entry should be evicted")
	}
	if _, ok := getVEDiscoverableCache("new", now); !ok {
		t.Fatal("newer entry should be retained")
	}
	if _, ok := getVEDiscoverableCache("inserted", now); !ok {
		t.Fatal("inserted entry should be retained")
	}
}

func TestVERuntimeCircuitOpenMetricOnlyCountsTransitions(t *testing.T) {
	key := "tenant-a|ve-circuit-transition"
	now := time.Now()
	veRuntimeDeliveryCircuit.Lock()
	veRuntimeDeliveryCircuit.Entries = map[string]veRuntimeDeliveryCircuitEntry{}
	veRuntimeDeliveryCircuit.Unlock()
	before := globalVEMetrics.snapshot().RuntimeDelivery

	for i := 0; i < veRuntimeCircuitFailureLimit+2; i++ {
		recordVERuntimeDeliveryResult(key, errors.New("MaClawSrv runtime returned status 502"), now)
	}
	afterFirstOpen := globalVEMetrics.snapshot().RuntimeDelivery
	if got := veMetricUint(afterFirstOpen, "circuit_open_total") - veMetricUint(before, "circuit_open_total"); got != 1 {
		t.Fatalf("first open delta=%d, want 1; metrics=%#v", got, afterFirstOpen)
	}

	reopenAt := now.Add(veRuntimeCircuitOpenDuration)
	recordVERuntimeDeliveryResult(key, errors.New("MaClawSrv runtime returned status 502"), reopenAt)
	afterFirstPostExpiryFailure := globalVEMetrics.snapshot().RuntimeDelivery
	if got := veMetricUint(afterFirstPostExpiryFailure, "circuit_open_total") - veMetricUint(afterFirstOpen, "circuit_open_total"); got != 0 {
		t.Fatalf("post-expiry first failure reopen delta=%d, want 0; metrics=%#v", got, afterFirstPostExpiryFailure)
	}

	for i := 1; i < veRuntimeCircuitFailureLimit; i++ {
		recordVERuntimeDeliveryResult(key, errors.New("MaClawSrv runtime returned status 502"), reopenAt)
	}
	afterReopen := globalVEMetrics.snapshot().RuntimeDelivery
	if got := veMetricUint(afterReopen, "circuit_open_total") - veMetricUint(afterFirstPostExpiryFailure, "circuit_open_total"); got != 1 {
		t.Fatalf("reopen delta=%d, want 1; metrics=%#v", got, afterReopen)
	}
}

func TestVERuntimeCircuitFailureWindowResetsOldFailures(t *testing.T) {
	key := "tenant-a|ve-circuit-window"
	now := time.Now()
	veRuntimeDeliveryCircuit.Lock()
	veRuntimeDeliveryCircuit.Entries = map[string]veRuntimeDeliveryCircuitEntry{}
	veRuntimeDeliveryCircuit.Unlock()
	before := globalVEMetrics.snapshot().RuntimeDelivery

	for i := 0; i < veRuntimeCircuitFailureLimit; i++ {
		recordVERuntimeDeliveryResult(key, errors.New("MaClawSrv runtime returned status 502"), now.Add(time.Duration(i)*(veRuntimeCircuitFailureWindow+time.Second)))
	}
	afterSlowFailures := globalVEMetrics.snapshot().RuntimeDelivery
	if got := veMetricUint(afterSlowFailures, "circuit_open_total") - veMetricUint(before, "circuit_open_total"); got != 0 {
		t.Fatalf("slow failure open delta=%d, want 0; metrics=%#v", got, afterSlowFailures)
	}

	for i := 0; i < veRuntimeCircuitFailureLimit; i++ {
		recordVERuntimeDeliveryResult(key, errors.New("MaClawSrv runtime returned status 502"), now.Add(10*veRuntimeCircuitFailureWindow))
	}
	afterBurstFailures := globalVEMetrics.snapshot().RuntimeDelivery
	if got := veMetricUint(afterBurstFailures, "circuit_open_total") - veMetricUint(afterSlowFailures, "circuit_open_total"); got != 1 {
		t.Fatalf("burst failure open delta=%d, want 1; metrics=%#v", got, afterBurstFailures)
	}
}

func TestVERuntimeCircuitOpenRefreshesMetricsTimestamp(t *testing.T) {
	key := "tenant-a|ve-circuit-updated"
	veRuntimeDeliveryCircuit.Lock()
	veRuntimeDeliveryCircuit.Entries = map[string]veRuntimeDeliveryCircuitEntry{}
	veRuntimeDeliveryCircuit.Unlock()
	previousUpdated := globalVERuntimeDeliveryMetrics.LastUpdatedUnix.Load()

	for i := 0; i < veRuntimeCircuitFailureLimit; i++ {
		recordVERuntimeDeliveryResult(key, errors.New("MaClawSrv runtime returned status 502"), time.Now())
	}
	if got := globalVERuntimeDeliveryMetrics.LastUpdatedUnix.Load(); got < previousUpdated || got == 0 {
		t.Fatalf("last_updated_unix=%d, previous=%d", got, previousUpdated)
	}
}

func TestVERuntimeCircuitIgnoresLateSuccessWhileOpen(t *testing.T) {
	key := "tenant-a|ve-circuit-late-success"
	now := time.Now()
	veRuntimeDeliveryCircuit.Lock()
	veRuntimeDeliveryCircuit.Entries = map[string]veRuntimeDeliveryCircuitEntry{}
	veRuntimeDeliveryCircuit.Unlock()

	for i := 0; i < veRuntimeCircuitFailureLimit; i++ {
		recordVERuntimeDeliveryResult(key, errors.New("MaClawSrv runtime returned status 502"), now)
	}
	recordVERuntimeDeliveryResult(key, nil, now.Add(time.Second))
	if veRuntimeDeliveryCircuitAllows(key, now.Add(2*time.Second)) {
		t.Fatal("late success should not close an open circuit")
	}

	recordVERuntimeDeliveryResult(key, nil, now.Add(veRuntimeCircuitOpenDuration+time.Second))
	if !veRuntimeDeliveryCircuitAllows(key, now.Add(veRuntimeCircuitOpenDuration+2*time.Second)) {
		t.Fatal("success after open window should allow runtime delivery")
	}
}

func TestVERuntimeCircuitSuccessClearsFailureAccumulator(t *testing.T) {
	key := "tenant-a|ve-circuit-success-clear"
	now := time.Now()
	veRuntimeDeliveryCircuit.Lock()
	veRuntimeDeliveryCircuit.Entries = map[string]veRuntimeDeliveryCircuitEntry{}
	veRuntimeDeliveryCircuit.Unlock()
	before := globalVEMetrics.snapshot().RuntimeDelivery

	recordVERuntimeDeliveryResult(key, errors.New("MaClawSrv runtime returned status 502"), now)
	recordVERuntimeDeliveryResult(key, nil, now.Add(time.Second))
	for i := 0; i < veRuntimeCircuitFailureLimit-1; i++ {
		recordVERuntimeDeliveryResult(key, errors.New("MaClawSrv runtime returned status 502"), now.Add(2*time.Second))
	}
	after := globalVEMetrics.snapshot().RuntimeDelivery
	if got := veMetricUint(after, "circuit_open_total") - veMetricUint(before, "circuit_open_total"); got != 0 {
		t.Fatalf("circuit_open delta=%d, want 0 after success cleared accumulator; metrics=%#v", got, after)
	}
}

func TestVEIntEnvUsesConfiguredValueWithinBounds(t *testing.T) {
	t.Setenv("HUB_VE_TEST_INT", "42")
	if got := veIntEnv("HUB_VE_TEST_INT", 7, 1, 100); got != 42 {
		t.Fatalf("veIntEnv returned %d, want 42", got)
	}
}

func TestVEIntEnvFallsBackForInvalidOrOutOfRangeValues(t *testing.T) {
	t.Setenv("HUB_VE_TEST_INT_INVALID", "nope")
	if got := veIntEnv("HUB_VE_TEST_INT_INVALID", 7, 1, 100); got != 7 {
		t.Fatalf("invalid env returned %d, want fallback 7", got)
	}
	t.Setenv("HUB_VE_TEST_INT_LOW", "0")
	if got := veIntEnv("HUB_VE_TEST_INT_LOW", 7, 1, 100); got != 7 {
		t.Fatalf("low env returned %d, want fallback 7", got)
	}
	t.Setenv("HUB_VE_TEST_INT_HIGH", "101")
	if got := veIntEnv("HUB_VE_TEST_INT_HIGH", 7, 1, 100); got != 7 {
		t.Fatalf("high env returned %d, want fallback 7", got)
	}
}

func TestClassifyVERuntimeDeliveryFailure(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{name: "deadline", err: context.DeadlineExceeded, want: "timeout"},
		{name: "timeout text", err: errors.New("Client.Timeout exceeded while awaiting headers"), want: "timeout"},
		{name: "http status", err: errors.New("MaClawSrv runtime returned status 502: down"), want: "http_status"},
		{name: "empty json reply", err: errors.New("MaClawSrv runtime response did not include assistant content"), want: "empty_reply"},
		{name: "empty sse reply", err: errors.New("MaClawSrv runtime SSE response did not include content"), want: "empty_reply"},
		{name: "transport", err: errors.New("MaClawSrv runtime delivery failed: dial tcp refused"), want: "transport"},
		{name: "other", err: errors.New("unexpected runtime error"), want: "other"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyVERuntimeDeliveryFailure(tc.err); got != tc.want {
				t.Fatalf("classify=%q, want %q", got, tc.want)
			}
		})
	}
}

func TestVEDiscoverableUsesShortServerCache(t *testing.T) {
	veDiscoverableCache.Lock()
	veDiscoverableCache.Entries = map[string]veDiscoverableCacheEntry{}
	veDiscoverableCache.Unlock()
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 1)
	authn := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-cache-a": {TenantID: "tenant-a", UserID: "user-cache-a", MachineID: "machine-cache-a"},
			"machine-cache-b": {TenantID: "tenant-a", UserID: "user-cache-b", MachineID: "machine-cache-b"},
		},
	}
	if rr := doVEMachineJSON(t, VERegisterHandler(settings, authn), http.MethodPost, "/api/ve/register", map[string]any{"name": "Cache Researcher"}, "machine-cache-a", "machine-token"); rr.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", rr.Code, rr.Body.String())
	}
	approveReq := httptest.NewRequest(http.MethodPost, "/api/ve/ve_machine-cache-a/approve", nil)
	approveReq = approveReq.WithContext(tenantAdminContext(approveReq.Context(), "tenant-a"))
	approveReq.SetPathValue("id", "ve_machine-cache-a")
	approveRec := httptest.NewRecorder()
	VEAdminActionHandler(settings, "approve").ServeHTTP(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve status=%d body=%s", approveRec.Code, approveRec.Body.String())
	}

	before := globalVEMetrics.snapshot().Discoverable
	handler := VEDiscoverableHandler(settings, authn)
	firstRR := doVEMachineJSON(t, handler, http.MethodGet, "/api/ve/discoverable", nil, "machine-cache-b", "machine-token")
	if firstRR.Code != http.StatusOK {
		t.Fatalf("first discover status=%d body=%s", firstRR.Code, firstRR.Body.String())
	}
	secondRR := doVEMachineJSON(t, handler, http.MethodGet, "/api/ve/discoverable", nil, "machine-cache-b", "machine-token")
	if secondRR.Code != http.StatusOK {
		t.Fatalf("second discover status=%d body=%s", secondRR.Code, secondRR.Body.String())
	}
	if firstRR.Header().Get("X-VE-Cache") != "miss" {
		t.Fatalf("expected initial cache miss header, got %q", firstRR.Header().Get("X-VE-Cache"))
	}
	if secondRR.Header().Get("X-VE-Cache") != "hit" {
		t.Fatalf("expected short cache hit header, got %q", secondRR.Header().Get("X-VE-Cache"))
	}
	after := globalVEMetrics.snapshot().Discoverable
	if got := veMetricUint(after, "cache_miss_total") - veMetricUint(before, "cache_miss_total"); got != 1 {
		t.Fatalf("cache miss delta=%d, want 1; metrics=%#v", got, after)
	}
	if got := veMetricUint(after, "cache_hit_total") - veMetricUint(before, "cache_hit_total"); got != 1 {
		t.Fatalf("cache hit delta=%d, want 1; metrics=%#v", got, after)
	}
	if firstRR.Body.String() != secondRR.Body.String() {
		t.Fatalf("cached response should match first response\nfirst=%s\nsecond=%s", firstRR.Body.String(), secondRR.Body.String())
	}
}

func TestVEDiscoverableCoalescesConcurrentCacheMisses(t *testing.T) {
	veDiscoverableCache.Lock()
	veDiscoverableCache.Entries = map[string]veDiscoverableCacheEntry{}
	veDiscoverableCache.Unlock()
	veDiscoverableSingleflight = singleflight.Group{}
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 1)
	authn := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-slow-a": {TenantID: "tenant-a", UserID: "user-slow-a", MachineID: "machine-slow-a"},
			"machine-slow-b": {TenantID: "tenant-a", UserID: "user-slow-b", MachineID: "machine-slow-b"},
		},
	}
	if rr := doVEMachineJSON(t, VERegisterHandler(settings, authn), http.MethodPost, "/api/ve/register", map[string]any{"name": "Slow Researcher"}, "machine-slow-a", "machine-token"); rr.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", rr.Code, rr.Body.String())
	}
	approveReq := httptest.NewRequest(http.MethodPost, "/api/ve/ve_machine-slow-a/approve", nil)
	approveReq = approveReq.WithContext(tenantAdminContext(approveReq.Context(), "tenant-a"))
	approveReq.SetPathValue("id", "ve_machine-slow-a")
	approveRec := httptest.NewRecorder()
	VEAdminActionHandler(settings, "approve").ServeHTTP(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve status=%d body=%s", approveRec.Code, approveRec.Body.String())
	}
	presence := &slowVEMachinePresence{
		infos:   map[string]*device.MachineRuntimeInfo{"machine-slow-a": {MachineID: "machine-slow-a", Online: true}},
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	handler := VEDiscoverableHandler(settings, authn, presence)
	before := globalVEMetrics.snapshot().Discoverable
	const requests = 6
	results := make(chan *httptest.ResponseRecorder, requests)
	var wg sync.WaitGroup
	wg.Add(requests)
	go func() {
		defer wg.Done()
		results <- doVEMachineJSON(t, handler, http.MethodGet, "/api/ve/discoverable", nil, "machine-slow-b", "machine-token")
	}()
	<-presence.started
	for i := 1; i < requests; i++ {
		go func() {
			defer wg.Done()
			results <- doVEMachineJSON(t, handler, http.MethodGet, "/api/ve/discoverable", nil, "machine-slow-b", "machine-token")
		}()
	}
	time.Sleep(25 * time.Millisecond)
	close(presence.release)
	wg.Wait()
	close(results)
	for rr := range results {
		if rr.Code != http.StatusOK {
			t.Fatalf("discover status=%d body=%s", rr.Code, rr.Body.String())
		}
		if !bytes.Contains(rr.Body.Bytes(), []byte(`"id":"ve_machine-slow-a"`)) {
			t.Fatalf("discover response missing employee: %s", rr.Body.String())
		}
	}
	if got := presence.Count(); got != 1 {
		t.Fatalf("presence calls=%d, want 1 coalesced build", got)
	}
	after := globalVEMetrics.snapshot().Discoverable
	if got := veMetricUint(after, "coalesced_total") - veMetricUint(before, "coalesced_total"); got == 0 {
		t.Fatalf("coalesced_total did not increase; metrics=%#v", after)
	}
}

func TestVEDiscoverableServesStaleCacheWhenBuildsOverloaded(t *testing.T) {
	veDiscoverableCache.Lock()
	veDiscoverableCache.Entries = map[string]veDiscoverableCacheEntry{}
	veDiscoverableCache.Unlock()
	veDiscoverableSingleflight = singleflight.Group{}
	veDiscoverableBuildSemaphore = make(chan struct{}, veDiscoverableBuildConcurrency)
	for i := 0; i < veDiscoverableBuildConcurrency; i++ {
		veDiscoverableBuildSemaphore <- struct{}{}
	}
	defer func() { veDiscoverableBuildSemaphore = make(chan struct{}, veDiscoverableBuildConcurrency) }()

	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 1)
	authn := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-stale-b": {TenantID: "tenant-a", UserID: "user-stale-b", MachineID: "machine-stale-b"},
		},
	}
	payload := []byte(`{"employees":[{"id":"ve_stale","machine_id":"machine-stale-a","owner_user_id":"user-stale-a","name":"Stale Researcher","skill_description":"","access_policy":"","status":"active","online_status":"online"}],"max_group_participants":3}`)
	etag := veResponseETag(payload)
	cacheKey := strings.ToLower("tenant-a|user-stale-b|machine-stale-b")
	veDiscoverableCache.Lock()
	veDiscoverableCache.Entries[cacheKey] = veDiscoverableCacheEntry{Data: payload, ETag: etag, Employees: 1, ExpiresAt: time.Now().Add(-time.Second)}
	veDiscoverableCache.Unlock()
	before := globalVEMetrics.snapshot().Discoverable

	rr := doVEMachineJSON(t, VEDiscoverableHandler(settings, authn), http.MethodGet, "/api/ve/discoverable", nil, "machine-stale-b", "machine-token")
	if rr.Code != http.StatusOK {
		t.Fatalf("discover status=%d body=%s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("X-VE-Cache") != "stale" {
		t.Fatalf("expected stale cache header, got %q", rr.Header().Get("X-VE-Cache"))
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte(`"id":"ve_stale"`)) {
		t.Fatalf("stale response missing employee: %s", rr.Body.String())
	}
	after := globalVEMetrics.snapshot().Discoverable
	if got := veMetricUint(after, "overloaded_total") - veMetricUint(before, "overloaded_total"); got != 0 {
		t.Fatalf("overloaded_total delta=%d, stale response should not count as failed overload; metrics=%#v", got, after)
	}
	if got := veMetricUint(after, "stale_served_total") - veMetricUint(before, "stale_served_total"); got != 1 {
		t.Fatalf("stale_served delta=%d, want 1; metrics=%#v", got, after)
	}
}

func TestVEDiscoverableReturnsTooManyRequestsWhenOverloadedWithoutCache(t *testing.T) {
	veDiscoverableCache.Lock()
	veDiscoverableCache.Entries = map[string]veDiscoverableCacheEntry{}
	veDiscoverableCache.Unlock()
	veDiscoverableSingleflight = singleflight.Group{}
	veDiscoverableBuildSemaphore = make(chan struct{}, veDiscoverableBuildConcurrency)
	for i := 0; i < veDiscoverableBuildConcurrency; i++ {
		veDiscoverableBuildSemaphore <- struct{}{}
	}
	defer func() { veDiscoverableBuildSemaphore = make(chan struct{}, veDiscoverableBuildConcurrency) }()

	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 1)
	authn := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-busy-b": {TenantID: "tenant-a", UserID: "user-busy-b", MachineID: "machine-busy-b"},
		},
	}
	before := globalVEMetrics.snapshot().Discoverable
	rr := doVEMachineJSON(t, VEDiscoverableHandler(settings, authn), http.MethodGet, "/api/ve/discoverable", nil, "machine-busy-b", "machine-token")
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("discover status=%d body=%s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("Retry-After") != "1" {
		t.Fatalf("Retry-After=%q, want 1", rr.Header().Get("Retry-After"))
	}
	after := globalVEMetrics.snapshot().Discoverable
	if got := veMetricUint(after, "overloaded_total") - veMetricUint(before, "overloaded_total"); got != 1 {
		t.Fatalf("overloaded delta=%d, want 1; metrics=%#v", got, after)
	}
}

func TestVEMetricsTracksInitiateAndAuthResponseOutcomes(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 1)
	authn := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-a": {TenantID: "tenant-a", UserID: "user-a", MachineID: "machine-a"},
			"machine-b": {TenantID: "tenant-a", UserID: "user-b", MachineID: "machine-b"},
		},
	}
	if rr := doVEMachineJSON(t, VERegisterHandler(settings, authn), http.MethodPost, "/api/ve/register", map[string]any{"name": "Legal Researcher"}, "machine-a", "machine-token"); rr.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", rr.Code, rr.Body.String())
	}
	approveReq := httptest.NewRequest(http.MethodPost, "/api/ve/ve_machine-a/approve", nil)
	approveReq = approveReq.WithContext(tenantAdminContext(approveReq.Context(), "tenant-a"))
	approveReq.SetPathValue("id", "ve_machine-a")
	approveRec := httptest.NewRecorder()
	VEAdminActionHandler(settings, "approve").ServeHTTP(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve status=%d body=%s", approveRec.Code, approveRec.Body.String())
	}

	before := globalVEMetrics.snapshot()
	initReq := httptest.NewRequest(http.MethodPost, "/api/ve/ve_machine-a/initiate", nil)
	initReq.Header.Set("X-Machine-ID", "machine-b")
	initReq.Header.Set("Authorization", "Bearer machine-token")
	initReq.SetPathValue("id", "ve_machine-a")
	initRec := httptest.NewRecorder()
	VEInitiateHandler(settings, NewGroupDiscussionService(), authn).ServeHTTP(initRec, initReq)
	if initRec.Code != http.StatusCreated {
		t.Fatalf("initiate status=%d body=%s", initRec.Code, initRec.Body.String())
	}

	authRR := doVEMachineJSON(t, VEAuthRespondHandler(settings, authn), http.MethodPost, "/api/ve/auth/respond", map[string]any{"request_id": "missing", "decision": "allow_once"}, "machine-a", "machine-token")
	if authRR.Code != http.StatusNotFound {
		t.Fatalf("auth respond status=%d body=%s", authRR.Code, authRR.Body.String())
	}
	after := globalVEMetrics.snapshot()
	if got := veMetricUint(after.Initiate, "created_session_total") - veMetricUint(before.Initiate, "created_session_total"); got != 1 {
		t.Fatalf("created session delta=%d, want 1; metrics=%#v", got, after.Initiate)
	}
	if got := veMetricUint(after.AuthResponse, "not_found_total") - veMetricUint(before.AuthResponse, "not_found_total"); got != 1 {
		t.Fatalf("auth not_found delta=%d, want 1; metrics=%#v", got, after.AuthResponse)
	}
}

func TestVEControlDeliveryMetricsClassifyAuthResultFailures(t *testing.T) {
	sender := &fakeVEMachineEventSender{err: fmt.Errorf("%w: %w", device.ErrMachineOffline, device.ErrMachineSendBufferFull)}
	req := digitalEmployeeAccessRequest{
		ID:                 "req-control-1",
		RequesterMachineID: "machine-gui",
		TargetMachineID:    "machine-ve",
		TargetVEID:         "ve_machine-ve",
		TargetVEName:       "Control Worker",
	}
	before := globalVEMetrics.snapshot().ControlDelivery

	emitVEAuthResult(sender, req, "allow_once", "allowed")

	after := globalVEMetrics.snapshot().ControlDelivery
	if len(sender.messages) != 2 {
		t.Fatalf("sent messages=%d, want 2", len(sender.messages))
	}
	if got := veMetricUint(after, "failed_total") - veMetricUint(before, "failed_total"); got != 2 {
		t.Fatalf("failed delta=%d, want 2; metrics=%#v", got, after)
	}
	if got := veMetricUint(after, "buffer_full_total") - veMetricUint(before, "buffer_full_total"); got != 2 {
		t.Fatalf("buffer_full delta=%d, want 2; metrics=%#v", got, after)
	}
}

func TestVEControlDeliveryMetricsTrackAdminActionEvents(t *testing.T) {
	sender := &fakeVEMachineEventSender{}
	before := globalVEMetrics.snapshot().ControlDelivery

	emitVEAdminActionEvent(sender, "approve", digitalEmployeeEntry{ID: "ve_machine-a", MachineID: "machine-a", Name: "Control Worker"})

	after := globalVEMetrics.snapshot().ControlDelivery
	if len(sender.messages) != 3 {
		t.Fatalf("sent messages=%d, want 3", len(sender.messages))
	}
	if got := veMetricUint(after, "sent_total") - veMetricUint(before, "sent_total"); got != 3 {
		t.Fatalf("sent delta=%d, want 3; metrics=%#v", got, after)
	}
}

func TestDigitalEmployeeAuthorizationRequiresTenantGrant(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	hubAuth := corelib.NormalizeDigitalEmployeeAuthorization(corelib.DigitalEmployeeAuthorization{Quota: 5, Enabled: true, ExpiresAt: time.Now().UTC().Add(365 * 24 * time.Hour).Format(time.RFC3339)}, time.Now().UTC())
	setVEHubOnlyRegistrationRecord(t, settings, hubAuth)
	authn := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-a":       {TenantID: "tenant-a", UserID: "user-a", MachineID: "machine-a"},
			"machine-b":       {TenantID: "tenant-b", UserID: "user-b", MachineID: "machine-b"},
			"machine-default": {TenantID: store.DefaultTenantID, UserID: "user-default", MachineID: "machine-default"},
		},
	}

	rr := doVEMachineJSON(t, VERegisterHandler(settings, authn), http.MethodPost, "/api/ve/register", map[string]any{"name": "Tenant A Worker"}, "machine-a", "machine-token")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("hub-level authorization should not authorize tenant-a, status=%d body=%s", rr.Code, rr.Body.String())
	}

	tenantAuth := corelib.NormalizeDigitalEmployeeAuthorization(corelib.DigitalEmployeeAuthorization{Quota: 1, Enabled: true, ExpiresAt: time.Now().UTC().Add(365 * 24 * time.Hour).Format(time.RFC3339)}, time.Now().UTC())
	defaultTenantAuth := corelib.NormalizeDigitalEmployeeAuthorization(corelib.DigitalEmployeeAuthorization{Quota: 1, Enabled: true, ExpiresAt: time.Now().UTC().Add(365 * 24 * time.Hour).Format(time.RFC3339)}, time.Now().UTC())
	setVETenantRegistrationRecord(t, settings, map[string]corelib.DigitalEmployeeAuthorization{"tenant-a": tenantAuth, store.DefaultTenantID: defaultTenantAuth})
	if rr := doVEMachineJSON(t, VERegisterHandler(settings, authn), http.MethodPost, "/api/ve/register", map[string]any{"name": "Tenant A Worker"}, "machine-a", "machine-token"); rr.Code != http.StatusOK {
		t.Fatalf("tenant-a grant should authorize tenant-a, status=%d body=%s", rr.Code, rr.Body.String())
	}
	if rr := doVEMachineJSON(t, VERegisterHandler(settings, authn), http.MethodPost, "/api/ve/register", map[string]any{"name": "Default Tenant Worker"}, "machine-default", "machine-token"); rr.Code != http.StatusOK {
		t.Fatalf("default tenant grant should authorize default tenant, status=%d body=%s", rr.Code, rr.Body.String())
	}
	if rr := doVEMachineJSON(t, VERegisterHandler(settings, authn), http.MethodPost, "/api/ve/register", map[string]any{"name": "Tenant B Worker"}, "machine-b", "machine-token"); rr.Code != http.StatusForbidden {
		t.Fatalf("tenant-b without grant should still be blocked, status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestVEAdminListUsesDefaultTenantAuthorization(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	hubAuth := corelib.NormalizeDigitalEmployeeAuthorization(corelib.DigitalEmployeeAuthorization{Quota: 5, Enabled: true, ExpiresAt: time.Now().UTC().Add(365 * 24 * time.Hour).Format(time.RFC3339)}, time.Now().UTC())
	defaultTenantAuth := corelib.NormalizeDigitalEmployeeAuthorization(corelib.DigitalEmployeeAuthorization{Quota: 2, Enabled: true, ExpiresAt: time.Now().UTC().Add(365 * 24 * time.Hour).Format(time.RFC3339)}, time.Now().UTC())
	setVERegistrationPayload(t, settings, map[string]any{
		"registered":                     true,
		"pending_confirmation":           false,
		"disabled":                       false,
		"hub_id":                         "hub-1",
		"hub_secret":                     "secret",
		"digital_employee_authorization": &hubAuth,
		"digital_employee_authorizations": map[string]*corelib.DigitalEmployeeAuthorization{
			store.DefaultTenantID: &defaultTenantAuth,
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/ve/list", nil)
	req = req.WithContext(tenantAdminContext(req.Context(), store.DefaultTenantID))
	rec := httptest.NewRecorder()
	VEAdminListHandler(settings, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Authorization struct {
			Quota int `json:"quota"`
		} `json:"authorization"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode list response: %v body=%s", err, rec.Body.String())
	}
	if payload.Authorization.Quota != 2 {
		t.Fatalf("default tenant admin auth quota=%d, want default tenant quota 2 body=%s", payload.Authorization.Quota, rec.Body.String())
	}
}

func TestDigitalEmployeeAuthorizationQuotaEnforced(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 1)
	authn := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-a": {UserID: "user-a", MachineID: "machine-a"},
			"machine-b": {UserID: "user-b", MachineID: "machine-b"},
		},
	}
	for _, machineID := range []string{"machine-a", "machine-b"} {
		if rr := doVEMachineJSON(t, VERegisterHandler(settings, authn), http.MethodPost, "/api/ve/register", map[string]any{"name": machineID}, machineID, "machine-token"); rr.Code != http.StatusOK {
			t.Fatalf("register %s status=%d body=%s", machineID, rr.Code, rr.Body.String())
		}
	}
	approve := func(id string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/ve/"+id+"/approve", nil)
		req.SetPathValue("id", id)
		rec := httptest.NewRecorder()
		VEAdminActionHandler(settings, "approve").ServeHTTP(rec, req)
		return rec
	}
	if rr := approve("ve_machine-a"); rr.Code != http.StatusOK {
		t.Fatalf("first approve status=%d body=%s", rr.Code, rr.Body.String())
	}
	if rr := approve("ve_machine-b"); rr.Code != http.StatusConflict {
		t.Fatalf("expected quota conflict, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestDigitalEmployeeHistorySearchAndPreview(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 1)
	authn := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-a": {TenantID: "tenant-a", UserID: "user-a", MachineID: "machine-a"},
		},
	}
	owners := fakeVEOwnerLookup{users: map[string]*store.User{
		"user-a": {ID: "user-a", TenantID: "tenant-a", Email: "owner@example.com"},
		"user-h": {ID: "user-h", TenantID: "tenant-a", Email: "human@example.com"},
	}}
	machines := fakeVEMachineLookup{machines: map[string]*store.Machine{
		"human-a":   {ID: "human-a", TenantID: "tenant-a", UserID: "user-h"},
		"machine-a": {ID: "machine-a", TenantID: "tenant-a", UserID: "user-a"},
	}}
	if rr := doVEMachineJSON(t, VERegisterHandler(settings, authn, owners), http.MethodPost, "/api/ve/register", map[string]any{"name": "Legal Researcher"}, "machine-a", "machine-token"); rr.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", rr.Code, rr.Body.String())
	}
	approveReq := httptest.NewRequest(http.MethodPost, "/api/ve/ve_machine-a/approve", nil)
	approveReq = approveReq.WithContext(tenantAdminContext(approveReq.Context(), "tenant-a"))
	approveReq.SetPathValue("id", "ve_machine-a")
	approveRec := httptest.NewRecorder()
	VEAdminActionHandler(settings, "approve").ServeHTTP(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve status=%d body=%s", approveRec.Code, approveRec.Body.String())
	}

	groupSvc := NewGroupDiscussionService()
	session, err := groupSvc.CreateSession("tenant-a", CreateSessionRequest{
		Topic: "Contract review",
		Goal:  "Review the renewal terms",
		Participants: []corea2a.Participant{
			{ID: "human-a", RoleCode: "initiator"},
			{ID: "machine-a", RoleCode: "review", Name: "Legal Researcher"},
		},
		DecisionPolicy: corea2a.PolicyMajority,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := groupSvc.AddDiscussionMessage("tenant-a", session.ID, corea2a.GroupDiscussionMessage{FromID: "machine-a", Kind: corea2a.MessageAnswer, Content: "Prefer a capped renewal.",
		FileAttachments: []corea2a.FileAttachment{{FileURL: "https://hub.local/files/spec-1", Filename: "renewal.pdf", MimeType: "application/pdf", SizeBytes: 2048}},
	}); err != nil {
		t.Fatalf("AddDiscussionMessage: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/ve/ve_machine-a/history?limit=5", nil)
	req = req.WithContext(tenantAdminContext(req.Context(), "tenant-a"))
	req.SetPathValue("id", "ve_machine-a")
	rec := httptest.NewRecorder()
	VEHistoryHandler(settings, groupSvc, owners, machines).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte("Contract review")) || !bytes.Contains(rec.Body.Bytes(), []byte("owner@example.com")) || !bytes.Contains(rec.Body.Bytes(), []byte("human@example.com")) {
		t.Fatalf("history status=%d body=%s", rec.Code, rec.Body.String())
	}

	searchReq := httptest.NewRequest(http.MethodGet, "/api/ve/history/search?q=owner%40example.com&limit=5", nil)
	searchReq = searchReq.WithContext(tenantAdminContext(searchReq.Context(), "tenant-a"))
	searchRec := httptest.NewRecorder()
	VEHistorySearchHandler(settings, groupSvc, owners).ServeHTTP(searchRec, searchReq)
	if searchRec.Code != http.StatusOK || !bytes.Contains(searchRec.Body.Bytes(), []byte("Contract review")) || !bytes.Contains(searchRec.Body.Bytes(), []byte("Legal Researcher")) {
		t.Fatalf("history search status=%d body=%s", searchRec.Code, searchRec.Body.String())
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/api/ve/history/"+session.ID+"/detail", nil)
	detailReq = detailReq.WithContext(tenantAdminContext(detailReq.Context(), "tenant-a"))
	detailReq.SetPathValue("id", session.ID)
	detailRec := httptest.NewRecorder()
	VEHistoryDetailHandler(settings, groupSvc, owners, machines).ServeHTTP(detailRec, detailReq)
	if detailRec.Code != http.StatusOK || !bytes.Contains(detailRec.Body.Bytes(), []byte("Prefer a capped renewal.")) || !bytes.Contains(detailRec.Body.Bytes(), []byte("renewal.pdf")) || !bytes.Contains(detailRec.Body.Bytes(), []byte("human@example.com")) {
		t.Fatalf("detail status=%d body=%s", detailRec.Code, detailRec.Body.String())
	}
}

func TestDigitalEmployeeHistorySearchBlankQueryDoesNotEnumerate(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 1)
	authn := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-a": {UserID: "user-a", MachineID: "machine-a"},
		},
	}
	owners := fakeVEOwnerLookup{users: map[string]*store.User{"user-a": {ID: "user-a", Email: "owner@example.com"}}}
	if rr := doVEMachineJSON(t, VERegisterHandler(settings, authn, owners), http.MethodPost, "/api/ve/register", map[string]any{"name": "Legal Researcher"}, "machine-a", "machine-token"); rr.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", rr.Code, rr.Body.String())
	}
	groupSvc := NewGroupDiscussionService()
	if _, err := groupSvc.CreateSession("tenant-a", CreateSessionRequest{
		Topic: "Private review",
		Goal:  "Review",
		Participants: []corea2a.Participant{
			{ID: "human-a", RoleCode: "initiator"},
			{ID: "machine-a", RoleCode: "review"},
		},
		DecisionPolicy: corea2a.PolicyMajority,
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/ve/history/search?q=%20%20%20&limit=5", nil)
	req = req.WithContext(tenantAdminContext(req.Context(), "tenant-a"))
	rec := httptest.NewRecorder()
	VEHistorySearchHandler(settings, groupSvc, owners).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"matches":[]`)) || !bytes.Contains(rec.Body.Bytes(), []byte(`"discussions":[]`)) {
		t.Fatalf("blank search should not enumerate history, status=%d body=%s", rec.Code, rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("Private review")) {
		t.Fatalf("blank search leaked discussion: %s", rec.Body.String())
	}
}

func TestDigitalEmployeeHistorySearchMergesMatches(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 2)
	authn := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-a": {TenantID: "tenant-a", UserID: "user-a", MachineID: "machine-a"},
			"machine-b": {TenantID: "tenant-a", UserID: "user-b", MachineID: "machine-b"},
		},
	}
	owners := fakeVEOwnerLookup{users: map[string]*store.User{
		"user-a": {ID: "user-a", Email: "alpha@example.com"},
		"user-b": {ID: "user-b", Email: "beta@example.com"},
	}}
	for _, tc := range []struct {
		machineID string
		name      string
	}{
		{machineID: "machine-a", name: "Legal Researcher"},
		{machineID: "machine-b", name: "Contract Analyst"},
	} {
		if rr := doVEMachineJSON(t, VERegisterHandler(settings, authn, owners), http.MethodPost, "/api/ve/register", map[string]any{"name": tc.name}, tc.machineID, "machine-token"); rr.Code != http.StatusOK {
			t.Fatalf("register %s status=%d body=%s", tc.machineID, rr.Code, rr.Body.String())
		}
	}

	groupSvc := NewGroupDiscussionService()
	for _, tc := range []struct {
		machineID string
		topic     string
	}{
		{machineID: "machine-a", topic: "Alpha review"},
		{machineID: "machine-b", topic: "Beta review"},
	} {
		if _, err := groupSvc.CreateSession("tenant-a", CreateSessionRequest{
			Topic: tc.topic,
			Goal:  "Review",
			Participants: []corea2a.Participant{
				{ID: "human-a", RoleCode: "initiator"},
				{ID: tc.machineID, RoleCode: "review"},
			},
			DecisionPolicy: corea2a.PolicyMajority,
		}); err != nil {
			t.Fatalf("CreateSession %s: %v", tc.machineID, err)
		}
	}

	if _, err := groupSvc.CreateSession("tenant-a", CreateSessionRequest{
		Topic: "Joint review",
		Goal:  "Review together",
		Participants: []corea2a.Participant{
			{ID: "human-a", RoleCode: "initiator"},
			{ID: "machine-a", RoleCode: "review"},
			{ID: "machine-b", RoleCode: "review"},
		},
		DecisionPolicy: corea2a.PolicyMajority,
	}); err != nil {
		t.Fatalf("CreateSession joint: %v", err)
	}

	searchReq := httptest.NewRequest(http.MethodGet, "/api/ve/history/search?q=example.com&limit=5", nil)
	searchReq = searchReq.WithContext(tenantAdminContext(searchReq.Context(), "tenant-a"))
	searchRec := httptest.NewRecorder()
	VEHistorySearchHandler(settings, groupSvc, owners).ServeHTTP(searchRec, searchReq)
	if searchRec.Code != http.StatusOK {
		t.Fatalf("history search status=%d body=%s", searchRec.Code, searchRec.Body.String())
	}
	var out struct {
		Matches []struct {
			Employee    digitalEmployeeEntry
			Discussions []corea2a.HubDiscussionSummary
		}
		Discussions []corea2a.HubDiscussionSummary
	}
	if err := json.Unmarshal(searchRec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode search response: %v body=%s", err, searchRec.Body.String())
	}
	if len(out.Matches) != 2 || len(out.Discussions) != 3 {
		t.Fatalf("expected two matched employees and three unique discussions, got matches=%d discussions=%d body=%s", len(out.Matches), len(out.Discussions), searchRec.Body.String())
	}

	noMatchReq := httptest.NewRequest(http.MethodGet, "/api/ve/history/search?q=missing&limit=5", nil)
	noMatchReq = noMatchReq.WithContext(tenantAdminContext(noMatchReq.Context(), "tenant-a"))
	noMatchRec := httptest.NewRecorder()
	VEHistorySearchHandler(settings, groupSvc, owners).ServeHTTP(noMatchRec, noMatchReq)
	if noMatchRec.Code != http.StatusOK || !bytes.Contains(noMatchRec.Body.Bytes(), []byte(`"matches":[]`)) {
		t.Fatalf("expected empty search result, status=%d body=%s", noMatchRec.Code, noMatchRec.Body.String())
	}
}

func TestDigitalEmployeeHistorySearchCapsFlattenedDiscussions(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 2)
	authn := fakeVEMachineAuth{
		token: "machine-token",
		principals: map[string]*auth.MachinePrincipal{
			"machine-a": {TenantID: "tenant-a", UserID: "user-a", MachineID: "machine-a"},
			"machine-b": {TenantID: "tenant-a", UserID: "user-b", MachineID: "machine-b"},
		},
	}
	owners := fakeVEOwnerLookup{users: map[string]*store.User{
		"user-a": {ID: "user-a", Email: "alpha@example.com"},
		"user-b": {ID: "user-b", Email: "beta@example.com"},
	}}
	for _, tc := range []struct {
		machineID string
		name      string
	}{
		{machineID: "machine-a", name: "Alpha Analyst"},
		{machineID: "machine-b", name: "Beta Analyst"},
	} {
		if rr := doVEMachineJSON(t, VERegisterHandler(settings, authn, owners), http.MethodPost, "/api/ve/register", map[string]any{"name": tc.name}, tc.machineID, "machine-token"); rr.Code != http.StatusOK {
			t.Fatalf("register %s status=%d body=%s", tc.machineID, rr.Code, rr.Body.String())
		}
	}

	groupSvc := NewGroupDiscussionService()
	for i, machineID := range []string{"machine-a", "machine-a", "machine-b", "machine-b"} {
		if _, err := groupSvc.CreateSession("tenant-a", CreateSessionRequest{
			Topic: fmt.Sprintf("Review %d", i+1),
			Goal:  "Review",
			Participants: []corea2a.Participant{
				{ID: "human-a", RoleCode: "initiator"},
				{ID: machineID, RoleCode: "review"},
			},
			DecisionPolicy: corea2a.PolicyMajority,
		}); err != nil {
			t.Fatalf("CreateSession %d: %v", i+1, err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/ve/history/search?q=example.com&limit=2", nil)
	req = req.WithContext(tenantAdminContext(req.Context(), "tenant-a"))
	rec := httptest.NewRecorder()
	VEHistorySearchHandler(settings, groupSvc, owners).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("history search status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Discussions []corea2a.HubDiscussionSummary
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode search response: %v body=%s", err, rec.Body.String())
	}
	if len(out.Discussions) != 2 {
		t.Fatalf("expected flattened discussions to honor limit, got %d body=%s", len(out.Discussions), rec.Body.String())
	}
}
func TestDigitalEmployeeHistoryAdminRoutesAreWired(t *testing.T) {
	router, _ := newAdminRouterTestServices(t)

	unauthorized := doHubAdminJSONRequest(t, router, http.MethodGet, "/api/ve/history/search?q=owner%40example.com", nil, "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected history search to require admin token, got %d body=%s", unauthorized.Code, unauthorized.Body.String())
	}

	globalToken := issueHubAdminToken(t, router)
	globalSearch := doHubAdminJSONRequest(t, router, http.MethodGet, "/api/ve/history/search?q=owner%40example.com", nil, globalToken)
	if globalSearch.Code != http.StatusForbidden {
		t.Fatalf("expected global admin to be blocked from tenant VE history, got %d body=%s", globalSearch.Code, globalSearch.Body.String())
	}
	token := issueTenantAdminToken(t, router, globalToken, "acme", "ve-admin")
	search := doHubAdminJSONRequest(t, router, http.MethodGet, "/api/ve/history/search?q=owner%40example.com", nil, token)
	if search.Code != http.StatusOK || !bytes.Contains(search.Body.Bytes(), []byte(`"matches":[]`)) {
		t.Fatalf("history search route not wired correctly, status=%d body=%s", search.Code, search.Body.String())
	}

	detail := doHubAdminJSONRequest(t, router, http.MethodGet, "/api/ve/history/missing-discussion/detail", nil, token)
	if detail.Code != http.StatusNotFound || !bytes.Contains(detail.Body.Bytes(), []byte("DISCUSSION_NOT_FOUND")) {
		t.Fatalf("history detail route not wired correctly, status=%d body=%s", detail.Code, detail.Body.String())
	}
}

func TestDigitalEmployeeConfigValidation(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	rr := doPlainJSON(t, VEAdminConfigHandler(settings), http.MethodPut, "/api/ve/config", map[string]any{"max_group_participants": 11})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestDigitalEmployeeConfigSavesAutoApprove(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	rr := doPlainJSON(t, VEAdminConfigHandler(settings), http.MethodPut, "/api/ve/config", map[string]any{"max_group_participants": 6, "auto_approve": true})
	if rr.Code != http.StatusOK || !bytes.Contains(rr.Body.Bytes(), []byte(`"auto_approve":true`)) {
		t.Fatalf("save config status=%d body=%s", rr.Code, rr.Body.String())
	}
	getRR := doPlainJSON(t, VEAdminConfigHandler(settings), http.MethodGet, "/api/ve/config", nil)
	if getRR.Code != http.StatusOK || !bytes.Contains(getRR.Body.Bytes(), []byte(`"auto_approve":true`)) || !bytes.Contains(getRR.Body.Bytes(), []byte(`"max_group_participants":6`)) {
		t.Fatalf("get config status=%d body=%s", getRR.Code, getRR.Body.String())
	}
}

func TestDigitalEmployeeConfigPreservesAutoApproveWhenOmitted(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	if err := settings.Set(context.Background(), veGroupConfigKey, `{"max_group_participants":5,"auto_approve":true}`); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	rr := doPlainJSON(t, VEAdminConfigHandler(settings), http.MethodPut, "/api/ve/config", map[string]any{"max_group_participants": 7})
	if rr.Code != http.StatusOK || !bytes.Contains(rr.Body.Bytes(), []byte(`"auto_approve":true`)) || !bytes.Contains(rr.Body.Bytes(), []byte(`"max_group_participants":7`)) {
		t.Fatalf("save config status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestDigitalEmployeeConfigAutoApprovesPendingWithinQuota(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 2)
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	seed := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{
		{ID: "ve_active", MachineID: "machine-active", Status: veStatusActive, OnlineStatus: veOnlineStatusOnline},
		{ID: "ve_pending_a", MachineID: "machine-pending-a", Status: veStatusPending, OnlineStatus: veOnlineStatusOnline},
		{ID: "ve_pending_b", MachineID: "machine-pending-b", Status: veStatusPending, OnlineStatus: veOnlineStatusOnline},
	}}
	data, err := json.Marshal(seed)
	if err != nil {
		t.Fatalf("marshal registry: %v", err)
	}
	if err := tenantSystem.Set(context.Background(), veRegistryKey, string(data)); err != nil {
		t.Fatalf("seed registry: %v", err)
	}

	sender := &fakeVEMachineEventSender{}
	rr := doPlainJSON(t, VEAdminConfigHandler(tenantSystem, sender), http.MethodPut, "/api/ve/config", map[string]any{"max_group_participants": 5, "auto_approve": true})
	if rr.Code != http.StatusOK {
		t.Fatalf("save config status=%d body=%s", rr.Code, rr.Body.String())
	}
	updated := loadVERegistry(context.Background(), tenantSystem)
	statuses := map[string]string{}
	for _, employee := range updated.Employees {
		statuses[employee.ID] = employee.Status
	}
	if statuses["ve_pending_a"] != veStatusActive || statuses["ve_pending_b"] != veStatusPending {
		t.Fatalf("auto approve should fill remaining quota only, got %#v", statuses)
	}
	if len(sender.messages) != 3 || sender.messages[0].machineID != "machine-pending-a" {
		t.Fatalf("auto approve should emit status events to approved machine, got %#v", sender.messages)
	}
}

func TestDigitalEmployeeConfigAutoApproveSkipsMissingMacLawSrvRuntimeEmployee(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	enableVEDigitalEmployeeAuthorization(t, settings, 2)
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/platform/runtime/report" {
			t.Fatalf("unexpected runtime path: %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "users": []map[string]any{}})
	}))
	defer runtime.Close()
	if err := saveMacLawSrvRuntimeRegistry(context.Background(), settings, macLawSrvRuntimeRegistry{Runtimes: []macLawSrvRuntimeEntry{{RuntimeID: maclawSrvRuntimePlatformID, BaseURL: runtime.URL, TenantIDs: []string{"tenant-a"}}}}); err != nil {
		t.Fatalf("save runtime registry: %v", err)
	}
	tenantSystem := scopedSystemSettingsForTenant("tenant-a", settings)
	seed := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{
		ID:                 "ve_deleted",
		MachineID:          "ve_deleted",
		PlatformID:         maclawSrvRuntimePlatformID,
		PlatformEmployeeID: "deleted-employee",
		RuntimeProviderID:  maclawSrvRuntimePlatformID,
		Status:             veStatusPending,
		OnlineStatus:       veOnlineStatusOnline,
	}}}
	if err := saveVERegistry(context.Background(), tenantSystem, seed); err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	reqBody, _ := json.Marshal(map[string]any{"max_group_participants": 5, "auto_approve": true})
	req := httptest.NewRequest(http.MethodPut, "/api/ve/config", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(tenantAdminContext(req.Context(), "tenant-a"))
	rec := httptest.NewRecorder()
	VEAdminConfigHandler(settings).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("save config status=%d body=%s", rec.Code, rec.Body.String())
	}
	updated := loadVERegistry(context.Background(), tenantSystem)
	if len(updated.Employees) != 1 || updated.Employees[0].Status != veStatusDisabled || updated.Employees[0].OnlineStatus != veOnlineStatusOffline {
		t.Fatalf("missing runtime employee should not be auto-approved: %#v", updated.Employees)
	}
}

func TestDigitalEmployeeFileRelayRoutesRequireAuthenticatedParticipant(t *testing.T) {
	router, _ := newAdminRouterTestServices(t)
	globalToken := issueHubAdminToken(t, router)
	adminToken := issueTenantAdminToken(t, router, globalToken, "acme", "ve-relay-admin")

	enrollRR := doHubAdminJSONRequest(t, router, http.MethodPost, "/api/enroll/start?tenant_id=tenant_acme", map[string]any{
		"email":        "relay-owner@example.com",
		"machine_name": "relay-machine",
		"platform":     "windows",
	}, "")
	if enrollRR.Code != http.StatusOK {
		t.Fatalf("enroll status=%d body=%s", enrollRR.Code, enrollRR.Body.String())
	}
	var enroll struct {
		MachineID    string `json:"machine_id"`
		MachineToken string `json:"machine_token"`
	}
	if err := json.Unmarshal(enrollRR.Body.Bytes(), &enroll); err != nil {
		t.Fatalf("decode enroll: %v", err)
	}
	if enroll.MachineID == "" || enroll.MachineToken == "" {
		t.Fatalf("enroll missing machine credentials: %+v", enroll)
	}

	unauthCreate := doHubAdminJSONRequest(t, router, http.MethodPost, "/api/a2a/consultations", map[string]any{
		"from_id":  enroll.MachineID,
		"topic":    "relay route auth",
		"question": "Can unauthenticated machines create discussions?",
	}, "")
	if unauthCreate.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated discussion create to be 401, got %d body=%s", unauthCreate.Code, unauthCreate.Body.String())
	}

	mismatchCreateBody, _ := json.Marshal(map[string]any{
		"from_id":  "other-machine",
		"topic":    "relay route auth",
		"question": "Can mismatched machines create discussions?",
	})
	mismatchCreateReq := httptest.NewRequest(http.MethodPost, "/api/a2a/consultations", bytes.NewReader(mismatchCreateBody))
	mismatchCreateReq.Header.Set("Content-Type", "application/json")
	mismatchCreateReq.Header.Set("X-Machine-ID", enroll.MachineID)
	mismatchCreateReq.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	mismatchCreate := httptest.NewRecorder()
	router.ServeHTTP(mismatchCreate, mismatchCreateReq)
	if mismatchCreate.Code != http.StatusForbidden {
		t.Fatalf("expected mismatched discussion create to be 403, got %d body=%s", mismatchCreate.Code, mismatchCreate.Body.String())
	}

	createBody, _ := json.Marshal(map[string]any{
		"from_id":  enroll.MachineID,
		"topic":    "relay route auth",
		"question": "Can attachments be shared safely?",
	})
	createReq := httptest.NewRequest(http.MethodPost, "/api/a2a/consultations", bytes.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("X-Machine-ID", enroll.MachineID)
	createReq.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	createRR := httptest.NewRecorder()
	router.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create discussion status=%d body=%s", createRR.Code, createRR.Body.String())
	}
	var created corea2a.ConsultationCreateResponse
	if err := json.Unmarshal(createRR.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode discussion: %v", err)
	}
	if created.Discussion.ID == "" {
		t.Fatalf("missing discussion id: %+v", created)
	}

	buildUpload := func(participantID string) (*http.Request, error) {
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)
		_ = writer.WriteField("session_id", created.Discussion.ID)
		if participantID != "" {
			_ = writer.WriteField("participant_id", participantID)
		}
		part, err := writer.CreateFormFile("file", "note.txt")
		if err != nil {
			return nil, err
		}
		_, _ = part.Write([]byte("route relay body"))
		if err := writer.Close(); err != nil {
			return nil, err
		}
		req := httptest.NewRequest(http.MethodPost, "/api/ve/files/upload", &buf)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		return req, nil
	}

	unauthReq, err := buildUpload(enroll.MachineID)
	if err != nil {
		t.Fatalf("build upload: %v", err)
	}
	unauth := httptest.NewRecorder()
	router.ServeHTTP(unauth, unauthReq)
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated upload to be 401, got %d body=%s", unauth.Code, unauth.Body.String())
	}

	mismatchReq, err := buildUpload("other-machine")
	if err != nil {
		t.Fatalf("build mismatch upload: %v", err)
	}
	mismatchReq.Header.Set("X-Machine-ID", enroll.MachineID)
	mismatchReq.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	mismatch := httptest.NewRecorder()
	router.ServeHTTP(mismatch, mismatchReq)
	if mismatch.Code != http.StatusForbidden {
		t.Fatalf("expected participant mismatch to be 403, got %d body=%s", mismatch.Code, mismatch.Body.String())
	}

	uploadReq, err := buildUpload(enroll.MachineID)
	if err != nil {
		t.Fatalf("build valid upload: %v", err)
	}
	uploadReq.Header.Set("X-Machine-ID", enroll.MachineID)
	uploadReq.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	upload := httptest.NewRecorder()
	router.ServeHTTP(upload, uploadReq)
	if upload.Code != http.StatusOK {
		t.Fatalf("expected valid upload to be 200, got %d body=%s", upload.Code, upload.Body.String())
	}
	var uploadResp struct {
		OK      bool   `json:"ok"`
		FileURL string `json:"file_url"`
	}
	if err := json.Unmarshal(upload.Body.Bytes(), &uploadResp); err != nil {
		t.Fatalf("decode upload: %v", err)
	}
	if !uploadResp.OK || !strings.HasPrefix(uploadResp.FileURL, "/api/ve/files/download/") {
		t.Fatalf("unexpected upload response: %+v", uploadResp)
	}

	downloadReq := httptest.NewRequest(http.MethodGet, uploadResp.FileURL+"?session_id="+created.Discussion.ID+"&participant_id="+enroll.MachineID, nil)
	downloadReq.Header.Set("X-Machine-ID", enroll.MachineID)
	downloadReq.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	download := httptest.NewRecorder()
	router.ServeHTTP(download, downloadReq)
	if download.Code != http.StatusOK || download.Body.String() != "route relay body" {
		t.Fatalf("download status=%d body=%q", download.Code, download.Body.String())
	}

	adminAttachmentPath := "/api/ve/history/" + created.Discussion.ID + "/attachments/" + strings.TrimPrefix(uploadResp.FileURL, "/api/ve/files/download/")
	adminUnauthDownload := httptest.NewRecorder()
	router.ServeHTTP(adminUnauthDownload, httptest.NewRequest(http.MethodGet, adminAttachmentPath, nil))
	if adminUnauthDownload.Code != http.StatusUnauthorized {
		t.Fatalf("expected admin attachment download to require admin auth, got %d body=%s", adminUnauthDownload.Code, adminUnauthDownload.Body.String())
	}

	adminDownloadReq := httptest.NewRequest(http.MethodGet, adminAttachmentPath, nil)
	adminDownloadReq.Header.Set("Authorization", "Bearer "+adminToken)
	adminDownload := httptest.NewRecorder()
	router.ServeHTTP(adminDownload, adminDownloadReq)
	if adminDownload.Code != http.StatusOK || adminDownload.Body.String() != "route relay body" {
		t.Fatalf("admin download status=%d body=%q", adminDownload.Code, adminDownload.Body.String())
	}

	adminWrongSessionReq := httptest.NewRequest(http.MethodGet, "/api/ve/history/other-discussion/attachments/"+strings.TrimPrefix(uploadResp.FileURL, "/api/ve/files/download/"), nil)
	adminWrongSessionReq.Header.Set("Authorization", "Bearer "+adminToken)
	adminWrongSession := httptest.NewRecorder()
	router.ServeHTTP(adminWrongSession, adminWrongSessionReq)
	if adminWrongSession.Code != http.StatusForbidden {
		t.Fatalf("expected wrong-session admin download to be 403, got %d body=%s", adminWrongSession.Code, adminWrongSession.Body.String())
	}

	outsiderRR := doHubAdminJSONRequest(t, router, http.MethodPost, "/api/enroll/start?tenant_id=tenant_acme", map[string]any{
		"email":        "relay-outsider@example.com",
		"machine_name": "outsider-machine",
		"platform":     "windows",
	}, "")
	if outsiderRR.Code != http.StatusOK {
		t.Fatalf("outsider enroll status=%d body=%s", outsiderRR.Code, outsiderRR.Body.String())
	}
	var outsider struct {
		MachineID    string `json:"machine_id"`
		MachineToken string `json:"machine_token"`
	}
	if err := json.Unmarshal(outsiderRR.Body.Bytes(), &outsider); err != nil {
		t.Fatalf("decode outsider enroll: %v", err)
	}
	outsiderDetail := httptest.NewRequest(http.MethodGet, "/api/a2a/consultations/"+created.Discussion.ID+"/detail", nil)
	outsiderDetail.Header.Set("X-Machine-ID", outsider.MachineID)
	outsiderDetail.Header.Set("Authorization", "Bearer "+outsider.MachineToken)
	outsiderDetailRR := httptest.NewRecorder()
	router.ServeHTTP(outsiderDetailRR, outsiderDetail)
	if outsiderDetailRR.Code != http.StatusForbidden {
		t.Fatalf("expected outsider detail to be 403, got %d body=%s", outsiderDetailRR.Code, outsiderDetailRR.Body.String())
	}

	outsiderCancel := httptest.NewRequest(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/cancel", nil)
	outsiderCancel.Header.Set("X-Machine-ID", outsider.MachineID)
	outsiderCancel.Header.Set("Authorization", "Bearer "+outsider.MachineToken)
	outsiderCancelRR := httptest.NewRecorder()
	router.ServeHTTP(outsiderCancelRR, outsiderCancel)
	if outsiderCancelRR.Code != http.StatusForbidden {
		t.Fatalf("expected outsider cancel to be 403, got %d body=%s", outsiderCancelRR.Code, outsiderCancelRR.Body.String())
	}
}

func TestDigitalEmployeeA2AIgnoresSpoofedTenantHeaders(t *testing.T) {
	router, _ := newAdminRouterTestServices(t)
	globalToken := issueHubAdminToken(t, router)
	acmeToken := issueTenantAdminToken(t, router, globalToken, "acme", "ve-a2a-acme-admin")
	otherToken := issueTenantAdminToken(t, router, globalToken, "other", "ve-a2a-other-admin")

	enrollRR := doHubAdminJSONRequest(t, router, http.MethodPost, "/api/enroll/start?tenant_id=tenant_acme", map[string]any{
		"email":        "tenant-spoof-owner@example.com",
		"machine_name": "tenant-spoof-machine",
		"platform":     "windows",
	}, "")
	if enrollRR.Code != http.StatusOK {
		t.Fatalf("enroll status=%d body=%s", enrollRR.Code, enrollRR.Body.String())
	}
	var enroll struct {
		MachineID    string `json:"machine_id"`
		MachineToken string `json:"machine_token"`
	}
	if err := json.Unmarshal(enrollRR.Body.Bytes(), &enroll); err != nil {
		t.Fatalf("decode enroll: %v", err)
	}

	createBody, _ := json.Marshal(map[string]any{
		"from_id":  enroll.MachineID,
		"topic":    "tenant spoof guard",
		"question": "Which tenant owns this discussion?",
	})
	createReq := httptest.NewRequest(http.MethodPost, "/api/a2a/consultations", bytes.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("X-Machine-ID", enroll.MachineID)
	createReq.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	createReq.Header.Set("X-Tenant-ID", "tenant_other")
	createReq.Header.Set("X-Hub-Tenant-ID", "tenant_other")
	createRR := httptest.NewRecorder()
	router.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create discussion status=%d body=%s", createRR.Code, createRR.Body.String())
	}

	acmeList := doHubAdminJSONRequest(t, router, http.MethodGet, "/api/admin/a2a/group-discussions", nil, acmeToken)
	if acmeList.Code != http.StatusOK || !bytes.Contains(acmeList.Body.Bytes(), []byte("tenant spoof guard")) {
		t.Fatalf("tenant_acme list should include machine-owned discussion, status=%d body=%s", acmeList.Code, acmeList.Body.String())
	}
	otherList := doHubAdminJSONRequest(t, router, http.MethodGet, "/api/admin/a2a/group-discussions", nil, otherToken)
	if otherList.Code != http.StatusOK {
		t.Fatalf("tenant_other list status=%d body=%s", otherList.Code, otherList.Body.String())
	}
	if bytes.Contains(otherList.Body.Bytes(), []byte("tenant spoof guard")) {
		t.Fatalf("spoofed tenant saw discussion: %s", otherList.Body.String())
	}
}

func TestDigitalEmployeeMachineAuthRequired(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	authn := fakeVEMachineAuth{token: "machine-token", principals: map[string]*auth.MachinePrincipal{}}
	rr := doVEMachineJSON(t, VEStatusHandler(settings, authn), http.MethodGet, "/api/ve/status", nil, "", "")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func doVEMachineJSON(t *testing.T, handler http.HandlerFunc, method, target string, body any, machineID, token string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, target, &buf)
	req.Header.Set("Content-Type", "application/json")
	if machineID != "" {
		req.Header.Set("X-Machine-ID", machineID)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func doPlainJSON(t *testing.T, handler http.HandlerFunc, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, target, &buf)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func veMetricUint(metrics map[string]any, key string) uint64 {
	switch value := metrics[key].(type) {
	case uint64:
		return value
	case int:
		return uint64(value)
	case float64:
		return uint64(value)
	default:
		return 0
	}
}

func enableVEDigitalEmployeeAuthorization(t *testing.T, settings *testSystemSettingsRepo, quota int) {
	t.Helper()
	authz := corelib.DigitalEmployeeAuthorization{
		Quota:     quota,
		Enabled:   true,
		ExpiresAt: time.Now().UTC().Add(365 * 24 * time.Hour).Format(time.RFC3339),
	}
	authz = corelib.NormalizeDigitalEmployeeAuthorization(authz, time.Now().UTC())
	setVERegistrationRecord(t, settings, authz)
}

func disableVEDigitalEmployeeAuthorization(t *testing.T, settings *testSystemSettingsRepo) {
	t.Helper()
	authz := corelib.NormalizeDigitalEmployeeAuthorization(corelib.DigitalEmployeeAuthorization{Quota: 1, Enabled: false}, time.Now().UTC())
	setVERegistrationRecord(t, settings, authz)
}

func setVERegistrationRecord(t *testing.T, settings *testSystemSettingsRepo, authz corelib.DigitalEmployeeAuthorization) {
	t.Helper()
	payload := map[string]any{
		"registered":                     true,
		"pending_confirmation":           false,
		"disabled":                       false,
		"hub_id":                         "hub-1",
		"hub_secret":                     "secret",
		"digital_employee_authorization": &authz,
		"digital_employee_authorizations": map[string]*corelib.DigitalEmployeeAuthorization{
			"tenant-a": &authz,
		},
	}
	setVERegistrationPayload(t, settings, payload)
}

func setVEHubOnlyRegistrationRecord(t *testing.T, settings *testSystemSettingsRepo, authz corelib.DigitalEmployeeAuthorization) {
	t.Helper()
	payload := map[string]any{
		"registered":                     true,
		"pending_confirmation":           false,
		"disabled":                       false,
		"hub_id":                         "hub-1",
		"hub_secret":                     "secret",
		"digital_employee_authorization": authz,
	}
	setVERegistrationPayload(t, settings, payload)
}

func setVETenantRegistrationRecord(t *testing.T, settings *testSystemSettingsRepo, authzs map[string]corelib.DigitalEmployeeAuthorization) {
	t.Helper()
	tenantAuthzs := make(map[string]corelib.DigitalEmployeeAuthorization, len(authzs))
	for tenantID, authz := range authzs {
		tenantAuthzs[tenantID] = authz
	}
	payload := map[string]any{
		"registered":                      true,
		"pending_confirmation":            false,
		"disabled":                        false,
		"hub_id":                          "hub-1",
		"hub_secret":                      "secret",
		"digital_employee_authorizations": tenantAuthzs,
	}
	setVERegistrationPayload(t, settings, payload)
}

func setVERegistrationPayload(t *testing.T, settings *testSystemSettingsRepo, payload map[string]any) {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal auth payload: %v", err)
	}
	if err := settings.Set(context.Background(), "center_registration", string(data)); err != nil {
		t.Fatalf("set auth payload: %v", err)
	}
}

func TestLoadVERegistryNormalizesLegacyPlatformOnlineStatusOffline(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", Status: veStatusActive, OnlineStatus: "platform"}}}
	data, err := json.Marshal(registry)
	if err != nil {
		t.Fatalf("marshal registry: %v", err)
	}
	if err := settings.Set(context.Background(), veRegistryKey, string(data)); err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	loaded := loadVERegistry(context.Background(), settings)
	if len(loaded.Employees) != 1 || loaded.Employees[0].OnlineStatus != veOnlineStatusOffline {
		t.Fatalf("legacy online status was not normalized to offline: %#v", loaded.Employees)
	}
	raw, err := settings.Get(context.Background(), veRegistryKey)
	if err != nil {
		t.Fatalf("load repaired registry: %v", err)
	}
	if strings.Contains(raw, `"online_status":"platform"`) {
		t.Fatalf("legacy online status was not repaired in storage: %s", raw)
	}
}
