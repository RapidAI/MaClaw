package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ConfigSection describes a group of related configuration keys.
type ConfigSection struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Keys        []ConfigKeySchema `json:"keys"`
}

// ConfigKeySchema describes a single configuration key within a section.
type ConfigKeySchema struct {
	Key         string   `json:"key"`
	Description string   `json:"description"`
	Type        string   `json:"type"`                    // string/bool/int/enum/list
	Default     string   `json:"default,omitempty"`
	ValidValues []string `json:"valid_values,omitempty"`  // for enum type
}

// ConfigChange represents a single configuration modification.
type ConfigChange struct {
	Section string `json:"section"`
	Key     string `json:"key"`
	Value   string `json:"value"`
}

// ImportReport summarises the result of a configuration import.
type ImportReport struct {
	Applied  int      `json:"applied"`
	Skipped  int      `json:"skipped"`
	Warnings []string `json:"warnings"`
}

// ConfigManager provides structured read/write access to AppConfig.
type ConfigManager struct {
	app    *App
	schema []ConfigSection
}

// NewConfigManager creates a ConfigManager and initialises its schema.
func NewConfigManager(app *App) *ConfigManager {
	m := &ConfigManager{app: app}
	m.initSchema()
	return m
}

// sensitiveKeys lists config keys whose values must be masked in output.
var sensitiveKeys = map[string]bool{
	"api_key":              true,
	"maclaw_llm_key":       true,
	"remote_machine_token": true,
	"proxy_password":       true,
}

// isSensitiveKey returns true when the key name refers to a secret value.
func isSensitiveKey(key string) bool {
	k := strings.ToLower(key)
	if sensitiveKeys[k] {
		return true
	}
	return strings.Contains(k, "api_key") || strings.Contains(k, "token") || strings.Contains(k, "password")
}

// maskSensitive masks a sensitive value, showing first 4 and last 4 chars.
// Values of 8 characters or fewer are fully masked.
func maskSensitive(value string) string {
	if value == "" {
		return ""
	}
	if len(value) <= 8 {
		return "****"
	}
	return value[:4] + "****" + value[len(value)-4:]
}

// maskIfSensitive returns the value masked when the key is sensitive.
func maskIfSensitive(key, value string) string {
	if isSensitiveKey(key) {
		return maskSensitive(value)
	}
	return value
}

// ---------------------------------------------------------------------------
// GetConfig reads the specified section's configuration values.
// If section is "all" or empty, a summary overview is returned.
// Sensitive fields are masked.
// ---------------------------------------------------------------------------

func (m *ConfigManager) GetConfig(section string) (string, error) {
	cfg, err := m.app.LoadConfig()
	if err != nil {
		return "", fmt.Errorf("failed to load config: %w", err)
	}

	sec := strings.TrimSpace(strings.ToLower(section))
	if sec == "" || sec == "all" {
		return m.configOverview(cfg), nil
	}

	switch sec {
	case "claude":
		return m.formatToolConfig("Claude", cfg.Claude), nil
	case "gemini":
		return m.formatToolConfig("Gemini", cfg.Gemini), nil
	case "codex":
		return m.formatToolConfig("Codex", cfg.Codex), nil
	case "opencode":
		return m.formatToolConfig("OpenCode", cfg.Opencode), nil
	case "codebuddy":
		return m.formatToolConfig("CodeBuddy", cfg.CodeBuddy), nil
	case "iflow":
		return m.formatToolConfig("iFlow", cfg.IFlow), nil
	case "kilo":
		return m.formatToolConfig("Kilo", cfg.Kilo), nil
	case "cursor":
		return m.formatToolConfig("Cursor", cfg.Cursor), nil
	case "remote":
		return m.formatRemoteConfig(cfg), nil
	case "projects":
		return m.formatProjectsConfig(cfg), nil
	case "maclaw_llm":
		return m.formatMaclawLLMConfig(cfg), nil
	case "maclaw_role":
		return m.formatMaclawRoleConfig(cfg), nil
	case "proxy":
		return m.formatProxyConfig(cfg), nil
	case "general":
		return m.formatGeneralConfig(cfg), nil
	case "power":
		return m.formatPowerConfig(cfg), nil
	default:
		return "", fmt.Errorf("unknown config section: %s", section)
	}
}

func (m *ConfigManager) configOverview(cfg AppConfig) string {
	var b strings.Builder
	b.WriteString("=== 配置概览 ===\n")
	b.WriteString(fmt.Sprintf("当前工具: %s\n", cfg.ActiveTool))
	b.WriteString(fmt.Sprintf("语言: %s\n", cfg.Language))
	b.WriteString(fmt.Sprintf("项目数量: %d\n", len(cfg.Projects)))
	b.WriteString(fmt.Sprintf("远程模式: %v\n", cfg.RemoteEnabled))
	b.WriteString(fmt.Sprintf("Maclaw LLM: %s\n", cfg.MaclawLLMModel))
	roleName := cfg.MaclawRoleName
	if roleName == "" {
		roleName = "MaClaw"
	}
	b.WriteString(fmt.Sprintf("MaClaw 角色: %s\n", roleName))
	b.WriteString(fmt.Sprintf("Claude 当前模型: %s\n", cfg.Claude.CurrentModel))
	b.WriteString(fmt.Sprintf("Gemini 当前模型: %s\n", cfg.Gemini.CurrentModel))
	b.WriteString(fmt.Sprintf("Codex 当前模型: %s\n", cfg.Codex.CurrentModel))
	return b.String()
}

func (m *ConfigManager) formatToolConfig(name string, tc ToolConfig) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("=== %s 配置 ===\n", name))
	b.WriteString(fmt.Sprintf("当前模型: %s\n", tc.CurrentModel))
	for _, model := range tc.Models {
		apiKey := maskIfSensitive("api_key", model.ApiKey)
		b.WriteString(fmt.Sprintf("  - %s: model_id=%s, url=%s, api_key=%s\n",
			model.ModelName, model.ModelId, model.ModelUrl, apiKey))
	}
	return b.String()
}

func (m *ConfigManager) formatRemoteConfig(cfg AppConfig) string {
	var b strings.Builder
	b.WriteString("=== 远程设置 ===\n")
	b.WriteString(fmt.Sprintf("remote_enabled: %v\n", cfg.RemoteEnabled))
	b.WriteString(fmt.Sprintf("remote_hub_url: %s\n", cfg.RemoteHubURL))
	b.WriteString(fmt.Sprintf("remote_email: %s\n", cfg.RemoteEmail))
	b.WriteString(fmt.Sprintf("remote_machine_id: %s\n", cfg.RemoteMachineID))
	b.WriteString(fmt.Sprintf("remote_machine_token: %s\n", maskSensitive(cfg.RemoteMachineToken)))
	b.WriteString(fmt.Sprintf("remote_heartbeat_sec: %d\n", cfg.RemoteHeartbeatSec))
	b.WriteString(fmt.Sprintf("default_launch_mode: %s\n", cfg.DefaultLaunchMode))
	return b.String()
}

func (m *ConfigManager) formatProjectsConfig(cfg AppConfig) string {
	var b strings.Builder
	b.WriteString("=== 项目列表 ===\n")
	b.WriteString(fmt.Sprintf("当前项目 ID: %s\n", cfg.CurrentProject))
	for _, p := range cfg.Projects {
		b.WriteString(fmt.Sprintf("  - %s (%s): path=%s, yolo=%v\n", p.Name, p.Id, p.Path, p.YoloMode))
	}
	return b.String()
}

func (m *ConfigManager) formatMaclawLLMConfig(cfg AppConfig) string {
	var b strings.Builder
	b.WriteString("=== Maclaw LLM 配置 ===\n")
	b.WriteString(fmt.Sprintf("maclaw_llm_url: %s\n", cfg.MaclawLLMUrl))
	b.WriteString(fmt.Sprintf("maclaw_llm_key: %s\n", maskSensitive(cfg.MaclawLLMKey)))
	b.WriteString(fmt.Sprintf("maclaw_llm_model: %s\n", cfg.MaclawLLMModel))
	proto := cfg.MaclawLLMProtocol
	if proto == "" {
		proto = "openai"
	}
	b.WriteString(fmt.Sprintf("maclaw_llm_protocol: %s\n", proto))
	b.WriteString(fmt.Sprintf("maclaw_llm_context_length: %d\n", cfg.MaclawLLMContextLength))
	b.WriteString(fmt.Sprintf("maclaw_llm_current_provider: %s\n", cfg.MaclawLLMCurrentProvider))
	maxIter := cfg.MaclawAgentMaxIterations
	switch {
	case maxIter > 0:
		b.WriteString(fmt.Sprintf("maclaw_agent_max_iterations: %d\n", maxIter))
	case maxIter < 0:
		b.WriteString("maclaw_agent_max_iterations: unlimited\n")
	default:
		b.WriteString("maclaw_agent_max_iterations: 12 (default)\n")
	}
	return b.String()
}

func (m *ConfigManager) formatMaclawRoleConfig(cfg AppConfig) string {
	var b strings.Builder
	b.WriteString("=== MaClaw 角色配置 ===\n")
	name := cfg.MaclawRoleName
	if name == "" {
		name = "MaClaw"
	}
	desc := cfg.MaclawRoleDescription
	if desc == "" {
		desc = "一个尽心尽责无所不能的软件开发管家"
	}
	b.WriteString(fmt.Sprintf("maclaw_role_name: %s\n", name))
	b.WriteString(fmt.Sprintf("maclaw_role_description: %s\n", desc))
	return b.String()
}

func (m *ConfigManager) formatProxyConfig(cfg AppConfig) string {
	var b strings.Builder
	b.WriteString("=== 代理设置 ===\n")
	b.WriteString(fmt.Sprintf("default_proxy_host: %s\n", cfg.DefaultProxyHost))
	b.WriteString(fmt.Sprintf("default_proxy_port: %s\n", cfg.DefaultProxyPort))
	b.WriteString(fmt.Sprintf("default_proxy_username: %s\n", cfg.DefaultProxyUsername))
	b.WriteString(fmt.Sprintf("default_proxy_password: %s\n", maskSensitive(cfg.DefaultProxyPassword)))
	return b.String()
}

func (m *ConfigManager) formatGeneralConfig(cfg AppConfig) string {
	var b strings.Builder
	b.WriteString("=== 通用设置 ===\n")
	b.WriteString(fmt.Sprintf("active_tool: %s\n", cfg.ActiveTool))
	b.WriteString(fmt.Sprintf("language: %s\n", cfg.Language))
	b.WriteString(fmt.Sprintf("power_optimization: %v\n", cfg.PowerOptimization))
	b.WriteString(fmt.Sprintf("screen_dim_timeout_min: %d\n", cfg.ScreenDimTimeoutMin))
	b.WriteString(fmt.Sprintf("check_update_on_startup: %v\n", cfg.CheckUpdateOnStartup))
	b.WriteString(fmt.Sprintf("hide_startup_popup: %v\n", cfg.HideStartupPopup))
	b.WriteString(fmt.Sprintf("hide_maclaw_llm_popup: %v\n", cfg.HideMaclawLLMPopup))
	return b.String()
}

func (m *ConfigManager) formatPowerConfig(cfg AppConfig) string {
	var b strings.Builder
	b.WriteString("=== 电源管理 ===\n")
	b.WriteString(fmt.Sprintf("power_optimization: %v\n", cfg.PowerOptimization))
	b.WriteString(fmt.Sprintf("screen_dim_timeout_min: %d\n", cfg.ScreenDimTimeoutMin))
	return b.String()
}

// ---------------------------------------------------------------------------
// UpdateConfig modifies a single configuration key and persists the change.
// Returns the old value (masked if sensitive).
// ---------------------------------------------------------------------------

func (m *ConfigManager) UpdateConfig(section, key, value string) (string, error) {
	cfg, err := m.app.LoadConfig()
	if err != nil {
		return "", fmt.Errorf("failed to load config: %w", err)
	}

	// Validate against schema
	if err := m.validateChange(section, key, value); err != nil {
		return "", err
	}

	oldValue, err := m.applyChange(&cfg, section, key, value)
	if err != nil {
		return "", err
	}

	if err := m.app.SaveConfig(cfg); err != nil {
		return "", fmt.Errorf("failed to save config: %w", err)
	}

	return maskIfSensitive(key, oldValue), nil
}

// ---------------------------------------------------------------------------
// BatchUpdate applies multiple configuration changes atomically.
// If any change fails validation, none are applied.
// ---------------------------------------------------------------------------

func (m *ConfigManager) BatchUpdate(changes []ConfigChange) error {
	// Phase 1: validate all changes
	for _, c := range changes {
		if err := m.validateChange(c.Section, c.Key, c.Value); err != nil {
			return fmt.Errorf("validation failed for %s.%s: %w", c.Section, c.Key, err)
		}
	}

	// Phase 2: load config and apply all
	cfg, err := m.app.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	for _, c := range changes {
		if _, err := m.applyChange(&cfg, c.Section, c.Key, c.Value); err != nil {
			return fmt.Errorf("apply failed for %s.%s: %w", c.Section, c.Key, err)
		}
	}

	// Phase 3: single save
	if err := m.app.SaveConfig(cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// ExportConfig serialises the current AppConfig to JSON with sensitive fields
// masked. The result is safe to share across devices.
// ---------------------------------------------------------------------------

func (m *ConfigManager) ExportConfig() (string, error) {
	cfg, err := m.app.LoadConfig()
	if err != nil {
		return "", fmt.Errorf("failed to load config: %w", err)
	}

	// Convert to a generic map so we can walk and mask sensitive values.
	raw, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("failed to marshal config: %w", err)
	}

	var cfgMap map[string]interface{}
	if err := json.Unmarshal(raw, &cfgMap); err != nil {
		return "", fmt.Errorf("failed to unmarshal config map: %w", err)
	}

	// Walk top-level keys and mask sensitive values.
	maskMapSensitive(cfgMap)

	out, err := json.MarshalIndent(cfgMap, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal exported config: %w", err)
	}
	return string(out), nil
}

// maskMapSensitive recursively masks sensitive string values in a map.
func maskMapSensitive(m map[string]interface{}) {
	for k, v := range m {
		switch val := v.(type) {
		case string:
			m[k] = maskIfSensitive(k, val)
		case map[string]interface{}:
			maskMapSensitive(val)
		case []interface{}:
			for _, item := range val {
				if sub, ok := item.(map[string]interface{}); ok {
					maskMapSensitive(sub)
				}
			}
		}
	}
}

// machineSpecificKeys lists keys that belong to the local machine and must
// not be overwritten during import.
var machineSpecificKeys = map[string]bool{
	"remote_machine_id":    true,
	"remote_machine_token": true,
}

// ---------------------------------------------------------------------------
// ImportConfig parses a JSON configuration string, validates it, and merges
// it into the current config. Machine-specific fields and masked values
// (containing "****") are skipped. Returns an ImportReport summarising the
// operation.
// ---------------------------------------------------------------------------

func (m *ConfigManager) ImportConfig(jsonData string) (*ImportReport, error) {
	// Parse incoming JSON into a generic map.
	var importMap map[string]interface{}
	if err := json.Unmarshal([]byte(jsonData), &importMap); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	// Load current config, marshal to map for merging.
	cfg, err := m.app.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	currentRaw, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal current config: %w", err)
	}

	var currentMap map[string]interface{}
	if err := json.Unmarshal(currentRaw, &currentMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal current config: %w", err)
	}

	report := &ImportReport{}

	// Merge import values into current config.
	mergeImport(currentMap, importMap, report, "")

	// Serialise merged map back to AppConfig.
	mergedRaw, err := json.Marshal(currentMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal merged config: %w", err)
	}

	var mergedCfg AppConfig
	if err := json.Unmarshal(mergedRaw, &mergedCfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal merged config: %w", err)
	}

	if err := m.app.SaveConfig(mergedCfg); err != nil {
		return nil, fmt.Errorf("failed to save config: %w", err)
	}

	return report, nil
}

// mergeImport recursively merges importMap values into currentMap, updating
// the ImportReport along the way.
func mergeImport(current, incoming map[string]interface{}, report *ImportReport, prefix string) {
	for k, inVal := range incoming {
		fullKey := k
		if prefix != "" {
			fullKey = prefix + "." + k
		}

		// Skip machine-specific keys.
		if machineSpecificKeys[k] {
			report.Skipped++
			report.Warnings = append(report.Warnings, fmt.Sprintf("skipped machine-specific key: %s", fullKey))
			continue
		}

		// Skip masked values (they came from a previous export and carry no real data).
		if strVal, ok := inVal.(string); ok && strings.Contains(strVal, "****") {
			report.Skipped++
			continue
		}

		curVal, exists := current[k]

		// If the incoming value is a nested map, recurse.
		if inMap, ok := inVal.(map[string]interface{}); ok {
			if curMap, ok2 := curVal.(map[string]interface{}); ok2 {
				mergeImport(curMap, inMap, report, fullKey)
				continue
			}
			// Key exists but current value is not a map (type mismatch), or
			// key doesn't exist at all — skip with warning either way.
			report.Skipped++
			if exists {
				report.Warnings = append(report.Warnings, fmt.Sprintf("type mismatch (expected map): %s", fullKey))
			} else {
				report.Warnings = append(report.Warnings, fmt.Sprintf("ignored unknown field: %s", fullKey))
			}
			continue
		}

		// If the key doesn't exist in current config, it's unknown — skip with warning.
		if !exists {
			report.Skipped++
			report.Warnings = append(report.Warnings, fmt.Sprintf("ignored unknown field: %s", fullKey))
			continue
		}

		// Apply the value.
		current[k] = inVal
		report.Applied++
	}
}

// GetSchema returns the full configuration schema.
func (m *ConfigManager) GetSchema() []ConfigSection {
	return m.schema
}

// ---------------------------------------------------------------------------
// validateChange checks that section/key exist in the schema and that value
// is acceptable (e.g. within ValidValues for enum types).
// ---------------------------------------------------------------------------

func (m *ConfigManager) validateChange(section, key, value string) error {
	sec := strings.ToLower(strings.TrimSpace(section))
	k := strings.ToLower(strings.TrimSpace(key))

	for _, s := range m.schema {
		if strings.ToLower(s.Name) != sec {
			continue
		}
		for _, ks := range s.Keys {
			if strings.ToLower(ks.Key) != k {
				continue
			}
			// Validate enum values
			if ks.Type == "enum" && len(ks.ValidValues) > 0 {
				found := false
				for _, v := range ks.ValidValues {
					if strings.EqualFold(v, value) {
						found = true
						break
					}
				}
				if !found {
					return fmt.Errorf("invalid value %q for %s.%s; valid values: %s",
						value, section, key, strings.Join(ks.ValidValues, ", "))
				}
			}
			// Validate bool
			if ks.Type == "bool" {
				v := strings.ToLower(value)
				if v != "true" && v != "false" {
					return fmt.Errorf("invalid value %q for %s.%s; expected true or false", value, section, key)
				}
			}
			return nil // found and valid
		}
		return fmt.Errorf("unknown key %q in section %q", key, section)
	}
	return fmt.Errorf("unknown section %q", section)
}

// ---------------------------------------------------------------------------
// applyChange sets a single key on the config struct and returns the old value.
// ---------------------------------------------------------------------------

func (m *ConfigManager) applyChange(cfg *AppConfig, section, key, value string) (string, error) {
	sec := strings.ToLower(strings.TrimSpace(section))
	k := strings.ToLower(strings.TrimSpace(key))

	// Helper to apply tool-level current_model changes
	applyToolModel := func(tc *ToolConfig) (string, error) {
		if k == "current_model" {
			old := tc.CurrentModel
			tc.CurrentModel = value
			return old, nil
		}
		return "", fmt.Errorf("unsupported key %q for tool section %q", key, section)
	}

	switch sec {
	case "claude":
		return applyToolModel(&cfg.Claude)
	case "gemini":
		return applyToolModel(&cfg.Gemini)
	case "codex":
		return applyToolModel(&cfg.Codex)
	case "opencode":
		return applyToolModel(&cfg.Opencode)
	case "codebuddy":
		return applyToolModel(&cfg.CodeBuddy)
	case "iflow":
		return applyToolModel(&cfg.IFlow)
	case "kilo":
		return applyToolModel(&cfg.Kilo)
	case "cursor":
		return applyToolModel(&cfg.Cursor)

	case "remote":
		return m.applyRemoteChange(cfg, k, value)
	case "projects":
		return m.applyProjectsChange(cfg, k, value)
	case "maclaw_llm":
		return m.applyMaclawLLMChange(cfg, k, value)
	case "maclaw_role":
		return m.applyMaclawRoleChange(cfg, k, value)
	case "proxy":
		return m.applyProxyChange(cfg, k, value)
	case "general":
		return m.applyGeneralChange(cfg, k, value)
	case "power":
		return m.applyPowerChange(cfg, k, value)
	}
	return "", fmt.Errorf("unknown section %q", section)
}

func (m *ConfigManager) applyProjectsChange(cfg *AppConfig, key, value string) (string, error) {
	switch key {
	case "current_project":
		old := cfg.CurrentProject
		cfg.CurrentProject = value
		return old, nil
	}
	return "", fmt.Errorf("unsupported projects key %q", key)
}

func (m *ConfigManager) applyRemoteChange(cfg *AppConfig, key, value string) (string, error) {
	switch key {
	case "remote_enabled":
		old := fmt.Sprintf("%v", cfg.RemoteEnabled)
		cfg.RemoteEnabled = strings.EqualFold(value, "true")
		return old, nil
	case "remote_hub_url":
		old := cfg.RemoteHubURL
		cfg.RemoteHubURL = value
		return old, nil
	case "remote_email":
		old := cfg.RemoteEmail
		cfg.RemoteEmail = value
		return old, nil
	case "remote_heartbeat_sec":
		old := fmt.Sprintf("%d", cfg.RemoteHeartbeatSec)
		var n int
		if _, err := fmt.Sscanf(value, "%d", &n); err != nil {
			return "", fmt.Errorf("invalid integer value %q for remote_heartbeat_sec", value)
		}
		cfg.RemoteHeartbeatSec = n
		return old, nil
	case "default_launch_mode":
		old := cfg.DefaultLaunchMode
		cfg.DefaultLaunchMode = value
		return old, nil
	}
	return "", fmt.Errorf("unsupported remote key %q", key)
}

func (m *ConfigManager) applyMaclawLLMChange(cfg *AppConfig, key, value string) (string, error) {
	switch key {
	case "maclaw_llm_url":
		old := cfg.MaclawLLMUrl
		cfg.MaclawLLMUrl = value
		return old, nil
	case "maclaw_llm_key":
		old := cfg.MaclawLLMKey
		cfg.MaclawLLMKey = value
		return old, nil
	case "maclaw_llm_model":
		old := cfg.MaclawLLMModel
		cfg.MaclawLLMModel = value
		return old, nil
	case "maclaw_llm_protocol":
		old := cfg.MaclawLLMProtocol
		cfg.MaclawLLMProtocol = value
		return old, nil
	case "maclaw_llm_context_length":
		old := fmt.Sprintf("%d", cfg.MaclawLLMContextLength)
		n, _ := strconv.Atoi(value)
		cfg.MaclawLLMContextLength = n
		return old, nil
	case "maclaw_llm_current_provider":
		old := cfg.MaclawLLMCurrentProvider
		cfg.MaclawLLMCurrentProvider = value
		return old, nil
	case "maclaw_agent_max_iterations":
		old := fmt.Sprintf("%d", cfg.MaclawAgentMaxIterations)
		n, err := strconv.Atoi(value)
		if err != nil {
			return "", fmt.Errorf("invalid integer value for maclaw_agent_max_iterations: %q", value)
		}
		switch {
		case n > 0:
			if n > maxAgentIterationsCap {
				n = maxAgentIterationsCap
			}
			cfg.MaclawAgentMaxIterations = n
		case n == 0:
			cfg.MaclawAgentMaxIterations = -1 // sentinel for "unlimited"
		default:
			cfg.MaclawAgentMaxIterations = 0 // zero-value = not configured → default(12)
		}
		return old, nil
	}
	return "", fmt.Errorf("unsupported maclaw_llm key %q", key)
}

func (m *ConfigManager) applyMaclawRoleChange(cfg *AppConfig, key, value string) (string, error) {
	switch key {
	case "maclaw_role_name":
		old := cfg.MaclawRoleName
		cfg.MaclawRoleName = value
		return old, nil
	case "maclaw_role_description":
		old := cfg.MaclawRoleDescription
		cfg.MaclawRoleDescription = value
		return old, nil
	}
	return "", fmt.Errorf("unsupported maclaw_role key %q", key)
}

func (m *ConfigManager) applyProxyChange(cfg *AppConfig, key, value string) (string, error) {
	switch key {
	case "default_proxy_host":
		old := cfg.DefaultProxyHost
		cfg.DefaultProxyHost = value
		return old, nil
	case "default_proxy_port":
		old := cfg.DefaultProxyPort
		cfg.DefaultProxyPort = value
		return old, nil
	case "default_proxy_username":
		old := cfg.DefaultProxyUsername
		cfg.DefaultProxyUsername = value
		return old, nil
	case "default_proxy_password":
		old := cfg.DefaultProxyPassword
		cfg.DefaultProxyPassword = value
		return old, nil
	}
	return "", fmt.Errorf("unsupported proxy key %q", key)
}

func (m *ConfigManager) applyGeneralChange(cfg *AppConfig, key, value string) (string, error) {
	switch key {
	case "active_tool":
		old := cfg.ActiveTool
		cfg.ActiveTool = value
		return old, nil
	case "language":
		old := cfg.Language
		cfg.Language = value
		return old, nil
	case "power_optimization":
		old := fmt.Sprintf("%v", cfg.PowerOptimization)
		cfg.PowerOptimization = strings.EqualFold(value, "true")
		return old, nil
	case "screen_dim_timeout_min":
		old := fmt.Sprintf("%d", cfg.ScreenDimTimeoutMin)
		var n int
		if _, err := fmt.Sscanf(value, "%d", &n); err != nil {
			return "", fmt.Errorf("invalid integer value %q for screen_dim_timeout_min", value)
		}
		cfg.ScreenDimTimeoutMin = n
		return old, nil
	case "check_update_on_startup":
		old := fmt.Sprintf("%v", cfg.CheckUpdateOnStartup)
		cfg.CheckUpdateOnStartup = strings.EqualFold(value, "true")
		return old, nil
	case "hide_startup_popup":
		old := fmt.Sprintf("%v", cfg.HideStartupPopup)
		cfg.HideStartupPopup = strings.EqualFold(value, "true")
		return old, nil
	case "hide_maclaw_llm_popup":
		old := fmt.Sprintf("%v", cfg.HideMaclawLLMPopup)
		cfg.HideMaclawLLMPopup = strings.EqualFold(value, "true")
		return old, nil
	case "maclaw_debug_tool_calls":
		old := fmt.Sprintf("%v", cfg.MaclawDebugToolCalls)
		cfg.MaclawDebugToolCalls = strings.EqualFold(value, "true")
		return old, nil
	}
	return "", fmt.Errorf("unsupported general key %q", key)
}

func (m *ConfigManager) applyPowerChange(cfg *AppConfig, key, value string) (string, error) {
	switch key {
	case "power_optimization":
		old := fmt.Sprintf("%v", cfg.PowerOptimization)
		cfg.PowerOptimization = strings.EqualFold(value, "true")
		return old, nil
	case "screen_dim_timeout_min":
		old := fmt.Sprintf("%d", cfg.ScreenDimTimeoutMin)
		var n int
		if _, err := fmt.Sscanf(value, "%d", &n); err != nil {
			return "", fmt.Errorf("invalid integer value %q for screen_dim_timeout_min", value)
		}
		cfg.ScreenDimTimeoutMin = n
		return old, nil
	}
	return "", fmt.Errorf("unsupported power key %q", key)
}

// ---------------------------------------------------------------------------
// initSchema initialises the full configuration schema covering all sections:
// tool models, projects, remote, proxy, maclaw LLM, and general settings.
// ---------------------------------------------------------------------------

func (m *ConfigManager) initSchema() {
	toolModelSection := func(name, desc string) ConfigSection {
		return ConfigSection{
			Name:        name,
			Description: desc,
			Keys: []ConfigKeySchema{
				{Key: "current_model", Description: "当前使用的模型名称", Type: "string"},
			},
		}
	}

	m.schema = []ConfigSection{
		// 1. Tool model sections
		toolModelSection("claude", "Claude 工具模型配置"),
		toolModelSection("gemini", "Gemini 工具模型配置"),
		toolModelSection("codex", "Codex 工具模型配置"),
		toolModelSection("opencode", "OpenCode 工具模型配置"),
		toolModelSection("iflow", "iFlow 工具模型配置"),
		toolModelSection("kilo", "Kilo 工具模型配置"),
		toolModelSection("cursor", "Cursor 工具模型配置"),
		toolModelSection("codebuddy", "CodeBuddy 工具模型配置"),

		// 2. Projects section
		{
			Name:        "projects",
			Description: "项目管理",
			Keys: []ConfigKeySchema{
				{Key: "current_project", Description: "当前活跃项目 ID", Type: "string"},
			},
		},

		// 3. Remote settings
		{
			Name:        "remote",
			Description: "远程连接设置",
			Keys: []ConfigKeySchema{
				{Key: "remote_enabled", Description: "是否启用远程模式", Type: "bool", Default: "false"},
				{Key: "remote_hub_url", Description: "Hub 服务器地址", Type: "string"},
				{Key: "remote_email", Description: "远程账户邮箱", Type: "string"},
				{Key: "remote_heartbeat_sec", Description: "心跳间隔（秒）", Type: "int", Default: "30"},
				{Key: "default_launch_mode", Description: "默认启动模式", Type: "enum", Default: "local", ValidValues: []string{"local", "remote"}},
			},
		},

		// 4. Proxy settings
		{
			Name:        "proxy",
			Description: "代理设置",
			Keys: []ConfigKeySchema{
				{Key: "default_proxy_host", Description: "默认代理主机地址", Type: "string"},
				{Key: "default_proxy_port", Description: "默认代理端口", Type: "string"},
				{Key: "default_proxy_username", Description: "默认代理用户名", Type: "string"},
				{Key: "default_proxy_password", Description: "默认代理密码", Type: "string"},
			},
		},

		// 5. Maclaw LLM
		{
			Name:        "maclaw_llm",
			Description: "Maclaw LLM 配置",
			Keys: []ConfigKeySchema{
				{Key: "maclaw_llm_url", Description: "Maclaw LLM 服务地址", Type: "string"},
				{Key: "maclaw_llm_key", Description: "Maclaw LLM API 密钥", Type: "string"},
				{Key: "maclaw_llm_model", Description: "Maclaw LLM 模型名称", Type: "string"},
				{Key: "maclaw_llm_context_length", Description: "LLM 上下文长度 (tokens)，0=默认128000", Type: "int", Default: "0"},
				{Key: "maclaw_llm_current_provider", Description: "当前 LLM 提供商", Type: "string"},
				{Key: "maclaw_agent_max_iterations", Description: "Agent 最大推理轮次（正整数=固定上限，0=无限制，负数=恢复默认12）", Type: "int", Default: "12"},
			},
		},

		// 6. Maclaw Role
		{
			Name:        "maclaw_role",
			Description: "MaClaw 角色配置",
			Keys: []ConfigKeySchema{
				{Key: "maclaw_role_name", Description: "Agent 角色名称", Type: "string", Default: "MaClaw"},
				{Key: "maclaw_role_description", Description: "Agent 角色描述", Type: "string", Default: "一个尽心尽责无所不能的软件开发管家"},
			},
		},

		// 7. General settings
		{
			Name:        "general",
			Description: "通用设置",
			Keys: []ConfigKeySchema{
				{Key: "active_tool", Description: "当前激活的编程工具", Type: "enum", ValidValues: []string{"claude", "gemini", "codex", "opencode", "iflow", "kilo", "cursor", "codebuddy"}},
				{Key: "language", Description: "界面语言", Type: "enum", Default: "zh", ValidValues: []string{"zh", "en"}},
				{Key: "power_optimization", Description: "是否启用省电优化", Type: "bool", Default: "false"},
				{Key: "screen_dim_timeout_min", Description: "无操作多少分钟后熄屏（0=禁用）", Type: "int", Default: "3"},
				{Key: "check_update_on_startup", Description: "启动时检查更新", Type: "bool", Default: "true"},
				{Key: "hide_startup_popup", Description: "隐藏启动弹窗", Type: "bool", Default: "false"},
				{Key: "hide_maclaw_llm_popup", Description: "隐藏MaClaw LLM未配置提示弹窗", Type: "bool", Default: "false"},
				{Key: "maclaw_debug_tool_calls", Description: "MaClaw Debug 开关：开启后显示工具调用过程", Type: "bool", Default: "false"},
			},
		},

		// 8. Power management
		{
			Name:        "power",
			Description: "电源管理",
			Keys: []ConfigKeySchema{
				{Key: "power_optimization", Description: "是否启用防锁屏（远程任务运行时阻止系统锁屏）", Type: "bool", Default: "false"},
				{Key: "screen_dim_timeout_min", Description: "无操作多少分钟后熄屏节能（0=禁用）", Type: "int", Default: "3"},
			},
		},
	}
}


// ---------------------------------------------------------------------------
// Helpers used by other packages (e.g. tests) to serialise schema to JSON.
// ---------------------------------------------------------------------------

func (m *ConfigManager) SchemaJSON() (string, error) {
	data, err := json.MarshalIndent(m.schema, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
