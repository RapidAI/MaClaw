package a2a

import (
	"encoding/json"
	"net/http"
	"strings"

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

func (h *Handler) handleSessions(w http.ResponseWriter, r *http.Request) {
	tid := tenant.TenantIDFromContext(r.Context())
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
	tid := tenant.TenantIDFromContext(r.Context())
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
