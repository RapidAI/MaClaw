package ve

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/shared/response"
)

const maxVEJSONBodyBytes = 64 << 10 // 64KB

// requestMachineID extracts the machine ID from the request.
// It checks X-Machine-ID header, then query parameter, then falls back to empty.
func requestMachineID(r *http.Request) string {
	if mid := r.Header.Get("X-Machine-ID"); mid != "" {
		return strings.TrimSpace(mid)
	}
	if mid := r.URL.Query().Get("machine_id"); mid != "" {
		return strings.TrimSpace(mid)
	}
	return ""
}

// Handler provides HTTP endpoints for virtual employee management.
type Handler struct {
	registry    *Registry
	authHandler *AuthHandler
	presence    *PresenceManager
	groupConfig *GroupConfig
}

// GroupConfig holds hub-level VE configuration.
type GroupConfig struct {
	MaxGroupParticipants int `json:"max_group_participants"` // 1-10, default 5
}

// NewHandler creates a new VE HTTP handler.
func NewHandler(registry *Registry, authHandler *AuthHandler, presence *PresenceManager) *Handler {
	return &Handler{
		registry:    registry,
		authHandler: authHandler,
		presence:    presence,
		groupConfig: &GroupConfig{MaxGroupParticipants: 5},
	}
}

// RegisterAdminRoutes registers admin-facing routes for VE management.
func (h *Handler) RegisterAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/ve/list", h.handleAdminList)
	mux.HandleFunc("/api/ve/config", h.handleAdminConfig)
	mux.HandleFunc("/api/ve/", h.handleAdminAction) // /api/ve/{id}/approve|reject|disable
}

// RegisterClientRoutes registers client-facing routes for VE operations.
func (h *Handler) RegisterClientRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/ve/register", h.handleClientRegister)
	mux.HandleFunc("/api/ve/status", h.handleClientStatus)
	mux.HandleFunc("/api/ve/settings", h.handleClientSettings)
	mux.HandleFunc("/api/ve/discoverable", h.handleClientDiscoverable)
	mux.HandleFunc("/api/ve/auth/respond", h.handleClientAuthRespond)
}

// --- Admin Endpoints ---

func (h *Handler) handleAdminList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	all := h.registry.ListAll()
	quota := h.registry.quotaStore.GetEffectiveQuota()
	activeCount := h.registry.ActiveCount()

	response.OK(w, map[string]any{
		"employees":    all,
		"active_count": activeCount,
		"quota":        quota,
		"group_config": h.groupConfig,
	})
}

func (h *Handler) handleAdminConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		response.OK(w, h.groupConfig)
	case http.MethodPut:
		var cfg GroupConfig
		if !decodeVEJSON(w, r, &cfg) {
			return
		}
		if cfg.MaxGroupParticipants < 1 || cfg.MaxGroupParticipants > 10 {
			response.BadRequest(w, "INVALID_CONFIG", "max_group_participants must be 1-10")
			return
		}
		h.groupConfig.MaxGroupParticipants = cfg.MaxGroupParticipants
		response.OK(w, h.groupConfig)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or PUT")
	}
}

func (h *Handler) handleAdminAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}

	// Parse /api/ve/{id}/{action}
	path := strings.TrimPrefix(r.URL.Path, "/api/ve/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 {
		response.BadRequest(w, "INVALID_PATH", "expected /api/ve/{id}/{action}")
		return
	}
	veID, action := parts[0], parts[1]

	switch action {
	case "approve":
		if err := h.registry.Approve(veID); err != nil {
			if isQuotaErr(err) {
				response.Error(w, http.StatusConflict, "QUOTA_EXCEEDED", err.Error())
			} else {
				response.BadRequest(w, "APPROVE_FAILED", err.Error())
			}
			return
		}
		response.OK(w, map[string]string{"status": "approved"})

	case "reject":
		var body struct {
			Reason string `json:"reason"`
		}
		decodeVEJSON(w, r, &body) // optional body
		if err := h.registry.Reject(veID, body.Reason); err != nil {
			response.BadRequest(w, "REJECT_FAILED", err.Error())
			return
		}
		response.OK(w, map[string]string{"status": "rejected"})

	case "disable":
		if err := h.registry.Disable(veID); err != nil {
			response.BadRequest(w, "DISABLE_FAILED", err.Error())
			return
		}
		response.OK(w, map[string]string{"status": "disabled"})

	default:
		response.NotFound(w, "ACTION_NOT_FOUND", fmt.Sprintf("unknown action: %s", action))
	}
}

// --- Client Endpoints ---

func (h *Handler) handleClientRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}

	var req VERegistrationRequest
	if !decodeVEJSON(w, r, &req) {
		return
	}

	// Fill owner machine ID from auth context
	machineID := requestMachineID(r)
	if machineID != "" {
		req.OwnerMachineID = machineID
	}

	ve, err := h.registry.Register(req)
	if err != nil {
		if isQuotaErr(err) {
			response.Error(w, http.StatusConflict, "QUOTA_EXCEEDED", err.Error())
		} else {
			response.BadRequest(w, "REGISTER_FAILED", err.Error())
		}
		return
	}
	response.Created(w, ve)
}

func (h *Handler) handleClientStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}

	machineID := requestMachineID(r)
	if machineID == "" {
		response.BadRequest(w, "MISSING_MACHINE_ID", "machine_id not found in request context")
		return
	}

	ve, ok := h.registry.GetByOwner(machineID)
	if !ok {
		response.OK(w, map[string]any{"registered": false})
		return
	}
	response.OK(w, map[string]any{"registered": true, "employee": ve})
}

func (h *Handler) handleClientSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use PUT")
		return
	}

	machineID := requestMachineID(r)
	ve, ok := h.registry.GetByOwner(machineID)
	if !ok {
		response.NotFound(w, "NOT_REGISTERED", "no virtual employee registration found for this machine")
		return
	}

	var body struct {
		Name         string       `json:"name"`
		SkillDesc    string       `json:"skill_description"`
		AccessPolicy AccessPolicy `json:"access_policy"`
		Whitelist    []string     `json:"whitelist"`
		Blacklist    []string     `json:"blacklist"`
	}
	if !decodeVEJSON(w, r, &body) {
		return
	}

	if err := h.registry.UpdateSettings(ve.ID, body.Name, body.SkillDesc, body.AccessPolicy, body.Whitelist, body.Blacklist); err != nil {
		response.BadRequest(w, "UPDATE_FAILED", err.Error())
		return
	}

	updated, _ := h.registry.GetByID(ve.ID)
	response.OK(w, updated)
}

func (h *Handler) handleClientDiscoverable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}

	machineID := requestMachineID(r)
	list := h.registry.ListDiscoverable(machineID)

	// Enrich with online status from presence manager
	for _, ve := range list {
		ve.OnlineStatus = h.presence.GetStatus(ve.ID)
	}

	response.OK(w, map[string]any{
		"employees":            list,
		"max_group_participants": h.groupConfig.MaxGroupParticipants,
	})
}

func (h *Handler) handleClientAuthRespond(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}

	var resp AuthorizationResponse
	if !decodeVEJSON(w, r, &resp) {
		return
	}

	if err := h.authHandler.HandleResponse(resp); err != nil {
		response.BadRequest(w, "AUTH_RESPOND_FAILED", err.Error())
		return
	}
	response.OK(w, map[string]string{"status": "ok"})
}

// GetGroupConfig returns the current group configuration.
func (h *Handler) GetGroupConfig() *GroupConfig {
	return h.groupConfig
}

// --- Helpers ---

func decodeVEJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	data, err := io.ReadAll(io.LimitReader(r.Body, maxVEJSONBodyBytes+1))
	if err != nil {
		response.BadRequest(w, "READ_FAILED", "failed to read request body")
		return false
	}
	if len(data) > maxVEJSONBodyBytes {
		response.BadRequest(w, "BODY_TOO_LARGE", "request body exceeds 64KB limit")
		return false
	}
	if len(data) == 0 {
		return true // empty body is OK for optional fields
	}
	if err := json.Unmarshal(data, out); err != nil {
		response.BadRequest(w, "INVALID_JSON", "invalid JSON body")
		return false
	}
	return true
}

func isQuotaErr(err error) bool {
	_, ok := err.(*QuotaExceededError)
	return ok
}
