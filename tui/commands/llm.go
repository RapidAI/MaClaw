package commands

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/brand"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/i18n"
	llmclient "github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/oauth"
)

// RunLLM 执行 llm 子命令。
func RunLLM(args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw llm <setup|test|ping|providers|status|set-provider|login|usage>")
	}
	switch args[0] {
	case "setup":
		return llmSetup(args[1:])
	case "test":
		return llmTest(args[1:])
	case "ping":
		return llmPing(args[1:])
	case "providers":
		return llmProviders(args[1:])
	case "status":
		return llmStatus(args[1:])
	case "set-provider":
		return llmSetProvider(args[1:])
	case "set-max-iterations":
		return llmSetMaxIterations(args[1:])
	case "get-max-iterations":
		return llmGetMaxIterations(args[1:])
	case "login":
		return llmLogin(args[1:])
	case "usage":
		return llmUsage(args[1:])
	default:
		return NewUsageError("unknown llm action: %s", args[0])
	}
}

func llmTUISetupHint() string {
	return llmTUISetupHintForLang("zh")
}

// LoadLLMConfig 从本地配置文件加载 LLM 配置（供外部复用）。
func LoadLLMConfig() (corelib.MaclawLLMConfig, error) {
	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return corelib.MaclawLLMConfig{}, err
	}
	llm := corelib.MaclawLLMConfig{
		URL:           cfg.MaclawLLMUrl,
		Key:           cfg.MaclawLLMKey,
		Model:         cfg.MaclawLLMModel,
		Protocol:      cfg.MaclawLLMProtocol,
		ContextLength: cfg.MaclawLLMContextLength,
		TimeoutSec:    cfg.MaclawLLMTimeoutSec,
		ProviderName:  cfg.MaclawLLMCurrentProvider,
		ThinkingMode:  cfg.MaclawLLMThinkingMode,
	}
	// Resolve AgentType and SupportsVision from the current provider (not stored as flat fields).
	for _, p := range cfg.MaclawLLMProviders {
		if p.Name == cfg.MaclawLLMCurrentProvider {
			if token := p.CodexSubscriptionOAuthToken(); token != "" {
				llm.Key = token
			} else if strings.TrimSpace(llm.Key) == "" {
				llm.Key = strings.TrimSpace(p.Key)
			}
			if p.TimeoutSec > 0 {
				llm.TimeoutSec = p.TimeoutSec
			}
			llm.AgentType = p.AgentType
			llm.SupportsVision = p.SupportsVision
			llm.WireAPI = p.WireAPI
			break
		}
	}
	if strings.TrimSpace(llm.Key) == "" && llmConfigUsesHubService(cfg) {
		llm.Key = strings.TrimSpace(cfg.RemoteViewerToken)
	}
	return llm, nil
}

// presetProvider 定义一个预置 LLM 服务商（与 GUI defaultMaclawLLMProviders 对齐）。
type presetProvider struct {
	Name          string
	URL           string
	Model         string
	Protocol      string
	ContextLength int
	TimeoutSec    int
	AuthType      string // "apikey", "oauth", "sso", "none"
	AgentType     string
	Hint          string // 显示给用户的说明
}

// presetProviders 返回 TUI 可用的预置服务商列表。
// 排除 OAuth 类型（需要浏览器），只保留填 API Key 即可使用的服务商。
func presetProviders() []presetProvider {
	return []presetProvider{
		{
			Name: "智谱 GLM (龙虾)", URL: "https://open.bigmodel.cn/api/coding/paas/v4",
			Model: "glm-5-turbo", ContextLength: 400000, TimeoutSec: corelib.DefaultLLMTimeoutSec,
			AuthType: "apikey", Hint: "open.bigmodel.cn 获取 API Key",
		},
		{
			Name: "智谱 GLM (Coding)", URL: "https://open.bigmodel.cn/api/anthropic",
			Model: "GLM-5.2", Protocol: "anthropic", AgentType: "claude code 2.0",
			ContextLength: 400000, TimeoutSec: corelib.DefaultLLMTimeoutSec,
			AuthType: "apikey", Hint: "open.bigmodel.cn 获取 API Key（Anthropic 协议）",
		},
		{
			Name: "MiniMax", URL: "https://api.minimaxi.com/v1",
			Model: "MiniMax-M2.7", ContextLength: 110000, TimeoutSec: corelib.DefaultLLMTimeoutSec,
			AuthType: "apikey", Hint: "platform.minimaxi.com 获取 API Key",
		},
		{
			Name: "Kimi", URL: "https://api.kimi.com/coding/v1",
			Model: "kimi-for-coding", ContextLength: 110000, TimeoutSec: corelib.DefaultLLMTimeoutSec,
			AuthType: "apikey", AgentType: "claude code 2.0", Hint: "platform.moonshot.cn 获取 API Key",
		},
		{
			Name: "讯飞星辰", URL: "https://maas-coding-api.cn-huabei-1.xf-yun.com/v2",
			Model: "astron-code-latest", ContextLength: 110000, TimeoutSec: corelib.DefaultLLMTimeoutSec,
			AuthType: "apikey", Hint: "training.xfyun.cn 获取 API Key",
		},
		{
			Name: "OpenAI (API Key)", URL: "https://api.openai.com/v1",
			Model: "gpt-4o", ContextLength: 110000, TimeoutSec: corelib.DefaultLLMTimeoutSec,
			AuthType: "apikey", Hint: "platform.openai.com 获取 API Key",
		},
		{
			Name: "Anthropic", URL: "https://api.anthropic.com",
			Model: "claude-sonnet-4-20250514", Protocol: "anthropic", AgentType: "claude code 2.0",
			ContextLength: 110000, TimeoutSec: corelib.DefaultLLMTimeoutSec,
			AuthType: "apikey", Hint: "console.anthropic.com 获取 API Key",
		},
		{
			Name: "Ollama Local", URL: "http://localhost:11434/v1",
			Model: "qwen2.5-coder:32b", ContextLength: 32000, TimeoutSec: corelib.DefaultLLMTimeoutSec,
			AuthType: "none", Hint: "本机/内网 Ollama，通常不需要 API Key",
		},
		{
			Name: "LM Studio Local", URL: "http://localhost:1234/v1",
			Model: "auto", ContextLength: 32000, TimeoutSec: corelib.DefaultLLMTimeoutSec,
			AuthType: "none", Hint: "本机 LM Studio OpenAI 兼容接口",
		},
		{
			Name: "自定义", URL: "", Model: "",
			AuthType: "apikey", Hint: "手动输入 URL、Model；本地/内网地址可跳过 API Key",
		},
	}
}

// llmSetup 交互式 LLM 配置向导。
func llmSetup(args []string) error {
	providers := presetProviders()

	Println()
	Println("  ╭─────────────────────────────────────╮")
	Println("  │       MaClaw LLM 配置向导           │")
	Println("  ╰─────────────────────────────────────╯")
	Println()
	Println("  选择 LLM 服务商：")
	Println()
	for i, p := range providers {
		Printf("    %d. %-20s %s\n", i+1, p.Name, p.Hint)
	}
	Println()
	if brand.Current().ID == "qianxin" {
		Printf("    另外：maclaw llm login openai   — OpenAI Codex 订阅（OAuth）\n")
		Printf("          maclaw llm login codegen  — CodeGen 企业 SSO\n")
	} else {
		Printf("    另外：maclaw llm login openai   — OpenAI Codex 订阅（OAuth）\n")
	}
	Println()
	Print("  请输入编号 (1-" + fmt.Sprintf("%d", len(providers)) + "): ")

	var choice int
	fmt.Scanln(&choice)
	if choice < 1 || choice > len(providers) {
		return fmt.Errorf("无效选择")
	}

	selected := providers[choice-1]
	apiURL := selected.URL
	model := selected.Model
	protocol := selected.Protocol
	agentType := selected.AgentType
	contextLength := selected.ContextLength

	// 自定义服务商需要输入 URL 和 Model
	if selected.Name == "自定义" {
		Print("  API URL: ")
		fmt.Scanln(&apiURL)
		apiURL = strings.TrimSpace(apiURL)
		if apiURL == "" {
			return fmt.Errorf("URL 不能为空")
		}

		Print("  模型名称: ")
		fmt.Scanln(&model)
		model = strings.TrimSpace(model)
		if model == "" {
			return fmt.Errorf("模型名称不能为空")
		}

		Print("  协议 (openai/anthropic，默认 openai): ")
		var proto string
		fmt.Scanln(&proto)
		proto = strings.TrimSpace(proto)
		if proto == "anthropic" {
			protocol = "anthropic"
		}
	}

	// 输入 API Key；本地/内网兼容接口可以跳过。
	apiKeyRequired := selected.AuthType != "none" && llmURLUsuallyNeedsKey(apiURL)
	if apiKeyRequired {
		Print("  API Key: ")
	} else {
		Print("  API Key（可选，直接回车跳过）: ")
	}
	var apiKey string
	fmt.Scanln(&apiKey)
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" && apiKeyRequired {
		return fmt.Errorf("API Key 不能为空")
	}
	providerAuthType := selected.AuthType
	if selected.AuthType == "none" || (!apiKeyRequired && apiKey == "") {
		providerAuthType = "none"
	}

	// 保存到配置
	store := NewFileConfigStore(ResolveDataDir())
	appCfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	providerName := selected.Name
	if providerName == "自定义" {
		providerName = "Custom1"
	}

	// 更新或创建 provider
	found := false
	for i, p := range appCfg.MaclawLLMProviders {
		if p.Name == providerName {
			appCfg.MaclawLLMProviders[i].URL = apiURL
			appCfg.MaclawLLMProviders[i].Key = apiKey
			appCfg.MaclawLLMProviders[i].Model = model
			appCfg.MaclawLLMProviders[i].Protocol = protocol
			appCfg.MaclawLLMProviders[i].AgentType = agentType
			appCfg.MaclawLLMProviders[i].AuthType = providerAuthType
			appCfg.MaclawLLMProviders[i].IsCustom = selected.Name == "自定义"
			if contextLength > 0 {
				appCfg.MaclawLLMProviders[i].ContextLength = contextLength
			}
			found = true
			break
		}
	}
	if !found {
		p := corelib.MaclawLLMProvider{
			Name:          providerName,
			URL:           apiURL,
			Key:           apiKey,
			Model:         model,
			Protocol:      protocol,
			AgentType:     agentType,
			ContextLength: contextLength,
			TimeoutSec:    selected.TimeoutSec,
			AuthType:      providerAuthType,
			IsCustom:      selected.Name == "自定义",
		}
		appCfg.MaclawLLMProviders = append(appCfg.MaclawLLMProviders, p)
	}

	// 设为当前 provider 并同步 legacy 字段
	appCfg.MaclawLLMCurrentProvider = providerName
	appCfg.MaclawLLMUrl = strings.TrimRight(apiURL, "/")
	appCfg.MaclawLLMKey = apiKey
	appCfg.MaclawLLMModel = model
	appCfg.MaclawLLMProtocol = protocol
	appCfg.MaclawLLMContextLength = contextLength

	if err := store.SaveConfig(appCfg); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}

	Println()
	Printf("  已配置 %s\n", providerName)
	Printf("    模型: %s\n", model)
	Printf("    URL:  %s\n", apiURL)
	Printf("    Key:  %s****\n", apiKey[:min(4, len(apiKey))])
	Println()
	Println("  运行 maclaw llm test 验证配置是否正确。")
	return nil
}

func llmStatus(args []string) error {
	fs := flag.NewFlagSet("llm status", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	llm, err := LoadLLMConfig()
	configured := err == nil && strings.TrimSpace(llm.URL) != "" && strings.TrimSpace(llm.Model) != ""
	store := NewFileConfigStore(ResolveDataDir())
	cfg, cfgErr := store.LoadConfig()
	hubServiceReady := cfgErr == nil && remoteHubServiceReady(cfg)
	missingKey := false
	mcpCount := 0
	lang := "zh"
	if cfgErr == nil {
		lang = i18n.NormalizeLang(cfg.Language)
		mcpCount = len(cfg.MCPServers) + len(cfg.LocalMCPServers)
		configured = llmConfiguredFromAppConfig(cfg)
		missingKey = llmMissingProviderKey(cfg)
	}

	info := map[string]interface{}{
		"configured":        configured,
		"url":               llm.URL,
		"model":             llm.Model,
		"protocol":          llm.Protocol,
		"hub_service_ready": hubServiceReady,
		"missing_key":       missingKey,
		"mcp_count":         mcpCount,
		"next_action":       llmNextAction(configured, hubServiceReady, mcpCount, lang),
		"next_tui_command":  llmNextTUICommand(configured, hubServiceReady, mcpCount),
	}
	if llm.ContextLength > 0 {
		info["context_length"] = llm.ContextLength
	}
	if *jsonOut {
		return PrintJSON(info)
	}
	if lang == "en" {
		printLLMStatusEN(llm, configured, hubServiceReady, mcpCount, missingKey)
		return nil
	}
	if !configured {
		Println("LLM 状态: 未配置")
		if missingKey {
			Println("  " + llmMissingKeyHint(lang))
		} else {
			Println("  " + llmUnconfiguredHint(hubServiceReady, lang))
		}
		Printf("  下一步: %s\n", llmNextAction(false, hubServiceReady, mcpCount, lang))
		return nil
	}
	Println("LLM 状态: 已配置")
	Printf("  URL:      %s\n", llm.URL)
	Printf("  Model:    %s\n", llm.Model)
	Printf("  Protocol: %s\n", orDefault(llm.Protocol, "openai"))
	if llm.ContextLength > 0 {
		Printf("  Context:  %d tokens\n", llm.ContextLength)
	}
	if llm.Key != "" {
		Printf("  API Key:  %s****\n", llm.Key[:min(4, len(llm.Key))])
	}
	Printf("  下一步: %s\n", llmNextAction(true, hubServiceReady, mcpCount, lang))
	return nil
}

func printLLMStatusEN(llm corelib.MaclawLLMConfig, configured bool, hubServiceReady bool, mcpCount int, missingKey bool) {
	if !configured {
		Println("LLM status: not configured")
		if missingKey {
			Println("  " + llmMissingKeyHint("en"))
		} else {
			Println("  " + llmUnconfiguredHint(hubServiceReady, "en"))
		}
		Printf("  Next: %s\n", llmNextAction(false, hubServiceReady, mcpCount, "en"))
		return
	}
	Println("LLM status: configured")
	Printf("  URL:      %s\n", llm.URL)
	Printf("  Model:    %s\n", llm.Model)
	Printf("  Protocol: %s\n", orDefault(llm.Protocol, "openai"))
	if llm.ContextLength > 0 {
		Printf("  Context:  %d tokens\n", llm.ContextLength)
	}
	if llm.Key != "" {
		Printf("  API Key:  %s****\n", llm.Key[:min(4, len(llm.Key))])
	}
	Printf("  Next: %s\n", llmNextAction(true, hubServiceReady, mcpCount, "en"))
}

func llmConfiguredFromAppConfig(cfg corelib.AppConfig) bool {
	if strings.TrimSpace(cfg.MaclawLLMUrl) == "" || strings.TrimSpace(cfg.MaclawLLMModel) == "" {
		return false
	}
	if llmConfigUsesHubService(cfg) {
		return currentLLMProviderKeyFromAppConfig(cfg) != "" || strings.TrimSpace(cfg.RemoteViewerToken) != ""
	}
	if llmProviderNeedsKeyFromAppConfig(cfg) {
		return currentLLMProviderKeyFromAppConfig(cfg) != ""
	}
	return true
}

func llmMissingProviderKey(cfg corelib.AppConfig) bool {
	if strings.TrimSpace(cfg.MaclawLLMUrl) == "" || strings.TrimSpace(cfg.MaclawLLMModel) == "" {
		return false
	}
	if llmConfigUsesHubService(cfg) {
		return false
	}
	return llmProviderNeedsKeyFromAppConfig(cfg) && currentLLMProviderKeyFromAppConfig(cfg) == ""
}

func currentLLMProviderKeyFromAppConfig(cfg corelib.AppConfig) string {
	current := strings.TrimSpace(cfg.MaclawLLMCurrentProvider)
	for _, provider := range cfg.MaclawLLMProviders {
		if strings.TrimSpace(provider.Name) == current {
			if token := provider.CodexSubscriptionOAuthToken(); token != "" {
				return token
			}
		}
	}
	if key := strings.TrimSpace(cfg.MaclawLLMKey); key != "" {
		return key
	}
	for _, provider := range cfg.MaclawLLMProviders {
		if strings.TrimSpace(provider.Name) == current {
			return strings.TrimSpace(provider.Key)
		}
	}
	return ""
}

func llmConfigUsesHubService(cfg corelib.AppConfig) bool {
	current := strings.TrimSpace(cfg.MaclawLLMCurrentProvider)
	if llmProviderNameIsOfficial(current) {
		return true
	}
	for _, provider := range cfg.MaclawLLMProviders {
		if strings.TrimSpace(provider.Name) != current {
			continue
		}
		return provider.IsHubService || llmProviderNameIsOfficial(provider.Name)
	}
	return false
}

func llmProviderNameIsOfficial(name string) bool {
	name = strings.TrimSpace(name)
	lower := strings.ToLower(name)
	return name == "MaClaw官方" || (strings.Contains(name, "MaClaw") && (strings.Contains(name, "官方") || strings.Contains(lower, "official")))
}

func llmProviderNeedsKeyFromAppConfig(cfg corelib.AppConfig) bool {
	provider := strings.TrimSpace(cfg.MaclawLLMCurrentProvider)
	if provider == "" {
		return llmURLUsuallyNeedsKey(cfg.MaclawLLMUrl)
	}
	for _, saved := range cfg.MaclawLLMProviders {
		if strings.TrimSpace(saved.Name) == provider {
			authType := strings.TrimSpace(saved.AuthType)
			if authType != "" {
				return !strings.EqualFold(authType, "none")
			}
			break
		}
	}
	switch provider {
	case "Ollama Local", "LM Studio Local":
		return false
	case "Custom":
		return llmURLUsuallyNeedsKey(cfg.MaclawLLMUrl)
	case "OpenAI API Key", "OpenAI (API Key)", "Anthropic",
		"Zhipu GLM Lobster", "Zhipu GLM Coding", "智谱 GLM (龙虾)", "智谱 GLM (Coding)",
		"MiniMax", "Kimi", "Xfyun Astron", "讯飞星辰":
		return true
	}
	return llmURLUsuallyNeedsKey(cfg.MaclawLLMUrl)
}

func llmURLUsuallyNeedsKey(rawURL string) bool {
	host := llmURLHost(rawURL)
	if host == "" {
		return false
	}
	host = strings.Trim(host, "[]")
	lower := strings.ToLower(host)
	if lower == "localhost" || lower == "host.docker.internal" || strings.HasSuffix(lower, ".local") {
		return false
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return true
	}
	return !(addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsUnspecified())
}

func llmURLHost(rawURL string) string {
	value := strings.TrimSpace(rawURL)
	if value == "" {
		return ""
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Host != "" {
		return parsed.Hostname()
	}
	if cut, _, ok := strings.Cut(value, "/"); ok {
		value = cut
	}
	if cut, _, ok := strings.Cut(value, "?"); ok {
		value = cut
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		return host
	}
	if strings.Count(value, ":") == 1 {
		if host, _, ok := strings.Cut(value, ":"); ok {
			return host
		}
	}
	return value
}

func llmUnconfiguredHint(hubServiceReady bool, lang string) string {
	cliName := llmTUIName()
	if lang == "en" {
		if hubServiceReady {
			return fmt.Sprintf("Hub service credentials are ready. Recommended: run %s redeem for MaClaw official service; or run %s llm setup for a local/custom LLM.", cliName, cliName)
		}
		return llmTUISetupHintForLang(lang)
	}
	if hubServiceReady {
		return fmt.Sprintf("Hub 服务凭据已可用。推荐运行 %s redeem 兑换 MaClaw 官方服务；也可运行 %s llm setup 选择本地/自定义 LLM。", cliName, cliName)
	}
	return llmTUISetupHintForLang(lang)
}

func llmMissingKeyHint(lang string) string {
	cliName := llmTUIName()
	if lang == "en" {
		return fmt.Sprintf("Selected provider needs an API key. Run %s llm setup to open the TUI key field, or run %s redeem for MaClaw official service.", cliName, cliName)
	}
	return fmt.Sprintf("当前服务商需要 API Key。运行 %s llm setup 打开 TUI 密钥配置；也可运行 %s redeem 兑换 MaClaw 官方服务。", cliName, cliName)
}

func llmNextAction(configured bool, hubServiceReady bool, mcpCount int, lang string) string {
	cliName := llmTUIName()
	if lang == "en" {
		if !configured {
			if hubServiceReady {
				return fmt.Sprintf("Run %s redeem to redeem or check MaClaw official service, or run %s status for the full readiness overview.", cliName, cliName)
			}
			return fmt.Sprintf("Run %s llm setup to open TUI LLM settings, or run %s status for the full readiness overview.", cliName, cliName)
		}
		if mcpCount == 0 {
			return fmt.Sprintf("Run %s llm test to verify the connection; optional: run %s mcp to add tool templates.", cliName, cliName)
		}
		return fmt.Sprintf("Run %s llm test to verify the connection, or run %s status for the full readiness overview.", cliName, cliName)
	}
	if !configured {
		if hubServiceReady {
			return fmt.Sprintf("运行 %s redeem 兑换或检查 MaClaw 官方服务；也可运行 %s status 查看整体状态。", cliName, cliName)
		}
		return fmt.Sprintf("运行 %s llm setup 打开 TUI LLM 设置；或运行 %s status 查看整体初始化状态。", cliName, cliName)
	}
	if mcpCount == 0 {
		return fmt.Sprintf("运行 %s llm test 验证连接；可选：运行 %s mcp 从模板添加工具能力。", cliName, cliName)
	}
	return fmt.Sprintf("运行 %s llm test 验证连接；或运行 %s status 查看整体初始化状态。", cliName, cliName)
}

func llmNextTUICommand(configured bool, hubServiceReady bool, mcpCount int) string {
	cliName := llmTUIName()
	if configured {
		if mcpCount == 0 {
			return cliName + " mcp"
		}
		return cliName + " status"
	}
	if hubServiceReady {
		return cliName + " redeem"
	}
	return cliName + " llm setup"
}

func llmTUIName() string {
	return strings.ToLower(brand.Current().DisplayName) + "-tui"
}

func llmTUISetupHintForLang(lang string) string {
	cliName := llmTUIName()
	if lang == "en" {
		return fmt.Sprintf("Run %s llm setup to open TUI LLM settings; scripted text wizard: %s llm setup cli.", cliName, cliName)
	}
	return fmt.Sprintf("运行 %s llm setup 打开 TUI LLM 设置；脚本/文字向导可用 %s llm setup cli。", cliName, cliName)
}

// ensureTUIoAuthToken 在 TUI 的 LLM 请求前检查并刷新 OAuth token。
func ensureTUIoAuthToken() error {
	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return nil // no config, nothing to refresh
	}
	if len(cfg.MaclawLLMProviders) == 0 {
		return nil
	}
	for i, p := range cfg.MaclawLLMProviders {
		if p.Name == cfg.MaclawLLMCurrentProvider && p.AuthType == "oauth" {
			oauthCfg := oauth.DefaultConfig()
			updated, err := oauth.EnsureValidToken(p, oauthCfg, func(up corelib.MaclawLLMProvider) error {
				cfg.MaclawLLMProviders[i] = up
				// Sync legacy fields
				cfg.MaclawLLMUrl = up.URL
				cfg.MaclawLLMKey = up.Key
				cfg.MaclawLLMModel = up.Model
				cfg.MaclawLLMProtocol = up.Protocol
				cfg.MaclawLLMContextLength = up.ContextLength
				cfg.MaclawLLMTimeoutSec = up.TimeoutSec
				return store.SaveConfig(cfg)
			})
			if err != nil {
				return err
			}
			cfg.MaclawLLMProviders[i] = updated
			break
		}
	}
	return nil
}

func llmTest(args []string) error {
	fs := flag.NewFlagSet("llm test", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if err := ensureTUIoAuthToken(); err != nil {
		return fmt.Errorf("OAuth token 刷新失败: %w", err)
	}

	llm, err := LoadLLMConfig()
	if err != nil {
		return fmt.Errorf("加载 LLM 配置失败: %w", err)
	}
	if strings.TrimSpace(llm.URL) == "" || strings.TrimSpace(llm.Model) == "" {
		return fmt.Errorf("LLM 未配置。%s", llmTUISetupHint())
	}

	Printf("测试 LLM: %s (%s)...\n", llm.Model, llm.URL)
	msgs := []interface{}{
		map[string]string{"role": "user", "content": "请回复 OK"},
	}
	client := &http.Client{Timeout: 30 * time.Second}
	start := time.Now()
	resp, err := agent.DoSimpleLLMRequest(llm, msgs, client, 30*time.Second)
	elapsed := time.Since(start)

	if err != nil {
		if *jsonOut {
			return PrintJSON(map[string]interface{}{"success": false, "error": err.Error(), "elapsed_ms": elapsed.Milliseconds()})
		}
		return fmt.Errorf("LLM 测试失败 (%v): %w", elapsed.Round(time.Millisecond), err)
	}
	if *jsonOut {
		return PrintJSON(map[string]interface{}{"success": true, "response": resp.Content, "elapsed_ms": elapsed.Milliseconds()})
	}
	Printf("成功 (%v)\n", elapsed.Round(time.Millisecond))
	Printf("  响应: %s\n", TruncateDisplay(resp.Content, 80))
	return nil
}

func llmPing(args []string) error {
	fs := flag.NewFlagSet("llm ping", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if err := ensureTUIoAuthToken(); err != nil {
		return fmt.Errorf("OAuth token 刷新失败: %w", err)
	}

	llm, err := LoadLLMConfig()
	if err != nil {
		return fmt.Errorf("加载 LLM 配置失败: %w", err)
	}
	if strings.TrimSpace(llm.URL) == "" {
		return fmt.Errorf("LLM URL 未配置。%s", llmTUISetupHint())
	}

	// 简单 HTTP GET 检测端点可达性
	client := &http.Client{Timeout: 10 * time.Second}
	endpoint := strings.TrimRight(llm.URL, "/") + "/models"
	if llm.Protocol == "anthropic" {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := llmclient.DoAnthropicRequestWithOptions(ctx, llm, []interface{}{
			map[string]interface{}{"role": "user", "content": "ping"},
		}, nil, client, llmclient.AnthropicMessagesRequestOptions{MaxTokens: 8})
		elapsed := time.Since(start)
		if err != nil {
			if *jsonOut {
				return PrintJSON(map[string]interface{}{"reachable": false, "error": err.Error(), "elapsed_ms": elapsed.Milliseconds()})
			}
			return fmt.Errorf("LLM 绔偣涓嶅彲杈?(%v): %w", elapsed.Round(time.Millisecond), err)
		}
		if *jsonOut {
			return PrintJSON(map[string]interface{}{"reachable": true, "status": http.StatusOK, "elapsed_ms": elapsed.Milliseconds()})
		}
		Printf("鉁?绔偣鍙揪 (HTTP %d, %v)\n", http.StatusOK, elapsed.Round(time.Millisecond))
		return nil
	}

	start := time.Now()
	req, _ := http.NewRequest(http.MethodGet, endpoint, nil)
	if llm.Key != "" {
		req.Header.Set("Authorization", "Bearer "+llm.Key)
	}
	resp, err := client.Do(req)
	elapsed := time.Since(start)

	if err != nil {
		if *jsonOut {
			return PrintJSON(map[string]interface{}{"reachable": false, "error": err.Error()})
		}
		return fmt.Errorf("LLM 端点不可达 (%v): %w", elapsed.Round(time.Millisecond), err)
	}
	resp.Body.Close()

	if *jsonOut {
		return PrintJSON(map[string]interface{}{"reachable": true, "status": resp.StatusCode, "elapsed_ms": elapsed.Milliseconds()})
	}
	Printf("端点可达 (HTTP %d, %v)\n", resp.StatusCode, elapsed.Round(time.Millisecond))
	return nil
}

func llmProviders(args []string) error {
	fs := flag.NewFlagSet("llm providers", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	if *jsonOut {
		return PrintJSON(map[string]interface{}{
			"providers": cfg.MaclawLLMProviders,
			"current":   cfg.MaclawLLMCurrentProvider,
		})
	}
	if len(cfg.MaclawLLMProviders) == 0 {
		Println("未配置 LLM 提供商。")
		Println("  " + llmTUISetupHint())
		if cfg.MaclawLLMUrl != "" {
			Printf("  当前直接配置: %s (%s)\n", cfg.MaclawLLMModel, cfg.MaclawLLMUrl)
		}
		return nil
	}
	Printf("%-20s %-10s %-30s %-16s %s\n", "NAME", "PROTOCOL", "URL", "AUTH", "MODEL")
	Println(strings.Repeat("-", 96))
	for _, p := range cfg.MaclawLLMProviders {
		marker := "  "
		if p.Name == cfg.MaclawLLMCurrentProvider {
			marker = "→ "
		}
		auth := "-"
		if p.AuthType == "oauth" {
			if p.Key == "" {
				auth = "未认证"
			} else if p.TokenExpiresAt > 0 && time.Now().Unix() >= p.TokenExpiresAt {
				auth = "已过期"
			} else {
				auth = "已认证"
			}
		}
		Printf("%s%-18s %-10s %-30s %-16s %s\n", marker, p.Name, orDefault(p.Protocol, "openai"), TruncateDisplay(p.URL, 30), auth, p.Model)
	}
	return nil
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func llmSetProvider(args []string) error {
	fs := flag.NewFlagSet("llm set-provider", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() == 0 {
		return NewUsageError("usage: llm set-provider <provider-name>")
	}
	name := fs.Arg(0)

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	// 查找匹配的 provider
	var found *corelib.MaclawLLMProvider
	for i := range cfg.MaclawLLMProviders {
		if cfg.MaclawLLMProviders[i].Name == name {
			found = &cfg.MaclawLLMProviders[i]
			break
		}
	}
	if found == nil {
		// 列出可用的
		var names []string
		for _, p := range cfg.MaclawLLMProviders {
			names = append(names, p.Name)
		}
		if len(names) == 0 {
			return fmt.Errorf("未配置任何 LLM 提供商。%s", llmTUISetupHint())
		}
		return fmt.Errorf("提供商 '%s' 不存在，可用: %s", name, strings.Join(names, ", "))
	}

	// 更新当前 provider 和 LLM 配置
	cfg.MaclawLLMCurrentProvider = name
	cfg.MaclawLLMUrl = found.URL
	cfg.MaclawLLMKey = found.Key
	cfg.MaclawLLMModel = found.Model
	cfg.MaclawLLMProtocol = found.Protocol
	cfg.MaclawLLMContextLength = found.ContextLength

	if err := store.SaveConfig(cfg); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}
	Printf("已切换到 LLM 提供商: %s (%s, %s)\n", name, found.Model, found.URL)
	return nil
}

func llmSetMaxIterations(args []string) error {
	fs := flag.NewFlagSet("llm set-max-iterations", flag.ExitOnError)
	value := fs.Int("value", 0, fmt.Sprintf("最大推理轮次（%d-%d）", config.MinAgentIterations, config.MaxAgentIterationsCap))
	fs.Parse(args)

	if *value <= 0 {
		return NewUsageError("usage: llm set-max-iterations --value <N> (%d-%d)", config.MinAgentIterations, config.MaxAgentIterationsCap)
	}
	// Use the single source of truth for value normalization.
	normalizedValue := config.EffectiveMaxIterations(*value)

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}
	cfg.MaclawAgentMaxIterations = normalizedValue
	if err := store.SaveConfig(cfg); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}
	Printf("Agent 最大推理轮次已设置为 %d\n", normalizedValue)
	return nil
}

func llmGetMaxIterations(args []string) error {
	fs := flag.NewFlagSet("llm get-max-iterations", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}
	value := config.EffectiveMaxIterations(cfg.MaclawAgentMaxIterations)
	if *jsonOut {
		return PrintJSON(map[string]int{"max_iterations": value})
	}
	Printf("Agent 最大推理轮次: %d\n", value)
	return nil
}

func llmLogin(args []string) error {
	if len(args) == 0 {
		if brand.Current().ID == "qianxin" {
			return NewUsageError("usage: llm login <openai|codegen>")
		}
		return NewUsageError("usage: llm login openai")
	}

	switch args[0] {
	case "openai":
		return llmLoginOpenAI(args[1:])
	case "codegen":
		if brand.Current().ID != "qianxin" {
			return fmt.Errorf("CodeGen SSO 仅在企业版（TigerClaw）中可用")
		}
		return llmLoginCodeGen(args[1:])
	default:
		return NewUsageError("usage: llm login <openai|codegen>")
	}
}

func llmLoginOpenAI(args []string) error {
	Println("╭──────────────────────────────────────────────────────╮")
	Println("│         OpenAI OAuth 登录（Codex 订阅）             │")
	Println("├──────────────────────────────────────────────────────┤")
	Println("│                                                      │")
	Println("│  1. 在任意浏览器中打开以下链接                       │")
	Println("│  2. 完成 OpenAI 登录授权                             │")
	Println("│  3. 浏览器会跳转到一个打不开的页面（正常）           │")
	Println("│  4. 复制浏览器地址栏中的完整 URL 粘贴到下方          │")
	Println("│                                                      │")
	Println("╰──────────────────────────────────────────────────────╯")
	Println()

	cfg := oauth.DefaultConfig()
	params, err := oauth.PrepareHeadlessOAuth(cfg)
	if err != nil {
		return fmt.Errorf("准备 OAuth 参数失败: %w", err)
	}

	Println("请在浏览器中打开:")
	Println()
	Println("  " + params.AuthURL)
	Println()
	Println("授权完成后，浏览器会跳转到 http://localhost:... 页面（无法打开是正常的）。")
	Println("请复制浏览器地址栏中的完整 URL，粘贴到下方：")
	Println()
	Print("回调 URL: ")

	var callbackURL string
	fmt.Scanln(&callbackURL)
	callbackURL = strings.TrimSpace(callbackURL)
	if callbackURL == "" {
		return fmt.Errorf("未输入回调 URL，登录取消")
	}

	Println("正在完成认证...")
	result, err := oauth.CompleteHeadlessOAuth(cfg, params, callbackURL)
	if err != nil {
		return fmt.Errorf("OAuth 认证失败: %w", err)
	}

	store := NewFileConfigStore(ResolveDataDir())
	appCfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	applyOpenAIOAuthResultToConfig(&appCfg, result)

	// Set OpenAI as current and sync legacy fields
	appCfg.MaclawLLMCurrentProvider = "OpenAI"
	for _, p := range appCfg.MaclawLLMProviders {
		if p.Name == "OpenAI" {
			appCfg.MaclawLLMUrl = p.URL
			appCfg.MaclawLLMKey = p.Key
			appCfg.MaclawLLMModel = p.Model
			appCfg.MaclawLLMProtocol = p.Protocol
			appCfg.MaclawLLMContextLength = p.ContextLength
			break
		}
	}

	if err := store.SaveConfig(appCfg); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}

	Println()
	Println("OpenAI OAuth 登录成功，已设为当前 LLM 提供商")
	return nil
}

func defaultOpenAIOAuthProvider() corelib.MaclawLLMProvider {
	return corelib.MaclawLLMProvider{
		Name:          "OpenAI",
		URL:           "https://chatgpt.com/backend-api/codex",
		Model:         oauth.CodexSubscriptionDefaultModel,
		AuthType:      "oauth",
		ContextLength: 110000,
		TimeoutSec:    corelib.DefaultLLMTimeoutSec,
		WireAPI:       "responses-ws",
	}
}

func applyOpenAIOAuthResultToConfig(appCfg *corelib.AppConfig, result *oauth.TokenResult) {
	defaultProvider := defaultOpenAIOAuthProvider()
	for i, p := range appCfg.MaclawLLMProviders {
		if p.Name == defaultProvider.Name && p.AuthType == defaultProvider.AuthType {
			p = oauth.ApplyTokenResult(p, result)
			p.URL = defaultProvider.URL
			p.Model = defaultProvider.Model
			p.ContextLength = defaultProvider.ContextLength
			p.TimeoutSec = defaultProvider.TimeoutSec
			p.WireAPI = defaultProvider.WireAPI
			appCfg.MaclawLLMProviders[i] = p
			return
		}
	}
	appCfg.MaclawLLMProviders = append([]corelib.MaclawLLMProvider{oauth.ApplyTokenResult(defaultProvider, result)}, appCfg.MaclawLLMProviders...)
}

// llmLoginCodeGen 实现无头环境下的 CodeGen SSO 登录。
// 显示 SSO URL 让用户在任意浏览器中打开，完成后粘贴 token。
func llmLoginCodeGen(args []string) error {
	params, err := oauth.PrepareHeadlessCodeGenOAuth()
	if err != nil {
		return fmt.Errorf("准备 CodeGen OAuth 参数失败: %w", err)
	}
	loginURL := params.AuthURL

	Println("╭──────────────────────────────────────────────────────╮")
	Println("│           CodeGen SSO 登录（无头模式）               │")
	Println("├──────────────────────────────────────────────────────┤")
	Println("│                                                      │")
	Println("│  1. 在任意浏览器中打开以下链接:                      │")
	Printf("│     %s\n", loginURL)
	Println("│                                                      │")
	Println("│  2. 完成扫码/登录后，浏览器会跳转到 localhost 页面   │")
	Println("│                                                      │")
	Println("│  3. 优先复制浏览器地址栏中的完整回调 URL             │")
	Println("│                                                      │")
	Println("│  4. 若页面仍直接显示 Token，也可直接粘贴 Token       │")
	Println("│                                                      │")
	Println("╰──────────────────────────────────────────────────────╯")
	Println()
	Print("请粘贴回调 URL 或 Token: ")

	var input string
	fmt.Scanln(&input)
	input = strings.TrimSpace(input)
	if input == "" {
		return fmt.Errorf("未输入回调 URL 或 Token，登录取消")
	}

	Println("正在完成 CodeGen 认证...")
	result, err := oauth.ResolveHeadlessCodeGenInput(input, params)
	if err != nil {
		return fmt.Errorf("CodeGen 认证失败: %w", err)
	}

	// 保存到配置
	store := NewFileConfigStore(ResolveDataDir())
	appCfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	// 更新或创建 CodeGen provider
	found := false
	for i, p := range appCfg.MaclawLLMProviders {
		if p.Name == "CodeGen" {
			appCfg.MaclawLLMProviders[i].Key = result.AccessToken
			appCfg.MaclawLLMProviders[i].URL = result.BaseURL
			appCfg.MaclawLLMProviders[i].Model = result.ModelID
			appCfg.MaclawLLMProviders[i].AuthType = "sso"
			appCfg.MaclawLLMProviders[i].Protocol = "openai"
			if result.ContextLength > 0 {
				appCfg.MaclawLLMProviders[i].ContextLength = result.ContextLength
			}
			found = true
			break
		}
	}
	if !found {
		p := corelib.MaclawLLMProvider{
			Name:          "CodeGen",
			URL:           result.BaseURL,
			Key:           result.AccessToken,
			Model:         result.ModelID,
			Protocol:      "openai",
			AuthType:      "sso",
			ContextLength: result.ContextLength,
		}
		appCfg.MaclawLLMProviders = append(appCfg.MaclawLLMProviders, p)
	}

	// 设为当前 provider 并同步 legacy 字段
	appCfg.MaclawLLMCurrentProvider = "CodeGen"
	appCfg.MaclawLLMUrl = result.BaseURL
	appCfg.MaclawLLMKey = result.AccessToken
	appCfg.MaclawLLMModel = result.ModelID
	appCfg.MaclawLLMProtocol = "openai"
	if result.ContextLength > 0 {
		appCfg.MaclawLLMContextLength = result.ContextLength
	}

	if err := store.SaveConfig(appCfg); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}

	Println()
	Println("CodeGen SSO 登录成功")
	if result.Email != "" {
		Printf("  用户: %s\n", result.Email)
	}
	Printf("  模型: %s\n", result.ModelID)
	Printf("  API:  %s\n", result.BaseURL)
	return nil
}

func llmUsage(args []string) error {
	fs := flag.NewFlagSet("llm usage", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if err := ensureTUIoAuthToken(); err != nil {
		return fmt.Errorf("OAuth token 刷新失败: %w", err)
	}

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	// Find the current OAuth provider
	var accessToken string
	for _, p := range cfg.MaclawLLMProviders {
		if p.Name == cfg.MaclawLLMCurrentProvider && p.AuthType == "oauth" {
			accessToken = p.Key
			break
		}
	}
	if accessToken == "" {
		return fmt.Errorf("当前 provider 不支持用量查询（非 OAuth 类型）")
	}

	info, err := oauth.QueryUsage(accessToken)
	if err != nil {
		return fmt.Errorf("查询用量失败: %w", err)
	}

	if *jsonOut {
		return PrintJSON(info)
	}

	Println("OpenAI 用量信息:")
	Printf("  总额度:   $%.2f\n", info.TotalGranted)
	Printf("  已使用:   $%.2f\n", info.TotalUsed)
	Printf("  剩余额度: $%.2f\n", info.TotalAvailable)
	return nil
}
