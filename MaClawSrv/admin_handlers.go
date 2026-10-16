package main

import (
	"errors"
	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	coreconfig "github.com/RapidAI/CodeClaw/corelib/config"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *HTTPServer) handleGetAdminReadiness(w http.ResponseWriter, r *http.Request) {
	report := buildReadinessReport(s.svc.DataRoot(), s.jobs.repositoryPath)
	if s.jobs != nil && !s.jobs.persistenceHealthy() {
		report.Status = "not_ready"
		report.Checks = append(report.Checks, readinessCheck{Name: "jobs_store_persistence", Status: "fail", Error: "async job store persistence unavailable"})
	}
	status := http.StatusOK
	if report.Status != "ready" {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, redactReadinessReport(report))
}

func (s *HTTPServer) handleGetAdminOverview(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetAdminOverview(r.Context())
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleGetAdminDashboard(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetAdminDashboard(r.Context())
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	dashboard := redactAdminDashboardForAdminAPI(s.svc.DataRoot(), *out)
	writeJSON(w, http.StatusOK, dashboard)
}

func (s *HTTPServer) handleGetAdminInsights(w http.ResponseWriter, r *http.Request) {
	limit := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid limit"})
			return
		}
		limit = parsed
	}
	inactiveForDays := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("inactive_for_days")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid inactive_for_days"})
			return
		}
		inactiveForDays = parsed
	}
	out, err := s.svc.GetAdminInsights(r.Context(), agentservice.AdminInsightsInput{InactiveForDays: inactiveForDays, Limit: limit})
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleGetAdminAlerts(w http.ResponseWriter, r *http.Request) {
	var since *time.Time
	sinceRaw := strings.TrimSpace(r.URL.Query().Get("since"))
	if sinceRaw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, sinceRaw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid since"})
			return
		}
		since = &parsed
	}
	limit := 0
	limitRaw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if limitRaw != "" {
		parsed, err := strconv.Atoi(limitRaw)
		if err != nil || parsed < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid limit"})
			return
		}
		limit = parsed
	}
	expiryWindowDays := 0
	expiryWindowRaw := strings.TrimSpace(r.URL.Query().Get("credential_expiry_window_days"))
	if expiryWindowRaw != "" {
		parsed, err := strconv.Atoi(expiryWindowRaw)
		if err != nil || parsed < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid credential_expiry_window_days"})
			return
		}
		expiryWindowDays = parsed
	}
	out, err := s.svc.GetAdminAlerts(r.Context(), agentservice.AdminAlertsInput{
		TenantID:                   strings.TrimSpace(r.URL.Query().Get("tenant_id")),
		UserID:                     strings.TrimSpace(r.URL.Query().Get("user_id")),
		Kind:                       strings.TrimSpace(r.URL.Query().Get("kind")),
		Since:                      since,
		Limit:                      limit,
		CredentialExpiryWindowDays: expiryWindowDays,
	})
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, sanitizeAdminAlertsForAdminAPI(s.svc.DataRoot(), *out))
}

func (s *HTTPServer) handleAdminSecuritySummary(w http.ResponseWriter, r *http.Request) {
	since, err := parseOptionalTimeQuery(r, "since")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	until, err := parseOptionalTimeQuery(r, "until")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := validateOptionalTimeRange(since, until); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	items, err := s.loadAdminRiskEvents(r.Context(), since, until)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	counts := countRiskEventsBySeverity(items)
	kindCounts := countRiskEventsByKind(items)
	status := "ok"
	if counts["high"] > 0 {
		status = "critical"
	} else if counts["medium"] > 0 {
		status = "warn"
	}
	recent := items
	if len(recent) > 10 {
		recent = recent[:10]
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"generated_at": time.Now().UTC(),
		"filters":      map[string]any{"since": since, "until": until},
		"status":       status,
		"total":        len(items),
		"counts":       counts,
		"kind_counts":  kindCounts,
		"recent":       recent,
	})
}

func (s *HTTPServer) handleAdminSecurityRiskEvents(w http.ResponseWriter, r *http.Request) {
	since, err := parseOptionalTimeQuery(r, "since")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	until, err := parseOptionalTimeQuery(r, "until")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := validateOptionalTimeRange(since, until); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	severity := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("severity")))
	if severity != "" && !isValidRiskSeverity(severity) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid severity"})
		return
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid limit"})
			return
		}
		limit = parsed
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}
	items, err := s.loadAdminRiskEvents(r.Context(), since, until)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	if severity != "" {
		items = filterRiskEventsBySeverity(items, severity)
	}
	kind := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("kind")))
	if kind != "" {
		items = filterRiskEventsByKind(items, kind)
	}
	total := len(items)
	counts := countRiskEventsBySeverity(items)
	kindCounts := countRiskEventsByKind(items)
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	writeJSON(w, http.StatusOK, map[string]any{"generated_at": time.Now().UTC(), "filters": map[string]any{"severity": severity, "kind": kind, "since": since, "until": until, "limit": limit}, "items": items, "total": total, "counts": counts, "kind_counts": kindCounts})
}

func (s *HTTPServer) handleListTenants(w http.ResponseWriter, r *http.Request) {
	status, ok := parseTenantStatus(r.URL.Query().Get("status"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid status"})
		return
	}
	out, err := s.svc.ListTenants(r.Context(), agentservice.ListTenantsInput{
		Status: status,
		Name:   strings.TrimSpace(r.URL.Query().Get("name")),
	})
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	page, err := parsePageQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	items, meta := paginateTenants(out, page)
	writeJSON(w, http.StatusOK, listResponse(items, meta))
}

func (s *HTTPServer) handleListAuditEvents(w http.ResponseWriter, r *http.Request) {
	since, err := parseOptionalTimeQuery(r, "since")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	until, err := parseOptionalTimeQuery(r, "until")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	out, err := s.svc.ListAuditEvents(r.Context(), agentservice.ListAuditEventsInput{
		TenantID:     strings.TrimSpace(r.URL.Query().Get("tenant_id")),
		UserID:       strings.TrimSpace(r.URL.Query().Get("user_id")),
		Action:       strings.TrimSpace(r.URL.Query().Get("action")),
		ResourceType: strings.TrimSpace(r.URL.Query().Get("resource_type")),
		ResourceID:   strings.TrimSpace(r.URL.Query().Get("resource_id")),
		ActorType:    strings.TrimSpace(r.URL.Query().Get("actor_type")),
		ActorTenant:  strings.TrimSpace(r.URL.Query().Get("actor_tenant_id")),
		ActorUser:    strings.TrimSpace(r.URL.Query().Get("actor_user_id")),
		Since:        since,
		Until:        until,
	})
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	page, err := parsePageQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	items, meta := paginateAuditEvents(out, page)
	items = redactAuditEventsForAdminAPI(s.svc.DataRoot(), items)
	writeJSON(w, http.StatusOK, listResponse(items, meta))
}

func (s *HTTPServer) handleExportServiceState(w http.ResponseWriter, r *http.Request) {
	includeMessages, err := parseOptionalBoolQuery(r, "include_messages")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	includeRuns, err := parseOptionalBoolQuery(r, "include_runs")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	includeAudit, err := parseOptionalBoolQuery(r, "include_audit")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	includeSecrets, err := parseOptionalBoolQuery(r, "include_secrets")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !s.requireSecretExportAccess(w, r, includeSecrets != nil && *includeSecrets) {
		return
	}
	out, err := s.svc.ExportServiceState(r.Context(), agentservice.ExportServiceStateInput{
		TenantID:        strings.TrimSpace(r.URL.Query().Get("tenant_id")),
		UserID:          strings.TrimSpace(r.URL.Query().Get("user_id")),
		IncludeMessages: includeMessages == nil || *includeMessages,
		IncludeRuns:     includeRuns == nil || *includeRuns,
		IncludeAudit:    includeAudit == nil || *includeAudit,
		IncludeSecrets:  includeSecrets != nil && *includeSecrets,
	})
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.service_state_exported", "service_state", out.Scope, map[string]string{"tenant_id": out.TenantID, "user_id": out.UserID, "include_secrets": strconv.FormatBool(out.IncludeSecrets), "include_messages": strconv.FormatBool(out.IncludeMessages), "include_runs": strconv.FormatBool(out.IncludeRuns), "include_audit": strconv.FormatBool(out.IncludeAudit), "users": strconv.Itoa(len(out.Users)), "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, sanitizeExportServiceStateForAdminAPI(s.svc.DataRoot(), *out))
}

func (s *HTTPServer) handleImportServiceState(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	in, ok := decodeImportStateRequest(w, r)
	if !ok {
		return
	}
	if overwrite, err := parseOptionalBoolQuery(r, "overwrite"); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	} else if overwrite != nil {
		in.Overwrite = *overwrite
	}
	if dryRun, err := parseOptionalBoolQuery(r, "dry_run"); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	} else if dryRun != nil {
		in.DryRun = *dryRun
	}
	if !in.DryRun {
		if err := requireAdminConfirmation(r, "import operations"); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}
	out, err := s.svc.ImportServiceState(r.Context(), *in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.service_state_imported", "service_state", out.Scope, map[string]string{"tenant_id": out.TenantID, "user_id": out.UserID, "dry_run": strconv.FormatBool(out.DryRun), "overwrite": strconv.FormatBool(out.Overwrite), "tenants": strconv.Itoa(out.Tenants), "users": strconv.Itoa(out.Users), "credentials": strconv.Itoa(out.Credentials), "instances": strconv.Itoa(out.Instances), "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, sanitizeImportServiceStateOutputForAdminAPI(s.svc.DataRoot(), out))
}

func (s *HTTPServer) handleListServiceSnapshots(w http.ResponseWriter, r *http.Request) {
	since, err := parseOptionalTimeQuery(r, "since")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	until, err := parseOptionalTimeQuery(r, "until")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	items, err := s.svc.ListServiceSnapshots(r.Context(), agentservice.ListServiceSnapshotsInput{
		TenantID: strings.TrimSpace(r.URL.Query().Get("tenant_id")),
		UserID:   strings.TrimSpace(r.URL.Query().Get("user_id")),
		Scope:    strings.TrimSpace(r.URL.Query().Get("scope")),
		Name:     strings.TrimSpace(r.URL.Query().Get("name")),
		Since:    since,
		Until:    until,
	})
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	page, err := parsePageQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	window, meta := paginateServiceSnapshots(items, page)
	writeJSON(w, http.StatusOK, listResponse(sanitizeServiceSnapshotsForAdminAPI(window), meta))
}

func (s *HTTPServer) handleCreateServiceSnapshot(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	var in agentservice.CreateServiceSnapshotInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.IncludeSecrets != nil && *in.IncludeSecrets {
		if err := requireAdminConfirmation(r, "secret snapshot operations"); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}
	out, err := s.svc.CreateServiceSnapshot(r.Context(), in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.snapshot_created", "snapshot", out.Snapshot.ID, map[string]string{"scope": out.Snapshot.Scope, "tenant_id": out.Snapshot.TenantID, "user_id": out.Snapshot.UserID, "include_secrets": strconv.FormatBool(out.Snapshot.IncludeSecrets), "include_messages": strconv.FormatBool(out.Snapshot.IncludeMessages), "include_runs": strconv.FormatBool(out.Snapshot.IncludeRuns), "include_audit": strconv.FormatBool(out.Snapshot.IncludeAudit), "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusCreated, sanitizeServiceSnapshotEnvelopeForAdminAPI(s.svc.DataRoot(), out))
}

func (s *HTTPServer) handlePruneServiceSnapshots(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	var in agentservice.PruneServiceSnapshotsInput
	if !decodeOptionalJSON(w, r, &in) {
		return
	}
	if tenantID := strings.TrimSpace(r.URL.Query().Get("tenant_id")); tenantID != "" {
		in.TenantID = tenantID
	}
	if userID := strings.TrimSpace(r.URL.Query().Get("user_id")); userID != "" {
		in.UserID = userID
	}
	if olderThan, err := parseOptionalTimeQuery(r, "older_than"); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	} else if olderThan != nil {
		in.OlderThan = olderThan
	}
	if keepLatestRaw := strings.TrimSpace(r.URL.Query().Get("keep_latest")); keepLatestRaw != "" {
		keepLatest, err := strconv.Atoi(keepLatestRaw)
		if err != nil || keepLatest < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "keep_latest must be greater than or equal to 0"})
			return
		}
		in.KeepLatest = keepLatest
	}
	if dryRun, err := parseOptionalBoolQuery(r, "dry_run"); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	} else if dryRun != nil {
		in.DryRun = *dryRun
	}
	out, err := s.svc.PruneServiceSnapshots(r.Context(), in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, sanitizePruneServiceSnapshotsOutputForAdminAPI(out))
}

func (s *HTTPServer) handleGetServiceSnapshot(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetServiceSnapshot(r.Context(), r.PathValue("snapshotId"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	if out.Snapshot.IncludeSecrets && !s.requireAdminOwner(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, sanitizeServiceSnapshotEnvelopeForAdminAPI(s.svc.DataRoot(), out))
}

func (s *HTTPServer) handleRestoreServiceSnapshot(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	var in agentservice.RestoreServiceSnapshotInput
	if !decodeOptionalJSON(w, r, &in) {
		return
	}
	if overwrite, err := parseOptionalBoolQuery(r, "overwrite"); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	} else if overwrite != nil {
		in.Overwrite = *overwrite
	}
	if dryRun, err := parseOptionalBoolQuery(r, "dry_run"); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	} else if dryRun != nil {
		in.DryRun = *dryRun
	}
	if !in.DryRun {
		if err := requireAdminConfirmation(r, "restore operations"); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}
	out, err := s.svc.RestoreServiceSnapshot(r.Context(), r.PathValue("snapshotId"), in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.snapshot_restored", "snapshot", out.Snapshot.ID, map[string]string{"scope": out.Snapshot.Scope, "tenant_id": out.Snapshot.TenantID, "user_id": out.Snapshot.UserID, "dry_run": strconv.FormatBool(in.DryRun), "overwrite": strconv.FormatBool(in.Overwrite), "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, sanitizeRestoreServiceSnapshotOutputForAdminAPI(s.svc.DataRoot(), out))
}

func (s *HTTPServer) handleDeleteServiceSnapshot(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	if err := requireDeleteConfirmation(r); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	out, err := s.svc.DeleteServiceSnapshot(r.Context(), r.PathValue("snapshotId"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, sanitizeServiceSnapshotForAdminAPI(*out))
}

func (s *HTTPServer) handleGetTenantRetirePlan(w http.ResponseWriter, r *http.Request) {
	in, err := parseExportServiceStateInput(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !s.requireSecretExportAccess(w, r, in.IncludeSecrets) {
		return
	}
	out, err := s.svc.GetTenantRetirePlan(r.Context(), r.PathValue("tenantId"), in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, sanitizeTenantRetirePlanForAdminAPI(s.svc.DataRoot(), *out))
}

func (s *HTTPServer) handleGetUserRetirePlan(w http.ResponseWriter, r *http.Request) {
	in, err := parseExportServiceStateInput(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !s.requireSecretExportAccess(w, r, in.IncludeSecrets) {
		return
	}
	out, err := s.svc.GetUserRetirePlan(r.Context(), r.PathValue("tenantId"), r.PathValue("userId"), in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, sanitizeUserRetirePlanForAdminAPI(s.svc.DataRoot(), *out))
}

func (s *HTTPServer) handleCreateTenant(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	var in agentservice.CreateTenantInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.CreateTenant(r.Context(), in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.tenant_created", "tenant", out.ID, tenantAuditMetadata(r, out))
	writeJSON(w, http.StatusCreated, out)
}

func (s *HTTPServer) handleGetTenant(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetTenant(r.Context(), r.PathValue("tenantId"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleGetTenantSummary(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetTenantSummary(r.Context(), r.PathValue("tenantId"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, sanitizeTenantSummaryForAdminAPI(*out))
}

func (s *HTTPServer) handleGetTenantDeleteCheck(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetTenantDeleteCheck(r.Context(), r.PathValue("tenantId"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleUpdateTenant(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	var in agentservice.UpdateTenantInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateTenant(r.Context(), r.PathValue("tenantId"), in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.tenant_updated", "tenant", out.ID, tenantAuditMetadata(r, out))
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handlePauseTenant(w http.ResponseWriter, r *http.Request) {
	s.updateTenantLifecycleStatus(w, r, agentservice.TenantStatusDisabled, "admin.tenant_paused")
}

func (s *HTTPServer) handleResumeTenant(w http.ResponseWriter, r *http.Request) {
	s.updateTenantLifecycleStatus(w, r, agentservice.TenantStatusActive, "admin.tenant_resumed")
}

func (s *HTTPServer) handleDeleteTenant(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	if err := requireDeleteConfirmation(r); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	tenantID := r.PathValue("tenantId")
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("force")), "true") {
		var in adminForceDeleteRequest
		if !decodeJSON(w, r, &in) {
			return
		}
		if !s.requireAdminForceDelete(w, r, in) {
			return
		}
		check, err := s.svc.GetTenantDeleteCheck(r.Context(), tenantID)
		if err != nil {
			writeRedactedError(w, err, s.svc.DataRoot())
			return
		}
		if blockers := nonDeleteProtectionBlockers(check.Blockers); len(blockers) > 0 {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "tenant has active delete blockers", "blockers": blockers})
			return
		}
		unprotected := false
		if _, err := s.svc.UpdateTenant(r.Context(), tenantID, agentservice.UpdateTenantInput{DeleteProtected: &unprotected}); err != nil {
			writeRedactedError(w, err, s.svc.DataRoot())
			return
		}
		users, err := s.svc.ListUsers(r.Context(), tenantID, agentservice.ListUsersAdminInput{})
		if err != nil {
			writeRedactedError(w, err, s.svc.DataRoot())
			return
		}
		for _, user := range users {
			if user.DeleteProtected {
				if _, err := s.svc.UpdateUser(r.Context(), tenantID, user.ID, agentservice.UpdateUserInput{DeleteProtected: &unprotected}); err != nil {
					writeRedactedError(w, err, s.svc.DataRoot())
					return
				}
			}
		}
		s.stopWeixinRuntimesForTenant(r.Context(), tenantID)
		s.stopIMRuntimesForTenant(r.Context(), tenantID)
		s.stopThirdPartyIMForTenant(tenantID)
		if err := s.svc.DeleteTenant(r.Context(), tenantID); err != nil {
			writeRedactedError(w, err, s.svc.DataRoot())
			return
		}
		_ = s.recordAdminAudit(r.Context(), "admin.tenant_force_deleted", "tenant", tenantID, map[string]string{"users": strconv.Itoa(len(users)), "remote_ip": requestClientIP(r)})
		writeJSON(w, http.StatusOK, map[string]any{"status": "deleted", "forced": true, "users_deleted": len(users)})
		return
	}
	s.stopWeixinRuntimesForTenant(r.Context(), tenantID)
	s.stopIMRuntimesForTenant(r.Context(), tenantID)
	s.stopThirdPartyIMForTenant(tenantID)
	if err := s.svc.DeleteTenant(r.Context(), tenantID); err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.tenant_deleted", "tenant", tenantID, map[string]string{"remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *HTTPServer) handleListAllUsers(w http.ResponseWriter, r *http.Request) {
	status, ok := parseUserStatus(r.URL.Query().Get("status"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid status"})
		return
	}
	out, err := s.svc.ListAllUsers(r.Context(), agentservice.ListAllUsersAdminInput{
		TenantID: strings.TrimSpace(r.URL.Query().Get("tenant_id")),
		Status:   status,
		Name:     strings.TrimSpace(r.URL.Query().Get("name")),
		Email:    strings.TrimSpace(r.URL.Query().Get("email")),
	})
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	page, err := parsePageQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	items, meta := paginateUsers(out, page)
	writeJSON(w, http.StatusOK, listResponse(items, meta))
}

func (s *HTTPServer) handleListUsers(w http.ResponseWriter, r *http.Request) {
	status, ok := parseUserStatus(r.URL.Query().Get("status"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid status"})
		return
	}
	out, err := s.svc.ListUsers(r.Context(), r.PathValue("tenantId"), agentservice.ListUsersAdminInput{
		Status: status,
		Name:   strings.TrimSpace(r.URL.Query().Get("name")),
		Email:  strings.TrimSpace(r.URL.Query().Get("email")),
	})
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	page, err := parsePageQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	items, meta := paginateUsers(out, page)
	writeJSON(w, http.StatusOK, listResponse(items, meta))
}

func (s *HTTPServer) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	var in agentservice.CreateUserInput
	if !decodeJSON(w, r, &in) {
		return
	}
	in.TenantID = r.PathValue("tenantId")
	out, err := s.svc.CreateUser(r.Context(), in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.user_created", "user", out.ID, userAuditMetadata(r, out))
	writeJSON(w, http.StatusCreated, out)
}

func (s *HTTPServer) handleGetUser(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetUser(r.Context(), r.PathValue("tenantId"), r.PathValue("userId"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleAdminGetUserConfigSchema(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetParameterDefinitions(r.Context(), s.adminUserPrincipal(r))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *HTTPServer) handleAdminGetUserConfig(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetUserConfig(r.Context(), s.adminUserPrincipal(r))
	if err != nil {
		if errors.Is(err, agentservice.ErrUserConfigNotFound) {
			writeJSON(w, http.StatusOK, map[string]any{"app_config": forceSrvAIAutoEnabledConfig(corelib.AppConfigDefaults())})
			return
		}
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	out.AppConfig = forceSrvAIAutoEnabledConfig(out.AppConfig)
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleAdminUpdateUserConfig(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	inPtr, ok := decodeOptionalAppConfig(w, r)
	if !ok {
		return
	}
	if inPtr == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "empty or invalid config body"})
		return
	}
	p := s.adminUserPrincipal(r)
	next := forceSrvAIAutoEnabledConfig(*inPtr)
	if err := s.validateThirdPartyGatewayTokenUnique(r.Context(), p, next); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	before, _ := s.svc.GetRawUserConfig(r.Context(), p)
	out, err := s.svc.UpdateUserConfig(r.Context(), p, next)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	s.syncWeixinRuntimeFromRawConfig(r.Context(), p)
	s.syncIMRuntimeFromRawConfig(r.Context(), p)
	beforeCfg := corelib.AppConfig{}
	if before != nil {
		beforeCfg = before.AppConfig
	}
	after, _ := s.svc.GetRawUserConfig(r.Context(), p)
	afterCfg := next
	if after != nil {
		afterCfg = after.AppConfig
	}
	s.syncThirdPartyIMConfigTransition(p, beforeCfg, afterCfg)
	s.ensureConfiguredAIModelsAsync(out.AppConfig)
	_ = s.recordAdminAudit(r.Context(), "admin.user_config_updated", "user", r.PathValue("userId"), map[string]string{"tenant_id": r.PathValue("tenantId"), "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleAdminValidateUserConfig(w http.ResponseWriter, r *http.Request) {
	candidate, ok := decodeOptionalAppConfig(w, r)
	if !ok {
		return
	}
	out, err := s.svc.ValidateConfigCandidate(r.Context(), s.adminUserPrincipal(r), candidate)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleAdminTestUserConfig(w http.ResponseWriter, r *http.Request) {
	candidate, ok := decodeOptionalAppConfig(w, r)
	if !ok {
		return
	}
	out, err := s.svc.TestConfigCandidate(r.Context(), s.adminUserPrincipal(r), candidate)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, sanitizeConfigTestResultForAPI(s.svc.DataRoot(), out))
}

func (s *HTTPServer) handleAdminGetClientConfigSchema(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"schema_version": coreconfig.AppConfigSchemaVersion, "items": agentservice.SharedClientParameterDefinitions()})
}

func (s *HTTPServer) handleAdminGetDefaultClientConfig(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetDefaultClientConfig(r.Context())
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	cfg := *out
	cfg.AppConfig = forceSrvAIAutoEnabledConfig(agentservice.SanitizeAppConfig(cfg.AppConfig))
	writeJSON(w, http.StatusOK, cfg)
}

func (s *HTTPServer) handleAdminUpdateDefaultClientConfig(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	current, err := s.svc.GetDefaultClientConfig(r.Context())
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	base := current.AppConfig
	if current.UpdatedAt.IsZero() {
		base = corelib.AppConfigDefaults()
	}
	inPtr, ok := decodeOptionalAppConfigWithBase(w, r, base)
	if !ok {
		return
	}
	if inPtr == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "empty or invalid config body"})
		return
	}
	next := forceSrvAIAutoEnabledConfig(*inPtr)
	out, err := s.svc.UpdateDefaultClientConfig(r.Context(), next)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.default_client_config_updated", "default_client_config", "global", map[string]string{"remote_ip": requestClientIP(r)})
	s.ensureConfiguredAIModelsAsync(out.AppConfig)
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleAdminValidateDefaultClientConfig(w http.ResponseWriter, r *http.Request) {
	current, err := s.svc.GetDefaultClientConfig(r.Context())
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	base := current.AppConfig
	if current.UpdatedAt.IsZero() {
		base = corelib.AppConfigDefaults()
	}
	candidate, ok := decodeOptionalAppConfigWithBase(w, r, base)
	if !ok {
		return
	}
	cfg := corelib.AppConfig{}
	if candidate != nil {
		cfg = agentservice.SharedClientAppConfigOnly(*candidate)
	}
	writeJSON(w, http.StatusOK, agentservice.ValidateAppConfig(cfg))
}

func (s *HTTPServer) handleGetUserDeleteCheck(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetUserDeleteCheck(r.Context(), r.PathValue("tenantId"), r.PathValue("userId"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	var in agentservice.UpdateUserInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateUser(r.Context(), r.PathValue("tenantId"), r.PathValue("userId"), in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.user_updated", "user", out.ID, userAuditMetadata(r, out))
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handlePauseUser(w http.ResponseWriter, r *http.Request) {
	s.updateUserLifecycleStatus(w, r, agentservice.UserStatusDisabled, "admin.user_paused")
}

func (s *HTTPServer) handleResumeUser(w http.ResponseWriter, r *http.Request) {
	s.updateUserLifecycleStatus(w, r, agentservice.UserStatusActive, "admin.user_resumed")
}

func (s *HTTPServer) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	if err := requireDeleteConfirmation(r); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	tenantID := r.PathValue("tenantId")
	userID := r.PathValue("userId")
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("force")), "true") {
		var in adminForceDeleteRequest
		if !decodeJSON(w, r, &in) {
			return
		}
		if !s.requireAdminForceDelete(w, r, in) {
			return
		}
		check, err := s.svc.GetUserDeleteCheck(r.Context(), tenantID, userID)
		if err != nil {
			writeRedactedError(w, err, s.svc.DataRoot())
			return
		}
		if blockers := nonDeleteProtectionBlockers(check.Blockers); len(blockers) > 0 {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "user has active delete blockers", "blockers": blockers})
			return
		}
		unprotected := false
		if _, err := s.svc.UpdateUser(r.Context(), tenantID, userID, agentservice.UpdateUserInput{DeleteProtected: &unprotected}); err != nil {
			writeRedactedError(w, err, s.svc.DataRoot())
			return
		}
		s.stopWeixinRuntimeForPrincipal(agentservice.Principal{TenantID: tenantID, UserID: userID})
		s.stopIMRuntimeForPrincipal(agentservice.Principal{TenantID: tenantID, UserID: userID})
		s.stopThirdPartyIMForPrincipal(agentservice.Principal{TenantID: tenantID, UserID: userID})
		if err := s.svc.DeleteUser(r.Context(), tenantID, userID); err != nil {
			writeRedactedError(w, err, s.svc.DataRoot())
			return
		}
		_ = s.recordAdminAudit(r.Context(), "admin.user_force_deleted", "user", userID, map[string]string{"tenant_id": tenantID, "remote_ip": requestClientIP(r)})
		writeJSON(w, http.StatusOK, map[string]any{"status": "deleted", "forced": true})
		return
	}
	s.stopWeixinRuntimeForPrincipal(agentservice.Principal{TenantID: tenantID, UserID: userID})
	s.stopIMRuntimeForPrincipal(agentservice.Principal{TenantID: tenantID, UserID: userID})
	s.stopThirdPartyIMForPrincipal(agentservice.Principal{TenantID: tenantID, UserID: userID})
	if err := s.svc.DeleteUser(r.Context(), tenantID, userID); err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.user_deleted", "user", userID, map[string]string{"tenant_id": tenantID, "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *HTTPServer) handleListCredentials(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListCredentials(r.Context(), r.PathValue("tenantId"), r.PathValue("userId"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	out, err = filterCredentialsByQuery(out, r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	page, err := parsePageQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	items, meta := paginateCredentials(out, page)
	writeJSON(w, http.StatusOK, listResponse(items, meta))
}

func (s *HTTPServer) handleCreateCredential(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	var in agentservice.CreateCredentialInput
	if !decodeJSON(w, r, &in) {
		return
	}
	in.TenantID = r.PathValue("tenantId")
	in.UserID = r.PathValue("userId")
	out, err := s.svc.CreateCredential(r.Context(), in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.credential_created", "credential", out.ID, credentialAuditMetadata(r, out))
	writeJSON(w, http.StatusCreated, out)
}

func (s *HTTPServer) handleGetCredential(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetCredential(r.Context(), r.PathValue("tenantId"), r.PathValue("userId"), r.PathValue("credentialId"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleUpdateCredential(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	var in agentservice.UpdateCredentialInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateCredential(r.Context(), r.PathValue("tenantId"), r.PathValue("userId"), r.PathValue("credentialId"), in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.credential_updated", "credential", out.ID, credentialAuditMetadata(r, out))
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleRotateCredentialSecret(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	var in agentservice.RotateCredentialSecretInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.RotateCredentialSecret(r.Context(), r.PathValue("tenantId"), r.PathValue("userId"), r.PathValue("credentialId"), in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.credential_secret_rotated", "credential", out.ID, credentialAuditMetadata(r, out))
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleRotateCredentialKey(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	var in agentservice.RotateCredentialKeyInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.RotateCredentialAPIKey(r.Context(), r.PathValue("tenantId"), r.PathValue("userId"), r.PathValue("credentialId"), in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.credential_key_rotated", "credential", out.ID, credentialAuditMetadata(r, out))
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleRevokeCredential(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	out, err := s.svc.RevokeCredential(r.Context(), r.PathValue("tenantId"), r.PathValue("userId"), r.PathValue("credentialId"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.credential_revoked", "credential", out.ID, credentialAuditMetadata(r, out))
	writeJSON(w, http.StatusOK, out)
}
