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
	svc     *Service
	monitor *Monitor
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) SetMonitor(monitor *Monitor) {
	if h != nil {
		h.monitor = monitor
	}
}

func (h *Handler) RegisterRuntimeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/runtime/goalwatch/check", h.handleCheck)
}

func (h *Handler) RegisterClientRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/client/goalwatch/pushes", h.handleClientPushes)
	mux.HandleFunc("/client/goalwatch/pushes/", h.handleClientPushAction)
}

func (h *Handler) RegisterAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/goalwatch/check", h.handleCheck)
	mux.HandleFunc("/admin/goalwatch/status", h.handleStatus)
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
	tid := requestTenantID(r)
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
	result, err := h.svc.AckPush(requestTenantID(r), req.ColleagueID, eventID, req.Status, req.Note, time.Now().UTC())
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
	tid := requestTenantID(r)
	svc := h.svc
	if minutes, _ := strconv.Atoi(r.URL.Query().Get("stalled_after_minutes")); minutes > 0 {
		cfg := h.svc.Config()
		cfg.StalledAfter = time.Duration(minutes) * time.Minute
		svc = NewService(h.svc.collabRepo, cfg)
		svc.SetAgentRuntime(h.svc.agentRuntime)
	}
	result, err := svc.CheckTenant(tid, time.Now().UTC())
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, result)
}

func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	if h.monitor == nil {
		response.OK(w, MonitorStatus{Config: configStatus(h.svc)})
		return
	}
	response.OK(w, h.monitor.Status())
}

func configStatus(svc *Service) MonitorConfigStatus {
	if svc == nil {
		return MonitorConfigStatus{}
	}
	cfg := svc.Config()
	return MonitorConfigStatus{TickIntervalSeconds: int64(cfg.TickInterval.Seconds()), StalledAfterSeconds: int64(cfg.StalledAfter.Seconds()), PushCooldownSeconds: int64(cfg.PushCooldown.Seconds()), WorkersPerShard: cfg.WorkersPerShard, MaxWatchers: cfg.MaxWatchers}
}

func requestTenantID(r *http.Request) string {
	return tenant.RequestTenantID(r)
}
