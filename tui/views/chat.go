// Package views 包含 TUI 的所有视图组件。
package views

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/i18n"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ChatClearMsg 清除聊天历史。
type ChatClearMsg struct{}

// ChatSendMsg 用户发送聊天消息。
type ChatSendMsg struct {
	Text      string
	AgentMode bool // true = 使用 Agent 循环（带工具调用）
}

// ChatResponseMsg LLM 返回的响应。
type ChatResponseMsg struct {
	Text  string
	Error string
}

// ChatMessage 聊天记录中的一条消息（导出用于 memoryshot 恢复）。
type ChatMessage struct {
	Role    string // "user" or "assistant" or "system"
	Content string
}

// ChatModel 是 AI 助手聊天视图。
type ChatModel struct {
	messages  []ChatMessage
	input     textinput.Model
	waiting   bool // 等待 LLM 响应
	agentMode bool // Agent 模式（带工具调用）
	scroll    int
	height    int
	width     int
	lang      string
}

// NewChatModel 创建聊天视图。
func NewChatModel(lang string) ChatModel {
	lang = i18n.NormalizeLang(lang)
	ti := textinput.New()
	ti.Placeholder = i18n.T(i18n.MsgTUIChatInputPlaceholder, lang)
	ti.CharLimit = 2000
	ti.Width = 60
	return ChatModel{
		input:     ti,
		agentMode: true,
		lang:      lang,
		messages: []ChatMessage{
			{Role: "system", Content: i18n.T(i18n.MsgTUIChatSystemReady, lang)},
		},
	}
}

func (m *ChatModel) SetLang(lang string) {
	m.lang = i18n.NormalizeLang(lang)
	m.input.Placeholder = i18n.T(i18n.MsgTUIChatInputPlaceholder, m.lang)
}

// SetMessages 设置聊天消息列表（用于从 memoryshot 恢复）。
func (m *ChatModel) SetMessages(msgs []ChatMessage) {
	if len(m.messages) > 0 && m.messages[0].Role == "system" {
		m.messages = append([]ChatMessage{m.messages[0]}, msgs...)
	} else {
		m.messages = msgs
	}
}

// GetMessages 返回当前聊天消息列表。
func (m ChatModel) GetMessages() []ChatMessage {
	return append([]ChatMessage(nil), m.messages...)
}

// Init 实现 tea.Model。
func (m ChatModel) Init() tea.Cmd { return nil }

// IsInputFocused 返回输入框是否聚焦（用于阻止 Tab 切换和 q 退出）。
func (m ChatModel) IsInputFocused() bool {
	return m.input.Focused()
}

// Update 处理消息。
func (m ChatModel) Update(msg tea.Msg) (ChatModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height - 6
		m.width = msg.Width
		if m.height < 1 {
			m.height = 1
		}
		m.input.Width = m.width - 6
		if m.input.Width < 20 {
			m.input.Width = 20
		}
	case ChatResponseMsg:
		m.waiting = false
		if msg.Error != "" {
			m.messages = append(m.messages, ChatMessage{Role: "system", Content: i18n.Tf(i18n.MsgTUIChatError, m.lang, msg.Error)})
		} else {
			m.messages = append(m.messages, ChatMessage{Role: "assistant", Content: msg.Text})
		}
		m.scrollToBottom()
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			if m.input.Focused() && !m.waiting {
				text := strings.TrimSpace(m.input.Value())
				if text != "" {
					m.messages = append(m.messages, ChatMessage{Role: "user", Content: text})
					m.input.SetValue("")
					m.waiting = true
					m.scrollToBottom()
					agentMode := m.agentMode
					return m, func() tea.Msg { return ChatSendMsg{Text: text, AgentMode: agentMode} }
				}
			}
			return m, nil
		case "i":
			if !m.input.Focused() {
				m.input.Focus()
				return m, nil
			}
		case "esc":
			if m.input.Focused() {
				m.input.Blur()
				return m, nil
			}
		case "up", "k":
			if !m.input.Focused() && m.scroll > 0 {
				m.scroll--
			}
		case "down", "j":
			if !m.input.Focused() {
				m.scrollDown()
			}
		case "G":
			if !m.input.Focused() {
				m.scrollToBottom()
			}
		case "g":
			if !m.input.Focused() {
				m.scroll = 0
			}
		case "c":
			if !m.input.Focused() && !m.waiting {
				m.messages = []ChatMessage{{Role: "system", Content: i18n.T(i18n.MsgTUIChatClearedMessage, m.lang)}}
				m.scroll = 0
				return m, func() tea.Msg { return ChatClearMsg{} }
			}
		case "a":
			if !m.input.Focused() && !m.waiting {
				m.agentMode = !m.agentMode
				mode := i18n.T(i18n.MsgTUIChatModeSimple, m.lang)
				if m.agentMode {
					mode = i18n.T(i18n.MsgTUIChatModeAgent, m.lang)
				}
				m.messages = append(m.messages, ChatMessage{Role: "system", Content: i18n.Tf(i18n.MsgTUIChatModeSwitched, m.lang, mode)})
				m.scrollToBottom()
			}
		}
	}

	if m.input.Focused() {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *ChatModel) scrollDown() {
	lines := m.renderLines()
	maxScroll := len(lines) - m.height
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.scroll < maxScroll {
		m.scroll++
	}
}

func (m *ChatModel) scrollToBottom() {
	lines := m.renderLines()
	maxScroll := len(lines) - m.height
	if maxScroll < 0 {
		maxScroll = 0
	}
	m.scroll = maxScroll
}

// renderLines 将所有消息渲染为行列表。
func (m ChatModel) renderLines() []string {
	userStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("117"))
	assistStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("156"))
	sysStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	maxWidth := m.width - 6
	if maxWidth < 20 {
		maxWidth = 60
	}

	var lines []string
	for _, msg := range m.messages {
		var prefix string
		var style lipgloss.Style
		switch msg.Role {
		case "user":
			prefix = i18n.T(i18n.MsgTUIChatUserPrefix, m.lang)
			style = userStyle
		case "assistant":
			prefix = i18n.T(i18n.MsgTUIChatAssistantPrefix, m.lang)
			style = assistStyle
		default:
			prefix = "  "
			style = sysStyle
		}
		contentLines := strings.Split(msg.Content, "\n")
		for i, cl := range contentLines {
			if i == 0 {
				lines = append(lines, style.Render(prefix+wrapLine(cl, maxWidth-len([]rune(prefix)))))
			} else {
				pad := strings.Repeat(" ", len([]rune(prefix)))
				lines = append(lines, style.Render(pad+wrapLine(cl, maxWidth-len([]rune(prefix)))))
			}
		}
	}
	if m.waiting {
		lines = append(lines, sysStyle.Render(i18n.T(i18n.MsgTUIChatWaiting, m.lang)))
	}
	return lines
}

// wrapLine 简单截断过长行。
func wrapLine(s string, maxW int) string {
	if maxW <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxW {
		return s
	}
	return string(runes[:maxW-1]) + "…"
}

// View 渲染聊天界面。
func (m ChatModel) View() string {
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	var b strings.Builder
	lines := m.renderLines()
	viewHeight := m.height - 3
	if viewHeight < 1 {
		viewHeight = 1
	}

	start := m.scroll
	end := start + viewHeight
	if end > len(lines) {
		end = len(lines)
	}
	if start > end {
		start = end
	}

	for _, line := range lines[start:end] {
		b.WriteString("  " + line + "\n")
	}
	for i := end - start; i < viewHeight; i++ {
		b.WriteString("\n")
	}

	w := m.width - 4
	if w < 20 {
		w = 60
	}
	b.WriteString("  " + strings.Repeat("─", w) + "\n")
	b.WriteString("  " + m.input.View() + "\n")

	hint := i18n.T(i18n.MsgTUIChatHint, m.lang)
	if m.waiting {
		hint = i18n.T(i18n.MsgTUIChatAwaitingResponse, m.lang)
	}
	modeLabel := i18n.T(i18n.MsgTUIChatModeLabelSimple, m.lang)
	if m.agentMode {
		modeLabel = i18n.T(i18n.MsgTUIChatModeLabelAgent, m.lang)
	}
	b.WriteString(dimStyle.Render(fmt.Sprintf("  %s  [%s]  %s", hint, modeLabel, i18n.Tf(i18n.MsgTUIChatMessageCount, m.lang, len(m.messages)-1))))

	return b.String()
}
