package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/tui/commands"
	"github.com/RapidAI/CodeClaw/tui/views"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type configUIModel struct {
	config views.ConfigModel
	cfg    corelib.AppConfig
	status string
	width  int
}

func newConfigUIModel(cfg corelib.AppConfig) configUIModel {
	lang := cfg.Language
	if lang == "" {
		lang = "zh"
	}
	cm := views.NewConfigModel(lang)
	cm.FocusLLMConfig()
	cm.LoadFromAppConfig(cfg)
	return configUIModel{
		config: cm,
		cfg:    cfg,
		status: configUIStatusReady(lang),
		width:  80,
	}
}

func (m configUIModel) Init() tea.Cmd { return nil }

func (m configUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.KeyMsg:
		if !m.config.IsEditing() {
			switch msg.String() {
			case "ctrl+c", "q", "esc":
				return m, tea.Quit
			case "ctrl+s":
				return m, tea.Sequence(m.saveCmd(), tea.Quit)
			}
		}
	case views.ConfigOpenSetupMsg:
		m.status = configUIStatusOpenFullTUI(m.cfg.Language, "setup")
		return m, nil
	case views.ConfigOpenServiceRedeemMsg:
		m.status = configUIStatusOpenFullTUI(m.cfg.Language, "redeem")
		return m, nil
	case views.ConfigOpenToolsMsg:
		m.status = configUIStatusOpenFullTUI(m.cfg.Language, "tools")
		return m, nil
	case views.ConfigQQBotScanMsg:
		if !m.config.QQBotQRScanRequested() {
			return m, nil
		}
		return m, startQQBotQRLoginCmd(m.cfg.Language)
	case views.ConfigQQBotPollMsg:
		if !m.config.QQBotQRPollTokenMatches(msg.Token) {
			return m, nil
		}
		return m, pollQQBotQRLoginCmd(m.cfg.Language, msg.Token)
	case views.ConfigQQBotCancelMsg:
		tuiQQBotQRClient().CancelBindTask(msg.Token)
		return m, nil
	case views.ConfigQQBotQRMsg:
		var cmd tea.Cmd
		m.config, cmd = m.config.Update(msg)
		return m, cmd
	case views.ConfigQQBotPollResultMsg:
		var cmd tea.Cmd
		m.config, cmd = m.config.Update(msg)
		if msg.Success {
			store := commands.NewFileConfigStore(commands.ResolveDataDir())
			if cfg, err := store.LoadConfig(); err == nil {
				m.cfg = cfg
				m.config.LoadFromAppConfig(cfg)
			}
			m.status = tuiText(m.cfg.Language, "qqbotBound")
		} else if strings.TrimSpace(msg.Message) != "" {
			m.status = msg.Message
		}
		return m, cmd
	case views.ConfigSaveMsg:
		if blocked, reason := rejectHubManagedSecurityConfigChange(m.cfg, msg.Key); blocked {
			m.status = configUIStatusSaveFailed(m.cfg.Language, views.ConfigDisplayNameForLang(msg.Key, m.cfg.Language), reason)
			return m, nil
		}
		current := m.cfg
		if msg.HasConfig {
			m.cfg = msg.Config
		} else {
			views.ApplyConfigValue(&m.cfg, msg.Key, msg.Value)
		}
		preserveHubManagedSecurityConfig(current, &m.cfg)
		if msg.Key == "language" {
			m.config.SetLang(m.cfg.Language)
		}
		m.config.LoadFromAppConfig(m.cfg)
		m.status = configUIStatusChanged(m.cfg.Language, views.ConfigDisplayNameForLang(msg.Key, m.cfg.Language))
		return m, nil
	case views.ConfigSaveFailedMsg:
		m.status = configUIStatusSaveFailed(m.cfg.Language, views.ConfigDisplayNameForLang(msg.Key, m.cfg.Language), msg.Error)
		return m, nil
	case views.ConfigSavedMsg:
		m.status = configUIStatusSaved(m.cfg.Language, views.ConfigDisplayNameForLang(msg.Key, m.cfg.Language))
		return m, nil
	}

	var cmd tea.Cmd
	m.config, cmd = m.config.Update(msg)
	return m, cmd
}

func (m configUIModel) View() string {
	footerText := "  " + m.status
	if m.width > 0 {
		footerText = configUIFitDisplay(footerText, m.width)
	}
	footer := lipgloss.NewStyle().
		Foreground(lipgloss.Color("245")).
		Width(m.width).
		Render(footerText)
	return m.config.View() + "\n" + footer
}

func configUIFitDisplay(s string, width int) string {
	if width <= 0 || lipgloss.Width(s) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if used+rw > width-1 {
			break
		}
		b.WriteRune(r)
		used += rw
	}
	b.WriteString("…")
	return b.String()
}

func configUIStatusReady(lang string) string {
	if lang == "en" {
		return "Enter opens choices/actions, Space cycles options, Ctrl+S saves and quits, q quits."
	}
	return "Enter 打开选择/执行，Space 切换/套用建议，Ctrl+S 保存并退出，q 退出。"
}

func configUIStatusOpenFullTUI(lang, target string) string {
	if lang == "en" {
		if target == "tools" {
			return "Tools/MCP templates are in the full TUI: run maclaw-tui mcp, press F3 in maclaw-tui, or type /mcp in chat."
		}
		if target == "redeem" {
			return "Service Redeem is in the full TUI: run maclaw-tui redeem, press F5 in maclaw-tui, or type /redeem in chat."
		}
		return "Setup is in the full TUI: run maclaw-tui setup, press F1 in maclaw-tui, or type /setup in chat."
	}
	if target == "tools" {
		return "工具/MCP 模板在完整 TUI 中：运行 maclaw-tui mcp，或在 maclaw-tui 中按 F3，也可在聊天中输入 /mcp。"
	}
	if target == "redeem" {
		return "服务兑换在完整 TUI 中：运行 maclaw-tui redeem，或在 maclaw-tui 中按 F5，也可在聊天中输入 /redeem。"
	}
	return "初始化在完整 TUI 中：运行 maclaw-tui setup，或在 maclaw-tui 中按 F1，也可在聊天中输入 /setup。"
}

func configUIStatusChanged(lang, key string) string {
	if lang == "en" {
		return fmt.Sprintf("Changed %s. Press Ctrl+S to save, or keep adjusting.", key)
	}
	return fmt.Sprintf("已修改 %s。按 Ctrl+S 保存，或继续调整。", key)
}

func configUIStatusSaveFailed(lang, key, errText string) string {
	if lang == "en" {
		return fmt.Sprintf("Save failed for %s: %s", key, errText)
	}
	return fmt.Sprintf("保存 %s 失败：%s", key, errText)
}

func configUIStatusSaved(lang, key string) string {
	if lang == "en" {
		return fmt.Sprintf("Saved %s", key)
	}
	return fmt.Sprintf("已保存 %s", key)
}

func (m configUIModel) saveCmd() tea.Cmd {
	cfg := m.cfg
	return func() tea.Msg {
		store := commands.NewFileConfigStore(commands.ResolveDataDir())
		if err := store.SaveConfig(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "save config failed: %v\n", err)
			return views.ConfigSaveFailedMsg{Key: "configuration", Error: err.Error()}
		}
		return views.ConfigSavedMsg{Key: "configuration", Value: "saved"}
	}
}
