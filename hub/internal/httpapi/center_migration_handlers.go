package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/feishu"
	"github.com/RapidAI/CodeClaw/hub/internal/invitation"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/voiceprint"
)

type centerMigrationRequest struct {
	HubSecretHash string                  `json:"hub_secret_hash"`
	TenantID      string                  `json:"tenant_id,omitempty"`
	Emails        []string                `json:"emails,omitempty"`
	Users         []centerUserDataPackage `json:"users,omitempty"`
}

type centerUserDataPackage struct {
	TenantID string           `json:"tenant_id,omitempty"`
	User     *store.User      `json:"user,omitempty"`
	Machines []*store.Machine `json:"machines,omitempty"`
}

func CenterUserMigrationExportHandler(centerSvc *center.Service, identity *auth.IdentityService, devices *device.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req centerMigrationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if centerSvc == nil || !centerSvc.VerifyHubSecretHash(r.Context(), req.HubSecretHash) {
			writeError(w, http.StatusUnauthorized, "CENTER_UNAUTHORIZED", "Hub Center is not authorized")
			return
		}
		if identity == nil || identity.UsersRepo() == nil {
			writeError(w, http.StatusInternalServerError, "USER_EXPORT_UNAVAILABLE", "User repository is unavailable")
			return
		}
		tenantID := centerMigrationTenantID(req.TenantID)
		emails := req.Emails
		if len(emails) == 0 {
			items, err := identity.ListUsersForTenant(r.Context(), tenantID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "USER_EXPORT_LIST_FAILED", err.Error())
				return
			}
			emails = make([]string, 0, len(items))
			for _, item := range items {
				if item != nil && strings.TrimSpace(item.Email) != "" {
					emails = append(emails, item.Email)
				}
			}
		}
		out := make([]centerUserDataPackage, 0, len(emails))
		for _, rawEmail := range emails {
			email := strings.TrimSpace(strings.ToLower(rawEmail))
			if email == "" {
				continue
			}
			user, err := identity.UsersRepo().GetByTenantEmail(r.Context(), tenantID, email)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "USER_EXPORT_FAILED", err.Error())
				return
			}
			if user == nil {
				continue
			}
			var machines []*store.Machine
			if devices != nil {
				machines, err = devices.ExportMachinesByUser(r.Context(), user.ID)
				if err != nil {
					writeError(w, http.StatusInternalServerError, "MACHINE_EXPORT_FAILED", err.Error())
					return
				}
			}
			out = append(out, centerUserDataPackage{TenantID: tenantID, User: user, Machines: machines})
		}
		writeJSON(w, http.StatusOK, map[string]any{"tenant_id": tenantID, "users": out})
	}
}

func CenterUserMigrationImportHandler(centerSvc *center.Service, identity *auth.IdentityService, devices *device.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req centerMigrationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if centerSvc == nil || !centerSvc.VerifyHubSecretHash(r.Context(), req.HubSecretHash) {
			writeError(w, http.StatusUnauthorized, "CENTER_UNAUTHORIZED", "Hub Center is not authorized")
			return
		}
		if identity == nil || identity.UsersRepo() == nil {
			writeError(w, http.StatusInternalServerError, "USER_IMPORT_UNAVAILABLE", "User repository is unavailable")
			return
		}
		imported := 0
		for _, pkg := range req.Users {
			if pkg.User == nil || strings.TrimSpace(pkg.User.Email) == "" {
				continue
			}
			email := strings.TrimSpace(strings.ToLower(pkg.User.Email))
			tenantID := centerMigrationPackageTenantID(req.TenantID, pkg)
			existing, err := identity.UsersRepo().GetByTenantEmail(r.Context(), tenantID, email)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "USER_IMPORT_LOOKUP_FAILED", err.Error())
				return
			}
			userID := pkg.User.ID
			if existing == nil {
				copy := *pkg.User
				copy.TenantID = tenantID
				copy.Email = email
				if err := identity.UsersRepo().Create(r.Context(), &copy); err != nil {
					writeError(w, http.StatusInternalServerError, "USER_IMPORT_FAILED", err.Error())
					return
				}
				imported++
			} else {
				userID = existing.ID
			}
			if devices != nil {
				for _, machine := range pkg.Machines {
					if machine != nil {
						machine.TenantID = tenantID
					}
				}
				if err := devices.ImportMachines(r.Context(), userID, pkg.Machines); err != nil {
					writeError(w, http.StatusInternalServerError, "MACHINE_IMPORT_FAILED", err.Error())
					return
				}
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "imported": imported})
	}
}

func CenterUserMigrationDeleteHandler(centerSvc *center.Service, identity *auth.IdentityService, devices *device.Service, invitationSvc *invitation.Service, feishuNotifier *feishu.Notifier, imCleaners []IMBindingCleaner, voiceprintSvc *voiceprint.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req centerMigrationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if centerSvc == nil || !centerSvc.VerifyHubSecretHash(r.Context(), req.HubSecretHash) {
			writeError(w, http.StatusUnauthorized, "CENTER_UNAUTHORIZED", "Hub Center is not authorized")
			return
		}
		if identity == nil || identity.UsersRepo() == nil {
			writeError(w, http.StatusInternalServerError, "USER_DELETE_UNAVAILABLE", "User repository is unavailable")
			return
		}
		deleted := 0
		tenantID := centerMigrationTenantID(req.TenantID)
		for _, rawEmail := range req.Emails {
			email := strings.TrimSpace(strings.ToLower(rawEmail))
			if email == "" {
				continue
			}
			user, err := identity.UsersRepo().GetByTenantEmail(r.Context(), tenantID, email)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "USER_DELETE_LOOKUP_FAILED", err.Error())
				return
			}
			if user == nil {
				continue
			}
			if devices != nil {
				if _, err := devices.ForceDeleteMachinesByUser(r.Context(), user.ID); err != nil {
					writeError(w, http.StatusInternalServerError, "DELETE_USER_MACHINES_FAILED", err.Error())
					return
				}
			}
			if voiceprintSvc != nil {
				_, _ = voiceprintSvc.DeleteByUser(voiceprint.WithTenant(r.Context(), tenantID), user.ID)
			}
			if invitationSvc != nil {
				if _, err := invitationSvc.DeleteCodeByTenantEmail(r.Context(), tenantID, user.Email); err != nil {
					writeError(w, http.StatusInternalServerError, "DELETE_USER_INVITES_FAILED", err.Error())
					return
				}
			}
			if feishuNotifier != nil {
				feishuNotifier.RemoveOpenIDForTenant(tenantID, user.Email)
			}
			removeIMBindingsForTenant(imCleaners, tenantID, user.Email)
			if err := identity.UsersRepo().DeleteByTenantEmail(r.Context(), tenantID, user.Email); err != nil {
				writeError(w, http.StatusInternalServerError, "DELETE_USER_FAILED", err.Error())
				return
			}
			deleted++
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": deleted})
	}
}

func centerMigrationTenantID(tenantID string) string {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return store.DefaultTenantID
	}
	return tenantID
}

func centerMigrationPackageTenantID(requestTenantID string, pkg centerUserDataPackage) string {
	if tenantID := strings.TrimSpace(requestTenantID); tenantID != "" {
		return tenantID
	}
	if tenantID := strings.TrimSpace(pkg.TenantID); tenantID != "" {
		return tenantID
	}
	if pkg.User != nil && strings.TrimSpace(pkg.User.TenantID) != "" {
		return strings.TrimSpace(pkg.User.TenantID)
	}
	return store.DefaultTenantID
}
