package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/session"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/go-lark/lark/v2"
)

const openIDMapKey = "feishu_openid_map"

// Mailer is the subset of mail.Service used by the Feishu notifier.
type Mailer interface {
	Send(ctx context.Context, to []string, subject string, body string) error
}

// DeviceLister lists machines for a user.
type DeviceLister interface {
	ListMachines(ctx context.Context, userID string) ([]MachineInfo, error)
	IsMachineOnline(machineID string) bool
	SendToMachine(machineID string, msg any) error
}

// MachineInfo is a minimal view of a machine (mirrors device.MachineRuntimeInfo fields we need).
type MachineInfo struct {
	MachineID      string
	Name           string
	Platform       string
	Hostname       string
	Online         bool
	ActiveSessions int
}

// SessionLister lists and queries sessions.
type SessionLister interface {
	ListByMachine(ctx context.Context, userID, machineID string) ([]*session.SessionCacheEntry, error)
	GetSnapshot(userID, machineID, sessionID string) (*session.SessionCacheEntry, bool)
}

// pendingBind holds a verification code for an in-progress open_id binding.
type pendingBind struct {
	Email  string
	Code   string
	Expiry time.Time
}

// Notifier listens to session events and pushes interactive cards to
// the corresponding Feishu user (matched by email or open_id).
type Notifier struct {
	bot    *lark.Bot
	users  store.UserRepository
	system store.SystemSettingsRepository
	mailer Mailer

	// Services for interactive commands (list machines, sessions, send input, etc.).
	devices  DeviceLister
	sessions SessionLister

	// cache userID → email to avoid repeated DB lookups.
	mu      sync.RWMutex
	idCache map[string]string

	// cache email → open_id for personal Feishu users.
	oidMu    sync.RWMutex
	oidCache map[string]string // email → open_id

	// active session context per user (open_id → session_id).
	activeMu      sync.RWMutex
	activeSession map[string]string // open_id → session_id

	// throttle preview delta pushes per session to avoid flooding.
	previewMu       sync.Mutex
	previewLastPush map[string]time.Time   // session_id → last push time
	previewBuf      map[string][]string    // session_id → buffered lines
	previewTimers   map[string]*time.Timer // session_id → idle flush timer

	// pending email verification for open_id binding (open_id → pendingBind).
	pendMu  sync.Mutex
	pending map[string]*pendingBind
}

// New creates a Notifier. The notifier is always returned (never nil) so that
// handlers and listeners can hold a stable pointer. If appID/appSecret are
// empty the bot field stays nil and messages are silently dropped until
// Reconfigure is called with valid credentials.
func New(appID, appSecret string, users store.UserRepository, system store.SystemSettingsRepository, mailer Mailer) *Notifier {
	n := &Notifier{
		users:           users,
		system:          system,
		mailer:          mailer,
		idCache:         make(map[string]string),
		oidCache:        make(map[string]string),
		activeSession:   make(map[string]string),
		previewLastPush: make(map[string]time.Time),
		previewBuf:      make(map[string][]string),
		previewTimers:   make(map[string]*time.Timer),
		pending:         make(map[string]*pendingBind),
	}
	if appID != "" && appSecret != "" {
		bot := lark.NewChatBot(appID, appSecret)
		bot.SetAutoRenew(true)
		n.bot = bot
		log.Printf("[feishu] notifier initialized (app_id=%s)", appID)
	}
	n.loadOpenIDMap()
	return n
}

// Reconfigure replaces the underlying lark.Bot at runtime (called when the
// admin saves new Feishu credentials via the UI). Pass empty strings to
// disable the bot without destroying the Notifier.
func (n *Notifier) Reconfigure(appID, appSecret string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if appID == "" || appSecret == "" {
		n.bot = nil
		log.Printf("[feishu] notifier disabled (empty credentials)")
		return
	}
	bot := lark.NewChatBot(appID, appSecret)
	bot.SetAutoRenew(true)
	n.bot = bot
	log.Printf("[feishu] notifier reconfigured (app_id=%s)", appID)
}

// SetServices wires the device and session services for interactive commands.
// Called from bootstrap after all services are created.
func (n *Notifier) SetServices(devices DeviceLister, sessions SessionLister) {
	n.devices = devices
	n.sessions = sessions
}

// Bot returns the underlying lark.Bot for use by the webhook handler.
func (n *Notifier) Bot() *lark.Bot {
	if n == nil {
		return nil
	}
	return n.bot
}

// BindOpenID stores an email → open_id mapping for personal Feishu users.
func (n *Notifier) BindOpenID(email, openID string) {
	if email == "" || openID == "" {
		return
	}
	n.oidMu.Lock()
	n.oidCache[email] = openID
	n.oidMu.Unlock()
	n.saveOpenIDMap()
	log.Printf("[feishu] bound open_id for email=%s", email)
}

// GetOpenIDMap returns a copy of the current email→open_id bindings.
func (n *Notifier) GetOpenIDMap() map[string]string {
	n.oidMu.RLock()
	defer n.oidMu.RUnlock()
	m := make(map[string]string, len(n.oidCache))
	for k, v := range n.oidCache {
		m[k] = v
	}
	return m
}

// RemoveOpenID removes an email → open_id binding.
func (n *Notifier) RemoveOpenID(email string) {
	n.oidMu.Lock()
	delete(n.oidCache, email)
	n.oidMu.Unlock()
	n.saveOpenIDMap()
}

func (n *Notifier) loadOpenIDMap() {
	if n.system == nil {
		return
	}
	raw, err := n.system.Get(context.Background(), openIDMapKey)
	if err != nil || raw == "" {
		return
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return
	}
	n.oidMu.Lock()
	n.oidCache = m
	n.oidMu.Unlock()
	log.Printf("[feishu] loaded %d open_id binding(s)", len(m))
}

func (n *Notifier) saveOpenIDMap() {
	if n.system == nil {
		return
	}
	n.oidMu.RLock()
	data, _ := json.Marshal(n.oidCache)
	n.oidMu.RUnlock()
	if err := n.system.Set(context.Background(), openIDMapKey, string(data)); err != nil {
		log.Printf("[feishu] failed to save open_id map: %v", err)
	}
}

// resolveOpenID returns the open_id for an email, if bound.
func (n *Notifier) resolveOpenID(email string) string {
	n.oidMu.RLock()
	defer n.oidMu.RUnlock()
	return n.oidCache[email]
}

// HandleEvent is a session.Listener that forwards key events to Feishu.
// Only high-value events are pushed to avoid notification fatigue:
//   - session.summary: only when waiting for user or error/failed
//   - session.important_event: only error/warning severity
//   - session.closed: always (user needs to know the result)
//   - session.created: not pushed (too noisy)
//
// When a user has an active session context (/use), summary and important_event
// notifications for that session are suppressed — the user is already watching
// the live preview_delta stream and doesn't need card noise.
func (n *Notifier) HandleEvent(event session.Event) {
	if n == nil || n.bot == nil {
		return
	}
	switch event.Type {
	case "session.summary":
		if !n.isSessionWatched(event.SessionID) {
			n.onSessionSummary(event)
		}
	case "session.important_event":
		if !n.isSessionWatched(event.SessionID) {
			n.onImportantEvent(event)
		}
	case "session.closed":
		n.onSessionClosed(event)
	case "session.preview_delta":
		n.onPreviewDelta(event)
	}
}

// isSessionWatched returns true if any user is currently /use-ing this session,
// meaning they are watching the live terminal output and don't need card notifications.
func (n *Notifier) isSessionWatched(sessionID string) bool {
	n.activeMu.RLock()
	defer n.activeMu.RUnlock()
	for _, activeSID := range n.activeSession {
		if strings.EqualFold(sessionID, activeSID) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Event handlers
// ---------------------------------------------------------------------------

func (n *Notifier) onSessionCreated(event session.Event) {
	tool, title := "", ""
	if event.Payload != nil {
		tool, _ = event.Payload["tool"].(string)
		title, _ = event.Payload["title"].(string)
	}
	if title == "" {
		title = "New Session"
	}
	cardJSON := buildCardJSON("🚀 会话已创建", "green", []cardField{
		{"工具", tool},
		{"会话", truncate(title, 60)},
		{"Session ID", shortID(event.SessionID)},
	})
	n.sendToUser(event.UserID, cardJSON)
}

func (n *Notifier) onSessionSummary(event session.Event) {
	if event.Summary == nil {
		return
	}
	s := event.Summary
	if s.Status == "" {
		return
	}
	// Only push when the user needs to act or something went wrong.
	isError := strings.EqualFold(s.Status, "error") || strings.EqualFold(s.Status, "failed")
	if !s.WaitingForUser && !isError {
		return
	}

	fields := []cardField{
		{"工具", s.Tool},
		{"状态", statusEmoji(s.Status) + " " + s.Status},
	}
	if s.Title != "" {
		fields = append(fields, cardField{"标题", truncate(s.Title, 60)})
	}
	if s.CurrentTask != "" {
		fields = append(fields, cardField{"当前任务", truncate(s.CurrentTask, 80)})
	}
	if s.ProgressSummary != "" {
		fields = append(fields, cardField{"进度", truncate(s.ProgressSummary, 80)})
	}
	if s.WaitingForUser {
		fields = append(fields, cardField{"⚠️ 等待用户操作", defaultStr(s.SuggestedAction, "请查看终端")})
	}
	cardJSON := buildCardJSON("📊 会话状态更新", statusColor(s.Status), fields)
	n.sendToUser(event.UserID, cardJSON)
}

func (n *Notifier) onImportantEvent(event session.Event) {
	if event.Important == nil {
		return
	}
	ie := event.Important
	// Only push error and warning severity events.
	sev := strings.ToLower(ie.Severity)
	if sev != "error" && sev != "critical" && sev != "warning" {
		return
	}
	fields := []cardField{
		{"类型", ie.Type},
		{"严重性", ie.Severity},
		{"标题", truncate(ie.Title, 60)},
	}
	if ie.Summary != "" {
		fields = append(fields, cardField{"摘要", truncate(ie.Summary, 120)})
	}
	if ie.RelatedFile != "" {
		fields = append(fields, cardField{"文件", ie.RelatedFile})
	}
	if ie.Command != "" {
		fields = append(fields, cardField{"命令", truncate(ie.Command, 80)})
	}
	cardJSON := buildCardJSON("🔔 重要事件", severityColor(ie.Severity), fields)
	n.sendToUser(event.UserID, cardJSON)
}

// previewPushInterval is the minimum interval between preview pushes for the
// same session to avoid flooding the Feishu chat.
const previewPushInterval = 8 * time.Second

// previewIdleFlush is the idle timeout: if no new lines arrive within this
// duration, the buffer is flushed as a "complete paragraph".
const previewIdleFlush = 3 * time.Second

// maxPreviewLines caps the number of buffered lines sent in one message.
const maxPreviewLines = 60

func (n *Notifier) onPreviewDelta(event session.Event) {
	if event.PreviewDelta == nil || len(event.PreviewDelta.AppendLines) == 0 {
		return
	}
	sid := event.SessionID

	// Find which open_id is watching this session.
	watcherOpenID := n.findWatcher(sid)
	if watcherOpenID == "" {
		return
	}

	// Buffer lines.
	n.previewMu.Lock()
	n.previewBuf[sid] = append(n.previewBuf[sid], event.PreviewDelta.AppendLines...)
	if len(n.previewBuf[sid]) > maxPreviewLines {
		n.previewBuf[sid] = n.previewBuf[sid][len(n.previewBuf[sid])-maxPreviewLines:]
	}

	// Reset or start the idle flush timer for this session.
	if t, ok := n.previewTimers[sid]; ok {
		t.Stop()
	}
	n.previewTimers[sid] = time.AfterFunc(previewIdleFlush, func() {
		n.flushPreview(sid)
	})

	// Also check the hard interval cap — if enough time has passed, flush now.
	lastPush := n.previewLastPush[sid]
	now := time.Now()
	if !lastPush.IsZero() && now.Sub(lastPush) < previewPushInterval {
		n.previewMu.Unlock()
		return
	}
	n.previewMu.Unlock()

	// Enough time has passed — flush immediately.
	n.flushPreview(sid)
}

// findWatcher returns the open_id of the user watching the given session, or "".
func (n *Notifier) findWatcher(sessionID string) string {
	n.activeMu.RLock()
	defer n.activeMu.RUnlock()
	for oid, activeSID := range n.activeSession {
		if strings.EqualFold(sessionID, activeSID) {
			return oid
		}
	}
	return ""
}

// flushPreview sends all buffered preview lines for a session to the watcher.
func (n *Notifier) flushPreview(sid string) {
	watcherOpenID := n.findWatcher(sid)
	if watcherOpenID == "" {
		return
	}

	n.previewMu.Lock()
	lines := n.previewBuf[sid]
	n.previewBuf[sid] = nil
	n.previewLastPush[sid] = time.Now()
	if t, ok := n.previewTimers[sid]; ok {
		t.Stop()
		delete(n.previewTimers, sid)
	}
	n.previewMu.Unlock()

	if len(lines) == 0 {
		return
	}

	var sb strings.Builder
	sb.WriteString("📺 ")
	sb.WriteString(shortID(sid))
	sb.WriteString(":\n")
	for _, line := range lines {
		cleaned := stripAnsi(line)
		if cleaned == "" {
			continue
		}
		sb.WriteString(truncate(cleaned, 120))
		sb.WriteString("\n")
	}
	text := sb.String()
	if len(text) > 4000 {
		text = text[:4000] + "\n..."
	}
	replyText(n, watcherOpenID, text)
}

// stripAnsi removes ANSI escape sequences from terminal output.
func stripAnsi(s string) string {
	var out strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) {
			// CSI sequence: ESC [ ... final_byte
			if s[i+1] == '[' {
				j := i + 2
				for j < len(s) && ((s[j] >= '0' && s[j] <= '9') || s[j] == ';' || s[j] == '?') {
					j++
				}
				if j < len(s) {
					j++ // skip final byte
				}
				i = j
				continue
			}
			// OSC or other: skip until BEL or ST
			j := i + 2
			for j < len(s) && s[j] != 0x07 && s[j] != 0x1b {
				j++
			}
			if j < len(s) {
				j++
			}
			i = j
			continue
		}
		// Skip non-printable control chars except newline/tab.
		if s[i] < 0x20 && s[i] != '\n' && s[i] != '\t' {
			i++
			continue
		}
		out.WriteByte(s[i])
		i++
	}
	return strings.TrimSpace(out.String())
}

func (n *Notifier) onSessionClosed(event session.Event) {
	// Collect watchers before clearing so we can notify them explicitly.
	n.activeMu.Lock()
	var watcherOIDs []string
	for oid, activeSID := range n.activeSession {
		if strings.EqualFold(event.SessionID, activeSID) {
			watcherOIDs = append(watcherOIDs, oid)
			delete(n.activeSession, oid)
		}
	}
	n.activeMu.Unlock()

	// Clean up preview buffers and timers.
	n.previewMu.Lock()
	delete(n.previewBuf, event.SessionID)
	delete(n.previewLastPush, event.SessionID)
	if t, ok := n.previewTimers[event.SessionID]; ok {
		t.Stop()
		delete(n.previewTimers, event.SessionID)
	}
	n.previewMu.Unlock()

	status := ""
	if event.Payload != nil {
		status, _ = event.Payload["status"].(string)
	}

	// Notify watchers that their active session context was cleared.
	for _, oid := range watcherOIDs {
		replyText(n, oid, fmt.Sprintf("⏹ 会话 %s 已结束，已自动退出会话上下文。", shortID(event.SessionID)))
	}

	cardJSON := buildCardJSON("✅ 会话已结束", "grey", []cardField{
		{"状态", statusEmoji(status) + " " + status},
		{"Session ID", shortID(event.SessionID)},
	})
	n.sendToUser(event.UserID, cardJSON)
}

// ---------------------------------------------------------------------------
// Card JSON builder (Feishu interactive message card format)
// ---------------------------------------------------------------------------

type cardField struct {
	label string
	value string
}

// buildCardJSON produces the JSON string for a Feishu interactive card.
// The card format follows the Feishu Open Platform message card spec.
func buildCardJSON(title, color string, fields []cardField) string {
	var md strings.Builder
	for _, f := range fields {
		if f.value == "" {
			continue
		}
		fmt.Fprintf(&md, "**%s**: %s\n", f.label, f.value)
	}

	card := map[string]any{
		"config": map[string]any{
			"wide_screen_mode": true,
		},
		"header": map[string]any{
			"title": map[string]any{
				"tag":     "plain_text",
				"content": title,
			},
			"template": color,
		},
		"elements": []any{
			map[string]any{
				"tag":     "markdown",
				"content": md.String(),
			},
		},
	}

	data, _ := json.Marshal(card)
	return string(data)
}

// ---------------------------------------------------------------------------
// User resolution & message sending
// ---------------------------------------------------------------------------

func (n *Notifier) sendToUser(userID string, cardJSON string) {
	email := n.resolveEmail(userID)
	if email == "" {
		log.Printf("[feishu] cannot resolve email for user_id=%s, skipping", userID)
		return
	}

	// Prefer open_id (works for both personal and enterprise Feishu).
	// Fall back to BindEmail (enterprise Feishu only).
	openID := n.resolveOpenID(email)

	var msg lark.OutcomingMessage
	if openID != "" {
		msg = lark.NewMsgBuffer(lark.MsgInteractive).
			BindOpenID(openID).
			Card(cardJSON).
			Build()
	} else {
		msg = lark.NewMsgBuffer(lark.MsgInteractive).
			BindEmail(email).
			Card(cardJSON).
			Build()
	}

	ctx := context.Background()
	resp, err := n.bot.PostMessage(ctx, msg)
	if err != nil {
		log.Printf("[feishu] send failed (email=%s, open_id=%s): %v", email, openID, err)
		return
	}
	if resp != nil && resp.Code != 0 {
		log.Printf("[feishu] API error (email=%s, open_id=%s): code=%d msg=%s", email, openID, resp.Code, resp.Msg)
	}
}

func (n *Notifier) resolveEmail(userID string) string {
	if userID == "" {
		return ""
	}

	n.mu.RLock()
	cached, ok := n.idCache[userID]
	n.mu.RUnlock()
	if ok {
		return cached
	}

	user, err := n.users.GetByID(context.Background(), userID)
	if err != nil || user == nil {
		log.Printf("[feishu] user lookup failed for user_id=%s: %v", userID, err)
		return ""
	}

	n.mu.Lock()
	// Evict all entries if cache grows too large.
	if len(n.idCache) > 500 {
		n.idCache = make(map[string]string)
	}
	n.idCache[userID] = user.Email
	n.mu.Unlock()
	return user.Email
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func defaultStr(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}

func statusEmoji(status string) string {
	switch strings.ToLower(status) {
	case "running", "busy":
		return "🔄"
	case "stopped", "finished", "completed", "done":
		return "✅"
	case "failed", "error":
		return "❌"
	case "waiting", "waiting_for_user":
		return "⏳"
	case "exited", "killed", "terminated":
		return "🛑"
	default:
		return "ℹ️"
	}
}

func statusColor(status string) string {
	switch strings.ToLower(status) {
	case "running", "busy":
		return "blue"
	case "stopped", "finished", "completed", "done":
		return "green"
	case "failed", "error":
		return "red"
	case "waiting", "waiting_for_user":
		return "orange"
	default:
		return "grey"
	}
}

func severityColor(severity string) string {
	switch strings.ToLower(severity) {
	case "error", "critical":
		return "red"
	case "warning":
		return "orange"
	default:
		return "blue"
	}
}
