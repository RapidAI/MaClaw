package main

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

// admitUserJob is the HTTP-only adapter for the shared Job admission
// identity. Domain workers receive only the digest through their context and
// remain reusable by GUI/TUI hosts.
func (s *HTTPServer) admitUserJob(r *http.Request, p agentservice.Principal, kind string, recoveryPolicy agentruntime.JobRecoveryPolicy, retryPolicy agentruntime.JobRetryPolicy, requestIdentity any, run func(context.Context) (any, error)) (*asyncJobRecord, error) {
	if run == nil {
		return nil, errors.New("async job worker is required")
	}
	// The HTTP request context is short-lived, but its safe correlation id must
	// remain attached to the service-owned worker after admission returns. Only
	// the validated opaque id is copied; cancellation/deadlines are deliberately
	// not propagated to avoid aborting an admitted Job when the client closes.
	correlationID := ""
	if r != nil {
		correlationID = agentruntime.CorrelationID(r.Context())
	}
	worker := run
	if correlationID != "" {
		worker = func(ctx context.Context) (any, error) {
			return run(agentruntime.WithCorrelationID(ctx, correlationID))
		}
	}
	if r == nil {
		return s.jobs.createUserJobWithPolicies(kind, p, recoveryPolicy, retryPolicy, worker), nil
	}
	idempotencyValues := r.Header.Values("Idempotency-Key")
	if len(idempotencyValues) == 0 {
		return s.jobs.createUserJobWithPolicies(kind, p, recoveryPolicy, retryPolicy, worker), nil
	}
	if len(idempotencyValues) != 1 {
		return nil, agentruntime.ErrInvalidJobIdempotency
	}
	idempotencyKey := idempotencyValues[0]
	// Do not normalize a caller-provided key before validating it. Leading or
	// trailing whitespace is an ambiguous identity and the shared Runtime
	// contract deliberately rejects it instead of silently addressing a
	// different durable Job.
	if strings.TrimSpace(idempotencyKey) == "" {
		return nil, agentruntime.ErrInvalidJobIdempotency
	}
	identity, err := agentruntime.NewJobIdempotencyIdentity(p.TenantID, p.UserID, kind, idempotencyKey, requestIdentity)
	if err != nil {
		return nil, err
	}
	return s.jobs.createUserJobIdempotent(kind, p, recoveryPolicy, retryPolicy, identity, worker)
}

func writeAsyncJobAdmissionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, agentruntime.ErrInvalidJobIdempotency):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error(), "code": "invalid_job_idempotency_key"})
	case errors.Is(err, agentruntime.ErrJobIdempotencyConflict):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error(), "code": "job_idempotency_conflict"})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "async job admission failed", "code": "job_admission_failed"})
	}
}
