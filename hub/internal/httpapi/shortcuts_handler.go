package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const (
	maxShortcuts   = 50
	maxNameLen     = 100
	maxCmdLen      = 500
	maxBodyBytes   = 32 * 1024 // 32 KB
)

type shortcutItem struct {
	Name string `json:"name"`
	Cmd  string `json:"cmd"`
}

func shortcutsKey(userID string) string {
	return fmt.Sprintf("shortcuts_%s", userID)
}

// GetShortcutsHandler returns the viewer's saved shortcuts.
func GetShortcutsHandler(identity *auth.IdentityService, system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}

		raw, err := system.Get(r.Context(), shortcutsKey(principal.UserID))
		if err != nil || raw == "" {
			writeJSON(w, http.StatusOK, map[string]any{"shortcuts": []shortcutItem{}})
			return
		}

		var items []shortcutItem
		if err := json.Unmarshal([]byte(raw), &items); err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"shortcuts": []shortcutItem{}})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"shortcuts": items})
	}
}

// PutShortcutsHandler replaces the viewer's shortcuts list.
func PutShortcutsHandler(identity *auth.IdentityService, system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}

		body := http.MaxBytesReader(w, r.Body, maxBodyBytes)
		defer body.Close()

		var req struct {
			Shortcuts []shortcutItem `json:"shortcuts"`
		}
		if err := json.NewDecoder(body).Decode(&req); err != nil {
			if err == io.EOF {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Empty request body")
			} else {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON body")
			}
			return
		}

		if len(req.Shortcuts) > maxShortcuts {
			writeError(w, http.StatusBadRequest, "TOO_MANY", fmt.Sprintf("Maximum %d shortcuts allowed", maxShortcuts))
			return
		}

		clean := make([]shortcutItem, 0, len(req.Shortcuts))
		for _, item := range req.Shortcuts {
			if item.Name == "" || item.Cmd == "" {
				continue
			}
			// Truncate oversized fields
			if len(item.Name) > maxNameLen {
				item.Name = item.Name[:maxNameLen]
			}
			if len(item.Cmd) > maxCmdLen {
				item.Cmd = item.Cmd[:maxCmdLen]
			}
			clean = append(clean, item)
		}

		data, err := json.Marshal(clean)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "MARSHAL_FAILED", "Failed to encode shortcuts")
			return
		}
		if err := system.Set(r.Context(), shortcutsKey(principal.UserID), string(data)); err != nil {
			writeError(w, http.StatusInternalServerError, "SAVE_FAILED", "Failed to save shortcuts")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "shortcuts": clean})
	}
}
