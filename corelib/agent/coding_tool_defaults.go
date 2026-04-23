package agent

// coding_tool_defaults.go provides default coding tool provider presets.
// Shared between GUI and TUI so both have the same defaults.

import "github.com/RapidAI/CodeClaw/corelib"

// CodingToolInfo describes a supported coding tool.
type CodingToolInfo struct {
	Name       string // "claude", "codex", "gemini", etc.
	Display    string // "Claude", "Codex", "Gemini"
	EnvKey     string // "ANTHROPIC_AUTH_TOKEN"
	EnvBaseURL string // "ANTHROPIC_BASE_URL"
	Binary     string // "claude", "codex", "gemini"
}

// SupportedCodingTools returns the list of coding tools with their env var mappings.
func SupportedCodingTools() []CodingToolInfo {
	return []CodingToolInfo{
		{Name: "claude", Display: "Claude", EnvKey: "ANTHROPIC_AUTH_TOKEN", EnvBaseURL: "ANTHROPIC_BASE_URL", Binary: "claude"},
		{Name: "codex", Display: "Codex", EnvKey: "OPENAI_API_KEY", EnvBaseURL: "OPENAI_BASE_URL", Binary: "codex"},
		{Name: "gemini", Display: "Gemini", EnvKey: "GEMINI_API_KEY", EnvBaseURL: "GOOGLE_GEMINI_BASE_URL", Binary: "gemini"},
		{Name: "opencode", Display: "OpenCode", EnvKey: "OPENCODE_API_KEY", EnvBaseURL: "OPENCODE_BASE_URL", Binary: "opencode"},
	}
}

// DefaultClaudeProviders returns the preset provider list for Claude.
func DefaultClaudeProviders() []corelib.ModelConfig {
	return []corelib.ModelConfig{
		{ModelName: "GLM", ModelId: "glm-4.7", ModelUrl: "https://open.bigmodel.cn/api/anthropic", IsBuiltin: false},
		{ModelName: "Kimi", ModelId: "kimi-k2-thinking", ModelUrl: "https://api.kimi.com/coding", IsBuiltin: false},
		{ModelName: "Doubao", ModelId: "doubao-seed-code-preview-latest", ModelUrl: "https://ark.cn-beijing.volces.com/api/coding", IsBuiltin: false},
		{ModelName: "DeepSeek", ModelId: "deepseek-chat", ModelUrl: "https://api.deepseek.com/anthropic", IsBuiltin: false},
		{ModelName: "MiniMax", ModelId: "MiniMax-M2.1", ModelUrl: "https://api.minimaxi.com/anthropic", IsBuiltin: false},
		{ModelName: "ChatFire", ModelId: "sonnet", ModelUrl: "https://api.chatfire.cn", IsBuiltin: false},
		{ModelName: "Custom", ModelId: "", ModelUrl: "", IsCustom: true},
	}
}

// DefaultCodexProviders returns the preset provider list for Codex.
func DefaultCodexProviders() []corelib.ModelConfig {
	return []corelib.ModelConfig{
		{ModelName: "ChatFire", ModelId: "gpt-5.1-codex-mini", ModelUrl: "https://api.chatfire.cn/v1", WireApi: "responses"},
		{ModelName: "DeepSeek", ModelId: "deepseek-chat", ModelUrl: "https://api.deepseek.com/v1"},
		{ModelName: "GLM", ModelId: "glm-5-turbo", ModelUrl: "https://open.bigmodel.cn/api/coding/paas/v4"},
		{ModelName: "Kimi", ModelId: "kimi-for-coding", ModelUrl: "https://api.kimi.com/coding/v1"},
		{ModelName: "MiniMax", ModelId: "MiniMax-M2.1", ModelUrl: "https://api.minimaxi.com/v1"},
		{ModelName: "Custom", ModelId: "", ModelUrl: "", IsCustom: true},
	}
}

// DefaultGeminiProviders returns the preset provider list for Gemini.
func DefaultGeminiProviders() []corelib.ModelConfig {
	return []corelib.ModelConfig{
		{ModelName: "ChatFire", ModelId: "gemini-2.5-pro", ModelUrl: "https://api.chatfire.cn/v1beta/models/gemini-2.5-pro:generateContent"},
		{ModelName: "Custom", ModelId: "", ModelUrl: "", IsCustom: true},
	}
}

// GetToolConfig returns the ToolConfig for a given tool name from AppConfig.
func GetToolConfig(cfg corelib.AppConfig, toolName string) corelib.ToolConfig {
	switch toolName {
	case "claude":
		return cfg.Claude
	case "codex":
		return cfg.Codex
	case "gemini":
		return cfg.Gemini
	case "opencode":
		return cfg.Opencode
	default:
		return corelib.ToolConfig{}
	}
}

// SetToolConfig sets the ToolConfig for a given tool name in AppConfig.
func SetToolConfig(cfg *corelib.AppConfig, toolName string, tc corelib.ToolConfig) {
	switch toolName {
	case "claude":
		cfg.Claude = tc
	case "codex":
		cfg.Codex = tc
	case "gemini":
		cfg.Gemini = tc
	case "opencode":
		cfg.Opencode = tc
	}
}

// DefaultProvidersForTool returns the default provider presets for a tool.
func DefaultProvidersForTool(toolName string) []corelib.ModelConfig {
	switch toolName {
	case "claude":
		return DefaultClaudeProviders()
	case "codex":
		return DefaultCodexProviders()
	case "gemini":
		return DefaultGeminiProviders()
	default:
		return []corelib.ModelConfig{
			{ModelName: "Custom", ModelId: "", ModelUrl: "", IsCustom: true},
		}
	}
}
