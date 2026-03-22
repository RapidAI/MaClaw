package main

import (
	"context"
	"encoding/base64"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

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
	status    string
	lastToken string

	// localHandler is a fully-wired IMMessageHandler for local mode.
	// Created lazily on first local-mode message.
	localHandler *IMMessageHandler
}

func newWeixinGatewayManager(app *App) *weixinGatewayManager {
	return &weixinGatewayManager{
		app:    app,
		status: "disconnected",
	}
}

// SyncFromConfig reads the current AppConfig and starts or stops the gateway.
func (m *weixinGatewayManager) SyncFromConfig() {
	cfg, err := m.app.LoadConfig()
	if err != nil {
		return
	}

	m.mu.Lock()
	if !cfg.WeixinEnabled || cfg.WeixinToken == "" {
		gw := m.gateway
		if gw != nil {
			m.gateway = nil
			m.status = "disconnected"
			m.mu.Unlock()
			_ = gw.Stop()
			m.emitStatusEvent()
			return
		}
		m.mu.Unlock()
		return
	}

	if m.gateway != nil && m.lastToken == cfg.WeixinToken {
		m.mu.Unlock()
		return
	}

	oldGw := m.gateway
	// Keep old gateway in place until new one is ready — avoids a nil window
	// where HandleGatewayReply would silently drop messages.
	m.mu.Unlock()

	if oldGw != nil {
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
		log.Printf("[weixin-mgr] start failed: %v", err)
		m.mu.Lock()
		m.status = "error"
		m.mu.Unlock()
		m.emitStatusEvent()
		return
	}
}

// Stop shuts down the gateway.
func (m *weixinGatewayManager) Stop() {
	m.mu.Lock()
	gw := m.gateway
	m.gateway = nil
	m.status = "disconnected"
	m.lastToken = ""
	lh := m.localHandler
	m.localHandler = nil
	m.mu.Unlock()
	if lh != nil {
		lh.memory.stop()
	}
	if gw != nil {
		_ = gw.Stop()
	}
}

// Status returns the current connection status.
func (m *weixinGatewayManager) Status() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

func (m *weixinGatewayManager) onStatusChange(status string) {
	m.mu.Lock()
	m.status = status
	m.mu.Unlock()
	m.emitStatusEvent()

	if status == "connected" {
		// In local mode, skip Hub gateway claim.
		if cfg, err := m.app.LoadConfig(); err == nil && cfg.IsWeixinLocalMode() {
			return
		}
		hubClient := m.app.hubClient()
		if hubClient != nil && hubClient.IsConnected() {
			hubClient.SendIMGatewayClaim("weixin")
		}
	}
}

func (m *weixinGatewayManager) emitStatusEvent() {
	m.app.emitEvent("weixin-status-changed", m.Status())
}

// resetLocalHandler tears down the cached local IMMessageHandler so it will
// be recreated on the next local-mode message. Safe to call when not in local mode.
func (m *weixinGatewayManager) resetLocalHandler() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.localHandler != nil {
		m.localHandler.memory.stop()
		m.localHandler = nil
	}
}

// onIncomingMessage routes WeChat messages based on config:
//   - Local mode: directly invokes the local MaClaw LLM agent loop
//   - Hub mode: forwards to Hub via im.gateway_message
func (m *weixinGatewayManager) onIncomingMessage(msg weixin.IncomingMessage) {
	cfg, err := m.app.LoadConfig()
	if err != nil {
		log.Printf("[weixin-mgr] LoadConfig error: %v", err)
		return
	}

	if cfg.IsWeixinLocalMode() {
		m.handleLocalMessage(msg)
		return
	}

	m.forwardToHub(msg)
}

// forwardToHub sends the message to Hub via im.gateway_message (original behaviour).
func (m *weixinGatewayManager) forwardToHub(msg weixin.IncomingMessage) {
	hubClient := m.app.hubClient()
	if hubClient == nil || !hubClient.IsConnected() {
		log.Printf("[weixin-mgr] hub not connected, cannot forward WX message from user=%s", msg.FromUserID)
		m.mu.Lock()
		gw := m.gateway
		m.mu.Unlock()
		if gw != nil {
			if err := gw.SendText(context.Background(), weixin.OutgoingText{
				ToUserID:     msg.FromUserID,
				Text:         "⚠️ Hub 未连接，无法处理消息。",
				ContextToken: msg.ContextToken,
			}); err != nil {
				log.Printf("[weixin-mgr] failed to send hub-disconnected notice to user=%s: %v", msg.FromUserID, err)
			}
		}
		return
	}

	payload := map[string]any{
		"platform_uid": msg.FromUserID,
		"text":         msg.Text,
		"message_type": "text",
	}

	// Include media if present
	if msg.MediaType != "" && len(msg.MediaData) > 0 {
		payload["message_type"] = msg.MediaType
		payload["media_data"] = base64.StdEncoding.EncodeToString(msg.MediaData)
		if msg.MediaName != "" {
			payload["file_name"] = msg.MediaName
		}
	}

	// Include context_token so Hub can pass it back in replies
	if msg.ContextToken != "" {
		payload["context_token"] = msg.ContextToken
	}

	hubClient.SendIMGatewayMessage("weixin", payload)
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
	a.ensureRemoteInfra()

	h := NewIMMessageHandler(a, a.remoteSessions)
	// Wire the same subsystems that createAndWireHubClient does.
	if a.capabilityGapDetector != nil {
		h.SetCapabilityGapDetector(a.capabilityGapDetector)
	}
	if a.toolDefGenerator != nil {
		h.SetToolDefGenerator(a.toolDefGenerator)
	}
	if a.toolRouter != nil {
		h.SetToolRouter(a.toolRouter)
	}
	if a.memoryStore != nil {
		h.SetMemoryStore(a.memoryStore)
	}
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
	if a.startupFeedback != nil {
		h.SetStartupFeedback(a.startupFeedback)
	}
	if a.securityFirewall != nil {
		h.SetSecurityFirewall(a.securityFirewall)
	}
	if a.conversationArchiver != nil {
		h.memory.archiver = a.conversationArchiver
	}

	m.localHandler = h
	log.Printf("[weixin-mgr] local IMMessageHandler created")
	return h
}

// handleLocalMessage processes a WeChat message through the local agent loop
// and sends the response back via the WeChat API.
func (m *weixinGatewayManager) handleLocalMessage(msg weixin.IncomingMessage) {
	// Check LLM is configured before entering the agent loop.
	if !m.app.isMaclawLLMConfigured() {
		m.mu.Lock()
		gw := m.gateway
		m.mu.Unlock()
		if gw != nil {
			_ = gw.SendText(context.Background(), weixin.OutgoingText{
				ToUserID:     msg.FromUserID,
				Text:         "⚠️ 本地 LLM 未配置，请先在设置中配置 MaClaw LLM。",
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
		return
	}

	contextToken := msg.ContextToken
	if contextToken == "" {
		contextToken = gw.GetContextToken(msg.FromUserID)
	}

	// Build the user message text; prepend media info if present.
	text := msg.Text
	if msg.MediaType != "" && len(msg.MediaData) > 0 {
		// Save media to a temp file so the agent can reference it.
		mediaPath, err := m.saveMediaToTemp(msg)
		if err != nil {
			log.Printf("[weixin-mgr] save media error: %v", err)
		} else {
			prefix := "[收到" + mediaLabel(msg.MediaType) + ": " + mediaPath + "]\n"
			text = prefix + text
		}
	}

	if text == "" {
		return
	}

	// Progress callback — send intermediate status to the WeChat user.
	// Use a rate limiter to avoid flooding: at most one progress message per 5s.
	var lastProgress time.Time
	onProgress := func(progressText string) {
		if progressText == "" {
			return
		}
		now := time.Now()
		if now.Sub(lastProgress) < 5*time.Second {
			return
		}
		lastProgress = now
		_ = gw.SendText(context.Background(), weixin.OutgoingText{
			ToUserID:     msg.FromUserID,
			Text:         "⏳ " + progressText,
			ContextToken: contextToken,
		})
	}

	resp := handler.HandleIMMessageWithProgress(IMUserMessage{
		UserID:   msg.FromUserID,
		Platform: "weixin_local",
		Text:     text,
	}, onProgress)

	if resp == nil {
		return
	}

	m.sendAgentResponse(gw, msg.FromUserID, contextToken, resp)
}

// sendAgentResponse dispatches all parts of an IMAgentResponse to the WeChat user.
func (m *weixinGatewayManager) sendAgentResponse(gw *weixin.Gateway, toUserID, contextToken string, resp *IMAgentResponse) {
	ctx := context.Background()

	// Send text response
	if resp.Text != "" {
		if err := gw.SendText(ctx, weixin.OutgoingText{
			ToUserID:     toUserID,
			Text:         resp.Text,
			ContextToken: contextToken,
		}); err != nil {
			log.Printf("[weixin-mgr] local SendText error (to=%s): %v", toUserID, err)
		}
	}

	// Send error as text if no text was sent
	if resp.Error != "" && resp.Text == "" {
		_ = gw.SendText(ctx, weixin.OutgoingText{
			ToUserID:     toUserID,
			Text:         "❌ " + resp.Error,
			ContextToken: contextToken,
		})
	}

	// Send image if present (base64-encoded screenshot or generated image)
	if resp.ImageKey != "" {
		imgData, err := base64.StdEncoding.DecodeString(resp.ImageKey)
		if err == nil && len(imgData) > 0 {
			_ = gw.SendMedia(ctx, weixin.OutgoingMedia{
				ToUserID:     toUserID,
				ContextToken: contextToken,
				FileData:     imgData,
				MediaType:    "image",
			})
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
	dir := filepath.Join(os.TempDir(), "maclaw-weixin-media")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := msg.MediaName
	if name == "" {
		ext := ".bin"
		switch msg.MediaType {
		case "image":
			ext = ".jpg"
		case "voice":
			ext = ".silk"
		case "video":
			ext = ".mp4"
		}
		name = "wx_" + msg.FromUserID + "_" + time.Now().Format("20060102_150405.000") + ext
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, msg.MediaData, 0o644); err != nil {
		return "", err
	}
	return p, nil
}

// mediaLabel returns a Chinese label for the media type.
func mediaLabel(mediaType string) string {
	switch mediaType {
	case "image":
		return "图片"
	case "voice":
		return "语音"
	case "video":
		return "视频"
	case "file":
		return "文件"
	default:
		return "媒体"
	}
}

// HandleGatewayReply dispatches a reply from Hub to the WeChat API.
func (m *weixinGatewayManager) HandleGatewayReply(reply GatewayReplyPayload) {
	m.mu.Lock()
	gw := m.gateway
	m.mu.Unlock()
	if gw == nil {
		return
	}

	// Resolve context token: prefer from reply payload, fall back to cached
	contextToken := ""
	if ct, ok := reply.Extra["context_token"]; ok {
		if s, ok := ct.(string); ok {
			contextToken = s
		}
	}
	if contextToken == "" {
		contextToken = gw.GetContextToken(reply.PlatformUID)
	}

	switch reply.ReplyType {
	case "text":
		if err := gw.SendText(context.Background(), weixin.OutgoingText{
			ToUserID:     reply.PlatformUID,
			Text:         reply.Text,
			ContextToken: contextToken,
		}); err != nil {
			log.Printf("[weixin-mgr] SendText error (to=%s): %v", reply.PlatformUID, err)
		}
	case "image":
		data, err := base64.StdEncoding.DecodeString(reply.ImageData)
		if err != nil {
			log.Printf("[weixin-mgr] image base64 decode error: %v", err)
			return
		}
		if err := gw.SendMedia(context.Background(), weixin.OutgoingMedia{
			ToUserID:     reply.PlatformUID,
			Caption:      reply.Caption,
			ContextToken: contextToken,
			FileData:     data,
			MediaType:    "image",
		}); err != nil {
			log.Printf("[weixin-mgr] SendMedia(image) error (to=%s): %v", reply.PlatformUID, err)
		}
	case "file":
		data, err := base64.StdEncoding.DecodeString(reply.FileData)
		if err != nil {
			log.Printf("[weixin-mgr] file base64 decode error: %v", err)
			return
		}
		if err := gw.SendMedia(context.Background(), weixin.OutgoingMedia{
			ToUserID:     reply.PlatformUID,
			Caption:      reply.Caption,
			ContextToken: contextToken,
			FileData:     data,
			FileName:     reply.FileName,
			MediaType:    "file",
		}); err != nil {
			log.Printf("[weixin-mgr] SendMedia(file) error (to=%s): %v", reply.PlatformUID, err)
		}
	case "video":
		data, err := base64.StdEncoding.DecodeString(reply.FileData)
		if err != nil {
			log.Printf("[weixin-mgr] video base64 decode error: %v", err)
			return
		}
		if err := gw.SendMedia(context.Background(), weixin.OutgoingMedia{
			ToUserID:     reply.PlatformUID,
			Caption:      reply.Caption,
			ContextToken: contextToken,
			FileData:     data,
			FileName:     reply.FileName,
			MediaType:    "video",
		}); err != nil {
			log.Printf("[weixin-mgr] SendMedia(video) error (to=%s): %v", reply.PlatformUID, err)
		}
	}
}

// ---------------------------------------------------------------------------
// App integration — Wails bindings and lifecycle
// ---------------------------------------------------------------------------

func (a *App) ensureWeixinGateway() {
	if a.weixinGateway == nil {
		a.weixinGateway = newWeixinGatewayManager(a)
	}
	a.weixinGateway.SyncFromConfig()
}

func (a *App) GetWeixinStatus() string {
	if a.weixinGateway == nil {
		return "disconnected"
	}
	return a.weixinGateway.Status()
}

func (a *App) RestartWeixin() string {
	a.ensureWeixinGateway()
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
	cfg.SetWeixinLocal(enabled)
	if err := a.SaveConfig(cfg); err != nil {
		return err
	}
	// Invalidate cached local handler so it's recreated on next message.
	if a.weixinGateway != nil {
		a.weixinGateway.resetLocalHandler()
	}
	return nil
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
		return map[string]string{"error": "获取二维码失败: " + err.Error()}
	}
	return map[string]string{
		"qrcode_url":   qrcodeURL,
		"qrcode_token": qrcodeToken,
	}
}

// WaitWeixinQRLogin waits for the user to scan the QR code and confirm login.
// qrcodeToken is from StartWeixinQRLogin. On success, saves credentials to config.
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
	if err := a.SaveConfig(cfg); err != nil {
		return map[string]string{
			"status": "connected",
			"error":  "登录成功但保存配置失败: " + err.Error(),
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
