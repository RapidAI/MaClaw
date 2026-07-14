package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/i18n"
	"github.com/RapidAI/CodeClaw/corelib/telegram"
	"github.com/RapidAI/CodeClaw/corelib/textutil"
)

// telegramGatewayManager manages the client-side Telegram Bot gateway.
// Supports two modes:
//   - Local / 单机 mode (default): routes messages directly to the
//     local MaClaw LLM agent loop, bypassing Hub entirely.
//   - Hub / 多机 mode (TelegramLocalMode=false): forwards messages to Hub
//     via im.gateway_message, receives replies via im.gateway_reply.
type telegramGatewayManager struct {
	app       *App
	mu        sync.Mutex
	gateway   *telegram.Gateway
	status    gatewayConnectionStatus
	lastToken string

	// localHandler is a fully-wired IMMessageHandler for local mode.
	localHandler *IMMessageHandler
}

func newTelegramGatewayManager(app *App) *telegramGatewayManager {
	return &telegramGatewayManager{
		app:    app,
		status: gatewayConnectionStatusDisconnected,
	}
}

// SyncFromConfig reads the current AppConfig and starts or stops the gateway.
func (m *telegramGatewayManager) SyncFromConfig() {
	cfg, err := m.app.LoadConfig()
	if err != nil {
		return
	}

	m.mu.Lock()
	if !cfg.TelegramBotEnabled || cfg.TelegramBotToken == "" {
		gw := m.gateway
		if gw != nil {
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
			_ = hubClient.SendIMGatewayUnclaim(imGatewayPlatformTelegram)
			log.Printf("[telegram-mgr] sent gateway unclaim to hub")
		}
		if gw != nil {
			m.emitStatusEvent()
		}
		return
	}

	if m.gateway != nil && m.lastToken == cfg.TelegramBotToken {
		m.mu.Unlock()
		return
	}

	oldGw := m.gateway
	m.gateway = nil
	m.mu.Unlock()

	if oldGw != nil {
		_ = oldGw.Stop()
	}

	gw := telegram.NewGateway(telegram.Config{BotToken: cfg.TelegramBotToken}, m.onIncomingMessage)
	gw.SetStatusCallback(m.onStatusChange)

	m.mu.Lock()
	m.gateway = gw
	m.lastToken = cfg.TelegramBotToken
	m.mu.Unlock()

	if err := gw.Start(context.Background()); err != nil {
		log.Printf("[telegram-mgr] start failed: %v", err)
		m.mu.Lock()
		m.gateway = nil
		m.lastToken = ""
		m.status = gatewayConnectionStatusError
		m.mu.Unlock()
		m.emitStatusEvent()
		return
	}
}

// Stop shuts down the gateway.
func (m *telegramGatewayManager) Stop() {
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
	m.emitStatusEvent()
}

// Status returns the current connection status.
func (m *telegramGatewayManager) Status() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status.String()
}

func (m *telegramGatewayManager) onStatusChange(status string) {
	m.mu.Lock()
	normalized := normalizeGatewayConnectionStatus(status)
	m.status = normalized
	if normalized == gatewayConnectionStatusError {
		m.gateway = nil
		m.lastToken = ""
	}
	m.mu.Unlock()
	m.emitStatusEvent()

	if normalized == gatewayConnectionStatusConnected {
		// In local mode, skip Hub gateway claim.
		if cfg, err := m.app.LoadConfig(); err == nil && cfg.IsTelegramLocalMode() {
			return
		}
		hubClient := m.app.hubClient()
		if hubClient != nil && hubClient.IsConnected() {
			hubClient.SendIMGatewayClaim(imGatewayPlatformTelegram)
		}
	}
}

func (m *telegramGatewayManager) emitStatusEvent() {
	m.app.emitEvent("telegram-status-changed", m.Status())
}

// resetLocalHandler invalidates the cached local handler.
func (m *telegramGatewayManager) resetLocalHandler() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.localHandler != nil {
		m.localHandler.memory.Stop()
		m.localHandler = nil
	}
}

// ensureLocalHandler lazily creates a fully-wired IMMessageHandler for local mode.
func (m *telegramGatewayManager) ensureLocalHandler() *IMMessageHandler {
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
	log.Printf("[telegram-mgr] local IMMessageHandler created")

	// Wire interrupt handler to gateway for cancel/merge/status during active loops.
	if m.gateway != nil && h.interruptHandler != nil {
		m.gateway.SetInterruptHandler(h.interruptHandler)
	}

	return h
}

// onIncomingMessage routes Telegram messages to local handler or Hub.
func (m *telegramGatewayManager) onIncomingMessage(msg telegram.IncomingMessage) {
	if isPassthroughSlashText(msg.Text) {
		log.Printf("[telegram-mgr] routing passthrough command locally: chat=%d", msg.ChatID)
		m.handleLocalMessage(msg)
		return
	}
	cfg, err := m.app.LoadConfig()
	if err != nil {
		log.Printf("[telegram-mgr] LoadConfig error: %v", err)
		return
	}

	isLocal := cfg.IsTelegramLocalMode()
	hubClient := m.app.hubClient()
	hubConnected := hubClient != nil && hubClient.IsConnected()
	log.Printf("[telegram-mgr] onIncomingMessage: chat=%d local_mode=%v hub_connected=%v", msg.ChatID, isLocal, hubConnected)

	if isLocal {
		m.handleLocalMessage(msg)
		return
	}

	// Hub mode — try forwarding; fall back to local if Hub unavailable.
	if !hubConnected {
		log.Printf("[telegram-mgr] Hub mode but Hub unavailable, falling back to local: chat=%d", msg.ChatID)
		m.notifyHubUnavailable(msg)
		m.handleLocalMessage(msg)
		return
	}

	m.forwardToHub(msg)
}

func (m *telegramGatewayManager) notifyHubUnavailable(msg telegram.IncomingMessage) {
	m.mu.Lock()
	gw := m.gateway
	m.mu.Unlock()
	if gw == nil {
		return
	}
	_ = gw.SendText(context.Background(), telegram.OutgoingText{
		ChatID: msg.ChatID,
		Text:   i18n.T(i18n.MsgHubUnavailable, "zh"),
	})
}

func (m *telegramGatewayManager) forwardToHub(msg telegram.IncomingMessage) {
	hubClient := m.app.hubClient()
	if hubClient == nil || !hubClient.IsConnected() {
		log.Printf("[telegram-mgr] forwardToHub FAILED: hub unavailable chat=%d", msg.ChatID)
		m.notifyHubUnavailable(msg)
		m.handleLocalMessage(msg)
		return
	}

	msgType := "text"
	if msg.MediaType != "" && len(msg.MediaData) > 0 {
		msgType = msg.MediaType
	}

	payload := map[string]any{
		"platform_uid": strconv.FormatInt(msg.ChatID, 10),
		"text":         msg.Text,
		"message_type": msgType,
	}

	if att := buildMediaAttachment(msg.MediaType, msg.MediaData, msg.MediaName, msg.MimeType); att != nil {
		payload["attachments"] = []map[string]any{att}
	}

	if err := hubClient.SendIMGatewayMessage(imGatewayPlatformTelegram, payload); err != nil {
		log.Printf("[telegram-mgr] forwardToHub SendIMGatewayMessage error: %v", err)
	}
}

func (m *telegramGatewayManager) handleLocalMessage(msg telegram.IncomingMessage) {
	if resp, handled := m.app.TryHandlePassthroughSlashCommandWithSource(msg.Text, "telegram:"+strconv.FormatInt(msg.ChatID, 10)); handled {
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
			_ = gw.SendText(context.Background(), telegram.OutgoingText{
				ChatID: msg.ChatID,
				Text:   reply,
			})
		}
		return
	}
	if !m.app.isMaclawLLMConfigured() {
		m.mu.Lock()
		gw := m.gateway
		m.mu.Unlock()
		if gw != nil {
			_ = gw.SendText(context.Background(), telegram.OutgoingText{
				ChatID: msg.ChatID,
				Text:   i18n.T(i18n.MsgLLMNotConfigured, "zh"),
			})
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

	text := msg.Text

	// Pass images as multimodal attachments so the LLM can "see" them.
	var attachments []MessageAttachment
	if msg.MediaType != "" && len(msg.MediaData) > 0 {
		if normalizeIMMediaKind(msg.MediaType).IsImage() {
			attachments = append(attachments, buildLocalImageAttachment(msg.MediaData, msg.MediaName, msg.MimeType))
		} else {
			mediaPath, err := saveTelegramMediaToTemp(msg)
			if err != nil {
				log.Printf("[telegram-mgr] save media error: %v", err)
			} else {
				prefix := "[收到" + mediaLabel(msg.MediaType) + ": " + mediaPath + "]\n"
				text = prefix + text
			}
		}
	}

	if text == "" && len(attachments) == 0 {
		return
	}

	progressFilter := newIMProgressVisibilityFilter(m.app)
	var lastProgress time.Time
	onProgress := func(progressText string) {
		if !progressFilter.ShouldSendProgress(progressText) {
			return
		}
		now := time.Now()
		if now.Sub(lastProgress) < 5*time.Second {
			return
		}
		lastProgress = now
		_ = gw.SendText(context.Background(), telegram.OutgoingText{
			ChatID: msg.ChatID,
			Text:   i18n.T(i18n.MsgProgressPrefix, appUILang(m.app)) + textutil.StripMarkdown(progressText),
		})
	}

	resp := handler.HandleIMMessageWithProgress(IMUserMessage{
		UserID:      strconv.FormatInt(msg.ChatID, 10),
		Platform:    "telegram_local",
		Text:        text,
		// Prefer GUI interface language so IM tool/status text matches desktop settings.
		Lang:        appUILang(m.app),
		Attachments: attachments,
	}, onProgress)

	if resp == nil || resp.Deferred {
		return
	}

	m.sendAgentResponse(gw, msg.ChatID, resp)
}

func (m *telegramGatewayManager) sendAgentResponse(gw *telegram.Gateway, chatID int64, resp *IMAgentResponse) {
	ctx := context.Background()

	if resp.Text != "" {
		text := textutil.StripMarkdown(resp.Text)
		if err := gw.SendText(ctx, telegram.OutgoingText{
			ChatID: chatID,
			Text:   text,
		}); err != nil {
			log.Printf("[telegram-mgr] local SendText error (to=%d): %v", chatID, err)
		}
	}

	if resp.Error != "" && resp.Text == "" {
		_ = gw.SendText(ctx, telegram.OutgoingText{
			ChatID: chatID,
			Text:   "" + textutil.StripMarkdown(resp.Error),
		})
	}

	if resp.ImageKey != "" {
		_ = gw.SendMedia(ctx, telegram.OutgoingMedia{
			ChatID:   chatID,
			FileType: "photo",
			FileData: resp.ImageKey,
		})
	}

	if resp.FileData != "" {
		mimeType := resp.FileMimeType
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		_ = gw.SendMedia(ctx, telegram.OutgoingMedia{
			ChatID:   chatID,
			FileType: "document",
			FileData: resp.FileData,
			FileName: resp.FileName,
			MimeType: mimeType,
		})
	}

	// Send voice message (OGG Opus for Telegram voice bubble).
	if resp.VoiceData != "" {
		if err := gw.SendMedia(ctx, telegram.OutgoingMedia{
			ChatID:   chatID,
			FileType: "voice",
			FileData: resp.VoiceData,
			FileName: resp.VoiceFileName,
		}); err != nil {
			log.Printf("[telegram-mgr] local SendMedia (voice) error (to=%d): %v", chatID, err)
		}
	}

	m.sendLocalFiles(gw, chatID, resp)
}

func (m *telegramGatewayManager) sendLocalFiles(gw *telegram.Gateway, chatID int64, resp *IMAgentResponse) {
	paths := resp.LocalFilePaths
	if resp.LocalFilePath != "" && !containsString(paths, resp.LocalFilePath) {
		paths = append([]string{resp.LocalFilePath}, paths...)
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			log.Printf("[telegram-mgr] read local file %s error: %v", p, err)
			continue
		}
		name := filepath.Base(p)
		mediaType := mediaTypeFromFileName(name)
		fileType := "document"
		switch normalizeIMMediaKind(mediaType) {
		case imMediaImage:
			fileType = "photo"
		case imMediaVoice:
			fileType = "voice"
		case imMediaAudio:
			fileType = "audio"
		case imMediaVideo:
			fileType = "video"
		}
		if err := gw.SendMedia(context.Background(), telegram.OutgoingMedia{
			ChatID:   chatID,
			FileType: fileType,
			FileData: base64.StdEncoding.EncodeToString(data),
			FileName: name,
			MimeType: guessMimeFromMedia(mediaType, name),
		}); err != nil {
			log.Printf("[telegram-mgr] SendMedia local file failed (to=%d file=%s): %v", chatID, p, err)
		}
	}
}

// GatewayReplyPayload holds the fields of an im.gateway_reply from Hub.
type GatewayReplyPayload struct {
	ReplyType    gatewayReplyTypeKind `json:"reply_type"`
	PlatformUID  string               `json:"platform_uid"`
	Text         string               `json:"text"`
	ImageData    string               `json:"image_data"`
	Caption      string               `json:"caption"`
	FileData     string               `json:"file_data"`
	FileName     string               `json:"file_name"`
	MimeType     string               `json:"mime_type"`
	ContextToken string               `json:"context_token,omitempty"`
	Extra        map[string]any       `json:"extra,omitempty"`
}

// HandleGatewayReply dispatches a reply from Hub to the Telegram API.
func (m *telegramGatewayManager) HandleGatewayReply(reply GatewayReplyPayload) {
	m.mu.Lock()
	gw := m.gateway
	m.mu.Unlock()
	if gw == nil {
		return
	}

	chatID, err := strconv.ParseInt(reply.PlatformUID, 10, 64)
	if err != nil {
		log.Printf("[telegram-mgr] invalid chatID %q: %v", reply.PlatformUID, err)
		return
	}

	switch normalizeGatewayReplyTypeKind(reply.ReplyType) {
	case gatewayReplyTypeText:
		_ = gw.SendText(context.Background(), telegram.OutgoingText{
			ChatID: chatID,
			Text:   reply.Text,
		})
	case gatewayReplyTypeImage:
		_ = gw.SendMedia(context.Background(), telegram.OutgoingMedia{
			ChatID:   chatID,
			FileType: "photo",
			FileData: reply.ImageData,
			Caption:  reply.Caption,
		})
	case gatewayReplyTypeFile:
		_ = gw.SendMedia(context.Background(), telegram.OutgoingMedia{
			ChatID:   chatID,
			FileType: "document",
			FileData: reply.FileData,
			FileName: reply.FileName,
			MimeType: reply.MimeType,
		})
	case gatewayReplyTypeVoice:
		_ = gw.SendMedia(context.Background(), telegram.OutgoingMedia{
			ChatID:   chatID,
			FileType: "voice",
			FileData: reply.FileData,
			FileName: reply.FileName,
		})
	}
}

// ---------------------------------------------------------------------------
// App integration — Wails bindings and lifecycle
// ---------------------------------------------------------------------------

// ensureTelegramGateway lazily creates the gateway manager and syncs from config.
// If Telegram is not enabled in config, skips entirely to avoid unnecessary work.
func (a *App) ensureTelegramGateway() {
	cfg, err := a.LoadConfig()
	if err != nil {
		return
	}
	if !cfg.TelegramBotEnabled || cfg.TelegramBotToken == "" {
		if a.telegramGateway != nil {
			a.telegramGateway.SyncFromConfig()
		}
		return
	}
	if a.telegramGateway == nil {
		a.telegramGateway = newTelegramGatewayManager(a)
	}
	a.telegramGateway.SyncFromConfig()
}

func (a *App) GetTelegramStatus() string {
	if a.telegramGateway == nil {
		return "disconnected"
	}
	return a.telegramGateway.Status()
}

func (a *App) RestartTelegram() string {
	a.ensureTelegramGateway()
	if a.telegramGateway == nil {
		return gatewayConnectionStatusDisconnected.String()
	}
	return a.telegramGateway.Status()
}

func (a *App) StopTelegram() {
	if a.telegramGateway != nil {
		a.telegramGateway.Stop()
	}
}

// GetTelegramLocalMode returns whether Telegram local mode is enabled.
func (a *App) GetTelegramLocalMode() bool {
	cfg, err := a.LoadConfig()
	if err != nil {
		return true
	}
	return cfg.IsTelegramLocalMode()
}

// SetTelegramLocalMode enables or disables Telegram local mode.
func (a *App) SetTelegramLocalMode(enabled bool) error {
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	// Switching to hub mode requires prior Hub registration.
	if !enabled && cfg.RemoteMachineID == "" {
		return fmt.Errorf("please register this machine to Hub before enabling multi-machine mode")
	}
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.SetTelegramLocal(enabled)
	}); err != nil {
		return err
	}
	log.Printf("[telegram-mgr] SetTelegramLocalMode: enabled=%v", enabled)

	if a.telegramGateway != nil {
		a.telegramGateway.resetLocalHandler()
	}

	if !enabled {
		hubClient := a.hubClient()
		if hubClient != nil && hubClient.IsConnected() {
			hubClient.SendIMGatewayClaim(imGatewayPlatformTelegram)
			log.Printf("[telegram-mgr] sent gateway claim after switching to hub mode")
		}
	}
	return nil
}

// saveTelegramMediaToTemp saves media from a Telegram message to a temp file.
func saveTelegramMediaToTemp(msg telegram.IncomingMessage) (string, error) {
	return saveMediaToTempDir("tg", "tg_", strconv.FormatInt(msg.ChatID, 10), msg.MediaType, msg.MediaData, msg.MediaName)
}
