package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/session"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type machineUserLookup interface {
	GetByID(ctx context.Context, id string) (*store.User, error)
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

		// resolve email to user_id if provided
		if email != "" && userID == "" && users != nil {
			items, err := devices.ListAllMachines(r.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
				return
			}
			enriched := enrichMachineList(r.Context(), items, users)
			filtered := make([]machineListItem, 0)
			for _, item := range enriched {
				if strings.EqualFold(item.UserEmail, email) {
					filtered = append(filtered, item)
				}
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"machines": filtered,
			})
			return
		}

		if userID != "" {
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

		if all == "1" || all == "true" {
			items, err := devices.ListAllMachines(r.Context())
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
		writeJSON(w, http.StatusOK, map[string]any{
			"events": devices.ListEvents(100),
		})
	}
}

func DebugListSessionsHandler(svc *session.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		machineID := strings.TrimSpace(r.URL.Query().Get("machine_id"))
		userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
		if machineID == "" || userID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "machine_id and user_id are required")
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

func DebugGetSessionHandler(svc *session.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		machineID := strings.TrimSpace(r.URL.Query().Get("machine_id"))
		userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
		sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
		if machineID == "" || userID == "" || sessionID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "machine_id, user_id and session_id are required")
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
		if err := devices.DeleteMachine(r.Context(), machineID); err != nil {
			writeError(w, http.StatusBadRequest, "DELETE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "machine_id": machineID})
	}
}

func ClearOfflineMachinesHandler(devices *device.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		count, err := devices.ClearOfflineMachines(r.Context())
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
		// find all machines, enrich with email, then collect matching user IDs
		items, err := devices.ListAllMachines(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
			return
		}
		enriched := enrichMachineList(r.Context(), items, users)
		userIDs := map[string]bool{}
		for _, item := range enriched {
			if strings.EqualFold(item.UserEmail, email) {
				userIDs[item.UserID] = true
			}
		}
		if len(userIDs) == 0 {
			writeJSON(w, http.StatusOK, map[string]any{"deleted": int64(0)})
			return
		}
		var total int64
		for uid := range userIDs {
			count, err := devices.DeleteMachinesByUser(r.Context(), uid)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "DELETE_FAILED", err.Error())
				return
			}
			total += count
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": total})
	}
}
