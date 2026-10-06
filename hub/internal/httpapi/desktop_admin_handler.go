package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
	"github.com/RapidAI/CodeClaw/hub/internal/desktoppool"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type securityDirectory struct {
	users store.UserRepository
	sec   *security.SecurityService
}

func (d securityDirectory) Email(ctx context.Context, userID string) (string, error) {
	if d.users == nil {
		return "", nil
	}
	user, err := d.users.GetByID(ctx, userID)
	if err != nil || user == nil {
		return "", err
	}
	return user.Email, nil
}

func (d securityDirectory) GroupID(ctx context.Context, email string) (string, error) {
	if d.sec == nil {
		return "", nil
	}
	return d.sec.GetUserGroupID(ctx, email)
}

func (d securityDirectory) ParentID(ctx context.Context, groupID string) (string, error) {
	if d.sec == nil {
		return "", nil
	}
	group, err := d.sec.GetGroupByID(ctx, groupID)
	if err != nil || group == nil {
		return "", err
	}
	return group.ParentID, nil
}

// botDesktopControl lets a bot message open the user's cloud desktop.
type botDesktopControl struct {
	pool *desktoppool.Pool
}

func (c botDesktopControl) Open(ctx context.Context, tenantID, userID string) (string, error) {
	if c.pool == nil {
		return "", desktoppool.ErrNotAssigned
	}
	session, err := c.pool.OpenDesktop(ctx, tenantID, userID)
	if err != nil {
		return "", err
	}
	return session.Novnc, nil
}

func (c botDesktopControl) Stop(ctx context.Context, tenantID, userID string) error {
	if c.pool == nil {
		return nil
	}
	_, err := c.pool.StopDesktop(ctx, tenantID, userID)
	return err
}

func desktopTenantID(r *http.Request) string {
	if t := AdminTenantID(r.Context()); t != "" {
		return t
	}
	return store.DefaultTenantID
}

func writeDesktopError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, desktoppool.ErrSettingsUnavailable):
		writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "desktop service store is unavailable")
	case errors.Is(err, desktoppool.ErrNotFound):
		writeError(w, http.StatusNotFound, "DOCKER_SERVICE_NOT_FOUND", "docker service not found")
	case errors.Is(err, desktoppool.ErrNotAssigned):
		writeError(w, http.StatusConflict, "DOCKER_SERVICE_NOT_ASSIGNED", "no docker service is assigned to this user")
	case errors.Is(err, desktoppool.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, "INVALID_DESKTOP_SERVICE", err.Error())
	default:
		writeError(w, http.StatusBadGateway, "DOCKER_SERVICE_REQUEST_FAILED", "docker service rejected the desktop request")
	}
}

func GetDesktopServicesAdminHandler(pool *desktoppool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if pool == nil {
			writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "desktop service store is unavailable")
			return
		}
		view, err := pool.View(r.Context(), desktopTenantID(r))
		if err != nil {
			writeDesktopError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}

func PostDesktopServiceAdminHandler(pool *desktoppool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if pool == nil {
			writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "desktop service store is unavailable")
			return
		}
		var in desktoppool.Server
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_DESKTOP_SERVICE", "invalid desktop service settings")
			return
		}
		view, err := pool.CreateServer(r.Context(), desktopTenantID(r), in)
		if err != nil {
			writeDesktopError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, view)
	}
}

func PatchDesktopServiceAdminHandler(pool *desktoppool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if pool == nil {
			writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "desktop service store is unavailable")
			return
		}
		var in struct {
			Name        string  `json:"name"`
			BaseURL     string  `json:"base_url"`
			AccessToken *string `json:"access_token"`
			Image       string  `json:"image"`
			Memory      string  `json:"memory"`
			CPUs        string  `json:"cpus"`
			ShmSize     string  `json:"shm_size"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_DESKTOP_SERVICE", "invalid desktop service settings")
			return
		}
		token := ""
		if in.AccessToken != nil {
			token = *in.AccessToken
		}
		view, err := pool.UpdateServer(r.Context(), desktopTenantID(r), r.PathValue("id"), desktoppool.Server{
			Name: in.Name, BaseURL: in.BaseURL, AccessToken: token,
			Image: in.Image, Memory: in.Memory, CPUs: in.CPUs, ShmSize: in.ShmSize,
		}, in.AccessToken != nil)
		if err != nil {
			writeDesktopError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}

func DeleteDesktopServiceAdminHandler(pool *desktoppool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if pool == nil {
			writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "desktop service store is unavailable")
			return
		}
		if err := pool.DeleteServer(r.Context(), desktopTenantID(r), r.PathValue("id")); err != nil {
			writeDesktopError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

func PostDesktopAssignmentAdminHandler(pool *desktoppool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if pool == nil {
			writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "desktop service store is unavailable")
			return
		}
		var in desktoppool.Assignment
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_DESKTOP_SERVICE", "invalid desktop service settings")
			return
		}
		item, err := pool.CreateAssignment(r.Context(), desktopTenantID(r), in)
		if err != nil {
			writeDesktopError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, item)
	}
}

func DeleteDesktopAssignmentAdminHandler(pool *desktoppool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if pool == nil {
			writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "desktop service store is unavailable")
			return
		}
		if err := pool.DeleteAssignment(r.Context(), desktopTenantID(r), r.PathValue("id")); err != nil {
			writeDesktopError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

func PostDesktopCreateAdminHandler(pool *desktoppool.Pool) http.HandlerFunc {
	return desktopUserAction(pool, true)
}

func PostDesktopStopAdminHandler(pool *desktoppool.Pool) http.HandlerFunc {
	return desktopUserAction(pool, false)
}

func desktopUserAction(pool *desktoppool.Pool, create bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if pool == nil {
			writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "desktop service store is unavailable")
			return
		}
		var in struct {
			UserID string `json:"user_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_DESKTOP_SERVICE", "invalid desktop service settings")
			return
		}
		var (
			desktop desktoppool.Desktop
			err     error
		)
		if create {
			desktop, err = pool.CreateDesktop(r.Context(), desktopTenantID(r), in.UserID)
		} else {
			desktop, err = pool.StopDesktop(r.Context(), desktopTenantID(r), in.UserID)
		}
		if err != nil {
			writeDesktopError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, desktop)
	}
}

func desktopAPIToken() string {
	return strings.TrimSpace(os.Getenv("MACLAW_DESKTOP_API_TOKEN"))
}

func authorizeDesktopAPI(w http.ResponseWriter, r *http.Request) bool {
	token := desktopAPIToken()
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" || len(got) != len(token) || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
		writeError(w, http.StatusUnauthorized, "DESKTOP_API_UNAUTHORIZED", "desktop api token is required")
		return false
	}
	return true
}

// PostDesktopSessionHandler is called by MaClawSrv. It resolves the user's
// Docker service and returns a CDP URL on that machine.
func PostDesktopSessionHandler(pool *desktoppool.Pool, bots *botmgmt.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if pool == nil {
			writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "desktop service store is unavailable")
			return
		}
		if !authorizeDesktopAPI(w, r) {
			return
		}
		var in struct {
			TenantID string `json:"tenant_id"`
			UserID   string `json:"user_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_DESKTOP_SERVICE", "invalid desktop service settings")
			return
		}
		session, err := pool.OpenDesktop(r.Context(), in.TenantID, in.UserID)
		if err != nil {
			writeDesktopError(w, err)
			return
		}
		if bots != nil {
			bots.NoteDesktopView(in.TenantID, in.UserID, session.Novnc)
		}
		writeJSON(w, http.StatusOK, map[string]string{"cdp_url": session.CDP, "display": session.Display})
	}
}

// PostDesktopStopHandler stops one user's desktop after MaClawSrv finishes the last run.
func PostDesktopStopHandler(pool *desktoppool.Pool, bots *botmgmt.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if pool == nil {
			writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "desktop service store is unavailable")
			return
		}
		if !authorizeDesktopAPI(w, r) {
			return
		}
		var in struct {
			TenantID   string `json:"tenant_id"`
			UserID     string `json:"user_id"`
			InstanceID string `json:"instance_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_DESKTOP_SERVICE", "invalid desktop service settings")
			return
		}
		if bots != nil {
			stopped, err := bots.StopDesktopIfIdle(r.Context(), in.TenantID, in.UserID, in.InstanceID)
			if err != nil {
				writeDesktopError(w, err)
				return
			}
			if !stopped {
				writeJSON(w, http.StatusOK, map[string]string{"status": "kept"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
			return
		}
		desktop, err := pool.StopDesktop(r.Context(), in.TenantID, in.UserID)
		if err != nil {
			writeDesktopError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": desktop.Status, "container": desktop.Container})
	}
}

// PostDesktopHoldHandler records that this instance handed the desktop to the
// person. MaClawSrv calls it before the message reply returns.
func PostDesktopHoldHandler(bots *botmgmt.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if bots == nil {
			writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "desktop service store is unavailable")
			return
		}
		if !authorizeDesktopAPI(w, r) {
			return
		}
		var in struct {
			TenantID   string `json:"tenant_id"`
			UserID     string `json:"user_id"`
			InstanceID string `json:"instance_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_DESKTOP_SERVICE", "invalid desktop service settings")
			return
		}
		if err := bots.HoldDesktop(r.Context(), in.TenantID, in.UserID, in.InstanceID); err != nil {
			switch {
			case errors.Is(err, botmgmt.ErrInvalidInput):
				writeError(w, http.StatusBadRequest, "INVALID_DESKTOP_SERVICE", "instance is required")
			case errors.Is(err, botmgmt.ErrNotFound):
				writeError(w, http.StatusNotFound, "BOT_NOT_FOUND", "bot not found")
			default:
				writeError(w, http.StatusBadGateway, "DOCKER_SERVICE_REQUEST_FAILED", "desktop hold failed")
			}
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// PostDesktopAppHandler forwards an app command from MaClawSrv to the user's Docker service.
func PostDesktopAppHandler(pool *desktoppool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if pool == nil {
			writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "desktop service store is unavailable")
			return
		}
		if !authorizeDesktopAPI(w, r) {
			return
		}
		var in struct {
			TenantID string   `json:"tenant_id"`
			UserID   string   `json:"user_id"`
			Display  string   `json:"display"`
			Args     []string `json:"args"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_DESKTOP_SERVICE", "invalid desktop service settings")
			return
		}
		output, err := pool.RunApp(r.Context(), in.TenantID, in.UserID, in.Display, in.Args)
		if err != nil {
			writeDesktopError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"output": output})
	}
}
