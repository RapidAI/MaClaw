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

	"github.com/RapidAI/CodeClaw/corelib/textutil"
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
//   - Hub mode fallback: if Hub is unavailable, notify user and fall back to local
func (m *weixinGatewayManager) onIncomingMessage(msg weixin.IncomingMessage) {
	cfg, err := m.app.LoadConfig()
	if err != nil {
		log.Printf("[weixin-mgr] LoadConfig error: %v", err)
		return
	}

	isLocal := cfg.IsWeixinLocalMode()
	localModePtr := cfg.WeixinLocalMode // nil = never set (default local)
	hubClient := m.app.hubClient()
	hubNil := hubClient == nil
	hubConn := !hubNil && hubClient.IsConnected()
	log.Printf("[weixin-mgr] onIncomingMessage: user=%s local_mode=%v local_mode_ptr=%v hub_nil=%v hub_connected=%v",
		msg.FromUserID, isLocal, localModePtr, hubNil, hubConn)

	if isLocal {
		m.handleLocalMessage(msg)
		return
	}

	// Hub mode — try forwarding; fall back to local if Hub unavailable.
	if hubNil || !hubConn {
		log.Printf("[weixin-mgr] Hub mode but Hub unavailable, falling back to local: user=%s", msg.FromUserID)
		m.notifyHubUnavailable(msg)
		m.handleLocalMessage(msg)
		return
	}

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
		Text:         "⚠️ 当前为多机模式，但 Hub 未连接。消息已回退到本地处理。\n请检查 Hub 连接状态，或切换回单机模式。",
		ContextToken: msg.ContextToken,
	})
}

// forwardToHub sends the message to Hub via im.gateway_message (original behaviour).
func (m *weixinGatewayManager) forwardToHub(msg weixin.IncomingMessage) {
	hubClient := m.app.hubClient()
	if hubClient == nil || !hubClient.IsConnected() {
		log.Printf("[weixin-mgr] forwardToHub FAILED: hub_nil=%v user=%s", hubClient == nil, msg.FromUserID)
		// Fall back to local processing instead of silently dropping.
		m.notifyHubUnavailable(msg)
		m.handleLocalMessage(msg)
		return
	}

	msgType := "text"
	if msg.MediaType != "" && len(msg.MediaData) > 0 {
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

	if err := hubClient.SendIMGatewayMessage("weixin", payload); err != nil {
		log.Printf("[weixin-mgr] forwardToHub SendIMGatewayMessage error: %v", err)
	} else {
		log.Printf("[weixin-mgr] forwardToHub OK: user=%s text=%q", msg.FromUserID, truncateForLog(msg.Text, 30))
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
			Text:         "⏳ " + textutil.StripMarkdown(progressText),
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

	// Send text response
	if resp.Text != "" {
		text := textutil.StripMarkdown(resp.Text)
		if err := gw.SendText(ctx, weixin.OutgoingText{
			ToUserID:     toUserID,
			Text:         text,
			ContextToken: contextToken,
		}); err != nil {
			log.Printf("[weixin-mgr] local SendText error (to=%s): %v", toUserID, err)
		}
	}

	// Send error as text if no text was sent
	if resp.Error != "" && resp.Text == "" {
		_ = gw.SendText(ctx, weixin.OutgoingText{
			ToUserID:     toUserID,
			Text:         "❌ " + textutil.StripMarkdown(resp.Error),
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
	return saveMediaToTempDir("maclaw-weixin-media", "wx_", msg.FromUserID, msg.MediaType, msg.MediaData, msg.MediaName)
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

// HandleGatewayReply dispatches a reply from Hub to the WeChat API.
func (m *weixinGatewayManager) HandleGatewayReply(reply GatewayReplyPayload) {
	log.Printf("[weixin-mgr] HandleGatewayReply: type=%s uid=%s text_len=%d", reply.ReplyType, reply.PlatformUID, len(reply.Text))
	m.mu.Lock()
	gw := m.gateway
	m.mu.Unlock()
	if gw == nil {
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
		log.Printf("[weixin-mgr] HandleGatewayReply: WARNING no contextToken for uid=%s, reply will likely fail", reply.PlatformUID)
	}

	switch reply.ReplyType {
	case "text":
		if err := gw.SendText(context.Background(), weixin.OutgoingText{
			ToUserID:     reply.PlatformUID,
			Text:         textutil.StripMarkdown(reply.Text),
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
	// Switching to hub mode requires prior Hub registration.
	if !enabled && cfg.RemoteMachineID == "" {
		return fmt.Errorf("请先注册到 Hub（设置 Hub 地址并完成注册），再开启多机模式")
	}
	cfg.SetWeixinLocal(enabled)
	if err := a.SaveConfig(cfg); err != nil {
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
			hubClient.SendIMGatewayClaim("weixin")
			log.Printf("[weixin-mgr] sent gateway claim after switching to hub mode")
		} else {
			log.Printf("[weixin-mgr] WARNING: cannot send gateway claim, hub not available")
		}
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
