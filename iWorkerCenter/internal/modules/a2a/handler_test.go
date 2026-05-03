package a2a

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	corea2a "github.com/RapidAI/CodeClaw/corelib/a2a"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
)

func tenantReq(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	return req.WithContext(tenant.WithTenantID(req.Context(), "tenant-a"))
}

func TestRuntimeA2AProposalReviewDecision(t *testing.T) {
	handler := NewHandler(NewService())
	mux := http.NewServeMux()
	handler.RegisterRuntimeRoutes(mux)

	createBody := `{"topic":"delivery exception","goal":"choose response plan","org_unit_id":"quality-domain","participants":[{"id":"ops"},{"id":"quality"}],"decision_policy":"majority"}`
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, tenantReq(http.MethodPost, "/runtime/a2a/sessions", createBody))
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", w.Code, w.Body.String())
	}
	var session corea2a.Session
	if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil {
		t.Fatalf("unmarshal session: %v", err)
	}
	if session.ID == "" || session.TenantID != "tenant-a" || session.OrgUnitID != "quality-domain" {
		t.Fatalf("unexpected session: %+v", session)
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, tenantReq(http.MethodPost, "/runtime/a2a/sessions/"+session.ID+"/proposals", `{"author_id":"ops","title":"repair first","content":"repair before customer notice"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("proposal status = %d, body=%s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil {
		t.Fatalf("unmarshal proposal session: %v", err)
	}
	proposalID := session.Proposals[0].ID

	for _, reviewer := range []string{"ops", "quality"} {
		body := `{"proposal_id":"` + proposalID + `","reviewer_id":"` + reviewer + `","position":"approve"}`
		w = httptest.NewRecorder()
		mux.ServeHTTP(w, tenantReq(http.MethodPost, "/runtime/a2a/sessions/"+session.ID+"/reviews", body))
		if w.Code != http.StatusOK {
			t.Fatalf("review status = %d, body=%s", w.Code, w.Body.String())
		}
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, tenantReq(http.MethodPost, "/runtime/a2a/sessions/"+session.ID+"/decide", `{"proposal_id":"`+proposalID+`","summary":"repair first"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("decide status = %d, body=%s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil {
		t.Fatalf("unmarshal decided session: %v", err)
	}
	if session.Status != corea2a.SessionDecided || session.Decision == nil {
		t.Fatalf("expected decided session, got %+v", session)
	}
}

func TestRuntimeA2AListFiltersByOrgUnit(t *testing.T) {
	handler := NewHandler(NewService())
	mux := http.NewServeMux()
	handler.RegisterRuntimeRoutes(mux)

	for _, body := range []string{
		`{"topic":"quality incident","org_unit_id":"quality-domain","participants":[{"id":"qa"}]}`,
		`{"topic":"sales exception","org_unit_id":"sales-domain","participants":[{"id":"sales"}]}`,
	} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, tenantReq(http.MethodPost, "/runtime/a2a/sessions", body))
		if w.Code != http.StatusCreated {
			t.Fatalf("create status = %d, body=%s", w.Code, w.Body.String())
		}
	}

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, tenantReq(http.MethodGet, "/runtime/a2a/sessions?org_unit_id=quality-domain", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Sessions []corea2a.Session `json:"sessions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if len(resp.Sessions) != 1 || resp.Sessions[0].OrgUnitID != "quality-domain" {
		t.Fatalf("unexpected filtered sessions: %+v", resp.Sessions)
	}
}

func TestRuntimeA2ARejectsPrematureDecision(t *testing.T) {
	svc := NewService()
	session, err := svc.CreateSession("tenant-a", CreateSessionRequest{Topic: "risk", Participants: []corea2a.Participant{{ID: "ops"}, {ID: "finance"}, {ID: "legal"}}})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	updated, err := svc.AddProposal("tenant-a", session.ID, AddProposalRequest{AuthorID: "ops", Title: "ship", Content: "ship now"})
	if err != nil {
		t.Fatalf("add proposal: %v", err)
	}
	_, _ = svc.AddReview("tenant-a", session.ID, AddReviewRequest{ProposalID: updated.Proposals[0].ID, ReviewerID: "ops", Position: corea2a.ReviewApprove})
	if _, err := svc.Decide("tenant-a", session.ID, DecideRequest{ProposalID: updated.Proposals[0].ID}); err == nil {
		t.Fatal("expected premature decision to fail")
	}
}

func TestAdminGroupDiscussionsSnapshotIncludesExpertsAndResults(t *testing.T) {
	svc := NewService()
	handler := NewHandler(svc)
	mux := http.NewServeMux()
	handler.RegisterRuntimeRoutes(mux)
	handler.RegisterAdminRoutes(mux)
	handler.RegisterHubRoutes(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, tenantReq(http.MethodPut, "/api/a2a/expert-profile", `{"agent_id":"maclaw-a","display_name":"Review MaClaw","skills":["go","security"],"discoverable":true,"available":true}`))
	if w.Code != http.StatusOK {
		t.Fatalf("profile status = %d, body=%s", w.Code, w.Body.String())
	}

	createBody := `{"topic":"group design","goal":"choose safe invitation policy","participants":[{"id":"maclaw-a"},{"id":"maclaw-b"}],"decision_policy":"majority"}`
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, tenantReq(http.MethodPost, "/runtime/a2a/sessions", createBody))
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", w.Code, w.Body.String())
	}
	var session corea2a.Session
	if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil {
		t.Fatalf("unmarshal session: %v", err)
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, tenantReq(http.MethodPost, "/runtime/a2a/sessions/"+session.ID+"/proposals", `{"author_id":"maclaw-a","title":"ask by default","content":"ask user unless same security group is allowed"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("proposal status = %d, body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, tenantReq(http.MethodGet, "/admin/a2a/group-discussions", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("admin status = %d, body=%s", w.Code, w.Body.String())
	}
	var snapshot AdminGroupDiscussionSnapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	if len(snapshot.Experts) != 1 || snapshot.Experts[0].AgentID != "maclaw-a" {
		t.Fatalf("experts = %+v", snapshot.Experts)
	}
	if len(snapshot.Discussions) != 1 || snapshot.Discussions[0].ResultSummary != "ask by default" {
		t.Fatalf("discussions = %+v", snapshot.Discussions)
	}
}

func TestHubConsultationLifecycleRoutes(t *testing.T) {
	svc := NewService()
	handler := NewHandler(svc)
	mux := http.NewServeMux()
	handler.RegisterHubRoutes(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, tenantReq(http.MethodPost, "/api/a2a/consultations", `{"from_id":"maclaw-a","topic":"security design","question":"Which invite policy is safest?","context_summary":"Need current-hub only discussion."}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", w.Code, w.Body.String())
	}
	var created corea2a.ConsultationCreateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create: %v", err)
	}
	if created.Discussion.ID == "" || created.Discussion.Question != "Which invite policy is safest?" {
		t.Fatalf("created = %+v", created)
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, tenantReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/invites", `{"from_id":"maclaw-a","to_id":"maclaw-b","role":"review"}`))
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
		t.Fatalf("invite response = %+v", inviteResp)
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, tenantReq(http.MethodPost, "/api/a2a/invites/"+inviteResp.InviteID+"/accept", `{"from_id":"maclaw-b","reason":"available"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("accept status = %d, body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, tenantReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/messages", `{"from_id":"maclaw-b","content":"Ask by default, auto-accept only for same security group."}`))
	if w.Code != http.StatusOK {
		t.Fatalf("message status = %d, body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, tenantReq(http.MethodPost, "/api/a2a/consultations/"+created.Discussion.ID+"/result", `{"summary":"Ask by default","rationale":"Keeps human authorization unless both sides explicitly allow same security group."}`))
	if w.Code != http.StatusOK {
		t.Fatalf("result status = %d, body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, tenantReq(http.MethodGet, "/api/a2a/consultations/"+created.Discussion.ID, ""))
	if w.Code != http.StatusOK {
		t.Fatalf("get status = %d, body=%s", w.Code, w.Body.String())
	}
	var got struct {
		Discussion corea2a.HubDiscussionSummary `json:"discussion"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal get: %v", err)
	}
	if got.Discussion.ResultSummary != "Ask by default" || got.Discussion.Status != "decided" || len(got.Discussion.ParticipantIDs) != 2 {
		t.Fatalf("discussion = %+v", got.Discussion)
	}
	if got.Discussion.AnswerCount != 1 || got.Discussion.ExpectedAnswerCount != 1 || !got.Discussion.ReadyToSummarize {
		t.Fatalf("discussion readiness = %+v", got.Discussion)
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, tenantReq(http.MethodGet, "/api/a2a/consultations/"+created.Discussion.ID+"/detail", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("detail status = %d, body=%s", w.Code, w.Body.String())
	}
	var detail corea2a.HubDiscussionDetail
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatalf("unmarshal detail: %v", err)
	}
	if detail.Discussion.ID != created.Discussion.ID || detail.Decision == nil || len(detail.Messages) < 3 {
		t.Fatalf("detail = %+v", detail)
	}
}
