package repomaint

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

type principalKey struct{}

// Register mounts the public webhook and the machine-authenticated routes.
// identity may be nil; machine routes then return 401. The webhook does not
// use identity: the path token plus HMAC is the check.
func (s *Service) Register(mux *http.ServeMux, identity MachineAuthenticator) {
	if s == nil || mux == nil {
		return
	}
	s.identity = identity
	mux.HandleFunc("POST /api/github/repo-maint/hooks/{hookToken}", s.serveWebhook)
	mux.HandleFunc("PUT /api/github/repo-maint/rules/{ruleID}", s.withMachine(s.handlePutRule))
	mux.HandleFunc("DELETE /api/github/repo-maint/rules/{ruleID}", s.withMachine(s.handleDeleteRule))
	mux.HandleFunc("GET /api/github/repo-maint/deliveries", s.withMachine(s.handleListDeliveries))
	mux.HandleFunc("POST /api/github/repo-maint/deliveries/{id}/ack", s.withMachine(s.handleAck))
}

func (s *Service) withMachine(next func(http.ResponseWriter, *http.Request, *auth.MachinePrincipal)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ifaceNil(s.identity) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		machineID := strings.TrimSpace(r.Header.Get("X-Machine-ID"))
		token := bearerToken(r)
		if machineID == "" || token == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		principal, err := s.identity.AuthenticateMachine(r.Context(), machineID, token)
		if err != nil || principal == nil || strings.TrimSpace(principal.MachineID) == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if principal.MachineID != machineID {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r, principal)
	}
}

func ifaceNil(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Slice, reflect.Map, reflect.Func:
		return rv.IsNil()
	default:
		return false
	}
}

func bearerToken(r *http.Request) string {
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	const prefix = "Bearer "
	if len(h) < len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(h[len(prefix):])
}

func (s *Service) handlePutRule(w http.ResponseWriter, r *http.Request, principal *auth.MachinePrincipal) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	status, view := s.putRule(r.Context(), principal, r.PathValue("ruleID"), body)
	if status != http.StatusOK {
		w.WriteHeader(status)
		return
	}
	writeJSON(w, status, view)
}

func (s *Service) handleDeleteRule(w http.ResponseWriter, r *http.Request, principal *auth.MachinePrincipal) {
	w.WriteHeader(s.deleteRule(r.Context(), principal, r.PathValue("ruleID")))
}

func (s *Service) handleListDeliveries(w http.ResponseWriter, r *http.Request, principal *auth.MachinePrincipal) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.listDeliveries(r.Context(), principal, limit)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deliveries": items})
}

func (s *Service) handleAck(w http.ResponseWriter, r *http.Request, principal *auth.MachinePrincipal) {
	w.WriteHeader(s.ackDelivery(r.Context(), principal, r.PathValue("id")))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
