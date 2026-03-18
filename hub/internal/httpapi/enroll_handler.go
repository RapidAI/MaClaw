package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/feishu"
)

type EnrollStartRequest struct {
	Email                string `json:"email"`
	Mobile               string `json:"mobile"`
	MachineName          string `json:"machine_name"`
	Platform             string `json:"platform"`
	Hostname             string `json:"hostname"`
	Arch                 string `json:"arch"`
	AppVersion           string `json:"app_version"`
	HeartbeatIntervalSec int    `json:"heartbeat_interval_sec"`
	ClientID             string `json:"client_id"`
	InvitationCode       string `json:"invitation_code"`
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

func EnrollStartHandler(identity *auth.IdentityService, feishuNotifier *feishu.Notifier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req EnrollStartRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}

		if req.Email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Email is required")
			return
		}

		resp, err := identity.StartEnrollment(r.Context(), req.Email, req.MachineName, req.Platform, req.ClientID, req.InvitationCode, req.Mobile)
		if err != nil {
			switch {
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

		if resp != nil && resp.MachineID != "" {
			heartbeat := req.HeartbeatIntervalSec
			if heartbeat < 5 || heartbeat > 3600 {
				heartbeat = 10
			}
			_ = identity.UpdateMachineMetadata(r.Context(), resp.MachineID, auth.MachineMetadata{
				Name:                 req.MachineName,
				Platform:             req.Platform,
				Hostname:             req.Hostname,
				Arch:                 req.Arch,
				AppVersion:           req.AppVersion,
				HeartbeatIntervalSec: heartbeat,
			})
		}

		// Auto-add user to Feishu organization so they can discover the bot.
		// This runs for all successful enrollments (including pending_approval)
		// so the user is already in the Feishu org when the admin approves.
		if resp != nil && feishuNotifier != nil {
			if ae := feishuNotifier.AutoEnroller(); ae != nil {
				email := req.Email
				mobile := req.Mobile
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					if err := ae.AddToFeishuOrg(ctx, email, "", mobile); err != nil {
						log.Printf("[enroll] feishu auto-enroll failed for %s: %v", email, err)
					}
				}()
			}
		}

		writeJSON(w, http.StatusOK, resp)
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

		resp, err := identity.RequestEmailLogin(r.Context(), req.Email)
		if err != nil {
			switch {
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
			"user": map[string]any{
				"email": user.Email,
				"sn":    user.SN,
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
