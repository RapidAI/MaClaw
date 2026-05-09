package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"

	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/store"
)

const defaultJSONBodyLimit = 1 << 16

var (
	errJSONBodyTooLarge = errors.New("json body exceeds size limit")
	errJSONTrailingData = errors.New("json body contains trailing data")
)

type centerAuthenticator interface {
	AuthenticateCenter(ctx context.Context, centerID, rawSecret string) (*store.Center, error)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": code, "message": msg})
}

func decodeJSON(r *http.Request, v interface{}) error {
	return decodeJSONLimit(r, v, defaultJSONBodyLimit)
}

func decodeJSONLimit(r *http.Request, v interface{}, limit int64) error {
	if limit <= 0 {
		limit = defaultJSONBodyLimit
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return err
	}
	if int64(len(body)) > limit {
		return errJSONBodyTooLarge
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(v); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return errJSONTrailingData
		}
		return err
	}
	return nil
}

func centerSecretFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	return r.Header.Get("X-Center-Secret")
}

func authenticateCenterRequest(w http.ResponseWriter, r *http.Request, auth centerAuthenticator, centerID string) (*store.Center, bool) {
	if auth == nil {
		writeError(w, http.StatusInternalServerError, "CENTER_AUTH_UNAVAILABLE", "center authentication is unavailable")
		return nil, false
	}
	secret := centerSecretFromRequest(r)
	if secret == "" {
		writeError(w, http.StatusUnauthorized, "AUTH_FAILED", "missing center secret")
		return nil, false
	}
	center, err := auth.AuthenticateCenter(r.Context(), centerID, secret)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "AUTH_FAILED", "invalid center credentials")
		return nil, false
	}
	if center.Status == "disabled" {
		writeError(w, http.StatusForbidden, "CENTER_DISABLED", "center is disabled")
		return nil, false
	}
	return center, true
}

// Simple in-memory session store (production should use JWT or Redis).
var sessions sync.Map

func StoreSession(token, adminID string) {
	sessions.Store(token, adminID)
}

func ValidateSession(token string) (string, bool) {
	v, ok := sessions.Load(token)
	if !ok {
		return "", false
	}
	return v.(string), true
}

func RequireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if len(token) > 7 && token[:7] == "Bearer " {
			token = token[7:]
		}
		if token == "" {
			if c, err := r.Cookie("session"); err == nil {
				token = c.Value
			}
		}
		if _, ok := ValidateSession(token); !ok {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "login required")
			return
		}
		next(w, r)
	}
}
