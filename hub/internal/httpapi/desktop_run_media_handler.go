package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"path"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
	"github.com/RapidAI/CodeClaw/hub/internal/desktoppool"
)

// GetBotRunImagesHandler GET /api/v1/bots/{id}/runs/{runID}/images
func GetBotRunImagesHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		items, err := svc.DesktopRunImages(principal.TenantID, principal.UserID, r.PathValue("id"), r.PathValue("runID"))
		if err != nil {
			writeBotUserError(w, r, principal, err, "")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

// GetBotRunFilesHandler GET /api/v1/bots/{id}/runs/{runID}/files
func GetBotRunFilesHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		items, err := svc.DesktopRunFiles(principal.TenantID, principal.UserID, r.PathValue("id"), r.PathValue("runID"))
		if err != nil {
			writeBotUserError(w, r, principal, err, "")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

// DeleteBotDesktopMediaHandler DELETE /api/v1/bots/{id}/desktop-media
// drops the pictures and files kept for this bot's runs. Clearing the chat
// calls it. Deleting the bot does the same.
func DeleteBotDesktopMediaHandler(svc *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || svc == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "bot settings store is unavailable")
			}
			return
		}
		svc.ForgetDesktopRunMedia(principal.TenantID, principal.UserID, r.PathValue("id"))
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// PostBotDesktopFileHandler POST /api/v1/bots/{id}/desktop-file
// reads one file the person may fetch. It uses the supervisor link op,
// which resolves symlinks and allows only the work tree and Desktop.
// The shared file-bytes read is not this path.
func PostBotDesktopFileHandler(pool *desktoppool.Pool, bots *botmgmt.Service, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok || bots == nil || pool == nil {
			if ok {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "desktop service store is unavailable")
			}
			return
		}
		var in struct {
			Path string `json:"path"`
			// Data/Name carry the bytes when the phone already holds them. A
			// delivered file in a bot chat lives on the bot agent's own working
			// path, which is not a desktop container path.
			Data string `json:"data"`
			Name string `json:"name"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_DESKTOP_SERVICE", "invalid desktop file request")
			return
		}
		if strings.TrimSpace(in.Data) != "" {
			name, mime, data, err := botmgmt.ReadDesktopDeliveredFile(in.Data, in.Name)
			if err != nil {
				writeBotUserError(w, r, principal, err, "")
				return
			}
			// Keep a copy: the run record is pruned, and the desktop path may
			// never be readable again. Later turns fetch by name.
			if err := bots.KeepDeliveredDesktopFile(principal.TenantID, principal.UserID, r.PathValue("id"), name, mime, data); err != nil {
				log.Printf("[bot-desktop-file] keep bot=%s file=%q: %v", r.PathValue("id"), name, err)
			}
			writeJSON(w, http.StatusOK, map[string]string{"name": name, "mime": mime, "data": data})
			return
		}
		name, mime, data, err := bots.ReadDesktopContainerFile(r.Context(), principal.TenantID, principal.UserID, r.PathValue("id"), in.Path, func(ctx context.Context, tenantID, desktopUser, path string) (string, error) {
			return pool.RunFile(ctx, tenantID, desktopUser, "link", path, "", "", "")
		})
		if err != nil {
			// A delivered file's path is the bot agent's own working path
			// (C:/Users/.../tasks/x/workspace/report.pdf), which is not a
			// container path. The bytes kept when any client received them
			// are still readable by file name.
			if kept, ok := bots.ReadDeliveredDesktopFile(principal.TenantID, principal.UserID, r.PathValue("id"), path.Base(strings.ReplaceAll(in.Path, "\\", "/"))); ok {
				writeJSON(w, http.StatusOK, map[string]string{"name": kept.Name, "mime": kept.MIME, "data": kept.Data})
				return
			}
			writeBotUserError(w, r, principal, err, "")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"name": name, "mime": mime, "data": data})
	}
}
