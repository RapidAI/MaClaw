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

	"github.com/RapidAI/CodeClaw/corelib/i18n"
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
		} else {
			m.mu.Unlock()
		}
		// Always notify Hub to release gateway claim, even if local gateway
		// was already nil — Hub may still hold a stale claim from a previous run.
		if hubClient := m.app.hubClient(); hubClient != nil && hubClient.IsConnected() {
			_ = hubClient.SendIMGatewayUnclaim("weixin")
			log.Printf("[weixin-mgr] sent gateway unclaim to hub")
		}
		if gw != nil {
			m.emitStatusEvent()
		}
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
	wl := weixin.GetWxLog()
	wl.Log("mgr.status", "---", "-", "status=%s", status)

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

	if status == "session_expired" {
		wl.Log("mgr.status", "---", "-", "session expired — tearing down gateway and releasing hub claim")
		log.Printf("[weixin-mgr] session expired, tearing down gateway")

		// Release Hub gateway claim so Hub doesn't route replies to a dead gateway.
		if hubClient := m.app.hubClient(); hubClient != nil && hubClient.IsConnected() {
			_ = hubClient.SendIMGatewayUnclaim("weixin")
		}

		// Tear down the gateway instance so HandleGatewayReply won't try to use it.
		// NOTE: we must NOT call gw.Stop() synchronously here because this
		// callback runs inside pollLoop → emitStatus, and Stop() waits for
		// pollLoop to finish — that would deadlock. Instead, nil out the
		// gateway reference (so no new messages are dispatched) and let
		// pollLoop's natural return + wg.Done() handle the cleanup.
		m.mu.Lock()
		gw := m.gateway
		m.gateway = nil
		m.lastToken = ""
		m.status = "disconnected"
		lh := m.localHandler
		m.localHandler = nil
		m.mu.Unlock()
		if lh != nil {
			lh.memory.stop()
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
		m.localHandler.memory.stop()
		m.localHandler = nil
	}
}

// onIncomingMessage routes WeChat messages based on config:
//   - Local mode: directly invokes the local MaClaw LLM agent loop
//   - Hub mode: forwards to Hub via im.gateway_message
//   - Hub mode fallback: if Hub is unavailable, notify user and fall back to local
func (m *weixinGatewayManager) onIncomingMessage(msg weixin.IncomingMessage) {
	wl := weixin.GetWxLog()
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
		Text:         "⚠️ 当前为多机模式，但 Hub 未连接。消息已回退到本地处理。\n请检查 Hub 连接状态，或切换回单机模式。",
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
		wl.Log("mgr.forward", "OUT", msg.FromUserID, "ERR SendIMGatewayMessage: %v, fallback to local", err)
		log.Printf("[weixin-mgr] forwardToHub SendIMGatewayMessage error: %v, falling back to local", err)
		m.handleLocalMessage(msg)
	} else {
		wl.Log("mgr.forward", "OUT", msg.FromUserID, "OK sent to hub, has_ctx_token=%v", msg.ContextToken != "")
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
	a.ensureInteractionInfra()

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
	wl := weixin.GetWxLog()
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

	// Build the user message; pass images as multimodal attachments so the
	// LLM can actually "see" them, instead of just a file-path text prefix.
	text := msg.Text
	var attachments []MessageAttachment
	if msg.MediaType != "" && len(msg.MediaData) > 0 {
		if msg.MediaType == "image" {
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

	// Progress callback — send intermediate status to the WeChat user.
	// Use a rate limiter to avoid flooding: at most one progress message per 5s.
	var lastProgress time.Time
	var lastProgressText string
	onProgress := func(progressText string) {
		if progressText == "" || progressText == imHeartbeatMsg {
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
			Text:         i18n.T(i18n.MsgProgressPrefix, "zh") + textutil.StripMarkdown(progressText),
			ContextToken: contextToken,
		})
	}

	wl.Log("mgr.local", "---", msg.FromUserID, "calling HandleIMMessageWithProgress text_len=%d attachments=%d", len(text), len(attachments))
	resp := handler.HandleIMMessageWithProgress(IMUserMessage{
		UserID:      msg.FromUserID,
		Platform:    "weixin_local",
		Text:        text,
		Lang:        "zh",
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

	switch reply.ReplyType {
	case "text":
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
	case "image":
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
			MediaType:    "image",
		}); err != nil {
			wl.Log("mgr.hubReply", "OUT", reply.PlatformUID, "ERR SendMedia(image): %v", err)
			log.Printf("[weixin-mgr] SendMedia(image) error (to=%s): %v", reply.PlatformUID, err)
		} else {
			wl.Log("mgr.hubReply", "OUT", reply.PlatformUID, "OK SendMedia(image) size=%d", len(data))
		}
	case "file":
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
			MediaType:    "file",
		}); err != nil {
			wl.Log("mgr.hubReply", "OUT", reply.PlatformUID, "ERR SendMedia(file): %v", err)
			log.Printf("[weixin-mgr] SendMedia(file) error (to=%s): %v", reply.PlatformUID, err)
		} else {
			wl.Log("mgr.hubReply", "OUT", reply.PlatformUID, "OK SendMedia(file) name=%s size=%d", reply.FileName, len(data))
		}
	case "video":
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
			MediaType:    "video",
		}); err != nil {
			wl.Log("mgr.hubReply", "OUT", reply.PlatformUID, "ERR SendMedia(video): %v", err)
			log.Printf("[weixin-mgr] SendMedia(video) error (to=%s): %v", reply.PlatformUID, err)
		} else {
			wl.Log("mgr.hubReply", "OUT", reply.PlatformUID, "OK SendMedia(video) size=%d", len(data))
		}
	default:
		wl.Log("mgr.hubReply", "---", reply.PlatformUID, "WARN unknown reply_type=%s", reply.ReplyType)
	}
}

// ---------------------------------------------------------------------------
// App integration — Wails bindings and lifecycle
// ---------------------------------------------------------------------------

// ensureWeixinGateway lazily creates the gateway manager and syncs from config.
// If WeChat is not enabled in config, skips entirely to avoid unnecessary work.
func (a *App) ensureWeixinGateway() {
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

// PollWeixinQRStatus performs a single poll of the QR code status.
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

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	result, status, err := weixin.PollQRStatus(ctx, baseURL, qrcodeToken)
	if err != nil {
		return map[string]string{"error": err.Error(), "status": "error"}
	}
	resp := map[string]string{
		"status":  status,
		"message": result.Message,
	}
	if status == "confirmed" {
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
		if err := a.SaveConfig(cfg); err != nil {
			resp["error"] = "登录成功但保存配置失败: " + err.Error()
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
