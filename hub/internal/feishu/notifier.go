package feishu

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg" // register JPEG decoder for image.Decode
	_ "image/png"  // register PNG decoder for image.Decode
	"log"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

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
	LLMConfigured  bool
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

	// dedup: track last pushed state per session to avoid re-pushing the same
	// status after hub restart. Persisted to DB under "feishu_last_push" key.
	dedupMu    sync.Mutex
	lastPushed map[string]string // session_id → hash of last pushed content

	// Short aliases for session IDs per user (open_id → {alias→sessionID, sessionID→alias}).
	aliasMu   sync.RWMutex
	aliasToID map[string]map[string]string // open_id → (alias → session_id)
	idToAlias map[string]map[string]string // open_id → (session_id → alias)
	aliasSeq  map[string]int              // open_id → next alias number

	// plugin is the optional FeishuPlugin reference. When set, handleBotMessage
	// will attempt to route messages through the IM Adapter pipeline first.
	plugin *FeishuPlugin

	// broadcaster sends verification codes to all reachable channels (cross-IM).
	broadcaster NotifyBroadcaster
}

// NotifyBroadcaster sends verification codes to all reachable channels.
type NotifyBroadcaster interface {
	BroadcastVerifyCode(ctx context.Context, email, code, excludePlatform string) (sentTo string, err error)
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
		lastPushed:      make(map[string]string),
		aliasToID:       make(map[string]map[string]string),
		idToAlias:       make(map[string]map[string]string),
		aliasSeq:        make(map[string]int),
	}
	if appID != "" && appSecret != "" {
		bot := lark.NewChatBot(appID, appSecret)
		bot.SetAutoRenew(true)
		n.bot = bot
		log.Printf("[feishu] notifier initialized (app_id=%s)", appID)
	}
	n.loadOpenIDMap()
	n.loadLastPushed()
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

// SetPlugin wires the FeishuPlugin so that handleBotMessage can route
// messages through the IM Adapter pipeline when available.
func (n *Notifier) SetPlugin(p *FeishuPlugin) {
	n.plugin = p
}

// SetBroadcaster wires the cross-IM notification broadcaster.
// Called from bootstrap after the IM Adapter is fully assembled.
func (n *Notifier) SetBroadcaster(b NotifyBroadcaster) {
	n.broadcaster = b
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

// ---------------------------------------------------------------------------
// Event dedup — avoid re-pushing the same status after hub restart
// ---------------------------------------------------------------------------

const lastPushedKey = "feishu_last_push"

func (n *Notifier) loadLastPushed() {
	if n.system == nil {
		return
	}
	raw, err := n.system.Get(context.Background(), lastPushedKey)
	if err != nil || raw == "" {
		return
	}
	var m map[string]string
	if json.Unmarshal([]byte(raw), &m) == nil {
		n.dedupMu.Lock()
		n.lastPushed = m
		n.dedupMu.Unlock()
	}
}

func (n *Notifier) saveLastPushed() {
	if n.system == nil {
		return
	}
	n.dedupMu.Lock()
	// Evict old entries to prevent unbounded growth (keep last 200).
	if len(n.lastPushed) > 200 {
		n.lastPushed = make(map[string]string)
	}
	data, _ := json.Marshal(n.lastPushed)
	n.dedupMu.Unlock()
	if err := n.system.Set(context.Background(), lastPushedKey, string(data)); err != nil {
		log.Printf("[feishu] failed to save last_push map: %v", err)
	}
}

// isDuplicate returns true if the given session+key combination was already
// pushed with the same content hash. If not a duplicate, it records the hash.
func (n *Notifier) isDuplicate(sessionID, eventKey, contentHash string) bool {
	key := sessionID + ":" + eventKey
	n.dedupMu.Lock()
	defer n.dedupMu.Unlock()
	if n.lastPushed[key] == contentHash {
		return true
	}
	n.lastPushed[key] = contentHash
	return false
}

// clearDedup removes dedup entries for a session (called on session close).
func (n *Notifier) clearDedup(sessionID string) {
	n.dedupMu.Lock()
	for k := range n.lastPushed {
		if strings.HasPrefix(k, sessionID+":") {
			delete(n.lastPushed, k)
		}
	}
	n.dedupMu.Unlock()
}

// resolveOpenID returns the open_id for an email, if bound.
func (n *Notifier) resolveOpenID(email string) string {
	n.oidMu.RLock()
	defer n.oidMu.RUnlock()
	return n.oidCache[email]
}

// ---------------------------------------------------------------------------
// Session alias helpers — short numeric IDs for Feishu display
// ---------------------------------------------------------------------------

// getOrCreateAlias returns a short alias (e.g. "1", "2") for a session ID,
// scoped to a specific open_id user. Creates one if it doesn't exist.
func (n *Notifier) getOrCreateAlias(openID, sessionID string) string {
	n.aliasMu.Lock()
	defer n.aliasMu.Unlock()
	if m, ok := n.idToAlias[openID]; ok {
		if alias, ok := m[sessionID]; ok {
			return alias
		}
	}
	// Create new alias.
	if n.aliasToID[openID] == nil {
		n.aliasToID[openID] = make(map[string]string)
		n.idToAlias[openID] = make(map[string]string)
	}
	n.aliasSeq[openID]++
	alias := fmt.Sprintf("%d", n.aliasSeq[openID])
	n.aliasToID[openID][alias] = sessionID
	n.idToAlias[openID][sessionID] = alias
	return alias
}

// resolveAlias resolves user input to a real session ID. It checks:
// 1. If input is a short alias number for this user
// 2. Otherwise falls through to suffix/prefix matching in findSession
func (n *Notifier) resolveAlias(openID, input string) string {
	n.aliasMu.RLock()
	defer n.aliasMu.RUnlock()
	if m, ok := n.aliasToID[openID]; ok {
		if sid, ok := m[input]; ok {
			return sid
		}
	}
	return input // not an alias, return as-is for suffix matching
}

// getAlias returns the alias for a session ID if one exists, otherwise returns shortID.
func (n *Notifier) getAlias(openID, sessionID string) string {
	n.aliasMu.RLock()
	defer n.aliasMu.RUnlock()
	if m, ok := n.idToAlias[openID]; ok {
		if alias, ok := m[sessionID]; ok {
			return "#" + alias
		}
	}
	return shortID(sessionID)
}

// buildAliasesForUser pre-assigns aliases for all sessions visible to a user.
// Called before listing sessions so that aliases are consistent.
func (n *Notifier) buildAliasesForUser(openID string) {
	if n.sessions == nil || n.devices == nil {
		return
	}
	userID := n.resolveUserID(openID)
	if userID == "" {
		return
	}
	machines, err := n.devices.ListMachines(context.Background(), userID)
	if err != nil {
		return
	}
	for _, m := range machines {
		sessions, err := n.sessions.ListByMachine(context.Background(), userID, m.MachineID)
		if err != nil {
			continue
		}
		for _, s := range sessions {
			n.getOrCreateAlias(openID, s.SessionID)
		}
	}
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
	case "session.image":
		n.onSessionImage(event)
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

	// Dedup: skip if we already pushed the same status+task for this session.
	hash := fmt.Sprintf("%s|%s|%v|%s", s.Status, s.CurrentTask, s.WaitingForUser, s.SuggestedAction)
	if n.isDuplicate(event.SessionID, "summary", hash) {
		return
	}
	go n.saveLastPushed()

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

	// Dedup: skip if we already pushed the same event for this session.
	hash := fmt.Sprintf("%s|%s|%s|%s", ie.Type, ie.Severity, ie.Title, ie.Summary)
	if n.isDuplicate(event.SessionID, "important", hash) {
		return
	}
	go n.saveLastPushed()
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

	// Clean lines: strip ANSI escape sequences, split on embedded \r\n or \r.
	// Preserve blank lines so that paragraph breaks in the original output
	// are kept intact when rendered in Feishu.
	var cleaned []string
	for _, line := range lines {
		c := stripAnsi(line)
		// A single "line" from the preview delta may contain embedded \r\n
		// or \r (terminal carriage returns). Split them into real lines.
		subLines := strings.Split(c, "\n")
		for _, sl := range subLines {
			sl = strings.TrimRight(sl, " \t")
			cleaned = append(cleaned, sl)
		}
	}
	// Trim leading/trailing blank lines but keep internal ones.
	for len(cleaned) > 0 && cleaned[0] == "" {
		cleaned = cleaned[1:]
	}
	for len(cleaned) > 0 && cleaned[len(cleaned)-1] == "" {
		cleaned = cleaned[:len(cleaned)-1]
	}
	if len(cleaned) == 0 {
		return
	}

	alias := n.getAlias(watcherOpenID, sid)
	var sb strings.Builder
	sb.WriteString("📺 ")
	sb.WriteString(alias)
	sb.WriteString("\n")
	for _, line := range cleaned {
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	text := sb.String()
	// Truncate at a rune-safe line boundary to avoid cutting multi-byte
	// characters (Chinese, emoji, etc.) in the middle.
	const maxBytes = 4000
	if len(text) > maxBytes {
		text = truncateAtLine(text, maxBytes)
	}
	replyText(n, watcherOpenID, text)
}

// stripAnsi removes ANSI escape sequences from terminal output and normalises
// line endings. \r\n → \n, bare \r → \n, other control chars (except \n, \t)
// are dropped. The result is NOT trimmed so that embedded newlines survive.
func stripAnsi(s string) string {
	// Normalise line endings first so the byte-level loop only sees \n.
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

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
		// Skip non-printable control chars except newline and tab.
		if s[i] < 0x20 && s[i] != '\n' && s[i] != '\t' {
			i++
			continue
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
}

// onSessionImage uploads a base64-encoded image to Feishu and sends it to
// the user watching the session. If no watcher is active, the image is sent
// to the session owner. Runs the heavy I/O (decode + upload) in a goroutine
// to avoid blocking the event dispatch loop.
func (n *Notifier) onSessionImage(event session.Event) {
	if event.ImageData == nil || event.ImageData.Data == "" {
		return
	}

	// Determine recipient: prefer the active watcher, fall back to session owner.
	watcherOpenID := n.findWatcher(event.SessionID)
	var targetOpenID string
	if watcherOpenID != "" {
		targetOpenID = watcherOpenID
	} else {
		email := n.resolveEmail(event.UserID)
		if email != "" {
			targetOpenID = n.resolveOpenID(email)
		}
	}
	if targetOpenID == "" {
		return
	}

	// Capture values for the goroutine.
	imgData := event.ImageData
	sessionID := event.SessionID
	openID := targetOpenID

	go func() {
		// Decode base64 image data.
		imgBytes, err := base64.StdEncoding.DecodeString(imgData.Data)
		if err != nil {
			log.Printf("[feishu] image decode failed (session=%s): %v", sessionID, err)
			return
		}

		// Decode into image.Image using the standard library's format
		// registry (png and jpeg are imported for side-effect registration).
		img, format, err := image.Decode(bytes.NewReader(imgBytes))
		if err != nil {
			log.Printf("[feishu] image parse failed (session=%s, media_type=%s): %v", sessionID, imgData.MediaType, err)
			return
		}
		_ = format

		// Upload to Feishu.
		resp, err := n.bot.UploadImageObject(context.Background(), img)
		if err != nil {
			log.Printf("[feishu] image upload failed (session=%s): %v", sessionID, err)
			return
		}
		imageKey := resp.Data.ImageKey
		if imageKey == "" {
			log.Printf("[feishu] image upload returned empty key (session=%s)", sessionID)
			return
		}

		// Send the image as a post message with a caption.
		alias := n.getAlias(openID, sessionID)
		caption := fmt.Sprintf("📷 %s", alias)
		sendImagePost(n, openID, imageKey, caption)
	}()
}

// sendImagePost sends an image as a Feishu post (rich text) message with a
// text caption line followed by the image.
func sendImagePost(n *Notifier, openID, imageKey, caption string) {
	if n == nil || n.bot == nil {
		return
	}
	captionText := caption
	rows := [][]lark.PostElem{
		{{Tag: "text", Text: &captionText}},
		{{Tag: "img", ImageKey: &imageKey}},
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
		log.Printf("[feishu/image] send failed (open_id=%s): %v", openID, err)
	}
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
		replyText(n, oid, fmt.Sprintf("⏹ 会话 %s 已结束，已自动退出会话上下文。", n.getAlias(oid, event.SessionID)))
	}

	cardJSON := buildCardJSON("✅ 会话已结束", "grey", []cardField{
		{"状态", statusEmoji(status) + " " + status},
		{"Session", shortID(event.SessionID)},
	})
	n.sendToUser(event.UserID, cardJSON)

	// Clean up dedup entries for this session.
	n.clearDedup(event.SessionID)
	go n.saveLastPushed()
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

// truncateAtLine truncates text to fit within maxBytes by cutting at the last
// complete line boundary. This avoids splitting multi-byte UTF-8 characters
// (Chinese, emoji, etc.) and ensures no partial lines are sent.
func truncateAtLine(text string, maxBytes int) string {
	if len(text) <= maxBytes {
		return text
	}
	// Find the last newline within the byte budget.
	cut := strings.LastIndex(text[:maxBytes], "\n")
	if cut <= 0 {
		// No newline found — fall back to rune-safe truncation.
		byteLen := 0
		for i, r := range text {
			byteLen += utf8.RuneLen(r)
			if byteLen > maxBytes {
				return text[:i] + "\n…"
			}
		}
		return text
	}
	return text[:cut] + "\n…"
}

func shortID(id string) string {
	// For IDs like "sess_177", show just the numeric suffix "177".
	if idx := strings.LastIndex(id, "_"); idx >= 0 && idx < len(id)-1 {
		return id[idx+1:]
	}
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
