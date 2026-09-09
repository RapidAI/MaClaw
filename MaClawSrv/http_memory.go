package main

import (
	"encoding/json"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"net/http"
	"strconv"
	"strings"
)

func (s *HTTPServer) handleListMemory(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	limit, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
	offset, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("offset")))
	out, err := s.svc.ListUserMemories(r.Context(), p, agentservice.UserMemoryListInput{Category: r.URL.Query().Get("category"), Query: r.URL.Query().Get("q"), Limit: limit, Offset: offset})
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleCreateMemory(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.UserMemorySaveInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid memory body"})
		return
	}
	out, err := s.svc.SaveUserMemory(r.Context(), p, in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *HTTPServer) handleUpdateMemory(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.UserMemorySaveInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid memory body"})
		return
	}
	out, err := s.svc.UpdateUserMemory(r.Context(), p, r.PathValue("id"), in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleDeleteMemory(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if err := s.svc.DeleteUserMemory(r.Context(), p, r.PathValue("id")); err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
