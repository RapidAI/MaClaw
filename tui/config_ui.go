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
		status: "Enter edits a field, Space toggles booleans, Ctrl+S saves and quits, q quits.",
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
	case views.ConfigSaveMsg:
		views.ApplyConfigValue(&m.cfg, msg.Key, msg.Value)
		if msg.Key == "language" {
			m.config.SetLang(m.cfg.Language)
		}
		m.config.LoadFromAppConfig(m.cfg)
		m.status = fmt.Sprintf("Changed %s. Press Ctrl+S to save, or keep editing.", msg.Key)
		return m, nil
	case views.ConfigSavedMsg:
		m.status = fmt.Sprintf("Saved %s", msg.Key)
		return m, nil
	}

	var cmd tea.Cmd
	m.config, cmd = m.config.Update(msg)
	return m, cmd
}

func (m configUIModel) View() string {
	footer := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render("  " + m.status)
	return m.config.View() + "\n" + footer
}

func (m configUIModel) saveCmd() tea.Cmd {
	cfg := m.cfg
	return func() tea.Msg {
		store := commands.NewFileConfigStore(commands.ResolveDataDir())
		if err := store.SaveConfig(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "save config failed: %v\n", err)
			return nil
		}
		model := strings.TrimSpace(cfg.MaclawLLMModel)
		if model == "" {
			model = "configuration"
		}
		return views.ConfigSavedMsg{Key: model, Value: "saved"}
	}
}
