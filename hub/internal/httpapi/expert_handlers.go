package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/expert"
)

const expertMaxBodyBytes = 1 << 20 // 1 MiB

// ExpertHandler serves /api/v1/experts/* with machine auth + tenant isolation.
type ExpertHandler struct {
	Store *expert.Store
}

func NewExpertHandler(st *expert.Store) *ExpertHandler {
	return &ExpertHandler{Store: st}
}

func (h *ExpertHandler) withMachine(identity veMachineAuthenticator, next func(http.ResponseWriter, *http.Request, *auth.MachinePrincipal)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateVEMachine(w, r, identity)
		if !ok {
			return
		}
		r = r.WithContext(WithRequestTenant(r.Context(), principal.TenantID))
		next(w, r, principal)
	}
}

func (h *ExpertHandler) Register(mux *http.ServeMux, identity veMachineAuthenticator) {
	authz := func(fn func(http.ResponseWriter, *http.Request, *auth.MachinePrincipal)) http.HandlerFunc {
		return h.withMachine(identity, fn)
	}
	mux.HandleFunc("GET /api/v1/experts", authz(h.list))
	mux.HandleFunc("POST /api/v1/experts", authz(h.create))
	mux.HandleFunc("GET /api/v1/experts/{id}", authz(h.get))
	mux.HandleFunc("PATCH /api/v1/experts/{id}", authz(h.update))
	mux.HandleFunc("DELETE /api/v1/experts/{id}", authz(h.delete))
}

func (h *ExpertHandler) list(w http.ResponseWriter, r *http.Request, p *auth.MachinePrincipal) {
	list, err := h.Store.List(r.Context(), p.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "EXPERT_LIST_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"experts": list})
}

func (h *ExpertHandler) decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, expertMaxBodyBytes)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		if err == io.EOF {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "empty body")
			return false
		}
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid body")
		return false
	}
	// Reject concatenated / trailing JSON (e.g. `{}{}`).
	if dec.More() {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "trailing data")
		return false
	}
	return true
}

func (h *ExpertHandler) create(w http.ResponseWriter, r *http.Request, p *auth.MachinePrincipal) {
	var in expert.CreateInput
	if !h.decodeJSON(w, r, &in) {
		return
	}
	ex, applied, err := h.Store.Upsert(r.Context(), p.TenantID, in)
	if err != nil {
		var verr *expert.ValidationError
		if errors.As(err, &verr) {
			writeError(w, http.StatusBadRequest, "EXPERT_CREATE_FAILED", verr.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "EXPERT_CREATE_FAILED", "internal error")
		return
	}
	// applied=false：LWW 过期写或命中墓碑，服务端值未被覆盖。
	writeJSON(w, http.StatusOK, struct {
		*expert.Expert
		Applied bool `json:"applied"`
	}{ex, applied})
}

// expertPathID returns a non-empty {id} path value or writes 400.
func expertPathID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "expert id required")
		return "", false
	}
	return id, true
}

// writeExpertMutateError maps missing experts to 404, validation errors to 400,
// and internal DB errors to 500 (no err.Error() echo).
func writeExpertMutateError(w http.ResponseWriter, code string, err error) {
	var verr *expert.ValidationError
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "EXPERT_NOT_FOUND", "expert not found")
	case errors.As(err, &verr):
		writeError(w, http.StatusBadRequest, code, verr.Error())
	default:
		writeError(w, http.StatusInternalServerError, code, "internal error")
	}
}

func (h *ExpertHandler) get(w http.ResponseWriter, r *http.Request, p *auth.MachinePrincipal) {
	id, ok := expertPathID(w, r)
	if !ok {
		return
	}
	ex, err := h.Store.Get(r.Context(), p.TenantID, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "EXPERT_NOT_FOUND", "expert not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "EXPERT_GET_FAILED", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, ex)
}

func (h *ExpertHandler) update(w http.ResponseWriter, r *http.Request, p *auth.MachinePrincipal) {
	id, ok := expertPathID(w, r)
	if !ok {
		return
	}
	var in expert.UpdateInput
	if !h.decodeJSON(w, r, &in) {
		return
	}
	ex, err := h.Store.Update(r.Context(), p.TenantID, id, in)
	if err != nil {
		writeExpertMutateError(w, "EXPERT_UPDATE_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, ex)
}

func (h *ExpertHandler) delete(w http.ResponseWriter, r *http.Request, p *auth.MachinePrincipal) {
	id, ok := expertPathID(w, r)
	if !ok {
		return
	}
	if err := h.Store.Delete(r.Context(), p.TenantID, id); err != nil {
		writeExpertMutateError(w, "EXPERT_DELETE_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
