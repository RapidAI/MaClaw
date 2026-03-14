package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

type EnrollStartRequest struct {
	Email                string `json:"email"`
	MachineName          string `json:"machine_name"`
	Platform             string `json:"platform"`
	Hostname             string `json:"hostname"`
	Arch                 string `json:"arch"`
	AppVersion           string `json:"app_version"`
	HeartbeatIntervalSec int    `json:"heartbeat_interval_sec"`
}

type EmailRequestLoginRequest struct {
	Email string `json:"email"`
}

type EmailConfirmLoginRequest struct {
	Token string `json:"token"`
}

func EnrollStartHandler(identity *auth.IdentityService) http.HandlerFunc {
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

		resp, err := identity.StartEnrollment(r.Context(), req.Email, req.MachineName, req.Platform)
		if err != nil {
			switch {
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
			_ = identity.UpdateMachineMetadata(r.Context(), resp.MachineID, auth.MachineMetadata{
				Name:                 req.MachineName,
				Platform:             req.Platform,
				Hostname:             req.Hostname,
				Arch:                 req.Arch,
				AppVersion:           req.AppVersion,
				HeartbeatIntervalSec: req.HeartbeatIntervalSec,
			})
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
			"expires_in":   86400,
			"user": map[string]any{
				"email": user.Email,
				"sn":    user.SN,
			},
		})
	}
}
