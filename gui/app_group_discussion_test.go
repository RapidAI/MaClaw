package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/a2a"
)

func TestGroupDiscussionOwnerIDIsStableAndNamespaced(t *testing.T) {
	if got := groupDiscussionOwnerID(" discussion-1 "); got != "group-discussion:discussion-1" {
		t.Fatalf("unexpected owner id: %q", got)
	}
	if got := groupDiscussionOwnerID(" "); !strings.HasPrefix(got, "group-discussion:") {
		t.Fatalf("unexpected empty owner fallback: %q", got)
	}
}

func TestGroupDiscussionListMineFallsBackToLocalCacheWhenDisabled(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{GroupDiscussion: corelib.GroupDiscussionConfig{Enabled: false}}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	store, err := app.openGroupDiscussionHistoryStore()
	if err != nil {
		t.Fatalf("openGroupDiscussionHistoryStore: %v", err)
	}
	defer store.Close()
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	if err := store.CacheSummaries(ctx, []a2a.HubDiscussionSummary{{ID: "cached-1", LocalRelation: "initiated_by_me", Status: "open", Topic: "Cached VE", ParticipantIDs: []string{"local", "ve-a"}, UpdatedAt: time.Now().UTC()}}, nil); err != nil {
		t.Fatalf("CacheSummaries: %v", err)
	}

	got, err := app.GroupDiscussionListMine("all")
	if err != nil {
		t.Fatalf("GroupDiscussionListMine: %v", err)
	}
	if len(got) != 1 || got[0].ID != "cached-1" {
		t.Fatalf("cached discussions = %+v", got)
	}
}

func TestGroupDiscussionRenameConsultationCachesRenamedSummary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/a2a/consultations/disc-1/rename":
			var body struct {
				FromID string `json:"from_id"`
				Topic  string `json:"topic"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode rename body: %v", err)
			}
			if body.FromID != "machine-1" || body.Topic != "Renamed group" {
				t.Fatalf("rename body = %+v, want machine-1/Renamed group", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"discussion": a2a.HubDiscussionSummary{ID: "disc-1", Topic: "Renamed group", Status: "open", UpdatedAt: time.Now().UTC()}})
		case "/api/a2a/consultations/disc-1/detail":
			_ = json.NewEncoder(w).Encode(a2a.HubDiscussionDetail{Discussion: a2a.HubDiscussionSummary{ID: "disc-1", Topic: "Renamed group", LocalRelation: "initiated_by_me", Status: "open", UpdatedAt: time.Now().UTC()}})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteMachineID: "machine-1", RemoteMachineToken: "token-1", GroupDiscussion: corelib.GroupDiscussionConfig{Enabled: true}}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	store, err := app.openGroupDiscussionHistoryStore()
	if err != nil {
		t.Fatalf("openGroupDiscussionHistoryStore: %v", err)
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	if err := store.CacheSummaries(ctx, []a2a.HubDiscussionSummary{{ID: "disc-1", Topic: "Old group", LocalRelation: "initiated_by_me", Status: "open", UpdatedAt: time.Now().UTC()}}, nil); err != nil {
		t.Fatalf("CacheSummaries: %v", err)
	}
	store.Close()

	if _, err := app.GroupDiscussionRenameConsultation("disc-1", "Renamed group"); err != nil {
		t.Fatalf("GroupDiscussionRenameConsultation: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	var cached []a2a.HubDiscussionSummary
	for {
		store, err = app.openGroupDiscussionHistoryStore()
		if err == nil {
			cached, err = store.CachedSummaries(ctx, false)
			_ = store.Close()
		}
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("CachedSummaries after rename: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(cached) != 1 || cached[0].ID != "disc-1" || cached[0].Topic != "Renamed group" || cached[0].LocalRelation != "initiated_by_me" || cached[0].Readonly {
		t.Fatalf("cached summaries = %+v, want renamed disc-1", cached)
	}
}

func TestGroupDiscussionSendInvitationWaitsForTrustedInviteJoin(t *testing.T) {
	var gotInvite a2a.GroupInvitation
	detailHits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/ve/discoverable":
			_ = json.NewEncoder(w).Encode(map[string]any{"employees": []VirtualEmployeeEntry{{ID: "ve-xiaoyan", MachineID: "machine-xiaoyan", Name: "Xiaoyan"}}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/a2a/consultations/disc-join/invites":
			if err := json.NewDecoder(r.Body).Decode(&gotInvite); err != nil {
				t.Fatalf("decode invite: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"invite_id": "invite-join"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/a2a/consultations/disc-join/detail":
			detailHits++
			if got := r.URL.Query().Get("participant_id"); got != "machine-1" {
				t.Fatalf("participant_id = %q", got)
			}
			_ = json.NewEncoder(w).Encode(a2a.HubDiscussionDetail{Session: &a2a.Session{ID: "disc-join", Participants: []a2a.Participant{{ID: "machine-1", RoleCode: "initiator"}, {ID: "machine-xiaoyan", RoleCode: "speak"}}}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/a2a/invites/mine":
			// Wait loop polls invite status in parallel with the detail check;
			// keep it pending so the join is driven by the detail response.
			_ = json.NewEncoder(w).Encode(a2a.InviteListResponse{Invites: []a2a.GroupInviteSummary{{ID: "invite-join", Status: "pending"}}})
		default:
			t.Fatalf("unexpected request %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir(), configCacheValid: true, configCache: corelib.AppConfig{RemoteHubURL: server.URL, RemoteMachineID: "machine-1", RemoteMachineToken: "token-1", GroupDiscussion: corelib.GroupDiscussionConfig{Enabled: true}}}
	inviteID, err := app.GroupDiscussionSendInvitation("disc-join", a2a.GroupInvitation{ToID: "machine-xiaoyan", Role: a2a.GroupRoleSpeak, Trusted: true})
	if err != nil {
		t.Fatalf("GroupDiscussionSendInvitation: %v", err)
	}
	if inviteID != "invite-join" || gotInvite.FromID != "machine-1" || gotInvite.ToID != "machine-xiaoyan" || !gotInvite.Trusted || detailHits == 0 {
		t.Fatalf("inviteID=%q invite=%+v detailHits=%d", inviteID, gotInvite, detailHits)
	}
}

func TestGroupDiscussionSendInvitationReturnsJoinFailureForTrustedInvite(t *testing.T) {
	oldTimeout := veGroupInviteJoinTimeout
	oldDelay := veGroupInviteJoinPollDelay
	veGroupInviteJoinTimeout = 20 * time.Millisecond
	veGroupInviteJoinPollDelay = time.Millisecond
	defer func() {
		veGroupInviteJoinTimeout = oldTimeout
		veGroupInviteJoinPollDelay = oldDelay
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/ve/discoverable":
			_ = json.NewEncoder(w).Encode(map[string]any{"employees": []VirtualEmployeeEntry{{ID: "ve-xiaoyan", MachineID: "machine-xiaoyan", Name: "Xiaoyan"}}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/a2a/consultations/disc-timeout/invites":
			_ = json.NewEncoder(w).Encode(map[string]string{"invite_id": "invite-timeout"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/a2a/consultations/disc-timeout/detail":
			_ = json.NewEncoder(w).Encode(a2a.HubDiscussionDetail{Session: &a2a.Session{ID: "disc-timeout", Participants: []a2a.Participant{{ID: "machine-1", RoleCode: "initiator"}}}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/a2a/invites/mine":
			_ = json.NewEncoder(w).Encode(a2a.InviteListResponse{Invites: []a2a.GroupInviteSummary{{ID: "invite-timeout", Status: "pending"}}})
		default:
			t.Fatalf("unexpected request %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir(), configCacheValid: true, configCache: corelib.AppConfig{RemoteHubURL: server.URL, RemoteMachineID: "machine-1", RemoteMachineToken: "token-1", GroupDiscussion: corelib.GroupDiscussionConfig{Enabled: true}}}
	inviteID, err := app.GroupDiscussionSendInvitation("disc-timeout", a2a.GroupInvitation{ToID: "machine-xiaoyan", Role: a2a.GroupRoleSpeak, Trusted: true})
	if err == nil {
		t.Fatalf("expected join failure")
	}
	if inviteID != "invite-timeout" {
		t.Fatalf("inviteID = %q", inviteID)
	}
}

func TestGroupDiscussionStartAuthorizedConsultationReturnsPartialSuccess(t *testing.T) {
	inviteHits := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/a2a/consultations":
			_ = json.NewEncoder(w).Encode(a2a.ConsultationCreateResponse{
				Discussion: a2a.HubDiscussionSummary{ID: "disc-created", Status: "open"},
				Request:    a2a.GroupConsultationRequest{ID: "request-created"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/a2a/experts":
			_ = json.NewEncoder(w).Encode(a2a.ExpertListResponse{Experts: []a2a.GroupProfile{
				{AgentID: "expert-fail"},
				{AgentID: "expert-ok"},
			}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/a2a/consultations/disc-created/invites":
			var invitation a2a.GroupInvitation
			if err := json.NewDecoder(r.Body).Decode(&invitation); err != nil {
				t.Fatalf("decode invitation: %v", err)
			}
			inviteHits = append(inviteHits, invitation.ToID)
			if invitation.ToID == "expert-fail" {
				http.Error(w, "invite rejected", http.StatusBadGateway)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"invite_id": "invite-ok"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir(), configCacheValid: true, configCache: corelib.AppConfig{
		RemoteHubURL: server.URL, RemoteMachineID: "machine-1", RemoteMachineToken: "token-1",
		GroupDiscussion: corelib.GroupDiscussionConfig{Enabled: true},
	}}
	result, err := app.GroupDiscussionStartAuthorizedConsultation(GroupDiscussionAuthorizedStartRequest{
		Request:    a2a.GroupConsultationRequest{Question: "Investigate the incident"},
		InviteeIDs: []string{"expert-fail", "expert-ok"},
	})
	if err != nil {
		t.Fatalf("GroupDiscussionStartAuthorizedConsultation: %v", err)
	}
	if result.Consultation.Discussion.ID != "disc-created" || !result.Partial {
		t.Fatalf("result = %+v", result)
	}
	if len(result.InviteIDs) != 1 || result.InviteIDs[0] != "invite-ok" {
		t.Fatalf("invite ids = %v", result.InviteIDs)
	}
	if len(result.InviteErrors) != 1 || result.InviteErrors[0].InviteeID != "expert-fail" {
		t.Fatalf("invite errors = %+v", result.InviteErrors)
	}
	if len(inviteHits) != 2 || inviteHits[0] != "expert-fail" || inviteHits[1] != "expert-ok" {
		t.Fatalf("invite hits = %v", inviteHits)
	}
}

func TestGroupDiscussionStartAuthorizedConsultationTreatsMissingInviteIDAsPartial(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/a2a/consultations":
			_ = json.NewEncoder(w).Encode(a2a.ConsultationCreateResponse{
				Discussion: a2a.HubDiscussionSummary{ID: "disc-created", Status: "open"},
				Request:    a2a.GroupConsultationRequest{ID: "request-created"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/a2a/experts":
			_ = json.NewEncoder(w).Encode(a2a.ExpertListResponse{})
		case r.Method == http.MethodPost && r.URL.Path == "/api/a2a/consultations/disc-created/invites":
			_ = json.NewEncoder(w).Encode(map[string]string{})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir(), configCacheValid: true, configCache: corelib.AppConfig{
		RemoteHubURL: server.URL, RemoteMachineID: "machine-1", RemoteMachineToken: "token-1",
		GroupDiscussion: corelib.GroupDiscussionConfig{Enabled: true},
	}}
	result, err := app.GroupDiscussionStartAuthorizedConsultation(GroupDiscussionAuthorizedStartRequest{
		Request:    a2a.GroupConsultationRequest{Question: "Investigate the incident"},
		InviteeIDs: []string{"expert-empty"},
	})
	if err != nil {
		t.Fatalf("GroupDiscussionStartAuthorizedConsultation: %v", err)
	}
	if result.Consultation.Discussion.ID != "disc-created" || !result.Partial {
		t.Fatalf("result = %+v", result)
	}
	if len(result.InviteIDs) != 0 || len(result.InviteErrors) != 1 || result.InviteErrors[0].InviteeID != "expert-empty" {
		t.Fatalf("invite ids=%v errors=%+v", result.InviteIDs, result.InviteErrors)
	}
}

func TestGroupDiscussionGetConsultationDetailFallsBackToLocalCacheWhenDisabled(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{GroupDiscussion: corelib.GroupDiscussionConfig{Enabled: false}}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	store, err := app.openGroupDiscussionHistoryStore()
	if err != nil {
		t.Fatalf("openGroupDiscussionHistoryStore: %v", err)
	}
	defer store.Close()
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	detail := a2a.HubDiscussionDetail{
		Discussion: a2a.HubDiscussionSummary{ID: "cached-detail", LocalRelation: "initiated_by_me", Status: "open", Topic: "Cached detail", UpdatedAt: time.Now().UTC()},
		Messages:   []a2a.Message{{ID: "m1", FromID: "local", Content: "hello"}},
	}
	if err := store.CacheDetail(ctx, detail, nil); err != nil {
		t.Fatalf("CacheDetail: %v", err)
	}

	got, err := app.GroupDiscussionGetConsultationDetail("cached-detail")
	if err != nil {
		t.Fatalf("GroupDiscussionGetConsultationDetail: %v", err)
	}
	if got.Discussion.ID != "cached-detail" || len(got.Messages) != 1 {
		t.Fatalf("cached detail = %+v", got)
	}
}
