package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"html"
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
	Language             string `json:"language,omitempty"`
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
		var enrollOpts []auth.EnrollOption
		if lang := strings.TrimSpace(req.Language); lang != "" {
			enrollOpts = append(enrollOpts, auth.WithLanguage(lang))
		} else if acceptLang := r.Header.Get("Accept-Language"); acceptLang != "" {
			// Fallback: derive language from HTTP Accept-Language header.
			if strings.Contains(acceptLang, "zh") {
				enrollOpts = append(enrollOpts, auth.WithLanguage("zh"))
			} else {
				enrollOpts = append(enrollOpts, auth.WithLanguage("en"))
			}
		}
		resp, err := identity.StartEnrollment(ctx, req.Email, req.MachineName, req.Platform, req.ClientID, req.InvitationCode, enrollOpts...)
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
			case errors.Is(err, auth.ErrEmailDomainNotAllowed):
				writeError(w, http.StatusForbidden, "EMAIL_DOMAIN_NOT_ALLOWED", err.Error())
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
				if heartbeat <= 0 || heartbeat > 3600 {
					heartbeat = 30
				} else if heartbeat < 5 {
					heartbeat = 5
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
			case errors.Is(err, auth.ErrEmailDomainNotAllowed):
				writeError(w, http.StatusForbidden, "EMAIL_DOMAIN_NOT_ALLOWED", err.Error())
			case errors.Is(err, auth.ErrEmailBlocked):
				writeError(w, http.StatusForbidden, "EMAIL_BLOCKED", err.Error())
			case errors.Is(err, auth.ErrInvalidEmail):
				writeError(w, http.StatusBadRequest, "INVALID_EMAIL", err.Error())
			default:
				writeError(w, http.StatusInternalServerError, "EMAIL_REQUEST_FAILED", err.Error())
			}
			return
		}

		setEmailLoginHubCenterURL(r, resp)
		writeJSON(w, http.StatusOK, resp)
	}
}

// VerifyEmailHandler handles GET /api/auth/verify-email?token=xxx.
// It confirms registration email verification only; it does not sign the PWA in.
func VerifyEmailHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimSpace(r.URL.Query().Get("token"))
		if token == "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(verifyEmailPage("验证失败", "链接无效或缺少 token 参数。", "Verification failed", "Invalid link or missing token parameter.", "")))
			return
		}

		code, _, err := identity.ConfirmRegistrationVerification(r.Context(), token)
		if err != nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(verifyEmailPage("验证失败", "链接已过期、已使用，或不是邮箱认证链接。请重新注册或联系管理员。", "Verification failed", "Link expired, already used, or not an email verification link. Please re-register or contact admin.", "")))
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(verifyEmailPage("邮箱验证成功", "您的完整注册奖励额度已激活。认证码如下：", "Email verified successfully", "Your full registration bonus credits are now active. Verification code:", code)))
	}
}

func verifyEmailPage(zhTitle, zhMsg, enTitle, enMsg, code string) string {
	codeBlock := ""
	if code != "" {
		codeBlock = `<div class="code" aria-label="verification code">` + html.EscapeString(code) + `</div>`
	}
	return `<!DOCTYPE html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>MaClaw Hub</title>
	<style>body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;display:flex;justify-content:center;align-items:center;min-height:100vh;margin:0;background:#f6f7fb;color:#172033}
	.card{background:#fff;border:1px solid #e5e7eb;border-radius:12px;padding:40px;box-shadow:0 12px 36px rgba(15,23,42,.08);text-align:center;max-width:460px;margin:24px}
	h1{font-size:24px;margin:0 0 12px}.en{font-size:17px;color:#64748b;margin-top:26px} p{color:#475569;margin:0;line-height:1.6}.code{font-size:34px;font-weight:700;letter-spacing:.18em;background:#f1f5f9;border:1px solid #dbe3ee;border-radius:10px;margin:24px auto 0;padding:16px 20px;width:max-content;max-width:100%;box-sizing:border-box;color:#0f172a}</style></head>
	<body><div class="card"><h1>` + html.EscapeString(zhTitle) + `</h1><p>` + html.EscapeString(zhMsg) + `</p>` + codeBlock + `<h1 class="en">` + html.EscapeString(enTitle) + `</h1><p style="font-size:14px">` + html.EscapeString(enMsg) + `</p></div></body></html>`
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

		hubURL := mobileRequestBaseURL(r)
		hubCenterURL := emailLoginHubCenterURL(r)
		writeJSON(w, http.StatusOK, map[string]any{
			"access_token":  token,
			"expires_in":    30 * 86400,
			"tenant_id":     user.TenantID,
			"hub_url":       hubURL,
			"hubcenter_url": hubCenterURL,
			"hub": map[string]any{
				"base_url": hubURL,
				"url":      hubURL,
			},
			"user": map[string]any{
				"tenant_id": user.TenantID,
				"email":     user.Email,
				"sn":        user.SN,
			},
			"llm": map[string]any{
				"mode": "maclaw_official",
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

		setEmailPollHubCenterURL(r, result)
		writeJSON(w, http.StatusOK, result)
	}
}

func emailLoginHubCenterURL(r *http.Request) string {
	if r == nil {
		return ""
	}
	for _, header := range []string{
		"X-MaClaw-HubCenter-URL",
		"X-HubCenter-URL",
		"X-Forwarded-HubCenter-URL",
	} {
		if value := strings.TrimSpace(r.Header.Get(header)); value != "" {
			return value
		}
	}
	return ""
}

func setEmailLoginHubCenterURL(r *http.Request, result *auth.EmailLoginRequestResult) {
	if result == nil || strings.TrimSpace(result.HubCenterURL) != "" {
		return
	}
	result.HubCenterURL = emailLoginHubCenterURL(r)
}

func setEmailPollHubCenterURL(r *http.Request, result *auth.EmailPollResult) {
	if result == nil || strings.TrimSpace(result.HubCenterURL) != "" {
		return
	}
	result.HubCenterURL = emailLoginHubCenterURL(r)
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
