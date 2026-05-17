package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

type adminServiceConfigSchemaField struct {
	Key              string   `json:"key"`
	EnvKey           string   `json:"env_key,omitempty"`
	Type             string   `json:"type"`
	Default          any      `json:"default,omitempty"`
	Description      string   `json:"description,omitempty"`
	RestartRequired  bool     `json:"restart_required"`
	MutableAtRuntime bool     `json:"mutable_at_runtime"`
	Sensitive        bool     `json:"sensitive"`
	Writable         bool     `json:"writable"`
	AllowedValues    []string `json:"allowed_values,omitempty"`
}

type adminServiceConfigDraft struct {
	Values    map[string]any `json:"values"`
	UpdatedAt time.Time      `json:"updated_at,omitempty"`
	UpdatedBy string         `json:"updated_by,omitempty"`
	Reason    string         `json:"reason,omitempty"`
}

type adminServiceConfigDraftRequest struct {
	Values map[string]any `json:"values"`
	Reason string         `json:"reason,omitempty"`
}

type adminServiceConfigValidationResult struct {
	Valid           bool                                     `json:"valid"`
	Errors          []string                                 `json:"errors,omitempty"`
	Warnings        []string                                 `json:"warnings,omitempty"`
	Normalized      map[string]any                           `json:"normalized,omitempty"`
	RestartRequired bool                                     `json:"restart_required"`
	EnvPlan         []adminServiceConfigEnvPlanItem          `json:"env_plan,omitempty"`
	Fields          map[string]adminServiceConfigSchemaField `json:"fields,omitempty"`
}

type adminServiceConfigEnvironmentItem struct {
	Key              string `json:"key"`
	EnvKey           string `json:"env_key"`
	Configured       bool   `json:"configured"`
	Value            any    `json:"value,omitempty"`
	Default          any    `json:"default,omitempty"`
	Sensitive        bool   `json:"sensitive"`
	Writable         bool   `json:"writable"`
	RestartRequired  bool   `json:"restart_required"`
	MutableAtRuntime bool   `json:"mutable_at_runtime"`
	Source           string `json:"source"`
}
type adminServiceConfigEnvPlanItem struct {
	Key       string `json:"key"`
	EnvKey    string `json:"env_key"`
	Value     any    `json:"value,omitempty"`
	Sensitive bool   `json:"sensitive"`
	Action    string `json:"action"`
}

func (s *HTTPServer) handleAdminServiceConfigEnvironment(w http.ResponseWriter, r *http.Request) {
	items := buildAdminServiceConfigEnvironment()
	configured := 0
	for _, item := range items {
		if item.Configured {
			configured++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "configured": configured, "total": len(items), "generated_at": time.Now().UTC()})
}
func (s *HTTPServer) handleAdminServiceConfigSchema(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": adminServiceConfigSchema()})
}

func (s *HTTPServer) handleAdminServiceConfigDraft(w http.ResponseWriter, r *http.Request) {
	draft, err := loadAdminServiceConfigDraft(s.svc.DataRoot())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	validation := validateAdminServiceConfigValues(draft.Values)
	writeJSON(w, http.StatusOK, map[string]any{"draft": draft, "validation": validation})
}

func (s *HTTPServer) handleUpdateAdminServiceConfigDraft(w http.ResponseWriter, r *http.Request) {
	var in adminServiceConfigDraftRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	validation := validateAdminServiceConfigValues(in.Values)
	if !validation.Valid {
		writeJSON(w, http.StatusBadRequest, validation)
		return
	}
	draft := adminServiceConfigDraft{Values: validation.Normalized, UpdatedAt: time.Now().UTC(), UpdatedBy: adminActorLabel(s.svc.DataRoot(), r.Header.Get("X-MaClaw-Admin-Secret")), Reason: trimMax(in.Reason, 500)}
	if err := saveAdminServiceConfigDraft(s.svc.DataRoot(), draft); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.service_config_draft_updated", "service_config", "draft", map[string]string{"keys": strings.Join(sortedAnyKeys(draft.Values), ","), "reason": draft.Reason, "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, map[string]any{"draft": draft, "validation": validation})
}

func (s *HTTPServer) handleValidateAdminServiceConfig(w http.ResponseWriter, r *http.Request) {
	var in adminServiceConfigDraftRequest
	if !decodeOptionalJSON(w, r, &in) {
		return
	}
	values := in.Values
	if values == nil {
		draft, err := loadAdminServiceConfigDraft(s.svc.DataRoot())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		values = draft.Values
	}
	writeJSON(w, http.StatusOK, validateAdminServiceConfigValues(values))
}

func adminServiceConfigSchema() []adminServiceConfigSchemaField {
	fields := []adminServiceConfigSchemaField{
		{Key: "http_addr", EnvKey: "MACLAW_HTTP_ADDR", Type: "string", Default: "127.0.0.1:18080", Description: "HTTP listen address.", RestartRequired: true, Writable: true},
		{Key: "tls_cert_file", EnvKey: "MACLAW_TLS_CERT_FILE", Type: "string", Description: "TLS certificate file path.", RestartRequired: true, Writable: true},
		{Key: "tls_key_file", EnvKey: "MACLAW_TLS_KEY_FILE", Type: "string", Description: "TLS private key file path.", RestartRequired: true, Sensitive: true, Writable: true},
		{Key: "allow_insecure_http", EnvKey: "MACLAW_ALLOW_INSECURE_HTTP", Type: "bool", Default: false, Description: "Allow non-loopback plaintext HTTP.", RestartRequired: true, Writable: true},
		{Key: "enable_scheduler", EnvKey: "MACLAW_ENABLE_SCHEDULER", Type: "bool", Default: false, Description: "Enable service scheduler.", RestartRequired: true, Writable: true},
		{Key: "log_file", EnvKey: "MACLAW_LOG_FILE", Type: "string", Description: "Service log file path.", RestartRequired: true, Writable: true},
		{Key: "admin_web_default_locale", EnvKey: "MACLAW_ADMIN_WEB_DEFAULT_LOCALE", Type: "enum", Default: "zh-CN", Description: "Default Admin Web locale.", MutableAtRuntime: true, Writable: true, AllowedValues: []string{"zh-CN", "en-US"}},
		{Key: "sandbox_mode", EnvKey: "MACLAW_SANDBOX_MODE", Type: "enum", Default: "auto", Description: "Default sandbox backend preference.", MutableAtRuntime: true, Writable: true, AllowedValues: []string{"auto", "landlock", "bwrap", "nsjail", "none"}},
		{Key: "sandbox_strict", EnvKey: "MACLAW_SANDBOX_STRICT", Type: "bool", Default: false, Description: "Fail closed when sandbox is unavailable.", MutableAtRuntime: true, Writable: true},
		{Key: "sandbox_install_policy", EnvKey: "MACLAW_SANDBOX_INSTALL_POLICY", Type: "enum", Default: "suggest", Description: "Sandbox package installation policy. suggest only prints commands; run permits confirmed owner-triggered execution.", RestartRequired: true, Writable: true, AllowedValues: []string{"suggest", "run", "disabled"}},
		{Key: "sandbox_report_retention", EnvKey: "MACLAW_SANDBOX_REPORT_RETENTION", Type: "int", Default: 20, Description: "Number of sandbox diagnose reports to retain.", MutableAtRuntime: true, Writable: true},
		{Key: "sandbox_startup_diagnose", EnvKey: "MACLAW_SANDBOX_STARTUP_DIAGNOSE", Type: "bool", Default: false, Description: "Run a lightweight sandbox diagnose after service startup when sandbox mode is enabled.", MutableAtRuntime: true, Writable: true},
		{Key: "enable_local_bash", EnvKey: "MACLAW_ENABLE_LOCAL_BASH", Type: "bool", Default: false, Description: "Enable local bash tool execution.", RestartRequired: true, Writable: true},
		{Key: "local_bash_trusted_single_user", EnvKey: "MACLAW_LOCAL_BASH_TRUSTED_SINGLE_USER", Type: "bool", Default: false, Description: "Acknowledge trusted single-user local bash deployment.", RestartRequired: true, Writable: true},
		{Key: "local_bash_tenant_id", EnvKey: "MACLAW_LOCAL_BASH_TENANT_ID", Type: "string", Description: "Tenant id for local bash principal.", RestartRequired: true, Writable: true},
		{Key: "local_bash_user_id", EnvKey: "MACLAW_LOCAL_BASH_USER_ID", Type: "string", Description: "User id for local bash principal.", RestartRequired: true, Writable: true},
		{Key: "admin_secret", EnvKey: "MACLAW_ADMIN_SECRET", Type: "secret", Description: "Root admin API secret. Managed out-of-band.", RestartRequired: true, Sensitive: true, Writable: false},
		{Key: "token_secret", EnvKey: "MACLAW_TOKEN_SECRET", Type: "secret", Description: "JWT signing secret. Managed out-of-band.", RestartRequired: true, Sensitive: true, Writable: false},
		{Key: "credential_pepper", EnvKey: "MACLAW_CREDENTIAL_PEPPER", Type: "secret", Description: "Credential hash pepper. Managed out-of-band.", RestartRequired: true, Sensitive: true, Writable: false},
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Key < fields[j].Key })
	return fields
}

func validateAdminServiceConfigValues(values map[string]any) adminServiceConfigValidationResult {
	result := adminServiceConfigValidationResult{Valid: true, Normalized: map[string]any{}, Fields: map[string]adminServiceConfigSchemaField{}}
	fields := adminServiceConfigFieldMap()
	for _, field := range fields {
		result.Fields[field.Key] = field
	}
	for key, value := range values {
		field, ok := fields[key]
		if !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "unknown config key: "+key)
			continue
		}
		if !field.Writable {
			result.Valid = false
			result.Errors = append(result.Errors, "config key is read-only: "+key)
			continue
		}
		normalized, err := normalizeAdminServiceConfigValue(field, value)
		if err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, err.Error())
			continue
		}
		result.Normalized[key] = normalized
		if field.RestartRequired {
			result.RestartRequired = true
		}
		result.EnvPlan = append(result.EnvPlan, adminServiceConfigEnvPlanItem{Key: key, EnvKey: field.EnvKey, Value: maskSensitivePlanValue(field, normalized), Sensitive: field.Sensitive, Action: "set"})
	}
	if policy, _ := result.Normalized["sandbox_install_policy"].(string); policy == "run" {
		result.Warnings = append(result.Warnings, "sandbox_install_policy=run permits confirmed owner-triggered host package manager commands")
	}
	if enabled, _ := result.Normalized["enable_local_bash"].(bool); enabled {
		trusted, _ := result.Normalized["local_bash_trusted_single_user"].(bool)
		if !trusted && !adminEnvBool("MACLAW_LOCAL_BASH_TRUSTED_SINGLE_USER", false) {
			result.Valid = false
			result.Errors = append(result.Errors, "enable_local_bash requires local_bash_trusted_single_user=true")
		}
		if strings.TrimSpace(stringAny(result.Normalized["local_bash_tenant_id"])) == "" && strings.TrimSpace(os.Getenv("MACLAW_LOCAL_BASH_TENANT_ID")) == "" {
			result.Valid = false
			result.Errors = append(result.Errors, "enable_local_bash requires local_bash_tenant_id")
		}
		if strings.TrimSpace(stringAny(result.Normalized["local_bash_user_id"])) == "" && strings.TrimSpace(os.Getenv("MACLAW_LOCAL_BASH_USER_ID")) == "" {
			result.Valid = false
			result.Errors = append(result.Errors, "enable_local_bash requires local_bash_user_id")
		}
	}
	sort.Slice(result.EnvPlan, func(i, j int) bool { return result.EnvPlan[i].Key < result.EnvPlan[j].Key })
	return result
}

func buildAdminServiceConfigEnvironment() []adminServiceConfigEnvironmentItem {
	fields := adminServiceConfigSchema()
	items := make([]adminServiceConfigEnvironmentItem, 0, len(fields))
	for _, field := range fields {
		value, configured := os.LookupEnv(field.EnvKey)
		out := adminServiceConfigEnvironmentItem{
			Key:              field.Key,
			EnvKey:           field.EnvKey,
			Configured:       configured,
			Default:          field.Default,
			Sensitive:        field.Sensitive,
			Writable:         field.Writable,
			RestartRequired:  field.RestartRequired,
			MutableAtRuntime: field.MutableAtRuntime,
			Source:           envSource(field.EnvKey),
		}
		if configured {
			if field.Sensitive {
				out.Value = maskConfigured(value)
			} else {
				out.Value = value
			}
		}
		items = append(items, out)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	return items
}
func adminServiceConfigFieldMap() map[string]adminServiceConfigSchemaField {
	out := map[string]adminServiceConfigSchemaField{}
	for _, field := range adminServiceConfigSchema() {
		out[field.Key] = field
	}
	return out
}

func normalizeAdminServiceConfigValue(field adminServiceConfigSchemaField, value any) (any, error) {
	switch field.Type {
	case "bool":
		v, ok := value.(bool)
		if !ok {
			return nil, fmt.Errorf("%s must be a boolean", field.Key)
		}
		return v, nil
	case "enum":
		v := strings.TrimSpace(stringAny(value))
		for _, allowed := range field.AllowedValues {
			if v == allowed {
				return v, nil
			}
		}
		return nil, fmt.Errorf("%s must be one of: %s", field.Key, strings.Join(field.AllowedValues, ", "))
	case "string":
		v, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("%s must be a string", field.Key)
		}
		return strings.TrimSpace(v), nil
	case "int":
		v, ok := intAny(value)
		if !ok {
			return nil, fmt.Errorf("%s must be an integer", field.Key)
		}
		if field.Key == "sandbox_report_retention" && (v < 1 || v > 200) {
			return nil, fmt.Errorf("%s must be between 1 and 200", field.Key)
		}
		return v, nil
	default:
		return nil, fmt.Errorf("%s is not writable", field.Key)
	}
}

func maskSensitivePlanValue(field adminServiceConfigSchemaField, value any) any {
	if field.Sensitive {
		return maskConfigured(stringAny(value))
	}
	return value
}

func loadAdminServiceConfigDraft(dataRoot string) (adminServiceConfigDraft, error) {
	var draft adminServiceConfigDraft
	if err := readAdminJSON(dataRoot, "admin_service_config_draft.json", &draft); err != nil {
		if os.IsNotExist(err) {
			return adminServiceConfigDraft{Values: map[string]any{}}, nil
		}
		return adminServiceConfigDraft{}, err
	}
	if draft.Values == nil {
		draft.Values = map[string]any{}
	}
	return draft, nil
}

func saveAdminServiceConfigDraft(dataRoot string, draft adminServiceConfigDraft) error {
	return writeAdminJSON(dataRoot, "admin_service_config_draft.json", draft)
}

func sortedAnyKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func stringAny(value any) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", value)
}

func intAny(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		if v != float64(int(v)) {
			return 0, false
		}
		return int(v), true
	case json.Number:
		out, err := v.Int64()
		return int(out), err == nil
	default:
		return 0, false
	}
}

type adminServiceConfigExportPlan struct {
	GeneratedAt          time.Time                          `json:"generated_at"`
	RestartRequired      bool                               `json:"restart_required"`
	WillExecute          bool                               `json:"will_execute"`
	RequiresManualApply  bool                               `json:"requires_manual_apply"`
	Env                  []adminServiceConfigEnvPlanItem    `json:"env"`
	DotEnvContent        string                             `json:"dotenv_content,omitempty"`
	SystemdDropInContent string                             `json:"systemd_dropin_content,omitempty"`
	Notes                []string                           `json:"notes,omitempty"`
	Validation           adminServiceConfigValidationResult `json:"validation"`
}

func (s *HTTPServer) handleExportAdminServiceConfigPlan(w http.ResponseWriter, r *http.Request) {
	var in adminServiceConfigDraftRequest
	if !decodeOptionalJSON(w, r, &in) {
		return
	}
	values := in.Values
	if values == nil {
		draft, err := loadAdminServiceConfigDraft(s.svc.DataRoot())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		values = draft.Values
	}
	validation := validateAdminServiceConfigValues(values)
	if !validation.Valid {
		writeJSON(w, http.StatusBadRequest, validation)
		return
	}
	plan := buildAdminServiceConfigExportPlan(validation)
	_ = s.recordAdminAudit(r.Context(), "admin.service_config_export_plan", "service_config", "draft", map[string]string{"keys": strings.Join(sortedAnyKeys(validation.Normalized), ","), "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, plan)
}

func buildAdminServiceConfigExportPlan(validation adminServiceConfigValidationResult) adminServiceConfigExportPlan {
	plan := adminServiceConfigExportPlan{
		GeneratedAt:         time.Now().UTC(),
		RestartRequired:     validation.RestartRequired,
		WillExecute:         false,
		RequiresManualApply: true,
		Env:                 validation.EnvPlan,
		Validation:          validation,
		Notes: []string{
			"This plan does not modify the host. Apply it through your service manager, deployment manifest, or environment file.",
			"Restart MaClawSrv after applying restart_required values.",
		},
	}
	plan.DotEnvContent = buildDotEnvContent(validation)
	plan.SystemdDropInContent = buildSystemdDropInContent(validation)
	return plan
}

func buildDotEnvContent(validation adminServiceConfigValidationResult) string {
	if len(validation.EnvPlan) == 0 {
		return ""
	}
	items := append([]adminServiceConfigEnvPlanItem(nil), validation.EnvPlan...)
	sort.Slice(items, func(i, j int) bool { return items[i].EnvKey < items[j].EnvKey })
	var b strings.Builder
	b.WriteString("# Generated by MaClawSrv Admin Web. Review before applying.\n")
	for _, item := range items {
		if item.EnvKey == "" {
			continue
		}
		b.WriteString(item.EnvKey)
		b.WriteString("=")
		b.WriteString(dotEnvQuote(stringAny(rawPlanValue(item))))
		b.WriteString("\n")
	}
	return b.String()
}

func buildSystemdDropInContent(validation adminServiceConfigValidationResult) string {
	if len(validation.EnvPlan) == 0 {
		return ""
	}
	items := append([]adminServiceConfigEnvPlanItem(nil), validation.EnvPlan...)
	sort.Slice(items, func(i, j int) bool { return items[i].EnvKey < items[j].EnvKey })
	var b strings.Builder
	b.WriteString("# /etc/systemd/system/maclawsrv.service.d/override.conf\n")
	b.WriteString("# Generated by MaClawSrv Admin Web. Review before applying.\n")
	b.WriteString("[Service]\n")
	for _, item := range items {
		if item.EnvKey == "" {
			continue
		}
		b.WriteString("Environment=\"")
		b.WriteString(systemdEnvironmentEscape(item.EnvKey + "=" + stringAny(rawPlanValue(item))))
		b.WriteString("\"\n")
	}
	return b.String()
}

func rawPlanValue(item adminServiceConfigEnvPlanItem) any {
	if item.Sensitive {
		return "<redacted>"
	}
	return item.Value
}

func dotEnvQuote(value string) string {
	if value == "" {
		return `""`
	}
	if strings.ContainsAny(value, " \t\n\r\"'#$\\") {
		value = strings.ReplaceAll(value, "\\", "\\\\")
		value = strings.ReplaceAll(value, "\"", "\\\"")
		value = strings.ReplaceAll(value, "\n", "\\n")
		return "\"" + value + "\""
	}
	return value
}

func systemdEnvironmentEscape(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	value = strings.ReplaceAll(value, "%", "%%")
	return value
}
