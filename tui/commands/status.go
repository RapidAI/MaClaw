package commands

import (
	"flag"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/brand"
	"github.com/RapidAI/CodeClaw/corelib/i18n"
)

type statusReport struct {
	SetupReady            bool   `json:"setup_ready"`
	HubReady              bool   `json:"hub_ready"`
	RemoteMachineReady    bool   `json:"remote_machine_ready"`
	RemoteActivationState string `json:"remote_activation_state"`
	OfficialServiceReady  bool   `json:"official_service_ready"`
	LLMReady              bool   `json:"llm_ready"`
	LLMNeedsKey           bool   `json:"llm_needs_key"`
	MCPCount              int    `json:"mcp_count"`
	HubCenterURL          string `json:"hubcenter_url"`
	HubURL                string `json:"hub_url,omitempty"`
	Email                 string `json:"email,omitempty"`
	NextAction            string `json:"next_action"`
	NextTUICommand        string `json:"next_tui_command"`
}

// RunStatus prints the same first-run readiness overview used by the TUI status page.
func RunStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON output")
	textOut := fs.Bool("text", false, "text output")
	fs.Parse(args)

	if !*jsonOut && !*textOut {
		return NewUsageError("usage: maclaw-tui status --text|--json")
	}

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	report := buildStatusReport(cfg)
	if *jsonOut {
		return PrintJSON(report)
	}
	printStatusReport(cfg, report)
	return nil
}

func buildStatusReport(cfg corelib.AppConfig) statusReport {
	llmEndpointSet := strings.TrimSpace(cfg.MaclawLLMUrl) != "" && strings.TrimSpace(cfg.MaclawLLMModel) != ""
	llmReady := llmConfiguredFromAppConfig(cfg)
	llmNeedsKey := llmMissingProviderKey(cfg)
	hubReady := strings.TrimSpace(cfg.RemoteHubURL) != "" && strings.TrimSpace(cfg.RemoteViewerToken) != ""
	officialReady := llmReady && statusUsesOfficialService(cfg)
	setupReady := cfg.OnboardingDone || hubReady || llmEndpointSet
	mcpCount := len(cfg.MCPServers) + len(cfg.LocalMCPServers)

	report := statusReport{
		SetupReady:            setupReady,
		HubReady:              hubReady,
		RemoteMachineReady:    remoteMachineActivationReady(cfg),
		RemoteActivationState: remoteActivationState(cfg),
		OfficialServiceReady:  officialReady,
		LLMReady:              llmReady,
		LLMNeedsKey:           llmNeedsKey,
		MCPCount:              mcpCount,
		HubCenterURL:          effectiveRemoteHubCenterURL(cfg),
		HubURL:                strings.TrimSpace(cfg.RemoteHubURL),
		Email:                 strings.TrimSpace(cfg.RemoteEmail),
	}
	report.NextTUICommand, report.NextAction = statusNextStep(cfg, report)
	return report
}

func statusUsesOfficialService(cfg corelib.AppConfig) bool {
	current := strings.TrimSpace(cfg.MaclawLLMCurrentProvider)
	for _, provider := range cfg.MaclawLLMProviders {
		if provider.Name == current {
			return provider.IsHubService
		}
	}
	return strings.Contains(current, "MaClaw") &&
		(strings.Contains(current, "官方") || strings.Contains(strings.ToLower(current), "official"))
}

func statusNextStep(cfg corelib.AppConfig, report statusReport) (string, string) {
	cliName := strings.ToLower(brand.Current().DisplayName) + "-tui"
	if i18n.NormalizeLang(cfg.Language) != "en" {
		switch {
		case !report.SetupReady:
			if strings.TrimSpace(cfg.RemoteEmail) != "" {
				return cliName + " setup", "打开 Setup；邮箱已保存，按 Enter 激活 Hub。Hub URL 会根据 HubCenter 自动选择。"
			}
			return cliName + " setup", "打开 Setup，输入邮箱，并通过 HubCenter 自动选择 Hub。"
		case report.HubReady && !report.OfficialServiceReady && !report.LLMReady:
			return cliName + " redeem", "兑换 MaClaw 官方服务，或在 LLM 设置里选择本地/自定义服务。"
		case report.LLMNeedsKey:
			return cliName + " llm setup", "当前 LLM 服务商需要密钥；打开 TUI 填写密钥、切换本地服务商，或去服务兑换。"
		case !report.LLMReady:
			return cliName + " llm setup", "从 TUI 预设中选择 LLM 服务商。"
		case report.MCPCount == 0:
			return cliName + " mcp", "可选：从模板添加 MCP 能力。"
		default:
			return cliName, "已就绪。打开完整 TUI 开始聊天或管理工具。"
		}
	}
	switch {
	case !report.SetupReady:
		if strings.TrimSpace(cfg.RemoteEmail) != "" {
			return cliName + " setup", "Open Setup; email is saved, press Enter to activate Hub. Hub URL is selected automatically from HubCenter."
		}
		return cliName + " setup", "Open Setup, enter email, and activate HubCenter-based Hub selection."
	case report.HubReady && !report.OfficialServiceReady && !report.LLMReady:
		return cliName + " redeem", "Redeem MaClaw official service, or choose a local/custom LLM provider."
	case report.LLMNeedsKey:
		return cliName + " llm setup", "The selected LLM provider needs a key; open the TUI key field, switch to a local provider, or redeem official service."
	case !report.LLMReady:
		return cliName + " llm setup", "Choose an LLM provider from TUI presets."
	case report.MCPCount == 0:
		return cliName + " mcp", "Optional: add MCP capabilities from templates."
	default:
		return cliName, "Ready. Open the full TUI to chat or manage tools."
	}
}

func printStatusReport(cfg corelib.AppConfig, report statusReport) {
	lang := i18n.NormalizeLang(cfg.Language)
	if lang == "en" {
		fmt.Println("MaClaw TUI status")
		fmt.Printf("  HubCenter: %s\n", report.HubCenterURL)
		if report.HubURL != "" {
			fmt.Printf("  Hub URL:   %s (display only)\n", report.HubURL)
		} else {
			fmt.Println("  Hub URL:   auto-selected during Setup")
		}
		fmt.Printf("  %s Setup\n", statusMark(report.SetupReady))
		fmt.Printf("  %s Hub activation\n", statusMark(report.HubReady))
		fmt.Printf("  %s Remote machine%s\n", statusMark(report.RemoteMachineReady), statusRemoteMachineSuffix(report, lang))
		fmt.Printf("  %s Official service\n", statusMark(report.OfficialServiceReady))
		fmt.Printf("  %s LLM%s\n", statusMark(report.LLMReady), statusLLMSuffix(report, lang))
		fmt.Printf("  %s MCP (%d configured)\n", statusMark(report.MCPCount > 0), report.MCPCount)
		fmt.Printf("Next: %s\n", report.NextAction)
		fmt.Printf("TUI:  %s\n", report.NextTUICommand)
		return
	}
	fmt.Println("MaClaw TUI 状态")
	fmt.Printf("  HubCenter: %s\n", report.HubCenterURL)
	if report.HubURL != "" {
		fmt.Printf("  Hub URL:   %s（仅展示）\n", report.HubURL)
	} else {
		fmt.Println("  Hub URL:   初始化时自动选择")
	}
	fmt.Printf("  %s 初始化\n", statusMark(report.SetupReady))
	fmt.Printf("  %s Hub 激活\n", statusMark(report.HubReady))
	fmt.Printf("  %s 远程机器%s\n", statusMark(report.RemoteMachineReady), statusRemoteMachineSuffix(report, lang))
	fmt.Printf("  %s 官方服务\n", statusMark(report.OfficialServiceReady))
	fmt.Printf("  %s LLM%s\n", statusMark(report.LLMReady), statusLLMSuffix(report, lang))
	fmt.Printf("  %s MCP（已配置 %d 个）\n", statusMark(report.MCPCount > 0), report.MCPCount)
	fmt.Printf("下一步: %s\n", report.NextAction)
	fmt.Printf("TUI:  %s\n", report.NextTUICommand)
}

func statusRemoteMachineSuffix(report statusReport, lang string) string {
	if report.RemoteMachineReady {
		return ""
	}
	if i18n.NormalizeLang(lang) != "en" {
		if report.HubReady {
			return "（仅远程任务需要）"
		}
		return "（Hub 激活后可用）"
	}
	if report.HubReady {
		return " (optional for remote tasks)"
	}
	return " (after Hub activation)"
}

func statusLLMSuffix(report statusReport, lang string) string {
	if !report.LLMNeedsKey {
		return ""
	}
	if i18n.NormalizeLang(lang) == "en" {
		return " (API key needed)"
	}
	return "（需要 API Key）"
}

func statusMark(ok bool) string {
	if ok {
		return "[x]"
	}
	return "[ ]"
}
