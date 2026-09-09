package config

import (
	"reflect"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// AppConfigScope describes which host owns an AppConfig field.  Keeping this
// metadata in corelib/config prevents GUI and MaClawSrv from maintaining
// separate field-name allow/deny lists.
type AppConfigScope string

// AppConfigSchemaVersion is bumped when field ownership or visibility
// semantics change.  Transport adapters may expose it alongside their
// historical "items" array so clients can cache and compare schemas safely.
const AppConfigSchemaVersion = "maclaw.app-config/v1"

const (
	AppConfigScopeShared  AppConfigScope = "shared"
	AppConfigScopeGUIOnly AppConfigScope = "gui_only"
	AppConfigScopeService AppConfigScope = "service"
)

// AppConfigFieldMetadata is the transport-neutral schema for one AppConfig
// field.  Consumers may add a policy overlay, but must not redefine the base
// ownership, secret, or user-web visibility rules.
type AppConfigFieldMetadata struct {
	Key              string         `json:"key"`
	Scope            AppConfigScope `json:"scope"`
	Type             string         `json:"type"`
	Title            string         `json:"title"`
	Description      string         `json:"description,omitempty"`
	Secret           bool           `json:"secret,omitempty"`
	Mutable          bool           `json:"mutable"`
	RestartRequired  bool           `json:"restart_required"`
	HeadlessSupport  bool           `json:"headless_supported"`
	UserWebVisible   bool           `json:"user_web_visible"`
	ComplexUserField bool           `json:"complex_user_field"`
}

// AppConfigSchema returns a deterministic, reflection-backed schema for all
// exported JSON fields on corelib.AppConfig.  Reflection ensures newly added
// fields cannot silently disappear from the schema; ownership and visibility
// are supplied by the centralized policy sets below.
func AppConfigSchema() []AppConfigFieldMetadata {
	t := reflect.TypeOf(corelib.AppConfig{})
	fields := make([]AppConfigFieldMetadata, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		key := appConfigJSONFieldName(field)
		if key == "" {
			continue
		}
		scope := AppConfigScopeService
		// GUI-only ownership takes precedence over the broader "shared client" set.
		if !IsAvailableInMaClawSrv(key) {
			scope = AppConfigScopeGUIOnly
		} else if IsSharedClientField(key) {
			scope = AppConfigScopeShared
		}
		complexField := IsUserWebComplexField(key)
		fields = append(fields, AppConfigFieldMetadata{
			Key:              key,
			Scope:            scope,
			Type:             appConfigFieldType(field.Type),
			Title:            titleFromConfigKey(key),
			Description:      "Shared AppConfig field " + key + ".",
			Secret:           configKeyLooksSecret(key),
			Mutable:          true,
			HeadlessSupport:  scope != AppConfigScopeGUIOnly,
			UserWebVisible:   IsUserWebVisibleField(key),
			ComplexUserField: complexField,
		})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Key < fields[j].Key })
	return fields
}

// AppConfigFieldMap returns a map copy suitable for request-time lookups.
func AppConfigFieldMap() map[string]AppConfigFieldMetadata {
	out := make(map[string]AppConfigFieldMetadata)
	for _, field := range AppConfigSchema() {
		out[field.Key] = field
	}
	return out
}

// SharedClientConfigKeys returns a copy of the canonical shared field set.
func SharedClientConfigKeys() map[string]bool {
	out := make(map[string]bool, len(sharedClientConfigKeys))
	for key, value := range sharedClientConfigKeys {
		out[key] = value
	}
	return out
}

// MaClawSrvHiddenConfigKeys returns a copy of fields that are GUI/desktop
// owned and must not be exposed as MaClawSrv user configuration.
func MaClawSrvHiddenConfigKeys() map[string]struct{} {
	out := make(map[string]struct{}, len(maclawSrvHiddenConfigKeys))
	for key := range maclawSrvHiddenConfigKeys {
		out[key] = struct{}{}
	}
	return out
}

// UserWebHiddenConfigKeys returns the canonical user-web hidden set.  The
// returned map is a copy so a transport cannot mutate global policy.
func UserWebHiddenConfigKeys() map[string]struct{} {
	out := make(map[string]struct{}, len(userWebHiddenConfigKeys)+len(userWebComplexConfigKeys))
	for key := range userWebHiddenConfigKeys {
		out[key] = struct{}{}
	}
	for key := range userWebComplexConfigKeys {
		out[key] = struct{}{}
	}
	return out
}

// UserWebComplexConfigKeys returns fields that are intentionally managed by
// dedicated structured editors rather than the basic user-web form.
func UserWebComplexConfigKeys() map[string]struct{} {
	out := make(map[string]struct{}, len(userWebComplexConfigKeys))
	for key := range userWebComplexConfigKeys {
		out[key] = struct{}{}
	}
	return out
}

func IsSharedClientField(key string) bool {
	return sharedClientConfigKeys[strings.TrimSpace(key)]
}

func IsAvailableInMaClawSrv(key string) bool {
	_, hidden := maclawSrvHiddenConfigKeys[strings.TrimSpace(key)]
	return !hidden
}

func IsUserWebComplexField(key string) bool {
	_, ok := userWebComplexConfigKeys[strings.TrimSpace(key)]
	return ok
}

// IsUserWebVisibleField is the single visibility predicate used by both the
// service schema and MaClawSrv user-facing config projection.
func IsUserWebVisibleField(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" || IsUserWebComplexField(key) {
		return false
	}
	if _, hidden := userWebHiddenConfigKeys[key]; hidden {
		return false
	}
	return !IsUserWebRetiredSettingsKey(key)
}

func IsUserWebRetiredSettingsKey(key string) bool {
	key = strings.TrimSpace(key)
	if key == "working_directory" || key == "data_dir" || key == "default_launch_mode" {
		return true
	}
	for _, prefix := range []string{"pet_", "floating_", "hide_", "power_", "workstation_", "check_", "pause_", "env_", "remote_", "local_"} {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

// JSONFieldName exposes the canonical AppConfig JSON tag parser to adapters
// that still need reflection for compatibility DTOs.
func JSONFieldName(field reflect.StructField) string { return appConfigJSONFieldName(field) }

// AppConfigFieldType exposes the schema type mapping used for legacy parameter
// definitions.
func AppConfigFieldType(t reflect.Type) string { return appConfigFieldType(t) }

// TitleFromConfigKey exposes the deterministic human-readable title mapping.
func TitleFromConfigKey(key string) string { return titleFromConfigKey(key) }

// ConfigKeyLooksSecret exposes the shared secret classification policy.
func ConfigKeyLooksSecret(key string) bool { return configKeyLooksSecret(key) }

// CopyAppConfigFields copies only fields accepted by keep from current into
// next.  The helper is intentionally reflection-backed and JSON-tag based so
// transports can project newly added AppConfig fields without another manual
// switch or assignment list.
func CopyAppConfigFields(current, next corelib.AppConfig, keep func(string) bool) corelib.AppConfig {
	if keep == nil {
		return next
	}
	currentValue := reflect.ValueOf(current)
	nextValue := reflect.ValueOf(&next).Elem()
	typ := nextValue.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		key := appConfigJSONFieldName(field)
		if key == "" || !keep(key) {
			continue
		}
		dst := nextValue.Field(i)
		if dst.CanSet() {
			dst.Set(currentValue.Field(i))
		}
	}
	return next
}

func appConfigJSONFieldName(field reflect.StructField) string {
	if field.PkgPath != "" {
		return ""
	}
	tag := field.Tag.Get("json")
	if tag == "-" {
		return ""
	}
	name := strings.Split(tag, ",")[0]
	if name != "" {
		return name
	}
	return field.Name
}

func appConfigFieldType(t reflect.Type) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Bool:
		return "bool"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.Map, reflect.Struct:
		return "object"
	default:
		return "string"
	}
}

func titleFromConfigKey(key string) string {
	parts := strings.Split(key, "_")
	for i, part := range parts {
		if part == "" {
			continue
		}
		upper := strings.ToUpper(part)
		switch upper {
		case "LLM", "MCP", "MIS", "QQ", "ASR", "TTS", "UI", "URL", "ID", "API", "CDN", "WSS", "YOLO", "IM", "VAD":
			parts[i] = upper
		default:
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, " ")
}

func configKeyLooksSecret(key string) bool {
	key = strings.ToLower(key)
	if strings.Contains(key, "token_usage") || strings.Contains(key, "token_budget") {
		return false
	}
	for _, marker := range []string{"key", "secret", "token", "password", "credential"} {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}

// sharedClientConfigKeys is the only source of truth for fields shared by
// GUI and MaClawSrv client configuration.  Keep values keyed by JSON name.
var sharedClientConfigKeys = map[string]bool{
	"maclaw_llm_url": true, "maclaw_llm_key": true, "maclaw_llm_model": true,
	"maclaw_llm_protocol": true, "maclaw_llm_context_length": true, "maclaw_llm_timeout_sec": true,
	"agent_response_timeout_sec": true, "skill_runner_timeout_sec": true,
	"maclaw_llm_providers": true, "maclaw_llm_current_provider": true, "llm_prompt_cache": true,
	"maclaw_agent_max_iterations": true, "subagent_concurrency": true,
	"web_search_providers": true, "web_search_current_provider": true,
	"default_proxy_enabled": true, "default_proxy_protocol": true, "default_proxy_host": true,
	"default_proxy_port": true, "default_proxy_username": true, "default_proxy_password": true,
	"default_proxy_bypass": true, "default_proxy_scope_maclaw": true,
	"default_proxy_scope_agent": true,
	"mcp_servers":               true, "local_mcp_servers": true, "ssh_hosts": true,
	"skill_hub_urls": true, "external_skill_dirs": true, "skill_sources_allowed": true,
	"security_policy_mode": true, "hub_security_centralized": true, "network_level": true,
	"semantic_tool_scope_routing": true,
	"network_allowlist":           true, "language": true, "ui_mode": true, "working_directory": true,
	"vector_search_enabled": true, "asr_enabled": true, "tts_voice_id": true, "tts_enabled": true,
	"im_progress_nudge_enabled": true, "knowledge_vision_llm": true, "knowledge_include_images": true,
	"auxiliary_llm": true, "model_routes": true, "daily_llm_budget_usd": true, "moa": true,
}

var maclawSrvHiddenConfigKeys = map[string]struct{}{
	"claude": {}, "codex": {}, "opencode": {}, "codebuddy": {}, "iflow": {}, "kilo": {},
	"projects": {}, "current_project": {}, "active_tool": {}, "default_tool": {}, "default_tool_provider": {},
	"show_codex": {}, "show_opencode": {}, "show_codebuddy": {}, "show_iflow": {}, "show_kilo": {},
	"extra_tool_configs": {}, "use_windows_terminal": {}, "nl_skills": {},
	// Desktop/coding-tool proxy scoping. Consumed only by the GUI client
	// (gui/app_proxy.go, guiapp/app_proxy.go) and absent from
	// sharedClientConfigKeys, so publishing it in the MaClawSrv or user-web
	// schema would expose a toggle that never applies to those surfaces.
	"default_proxy_scope_coding_tools": {},
}

var userWebComplexConfigKeys = map[string]struct{}{
	"maclaw_llm_protocol": {}, "maclaw_llm_context_length": {}, "maclaw_llm_timeout_sec": {},
	"skill_runner_timeout_sec": {}, "maclaw_llm_current_provider": {}, "maclaw_llm_providers": {},
	"llm_prompt_cache": {}, "auxiliary_llm": {}, "model_routes": {},
	"database_profiles": {},
}

var userWebHiddenConfigKeys = map[string]struct{}{
	"claude": {}, "codex": {}, "opencode": {}, "codebuddy": {}, "iflow": {}, "kilo": {},
	"projects": {}, "current_project": {}, "active_tool": {}, "default_tool": {}, "default_tool_provider": {},
	"show_codex": {}, "show_opencode": {}, "show_codebuddy": {}, "show_iflow": {}, "show_kilo": {},
	"extra_tool_configs": {}, "use_windows_terminal": {}, "nl_skills": {},
	"default_proxy_scope_coding_tools": {},
	"llm_token_usage":                  {}, "mcp_servers": {}, "local_mcp_servers": {}, "ssh_hosts": {},
	"skill_hub_urls": {}, "external_skill_dirs": {}, "skill_sources_allowed": {},
	"remote_user_id": {}, "remote_tenant_id": {}, "remote_tenant_name": {}, "remote_machine_id": {},
	"remote_machine_name": {}, "remote_machine_token": {}, "remote_viewer_token": {},
	"skill_market_session_token": {}, "remote_client_id": {}, "remote_sn": {}, "env_check_done": {},
	"last_env_check_time": {}, "onboarding_done": {}, "floating_btn_x": {}, "floating_btn_y": {},
	"floating_btn_position_set": {}, "noise_floor_calibrated": {}, "speech_level_calibrated": {},
}
