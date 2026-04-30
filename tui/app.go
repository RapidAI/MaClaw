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
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
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
	"github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/corelib/steering"
	"github.com/RapidAI/CodeClaw/corelib/task"
	"github.com/RapidAI/CodeClaw/corelib/tts"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
	"github.com/RapidAI/CodeClaw/tui/commands"
	"github.com/RapidAI/CodeClaw/tui/views"
	tea "github.com/charmbracelet/bubbletea"
)

// runTUI starts the Bubble Tea interactive mode.
func runTUI() {
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
	appCfg, _ := configStore.LoadConfig()

	// Build LLM config from the shared config file.
	llmCfg := buildLLMConfigFromAppConfig(appCfg)
	if llmCfg.URL == "" || llmCfg.Model == "" {
		fmt.Fprintln(os.Stderr, "LLM not configured.")
		fmt.Fprintln(os.Stderr, "Either configure via the maclaw GUI, or run: maclaw-tui llm setup")
		os.Exit(1)
	}

	// Hint: incomplete remote config (has email+hub but no credentials).
	if appCfg.RemoteEmail != "" && appCfg.RemoteHubURL != "" &&
		(appCfg.RemoteMachineID == "" || appCfg.RemoteMachineToken == "") {
		fmt.Fprintln(os.Stderr, "提示: 远程模式配置不完整（有邮箱和 Hub URL 但缺少凭据）。")
		fmt.Fprintf(os.Stderr, "运行 %s-tui remote activate 完成注册。\n", strings.ToLower(brand.Current().DisplayName))
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

	// Initialize workflow engine (19 templates, same as GUI).
	app.workflowEngine = app.initWorkflowEngine()

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
			"manage_skill": newManageSkillHandler(app),
			"tts":          newTTSHandler(app),
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

	root := views.NewRootModel(lang)
	root.Chat.FocusInput()
	// Show current LLM model in status bar.
	modelLabel := llmCfg.Model
	if llmCfg.ProviderName != "" {
		modelLabel = llmCfg.ProviderName + " / " + llmCfg.Model
	}
	root.StatusBar.SetModelInfo(modelLabel)

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
			root.Chat.AppendSystemMessage(fmt.Sprintf("📜 已恢复 %d 条历史消息（/new 清除）", len(restored)))
		}
	}

	tuiModel := &tuiModel{
		app:  app,
		root: root,
	}

	// Populate initial tool data from config.
	tuiModel.refreshToolData()

	// Populate config view from AppConfig.
	tuiModel.root.Config.LoadFromAppConfig(appCfg)

	// Populate memory view.
	tuiModel.refreshMemoryData()

	// Mark task sub-tabs and audit as loaded (no data source in standalone TUI yet).
	tuiModel.root.Tasks.SetTasks(nil)
	tuiModel.root.Tasks.SetRemoteTasks(nil)
	tuiModel.root.Tasks.SetBackgroundTasks(nil)
	tuiModel.root.Audit.SetEntries(nil)

	p := tea.NewProgram(tuiModel, tea.WithAltScreen())
	tuiModel.program = p

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}

	// Cleanup.
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
			llm.AgentType = p.AgentType
			llm.SupportsVision = p.SupportsVision
			llm.WireAPI = p.WireAPI
			break
		}
	}
	return llm
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
		roleDesc = "一个尽心尽责无所不能的软件开发管家"
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
	app      *TUIApp
	program  *tea.Program
	root     views.RootModel
	ready    bool
	activeCb cancellable // non-nil while agent loop is running
}

func (m *tuiModel) Init() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(100 * time.Millisecond)
		return tuiReadyMsg{}
	}
}

type tuiReadyMsg struct{}

func (m *tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tuiReadyMsg:
		m.ready = true
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		// Esc while waiting: cancel the running agent loop.
		if msg.String() == "esc" && m.activeCb != nil {
			m.activeCb.Cancel()
			m.activeCb = nil
			m.root.Chat.AppendSystemMessage("⏹ 已取消")
			return m, nil
		}

	case views.ChatSendMsg:
		// Handle slash commands locally.
		if strings.HasPrefix(strings.TrimSpace(msg.Text), "/") {
			// /btw requires an async agent loop — route through handleChatSend.
			trimmedCmd := strings.TrimSpace(msg.Text)
			if trimmedCmd == "/btw" || strings.HasPrefix(trimmedCmd, "/btw ") {
				return m, m.handleChatSend(msg.Text)
			}
			m.handleSlashCommand(msg.Text)
			return m, nil
		}
		// Start the agent loop directly. The user message was already added
		// to ChatModel.messages and rendered in the previous Update→View cycle
		// (ChatModel.Update handles the Enter key, adds the message, and
		// returns ChatSendMsg as a Cmd — Bubble Tea renders View() between
		// that Update and this one). No artificial delay needed.
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
			dataDir := commands.ResolveDataDir()
			store := commands.NewFileConfigStore(dataDir)
			if cfg, err := store.LoadConfig(); err == nil {
				m.app.appConfig = cfg
			}
			m.refreshToolData()
		}

	case views.ToolRefreshMsg:
		m.refreshToolData()
		m.refreshMemoryData()
		return m, nil

	case views.MemoryDeleteMsg:
		return m, m.deleteMemory(msg.ID)

	case views.ConfigSaveMsg:
		return m, m.saveConfig(msg.Section, msg.Key, msg.Value)

	case views.ConfigSavedMsg:
		// Reload config from disk (the goroutine already saved it).
		dataDir := commands.ResolveDataDir()
		store := commands.NewFileConfigStore(dataDir)
		if cfg, err := store.LoadConfig(); err == nil {
			m.app.appConfig = cfg
			if strings.HasPrefix(msg.Key, "maclaw_llm_") {
				m.app.llmConfig = buildLLMConfigFromAppConfig(cfg)
				label := m.app.llmConfig.Model
				if m.app.llmConfig.ProviderName != "" {
					label = m.app.llmConfig.ProviderName + " / " + m.app.llmConfig.Model
				}
				m.root.StatusBar.SetModelInfo(label)
			}
		}
		m.root.StatusBar.SetMessage(fmt.Sprintf("✅ 已保存: %s", msg.Key))
	}

	var cmd tea.Cmd
	m.root, cmd = m.root.Update(msg)
	return m, cmd
}

// handleSlashCommand processes /commands. Only called when text starts with "/".
func (m *tuiModel) handleSlashCommand(text string) {
	text = strings.TrimSpace(text)
	switch {
	case text == "/new" || text == "/clear":
		m.app.history.Clear("tui-user")
		// Cancel active workflow if any (even if workflow is disabled,
		// to clean up stale state from before the toggle was turned off).
		if m.app.workflowEngine != nil {
			_ = m.app.workflowEngine.CancelWorkflow("tui-user")
		}
		m.app.workflowMu.Lock()
		m.app.pendingPhasePrompt = ""
		m.app.workflowAgentLoop = false
		m.app.workflowMu.Unlock()
		m.root.Chat.ClearMessages("🗑 对话已清除")

	case text == "/model":
		cfg := m.app.llmConfig
		info := fmt.Sprintf("🧠 当前模型: %s\n   服务商: %s\n   协议: %s\n   上下文: %d tokens",
			cfg.Model, cfg.ProviderName, cfg.Protocol, cfg.ContextLength)
		if cfg.Protocol == "" {
			info = fmt.Sprintf("🧠 当前模型: %s\n   服务商: %s", cfg.Model, cfg.ProviderName)
		}
		m.root.Chat.AppendSystemMessage(info)

	case text == "/memory":
		if m.app.memoryStore == nil {
			m.root.Chat.AppendSystemMessage("记忆存储未初始化")
			return
		}
		entries := m.app.memoryStore.List("", "")
		if len(entries) == 0 {
			m.root.Chat.AppendSystemMessage("📭 记忆库为空")
		} else {
			var b strings.Builder
			fmt.Fprintf(&b, "📚 记忆库（共 %d 条）:\n", len(entries))
			shown := 0
			for _, e := range entries {
				if shown >= 10 {
					fmt.Fprintf(&b, "  ... 还有 %d 条\n", len(entries)-shown)
					break
				}
				content := e.Content
				if len([]rune(content)) > 60 {
					content = string([]rune(content)[:60]) + "…"
				}
				fmt.Fprintf(&b, "  [%s] %s\n", e.Category, content)
				shown++
			}
			m.root.Chat.AppendSystemMessage(b.String())
		}

	case text == "/help":
		help := `可用命令:

对话管理:
  /new /clear    清除对话历史，开始新对话
  /btw <查询>    侧查询（不打断当前任务上下文）

信息查看:
  /model         显示当前 LLM 模型信息
  /memory        查看记忆库摘要

  /help          显示此帮助

快捷键:
  Esc            取消正在执行的请求 / 退出输入框
  i              聚焦输入框
  c              清除对话（非输入状态）
  ↑↓/jk         滚动消息
  g/G            跳到顶部/底部`
		m.root.Chat.AppendSystemMessage(help)

	default:
		m.root.Chat.AppendSystemMessage(fmt.Sprintf("未知命令: %s（输入 /help 查看可用命令）", text))
	}
}

func (m *tuiModel) View() string {
	if !m.ready {
		return "Initializing..."
	}
	return m.root.View()
}

// handleChatSend runs the agent loop in a goroutine and streams results back.
func (m *tuiModel) handleChatSend(text string) tea.Cmd {
	prog := m.program
	app := m.app

	// --- /btw side query: independent agent loop ---
	trimmedText := strings.TrimSpace(text)
	if trimmedText == "/btw" || strings.HasPrefix(trimmedText, "/btw ") {
		btwQuery := ""
		if len(trimmedText) > 4 {
			btwQuery = strings.TrimSpace(trimmedText[4:])
		}
		if btwQuery == "" {
			return func() tea.Msg {
				return views.ChatResponseMsg{Text: "用法: /btw <查询内容>\n\n示例:\n  /btw 最新的 Go 1.23 有什么新特性\n  /btw React 19 的主要变化"}
			}
		}
		cb := newTuiBtwCallbacks(app, prog)
		m.activeCb = cb
		return func() tea.Msg {
			result := agent.RunLoop(cb, btwQuery, nil, nil)

			responseText := result.Text
			if responseText == "" && result.Error != "" {
				return views.ChatResponseMsg{Error: fmt.Sprintf("/btw 查询失败: %s", result.Error)}
			}
			if responseText == "" {
				responseText = "未找到相关信息。"
			}
			responseText = "🔍 **/btw 查询结果**\n\n" + responseText

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
			return views.ToolSkillSearchResultMsg{Error: "未找到匹配的 Skill"}
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
						Message: fmt.Sprintf("Skill '%s' 已安装", s.Name),
					}
				}
			}
			entry, err = client.DownloadClawHub(ctx, skillID)
		case "github":
			if installRef == "" {
				return views.ToolOperationResultMsg{
					Tab: views.ToolSubSkill, Success: false,
					Message: "GitHub Skill 缺少安装引用信息",
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
						Message: fmt.Sprintf("Skill '%s' 已安装", s.Name),
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
				Message: fmt.Sprintf("加载配置失败: %v", err),
			}
		}
		cfg.NLSkills = append(cfg.NLSkills, *entry)
		if err := store.SaveConfig(cfg); err != nil {
			return views.ToolOperationResultMsg{
				Tab: views.ToolSubSkill, Success: false,
				Message: fmt.Sprintf("保存失败: %v", err),
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
			Message: fmt.Sprintf("已安装: %s (来源: %s)", entry.Name, sourceLabel),
		}
	}
}

func (m *tuiModel) addLocalMCP(entry corelib.LocalMCPServerEntry) tea.Cmd {
	return func() tea.Msg {
		entry.ID = fmt.Sprintf("local_%s_%d", strings.ReplaceAll(entry.Name, " ", "_"), time.Now().Unix())
		entry.CreatedAt = time.Now().Format(time.RFC3339)

		dataDir := commands.ResolveDataDir()
		store := commands.NewFileConfigStore(dataDir)
		cfg, err := store.LoadConfig()
		if err != nil {
			return views.ToolOperationResultMsg{Tab: views.ToolSubMCP, Success: false, Message: "加载配置失败: " + err.Error()}
		}
		cfg.LocalMCPServers = append(cfg.LocalMCPServers, entry)
		if err := store.SaveConfig(cfg); err != nil {
			return views.ToolOperationResultMsg{Tab: views.ToolSubMCP, Success: false, Message: "保存配置失败: " + err.Error()}
		}
		// Return success — the main loop will refresh data via ToolRefreshMsg.
		return views.ToolOperationResultMsg{Tab: views.ToolSubMCP, Success: true, Message: "已添加: " + entry.Name}
	}
}

func (m *tuiModel) addRemoteMCP(entry corelib.MCPServerEntry) tea.Cmd {
	return func() tea.Msg {
		entry.ID = fmt.Sprintf("remote_%s_%d", strings.ReplaceAll(entry.Name, " ", "_"), time.Now().Unix())
		entry.CreatedAt = time.Now().Format(time.RFC3339)

		dataDir := commands.ResolveDataDir()
		store := commands.NewFileConfigStore(dataDir)
		cfg, err := store.LoadConfig()
		if err != nil {
			return views.ToolOperationResultMsg{Tab: views.ToolSubMCP, Success: false, Message: "加载配置失败: " + err.Error()}
		}
		cfg.MCPServers = append(cfg.MCPServers, entry)
		if err := store.SaveConfig(cfg); err != nil {
			return views.ToolOperationResultMsg{Tab: views.ToolSubMCP, Success: false, Message: "保存配置失败: " + err.Error()}
		}
		return views.ToolOperationResultMsg{Tab: views.ToolSubMCP, Success: true, Message: "已添加: " + entry.Name}
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

func (m *tuiModel) refreshMemoryData() {
	if m.app.memoryStore == nil {
		return
	}
	entries := m.app.memoryStore.List("", "")
	var items []views.MemoryItem
	for _, e := range entries {
		items = append(items, views.MemoryItem{
			ID:       e.ID,
			Category: string(e.Category),
			Content:  e.Content,
			Access:   e.AccessCount,
		})
	}
	m.root.Memory.SetEntries(items)
}

func (m *tuiModel) deleteMemory(id string) tea.Cmd {
	return func() tea.Msg {
		if m.app.memoryStore != nil {
			m.app.memoryStore.Delete(id)
		}
		return views.ToolRefreshMsg{} // reuse refresh to update memory view
	}
}

func (m *tuiModel) saveConfig(section, key, value string) tea.Cmd {
	return func() tea.Msg {
		dataDir := commands.ResolveDataDir()
		store := commands.NewFileConfigStore(dataDir)
		cfg, err := store.LoadConfig()
		if err != nil {
			return nil
		}
		applyConfigValue(&cfg, key, value)
		if err := store.SaveConfig(cfg); err != nil {
			return nil
		}
		return views.ConfigSavedMsg{Key: key, Value: value}
	}
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
		return fmt.Sprintf("参数解析失败: %v", err)
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
	roleName := cfg.MaclawRoleName
	if roleName == "" {
		roleName = brand.Current().DisplayName
	}
	roleDesc := cfg.MaclawRoleDescription
	if roleDesc == "" {
		roleDesc = "一个尽心尽责无所不能的软件开发管家"
	}

	var b strings.Builder

	// Identity.
	var selfIdentity string
	if app.memoryStore != nil {
		selfIdentity = app.memoryStore.SelfIdentitySummary(600)
	}
	if selfIdentity != "" {
		fmt.Fprintf(&b, "你的自我认知（来自记忆）：%s\n你的底层系统名为 %s。\n", selfIdentity, roleName)
	} else {
		fmt.Fprintf(&b, "你是 %s，%s。\n", roleName, roleDesc)
	}

	// /btw mode.
	b.WriteString(tuiBtwSuffix)

	// User fact summary.
	if app.memoryStore != nil {
		if summary := app.memoryStore.UserFactSummary(400); summary != "" {
			fmt.Fprintf(&b, "\n## 用户信息\n%s\n", summary)
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
			b.WriteString("\n## 相关记忆（自动召回）\n")
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
		return fmt.Sprintf("未知工具: %s（/btw 仅支持 web_search, web_fetch, read_file, memory, agent_status）", name)
	}

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf("参数解析失败: %v", err)
	}

	// Mechanism-level enforcement: memory tool is recall-only in /btw.
	if name == "memory" {
		action, _ := args["action"].(string)
		if action != "recall" {
			return "错误: /btw 侧查询中 memory 工具仅支持 action=\"recall\"（只读查询）"
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
				b.WriteString("🔗 **SSH 连接**\n")
				for _, s := range sessions {
					summary := s.GetSummary()
					fmt.Fprintf(&b, "- %s [%s] %s\n", s.ID, summary.Status, summary.HostLabel)
				}
				sections = append(sections, b.String())
			}
		}
	}

	if len(sections) == 0 {
		return "当前没有活跃的 SSH 连接或后台任务。"
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
