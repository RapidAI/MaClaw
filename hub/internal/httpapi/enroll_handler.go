package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/brand"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/invitation"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
)

type EnrollStartRequest struct {
	Email                string `json:"email"`
	MachineName          string `json:"machine_name"`
	Platform             string `json:"platform"`
	Hostname             string `json:"hostname"`
	Arch                 string `json:"arch"`
	AppVersion           string `json:"app_version"`
	HeartbeatIntervalSec int    `json:"heartbeat_interval_sec"`
	ClientID             string `json:"client_id"`
	InvitationCode       string `json:"invitation_code"`
	TenantID             string `json:"tenant_id"`
	GroupID              string `json:"group_id"`
}

type tenantResolver interface {
	ResolveTenantByEmail(ctx context.Context, email string) (tenantID string, found bool, ambiguous bool, err error)
}

type EmailRequestLoginRequest struct {
	Email string `json:"email"`
}

type EmailConfirmLoginRequest struct {
	Token string `json:"token"`
}

type EmailPollLoginRequest struct {
	PollID string `json:"poll_id"`
}

func EnrollStartHandler(identity *auth.IdentityService, invSvc *invitation.Service, securitySvc *security.SecurityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		var req EnrollStartRequest
		decodeStart := time.Now()
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		log.Printf("[onboarding] EnrollStartHandler decode_request=%s", time.Since(decodeStart))

		if req.Email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Email is required")
			return
		}
		// If client explicitly provides tenant_id (from invitation code routing via HubCenter),
		// use it directly. This is only trusted when paired with an invitation code —
		// the code validation on the target tenant serves as the authorization check.
		var tenantID string
		var err error
		if strings.TrimSpace(req.TenantID) != "" && strings.TrimSpace(req.InvitationCode) != "" {
			tenantID = strings.TrimSpace(req.TenantID)
		} else {
			tenantID, err = tenantIDForEmailRequest(r, identity, req.Email)
			if err != nil {
				writeError(w, http.StatusBadRequest, "TENANT_AMBIGUOUS", err.Error())
				return
			}
		}
		ctx := auth.WithTenant(r.Context(), tenantID)

		enrollStart := time.Now()
		resp, err := identity.StartEnrollment(ctx, req.Email, req.MachineName, req.Platform, req.ClientID, req.InvitationCode)
		log.Printf("[onboarding] EnrollStartHandler start_enrollment=%s email=%s status=%s err=%v", time.Since(enrollStart), req.Email, func() string {
			if resp == nil {
				return ""
			}
			return resp.Status
		}(), err)
		if err != nil {
			switch {
			case errors.Is(err, auth.ErrRoutedToAnotherHub):
				writeError(w, http.StatusConflict, "EMAIL_ROUTED_TO_ANOTHER_HUB", err.Error())
			case errors.Is(err, auth.ErrRegistrationDisabled):
				writeError(w, http.StatusForbidden, "REGISTRATION_DISABLED", err.Error())
			case errors.Is(err, auth.ErrInvitationExpired):
				errResp := map[string]any{
					"ok":      false,
					"code":    "INVITATION_EXPIRED",
					"message": err.Error(),
				}
				if resp != nil && resp.ExpiresAt != "" {
					errResp["expires_at"] = resp.ExpiresAt
				}
				writeJSON(w, http.StatusForbidden, errResp)
			case errors.Is(err, auth.ErrInvitationCodeRequired):
				writeError(w, http.StatusBadRequest, "INVITATION_CODE_REQUIRED", err.Error())
			case errors.Is(err, auth.ErrInvalidInvitationCode):
				writeError(w, http.StatusBadRequest, "INVALID_INVITATION_CODE", err.Error())
			case errors.Is(err, auth.ErrEmailBlocked):
				writeError(w, http.StatusForbidden, "EMAIL_BLOCKED", err.Error())
			case errors.Is(err, auth.ErrInvalidEmail):
				writeError(w, http.StatusBadRequest, "INVALID_EMAIL", err.Error())
			default:
				writeError(w, http.StatusInternalServerError, "ENROLL_FAILED", err.Error())
			}
			return
		}

		respMap := map[string]any{
			"status": resp.Status,
			"brand":  brand.Current().DisplayName,
		}
		if resp.TenantID != "" {
			respMap["tenant_id"] = resp.TenantID
		}
		if resp.TenantName != "" {
			respMap["tenant_name"] = resp.TenantName
		}
		if resp.Message != "" {
			respMap["message"] = resp.Message
		}
		if resp.UserID != "" {
			respMap["user_id"] = resp.UserID
		}
		if resp.Email != "" {
			respMap["email"] = resp.Email
		}
		if resp.SN != "" {
			respMap["sn"] = resp.SN
		}
		if resp.MachineID != "" {
			respMap["machine_id"] = resp.MachineID
		}
		if resp.MachineToken != "" {
			respMap["machine_token"] = resp.MachineToken
		}
		if resp.ViewerToken != "" {
			respMap["viewer_token"] = resp.ViewerToken
		}
		if resp.ExpiresAt != "" {
			respMap["expires_at"] = resp.ExpiresAt
		}

		var enrichMu sync.Mutex
		var enrichWG sync.WaitGroup

		if resp != nil && resp.Status == "approved" && invSvc != nil && req.Email != "" {
			enrichWG.Add(1)
			go func() {
				defer enrichWG.Done()
				vipLookupStart := time.Now()
				if ic, err := invSvc.GetCodeByTenantEmail(ctx, tenantID, req.Email); err == nil && ic != nil {
					enrichMu.Lock()
					respMap["vip_flag"] = ic.VIP
					enrichMu.Unlock()
				}
				log.Printf("[onboarding] EnrollStartHandler vip_lookup=%s", time.Since(vipLookupStart))
			}()
		}

		if securitySvc != nil {
			enrichWG.Add(1)
			go func() {
				defer enrichWG.Done()
				securityReadStart := time.Now()
				settingsStart := time.Now()
				if settings, err := securitySvc.GetSettings(security.WithTenant(ctx, tenantID)); err == nil {
					log.Printf("[onboarding] EnrollStartHandler security_get_settings=%s", time.Since(settingsStart))
					enrichMu.Lock()
					respMap["org_structure_enabled"] = settings.OrgStructureEnabled
					enrichMu.Unlock()
					if settings.OrgStructureEnabled && settings.DefaultGroupID != "" {
						enrichMu.Lock()
						respMap["default_group_id"] = settings.DefaultGroupID
						enrichMu.Unlock()
					}
				} else {
					log.Printf("[onboarding] EnrollStartHandler security_get_settings=%s err=%v", time.Since(settingsStart), err)
				}
				log.Printf("[onboarding] EnrollStartHandler security_enrichment=%s", time.Since(securityReadStart))
			}()
		}

		enrichWG.Wait()

		writeJSON(w, http.StatusOK, respMap)
		log.Printf("[onboarding] EnrollStartHandler respond=%s total=%s", time.Since(start), time.Since(start))

		if resp == nil || resp.Status != "approved" {
			return
		}

		go func(req EnrollStartRequest, resp *auth.EnrollmentResult, tenantID string) {
			bgStart := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			ctx = auth.WithTenant(ctx, tenantID)

			if resp.MachineID != "" {
				metadataStart := time.Now()
				heartbeat := req.HeartbeatIntervalSec
				if heartbeat < 5 || heartbeat > 3600 {
					heartbeat = 10
				}
				if err := identity.UpdateMachineMetadata(ctx, resp.MachineID, auth.MachineMetadata{
					Name:                 req.MachineName,
					Platform:             req.Platform,
					Hostname:             req.Hostname,
					Arch:                 req.Arch,
					AppVersion:           req.AppVersion,
					HeartbeatIntervalSec: heartbeat,
				}); err != nil {
					log.Printf("[enroll] update machine metadata failed for %s: %v", resp.MachineID, err)
				}
				log.Printf("[onboarding] EnrollStartHandler background_metadata=%s", time.Since(metadataStart))
			}

			if securitySvc != nil && req.Email != "" {
				assignStart := time.Now()
				if err := securitySvc.AssignNewUser(security.WithTenant(ctx, tenantID), req.Email, req.GroupID); err != nil {
					log.Printf("[enroll] security group assignment failed for %s: %v", req.Email, err)
				}
				log.Printf("[onboarding] EnrollStartHandler background_assign_user=%s", time.Since(assignStart))
			}

			if invSvc != nil && req.Email != "" {
				vipLookupStart := time.Now()
				if _, err := invSvc.GetCodeByTenantEmail(ctx, tenantID, req.Email); err != nil {
					log.Printf("[enroll] vip lookup skipped for %s: %v", req.Email, err)
				}
				log.Printf("[onboarding] EnrollStartHandler background_vip_lookup=%s", time.Since(vipLookupStart))
			}
			log.Printf("[onboarding] EnrollStartHandler background_total=%s", time.Since(bgStart))
		}(req, resp, tenantID)
	}
}

func EmailRequestLoginHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req EmailRequestLoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}

		if req.Email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Email is required")
			return
		}

		tenantID, err := tenantIDForEmailRequest(r, identity, req.Email)
		if err != nil {
			writeError(w, http.StatusBadRequest, "TENANT_AMBIGUOUS", err.Error())
			return
		}

		ctx := auth.WithTenant(r.Context(), tenantID)
		resp, err := identity.RequestEmailLogin(ctx, req.Email)
		if err != nil {
			switch {
			case errors.Is(err, auth.ErrRoutedToAnotherHub):
				writeError(w, http.StatusConflict, "EMAIL_ROUTED_TO_ANOTHER_HUB", err.Error())
			case errors.Is(err, auth.ErrRegistrationDisabled):
				writeError(w, http.StatusForbidden, "REGISTRATION_DISABLED", err.Error())
			case errors.Is(err, auth.ErrEmailBlocked):
				writeError(w, http.StatusForbidden, "EMAIL_BLOCKED", err.Error())
			case errors.Is(err, auth.ErrInvalidEmail):
				writeError(w, http.StatusBadRequest, "INVALID_EMAIL", err.Error())
			default:
				writeError(w, http.StatusInternalServerError, "EMAIL_REQUEST_FAILED", err.Error())
			}
			return
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

func EmailConfirmLoginHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req EmailConfirmLoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}

		if req.Token == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Token is required")
			return
		}

		token, user, err := identity.ConfirmEmailLogin(r.Context(), req.Token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "LOGIN_CONFIRM_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"access_token": token,
			"expires_in":   30 * 86400,
			"tenant_id":    user.TenantID,
			"user": map[string]any{
				"tenant_id": user.TenantID,
				"email":     user.Email,
				"sn":        user.SN,
			},
		})
	}
}

func EmailPollLoginHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req EmailPollLoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}

		if req.PollID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "poll_id is required")
			return
		}

		result, err := identity.PollEmailLogin(r.Context(), req.PollID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "POLL_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, result)
	}
}

func tenantIDFromClientHint(r *http.Request) string {
	if r == nil {
		return DefaultTenantID
	}
	if tenantID := r.Header.Get("X-Tenant-ID"); tenantID != "" {
		return tenantID
	}
	if tenantID := r.URL.Query().Get("tenant_id"); tenantID != "" {
		return tenantID
	}
	return DefaultTenantID
}

func tenantIDForEmailRequest(r *http.Request, resolver tenantResolver, email string) (string, error) {
	if r == nil {
		return DefaultTenantID, nil
	}
	if tenantID := r.Header.Get("X-Tenant-ID"); tenantID != "" {
		return tenantID, nil
	}
	if tenantID := r.URL.Query().Get("tenant_id"); tenantID != "" {
		return tenantID, nil
	}
	if resolver == nil {
		return DefaultTenantID, nil
	}
	tenantID, found, ambiguous, err := resolver.ResolveTenantByEmail(r.Context(), email)
	if err != nil {
		return DefaultTenantID, err
	}
	if ambiguous {
		return "", errors.New("email is associated with multiple tenants; tenant_id is required")
	}
	if found && tenantID != "" {
		return tenantID, nil
	}
	return DefaultTenantID, nil
}
