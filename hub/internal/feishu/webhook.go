package feishu

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/session"
	"github.com/go-lark/lark/v2"
)

const verifyCodeTTL = 10 * time.Minute

// WebhookHandler handles Feishu event callbacks.
//
// Binding flow (2-step email verification):
//  1. User sends their Hub email → Hub sends a 6-digit code to that email.
//  2. User sends the code back → verified, open_id bound to email.
func WebhookHandler(n *Notifier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// GET requests (browser probes, health checks) get a simple OK.
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"ok":true,"service":"feishu-webhook"}`))
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB max
		if err != nil {
			http.Error(w, "read body failed", http.StatusBadRequest)
			return
		}

		log.Printf("[feishu/webhook] received POST (%d bytes)", len(body))

		// Feishu URL verification challenge — must respond even if notifier is nil,
		// because the admin configures the webhook URL before the bot is fully wired.
		var challenge struct {
			Challenge string `json:"challenge"`
			Type      string `json:"type"`
		}
		if json.Unmarshal(body, &challenge) == nil && challenge.Type == "url_verification" && challenge.Challenge != "" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"challenge": challenge.Challenge})
			return
		}

		if n == nil || n.bot == nil {
			http.Error(w, "feishu not configured", http.StatusServiceUnavailable)
			return
		}

		// Parse the event envelope.
		var envelope struct {
			Schema string `json:"schema"`
			Header struct {
				EventType string `json:"event_type"`
			} `json:"header"`
			Event   json.RawMessage `json:"event"`
			Encrypt string          `json:"encrypt"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		// If the event is encrypted, we cannot process it.
		if envelope.Encrypt != "" {
			log.Printf("[feishu/webhook] received encrypted event — Encrypt Key is set in Feishu app config. Please remove it or leave it empty.")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"ok":true}`))
			return
		}

		log.Printf("[feishu/webhook] event_type=%s", envelope.Header.EventType)

		// We only care about im.message.receive_v1 (user sent a message to bot).
		if envelope.Header.EventType == "im.message.receive_v1" {
			go handleBotMessage(n, envelope.Event)
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}
}

func handleBotMessage(n *Notifier, raw json.RawMessage) {
	var event struct {
		Sender struct {
			SenderID struct {
				OpenID string `json:"open_id"`
			} `json:"sender_id"`
		} `json:"sender"`
		Message struct {
			MessageType string `json:"message_type"`
			Content     string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &event); err != nil {
		log.Printf("[feishu/webhook] parse event failed: %v", err)
		return
	}

	openID := event.Sender.SenderID.OpenID
	if openID == "" {
		return
	}

	text := extractText(event.Message.Content)

	// --- IM Adapter routing ---
	// If a FeishuPlugin is wired and the IM Adapter is active, convert the
	// message to IncomingMessage and route through the adapter pipeline.
	// Legacy flows (email binding, verification codes, slash commands when
	// no adapter) are preserved as fallback.
	if n.plugin != nil {
		// Email binding and verification code flows are always handled by
		// the legacy path — they are Feishu-specific onboarding flows that
		// the generic IM Adapter does not handle.
		// Agent conversation commands (/new, /reset, /clear) are routed to
		// the adapter so the agent can handle them directly.
		if text != "" && !looksLikeEmail(text) && !isVerifyCode(text) {
			// Legacy slash commands stay in the legacy path, except for
			// agent conversation reset commands which the adapter handles.
			isAgentCmd := text == "/new" || text == "/reset" || text == "/clear" || strings.HasPrefix(text, "/call ") || strings.HasPrefix(text, "/discuss ") || text == "/discuss" || text == "/stop"
			if isAgentCmd || !strings.HasPrefix(text, "/") {
				msgType := event.Message.MessageType
				if msgType == "" {
					msgType = "text"
				}
				if n.plugin.DispatchBotMessage(openID, msgType, text, raw) {
					return
				}
			}
		}
	}

	// --- Legacy path (backward compatible) ---
	if text == "" {
		replyText(n, openID, welcomeOrHelp(n, openID))
		return
	}

	// Command routing: /help, /machines, /sessions, /detail, /send, /interrupt, /kill, /use, /exit, /screenshot
	if strings.HasPrefix(text, "/") {
		handleCommand(n, openID, text)
		return
	}

	// If user has an active session context, send text as input directly.
	n.activeMu.RLock()
	activeID := n.activeSession[openID]
	n.activeMu.RUnlock()
	if activeID != "" {
		handleSendInput(n, openID, []string{activeID, text})
		return
	}

	// If the text looks like a 6-digit code, try to verify.
	if isVerifyCode(text) {
		handleVerifyCode(n, openID, text)
		return
	}

	// If the text looks like an email, start the verification flow.
	if looksLikeEmail(text) {
		handleEmailSubmit(n, openID, strings.ToLower(text))
		return
	}

	replyText(n, openID, welcomeOrHelp(n, openID))
}

// handleCommand dispatches slash commands.
func handleCommand(n *Notifier, openID, text string) {
	parts := strings.Fields(text)
	cmd := strings.ToLower(parts[0])
	args := parts[1:]

	switch cmd {
	case "/help", "/h":
		replyText(n, openID, welcomeOrHelp(n, openID))
	case "/machines", "/m":
		handleListMachines(n, openID)
	case "/sessions", "/s":
		handleListSessions(n, openID, args)
	case "/detail", "/d":
		handleSessionDetail(n, openID, args)
	case "/send":
		handleSendInput(n, openID, args)
	case "/interrupt", "/i":
		handleInterrupt(n, openID, args)
	case "/kill", "/k":
		handleKill(n, openID, args)
	case "/use", "/u":
		handleUseSession(n, openID, args)
	case "/call":
		handleCallMachine(n, openID, args)
	case "/info":
		handleInfo(n, openID)
	case "/screenshot", "/sc":
		handleScreenshot(n, openID, args)
	case "/exit", "/e", "/quit", "/q":
		handleExitSession(n, openID)
	case "/unbind":
		handleUnbind(n, openID)
	default:
		replyText(n, openID, "未知命令。输入 /help 查看可用命令。\nUnknown command. Type /help for available commands.")
	}
}

func helpText() string {
	return "📋 可用命令 / Available Commands:\n\n" +
		"/info — 查看概览（有会话时显示会话详情）\n" +
		"/machines — 查看设备列表\n" +
		"/call <昵称> — 切换目标设备（也可直接发送设备昵称切换）\n" +
		"/call all — 进入群聊模式，消息发送给所有在线设备\n" +
		"/discuss <话题> — AI 多轮讨论模式，设备间互相印证\n" +
		"/stop — 终止讨论 / 退出讨论模式\n" +
		"/sessions — 查看会话列表\n" +
		"/use <编号> — 切换到会话（之后直接发文本即为命令）\n" +
		"/exit — 退出当前会话上下文\n" +
		"/detail <编号> — 查看会话详情\n" +
		"/send <编号> <text> — 发送命令到会话\n" +
		"/screenshot <编号> [窗口标题] — 截屏并发送到此对话\n" +
		"/interrupt <编号> — 中断会话\n" +
		"/kill <编号> — 终止会话\n" +
		"/unbind — 解除飞书绑定\n" +
		"/help — 显示此帮助\n\n" +
		"💡 先 /machines 查看设备，/call 昵称 切换设备，再发消息给 Agent。"
}

// bindingGuide returns onboarding text for users who haven't bound their email yet.
func bindingGuide() string {
	return "👋 欢迎使用 MaClaw Hub 飞书机器人！\n\n" +
		"使用前需要先绑定您的 Hub 账号。步骤很简单：\n\n" +
		"1️⃣ 直接发送您的 Hub 注册邮箱（例如: you@example.com）\n" +
		"2️⃣ 系统会向该邮箱发送 6 位验证码\n" +
		"3️⃣ 在此对话中回复验证码即可完成绑定\n\n" +
		"绑定后即可通过飞书查看设备、管理会话、接收通知。\n\n" +
		"Welcome! Please send your Hub email to get started."
}

// welcomeOrHelp returns the binding guide for unbound users, or the command
// help text for users who have already bound their email.
func welcomeOrHelp(n *Notifier, openID string) string {
	if n.resolveUserID(openID) == "" {
		return bindingGuide()
	}
	return helpText()
}

// resolveUserID returns the Hub userID for the given open_id via the email binding.
func (n *Notifier) resolveUserID(openID string) string {
	// Find email for this open_id.
	n.oidMu.RLock()
	var email string
	for e, oid := range n.oidCache {
		if oid == openID {
			email = e
			break
		}
	}
	n.oidMu.RUnlock()
	if email == "" {
		return ""
	}
	user, err := n.users.GetByEmail(context.Background(), email)
	if err != nil || user == nil {
		return ""
	}
	return user.ID
}

func handleListMachines(n *Notifier, openID string) {
	if n.devices == nil {
		replyText(n, openID, "⚠️ 设备服务未配置。")
		return
	}
	userID := n.resolveUserID(openID)
	if userID == "" {
		replyText(n, openID, bindingGuide())
		return
	}

	machines, err := n.devices.ListMachines(context.Background(), userID)
	if err != nil {
		replyText(n, openID, fmt.Sprintf("❌ 获取设备列表失败: %v", err))
		return
	}
	if len(machines) == 0 {
		replyText(n, openID, "暂无设备。No machines found.")
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🖥 设备列表 (%d 台):\n\n", len(machines)))
	for _, m := range machines {
		status := "🔴 离线"
		if m.Online {
			status = "🟢 在线"
		}
		name := m.Name
		if name == "" {
			name = m.Hostname
		}
		if name == "" {
			name = m.MachineID[:8]
		}
		aliasTag := ""
		if m.Alias != "" && m.Alias != name {
			aliasTag = fmt.Sprintf(" (%s)", m.Alias)
		}
		sb.WriteString(fmt.Sprintf("%s %s%s [%s]\n  ID: %s\n", status, name, aliasTag, m.Platform, shortID(m.MachineID)))
		if m.Online && !m.LLMConfigured {
			sb.WriteString("  ⚠️ LLM 未配置，Agent 无法运行\n")
		}
		if m.ActiveSessions > 0 {
			sb.WriteString(fmt.Sprintf("  活跃会话: %d\n", m.ActiveSessions))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("使用 /sessions <machine_id前8位> 查看会话")
	replyText(n, openID, sb.String())
}

func handleCallMachine(n *Notifier, openID string, args []string) {
	if len(args) == 0 {
		replyText(n, openID, "用法: /call <设备昵称>\n\n输入 /machines 查看在线设备列表。")
		return
	}
	// In legacy mode (no IM adapter), just show a hint.
	replyText(n, openID, "⚠️ /call 命令需要通过 IM Adapter 处理。请确认 IM 插件已启用。")
}

func handleListSessions(n *Notifier, openID string, args []string) {
	if n.sessions == nil || n.devices == nil {
		replyText(n, openID, "⚠️ 会话服务未配置。")
		return
	}
	userID := n.resolveUserID(openID)
	if userID == "" {
		replyText(n, openID, bindingGuide())
		return
	}

	// Pre-assign aliases so numbers are stable.
	n.buildAliasesForUser(openID)

	machines, err := n.devices.ListMachines(context.Background(), userID)
	if err != nil {
		replyText(n, openID, fmt.Sprintf("❌ 获取设备列表失败: %v", err))
		return
	}

	machineFilter := ""
	if len(args) > 0 {
		machineFilter = strings.ToLower(args[0])
	}

	var sb strings.Builder
	total := 0
	for _, m := range machines {
		if machineFilter != "" && !strings.HasPrefix(strings.ToLower(m.MachineID), machineFilter) {
			continue
		}
		sessions, err := n.sessions.ListByMachine(context.Background(), userID, m.MachineID)
		if err != nil {
			continue
		}
		for _, s := range sessions {
			total++
			alias := n.getAlias(openID, s.SessionID)
			status := statusEmoji(s.Summary.Status) + " " + s.Summary.Status
			title := s.Summary.Title
			if title == "" {
				title = s.Summary.Tool
			}
			sb.WriteString(fmt.Sprintf("[%s] %s\n  工具: %s | 状态: %s\n",
				alias, truncate(title, 40), s.Summary.Tool, status))
			if s.Summary.CurrentTask != "" {
				sb.WriteString(fmt.Sprintf("  任务: %s\n", truncate(s.Summary.CurrentTask, 60)))
			}
			if s.Summary.WaitingForUser {
				sb.WriteString("  ⚠️ 等待用户操作\n")
			}
			sb.WriteString("\n")
		}
	}

	if total == 0 {
		replyText(n, openID, "暂无活跃会话。No active sessions.")
		return
	}
	header := fmt.Sprintf("📋 会话列表 (%d 个):\n\n", total)
	replyText(n, openID, header+sb.String()+"💡 /use <编号> 切换会话, /detail <编号> 查看详情")
}

func handleSessionDetail(n *Notifier, openID string, args []string) {
	sessionPrefix := ""
	if len(args) >= 1 {
		sessionPrefix = args[0]
	} else {
		n.activeMu.RLock()
		sessionPrefix = n.activeSession[openID]
		n.activeMu.RUnlock()
	}
	if sessionPrefix == "" {
		replyText(n, openID, "用法: /detail <编号>  (或先 /use 切换会话)")
		return
	}
	entry := n.findSession(openID, sessionPrefix)
	if entry == nil {
		replyText(n, openID, "❌ 未找到该会话（仅可查看自己的会话）。")
		return
	}

	s := entry.Summary
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📊 会话详情\n\n"))
	sb.WriteString(fmt.Sprintf("工具: %s\n", s.Tool))
	sb.WriteString(fmt.Sprintf("标题: %s\n", truncate(s.Title, 60)))
	sb.WriteString(fmt.Sprintf("状态: %s %s\n", statusEmoji(s.Status), s.Status))
	if s.CurrentTask != "" {
		sb.WriteString(fmt.Sprintf("当前任务: %s\n", truncate(s.CurrentTask, 80)))
	}
	if s.ProgressSummary != "" {
		sb.WriteString(fmt.Sprintf("进度: %s\n", truncate(s.ProgressSummary, 80)))
	}
	if s.LastCommand != "" {
		sb.WriteString(fmt.Sprintf("最近命令: %s\n", truncate(s.LastCommand, 80)))
	}
	if s.LastResult != "" {
		sb.WriteString(fmt.Sprintf("最近结果: %s\n", truncate(s.LastResult, 120)))
	}
	if s.WaitingForUser {
		action := s.SuggestedAction
		if action == "" {
			action = "请查看终端"
		}
		sb.WriteString(fmt.Sprintf("⚠️ 等待用户: %s\n", action))
	}
	sb.WriteString(fmt.Sprintf("编号: %s\n", n.getAlias(openID, entry.SessionID)))
	sb.WriteString(fmt.Sprintf("Machine ID: %s\n", shortID(entry.MachineID)))

	// Show preview lines if available.
	if len(entry.Preview.PreviewLines) > 0 {
		sb.WriteString("\n--- 终端输出 (最近) ---\n")
		lines := entry.Preview.PreviewLines
		start := 0
		if len(lines) > 10 {
			start = len(lines) - 10
		}
		for _, line := range lines[start:] {
			sb.WriteString(truncate(line, 80) + "\n")
		}
	}

	// Show recent events.
	if len(entry.RecentEvents) > 0 {
		sb.WriteString("\n--- 最近事件 ---\n")
		limit := 5
		if len(entry.RecentEvents) < limit {
			limit = len(entry.RecentEvents)
		}
		for i := len(entry.RecentEvents) - limit; i < len(entry.RecentEvents); i++ {
			ev := entry.RecentEvents[i]
			sb.WriteString(fmt.Sprintf("[%s] %s: %s\n", ev.Severity, ev.Type, truncate(ev.Title, 60)))
		}
	}

	replyText(n, openID, sb.String())
}

func handleSendInput(n *Notifier, openID string, args []string) {
	if len(args) < 2 {
		replyText(n, openID, "用法: /send <编号> <命令文本>")
		return
	}
	// Always verify the session belongs to the current user.
	entry := n.findSession(openID, args[0])
	if entry == nil {
		replyText(n, openID, "❌ 未找到该会话（仅可操作自己的会话）。")
		return
	}
	text := strings.Join(args[1:], " ")
	if err := n.sendSessionCommand(entry, "session.input", map[string]any{"text": text}); err != nil {
		replyText(n, openID, fmt.Sprintf("❌ 发送失败: %v", err))
		return
	}
	replyText(n, openID, fmt.Sprintf("✅ 已发送到会话 %s:\n%s", n.getAlias(openID, entry.SessionID), truncate(text, 100)))
}

func handleInterrupt(n *Notifier, openID string, args []string) {
	sessionPrefix := ""
	if len(args) >= 1 {
		sessionPrefix = args[0]
	} else {
		n.activeMu.RLock()
		sessionPrefix = n.activeSession[openID]
		n.activeMu.RUnlock()
	}
	if sessionPrefix == "" {
		replyText(n, openID, "用法: /interrupt <编号>  (或先 /use 切换会话)")
		return
	}
	entry := n.findSession(openID, sessionPrefix)
	if entry == nil {
		replyText(n, openID, "❌ 未找到该会话（仅可操作自己的会话）。")
		return
	}
	if err := n.sendSessionCommand(entry, "session.interrupt", nil); err != nil {
		replyText(n, openID, fmt.Sprintf("❌ 中断失败: %v", err))
		return
	}
	replyText(n, openID, fmt.Sprintf("⏸ 已发送中断信号到会话 %s", n.getAlias(openID, entry.SessionID)))
}

func handleKill(n *Notifier, openID string, args []string) {
	sessionPrefix := ""
	if len(args) >= 1 {
		sessionPrefix = args[0]
	} else {
		n.activeMu.RLock()
		sessionPrefix = n.activeSession[openID]
		n.activeMu.RUnlock()
	}
	if sessionPrefix == "" {
		replyText(n, openID, "用法: /kill <编号>  (或先 /use 切换会话)")
		return
	}
	entry := n.findSession(openID, sessionPrefix)
	if entry == nil {
		replyText(n, openID, "❌ 未找到该会话（仅可操作自己的会话）。")
		return
	}
	if err := n.sendSessionCommand(entry, "session.kill", nil); err != nil {
		replyText(n, openID, fmt.Sprintf("❌ 终止失败: %v", err))
		return
	}
	// Clear active session if it was the killed one.
	n.activeMu.Lock()
	if strings.HasPrefix(entry.SessionID, n.activeSession[openID]) ||
		strings.HasPrefix(n.activeSession[openID], shortID(entry.SessionID)) {
		delete(n.activeSession, openID)
	}
	n.activeMu.Unlock()
	replyText(n, openID, fmt.Sprintf("⏹ 已发送终止信号到会话 %s", n.getAlias(openID, entry.SessionID)))
}

func handleUseSession(n *Notifier, openID string, args []string) {
	if len(args) < 1 {
		replyText(n, openID, "用法: /use <编号>")
		return
	}
	entry := n.findSession(openID, args[0])
	if entry == nil {
		replyText(n, openID, "❌ 未找到该会话（仅可切换自己的会话）。")
		return
	}
	// Store the full session ID so subsequent commands match exactly.
	n.activeMu.Lock()
	n.activeSession[openID] = entry.SessionID
	n.activeMu.Unlock()

	alias := n.getAlias(openID, entry.SessionID)
	title := entry.Summary.Title
	if title == "" {
		title = entry.Summary.Tool
	}
	replyText(n, openID, fmt.Sprintf("✅ 已切换到会话: %s [%s]\n\n现在直接发文本就是给这个会话发命令。\n输入 /exit 退出会话上下文。",
		truncate(title, 40), alias))
}

// handleInfo shows context-dependent info:
// - If user has an active session: show session detail
// - Otherwise: show an overview of all machines and their session counts
func handleInfo(n *Notifier, openID string) {
	// Check for active session context first.
	n.activeMu.RLock()
	activeID := n.activeSession[openID]
	n.activeMu.RUnlock()

	if activeID != "" {
		// Show current session detail (reuse handleSessionDetail with no args).
		handleSessionDetail(n, openID, nil)
		return
	}

	// No active session — show overview.
	if n.devices == nil {
		replyText(n, openID, "⚠️ 设备服务未配置。")
		return
	}
	userID := n.resolveUserID(openID)
	if userID == "" {
		replyText(n, openID, bindingGuide())
		return
	}

	machines, err := n.devices.ListMachines(context.Background(), userID)
	if err != nil {
		replyText(n, openID, fmt.Sprintf("❌ 获取设备列表失败: %v", err))
		return
	}
	if len(machines) == 0 {
		replyText(n, openID, "暂无设备。")
		return
	}

	var sb strings.Builder
	totalSessions := 0
	onlineCount := 0
	sb.WriteString(fmt.Sprintf("📊 概览 (%d 台设备)\n\n", len(machines)))
	for _, m := range machines {
		status := "🔴 离线"
		if m.Online {
			status = "🟢 在线"
			onlineCount++
		}
		name := m.Name
		if name == "" {
			name = m.Hostname
		}
		if name == "" {
			name = shortID(m.MachineID)
		}
		sb.WriteString(fmt.Sprintf("%s %s [%s]", status, name, m.Platform))

		// Count sessions for this machine.
		if n.sessions != nil {
			sessions, err := n.sessions.ListByMachine(context.Background(), userID, m.MachineID)
			if err == nil && len(sessions) > 0 {
				active := 0
				waiting := 0
				for _, s := range sessions {
					st := strings.ToLower(s.Summary.Status)
					if st == "running" || st == "busy" {
						active++
					}
					if s.Summary.WaitingForUser {
						waiting++
					}
				}
				totalSessions += len(sessions)
				parts := []string{fmt.Sprintf("%d 个会话", len(sessions))}
				if active > 0 {
					parts = append(parts, fmt.Sprintf("%d 运行中", active))
				}
				if waiting > 0 {
					parts = append(parts, fmt.Sprintf("⚠️ %d 等待操作", waiting))
				}
				sb.WriteString(" | ")
				sb.WriteString(strings.Join(parts, ", "))
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("\n在线: %d/%d | 总会话: %d", onlineCount, len(machines), totalSessions))
	sb.WriteString("\n\n💡 /use <会话号> 切换会话, /sessions 查看详细列表")
	replyText(n, openID, sb.String())
}


func handleScreenshot(n *Notifier, openID string, args []string) {
	// Determine which session to screenshot.
	// If user has an active session context, the first arg (if any) is treated
	// as an optional window title rather than a session prefix.
	n.activeMu.RLock()
	activeID := n.activeSession[openID]
	n.activeMu.RUnlock()

	var sessionPrefix, windowTitle string
	if activeID != "" {
		// Active session context — all args are window title.
		sessionPrefix = activeID
		if len(args) >= 1 {
			windowTitle = strings.Join(args, " ")
		}
	} else {
		// No active session — first arg is session prefix, rest is window title.
		if len(args) < 1 {
			replyText(n, openID, "用法: /screenshot <编号> [窗口标题]  (或先 /use 切换会话)\nUsage: /screenshot <id> [window title]  (or /use a session first)")
			return
		}
		sessionPrefix = args[0]
		if len(args) >= 2 {
			windowTitle = strings.Join(args[1:], " ")
		}
	}

	entry := n.findSession(openID, sessionPrefix)
	if entry == nil {
		replyText(n, openID, "❌ 未找到该会话（仅可操作自己的会话）。")
		return
	}

	payload := map[string]any{
		"machine_id": entry.MachineID,
		"session_id": entry.SessionID,
	}
	if windowTitle != "" {
		payload["window_title"] = windowTitle
	}

	if err := n.sendSessionCommand(entry, "session.screenshot", payload); err != nil {
		replyText(n, openID, fmt.Sprintf("❌ 截屏命令发送失败: %v", err))
		return
	}
	replyText(n, openID, fmt.Sprintf("📸 已向会话 %s 发送截屏请求，稍后图片将发送到此对话。", n.getAlias(openID, entry.SessionID)))
}

func handleExitSession(n *Notifier, openID string) {
	n.activeMu.Lock()
	old := n.activeSession[openID]
	delete(n.activeSession, openID)
	n.activeMu.Unlock()
	if old == "" {
		replyText(n, openID, "当前没有活跃的会话上下文。")
	} else {
		replyText(n, openID, fmt.Sprintf("已退出会话 %s 的上下文。", n.getAlias(openID, old)))
	}
}

func handleUnbind(n *Notifier, openID string) {
	n.oidMu.RLock()
	var email string
	for e, oid := range n.oidCache {
		if oid == openID {
			email = e
			break
		}
	}
	n.oidMu.RUnlock()
	if email == "" {
		replyText(n, openID, "当前未绑定任何账号。")
		return
	}
	n.RemoveOpenID(email)
	replyText(n, openID, fmt.Sprintf("✅ 已解除 %s 的绑定。", email))
}

// findSession looks up a session by alias, suffix, or prefix match across all user's machines.
func (n *Notifier) findSession(openID, idInput string) *session.SessionCacheEntry {
	if n.sessions == nil || n.devices == nil {
		return nil
	}
	userID := n.resolveUserID(openID)
	if userID == "" {
		return nil
	}
	// Resolve alias first (e.g. "1" → "sess_1750012345678").
	resolved := n.resolveAlias(openID, strings.TrimSpace(idInput))
	input := strings.ToLower(resolved)

	machines, err := n.devices.ListMachines(context.Background(), userID)
	if err != nil {
		return nil
	}
	for _, m := range machines {
		sessions, err := n.sessions.ListByMachine(context.Background(), userID, m.MachineID)
		if err != nil {
			continue
		}
		for _, s := range sessions {
			sid := strings.ToLower(s.SessionID)
			// Match by: exact, prefix, or numeric suffix (e.g. "177" matches "sess_177").
			if sid == input || strings.HasPrefix(sid, input) {
				return s
			}
			if idx := strings.LastIndex(sid, "_"); idx >= 0 {
				suffix := sid[idx+1:]
				if suffix == input {
					return s
				}
			}
		}
	}
	return nil
}

// sendSessionCommand sends a control message to the machine hosting the session.
func (n *Notifier) sendSessionCommand(entry *session.SessionCacheEntry, msgType string, payload map[string]any) error {
	if n.devices == nil {
		return fmt.Errorf("device service not available")
	}
	if payload == nil {
		payload = map[string]any{}
	}
	msg := map[string]any{
		"type":       msgType,
		"request_id": "feishu-" + entry.SessionID,
		"ts":         time.Now().Unix(),
		"machine_id": entry.MachineID,
		"session_id": entry.SessionID,
		"payload":    payload,
	}
	return n.devices.SendToMachine(entry.MachineID, msg)
}

// handleEmailSubmit validates the email exists in Hub, generates a code,
// sends it to the email, and stores the pending verification.
func handleEmailSubmit(n *Notifier, openID, email string) {
	// Check if this email is already bound to this open_id.
	existing := n.resolveOpenID(email)
	if existing == openID {
		replyText(n, openID, "✅ 该邮箱已绑定，无需重复操作。\n✅ This email is already bound.")
		return
	}

	// Verify the email exists in Hub.
	user, err := n.users.GetByEmail(context.Background(), email)
	if err != nil || user == nil {
		replyText(n, openID, "未找到该邮箱对应的 Hub 用户，请确认邮箱是否正确。\nNo Hub user found for this email.")
		return
	}

	// Generate a 6-digit verification code.
	code := generateCode()

	// Store pending verification (and clean expired entries).
	n.pendMu.Lock()
	now := time.Now()
	for k, v := range n.pending {
		if now.After(v.Expiry) {
			delete(n.pending, k)
		}
	}
	n.pending[openID] = &pendingBind{
		Email:  email,
		Code:   code,
		Expiry: now.Add(verifyCodeTTL),
	}
	n.pendMu.Unlock()

	// Use cross-IM broadcaster if available, otherwise fall back to email-only.
	if n.broadcaster != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		sentTo, err := n.broadcaster.BroadcastVerifyCode(ctx, email, code, "feishu")
		if err != nil {
			log.Printf("[feishu/webhook] broadcast verification code for %s failed: %v", email, err)
			replyText(n, openID, fmt.Sprintf("验证码发送失败: %v", err))
			n.pendMu.Lock()
			delete(n.pending, openID)
			n.pendMu.Unlock()
			return
		}
		replyText(n, openID,
			fmt.Sprintf("验证码已发送到: %s\n请查看验证码，在此对话中回复 6 位验证码完成绑定。\nVerification code sent to: %s. Reply with the 6-digit code.", sentTo, sentTo))
		return
	}

	// Fallback: email-only
	if n.mailer == nil {
		replyText(n, openID, "Hub 邮件服务未配置，无法发送验证码。请联系管理员。\nHub mail service is not configured.")
		n.pendMu.Lock()
		delete(n.pending, openID)
		n.pendMu.Unlock()
		return
	}
	subject := "MaClaw Hub 飞书绑定验证码 / Feishu Binding Verification Code"
	body := fmt.Sprintf(
		"您的飞书绑定验证码是 / Your Feishu binding verification code is:\r\n\r\n    %s\r\n\r\n"+
			"该验证码 %d 分钟内有效。如非本人操作请忽略。\r\n"+
			"This code expires in %d minutes. Ignore this email if you did not request it.\r\n",
		code, int(verifyCodeTTL.Minutes()), int(verifyCodeTTL.Minutes()),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := n.mailer.Send(ctx, []string{email}, subject, body); err != nil {
		log.Printf("[feishu/webhook] send verification email failed (email=%s): %v", email, err)
		replyText(n, openID, "验证码邮件发送失败，请稍后重试或联系管理员。\nFailed to send verification email.")
		n.pendMu.Lock()
		delete(n.pending, openID)
		n.pendMu.Unlock()
		return
	}

	replyText(n, openID,
		fmt.Sprintf("验证码已发送到 %s，请在飞书中回复 6 位验证码完成绑定。\nVerification code sent to %s. Reply with the 6-digit code to complete binding.", email, email))
}

// handleVerifyCode checks the code against the pending verification.
func handleVerifyCode(n *Notifier, openID, code string) {
	n.pendMu.Lock()
	pb, ok := n.pending[openID]
	if ok {
		delete(n.pending, openID)
	}
	n.pendMu.Unlock()

	if !ok || pb == nil {
		replyText(n, openID, "没有待验证的绑定请求。请先发送邮箱地址。\nNo pending binding. Please send your email first.")
		return
	}
	if time.Now().After(pb.Expiry) {
		replyText(n, openID, "验证码已过期，请重新发送邮箱地址。\nVerification code expired. Please send your email again.")
		return
	}
	if !strings.EqualFold(pb.Code, code) {
		// Put it back so user can retry (within expiry).
		n.pendMu.Lock()
		n.pending[openID] = pb
		n.pendMu.Unlock()
		replyText(n, openID, "验证码不正确，请重新输入。\nIncorrect code, please try again.")
		return
	}

	n.BindOpenID(pb.Email, openID)
	replyText(n, openID, "✅ 绑定成功！您将通过飞书接收 Hub 会话通知。\n✅ Binding succeeded! You will receive Hub session notifications via Feishu.")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func extractText(content string) string {
	var msg struct {
		Text string `json:"text"`
	}
	if json.Unmarshal([]byte(content), &msg) != nil {
		return ""
	}
	return strings.TrimSpace(msg.Text)
}

func looksLikeEmail(s string) bool {
	return strings.Contains(s, "@") && strings.Contains(s, ".") && !strings.Contains(s, " ")
}

func isVerifyCode(s string) bool {
	runes := []rune(s)
	if len(runes) != 6 {
		return false
	}
	for _, c := range runes {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func generateCode() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		// Fallback — should never happen.
		return "123456"
	}
	return fmt.Sprintf("%06d", n.Int64())
}

func replyText(n *Notifier, openID, text string) {
	if n == nil || n.bot == nil {
		return
	}
	// Use "post" (rich text) message type so Feishu renders line breaks
	// properly. Each \n becomes a separate content row.
	lines := strings.Split(text, "\n")
	var rows [][]lark.PostElem
	for _, line := range lines {
		t := line
		rows = append(rows, []lark.PostElem{
			{Tag: "text", Text: &t},
		})
	}
	pc := lark.PostContent{
		"zh_cn": {Content: rows},
	}
	msg := lark.NewMsgBuffer(lark.MsgPost).
		BindOpenID(openID).
		Post(&pc).
		Build()
	ctx := context.Background()
	if _, err := n.bot.PostMessage(ctx, msg); err != nil {
		log.Printf("[feishu/webhook] reply failed (open_id=%s): %v", openID, err)
	}
}

// buildPost is no longer needed — post content is built inline in replyText.

// buildSimpleCard wraps text in a minimal interactive card with a single
// markdown element. Feishu renders markdown inside cards (bold, code, etc.).
func buildSimpleCard(text string) string {
	card := map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"elements": []any{
			map[string]any{
				"tag":     "markdown",
				"content": text,
			},
		},
	}
	data, _ := json.Marshal(card)
	return string(data)
}
