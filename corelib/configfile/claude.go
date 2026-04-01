package configfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ClaudeSettingsPath returns ~/.claude/settings.json
func ClaudeSettingsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "settings.json")
}

// CodeGenSettingsPath returns ~/.maclaw/codegen/settings.json
// 这是 TigerClaw SSO 专用的配置文件，与 Claude Code 的标准路径分开管理，
// 避免覆盖用户在 ~/.claude/settings.json 中的手动配置。
func CodeGenSettingsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".maclaw", "codegen", "settings.json")
}

// internalOnlyFields are fields that cc-switch filters out before writing
// to Claude Code's settings.json — they are not recognized by Claude Code
// and can cause unexpected behavior.
var internalOnlyFields = []string{
	"api_format", "apiFormat",
	"openrouter_compat_mode", "openrouterCompatMode",
}

// WriteClaudeSettings writes ~/.claude/settings.json with the provider's
// env configuration. This is what cc-switch does instead of relying solely
// on process environment variables.
//
// The settings.json approach is more stable because:
// 1. Claude Code reads it on startup and on internal subprocess restarts
// 2. Environment variables can be lost when Claude Code spawns child processes
// 3. It persists across terminal sessions
func WriteClaudeSettings(apiKey, baseURL, modelID string) error {
	if apiKey == "" {
		return nil // builtin provider, skip
	}
	return writeAnthropicSettings(ClaudeSettingsPath(), apiKey, baseURL, modelID)
}

// WriteCodeGenSettings writes the CodeGen SSO credentials to
// ~/.maclaw/codegen/settings.json. 该文件专用于 TigerClaw SSO 认证结果的持久化，
// 与 ~/.claude/settings.json 分开管理，不会干扰用户的 Claude Code 配置。
// TigerClaw Code 启动时会优先读取此文件中的 env 字段作为认证凭证。
func WriteCodeGenSettings(apiKey, baseURL, modelID string) error {
	if apiKey == "" {
		return nil
	}
	return writeAnthropicSettings(CodeGenSettingsPath(), apiKey, baseURL, modelID)
}

func WriteClaudeProviderSettings(providerName, apiKey, baseURL, modelID string) error {
	if err := WriteClaudeSettings(apiKey, baseURL, modelID); err != nil {
		return err
	}
	if strings.EqualFold(strings.TrimSpace(providerName), "codegen") {
		if err := WriteCodeGenSettings(apiKey, baseURL, modelID); err != nil {
			return err
		}
	}
	return nil
}

// ReadClaudeSettings reads the current ~/.claude/settings.json for backfill.
func ReadClaudeSettings() (map[string]interface{}, error) {
	data, err := os.ReadFile(ClaudeSettingsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read claude settings: %w", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse claude settings: %w", err)
	}
	return result, nil
}

// ReadCodeGenSettings reads the current ~/.maclaw/codegen/settings.json for backfill.
func ReadCodeGenSettings() (map[string]interface{}, error) {
	data, err := os.ReadFile(CodeGenSettingsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read codegen settings: %w", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse codegen settings: %w", err)
	}
	return result, nil
}

func writeAnthropicSettings(settingsPath, apiKey, baseURL, modelID string) error {
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return fmt.Errorf("create settings dir: %w", err)
	}

	existing := make(map[string]interface{})
	if data, err := os.ReadFile(settingsPath); err == nil {
		_ = json.Unmarshal(data, &existing)
	}

	env, _ := existing["env"].(map[string]interface{})
	if env == nil {
		env = make(map[string]interface{})
	}

	env["ANTHROPIC_AUTH_TOKEN"] = apiKey
	if baseURL != "" {
		env["ANTHROPIC_BASE_URL"] = baseURL
	}
	if modelID != "" {
		env["ANTHROPIC_MODEL"] = modelID
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = modelID
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = modelID
		env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = modelID
		delete(env, "ANTHROPIC_SMALL_FAST_MODEL")
	}

	existing["env"] = env
	for _, field := range internalOnlyFields {
		delete(existing, field)
	}

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings json: %w", err)
	}
	data = append(data, '\n')
	if current, err := os.ReadFile(settingsPath); err == nil && string(current) == string(data) {
		return nil
	}
	return AtomicWrite(settingsPath, data)
}

// UpdateClaudeCodeConfig 更新 Claude Code 的 settings.json，设置 ANTHROPIC 相关的环境变量。
func UpdateClaudeCodeConfig(apiKey, baseURL, modelID string) error {
	return WriteClaudeSettings(apiKey, baseURL, modelID)
}
