package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentnet"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/i18n"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/memoryshot"
	"github.com/RapidAI/CodeClaw/corelib/pyenv"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	"github.com/RapidAI/CodeClaw/corelib/security"
	"github.com/RapidAI/CodeClaw/corelib/steering"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
	"github.com/RapidAI/CodeClaw/tui/commands"
	"github.com/RapidAI/CodeClaw/tui/views"
	tea "github.com/charmbracelet/bubbletea"
)

// TUIApp 是 Bubble Tea 的顶层 Model，持有 Kernel 和 UI 状态。
type TUIApp struct {
	kernel        *corelib.Kernel
	bridge        *BubbleTeaEventBridge
	logger        *TUILogger
	qqBotMgr      *tuiQQBotManager
	telegramMgr   *tuiTelegramManager
	loopMgr       *agent.BackgroundLoopManager
	configWatcher *ConfigWatcher
	sessionMgr    *TUISessionManager

	// 安全与路由组件
	firewall       *security.Firewall
	auditLog       *security.AuditLog
	sessionMonitor *remote.SessionMonitor
	statusCh       chan agent.StatusEvent
	defGenerator   *tool.DefinitionGenerator
	router         *tool.Router
	selector       *tool.Selector
	configMgr      *config.Manager
	memoryStore    *memory.Store
	memPipeline    *memory.Pipeline
	schedulerMgr   *scheduler.Manager
	AgentNetClient *agentnet.Client
	memShotMgr     *memoryshot.Manager

	// AI 助手聊天
	chatHistory []memoryshot.ChatMessage
	llmClient   *http.Client

	// Gossip 聊天八卦自动发帖
	gossipDetector *TUIGossipDetector

	// Workflow engine (corelib/workflow)
	workflowEngine *workflow.WorkflowEngine

	// Steering store (declarative rule injection)
	steeringStore *steering.Store

	// Tool usage tracker (outcome learning)
	usageTracker *tool.UsageTracker

	// Bubble Tea program reference for sending async messages
	program *tea.Program

	root  views.RootModel
	ready bool
	err   error
}

// kernelStartedMsg 内核启动完成的消息。
type kernelStartedMsg struct{ err error }

// kernelEventMsg 内核事件转发到 Bubble Tea 的消息。
type kernelEventMsg struct {
	eventType string
	data      interface{}
}

// sessionMonitorMsg 会话监控状态变更消息。
type sessionMonitorMsg struct {
	eventType string
	sessionID string
	message   string
}

// sessionUpdateMsg 会话状态变更消息。
type sessionUpdateMsg struct {
	sessionID string
}

// pythonEnvMsg Python 环境检测/安装完成消息。
type pythonEnvMsg struct {
	status pyenv.Status
}

func copyChatHistory(history []memoryshot.ChatMessage) []memoryshot.ChatMessage {
	if len(history) == 0 {
		return nil
	}
	cloned := make([]memoryshot.ChatMessage, len(history))
	copy(cloned, history)
	return cloned
}

func chatHistoryToViewMessages(history []memoryshot.ChatMessage) []views.ChatMessage {
	if len(history) == 0 {
		return nil
	}
	msgs := make([]views.ChatMessage, 0, len(history))
	for _, msg := range history {
		msgs = append(msgs, views.ChatMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}
	return msgs
}

func (a *TUIApp) syncChatHistoryToMemoryShot(save bool) {
	if a.memShotMgr == nil {
		return
	}
	a.memShotMgr.UpdateChatHistory(copyChatHistory(a.chatHistory))
	if save {
		_ = a.memShotMgr.Save()
	}
}

func (a *TUIApp) shutdown() {
	if a.qqBotMgr != nil {
		a.qqBotMgr.Stop()
	}
	if a.telegramMgr != nil {
		a.telegramMgr.Stop()
	}
	if a.configWatcher != nil {
		a.configWatcher.Stop()
	}
	if a.sessionMonitor != nil {
		a.sessionMonitor.Close()
	}
	if a.schedulerMgr != nil {
		a.schedulerMgr.Stop()
	}
	if a.memPipeline != nil {
		a.memPipeline.Stop()
	}
	if a.memoryStore != nil {
		a.memoryStore.Stop()
	}
	a.syncChatHistoryToMemoryShot(true)
	if a.memShotMgr != nil {
		a.memShotMgr.Stop()
	}
	if a.auditLog != nil {
		_ = a.auditLog.Close()
	}
	if a.usageTracker != nil {
		_ = a.usageTracker.Save()
	}
	if a.kernel != nil {
		ctx := context.Background()
		_ = a.kernel.Shutdown(ctx)
	}
}

// NewTUIApp 创建 TUI 应用实例。
func NewTUIApp() *TUIApp {
	lang := defaultTUILang()
	return &TUIApp{
		root:           views.NewRootModel(lang),
		gossipDetector: NewTUIGossipDetector(),
	}
}

// defaultTUILang 从配置读取 TUI 语言，失败时回退默认语言。
func defaultTUILang() string {
	store := commands.NewFileConfigStore(commands.ResolveDataDir())
	if cfg, err := store.LoadConfig(); err == nil {
		return i18n.NormalizeLang(cfg.Language)
	}
	return i18n.NormalizeLang("")
}

// Init 实现 tea.Model 接口。
func (a *TUIApp) Init() tea.Cmd {
	return a.initKernel
}

// initKernel 在后台初始化内核。
func (a *TUIApp) initKernel() tea.Msg {
	logger := NewTUILogger()
	// TUI 模式下必须将日志写入文件，避免 stderr 输出渗透到 Bubble Tea alt-screen。
	dataDir := commands.ResolveDataDir()
	tuiLogPath := filepath.Join(dataDir, "logs", "maclaw.log")
	_ = os.MkdirAll(filepath.Dir(tuiLogPath), 0o755)
	if err := logger.SetLogFile(tuiLogPath); err != nil {
		// logFile 设置失败时静默降级，不写 stderr
	}
	a.logger = logger

	bridge := NewBubbleTeaEventBridge()
	a.bridge = bridge

	opts := buildKernelOptions(logger, bridge)
	kernel, err := corelib.NewKernel(opts)
	if err != nil {
		return kernelStartedMsg{err: err}
	}
	a.kernel = kernel

	// 初始化后台任务管理器
	a.loopMgr = agent.NewBackgroundLoopManager(nil)

	// 初始化会话管理器
	a.sessionMgr = NewTUISessionManager()

	// 在后台启动内核事件循环
	go func() {
		ctx := context.Background()
		if err := kernel.Run(ctx); err != nil {
			logger.Error("kernel run error: %v", err)
		}
	}()

	// 启动 QQ Bot 网关（转发模式）
	a.qqBotMgr = newTUIQQBotManager(logger)
	go a.qqBotMgr.SyncFromConfig()

	// 启动 Telegram 网关（转发模式）
	a.telegramMgr = newTUITelegramManager(logger)
	go a.telegramMgr.SyncFromConfig()

	// 启动配置文件监听
	cw, cwErr := NewConfigWatcher(logger)
	if cwErr != nil {
		logger.Error("config watcher init failed: %v", cwErr)
	} else {
		a.configWatcher = cw
		cw.Start()
	}

	// --- 新增：安全组件 ---
	dataDir = commands.ResolveDataDir()
	riskAnalyzer := security.NewRiskAnalyzer()
	policyEngine := security.NewPolicyEngine()
	auditLogDir := filepath.Join(dataDir, "audit")
	auditLog, auditErr := security.NewAuditLog(auditLogDir)
	if auditErr != nil {
		logger.Error("audit log init failed: %v", auditErr)
	}
	a.auditLog = auditLog

	var fw *security.Firewall
	if auditLog != nil {
		fw = security.NewFirewall(riskAnalyzer, policyEngine, auditLog)
	} else {
		fw = security.NewFirewall(riskAnalyzer, policyEngine, nil)
	}
	// onAsk 回调：TUI 模式下默认允许（非交互式 agent 循环）
	fw.SetOnAsk(func(toolName string, risk security.RiskAssessment) (bool, error) {
		// TUI agent 循环中无法交互式确认，高风险操作默认拒绝
		if risk.Level == security.RiskCritical {
			return false, nil
		}
		return true, nil
	})
	a.firewall = fw

	// --- 新增：SessionMonitor ---
	statusCh := make(chan agent.StatusEvent, 32)
	sessionMonitor := remote.NewSessionMonitor(a.sessionMgr, statusCh, 20*time.Second)
	a.sessionMonitor = sessionMonitor
	a.statusCh = statusCh

	// --- 新增：ConfigManager ---
	store := commands.NewFileConfigStore(dataDir)
	a.configMgr = config.NewManager(store)

	// --- 新增：MemoryStore ---
	memPath := filepath.Join(dataDir, "memories.json")
	memStore, memErr := memory.NewStore(memPath)
	if memErr != nil {
		logger.Error("memory store init failed: %v", memErr)
	}
	a.memoryStore = memStore

	// --- Embedding: try GemmaEmbedder, fall back to Noop ---
	if memStore != nil {
		modelPath := embedding.DefaultModelPath()
		emb := embedding.NewDefaultEmbedder(modelPath)
		memStore.SetEmbedder(emb)
	}

	// --- Memory Pipeline (compress → promote → reflect → consolidate) ---
	if memStore != nil {
		compressor := memory.NewCompressor(memStore, nil, nil)
		// Wire up all pipeline components. LLM-dependent components gracefully
		// skip when LLM is nil/unconfigured. Step 0 (decay) always runs.
		promoter := memory.NewPromoter(memStore, nil)
		reflector := memory.NewReflector(memStore, nil)
		a.memPipeline = memory.NewPipeline(memStore, compressor, promoter, reflector, nil)
		// Attach TiMem consolidators.
		consolidator := memory.NewConsolidator(memStore, memStore.TMT(), nil)
		profiler := memory.NewProfileConsolidator(memStore, memStore.TMT(), nil)
		a.memPipeline.SetConsolidator(consolidator, profiler)
		// Attach TiMem recall gating for post-retrieval LLM filtering.
		memStore.SetRecallGating(memory.NewRecallGating(nil))
		a.memPipeline.Start()
	}

	// --- 新增：SchedulerManager ---
	schedPath := filepath.Join(dataDir, "scheduled_tasks.json")
	schedMgr, schedErr := scheduler.NewManager(schedPath)
	if schedErr != nil {
		logger.Error("scheduler init failed: %v", schedErr)
	} else {
		schedMgr.Start()
	}
	a.schedulerMgr = schedMgr

	// --- 新增：AgentNet Client ---
	a.AgentNetClient = agentnet.NewClient()

	// --- 新增：Selector ---
	a.selector = tool.NewSelector()

	// --- 新增：DefinitionGenerator + Router ---
	builtinDefs := (&TUIAgentHandler{sessionMgr: a.sessionMgr}).buildBuiltinToolDefinitions()
	defGen := tool.NewDefinitionGenerator(nil, builtinDefs)
	a.defGenerator = defGen
	a.router = tool.NewRouter(defGen)

	// --- IntentClassifier (hybrid intent detection) ---
	if memStore != nil {
		emb := memStore.Embedder()
		if emb != nil {
			ic := tool.NewIntentClassifier(emb)
			a.router.SetIntentClassifier(ic)
		}
	}

	// --- 新增：UsageTracker (Tool Outcome Learning) ---
	trackerPath := tool.DefaultUsageTrackerPath()
	if trackerPath != "" {
		tracker, trackerErr := tool.NewUsageTracker(trackerPath)
		if trackerErr != nil {
			logger.Error("usage tracker init failed: %v", trackerErr)
		} else {
			a.usageTracker = tracker
			a.router.SetUsageTracker(tracker)
		}
	}

	// 启动 SessionMonitor 状态通知转发
	go func() {
		for evt := range statusCh {
			logger.Info("session monitor event: %d session=%s", evt.Type, evt.SessionID)
		}
	}()

	// --- 新增：MemoryShot Manager ---
	mshotMgr, mshotErr := memoryshot.DefaultManager()
	if mshotErr != nil {
		logger.Error("memoryshot init failed: %v", mshotErr)
	} else {
		a.memShotMgr = mshotMgr
		a.memShotMgr.Start(30 * time.Second)
		// 恢复聊天历史
		if loaded, err := mshotMgr.Load(); err == nil && loaded {
			snap := mshotMgr.GetSnapshot()
			a.chatHistory = copyChatHistory(snap.ChatHistory)
			a.root.Chat.SetMessages(chatHistoryToViewMessages(snap.ChatHistory))
			logger.Info("restored %d messages from memoryshot", len(snap.ChatHistory))
		}
	}

	// --- 新增：WorkflowEngine ---
	a.initTUIWorkflowEngine()

	// --- 新增：Steering Store ---
	a.initTUISteeringStore()

	return kernelStartedMsg{}
}

// Update 实现 tea.Model 接口，处理消息。
func (a *TUIApp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// 编辑模式下不响应全局退出快捷键
		configEditing := a.root.ActiveTab() == views.TabConfig && a.root.Config.IsEditing()
		auditFiltering := a.root.ActiveTab() == views.TabAudit && a.root.Audit.IsFiltering()
		chatFocused := a.root.ActiveTab() == views.TabChat && a.root.Chat.IsInputFocused()
		switch msg.String() {
		case "ctrl+c":
			a.shutdown()
			return a, tea.Quit
		case "q":
			if !configEditing && !auditFiltering && !chatFocused {
				a.shutdown()
				return a, tea.Quit
			}
		}

	case tea.WindowSizeMsg:
		a.ready = true

	case kernelStartedMsg:
		if msg.err != nil {
			a.err = msg.err
			a.root.StatusBar.SetMessage(i18n.Tf(i18n.MsgTUIKernelInitFailed, a.root.Lang(), msg.err))
		} else {
			a.root.StatusBar.SetMessage(i18n.T(i18n.MsgTUIReady, a.root.Lang()))
			a.root.StatusBar.SetHubStatus("disconnected")
			a.root.Sessions.SetSessions(nil) // 清除 loading 状态
			a.root.Audit.SetEntries(nil)
			a.root.AgentNet.SetPeers(nil)

			// 从配置文件加载当前值到配置视图
			a.syncConfigView()

			// 检测已安装工具
			detected := commands.DetectTools()
			var toolInfos []views.ToolInfo
			for _, dt := range detected {
				toolInfos = append(toolInfos, views.ToolInfo{
					Name:      dt.DisplayName,
					Available: dt.Available,
					Path:      dt.Path,
				})
			}
			a.root.Tools.SetTools(toolInfos)

			// 异步检测 Python 环境（不阻塞 UI）
			return a, a.checkPythonEnvCmd()
		}

	case pythonEnvMsg:
		if msg.status.Error != "" {
			a.root.StatusBar.SetMessage(i18n.Tf(i18n.MsgTUIPythonEnvError, a.root.Lang(), msg.status.Error))
		} else if msg.status.Available && msg.status.VenvReady {
			a.root.StatusBar.SetMessage(i18n.Tf(i18n.MsgTUIPythonEnvReady, a.root.Lang(), msg.status.Version, msg.status.VenvPath))
		} else if msg.status.Available {
			a.root.StatusBar.SetMessage(i18n.Tf(i18n.MsgTUIPythonAvailable, a.root.Lang(), msg.status.Version))
		}

	case kernelEventMsg:
		a.root.StatusBar.SetMessage(fmt.Sprintf("[%s] %v", msg.eventType, msg.data))

	case views.ConfigSaveMsg:
		// 通过 config.Manager 持久化到文件
		displayVal := msg.Value
		if a.configMgr != nil {
			if _, err := a.configMgr.UpdateConfig(msg.Section, msg.Key, msg.Value); err != nil {
				a.root.StatusBar.SetMessage(i18n.Tf(i18n.MsgTUIConfigSaveFailed, a.root.Lang(), msg.Key, err))
			} else {
				if isSensitiveConfigKey(msg.Key) {
					displayVal = "********"
				}
				a.root.StatusBar.SetMessage(i18n.Tf(i18n.MsgTUIConfigSaved, a.root.Lang(), msg.Key, displayVal))
			}
		} else {
			a.root.StatusBar.SetMessage(i18n.Tf(i18n.MsgTUIConfigSaved, a.root.Lang(), msg.Key, displayVal))
		}
		// Re-sync QQ Bot gateway on config change
		if a.qqBotMgr != nil && msg.Section == "qqbot" {
			go a.qqBotMgr.SyncFromConfig()
		}
		// Re-sync Telegram gateway on config change
		if a.telegramMgr != nil && msg.Section == "telegram" {
			go a.telegramMgr.SyncFromConfig()
		}

	case views.MemoryCompressMsg:
		a.root.StatusBar.SetMessage(i18n.T(i18n.MsgTUIMemoryCompressHint, a.root.Lang()))

	case views.MemoryBackupListMsg:
		a.root.StatusBar.SetMessage(i18n.T(i18n.MsgTUIMemoryBackupListHint, a.root.Lang()))

	case views.ToolRefreshMsg:
		detected := commands.DetectTools()
		var toolInfos []views.ToolInfo
		for _, dt := range detected {
			toolInfos = append(toolInfos, views.ToolInfo{
				Name:      dt.DisplayName,
				Available: dt.Available,
				Path:      dt.Path,
			})
		}
		a.root.Tools.SetTools(toolInfos)
		a.root.StatusBar.SetMessage(i18n.T(i18n.MsgTUIToolStatusRefreshed, a.root.Lang()))

	case views.ChatSendMsg:
		// 检查 LLM 是否已配置
		if _, err := loadLLMConfig(); err != nil {
			// LLM 未配置：在聊天中显示提示，切换到配置 Tab
			a.root.Chat.AppendSystemMessage(i18n.T(i18n.MsgTUILLMNotConfiguredHint, a.root.Lang()))
			a.root.SetTab(views.TabConfig)
			a.root.Config.FocusLLMConfig()
			a.root.StatusBar.SetMessage(i18n.T(i18n.MsgTUILLMNotConfiguredHint, a.root.Lang()))
			return a, nil
		}
		a.root.StatusBar.SetMessage(i18n.T(i18n.MsgTUIAIThinking, a.root.Lang()))
		if msg.AgentMode {
			return a, a.sendAgentMessage(msg.Text)
		}
		return a, a.sendChatMessage(msg.Text)

	case views.ChatClearMsg:
		a.chatHistory = nil
		if a.memShotMgr != nil {
			a.memShotMgr.ClearChatHistory()
		}
		a.syncChatHistoryToMemoryShot(true)
		if a.gossipDetector != nil {
			a.gossipDetector.ClearBuffer()
		}
		a.root.StatusBar.SetMessage(i18n.T(i18n.MsgTUIChatHistoryCleared, a.root.Lang()))

	case configChangedMsg:
		a.root.StatusBar.SetMessage(i18n.T(i18n.MsgTUIConfigReloading, a.root.Lang()))
		// 刷新配置视图
		a.syncConfigView()
		// Re-sync gateways
		if a.qqBotMgr != nil {
			go a.qqBotMgr.SyncFromConfig()
		}
		if a.telegramMgr != nil {
			go a.telegramMgr.SyncFromConfig()
		}
		// Refresh tool status
		detected := commands.DetectTools()
		var toolInfos []views.ToolInfo
		for _, dt := range detected {
			toolInfos = append(toolInfos, views.ToolInfo{
				Name:      dt.DisplayName,
				Available: dt.Available,
				Path:      dt.Path,
			})
		}
		a.root.Tools.SetTools(toolInfos)

	case toolFinishedMsg:
		if msg.err != nil {
			a.root.StatusBar.SetMessage(i18n.Tf(i18n.MsgTUIToolExitedWithError, a.root.Lang(), msg.name, msg.err))
		} else {
			a.root.StatusBar.SetMessage(i18n.Tf(i18n.MsgTUIToolExited, a.root.Lang(), msg.name))
		}

	case sessionMonitorMsg:
		a.root.StatusBar.SetMessage(i18n.Tf(i18n.MsgTUISessionMonitorEvent, a.root.Lang(), msg.eventType, msg.message))

	case sessionUpdateMsg:
		// 将会话输出同步到 SessionDetail 视图
		if a.root.SessionDetail != nil && a.sessionMgr != nil {
			s, ok := a.sessionMgr.Get(msg.sessionID)
			if ok {
				s.mu.Lock()
				lines := make([]string, len(s.PreviewLines))
				copy(lines, s.PreviewLines)
				status := string(s.Status)
				s.mu.Unlock()
				a.root.SessionDetail.SetStatus(status)
				// 追加新行（简化：重设所有行）
				for i := len(a.root.SessionDetail.GetLines()); i < len(lines); i++ {
					a.root.SessionDetail.AppendOutput(lines[i])
				}
			}
		}
	}

	// 委托给 root model
	var cmd tea.Cmd
	a.root, cmd = a.root.Update(msg)
	return a, cmd
}

// View 实现 tea.Model 接口，渲染 UI。
func (a *TUIApp) View() string {
	if !a.ready {
		return i18n.T(i18n.MsgTUIInitializing, a.root.Lang()) + " MaClaw TUI...\n"
	}
	if a.err != nil {
		return i18n.Tf(i18n.MsgTUIErrorExit, a.root.Lang(), a.err)
	}
	return a.root.View()
}

// runTUI 启动 TUI 交互模式。
func runTUI() {
	// Redirect Go standard log output to a log file so that library messages
	// (gse dictionary loading, workflow init, etc.) don't bleed into the
	// Bubble Tea alt-screen UI.
	dataDir := commands.ResolveDataDir()
	logPath := filepath.Join(dataDir, "logs", "tui.log")
	_ = os.MkdirAll(filepath.Dir(logPath), 0o755)
	if lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		log.SetOutput(lf)
		defer lf.Close()
	}

	app := NewTUIApp()
	p := tea.NewProgram(app, tea.WithAltScreen())
	app.program = p

	// 绑定 Program 到 config watcher（initKernel 后才有 configWatcher）
	go func() {
		// 等待 initKernel 完成
		for app.configWatcher == nil && app.err == nil {
			time.Sleep(50 * time.Millisecond)
			if app.ready {
				break
			}
		}
		if app.configWatcher != nil {
			app.configWatcher.SetProgram(p)
		}
	}()

	// 转发 SessionMonitor 事件到 Bubble Tea
	go func() {
		// 等待 initKernel 完成
		for app.statusCh == nil && app.err == nil {
			time.Sleep(50 * time.Millisecond)
			if app.ready {
				break
			}
		}
		if app.statusCh != nil {
			for evt := range app.statusCh {
				p.Send(sessionMonitorMsg{
					eventType: fmt.Sprintf("%d", evt.Type),
					sessionID: evt.SessionID,
					message:   evt.Message,
				})
			}
		}
	}()

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}
}

// loadLLMConfig 从本地配置文件加载 LLM 配置。
func loadLLMConfig() (corelib.MaclawLLMConfig, error) {
	llm, err := commands.LoadLLMConfig()
	if err != nil {
		return llm, err
	}
	if strings.TrimSpace(llm.URL) == "" || strings.TrimSpace(llm.Model) == "" {
		return llm, errors.New(i18n.T(i18n.MsgTUILLMNotConfiguredHint, i18n.NormalizeLang("")))
	}
	return llm, nil
}

// syncConfigView 从配置文件加载当前值到配置视图。
func (a *TUIApp) syncConfigView() {
	store := commands.NewFileConfigStore(commands.ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return
	}
	a.root.Config.LoadFromAppConfig(cfg)
	a.root.SetLang(cfg.Language)
}

// isSensitiveConfigKey 判断配置 key 是否为敏感字段。
func isSensitiveConfigKey(key string) bool {
	return strings.Contains(key, "token") || strings.Contains(key, "secret") ||
		strings.Contains(key, "key") || strings.Contains(key, "password")
}

// sendAgentMessage 在后台执行 Agent 循环（带工具调用）。
func (a *TUIApp) sendAgentMessage(text string) tea.Cmd {
	// 追加用户消息到历史
	a.chatHistory = append(a.chatHistory, memoryshot.ChatMessage{
		Role:    "user",
		Content: text,
	})
	a.syncChatHistoryToMemoryShot(true)

	// 保留最近 20 轮对话
	history := a.chatHistory
	if len(history) > 40 {
		history = history[len(history)-40:]
	}
	conversation := make([]map[string]string, 0, len(history))
	for _, msg := range history {
		conversation = append(conversation, map[string]string{
			"role":    msg.Role,
			"content": msg.Content,
		})
	}

	prog := a.program // capture for goroutine

	return func() tea.Msg {
		handler := NewTUIAgentHandler(a.sessionMgr,
			WithFirewall(a.firewall),
			WithDefGenerator(a.defGenerator),
			WithRouter(a.router),
			WithSelector(a.selector),
			WithConfigMgr(a.configMgr),
			WithMemoryStore(a.memoryStore),
			WithSchedulerMgr(a.schedulerMgr),
			WithAgentNetClient(a.AgentNetClient),
			WithAuditLog(a.auditLog),
			WithWorkflowEngine(a.workflowEngine),
			WithUsageTracker(a.usageTracker),
			WithSteeringStore(a.steeringStore),
		)

		// 设置流式回调：工具调用时推送中间状态到 UI
		if prog != nil {
			handler.SetStreamCallback(func(msgType, toolName, content string) {
				prog.Send(views.ChatStreamMsg{
					Type:    msgType,
					Tool:    toolName,
					Content: content,
				})
			})
		}

		resp := handler.RunAgentLoop(text, conversation)
		if resp.Error != "" {
			return views.ChatResponseMsg{Error: resp.Error}
		}
		// 追加助手回复到历史
		a.chatHistory = append(a.chatHistory, memoryshot.ChatMessage{
			Role:    "assistant",
			Content: resp.Text,
		})
		// 触发聊天八卦检测
		if a.gossipDetector != nil && resp.Text != "" {
			a.gossipDetector.OnChatCompleted(text, resp.Text)
		}
		a.syncChatHistoryToMemoryShot(true)
		return views.ChatResponseMsg{Text: resp.Text}
	}
}

// tuiRoleTitle returns "AI编程助手" for pro mode, "AI个人助手" otherwise.
func tuiRoleTitle() string {
	store := commands.NewFileConfigStore(commands.ResolveDataDir())
	if cfg, err := store.LoadConfig(); err == nil && cfg.UIMode == "pro" {
		return "AI编程助手"
	}
	return "AI个人助手"
}

// tuiSystemGreeting returns the TUI system prompt greeting based on ui_mode.
func tuiSystemGreeting(memoryStore *memory.Store) string {
	return buildTUIIdentityPrompt(memoryStore, tuiRoleTitle(), false)
}

// checkPythonEnvCmd 返回一个异步检测并安装 Python 环境的 tea.Cmd。
func (a *TUIApp) checkPythonEnvCmd() tea.Cmd {
	return func() tea.Msg {
		st := pyenv.EnsureEnvironment(func(stage string, pct int, msg string) {
			if a.logger != nil {
				a.logger.Info("[python-env] [%s] %d%% %s", stage, pct, msg)
			}
		})
		return pythonEnvMsg{status: st}
	}
}

// sendChatMessage 在后台调用 LLM 并返回响应。
func (a *TUIApp) sendChatMessage(text string) tea.Cmd {
	// 追加用户消息到历史
	a.chatHistory = append(a.chatHistory, memoryshot.ChatMessage{
		Role:    "user",
		Content: text,
	})
	a.syncChatHistoryToMemoryShot(true)

	// Build system greeting with memory-based identity override.
	greeting := tuiSystemGreeting(a.memoryStore)

	// 构建消息列表（含系统提示 + 历史）
	var msgs []interface{}
	msgs = append(msgs, map[string]string{
		"role":    "system",
		"content": greeting,
	})
	// 保留最近 20 轮对话
	history := a.chatHistory
	if len(history) > 40 {
		history = history[len(history)-40:]
	}
	for _, h := range history {
		msgs = append(msgs, map[string]string{
			"role":    h.Role,
			"content": h.Content,
		})
	}

	return func() tea.Msg {
		cfg, err := loadLLMConfig()
		if err != nil {
			return views.ChatResponseMsg{Error: err.Error()}
		}

		if a.llmClient == nil {
			a.llmClient = &http.Client{Timeout: 120 * time.Second}
		}

		resp, err := agent.DoSimpleLLMRequest(cfg, msgs, a.llmClient, 90*time.Second)
		if err != nil {
			return views.ChatResponseMsg{Error: err.Error()}
		}

		// 追加助手回复到历史
		a.chatHistory = append(a.chatHistory, memoryshot.ChatMessage{
			Role:    "assistant",
			Content: resp.Content,
		})
		a.syncChatHistoryToMemoryShot(true)

		return views.ChatResponseMsg{Text: resp.Content}
	}
}
