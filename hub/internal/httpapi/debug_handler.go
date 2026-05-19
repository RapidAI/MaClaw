package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/session"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type machineUserLookup interface {
	GetByID(ctx context.Context, id string) (*store.User, error)
	GetByTenantEmail(ctx context.Context, tenantID, email string) (*store.User, error)
}

type machineListItem struct {
	device.MachineRuntimeInfo
	UserEmail string `json:"user_email,omitempty"`
}

func DebugListMachinesHandler(devices *device.Service, users machineUserLookup) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
		email := strings.TrimSpace(r.URL.Query().Get("email"))
		all := strings.TrimSpace(r.URL.Query().Get("all"))

		tenantID := RequestTenantID(r)

		// resolve email to user_id if provided
		if email != "" && userID == "" && users != nil {
			user, err := users.GetByTenantEmail(r.Context(), tenantID, email)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
				return
			}
			if user == nil {
				writeJSON(w, http.StatusOK, map[string]any{"machines": []machineListItem{}})
				return
			}
			items, err := devices.ListMachines(r.Context(), user.ID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
				return
			}
			enriched := enrichMachineList(r.Context(), items, users)
			writeJSON(w, http.StatusOK, map[string]any{
				"machines": enriched,
			})
			return
		}

		if userID != "" {
			if !canAccessUser(r.Context(), r, users, userID) {
				writeError(w, http.StatusForbidden, "FORBIDDEN", "user is outside current tenant")
				return
			}
			items, err := devices.ListMachines(r.Context(), userID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
				return
			}

			writeJSON(w, http.StatusOK, map[string]any{
				"machines": enrichMachineList(r.Context(), items, users),
			})
			return
		}

		if all == "1" || all == "true" || isTenantScopedAdminRequest(r) {
			var items []device.MachineRuntimeInfo
			var err error
			if isTenantScopedAdminRequest(r) {
				items, err = devices.ListMachinesByTenant(r.Context(), tenantID)
			} else {
				items, err = devices.ListAllMachines(r.Context())
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"machines": enrichMachineList(r.Context(), items, users),
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"machines": enrichMachineList(r.Context(), devices.ListOnlineMachines(), users),
		})
	}
}

func enrichMachineList(ctx context.Context, items []device.MachineRuntimeInfo, users machineUserLookup) []machineListItem {
	if len(items) == 0 {
		return []machineListItem{}
	}
	out := make([]machineListItem, 0, len(items))
	cache := map[string]string{}
	for _, item := range items {
		enriched := machineListItem{MachineRuntimeInfo: item}
		if users != nil && strings.TrimSpace(item.UserID) != "" {
			if email, ok := cache[item.UserID]; ok {
				enriched.UserEmail = email
			} else if user, err := users.GetByID(ctx, item.UserID); err == nil && user != nil {
				enriched.UserEmail = strings.TrimSpace(user.Email)
				cache[item.UserID] = enriched.UserEmail
			}
		}
		out = append(out, enriched)
	}
	return out
}

func DebugListMachineEventsHandler(devices *device.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		events := devices.ListEvents(100)
		if isTenantScopedAdminRequest(r) {
			events = devices.ListEventsByTenant(RequestTenantID(r), 100)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"events": events,
		})
	}
}

func DebugListSessionsHandler(svc *session.Service, users machineUserLookup) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		machineID := strings.TrimSpace(r.URL.Query().Get("machine_id"))
		userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
		if machineID == "" || userID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "machine_id and user_id are required")
			return
		}
		if !canAccessUser(r.Context(), r, users, userID) {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "user is outside current tenant")
			return
		}

		items, err := svc.ListByMachine(r.Context(), userID, machineID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"sessions": items,
		})
	}
}

// AdminListAllSessionsHandler returns all cached sessions across all machines.
// Each entry includes a "source" field ("ai", "desktop", "mobile", "handoff")
// so the admin UI can split them into AI vs Human tabs.
func AdminListAllSessionsHandler(svc *session.Service, users machineUserLookup) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items := svc.ListAll()
		tenantID := RequestTenantID(r)
		tenantScoped := isTenantScopedAdminRequest(r)
		userTenantCache := map[string]string{}
		type sessionItem struct {
			SessionID     string                   `json:"session_id"`
			MachineID     string                   `json:"machine_id"`
			UserID        string                   `json:"user_id"`
			Source        string                   `json:"source"`
			ExecutionMode string                   `json:"execution_mode"`
			Summary       session.SessionSummary   `json:"summary"`
			Preview       session.SessionPreview   `json:"preview"`
			RecentEvents  []session.ImportantEvent `json:"recent_events"`
			HostOnline    bool                     `json:"host_online"`
			UpdatedAt     int64                    `json:"updated_at"`
		}
		out := make([]sessionItem, 0, len(items))
		for _, item := range items {
			if tenantScoped {
				itemTenantID, ok, err := lookupUserTenantID(r.Context(), users, item.UserID, userTenantCache)
				if err != nil {
					writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
					return
				}
				if !ok || itemTenantID != tenantID {
					continue
				}
			}
			out = append(out, sessionItem{
				SessionID:     item.SessionID,
				MachineID:     item.MachineID,
				UserID:        item.UserID,
				Source:        item.Source,
				ExecutionMode: item.ExecutionMode,
				Summary:       item.Summary,
				Preview:       item.Preview,
				RecentEvents:  item.RecentEvents,
				HostOnline:    item.HostOnline,
				UpdatedAt:     item.UpdatedAt.Unix(),
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"sessions": out,
		})
	}
}

func lookupUserTenantID(ctx context.Context, users machineUserLookup, userID string, cache map[string]string) (string, bool, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" || users == nil {
		return "", false, nil
	}
	if tenantID, ok := cache[userID]; ok {
		return tenantID, tenantID != "", nil
	}
	user, err := users.GetByID(ctx, userID)
	if err != nil {
		return "", false, err
	}
	if user == nil || strings.TrimSpace(user.TenantID) == "" {
		cache[userID] = ""
		return "", false, nil
	}
	tenantID := strings.TrimSpace(user.TenantID)
	cache[userID] = tenantID
	return tenantID, true, nil
}

func DebugGetSessionHandler(svc *session.Service, users machineUserLookup) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		machineID := strings.TrimSpace(r.URL.Query().Get("machine_id"))
		userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
		sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
		if machineID == "" || userID == "" || sessionID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "machine_id, user_id and session_id are required")
			return
		}
		if !canAccessUser(r.Context(), r, users, userID) {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "user is outside current tenant")
			return
		}

		item, ok := svc.GetSnapshot(userID, machineID, sessionID)
		if !ok || item == nil {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "session not found")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"session_id":    item.SessionID,
			"machine_id":    item.MachineID,
			"user_id":       item.UserID,
			"source":        item.Source,
			"summary":       item.Summary,
			"preview":       item.Preview,
			"recent_events": item.RecentEvents,
			"host_online":   item.HostOnline,
			"updated_at":    item.UpdatedAt.Unix(),
		})
	}
}

func DeleteMachineHandler(devices *device.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		machineID := strings.TrimSpace(r.URL.Query().Get("machine_id"))
		if machineID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "machine_id is required")
			return
		}
		if !canManageMachine(r.Context(), r, devices, machineID) {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "machine is outside current tenant")
			return
		}
		if err := devices.DeleteMachine(r.Context(), machineID); err != nil {
			writeError(w, http.StatusBadRequest, "DELETE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "machine_id": machineID})
	}
}

func RenameMachineHandler(devices *device.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			MachineID string `json:"machine_id"`
			Alias     string `json:"alias"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		body.MachineID = strings.TrimSpace(body.MachineID)
		body.Alias = strings.TrimSpace(body.Alias)
		if body.MachineID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "machine_id is required")
			return
		}
		if !canManageMachine(r.Context(), r, devices, body.MachineID) {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "machine is outside current tenant")
			return
		}
		if err := devices.RenameMachine(r.Context(), body.MachineID, body.Alias); err != nil {
			writeError(w, http.StatusInternalServerError, "RENAME_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"renamed": true, "machine_id": body.MachineID, "alias": body.Alias})
	}
}

func ClearOfflineMachinesHandler(devices *device.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var count int64
		var err error
		if !isTenantScopedAdminRequest(r) {
			count, err = devices.ClearOfflineMachines(r.Context())
		} else {
			count, err = devices.ClearOfflineMachinesByTenant(r.Context(), RequestTenantID(r))
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CLEAR_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"cleared": count})
	}
}

func DeleteMachinesByEmailHandler(devices *device.Service, users machineUserLookup) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		email := strings.TrimSpace(r.URL.Query().Get("email"))
		if email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "email is required")
			return
		}
		if users == nil {
			writeError(w, http.StatusInternalServerError, "NO_USER_LOOKUP", "user lookup not available")
			return
		}
		user, err := users.GetByTenantEmail(r.Context(), RequestTenantID(r), email)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LOOKUP_FAILED", err.Error())
			return
		}
		if user == nil {
			writeJSON(w, http.StatusOK, map[string]any{"deleted": int64(0)})
			return
		}
		count, err := devices.DeleteMachinesByTenantUser(r.Context(), RequestTenantID(r), user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "DELETE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": count})
	}
}

func ForceDeleteMachinesByEmailHandler(devices *device.Service, users machineUserLookup) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		email := strings.TrimSpace(r.URL.Query().Get("email"))
		if email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "email is required")
			return
		}
		if users == nil {
			writeError(w, http.StatusInternalServerError, "NO_USER_LOOKUP", "user lookup not available")
			return
		}
		user, err := users.GetByTenantEmail(r.Context(), RequestTenantID(r), email)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LOOKUP_FAILED", err.Error())
			return
		}
		if user == nil {
			writeJSON(w, http.StatusOK, map[string]any{"deleted": int64(0)})
			return
		}
		count, err := devices.ForceDeleteMachinesByTenantUser(r.Context(), RequestTenantID(r), user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "DELETE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": count})
	}
}

func canManageMachine(ctx context.Context, r *http.Request, devices *device.Service, machineID string) bool {
	if !isTenantScopedAdminRequest(r) {
		return true
	}
	info, err := devices.GetMachineInfo(ctx, machineID)
	if err != nil || info == nil {
		return false
	}
	return strings.TrimSpace(info.TenantID) == RequestTenantID(r)
}

func canAccessUser(ctx context.Context, r *http.Request, users machineUserLookup, userID string) bool {
	if !isTenantScopedAdminRequest(r) {
		return true
	}
	if users == nil {
		return false
	}
	user, err := users.GetByID(ctx, userID)
	if err != nil || user == nil {
		return false
	}
	return strings.TrimSpace(user.TenantID) == RequestTenantID(r)
}

func isTenantScopedAdminRequest(r *http.Request) bool {
	if r == nil || AdminFromContext(r.Context()) == nil {
		return false
	}
	return !IsGlobalAdmin(r.Context()) || strings.TrimSpace(r.URL.Query().Get("tenant_id")) != ""
}
