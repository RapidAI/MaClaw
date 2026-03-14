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
