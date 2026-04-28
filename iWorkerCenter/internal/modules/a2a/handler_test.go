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
