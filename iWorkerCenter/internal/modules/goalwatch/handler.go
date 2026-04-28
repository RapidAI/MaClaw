package goalwatch

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/shared/response"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) RegisterRuntimeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/runtime/goalwatch/check", h.handleCheck)
}

func (h *Handler) RegisterClientRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/client/goalwatch/pushes", h.handleClientPushes)
	mux.HandleFunc("/client/goalwatch/pushes/", h.handleClientPushAction)
}

func (h *Handler) RegisterAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/goalwatch/check", h.handleCheck)
}

func parsePushAction(path string) (string, string) {
	rest := strings.TrimPrefix(path, "/client/goalwatch/pushes/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) != 2 {
		return "", ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

func (h *Handler) handleClientPushes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	tid := tenant.TenantIDFromContext(r.Context())
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	pushes, err := h.svc.ListPushesForColleague(tid, r.URL.Query().Get("colleague_id"), limit)
	if err != nil {
		response.BadRequest(w, "LIST_PUSHES_FAILED", err.Error())
		return
	}
	response.OK(w, map[string]any{"pushes": pushes})
}

func (h *Handler) handleClientPushAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	eventID, action := parsePushAction(r.URL.Path)
	if eventID == "" || action != "ack" {
		response.NotFound(w, "ACTION_NOT_FOUND", "expected /client/goalwatch/pushes/{event_id}/ack")
		return
	}
	var req struct {
		ColleagueID string `json:"colleague_id"`
		Status      string `json:"status"`
		Note        string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}
	result, err := h.svc.AckPush(tenant.TenantIDFromContext(r.Context()), req.ColleagueID, eventID, req.Status, req.Note, time.Now().UTC())
	if err != nil {
		response.BadRequest(w, "ACK_PUSH_FAILED", err.Error())
		return
	}
	response.OK(w, result)
}

func (h *Handler) handleCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or POST")
		return
	}
	tid := tenant.TenantIDFromContext(r.Context())
	svc := h.svc
	if minutes, _ := strconv.Atoi(r.URL.Query().Get("stalled_after_minutes")); minutes > 0 {
		cfg := h.svc.Config()
		cfg.StalledAfter = time.Duration(minutes) * time.Minute
		svc = NewService(h.svc.collabRepo, cfg)
	}
	result, err := svc.CheckTenant(tid, time.Now().UTC())
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, result)
}
