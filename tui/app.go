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
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agent/sshtool"
	"github.com/RapidAI/CodeClaw/corelib/brand"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/steering"
	"github.com/RapidAI/CodeClaw/corelib/task"
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
	}

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

	// Share config with root model for coding tool wizard.
	tuiModel.root.SetAppConfig(appCfg)

	// Populate config view from AppConfig.
	tuiModel.root.Config.LoadFromAppConfig(appCfg)

	// Populate memory view.
	tuiModel.refreshMemoryData()

	// Populate session view (SSH sessions).
	tuiModel.refreshSessionData()

	// Mark schedule/audit as loaded (no data source in standalone TUI yet).
	tuiModel.root.Schedule.SetTasks(nil)
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
}

// tuiModel is the Bubble Tea top-level model.
type tuiModel struct {
	app         *TUIApp
	program     *tea.Program
	root        views.RootModel
	ready       bool
	activeCb    *tuiCallbacks // non-nil while agent loop is running
	pendingText string        // text waiting for render before starting agent loop
}

func (m *tuiModel) Init() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(100 * time.Millisecond)
		return tuiReadyMsg{}
	}
}

type tuiReadyMsg struct{}
type tuiStartLoopMsg struct{}

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
			m.handleSlashCommand(msg.Text)
			return m, nil
		}
		// Use tea.Tick(0) to force a render of the user message before
		// starting the blocking agent loop. Without this, Bubble Tea may
		// batch the ChatSendMsg handling with the Enter key handling and
		// skip the intermediate View call, so the user message only appears
		// when the AI response arrives.
		m.pendingText = msg.Text
		return m, tea.Tick(0, func(time.Time) tea.Msg { return tuiStartLoopMsg{} })

	case tuiStartLoopMsg:
		if m.pendingText != "" {
			text := m.pendingText
			m.pendingText = ""
			return m, m.handleChatSend(text)
		}
		return m, nil

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
		return m, m.installSkill(msg.SkillID, msg.HubURL)

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
		m.refreshSessionData()
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
			m.root.SetAppConfig(cfg)
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

	case views.CodingConfigSaveMsg:
		return m, m.saveCodingToolConfig(msg)

	case views.CodingLaunchMsg:
		return m, m.launchCodingTool(msg.ToolName, msg.Provider, msg.ProjectPath)
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
  /new      清除对话历史，开始新对话
  /model    显示当前 LLM 模型信息
  /memory   查看记忆库摘要
  /help     显示此帮助

快捷键:
  Esc       取消正在执行的请求 / 退出输入框
  i         聚焦输入框
  c         清除对话（非输入状态）
  ↑↓/jk    滚动消息
  g/G       跳到顶部/底部`
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
	cb := newTuiCallbacks(app, prog)
	m.activeCb = cb

	return func() tea.Msg {
		history := app.history.Load("tui-user")
		result := agent.RunLoop(cb, text, history, nil)

		// Save conversation history (persisted to disk).
		history = append(history, agent.ConversationEntry{Role: "user", Content: text})
		if result.Text != "" {
			history = append(history, agent.ConversationEntry{Role: "assistant", Content: result.Text})
		}
		app.history.Save("tui-user", history)

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
		endpoint := fmt.Sprintf("%s/api/v1/skills/search?q=%s&page=1",
			hubURL, url.QueryEscape(query))

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return views.ToolSkillSearchResultMsg{Error: err.Error()}
		}
		req.Header.Set("User-Agent", "MaClaw-TUI/1.0")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return views.ToolSkillSearchResultMsg{Error: fmt.Sprintf("搜索失败: %v", err)}
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return views.ToolSkillSearchResultMsg{Error: fmt.Sprintf("HTTP %d", resp.StatusCode)}
		}

		var raw struct {
			Skills []struct {
				ID         string  `json:"id"`
				Name       string  `json:"name"`
				Version    string  `json:"version"`
				AvgRating  float64 `json:"avg_rating"`
				Downloads  int     `json:"downloads"`
				TrustLevel string  `json:"trust_level"`
			} `json:"skills"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
			return views.ToolSkillSearchResultMsg{Error: fmt.Sprintf("解析失败: %v", err)}
		}

		var results []views.SkillSearchResult
		for _, s := range raw.Skills {
			results = append(results, views.SkillSearchResult{
				ID:        s.ID,
				Name:      s.Name,
				Version:   s.Version,
				Rating:    s.AvgRating,
				Downloads: s.Downloads,
				Trust:     s.TrustLevel,
			})
		}
		return views.ToolSkillSearchResultMsg{Results: results}
	}
}

func (m *tuiModel) installSkill(skillID, hubURL string) tea.Cmd {
	return func() tea.Msg {
		// TUI cannot directly install skills (requires skill scanner + file system ops).
		// Return guidance to use the chat assistant which has the manage_skill tool.
		return views.ToolOperationResultMsg{
			Tab:     views.ToolSubSkill,
			Success: false,
			Message: "请在助手聊天中输入「安装 " + skillID + "」来安装此 Skill",
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

func (m *tuiModel) saveCodingToolConfig(msg views.CodingConfigSaveMsg) tea.Cmd {
	return func() tea.Msg {
		dataDir := commands.ResolveDataDir()
		store := commands.NewFileConfigStore(dataDir)
		cfg, err := store.LoadConfig()
		if err != nil {
			return nil
		}
		tc := agent.GetToolConfig(cfg, msg.ToolName)
		if len(tc.Models) == 0 {
			tc.Models = agent.DefaultProvidersForTool(msg.ToolName)
		}
		for i := range tc.Models {
			if tc.Models[i].ModelName == msg.Provider {
				if msg.ApiKey != "" {
					tc.Models[i].ApiKey = msg.ApiKey
				}
				break
			}
		}
		tc.CurrentModel = msg.Provider
		agent.SetToolConfig(&cfg, msg.ToolName, tc)
		store.SaveConfig(cfg)

		// Update in-memory config so launch reads the latest.
		m.app.appConfig = cfg

		if msg.Launch {
			return views.CodingLaunchMsg{
				ToolName:    msg.ToolName,
				Provider:    msg.Provider,
				ProjectPath: msg.ProjectPath,
			}
		}
		return views.ConfigSavedMsg{Key: msg.ToolName + "." + msg.Provider}
	}
}

func (m *tuiModel) launchCodingTool(toolName, provider, projectPath string) tea.Cmd {
	return func() tea.Msg {
		// Find the tool info.
		var toolInfo *agent.CodingToolInfo
		for _, t := range agent.SupportedCodingTools() {
			if t.Name == toolName {
				ti := t
				toolInfo = &ti
				break
			}
		}
		if toolInfo == nil {
			return views.ChatResponseMsg{Error: "未知工具: " + toolName}
		}

		// Get the provider config.
		tc := agent.GetToolConfig(m.app.appConfig, toolName)
		var model *corelib.ModelConfig
		for i := range tc.Models {
			if tc.Models[i].ModelName == provider {
				model = &tc.Models[i]
				break
			}
		}
		if model == nil {
			return views.ChatResponseMsg{Error: "未找到服务商: " + provider}
		}
		if model.ApiKey == "" && !model.IsBuiltin {
			return views.ChatResponseMsg{Error: "服务商 " + provider + " 未配置 API Key"}
		}

		// Build environment variables.
		env := make(map[string]string)
		if model.ApiKey != "" {
			env[toolInfo.EnvKey] = model.ApiKey
		}
		if model.ModelUrl != "" {
			env[toolInfo.EnvBaseURL] = model.ModelUrl
		}
		if model.ModelId != "" {
			// Claude uses ANTHROPIC_MODEL, Codex uses OPENAI_MODEL, etc.
			switch toolName {
			case "claude":
				env["ANTHROPIC_MODEL"] = model.ModelId
			case "codex":
				env["OPENAI_MODEL"] = model.ModelId
			}
		}

		// Resolve project path.
		if projectPath == "" {
			if wd, err := os.Getwd(); err == nil {
				projectPath = wd
			}
		}

		// Launch the binary.
		binary := toolInfo.Binary
		args := []string{}
		if projectPath != "" {
			args = append(args, "--project", projectPath)
		}

		// Use exec to replace the TUI process with the coding tool.
		// This gives the coding tool full terminal control.
		execPath, err := exec.LookPath(binary)
		if err != nil {
			return views.ChatResponseMsg{Error: fmt.Sprintf("未找到 %s 命令。请先安装: npm install -g @anthropic-ai/claude-code", binary)}
		}

		// Build full environment.
		fullEnv := os.Environ()
		for k, v := range env {
			fullEnv = append(fullEnv, k+"="+v)
		}

		// We can't exec.Replace in TUI mode (would kill Bubble Tea).
		// Instead, run the tool in a subprocess and wait.
		cmd := exec.Command(execPath, args...)
		cmd.Dir = projectPath
		cmd.Env = fullEnv
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		// Temporarily exit alt-screen so the coding tool gets the real terminal.
		if m.program != nil {
			m.program.ReleaseTerminal()
		}
		err = cmd.Run()
		if m.program != nil {
			m.program.RestoreTerminal()
		}

		if err != nil {
			return views.ChatResponseMsg{Text: fmt.Sprintf("🔧 %s 已退出: %v", binary, err)}
		}
		return views.ChatResponseMsg{Text: fmt.Sprintf("🔧 %s 已退出", binary)}
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

func (m *tuiModel) refreshSessionData() {
	if m.app.sshMgr == nil {
		m.root.Sessions.SetSessions(nil)
		return
	}
	sshSessions := m.app.sshMgr.List()
	var items []views.SessionItem
	for _, s := range sshSessions {
		summary := s.GetSummary()
		items = append(items, views.SessionItem{
			ID:     summary.SessionID,
			Tool:   "ssh",
			Title:  summary.HostID,
			Status: summary.Status,
		})
	}
	m.root.Sessions.SetSessions(items)
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
func applyConfigValue(cfg *corelib.AppConfig, key, value string) {
	switch key {
	case "hub_url":
		cfg.RemoteHubURL = value
	case "token":
		cfg.RemoteMachineToken = value
	case "max_iterations":
		fmt.Sscanf(value, "%d", &cfg.MaclawAgentMaxIterations)
	case "agentnet_enabled":
		cfg.AgentNetEnabled = value == "true"
	case "maclaw_llm_url":
		cfg.MaclawLLMUrl = value
	case "maclaw_llm_key":
		cfg.MaclawLLMKey = value
	case "maclaw_llm_model":
		cfg.MaclawLLMModel = value
	case "maclaw_llm_protocol":
		cfg.MaclawLLMProtocol = value
	case "maclaw_llm_context_length":
		fmt.Sscanf(value, "%d", &cfg.MaclawLLMContextLength)
	case "qqbot_enabled":
		cfg.QQBotEnabled = value == "true"
	case "qqbot_app_id":
		cfg.QQBotAppID = value
	case "qqbot_app_secret":
		cfg.QQBotAppSecret = value
	case "telegram_bot_enabled":
		cfg.TelegramBotEnabled = value == "true"
	case "telegram_bot_token":
		cfg.TelegramBotToken = value
	}
}

// tuiCallbacks implements agent.LoopCallbacks for the TUI.
type tuiCallbacks struct {
	app      *TUIApp
	program  *tea.Program
	stopped  bool
	cancelCh chan struct{} // closed by Esc key to cancel the running loop
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
	if n := c.app.appConfig.MaclawAgentMaxIterations; n > 0 {
		return n
	}
	return 30
}

func (c *tuiCallbacks) BuildSystemPrompt(userText string, isFirstTurn bool) string {
	cfg := c.app.appConfig
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
			RoleName:        roleName,
			RoleDescription: roleDesc,
			IsProMode:       true,
			Nickname:        cfg.RemoteNickname,
		},
		MemoryStore: c.app.memoryStore,
	}

	// SSH hosts from config.
	if len(cfg.SSHHosts) > 0 {
		deps.SSHHostLister = func() []corelib.SSHHostEntry {
			return cfg.SSHHosts
		}
	}

	// Steering resolver.
	if c.app.steeringStore != nil {
		deps.SteeringResolver = func(userMessage string, contextTokens int) []steering.File {
			ctx := steering.ResolveContext{
				UserMessage:            userMessage,
				EffectiveContextTokens: contextTokens,
			}
			return c.app.steeringStore.Resolve(ctx)
		}
	}

	return agent.BuildSystemPrompt(deps, userText, isFirstTurn)
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
