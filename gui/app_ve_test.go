package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/a2a"
)

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

func TestVEGroupKeyNormalizesParticipantSet(t *testing.T) {
	left := veGroupKey([]string{" ve-b ", "ve-a", "ve-b", ""})
	right := veGroupKey([]string{"ve-a", "ve-b"})
	if left != right || left != "ve-a|ve-b" {
		t.Fatalf("group key = %q, want %q", left, "ve-a|ve-b")
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

func TestSendVEGroupMessageLocalMentionTextFallbackDoesNotBroadcast(t *testing.T) {
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

func TestIsLocalGroupMentionIDAcceptsCanonicalAndDisplayAliases(t *testing.T) {
	cases := []string{"machine-1", "MACHINE-1", "local-maclaw", "local ai", "local-ai", "localai", "本机AI", "本机 AI", "本機AI", "本機 AI"}
	for _, id := range cases {
		if !isLocalGroupMentionID(id, "machine-1") {
			t.Fatalf("isLocalGroupMentionID(%q) = false, want true", id)
		}
	}
}

func TestContentMentionsLocalGroupAIRequiresMentionBoundary(t *testing.T) {
	positive := []string{"@\u672c\u673aAI \u4f60\u662f?", "@\u672c\u673a AI\u4f60\u662f?", "\u8bf7@\u672c\u673aAI\u770b\u4e00\u4e0b", "@machine-1, please"}
	for _, content := range positive {
		if !contentMentionsLocalGroupAI(content, "machine-1") {
			t.Fatalf("contentMentionsLocalGroupAI(%q) = false, want true", content)
		}
	}

	negative := []string{"@\u672c\u673aAI2 \u4f60\u662f?", "@machine-10 please", "ops@localai.example", "@local-ai_extra"}
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
