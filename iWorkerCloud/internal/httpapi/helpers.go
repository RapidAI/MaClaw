package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"
)

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": code, "message": msg})
}

func decodeJSON(r *http.Request, v interface{}) error {
	return json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(v)
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
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "请先登录")
			return
		}
		next(w, r)
	}
}
