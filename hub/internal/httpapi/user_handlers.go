package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/session"
)

type machineCommandSender interface {
	SendToMachine(machineID string, msg any) error
}

type SessionControlRequest struct {
	MachineID string `json:"machine_id"`
	SessionID string `json:"session_id"`
	Text      string `json:"text,omitempty"`
}

type SessionStartRequest struct {
	MachineID   string `json:"machine_id"`
	Tool        string `json:"tool"`
	ProjectID   string `json:"project_id,omitempty"`
	ProjectPath string `json:"project_path,omitempty"`
	PythonEnv   string `json:"python_env,omitempty"`
	UseProxy    *bool  `json:"use_proxy,omitempty"`
	YoloMode    *bool  `json:"yolo_mode,omitempty"`
	AdminMode   *bool  `json:"admin_mode,omitempty"`
}

type viewerMachineDTO struct {
	ID         string `json:"id"`
	MachineID  string `json:"machine_id"`
	UserID     string `json:"user_id,omitempty"`
	Name       string `json:"name,omitempty"`
	Platform   string `json:"platform,omitempty"`
	Status     string `json:"status,omitempty"`
	LastSeenAt string `json:"last_seen_at,omitempty"`
	Role       string `json:"role,omitempty"`
	Online     bool   `json:"online"`
}

type viewerSessionDTO struct {
	ID           string                   `json:"id"`
	SessionID    string                   `json:"session_id"`
	MachineID    string                   `json:"machine_id"`
	UserID       string                   `json:"user_id,omitempty"`
	Summary      session.SessionSummary   `json:"summary"`
	Preview      session.SessionPreview   `json:"preview"`
	RecentEvents []session.ImportantEvent `json:"recent_events"`
	HostOnline   bool                     `json:"host_online"`
	UpdatedAt    int64                    `json:"updated_at"`
}

func authenticateViewerRequest(r *http.Request, identity *auth.IdentityService) (*auth.ViewerPrincipal, error) {
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(authz), "bearer ") {
		return nil, auth.ErrInvalidUserCredentials
	}
	token := strings.TrimSpace(authz[7:])
	return identity.AuthenticateViewer(r.Context(), token)
}

func ListMachinesHandler(identity *auth.IdentityService, devices *device.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}

		items, err := devices.ListMachines(r.Context(), principal.UserID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
			return
		}

		out := make([]viewerMachineDTO, 0, len(items))
		for _, item := range items {
			dto := viewerMachineDTO{
				ID:        item.MachineID,
				MachineID: item.MachineID,
				UserID:    item.UserID,
				Name:      item.Name,
				Platform:  item.Platform,
				Status:    item.Status,
				Role:      item.Role,
				Online:    item.Online,
			}
			if item.LastSeenAt != nil {
				dto.LastSeenAt = item.LastSeenAt.Format(time.RFC3339)
			}
			out = append(out, dto)
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"machines": out,
		})
	}
}

func ListSessionsHandler(identity *auth.IdentityService, sessionSvc *session.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}

		machineID := strings.TrimSpace(r.URL.Query().Get("machine_id"))
		if machineID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "machine_id is required")
			return
		}

		items, err := sessionSvc.ListByMachine(r.Context(), principal.UserID, machineID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
			return
		}

		out := make([]viewerSessionDTO, 0, len(items))
		for _, item := range items {
			out = append(out, viewerSessionDTO{
				ID:           item.SessionID,
				SessionID:    item.SessionID,
				MachineID:    item.MachineID,
				UserID:       item.UserID,
				Summary:      item.Summary,
				Preview:      item.Preview,
				RecentEvents: item.RecentEvents,
				HostOnline:   item.HostOnline,
				UpdatedAt:    item.UpdatedAt.Unix(),
			})
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"sessions": out,
		})
	}
}

func GetSessionHandler(identity *auth.IdentityService, sessionSvc *session.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}

		machineID := strings.TrimSpace(r.URL.Query().Get("machine_id"))
		sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
		if machineID == "" || sessionID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "machine_id and session_id are required")
			return
		}

		item, ok := sessionSvc.GetSnapshot(principal.UserID, machineID, sessionID)
		if !ok || item == nil {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "session not found")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"session_id":    item.SessionID,
			"machine_id":    item.MachineID,
			"user_id":       item.UserID,
			"summary":       item.Summary,
			"preview":       item.Preview,
			"recent_events": item.RecentEvents,
			"host_online":   item.HostOnline,
			"updated_at":    item.UpdatedAt.Unix(),
		})
	}
}

func SessionInputHandler(identity *auth.IdentityService, sessionSvc *session.Service, devices machineCommandSender) http.HandlerFunc {
	return sessionControlHandler(identity, sessionSvc, devices, "session.input")
}

func SessionInterruptHandler(identity *auth.IdentityService, sessionSvc *session.Service, devices machineCommandSender) http.HandlerFunc {
	return sessionControlHandler(identity, sessionSvc, devices, "session.interrupt")
}

func SessionKillHandler(identity *auth.IdentityService, sessionSvc *session.Service, devices machineCommandSender) http.HandlerFunc {
	return sessionControlHandler(identity, sessionSvc, devices, "session.kill")
}

func SessionStartHandler(identity *auth.IdentityService, devices machineCommandSender) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}

		var req SessionStartRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		req.MachineID = strings.TrimSpace(req.MachineID)
		req.Tool = strings.TrimSpace(req.Tool)
		req.ProjectID = strings.TrimSpace(req.ProjectID)
		req.ProjectPath = strings.TrimSpace(req.ProjectPath)
		req.PythonEnv = strings.TrimSpace(req.PythonEnv)
		if req.MachineID == "" || req.Tool == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "machine_id and tool are required")
			return
		}

		payload := map[string]any{
			"tool":         req.Tool,
			"project_id":   req.ProjectID,
			"project_path": req.ProjectPath,
			"python_env":   req.PythonEnv,
		}
		if req.UseProxy != nil {
			payload["use_proxy"] = *req.UseProxy
		}
		if req.YoloMode != nil {
			payload["yolo_mode"] = *req.YoloMode
		}
		if req.AdminMode != nil {
			payload["admin_mode"] = *req.AdminMode
		}

		msg := map[string]any{
			"type":       "session.start",
			"request_id": "http-start-" + req.MachineID + "-" + req.Tool,
			"ts":         time.Now().Unix(),
			"machine_id": req.MachineID,
			"payload":    payload,
		}
		if err := devices.SendToMachine(req.MachineID, msg); err != nil {
			writeError(w, http.StatusConflict, "MACHINE_OFFLINE", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":         true,
			"machine_id": req.MachineID,
			"tool":       req.Tool,
		})
	}
}

func sessionControlHandler(identity *auth.IdentityService, sessionSvc *session.Service, devices machineCommandSender, msgType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}

		var req SessionControlRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		req.MachineID = strings.TrimSpace(req.MachineID)
		req.SessionID = strings.TrimSpace(req.SessionID)
		if req.MachineID == "" || req.SessionID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "machine_id and session_id are required")
			return
		}
		if msgType == "session.input" && strings.TrimSpace(req.Text) == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "text is required")
			return
		}

		item, ok := sessionSvc.GetSnapshot(principal.UserID, req.MachineID, req.SessionID)
		if !ok || item == nil {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "session not found")
			return
		}

		payload := map[string]any{}
		if msgType == "session.input" {
			payload["text"] = req.Text
		}

		msg := map[string]any{
			"type":       msgType,
			"request_id": "http-" + req.SessionID,
			"ts":         time.Now().Unix(),
			"machine_id": req.MachineID,
			"session_id": req.SessionID,
			"payload":    payload,
		}
		if err := devices.SendToMachine(req.MachineID, msg); err != nil {
			writeError(w, http.StatusConflict, "MACHINE_OFFLINE", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":         true,
			"session_id": item.SessionID,
			"machine_id": item.MachineID,
		})
	}
}
