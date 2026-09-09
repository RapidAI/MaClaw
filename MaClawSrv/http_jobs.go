package main

import (
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"net/http"
	"strings"
)

func (s *HTTPServer) handleListAsyncJobs(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	page, err := parsePageQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	kind := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("kind")))
	status, ok := parseAsyncJobStatus(r.URL.Query().Get("status"), false)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid status"})
		return
	}
	out := s.jobs.listUserJobs(p, kind, status)
	items, meta := paginateAsyncJobs(out, page)
	writeJSON(w, http.StatusOK, listResponse(items, meta))
}

func (s *HTTPServer) handleDeleteAsyncJobs(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	kind := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("kind")))
	statusRaw := strings.TrimSpace(r.URL.Query().Get("status"))
	status, ok := parseAsyncJobStatus(statusRaw, true)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "status must be succeeded, failed, or canceled"})
		return
	}
	before, err := parseOptionalTimeQuery(r, "before")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	deleteAll, err := parseOptionalBoolQuery(r, "all")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if (deleteAll == nil || !*deleteAll) && kind == "" && statusRaw == "" && before == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "specify kind, status, before, or all=true"})
		return
	}
	items := s.jobs.deleteUserJobs(p, kind, status, before)
	if err := s.jobs.persistenceError(); err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted", "deleted": len(items), "items": items})
}

func (s *HTTPServer) handleGetAsyncJob(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	job, ok := s.jobs.getUserJob(r.PathValue("jobId"), p)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}
	if job.Status == asyncJobStatusUnknown && (job.Kind == "migration.export" || job.Kind == "migration.import") {
		localReconciler := agentruntime.JobReconciler(migrationExportJobReconciler{server: s})
		if job.Kind == "migration.import" {
			localReconciler = migrationImportJobReconciler{server: s}
		}
		if reconciled, found, reconcileErr := s.jobs.reconcileUserJob(r.Context(), job.ID, p, localReconciler); reconcileErr == nil && found {
			job = reconciled
		}
		if job.Status == asyncJobStatusUnknown {
			if cfg, err := s.migrationConfig(r.Context(), p); err == nil {
				var remoteReconciler agentruntime.JobReconciler = migrationExportJobReconciler{server: s, cfg: cfg}
				if job.Kind == "migration.import" {
					remoteReconciler = migrationImportJobReconciler{server: s, cfg: cfg}
				}
				if reconciled, found, reconcileErr := s.jobs.reconcileUserJob(r.Context(), job.ID, p, remoteReconciler); reconcileErr == nil && found {
					job = reconciled
				}
			}
		}
	}
	if job.Status == asyncJobStatusUnknown {
		reconciler := domainJobReconcilerFor(s, job.Kind)
		if reconciler != nil {
			if reconciled, found, reconcileErr := s.jobs.reconcileUserJob(r.Context(), job.ID, p, reconciler); reconcileErr == nil && found {
				job = reconciled
			}
		}
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *HTTPServer) handleCancelAsyncJob(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	job, ok := s.jobs.cancelUserJob(r.PathValue("jobId"), p)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *HTTPServer) handleDeleteAsyncJob(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	job, found, deleted := s.jobs.deleteUserJob(r.PathValue("jobId"), p)
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}
	if !deleted {
		if job != nil && job.CompletedAt != nil {
			if err := s.jobs.persistenceError(); err != nil {
				writeRedactedError(w, err, s.svc.DataRoot())
				return
			}
		}
		writeJSON(w, http.StatusConflict, map[string]any{"error": "job is still active", "job": job})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted", "job": job})
}
