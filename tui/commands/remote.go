package commands

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/brand"
	"github.com/RapidAI/CodeClaw/corelib/i18n"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

// RunRemote 执行 remote 子命令。
func RunRemote(args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui remote <status|activate|set-hubcenter|set-email|deactivate>")
	}
	switch args[0] {
	case "status":
		return remoteStatus(args[1:])
	case "activate":
		return remoteActivate(args[1:])
	case "set-hubcenter":
		return remoteSetHubCenter(args[1:])
	case "set-hub":
		return remoteSetHub(args[1:])
	case "set-email":
		return remoteSetEmail(args[1:])
	case "deactivate":
		return remoteDeactivate(args[1:])
	default:
		return NewUsageError("unknown remote action: %s", args[0])
	}
}

func remoteStatus(args []string) error {
	fs := flag.NewFlagSet("remote status", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}
	lang := i18n.NormalizeLang(cfg.Language)

	info := map[string]interface{}{
		"enabled":                  cfg.RemoteEnabled,
		"activation_state":         remoteActivationState(cfg),
		"hub_url":                  cfg.RemoteHubURL,
		"hubcenter_url":            effectiveRemoteHubCenterURL(cfg),
		"email":                    cfg.RemoteEmail,
		"machine_id":               cfg.RemoteMachineID,
		"sn":                       cfg.RemoteSN,
		"user_id":                  cfg.RemoteUserID,
		"hub_service_ready":        remoteHubServiceReady(cfg),
		"viewer_token_ready":       cfg.RemoteViewerToken != "",
		"machine_token_ready":      strings.TrimSpace(cfg.RemoteMachineToken) != "",
		"machine_activation_ready": remoteMachineActivationReady(cfg),
		"next_action":              remoteNextAction(cfg, lang),
		"next_tui_command":         remoteNextTUICommand(cfg),
	}

	if *jsonOut {
		return PrintJSON(info)
	}

	printRemoteStatus(cfg, lang)
	return nil
}

func printRemoteStatus(cfg corelib.AppConfig, lang string) {
	if lang == "en" {
		status := "inactive"
		switch remoteActivationState(cfg) {
		case "active":
			status = "active"
		case "service_ready":
			status = "service credentials ready (remote machine not fully activated)"
		case "incomplete":
			status = "incomplete (run Setup again)"
		}

		Printf("Remote mode: %s\n", status)
		Printf("  Enabled:   %v\n", cfg.RemoteEnabled)
		Printf("  HubCenter: %s\n", effectiveRemoteHubCenterURL(cfg))
		Printf("  Hub URL:   %s\n", orDefault(cfg.RemoteHubURL, "(auto-selected during registration)"))
		Printf("  Email:     %s\n", orDefault(cfg.RemoteEmail, "(not set)"))
		Printf("  Machine ID:%s\n", remoteAlignedValue(cfg.RemoteMachineID, "(not activated)"))
		Printf("  SN:        %s\n", orDefault(cfg.RemoteSN, "(not activated)"))
		if cfg.RemoteMachineToken != "" {
			Printf("  Token:     %s****\n", cfg.RemoteMachineToken[:min(4, len(cfg.RemoteMachineToken))])
		}
		if cfg.RemoteViewerToken != "" {
			Printf("  Viewer:    %s****\n", cfg.RemoteViewerToken[:min(4, len(cfg.RemoteViewerToken))])
		} else if strings.TrimSpace(cfg.RemoteEmail) != "" {
			Println("  Viewer:    (missing; run maclaw-tui setup again)")
		}
		Printf("  Service credentials: %s\n", yesNoEN(remoteHubServiceReady(cfg)))
		Printf("  Machine activation:  %s\n", yesNoEN(remoteMachineActivationReady(cfg)))
		Printf("  Next: %s\n", remoteNextAction(cfg, lang))
		Printf("  TUI status: %s\n", remoteNextTUICommand(cfg))
		Println("  Note: Hub URL is display-only. Change HubCenter, then register again if the entrypoint changes.")
		return
	}

	status := "未激活"
	switch remoteActivationState(cfg) {
	case "active":
		status = "已激活"
	case "service_ready":
		status = "服务凭据可用（远程机器未完整激活）"
	case "incomplete":
		status = "未完成（请重新初始化）"
	}

	Printf("远程模式: %s\n", status)
	Printf("  启用:     %v\n", cfg.RemoteEnabled)
	Printf("  HubCenter: %s\n", effectiveRemoteHubCenterURL(cfg))
	Printf("  Hub URL:  %s\n", orDefault(cfg.RemoteHubURL, "(注册时自动选择，当前未设置)"))
	Printf("  邮箱:     %s\n", orDefault(cfg.RemoteEmail, "(未设置)"))
	Printf("  机器 ID:  %s\n", orDefault(cfg.RemoteMachineID, "(未激活)"))
	Printf("  SN:       %s\n", orDefault(cfg.RemoteSN, "(未激活)"))
	if cfg.RemoteMachineToken != "" {
		Printf("  Token:    %s****\n", cfg.RemoteMachineToken[:min(4, len(cfg.RemoteMachineToken))])
	}
	if cfg.RemoteViewerToken != "" {
		Printf("  Viewer:   %s****\n", cfg.RemoteViewerToken[:min(4, len(cfg.RemoteViewerToken))])
	} else if strings.TrimSpace(cfg.RemoteEmail) != "" {
		Println("  Viewer:   (缺失，请运行 maclaw-tui setup 重新初始化)")
	}
	Printf("  服务凭据:  %s\n", yesNoCN(remoteHubServiceReady(cfg)))
	Printf("  机器激活:  %s\n", yesNoCN(remoteMachineActivationReady(cfg)))
	Printf("  下一步: %s\n", remoteNextAction(cfg, lang))
	Printf("  TUI 状态总览: %s\n", remoteNextTUICommand(cfg))
	Println("  提示: Hub URL 仅展示；如需更换入口，请设置 HubCenter 后重新注册。")
}

func remoteNextAction(cfg corelib.AppConfig, lang string) string {
	cliName := remoteCLIName()
	if lang == "en" {
		switch remoteActivationState(cfg) {
		case "active", "service_ready":
			return fmt.Sprintf("Run %s redeem to check or redeem MaClaw official service, or run %s status for the full readiness overview.", cliName, cliName)
		case "incomplete":
			return fmt.Sprintf("Run %s setup to reactivate Hub credentials. Hub is selected automatically from HubCenter and email.", cliName)
		default:
			return fmt.Sprintf("Run %s setup, enter email, and choose HubCenter in the TUI.", cliName)
		}
	}
	switch remoteActivationState(cfg) {
	case "active", "service_ready":
		return fmt.Sprintf("运行 %s redeem 检查或兑换 MaClaw 官方服务；也可运行 %s status 查看整体状态。", cliName, cliName)
	case "incomplete":
		return fmt.Sprintf("运行 %s setup 重新激活 Hub 凭据；Hub 会根据 HubCenter 和邮箱自动选择。", cliName)
	default:
		return fmt.Sprintf("运行 %s setup，在 TUI 中输入邮箱并选择 HubCenter。", cliName)
	}
}

func remoteNextTUICommand(cfg corelib.AppConfig) string {
	cliName := remoteCLIName()
	switch remoteActivationState(cfg) {
	case "active", "service_ready":
		return cliName + " status"
	}
	return cliName + " setup"
}

func remoteCLIName() string {
	return strings.ToLower(brand.Current().DisplayName) + "-tui"
}

func remoteAlignedValue(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	return " " + value
}

// remoteActivate performs the full HubCenter discovery → Hub resolve → Enroll flow.
// This is the TUI equivalent of GUI's ActivateRemote.
func remoteActivate(args []string) error {
	fs := flag.NewFlagSet("remote activate", flag.ExitOnError)
	email := fs.String("email", "", "注册邮箱")
	invCode := fs.String("invitation-code", "", "邀请码（如需要）")
	fs.Parse(args)

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	// Use flag value, fall back to saved config.
	activateEmail := strings.TrimSpace(*email)
	if activateEmail == "" {
		activateEmail = strings.TrimSpace(cfg.RemoteEmail)
	}
	if activateEmail == "" {
		cliName := remoteCLIName()
		return fmt.Errorf("邮箱未配置。推荐运行 %s setup 在 TUI 中完成；脚本可使用 %s remote activate --email <email>", cliName, cliName)
	}
	if !validOnboardingEmailForCLI(activateEmail) {
		return fmt.Errorf("邮箱格式无效: %s。推荐运行 %s setup 在 TUI 中重新输入", activateEmail, remoteCLIName())
	}
	activateEmail = strings.ToLower(activateEmail)

	// Check if already activated.
	if remoteActivationComplete(cfg) {
		Printf("已激活 (machine_id=%s)。如需重新注册，请先运行: %s remote deactivate\n", cfg.RemoteMachineID, remoteCLIName())
		return nil
	}

	Printf("正在注册 %s ...\n", activateEmail)

	// Build machine profile with TUI version. Hub URL is intentionally not
	// supplied; HubCenter resolves the correct Hub from the registration email.
	profile := buildRemoteEnrollmentProfile(cfg, activateEmail, strings.TrimSpace(*invCode))

	client := remote.NewEnrollmentClient()
	result, err := client.Enroll(context.Background(), profile)
	if err != nil {
		return fmt.Errorf("注册失败: %w", err)
	}

	// Persist credentials to config.json (shared with GUI).
	cfg = applyRemoteEnrollResultToConfig(cfg, result)

	if err := store.SaveConfig(cfg); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}

	// Auto-acquire SkillMarket session token (non-fatal on failure).
	if result.ViewerToken != "" && cfg.SkillMarketSessionToken == "" {
		smClient := remote.NewSkillMarketAuthClient()
		smCtx, smCancel := context.WithTimeout(context.Background(), 15*time.Second)
		smBaseURL := ResolveHubCenterWithFailover(cfg, cfg.SkillMarketBaseURL(remote.DefaultRemoteHubCenterURL), nil, nil)
		smResult, smErr := smClient.MachineLogin(smCtx, smBaseURL, result.Email, result.MachineID, result.ViewerToken)
		smCancel()
		if smErr == nil && smResult.SessionToken != "" {
			cfg.SkillMarketSessionToken = smResult.SessionToken
			_ = store.SaveConfig(cfg)
			Println("  SkillMarket: 已自动登录")
		}
	}

	Println("注册成功")
	Printf("  Hub URL:    %s\n", result.HubURL)
	Printf("  Machine ID: %s\n", result.MachineID)
	Printf("  Email:      %s\n", result.Email)
	if result.SN != "" {
		Printf("  SN:         %s\n", result.SN)
	}
	Printf("  下一步: %s\n", remoteNextAction(cfg, i18n.NormalizeLang(cfg.Language)))
	Printf("  TUI 状态总览: %s\n", remoteNextTUICommand(cfg))
	return nil
}

func applyRemoteEnrollResultToConfig(cfg corelib.AppConfig, result *remote.EnrollResult) corelib.AppConfig {
	if result == nil {
		return cfg
	}
	if strings.TrimSpace(result.Email) != "" {
		cfg.RemoteEmail = strings.TrimSpace(result.Email)
	}
	cfg.RemoteSN = result.SN
	cfg.RemoteUserID = result.UserID
	cfg.RemoteMachineID = result.MachineID
	cfg.RemoteMachineToken = result.MachineToken
	cfg.RemoteHubURL = result.HubURL
	cfg.RemoteEnabled = true
	cfg.DefaultLaunchMode = "remote"
	if result.ViewerToken != "" {
		cfg.RemoteViewerToken = result.ViewerToken
	}
	if result.ClientID != "" && cfg.RemoteClientID == "" {
		cfg.RemoteClientID = result.ClientID
	}
	if result.HubCenterURL != "" || len(result.DiscoveredURLs) > 0 {
		preferred, discovered := sanitizeRemoteHubCenterURLs(result.HubCenterURL, result.DiscoveredURLs)
		if preferred != "" {
			cfg.RemoteHubCenterURL = preferred
		}
		if len(discovered) > 0 || len(result.DiscoveredURLs) > 0 {
			cfg.RemoteHubCenterURLs = discovered
		}
	}
	cfg.OnboardingDone = remoteActivationComplete(cfg)
	return cfg
}

func buildRemoteEnrollmentProfile(cfg corelib.AppConfig, email, invCode string) remote.EnrollConfig {
	profile := remote.BuildMachineProfile(resolveAppVersion())
	profile.Email = strings.TrimSpace(email)
	profile.InvitationCode = strings.TrimSpace(invCode)
	profile.ClientID = cfg.RemoteClientID
	profile.HubURL = ""
	profile.HubCenterURL = strings.TrimSpace(cfg.RemoteHubCenterURL)
	profile.HubCenterURLs = cfg.HubCenterBaseURLs(remote.DefaultRemoteHubCenterURL, remote.DefaultRemoteHubCenterURLs)
	return profile
}

// resolveAppVersion returns the TUI app version string.
func resolveAppVersion() string {
	// The version variable is set by the linker at build time in main.go.
	// We can't access it from the commands package, so return a generic value.
	// The actual version is injected by the caller if needed.
	return "tui"
}

func remoteSetHub(args []string) error {
	cliName := remoteCLIName()
	return NewUsageError("Hub URL 由 HubCenter 和注册邮箱自动选择，不再手动设置。请运行 %s remote set-hubcenter <url>，或直接运行 %s setup。", cliName, cliName)
}

func remoteSetHubCenter(args []string) error {
	fs := flag.NewFlagSet("remote set-hubcenter", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: remote set-hubcenter <hubcenter-url>")
	}
	hubCenterURL := strings.TrimRight(strings.TrimSpace(fs.Arg(0)), "/")
	if !validRemoteHubCenterURL(hubCenterURL) {
		return fmt.Errorf("HubCenter must be a valid http(s) URL: %s", fs.Arg(0))
	}
	if remote.IsLoopbackURL(hubCenterURL) {
		return fmt.Errorf("HubCenter must be a public address, not a loopback address: %s", fs.Arg(0))
	}

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	cfg.RemoteHubCenterURL = hubCenterURL
	if err := store.SaveConfig(cfg); err != nil {
		return err
	}
	Printf("HubCenter 已设为 %s。下次注册会根据邮箱自动选择 Hub。\n", hubCenterURL)
	Printf("下一步: 运行 %s setup 重新激活 Hub；Hub URL 会自动刷新。\n", remoteCLIName())
	return nil
}

func validRemoteHubCenterURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func sanitizeRemoteHubCenterURLs(preferred string, discovered []string) (string, []string) {
	candidates := append([]string{preferred}, discovered...)
	normalized := remote.NormalizeHubCenterURLs(candidates)
	public := make([]string, 0, len(normalized))
	for _, value := range normalized {
		if value == "" || remote.IsLoopbackURL(value) {
			continue
		}
		public = append(public, value)
	}
	if len(public) == 0 {
		return "", nil
	}
	return public[0], public
}

func remoteMachineActivationReady(cfg corelib.AppConfig) bool {
	return strings.TrimSpace(cfg.RemoteMachineID) != "" &&
		strings.TrimSpace(cfg.RemoteMachineToken) != ""
}

func remoteHubServiceReady(cfg corelib.AppConfig) bool {
	return strings.TrimSpace(cfg.RemoteHubURL) != "" &&
		strings.TrimSpace(cfg.RemoteViewerToken) != ""
}

func remoteActivationComplete(cfg corelib.AppConfig) bool {
	return remoteMachineActivationReady(cfg) && remoteHubServiceReady(cfg)
}

func remoteActivationIncomplete(cfg corelib.AppConfig) bool {
	if strings.TrimSpace(cfg.RemoteEmail) == "" {
		return false
	}
	return strings.TrimSpace(cfg.RemoteMachineID) == "" ||
		strings.TrimSpace(cfg.RemoteMachineToken) == "" ||
		strings.TrimSpace(cfg.RemoteViewerToken) == ""
}

func remoteActivationState(cfg corelib.AppConfig) string {
	if remoteActivationComplete(cfg) {
		return "active"
	}
	if remoteHubServiceReady(cfg) {
		return "service_ready"
	}
	if remoteActivationIncomplete(cfg) {
		return "incomplete"
	}
	return "inactive"
}

func yesNoCN(ok bool) string {
	if ok {
		return "是"
	}
	return "否"
}

func yesNoEN(ok bool) string {
	if ok {
		return "yes"
	}
	return "no"
}

func effectiveRemoteHubCenterURL(cfg corelib.AppConfig) string {
	if value := cfg.ConfiguredHubCenterBaseURL(); value != "" {
		return value
	}
	return "(auto-discover on activation)"
}

func remoteSetEmail(args []string) error {
	fs := flag.NewFlagSet("remote set-email", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: remote set-email <email>")
	}
	email := strings.ToLower(strings.TrimSpace(fs.Arg(0)))
	if !validOnboardingEmailForCLI(email) {
		return fmt.Errorf("邮箱格式无效: %s。请运行 %s setup 在 TUI 中重新输入", fs.Arg(0), remoteCLIName())
	}

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	cfg.RemoteEmail = email
	if err := store.SaveConfig(cfg); err != nil {
		return err
	}
	Printf("远程邮箱已设为: %s\n", email)
	Printf("下一步: 运行 %s setup 激活 Hub；Hub 会根据 HubCenter 和邮箱自动选择。\n", remoteCLIName())
	return nil
}

func remoteDeactivate(args []string) error {
	fs := flag.NewFlagSet("remote deactivate", flag.ExitOnError)
	fs.Parse(args)

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	if !remoteHasAnyCredential(cfg) {
		Println("远程模式未激活，无需取消。")
		return nil
	}

	cfg.RemoteMachineID = ""
	cfg.RemoteMachineToken = ""
	cfg.RemoteViewerToken = ""
	cfg.RemoteHubURL = ""
	cfg.RemoteEmail = ""
	cfg.RemoteSN = ""
	cfg.RemoteUserID = ""
	cfg.RemoteEnabled = false
	cfg.OnboardingDone = false
	if strings.EqualFold(strings.TrimSpace(cfg.DefaultLaunchMode), "remote") {
		cfg.DefaultLaunchMode = ""
	}

	if err := store.SaveConfig(cfg); err != nil {
		return err
	}
	Println("远程模式已取消激活。")
	return nil
}

func remoteHasAnyCredential(cfg corelib.AppConfig) bool {
	return strings.TrimSpace(cfg.RemoteMachineID) != "" ||
		strings.TrimSpace(cfg.RemoteMachineToken) != "" ||
		strings.TrimSpace(cfg.RemoteViewerToken) != "" ||
		strings.TrimSpace(cfg.RemoteHubURL) != "" ||
		strings.TrimSpace(cfg.RemoteEmail) != "" ||
		strings.TrimSpace(cfg.RemoteSN) != "" ||
		strings.TrimSpace(cfg.RemoteUserID) != ""
}
