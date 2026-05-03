package a2a

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	corea2a "github.com/RapidAI/CodeClaw/corelib/a2a"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/shared/response"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) RegisterRuntimeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/runtime/a2a/sessions", h.handleSessions)
	mux.HandleFunc("/runtime/a2a/sessions/", h.handleSessionAction)
}

func (h *Handler) RegisterAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/a2a/group-discussions", h.handleAdminGroupDiscussions)
}

func (h *Handler) RegisterHubRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/a2a/experts", h.handleHubExperts)
	mux.HandleFunc("/api/a2a/expert-profile", h.handleHubExpertProfile)
	mux.HandleFunc("/api/a2a/discussions/mine", h.handleHubDiscussionsMine)
	mux.HandleFunc("/api/a2a/invites/mine", h.handleHubInvitesMine)
	mux.HandleFunc("/api/a2a/consultations", h.handleHubConsultations)
	mux.HandleFunc("/api/a2a/consultations/", h.handleHubConsultationAction)
	mux.HandleFunc("/api/a2a/invites/", h.handleHubInviteAction)
}

func (h *Handler) handleAdminGroupDiscussions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	snapshot, err := h.svc.AdminGroupDiscussionSnapshot(tenant.RequestTenantID(r))
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
		return
	}
	response.OK(w, snapshot)
}

func (h *Handler) handleHubExperts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	experts := h.svc.ListExpertProfiles(tenant.RequestTenantID(r), 10*time.Minute)
	response.OK(w, map[string]any{"experts": experts})
}

func (h *Handler) handleHubExpertProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use PUT")
		return
	}
	var profile corea2a.GroupProfile
	if !decodeJSON(w, r, &profile) {
		return
	}
	stored, err := h.svc.UpsertExpertProfile(tenant.RequestTenantID(r), profile)
	if err != nil {
		response.BadRequest(w, "PROFILE_REJECTED", err.Error())
		return
	}
	response.OK(w, map[string]any{"profile": stored})
}

func (h *Handler) handleHubDiscussionsMine(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	items, err := h.svc.ListDiscussionSummaries(tenant.RequestTenantID(r), listFilterFromRequest(r))
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
		return
	}
	response.OK(w, map[string]any{"discussions": items})
}

func (h *Handler) handleHubInvitesMine(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	toID := strings.TrimSpace(r.URL.Query().Get("to_id"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	invites := h.svc.ListInvitations(tenant.RequestTenantID(r), toID, status)
	response.OK(w, map[string]any{"invites": invites})
}

func (h *Handler) handleHubConsultations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	var req corea2a.GroupConsultationRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	out, err := h.svc.CreateConsultation(tenant.RequestTenantID(r), req)
	if err != nil {
		response.BadRequest(w, "CONSULTATION_REJECTED", err.Error())
		return
	}
	response.Created(w, out)
}

func (h *Handler) handleHubConsultationAction(w http.ResponseWriter, r *http.Request) {
	tid := tenant.RequestTenantID(r)
	id, action := parseHubConsultationAction(r.URL.Path)
	if id == "" {
		response.BadRequest(w, "MISSING_CONSULTATION_ID", "consultation id is required")
		return
	}
	if action == "" {
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
			return
		}
		discussion, err := h.svc.GetDiscussionSummary(tid, id)
		if err != nil {
			response.NotFound(w, "CONSULTATION_NOT_FOUND", err.Error())
			return
		}
		response.OK(w, map[string]any{"discussion": discussion})
		return
	}
	if action == "detail" {
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
			return
		}
		detail, err := h.svc.GetDiscussionDetail(tid, id)
		if err != nil {
			response.NotFound(w, "CONSULTATION_NOT_FOUND", err.Error())
			return
		}
		response.OK(w, detail)
		return
	}
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
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
			response.BadRequest(w, "INVITE_REJECTED", err.Error())
			return
		}
		response.Created(w, map[string]any{"invite_id": inviteID})
	case "messages":
		var msg corea2a.GroupDiscussionMessage
		if !decodeJSON(w, r, &msg) {
			return
		}
		session, err := h.svc.AddDiscussionMessage(tid, id, msg)
		if err != nil {
			response.BadRequest(w, "MESSAGE_REJECTED", err.Error())
			return
		}
		response.OK(w, map[string]any{"discussion": discussionSummaryFromSession(session)})
	case "result":
		var result corea2a.GroupDiscussionResult
		if !decodeJSON(w, r, &result) {
			return
		}
		session, err := h.svc.SubmitDiscussionResult(tid, id, result)
		if err != nil {
			response.BadRequest(w, "RESULT_REJECTED", err.Error())
			return
		}
		response.OK(w, map[string]any{"discussion": discussionSummaryFromSession(session)})
	case "pause", "resume", "cancel":
		session, err := h.svc.SetDiscussionState(tid, id, action)
		if err != nil {
			response.BadRequest(w, "STATE_REJECTED", err.Error())
			return
		}
		response.OK(w, map[string]any{"discussion": discussionSummaryFromSession(session)})
	default:
		response.NotFound(w, "ACTION_NOT_FOUND", "unsupported consultation action")
	}
}

func (h *Handler) handleHubInviteAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	inviteID, action := parseHubInviteAction(r.URL.Path)
	if inviteID == "" || (action != "accept" && action != "reject") {
		response.NotFound(w, "INVITE_ACTION_NOT_FOUND", "unsupported invite action")
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
	if err := h.svc.RespondInvitation(tenant.RequestTenantID(r), inviteID, resp); err != nil {
		response.BadRequest(w, "INVITE_RESPONSE_REJECTED", err.Error())
		return
	}
	response.OK(w, map[string]any{"ok": true})
}

func (h *Handler) handleSessions(w http.ResponseWriter, r *http.Request) {
	tid := tenant.RequestTenantID(r)
	switch r.Method {
	case http.MethodGet:
		filter := listFilterFromRequest(r)
		sessions, err := h.svc.ListSessions(tid, filter)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
			return
		}
		response.OK(w, map[string]any{"sessions": sessions})
	case http.MethodPost:
		var req CreateSessionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.BadRequest(w, "INVALID_BODY", "invalid JSON")
			return
		}
		session, err := h.svc.CreateSession(tid, req)
		if err != nil {
			response.BadRequest(w, "CREATE_FAILED", err.Error())
			return
		}
		response.Created(w, session)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or POST")
	}
}

func (h *Handler) handleSessionAction(w http.ResponseWriter, r *http.Request) {
	tid := tenant.RequestTenantID(r)
	id, action := parseSessionAction(r.URL.Path)
	if id == "" {
		response.BadRequest(w, "MISSING_SESSION_ID", "session id is required")
		return
	}
	if action == "" {
		if r.Method != http.MethodGet {
			response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
			return
		}
		session, err := h.svc.GetSession(tid, id)
		if err != nil {
			response.NotFound(w, "SESSION_NOT_FOUND", err.Error())
			return
		}
		response.OK(w, session)
		return
	}
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
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
		response.NotFound(w, "ACTION_NOT_FOUND", "unsupported a2a action")
		return
	}
	if err != nil {
		response.BadRequest(w, "A2A_ACTION_FAILED", err.Error())
		return
	}
	if session != nil {
		response.OK(w, session)
	}
}

func listFilterFromRequest(r *http.Request) ListSessionsFilter {
	q := r.URL.Query()
	return ListSessionsFilter{
		OrgUnitID: normalizeOrgUnitID(q.Get("org_unit_id"), q.Get("department_id")),
		Status:    normalizeSessionStatus(q.Get("status")),
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	if err := json.NewDecoder(r.Body).Decode(out); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
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
