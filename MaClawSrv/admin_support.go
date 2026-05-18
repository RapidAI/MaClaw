package main

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

var (
	adminSecretKeyPattern            = adminSecretKeyRegexpPattern()
	supportBundleInlineSecretPattern = regexp.MustCompile(`(?i)(` + adminSecretKeyPattern + `)(\s*[:=]\s*)([^\s,;]+)`)
	supportBundleBearerPattern       = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
	supportBundleJSONSecretPattern   = regexp.MustCompile(`(?i)("?(?:` + adminSecretKeyPattern + `)"?\s*:\s*)"[^"]*"`)
)

type adminSupportBundle struct {
	GeneratedAt       time.Time                   `json:"generated_at"`
	Runtime           adminRuntimeStatus          `json:"runtime"`
	Dashboard         agentservice.AdminDashboard `json:"dashboard"`
	SecurityRisks     map[string]any              `json:"security_risks"`
	ServiceConfig     map[string]any              `json:"service_config"`
	LogSources        []adminLogSource            `json:"log_sources"`
	RecentLogErrors   []adminRecentLogLine        `json:"recent_log_errors"`
	RecentAuditEvents []agentservice.AuditEvent   `json:"recent_audit_events"`
	Jobs              map[asyncJobStatus]int      `json:"jobs"`
	Sandbox           map[string]any              `json:"sandbox"`
	DataRootName      string                      `json:"data_root_name"`
	DataRootRedacted  bool                        `json:"data_root_redacted"`
	Redactions        []string                    `json:"redactions"`
	Counts            map[string]int              `json:"counts"`
}

func (s *HTTPServer) handleAdminSupportBundle(w http.ResponseWriter, r *http.Request) {
	download := false
	if raw := strings.TrimSpace(r.URL.Query().Get("download")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid download"})
			return
		}
		download = parsed
	}
	bundle, err := buildAdminSupportBundle(r.Context(), s)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	action := "admin.support_bundle_generated"
	if download {
		action = "admin.support_bundle_downloaded"
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"maclawsrv-support-bundle-%s.json\"", bundle.GeneratedAt.Format("20060102T150405Z")))
	}
	_ = s.recordAdminAudit(r.Context(), action, "support_bundle", "service", map[string]string{"download": strconv.FormatBool(download), "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, bundle)
}

func buildAdminSupportBundle(ctx context.Context, s *HTTPServer) (adminSupportBundle, error) {
	generatedAt := time.Now().UTC()
	runtimeStatus := buildAdminRuntimeStatus(s)
	runtimeStatus.DataRoot = filepath.Base(runtimeStatus.DataRoot)
	runtimeStatus.RuntimeConfigDir = filepath.Base(runtimeStatus.RuntimeConfigDir)
	runtimeStatus.Readiness = redactReadinessReport(runtimeStatus.Readiness)
	runtimeStatus.Scheduler.Path = redactSupportBundleValue(s.svc.DataRoot(), runtimeStatus.Scheduler.Path)
	runtimeStatus.Scheduler.LastError = redactSupportBundleText(s.svc.DataRoot(), runtimeStatus.Scheduler.LastError)
	runtimeStatus.LogSources = redactSupportBundleLogSources(s.svc.DataRoot(), runtimeStatus.LogSources)
	if runtimeStatus.LastSandboxReport != nil {
		report := redactSandboxReportsForSupportBundle(s.svc.DataRoot(), []sandboxDiagnoseReport{*runtimeStatus.LastSandboxReport})
		runtimeStatus.LastSandboxReport = &report[0]
	}
	runtimeStatus.Sandbox = redactSandboxStatusForSupportBundle(s.svc.DataRoot(), runtimeStatus.Sandbox)
	dashboard, err := s.svc.GetAdminDashboard(ctx)
	if err != nil {
		return adminSupportBundle{}, err
	}
	auditEvents, err := s.svc.ListAuditEvents(ctx, agentservice.ListAuditEventsInput{ActorType: "admin"})
	if err != nil {
		return adminSupportBundle{}, err
	}
	sortAuditEventsDesc(auditEvents)
	recentAudit := redactSupportBundleAuditEvents(s.svc.DataRoot(), auditEvents, 20)
	riskItems := buildAdminRiskEvents(s.svc.DataRoot(), auditEvents)
	sortRiskEventsDesc(riskItems)
	riskRecent := redactSupportBundleRiskEvents(s.svc.DataRoot(), riskItems, 20)
	draft, err := loadAdminServiceConfigDraft(s.svc.DataRoot())
	if err != nil {
		return adminSupportBundle{}, err
	}
	validation := validateAdminServiceConfigValues(draft.Values)
	serviceConfig := map[string]any{
		"environment": buildAdminServiceConfigEnvironment(s.svc.DataRoot()),
		"draft":       redactServiceConfigDraftForSupportBundle(s.svc.DataRoot(), draft),
		"validation":  redactServiceConfigValidationForSupportBundle(s.svc.DataRoot(), validation),
	}
	if validation.Valid {
		serviceConfig["diff"] = redactServiceConfigDiffForSupportBundle(buildAdminServiceConfigDiff(s.svc.DataRoot(), validation))
	}
	sandboxReports, err := listSandboxReports(s.svc.DataRoot())
	if err != nil {
		return adminSupportBundle{}, err
	}
	if len(sandboxReports) > 3 {
		sandboxReports = sandboxReports[:3]
	}
	logSources := adminLogSources(s.svc.DataRoot())
	recentLogErrors := recentAdminLogErrors(s.svc.DataRoot(), 20, true)
	bundle := adminSupportBundle{
		GeneratedAt:       generatedAt,
		Runtime:           runtimeStatus,
		Dashboard:         redactAdminDashboardForSupportBundle(s.svc.DataRoot(), *dashboard),
		SecurityRisks:     map[string]any{"generated_at": generatedAt, "filters": map[string]any{"source": "support_bundle", "limit": 20}, "total": len(riskItems), "counts": countRiskEventsBySeverity(riskItems), "kind_counts": countRiskEventsByKind(riskItems), "recent": riskRecent},
		ServiceConfig:     serviceConfig,
		LogSources:        redactSupportBundleLogSources(s.svc.DataRoot(), logSources),
		RecentLogErrors:   redactSupportBundleLogLines(s.svc.DataRoot(), recentLogErrors),
		RecentAuditEvents: recentAudit,
		Jobs:              s.jobs.snapshotCounts(),
		Sandbox:           map[string]any{"status": redactSandboxStatusForSupportBundle(s.svc.DataRoot(), buildSandboxStatus(s.svc.DataRoot(), false)), "reports": redactSandboxReportsForSupportBundle(s.svc.DataRoot(), sandboxReports)},
		DataRootName:      filepath.Base(s.svc.DataRoot()),
		DataRootRedacted:  true,
		Redactions:        []string{"data_root", "runtime_config_dir", "sensitive_service_config_values", "runtime_dumps", "audit_metadata", "log_line_secrets", "sandbox_report_raw"},
		Counts:            map[string]int{"audit_events": len(recentAudit), "risk_events": len(riskItems), "log_sources": len(logSources), "recent_log_errors": len(recentLogErrors), "sandbox_reports": len(sandboxReports)},
	}
	return bundle, nil
}

func sortAuditEventsDesc(events []agentservice.AuditEvent) {
	sort.SliceStable(events, func(i, j int) bool { return events[i].CreatedAt.After(events[j].CreatedAt) })
}

func sortRiskEventsDesc(events []adminRiskEvent) {
	sort.SliceStable(events, func(i, j int) bool { return events[i].CreatedAt.After(events[j].CreatedAt) })
}

func redactReadinessReport(in readinessReport) readinessReport {
	out := in
	out.DataRoot = filepath.Base(out.DataRoot)
	for i := range out.Checks {
		if out.Checks[i].Path != "" {
			out.Checks[i].Path = filepath.Base(out.Checks[i].Path)
		}
	}
	return out
}

func redactSupportBundleAuditEvents(dataRoot string, events []agentservice.AuditEvent, limit int) []agentservice.AuditEvent {
	if limit <= 0 || len(events) == 0 {
		return []agentservice.AuditEvent{}
	}
	if len(events) < limit {
		limit = len(events)
	}
	out := make([]agentservice.AuditEvent, 0, limit)
	for _, event := range events {
		copyEvent := event
		copyEvent.ResourceID = redactSupportBundleValue(dataRoot, copyEvent.ResourceID)
		copyEvent.Metadata = redactSupportBundleMetadata(dataRoot, event.Metadata)
		out = append(out, copyEvent)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func redactSupportBundleRiskEvents(dataRoot string, events []adminRiskEvent, limit int) []adminRiskEvent {
	if limit <= 0 || len(events) == 0 {
		return []adminRiskEvent{}
	}
	if len(events) < limit {
		limit = len(events)
	}
	out := make([]adminRiskEvent, 0, limit)
	for _, event := range events {
		copyEvent := event
		copyEvent.ResourceID = redactSupportBundleValue(dataRoot, copyEvent.ResourceID)
		copyEvent.Metadata = redactSupportBundleMetadata(dataRoot, event.Metadata)
		out = append(out, copyEvent)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func redactSupportBundleMetadata(dataRoot string, metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	out := make(map[string]string, len(metadata))
	for key, value := range metadata {
		if supportBundleSensitiveKey(key) {
			out[key] = "[redacted]"
			continue
		}
		out[key] = redactSupportBundleValue(dataRoot, value)
	}
	return out
}

func supportBundleSensitiveKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	for _, marker := range adminSecretKeyMarkers() {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return supportBundleAuthSecretKey(key)
}

func supportBundleAuthSecretKey(key string) bool {
	key = strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(strings.ToLower(strings.TrimSpace(key)))
	switch key {
	case "auth", "authentication", "auth_header", "authheader", "authentication_header":
		return true
	default:
		return false
	}
}

func adminSecretKeyMarkers() []string {
	return adminSecretKeyMarkersList[:]
}

var adminSecretKeyMarkersList = [...]string{"secret", "token", "password", "passwd", "authorization", "cookie", "bearer", "private", "api_key", "api-key", "apikey", "api_secret", "api-secret", "apisecret"}

func adminSecretKeyRegexpPattern() string {
	parts := make([]string, 0, len(adminSecretKeyMarkersList)+3)
	for _, marker := range adminSecretKeyMarkersList {
		parts = append(parts, regexp.QuoteMeta(marker))
	}
	parts = append(parts, `api[-_\s]?key`, `api[-_\s]?secret`, `auth`)
	sort.Slice(parts, func(i, j int) bool {
		return len(parts[i]) > len(parts[j])
	})
	return strings.Join(parts, "|")
}

func redactSupportBundleValue(dataRoot, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	value = redactSupportBundleText(dataRoot, value)
	if supportBundleLooksAbsolutePath(value) {
		return supportBundlePathBase(value)
	}
	return value
}

func supportBundleLooksAbsolutePath(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if filepath.IsAbs(value) || strings.HasPrefix(value, "/") || strings.HasPrefix(value, `\`) {
		return true
	}
	return len(value) >= 3 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' && (value[2] == '\\' || value[2] == '/')
}

func supportBundlePathBase(value string) string {
	if value = strings.TrimSpace(value); value == "" {
		return value
	}
	base := filepath.Base(value)
	if base != value {
		return base
	}
	return path.Base(strings.ReplaceAll(value, `\`, "/"))
}

func redactSupportBundleText(dataRoot, text string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	if dataRoot = strings.TrimSpace(dataRoot); dataRoot != "" {
		base := supportBundlePathBase(dataRoot)
		for _, variant := range supportBundlePathRedactionVariants(dataRoot) {
			text = strings.ReplaceAll(text, variant, base)
		}
	}
	text = supportBundleBearerPattern.ReplaceAllString(text, "Bearer [redacted]")
	text = supportBundleJSONSecretPattern.ReplaceAllString(text, `${1}"[redacted]"`)
	return supportBundleInlineSecretPattern.ReplaceAllString(text, `${1}${2}[redacted]`)
}

func supportBundlePathRedactionVariants(value string) []string {
	seen := map[string]bool{}
	variants := make([]string, 0, 6)
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			return
		}
		seen[v] = true
		variants = append(variants, v)
	}
	add(value)
	add(filepath.ToSlash(value))
	add(filepath.FromSlash(value))
	for _, v := range append([]string(nil), variants...) {
		add(strings.ReplaceAll(v, `\`, `\\`))
	}
	return variants
}

func redactSupportBundleLogSources(dataRoot string, sources []adminLogSource) []adminLogSource {
	out := make([]adminLogSource, 0, len(sources))
	for _, source := range sources {
		source.Path = redactSupportBundleValue(dataRoot, source.Path)
		out = append(out, source)
	}
	return out
}

func redactSupportBundleLogLines(dataRoot string, lines []adminRecentLogLine) []adminRecentLogLine {
	out := make([]adminRecentLogLine, 0, len(lines))
	for _, line := range lines {
		line.Line.Text = redactSupportBundleText(dataRoot, line.Line.Text)
		out = append(out, line)
	}
	return out
}

func redactAdminSandboxConfigForSupportBundle(dataRoot string, cfg adminSandboxConfigResponse) adminSandboxConfigResponse {
	cfg.Reason = redactSupportBundleText(dataRoot, cfg.Reason)
	return cfg
}

func redactSandboxStatusForSupportBundle(dataRoot string, status sandboxStatus) sandboxStatus {
	for i := range status.Capabilities {
		status.Capabilities[i].Path = redactSupportBundleValue(dataRoot, status.Capabilities[i].Path)
		status.Capabilities[i].Detail = redactSupportBundleText(dataRoot, status.Capabilities[i].Detail)
		status.Capabilities[i].Raw = redactSupportBundleText(dataRoot, status.Capabilities[i].Raw)
	}
	for i := range status.Backends {
		status.Backends[i].Path = redactSupportBundleValue(dataRoot, status.Backends[i].Path)
	}
	return status
}

func redactSandboxReportsForAdminAPI(dataRoot string, reports []sandboxDiagnoseReport) []sandboxDiagnoseReport {
	return redactSandboxReportsForSupportBundle(dataRoot, reports)
}

func redactSandboxReportsForSupportBundle(dataRoot string, reports []sandboxDiagnoseReport) []sandboxDiagnoseReport {
	out := make([]sandboxDiagnoseReport, 0, len(reports))
	for _, report := range reports {
		report.Summary = redactSupportBundleText(dataRoot, report.Summary)
		for i := range report.Checks {
			report.Checks[i].Expected = redactSupportBundleText(dataRoot, report.Checks[i].Expected)
			report.Checks[i].Actual = redactSupportBundleText(dataRoot, report.Checks[i].Actual)
		}
		for i := range report.Warnings {
			report.Warnings[i] = redactSupportBundleText(dataRoot, report.Warnings[i])
		}
		for i := range report.Recommendations {
			report.Recommendations[i] = redactSupportBundleText(dataRoot, report.Recommendations[i])
		}
		report.Raw = nil
		out = append(out, report)
	}
	return out
}

func redactSandboxProfilesForSupportBundle(dataRoot string, profiles []sandboxProfile) []sandboxProfile {
	out := make([]sandboxProfile, 0, len(profiles))
	for _, profile := range profiles {
		for i := range profile.ReadOnlyPaths {
			profile.ReadOnlyPaths[i] = redactSupportBundleValue(dataRoot, profile.ReadOnlyPaths[i])
		}
		for i := range profile.WritablePaths {
			profile.WritablePaths[i] = redactSupportBundleValue(dataRoot, profile.WritablePaths[i])
		}
		out = append(out, profile)
	}
	return out
}
func redactServiceConfigDraftForSupportBundle(dataRoot string, draft adminServiceConfigDraft) adminServiceConfigDraft {
	out := draft
	out.Values = redactServiceConfigValuesForSupportBundle(dataRoot, draft.Values)
	out.Reason = redactSupportBundleText(dataRoot, draft.Reason)
	return out
}

func redactServiceConfigValidationForSupportBundle(dataRoot string, validation adminServiceConfigValidationResult) adminServiceConfigValidationResult {
	out := validation
	out.Normalized = redactServiceConfigValuesForSupportBundle(dataRoot, validation.Normalized)
	if len(validation.EnvPlan) > 0 {
		out.EnvPlan = make([]adminServiceConfigEnvPlanItem, len(validation.EnvPlan))
		copy(out.EnvPlan, validation.EnvPlan)
		fields := adminServiceConfigFieldMap()
		for i := range out.EnvPlan {
			if out.EnvPlan[i].Sensitive {
				out.EnvPlan[i].Value = "[redacted]"
				continue
			}
			if field, ok := fields[out.EnvPlan[i].Key]; ok && adminServiceConfigPathField(field.Key) {
				out.EnvPlan[i].Value = redactSupportBundleValue(dataRoot, stringAny(out.EnvPlan[i].Value))
			}
		}
	}
	return out
}

func redactServiceConfigValuesForSupportBundle(dataRoot string, values map[string]any) map[string]any {
	if len(values) == 0 {
		return map[string]any{}
	}
	fields := adminServiceConfigFieldMap()
	out := make(map[string]any, len(values))
	for key, value := range values {
		if field, ok := fields[key]; ok {
			if field.Sensitive {
				out[key] = "[redacted]"
				continue
			}
			if adminServiceConfigPathField(field.Key) {
				out[key] = redactSupportBundleValue(dataRoot, stringAny(value))
				continue
			}
		}
		out[key] = value
	}
	return out
}

func redactAdminDashboardForSupportBundle(dataRoot string, dashboard agentservice.AdminDashboard) agentservice.AdminDashboard {
	return redactAdminDashboardForAdminAPI(dataRoot, dashboard)
}

func redactAdminDashboardForAdminAPI(dataRoot string, dashboard agentservice.AdminDashboard) agentservice.AdminDashboard {
	dashboard.RecentAuditEvents = redactAuditEventsForAdminAPI(dataRoot, dashboard.RecentAuditEvents)
	return dashboard
}

func redactAuditEventsForAdminAPI(dataRoot string, events []agentservice.AuditEvent) []agentservice.AuditEvent {
	return redactSupportBundleAuditEvents(dataRoot, events, len(events))
}

func redactServiceConfigDiffForSupportBundle(items []adminServiceConfigDiffItem) []adminServiceConfigDiffItem {
	out := make([]adminServiceConfigDiffItem, len(items))
	copy(out, items)
	for i := range out {
		if out[i].Sensitive {
			out[i].Current = "[redacted]"
			out[i].Desired = "[redacted]"
		}
	}
	return out
}
