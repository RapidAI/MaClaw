// Package views 包含 TUI 的所有视图组件。
package views

import (
	"fmt"
	"strings"

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

// chatMessage 聊天记录中的一条消息。
type chatMessage struct {
	Role    string // "user" or "assistant" or "system"
	Content string
}

// ChatModel 是 AI 助手聊天视图。
type ChatModel struct {
	messages  []chatMessage
	input     textinput.Model
	waiting   bool // 等待 LLM 响应
	agentMode bool // Agent 模式（带工具调用）
	scroll    int
	height    int
	width     int
}

// NewChatModel 创建聊天视图。
func NewChatModel() ChatModel {
	ti := textinput.New()
	ti.Placeholder = "输入消息... (Enter 发送)"
	ti.CharLimit = 2000
	ti.Width = 60
	return ChatModel{
		input:     ti,
		agentMode: true, // 默认开启 Agent 模式
		messages: []chatMessage{
			{Role: "system", Content: "AI 助手就绪 [Agent 模式]。支持工具调用（bash/文件操作/会话管理）。"},
		},
	}
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
			m.messages = append(m.messages, chatMessage{Role: "system", Content: "错误: " + msg.Error})
		} else {
			m.messages = append(m.messages, chatMessage{Role: "assistant", Content: msg.Text})
		}
		m.scrollToBottom()
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			if m.input.Focused() && !m.waiting {
				text := strings.TrimSpace(m.input.Value())
				if text != "" {
					m.messages = append(m.messages, chatMessage{Role: "user", Content: text})
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
			// 清除聊天历史
			if !m.input.Focused() && !m.waiting {
				m.messages = []chatMessage{
					{Role: "system", Content: "聊天历史已清除。"},
				}
				m.scroll = 0
				return m, func() tea.Msg { return ChatClearMsg{} }
			}
		case "a":
			// 切换 Agent 模式
			if !m.input.Focused() && !m.waiting {
				m.agentMode = !m.agentMode
				mode := "简单问答"
				if m.agentMode {
					mode = "Agent（工具调用）"
				}
				m.messages = append(m.messages, chatMessage{Role: "system", Content: fmt.Sprintf("已切换到 %s 模式", mode)})
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
	userStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("117")) // 蓝色
	assistStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("156")) // 绿色
	sysStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))   // 灰色

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
			prefix = "你: "
			style = userStyle
		case "assistant":
			prefix = "AI: "
			style = assistStyle
		default:
			prefix = "  "
			style = sysStyle
		}
		// 按行拆分内容
		contentLines := strings.Split(msg.Content, "\n")
		for i, cl := range contentLines {
			if i == 0 {
				lines = append(lines, style.Render(prefix+wrapLine(cl, maxWidth-len([]rune(prefix)))+""))
			} else {
				pad := strings.Repeat(" ", len([]rune(prefix)))
				lines = append(lines, style.Render(pad+wrapLine(cl, maxWidth-len([]rune(prefix)))+""))
			}
		}
	}
	if m.waiting {
		lines = append(lines, sysStyle.Render("  ⏳ 思考中..."))
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

	// 消息区域
	lines := m.renderLines()
	viewHeight := m.height - 3 // 留出输入框和提示行
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
	// 填充空行
	for i := end - start; i < viewHeight; i++ {
		b.WriteString("\n")
	}

	// 分隔线
	w := m.width - 4
	if w < 20 {
		w = 60
	}
	b.WriteString("  " + strings.Repeat("─", w) + "\n")

	// 输入框
	b.WriteString("  " + m.input.View() + "\n")

	// 提示
	hint := "i:输入  Enter:发送  Esc:退出输入  c:清除  a:切换模式  ↑↓:滚动"
	if m.waiting {
		hint = "等待响应中..."
	}
	modeLabel := "问答"
	if m.agentMode {
		modeLabel = "Agent"
	}
	b.WriteString(dimStyle.Render(fmt.Sprintf("  %s  [%s]  消息:%d", hint, modeLabel, len(m.messages)-1)))

	return b.String()
}
