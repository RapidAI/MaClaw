package main

import (
	"context"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"net/http"
	"strings"
)

func (s *HTTPServer) handleListMCPServers(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.ListMCPServers(r.Context(), p)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	page, err := parsePageQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	items, meta := paginateMCPServers(sanitizeMCPServerViewsForAPI(s.svc.DataRoot(), out), page)
	writeJSON(w, http.StatusOK, listResponse(items, meta))
}

func (s *HTTPServer) handleSearchMCPMarket(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.SearchMCPMarket(r.Context(), p, strings.TrimSpace(r.URL.Query().Get("q")))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *HTTPServer) handleInstallMCPMarket(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.MCPCapabilitySummary
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.InstallMCPMarketCapability(r.Context(), p, in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusCreated, sanitizeMCPServerViewPtrForAPI(s.svc.DataRoot(), out))
}

func (s *HTTPServer) handleCreateMCPServer(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.MCPServerCreateInput
	if !decodeJSON(w, r, &in) {
		return
	}
	asyncMode, err := parseRequiredBoolLikeQuery(r, "async")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if asyncMode {
		job, admissionErr := s.admitUserJob(r, p, "mcp.create", agentruntime.JobRecoveryPolicyReconcile, agentruntime.JobRetryPolicy{}, in, func(ctx context.Context) (any, error) {
			return executeRecordedJobEffect(ctx, "mcp.create", map[string]any{"operation": "create", "kind": in.Kind, "name": in.Name}, false, func(ctx context.Context) (any, string, error) {
				out, err := s.svc.CreateMCPServer(ctx, p, in)
				if err != nil {
					return nil, "", err
				}
				result := sanitizeMCPServerViewForAPI(s.svc.DataRoot(), *out)
				return result, out.ID, nil
			})
		})
		if admissionErr != nil {
			writeAsyncJobAdmissionError(w, admissionErr)
			return
		}
		writeJSON(w, http.StatusAccepted, job)
		return
	}
	out, err := s.svc.CreateMCPServer(r.Context(), p, in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusCreated, sanitizeMCPServerViewPtrForAPI(s.svc.DataRoot(), out))
}

func (s *HTTPServer) handleGetMCPServer(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.GetMCPServer(r.Context(), p, r.PathValue("serverId"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, sanitizeMCPServerViewPtrForAPI(s.svc.DataRoot(), out))
}

func (s *HTTPServer) handleUpdateMCPServer(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.MCPServerUpdateInput
	if !decodeJSON(w, r, &in) {
		return
	}
	asyncMode, err := parseRequiredBoolLikeQuery(r, "async")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if asyncMode {
		serverID := r.PathValue("serverId")
		identity := struct {
			ServerID string                            `json:"server_id"`
			Input    agentservice.MCPServerUpdateInput `json:"input"`
		}{ServerID: serverID, Input: in}
		job, admissionErr := s.admitUserJob(r, p, "mcp.update", agentruntime.JobRecoveryPolicyReconcile, agentruntime.JobRetryPolicy{}, identity, func(ctx context.Context) (any, error) {
			return executeRecordedJobEffect(ctx, "mcp.update", map[string]any{"operation": "update", "server_id": serverID}, true, func(ctx context.Context) (any, string, error) {
				out, err := s.svc.UpdateMCPServer(ctx, p, serverID, in)
				if err != nil {
					return nil, serverID, err
				}
				result := sanitizeMCPServerViewForAPI(s.svc.DataRoot(), *out)
				return result, serverID, nil
			})
		})
		if admissionErr != nil {
			writeAsyncJobAdmissionError(w, admissionErr)
			return
		}
		writeJSON(w, http.StatusAccepted, job)
		return
	}
	out, err := s.svc.UpdateMCPServer(r.Context(), p, r.PathValue("serverId"), in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, sanitizeMCPServerViewPtrForAPI(s.svc.DataRoot(), out))
}

func (s *HTTPServer) handleDeleteMCPServer(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if err := s.svc.DeleteMCPServer(r.Context(), p, r.PathValue("serverId")); err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *HTTPServer) handleStartMCPServer(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	asyncMode, err := parseRequiredBoolLikeQuery(r, "async")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if asyncMode {
		serverID := r.PathValue("serverId")
		job, admissionErr := s.admitUserJob(r, p, "mcp.start", agentruntime.JobRecoveryPolicyReconcile, agentruntime.JobRetryPolicy{}, map[string]string{"server_id": serverID}, func(ctx context.Context) (any, error) {
			return executeRecordedJobEffect(ctx, "mcp.start", map[string]any{"operation": "start", "server_id": serverID}, true, func(ctx context.Context) (any, string, error) {
				out, err := s.svc.StartMCPServer(ctx, p, serverID)
				if err != nil {
					return nil, serverID, err
				}
				result := sanitizeMCPServerViewForAPI(s.svc.DataRoot(), *out)
				return result, serverID, nil
			})
		})
		if admissionErr != nil {
			writeAsyncJobAdmissionError(w, admissionErr)
			return
		}
		writeJSON(w, http.StatusAccepted, job)
		return
	}
	out, err := s.svc.StartMCPServer(r.Context(), p, r.PathValue("serverId"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, sanitizeMCPServerViewPtrForAPI(s.svc.DataRoot(), out))
}

func (s *HTTPServer) handleStopMCPServer(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	asyncMode, err := parseRequiredBoolLikeQuery(r, "async")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if asyncMode {
		serverID := r.PathValue("serverId")
		job, admissionErr := s.admitUserJob(r, p, "mcp.stop", agentruntime.JobRecoveryPolicyReconcile, agentruntime.JobRetryPolicy{}, map[string]string{"server_id": serverID}, func(ctx context.Context) (any, error) {
			return executeRecordedJobEffect(ctx, "mcp.stop", map[string]any{"operation": "stop", "server_id": serverID}, true, func(ctx context.Context) (any, string, error) {
				out, err := s.svc.StopMCPServer(ctx, p, serverID)
				if err != nil {
					return nil, serverID, err
				}
				result := sanitizeMCPServerViewForAPI(s.svc.DataRoot(), *out)
				return result, serverID, nil
			})
		})
		if admissionErr != nil {
			writeAsyncJobAdmissionError(w, admissionErr)
			return
		}
		writeJSON(w, http.StatusAccepted, job)
		return
	}
	out, err := s.svc.StopMCPServer(r.Context(), p, r.PathValue("serverId"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, sanitizeMCPServerViewPtrForAPI(s.svc.DataRoot(), out))
}

func (s *HTTPServer) handleCheckMCPServer(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	serverID := r.PathValue("serverId")
	asyncMode, err := parseRequiredBoolLikeQuery(r, "async")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if asyncMode {
		const jobKind = "mcp.health_check"
		retryPolicy := agentruntime.JobRetryPolicy{MaxAttempts: 3, InitialBackoffMillis: 250, MaximumBackoffMillis: 1000}
		run := func(ctx context.Context) (any, error) {
			return s.svc.CheckMCPServer(ctx, p, serverID)
		}
		job, admissionErr := s.admitUserJob(r, p, jobKind, agentruntime.JobRecoveryPolicyFail, retryPolicy, map[string]string{"server_id": serverID}, run)
		if admissionErr != nil {
			writeAsyncJobAdmissionError(w, admissionErr)
			return
		}
		writeJSON(w, http.StatusAccepted, job)
		return
	}
	out, err := s.svc.CheckMCPServer(r.Context(), p, serverID)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, sanitizeMCPServerViewPtrForAPI(s.svc.DataRoot(), out))
}

func (s *HTTPServer) handleGetMCPServerTools(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.GetMCPServerTools(r.Context(), p, r.PathValue("serverId"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}
