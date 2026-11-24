package cloudagent

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/remotecontrol"
)

// ViewerFunc authenticates the GUI. Production uses the viewer bearer.
type ViewerFunc func(r *http.Request) (*auth.ViewerPrincipal, error)

// UserHandler is the user REST surface.
type UserHandler struct {
	Svc    *Service
	RC     *remotecontrol.Service
	Viewer ViewerFunc
}

func (h UserHandler) open(w http.ResponseWriter, r *http.Request) (*auth.ViewerPrincipal, string, bool) {
	if h.Viewer == nil {
		writeErr(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
		return nil, "", false
	}
	principal, err := h.Viewer(r)
	if err != nil || principal == nil || strings.TrimSpace(principal.UserID) == "" {
		writeErr(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
		return nil, "", false
	}
	machineID := strings.TrimSpace(r.Header.Get("X-Machine-Id"))
	if machineID == "" {
		machineID = strings.TrimSpace(r.URL.Query().Get("machine_id"))
	}
	if machineID == "" {
		writeErr(w, http.StatusBadRequest, "INVALID", "云端控制需要登录这台电脑")
		return nil, "", false
	}
	return principal, machineID, true
}

func (h UserHandler) List(w http.ResponseWriter, r *http.Request) {
	principal, _, ok := h.open(w, r)
	if !ok {
		return
	}
	items, err := h.Svc.List(r.Context(), principal.TenantID, principal.UserID)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	// The list query is already closed. EffectiveLimits must not run while a
	// cursor is open: tests share one connection.
	_, _, enabled, err := h.Svc.EffectiveLimits(r.Context(), principal.TenantID, principal.UserID)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"instances": items, "enabled": enabled})
}

func (h UserHandler) Create(w http.ResponseWriter, r *http.Request) {
	principal, _, ok := h.open(w, r)
	if !ok {
		return
	}
	var in CreateInput
	if !decodeBody(w, r, &in) {
		return
	}
	inst, err := h.Svc.Create(r.Context(), principal.TenantID, principal.UserID, in)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, inst)
}

func (h UserHandler) Get(w http.ResponseWriter, r *http.Request) {
	principal, _, ok := h.open(w, r)
	if !ok {
		return
	}
	inst, err := h.Svc.Get(r.Context(), principal.TenantID, principal.UserID, r.PathValue("id"))
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, inst)
}

func (h UserHandler) Patch(w http.ResponseWriter, r *http.Request) {
	principal, _, ok := h.open(w, r)
	if !ok {
		return
	}
	var in PatchInput
	if !decodeBody(w, r, &in) {
		return
	}
	inst, err := h.Svc.Patch(r.Context(), principal.TenantID, principal.UserID, r.PathValue("id"), in)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, inst)
}

func (h UserHandler) Delete(w http.ResponseWriter, r *http.Request) {
	principal, _, ok := h.open(w, r)
	if !ok {
		return
	}
	if err := h.Svc.Delete(r.Context(), principal.TenantID, principal.UserID, r.PathValue("id")); err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h UserHandler) SetLLM(w http.ResponseWriter, r *http.Request) {
	principal, _, ok := h.open(w, r)
	if !ok {
		return
	}
	var in CustomLLM
	if !decodeBody(w, r, &in) {
		return
	}
	inst, err := h.Svc.SetCustomLLM(r.Context(), principal.TenantID, principal.UserID, r.PathValue("id"), in, hubHosts(r))
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, inst)
}

func (h UserHandler) ConnectGit(w http.ResponseWriter, r *http.Request) {
	principal, _, ok := h.open(w, r)
	if !ok {
		return
	}
	var in GitConnect
	if !decodeBody(w, r, &in) {
		return
	}
	view, err := h.Svc.ConnectGit(r.Context(), principal.TenantID, principal.UserID, r.PathValue("id"), in)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h UserHandler) ReadGit(w http.ResponseWriter, r *http.Request) {
	principal, _, ok := h.open(w, r)
	if !ok {
		return
	}
	view, err := h.Svc.ReadGit(r.Context(), principal.TenantID, principal.UserID, r.PathValue("id"))
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h UserHandler) DisconnectGit(w http.ResponseWriter, r *http.Request) {
	principal, _, ok := h.open(w, r)
	if !ok {
		return
	}
	if err := h.Svc.DisconnectGit(r.Context(), principal.TenantID, principal.UserID, r.PathValue("id")); err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h UserHandler) Stop(w http.ResponseWriter, r *http.Request) {
	principal, _, ok := h.open(w, r)
	if !ok {
		return
	}
	inst, err := h.Svc.Stop(r.Context(), principal.TenantID, principal.UserID, r.PathValue("id"))
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, inst)
}

func (h UserHandler) Resume(w http.ResponseWriter, r *http.Request) {
	principal, _, ok := h.open(w, r)
	if !ok {
		return
	}
	inst, err := h.Svc.Resume(r.Context(), principal.TenantID, principal.UserID, r.PathValue("id"))
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, inst)
}

func (h UserHandler) ControlGrant(w http.ResponseWriter, r *http.Request) {
	principal, machineID, ok := h.open(w, r)
	if !ok {
		return
	}
	_, _, enabled, err := h.Svc.EffectiveLimits(r.Context(), principal.TenantID, principal.UserID)
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	if !enabled {
		writeServiceErr(w, failDisabled())
		return
	}
	inst, err := h.Svc.getOwned(r.Context(), principal.TenantID, principal.UserID, r.PathValue("id"))
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	if h.RC == nil {
		writeErr(w, http.StatusServiceUnavailable, "OFFLINE", "云端运行时不可用")
		return
	}
	grant, err := h.RC.EnsureCloudGrant(r.Context(), principal.TenantID, principal.UserID, machineID, inst.ID, inst.OwnerUserID)
	if err != nil {
		var status *remotecontrol.StatusError
		if errors.As(err, &status) {
			code := http.StatusBadRequest
			if status.Code == "FORBIDDEN" {
				code = http.StatusForbidden
			}
			writeErr(w, code, status.Code, status.Message)
			return
		}
		writeErr(w, http.StatusBadRequest, "REMOTE_CONTROL", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"grant_id": grant.ID})
}

// ServeHeartbeat is the loopback heartbeat from maclawsrv.
func (s *Service) ServeHeartbeat(w http.ResponseWriter, r *http.Request) {
	if s == nil {
		writeErr(w, http.StatusServiceUnavailable, "OFFLINE", "云端运行时不可用")
		return
	}
	var body struct {
		InstanceID string `json:"instance_id"`
		TaskID     string `json:"task_id"`
		RequestID  string `json:"request_id"`
		Finished   bool   `json:"finished"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID", "心跳无效")
		return
	}
	inst, err := s.getByID(r.Context(), body.InstanceID)
	if err != nil {
		// A remote caller gets no 404-vs-401 distinction: probing whether
		// an instance id exists is not information it should have.
		if !LoopbackAddr(r.RemoteAddr) {
			writeErr(w, http.StatusUnauthorized, "UNAUTHORIZED", "管理凭证无效")
			return
		}
		writeServiceErr(w, err)
		return
	}
	// A public-connection heartbeat still must be loopback. A node srv
	// calls in over the open network, so its admin secret is the gate.
	isNode, authed := s.SrvAuth(r.Context(), inst.TenantID, inst.SrvNodeID, r.Header.Get("X-MaClaw-Admin-Secret"))
	if !isNode && !LoopbackAddr(r.RemoteAddr) {
		writeErr(w, http.StatusForbidden, "FORBIDDEN", "云端心跳只接受本机")
		return
	}
	if !authed {
		writeErr(w, http.StatusUnauthorized, "UNAUTHORIZED", "管理凭证无效")
		return
	}
	if body.Finished {
		if err := s.FinishTurn(r.Context(), inst.ID, body.TaskID, body.RequestID, s.now()); err != nil {
			writeServiceErr(w, err)
			return
		}
		_, _, enabled, err := s.EffectiveLimits(r.Context(), inst.TenantID, inst.OwnerUserID)
		if err != nil {
			writeServiceErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "enabled": enabled})
		return
	}
	enabled, err := s.TouchHeartbeat(r.Context(), inst.ID, body.TaskID, body.RequestID, s.now())
	if err != nil {
		writeServiceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "enabled": enabled})
}

func hubHosts(r *http.Request) []string {
	var hosts []string
	if r != nil {
		hosts = append(hosts, r.Host)
		if origin := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); origin != "" {
			hosts = append(hosts, origin)
		}
	}
	if raw := strings.TrimSpace(os.Getenv("MACLAW_HUB_URL")); raw != "" {
		hosts = append(hosts, raw)
	}
	return hosts
}

func decodeBody(w http.ResponseWriter, r *http.Request, dest any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(dest); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID", "请求无效")
		return false
	}
	return true
}

func writeServiceErr(w http.ResponseWriter, err error) {
	var failure *Failure
	if errors.As(err, &failure) {
		writeErr(w, failure.HTTP, failure.Code, failure.Message)
		return
	}
	writeErr(w, http.StatusInternalServerError, "CLOUD_AGENT", "云端管理暂时不可用")
}

func writeErr(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"ok": false, "code": code, "message": message})
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
