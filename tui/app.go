package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/tui/commands"
	"github.com/RapidAI/CodeClaw/tui/views"
	tea "github.com/charmbracelet/bubbletea"
)

// TUIApp 是 Bubble Tea 的顶层 Model，持有 Kernel 和 UI 状态。
type TUIApp struct {
	kernel     *corelib.Kernel
	bridge     *BubbleTeaEventBridge
	logger     *TUILogger
	qqBotMgr   *tuiQQBotManager
	telegramMgr *tuiTelegramManager
	loopMgr    *agent.BackgroundLoopManager
	configWatcher *ConfigWatcher
	sessionMgr *TUISessionManager

	// AI 助手聊天
	chatHistory []map[string]string // 对话历史 (role/content)
	llmClient   *http.Client

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

// sessionUpdateMsg 会话状态变更消息。
type sessionUpdateMsg struct {
	sessionID string
}

// NewTUIApp 创建 TUI 应用实例。
func NewTUIApp() *TUIApp {
	return &TUIApp{
		root: views.NewRootModel(),
	}
}

// Init 实现 tea.Model 接口。
func (a *TUIApp) Init() tea.Cmd {
	return a.initKernel
}

// initKernel 在后台初始化内核。
func (a *TUIApp) initKernel() tea.Msg {
	logger := NewTUILogger()
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
			if a.qqBotMgr != nil {
				a.qqBotMgr.Stop()
			}
			if a.telegramMgr != nil {
				a.telegramMgr.Stop()
			}
			if a.configWatcher != nil {
				a.configWatcher.Stop()
			}
			if a.kernel != nil {
				ctx := context.Background()
				_ = a.kernel.Shutdown(ctx)
			}
			return a, tea.Quit
		case "q":
			if !configEditing && !auditFiltering && !chatFocused {
				if a.qqBotMgr != nil {
					a.qqBotMgr.Stop()
				}
				if a.telegramMgr != nil {
					a.telegramMgr.Stop()
				}
				if a.configWatcher != nil {
					a.configWatcher.Stop()
				}
				if a.kernel != nil {
					ctx := context.Background()
					_ = a.kernel.Shutdown(ctx)
				}
				return a, tea.Quit
			}
		}

	case tea.WindowSizeMsg:
		a.ready = true

	case kernelStartedMsg:
		if msg.err != nil {
			a.err = msg.err
			a.root.StatusBar.SetMessage(fmt.Sprintf("内核初始化失败: %v", msg.err))
		} else {
			a.root.StatusBar.SetMessage("就绪")
			a.root.StatusBar.SetHubStatus("disconnected")
			a.root.Sessions.SetSessions(nil) // 清除 loading 状态
			a.root.Audit.SetEntries(nil)
			a.root.ClawNet.SetPeers(nil)

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
		}

	case kernelEventMsg:
		a.root.StatusBar.SetMessage(fmt.Sprintf("[%s] %v", msg.eventType, msg.data))

	case views.ConfigSaveMsg:
		// TODO: 接入 config.Manager 持久化
		a.root.StatusBar.SetMessage(fmt.Sprintf("已保存: %s = %s", msg.Key, msg.Value))
		// Re-sync QQ Bot gateway on config change
		if a.qqBotMgr != nil && msg.Section == "qqbot" {
			go a.qqBotMgr.SyncFromConfig()
		}
		// Re-sync Telegram gateway on config change
		if a.telegramMgr != nil && msg.Section == "telegram" {
			go a.telegramMgr.SyncFromConfig()
		}

	case views.MemoryCompressMsg:
		a.root.StatusBar.SetMessage("记忆压缩中... 请使用 CLI: maclaw-tui memory compress")

	case views.MemoryBackupListMsg:
		a.root.StatusBar.SetMessage("备份列表请使用 CLI: maclaw-tui memory backup list")

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
		a.root.StatusBar.SetMessage("工具状态已刷新")

	case views.ChatSendMsg:
		a.root.StatusBar.SetMessage("AI 思考中...")
		return a, a.sendChatMessage(msg.Text)

	case views.ChatClearMsg:
		a.chatHistory = nil
		a.root.StatusBar.SetMessage("聊天历史已清除")

	case configChangedMsg:
		a.root.StatusBar.SetMessage("配置文件已变更，正在重新加载...")
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
			a.root.StatusBar.SetMessage(fmt.Sprintf("工具 %s 退出: %v", msg.name, msg.err))
		} else {
			a.root.StatusBar.SetMessage(fmt.Sprintf("工具 %s 已退出", msg.name))
		}

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
		return "正在初始化 MaClaw TUI...\n"
	}
	if a.err != nil {
		return fmt.Sprintf("错误: %v\n\n按 q 退出\n", a.err)
	}
	return a.root.View()
}

// runTUI 启动 TUI 交互模式。
func runTUI() {
	app := NewTUIApp()
	p := tea.NewProgram(app, tea.WithAltScreen())

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
		return llm, fmt.Errorf("LLM 未配置，请在配置 Tab 或 GUI 中设置 maclaw_llm_url 和 maclaw_llm_model")
	}
	return llm, nil
}

// sendChatMessage 在后台调用 LLM 并返回响应。
func (a *TUIApp) sendChatMessage(text string) tea.Cmd {
	// 追加用户消息到历史
	a.chatHistory = append(a.chatHistory, map[string]string{
		"role": "user", "content": text,
	})

	// 构建消息列表（含系统提示 + 历史）
	var msgs []interface{}
	msgs = append(msgs, map[string]string{
		"role":    "system",
		"content": "你是 MaClaw AI 助手，运行在 TUI 终端中。请用简洁的中文回答用户问题。",
	})
	// 保留最近 20 轮对话
	history := a.chatHistory
	if len(history) > 40 {
		history = history[len(history)-40:]
	}
	for _, h := range history {
		msgs = append(msgs, h)
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
		a.chatHistory = append(a.chatHistory, map[string]string{
			"role": "assistant", "content": resp.Content,
		})

		return views.ChatResponseMsg{Text: resp.Content}
	}
}
