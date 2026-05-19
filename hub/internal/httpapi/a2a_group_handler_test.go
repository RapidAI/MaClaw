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
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
)

func groupReq(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "tenant-a")
	return req
}

func TestRequestGroupDiscussionTenantIDUsesMachineHeaderAndDefaultTenant(t *testing.T) {
	defaultReq := httptest.NewRequest(http.MethodGet, "/api/a2a/experts", nil)
	if got := requestGroupDiscussionTenantID(defaultReq); got != store.DefaultTenantID {
		t.Fatalf("default tenant = %q, want %q", got, store.DefaultTenantID)
	}

	tenantReq := httptest.NewRequest(http.MethodGet, "/api/a2a/experts", nil)
	tenantReq.Header.Set("X-Hub-Tenant-ID", "tenant_acme")
	if got := requestGroupDiscussionTenantID(tenantReq); got != "tenant_acme" {
		t.Fatalf("header tenant = %q, want tenant_acme", got)
	}
}

type captureGroupDiscussionSender struct {
	messages []sentGroupDiscussionMessage
}

type sentGroupDiscussionMessage struct {
	machineID string
	msg       map[string]any
}

func (s *captureGroupDiscussionSender) SendToMachine(machineID string, msg any) error {
	mapped, _ := msg.(map[string]any)
	s.messages = append(s.messages, sentGroupDiscussionMessage{machineID: machineID, msg: mapped})
	return nil
}

func TestGroupDiscussionMessagePushesToOtherParticipants(t *testing.T) {
	svc := NewGroupDiscussionService()
	sender := &captureGroupDiscussionSender{}
	handler := NewGroupDiscussionHandler(svc, sender)
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations", `{"from_id":"maclaw-a","topic":"direct","question":"Please review."}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("consultation status=%d body=%s", w.Code, w.Body.String())
	}
	var created corea2a.ConsultationCreateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode consultation: %v", err)
	}

	if _, err := svc.AddInvitation("tenant-a", created.Discussion.ID, corea2a.GroupInvitation{FromID: "maclaw-a", ToID: "maclaw-b", Role: corea2a.GroupRoleSpeak}); err != nil {
		t.Fatalf("AddInvitation: %v", err)
	}
	invites := svc.ListInvitations("tenant-a", "maclaw-b", "pending")
	if len(invites) != 1 {
		t.Fatalf("pending invites = %+v", invites)
	}
	if err := svc.RespondInvitation("tenant-a", invites[0].ID, corea2a.GroupInvitationResponse{FromID: "maclaw-b", Decision: corea2a.GroupInvitationAccept}); err != nil {
		t.Fatalf("RespondInvitation: %v", err)
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/messages", `{"from_id":"maclaw-a","content":"Please analyze this clause."}`))
	if w.Code != http.StatusOK {
		t.Fatalf("message status=%d body=%s", w.Code, w.Body.String())
	}
	if len(sender.messages) != 1 {
		t.Fatalf("sent messages = %+v", sender.messages)
	}
	sent := sender.messages[0]
	if sent.machineID != "maclaw-b" || sent.msg["type"] != "ve:discussion_message" {
		t.Fatalf("unexpected sent message: %+v", sent)
	}
	payload, ok := sent.msg["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T", sent.msg["payload"])
	}
	if payload["target_role"] != "speak" {
		t.Fatalf("target_role = %#v", payload["target_role"])
	}
	envelope, ok := payload["envelope"].(corea2a.GroupEnvelope)
	if !ok {
		t.Fatalf("envelope type = %T", payload["envelope"])
	}
	if envelope.Message == nil || envelope.Message.Content != "Please analyze this clause." || envelope.SessionID != created.Discussion.ID {
		t.Fatalf("unexpected payload: %+v", envelope)
	}
	if envelope.Message.ID == "" || envelope.Message.SessionID != created.Discussion.ID || envelope.Message.CreatedAt.IsZero() {
		t.Fatalf("push payload should use persisted message metadata: %+v", envelope.Message)
	}
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
	mux.ServeHTTP(w, groupReq(http.MethodGet, "/api/a2a/discussions/mine?participant_id=maclaw-b&role=all", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("mine all status = %d, body=%s", w.Code, w.Body.String())
	}
	mine = struct {
		Discussions []corea2a.HubDiscussionSummary `json:"discussions"`
	}{}
	if err := json.Unmarshal(w.Body.Bytes(), &mine); err != nil {
		t.Fatalf("unmarshal mine all: %v", err)
	}
	if len(mine.Discussions) != 1 || mine.Discussions[0].ID != created.Discussion.ID {
		t.Fatalf("mine all = %+v", mine.Discussions)
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
	mux.ServeHTTP(w, groupReq(http.MethodGet, "/api/a2a/consultations/"+created.Discussion.ID+"/detail?participant_id=maclaw-b", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("participant detail status = %d, body=%s", w.Code, w.Body.String())
	}
	detail = corea2a.HubDiscussionDetail{}
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatalf("unmarshal participant detail: %v", err)
	}
	if detail.Discussion.LocalRelation != "owned_ve_invited" || !detail.Discussion.Readonly || detail.Discussion.Role != "review" {
		t.Fatalf("participant detail relation = %+v, want invited read-only review", detail.Discussion)
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

func TestGroupDiscussionConsultationContextUsesFallbackInitiator(t *testing.T) {
	svc := NewGroupDiscussionService()
	handler := NewGroupDiscussionHandler(svc)
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations", `{"topic":"context fallback","question":"What happened?","context_summary":"Initial facts"}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("consultation status = %d, body=%s", w.Code, w.Body.String())
	}
	var created corea2a.ConsultationCreateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal consultation: %v", err)
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
	if len(detail.Messages) != 1 || detail.Messages[0].FromID != "initiator" || detail.Messages[0].Content != "Initial facts" {
		t.Fatalf("context messages = %+v", detail.Messages)
	}
}

func TestAuthenticatedInvitedParticipantCannotFinalizeDiscussion(t *testing.T) {
	svc := NewGroupDiscussionService()
	handler := NewGroupDiscussionHandler(svc)
	mux := http.NewServeMux()
	handler.RegisterHubRoutesWithMiddleware(mux, func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if machineID := r.Header.Get("X-Machine-ID"); machineID != "" {
				r.Header.Set("X-Authenticated-Machine-ID", machineID)
			}
			next(w, r)
		}
	})

	req := groupReq(http.MethodPost, "/api/a2a/consultations", `{"from_id":"maclaw-a","topic":"ownership boundary","question":"Who may finalize?"}`)
	req.Header.Set("X-Machine-ID", "maclaw-a")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("consultation status=%d body=%s", w.Code, w.Body.String())
	}
	var created corea2a.ConsultationCreateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal consultation: %v", err)
	}

	req = groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/invites", `{"from_id":"maclaw-a","to_id":"maclaw-b","role":"review","trusted":true}`)
	req.Header.Set("X-Machine-ID", "maclaw-a")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("invite status=%d body=%s", w.Code, w.Body.String())
	}
	var invite struct {
		InviteID string `json:"invite_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &invite); err != nil {
		t.Fatalf("unmarshal invite: %v", err)
	}

	req = groupReq(http.MethodPost, "/api/a2a/invites/"+invite.InviteID+"/accept", `{"from_id":"maclaw-b"}`)
	req.Header.Set("X-Machine-ID", "maclaw-b")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("accept status=%d body=%s", w.Code, w.Body.String())
	}

	req = groupReq(http.MethodGet, "/api/a2a/consultations/"+created.Discussion.ID+"/detail", "")
	req.Header.Set("X-Machine-ID", "maclaw-b")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("invited detail status=%d body=%s", w.Code, w.Body.String())
	}
	var detail corea2a.HubDiscussionDetail
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatalf("unmarshal detail: %v", err)
	}
	if detail.Discussion.LocalRelation != "owned_ve_invited" || !detail.Discussion.Readonly {
		t.Fatalf("invited detail relation=%+v, want readonly owned_ve_invited", detail.Discussion)
	}

	req = groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/messages", `{"from_id":"maclaw-b","content":"I can contribute, but not finalize."}`)
	req.Header.Set("X-Machine-ID", "maclaw-b")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("invited message status=%d body=%s", w.Code, w.Body.String())
	}

	req = groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/result", `{"summary":"Finalize from invited participant"}`)
	req.Header.Set("X-Machine-ID", "maclaw-b")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected invited result to be 403, got %d body=%s", w.Code, w.Body.String())
	}

	req = groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/cancel", "")
	req.Header.Set("X-Machine-ID", "maclaw-b")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected invited cancel to be 403, got %d body=%s", w.Code, w.Body.String())
	}
}
func TestGroupDiscussionMessageRejectsReadOnlyOrNonParticipant(t *testing.T) {
	svc := NewGroupDiscussionService()
	handler := NewGroupDiscussionHandler(svc)
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations", `{"from_id":"maclaw-a","topic":"observer boundary","question":"Who can write?"}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("consultation status = %d, body=%s", w.Code, w.Body.String())
	}
	var created corea2a.ConsultationCreateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal consultation: %v", err)
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/messages", `{"from_id":"maclaw-x","content":"I should not be here."}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("non-participant message status = %d, body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/invites", `{"from_id":"maclaw-a","to_id":"maclaw-b","role":"observe","trusted":true}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("invite status = %d, body=%s", w.Code, w.Body.String())
	}
	var inviteResp struct {
		InviteID string `json:"invite_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &inviteResp); err != nil {
		t.Fatalf("unmarshal invite: %v", err)
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/invites/"+inviteResp.InviteID+"/accept", `{"from_id":"maclaw-x","reason":"spoof"}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("spoofed accept status = %d, body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/invites/"+inviteResp.InviteID+"/accept", `{"from_id":"maclaw-b","reason":"watching"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("accept status = %d, body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/invites", `{"from_id":"maclaw-b","to_id":"maclaw-c","role":"speak"}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("observer invite status = %d, body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/messages", `{"from_id":"maclaw-b","content":"Observer tries to speak."}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("observer message status = %d, body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/proposals", `{"author_id":"maclaw-b","title":"Observer proposal","content":"Should fail"}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("observer proposal status = %d, body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/escalate", `{"raised_by":"maclaw-b","reason":"Should fail"}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("observer escalation status = %d, body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/messages", `{"from_id":"maclaw-a","content":"Initiator can continue."}`))
	if w.Code != http.StatusOK {
		t.Fatalf("initiator message status = %d, body=%s", w.Code, w.Body.String())
	}
}

func TestGroupDiscussionInvitationRejectsClosedSession(t *testing.T) {
	svc := NewGroupDiscussionService()
	handler := NewGroupDiscussionHandler(svc)
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations", `{"from_id":"maclaw-a","topic":"closed invite","question":"Can we add later?"}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("consultation status = %d, body=%s", w.Code, w.Body.String())
	}
	var created corea2a.ConsultationCreateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal consultation: %v", err)
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/cancel", `{}`))
	if w.Code != http.StatusOK {
		t.Fatalf("cancel status = %d, body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/invites", `{"from_id":"maclaw-a","to_id":"maclaw-b","role":"speak"}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("closed invite status = %d, body=%s", w.Code, w.Body.String())
	}
}

func TestGroupDiscussionAcceptInviteRejectsClosedSession(t *testing.T) {
	svc := NewGroupDiscussionService()
	created, err := svc.CreateConsultation("tenant-a", corea2a.GroupConsultationRequest{FromID: "maclaw-a", Topic: "accept closed", Question: "Can accept later?"})
	if err != nil {
		t.Fatalf("CreateConsultation: %v", err)
	}
	inviteID, err := svc.AddInvitation("tenant-a", created.Discussion.ID, corea2a.GroupInvitation{FromID: "maclaw-a", ToID: "maclaw-b", Role: corea2a.GroupRoleSpeak})
	if err != nil {
		t.Fatalf("AddInvitation: %v", err)
	}
	if _, err := svc.SetDiscussionState("tenant-a", created.Discussion.ID, "cancel"); err != nil {
		t.Fatalf("SetDiscussionState cancel: %v", err)
	}
	if err := svc.RespondInvitation("tenant-a", inviteID, corea2a.GroupInvitationResponse{FromID: "maclaw-b", Decision: corea2a.GroupInvitationAccept}); err == nil {
		t.Fatal("expected accepting invite for closed session to fail")
	}
	if err := svc.RespondInvitation("tenant-a", inviteID, corea2a.GroupInvitationResponse{FromID: "maclaw-b", Decision: corea2a.GroupInvitationReject}); err != nil {
		t.Fatalf("rejecting invite for closed session should still record decision: %v", err)
	}
}

func TestGroupDiscussionSessionMessageRejectsReadOnlyOrNonParticipant(t *testing.T) {
	svc := NewGroupDiscussionService()
	handler := NewGroupDiscussionHandler(svc)
	mux := http.NewServeMux()
	handler.RegisterRuntimeRoutes(mux)

	body := `{"topic":"session boundary","goal":"Who can write?","participants":[{"id":"maclaw-a","role_code":"initiator"},{"id":"maclaw-b","role_code":"observe"}]}`
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/runtime/a2a/sessions", body))
	if w.Code != http.StatusCreated {
		t.Fatalf("session create status = %d, body=%s", w.Code, w.Body.String())
	}
	var created corea2a.Session
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal session: %v", err)
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/runtime/a2a/sessions/"+created.ID+"/messages", `{"from_id":"maclaw-x","content":"I should not be here."}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("non-participant session message status = %d, body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/runtime/a2a/sessions/"+created.ID+"/messages", `{"from_id":"maclaw-b","content":"Observer tries to speak."}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("observer session message status = %d, body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/runtime/a2a/sessions/"+created.ID+"/messages", `{"from_id":"hub","content":"System note."}`))
	if w.Code != http.StatusOK {
		t.Fatalf("hub system message status = %d, body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/runtime/a2a/sessions/"+created.ID+"/messages", `{"from_id":"maclaw-a","content":"Initiator can continue."}`))
	if w.Code != http.StatusOK {
		t.Fatalf("initiator session message status = %d, body=%s", w.Code, w.Body.String())
	}
}
func TestGroupDiscussionHubProposalReviewDecision(t *testing.T) {
	svc := NewGroupDiscussionService()
	handler := NewGroupDiscussionHandler(svc)
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations", `{"from_id":"maclaw-a","topic":"proposal loop","question":"Which path?"}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("consultation status = %d, body=%s", w.Code, w.Body.String())
	}
	var created corea2a.ConsultationCreateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal consultation: %v", err)
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/proposals", `{"author_id":"maclaw-a","title":"Use staged rollout","content":"Ship behind gates","risks":["rollback drift"]}`))
	if w.Code != http.StatusOK {
		t.Fatalf("proposal status = %d, body=%s", w.Code, w.Body.String())
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
	if len(detail.Proposals) != 1 || detail.Proposals[0].Title != "Use staged rollout" {
		t.Fatalf("proposals = %+v", detail.Proposals)
	}
	proposalID := detail.Proposals[0].ID

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/reviews", `{"proposal_id":"`+proposalID+`","reviewer_id":"maclaw-a","position":"approve","comment":"safe enough"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("review status = %d, body=%s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/decide", `{"proposal_id":"`+proposalID+`","summary":"Use staged rollout","rationale":"Approval includes rollback gates","rollback_on":["gate fails", "", "gate fails"]}`))
	if w.Code != http.StatusOK {
		t.Fatalf("decide status = %d, body=%s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodGet, "/api/a2a/consultations/"+created.Discussion.ID+"/detail", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("final detail status = %d, body=%s", w.Code, w.Body.String())
	}
	detail = corea2a.HubDiscussionDetail{}
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatalf("unmarshal final detail: %v", err)
	}
	if detail.Decision == nil || detail.Decision.ProposalID != proposalID || detail.Discussion.Status != string(corea2a.SessionDecided) {
		t.Fatalf("decision detail = %+v status=%s", detail.Decision, detail.Discussion.Status)
	}
	if detail.Decision.Rationale != "Approval includes rollback gates" || len(detail.Decision.RollbackOn) != 1 || detail.Decision.RollbackOn[0] != "gate fails" {
		t.Fatalf("decision rationale/rollback = %+v", detail.Decision)
	}
	summary := detail.ReviewSummaries[proposalID]
	if summary.Approvals != 1 || summary.Rejections != 0 || summary.Concerns != 0 || len(summary.ReviewedBy) != 1 || summary.ReviewedBy[0] != "maclaw-a" {
		t.Fatalf("review summary = %+v", summary)
	}
}

func TestGroupDiscussionHubEscalation(t *testing.T) {
	svc := NewGroupDiscussionService()
	handler := NewGroupDiscussionHandler(svc)
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations", `{"from_id":"maclaw-a","topic":"blocked rollout","question":"Who should decide?"}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("consultation status = %d, body=%s", w.Code, w.Body.String())
	}
	var created corea2a.ConsultationCreateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal consultation: %v", err)
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/escalate", `{"raised_by":"maclaw-a","reason":"needs executive owner","target":"human_owner"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("escalate status = %d, body=%s", w.Code, w.Body.String())
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
	if detail.Session == nil || detail.Session.Escalation == nil || detail.Discussion.Status != string(corea2a.SessionEscalated) {
		t.Fatalf("escalation detail = %+v status=%s", detail.Session, detail.Discussion.Status)
	}
	if detail.Session.Escalation.RaisedBy != "maclaw-a" || detail.Session.Escalation.Reason != "needs executive owner" || detail.Session.Escalation.Target != "human_owner" {
		t.Fatalf("escalation = %+v", detail.Session.Escalation)
	}
}

func TestGroupDiscussionNonOpenSessionsRejectParticipantWrites(t *testing.T) {
	assertRejected := func(t *testing.T, status corea2a.SessionStatus, session *corea2a.Session, proposalID string, svc *GroupDiscussionService) {
		t.Helper()
		if _, err := svc.AddDiscussionMessage("tenant-a", session.ID, corea2a.GroupDiscussionMessage{FromID: "maclaw-a", Content: "continue"}); err == nil {
			t.Fatalf("expected %s session message to be rejected", status)
		}
		if _, err := svc.AddProposal("tenant-a", session.ID, AddProposalRequest{AuthorID: "maclaw-a", Title: "late", Content: "too late"}); err == nil {
			t.Fatalf("expected %s session proposal to be rejected", status)
		}
		if proposalID != "" {
			if _, err := svc.AddReview("tenant-a", session.ID, AddReviewRequest{ProposalID: proposalID, ReviewerID: "maclaw-a", Position: corea2a.ReviewApprove}); err == nil {
				t.Fatalf("expected %s session review to be rejected", status)
			}
		}
		if _, err := svc.Escalate("tenant-a", session.ID, EscalateRequest{RaisedBy: "maclaw-a", Reason: "late escalation"}); err == nil {
			t.Fatalf("expected %s session escalation to be rejected", status)
		}
		if _, err := svc.SubmitDiscussionResult("tenant-a", session.ID, corea2a.GroupDiscussionResult{Summary: "late result"}); err == nil {
			t.Fatalf("expected %s session result submission to be rejected", status)
		}
	}

	t.Run("closed", func(t *testing.T) {
		svc := NewGroupDiscussionService()
		created, err := svc.CreateConsultation("tenant-a", corea2a.GroupConsultationRequest{FromID: "maclaw-a", Topic: "closed writes", Question: "Can continue?"})
		if err != nil {
			t.Fatalf("CreateConsultation: %v", err)
		}
		withProposal, err := svc.AddProposal("tenant-a", created.Discussion.ID, AddProposalRequest{AuthorID: "maclaw-a", Title: "before close", Content: "available while open"})
		if err != nil {
			t.Fatalf("AddProposal: %v", err)
		}
		proposalID := withProposal.Proposals[0].ID
		closed, err := svc.SetDiscussionState("tenant-a", created.Discussion.ID, "cancel")
		if err != nil {
			t.Fatalf("SetDiscussionState cancel: %v", err)
		}
		assertRejected(t, corea2a.SessionClosed, closed, proposalID, svc)
	})

	t.Run("decided", func(t *testing.T) {
		svc := NewGroupDiscussionService()
		created, err := svc.CreateConsultation("tenant-a", corea2a.GroupConsultationRequest{FromID: "maclaw-a", Topic: "decided writes", Question: "Can continue?"})
		if err != nil {
			t.Fatalf("CreateConsultation: %v", err)
		}
		withProposal, err := svc.AddProposal("tenant-a", created.Discussion.ID, AddProposalRequest{AuthorID: "maclaw-a", Title: "ship", Content: "ship it"})
		if err != nil {
			t.Fatalf("AddProposal: %v", err)
		}
		proposalID := withProposal.Proposals[0].ID
		if _, err := svc.AddReview("tenant-a", created.Discussion.ID, AddReviewRequest{ProposalID: proposalID, ReviewerID: "maclaw-a", Position: corea2a.ReviewApprove}); err != nil {
			t.Fatalf("AddReview: %v", err)
		}
		decided, err := svc.Decide("tenant-a", created.Discussion.ID, DecideRequest{ProposalID: proposalID, Summary: "ship"})
		if err != nil {
			t.Fatalf("Decide: %v", err)
		}
		assertRejected(t, corea2a.SessionDecided, decided, proposalID, svc)
	})

	t.Run("escalated", func(t *testing.T) {
		svc := NewGroupDiscussionService()
		created, err := svc.CreateConsultation("tenant-a", corea2a.GroupConsultationRequest{FromID: "maclaw-a", Topic: "escalated writes", Question: "Can continue?"})
		if err != nil {
			t.Fatalf("CreateConsultation: %v", err)
		}
		withProposal, err := svc.AddProposal("tenant-a", created.Discussion.ID, AddProposalRequest{AuthorID: "maclaw-a", Title: "before escalation", Content: "available while open"})
		if err != nil {
			t.Fatalf("AddProposal: %v", err)
		}
		proposalID := withProposal.Proposals[0].ID
		escalated, err := svc.Escalate("tenant-a", created.Discussion.ID, EscalateRequest{RaisedBy: "maclaw-a", Reason: "needs owner"})
		if err != nil {
			t.Fatalf("Escalate: %v", err)
		}
		assertRejected(t, corea2a.SessionEscalated, escalated, proposalID, svc)
	})
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

func TestGroupDiscussionMessageHonorsTargetIDs(t *testing.T) {
	svc := NewGroupDiscussionService()
	sender := &captureGroupDiscussionSender{}
	handler := NewGroupDiscussionHandler(svc, sender)
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations", `{"from_id":"maclaw-a","topic":"targeted","question":"Please review."}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("consultation status=%d body=%s", w.Code, w.Body.String())
	}
	var created corea2a.ConsultationCreateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode consultation: %v", err)
	}

	for _, toID := range []string{"maclaw-b", "maclaw-c"} {
		if _, err := svc.AddInvitation("tenant-a", created.Discussion.ID, corea2a.GroupInvitation{FromID: "maclaw-a", ToID: toID, Role: corea2a.GroupRoleSpeak}); err != nil {
			t.Fatalf("AddInvitation %s: %v", toID, err)
		}
		invites := svc.ListInvitations("tenant-a", toID, "pending")
		if len(invites) != 1 {
			t.Fatalf("pending invites for %s = %+v", toID, invites)
		}
		if err := svc.RespondInvitation("tenant-a", invites[0].ID, corea2a.GroupInvitationResponse{FromID: toID, Decision: corea2a.GroupInvitationAccept}); err != nil {
			t.Fatalf("RespondInvitation %s: %v", toID, err)
		}
	}

	sender.messages = nil
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/messages", `{"from_id":"maclaw-a","to_ids":["maclaw-c"],"content":"Only C should see this."}`))
	if w.Code != http.StatusOK {
		t.Fatalf("message status=%d body=%s", w.Code, w.Body.String())
	}
	if len(sender.messages) != 1 || sender.messages[0].machineID != "maclaw-c" {
		t.Fatalf("sent messages = %+v, want only maclaw-c", sender.messages)
	}

	detail, err := svc.GetDiscussionDetail("tenant-a", created.Discussion.ID)
	if err != nil {
		t.Fatalf("GetDiscussionDetail: %v", err)
	}
	last := detail.Session.Messages[len(detail.Session.Messages)-1]
	if len(last.ToIDs) != 1 || last.ToIDs[0] != "maclaw-c" {
		t.Fatalf("persisted to_ids = %v, want [maclaw-c]", last.ToIDs)
	}
}

func TestGroupDiscussionMessageRejectsUnknownTargetID(t *testing.T) {
	svc := NewGroupDiscussionService()
	sender := &captureGroupDiscussionSender{}
	handler := NewGroupDiscussionHandler(svc, sender)
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations", `{"from_id":"maclaw-a","topic":"targeted","question":"Please review."}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("consultation status=%d body=%s", w.Code, w.Body.String())
	}
	var created corea2a.ConsultationCreateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode consultation: %v", err)
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/messages", `{"from_id":"maclaw-a","to_ids":["missing-ve"],"content":"Only missing should see this."}`))
	if w.Code == http.StatusOK {
		t.Fatalf("message status=%d body=%s, want failure", w.Code, w.Body.String())
	}
	if len(sender.messages) != 0 {
		t.Fatalf("sent messages = %+v, want none", sender.messages)
	}
}

func TestGroupDiscussionMessageRejectsBlankTargetIDs(t *testing.T) {
	svc := NewGroupDiscussionService()
	sender := &captureGroupDiscussionSender{}
	handler := NewGroupDiscussionHandler(svc, sender)
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations", `{"from_id":"maclaw-a","topic":"targeted","question":"Please review."}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("consultation status=%d body=%s", w.Code, w.Body.String())
	}
	var created corea2a.ConsultationCreateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode consultation: %v", err)
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/messages", `{"from_id":"maclaw-a","to_ids":[" ",""],"content":"Do not broadcast this."}`))
	if w.Code == http.StatusOK {
		t.Fatalf("message status=%d body=%s, want failure", w.Code, w.Body.String())
	}
	if len(sender.messages) != 0 {
		t.Fatalf("sent messages = %+v, want none", sender.messages)
	}
}

func TestGroupDiscussionMessageNormalizesTargetIDs(t *testing.T) {
	svc := NewGroupDiscussionService()
	sender := &captureGroupDiscussionSender{}
	handler := NewGroupDiscussionHandler(svc, sender)
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations", `{"from_id":"maclaw-a","topic":"targeted","question":"Please review."}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("consultation status=%d body=%s", w.Code, w.Body.String())
	}
	var created corea2a.ConsultationCreateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode consultation: %v", err)
	}
	if _, err := svc.AddInvitation("tenant-a", created.Discussion.ID, corea2a.GroupInvitation{FromID: "maclaw-a", ToID: "Maclaw-C", Role: corea2a.GroupRoleSpeak}); err != nil {
		t.Fatalf("AddInvitation: %v", err)
	}
	invites := svc.ListInvitations("tenant-a", "Maclaw-C", "pending")
	if len(invites) != 1 {
		t.Fatalf("pending invites = %+v", invites)
	}
	if err := svc.RespondInvitation("tenant-a", invites[0].ID, corea2a.GroupInvitationResponse{FromID: "maclaw-c", Decision: corea2a.GroupInvitationAccept}); err != nil {
		t.Fatalf("RespondInvitation: %v", err)
	}

	sender.messages = nil
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/messages", `{"from_id":"maclaw-a","to_ids":["maclaw-c","MACLAW-C"],"content":"Only C should see this."}`))
	if w.Code != http.StatusOK {
		t.Fatalf("message status=%d body=%s", w.Code, w.Body.String())
	}
	if len(sender.messages) != 1 || sender.messages[0].machineID != "maclaw-c" {
		t.Fatalf("sent messages = %+v, want only maclaw-c", sender.messages)
	}
	detail, err := svc.GetDiscussionDetail("tenant-a", created.Discussion.ID)
	if err != nil {
		t.Fatalf("GetDiscussionDetail: %v", err)
	}
	last := detail.Session.Messages[len(detail.Session.Messages)-1]
	if len(last.ToIDs) != 1 || last.ToIDs[0] != "maclaw-c" {
		t.Fatalf("persisted to_ids = %v, want [maclaw-c]", last.ToIDs)
	}
}
func TestGroupDiscussionListPagination(t *testing.T) {
	svc := NewGroupDiscussionService()
	first, err := svc.CreateConsultation("tenant-a", corea2a.GroupConsultationRequest{FromID: "maclaw-a", Topic: "first", Question: "First?"})
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	time.Sleep(time.Millisecond)
	if _, err := svc.CreateConsultation("tenant-a", corea2a.GroupConsultationRequest{FromID: "maclaw-a", Topic: "second", Question: "Second?"}); err != nil {
		t.Fatalf("create second: %v", err)
	}
	discussions, err := svc.ListDiscussionSummaries("tenant-a", ListSessionsFilter{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("list discussions: %v", err)
	}
	if len(discussions) != 1 || discussions[0].ID != first.Discussion.ID {
		t.Fatalf("expected second page to contain first discussion, got %+v", discussions)
	}
	inviteOne, err := svc.AddInvitation("tenant-a", first.Discussion.ID, corea2a.GroupInvitation{FromID: "maclaw-a", ToID: "maclaw-b", Role: corea2a.GroupRoleSpeak})
	if err != nil {
		t.Fatalf("invite one: %v", err)
	}
	time.Sleep(time.Millisecond)
	if _, err := svc.AddInvitation("tenant-a", first.Discussion.ID, corea2a.GroupInvitation{FromID: "maclaw-a", ToID: "maclaw-b", Role: corea2a.GroupRoleReview}); err != nil {
		t.Fatalf("invite two: %v", err)
	}
	invites := svc.ListInvitations("tenant-a", "", "", ListInvitationsFilter{ToID: "maclaw-b", Status: "pending", Limit: 1, Offset: 1})
	if len(invites) != 1 || invites[0].ID != inviteOne {
		t.Fatalf("expected second invite page to contain first invite, got %+v", invites)
	}
}

func TestAddParticipantIfMissingUpdatesExistingParticipantCaseInsensitive(t *testing.T) {
	session := &corea2a.Session{
		Participants: []corea2a.Participant{{ID: "MACHINE-1"}},
	}

	addParticipantIfMissing(session, corea2a.Participant{ID: "machine-1", RoleCode: "speak", Name: "Local AI", Skills: []string{"tools"}})

	if len(session.Participants) != 1 {
		t.Fatalf("participants = %+v, want one updated participant", session.Participants)
	}
	participant := session.Participants[0]
	if participant.ID != "MACHINE-1" || participant.RoleCode != "speak" || participant.Name != "Local AI" || len(participant.Skills) != 1 || participant.Skills[0] != "tools" {
		t.Fatalf("participant = %+v, want existing id with updated metadata", participant)
	}
}

func TestAddParticipantIfMissingPreservesExistingRole(t *testing.T) {
	session := &corea2a.Session{
		Participants: []corea2a.Participant{{ID: "machine-1", RoleCode: "initiator"}},
	}

	addParticipantIfMissing(session, corea2a.Participant{ID: "MACHINE-1", RoleCode: "speak"})

	if len(session.Participants) != 1 {
		t.Fatalf("participants = %+v, want one participant", session.Participants)
	}
	if got := session.Participants[0].RoleCode; got != "initiator" {
		t.Fatalf("role = %q, want initiator", got)
	}
}

func TestAddParticipantIfMissingUpgradesReadOnlyRole(t *testing.T) {
	session := &corea2a.Session{
		Participants: []corea2a.Participant{{ID: "machine-1", RoleCode: "observe"}},
	}

	addParticipantIfMissing(session, corea2a.Participant{ID: "MACHINE-1", RoleCode: "speak"})

	if len(session.Participants) != 1 {
		t.Fatalf("participants = %+v, want one participant", session.Participants)
	}
	if got := session.Participants[0].RoleCode; got != "speak" {
		t.Fatalf("role = %q, want speak", got)
	}
}

func TestGroupDiscussionParticipantCanMessageAllowsLegacyExecutorRole(t *testing.T) {
	if !groupDiscussionParticipantCanMessage("executor") {
		t.Fatal("legacy executor participants should remain writable")
	}
	if groupDiscussionParticipantCanMessage("observe") {
		t.Fatal("observe participants should remain read-only")
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

func TestGroupDiscussionReadinessIgnoresSelfTargetedRoutingCopies(t *testing.T) {
	now := time.Now().UTC()
	session := &corea2a.Session{
		ID:        "disc-self-target",
		Topic:     "Local AI routing",
		Goal:      "Ask local AI only",
		Status:    corea2a.SessionOpen,
		CreatedAt: now,
		UpdatedAt: now,
		Participants: []corea2a.Participant{
			{ID: "human-1", RoleCode: "initiator"},
			{ID: "machine-1", RoleCode: "speak"},
		},
		Messages: []corea2a.Message{
			{ID: "m1", SessionID: "disc-self-target", FromID: "machine-1", ToIDs: []string{"machine-1"}, Kind: corea2a.MessageStatement, Content: "@local-maclaw summarize this locally.", CreatedAt: now},
		},
	}

	summary := discussionSummaryFromSession(session)
	if summary.AnswerCount != 0 || summary.ExpectedAnswerCount != 1 || summary.ReadyToSummarize {
		t.Fatalf("summary with self-targeted routing copy = %+v", summary)
	}

	session.Messages = append(session.Messages, corea2a.Message{ID: "m2", SessionID: "disc-self-target", FromID: "machine-1", Kind: corea2a.MessageStatement, Content: "Local summary is ready.", CreatedAt: now})
	summary = discussionSummaryFromSession(session)
	if summary.AnswerCount != 1 || !summary.ReadyToSummarize {
		t.Fatalf("summary with local AI answer = %+v", summary)
	}
}

func TestGroupDiscussionReadinessIgnoresInitiatorStatements(t *testing.T) {
	now := time.Now().UTC()
	session := &corea2a.Session{
		ID:        "disc-readiness",
		Topic:     "Policy review",
		Goal:      "Check the policy",
		Status:    corea2a.SessionOpen,
		CreatedAt: now,
		UpdatedAt: now,
		Participants: []corea2a.Participant{
			{ID: "human-1", RoleCode: "initiator"},
			{ID: "maclaw-1", RoleCode: "review"},
			{ID: "observer-1", RoleCode: "observe"},
		},
		Messages: []corea2a.Message{
			{ID: "m1", SessionID: "disc-readiness", FromID: "human-1", Kind: corea2a.MessageStatement, Content: "Adding one more detail.", CreatedAt: now},
			{ID: "m2", SessionID: "disc-readiness", FromID: "observer-1", Kind: corea2a.MessageStatement, Content: "Watching only.", CreatedAt: now},
		},
	}
	summary := discussionSummaryFromSession(session)
	if summary.AnswerCount != 0 || summary.ExpectedAnswerCount != 1 || summary.ReadyToSummarize {
		t.Fatalf("summary with initiator/observer statements = %+v", summary)
	}
	session.Messages = append(session.Messages, corea2a.Message{ID: "m3", SessionID: "disc-readiness", FromID: "maclaw-1", Kind: corea2a.MessageStatement, Content: "The policy is acceptable with a retention note.", CreatedAt: now})
	summary = discussionSummaryFromSession(session)
	if summary.AnswerCount != 1 || !summary.ReadyToSummarize {
		t.Fatalf("summary with reviewer answer = %+v", summary)
	}
}
