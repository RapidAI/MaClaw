package desktopd

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

// Handler is the Docker service API Hub calls, plus the /admin operator
// panel when a state directory is configured. stateDir may be empty to keep
// the API-only behaviour of older deployments.
func Handler(svc *Service, token, stateDir string) http.Handler {
	token = strings.TrimSpace(token)
	admin := newAdminServer(svc, token, stateDir, adminPage)
	// /v1/* accepts the primary DESKTOPD_TOKEN and every key issued through
	// the admin panel.
	authOK := func(r *http.Request) bool {
		return admin.tokenAccepted(bearerToken(r))
	}
	mux := http.NewServeMux()
	mux.Handle("GET /admin", admin)
	mux.Handle("/admin/", admin)
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, r *http.Request) {
		if !authOK(r) {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("POST /v1/desktops", func(w http.ResponseWriter, r *http.Request) {
		if !authOK(r) {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		var in struct {
			TenantID string `json:"tenant_id"`
			UserID   string `json:"user_id"`
			Image    string `json:"image"`
			Memory   string `json:"memory"`
			CPUs     string `json:"cpus"`
			ShmSize  string `json:"shm_size"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		desktop, err := svc.Create(r.Context(), Spec{
			TenantID: in.TenantID, UserID: in.UserID, Image: in.Image,
			Memory: in.Memory, CPUs: in.CPUs, ShmSize: in.ShmSize,
		})
		if err != nil {
			writeServiceErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, desktop)
	})
	mux.HandleFunc("POST /v1/desktops/session", func(w http.ResponseWriter, r *http.Request) {
		if !authOK(r) {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		var in struct {
			TenantID string `json:"tenant_id"`
			UserID   string `json:"user_id"`
			Image    string `json:"image"`
			Memory   string `json:"memory"`
			CPUs     string `json:"cpus"`
			ShmSize  string `json:"shm_size"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		session, err := svc.OpenSession(r.Context(), Spec{
			TenantID: in.TenantID, UserID: in.UserID, Image: in.Image,
			Memory: in.Memory, CPUs: in.CPUs, ShmSize: in.ShmSize,
		})
		if err != nil {
			writeServiceErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, session)
	})
	mux.HandleFunc("POST /v1/desktops/app", func(w http.ResponseWriter, r *http.Request) {
		if !authOK(r) {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		var in struct {
			TenantID string   `json:"tenant_id"`
			UserID   string   `json:"user_id"`
			Display  string   `json:"display"`
			Args     []string `json:"args"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		text, err := svc.App(r.Context(), in.TenantID, in.UserID, in.Display, in.Args)
		if err != nil {
			writeServiceErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"output": text})
	})
	// screenshot returns the user's display as image/png. GET takes query
	// parameters, POST the same JSON fields as /v1/desktops/app.
	screenshot := func(w http.ResponseWriter, r *http.Request) {
		if !authOK(r) {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		var in struct {
			TenantID string `json:"tenant_id"`
			UserID   string `json:"user_id"`
			Display  string `json:"display"`
		}
		if r.Method == http.MethodGet {
			query := r.URL.Query()
			in.TenantID, in.UserID, in.Display = query.Get("tenant_id"), query.Get("user_id"), query.Get("display")
		} else if !decodeBody(w, r, &in) {
			return
		}
		png, err := svc.Screenshot(r.Context(), in.TenantID, in.UserID, in.Display)
		if err != nil {
			writeServiceErr(w, err)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Content-Length", strconv.Itoa(len(png)))
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(png)
	}
	mux.HandleFunc("GET /v1/desktops/screenshot", screenshot)
	mux.HandleFunc("POST /v1/desktops/screenshot", screenshot)
	mux.HandleFunc("POST /v1/desktops/stop", func(w http.ResponseWriter, r *http.Request) {
		if !authOK(r) {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		var in struct {
			TenantID string `json:"tenant_id"`
			UserID   string `json:"user_id"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		desktop, err := svc.Stop(r.Context(), in.TenantID, in.UserID)
		if err != nil {
			writeServiceErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, desktop)
	})
	return mux
}

// decodeBody caps request bodies: the desktop API only ever carries small
// JSON specs, so an oversized body is a client bug or an attack.
func decodeBody(w http.ResponseWriter, r *http.Request, dest any) bool {
	return decodeMessage(w, r, "invalid desktop request", dest)
}

// decodeMessage is decodeBody with a caller-specific error message.
func decodeMessage(w http.ResponseWriter, r *http.Request, message string, dest any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(dest); err != nil {
		writeErr(w, http.StatusBadRequest, message)
		return false
	}
	return true
}

// bearerToken strips the Authorization header down to the bare credential.
func bearerToken(r *http.Request) string {
	return strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
}

func writeServiceErr(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrInvalid) {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeErr(w, http.StatusBadGateway, "docker request failed")
}

func writeErr(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"ok": false, "message": message})
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
