package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/survey"
)

const surveyMaxBodyBytes = 1 << 20 // 1 MiB

// SurveyHandler serves /api/v1/surveys/* with machine auth + tenant isolation.
type SurveyHandler struct {
	Store   *survey.Store
	Runtime *survey.Runtime
}

func NewSurveyHandler(st *survey.Store) *SurveyHandler {
	return &SurveyHandler{Store: st, Runtime: survey.NewRuntime(st)}
}

func (h *SurveyHandler) withMachine(identity veMachineAuthenticator, next func(http.ResponseWriter, *http.Request, *auth.MachinePrincipal)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateVEMachine(w, r, identity)
		if !ok {
			return
		}
		r = r.WithContext(WithRequestTenant(r.Context(), principal.TenantID))
		next(w, r, principal)
	}
}

func (h *SurveyHandler) Register(mux *http.ServeMux, identity veMachineAuthenticator) {
	authz := func(fn func(http.ResponseWriter, *http.Request, *auth.MachinePrincipal)) http.HandlerFunc {
		return h.withMachine(identity, fn)
	}
	mux.HandleFunc("GET /api/v1/surveys", authz(h.list))
	mux.HandleFunc("POST /api/v1/surveys", authz(h.create))
	mux.HandleFunc("GET /api/v1/surveys/{id}", authz(h.get))
	mux.HandleFunc("PATCH /api/v1/surveys/{id}", authz(h.update))
	mux.HandleFunc("DELETE /api/v1/surveys/{id}", authz(h.delete))
	mux.HandleFunc("POST /api/v1/surveys/{id}/publish", authz(h.publish))
	mux.HandleFunc("POST /api/v1/surveys/{id}/close", authz(h.close))
	mux.HandleFunc("POST /api/v1/surveys/{id}/reopen", authz(h.reopen))
	mux.HandleFunc("POST /api/v1/surveys/{id}/archive", authz(h.archive))
	mux.HandleFunc("POST /api/v1/surveys/{id}/duplicate", authz(h.duplicate))
	mux.HandleFunc("POST /api/v1/surveys/{id}/bindings", authz(h.bind))
	mux.HandleFunc("DELETE /api/v1/surveys/{id}/bindings/{platform}/{groupId}", authz(h.unbind))
	mux.HandleFunc("GET /api/v1/surveys/{id}/stats", authz(h.stats))
	mux.HandleFunc("GET /api/v1/surveys/{id}/responses", authz(h.responses))
	mux.HandleFunc("POST /api/v1/surveys/im/handle", authz(h.imHandle))
}

func (h *SurveyHandler) list(w http.ResponseWriter, r *http.Request, p *auth.MachinePrincipal) {
	status := r.URL.Query().Get("status")
	list, err := h.Store.List(r.Context(), p.TenantID, status)
	if err != nil {
		if errors.Is(err, survey.ErrInvalidStatus) {
			writeError(w, http.StatusBadRequest, "INVALID_STATUS", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "SURVEY_LIST_FAILED", err.Error())
		return
	}
	for i := range list {
		list[i].Redact()
	}
	writeJSON(w, http.StatusOK, map[string]any{"surveys": list})
}

func (h *SurveyHandler) decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, surveyMaxBodyBytes)
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

func (h *SurveyHandler) create(w http.ResponseWriter, r *http.Request, p *auth.MachinePrincipal) {
	var in survey.CreateInput
	if !h.decodeJSON(w, r, &in) {
		return
	}
	sv, err := h.Store.Create(r.Context(), p.TenantID, p.UserID, in)
	if err != nil {
		writeError(w, http.StatusBadRequest, "SURVEY_CREATE_FAILED", err.Error())
		return
	}
	sv.Redact()
	writeJSON(w, http.StatusOK, sv)
}

// surveyPathID returns a non-empty {id} path value or writes 400.
func surveyPathID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "survey id required")
		return "", false
	}
	return id, true
}

func (h *SurveyHandler) get(w http.ResponseWriter, r *http.Request, p *auth.MachinePrincipal) {
	id, ok := surveyPathID(w, r)
	if !ok {
		return
	}
	sv, err := h.Store.Get(r.Context(), p.TenantID, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "SURVEY_NOT_FOUND", "survey not found")
		return
	}
	sv.Redact()
	writeJSON(w, http.StatusOK, sv)
}

func (h *SurveyHandler) update(w http.ResponseWriter, r *http.Request, p *auth.MachinePrincipal) {
	id, ok := surveyPathID(w, r)
	if !ok {
		return
	}
	var in survey.UpdateInput
	if !h.decodeJSON(w, r, &in) {
		return
	}
	sv, err := h.Store.Update(r.Context(), p.TenantID, id, in)
	if err != nil {
		writeError(w, http.StatusBadRequest, "SURVEY_UPDATE_FAILED", err.Error())
		return
	}
	sv.Redact()
	writeJSON(w, http.StatusOK, sv)
}

func (h *SurveyHandler) delete(w http.ResponseWriter, r *http.Request, p *auth.MachinePrincipal) {
	id, ok := surveyPathID(w, r)
	if !ok {
		return
	}
	if err := h.Store.Delete(r.Context(), p.TenantID, id); err != nil {
		writeError(w, http.StatusBadRequest, "SURVEY_DELETE_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *SurveyHandler) publish(w http.ResponseWriter, r *http.Request, p *auth.MachinePrincipal) {
	id, ok := surveyPathID(w, r)
	if !ok {
		return
	}
	sv, err := h.Store.Publish(r.Context(), p.TenantID, id)
	if err != nil {
		writeError(w, http.StatusBadRequest, "SURVEY_PUBLISH_FAILED", err.Error())
		return
	}
	sv.Redact()
	writeJSON(w, http.StatusOK, sv)
}

func (h *SurveyHandler) close(w http.ResponseWriter, r *http.Request, p *auth.MachinePrincipal) {
	id, ok := surveyPathID(w, r)
	if !ok {
		return
	}
	sv, err := h.Store.Close(r.Context(), p.TenantID, id)
	if err != nil {
		writeError(w, http.StatusBadRequest, "SURVEY_CLOSE_FAILED", err.Error())
		return
	}
	sv.Redact()
	writeJSON(w, http.StatusOK, sv)
}

func (h *SurveyHandler) reopen(w http.ResponseWriter, r *http.Request, p *auth.MachinePrincipal) {
	id, ok := surveyPathID(w, r)
	if !ok {
		return
	}
	sv, err := h.Store.Reopen(r.Context(), p.TenantID, id)
	if err != nil {
		writeError(w, http.StatusBadRequest, "SURVEY_REOPEN_FAILED", err.Error())
		return
	}
	sv.Redact()
	writeJSON(w, http.StatusOK, sv)
}

func (h *SurveyHandler) archive(w http.ResponseWriter, r *http.Request, p *auth.MachinePrincipal) {
	id, ok := surveyPathID(w, r)
	if !ok {
		return
	}
	sv, err := h.Store.Archive(r.Context(), p.TenantID, id)
	if err != nil {
		writeError(w, http.StatusBadRequest, "SURVEY_ARCHIVE_FAILED", err.Error())
		return
	}
	sv.Redact()
	writeJSON(w, http.StatusOK, sv)
}

func (h *SurveyHandler) duplicate(w http.ResponseWriter, r *http.Request, p *auth.MachinePrincipal) {
	id, ok := surveyPathID(w, r)
	if !ok {
		return
	}
	sv, err := h.Store.Duplicate(r.Context(), p.TenantID, id, p.UserID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "SURVEY_DUPLICATE_FAILED", err.Error())
		return
	}
	sv.Redact()
	writeJSON(w, http.StatusOK, sv)
}

func (h *SurveyHandler) bind(w http.ResponseWriter, r *http.Request, p *auth.MachinePrincipal) {
	id, ok := surveyPathID(w, r)
	if !ok {
		return
	}
	var body struct {
		Bindings []survey.Binding `json:"bindings"`
	}
	if !h.decodeJSON(w, r, &body) {
		return
	}
	if err := h.Store.Bind(r.Context(), p.TenantID, id, body.Bindings); err != nil {
		writeError(w, http.StatusBadRequest, "SURVEY_BIND_FAILED", err.Error())
		return
	}
	sv, err := h.Store.Get(r.Context(), p.TenantID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "SURVEY_GET_FAILED", err.Error())
		return
	}
	sv.Redact()
	writeJSON(w, http.StatusOK, sv)
}

func (h *SurveyHandler) unbind(w http.ResponseWriter, r *http.Request, p *auth.MachinePrincipal) {
	id, ok := surveyPathID(w, r)
	if !ok {
		return
	}
	platform := r.PathValue("platform")
	groupID := r.PathValue("groupId")
	if err := h.Store.Unbind(r.Context(), p.TenantID, id, platform, groupID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "SURVEY_BINDING_NOT_FOUND", "binding not found")
			return
		}
		writeError(w, http.StatusBadRequest, "SURVEY_UNBIND_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *SurveyHandler) stats(w http.ResponseWriter, r *http.Request, p *auth.MachinePrincipal) {
	id, ok := surveyPathID(w, r)
	if !ok {
		return
	}
	sv, err := h.Store.Get(r.Context(), p.TenantID, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "SURVEY_NOT_FOUND", "survey not found")
		return
	}
	list, err := h.Store.ListResponses(r.Context(), p.TenantID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "SURVEY_RESPONSES_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, survey.ComputeStats(sv, list))
}

func (h *SurveyHandler) responses(w http.ResponseWriter, r *http.Request, p *auth.MachinePrincipal) {
	id, ok := surveyPathID(w, r)
	if !ok {
		return
	}
	list, err := h.Store.ListResponses(r.Context(), p.TenantID, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "SURVEY_NOT_FOUND", err.Error())
		return
	}
	// Cheap anonymous flag — avoid full Get (questions+bindings).
	// Fail closed on error so we never return respondent PII for anonymous surveys.
	anon, err := h.Store.IsAnonymous(r.Context(), p.TenantID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "SURVEY_RESPONSES_FAILED", err.Error())
		return
	}
	if anon {
		for i := range list {
			list[i].RespondentKey = "anonymous"
			list[i].RespondentName = ""
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"responses": list})
}

func (h *SurveyHandler) imHandle(w http.ResponseWriter, r *http.Request, p *auth.MachinePrincipal) {
	var req survey.IMHandleRequest
	if !h.decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Platform) == "" {
		req.Platform = survey.PlatformLansenger
	}
	out, err := h.Runtime.Handle(r.Context(), p.TenantID, req)
	if err != nil {
		// Client input problems (missing user_id, etc.) → 400; domain faults → 500.
		msg := err.Error()
		if msg == "user_id required" {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST", msg)
			return
		}
		writeError(w, http.StatusInternalServerError, "SURVEY_IM_HANDLE_FAILED", msg)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
