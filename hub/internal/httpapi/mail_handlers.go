package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/mail"
)

type AdminSendTestMailRequest struct {
	Email string `json:"email"`
}

func GetMailConfigHandler(mailer *mail.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if mailer == nil {
			writeError(w, http.StatusInternalServerError, "MAIL_UNAVAILABLE", "Mail service is unavailable")
			return
		}
		cfg, err := mailer.CurrentConfig(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "MAIL_CONFIG_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, cfg)
	}
}

func UpdateMailConfigHandler(mailer *mail.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if mailer == nil {
			writeError(w, http.StatusInternalServerError, "MAIL_UNAVAILABLE", "Mail service is unavailable")
			return
		}
		var cfg mail.ConfigState
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		saved, err := mailer.SaveConfig(r.Context(), cfg)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "MAIL_CONFIG_SAVE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, saved)
	}
}

func AdminSendTestMailHandler(mailer mail.Mailer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if mailer == nil {
			writeError(w, http.StatusBadRequest, "MAIL_NOT_CONFIGURED", "Mail delivery is not configured")
			return
		}

		var req AdminSendTestMailRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}

		email := strings.TrimSpace(req.Email)
		if email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "Email is required")
			return
		}

		body := "This is a MaClaw Hub test email.\r\n\r\nYour mail configuration is working."
		if err := mailer.Send(r.Context(), []string{email}, "MaClaw Hub test email", body); err != nil {
			writeError(w, http.StatusInternalServerError, "MAIL_SEND_FAILED", fmt.Sprintf("Failed to send test email: %v", err))
			return
		}
		if svc, ok := mailer.(*mail.Service); ok {
			if _, err := svc.MarkTestSuccess(r.Context()); err != nil {
				writeError(w, http.StatusInternalServerError, "MAIL_TEST_MARK_FAILED", err.Error())
				return
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"message": "Test email sent",
		})
	}
}
