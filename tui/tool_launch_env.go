package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/configfile"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

// buildToolEnv 根据工具名和配置构建环境变量。
// 移植自 gui/app.go 的 buildXxxLaunchEnv 系列函数，不依赖 GUI App。
func buildToolEnv(toolName string, cfg corelib.AppConfig, projectDir string) (map[string]string, *corelib.ModelConfig, error) {
	tool := normalizeToolName(toolName)
	tc, err := toolConfigFromApp(cfg, tool)
	if err != nil {
		return nil, nil, err
	}

	selected := resolveSelectedModel(tc)
	if selected == nil {
		return nil, nil, fmt.Errorf("工具 %s 未配置 provider", tool)
	}
	if !isValidToolProvider(*selected) {
		return nil, nil, fmt.Errorf("provider %q 未配置 API Key", selected.ModelName)
	}

	var env map[string]string
	switch tool {
	case "claude":
		env = buildClaudeEnv(cfg, selected, projectDir)
	case "codex":
		env = buildCodexEnv(selected)
	case "gemini":
		env = buildGeminiEnv(selected)
	case "opencode":
		env = buildOpencodeEnv(selected)
	case "iflow":
		env = buildIFlowEnv(selected)
	case "kilo":
		env = buildKiloEnv(selected)
	case "cursor":
		env = buildCursorEnv(selected)
	default:
		return nil, nil, fmt.Errorf("不支持的工具: %s", tool)
	}

	// 注入代理
	applyProxy(env, cfg, projectDir)

	return env, selected, nil
}

func normalizeToolName(name string) string {
	t := strings.ToLower(strings.TrimSpace(name))
	if t == "" {
		return "claude"
	}
	return t
}

func toolConfigFromApp(cfg corelib.AppConfig, tool string) (corelib.ToolConfig, error) {
	switch tool {
	case "claude":
		return cfg.Claude, nil
	case "codex":
		return cfg.Codex, nil
	case "gemini":
		return cfg.Gemini, nil
	case "opencode":
		return cfg.Opencode, nil
	case "iflow":
		return cfg.IFlow, nil
	case "kilo":
		return cfg.Kilo, nil
	case "cursor":
		return cfg.Cursor, nil
	case "codebuddy":
		return cfg.CodeBuddy, nil
	default:
		return corelib.ToolConfig{}, fmt.Errorf("未知工具: %s", tool)
	}
}

func resolveSelectedModel(tc corelib.ToolConfig) *corelib.ModelConfig {
	target := strings.TrimSpace(tc.CurrentModel)
	for _, m := range tc.Models {
		if strings.EqualFold(m.ModelName, target) {
			cp := m
			return &cp
		}
	}
	// 如果没有匹配，返回第一个有效的
	for _, m := range tc.Models {
		if isValidToolProvider(m) {
			cp := m
			return &cp
		}
	}
	return nil
}

func isValidToolProvider(m corelib.ModelConfig) bool {
	if m.IsBuiltin || m.HasSubscription {
		return true
	}
	return strings.TrimSpace(m.ApiKey) != ""
}

func buildClaudeEnv(cfg corelib.AppConfig, m *corelib.ModelConfig, projectDir string) map[string]string {
	env := map[string]string{
		"CLAUDE_CODE_USE_COLORS":        "true",
		"CLAUDE_CODE_MAX_OUTPUT_TOKENS": "64000",
		"MAX_THINKING_TOKENS":           "10000",
	}
	if !m.IsBuiltin {
		if m.ApiKey != "" {
			env["ANTHROPIC_AUTH_TOKEN"] = m.ApiKey
		}
		if m.ModelUrl != "" {
			env["ANTHROPIC_BASE_URL"] = m.ModelUrl
		}
		if m.ModelId != "" {
			env["ANTHROPIC_MODEL"] = m.ModelId
			env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = m.ModelId
			env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = m.ModelId
			env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = m.ModelId
		}
		// Write settings.json unless 百度千帆 (handled below with different URL)
		lowerName := strings.ToLower(m.ModelName)
		if lowerName != "百度千帆" && lowerName != "qianfan" {
			if err := configfile.WriteClaudeSettings(m.ApiKey, m.ModelUrl, m.ModelId); err != nil {
				fmt.Fprintf(os.Stderr, "[claude-config] failed to write settings.json: %v\n", err)
			}
		}
	}
	// 百度千帆特殊处理
	switch strings.ToLower(m.ModelName) {
	case "百度千帆", "qianfan":
		modelID := m.ModelId
		if modelID == "" {
			modelID = "qianfan-code-latest"
		}
		env["ANTHROPIC_AUTH_TOKEN"] = m.ApiKey
		env["ANTHROPIC_BASE_URL"] = "https://qianfan.baidubce.com/anthropic/coding"
		env["ANTHROPIC_MODEL"] = modelID
		env["ANTHROPIC_SMALL_FAST_MODEL"] = modelID
		env["CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"] = "1"
		env["API_TIMEOUT_MS"] = "600000"
		// Also write settings.json for 百度千帆
		if err := configfile.WriteClaudeSettings(m.ApiKey, "https://qianfan.baidubce.com/anthropic/coding", modelID); err != nil {
			fmt.Fprintf(os.Stderr, "[claude-config] failed to write settings.json: %v\n", err)
		}
	}
	// TeamMode
	for _, proj := range cfg.Projects {
		if proj.Path == projectDir || proj.Id == cfg.CurrentProject {
			if proj.TeamMode {
				env["CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS"] = "1"
			}
			break
		}
	}
	return env
}

func buildCodexEnv(m *corelib.ModelConfig) map[string]string {
	env := map[string]string{}
	if !m.IsBuiltin {
		// Only pass API key via env var; all other config goes through config.toml.
		if m.ApiKey != "" {
			env["OPENAI_API_KEY"] = m.ApiKey
		}
		// Write auth.json + config.toml atomically with rollback via configfile package.
		// This preserves user's MCP servers, profiles, and comments in config.toml.
		if err := configfile.WriteCodexConfig(m.ApiKey, m.ModelUrl, m.ModelId, m.ModelName, m.WireApi); err != nil {
			fmt.Fprintf(os.Stderr, "[codex-config] failed to write config: %v\n", err)
			// Fallback to legacy full-regeneration approach
			if err2 := writeCodexConfigToml(m); err2 != nil {
				fmt.Fprintf(os.Stderr, "[codex-config] fallback also failed: %v\n", err2)
			}
		}
	}
	return env
}

// writeCodexConfigToml generates ~/.codex/config.toml with unified provider config.
func writeCodexConfigToml(m *corelib.ModelConfig) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	configToml := remote.BuildCodexConfigToml(m)
	configPath := filepath.Join(dir, "config.toml")
	configBytes := []byte(configToml)
	// Skip write if unchanged
	if existing, err := os.ReadFile(configPath); err == nil {
		if bytes.Equal(existing, configBytes) {
			return nil
		}
	}
	return os.WriteFile(configPath, configBytes, 0644)
}

func buildGeminiEnv(m *corelib.ModelConfig) map[string]string {
	env := map[string]string{}
	if !m.IsBuiltin {
		if m.ApiKey != "" {
			env["GEMINI_API_KEY"] = m.ApiKey
			env["GOOGLE_API_KEY"] = m.ApiKey
		}
		if m.ModelUrl != "" {
			env["GOOGLE_GEMINI_BASE_URL"] = m.ModelUrl
		}
		if m.ModelId != "" {
			env["GEMINI_MODEL"] = m.ModelId
		}
		// Write ~/.gemini/.env + settings.json for persistence across subprocess restarts
		if err := configfile.WriteGeminiConfig(m.ApiKey, m.ModelUrl, m.ModelId); err != nil {
			fmt.Fprintf(os.Stderr, "[gemini-config] failed to write config: %v\n", err)
		}
	}
	return env
}

func buildOpencodeEnv(m *corelib.ModelConfig) map[string]string {
	env := map[string]string{}
	if m.ApiKey != "" {
		env["OPENCODE_API_KEY"] = m.ApiKey
	}
	if m.ModelUrl != "" {
		env["OPENCODE_BASE_URL"] = m.ModelUrl
	}
	if m.ModelId != "" {
		env["OPENCODE_MODEL"] = m.ModelId
	}
	return env
}

func buildIFlowEnv(m *corelib.ModelConfig) map[string]string {
	env := map[string]string{}
	if m.ApiKey != "" {
		env["OPENAI_API_KEY"] = m.ApiKey
		env["IFLOW_API_KEY"] = m.ApiKey
	}
	if m.ModelUrl != "" {
		env["OPENAI_BASE_URL"] = m.ModelUrl
		env["IFLOW_BASE_URL"] = m.ModelUrl
	}
	if m.ModelId != "" {
		env["IFLOW_MODEL"] = m.ModelId
	}
	return env
}

func buildKiloEnv(m *corelib.ModelConfig) map[string]string {
	env := map[string]string{}
	if m.ApiKey != "" {
		env["OPENAI_API_KEY"] = m.ApiKey
		env["KILO_API_KEY"] = m.ApiKey
	}
	if m.ModelUrl != "" {
		env["OPENAI_BASE_URL"] = m.ModelUrl
		env["KILO_BASE_URL"] = m.ModelUrl
	}
	if m.ModelId != "" {
		env["KILO_MODEL"] = m.ModelId
	}
	return env
}

func buildCursorEnv(m *corelib.ModelConfig) map[string]string {
	env := map[string]string{}
	if m.ApiKey != "" {
		env["CURSOR_API_KEY"] = m.ApiKey
	}
	if m.ModelUrl != "" {
		env["CURSOR_BASE_URL"] = m.ModelUrl
	}
	return env
}

// applyProxy 注入代理环境变量。
func applyProxy(env map[string]string, cfg corelib.AppConfig, projectDir string) {
	// 查找项目级代理
	for _, proj := range cfg.Projects {
		if proj.Path == projectDir || proj.Id == cfg.CurrentProject {
			if proj.UseProxy && proj.ProxyHost != "" {
				proxyURL := buildProxyURL(proj.ProxyHost, proj.ProxyPort, proj.ProxyUsername, proj.ProxyPassword)
				setProxyEnv(env, proxyURL)
				return
			}
			break
		}
	}
	// 全局默认代理
	if cfg.DefaultProxyHost != "" {
		proxyURL := buildProxyURL(cfg.DefaultProxyHost, cfg.DefaultProxyPort, cfg.DefaultProxyUsername, cfg.DefaultProxyPassword)
		setProxyEnv(env, proxyURL)
	}
}

func buildProxyURL(host, port, user, pass string) string {
	h := strings.TrimSpace(host)
	if h == "" {
		return ""
	}
	p := strings.TrimSpace(port)
	u := strings.TrimSpace(user)
	pw := strings.TrimSpace(pass)

	var url string
	if u != "" && pw != "" {
		url = fmt.Sprintf("http://%s:%s@%s", u, pw, h)
	} else {
		url = fmt.Sprintf("http://%s", h)
	}
	if p != "" {
		url += ":" + p
	}
	return url
}

func setProxyEnv(env map[string]string, proxyURL string) {
	if proxyURL == "" {
		return
	}
	env["HTTP_PROXY"] = proxyURL
	env["HTTPS_PROXY"] = proxyURL
	env["http_proxy"] = proxyURL
	env["https_proxy"] = proxyURL
}
