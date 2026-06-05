package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/brand"
	"github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/tui/commands"
	"github.com/RapidAI/CodeClaw/tui/views"
)

var version = "dev"

func init() {
	// Security policy bridge functions for the commands package.
	// Standalone TUI honors the last Hub-pushed security snapshot persisted by GUI.
	commands.GossipGuardFn = func() error {
		cfg, err := commands.NewFileConfigStore(commands.ResolveDataDir()).LoadConfig()
		if err != nil || !cfg.HubSecurityCentralized || cfg.GossipEnabled {
			return nil
		}
		return fmt.Errorf("gossip is disabled by Hub security policy")
	}
	commands.SecurityReadOnlyFn = func() bool { return false }
	commands.ConfigSecurityReadOnlyFn = func() bool {
		cfg, err := commands.NewFileConfigStore(commands.ResolveDataDir()).LoadConfig()
		return err == nil && cfg.HubSecurityCentralized
	}
}

func main() {
	// Migrate ~/.maclaw/skills → ~/.maclaw/data/skills (one-time).
	skill.MigrateSkillsDir()

	// --- -p / --prompt flag: non-interactive single-prompt mode ---
	// Usage: maclaw-tui -p "your prompt here"
	// Runs the agent loop once, prints the result to stdout, and exits.
	// Supports piping: maclaw-tui -p "return JSON" | jq .version
	if promptText := parsePipePromptFlag(); promptText != "" {
		runPrompt(promptText)
		return
	}

	if len(os.Args) < 2 {
		// 默认启动 TUI 交互模式
		runTUI()
		return
	}

	switch os.Args[1] {
	case "tui", "ui":
		runTUICommand(os.Args[2:])
	case "daemon":
		runDaemon()
	case "session":
		runSessionCommand(os.Args[2:])
	case "config":
		runConfigCommand(os.Args[2:])
	case "settings":
		runSettingsCommand(os.Args[2:])
	case "status", "doctor", "health":
		if statusCommandUsesCLI(os.Args[2:]) {
			if err := commands.RunStatus(os.Args[2:]); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(exitCodeForError(err))
			}
			return
		}
		runTUIWithOptions(startupForStatus())
	case "chat":
		runTUI(views.TabChat)
	case "tools":
		runToolsPageCommand(os.Args[2:])
	case "tasks":
		runTasksPageCommand(os.Args[2:])
	case "project":
		runLocalCommand("project", os.Args[2:])
	case "template":
		runLocalCommand("template", os.Args[2:])
	case "memory":
		runLocalCommand("memory", os.Args[2:])
	case "knowledge":
		dataDir := commands.ResolveDataDir()
		if err := commands.RunKnowledge(os.Args[2:], dataDir); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(exitCodeForError(err))
		}
	case "schedule":
		runLocalCommand("schedule", os.Args[2:])
	case "audit":
		runLocalCommand("audit", os.Args[2:])
	case "policy":
		runLocalCommand("policy", os.Args[2:])
	case "tool", "skill", "skillhub", "skillmarket", "capabilitymarket":
		runToolsBackedCommand(os.Args[1], os.Args[2:])
	case "mcp":
		runMCPCommand(os.Args[2:])
	case "nlskill":
		runToolsBackedCommand(os.Args[1], os.Args[2:])
	case "remote":
		runRemoteCommand(os.Args[2:])
	case "onboarding":
		runOnboardingCommand(os.Args[2:])
	case "setup":
		runSetupCommand(os.Args[2:])
	case "redeem", "service":
		runServiceCommand(os.Args[2:])
	case "loop":
		if loopCommandOpensTUI(os.Args[2:]) {
			runTUI(views.TabTasks, views.TaskSubBackground)
			return
		}
		// loop 命令需要运行中的 BackgroundLoopManager，CLI 模式下不可用
		fmt.Fprintln(os.Stderr, "Error: loop 命令仅在 TUI 交互模式或 daemon 模式下可用")
		os.Exit(commands.ExitUsage)
	case "launch":
		runLaunchCommand(os.Args[2:])
	case "swarm":
		if err := commands.RunSwarm(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(exitCodeForError(err))
		}
	case "llm":
		runLLMCommand(os.Args[2:])
	case "system":
		if err := commands.RunSystem(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(exitCodeForError(err))
		}
	case "gossip":
		if err := commands.RunGossip(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(exitCodeForError(err))
		}
	case "--version", "-v":
		fmt.Printf("%s-tui %s\n", strings.ToLower(brand.Current().DisplayName), version)
	case "--help", "-h", "help":
		printUsage()
	default:
		// 检查 --no-tui 标志
		for _, arg := range os.Args[1:] {
			if arg == "--no-tui" {
				runBatch()
				return
			}
		}
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(commands.ExitUsage)
	}
}

func printUsage() {
	brandName := brand.Current().DisplayName
	cliName := strings.ToLower(brandName) + "-tui"

	// 构建 launch 命令的工具列表描述
	launchTools := "claude/codex/opencode/iflow/kilo"
	for _, t := range brand.Current().ExtraTools {
		launchTools += "/" + t.Name
	}

	fmt.Fprintf(os.Stderr, `Usage: %s [command] [flags]

Commands:
  快速开始:
    %s              打开完整 TUI（新机器会自动进入初始化）
    %s tui [页面]   显式打开完整 TUI；可加 setup/redeem/config/tools/tasks/mcp 直达
    %s setup [邮箱] 首次设置：邮箱 + HubCenter 自动选择 Hub（可加 llm/mcp/security/redeem 直达）
    %s onboarding   同上，打开完整 TUI 初始化页
    %s redeem [码]  服务兑换：使用兑换码启用 MaClaw 官方服务
    %s config       打开完整 TUI 设置页，优先选择，必要时再输入
    %s status       打开 TUI 状态总览（别名：doctor/health；--text/--json 可脚本输出）

  (default)     启动 TUI 交互界面（F1 初始化 / F5 服务兑换 / F6 设置；聊天内可用 /setup /redeem /config）
  daemon        以守护进程模式运行（无 UI，仅后台服务）
  session       会话管理（list/start/attach/kill）
  config        配置 TUI（无参数打开完整 TUI 设置；llm/security/proxy/im/advanced 直达设置页；setup 打开初始化；ui 打开独立设置页；get/set/export/import/schema）
  settings      打开完整 TUI 的设置页（可加 llm/security/proxy/im/advanced 直达）
  status        打开完整 TUI 的状态总览（doctor/health 同义；--text/--json 输出脚本状态）
  chat          打开完整 TUI 的聊天页
  tools         打开完整 TUI 的工具页（可加 skill/mcp 直达）
  tasks         打开完整 TUI 的任务页（可加 remote/background/schedule 直达）
  project       项目管理（create/list/delete/switch）
  template      会话模板管理（list/create/delete）
  memory        兼容脚本记忆命令（无参数打开聊天页；记忆在 TUI 后台自动维护）
  knowledge     知识库管理（import/list/search/status/delete/clear）
  schedule      定时任务管理（无参数打开 TUI 任务页；list/create/delete/pause/resume/trigger 为脚本命令）
  audit         兼容审计命令（无参数打开服务兑换；list 查看本地审计日志）
  policy        安全策略配置（无参数打开 TUI 安全设置；list 为脚本命令）
  tool          工具管理（无参数打开 TUI Tools；recommend/status 为脚本命令）
  skill         技能管理（无参数打开 TUI Skill；list/add/delete/backup/restore/import/export 为脚本命令）
  skillhub      SkillHub 市场（无参数打开 TUI Skill；search/install/rate/check-updates/update 为脚本命令）
  skillmarket   SkillMarket 商店（无参数打开 TUI Skill；search/submit/status/account 为脚本命令）
  nlskill       NL 技能管理（无参数打开 TUI Skill；list/add/remove/enable/disable/execute 为脚本命令）
  mcp           MCP 管理（无配置时打开模板选择，已有配置时查看列表；remote 进入远程模板；list/remove/health-check/tools/call-tool 为脚本命令）
  remote        远程模式管理（无参数打开 TUI 初始化；status 查看脚本状态；set-hubcenter/set-email/deactivate；Hub URL 注册后自动选择）
  onboarding    打开完整 TUI 的初始化页（脚本向导：onboarding cli）
  setup         打开完整 TUI 的初始化页（可跟邮箱预填；可加 llm/mcp/security/redeem 直达）
  redeem        打开完整 TUI 的服务兑换页（可跟兑换码预填；别名：service；可加 setup/llm 跳到相关页）
  loop          后台任务管理（无参数打开 TUI 后台任务页；list/stop/continue 仅 TUI/daemon 模式）
  launch        启动编程工具（%s）
  swarm         Swarm 多任务编排（create/status/cancel/resume/list）
  llm           LLM 管理（无参数/setup 打开 TUI LLM 设置；setup cli 文字向导；test/ping/providers/status/set-provider 为脚本命令）
  system        系统信息（info/python-envs/python-status/python-ensure）
  gossip        八卦社区（browse/publish/comment/rate/comments）

Flags:
  -p "prompt"   非交互模式：执行单次 prompt 后退出（支持管道：-p "..." | jq .）
  --prompt      同 -p
  --no-tui      批处理模式（无交互 UI）
  --version     显示版本号
  --help        显示帮助信息
`, cliName, cliName, cliName, cliName, cliName, cliName, cliName, cliName, launchTools)
}

// buildKernelOptions 从环境变量和命令行参数构建 KernelOptions。
func buildKernelOptions(logger corelib.Logger, emitter corelib.EventEmitter) corelib.KernelOptions {
	dataDir := os.Getenv("MACLAW_DATA_DIR")
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		dataDir = home + "/.maclaw"
	}

	return corelib.KernelOptions{
		DataDir:      dataDir,
		HubURL:       os.Getenv("MACLAW_HUB_URL"),
		HubToken:     os.Getenv("MACLAW_TOKEN"),
		MachineID:    os.Getenv("MACLAW_MACHINE_ID"),
		Logger:       logger,
		EventEmitter: emitter,
	}
}

// runLocalCommand 处理本地数据子命令（template/memory/schedule/audit）。
func runLocalCommand(cmd string, args []string) {
	if tab, subTab, ok := localCommandInitialTab(cmd, args); ok {
		if subTab >= 0 {
			runTUI(tab, subTab)
		} else {
			runTUI(tab)
		}
		return
	}
	dataDir := commands.ResolveDataDir()
	var err error
	switch cmd {
	case "project":
		err = commands.RunProject(args, dataDir)
	case "template":
		err = commands.RunTemplate(args, dataDir)
	case "memory":
		err = commands.RunMemory(args, dataDir)
	case "schedule":
		err = commands.RunSchedule(args, dataDir)
	case "audit":
		err = commands.RunAudit(args, dataDir)
	case "policy":
		err = commands.RunPolicy(args, dataDir)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(exitCodeForError(err))
	}
}

func runTUICommand(args []string) {
	startup, ok := tuiCommandStartup(args)
	if !ok {
		runTUI()
		return
	}
	runTUIWithOptions(startup)
}

func tuiCommandStartup(args []string) (tuiStartupOptions, bool) {
	if len(args) == 0 {
		return tuiStartupOptions{}, true
	}
	first := strings.ToLower(strings.TrimSpace(args[0]))
	switch first {
	case "setup", "onboarding":
		return setupCommandStartup(args[1:]), true
	case "redeem", "service":
		return serviceCommandStartup(args[1:]), true
	case "chat":
		return startupForTab(views.TabChat, -1), true
	case "tools", "tool":
		return toolsPageStartup(args[1:]), true
	case "tasks", "task":
		return tasksPageStartup(args[1:]), true
	case "config", "settings":
		return configPageStartup(args[1:]), true
	case "status", "doctor", "health":
		return startupForStatus(), true
	case "mcp":
		startup := startupForTab(views.TabTools, views.ToolSubMCP)
		startup.mcpAddMode = mcpDefaultAddModeFromArgs(args[1:])
		return startup, true
	case "skill", "skills", "skillhub", "skillmarket", "nlskill":
		return startupForTab(views.TabTools, views.ToolSubSkill), true
	case "llm", "model", "models":
		return startupForTab(views.TabConfig, views.CfgTabLLM), true
	case "security", "policy", "sandbox":
		return startupForTab(views.TabConfig, views.CfgTabSecurity), true
	case "proxy", "network":
		return startupForTab(views.TabConfig, views.CfgTabProxy), true
	case "im", "messaging", "message":
		return startupForTab(views.TabConfig, views.CfgTabIM), true
	case "advanced", "advance":
		return startupForTab(views.TabConfig, views.CfgTabAdvanced), true
	case "general", "basic":
		return startupForTab(views.TabConfig, views.CfgTabGeneral), true
	case "schedule", "scheduled", "schedules", "cron":
		return startupForTab(views.TabTasks, views.TaskSubScheduled), true
	case "background", "bg", "loop":
		return startupForTab(views.TabTasks, views.TaskSubBackground), true
	case "remote", "hub", "account", "email", "wechat", "weixin":
		return setupCommandStartup(args), true
	}
	if email, ok := setupEmailFromArgs(args); ok {
		startup := startupForTab(views.TabOnboarding, -1)
		startup.onboardingEmail = email
		return startup, true
	}
	if tab, subTab, ok := setupInitialRoute(args); ok {
		startup := startupForTab(tab, subTab)
		if tab == views.TabTools && subTab == views.ToolSubMCP {
			startup.mcpAddMode = mcpDefaultAddModeFromArgs(args)
		}
		return startup, true
	}
	return tuiStartupOptions{}, false
}

func startupForTab(tab, subTab int) tuiStartupOptions {
	startup := tuiStartupOptions{forceInitialTab: []int{tab}}
	if subTab >= 0 {
		startup.forceInitialTab = append(startup.forceInitialTab, subTab)
	}
	return startup
}

func startupForStatus() tuiStartupOptions {
	startup := startupForTab(views.TabConfig, views.CfgTabGeneral)
	startup.focusSetupStatus = true
	return startup
}

func statusCommandUsesCLI(args []string) bool {
	for _, arg := range args {
		switch strings.ToLower(strings.TrimSpace(arg)) {
		case "--text", "-text", "--json", "-json":
			return true
		}
	}
	return false
}

func localCommandInitialTab(cmd string, args []string) (int, int, bool) {
	if !localCommandArgsOpenTUI(cmd, args) {
		return 0, -1, false
	}
	switch cmd {
	case "memory":
		return views.TabChat, -1, true
	case "schedule":
		return views.TabTasks, views.TaskSubScheduled, true
	case "audit":
		return views.TabServiceRedeem, -1, true
	case "policy":
		return views.TabConfig, views.CfgTabSecurity, true
	default:
		return 0, -1, false
	}
}

func localCommandArgsOpenTUI(cmd string, args []string) bool {
	if len(args) == 0 {
		return true
	}
	first := strings.ToLower(strings.TrimSpace(args[0]))
	switch cmd {
	case "memory":
		switch first {
		case "tui", "setup", "--tui", "chat":
			return true
		}
	case "schedule":
		switch first {
		case "tui", "setup", "--tui", "tasks", "task", "schedule", "scheduled", "cron":
			return true
		}
	case "audit":
		switch first {
		case "tui", "setup", "--tui", "redeem", "service":
			return true
		}
	case "policy":
		switch first {
		case "tui", "setup", "--tui", "security", "sandbox", "policy":
			return true
		}
	}
	return false
}

// runDaemon 以守护进程模式运行内核（无 UI）。
// 支持 --pid-file 和 --log-file 参数。
func runDaemon() {
	daemonFlags := flag.NewFlagSet("daemon", flag.ExitOnError)
	pidFile := daemonFlags.String("pid-file", "", "PID 文件路径")
	logFile := daemonFlags.String("log-file", "", "日志文件路径（默认 stderr）")
	daemonFlags.Parse(os.Args[2:])

	logger := NewTUILogger()
	logger.allowStderr = true // daemon 模式无 alt-screen，可安全写 stderr
	if *logFile != "" {
		if err := logger.SetLogFile(*logFile); err != nil {
			fmt.Fprintf(os.Stderr, "failed to open log file: %v\n", err)
			os.Exit(1)
		}
		defer logger.Close()
	}

	logger.Info("%s-tui daemon starting (version %s)", strings.ToLower(brand.Current().DisplayName), version)

	// 写 PID 文件
	if *pidFile != "" {
		pid := fmt.Sprintf("%d", os.Getpid())
		if err := os.WriteFile(*pidFile, []byte(pid), 0644); err != nil {
			logger.Error("failed to write PID file: %v", err)
			os.Exit(1)
		}
		defer os.Remove(*pidFile)
		logger.Info("PID %s written to %s", pid, *pidFile)
	}

	opts := buildKernelOptions(logger, nil)
	kernel, err := corelib.NewKernel(opts)
	if err != nil {
		logger.Error("kernel init failed: %v", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := kernel.Run(ctx); err != nil {
		logger.Error("kernel run error: %v", err)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5e9) // 5s
	defer shutdownCancel()
	_ = kernel.Shutdown(shutdownCtx)

	logger.Info("%s-tui daemon stopped", strings.ToLower(brand.Current().DisplayName))
}

// runBatch 批处理模式（--no-tui），执行一次性操作后退出。
func runBatch() {
	fmt.Fprintln(os.Stderr, "batch mode: not yet implemented")
	os.Exit(1)
}

// runSessionCommand 处理 session 子命令。
func runSessionCommand(args []string) {
	hubURL, token := resolveHubCredentials()
	if err := commands.RunSession(args, hubURL, token); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(exitCodeForError(err))
	}
}

type configCommandMode int

const (
	configCommandOpenSettings configCommandMode = iota
	configCommandOpenSetup
	configCommandOpenStandaloneUI
	configCommandRunCLI
)

func configTabFromArg(arg string) (int, bool) {
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "", "general", "basic":
		return views.CfgTabGeneral, true
	case "llm", "model", "models":
		return views.CfgTabLLM, true
	case "im", "messaging", "message":
		return views.CfgTabIM, true
	case "proxy", "network":
		return views.CfgTabProxy, true
	case "security", "policy", "sandbox":
		return views.CfgTabSecurity, true
	case "advanced", "advance":
		return views.CfgTabAdvanced, true
	default:
		return 0, false
	}
}

func configCommandInitialTab(args []string) (int, bool) {
	if len(args) == 0 {
		return views.CfgTabGeneral, true
	}
	return configTabFromArg(args[0])
}

func classifyConfigCommand(args []string) configCommandMode {
	if _, ok := configCommandInitialTab(args); ok {
		return configCommandOpenSettings
	}
	switch args[0] {
	case "setup":
		return configCommandOpenSetup
	case "ui":
		return configCommandOpenStandaloneUI
	default:
		return configCommandRunCLI
	}
}

// runConfigCommand 处理 config 子命令。
func runConfigCommand(args []string) {
	switch classifyConfigCommand(args) {
	case configCommandOpenSettings:
		if tab, ok := configCommandInitialTab(args); ok {
			runTUI(views.TabConfig, tab)
		} else {
			runTUI(views.TabConfig)
		}
		return
	case configCommandOpenSetup:
		if email, ok := configSetupEmailFromArgs(args); ok {
			runTUIWithOptions(tuiStartupOptions{forceInitialTab: []int{views.TabOnboarding}, onboardingEmail: email})
			return
		}
		runTUI(views.TabOnboarding)
		return
	case configCommandOpenStandaloneUI:
		runConfigUI()
		return
	}
	if !configCLIActionKnown(args) {
		fmt.Fprintf(os.Stderr, "Error: unknown config action: %s\n", args[0])
		fmt.Fprintln(os.Stderr, "Run maclaw-tui config to open TUI settings, or maclaw-tui config get --local for scripted reads.")
		os.Exit(commands.ExitUsage)
	}
	if configUsesLocalStore(args) {
		if err := commands.RunConfig(args, "", ""); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(exitCodeForError(err))
		}
		return
	}
	if configNeedsHubCredentials(args) && strings.TrimSpace(os.Getenv("MACLAW_TOKEN")) == "" {
		fmt.Fprintln(os.Stderr, "Error: MACLAW_TOKEN is required for remote config get/set.")
		fmt.Fprintln(os.Stderr, "Run maclaw-tui config to edit settings in the TUI, or add --local for local scripted config.")
		os.Exit(commands.ExitUsage)
	}
	hubURL, token := resolveHubCredentials()
	if err := commands.RunConfig(args, hubURL, token); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(exitCodeForError(err))
	}
}

func configUsesLocalStore(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "export", "import", "schema":
		return true
	case "get", "set":
		for _, arg := range args[1:] {
			switch strings.ToLower(strings.TrimSpace(arg)) {
			case "--local", "-local":
				return true
			}
		}
	}
	return false
}

func configNeedsHubCredentials(args []string) bool {
	if len(args) == 0 {
		return false
	}
	action := strings.ToLower(strings.TrimSpace(args[0]))
	if action != "get" && action != "set" {
		return false
	}
	for _, arg := range args[1:] {
		switch strings.ToLower(strings.TrimSpace(arg)) {
		case "--local", "-local":
			return false
		}
	}
	return true
}

func configCLIActionKnown(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "get", "set", "export", "import", "schema":
		return true
	default:
		return false
	}
}

func configSetupEmailFromArgs(args []string) (string, bool) {
	if len(args) < 1 || strings.ToLower(strings.TrimSpace(args[0])) != "setup" {
		return "", false
	}
	return setupEmailFromArgs(args[1:])
}

func runSettingsCommand(args []string) {
	if tab, ok := configCommandInitialTab(args); ok {
		runTUI(views.TabConfig, tab)
		return
	}
	runTUI(views.TabConfig)
}

func configPageStartup(args []string) tuiStartupOptions {
	if tab, ok := configCommandInitialTab(args); ok {
		return startupForTab(views.TabConfig, tab)
	}
	return startupForTab(views.TabConfig, -1)
}

func toolsPageInitialSubTab(args []string) (int, bool) {
	if len(args) == 0 {
		return views.ToolSubSkill, true
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "skill", "skills", "skillhub", "skillmarket", "nlskill", "tool":
		return views.ToolSubSkill, true
	case "mcp":
		return views.ToolSubMCP, true
	default:
		return 0, false
	}
}

func toolsPageStartup(args []string) tuiStartupOptions {
	if subTab, ok := toolsPageInitialSubTab(args); ok {
		startup := startupForTab(views.TabTools, subTab)
		if subTab == views.ToolSubMCP {
			startup.mcpAddMode = mcpDefaultAddModeFromArgs(args)
		}
		return startup
	}
	return startupForTab(views.TabTools, -1)
}

func runToolsPageCommand(args []string) {
	startup := toolsPageStartup(args)
	if startup.mcpAddMode != "" {
		runTUIWithOptions(startup)
		return
	}
	runTUI(startup.forceInitialTab...)
}

func tasksPageInitialSubTab(args []string) (int, bool) {
	if len(args) == 0 {
		return views.TaskSubRemote, true
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "remote", "ssh":
		return views.TaskSubRemote, true
	case "background", "bg", "loop":
		return views.TaskSubBackground, true
	case "schedule", "scheduled", "schedules", "cron":
		return views.TaskSubScheduled, true
	default:
		return 0, false
	}
}

func tasksPageStartup(args []string) tuiStartupOptions {
	if subTab, ok := tasksPageInitialSubTab(args); ok {
		return startupForTab(views.TabTasks, subTab)
	}
	return startupForTab(views.TabTasks, -1)
}

func runTasksPageCommand(args []string) {
	startup := tasksPageStartup(args)
	runTUI(startup.forceInitialTab...)
}

func loopCommandOpensTUI(args []string) bool {
	if len(args) == 0 {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "tui", "tasks", "background", "bg":
		return true
	default:
		return false
	}
}

func setupInitialRoute(args []string) (int, int, bool) {
	if len(args) == 0 {
		return views.TabOnboarding, -1, true
	}
	target := strings.ToLower(strings.TrimSpace(args[0]))
	switch target {
	case "onboarding", "hub", "remote", "account", "email", "wechat", "weixin":
		return views.TabOnboarding, -1, true
	case "redeem", "service":
		return views.TabServiceRedeem, -1, true
	case "llm", "model", "models":
		return views.TabConfig, views.CfgTabLLM, true
	case "security", "policy", "sandbox":
		return views.TabConfig, views.CfgTabSecurity, true
	case "proxy", "network":
		return views.TabConfig, views.CfgTabProxy, true
	case "im", "messaging", "message":
		return views.TabConfig, views.CfgTabIM, true
	case "advanced", "advance":
		return views.TabConfig, views.CfgTabAdvanced, true
	case "general", "basic", "config", "settings":
		return views.TabConfig, views.CfgTabGeneral, true
	case "mcp":
		return views.TabTools, views.ToolSubMCP, true
	case "skill", "skills", "tool", "tools":
		return views.TabTools, views.ToolSubSkill, true
	case "task", "tasks", "schedule", "scheduled", "cron":
		return views.TabTasks, views.TaskSubScheduled, true
	case "chat":
		return views.TabChat, -1, true
	default:
		return views.TabOnboarding, -1, false
	}
}

func runSetupCommand(args []string) {
	runTUIWithOptions(setupCommandStartup(args))
}

func setupCommandStartup(args []string) tuiStartupOptions {
	tab, subTab, ok := setupInitialRoute(args)
	if !ok {
		tab, subTab = views.TabOnboarding, -1
	}
	startup := startupForTab(tab, subTab)
	if email, ok := setupEmailFromArgs(args); ok {
		startup = startupForTab(views.TabOnboarding, -1)
		startup.onboardingEmail = email
		return startup
	}
	if tab == views.TabTools && subTab == views.ToolSubMCP {
		if mode := mcpDefaultAddModeFromArgs(args); mode != "" {
			startup.mcpAddMode = mode
		}
	}
	return startup
}

func setupEmailFromArgs(args []string) (string, bool) {
	if email, ok := onboardingEmailFromArgs(args); ok {
		return email, true
	}
	if len(args) == 2 && setupEmailRoutePrefix(args[0]) {
		return onboardingEmailFromArgs(args[1:])
	}
	if len(args) == 3 && setupEmailRoutePrefix(args[0]) && setupEmailFlag(args[1]) {
		return onboardingEmailFromArgs(args[2:])
	}
	return "", false
}

func setupEmailRoutePrefix(arg string) bool {
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "setup", "tui", "--tui", "onboarding", "hub", "remote", "account", "email", "--email", "-email":
		return true
	default:
		return false
	}
}

func setupEmailFlag(arg string) bool {
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "email", "--email", "-email":
		return true
	default:
		return false
	}
}

func onboardingEmailFromArgs(args []string) (string, bool) {
	if len(args) != 1 {
		return "", false
	}
	value := strings.TrimSpace(args[0])
	if !strings.Contains(value, "@") {
		return "", false
	}
	email := strings.ToLower(value)
	if at := strings.Index(email, "@"); at <= 0 || at >= len(email)-1 || !strings.Contains(email[at+1:], ".") {
		return "", false
	}
	return email, true
}

func serviceInitialRoute(args []string) (int, int) {
	if len(args) == 0 {
		return views.TabServiceRedeem, -1
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "setup", "onboarding", "hub", "remote", "account":
		return views.TabOnboarding, -1
	case "llm", "model", "models", "config", "settings":
		return views.TabConfig, views.CfgTabLLM
	default:
		return views.TabServiceRedeem, -1
	}
}

func serviceRedeemCodeFromArgs(args []string) (string, bool) {
	if len(args) == 0 {
		return "", false
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "setup", "onboarding", "hub", "remote", "account", "llm", "model", "models", "config", "settings", "status", "refresh", "check":
		return "", false
	case "code", "--code", "-code", "redeem", "service-code", "redeem-code":
		if len(args) == 1 {
			return "", false
		}
		code := views.NormalizeServiceRedeemCodeForInput(strings.Join(args[1:], ""))
		return code, code != ""
	default:
		code := views.NormalizeServiceRedeemCodeForInput(strings.Join(args, ""))
		return code, code != ""
	}
}

func runServiceCommand(args []string) {
	runTUIWithOptions(serviceCommandStartup(args))
}

func serviceCommandStartup(args []string) tuiStartupOptions {
	tab, subTab := serviceInitialRoute(args)
	startup := startupForTab(tab, subTab)
	if email, ok := serviceSetupEmailFromArgs(args); ok {
		startup = startupForTab(views.TabOnboarding, -1)
		startup.onboardingEmail = email
		return startup
	}
	if code, ok := serviceRedeemCodeFromArgs(args); ok {
		startup = startupForTab(views.TabServiceRedeem, -1)
		startup.serviceRedeemCode = code
		return startup
	}
	return startup
}

func serviceSetupEmailFromArgs(args []string) (string, bool) {
	if len(args) == 2 {
		switch strings.ToLower(strings.TrimSpace(args[0])) {
		case "setup", "onboarding", "hub", "remote", "account", "email":
			return onboardingEmailFromArgs(args[1:])
		}
	}
	if len(args) == 3 {
		switch strings.ToLower(strings.TrimSpace(args[0])) {
		case "setup", "onboarding", "hub", "remote", "account", "email":
			if setupEmailFlag(args[1]) {
				return onboardingEmailFromArgs(args[2:])
			}
		}
	}
	return "", false
}

type llmCommandMode int

const (
	llmCommandOpenConfig llmCommandMode = iota
	llmCommandRunCLI
)

func classifyLLMCommand(args []string) llmCommandMode {
	if len(args) == 0 {
		return llmCommandOpenConfig
	}
	if args[0] != "setup" {
		return llmCommandRunCLI
	}
	if len(args) == 1 {
		return llmCommandOpenConfig
	}
	switch args[1] {
	case "tui", "--tui":
		return llmCommandOpenConfig
	case "cli", "--cli", "--no-tui":
		return llmCommandRunCLI
	default:
		return llmCommandRunCLI
	}
}

func runLLMCommand(args []string) {
	switch classifyLLMCommand(args) {
	case llmCommandOpenConfig:
		runTUI(views.TabConfig, views.CfgTabLLM)
		return
	}
	if err := commands.RunLLM(llmCLIArgs(args)); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(exitCodeForError(err))
	}
}

type remoteCommandMode int

const (
	remoteCommandOpenSetup remoteCommandMode = iota
	remoteCommandRunCLI
)

func classifyRemoteCommand(args []string) remoteCommandMode {
	if len(args) == 0 {
		return remoteCommandOpenSetup
	}
	if _, ok := remoteSetupEmailFromArgs(args); ok {
		return remoteCommandOpenSetup
	}
	switch args[0] {
	case "setup", "tui", "--tui":
		return remoteCommandOpenSetup
	default:
		return remoteCommandRunCLI
	}
}

func runRemoteCommand(args []string) {
	if email, ok := remoteSetupEmailFromArgs(args); ok {
		runTUIWithOptions(tuiStartupOptions{forceInitialTab: []int{views.TabOnboarding}, onboardingEmail: email})
		return
	}
	switch classifyRemoteCommand(args) {
	case remoteCommandOpenSetup:
		runTUI(views.TabOnboarding)
		return
	}
	if err := enforceScriptedCommandSecurity("remote", args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(exitCodeForError(err))
	}
	if err := commands.RunRemote(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(exitCodeForError(err))
	}
}

func remoteSetupEmailFromArgs(args []string) (string, bool) {
	if email, ok := onboardingEmailFromArgs(args); ok {
		return email, true
	}
	if len(args) == 2 {
		switch strings.ToLower(strings.TrimSpace(args[0])) {
		case "setup", "tui", "--tui", "hub", "account", "email":
			return onboardingEmailFromArgs(args[1:])
		}
	}
	if len(args) == 3 {
		switch strings.ToLower(strings.TrimSpace(args[0])) {
		case "setup", "tui", "--tui", "hub", "account", "email":
			if setupEmailFlag(args[1]) {
				return onboardingEmailFromArgs(args[2:])
			}
		}
	}
	return "", false
}

type toolsBackedCommandMode int

const (
	toolsBackedCommandOpenTools toolsBackedCommandMode = iota
	toolsBackedCommandRunCLI
)

func classifyToolsBackedCommand(args []string) toolsBackedCommandMode {
	if len(args) == 0 {
		return toolsBackedCommandOpenTools
	}
	switch args[0] {
	case "tui", "setup", "--tui":
		return toolsBackedCommandOpenTools
	default:
		return toolsBackedCommandRunCLI
	}
}

func runToolsBackedCommand(name string, args []string) {
	switch classifyToolsBackedCommand(args) {
	case toolsBackedCommandOpenTools:
		runTUI(views.TabTools)
		return
	}
	if err := enforceScriptedCommandSecurity(name, args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(exitCodeForError(err))
	}
	var err error
	switch name {
	case "tool":
		err = commands.RunTool(args)
	case "skill":
		err = commands.RunSkill(args)
	case "skillhub":
		err = commands.RunSkillHub(args)
	case "skillmarket", "capabilitymarket":
		err = commands.RunCapabilityMarket(args)
	case "nlskill":
		err = commands.RunNLSkill(args)
	default:
		err = commands.NewUsageError("unknown tools-backed command: %s", name)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(exitCodeForError(err))
	}
}

type mcpCommandMode int

const (
	mcpCommandOpenTools mcpCommandMode = iota
	mcpCommandRunCLI
)

func classifyMCPCommand(args []string) mcpCommandMode {
	if len(args) == 0 {
		return mcpCommandOpenTools
	}
	switch args[0] {
	case "tui", "setup", "--tui", "local", "remote", "add-local", "add-remote", "remote-add", "local-add":
		return mcpCommandOpenTools
	case "add":
		if len(args) == 1 || mcpAddModeFromArgs(args[1:]) != "" {
			return mcpCommandOpenTools
		}
		return mcpCommandRunCLI
	default:
		return mcpCommandRunCLI
	}
}

func runMCPCommand(args []string) {
	switch classifyMCPCommand(args) {
	case mcpCommandOpenTools:
		runTUIWithOptions(tuiStartupOptions{forceInitialTab: []int{views.TabTools, views.ToolSubMCP}, mcpAddMode: mcpDefaultAddModeFromArgs(args)})
		return
	}
	if err := enforceScriptedCommandSecurity("mcp", args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(exitCodeForError(err))
	}
	if err := commands.RunMCP(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(exitCodeForError(err))
	}
}

func mcpAddModeFromArgs(args []string) string {
	for i, arg := range args {
		if !strings.EqualFold(strings.TrimSpace(arg), "add") {
			continue
		}
		if i == len(args)-1 {
			return "local"
		}
		if mode := mcpAddModeFromArgs(args[i+1:]); mode != "" {
			return mode
		}
		return ""
	}
	if len(args) > 0 && strings.EqualFold(strings.TrimSpace(args[0]), "add") {
		if len(args) == 1 {
			return "local"
		}
		return mcpAddModeFromArgs(args[1:])
	}
	for _, arg := range args {
		switch strings.ToLower(strings.TrimSpace(arg)) {
		case "remote", "add-remote", "remote-add", "http", "sse":
			return "remote"
		case "local", "add-local", "local-add", "npx", "uvx":
			return "local"
		}
	}
	return ""
}

func mcpDefaultAddModeFromArgs(args []string) string {
	if mode := mcpAddModeFromArgs(args); mode != "" {
		return mode
	}
	for _, arg := range args {
		switch strings.ToLower(strings.TrimSpace(arg)) {
		case "", "mcp", "setup", "tui", "--tui":
			continue
		default:
			return ""
		}
	}
	return mcpAddModeAutoLocal
}

func llmCLIArgs(args []string) []string {
	if len(args) >= 2 && args[0] == "setup" {
		switch args[1] {
		case "cli", "--cli", "--no-tui":
			return append([]string{"setup"}, args[2:]...)
		}
	}
	return args
}

type onboardingCommandMode int

const (
	onboardingCommandOpenSetup onboardingCommandMode = iota
	onboardingCommandRunCLI
)

func classifyOnboardingCommand(args []string) onboardingCommandMode {
	if len(args) == 0 {
		return onboardingCommandOpenSetup
	}
	if _, ok := onboardingTUIEmailFromArgs(args); ok {
		return onboardingCommandOpenSetup
	}
	switch args[0] {
	case "tui", "setup", "--tui":
		return onboardingCommandOpenSetup
	case "cli", "--cli", "--no-tui":
		return onboardingCommandRunCLI
	default:
		// Keep existing scripted onboarding invocations working: any flags or
		// positional arguments still use the CLI wizard.
		return onboardingCommandRunCLI
	}
}

func runOnboardingCommand(args []string) {
	if email, ok := onboardingTUIEmailFromArgs(args); ok {
		runTUIWithOptions(tuiStartupOptions{forceInitialTab: []int{views.TabOnboarding}, onboardingEmail: email})
		return
	}
	switch classifyOnboardingCommand(args) {
	case onboardingCommandOpenSetup:
		runTUI(views.TabOnboarding)
		return
	}
	if err := commands.RunOnboarding(onboardingCLIArgs(args)); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(exitCodeForError(err))
	}
}

func onboardingTUIEmailFromArgs(args []string) (string, bool) {
	if len(args) == 2 && setupEmailFlag(args[0]) {
		return "", false
	}
	return setupEmailFromArgs(args)
}

func onboardingCLIArgs(args []string) []string {
	if len(args) == 0 {
		return args
	}
	switch args[0] {
	case "cli", "--cli", "--no-tui":
		return args[1:]
	default:
		return args
	}
}

// exitCodeForError 根据错误类型返回退出码。
func exitCodeForError(err error) int {
	var ue *commands.UsageError
	if errors.As(err, &ue) {
		return commands.ExitUsage
	}
	return commands.ExitError
}

// resolveHubCredentials 从环境变量获取 Hub 连接信息。
func resolveHubCredentials() (hubURL, token string) {
	hubURL = os.Getenv("MACLAW_HUB_URL")
	if hubURL == "" {
		hubURL = "http://localhost:9099"
	}
	token = os.Getenv("MACLAW_TOKEN")
	if token == "" {
		fmt.Fprintln(os.Stderr, "Error: MACLAW_TOKEN environment variable is required")
		os.Exit(commands.ExitUsage)
	}
	return
}

// runLaunchCommand 处理 launch 子命令：启动编程工具。
// 用法: <brand>-tui launch <tool> [--project <dir>] [--yolo] [--admin]
func runLaunchCommand(args []string) {
	fmt.Fprintln(os.Stderr, "launch command has been moved to the main binary. Use: maclaw launch <tool>")
	os.Exit(commands.ExitUsage)
}
