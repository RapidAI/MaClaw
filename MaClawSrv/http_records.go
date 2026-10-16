package main

import (
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"net/http"
	"strings"
)

func (s *HTTPServer) handleListStructuredRecords(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	page, err := parsePageQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	out, err := s.svc.ListStructuredRecords(r.Context(), p, agentservice.ListStructuredRecordsInput{
		Collection: r.PathValue("collection"),
		Tag:        strings.TrimSpace(r.URL.Query().Get("tag")),
		Q:          strings.TrimSpace(r.URL.Query().Get("q")),
		Limit:      page.Limit,
		Before:     formatOptionalCursorTime(page.Before),
	})
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	items, meta := recordsPageMeta(out, page)
	writeJSON(w, http.StatusOK, listResponse(items, meta))
}

func (s *HTTPServer) handleCreateStructuredRecord(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.CreateStructuredRecordInput
	if !decodeJSON(w, r, &in) {
		return
	}
	in.Collection = r.PathValue("collection")
	out, err := s.svc.CreateStructuredRecord(r.Context(), p, in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *HTTPServer) handleGetStructuredRecord(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.GetStructuredRecord(r.Context(), p, r.PathValue("collection"), r.PathValue("recordId"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleUpdateStructuredRecord(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.UpdateStructuredRecordInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateStructuredRecord(r.Context(), p, r.PathValue("collection"), r.PathValue("recordId"), in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleDeleteStructuredRecord(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if err := s.svc.DeleteStructuredRecord(r.Context(), p, r.PathValue("collection"), r.PathValue("recordId")); err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
