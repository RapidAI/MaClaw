package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/a2a"
)

const testVEAvatarPNGDataURL = "data:image/png;base64,iVBORw0KGgo="

func TestResolveVEInviteMachineIDPrefersMachineID(t *testing.T) {
	employees := []VirtualEmployeeEntry{
		{ID: "ve_machine-a", MachineID: "machine-a", Name: "Legal Researcher"},
		{ID: "ve_machine-b", Name: "Fallback Analyst"},
	}
	if got := resolveVEInviteMachineID(employees, "ve_machine-a"); got != "machine-a" {
		t.Fatalf("resolved id = %q, want machine-a", got)
	}
	if got := resolveVEInviteMachineID(employees, "machine-a"); got != "machine-a" {
		t.Fatalf("resolved machine id = %q, want machine-a", got)
	}
	if got := resolveVEInviteMachineID(employees, "ve-machine-a"); got != "machine-a" {
		t.Fatalf("resolved dash alias id = %q, want machine-a", got)
	}
	if got := resolveVEInviteMachineID(employees, "ve_machine-b"); got != "ve_machine-b" {
		t.Fatalf("fallback id = %q, want ve_machine-b", got)
	}
	if got := resolveVEInviteMachineID(employees, "unknown"); got != "unknown" {
		t.Fatalf("unknown id = %q, want unknown", got)
	}
}

func TestVirtualEmployeeEntryDecodesAccessLists(t *testing.T) {
	var resp VEStatusResponse
	raw := []byte(`{"registered":true,"employee":{"id":"ve-1","name":"Legal Researcher","skill_description":"contracts","access_policy":"whitelist","status":"active","whitelist":["user-a"],"blacklist":["user-b"]}}`)
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal VE status: %v", err)
	}
	if resp.Employee == nil || len(resp.Employee.Whitelist) != 1 || resp.Employee.Whitelist[0] != "user-a" {
		t.Fatalf("whitelist not decoded: %+v", resp.Employee)
	}
	if len(resp.Employee.Blacklist) != 1 || resp.Employee.Blacklist[0] != "user-b" {
		t.Fatalf("blacklist not decoded: %+v", resp.Employee)
	}
}

func TestRegisterVirtualEmployeeRejectsInvalidAvatarBeforeHub(t *testing.T) {
	hubCalls := int32(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hubCalls, 1)
		t.Errorf("unexpected Hub request: %s", r.URL.Path)
		http.NotFound(w, r)
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteMachineID: "machine-1", RemoteMachineToken: "token-1"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	for _, avatarDataURL := range []string{
		"data:image/png;base64,QUJD",
		strings.Replace(testVEAvatarPNGDataURL, "data:image/png", "data:image/jpeg", 1),
		"data:image/png;base64," + strings.Repeat("A", veAvatarDataURLMaxLength),
	} {
		t.Run(avatarDataURL, func(t *testing.T) {
			if err := app.RegisterVirtualEmployee("Name", "Skill", "public", nil, avatarDataURL); err == nil {
				t.Fatal("RegisterVirtualEmployee succeeded with invalid avatar")
			}
		})
	}
	if got := atomic.LoadInt32(&hubCalls); got != 0 {
		t.Fatalf("Hub calls = %d, want 0", got)
	}
}

func TestUpdateVESettingsRejectsInvalidAvatarBeforeHub(t *testing.T) {
	hubCalls := int32(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hubCalls, 1)
		t.Errorf("unexpected Hub request: %s", r.URL.Path)
		http.NotFound(w, r)
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteMachineID: "machine-1", RemoteMachineToken: "token-1"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	if err := app.UpdateVESettings("Name", "Skill", "public", nil, "data:image/png;base64,QUJD"); err == nil {
		t.Fatal("UpdateVESettings succeeded with invalid avatar")
	}
	if got := atomic.LoadInt32(&hubCalls); got != 0 {
		t.Fatalf("Hub calls = %d, want 0", got)
	}
}

func TestRegisterVirtualEmployeeSendsValidAvatarToHub(t *testing.T) {
	hubCalls := int32(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hubCalls, 1)
		if r.Method != http.MethodPost || r.URL.Path != "/api/ve/register" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("X-Machine-ID"); got != "machine-1" {
			t.Fatalf("X-Machine-ID = %q, want machine-1", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if got := body["avatar_data_url"]; got != testVEAvatarPNGDataURL {
			t.Fatalf("avatar_data_url = %#v, want valid avatar", got)
		}
		if got := body["name"]; got != "Name" {
			t.Fatalf("name = %#v, want trimmed Name", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteMachineID: "machine-1", RemoteMachineToken: "token-1"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	if err := app.RegisterVirtualEmployee(" Name ", " Skill ", "public", nil, " "+testVEAvatarPNGDataURL+" "); err != nil {
		t.Fatalf("RegisterVirtualEmployee: %v", err)
	}
	if got := atomic.LoadInt32(&hubCalls); got != 1 {
		t.Fatalf("Hub calls = %d, want 1", got)
	}
}

func TestValidateVEAvatarAllowsDecodedImageUnderOneMiB(t *testing.T) {
	decoded := append([]byte{0xff, 0xd8, 0xff}, bytes.Repeat([]byte{0}, 800*1024)...)
	avatarDataURL := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(decoded)
	if len(avatarDataURL) <= 1024*1024 {
		t.Fatalf("test avatar data URL length = %d, want over 1MiB", len(avatarDataURL))
	}
	if err := validateVEAvatarDataURL(avatarDataURL); err != nil {
		t.Fatalf("validateVEAvatarDataURL rejected decoded-under-limit avatar: %v", err)
	}
}

func TestFilterOwnVirtualEmployeesRemovesLocalMachineCaseInsensitive(t *testing.T) {
	employees := []VirtualEmployeeEntry{
		{ID: "ve-local", MachineID: "Machine-Local", Name: "Local"},
		{ID: "machine-local", Name: "Legacy Local"},
		{ID: "ve_machine-local", Name: "Generated ID Local"},
		{ID: "ve-machine-local", Name: "Generated Dash ID Local"},
		{ID: "ve-remote", MachineID: "machine-remote", Name: "Remote"},
	}
	got := filterOwnVirtualEmployees(employees, "machine-local")
	if len(got) != 1 || got[0].ID != "ve-remote" {
		t.Fatalf("filtered employees = %#v, want only ve-remote", got)
	}
}

func TestVEGroupKeyNormalizesParticipantSet(t *testing.T) {
	left := veGroupKey([]string{" ve-b ", "ve-a", "ve-b", ""})
	right := veGroupKey([]string{"ve-a", "ve-b"})
	if left != right || left != "ve-a|ve-b" {
		t.Fatalf("group key = %q, want %q", left, "ve-a|ve-b")
	}
}

func TestInitiateGroupConversationErrorsOnMissingSessionID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/a2a/consultations":
			_ = json.NewEncoder(w).Encode(map[string]any{"discussion": map[string]any{}})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: false},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	if _, err := app.InitiateGroupConversation([]string{"ve-a", "ve-b"}); err == nil {
		t.Fatal("InitiateGroupConversation succeeded with missing session id")
	}
}

func TestInitiateGroupConversationReturnsInviteFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/a2a/consultations":
			_ = json.NewEncoder(w).Encode(map[string]any{"discussion": map[string]any{"id": "session-1", "status": "open"}})
		case "/api/ve/discoverable":
			_ = json.NewEncoder(w).Encode(map[string]any{"employees": []map[string]string{{"id": "ve-a", "machine_id": "machine-a"}, {"id": "ve-b", "machine_id": "machine-b"}}})
		case "/api/a2a/consultations/session-1/invites":
			http.Error(w, "invite failed", http.StatusBadGateway)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: false},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	if _, err := app.InitiateGroupConversation([]string{"ve-a", "ve-b"}); err == nil {
		t.Fatal("InitiateGroupConversation succeeded after invite failure")
	}
	if _, ok := app.groupSessionCache.Load(veGroupKey([]string{"ve-a", "ve-b"})); ok {
		t.Fatal("failed group session should not be cached")
	}
}

func TestCacheVESessionIgnoresBlankValues(t *testing.T) {
	app := &App{}
	app.cacheVESession(" ", "session-1")
	app.cacheVESession("ve-a", " ")
	if _, ok := app.veSessionCache.Load("ve-a"); ok {
		t.Fatal("blank session id should not be cached")
	}
	app.cacheVESession(" ve-a ", " session-1 ")
	if got, ok := app.veSessionCache.Load("ve-a"); !ok || got != "session-1" {
		t.Fatalf("cached session = %#v ok=%v", got, ok)
	}
}

func TestInitiateVEConversationReusesFreshStickySessionWithoutHubValidation(t *testing.T) {
	hubCalls := int32(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hubCalls, 1)
		t.Errorf("unexpected Hub request: %s", r.URL.Path)
		http.NotFound(w, r)
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	app.cacheVESession("ve-a", "session-1")

	info, err := app.InitiateVEConversation("ve-a")
	if err != nil {
		t.Fatalf("InitiateVEConversation: %v", err)
	}
	if info.SessionID != "session-1" || info.VEID != "ve-a" {
		t.Fatalf("session info = %+v, want cached session", info)
	}
	if got := atomic.LoadInt32(&hubCalls); got != 0 {
		t.Fatalf("Hub calls = %d, want 0", got)
	}
}

func TestInitiateVEConversationValidatesExpiredStickySession(t *testing.T) {
	validationCalls := int32(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/a2a/consultations/session-1":
			atomic.AddInt32(&validationCalls, 1)
			_ = json.NewEncoder(w).Encode(map[string]any{"discussion": map[string]any{"id": "session-1", "status": "open"}})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	app.cacheVESession("ve-a", "session-1")
	app.veSessionActiveCache.Store("session-1", time.Now().Add(-veSessionActiveValidationTTL-time.Second))

	info, err := app.InitiateVEConversation("ve-a")
	if err != nil {
		t.Fatalf("InitiateVEConversation: %v", err)
	}
	if info.SessionID != "session-1" {
		t.Fatalf("session id = %q, want session-1", info.SessionID)
	}
	if got := atomic.LoadInt32(&validationCalls); got != 1 {
		t.Fatalf("validation calls = %d, want 1", got)
	}
}

func TestInitiateVEConversationReturnsPendingConfirmation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ve/ve-a/initiate":
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s, want POST", r.Method)
			}
			if r.Header.Get("X-Machine-ID") != "machine-1" {
				t.Fatalf("X-Machine-ID = %q, want machine-1", r.Header.Get("X-Machine-ID"))
			}
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "pending_confirmation", "request_id": "req-1", "expires_at": "2026-05-30T10:00:00Z"})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: false},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	if _, err := app.InitiateVEConversation("ve-a"); err == nil || !strings.Contains(err.Error(), "pending_confirmation") || !strings.Contains(err.Error(), "request_id=req-1") || !strings.Contains(err.Error(), "expires_at=2026-05-30T10:00:00Z") {
		t.Fatalf("InitiateVEConversation error = %v, want pending_confirmation with request metadata", err)
	}
}

func TestRespondAuthRequestPostsDecision(t *testing.T) {
	var gotPath, gotDecision, gotRequestID, gotMachineID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMachineID = r.Header.Get("X-Machine-ID")
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		gotRequestID = body["request_id"]
		gotDecision = body["decision"]
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-owner",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: false},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	if err := app.RespondAuthRequest(" req-1 ", " allow_long "); err != nil {
		t.Fatalf("RespondAuthRequest: %v", err)
	}
	if gotPath != "/api/ve/auth/respond" || gotRequestID != "req-1" || gotDecision != "allow_long" || gotMachineID != "machine-owner" {
		t.Fatalf("posted path=%q request=%q decision=%q machine=%q", gotPath, gotRequestID, gotDecision, gotMachineID)
	}
}

func TestRespondAuthRequestRejectsInvalidDecision(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.NotFound(w, r)
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-owner",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: false},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	if err := app.RespondAuthRequest("req-1", "approve_forever"); err == nil {
		t.Fatal("RespondAuthRequest accepted invalid decision")
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("server calls = %d, want 0", got)
	}
}

func TestInitiateVEConversationReusesCachedDirectSession(t *testing.T) {
	hubCalls := int32(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hubCalls, 1)
		if r.URL.Path == "/api/ve/discoverable" {
			http.Error(w, "offline", http.StatusServiceUnavailable)
			return
		}
		t.Errorf("unexpected Hub request: %s", r.URL.Path)
		http.NotFound(w, r)
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	store, err := app.openGroupDiscussionHistoryStore()
	if err != nil {
		t.Fatalf("openGroupDiscussionHistoryStore: %v", err)
	}
	defer store.Close()
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	if err := store.CacheSummaries(ctx, []a2a.HubDiscussionSummary{{ID: "session-cached", LocalRelation: "initiated_by_me", Status: "open", Topic: "数字员工会话", ParticipantIDs: []string{"machine-1", "ve-machine-2"}, UpdatedAt: time.Now().UTC()}}, nil); err != nil {
		t.Fatalf("CacheSummaries: %v", err)
	}

	info, err := app.InitiateVEConversation("ve_ve-machine-2")
	if err != nil {
		t.Fatalf("InitiateVEConversation: %v", err)
	}
	if info.SessionID != "session-cached" {
		t.Fatalf("session id = %q, want session-cached", info.SessionID)
	}
	if _, ok := app.veSessionCache.Load("ve-machine-2"); !ok {
		t.Fatalf("participant id was not cached for future reuse")
	}
	if got := atomic.LoadInt32(&hubCalls); got != 0 {
		t.Fatalf("Hub calls = %d, want cached direct lookup only", got)
	}
}

func TestInitiateVEConversationReusesCachedDirectSessionWithDuplicateAliases(t *testing.T) {
	hubCalls := int32(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hubCalls, 1)
		if r.URL.Path == "/api/ve/discoverable" {
			http.Error(w, "offline", http.StatusServiceUnavailable)
			return
		}
		t.Errorf("unexpected Hub request: %s", r.URL.Path)
		http.NotFound(w, r)
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	store, err := app.openGroupDiscussionHistoryStore()
	if err != nil {
		t.Fatalf("openGroupDiscussionHistoryStore: %v", err)
	}
	defer store.Close()
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	if err := store.CacheSummaries(ctx, []a2a.HubDiscussionSummary{{ID: "session-cached-alias", LocalRelation: "initiated_by_me", Status: "open", Topic: "VE session", ParticipantIDs: []string{"machine-1", "machine-2", "ve-machine-2"}, UpdatedAt: time.Now().UTC()}}, nil); err != nil {
		t.Fatalf("CacheSummaries: %v", err)
	}

	info, err := app.InitiateVEConversation("ve-machine-2")
	if err != nil {
		t.Fatalf("InitiateVEConversation: %v", err)
	}
	if info.SessionID != "session-cached-alias" {
		t.Fatalf("session id = %q, want session-cached-alias", info.SessionID)
	}
	if got := atomic.LoadInt32(&hubCalls); got != 0 {
		t.Fatalf("Hub calls = %d, want cached direct lookup only", got)
	}
}

func TestInitiateVEConversationUsesDiscoverableMappingForCachedDirectSession(t *testing.T) {
	hubCalls := int32(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hubCalls, 1)
		if r.URL.Path == "/api/ve/discoverable" {
			_ = json.NewEncoder(w).Encode(map[string]any{"employees": []map[string]any{{"id": "ve-remote", "machine_id": "machine-2", "name": "Remote VE"}}})
			return
		}
		t.Errorf("unexpected Hub request: %s", r.URL.Path)
		http.NotFound(w, r)
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	store, err := app.openGroupDiscussionHistoryStore()
	if err != nil {
		t.Fatalf("openGroupDiscussionHistoryStore: %v", err)
	}
	defer store.Close()
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	if err := store.CacheSummaries(ctx, []a2a.HubDiscussionSummary{{ID: "session-mapped", LocalRelation: "initiated_by_me", Status: "open", Topic: "VE session", ParticipantIDs: []string{"machine-1", "machine-2"}, UpdatedAt: time.Now().UTC()}}, nil); err != nil {
		t.Fatalf("CacheSummaries: %v", err)
	}

	info, err := app.InitiateVEConversation("ve-remote")
	if err != nil {
		t.Fatalf("InitiateVEConversation: %v", err)
	}
	if info.SessionID != "session-mapped" {
		t.Fatalf("session id = %q, want session-mapped", info.SessionID)
	}
	if got := atomic.LoadInt32(&hubCalls); got != 1 {
		t.Fatalf("Hub calls = %d, want one discoverable lookup", got)
	}
}

func TestInitiateVEConversationUsesCachedDirectSessionWithoutHubCredentials(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteMachineID: "machine-1", GroupDiscussion: corelib.GroupDiscussionConfig{Enabled: true}}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	store, err := app.openGroupDiscussionHistoryStore()
	if err != nil {
		t.Fatalf("openGroupDiscussionHistoryStore: %v", err)
	}
	defer store.Close()
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	if err := store.CacheSummaries(ctx, []a2a.HubDiscussionSummary{{ID: "session-local", LocalRelation: "initiated_by_me", Status: "open", Topic: "数字员工会话", ParticipantIDs: []string{"machine-1", "ve-a"}, UpdatedAt: time.Now().UTC()}}, nil); err != nil {
		t.Fatalf("CacheSummaries: %v", err)
	}

	info, err := app.InitiateVEConversation("ve-a")
	if err != nil {
		t.Fatalf("InitiateVEConversation: %v", err)
	}
	if info.SessionID != "session-local" {
		t.Fatalf("session id = %q, want session-local", info.SessionID)
	}
}

func TestIsVEConsultationActiveJSONReadsNestedStatus(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "top-level open", raw: `{"status":"open"}`, want: true},
		{name: "discussion open", raw: `{"discussion":{"status":"open"}}`, want: true},
		{name: "session open", raw: `{"session":{"status":"open"}}`, want: true},
		{name: "closed", raw: `{"discussion":{"status":"closed"},"session":{"status":"open"}}`, want: false},
		{name: "unknown", raw: `{"discussion":{"status":""}}`, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isVEConsultationActiveJSON([]byte(tc.raw)); got != tc.want {
				t.Fatalf("active = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRegisterLocalExecutorInGroupUsesSpeakRoleAndAcceptsInvite(t *testing.T) {
	var mu sync.Mutex
	var errs []string
	var sawInvite, sawAccept bool
	recordErr := func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		errs = append(errs, fmt.Sprintf(format, args...))
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token-1" {
			recordErr("Authorization header = %q", got)
		}
		if got := r.Header.Get("X-Machine-ID"); got != "machine-1" {
			recordErr("X-Machine-ID header = %q", got)
		}

		switch r.URL.Path {
		case "/api/a2a/consultations/session-1/detail":
			w.WriteHeader(http.StatusNotFound)
		case "/api/a2a/consultations/session-1/invites":
			sawInvite = true
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				recordErr("decode invite body: %v", err)
			}
			if body["from_id"] != "machine-1" || body["to_id"] != "machine-1" {
				recordErr("invite endpoints = %#v", body)
			}
			if body["role"] != "speak" {
				recordErr("invite role = %q", body["role"])
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"invite_id":"invite-1"}`))
		case "/api/a2a/invites/invite-1/accept":
			sawAccept = true
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				recordErr("decode accept body: %v", err)
			}
			if body["from_id"] != "machine-1" {
				recordErr("accept from_id = %q", body["from_id"])
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			recordErr("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	app.remoteSessions = NewRemoteSessionManager(app)
	client := NewRemoteHubClient(app, app.remoteSessions)
	app.remoteSessions.SetHubClient(client)

	registration, err := app.RegisterLocalExecutorInGroup("session-1")
	if err != nil {
		t.Fatalf("RegisterLocalExecutorInGroup: %v", err)
	}
	if registration.ParticipantID != "machine-1" || registration.DisplayName != "Local AI" || registration.SessionID != "session-1" {
		t.Fatalf("registration = %#v, want canonical local participant metadata", registration)
	}
	if !sawInvite || !sawAccept {
		t.Fatalf("sawInvite=%v sawAccept=%v", sawInvite, sawAccept)
	}
	if dispatcher := client.groupChatDispatcher(); dispatcher == nil || !dispatcher.IsRegistered("session-1") {
		t.Fatal("local dispatcher was not registered")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(errs) > 0 {
		t.Fatalf("handler assertions failed: %v", errs)
	}
}

func TestRegisterLocalExecutorInGroupSkipsInviteWhenAlreadyParticipant(t *testing.T) {
	var mu sync.Mutex
	var errs []string
	var sawDetail bool
	recordErr := func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		errs = append(errs, fmt.Sprintf(format, args...))
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token-1" {
			recordErr("Authorization header = %q", got)
		}
		if got := r.Header.Get("X-Machine-ID"); got != "machine-1" {
			recordErr("X-Machine-ID header = %q", got)
		}

		switch r.URL.Path {
		case "/api/a2a/consultations/session-1/detail":
			sawDetail = true
			if got := r.URL.Query().Get("participant_id"); got != "machine-1" {
				recordErr("participant_id = %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"session": map[string]any{
					"id":           "session-1",
					"participants": []map[string]string{{"id": "MACHINE-1", "role_code": "initiator"}},
				},
			})
		case "/api/a2a/consultations/session-1/invites":
			recordErr("unexpected invite for existing local participant")
			http.NotFound(w, r)
		default:
			recordErr("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	app.remoteSessions = NewRemoteSessionManager(app)
	client := NewRemoteHubClient(app, app.remoteSessions)
	app.remoteSessions.SetHubClient(client)

	registration, err := app.RegisterLocalExecutorInGroup("session-1")
	if err != nil {
		t.Fatalf("RegisterLocalExecutorInGroup: %v", err)
	}
	if registration.ParticipantID != "machine-1" || registration.DisplayName != "Local AI" || registration.SessionID != "session-1" {
		t.Fatalf("registration = %#v, want canonical local participant metadata", registration)
	}
	if !sawDetail {
		t.Fatal("expected detail preflight")
	}
	if dispatcher := client.groupChatDispatcher(); dispatcher == nil || !dispatcher.IsRegistered("session-1") {
		t.Fatal("local dispatcher was not registered")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(errs) > 0 {
		t.Fatalf("handler assertions failed: %v", errs)
	}
}

func TestGroupChatDispatcherLocalIDFallsBackToRemoteClientID(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteClientID: "client-1"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	dispatcher := NewGroupChatDispatcher(app)
	if got := dispatcher.getLocalMachineID(); got != "client-1" {
		t.Fatalf("local id = %q, want client-1", got)
	}
}

func TestSendVEGroupMessageRemoteMentionTargetsRemoteParticipant(t *testing.T) {
	var discoverableCalls int
	gotToIDs := make(chan []string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ve/discoverable":
			discoverableCalls++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"employees": []map[string]string{{"id": "profile-ve", "machine_id": "machine-ve", "name": "Remote VE"}},
			})
		case "/api/a2a/consultations/session-1/messages":
			var body struct {
				ToIDs []string `json:"to_ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode body: %v", err)
				return
			}
			gotToIDs <- append([]string(nil), body.ToIDs...)
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/api/a2a/consultations/session-1/detail":
			_ = json.NewEncoder(w).Encode(map[string]any{"discussion": map[string]any{"id": "session-1"}})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	if err := app.SendVEGroupMessage("session-1", "hello remote", []string{"profile-ve", "profile-ve", "machine-ve"}); err != nil {
		t.Fatalf("SendVEGroupMessage: %v", err)
	}

	select {
	case got := <-gotToIDs:
		if len(got) != 1 || got[0] != "machine-ve" {
			t.Fatalf("to_ids = %v, want [machine-ve]", got)
		}
		if discoverableCalls != 1 {
			t.Fatalf("discoverable calls = %d, want 1", discoverableCalls)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for remote-targeted Hub send")
	}
}

func TestSendVEGroupMessageWorksWhenGroupDiscussionDisabled(t *testing.T) {
	gotMessage := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/a2a/consultations/session-1/detail":
			_ = json.NewEncoder(w).Encode(map[string]any{"discussion": map[string]any{"id": "session-1"}, "session": map[string]any{"participants": []map[string]string{
				{"id": "machine-1", "role_code": "initiator"},
				{"id": "anna-machine", "role_code": "speak"},
				{"id": "other-machine", "role_code": "speak"},
			}}})
		case "/api/a2a/consultations/session-1/messages":
			if got := r.Header.Get("X-Machine-ID"); got != "machine-1" {
				t.Errorf("X-Machine-ID = %q, want machine-1", got)
			}
			gotMessage <- struct{}{}
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: false},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	if err := app.SendVEGroupMessage("session-1", "hello", nil); err != nil {
		t.Fatalf("SendVEGroupMessage: %v", err)
	}

	select {
	case <-gotMessage:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Hub send")
	}
}

func TestSendVEGroupMessageDefaultsWithDuplicateRemoteAliases(t *testing.T) {
	gotToIDs := make(chan []string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/a2a/consultations/session-1/detail":
			_ = json.NewEncoder(w).Encode(map[string]any{"discussion": map[string]any{"id": "session-1"}, "session": map[string]any{"participants": []map[string]string{
				{"id": "machine-1", "role_code": "initiator"},
				{"id": "anna-machine", "role_code": "speak"},
				{"id": "ve-anna-machine", "role_code": "speak"},
			}}})
		case "/api/a2a/consultations/session-1/messages":
			var body struct {
				ToIDs []string `json:"to_ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode body: %v", err)
				return
			}
			gotToIDs <- append([]string(nil), body.ToIDs...)
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	if err := app.SendVEGroupMessage("session-1", "hello", nil); err != nil {
		t.Fatalf("SendVEGroupMessage: %v", err)
	}

	select {
	case got := <-gotToIDs:
		if len(got) != 1 || got[0] != "anna-machine" {
			t.Fatalf("to_ids = %v, want [anna-machine]", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Hub send")
	}
}

func TestRegisterLocalExecutorInGroupUsesRegisteredDispatcherFastPath(t *testing.T) {
	hubCalls := int32(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hubCalls, 1)
		t.Errorf("unexpected Hub request: %s", r.URL.Path)
		http.NotFound(w, r)
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	app.remoteSessions = NewRemoteSessionManager(app)
	client := NewRemoteHubClient(app, app.remoteSessions)
	app.remoteSessions.SetHubClient(client)
	client.groupChatDispatcher().RegisterSession("session-1")

	registration, err := app.RegisterLocalExecutorInGroup("session-1")
	if err != nil {
		t.Fatalf("RegisterLocalExecutorInGroup: %v", err)
	}
	if registration.ParticipantID != "machine-1" || registration.SessionID != "session-1" {
		t.Fatalf("registration = %#v, want existing local dispatcher metadata", registration)
	}
	if got := atomic.LoadInt32(&hubCalls); got != 0 {
		t.Fatalf("Hub calls = %d, want 0", got)
	}
}

func TestSendVEMessageReturnsBeforeDetailCacheRefresh(t *testing.T) {
	messageSent := make(chan struct{}, 1)
	detailStarted := make(chan struct{}, 1)
	releaseDetail := make(chan struct{})
	releaseDetailOnce := sync.Once{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/a2a/consultations/session-1/messages":
			messageSent <- struct{}{}
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/api/a2a/consultations/session-1/detail":
			detailStarted <- struct{}{}
			<-releaseDetail
			_ = json.NewEncoder(w).Encode(map[string]any{"discussion": map[string]any{"id": "session-1"}})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	defer releaseDetailOnce.Do(func() { close(releaseDetail) })

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	returned := make(chan error, 1)
	go func() { returned <- app.SendVEMessage("session-1", "hello remote") }()

	select {
	case <-messageSent:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for message send")
	}
	select {
	case err := <-returned:
		if err != nil {
			t.Fatalf("SendVEMessage: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("SendVEMessage waited for detail cache refresh")
	}
	select {
	case <-detailStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for async detail refresh")
	}
	releaseDetailOnce.Do(func() { close(releaseDetail) })
	time.Sleep(100 * time.Millisecond)
}

func TestSendVEA2AMessageSkipsDetailRefreshForStreamChunk(t *testing.T) {
	detailCalls := int32(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/a2a/consultations/session-1/messages":
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/api/a2a/consultations/session-1/detail":
			atomic.AddInt32(&detailCalls, 1)
			t.Errorf("unexpected detail refresh for stream chunk")
			http.NotFound(w, r)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	if err := app.sendVEA2AMessage("session-1", a2a.GroupDiscussionMessage{Kind: a2a.MessageStreamChunk, Content: "chunk"}); err != nil {
		t.Fatalf("sendVEA2AMessage: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if got := atomic.LoadInt32(&detailCalls); got != 0 {
		t.Fatalf("detail calls = %d, want 0", got)
	}
}

func TestCacheVEA2ADetailAsyncCoalescesConcurrentRefreshes(t *testing.T) {
	detailCalls := int32(0)
	firstDetailStarted := make(chan struct{}, 1)
	secondDetailStarted := make(chan struct{}, 1)
	releaseFirstDetail := make(chan struct{})
	var releaseOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/a2a/consultations/session-1/detail":
			call := atomic.AddInt32(&detailCalls, 1)
			if call == 1 {
				firstDetailStarted <- struct{}{}
				<-releaseFirstDetail
			} else if call == 2 {
				secondDetailStarted <- struct{}{}
			} else {
				t.Errorf("unexpected detail refresh call %d", call)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"discussion": map[string]any{"id": "session-1"}})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	defer releaseOnce.Do(func() { close(releaseFirstDetail) })

	app := &App{testHomeDir: t.TempDir(), configCacheValid: true, configCache: corelib.AppConfig{RemoteHubURL: server.URL, RemoteMachineID: "machine-1", RemoteMachineToken: "token-1", GroupDiscussion: corelib.GroupDiscussionConfig{Enabled: true}}}
	client, cfg, err := app.veA2AHubClient()
	if err != nil {
		t.Fatalf("veA2AHubClient: %v", err)
	}
	agentID := groupDiscussionAgentID(cfg)
	app.cacheVEA2ADetailAsync(client, "session-1", agentID)
	select {
	case <-firstDetailStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first detail refresh")
	}
	app.cacheVEA2ADetailAsync(client, "session-1", agentID)
	app.cacheVEA2ADetailAsync(client, "session-1", agentID)
	releaseOnce.Do(func() { close(releaseFirstDetail) })
	select {
	case <-secondDetailStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for coalesced follow-up detail refresh")
	}
	time.Sleep(100 * time.Millisecond)
	if got := atomic.LoadInt32(&detailCalls); got != 2 {
		t.Fatalf("detail calls = %d, want 2", got)
	}
}

func TestSendVEGroupMessageLocalMentionTextFallbackDoesNotBroadcast(t *testing.T) {
	gotToIDs := make(chan []string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/a2a/consultations/session-1/detail":
			_ = json.NewEncoder(w).Encode(map[string]any{"discussion": map[string]any{"id": "session-1"}})
		case "/api/a2a/consultations/session-1/messages":
			var body struct {
				ToIDs []string `json:"to_ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode body: %v", err)
				return
			}
			gotToIDs <- append([]string(nil), body.ToIDs...)
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	app.remoteSessions = NewRemoteSessionManager(app)
	client := NewRemoteHubClient(app, app.remoteSessions)
	app.remoteSessions.SetHubClient(client)
	client.groupChatDispatcher().RegisterSession("session-1")

	if err := app.SendVEGroupMessage("session-1", "@本机AI 你是?", nil); err != nil {
		t.Fatalf("SendVEGroupMessage: %v", err)
	}

	select {
	case got := <-gotToIDs:
		if len(got) != 1 || got[0] != "machine-1" {
			t.Fatalf("to_ids = %v, want [machine-1]", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for local-targeted Hub sync")
	}
}

func TestSendVEGroupMessageWithoutMentionTargetsMultiVEGroup(t *testing.T) {
	gotToIDs := make(chan []string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/a2a/consultations/session-1/detail":
			_ = json.NewEncoder(w).Encode(map[string]any{"session": map[string]any{"participants": []map[string]string{
				{"id": "machine-1", "role_code": "initiator"},
				{"id": "anna-machine", "role_code": "speak"},
				{"id": "other-machine", "role_code": "speak"},
			}}})
		case "/api/a2a/consultations/session-1/messages":
			var body struct {
				ToIDs []string `json:"to_ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode body: %v", err)
				return
			}
			gotToIDs <- append([]string(nil), body.ToIDs...)
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	app.remoteSessions = NewRemoteSessionManager(app)
	client := NewRemoteHubClient(app, app.remoteSessions)
	app.remoteSessions.SetHubClient(client)
	client.groupChatDispatcher().RegisterSession("session-1")

	if err := app.SendVEGroupMessage("session-1", "北京天气", nil); err != nil {
		t.Fatalf("SendVEGroupMessage: %v", err)
	}

	select {
	case got := <-gotToIDs:
		want := []string{"anna-machine", "other-machine"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("to_ids = %v, want %v", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Hub message")
	}
}

func TestSendVEGroupMessageWithoutMentionTargetsRemoteVEsAndSkipsLocalAI(t *testing.T) {
	gotToIDs := make(chan []string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/a2a/consultations/session-1/detail":
			_ = json.NewEncoder(w).Encode(map[string]any{"session": map[string]any{"participants": []map[string]string{
				{"id": "machine-1", "role_code": "initiator"},
				{"id": "anna-machine", "role_code": "speak"},
				{"id": "xiaoyan-machine", "role_code": "speak"},
				{"id": "local-maclaw", "role_code": "speak"},
			}}})
		case "/api/a2a/consultations/session-1/messages":
			var body struct {
				ToIDs []string `json:"to_ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode body: %v", err)
				return
			}
			gotToIDs <- append([]string(nil), body.ToIDs...)
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	if err := app.SendVEGroupMessage("session-1", "continue", nil); err != nil {
		t.Fatalf("SendVEGroupMessage: %v", err)
	}

	select {
	case got := <-gotToIDs:
		want := []string{"anna-machine", "xiaoyan-machine"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("to_ids = %v, want %v", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Hub message")
	}
}

func TestSendVEGroupMessageWithoutMentionKeepsSingleVEDefaultResponder(t *testing.T) {
	gotToIDs := make(chan []string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/a2a/consultations/session-1/detail":
			_ = json.NewEncoder(w).Encode(map[string]any{"session": map[string]any{"participants": []map[string]string{
				{"id": "machine-1", "role_code": "initiator"},
				{"id": "anna-machine", "role_code": "speak"},
				{"id": "local-maclaw", "role_code": "speak"},
			}}})
		case "/api/a2a/consultations/session-1/messages":
			var body struct {
				ToIDs []string `json:"to_ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode body: %v", err)
				return
			}
			gotToIDs <- append([]string(nil), body.ToIDs...)
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	app.remoteSessions = NewRemoteSessionManager(app)
	client := NewRemoteHubClient(app, app.remoteSessions)
	app.remoteSessions.SetHubClient(client)
	client.groupChatDispatcher().RegisterSession("session-1")

	if err := app.SendVEGroupMessage("session-1", "continue", nil); err != nil {
		t.Fatalf("SendVEGroupMessage: %v", err)
	}

	select {
	case got := <-gotToIDs:
		if len(got) != 1 || got[0] != "anna-machine" {
			t.Fatalf("to_ids = %v, want [anna-machine]", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Hub message")
	}
}

func TestSendVEGroupMessageWithoutMentionIgnoresCachedDefaultInMultiVEGroup(t *testing.T) {
	gotToIDs := make(chan []string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/a2a/consultations/session-1/detail":
			_ = json.NewEncoder(w).Encode(map[string]any{"session": map[string]any{"participants": []map[string]string{
				{"id": "machine-1", "role_code": "initiator"},
				{"id": "other-machine", "role_code": "speak"},
				{"id": "anna-machine", "role_code": "speak"},
			}}})
		case "/api/a2a/consultations/session-1/messages":
			var body struct {
				ToIDs []string `json:"to_ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode body: %v", err)
				return
			}
			gotToIDs <- append([]string(nil), body.ToIDs...)
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	app.cacheVEGroupDefaultResponder("session-1", "ve_anna-machine")

	if err := app.SendVEGroupMessage("session-1", "继续", nil); err != nil {
		t.Fatalf("SendVEGroupMessage: %v", err)
	}

	select {
	case got := <-gotToIDs:
		want := []string{"other-machine", "anna-machine"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("to_ids = %v, want %v", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Hub message")
	}
}

func TestSendVEGroupMessageWithoutMentionFallsBackToCachedDefaultResponder(t *testing.T) {
	gotToIDs := make(chan []string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/a2a/consultations/session-1/detail":
			http.Error(w, "temporary detail failure", http.StatusBadGateway)
		case "/api/a2a/consultations/session-1/messages":
			var body struct {
				ToIDs []string `json:"to_ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode body: %v", err)
				return
			}
			gotToIDs <- append([]string(nil), body.ToIDs...)
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	app.cacheVEGroupDefaultResponder("session-1", "anna-machine")

	if err := app.SendVEGroupMessage("session-1", "继续", nil); err != nil {
		t.Fatalf("SendVEGroupMessage: %v", err)
	}

	select {
	case got := <-gotToIDs:
		if len(got) != 1 || got[0] != "anna-machine" {
			t.Fatalf("to_ids = %v, want [anna-machine]", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Hub message")
	}
}

func TestSendVEGroupMessageWithoutMentionSkipsLegacyLocalAIResponder(t *testing.T) {
	gotToIDs := make(chan []string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/a2a/consultations/session-1/detail":
			_ = json.NewEncoder(w).Encode(map[string]any{"session": map[string]any{"participants": []map[string]string{
				{"id": "machine-1", "role_code": "initiator"},
				{"id": "local-maclaw", "role_code": "speak"},
				{"id": "anna-machine", "role_code": "speak"},
			}}})
		case "/api/a2a/consultations/session-1/messages":
			var body struct {
				ToIDs []string `json:"to_ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode body: %v", err)
				return
			}
			gotToIDs <- append([]string(nil), body.ToIDs...)
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	if err := app.SendVEGroupMessage("session-1", "continue", nil); err != nil {
		t.Fatalf("SendVEGroupMessage: %v", err)
	}

	select {
	case got := <-gotToIDs:
		if len(got) != 1 || got[0] != "anna-machine" {
			t.Fatalf("to_ids = %v, want [anna-machine]", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Hub message")
	}
}

func TestSendVEGroupMessageWithoutMentionSkipsGeneratedLocalResponders(t *testing.T) {
	gotToIDs := make(chan []string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/a2a/consultations/session-1/detail":
			_ = json.NewEncoder(w).Encode(map[string]any{"session": map[string]any{"participants": []map[string]string{
				{"id": "ve_machine-1", "role_code": "speak"},
				{"id": "ve_local-maclaw", "role_code": "speak"},
				{"id": "anna-machine", "role_code": "speak"},
			}}})
		case "/api/a2a/consultations/session-1/messages":
			var body struct {
				ToIDs []string `json:"to_ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode body: %v", err)
				return
			}
			gotToIDs <- append([]string(nil), body.ToIDs...)
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	if err := app.SendVEGroupMessage("session-1", "continue", nil); err != nil {
		t.Fatalf("SendVEGroupMessage: %v", err)
	}

	select {
	case got := <-gotToIDs:
		if len(got) != 1 || got[0] != "anna-machine" {
			t.Fatalf("to_ids = %v, want [anna-machine]", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Hub message")
	}
}

func TestSendVEGroupMessageLocalMentionSyncsInputAsLocalTarget(t *testing.T) {
	gotToIDs := make(chan []string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/a2a/consultations/session-1/messages":
			var body struct {
				ToIDs []string `json:"to_ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode body: %v", err)
				return
			}
			gotToIDs <- append([]string(nil), body.ToIDs...)
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/api/a2a/consultations/session-1/detail":
			_ = json.NewEncoder(w).Encode(map[string]any{"discussion": map[string]any{"id": "session-1"}})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	app.remoteSessions = NewRemoteSessionManager(app)
	client := NewRemoteHubClient(app, app.remoteSessions)
	app.remoteSessions.SetHubClient(client)
	client.groupChatDispatcher().RegisterSession("session-1")

	if err := app.SendVEGroupMessage("session-1", "hello local", []string{"local-maclaw"}); err != nil {
		t.Fatalf("SendVEGroupMessage: %v", err)
	}

	select {
	case got := <-gotToIDs:
		if len(got) != 1 || got[0] != "machine-1" {
			t.Fatalf("to_ids = %v, want [machine-1]", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for local-targeted Hub sync")
	}
}

func TestSendVEGroupMessageMixedMentionsTargetsLocalAndRemote(t *testing.T) {
	gotToIDs := make(chan []string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/a2a/consultations/session-1/messages":
			var body struct {
				ToIDs []string `json:"to_ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode body: %v", err)
				return
			}
			gotToIDs <- append([]string(nil), body.ToIDs...)
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/api/a2a/consultations/session-1/detail":
			_ = json.NewEncoder(w).Encode(map[string]any{"discussion": map[string]any{"id": "session-1"}, "session": map[string]any{"participants": []map[string]string{
				{"id": "machine-1", "role_code": "initiator"},
				{"id": "remote-ve", "role_code": "speak"},
			}}})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	app.remoteSessions = NewRemoteSessionManager(app)
	client := NewRemoteHubClient(app, app.remoteSessions)
	app.remoteSessions.SetHubClient(client)
	client.groupChatDispatcher().RegisterSession("session-1")

	if err := app.SendVEGroupMessage("session-1", "@local-maclaw @remote-ve hello", []string{"local-maclaw", "remote-ve"}); err != nil {
		t.Fatalf("SendVEGroupMessage: %v", err)
	}

	seen := map[string]bool{}
	deadline := time.After(2 * time.Second)
	for len(seen) < 2 {
		select {
		case got := <-gotToIDs:
			if len(got) != 1 {
				t.Fatalf("to_ids = %v, want one target per delivery", got)
			}
			seen[got[0]] = true
		case <-deadline:
			t.Fatalf("timed out waiting for local and remote sends, saw %v", seen)
		}
	}
	if !seen["machine-1"] || !seen["remote-ve"] {
		t.Fatalf("targets = %v, want machine-1 and remote-ve", seen)
	}
}

func TestSyncLocalDispatchInputToHubScopesMessageToLocalParticipant(t *testing.T) {
	var gotToIDs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/a2a/consultations/session-1/detail":
			_ = json.NewEncoder(w).Encode(map[string]any{"discussion": map[string]any{"id": "session-1"}})
		case "/api/a2a/consultations/session-1/messages":
			var body struct {
				FromID string   `json:"from_id"`
				ToIDs  []string `json:"to_ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body.FromID != "machine-1" {
				t.Fatalf("from_id = %q, want machine-1", body.FromID)
			}
			gotToIDs = append([]string(nil), body.ToIDs...)
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	app.syncLocalDispatchInputToHub("session-1", a2a.GroupDiscussionMessage{Content: "hello"})

	if len(gotToIDs) != 1 || gotToIDs[0] != "machine-1" {
		t.Fatalf("to_ids = %v, want [machine-1]", gotToIDs)
	}
}

func TestLocalTargetedGroupMessageStripsLocalOnlyAttachmentPaths(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteMachineID: "machine-1"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	msg, ok := app.localTargetedGroupMessage(a2a.GroupDiscussionMessage{
		Kind:    a2a.MessageStatement,
		Content: "see attachments",
		TextAttachments: []a2a.TextAttachment{{
			Content:   "aGVsbG8=",
			Filename:  "note.txt",
			LocalPath: `C:\tmp\note.txt`,
		}},
		ImageAttachments: []a2a.ImageAttachment{{
			Filename:  "local.png",
			LocalPath: `C:\tmp\local.png`,
		}, {
			FileURL:   "https://hub/files/remote.png",
			Filename:  "remote.png",
			LocalPath: `C:\tmp\remote.png`,
		}},
		FileAttachments: []a2a.FileAttachment{{
			Filename:  "local.pdf",
			LocalPath: `C:\tmp\local.pdf`,
		}},
	})
	if !ok {
		t.Fatal("localTargetedGroupMessage returned false")
	}
	if len(msg.ToIDs) != 1 || msg.ToIDs[0] != "machine-1" {
		t.Fatalf("ToIDs = %v, want [machine-1]", msg.ToIDs)
	}
	if len(msg.TextAttachments) != 1 || msg.TextAttachments[0].LocalPath != "" {
		t.Fatalf("text attachments not sanitized: %+v", msg.TextAttachments)
	}
	if len(msg.ImageAttachments) != 1 || msg.ImageAttachments[0].FileURL != "https://hub/files/remote.png" || msg.ImageAttachments[0].LocalPath != "" {
		t.Fatalf("image attachments not sanitized: %+v", msg.ImageAttachments)
	}
	if len(msg.FileAttachments) != 0 {
		t.Fatalf("local-only file attachments should be omitted from Hub sync: %+v", msg.FileAttachments)
	}
}

func TestResolveVEGroupMentionTargetsUsesCanonicalParticipants(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ve/discoverable":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"employees": []map[string]string{{"id": "profile-ve", "machine_id": "machine-ve", "name": "Remote VE"}},
			})
		case "/api/a2a/consultations/session-1/detail":
			_ = json.NewEncoder(w).Encode(map[string]any{"session": map[string]any{"participants": []map[string]string{
				{"id": "machine-1", "role_code": "initiator"},
				{"id": "MACHINE-VE", "role_code": "speak"},
			}}})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	targets, err := app.resolveVEGroupMentionTargets("session-1", "", []string{" profile-ve ", "machine-ve", "MACHINE-1"})
	if err != nil {
		t.Fatalf("resolveVEGroupMentionTargets: %v", err)
	}
	if !targets.Explicit || !targets.Local {
		t.Fatalf("targets explicit/local = %v/%v, want true/true", targets.Explicit, targets.Local)
	}
	if len(targets.RemoteToIDs) != 1 || targets.RemoteToIDs[0] != "MACHINE-VE" {
		t.Fatalf("remote targets = %v, want [MACHINE-VE]", targets.RemoteToIDs)
	}
}

func TestResolveVEGroupMentionTargetsRawParticipantAvoidsDiscoverableLookup(t *testing.T) {
	discoverCalls := int32(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/a2a/consultations/session-1/detail":
			_ = json.NewEncoder(w).Encode(map[string]any{"session": map[string]any{"participants": []map[string]string{
				{"id": "machine-1", "role_code": "initiator"},
				{"id": "MACHINE-VE", "role_code": "speak"},
			}}})
		case "/api/ve/discoverable":
			atomic.AddInt32(&discoverCalls, 1)
			t.Errorf("unexpected discoverable lookup for raw participant mention")
			http.NotFound(w, r)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	targets, err := app.resolveVEGroupMentionTargets("session-1", "", []string{"machine-ve"})
	if err != nil {
		t.Fatalf("resolveVEGroupMentionTargets: %v", err)
	}
	if len(targets.RemoteToIDs) != 1 || targets.RemoteToIDs[0] != "MACHINE-VE" {
		t.Fatalf("remote targets = %v, want [MACHINE-VE]", targets.RemoteToIDs)
	}
	if got := atomic.LoadInt32(&discoverCalls); got != 0 {
		t.Fatalf("discover calls = %d, want 0", got)
	}
}

func TestResolveVEGroupMentionTargetsGeneratedVEIDAvoidsDiscoverableLookup(t *testing.T) {
	discoverCalls := int32(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/a2a/consultations/session-1/detail":
			_ = json.NewEncoder(w).Encode(map[string]any{"session": map[string]any{"participants": []map[string]string{
				{"id": "machine-1", "role_code": "initiator"},
				{"id": "m_anna", "role_code": "speak"},
			}}})
		case "/api/ve/discoverable":
			atomic.AddInt32(&discoverCalls, 1)
			t.Errorf("unexpected discoverable lookup for generated VE participant mention")
			http.NotFound(w, r)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	targets, err := app.resolveVEGroupMentionTargets("session-1", "", []string{"ve_m_anna"})
	if err != nil {
		t.Fatalf("resolveVEGroupMentionTargets: %v", err)
	}
	if len(targets.RemoteToIDs) != 1 || targets.RemoteToIDs[0] != "m_anna" {
		t.Fatalf("remote targets = %v, want [m_anna]", targets.RemoteToIDs)
	}
	if got := atomic.LoadInt32(&discoverCalls); got != 0 {
		t.Fatalf("discover calls = %d, want 0", got)
	}
}

func TestResolveVEGroupMentionTargetsLocalMentionAvoidsHubDetailLookup(t *testing.T) {
	detailCalls := int32(0)
	discoverCalls := int32(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/a2a/consultations/session-1/detail":
			atomic.AddInt32(&detailCalls, 1)
			t.Errorf("unexpected detail lookup for local-only mention")
			http.NotFound(w, r)
		case "/api/ve/discoverable":
			atomic.AddInt32(&discoverCalls, 1)
			t.Errorf("unexpected discoverable lookup for local-only mention")
			http.NotFound(w, r)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	targets, err := app.resolveVEGroupMentionTargets("session-1", "hello local", []string{"local-maclaw"})
	if err != nil {
		t.Fatalf("resolveVEGroupMentionTargets explicit local: %v", err)
	}
	if !targets.Explicit || !targets.Local || len(targets.RemoteToIDs) != 0 {
		t.Fatalf("explicit local targets = %+v, want explicit local only", targets)
	}

	targets, err = app.resolveVEGroupMentionTargets("session-1", "@本机AI 你是?", nil)
	if err != nil {
		t.Fatalf("resolveVEGroupMentionTargets text local: %v", err)
	}
	if !targets.Explicit || !targets.Local || len(targets.RemoteToIDs) != 0 {
		t.Fatalf("text local targets = %+v, want explicit local only", targets)
	}
	if got := atomic.LoadInt32(&detailCalls); got != 0 {
		t.Fatalf("detail calls = %d, want 0", got)
	}
	if got := atomic.LoadInt32(&discoverCalls); got != 0 {
		t.Fatalf("discover calls = %d, want 0", got)
	}
}

func TestLoadDiscoverableVEEntriesUsesShortLivedCache(t *testing.T) {
	discoverCalls := int32(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ve/discoverable":
			atomic.AddInt32(&discoverCalls, 1)
			_ = json.NewEncoder(w).Encode(map[string]any{"employees": []map[string]any{
				{"id": "ve-a", "machine_id": "machine-a", "name": "Remote A", "whitelist": []string{"team-a"}},
				{"id": "ve_machine-1", "machine_id": "machine-1", "name": "Local"},
			}})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteMachineID: "machine-1"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	first, err := app.loadDiscoverableVEEntries(server.URL, "token-1")
	if err != nil {
		t.Fatalf("first loadDiscoverableVEEntries: %v", err)
	}
	if len(first) != 1 || first[0].MachineID != "machine-a" {
		t.Fatalf("first employees = %+v, want only remote machine-a", first)
	}
	first[0].Name = "mutated"
	first[0].Whitelist[0] = "mutated"

	second, err := app.loadDiscoverableVEEntries(server.URL, "token-1")
	if err != nil {
		t.Fatalf("second loadDiscoverableVEEntries: %v", err)
	}
	if got := atomic.LoadInt32(&discoverCalls); got != 1 {
		t.Fatalf("discoverable calls = %d, want 1", got)
	}
	if len(second) != 1 || second[0].Name != "Remote A" || len(second[0].Whitelist) != 1 || second[0].Whitelist[0] != "team-a" {
		t.Fatalf("cached employees were mutated: %+v", second)
	}
}

func TestIsLocalGroupMentionIDAcceptsCanonicalAndDisplayAliases(t *testing.T) {
	cases := []string{"machine-1", "MACHINE-1", "local-maclaw", "local ai", "local-ai", "localai", "本机AI", "本机 AI", "本機AI", "本機 AI", "本地AI", "本地 AI"}
	for _, id := range cases {
		if !isLocalGroupMentionID(id, "machine-1") {
			t.Fatalf("isLocalGroupMentionID(%q) = false, want true", id)
		}
	}
}

func TestContentMentionsLocalGroupAIRequiresMentionBoundary(t *testing.T) {
	positive := []string{"@\u672c\u673aAI \u4f60\u662f?", "@\u672c\u673a AI\u4f60\u662f?", "\u8bf7@\u672c\u673aAI\u770b\u4e00\u4e0b", "@\u672c\u5730AI \u5e2e\u6211\u5904\u7406", "@machine-1, please"}
	for _, content := range positive {
		if !contentMentionsLocalGroupAI(content, "machine-1") {
			t.Fatalf("contentMentionsLocalGroupAI(%q) = false, want true", content)
		}
	}

	negative := []string{"@\u672c\u673aAI2 \u4f60\u662f?", "@\u672c\u5730AI2 \u4f60\u662f?", "@machine-10 please", "ops@localai.example", "@local-ai_extra"}
	for _, content := range negative {
		if contentMentionsLocalGroupAI(content, "machine-1") {
			t.Fatalf("contentMentionsLocalGroupAI(%q) = true, want false", content)
		}
	}
}

func TestResolveVEGroupMentionTargetsRejectsUnknownParticipant(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ve/discoverable":
			_ = json.NewEncoder(w).Encode(map[string]any{"employees": []map[string]string{}})
		case "/api/a2a/consultations/session-1/detail":
			_ = json.NewEncoder(w).Encode(map[string]any{"session": map[string]any{"participants": []map[string]string{{"id": "machine-1", "role_code": "initiator"}}}})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	if _, err := app.resolveVEGroupMentionTargets("session-1", "", []string{"missing-ve"}); err == nil {
		t.Fatal("expected unknown participant error")
	}
}

func TestRegisterLocalExecutorInGroupRejectsMissingInviteID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/a2a/consultations/session-1/detail":
			w.WriteHeader(http.StatusNotFound)
		case "/api/a2a/consultations/session-1/invites":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{} `))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	app.remoteSessions = NewRemoteSessionManager(app)
	client := NewRemoteHubClient(app, app.remoteSessions)
	app.remoteSessions.SetHubClient(client)

	if _, err := app.RegisterLocalExecutorInGroup("session-1"); err == nil {
		t.Fatal("expected missing invite id error")
	}
	if dispatcher := client.groupChatDispatcher(); dispatcher != nil && dispatcher.IsRegistered("session-1") {
		t.Fatal("dispatcher should not register when Hub invite id is missing")
	}
}

func TestAddVEToGroupWaitsUntilInvitedVEIsParticipant(t *testing.T) {
	detailCalls := int32(0)
	inviteCalls := int32(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ve/discoverable":
			_ = json.NewEncoder(w).Encode(map[string]any{"employees": []map[string]string{{"id": "ve_emp_xiaoyan", "machine_id": "xiaoyan-machine", "name": "程序员鼓励师小妍"}}})
		case "/api/a2a/consultations/session-1/detail":
			call := atomic.AddInt32(&detailCalls, 1)
			participants := []map[string]string{{"id": "machine-1", "role_code": "initiator"}, {"id": "anna-machine", "role_code": "speak"}}
			if call >= 2 {
				participants = append(participants, map[string]string{"id": "xiaoyan-machine", "role_code": "speak"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"session": map[string]any{"id": "session-1", "participants": participants}})
		case "/api/a2a/consultations/session-1/invites":
			atomic.AddInt32(&inviteCalls, 1)
			var body struct {
				FromID  string `json:"from_id"`
				ToID    string `json:"to_id"`
				Role    string `json:"role"`
				Trusted bool   `json:"trusted"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode invite: %v", err)
				return
			}
			if body.FromID != "machine-1" || body.ToID != "xiaoyan-machine" || body.Role != "speak" || !body.Trusted {
				t.Errorf("invite body = %+v, want trusted machine-1 -> xiaoyan-machine speak", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"invite_id": "invite-1"})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	oldTimeout := veGroupInviteJoinTimeout
	oldDelay := veGroupInviteJoinPollDelay
	veGroupInviteJoinTimeout = time.Second
	veGroupInviteJoinPollDelay = time.Millisecond
	defer func() {
		veGroupInviteJoinTimeout = oldTimeout
		veGroupInviteJoinPollDelay = oldDelay
	}()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	if err := app.AddVEToGroup("session-1", "ve_emp_xiaoyan"); err != nil {
		t.Fatalf("AddVEToGroup: %v", err)
	}
	if got := atomic.LoadInt32(&inviteCalls); got != 1 {
		t.Fatalf("invite calls = %d, want 1", got)
	}
	if got, ok := app.veSessionCache.Load("ve_emp_xiaoyan"); !ok || got != "session-1" {
		t.Fatalf("cached VE session = %#v ok=%v, want session-1", got, ok)
	}
}

func TestAddVEToGroupFailsWhenInviteeDoesNotJoin(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ve/discoverable":
			_ = json.NewEncoder(w).Encode(map[string]any{"employees": []map[string]string{{"id": "ve_emp_xiaoyan", "machine_id": "xiaoyan-machine"}}})
		case "/api/a2a/consultations/session-1/detail":
			_ = json.NewEncoder(w).Encode(map[string]any{"session": map[string]any{"id": "session-1", "participants": []map[string]string{{"id": "machine-1", "role_code": "initiator"}, {"id": "anna-machine", "role_code": "speak"}}}})
		case "/api/a2a/consultations/session-1/invites":
			_ = json.NewEncoder(w).Encode(map[string]string{"invite_id": "invite-1"})
		case "/api/a2a/invites/mine":
			if r.URL.Query().Get("from_id") != "machine-1" || r.URL.Query().Get("invite_id") != "invite-1" || r.URL.Query().Get("status") != "all" {
				t.Errorf("invite status query = %s, want from_id machine-1 invite_id invite-1 status all", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"invites": []map[string]string{{"id": "invite-1", "to_id": "xiaoyan-machine", "status": "pending"}}})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	oldTimeout := veGroupInviteJoinTimeout
	oldDelay := veGroupInviteJoinPollDelay
	veGroupInviteJoinTimeout = 20 * time.Millisecond
	veGroupInviteJoinPollDelay = time.Millisecond
	defer func() {
		veGroupInviteJoinTimeout = oldTimeout
		veGroupInviteJoinPollDelay = oldDelay
	}()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	err := app.AddVEToGroup("session-1", "ve_emp_xiaoyan")
	if err == nil || !strings.Contains(err.Error(), "has not joined discussion") {
		t.Fatalf("AddVEToGroup error = %v, want not joined", err)
	}
	if got, ok := app.veSessionCache.Load("ve_emp_xiaoyan"); ok {
		t.Fatalf("cached VE session = %#v, want empty", got)
	}
}

func TestAddVEToGroupFailsWhenInviteRejected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ve/discoverable":
			_ = json.NewEncoder(w).Encode(map[string]any{"employees": []map[string]string{{"id": "ve_emp_xiaoyan", "machine_id": "xiaoyan-machine"}}})
		case "/api/a2a/consultations/session-1/detail":
			_ = json.NewEncoder(w).Encode(map[string]any{"session": map[string]any{"id": "session-1", "participants": []map[string]string{{"id": "machine-1", "role_code": "initiator"}, {"id": "anna-machine", "role_code": "speak"}}}})
		case "/api/a2a/consultations/session-1/invites":
			_ = json.NewEncoder(w).Encode(map[string]string{"invite_id": "invite-1"})
		case "/api/a2a/invites/mine":
			if r.URL.Query().Get("from_id") != "machine-1" || r.URL.Query().Get("invite_id") != "invite-1" || r.URL.Query().Get("status") != "all" {
				t.Errorf("invite status query = %s, want from_id machine-1 invite_id invite-1 status all", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"invites": []map[string]string{{"id": "invite-1", "to_id": "xiaoyan-machine", "status": "reject", "reason": "platform employee xiaoyan-machine is not active"}}})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	oldTimeout := veGroupInviteJoinTimeout
	oldDelay := veGroupInviteJoinPollDelay
	veGroupInviteJoinTimeout = time.Second
	veGroupInviteJoinPollDelay = time.Millisecond
	defer func() {
		veGroupInviteJoinTimeout = oldTimeout
		veGroupInviteJoinPollDelay = oldDelay
	}()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	err := app.AddVEToGroup("session-1", "ve_emp_xiaoyan")
	if err == nil || !strings.Contains(err.Error(), "invitation invite-1 rejected: platform employee xiaoyan-machine is not active") {
		t.Fatalf("AddVEToGroup error = %v, want rejection reason", err)
	}
	if got, ok := app.veSessionCache.Load("ve_emp_xiaoyan"); ok {
		t.Fatalf("cached VE session = %#v, want empty", got)
	}
}

func TestAddVEToGroupRejectsMissingInviteID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ve/discoverable":
			_ = json.NewEncoder(w).Encode(map[string]any{"employees": []map[string]string{{"id": "ve_emp_xiaoyan", "machine_id": "xiaoyan-machine"}}})
		case "/api/a2a/consultations/session-1/detail":
			_ = json.NewEncoder(w).Encode(map[string]any{"session": map[string]any{"id": "session-1", "participants": []map[string]string{{"id": "machine-1", "role_code": "initiator"}, {"id": "anna-machine", "role_code": "speak"}}}})
		case "/api/a2a/consultations/session-1/invites":
			_ = json.NewEncoder(w).Encode(map[string]string{})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: true},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	err := app.AddVEToGroup("session-1", "ve_emp_xiaoyan")
	if err == nil || !strings.Contains(err.Error(), "missing invite id") {
		t.Fatalf("AddVEToGroup error = %v, want missing invite id", err)
	}
	if got, ok := app.veSessionCache.Load("ve_emp_xiaoyan"); ok {
		t.Fatalf("cached VE session = %#v, want empty", got)
	}
}
