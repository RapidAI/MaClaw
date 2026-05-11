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
//
// 渲染管线设计：
//
//	renderLines() 是 O(messages × width) 的纯计算函数（Markdown 渲染、文本换行）。
//	为避免在一个 View() 调用中重复计算，引入 cachedLines 缓存。
//	任何修改 messages/width 的操作通过 invalidateCache() 标记缓存失效。
//	getLines() 是唯一的缓存入口——首次调用时计算并缓存，后续调用直接返回。
//	scrollToBottom/scrollDown/View 全部通过 getLines() 读取，零重复计算。
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

	// 渲染缓存：避免 renderLines() 在一个事件周期内被重复调用。
	cachedLines []string
	cacheValid  bool

	// 预输入队列：agent 忙碌时用户输入的消息暂存于此。
	// agent 完成后自动发射第一条，或用户可手动管理（删除/编辑/发射）。
	queue       []BufferEntry
	queueCursor int  // 当前选中的队列项索引（-1 表示无选中）
	queueActive bool // 队列面板是否激活（显示操作提示）
}

// BufferEntry 预输入队列中的一条消息。
type BufferEntry struct {
	Text      string
	CreatedAt int64 // unix timestamp
}

// ChatQueueFireMsg 从队列中发射一条消息（由 ChatModel 发出，app.go 消费）。
type ChatQueueFireMsg struct {
	Text      string
	AgentMode bool
}

// removeQueueEntry 移除队列中 cursor 位置的条目，调整 cursor 和 active 状态。
// 返回被移除的条目。调用方必须确保 queue 非空且 queueCursor 有效。
func (m *ChatModel) removeQueueEntry() BufferEntry {
	entry := m.queue[m.queueCursor]
	m.queue = append(m.queue[:m.queueCursor], m.queue[m.queueCursor+1:]...)
	if m.queueCursor >= len(m.queue) {
		m.queueCursor = max(0, len(m.queue)-1)
	}
	if len(m.queue) == 0 {
		m.queueActive = false
	}
	m.invalidateCache()
	return entry
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
	oldLang := m.lang
	m.lang = i18n.NormalizeLang(lang)
	m.input.Placeholder = i18n.T(i18n.MsgTUIChatInputPlaceholder, m.lang)
	for i := range m.messages {
		m.messages[i].Content = translateChatSystemMessage(m.messages[i].Content, oldLang, m.lang)
	}
	m.invalidateCache()
}

func translateChatSystemMessage(text, oldLang, newLang string) string {
	keys := []string{
		i18n.MsgTUIChatSystemReady,
		i18n.MsgTUIChatClearedMessage,
	}
	for _, key := range keys {
		for _, lang := range []string{oldLang, "zh", "en"} {
			if text == i18n.T(key, lang) {
				return i18n.T(key, newLang)
			}
		}
	}
	return text
}

// FocusInput 聚焦输入框。
func (m *ChatModel) FocusInput() {
	m.input.Focus()
}

// invalidateCache 标记渲染缓存失效。
// 任何修改 messages、width、waiting、spinnerTick 的操作都必须调用此方法。
func (m *ChatModel) invalidateCache() {
	m.cacheValid = false
}

// getLines 返回当前渲染行列表（带缓存）。
// 这是渲染管线的唯一入口——scrollToBottom、scrollDown、View 全部通过此方法读取。
func (m *ChatModel) getLines() []string {
	if !m.cacheValid {
		m.cachedLines = m.renderLines()
		m.cacheValid = true
	}
	return m.cachedLines
}

// AppendSystemMessage 追加一条系统消息到聊天记录。
func (m *ChatModel) AppendSystemMessage(text string) {
	m.messages = append(m.messages, ChatMessage{Role: "system", Content: text})
	m.waiting = false
	m.invalidateCache()
	m.scrollToBottom()
}

// ClearMessages resets the chat to a single system message.
func (m *ChatModel) ClearMessages(systemMsg string) {
	m.messages = []ChatMessage{{Role: "system", Content: systemMsg}}
	m.scroll = 0
	m.waiting = false
	m.invalidateCache()
}

// SetMessages 设置聊天消息列表（用于从 memoryshot 恢复）。
func (m *ChatModel) SetMessages(msgs []ChatMessage) {
	if len(m.messages) > 0 && m.messages[0].Role == "system" {
		m.messages = append([]ChatMessage{m.messages[0]}, msgs...)
	} else {
		m.messages = msgs
	}
	m.invalidateCache()
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
		m.input.Width = min(60, max(8, m.width-4))
		m.invalidateCache() // width 变化影响换行
		// 终端缩小时 scroll 可能超出新的 maxScroll，钳位但不强制到底部
		m.clampScroll()
	case ChatResponseMsg:
		m.waiting = false
		m.invalidateCache()
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
		m.invalidateCache()
		m.scrollToBottom()
		// 自动发射队列中的第一条消息（agent 刚完成，队列非空）。
		if len(m.queue) > 0 {
			m.queueCursor = 0
			entry := m.removeQueueEntry()
			m.messages = append(m.messages, ChatMessage{Role: "user", Content: entry.Text})
			m.waiting = true
			m.spinnerTick = 0
			m.scrollToBottom()
			agentMode := m.agentMode
			return m, tea.Batch(
				func() tea.Msg { return ChatQueueFireMsg{Text: entry.Text, AgentMode: agentMode} },
				tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return chatTickMsg{} }),
			)
		}
		return m, nil
	case chatTickMsg:
		if m.waiting {
			m.spinnerTick++
			m.invalidateCache() // spinner frame 变化
			return m, tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return chatTickMsg{} })
		}
		return m, nil
	case ChatStreamMsg:
		m.invalidateCache()
		switch msg.Type {
		case "tool_call":
			// 当 tool_call 到达时，如果最后一条消息是流式产生的 assistant 消息
			// （同一 LLM 迭代中先输出 text 再输出 tool_calls），这段中间文本
			// 不是最终回复——移除它。最终文本会通过 ChatResponseMsg 到达。
			// 判断依据：最后一条消息是 assistant 且在最后一条 user 消息之后。
			if idx := m.lastAssistantAfterUser(); idx >= 0 && idx == len(m.messages)-1 {
				m.messages = m.messages[:len(m.messages)-1]
			}
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
				text := strings.TrimSpace(m.input.Value())
				if text == "" {
					return m, nil
				}
				if m.waiting {
					// Agent 忙碌时，消息进入预输入队列。
					m.queue = append(m.queue, BufferEntry{
						Text:      text,
						CreatedAt: time.Now().Unix(),
					})
					m.queueCursor = len(m.queue) - 1
					m.queueActive = true
					m.input.SetValue("")
					m.invalidateCache()
				} else {
					m.messages = append(m.messages, ChatMessage{Role: "user", Content: text})
					m.input.SetValue("")
					m.waiting = true
					m.spinnerTick = 0
					m.queueActive = false
					m.invalidateCache()
					m.scrollToBottom()
					agentMode := m.agentMode
					return m, tea.Batch(
						func() tea.Msg { return ChatSendMsg{Text: text, AgentMode: agentMode} },
						tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return chatTickMsg{} }),
					)
				}
				return m, nil
			case "esc":
				if m.queueActive {
					m.queueActive = false
					m.invalidateCache()
					return m, nil
				}
				m.input.Blur()
				return m, nil
			// 队列操作快捷键（输入框聚焦 + 队列非空时）
			case "ctrl+d":
				if len(m.queue) > 0 && m.queueActive {
					m.removeQueueEntry()
					return m, nil
				}
			case "ctrl+e":
				if len(m.queue) > 0 && m.queueActive {
					entry := m.removeQueueEntry()
					m.input.SetValue(entry.Text)
					m.input.CursorEnd()
					return m, nil
				}
			case "ctrl+f":
				if len(m.queue) > 0 && m.queueActive && !m.waiting {
					entry := m.removeQueueEntry()
					m.messages = append(m.messages, ChatMessage{Role: "user", Content: entry.Text})
					m.waiting = true
					m.spinnerTick = 0
					m.scrollToBottom()
					agentMode := m.agentMode
					return m, tea.Batch(
						func() tea.Msg { return ChatQueueFireMsg{Text: entry.Text, AgentMode: agentMode} },
						tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return chatTickMsg{} }),
					)
				}
			case "ctrl+up":
				if m.queueActive && m.queueCursor > 0 {
					m.queueCursor--
					m.invalidateCache()
					return m, nil
				}
			case "ctrl+down":
				if m.queueActive && m.queueCursor < len(m.queue)-1 {
					m.queueCursor++
					m.invalidateCache()
					return m, nil
				}
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
				m.invalidateCache()
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
				m.invalidateCache()
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

func (m *ChatModel) viewHeight() int {
	vh := m.height - 3 // 减去分隔线、输入框、状态栏
	// 预输入队列占用的行数。
	if n := len(m.queue); n > 0 {
		show := n
		if show > 3 {
			show = 4 // 3 items + "还有 N 条"
		}
		if m.queueActive {
			show++ // 操作提示行
		}
		vh -= show
	}
	if vh < 1 {
		vh = 1
	}
	return vh
}

// clampScroll 将 scroll 钳位到有效范围 [0, maxScroll]。
// 用于终端缩小后 scroll 可能越界的场景，不改变用户的阅读位置。
func (m *ChatModel) clampScroll() {
	lines := m.getLines()
	maxScroll := len(lines) - m.viewHeight()
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.scroll > maxScroll {
		m.scroll = maxScroll
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
}

func (m *ChatModel) scrollDown() {
	lines := m.getLines()
	maxScroll := len(lines) - m.viewHeight()
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.scroll < maxScroll {
		m.scroll++
	}
}

func (m *ChatModel) scrollToBottom() {
	lines := m.getLines()
	maxScroll := len(lines) - m.viewHeight()
	if maxScroll < 0 {
		maxScroll = 0
	}
	m.scroll = maxScroll
}

// renderLines 将所有消息渲染为行列表。
// 这是一个纯计算函数——不要直接调用，通过 getLines() 使用缓存版本。
func (m ChatModel) renderLines() []string {
	userStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("117"))
	assistStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("156"))
	sysStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	toolStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // orange for tool calls

	maxWidth := m.contentWidth()

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
				lines = append(lines, truncateToWidthVisible("  "+ml, maxWidth))
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

func (m ChatModel) contentWidth() int {
	width := m.width
	if width <= 0 {
		width = 80
	}
	return max(8, width-6)
}

// wrapLine 自动换行：将超长行按 maxW 显示宽度折行，返回多行。
// 使用 displayWidth 正确处理 CJK 字符（宽度 2）和 ASCII 字符（宽度 1）。
func wrapLine(s string, maxW int) string {
	if maxW <= 0 {
		return s
	}
	if displayWidth(s) <= maxW {
		return s
	}
	runes := []rune(s)
	var lines []string
	lineStart := 0
	w := 0
	for i, r := range runes {
		// Zero-width modifiers: don't count toward line width.
		if r == 0xFE0F || r == 0xFE0E || r == 0x200D {
			continue
		}
		rw := 1
		if r >= 0x1100 && isCJKOrFullwidth(r) {
			rw = 2
		}
		if w+rw > maxW {
			lines = append(lines, string(runes[lineStart:i]))
			lineStart = i
			w = 0
		}
		w += rw
	}
	if lineStart < len(runes) {
		lines = append(lines, string(runes[lineStart:]))
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
//
// 渲染管线：getLines()（缓存）→ 视口切片 → 滚动条 → 输入框 → 状态栏。
// getLines() 在一个 View() 调用中只计算一次（缓存命中），
// 滚动条和状态栏的 totalLines 信息零额外开销。
func (m ChatModel) View() string {
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	var b strings.Builder
	viewHeight := m.viewHeight()

	if !m.hasUserMessages() {
		m.renderWelcomeView(&b, viewHeight)
	} else {
		lines := m.getLines() // 缓存入口——整个 View() 只调用一次
		totalLines := len(lines)
		start := m.scroll
		end := start + viewHeight
		if end > totalLines {
			end = totalLines
		}
		if start > end {
			start = end
		}

		// 滚动条：内容超出视口时在右侧显示。
		needsScrollBar := totalLines > viewHeight
		var scrollTrack []rune
		if needsScrollBar {
			scrollTrack = buildScrollTrack(viewHeight, totalLines, m.scroll)
		}

		visibleCount := end - start
		for i := 0; i < visibleCount; i++ {
			line := lines[start+i]
			if needsScrollBar {
				line = truncateToWidthVisible(line, max(1, m.width-4))
				b.WriteString("  " + line + " " + renderTrackChar(scrollTrack[i]) + "\n")
			} else {
				line = truncateToWidthVisible(line, max(1, m.width-2))
				b.WriteString("  " + line + "\n")
			}
		}
		// 填充剩余空行（内容不足 viewHeight 时补齐，保持滚动条轨道连续）
		for i := visibleCount; i < viewHeight; i++ {
			if needsScrollBar && i < len(scrollTrack) {
				// 对齐：2(左边距) + (width-4)(内容区) + 1(间隔) + 1(轨道) = width
				pad := strings.Repeat(" ", max(0, m.width-3))
				b.WriteString("  " + pad + renderTrackChar(scrollTrack[i]) + "\n")
			} else {
				b.WriteString("\n")
			}
		}
	}

	w := m.width - 4
	if w < 1 {
		w = 1
	}
	b.WriteString("  " + strings.Repeat("─", w) + "\n")

	// 预输入队列显示（在分隔线和输入框之间）。
	if len(m.queue) > 0 {
		queueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
		selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57"))
		maxShow := 3 // 最多显示 3 条，避免占据太多空间
		showCount := len(m.queue)
		if showCount > maxShow {
			showCount = maxShow
		}
		for i := 0; i < showCount; i++ {
			entry := m.queue[i]
			preview := entry.Text
			if len([]rune(preview)) > 40 {
				preview = string([]rune(preview)[:40]) + "..."
			}
			label := fmt.Sprintf("  📋 %d. %s", i+1, preview)
			if m.queueActive && i == m.queueCursor {
				b.WriteString(selectedStyle.Render(fitDisplay(label, max(1, m.width-2))) + "\n")
			} else {
				b.WriteString(queueStyle.Render(fitDisplay(label, max(1, m.width-2))) + "\n")
			}
		}
		if len(m.queue) > maxShow {
			b.WriteString(queueStyle.Render(fmt.Sprintf("  ... 还有 %d 条", len(m.queue)-maxShow)) + "\n")
		}
		if m.queueActive {
			hintText := "  Ctrl+D:删除  Ctrl+E:编辑  Ctrl+F:发射  Ctrl+↑↓:选择  Esc:关闭"
			b.WriteString(dimStyle.Render(fitDisplay(hintText, max(1, m.width-2))) + "\n")
		}
	}

	b.WriteString("  " + m.input.View() + "\n")

	hint := i18n.T(i18n.MsgTUIChatHint, m.lang)
	if m.waiting {
		hint = i18n.T(i18n.MsgTUIChatAwaitingResponse, m.lang)
	}
	modeLabel := i18n.T(i18n.MsgTUIChatModeLabelSimple, m.lang)
	if m.agentMode {
		modeLabel = i18n.T(i18n.MsgTUIChatModeLabelAgent, m.lang)
	}

	// 滚动位置指示器：基于 getLines() 缓存，零额外开销。
	scrollInfo := ""
	if totalLines := len(m.getLines()); totalLines > viewHeight {
		maxScroll := totalLines - viewHeight
		if maxScroll < 1 {
			maxScroll = 1
		}
		pct := m.scroll * 100 / maxScroll
		if pct > 100 {
			pct = 100
		}
		scrollInfo = fmt.Sprintf("  ↕%d%%", pct)
	}

	statusText := fmt.Sprintf("%s  [%s]  %s%s", hint, modeLabel, i18n.Tf(i18n.MsgTUIChatMessageCount, m.lang, len(m.messages)-1), scrollInfo)
	b.WriteString("  " + dimStyle.Render(fitDisplay(statusText, max(1, m.width-2))))

	return b.String()
}

// 滚动条样式（包级别，避免每帧重复分配）。
var (
	scrollThumbStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252")) // light gray thumb — visible but not distracting
	scrollTrackStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("238")) // dark gray track — subtle
)

// renderTrackChar 渲染单个滚动条轨道字符（带颜色）。
// 滑块 '┃' 用浅灰色（明显但不刺眼），轨道 '│' 用深灰色（微妙）。
func renderTrackChar(ch rune) string {
	if ch == '┃' {
		return scrollThumbStyle.Render(string(ch))
	}
	return scrollTrackStyle.Render(string(ch))
}

// buildScrollTrack 生成垂直滚动条轨道。
// 返回 viewHeight 长度的 rune 切片。滑块用 '┃'（粗竖线），轨道用 '│'（细竖线）。
// 滑块大小上限为 viewHeight 的 1/3，确保滑块不会占据大部分轨道。
func buildScrollTrack(viewHeight, totalLines, scroll int) []rune {
	track := make([]rune, viewHeight)
	for i := range track {
		track[i] = '│'
	}

	if totalLines <= viewHeight || viewHeight < 1 {
		return track
	}

	// 滑块大小：viewport/total 比例，最小 1 行，最大 viewHeight/3。
	thumbSize := viewHeight * viewHeight / totalLines
	if thumbSize < 1 {
		thumbSize = 1
	}
	maxThumb := viewHeight / 3
	if maxThumb < 1 {
		maxThumb = 1
	}
	if thumbSize > maxThumb {
		thumbSize = maxThumb
	}

	maxScroll := totalLines - viewHeight
	if maxScroll < 1 {
		maxScroll = 1
	}

	// 滑块位置：scroll/maxScroll 比例。
	thumbStart := scroll * (viewHeight - thumbSize) / maxScroll
	if thumbStart < 0 {
		thumbStart = 0
	}
	if thumbStart+thumbSize > viewHeight {
		thumbStart = viewHeight - thumbSize
	}

	for i := thumbStart; i < thumbStart+thumbSize && i < viewHeight; i++ {
		track[i] = '┃'
	}

	return track
}

func chatLocalText(lang, key string) string {
	if i18n.NormalizeLang(lang) == "en" {
		texts := map[string]string{
			"welcomeHint": "Type a message to start, or type /help to view commands",
		}
		if text, ok := texts[key]; ok {
			return text
		}
	}
	texts := map[string]string{
		"welcomeHint": "输入消息开始对话，或输入 /help 查看命令",
	}
	if text, ok := texts[key]; ok {
		return text
	}
	return key
}

// renderWelcomeView renders the MaClaw logo centered in the chat area.
func (m ChatModel) renderWelcomeView(b *strings.Builder, viewHeight int) {
	logoStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("39")).
		Bold(true)
	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("245"))

	if m.width > 0 && m.width < 84 {
		m.renderCompactWelcomeView(b, viewHeight)
		return
	}

	logoLines := []string{
		"  ███╗   ███╗ █████╗  ██████╗██╗      █████╗ ██╗    ██╗",
		"  ████╗ ████║██╔══██╗██╔════╝██║     ██╔══██╗██║    ██║",
		"  ██╔████╔██║███████║██║     ██║     ███████║██║ █╗ ██║",
		"  ██║╚██╔╝██║██╔══██║██║     ██║     ██╔══██║██║███╗██║",
		"  ██║ ╚═╝ ██║██║  ██║╚██████╗███████╗██║  ██║╚███╔███╔╝",
		"  ╚═╝     ╚═╝╚═╝  ╚═╝ ╚═════╝╚══════╝╚═╝  ╚═╝ ╚══╝╚══╝",
	}

	contentLines := len(logoLines) + 3
	topPad := (viewHeight - contentLines) / 2
	if topPad < 0 {
		topPad = 0
	}

	for i := 0; i < topPad; i++ {
		b.WriteString("\n")
	}
	for _, line := range logoLines {
		b.WriteString(logoStyle.Render(fitDisplay(line, max(1, m.width))) + "\n")
	}
	b.WriteString("\n")
	b.WriteString(hintStyle.Render("  "+fitDisplay(i18n.T(i18n.MsgTUIChatSystemReady, m.lang), max(1, m.width-2))) + "\n")
	b.WriteString(hintStyle.Render("  "+fitDisplay(chatLocalText(m.lang, "welcomeHint"), max(1, m.width-2))) + "\n")

	used := topPad + contentLines
	for i := used; i < viewHeight; i++ {
		b.WriteString("\n")
	}
}

func (m ChatModel) renderCompactWelcomeView(b *strings.Builder, viewHeight int) {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	lines := []string{
		titleStyle.Render("  MaClaw"),
		hintStyle.Render("  " + fitDisplay(i18n.T(i18n.MsgTUIChatSystemReady, m.lang), max(1, m.width-2))),
		hintStyle.Render("  " + fitDisplay(chatLocalText(m.lang, "welcomeHint"), max(1, m.width-2))),
	}
	topPad := (viewHeight - len(lines)) / 2
	if topPad < 0 {
		topPad = 0
	}
	for i := 0; i < topPad; i++ {
		b.WriteString("\n")
	}
	for _, line := range lines {
		b.WriteString(truncateToWidthVisible(line, max(1, m.width)) + "\n")
	}
	for i := topPad + len(lines); i < viewHeight; i++ {
		b.WriteString("\n")
	}
}
