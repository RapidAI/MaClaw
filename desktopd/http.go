package desktopd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// Handler is the Docker service API Hub calls, plus the /admin operator
// panel when a state directory is configured. stateDir may be empty to keep
// the API-only behaviour of older deployments. proxyKeys and egress may be
// nil; when set, the panel's proxy-token and egress-proxy APIs resolve from
// the same file-backed sources the forward proxy and the desktop service use.
func Handler(svc *Service, token, stateDir string, proxyKeys *ProxyKeySource, egress *EgressSource) http.Handler {
	token = strings.TrimSpace(token)
	admin := newAdminServer(svc, token, stateDir, adminPage, proxyKeys, egress)
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
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "proxy": SnapshotProxyMetrics()})
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
			if errors.Is(err, ErrInvalid) {
				writeServiceErr(w, err)
				return
			}
			message := desktop.AppFailureText(text, err)
			writeJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": message, "message": message})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"output": text})
	})
	mux.HandleFunc("POST /v1/desktops/open", func(w http.ResponseWriter, r *http.Request) {
		if !authOK(r) {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		var in struct {
			TenantID string   `json:"tenant_id"`
			UserID   string   `json:"user_id"`
			Display  string   `json:"display"`
			Program  string   `json:"program"`
			Args     []string `json:"args"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		text, err := svc.Open(r.Context(), in.TenantID, in.UserID, in.Display, in.Program, in.Args)
		if err != nil {
			if errors.Is(err, ErrInvalid) {
				writeServiceErr(w, err)
				return
			}
			message := desktop.OpenFailureText(in.Program, text, err)
			writeJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": message, "message": message})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"output": text})
	})
	mux.HandleFunc("POST /v1/desktops/file", func(w http.ResponseWriter, r *http.Request) {
		if !authOK(r) {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		var in struct {
			TenantID  string `json:"tenant_id"`
			UserID    string `json:"user_id"`
			Action    string `json:"action"`
			Path      string `json:"path"`
			Content   string `json:"content"`
			OldString string `json:"old_string"`
			NewString string `json:"new_string"`
		}
		if !decodeLimited(w, r, 1<<20, &in) {
			return
		}
		var text string
		var err error
		switch strings.ToLower(strings.TrimSpace(in.Action)) {
		case "read":
			text, err = svc.ReadFile(r.Context(), in.TenantID, in.UserID, in.Path)
		case "bytes":
			text, err = svc.ReadBytes(r.Context(), in.TenantID, in.UserID, in.Path)
		case "write":
			text, err = svc.WriteFile(r.Context(), in.TenantID, in.UserID, in.Path, in.Content)
		case "edit":
			text, err = svc.EditFile(r.Context(), in.TenantID, in.UserID, in.Path, in.OldString, in.NewString)
		case "list":
			text, err = svc.ListFile(r.Context(), in.TenantID, in.UserID, in.Path)
		default:
			err = fmt.Errorf("%w: file action is invalid", ErrInvalid)
		}
		writeOp(w, text, err)
	})
	mux.HandleFunc("POST /v1/desktops/bash", func(w http.ResponseWriter, r *http.Request) {
		if !authOK(r) {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		deadline := time.Now().Add(desktop.BashBudget + time.Minute)
		controller := http.NewResponseController(w)
		_ = controller.SetWriteDeadline(deadline)
		_ = controller.SetReadDeadline(deadline)
		var in struct {
			TenantID string `json:"tenant_id"`
			UserID   string `json:"user_id"`
			Command  string `json:"command"`
		}
		if !decodeLimited(w, r, 64<<10, &in) {
			return
		}
		text, err := svc.Bash(r.Context(), in.TenantID, in.UserID, in.Command)
		writeOp(w, text, err)
	})
	mux.HandleFunc("POST /v1/desktops/http", func(w http.ResponseWriter, r *http.Request) {
		if !authOK(r) {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		var in struct {
			TenantID string `json:"tenant_id"`
			UserID   string `json:"user_id"`
			URL      string `json:"url"`
		}
		if !decodeLimited(w, r, 64<<10, &in) {
			return
		}
		body, status, kind, err := svc.HTTPGet(r.Context(), in.TenantID, in.UserID, in.URL)
		if err != nil {
			writeOp(w, "", err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"body":         body,
			"status":       status,
			"content_type": kind,
		})
	})
	mux.HandleFunc("POST /v1/desktops/install", func(w http.ResponseWriter, r *http.Request) {
		if !authOK(r) {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		// apt-get stops at InstallBudget. This deadline is one minute longer
		// so the budget sentence can be written after apt stops.
		deadline := time.Now().Add(desktop.InstallClientTimeout)
		controller := http.NewResponseController(w)
		_ = controller.SetWriteDeadline(deadline)
		_ = controller.SetReadDeadline(deadline)
		var in struct {
			TenantID string   `json:"tenant_id"`
			UserID   string   `json:"user_id"`
			Packages []string `json:"packages"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		text, err := svc.Install(r.Context(), in.TenantID, in.UserID, in.Packages)
		if err != nil {
			if errors.Is(err, ErrInvalid) {
				writeServiceErr(w, err)
				return
			}
			message := desktop.AptFailureText(text, err)
			writeJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": message, "message": message})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"output": text})
	})
	// screenshot returns the user's display as an image. GET takes query
	// parameters, POST the same JSON fields as /v1/desktops/app, plus the
	// optional desktop.ShotOptions fields (max_width, max_height, region,
	// format, quality, cursor, marks). X-Desktop-* headers carry the screen
	// size, image size, region, scale and pointer.
	screenshot := func(w http.ResponseWriter, r *http.Request) {
		if !authOK(r) {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		var in struct {
			TenantID string `json:"tenant_id"`
			UserID   string `json:"user_id"`
			Display  string `json:"display"`
			Name     string `json:"name"`
			desktop.ShotOptions
		}
		if r.Method == http.MethodGet {
			query := r.URL.Query()
			in.TenantID, in.UserID, in.Display = query.Get("tenant_id"), query.Get("user_id"), query.Get("display")
			opts, err := shotOptionsFromQuery(query)
			if err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			in.ShotOptions = opts
		} else if !decodeBody(w, r, &in) {
			return
		}
		shot, saved, err := svc.CaptureShot(r.Context(), in.TenantID, in.UserID, in.Display, in.Name, in.ShotOptions)
		if err != nil {
			writeServiceErr(w, err)
			return
		}
		w.Header().Set("Content-Type", shot.MIME)
		w.Header().Set("Content-Length", strconv.Itoa(len(shot.Data)))
		w.Header().Set("Cache-Control", "no-store")
		for key, value := range desktop.ShotHeaderValues(shot) {
			w.Header().Set(key, value)
		}
		if saved != "" {
			w.Header().Set("X-Desktop-Saved-Path", saved)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(shot.Data)
	}
	// ctl runs one control request: {"tenant_id","user_id","display",
	// "request":{"op":...}}. The answer is the supervisor's JSON object;
	// "ok":false is a 200 with the reason, a bad request is a 400.
	mux.HandleFunc("POST /v1/desktops/ctl", func(w http.ResponseWriter, r *http.Request) {
		if !authOK(r) {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		var in struct {
			TenantID string         `json:"tenant_id"`
			UserID   string         `json:"user_id"`
			Display  string         `json:"display"`
			Request  map[string]any `json:"request"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		if in.Request == nil {
			writeErr(w, http.StatusBadRequest, "request is required")
			return
		}
		result, err := svc.Ctl(r.Context(), in.TenantID, in.UserID, in.Display, in.Request)
		if err != nil {
			writeServiceErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
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
	return withBotID(mux)
}

// decodeBody caps request bodies: the desktop API only ever carries small
// JSON specs, so an oversized body is a client bug or an attack.
func decodeBody(w http.ResponseWriter, r *http.Request, dest any) bool {
	return decodeMessage(w, r, "invalid desktop request", dest)
}

// decodeLimited is decodeBody with a caller-chosen cap. File writes carry
// the file itself, so they need more room than a package list.
func decodeLimited(w http.ResponseWriter, r *http.Request, limit int64, dest any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	if err := json.NewDecoder(r.Body).Decode(dest); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid desktop request")
		return false
	}
	return true
}

func writeOp(w http.ResponseWriter, output string, err error) {
	if err == nil {
		writeJSON(w, http.StatusOK, map[string]string{"output": output})
		return
	}
	status := http.StatusBadGateway
	if errors.Is(err, ErrInvalid) {
		status = http.StatusBadRequest
	}
	writeJSON(w, status, map[string]any{"ok": false, "error": err.Error(), "message": err.Error()})
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

// shotOptionsFromQuery reads screenshot options from GET parameters:
// max_width, max_height, format, quality, cursor, marks, and region=x,y,w,h.
func shotOptionsFromQuery(query url.Values) (desktop.ShotOptions, error) {
	var opts desktop.ShotOptions
	number := func(key string) (int, error) {
		raw := strings.TrimSpace(query.Get(key))
		if raw == "" {
			return 0, nil
		}
		n, err := strconv.Atoi(raw)
		if err != nil {
			return 0, fmt.Errorf("%s must be a number", key)
		}
		return n, nil
	}
	var err error
	if opts.MaxWidth, err = number("max_width"); err != nil {
		return opts, err
	}
	if opts.MaxHeight, err = number("max_height"); err != nil {
		return opts, err
	}
	if opts.Quality, err = number("quality"); err != nil {
		return opts, err
	}
	opts.Format = query.Get("format")
	opts.Cursor = query.Get("cursor") == "1" || query.Get("cursor") == "true"
	opts.Marks = query.Get("marks") == "1" || query.Get("marks") == "true"
	if raw := strings.TrimSpace(query.Get("region")); raw != "" {
		r := &opts.Region
		if _, err := fmt.Sscanf(raw, "%d,%d,%d,%d", &r.X, &r.Y, &r.Width, &r.Height); err != nil {
			return opts, fmt.Errorf("region must be x,y,width,height")
		}
	}
	return opts, nil
}
