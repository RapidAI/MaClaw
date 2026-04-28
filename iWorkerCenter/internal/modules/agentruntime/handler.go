package agentruntime

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/shared/response"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) RegisterRuntimeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/runtime/iworker/instances/heartbeat", h.handleHeartbeat)
}

func (h *Handler) RegisterClientRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/client/iworker/instances", h.handleList)
}

func (h *Handler) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	var req HeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}
	result, err := h.svc.Heartbeat(requestTenantID(r), req, time.Now().UTC())
	if err != nil {
		response.BadRequest(w, "HEARTBEAT_FAILED", err.Error())
		return
	}
	response.OK(w, result)
}

func (h *Handler) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	offlineAfter := DefaultOfflineAfter
	if seconds, _ := strconv.Atoi(r.URL.Query().Get("offline_after_seconds")); seconds > 0 {
		offlineAfter = time.Duration(seconds) * time.Second
	}
	items, err := h.svc.ListWithHealth(requestTenantID(r), r.URL.Query().Get("worker_id"), time.Now().UTC(), offlineAfter)
	if err != nil {
		response.BadRequest(w, "LIST_INSTANCES_FAILED", err.Error())
		return
	}
	response.OK(w, map[string]any{"instances": items})
}

func requestTenantID(r *http.Request) string {
	return tenant.RequestTenantID(r)
}
