package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	corea2a "github.com/RapidAI/CodeClaw/corelib/a2a"
)

type GroupDiscussionHandler struct {
	svc *GroupDiscussionService
}

func NewGroupDiscussionHandler(svc *GroupDiscussionService) *GroupDiscussionHandler {
	return &GroupDiscussionHandler{svc: svc}
}

func (h *GroupDiscussionHandler) RegisterRuntimeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/runtime/a2a/sessions", h.handleSessions)
	mux.HandleFunc("/runtime/a2a/sessions/", h.handleSessionAction)
}

func (h *GroupDiscussionHandler) RegisterAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/a2a/group-discussions", h.handleAdminGroupDiscussions)
}

func (h *GroupDiscussionHandler) RegisterHubRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/a2a/experts", h.handleHubExperts)
	mux.HandleFunc("/api/a2a/expert-profile", h.handleHubExpertProfile)
	mux.HandleFunc("/api/a2a/discussions/mine", h.handleHubDiscussionsMine)
	mux.HandleFunc("/api/a2a/invites/mine", h.handleHubInvitesMine)
	mux.HandleFunc("/api/a2a/consultations", h.handleHubConsultations)
	mux.HandleFunc("/api/a2a/consultations/", h.handleHubConsultationAction)
	mux.HandleFunc("/api/a2a/invites/", h.handleHubInviteAction)
}

func (h *GroupDiscussionHandler) handleAdminGroupDiscussions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	snapshot, err := h.svc.AdminGroupDiscussionSnapshot(requestGroupDiscussionTenantID(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (h *GroupDiscussionHandler) handleHubExperts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	experts := h.svc.ListExpertProfiles(requestGroupDiscussionTenantID(r), 10*time.Minute)
	writeJSON(w, http.StatusOK, map[string]any{"experts": experts})
}

func (h *GroupDiscussionHandler) handleHubExpertProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use PUT")
		return
	}
	var profile corea2a.GroupProfile
	if !decodeJSON(w, r, &profile) {
		return
	}
	stored, err := h.svc.UpsertExpertProfile(requestGroupDiscussionTenantID(r), profile)
	if err != nil {
		writeError(w, http.StatusBadRequest, "PROFILE_REJECTED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"profile": stored})
}

func (h *GroupDiscussionHandler) handleHubDiscussionsMine(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	items, err := h.svc.ListDiscussionSummaries(requestGroupDiscussionTenantID(r), listFilterFromRequest(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"discussions": items})
}

func (h *GroupDiscussionHandler) handleHubInvitesMine(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	q := r.URL.Query()
	filter := ListInvitationsFilter{ToID: q.Get("to_id"), Status: q.Get("status"), Limit: intQuery(q, "limit"), Offset: intQuery(q, "offset")}
	invites := h.svc.ListInvitations(requestGroupDiscussionTenantID(r), "", "", filter)
	writeJSON(w, http.StatusOK, map[string]any{"invites": invites})
}

func (h *GroupDiscussionHandler) handleHubConsultations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	var req corea2a.GroupConsultationRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	out, err := h.svc.CreateConsultation(requestGroupDiscussionTenantID(r), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "CONSULTATION_REJECTED", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (h *GroupDiscussionHandler) handleHubConsultationAction(w http.ResponseWriter, r *http.Request) {
	tid := requestGroupDiscussionTenantID(r)
	id, action := parseHubConsultationAction(r.URL.Path)
	if id == "" {
		writeError(w, http.StatusBadRequest, "MISSING_CONSULTATION_ID", "consultation id is required")
		return
	}
	if action == "" {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
			return
		}
		discussion, err := h.svc.GetDiscussionSummary(tid, id)
		if err != nil {
			writeError(w, http.StatusNotFound, "CONSULTATION_NOT_FOUND", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"discussion": discussion})
		return
	}
	if action == "detail" {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
			return
		}
		detail, err := h.svc.GetDiscussionDetail(tid, id)
		if err != nil {
			writeError(w, http.StatusNotFound, "CONSULTATION_NOT_FOUND", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, detail)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	switch action {
	case "invites":
		var inv corea2a.GroupInvitation
		if !decodeJSON(w, r, &inv) {
			return
		}
		inviteID, err := h.svc.AddInvitation(tid, id, inv)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVITE_REJECTED", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"invite_id": inviteID})
	case "messages":
		var msg corea2a.GroupDiscussionMessage
		if !decodeJSON(w, r, &msg) {
			return
		}
		session, err := h.svc.AddDiscussionMessage(tid, id, msg)
		if err != nil {
			writeError(w, http.StatusBadRequest, "MESSAGE_REJECTED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"discussion": discussionSummaryFromSession(session)})
	case "proposals":
		var req AddProposalRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		session, err := h.svc.AddProposal(tid, id, req)
		if err != nil {
			writeError(w, http.StatusBadRequest, "PROPOSAL_REJECTED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"discussion": discussionSummaryFromSession(session)})
	case "reviews":
		var req AddReviewRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		session, err := h.svc.AddReview(tid, id, req)
		if err != nil {
			writeError(w, http.StatusBadRequest, "REVIEW_REJECTED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"discussion": discussionSummaryFromSession(session)})
	case "decide":
		var req DecideRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		session, err := h.svc.Decide(tid, id, req)
		if err != nil {
			writeError(w, http.StatusBadRequest, "DECISION_REJECTED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"discussion": discussionSummaryFromSession(session)})
	case "escalate":
		var req EscalateRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		session, err := h.svc.Escalate(tid, id, req)
		if err != nil {
			writeError(w, http.StatusBadRequest, "ESCALATION_REJECTED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"discussion": discussionSummaryFromSession(session)})
	case "result":
		var result corea2a.GroupDiscussionResult
		if !decodeJSON(w, r, &result) {
			return
		}
		session, err := h.svc.SubmitDiscussionResult(tid, id, result)
		if err != nil {
			writeError(w, http.StatusBadRequest, "RESULT_REJECTED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"discussion": discussionSummaryFromSession(session)})
	case "pause", "resume", "cancel":
		session, err := h.svc.SetDiscussionState(tid, id, action)
		if err != nil {
			writeError(w, http.StatusBadRequest, "STATE_REJECTED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"discussion": discussionSummaryFromSession(session)})
	default:
		writeError(w, http.StatusNotFound, "ACTION_NOT_FOUND", "unsupported consultation action")
	}
}

func (h *GroupDiscussionHandler) handleHubInviteAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	inviteID, action := parseHubInviteAction(r.URL.Path)
	if inviteID == "" || (action != "accept" && action != "reject") {
		writeError(w, http.StatusNotFound, "INVITE_ACTION_NOT_FOUND", "unsupported invite action")
		return
	}
	var resp corea2a.GroupInvitationResponse
	if !decodeJSON(w, r, &resp) {
		return
	}
	if action == "accept" {
		resp.Decision = corea2a.GroupInvitationAccept
	} else {
		resp.Decision = corea2a.GroupInvitationReject
	}
	if err := h.svc.RespondInvitation(requestGroupDiscussionTenantID(r), inviteID, resp); err != nil {
		writeError(w, http.StatusBadRequest, "INVITE_RESPONSE_REJECTED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *GroupDiscussionHandler) handleSessions(w http.ResponseWriter, r *http.Request) {
	tid := requestGroupDiscussionTenantID(r)
	switch r.Method {
	case http.MethodGet:
		filter := listFilterFromRequest(r)
		sessions, err := h.svc.ListSessions(tid, filter)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
	case http.MethodPost:
		var req CreateSessionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_BODY", "invalid JSON")
			return
		}
		session, err := h.svc.CreateSession(tid, req)
		if err != nil {
			writeError(w, http.StatusBadRequest, "CREATE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, session)
	default:
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or POST")
	}
}

func (h *GroupDiscussionHandler) handleSessionAction(w http.ResponseWriter, r *http.Request) {
	tid := requestGroupDiscussionTenantID(r)
	id, action := parseSessionAction(r.URL.Path)
	if id == "" {
		writeError(w, http.StatusBadRequest, "MISSING_SESSION_ID", "session id is required")
		return
	}
	if action == "" {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
			return
		}
		session, err := h.svc.GetSession(tid, id)
		if err != nil {
			writeError(w, http.StatusNotFound, "SESSION_NOT_FOUND", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, session)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	var session any
	var err error
	switch action {
	case "messages":
		var req AddMessageRequest
		if decodeJSON(w, r, &req) {
			session, err = h.svc.AddMessage(tid, id, req)
		}
	case "proposals":
		var req AddProposalRequest
		if decodeJSON(w, r, &req) {
			session, err = h.svc.AddProposal(tid, id, req)
		}
	case "reviews":
		var req AddReviewRequest
		if decodeJSON(w, r, &req) {
			session, err = h.svc.AddReview(tid, id, req)
		}
	case "decide":
		var req DecideRequest
		if decodeJSON(w, r, &req) {
			session, err = h.svc.Decide(tid, id, req)
		}
	case "escalate":
		var req EscalateRequest
		if decodeJSON(w, r, &req) {
			session, err = h.svc.Escalate(tid, id, req)
		}
	default:
		writeError(w, http.StatusNotFound, "ACTION_NOT_FOUND", "unsupported a2a action")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "A2A_ACTION_FAILED", err.Error())
		return
	}
	if session != nil {
		writeJSON(w, http.StatusOK, session)
	}
}

func listFilterFromRequest(r *http.Request) ListSessionsFilter {
	q := r.URL.Query()
	return ListSessionsFilter{
		OrgUnitID:     normalizeOrgUnitID(q.Get("org_unit_id"), q.Get("department_id")),
		Status:        normalizeSessionStatus(q.Get("status")),
		ParticipantID: firstNonEmptyQuery(q, "participant_id", "agent_id", "from_id"),
		Role:          strings.TrimSpace(q.Get("role")),
		Limit:         intQuery(q, "limit"),
		Offset:        intQuery(q, "offset"),
	}
}

func intQuery(q map[string][]string, key string) int {
	values := q[key]
	if len(values) == 0 {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(values[0]))
	if err != nil {
		return 0
	}
	return n
}

func firstNonEmptyQuery(q map[string][]string, keys ...string) string {
	for _, key := range keys {
		if values, ok := q[key]; ok {
			for _, value := range values {
				if strings.TrimSpace(value) != "" {
					return strings.TrimSpace(value)
				}
			}
		}
	}
	return ""
}

func decodeJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	if err := json.NewDecoder(r.Body).Decode(out); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "invalid JSON")
		return false
	}
	return true
}

func normalizeSessionStatus(value string) corea2a.SessionStatus {
	value = strings.TrimSpace(value)
	switch corea2a.SessionStatus(value) {
	case corea2a.SessionOpen, corea2a.SessionDecided, corea2a.SessionEscalated, corea2a.SessionClosed:
		return corea2a.SessionStatus(value)
	default:
		return ""
	}
}

func parseSessionAction(path string) (string, string) {
	rest := strings.TrimPrefix(path, "/runtime/a2a/sessions/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return "", ""
	}
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}

func parseHubConsultationAction(path string) (string, string) {
	rest := strings.TrimPrefix(path, "/api/a2a/consultations/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return "", ""
	}
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}

func parseHubInviteAction(path string) (string, string) {
	rest := strings.TrimPrefix(path, "/api/a2a/invites/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) < 2 || parts[0] == "" {
		return "", ""
	}
	return parts[0], parts[1]
}

func requestGroupDiscussionTenantID(r *http.Request) string {
	if r == nil {
		return "default"
	}
	for _, key := range []string{"X-Tenant-ID", "X-Hub-Tenant-ID"} {
		if value := strings.TrimSpace(r.Header.Get(key)); value != "" {
			return value
		}
	}
	return "default"
}
