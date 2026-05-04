package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	corea2a "github.com/RapidAI/CodeClaw/corelib/a2a"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
)

func groupReq(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "tenant-a")
	return req
}

func TestGroupDiscussionHubLifecycleAndAdminSnapshot(t *testing.T) {
	svc := NewGroupDiscussionService()
	handler := NewGroupDiscussionHandler(svc)
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)
	handler.RegisterAdminRoutes(mux)

	// Hidden profiles must not be discoverable even if they heartbeat to the Hub.
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPut, "/api/a2a/expert-profile", `{"agent_id":"hidden-maclaw","display_name":"Hidden","discoverable":false,"available":true}`))
	if w.Code != http.StatusOK {
		t.Fatalf("hidden profile status = %d, body=%s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPut, "/api/a2a/expert-profile", `{"agent_id":"maclaw-b","display_name":"Security Reviewer","skills":["go","security"],"model_class":"reasoning","languages":["zh","en"],"discoverable":true,"available":true}`))
	if w.Code != http.StatusOK {
		t.Fatalf("profile status = %d, body=%s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodGet, "/api/a2a/experts", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("experts status = %d, body=%s", w.Code, w.Body.String())
	}
	var experts struct {
		Experts []corea2a.GroupProfile `json:"experts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &experts); err != nil {
		t.Fatalf("unmarshal experts: %v", err)
	}
	if len(experts.Experts) != 1 || experts.Experts[0].AgentID != "maclaw-b" {
		t.Fatalf("expected only discoverable expert, got %+v", experts.Experts)
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations", `{"from_id":"maclaw-a","topic":"safe rollout","question":"How should we roll this out?","context_summary":"Need current-Hub discussion only."}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("consultation status = %d, body=%s", w.Code, w.Body.String())
	}
	var created corea2a.ConsultationCreateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal consultation: %v", err)
	}
	if created.Discussion.ID == "" || created.Discussion.Question != "How should we roll this out?" {
		t.Fatalf("created = %+v", created)
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/invites", `{"from_id":"maclaw-a","to_id":"maclaw-b","role":"review","trusted":true,"security_group_id":"team-a"}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("invite status = %d, body=%s", w.Code, w.Body.String())
	}
	var inviteResp struct {
		InviteID string `json:"invite_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &inviteResp); err != nil {
		t.Fatalf("unmarshal invite: %v", err)
	}
	if inviteResp.InviteID == "" {
		t.Fatal("invite id is empty")
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodGet, "/api/a2a/invites/mine?to_id=maclaw-b", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("list invites status = %d, body=%s", w.Code, w.Body.String())
	}
	var invites struct {
		Invites []corea2a.GroupInviteSummary `json:"invites"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &invites); err != nil {
		t.Fatalf("unmarshal invites: %v", err)
	}
	if len(invites.Invites) != 1 || invites.Invites[0].ToID != "maclaw-b" || invites.Invites[0].Role != corea2a.GroupRoleReview {
		t.Fatalf("invites = %+v", invites.Invites)
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/invites/"+inviteResp.InviteID+"/accept", `{"from_id":"maclaw-b","reason":"available"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("accept status = %d, body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodGet, "/api/a2a/discussions/mine?participant_id=maclaw-b&role=review", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("mine status = %d, body=%s", w.Code, w.Body.String())
	}
	var mine struct {
		Discussions []corea2a.HubDiscussionSummary `json:"discussions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &mine); err != nil {
		t.Fatalf("unmarshal mine: %v", err)
	}
	if len(mine.Discussions) != 1 || mine.Discussions[0].Role != "review" {
		t.Fatalf("mine = %+v", mine.Discussions)
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodGet, "/api/a2a/discussions/mine?participant_id=maclaw-c", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("other mine status = %d, body=%s", w.Code, w.Body.String())
	}
	mine = struct {
		Discussions []corea2a.HubDiscussionSummary `json:"discussions"`
	}{}
	if err := json.Unmarshal(w.Body.Bytes(), &mine); err != nil {
		t.Fatalf("unmarshal other mine: %v", err)
	}
	if len(mine.Discussions) != 0 {
		t.Fatalf("non-participant should not see mine discussions: %+v", mine.Discussions)
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/messages", `{"from_id":"maclaw-b","content":"Use a staged rollout with rollback gates."}`))
	if w.Code != http.StatusOK {
		t.Fatalf("message status = %d, body=%s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodGet, "/api/a2a/consultations/"+created.Discussion.ID+"/detail", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("detail status = %d, body=%s", w.Code, w.Body.String())
	}
	var detail corea2a.HubDiscussionDetail
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatalf("unmarshal detail: %v", err)
	}
	if detail.Discussion.AnswerCount != 1 || !detail.Discussion.ReadyToSummarize || len(detail.Messages) < 4 {
		t.Fatalf("detail readiness/messages = %+v messages=%d", detail.Discussion, len(detail.Messages))
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/result", `{"summary":"Use staged rollout","rationale":"Security reviewer agreed and risk is controlled.","risks":["rollback complexity"]}`))
	if w.Code != http.StatusOK {
		t.Fatalf("result status = %d, body=%s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodGet, "/api/admin/a2a/group-discussions", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("admin snapshot status = %d, body=%s", w.Code, w.Body.String())
	}
	var snapshot AdminGroupDiscussionSnapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	if snapshot.ActiveExperts != 1 || snapshot.TotalExperts != 1 || len(snapshot.Experts) != 1 {
		t.Fatalf("snapshot experts = %+v active=%d total=%d", snapshot.Experts, snapshot.ActiveExperts, snapshot.TotalExperts)
	}
	if len(snapshot.Discussions) != 1 || snapshot.Discussions[0].ResultSummary != "Use staged rollout" || snapshot.Discussions[0].Status != "decided" {
		t.Fatalf("snapshot discussions = %+v", snapshot.Discussions)
	}
}

func TestGroupDiscussionActiveExpertWindow(t *testing.T) {
	svc := NewGroupDiscussionService()
	old := time.Now().Add(-20 * time.Minute)
	_, err := svc.UpsertExpertProfile("tenant-a", corea2a.GroupProfile{AgentID: "old", Discoverable: true, Available: true, UpdatedAt: old})
	if err != nil {
		t.Fatalf("upsert old: %v", err)
	}
	_, err = svc.UpsertExpertProfile("tenant-a", corea2a.GroupProfile{AgentID: "fresh", Discoverable: true, Available: true, UpdatedAt: time.Now()})
	if err != nil {
		t.Fatalf("upsert fresh: %v", err)
	}
	snapshot, err := svc.AdminGroupDiscussionSnapshot("tenant-a")
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snapshot.ActiveExperts != 1 || snapshot.TotalExperts != 2 || snapshot.Experts[0].AgentID != "fresh" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestGroupDiscussionServicePersistsAcrossRestart(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "group-discussion.db")
	provider, err := sqlite.NewProvider(sqlite.Config{DSN: dbPath, WAL: true, BusyTimeoutMS: 5000, MaxReadOpenConns: 2, MaxReadIdleConns: 1, MaxWriteOpenConns: 1, MaxWriteIdleConns: 1})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	svc := NewGroupDiscussionService(provider.Write)
	_, err = svc.UpsertExpertProfile("tenant-a", corea2a.GroupProfile{AgentID: "maclaw-b", DisplayName: "Reviewer", Discoverable: true, Available: true, UpdatedAt: time.Now()})
	if err != nil {
		t.Fatalf("upsert profile: %v", err)
	}
	created, err := svc.CreateConsultation("tenant-a", corea2a.GroupConsultationRequest{FromID: "maclaw-a", Topic: "persist me", Question: "Does this survive restart?"})
	if err != nil {
		t.Fatalf("create consultation: %v", err)
	}
	inviteID, err := svc.AddInvitation("tenant-a", created.Discussion.ID, corea2a.GroupInvitation{FromID: "maclaw-a", ToID: "maclaw-b", Role: corea2a.GroupRoleReview})
	if err != nil {
		t.Fatalf("add invite: %v", err)
	}
	if err := svc.RespondInvitation("tenant-a", inviteID, corea2a.GroupInvitationResponse{FromID: "maclaw-b", Decision: corea2a.GroupInvitationAccept}); err != nil {
		t.Fatalf("accept invite: %v", err)
	}
	if _, err := svc.SubmitDiscussionResult("tenant-a", created.Discussion.ID, corea2a.GroupDiscussionResult{Summary: "Persistence works"}); err != nil {
		t.Fatalf("submit result: %v", err)
	}

	restarted := NewGroupDiscussionService(provider.Write)
	experts := restarted.ListExpertProfiles("tenant-a", 0)
	if len(experts) != 1 || experts[0].AgentID != "maclaw-b" {
		t.Fatalf("restored experts = %+v", experts)
	}
	discussion, err := restarted.GetDiscussionSummary("tenant-a", created.Discussion.ID)
	if err != nil {
		t.Fatalf("restored discussion: %v", err)
	}
	if discussion.ResultSummary != "Persistence works" || discussion.Status != "decided" {
		t.Fatalf("restored discussion = %+v", discussion)
	}
	mine, err := restarted.ListDiscussionSummaries("tenant-a", ListSessionsFilter{ParticipantID: "maclaw-b", Role: "review"})
	if err != nil {
		t.Fatalf("restored mine: %v", err)
	}
	if len(mine) != 1 || mine[0].ID != created.Discussion.ID {
		t.Fatalf("restored mine = %+v", mine)
	}
}
