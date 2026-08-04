package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/i18n"
	"github.com/RapidAI/CodeClaw/corelib/improactive"
	"github.com/RapidAI/CodeClaw/corelib/lansenger"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	"github.com/RapidAI/CodeClaw/corelib/textutil"
)

// lansengerGatewayManager manages the client-side Lansenger gateway.
// Supports two modes:
//   - Local / 单机 mode (default): routes messages directly to the
//     local MaClaw LLM agent loop, bypassing Hub entirely.
//   - Hub / 多机 mode (LansengerLocalMode=false): forwards messages to Hub
//     via im.gateway_message, receives replies via im.gateway_reply.
type lansengerGatewayManager struct {
	app *App
	mu  sync.Mutex
	// syncMu serializes config reconciliation with Stop. Gateway Stop may wait
	// for its connection goroutine, so keep it outside mu while still ensuring a
	// concurrent sync cannot publish or stop the same gateway out of order.
	syncMu       sync.Mutex
	gateway      *lansenger.Gateway
	status       gatewayConnectionStatus
	statusSince  time.Time
	lastRestart  time.Time
	lastToken    string
	healthCancel context.CancelFunc
	hubClaimSent bool // true after first successful claim in hub mode
	// replyRoutes retains the chat kind for conversations forwarded to Hub.
	// Hub replies identify only platform_uid, so without this local routing hint
	// a group reply would be incorrectly sent through the private-message API.
	replyRoutes map[string]lansengerReplyRoute

	localHandler *IMMessageHandler

	// surveyRate throttles survey IM attempts (~2 msg/s per user; design §9).
	surveyRate *surveyUserRateLimit
	// surveyHints remembers users mid-survey so free-text answers can hit Hub
	// without probing every short chat message.
	surveyHints *surveySessionHint

	// groupInfoCache avoids hammering GetGroupInfo on every group message.
	groupInfoCache map[string]lansengerGroupInfoCacheEntry

	// lastPrivateUserID is the most recent p2p peer (owner talking to the bot).
	// Used for 盯人 self-forward onto the Lansenger private channel.
	lastPrivateUserID string

	// groupSummary buffers group chat and handles /summary (群讨论摘要).
	groupSummary *lansengerGroupSummaryService
	// groupSummaryAtomic is the lock-free hot-path pointer after first init.
	groupSummaryAtomic atomic.Pointer[lansengerGroupSummaryService]
	// groupFileLimits is an immutable snapshot used by the inbound attachment
	// callback. Downloads must not parse config.json on the WebSocket hot path.
	groupFileLimits atomic.Pointer[lansengerGroupFileLimits]
}

type lansengerGroupFileLimits map[string]int64

func (m *lansengerGatewayManager) currentLocalHandler() *IMMessageHandler {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.localHandler
}

type lansengerGroupInfoCacheEntry struct {
	info *lansenger.GroupInfo
	err  bool // true when last fetch failed (negative cache)
	at   time.Time
}

const (
	lansengerGroupInfoCacheTTL     = 5 * time.Minute
	lansengerGroupInfoCacheNegTTL  = 30 * time.Second
	lansengerGroupInfoFetchTimeout = 2 * time.Second
)

// lansengerReplyDecor is one inbound message's decoration slot for hub replies
// (@mention / text quote / refMsgId). Multi-chunk replies consume the slot once.
type lansengerReplyDecor struct {
	senderID      string    // Lansenger staffId of the asker (for @mention)
	senderName    string    // display name for "xx问：" text quotes
	question      string    // cleaned user question (no leading @Bot)
	messageID     string    // platform message id for native refMsgId (empty if unknown)
	correlationID string    // hub pairing key (platform id or synthetic mc-…)
	at            time.Time // when the inbound was remembered (TTL)
}

type lansengerReplyRoute struct {
	isGroup bool
	// pending is a FIFO of undecorated inbound slots. Concurrent messages in the
	// same group each push a slot so replies decorate the matching question
	// instead of overwriting a single shared record.
	pending []lansengerReplyDecor
	// last* retains the most recent metadata for isGroup / quote peeks after
	// pending has been fully consumed by multi-chunk first-take.
	lastSender     string
	lastSenderName string
	lastQuestion   string
	lastMessageID  string
	seenAt         time.Time
}

const maxLansengerReplyRoutes = 1024

// Cap how many concurrent undecorated inbound messages we remember per target
// (same group or same DM peer) to bound memory under reply storms.
const maxLansengerPendingDecors = 32

// Drop pending slots that never got a matching hub agent reply (reject, timeout,
// abandoned). 15m covers long agent turns without retaining forever.
const lansengerPendingDecorTTL = 15 * time.Minute

func newLansengerGatewayManager(app *App) *lansengerGatewayManager {
	peers := improactive.NewStore("").LoadOrEmpty()
	return &lansengerGatewayManager{
		app:               app,
		status:            gatewayConnectionStatusDisconnected,
		statusSince:       time.Now(),
		replyRoutes:       make(map[string]lansengerReplyRoute),
		groupInfoCache:    make(map[string]lansengerGroupInfoCacheEntry),
		lastPrivateUserID: strings.TrimSpace(peers.LansengerPrivateUserID),
	}
}

// SyncFromConfig reads the current AppConfig and starts or stops the gateway.
func (m *lansengerGatewayManager) SyncFromConfig() {
	m.syncFromConfig(false)
}

// Restart forcefully tears down the current gateway and starts a fresh
// connection from the current AppConfig.
func (m *lansengerGatewayManager) Restart() {
	m.syncFromConfig(true)
}

func (m *lansengerGatewayManager) syncFromConfig(forceRestart bool) {
	m.syncMu.Lock()
	defer m.syncMu.Unlock()

	cfg, err := m.app.LoadConfig()
	if err != nil {
		return
	}
	m.storeGroupFileLimits(cfg.LansengerGroupFileMaxBytes)

	appID := strings.TrimSpace(cfg.LansengerAppID)
	appSecret := strings.TrimSpace(cfg.LansengerAppSecret)
	gwURL := cfg.LansengerApiGatewayURL()
	wssURL := cfg.LansengerWebSocketGatewayURL()

	m.mu.Lock()
	if !cfg.LansengerEnabled || appID == "" || appSecret == "" {
		gw := m.gateway
		healthCancel := m.healthCancel
		m.gateway = nil
		m.status = gatewayConnectionStatusDisconnected
		m.statusSince = time.Now()
		m.lastToken = ""
		m.healthCancel = nil
		m.hubClaimSent = false
		clear(m.replyRoutes)
		clear(m.groupInfoCache)
		m.mu.Unlock()
		if healthCancel != nil {
			healthCancel()
		}
		if gw != nil {
			_ = gw.Stop()
		}
		if hubClient := m.app.hubClient(); hubClient != nil && hubClient.IsConnected() {
			_ = hubClient.SendIMGatewayUnclaim(imGatewayPlatformLansenger)
		}
		m.emitStatusEvent()
		return
	}

	cacheKey := appID + "|" + appSecret + "|" + gwURL + "|" + wssURL
	if m.gateway != nil && m.lastToken == cacheKey && !forceRestart && m.gateway.IsRunning() && m.status != gatewayConnectionStatusDisconnected && m.status != gatewayConnectionStatusError {
		m.mu.Unlock()
		return
	}

	oldGw := m.gateway
	// Remove the previous gateway before waiting for it to stop. This prevents
	// status callbacks or a concurrent read path from treating an obsolete
	// connection as the active one during a restart.
	m.gateway = nil
	m.mu.Unlock()
	if oldGw != nil {
		_ = oldGw.Stop()
	}

	gwCfg := lansenger.Config{
		AppID:             appID,
		AppSecret:         appSecret,
		ApiGatewayURL:     gwURL,
		WebSocketBaseURL:  wssURL,
		InboundMediaLimit: m.inboundMediaLimit,
	}
	gw := lansenger.NewGateway(gwCfg, m.onIncomingMessage)
	gw.SetStatusCallback(func(status string) {
		m.onGatewayStatusChange(gw, status)
	})

	m.mu.Lock()
	m.gateway = gw
	m.lastToken = cacheKey
	m.mu.Unlock()
	m.ensureHealthMonitor()

	if err := gw.Start(context.Background()); err != nil {
		log.Printf("[lansenger-mgr] start failed: %v", err)
		m.mu.Lock()
		m.gateway = nil
		m.lastToken = ""
		m.status = gatewayConnectionStatusError
		m.statusSince = time.Now()
		m.mu.Unlock()
		m.emitStatusEvent()
		return
	}
}

// Stop shuts down the gateway.
func (m *lansengerGatewayManager) Stop() {
	m.syncMu.Lock()
	defer m.syncMu.Unlock()

	m.mu.Lock()
	gw := m.gateway
	m.gateway = nil
	m.status = gatewayConnectionStatusDisconnected
	m.statusSince = time.Now()
	m.lastToken = ""
	m.hubClaimSent = false
	clear(m.replyRoutes)
	healthCancel := m.healthCancel
	m.healthCancel = nil
	lh := m.localHandler
	m.localHandler = nil
	m.mu.Unlock()
	if healthCancel != nil {
		healthCancel()
	}
	_ = lh // shared App conversation memory remains alive
	if gw != nil {
		_ = gw.Stop()
	}
	m.emitStatusEvent()
}

// Status returns the current connection status.
func (m *lansengerGatewayManager) Status() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status.String()
}

func (m *lansengerGatewayManager) ensureHealthMonitor() {
	m.mu.Lock()
	if m.healthCancel != nil {
		m.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.healthCancel = cancel
	m.mu.Unlock()

	go m.healthMonitorLoop(ctx)
}

func (m *lansengerGatewayManager) healthMonitorLoop(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.mu.Lock()
			gw := m.gateway
			status := m.status
			statusSince := m.statusSince
			lastRestart := m.lastRestart
			m.mu.Unlock()

			if lansengerRestartInCooldown(lastRestart, time.Now()) {
				continue
			}

			if status == gatewayConnectionStatusError || (gw != nil && !gw.IsRunning()) {
				running := gw != nil && gw.IsRunning()
				log.Printf("[lansenger-mgr] health monitor restarting gateway: status=%s running=%v", status, running)
				m.restartFromHealthMonitor("not_running_or_error")
				continue
			}
			if gw == nil || status == gatewayConnectionStatusDisconnected {
				continue
			}
			if lansengerStatusNeedsWatchdogRestart(status) && time.Since(statusSince) > 5*time.Minute {
				log.Printf("[lansenger-mgr] health monitor restarting stale gateway status: status=%s since=%v", status, statusSince.Format(time.RFC3339))
				m.restartFromHealthMonitor("stale_status_" + status.String())
				continue
			}
		}
	}
}

func lansengerStatusNeedsWatchdogRestart(status gatewayConnectionStatus) bool {
	switch status {
	case gatewayConnectionStatusConnecting, gatewayConnectionStatusReconnecting, gatewayConnectionStatusUnknown:
		return true
	default:
		return false
	}
}

func lansengerRestartInCooldown(lastRestart, now time.Time) bool {
	return !lastRestart.IsZero() && now.Sub(lastRestart) < time.Minute
}

func (m *lansengerGatewayManager) onGatewayStatusChange(gw *lansenger.Gateway, status string) {
	m.mu.Lock()
	if m.gateway != gw {
		m.mu.Unlock()
		log.Printf("[lansenger-mgr] ignoring stale gateway status: %s", status)
		return
	}
	normalized := normalizeGatewayConnectionStatus(status)
	if normalized != m.status {
		m.statusSince = time.Now()
	}
	m.status = normalized
	if normalized == gatewayConnectionStatusError {
		// Clear gateway reference so SyncFromConfig can retry on next call.
		m.gateway = nil
		m.lastToken = ""
	}
	m.mu.Unlock()
	m.emitStatusEvent()

	if normalized == gatewayConnectionStatusConnected {
		if cfg, err := m.app.LoadConfig(); err == nil && cfg.IsLansengerLocalMode() {
			return
		}
		hubClient := m.app.hubClient()
		if hubClient != nil && hubClient.IsConnected() {
			hubClient.SendIMGatewayClaim(imGatewayPlatformLansenger)
			m.mu.Lock()
			m.hubClaimSent = true
			m.mu.Unlock()
		}
	}
}

func (m *lansengerGatewayManager) restartFromHealthMonitor(reason string) {
	m.mu.Lock()
	if lansengerRestartInCooldown(m.lastRestart, time.Now()) {
		m.mu.Unlock()
		return
	}
	m.lastRestart = time.Now()
	m.mu.Unlock()
	m.app.emitEvent("lansenger-auto-restarting", reason)
	m.Restart()
}

func (m *lansengerGatewayManager) emitStatusEvent() {
	m.app.emitEvent("lansenger-status-changed", m.Status())
}

func (m *lansengerGatewayManager) resetLocalHandler() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.localHandler = nil
	m.hubClaimSent = false
}

// ---------------------------------------------------------------------------
// Message routing
// ---------------------------------------------------------------------------

func (m *lansengerGatewayManager) onIncomingMessage(msg lansenger.IncomingMessage) {
	cfg, err := m.app.LoadConfig()
	if err != nil {
		log.Printf("[lansenger-mgr] LoadConfig error: %v", err)
		return
	}
	groupOpts := lansengerGroupChatOptionsFromConfig(&cfg)
	if isLansengerGroupMessage(msg) && msg.MediaType != "" && !normalizeIMMediaKind(msg.MediaType).IsImage() {
		msg.AuditUserRecorded = m.recordGroupFileAudit(msg)
	}
	// Remember last private peer so proactive self-notify can reach "my" 蓝信会话.
	if !isLansengerGroupMessage(msg) {
		if uid := strings.TrimSpace(msg.FromUserID); uid != "" {
			m.noteLastPrivatePeer(uid)
		}
	}
	// Watch (盯人): record / keyword / CLI / auto-reply runs for delivered group
	// messages even when the bot is not @mentioned — so full speech capture works
	// when the platform pushes non-@ events. Does not claim the message for LLM.
	if isLansengerGroupMessage(msg) {
		if svc := m.app.watchService(); svc != nil {
			svc.processMessage(msg)
		}
		// Group summary buffer: same delivery scope as watch (before mention gate).
		m.recordGroupMessage(msg)
	}
	// Group policy + mention gate (OpenClaw / 蓝信文档: groupPolicy, requireMention,
	// respondToAtAll, allowlist, ignore list). Watch above intentionally runs first.
	//
	// Survey Q&A is special: after /survey starts, users often reply with bare "1"
	// or short text without @bot. requireMention would drop those before the
	// survey interceptor. Only bypass the *mention* requirement for survey-shaped
	// traffic; ignore list / allowlist / disabled policy still block.
	if isLansengerGroupMessage(msg) {
		if ok, reason := lansenger.GroupMessageAllowed(msg, groupOpts); !ok {
			// A /startmenu selection is commonly a bare `1`, parameter value,
			// or /confirm. In local mode it must be able to continue without
			// making the user @ the bot on every wizard step. This exemption is
			// session- and user-specific; all other group policy checks still win.
			if reason == "require_mention" && cfg.IsLansengerLocalMode() && m.app.localStartMenuService().active(lansengerLocalStartMenuSessionKey(msg)) {
				log.Printf("[lansenger-mgr] local startmenu mention-bypass: group=%s user=%s", msg.GroupID, msg.FromUserID)
			} else {
				if reason == "require_mention" && m.surveyCandidateBypassesMention(msg) {
					log.Printf("[lansenger-mgr] survey mention-bypass: group=%s user=%s", msg.GroupID, msg.FromUserID)
					if m.tryHandleSurveyMessage(msg) {
						log.Printf("[lansenger-mgr] survey intercept handled (no @): user=%s group=%s", msg.FromUserID, msg.GroupID)
						return
					}
					// Not an active survey answer — drop (same as require_mention).
					return
				}
				log.Printf("[lansenger-mgr] ignoring group message (agent): group=%s user=%s reason=%s", msg.GroupID, msg.FromUserID, reason)
				return
			}
		}
	}
	// /summary [start]: @Bot 群讨论摘要 — always local (needs on-device message buffer).
	// Bare /summary generates a summary; /summary start sets the cursor (ignore older msgs).
	if m.tryHandleGroupSummaryCommand(msg) {
		log.Printf("[lansenger-mgr] group summary command handled: group=%s user=%s", msg.GroupID, msg.FromUserID)
		return
	}
	// Survey intercept: after mention gate, before passthrough / local agent / Hub LLM.
	if m.tryHandleSurveyMessage(msg) {
		log.Printf("[lansenger-mgr] survey intercept handled: user=%s group=%s", msg.FromUserID, msg.GroupID)
		return
	}
	// Group messages often look like "@Bot /help" — detect slash commands on the
	// cleaned text so hub mode still forces local passthrough handling.
	if isPassthroughSlashText(stripLansengerBotMentions(msg)) {
		log.Printf("[lansenger-mgr] routing passthrough command locally: user=%s", msg.FromUserID)
		m.handleLocalMessage(msg, &cfg)
		return
	}
	// Group permissions are configured and enforced by this desktop instance.
	// Do not hand a group turn to Hub: the Hub-side generic IM pipeline does not
	// carry this machine's directory and knowledge-source allowlists, so forwarding
	// would turn a configured local restriction into a bypass in multi-machine
	// mode. Private messages retain their selected local/Hub routing behavior.
	if isLansengerGroupMessage(msg) {
		log.Printf("[lansenger-mgr] routing group message locally for permission enforcement: group=%s user=%s", msg.GroupID, msg.FromUserID)
		m.handleLocalMessage(msg, &cfg)
		return
	}

	isLocal := cfg.IsLansengerLocalMode()
	hubClient := m.app.hubClient()
	hubNil := hubClient == nil
	hubConn := !hubNil && hubClient.IsConnected()

	log.Printf("[lansenger-mgr] incoming: user=%s chat_type=%s group=%s local=%v hub_nil=%v hub_conn=%v text_len=%d",
		msg.FromUserID, msg.ChatType, msg.GroupID, isLocal, hubNil, hubConn, len(msg.Text))

	if isLocal {
		m.handleLocalMessage(msg, &cfg)
		return
	}

	if hubNil || !hubConn {
		log.Printf("[lansenger-mgr] Hub unavailable, falling back to local")
		m.notifyHubUnavailable(msg)
		m.handleLocalMessage(msg, &cfg)
		return
	}

	m.forwardToHub(msg)
}

func isLansengerGroupMessage(msg lansenger.IncomingMessage) bool {
	return lansenger.IsGroupChat(msg.ChatType)
}

func (m *lansengerGatewayManager) inboundMediaLimit(info lansenger.IncomingMediaInfo) (int64, bool) {
	if !lansenger.IsGroupChat(info.ChatType) {
		return 0, true
	}
	// Images retain the established multimodal path and legacy safety cap.
	if normalizeIMMediaKind(info.MediaType).IsImage() {
		return 0, true
	}
	limits := m.groupFileLimits.Load()
	if limits == nil {
		return 0, true
	}
	return (*limits)[strings.TrimSpace(info.GroupID)], true
}

func (m *lansengerGatewayManager) storeGroupFileLimits(source map[string]int64) {
	limits := make(lansengerGroupFileLimits, len(source))
	for groupID, maxBytes := range source {
		if groupID = strings.TrimSpace(groupID); groupID != "" && maxBytes > 0 {
			limits[groupID] = maxBytes
		}
	}
	m.groupFileLimits.Store(&limits)
}

// updateGroupFileLimit publishes a copy-on-write snapshot. syncMu serializes
// this with config reconciliation so an older config read cannot win afterward.
func (m *lansengerGatewayManager) updateGroupFileLimit(groupID string, maxBytes int64) {
	m.syncMu.Lock()
	defer m.syncMu.Unlock()
	limits := make(lansengerGroupFileLimits)
	if current := m.groupFileLimits.Load(); current != nil {
		for key, value := range *current {
			limits[key] = value
		}
	}
	groupID = strings.TrimSpace(groupID)
	if maxBytes > 0 {
		limits[groupID] = maxBytes
	} else {
		delete(limits, groupID)
	}
	m.groupFileLimits.Store(&limits)
}

// recordGroupFileAudit persists group files independently of agent routing.
// This keeps chat history complete even when mention/allowlist policy drops the
// message before it reaches the local agent loop.
func (m *lansengerGatewayManager) recordGroupFileAudit(msg lansenger.IncomingMessage) bool {
	store := m.app.getIMAuditStore()
	if store == nil {
		return false
	}
	audit := IMAuditMessage{
		UserID: lansengerConversationUserID(msg), Platform: "lansenger_local", Role: "user", Content: msg.Text,
		AttachmentName: safeIMAuditAttachmentName(msg.MediaName), AttachmentMediaType: msg.MediaType,
	}
	if len(msg.MediaData) > 0 {
		audit.AttachmentSize = int64(len(msg.MediaData))
		path, err := m.app.saveIMAuditAttachmentNamed("lansenger", msg.GroupID, msg.MessageID, audit.AttachmentName, msg.MediaData)
		if err != nil {
			log.Printf("[lansenger-mgr] save audit attachment failed: group=%s message=%s err=%v", msg.GroupID, msg.MessageID, err)
			if strings.TrimSpace(audit.Content) == "" {
				audit.Content = "[文件未保存到本地]"
			}
		} else {
			audit.AttachmentPath = path
		}
	} else if strings.TrimSpace(audit.Content) == "" {
		audit.Content = "[文件未保存到本地]"
	}
	if store.WriteCritical(audit) {
		return true
	}
	if audit.AttachmentPath != "" {
		_ = os.Remove(audit.AttachmentPath)
	}
	return false
}

// lansengerMayStageNonImageMediaLocally keeps group attachments outside the
// shared temp directory until a future, explicitly scoped attachment policy is
// available. Private chats retain their established attachment workflow.
func lansengerMayStageNonImageMediaLocally(msg lansenger.IncomingMessage) bool {
	return !isLansengerGroupMessage(msg)
}

// agentTextWithGroupContext prepends platform group metadata for LLM turns.
// Original msg.Text must stay unchanged so group reply quotes quote the user.
func (m *lansengerGatewayManager) agentTextWithGroupContext(msg lansenger.IncomingMessage, text string) string {
	if !isLansengerGroupMessage(msg) {
		return text
	}
	info := m.lookupGroupInfo(msg.GroupID)
	return lansenger.WithAgentGroupContext(text, msg, info)
}

// lookupGroupInfo returns cached or freshly fetched group metadata. Failures
// degrade to nil so message-level context is still injected.
func (m *lansengerGatewayManager) lookupGroupInfo(groupID string) *lansenger.GroupInfo {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return nil
	}

	now := time.Now()
	m.mu.Lock()
	if m.groupInfoCache == nil {
		m.groupInfoCache = make(map[string]lansengerGroupInfoCacheEntry)
	}
	if ent, ok := m.groupInfoCache[groupID]; ok {
		ttl := lansengerGroupInfoCacheTTL
		if ent.err {
			ttl = lansengerGroupInfoCacheNegTTL
		}
		if now.Sub(ent.at) < ttl {
			info := ent.info
			m.mu.Unlock()
			return info
		}
	}
	gw := m.gateway
	m.mu.Unlock()
	if gw == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), lansengerGroupInfoFetchTimeout)
	defer cancel()
	info, err := gw.GetGroupInfo(ctx, groupID)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.groupInfoCache == nil {
		m.groupInfoCache = make(map[string]lansengerGroupInfoCacheEntry)
	}
	if err != nil {
		log.Printf("[lansenger-mgr] GetGroupInfo %s: %v", groupID, err)
		m.groupInfoCache[groupID] = lansengerGroupInfoCacheEntry{err: true, at: now}
		return nil
	}
	// Cap cache size to avoid unbounded growth across many groups.
	if len(m.groupInfoCache) >= 256 {
		// Drop one arbitrary entry (map iteration order is random).
		for k := range m.groupInfoCache {
			delete(m.groupInfoCache, k)
			break
		}
	}
	m.groupInfoCache[groupID] = lansengerGroupInfoCacheEntry{info: info, at: now}
	return info
}

// lansengerGroupMessageMentionsBot is a thin wrapper kept for existing tests.
func lansengerGroupMessageMentionsBot(msg lansenger.IncomingMessage, appID string) bool {
	return lansenger.GroupMessageMentionsBot(msg, appID)
}

// lansengerGroupChatOptionsFromConfig maps AppConfig to package-level options.
func lansengerGroupChatOptionsFromConfig(cfg *corelib.AppConfig) lansenger.GroupChatOptions {
	if cfg == nil {
		return lansenger.GroupChatOptions{RequireMention: true, Policy: lansenger.GroupPolicyOpen}
	}
	return lansenger.GroupChatOptions{
		Policy:           cfg.EffectiveLansengerGroupPolicy(),
		RequireMention:   cfg.IsLansengerRequireMention(),
		RespondToAtAll:   cfg.LansengerRespondToAtAll,
		AutoMentionReply: cfg.LansengerAutoMentionReply,
		AutoQuoteReply:   cfg.LansengerAutoQuoteReply,
		AllowedGroupIDs:  append([]string(nil), cfg.LansengerAllowedGroupIDs...),
		IgnoredGroupIDs:  append([]string(nil), cfg.LansengerIgnoredGroupIDs...),
		AppID:            strings.TrimSpace(cfg.LansengerAppID),
	}
}

// buildLansengerOutgoingText builds a reply with optional native @mention and quote.
// When AutoQuoteReply is on and MessageID is present, text-based "xx问：" quotes are
// skipped to avoid double-quoting. systemNotice disables @/native-quote (status text).
func buildLansengerOutgoingText(msg lansenger.IncomingMessage, text string, opts lansenger.GroupChatOptions) lansenger.OutgoingText {
	return buildLansengerOutgoingTextEx(msg, text, opts, false)
}

func buildLansengerOutgoingTextEx(msg lansenger.IncomingMessage, text string, opts lansenger.GroupChatOptions, systemNotice bool) lansenger.OutgoingText {
	reminder, refMsgID := lansenger.BuildReplyDecorationsEx(msg, opts, systemNotice)
	isGroup := isLansengerGroupMessage(msg)
	// Status/error notices stay plain (no "xx问：" / @ / refMsgId). Agent answers
	// in groups get a text quote unless a native refMsgId quote will be attached.
	if isGroup && !systemNotice && !lansenger.PreferNativeGroupQuote(opts, refMsgID) {
		// Quote the cleaned user text (without leading @Bot tokens) when available.
		question := stripLansengerBotMentions(msg)
		if question == "" {
			question = msg.Text
		}
		decision := lansenger.DecideGroupReplySender(msg)
		// Log only when quote falls back away from a display name (the common bug class).
		if decision.Source != lansenger.GroupReplySenderSourceDisplayName {
			log.Printf("[lansenger-mgr] text-quote fallback: from=%s rawName=%q label=%q source=%s reason=%s q_runes=%d",
				msg.FromUserID, msg.SenderName, decision.Label, decision.Source, decision.Reason, len([]rune(question)))
		}
		text = lansenger.MaybeFormatGroupReplyWithQuoteFromMessage(true, msg, question, text)
	}
	return lansenger.OutgoingText{
		ToUserID: lansengerReplyTarget(msg),
		Text:     text,
		IsGroup:  isGroup,
		Reminder: reminder,
		RefMsgID: refMsgID,
	}
}

func (m *lansengerGatewayManager) notifyHubUnavailable(msg lansenger.IncomingMessage) {
	m.mu.Lock()
	gw := m.gateway
	m.mu.Unlock()
	if gw == nil {
		return
	}
	_ = gw.SendText(context.Background(), buildLansengerOutgoingTextEx(
		msg, "当前为多机模式，但 Hub 未连接。消息已回退到本地处理。", m.currentGroupOpts(), true))
}

func (m *lansengerGatewayManager) forwardToHub(msg lansenger.IncomingMessage) {
	hubClient := m.app.hubClient()
	if hubClient == nil || !hubClient.IsConnected() {
		m.notifyHubUnavailable(msg)
		m.handleLocalMessage(msg)
		return
	}

	// Ensure gateway claim is registered before forwarding. In rare cases the
	// claim may not have been sent yet (e.g. hub reconnected after mode switch).
	// Re-sending claim is idempotent — Hub accepts it if already owned by us.
	// Only send once per connection to avoid unnecessary WebSocket writes.
	m.mu.Lock()
	needsClaim := !m.hubClaimSent
	m.mu.Unlock()
	if needsClaim {
		if err := hubClient.SendIMGatewayClaim(imGatewayPlatformLansenger); err != nil {
			log.Printf("[lansenger-mgr] forwardToHub: pre-claim failed: %v, falling back to local", err)
			m.notifyHubUnavailable(msg)
			m.handleLocalMessage(msg)
			return
		}
		m.mu.Lock()
		m.hubClaimSent = true
		m.mu.Unlock()
	}

	msgType := "text"
	if msg.MediaType != "" && len(msg.MediaData) > 0 {
		msgType = msg.MediaType
	}
	replyTarget := lansengerReplyTarget(msg)
	// Strip @Bot tokens for hub agent text and for outbound quote cache.
	cleanText := stripLansengerBotMentions(msg)
	// Correlation id is always set so multi-chunk hub replies can pair without
	// FIFO-stealing. Platform message id is only used for native refMsgId.
	platformMsgID := strings.TrimSpace(msg.MessageID)
	corrID := platformMsgID
	if corrID == "" {
		corrID = "mc-" + uuid.NewString()
	}
	// Cache the cleaned user question + ids for group reply quotes —
	// not the agent-enriched text that may include group metadata prefixes.
	m.rememberReplyRoute(replyTarget, isLansengerGroupMessage(msg), msg.FromUserID, msg.SenderName, cleanText, platformMsgID, corrID)
	payload := map[string]any{
		// platform_uid remains the human sender for Hub identity, binding and
		// per-user routing. The separate reply target preserves group delivery.
		"platform_uid": msg.FromUserID,
		"reply_target": replyTarget,
		"text":         m.agentTextWithGroupContext(msg, cleanText),
		"message_type": msgType,
		"chat_type":    msg.ChatType,
		"group_id":     msg.GroupID,
		// Always send so Hub echoes source_message_id (concurrent + multi-chunk).
		"message_id": corrID,
	}
	if attachment := buildMediaAttachment(msg.MediaType, msg.MediaData, msg.MediaName, ""); attachment != nil {
		payload["attachments"] = []map[string]any{attachment}
	}
	if msg.SenderName != "" {
		payload["sender_name"] = msg.SenderName
	}
	if msg.GroupName != "" {
		payload["group_name"] = msg.GroupName
	}

	if err := hubClient.SendIMGatewayMessage(imGatewayPlatformLansenger, payload); err != nil {
		log.Printf("[lansenger-mgr] forwardToHub error: %v, falling back to local", err)
		// Local path decorates via buildLansengerOutgoingText; drop the hub slot
		// so a late hub rejection/ack cannot steal or double-decorate.
		m.forgetReplyDecor(replyTarget, corrID)
		m.handleLocalMessage(msg)
	}
}

// lansengerReplyTarget selects the conversation ID for replies.  Group events
// carry a sender ID and a group ID; sending to the sender would turn a group
// reply into a private message.
func lansengerReplyTarget(msg lansenger.IncomingMessage) string {
	if isLansengerGroupMessage(msg) && strings.TrimSpace(msg.GroupID) != "" {
		return msg.GroupID
	}
	return msg.FromUserID
}

func (m *lansengerGatewayManager) rememberReplyRoute(target string, isGroup bool, senderID, senderName, question, messageID, correlationID string) {
	target = strings.TrimSpace(target)
	if target == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.replyRoutes == nil {
		m.replyRoutes = make(map[string]lansengerReplyRoute)
	}
	if _, exists := m.replyRoutes[target]; !exists && len(m.replyRoutes) >= maxLansengerReplyRoutes {
		var oldestKey string
		var oldest time.Time
		for key, route := range m.replyRoutes {
			if oldestKey == "" || route.seenAt.Before(oldest) {
				oldestKey, oldest = key, route.seenAt
			}
		}
		delete(m.replyRoutes, oldestKey)
	}
	route := m.replyRoutes[target]
	route.isGroup = isGroup || route.isGroup
	msgID := strings.TrimSpace(messageID)
	corrID := strings.TrimSpace(correlationID)
	if corrID == "" {
		corrID = msgID
	}
	now := time.Now()
	// Cache only a distinct display name (never staffId-as-name). Hub reply
	// reconstruction feeds this back into GroupReplySenderLabel.
	decision := lansenger.DecideGroupReplySender(lansenger.IncomingMessage{
		FromUserID: senderID,
		SenderName: senderName,
	})
	cleanName := decision.CleanName
	if isGroup && decision.Source != lansenger.GroupReplySenderSourceDisplayName {
		log.Printf("[lansenger-mgr] rememberReplyRoute no-display-name target=%s senderID=%s rawName=%q source=%s reason=%s corr=%s",
			target, strings.TrimSpace(senderID), senderName, decision.Source, decision.Reason, corrID)
	}
	decor := lansengerReplyDecor{
		senderID:      strings.TrimSpace(senderID),
		senderName:    cleanName,
		question:      strings.TrimSpace(question),
		messageID:     msgID,
		correlationID: corrID,
		at:            now,
	}
	route.lastSender = decor.senderID
	route.lastSenderName = decor.senderName
	route.lastQuestion = decor.question
	route.lastMessageID = decor.messageID
	route.pending = append(pruneLansengerPendingDecors(route.pending, now), decor)
	// Drop oldest pending slots if this target is under a reply storm.
	if len(route.pending) > maxLansengerPendingDecors {
		route.pending = append([]lansengerReplyDecor(nil), route.pending[len(route.pending)-maxLansengerPendingDecors:]...)
	}
	route.seenAt = now
	m.replyRoutes[target] = route
}

// pruneLansengerPendingDecors drops slots older than lansengerPendingDecorTTL.
func pruneLansengerPendingDecors(pending []lansengerReplyDecor, now time.Time) []lansengerReplyDecor {
	if len(pending) == 0 {
		return pending
	}
	out := pending[:0]
	for _, d := range pending {
		if d.at.IsZero() || now.Sub(d.at) <= lansengerPendingDecorTTL {
			out = append(out, d)
		}
	}
	// If all pruned, return a fresh nil slice (not a reused full-cap alias).
	if len(out) == 0 {
		return nil
	}
	return out
}

func (m *lansengerGatewayManager) isGroupReplyTarget(target string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	route, ok := m.replyRoutes[strings.TrimSpace(target)]
	return ok && route.isGroup
}

func (m *lansengerGatewayManager) groupReplyQuote(target string) (decor lansengerReplyDecor, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	route, found := m.replyRoutes[strings.TrimSpace(target)]
	if !found || !route.isGroup {
		return lansengerReplyDecor{}, false
	}
	// Prefer the next pending slot (matches the next reply); fall back to last.
	if len(route.pending) > 0 {
		return route.pending[0], true
	}
	if route.lastQuestion == "" && route.lastSender == "" && route.lastSenderName == "" && route.lastMessageID == "" {
		return lansengerReplyDecor{}, false
	}
	return lansengerReplyDecor{
		senderID:   route.lastSender,
		senderName: route.lastSenderName,
		question:   route.lastQuestion,
		messageID:  route.lastMessageID,
	}, true
}

// takeReplyDecorations pops a pending decoration slot for this target
// (group or DM). preferredID must match correlationID (hub source_message_id)
// or platform messageID. Empty preferredID never decorates — queue acks,
// ownership rejects, and progress without ReplyMeta would otherwise FIFO-steal
// agent answer slots. When preferredID is set but not pending (already consumed
// by an earlier multi-chunk fragment), returns ok=false.
// Returned messageID is the platform id for native refMsgId (may be empty).
func (m *lansengerGatewayManager) takeReplyDecorations(target string, preferredID string) (decor lansengerReplyDecor, ok bool) {
	target = strings.TrimSpace(target)
	preferredID = strings.TrimSpace(preferredID)
	if preferredID == "" {
		return lansengerReplyDecor{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	route, found := m.replyRoutes[target]
	if !found {
		return lansengerReplyDecor{}, false
	}
	now := time.Now()
	route.pending = pruneLansengerPendingDecors(route.pending, now)
	if len(route.pending) == 0 {
		m.replyRoutes[target] = route
		return lansengerReplyDecor{}, false
	}
	idx := -1
	for i, d := range route.pending {
		if strings.TrimSpace(d.correlationID) == preferredID || strings.TrimSpace(d.messageID) == preferredID {
			idx = i
			break
		}
	}
	if idx < 0 {
		// Multi-chunk: first chunk already took this id.
		m.replyRoutes[target] = route
		return lansengerReplyDecor{}, false
	}
	d := route.pending[idx]
	route.pending = append(route.pending[:idx], route.pending[idx+1:]...)
	m.replyRoutes[target] = route
	d.messageID = strings.TrimSpace(d.messageID)
	return d, true
}

// forgetReplyDecor drops a pending decoration slot by correlation id (e.g. when
// hub forward fails and we fall back to local handling that decorates itself).
func (m *lansengerGatewayManager) forgetReplyDecor(target, correlationID string) {
	target = strings.TrimSpace(target)
	correlationID = strings.TrimSpace(correlationID)
	if target == "" || correlationID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	route, found := m.replyRoutes[target]
	if !found {
		return
	}
	for i, d := range route.pending {
		if strings.TrimSpace(d.correlationID) == correlationID || strings.TrimSpace(d.messageID) == correlationID {
			route.pending = append(route.pending[:i], route.pending[i+1:]...)
			m.replyRoutes[target] = route
			return
		}
	}
}

// groupReplyText prefixes group replies with "{显示名}问：问题" so interleaved
// group answers remain attributable. Private replies are unchanged.
// Prefer native RefMsgID quotes when AutoQuoteReply is enabled.
func (m *lansengerGatewayManager) groupReplyText(msg lansenger.IncomingMessage, reply string) string {
	opts := m.currentGroupOpts()
	if lansenger.PreferNativeGroupQuote(opts, msg.MessageID) {
		return strings.TrimSpace(reply)
	}
	question := stripLansengerBotMentions(msg)
	if question == "" {
		question = msg.Text
	}
	return lansenger.MaybeFormatGroupReplyWithQuoteFromMessage(isLansengerGroupMessage(msg), msg, question, reply)
}

func (m *lansengerGatewayManager) currentGroupOpts() lansenger.GroupChatOptions {
	if m == nil || m.app == nil {
		return lansenger.GroupChatOptions{RequireMention: true, Policy: lansenger.GroupPolicyOpen}
	}
	cfg, err := m.app.LoadConfig()
	if err != nil {
		return lansenger.GroupChatOptions{RequireMention: true, Policy: lansenger.GroupPolicyOpen}
	}
	return lansengerGroupChatOptionsFromConfig(&cfg)
}

// ---------------------------------------------------------------------------
// Local mode — direct LLM agent loop
// ---------------------------------------------------------------------------

func (m *lansengerGatewayManager) ensureLocalHandler() *IMMessageHandler {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.localHandler != nil {
		return m.localHandler
	}

	a := m.app
	a.ensureInteractionInfra()
	if a.memoryStore == nil {
		a.ensureMemoryStore()
	}
	if a.contextResolver == nil {
		a.ensureContextResolver()
	}
	if a.sessionPrecheck == nil {
		a.ensureSessionPrecheck()
	}

	h := NewIMMessageHandler(a, a.remoteSessions)
	if a.capabilityGapDetector == nil {
		a.ensureCapabilityGapDetector()
	}
	if a.capabilityGapDetector != nil {
		h.SetCapabilityGapDetector(a.capabilityGapDetector)
	}
	if a.toolDefGenerator != nil {
		h.SetToolDefGenerator(a.toolDefGenerator)
	}
	if a.toolRouter != nil {
		h.SetToolRouter(a.toolRouter)
	}
	if a.usageTracker != nil {
		h.SetUsageTracker(a.usageTracker)
	}
	if a.memoryStore != nil {
		h.SetMemoryStore(a.memoryStore)
	}
	h.SetTrajectoryRecorderFactory(a.buildTrajectoryRecorderFactory())
	if a.configManager != nil {
		h.SetConfigManager(a.configManager)
	}
	if a.templateManager != nil {
		h.SetTemplateManager(a.templateManager)
	}
	if a.scheduledTaskManager != nil {
		h.SetScheduledTaskManager(a.scheduledTaskManager)
	}
	if a.contextResolver != nil {
		h.SetContextResolver(a.contextResolver)
	}
	if a.sessionPrecheck != nil {
		h.SetSessionPrecheck(a.sessionPrecheck)
	}
	a.ensureStartupFeedback()
	if a.startupFeedback != nil {
		h.SetStartupFeedback(a.startupFeedback)
	}
	if a.securityFirewall == nil {
		a.ensureSecurityFirewall()
	}
	if a.securityFirewall != nil {
		h.SetSecurityFirewall(a.securityFirewall)
	}
	a.ensureConversationArchiver()
	if a.conversationArchiver != nil {
		h.memory.Archiver = a.conversationArchiver
	}

	m.localHandler = h
	log.Printf("[lansenger-mgr] local IMMessageHandler created")
	return h
}

func (m *lansengerGatewayManager) handleLocalMessage(msg lansenger.IncomingMessage, loadedConfig ...*corelib.AppConfig) {
	// Always work from cleaned text so "@Bot /help" matches slash passthrough
	// the same way as plain "/help".
	cleanText := stripLansengerBotMentions(msg)
	// A start-menu confirmation creates a local coding/general task outside the
	// group permission boundary. Keep it available in private chats, but never
	// allow a group member to bootstrap an unrestricted local task this way.
	if isLansengerGroupMessage(msg) && m.app.localStartMenuService().active(lansengerLocalStartMenuSessionKey(msg)) {
		m.mu.Lock()
		gw := m.gateway
		m.mu.Unlock()
		if gw != nil {
			_ = gw.SendText(context.Background(), buildLansengerOutgoingTextEx(
				msg, "群聊中不能启动本地任务快捷方式，请在私聊中操作。", m.currentGroupOpts(), true))
		}
		return
	}
	if !isLansengerGroupMessage(msg) {
		// Standalone LANXIN must stay fully local. Handle the stateful wizard
		// before generic passthrough consumes /run during an active shortcut flow.
		menu := m.app.localStartMenuService().handle(lansengerLocalStartMenuSessionKey(msg), cleanText)
		if menu.Handled {
			m.mu.Lock()
			gw := m.gateway
			m.mu.Unlock()
			reply := menu.Reply
			if menu.Confirmed {
				if err := m.app.openLocalStartMenuTask(menu, "lansenger", lansengerReplyTarget(msg), false); err != nil {
					reply = "启动任务失败：" + err.Error()
				} else {
					reply = startMenuTaskCreatedReply(menu.AgentMode == "remote_coding_dev")
				}
			}
			if gw != nil && reply != "" {
				_ = gw.SendText(context.Background(), buildLansengerOutgoingText(msg, reply, m.currentGroupOpts()))
			}
			return
		}
	}
	// Passthrough commands can execute registered local programs directly and do
	// not pass through the agent-loop permission guard. Never expose that escape
	// hatch to a group, even if the caller has configured a directory allowlist.
	if isLansengerGroupMessage(msg) && isPassthroughSlashText(cleanText) {
		m.mu.Lock()
		gw := m.gateway
		m.mu.Unlock()
		if gw != nil {
			_ = gw.SendText(context.Background(), buildLansengerOutgoingTextEx(
				msg, "群聊中不能执行直通命令，请在私聊中操作。", m.currentGroupOpts(), true))
		}
		return
	}
	if resp, handled := m.app.TryHandlePassthroughSlashCommandWithSource(cleanText, "lansenger:"+msg.FromUserID); handled {
		m.mu.Lock()
		gw := m.gateway
		m.mu.Unlock()
		if gw != nil {
			reply := resp.Text
			if reply == "" {
				reply = resp.Error
			}
			if reply == "" {
				reply = "(no output)"
			}
			_ = gw.SendText(context.Background(), buildLansengerOutgoingText(msg, reply, m.currentGroupOpts()))
		}
		return
	}
	if !m.app.isMaclawLLMConfigured() {
		m.mu.Lock()
		gw := m.gateway
		m.mu.Unlock()
		if gw != nil {
			_ = gw.SendText(context.Background(), buildLansengerOutgoingTextEx(
				msg, i18n.T(i18n.MsgLLMNotConfigured, "zh"), m.currentGroupOpts(), true))
		}
		return
	}

	handler := m.ensureLocalHandler()

	m.mu.Lock()
	gw := m.gateway
	m.mu.Unlock()
	if gw == nil {
		return
	}

	// Agent-facing text: already cleaned of leading @Bot tokens (survey/watch match).
	// Keep msg.Text raw for any call sites that still need the platform payload;
	// outbound quotes prefer the stripped form via buildLansengerOutgoingTextEx.
	text := cleanText

	// Pass images/files as attachments, matching the pattern used by
	// WeChat, Telegram and QQ gateways.
	var attachments []MessageAttachment
	if msg.MediaType != "" && len(msg.MediaData) > 0 {
		if normalizeIMMediaKind(msg.MediaType).IsImage() {
			// Image → multimodal attachment for LLM vision.
			// If the LLM doesn't support vision, buildUserContent will
			// save it to a local file and tell the LLM accordingly.
			attachments = append(attachments, buildLocalImageAttachment(msg.MediaData, msg.MediaName, ""))
		} else if !lansengerMayStageNonImageMediaLocally(msg) {
			// A group turn begins with no filesystem grant. Staging an arbitrary
			// inbound file in the shared local temp directory before the agent-loop
			// policy runs would violate that boundary (and the file would not be
			// readable by the restricted tool set anyway). Keep the metadata in the
			// prompt, but require a private chat for non-image file processing.
			text = "[收到" + mediaLabel(msg.MediaType) + "附件；群聊权限不会将非图片附件保存到本机，请在私聊中发送或粘贴需要处理的文本。]\n" + text
		} else {
			// Non-image media (file, voice, video) → save to local temp
			// and prepend the path to the text so the agent can read it.
			mediaPath, err := saveMediaToTempDir("lansenger", "ls_", msg.FromUserID, msg.MediaType, msg.MediaData, msg.MediaName)
			if err != nil {
				log.Printf("[lansenger-mgr] save media error: %v", err)
			} else {
				prefix := "[收到" + mediaLabel(msg.MediaType) + ": " + mediaPath + "]\n"
				text = prefix + text
			}
		}
	}

	// Inject structured group metadata + anti-hallucination rules so the agent
	// can reason about the room without inventing member/bot rosters.
	text = m.agentTextWithGroupContext(msg, text)

	if text == "" && len(attachments) == 0 {
		return
	}

	progressFilter := newIMProgressVisibilityFilter(m.app)
	var lastProgress time.Time
	var lastProgressText string
	onProgress := func(progressText string) {
		// Progress is IM implementation detail.  In a group it would expose
		// agent activity to every member, so never publish it (including the
		// first progress update that the generic visibility filter permits).
		if !shouldSendLansengerIMDetail(msg.ChatType) {
			return
		}
		if !progressFilter.ShouldSendProgress(progressText) {
			return
		}
		now := time.Now()
		if now.Sub(lastProgress) < 2*time.Second {
			return
		}
		stripped := textutil.StripMarkdown(progressText)
		if stripped == lastProgressText {
			return
		}
		lastProgress = now
		lastProgressText = stripped
		_ = gw.SendText(context.Background(), lansenger.OutgoingText{
			ToUserID: lansengerReplyTarget(msg),
			Text:     i18n.T(i18n.MsgProgressPrefix, appUILang(m.app)) + stripped,
			IsGroup:  isLansengerGroupMessage(msg),
		})
	}

	userMessage := IMUserMessage{
		UserID:        lansengerConversationUserID(msg),
		Platform:      "lansenger_local",
		Text:          text,
		Lang:          appUILang(m.app),
		Attachments:   attachments,
		SkipUserAudit: msg.AuditUserRecorded,
	}
	var loopCtx *LoopContext
	if isLansengerGroupMessage(msg) {
		cfg := (*corelib.AppConfig)(nil)
		if len(loadedConfig) > 0 {
			cfg = loadedConfig[0]
		}
		if cfg == nil {
			if current, err := m.app.LoadConfig(); err == nil {
				cfg = &current
			}
		}
		permissions := lansengerGroupPermissionsFromConfig(cfg)
		loopCtx = NewLoopContext("lansenger-group", handler.getMaclawAgentMaxIterations(), handler.client)
		loopCtx.Platform = userMessage.Platform
		loopCtx.UserID = userMessage.UserID
		loopCtx.Lang = userMessage.Lang
		loopCtx.LansengerGroupPermissions = &permissions
	}
	var resp *IMAgentResponse
	if loopCtx != nil {
		resp = handler.HandleIMMessageWithExistingLoop(userMessage, loopCtx, onProgress, nil, nil, nil)
	} else {
		resp = handler.HandleIMMessageWithProgress(userMessage, onProgress)
	}

	if resp == nil || resp.Deferred {
		return
	}

	m.sendAgentResponse(gw, msg, resp)
}

// lansengerConversationUserID keeps each group conversation independent from
// both the same person's private chat and that person's messages in other
// groups. Conversation memory, pending confirmations, session-scoped state,
// and long-running task bookkeeping all key off IMUserMessage.UserID; using
// FromUserID alone would let a group prompt inherit or expose private context.
func lansengerConversationUserID(msg lansenger.IncomingMessage) string {
	if !isLansengerGroupMessage(msg) {
		return strings.TrimSpace(msg.FromUserID)
	}
	groupID := strings.TrimSpace(msg.GroupID)
	userID := strings.TrimSpace(msg.FromUserID)
	return fmt.Sprintf("lansenger-group:%d:%s:%d:%s", len(groupID), groupID, len(userID), userID)
}

func lansengerLocalStartMenuSessionKey(msg lansenger.IncomingMessage) string {
	// Delimit each part unambiguously. IDs are external input and may contain
	// ':'; the previous concatenation could make two different conversations
	// share a wizard state (for example target "a:b" + user "c" versus target
	// "a" + user "b:c").
	target := strings.TrimSpace(lansengerReplyTarget(msg))
	user := strings.TrimSpace(msg.FromUserID)
	return fmt.Sprintf("lansenger:%d:%s:%d:%s", len(target), target, len(user), user)
}

// shouldSendLansengerIMDetail controls process/status messages only.  Final
// agent replies must still be delivered to groups; they are conversation
// content rather than implementation detail.
func shouldSendLansengerIMDetail(chatType string) bool {
	return !strings.EqualFold(strings.TrimSpace(chatType), "group")
}

func (m *lansengerGatewayManager) sendAgentResponse(gw *lansenger.Gateway, msg lansenger.IncomingMessage, resp *IMAgentResponse) {
	ctx := context.Background()
	toUserID := lansengerReplyTarget(msg)
	isGroup := isLansengerGroupMessage(msg)
	opts := m.currentGroupOpts()

	if resp.Text != "" {
		text := textutil.StripMarkdown(resp.Text)
		// Lanxin (蓝信) does not support interactive buttons/cards.
		// Degrade resp.Actions to numbered text options appended to the message.
		if len(resp.Actions) > 0 {
			// FormatAskUserForDisplay may append generic input hints.
			// Strip those hints and replace them with concrete numbered options.
			// Strip these generic hints and replace with the concrete numbered
			// options so the user sees one clear instruction.
			for _, hint := range []string{
				"请输入：确认 或 取消",
				"请输入：选项编号或内容",
				"请输入：确认 或 修改意见",
				"请直接输入您的回复",
				"请回复「确认」或「取消」", // legacy
			} {
				text = strings.TrimSuffix(strings.TrimSpace(text), hint)
			}
			text = strings.TrimSpace(text)
			text += "\n\n请回复对应选项："
			for i, action := range resp.Actions {
				text += fmt.Sprintf("\n%d. %s", i+1, action.Label)
			}
		}
		// Quote / @mention: native refMsgId + reminder when configured; else text quote.
		if err := gw.SendText(ctx, buildLansengerOutgoingText(msg, text, opts)); err != nil {
			log.Printf("[lansenger-mgr] SendText error: %v", err)
		}
	} else if len(resp.Actions) > 0 {
		// Actions without text body — send as standalone options message.
		text := "请回复对应选项："
		for i, action := range resp.Actions {
			text += fmt.Sprintf("\n%d. %s", i+1, action.Label)
		}
		_ = gw.SendText(ctx, buildLansengerOutgoingText(msg, text, opts))
	}

	if resp.Error != "" && resp.Text == "" && len(resp.Actions) == 0 {
		// Errors are system notices: no auto-@ / native quote spam.
		_ = gw.SendText(ctx, buildLansengerOutgoingTextEx(msg, textutil.StripMarkdown(resp.Error), opts, true))
	}

	if resp.ImageKey != "" {
		imgData, err := base64.StdEncoding.DecodeString(resp.ImageKey)
		if err == nil && len(imgData) > 0 {
			_ = gw.SendMedia(ctx, lansenger.OutgoingMedia{
				ToUserID:  toUserID,
				FileData:  imgData,
				MediaType: "image",
				IsGroup:   isGroup,
			})
		}
	}

	if resp.FileData != "" {
		fileBytes, err := base64.StdEncoding.DecodeString(resp.FileData)
		if err == nil && len(fileBytes) > 0 {
			_ = gw.SendMedia(ctx, lansenger.OutgoingMedia{
				ToUserID:  toUserID,
				FileData:  fileBytes,
				FileName:  resp.FileName,
				MediaType: "file",
				IsGroup:   isGroup,
			})
		}
	}

	// Send voice message as a file because Lansenger does not expose a native voice type.
	if resp.VoiceData != "" {
		voiceBytes, err := base64.StdEncoding.DecodeString(resp.VoiceData)
		if err != nil || len(voiceBytes) == 0 {
			log.Printf("[lansenger-mgr] decode voice data failed (to=%s): %v", toUserID, err)
		} else if err := gw.SendMedia(ctx, lansenger.OutgoingMedia{
			ToUserID:  toUserID,
			FileData:  voiceBytes,
			FileName:  resp.VoiceFileName,
			MediaType: "file",
			IsGroup:   isGroup,
		}); err != nil {
			log.Printf("[lansenger-mgr] SendMedia voice file failed (to=%s): %v", toUserID, err)
		}
	}

	m.sendLocalFiles(gw, toUserID, isGroup, resp)
}

func (m *lansengerGatewayManager) sendLocalFiles(gw *lansenger.Gateway, toUserID string, isGroup bool, resp *IMAgentResponse) {
	paths := resp.LocalFilePaths
	if resp.LocalFilePath != "" && !containsString(paths, resp.LocalFilePath) {
		paths = append([]string{resp.LocalFilePath}, paths...)
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			log.Printf("[lansenger-mgr] read local file %s error: %v", p, err)
			continue
		}
		name := filepath.Base(p)
		mediaType := mediaTypeFromFileName(name)
		mediaKind := normalizeIMMediaKind(mediaType)
		if mediaKind.IsVoice() || mediaKind.IsAudio() {
			mediaType = imMediaFile.String()
		}
		if err := gw.SendMedia(context.Background(), lansenger.OutgoingMedia{
			ToUserID:  toUserID,
			FileData:  data,
			FileName:  name,
			MediaType: mediaType,
			IsGroup:   isGroup,
		}); err != nil {
			log.Printf("[lansenger-mgr] SendMedia local file failed (to=%s file=%s): %v", toUserID, p, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Hub reply handling
// ---------------------------------------------------------------------------

// HandleGatewayReply dispatches a reply from Hub to the Lansenger API.
func (m *lansengerGatewayManager) HandleGatewayReply(reply GatewayReplyPayload) {
	m.mu.Lock()
	gw := m.gateway
	m.mu.Unlock()
	if gw == nil {
		log.Printf("[lansenger-mgr] HandleGatewayReply: gateway is nil, dropping")
		return
	}

	ctx := context.Background()
	isGroup := lansenger.IsGroupChat(reply.ChatType) || m.isGroupReplyTarget(reply.PlatformUID)
	switch normalizeGatewayReplyTypeKind(reply.ReplyType) {
	case gatewayReplyTypeText:
		text := textutil.StripMarkdown(reply.Text)
		opts := m.currentGroupOpts()
		// First hub text chunk only: reuse the same decoration builder as local
		// mode so @mention / refMsgId / text-quote stay consistent. Later chunks
		// stay plain (takeReplyDecorations returns false once marked).
		out := lansenger.OutgoingText{
			ToUserID: reply.PlatformUID,
			Text:     text,
			IsGroup:  isGroup,
		}
		if decor, ok := m.takeReplyDecorations(reply.PlatformUID, reply.SourceMessageID); ok {
			inbound := lansenger.IncomingMessage{
				ChatType:  "p2p",
				MessageID: decor.messageID,
				// question is already cleaned (no leading @Bot) when cached.
				Text:       decor.question,
				SenderName: strings.TrimSpace(decor.senderName),
			}
			if isGroup {
				inbound.ChatType = "group"
				inbound.GroupID = reply.PlatformUID
				// Prefer cached route sender; hub may also echo sender_id.
				inbound.FromUserID = strings.TrimSpace(decor.senderID)
				if inbound.FromUserID == "" {
					inbound.FromUserID = strings.TrimSpace(reply.SenderID)
				}
			} else {
				// DM: route sender if present, else conversation peer (PlatformUID).
				inbound.FromUserID = strings.TrimSpace(decor.senderID)
				if inbound.FromUserID == "" {
					inbound.FromUserID = strings.TrimSpace(reply.SenderID)
				}
				if inbound.FromUserID == "" {
					inbound.FromUserID = strings.TrimSpace(reply.PlatformUID)
				}
			}
			if isGroup {
				d := lansenger.DecideGroupReplySender(inbound)
				if d.Source != lansenger.GroupReplySenderSourceDisplayName {
					log.Printf("[lansenger-mgr] hub reply decor fallback: target=%s sourceMsg=%s senderID=%s cachedName=%q label=%q source=%s reason=%s",
						reply.PlatformUID, reply.SourceMessageID, inbound.FromUserID, decor.senderName, d.Label, d.Source, d.Reason)
				}
			}
			out = buildLansengerOutgoingText(inbound, text, opts)
			out.ToUserID = reply.PlatformUID
			out.IsGroup = isGroup
		}
		if err := gw.SendText(ctx, out); err != nil {
			log.Printf("[lansenger-mgr] HandleGatewayReply SendText failed (to=%s group=%v): %v", reply.PlatformUID, isGroup, err)
		}
	case gatewayReplyTypeImage:
		data, err := base64.StdEncoding.DecodeString(reply.ImageData)
		if err != nil {
			log.Printf("[lansenger-mgr] HandleGatewayReply image decode: %v", err)
			return
		}
		if err := gw.SendMedia(ctx, lansenger.OutgoingMedia{
			ToUserID:  reply.PlatformUID,
			FileData:  data,
			MediaType: imMediaImage.String(),
			IsGroup:   isGroup,
		}); err != nil {
			log.Printf("[lansenger-mgr] HandleGatewayReply SendMedia image failed: %v", err)
		}
	case gatewayReplyTypeFile:
		data, err := base64.StdEncoding.DecodeString(reply.FileData)
		if err != nil {
			log.Printf("[lansenger-mgr] HandleGatewayReply file decode: %v", err)
			return
		}
		if err := gw.SendMedia(ctx, lansenger.OutgoingMedia{
			ToUserID:  reply.PlatformUID,
			FileData:  data,
			FileName:  reply.FileName,
			MediaType: imMediaFile.String(),
			IsGroup:   isGroup,
		}); err != nil {
			log.Printf("[lansenger-mgr] HandleGatewayReply SendMedia file failed: %v", err)
		}
	case gatewayReplyTypeVoice:
		data, err := base64.StdEncoding.DecodeString(reply.FileData)
		if err != nil {
			log.Printf("[lansenger-mgr] HandleGatewayReply voice decode: %v", err)
			return
		}
		if err := gw.SendMedia(ctx, lansenger.OutgoingMedia{
			ToUserID:  reply.PlatformUID,
			FileData:  data,
			FileName:  reply.FileName,
			MediaType: imMediaFile.String(),
			IsGroup:   isGroup,
		}); err != nil {
			log.Printf("[lansenger-mgr] HandleGatewayReply SendMedia voice failed: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// App integration — Wails bindings
// ---------------------------------------------------------------------------

func (a *App) ensureLansengerGateway() {
	cfg, err := a.LoadConfig()
	if err != nil {
		return
	}
	if !cfg.LansengerEnabled || strings.TrimSpace(cfg.LansengerAppID) == "" || strings.TrimSpace(cfg.LansengerAppSecret) == "" {
		if a.lansengerGateway != nil {
			a.lansengerGateway.SyncFromConfig()
		}
		return
	}
	if a.lansengerGateway == nil {
		a.lansengerGateway = newLansengerGatewayManager(a)
	}
	a.lansengerGateway.SyncFromConfig()
}

func (a *App) GetLansengerStatus() string {
	if a.lansengerGateway == nil {
		return "disconnected"
	}
	return a.lansengerGateway.Status()
}

// noteLastPrivatePeer remembers the owner private peer and persists across restarts.
func (m *lansengerGatewayManager) noteLastPrivatePeer(uid string) {
	uid = strings.TrimSpace(uid)
	if m == nil || uid == "" {
		return
	}
	m.mu.Lock()
	prev := m.lastPrivateUserID
	m.lastPrivateUserID = uid
	m.mu.Unlock()
	if prev == uid {
		return
	}
	if err := improactive.NewStore("").Patch(func(p *improactive.Peers) {
		p.LansengerPrivateUserID = uid
	}); err != nil {
		log.Printf("[lansenger-mgr] persist last private peer: %v", err)
	}
}

// LastPrivatePeerID returns the last known private-chat peer for proactive sends.
func (m *lansengerGatewayManager) LastPrivatePeerID() string {
	if m == nil {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	to := strings.TrimSpace(m.lastPrivateUserID)
	if to != "" {
		return to
	}
	var newest time.Time
	for target, route := range m.replyRoutes {
		if route.isGroup {
			continue
		}
		if newest.IsZero() || route.seenAt.After(newest) {
			newest = route.seenAt
			to = strings.TrimSpace(target)
		}
	}
	return to
}

// HasProactiveSession reports whether a private peer is known for self-notify
// (盯人 forward / scheduled delivery). Gateway "connected" alone is not enough.
func (m *lansengerGatewayManager) HasProactiveSession() bool {
	return strings.TrimSpace(m.LastPrivatePeerID()) != ""
}

// SendProactiveText pushes text to the owner's last Lansenger private session.
func (m *lansengerGatewayManager) SendProactiveText(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("empty proactive text")
	}
	if m == nil {
		return fmt.Errorf("lansenger gateway manager is nil")
	}
	to := m.LastPrivatePeerID()
	m.mu.Lock()
	gw := m.gateway
	m.mu.Unlock()
	if gw == nil {
		return fmt.Errorf("lansenger gateway not running")
	}
	if to == "" {
		return fmt.Errorf("no active lansenger private session (先用蓝信私聊机器人一次)")
	}
	return gw.SendText(context.Background(), lansenger.OutgoingText{
		ToUserID: to,
		Text:     text,
		IsGroup:  false,
	})
}

func (a *App) RestartLansenger() string {
	a.ensureLansengerGateway()
	if a.lansengerGateway == nil {
		return "disconnected"
	}
	a.lansengerGateway.Restart()
	// Group membership may change after reconnect; drop stale catalog.
	a.invalidateScheduleTargetListCache(scheduler.DeliveryChannelLansenger)
	return a.lansengerGateway.Status()
}

func (a *App) StopLansenger() {
	if a.lansengerGateway != nil {
		a.lansengerGateway.Stop()
	}
}

func (a *App) GetLansengerLocalMode() bool {
	cfg, err := a.LoadConfig()
	if err != nil {
		return true
	}
	return cfg.IsLansengerLocalMode()
}

func (a *App) SetLansengerLocalMode(enabled bool) error {
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	if !enabled && cfg.RemoteMachineID == "" {
		return fmt.Errorf("请先注册到 Hub（设置 Hub 地址并完成注册），再开启多机模式")
	}
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.SetLansengerLocal(enabled)
	}); err != nil {
		return err
	}

	if a.lansengerGateway != nil {
		a.lansengerGateway.resetLocalHandler()
	}

	if !enabled {
		hubClient := a.hubClient()
		if hubClient != nil && hubClient.IsConnected() {
			hubClient.SendIMGatewayClaim(imGatewayPlatformLansenger)
			if a.lansengerGateway != nil {
				a.lansengerGateway.mu.Lock()
				a.lansengerGateway.hubClaimSent = true
				a.lansengerGateway.mu.Unlock()
			}
		}
	}
	return nil
}

// LansengerGroupListEntry is one row in the settings group dialog.
type LansengerGroupListEntry struct {
	GroupID      string `json:"group_id"`
	Name         string `json:"name"`
	AvatarURL    string `json:"avatar_url,omitempty"`
	Description  string `json:"description,omitempty"`
	OwnerID      string `json:"owner_id,omitempty"`
	OwnerName    string `json:"owner_name,omitempty"`
	State        int    `json:"state"`
	TotalMembers int    `json:"total_members"`
	MaxMembers   int    `json:"max_members,omitempty"`
	IsPublic     bool   `json:"is_public,omitempty"`
	// Ignored means the bot will not answer messages from this group (open policy denylist).
	Ignored bool `json:"ignored"`
	// Allowed means the group is on the allowlist (allowlist policy only).
	Allowed bool `json:"allowed"`
	// Orphan means this row comes only from the local ignore/allow list (not the
	// platform group fetch). Used by the UI to drop the row after "Resume".
	Orphan bool `json:"orphan,omitempty"`
	// FileMaxBytes is the local chat-history attachment cap. Zero means unlimited.
	FileMaxBytes int64 `json:"file_max_bytes"`
}

// LansengerGroupListResult is the Wails payload for ListLansengerGroups.
type LansengerGroupListResult struct {
	Total  int                       `json:"total"`
	Groups []LansengerGroupListEntry `json:"groups"`
}

// ListLansengerGroups queries the Lansenger Open Platform for groups the bot
// has joined (GET /v2/groups/fetch + per-group info). Works with credentials
// even when the WebSocket gateway is temporarily disconnected.
func (a *App) ListLansengerGroups() (*LansengerGroupListResult, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	appID := strings.TrimSpace(cfg.LansengerAppID)
	appSecret := strings.TrimSpace(cfg.LansengerAppSecret)
	if appID == "" || appSecret == "" {
		return nil, fmt.Errorf("请先填写蓝信 App ID 和 App Secret")
	}
	apiURL := strings.TrimSpace(cfg.LansengerApiGatewayURL())
	if apiURL == "" {
		return nil, fmt.Errorf("请先填写蓝信网关地址")
	}

	// Prefer the running gateway so we can reuse its token cache.
	var gw *lansenger.Gateway
	if a.lansengerGateway != nil {
		a.lansengerGateway.mu.Lock()
		gw = a.lansengerGateway.gateway
		a.lansengerGateway.mu.Unlock()
	}
	// Fall back to a short-lived client when the WS manager is not up yet.
	if gw == nil {
		gw = lansenger.NewGateway(lansenger.Config{
			AppID:            appID,
			AppSecret:        appSecret,
			ApiGatewayURL:    apiURL,
			WebSocketBaseURL: strings.TrimSpace(cfg.LansengerWebSocketGatewayURL()),
		}, nil)
	}

	// Parallel detail fetch (up to 300 groups × 8 workers) usually finishes well
	// under a minute; keep a hard ceiling so the UI never hangs indefinitely.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	list, err := gw.ListJoinedGroups(ctx)
	if err != nil {
		var apiErr *lansenger.APIError
		if errors.As(err, &apiErr) && apiErr.Code == 10005 {
			return nil, fmt.Errorf("蓝信接口无权限查询群列表 (errCode=10005)。请在开放平台开通 /v2/groups/fetch 权限，或联系管理员")
		}
		return nil, fmt.Errorf("查询蓝信群列表失败: %w", err)
	}
	if list == nil {
		return &LansengerGroupListResult{Groups: []LansengerGroupListEntry{}}, nil
	}
	entries := make([]LansengerGroupListEntry, 0, len(list.Groups))
	seen := make(map[string]struct{}, len(list.Groups))
	for _, g := range list.Groups {
		id := strings.TrimSpace(g.GroupID)
		if id != "" {
			seen[id] = struct{}{}
		}
		entries = append(entries, LansengerGroupListEntry{
			GroupID:      g.GroupID,
			Name:         g.Name,
			AvatarURL:    g.AvatarURL,
			Description:  g.Description,
			OwnerID:      g.OwnerID,
			OwnerName:    g.OwnerName,
			State:        g.State,
			TotalMembers: g.TotalMembers,
			MaxMembers:   g.MaxMembers,
			IsPublic:     g.IsPublic,
			Ignored:      cfg.IsLansengerGroupIgnored(g.GroupID),
			Allowed:      cfg.IsLansengerGroupAllowed(g.GroupID),
			FileMaxBytes: cfg.LansengerGroupFileLimit(g.GroupID),
		})
	}
	// Surface ignore/allowlist-only entries that are no longer returned by the
	// platform so the user can still re-enable or un-allow them.
	for _, id := range cfg.LansengerIgnoredGroupIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		entries = append(entries, LansengerGroupListEntry{
			GroupID:      id,
			Name:         id,
			Ignored:      true,
			Allowed:      cfg.IsLansengerGroupAllowed(id),
			FileMaxBytes: cfg.LansengerGroupFileLimit(id),
			Orphan:       true,
		})
		seen[id] = struct{}{}
	}
	for _, id := range cfg.LansengerAllowedGroupIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		entries = append(entries, LansengerGroupListEntry{
			GroupID:      id,
			Name:         id,
			Allowed:      true,
			FileMaxBytes: cfg.LansengerGroupFileLimit(id),
			Orphan:       true,
		})
	}
	return &LansengerGroupListResult{Total: list.Total, Groups: entries}, nil
}

// GetLansengerIgnoredGroups returns group IDs the bot will not respond to.
func (a *App) GetLansengerIgnoredGroups() []string {
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(cfg.LansengerIgnoredGroupIDs))
	for _, id := range cfg.LansengerIgnoredGroupIDs {
		if id = strings.TrimSpace(id); id != "" {
			out = append(out, id)
		}
	}
	return out
}

// SetLansengerGroupIgnored marks or unmarks a group so the bot does not respond
// there. The bot is not removed from the Lansenger group on the server.
func (a *App) SetLansengerGroupIgnored(groupID string, ignored bool) error {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return fmt.Errorf("group id is required")
	}
	return a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.SetLansengerGroupIgnored(groupID, ignored)
	})
}

// SetLansengerGroupAllowed marks or unmarks a group on the allowlist (allowlist policy).
func (a *App) SetLansengerGroupAllowed(groupID string, allowed bool) error {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return fmt.Errorf("group id is required")
	}
	return a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.SetLansengerGroupAllowed(groupID, allowed)
	})
}

// SetLansengerGroupFileMaxBytes updates the local history attachment cap for a group.
func (a *App) SetLansengerGroupFileMaxBytes(groupID string, maxBytes int64) error {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return fmt.Errorf("group id is required")
	}
	if maxBytes < 0 {
		return fmt.Errorf("file size limit cannot be negative")
	}
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.SetLansengerGroupFileLimit(groupID, maxBytes)
	}); err != nil {
		return err
	}
	if a.lansengerGateway != nil {
		a.lansengerGateway.updateGroupFileLimit(groupID, maxBytes)
	}
	return nil
}
