package main

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

// handlePauseRun holds a running run at its next step boundary. Body is
// optional: {"reason":"...","timeout_sec":n}.
func (s *HTTPServer) handlePauseRun(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in struct {
		Reason     string `json:"reason"`
		TimeoutSec int64  `json:"timeout_sec"`
	}
	if r.Body != nil {
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 64<<10))
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &in); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"code": "INVALID_REQUEST", "message": "invalid pause body"}})
				return
			}
		}
	}
	if in.TimeoutSec < 0 {
		in.TimeoutSec = 0
	}
	out, err := s.svc.PauseRun(r.Context(), p, r.PathValue("instanceId"), r.PathValue("runId"), in.Reason, time.Duration(in.TimeoutSec)*time.Second)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleResumeRun(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.ResumeRun(r.Context(), p, r.PathValue("instanceId"), r.PathValue("runId"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleGetRunPause(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.RunPause(r.Context(), p, r.PathValue("instanceId"), r.PathValue("runId"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, out)
}
