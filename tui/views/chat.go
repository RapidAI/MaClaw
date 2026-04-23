// Package views 包含 TUI 的所有视图组件。
package views

import (
	"fmt"
	"strings"
	"time"

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

// ChatStreamMsg 中间流式更新（工具调用、状态等）。
type ChatStreamMsg struct {
	Type    string // "tool_call", "tool_result", "thinking", "text_delta"
	Tool    string // 工具名称（tool_call/tool_result 时使用）
	Content string // 内容
}

// ChatMessage 聊天记录中的一条消息（导出用于 memoryshot 恢复）。
type ChatMessage struct {
	Role    string // "user" or "assistant" or "system"
	Content string
}

// ChatModel 是 AI 助手聊天视图。
type ChatModel struct {
	messages    []ChatMessage
	input       textinput.Model
	waiting     bool // 等待 LLM 响应
	agentMode   bool // Agent 模式（带工具调用）
	scroll      int
	height      int
	width       int
	lang        string
	spinnerTick int // animation frame counter for waiting indicator
}

// spinnerFrames defines the animated spinner shown while waiting for AI response.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// chatTickMsg drives the waiting spinner animation.
type chatTickMsg struct{}

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

// FocusInput 聚焦输入框。
func (m *ChatModel) FocusInput() {
	m.input.Focus()
}

// AppendSystemMessage 追加一条系统消息到聊天记录。
func (m *ChatModel) AppendSystemMessage(text string) {
	m.messages = append(m.messages, ChatMessage{Role: "system", Content: text})
	m.waiting = false
	m.scrollToBottom()
}

// ClearMessages resets the chat to a single system message.
func (m *ChatModel) ClearMessages(systemMsg string) {
	m.messages = []ChatMessage{{Role: "system", Content: systemMsg}}
	m.scroll = 0
	m.waiting = false
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
		} else if msg.Text != "" {
			m.cleanupToolMessages()
			// Check if streaming already created an assistant message for
			// this response (text_delta path). If so, replace its content
			// with the final post-processed text. Otherwise append new.
			if idx := m.lastAssistantAfterUser(); idx >= 0 {
				m.messages[idx].Content = msg.Text
			} else {
				m.messages = append(m.messages, ChatMessage{Role: "assistant", Content: msg.Text})
			}
		}
		m.scrollToBottom()
		return m, nil
	case chatTickMsg:
		if m.waiting {
			m.spinnerTick++
			return m, tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return chatTickMsg{} })
		}
		return m, nil
	case ChatStreamMsg:
		switch msg.Type {
		case "tool_call":
			m.messages = append(m.messages, ChatMessage{Role: "system", Content: "🔧 " + msg.Tool + "..."})
		case "tool_result":
			// Update the last tool message with result status and optional elapsed time.
			for i := len(m.messages) - 1; i >= 0; i-- {
				if m.messages[i].Role == "system" && strings.HasPrefix(m.messages[i].Content, "🔧 "+msg.Tool) {
					suffix := " ✓"
					if msg.Content != "" {
						suffix = " ✓ " + msg.Content
					}
					m.messages[i].Content = "🔧 " + msg.Tool + suffix
					break
				}
			}
		case "thinking":
			// 更新思考状态
		case "text_delta":
			// 追加到最后一条 assistant 消息，或创建新的
			if len(m.messages) > 0 && m.messages[len(m.messages)-1].Role == "assistant" {
				m.messages[len(m.messages)-1].Content += msg.Content
			} else {
				m.messages = append(m.messages, ChatMessage{Role: "assistant", Content: msg.Content})
			}
		}
		m.scrollToBottom()
		return m, nil
	case tea.KeyMsg:
		key := msg.String()

		// Category 1: Navigation keys — always work regardless of input focus.
		switch key {
		case "up":
			if m.scroll > 0 {
				m.scroll--
			}
			return m, nil
		case "down":
			m.scrollDown()
			return m, nil
		case "pgup":
			for i := 0; i < 10 && m.scroll > 0; i++ {
				m.scroll--
			}
			return m, nil
		case "pgdown":
			for i := 0; i < 10; i++ {
				m.scrollDown()
			}
			return m, nil
		}

		// Category 2: Input submission — only when input is focused.
		if m.input.Focused() {
			switch key {
			case "enter":
				if !m.waiting {
					text := strings.TrimSpace(m.input.Value())
					if text != "" {
						m.messages = append(m.messages, ChatMessage{Role: "user", Content: text})
						m.input.SetValue("")
						m.waiting = true
						m.spinnerTick = 0
						m.scrollToBottom()
						agentMode := m.agentMode
						return m, tea.Batch(
							func() tea.Msg { return ChatSendMsg{Text: text, AgentMode: agentMode} },
							tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return chatTickMsg{} }),
						)
					}
				}
				return m, nil
			case "esc":
				m.input.Blur()
				return m, nil
			}
			// All other keys go to textinput when focused.
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}

		// Category 3: Command keys — only when input is NOT focused.
		switch key {
		case "i", "enter":
			m.input.Focus()
			return m, nil
		case "k":
			if m.scroll > 0 {
				m.scroll--
			}
		case "j":
			m.scrollDown()
		case "G":
			m.scrollToBottom()
		case "g":
			m.scroll = 0
		case "c":
			if !m.waiting {
				m.messages = []ChatMessage{{Role: "system", Content: i18n.T(i18n.MsgTUIChatClearedMessage, m.lang)}}
				m.scroll = 0
				return m, func() tea.Msg { return ChatClearMsg{} }
			}
		case "a":
			if !m.waiting {
				m.agentMode = !m.agentMode
				mode := i18n.T(i18n.MsgTUIChatModeSimple, m.lang)
				if m.agentMode {
					mode = i18n.T(i18n.MsgTUIChatModeAgent, m.lang)
				}
				m.messages = append(m.messages, ChatMessage{Role: "system", Content: i18n.Tf(i18n.MsgTUIChatModeSwitched, m.lang, mode)})
				m.scrollToBottom()
			}
		}
		return m, nil
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
	toolStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // orange for tool calls

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
			// 工具调用消息用特殊颜色
			if strings.HasPrefix(msg.Content, "🔧") {
				style = toolStyle
			}
		}

		// Assistant messages: render Markdown with syntax highlighting.
		if msg.Role == "assistant" {
			lines = append(lines, assistStyle.Render(prefix))
			mdLines := RenderMarkdown(msg.Content, maxWidth-2)
			for _, ml := range mdLines {
				lines = append(lines, "  "+ml)
			}
			continue
		}

		// User / system messages: plain text with prefix.
		prefixWidth := lipgloss.Width(prefix)
		contentWidth := maxWidth - prefixWidth
		if contentWidth < 10 {
			contentWidth = 10
		}
		pad := strings.Repeat(" ", prefixWidth)

		contentLines := strings.Split(msg.Content, "\n")
		firstLine := true
		for _, cl := range contentLines {
			wrapped := wrapLine(cl, contentWidth)
			for _, wl := range strings.Split(wrapped, "\n") {
				if firstLine {
					lines = append(lines, style.Render(prefix+wl))
					firstLine = false
				} else {
					lines = append(lines, style.Render(pad+wl))
				}
			}
		}
	}
	if m.waiting {
		frame := spinnerFrames[m.spinnerTick%len(spinnerFrames)]
		lines = append(lines, sysStyle.Render("  "+frame+" "+i18n.T(i18n.MsgTUIChatSpinnerLabel, m.lang)))
	}
	return lines
}

// wrapLine 自动换行：将超长行按 maxW 宽度折行，返回多行。
func wrapLine(s string, maxW int) string {
	if maxW <= 0 || len([]rune(s)) <= maxW {
		return s
	}
	runes := []rune(s)
	var lines []string
	for len(runes) > maxW {
		lines = append(lines, string(runes[:maxW]))
		runes = runes[maxW:]
	}
	if len(runes) > 0 {
		lines = append(lines, string(runes))
	}
	return strings.Join(lines, "\n")
}

// hasUserMessages returns true if there are any user or assistant messages.
func (m ChatModel) hasUserMessages() bool {
	for _, msg := range m.messages {
		if msg.Role == "user" || msg.Role == "assistant" {
			return true
		}
	}
	return false
}

// cleanupToolMessages 移除末尾连续的工具调用中间消息（🔧 开头的 system 消息）。
// 在最终 assistant 回复到达时调用，避免工具调用噪音残留。
func (m *ChatModel) cleanupToolMessages() {
	// 从末尾向前扫描，移除连续的工具消息
	for len(m.messages) > 0 {
		last := m.messages[len(m.messages)-1]
		if last.Role == "system" && strings.HasPrefix(last.Content, "🔧") {
			m.messages = m.messages[:len(m.messages)-1]
		} else {
			break
		}
	}
}

// lastAssistantAfterUser returns the index of the last assistant message
// that appears after the last user message, or -1 if none exists.
// Used to detect whether streaming already created an assistant message
// for the current response.
func (m *ChatModel) lastAssistantAfterUser() int {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].Role == "assistant" {
			return i
		}
		if m.messages[i].Role == "user" {
			return -1
		}
	}
	return -1
}

// View 渲染聊天界面。
func (m ChatModel) View() string {
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	var b strings.Builder
	viewHeight := m.height - 3
	if viewHeight < 1 {
		viewHeight = 1
	}

	if !m.hasUserMessages() {
		// 无用户消息时显示 MaClaw logo
		m.renderWelcomeView(&b, viewHeight)
	} else {
		// 正常聊天消息渲染
		lines := m.renderLines()
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

// renderWelcomeView renders the MaClaw logo centered in the chat area.
func (m ChatModel) renderWelcomeView(b *strings.Builder, viewHeight int) {
	logoStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("39")).
		Bold(true)
	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("245"))

	logoLines := []string{
		"  ███╗   ███╗ █████╗  ██████╗██╗      █████╗ ██╗    ██╗",
		"  ████╗ ████║██╔══██╗██╔════╝██║     ██╔══██╗██║    ██║",
		"  ██╔████╔██║███████║██║     ██║     ███████║██║ █╗ ██║",
		"  ██║╚██╔╝██║██╔══██║██║     ██║     ██╔══██║██║███╗██║",
		"  ██║ ╚═╝ ██║██║  ██║╚██████╗███████╗██║  ██║╚███╔███╔╝",
		"  ╚═╝     ╚═╝╚═╝  ╚═╝ ╚═════╝╚══════╝╚═╝  ╚═╝ ╚══╝╚══╝",
	}

	// logo (6 lines) + blank + hint (2 lines) = 9 lines
	contentLines := len(logoLines) + 3
	topPad := (viewHeight - contentLines) / 2
	if topPad < 0 {
		topPad = 0
	}

	for i := 0; i < topPad; i++ {
		b.WriteString("\n")
	}
	for _, line := range logoLines {
		b.WriteString(logoStyle.Render(line) + "\n")
	}
	b.WriteString("\n")
	b.WriteString(hintStyle.Render("  " + i18n.T(i18n.MsgTUIChatSystemReady, m.lang)) + "\n")
	b.WriteString(hintStyle.Render("  输入消息开始对话，或输入 /help 查看命令") + "\n")

	// 填充剩余空间
	used := topPad + contentLines
	for i := used; i < viewHeight; i++ {
		b.WriteString("\n")
	}
}
