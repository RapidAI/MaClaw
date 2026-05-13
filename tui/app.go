package main

// app.go is the TUI interactive mode entry point.
//
// Architecture: The TUI is an independent binary (CGO_ENABLED=0) for headless
// environments. It shares ALL agent logic with the GUI via corelib/agent/:
//   - corelib/agent.RunLoop — shared agent loop
//   - corelib/agent.BuildSystemPrompt — shared system prompt
//   - corelib/agent.CoreToolRegistry — shared tool registry (definition + handler bound)
//   - corelib/agent/sshtool — shared SSH tool
//
// On Windows, the TUI reads the existing maclaw GUI config from ~/.maclaw/config.json
// so it works out of the box if the GUI has already been configured.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agent/sshtool"
	"github.com/RapidAI/CodeClaw/corelib/brand"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	"github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/corelib/steering"
	"github.com/RapidAI/CodeClaw/corelib/task"
	"github.com/RapidAI/CodeClaw/corelib/tts"
	"github.com/RapidAI/CodeClaw/corelib/weixin"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
	"github.com/RapidAI/CodeClaw/tui/commands"
	"github.com/RapidAI/CodeClaw/tui/views"
	tea "github.com/charmbracelet/bubbletea"
)

// runConfigUI starts a config-only terminal UI. It is intentionally usable
// before LLM setup, so headless Linux users can configure the first provider
// without typing long config set commands.
func runConfigUI() {
	dataDir := commands.ResolveDataDir()
	store := commands.NewFileConfigStore(dataDir)
	appCfg, _ := store.LoadConfig()
	model := newConfigUIModel(appCfg)
	if _, err := tea.NewProgram(model, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "config UI error: %v\n", err)
		os.Exit(1)
	}
}

type tuiStartupOptions struct {
	forceInitialTab   []int
	serviceRedeemCode string
	onboardingEmail   string
	mcpAddMode        string
	focusSetupStatus  bool
}

const mcpAddModeAutoLocal = "auto-local"

// runTUI starts the Bubble Tea interactive mode.
func runTUI(forceInitialTab ...int) {
	runTUIWithOptions(tuiStartupOptions{forceInitialTab: forceInitialTab})
}

func runTUIWithOptions(startup tuiStartupOptions) {
	logger := NewTUILogger()

	// Data directory: ~/.maclaw (shared with GUI).
	dataDir := commands.ResolveDataDir()
	logDir := filepath.Join(dataDir, "logs")
	os.MkdirAll(logDir, 0755)
	logPath := filepath.Join(logDir, "tui.log")
	if err := logger.SetLogFile(logPath); err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot open log file: %v\n", err)
	}
	// Redirect stderr (fd 2) to the log file BEFORE entering Bubble Tea's
	// alt-screen. This is the mechanism-level fix: every write to stderr —
	// whether from Go's log package, fmt.Fprintf(os.Stderr,...), corelib
	// packages, or even C libraries — goes to the log file instead of
	// corrupting the terminal. See redirect_stderr_{unix,windows}.go.
	if lf := logger.LogFile(); lf != nil {
		redirectStderr(lf)
	}
	defer logger.Close()

	logger.Info("%s-tui starting (version %s)", strings.ToLower(brand.Current().DisplayName), version)

	// Load full app config from ~/.maclaw/config.json (shared with GUI).
	configStore := commands.NewFileConfigStore(dataDir)
	appCfg, configLoadErr := configStore.LoadConfig()
	if configLoadErr != nil {
		logger.Warn("config load failed, using defaults: %v", configLoadErr)
	}

	// Build LLM config from the shared config file.
	llmCfg := buildLLMConfigFromAppConfig(appCfg)
	llmConfigured := tuiConfigLLMReady(appCfg)

	// Hint: incomplete remote config (has email+hub but no credentials).
	if tuiRemoteActivationIncomplete(appCfg) {
		fmt.Fprintln(os.Stderr, tuiText(tuiConfigLang(appCfg), "incompleteRemoteHint"))
		fmt.Fprintln(os.Stderr, tuiFormat(tuiConfigLang(appCfg), "incompleteRemoteActivate", strings.ToLower(brand.Current().DisplayName)))
	}

	// Initialize memory store (shared with GUI — same ~/.maclaw/memory/).
	memoryDir := filepath.Join(dataDir, "memory")
	os.MkdirAll(memoryDir, 0755)
	memStore, err := memory.NewStore(memoryDir)
	if err != nil {
		logger.Warn("memory store init failed: %v", err)
	}

	// Initialize SSH manager.
	sshMgr := remote.NewSSHSessionManager(nil)

	// Initialize steering store (shared with GUI — same ~/.maclaw/steering/).
	home, _ := os.UserHomeDir()
	steeringDir := filepath.Join(home, ".maclaw", "steering")
	steeringStore := steering.NewStore(steeringDir, "")
	steeringStore.Load()
	steering.EnsureDefaults(steeringDir)

	// Resolve language from config.
	lang := appCfg.Language
	if lang == "" {
		lang = "zh"
	}

	// Persistent conversation history (survives TUI restart).
	convPath := filepath.Join(dataDir, "data", "tui_conversation.json")
	os.MkdirAll(filepath.Dir(convPath), 0755)
	convMemory := agent.NewPersistentConversationMemory(convPath)

	// Build the TUI app.
	// HubCenter failover uses the shared singleton cache and persister from
	// tui/commands/skill_search_api.go — no need to create local instances.
	app := &TUIApp{
		logger:        logger,
		llmConfig:     llmCfg,
		memoryStore:   memStore,
		sshMgr:        sshMgr,
		steeringStore: steeringStore,
		appConfig:     appCfg,
		history:       convMemory,
		taskStore:     task.NewStore(),
		toolRegistry:  agent.NewCoreToolRegistry(),
		ttsManager:    initTUITTSManager(),
	}

	// Initialize scheduled task manager with background ticker.
	schPath := filepath.Join(dataDir, "scheduled_tasks.json")
	if schMgr, err := scheduler.NewManager(schPath); err == nil {
		app.scheduledTaskManager = schMgr
	} else {
		log.Printf("[TUI] WARNING: scheduled task manager init failed: %v", err)
	}

	// Initialize workflow engine (19 templates, same as GUI).
	app.workflowEngine = app.initWorkflowEngine()

	// Initialize WeChat gateway (runs in background if configured).
	app.weixinGateway = newTUIWeixinGateway(app)

	// Register tools: definition + handler bound together.
	sshHandler := func(args map[string]interface{}) string {
		deps := sshtool.SSHToolDeps{
			Manager: sshMgr,
			HostLoader: func() []corelib.SSHHostEntry {
				return app.appConfig.SSHHosts
			},
		}
		return sshtool.ToolSSH(deps, args)
	}
	agent.RegisterCoreTools(app.toolRegistry, agent.CoreToolDeps{
		MemoryStore: memStore,
		TaskStore:   app.taskStore,
		SSHHandler:  sshHandler,
		ExtraHandlers: map[string]agent.ToolHandler{
			"manage_skill":     newManageSkillHandler(app),
			"manage_schedule":  newManageScheduleHandler(app),
			"tts":             newTTSHandler(app),
		},
		WebSearchHandler: func(args map[string]interface{}) string {
			// Use the first configured web search provider, or DuckDuckGo fallback.
			var provider corelib.WebSearchProvider
			if len(app.appConfig.WebSearchProviders) > 0 {
				for _, p := range app.appConfig.WebSearchProviders {
					if p.Name == app.appConfig.WebSearchCurrentProvider {
						provider = p
						break
					}
				}
				if provider.Name == "" {
					provider = app.appConfig.WebSearchProviders[0]
				}
			}
			if provider.Type == "" {
				provider.Type = "duckduckgo"
			}
			return agent.ToolWebSearch(provider, args)
		},
		WebFetchHandler: func(args map[string]interface{}) string {
			return agent.ToolWebFetch(args)
		},
	})

	// Start the scheduled task manager now that tools are registered.
	// The executor uses agent.RunLoop with the full tool registry.
	if app.scheduledTaskManager != nil && llmConfigured {
		app.scheduledTaskManager.StartWithExecutor(app.buildScheduledTaskExecutor())
	}

	root := views.NewRootModel(lang)
	root.Chat.FocusInput()
	// Show current LLM model in status bar.
	modelLabel := tuiModelDisplayLabel(lang, llmCfg.ProviderName, llmCfg.Model)
	root.StatusBar.SetModelInfo(modelLabel)
	if !llmConfigured {
		root.StatusBar.SetMessage(tuiText(lang, "llmNotConfigured"))
	}

	// Restore previous conversation into the chat view.
	if prevHistory := convMemory.Load("tui-user"); len(prevHistory) > 0 {
		var restored []views.ChatMessage
		for _, entry := range prevHistory {
			content, _ := entry.Content.(string)
			if content == "" {
				continue
			}
			if entry.Role == "user" || entry.Role == "assistant" {
				restored = append(restored, views.ChatMessage{Role: entry.Role, Content: content})
			}
		}
		if len(restored) > 0 {
			root.Chat.SetMessages(restored)
			root.Chat.AppendSystemMessage(tuiFormat(lang, "restoredHistory", len(restored)))
		}
	}

	tuiModel := &tuiModel{
		app:  app,
		root: root,
	}

	// Populate initial tool data from config.
	tuiModel.refreshToolData()

	// Populate config-backed setup views from AppConfig.
	tuiModel.root.Config.LoadFromAppConfig(appCfg)
	tuiModel.root.Onboarding.LoadFromAppConfig(appCfg)
	tuiModel.root.Service.LoadFromAppConfig(appCfg)
	tuiModel.configureInitialTab(appCfg, llmConfigured, lang)
	if len(startup.forceInitialTab) > 0 {
		tuiModel.applyForcedInitialTab(startup.forceInitialTab[0], appCfg, llmConfigured, lang)
		if len(startup.forceInitialTab) > 1 && startup.forceInitialTab[0] == views.TabConfig {
			tuiModel.root.Config.FocusTab(startup.forceInitialTab[1])
		}
		if len(startup.forceInitialTab) > 1 && startup.forceInitialTab[0] == views.TabTools {
			tuiModel.root.Tools.FocusTab(startup.forceInitialTab[1])
		}
		if len(startup.forceInitialTab) > 1 && startup.forceInitialTab[0] == views.TabTasks {
			tuiModel.root.Tasks.FocusTab(startup.forceInitialTab[1])
		}
	}
	tuiModel.applyStartupPrefills(startup)
	if startup.focusSetupStatus {
		tuiModel.root.SetTab(views.TabConfig)
		tuiModel.root.Config.FocusSetupStatus()
		tuiModel.root.StatusBar.SetMessage(tuiText(lang, "slashOpenStatus"))
	}
	if configLoadErr != nil {
		tuiModel.root.StatusBar.SetMessage(tuiFormat(lang, "configLoadWarning", configLoadErr.Error()))
	}

	// Mark task sub-tabs as loaded (no data source in standalone TUI yet).
	tuiModel.root.Tasks.SetTasks(nil)
	tuiModel.root.Tasks.SetRemoteTasks(nil)
	tuiModel.root.Tasks.SetBackgroundTasks(nil)

	p := tea.NewProgram(tuiModel, tea.WithAltScreen())
	tuiModel.program = p

	// Start WeChat gateway now that the program is available for UI messages.
	if app.weixinGateway != nil {
		app.weixinGateway.SetProgram(p)
		app.weixinGateway.Start()
	}

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}

	// Cleanup.
	if app.weixinGateway != nil {
		app.weixinGateway.Shutdown()
	}
	if app.scheduledTaskManager != nil {
		app.scheduledTaskManager.Stop()
	}
	if err := convMemory.FlushNow(); err != nil {
		logger.Error("conversation memory flush failed: %v", err)
	}
	convMemory.Stop()
	if memStore != nil {
		memStore.Stop()
	}
	sshMgr.Close()
	logger.Info("%s-tui stopped", strings.ToLower(brand.Current().DisplayName))
}

// buildLLMConfigFromAppConfig constructs MaclawLLMConfig from the shared
// AppConfig. This reads the same config.json that the GUI writes, so if
// the user has already configured maclaw via the GUI, the TUI works immediately.
func buildLLMConfigFromAppConfig(cfg corelib.AppConfig) corelib.MaclawLLMConfig {
	llm := corelib.MaclawLLMConfig{
		URL:           cfg.MaclawLLMUrl,
		Key:           cfg.MaclawLLMKey,
		Model:         cfg.MaclawLLMModel,
		Protocol:      cfg.MaclawLLMProtocol,
		ContextLength: cfg.MaclawLLMContextLength,
		TimeoutSec:    cfg.MaclawLLMTimeoutSec,
		ProviderName:  cfg.MaclawLLMCurrentProvider,
	}
	// Resolve provider-specific fields from the current provider entry.
	for _, p := range cfg.MaclawLLMProviders {
		if p.Name == cfg.MaclawLLMCurrentProvider {
			if strings.TrimSpace(llm.Key) == "" {
				llm.Key = strings.TrimSpace(p.Key)
			}
			llm.AgentType = p.AgentType
			llm.SupportsVision = p.SupportsVision
			llm.WireAPI = p.WireAPI
			break
		}
	}
	if strings.TrimSpace(llm.Key) == "" && tuiAppConfigUsesHubLLMService(cfg) {
		llm.Key = strings.TrimSpace(cfg.RemoteViewerToken)
	}
	return llm
}

func tuiAppConfigUsesHubLLMService(cfg corelib.AppConfig) bool {
	current := strings.TrimSpace(cfg.MaclawLLMCurrentProvider)
	if current == tuiHubServiceProviderName || strings.EqualFold(current, "MaClaw Official") {
		return true
	}
	for _, provider := range cfg.MaclawLLMProviders {
		if provider.IsHubService && strings.TrimSpace(provider.Name) == current {
			return true
		}
	}
	return false
}

// TUIApp holds the TUI's agent infrastructure.
type TUIApp struct {
	logger        *TUILogger
	llmConfig     corelib.MaclawLLMConfig
	memoryStore   *memory.Store
	sshMgr        *remote.SSHSessionManager
	steeringStore *steering.Store
	appConfig     corelib.AppConfig
	history       *agent.ConversationMemory
	taskStore     *task.Store
	toolRegistry  *agent.CoreToolRegistry
	ttsManager    *tts.Manager
	// HubCenter failover uses the shared singleton cache and persister from
	// tui/commands/skill_search_api.go — no fields needed here.

	// Scheduled task manager — background ticker fires due tasks.
	scheduledTaskManager *scheduler.Manager

	// WeChat gateway — runs in background, receives/sends WeChat messages.
	weixinGateway *tuiWeixinGateway

	// Workflow engine integration (Fix #6).
	workflowEngine *workflow.WorkflowEngine

	// workflowMu protects pendingPhasePrompt and workflowAgentLoop from
	// concurrent access between the Bubble Tea main goroutine (which sets
	// them in handleWorkflowInterception) and the agent loop goroutine
	// (which reads and clears them in handleChatSend).
	workflowMu         sync.Mutex
	pendingPhasePrompt string // stashed phase prompt for the next agent loop
	workflowAgentLoop  bool   // true when the agent loop runs on behalf of the workflow
}

// buildSystemPromptDeps constructs the SystemPromptDeps from TUIApp's config.
// Used by tuiCallbacks (main agent loop). The /btw SubAgent builds its own
// focused prompt via buildTuiBtwSystemPrompt instead.
func (app *TUIApp) buildSystemPromptDeps() agent.SystemPromptDeps {
	cfg := app.appConfig
	roleName := cfg.MaclawRoleName
	if roleName == "" {
		roleName = brand.Current().DisplayName
	}
	roleDesc := cfg.MaclawRoleDescription
	if roleDesc == "" {
		roleDesc = tuiText(tuiConfigLang(cfg), "defaultRoleDescription")
	}

	deps := agent.SystemPromptDeps{
		Config: agent.SystemPromptConfig{
			RoleName:          roleName,
			RoleDescription:   roleDesc,
			IsProMode:         true,
			Nickname:          cfg.RemoteNickname,
			HasCodingSessions: false,
		},
		MemoryStore: app.memoryStore,
	}

	if len(cfg.SSHHosts) > 0 {
		deps.SSHHostLister = func() []corelib.SSHHostEntry {
			return cfg.SSHHosts
		}
	}

	if app.steeringStore != nil {
		deps.SteeringResolver = func(userMessage string, contextTokens int) []steering.File {
			ctx := steering.ResolveContext{
				UserMessage:            userMessage,
				EffectiveContextTokens: contextTokens,
			}
			return app.steeringStore.Resolve(ctx)
		}
	}

	return deps
}

// cancellable is implemented by agent loop callbacks that support cancellation.
type cancellable interface {
	Cancel()
}

// tuiModel is the Bubble Tea top-level model.
type tuiModel struct {
	app        *TUIApp
	program    *tea.Program
	root       views.RootModel
	ready      bool
	startupCmd tea.Cmd
	activeCb   cancellable // non-nil while agent loop is running
}

func (m *tuiModel) Init() tea.Cmd {
	readyCmd := func() tea.Msg {
		time.Sleep(100 * time.Millisecond)
		return tuiReadyMsg{}
	}
	if m.startupCmd != nil {
		return tea.Batch(readyCmd, m.startupCmd)
	}
	return readyCmd
}

type tuiReadyMsg struct{}

func (m *tuiModel) configureInitialTab(cfg corelib.AppConfig, llmConfigured bool, lang string) {
	if tuiConfigLLMNeedsKey(cfg) {
		m.root.SetTab(views.TabConfig)
		m.root.Config.FocusLLMKey()
		m.root.StatusBar.SetMessage(tuiText(lang, "llmKeyMissing"))
		return
	}
	if !tuiSetupReady(cfg, llmConfigured) {
		m.root.SetTab(views.TabOnboarding)
		m.root.StatusBar.SetMessage(tuiText(lang, "slashOpenSetup"))
		return
	}
	if llmConfigured {
		if !tuiMCPConfigured(cfg) {
			m.root.StatusBar.SetMessage(tuiText(lang, "mcpOptionalReady"))
		}
		return
	}
	if tuiRemoteActivationIncomplete(cfg) {
		m.root.SetTab(views.TabOnboarding)
		m.root.StatusBar.SetMessage(tuiText(lang, "incompleteRemoteSetup"))
		return
	}
	if tuiRemoteActivationReady(cfg) {
		m.root.SetTab(views.TabServiceRedeem)
		m.root.StatusBar.SetMessage(tuiText(lang, "serviceRedeemPrompt"))
		m.startupCmd = m.refreshServiceStatusFromTUI()
		return
	}
	m.root.SetTab(views.TabConfig)
	m.root.Config.FocusLLMConfig()
}

func tuiSetupReady(cfg corelib.AppConfig, llmConfigured bool) bool {
	return cfg.OnboardingDone || llmConfigured || tuiConfigLLMReady(cfg) || tuiRemoteActivationReady(cfg)
}

func tuiConfigLLMReady(cfg corelib.AppConfig) bool {
	return views.ConfigLLMReady(cfg)
}

func tuiConfigLLMNeedsKey(cfg corelib.AppConfig) bool {
	return views.ConfigLLMNeedsKey(cfg)
}

func tuiAppLLMReady(app *TUIApp) bool {
	if app == nil {
		return false
	}
	if !tuiRuntimeLLMConfigReady(app.llmConfig) {
		return false
	}
	cfg := app.appConfig
	if strings.TrimSpace(cfg.MaclawLLMUrl) != "" ||
		strings.TrimSpace(cfg.MaclawLLMModel) != "" ||
		strings.TrimSpace(cfg.MaclawLLMCurrentProvider) != "" {
		return tuiConfigLLMReady(cfg)
	}
	return true
}

func tuiRuntimeLLMConfigReady(llm corelib.MaclawLLMConfig) bool {
	return strings.TrimSpace(llm.URL) != "" && strings.TrimSpace(llm.Model) != ""
}

func tuiMCPConfigured(cfg corelib.AppConfig) bool {
	return len(cfg.MCPServers)+len(cfg.LocalMCPServers) > 0
}

func (m *tuiModel) applyForcedInitialTab(tab int, cfg corelib.AppConfig, llmConfigured bool, lang string) {
	m.root.SetTab(tab)
	if tab != views.TabServiceRedeem {
		m.startupCmd = nil
	}
	switch tab {
	case views.TabOnboarding:
		m.root.StatusBar.SetMessage(tuiText(lang, "slashOpenSetup"))
	case views.TabServiceRedeem:
		m.root.StatusBar.SetMessage(tuiText(lang, "slashOpenRedeem"))
		if strings.TrimSpace(cfg.RemoteHubURL) != "" && strings.TrimSpace(cfg.RemoteViewerToken) != "" && m.startupCmd == nil {
			m.startupCmd = m.refreshServiceStatusFromTUI()
		}
	case views.TabConfig:
		m.root.StatusBar.SetMessage(tuiText(lang, "slashOpenConfig"))
	case views.TabTools:
		m.root.StatusBar.SetMessage(tuiText(lang, "slashOpenTools"))
	case views.TabTasks:
		m.root.StatusBar.SetMessage(tuiText(lang, "slashOpenTasks"))
	case views.TabChat:
		m.root.StatusBar.SetMessage(tuiText(lang, "slashOpenChat"))
	}
}

func (m *tuiModel) applyStartupPrefills(startup tuiStartupOptions) {
	if startup.onboardingEmail != "" {
		m.root.Onboarding.SetInitialEmail(startup.onboardingEmail)
	}
	if startup.serviceRedeemCode != "" {
		m.root.Service.SetInitialCode(startup.serviceRedeemCode)
		m.startupCmd = nil
	}
	switch startup.mcpAddMode {
	case mcpAddModeAutoLocal:
		m.root.SetTab(views.TabTools)
		m.root.Tools.FocusMCP()
		if m.app != nil && !tuiMCPConfigured(m.app.appConfig) {
			m.root.StatusBar.SetMessage(tuiText(m.uiLang(), "configOpenTools"))
			return
		}
		m.root.StatusBar.SetMessage(tuiText(m.uiLang(), "slashOpenMCPList"))
	case "local":
		m.root.SetTab(views.TabTools)
		m.root.Tools.StartMCPLocalTemplate()
		m.root.StatusBar.SetMessage(tuiText(m.uiLang(), "slashOpenMCP"))
	case "remote":
		m.root.SetTab(views.TabTools)
		m.root.Tools.StartMCPRemoteTemplate()
		m.root.StatusBar.SetMessage(tuiText(m.uiLang(), "slashOpenMCP"))
	}
}

func tuiRemoteActivationIncomplete(cfg corelib.AppConfig) bool {
	if strings.TrimSpace(cfg.RemoteEmail) == "" {
		return false
	}
	if tuiRemoteActivationReady(cfg) {
		return false
	}
	return strings.TrimSpace(cfg.RemoteMachineID) == "" ||
		strings.TrimSpace(cfg.RemoteMachineToken) == "" ||
		strings.TrimSpace(cfg.RemoteViewerToken) == ""
}

func tuiRemoteActivationReady(cfg corelib.AppConfig) bool {
	return strings.TrimSpace(cfg.RemoteHubURL) != "" && strings.TrimSpace(cfg.RemoteViewerToken) != ""
}

// cancelActiveLoop cancels the running agent loop (if any) and clears the reference.
func (m *tuiModel) cancelActiveLoop() {
	if m.activeCb != nil {
		m.activeCb.Cancel()
		m.activeCb = nil
	}
}

func (m *tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tuiReadyMsg:
		m.ready = true
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || msg.String() == "ctrl+q" {
			m.cancelActiveLoop()
			return m, tea.Quit
		}
		// 'q' quits when no view has a focused text input and help is not visible.
		if msg.String() == "q" && !m.root.AcceptsTextInput() && !m.root.Help.IsVisible() {
			m.cancelActiveLoop()
			return m, tea.Quit
		}
		if (msg.String() == "f3" || msg.String() == "alt+3") && m.shouldOpenMCPTemplateShortcut() {
			m.root.Help.Hide()
			m.root.SetTab(views.TabTools)
			m.root.Tools.StartMCPLocalTemplate()
			m.root.StatusBar.SetMessage(tuiText(m.uiLang(), "slashOpenMCP"))
			return m, nil
		}
		// Esc while waiting: cancel the running agent loop.
		// But if Help is visible, let Esc close Help instead (handled by root→help).
		if msg.String() == "esc" && m.activeCb != nil && !m.root.Help.IsVisible() {
			m.cancelActiveLoop()
			m.root.Chat.AppendSystemMessage(tuiText(m.uiLang(), "cancelled"))
			return m, nil
		}

	case views.ChatSendMsg:
		// Handle slash commands locally.
		if strings.HasPrefix(strings.TrimSpace(msg.Text), "/") {
			// /btw requires an async agent loop — route through handleChatSend.
			trimmedCmd := strings.TrimSpace(msg.Text)
			if trimmedCmd == "/btw" || strings.HasPrefix(trimmedCmd, "/btw ") {
				if m.llmMissing() {
					return m, m.routeMissingLLMFromChat()
				}
				return m, m.handleChatSend(msg.Text)
			}
			return m, m.handleSlashCommand(msg.Text)
		}
		if m.llmMissing() {
			return m, m.routeMissingLLMFromChat()
		}
		// Start the agent loop directly. The user message was already added
		// to ChatModel.messages and rendered in the previous Update→View cycle
		// (ChatModel.Update handles the Enter key, adds the message, and
		// returns ChatSendMsg as a Cmd — Bubble Tea renders View() between
		// that Update and this one). No artificial delay needed.
		return m, m.handleChatSend(msg.Text)

	case views.ChatQueueFireMsg:
		// 预输入队列自动发射或手动发射——与 ChatSendMsg 相同处理。
		if m.llmMissing() {
			return m, m.routeMissingLLMFromChat()
		}
		return m, m.handleChatSend(msg.Text)

	case views.ChatResponseMsg:
		if m.activeCb == nil && msg.Error == "cancelled" {
			return m, nil // ignore stale cancel response after Esc
		}
		m.activeCb = nil // loop finished

	case views.ChatClearMsg:
		m.app.history.Clear("tui-user")
		return m, nil

	// --- Tool view async messages ---
	case views.ToolSkillSearchMsg:
		return m, m.searchSkills(msg.Query)

	case views.ToolSkillInstallMsg:
		return m, m.installSkill(msg.SkillID, msg.HubURL, msg.Source, msg.InstallRef)

	case views.ToolMCPAddMsg:
		return m, m.addLocalMCP(msg.Entry)

	case views.ToolMCPAddRemoteMsg:
		return m, m.addRemoteMCP(msg.Entry)

	case views.ToolOperationResultMsg:
		// After successful MCP add, reload config and refresh data.
		if msg.Success && msg.Tab == views.ToolSubMCP {
			m.reloadConfigBackedViews()
			m.refreshToolData()
			if strings.TrimSpace(msg.Message) != "" {
				m.root.StatusBar.SetMessage(tuiFormat(m.uiLang(), "mcpAddedReady", msg.Message))
			}
		}

	case views.ToolRefreshMsg:
		m.refreshToolData()
		return m, nil

	case views.ConfigSaveMsg:
		return m, m.saveConfig(msg)

	case views.ConfigSaveFailedMsg:
		label := views.ConfigDisplayNameForLang(msg.Key, m.uiLang())
		m.root.StatusBar.SetMessage(tuiFormat(m.uiLang(), "configSaveFailed", label, msg.Error))
		return m, nil

	case views.ConfigSavedMsg:
		// Reload config from disk (the goroutine already saved it).
		dataDir := commands.ResolveDataDir()
		store := commands.NewFileConfigStore(dataDir)
		savedMessage := tuiFormat(m.uiLang(), "configSaved", views.ConfigDisplayNameForLang(msg.Key, m.uiLang()))
		var nextCmd tea.Cmd
		if cfg, err := store.LoadConfig(); err == nil {
			m.app.appConfig = cfg
			if msg.Key == "language" {
				m.root.SetLang(cfg.Language)
				m.refreshStatusBarModelInfo()
				savedMessage = tuiFormat(m.uiLang(), "configSaved", views.ConfigDisplayNameForLang(msg.Key, m.uiLang()))
			}
			m.root.Config.LoadFromAppConfig(cfg)
			m.root.Onboarding.LoadFromAppConfig(cfg)
			m.root.Service.LoadFromAppConfig(cfg)
			if msg.Key == "onboarding" {
				m.app.llmConfig = buildLLMConfigFromAppConfig(cfg)
				m.refreshStatusBarModelInfo()
				llmConfigured := tuiConfigLLMReady(cfg)
				if !llmConfigured {
					if strings.TrimSpace(cfg.RemoteHubURL) != "" && strings.TrimSpace(cfg.RemoteViewerToken) != "" {
						m.root.SetTab(views.TabServiceRedeem)
						savedMessage = tuiText(m.uiLang(), "onboardingCheckService")
						if !m.root.Service.HasPendingCode() {
							nextCmd = m.refreshServiceStatusFromTUI()
						}
					} else {
						m.root.SetTab(views.TabConfig)
						m.root.Config.FocusLLMConfig()
						savedMessage = tuiText(m.uiLang(), "onboardingNeedConfig")
					}
				} else {
					m.root.SetTab(views.TabChat)
					savedMessage = tuiText(m.uiLang(), "onboardingComplete")
					if !tuiMCPConfigured(cfg) {
						savedMessage = tuiText(m.uiLang(), "onboardingCompleteMCP")
					}
				}
			}
			if strings.HasPrefix(msg.Key, "maclaw_llm_") {
				m.app.llmConfig = buildLLMConfigFromAppConfig(cfg)
				m.refreshStatusBarModelInfo()
			}
			// Re-sync WeChat gateway on IM config changes.
			if strings.HasPrefix(msg.Key, "weixin_") || msg.Key == "im_channel_profile" {
				if m.app.weixinGateway != nil {
					m.app.weixinGateway.Start()
				}
			}
		}
		m.root.StatusBar.SetMessage(savedMessage)
		if nextCmd != nil {
			return m, nextCmd
		}
		return m, nil

	case views.OnboardingActivateRemoteMsg:
		return m, m.activateRemoteFromTUI(msg.Email, msg.HubCenterURL)

	case views.OnboardingStartWeixinMsg:
		return m, m.startWeixinFromTUI()

	case views.OnboardingPollWeixinMsg:
		return m, m.pollWeixinFromTUI(msg.Token)

	case views.OnboardingLanguageChangedMsg:
		return m, m.saveOnboardingLanguage(msg.Language)

	case views.OnboardingRemoteResultMsg:
		if msg.Success {
			m.reloadConfigBackedViews()
		}
		var cmd tea.Cmd
		m.root, cmd = m.root.Update(msg)
		if msg.Success && (msg.HubServiceReady || msg.MachineReady) {
			m.root.StatusBar.SetMessage(tuiText(m.uiLang(), "hubActivationSuccess"))
		} else if msg.Success {
			status := strings.TrimSpace(msg.Message)
			if status == "" {
				status = tuiText(m.uiLang(), "viewerTokenMissing")
			}
			m.root.StatusBar.SetMessage(status)
		} else {
			m.root.StatusBar.SetMessage(tuiFormat(m.uiLang(), "hubActivationFailed", msg.Message))
		}
		return m, cmd

	case views.OnboardingWeixinQRMsg, views.OnboardingWeixinPollResultMsg:
		var cmd tea.Cmd
		m.root, cmd = m.root.Update(msg)
		if result, ok := msg.(views.OnboardingWeixinPollResultMsg); ok && result.Success {
			m.reloadConfigBackedViews()
			m.root.StatusBar.SetMessage(tuiText(m.uiLang(), "weixinBound"))
			// Start WeChat gateway after successful binding.
			if m.app.weixinGateway != nil {
				m.app.weixinGateway.Start()
			}
		}
		return m, cmd

	case tuiWeixinStatusMsg:
		handleTUIWeixinStatus(m, msg)
		return m, nil

	case tuiWeixinIncomingMsg:
		handleTUIWeixinIncoming(m, msg)
		return m, nil

	case views.OnboardingFinishMsg:
		return m, m.finishOnboardingFromTUI()

	case views.ConfigOpenSetupMsg:
		m.root.SetTab(views.TabOnboarding)
		m.root.StatusBar.SetMessage(tuiText(m.uiLang(), "configOpenSetup"))
		return m, nil

	case views.ConfigOpenServiceRedeemMsg:
		m.root.SetTab(views.TabServiceRedeem)
		m.root.StatusBar.SetMessage(tuiText(m.uiLang(), "configOpenRedeem"))
		if m.serviceRedeemRefreshReady() {
			return m, m.refreshServiceStatusFromTUI()
		}
		return m, nil

	case views.ConfigOpenToolsMsg:
		m.root.SetTab(views.TabTools)
		m.root.Tools.FocusMCP()
		m.root.StatusBar.SetMessage(tuiText(m.uiLang(), "configOpenTools"))
		return m, nil

	case views.ServiceRedeemOpenSetupMsg:
		m.root.SetTab(views.TabOnboarding)
		m.root.StatusBar.SetMessage(tuiText(m.uiLang(), "serviceOpenSetup"))
		return m, nil

	case views.TaskOpenToolsMsg:
		m.root.SetTab(views.TabTools)
		m.root.Tools.FocusMCP()
		m.root.StatusBar.SetMessage(tuiText(m.uiLang(), "configOpenTools"))
		return m, nil

	case views.TaskOpenChatMsg:
		m.root.SetTab(views.TabChat)
		m.root.StatusBar.SetMessage(tuiText(m.uiLang(), "slashOpenChat"))
		return m, nil

	case views.ServiceRedeemRefreshMsg:
		return m, m.refreshServiceStatusFromTUI()

	case views.ServiceRedeemSubmitMsg:
		return m, m.redeemServiceFromTUI(msg.Code)

	case views.ServiceRedeemResultMsg:
		if msg.HasConfig && m.app != nil {
			m.app.appConfig = msg.Config
			m.app.llmConfig = buildLLMConfigFromAppConfig(msg.Config)
			label := tuiModelDisplayLabel(m.uiLang(), m.app.llmConfig.ProviderName, m.app.llmConfig.Model)
			m.root.StatusBar.SetModelInfo(label)
			m.root.Config.LoadFromAppConfig(msg.Config)
			m.root.Onboarding.LoadFromAppConfig(msg.Config)
			m.root.Service.LoadFromAppConfig(msg.Config)
		}
		var cmd tea.Cmd
		m.root, cmd = m.root.Update(msg)
		if msg.Success {
			if msg.FromRefresh {
				m.root.Chat.AppendSystemMessage(tuiText(m.uiLang(), "serviceRefreshReadyChat"))
			} else {
				m.root.Chat.AppendSystemMessage(tuiText(m.uiLang(), "serviceRedeemSuccessChat"))
			}
		}
		if strings.TrimSpace(msg.Message) != "" {
			m.root.StatusBar.SetMessage(msg.Message)
		}
		return m, cmd
	}

	previousTab := m.root.ActiveTab()
	var cmd tea.Cmd
	m.root, cmd = m.root.Update(msg)
	if previousTab != views.TabServiceRedeem && m.root.ActiveTab() == views.TabServiceRedeem && m.serviceRedeemRefreshReady() {
		return m, tea.Batch(cmd, m.refreshServiceStatusFromTUI())
	}
	return m, cmd
}

func (m *tuiModel) refreshStatusBarModelInfo() {
	if m == nil || m.app == nil {
		return
	}
	label := tuiModelDisplayLabel(m.uiLang(), m.app.llmConfig.ProviderName, m.app.llmConfig.Model)
	m.root.StatusBar.SetModelInfo(label)
}

func (m *tuiModel) serviceRedeemRefreshReady() bool {
	if m == nil || m.app == nil {
		return false
	}
	cfg := m.app.appConfig
	return strings.TrimSpace(cfg.RemoteHubURL) != "" && strings.TrimSpace(cfg.RemoteViewerToken) != ""
}

// handleSlashCommand processes /commands. Only called when text starts with "/".
func (m *tuiModel) handleSlashCommand(text string) tea.Cmd {
	text = strings.TrimSpace(text)
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return nil
	}
	cmdName := fields[0]
	args := fields[1:]
	switch {
	case cmdName == "/new" || cmdName == "/clear":
		m.app.history.Clear("tui-user")
		// Cancel active workflow if any (even if workflow is disabled,
		// to clean up stale state from before the toggle was turned off).
		if m.app.workflowEngine != nil {
			_ = m.app.workflowEngine.CancelWorkflow("tui-user")
			if understanding := m.app.workflowEngine.GetUnderstanding(); understanding != nil && understanding.HasActiveSession("tui-user") {
				_, _, _, _, _ = understanding.HandleInput("tui-user", "取消")
			}
		}
		m.app.workflowMu.Lock()
		m.app.pendingPhasePrompt = ""
		m.app.workflowAgentLoop = false
		m.app.workflowMu.Unlock()
		m.root.Chat.ClearMessages(tuiText(m.uiLang(), "chatCleared"))
		return nil

	case cmdName == "/model":
		cfg := m.app.llmConfig
		info := tuiFormat(m.uiLang(), "modelInfoFull", cfg.Model, tuiProviderDisplayName(m.uiLang(), cfg.ProviderName), cfg.Protocol, cfg.ContextLength)
		if cfg.Protocol == "" {
			info = tuiFormat(m.uiLang(), "modelInfoBasic", cfg.Model, tuiProviderDisplayName(m.uiLang(), cfg.ProviderName))
		}
		m.root.Chat.AppendSystemMessage(info)
		return nil

	case cmdName == "/setup" || cmdName == "/onboarding":
		if email, ok := setupEmailFromArgs(args); ok {
			m.root.SetTab(views.TabOnboarding)
			m.root.Onboarding.SetInitialEmail(email)
			m.root.StatusBar.SetMessage(tuiText(m.uiLang(), "slashOpenSetup"))
			return nil
		}
		statusKey := "slashOpenSetup"
		if tab, subTab, ok := setupInitialRoute(args); ok {
			m.root.SetTab(tab)
			switch tab {
			case views.TabConfig:
				m.root.Config.FocusTab(subTab)
			case views.TabTools:
				m.root.Tools.FocusTab(subTab)
				statusKey = "slashOpenTools"
				if subTab == views.ToolSubMCP {
					if m.startMCPDefaultAddModeFromArgs(args) {
						statusKey = "slashOpenMCP"
					}
				}
			case views.TabTasks:
				m.root.Tasks.FocusTab(subTab)
			}
		} else {
			m.root.SetTab(views.TabOnboarding)
		}
		m.root.StatusBar.SetMessage(tuiText(m.uiLang(), statusKey))
		return nil

	case cmdName == "/redeem" || cmdName == "/service":
		if email, ok := serviceSetupEmailFromArgs(args); ok {
			m.root.SetTab(views.TabOnboarding)
			m.root.Onboarding.SetInitialEmail(email)
			m.root.StatusBar.SetMessage(tuiText(m.uiLang(), "slashOpenSetup"))
			return nil
		}
		tab, subTab := serviceInitialRoute(args)
		m.root.SetTab(tab)
		switch tab {
		case views.TabConfig:
			m.root.Config.FocusTab(subTab)
		case views.TabOnboarding:
			m.root.StatusBar.SetMessage(tuiText(m.uiLang(), "slashOpenSetup"))
			return nil
		}
		prefilledCode := false
		if code, ok := serviceRedeemCodeFromArgs(args); ok {
			m.root.Service.SetInitialCode(code)
			prefilledCode = true
		}
		m.root.StatusBar.SetMessage(tuiText(m.uiLang(), "slashOpenRedeem"))
		if tab == views.TabServiceRedeem && !prefilledCode && m.serviceRedeemRefreshReady() {
			return m.refreshServiceStatusFromTUI()
		}
		return nil

	case cmdName == "/chat":
		m.root.SetTab(views.TabChat)
		m.root.StatusBar.SetMessage(tuiText(m.uiLang(), "slashOpenChat"))
		return nil

	case cmdName == "/tools" || cmdName == "/tool" || cmdName == "/skill" || cmdName == "/skills":
		m.root.SetTab(views.TabTools)
		statusKey := "slashOpenTools"
		if cmdName == "/skill" || cmdName == "/skills" {
			m.root.Tools.FocusSkill()
		} else if subTab, ok := toolsPageInitialSubTab(args); ok {
			m.root.Tools.FocusTab(subTab)
			if subTab == views.ToolSubMCP {
				if mcpDefaultAddModeFromArgs(args) == mcpAddModeAutoLocal && m.app != nil && !tuiMCPConfigured(m.app.appConfig) {
					m.root.Tools.FocusMCP()
					m.root.StatusBar.SetMessage(tuiText(m.uiLang(), "configOpenTools"))
					return nil
				}
				if m.startMCPDefaultAddModeFromArgs(args) {
					statusKey = "slashOpenMCP"
				} else {
					statusKey = "slashOpenMCPList"
				}
			}
		}
		m.root.StatusBar.SetMessage(tuiText(m.uiLang(), statusKey))
		return nil

	case cmdName == "/mcp":
		m.root.SetTab(views.TabTools)
		if mcpDefaultAddModeFromArgs(args) == mcpAddModeAutoLocal && m.app != nil && !tuiMCPConfigured(m.app.appConfig) {
			m.root.Tools.FocusMCP()
			m.root.StatusBar.SetMessage(tuiText(m.uiLang(), "configOpenTools"))
			return nil
		}
		statusKey := "slashOpenMCPList"
		if !m.startMCPDefaultAddModeFromArgs(args) {
			m.root.Tools.FocusMCP()
		} else {
			statusKey = "slashOpenMCP"
		}
		m.root.StatusBar.SetMessage(tuiText(m.uiLang(), statusKey))
		return nil

	case cmdName == "/tasks" || cmdName == "/task":
		m.root.SetTab(views.TabTasks)
		if subTab, ok := tasksPageInitialSubTab(args); ok {
			m.root.Tasks.FocusTab(subTab)
		}
		m.root.StatusBar.SetMessage(tuiText(m.uiLang(), "slashOpenTasks"))
		return nil

	case cmdName == "/schedule":
		m.root.SetTab(views.TabTasks)
		m.root.Tasks.FocusScheduled()
		m.root.StatusBar.SetMessage(tuiText(m.uiLang(), "slashOpenTasks"))
		return nil

	case cmdName == "/config" || cmdName == "/settings":
		m.root.SetTab(views.TabConfig)
		if tab, ok := configCommandInitialTab(args); ok {
			m.root.Config.FocusTab(tab)
		}
		m.root.StatusBar.SetMessage(tuiText(m.uiLang(), "slashOpenConfig"))
		return nil

	case cmdName == "/status" || cmdName == "/doctor" || cmdName == "/health":
		m.root.SetTab(views.TabConfig)
		m.root.Config.FocusSetupStatus()
		m.root.StatusBar.SetMessage(tuiText(m.uiLang(), "slashOpenStatus"))
		return nil

	case cmdName == "/llm":
		m.root.SetTab(views.TabConfig)
		m.root.Config.FocusLLMConfig()
		m.root.StatusBar.SetMessage(tuiText(m.uiLang(), "slashOpenConfig"))
		return nil

	case cmdName == "/security" || cmdName == "/policy":
		m.root.SetTab(views.TabConfig)
		m.root.Config.FocusSecurityConfig()
		m.root.StatusBar.SetMessage(tuiText(m.uiLang(), "slashOpenConfig"))
		return nil

	case cmdName == "/memory":
		m.root.Chat.AppendSystemMessage(tuiText(m.uiLang(), "memoryTUISimplified"))
		return nil

	case cmdName == "/help":
		m.root.Help.Show()
		m.root.StatusBar.SetMessage(tuiText(m.uiLang(), "slashOpenHelp"))
		return nil

	default:
		m.root.Chat.AppendSystemMessage(tuiFormat(m.uiLang(), "unknownCommand", text))
		return nil
	}
}

func memoryCategorySummary(categories map[string]int) string {
	if len(categories) == 0 {
		return "n/a"
	}
	names := make([]string, 0, len(categories))
	for name, count := range categories {
		if name == "" {
			name = "default"
		}
		names = append(names, fmt.Sprintf("%s:%d", name, count))
	}
	sort.Strings(names)
	if len(names) > 4 {
		names = append(names[:4], fmt.Sprintf("+%d", len(names)-4))
	}
	return strings.Join(names, ", ")
}

func (m *tuiModel) startMCPAddModeFromArgs(args []string) bool {
	switch mcpAddModeFromArgs(args) {
	case "local":
		m.root.Tools.StartMCPLocalTemplate()
		return true
	case "remote":
		m.root.Tools.StartMCPRemoteTemplate()
		return true
	default:
		return false
	}
}

func (m *tuiModel) startMCPDefaultAddModeFromArgs(args []string) bool {
	switch mcpDefaultAddModeFromArgs(args) {
	case mcpAddModeAutoLocal:
		m.root.Tools.FocusMCP()
		return false
	case "local":
		m.root.Tools.StartMCPLocalTemplate()
		return true
	case "remote":
		m.root.Tools.StartMCPRemoteTemplate()
		return true
	default:
		return false
	}
}

func (m *tuiModel) llmMissing() bool {
	if m == nil || m.app == nil {
		return true
	}
	return !tuiAppLLMReady(m.app)
}

func (m *tuiModel) shouldOpenMCPTemplateShortcut() bool {
	if m == nil || m.app == nil {
		return false
	}
	return tuiAppLLMReady(m.app) && !tuiMCPConfigured(m.app.appConfig)
}

func (m *tuiModel) routeMissingLLMFromChat() tea.Cmd {
	lang := m.uiLang()
	m.root.Chat.AppendSystemMessage(tuiText(lang, "llmNotConfiguredChat"))
	cfg := corelib.AppConfig{}
	if m.app != nil {
		cfg = m.app.appConfig
	}
	if tuiConfigLLMNeedsKey(cfg) {
		m.root.SetTab(views.TabConfig)
		m.root.Config.FocusLLMKey()
		m.root.StatusBar.SetMessage(tuiText(lang, "llmKeyMissing"))
		return nil
	}
	if !tuiSetupReady(cfg, false) || tuiRemoteActivationIncomplete(cfg) {
		m.root.SetTab(views.TabOnboarding)
		if tuiRemoteActivationIncomplete(cfg) {
			m.root.StatusBar.SetMessage(tuiText(lang, "incompleteRemoteSetup"))
		} else {
			m.root.StatusBar.SetMessage(tuiText(lang, "configOpenSetup"))
		}
		return nil
	}
	if tuiRemoteActivationReady(cfg) {
		m.root.SetTab(views.TabServiceRedeem)
		m.root.StatusBar.SetMessage(tuiText(lang, "serviceRedeemPrompt"))
		return m.refreshServiceStatusFromTUI()
	}
	m.root.SetTab(views.TabConfig)
	m.root.Config.FocusLLMConfig()
	m.root.StatusBar.SetMessage(tuiText(lang, "onboardingNeedConfig"))
	return nil
}

func (m *tuiModel) View() string {
	if !m.ready {
		return tuiText(m.uiLang(), "initializing")
	}
	return m.root.View()
}

// handleChatSend runs the agent loop in a goroutine and streams results back.
func (m *tuiModel) handleChatSend(text string) tea.Cmd {
	prog := m.program
	app := m.app
	lang := m.uiLang()

	// --- /btw side query: independent agent loop ---
	trimmedText := strings.TrimSpace(text)
	if !tuiAppLLMReady(app) {
		return func() tea.Msg {
			return views.ChatResponseMsg{Error: tuiText(lang, "llmNotConfiguredChat")}
		}
	}
	if trimmedText == "/btw" || strings.HasPrefix(trimmedText, "/btw ") {
		btwQuery := ""
		if len(trimmedText) > 4 {
			btwQuery = strings.TrimSpace(trimmedText[4:])
		}
		if btwQuery == "" {
			return func() tea.Msg {
				return views.ChatResponseMsg{Text: tuiText(lang, "btwUsage")}
			}
		}
		cb := newTuiBtwCallbacks(app, prog)
		m.activeCb = cb
		return func() tea.Msg {
			result := agent.RunLoop(cb, btwQuery, nil, nil)

			responseText := result.Text
			if responseText == "" && result.Error != "" {
				return views.ChatResponseMsg{Error: tuiFormat(lang, "btwFailed", result.Error)}
			}
			if responseText == "" {
				responseText = tuiText(lang, "btwNoInfo")
			}
			responseText = tuiFormat(lang, "btwHeader", responseText)

			// NOTE: /btw results are NOT appended to the main conversation
			// history. The result is displayed in the chat UI via ChatResponseMsg
			// but does not become part of the LLM's context for future turns.
			// This is by design — /btw is a side query, not part of the main task.

			return views.ChatResponseMsg{Text: responseText}
		}
	}

	cb := newTuiCallbacks(app, prog)
	m.activeCb = cb

	return func() tea.Msg {
		// --- Workflow engine interception (Fix #6) ---
		// Check if the message should be handled by the workflow engine
		// before running the agent loop. This provides the same structured
		// workflow experience as the GUI (19 templates with phase-by-phase
		// execution).
		if wfResp := app.handleWorkflowInterception(text); wfResp != "" {
			// Save both the user message and workflow response to history
			// so the agent loop has context when it runs for phase generation.
			history := app.history.Load("tui-user")
			history = append(history,
				agent.ConversationEntry{Role: "user", Content: text},
				agent.ConversationEntry{Role: "assistant", Content: wfResp},
			)
			app.history.Save("tui-user", history)
			return views.ChatResponseMsg{Text: wfResp}
		}

		history := app.history.Load("tui-user")

		// Inject workflow phase prompt if the workflow engine requested
		// an agent loop run (e.g., to generate a phase document).
		app.workflowMu.Lock()
		phasePrompt := app.pendingPhasePrompt
		wasWorkflowLoop := app.workflowAgentLoop
		app.pendingPhasePrompt = ""
		app.workflowAgentLoop = false
		app.workflowMu.Unlock()

		if wasWorkflowLoop && phasePrompt != "" {
			cb.phasePromptOverride = phasePrompt
		}

		result := agent.RunLoop(cb, text, history, nil)

		// Save conversation history (persisted to disk).
		history = append(history, agent.ConversationEntry{Role: "user", Content: text})
		if result.Text != "" {
			history = append(history, agent.ConversationEntry{Role: "assistant", Content: result.Text})

			// --- Workflow doc capture ---
			// When the agent loop ran on behalf of the workflow engine,
			// save the output as the phase document.
			if cb.phasePromptOverride != "" {
				if engine := app.workflowEngine; engine != nil {
					if phaseID := engine.SavePhaseOutput("tui-user", result.Text); phaseID != "" {
						log.Printf("[TUI-workflow] saved phase output: phase=%s len=%d", phaseID, len(result.Text))
					}
				}
			}
		}
		app.history.Save("tui-user", history)

		// --- Online incremental extraction (Mem0-style) ---
		// Trigger asynchronously after each agent loop to extract salient
		// facts from the conversation and integrate them into long-term memory.
		if app.memoryStore != nil && len(history) >= 4 {
			if oe := app.memoryStore.OnlineExtractor(); oe != nil {
				go func() {
					msgs := convertTUIHistoryToMessages(history, 10)
					if len(msgs) < 2 {
						return
					}
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					oeResult := oe.ExtractAndIntegrate(ctx, msgs, "", time.Now(), "tui-user")
					if oeResult.Added > 0 || oeResult.Updated > 0 || oeResult.Deleted > 0 {
						log.Printf("[online_extraction] tui: extracted=%d added=%d updated=%d deleted=%d",
							oeResult.ExtractedFacts, oeResult.Added, oeResult.Updated, oeResult.Deleted)
					}
				}()
			}
		}

		if result.Error != "" {
			return views.ChatResponseMsg{Error: result.Error}
		}
		return views.ChatResponseMsg{Text: result.Text}
	}
}

// --- Skill / MCP async handlers ---

func (m *tuiModel) searchSkills(query string) tea.Cmd {
	lang := m.uiLang()
	return func() tea.Msg {
		hubURL := m.app.appConfig.SkillHubBaseURL(remote.DefaultRemoteHubCenterURL)

		// Multi-node failover using shared infrastructure from tui/commands.
		// This is the single source of truth for failover logic.
		hubURL = commands.ResolveHubCenterWithFailover(m.app.appConfig, hubURL, nil, nil)

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		client := skill.DefaultHubClient()
		hubResults := client.SearchAll(ctx, hubURL, query)

		if len(hubResults) == 0 {
			return views.ToolSkillSearchResultMsg{Error: tuiText(lang, "skillNoMatch")}
		}

		results := make([]views.SkillSearchResult, 0, len(hubResults))
		for _, r := range hubResults {
			results = append(results, views.SkillSearchResult{
				ID:         r.ID,
				Name:       r.Name,
				Version:    r.Version,
				Rating:     r.AvgRating,
				Downloads:  r.Downloads,
				Trust:      r.TrustLevel,
				Source:     r.Source,
				InstallRef: r.InstallRef,
			})
		}
		return views.ToolSkillSearchResultMsg{Results: results}
	}
}

func (m *tuiModel) installSkill(skillID, hubURL string, source string, installRef string) tea.Cmd {
	lang := m.uiLang()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		client := skill.DefaultHubClient()

		var entry *corelib.NLSkillEntry
		var err error

		switch source {
		case "clawhub":
			// Check if already installed.
			for _, s := range m.app.appConfig.NLSkills {
				if s.Source == "clawhub" && strings.EqualFold(s.Name, skillID) {
					return views.ToolOperationResultMsg{
						Tab: views.ToolSubSkill, Success: true,
						Message: tuiFormat(lang, "skillAlreadyInstalled", s.Name),
					}
				}
			}
			entry, err = client.DownloadClawHub(ctx, skillID)
		case "github":
			if installRef == "" {
				return views.ToolOperationResultMsg{
					Tab: views.ToolSubSkill, Success: false,
					Message: tuiText(lang, "githubSkillMissingRef"),
				}
			}
			entry, err = client.DownloadGitHub(ctx, installRef)
		default:
			if hubURL == "" {
				hubURL = m.app.appConfig.SkillHubBaseURL(remote.DefaultRemoteHubCenterURL)
			}
			// Check if already installed.
			for _, s := range m.app.appConfig.NLSkills {
				if s.HubSkillID == skillID {
					return views.ToolOperationResultMsg{
						Tab: views.ToolSubSkill, Success: true,
						Message: tuiFormat(lang, "skillAlreadyInstalled", s.Name),
					}
				}
			}
			entry, err = client.DownloadSkillHub(ctx, hubURL, skillID)
		}

		if err != nil {
			return views.ToolOperationResultMsg{
				Tab: views.ToolSubSkill, Success: false,
				Message: err.Error(),
			}
		}

		store := commands.NewFileConfigStore(commands.ResolveDataDir())
		cfg, err := store.LoadConfig()
		if err != nil {
			return views.ToolOperationResultMsg{
				Tab: views.ToolSubSkill, Success: false,
				Message: tuiFormat(lang, "configLoadFailed", err.Error()),
			}
		}
		cfg.NLSkills = append(cfg.NLSkills, *entry)
		if err := store.SaveConfig(cfg); err != nil {
			return views.ToolOperationResultMsg{
				Tab: views.ToolSubSkill, Success: false,
				Message: tuiFormat(lang, "saveFailed", err.Error()),
			}
		}
		m.app.appConfig = cfg

		sourceLabel := "SkillHub"
		switch source {
		case "clawhub":
			sourceLabel = "ClawHub"
		case "github":
			sourceLabel = "GitHub"
		}
		return views.ToolOperationResultMsg{
			Tab: views.ToolSubSkill, Success: true,
			Message: tuiFormat(lang, "installedFrom", entry.Name, sourceLabel),
		}
	}
}

func (m *tuiModel) addLocalMCP(entry corelib.LocalMCPServerEntry) tea.Cmd {
	lang := m.uiLang()
	return func() tea.Msg {
		entry.ID = fmt.Sprintf("local_%s_%d", strings.ReplaceAll(entry.Name, " ", "_"), time.Now().Unix())
		entry.CreatedAt = time.Now().Format(time.RFC3339)

		dataDir := commands.ResolveDataDir()
		store := commands.NewFileConfigStore(dataDir)
		cfg, err := store.LoadConfig()
		if err != nil {
			return views.ToolOperationResultMsg{Tab: views.ToolSubMCP, Success: false, Message: tuiFormat(lang, "configLoadFailed", err.Error())}
		}
		cfg.LocalMCPServers = append(cfg.LocalMCPServers, entry)
		if err := store.SaveConfig(cfg); err != nil {
			return views.ToolOperationResultMsg{Tab: views.ToolSubMCP, Success: false, Message: tuiFormat(lang, "configSaveFailedPlain", err.Error())}
		}
		// Return success — the main loop will refresh data via ToolRefreshMsg.
		return views.ToolOperationResultMsg{Tab: views.ToolSubMCP, Success: true, Message: tuiFormat(lang, "added", entry.Name)}
	}
}

func (m *tuiModel) addRemoteMCP(entry corelib.MCPServerEntry) tea.Cmd {
	lang := m.uiLang()
	return func() tea.Msg {
		entry.ID = fmt.Sprintf("remote_%s_%d", strings.ReplaceAll(entry.Name, " ", "_"), time.Now().Unix())
		entry.CreatedAt = time.Now().Format(time.RFC3339)

		dataDir := commands.ResolveDataDir()
		store := commands.NewFileConfigStore(dataDir)
		cfg, err := store.LoadConfig()
		if err != nil {
			return views.ToolOperationResultMsg{Tab: views.ToolSubMCP, Success: false, Message: tuiFormat(lang, "configLoadFailed", err.Error())}
		}
		cfg.MCPServers = append(cfg.MCPServers, entry)
		if err := store.SaveConfig(cfg); err != nil {
			return views.ToolOperationResultMsg{Tab: views.ToolSubMCP, Success: false, Message: tuiFormat(lang, "configSaveFailedPlain", err.Error())}
		}
		return views.ToolOperationResultMsg{Tab: views.ToolSubMCP, Success: true, Message: tuiFormat(lang, "added", entry.Name)}
	}
}

func (m *tuiModel) refreshToolData() {
	cfg := m.app.appConfig

	// Populate skills from config.
	var skills []views.SkillItem
	for _, sk := range cfg.NLSkills {
		skills = append(skills, views.SkillItem{
			Name:        sk.Name,
			Description: sk.Description,
			Status:      sk.Status,
			Source:      sk.Source,
			Publisher:   sk.Publisher,
		})
	}
	m.root.Tools.SetSkills(skills)

	// Populate MCP servers from config.
	var mcpServers []views.MCPItem
	for _, srv := range cfg.LocalMCPServers {
		status := "stopped"
		if !srv.Disabled {
			status = "ready"
		}
		mcpServers = append(mcpServers, views.MCPItem{
			ID:       srv.ID,
			Name:     srv.Name,
			Type:     "local",
			Status:   status,
			Endpoint: srv.Command + " " + strings.Join(srv.Args, " "),
		})
	}
	for _, srv := range cfg.MCPServers {
		mcpServers = append(mcpServers, views.MCPItem{
			ID:       srv.ID,
			Name:     srv.Name,
			Type:     "remote",
			Status:   "ready",
			Endpoint: srv.EndpointURL,
		})
	}
	m.root.Tools.SetMCPServers(mcpServers)
}

func (m *tuiModel) reloadConfigBackedViews() {
	if m == nil || m.app == nil {
		return
	}
	store := commands.NewFileConfigStore(commands.ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return
	}
	m.app.appConfig = cfg
	m.app.llmConfig = buildLLMConfigFromAppConfig(cfg)
	m.root.Config.LoadFromAppConfig(cfg)
	m.root.Onboarding.LoadFromAppConfig(cfg)
	m.root.Service.LoadFromAppConfig(cfg)
	m.refreshStatusBarModelInfo()
}

func (m *tuiModel) saveConfig(msg views.ConfigSaveMsg) tea.Cmd {
	return func() tea.Msg {
		dataDir := commands.ResolveDataDir()
		store := commands.NewFileConfigStore(dataDir)
		cfg, err := store.LoadConfig()
		if err != nil {
			return views.ConfigSaveFailedMsg{Key: msg.Key, Error: err.Error()}
		}
		if msg.HasConfig {
			cfg = msg.Config
		} else {
			applyConfigValue(&cfg, msg.Key, msg.Value)
		}
		if err := store.SaveConfig(cfg); err != nil {
			return views.ConfigSaveFailedMsg{Key: msg.Key, Error: err.Error()}
		}
		return views.ConfigSavedMsg{Key: msg.Key, Value: msg.Value}
	}
}

func (m *tuiModel) activateRemoteFromTUI(email, hubCenterURL string) tea.Cmd {
	lang := m.uiLang()
	return func() tea.Msg {
		store := commands.NewFileConfigStore(commands.ResolveDataDir())
		cfg, err := store.LoadConfig()
		if err != nil {
			return views.OnboardingRemoteResultMsg{Success: false, Message: tuiFormat(lang, "loadConfigFailed", err.Error())}
		}
		hubCenterURL = strings.TrimRight(strings.TrimSpace(hubCenterURL), "/")
		if hubCenterURL != "" && hubCenterURL != strings.TrimRight(strings.TrimSpace(cfg.RemoteHubCenterURL), "/") {
			cfg.RemoteHubCenterURL = hubCenterURL
			if err := store.SaveConfig(cfg); err != nil {
				return views.OnboardingRemoteResultMsg{Success: false, Message: tuiFormat(lang, "saveHubCenterFailed", err.Error())}
			}
		}

		profile := remote.BuildMachineProfile(version)
		profile.Email = strings.TrimSpace(email)
		profile.ClientID = cfg.RemoteClientID
		// Hub URL is display-only in the TUI. Registration resolves the actual
		// Hub from HubCenter + email so users do not need to guess or type it.
		profile.HubURL = ""
		profile.HubCenterURL = strings.TrimSpace(cfg.RemoteHubCenterURL)
		profile.HubCenterURLs = cfg.HubCenterBaseURLs(remote.DefaultRemoteHubCenterURL, remote.DefaultRemoteHubCenterURLs)
		result, err := remote.NewEnrollmentClient().Enroll(context.Background(), profile)
		if err != nil {
			return views.OnboardingRemoteResultMsg{Success: false, Message: err.Error()}
		}
		cfg.RemoteEmail = result.Email
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
		if result.HubCenterURL != "" {
			cfg.RemoteHubCenterURL = result.HubCenterURL
		}
		if len(result.DiscoveredURLs) > 0 {
			cfg.RemoteHubCenterURLs = remote.NormalizeHubCenterURLs(result.DiscoveredURLs)
		}
		if err := store.SaveConfig(cfg); err != nil {
			return views.OnboardingRemoteResultMsg{Success: false, Message: tuiFormat(lang, "saveConfigFailed", err.Error())}
		}
		m.app.appConfig = cfg
		hubServiceReady := strings.TrimSpace(cfg.RemoteHubURL) != "" && strings.TrimSpace(cfg.RemoteViewerToken) != ""
		machineReady := strings.TrimSpace(cfg.RemoteMachineID) != "" &&
			strings.TrimSpace(cfg.RemoteMachineToken) != "" &&
			strings.TrimSpace(cfg.RemoteViewerToken) != ""
		message := tuiText(lang, "activated")
		if !hubServiceReady && !machineReady {
			message = tuiText(lang, "viewerTokenMissing")
			if strings.TrimSpace(cfg.RemoteHubURL) == "" {
				message = tuiText(lang, "hubURLMissing")
			}
		}
		return views.OnboardingRemoteResultMsg{
			Success:         true,
			Message:         message,
			HubURL:          result.HubURL,
			MachineID:       result.MachineID,
			HubServiceReady: hubServiceReady,
			MachineReady:    machineReady,
		}
	}
}

func (m *tuiModel) startWeixinFromTUI() tea.Cmd {
	return func() tea.Msg {
		cfg := m.app.appConfig
		baseURL := strings.TrimSpace(cfg.WeixinBaseURL)
		if baseURL == "" {
			baseURL = weixin.DefaultBaseURL
		}
		qr, token, err := weixin.StartQRLogin(context.Background(), baseURL, weixin.DefaultBotType)
		if err != nil {
			return views.OnboardingWeixinQRMsg{Success: false, Message: err.Error()}
		}
		return views.OnboardingWeixinQRMsg{Success: true, QR: qr, Token: token}
	}
}

func (m *tuiModel) pollWeixinFromTUI(token string) tea.Cmd {
	lang := m.uiLang()
	return func() tea.Msg {
		time.Sleep(1 * time.Second)
		cfg := m.app.appConfig
		baseURL := strings.TrimSpace(cfg.WeixinBaseURL)
		if baseURL == "" {
			baseURL = weixin.DefaultBaseURL
		}
		result, status, err := weixin.PollQRStatus(context.Background(), baseURL, token)
		if err != nil {
			return views.OnboardingWeixinPollResultMsg{Status: "error", Message: err.Error(), Completed: true}
		}
		msg := status.String()
		if result != nil && result.Message != "" {
			msg = result.Message
		}
		if status == weixin.QRLoginStatusConfirmed {
			if result == nil || !result.Connected {
				return views.OnboardingWeixinPollResultMsg{Status: status.String(), Message: msg, Completed: true}
			}
			store := commands.NewFileConfigStore(commands.ResolveDataDir())
			cfg, _ := store.LoadConfig()
			cfg.WeixinEnabled = true
			cfg.WeixinToken = result.BotToken
			cfg.WeixinAccountID = result.AccountID
			if result.BaseURL != "" {
				cfg.WeixinBaseURL = result.BaseURL
			}
			if cfg.WeixinLocalMode == nil {
				local := true
				cfg.WeixinLocalMode = &local
			}
			if err := store.SaveConfig(cfg); err != nil {
				return views.OnboardingWeixinPollResultMsg{Status: status.String(), Message: tuiFormat(lang, "saveConfigFailed", err.Error()), Completed: true}
			}
			m.app.appConfig = cfg
			return views.OnboardingWeixinPollResultMsg{Status: status.String(), Message: tuiText(lang, "weixinBoundShort"), Success: true, Completed: true, AccountID: result.AccountID}
		}
		if status == weixin.QRLoginStatusExpired {
			return views.OnboardingWeixinPollResultMsg{Status: status.String(), Message: msg, Completed: true}
		}
		return views.OnboardingWeixinPollResultMsg{Status: status.String(), Message: msg}
	}
}

func (m *tuiModel) finishOnboardingFromTUI() tea.Cmd {
	lang := m.uiLang()
	return func() tea.Msg {
		store := commands.NewFileConfigStore(commands.ResolveDataDir())
		cfg, err := store.LoadConfig()
		if err != nil {
			return views.ConfigSaveFailedMsg{Key: "onboarding", Error: tuiFormat(lang, "loadConfigFailed", err.Error())}
		}
		cfg.OnboardingDone = true
		if err := store.SaveConfig(cfg); err != nil {
			return views.ConfigSaveFailedMsg{Key: "onboarding", Error: err.Error()}
		}
		m.app.appConfig = cfg
		return views.ConfigSavedMsg{Key: "onboarding", Value: "done"}
	}
}

func (m *tuiModel) saveOnboardingLanguage(lang string) tea.Cmd {
	lang = tuiConfigLang(corelib.AppConfig{Language: lang})
	m.root.SetLang(lang)
	m.refreshStatusBarModelInfo()
	return func() tea.Msg {
		store := commands.NewFileConfigStore(commands.ResolveDataDir())
		cfg, err := store.LoadConfig()
		if err != nil {
			return views.ConfigSaveFailedMsg{Key: "language", Error: err.Error()}
		}
		cfg.Language = lang
		if err := store.SaveConfig(cfg); err != nil {
			return views.ConfigSaveFailedMsg{Key: "language", Error: err.Error()}
		}
		m.app.appConfig = cfg
		return views.ConfigSavedMsg{Key: "language", Value: lang}
	}
}

const (
	tuiHubServiceProviderName = "MaClaw官方"
	tuiHubServiceAutoModel    = "auto"
)

type tuiHubLLMServiceStatus struct {
	Active             bool    `json:"active"`
	HubLLMBaseURL      string  `json:"hub_llm_base_url"`
	DefaultModel       string  `json:"default_model,omitempty"`
	CreditsRemaining   float64 `json:"credits_remaining,omitempty"`
	CreditsAvailable   float64 `json:"credits_available,omitempty"`
	EffectiveExpiresAt string  `json:"effective_expires_at,omitempty"`
	NearestExpiresAt   string  `json:"nearest_expires_at,omitempty"`
}

type tuiHubLLMServiceRedeemResponse struct {
	Success       bool                   `json:"success"`
	ServiceStatus tuiHubLLMServiceStatus `json:"service_status"`
}

func (m *tuiModel) refreshServiceStatusFromTUI() tea.Cmd {
	lang := m.uiLang()
	return func() tea.Msg {
		store := commands.NewFileConfigStore(commands.ResolveDataDir())
		cfg, err := store.LoadConfig()
		if err != nil {
			return views.ServiceRedeemResultMsg{Success: false, Message: tuiFormat(lang, "loadConfigFailed", err.Error()), FromRefresh: true}
		}
		if strings.TrimSpace(cfg.RemoteHubURL) == "" {
			return views.ServiceRedeemResultMsg{Success: false, Message: tuiText(lang, "hubURLMissing"), FromRefresh: true}
		}
		if strings.TrimSpace(cfg.RemoteViewerToken) == "" {
			return views.ServiceRedeemResultMsg{Success: false, Message: tuiText(lang, "viewerTokenMissing"), FromRefresh: true}
		}
		req, err := http.NewRequest(http.MethodGet, strings.TrimRight(cfg.RemoteHubURL, "/")+"/api/llm/service/status", nil)
		if err != nil {
			return views.ServiceRedeemResultMsg{Success: false, Message: err.Error(), FromRefresh: true}
		}
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.RemoteViewerToken))
		client := &http.Client{Timeout: 20 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return views.ServiceRedeemResultMsg{Success: false, Message: err.Error(), FromRefresh: true}
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			var failure map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&failure); err == nil {
				if msg, _ := failure["message"].(string); strings.TrimSpace(msg) != "" {
					return views.ServiceRedeemResultMsg{Success: false, Message: msg, FromRefresh: true}
				}
			}
			return views.ServiceRedeemResultMsg{Success: false, Message: tuiFormat(lang, "serviceStatusFailed", resp.Status), FromRefresh: true}
		}
		var status tuiHubLLMServiceStatus
		if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
			return views.ServiceRedeemResultMsg{Success: false, Message: err.Error(), FromRefresh: true}
		}
		if !status.Active || strings.TrimSpace(status.HubLLMBaseURL) == "" {
			return views.ServiceRedeemResultMsg{Success: false, Message: tuiText(lang, "officialServiceMissing"), FromRefresh: true}
		}
		applyTUIHubLLMServiceStatusToConfig(&cfg, status)
		if err := store.SaveConfig(cfg); err != nil {
			return views.ServiceRedeemResultMsg{Success: false, Message: tuiFormat(lang, "saveConfigFailed", err.Error()), FromRefresh: true}
		}
		expires, credits := serviceStatusExpiryAndCredits(status)
		return views.ServiceRedeemResultMsg{Success: true, Message: tuiText(lang, "officialServiceReady"), ProviderName: tuiHubServiceProviderName, CreditsRemaining: credits, ExpiresAt: expires, FromRefresh: true, Config: cfg, HasConfig: true}
	}
}

func (m *tuiModel) redeemServiceFromTUI(code string) tea.Cmd {
	lang := m.uiLang()
	return func() tea.Msg {
		store := commands.NewFileConfigStore(commands.ResolveDataDir())
		cfg, err := store.LoadConfig()
		if err != nil {
			return views.ServiceRedeemResultMsg{Success: false, Message: tuiFormat(lang, "loadConfigFailed", err.Error())}
		}
		if strings.TrimSpace(cfg.RemoteHubURL) == "" {
			return views.ServiceRedeemResultMsg{Success: false, Message: tuiText(lang, "hubURLMissing")}
		}
		if strings.TrimSpace(cfg.RemoteViewerToken) == "" {
			return views.ServiceRedeemResultMsg{Success: false, Message: tuiText(lang, "viewerTokenMissing")}
		}
		payload, _ := json.Marshal(map[string]string{"code": strings.TrimSpace(code)})
		req, err := http.NewRequest(http.MethodPost, strings.TrimRight(cfg.RemoteHubURL, "/")+"/api/llm/service/redeem", bytes.NewReader(payload))
		if err != nil {
			return views.ServiceRedeemResultMsg{Success: false, Message: err.Error()}
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.RemoteViewerToken))
		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return views.ServiceRedeemResultMsg{Success: false, Message: err.Error()}
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			var failure map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&failure); err == nil {
				if msg, _ := failure["message"].(string); strings.TrimSpace(msg) != "" {
					return views.ServiceRedeemResultMsg{Success: false, Message: msg}
				}
			}
			return views.ServiceRedeemResultMsg{Success: false, Message: tuiFormat(lang, "redeemFailed", resp.Status)}
		}
		var result tuiHubLLMServiceRedeemResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return views.ServiceRedeemResultMsg{Success: false, Message: err.Error()}
		}
		if !result.ServiceStatus.Active || strings.TrimSpace(result.ServiceStatus.HubLLMBaseURL) == "" {
			return views.ServiceRedeemResultMsg{Success: false, Message: tuiText(lang, "redeemNoActiveService")}
		}
		applyTUIHubLLMServiceStatusToConfig(&cfg, result.ServiceStatus)
		if err := store.SaveConfig(cfg); err != nil {
			return views.ServiceRedeemResultMsg{Success: false, Message: tuiFormat(lang, "saveConfigFailed", err.Error())}
		}
		expires, credits := serviceStatusExpiryAndCredits(result.ServiceStatus)
		return views.ServiceRedeemResultMsg{Success: true, Message: tuiText(lang, "redeemSuccessStatus"), ProviderName: tuiHubServiceProviderName, CreditsRemaining: credits, ExpiresAt: expires, Config: cfg, HasConfig: true}
	}
}

func serviceStatusExpiryAndCredits(status tuiHubLLMServiceStatus) (string, float64) {
	expires := status.EffectiveExpiresAt
	if expires == "" {
		expires = status.NearestExpiresAt
	}
	credits := status.CreditsRemaining
	if credits == 0 {
		credits = status.CreditsAvailable
	}
	return expires, credits
}

func applyTUIHubLLMServiceStatusToConfig(cfg *corelib.AppConfig, status tuiHubLLMServiceStatus) {
	model := strings.TrimSpace(status.DefaultModel)
	if model == "" {
		model = tuiHubServiceAutoModel
	}
	provider := corelib.MaclawLLMProvider{
		Name:          tuiHubServiceProviderName,
		IsHubService:  true,
		URL:           strings.TrimRight(strings.TrimSpace(status.HubLLMBaseURL), "/"),
		Key:           strings.TrimSpace(cfg.RemoteViewerToken),
		Model:         model,
		Protocol:      "openai",
		ContextLength: corelib.DefaultContextTokens,
		TimeoutSec:    corelib.DefaultLLMTimeoutSec,
		AgentType:     "openclaw",
	}
	providers := make([]corelib.MaclawLLMProvider, 0, len(cfg.MaclawLLMProviders)+1)
	for i := range cfg.MaclawLLMProviders {
		if cfg.MaclawLLMProviders[i].Name != tuiHubServiceProviderName {
			providers = append(providers, cfg.MaclawLLMProviders[i])
		}
	}
	cfg.MaclawLLMProviders = append([]corelib.MaclawLLMProvider{provider}, providers...)
	cfg.MaclawLLMCurrentProvider = tuiHubServiceProviderName
	cfg.MaclawLLMUrl = provider.URL
	cfg.MaclawLLMKey = provider.Key
	cfg.MaclawLLMModel = provider.Model
	cfg.MaclawLLMProtocol = provider.Protocol
	cfg.MaclawLLMContextLength = provider.ContextLength
	cfg.MaclawLLMTimeoutSec = provider.TimeoutSec
}

// applyConfigValue sets a single config field by key name.
// Delegates to the single source of truth in config_fields.go.
func applyConfigValue(cfg *corelib.AppConfig, key, value string) {
	views.ApplyConfigValue(cfg, key, value)
}

// tuiCallbacks implements agent.LoopCallbacks for the TUI.
type tuiCallbacks struct {
	app      *TUIApp
	program  *tea.Program
	stopped  bool
	cancelCh chan struct{} // closed by Esc key to cancel the running loop

	// phasePromptOverride is set when the workflow engine wants the agent
	// loop to generate a phase document. It's appended to the system prompt.
	phasePromptOverride string
}

func newTuiCallbacks(app *TUIApp, prog *tea.Program) *tuiCallbacks {
	return &tuiCallbacks{
		app:      app,
		program:  prog,
		cancelCh: make(chan struct{}),
	}
}

func (c *tuiCallbacks) Cancel() {
	select {
	case <-c.cancelCh:
	default:
		close(c.cancelCh)
		c.stopped = true
	}
}

func (c *tuiCallbacks) GetLLMConfig() corelib.MaclawLLMConfig {
	return c.app.llmConfig
}

func (c *tuiCallbacks) GetMaxIterations() int {
	return config.EffectiveMaxIterations(c.app.appConfig.MaclawAgentMaxIterations)
}

func (c *tuiCallbacks) BuildSystemPrompt(userText string, isFirstTurn bool) string {
	deps := c.app.buildSystemPromptDeps()
	prompt := agent.BuildSystemPrompt(deps, userText, isFirstTurn)

	// Inject workflow phase prompt when the agent loop runs on behalf of
	// the workflow engine (e.g., generating a requirements document).
	if c.phasePromptOverride != "" {
		prompt += "\n" + c.phasePromptOverride
	}

	return prompt
}

func (c *tuiCallbacks) BuildTools(userText string) []map[string]interface{} {
	return c.app.toolRegistry.BuildDefinitions()
}

func (c *tuiCallbacks) ExecuteTool(name, argsJSON string) string {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return tuiFormat(tuiConfigLang(c.app.appConfig), "toolArgParseFailed", err.Error())
	}
	start := time.Now()
	result := c.app.toolRegistry.Execute(name, args)
	elapsed := time.Since(start)

	// Update the tool status message with completion + optional elapsed time.
	if c.program != nil {
		content := ""
		if elapsed > 500*time.Millisecond {
			content = fmt.Sprintf("%.1fs", elapsed.Seconds())
		}
		c.program.Send(views.ChatStreamMsg{Type: "tool_result", Tool: name, Content: content})
	}
	return result
}

func (c *tuiCallbacks) IsToolAllowed(name string) bool {
	return c.app.isWorkflowToolAllowedTUI(name)
}

func (c *tuiCallbacks) IsToolCallAllowed(name, argsJSON string) (bool, string) {
	var args map[string]interface{}
	if strings.TrimSpace(argsJSON) != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return false, tuiFormat(tuiConfigLang(c.app.appConfig), "toolArgParseFailed", err.Error())
		}
	}
	var approved []workflow.OpsApprovedCommand
	if engine := c.app.getWorkflowEngine(); engine != nil {
		approved = engine.GetOpsApprovedCommands("tui-user")
	}
	if err := workflow.ValidateToolCallByPolicyWithApproval(c.app.currentWorkflowToolFilterTUI(), strings.TrimSpace(name), args, approved); err != nil {
		return false, err.Error()
	}
	return true, ""
}

func (app *TUIApp) isWorkflowToolAllowedTUI(name string) bool {
	return workflow.IsToolAllowedByPolicy(app.currentWorkflowToolFilterTUI(), name)
}

func (app *TUIApp) currentWorkflowToolFilterTUI() workflow.ToolFilterPolicy {
	if app == nil {
		return workflow.ToolFilterNone
	}
	engine := app.getWorkflowEngine()
	if engine == nil {
		return workflow.ToolFilterNone
	}
	return engine.GetPhaseToolFilter("tui-user")
}

func (c *tuiCallbacks) OnToken(delta string) {
	if c.program != nil {
		c.program.Send(views.ChatStreamMsg{Type: "text_delta", Content: delta})
	}
}

func (c *tuiCallbacks) OnProgress(text string) {
	if c.program != nil {
		c.program.Send(views.ChatStreamMsg{Type: "thinking", Content: text})
	}
}

func (c *tuiCallbacks) OnToolCall(name string) {
	if c.program != nil {
		c.program.Send(views.ChatStreamMsg{Type: "tool_call", Tool: name, Content: name})
	}
}

func (c *tuiCallbacks) OnToolResult(name string) {
	// No-op: elapsed time is already sent by ExecuteTool when > 500ms.
	// RunLoop calls OnToolResult after ExecuteTool; sending a second
	// ChatStreamMsg here would overwrite the elapsed time display.
}

func (c *tuiCallbacks) ShouldStop() bool {
	select {
	case <-c.cancelCh:
		return true
	default:
		return c.stopped
	}
}

// ---------------------------------------------------------------------------
// tuiBtwCallbacks implements agent.LoopCallbacks for /btw side queries.
// Minimal tool set: web_search, web_fetch, read_file, memory (recall only).
// Independent conversation — does not pollute the main chat history.
// ---------------------------------------------------------------------------

var tuiBtwToolNames = map[string]bool{
	"web_search":   true,
	"web_fetch":    true,
	"read_file":    true,
	"memory":       true,
	"agent_status": true,
}

type tuiBtwCallbacks struct {
	app      *TUIApp
	program  *tea.Program
	stopped  bool
	cancelCh chan struct{}

	cachedTools []map[string]interface{}
}

func newTuiBtwCallbacks(app *TUIApp, prog *tea.Program) *tuiBtwCallbacks {
	return &tuiBtwCallbacks{
		app:      app,
		program:  prog,
		cancelCh: make(chan struct{}),
	}
}

func (c *tuiBtwCallbacks) Cancel() {
	select {
	case <-c.cancelCh:
	default:
		close(c.cancelCh)
		c.stopped = true
	}
}

func (c *tuiBtwCallbacks) GetLLMConfig() corelib.MaclawLLMConfig {
	return c.app.llmConfig
}

func (c *tuiBtwCallbacks) GetMaxIterations() int {
	// Use MinAgentIterations (30) — EffectiveMaxIterations enforces a floor
	// of 30. Side queries typically finish in 3-5 iterations.
	return config.EffectiveMaxIterations(config.MinAgentIterations)
}

func (c *tuiBtwCallbacks) BuildSystemPrompt(userText string, isFirstTurn bool) string {
	// Build a focused system prompt for /btw — identity + memory recall only.
	// Does NOT call agent.BuildSystemPrompt (which injects coding workflow,
	// memory management guide, and other multi-turn noise).
	return buildTuiBtwSystemPrompt(c.app, userText)
}

func buildTuiBtwSystemPrompt(app *TUIApp, userText string) string {
	cfg := app.appConfig
	lang := tuiConfigLang(cfg)
	roleName := cfg.MaclawRoleName
	if roleName == "" {
		roleName = brand.Current().DisplayName
	}
	roleDesc := cfg.MaclawRoleDescription
	if roleDesc == "" {
		roleDesc = tuiText(lang, "defaultRoleDescription")
	}

	var b strings.Builder

	// Identity.
	var selfIdentity string
	if app.memoryStore != nil {
		selfIdentity = app.memoryStore.SelfIdentitySummary(600)
	}
	if selfIdentity != "" {
		if lang == "en" {
			fmt.Fprintf(&b, "Your self-understanding (from memory): %s\nYour underlying system name is %s.\n", selfIdentity, roleName)
		} else {
			fmt.Fprintf(&b, "你的自我认知（来自记忆）：%s\n你的底层系统名为 %s。\n", selfIdentity, roleName)
		}
	} else {
		if lang == "en" {
			fmt.Fprintf(&b, "You are %s, %s.\n", roleName, roleDesc)
		} else {
			fmt.Fprintf(&b, "你是 %s，%s。\n", roleName, roleDesc)
		}
	}

	// /btw mode.
	b.WriteString(tuiBtwSuffixForLang(lang))

	// User fact summary.
	if app.memoryStore != nil {
		if summary := app.memoryStore.UserFactSummary(400); summary != "" {
			fmt.Fprintf(&b, tuiBtwSectionFormat(lang, "userInfo"), summary)
		}
	}

	// Proactive memory recall (read-only, no side effects).
	if app.memoryStore != nil && userText != "" {
		recalled := app.memoryStore.RecallDynamic(userText, "", "")
		var relevant []memory.Entry
		for _, e := range recalled {
			cat := memory.MapToCanonical(e.Category)
			if cat == memory.CategoryUserFact || cat == memory.CategorySelfIdentity {
				continue
			}
			if e.Category == memory.CategorySessionCheckpoint || e.Category == memory.CategoryConversationSummary {
				continue
			}
			relevant = append(relevant, e)
		}
		if len(relevant) > 8 {
			relevant = relevant[:8]
		}
		if len(relevant) > 0 {
			b.WriteString(tuiBtwSectionHeader(lang, "relevantMemory"))
			for _, e := range relevant {
				text := e.Content
				if e.CompactForm != "" {
					text = e.CompactForm
				}
				runes := []rune(text)
				if len(runes) > 200 {
					text = string(runes[:200]) + "…"
				}
				fmt.Fprintf(&b, "- [%s] %s\n", e.Category, text)
			}
		}
	}

	return b.String()
}

func (c *tuiBtwCallbacks) BuildTools(userText string) []map[string]interface{} {
	if c.cachedTools == nil {
		c.cachedTools = buildTuiBtwToolDefinitions(c.app)
	}
	return c.cachedTools
}

func (c *tuiBtwCallbacks) ExecuteTool(name, argsJSON string) string {
	if !tuiBtwToolNames[name] {
		return tuiFormat(tuiConfigLang(c.app.appConfig), "unknownBtwTool", name)
	}

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return tuiFormat(tuiConfigLang(c.app.appConfig), "toolArgParseFailed", err.Error())
	}

	// Mechanism-level enforcement: memory tool is recall-only in /btw.
	if name == "memory" {
		action, _ := args["action"].(string)
		if action != "recall" {
			return tuiText(tuiConfigLang(c.app.appConfig), "btwMemoryReadOnly")
		}
	}

	// agent_status: query TUI runtime state directly.
	if name == "agent_status" {
		return c.app.tuiAgentStatus(args)
	}

	// Delegate to the shared tool registry.
	result := c.app.toolRegistry.Execute(name, args)

	if c.program != nil {
		c.program.Send(views.ChatStreamMsg{Type: "tool_result", Tool: name})
	}
	return result
}

func (c *tuiBtwCallbacks) OnToken(delta string) {
	if c.program != nil {
		c.program.Send(views.ChatStreamMsg{Type: "text_delta", Content: delta})
	}
}

func (c *tuiBtwCallbacks) OnProgress(text string) {
	if c.program != nil {
		c.program.Send(views.ChatStreamMsg{Type: "thinking", Content: text})
	}
}

func (c *tuiBtwCallbacks) OnToolCall(name string) {
	if c.program != nil {
		c.program.Send(views.ChatStreamMsg{Type: "tool_call", Tool: name, Content: name})
	}
}

func (c *tuiBtwCallbacks) OnToolResult(name string) {}

func (c *tuiBtwCallbacks) ShouldStop() bool {
	select {
	case <-c.cancelCh:
		return true
	default:
		return c.stopped
	}
}

func tuiBtwSectionFormat(lang, key string) string {
	if lang == "en" {
		if key == "userInfo" {
			return "\n## User Information\n%s\n"
		}
	}
	return "\n## 用户信息\n%s\n"
}

func tuiBtwSectionHeader(lang, key string) string {
	if lang == "en" {
		if key == "relevantMemory" {
			return "\n## Relevant Memory (Auto-recalled)\n"
		}
	}
	return "\n## 相关记忆（自动召回）\n"
}

func tuiBtwSuffixForLang(lang string) string {
	if lang == "en" {
		return tuiBtwSuffixEN
	}
	return tuiBtwSuffix
}

const tuiBtwSuffixEN = `
## /btw Side Query Mode (Current)

You are handling a /btw side query. This is an independent, single-turn quick query, not part of the main task.

Rules:
1. If the user asks about task progress or runtime status, use the agent_status tool first.
2. Use web_search for fresh information, then web_fetch for details.
3. If the question involves local project files, use read_file.
4. If the question involves previous conversation or memory, use memory(action=\"recall\").
5. Keep the answer concise, structured, and direct.
6. Include URLs when citing web sources.
7. This is read-only: do not modify files.
8. Try to finish within 2-3 tool turns.
`

const tuiBtwSuffix = `
## /btw 侧查询模式（当前生效）

你正在处理一个 /btw 侧查询。这是一个独立的单轮快速查询，不是主任务的一部分。

规则：
1. 如果用户询问任务进度、运行状态等问题，优先使用 agent_status 工具查询实际运行时状态
2. 使用 web_search 搜索最新信息，然后用 web_fetch 获取详细内容
3. 如果问题涉及本地项目文件，使用 read_file 查看
4. 如果问题涉及之前的对话或记忆，使用 memory(action="recall") 召回
5. 回答要简洁、结构化，直接给出关键信息
6. 引用网络来源时附上 URL
7. 这是一个只读查询——不要修改任何文件
8. 尽量在 2-3 轮工具调用内完成查询，不要过度搜索
`

// tuiAgentStatusToolDef is the inline tool definition for agent_status in TUI.
// Defined once, used in both the registry path and the fallback path.
var tuiAgentStatusToolDef_ = tuiBtwToolDef("agent_status",
	"查询主 Agent 的运行时状态，包括 SSH 连接等。当用户询问任务进度、后台任务状态时使用此工具。",
	map[string]interface{}{
		"category": map[string]string{"type": "string", "description": "查询类别: all（全部状态）、ssh_sessions（SSH 连接）。默认 all"},
	}, nil)

func buildTuiBtwToolDefinitions(app *TUIApp) []map[string]interface{} {
	// Try to get definitions from the shared tool registry.
	if app != nil && app.toolRegistry != nil {
		allDefs := app.toolRegistry.BuildDefinitions()
		var btwDefs []map[string]interface{}
		for _, def := range allDefs {
			fn, _ := def["function"].(map[string]interface{})
			if fn == nil {
				continue
			}
			name, _ := fn["name"].(string)
			if tuiBtwToolNames[name] && name != "agent_status" {
				btwDefs = append(btwDefs, def)
			}
		}
		// agent_status is always inline-defined (not in the shared registry).
		btwDefs = append(btwDefs, tuiAgentStatusToolDef_)
		if len(btwDefs) > 0 {
			return btwDefs
		}
	}

	// Fallback: inline definitions (same as GUI btw_subagent.go).
	return []map[string]interface{}{
		tuiBtwToolDef("web_search", "搜索互联网获取最新信息",
			map[string]interface{}{
				"query":       map[string]string{"type": "string", "description": "搜索关键词"},
				"max_results": map[string]string{"type": "integer", "description": "最大结果数（默认 8）"},
			}, []string{"query"}),
		tuiBtwToolDef("web_fetch", "抓取指定 URL 的网页内容并提取正文",
			map[string]interface{}{
				"url":       map[string]string{"type": "string", "description": "要抓取的 URL"},
				"max_chars": map[string]string{"type": "integer", "description": "最多返回字符数（可选）"},
			}, []string{"url"}),
		tuiBtwToolDef("read_file", "读取本地文件内容",
			map[string]interface{}{
				"path":   map[string]string{"type": "string", "description": "文件路径"},
				"lines":  map[string]string{"type": "integer", "description": "读取行数（可选）"},
				"offset": map[string]string{"type": "integer", "description": "从末尾倒数行数开始读取（可选）"},
			}, []string{"path"}),
		tuiBtwToolDef("memory", "查询长期记忆",
			map[string]interface{}{
				"action": map[string]string{"type": "string", "description": "操作: recall"},
				"query":  map[string]string{"type": "string", "description": "查询关键词"},
			}, []string{"action"}),
		tuiAgentStatusToolDef_,
	}
}

func tuiBtwToolDef(name, desc string, props map[string]interface{}, required []string) map[string]interface{} {
	return agent.ToolDef(name, desc, props, required)
}

// tuiAgentStatus queries the TUI's runtime state for /btw side queries.
// TUI has fewer runtime components than GUI (no local background tasks,
// no coding sessions), but SSH sessions are available.
func (app *TUIApp) tuiAgentStatus(args map[string]interface{}) string {
	category, _ := args["category"].(string)
	if category == "" {
		category = "all"
	}

	var sections []string

	if category == "all" || category == "ssh_sessions" {
		if app.sshMgr != nil {
			sessions := app.sshMgr.List()
			if len(sessions) > 0 {
				var b strings.Builder
				b.WriteString(tuiText(tuiConfigLang(app.appConfig), "agentStatusSSH") + "\n")
				for _, s := range sessions {
					summary := s.GetSummary()
					fmt.Fprintf(&b, "- %s [%s] %s\n", s.ID, summary.Status, summary.HostLabel)
				}
				sections = append(sections, b.String())
			}
		}
	}

	if len(sections) == 0 {
		return tuiText(tuiConfigLang(app.appConfig), "agentStatusNoActive")
	}

	return strings.Join(sections, "\n\n")
}

// convertTUIHistoryToMessages converts the last N conversation entries to
// the ConversationMessage format expected by OnlineExtractor.
func convertTUIHistoryToMessages(history []agent.ConversationEntry, maxEntries int) []memory.ConversationMessage {
	start := len(history) - maxEntries
	if start < 0 {
		start = 0
	}

	var messages []memory.ConversationMessage
	for _, e := range history[start:] {
		role := e.Role
		if role == "" {
			continue
		}
		content, ok := e.Content.(string)
		if !ok || strings.TrimSpace(content) == "" {
			continue
		}
		// Truncate very long entries.
		if runes := []rune(content); len(runes) > 2000 {
			content = string(runes[:2000]) + "\n[...truncated...]"
		}
		messages = append(messages, memory.ConversationMessage{
			Role:    role,
			Content: content,
		})
	}
	return messages
}
