package main

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/httpthreat"
)

func (s *HTTPServer) attachHTTPThreat() {
	if s == nil {
		return
	}
	n := httpthreat.NodeFromEnv()
	if n == nil {
		return
	}
	s.threatNode = n
	s.threatWrap = httpthreat.WrapEnabled()
	s.mux.HandleFunc("POST /api/httpthreat/inspect", s.handleHTTPThreatInspect)
}

func (s *HTTPServer) StartHTTPThreat(ctx context.Context) {
	if s == nil || s.threatNode == nil {
		return
	}
	s.threatNode.StartPull(ctx, 30*time.Second)
}

func (s *HTTPServer) handleHTTPThreatInspect(w http.ResponseWriter, r *http.Request) {
	if s == nil || s.threatNode == nil {
		http.NotFound(w, r)
		return
	}
	dec := s.threatNode.Inspect(r)
	if httpthreat.WriteAction(w, dec) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(dec)
}
