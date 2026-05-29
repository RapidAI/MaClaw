package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/auth"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/entry"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

type publicBaseURLReader interface {
	PublicBaseURL(rctx context.Context) (string, error)
}

type publicBaseURLWriter interface {
	SetPublicBaseURL(rctx context.Context, publicBaseURL string) (string, error)
}

type routingDiagnosticsReader interface {
	RoutingDiagnostics(ctx context.Context) (entry.RoutingDiagnostics, error)
}

type AdminSetupRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email"`
}

type AdminLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type AdminChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type AdminUpdateProfileRequest struct {
	Email string `json:"email"`
}

type AdminServerConfigRequest struct {
	PublicBaseURL string `json:"public_base_url"`
}

func AdminStatusHandler(admins *auth.AdminService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		initialized, err := admins.IsInitialized(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"initialized": initialized,
		})
	}
}

func SetupAdminHandler(admins *auth.AdminService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req AdminSetupRequest
		if err := decodeLimitedJSON(w, r, &req, defaultJSONBodyLimit); err != nil {
			writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.Username == "" || req.Password == "" || req.Email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Username, password, and email are required")
			return
		}

		initialized, err := admins.IsInitialized(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		if initialized {
			writeError(w, http.StatusConflict, "ADMIN_ALREADY_INITIALIZED", "Admin has already been initialized")
			return
		}

		if err := admins.SetupInitialAdmin(r.Context(), req.Username, req.Password, req.Email); err != nil {
			writeError(w, http.StatusInternalServerError, "SETUP_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"message": "MaClaw Hub Center admin initialized",
		})
	}
}

func AdminLoginHandler(admins *auth.AdminService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req AdminLoginRequest
		if err := decodeLimitedJSON(w, r, &req, defaultJSONBodyLimit); err != nil {
			writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.Username == "" || req.Password == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Username and password are required")
			return
		}

		token, admin, err := admins.Login(r.Context(), req.Username, req.Password)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "LOGIN_FAILED", "Invalid username or password")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"access_token": token,
			"expires_in":   7200,
			"admin": map[string]any{
				"username": admin.Username,
				"email":    admin.Email,
			},
		})
	}
}

func AdminChangePasswordHandler(admins *auth.AdminService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		admin := AdminFromContext(r.Context())
		if admin == nil {
			writeError(w, http.StatusUnauthorized, "ADMIN_UNAUTHORIZED", "Admin authorization required")
			return
		}

		var req AdminChangePasswordRequest
		if err := decodeLimitedJSON(w, r, &req, defaultJSONBodyLimit); err != nil {
			writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.CurrentPassword == "" || req.NewPassword == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Current password and new password are required")
			return
		}

		token, updatedAdmin, err := admins.ChangePassword(r.Context(), admin.Username, req.CurrentPassword, req.NewPassword)
		if err != nil {
			if err == auth.ErrInvalidAdminPassword {
				writeError(w, http.StatusUnauthorized, "INVALID_PASSWORD", "Current password is incorrect")
				return
			}
			writeError(w, http.StatusInternalServerError, "CHANGE_PASSWORD_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":           true,
			"access_token": token,
			"admin": map[string]any{
				"username": updatedAdmin.Username,
				"email":    updatedAdmin.Email,
			},
		})
	}
}

func AdminUpdateProfileHandler(admins *auth.AdminService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		admin := AdminFromContext(r.Context())
		if admin == nil {
			writeError(w, http.StatusUnauthorized, "ADMIN_UNAUTHORIZED", "Admin authorization required")
			return
		}

		var req AdminUpdateProfileRequest
		if err := decodeLimitedJSON(w, r, &req, defaultJSONBodyLimit); err != nil {
			writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.Email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Email is required")
			return
		}

		token, updatedAdmin, err := admins.UpdateEmail(r.Context(), admin.Username, req.Email)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "UPDATE_PROFILE_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":           true,
			"access_token": token,
			"admin": map[string]any{
				"username": updatedAdmin.Username,
				"email":    updatedAdmin.Email,
			},
		})
	}
}

func GetAdminServerConfigHandler(hubService publicBaseURLReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		publicBaseURL, err := hubService.PublicBaseURL(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "SERVER_CONFIG_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"public_base_url": publicBaseURL,
		})
	}
}

func UpdateAdminServerConfigHandler(hubService publicBaseURLWriter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req AdminServerConfigRequest
		if err := decodeLimitedJSON(w, r, &req, defaultJSONBodyLimit); err != nil {
			writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
			return
		}
		if req.PublicBaseURL == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Public base URL is required")
			return
		}
		publicBaseURL, err := hubService.SetPublicBaseURL(r.Context(), req.PublicBaseURL)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "SERVER_CONFIG_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":              true,
			"public_base_url": publicBaseURL,
		})
	}
}

func AdminRoutingDiagnosticsHandler(service routingDiagnosticsReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if service == nil {
			writeError(w, http.StatusNotImplemented, "ROUTING_DIAGNOSTICS_UNAVAILABLE", "Routing diagnostics are unavailable")
			return
		}
		diagnostics, err := service.RoutingDiagnostics(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "ROUTING_DIAGNOSTICS_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, diagnostics)
	}
}

type FailureLogView struct {
	ID        string         `json:"id"`
	TenantID  string         `json:"tenant_id"`
	Category  string         `json:"category"`
	EventCode string         `json:"event_code"`
	Message   string         `json:"message"`
	EntityID  string         `json:"entity_id"`
	Email     string         `json:"email"`
	ClientIP  string         `json:"client_ip"`
	Details   map[string]any `json:"details"`
	CreatedAt string         `json:"created_at"`
}

func ListFailureLogsHandler(repo store.FailureEventLogRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if repo == nil {
			writeError(w, http.StatusNotImplemented, "FAILURE_LOGS_UNAVAILABLE", "Failure logs are unavailable")
			return
		}
		limit, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
		offset, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("offset")))
		tenantID, tenantIDSet := adminFailureLogTenantFilter(r)
		items, total, err := repo.List(r.Context(), store.FailureEventLogFilter{
			TenantID:    tenantID,
			TenantIDSet: tenantIDSet,
			Keyword:     strings.TrimSpace(r.URL.Query().Get("keyword")),
			Category:    strings.TrimSpace(r.URL.Query().Get("category")),
			Offset:      offset,
			Limit:       limit,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_FAILURE_LOGS_FAILED", err.Error())
			return
		}
		logs := make([]FailureLogView, 0, len(items))
		for _, item := range items {
			if item == nil {
				continue
			}
			details := map[string]any{}
			if strings.TrimSpace(item.DetailsJSON) != "" {
				_ = json.Unmarshal([]byte(item.DetailsJSON), &details)
			}
			logs = append(logs, FailureLogView{
				ID:        item.ID,
				TenantID:  adminExternalTenantID(item.TenantID),
				Category:  item.Category,
				EventCode: item.EventCode,
				Message:   item.Message,
				EntityID:  item.EntityID,
				Email:     item.Email,
				ClientIP:  item.ClientIP,
				Details:   details,
				CreatedAt: item.CreatedAt.Format(time.RFC3339),
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"logs": logs, "total": total, "offset": offset, "limit": limit})
	}
}

func adminFailureLogTenantFilter(r *http.Request) (string, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	if raw == "" {
		return "", false
	}
	return normalizeHubSyncTenantID(raw), true
}
