package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
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
	Strict           bool                      `json:"strict"`
	EffectiveBackend string                    `json:"effective_backend"`
	FallbackReason   string                    `json:"fallback_reason,omitempty"`
	Capabilities     []sandboxCapabilityStatus `json:"capabilities"`
	Backends         []sandboxBackendStatus    `json:"backends"`
}

type sandboxDiagnoseRequest struct {
	Profile                   string `json:"profile,omitempty"`
	IncludeNetworkTests       bool   `json:"include_network_tests,omitempty"`
	IncludeMCPStdioTest       bool   `json:"include_mcp_stdio_test,omitempty"`
	IncludeResourceLimitTests bool   `json:"include_resource_limit_tests,omitempty"`
	WriteReport               *bool  `json:"write_report,omitempty"`
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

func (s *HTTPServer) handleGetAdminServiceConfigEffective(w http.ResponseWriter, r *http.Request) {
	addr := getenv("MACLAW_HTTP_ADDR", "127.0.0.1:18080")
	writeJSON(w, http.StatusOK, map[string]any{
		"generated_at": time.Now().UTC(),
		"fields": map[string]adminConfigField{
			"data_root":                      adminField(s.svc.DataRoot(), envSource("MACLAW_DATA_ROOT"), true, false, false),
			"http_addr":                      adminField(addr, envSource("MACLAW_HTTP_ADDR"), true, false, false),
			"tls_cert_file":                  adminField(os.Getenv("MACLAW_TLS_CERT_FILE"), envSource("MACLAW_TLS_CERT_FILE"), true, false, false),
			"tls_key_file":                   adminField(maskConfigured(os.Getenv("MACLAW_TLS_KEY_FILE")), envSource("MACLAW_TLS_KEY_FILE"), true, true, false),
			"allow_insecure_http":            adminField(os.Getenv("MACLAW_ALLOW_INSECURE_HTTP"), envSource("MACLAW_ALLOW_INSECURE_HTTP"), true, false, false),
			"admin_secret_configured":        adminField(s.adminSecret != "", envSource("MACLAW_ADMIN_SECRET"), true, true, false),
			"local_bash_enabled":             adminField(os.Getenv("MACLAW_ENABLE_LOCAL_BASH"), envSource("MACLAW_ENABLE_LOCAL_BASH"), true, false, false),
			"local_bash_trusted_single_user": adminField(os.Getenv("MACLAW_LOCAL_BASH_TRUSTED_SINGLE_USER"), envSource("MACLAW_LOCAL_BASH_TRUSTED_SINGLE_USER"), true, false, false),
			"local_bash_tenant_id":           adminField(os.Getenv("MACLAW_LOCAL_BASH_TENANT_ID"), envSource("MACLAW_LOCAL_BASH_TENANT_ID"), true, false, false),
			"local_bash_user_id":             adminField(os.Getenv("MACLAW_LOCAL_BASH_USER_ID"), envSource("MACLAW_LOCAL_BASH_USER_ID"), true, false, false),
			"sandbox_mode":                   adminField(adminSandboxMode(), envSource("MACLAW_SANDBOX_MODE"), false, false, true),
			"sandbox_strict":                 adminField(adminEnvBool("MACLAW_SANDBOX_STRICT", false), envSource("MACLAW_SANDBOX_STRICT"), false, false, true),
			"admin_web_default_locale":       adminField(getenv("MACLAW_ADMIN_WEB_DEFAULT_LOCALE", "zh-CN"), envSource("MACLAW_ADMIN_WEB_DEFAULT_LOCALE"), false, false, true),
		},
	})
}

func (s *HTTPServer) handleAdminI18NLocales(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"default_locale":  getenv("MACLAW_ADMIN_WEB_DEFAULT_LOCALE", "zh-CN"),
		"enabled_locales": []string{"zh-CN", "en-US"},
	})
}

func (s *HTTPServer) handleAdminI18NMessages(w http.ResponseWriter, r *http.Request) {
	locale := strings.TrimSpace(r.URL.Query().Get("locale"))
	if locale == "" {
		locale = getenv("MACLAW_ADMIN_WEB_DEFAULT_LOCALE", "zh-CN")
	}
	messages, ok := adminI18NMessages()[locale]
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported locale"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"locale": locale, "messages": messages})
}

func (s *HTTPServer) handleAdminSandboxStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, buildSandboxStatus(false))
}

func (s *HTTPServer) handleAdminSandboxDetect(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, buildSandboxStatus(true))
}

func (s *HTTPServer) handleAdminSandboxSmokeTest(w http.ResponseWriter, r *http.Request) {
	report := buildSandboxDiagnoseReport(r.Context(), sandboxDiagnoseRequest{WriteReport: boolPtr(false)}, s.svc.DataRoot())
	writeJSON(w, statusForDiagnose(report.Status), report)
}

func (s *HTTPServer) handleAdminSandboxDiagnose(w http.ResponseWriter, r *http.Request) {
	var in sandboxDiagnoseRequest
	if !decodeOptionalJSON(w, r, &in) {
		return
	}
	report := buildSandboxDiagnoseReport(r.Context(), in, s.svc.DataRoot())
	if in.WriteReport == nil || *in.WriteReport {
		if err := saveSandboxReport(s.svc.DataRoot(), report); err != nil {
			report.Warnings = append(report.Warnings, "failed to save report: "+err.Error())
			if report.Status == "pass" {
				report.Status = "warn"
			}
		}
	}
	writeJSON(w, statusForDiagnose(report.Status), report)
}

func (s *HTTPServer) handleAdminSandboxReports(w http.ResponseWriter, r *http.Request) {
	reports, err := listSandboxReports(s.svc.DataRoot())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": reports})
}

func (s *HTTPServer) handleAdminSandboxReport(w http.ResponseWriter, r *http.Request) {
	reportID := r.PathValue("reportId")
	report, err := readSandboxReport(s.svc.DataRoot(), reportID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "report not found"})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *HTTPServer) handleAdminSandboxInstallPlan(w http.ResponseWriter, r *http.Request) {
	backend := strings.TrimSpace(r.URL.Query().Get("backend"))
	if backend == "" {
		status := buildSandboxStatus(false)
		backend = status.EffectiveBackend
		if backend == "none" || backend == "" {
			backend = "bwrap"
		}
	}
	writeJSON(w, http.StatusOK, buildSandboxInstallPlan(backend))
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
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_SANDBOX_MODE")))
	switch mode {
	case "", "auto":
		return "auto"
	case "none", "landlock", "bwrap", "nsjail":
		return mode
	default:
		return "auto"
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

func buildSandboxStatus(runSmoke bool) sandboxStatus {
	status := sandboxStatus{
		GeneratedAt:  time.Now().UTC(),
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		Kernel:       kernelVersion(),
		Mode:         adminSandboxMode(),
		Strict:       adminEnvBool("MACLAW_SANDBOX_STRICT", false),
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
	status := buildSandboxStatus(true)
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
	return os.WriteFile(filepath.Join(dir, report.ReportID+".json"), data, 0o600)
}

func listSandboxReports(dataRoot string) ([]sandboxDiagnoseReport, error) {
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
	if len(reports) > 20 {
		reports = reports[:20]
	}
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

func redactShort(s string) string {
	s = strings.TrimSpace(s)
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
