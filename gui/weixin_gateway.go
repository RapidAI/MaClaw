package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/i18n"
	"github.com/RapidAI/CodeClaw/corelib/textutil"
	"github.com/RapidAI/CodeClaw/corelib/tts"
	"github.com/RapidAI/CodeClaw/corelib/weixin"
)

// weixinGatewayManager manages the client-side WeChat gateway.
// Supports two modes:
//   - Local / 单机 mode (default): routes messages directly to the
//     local MaClaw LLM agent loop, bypassing Hub entirely.
//   - Hub / 多机 mode (WeixinLocalMode=false): forwards messages to Hub
//     via im.gateway_message, receives replies via im.gateway_reply.
type weixinGatewayManager struct {
	app       *App
	mu        sync.Mutex
	gateway   *weixin.Gateway
	status    gatewayConnectionStatus
	lastToken string

	// localHandler is a fully-wired IMMessageHandler for local mode.
	// Created lazily on first local-mode message.
	localHandler *IMMessageHandler
}

func newWeixinGatewayManager(app *App) *weixinGatewayManager {
	return &weixinGatewayManager{
		app:    app,
		status: gatewayConnectionStatusDisconnected,
	}
}

// SyncFromConfig reads the current AppConfig and starts or stops the gateway.
func (m *weixinGatewayManager) SyncFromConfig() {
	wl := weixin.GetWxLog()
	m.app.logMemorySnapshot("weixinGateway:sync-start")
	cfg, err := m.app.LoadConfig()
	if err != nil {
		wl.Log("mgr.sync", "---", "-", "ERR LoadConfig: %v", err)
		return
	}
	wl.Log("mgr.sync", "---", "-", "config enabled=%v token_len=%d local=%v base_url=%s cdn_url_set=%v", cfg.WeixinEnabled, len(cfg.WeixinToken), cfg.IsWeixinLocalMode(), cfg.WeixinBaseURL, cfg.WeixinCDNURL != "")

	m.mu.Lock()
	if !cfg.WeixinEnabled || cfg.WeixinToken == "" {
		gw := m.gateway
		if gw != nil {
			wl.Log("mgr.sync", "---", "-", "stopping gateway because enabled=%v token_len=%d", cfg.WeixinEnabled, len(cfg.WeixinToken))
			m.gateway = nil
			m.status = gatewayConnectionStatusDisconnected
			m.mu.Unlock()
			_ = gw.Stop()
		} else {
			m.mu.Unlock()
		}
		// Always notify Hub to release gateway claim, even if local gateway
		// was already nil — Hub may still hold a stale claim from a previous run.
		if hubClient := m.app.hubClient(); hubClient != nil && hubClient.IsConnected() {
			_ = hubClient.SendIMGatewayUnclaim(imGatewayPlatformWeixin)
			log.Printf("[weixin-mgr] sent gateway unclaim to hub")
		}
		if gw != nil {
			m.emitStatusEvent()
		}
		return
	}

	if m.gateway != nil && m.lastToken == cfg.WeixinToken {
		wl.Log("mgr.sync", "---", "-", "gateway already running token_len=%d", len(cfg.WeixinToken))
		m.mu.Unlock()
		return
	}

	oldGw := m.gateway
	tokenChanged := m.lastToken != "" && m.lastToken != cfg.WeixinToken
	// Keep old gateway in place until new one is ready ->avoids a nil window
	// where HandleGatewayReply would silently drop messages.
	m.mu.Unlock()

	if oldGw != nil {
		wl.Log("mgr.sync", "---", "-", "stopping old gateway before restart token_changed=%v", tokenChanged)
		_ = oldGw.Stop()
	}

	baseURL := cfg.WeixinBaseURL
	if baseURL == "" {
		baseURL = weixin.DefaultBaseURL
	}
	cdnURL := cfg.WeixinCDNURL
	if cdnURL == "" {
		cdnURL = weixin.DefaultCDNBaseURL
	}

	gw := weixin.NewGateway(weixin.Config{
		Token:     cfg.WeixinToken,
		BaseURL:   baseURL,
		CDNURL:    cdnURL,
		AccountID: cfg.WeixinAccountID,
	}, m.onIncomingMessage)
	gw.SetStatusCallback(m.onStatusChange)

	m.mu.Lock()
	m.gateway = gw
	m.lastToken = cfg.WeixinToken
	m.mu.Unlock()

	if err := gw.Start(context.Background()); err != nil {
		wl.Log("mgr.sync", "---", "-", "ERR start failed: %v", err)
		log.Printf("[weixin-mgr] start failed: %v", err)
		m.mu.Lock()
		m.status = gatewayConnectionStatusError
		m.mu.Unlock()
		m.emitStatusEvent()
		return
	}
	wl.Log("mgr.sync", "---", "-", "gateway start requested token_len=%d base_url=%s cdn_url=%s", len(cfg.WeixinToken), baseURL, cdnURL)
}

// Stop shuts down the gateway.
func (m *weixinGatewayManager) Stop() {
	wl := weixin.GetWxLog()
	wl.Log("mgr.stop", "---", "-", "begin")
	m.mu.Lock()
	gw := m.gateway
	m.gateway = nil
	m.status = gatewayConnectionStatusDisconnected
	m.lastToken = ""
	lh := m.localHandler
	m.localHandler = nil
	m.mu.Unlock()
	if lh != nil {
		lh.memory.Stop()
	}
	if gw != nil {
		_ = gw.Stop()
	}
	wl.Log("mgr.stop", "---", "-", "done had_gateway=%v had_local_handler=%v", gw != nil, lh != nil)
}

// Status returns the current connection status.
func (m *weixinGatewayManager) Status() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status.String()
}

func (m *weixinGatewayManager) onStatusChange(status string) {
	wl := weixin.GetWxLog()
	wl.Log("mgr.status", "---", "-", "status=%s", status)

	m.mu.Lock()
	normalized := normalizeGatewayConnectionStatus(status)
	m.status = normalized
	m.mu.Unlock()
	m.emitStatusEvent()

	if normalized == gatewayConnectionStatusConnected {
		// In local mode, skip Hub gateway claim.
		if cfg, err := m.app.LoadConfig(); err == nil && cfg.IsWeixinLocalMode() {
			return
		}
		hubClient := m.app.hubClient()
		if hubClient != nil && hubClient.IsConnected() {
			hubClient.SendIMGatewayClaim(imGatewayPlatformWeixin)
		}
	}

	if normalized == gatewayConnectionStatusSessionExpired {
		wl.Log("mgr.status", "---", "-", "session expired ->tearing down gateway and releasing hub claim")
		log.Printf("[weixin-mgr] session expired, tearing down gateway")

		// Release Hub gateway claim so Hub doesn't route replies to a dead gateway.
		if hubClient := m.app.hubClient(); hubClient != nil && hubClient.IsConnected() {
			_ = hubClient.SendIMGatewayUnclaim(imGatewayPlatformWeixin)
		}

		// Tear down the gateway instance so HandleGatewayReply won't try to use it.
		// NOTE: we must NOT call gw.Stop() synchronously here because this
		// callback runs inside pollLoop ->emitStatus, and Stop() waits for
		// pollLoop to finish ->that would deadlock. Instead, nil out the
		// gateway reference (so no new messages are dispatched) and let
		// pollLoop's natural return + wg.Done() handle the cleanup.
		m.mu.Lock()
		gw := m.gateway
		m.gateway = nil
		m.lastToken = ""
		m.status = gatewayConnectionStatusDisconnected
		lh := m.localHandler
		m.localHandler = nil
		m.mu.Unlock()
		if lh != nil {
			lh.memory.Stop()
		}
		// Async stop: pollLoop is about to return (emitStatus is the last
		// call before return), so Stop() will complete quickly once we
		// release this callback.
		if gw != nil {
			go func() {
				_ = gw.Stop()
				wl.Log("mgr.status", "---", "-", "gateway Stop() completed after session_expired")
			}()
		}
		m.emitStatusEvent()
	}
}

func (m *weixinGatewayManager) emitStatusEvent() {
	m.app.emitEvent("weixin-status-changed", m.Status())
}

func modeLabel(isLocal bool) string {
	if isLocal {
		return "local"
	}
	return "hub"
}

// resetLocalHandler tears down the cached local IMMessageHandler so it will
// be recreated on the next local-mode message. Safe to call when not in local mode.
func (m *weixinGatewayManager) resetLocalHandler() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.localHandler != nil {
		m.localHandler.memory.Stop()
		m.localHandler = nil
	}
}

// onIncomingMessage routes WeChat messages based on config:
//   - Local mode: directly invokes the local MaClaw LLM agent loop
//   - Hub mode: forwards to Hub via im.gateway_message
//   - Hub mode fallback: if Hub is unavailable, notify user and fall back to local
func (m *weixinGatewayManager) onIncomingMessage(msg weixin.IncomingMessage) {
	wl := weixin.GetWxLog()
	if isPassthroughSlashText(msg.Text) {
		wl.Log("mgr.incoming", "---", msg.FromUserID, "ROUTE -> passthrough")
		m.handleLocalMessage(msg)
		return
	}
	cfg, err := m.app.LoadConfig()
	if err != nil {
		wl.Log("mgr.incoming", "IN", msg.FromUserID, "ERR LoadConfig: %v", err)
		log.Printf("[weixin-mgr] LoadConfig error: %v", err)
		return
	}

	isLocal := cfg.IsWeixinLocalMode()
	localModePtr := cfg.WeixinLocalMode // nil = never set (default local)
	hubClient := m.app.hubClient()
	hubNil := hubClient == nil
	hubConn := !hubNil && hubClient.IsConnected()
	wl.Log("mgr.incoming", "IN", msg.FromUserID, "mode=%s hub_nil=%v hub_conn=%v text_len=%d media=%s",
		modeLabel(isLocal), hubNil, hubConn, len(msg.Text), msg.MediaType)
	log.Printf("[weixin-mgr] onIncomingMessage: user=%s local_mode=%v local_mode_ptr=%v hub_nil=%v hub_connected=%v",
		msg.FromUserID, isLocal, localModePtr, hubNil, hubConn)

	if isLocal {
		wl.Log("mgr.incoming", "---", msg.FromUserID, "ROUTE → local handler")
		m.handleLocalMessage(msg)
		return
	}

	// Hub mode — try forwarding; fall back to local if Hub unavailable.
	if hubNil || !hubConn {
		wl.Log("mgr.incoming", "---", msg.FromUserID, "ROUTE → local FALLBACK (hub unavailable)")
		log.Printf("[weixin-mgr] Hub mode but Hub unavailable, falling back to local: user=%s", msg.FromUserID)
		m.notifyHubUnavailable(msg)
		m.handleLocalMessage(msg)
		return
	}

	wl.Log("mgr.incoming", "---", msg.FromUserID, "ROUTE → hub forward")
	m.forwardToHub(msg)
}

// notifyHubUnavailable sends a one-time warning to the WeChat user when Hub
// mode is configured but Hub is not connected. The message is rate-limited
// to avoid spamming on every incoming message.
func (m *weixinGatewayManager) notifyHubUnavailable(msg weixin.IncomingMessage) {
	m.mu.Lock()
	gw := m.gateway
	m.mu.Unlock()
	if gw == nil {
		return
	}
	_ = gw.SendText(context.Background(), weixin.OutgoingText{
		ToUserID:     msg.FromUserID,
		Text:         "Hub is not connected; message fell back to local handling.",
		ContextToken: msg.ContextToken,
	})
}

// forwardToHub sends the message to Hub via im.gateway_message (original behaviour).
func (m *weixinGatewayManager) forwardToHub(msg weixin.IncomingMessage) {
	wl := weixin.GetWxLog()
	hubClient := m.app.hubClient()
	if hubClient == nil || !hubClient.IsConnected() {
		wl.Log("mgr.forward", "OUT", msg.FromUserID, "ERR hub disconnected, fallback to local")
		log.Printf("[weixin-mgr] forwardToHub FAILED: hub_nil=%v user=%s", hubClient == nil, msg.FromUserID)
		// Fall back to local processing instead of silently dropping.
		m.notifyHubUnavailable(msg)
		m.handleLocalMessage(msg)
		return
	}

	msgType := "text"
	if msg.MediaType != "" {
		msgType = msg.MediaType
	}

	payload := map[string]any{
		"platform_uid": msg.FromUserID,
		"text":         msg.Text,
		"message_type": msgType,
	}

	// Include media as attachments (matches Hub's MessageAttachment schema)
	if att := buildMediaAttachment(msg.MediaType, msg.MediaData, msg.MediaName, ""); att != nil {
		payload["attachments"] = []map[string]any{att}
	}

	// Include context_token so Hub can pass it back in replies
	if msg.ContextToken != "" {
		payload["context_token"] = msg.ContextToken
	}

	if err := hubClient.SendIMGatewayMessage(imGatewayPlatformWeixin, payload); err != nil {
		wl.Log("mgr.forward", "OUT", msg.FromUserID, "ERR SendIMGatewayMessage: %v, fallback to local", err)
		log.Printf("[weixin-mgr] forwardToHub SendIMGatewayMessage error: %v, falling back to local", err)
		m.handleLocalMessage(msg)
	} else {
		wl.Log("mgr.forward", "OUT", msg.FromUserID, "OK sent to hub, has_ctx_token=%v", msg.ContextToken != "")
		log.Printf("[weixin-mgr] forwardToHub OK: user=%s text_len=%d", msg.FromUserID, len([]rune(msg.Text)))
	}
}

// ---------------------------------------------------------------------------
// Local mode — direct LLM agent loop
// ---------------------------------------------------------------------------

// ensureLocalHandler lazily creates and wires an IMMessageHandler for local mode.
func (m *weixinGatewayManager) ensureLocalHandler() *IMMessageHandler {
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
	// Wire the same subsystems that createAndWireHubClient does.
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
		emb := a.memoryStore.Embedder()
		if emb != nil && !embedding.IsNoop(emb) {
			h.toolBuilder.SetEmbedder(emb)
			// Wire embedder into interrupt handler for semantic relevance.
			if h.interruptHandler != nil {
				h.interruptHandler.SetEmbedder(emb)
			}
		}
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
	log.Printf("[weixin-mgr] local IMMessageHandler created")

	// Wire the interrupt handler to the gateway so incoming messages during
	// an active agent loop can trigger cancel/merge/status without waiting
	// for the per-user lock.
	if m.gateway != nil && h.interruptHandler != nil {
		m.gateway.SetInterruptHandler(h.interruptHandler)
	}

	return h
}

// handleLocalMessage processes a WeChat message through the local agent loop
// and sends the response back via the WeChat API.
func (m *weixinGatewayManager) handleLocalMessage(msg weixin.IncomingMessage) {
	wl := weixin.GetWxLog()
	if resp, handled := m.app.TryHandlePassthroughSlashCommandWithSource(msg.Text, "weixin:"+msg.FromUserID); handled {
		m.mu.Lock()
		gw := m.gateway
		m.mu.Unlock()
		if gw != nil {
			contextToken := msg.ContextToken
			if contextToken == "" {
				contextToken = gw.GetContextToken(msg.FromUserID)
			}
			reply := resp.Text
			if reply == "" {
				reply = resp.Error
			}
			if reply == "" {
				reply = "(no output)"
			}
			_ = gw.SendText(context.Background(), weixin.OutgoingText{
				ToUserID:     msg.FromUserID,
				Text:         reply,
				ContextToken: contextToken,
			})
		}
		return
	}
	// Check LLM is configured before entering the agent loop.
	if !m.app.isMaclawLLMConfigured() {
		wl.Log("mgr.local", "---", msg.FromUserID, "ERR LLM not configured")
		m.mu.Lock()
		gw := m.gateway
		m.mu.Unlock()
		if gw != nil {
			_ = gw.SendText(context.Background(), weixin.OutgoingText{
				ToUserID:     msg.FromUserID,
				Text:         i18n.T(i18n.MsgLLMNotConfigured, "zh"),
				ContextToken: msg.ContextToken,
			})
		}
		return
	}

	handler := m.ensureLocalHandler()

	m.mu.Lock()
	gw := m.gateway
	m.mu.Unlock()
	if gw == nil {
		wl.Log("mgr.local", "---", msg.FromUserID, "ERR gateway is nil")
		return
	}

	contextToken := msg.ContextToken
	if contextToken == "" {
		contextToken = gw.GetContextToken(msg.FromUserID)
	}
	wl.Log("mgr.local", "---", msg.FromUserID, "ctx_token=%v text_len=%d media=%s", contextToken != "", len(msg.Text), msg.MediaType)
	if m.echoInboundVoiceForDiagnostics(context.Background(), gw, msg, contextToken) {
		return
	}

	// Build the user message; pass images as multimodal attachments so the
	// LLM can actually "see" them, instead of just a file-path text prefix.
	text := msg.Text
	var attachments []MessageAttachment
	if msg.MediaType != "" && len(msg.MediaData) > 0 {
		if normalizeIMMediaKind(msg.MediaType).IsImage() {
			// Pass image as a proper attachment for multimodal vision.
			attachments = append(attachments, buildLocalImageAttachment(msg.MediaData, msg.MediaName, ""))
		} else {
			// Non-image media: save to temp file and prepend path as text.
			mediaPath, err := m.saveMediaToTemp(msg)
			if err != nil {
				log.Printf("[weixin-mgr] save media error: %v", err)
			} else {
				prefix := "[收到" + mediaLabel(msg.MediaType) + ": " + mediaPath + "]\n"
				text = prefix + text
			}
		}
	}

	if text == "" && len(attachments) == 0 {
		wl.Log("mgr.local", "---", msg.FromUserID, "SKIP empty text after media processing")
		return
	}

	// Progress callback -> send intermediate status to the WeChat user.
	// Use a rate limiter to avoid flooding: at most one progress message per 5s.
	progressFilter := newIMProgressVisibilityFilter(m.app)
	var lastProgress time.Time
	var lastProgressText string
	onProgress := func(progressText string) {
		if !progressFilter.ShouldSendProgress(progressText) {
			return
		}
		now := time.Now()
		if now.Sub(lastProgress) < 5*time.Second {
			return
		}
		// Dedup: suppress identical consecutive progress messages.
		stripped := textutil.StripMarkdown(progressText)
		if stripped == lastProgressText {
			return
		}
		lastProgress = now
		lastProgressText = stripped
		_ = gw.SendText(context.Background(), weixin.OutgoingText{
			ToUserID:     msg.FromUserID,
			Text:         i18n.T(i18n.MsgProgressPrefix, appUILang(m.app)) + textutil.StripMarkdown(progressText),
			ContextToken: contextToken,
		})
	}

	wl.Log("mgr.local", "---", msg.FromUserID, "calling HandleIMMessageWithProgress text_len=%d attachments=%d", len(text), len(attachments))
	resp := handler.HandleIMMessageWithProgress(IMUserMessage{
		UserID:      msg.FromUserID,
		Platform:    "weixin_local",
		MessageType: msg.MediaType,
		Text:        text,
		Lang:        appUILang(m.app),
		Attachments: attachments,
	}, onProgress)

	if resp == nil {
		wl.Log("mgr.local", "---", msg.FromUserID, "agent returned nil response")
		return
	}
	if resp.Deferred {
		wl.Log("mgr.local", "---", msg.FromUserID, "media buffered, waiting for user intent")
		return
	}

	wl.Log("mgr.local", "OUT", msg.FromUserID, "agent OK text_len=%d err=%q", len(resp.Text), resp.Error)
	m.sendAgentResponse(gw, msg.FromUserID, contextToken, resp)
}

func (m *weixinGatewayManager) echoInboundVoiceForDiagnostics(ctx context.Context, gw *weixin.Gateway, msg weixin.IncomingMessage, contextToken string) bool {
	mode := strings.TrimSpace(os.Getenv("MACLAW_WEIXIN_ECHO_INBOUND_VOICE"))
	if mode == "" || mode == "0" || strings.EqualFold(mode, "false") || strings.EqualFold(mode, "off") {
		return false
	}
	if msg.MediaType != "voice" || len(msg.MediaData) == 0 {
		return false
	}
	name := msg.MediaName
	if name == "" {
		name = "inbound.silk"
	}
	err := gw.SendMedia(ctx, weixin.OutgoingMedia{
		ToUserID:     msg.FromUserID,
		ContextToken: contextToken,
		FileData:     msg.MediaData,
		FileName:     name,
		MediaType:    "voice",
	})
	if err != nil {
		log.Printf("[weixin-mgr] echo inbound voice failed (to=%s size=%d): %v", msg.FromUserID, len(msg.MediaData), err)
		weixin.GetWxLog().Log("mgr.local", "OUT", msg.FromUserID, "ERR echo-inbound-voice name=%s size=%d err=%v", name, len(msg.MediaData), err)
	} else {
		log.Printf("[weixin-mgr] echo inbound voice OK (to=%s size=%d name=%s)", msg.FromUserID, len(msg.MediaData), name)
		weixin.GetWxLog().Log("mgr.local", "OUT", msg.FromUserID, "OK echo-inbound-voice name=%s size=%d mode=%s", name, len(msg.MediaData), mode)
	}
	return strings.EqualFold(mode, "only")
}

// sendAgentResponse dispatches all parts of an IMAgentResponse to the WeChat user.
// reMarkdownImage matches ![alt](url) patterns in LLM response text.
var reMarkdownImage = regexp.MustCompile(`!\[[^\]]*\]\(([^)]+)\)`)

func (m *weixinGatewayManager) sendAgentResponse(gw *weixin.Gateway, toUserID, contextToken string, resp *IMAgentResponse) {
	ctx := context.Background()

	// Extract markdown image URLs from text before stripping markdown.
	var imageURLs []string
	var imageDataURIs []string // data:image/...;base64,xxx
	if resp.Text != "" {
		matches := reMarkdownImage.FindAllStringSubmatch(resp.Text, 10)
		for _, match := range matches {
			if len(match) <= 1 {
				continue
			}
			src := match[1]
			if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
				imageURLs = append(imageURLs, src)
			} else if strings.HasPrefix(src, "data:image/") {
				imageDataURIs = append(imageDataURIs, src)
			}
		}
	}

	voiceSent := false
	if resp.VoiceData != "" {
		nativeVoiceAccepted := m.sendVoiceResponse(ctx, gw, toUserID, contextToken, resp)
		playableFileSent := m.sendVoiceFileFallback(ctx, gw, toUserID, contextToken, resp)
		voiceSent = playableFileSent
		weixin.GetWxLog().Log("mgr.local", "OUT", toUserID, "voice_delivery_summary native_api_accepted=%v native_green_bubble_expected=false playable_file_sent=%v user_visible_audio_file=%v", nativeVoiceAccepted, playableFileSent, playableFileSent)
	}

	if resp.Text != "" {
		text := textutil.StripMarkdown(resp.Text)
		if voiceSent {
			text = "\u97f3\u9891\u6587\u4ef6\u5df2\u53d1\u9001\uff1b\u5fae\u4fe1\u539f\u751f\u8bed\u97f3\u6c14\u6ce1\u4ecd\u5728\u8bca\u65ad\uff0c\u6587\u5b57\u7248\u5982\u4e0b\uff1a\n\n" + textutil.StripMarkdown(resp.Text)
		}
		if err := gw.SendText(ctx, weixin.OutgoingText{
			ToUserID:     toUserID,
			Text:         text,
			ContextToken: contextToken,
		}); err != nil {
			log.Printf("[weixin-mgr] local SendText error (to=%s voice_sent=%v): %v", toUserID, voiceSent, err)
		} else if voiceSent {
			log.Printf("[weixin-mgr] sent text fallback after voice (to=%s text_len=%d)", toUserID, len([]rune(resp.Text)))
		}
	}

	// Send error as text if no text or voice was sent
	if resp.Error != "" && resp.Text == "" && !voiceSent {
		_ = gw.SendText(ctx, weixin.OutgoingText{
			ToUserID:     toUserID,
			Text:         "" + textutil.StripMarkdown(resp.Error),
			ContextToken: contextToken,
		})
	}

	// Send image if present (base64-encoded screenshot or generated image)
	if resp.ImageKey != "" {
		imgData, err := base64.StdEncoding.DecodeString(resp.ImageKey)
		if err == nil && len(imgData) > 0 {
			if err := gw.SendMedia(ctx, weixin.OutgoingMedia{
				ToUserID:     toUserID,
				ContextToken: contextToken,
				FileData:     imgData,
				MediaType:    "image",
			}); err != nil {
				log.Printf("[weixin-mgr] SendMedia screenshot failed (to=%s size=%d): %v", toUserID, len(imgData), err)
			} else {
				log.Printf("[weixin-mgr] SendMedia screenshot OK (to=%s size=%d)", toUserID, len(imgData))
			}
		}
	}

	// Download and send markdown images extracted from LLM response text.
	for _, imgURL := range imageURLs {
		imgData, err := downloadImageURL(ctx, imgURL)
		if err != nil {
			log.Printf("[weixin-mgr] download markdown image failed (url=%s): %v", imgURL, err)
			continue
		}
		if err := gw.SendMedia(ctx, weixin.OutgoingMedia{
			ToUserID:     toUserID,
			ContextToken: contextToken,
			FileData:     imgData,
			MediaType:    "image",
		}); err != nil {
			log.Printf("[weixin-mgr] send markdown image failed (url=%s): %v", imgURL, err)
		}
	}

	// Send inline data URI images (data:image/png;base64,...).
	for _, dataURI := range imageDataURIs {
		// Format: data:image/png;base64,iVBOR...
		if idx := strings.Index(dataURI, ";base64,"); idx > 0 {
			b64 := dataURI[idx+8:]
			imgData, err := base64.StdEncoding.DecodeString(b64)
			if err == nil && len(imgData) > 0 {
				_ = gw.SendMedia(ctx, weixin.OutgoingMedia{
					ToUserID:     toUserID,
					ContextToken: contextToken,
					FileData:     imgData,
					MediaType:    "image",
				})
			}
		}
	}

	// Send file if present (base64-encoded)
	if resp.FileData != "" {
		fileBytes, err := base64.StdEncoding.DecodeString(resp.FileData)
		if err == nil && len(fileBytes) > 0 {
			_ = gw.SendMedia(ctx, weixin.OutgoingMedia{
				ToUserID:     toUserID,
				ContextToken: contextToken,
				FileData:     fileBytes,
				FileName:     resp.FileName,
				MediaType:    "file",
			})
		}
	}

	// Send local file(s) if present
	m.sendLocalFiles(gw, toUserID, contextToken, resp)
}

func (m *weixinGatewayManager) sendVoiceResponse(ctx context.Context, gw *weixin.Gateway, toUserID, contextToken string, resp *IMAgentResponse) bool {
	voiceFileName := resp.VoiceFileName
	if voiceFileName == "" {
		voiceFileName = "voice.wav"
	}
	voiceBytes, err := base64.StdEncoding.DecodeString(resp.VoiceData)
	if err != nil || len(voiceBytes) == 0 {
		log.Printf("[weixin-mgr] decode voice data failed (to=%s): %v", toUserID, err)
		return false
	}
	if err := gw.SendMedia(ctx, weixin.OutgoingMedia{
		ToUserID:     toUserID,
		ContextToken: contextToken,
		FileData:     voiceBytes,
		FileName:     voiceFileName,
		MediaType:    "voice",
	}); err != nil {
		log.Printf("[weixin-mgr] SendMedia voice failed (to=%s size=%d): %v", toUserID, len(voiceBytes), err)
		return false
	}
	log.Printf("[weixin-mgr] SendMedia voice OK (to=%s size=%d name=%s)", toUserID, len(voiceBytes), voiceFileName)
	weixin.GetWxLog().Log("mgr.local", "OUT", toUserID, "OK SendMedia(voice-native) variant=inbound_shape name=%s size=%d", voiceFileName, len(voiceBytes))
	variants := weixinNativeVoiceExperimentVariants()
	if len(variants) == 0 {
		weixin.GetWxLog().Log("mgr.local", "OUT", toUserID, "SKIP voice-native-experiments reason=disabled env=MACLAW_WEIXIN_VOICE_EXPERIMENTS")
	}
	for _, variant := range variants {
		if err := gw.SendMedia(ctx, weixin.OutgoingMedia{
			ToUserID:     toUserID,
			ContextToken: contextToken,
			FileData:     voiceBytes,
			FileName:     voiceFileName,
			MediaType:    "voice",
			VoiceVariant: variant,
		}); err != nil {
			log.Printf("[weixin-mgr] SendMedia voice experiment failed (to=%s size=%d variant=%s): %v", toUserID, len(voiceBytes), variant, err)
			weixin.GetWxLog().Log("mgr.local", "OUT", toUserID, "ERR SendMedia(voice-native-experiment) variant=%s name=%s size=%d err=%v", variant, voiceFileName, len(voiceBytes), err)
		} else {
			log.Printf("[weixin-mgr] SendMedia voice experiment OK (to=%s size=%d name=%s variant=%s)", toUserID, len(voiceBytes), voiceFileName, variant)
			weixin.GetWxLog().Log("mgr.local", "OUT", toUserID, "OK SendMedia(voice-native-experiment) variant=%s name=%s size=%d", variant, voiceFileName, len(voiceBytes))
		}
	}
	return true
}

func weixinNativeVoiceExperimentVariants() []string {
	raw := strings.TrimSpace(os.Getenv("MACLAW_WEIXIN_VOICE_EXPERIMENTS"))
	if raw == "" || raw == "0" || strings.EqualFold(raw, "false") || strings.EqualFold(raw, "off") {
		return nil
	}
	all := []string{"integrity_encrypt1", "upload_param_encrypt0", "raw_aes_encrypt0", "silk_encode6_raw_aes_encrypt0"}
	if raw == "1" || strings.EqualFold(raw, "true") || strings.EqualFold(raw, "all") {
		return all
	}
	allowed := make(map[string]bool, len(all))
	for _, variant := range all {
		allowed[variant] = true
	}
	variants := make([]string, 0, len(all))
	seen := make(map[string]bool, len(all))
	for _, part := range strings.Split(raw, ",") {
		variant := strings.TrimSpace(part)
		if !allowed[variant] || seen[variant] {
			continue
		}
		seen[variant] = true
		variants = append(variants, variant)
	}
	return variants
}

func (m *weixinGatewayManager) sendVoiceFileFallback(ctx context.Context, gw *weixin.Gateway, toUserID, contextToken string, resp *IMAgentResponse) bool {
	voiceFileName := resp.VoiceFileName
	if voiceFileName == "" {
		voiceFileName = "voice.wav"
	}
	voiceBytes, err := base64.StdEncoding.DecodeString(resp.VoiceData)
	if err != nil || len(voiceBytes) == 0 {
		log.Printf("[weixin-mgr] decode voice fallback data failed (to=%s): %v", toUserID, err)
		return false
	}
	fallback, err := prepareWeixinPlayableVoiceFile(ctx, voiceFileName, voiceBytes)
	if err != nil {
		log.Printf("[weixin-mgr] prepare voice playable fallback failed (to=%s name=%s size=%d): %v", toUserID, voiceFileName, len(voiceBytes), err)
		weixin.GetWxLog().Log("mgr.local", "OUT", toUserID, "ERR voice playable fallback prepare name=%s size=%d err=%v", voiceFileName, len(voiceBytes), err)
		return false
	}
	m.saveWeixinVoicePlayableDebug(fallback)
	if err := gw.SendMedia(ctx, weixin.OutgoingMedia{
		ToUserID:     toUserID,
		ContextToken: contextToken,
		FileData:     fallback.data,
		FileName:     fallback.name,
		MediaType:    "file",
	}); err != nil {
		log.Printf("[weixin-mgr] SendMedia voice file fallback failed (to=%s size=%d name=%s source=%s): %v", toUserID, len(fallback.data), fallback.name, voiceFileName, err)
		weixin.GetWxLog().Log("mgr.local", "OUT", toUserID, "ERR SendMedia(voice-file-fallback) name=%s size=%d mime=%s source=%s source_size=%d err=%v", fallback.name, len(fallback.data), fallback.mime, voiceFileName, len(voiceBytes), err)
		return false
	}
	log.Printf("[weixin-mgr] SendMedia voice file fallback OK (to=%s size=%d name=%s mime=%s source=%s source_size=%d converted=%v)", toUserID, len(fallback.data), fallback.name, fallback.mime, voiceFileName, len(voiceBytes), fallback.converted)
	weixin.GetWxLog().Log("mgr.local", "OUT", toUserID, "OK SendMedia(voice-file-fallback) name=%s size=%d mime=%s source=%s source_size=%d converted=%v", fallback.name, len(fallback.data), fallback.mime, voiceFileName, len(voiceBytes), fallback.converted)
	return true
}

type weixinPlayableVoiceFile struct {
	data      []byte
	name      string
	mime      string
	converted bool
}

var weixinPreparePlayableVoiceMP3 = tts.PreparePlayableVoiceMP3

func prepareWeixinPlayableVoiceFile(ctx context.Context, voiceFileName string, voiceBytes []byte) (weixinPlayableVoiceFile, error) {
	file, err := weixinPreparePlayableVoiceMP3(ctx, voiceFileName, voiceBytes)
	if err != nil {
		return weixinPlayableVoiceFile{}, err
	}
	return weixinPlayableVoiceFile{data: file.Data, name: file.Name, mime: file.MIME, converted: file.Converted}, nil
}

func (m *weixinGatewayManager) saveWeixinVoicePlayableDebug(file weixinPlayableVoiceFile) {
	if len(file.data) == 0 || m == nil || m.app == nil {
		return
	}
	dir := filepath.Join(m.app.GetTempDir(), "weixin_voice_debug")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[weixin-mgr] save playable voice debug mkdir failed: %v", err)
		return
	}
	path := filepath.Join(dir, fmt.Sprintf("playable_%d_%s", time.Now().UnixMilli(), file.name))
	if err := os.WriteFile(path, file.data, 0o600); err != nil {
		log.Printf("[weixin-mgr] save playable voice debug failed (path=%s): %v", path, err)
		return
	}
	log.Printf("[weixin-mgr] saved playable voice debug file path=%s size=%d mime=%s converted=%v", path, len(file.data), file.mime, file.converted)
	weixin.GetWxLog().Log("mgr.local", "OUT", "-", "saved playable voice debug file path=%s size=%d mime=%s converted=%v", path, len(file.data), file.mime, file.converted)
}

// imageDownloadClient is a dedicated HTTP client for downloading markdown
// images with a hard timeout, independent of context cancellation.
var imageDownloadClient = &http.Client{Timeout: 20 * time.Second}

// downloadImageURL fetches an image from a URL with a timeout and size limit.
func downloadImageURL(ctx context.Context, rawURL string) ([]byte, error) {
	dlCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(dlCtx, "GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MaClaw/1.0")

	resp, err := imageDownloadClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// Verify Content-Type is an image (or octet-stream which some CDNs use).
	ct := resp.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(ct, "image/") && !strings.HasPrefix(ct, "application/octet-stream") {
		return nil, fmt.Errorf("unexpected Content-Type: %s", ct)
	}

	// Limit to 10 MB to avoid memory issues.
	const maxImageSize = 10 * 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxImageSize {
		return nil, fmt.Errorf("image too large (>10MB)")
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("empty response body")
	}
	return data, nil
}

// sendLocalFiles reads local file paths from the agent response and sends them
// to the WeChat user.
func (m *weixinGatewayManager) sendLocalFiles(gw *weixin.Gateway, toUserID, contextToken string, resp *IMAgentResponse) {
	paths := resp.LocalFilePaths
	if resp.LocalFilePath != "" {
		// Avoid duplicate if LocalFilePath is already in LocalFilePaths.
		found := false
		for _, p := range paths {
			if p == resp.LocalFilePath {
				found = true
				break
			}
		}
		if !found {
			paths = append([]string{resp.LocalFilePath}, paths...)
		}
	}
	ctx := context.Background()
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			log.Printf("[weixin-mgr] read local file %s error: %v", p, err)
			continue
		}
		mediaType := detectMediaType(filepath.Ext(p))
		_ = gw.SendMedia(ctx, weixin.OutgoingMedia{
			ToUserID:     toUserID,
			ContextToken: contextToken,
			FileData:     data,
			FileName:     filepath.Base(p),
			MediaType:    mediaType,
		})
	}
}

// detectMediaType maps a file extension to a WeChat media type.
func detectMediaType(ext string) string {
	switch strings.ToLower(ext) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return "image"
	case ".mp4", ".avi", ".mov", ".mkv":
		return "video"
	default:
		return "file"
	}
}

// saveMediaToTemp saves incoming media data to a temp file and returns the path.
func (m *weixinGatewayManager) saveMediaToTemp(msg weixin.IncomingMessage) (string, error) {
	return saveMediaToTempDir("wx", "wx_", msg.FromUserID, msg.MediaType, msg.MediaData, msg.MediaName)
}

// sendDiag sends a diagnostic message to the WeChat user for remote debugging.
func (m *weixinGatewayManager) sendDiag(toUserID, contextToken, text string) {
	m.mu.Lock()
	gw := m.gateway
	m.mu.Unlock()
	if gw == nil {
		return
	}
	_ = gw.SendText(context.Background(), weixin.OutgoingText{
		ToUserID:     toUserID,
		Text:         text,
		ContextToken: contextToken,
	})
}

// truncateForLog truncates a string for log output.
func truncateForLog(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

// SendProactiveText delivers a plain text push to the owner's active local WeChat
// session (last-active first). Used by 盯人 forward and similar self-notify paths.
func (m *weixinGatewayManager) SendProactiveText(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("empty proactive text")
	}
	if m == nil {
		return fmt.Errorf("weixin gateway manager is nil")
	}
	m.mu.Lock()
	gw := m.gateway
	m.mu.Unlock()
	if gw == nil || !gw.IsRunning() {
		return fmt.Errorf("weixin gateway not running")
	}
	type session struct{ uid, tok string }
	var candidates []session
	seen := map[string]bool{}
	if last := strings.TrimSpace(gw.LastActiveUserID()); last != "" {
		if tok := strings.TrimSpace(gw.GetContextToken(last)); tok != "" {
			candidates = append(candidates, session{uid: last, tok: tok})
			seen[last] = true
		}
	}
	for _, pair := range gw.ContextSessionsByRecency() {
		uid, tok := strings.TrimSpace(pair[0]), strings.TrimSpace(pair[1])
		if uid == "" || tok == "" || seen[uid] {
			continue
		}
		candidates = append(candidates, session{uid: uid, tok: tok})
		seen[uid] = true
	}
	if len(candidates) == 0 {
		return fmt.Errorf("no active weixin session")
	}
	var lastErr error
	for _, c := range candidates {
		if err := gw.SendText(context.Background(), weixin.OutgoingText{
			ToUserID:     c.uid,
			Text:         text,
			ContextToken: c.tok,
		}); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("weixin proactive text failed")
	}
	return lastErr
}

// SendProactiveFile delivers a file/image from the desktop AI assistant to the
// active local WeChat session (does not go through Hub). Prefers the last active
// user, then other sessions newest-first, until one SendMedia succeeds.
func (m *weixinGatewayManager) SendProactiveFile(b64Data, fileName, mimeType, message string) error {
	lang := "zh"
	if m != nil {
		lang = appUILang(m.app)
	}
	if m == nil {
		return fmt.Errorf("weixin gateway manager is nil")
	}
	m.mu.Lock()
	gw := m.gateway
	m.mu.Unlock()
	if gw == nil || !gw.IsRunning() {
		return fmt.Errorf("%s", i18n.T(i18n.MsgWeixinGatewayNotRunning, lang))
	}
	raw, err := decodeToolPayloadBase64(b64Data)
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T(i18n.MsgWeixinFileDecodeFailed, lang), err)
	}
	if len(raw) == 0 {
		return fmt.Errorf("%s", i18n.T(i18n.MsgWeixinFileDataEmpty, lang))
	}

	// Build candidate sessions: last-active first, then remaining by recency.
	type session struct{ uid, tok string }
	var candidates []session
	seen := map[string]bool{}
	if last := strings.TrimSpace(gw.LastActiveUserID()); last != "" {
		if tok := strings.TrimSpace(gw.GetContextToken(last)); tok != "" {
			candidates = append(candidates, session{uid: last, tok: tok})
			seen[last] = true
		}
	}
	for _, pair := range gw.ContextSessionsByRecency() {
		uid, tok := strings.TrimSpace(pair[0]), strings.TrimSpace(pair[1])
		if uid == "" || tok == "" || seen[uid] {
			continue
		}
		candidates = append(candidates, session{uid: uid, tok: tok})
		seen[uid] = true
	}
	if len(candidates) == 0 {
		return fmt.Errorf("%s", i18n.T(i18n.MsgWeixinNoActiveSession, lang))
	}

	name := strings.TrimSpace(fileName)
	if name == "" {
		name = "file.bin"
	}
	mediaType := mediaTypeForProactiveFile(mimeType, name)
	// Sniff magic when MIME/extension are missing or generic (common for screenshots).
	if mediaType == imMediaFile.String() {
		if sniffed := sniffProactiveMediaType(raw); sniffed != "" {
			mediaType = sniffed
		}
	}
	// Defense-in-depth: never ship legacy English bot instructions as captions.
	caption := resolveIMProactiveCaption(lang, message, name, mimeType)

	var lastErr error
	for _, c := range candidates {
		if err := gw.SendMedia(context.Background(), weixin.OutgoingMedia{
			ToUserID:     c.uid,
			Caption:      caption,
			ContextToken: c.tok,
			FileData:     raw,
			FileName:     name,
			MediaType:    mediaType,
		}); err != nil {
			lastErr = err
			log.Printf("[weixin-mgr] SendProactiveFile failed (to=%s name=%s size=%d): %v", c.uid, name, len(raw), err)
			weixin.GetWxLog().Log("mgr.proactive", "OUT", c.uid, "ERR SendProactiveFile name=%s size=%d err=%v", name, len(raw), err)
			continue
		}
		log.Printf("[weixin-mgr] SendProactiveFile OK (to=%s name=%s size=%d media=%s mime=%s caption=%q)", c.uid, name, len(raw), mediaType, mimeType, truncateForLog(caption, 80))
		weixin.GetWxLog().Log("mgr.proactive", "OUT", c.uid, "OK SendProactiveFile name=%s size=%d media=%s mime=%s caption=%s", name, len(raw), mediaType, mimeType, caption)
		return nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("%s", i18n.T(i18n.MsgWeixinNoActiveSession, lang))
	}
	return lastErr
}

// decodeToolPayloadBase64 decodes standard or raw/unpadded base64 used in tool payloads.
// Strips whitespace/newlines that may appear when models wrap large payloads.
func decodeToolPayloadBase64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty base64")
	}
	if strings.ContainsAny(s, " \t\r\n") {
		s = strings.Map(func(r rune) rune {
			switch r {
			case ' ', '\t', '\r', '\n':
				return -1
			default:
				return r
			}
		}, s)
	}
	if raw, err := base64.StdEncoding.DecodeString(s); err == nil {
		return raw, nil
	}
	// Some tool paths strip padding; accept raw standard encoding too.
	if raw, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return raw, nil
	}
	return nil, fmt.Errorf("invalid base64 payload")
}

// mediaTypeForProactiveFile picks WeChat media kind from MIME and filename.
// Screenshots often arrive as application/octet-stream with a .png name.
func mediaTypeForProactiveFile(mimeType, fileName string) string {
	mt := strings.ToLower(strings.TrimSpace(mimeType))
	if strings.HasPrefix(mt, "image/") {
		return imMediaImage.String()
	}
	if strings.HasPrefix(mt, "video/") {
		return imMediaVideo.String()
	}
	if strings.HasPrefix(mt, "audio/") {
		return imMediaVoice.String()
	}
	ext := strings.ToLower(filepath.Ext(fileName))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".heic", ".heif":
		return imMediaImage.String()
	case ".mp4", ".mov", ".avi", ".mkv", ".webm":
		return imMediaVideo.String()
	case ".mp3", ".wav", ".ogg", ".m4a", ".aac", ".silk", ".amr":
		return imMediaVoice.String()
	default:
		return imMediaFile.String()
	}
}

// sniffProactiveMediaType returns image/video/voice from magic bytes, or "".
func sniffProactiveMediaType(data []byte) string {
	if len(data) < 12 {
		return ""
	}
	// PNG
	if data[0] == 0x89 && data[1] == 'P' && data[2] == 'N' && data[3] == 'G' {
		return imMediaImage.String()
	}
	// JPEG (must run before MP3 frame-sync — both can start with 0xFF)
	if data[0] == 0xff && data[1] == 0xd8 {
		return imMediaImage.String()
	}
	// GIF
	if data[0] == 'G' && data[1] == 'I' && data[2] == 'F' {
		return imMediaImage.String()
	}
	// WEBP: RIFF....WEBP
	if data[0] == 'R' && data[1] == 'I' && data[2] == 'F' && data[3] == 'F' &&
		data[8] == 'W' && data[9] == 'E' && data[10] == 'B' && data[11] == 'P' {
		return imMediaImage.String()
	}
	// BMP
	if data[0] == 'B' && data[1] == 'M' {
		return imMediaImage.String()
	}
	// MP4/MOV: ftyp at offset 4
	if data[4] == 'f' && data[5] == 't' && data[6] == 'y' && data[7] == 'p' {
		return imMediaVideo.String()
	}
	// WAV
	if data[0] == 'R' && data[1] == 'I' && data[2] == 'F' && data[3] == 'F' &&
		data[8] == 'W' && data[9] == 'A' && data[10] == 'V' && data[11] == 'E' {
		return imMediaVoice.String()
	}
	// MP3 with ID3 tag
	if data[0] == 'I' && data[1] == 'D' && data[2] == '3' {
		return imMediaVoice.String()
	}
	// MP3 frame sync (after JPEG so 0xFF 0xD8 is not misclassified)
	if data[0] == 0xff && (data[1]&0xe0) == 0xe0 {
		return imMediaVoice.String()
	}
	return ""
}

// HandleGatewayReply dispatches a reply from Hub to the WeChat API.
func (m *weixinGatewayManager) HandleGatewayReply(reply GatewayReplyPayload) {
	wl := weixin.GetWxLog()
	wl.Log("mgr.hubReply", "IN", reply.PlatformUID, "type=%s text_len=%d ctx_token_len=%d", reply.ReplyType, len(reply.Text), len(reply.ContextToken))
	log.Printf("[weixin-mgr] HandleGatewayReply: type=%s uid=%s text_len=%d", reply.ReplyType, reply.PlatformUID, len(reply.Text))
	m.mu.Lock()
	gw := m.gateway
	m.mu.Unlock()
	if gw == nil {
		wl.Log("mgr.hubReply", "---", reply.PlatformUID, "ERR gateway is nil, dropping reply")
		log.Printf("[weixin-mgr] HandleGatewayReply: gateway is nil, dropping reply")
		return
	}

	// Resolve context token: prefer from reply payload (injected by Hub),
	// fall back to locally cached token.
	contextToken := reply.ContextToken
	if contextToken == "" {
		contextToken = gw.GetContextToken(reply.PlatformUID)
	}
	if contextToken == "" {
		wl.Log("mgr.hubReply", "---", reply.PlatformUID, "WARN no contextToken, reply will likely fail")
		log.Printf("[weixin-mgr] HandleGatewayReply: WARNING no contextToken for uid=%s, reply will likely fail", reply.PlatformUID)
	} else {
		wl.Log("mgr.hubReply", "---", reply.PlatformUID, "ctx_token resolved (from_payload=%v)", reply.ContextToken != "")
	}

	switch normalizeGatewayReplyTypeKind(reply.ReplyType) {
	case gatewayReplyTypeText:
		if err := gw.SendText(context.Background(), weixin.OutgoingText{
			ToUserID:     reply.PlatformUID,
			Text:         textutil.StripMarkdown(reply.Text),
			ContextToken: contextToken,
		}); err != nil {
			wl.Log("mgr.hubReply", "OUT", reply.PlatformUID, "ERR SendText: %v", err)
			log.Printf("[weixin-mgr] SendText error (to=%s): %v", reply.PlatformUID, err)
		} else {
			wl.Log("mgr.hubReply", "OUT", reply.PlatformUID, "OK SendText text_len=%d", len(reply.Text))
		}
	case gatewayReplyTypeImage:
		data, err := base64.StdEncoding.DecodeString(reply.ImageData)
		if err != nil {
			wl.Log("mgr.hubReply", "OUT", reply.PlatformUID, "ERR image base64 decode: %v", err)
			log.Printf("[weixin-mgr] image base64 decode error: %v", err)
			return
		}
		if err := gw.SendMedia(context.Background(), weixin.OutgoingMedia{
			ToUserID:     reply.PlatformUID,
			Caption:      reply.Caption,
			ContextToken: contextToken,
			FileData:     data,
			MediaType:    imMediaImage.String(),
		}); err != nil {
			wl.Log("mgr.hubReply", "OUT", reply.PlatformUID, "ERR SendMedia(image): %v", err)
			log.Printf("[weixin-mgr] SendMedia(image) error (to=%s): %v", reply.PlatformUID, err)
		} else {
			wl.Log("mgr.hubReply", "OUT", reply.PlatformUID, "OK SendMedia(image) size=%d", len(data))
		}
	case gatewayReplyTypeFile:
		data, err := base64.StdEncoding.DecodeString(reply.FileData)
		if err != nil {
			wl.Log("mgr.hubReply", "OUT", reply.PlatformUID, "ERR file base64 decode: %v", err)
			log.Printf("[weixin-mgr] file base64 decode error: %v", err)
			return
		}
		if err := gw.SendMedia(context.Background(), weixin.OutgoingMedia{
			ToUserID:     reply.PlatformUID,
			Caption:      reply.Caption,
			ContextToken: contextToken,
			FileData:     data,
			FileName:     reply.FileName,
			MediaType:    imMediaFile.String(),
		}); err != nil {
			wl.Log("mgr.hubReply", "OUT", reply.PlatformUID, "ERR SendMedia(file): %v", err)
			log.Printf("[weixin-mgr] SendMedia(file) error (to=%s): %v", reply.PlatformUID, err)
		} else {
			wl.Log("mgr.hubReply", "OUT", reply.PlatformUID, "OK SendMedia(file) name=%s size=%d", reply.FileName, len(data))
		}
	case gatewayReplyTypeVideo:
		data, err := base64.StdEncoding.DecodeString(reply.FileData)
		if err != nil {
			wl.Log("mgr.hubReply", "OUT", reply.PlatformUID, "ERR video base64 decode: %v", err)
			log.Printf("[weixin-mgr] video base64 decode error: %v", err)
			return
		}
		if err := gw.SendMedia(context.Background(), weixin.OutgoingMedia{
			ToUserID:     reply.PlatformUID,
			Caption:      reply.Caption,
			ContextToken: contextToken,
			FileData:     data,
			FileName:     reply.FileName,
			MediaType:    imMediaVideo.String(),
		}); err != nil {
			wl.Log("mgr.hubReply", "OUT", reply.PlatformUID, "ERR SendMedia(video): %v", err)
			log.Printf("[weixin-mgr] SendMedia(video) error (to=%s): %v", reply.PlatformUID, err)
		} else {
			wl.Log("mgr.hubReply", "OUT", reply.PlatformUID, "OK SendMedia(video) size=%d", len(data))
		}
	case gatewayReplyTypeVoice:
		data, err := base64.StdEncoding.DecodeString(reply.FileData)
		if err != nil || len(data) == 0 {
			wl.Log("mgr.hubReply", "OUT", reply.PlatformUID, "ERR voice base64 decode: %v", err)
			log.Printf("[weixin-mgr] voice base64 decode error: %v", err)
			return
		}
		voiceFileName := reply.FileName
		if voiceFileName == "" {
			voiceFileName = "voice.wav"
		}
		if err := gw.SendMedia(context.Background(), weixin.OutgoingMedia{
			ToUserID:     reply.PlatformUID,
			ContextToken: contextToken,
			FileData:     data,
			FileName:     voiceFileName,
			MediaType:    imMediaVoice.String(),
		}); err != nil {
			wl.Log("mgr.hubReply", "OUT", reply.PlatformUID, "ERR SendMedia(voice): %v", err)
			log.Printf("[weixin-mgr] SendMedia(voice) error (to=%s): %v", reply.PlatformUID, err)
		} else {
			wl.Log("mgr.hubReply", "OUT", reply.PlatformUID, "OK SendMedia(voice) name=%s size=%d", voiceFileName, len(data))
		}
	default:
		wl.Log("mgr.hubReply", "---", reply.PlatformUID, "WARN unknown reply_type=%s", reply.ReplyType)
	}
}

// ---------------------------------------------------------------------------
// App integration — Wails bindings and lifecycle
// ---------------------------------------------------------------------------

// forwardDesktopFileToIM delivers a file from the desktop AI assistant to the
// user's IM channels. Prefer the local Weixin gateway (single-machine mode);
// fall back to Hub proactive file broadcast for multi-machine / Feishu etc.
func (a *App) forwardDesktopFileToIM(hubClient *RemoteHubClient, b64Data, fileName, mimeType, message string) error {
	var localErr, hubErr error
	// Local Weixin first — users on weixin_local often have no Hub IM route.
	if a != nil {
		a.ensureWeixinGateway()
		if a.weixinGateway != nil {
			localErr = a.weixinGateway.SendProactiveFile(b64Data, fileName, mimeType, message)
			if localErr == nil {
				return nil
			}
			log.Printf("[IM-forward] local weixin SendProactiveFile failed: %v", localErr)
		} else {
			localErr = fmt.Errorf("local weixin gateway unavailable")
		}
	}
	if hubClient != nil {
		hubErr = hubClient.SendIMProactiveFile(b64Data, fileName, mimeType, message)
		if hubErr == nil {
			if localErr != nil {
				log.Printf("[IM-forward] hub proactive file OK after local failure: %v", localErr)
			}
			return nil
		}
		log.Printf("[IM-forward] hub SendIMProactiveFile failed: %v", hubErr)
	} else {
		hubErr = fmt.Errorf("hub client unavailable")
	}
	if localErr != nil && hubErr != nil {
		return fmt.Errorf("local weixin: %v; hub: %v", localErr, hubErr)
	}
	if localErr != nil {
		return localErr
	}
	return hubErr
}

// ensureWeixinGateway lazily creates the gateway manager and syncs from config.
// If WeChat is not enabled in config, skips entirely to avoid unnecessary work.
func (a *App) ensureWeixinGateway() {
	a.logMemorySnapshot("ensureWeixinGateway:start")
	cfg, err := a.LoadConfig()
	if err != nil {
		return
	}
	if !cfg.WeixinEnabled || cfg.WeixinToken == "" {
		if a.weixinGateway != nil {
			a.weixinGateway.SyncFromConfig()
		}
		return
	}
	if a.weixinGateway == nil {
		a.weixinGateway = newWeixinGatewayManager(a)
	}
	a.weixinGateway.SyncFromConfig()
	a.logMemorySnapshot("ensureWeixinGateway:done")
}

func (a *App) GetWeixinStatus() string {
	if a.weixinGateway == nil {
		return "disconnected"
	}
	return a.weixinGateway.Status()
}

func (a *App) RestartWeixin() string {
	a.ensureWeixinGateway()
	if a.weixinGateway == nil {
		return gatewayConnectionStatusDisconnected.String()
	}
	return a.weixinGateway.Status()
}

func (a *App) StopWeixin() {
	if a.weixinGateway != nil {
		a.weixinGateway.Stop()
	}
}

// GetWeixinLocalMode returns whether WeChat local mode is enabled.
func (a *App) GetWeixinLocalMode() bool {
	cfg, err := a.LoadConfig()
	if err != nil {
		return true // default: local
	}
	return cfg.IsWeixinLocalMode()
}

// SetWeixinLocalMode enables or disables WeChat local mode.
func (a *App) SetWeixinLocalMode(enabled bool) error {
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	// Switching to hub mode requires prior Hub registration.
	if !enabled && cfg.RemoteMachineID == "" {
		return fmt.Errorf("please register this machine to Hub before enabling multi-machine mode")
	}
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.SetWeixinLocal(enabled)
	}); err != nil {
		return err
	}
	log.Printf("[weixin-mgr] SetWeixinLocalMode: enabled=%v (local_mode after save: %v)", enabled, cfg.IsWeixinLocalMode())

	// Invalidate cached local handler so it's recreated on next message.
	if a.weixinGateway != nil {
		a.weixinGateway.resetLocalHandler()
	}

	// When switching to hub mode, the gateway is already connected so
	// onStatusChange("connected") won't fire again. We must explicitly
	// send the gateway claim so Hub registers this machine as the owner.
	if !enabled {
		hubClient := a.hubClient()
		hubNil := hubClient == nil
		hubConnected := !hubNil && hubClient.IsConnected()
		log.Printf("[weixin-mgr] switching to hub mode: hub_nil=%v hub_connected=%v", hubNil, hubConnected)
		if hubClient != nil && hubClient.IsConnected() {
			hubClient.SendIMGatewayClaim(imGatewayPlatformWeixin)
			log.Printf("[weixin-mgr] sent gateway claim after switching to hub mode")
		} else {
			log.Printf("[weixin-mgr] WARNING: cannot send gateway claim, hub not available")
		}
	}
	return nil
}

func (a *App) saveWeixinLoginConfig(result *weixin.QRLoginResult) error {
	if result == nil {
		return fmt.Errorf("weixin login result is nil")
	}
	return a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.WeixinEnabled = true
		cfg.WeixinToken = result.BotToken
		cfg.WeixinAccountID = result.AccountID
		if result.BaseURL != "" {
			cfg.WeixinBaseURL = result.BaseURL
		}
		if cfg.WeixinLocalMode == nil {
			local := true
			cfg.WeixinLocalMode = &local
			log.Printf("[weixin-mgr] first-time binding: auto-setting local mode")
		}
	})
}

// StartWeixinQRLogin initiates a QR code login flow.
// Returns the QR code image URL for the frontend to display.
func (a *App) StartWeixinQRLogin() map[string]string {
	cfg, err := a.LoadConfig()
	if err != nil {
		return map[string]string{"error": "无法加载配置: " + err.Error()}
	}
	baseURL := cfg.WeixinBaseURL
	if baseURL == "" {
		baseURL = weixin.DefaultBaseURL
	}

	qrcodeURL, qrcodeToken, err := weixin.StartQRLogin(context.Background(), baseURL, weixin.DefaultBotType)
	if err != nil {
		return map[string]string{"error": "获取二维码失败 " + err.Error()}
	}
	return map[string]string{
		"qrcode_url":   qrcodeURL,
		"qrcode_token": qrcodeToken,
	}
}

const weixinQRStatusPollTimeout = 5 * time.Second

// PollWeixinQRStatus performs a single short poll of the QR code status.
// Returns status ("wait", "scaned", "confirmed", "expired") and a message.
// On "confirmed", automatically saves config and starts gateway (no separate confirm call needed).
func (a *App) PollWeixinQRStatus(qrcodeToken string) map[string]string {
	cfg, err := a.LoadConfig()
	if err != nil {
		return map[string]string{"error": "无法加载配置: " + err.Error()}
	}
	baseURL := cfg.WeixinBaseURL
	if baseURL == "" {
		baseURL = weixin.DefaultBaseURL
	}

	ctx, cancel := context.WithTimeout(context.Background(), weixinQRStatusPollTimeout)
	defer cancel()

	start := time.Now()
	result, status, err := weixin.PollQRStatus(ctx, baseURL, qrcodeToken)
	if err != nil {
		log.Printf("[weixin-qr] poll failed status=error elapsed=%s retryable=%v err=%v", time.Since(start).Round(time.Millisecond), weixin.IsQRLoginRetryableError(err), err)
		resp := map[string]string{"error": err.Error(), "status": string(gatewayConnectionStatusError)}
		if weixin.IsQRLoginRetryableError(err) {
			resp["retryable"] = "true"
		}
		return resp
	}
	log.Printf("[weixin-qr] poll status=%s elapsed=%s", status.String(), time.Since(start).Round(time.Millisecond))
	resp := map[string]string{
		"status":  status.String(),
		"message": result.Message,
	}
	if status == weixin.QRLoginStatusConfirmed {
		if !result.Connected {
			resp["error"] = result.Message
			return resp
		}
		// Auto-save credentials and start gateway on confirmed
		cfg.WeixinEnabled = true
		cfg.WeixinToken = result.BotToken
		cfg.WeixinAccountID = result.AccountID
		if result.BaseURL != "" {
			cfg.WeixinBaseURL = result.BaseURL
		}
		if cfg.WeixinLocalMode == nil {
			local := true
			cfg.WeixinLocalMode = &local
			log.Printf("[weixin-mgr] first-time binding: auto-setting local mode")
		}
		if err := a.saveWeixinLoginConfig(result); err != nil {
			resp["error"] = "登录成功但保存配置失败 " + err.Error()
			return resp
		}
		go a.ensureWeixinGateway()
		resp["account_id"] = result.AccountID
	}
	return resp
}

// WaitWeixinQRLogin waits for the user to scan the QR code and confirm login.
// qrcodeToken is from StartWeixinQRLogin. On success, saves credentials to config.
// Deprecated: prefer PollWeixinQRStatus + ConfirmWeixinQRLogin for non-blocking UI.
func (a *App) WaitWeixinQRLogin(qrcodeToken string) map[string]string {
	cfg, err := a.LoadConfig()
	if err != nil {
		return map[string]string{"error": "无法加载配置: " + err.Error()}
	}
	baseURL := cfg.WeixinBaseURL
	if baseURL == "" {
		baseURL = weixin.DefaultBaseURL
	}

	result, err := weixin.WaitForQRLogin(context.Background(), baseURL, qrcodeToken, 8*time.Minute)
	if err != nil {
		return map[string]string{"error": err.Error()}
	}
	if !result.Connected {
		return map[string]string{"error": result.Message}
	}

	// Save to config
	cfg.WeixinEnabled = true
	cfg.WeixinToken = result.BotToken
	cfg.WeixinAccountID = result.AccountID
	if result.BaseURL != "" {
		cfg.WeixinBaseURL = result.BaseURL
	}
	// First-time WeChat binding: if the user has never explicitly set local
	// mode, default to local so the gateway works immediately without
	// requiring a Hub round-trip. Users can switch to Hub mode later.
	if cfg.WeixinLocalMode == nil {
		local := true
		cfg.WeixinLocalMode = &local
		log.Printf("[weixin-mgr] first-time binding: auto-setting local mode")
	}
	if err := a.saveWeixinLoginConfig(result); err != nil {
		return map[string]string{
			"status": "connected",
			"error":  "登录成功但保存配置失败 " + err.Error(),
		}
	}

	// Start gateway
	a.ensureWeixinGateway()

	return map[string]string{
		"status":     "connected",
		"account_id": result.AccountID,
		"message":    result.Message,
	}
}
