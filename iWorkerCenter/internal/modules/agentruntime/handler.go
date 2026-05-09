package agentruntime

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/shared/response"
)

const maxHeartbeatBodyBytes = 64 << 10

var (
	errHeartbeatJSONTooLarge = errors.New("heartbeat json body exceeds size limit")
	errHeartbeatJSONTrailing = errors.New("heartbeat json body contains trailing data")
)

type Handler struct {
	svc         *Service
	onHeartbeat func()
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) SetHeartbeatObserver(observer func()) {
	if h == nil {
		return
	}
	h.onHeartbeat = observer
}

func (h *Handler) RegisterRuntimeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/runtime/iworker/instances/heartbeat", h.handleHeartbeat)
}

func (h *Handler) RegisterClientRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/client/iworker/instances", h.handleList)
}

func (h *Handler) RegisterAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/iworker/instances", h.handleList)
}

func (h *Handler) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	var req HeartbeatRequest
	if err := decodeHeartbeatJSON(r.Body, &req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}
	result, err := h.svc.Heartbeat(requestTenantID(r), req, time.Now().UTC())
	if err != nil {
		response.BadRequest(w, "HEARTBEAT_FAILED", err.Error())
		return
	}
	if h.onHeartbeat != nil {
		go h.onHeartbeat()
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

func decodeHeartbeatJSON(body io.Reader, dst any) error {
	data, err := io.ReadAll(io.LimitReader(body, maxHeartbeatBodyBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxHeartbeatBodyBytes {
		return errHeartbeatJSONTooLarge
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return errHeartbeatJSONTrailing
		}
		return err
	}
	return nil
}
