package main

// app.go is the TUI interactive mode entry point.
//
// Architecture: The TUI is an independent binary (CGO_ENABLED=0) for headless
// environments. It shares ALL agent logic with the GUI via corelib/agent/:
//   - corelib/agent.RunLoop 鈥?shared agent loop
//   - corelib/agent.BuildSystemPrompt 鈥?shared system prompt
//   - corelib/agent.CoreToolRegistry 鈥?shared tool registry (definition + handler bound)
//   - corelib/agent/sshtool 鈥?shared SSH tool
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
	"net/url"
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
	"github.com/RapidAI/CodeClaw/corelib/configfile"
	"github.com/RapidAI/CodeClaw/corelib/doctor"
	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
	"github.com/RapidAI/CodeClaw/corelib/goal"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/llm/moa"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/needleruntime"
	"github.com/RapidAI/CodeClaw/corelib/oauth"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	"github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/corelib/steering"
	"github.com/RapidAI/CodeClaw/corelib/task"
	"github.com/RapidAI/CodeClaw/corelib/tts"
	"github.com/RapidAI/CodeClaw/corelib/weixin"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
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
	startupIndicator := startTUIStartupIndicator()
	defer startupIndicator.Stop()

	// Data directory: ~/.maclaw (shared with GUI).
	startupIndicator.Stage(12, "准备数据目录")
	dataDir := commands.ResolveDataDir()
	logDir := filepath.Join(dataDir, "logs")
	os.MkdirAll(logDir, 0755)
	logPath := filepath.Join(logDir, "tui.log")
	if err := logger.SetLogFile(logPath); err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot open log file: %v\n", err)
	}
	// Redirect stderr (fd 2) to the log file BEFORE entering Bubble Tea's
	// alt-screen. This is the mechanism-level fix: every write to stderr 鈥?	// whether from Go's log package, fmt.Fprintf(os.Stderr,...), corelib
	// packages, or even C libraries 鈥?goes to the log file instead of
	// corrupting the terminal. See redirect_stderr_{unix,windows}.go.
	if lf := logger.LogFile(); lf != nil {
		redirectStderr(lf)
	}
	defer logger.Close()

	logger.Info("%s-tui starting (version %s)", strings.ToLower(brand.Current().DisplayName), version)

	// Load full app config from ~/.maclaw/config.json (shared with GUI).
	startupIndicator.Stage(22, "加载配置")
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

	// Initialize memory store (shared with GUI and MaClawSrv via corelib).
	startupIndicator.Stage(38, "初始化记忆库")
	memStore, err := memory.OpenDataDirStore(dataDir, memory.StoreModeAuto)
	if err != nil {
		logger.Warn("memory store init failed: %v", err)
	}
	experienceEvents := lifecycle.NewEventTrail(512)
	experienceSink := &lifecycle.AttributingEventSink{Sink: experienceEvents, Provider: memory.NewExperienceProvider(memStore)}
	if memStore != nil {
		memStore.SetExperienceEventSink(experienceSink)
	}

	// Initialize SSH manager.
	sshMgr := remote.NewSSHSessionManager(nil)

	// Initialize steering store (shared with GUI 鈥?same ~/.maclaw/steering/).
	steeringDir := filepath.Join(corelib.MaclawBaseDir(), "steering")
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
	startupIndicator.Stage(55, "装配运行时")
	// HubCenter failover uses the shared singleton cache and persister from
	// tui/commands/skill_search_api.go 鈥?no need to create local instances.
	app := &TUIApp{
		logger:           logger,
		llmConfig:        llmCfg,
		memoryStore:      memStore,
		experienceEvents: experienceEvents,
		sshMgr:           sshMgr,
		steeringStore:    steeringStore,
		appConfig:        appCfg,
		history:          convMemory,
		taskStore:        task.NewStore(),
		toolRegistry:     agent.NewCoreToolRegistry(),
		ttsManager:       initTUITTSManager(),
		costTracker:      llm.NewCostTracker(appCfg.DailyLLMBudgetUSD),
	}

	// Initialize scheduled task manager with background ticker.
	schPath := filepath.Join(dataDir, "scheduled_tasks.json")
	if schMgr, err := scheduler.NewManager(schPath); err == nil {
		app.scheduledTaskManager = schMgr
	} else {
		log.Printf("[TUI] WARNING: scheduled task manager init failed: %v", err)
	}

	// Initialize V2 workflow engine (StateMachine + Router + SQLiteStore).
	// V2 is the sole engine for runtime workflow routing and state management.
	app.workflowV2 = app.initWorkflowV2TUI()

	// Initialize knowledge store (shared with GUI 鈥?same ~/.maclaw/knowledge.db).
	startupIndicator.Stage(68, "加载知识库")
	app.initKnowledgeStore(dataDir)

	// Initialize WeChat gateway (runs in background if configured).
	app.weixinGateway = newTUIWeixinGateway(app)

	// Register tools: definition + handler bound together.
	startupIndicator.Stage(78, "注册工具")
	bgTaskMgr := remote.NewSSHBackgroundTaskManager(sshMgr)
	bgTaskMgr.SetPersistDir(filepath.Join(dataDir, "data"))
	sshHandler := func(args map[string]interface{}) string {
		deps := sshtool.SSHToolDeps{
			Manager:       sshMgr,
			BGTaskMgr:     bgTaskMgr,
			PolicyOwnerID: "tui:default",
			HostLoader: func() []corelib.SSHHostEntry {
				return app.appConfig.SSHHosts
			},
			OnConnected: func(session *remote.SSHManagedSession, cfg remote.SSHHostConfig) {
				// Rediscover orphan tasks after SSH connect (sync to avoid PTY race
				// with the next exec command LLM sends).
				bgTaskMgr.RediscoverOrphanTasksForOwner(session.ID, "tui:default")
			},
		}
		return sshtool.ToolSSH(deps, args)
	}
	goalStore := goal.NewStore(filepath.Join(dataDir, "data", "goals"))
	app.goalStore = goalStore
	agent.RegisterCoreTools(app.toolRegistry, agent.CoreToolDeps{
		MemoryStore: memStore,
		TaskStore:   app.taskStore,
		GoalStore:   goalStore,
		SecurityGuard: tuiSecurityGuard(func() corelib.AppConfig {
			return app.appConfig
		}),
		SSHHandler: sshHandler,
		ExtraHandlers: map[string]agent.ToolHandler{
			"manage_skill":           newManageSkillHandler(app),
			"manage_schedule":        newManageScheduleHandler(app),
			"im_message":             newIMMessageHandler(app),
			"tts":                    newTTSHandler(app),
			"knowledge_search":       app.toolKnowledgeSearch,
			"knowledge_context_pack": app.toolKnowledgeContextPack,
			"knowledge_save_text":    app.toolKnowledgeSaveText,
			"knowledge_save_url":     app.toolKnowledgeSaveURL,
		},
		WebSearchHandlerCtx: func(ctx context.Context, args map[string]interface{}) string {
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
			return agent.ToolWebSearchCtx(ctx, provider, args)
		},
		WebFetchHandlerCtx: func(ctx context.Context, args map[string]interface{}) string {
			// Use the same provider as web_search for enhanced fetch (e.g. TinyFish).
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
			return agent.ToolWebFetchWithProviderCtx(ctx, args, provider)
		},
	})

	// Start the scheduled task manager now that tools are registered.
	// The executor uses agent.RunLoop with the full tool registry.
	startupIndicator.Stage(86, "启动后台任务")
	if app.scheduledTaskManager != nil && llmConfigured {
		app.scheduledTaskManager.StartWithExecutor(app.buildScheduledTaskExecutor())
	}

	startupIndicator.Stage(92, "渲染界面")
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
	startupIndicator.Stage(98, "进入界面")

	// Wire the program before Run; gateway startup is scheduled from Init.
	if app.weixinGateway != nil {
		app.weixinGateway.SetProgram(p)
	}

	startupIndicator.Stop()
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
	if app.knowledgeStore != nil {
		app.knowledgeStore.Close()
	}
	sshMgr.Close()
	logger.Info("%s-tui stopped", strings.ToLower(brand.Current().DisplayName))
}

// buildLLMConfigFromAppConfig constructs MaclawLLMConfig from the shared
// AppConfig. This reads the same config.json that the GUI writes, so if
// the user has already configured maclaw via the GUI, the TUI works immediately.
func buildLLMConfigFromAppConfig(cfg corelib.AppConfig) corelib.MaclawLLMConfig {
	currentProvider := tuiCanonicalHubServiceProviderName(cfg.MaclawLLMCurrentProvider)
	llm := corelib.MaclawLLMConfig{
		URL:           cfg.MaclawLLMUrl,
		Key:           cfg.MaclawLLMKey,
		Model:         cfg.MaclawLLMModel,
		Protocol:      cfg.MaclawLLMProtocol,
		ContextLength: cfg.MaclawLLMContextLength,
		TimeoutSec:    cfg.MaclawLLMTimeoutSec,
		ProviderName:  currentProvider,
	}
	// Resolve provider-specific fields from the current provider entry.
	for _, p := range cfg.MaclawLLMProviders {
		if tuiCanonicalHubServiceProviderName(p.Name) == currentProvider {
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
			llm.MaxOutputTokens = p.MaxOutputTokens
			break
		}
	}
	if strings.TrimSpace(llm.Key) == "" && tuiAppConfigUsesHubLLMService(cfg) {
		llm.Key = strings.TrimSpace(cfg.RemoteViewerToken)
	}
	return llm
}

func tuiAppConfigUsesHubLLMService(cfg corelib.AppConfig) bool {
	current := tuiCanonicalHubServiceProviderName(cfg.MaclawLLMCurrentProvider)
	if tuiHubServiceProviderNameIsOfficial(current) {
		return true
	}
	for _, provider := range cfg.MaclawLLMProviders {
		if provider.IsHubService && tuiCanonicalHubServiceProviderName(provider.Name) == current {
			return true
		}
	}
	return false
}

// TUIApp holds the TUI's agent infrastructure.
type TUIApp struct {
	logger           *TUILogger
	llmConfig        corelib.MaclawLLMConfig
	memoryStore      *memory.Store
	experienceEvents *lifecycle.EventTrail
	knowledgeStore   *knowledge.SQLiteStore // cached, nil if DB doesn't exist
	sshMgr           *remote.SSHSessionManager
	steeringStore    *steering.Store
	appConfig        corelib.AppConfig
	history          *agent.ConversationMemory
	taskStore        *task.Store
	goalStore        *goal.Store
	toolRegistry     *agent.CoreToolRegistry
	ttsManager       *tts.Manager
	// costTracker tracks daily LLM $ (fleet-persisted) for budget gates.
	costTracker   *llm.CostTracker
	costTrackerMu sync.Mutex
	// moa holds one-shot / sticky multi-model council state for TUI chat.
	moa *tuiMoAState
	// HubCenter failover uses the shared singleton cache and persister from
	// tui/commands/skill_search_api.go 鈥?no fields needed here.

	// Scheduled task manager 鈥?background ticker fires due tasks.
	scheduledTaskManager *scheduler.Manager
	// Delivery target catalogs (channel → group/user list) for manage_schedule.
	scheduleTargetCatalogs     *scheduler.TargetCatalogRegistry
	scheduleTargetListCache    *scheduler.TargetListCache
	scheduleTargetCatalogsOnce sync.Once
	deliveryStateStoreCached   *scheduler.DeliveryStateStore
	deliveryStateStoreOnce     sync.Once

	// WeChat gateway 鈥?runs in background, receives/sends WeChat messages.
	weixinGateway *tuiWeixinGateway

	// V2 workflow engine — StateMachine + Router + SQLiteStore.
	// This is the sole workflow engine for TUI runtime operations.
	workflowV2 *tuiWorkflowV2State

	// workflowEngine is DEPRECATED — retained only for test backward compat.
	// Production code MUST NOT use this field. Tests that still reference it
	// should migrate to V2 (machine.Create / machine.GetActive etc.).
	// This field will be removed once all test files are migrated.
	workflowEngine *v2.WorkflowEngine

	// workflowMu protects pendingPhasePrompt and workflowAgentLoop from
	// concurrent access between the Bubble Tea main goroutine (which sets
	// them in handleWorkflowInterception) and the agent loop goroutine
	// (which reads and clears them in handleChatSend).
	workflowMu           sync.Mutex
	pendingPhasePrompt   string // stashed phase prompt for the next agent loop
	workflowAgentLoop    bool   // true when the agent loop runs on behalf of the workflow
	pendingWorkflowStart *tuiPendingWorkflowStart
	needleRuntime        *needleruntime.Runtime

	// evolutionPipeline schedules TUI skill self-repair after runs.
	evolutionPipeline *skill.EvolutionPipeline
	evolutionOnce     sync.Once
}

// initKnowledgeStore opens the knowledge DB if it exists.
// Called during runTUI startup, after dataDir is resolved.
func (app *TUIApp) initKnowledgeStore(dataDir string) {
	dbPath := filepath.Join(dataDir, "knowledge.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return // graceful skip 鈥?DB doesn't exist yet
	}
	store, err := knowledge.NewSQLiteStore(dbPath)
	if err != nil {
		log.Printf("[knowledge] failed to open store: %v", err)
		return
	}
	app.knowledgeStore = store
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
		MemoryStore:      app.memoryStore,
		HasKnowledgeBase: app.knowledgeStore != nil,
	}

	// Knowledge auto-recall hook (prior turns supplied by BuildSystemPrompt override when history is set)
	if app.knowledgeStore != nil {
		deps.KnowledgeAutoRecall = func(b *strings.Builder, userMsg string) {
			app.appendKnowledgeAutoRecall(b, userMsg, nil)
		}
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
	ssoMu      sync.Mutex
	ssoCancels map[string]*codeGenSSOCancel
	ssoParams  map[string]*oauth.HeadlessOAuthParams
}

type codeGenSSOCancel struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func (m *tuiModel) Init() tea.Cmd {
	readyCmd := func() tea.Msg {
		time.Sleep(100 * time.Millisecond)
		return tuiReadyMsg{}
	}
	weixinCmd := m.startWeixinGatewayCmd()
	if m.startupCmd != nil {
		return tea.Batch(readyCmd, m.startupCmd, weixinCmd)
	}
	return tea.Batch(readyCmd, weixinCmd)
}

func (m *tuiModel) startWeixinGatewayCmd() tea.Cmd {
	return func() tea.Msg {
		if m != nil && m.app != nil && m.app.weixinGateway != nil {
			m.app.weixinGateway.Start()
		}
		return nil
	}
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
		// But if Help is visible, let Esc close Help instead (handled by root鈫抙elp).
		if msg.String() == "esc" && m.activeCb != nil && !m.root.Help.IsVisible() {
			m.cancelActiveLoop()
			m.root.Chat.AppendSystemMessage(tuiText(m.uiLang(), "cancelled"))
			return m, nil
		}

	case views.ChatSendMsg:
		// Handle slash commands locally.
		if strings.HasPrefix(strings.TrimSpace(msg.Text), "/") {
			// /btw and /loop require an async agent loop 鈥?route through handleChatSend.
			trimmedCmd := strings.TrimSpace(msg.Text)
			if trimmedCmd == "/btw" || strings.HasPrefix(trimmedCmd, "/btw ") {
				if m.llmMissing() {
					return m, m.routeMissingLLMFromChat()
				}
				return m, m.handleChatSend(msg.Text, msg.AgentMode)
			}
			if trimmedCmd == "/moa" || strings.HasPrefix(trimmedCmd, "/moa ") ||
				strings.EqualFold(trimmedCmd, "/moa") || strings.HasPrefix(strings.ToLower(trimmedCmd), "/moa ") {
				if m.llmMissing() {
					return m, m.routeMissingLLMFromChat()
				}
				return m, m.handleChatSend(msg.Text, msg.AgentMode)
			}
			if trimmedCmd == "/loop" || strings.HasPrefix(trimmedCmd, "/loop ") {
				if m.llmMissing() {
					return m, m.routeMissingLLMFromChat()
				}
				return m, m.handleLoopCommand(msg.Text)
			}
			if trimmedCmd == "/goal" || strings.HasPrefix(trimmedCmd, "/goal ") {
				return m, m.handleSlashCommand(msg.Text)
			}
			return m, m.handleSlashCommand(msg.Text)
		}
		if m.llmMissing() {
			return m, m.routeMissingLLMFromChat()
		}
		// Start the agent loop directly. The user message was already added
		// to ChatModel.messages and rendered in the previous Update鈫扸iew cycle
		// (ChatModel.Update handles the Enter key, adds the message, and
		// returns ChatSendMsg as a Cmd 鈥?Bubble Tea renders View() between
		// that Update and this one). No artificial delay needed.
		return m, m.handleChatSend(msg.Text, msg.AgentMode)

	case views.ChatQueueFireMsg:
		// Pre-input queue auto-fire or manual fire follows the same path as ChatSendMsg.
		if m.llmMissing() {
			return m, m.routeMissingLLMFromChat()
		}
		return m, m.handleChatSend(msg.Text, msg.AgentMode)

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

	case views.OnboardingResolveIdentityMsg:
		return m, m.resolveIdentityFromTUI(msg.Identity, msg.HubCenterURL)

	case views.OnboardingVerifyCodeMsg:
		return m, m.verifyCodeFromTUI(msg.Identity, msg.VerifyCode, msg.Method, msg.HubURL, msg.HubID, msg.TenantID, msg.HubCenterURL)

	case views.OnboardingStartSSOMsg:
		return m, m.startCodeGenSSOFromTUI(msg.FlowID)

	case views.OnboardingPollSSOMsg:
		return m, m.pollCodeGenSSOFromTUI(msg.FlowID, msg.Client)

	case views.OnboardingSubmitSSOInputMsg:
		return m, m.submitCodeGenSSOInputFromTUI(msg.FlowID, msg.Input)

	case views.OnboardingCancelSSOMsg:
		m.cancelCodeGenSSOFlow(msg.FlowID)
		return m, nil

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

	case views.OnboardingResolveIdentityResultMsg:
		var cmd tea.Cmd
		m.root, cmd = m.root.Update(msg)
		if !msg.Success {
			m.root.StatusBar.SetMessage(tuiFormat(m.uiLang(), "hubActivationFailed", msg.Message))
		}
		return m, cmd

	case views.OnboardingVerifyCodeResultMsg:
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

	case views.OnboardingSSOQRMsg, views.OnboardingSSOResultMsg:
		var cmd tea.Cmd
		accepted := true
		if result, ok := msg.(views.OnboardingSSOResultMsg); ok {
			accepted = m.root.Onboarding.AcceptsSSOFlow(result.FlowID)
		}
		if result, ok := msg.(views.OnboardingSSOResultMsg); ok && result.Success && accepted {
			if saveErr := m.saveAcceptedCodeGenSSOResultToConfig(result); saveErr != nil {
				msg = views.OnboardingSSOResultMsg{FlowID: result.FlowID, Success: false, Message: saveErr.Error(), KeepOpen: true, FromManual: result.FromManual}
				accepted = true
			} else {
				m.cancelCodeGenSSOFlow(result.FlowID)
			}
		}
		m.root, cmd = m.root.Update(msg)
		if result, ok := msg.(views.OnboardingSSOResultMsg); ok && result.Success && accepted {
			m.reloadConfigBackedViews()
			m.root.StatusBar.SetMessage(tuiText(m.uiLang(), "codeGenSSOSuccess"))
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

// runLocalDoctor evaluates shared readiness against the TUI's loaded AppConfig.
func (m *tuiModel) runLocalDoctor() doctor.Report {
	cfg := corelib.AppConfig{}
	cfgPath := ""
	if m != nil && m.app != nil {
		cfg = m.app.appConfig
		cfgPath = filepath.Join(commands.ResolveDataDir(), "config.json")
	}
	return doctor.Run(doctor.Input{
		Config:     cfg,
		ConfigPath: cfgPath,
	})
}

// firstNonFlagArg returns the first slash-command argument that is not a --flag.
func firstNonFlagArg(args []string) string {
	for _, a := range args {
		a = strings.TrimSpace(a)
		if a == "" || strings.HasPrefix(a, "-") {
			continue
		}
		return a
	}
	return ""
}

// formatCanaryPreviewLine builds a one-line sticky canary membership summary.
// percent should already be resolved via ResolveSharedLoopEnv (env>config).
func formatCanaryPreviewLine(userID string, percent int) string {
	p := doctor.PreviewSharedLoopCanary(userID, percent)
	inOut := "OUT"
	if p.Allows {
		inOut = "IN"
	}
	return fmt.Sprintf("canary-preview: user=%q %s canary · percent=%d bucket=%d",
		p.UserID, inOut, p.Percent, p.Bucket)
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
		// Cancel active V2 workflow + understanding session.
		if wf := m.app.workflowV2; wf != nil {
			_ = wf.machine.Cancel("tui-user")
			if wf.understanding != nil && wf.understanding.HasActiveSession("tui-user") {
				wf.understanding.CancelSession("tui-user")
			}
		}
		// Clear active goal on conversation reset.
		if m.app.goalStore != nil {
			m.app.goalStore.Clear("default")
		}
		m.app.workflowMu.Lock()
		m.app.pendingPhasePrompt = ""
		m.app.workflowAgentLoop = false
		m.app.pendingWorkflowStart = nil
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

	case cmdName == "/doctor":
		// Print the shared readiness report into chat, then open the setup panel.
		report := m.runLocalDoctor()
		m.root.Chat.AppendSystemMessage(doctor.FormatReport(report))
		m.root.SetTab(views.TabConfig)
		m.root.Config.FocusSetupStatus()
		if report.OK {
			m.root.StatusBar.SetMessage(tuiText(m.uiLang(), "slashOpenStatus"))
		} else {
			m.root.StatusBar.SetMessage(report.Summary)
		}
		return nil

	case cmdName == "/status" || cmdName == "/health":
		// Short summary + setup panel (full detail via /doctor).
		// Optional: /status <user-id> appends sticky canary membership preview.
		report := m.runLocalDoctor()
		msg := fmt.Sprintf("MaClaw doctor: %s (use /doctor for full checks)", report.Summary)
		cfg := corelib.AppConfig{}
		if m.app != nil {
			cfg = m.app.appConfig
		}
		env := doctor.ResolveSharedLoopEnv(cfg)
		if line := doctor.FormatSharedLoopLine(env); line != "" {
			msg = msg + "\n" + line
		}
		if line := agent.FormatPromptProfileLine(); line != "" {
			msg = msg + "\n" + line
		}
		if !agent.LightToolRetryEnabled() {
			msg = msg + "\nlight_retry=off (" + agent.PromptLightRetryEnvKey + ")"
		}
		if uid := firstNonFlagArg(args); uid != "" {
			msg = msg + "\n" + formatCanaryPreviewLine(uid, env.Percent)
		}
		m.root.Chat.AppendSystemMessage(msg)
		m.root.SetTab(views.TabConfig)
		m.root.Config.FocusSetupStatus()
		m.root.StatusBar.SetMessage(tuiText(m.uiLang(), "slashOpenStatus"))
		return nil

	case cmdName == "/canary":
		// Sticky shared-loop canary membership preview (same FNV algorithm as runtime).
		uid := firstNonFlagArg(args)
		if uid == "" {
			m.root.Chat.AppendSystemMessage("usage: /canary <user-id>\n  preview sticky canary vs MACLAW_SHARED_AGENT_LOOP_PERCENT")
			m.root.StatusBar.SetMessage("usage: /canary <user-id>")
			return nil
		}
		cfg := corelib.AppConfig{}
		if m.app != nil {
			cfg = m.app.appConfig
		}
		env := doctor.ResolveSharedLoopEnv(cfg)
		line := formatCanaryPreviewLine(uid, env.Percent)
		m.root.Chat.AppendSystemMessage(line)
		m.root.StatusBar.SetMessage(line)
		return nil

	case cmdName == "/prompt-export" || cmdName == "/export-prompt-stats":
		// Portable adaptive-prompt snapshot (same as maclaw-cli shared-loop export --write).
		exp := agent.BuildPromptProfileExport()
		path := agent.DefaultPromptProfileExportPath()
		if err := agent.WritePromptProfileExport(path, exp); err != nil {
			m.root.Chat.AppendSystemMessage("prompt-export failed: " + err.Error())
			m.root.StatusBar.SetMessage(err.Error())
			return nil
		}
		sum := strings.TrimSpace(exp.Summary)
		if sum == "" {
			sum = "adaptive-prompt: no turns yet"
		}
		msg := fmt.Sprintf("prompt-export written:\n  %s\n  %s\n  merge: maclaw-cli shared-loop merge-exports FILE…", path, sum)
		m.root.Chat.AppendSystemMessage(msg)
		m.root.StatusBar.SetMessage("exported " + filepath.Base(path))
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

	case cmdName == "/goal":
		return m.handleGoalSlashCommand(text)

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

// handleChatSend runs either the Agent loop or simple no-tool chat in a goroutine.
func (m *tuiModel) handleChatSend(text string, agentMode bool) tea.Cmd {
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
			// This is by design 鈥?/btw is a side query, not part of the main task.

			return views.ChatResponseMsg{Text: responseText}
		}
	}

	// /moa — shared parser (corelib/llm/moa.ParseSlash): one-shot, @preset, sticky, stats.
	if moa.IsMoASlash(trimmedText) {
		cmd := moa.ParseSlash(trimmedText)
		switch cmd.Kind {
		case moa.SlashHelp:
			return func() tea.Msg {
				return views.ChatResponseMsg{Text: tuiText(lang, "moaUsage")}
			}
		case moa.SlashUsage:
			return func() tea.Msg {
				return views.ChatResponseMsg{Text: tuiText(lang, "moaAtPresetUsage")}
			}
		case moa.SlashStats:
			line := moa.FormatStatsLine()
			if line == "" {
				line = tuiText(lang, "moaStatsEmpty")
			}
			return func() tea.Msg {
				return views.ChatResponseMsg{Text: line}
			}
		case moa.SlashSticky:
			arg := cmd.StickyArg
			switch arg {
			case "on", "1", "true", "enable":
				presetName := cmd.StickyPreset
				var resolved moa.ResolvedPreset
				var err error
				if presetName != "" {
					resolved, err = app.resolveMoAPresetNamed(presetName)
				} else {
					resolved, err = app.resolveMoADefaultPreset()
				}
				if err != nil {
					return func() tea.Msg {
						return views.ChatResponseMsg{Error: tuiFormat(lang, "moaUnavailable", err.Error())}
					}
				}
				app.moaState().armSticky(resolved)
				return func() tea.Msg {
					return views.ChatResponseMsg{Text: tuiFormat(lang, "moaStickyOnNamed", resolved.Name)}
				}
			case "off", "0", "false", "disable", "clear":
				app.moaState().clear()
				return func() tea.Msg {
					return views.ChatResponseMsg{Text: tuiText(lang, "moaStickyOff")}
				}
			case "status", "":
				st := app.moaState()
				st.mu.Lock()
				sticky := st.sticky != nil
				oneshot := st.oneShot != nil
				name := ""
				if st.sticky != nil {
					name = st.sticky.Name
				}
				st.mu.Unlock()
				return func() tea.Msg {
					return views.ChatResponseMsg{Text: tuiFormat(lang, "moaStickyStatus", sticky, oneshot, name)}
				}
			default:
				return func() tea.Msg {
					return views.ChatResponseMsg{Text: tuiText(lang, "moaStickyUsage")}
				}
			}
		case moa.SlashOneShot:
			if strings.TrimSpace(cmd.Prompt) == "" {
				return func() tea.Msg {
					return views.ChatResponseMsg{Text: tuiText(lang, "moaUsage")}
				}
			}
			var resolved moa.ResolvedPreset
			var err error
			if cmd.Preset != "" {
				resolved, err = app.resolveMoAPresetNamed(cmd.Preset)
			} else {
				resolved, err = app.resolveMoADefaultPreset()
			}
			if err != nil {
				return func() tea.Msg {
					return views.ChatResponseMsg{Error: tuiFormat(lang, "moaUnavailable", err.Error())}
				}
			}
			app.moaState().armOneShot(resolved)
			text = cmd.Prompt
			trimmedText = cmd.Prompt
		default:
			return func() tea.Msg {
				return views.ChatResponseMsg{Text: tuiText(lang, "moaUsage")}
			}
		}
	}

	if !agentMode {
		return m.handleSimpleChatSend(text)
	}

	cb := newTuiCallbacks(app, prog)
	cb.lastUserText = text
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
		cb.history = history

		result := agent.RunLoop(cb, text, history, nil)

		// Budget hard-stop: surface gate message even when assistant text is set.
		if result.Error == "daily_llm_budget_exceeded" {
			msg := strings.TrimSpace(result.Text)
			if msg == "" {
				if _, _, gateMsg := app.earlyStopBudget(); gateMsg != "" {
					msg = gateMsg
				} else {
					msg = "今日 LLM 预算已用尽。"
				}
			}
			return views.ChatResponseMsg{Text: msg}
		}

		// Save conversation history (persisted to disk).
		history = append(history, agent.ConversationEntry{Role: "user", Content: text})
		if result.Text != "" {
			history = append(history, agent.ConversationEntry{Role: "assistant", Content: result.Text})

			// --- Workflow doc capture ---
			// When the agent loop ran on behalf of the workflow engine,
			// save the output as the phase document.
			if cb.phasePromptOverride != "" {
				// Record output in V2 state machine (SQLite persistence).
				if wf := app.getWorkflowV2TUI(); wf != nil {
					if err := wf.machine.RecordOutput("tui-user", result.Text); err != nil {
						log.Printf("[TUI-workflow-v2] RecordOutput failed: %v", err)
					} else {
						log.Printf("[TUI-workflow-v2] recorded phase output len=%d", len(result.Text))
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
		text := result.Text
		promptProfile := cb.lastPromptProfile
		if cb.forceFullPrompt || result.LightUpgraded || cb.lastPromptABSample || cb.lastPromptSoftFull {
			promptProfile = agent.PromptProfileFull
		}
		if promptProfile == "" {
			promptProfile, _ = cb.resolvePromptProfile(text)
		}
		if meta := agent.FormatTurnMetaOpts(agent.TurnMetaOptions{
			Route:             result.Route,
			Usage:             result.Usage,
			PromptProfile:     string(promptProfile),
			PromptSavedTokens: cb.lastPromptSavedTokens,
			PromptUpgraded:    result.LightUpgraded || cb.forceFullPrompt,
			PromptABSample:    cb.lastPromptABSample && !result.LightUpgraded && !cb.forceFullPrompt,
			PromptSoftFull:    cb.lastPromptSoftFull && !result.LightUpgraded && !cb.forceFullPrompt && !cb.lastPromptABSample,
		}); meta != "" {
			if strings.TrimSpace(text) == "" {
				text = meta
			} else {
				text = text + "\n\n" + meta
			}
		}
		return views.ChatResponseMsg{Text: text}
	}
}

func (m *tuiModel) handleSimpleChatSend(text string) tea.Cmd {
	app := m.app
	lang := m.uiLang()
	return func() tea.Msg {
		history := app.history.Load("tui-user")
		messages := simpleChatMessages(history, text)
		client := &http.Client{Timeout: 60 * time.Second}
		resp, err := agent.DoSimpleLLMRequest(app.llmConfig, messages, client, 60*time.Second)
		if err != nil {
			return views.ChatResponseMsg{Error: err.Error()}
		}

		answer := ""
		if resp != nil {
			answer = strings.TrimSpace(resp.Content)
		}
		if answer == "" {
			answer = "模型返回了空响应。"
			if lang == "en" {
				answer = "The model returned an empty response."
			}
		}

		history = append(history, agent.ConversationEntry{Role: "user", Content: text})
		history = append(history, agent.ConversationEntry{Role: "assistant", Content: answer})
		app.history.Save("tui-user", history)
		return views.ChatResponseMsg{Text: answer}
	}
}

func simpleChatMessages(history []agent.ConversationEntry, text string) []interface{} {
	const maxHistoryEntries = 20
	start := 0
	if len(history) > maxHistoryEntries {
		start = len(history) - maxHistoryEntries
	}
	messages := make([]interface{}, 0, len(history)-start+1)
	for _, entry := range history[start:] {
		role := strings.TrimSpace(entry.Role)
		if role != "user" && role != "assistant" && role != "system" {
			continue
		}
		content, ok := entry.Content.(string)
		if !ok || strings.TrimSpace(content) == "" {
			continue
		}
		messages = append(messages, map[string]interface{}{"role": role, "content": content})
	}
	messages = append(messages, map[string]interface{}{"role": "user", "content": text})
	return messages
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
		allowedSources, err := tuiAllowedSkillSearchSourcesForPolicy(m.app.appConfig, query)
		if err != nil {
			return views.ToolSkillSearchResultMsg{Error: err.Error()}
		}
		report := client.SearchAllFilteredReport(ctx, hubURL, query, allowedSources)
		hubResults := report.Results
		warn := report.FormatDegradedNote()

		if len(hubResults) == 0 {
			errMsg := tuiText(lang, "skillNoMatch")
			if warn != "" {
				errMsg = errMsg + "\n" + warn
			}
			return views.ToolSkillSearchResultMsg{Error: errMsg, Warning: warn}
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
		return views.ToolSkillSearchResultMsg{Results: results, Warning: warn}
	}
}

func (m *tuiModel) installSkill(skillID, hubURL string, source string, installRef string) tea.Cmd {
	lang := m.uiLang()
	return func() tea.Msg {
		if source != "clawhub" && source != "github" && hubURL == "" {
			hubURL = commands.ResolveHubCenterWithFailover(m.app.appConfig, m.app.appConfig.SkillHubBaseURL(remote.DefaultRemoteHubCenterURL), nil, nil)
		}
		if m != nil && m.app != nil {
			guardArgs := map[string]interface{}{"action": "install", "skill_id": skillID, "source": source, "hub_url": hubURL, "install_ref": installRef}
			if ok, reason := enforceClientSecurityPolicy(m.app.appConfig, "manage_skill", guardArgs); !ok {
				return views.ToolOperationResultMsg{Tab: views.ToolSubSkill, Success: false, Message: reason}
			}
			effectiveSource := source
			if effectiveSource == "" {
				effectiveSource = "skillhub"
			}
			recordTUIDeveloperSkillRisk(m.app.appConfig, effectiveSource, "install", guardArgs)
		}
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
		if ok, reason := enforceClientSecurityPolicy(cfg, "bash", map[string]interface{}{"command": strings.Join(append([]string{entry.Command}, entry.Args...), " ")}); !ok {
			return views.ToolOperationResultMsg{Tab: views.ToolSubMCP, Success: false, Message: reason}
		}
		cfg.LocalMCPServers = append(cfg.LocalMCPServers, entry)
		if err := store.SaveConfig(cfg); err != nil {
			return views.ToolOperationResultMsg{Tab: views.ToolSubMCP, Success: false, Message: tuiFormat(lang, "configSaveFailedPlain", err.Error())}
		}
		// Return success 鈥?the main loop will refresh data via ToolRefreshMsg.
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
		if ok, reason := enforceClientSecurityPolicy(cfg, "web_fetch", map[string]interface{}{"url": entry.EndpointURL}); !ok {
			return views.ToolOperationResultMsg{Tab: views.ToolSubMCP, Success: false, Message: reason}
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
		if blocked, reason := rejectHubManagedSecurityConfigChange(cfg, msg.Key); blocked {
			return views.ConfigSaveFailedMsg{Key: msg.Key, Error: reason}
		}
		current := cfg
		if msg.HasConfig {
			cfg = msg.Config
		} else {
			applyConfigValue(&cfg, msg.Key, msg.Value)
		}
		preserveHubManagedSecurityConfig(current, &cfg)
		if err := store.SaveConfig(cfg); err != nil {
			return views.ConfigSaveFailedMsg{Key: msg.Key, Error: err.Error()}
		}
		return views.ConfigSavedMsg{Key: msg.Key, Value: msg.Value}
	}
}

func (m *tuiModel) resolveIdentityFromTUI(identity, hubCenterURL string) tea.Cmd {
	lang := m.uiLang()
	return func() tea.Msg {
		store := commands.NewFileConfigStore(commands.ResolveDataDir())
		cfg, err := store.LoadConfig()
		if err != nil {
			return views.OnboardingResolveIdentityResultMsg{Success: false, Message: err.Error()}
		}
		hubCenterURL = strings.TrimRight(strings.TrimSpace(hubCenterURL), "/")
		if hubCenterURL == "" {
			hubCenterURL = strings.TrimRight(strings.TrimSpace(cfg.RemoteHubCenterURL), "/")
		}
		if hubCenterURL == "" {
			hubCenterURL = remote.DefaultRemoteHubCenterURL
		}

		// Single timeout covers all steps (resolve + auth check + send code)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Step 1: Resolve Hub from HubCenter using identity
		hubCenterURLs := cfg.HubCenterBaseURLs(remote.DefaultRemoteHubCenterURL, remote.DefaultRemoteHubCenterURLs)
		resolveResult, _, _, err := remote.NewEnrollmentClient().ResolveHubs(ctx, identity, "", hubCenterURL, hubCenterURLs)
		if err != nil {
			return views.OnboardingResolveIdentityResultMsg{Success: false, Message: err.Error()}
		}
		hubURL, hubID, tenantID, err := remote.PickBestHubWithTenantAndID(*resolveResult)
		if err != nil {
			// Fallback: if identity looks like a phone number and resolve failed
			// with "no phone route found", try using the configured hub URL
			configuredHub := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
			if configuredHub != "" && isPhoneIdentityForTUI(identity) {
				hubURL = configuredHub
				hubID = strings.TrimSpace(cfg.RemoteHubID)
				tenantID = strings.TrimSpace(cfg.RemoteTenantID)
			} else {
				return views.OnboardingResolveIdentityResultMsg{Success: false, Message: err.Error()}
			}
		}

		// Step 2: Get registration auth method from Hub
		authURL := strings.TrimRight(hubURL, "/") + "/api/enroll/registration-auth"
		if tid := strings.TrimSpace(tenantID); tid != "" {
			authURL += "?tenant_id=" + url.QueryEscape(tid)
		}
		httpClient := remote.NewHubHTTPClient()
		authReq, err := http.NewRequestWithContext(ctx, http.MethodGet, authURL, nil)
		if err != nil {
			return views.OnboardingResolveIdentityResultMsg{Success: false, Message: err.Error()}
		}
		authResp, err := httpClient.Do(authReq)
		if err != nil {
			return views.OnboardingResolveIdentityResultMsg{Success: false, Message: err.Error()}
		}
		defer authResp.Body.Close()
		var authResult struct {
			Method     string `json:"method"`
			CodeLength int    `json:"code_length"`
		}
		if err := remote.DecodeHTTPJSONResponse(authResp, &authResult, "registration auth"); err != nil {
			return views.OnboardingResolveIdentityResultMsg{Success: false, Message: err.Error()}
		}
		if authResp.StatusCode >= 300 {
			return views.OnboardingResolveIdentityResultMsg{Success: false, Message: tuiText(lang, "regAuthFailed")}
		}
		method := strings.ToLower(strings.TrimSpace(authResult.Method))
		if method == "" {
			method = "email"
		}
		// If Hub declares phone auth but identity looks like email, override to email.
		// This mirrors GUI's registrationIdentityLooksPhone logic.
		if method == "phone" && !isPhoneIdentityForTUI(identity) {
			method = "email"
		}
		// If Hub requires email auth but identity is a phone number, inform user.
		if method == "email" && isPhoneIdentityForTUI(identity) && !strings.Contains(identity, "@") {
			return views.OnboardingResolveIdentityResultMsg{
				Success: false,
				Message: tuiText(lang, "regRequiresEmail"),
			}
		}
		codeLength := authResult.CodeLength
		if codeLength <= 0 {
			codeLength = 6
		}

		// Step 3: Send verification code (phone only)
		if method == "phone" {
			phone := normalizeOnboardingPhoneForTUI(identity)
			if len(phone) < 6 {
				return views.OnboardingResolveIdentityResultMsg{Success: false, Message: tuiText(lang, "regInvalidPhone")}
			}
			payload := map[string]string{"phone_number": phone}
			if strings.TrimSpace(tenantID) != "" {
				payload["tenant_id"] = strings.TrimSpace(tenantID)
			}
			data, err := json.Marshal(payload)
			if err != nil {
				return views.OnboardingResolveIdentityResultMsg{Success: false, Message: err.Error()}
			}
			smsReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(hubURL, "/")+"/api/enroll/sms/send-code", bytes.NewReader(data))
			if err != nil {
				return views.OnboardingResolveIdentityResultMsg{Success: false, Message: err.Error()}
			}
			smsReq.Header.Set("Content-Type", "application/json")
			resp, err := httpClient.Do(smsReq)
			if err != nil {
				return views.OnboardingResolveIdentityResultMsg{Success: false, Message: err.Error()}
			}
			defer resp.Body.Close()
			var smsResult struct {
				OK      bool   `json:"ok"`
				Code    string `json:"code,omitempty"`
				Message string `json:"message,omitempty"`
			}
			if err := remote.DecodeHTTPJSONResponse(resp, &smsResult, "send SMS"); err != nil {
				return views.OnboardingResolveIdentityResultMsg{Success: false, Message: err.Error()}
			}
			if resp.StatusCode >= 300 {
				msg := smsResult.Message
				if msg == "" {
					msg = tuiText(lang, "regSMSSendFailed")
				}
				if smsResult.Code != "" {
					msg = smsResult.Code + ": " + msg
				}
				return views.OnboardingResolveIdentityResultMsg{Success: false, Message: msg}
			}
		}

		return views.OnboardingResolveIdentityResultMsg{
			Success:    true,
			Identity:   identity,
			Method:     method,
			HubURL:     hubURL,
			HubID:      hubID,
			TenantID:   tenantID,
			CodeLength: codeLength,
		}
	}
}

func (m *tuiModel) verifyCodeFromTUI(identity, verifyCode, method, hubURL, hubID, tenantID, hubCenterURL string) tea.Cmd {
	lang := m.uiLang()
	return func() tea.Msg {
		store := commands.NewFileConfigStore(commands.ResolveDataDir())
		cfg, err := store.LoadConfig()
		if err != nil {
			return views.OnboardingVerifyCodeResultMsg{Success: false, Message: err.Error()}
		}

		if hubURL == "" {
			return views.OnboardingVerifyCodeResultMsg{Success: false, Message: tuiText(lang, "regHubNotResolved")}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		profile := remote.BuildMachineProfile(version)
		clientID := strings.TrimSpace(cfg.RemoteClientID)
		if clientID == "" {
			clientID = remote.GenerateClientID()
		}
		heartbeat := 30

		httpClient := remote.NewHubHTTPClient()
		var enrollResult remote.EnrollResult

		if method == "phone" {
			phone := normalizeOnboardingPhoneForTUI(identity)
			body := map[string]any{
				"phone_number":           phone,
				"verify_code":            verifyCode,
				"machine_name":           profile.MachineName,
				"platform":               profile.Platform,
				"hostname":               profile.Hostname,
				"arch":                   profile.Arch,
				"app_version":            profile.AppVersion,
				"heartbeat_interval_sec": heartbeat,
				"client_id":              clientID,
			}
			if strings.TrimSpace(tenantID) != "" {
				body["tenant_id"] = strings.TrimSpace(tenantID)
			}
			data, err := json.Marshal(body)
			if err != nil {
				return views.OnboardingVerifyCodeResultMsg{Success: false, Message: err.Error()}
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(hubURL, "/")+"/api/enroll/sms/verify-and-start", bytes.NewReader(data))
			if err != nil {
				return views.OnboardingVerifyCodeResultMsg{Success: false, Message: err.Error()}
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := httpClient.Do(req)
			if err != nil {
				return views.OnboardingVerifyCodeResultMsg{Success: false, Message: err.Error()}
			}
			defer resp.Body.Close()
			if err := remote.DecodeHTTPJSONResponse(resp, &enrollResult, "SMS activation"); err != nil {
				return views.OnboardingVerifyCodeResultMsg{Success: false, Message: err.Error()}
			}
			if resp.StatusCode >= 300 {
				msg := enrollResult.Message
				if msg == "" {
					msg = tuiFormat(lang, "regActivationFailed", resp.Status)
				}
				if enrollResult.Code != "" {
					msg = enrollResult.Code + ": " + msg
				}
				return views.OnboardingVerifyCodeResultMsg{Success: false, Message: msg}
			}
		} else {
			// Email method: use standard enroll with email
			body := map[string]any{
				"email":                  identity,
				"machine_name":           profile.MachineName,
				"platform":               profile.Platform,
				"hostname":               profile.Hostname,
				"arch":                   profile.Arch,
				"app_version":            profile.AppVersion,
				"heartbeat_interval_sec": heartbeat,
				"client_id":              clientID,
			}
			if strings.TrimSpace(tenantID) != "" {
				body["tenant_id"] = strings.TrimSpace(tenantID)
			}
			data, err := json.Marshal(body)
			if err != nil {
				return views.OnboardingVerifyCodeResultMsg{Success: false, Message: err.Error()}
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(hubURL, "/")+"/api/enroll/start", bytes.NewReader(data))
			if err != nil {
				return views.OnboardingVerifyCodeResultMsg{Success: false, Message: err.Error()}
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := httpClient.Do(req)
			if err != nil {
				return views.OnboardingVerifyCodeResultMsg{Success: false, Message: err.Error()}
			}
			defer resp.Body.Close()
			if err := remote.DecodeHTTPJSONResponse(resp, &enrollResult, "email activation"); err != nil {
				return views.OnboardingVerifyCodeResultMsg{Success: false, Message: err.Error()}
			}
			if resp.StatusCode >= 300 {
				msg := enrollResult.Message
				if msg == "" {
					msg = tuiFormat(lang, "regActivationFailed", resp.Status)
				}
				if enrollResult.Code != "" {
					msg = enrollResult.Code + ": " + msg
				}
				return views.OnboardingVerifyCodeResultMsg{Success: false, Message: msg}
			}
		}

		cfg.RemoteEmail = enrollResult.Email
		if method == "phone" {
			cfg.RemoteMobile = normalizeOnboardingPhoneForTUI(identity)
		}
		cfg.RemoteSN = enrollResult.SN
		cfg.RemoteUserID = enrollResult.UserID
		cfg.RemoteTenantID = enrollResult.TenantID
		cfg.RemoteTenantName = enrollResult.TenantName
		cfg.RemoteMachineID = enrollResult.MachineID
		cfg.RemoteMachineToken = enrollResult.MachineToken
		cfg.RemoteHubURL = hubURL
		cfg.RemoteEnabled = true
		cfg.DefaultLaunchMode = "remote"
		if strings.TrimSpace(hubID) != "" {
			cfg.RemoteHubID = strings.TrimSpace(hubID)
		} else if strings.TrimSpace(enrollResult.HubID) != "" {
			cfg.RemoteHubID = strings.TrimSpace(enrollResult.HubID)
		}
		if enrollResult.ViewerToken != "" {
			cfg.RemoteViewerToken = enrollResult.ViewerToken
		}
		if clientID != "" && cfg.RemoteClientID == "" {
			cfg.RemoteClientID = clientID
		}
		if strings.TrimSpace(hubCenterURL) != "" && !remote.IsLoopbackURL(hubCenterURL) {
			cfg.RemoteHubCenterURL = strings.TrimSpace(hubCenterURL)
		}
		if err := store.SaveConfig(cfg); err != nil {
			return views.OnboardingVerifyCodeResultMsg{Success: false, Message: err.Error()}
		}
		m.app.appConfig = cfg
		hubServiceReady := strings.TrimSpace(cfg.RemoteHubURL) != "" && strings.TrimSpace(cfg.RemoteViewerToken) != ""
		machineReady := strings.TrimSpace(cfg.RemoteMachineID) != "" &&
			strings.TrimSpace(cfg.RemoteMachineToken) != "" &&
			strings.TrimSpace(cfg.RemoteViewerToken) != ""
		message := tuiText(lang, "activated")
		if !hubServiceReady && !machineReady {
			message = tuiText(lang, "viewerTokenMissing")
		}
		return views.OnboardingVerifyCodeResultMsg{
			Success:         true,
			Message:         message,
			HubURL:          hubURL,
			MachineID:       enrollResult.MachineID,
			HubServiceReady: hubServiceReady,
			MachineReady:    machineReady,
		}
	}
}

func normalizeOnboardingPhoneForTUI(value string) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isPhoneIdentityForTUI(identity string) bool {
	identity = strings.TrimSpace(identity)
	if identity == "" || strings.Contains(identity, "@") {
		return false
	}
	return len(normalizeOnboardingPhoneForTUI(identity)) >= 6
}

func (m *tuiModel) startCodeGenSSOFromTUI(flowID string) tea.Cmd {
	lang := m.uiLang()
	m.logCodeGenSSO("start flow=%s", flowID)
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		entry := m.registerCodeGenSSOCancel(flowID, cancel)
		loginURL, callbackURL, err := oauth.StartCodeGenSSOCallbackServer(ctx)
		if err != nil {
			m.finishCodeGenSSOFlow(flowID, entry)
			m.logCodeGenSSO("callback start failed flow=%s err=%v", flowID, err)
			return views.OnboardingSSOQRMsg{FlowID: flowID, Success: false, Message: err.Error()}
		}
		m.logCodeGenSSO("callback ready flow=%s loginURL=%s callback=%s", flowID, loginURL, callbackURL)
		return views.OnboardingSSOQRMsg{FlowID: flowID, Success: true, QR: loginURL, LoginURL: loginURL, PollClient: http.DefaultClient, Message: tuiText(lang, "ssoWaitingScan")}
	}
}

func (m *tuiModel) pollCodeGenSSOFromTUI(flowID string, _ *http.Client) tea.Cmd {
	lang := m.uiLang()
	return func() tea.Msg {
		if strings.TrimSpace(flowID) == "" {
			m.logCodeGenSSO("poll rejected: empty flow")
			return views.OnboardingSSOResultMsg{FlowID: flowID, Success: false, Message: tuiText(lang, "ssoSessionEmpty")}
		}
		m.logCodeGenSSO("poll start flow=%s", flowID)
		done := m.codeGenSSODone(flowID)
		resultCh := make(chan views.OnboardingSSOResultMsg, 1)
		go func() {
			result, err := oauth.WaitForCodeGenSSOCallback(oauth.CodeGenTimeout)
			if err != nil {
				m.logCodeGenSSO("poll failed flow=%s err=%v", flowID, err)
				resultCh <- views.OnboardingSSOResultMsg{FlowID: flowID, Success: false, Message: err.Error(), KeepOpen: true}
				return
			}
			m.logCodeGenSSO("poll success flow=%s email=%s model=%s", flowID, result.Email, result.ModelID)
			resultCh <- codeGenSSOResultMsgFromResult(lang, flowID, result, false)
		}()
		select {
		case result := <-resultCh:
			return result
		case <-done:
			m.logCodeGenSSO("poll cancelled flow=%s", flowID)
			return views.OnboardingSSOResultMsg{FlowID: flowID, Success: false, Message: tuiText(lang, "cancelled")}
		}
	}
}

func (m *tuiModel) submitCodeGenSSOInputFromTUI(flowID, input string) tea.Cmd {
	lang := m.uiLang()
	return func() tea.Msg {
		normalized := strings.TrimSpace(input)
		if normalized == "" {
			m.logCodeGenSSO("manual submit rejected flow=%s empty token inputLen=%d", flowID, len(input))
			return views.OnboardingSSOResultMsg{FlowID: flowID, Success: false, Message: tuiText(lang, "ssoInputEmpty"), KeepOpen: true, FromManual: true}
		}
		params := m.codeGenSSOParams(flowID)
		if oauth.LooksLikeOAuthCallbackURL(normalized) {
			m.logCodeGenSSO("manual submit start flow=%s inputLen=%d mode=callback", flowID, len(input))
		} else {
			token := oauth.ExtractCodeGenTokenInput(normalized)
			m.logCodeGenSSO("manual submit start flow=%s inputLen=%d tokenLen=%d", flowID, len(input), len(token))
		}
		result, err := oauth.ResolveHeadlessCodeGenInput(normalized, params)
		if err != nil {
			m.logCodeGenSSO("manual submit failed flow=%s err=%v", flowID, err)
			return views.OnboardingSSOResultMsg{FlowID: flowID, Success: false, Message: err.Error(), KeepOpen: true, FromManual: true}
		}
		m.logCodeGenSSO("manual submit success flow=%s email=%s model=%s", flowID, result.Email, result.ModelID)
		return codeGenSSOResultMsgFromResult(lang, flowID, result, true)
	}
}

func codeGenSSOResultMsgFromResult(lang, flowID string, result oauth.CodeGenSSOResult, fromManual bool) views.OnboardingSSOResultMsg {
	return views.OnboardingSSOResultMsg{
		FlowID:        flowID,
		Success:       true,
		Message:       tuiText(lang, "codeGenSSOSuccess"),
		AccessToken:   result.AccessToken,
		BaseURL:       result.BaseURL,
		Email:         result.Email,
		ModelID:       result.ModelID,
		ContextLength: result.ContextLength,
		FromManual:    fromManual,
	}
}

func (m *tuiModel) saveAcceptedCodeGenSSOResultToConfig(msg views.OnboardingSSOResultMsg) error {
	store := commands.NewFileConfigStore(commands.ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		m.logCodeGenSSO("save load-config failed flow=%s err=%v", msg.FlowID, err)
		return fmt.Errorf("%s", tuiFormat(m.uiLang(), "loadConfigFailed", err.Error()))
	}
	result := oauth.CodeGenSSOResult{
		AccessToken:   msg.AccessToken,
		BaseURL:       msg.BaseURL,
		Email:         msg.Email,
		ModelID:       msg.ModelID,
		ContextLength: msg.ContextLength,
	}
	applyCodeGenSSOResultToConfig(&cfg, result)
	if err := store.SaveConfig(cfg); err != nil {
		m.logCodeGenSSO("save app-config failed flow=%s err=%v", msg.FlowID, err)
		return fmt.Errorf("%s", tuiFormat(m.uiLang(), "saveConfigFailed", err.Error()))
	}
	if err := configfile.WriteCodeGenSettings(result.AccessToken, result.BaseURL, result.ModelID); err != nil {
		m.logCodeGenSSO("save codegen-settings failed flow=%s err=%v", msg.FlowID, err)
		return err
	}
	m.logCodeGenSSO("save success flow=%s provider=CodeGen model=%s baseURL=%s", msg.FlowID, result.ModelID, result.BaseURL)
	return nil
}

func (m *tuiModel) logCodeGenSSO(format string, args ...interface{}) {
	if m == nil || m.app == nil || m.app.logger == nil {
		return
	}
	m.app.logger.Info("codegen sso: "+format, args...)
}

func (m *tuiModel) registerCodeGenSSOCancel(flowID string, cancel context.CancelFunc) *codeGenSSOCancel {
	flowID = strings.TrimSpace(flowID)
	if m == nil || flowID == "" || cancel == nil {
		return nil
	}
	m.ssoMu.Lock()
	defer m.ssoMu.Unlock()
	if m.ssoCancels == nil {
		m.ssoCancels = make(map[string]*codeGenSSOCancel)
	}
	if prev := m.ssoCancels[flowID]; prev != nil && prev.cancel != nil {
		prev.cancel()
		prev.closeDone()
	}
	entry := &codeGenSSOCancel{cancel: cancel, done: make(chan struct{})}
	m.ssoCancels[flowID] = entry
	return entry
}

func (entry *codeGenSSOCancel) closeDone() {
	if entry == nil || entry.done == nil {
		return
	}
	select {
	case <-entry.done:
	default:
		close(entry.done)
	}
}

func (m *tuiModel) codeGenSSODone(flowID string) <-chan struct{} {
	flowID = strings.TrimSpace(flowID)
	if m == nil || flowID == "" {
		done := make(chan struct{})
		close(done)
		return done
	}
	m.ssoMu.Lock()
	entry := m.ssoCancels[flowID]
	m.ssoMu.Unlock()
	if entry == nil || entry.done == nil {
		done := make(chan struct{})
		close(done)
		return done
	}
	return entry.done
}

func (m *tuiModel) cancelCodeGenSSOFlow(flowID string) {
	flowID = strings.TrimSpace(flowID)
	if m == nil || flowID == "" {
		return
	}
	m.ssoMu.Lock()
	entry := m.ssoCancels[flowID]
	delete(m.ssoCancels, flowID)
	m.ssoMu.Unlock()
	if entry != nil && entry.cancel != nil {
		m.logCodeGenSSO("cancel flow=%s", flowID)
		entry.cancel()
	}
	if entry != nil {
		entry.closeDone()
	}
	m.clearCodeGenSSOParams(flowID)
}

func (m *tuiModel) finishCodeGenSSOFlow(flowID string, entry *codeGenSSOCancel) {
	flowID = strings.TrimSpace(flowID)
	if entry != nil && entry.cancel != nil {
		entry.cancel()
	}
	if entry != nil {
		entry.closeDone()
	}
	if m == nil || flowID == "" {
		return
	}
	m.ssoMu.Lock()
	if m.ssoCancels[flowID] == entry {
		delete(m.ssoCancels, flowID)
	}
	m.ssoMu.Unlock()
	m.clearCodeGenSSOParams(flowID)
}

func extractCodeGenSSOTokenInput(input string) string {
	return oauth.ExtractCodeGenTokenInput(input)
}

func (m *tuiModel) storeCodeGenSSOParams(flowID string, params *oauth.HeadlessOAuthParams) {
	flowID = strings.TrimSpace(flowID)
	if m == nil || flowID == "" || params == nil {
		return
	}
	m.ssoMu.Lock()
	defer m.ssoMu.Unlock()
	if m.ssoParams == nil {
		m.ssoParams = make(map[string]*oauth.HeadlessOAuthParams)
	}
	m.ssoParams[flowID] = params
}

func (m *tuiModel) codeGenSSOParams(flowID string) *oauth.HeadlessOAuthParams {
	flowID = strings.TrimSpace(flowID)
	if m == nil || flowID == "" {
		return nil
	}
	m.ssoMu.Lock()
	defer m.ssoMu.Unlock()
	return m.ssoParams[flowID]
}

func (m *tuiModel) clearCodeGenSSOParams(flowID string) {
	flowID = strings.TrimSpace(flowID)
	if m == nil || flowID == "" {
		return
	}
	m.ssoMu.Lock()
	defer m.ssoMu.Unlock()
	delete(m.ssoParams, flowID)
}

func applyCodeGenSSOResultToConfig(cfg *corelib.AppConfig, result oauth.CodeGenSSOResult) {
	models := make([]string, 0, len(result.Models))
	for _, model := range result.Models {
		models = appendUniqueString(models, model.ID)
	}
	provider := corelib.MaclawLLMProvider{
		Name:          "CodeGen",
		URL:           strings.TrimSpace(result.BaseURL),
		Key:           strings.TrimSpace(result.AccessToken),
		Model:         strings.TrimSpace(result.ModelID),
		Protocol:      "openai",
		AuthType:      "sso",
		ContextLength: result.ContextLength,
		AgentType:     "openclaw",
		Models:        models,
	}
	if provider.ContextLength <= 0 {
		provider.ContextLength = corelib.DefaultContextTokens
	}
	if provider.TimeoutSec <= 0 {
		provider.TimeoutSec = corelib.DefaultLLMTimeoutSec
	}
	if provider.URL == "" {
		provider.URL = oauth.CodeGenBaseURL
	}
	found := false
	for i := range cfg.MaclawLLMProviders {
		if strings.EqualFold(strings.TrimSpace(cfg.MaclawLLMProviders[i].Name), "CodeGen") {
			cfg.MaclawLLMProviders[i] = provider
			found = true
			break
		}
	}
	if !found {
		cfg.MaclawLLMProviders = append([]corelib.MaclawLLMProvider{provider}, cfg.MaclawLLMProviders...)
	}
	cfg.MaclawLLMCurrentProvider = "CodeGen"
	cfg.MaclawLLMUrl = provider.URL
	cfg.MaclawLLMKey = provider.Key
	cfg.MaclawLLMModel = provider.Model
	cfg.MaclawLLMProtocol = provider.Protocol
	cfg.MaclawLLMContextLength = provider.ContextLength
	cfg.MaclawLLMTimeoutSec = provider.TimeoutSec
}

func appendUniqueString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func (m *tuiModel) startWeixinFromTUI() tea.Cmd {
	lang := m.uiLang()
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
		qr = strings.TrimSpace(qr)
		token = strings.TrimSpace(token)
		if qr == "" || token == "" {
			return views.OnboardingWeixinQRMsg{Success: false, Message: tuiText(lang, "weixinQREmpty")}
		}
		return views.OnboardingWeixinQRMsg{Success: true, QR: qr, Token: token}
	}
}

func (m *tuiModel) pollWeixinFromTUI(token string) tea.Cmd {
	lang := m.uiLang()
	return func() tea.Msg {
		if strings.TrimSpace(token) == "" {
			return views.OnboardingWeixinPollResultMsg{Status: "error", Message: tuiText(lang, "weixinQREmpty"), Completed: true}
		}
		time.Sleep(1 * time.Second)
		cfg := m.app.appConfig
		baseURL := strings.TrimSpace(cfg.WeixinBaseURL)
		if baseURL == "" {
			baseURL = weixin.DefaultBaseURL
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		result, status, err := weixin.PollQRStatus(ctx, baseURL, token)
		if err != nil {
			return views.OnboardingWeixinPollResultMsg{Token: token, Status: "error", Message: err.Error(), Completed: !weixin.IsQRLoginRetryableError(err)}
		}
		msg := tuiWeixinQRStatusMessage(lang, status, result)
		if status == weixin.QRLoginStatusConfirmed {
			if result == nil || !result.Connected {
				return views.OnboardingWeixinPollResultMsg{Token: token, Status: status.String(), Message: msg, Completed: true}
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
				return views.OnboardingWeixinPollResultMsg{Token: token, Status: status.String(), Message: tuiFormat(lang, "saveConfigFailed", err.Error()), Completed: true}
			}
			m.app.appConfig = cfg
			return views.OnboardingWeixinPollResultMsg{Token: token, Status: status.String(), Message: tuiText(lang, "weixinBoundShort"), Success: true, Completed: true, AccountID: result.AccountID}
		}
		if status == weixin.QRLoginStatusExpired {
			return views.OnboardingWeixinPollResultMsg{Token: token, Status: status.String(), Message: msg, Completed: true}
		}
		statusText := status.String()
		return views.OnboardingWeixinPollResultMsg{Token: token, Status: statusText, Message: msg, Completed: tuiWeixinQRStatusIsTerminal(statusText)}
	}
}

func tuiWeixinQRStatusIsTerminal(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "error", "failed", "fail", "cancelled", "canceled":
		return true
	default:
		return false
	}
}

func tuiWeixinQRStatusMessage(lang string, status weixin.QRLoginStatus, result *weixin.QRLoginResult) string {
	switch status {
	case weixin.QRLoginStatusWait:
		return tuiText(lang, "weixinWaitingScan")
	case weixin.QRLoginStatusScanned:
		return tuiText(lang, "weixinScannedConfirm")
	case weixin.QRLoginStatusConfirmed:
		if result != nil && result.Connected {
			return tuiText(lang, "weixinBoundShort")
		}
	case weixin.QRLoginStatusExpired:
		return tuiText(lang, "weixinQRExpired")
	}
	if result != nil && strings.TrimSpace(result.Message) != "" {
		return result.Message
	}
	statusText := status.String()
	if tuiWeixinQRStatusIsTerminal(statusText) {
		return tuiText(lang, "weixinQRFailed")
	}
	return statusText
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
	tuiHubServiceProviderName = "MaClaw\u5b98\u65b9"
	tuiHubServiceAutoModel    = "auto"
)

func tuiHubServiceProviderNameIsOfficial(name string) bool {
	switch strings.TrimSpace(name) {
	case tuiHubServiceProviderName, "MaClaw Official", "MaClaw\u7039\u6a3b\u67df":
		return true
	default:
		return false
	}
}

func tuiCanonicalHubServiceProviderName(name string) string {
	if tuiHubServiceProviderNameIsOfficial(name) {
		return tuiHubServiceProviderName
	}
	return name
}

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
		if !tuiHubServiceProviderNameIsOfficial(cfg.MaclawLLMProviders[i].Name) {
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
	// activeLLM is set by RouteTurn so mid-loop GetLLMConfig keeps cost-route model.
	activeLLM tuiActiveLLM

	// phasePromptOverride is set when the workflow engine wants the agent
	// loop to generate a phase document. It's appended to the system prompt.
	phasePromptOverride string
	// lastPromptProfile is set during BuildSystemPrompt for Turn meta + tool filter alignment.
	lastPromptProfile     agent.PromptProfile
	lastPromptSavedTokens int
	// lastPromptABSample is set when quality A/B forced full on a light-eligible turn.
	lastPromptABSample bool
	// lastPromptSoftFull is set when SoftFullAgentIntent upgraded light→full.
	lastPromptSoftFull bool
	// forceFullPrompt is set by UpgradeLightPromptToFull (light tool-deny recovery).
	forceFullPrompt bool
	// history is pre-turn conversation for multi-turn knowledge auto-recall.
	history []agent.ConversationEntry
	// MoA council pin for this loop.
	moaPreset     *moa.ResolvedPreset
	moaAuto       bool
	lastUserText  string
	lastRoute     agent.RouteDecision
}

// CurrentPromptProfile implements agent.PromptProfileProvider for light-tool deny.
func (c *tuiCallbacks) CurrentPromptProfile() agent.PromptProfile {
	if c == nil {
		return agent.PromptProfileFull
	}
	if c.forceFullPrompt {
		return agent.PromptProfileFull
	}
	return c.lastPromptProfile
}

// UpgradeLightPromptToFull implements agent.LightProfileUpgrader.
func (c *tuiCallbacks) UpgradeLightPromptToFull(reason string) bool {
	if c == nil {
		return false
	}
	if c.forceFullPrompt || c.lastPromptProfile == agent.PromptProfileFull {
		// Already full — still true so RunLoop can refresh tools if needed.
		c.forceFullPrompt = true
		c.lastPromptProfile = agent.PromptProfileFull
		return true
	}
	if !c.lastPromptProfile.IsLight() {
		return false
	}
	c.forceFullPrompt = true
	c.lastPromptProfile = agent.PromptProfileFull
	log.Printf("[tui] light→full prompt upgrade reason=%s", reason)
	return true
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
	return c.activeLLM.get(c.app.llmConfig)
}

// RouteTurn implements agent.TurnRouter — cost-route / model_routes / aux.
func (c *tuiCallbacks) RouteTurn(userText string) (corelib.MaclawLLMConfig, agent.RouteDecision, bool) {
	if c == nil || c.app == nil {
		return corelib.MaclawLLMConfig{}, agent.RouteDecision{}, false
	}
	c.lastUserText = userText
	cfg, d, ok := c.app.routeTurn(userText, llm.ClassifyHints{})
	if ok {
		c.activeLLM.set(cfg)
		c.lastRoute = d
	}
	return cfg, d, ok
}

func (c *tuiCallbacks) GetMaxIterations() int {
	return config.EffectiveMaxIterations(c.app.appConfig.MaclawAgentMaxIterations)
}

func (c *tuiCallbacks) BuildSystemPrompt(userText string, isFirstTurn bool) string {
	deps := c.app.buildSystemPromptDeps()
	// Multi-turn knowledge auto-recall: blend prior user turns when history is available.
	if c.app != nil && c.app.knowledgeStore != nil {
		prior := agent.PriorUserMessagesFromHistory(c.history, agent.KnowledgeAutoRecallPriorUserTurns)
		deps.KnowledgeAutoRecall = func(b *strings.Builder, userMsg string) {
			c.app.appendKnowledgeAutoRecall(b, userMsg, prior)
		}
	}
	// Adaptive system prompt: light turns skip coding/SSH/MCP bulk.
	// Workflow phase generation always needs the full policy surface.
	profile, classified := c.resolvePromptProfile(userText)
	c.lastPromptProfile = profile
	c.lastPromptABSample = agent.IsQualityABReason(classified.Reason)
	c.lastPromptSoftFull = agent.IsSoftFullUpgradeReason(classified.Reason)
	deps.Config.PromptProfile = c.lastPromptProfile
	fullTok, lightTok := 0, 0
	c.lastPromptSavedTokens = 0
	if c.lastPromptProfile.IsLight() {
		fullTok, lightTok = agent.EstimatePromptProfileTokens(deps, userText, isFirstTurn)
		if fullTok > lightTok {
			c.lastPromptSavedTokens = fullTok - lightTok
		}
	}
	// Skip re-recording when rebuilding after mid-loop light→full upgrade.
	if !(c.forceFullPrompt && strings.Contains(classified.Reason, "tool-deny upgrade")) {
		agent.RecordPromptProfileDecision(agent.PromptProfileDecision{
			Profile:     c.lastPromptProfile,
			FullTokens:  fullTok,
			LightTokens: lightTok,
			Task:        string(classified.Task),
			Reason:      classified.Reason,
		})
	}
	prompt := agent.BuildSystemPrompt(deps, userText, isFirstTurn)

	// Inject workflow phase prompt when the agent loop runs on behalf of
	// the workflow engine (e.g., generating a requirements document).
	if c.phasePromptOverride != "" {
		prompt += "\n" + c.phasePromptOverride
	}

	return prompt
}

func (c *tuiCallbacks) resolvePromptProfile(userText string) (agent.PromptProfile, llm.ClassifyResult) {
	if c.forceFullPrompt {
		return agent.PromptProfileFull, llm.ClassifyResult{
			Task:   llm.TaskReasoning,
			Reason: "force full after light tool-deny upgrade",
		}
	}
	if c.phasePromptOverride != "" {
		return agent.PromptProfileFull, llm.ClassifyResult{
			Task:   llm.TaskDefault,
			Reason: "workflow phase override",
		}
	}
	return agent.ResolvePromptProfile(userText, llm.ClassifyHints{})
}

func (c *tuiCallbacks) BuildTools(userText string) []map[string]interface{} {
	defs := c.app.toolRegistry.BuildDefinitions()
	// Align tool surface with light system prompt (no bash/coding/files).
	profile, _ := c.resolvePromptProfile(userText)
	if profile.IsLight() {
		return agent.FilterToolDefsForLightTurn(defs)
	}
	return defs
}

func (c *tuiCallbacks) ExecuteTool(name, argsJSON string) string {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return tuiFormat(tuiConfigLang(c.app.appConfig), "toolArgParseFailed", err.Error())
	}
	start := time.Now()
	ctx, cancel := contextFromCancelCh(c.cancelCh)
	defer cancel()
	// Inject context into args so handlers that support cancellation (e.g.
	// manage_skill's skillRunDetailed) can extract it via the "_ctx" key.
	// This bridges the gap between the plain Handler signature (which doesn't
	// accept context) and the agent loop's cancel signal.
	args["_ctx"] = ctx
	result := c.app.toolRegistry.ExecuteCtx(ctx, name, args)
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
	if c == nil {
		return true, ""
	}
	return c.app.isWorkflowToolCallAllowedTUI(name, argsJSON)
}

func (app *TUIApp) isWorkflowToolAllowedTUI(name string) bool {
	if app == nil {
		return true
	}
	if app.isWorkflowPhaseExecutionBlockedTUI() {
		return false
	}
	if phase := app.activeWorkflowPhaseTUI(); v2.IsArtifactPhase(phase) {
		return v2.IsToolAllowedInArtifactPhase(name)
	}
	return v2.IsToolAllowedByPolicy(app.currentWorkflowToolFilterTUI(), name)
}

func (app *TUIApp) isWorkflowToolCallAllowedTUI(name, argsJSON string) (bool, string) {
	if app == nil {
		return true, ""
	}
	if app.isWorkflowPhaseExecutionBlockedTUI() {
		return false, "current workflow phase is waiting for required input or review; tool execution is paused"
	}
	var args map[string]interface{}
	if strings.TrimSpace(argsJSON) != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return false, tuiFormat(tuiConfigLang(app.appConfig), "toolArgParseFailed", err.Error())
		}
	}
	if args == nil {
		args = map[string]interface{}{}
	}
	if phase := app.activeWorkflowPhaseTUI(); v2.IsArtifactPhase(phase) {
		if err := v2.ValidateArtifactPhaseToolCall(name, args); err != nil {
			return false, err.Error()
		}
	}
	var approved []v2.OpsApprovedCommand
	// Read approved commands from V2 state machine's phase outputs.
	if wf := app.getWorkflowV2TUI(); wf != nil {
		if state := wf.machine.GetActive("tui-user"); state != nil {
			outputs := state.PreviousOutputs(0)
			approved = v2.ExtractOpsApprovedCommands(outputs["risk_policy"])
		}
	}
	// Use policy-based validation directly.
	if err := v2.ValidateToolCallByPolicyWithApproval(app.currentWorkflowToolFilterTUI(), strings.TrimSpace(name), args, approved); err != nil {
		return false, err.Error()
	}
	return true, ""
}

func (app *TUIApp) activeWorkflowPhaseTUI() *v2.Phase {
	if app == nil {
		return nil
	}
	wf := app.getWorkflowV2TUI()
	if wf == nil || wf.machine == nil {
		return nil
	}
	state := wf.machine.GetActive("tui-user")
	if state == nil {
		return nil
	}
	return state.ActivePhase()
}

func (app *TUIApp) isWorkflowPhaseExecutionBlockedTUI() bool {
	if app == nil {
		return false
	}
	// Check V2: phase is waiting for confirmation → execution blocked.
	if wf := app.getWorkflowV2TUI(); wf != nil {
		if state := wf.machine.GetActive("tui-user"); state != nil {
			if state.IsWaitingConfirm() {
				return true
			}
		}
	}
	return false
}

func (app *TUIApp) currentWorkflowToolFilterTUI() v2.ToolFilterPolicy {
	if app == nil {
		return v2.ToolFilterNone
	}
	// V2 state machine is the sole source for tool filter policy.
	return app.currentWorkflowToolFilterV2()
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

// EarlyStop implements agent.EarlyStopper (daily LLM budget).
func (c *tuiCallbacks) EarlyStop() (bool, string, string) {
	if c == nil || c.app == nil {
		return false, "", ""
	}
	return c.app.earlyStopBudget()
}

// OnLLMUsage implements agent.LLMUsageRecorder.
func (c *tuiCallbacks) OnLLMUsage(model string, inputTokens, outputTokens int) {
	if c == nil || c.app == nil {
		return
	}
	c.app.recordLLMCost(model, inputTokens, outputTokens)
}

// ensureCostTracker lazily creates a CostTracker (pipe/rpc may not set it at init).
func (app *TUIApp) ensureCostTracker() *llm.CostTracker {
	if app == nil {
		return nil
	}
	app.costTrackerMu.Lock()
	defer app.costTrackerMu.Unlock()
	if app.costTracker == nil {
		app.costTracker = llm.NewCostTracker(app.appConfig.DailyLLMBudgetUSD)
	}
	return app.costTracker
}

// recordLLMCost charges the shared CostTracker (durable fleet slot).
func (app *TUIApp) recordLLMCost(model string, inputTokens, outputTokens int) {
	ct := app.ensureCostTracker()
	if ct == nil {
		return
	}
	if model == "" {
		model = app.llmConfig.Model
	}
	cost := ct.Record(model, inputTokens, outputTokens)
	if cost > 0.01 {
		log.Printf("[cost] tui %s: in=%d out=%d cost=$%.4f", model, inputTokens, outputTokens, cost)
	}
}

// earlyStopBudget returns stop when daily budget is exceeded (fleet-aware).
func (app *TUIApp) earlyStopBudget() (bool, string, string) {
	ct := app.ensureCostTracker()
	if ct == nil {
		return false, "", ""
	}
	// Keep budget in sync if config was reloaded.
	if b := app.appConfig.DailyLLMBudgetUSD; b != ct.BudgetLimit() {
		ct.SetBudget(b)
	}
	if ct.BudgetLimit() <= 0 {
		return false, "", ""
	}
	if ct.IsOverBudget() {
		msg := ct.BudgetGateMessage()
		log.Printf("[cost] tui budget hard-stop: %s", ct.DailySummary())
		return true, "daily_llm_budget_exceeded", msg
	}
	if ct.ShouldWarn() {
		log.Printf("[cost] tui budget warning: %s", ct.DailySummary())
	}
	return false, "", ""
}

// ---------------------------------------------------------------------------
// tuiBtwCallbacks implements agent.LoopCallbacks for /btw side queries.
// Minimal tool set: web_search, web_fetch, read_file, memory (read-only).
// Independent conversation 鈥?does not pollute the main chat history.
// ---------------------------------------------------------------------------

var tuiBtwToolNames = map[string]bool{
	"web_search":   true,
	"web_fetch":    true,
	"read_file":    true,
	"memory":       true,
	"agent_status": true,
}

type tuiBtwCallbacks struct {
	app       *TUIApp
	program   *tea.Program
	stopped   bool
	cancelCh  chan struct{}
	activeLLM tuiActiveLLM

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
	return c.activeLLM.get(c.app.llmConfig)
}

func (c *tuiBtwCallbacks) RouteTurn(userText string) (corelib.MaclawLLMConfig, agent.RouteDecision, bool) {
	if c == nil || c.app == nil {
		return corelib.MaclawLLMConfig{}, agent.RouteDecision{}, false
	}
	cfg, d, ok := c.app.routeTurn(userText, llm.ClassifyHints{})
	if ok {
		c.activeLLM.set(cfg)
	}
	return cfg, d, ok
}

func (c *tuiBtwCallbacks) GetMaxIterations() int {
	// Use MinAgentIterations (30) 鈥?EffectiveMaxIterations enforces a floor
	// of 30. Side queries typically finish in 3-5 iterations.
	return config.EffectiveMaxIterations(config.MinAgentIterations)
}

func (c *tuiBtwCallbacks) BuildSystemPrompt(userText string, isFirstTurn bool) string {
	// Build a focused system prompt for /btw: identity + read-only memory context.
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
			fmt.Fprintf(&b, "Your self-understanding (from memory): %s\nYour underlying system name is %s.\n", selfIdentity, roleName)
		}
	} else {
		if lang == "en" {
			fmt.Fprintf(&b, "You are %s, %s.\n", roleName, roleDesc)
		} else {
			fmt.Fprintf(&b, "You are %s, %s.\n", roleName, roleDesc)
		}
	}

	// /btw mode.
	b.WriteString(tuiBtwSuffixForLang(lang))

	// User fact summary.
	if app.memoryStore != nil {
		b.WriteString(app.memoryStore.UserFactSummaryForPrompt(memory.UserFactTemplatePromptOptions(tuiBtwSectionFormat(lang, "userInfo"))))
	}

	// Proactive memory recall (read-only, no side effects).
	if app.memoryStore != nil && userText != "" {
		promptContext, _ := app.memoryStore.ProactiveContextForPrompt(userText, memory.BtwProactivePromptOptions("", tuiBtwSectionHeader(lang, "relevantMemory")))
		b.WriteString(promptContext)
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

	// Mechanism-level enforcement: memory tool is read-only in /btw.
	if name == "memory" {
		action, _ := args["action"].(string)
		if !memory.NormalizeMemoryToolAction(action).IsRecallOnlyAllowed() {
			return tuiText(tuiConfigLang(c.app.appConfig), "btwMemoryReadOnly")
		}
	}

	// agent_status: query TUI runtime state directly.
	if name == "agent_status" {
		return c.app.tuiAgentStatus(args)
	}

	// Delegate to the shared tool registry.
	ctx, cancel := contextFromCancelCh(c.cancelCh)
	defer cancel()
	args["_ctx"] = ctx
	result := c.app.toolRegistry.ExecuteCtx(ctx, name, args)

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

func (c *tuiBtwCallbacks) EarlyStop() (bool, string, string) {
	if c == nil || c.app == nil {
		return false, "", ""
	}
	return c.app.earlyStopBudget()
}

func (c *tuiBtwCallbacks) OnLLMUsage(model string, inputTokens, outputTokens int) {
	if c == nil || c.app == nil {
		return
	}
	c.app.recordLLMCost(model, inputTokens, outputTokens)
}

func tuiBtwSectionFormat(lang, key string) string {
	if lang == "en" {
		if key == "userInfo" {
			return "\n## User Information\n%s\n"
		}
	}
	return "\n## 鐢ㄦ埛淇℃伅\n%s\n"
}

func tuiBtwSectionHeader(lang, key string) string {
	if lang == "en" {
		if key == "relevantMemory" {
			return "\n## Relevant Memory (Auto-recalled)\n"
		}
	}
	return "\n## 鐩稿叧璁板繂锛堣嚜鍔ㄥ彫鍥烇級\n"
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
4. If the question involves previous conversation or memory, use memory(action="recall").
5. Keep the answer concise, structured, and direct.
6. Include URLs when citing web sources.
7. This is read-only: do not modify files.
8. Try to finish within 2-3 tool turns.
`

const tuiBtwSuffix = `
## /btw side query mode

You are handling a /btw side query. This is an independent, single-turn quick query, not part of the main task.

Rules:
1. If the user asks about task progress or runtime status, use the agent_status tool first.
2. Use web_search for fresh information, then web_fetch for details.
3. If the question involves local project files, use read_file.
4. If the question involves previous conversation or memory, use memory(action="recall").
5. Keep the answer concise, structured, and direct.
6. Include URLs when citing web sources.
7. This is read-only: do not modify files.
8. Try to finish within 2-3 tool turns.
`

// tuiAgentStatusToolDef is the inline tool definition for agent_status in TUI.
// Defined once, used in both the registry path and the fallback path.
var tuiAgentStatusToolDef_ = tuiBtwToolDef("agent_status",
	"Query runtime agent status, including SSH sessions. Use when the user asks about task progress or background activity.",
	map[string]interface{}{
		"category": map[string]string{"type": "string", "description": "Query category: all or ssh_sessions. Default: all"},
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
		tuiBtwToolDef("web_search", "Search the web for fresh information",
			map[string]interface{}{
				"query":       map[string]string{"type": "string", "description": "Search keywords"},
				"max_results": map[string]string{"type": "integer", "description": "Maximum result count; default 8"},
			}, []string{"query"}),
		tuiBtwToolDef("web_fetch", "Fetch a URL and extract readable page content",
			map[string]interface{}{
				"url":       map[string]string{"type": "string", "description": "URL to fetch"},
				"max_chars": map[string]string{"type": "integer", "description": "Maximum returned characters"},
			}, []string{"url"}),
		tuiBtwToolDef("read_file", "Read a local file",
			map[string]interface{}{
				"path":   map[string]string{"type": "string", "description": "File path"},
				"lines":  map[string]string{"type": "integer", "description": "Line count"},
				"offset": map[string]string{"type": "integer", "description": "Offset from end"},
			}, []string{"path"}),
		tuiBtwToolDef("memory", "Query long-term memory",
			map[string]interface{}{
				"action": map[string]string{"type": "string", "description": "Action: recall"},
				"query":  map[string]string{"type": "string", "description": "Query keywords"},
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
