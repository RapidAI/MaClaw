package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

type adminConfigField struct {
	Value            any    `json:"value"`
	Source           string `json:"source"`
	RestartRequired  bool   `json:"restart_required"`
	Sensitive        bool   `json:"sensitive"`
	MutableAtRuntime bool   `json:"mutable_at_runtime"`
}

type sandboxBackendStatus struct {
	Name        string `json:"name"`
	Path        string `json:"path,omitempty"`
	Available   bool   `json:"available"`
	Version     string `json:"version,omitempty"`
	SmokeStatus string `json:"smoke_status,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

type sandboxCapabilityStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	Path   string `json:"path,omitempty"`
	Raw    string `json:"raw,omitempty"`
}

type sandboxStatus struct {
	GeneratedAt      time.Time                 `json:"generated_at"`
	OS               string                    `json:"os"`
	Arch             string                    `json:"arch"`
	Kernel           string                    `json:"kernel,omitempty"`
	Mode             string                    `json:"mode"`
	ModeSource       string                    `json:"mode_source"`
	Strict           bool                      `json:"strict"`
	StrictSource     string                    `json:"strict_source"`
	EffectiveBackend string                    `json:"effective_backend"`
	FallbackReason   string                    `json:"fallback_reason,omitempty"`
	Capabilities     []sandboxCapabilityStatus `json:"capabilities"`
	Backends         []sandboxBackendStatus    `json:"backends"`
}

type adminRuntimeConfig struct {
	SandboxMode   string    `json:"sandbox_mode,omitempty"`
	SandboxStrict *bool     `json:"sandbox_strict,omitempty"`
	UpdatedAt     time.Time `json:"updated_at,omitempty"`
	UpdatedBy     string    `json:"updated_by,omitempty"`
	Reason        string    `json:"reason,omitempty"`
}

type adminSandboxConfigResponse struct {
	Mode      adminConfigField `json:"mode"`
	Strict    adminConfigField `json:"strict"`
	UpdatedAt time.Time        `json:"updated_at,omitempty"`
	UpdatedBy string           `json:"updated_by,omitempty"`
	Reason    string           `json:"reason,omitempty"`
}

type updateAdminSandboxConfigRequest struct {
	Mode          *string `json:"mode,omitempty"`
	Strict        *bool   `json:"strict,omitempty"`
	Reason        string  `json:"reason,omitempty"`
	ConfirmUnsafe bool    `json:"confirm_unsafe,omitempty"`
}

type rollbackAdminSandboxConfigRequest struct {
	Reason string `json:"reason,omitempty"`
}

type sandboxDiagnoseRequest struct {
	Profile                   string `json:"profile,omitempty"`
	IncludeNetworkTests       bool   `json:"include_network_tests,omitempty"`
	IncludeMCPStdioTest       bool   `json:"include_mcp_stdio_test,omitempty"`
	IncludeResourceLimitTests bool   `json:"include_resource_limit_tests,omitempty"`
	WriteReport               *bool  `json:"write_report,omitempty"`
}

type sandboxProfile struct {
	Name          string         `json:"name"`
	Backend       string         `json:"backend,omitempty"`
	Network       string         `json:"network,omitempty"`
	ReadOnlyPaths []string       `json:"readonly_paths,omitempty"`
	WritablePaths []string       `json:"writable_paths,omitempty"`
	EnvAllowlist  []string       `json:"env_allowlist,omitempty"`
	Limits        map[string]any `json:"limits,omitempty"`
	UpdatedAt     time.Time      `json:"updated_at,omitempty"`
	UpdatedBy     string         `json:"updated_by,omitempty"`
}

type sandboxProfilesFile struct {
	Profiles map[string]sandboxProfile `json:"profiles"`
}

type sandboxCheckResult struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	Expected   string `json:"expected,omitempty"`
	Actual     string `json:"actual,omitempty"`
	Severity   string `json:"severity"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	MessageKey string `json:"message_key,omitempty"`
}

type sandboxDiagnoseReport struct {
	ReportID         string                 `json:"report_id"`
	GeneratedAt      time.Time              `json:"generated_at"`
	Status           string                 `json:"status"`
	Summary          string                 `json:"summary"`
	Mode             string                 `json:"mode"`
	EffectiveBackend string                 `json:"effective_backend"`
	Profile          string                 `json:"profile"`
	Strict           bool                   `json:"strict"`
	Checks           []sandboxCheckResult   `json:"checks"`
	Warnings         []string               `json:"warnings,omitempty"`
	Recommendations  []string               `json:"recommendations,omitempty"`
	Raw              map[string]interface{} `json:"raw,omitempty"`
}

type sandboxInstallPlan struct {
	Platform          string   `json:"platform"`
	Backend           string   `json:"backend"`
	Commands          []string `json:"commands"`
	RequiresPrivilege bool     `json:"requires_privilege"`
	WillExecute       bool     `json:"will_execute"`
	Notes             []string `json:"notes,omitempty"`
}

type sandboxInstallRequest struct {
	Backend string `json:"backend,omitempty"`
	Confirm bool   `json:"confirm,omitempty"`
	Mode    string `json:"mode,omitempty"`
}

type sandboxInstallCommandResult struct {
	Command    string `json:"command"`
	Output     string `json:"output,omitempty"`
	Error      string `json:"error,omitempty"`
	ExitCode   int    `json:"exit_code"`
	DurationMS int64  `json:"duration_ms"`
}

type sandboxInstallResponse struct {
	Platform    string                        `json:"platform"`
	Backend     string                        `json:"backend"`
	Mode        string                        `json:"mode"`
	Policy      string                        `json:"policy"`
	Confirmed   bool                          `json:"confirmed"`
	Executed    bool                          `json:"executed"`
	Plan        sandboxInstallPlan            `json:"plan"`
	Results     []sandboxInstallCommandResult `json:"results,omitempty"`
	GeneratedAt time.Time                     `json:"generated_at"`
}

func (s *HTTPServer) handleGetAdminServiceConfigEffective(w http.ResponseWriter, r *http.Request) {
	addr := getenv("MACLAW_HTTP_ADDR", "127.0.0.1:18080")
	dataRoot := s.svc.DataRoot()
	writeJSON(w, http.StatusOK, map[string]any{
		"generated_at": time.Now().UTC(),
		"fields": map[string]adminConfigField{
			"data_root":                      adminField(redactSupportBundleValue(dataRoot, dataRoot), envSource("MACLAW_DATA_ROOT"), true, false, false),
			"http_addr":                      adminField(addr, envSource("MACLAW_HTTP_ADDR"), true, false, false),
			"tls_cert_file":                  adminField(redactSupportBundleValue(dataRoot, os.Getenv("MACLAW_TLS_CERT_FILE")), envSource("MACLAW_TLS_CERT_FILE"), true, false, false),
			"tls_key_file":                   adminField(maskConfigured(os.Getenv("MACLAW_TLS_KEY_FILE")), envSource("MACLAW_TLS_KEY_FILE"), true, true, false),
			"allow_insecure_http":            adminField(os.Getenv("MACLAW_ALLOW_INSECURE_HTTP"), envSource("MACLAW_ALLOW_INSECURE_HTTP"), true, false, false),
			"enable_scheduler":               adminField(os.Getenv("MACLAW_ENABLE_SCHEDULER"), envSource("MACLAW_ENABLE_SCHEDULER"), true, false, false),
			"log_file":                       adminField(redactSupportBundleValue(dataRoot, os.Getenv("MACLAW_LOG_FILE")), envSource("MACLAW_LOG_FILE"), true, false, false),
			"admin_secret_configured":        adminField(s.adminSecret != "", envSource("MACLAW_ADMIN_SECRET"), true, true, false),
			"local_bash_enabled":             adminField(os.Getenv("MACLAW_ENABLE_LOCAL_BASH"), envSource("MACLAW_ENABLE_LOCAL_BASH"), true, false, false),
			"local_bash_trusted_single_user": adminField(os.Getenv("MACLAW_LOCAL_BASH_TRUSTED_SINGLE_USER"), envSource("MACLAW_LOCAL_BASH_TRUSTED_SINGLE_USER"), true, false, false),
			"local_bash_tenant_id":           adminField(os.Getenv("MACLAW_LOCAL_BASH_TENANT_ID"), envSource("MACLAW_LOCAL_BASH_TENANT_ID"), true, false, false),
			"local_bash_user_id":             adminField(os.Getenv("MACLAW_LOCAL_BASH_USER_ID"), envSource("MACLAW_LOCAL_BASH_USER_ID"), true, false, false),
			"sandbox_mode":                   adminField(adminSandboxMode(), envSource("MACLAW_SANDBOX_MODE"), false, false, true),
			"sandbox_strict":                 adminField(adminEnvBool("MACLAW_SANDBOX_STRICT", false), envSource("MACLAW_SANDBOX_STRICT"), false, false, true),
			"sandbox_install_policy":         adminField(sandboxInstallPolicy(), envSource("MACLAW_SANDBOX_INSTALL_POLICY"), true, false, false),
			"sandbox_report_retention":       adminField(sandboxReportRetention(), envSource("MACLAW_SANDBOX_REPORT_RETENTION"), false, false, true),
			"sandbox_startup_diagnose":       adminField(adminEnvBool("MACLAW_SANDBOX_STARTUP_DIAGNOSE", false), envSource("MACLAW_SANDBOX_STARTUP_DIAGNOSE"), false, false, true),
			"admin_web_default_locale":       adminField(getenv("MACLAW_ADMIN_WEB_DEFAULT_LOCALE", "zh-CN"), envSource("MACLAW_ADMIN_WEB_DEFAULT_LOCALE"), false, false, true),
		},
	})
}

func (s *HTTPServer) handleAdminI18NLocales(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"default_locale":  defaultAdminLocale(),
		"enabled_locales": adminEnabledLocales(),
		"locales":         adminLocaleMetadata(),
	})
}

func (s *HTTPServer) handleAdminI18NMessages(w http.ResponseWriter, r *http.Request) {
	locale := strings.TrimSpace(r.URL.Query().Get("locale"))
	if locale == "" {
		locale = defaultAdminLocale()
	}
	if !isAdminLocaleSupported(locale) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "unsupported locale", "enabled_locales": adminEnabledLocales()})
		return
	}
	locale = normalizeAdminLocale(locale)
	messages := adminI18NMessages()[locale]
	writeJSON(w, http.StatusOK, map[string]any{"locale": locale, "messages": messages})
}

func (s *HTTPServer) startSandboxStartupDiagnoseIfEnabled() {
	if !adminEnvBool("MACLAW_SANDBOX_STARTUP_DIAGNOSE", false) {
		return
	}
	mode, _ := effectiveSandboxMode(s.svc.DataRoot())
	if mode == "none" {
		return
	}
	go func() {
		report := buildSandboxDiagnoseReport(context.Background(), sandboxDiagnoseRequest{WriteReport: boolPtr(false)}, s.svc.DataRoot())
		if err := saveSandboxReport(s.svc.DataRoot(), report); err != nil {
			report.Warnings = append(report.Warnings, "failed to save startup report: "+redactSupportBundleText(s.svc.DataRoot(), err.Error()))
		}
		action := "admin.sandbox_startup_diagnose_completed"
		if report.Status == "fail" {
			action = "admin.sandbox_startup_diagnose_failed"
		}
		_ = s.recordAdminAudit(context.Background(), action, "sandbox_report", report.ReportID, map[string]string{
			"status":            report.Status,
			"mode":              report.Mode,
			"effective_backend": report.EffectiveBackend,
			"profile":           report.Profile,
			"report_id":         report.ReportID,
			"trigger":           "startup",
		})
	}()
}

func (s *HTTPServer) handleAdminSandboxStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, redactSandboxStatusForSupportBundle(s.svc.DataRoot(), buildSandboxStatus(s.svc.DataRoot(), false)))
}

func (s *HTTPServer) handleAdminSandboxDetect(w http.ResponseWriter, r *http.Request) {
	status := buildSandboxStatus(s.svc.DataRoot(), true)
	_ = s.recordAdminAudit(r.Context(), "admin.sandbox_detected", "sandbox", status.EffectiveBackend, map[string]string{"mode": status.Mode, "fallback_reason": status.FallbackReason, "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, redactSandboxStatusForSupportBundle(s.svc.DataRoot(), status))
}

func (s *HTTPServer) handleAdminSandboxConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := loadAdminRuntimeConfig(s.svc.DataRoot())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	writeJSON(w, http.StatusOK, redactAdminSandboxConfigForSupportBundle(s.svc.DataRoot(), buildAdminSandboxConfigResponse(cfg)))
}

func (s *HTTPServer) handleUpdateAdminSandboxConfig(w http.ResponseWriter, r *http.Request) {
	s.updateAdminSandboxConfig(w, r, false)
}

func (s *HTTPServer) handleSwitchAdminSandbox(w http.ResponseWriter, r *http.Request) {
	s.updateAdminSandboxConfig(w, r, true)
}

func (s *HTTPServer) updateAdminSandboxConfig(w http.ResponseWriter, r *http.Request, switchMode bool) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	var in updateAdminSandboxConfigRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	cfg, err := loadAdminRuntimeConfig(s.svc.DataRoot())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	oldMode, _ := effectiveSandboxMode(s.svc.DataRoot())
	oldStrict, _ := effectiveSandboxStrict(s.svc.DataRoot())
	if switchMode && in.Mode == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "mode is required"})
		return
	}
	if in.Mode != nil {
		mode, err := normalizeSandboxMode(*in.Mode)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
			return
		}
		if mode == "none" && !in.ConfirmUnsafe {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "confirm_unsafe is required when switching sandbox mode to none"})
			return
		}
		cfg.SandboxMode = mode
	}
	if in.Strict != nil {
		cfg.SandboxStrict = in.Strict
	}
	cfg.UpdatedAt = time.Now().UTC()
	cfg.UpdatedBy = adminActorLabel(s.svc.DataRoot(), r.Header.Get("X-MaClaw-Admin-Secret"))
	cfg.Reason = redactSupportBundleText(s.svc.DataRoot(), trimMax(in.Reason, 500))
	if err := saveAdminRuntimeConfig(s.svc.DataRoot(), cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	newMode, _ := effectiveSandboxMode(s.svc.DataRoot())
	newStrict, _ := effectiveSandboxStrict(s.svc.DataRoot())
	action := "admin.sandbox_config_updated"
	if switchMode {
		action = "admin.sandbox_backend_switched"
	}
	_ = s.recordAdminAudit(r.Context(), action, "sandbox", "runtime", map[string]string{
		"old_mode":   oldMode,
		"new_mode":   newMode,
		"old_strict": fmt.Sprintf("%t", oldStrict),
		"new_strict": fmt.Sprintf("%t", newStrict),
		"reason":     cfg.Reason,
		"remote_ip":  requestClientIP(r),
	})
	resp := map[string]any{"config": redactAdminSandboxConfigForSupportBundle(s.svc.DataRoot(), buildAdminSandboxConfigResponse(cfg)), "status": redactSandboxStatusForSupportBundle(s.svc.DataRoot(), buildSandboxStatus(s.svc.DataRoot(), false))}
	if switchMode {
		report := buildSandboxDiagnoseReport(r.Context(), sandboxDiagnoseRequest{WriteReport: boolPtr(false)}, s.svc.DataRoot())
		resp["diagnose"] = redactSandboxReportsForAdminAPI(s.svc.DataRoot(), []sandboxDiagnoseReport{report})[0]
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *HTTPServer) handleRollbackAdminSandboxConfig(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	var in rollbackAdminSandboxConfigRequest
	if !decodeOptionalJSON(w, r, &in) {
		return
	}
	oldMode, _ := effectiveSandboxMode(s.svc.DataRoot())
	oldStrict, _ := effectiveSandboxStrict(s.svc.DataRoot())
	cfg := adminRuntimeConfig{UpdatedAt: time.Now().UTC(), UpdatedBy: adminActorLabel(s.svc.DataRoot(), r.Header.Get("X-MaClaw-Admin-Secret")), Reason: redactSupportBundleText(s.svc.DataRoot(), trimMax(in.Reason, 500))}
	if err := saveAdminRuntimeConfig(s.svc.DataRoot(), cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	newMode, _ := effectiveSandboxMode(s.svc.DataRoot())
	newStrict, _ := effectiveSandboxStrict(s.svc.DataRoot())
	_ = s.recordAdminAudit(r.Context(), "admin.sandbox_config_rollback", "sandbox", "runtime", map[string]string{
		"old_mode":   oldMode,
		"new_mode":   newMode,
		"old_strict": fmt.Sprintf("%t", oldStrict),
		"new_strict": fmt.Sprintf("%t", newStrict),
		"reason":     cfg.Reason,
		"remote_ip":  requestClientIP(r),
	})
	writeJSON(w, http.StatusOK, map[string]any{"config": redactAdminSandboxConfigForSupportBundle(s.svc.DataRoot(), buildAdminSandboxConfigResponse(cfg)), "status": redactSandboxStatusForSupportBundle(s.svc.DataRoot(), buildSandboxStatus(s.svc.DataRoot(), false))})
}

func (s *HTTPServer) handleAdminSandboxSmokeTest(w http.ResponseWriter, r *http.Request) {
	report := buildSandboxDiagnoseReport(r.Context(), sandboxDiagnoseRequest{WriteReport: boolPtr(false)}, s.svc.DataRoot())
	action := "admin.sandbox_smoke_test_succeeded"
	if report.Status == "fail" {
		action = "admin.sandbox_smoke_test_failed"
	}
	_ = s.recordAdminAudit(r.Context(), action, "sandbox", report.EffectiveBackend, sandboxReportAuditMetadata(r, report))
	writeJSON(w, statusForDiagnose(report.Status), redactSandboxReportsForAdminAPI(s.svc.DataRoot(), []sandboxDiagnoseReport{report})[0])
}

func (s *HTTPServer) handleAdminSandboxDiagnose(w http.ResponseWriter, r *http.Request) {
	var in sandboxDiagnoseRequest
	if !decodeOptionalJSON(w, r, &in) {
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.sandbox_diagnose_started", "sandbox", strings.TrimSpace(in.Profile), map[string]string{"profile": strings.TrimSpace(in.Profile), "remote_ip": requestClientIP(r)})
	report := buildSandboxDiagnoseReport(r.Context(), in, s.svc.DataRoot())
	if in.WriteReport == nil || *in.WriteReport {
		if err := saveSandboxReport(s.svc.DataRoot(), report); err != nil {
			report.Warnings = append(report.Warnings, "failed to save report: "+redactSupportBundleText(s.svc.DataRoot(), err.Error()))
			if report.Status == "pass" {
				report.Status = "warn"
			}
		}
	}
	action := "admin.sandbox_diagnose_completed"
	if report.Status == "fail" {
		action = "admin.sandbox_diagnose_failed"
	}
	_ = s.recordAdminAudit(r.Context(), action, "sandbox_report", report.ReportID, sandboxReportAuditMetadata(r, report))
	writeJSON(w, statusForDiagnose(report.Status), redactSandboxReportsForAdminAPI(s.svc.DataRoot(), []sandboxDiagnoseReport{report})[0])
}

func (s *HTTPServer) handleAdminSandboxReports(w http.ResponseWriter, r *http.Request) {
	reports, err := listSandboxReports(s.svc.DataRoot())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": redactSandboxReportsForAdminAPI(s.svc.DataRoot(), reports)})
}

func (s *HTTPServer) handleAdminSandboxSupportBundle(w http.ResponseWriter, r *http.Request) {
	status := buildSandboxStatus(s.svc.DataRoot(), false)
	cfg, err := loadAdminRuntimeConfig(s.svc.DataRoot())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	reports, err := listSandboxReports(s.svc.DataRoot())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	if len(reports) > 5 {
		reports = reports[:5]
	}
	profiles, err := loadSandboxProfiles(s.svc.DataRoot())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	profileItems := make([]sandboxProfile, 0, len(profiles.Profiles))
	for _, profile := range profiles.Profiles {
		profileItems = append(profileItems, profile)
	}
	sort.Slice(profileItems, func(i, j int) bool { return profileItems[i].Name < profileItems[j].Name })
	events, err := s.svc.ListAuditEvents(r.Context(), agentservice.ListAuditEventsInput{ActorType: "admin"})
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	sortAuditEventsDesc(events)
	sandboxEvents := make([]agentservice.AuditEvent, 0, 20)
	for _, event := range events {
		if isSandboxAuditEvent(event.Action, event.ResourceType) {
			sandboxEvents = append(sandboxEvents, redactSupportBundleAuditEvents(s.svc.DataRoot(), []agentservice.AuditEvent{event}, 1)[0])
			if len(sandboxEvents) >= 20 {
				break
			}
		}
	}
	riskItems := buildAdminRiskEvents(s.svc.DataRoot(), events)
	sort.Slice(riskItems, func(i, j int) bool { return riskItems[i].CreatedAt.After(riskItems[j].CreatedAt) })
	riskRecent := redactSupportBundleRiskEvents(s.svc.DataRoot(), riskItems, 10)
	backend := status.EffectiveBackend
	if backend == "" || backend == "none" {
		backend = status.Mode
	}
	if backend == "" || backend == "none" || backend == "auto" {
		backend = "bwrap"
	}
	generatedAt := time.Now().UTC()
	bundle := map[string]any{
		"generated_at":      generatedAt,
		"status":            redactSandboxStatusForSupportBundle(s.svc.DataRoot(), status),
		"config":            redactAdminSandboxConfigForSupportBundle(s.svc.DataRoot(), buildAdminSandboxConfigResponse(cfg)),
		"reports":           redactSandboxReportsForSupportBundle(s.svc.DataRoot(), reports),
		"events":            sandboxEvents,
		"profiles":          redactSandboxProfilesForSupportBundle(s.svc.DataRoot(), profileItems),
		"install_plan":      buildSandboxInstallPlan(backend),
		"log_sources":       redactSupportBundleLogSources(s.svc.DataRoot(), adminLogSources(s.svc.DataRoot())),
		"recent_log_errors": redactSupportBundleLogLines(s.svc.DataRoot(), recentAdminLogErrors(s.svc.DataRoot(), 20, true)),
		"security_risks": map[string]any{
			"generated_at": generatedAt,
			"filters":      map[string]any{"source": "sandbox_support_bundle", "limit": 10},
			"total":        len(riskItems),
			"counts":       countRiskEventsBySeverity(riskItems),
			"kind_counts":  countRiskEventsByKind(riskItems),
			"recent":       riskRecent,
		},
		"data_root_name":     filepath.Base(s.svc.DataRoot()),
		"data_root_redacted": true,
		"redactions":         []string{"data_root", "audit_metadata", "log_line_secrets", "sandbox_report_raw", "profile_paths"},
		"report_count":       len(reports),
		"event_count":        len(sandboxEvents),
		"profile_count":      len(profileItems),
	}
	download := false
	if raw := strings.TrimSpace(r.URL.Query().Get("download")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid download"})
			return
		}
		download = parsed
	}
	action := "admin.sandbox_support_bundle_generated"
	if download {
		action = "admin.sandbox_support_bundle_downloaded"
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"maclawsrv-sandbox-support-bundle-%s.json\"", generatedAt.Format("20060102T150405Z")))
	}
	_ = s.recordAdminAudit(r.Context(), action, "sandbox", status.EffectiveBackend, map[string]string{"mode": status.Mode, "effective_backend": status.EffectiveBackend, "download": strconv.FormatBool(download), "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, bundle)
}

func (s *HTTPServer) handleAdminSandboxEvents(w http.ResponseWriter, r *http.Request) {
	since, err := parseOptionalTimeQuery(r, "since")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	until, err := parseOptionalTimeQuery(r, "until")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	events, err := s.svc.ListAuditEvents(r.Context(), agentservice.ListAuditEventsInput{ActorType: "admin", Since: since, Until: until})
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	backend := strings.TrimSpace(r.URL.Query().Get("backend"))
	filtered := events[:0]
	for _, event := range events {
		if !isSandboxAuditEvent(event.Action, event.ResourceType) {
			continue
		}
		if status != "" && event.Metadata["status"] != status {
			continue
		}
		if backend != "" && event.Metadata["effective_backend"] != backend && event.ResourceID != backend {
			continue
		}
		filtered = append(filtered, event)
	}
	page, err := parsePageQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	items, meta := paginateAuditEvents(filtered, page)
	writeJSON(w, http.StatusOK, listResponse(redactAuditEventsForAdminAPI(s.svc.DataRoot(), items), meta))
}

func (s *HTTPServer) handleAdminSandboxProfiles(w http.ResponseWriter, r *http.Request) {
	profiles, err := loadSandboxProfiles(s.svc.DataRoot())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	items := make([]sandboxProfile, 0, len(profiles.Profiles))
	for _, profile := range profiles.Profiles {
		items = append(items, profile)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	writeJSON(w, http.StatusOK, map[string]any{"items": redactSandboxProfilesForSupportBundle(s.svc.DataRoot(), items)})
}

func (s *HTTPServer) handleAdminSandboxProfile(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("profileName"))
	profiles, err := loadSandboxProfiles(s.svc.DataRoot())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	profile, ok := profiles.Profiles[name]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "sandbox profile not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"profile": redactSandboxProfilesForSupportBundle(s.svc.DataRoot(), []sandboxProfile{profile})[0], "validation": validateSandboxProfile(profile)})
}

func (s *HTTPServer) handleUpdateAdminSandboxProfile(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	name := strings.TrimSpace(r.PathValue("profileName"))
	if !isSafeID(name) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid profile name"})
		return
	}
	var profile sandboxProfile
	if !decodeJSON(w, r, &profile) {
		return
	}
	profile.Name = name
	profile.UpdatedAt = time.Now().UTC()
	profile.UpdatedBy = adminActorLabel(s.svc.DataRoot(), r.Header.Get("X-MaClaw-Admin-Secret"))
	validation := validateSandboxProfile(profile)
	if !validation.Valid {
		writeJSON(w, http.StatusBadRequest, validation)
		return
	}
	profiles, err := loadSandboxProfiles(s.svc.DataRoot())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	profiles.Profiles[name] = profile
	if err := saveSandboxProfiles(s.svc.DataRoot(), profiles); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.sandbox_profile_updated", "sandbox_profile", name, map[string]string{"backend": profile.Backend, "network": profile.Network, "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, map[string]any{"profile": redactSandboxProfilesForSupportBundle(s.svc.DataRoot(), []sandboxProfile{profile})[0], "validation": validation})
}

func (s *HTTPServer) handleValidateAdminSandboxProfile(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("profileName"))
	if !isSafeID(name) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid profile name"})
		return
	}
	var profile sandboxProfile
	if !decodeJSON(w, r, &profile) {
		return
	}
	profile.Name = name
	writeJSON(w, http.StatusOK, validateSandboxProfile(profile))
}

func (s *HTTPServer) handleDeleteAdminSandboxProfile(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	confirm, err := parseOptionalBoolQuery(r, "confirm")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	if confirm == nil || !*confirm {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "confirm=true is required"})
		return
	}
	name := strings.TrimSpace(r.PathValue("profileName"))
	if !isSafeID(name) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid profile name"})
		return
	}
	profiles, err := loadSandboxProfiles(s.svc.DataRoot())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	if _, ok := profiles.Profiles[name]; !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "sandbox profile not found"})
		return
	}
	delete(profiles.Profiles, name)
	if err := saveSandboxProfiles(s.svc.DataRoot(), profiles); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.sandbox_profile_deleted", "sandbox_profile", name, map[string]string{"remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "profile": name})
}

func (s *HTTPServer) handleAdminSandboxReport(w http.ResponseWriter, r *http.Request) {
	reportID := r.PathValue("reportId")
	report, err := readSandboxReport(s.svc.DataRoot(), reportID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "report not found"})
		return
	}
	writeJSON(w, http.StatusOK, redactSandboxReportsForAdminAPI(s.svc.DataRoot(), []sandboxDiagnoseReport{*report})[0])
}

func (s *HTTPServer) handleDeleteAdminSandboxReport(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	confirm, err := parseOptionalBoolQuery(r, "confirm")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	if confirm == nil || !*confirm {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "confirm=true is required"})
		return
	}
	reportID := r.PathValue("reportId")
	if err := deleteSandboxReport(s.svc.DataRoot(), reportID); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "report not found"})
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.sandbox_report_deleted", "sandbox_report", reportID, map[string]string{"remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "report_id": reportID})
}

func (s *HTTPServer) handleAdminSandboxInstallPlan(w http.ResponseWriter, r *http.Request) {
	backend := strings.TrimSpace(r.URL.Query().Get("backend"))
	if backend == "" {
		status := buildSandboxStatus(s.svc.DataRoot(), false)
		backend = status.EffectiveBackend
		if backend == "none" || backend == "" {
			backend = "bwrap"
		}
	}
	plan := buildSandboxInstallPlan(backend)
	_ = s.recordAdminAudit(r.Context(), "admin.sandbox_install_plan_generated", "sandbox", plan.Backend, map[string]string{"platform": plan.Platform, "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, plan)
}

func (s *HTTPServer) handleAdminSandboxInstall(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	var in sandboxInstallRequest
	if !decodeOptionalJSON(w, r, &in) {
		return
	}
	backend := strings.TrimSpace(in.Backend)
	if backend == "" {
		backend = strings.TrimSpace(r.URL.Query().Get("backend"))
	}
	if backend == "" {
		status := buildSandboxStatus(s.svc.DataRoot(), false)
		backend = status.EffectiveBackend
		if backend == "none" || backend == "" {
			backend = "bwrap"
		}
	}
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode == "" {
		mode = "print_only"
	}
	if mode != "print_only" && mode != "run" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "mode must be print_only or run"})
		return
	}
	policy := sandboxInstallPolicy()
	plan := buildSandboxInstallPlan(backend)
	resp := sandboxInstallResponse{Platform: plan.Platform, Backend: plan.Backend, Mode: mode, Policy: policy, Confirmed: in.Confirm, Plan: plan, GeneratedAt: time.Now().UTC()}
	if mode == "print_only" {
		_ = s.recordAdminAudit(r.Context(), "admin.sandbox_install_plan_generated", "sandbox", plan.Backend, map[string]string{"platform": plan.Platform, "mode": mode, "remote_ip": requestClientIP(r)})
		writeJSON(w, http.StatusOK, resp)
		return
	}
	if !in.Confirm {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "confirm=true is required when mode is run"})
		return
	}
	if policy != "run" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "sandbox install policy does not allow command execution"})
		return
	}
	if runtime.GOOS != "linux" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sandbox install execution is Linux-only"})
		return
	}
	if !sandboxInstallPlanRunnable(plan) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sandbox install plan is not executable on this platform/backend"})
		return
	}
	resp.Plan.WillExecute = true
	_ = s.recordAdminAudit(r.Context(), "admin.sandbox_install_started", "sandbox", plan.Backend, map[string]string{"platform": plan.Platform, "remote_ip": requestClientIP(r)})
	results, err := runSandboxInstallCommands(r.Context(), plan.Commands)
	resp.Results = results
	resp.Executed = true
	if err != nil {
		_ = s.recordAdminAudit(r.Context(), "admin.sandbox_install_failed", "sandbox", plan.Backend, map[string]string{"platform": plan.Platform, "error": redactShort(err.Error()), "remote_ip": requestClientIP(r)})
		writeJSON(w, http.StatusInternalServerError, resp)
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.sandbox_install_completed", "sandbox", plan.Backend, map[string]string{"platform": plan.Platform, "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, resp)
}

func adminField(value any, source string, restartRequired, sensitive, mutable bool) adminConfigField {
	return adminConfigField{Value: value, Source: source, RestartRequired: restartRequired, Sensitive: sensitive, MutableAtRuntime: mutable}
}

func envSource(key string) string {
	if os.Getenv(key) != "" {
		return "env"
	}
	return "default"
}

func maskConfigured(v string) any {
	return strings.TrimSpace(v) != ""
}

func adminSandboxMode() string {
	mode, _ := normalizeSandboxMode(os.Getenv("MACLAW_SANDBOX_MODE"))
	return mode
}

func normalizeSandboxMode(value string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(value))
	switch mode {
	case "", "auto":
		return "auto", nil
	case "none", "landlock", "bwrap", "nsjail":
		return mode, nil
	default:
		return "", fmt.Errorf("unsupported sandbox mode %q", value)
	}
}

func adminEnvBool(key string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func effectiveSandboxMode(dataRoot string) (string, string) {
	cfg, err := loadAdminRuntimeConfig(dataRoot)
	if err == nil && strings.TrimSpace(cfg.SandboxMode) != "" {
		if mode, err := normalizeSandboxMode(cfg.SandboxMode); err == nil {
			return mode, "runtime_config"
		}
	}
	env := strings.TrimSpace(os.Getenv("MACLAW_SANDBOX_MODE"))
	mode, err := normalizeSandboxMode(env)
	if err != nil {
		return "auto", "default"
	}
	if env != "" {
		return mode, "env"
	}
	return mode, "default"
}

func effectiveSandboxStrict(dataRoot string) (bool, string) {
	cfg, err := loadAdminRuntimeConfig(dataRoot)
	if err == nil && cfg.SandboxStrict != nil {
		return *cfg.SandboxStrict, "runtime_config"
	}
	if strings.TrimSpace(os.Getenv("MACLAW_SANDBOX_STRICT")) != "" {
		return adminEnvBool("MACLAW_SANDBOX_STRICT", false), "env"
	}
	return false, "default"
}

func buildAdminSandboxConfigResponse(cfg adminRuntimeConfig) adminSandboxConfigResponse {
	mode, modeSource := effectiveSandboxModeFromConfig(cfg)
	strict, strictSource := effectiveSandboxStrictFromConfig(cfg)
	return adminSandboxConfigResponse{
		Mode:      adminField(mode, modeSource, false, false, true),
		Strict:    adminField(strict, strictSource, false, false, true),
		UpdatedAt: cfg.UpdatedAt,
		UpdatedBy: cfg.UpdatedBy,
		Reason:    cfg.Reason,
	}
}

func effectiveSandboxModeFromConfig(cfg adminRuntimeConfig) (string, string) {
	if strings.TrimSpace(cfg.SandboxMode) != "" {
		if mode, err := normalizeSandboxMode(cfg.SandboxMode); err == nil {
			return mode, "runtime_config"
		}
	}
	env := strings.TrimSpace(os.Getenv("MACLAW_SANDBOX_MODE"))
	mode, err := normalizeSandboxMode(env)
	if err != nil {
		return "auto", "default"
	}
	if env != "" {
		return mode, "env"
	}
	return mode, "default"
}

func effectiveSandboxStrictFromConfig(cfg adminRuntimeConfig) (bool, string) {
	if cfg.SandboxStrict != nil {
		return *cfg.SandboxStrict, "runtime_config"
	}
	if strings.TrimSpace(os.Getenv("MACLAW_SANDBOX_STRICT")) != "" {
		return adminEnvBool("MACLAW_SANDBOX_STRICT", false), "env"
	}
	return false, "default"
}

func loadAdminRuntimeConfig(dataRoot string) (adminRuntimeConfig, error) {
	var cfg adminRuntimeConfig
	if err := readAdminJSON(dataRoot, "admin_runtime_config.json", &cfg); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return adminRuntimeConfig{}, nil
		}
		return adminRuntimeConfig{}, err
	}
	return cfg, nil
}

func saveAdminRuntimeConfig(dataRoot string, cfg adminRuntimeConfig) error {
	return writeAdminJSON(dataRoot, "admin_runtime_config.json", cfg)
}

func adminActorLabel(dataRoot, token string) string {
	if session, user, err := getAdminSessionUser(dataRoot, token, time.Now().UTC()); err == nil && session != nil && user != nil {
		return user.Username
	}
	return "admin_secret"
}

func buildSandboxStatus(dataRoot string, runSmoke bool) sandboxStatus {
	mode, modeSource := effectiveSandboxMode(dataRoot)
	strict, strictSource := effectiveSandboxStrict(dataRoot)
	status := sandboxStatus{
		GeneratedAt:  time.Now().UTC(),
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		Kernel:       kernelVersion(),
		Mode:         mode,
		ModeSource:   modeSource,
		Strict:       strict,
		StrictSource: strictSource,
		Capabilities: sandboxCapabilities(),
		Backends:     sandboxBackends(runSmoke),
	}
	status.EffectiveBackend, status.FallbackReason = chooseSandboxBackend(status.Mode, status.Backends)
	return status
}

func sandboxBackends(runSmoke bool) []sandboxBackendStatus {
	backends := []sandboxBackendStatus{}
	for _, name := range []string{"sandlock", "landrun", "rstrict", "sandboxec", "bwrap", "nsjail"} {
		path, err := exec.LookPath(name)
		item := sandboxBackendStatus{Name: name, Available: err == nil, Path: path}
		if err != nil {
			item.Reason = "not found in PATH"
		} else {
			item.Version = sandboxVersion(name, path)
			if runSmoke {
				item.SmokeStatus, item.Reason = smokeSandboxBackend(name, path)
			}
		}
		backends = append(backends, item)
	}
	return backends
}

func sandboxCapabilities() []sandboxCapabilityStatus {
	if runtime.GOOS != "linux" {
		return []sandboxCapabilityStatus{{Name: "linux", Status: "fail", Detail: "sandbox backends are Linux-only in this design"}}
	}
	caps := []sandboxCapabilityStatus{{Name: "linux", Status: "pass", Detail: "linux platform detected"}}
	caps = append(caps, readProcCapability("user_namespace", "/proc/sys/kernel/unprivileged_userns_clone"))
	caps = append(caps, sandboxCapabilityStatus{Name: "landlock", Status: landlockCapabilityStatus(), Detail: "best-effort kernel capability check"})
	caps = append(caps, sandboxCapabilityStatus{Name: "seccomp", Status: "unknown", Detail: "backend-specific seccomp profile validation is deferred to diagnose"})
	return caps
}

func readProcCapability(name, path string) sandboxCapabilityStatus {
	data, err := os.ReadFile(path)
	if err != nil {
		return sandboxCapabilityStatus{Name: name, Status: "unknown", Path: path, Detail: err.Error()}
	}
	raw := strings.TrimSpace(string(data))
	status := "warn"
	if raw == "1" {
		status = "pass"
	}
	return sandboxCapabilityStatus{Name: name, Status: status, Path: path, Raw: raw}
}

func landlockCapabilityStatus() string {
	if runtime.GOOS != "linux" {
		return "fail"
	}
	major, minor := linuxKernelMajorMinor(kernelVersion())
	if major > 5 || (major == 5 && minor >= 13) {
		return "pass"
	}
	if major == 0 && minor == 0 {
		return "unknown"
	}
	return "warn"
}

func chooseSandboxBackend(mode string, backends []sandboxBackendStatus) (string, string) {
	if runtime.GOOS != "linux" {
		return "none", "sandbox is currently Linux-only"
	}
	available := map[string]bool{}
	for _, backend := range backends {
		available[backend.Name] = backend.Available
	}
	landlockAvailable := available["sandlock"] || available["landrun"] || available["rstrict"] || available["sandboxec"]
	switch mode {
	case "none":
		return "none", "sandbox mode is none"
	case "landlock":
		if landlockAvailable {
			return "landlock", ""
		}
		return "none", "requested landlock backend is unavailable"
	case "bwrap", "nsjail":
		if available[mode] {
			return mode, ""
		}
		return "none", "requested " + mode + " backend is unavailable"
	}
	if landlockAvailable {
		return "landlock", ""
	}
	if available["bwrap"] {
		return "bwrap", "landlock wrapper unavailable"
	}
	if available["nsjail"] {
		return "nsjail", "landlock and bwrap unavailable"
	}
	return "none", "no supported sandbox backend found"
}

func sandboxVersion(name, path string) string {
	args := []string{"--version"}
	if name == "bwrap" {
		args = []string{"--version"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	out, err := cmd.CombinedOutput()
	if err != nil || len(bytes.TrimSpace(out)) == 0 {
		return "unknown"
	}
	line := strings.Split(strings.TrimSpace(string(out)), "\n")[0]
	if len(line) > 120 {
		line = line[:120]
	}
	return line
}

func smokeSandboxBackend(name, path string) (string, string) {
	if runtime.GOOS != "linux" {
		return "skipped", "non-linux platform"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	switch name {
	case "bwrap":
		truePath := firstExistingPath("/usr/bin/true", "/bin/true")
		if truePath == "" {
			return "skipped", "true binary not found"
		}
		args := []string{"--ro-bind", "/usr", "/usr", "--ro-bind", "/bin", "/bin", "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp", "--", truePath}
		cmd = exec.CommandContext(ctx, path, args...)
	case "nsjail":
		truePath := firstExistingPath("/usr/bin/true", "/bin/true")
		if truePath == "" {
			return "skipped", "true binary not found"
		}
		cmd = exec.CommandContext(ctx, path, "--quiet", "--mode", "o", "--", truePath)
	case "sandlock":
		cmd = exec.CommandContext(ctx, path, "run", "--", "true")
	default:
		return "skipped", "smoke command is not defined for " + name
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return "failed", redactShort(msg)
	}
	return "passed", ""
}

func buildSandboxDiagnoseReport(ctx context.Context, in sandboxDiagnoseRequest, dataRoot string) sandboxDiagnoseReport {
	status := buildSandboxStatus(dataRoot, true)
	profile := strings.TrimSpace(in.Profile)
	if profile == "" {
		profile = "default"
	}
	report := sandboxDiagnoseReport{
		ReportID:         newSandboxReportID(),
		GeneratedAt:      time.Now().UTC(),
		Status:           "pass",
		Summary:          "Sandbox core checks passed.",
		Mode:             status.Mode,
		EffectiveBackend: status.EffectiveBackend,
		Profile:          profile,
		Strict:           status.Strict,
		Raw:              map[string]interface{}{"os": status.OS, "arch": status.Arch, "kernel": status.Kernel},
	}
	checks := []sandboxCheckResult{}
	profileCheck, profileWarnings := checkSandboxProfile(dataRoot, profile)
	checks = append(checks, profileCheck)
	for _, warning := range profileWarnings {
		report.Warnings = append(report.Warnings, warning)
	}
	checks = append(checks, checkFromBool("linux_platform", "Linux platform", runtime.GOOS == "linux", "linux", runtime.GOOS, "critical"))
	checks = append(checks, checkFromBool("backend_selected", "Sandbox backend selected", status.EffectiveBackend != "none", "non-none backend", status.EffectiveBackend, "critical"))
	checks = append(checks, checkWorkspaceWritable(dataRoot))
	checks = append(checks, checkForbiddenPathBlocked())
	checks = append(checks, checkTmpWritable())
	if in.IncludeMCPStdioTest {
		checks = append(checks, checkMCPStdioProbe(ctx))
	}
	if in.IncludeNetworkTests {
		checks = append(checks, sandboxCheckResult{ID: "network_policy", Title: "Network policy", Status: "skipped", Expected: "backend-specific network ACL", Actual: "not implemented in first API slice", Severity: "medium"})
	}
	if in.IncludeResourceLimitTests {
		checks = append(checks, sandboxCheckResult{ID: "resource_limits", Title: "Resource limits", Status: "skipped", Expected: "timeout/output/process limits", Actual: "not implemented in first API slice", Severity: "medium"})
	}
	report.Checks = checks
	for _, check := range checks {
		switch check.Status {
		case "fail":
			report.Status = "fail"
		case "warn", "skipped":
			if report.Status == "pass" {
				report.Status = "warn"
			}
		}
	}
	if status.FallbackReason != "" {
		report.Warnings = append(report.Warnings, status.FallbackReason)
		if report.Status == "pass" {
			report.Status = "warn"
		}
	}
	if len(report.Warnings) > 0 && report.Status == "pass" {
		report.Status = "warn"
	}
	if report.Status == "fail" {
		report.Summary = "Sandbox diagnose failed. Review failed checks before enabling protected local execution."
		report.Recommendations = append(report.Recommendations, "Use install-plan or switch to another sandbox backend, then rerun diagnose.")
	} else if report.Status == "warn" {
		report.Summary = "Sandbox is partially available, but warnings or skipped checks need administrator review."
	} else {
		report.Recommendations = append(report.Recommendations, "Run MCP stdio test before enabling sandbox for all local MCP servers.")
	}
	return report
}

func isSandboxAuditEvent(action, resourceType string) bool {
	return strings.HasPrefix(action, "admin.sandbox_") || resourceType == "sandbox" || resourceType == "sandbox_report"
}

type sandboxProfileValidation struct {
	Valid    bool     `json:"valid"`
	Errors   []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

func validateSandboxProfile(profile sandboxProfile) sandboxProfileValidation {
	out := sandboxProfileValidation{Valid: true}
	if !isSafeID(profile.Name) {
		out.Valid = false
		out.Errors = append(out.Errors, "profile name must be a safe id")
	}
	backend := strings.ToLower(strings.TrimSpace(profile.Backend))
	if backend != "" {
		if _, err := normalizeSandboxMode(backend); err != nil || backend == "auto" || backend == "none" {
			out.Valid = false
			out.Errors = append(out.Errors, "backend must be landlock, bwrap, or nsjail")
		}
	}
	network := strings.ToLower(strings.TrimSpace(profile.Network))
	switch network {
	case "", "default", "disabled", "host":
	default:
		out.Valid = false
		out.Errors = append(out.Errors, "network must be default, disabled, or host")
	}
	for _, path := range append(append([]string{}, profile.ReadOnlyPaths...), profile.WritablePaths...) {
		if strings.TrimSpace(path) == "" {
			out.Valid = false
			out.Errors = append(out.Errors, "paths must not be empty")
		}
	}
	if network == "host" {
		out.Warnings = append(out.Warnings, "host network weakens sandbox isolation")
	}
	return out
}

func loadSandboxProfiles(dataRoot string) (sandboxProfilesFile, error) {
	var profiles sandboxProfilesFile
	if err := readAdminJSON(dataRoot, "sandbox_profiles.json", &profiles); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return sandboxProfilesFile{Profiles: map[string]sandboxProfile{}}, nil
		}
		return sandboxProfilesFile{}, err
	}
	if profiles.Profiles == nil {
		profiles.Profiles = map[string]sandboxProfile{}
	}
	return profiles, nil
}

func saveSandboxProfiles(dataRoot string, profiles sandboxProfilesFile) error {
	if profiles.Profiles == nil {
		profiles.Profiles = map[string]sandboxProfile{}
	}
	return writeAdminJSON(dataRoot, "sandbox_profiles.json", profiles)
}

func sandboxReportAuditMetadata(r *http.Request, report sandboxDiagnoseReport) map[string]string {
	return map[string]string{
		"status":            report.Status,
		"mode":              report.Mode,
		"effective_backend": report.EffectiveBackend,
		"profile":           report.Profile,
		"report_id":         report.ReportID,
		"remote_ip":         requestClientIP(r),
	}
}

func checkSandboxProfile(dataRoot, profileName string) (sandboxCheckResult, []string) {
	profileName = strings.TrimSpace(profileName)
	if profileName == "" || profileName == "default" {
		return sandboxCheckResult{ID: "profile_load", Title: "Sandbox profile load", Status: "pass", Expected: "default profile", Actual: "default", Severity: "high"}, nil
	}
	profiles, err := loadSandboxProfiles(dataRoot)
	if err != nil {
		return sandboxCheckResult{ID: "profile_load", Title: "Sandbox profile load", Status: "fail", Expected: "load profile", Actual: err.Error(), Severity: "high"}, nil
	}
	profile, ok := profiles.Profiles[profileName]
	if !ok {
		return sandboxCheckResult{ID: "profile_load", Title: "Sandbox profile load", Status: "fail", Expected: "existing profile", Actual: "profile not found: " + profileName, Severity: "high"}, nil
	}
	validation := validateSandboxProfile(profile)
	if !validation.Valid {
		return sandboxCheckResult{ID: "profile_load", Title: "Sandbox profile load", Status: "fail", Expected: "valid profile", Actual: strings.Join(validation.Errors, "; "), Severity: "high"}, validation.Warnings
	}
	return sandboxCheckResult{ID: "profile_load", Title: "Sandbox profile load", Status: "pass", Expected: "valid profile", Actual: profileName, Severity: "high"}, validation.Warnings
}

func checkFromBool(id, title string, ok bool, expected, actual, severity string) sandboxCheckResult {
	status := "pass"
	if !ok {
		status = "fail"
	}
	return sandboxCheckResult{ID: id, Title: title, Status: status, Expected: expected, Actual: actual, Severity: severity}
}

func checkWorkspaceWritable(dataRoot string) sandboxCheckResult {
	start := time.Now()
	target := filepath.Join(dataRoot, "state", "sandbox_probe")
	if err := os.MkdirAll(target, 0o700); err != nil {
		return sandboxCheckResult{ID: "workspace_write", Title: "Workspace write", Status: "fail", Expected: "write allowed", Actual: err.Error(), Severity: "critical", DurationMS: time.Since(start).Milliseconds()}
	}
	f, err := os.CreateTemp(target, "probe-*")
	if err != nil {
		return sandboxCheckResult{ID: "workspace_write", Title: "Workspace write", Status: "fail", Expected: "write allowed", Actual: err.Error(), Severity: "critical", DurationMS: time.Since(start).Milliseconds()}
	}
	name := f.Name()
	_, _ = f.WriteString("ok")
	_ = f.Close()
	_ = os.Remove(name)
	return sandboxCheckResult{ID: "workspace_write", Title: "Workspace write", Status: "pass", Expected: "write allowed", Actual: "write succeeded", Severity: "critical", DurationMS: time.Since(start).Milliseconds()}
}

func checkForbiddenPathBlocked() sandboxCheckResult {
	start := time.Now()
	if runtime.GOOS != "linux" {
		return sandboxCheckResult{ID: "forbidden_path_read", Title: "Forbidden path is blocked", Status: "skipped", Expected: "read denied", Actual: "non-linux platform", Severity: "critical", DurationMS: time.Since(start).Milliseconds()}
	}
	_, err := os.ReadFile("/etc/shadow")
	if err != nil {
		return sandboxCheckResult{ID: "forbidden_path_read", Title: "Forbidden path is blocked", Status: "pass", Expected: "read denied", Actual: "read denied", Severity: "critical", DurationMS: time.Since(start).Milliseconds()}
	}
	return sandboxCheckResult{ID: "forbidden_path_read", Title: "Forbidden path is blocked", Status: "fail", Expected: "read denied", Actual: "read succeeded", Severity: "critical", DurationMS: time.Since(start).Milliseconds()}
}

func checkTmpWritable() sandboxCheckResult {
	start := time.Now()
	f, err := os.CreateTemp("", "maclawsrv-sandbox-tmp-*")
	if err != nil {
		return sandboxCheckResult{ID: "tmp_write", Title: "Temporary directory write", Status: "fail", Expected: "tmp write allowed", Actual: err.Error(), Severity: "medium", DurationMS: time.Since(start).Milliseconds()}
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return sandboxCheckResult{ID: "tmp_write", Title: "Temporary directory write", Status: "pass", Expected: "tmp write allowed", Actual: "write succeeded", Severity: "medium", DurationMS: time.Since(start).Milliseconds()}
}

func checkMCPStdioProbe(ctx context.Context) sandboxCheckResult {
	start := time.Now()
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(probeCtx, "cmd", "/c", "echo {}")
	} else {
		cmd = exec.CommandContext(probeCtx, "sh", "-c", "printf '{}' ")
	}
	out, err := cmd.Output()
	if err != nil {
		return sandboxCheckResult{ID: "mcp_stdio", Title: "MCP stdio probe", Status: "fail", Expected: "stdio roundtrip", Actual: err.Error(), Severity: "high", DurationMS: time.Since(start).Milliseconds()}
	}
	if strings.TrimSpace(string(out)) != "{}" {
		return sandboxCheckResult{ID: "mcp_stdio", Title: "MCP stdio probe", Status: "fail", Expected: "{}", Actual: redactShort(string(out)), Severity: "high", DurationMS: time.Since(start).Milliseconds()}
	}
	return sandboxCheckResult{ID: "mcp_stdio", Title: "MCP stdio probe", Status: "pass", Expected: "stdio roundtrip", Actual: "ok", Severity: "high", DurationMS: time.Since(start).Milliseconds()}
}

func saveSandboxReport(dataRoot string, report sandboxDiagnoseReport) error {
	dir := sandboxReportDir(dataRoot)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, report.ReportID+".json"), data, 0o600); err != nil {
		return err
	}
	return pruneSandboxReports(dataRoot)
}

func latestSandboxReport(dataRoot string) (*sandboxDiagnoseReport, error) {
	reports, err := listSandboxReports(dataRoot)
	if err != nil || len(reports) == 0 {
		return nil, err
	}
	return &reports[0], nil
}

func listSandboxReports(dataRoot string) ([]sandboxDiagnoseReport, error) {
	reports, err := readSandboxReports(dataRoot)
	if err != nil {
		return nil, err
	}
	retention := sandboxReportRetention()
	if len(reports) > retention {
		reports = reports[:retention]
	}
	return reports, nil
}

func readSandboxReports(dataRoot string) ([]sandboxDiagnoseReport, error) {
	dir := sandboxReportDir(dataRoot)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []sandboxDiagnoseReport{}, nil
		}
		return nil, err
	}
	reports := []sandboxDiagnoseReport{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		report, err := readSandboxReport(dataRoot, strings.TrimSuffix(entry.Name(), ".json"))
		if err == nil {
			reports = append(reports, *report)
		}
	}
	sort.Slice(reports, func(i, j int) bool { return reports[i].GeneratedAt.After(reports[j].GeneratedAt) })
	return reports, nil
}
func readSandboxReport(dataRoot, reportID string) (*sandboxDiagnoseReport, error) {
	if !isSafeID(reportID) {
		return nil, fs.ErrNotExist
	}
	data, err := os.ReadFile(filepath.Join(sandboxReportDir(dataRoot), reportID+".json"))
	if err != nil {
		return nil, err
	}
	var report sandboxDiagnoseReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, err
	}
	return &report, nil
}

func deleteSandboxReport(dataRoot, reportID string) error {
	if !isSafeID(reportID) {
		return fs.ErrNotExist
	}
	return os.Remove(filepath.Join(sandboxReportDir(dataRoot), reportID+".json"))
}

func pruneSandboxReports(dataRoot string) error {
	reports, err := readSandboxReports(dataRoot)
	if err != nil {
		return err
	}
	retention := sandboxReportRetention()
	if len(reports) <= retention {
		return nil
	}
	for _, report := range reports[retention:] {
		_ = deleteSandboxReport(dataRoot, report.ReportID)
	}
	return nil
}

func sandboxReportDir(dataRoot string) string {
	return filepath.Join(dataRoot, "state", "sandbox_reports")
}

func newSandboxReportID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("sandbox_report_%d", time.Now().UnixNano())
	}
	return "sandbox_report_" + hex.EncodeToString(b[:])
}

func statusForDiagnose(status string) int {
	if status == "fail" {
		return http.StatusServiceUnavailable
	}
	return http.StatusOK
}

func sandboxReportRetention() int {
	return adminEnvInt("MACLAW_SANDBOX_REPORT_RETENTION", 20, 1, 200)
}

func adminEnvInt(key string, fallback, min, max int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	var out int
	if _, err := fmt.Sscanf(value, "%d", &out); err != nil {
		return fallback
	}
	if out < min {
		return min
	}
	if out > max {
		return max
	}
	return out
}

func sandboxInstallPolicy() string {
	policy := strings.ToLower(strings.TrimSpace(getenv("MACLAW_SANDBOX_INSTALL_POLICY", "suggest")))
	if policy == "" {
		return "suggest"
	}
	return policy
}

func sandboxInstallPlanRunnable(plan sandboxInstallPlan) bool {
	if runtime.GOOS != "linux" || len(plan.Commands) == 0 {
		return false
	}
	if plan.Backend != "bwrap" && plan.Backend != "nsjail" {
		return false
	}
	for _, command := range plan.Commands {
		if strings.HasPrefix(command, "install ") || strings.Contains(command, "unsupported backend") {
			return false
		}
	}
	return true
}

func runSandboxInstallCommands(ctx context.Context, commands []string) ([]sandboxInstallCommandResult, error) {
	results := make([]sandboxInstallCommandResult, 0, len(commands))
	for _, command := range commands {
		start := time.Now()
		cmdText := sandboxInstallCommandForRun(command)
		cmdCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		cmd := exec.CommandContext(cmdCtx, "sh", "-c", cmdText)
		cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
		out, err := cmd.CombinedOutput()
		cancel()
		result := sandboxInstallCommandResult{Command: command, Output: trimMax(redactLogLine(string(out)), 8192), ExitCode: 0, DurationMS: time.Since(start).Milliseconds()}
		if err != nil {
			result.Error = redactShort(err.Error())
			result.ExitCode = 1
			if exitErr, ok := err.(*exec.ExitError); ok {
				result.ExitCode = exitErr.ExitCode()
			}
			results = append(results, result)
			return results, err
		}
		results = append(results, result)
	}
	return results, nil
}

func sandboxInstallCommandForRun(command string) string {
	command = strings.TrimSpace(command)
	if strings.HasPrefix(command, "sudo ") {
		return "sudo -n " + strings.TrimSpace(strings.TrimPrefix(command, "sudo "))
	}
	return command
}

func buildSandboxInstallPlan(backend string) sandboxInstallPlan {
	platform := linuxDistroID()
	plan := sandboxInstallPlan{Platform: platform, Backend: backend, RequiresPrivilege: true, WillExecute: false}
	switch strings.ToLower(backend) {
	case "landlock", "sandlock":
		plan.Commands = []string{"install sandlock or another Landlock wrapper from the approved release source"}
		plan.Notes = []string{"Landlock wrappers may not be available from the OS package manager."}
	case "nsjail":
		plan.Commands = packageInstallCommands(platform, "nsjail")
	case "bwrap", "bubblewrap", "":
		plan.Backend = "bwrap"
		plan.Commands = packageInstallCommands(platform, "bubblewrap")
	default:
		plan.Commands = []string{"unsupported backend: " + backend}
		plan.RequiresPrivilege = false
	}
	return plan
}

func packageInstallCommands(platform, pkg string) []string {
	switch platform {
	case "debian", "ubuntu":
		return []string{"sudo apt-get update", "sudo apt-get install -y " + pkg}
	case "fedora":
		return []string{"sudo dnf install -y " + pkg}
	case "rhel", "centos", "rocky", "almalinux":
		return []string{"sudo dnf install -y " + pkg}
	case "arch":
		return []string{"sudo pacman -S --needed " + pkg}
	default:
		return []string{"install " + pkg + " with the host package manager"}
	}
}

func linuxDistroID() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return runtime.GOOS
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "ID=") {
			return strings.Trim(strings.TrimPrefix(line, "ID="), "\"")
		}
	}
	return runtime.GOOS
}

func kernelVersion() string {
	if runtime.GOOS != "linux" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "uname", "-r").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func linuxKernelMajorMinor(v string) (int, int) {
	var major, minor int
	_, _ = fmt.Sscanf(v, "%d.%d", &major, &minor)
	return major, minor
}

func firstExistingPath(paths ...string) string {
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func isSafeID(id string) bool {
	if id == "" || strings.Contains(id, "/") || strings.Contains(id, "\\") || strings.Contains(id, "..") {
		return false
	}
	return true
}

func defaultAdminLocale() string {
	locale := getenv("MACLAW_ADMIN_WEB_DEFAULT_LOCALE", "zh-CN")
	if !isAdminLocaleSupported(locale) {
		return "zh-CN"
	}
	return normalizeAdminLocale(locale)
}

func adminEnabledLocales() []string {
	return []string{"zh-CN", "en-US"}
}

func adminLocaleMetadata() []map[string]string {
	return []map[string]string{
		{"locale": "zh-CN", "label": "\u7b80\u4f53\u4e2d\u6587"},
		{"locale": "en-US", "label": "English"},
	}
}

func isAdminLocaleSupported(locale string) bool {
	key := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(locale), "_", "-"))
	switch key {
	case "zh", "zh-cn", "zh-hans", "zh-hans-cn", "en", "en-us":
		return true
	default:
		return false
	}
}

func redactShort(s string) string {
	s = strings.TrimSpace(s)
	s = redactSupportBundleText("", s)
	if len(s) > 512 {
		s = s[:512] + "..."
	}
	return s
}

func boolPtr(v bool) *bool { return &v }

func adminI18NMessages() map[string]map[string]string {
	return map[string]map[string]string{
		"zh-CN": {
			"sandbox.switch.title":             "\u5207\u6362\u6c99\u7bb1\u6a21\u5f0f",
			"sandbox.switch.none_warning":      "\u65b0\u7684\u672c\u5730\u6267\u884c\u5c06\u4e0d\u518d\u53d7\u6c99\u7bb1\u4fdd\u62a4\u3002",
			"sandbox.diagnose.title":           "\u6c99\u7bb1\u5065\u5eb7\u68c0\u6d4b\u62a5\u544a",
			"sandbox.diagnose.pass":            "\u6c99\u7bb1\u6838\u5fc3\u68c0\u6d4b\u901a\u8fc7\u3002",
			"sandbox.diagnose.warn":            "\u6c99\u7bb1\u53ef\u7528\uff0c\u4f46\u5b58\u5728\u9700\u8981\u7ba1\u7406\u5458\u786e\u8ba4\u7684\u8b66\u544a\u3002",
			"sandbox.diagnose.fail":            "\u6c99\u7bb1\u68c0\u6d4b\u5931\u8d25\uff0c\u8bf7\u4fee\u590d\u540e\u518d\u542f\u7528\u53d7\u4fdd\u62a4\u6267\u884c\u3002",
			"tenants.delete.confirmation":      "\u8bf7\u8f93\u5165 DELETE {id} \u786e\u8ba4\u6c38\u4e45\u5220\u9664\u3002",
			"errors.sandbox_smoke_test_failed": "\u6c99\u7bb1 smoke test \u5931\u8d25\u3002",
		},
		"en-US": {
			"sandbox.switch.title":             "Switch sandbox mode",
			"sandbox.switch.none_warning":      "Sandbox protection will be disabled for new local executions.",
			"sandbox.diagnose.title":           "Sandbox diagnose report",
			"sandbox.diagnose.pass":            "Sandbox core checks passed.",
			"sandbox.diagnose.warn":            "Sandbox is available, but warnings need administrator review.",
			"sandbox.diagnose.fail":            "Sandbox diagnose failed. Fix issues before enabling protected execution.",
			"tenants.delete.confirmation":      "Type DELETE {id} to confirm permanent deletion.",
			"errors.sandbox_smoke_test_failed": "Sandbox smoke test failed.",
		},
	}
}
