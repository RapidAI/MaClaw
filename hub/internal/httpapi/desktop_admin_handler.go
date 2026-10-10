package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
	"github.com/RapidAI/CodeClaw/hub/internal/desktoppool"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
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
	case errors.Is(err, desktoppool.ErrService):
		writeError(w, http.StatusBadGateway, "DOCKER_SERVICE_REQUEST_FAILED", desktoppool.ServiceFailureMessage(err))
	default:
		writeError(w, http.StatusBadGateway, "DOCKER_SERVICE_REQUEST_FAILED", "docker service rejected the desktop request")
	}
}

func GetDesktopServicesAdminHandler(pool *desktoppool.Pool, users store.UserRepository, bots *botmgmt.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if pool == nil {
			writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "desktop service store is unavailable")
			return
		}
		tenantID := desktopTenantID(r)
		// 开通范围 first, then the Docker assignment. A user the Bot entry
		// hides never reaches the department walk the assignment filter does.
		candidates := desktopCandidatesInBotScope(r.Context(), bots, tenantID, desktopUserCandidates(r.Context(), users, tenantID))
		view, err := pool.ViewAuthorized(r.Context(), tenantID, candidates)
		if err != nil {
			writeDesktopError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}

// desktopCandidatesInBotScope keeps users 开通范围 covers. The Docker
// assignment filter still runs afterwards, so the check-desktop list is the
// intersection: enabled in 开通范围, and a desktop call would resolve.
// No grants means the feature is off, so a global Docker assignment does
// not put the whole tenant in the dropdown.
func desktopCandidatesInBotScope(ctx context.Context, bots *botmgmt.Service, tenantID string, candidates []desktoppool.AuthorizedUser) []desktoppool.AuthorizedUser {
	if len(candidates) == 0 {
		return nil
	}
	subjects := make([]botmgmt.GrantSubject, 0, len(candidates))
	byID := make(map[string]desktoppool.AuthorizedUser, len(candidates))
	for _, user := range candidates {
		id := strings.TrimSpace(user.ID)
		if id == "" {
			continue
		}
		if _, ok := byID[id]; ok {
			continue
		}
		user.ID = id
		byID[id] = user
		subjects = append(subjects, botmgmt.GrantSubject{ID: id, Email: strings.TrimSpace(user.Email)})
	}
	granted := bots.FilterGranted(ctx, tenantID, subjects)
	if len(granted) == 0 {
		return nil
	}
	out := make([]desktoppool.AuthorizedUser, 0, len(granted))
	for _, id := range granted {
		user, ok := byID[id]
		if !ok {
			continue
		}
		out = append(out, user)
	}
	return out
}

// desktopUserCandidates turns the tenant's users into filter candidates for
// the pool: real users only, labeled by email when there is one. A repository
// failure yields no candidates, so the panel lists nobody rather than users
// whose desktop calls would fail.
func desktopUserCandidates(ctx context.Context, repo store.UserRepository, tenantID string) []desktoppool.AuthorizedUser {
	if repo == nil {
		return nil
	}
	items, err := repo.ListByTenant(ctx, tenantID)
	if err != nil {
		log.Printf("[admin/desktop-services] list users failed: %v", err)
		return nil
	}
	candidates := make([]desktoppool.AuthorizedUser, 0, len(items))
	for _, user := range items {
		if user == nil {
			continue
		}
		id := strings.TrimSpace(user.ID)
		email := strings.TrimSpace(user.Email)
		if id == "" || llmservice.IsSystemLLMUser(id, email) {
			continue
		}
		label := email
		if label == "" {
			label = id
		}
		candidates = append(candidates, desktoppool.AuthorizedUser{ID: id, Label: label, Email: email})
	}
	return candidates
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

func PostDesktopCreateAdminHandler(pool *desktoppool.Pool, bots *botmgmt.Service) http.HandlerFunc {
	return desktopUserAction(pool, bots, true)
}

func PostDesktopStopAdminHandler(pool *desktoppool.Pool, bots *botmgmt.Service) http.HandlerFunc {
	return desktopUserAction(pool, bots, false)
}

func desktopUserAction(pool *desktoppool.Pool, bots *botmgmt.Service, create bool) http.HandlerFunc {
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
		tenantID := desktopTenantID(r)
		if create {
			desktop, err = pool.CreateDesktop(r.Context(), tenantID, in.UserID)
		} else {
			desktop, err = pool.StopDesktop(r.Context(), tenantID, in.UserID)
		}
		if err != nil {
			writeDesktopError(w, err)
			return
		}
		// The desktop is gone, so the noVNC picture an admin may have opened
		// is gone too. Leaving it behind would hand the chat a dead page.
		if bots != nil && !create {
			bots.ForgetDesktopView(tenantID, in.UserID)
		}
		writeJSON(w, http.StatusOK, desktop)
	}
}

// PostDesktopViewAdminHandler opens (or reuses) the user's desktop and
// returns the Hub noVNC page so an admin can watch that desktop. botmgmt is
// required: it gates the page, and without it the reply would carry the
// Docker host's address and its VNC token straight to the browser.
func PostDesktopViewAdminHandler(pool *desktoppool.Pool, bots *botmgmt.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if pool == nil || bots == nil {
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
		tenantID := desktopTenantID(r)
		session, err := pool.OpenDesktop(r.Context(), tenantID, in.UserID)
		if err != nil {
			writeDesktopError(w, err)
			return
		}
		novnc := strings.TrimSpace(session.Novnc)
		if novnc == "" {
			writeError(w, http.StatusBadGateway, "DESKTOP_NOVNC_UNAVAILABLE", "docker service returned no vnc address")
			return
		}
		bots.NoteDesktopView(tenantID, in.UserID, novnc)
		// The admin is watching this desktop now. A bot command that finishes
		// mid-check must not pull it out from under the open noVNC page.
		bots.NoteDesktopAdminView(tenantID, in.UserID)
		gated := bots.DesktopViewURL(tenantID, in.UserID)
		if gated == "" {
			// Never fall back to the raw address: it points at the Docker
			// host's published port and carries the VNC token as userinfo.
			writeError(w, http.StatusBadGateway, "DESKTOP_NOVNC_UNAVAILABLE", "hub could not publish a vnc address")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"novnc_url": gated})
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

// PostDesktopOpenHandler forwards a request to start one GUI program.
// The program is argv, not a shell line.
func PostDesktopOpenHandler(pool *desktoppool.Pool) http.HandlerFunc {
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
			Program  string   `json:"program"`
			Args     []string `json:"args"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_DESKTOP_SERVICE", "invalid desktop service settings")
			return
		}
		output, err := pool.RunOpen(r.Context(), in.TenantID, in.UserID, in.Display, in.Program, in.Args)
		if err != nil {
			writeDesktopError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"output": output})
	}
}

// PostDesktopInstallHandler forwards a package install from MaClawSrv to the
// user's Docker service. The package list is apt-get argv, not a shell line.
func PostDesktopInstallHandler(pool *desktoppool.Pool) http.HandlerFunc {
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
			Packages []string `json:"packages"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_DESKTOP_SERVICE", "invalid desktop service settings")
			return
		}
		output, err := pool.RunInstall(r.Context(), in.TenantID, in.UserID, in.Packages)
		if err != nil {
			writeDesktopError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"output": output})
	}
}

// PostDesktopFileHandler forwards a container file read, write, edit, list, or bytes fetch.
func PostDesktopFileHandler(pool *desktoppool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if pool == nil {
			writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "desktop service store is unavailable")
			return
		}
		if !authorizeDesktopAPI(w, r) {
			return
		}
		var in struct {
			TenantID  string `json:"tenant_id"`
			UserID    string `json:"user_id"`
			Action    string `json:"action"`
			Path      string `json:"path"`
			Content   string `json:"content"`
			OldString string `json:"old_string"`
			NewString string `json:"new_string"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_DESKTOP_SERVICE", "invalid desktop service settings")
			return
		}
		output, err := pool.RunFile(r.Context(), in.TenantID, in.UserID, in.Action, in.Path, in.Content, in.OldString, in.NewString)
		if err != nil {
			writeDesktopError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"output": output})
	}
}

// PostDesktopBashHandler runs one command in the user's desktop container.
func PostDesktopBashHandler(pool *desktoppool.Pool) http.HandlerFunc {
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
			Command  string `json:"command"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_DESKTOP_SERVICE", "invalid desktop service settings")
			return
		}
		output, err := pool.RunBash(r.Context(), in.TenantID, in.UserID, in.Command)
		if err != nil {
			writeDesktopError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"output": output})
	}
}

// PostDesktopHTTPHandler fetches one URL through the desktop container's network.
func PostDesktopHTTPHandler(pool *desktoppool.Pool) http.HandlerFunc {
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
			URL      string `json:"url"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_DESKTOP_SERVICE", "invalid desktop service settings")
			return
		}
		body, status, kind, err := pool.RunHTTP(r.Context(), in.TenantID, in.UserID, in.URL)
		if err != nil {
			writeDesktopError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"body": body, "status": status, "content_type": kind})
	}
}

// PostDesktopScreenshotHandler returns a PNG of the user's cloud desktop for
// MaClawSrv's desktop tool. It uses the same desktop API token as /app, and
// the image travels base64-encoded in JSON like the other desktop replies.
func PostDesktopScreenshotHandler(pool *desktoppool.Pool) http.HandlerFunc {
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
			Display  string `json:"display"`
			Name     string `json:"name"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_DESKTOP_SERVICE", "invalid desktop service settings")
			return
		}
		png, saved, err := pool.Screenshot(r.Context(), in.TenantID, in.UserID, in.Display, in.Name)
		if err != nil {
			writeDesktopError(w, err)
			return
		}
		body := map[string]any{
			"mime":         "image/png",
			"image_base64": base64.StdEncoding.EncodeToString(png),
			"bytes":        len(png),
		}
		if saved != "" {
			body["saved_path"] = saved
		}
		writeJSON(w, http.StatusOK, body)
	}
}
