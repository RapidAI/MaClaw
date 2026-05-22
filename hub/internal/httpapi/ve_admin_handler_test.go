package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	corea2a "github.com/RapidAI/CodeClaw/corelib/a2a"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
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

type fakeVEMachineEventSender struct {
	messages []sentVEMachineEvent
}

type sentVEMachineEvent struct {
	machineID string
	msg       map[string]any
}

func (f *fakeVEMachineEventSender) SendToMachine(machineID string, msg any) error {
	mapped, _ := msg.(map[string]any)
	f.messages = append(f.messages, sentVEMachineEvent{machineID: machineID, msg: mapped})
	return nil
}

func (f fakeVEOwnerLookup) GetByID(ctx context.Context, id string) (*store.User, error) {
	_ = ctx
	return f.users[id], nil
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
		"access_policy":     "public",
	}, "machine-a", "machine-token")
	if registerRR.Code != http.StatusOK {
		t.Fatalf("register status=%d body=%s", registerRR.Code, registerRR.Body.String())
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
	if discoverRR.Code != http.StatusOK || !bytes.Contains(discoverRR.Body.Bytes(), []byte(`"id":"ve_machine-a"`)) {
		t.Fatalf("discover status=%d body=%s", discoverRR.Code, discoverRR.Body.String())
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
	invited, err := groupSvc.ListDiscussionSummaries("tenant-a", ListSessionsFilter{ParticipantID: "machine-a"})
	if err != nil || len(invited) != 1 || invited[0].LocalRelation != "owned_ve_invited" || !invited[0].Readonly {
		t.Fatalf("target summaries err=%v items=%+v", err, invited)
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
	registry := digitalEmployeeRegistry{Employees: []digitalEmployeeEntry{{ID: "ve_employee_1", MachineID: "ve_employee_1", PlatformID: "platform-1", PlatformEmployeeID: "platform-employee-1", OwnerUserID: "hub-account-1", Status: veStatusPending, OnlineStatus: "platform"}}}
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
	owners := fakeVEOwnerLookup{users: map[string]*store.User{"user-a": {ID: "user-a", Email: "owner@example.com"}}}
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
	VEHistoryHandler(settings, groupSvc, owners).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte("Contract review")) || !bytes.Contains(rec.Body.Bytes(), []byte("owner@example.com")) {
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
	VEHistoryDetailHandler(groupSvc).ServeHTTP(detailRec, detailReq)
	if detailRec.Code != http.StatusOK || !bytes.Contains(detailRec.Body.Bytes(), []byte("Prefer a capped renewal.")) || !bytes.Contains(detailRec.Body.Bytes(), []byte("renewal.pdf")) {
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
		"digital_employee_authorization": authz,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal auth payload: %v", err)
	}
	if err := settings.Set(context.Background(), "center_registration", string(data)); err != nil {
		t.Fatalf("set auth payload: %v", err)
	}
}
