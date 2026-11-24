package main

import (
	"context"
	"net/http"

	"github.com/RapidAI/CodeClaw/MaClawSrv/cloudagent"
)

func (s *HTTPServer) registerCloudAgentRoutes() {
	if s == nil || s.cloudRuntime == nil {
		return
	}
	s.mux.HandleFunc("PUT /api/v1/cloud-agents/{id}", s.cloudAdmin(s.cloudRuntime.ServePut))
	s.mux.HandleFunc("DELETE /api/v1/cloud-agents/{id}", s.cloudAdmin(s.cloudRuntime.ServeDelete))
	s.mux.HandleFunc("POST /api/v1/cloud-agents/{id}/stop", s.cloudAdmin(s.cloudRuntime.ServeStop))
	s.mux.HandleFunc("POST /api/v1/cloud-agents/{id}/resume", s.cloudAdmin(s.cloudRuntime.ServeResume))
	s.mux.HandleFunc("PUT /api/v1/cloud-agents/{id}/llm", s.cloudAdmin(s.cloudRuntime.ServeLLM))
	s.mux.HandleFunc("DELETE /api/v1/cloud-agents/{id}/llm", s.cloudAdmin(s.cloudRuntime.ServeLLMDelete))
	s.mux.HandleFunc("GET /api/v1/cloud-agents/{id}/git", s.cloudAdmin(s.cloudRuntime.ServeGitGet))
	s.mux.HandleFunc("PUT /api/v1/cloud-agents/{id}/git", s.cloudAdmin(s.cloudRuntime.ServeGitPut))
	s.mux.HandleFunc("DELETE /api/v1/cloud-agents/{id}/git", s.cloudAdmin(s.cloudRuntime.ServeGitDelete))
	s.mux.HandleFunc("POST /api/v1/cloud-agents/{id}/control", s.cloudAdmin(s.cloudRuntime.ServeControl))
}

// cloudAdmin accepts only a loopback caller that presents the process admin
// secret. An admin session cookie is not enough for cloud-agent routes.
func (s *HTTPServer) cloudAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !cloudagent.LoopbackRemote(r.RemoteAddr) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "云端运行时不可用"})
			return
		}
		if !cloudagent.SecretOK(r.Header.Get("X-MaClaw-Admin-Secret"), s.adminSecret) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid admin secret"})
			return
		}
		next(w, r)
	}
}

// StartCloudAgents runs the heartbeat and deleted-directory sweep.
func (s *HTTPServer) StartCloudAgents(ctx context.Context) {
	if s != nil && s.cloudRuntime != nil {
		s.cloudRuntime.Start(ctx)
	}
}
