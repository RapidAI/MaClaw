package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	corea2a "github.com/RapidAI/CodeClaw/corelib/a2a"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
)

func groupReq(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(WithRequestTenant(req.Context(), "tenant-a"))
	return req
}

func TestRequestGroupDiscussionTenantIDUsesAuthenticatedContextAndDefaultTenant(t *testing.T) {
	defaultReq := httptest.NewRequest(http.MethodGet, "/api/a2a/experts", nil)
	if got := requestGroupDiscussionTenantID(defaultReq); got != store.DefaultTenantID {
		t.Fatalf("default tenant = %q, want %q", got, store.DefaultTenantID)
	}

	spoofReq := httptest.NewRequest(http.MethodGet, "/api/a2a/experts?tenant_id=tenant_query", nil)
	spoofReq.Header.Set("X-Hub-Tenant-ID", "tenant_spoof")
	spoofReq.Header.Set("X-Tenant-ID", "tenant_spoof")
	if got := requestGroupDiscussionTenantID(spoofReq); got != store.DefaultTenantID {
		t.Fatalf("spoofed tenant = %q, want default tenant", got)
	}

	tenantReq := httptest.NewRequest(http.MethodGet, "/api/a2a/experts", nil)
	tenantReq = tenantReq.WithContext(WithRequestTenant(tenantReq.Context(), "tenant_acme"))
	if got := requestGroupDiscussionTenantID(tenantReq); got != "tenant_acme" {
		t.Fatalf("context tenant = %q, want tenant_acme", got)
	}

	adminReq := httptest.NewRequest(http.MethodGet, "/api/admin/a2a/group-discussions?tenant_id=tenant_query", nil)
	adminReq.Header.Set("X-Tenant-ID", "tenant_spoof")
	adminReq = adminReq.WithContext(context.WithValue(adminReq.Context(), adminUserContextKey, &store.AdminUser{ID: "adm-tenant", Scope: "tenant", TenantID: "tenant_real"}))
	if got := requestGroupDiscussionTenantID(adminReq); got != "tenant_real" {
		t.Fatalf("tenant admin tenant = %q, want tenant_real", got)
	}
}

type captureGroupDiscussionSender struct {
	mu       sync.Mutex
	messages []sentGroupDiscussionMessage
	err      error
}

type sentGroupDiscussionMessage struct {
	machineID string
	msg       map[string]any
}

func (s *captureGroupDiscussionSender) SendToMachine(machineID string, msg any) error {
	mapped, _ := msg.(map[string]any)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, sentGroupDiscussionMessage{machineID: machineID, msg: mapped})
	return s.err
}

func (s *captureGroupDiscussionSender) snapshotMessages() []sentGroupDiscussionMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]sentGroupDiscussionMessage(nil), s.messages...)
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

func TestDiscussionAnswerCountCountsDistinctParticipantAliases(t *testing.T) {
	now := time.Date(2026, 5, 31, 1, 2, 3, 0, time.UTC)
	session, err := corea2a.NewSession("disc-answer-alias", "routing", "who should reply", []corea2a.Participant{{ID: "owner", RoleCode: "initiator"}, {ID: "machine-a", RoleCode: "speak"}, {ID: "machine-b", RoleCode: "speak"}}, corea2a.PolicyMajority, now)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	session.Participants = append(session.Participants, corea2a.Participant{ID: "ve_machine-a", RoleCode: "speak"})
	for _, msg := range []corea2a.Message{
		{ID: "msg-1", FromID: "machine-a", Kind: corea2a.MessageAnswer, Content: "first", CreatedAt: now},
		{ID: "msg-2", FromID: "ve-machine-a", Kind: corea2a.MessageEvidence, Content: "same speaker more evidence", CreatedAt: now.Add(time.Second)},
		{ID: "msg-3", FromID: "machine/a", Kind: corea2a.MessageObjection, Content: "same speaker slash alias", CreatedAt: now.Add(2 * time.Second)},
	} {
		if err := session.AddMessage(msg); err != nil {
			t.Fatalf("AddMessage: %v", err)
		}
	}
	if got := discussionAnswerCount(session); got != 1 {
		t.Fatalf("discussionAnswerCount = %d, want one distinct responder", got)
	}
	summary := discussionSummaryFromSession(session)
	if summary.AnswerCount != 1 || summary.ExpectedAnswerCount != 2 || summary.ReadyToSummarize {
		t.Fatalf("summary readiness = answers %d/%d ready=%v", summary.AnswerCount, summary.ExpectedAnswerCount, summary.ReadyToSummarize)
	}
	if strings.Join(summary.ParticipantIDs, ",") != "owner,machine-a,machine-b" {
		t.Fatalf("summary participants = %v, want alias-deduped participants", summary.ParticipantIDs)
	}

	if err := session.AddMessage(corea2a.Message{ID: "msg-4", FromID: "machine-b", Kind: corea2a.MessageAnswer, Content: "second speaker", CreatedAt: now.Add(3 * time.Second)}); err != nil {
		t.Fatalf("AddMessage second speaker: %v", err)
	}
	summary = discussionSummaryFromSession(session)
	if summary.AnswerCount != 2 || !summary.ReadyToSummarize {
		t.Fatalf("summary after second speaker = answers %d/%d ready=%v", summary.AnswerCount, summary.ExpectedAnswerCount, summary.ReadyToSummarize)
	}
}

func TestGroupDiscussionMessageCanonicalizesGeneratedVETargetAliases(t *testing.T) {
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
	inviteID, err := svc.AddInvitation("tenant-a", created.Discussion.ID, corea2a.GroupInvitation{FromID: "maclaw-a", ToID: "maclaw-b", Role: corea2a.GroupRoleSpeak})
	if err != nil {
		t.Fatalf("AddInvitation: %v", err)
	}
	if err := svc.RespondInvitation("tenant-a", inviteID, corea2a.GroupInvitationResponse{FromID: "maclaw-b", Decision: corea2a.GroupInvitationAccept}); err != nil {
		t.Fatalf("RespondInvitation: %v", err)
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/messages", `{"from_id":"maclaw-a","to_ids":["ve-maclaw-b"],"content":"Please analyze this clause."}`))
	if w.Code != http.StatusOK {
		t.Fatalf("message status=%d body=%s", w.Code, w.Body.String())
	}
	if len(sender.messages) != 1 || sender.messages[0].machineID != "maclaw-b" {
		t.Fatalf("sent messages = %+v, want one canonical maclaw-b target", sender.messages)
	}
}

func TestGroupDiscussionMessageDeliveryDedupesParticipantAliases(t *testing.T) {
	svc := NewGroupDiscussionService()
	sender := &captureGroupDiscussionSender{}
	handler := NewGroupDiscussionHandler(svc, sender)
	session := &corea2a.Session{
		ID:       "disc-alias-delivery",
		TenantID: "tenant-a",
		Status:   corea2a.SessionOpen,
		Participants: []corea2a.Participant{
			{ID: "maclaw-a", RoleCode: "initiator"},
			{ID: "maclaw-b", RoleCode: "speak"},
			{ID: "ve-maclaw-b", RoleCode: "speak"},
		},
	}
	msg := corea2a.GroupDiscussionMessage{ID: "msg-1", SessionID: session.ID, FromID: "maclaw-a", Kind: corea2a.MessageStatement, Content: "Please answer.", CreatedAt: time.Now().UTC()}

	if err := handler.notifyDiscussionMessage(session, msg); err != nil {
		t.Fatalf("notifyDiscussionMessage: %v", err)
	}
	sent := sender.snapshotMessages()
	if len(sent) != 1 || !groupDiscussionParticipantIdentityMatches(sent[0].machineID, "maclaw-b") {
		t.Fatalf("sent messages = %+v, want one alias-deduped delivery to maclaw-b", sent)
	}
}

func TestGroupDiscussionBuildsSharedContextSummaryWithoutDroppingRawMessages(t *testing.T) {
	svc := NewGroupDiscussionService()
	session, err := svc.CreateSession("tenant-a", CreateSessionRequest{Topic: "Long group", Goal: "Keep shared memory", Participants: []corea2a.Participant{{ID: "owner", RoleCode: "initiator"}, {ID: "anna", RoleCode: "speak"}, {ID: "xiaoyan", RoleCode: "speak"}}, DecisionPolicy: corea2a.PolicyMajority})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	for i := 0; i < groupDiscussionSummaryMinMessages; i++ {
		fromID := "owner"
		content := "@anna shared fact about project"
		if i%2 == 1 {
			fromID = "anna"
			content = "Anna answer with reusable fact"
		}
		if _, err := svc.AddDiscussionMessage("tenant-a", session.ID, corea2a.GroupDiscussionMessage{ID: fmt.Sprintf("msg-%02d", i), FromID: fromID, Kind: corea2a.MessageStatement, Content: content}); err != nil {
			t.Fatalf("AddDiscussionMessage %d: %v", i, err)
		}
	}

	detail, err := svc.GetDiscussionDetail("tenant-a", session.ID)
	if err != nil {
		t.Fatalf("GetDiscussionDetail: %v", err)
	}
	if len(detail.Messages) != groupDiscussionSummaryMinMessages {
		t.Fatalf("raw messages should be retained, got %d", len(detail.Messages))
	}
	if detail.Session == nil || !strings.Contains(detail.Session.ContextSummary, "@anna shared fact") || !strings.Contains(detail.Session.ContextSummary, "to_ids are reply targets") {
		t.Fatalf("shared summary missing @ visibility semantics: %#v", detail.Session)
	}
	if detail.Session.SummaryUpToID == "" {
		t.Fatalf("summary up-to marker should be set")
	}
}

func TestGroupDiscussionSharedContextSummaryKeepsNewestCompressedLines(t *testing.T) {
	svc := NewGroupDiscussionService()
	session, err := svc.CreateSession("tenant-a", CreateSessionRequest{Topic: "Long group", Goal: strings.Repeat("goal ", 200), Participants: []corea2a.Participant{{ID: "owner", RoleCode: "initiator"}, {ID: "anna", RoleCode: "speak"}}, DecisionPolicy: corea2a.PolicyMajority})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	for i := 0; i < groupDiscussionSummaryMinMessages+8; i++ {
		content := fmt.Sprintf("old fact %02d %s", i, strings.Repeat("x", groupDiscussionSummaryLineMaxChars))
		if i == groupDiscussionSummaryMinMessages-18 {
			content = "newest compressed boundary fact"
		}
		if _, err := svc.AddDiscussionMessage("tenant-a", session.ID, corea2a.GroupDiscussionMessage{ID: fmt.Sprintf("msg-%02d", i), FromID: "anna", Kind: corea2a.MessageStatement, Content: content}); err != nil {
			t.Fatalf("AddDiscussionMessage %d: %v", i, err)
		}
	}

	detail, err := svc.GetDiscussionDetail("tenant-a", session.ID)
	if err != nil {
		t.Fatalf("GetDiscussionDetail: %v", err)
	}
	if detail.Session == nil || !strings.Contains(detail.Session.ContextSummary, "newest compressed boundary fact") {
		t.Fatalf("summary should keep newest compressed lines, got %q", detail.Session.ContextSummary)
	}
	if len(detail.Session.ContextSummary) > groupDiscussionSummaryMaxChars+len("[/compressed shared group memory]")+200 {
		t.Fatalf("summary grew too large: %d", len(detail.Session.ContextSummary))
	}
}

func TestGroupDiscussionSharedContextSummaryRollsForwardFromExistingSummary(t *testing.T) {
	svc := NewGroupDiscussionService()
	session, err := svc.CreateSession("tenant-a", CreateSessionRequest{Topic: "Rolling group", Goal: "Keep old and new compressed facts", Participants: []corea2a.Participant{{ID: "owner", RoleCode: "initiator"}, {ID: "anna", RoleCode: "speak"}}, DecisionPolicy: corea2a.PolicyMajority})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	for i := 0; i < groupDiscussionSummaryMinMessages; i++ {
		content := fmt.Sprintf("warmup fact %02d", i)
		if i == 0 {
			content = "old durable fact"
		}
		if _, err := svc.AddDiscussionMessage("tenant-a", session.ID, corea2a.GroupDiscussionMessage{ID: fmt.Sprintf("msg-%02d", i), FromID: "anna", Kind: corea2a.MessageStatement, Content: content}); err != nil {
			t.Fatalf("AddDiscussionMessage %d: %v", i, err)
		}
	}
	first, err := svc.GetDiscussionDetail("tenant-a", session.ID)
	if err != nil {
		t.Fatalf("GetDiscussionDetail first: %v", err)
	}
	if first.Session == nil || !strings.Contains(first.Session.ContextSummary, "old durable fact") {
		t.Fatalf("first summary missing old fact: %#v", first.Session)
	}
	for i := groupDiscussionSummaryMinMessages; i < groupDiscussionSummaryMinMessages+groupDiscussionSummaryRecentMessages+2; i++ {
		content := fmt.Sprintf("new fact %02d", i)
		if i == groupDiscussionSummaryMinMessages {
			content = "new durable fact"
		}
		if _, err := svc.AddDiscussionMessage("tenant-a", session.ID, corea2a.GroupDiscussionMessage{ID: fmt.Sprintf("msg-%02d", i), FromID: "anna", Kind: corea2a.MessageStatement, Content: content}); err != nil {
			t.Fatalf("AddDiscussionMessage %d: %v", i, err)
		}
	}

	detail, err := svc.GetDiscussionDetail("tenant-a", session.ID)
	if err != nil {
		t.Fatalf("GetDiscussionDetail final: %v", err)
	}
	if detail.Session == nil || !strings.Contains(detail.Session.ContextSummary, "old durable fact") || !strings.Contains(detail.Session.ContextSummary, "new durable fact") {
		t.Fatalf("rolling summary should preserve prior summary and add new compressed facts: %q", detail.Session.ContextSummary)
	}
}

func TestNewestGroupDiscussionSummaryLinesHandlesTinyBudget(t *testing.T) {
	if got := newestGroupDiscussionSummaryLines([]string{"large line"}, 3); len(got) != 0 {
		t.Fatalf("tiny budget should not overflow summary lines: %+v", got)
	}
	got := newestGroupDiscussionSummaryLines([]string{"older", "newest line"}, 10)
	if len(got) != 1 || got[0] != "new..." {
		t.Fatalf("summary should truncate newest line within budget: %+v", got)
	}
	got = newestGroupDiscussionSummaryLines([]string{"旧事实旧事实"}, 12)
	if len(got) != 1 || len(got[0])+4 > 12 {
		t.Fatalf("summary should keep multibyte line within byte budget: %+v len=%d", got, len(got[0]))
	}
}

func TestGroupDiscussionRenameUpdatesTopicAndRequiresInitiator(t *testing.T) {
	svc := NewGroupDiscussionService()
	sender := &captureGroupDiscussionSender{}
	h := NewGroupDiscussionHandler(svc, sender)
	created, err := svc.CreateConsultation("tenant-a", corea2a.GroupConsultationRequest{FromID: "maclaw-a", Topic: "old topic", Question: "Discuss?"})
	if err != nil {
		t.Fatalf("CreateConsultation: %v", err)
	}
	id := created.Discussion.ID
	inviteID, err := svc.AddInvitation("tenant-a", id, corea2a.GroupInvitation{FromID: "maclaw-a", ToID: "maclaw-b", Role: corea2a.GroupRoleSpeak})
	if err != nil {
		t.Fatalf("AddInvitation: %v", err)
	}
	if err := svc.RespondInvitation("tenant-a", inviteID, corea2a.GroupInvitationResponse{FromID: "maclaw-b", Decision: corea2a.GroupInvitationAccept}); err != nil {
		t.Fatalf("RespondInvitation: %v", err)
	}

	bad := httptest.NewRecorder()
	h.handleHubConsultationAction(bad, groupReq(http.MethodPost, "/api/a2a/consultations/"+id+"/rename", `{"from_id":"maclaw-b","topic":"bad"}`))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("non-initiator rename status = %d, want %d; body=%s", bad.Code, http.StatusBadRequest, bad.Body.String())
	}

	good := httptest.NewRecorder()
	h.handleHubConsultationAction(good, groupReq(http.MethodPost, "/api/a2a/consultations/"+id+"/rename", `{"from_id":"maclaw-a","topic":"new group"}`))
	if good.Code != http.StatusOK {
		t.Fatalf("rename status = %d, want %d; body=%s", good.Code, http.StatusOK, good.Body.String())
	}
	var renameResp struct {
		Discussion corea2a.HubDiscussionSummary `json:"discussion"`
	}
	if err := json.Unmarshal(good.Body.Bytes(), &renameResp); err != nil {
		t.Fatalf("decode rename response: %v", err)
	}
	if renameResp.Discussion.LocalRelation != "initiated_by_me" || renameResp.Discussion.Readonly {
		t.Fatalf("rename response should be decorated for initiator, got %+v", renameResp.Discussion)
	}
	got, err := svc.GetDiscussionSummary("tenant-a", id)
	if err != nil {
		t.Fatalf("GetDiscussionSummary: %v", err)
	}
	if got.Topic != "new group" {
		t.Fatalf("topic = %q, want new group", got.Topic)
	}
	sent := sender.snapshotMessages()
	if len(sent) != 1 || sent[0].machineID != "maclaw-b" || sent[0].msg["type"] != "ve:discussion_rename" {
		t.Fatalf("rename push = %#v, want one ve:discussion_rename to maclaw-b", sent)
	}
	payload, _ := sent[0].msg["payload"].(map[string]any)
	if payload["topic"] != "new group" || payload["discussion_id"] != id {
		t.Fatalf("rename payload = %#v", payload)
	}
}

func TestGroupDiscussionInviteAndCancelPushToTargetParticipant(t *testing.T) {
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

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/invites", `{"from_id":"maclaw-a","to_id":"maclaw-b","role":"review"}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("invite status=%d body=%s", w.Code, w.Body.String())
	}
	if len(sender.messages) != 1 {
		t.Fatalf("sent invite messages = %+v", sender.messages)
	}
	invite := sender.messages[0]
	if invite.machineID != "maclaw-b" || invite.msg["type"] != "ve:discussion_invite" {
		t.Fatalf("unexpected invite push: %+v", invite)
	}
	invitePayload, ok := invite.msg["payload"].(map[string]any)
	if !ok || invitePayload["invite_id"] == "" {
		t.Fatalf("invite payload missing id: %#v", invite.msg["payload"])
	}
	inviteEnvelope, ok := invitePayload["envelope"].(corea2a.GroupEnvelope)
	if !ok || inviteEnvelope.Invitation == nil || inviteEnvelope.SessionID != created.Discussion.ID || inviteEnvelope.Type != corea2a.GroupMessageInvitation {
		t.Fatalf("unexpected invite envelope: %#v", invitePayload["envelope"])
	}

	invites := svc.ListInvitations("tenant-a", "maclaw-b", "pending")
	if len(invites) != 1 {
		t.Fatalf("pending invites = %+v", invites)
	}
	if err := svc.RespondInvitation("tenant-a", invites[0].ID, corea2a.GroupInvitationResponse{FromID: "maclaw-b", Decision: corea2a.GroupInvitationAccept}); err != nil {
		t.Fatalf("RespondInvitation: %v", err)
	}
	sender.messages = nil

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/cancel", `{}`))
	if w.Code != http.StatusOK {
		t.Fatalf("cancel status=%d body=%s", w.Code, w.Body.String())
	}
	if len(sender.messages) != 1 {
		t.Fatalf("sent cancel messages = %+v", sender.messages)
	}
	cancel := sender.messages[0]
	if cancel.machineID != "maclaw-b" || cancel.msg["type"] != "ve:discussion_cancel" {
		t.Fatalf("unexpected cancel push: %+v", cancel)
	}
	cancelPayload, ok := cancel.msg["payload"].(map[string]any)
	if !ok {
		t.Fatalf("cancel payload type = %T", cancel.msg["payload"])
	}
	cancelEnvelope, ok := cancelPayload["envelope"].(corea2a.GroupEnvelope)
	if !ok || cancelEnvelope.SessionID != created.Discussion.ID {
		t.Fatalf("unexpected cancel envelope: %#v", cancelPayload["envelope"])
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

func TestGroupDiscussionInvitationTerminalDecisionIsImmutable(t *testing.T) {
	svc := NewGroupDiscussionService()
	created, err := svc.CreateConsultation("tenant-a", corea2a.GroupConsultationRequest{FromID: "maclaw-a", Topic: "terminal invite", Question: "Can flip?"})
	if err != nil {
		t.Fatalf("CreateConsultation: %v", err)
	}
	inviteID, err := svc.AddInvitation("tenant-a", created.Discussion.ID, corea2a.GroupInvitation{FromID: "maclaw-a", ToID: "maclaw-b", Role: corea2a.GroupRoleSpeak})
	if err != nil {
		t.Fatalf("AddInvitation: %v", err)
	}
	if err := svc.RespondInvitation("tenant-a", inviteID, corea2a.GroupInvitationResponse{FromID: "maclaw-b", Decision: corea2a.GroupInvitationAccept}); err != nil {
		t.Fatalf("accept invite: %v", err)
	}
	if err := svc.RespondInvitation("tenant-a", inviteID, corea2a.GroupInvitationResponse{FromID: "maclaw-b", Decision: corea2a.GroupInvitationAccept}); err != nil {
		t.Fatalf("duplicate accept should be idempotent: %v", err)
	}
	if err := svc.RespondInvitation("tenant-a", inviteID, corea2a.GroupInvitationResponse{FromID: "maclaw-b", Decision: corea2a.GroupInvitationReject}); err == nil || !strings.Contains(err.Error(), "already accept") {
		t.Fatalf("reject after accept error = %v, want already accept", err)
	}
	invites := svc.ListInvitations("tenant-a", "maclaw-b", "accept")
	if len(invites) != 1 || invites[0].Status != "accept" {
		t.Fatalf("accepted invite status = %+v", invites)
	}
}

func TestGroupDiscussionDuplicateAcceptRepairsMissingParticipant(t *testing.T) {
	svc := NewGroupDiscussionService()
	created, err := svc.CreateConsultation("tenant-a", corea2a.GroupConsultationRequest{FromID: "maclaw-a", Topic: "repair invite", Question: "Can recover?"})
	if err != nil {
		t.Fatalf("CreateConsultation: %v", err)
	}
	inviteID, err := svc.AddInvitation("tenant-a", created.Discussion.ID, corea2a.GroupInvitation{FromID: "maclaw-a", ToID: "maclaw-b", Role: corea2a.GroupRoleSpeak})
	if err != nil {
		t.Fatalf("AddInvitation: %v", err)
	}
	if err := svc.RespondInvitation("tenant-a", inviteID, corea2a.GroupInvitationResponse{FromID: "maclaw-b", Decision: corea2a.GroupInvitationAccept}); err != nil {
		t.Fatalf("accept invite: %v", err)
	}

	svc.mu.Lock()
	session := svc.sessions["tenant-a"][created.Discussion.ID]
	participants := session.Participants[:0]
	for _, participant := range session.Participants {
		if strings.EqualFold(strings.TrimSpace(participant.ID), "maclaw-b") {
			continue
		}
		participants = append(participants, participant)
	}
	session.Participants = participants
	svc.mu.Unlock()

	if err := svc.RespondInvitation("tenant-a", inviteID, corea2a.GroupInvitationResponse{FromID: "maclaw-b", Decision: corea2a.GroupInvitationAccept}); err != nil {
		t.Fatalf("duplicate accept should repair participant: %v", err)
	}
	detail, err := svc.GetDiscussionDetail("tenant-a", created.Discussion.ID)
	if err != nil {
		t.Fatalf("GetDiscussionDetail: %v", err)
	}
	for _, participant := range detail.Session.Participants {
		if strings.EqualFold(strings.TrimSpace(participant.ID), "maclaw-b") && participant.RoleCode == string(corea2a.GroupRoleSpeak) {
			return
		}
	}
	t.Fatalf("duplicate accept did not restore participant, got %+v", detail.Session.Participants)
}

func TestGroupDiscussionInvitationRejectsUnsupportedDecision(t *testing.T) {
	svc := NewGroupDiscussionService()
	created, err := svc.CreateConsultation("tenant-a", corea2a.GroupConsultationRequest{FromID: "maclaw-a", Topic: "invalid decision", Question: "Can decide?"})
	if err != nil {
		t.Fatalf("CreateConsultation: %v", err)
	}
	inviteID, err := svc.AddInvitation("tenant-a", created.Discussion.ID, corea2a.GroupInvitation{FromID: "maclaw-a", ToID: "maclaw-b", Role: corea2a.GroupRoleSpeak})
	if err != nil {
		t.Fatalf("AddInvitation: %v", err)
	}
	if err := svc.RespondInvitation("tenant-a", inviteID, corea2a.GroupInvitationResponse{FromID: "maclaw-b", Decision: corea2a.GroupInvitationDecision("maybe")}); err == nil || !strings.Contains(err.Error(), "unsupported invite decision") {
		t.Fatalf("invalid decision error = %v, want unsupported invite decision", err)
	}
	invites := svc.ListInvitations("tenant-a", "maclaw-b", "pending")
	if len(invites) != 1 || invites[0].Status != "pending" {
		t.Fatalf("invalid decision should leave invite pending, got %+v", invites)
	}
}

func TestGroupDiscussionInvitationRejectReasonInSummary(t *testing.T) {
	svc := NewGroupDiscussionService()
	created, err := svc.CreateConsultation("tenant-a", corea2a.GroupConsultationRequest{FromID: "maclaw-a", Topic: "reject reason", Question: "Why?"})
	if err != nil {
		t.Fatalf("CreateConsultation: %v", err)
	}
	inviteID, err := svc.AddInvitation("tenant-a", created.Discussion.ID, corea2a.GroupInvitation{FromID: "maclaw-a", ToID: "maclaw-b", Role: corea2a.GroupRoleSpeak})
	if err != nil {
		t.Fatalf("AddInvitation: %v", err)
	}
	if err := svc.RespondInvitation("tenant-a", inviteID, corea2a.GroupInvitationResponse{FromID: "maclaw-b", Decision: corea2a.GroupInvitationReject, Reason: "not active"}); err != nil {
		t.Fatalf("reject invite: %v", err)
	}
	invites := svc.ListInvitations("tenant-a", "maclaw-b", "reject")
	if len(invites) != 1 || invites[0].Reason != "not active" {
		t.Fatalf("rejected invite summary = %+v", invites)
	}
}

func TestGroupDiscussionListSentInvitationsForAuthenticatedSender(t *testing.T) {
	svc := NewGroupDiscussionService()
	handler := NewGroupDiscussionHandler(svc)
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)
	created, err := svc.CreateConsultation("tenant-a", corea2a.GroupConsultationRequest{FromID: "maclaw-a", Topic: "sent invite", Question: "Who joined?"})
	if err != nil {
		t.Fatalf("CreateConsultation: %v", err)
	}
	inviteID, err := svc.AddInvitation("tenant-a", created.Discussion.ID, corea2a.GroupInvitation{FromID: "maclaw-a", ToID: "maclaw-b", Role: corea2a.GroupRoleSpeak})
	if err != nil {
		t.Fatalf("AddInvitation: %v", err)
	}
	if err := svc.RespondInvitation("tenant-a", inviteID, corea2a.GroupInvitationResponse{FromID: "maclaw-b", Decision: corea2a.GroupInvitationReject, Reason: "busy"}); err != nil {
		t.Fatalf("reject invite: %v", err)
	}

	req := groupReq(http.MethodGet, "/api/a2a/invites/mine?from_id=maclaw-a&status=all", "")
	req.Header.Set("X-Authenticated-Machine-ID", "maclaw-a")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list sent invites status = %d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Invites []corea2a.GroupInviteSummary `json:"invites"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Invites) != 1 || body.Invites[0].ID != inviteID || body.Invites[0].Reason != "busy" {
		t.Fatalf("sent invites = %+v", body.Invites)
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

func TestGroupDiscussionSubmitResultDedupesDeciderAliases(t *testing.T) {
	svc := NewGroupDiscussionService()
	session, err := svc.CreateSession("tenant-a", CreateSessionRequest{
		Topic: "alias result",
		Goal:  "summarize",
		Participants: []corea2a.Participant{
			{ID: "owner", RoleCode: "initiator"},
			{ID: "machine-a", RoleCode: "speak"},
			{ID: "ve-machine-a", RoleCode: "speak"},
			{ID: "machine-b", RoleCode: "speak"},
		},
		DecisionPolicy: corea2a.PolicyMajority,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	session, err = svc.SubmitDiscussionResult("tenant-a", session.ID, corea2a.GroupDiscussionResult{Summary: "Use staged rollout"})
	if err != nil {
		t.Fatalf("SubmitDiscussionResult: %v", err)
	}
	if session.Decision == nil {
		t.Fatal("missing decision")
	}
	if strings.Join(session.Decision.DecidedBy, ",") != "owner,machine-a,machine-b" {
		t.Fatalf("decided_by = %v, want alias-deduped participants", session.Decision.DecidedBy)
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

func TestGroupDiscussionMessageDefaultTargetsRemoteVEsAndSkipsLocalAI(t *testing.T) {
	svc := NewGroupDiscussionService()
	sender := &captureGroupDiscussionSender{}
	handler := NewGroupDiscussionHandler(svc, sender)
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations", `{"from_id":"machine-1","topic":"targeted","question":"Please review."}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("consultation status=%d body=%s", w.Code, w.Body.String())
	}
	var created corea2a.ConsultationCreateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode consultation: %v", err)
	}

	for _, toID := range []string{"anna-machine", "xiaoyan-machine", "local-maclaw"} {
		if _, err := svc.AddInvitation("tenant-a", created.Discussion.ID, corea2a.GroupInvitation{FromID: "machine-1", ToID: toID, Role: corea2a.GroupRoleSpeak}); err != nil {
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
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/messages", `{"from_id":"machine-1","content":"continue"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("message status=%d body=%s", w.Code, w.Body.String())
	}
	if len(sender.messages) != 2 || sender.messages[0].machineID != "anna-machine" || sender.messages[1].machineID != "xiaoyan-machine" {
		t.Fatalf("sent messages = %+v, want anna and xiaoyan only", sender.messages)
	}
	detail, err := svc.GetDiscussionDetail("tenant-a", created.Discussion.ID)
	if err != nil {
		t.Fatalf("GetDiscussionDetail: %v", err)
	}
	last := detail.Session.Messages[len(detail.Session.Messages)-1]
	want := []string{"anna-machine", "xiaoyan-machine"}
	if len(last.ToIDs) != len(want) || last.ToIDs[0] != want[0] || last.ToIDs[1] != want[1] {
		t.Fatalf("persisted to_ids = %v, want %v", last.ToIDs, want)
	}
}

func TestGroupDiscussionMessageDefaultReplyTargetFollowsLastExplicitTarget(t *testing.T) {
	svc := NewGroupDiscussionService()
	sender := &captureGroupDiscussionSender{}
	handler := NewGroupDiscussionHandler(svc, sender)
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations", `{"from_id":"machine-1","topic":"targeted","question":"Please review."}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("consultation status=%d body=%s", w.Code, w.Body.String())
	}
	var created corea2a.ConsultationCreateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode consultation: %v", err)
	}
	for _, toID := range []string{"anna-machine", "xiaoyan-machine", "local-maclaw"} {
		if _, err := svc.AddInvitation("tenant-a", created.Discussion.ID, corea2a.GroupInvitation{FromID: "machine-1", ToID: toID, Role: corea2a.GroupRoleSpeak}); err != nil {
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
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/messages", `{"from_id":"machine-1","to_ids":["local-maclaw"],"content":"@local inspect"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("explicit local status=%d body=%s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/messages", `{"from_id":"machine-1","content":"continue"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("follow-up local status=%d body=%s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/messages", `{"from_id":"machine-1","to_ids":["xiaoyan-machine"],"content":"@xiaoyan switch"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("explicit xiaoyan status=%d body=%s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/messages", `{"from_id":"machine-1","content":"continue again"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("follow-up xiaoyan status=%d body=%s", w.Code, w.Body.String())
	}

	if len(sender.messages) != 4 {
		t.Fatalf("sent messages = %+v", sender.messages)
	}
	if sender.messages[0].machineID != "local-maclaw" || sender.messages[1].machineID != "local-maclaw" || sender.messages[2].machineID != "xiaoyan-machine" || sender.messages[3].machineID != "xiaoyan-machine" {
		t.Fatalf("sent messages = %+v, want local/local/xiaoyan/xiaoyan", sender.messages)
	}
	detail, err := svc.GetDiscussionDetail("tenant-a", created.Discussion.ID)
	if err != nil {
		t.Fatalf("GetDiscussionDetail: %v", err)
	}
	last := detail.Session.Messages[len(detail.Session.Messages)-1]
	if len(last.ToIDs) != 1 || last.ToIDs[0] != "xiaoyan-machine" {
		t.Fatalf("persisted follow-up to_ids = %v, want [xiaoyan-machine]", last.ToIDs)
	}
}

func TestGroupDiscussionUnresolvedMentionClearsDefaultReplyTarget(t *testing.T) {
	svc := NewGroupDiscussionService()
	sender := &captureGroupDiscussionSender{}
	handler := NewGroupDiscussionHandler(svc, sender)
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations", `{"from_id":"machine-1","topic":"targeted","question":"Please review."}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("consultation status=%d body=%s", w.Code, w.Body.String())
	}
	var created corea2a.ConsultationCreateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode consultation: %v", err)
	}
	for _, toID := range []string{"anna-machine", "xiaoyan-machine", "local-maclaw"} {
		if _, err := svc.AddInvitation("tenant-a", created.Discussion.ID, corea2a.GroupInvitation{FromID: "machine-1", ToID: toID, Role: corea2a.GroupRoleSpeak}); err != nil {
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

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/messages", `{"from_id":"machine-1","to_ids":["local-maclaw"],"content":"@local inspect"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("explicit local status=%d body=%s", w.Code, w.Body.String())
	}
	sender.messages = nil
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/messages", `{"from_id":"machine-1","content":"@unknown continue"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("unresolved status=%d body=%s", w.Code, w.Body.String())
	}
	if len(sender.messages) != 3 {
		t.Fatalf("unresolved mention should clear default target and broadcast, sent=%+v", sender.messages)
	}
	detail, err := svc.GetDiscussionDetail("tenant-a", created.Discussion.ID)
	if err != nil {
		t.Fatalf("GetDiscussionDetail: %v", err)
	}
	last := detail.Session.Messages[len(detail.Session.Messages)-1]
	if len(last.ToIDs) != 0 {
		t.Fatalf("unresolved mention persisted to_ids = %v, want broadcast", last.ToIDs)
	}
}

func TestGroupDiscussionDeliveryFailureRollsBackDefaultReplyTarget(t *testing.T) {
	svc := NewGroupDiscussionService()
	sender := &captureGroupDiscussionSender{}
	handler := NewGroupDiscussionHandler(svc, sender)
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations", `{"from_id":"machine-1","topic":"targeted","question":"Please review."}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("consultation status=%d body=%s", w.Code, w.Body.String())
	}
	var created corea2a.ConsultationCreateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode consultation: %v", err)
	}
	for _, toID := range []string{"anna-machine", "xiaoyan-machine", "local-maclaw"} {
		if _, err := svc.AddInvitation("tenant-a", created.Discussion.ID, corea2a.GroupInvitation{FromID: "machine-1", ToID: toID, Role: corea2a.GroupRoleSpeak}); err != nil {
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

	sender.err = errors.New("delivery unavailable")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/messages", `{"from_id":"machine-1","to_ids":["local-maclaw"],"content":"@local inspect"}`))
	if w.Code != http.StatusBadGateway {
		t.Fatalf("failed delivery status=%d body=%s", w.Code, w.Body.String())
	}
	detail, err := svc.GetDiscussionDetail("tenant-a", created.Discussion.ID)
	if err != nil {
		t.Fatalf("GetDiscussionDetail after failure: %v", err)
	}
	if len(detail.Session.DefaultReplyTargets) != 0 {
		t.Fatalf("failed explicit target should not leave default reply target: %+v", detail.Session.DefaultReplyTargets)
	}

	sender.err = nil
	sender.messages = nil
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/messages", `{"from_id":"machine-1","content":"continue"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("follow-up status=%d body=%s", w.Code, w.Body.String())
	}
	if len(sender.messages) != 2 || sender.messages[0].machineID != "anna-machine" || sender.messages[1].machineID != "xiaoyan-machine" {
		t.Fatalf("follow-up should use normal default targets after rollback, sent=%+v", sender.messages)
	}
}

func TestGroupDiscussionDuplicateMessageDoesNotMutateDefaultReplyTarget(t *testing.T) {
	svc := NewGroupDiscussionService()
	session, err := svc.CreateSession("tenant-a", CreateSessionRequest{Topic: "idempotent target", Participants: []corea2a.Participant{{ID: "owner", RoleCode: "initiator"}, {ID: "anna", RoleCode: "speak"}, {ID: "local-maclaw", RoleCode: "speak"}}, DecisionPolicy: corea2a.PolicyMajority})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := svc.AddDiscussionMessage("tenant-a", session.ID, corea2a.GroupDiscussionMessage{ID: "msg-same", FromID: "owner", ToIDs: []string{"local-maclaw"}, Kind: corea2a.MessageStatement, Content: "ask local"}); err != nil {
		t.Fatalf("first AddDiscussionMessage: %v", err)
	}
	if _, err := svc.AddDiscussionMessage("tenant-a", session.ID, corea2a.GroupDiscussionMessage{ID: "msg-same", FromID: "owner", ToIDs: []string{"anna"}, Kind: corea2a.MessageStatement, Content: "duplicate retry with stale target"}); err != nil {
		t.Fatalf("duplicate AddDiscussionMessage: %v", err)
	}
	updated, err := svc.AddDiscussionMessage("tenant-a", session.ID, corea2a.GroupDiscussionMessage{ID: "msg-follow", FromID: "owner", Kind: corea2a.MessageStatement, Content: "continue"})
	if err != nil {
		t.Fatalf("follow AddDiscussionMessage: %v", err)
	}
	last := updated.Messages[len(updated.Messages)-1]
	if len(last.ToIDs) != 1 || last.ToIDs[0] != "local-maclaw" {
		t.Fatalf("duplicate message mutated default target, follow-up to_ids=%v", last.ToIDs)
	}
}
func TestGroupDiscussionFailedMessageDoesNotCanonicalizeStoredDefaultReplyTarget(t *testing.T) {
	svc := NewGroupDiscussionService()
	session, err := svc.CreateSession("tenant-a", CreateSessionRequest{Topic: "failed canonical target", Participants: []corea2a.Participant{{ID: "owner", RoleCode: "initiator"}, {ID: "local-maclaw", RoleCode: "speak"}}, DecisionPolicy: corea2a.PolicyMajority})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	svc.mu.Lock()
	svc.sessions["tenant-a"][session.ID].DefaultReplyTargets = map[string][]string{"owner": {"ve_local-maclaw"}}
	svc.mu.Unlock()

	if _, err := svc.AddDiscussionMessage("tenant-a", session.ID, corea2a.GroupDiscussionMessage{ID: "msg-empty", FromID: "owner", Kind: corea2a.MessageStatement}); err == nil {
		t.Fatalf("empty AddDiscussionMessage should fail")
	}
	detail, err := svc.GetDiscussionDetail("tenant-a", session.ID)
	if err != nil {
		t.Fatalf("GetDiscussionDetail: %v", err)
	}
	stored := detail.Session.DefaultReplyTargets["owner"]
	if len(stored) != 1 || stored[0] != "ve_local-maclaw" {
		t.Fatalf("failed message canonicalized default target: %+v", detail.Session.DefaultReplyTargets)
	}
	updated, err := svc.AddDiscussionMessage("tenant-a", session.ID, corea2a.GroupDiscussionMessage{ID: "msg-follow", FromID: "owner", Kind: corea2a.MessageStatement, Content: "continue"})
	if err != nil {
		t.Fatalf("follow AddDiscussionMessage: %v", err)
	}
	last := updated.Messages[len(updated.Messages)-1]
	if len(last.ToIDs) != 1 || last.ToIDs[0] != "local-maclaw" {
		t.Fatalf("follow-up should still resolve stored alias, to_ids=%v", last.ToIDs)
	}
}
func TestGroupDiscussionFailedMessageDoesNotMutateDefaultReplyTarget(t *testing.T) {
	svc := NewGroupDiscussionService()
	session, err := svc.CreateSession("tenant-a", CreateSessionRequest{Topic: "failed target", Participants: []corea2a.Participant{{ID: "owner", RoleCode: "initiator"}, {ID: "anna", RoleCode: "speak"}, {ID: "local-maclaw", RoleCode: "speak"}}, DecisionPolicy: corea2a.PolicyMajority})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := svc.AddDiscussionMessage("tenant-a", session.ID, corea2a.GroupDiscussionMessage{ID: "msg-local", FromID: "owner", ToIDs: []string{"local-maclaw"}, Kind: corea2a.MessageStatement, Content: "ask local"}); err != nil {
		t.Fatalf("seed AddDiscussionMessage: %v", err)
	}
	if _, err := svc.AddDiscussionMessage("tenant-a", session.ID, corea2a.GroupDiscussionMessage{ID: "msg-empty", FromID: "owner", ToIDs: []string{"anna"}, Kind: corea2a.MessageStatement}); err == nil {
		t.Fatalf("empty AddDiscussionMessage should fail")
	}
	updated, err := svc.AddDiscussionMessage("tenant-a", session.ID, corea2a.GroupDiscussionMessage{ID: "msg-follow", FromID: "owner", Kind: corea2a.MessageStatement, Content: "continue"})
	if err != nil {
		t.Fatalf("follow AddDiscussionMessage: %v", err)
	}
	last := updated.Messages[len(updated.Messages)-1]
	if len(last.ToIDs) != 1 || last.ToIDs[0] != "local-maclaw" {
		t.Fatalf("failed AddDiscussionMessage mutated default target, follow-up to_ids=%v", last.ToIDs)
	}

	runtimeSession, err := svc.CreateSession("tenant-a", CreateSessionRequest{Topic: "failed runtime target", Participants: []corea2a.Participant{{ID: "owner", RoleCode: "initiator"}, {ID: "anna", RoleCode: "speak"}, {ID: "local-maclaw", RoleCode: "speak"}}, DecisionPolicy: corea2a.PolicyMajority})
	if err != nil {
		t.Fatalf("CreateSession runtime: %v", err)
	}
	if _, err := svc.AddMessage("tenant-a", runtimeSession.ID, AddMessageRequest{FromID: "owner", ToIDs: []string{"local-maclaw"}, Kind: corea2a.MessageStatement, Content: "ask local"}); err != nil {
		t.Fatalf("seed AddMessage: %v", err)
	}
	if _, err := svc.AddMessage("tenant-a", runtimeSession.ID, AddMessageRequest{FromID: "owner", ToIDs: []string{"anna"}, Kind: corea2a.MessageStatement}); err == nil {
		t.Fatalf("empty AddMessage should fail")
	}
	updated, err = svc.AddMessage("tenant-a", runtimeSession.ID, AddMessageRequest{FromID: "owner", Kind: corea2a.MessageStatement, Content: "continue"})
	if err != nil {
		t.Fatalf("follow AddMessage: %v", err)
	}
	last = updated.Messages[len(updated.Messages)-1]
	if len(last.ToIDs) != 1 || last.ToIDs[0] != "local-maclaw" {
		t.Fatalf("failed AddMessage mutated default target, follow-up to_ids=%v", last.ToIDs)
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

func TestGroupDiscussionMessageNormalizesGeneratedVETargetIDs(t *testing.T) {
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
	if _, err := svc.AddInvitation("tenant-a", created.Discussion.ID, corea2a.GroupInvitation{FromID: "maclaw-a", ToID: "maclaw-c", Role: corea2a.GroupRoleSpeak}); err != nil {
		t.Fatalf("AddInvitation: %v", err)
	}
	invites := svc.ListInvitations("tenant-a", "maclaw-c", "pending")
	if len(invites) != 1 {
		t.Fatalf("pending invites = %+v", invites)
	}
	if err := svc.RespondInvitation("tenant-a", invites[0].ID, corea2a.GroupInvitationResponse{FromID: "maclaw-c", Decision: corea2a.GroupInvitationAccept}); err != nil {
		t.Fatalf("RespondInvitation: %v", err)
	}

	sender.messages = nil
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, groupReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/messages", `{"from_id":"maclaw-a","to_ids":["ve_maclaw-c"],"content":"Only C should see this."}`))
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

func TestGroupDiscussionMessageDedupesGeneratedVETargetAliases(t *testing.T) {
	svc := NewGroupDiscussionService()
	session, err := svc.CreateSession("tenant-a", CreateSessionRequest{
		Topic: "target aliases",
		Goal:  "route once",
		Participants: []corea2a.Participant{
			{ID: "maclaw-a", RoleCode: "initiator"},
			{ID: "maclaw-c", RoleCode: "speak"},
			{ID: "ve-maclaw-c", RoleCode: "speak"},
		},
		DecisionPolicy: corea2a.PolicyMajority,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	session, err = svc.AddDiscussionMessage("tenant-a", session.ID, corea2a.GroupDiscussionMessage{FromID: "maclaw-a", ToIDs: []string{"maclaw-c", "ve_maclaw-c"}, Content: "Only C should see this."})
	if err != nil {
		t.Fatalf("AddDiscussionMessage: %v", err)
	}
	last := session.Messages[len(session.Messages)-1]
	if len(last.ToIDs) != 1 || last.ToIDs[0] != "maclaw-c" {
		t.Fatalf("persisted to_ids = %v, want [maclaw-c]", last.ToIDs)
	}
}

func TestGroupDiscussionListInvitationsMatchesToIDCaseInsensitive(t *testing.T) {
	svc := NewGroupDiscussionService()
	created, err := svc.CreateConsultation("tenant-a", corea2a.GroupConsultationRequest{FromID: "maclaw-a", Topic: "case", Question: "Invite?"})
	if err != nil {
		t.Fatalf("CreateConsultation: %v", err)
	}
	if _, err := svc.AddInvitation("tenant-a", created.Discussion.ID, corea2a.GroupInvitation{FromID: "maclaw-a", ToID: "Maclaw-C", Role: corea2a.GroupRoleSpeak}); err != nil {
		t.Fatalf("AddInvitation: %v", err)
	}
	invites := svc.ListInvitations("tenant-a", "maclaw-c", "pending")
	if len(invites) != 1 || invites[0].ToID != "Maclaw-C" {
		t.Fatalf("case-insensitive invite lookup = %+v", invites)
	}
}

func TestGroupDiscussionListInvitationsMatchesGeneratedVEAliases(t *testing.T) {
	svc := NewGroupDiscussionService()
	created, err := svc.CreateConsultation("tenant-a", corea2a.GroupConsultationRequest{FromID: "maclaw-a", Topic: "alias", Question: "Invite?"})
	if err != nil {
		t.Fatalf("CreateConsultation: %v", err)
	}
	if _, err := svc.AddInvitation("tenant-a", created.Discussion.ID, corea2a.GroupInvitation{FromID: "maclaw-a", ToID: "maclaw-b", Role: corea2a.GroupRoleSpeak}); err != nil {
		t.Fatalf("AddInvitation: %v", err)
	}

	invites := svc.ListInvitations("tenant-a", "ve-maclaw-b", "pending", ListInvitationsFilter{FromID: "ve_maclaw-a"})
	if len(invites) != 1 || invites[0].FromID != "maclaw-a" || invites[0].ToID != "maclaw-b" {
		t.Fatalf("alias invite lookup = %+v", invites)
	}
}

func TestGroupDiscussionRespondInvitationAcceptsGeneratedVEAliasWithoutDuplicate(t *testing.T) {
	svc := NewGroupDiscussionService()
	created, err := svc.CreateConsultation("tenant-a", corea2a.GroupConsultationRequest{FromID: "maclaw-a", Topic: "alias", Question: "Invite?"})
	if err != nil {
		t.Fatalf("CreateConsultation: %v", err)
	}
	inviteID, err := svc.AddInvitation("tenant-a", created.Discussion.ID, corea2a.GroupInvitation{FromID: "maclaw-a", ToID: "maclaw-b", Role: corea2a.GroupRoleSpeak})
	if err != nil {
		t.Fatalf("AddInvitation: %v", err)
	}
	if err := svc.RespondInvitation("tenant-a", inviteID, corea2a.GroupInvitationResponse{FromID: "ve-maclaw-b", Decision: corea2a.GroupInvitationAccept}); err != nil {
		t.Fatalf("RespondInvitation alias accept: %v", err)
	}
	if err := svc.RespondInvitation("tenant-a", inviteID, corea2a.GroupInvitationResponse{FromID: "ve_maclaw-b", Decision: corea2a.GroupInvitationAccept}); err != nil {
		t.Fatalf("RespondInvitation duplicate alias accept: %v", err)
	}
	detail, err := svc.GetDiscussionDetail("tenant-a", created.Discussion.ID)
	if err != nil {
		t.Fatalf("GetDiscussionDetail: %v", err)
	}
	count := 0
	for _, participant := range detail.Session.Participants {
		if groupDiscussionParticipantIdentityMatches(participant.ID, "maclaw-b") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("maclaw-b participant count = %d in %+v, want 1", count, detail.Session.Participants)
	}
}

func TestAuthenticatedMineEndpointsAcceptGeneratedVEAliases(t *testing.T) {
	svc := NewGroupDiscussionService()
	handler := NewGroupDiscussionHandler(svc, &captureGroupDiscussionSender{})
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)
	created, err := svc.CreateConsultation("tenant-a", corea2a.GroupConsultationRequest{FromID: "maclaw-a", Topic: "alias", Question: "Mine?"})
	if err != nil {
		t.Fatalf("CreateConsultation: %v", err)
	}
	inviteID, err := svc.AddInvitation("tenant-a", created.Discussion.ID, corea2a.GroupInvitation{FromID: "maclaw-a", ToID: "maclaw-b", Role: corea2a.GroupRoleSpeak})
	if err != nil {
		t.Fatalf("AddInvitation: %v", err)
	}

	w := httptest.NewRecorder()
	req := groupReq(http.MethodGet, "/api/a2a/discussions/mine?participant_id=ve-maclaw-a", "")
	req.Header.Set("X-Authenticated-Machine-ID", "maclaw-a")
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("discussions/mine status=%d body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = groupReq(http.MethodGet, "/api/a2a/invites/mine?invite_id="+inviteID+"&from_id=ve_maclaw-a", "")
	req.Header.Set("X-Authenticated-Machine-ID", "maclaw-a")
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("invites/mine status=%d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Invites []corea2a.GroupInviteSummary `json:"invites"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode invites/mine: %v", err)
	}
	if len(body.Invites) != 1 || body.Invites[0].ID != inviteID {
		t.Fatalf("invites/mine = %+v, want invite %s", body.Invites, inviteID)
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

func TestGroupDiscussionServicePersistsDerivedGroupChatStateAcrossRestart(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "group-discussion-derived.db")
	provider, err := sqlite.NewProvider(sqlite.Config{DSN: dbPath, WAL: true, BusyTimeoutMS: 5000, MaxReadOpenConns: 2, MaxReadIdleConns: 1, MaxWriteOpenConns: 1, MaxWriteIdleConns: 1})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })

	svc := NewGroupDiscussionService(provider.Write)
	session, err := svc.CreateSession("tenant-a", CreateSessionRequest{Topic: "persist derived", Goal: "shared memory survives restart", Participants: []corea2a.Participant{{ID: "owner", RoleCode: "initiator"}, {ID: "anna", RoleCode: "speak"}, {ID: "local-maclaw", RoleCode: "speak"}}, DecisionPolicy: corea2a.PolicyMajority})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := svc.AddDiscussionMessage("tenant-a", session.ID, corea2a.GroupDiscussionMessage{ID: "msg-00", FromID: "owner", ToIDs: []string{"local-maclaw"}, Kind: corea2a.MessageStatement, Content: "local should keep default reply right"}); err != nil {
		t.Fatalf("explicit AddDiscussionMessage: %v", err)
	}
	for i := 1; i < groupDiscussionSummaryMinMessages; i++ {
		if _, err := svc.AddDiscussionMessage("tenant-a", session.ID, corea2a.GroupDiscussionMessage{ID: fmt.Sprintf("msg-%02d", i), FromID: "owner", Kind: corea2a.MessageStatement, Content: fmt.Sprintf("durable shared fact %02d", i)}); err != nil {
			t.Fatalf("AddDiscussionMessage %d: %v", i, err)
		}
	}

	restarted := NewGroupDiscussionService(provider.Write)
	detail, err := restarted.GetDiscussionDetail("tenant-a", session.ID)
	if err != nil {
		t.Fatalf("GetDiscussionDetail after restart: %v", err)
	}
	if detail.Session == nil || !strings.Contains(detail.Session.ContextSummary, "durable shared fact") || detail.Session.SummaryUpToID == "" {
		t.Fatalf("restored summary missing: %+v", detail.Session)
	}
	updated, err := restarted.AddDiscussionMessage("tenant-a", session.ID, corea2a.GroupDiscussionMessage{ID: "msg-after-restart", FromID: "owner", Kind: corea2a.MessageStatement, Content: "continue after restart"})
	if err != nil {
		t.Fatalf("AddDiscussionMessage after restart: %v", err)
	}
	last := updated.Messages[len(updated.Messages)-1]
	if len(last.ToIDs) != 1 || last.ToIDs[0] != "local-maclaw" {
		t.Fatalf("restored default reply target to_ids=%v", last.ToIDs)
	}
}
func TestGroupDiscussionServicePersistsRejectReasonAcrossRestart(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "group-discussion-reject.db")
	provider, err := sqlite.NewProvider(sqlite.Config{DSN: dbPath, WAL: true, BusyTimeoutMS: 5000, MaxReadOpenConns: 2, MaxReadIdleConns: 1, MaxWriteOpenConns: 1, MaxWriteIdleConns: 1})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	svc := NewGroupDiscussionService(provider.Write)
	created, err := svc.CreateConsultation("tenant-a", corea2a.GroupConsultationRequest{FromID: "maclaw-a", Topic: "persist reject", Question: "Does reason survive?"})
	if err != nil {
		t.Fatalf("create consultation: %v", err)
	}
	inviteID, err := svc.AddInvitation("tenant-a", created.Discussion.ID, corea2a.GroupInvitation{FromID: "maclaw-a", ToID: "maclaw-b", Role: corea2a.GroupRoleSpeak})
	if err != nil {
		t.Fatalf("add invite: %v", err)
	}
	if err := svc.RespondInvitation("tenant-a", inviteID, corea2a.GroupInvitationResponse{FromID: "maclaw-b", Decision: corea2a.GroupInvitationReject, Reason: "offline"}); err != nil {
		t.Fatalf("reject invite: %v", err)
	}

	restarted := NewGroupDiscussionService(provider.Write)
	invites := restarted.ListInvitations("tenant-a", "maclaw-b", "reject")
	if len(invites) != 1 || invites[0].ID != inviteID || invites[0].Reason != "offline" {
		t.Fatalf("restored rejected invites = %+v", invites)
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

func TestGroupDiscussionReadinessUsesParticipantRoleAliases(t *testing.T) {
	now := time.Now().UTC()
	session := &corea2a.Session{
		ID:        "disc-role-alias",
		Topic:     "Alias roles",
		Goal:      "Ignore generated initiator and observer aliases",
		Status:    corea2a.SessionOpen,
		CreatedAt: now,
		UpdatedAt: now,
		Participants: []corea2a.Participant{
			{ID: "human-1", RoleCode: "initiator"},
			{ID: "observer-1", RoleCode: "observe"},
			{ID: "maclaw-1", RoleCode: "speak"},
		},
		Messages: []corea2a.Message{
			{ID: "m1", SessionID: "disc-role-alias", FromID: "human/1", Kind: corea2a.MessageStatement, Content: "I am starting the discussion.", CreatedAt: now},
			{ID: "m2", SessionID: "disc-role-alias", FromID: "ve_observer-1", Kind: corea2a.MessageStatement, Content: "Watching only.", CreatedAt: now},
			{ID: "m3", SessionID: "disc-role-alias", FromID: "ve-maclaw-1", Kind: corea2a.MessageStatement, Content: "Substantive answer.", CreatedAt: now},
		},
	}

	summary := discussionSummaryFromSession(session)
	if summary.AnswerCount != 1 || summary.ExpectedAnswerCount != 1 || !summary.ReadyToSummarize {
		t.Fatalf("summary with aliased roles = %+v", summary)
	}
}
